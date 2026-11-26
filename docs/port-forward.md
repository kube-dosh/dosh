# Port-forward 隧道

Dosh 的 Port-forward 面向浏览器运维场景：用户选择一个 Pod，或选择一个 Service 并由 Dosh 固定到某个 Ready 后端 Pod，然后通过同源临时路径访问目标 TCP 端口。隧道地址不会暴露集群 Pod IP、节点 IP 或 Dosh 进程的 loopback 监听地址。

## 1. 支持范围

- 目标类型：Pod，或带 selector 的 Service。
- Service 必须解析到至少一个 Ready 的 Running Pod；ExternalName、无 selector Service 和 UDP 端口不支持。
- 多端口会话：一次可创建最多 8 个端口，任一端口解析或 SPDY 预检失败时整次创建失败，不留下半个会话。
- Service 多端口会话固定到同一个后端 Pod，避免 80/443 等端口落到不同副本。
- 用户可以在 Service 页面指定 Ready 后端 Pod；后端 Pod 列表由 Service selector 精确查询，不受普通 Pod 列表分页上限影响。
- 仅支持 TCP。UDP 与 SCTP 不能通过该 HTTP 隧道访问。
- 支持普通 HTTP 请求、流式响应、SSE 和 WebSocket Upgrade；请求体上限为 16 MiB。

## 2. 会话生命周期与跨副本

会话元数据保存在控制面命名空间的用户级 ConfigMap 中，名称由认证用户名哈希得到：

```text
dosh-port-forward-<sha256(username) 前 20 字节 hex>
```

ConfigMap 只保存命名空间、Pod、Service、请求端口、解析后的容器端口、路径和到期时间，不保存 SPDY 连接或凭据。每个用户最多 4 个并发会话、每会话最多 8 个端口，默认 TTL 30 分钟。限额和 TTL 可通过环境变量调整：

```text
DOSH_PORT_FORWARD_TTL=30m
DOSH_PORT_FORWARD_MAX_SESSIONS_PER_USER=4
```

创建会话时使用 `resourceVersion` 乐观并发控制；同一用户的并发创建和关闭最多重试 8 次。浏览器刷新、请求被负载均衡到另一个 Dosh 副本，或 Dosh 滚动重启后，只要控制面 ConfigMap 仍存在且未过期，活动会话都能恢复。

SPDY 通道不持久化。真正代理请求时，当前副本按需打开 Kubernetes Pod port-forward 连接，并在该 HTTP 请求或 WebSocket 连接结束时关闭。这样可以安全跨副本恢复，也避免长期占用闲置连接。

## 3. 访问控制与安全边界

打开会话至少需要：

- Pod 目标：`create pods/portforward`。
- Service 目标：额外的 `get services` 权限。
- 后端 Pod 列表：`get services` 与 `list pods`。

隧道使用当前 Dosh 登录会话，但代理请求本身不要求 CSRF Header，因为任意上游 Web 应用无法获得 Dosh CSRF Token。安全边界由以下因素共同组成：

- 不可猜测的 24 字节随机会话 ID；
- 会话绑定认证用户名；
- 同源检查拒绝跨站非安全请求；
- 每个端口固定到创建时校验过的 Pod 与容器端口；
- 上游只允许 Dosh 本机 loopback 监听地址；
- 转发前移除 `dosh_session` 与 `dosh_oidc_state` Cookie；
- 上游 `Set-Cookie` 被重写到隧道路径前缀并移除 Domain，避免覆盖 Dosh 控制台 Cookie；
- 上游 `Location` 仅在本隧道目标范围内改写为隧道前缀路径。

隧道只应访问受信任的内部应用。不要把包含凭据的 URL 放入聊天记录、工单或浏览器历史。

## 4. 页面操作

1. 打开 Pod 详情、工作负载容器中心或 Service 详情。
2. 选择 TCP 端口；Service 目标还可以固定 Ready 后端 Pod。
3. 点击创建，获得每个端口的临时同源 URL。
4. 可逐项关闭端口会话，或使用“关闭全部”撤销当前身份的全部活动会话。
5. 页面每 10 秒刷新活动会话，刷新浏览器不会丢失已创建会话。

## 5. 故障排查

| 现象 | 检查项 |
|---|---|
| `port-forward target kind must be pod or service` | 入口只允许 Pod 或 Service，不支持 Deployment 名称直接转发 |
| `Service ... has no ready backing Pod` | 检查 selector、Pod Ready 条件与副本数 |
| `named target port ... is not exposed` | Service `targetPort` 使用了名称，但所选 Pod 没有同名 TCP containerPort |
| `port-forward session limit reached` | 关闭已有会话，或调整 `DOSH_PORT_FORWARD_MAX_SESSIONS_PER_USER` |
| `Pod port-forward did not become ready` | 检查 Dosh 到 API Server 的 SPDY 连接、NetworkPolicy、APIServer 审计与 `pods/portforward` RBAC |
| 隧道列表不可用 | 检查 Dosh ServiceAccount/RBAC 是否能读写控制面命名空间 ConfigMap |
| 刷新后会话消失 | 确认仍由同一认证身份访问，且会话未超过 TTL；检查 ConfigMap 是否被外部清理 |

## 6. 已知边界

- 隧道采用路径前缀反向代理；目标应用如果硬编码以 `/` 开头的绝对静态资源、前端 router basename 或第三方回调地址，可能需要在应用侧配置子路径。
- 不支持 UDP、SCTP、原始 TCP 客户端协议或任意非 HTTP Upgrade 协议。
- 请求体有 16 MiB 上限；大文件上传应使用对象存储或专用入口。
- 跨副本恢复依赖控制面命名空间、ConfigMap 读写 RBAC 与多副本共享的 Kubernetes API，而不是本地内存。
