# Storage Center

Dosh 的存储能力分为两层：

1. **Kubernetes 原生资源**：StorageClass、PersistentVolume、PersistentVolumeClaim、VolumeSnapshot 的浏览、创建、更新、删除、YAML 和关联影响分析。
2. **存储中心（Storage Center）**：发现存储环境、展示受控方案目录、复用 Helm 安装、执行预检、安装后验证、可选 Smoke Test 和诊断。

StorageClass 不等于存储驱动。一个驱动可以创建多个 StorageClass，也可以被纳管为已有资源。Dosh 在产品层把“方案/驱动”和“StorageClass”分开，避免把驱动安装逻辑绑死在单个 StorageClass 上。

## 三层来源模型

### Dosh Verified

Dosh 只为少量方案维护薄集成描述。上游仍负责 Chart、CRD、镜像、版本和安全修复；Dosh 负责目录展示、固定版本、预检、验证和诊断入口。

首期内置条目：

- Local Path Provisioner
- CSI Driver for NFS

NFS 文件存储只推荐维护中的 CSI Driver for NFS。已停止维护的 NFS Subdir External Provisioner 不再作为内置方案推荐，也不会提供安装入口；集群中已有的相关 StorageClass 仍会按纳管资源只读展示。

Verified 不等于厂商认证，也不代表存储后端具备高可用。它表示 Dosh 为该方案提供了完整的安装前预检和安装后验证路径。

### Generic Helm

管理员已登记的可信 Helm/OCI 仓库中的任意 Chart 可以使用现有 Helm 受控安装链路。Dosh 继续执行：

- 固定 Chart 版本和摘要；
- Values Schema 校验；
- 渲染资源冲突检查；
- 逐资源 Kubernetes RBAC 预检；
- 安装计划摘要和 Release 名称确认；
- 操作记录和逐资源执行进度。

Generic Helm 不会被标记为已验证。它不会因为缺少 Dosh 专用元数据而无法安装。

### Adopted

Dosh 会发现集群中已有的 CSIDriver、Provisioner 和 StorageClass，并按只读方式纳管展示：

- 不自动接管 Helm 所有权；
- 不自动升级或卸载；
- 不删除现有 StorageClass/PV/PVC；
- 验证和诊断按显式用户操作执行。

## 元数据边界

Dosh 不要求存储厂商为 Dosh 维护元数据。厂商或开源社区维护上游 Chart、CRD 和镜像。Dosh 团队只维护少量 Verified 条目，用来描述：

- 使用哪个可信仓库中的哪个 Chart；
- 上游项目坐标，例如 `rancher/local-path-provisioner`；
- 默认命名空间和 Release；
- 预期 Provisioner 和 StorageClass；
- 前置条件、限制和风险；
- 驱动工作负载的识别关键字。

没有被 Dosh 收录的驱动继续走 Generic Helm，不会形成生态阻塞。

### Provisioner 与上游项目

`provisioners` 中的值是 Kubernetes StorageClass/CSI 使用的驱动标识，例如 Local Path Provisioner 的 `rancher.io/local-path`。它不是项目网址，也不能直接用于访问 GitHub 或 ArtifactHub；Dosh 依靠该值匹配集群中的真实 StorageClass。

`project` 字段才是上游项目坐标，例如 `rancher/local-path-provisioner`。UI 将两者分别展示为“Provisioner（Kubernetes 标识）”和“上游项目”，避免把驱动标识误认为项目来源。

## 受控安装流程

1. 存储中心从已启用的可信 Helm 仓库解析 Chart。
2. 生成安装计划前执行方案预检：
   - Chart 是否可用；
   - 命名空间和 Release 是否合法、是否存在冲突；
   - 节点是否 Ready；
   - 预期 StorageClass 是否已存在；
   - 当前身份是否具备 Helm Release Secret 写入权限；
   - 默认 StorageClass 是否冲突；
   - Local Path 等方案的特殊限制。
3. 进入现有 Helm 安装向导，审阅 Values、渲染资源、CRD、Hook、RBAC 和计划摘要。
4. 完成后可由存储中心直接进入验证。

### 安装命名空间

存储中心的 Provider 安装命名空间与业务 PVC 命名空间是两件事。安装向导默认选中方案推荐的 Provider 命名空间，同时保留管理员修改能力：

- 已有命名空间通过可搜索下拉框选择；
- 推荐命名空间尚不存在时，明确展示“推荐，将新建”；
- 下拉底部始终保留“手动输入”兜底；
- 不存在或手动输入的命名空间会显式开启 Helm `createNamespace`，不会隐式创建；
- `Terminating` 命名空间不会出现在列表中；
- 命名空间列表因权限不足无法读取时自动降级为手动输入，不阻塞安装；
- 选择 `default`、`kube-system` 或 `kube-*` 时展示系统命名空间高风险提示。

存储驱动通常需要集群级权限、hostPath、特权 DaemonSet 或节点挂载能力，因此安装必须按高风险集群运维操作对待。

## 验证与 Smoke Test

验证会检查：

- StorageClass 是否存在；
- Provisioner 是否可识别；
- CSI Driver 是否完成注册（仅对 CSI 方案）；
- 驱动 Controller/Node Plugin 工作负载是否 Running；
- 默认 StorageClass 是否冲突；
- 可选的动态供应和挂载 Smoke Test。

Smoke Test 会创建：

- 一个 1Gi 的临时 PVC；
- 一个挂载该 PVC 的临时 pause Pod；
- 默认使用 `registry.k8s.io/pause:3.10`，可在页面填写节点可拉取的替代镜像。

Pod 是必要的，因为很多 StorageClass 使用 `WaitForFirstConsumer`，只有创建消费者 Pod 后才会触发供应。验证完成后 Dosh 会清理临时 Pod 和 PVC。验证需要目标命名空间的 Pod/PVC create/delete 权限。

## API

- `GET /api/v1/storage/center`：环境概览、目录、已有驱动和诊断。
- `POST /api/v1/storage/center/preflight`：对选中方案执行预检。
- `POST /api/v1/storage/center/verify`：验证 StorageClass，可选执行 Smoke Test。

所有请求继续使用 Dosh 会话、CSRF 和 Kubernetes impersonation。读取和写入仍由目标身份在集群中的 RBAC 决定。

## 当前边界

- 不提供离线包导入或页面上传驱动。
- 不接受任意 URL 安装。
- 不实现云厂商托管存储 Adapter；接口边界保留，后续再接入。
- 不自动安装默认存储驱动。
- 不自动接管已有 CSI 的 Helm 所有权。
- NFS 方案只安装 Provisioner/CSI Driver，不负责高可用 NFS 服务本身。
- Local Path 数据与节点绑定，不适合跨节点高可用生产数据。
