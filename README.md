# Dosh Console

Dosh 是一套自研的 Kubernetes 运维控制台，后端使用 Go + chi + client-go，前端使用 React + TypeScript + Tailwind + shadcn 风格组件。前端构建产物通过 `go:embed` 打包进单个静态二进制，以同源 HTTP 或 HTTPS 服务提供 SPA 和 `/api/v1`。

设计约束：

- 默认监听端口：`32626`
- Helm Release / Deployment / Service / Namespace：`dosh`
- NodePort：`32626`
- 认证：默认使用 Kubernetes Bearer Token + TokenReview，身份和权限由 Kubernetes RBAC 决定；本地密码和 OIDC 为可选模式
- 传输：HTTP/HTTPS 均可运行；TLS 证书和自定义 CA 都不是启动必需项，生产仍推荐 HTTPS
- 证书：HTTPS 证书文件/Secret 更新后自动热加载，提供只读证书状态和受审计的手动重载
- 节点运维：按 `update nodes` 权限提供 Cordon/Uncordon，二次确认、审计且不驱逐现有 Pod
- 部署：2 副本、跨节点反亲和、PDB、非 root、只读根文件系统、distroless 运行镜像
- 契约：`api/openapi.yaml` 为唯一 API 契约源

详细需求、WBS、验收门槛和当前验收状态见 `docs/ears-implementation-plan.md`，运行与应急流程见 `docs/operations-runbook.md`。

## 架构

```text
Browser
  -> HTTP/HTTPS :32626
  -> Go BFF / embedded SPA
       -> auth/http -> auth/app -> domain ports
       -> module http -> module app -> platform/kube -> Kubernetes API
       -> SSE events / pod log streams
       -> metrics.k8s.io (optional)
       -> audit JSON logs -> stdout / optional signed webhook
```

依赖方向固定为 `http -> app -> domain`，`platform/kube` 只实现 app 层定义的最小端口。`domain` 和模块 `app` 不允许导入 `net/http`、`chi` 或 `k8s.io`。

## 当前候选 v0.1.5

- 源码版本：App/OpenAPI/npm `0.1.5`，Chart `0.1.7`；默认认证模式为 `token`。
- 候选镜像：`192.168.122.10:5000/dosh/dosh:0.1.5-rc-token-20261105T012750Z`，registry digest
  `sha256:c114113009ddad67313f37c288fe4cc6f7d13848de01d22dd68499146744ea2b`。
- Token 登录链路：浏览器提交 Bearer Token → `TokenReview` 校验 → 加密 HttpOnly 会话 →
  业务请求使用 `Impersonate-User`/`Impersonate-Group` → `SubjectAccessReview` 判权。Token 不写入
  Cookie、浏览器存储、审计或日志；用户 Token 与 ServiceAccount Token 均受支持。
- 传输模式：HTTP 与 HTTPS 使用同一二进制，由 `DOSH_TLS_ENABLED` 切换；HTTPS 可使用自签证书，
  OIDC 自定义 CA 和 TLS 受信 CA 均为可选增强，不是启动前提。
- 本轮验收：Go 全量/race、Vitest `63/63`、Chromium/Firefox 各 `39/39`（单 worker、零重试、
  `4g/2 CPU` 独立容器）、容量 SLO（p95 pods `20.45ms`、workloads `180.71ms`、overview
  `199.91ms`）、govulncheck、生产 npm audit 0、Trivy 0 HIGH/CRITICAL。
  NodePort `32628` 完成 HTTP→HTTPS 升级/回滚、有效/无效 Token、Cookie `Secure`、
  viewer/operator/admin 权限矩阵、审计历史 SAR、CSRF 拒绝、logout、ServiceAccount
  impersonation RBAC 和 Kubernetes Lease 分布式登录限速；生产未改动。
- 源码绑定：`deploy/release/bindings/source-binding-v247-token.txt`（218 个源文件，binary
  SHA-256 `b01911f428ac8732fc80b6ef02ce7edb610c4178454a48791d9fd9d73ebc0511`）。
- 证据归档：`/var/lib/dosh/evidence/c114113009dd/20261105T014514Z-v247-final-token-r1`，
  23 文件，manifest SHA-256
  `734cbdbc10b15e4d48f56665eed24e5e170ec68622ce8a5390b0a92114570005`。
- 生产 `dosh` 保持 App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626`。
- 当前外部商用门禁剩余 5 项：生产 SIEM、Edge/Safari 实机、GitHub Actions/分支保护、离机 WORM、
  生产变更授权。它们未关闭前不得升级生产或宣称完整商用放行。
- v2.46 及更早候选的浏览器、OIDC、性能和归档证据只作历史追溯，不继承给 v0.1.5。
- 下一候选（尚未构建镜像、未生成新绑定）：服务端证书状态、热加载和受审计手动重载。v2.47 镜像
  不包含该能力，不能用于该功能的发布验收。

## 历史实验室证据（不继承）

以下记录描述 v2.46/v2.44 等历史 digest 的实验室状态，其中“七个外部商用门禁”是历史口径；
当前源码与发布清单以本节之前的 5 项门禁为准。

- 镜像：`192.168.122.10:5000/dosh/dosh:0.1.4-rc-nodeuncordon-20261104T132617Z`，registry digest
  `sha256:2135c2aea864c25975a172d35c7973c1f15c76db266862cd09f7bc0253307bfd`。该候选修复节点
  Cordon 后 Uncordon 返回 409：只对 `spec.unschedulable` 发送 JSON merge patch，不再提交包含
  `resourceVersion`/`status` 的完整 Node，避免与 kubelet 心跳冲突。
- 当前候选源码绑定见 `deploy/release/bindings/source-binding-v246-nodeuncordon.txt`（213 个源文件）。
  生产 `dosh` 仍为 App `0.1.2` / Chart `0.1.4` / revision 12 / NodePort `32626`，本轮未改动生产。
- v2.46 当前 digest 内部门禁已通过：`make verify`、Vitest `62/62`、全量 race、mock
  Chromium/Firefox/WebKit 各 `38/38`、真实 OIDC Chromium/Firefox/WebKit 各 `16/16`、三身份
  API/RBAC、Cordon/Uncordon、rollback/restore `198→199→200→201`、Metrics 三态、429/5xx retry、
  隔离 Audit/Reveal、真实 Loki 59 事件/34 requestId、30 分钟 soak、容量 SLO、LCP `716.0 ms`、
  bundle `350.62 KiB`、govulncheck 0、生产 npm 审计 0 和 Trivy 0 HIGH/CRITICAL。
- v2.46 的 soak 为 30 条事件、60,039 帧、26,553 行日志、5 次重连、0 app error/stream failure，
  内存 `24→18 MiB`、峰值 34 MiB；Audit/SIGTERM 20/20 排空，Token/HMAC/Secret 泄露扫描 0。
- 最终证据归档：`/var/lib/dosh/evidence/2135c2aea864/20261104T145034Z-v246-final-internal-matrix-r1`，
  599 文件，manifest SHA-256 `d761698e13819c4491876a7ee6f905ba37f85d589a895ec494215fab3c6aaff9`，
  双层校验通过。Trivy 使用固定 cache 的 `--skip-db-update`，数据库更新时间与 SHA-256 见归档内
  `17-trivy-db-provenance.txt`；七个外部商用门禁仍全部为 `pending`。
- v2.44 历史 digest `sha256:e4a27d8b153b8f96ad52ba5f82f92fa3ddaf22e13bc69432c72d16e8ea449439`
  的真实 OIDC Chromium/Firefox/WebKit、rollback/restore、Metrics、retry、Loki、soak、生命周期和
  CronJob 证据仍保留在 `deploy/release/bindings/documentation-binding-v244-r4.txt` 所绑定归档中，
  只能作为历史追溯。
- WBS-4.7 日志流闭环：日志接口改为有界 NDJSON；`log` 帧承载 UTF-8 日志，正常 EOF 发送 `end`，
  流中失败发送带稳定 `code`、安全 `message` 和 `requestId` 的 `error` 并终止；响应头前的错误仍使用
  标准 JSON Problem。前端增量解析协议帧、继续净化 ANSI/OSC/DCS 和控制字符，并保留手动重连。
  真实隔离集群完成正常结束、缺失容器 4xx、未认证 401 和删除 Pod 后 follow 终止验收。前端把未收到
  `end` 帧的 EOF 显示为“日志流意外中断”，并有跨读取边界 UTF-8、断流回归测试。
- v2.44 历史 digest 已完成独立隔离矩阵：真实 OIDC Chromium/Firefox/WebKit 各 `16/16`（每浏览器 6/6 denied
  审计带 `requestId`、Token 扫描 0）、Helm `181→182 upgrade→183 rollback→184 restore` 身份/RBAC/
  logout 对账、Metrics 可用/不可用/恢复、429/500/502/503/504 有界重试、真实 Loki 59 事件/34
  requestId 与 20/20 SIGTERM 排空、30 分钟 soak（30 事件、50,577 帧、26,577 行、5 次重连、0
  app error/stream failure、内存 `22→18 MiB`/峰值 30 MiB）、工作负载完整生命周期和 CronJob
  空 selector 详情。全部 Playwright 串行、单 worker、零重试、每浏览器独立受限容器。综合归档
  `/var/lib/dosh/evidence/e4a27d8b153b/20261104T112513Z-v244-internal-matrix-r2`，553 文件，manifest
  SHA-256 `9f9560d2a7d0e30dd0c163e2ed8a5d674ee93f2bbef7dcd4d1f90c5c569472b9`；WebKit mock `37/37`
  和真实 OIDC `16/16` 补充归档 11 文件，manifest SHA-256
  `be5d5c1f4a15f041274869a83e24573ccf501ef0a05d9f60da6ccf9c8fc9f653`；均通过双层校验。容器 WebKit
  不等于 Safari，Edge/Safari 实机仍是外部门禁。
- WBS-5.8/WBS-5.9 实验室验收：真实隔离集群完成 Deployment/StatefulSet/DaemonSet 容器镜像更新、
  旁容器不变、旧 `resourceVersion` 409、同镜像幂等、Job/CronJob 501；CronJob 暂停 75 秒无新
  Job、已有 Job/Pod 不变，恢复后继续调度；`update-image`、`suspend`、`resume` 审计可检索。
- WBS-5.1 端到端闭环：独立命名空间和 Release 完成预览、viewer 拒绝、非法字段 400、创建、
  1→2 扩容、旧版本 409、镜像更新与旁容器保持、重启注解和 Pod UID 全部替换、删除后资源/Pod
  消失和详情 404；13 条生命周期审计覆盖 5 success、5 failure、3 denied，全部带 `requestId`。
  证据归档 `/var/lib/dosh/evidence/779ecae706ea/20261104T094801Z-v243-workload-lifecycle`，
  28 个文件，manifest SHA-256 `4b5205ba2ed5935de8397e0c7383695c69b62c8cc0e8f62336b4deca1b16022a`。
- WBS-4.8 完成：管理员可查询当前实例最近 1000 条审计记录，支持命名空间、动作、结果、关键词、
  分页和排序；真实 OIDC 验证未认证 401、非管理员 403、管理员 200、非法 result 400。该历史为
  进程内有界环形缓冲，重启或副本切换会清空/分叉，不替代 stdout、SIEM 或 WORM。
- WBS-4.7 回归：日志流使用增量 ANSI/OSC/DCS/C0-C1 净化器，HTML 与 `javascript:` OSC 链接只能作为
  文本显示；日志不写入浏览器持久存储，事件消息不创建可执行 DOM。
- 可访问性：保留 `--destructive-strong` 对比度修复；v2.41 三浏览器容器矩阵的亮暗 axe、主题和
  键盘路径通过。Edge/Safari 实机和企业 IdP 仍需另行验收。
- 节点、工作负载和 Pod 详情统一为“概览、实例、事件、日志、YAML”五标签；节点事件按
  `objectKind=Node`/`objectName` 过滤，节点日志只展示该节点上的 Pod。
- 工作负载详情支持 Deployment/StatefulSet/DaemonSet 容器级镜像更新，以及 CronJob 暂停/恢复；
  两类操作都携带当前 `resourceVersion`，冲突返回 409 并提示刷新，成功和失败写入审计。
- Service 与 Ingress 列表可进入详情；Service 展示 EndpointSlice 就绪与目标，Ingress 展示默认
  后端、TLS Secret 和路由目标状态，并支持事件与 YAML 深链。
- StorageClass、PV 与 PVC 列表可进入详情；StorageClass 展示参数、回收策略、卷绑定和扩容，PV
  展示容量、卷模式、CSI 与绑定 claim，PVC 展示申请/实际容量、绑定 PV、volumeMode 和 conditions，
  并支持事件与 YAML 深链。
- 命名空间列表可进入详情，展示标签、注解、ResourceQuota hard/used 与 LimitRange
  min/max/default/defaultRequest，并支持事件与 YAML 深链；local 模式 ClusterRole 只增加这三类
  只读权限。
- v2.43 历史 digest 已完成真实隔离集群镜像更新/CronJob 暂停恢复、Vitest `51/51`、mock
  Chromium/Firefox/WebKit 各 `37/37`、真实 OIDC Chromium/Firefox/WebKit 各 `16/16`、全量 race、
  容量、LCP、rollback/restore、30 分钟 soak、Metrics 三态、429/5xx 重试和真实 Loki 审计。主归档
  `/var/lib/dosh/evidence/779ecae706ea/20261104T090311Z-v243-current-candidate-lab`，manifest
  SHA-256 `f5dbfb5af524ed5fe5778873de1b8098f6cd9c9f69e6494f6a1e4c15d0d0dc15`；OIDC 补充归档
  `/var/lib/dosh/evidence/779ecae706ea/20261104T092210Z-v243-oidc-browser-matrix`，manifest
  SHA-256 `e0bea388800f741f1a147dceb9c84bafde676400ef443fc26f1002603116660e`；Metrics/retry
  补充归档的 manifest SHA-256 为 `9e58e69dd85b5e36aa502c5e32b7c28f190c9bb88537853e7066eebbad32331e`；
  真实 Loki 审计补充归档的 manifest SHA-256 为
  `7fa723805340ced9942a416e26c7c3ad2d0f2e1194614906ca4cede15e7af587`。上述补充归档均通过
  manifest 双层校验，凭据和请求/响应敏感转储未进入归档。
  企业 IdP、受信 TLS、企业 SIEM、Edge/Safari 实机、CI 真跑、离机 WORM、生产发布和七个外部
  商用门禁仍未验收。

## 本地开发

前置条件：Go 1.26、Node.js 24+、npm、可访问 Kubernetes 的 kubeconfig。

```bash
make web-install
export DOSH_SESSION_KEY="$(openssl rand -base64 48)"
export DOSH_AUTH_MODE=token
export DOSH_CLUSTER_NAME=kubedo-lab
export DOSH_READ_ONLY=false

make web-build
go run ./cmd/dosh
cd web && npm run dev
```

开发服务器为 `http://127.0.0.1:5173`，并将 `/api`、`/healthz`、`/readyz` 代理到 `http://127.0.0.1:32626`。

Token 的获取与登录方式见下一节。开发进程直接调用本机 kubeconfig 指向的集群；需要为本地身份授予
`tokenreviews/create`、`users/groups/serviceaccounts/impersonate`、`subjectaccessreviews/create`。
如需密码登录，显式设置 `DOSH_AUTH_MODE=local`、`DOSH_ADMIN_USERNAME` 和
`DOSH_ADMIN_PASSWORD_HASH`。

## 获取登录 Token

Dosh 默认使用 Kubernetes Bearer Token 登录。登录页选择 **Kubernetes Token**，粘贴 Token 后提交；
服务端通过 `TokenReview` 校验身份，再按该身份执行 `SubjectAccessReview` 和 impersonation。

Token 不是 kubeconfig 文件，也不需要把 kubeconfig 上传到 Dosh。Token 对应身份必须具有要访问的
Kubernetes 资源权限；Dosh 自身的 ServiceAccount 不会替用户提升权限。

### 预览环境

当前预览环境使用独立 ServiceAccount。生成一个有效期为 8 小时的 Token：

```bash
kubectl -n dosh-preview create token dosh-preview-admin --duration=8h \
  > /tmp/dosh-preview-admin-token
chmod 600 /tmp/dosh-preview-admin-token
```

在终端读取并粘贴到登录页：

```bash
cat /tmp/dosh-preview-admin-token
```

Token 过期后重新执行生成命令即可，不需要重启 Dosh。

### 任意具备目标权限的 ServiceAccount

先用 Kubernetes RBAC 给 ServiceAccount 授予所需命名空间/资源的访问权限，再生成短期 Token：

```bash
kubectl -n <namespace> create token <service-account> --duration=1h
```

可以先确认 Token 身份和权限：

```bash
TOKEN="$(kubectl -n <namespace> create token <service-account> --duration=1h)"
kubectl auth whoami --token="$TOKEN"
kubectl auth can-i list pods -n <namespace> --token="$TOKEN"
```

不要把 Token 直接写在命令行历史、脚本参数或代码仓库中。需要保存时写入权限为 `0600` 的临时文件，
并在使用后删除：

```bash
umask 077
kubectl -n <namespace> create token <service-account> --duration=1h > /tmp/dosh-token
# 登录完成后
rm -f /tmp/dosh-token
```

### 从 kubeconfig 获取 Token

只有 kubeconfig 明确使用静态 Bearer Token 时，才能直接读取：

```bash
kubectl config view --raw -o jsonpath='{.users[0].user.token}{"\n"}'
```

如果 kubeconfig 使用客户端证书或 `exec` 插件（例如云厂商 CLI），其中通常没有可直接粘贴的 Bearer
Token；请改用具备权限的 ServiceAccount Token，或由集群管理员通过支持的 Token 签发流程生成。

### 安全要求

- Token 等同于登录凭据，不要提交到代码库、写入 Dosh 配置、审计日志、浏览器存储或发送给无关人员。
- 使用满足任务所需的最小权限和最短有效期；验收完成后立即删除临时文件。
- 生产环境不要长期复用 `cluster-admin` Token；应为 Dosh 用户或 ServiceAccount 配置最小 RBAC。
- Token 被撤销后，已经建立的 Dosh 加密 Cookie 会话仍会持续到 `DOSH_SESSION_TTL`；需要立即失效时
  同时轮换 Session Key。

## 构建与验证

```bash
make fmt
make test
make web-typecheck
make web-test
make web-build
make web-e2e-container
make web-performance
make e2e-cronjob-detail
make e2e-workload-lifecycle
make e2e-log-stream
make e2e-audit-loki
make web-bundle-check
make capacity-test
make arch
make api-check
make evidence-check
make helm-lint
make build
make image TAG=0.1.5
make image-scan TAG=0.1.5
```

镜像扫描默认设置 `TRIVY_GODEBUG=http2client=0`，避免漏洞库下载时触发 HTTP/2
`PROTOCOL_ERROR`。漏洞库下载较慢时可通过 `TRIVY_TIMEOUT=30m make image-scan TAG=<version>`
调整扫描超时；若必须复用已预取的 OCI 漏洞库，可显式传入
`TRIVY_DB_FLAGS=--skip-db-update`，并记录数据库 artifact digest 与更新时间。超时、漏洞库不可达
或数据库过旧都只表示扫描未完成，不能替代漏洞结论。

发布候选必须执行 `make release-check TAG=<version>`。该目标串联 `make verify`、全量 race、
Vitest、容量 SLO、mock 容器 E2E、生产构建性能、bundle 预算和证据归档检查，再从当前源码
构建指定 tag 并扫描该镜像；任一环节失败都不得生成放行结论，也不得扫描本地旧 `dev` 镜像代替。
完整浏览器矩阵必须在受限容器中串行执行；WebKit 只能在独立 browser runner/CI 运行，当前物理宿主
禁止直接重跑 WebKit。本机只能执行 `make verify` 和 Chromium/Firefox 定向验证，不能用其替代
Edge/Safari 实机或完整浏览器矩阵。

`make arch` 执行 `scripts/check-architecture.sh`，并作为架构门禁而非提示：它拒绝越层依赖、
跨模块生产代码 import、非 `bootstrap` Router、`init()` 路由、任意 proxy/通配路由，以及
`internal/platform/config` 之外的生产环境变量读取。路由变更还必须通过
`go test ./internal/bootstrap -run 'TestRouteInventoryMatchesOpenAPI|TestModuleRoutesAreScoped'`。

`make evidence-check` 验证证据归档脚本的完整性链：`summary.txt` 必须被 `manifest.sha256`
覆盖，manifest 自身摘要写入 `manifest-digest.txt`；凭据文件、非法 digest 和空归档均应被拒绝。

可选 Metrics 依赖在隔离测试环境使用独立 namespace `dosh-metrics` 安装，不纳入 `dosh` Helm Release：

```bash
bash scripts/install-metrics-server.sh
DOSH_E2E_KEEP_ARTIFACTS=1 bash scripts/e2e-metrics.sh
```

`make web-e2e-container` 固定使用 `@playwright/test 1.64.1` 和
`mcr.microsoft.com/playwright:v1.64.1-noble`，执行登录、总览、节点 Cordon/Uncordon、工作负载
扩容、五标签详情、日志流、未认证跳转和移动端导航。宿主默认只允许 Chromium/Firefox，WebKit 仅在独立
runner/CI 或显式 opt-in 时注册。
宿主机直接执行 `cd web && npm run e2e` 时要求本机已安装对应浏览器及系统依赖。
`npm run e2e` 现已统一转交受限容器 runner；本机只需安装 Podman 和对应 Playwright 镜像，
不再要求在宿主安装浏览器或系统依赖。

浏览器测试默认单 worker、每个浏览器独立容器，并由 `scripts/run-playwright-container.sh`
默认限制为 `memory=4g`、`cpus=2`、私有 `/dev/shm=1g`、`pids=2048` 和 20 分钟硬超时，硬上限为
`6g/4 CPU`；宿主级
`flock` 禁止两个 Playwright 实例重叠运行。所有 `npm run e2e:*` 入口都转交该 runner，
不允许再直接调用宿主上的 `npx playwright test`。
禁止使用 `--ipc=host`；Chromium、Firefox 和 WebKit 必须分别启动容器，浏览器测试串行执行，
不能通过 `--workers` 覆盖并发，也不能用正数 `--retries` 重试掩盖失败。v2.45 当前候选已完成
mock Chromium/Firefox/WebKit 各 `38/38` 和真实 OIDC Chromium/Firefox/WebKit 各 `16/16`。
v2.44 历史候选的 mock Chromium/Firefox/WebKit 各 `37/37` 和真实 OIDC 三浏览器各 `16/16` 只作追溯。
v2.41/v2.43 的浏览器结果只作历史追溯，不继承。
WebKit 只能在隔离 libvirt runner 执行，物理宿主不得直接运行 WebKit。runner
在无 `--project` 时安全默认 Chromium/Firefox 两个独立容器；显式多项目也逐项目拆容器，只有隔离
runner 才允许设置 `DOSH_PLAYWRIGHT_ALLOW_WEBKIT=1` 调度 WebKit。

独立 WebKit runner 的固定环境为 `dosh-browser-runner`（8 GiB RAM、4 vCPU）和镜像
`192.168.122.10:5000/dosh/playwright:v1.64.1-noble`。runner 不能访问 Docker Hub，必须使用本地
registry 镜像；不要在物理宿主执行以下命令。

```bash
virsh start dosh-browser-runner
ssh runner@192.168.122.173

# 以下在 runner 内执行；源码位于 /opt/dosh
cd /opt/dosh
DOSH_PLAYWRIGHT_ALLOW_WEBKIT=1 \
DOSH_PLAYWRIGHT_IMAGE=192.168.122.10:5000/dosh/playwright:v1.64.1-noble \
  bash scripts/run-playwright-container.sh --project=webkit --retries=0

DOSH_PLAYWRIGHT_ALLOW_WEBKIT=1 \
DOSH_PLAYWRIGHT_IMAGE=192.168.122.10:5000/dosh/playwright:v1.64.1-noble \
DOSH_PLAYWRIGHT_CONFIG=playwright.oidc.config.ts \
DOSH_PLAYWRIGHT_HOST_NETWORK=1 \
DOSH_E2E_BASE_URL=https://192.168.122.100:32627 \
  bash scripts/run-playwright-container.sh --project=webkit --retries=0

exit
virsh destroy dosh-browser-runner
```

v2.46 当前 digest 已完成 mock Chromium/Firefox/WebKit 各 `38/38`、真实 OIDC
Chromium/Firefox/WebKit 各 `16/16`，全部保持 `workers=1`、`retries=0` 和每浏览器独立容器。
v2.45 及更早结果只作追溯，不能继承给后续 digest。
WebKit 容器不等于 Safari 实机，Edge/Safari 仍需单独的门禁证据；
源码或镜像改变后必须在新 digest 重新执行，不得继承本轮结果。
如需调整，使用 `DOSH_PLAYWRIGHT_MEMORY`、`DOSH_PLAYWRIGHT_CPUS`、
`DOSH_PLAYWRIGHT_SHM_SIZE`、`DOSH_PLAYWRIGHT_TIMEOUT`，并先运行 `make playwright-check`。

`make e2e-cronjob-detail` 在已运行的隔离 OIDC Release 上创建无 selector 的 suspended CronJob，
通过真实 API 验证详情不 panic、`selector={}`、`pods=[]`，并在脚本退出前删除资源且确认 NotFound。

`make e2e-workload-lifecycle` 串行启动独立 OIDC 夹具，在隔离 NodePort `32630` 和独立目标
命名空间完成工作负载写操作闭环。脚本校验生产 Release/revision/NodePort 指纹前后不变，验证
preview 不落盘、viewer 403、非法输入 400、创建 201、重复创建 409、扩容、旧
`resourceVersion` 409、镜像更新、重启和删除，并用 kubectl 对账 Deployment、Pod UID 与容器镜像。
脚本还会查询 `audit-events`，要求 create/scale/update-image/restart/delete 的成功、失败和
拒绝记录都带 `requestId`；默认保留 artifact 并清理临时 namespace、Release 和 RBAC。

`make e2e-log-stream` 使用独立本地认证 Release、NodePort `32631` 和临时 TLS/Secret，验证日志
NDJSON 的 `log`→`end` 完成语义、缺失容器与未认证的 JSON Problem、删除 Pod 后 follow 流的
`end`/`error` 终止，以及测试 HTML/ANSI 字节保真。脚本会核对生产 revision、镜像、NodePort 和
Ready 指纹前后一致，结束后清理全部夹具；不得把 Cookie、密码或 Session Key 写入 artifact。

`make e2e-audit-loki` 运行审计 Webhook 全矩阵并把已验签事件写入隔离 Loki：自动部署 TLS、
租户鉴权、非 root、只读根文件系统的 `dosh-e2e-loki`，再通过受控写入验证 34 个 requestId
可检索、SIGTERM 记录无丢失和租户隔离。该目标保留 artifact，默认镜像为内部 registry 的
`dosh/loki:3.5.4`；这是实验室验证，不替代企业 SIEM 的持久化、告警、访问审批、长期保留和
离机 WORM。

构建完成并取得 registry digest 后，使用 `scripts/create-source-binding.sh` 固定 tag、digest、
镜像内二进制、运行时源码树、构建日志和 digest 文件哈希。源码绑定、扫描输出和隔离验收目录必须
通过 `scripts/archive-evidence.sh` 按同一 digest 归档，并执行 manifest 双层 SHA-256 校验。

`make image` 先在宿主机完成前端和 Go 构建，再使用 distroless 非 root 基镜像打包，以避免将完整构建工具链带入运行镜像。

发布前还应执行：

```bash
GOPROXY=https://goproxy.cn,direct go run golang.org/x/vuln/cmd/govulncheck@latest ./...
cd web && npm audit --omit=dev --audit-level=high --registry=https://registry.npmjs.org
```

## 部署

集群跳板机和镜像仓库按当前实验环境填写：

```bash
# 1. 创建命名空间和会话密钥；Token 模式不需要管理员密码
kubectl create namespace dosh --dry-run=client -o yaml | kubectl apply -f -
openssl rand -base64 48 > /tmp/session-key
kubectl -n dosh create secret generic dosh-auth \
  --from-file=session-key=/tmp/session-key \
  --dry-run=client -o yaml | kubectl apply -f -

# 2a. HTTPS（推荐）：准备 TLS Secret，SAN 必须包含访问地址
kubectl -n dosh create secret tls dosh-tls \
  --cert=/path/to/tls.crt --key=/path/to/tls.key \
  --dry-run=client -o yaml | kubectl apply -f -

helm upgrade --install dosh deploy/helm/dosh \
  --namespace dosh --create-namespace --wait --timeout 5m

# 2b. 或 HTTP：明确关闭 TLS 和 Secure Cookie；无需 TLS Secret/CA
helm upgrade --install dosh deploy/helm/dosh \
  --namespace dosh --create-namespace \
  --set tls.enabled=false \
  --set auth.cookieSecure=false \
  --wait --timeout 5m

# 3. 验收
kubectl -n dosh get deploy,svc,pod,pdb,sa -o wide
kubectl -n dosh get svc dosh -o jsonpath='{.spec.ports[0].nodePort}'
curl -k https://<worker-node-ip>:32626/healthz   # HTTPS
curl -fsS http://<worker-node-ip>:32626/healthz   # HTTP
```

浏览器访问对应协议地址。登录时粘贴具有 Kubernetes 权限的 Bearer Token；也可用
`kubectl create token <serviceaccount> --duration=1h` 临时生成。用户需在集群中具备相应业务权限；
dosh 自身的 ServiceAccount 只用于 TokenReview、SubjectAccessReview 和 impersonation，不继承其
权限执行用户操作。

### 服务端证书热加载

#### 证书存放位置与挂载方式

服务端证书存放在 **Kubernetes Secret** 中，不使用 ConfigMap。默认 Secret 名称为 `dosh-tls`，
必须位于 Dosh Release 所在 namespace，并包含标准 TLS Secret 键：

| Secret 键 | 内容 | 容器内路径 |
|---|---|---|
| `tls.crt` | 服务器证书，可包含中间 CA 证书链 | `/etc/dosh/tls/tls.crt` |
| `tls.key` | 与证书匹配的私钥 | `/etc/dosh/tls/tls.key` |

Chart 的 `deploy/helm/dosh/templates/deployment.yaml` 会创建 Secret volume，并以只读方式挂载：

```yaml
volumes:
  - name: tls
    secret:
      secretName: dosh-tls
      defaultMode: 0440
containers:
  - name: dosh
    volumeMounts:
      - name: tls
        mountPath: /etc/dosh/tls
        readOnly: true
```

对应环境变量为：

```text
DOSH_TLS_ENABLED=true
DOSH_TLS_CERT_FILE=/etc/dosh/tls/tls.crt
DOSH_TLS_KEY_FILE=/etc/dosh/tls/tls.key
DOSH_TLS_RELOAD_INTERVAL=30s
```

Chart **不把证书或私钥写入 `values.yaml`，也不自动创建 TLS Secret**。这是有意的：私钥不能进入
Git、Helm values 或 Release 历史，证书由运维或 cert-manager 等外部签发系统写入 `existingSecret`。
Chart 只负责引用和挂载该 Secret。`tls.enabled=false` 时不会创建 TLS volume、不会挂载
`/etc/dosh/tls`，探针也切换为 HTTP。

创建默认 Secret：

```bash
kubectl -n dosh create secret tls dosh-tls \
  --cert=/path/to/tls.crt --key=/path/to/tls.key \
  --dry-run=client -o yaml | kubectl apply -f -
```

使用其他 Secret 名称时显式覆盖：

```bash
helm upgrade --install dosh deploy/helm/dosh \
  --namespace dosh \
  --set tls.existingSecret=dosh-prod-tls
```

可以只检查 Secret 的键名，不输出值：

```bash
kubectl -n dosh get secret dosh-tls \
  -o go-template='{{range $key, $_ := .data}}{{$key}}{{"\n"}}{{end}}'
```

Chart 相关配置全部位于 `deploy/helm/dosh/values.yaml`：

```yaml
tls:
  enabled: true
  existingSecret: dosh-tls
  certFile: /etc/dosh/tls/tls.crt
  keyFile: /etc/dosh/tls/tls.key
  reloadInterval: 30s
```

可用下面的命令核对最终渲染结果：

```bash
helm template dosh deploy/helm/dosh -n dosh \
  --set tls.enabled=true \
  --set tls.existingSecret=dosh-tls \
  | rg -n 'DOSH_TLS_|secretName:|mountPath: /etc/dosh/tls|readOnly: true'
```

#### 热加载行为

HTTPS 模式下，Dosh 启动时校验上述挂载的证书与私钥；此后默认每 `30s` 检查一次文件，
发现变化后原子切换到新证书，不需要重启 Pod。轮询间隔由 Helm 值 `tls.reloadInterval` 或环境变量
`DOSH_TLS_RELOAD_INTERVAL` 控制，设为 `0` 时关闭自动轮询，仅保留手动重载。

更新证书：

```bash
kubectl -n dosh create secret tls dosh-tls \
  --cert=/path/to/new/tls.crt --key=/path/to/new/tls.key \
  --dry-run=client -o yaml | kubectl apply -f -
```

Kubernetes 更新 Secret 后，kubelet 同步挂载内容通常需要几十秒；随后 Dosh 在下一个轮询周期加载
新证书。可在“配置与密钥 → 服务端证书”查看有效期、SAN、密钥算法、SHA-256 指纹、最近加载时间和
最近错误。也可以通过受保护 API 主动重载：

```text
GET  /api/v1/certificate
POST /api/v1/certificate/reload
```

手动重载需要 `update` `dosh.io/server-certificates` 权限，并写入审计；cluster-admin 默认满足。
HTTP 模式返回 `status=disabled`，不会读取证书文件。新证书或私钥无效时，Dosh 保留上一份可用证书
继续服务，并在状态中显示最近一次加载错误；私钥不会通过 API、审计日志或浏览器存储返回。

直接执行 `kubectl apply` 更新 Secret 不会触发 Pod 滚动重启，因此由热加载接管；如果随后执行
`helm upgrade`，Deployment 中的 `checksum/tls-secret` annotation 会变化并触发滚动发布，这属于
Helm 的正常行为。

#### Cloudflare Full (strict) 前置 Nginx（preview）

`dosh-preview` 的公网预览链路是
`Cloudflare → 主机 Nginx（kubedo-dashboard-proxy）→ preview NodePort 32630 → dosh-preview`。
Cloudflare 的 Full (strict) 校验发生在前置 Nginx，而不是 Dosh Pod：

| 组件 | 证书/位置 | 说明 |
|---|---|---|
| Cloudflare 源站 | `/etc/kubedo/dashboard-proxy/cloudflare/dosh.xuthus.cc.crt` | Cloudflare Origin CA 证书，SAN 为 `dosh.xuthus.cc` |
| Cloudflare 源站私钥 | `/etc/kubedo/dashboard-proxy/cloudflare/dosh.xuthus.cc.key` | 权限 `0600`，不进入仓库、Helm values 或 Secret |
| Nginx 挂载点 | `/etc/nginx/cloudflare` | 只读挂载，SNI 为 `dosh.xuthus.cc` 时使用 |
| Dosh 自身 TLS | `dosh-preview/dosh-tls` | 仍为内部自签证书，仅供 Nginx 到 Dosh 的后端加密 |

Nginx 在 `32630` 上按 SNI 分流：带 `dosh.xuthus.cc` SNI 的 Cloudflare 回源使用 Origin CA
证书并代理到 `kubedo_dashboard_preview`；不带 SNI 的本机/局域网直连仍使用原自签证书。Cloudflare
Origin Rule `dosh` 当前回源端口为 `32630`。主机也准备了 `443` 监听，但切换前必须先确认上游
防火墙已放行公网 `443`，否则会出现 Cloudflare `522`。

验收命令：

```bash
openssl s_client -connect 127.0.0.1:32630 -servername dosh.xuthus.cc </dev/null 2>/dev/null \
  | openssl x509 -noout -issuer -dates -ext subjectAltName
curl -sS -o /dev/null -w '%{http_code}\n' https://dosh.xuthus.cc/
```

轮换 Cloudflare 源站证书时，替换上述 `.crt`/`.key` 文件，执行
`podman exec kubedo-dashboard-proxy nginx -t`，再执行
`systemctl restart kubedo-dashboard-proxy.service`。不要输出私钥，也不要把 Cloudflare API Token
写入脚本、命令历史、文档或仓库。

OIDC 与自定义 CA 均非必需。需要企业 SSO 时再设置 `auth.mode=oidc` 并提供 issuer/client/redirect；
只有 IdP 使用私有 CA 时才配置 `auth.oidc.ca.existingConfigMap`。

镜像推送命令：

```bash
podman push --tls-verify=false 192.168.122.10:5000/dosh/dosh:0.1.5
```

## 安全说明

- 不提供任意 Kubernetes API proxy、Pod exec 或 port-forward。
- Secret 列表只返回键名和大小；明文查看需要管理员能力、CSRF、二次确认并写审计日志。
- 所有写操作同时执行同源/Fetch Metadata 校验、CSRF、前端按钮隐藏、后端能力校验、
  Kubernetes RBAC 和审计记录；跨域 `Origin` 或 `Sec-Fetch-Site: cross-site` 写请求返回 403。
- Cordon/Uncordon 需要 `update nodes`；Cordon 只阻止新 Pod 调度，不驱逐已有 Pod。local 模式
  Chart 只增加 `nodes` 的 `update/patch`；OIDC/Token 模式使用 impersonation + SubjectAccessReview。
- Token 模式 Chart 增加 `tokenreviews/create`、`subjectaccessreviews/create`、
  `users/groups/serviceaccounts/impersonate` 和命名空间 Lease 限速权限；不授予业务资源权限。
- Bearer Token 只参与 TokenReview 校验，不进入 URL、日志、审计、Cookie 或浏览器持久存储。
  会话是加密 HttpOnly Cookie；Token 被撤销后，已建立会话仍持续到 `DOSH_SESSION_TTL` 上限。
- Helm 默认启用 NetworkPolicy：Pod 仅接受入口 TCP 32626，出口仅允许 DNS 与 Kubernetes API；检测到 `cilium.io/v2` 时自动生成 CNP，并通过 `toEntities: kube-apiserver` 访问 API Server。
- 请求体大小由 `DOSH_MAX_REQUEST_BODY` 控制；默认 2 MiB。
- 会话密钥、可选密码哈希和 TLS 私钥只通过 Secret 注入，不写入镜像或仓库；OIDC CA 和审计 CA
  仅在需要时以只读 ConfigMap 挂载。
- 服务端证书状态 API 只返回公开证书元数据；证书热加载失败时保留上一份可用证书，且不会把私钥、
  PEM 内容或 Secret 原文写入日志、审计和浏览器存储。
- 生产 `dosh` 当前仍为 App `0.1.2` / Chart `0.1.4` / revision 12，未升级。当前源码的外部商用
  门禁剩余 5 项：生产 SIEM、Edge/Safari 实机、GitHub Actions/分支保护、离机 WORM 和生产变更授权。

## 目录

```text
cmd/dosh/                    # 进程入口、version、hash-password
internal/domain/             # 纯业务类型、错误、端口接口
internal/modules/*/app/      # 用例、校验、权限、审计
internal/modules/*/http/     # chi 路由和 DTO 适配
internal/platform/kube/      # client-go 适配器
internal/bootstrap/          # 组合根、路由、嵌入 SPA
api/openapi.yaml             # OpenAPI 3.1 契约
web/src/                     # React 控制台
deploy/helm/dosh/            # Helm Chart
docs/                        # EARS 实施文档和运行手册
```
