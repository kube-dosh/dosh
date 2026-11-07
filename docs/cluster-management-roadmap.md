# Dosh 集群配置与 YAML 创建能力方案

| 项目 | 内容 |
|---|---|
| 文档编号 | DOSH-EARS-002 |
| 适用版本 | v2.47 后续迭代 |
| 状态 | P0 已实现，P1/P2 待排期 |
| 关联文档 | `docs/ears-implementation-plan.md`、`api/openapi.yaml` |

## 1. 背景与目标

dosh 已具备节点、工作负载、Pod、网络、配置、存储、事件和审计等资源视图，但缺少两个高频入口：

1. 无法直接粘贴 Kubernetes YAML 创建或更新资源，必须依赖工作负载、Service、ConfigMap 等结构化表单。
2. 无法集中查看集群身份和网络地址规划，例如 Pod 网段、Service 网段、DNS 域、控制面地址和各节点 Pod CIDR。

本方案参照华为云 CCE 的信息层级，但沿用 dosh 自己的导航、卡片、权限和审计体系，不复制第三方面板代码或视觉资产。

## 2. 设计结论

### 2.1 YAML 创建

- 首版复用现有 `POST /api/v1/yaml/apply`，不新增任意 Kubernetes API 代理。
- 后端采用 Server-Side Apply，字段管理器固定为 `dosh`；页面文案明确“创建或应用”，不能宣称只创建。
- 后端每个请求只接受一个 YAML 文档，继续限制为现有白名单资源类型；前端可接收多个文档，在本地完成解析后逐个调用后端，因此多资源提交是顺序编排而不是原子事务。
- 页面提供 Deployment、Service、ConfigMap、Secret、PVC、Ingress 六类模板。
- 模板命名空间用于生成起始 YAML，也可通过“写入”操作批量覆盖所有命名空间级文档的 `metadata.namespace`；实际作用域始终以 YAML 内容为准。
- “Dry Run”只调用 API Server 校验，不落盘；正式应用仍由后端自动执行一次 Dry Run 后再写入。
- 写入前由后端检查目标资源是否存在：不存在时校验 `create`，已存在时允许 `update` 或 `patch`；存在性检查失败时 fail-closed，不触发写入。
- 创建成功展示返回的 kind、namespace、name，并在支持详情页的资源上提供跳转。
- Secret 返回值继续由服务端脱敏，前端不得尝试还原或缓存明文。

当前支持的资源类型：

| 作用域 | 资源 |
|---|---|
| 命名空间级 | Pod、Deployment、StatefulSet、DaemonSet、Job、CronJob、Service、Ingress、ConfigMap、Secret、PersistentVolumeClaim |
| 集群级 | StorageClass、PersistentVolume |

当前支持多个 YAML 文档顺序 Dry Run 和顺序应用，但不提供跨资源回滚。仍不支持 Namespace、Node 和 CRD。

### 2.2 集群配置

- 新增专用只读接口 `GET /api/v1/cluster/config`，不暴露任意 API 读取能力。
- 接口读取 Kubernetes ServerVersion、`kube-system/kubeadm-config`、节点 `spec.podCIDR(s)`、`default/kubernetes` 和 `kube-system/kube-dns` Service。
- 单个数据源失败时返回 warnings 和可用字段，不因非关键信息缺失让整个页面不可用。
- 页面展示集群名称、Kubernetes 版本、平台、CNI、控制面 Endpoint、DNS 域名、Pod/Service 网段、Kubernetes Service IP、DNS Service IP 和节点 Pod CIDR。
- 节点名称可跳转节点详情；移动端表格不得造成页面级横向溢出。

## 3. EARS 需求

### 3.1 YAML 创建

**YC-001 页面入口**

当已登录用户具有页面访问权限时，系统应提供 `/yaml/create` 入口，并在主导航显示“YAML 创建”。

验收：直接访问 `/yaml/create` 可渲染标题“YAML 创建”；主导航链接可到达该页面。

**YC-002 模板**

当用户打开 YAML 创建页时，系统应提供 Deployment、组合示例、Service、ConfigMap、Secret、PVC、Ingress 模板，并默认加载一个可提交的 Deployment 示例。

验收：切换模板后编辑器内容包含对应 `apiVersion`、`kind` 和 `metadata.namespace`；未修改编辑器时切换命名空间会重新生成模板。

**YC-003 后端单文档与白名单约束**

当单个后端 YAML 请求为空、超过 1 MiB、包含多个文档、不包含 `metadata.name`，或资源 API 版本、类型不在白名单内时，系统应拒绝提交并返回可理解的错误；前端必须把多文档清单拆成多个单文档请求。

验收：后端单元测试覆盖空文档、多文档、缺失名称和缺失命名空间；前端单元测试覆盖不支持类型和超限清单；前端不得自行绕过错误。

**YC-004 Dry Run**

当用户点击“Dry Run”时，系统应调用 API Server Dry Run，不持久化资源，并展示校验结果或错误。

验收：请求体 `dryRun=true`；API Server 成功时页面显示“Dry Run 校验通过”；失败时不展示成功状态。

**YC-005 创建或应用**

当用户点击“创建 / 应用”时，系统应先执行权限检查和一次内部 Dry Run，再使用 Server-Side Apply 创建或更新资源。

验收：请求成功时返回 kind、namespace、name；审计记录包含 apply 动作；已有同名资源时按 SSA 合并而不是隐式失败。

**YC-006 结果反馈**

当资源应用成功时，系统应展示资源身份，并在存在详情页或列表页时提供跳转入口。

验收：Deployment、Pod、Service、Ingress、PVC、PV、StorageClass、ConfigMap 和 Secret 至少提供一种有效跳转；无详情页的资源不得生成错误路径。

**YC-007 权限**

当当前身份没有 YAML Apply 权限时，系统应禁用 Dry Run 和创建按钮，并说明需要目标资源的 create 或 update / patch 权限。

验收：只读身份下按钮禁用；后端直接调用仍返回 403；前端不作为最终权限边界。

**YC-013 创建与更新分权**

当后端准备应用 YAML 时，系统应先检查目标资源是否存在；不存在时校验 `create`，已存在时校验 `update`，仅在 `update` 被拒绝且理由为无权限时回退校验 `patch`。

验收：新建资源只发起 `create` 授权检查；已有资源在无 `update` 权限但具备 `patch` 权限时可继续；存在性检查失败时不执行授权放行和写入，并记录失败审计。

**YC-014 批量命名空间写入安全**

当用户把多个文档的命名空间批量写入同一目标命名空间时，系统应重新校验结果；如果因此产生重复资源目标，应保留原始 YAML 并提示错误，不得静默生成重复清单。

验收：不同命名空间下的同名资源批量改写后触发重复目标错误；编辑器内容不变，用户可先修改资源名称或拆分清单。

**YC-008 命名空间语义**

当用户选择模板命名空间时，系统应只更新未被用户修改的模板内容，并明确实际命名空间以 YAML 为准。

验收：未编辑 YAML 时切换命名空间会更新 `metadata.namespace`；已编辑时不得静默覆盖用户内容。

**YC-009 失败语义**

当 YAML 解析、RBAC、Dry Run 或 apply 失败时，系统应保留编辑器内容并显示错误，不得展示成功结果。

验收：错误请求后仍可修改并重试；失败审计按现有模块规则记录。

**YC-010 移动端**

当页面宽度小于 1024 px 时，YAML 编辑器和操作区应纵向排列，页面不得出现水平滚动。

验收：Chromium 移动视口访问 `/yaml/create`，`documentElement.scrollWidth <= viewportWidth + 1`。

**YC-011 多文档顺序编排**

当用户输入多个 YAML 文档时，系统应先在浏览器端验证文档结构并按编辑顺序拆分，随后逐文档执行 RBAC、Dry Run 和 Server-Side Apply。

验收：三文档组合模板执行 Dry Run 时产生三个 `dryRun=true` 请求；正式应用时产生三个 `dryRun=false` 请求；任一文档失败后停止后续文档，页面展示逐资源结果和“非原子、不回滚”说明。

**YC-012 编辑工具**

当 YAML 内容非空且语法有效时，系统应支持格式化、复制、下载和本地文件导入，并展示有效文档数量、文档身份、行数和大小。

验收：格式化保留注释，复制和导出使用当前编辑器内容；导入文件限制为 1 MiB；解析错误会禁用验证与应用按钮。

### 3.2 集群配置

**CC-001 专用接口**

当已认证用户请求 `GET /api/v1/cluster/config` 时，系统应返回结构化集群配置，不提供任意 Kubernetes API 路径转发。

验收：OpenAPI 路径与 Schema 存在；未认证请求返回 401；接口不接收用户提供的资源路径。

**CC-002 身份信息**

当 Kubernetes Discovery 可用时，系统应返回 Kubernetes 版本和运行平台；不可用时返回 warning 并保留其他可用字段。

**CC-003 网络规划**

当 `kubeadm-config` 可读时，系统应返回控制面 Endpoint、Pod 网段、Service 网段和 DNS 域名。

**CC-004 Service 地址**

当 `default/kubernetes` 或 `kube-system/kube-dns` 可读时，系统应分别返回 Kubernetes Service IP 和 DNS Service IP。

**CC-005 节点 Pod CIDR**

当节点列表可读时，系统应按节点名称排序返回节点名、内网 IP 和全部 Pod CIDR；节点无 CIDR 时返回空列表而非省略节点。

**CC-006 部分降级**

当任一配置来源不可用时，系统应返回 `warnings`，并继续返回其余可用数据；只有全部关键数据源均不可用时才返回依赖不可用错误。

**CC-007 页面展示**

当用户访问 `/cluster/config` 时，系统应展示基本信息、网络配置和节点 Pod 网段，并在存在 warnings 时给出非阻断提示。

**CC-008 导航**

当用户在集群详情中操作时，应从主导航进入“集群配置”；节点名称应链接到对应节点详情。

**CC-009 移动端**

当页面宽度小于 1024 px 时，配置卡片和节点表格不得造成页面级水平溢出；CIDR、Endpoint 等长值应软换行。

**CC-010 可扩展性**

当后续增加 kube-proxy、kubelet、双栈或 NodePort 范围时，应扩展现有结构化响应字段，不得把原始 ConfigMap 整体暴露给前端。

## 4. 实施 WBS

| 编号 | 工作项 | 文件/位置 | 完成标准 |
|---|---|---|---|
| WBS-Y1 | domain DTO 与 OpenAPI | `internal/domain/types.go`、`api/openapi.yaml` | Go 与 TypeScript 类型一致，`npm run api:check` 通过 |
| WBS-Y2 | YAML 模板与编辑器页面 | `web/src/features/yaml/yaml-create-page.tsx` | 七类模板、多文档解析、Dry Run、顺序应用、编辑工具、错误与逐资源结果可用 |
| WBS-Y2.1 | YAML 文档解析工具 | `web/src/lib/yaml-documents.ts` | 文档身份、重复目标、命名空间回填、注释保留格式化均有单测 |
| WBS-Y3 | 路由与导航 | `web/src/router.tsx`、`web/src/components/layout/app-shell.tsx` | `/yaml/create` 和导航入口可用，无路由类型错误 |
| WBS-Y4 | 权限与安全交互 | `web/src/features/auth/use-session.ts`、YAML 页面 | 只读身份禁用操作；Secret 响应脱敏 |
| WBS-Y5 | YAML 前端测试 | `web/src/app.integration.test.tsx`、Playwright | Dry Run 和正式应用请求体、结果跳转、移动端溢出均有覆盖 |
| WBS-Y6 | YAML 写入分权与存在性检查 | `internal/modules/yaml/app`、`internal/platform/kube/yaml.go` | create/update/patch 分权、fail-closed 与审计均有单测 |
| WBS-C1 | 集群配置读取器 | `internal/platform/kube/cluster_config.go` | 读取版本、kubeadm 网络配置、节点 CIDR、Service IP |
| WBS-C2 | 模块与路由 | `internal/modules/clusterconfig/**`、`internal/bootstrap/**` | 专用只读路由完成接线，架构边界检查通过 |
| WBS-C3 | 集群配置页面 | `web/src/features/cluster/cluster-configuration-page.tsx` | 基本信息、网络配置、节点 CIDR、warnings 展示完整 |
| WBS-C4 | 前端 API 归一化 | `web/src/api/client.ts`、`web/src/api/types.ts` | 空数组可安全处理，不出现 null length 崩溃 |
| WBS-C5 | 集群配置测试 | Go 单测、Vitest、Playwright | 正常、部分降级、节点跳转、移动端布局均有覆盖 |

## 5. 验收矩阵

| 场景 | 预期 | 验证方式 |
|---|---|---|
| 创建合法 Deployment | API 返回 Deployment 身份，资源可查询 | Vitest + API Server Dry Run + Chromium E2E |
| 同名资源再次应用 | SSA 合并更新，不返回重复创建错误 | 后端单测 + 可选真实集群脚本 |
| YAML 语法错误 | 400/422，页面保留内容并提示错误 | Handler/Service 单测 + 前端错误测试 |
| 多文档 YAML | 前端拆分后逐文档提交，失败即停止并展示部分结果 | Vitest + Chromium E2E |
| 不支持资源 | 拒绝，不触碰 API Server | Service 单测 |
| 无权限 | 403，前端按钮禁用 | Handler 单测 + 前端能力测试 |
| 新建资源 | 仅要求 create，成功应用 | Service 单测 + Kube Exists 单测 |
| 更新已有资源 | update 或 patch 任一可用即可应用 | Service 单测 |
| 存在性检查失败 | fail-closed，不写入并记录失败审计 | Service 单测 |
| Secret 创建成功 | 响应 data/stringData 为 `<redacted>` | 后端单测 |
| kubeadm-config 缺失 | 返回 Node CIDR/Service IP 与 warnings | Go 单测 |
| 节点列表缺失 | 返回其他配置与 warnings | Go 单测 |
| 全部数据源不可用 | 返回依赖不可用错误 | Go 单测 |
| 移动端页面 | 无页面级水平溢出 | Chromium 移动视口 E2E |

建议命令：

```bash
go test ./internal/bootstrap ./internal/modules/clusterconfig/... ./internal/modules/yaml/... ./internal/platform/kube
go test ./...
cd web && npm run typecheck && npm test && npm run build
cd web && npm run api:check && npm run bundle:check
make arch
cd web && npm run e2e:chromium -- --grep "YAML|集群配置"
```

## 6. 分阶段路线

### P0：当前版本闭环（本次）

- `/yaml/create` 页面、六类模板、Dry Run、创建/应用和结果反馈。
- `GET /api/v1/cluster/config` 与 `/cluster/config` 页面。
- 导航、权限提示、部分降级、节点跳转和基础测试。

退出条件：Go 定向测试、前端类型检查、集成测试、构建和架构检查通过；移动端无页面级横向溢出。

### P1：商用易用性与权限正确性

- ✅ 区分 create 与 update 的 RBAC：后端根据资源是否存在分别校验 create/update/patch，避免“有 create 无 patch”的用户被错误拦截。
- 增加 namespace 覆盖或锁定机制，明确模板命名空间与最终对象命名空间的一致性校验。
- 增加资源 API Discovery 与 CRD 支持，前端根据发现结果提供模板和类型校验。
- 增加折叠、搜索结果导航、Schema 基础校验和编辑器全屏模式。
- 增加应用前 Diff 预览，突出新增、修改与删除字段；Secret 字段继续保持脱敏。
- 增加最近使用模板、资源历史和从已有资源“另存为 YAML”。
- 扩展集群配置字段：kube-proxy 模式、NodePort 范围、Pod/Service 双栈、CNI 配置来源和集群创建时间。

### P2：企业治理

- 服务端批量编排：把前端多文档循环下沉为一次受控任务，提供进度、取消、失败恢复和完整审计链；仍不宣称跨资源原子事务。
- 策略预检：接入 OPA/Kyverno 或企业准入策略，在 Dry Run 前给出风险提示。
- GitOps 导入/导出与仓库同步，支持 PR 审核、变更集和漂移检测。
- 集群配置变更建议与健康检查，不直接修改 Kubernetes 控制面配置。
- 大文件编辑器性能、服务端校验缓存、审计证据导出和大规模节点 CIDR 虚拟化。

## 7. 风险与约束

- Server-Side Apply 的语义是合并更新；同名资源存在时可能修改现有对象，页面必须持续展示风险提示。
- 存在性检查与真正写入之间仍存在极小竞态窗口；资源可能在此期间被其他客户端创建或删除，最终以 Server-Side Apply 和 API Server 校验结果为准。
- `kubeadm-config` 不是所有发行版都存在；非 kubeadm 集群必须允许按节点和 Service 信息降级展示。
- CNI 网段可能由 CNI 自身管理而不是 kubeadm；页面展示应标注配置来源，不能把推断值当作强一致配置。
- 多文档应用由前端顺序编排，无法保证跨资源原子性；失败时后续文档停止，已成功资源不会回滚。
