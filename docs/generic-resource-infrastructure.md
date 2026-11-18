# 通用 Kubernetes 资源接入基础设施

| 项目 | 内容 |
|---|---|
| 状态 | 第七阶段已实现：在第六阶段通用资源读写与排障能力之上，补齐 Pod/Service 临时端口转发、Helm 受控升级、搜索短缓存与提前取消、关系拓扑筛选和常用工作负载推荐模板；固定资源页面继续保留并强化 |
| 目标 | 从固定资源控制台走向通用 Kubernetes 控制台，为 CRD、Operator 资源和后续动态表单提供统一底座 |
| 边界 | 不开放任意 API 代理、客户端指定上游 URL、跨资源批量原子写入或绕过 SubjectAccessReview 的读写路径；通用 Secret 创建/更新仍由专用页面承担 |

## 1. EARS 任务清单

### GR-001 资源发现

当 Kubernetes API Discovery 可用时，系统应返回核心 API 和每个 API Group 的全部 served version 中支持 `list` 或 `get` 的资源类型，并包含 Group、Version、Resource、Kind、作用域、preferred 标记、动词、短名称、类别和单数名称。

### GR-002 部分发现失败降级

当某个 API Group 或聚合 API 发现失败时，系统应保留其他成功发现的资源类型，返回可展示的 warning，而不是阻断整个资源浏览器。

### GR-003 发现缓存

当同一 Dosh 进程在 60 秒内重复读取资源目录时，系统应复用最近一次成功结果，避免重复扫描全部 API Group；共享目录必须使用服务端发现凭据读取，不能因首个调用者的身份不同而污染缓存。

### GR-004 GVR 校验

当用户请求列表或详情时，系统应只接受 Discovery 返回的 `group/version/resource`，不得接受任意 URL、子资源路径或未发现类型。

### GR-005 列表授权

当读取资源列表时，系统应在调用 Kubernetes 动态客户端前以当前身份执行 `list` SubjectAccessReview；Namespaced 资源应使用请求命名空间，Cluster-scoped 资源应使用空命名空间。

### GR-006 详情授权与命名空间约束

当读取 Namespaced 资源详情时，系统应要求显式 namespace，并以 `get` 动词和资源名称执行授权；未发现类型返回 404，非法 GVR 返回 400，无权限返回 403。

### GR-007 敏感资源保护

当通用视图读取核心 Secret 时，系统应移除 `data` 和 `stringData`，并将全部 metadata annotations 的值替换为遮蔽标记，避免 `kubectl.kubernetes.io/last-applied-configuration` 等字段夹带敏感内容；遮蔽必须在对象与 YAML 中同时生效，并写入 `get-generic-secret` 成功或失败审计。敏感值只能通过原有、受控且带审计的 Secret 专用流程查看。

### GR-008 标准资源清理

当返回任意资源详情时，系统应移除 `metadata.managedFields`，避免暴露无助于排障的字段管理记录，并保持 resourceVersion、labels、annotations、创建时间等标准元数据可读；Secret 的 annotation 值按 GR-007 统一遮蔽。

### GR-009 分页与搜索

当列表包含搜索结果时，系统应使用有界 Kubernetes 扫描、不透明 cursor 和明确截断 warning，避免把 Kubernetes continue token 暴露给浏览器；排序仅作用于当前页。

### GR-010 前端类型目录

当用户打开资源浏览器时，系统应展示发现到的资源类型，支持按 Kind、资源名、API Group、短名称搜索和按 API Group 筛选，并从类型目录进入对应资源列表。

### GR-011 通用对象浏览

当用户选择资源类型时，系统应展示该类型的对象列表、命名空间筛选、分页、详情、事件和 YAML；Namespaced 资源缺少 namespace 时应给出可恢复提示。

### GR-012 固定视图保留

系统应保留 Pod、Service、ConfigMap、Secret、存储、节点、工作负载等现有固定页面。它们继续提供经过编排的写操作、关联关系和排障入口；资源浏览器提供通用读写兜底，不替代这些高价值工作流。

### GR-013 安全边界

系统不得提供任意 Kubernetes API 代理、客户端指定的上游地址、跨资源批量写入或绕过 SubjectAccessReview 的通用读写路径。

### GR-014 本地模式权限

当 `auth.mode=local` 且 `rbac.localReadAllResources=true` 时，Helm Chart 应授予运行 ServiceAccount 全资源 `get/list/watch` 和 API Discovery 权限，使单管理员模式下的资源浏览器可覆盖内置资源与 CRD；Token/OIDC 模式不得复用该规则，必须继续以用户身份执行 SubjectAccessReview 和 impersonation。

### GR-015 类型感知列

当用户列出资源对象时，系统应优先使用 Kubernetes Table API 返回该类型的显示列和逐行 cells；当 Table API 不可用或返回无效响应时，应回退到动态客户端和基础元数据列，并在分页 metadata 中给出降级 warning。列名应去重，priority 大于 0 的宽列不进入默认视图。

### GR-016 Discovery 授权过滤

当 Discovery 请求携带 namespace 时，系统应使用当前身份对该 namespace 执行 `SelfSubjectRulesReview`，并额外使用空 namespace 获取集群级规则；只有具备无 resourceNames 限制的 `list` 权限的资源类型才应出现在目录中。规则应短期缓存，失败时按失败关闭策略隐藏对应作用域资源并返回 warning，不得逐资源发送 SubjectAccessReview。

### GR-017 受控全局搜索

当用户从全局搜索输入至少两个字符时，系统应在当前身份可 list 的已发现资源类型中执行有界搜索。用户可选择单个命名空间或 `all`；`all` 模式最多扫描 8 个命名空间、24 个资源类型和 64 个类型/命名空间组合，并为每个命名空间独立执行 `SelfSubjectRulesReview`，失败或无权访问的命名空间必须跳过。结果必须去重，并对类型、命名空间或结果截断返回 warning；不得把无权限命名空间的名称或对象泄露给当前身份。

### GR-018 治理资源入口

当用户打开治理入口时，系统应展示 ServiceAccount、Role、RoleBinding、ClusterRole、ClusterRoleBinding、NetworkPolicy、HPA、PDB、ResourceQuota、LimitRange 和 PriorityClass 的统一入口，并复用通用列表与详情。详情页应解释 RoleBinding 角色引用与授权主体、NetworkPolicy Pod 选择器、HPA 扩缩目标、PDB 可用性约束等关键关系。

### GR-019 Pod Previous 日志

当用户请求 `previous=true` 时，系统应读取上一次容器实例日志；在建立日志流前必须执行 `get pods/log` SubjectAccessReview，并对成功、失败和拒绝记录 `stream-pod-logs` 审计。前端应提供 Previous 开关、容器、tailLines、时间戳和当前日志窗口关键词筛选。

### GR-020 通用资源创建

当用户对已发现资源提交创建请求时，系统应校验 `group/version/resource`、单文档、1 MiB 上限、`apiVersion`、`kind`、`metadata.name` 和命名空间，执行 `create` SubjectAccessReview；非 Dry Run 请求必须先在 API Server 执行一次同操作 Dry Run，再正式创建，并记录成功或失败审计。通用 Secret 创建必须拒绝并提示使用专用页面。

### GR-021 通用资源更新

当用户更新已发现资源时，系统应校验路径名称与 YAML 名称一致，优先要求 `update`，仅在 `update` 被拒绝且身份具备 `patch` 时回退到 Server-Side Apply；正式写入前必须执行同操作 Dry Run，并携带可用 `resourceVersion`。通用 Secret 更新必须拒绝。

### GR-022 通用资源删除与能力查询

当用户查询资源能力时，系统应基于 Discovery 动词和当前身份 SubjectAccessReview 返回 create/update/delete；当用户删除资源时，应要求 `delete` 权限、支持 Background/Foreground/Orphan 策略、可携带 `resourceVersion` 前置条件，并记录删除审计。前端必须以后端能力查询结果而非仅 Discovery 动词决定按钮状态。

### GR-023 工作负载发布历史与回滚

当用户查看 Deployment 或 StatefulSet 时，系统应展示 ReplicaSet 或 ControllerRevision 形成的 revision、镜像、变更原因、当前标记和可用状态；当用户发起回滚且具备 `update` 权限时，系统应校验正整数 revision，通过定向 Merge Patch 恢复目标 Pod 模板和 change-cause，保留副本数、状态及其他并发字段，并记录 `rollback` 审计。

### GR-024 多版本选择

当同一 API Group/Resource 暴露多个 served version 时，资源浏览器应聚合为一个资源类型并标记 preferred version；列表页和详情页应允许切换 version，并让后续列表、详情、Schema、能力查询和写入继续使用所选 GVR。跨类型搜索应只使用 preferred version，避免重复对象。

### GR-025 OpenAPI Schema 与动态表单

当用户创建或编辑通用资源时，系统应通过受保护接口读取集群 OpenAPI v2 定义，只返回根 Schema 和递归引用定义，并按 TTL 缓存。Schema 可用时，前端应提供 YAML/动态表单双模式，对必填字段和基础类型给出提示；服务端对明确缺失或类型错误执行预检。Schema 不可用时必须降级到 YAML，不得阻断资源工作流，API Server Dry Run 始终是权威校验。

### GR-026 高风险写入策略

当资源类型属于 Namespace、Node、CRD、准入 Webhook、集群 RBAC、PV/StorageClass、PriorityClass 等高影响对象时，能力查询应返回 `riskLevel`、`riskReasons` 和 `requireConfirmation`。正式创建、更新和删除必须由服务端再次校验资源名称确认值；Dry Run 不需要确认。前端必须展示风险原因，且不得仅依赖按钮禁用实现防护。

### GR-027 变更 Diff 预览

当用户编辑已有通用资源时，系统应提供 YAML 变更预览，标记新增、删除和上下文行及行号；大文件应使用有界降级提示。Diff 必须可换行且只在自身容器内滚动，不能造成页面级横向溢出。

### GR-028 通用资源移动端与无障碍

当用户在 320px、390px、大字体或 Android 视口使用资源浏览器、列表、详情、编辑器、Schema 表单和删除确认框时，页面不得产生横向溢出；弹窗及后代控件不得超出弹窗或主内容区。表单字段必须有关联 label、可键盘操作、错误使用可读文本和 `aria-invalid`/`role=alert`，并进入 Chromium、Firefox 与 axe 验收矩阵。

### GR-029 节点排空

当用户具备 `update nodes` 权限时，系统应先生成非变更排空计划，列出每个 Pod 的 owner、PDB 关联、将驱逐/跳过/需确认原因和 warning；正式执行必须输入节点名称确认，并通过 Kubernetes Eviction API 逐个驱逐。PDB 阻止、裸 Pod、DaemonSet、mirror Pod 和已结束 Pod 必须被明确分类；PDB 拒绝或权限拒绝后停止后续驱逐，结果必须如实返回已驱逐、阻止、失败、跳过和未处理数量。

### GR-030 Pod 交互终端

当用户具备 `create pods/exec` 且容器存在时，系统应通过同源 WebSocket 将浏览器输入/输出桥接到 Kubernetes SPDY exec。Shell 仅允许白名单中的 `sh`、`bash`、`ash`；PTY 尺寸必须在 20–300 列、5–100 行内；单次输入和命令参数有界；打开和关闭分别生成审计。当用户具备 `update pods/ephemeralcontainers` 时，系统应允许为缺少 Shell 的容器创建服务端命名、Shell 白名单约束并经审计的 Ephemeral Container，并支持选择目标容器和指定调试镜像。终端必须执行每用户并发会话、空闲、最大时长和每秒输出限额；打开、拒绝、关闭、超时、限额和字节数必须写入审计。终端不承诺支持 `vim`、`top` 等依赖完整终端能力的全屏程序，移动端以命令输入模式优先。

### GR-031 Helm Release 可见性

当用户具备目标命名空间的 `list secrets` 权限时，系统应读取 Helm v3 Secret 存储，展示每个 Release 的最新 revision、Chart/App 版本、状态、说明、values、渲染 manifest、Notes 和历史 revision。Helm 存储解析失败必须以 warning 跳过，不得阻断其他 Release；values 中常见敏感键、manifest 中 Secret `data/stringData` 以及敏感名称对应的环境变量值必须在服务端遮蔽。

### GR-032 权限解释

当用户提交 verb、apiGroup、resource、subresource、namespace 和可选资源名称时，系统应以当前身份执行 SubjectAccessReview，返回 allowed、reason、guidance 和可复现的 `kubectl auth can-i` 命令，并区分 allowed、read_only、rbac_denied 与 authorization_unavailable；接口不得接受任意待检查身份。

### GR-033 本地资源工作区

当用户浏览、收藏、保存视图或编辑资源时，系统应支持按资源类型持久化收藏、最近访问对象、保存视图和隐藏列，并支持按 GVR 保存本地模板和最近草稿。收藏、最近资源、保存视图和隐藏列应本地即时生效，并在登录后与当前用户的服务端偏好合并及防抖同步；服务端偏好存放在控制面命名空间的按用户名哈希命名的 ConfigMap 中，客户端不得提交任意 ConfigMap 名称或路径。模板和草稿仍仅保存在本地。损坏的本地或服务端偏好必须被安全忽略，不得阻断页面。

### GR-034 日志与关联导航

当用户查看 Pod 时，系统应展示 Init Container、归属工作负载和节点深链；日志支持容器选择、Previous、tailLines、timestamps、时间范围和关键词过滤。服务端应将时间范围限制在最多七天，并对非流式日志实施有界读取。

### GR-035 回滚 Diff

当用户查看 Deployment 或 StatefulSet 发布历史时，系统应提供历史 Pod 模板快照，并在回滚确认框中展示当前模板与目标模板的只读 Diff；模板快照不参与写入，回滚仍只 Patch Pod 模板和变更原因。

### GR-036 新页面移动端验收

当用户在 320px、390px、大字体或 Android 视口访问 Helm、权限检查、资源偏好、终端、Ephemeral Container 弹窗、排空和回滚确认时，页面与弹窗不得横向溢出；新页面必须进入 Chromium、Firefox、明暗主题和 axe 验收矩阵。

### GR-037 工作负载级日志聚合

当用户查看 Namespaced 工作负载日志时，系统应解析该工作负载当前 Pod，并为每个 Pod/容器建立至多 20 条聚合日志流。每条流打开前必须独立执行 `get pods/log` SubjectAccessReview；单 Pod 拒绝、容器不存在、流打开失败或达到流数上限时，应保留其他可用流并返回可定位 Pod 和容器的 warning。全部流因权限被拒绝时返回 403，无 Pod 时返回 404，全部流因运行时原因无法打开时返回 503。聚合帧必须带 Pod 和容器来源；客户端断开后，服务端必须可取消发送并释放所有上游流。

### GR-038 Helm Revision Diff 与受控回滚

当用户具备 `list secrets` 权限时，系统应允许选择同一 Release 的两个不同 revision，并分别生成有界的 values 和渲染清单 unified diff；两侧内容必须先执行与 Release 详情一致的敏感值遮蔽，每侧最多 6000 行。回滚必须要求目标 revision 为正整数、确认字符串等于 Release 名称，并同时具备 `secrets/create` 与 `secrets/update`。系统应先读取目标 revision 清单，对所有非 Hook 对象执行 Server-Side Apply Dry Run；任一预检失败时不得写入。全部预检通过后才可逐个 live apply，写入 Helm 兼容的新 revision Secret，并将原当前 revision 标记为 superseded。Helm Hook 必须跳过，目标清单中不存在的旧对象不得自动删除，两类限制均须在结果中返回 warning；成功和失败均须审计。

### GR-039 资源关系与故障拓扑

当用户查看通用资源、工作负载、Pod、Service、Ingress、Node、PVC、PV 或 CronJob 详情时，系统应返回最多 40 条已授权的关系，覆盖 Owner、Node、PVC、匹配 Service、Ingress、PV/PVC 绑定、工作负载 Pod 和 CronJob Job 等高频故障定位路径。关系接口必须校验目标 GVR、命名空间与 `get` 权限；关系对象缺失或读取失败时返回 warning，不得阻断主详情。前端关系项必须支持跳转到对应资源详情，并在移动端换行显示、不得造成页面级横向溢出。

### GR-040 跨命名空间搜索与用户偏好

当用户执行全局资源搜索时，系统应默认使用 `namespace=all` 的受控跨命名空间模式，并遵守 GR-017 的扫描上限。系统应提供当前用户偏好的 `GET` 与 `PUT` 接口，只接受收藏、最近资源、保存视图和隐藏列的规范化数据，拒绝非法 URL、超长键值和未绑定 GVR 的视图；服务端使用控制面身份读写按用户哈希命名的 ConfigMap，不得信任客户端指定的资源名称。登录后前端应合并服务端与本地偏好并防抖保存；仅浏览页面不得产生空偏好写请求。

### GR-041 Pod 与 Service 临时端口转发

当用户具备目标 Pod 的 `create pods/portforward` 权限时，系统应在 Dosh 容器回环地址创建随机本地端口，并仅通过高熵会话路径代理 HTTP 请求。Service 转发必须先校验 `get services`，再解析到同一命名空间内 Ready 且 Running 的后端 Pod；命名端口缺失时继续尝试其他候选 Pod，最多扫描 100 个。会话必须有明确的过期时间和每用户并发上限，禁止浏览器指定上游地址；关闭、到期和拒绝均须审计。代理不得向目标应用转发 Dosh 会话 Cookie，也不得允许目标应用覆盖 Dosh Cookie，但应保留目标应用自身的 Cookie。

### GR-042 Helm 受控升级

当 Release 状态为 deployed 且用户同时具备目标命名空间 Secrets 的 `create` 与 `update` 权限时，系统应允许提交增量 Values。服务端必须使用当前 Release Secret 中保存的原始 Chart 与原始 Values 渲染新 revision，不得依赖浏览器回传的遮蔽值；预览和正式升级均须在结果中展示 values/manifest Diff 与 warning。正式升级必须再次校验 Release 名称确认，先对全部非 Hook 对象执行 Server-Side Apply Dry Run，任一预检失败时不得写入；全部通过后才可逐个应用并创建 Helm 兼容 revision。Helm Hook 不执行，目标清单中不存在的旧对象不删除，包含子 Chart 依赖的 Release 因 Secret 不保留依赖模板而必须明确拒绝升级。

### GR-043 搜索性能与取消

当相同用户以相同 API Groups、查询词、命名空间和结果上限在 10 秒内重复搜索时，系统应复用最近一次成功结果；缓存键必须隔离身份、授权范围和查询参数。达到结果上限后，系统应取消尚未完成的并发查询，返回已收集结果、截断标记和取消 warning，不得继续无界扫描。

### GR-044 关系拓扑筛选与有界读取

当读取资源关系时，系统应对每类 Kubernetes 查询使用 200 条上限，并优先使用 Label Selector 和 Field Selector 缩小范围；达到上限时必须返回截断 warning。前端应展示上游、下游、关联和异常关联摘要，允许按方向、资源类型和关系类型筛选，并按 Kind 分组；桌面使用多列卡片，移动端使用单列流式布局且不得产生页面级横向溢出。

### GR-045 常用资源推荐模板

当用户通过资源浏览器创建 StatefulSet、DaemonSet、Job、CronJob、PersistentVolume 或 StorageClass 时，系统应提供可编辑的推荐模板，并可随时恢复推荐内容；模板仍通过现有 Schema 动态表单和 YAML 双模式编辑，复杂字段或 Schema 不可用时必须可回退到 YAML。

### GR-046 多容器与 Init Container 创建

当用户创建 Deployment、StatefulSet、DaemonSet、Job 或 CronJob 时，系统应允许配置最多 32 个应用容器和 32 个 Init Container。每个容器应独立支持镜像与镜像仓关联、端口、CPU/内存、环境变量、Command/Args 和容器级 SecurityContext；容器名称在应用容器与 Init Container 之间必须全局唯一。未提供应用容器列表的旧客户端仍可使用首容器标量字段，服务端应将其规范化为单容器请求。

### GR-047 Pod 调度与安全上下文

当用户创建工作负载时，系统应支持 NodeSelector、Equal/Exists 容忍、required 节点亲和性、TopologySpreadConstraints、PriorityClass、ServiceAccount，以及 Pod 级 runAsUser、runAsGroup、fsGroup、runAsNonRoot、seccompProfile 和容器级 runAsUser、runAsGroup、runAsNonRoot、readOnlyRootFilesystem、allowPrivilegeEscalation、capabilities add/drop。所有字段必须在写 Kubernetes 前完成范围、枚举、标签键值和长度校验；节点亲和性 label 值必须保持大小写敏感，不得被前端统一转成大写。

### GR-048 Deployment 发布控制

当用户具备 `update deployments` 权限时，系统应展示 Deployment 的发布进度、当前策略、maxSurge/maxUnavailable、observedGeneration、更新/就绪/可用副本和 ReplicaSet 列表，并允许暂停、恢复发布及调整滚动策略。更新必须携带 `resourceVersion`，冲突返回 409；maxSurge 和 maxUnavailable 仅接受非负整数或 0%–100% 百分比，且不得同时为零。请求成功、失败或被拒绝必须写入审计。

### GR-049 Job/CronJob 运行参数与受控执行

当用户创建 Job 或 CronJob 时，系统应支持 completions、parallelism、backoffLimit、activeDeadlineSeconds 和 ttlSecondsAfterFinished。详情应展示活动、成功、失败数量，最近调度/成功时间、失败原因与消息，以及由该 CronJob 控制的有界近期 Job 列表。用户具备 `get cronjobs` 与 `create jobs` 权限时，可选择任一应用容器并临时覆盖 Command/Args 立即创建一次性 Job；不得修改 CronJob 调度或补跑历史任务。目标容器不存在、命令/参数越界或权限不足时，必须在 Kubernetes 写入前拒绝并审计。

## 2. 实现落点

| 层 | 文件 | 职责 |
|---|---|---|
| domain | `internal/domain/resources.go`、`internal/domain/preferences.go` | ResourceDescriptor、ResourceReference、资源关系、通用摘要与详情、用户偏好 DTO |
| app | `internal/modules/resources/app/service.go`、`risk.go`、`schema_validation.go`、`internal/modules/preferences/app/` | GVR 校验、SAR、通用读写、能力/风险、Schema 预检、Secret 脱敏、managedFields 清理、资源关系、授权搜索、用户偏好和审计 |
| http | `internal/modules/resources/http/handler.go` | Discovery、Schema、列表、详情、能力、创建/更新/删除路由与稳定错误映射 |
| kube | `internal/platform/kube/resources.go`、`resource_relations.go`、`resource_schema.go`、`preferences.go` | API Discovery、OpenAPI v2 读取与缓存、动态客户端读写、分页和搜索、关系拓扑、偏好 ConfigMap 存储 |
| nodes | `internal/modules/nodes/**`、`internal/platform/kube/node_drain.go` | 排空计划、PDB/DaemonSet/mirror/bare Pod 分类、Eviction API 执行和部分失败结果 |
| workloads | `internal/modules/workloads/**`、`internal/platform/kube/workloads.go`、`pod_exec.go`、`pods.go`、`pod_port_forward.go` | 发布历史/模板快照、回滚 Diff、日志时间范围、Init/Ephemeral Container、SPDY Exec、会话限额、Ephemeral Debug、Pod/Service 端口转发与审计详情 |
| releases | `internal/modules/releases/**`、`internal/platform/kube/helm_releases.go`、`helm_upgrade.go` | Helm v3 Secret 解码、最新 revision/历史、values/manifest 脱敏、revision Diff、Dry Run 回滚与升级、Hook 跳过和 Helm 兼容 revision 写入 |
| auth | `internal/modules/auth/**` | 当前身份 SubjectAccessReview 解释、reason/guidance 与 `kubectl auth can-i` 复现命令 |
| bootstrap | `internal/bootstrap/app.go`、`router.go` | 依赖装配和受保护路由挂载 |
| frontend | `web/src/features/resources/`、`workloads/`、`nodes/`、`helm/`、`access/`、`preferences/`、`web/src/lib/` | 通用资源、排空、终端、端口转发、聚合日志、Helm Diff/回滚/升级、关系拓扑筛选、权限检查、跨设备偏好、推荐模板与草稿 |
| contract | `api/openapi.yaml`、`web/src/api/schema.d.ts` | Discovery、Schema、通用读写、关系拓扑、用户偏好、排空、Exec、聚合日志、Helm Diff/回滚/升级、端口转发、权限检查和发布历史 REST 契约 |

## 3. 验证矩阵

- `go test ./internal/modules/resources/... ./internal/platform/kube/...`
  - Discovery 过滤子资源和仅有 `create` 的资源。
  - 核心 API 使用 `/api/v1`，扩展 API 使用 `/apis/{group}/{version}`。
  - 未知 GVR、非法引用、缺少 namespace、RBAC 拒绝、依赖失败和 Secret 脱敏/审计。
  - 通用列表的命名空间、搜索、分页和详情读取。
  - Table API 成功、priority 列过滤、Table API 回退和 cells 映射。
  - SelfSubjectRulesReview 过滤、规则缓存、失败关闭和集群级搜索范围。
  - Pod 日志 `pods/log` 授权、Previous 参数和审计。
  - 多应用容器/Init Container 创建、调度与安全上下文规范化、亲和性 label 值大小写保持、Deployment 发布控制、Job 参数和 CronJob 指定容器覆盖执行。
  - 工作负载多 Pod/多容器日志聚合、20 流上限、逐流拒绝、全部拒绝/无 Pod/不可用错误分类和取消释放。
  - 通用创建/更新 Dry Run、Secret 保护、update→patch 回退、能力查询和删除策略。
  - Deployment ReplicaSet 与 StatefulSet ControllerRevision 历史、回滚冲突和 change-cause 恢复。
  - 多 served version Discovery、preferred 标记、OpenAPI 定义匹配/引用裁剪/缓存/降级。
  - 高风险能力分级、名称确认和 HTTP 边界；Schema 必填/类型预检与 Schema 不可用降级。
  - 排空计划分类、PDB 检测、mirror/已结束 Pod 跳过、PDB 拒绝中止和部分结果计数。
  - 终端 shell/尺寸白名单、`pods/exec` 授权、会话并发/空闲/时长/输出限额、打开/关闭审计、Ephemeral Container 创建授权与 `sinceSeconds` 透传。
  - Helm gzip/base64 解码、最新 revision/历史、损坏 Secret warning、values/manifest/环境变量敏感值遮蔽、双 revision Diff、Dry Run 先于 live apply、Hook 跳过和 superseded revision。
  - Helm 升级原始 Chart/Values 合并、真实 Chart payload 渲染、子 Chart 拒绝、无基础 Values 合并和权限/确认边界。
  - Pod/Service 端口转换、Ready 后端筛选、命名 targetPort 回退、会话归属/TTL/并发上限、Cookie 隔离和任意 HTTP 方法代理。
  - Pod/Service/Ingress/Node/PVC/PV/Workload/CronJob 关系拓扑、查询上限、截断 warning、筛选计数和读取失败降级。
  - 全局搜索 `namespace=all` 上限、逐命名空间规则失败跳过、结果去重和 warning 聚合；用户偏好规范化、用户隔离、服务端 ConfigMap 存储和本地/服务端合并。
  - 资源搜索短 TTL 缓存的身份/授权/查询隔离、并发取消和达到上限 warning；常用工作负载推荐模板与恢复到 Schema 表单。
  - 权限解释 allowed/read_only/rbac_denied/unavailable、匿名身份提示和 HTTP 参数校验。
  - 本地收藏、保存视图、列偏好、最近资源、模板和草稿的持久化与损坏数据降级。
- `cd web && npm run api:check && npm run typecheck`
- `cd web && npm test -- --run`
- Playwright Chromium/Firefox：多版本切换、动态表单、Diff、风险确认、排空、终端、Ephemeral Container 创建与移动端弹窗、聚合日志、Helm Diff/回滚/升级、端口转发、关系拓扑筛选、跨命名空间搜索、偏好同步、权限检查、本地模板、390px/320px/大字体/Android 和弹窗后代边界。
- Playwright N05–N08：多容器与 Init Container 创建请求体、NodeSelector/容忍/亲和性/拓扑/PriorityClass/ServiceAccount/SecurityContext、Deployment 发布进度与发布控制、CronJob 批处理详情和指定容器 Command/Args 覆盖；在桌面 Chromium、Firefox 以及 320/360/390px 视口检查页面与弹窗横向溢出。
- axe：资源浏览器、Helm、权限检查、列表、详情在亮暗主题下无 serious/critical 问题。
- `make arch`

## 4. 后续阶段

1. 为 CRD Schema 增加字段级文档、更多组合类型提示和从已有对象一键另存模板。
2. 增加可配置日志后端和 Loki 等外部数据源，保留当前按需 Pod 流作为无外部依赖的降级路径。
3. 为端口转发增加可选的独立代理源、目标应用 Cookie 路径隔离和 WebSocket 真实应用验收。
4. 增强超大集群搜索索引、资源关系深度和节点/工作负载故障拓扑的分批排空策略。
