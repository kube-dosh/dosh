# 网络路由、策略与连通性诊断

## 目标

Dosh 的网络页面从 Service/Ingress 控制台扩展为 Kubernetes 通用网络接入面：动态发现 Gateway API，解释 NetworkPolicy，并将 Pod → Service 的 DNS、端口、EndpointSlice 与策略路径组合成一次只读静态诊断。

本轮不引入新的 Kubernetes 控制器，不代理任意 Kubernetes API，不发送真实 TCP/UDP 探测包，也不自研 Service Mesh 控制面。

## 能力范围

### Gateway API

- 通过 API Discovery 动态发现 `gateway.networking.k8s.io` 下的 GatewayClass、Gateway、HTTPRoute 和 GRPCRoute。
- 优先使用 preferred API version，同时不把版本写死在二进制中。
- 展示 GatewayClass controller、conditions，Gateway 地址、listener、TLS 与 conditions。
- 展示 Route hostnames、parentRefs、matches、filters、backendRefs、weight 与 parent conditions。
- CRD 未安装时返回 `available=false` 和说明，不把“未安装”误报为 404。
- 编辑入口复用通用资源 YAML 编辑器，受 Kubernetes RBAC、只读模式和审计约束。

### NetworkPolicy 可视化与解释

- 解析 `podSelector`、`namespaceSelector`、`matchExpressions`、`ipBlock`、`except`、数字端口、命名端口和端口范围。
- 选定命名空间时计算每条策略实际匹配的 Pod 数量；无 Pod 列表权限时降级为 warning。
- 明确提示空 Pod selector 选择全部 Pod、隔离但无允许规则、全放行规则、没有 Pod 匹配，以及 Egress 未明确放行 DNS。
- 跨命名空间 Pod 访问必须同时匹配 `namespaceSelector` 与 `podSelector`；单独的 Pod selector 只匹配同命名空间 Pod。
- NetworkPolicy 的语义是“选中即隔离”：只要策略的 `policyTypes` 包含 Ingress/Egress，该方向就必须至少有一条允许规则，否则默认拒绝。

### 连通性诊断

输入源 Pod、目标 Service、端口和协议后，Dosh 执行以下静态推演：

1. 校验命名空间、Pod、Service、端口和协议。
2. 解析 Service DNS 名称、ClusterIP/Headless/ExternalName 语义。
3. 校验 Service 是否声明请求端口，并解析 targetPort。
4. 读取 EndpointSlice，标记未就绪 Endpoint 和无 Pod targetRef 的 Endpoint。
5. 对每个 Endpoint 解析命名 targetPort。
6. 评估源 Pod Egress 策略和目标 Pod Ingress 策略。
7. 输出 `allowed`、`degraded`、`blocked` 或 `unknown` 结论，并给出 DNS、端口映射、Endpoint、策略决策、检查项和发现。

静态推演不能替代 CNI 实施验证。FQDN 策略、节点路由、运行时丢包、网络插件差异和 Service Mesh 数据面行为仍需要真实环境验证。

### Service Mesh 边界

- 检测 Istio、Linkerd 等 API Group 是否已安装。
- 提供通用资源列表/YAML 编辑入口。
- 不安装、不升级、不配置网格控制面，不解释专有流量规则的数据面行为。

## HTTP API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/network/gateway-api` | Gateway API 动态发现与状态摘要；支持 `namespace`、`q` |
| GET | `/api/v1/network/policies` | NetworkPolicy 规则、匹配数量和风险提示；支持 `namespace`、`q` |
| GET | `/api/v1/network/connectivity` | Pod → Service 静态连通性诊断；需要源/目标命名空间、Pod、Service、端口与协议 |

所有接口均为只读。`connectivity` 诊断会记录 `diagnose-connectivity` 审计事件。

## 安全与权限

- API Discovery 使用应用凭据，具体对象读取继续受当前身份 RBAC 约束。
- 不新增任意 raw API 代理路由。
- 不发送探测包，不执行 `exec`、`port-forward` 或任意命令。
- 编辑 Gateway API/NetworkPolicy 时复用既有通用资源写入链路，包含 Dry Run、resourceVersion 冲突保护、只读模式和审计。
- Service Mesh 仅做发现与通用资源入口，不扩大权限边界。

## 验收条件（EARS）

- When Gateway API CRD 已安装, Dosh shall 动态发现 GatewayClass、Gateway、HTTPRoute 和 GRPCRoute，并展示状态、地址、listener、parentRef 与 backend。
- When Gateway API CRD 未安装, Dosh shall 返回空视图和可理解的安装提示，而不是 HTTP 404。
- When 用户打开 NetworkPolicy 页面, Dosh shall 逐条展示 selector、Ingress/Egress peers、端口、匹配 Pod 数量和默认拒绝提示。
- When Egress 只选择同命名空间 Pod 且没有明确目标或 DNS 端口, Dosh shall 提示 DNS 可能失败。
- When 用户提交连通性诊断, Dosh shall 校验输入并解析 Service DNS、端口映射、EndpointSlice 与双向策略。
- When 目标 Pod 被 Ingress 策略选中但没有允许规则, Dosh shall 返回 blocked 并解释默认拒绝。
- When Endpoint 未就绪或缺少 Pod targetRef, Dosh shall 返回 degraded/unknown，不得宣称必然可达。
- When 诊断结果包含长 hostname、CIDR、selector 或建议文本, Dosh shall 在桌面和移动端换行显示，不产生页面级横向溢出。
- When 用户编辑 Gateway API 或 NetworkPolicy, Dosh shall 复用通用资源编辑器并继续执行 RBAC、只读、Dry Run、并发保护和审计。
- When Service Mesh API 已安装, Dosh shall 展示只读发现结果和通用资源入口，而不托管控制面。

## 后续边界

- 真实探测包、DNS 查询探针和 Mesh 数据面拓扑属于后续独立安全设计。
- 多集群诊断、超大 EndpointSlice 分页和历史诊断快照尚未纳入本轮。
