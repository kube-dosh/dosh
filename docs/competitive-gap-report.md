# Dosh 与主流 Kubernetes Dashboard 能力 Gap 报告

| 项目 | 内容 |
|---|---|
| 评估日期 | 2026-11-16 |
| 代码基线 | `main` 提交 `f08f789`，并包含当前工作区的 2026-11-16 功能实现 |
| 评估依据 | 当前仓库代码、OpenAPI、README、路线图，以及 Kubernetes Dashboard、Headlamp、Lens、KubeSphere、Rancher、Devtron 官方文档 |
| 说明 | 本文是能力方向对比，不代表逐个版本逐项核验；竞品功能会受版本、插件和商业版许可影响 |

## 1. 执行摘要

Dosh 当前不是“通用 Kubernetes Dashboard”，而是一个定位清晰的单集群、轻量、单二进制、无外部数据库/Agent 的日常运维控制台。它的优势不在功能数量，而在：

- 单二进制部署，前端通过 `go:embed` 内置。
- Kubernetes 原生认证、impersonation、SubjectAccessReview、CSRF、RBAC。
- 写操作审计、资源版本锁、Dry Run、Server-Side Apply。
- 单集群下的节点、工作负载、Pod、日志、网络、配置、存储和事件闭环。
- 移动端可用性明显优于多数桌面型或平台型产品。

但与 Headlamp、Lens 以及 KubeSphere、Rancher、Devtron 相比，Dosh 的主要差距可以归纳为五类：

1. 通用资源模型、多版本 API 选择、Schema 基础校验和高风险写入护栏已经落地，剩余主要是超大集群搜索与服务端索引。
2. Pod 终端、Ephemeral Debug 和节点排空已经落地；仍缺 Port-forward。
3. Deployment/StatefulSet 已具备 revision 历史、回滚 Diff 和回滚；Helm 已具备 HTTP/HTTPS 与 OCI 仓库、固定版本安装、更换 Chart 版本升级、增量 Values 升级、历史 Diff 和受控回滚。
4. 可观测性仍偏浅：只有 Metrics Server 瞬时值和按需 Pod 日志聚合，没有 Prometheus 历史指标、告警或外部日志后端。
5. 平台扩展能力缺失：没有多集群、插件机制、GitOps、多租户和集群生命周期管理。

最重要的判断：Dosh 不应直接追平 Rancher/KubeSphere 的平台能力，但必须补齐“通用 Dashboard 表必备能力”，否则会长期停留在固定资源 CRUD 工具，难以成为主流 K8s 控制台替代品。

## 2. Dosh 当前能力基线

### 2.1 已具备

| 领域 | 当前能力 |
|---|---|
| 部署 | 单二进制、Helm Chart、distroless 非 root 镜像、无外部数据库、无集群内 Agent |
| 认证 | Token、OIDC、本地管理员；HttpOnly 会话；CSRF |
| 权限 | Kubernetes RBAC、impersonation、SubjectAccessReview；当前身份 can-i 权限解释与拒绝原因 |
| 集群总览 | 版本、CNI、节点/Pod 健康度、工作负载数量、CPU/内存、Metrics 缺失时回退到 requests |
| 节点 | 状态、角色、容量、标签、污点、Pod、事件、日志、Cordon/Uncordon、PDB 感知排空计划与执行 |
| 工作负载 | Deployment、StatefulSet、DaemonSet、Job、CronJob 的列表、详情、实例、YAML |
| 工作负载操作 | Deployment 创建、扩缩容、滚动重启、更新镜像、容器配置、CronJob 暂停/恢复、删除、Deployment/StatefulSet revision 历史和回滚 |
| Pod | 状态、普通与 Init Container、资源、所有者、节点、事件、日志、YAML、受审计的交互式终端 |
| 日志 | 单 Pod 实时流与最多 20 条 Pod/容器流的工作负载级聚合；Previous 实例、Init Container、tailLines、timestamps、时间范围、关键词筛选和逐流 warning |
| Helm | Release 列表、values、rendered manifest、notes、warnings、revision history、跨 revision Diff 和受控回滚；敏感字段服务端遮蔽，回滚要求名称确认和 Secret 写权限 |
| 网络 | Service、Ingress、IngressClass 的查看、创建、编辑、删除 |
| 配置 | ConfigMap、Secret 的查看、创建、编辑、删除、数据查看、YAML |
| 存储 | PVC 创建、编辑、删除；PV、StorageClass 主要只读和关联导航 |
| 命名空间 | 创建、详情、YAML、受保护级联删除 |
| 事件 | 检索、命名空间过滤、Warning 识别、对象跳转、实时流；最多展示最近 500 条 |
| YAML | 模板、Dry Run、Server-Side Apply、多文档顺序提交、内置资源白名单；资源浏览器支持已发现资源的通用 YAML 创建、更新和删除 |
| 审计 | 最近审计记录、stdout、可选 HTTPS Webhook、HMAC |
| 证书 | HTTPS 证书元数据、热加载、手动重载 |
| 移动端 | 列表、搜索、弹窗、详情页响应式适配；新页面纳入 320px/390px、大字体、Android、亮暗主题和 axe 验收 |

相关边界在 `README.zh-CN.md` 已有明确说明：`/yaml/create` 暂不开放 Namespace 和 Node；CRD 等已发现资源通过资源浏览器写入；通用 Secret 写入关闭；PV、StorageClass 仍以查看和关联导航为主；Secret 专用页面不做额外二次遮蔽层。

### 2.2 架构上的固定扩展点

Dosh 当前的大量能力是“按资源硬编码”的：

- 路由按固定模块挂载，见 `internal/bootstrap/router.go:66`。
- 权限能力按固定资源枚举，见 `internal/modules/auth/app/session_service.go:58`。
- YAML 写入按固定 `apiVersion/kind` 白名单匹配，见 `internal/modules/yaml/app/service.go:111`。
- OpenAPI 的 `ResourceType` 是有限枚举，见 `api/openapi.yaml:2739`。
- 全局搜索只覆盖节点、工作负载和 Pod，见 `web/src/components/layout/global-search.tsx:16`。
- Metrics 只读取 Metrics API 的瞬时数据，见 `internal/platform/kube/metrics.go:13`。
- Pod 日志选项没有 `Previous`，见 `internal/platform/kube/pods.go:152`。

这不是当前实现的“错误”，但决定了 Dosh 很难通过小修小补支持 CRD、任意资源和 Operator 生态。

## 3. 竞品定位与对标结论

### 3.1 Kubernetes Dashboard

官方 README 已明确归档并停止维护，推荐迁移到 Headlamp。它仍有参考价值：

- 通用 Kubernetes 资源查看和管理。
- Deployment、Pod、Service、ConfigMap、Secret 等资源操作。
- CRD 资源和 CRD 对象查看。
- Pod 日志和 Shell。
- 基于 Kubernetes RBAC 的权限控制。

但不适合作为 Dosh 的长期主要对标对象。Dosh 更应该对标 Headlamp 和 Lens。

### 3.2 Headlamp

Headlamp 是最接近 Dosh 轻量定位的竞品，官方能力包括：

- 单集群和多集群支持。
- In-cluster Web UI 和桌面应用。
- 基于 Kubernetes API 的通用资源浏览。
- CRD 和自定义资源支持。
- 日志、终端、Ephemeral Container Debug。
- Port-forward。
- 资源编辑器及文档。
- 插件体系。
- RBAC 感知的 UI。
- Prometheus、Flux、cert-manager、Kyverno 等官方插件。

Headlamp 是 Dosh 最值得直接对标的“轻量通用 Dashboard”基准。

### 3.3 Lens

Lens 更偏“Kubernetes IDE”，官方文档明确覆盖：

- 多集群和 kubeconfig 管理。
- 日志查看。
- 终端。
- Port-forward。
- Helm Charts 和 Helm Releases。
- 集群指标。
- RBAC 资源查看。
- 扩展市场和桌面端。

Lens 的强项是开发者体验和本地工作流；Dosh 不必复制桌面端，但应补齐终端、Port-forward、Helm 和资源探索体验。

### 3.4 KubeSphere

KubeSphere 是平台型产品，核心能力包括：

- 多集群管理。
- Kubernetes 集群安装与生命周期。
- GitOps 与 DevOps。
- 可观测性：监控、事件、审计、日志、告警。
- Service Mesh。
- App Store 和 Helm 应用生命周期。
- 多租户、工作空间、配额。
- 边缘计算、GPU 工作负载等。

它的能力远超“Dashboard”，不适合作为 Dosh 的完整追赶目标。

### 3.5 Rancher

Rancher 的核心是集群管理和企业治理：

- 集群 Provisioning、导入和生命周期管理。
- 多集群统一认证和 RBAC。
- Helm 应用目录。
- Monitoring/Alerting。
- Logging。
- Backup/Restore。
- Fleet GitOps。
- CIS、Gatekeeper 等安全合规能力。

Rancher 适合作为“平台化能力地图”，但不建议 Dosh 直接进入集群 Provisioning 赛道。

### 3.6 Devtron

Devtron 更偏 Kubernetes 应用交付平台：

- Helm 应用管理。
- 多集群资源浏览。
- CRD 浏览。
- GitOps：Argo CD、Flux CD。
- CI/CD、部署编排、回滚。
- 安全扫描、策略、成本、可观测性。
- SSO 和细粒度 RBAC。

Devtron 的参考价值主要在 Helm、发布历史、回滚和应用生命周期，不是 Dosh 当前阶段应全部复制的方向。

## 4. 能力对标矩阵

状态说明：`✅` 原生支持；`◐` 部分支持、依赖插件或外部组件；`—` 非核心能力或未提供。

### 4.1 通用 Dashboard 能力

| 能力 | Dosh | K8s Dashboard | Headlamp | Lens | 判断 |
|---|---|---|---|---|---|
| 单集群资源浏览 | ✅ 固定视图 + 通用资源目录 | ✅ | ✅ | ✅ | 已具备通用模型 |
| CRD / API Discovery | ✅ CRD 类型与对象 | ✅ CRD 资源与对象 | ✅ | ✅ | 已落地，多版本待补 |
| YAML 编辑 / Schema | ✅ 通用 YAML/表单编辑、Schema 预检、字段提示和变更预览 | ◐ 基础编辑 | ✅ 编辑器和文档 | ✅ | 已支持 Schema 降级 |
| 资源创建/编辑/删除 | ◐ 已发现资源通用写入，缺少高风险策略分层 | ✅ | ✅ | ✅ | Secret 仅专用页面；高危资源护栏不足 |
| Pod 日志 | ✅ 单 Pod、Previous、Init Container、时间范围、关键词筛选和最多 20 条 Pod/容器流聚合 | ✅ | ✅ | ✅ | 外部日志后端和长期检索仍未接入 |
| Exec / Terminal | ✅ 受 RBAC、会话限额与审计保护的容器终端 | ✅ | ✅ | ✅ | 已覆盖常规容器排障 |
| Ephemeral Debug | ✅ 受控 Ephemeral Container、目标容器选择、无 Shell 镜像调试与审计 | — | ✅ | ◐ | 已完成首版闭环 |
| Port-forward | — | — | ✅ | ✅ | P0/P1 缺口 |
| 工作负载 revision / rollback | ✅ Deployment/StatefulSet 历史、Pod 模板 Diff 与回滚 | ◐ 可看部分历史对象 | ◐ | ◐ | 已具备回滚前预览和一键回滚 |
| Helm Release 管理 | ✅ HTTP/HTTPS/OCI Chart 仓库、安装、更换版本升级、增量 Values 升级、values/manifest 历史、Diff 和受控回滚 | — | ◐ 桌面插件 | ✅ | 主流管理基线已闭环；OCI 签名验证待补 |
| Metrics 历史 / 告警 | ◐ 仅瞬时 Metrics API | ◐ Metrics Server | ◐ Metrics + Prometheus 插件 | ◐ Metrics | 缺历史趋势和告警 |
| RBAC / Policy 资源视图 | ✅ 治理入口、can-i 解释和通用资源视图 | ◐ | ✅ | ✅ | 已覆盖高频治理资源 |
| 多集群 | — | — | ✅ | ✅ | P2，架构级变化 |
| 插件 / 扩展 | — | — | ✅ | ✅ | P2，取决于生态战略 |
| 桌面端 / 离线 | — | — | ✅ | ✅ | 不建议优先做 |
| 移动端 | ✅ | ◐ | ◐ | — | Dosh 的相对优势 |

### 4.2 平台型能力

| 能力 | KubeSphere | Rancher | Devtron | Dosh | 建议 |
|---|---|---|---|---|---|
| 多集群 Fleet | ✅ | ✅ | ✅ | — | P2，明确商业需求后再做 |
| 集群 Provisioning | ✅ | ✅ | ◐ | — | 不建议进入 |
| GitOps / CI/CD | ✅ | ✅ Fleet | ✅ Argo/Flux | — | P2 |
| 监控 / 日志 / 告警 | ✅ | ✅ | ✅ | ◐ | P1 补可观测性 |
| Helm / 应用生命周期 | ✅ | ✅ | ✅ | ✅ | 已具备仓库、安装、升级、卸载、历史、Diff 和回滚 |
| 多租户 / 工作空间 | ✅ | ✅ | ✅ | — | P2 |
| 策略 / 合规 / 安全扫描 | ✅ | ✅ | ✅ | ◐ | P1 做预检和集成 |
| 成本 / 备份 / Service Mesh | ◐ | ✅ | ✅ | — | 合作/集成，不建议自研 |

## 5. Gap 清单

工作量估算前提：1 名熟悉 Go、React、Kubernetes 的工程师，包含开发、测试、文档和安全验证；不包含外部 Prometheus、Loki、IdP 等环境准备。仅作为量级参考。

### 5.1 P0：通用 Dashboard 必备能力

| ID | Gap | Dosh 当前状态 | 主流基线 | 业务影响 | 建议补齐 | 粗估 |
|---|---|---|---|---|---|---|
| GAP-01 | 通用资源浏览器与 API Discovery | ✅ 已实现全 served version Discovery、preferred 标记、通用列表/详情、Table API、搜索、版本切换、治理入口和跨资源关系拓扑 | Headlamp、Lens、Devtron、K8s Dashboard 均支持较通用的资源浏览 | 已解锁 Operator、CRD、HPA、PDB、NetworkPolicy、RBAC 等资源 | 后续补超大集群索引和更深的关系图 | 已完成主体 |
| GAP-02 | CRD 生命周期与动态 YAML | ✅ 通用 YAML 创建/编辑/删除、Dry Run、SSA、SAR、审计、OpenAPI Schema 预检、动态表单和字段级提示已实现 | Headlamp、Lens、Devtron 支持 CRD 和自定义资源编辑 | 已可通过 UI 管理复杂 CRD，Schema 不可用时仍有 YAML 降级 | 后续补 CRD 模板库、字段级文档和从已有对象另存为模板 | 已完成主体 |
| GAP-03 | Pod Exec / Terminal / Debug | ✅ 已实现受 RBAC、同源 Cookie 会话、Shell 白名单、PTY 尺寸约束、会话限额和审计保护的终端；支持受控 Ephemeral Container 与目标容器选择 | Headlamp、Lens、K8s Dashboard | 常规容器和无 Shell 容器均可在控制台完成主要排障 | 后续评估文件传输、共享终端和更细粒度的调试镜像策略 | 已完成首版 |
| GAP-04 | Port-forward | ✅ 已实现回环、限时、自动回收的 Pod/Service 临时 HTTP 隧道 | Headlamp、Lens | 常规调试端口可在控制台临时访问 | 后续评估 TCP 多端口与更细会话配额 | 已完成主体 |
| GAP-05 | Helm Release 管理 | ✅ 已实现 HTTP/HTTPS 与 OCI 仓库、Chart 目录与详情、values.schema.json 预检、固定摘要安装、Chart 目录更换版本升级、Release 增量 Values 升级、卸载、双 revision Diff 和受控回滚 | Lens、KubeSphere、Rancher、Devtron | 主流 Helm 管理生命周期可在 Dosh 内闭环 | 后续补 OCI 签名/证明验证与 Release 详情直接切换 Chart 版本 | 已完成基线 |
| GAP-06 | 工作负载 revision 与回滚 | ✅ Deployment/StatefulSet revision 历史、Pod 模板快照、回滚前 Diff 和定向回滚已实现 | 平台型产品普遍有发布历史 | 已提供可追踪、可预览的发布恢复路径 | 后续补更细的 rollout 状态和仅 Patch 指定字段的恢复策略 | 已完成主体 |
| GAP-07 | 结构化工作负载与存储表单 | ✅ Deployment、StatefulSet、DaemonSet、Job、CronJob、PV、StorageClass、PVC 等结构化表单与 YAML 回退已实现 | Headlamp 通用编辑；平台产品有向导 | 常规工作负载和存储对象不再强制依赖 YAML | 后续补更细字段联动与模板复用 | 已完成主体 |
| GAP-08 | 节点排空与故障拓扑 | ✅ 已实现 PDB 感知排空，以及 Pod/工作负载/Service/Ingress/Node/PVC/PV/CronJob 高频关系拓扑 | Lens/Headlamp 有更完整资源关系和排障入口 | 常规维护和故障对象跳转已有闭环；超大关系图与分批策略仍可增强 | 后续补关系图可视化、跨节点影响面和更细分批排空 | 已完成主体 |
| GAP-09 | RBAC、网络策略、弹性资源视图 | ✅ 治理入口覆盖 ServiceAccount、Role、Binding、NetworkPolicy、HPA、PDB、Quota、LimitRange、PriorityClass；已支持当前身份 can-i 解释 | Headlamp、Lens、平台产品 | 已可查看高频治理对象、关键关系并解释访问拒绝 | 后续补 VPA 和更深的关联导航 | 已完成主体 |

按当前实现重新估算，P0 主体已闭环；剩余主要是超大集群索引、更深关系图、更细分批排空、Release 并发操作锁和 OCI 供应链证明验证，约 4–8 人周。该区间仍只用于量级判断，实际排期需要结合安全评审和真实集群验证。

### 5.2 P1：竞争力与可观测性

| ID | Gap | Dosh 当前状态 | 主流基线 | 业务影响 | 建议补齐 | 粗估 |
|---|---|---|---|---|---|---|
| GAP-10 | 历史指标与告警 | 只有 Metrics API 瞬时 CPU/内存 | Lens metrics、Headlamp Prometheus 插件、KubeSphere/Rancher/Devtron 监控 | 无法看趋势、容量规划和告警 | Prometheus/VictoriaMetrics 适配器、节点/Pod/工作负载图表、告警状态；不可用时无障碍降级 | 4–6 人周 |
| GAP-11 | 日志聚合、Previous、检索 | ✅ 已实现单 Pod 实时流和最多 20 条 Pod/容器流的工作负载级聚合，含 Previous、时间范围、关键词筛选和逐流 warning | Lens、Headlamp、平台产品更强 | 常规跨 Pod 排障已不再需要打开多个日志窗口 | 补 Loki 等外部后端、长期检索和可配置保留策略 | 已完成按需聚合 |
| GAP-12 | 全局搜索、保存视图、Diff | ✅ 已实现受授权跨命名空间搜索、保存视图、收藏、隐藏列、最近资源、跨设备偏好、本地模板和 YAML Diff | Headlamp/Lens 通用搜索；平台产品有保存视图 | 日常资源定位和编辑效率已明显提升 | 补超大集群服务端索引和更长的资源历史 | 已完成主体 |
| GAP-13 | 策略预检、镜像漏洞和健康评分 | 无 | Devtron、Rancher、KubeSphere，Headlamp 可通过 Kyverno/Trivy 插件 | 高风险配置和漏洞只能在集群外发现 | Kyverno/OPA Dry Run 预检、Trivy 镜像扫描、工作负载健康评分、风险提示 | 4–6 人周 |
| GAP-14 | 审计运营化 | 最近记录 + stdout/Webhook | 企业平台有集中审计、保留、检索和告警 | 当前更适合技术审计，不足以直接满足企业 SIEM 运营 | 明确保留策略、SIEM 投递、检索、告警、访问审批和证据导出 | 3–5 人周 + 外部运营 |

### 5.3 P2：平台化能力

| ID | Gap | 建议 | 说明 |
|---|---|---|---|
| GAP-15 | 多集群 | 谨慎进入 | 需要每集群凭据、会话隔离、RBAC 聚合、审计关联和 UI 导航重构，不能只加一个集群选择器 |
| GAP-16 | 多租户与工作空间 | 谨慎进入 | 需要项目/工作空间、配额、租户边界和平台侧授权模型，超出 Kubernetes 原生 RBAC 范围 |
| GAP-17 | GitOps / CI/CD | 不建议当前阶段自研 | 可通过集成 Argo CD、Flux、Fleet 提供状态视图，不建议重建发布平台 |
| GAP-18 | 插件机制 | 明确生态战略后再做 | Headlamp/Lens 的插件体系是长期工程；没有第三方生态时投入产出比低 |
| GAP-19 | 桌面端 / 离线 | 非优先 | Dosh 的移动端和单二进制已经有差异化，不必复制 Lens 桌面 IDE |
| GAP-20 | 集群 Provisioning、Service Mesh、App Store、成本、备份 | 不建议盲目补齐 | 应通过集成或合作伙伴解决，不应把 Dosh 变成 Rancher/KubeSphere 的缩小版 |

### 5.4 不建议直接补齐的能力

- 完整集群 Provisioning 和节点生命周期管理。
- 完整 CI/CD 与 DevOps 平台。
- 重型 Service Mesh 管理。
- 通用插件运行时，除非已经明确第三方插件生态战略。
- 大而全的成本分析、备份恢复、GPU/边缘平台。
- 为了“功能看起来像 Rancher”而增加多租户和项目模型。

Dosh 的核心竞争力应该是轻量、安全、可审计、移动端可用、安装简单，而不是功能数量。

## 6. 建议的实施顺序

### 6.1 阶段 A：资源模型基础，已完成主体

- ✅ GAP-01：API Discovery 和通用资源模型。
- ✅ GAP-09：RBAC、NetworkPolicy、HPA、PDB、Quota 等高频资源和 can-i 解释。
- ✅ GAP-11 的小范围：`previous` 日志、Init Container、时间范围和关键词筛选。

退出条件：用户能够根据授权浏览任意已发现资源，并能通过统一资源详情页查看 YAML 和事件。

### 6.2 阶段 B：排障闭环，已完成主体

- ✅ GAP-03：Pod Exec / Terminal 与 Ephemeral Container Debug。
- GAP-04：Port-forward，建议先管理员和受限命名空间可用。
- ✅ GAP-08：节点 drain/evict 和 PDB 提示（故障拓扑待补）。
- ✅ GAP-06：Deployment/StatefulSet revision 和回滚。

退出条件：常见“Pod 起不来、节点维护、发布失败”三类问题可以在 Dosh 内完成主要诊断和恢复。

### 6.3 阶段 C：发布与编辑能力，已完成主体

- ✅ GAP-05：Helm 仓库、安装、更换 Chart 版本升级、增量 Values 升级、历史 Diff、卸载和受控回滚已完成。
- ✅ GAP-07：StatefulSet、DaemonSet、Job、CronJob 与存储对象结构化表单已完成主体。
- ✅ GAP-02：CRD 动态 YAML 和 Schema 校验（字段级文档待补）。

退出条件：不需要频繁跳出 Dosh 去执行 Helm 或 kubectl apply。

### 6.4 阶段 D：可观测性和治理，部分完成

- GAP-10：Prometheus/VictoriaMetrics 历史指标。
- GAP-11：跨 Pod 日志聚合和服务端后端。
- ◐ GAP-12：已授权通用资源搜索、保存视图、收藏、隐藏列、本地模板和 Diff 已完成；服务端全量索引待补。
- GAP-13：策略预检和漏洞扫描。

### 6.5 阶段 E：商业决策门禁

在完成 A–D 后，再根据真实客户需求决定是否进入：

- 多集群。
- 多租户。
- GitOps/CI/CD。
- 插件生态。
- 桌面端。

## 7. EARS 风格验收建议

### 7.1 GAP-01：通用资源发现

- 当 API Discovery 可用时，系统应展示当前身份有权访问的资源类型，并允许按 API Group、资源和命名空间筛选。
- 当 API Discovery 部分失败时，系统应保留内置资源视图，并显示明确的降级提示，而不是阻断整个控制台。
- 当用户没有某资源的 `list` 权限时，系统不应展示该资源对象内容，也不应通过搜索接口泄露对象名称。

### 7.2 GAP-02：CRD 与自定义资源

- 当用户打开 CRD 实例列表时，系统应使用 API Server 提供的 printer columns 展示主要字段。
- 当用户创建或编辑 CRD 实例时，系统应执行 Server-Side Apply、Dry Run 和资源级 RBAC 校验，并记录审计。
- 当 CRD Schema 不可用时，系统应允许进入 YAML 模式，但必须提示校验能力受限。

### 7.3 GAP-03：Pod 终端

- 当用户打开 Pod 详情且具备 `pods/exec` 权限时，系统应允许选择容器并建立终端会话。
- 当用户不具备 Exec 权限时，系统应返回 403，并记录失败审计，而不是仅隐藏按钮。
- 当终端长时间无输入或连接异常时，系统应自动关闭会话并提示重新连接。

### 7.4 GAP-04：Port-forward

- 当用户发起端口转发时，系统应校验目标 Pod/Service、端口和 RBAC 权限，并记录启动、停止和失败审计。
- 当目标端口不可达或会话超时时，系统应关闭转发并给出可理解的状态，不应静默保持连接。

### 7.5 GAP-06：工作负载历史

- 当用户查看 Deployment 或 StatefulSet 时，系统应展示 revision、镜像、副本数、创建时间和当前状态。
- 当用户具备更新权限时，系统应允许回滚到指定 revision，并要求确认和记录审计。
- 当工作负载没有历史记录时，系统应展示空状态，而不是报错阻断详情页。

### 7.6 GAP-05：Helm Release

- 当用户浏览 Helm Release 时，系统应展示 release 名称、命名空间、chart、version、status 和 revision。
- 当用户查看 values 或 manifest 时，系统应遮蔽 Secret 值，并按 RBAC 控制敏感字段访问。
- 当用户执行升级或回滚时，系统应显示目标版本、差异、影响范围，并记录审计。

### 7.7 GAP-10：指标降级

- 当 Prometheus/VictoriaMetrics 不可用时，系统应显示降级提示并保留资源管理和日志功能，不应阻断页面。
- 当 Metrics Server 不可用时，系统应回退到 requests/allocatable 数据，并明确标注数据来源。

### 7.8 GAP-11：日志

- 当容器发生重启时，系统应允许查看 previous container logs。
- 当用户按时间、容器或关键词筛选日志时，系统应只返回符合条件的内容，并限制最大返回量。
- 当日志后端不可用时，系统应保留单 Pod 原生日志查看能力。

### 7.9 GAP-12：搜索

- 当用户输入搜索词时，系统应在当前身份可访问的已索引资源中搜索，而不是只搜索节点、工作负载和 Pod。
- 当搜索请求超过资源限制时，系统应分页或提示缩小范围，不应导致 API Server 过载。

## 8. 发布与运营阻断项

以下内容不是产品功能 Gap，但如果目标是商业化或生产发布，需要单独列为门禁：

- 生产 SIEM 投递、租户隔离、日志保留、告警和访问审批。
- 真实 Edge 与 macOS/iOS Safari 验收。
- GitHub Actions 全量绿色和 `main` 分支保护。
- 离机 WORM 证据留存。
- 生产变更授权、维护窗口、回滚责任人和回滚演练。
- 生产 Secret 读取策略和二次遮蔽方案的产品决策。

这些门禁的当前规划已在 `docs/commercialization-todo.md` 中部分覆盖，不能与“功能补齐”混为一谈。

## 9. 最终结论

Dosh 当前已经具备一套不错的安全边界和单集群日常运维闭环，优势是：

- 单二进制部署。
- Kubernetes 原生权限。
- 审计和写操作保护。
- 移动端可用。
- 不依赖 Agent 和外部数据库。

但与主流通用 Dashboard 相比，最大差距不是“再多几个页面”，而是：

1. 通用资源模型与 CRD。
2. 终端、Port-forward 和交互式排障。
3. Helm Release、发布历史和回滚。
4. 历史指标、日志聚合和告警。
5. RBAC、网络策略、弹性资源等治理对象视图。

建议路线是：

- 先补 P0 通用 Dashboard 能力，确保 Dosh 能作为 Headlamp/Lens 的轻量替代品。
- 再补 P1 可观测性和治理能力，形成差异化竞争力。
- P2 多集群、多租户、GitOps、插件生态按商业需求决策，不要为了追平 Rancher/KubeSphere 而破坏单二进制和轻量定位。

如果只选择一个最重要的优先级，建议先做 GAP-01 通用资源发现与 CRD 支持。它会同时解锁 RBAC、NetworkPolicy、HPA、PDB、Operator 和大量第三方资源视图，是后续能力建设的底座。

## 10. 实施进展

截至 2026-11-21，阶段 A 的通用资源只读与受控写入、阶段 B 的主要排障路径，以及 Helm 管理基线已落地：

- 新增 API Discovery 类型目录、统一 GVR 校验和通用列表/详情。
- 通用读取执行 `list`/`get` SubjectAccessReview，并支持命名空间、搜索、cursor 分页和截断提示。
- 核心 Secret 在通用对象和 YAML 中遮蔽 `data`/`stringData`，并记录成功与失败审计。
- 前端新增资源浏览器，可从类型目录进入任意已发现资源的列表、详情、事件和 YAML，并按能力查询结果提供创建、编辑和删除入口。
- 通用创建/更新支持 API Server Dry Run、`create`/`update`/`patch` 分权、SSA 回退和审计；通用 Secret 创建/更新继续关闭，删除仍需资源级 RBAC。
- Pod、Service、ConfigMap、Secret 等固定页面继续保留，承担高价值关联、排障和受控写操作；资源浏览器提供通用读写兜底。
- 通用列表优先使用 Kubernetes Table API 展示类型专属列，并在不支持 Table 响应时回退到基础元数据列。
- 携带 namespace 的 Discovery 使用 `SelfSubjectRulesReview` 缓存过滤当前身份可 list 的资源类型；失败时按失败关闭策略隐藏对应作用域。
- 全局搜索接入通用资源，但限制为当前身份可 list 的已授权类型、最多 16 个类型，并要求 Namespaced 资源显式选择命名空间。
- 新增“治理与策略”入口，覆盖 RBAC、NetworkPolicy、HPA、PDB、配额和 PriorityClass，并解释关键资源关系。
- Pod 日志新增 Previous 实例、Init Container、时间范围、关键词筛选，并补齐 `get pods/log` SubjectAccessReview 与 `stream-pod-logs` 审计。
- 工作负载详情新增发布历史页签，Deployment 使用 ReplicaSet、StatefulSet 使用 ControllerRevision 展示 revision、镜像、变更原因和当前标记；回滚前展示 Pod 模板 Diff。
- Deployment/StatefulSet 支持通过定向 Merge Patch 回滚，只恢复 Pod 模板与变更原因，保留副本数和其他并发字段，并记录回滚审计；避免状态控制器持续更新导致整体对象 `resourceVersion` 冲突。
- 节点新增 PDB 感知的排空计划和 Eviction 执行；PDB、Forbidden 或不可驱逐 Pod 会中止后续驱逐，并保留部分结果。
- Pod 详情新增受 `pods/exec`、CSRF 会话、Shell 白名单、PTY 尺寸约束和审计保护的终端，通过同源 WebSocket 桥接 Kubernetes SPDY Exec。
- Helm 已支持 HTTP/HTTPS `index.yaml` 与 OCI Chart 仓库、Chart 目录/详情、values.schema.json 预检、固定摘要安装、Chart 目录更换版本升级、Release 增量 Values 升级、卸载、revision 历史、Diff 和受控回滚；Release Secret 中的敏感键、Secret payload 与敏感环境变量在服务端遮蔽。
- 新增权限检查页面，以当前身份执行 SubjectAccessReview，展示允许/拒绝原因、只读模式提示和可复现的 `kubectl auth can-i` 命令。
- 资源浏览器新增收藏、仅看收藏、保存视图、隐藏列、最近资源、本地模板、草稿恢复、全屏编辑和 Schema 字段搜索；偏好仅保存在浏览器本地。

仍属于后续阶段：跨 Pod 长期日志后端、服务端全资源搜索索引、CRD 模板库和字段级文档、超大关系图、OCI 签名/证明验证与 Release 并发操作锁。
