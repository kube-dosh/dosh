# Dosh 运行手册

## 1. 当前部署基线

| 项目 | 值 |
|---|---|
| Namespace | `dosh` |
| Helm Release | `dosh` |
| Deployment / Service | `dosh` |
| 副本数 | 2 |
| NodePort | `32626` |
| 容器端口 | `32626` |
| 当前生产版本 | App `0.1.2` / Chart `dosh-0.1.4`（revision 12，2/2 Ready） |
| 当前源码版本 | App `0.1.5` / Chart `dosh-0.1.7`（Token/HTTP/HTTPS 候选，生产未升级） |
| 当前源码状态 | v2.47：默认 TokenReview 登录；OIDC 与自定义 CA 可选；HTTP/HTTPS 双模式；Token 模式使用 Kubernetes Lease 分布式登录限速。v2.46 及更早证据不继承 |
| 下一源码候选 | v2.48（实施中）：服务端证书状态、文件/Secret 热加载和受审计手动重载；尚未构建镜像或更新发布绑定 |
| 当前候选 | `192.168.122.10:5000/dosh/dosh:0.1.5-rc-token-20261105T012750Z@sha256:c114113009ddad67313f37c288fe4cc6f7d13848de01d22dd68499146744ea2b` |
| 隔离 Token Release | `dosh-token-e2e/dosh-token-e2e` revision 10（rollback 到 revision 8 配置）、1/1 Ready、NodePort `32628`；HTTPS 自签证书；生产未改动 |
| 源码绑定 | `deploy/release/bindings/source-binding-v247-token.txt`；218 个源文件，binary SHA-256 `b01911f428ac8732fc80b6ef02ce7edb610c4178454a48791d9fd9d73ebc0511` |
| 证据归档 | `/var/lib/dosh/evidence/c114113009dd/20261105T014514Z-v247-final-token-r1`；23 文件，manifest SHA-256 `734cbdbc10b15e4d48f56665eed24e5e170ec68622ce8a5390b0a92114570005` |
| 历史隔离候选 | `0.1.4-rc-nodeuncordon-20261104T132617Z@sha256:2135c2ae…07bfd`；证据不继承给 v2.47 |
| 历史日志流候选 Release | `dosh-logs-e2e/dosh-e2e-logs` revision 1、2/2 Ready、NodePort `32631`；绑定 v2.44 digest，验收后已清理，证据不继承给 v2.46 |
| 历史隔离候选 | `192.168.122.10:5000/dosh/dosh:0.1.4-rc-audit-20261104T062708Z@sha256:1e16c0fb923b6451c46a676bdf671eef9dcfd21e08a8a093d1193b844b655f94`（证据不继承给当前源码） |
| 历史隔离 Release | `dosh-ui-e2e/dosh-ui-e2e` revision 15、2/2 Ready、NodePort `32629`；绑定 v2.43 digest，不继承给 v2.44 |
| 历史完整候选 | `192.168.122.10:5000/dosh/dosh:0.1.3-rc-sec-20261104035000@sha256:69f30f0016516ac9b56ff867f23c8c0039810dcff29179420c12da323cc00523`（证据不继承给 `0.1.4`） |
| 镜像 | `192.168.122.10:5000/dosh/dosh:<tag>`；生产发布必须使用 registry digest |
| 认证 Secret | `dosh-auth` |
| TLS Secret | `dosh-tls`；仅在 `tls.enabled=true` 时需要 |
| 认证方式 | 默认 Kubernetes Token；`local` 和 `oidc` 为可选模式 |
| 访问方式 | HTTPS 或 HTTP：`https?://<worker-node-ip>:32626` |

## 2. 日常检查

```bash
helm -n dosh list
kubectl -n dosh rollout status deployment/dosh --timeout=120s
kubectl -n dosh get deploy,svc,pod,pdb,sa -o wide
kubectl -n dosh get events --sort-by=.lastTimestamp | tail -20
DOSH_BASE_URL=${DOSH_BASE_URL:-https://192.168.122.100:32626}
curl -k "$DOSH_BASE_URL/healthz"
curl -k "$DOSH_BASE_URL/readyz"
```

预期：2 个 Pod Ready，分布在两个 Worker；`/readyz` 中 `kubernetes` 为 `ok`。若 Kubernetes API 不可达，`/readyz` 返回 503，`/healthz` 仍返回 200。

Token 登录对账（HTTP 模式去掉 `-k`）：

```bash
TOKEN=$(kubectl -n <namespace> create token <serviceaccount> --duration=1h)
curl -k -c /tmp/dosh.cookies -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg token "$TOKEN" '{token:$token}')" \
  "$DOSH_BASE_URL/api/v1/session" | jq '{authMode,user:.user.username,capabilities}'
unset TOKEN
curl -k -b /tmp/dosh.cookies "$DOSH_BASE_URL/api/v1/pods?namespace=<namespace>"
```

有效 Token 返回 200，无业务权限返回 403，无效 Token 返回 401。服务端要求
`tokenreviews/create`、`users/groups/serviceaccounts/impersonate`、`subjectaccessreviews/create`；
Token 不得出现在日志、审计、Cookie 或浏览器存储中。

### 2.1 节点 Cordon/Uncordon

节点详情页的“停止调度/恢复调度”只在当前会话具备 `nodeCordon` capability（Kubernetes
`update nodes`）时显示；点击后必须二次确认。Cordon 不会驱逐已有 Pod，只影响后续调度。

```bash
kubectl get node <node> -o jsonpath='{.spec.unschedulable}{"\n"}'
```

API 对账（已登录会话、带 CSRF）：

```bash
curl -k -b session.cookie -H "X-CSRF-Token: $CSRF" -X POST \
  https://<node-ip>:32626/api/v1/nodes/<node>/cordon
kubectl get node <node> -o jsonpath='{.spec.unschedulable}{"\n"}'   # true
curl -k -b session.cookie -H "X-CSRF-Token: $CSRF" -X POST \
  https://<node-ip>:32626/api/v1/nodes/<node>/uncordon
kubectl get node <node> -o jsonpath='{.spec.unschedulable}{"\n"}'   # 空/false
```

变更前后检查 `kubectl get pods -A -o wide --field-selector spec.nodeName=<node>`、节点容量和
审计日志中的 `action=cordon|uncordon`、`result=denied|success`、`request_id`。任何验收失败都
必须立即执行 Uncordon；不要在生产节点上做测试。

### 2.1.1 节点标签与污点

节点详情的“标签”和“污点”页签支持新增、编辑和删除，编辑权限与 Cordon/Uncordon 一致，要求
Kubernetes `update nodes`。每次写入都携带页面加载时的节点 `resourceVersion`；节点在此期间被
其他控制器更新时返回 409，页面提示刷新后重试，避免覆盖并发变更。标签和污点变更分别记录
`action=update-labels|update-taints` 审计事件。

标签 API 对账（已登录会话、带 CSRF；先读取 `resourceVersion`）：

```bash
RESOURCE_VERSION=$(kubectl get node <node> -o jsonpath='{.metadata.resourceVersion}')
curl -k -b session.cookie -H "X-CSRF-Token: $CSRF" -X PUT \
  -H 'Content-Type: application/json' \
  https://<node-ip>:32626/api/v1/nodes/<node>/labels \
  -d "{\"resourceVersion\":\"$RESOURCE_VERSION\",\"labels\":{\"topology.kubernetes.io/zone\":\"az-1\"}}"
kubectl get node <node> -o jsonpath='{.metadata.labels.topology\.kubernetes\.io/zone}{"\n"}'
```

污点 API 使用同一资源版本和安全边界：

```bash
RESOURCE_VERSION=$(kubectl get node <node> -o jsonpath='{.metadata.resourceVersion}')
curl -k -b session.cookie -H "X-CSRF-Token: $CSRF" -X PUT \
  -H 'Content-Type: application/json' \
  https://<node-ip>:32626/api/v1/nodes/<node>/taints \
  -d "{\"resourceVersion\":\"$RESOURCE_VERSION\",\"taints\":[{\"key\":\"dedicated\",\"value\":\"gpu\",\"effect\":\"NoSchedule\"}]}"
kubectl get node <node> -o jsonpath='{.spec.taints}{"\n"}'
```

`NoExecute` 会驱逐缺少对应 toleration 的已有 Pod。生产验收应先在非生产节点使用
`PreferNoSchedule` 或无害标签完成 API 对账，禁止直接在承载关键业务的节点上测试污点。

### 2.1.2 审计历史查询

管理员可在“审计历史”页面查询当前实例最近 1000 条审计记录，按命名空间、动作、结果和关键词
过滤，并分页/排序。API 为 `GET /api/v1/audit-events`；viewer/operator 返回 403，未认证返回 401，
非法 `result` 返回 400。详情包含用户组、错误和 `requestId`，用于与 stdout/SIEM 关联。

Token/OIDC 模式下，审计历史权限由 SubjectAccessReview 的
`apiGroups=["dosh.io"], resources=["audit-events"], verbs=["get"]` 决定；cluster-admin 自动满足。
如需授予非管理员，可创建对应 ClusterRole/Binding，而不是依赖前端显示。

该记录保存在单进程有界内存中，Pod 重启或副本切换后会清空或分叉，不是持久审计系统，也不能替代
stdout、SIEM 或离机 WORM。调查事件时仍以集中日志和 SIEM 为合规事实源。

### 2.1.2 工作负载镜像更新

工作负载详情页的“更新镜像”只在当前会话具备 `updateImage` capability 时显示，适用于
Deployment、StatefulSet 和 DaemonSet。请求为：

```http
POST /api/v1/workloads/{kind}/{namespace}/{name}/image
Content-Type: application/json
X-CSRF-Token: <token>

{"containerName":"app","image":"registry.example.test/app@sha256:...","resourceVersion":"<当前版本>"}
```

服务端先校验 kind、命名空间、工作负载名、容器名、镜像和资源版本，再执行资源级授权；随后读取
工作负载并比较 `resourceVersion`，只有版本一致时才更新 Pod 模板并触发滚动发布。旧版本且目标
镜像尚未生效时返回 409，不覆盖他人变更；目标镜像已经生效时按幂等成功处理。Job/CronJob 返回
501，本功能不修改其 Pod 模板。所有成功、校验失败、无权限和冲突都记录 `action=update-image` 审计。

发布前应使用不可变 digest，避免节点无法拉取新镜像或 tag 漂移。更新后执行：

```bash
kubectl -n <namespace> get <kind> <name> -o jsonpath='{range .spec.template.spec.containers[*]}{.name}{"="}{.image}{"\n"}{end}'
kubectl -n <namespace> rollout status <kind>/<name> --timeout=5m
```

完整写操作闭环（预览、创建、扩容、镜像更新、重启、删除、409/RBAC/审计对账）使用独立夹具：

```bash
make e2e-workload-lifecycle
```

该目标串行启动独立 OIDC Release/NodePort `32630` 和目标 namespace，校验生产 revision、
NodePort、镜像、UID/generation/resourceVersion 指纹前后不变；默认保留 artifact，并在退出时
删除临时 Deployment、Release、namespace 和 RBAC。v2.44 历史候选通过结果为 13 条生命周期审计
（5 success、5 failure、3 denied）全部带 `requestId`，生产指纹保持 App `0.1.2` / Chart
`0.1.4` / revision 12 / NodePort `32626` / 2/2 Ready；证据纳入
`/var/lib/dosh/evidence/e4a27d8b153b/20261104T112513Z-v244-internal-matrix-r2`，manifest
SHA-256 `9f9560d2a7d0e30dd0c163e2ed8a5d674ee93f2bbef7dcd4d1f90c5c569472b9`，双层校验通过。

### 2.1.3 CronJob 暂停与恢复

CronJob 详情页的“暂停调度/恢复调度”只在当前会话具备 `suspendCronJob` capability（Kubernetes
`batch/cronjobs update`）时显示，点击后需要二次确认。请求为：

```http
POST /api/v1/workloads/cronjob/{namespace}/{name}/suspend
Content-Type: application/json
X-CSRF-Token: <token>

{"suspend":true,"resourceVersion":"<当前版本>"}
```

服务端先校验 kind、命名空间、名称和资源版本，再执行资源级授权；随后读取 CronJob 并比较
`resourceVersion`。旧版本且目标状态未生效时返回 409，不覆盖他人变更；目标状态已经生效时按
幂等成功处理；非 CronJob 返回 501。暂停不会删除正在运行的 Job/Pod，恢复也不会补跑暂停期间
遗漏的任务。成功、校验失败、无权限和冲突分别记录 `action=suspend|resume` 审计。

```bash
kubectl -n <namespace> get cronjob <name> -o jsonpath='{.spec.suspend}{"\n"}'
kubectl -n <namespace> get jobs --sort-by=.metadata.creationTimestamp
```

### 2.1.4 工作负载关联 Service

工作负载详情的“访问方式”页签按 Pod Selector 展示同命名空间 Service。创建、编辑和删除分别要求
Kubernetes `create services`、`update services`、`delete services`，服务端同时执行 CSRF、只读模式
和资源级授权检查。

```http
POST /api/v1/services
PUT  /api/v1/services/{namespace}/{name}
DELETE /api/v1/services/{namespace}/{name}?resourceVersion=<当前版本>
```

创建请求包含 `name`、`namespace`、`type`、`selector` 和 1–10 个 `ports`。支持 `ClusterIP`、
`NodePort`、`LoadBalancer` 与 `TCP`、`UDP`、`SCTP`；`targetPort` 可填数字或命名端口。NodePort 和
LoadBalancer 不要求手工指定节点端口，由 Kubernetes 自动分配；更新时按 `protocol/port` 保留已有
NodePort。多端口必须为每个端口命名，同一 `protocol/port` 不可重复。更新和删除携带
`resourceVersion`，旧版本返回 409，避免覆盖并发修改。

```bash
kubectl -n <namespace> get service <name> -o wide
kubectl -n <namespace> get endpointslice -l kubernetes.io/service-name=<name>
```

成功、校验失败、无权限和冲突分别写入 `action=create-service|update-service|delete-service` 审计。

### 2.1.5 命名空间删除

命名空间列表和详情可发起删除，页面要求输入命名空间名称确认。服务端要求 Kubernetes
`delete namespaces`，使用 `propagationPolicy=Foreground`，在命名空间及其全部受控资源删除完成后
才完成级联清理；请求携带页面加载时的 `resourceVersion`，冲突返回 409。`default`、`kube-*` 和空名称
在授权前即被拒绝，前端也不会展示这些命名空间的删除入口。

```http
DELETE /api/v1/namespaces/{name}?resourceVersion=<当前版本>
```

```bash
kubectl get namespace <name> -o jsonpath='{.status.phase}{"\n"}'
kubectl get all,configmap,secret,pvc -n <name>
```

删除请求返回 202 并记录 `action=delete-namespace` 审计。验收必须先使用临时命名空间，禁止删除
`default`、`kube-*`、`dosh` 或 `dosh-preview`。local 模式 ClusterRole 除只读权限外仅增加
`namespaces delete` 以及既有的受控写权限；Token/OIDC 模式仍由用户自身 RBAC 决定。

## 3. 发布

```bash
test -z "$(gofmt -l cmd internal web/embed.go)"
go vet ./...
go test -race ./...
make web-typecheck
make web-test
make web-e2e-container
make web-performance
make web-bundle-check
make capacity-test
make api-check
make arch
GOPROXY=https://goproxy.cn,direct go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
(cd web && npm audit --omit=dev --audit-level=high --registry=https://registry.npmjs.org)
make helm-lint
make build
make image TAG=<version>
make image-scan TAG=<version>
podman push --tls-verify=false 192.168.122.10:5000/dosh/dosh:<version>
```

镜像扫描默认使用 `TRIVY_GODEBUG=http2client=0`，规避漏洞库下载时的 HTTP/2
`PROTOCOL_ERROR`。漏洞库下载较慢时可设置 `TRIVY_TIMEOUT=30m`；复用已预取的 OCI 漏洞库时，
应显式传入 `TRIVY_DB_FLAGS=--skip-db-update`，并记录数据库 artifact digest 与更新时间。
扫描超时、漏洞库不可达或数据库过旧时必须记录为未完成，不得沿用旧镜像的 Trivy 结论。

发布候选统一执行 `make release-check TAG=<version>`；该目标包含 `make verify`、全量 Go race、
Vitest、容量 SLO、mock 容器 E2E、生产构建性能、bundle 预算和证据归档检查，并从当前源码
构建指定 tag 后扫描该镜像。不得只运行其中一部分，也不得扫描本地旧 `dev` 镜像代替候选。
完整浏览器矩阵必须在受限容器中串行执行；当前物理宿主禁止重跑 WebKit，本机只能执行
`make verify` 和 Chromium/Firefox 定向验证。v2.46 当前 digest 已通过隔离 runner 的 mock
Chromium/Firefox/WebKit 各 `38/38`，以及真实 OIDC Chromium/Firefox/WebKit 各 `16/16`。
WebKit 在隔离 libvirt runner 串行执行，v2.44 及更早结果不能继承。WebKit 容器结果不能替代
Edge/Safari 实机，也不能成为在物理宿主重跑 WebKit 的依据。

记录 registry 返回的 `Docker-Content-Digest`，不要只记录可变 tag、image ID 或本地 manifest
digest。若缺少 `govulncheck`，可使用固定版本
`GOPROXY=https://goproxy.cn,direct go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...`。

通过 Helm 更新镜像：

```bash
helm upgrade dosh deploy/helm/dosh -n dosh \
  --set image.digest=sha256:<registry-digest> \
  --wait --timeout 5m
```

发布后重新执行第 2 节和登录冒烟测试。

### 3.1 Playwright 主机保护

浏览器 E2E 与 libvirt 虚拟机不得无边界争用宿主资源。所有容器化 Playwright 运行必须经过
`scripts/run-playwright-container.sh`，默认单 worker、每浏览器独立容器，并应用以下限制：

| 参数 | 默认值 | 环境变量 |
|---|---|---|
| 内存与 swap 默认上限 | `4g`（硬上限 `6g`） | `DOSH_PLAYWRIGHT_MEMORY` |
| CPU 默认上限 | `2`（硬上限 `4`） | `DOSH_PLAYWRIGHT_CPUS` |
| 私有共享内存 | `1g` | `DOSH_PLAYWRIGHT_SHM_SIZE` |
| PID 上限 | `2048` | `DOSH_PLAYWRIGHT_PIDS_LIMIT` |
| 单浏览器硬超时 | `20m` | `DOSH_PLAYWRIGHT_TIMEOUT` |
| WebKit 完整矩阵许可 | `0`（禁止） | `DOSH_PLAYWRIGHT_ALLOW_WEBKIT=1` |
| 宿主互斥锁 | `/tmp/dosh-playwright.lock` | `DOSH_PLAYWRIGHT_LOCK_FILE` |

严禁使用 `--ipc=host`；浏览器必须使用容器私有 `/dev/shm`。Chromium、Firefox、WebKit
分别启动容器，避免一个浏览器崩溃或泄漏后继续影响下一个浏览器。`make playwright-check`
是发布门禁，会拒绝宿主 IPC、并行 worker 或缺少资源限制的配置。无 `--project` 时 runner
安全默认调度 Chromium、Firefox 两个独立容器；显式多项目也逐项目拆容器。当前物理宿主不会
自动调度 WebKit，只有独立 runner 才允许设置 `DOSH_PLAYWRIGHT_ALLOW_WEBKIT=1` 执行 WebKit。所有
`npm run e2e:*` 入口都转交统一 runner，不再直接调用宿主 `npx playwright test`；runner 通过宿主级
`flock` 禁止两个 Playwright 实例重叠；直接宿主运行 WebKit 会被
`scripts/e2e-oidc-browser.sh` 拒绝。CI 的 Chromium/Firefox/WebKit 矩阵也设置
`max-parallel: 1`，浏览器任务按顺序执行，每个任务仍使用独立容器和上述资源上限。

本环境的浏览器验收只按串行隔离方式执行，不再把物理宿主机故障分析纳入功能实施范围。
v2.47 当前候选默认资源为 `4g/2 CPU`；mock Chromium/Firefox 各 `39/39`，`workers=1`、
`retries=0`。Token 登录不写入 localStorage/sessionStorage；viewer/operator/admin 的真实
Kubernetes Token/RBAC 矩阵、CSRF、logout、审计 SAR 和 Lease 分布式限速均已在隔离环境验证。
当前 digest 同时通过 Vitest `63/63`、Go 全量/race、容量 SLO（p95 pods `20.45ms`、
workloads `180.71ms`、overview `199.91ms`）、bundle `350.93 KiB`、govulncheck 0、生产 npm
审计 0 和 Trivy 0 HIGH/CRITICAL。源码绑定和证据归档路径见第 1 节。

以下 v2.46 记录仅作历史追溯，不继承给 v2.47。v2.46 候选默认资源为 `4g/2 CPU`；mock
Chromium/Firefox/WebKit 各 `38/38`，真实 OIDC
Chromium/Firefox/WebKit 各 `16/16`，`workers=1`、`retries=0`。该历史 digest 同时通过 Vitest
`62/62`、全量 race、rollback/restore `198→199→200→201`、Metrics 三态、429/5xx retry、隔离
Audit/Reveal、真实 Loki 59 事件/34 requestId、30 分钟 soak、容量 SLO、LCP `716.0 ms`、bundle
`350.62 KiB`、govulncheck 0、生产 npm 审计 0 和 Trivy 0 HIGH/CRITICAL。最终证据归档为
`/var/lib/dosh/evidence/2135c2aea864/20261104T145034Z-v246-final-internal-matrix-r1`，599 文件，
manifest SHA-256 `d761698e13819c4491876a7ee6f905ba37f85d589a895ec494215fab3c6aaff9`，双层校验通过。

v2.44 历史 digest 已完成真实 OIDC Chromium/Firefox/WebKit 各 `16/16`（每浏览器 6/6 denied 审计带
`requestId`，Token 扫描 0）、Helm `181→182 upgrade→183 rollback→184 restore`、Metrics
可用/不可用/恢复、429/500/502/503/504 有界重试与持续 429 三次、真实 Loki 59 事件/34 requestId、
20/20 SIGTERM 排空、30 分钟 soak（30 事件、50,577 帧、26,577 行、5 次重连、0 app error/stream
failure、内存 `22→18 MiB`/峰值 30 MiB）、完整工作负载生命周期和 CronJob 空 selector 详情。综合
归档为 `/var/lib/dosh/evidence/e4a27d8b153b/20261104T112513Z-v244-internal-matrix-r2`，553 文件，
manifest SHA-256 `9f9560d2a7d0e30dd0c163e2ed8a5d674ee93f2bbef7dcd4d1f90c5c569472b9`，双层校验通过。
v2.41/v2.43 历史结果只作追溯，不继承给当前 digest。WebKit mock `37/37` 和真实 OIDC `16/16`
补充归档为 `/var/lib/dosh/evidence/e4a27d8b153b/20261104T114204Z-v244-webkit-r2`，manifest
SHA-256 `be5d5c1f4a15f041274869a83e24573ccf501ef0a05d9f60da6ccf9c8fc9f653`；Edge/Safari 实机仍未验收。
WebKit 仅在隔离 libvirt runner 执行，当前宿主不得重跑 WebKit；容器 WebKit 不等于 Safari 实机。

#### 3.1.1 隔离 WebKit runner 操作步骤

固定环境：libvirt domain `dosh-browser-runner`，8 GiB RAM、4 vCPU，IP `192.168.122.173`，
源码 `/opt/dosh`；浏览器镜像 `192.168.122.10:5000/dosh/playwright:v1.64.1-noble`（image ID
`a0d7b2b49ffa0e99787a00b7ec2af46eca58020082713f002ca9f0f1cfd1a017`）。runner 不能访问
Docker Hub，必须使用本地 registry 镜像。域名/磁盘/seed 保留：

```bash
/var/lib/libvirt/images/dosh-browser-runner.qcow2
/var/lib/libvirt/boot/dosh-browser-runner-seed.iso
```

操作顺序：

```bash
virsh start dosh-browser-runner
ssh -o BatchMode=yes runner@192.168.122.173 'test -d /opt/dosh'
```

在 runner 内执行 mock 矩阵：

```bash
cd /opt/dosh
DOSH_PLAYWRIGHT_ALLOW_WEBKIT=1 \
DOSH_PLAYWRIGHT_IMAGE=192.168.122.10:5000/dosh/playwright:v1.64.1-noble \
  bash scripts/run-playwright-container.sh --project=webkit --retries=0
```

再执行真实 OIDC 矩阵：

```bash
cd /opt/dosh
DOSH_PLAYWRIGHT_ALLOW_WEBKIT=1 \
DOSH_PLAYWRIGHT_IMAGE=192.168.122.10:5000/dosh/playwright:v1.64.1-noble \
DOSH_PLAYWRIGHT_CONFIG=playwright.oidc.config.ts \
DOSH_PLAYWRIGHT_HOST_NETWORK=1 \
DOSH_E2E_BASE_URL=https://192.168.122.100:32627 \
  bash scripts/run-playwright-container.sh --project=webkit --retries=0
```

验收完成后回宿主停止 VM，保留 domain 以复现：

```bash
virsh destroy dosh-browser-runner
virsh dominfo dosh-browser-runner
```

历史 v2.43 证据：mock WebKit `37/37`，`workers=1`、`retries=0`，隔离容器资源上限
`4g/2 CPU/1g shm/2048 PIDs`，运行目录
`/var/lib/dosh/e2e-runs/20261104T081749Z-v243-webkit-mock/`，日志 SHA-256
`8cc4765a02d01fb1667318570d337a24c9353908147f3ede602f87eb3f56fc9a`。真实 OIDC WebKit 也已在同一
runner 内完成 `16/16`，运行目录
`/var/lib/dosh/e2e-runs/20261104T091633Z-v243-oidc-browser-webkit/`；Chromium/Firefox 为
`/var/lib/dosh/e2e-runs/20261104T091212Z-v243-oidc-browser-host/` 各 `16/16`。v2.44 历史 digest 的
WebKit mock `37/37` 和真实 OIDC `16/16` 已在同一隔离 runner 串行完成，artifact 为
`/var/lib/dosh/e2e-runs/20261104T113343Z-v244-webkit-mock/` 与
`/var/lib/dosh/e2e-runs/20261104T113800Z-v244-oidc-browser-webkit/`；真实 OIDC Chromium/Firefox
各 `16/16` 已由宿主隔离容器串行完成。v2.41 历史证据：mock `35/35`、
真实 OIDC `16/16`，均 `workers=1`、`retries=0`；真实 OIDC
每浏览器 6/6 denied 审计带 `requestId`，Token 扫描 0。历史 v2.33 的 `15/15` 结果仅作追溯，
不继承给后续新 digest。WebKit axe 在
`waitForVisualStability` 等到字体、有限动画和
两帧 rAF 后执行，避免动画期间的低不透明度误报。受控归档：

```text
/var/lib/dosh/evidence/69f30f001651/20261103T213047Z-v231-webkit-final
manifest.sha256 SHA-256=bc8718419fcd6a8f4b5f11135c028ce3bda847f331d7c594f49d1cc14143b25e
```

复核命令：

```bash
cd /var/lib/dosh/evidence/69f30f001651/20261103T213047Z-v231-webkit-final
sha256sum -c manifest.sha256
sha256sum -c manifest-digest.txt
```

### 3.1.1 日志流 NDJSON 隔离验收

```bash
make e2e-log-stream
```

脚本使用独立 namespace、Release、NodePort `32631`、临时 TLS 和本地管理员 Secret，结束后删除所有
夹具。验收内容包括：响应头前的 401/容器错误为 JSON Problem；正常读取最后一条帧为 `end`；HTML/ANSI
字节不因协议转换丢失；删除 Pod 后 follow 流以 `end` 或结构化 `error` 终止；生产 `dosh` 的 revision、
镜像、NodePort 和 Ready 指纹前后一致。原始帧、错误响应、构建日志和摘要保存在
`/var/lib/dosh/e2e-runs/<run>-log-stream/`，不得记录 Cookie、密码或 Session Key。

历史 v2.44 结果：通过，候选 digest `sha256:e4a27d8b…49439`，26 个文件的最终归档位于
`/var/lib/dosh/evidence/e4a27d8b153b/20261104T103215Z-v244-final-log-stream`，manifest SHA-256
`ce20093711d6e5a8f054aca147faa38aa0f6fd98efd73f42010d2a7c99d070cb`；两层 SHA-256 校验通过。
生产仍为 App `0.1.2` / revision 12 / NodePort `32626` / 2/2 Ready。

### 3.2 源码绑定与证据归档

镜像构建完成后、证据归档前生成源码绑定文件：

```bash
bash scripts/create-source-binding.sh \
  --output /var/lib/dosh/e2e-runs/<run>/source-binding.txt \
  --tag <candidate-tag> \
  --digest sha256:<registry-digest> \
  --image-id <podman-image-id> \
  --build-log <build.log> \
  --digest-file <registry.digest>
```

该命令固定 App/Chart/OpenAPI 版本、运行时源码树 SHA-256、`bin/dosh` SHA-256、构建日志和
registry digest；若 digest 文件与参数不一致会失败。归档时必须使用同一 registry digest，
并运行 `sha256sum -c manifest.sha256` 与 `sha256sum -c manifest-digest.txt` 双重校验。

在已启动隔离 OIDC fixture 的环境中，可验证 CronJob 无 selector 详情：

```bash
make e2e-cronjob-detail
```

该目标要求 `dosh-e2e-oidc` Release Ready、测试 IdP 可达；脚本会拒绝生产 namespace 和
`32626`，创建 suspended CronJob，验证 HTTP 200、空 selector、空 Pod 列表，删除后再次确认
NotFound，并输出 evidence 路径。

### 3.3 审计 Webhook 与真实 Loki 实验室验证

审计 Webhook 全矩阵与真实 Loki 写入/检索使用独立 namespace，不接触生产 `dosh`：

```bash
make e2e-audit-loki
```

该目标设置 `DOSH_E2E_AUDIT_BACKEND=loki` 并保留 artifact；也可手工执行：

```bash
DOSH_E2E_AUDIT_BACKEND=loki \
DOSH_E2E_KEEP_ARTIFACTS=1 \
DOSH_E2E_SKIP_BUILD=1 \
DOSH_E2E_IMAGE_TAG=<candidate-tag> \
DOSH_E2E_IMAGE_DIGEST=sha256:<registry-digest> \
DOSH_E2E_ARTIFACT_DIR=/var/lib/dosh/e2e-runs/<run> \
  bash scripts/e2e-audit-webhook.sh
```

实验室 Loki 固定为内部 registry 的 `192.168.122.10:5000/dosh/loki:3.5.4`，验收时必须使用
registry digest 而非可变 tag。脚本通过私 CA TLS、`X-Scope-OrgID` 租户鉴权和
`kubectl port-forward` 完成真实写入与回查，并验证无租户查询 401、Request ID 可检索、
SIGTERM 排空和泄露扫描 0。

v2.44 历史 digest 的通过结果为 59 条事件、34 个 requestId、20/20 SIGTERM 排空、租户隔离、
私 CA、队列压力、超时/503 矩阵和凭据扫描 0；证据纳入
`/var/lib/dosh/evidence/e4a27d8b153b/20261104T112513Z-v244-internal-matrix-r2`。v2.34 的同类
结果只作历史追溯，不继承给当前候选。

限制：当前 Loki 使用 `emptyDir`、临时测试 token 和 24h retention，只证明实验室投递/检索路径；
不能作为企业生产 SIEM，不能证明持久化、告警、访问审批、容量 SLA、长期保留或离机 WORM。
归档前排除 `fixture-certs`、`headers.*`、`body.*`、私钥和凭据文件，归档后必须执行
`sha256sum -c manifest.sha256` 和 `sha256sum -c manifest-digest.txt`。

## 4. 回滚

```bash
helm -n dosh history dosh
helm -n dosh rollback dosh <previous-revision> --wait --timeout 5m
kubectl -n dosh rollout status deployment/dosh --timeout=120s
```

回滚只影响 Helm 管理的 `dosh` 命名空间资源，不删除用户命名空间。

## 5. 管理员密码轮换（仅 local 模式）

```bash
printf '%s' '<new-strong-password>' | ./bin/dosh hash-password > /tmp/admin-password-hash
kubectl -n dosh create secret generic dosh-auth \
  --from-file=admin-password-hash=/tmp/admin-password-hash \
  --from-file=session-key=<(kubectl -n dosh get secret dosh-auth -o jsonpath='{.data.session-key}' | base64 -d) \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n dosh rollout restart deployment/dosh
kubectl -n dosh rollout status deployment/dosh --timeout=120s
```

轮换密码后，旧会话仍然有效，直到 Session Key 同时轮换或会话过期。若需要立即让所有会话失效，同时生成新的 `session-key`，再重启 Deployment。

## 6. Session Key 轮换（Token/本地/ OIDC 模式）

```bash
openssl rand -base64 48 > /tmp/session-key
kubectl -n dosh create secret generic dosh-auth \
  --from-file=session-key=/tmp/session-key \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n dosh rollout restart deployment/dosh
```

Token 模式只需保留 `session-key`；local 模式还需保留 `admin-password-hash`。轮换后所有用户必须重新
登录。不要把生成文件提交到仓库或写入日志。Token 本身没有服务端会话存储，吊销 Kubernetes Token 或
关联身份权限后，已经建立的加密 Cookie 会话仍会持续到 TTL 结束；需要立即失效时轮换 Session Key。

## 7. TLS 证书轮换

TLS 证书只存放在 Kubernetes Secret 中，不使用 ConfigMap。默认 Release 引用同 namespace 下的
`dosh-tls`，Secret 必须包含 `tls.crt` 和 `tls.key`；Chart 将整个 Secret 以 `defaultMode: 0440`
只读挂载到 `/etc/dosh/tls`，并通过 `DOSH_TLS_CERT_FILE`、`DOSH_TLS_KEY_FILE` 指向这两个文件。
Chart 不创建 Secret，也不接受把私钥写进 values；`tls.existingSecret` 用于替换 Secret 名称。

1. 新证书 SAN 必须覆盖实际访问的主机名或 Node IP。
2. 更新 Secret `dosh-tls`，不要输出或转存 Secret 中的私钥。
3. 等待 kubelet 同步挂载内容，再等待 Dosh 的证书轮询周期（默认 `30s`）。
4. 在“配置与密钥 → 服务端证书”核对新证书主题、SAN、到期时间和 SHA-256 指纹；也可以调用
   `GET /api/v1/certificate`。状态中的 `lastReloadError` 非空时，当前进程仍使用上一份可用证书。
5. 使用 `curl -k` 验证自签证书；生产可替换为受信 CA，CA 不是运行前提。HTTP 模式跳过本节。

用户需要 `update` `dosh.io/server-certificates` 才能显式调用
`POST /api/v1/certificate/reload`，操作会写入审计。自动热加载默认开启，Helm 值
`tls.reloadInterval`（环境变量 `DOSH_TLS_RELOAD_INTERVAL`）控制轮询间隔；设为 `0` 表示关闭自动
轮询。轮换失败不会中断现有 TLS 服务，但必须保留失败状态并排查证书/私钥是否匹配、PEM 是否完整。

```bash
kubectl -n dosh create secret tls dosh-tls \
  --cert=/path/to/new/tls.crt --key=/path/to/new/tls.key \
  --dry-run=client -o yaml | kubectl apply -f -

# 查看证书公开信息，不要输出私钥
kubectl -n dosh get secret dosh-tls -o jsonpath='{.data.tls\.crt}' \
  | base64 -d | openssl x509 -noout -subject -issuer -dates -fingerprint -sha256

# 仅在挂载传播或热加载未生效时使用；优先保留自动热加载能力
kubectl -n dosh rollout restart deployment/dosh
```

验证热加载失败回退时，只能使用隔离 namespace/Release 和临时证书；不要在生产 Secret 中写入无效
证书。预期旧证书持续提供 TLS 服务、`status` 仍为上一份证书状态、`lastReloadError` 记录失败原因。

### Cloudflare Full (strict) 源站（仅 preview）

`dosh-preview` 的公网入口为
`Cloudflare → kubedo-dashboard-proxy（主机 Nginx）→ preview NodePort 32630 → dosh-preview`。
Cloudflare Full (strict) 的源站证书安装在前置 Nginx，不使用 Dosh 的 `dosh-tls` Secret。

- Cloudflare Origin CA 证书：`/etc/kubedo/dashboard-proxy/cloudflare/dosh.xuthus.cc.crt`
- 对应私钥：`/etc/kubedo/dashboard-proxy/cloudflare/dosh.xuthus.cc.key`（`0600`）
- Nginx 只读挂载：`/etc/nginx/cloudflare`
- Cloudflare Origin Rule `dosh`：回源端口 `32630`
- SNI 分流：`dosh.xuthus.cc` 使用 Origin CA 证书；无 SNI 的直连继续使用原自签证书

源站证书轮换：

```bash
# 替换证书和私钥后，先校验再重启；不要把私钥输出到终端或日志
podman exec kubedo-dashboard-proxy nginx -t
systemctl restart kubedo-dashboard-proxy.service

openssl s_client -connect 127.0.0.1:32630 -servername dosh.xuthus.cc </dev/null 2>/dev/null \
  | openssl x509 -noout -issuer -dates -ext subjectAltName
curl -sS -o /dev/null -w '%{http_code}\n' https://dosh.xuthus.cc/
```

若把 Origin Rule 改为 `443`，必须先确认上游 IPv6 防火墙已放行 `443`；当前预览生产路径使用已验证
可达的 `32630`。Cloudflare API Token 只通过临时环境变量或交互式输入使用，不写入仓库、脚本、
命令历史或证据文件。生产 `dosh` 的入口和证书不在本节变更范围内。

## 8. 故障排查

### Pod 未 Ready

```bash
kubectl -n dosh describe pod -l app.kubernetes.io/name=dosh
kubectl -n dosh logs -l app.kubernetes.io/name=dosh --prefix --tail=200
```

常见原因：`dosh-auth` 缺失或 key 不正确、HTTPS 模式下 `dosh-tls` 缺失或证书无效、32626 被占用、
镜像无法拉取。

### Token 登录失败或业务 API 403

- 用 `kubectl auth whoami --token=<token>` 或 TokenReview 确认 Token 有效、未过期。
- 确认 Token 对应身份在目标命名空间具备资源权限；dosh 不把 Token 的权限转成 ServiceAccount 权限。
- 检查 Token 模式 ClusterRole 是否包含 `tokenreviews/create`、`subjectaccessreviews/create`、
  `users`、`groups`、`serviceaccounts` 的 `impersonate` 权限。
- 检查登录限速 Lease 是否集中在命名空间 `dosh`，以及 Role/RoleBinding 是否存在。
- 审计日志只应出现用户名、组和 `requestId`，不得出现原始 Token。

### 本地登录失败（local 模式）

- 确认使用 `DOSH_ADMIN_USERNAME` 配置的用户名。
- 确认 Secret 中保存的是 `./bin/dosh hash-password` 生成的 Argon2id 哈希，而不是明文。
- 连续失败 5 次会按来源 IP 限速 5 分钟；等待或清理对应 Pod 内存中的限速状态。

### 403

- 前端能力位来自 `/api/v1/capabilities`；只读模式会关闭写能力。
- 检查 ClusterRole 是否包含目标资源和子资源，例如扩容需要 `deployments/scale`。
- 检查审计日志中的 Kubernetes 原始错误。

### 日志流中断

- 检查 Pod 是否已删除或容器是否重启。
- 检查所选容器名是否正确。
- 接口返回 `application/x-ndjson`。`log` 帧承载日志，正常 EOF 后最后一条为 `end`；连接中失败时最后
  一条为 `error`，包含稳定 `code`、安全 `message` 和 `requestId`，随后终止。
- 连接在未收到 `end` 或 `error` 帧时关闭，前端显示“日志流意外中断（未收到结束帧）”，不会误报为
  正常结束。
- 响应头发送前的认证、Pod/容器不存在等错误仍是标准 JSON Problem，不会伪装成日志行。
- 浏览器会显示断流状态、错误码和请求 ID，用户可点击“重新加载”手动重连；日志内容不会写入
  `localStorage` 或 `sessionStorage`。

排查命令：

```bash
curl -skN -b /path/to/session.cookie \
  'https://<node-ip>:32626/api/v1/pods/<namespace>/<pod>/logs?container=<container>&follow=false' |
  jq -c .
```

### Cilium 策略阻断 Kubernetes API

症状：Pod 启动后探针超时、`/readyz` 返回 503，但 DNS 正常。

```bash
kubectl -n dosh get networkpolicy dosh -o yaml
kubectl -n dosh get ciliumnetworkpolicy dosh-api-server -o wide
kubectl -n dosh describe pod -l app.kubernetes.io/name=dosh
```

预期：`dosh-api-server` 的 `VALID` 为 `True`，出口仅包含 `toEntities: [kube-apiserver]`；标准 NetworkPolicy 在 Cilium 模式下不应再包含 `0.0.0.0/0` API 出口。Chart 会在检测到 `cilium.io/v2` 时自动创建 CNP；若自动探测失败，可显式设置 `networkPolicy.ciliumApiServer.enabled=true` 后重新执行 `helm upgrade --wait`。

## 9. 安全事件响应

1. 立即轮换 `dosh-auth` 中的 Session Key，使全部会话失效。
2. Token 模式立即吊销相关 ServiceAccount/用户 Token 和 RBAC 绑定；local 模式轮换管理员密码；
   HTTPS 模式轮换 TLS 私钥。
3. 导出 `dosh` 命名空间日志，重点检索 `audit_event`、`action=reveal` 和写操作。
4. 检查 ClusterRoleBinding `dosh` 是否被修改。
5. 若确认凭据泄露，撤销 Helm Release 并删除可能被利用的测试资源。

审计基线写 stdout；启用 `DOSH_AUDIT_WEBHOOK_URL` 后还会投递到受 TLS 保护的审计接收端，
支持 Bearer/HMAC、私 CA、有限重试和有界队列。生产仍应接入 Loki、OpenSearch 或 SIEM，
完成保留、检索、告警、访问控制和离机 WORM 归档。
