# 通用 Kubernetes 资源接入基础设施

| 项目 | 内容 |
|---|---|
| 状态 | 第四阶段已实现：全 served version Discovery、OpenAPI Schema 预检与动态表单、高风险写入确认、变更 Diff、移动端与无障碍专项；保留通用列表/详情、受控 YAML 写入、发布历史与回滚、Secret 脱敏与审计 |
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

当用户从全局搜索输入至少两个字符时，系统应在当前身份可 list 的已发现资源类型中执行有界搜索。Namespaced 资源必须显式选择命名空间；未选择命名空间时只搜索集群级资源。单次搜索最多覆盖 16 个资源类型，并返回截断与单类型失败 warning。

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

## 2. 实现落点

| 层 | 文件 | 职责 |
|---|---|---|
| domain | `internal/domain/resources.go` | ResourceDescriptor、ResourceReference、通用摘要和详情 DTO |
| app | `internal/modules/resources/app/service.go`、`risk.go`、`schema_validation.go` | GVR 校验、SAR、通用读写、能力/风险、Schema 预检、Secret 脱敏、managedFields 清理和审计 |
| http | `internal/modules/resources/http/handler.go` | Discovery、Schema、列表、详情、能力、创建/更新/删除路由与稳定错误映射 |
| kube | `internal/platform/kube/resources.go`、`resource_schema.go` | API Discovery、OpenAPI v2 读取与缓存、动态客户端读写、分页和搜索 |
| workloads | `internal/modules/workloads/**`、`internal/platform/kube/workloads.go` | 发布历史读取、revision 解析、回滚和冲突映射 |
| bootstrap | `internal/bootstrap/app.go`、`router.go` | 依赖装配和受保护路由挂载 |
| frontend | `web/src/features/resources/`、`web/src/features/workloads/`、`web/src/features/governance/` | 多版本类型目录、对象列表/详情、Schema 表单、YAML/Diff 编辑器、风险确认、发布历史、回滚和治理入口 |
| contract | `api/openapi.yaml`、`web/src/api/schema.d.ts` | Discovery、Schema、通用读写、能力/风险查询、发布历史与回滚 REST 契约 |

## 3. 验证矩阵

- `go test ./internal/modules/resources/... ./internal/platform/kube/...`
  - Discovery 过滤子资源和仅有 `create` 的资源。
  - 核心 API 使用 `/api/v1`，扩展 API 使用 `/apis/{group}/{version}`。
  - 未知 GVR、非法引用、缺少 namespace、RBAC 拒绝、依赖失败和 Secret 脱敏/审计。
  - 通用列表的命名空间、搜索、分页和详情读取。
  - Table API 成功、priority 列过滤、Table API 回退和 cells 映射。
  - SelfSubjectRulesReview 过滤、规则缓存、失败关闭和集群级搜索范围。
  - Pod 日志 `pods/log` 授权、Previous 参数和审计。
  - 通用创建/更新 Dry Run、Secret 保护、update→patch 回退、能力查询和删除策略。
  - Deployment ReplicaSet 与 StatefulSet ControllerRevision 历史、回滚冲突和 change-cause 恢复。
  - 多 served version Discovery、preferred 标记、OpenAPI 定义匹配/引用裁剪/缓存/降级。
  - 高风险能力分级、名称确认和 HTTP 边界；Schema 必填/类型预检与 Schema 不可用降级。
- `cd web && npm run api:check && npm run typecheck`
- `cd web && npm test -- --run`
- Playwright Chromium/Firefox：多版本切换、动态表单、Diff、风险确认、390px/320px/大字体/Android 和弹窗后代边界。
- axe：资源浏览器、列表、详情在亮暗主题下无 serious/critical 问题。
- `make arch`

## 4. 后续阶段

1. 提供 CRD 模板库、字段搜索、Schema 字段级文档和从已有对象“另存为模板”。
2. 将回滚改为仅 Patch Pod 模板，并增加回滚前 Diff。
3. 将 CRD 资源接入事件对象跳转、保存视图和按资源类型收藏。
4. 增加跨 Pod 日志聚合和可配置日志后端，不在浏览器端做无界全量扫描。
