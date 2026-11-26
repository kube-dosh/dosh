package app

import "dosh/internal/domain"

type storageCatalogDefinition struct {
	Item      domain.StorageCatalogItem
	ChartName string
	PodTokens []string
}

// builtinStorageCatalog is deliberately small. Each entry is an integration
// profile maintained by Dosh: it says which upstream Chart to use and what a
// successful installation should look like. It does not fork or wrap the
// storage implementation, and absence of an entry never blocks Generic Helm.
var builtinStorageCatalog = []storageCatalogDefinition{
	{
		Item: domain.StorageCatalogItem{
			ID:                     "local-path-provisioner",
			Name:                   "Local Path Provisioner",
			Vendor:                 "Rancher",
			Project:                "rancher/local-path-provisioner",
			Category:               "node-local",
			Description:            "使用节点本地目录动态供应 PV，部署简单，适合单节点、开发、边缘和测试环境。数据与节点绑定，不具备跨节点高可用能力。",
			SupportLevel:           domain.StorageSupportVerified,
			DefaultNamespace:       "local-path-storage",
			DefaultReleaseName:     "local-path-provisioner",
			Provisioners:           []string{"rancher.io/local-path"},
			ExpectedStorageClasses: []string{"local-path"},
			Capabilities:           []string{"动态供应", "ReadWriteOnce", "节点本地数据"},
			Prerequisites:          []string{"节点本地磁盘可用", "调度到目标节点的工作负载可接受本地数据绑定"},
			Warnings:               []string{"Local Path 不是高可用存储。Pod 迁移到其他节点后不能自动访问原节点上的数据。"},
		},
		ChartName: "local-path-provisioner",
		PodTokens: []string{"local-path", "local-path-provisioner"},
	},
	{
		Item: domain.StorageCatalogItem{
			ID:                     "csi-driver-nfs",
			Name:                   "CSI Driver for NFS",
			Vendor:                 "Kubernetes CSI",
			Project:                "kubernetes-csi/csi-driver-nfs",
			Category:               "csi-network-file",
			Description:            "通过 CSI 接口挂载外部 NFS 共享，支持标准 CSI 生命周期和 StorageClass 参数配置。",
			SupportLevel:           domain.StorageSupportVerified,
			DefaultNamespace:       "kube-system",
			DefaultReleaseName:     "csi-driver-nfs",
			Provisioners:           []string{"nfs.csi.k8s.io"},
			ExpectedStorageClasses: []string{"nfs-csi"},
			Capabilities:           []string{"CSI Driver", "动态供应", "ReadWriteMany", "快照能力取决于驱动版本"},
			Prerequisites:          []string{"可访问的 NFS Server 与导出路径", "节点具备 NFS 客户端挂载能力", "集群允许部署 CSI Controller 与 Node Plugin"},
			Warnings:               []string{"NFS 服务的高可用、备份和容量不由该 CSI Driver 提供。"},
		},
		ChartName: "csi-driver-nfs",
		PodTokens: []string{"csi-nfs", "nfs.csi.k8s.io"},
	},
}

const storageGenericCatalogID = "generic-helm"

func findStorageCatalogDefinition(id string) (storageCatalogDefinition, bool) {
	for _, definition := range builtinStorageCatalog {
		if definition.Item.ID == id {
			return definition, true
		}
	}
	return storageCatalogDefinition{}, false
}
