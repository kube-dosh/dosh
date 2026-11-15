package app

import (
	"fmt"
	"strings"

	"dosh/internal/domain"
)

const (
	riskCritical = "critical"
	riskHigh     = "high"
	riskNormal   = "normal"
)

// resourceRisk classifies writes to cluster-wide or security-sensitive
// resource types. The classification is deterministic and evaluated on the
// server so UI controls cannot be bypassed by calling the API directly.
func resourceRisk(descriptor domain.ResourceDescriptor) (string, []string, bool) {
	kind := strings.ToLower(descriptor.Kind)
	resource := strings.ToLower(descriptor.Resource)
	group := strings.ToLower(descriptor.Group)
	reasons := make([]string, 0, 3)
	level := riskNormal

	switch {
	case group == "" && resource == "namespaces", resource == "nodes",
		group == "apiextensions.k8s.io" && resource == "customresourcedefinitions",
		group == "admissionregistration.k8s.io" && (resource == "validatingwebhookconfigurations" || resource == "mutatingwebhookconfigurations"),
		group == "rbac.authorization.k8s.io" && (resource == "clusterroles" || resource == "clusterrolebindings"),
		group == "storage.k8s.io" && (resource == "storageclasses" || resource == "csidrivers" || resource == "csinodes"),
		group == "" && resource == "persistentvolumes",
		group == "scheduling.k8s.io" && resource == "priorityclasses",
		group == "apiregistration.k8s.io" && resource == "apiservices":
		level = riskCritical
	case group == "rbac.authorization.k8s.io" && (resource == "roles" || resource == "rolebindings"),
		group == "networking.k8s.io" && resource == "networkpolicies",
		group == "policy" && resource == "poddisruptionbudgets",
		group == "" && (resource == "serviceaccounts" || resource == "resourcequotas" || resource == "limitranges"),
		group == "admissionregistration.k8s.io" && resource == "validatingadmissionpolicies",
		group == "admissionregistration.k8s.io" && resource == "validatingadmissionpolicybindings",
		group == "coordination.k8s.io" && resource == "leases":
		level = riskHigh
	}
	if level == riskNormal {
		return level, nil, false
	}

	switch {
	case kind == "namespace":
		reasons = append(reasons, "Namespace 变更可能影响其下全部工作负载、网络策略与配额")
	case kind == "node":
		reasons = append(reasons, "节点对象写入可能影响调度、污点、容量和工作负载可用性")
	case kind == "customresourcedefinition":
		reasons = append(reasons, "CRD 变更可能使现有自定义资源无法读取、升级或恢复")
	case strings.Contains(kind, "webhook"):
		reasons = append(reasons, "准入 Webhook 变更可能影响整个集群的创建、更新与删除请求")
	case kind == "clusterrole" || kind == "clusterrolebinding":
		reasons = append(reasons, "集群 RBAC 变更可能扩大或撤销整个集群的访问权限")
	case kind == "role" || kind == "rolebinding":
		reasons = append(reasons, "RBAC 变更可能扩大或撤销命名空间内的访问权限")
	case kind == "storageclass" || kind == "persistentvolume" || kind == "csidriver" || kind == "csinode":
		reasons = append(reasons, "存储对象写入可能影响持久化数据、挂载和节点存储路径")
	case kind == "networkpolicy":
		reasons = append(reasons, "网络策略变更可能立即阻断或放开工作负载流量")
	case kind == "poddisruptionbudget":
		reasons = append(reasons, "PDB 变更可能影响节点维护和自愿驱逐期间的可用性")
	case kind == "priorityclass":
		reasons = append(reasons, "优先级变更可能影响抢占、调度顺序和工作负载稳定性")
	case kind == "apiservice":
		reasons = append(reasons, "APIService 变更可能使聚合 API 不可用")
	default:
		reasons = append(reasons, "该资源类型会影响集群治理、授权或运行安全")
	}
	if !descriptor.Namespaced {
		reasons = append(reasons, "这是集群级资源，影响范围可能覆盖全部命名空间")
	}
	return level, reasons, true
}

func requireWriteConfirmation(descriptor domain.ResourceDescriptor, name, confirmation string, dryRun bool) error {
	if dryRun {
		return nil
	}
	level, _, required := resourceRisk(descriptor)
	if !required {
		return nil
	}
	if confirmation != name {
		return fmt.Errorf("%w: %s 风险资源写入需要输入资源名称 %q 进行二次确认", domain.ErrInvalid, level, name)
	}
	return nil
}
