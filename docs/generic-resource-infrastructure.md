# 通用 Kubernetes 资源接入基础设施

| 项目 | 内容 |
|---|---|
| 状态 | 第二阶段已实现：Discovery、类型感知列表、按身份过滤、通用只读列表/详情、RBAC、Secret 脱敏与审计、前端资源浏览器与治理入口 |
| 目标 | 从固定资源控制台走向通用 Kubernetes 控制台，为 CRD、Operator 资源和后续动态写入提供统一底座 |
| 边界 | 本阶段不开放任意 API 代理、CRD 写入、动态表单或客户端指定上游 URL |

## 1. EARS 任务清单

### GR-001 资源发现

当 Kubernetes API Discovery 可用时，系统应返回核心 API 和每个 API Group 的 preferred version 中支持 `list` 或 `get` 的资源类型，并包含 Group、Version、Resource、Kind、作用域、动词、短名称、类别和单数名称。

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

当用户选择资源类型时，系统应展示该类型的对象列表、命名空间筛选、分页、详情、事件和只读 YAML；Namespaced 资源缺少 namespace 时应给出可恢复提示。

### GR-012 固定视图保留

系统应保留 Pod、Service、ConfigMap、Secret、存储、节点、工作负载等现有固定页面。它们继续提供经过编排的写操作、关联关系和排障入口；资源浏览器提供通用只读兜底，不替代这些高价值工作流。

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

## 2. 实现落点

| 层 | 文件 | 职责 |
|---|---|---|
| domain | `internal/domain/resources.go` | ResourceDescriptor、ResourceReference、通用摘要和详情 DTO |
| app | `internal/modules/resources/app/service.go` | GVR 校验、SAR、Secret 脱敏、managedFields 清理和审计 |
| http | `internal/modules/resources/http/handler.go` | Discovery、列表、详情路由与稳定错误映射 |
| kube | `internal/platform/kube/resources.go` | API Discovery、动态客户端读取、缓存、分页和搜索 |
| bootstrap | `internal/bootstrap/app.go`、`router.go` | 依赖装配和受保护路由挂载 |
| frontend | `web/src/features/resources/`、`web/src/features/governance/` | 类型目录、类型列、对象列表、详情、事件、只读 YAML 和治理入口 |
| contract | `api/openapi.yaml`、`web/src/api/schema.d.ts` | Discovery 与通用读取 REST 契约 |

## 3. 验证矩阵

- `go test ./internal/modules/resources/... ./internal/platform/kube/...`
  - Discovery 过滤子资源和仅有 `create` 的资源。
  - 核心 API 使用 `/api/v1`，扩展 API 使用 `/apis/{group}/{version}`。
  - 未知 GVR、非法引用、缺少 namespace、RBAC 拒绝、依赖失败和 Secret 脱敏/审计。
  - 通用列表的命名空间、搜索、分页和详情读取。
  - Table API 成功、priority 列过滤、Table API 回退和 cells 映射。
  - SelfSubjectRulesReview 过滤、规则缓存、失败关闭和集群级搜索范围。
  - Pod 日志 `pods/log` 授权、Previous 参数和审计。
- `cd web && npm run api:check && npm run typecheck`
- `cd web && npm test -- --run`
- `make arch`

## 4. 后续阶段

1. 基于 OpenAPI Schema 的 CRD 创建/编辑、Dry Run、Server-Side Apply 和删除审计。
2. 将 CRD 资源接入事件对象跳转、保存视图和按资源类型收藏。
3. 为 Discovery 过滤增加更细的 watch/create/update/delete 能力标记。
4. 增加跨 Pod 日志聚合和可配置日志后端，不在浏览器端做无界全量扫描。
