package app

import (
	"context"
	"fmt"
	"strings"

	"dosh/internal/domain"
)

func (s *Service) diagnosePersistentVolumeClaim(ctx context.Context, actor domain.Actor, namespace, name string) (*reportBuilder, error) {
	claim, err := s.repository.GetPersistentVolumeClaim(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	report := newReport("PersistentVolumeClaim", namespace, name, claim.Status)
	report.relatedResource("PersistentVolumeClaim", namespace, name, claim.Status, "当前对象", pvcPath(namespace, name))
	if claim.VolumeName != "" {
		report.relatedResource("PersistentVolume", "", claim.VolumeName, "", "绑定到持久卷", fmt.Sprintf("/storage/volumes/%s", claim.VolumeName))
	}

	switch claim.Status {
	case "Bound":
		report.check("phase", "PVC 绑定状态", domain.DiagnosisCheckPass, "PVC 已绑定到持久卷 "+claim.VolumeName)
	case "Pending":
		report.check("phase", "PVC 绑定状态", domain.DiagnosisCheckWarning, "PVC 仍在等待绑定")
		report.finding(domain.DiagnosisFinding{
			ID: "pvc.pending", Severity: domain.DiagnosisWarning, Title: "PVC 尚未完成绑定",
			Cause: "PVC 可能需要等待 StorageClass 动态供应、可用 PV 或节点拓扑条件。若长时间 Pending，Pod 挂载也会持续阻塞。",
			Evidence: []domain.DiagnosisEvidence{
				{Label: "状态", Value: claim.Status},
				{Label: "StorageClass", Value: fallback(claim.StorageClass, "未指定")},
				{Label: "请求容量", Value: formatBytes(claim.RequestedCapacityBytes)},
			},
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "查看 PVC 详情", Path: pvcPath(namespace, name)},
				{Label: "复制排查命令", Command: fmt.Sprintf("kubectl describe pvc %s -n %s", name, namespace)},
			},
		})
	case "Lost":
		report.check("phase", "PVC 绑定状态", domain.DiagnosisCheckCritical, "PVC 已与其绑定的持久卷失去关联")
		report.finding(domain.DiagnosisFinding{
			ID: "pvc.lost", Severity: domain.DiagnosisCritical, Title: "PVC 已进入 Lost 状态",
			Cause: "PersistentVolume 已不存在或绑定关系被破坏，使用该 PVC 的工作负载可能无法继续读写数据。",
			Evidence: []domain.DiagnosisEvidence{
				{Label: "PV", Value: fallback(claim.VolumeName, "未记录")},
				{Label: "StorageClass", Value: fallback(claim.StorageClass, "未指定")},
			},
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "查看 PV", Path: "/storage?tab=volumes"},
				{Label: "复制排查命令", Command: fmt.Sprintf("kubectl describe pvc %s -n %s", name, namespace)},
			},
		})
	default:
		report.check("phase", "PVC 绑定状态", domain.DiagnosisCheckInfo, "当前状态为 "+fallback(claim.Status, "Unknown"))
	}

	if claim.StorageClass == "" {
		if claim.VolumeName == "" {
			report.check("storage-class", "StorageClass", domain.DiagnosisCheckWarning, "未指定 StorageClass，只能等待静态 PV 绑定")
			report.finding(domain.DiagnosisFinding{
				ID: "pvc.no-storage-class", Severity: domain.DiagnosisWarning, Title: "PVC 未指定 StorageClass",
				Cause:           "除非集群中存在可匹配的静态 PersistentVolume，否则该 PVC 无法动态供应存储。",
				Evidence:        []domain.DiagnosisEvidence{{Label: "StorageClass", Value: "未指定"}, {Label: "绑定 PV", Value: fallback(claim.VolumeName, "未绑定")}},
				Recommendations: []domain.DiagnosisRecommendation{{Label: "查看存储配置", Path: pvcPath(namespace, name)}},
			})
		} else {
			report.check("storage-class", "StorageClass", domain.DiagnosisCheckInfo, "PVC 已通过静态 PV 绑定")
		}
	} else {
		report.check("storage-class", "StorageClass", domain.DiagnosisCheckPass, "StorageClass 为 "+claim.StorageClass)
	}

	if claim.RequestedCapacityBytes <= 0 {
		report.check("capacity", "容量请求", domain.DiagnosisCheckCritical, "PVC 没有有效的存储容量请求")
		report.finding(domain.DiagnosisFinding{
			ID: "pvc.invalid-capacity", Severity: domain.DiagnosisCritical, Title: "PVC 容量请求无效",
			Cause:           "PVC 必须声明大于 0 的 storage 请求，否则无法完成供应与绑定。",
			Evidence:        []domain.DiagnosisEvidence{{Label: "请求容量", Value: formatBytes(claim.RequestedCapacityBytes)}},
			Recommendations: []domain.DiagnosisRecommendation{{Label: "查看 PVC", Path: pvcPath(namespace, name)}},
		})
	} else if claim.Status == "Bound" && claim.CapacityBytes <= 0 {
		report.check("capacity", "容量请求", domain.DiagnosisCheckWarning, "PVC 已绑定但未报告实际容量")
	} else {
		report.check("capacity", "容量请求", domain.DiagnosisCheckPass, "请求 "+formatBytes(claim.RequestedCapacityBytes)+"，当前容量 "+formatBytes(claim.CapacityBytes))
	}

	if len(claim.Conditions) == 0 {
		report.check("conditions", "PVC Conditions", domain.DiagnosisCheckInfo, "PVC 未报告 Condition")
	} else {
		status := domain.DiagnosisCheckPass
		for _, condition := range claim.Conditions {
			detail := fallback(condition.Message, fallback(condition.Reason, condition.Type+"="+condition.Status))
			severity := ""
			if strings.Contains(strings.ToLower(condition.Type), "error") || strings.Contains(strings.ToLower(condition.Reason), "fail") || strings.Contains(strings.ToLower(condition.Reason), "error") {
				severity = domain.DiagnosisWarning
				status = domain.DiagnosisCheckWarning
			}
			if severity == "" {
				continue
			}
			report.finding(domain.DiagnosisFinding{
				ID: "pvc.condition." + strings.ToLower(condition.Type), Severity: severity, Title: "PVC Condition 报告异常",
				Cause: detail,
				Evidence: []domain.DiagnosisEvidence{
					{Label: "Condition", Value: condition.Type + "=" + condition.Status},
					{Label: "原因", Value: fallback(condition.Reason, "Unknown")},
				},
				Recommendations: []domain.DiagnosisRecommendation{{Label: "查看 PVC 事件", Path: pvcPath(namespace, name)}},
			})
		}
		report.check("conditions", "PVC Conditions", status, fmt.Sprintf("共 %d 个 Condition", len(claim.Conditions)))
	}

	s.collectEvents(ctx, actor, namespace, "PersistentVolumeClaim", name, pvcPath(namespace, name)+"?tab=events", report)
	return report, nil
}
