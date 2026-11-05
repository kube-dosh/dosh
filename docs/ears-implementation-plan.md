# dosh EARS 实施文档

| 项目 | 内容 |
|---|---|
| 文档编号 | DOSH-EARS-001 |
| 版本 | v2.46-dev |
| 日期 | 2026-11-04 |
| 状态 | 实施与验收基线。2026-11-04 复核：生产 `dosh` 仍为 App `0.1.2` / Chart `0.1.4`、revision 12、2/2 Ready、NodePort `32626`，生产未改动。当前候选 `0.1.4-rc-nodeuncordon-20261104T132617Z@sha256:2135c2aea864c25975a172d35c7973c1f15c76db266862cd09f7bc0253307bfd` 修复 Cordon 后 Uncordon 的 `resourceVersion` 409，并完成 `make verify`、Vitest 62/62、全量 race、mock/真实 OIDC 三浏览器、rollback/restore、Metrics 三态、429/5xx retry、Audit/Reveal、真实 Loki、30 分钟 soak、性能/bundle、govulncheck、npm audit 和 Trivy；最终证据归档 599 文件。Edge/Safari 实机、企业 IdP、受信 TLS、企业 SIEM、离机 WORM、CI 真跑、生产发布/回滚及七个外部商用门禁仍未关闭，因此不构成完整商用放行 |
| 目标仓库 | `/root/projects/dosh` |
| 部署命名空间 | `dosh` |
| 默认监听端口 | `32626` |
| 目标集群 | kubedo-lab，Kubernetes `v1.36.5` |
| 访问地址 | `https://192.168.122.100:32626`（当前实验室 NodePort） |
| API 契约 | `api/openapi.yaml` `0.1.4`，44 个路径、59 个 schema；以 `make api-check` 逐字节对账生成类型 |

> 本文档是 dosh 的设计、实施和验收基线，不是“完成度宣传页”。只有同时具备实现位置、
> 可复现命令、真实输出和验收记录的条目，才可以标记为“已验收”。代码已写、测试已写但
> 未执行，只能标记为“已实现”；代码已部署但缺少生产级证据，只能标记为“实验室已验收”。

## 1. 文档目的

本文档把 dosh 的产品目标、技术架构、交互设计、安全约束和交付计划转换为可验证的 EARS 需求，并按阶段说明要完成的工作、产物、验收方法与退出条件。

本文档同时作为实施基线、验收清单和需求追踪入口。截至 2026-11-04 22:50，`0.1.2/0.1.4` 仍在生产命名空间 `dosh`，实测 revision 12、2/2 Ready、NodePort `32626`；当前源码版本为 App `0.1.4` / Chart `0.1.6`。当前候选 `0.1.4-rc-nodeuncordon-20261104T132617Z@sha256:2135c2ae…7bfd` 修复节点 Cordon/Uncordon 并发冲突，并完成 `make verify`、Vitest 62/62、全量 race、mock 三浏览器各 38/38、真实 OIDC 三浏览器各 16/16、rollback/restore `198→201`、Metrics 三态、429/5xx retry、工作负载生命周期、CronJob 详情、Audit/Reveal、真实 Loki、30 分钟 soak、容量 SLO、LCP `716.0 ms`、bundle `350.62 KiB`、govulncheck 0、npm audit 0、Trivy 0 HIGH/CRITICAL 和最终 599 文件归档；详细边界见 21.2.42。v2.45 的历史证据见 21.2.39 至 21.2.41，不能继承。企业 SIEM、Edge/Safari 实机、离机 WORM、企业 IdP、受信 TLS、CI 真跑和生产发布/回滚仍未关闭。未完成或只完成部分自动化的条目必须在第 21 节和风险登记表中如实标记，不得以“已实现”代替“已验收”。 |

### 1.1 文档使用方式

1. 设计或开发前，先确认需求编号、依赖方向和第 4 节架构契约。
2. 每个实施任务必须对应 WBS 编号、文件路径、需求编号和至少一个验收命令。
3. 开发完成后先运行第 15 节自动化门禁，再执行对应阶段的人工验收。
4. 验收结论、命令输出摘要、镜像摘要和 Helm revision 写入第 21 节；没有证据的事项不得勾选。
5. 若实现偏离本文档，先修改文档并记录原因，再修改代码，避免“代码事实”和“设计契约”持续漂移。
6. 后续版本发布时复制第 25.7 节的任务单，逐项填写“实施结果、证据、结论”，不得覆盖历史记录。

### 1.2 当前版本与结论口径

| 对象 | 当前事实 | 结论口径 |
|---|---|---|
| 集群已部署版本 | 2026-11-04 11:20 实测：App `0.1.2`、Chart `0.1.4`、Helm revision 12、2/2 Ready、NodePort `32626`；`/healthz`/`/readyz` 200，未认证 overview 401 | 生产未改动；仍需在维护窗口执行授权发布和滚动升级证据 |
| 已隔离部署候选（历史，不含最新修复） | App 镜像 `192.168.122.10:5000/dosh/dosh:0.1.3-rc-oidc-20261102171324`，registry digest `sha256:0be6df2586a3e54a34c4b8221622a3ccaefaa7224a9e5ed3c718cf5cf0798ca8` | `dosh-e2e-oidc` 最终 revision 75、1/1 Ready、NodePort `32627`；OIDC 浏览器 33/33、18/18 denied 审计、Token 扫描 0、retry、Metrics 三态、审计 Webhook/SIGTERM、rollback/restore、Trivy 0 HIGH/CRITICAL；不得继承给当前源码 |
| 历史完整门禁候选（不覆盖当前源码） | 镜像 `192.168.122.10:5000/dosh/dosh:0.1.3-rc-a11y2-20261103093530`，image ID `sha256:7a109763…11c01f`，registry digest `sha256:45090acfc99e96cbf40d7a2808736c6dbdc043feee3d8a69164917fc0bf680ab`；原隔离 Release revision 89 | `make verify`、`make race`、Vitest 15/15、Trivy 0 HIGH/CRITICAL 通过；mock 45/45、真实 UX 9/9、完整 OIDC 42/42、30 分钟 soak、回滚/恢复、Metrics、retry、动态上下文、审计全矩阵、失败审计、CronJob 和 15 文件归档通过；上述证据不得继承给新 digest |
| 最终隔离候选（v2.31，v2.32 完成 WebKit，v2.33 完成真实 Loki） | 镜像 `192.168.122.10:5000/dosh/dosh:0.1.3-rc-sec-20261104035000`，registry digest `sha256:69f30f0016516ac9b56ff867f23c8c0039810dcff29179420c12da323cc00523`，本地 image ID `06927c828cd8759a0e6a51a4788fe874857de2a0abb92c979b6fda4683057e27`；隔离 Release revision 138、1/1 Ready、NodePort `32627` | 通过源码绑定、本地门禁、mock/真实 OIDC 的 Chromium/Firefox/WebKit 各 15/15、审计、soak、rollback/restore、Metrics、retry、CronJob、真实 Loki 投递/检索、Trivy/govulncheck/npm audit；不得用 image ID 代替 registry digest |
| v2.41 历史候选（WBS-4.8 审计历史，不继承给当前源码） | 镜像 `192.168.122.10:5000/dosh/dosh:0.1.4-rc-audit-20261104T062708Z`，registry digest `sha256:1e16c0fb923b6451c46a676bdf671eef9dcfd21e08a8a093d1193b844b655f94`，image ID `sha256:dc7953cee005287dd4504336776b6ec1b1d01fb9d96075085d68bd8bf28fdaaa`；隔离 UI Release `dosh-ui-e2e` revision 14、2/2 Ready、NodePort `32629`；隔离 OIDC Release `dosh-e2e-oidc/dosh-oidc-e2e` revision 168、1/1 Ready、NodePort `32627`；App/OpenAPI/Web `0.1.4`、Chart `0.1.6` | 当时通过 Vitest 47/47、mock Chromium/Firefox/WebKit 各 35/35、真实本地认证 Chromium/Firefox 各 1/1、真实 OIDC Chromium/Firefox/WebKit 各 16/16、LCP `448.0 ms`、bundle `348.62 KiB`、容量 SLO、race、`make verify`、Trivy 0 HIGH/CRITICAL 和 207 文件源码绑定。新增镜像更新后源码与绑定失配，全部候选证据必须重建 |
| v2.46 当前候选（节点 Cordon/Uncordon 并发修复） | 镜像 `192.168.122.10:5000/dosh/dosh:0.1.4-rc-nodeuncordon-20261104T132617Z`，registry digest `sha256:2135c2aea864c25975a172d35c7973c1f15c76db266862cd09f7bc0253307bfd`，image ID `sha256:7394e1d6a8436c0b854b63b253d7a085a97dc1d364b3a96b462c9a7b352d25a9`；隔离 OIDC Release `dosh-e2e-oidc/dosh-oidc-e2e` 验收时 revision 209、1/1 Ready，后续复核推进至 revision 210，NodePort `32627`；源码绑定 `source-binding-v246-nodeuncordon.txt`（213 文件）；最终证据归档 599 文件 manifest SHA-256 `d761698e13819c4491876a7ee6f905ba37f85d589a895ec494215fab3c6aaff9` | 将节点调度切换改为只 patch `spec.unschedulable`，消除完整 Node Update 与 kubelet 心跳的 resourceVersion 409。当前 digest 通过 `make verify`、Vitest 62/62、全量 race、mock 三浏览器各 38/38、真实 OIDC 三浏览器各 16/16、三身份 API/RBAC、Cordon/Uncordon、rollback/restore `198→199→200→201`、Metrics 三态、429/5xx retry、工作负载生命周期、CronJob 详情、Audit/Reveal、真实 Loki、30 分钟 soak、容量 SLO、LCP 716.0 ms、bundle 350.62 KiB、govulncheck 0、npm audit 0 和 Trivy 0 HIGH/CRITICAL。七个外部门禁及生产发布仍待完成 |
| v2.45 历史候选（Secret 明文解码 + Playwright 资源收紧） | 镜像 `192.168.122.10:5000/dosh/dosh:0.1.4-rc-secretdecode-20261104T120606Z`，registry digest `sha256:c7eff34a80ca1be0de50d12cd27b97d52ca54029329ed7b19e4bcc70c283ab06`，image ID `051c50c635fe655f1943ef7a551a99ed56671bb890383044e042adf6793fedbf`；隔离 OIDC Release `dosh-e2e-oidc/dosh-oidc-e2e` revision 194、1/1 Ready、NodePort `32627`；源码绑定 `source-binding-v245-secretdecode.txt`（213 文件）；主证据归档 33 文件 manifest SHA-256 `e71c466b80159f51022f3addd0bcbc0c69393dc22d3f0b9b8bdf6a951c9964c6`；Metrics 补充归档 4 文件 manifest SHA-256 `d2bae3531ddd533e712e2a6fc9e8fb558db74462d89c3a02d20fcc7f393348b8`；retry 补充归档 5 文件 manifest SHA-256 `27282a7d1986be6cdceaf442f406f230146959bf55e050d071cec7875a95815c` | 修复 Secret 明文 Base64 解码、二进制保留和非法编码回退。当时通过 Vitest 62/62、全量 race、mock 三浏览器各 38/38、真实 OIDC 三浏览器各 16/16、隔离 Audit/Reveal、Metrics 三态、429/5xx retry、容量 SLO、LCP、bundle、govulncheck、npm audit 和 Trivy；其所有证据不得继承给 v2.46 |
| v2.44 历史候选（WBS-4.10 日志流结构化 + 内部商用验收矩阵） | 镜像 `192.168.122.10:5000/dosh/dosh:0.1.4-rc-logframes-20261104T102214Z`，registry digest `sha256:e4a27d8b153b8f96ad52ba5f82f92fa3ddaf22e13bc69432c72d16e8ea449439`，image ID `sha256:1525e8d063388e3f68e5a66a9697c97405527f67fb04fd4cc523a436d76c9f23`；隔离日志 Release `dosh-logs-e2e/dosh-e2e-logs` revision 1、2/2 Ready、NodePort `32631`，验收后已清理；源码绑定 `source-binding-v244.txt`（211 文件） | 新增有界 NDJSON 日志协议、UTF-8 安全拆帧、正常 `end`、流中结构化 `error` 和 `requestId`、响应头前 JSON Problem、前端协议解析/终止状态、无 `end` 断流提示与手动重连。通过 `make verify`、Vitest 56/56、mock Chromium/Firefox/WebKit 各 37/37、真实 OIDC Chromium/Firefox/WebKit 各 16/16、新鲜 race、容量 SLO、LCP、bundle、govulncheck 0、生产 npm 审计 0、Trivy 0 HIGH/CRITICAL、rollback/restore、Metrics、retry、Loki、soak、工作负载生命周期和 CronJob 详情；综合归档 553 文件，WebKit 补充归档 11 文件。该结果不继承给 v2.45 |
| v2.43 历史候选（WBS-5.1 写操作闭环 + WBS-5.8 镜像更新 + WBS-5.9 CronJob 暂停/恢复，不继承给 v2.44） | 镜像 `192.168.122.10:5000/dosh/dosh:0.1.4-rc-cronjobsuspend-20261104T075420Z`，registry digest `sha256:779ecae706eab6d0645e7b486aac1923f07e79e4919a969307cc6eb0d5c33306`，image ID `sha256:4472383799e341e6b5065cfbd236214f89fbcb062ad6582cb02ea7c8b14c5c71`；隔离 UI Release `dosh-ui-e2e` revision 15、2/2 Ready、NodePort `32629`；隔离 OIDC Release revision 173、1/1 Ready、NodePort `32627` | 当时新增 Deployment/StatefulSet/DaemonSet 容器镜像更新、CronJob 暂停/恢复、完整 Deployment 预览→创建→扩容→镜像更新→重启→删除闭环、资源版本并发保护、`update-image`/`suspend`/`resume` 审计、独立 `updateImage`/`suspendCronJob` capability、OpenAPI/TypeScript 契约与确认弹窗。Vitest 51/51、mock Chromium/Firefox/WebKit 各 37/37、真实 OIDC Chromium/Firefox/WebKit 各 16/16、新鲜 race、容量 SLO、LCP 692 ms、隔离 revision `169→172` rollback/restore、30 分钟 soak、Metrics 三态、429/5xx retry、真实 Loki、真实隔离集群镜像更新/暂停恢复和写操作闭环审计矩阵、585 文件主归档及 OIDC（10）、Metrics/retry（20）、Loki（9）、workload lifecycle（28）补充归档通过；源码绑定 `source-binding-v243.txt` 已生成。上述证据绑定旧 digest，不继承给 v2.44 |
| v2.40 前一候选（WBS-4.7 日志安全，历史，不继承） | 镜像 `192.168.122.10:5000/dosh/dosh:0.1.4-rc-logsec-20261104T045837Z`，registry digest `sha256:f866e3ba3448a82feb8fb6a66a77da713ff41e79f90889979024733ed2cacaaf`，image ID `sha256:ffca2f634d1f7bf2d54535ea3341e44b936fd66518b08907bd13765b64d3a26d`；隔离 UI Release `dosh-ui-e2e` revision 13、2/2 Ready、NodePort `32629`；隔离 OIDC Release `dosh-e2e-oidc/dosh-oidc-e2e` revision 165、1/1 Ready、NodePort `32627`；App/OpenAPI/Web `0.1.4`、Chart `0.1.6` | 当时通过 Vitest 46/46、mock Chromium/Firefox 各 34/34、真实本地认证 Chromium/Firefox 各 1/1、真实 OIDC Chromium/Firefox 各 15/15、隔离 WebKit mock 34/34 与真实 OIDC 15/15、LCP `484.0 ms`、Metrics 三态、429/5xx retry、真实 Loki、rollback/restore `161→164`、30 分钟 soak、Trivy 0 HIGH/CRITICAL、bundle 347.40 KiB 和源码绑定；不得继承给 v2.41 |
| v2.39 前一候选（可访问性修复，历史，不继承） | 镜像 `192.168.122.10:5000/dosh/dosh:0.1.4-rc-a11y-20261104T041000Z`，registry digest `sha256:c62d2d9773839a8b2100f85aef0ac98a2f7df4231e09e9b3ef8d00ef2fbadb61`，image ID `sha256:b63edaeddd2ddbc69b3d1750d35682f28c9d0c388c11006608532f6ab091bd11`；隔离 UI Release `dosh-ui-e2e` revision 12、2/2 Ready、NodePort `32629`；App/OpenAPI/Web `0.1.4`、Chart `0.1.6` | 当时修复危险状态文字对比度（`4.13:1 → >=4.5:1`），通过 Vitest 37/37、真实本地认证 Chromium/Firefox 各 1/1、真实 OIDC Chromium/Firefox 各 15/15、负向审计 12/12、rollback/restore `152→155` 和 30 分钟 soak。其证据不得继承给 v2.40 |
| v2.38 前一候选（命名空间详情，历史，不继承） | 镜像 `192.168.122.10:5000/dosh/dosh:0.1.4-rc-namespacedetail-20261104T034542Z`，registry digest `sha256:1387225664a7927fb872f68b988ec3fe731f8b80de62737d2db880e93322e2e5`，image ID `sha256:11bd8e488375566dd3babafbd670fd1f65e97c82ede581aee225dcefe9cbc77f`；隔离 Release `dosh-ui-e2e` revision 11、2/2 Ready、NodePort `32629`；App/OpenAPI/Web `0.1.4`、Chart `0.1.6` | 当时通过 Go 全量测试、race、Vitest 36/36、mock Chromium/Firefox 各 32/32、真实本地认证 Chromium/Firefox 各 1/1、临时命名空间真实配额/限制/YAML 对账、容量 SLO、Trivy/govulncheck/npm audit、Helm/API/架构/Playwright 隔离/E2E digest 门禁和源码绑定；完成命名空间详情、ResourceQuota、LimitRange、事件/YAML 深链和 local 模式最小 RBAC。其证据不得继承给 v2.39 |
| v2.37 前一候选（存储详情，历史，不继承） | 镜像 `192.168.122.10:5000/dosh/dosh:0.1.4-rc-storagedetail-20261104T030457Z`，registry digest `sha256:d75a7eaf2159f8cfeb74b97e288d60ebb572ab02e72f717af75caa73852d5b51`，image ID `1d7eaac0c6b3705fbd05665abee4f90858480aaf2684e95c5d3dadb6f4054484`；隔离 Release `dosh-ui-e2e` revision 9、2/2 Ready、NodePort `32629`；App/OpenAPI/Web `0.1.4`、Chart `0.1.6` | 当时通过 Go 全量测试、race、Vitest 36/36、mock Chromium/Firefox 各 31/31、真实本地认证 Chromium/Firefox 各 1/1、容量 SLO、Trivy/govulncheck/npm audit、隔离 API 对账、Helm/API/架构/Playwright 隔离/E2E digest 门禁和源码绑定；完成 StorageClass/PV/PVC 详情、容量、卷模式、CSI、绑定关系、PVC conditions、事件与 YAML 深链。其证据不得继承给 v2.38 |
| v2.36 前一候选（网络详情，历史，不继承） | 镜像 `192.168.122.10:5000/dosh/dosh:0.1.4-rc-netdetail-20261104T024228Z`，registry digest `sha256:dde520b65d66cbe45a2abdd085bdfd9037cb5457b6138ee8861912dbf22e0f95`，image ID `e9d1e15d5f3f0f4e97eaaaa8b1f5bf3ac33bb5ff0323393ea0749d885fa915c9`；隔离 Release `dosh-ui-e2e` revision 8、2/2 Ready、NodePort `32629`；App/OpenAPI/Web `0.1.4`、Chart `0.1.6` | 该候选完成 Service/Ingress 详情、EndpointSlice、默认后端、TLS Secret、路由目标状态、事件/YAML 深链，并通过当轮门禁；其证据不得继承给 v2.37 |
| v2.35 前一候选（历史，不继承） | 镜像 `192.168.122.10:5000/dosh/dosh:0.1.4-rc-nodetabs-20261104T021724Z`，registry digest `sha256:844bd19fceebc9cc249ce7bf12772b3ea13b15b1204c05277adcee6df475b744`，image ID `a1f7e71dbd532ae78f6eee24eb467c1f8b57dd99195d09832d71da3fbeeab1a7`；隔离 Release `dosh-ui-e2e` revision 7、2/2 Ready、NodePort `32629`；App/OpenAPI/Web `0.1.4`、Chart `0.1.6` | 通过 Go 全量测试、race、Vitest 31/31、mock Chromium/Firefox 各 26/26、真实本地认证 Chromium/Firefox 各 1/1、容量 SLO、Trivy/govulncheck/npm audit、隔离 API 对账、Helm/API/架构/Playwright 隔离/E2E digest 门禁和源码绑定；完成 IngressClass、Ingress 目标状态、日志上限、列表元数据、节点/工作负载/Pod 五标签详情、事件筛选/UID/退避重连。OIDC、soak、回滚、LCP、WebKit 未在本 digest 复跑；不得继承给 v2.36 |
| v2.34 节点运维候选（历史，不继承） | 镜像 `192.168.122.10:5000/dosh/dosh:0.1.4-rc-node-20261103T223506Z`，registry digest `sha256:0c61ce34873471d85e9d47b957be81de1a970eb166151477468a197045bc5bda`；隔离 Release revision 149、1/1 Ready、NodePort `32627`；App/OpenAPI/Web `0.1.4`、Chart `0.1.6` | 新增节点 Cordon/Uncordon；其 `make verify`、race、Vitest 18/18、三浏览器 mock/真实 OIDC、soak、rollback、Metrics、Loki、LCP 和归档结果不得继承给 v2.35 |
| 历史隔离候选（不覆盖当前源码） | 镜像 `192.168.122.10:5000/dosh/dosh:0.1.3-rc-origin-20261103170401`，registry digest `sha256:03ac9628fe8b19dde1ed7a950de63db9795ec5fc04db6b66b62779f5fe30000c`；隔离 Release revision 114、1/1 Ready、NodePort `32627` | 该 digest 的浏览器、soak、rollback 和归档只保留历史追溯，不继承给 `69f30f00…00523` |
| 浏览器测试基础设施 | `scripts/run-playwright-container.sh`、`scripts/create-source-binding.sh`、`make playwright-check`；单 worker、`retries=0`、每浏览器独立容器、默认 `4g` 内存 / `2` CPU（硬上限 `6g` / `4` CPU），私有 `1g` shm、`2048` PID、20 分钟硬超时、禁止宿主 IPC、宿主级 `flock` 禁止重叠运行，并拒绝并发/重试覆盖；performance 项目也必须经 runner 且仅允许 performance 配置 | v2.46 当前候选已完成 mock Chromium/Firefox/WebKit 各 38/38，以及真实 OIDC Chromium/Firefox/WebKit 各 16/16；WebKit 在隔离 runner 串行执行。v2.45 及更早结果不能继承。所有配置均为 `workers=1`、`retries=0` |
| 前一完整门禁候选（仅历史参考） | App 镜像 `192.168.122.10:5000/dosh/dosh:0.1.3-rc-20261102153511`，digest `sha256:47983a7aedf46d683f56982b0f352a684ae81d126b3390eada1b9bae0ba4312e` | retry、Metrics、30 分钟 soak、审计 Webhook/SIGTERM、rollback/restore 已通过；不得替换当前候选证据，v2.45 未复跑这些专项 |
| 历史候选（不代表当前源码） | `0.1.3-rc-1793626263`、`0.1.3-oidc-e2e-1793622219` 等早期镜像及其 digest；仅用于历史回归参考 | 不得用于当前候选放行；审计 CA/Token、动态集群上下文和 Helm digest 门禁都晚于这些镜像 |
| Deployment 创建向导 | domain DTO、app 校验/授权/审计、API Server Dry Run、YAML 预览、三步前端向导、409 `already_exists` 已实现 | v2.46 当前候选的 mock Chromium/Firefox/WebKit 各 38/38、真实 OIDC 各 16/16 和专用工作负载生命周期均包含向导创建、扩容、更新、重启和删除 |
| 节点运维 | `POST /nodes/{name}/cordon`、`POST /nodes/{name}/uncordon`、`nodeCordon` capability、详情页二次确认、空操作幂等和条件写权限已实现 | 新候选在隔离 Release 通过 viewer/operator 403、admin 200、kubectl `unschedulable` 对账、恢复调度、审计 `denied/success` 和 requestId；生产节点未被测试 |
| API 契约增量 | Deployment 创建、节点调度、事件对象过滤、列表元数据、存储详情、命名空间配额/限制详情、审计历史、镜像更新和 CronJob 暂停/恢复已写入 OpenAPI，生成的 TS 类型与契约同步 | 当前 App/OpenAPI 为 `0.1.4`，44 paths / 59 schemas；`make api-check` 已通过 |
| 架构与路由护栏 | `make arch` 已规则化检查依赖方向、跨模块 import、Router 创建位置、`init()` 路由、proxy/通配路由和环境变量访问；`TestRouteInventoryMatchesOpenAPI` 与 `TestModuleRoutesAreScoped` 已通过 | 本地架构门禁已验收；CI 真跑待完成 |
| 动态集群上下文 | 顶部栏、侧栏、关于弹窗和总览面包屑统一读取 `/api/v1/overview`；Healthy/Degraded/Unavailable/请求失败均有文本与颜色状态 | v2.45 当前候选 mock Chromium/Firefox/WebKit 各 38/38、真实 OIDC 各 16/16；v2.41 的 35/35 和 v2.44 的 37/37 只作历史追溯 |
| OIDC + impersonation | OIDC Authorization Code + PKCE、SAR、impersonation、动态 capabilities、RP logout 和 Helm 双模式已实现 | v2.46 当前候选通过三身份 API/RBAC、Deployment 创建浏览器路径、Chromium/Firefox/WebKit 真实 OIDC 各 16/16、6/6 denied 审计和 Token 扫描，并在当前 digest 复跑 rollback/restore；企业 IdP 未验收 |
| OIDC 浏览器负向矩阵 | `DOSH_OIDC_STATE_TTL`、state Cookie 缺失/篡改/不匹配/过期、nonce mismatch、code 重放、开放重定向均已覆盖 | v2.46 当前候选真实 OIDC Chromium/Firefox/WebKit 各 16/16，七类负向登录全通过；每浏览器 6/6 denied 审计带 `requestId`；企业 IdP 待验收 |
| 多用户身份 | OIDC Authorization Code + PKCE、state/nonce/verifier、真实 RSA/JWKS 校验、动态 capabilities、Kubernetes impersonation、SubjectAccessReview 和 RP logout 已实现 | 签名测试 IdP 的 API/RBAC、浏览器、Token 扫描与 logout 矩阵已通过（实验室已验收）；企业 IdP 和生产发布未验收 |
| Metrics 实际利用率 | `metrics.k8s.io` 适配、DTO、页面和 fake client 测试已实现；Metrics Server `v0.9.1` 安装在 `dosh-metrics`，APIService `v1beta1.metrics.k8s.io` 为 Available；可用/不可用/恢复三态绑定 v2.45 当前 digest | v2.45 当前候选实验室已验收：可用时 CPU `2152m`、内存 `11501375488` bytes，禁用时 `metricsAvailable=false` 且无陈旧值，恢复后 CPU `2141m`、内存 `11518554112` bytes；目标生产 Metrics 拓扑待验收 |
| Kubernetes 429/5xx 重试 | `internal/platform/kube/retry.go` 对 `GET/HEAD/OPTIONS` 的 429/500/502/503/504 和可重试网络错误最多尝试 3 次；429/500/502/503/504、持续 429、`Retry-After`、连接重置、客户端取消和恢复矩阵绑定 v2.45 当前 digest | v2.45 当前候选实验室已验收：五类故障各注入 2 次后恢复 200，持续 429 恰好 3 次并返回 `rate_limited`，`Retry-After` 约 2099 ms；写请求仅 1 次且副本数保持 `2→2`；生产故障注入待复验 |
| 列表筛选 URL 持久化 | 已实现 `q`、`namespace`、`kind` 的路由 search schema、页面回填和 E2E 用例 | v2.45 当前候选 mock Chromium/Firefox/WebKit 各 38/38、真实 OIDC 各 16/16，均包含刷新、分享、前进/后退和空参数清理 |
| 暗色主题与可访问性 | `.dark` 语义 token、系统偏好/持久化、AppShell 切换入口；axe 覆盖登录和核心运维页；新增真实 Release 主题/axe/键盘套件、共享表格键盘回归和浅色 muted 对比度修复 | v2.45 当前候选 mock Chromium/Firefox/WebKit 各 38/38、真实 OIDC 各 16/16（含主题/axe/键盘），axe 0 serious/critical；WebKit 由隔离 runner 执行 |
| 工作负载失败审计 | 扩容/重启/删除的无效 kind、不支持操作、非法删除策略均在 app 层记录失败审计；`SEC-U-004` 和 `WL-N-005` 已落到单测 | 当前候选隔离 Release 已验收：无效 Pod scale、非法 DaemonSet scale、非法 Job restart、非法 delete propagation 均产生 failure 审计，目标 Deployment 不变；单测断言 Repository 写调用为 0 |
| CronJob 详情健壮性 | `GetWorkload` 对无 selector 的 CronJob 安全返回空 selector 和空 Pod 列表，不再解引用 nil；`WL-N-006` 已落到五种工作负载详情测试；`scripts/e2e-cronjob-detail.sh` 负责真实集群创建、校验和清理 | v2.34 历史 digest 在隔离 Release revision 149 通过：HTTP 200、`selector={}`、`pods=[]`、`status=Suspended`，资源已删除；v2.45 未重跑专用 CronJob 详情矩阵 |
| 审计远程投递 | stdout + 可选 Webhook；支持 Bearer-only、HMAC-only、两者并用、私 CA（`DOSH_AUDIT_WEBHOOK_CA_FILE`）、TLS 校验、有限重试和有界队列；`tools/auditreceiver` 支持故障注入和可选 Loki push；Cilium 精确出口已实现；`scripts/e2e-audit-webhook.sh` 已建立隔离矩阵 | v2.45 当前候选通过隔离 Audit/Reveal：私 CA、双认证、业务成功/拒绝、失败审计、瞬时/持续 503、超时恢复、队列压力、只读拒绝、20/20 SIGTERM 排空，token/hmac/secret 泄露扫描均为 0。真实 Loki 59 条事件/34 个 requestId 与租户隔离是 v2.44 历史证据，v2.45 未复跑；企业生产 SIEM 未验收 |
| 受控证据归档 | `scripts/archive-evidence.sh` 按 candidate digest 归档，过滤凭据材料；`scripts/test-archive-evidence.sh` 覆盖正负向；`summary.txt` 被 manifest 覆盖，manifest 自身摘要独立保存 | v2.45 当前候选主归档 33 文件、2,837,535 字节，manifest SHA-256 `e71c466b80159f51022f3addd0bcbc0c69393dc22d3f0b9b8bdf6a951c9964c6`；Metrics 补充归档 4 文件 manifest `d2bae3531ddd533e712e2a6fc9e8fb558db74462d89c3a02d20fcc7f393348b8`；retry 补充归档 5 文件 manifest `27282a7d1986be6cdceaf442f406f230146959bf55e050d071cec7875a95815c`；v2.44 历史综合归档 553 文件和 WebKit 补充归档 11 文件只作追溯；均通过双层校验，敏感 fixture/header/body/证书未进入归档；离机 WORM 未完成 |
| CI 与版本冻结 | `.github/workflows/ci.yml` 已建立 8 个 job（含 actionlint/Playwright 隔离/E2E digest/证据归档 policy）；新增 `scripts/check-version.sh` 并在 `make verify`/CI 调用，Helm 支持 `image.digest` 且拒绝非 64 位 hex digest | 本地版本一致性和 Helm 正/负向渲染已通过；GitHub Actions 尚未实际运行 |
| 生产就绪 | 生产命名空间仍为本地单管理员、自签 TLS；OIDC 负向矩阵、Metrics、retry、soak、审计/SIGTERM、真实 Loki、回滚和动态上下文仅在隔离命名空间验证；企业 IdP、企业 SIEM、CI 和生产发布未完成 | 未达到完整可商用 |

> 口径提示：本表的当前状态以 v2.45 为准；明确标注“v2.44 历史”的行只表示功能曾在旧 digest 通过，
> 不得继承给 v2.45。v2.45 的实际复跑范围见 21.2.39 至 21.2.41：Loki、soak、工作负载生命周期
> 和 rollback/restore 未在 v2.45 复跑。

### 1.3 阅读与执行路径

| 读者/阶段 | 先读 | 再读 | 输出 |
|---|---|---|---|
| 架构评审 | 第 2、4 节 | 4.7、4.8、22.1–22.5 | 依赖边界、chi ADR、OIDC/授权端口、模块设计结论 |
| 功能开发 | 第 5–9、13 节 | 第 14、22 节 | 每条需求的实现位置、测试和验收命令 |
| 发布负责人 | 第 15、17、19 节 | 第 21、23、25 节 | 门禁记录、镜像 digest、Helm revision、回滚证据 |
| 安全/合规 | 第 11、18 节 | 22.10、22.12、22.14、26.1、26.2、26.6 | 威胁处置、审计投递、TLS/OIDC/RBAC 放行证据 |
| 当前迭代执行 | 1.2、1.5、1.6、21.2.24、22.12–22.14 | 阶段 8、15.9、15.10、24、25.10、26.3、26.4、26.6 | 最终候选的源码绑定、Playwright 主机隔离、OIDC/Metrics/retry/soak/审计/回滚和逐步骤验收记录 |
| 商用决策 | 21.4、25.3、25.4 | 第 26 节 | 关闭项、风险接受人、是否允许生产试点 |

### 1.4 修订说明

**v2.41 修订摘要**

- 完成 WBS-4.8 审计历史查询：`internal/platform/audit/history.go` 提供当前实例最近 1000 条的有界
  内存环形缓冲，`GET /api/v1/audit-events` 支持命名空间、动作、结果、关键词、分页和排序；管理员
  可见，非管理员 403，未认证 401，非法 `result` 返回 400。
- 前端新增审计历史页、筛选/分页/排序、详情弹窗和导航入口；OpenAPI 增至 42 个路径、58 个 schema，
  MSW/Vitest 与浏览器 E2E 覆盖真实权限和查询语义。
- 当前候选 `0.1.4-rc-audit-20261104T062708Z@sha256:1e16c0fb…5f94` 已绑定隔离 UI Release
  revision 14、2/2 Ready、NodePort `32629`；隔离 OIDC Release revision 168、1/1 Ready、
  NodePort `32627`。
- 通过 Vitest `47/47`、mock Chromium/Firefox/WebKit 各 `35/35`、真实本地认证 Chromium/Firefox
  各 `1/1`、真实 OIDC Chromium/Firefox/WebKit 各 `16/16`、每浏览器 6/6 denied 审计带 requestId、
  Token 扫描 0、LCP `448.0 ms`、bundle `348.62 KiB`、容量 SLO、Trivy Debian/Go
  `0 HIGH/CRITICAL`、govulncheck 0、npm 生产依赖 0 和 207 文件源码绑定。
- Playwright 继续强制单 worker、`retries=0`、每浏览器独立受限容器、`6g/4 CPU/1g shm/2048 PIDs`、
  宿主级串行锁并拒绝并发/重试覆盖；WebKit 只在隔离 libvirt runner 执行。
- 内存审计历史不是持久审计系统，重启或副本切换会清空/分叉。v2.40 的 Metrics/retry/Loki/soak/
  rollback 证据不继承给 v2.41；企业 IdP、受信 TLS、企业 SIEM、Edge/Safari 实机、CI 真跑、离机
  WORM、生产发布/回滚和七个外部商用门禁仍未关闭。
- 生产 `dosh` 保持 App `0.1.2` / Chart `0.1.4` / revision 12 / NodePort `32626`，未改动。

**v2.40 修订摘要**

- 完成 WBS-4.7 日志与事件安全回归：`web/src/features/workloads/log-sanitizer.ts` 使用增量状态机
  移除 ANSI CSI、OSC、DCS、C0/C1、DEL 和双向显示控制字符，保留换行与制表符，并正确处理跨网络
  chunk 的转义序列；日志仍然只通过 `<pre>` 文本节点渲染。
- 新增 `log-sanitizer.test.ts`、`log-viewer.test.tsx` 和浏览器 E2E：覆盖跨 chunk CSI/OSC/DCS、
  HTML 注入、`javascript:` OSC 链接、日志不写入 `localStorage`/`sessionStorage`、事件消息不创建
  可执行 DOM；Vitest 增至 `46/46`，mock Chromium/Firefox 各 `34/34`。
- 当前候选 `0.1.4-rc-logsec-20261104T045837Z@sha256:f866e3ba…caaf` 已绑定隔离 UI Release
  revision 13、2/2 Ready、NodePort `32629`；`/healthz`、`/readyz` 200，未认证 overview 401。
- 通过 Trivy Debian/Go `0 HIGH/CRITICAL`、bundle `347.40 KiB`、`make playwright-check` 和源码绑定；
  `make verify`、`make race`、容量 SLO、govulncheck 和 npm 生产审计通过；Playwright 保持单 worker、
  `retries=0`、每浏览器独立受限容器和宿主级串行锁。
- 生产 `dosh` 保持 App `0.1.2` / Chart `0.1.4` / revision 12 / NodePort `32626`，未改动。
- 未在本 digest 复跑真实本地认证、真实 OIDC、回滚/soak、LCP、WebKit、Edge/Safari 实机、企业
  IdP、受信 TLS、CI 真跑/分支保护、离机 WORM、生产发布和生产回滚。

**v2.39 修订摘要（历史）**

- 复现工作负载详情 `Failed` 徽标在亮色主题下仅 `4.13:1` 的 WCAG AA 缺陷；新增亮色
  `--destructive-strong: 0 72% 42%` 与暗色 `0 76% 80%`，仅危险状态文字使用该 token，危险实色
  按钮继续使用原 `--destructive`，避免改变按钮白字对比度。
- 同步 `badge`、状态反馈、指标卡、登录页和 Deployment 创建确认；新增
  `web/src/components/feedback/status-badge.test.tsx`，Vitest 增至 `37/37`。
- 当时候选 `0.1.4-rc-a11y-20261104T041000Z@sha256:c62d2d97…adb61` 已绑定隔离 UI Release
  revision 12、2/2 Ready、NodePort `32629`；真实 OIDC Chromium/Firefox 各 `15/15`、
  负向审计 `12/12`、Token 扫描 0、rollback/restore `152→155`。
- 通过真实本地认证 Chromium/Firefox 各 `1/1`、关键详情 API `4/4`、mock Chromium/Firefox 各
  `32/32`、bundle `347.10 KiB`、Trivy/govulncheck/npm audit、源码绑定和最终本地门禁。
- 生产 `dosh` 保持 App `0.1.2` / Chart `0.1.4` / revision 12 / NodePort `32626`，未改动。
- 未在本 digest 复跑 WebKit、LCP、Metrics 三态、retry、真实 Loki/SIEM、Edge/Safari 实机、
  企业 IdP、受信 TLS、CI 真跑/分支保护、离机 WORM、生产发布和生产回滚。

**v2.38 修订摘要（历史）**

- 新增命名空间详情：概览展示状态、标签、注解、创建时间和资源治理摘要；ResourceQuota 展示
  hard/used，LimitRange 展示 min/max/default/defaultRequest；提供事件与 YAML 深链。
- `internal/domain/types.go` 新增 `NamespaceDetail`、`ResourceQuota`、`LimitRange` 和
  `LimitRangeItem`；`internal/platform/kube/nodes.go` 聚合 Namespace、ResourceQuota 和 LimitRange；
  `internal/modules/nodes/{app,http}` 保持既有路由与依赖方向；OpenAPI 与 TS 类型同步。
- 修复 local 模式最小 RBAC：仅在 local ClusterRole 中增加 `resourcequotas`、`limitranges` 的
  `get/list/watch`；OIDC 模式继续使用 impersonation/SAR，不增加直接业务资源权限。
- Playwright 统一强制 `workers=1`、`retries=0`，runner 拒绝 `--workers`、`--fully-parallel` 和正数
  `--retries`；宿主级 `flock`、每浏览器独立容器、私有 IPC 和资源上限保持不变。
- 当时候选 `0.1.4-rc-namespacedetail-20261104T034542Z@sha256:13872256…e2e5` 已绑定隔离 Release
  revision 11、2/2 Ready、NodePort `32629`；生产 `dosh` 保持 App `0.1.2` / Chart `0.1.4` /
  revision 12 / 2/2 / NodePort `32626`，未改动。
- 本轮通过 `go test ./...`、`go test -race ./...`、`make arch`、`make api-check`、`make helm-check`、
  `make playwright-check`、Vitest `36/36`、mock Chromium/Firefox 各 `32/32`、真实本地认证
  Chromium/Firefox 各 `1/1`、临时命名空间真实配额/限制/YAML 对账、容量 SLO、bundle `347.09 KiB`、
  Trivy Debian/Go 0 HIGH/CRITICAL、govulncheck 0 和公共 npm registry 0 vulnerabilities。
- 未在本 digest 复跑 OIDC 浏览器矩阵、WebKit、30 分钟 soak、rollback/restore、生产 LCP、企业 IdP、
  受信 TLS、企业 SIEM、Edge/Safari 实机、CI 真跑、离机 WORM、生产发布和生产回滚；因此本轮只
  证明当前候选在实验室隔离环境的功能与本地/浏览器门禁，不构成完整商用放行。

**v2.37 修订摘要（历史）**

- 新增 `GET /api/v1/storageclasses/{name}`、`GET /api/v1/persistentvolumes/{name}` 和
  `GET /api/v1/persistentvolumeclaims/{namespace}/{name}`，保持 `http -> app -> domain -> kube
  adapter` 依赖方向；详情覆盖 StorageClass 参数/回收策略/卷绑定/扩容、PV 容量/访问模式/卷模式/
  CSI/claim 引用、PVC 申请/实际容量/绑定 PV/volumeMode/conditions。
- 新增 StorageClass/PV/PVC 详情页，统一概览、元数据、事件和 YAML 标签；列表名称以及 PVC 的
  StorageClass/PV、PV 的 claim 关联可进入详情；路由、OpenAPI 和生成的 TS 类型同步。
- v2.37 当时候选 `0.1.4-rc-storagedetail-20261104T030457Z@sha256:d75a7eaf…5b51` 已推送并绑定隔离
  Release `dosh-ui-e2e/dosh-ui-e2e` revision 9、2/2 Ready、NodePort `32629`；生产 `dosh` 保持
  App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 / NodePort `32626`，未改动。
- 本轮通过 `go test ./...`、`go test -race ./...`、`make arch`、`make api-check`、`make helm-check`、
  `make playwright-check`、Vitest `36/36`、mock Chromium/Firefox 各 `31/31`、真实本地认证
  Chromium/Firefox 各 `1/1`、容量 SLO、bundle `345.99 KiB`、Trivy Debian/Go 0 HIGH/CRITICAL、
  govulncheck 0 和公共 npm registry 0 vulnerabilities；隔离 API 用临时 StorageClass/PV/PVC 验证
  详情、YAML、容量、绑定与 conditions，清理后均返回 404。证据见 21.2.30。
- 未在本 digest 复跑 OIDC 浏览器矩阵、WebKit、30 分钟 soak、rollback/restore、生产 LCP、企业 IdP、
  受信 TLS、企业 SIEM、Edge/Safari 实机、CI 真跑、离机 WORM、生产发布和生产回滚；因此本轮只
  证明当前候选在实验室隔离环境的功能与本地/浏览器门禁，不构成完整商用放行。

**v2.36 修订摘要（历史）**

- 新增 `GET /api/v1/services/{namespace}/{name}` 与 `GET /api/v1/ingresses/{namespace}/{name}`，
  保持 `http -> app -> domain -> kube adapter` 依赖方向；Service 详情包含 EndpointSlice 地址、就绪
  状态、目标 Pod/节点、Endpoint 计数、SessionAffinity、IP 族和负载均衡配置，Ingress 详情包含
  路由目标状态、默认后端、TLS Secret、标签和注解。
- 新增 Service/Ingress 详情页与 URL 深链；支持概览、Endpoint/路由、事件、YAML 标签，列表名称
  可进入详情，筛选返回上下文保留。事件按 `Service`/`Ingress` Kind 和资源名精确过滤。
- 当前候选 `0.1.4-rc-netdetail-20261104T024228Z@sha256:dde520b6…0f95` 已推送并绑定隔离 Release
  `dosh-ui-e2e/dosh-ui-e2e` revision 8、2/2 Ready、NodePort `32629`；生产 `dosh` 保持 App `0.1.2`
  / Chart `0.1.4` / revision 12 / 2/2 / NodePort `32626`，未改动。
- 本轮通过 `go test ./...`、`go test -race ./...`、`make arch`、`make api-check`、`make helm-check`、
  `make playwright-check`、Vitest `33/33`、mock Chromium/Firefox 各 `28/28`、真实本地认证
  Chromium/Firefox 各 `1/1`、容量 SLO、bundle `344.69 KiB`、Trivy Debian/Go 0 HIGH/CRITICAL、
  govulncheck 0 和公共 npm registry 0 vulnerabilities；隔离 API 验证 Service 2 个就绪 Endpoint、
  Ingress 默认后端 `Ready`、TLS Secret 与 Service/Ingress YAML。证据见 21.2.29。
- 未在本 digest 复跑 OIDC 浏览器矩阵、WebKit、30 分钟 soak、rollback/restore、生产 LCP、企业 IdP、
  受信 TLS、企业 SIEM、Edge/Safari 实机、CI 真跑、离机 WORM、生产发布和生产回滚；因此本轮只
  证明当前候选在实验室隔离环境的功能与本地/浏览器门禁，不构成完整商用放行。

**v2.35 修订摘要（历史）**

- 在 `web/src/features/nodes/node-detail-page.tsx` 将节点详情统一为“概览、实例、事件、日志、YAML”
  五标签，并将 Pod 日志选择器抽到 `web/src/features/workloads/pod-logs.tsx`，供节点与工作负载详情
  复用；标签状态写入 URL，支持刷新、分享和前进/后退恢复。节点事件按 `objectKind=Node` 和
  `objectName=<node>` 精确过滤，日志仅展示该节点上的 Pod。
- 保留本轮已完成的功能补齐：IngressClass 空状态依据、Ingress 目标状态、非流式日志 1 MiB 上限、
  列表分页/排序/筛选元数据、工作负载/Pod 五标签详情、资源级事件筛选、事件 UID 去重与指数退避重连。
- 当前候选 `0.1.4-rc-nodetabs-20261104T021724Z@sha256:844bd19f…b744` 已推送并绑定隔离 Release
  `dosh-ui-e2e/dosh-ui-e2e` revision 7、2/2 Ready、NodePort `32629`；生产 `dosh` 保持 App `0.1.2`
  / Chart `0.1.4` / revision 12 / 2/2 / NodePort `32626`，未改动。
- 本轮通过 `go test ./...`、`go test -race ./...`、`make arch`、`make api-check`、`make helm-check`、
  `make playwright-check`、Vitest `31/31`、mock Chromium/Firefox 各 `26/26`、真实本地认证
  Chromium/Firefox 各 `1/1`、容量 SLO、bundle `343.07 KiB`、Trivy Debian/Go 0 HIGH/CRITICAL、
  govulncheck 0 和公共 npm registry 0 vulnerabilities；隔离 API 登录、分页、排序、非法排序 400、
  节点事件过滤与节点 YAML 对账通过。证据见 21.2.28。
- 未在本 digest 复跑 OIDC 浏览器矩阵、WebKit、30 分钟 soak、rollback/restore、生产 LCP、企业 IdP、
  受信 TLS、企业 SIEM、Edge/Safari 实机、CI 真跑、离机 WORM、生产发布和生产回滚；因此本轮只
  证明当前候选在实验室隔离环境的功能与本地/浏览器门禁，不构成完整商用放行。

**v2.33 修订摘要**

- 在隔离 `dosh-e2e-loki` 部署真实 Loki `3.5.4`，使用内部 registry digest
  `sha256:98242d6a7f2403a1dc35ce256e906f7524a5d9aa166f22221c3aebb38d08643c`；启用私 CA TLS、
  `X-Scope-OrgID` 租户鉴权、非 root 和只读根文件系统。
- `tools/auditreceiver` 增加可选 Loki push：先校验 Bearer/HMAC，再推送 Loki；只有 Loki 返回 2xx
  才确认给 dosh，避免测试接收端先确认、真实存储后失败的假阳性。Loki 时间戳使用 Unix 纳秒字符串。
- `scripts/e2e-audit-webhook.sh` 增加 `DOSH_E2E_AUDIT_BACKEND=loki`，自动部署 Loki、建立
  port-forward、真实写入并回查，同时验证无租户 401、租户隔离、标签和 Request ID；`make e2e-audit-loki`
  为统一入口并保留 artifact。
- r4 全量矩阵通过：`audit_result=pass`，隔离 Release revision 10；私 CA、Bearer/HMAC、业务成功/拒绝、
  四类失败审计且资源不变、Token-only、HMAC-only、瞬时/持续 503、超时恢复、队列满、只读拒绝、
  20 条 SIGTERM 排空和泄露扫描 0 均通过。
- Loki 回查 `events=55 request_ids=34 labels=job/service tenant_isolation=isolated`；SIGTERM 的
  20 个 requestId 已全部纳入查询集合。证据归档见 21.2.26。
- 生产 `dosh` 未变化：App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 / NodePort `32626`。
- 仍未关闭：企业真实 IdP、受信公共 CA、企业 SIEM 的保留/告警/访问审批/WORM、Edge/Safari 实机、
  GitHub Actions 真跑与分支保护、生产发布和生产回滚。Loki 实验室通过不等于完整商用放行。

**v2.32 修订摘要**

- 在隔离 libvirt domain `dosh-browser-runner`（8 GiB RAM、4 vCPU、独立 Fedora 44 内核）完成 WebKit 补充验收：mock 矩阵 15/15、真实 OIDC 矩阵 15/15，均为 `workers=1`、`retries=0`，每浏览器 6/6 denied 审计带 `requestId`，应用日志/配置/Helm values Token 扫描 0。
- 修复 WebKit 无障碍用例的测试时序缺陷：`waitForVisualStability` 等待字体、有限动画和两帧 `requestAnimationFrame` 后再运行 axe，避免 `animate-fade-in` 期间的瞬时低不透明度造成对比度误报；只改测试 harness，不改变产品运行时。
- 修复统一 runner 的多项目调度缺陷：无 `--project` 时默认按 Chromium、Firefox 两个独立容器串行；显式多项目也逐项目拆容器；WebKit 仍需 `DOSH_PLAYWRIGHT_ALLOW_WEBKIT=1`。`make playwright-check` 增加干跑断言，`make web-e2e-container` 的 Chromium 15/15、Firefox 15/15 复跑通过。
- 宿主仍禁止直接运行 WebKit；容器镜像固定为 `192.168.122.10:5000/dosh/playwright:v1.64.1-noble`，隔离 runner 不使用宿主图形栈。补充证据归档 `v231-webkit-final` 共 15 个文件，manifest SHA-256 `bc8718419fcd6a8f4b5f11135c028ce3bda847f331d7c594f49d1cc14143b25e`，双层校验和敏感扫描通过。
- 隔离 Release 当前为 revision 138，仍固定同一候选 digest、1/1 Ready、NodePort `32627`；生产 `dosh` 保持 App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 / `32626`。
- 仍未关闭：企业 IdP、受信公共 CA、真实 SIEM/Loki/OpenSearch、Edge/Safari 实机、GitHub Actions 真跑与分支保护、离机 WORM、生产发布和生产回滚。容器 WebKit 通过不能替代 Safari 实机验收。

**v2.31 修订摘要**

- 冻结并验收最终候选 `0.1.3-rc-sec-20261104035000@sha256:69f30f0016516ac9b56ff867f23c8c0039810dcff29179420c12da323cc00523`；镜像内二进制与工作区 `bin/dosh` 的 SHA-256 均为 `de3d1c59…f386b3`，源码绑定覆盖 183 个运行时文件，`source_tree_sha256=7a254dbd…ae38a`。
- 修复真实集群扩容冲突：扩缩容写请求只发送期望副本数 `{replicas}`，不再固定页面加载时的旧 `resourceVersion`；YAML apply 仍保留版本保护。mock 与真实 OIDC 用例同步验证请求体，避免测试继续要求旧字段。
- 本地与前端门禁：`make verify`、`make race`、容量测试、Vitest 16/16、bundle `340.72 KiB`、mock Chromium 15/15、Firefox 15/15、生产构建 LCP `468.0 ms` 全部通过；容量 P95 为 Pods `20.271482 ms`、Workloads `191.788973 ms`、Overview `202.688339 ms`，Heap 28 MiB。
- 真实隔离验收：Chromium/Firefox 真实 OIDC 各 15/15，每浏览器 6/6 denied 审计带 `requestId`，应用日志/ConfigMap/Deployment/Helm values 泄露扫描 0；审计 Webhook revision 10、CronJob revision 127、rollback/restore `128→129→130→131`、Metrics 三态、429/5xx retry 和 30 分钟 soak 均绑定当前 digest。
- 依赖与镜像：Trivy Debian 12.15 与 Go binary 0 HIGH/CRITICAL；govulncheck `No vulnerabilities found`（DB `2026-11-02 22:01:43 UTC`）；公共 npm registry 生产依赖 `found 0 vulnerabilities`。
- 宿主安全：Playwright 统一 `workers: 1`、`fullyParallel: false`，新增宿主级 `flock` 防止重叠运行；WebKit 默认不注册，宿主直接请求 WebKit 会被拒绝，当前宿主只运行 Chromium/Firefox 受限容器。
- 生产边界：`dosh` 仍为 App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626`，本轮未执行生产发布。
- v2.31 当时仍未关闭 WebKit；该缺口已由 v2.32 的隔离 runner 补齐。仍未关闭：企业 IdP、
  受信公共 CA、真实 SIEM/Loki/OpenSearch、Edge/Safari 实机、GitHub Actions 真跑与分支保护、
  离机 WORM、生产发布和生产回滚。因此 v2.31 是“最终隔离候选已验收”，不是完整商用放行。

**v2.30 修订摘要**

- 会话策略收紧：`internal/platform/config/config.go` 将会话 TTL 上限从 `24h` 收紧为 `8h`，
  与 `AUTH-E-001` 对齐；启用 TLS 时强制要求 `DOSH_COOKIE_SECURE=true`，避免 HTTPS 部署下发
  非 Secure 会话 Cookie。新增超限拒绝和 TLS/Cookie 组合单测。
- 缓存与浏览器隔离：`internal/platform/httpx/httpx.go` 新增 `NoStoreAPI`，所有 `/api/` 响应
  默认 `Cache-Control: no-store`，静态资源不注入；安全头新增
  `Cross-Origin-Opener-Policy: same-origin` 与 `Cross-Origin-Resource-Policy: same-origin`。
  `internal/bootstrap/router.go` 全局挂载对应中间件。
- 退出路径收紧：`/api/v1/auth/oidc/logout` 从公开路由移到会话保护路由；未认证请求返回 401，
  不再允许直接清理 Cookie。`api/openapi.yaml` 和生成类型同步为默认 Cookie 要求并声明 401。
- YAML 写入边界：`internal/modules/yaml/app/service.go` 使用严格单文档解析，拒绝空文档、多文档
  和重复键；避免 UI 审计的是首个文档/后值，而 Kubernetes apply 产生不同解释。
- 授权顺序：`internal/modules/workloads/app/service.go` 的 scale/restart/delete 先校验工作负载
  kind，再做 SubjectAccessReview；无效 kind 返回 `not_supported` 并记录失败审计，不再对伪造资源
  发起无意义的 RBAC 审查。
- 本地证据：`make verify`、`make race`、Vitest 15/15、前端生产构建、bundle `340.73 KiB`、
  Helm、架构、OpenAPI、Playwright 隔离、E2E digest、shell 语法和证据归档检查全部通过。
- 发布边界：上述修复发生在 `sha256:03ac9628…00000c` 之后，旧 digest 和其浏览器/soak/回滚证据
  不得继承到当前源码；必须重新构建镜像、记录 registry digest、在隔离 Release 复验后才可进入
  发布候选。生产 `dosh:32626` 未改动。
- WebKit、企业 IdP、受信公共 CA TLS、真实 SIEM、Edge/Safari、GitHub Actions 真跑与分支保护、
  离机 WORM、生产发布和生产回滚仍未验收，本版本仍不构成完整商用放行。

**v2.29 修订摘要**

- 关闭可在本机验证的 `SEC-U-002` 缺口：新增 `httpx.SameOrigin` 并在
  `internal/bootstrap/router.go` 全局挂载。写请求命中 `Sec-Fetch-Site: cross-site`，或
  `Origin` 为 `null`、畸形、非 HTTP(S) 或与请求 Host 不一致时返回 403；无浏览器 Origin
  元数据的 CLI 请求仍允许，GET/HEAD/OPTIONS 不受影响。已认证状态变更继续由 CSRF token
  保护，两者不是替代关系。
- 单测覆盖同源、跨域、`null`、畸形 Origin、Fetch Metadata、CLI 请求和只读放行；
  `make verify`、`make race` 通过，`httpx` 语句覆盖 `70.6%`、`bootstrap` `26.9%`。
  隔离 Release 运行时验证跨域 Origin 403、cross-site Fetch Metadata 403、同源请求通过中间件。
- 新候选 `0.1.3-rc-origin-20261103170401@sha256:03ac9628…00000c` 已推送并绑定到
  `dosh-e2e-oidc/dosh-oidc-e2e` revision 114；生产 `dosh` 仍为 revision 12、App
  `0.1.2`、Chart `0.1.4`、NodePort `32626`，未改动。
- 当前候选通过 Chromium/Firefox 真实 OIDC 各 15/15、每浏览器 6/6 denied 审计与 token
  扫描、审计 Webhook 全矩阵 revision 10、rollback/restore `109→110→111→112`、Metrics
  三态、429/5xx retry、30 分钟 soak、CronJob 详情、容量、govulncheck 0、公共 npm registry
  `0 vulnerabilities`、Trivy Debian/Go binary 0 HIGH/CRITICAL。
- 容量复验：1000 Pod / 200 工作负载下 Pods P95 `21.016489ms`、Workloads P95
  `189.099579ms`、Overview P95 `197.942489ms`，容量请求后 Heap `28 MiB`。
- 宿主事故口径修正：`01:56:28` 与 `09:39:01` 两次日志中断分别发生在
  `Running 9 tests using 1 worker` 和 `Running 3 tests using 1 worker`；两次 `2 workers`
  运行均正常完成，因此不能把宿主冻结归因于 Playwright worker 并发。`01:16` 的
  `WPEWebProcess` `llvmpipe-15` SIGSEGV 是独立图形栈异常。WebKit 继续禁止在当前宿主机运行。
- WebKit、企业 IdP、受信公共 CA TLS、真实 SIEM、Edge/Safari、GitHub Actions 真跑与分支保护、
  离机 WORM、生产发布和生产回滚仍未验收，本版本仍不构成完整商用放行。

**v2.28 修订摘要**

- 补齐 `SEC-O-003` 的 OIDC 私有 CA 组件级证据：`internal/platform/oidc/provider_test.go` 新增
  TLS 回归，验证配置 `DOSH_OIDC_CA_FILE` 后可连接私 CA HTTPS、未配置该 CA 时拒绝握手、
  非法 PEM 在启动配置阶段失败；代码仍无 `InsecureSkipVerify` 开关。
- 定向与全量结果：`go test -race ./internal/platform/oidc` 通过，OIDC 包语句覆盖 `76.0%`；
  `make verify`、`make race` 通过；该证据只关闭“私 CA TLS 组件测试缺失”，不替代真实企业 IdP、
  企业 CA 链、IdP 短暂不可达和 RP logout 的集成验收。
- 本地性能复验：Chromium 生产构建 LCP `508 ms`，低于 `2.5 s` 门限；本轮未运行 WebKit。
- 生产仍为 App `0.1.2` / Chart `0.1.4`、revision 12、`2/2` Ready、NodePort `32626`；
  隔离 Release 仍为 revision 102、`1/1` Ready、NodePort `32627`。外部商用门禁和生产发布/回滚
  仍未完成，不得据此宣称完整可商用。

**v2.27 修订摘要**

- 当前候选补齐 Firefox 受限 runner 证据：mock `15/15`、隔离 Release 真实 OIDC `15/15`，
  与 Chromium 相同覆盖登录、三身份、Deployment 创建、七类负向、主题、axe 和键盘。
- 修复 `scripts/e2e-oidc-browser.sh` 的单浏览器审计期望：不再固定要求三浏览器合计的 18 次
  callback failure，而是按 `--project` 数量计算每浏览器 6 次；显式环境变量仍可覆盖。
  当前候选 Chromium/Firefox 均得到 `6/6` callback failure 和 `6/6` denied 审计。
- Chromium 与 Firefox 的 token 扫描均覆盖应用日志、ConfigMap/Deployment、Helm values、
  浏览器 URL/DOM/Web Storage 和响应体，全部 `PASS`。
- 隔离 Release 复验后为 revision 102、1/1 Ready，镜像仍绑定 registry digest
  `sha256:66874e74…fb264`；证据新增到
  `/var/lib/dosh/e2e-runs/20261103T162409Z-doc-scope-cleanup/` 的 Firefox/Chromium OIDC 子目录。
- 新增证据已用 `scripts/archive-evidence.sh` 生成 candidate digest 受控归档：34 文件，
  manifest SHA-256 `882acb50…a7e4`，manifest 自身摘要外锚并通过双层校验。
- WebKit 仍只允许在独立 runner/CI 执行，v2.25 当时候选未执行 WebKit；企业 IdP、受信 TLS、真实
  SIEM、Edge/Safari、CI 真跑、离机 WORM 和生产发布/回滚仍未验收，因此不能宣称完整商用放行。

**v2.26 修订摘要**

- 统一当前候选 `0.1.3-rc-audit-https-20261103231526@sha256:66874e74…fb264` 的证据口径：
  Chromium mock/真实 OIDC 各 `15/15`，Firefox/WebKit 明确标记为待独立 runner，历史候选的
  `45/45`、`9/9`、`42/42` 和 `33/33` 只保留为历史参考。
- 将阶段总览、WBS、交付物、OIDC、soak、风险登记和归档条目更新到当前 digest：30 分钟 soak
  为 30 个事件、15,760 个事件帧、28,395 行日志、5 次重连、0 错误、峰值 24 MiB；回滚为
  revision `93→94→95→96`；证据归档为 35 文件、manifest SHA-256 `bc42ef92…abf07a`。
- 修正宿主事故描述中的“2 worker”残留：09:39 事故日志明确为 `Running 3 tests using 1 worker`，
  当前两个 Playwright 配置也都固定为单 worker、串行独立容器；历史 mock 曾使用 2 worker，但不得
  写成事故当时配置。01:16 是 `WPEWebProcess` SIGSEGV 后宿主继续运行，两者不得混写。
- 生产仍为 App `0.1.2` / Chart `0.1.4`、revision 12、`2/2` Ready、NodePort `32626`，本轮不改动。
  外部商用门禁、Firefox/WebKit 独立 runner 和生产发布/回滚仍未完成。
- 本轮文档复核已执行 `make verify`、`actionlint v1.7.7`、OIDC/Playwright runner `bash -n` 和
  `make playwright-check`，全部通过；证据保存于
  `/var/lib/dosh/e2e-runs/20261103T162409Z-doc-scope-cleanup/`。
- CI 的 E2E 已拆为 Chromium/Firefox/WebKit 三个 matrix job，每个 job 使用独立容器和相同
  的 shm/内存/CPU/PID/超时限制；`make playwright-check` 现在会同时拒绝“一个容器串跑多浏览器”。
- CI 新增独立 `policy` job，执行 actionlint、Playwright 隔离、E2E registry digest 绑定和证据
  归档回归；`release-summary` 已将其纳入必需依赖。

**v2.25 修订摘要**

- 当前候选完成隔离门禁复验：30 分钟 soak、revision `93→94→95→96` rollback/restore、
  Metrics 三态、429/5xx retry、CronJob 无 selector 详情、Trivy、govulncheck 和生产 npm audit 均通过。
- 修复测试夹具竞态：`scripts/e2e-oidc.sh` 启动前检查 IdP 端口占用，启动后检查子进程存活，并用新 CA
  `--cacert` 严格验证健康与 discovery；失败证据保留在 `soak-failed-r1-infra.log`，第二轮通过。
- Trivy 下载改为可配置 HTTP/1.1；本轮通过 OCI artifact 预取 `trivy-db:2` 并显式
  `--skip-db-update` 扫描，记录数据库 artifact digest、更新时间、Debian 和 Go binary 结果。
- 35 文件证据归档完成双层 SHA-256 和敏感信息扫描，manifest SHA-256 为 `bc42ef92…abf07a`；
  Firefox/WebKit、外部商用门禁和生产发布仍未完成。

**v2.24 修订摘要**

- 审计 Webhook 从“优先 HTTPS”收紧为强制 HTTPS：运行时 `DOSH_AUDIT_WEBHOOK_URL` 拒绝 `http://`，
  Helm 在 `audit.webhook.enabled=true` 时拒绝非 `https://` URL；新增 Go 配置负向用例和 Helm 渲染负向用例。
- 列表筛选 URL 验收补齐真实交互：mock Chromium 覆盖关键词、命名空间、类型、刷新、新标签页分享、
  前进/后退和空参数清理；真实隔离 Release Chromium 增加同一持久化抽样。
- 将当前源码与历史候选解绑：旧候选 `0.1.3-rc-a11y2-20261103093530@sha256:45090acf…f680ab` 的
  45/45、42/42、soak、回滚和归档不能继承给新源码。
- 构建并推送新候选 `0.1.3-rc-audit-https-20261103231526@sha256:66874e74…fb264`，隔离 Release
  `dosh-e2e-oidc` 升级到 revision 90、1/1 Ready；健康检查 200，Chromium mock 与真实 OIDC 各 15/15，
  审计 Webhook 全矩阵在 revision 10 通过。生产 `dosh` 仍为 App `0.1.2` / Chart `0.1.4` revision 12，
  未被本轮修改。
- v2.24 当时新候选的 Firefox/WebKit、soak、回滚、Metrics/retry、Trivy 和完整证据归档尚未执行，不得宣称完整商用放行。

**v2.23 修订摘要**

- 清理非历史性状态漂移：第 14.0 节阶段总览回填历史候选的完整 OIDC 42/42、mock 45/45、
  30 分钟 soak、回滚/恢复、Metrics、retry、动态上下文、审计和证据归档结果，不再引用历史
  候选的 33/33 或“soak 待补”。
- 第 16 节需求追踪矩阵同步当前候选证据；第 21.4 节更新动态上下文、Metrics、长连接和当前
  15 文件受控归档状态，仍保留 URL 持久化的真实 Release 刷新/分享/前进后退抽样缺口。
- 第 23 节验收模板改为显式占位符，禁止复制模板后继承示例或历史候选的通过状态；第 26.6 节
  将“测试接收端尚未构建”修正为“测试接收端已构建并通过隔离矩阵，真实 SIEM 仍未接入”。
- `make release-check` 补齐 `race` 与 `web-test`，并改为从当前源码构建指定 `TAG` 后再扫描，
  避免把本地旧 `dev` 镜像当作候选；README 与运行手册同步要求候选发布统一执行该入口。
- 历史候选章节、旧 digest、旧 revision 和当时的未完成状态均保持原样，不作为当前发布判断依据。

**v2.22 修订摘要**

- 将 CronJob 无 selector 详情抽样固化为 `scripts/e2e-cronjob-detail.sh`，新增
  `make e2e-cronjob-detail`。脚本强制测试 namespace、NodePort 与生产隔离，创建 suspended
  CronJob，通过签名测试 IdP 登录 viewer 后请求真实详情 API，验证 HTTP 200、`selector={}`、
  `pods=[]`、`status=Suspended`，并在退出时自动删除测试资源。
- 当前候选真实执行通过：`/var/lib/dosh/e2e-runs/20261103T145200Z-cronjob-detail/cronjob-detail.log`；
  `dosh-e2e-oidc` revision 89、`1/1` Ready，镜像仍绑定 registry digest
  `sha256:45090acf…f680ab`，CronJob 已确认不存在。
- 重新生成 v5 受控归档：
  `/var/lib/dosh/evidence/45090acfc99e/20261103T144947Z-final-candidate-gates-v6-20261103`；
  15 文件、34,269 bytes，双层 SHA-256 与敏感扫描通过，manifest SHA-256 为
  `ab372addd8cede51eedc25037b5efa535a50ee6f4c3e0a419d9955df155a47f0`。
- 同期复核 Playwright 资源：配置为单 worker、三浏览器串行独立容器、6 GiB/4 CPU/2048 PID、
  私有 1 GiB shm；历史运行峰值低于 3.7 GiB，当前矩阵峰值约 1.4–1.9 GiB。宿主硬冻结不能归因于
  Playwright 并发；缺少 OOM、panic、lockup 和 vmcore，仍需主机级内核/硬件取证。

**v2.21 修订摘要**

- 回填当前候选 `0.1.3-rc-a11y2-20261103093530@sha256:45090acfc99e96cbf40d7a2808736c6dbdc043feee3d8a69164917fc0bf680ab`
  的完成性证据：完整 OIDC 42/42、mock 45/45、30 分钟 soak、回滚/恢复、Metrics 三态、
  Kubernetes 429/5xx 重试、动态集群上下文 3/3、审计 Webhook 全矩阵和四种失败审计均通过。
- 新增失败审计真实集群验收：无效 Pod scale、非法 DaemonSet scale、非法 Job restart 和非法删除
  propagation 分别产生 `scale/failure`、`scale/failure`、`restart/failure`、`delete/failure`
  审计；目标 Deployment 保持不变。审计脚本最终输出 `failure_audit ... resource_unchanged=true`
  和 `audit_result=pass`，证据为
  `/var/lib/dosh/e2e-runs/20261103T143453Z-audit-webhook-isolated-idp/audit-webhook-evidence.log`。
- 回填回滚证据：隔离 Release `dosh-e2e-oidc` revision `83 → 84 → 85 → 86`，每个阶段
  health/ready、viewer/operator/admin RBAC 和 logout 均通过；完整浏览器矩阵随后在 revision 89
  完成，当前 Pod `1/1` Ready。
- 回填 Metrics 与重试证据：Available CPU `2149m` / 内存 `10672955392` bytes，禁用时
  `metricsAvailable=false` 且无陈旧值，恢复 CPU `2047m` / 内存 `10676428800` bytes；429/500/502/503/504
  各注入两次后恢复，持续 429 恰好尝试 3 次，写请求仅 1 次且副本数不变，`Retry-After` 约 2097 ms。
- 回填动态上下文证据：隔离 Release `dosh-ui-e2e` revision 3、2/2 Ready，Chromium/Firefox/WebKit
  各 1 条真实上下文用例共 3/3 通过。
- 修正隔离环境短暂 CrashLoop 的记录：Pod 在审计夹具运行期间重启并误连另一套私 CA 的临时 IdP，
  使用原测试 CA 恢复后 Ready；这是测试夹具端口复用问题，不是产品启动回归。审计夹具已改用
  `9445` 独立 IdP 端口并增加冲突检查，复跑期间原 OIDC 夹具的 `9443` IdP 与 Release 均保持
  健康，证明两套夹具可并存。
- 已完成 candidate digest 受控归档：`/var/lib/dosh/evidence/45090acfc99e/20261103T143902Z-final-candidate-gates-v4-20261103`，
  15 文件，`make evidence-check`、`sha256sum -c manifest.sha256`、`sha256sum -c manifest-digest.txt`
  和敏感扫描均通过；manifest SHA-256 为 `2b34ac2f78131274b231517ae16f47148b1905bddbf7a6d533314c0efe9f3282`。
- 当前仍未完成：离机 WORM/长期保留、企业 IdP、
  受信公共 CA TLS、真实 SIEM/Loki/OpenSearch、Edge/Safari 实机、GitHub Actions 真跑与分支保护、
  生产发布和生产回滚。上述任何一项不得用实验室结果替代。

**v2.20 修订摘要**

- 修复共享表格的可访问性：滚动容器增加 `role="region"`、可读名称、`tabIndex=0` 和可见焦点环，
  新增组件级回归测试；浅色 `--muted-foreground` 从 `45%` 调整为 `42%`，消除 axe 报出的
  `4.44:1` 对比度失败。
- 修复真实 Release WebKit 主题用例的清理导航：`web/e2e-oidc/ux.spec.ts` 改用共享浏览器上下文的
  `page.request.get(..., { maxRedirects: 0 })` 调用 logout，断言 302 后回到登录页；主题用例超时
  提高到 60 秒。Chromium、Firefox、WebKit 三浏览器 UX 9/9 和完整 OIDC 42/42 已通过。
- 新增 `TST-N-002` 和 `scripts/run-playwright-container.sh`：容器化 Playwright 默认单 worker、
  每个浏览器独立容器，限制 `6g` 内存、`4` CPU、私有 `1g` shm、`2048` PID 和 20 分钟硬超时，
  禁止 `--ipc=host`；`web/playwright.config.ts` 与 `web/playwright.oidc.config.ts` 均固定
  `workers: 1`、关闭 `fullyParallel`。`make playwright-check` 已接入 `make verify`。
- CI 的 Playwright E2E 已拆为 Chromium/Firefox/WebKit 三个 matrix job，每个 job 独立容器；E2E
  与 performance job 均移除宿主 IPC，改用私有 `1g` shm、`6g` 内存、`4` CPU、`2048` PID、
  `ulimit core=0` 和 20 分钟 job 超时；静态门禁同时检查本地 runner 与 CI 工作流。
- 新增 `make e2e-image-check`：强制 rollback、soak、retry、Metrics 和 audit E2E 脚本把候选
  registry digest 传给隔离夹具，防止以可变 tag 冒充当前候选；已接入 `make verify`。
- 主机事故复核：2026-11-03 09:39 WebKit 定向运行期间宿主硬冻结，boot 日志没有正常关机、
  OOM 或 panic；01:16 另有一条 `WPEWebProcess` SIGSEGV core dump。事故运行日志为 1 worker；
  历史 mock 的 2 worker 容器峰值约 `1.6 GiB`，而宿主为 `124 GiB`、`36` 核，因此“并发过大”
  既不是唯一原因，也尚未被证明
  是主因；更可疑的是 WebKit 稳定性、旧宿主 IPC 共享和容器无资源边界。事故后未再启动浏览器
  测试，新的隔离运行器已按单 worker、`6g` 内存、`4` CPU、私有 `1g` shm、`2048` PID、
  20 分钟硬超时和禁止宿主 IPC 完成真实三浏览器实跑。
- 当前候选 `0.1.3-rc-a11y2-20261103093530`：image ID `sha256:7a109763…11c01f`，
  本地 manifest `sha256:5b027290…9063e3`，registry digest
  `sha256:45090acfc99e96cbf40d7a2808736c6dbdc043feee3d8a69164917fc0bf680ab`；
  Trivy Debian 12.15 与 Go binary 均 0 HIGH/CRITICAL。
- 当前 digest 的隔离 UX 定向矩阵为 9/9：Chromium/Firefox/WebKit 的主题、axe 和键盘用例全部
  通过；完整 OIDC 矩阵为 42/42（每个浏览器 14/14）。mock E2E 最近一次完整结果为 45/45，
  但该结果产生于隔离运行器和 API logout 改动之前，仍不能替代当前源码复跑。
- 本地门禁：`make verify`（含 `playwright-check`）、`make race`、容量测试、Vitest 15/15、
  bundle `340.71 KiB`、govulncheck 0 与 Trivy 0 HIGH/CRITICAL 通过。
- 恢复后复核：9 台 VM 运行、8 个 Kubernetes 节点 Ready；生产 `dosh` 为 revision 12、2/2、
  NodePort `32626`，`/healthz` 与 `/readyz` 均 200。隔离 Release `dosh-e2e-oidc` revision 80
  曾因 `exec /usr/local/bin/dosh: exec format error` 启动失败；根因是 `worker-0003` 上的镜像
  解包快照中该二进制为 0 字节，而 containerd 内容层完整。缩容到 0、清除损坏镜像/快照并重拉同一
  registry digest 后二进制恢复为 30,699,682 字节、amd64 静态 ELF、SHA-256
  `49971fa855de36adf470b8671ffe2fc9755e51149283b6df35d6d0d9b308fa1d`；重新安装的
  revision 81 调度到 `worker-0005`，Pod `1/1` Ready。
- 生产和其他隔离 Release 均未被本轮修改。CI 真跑、企业 IdP、受信 TLS、真实 SIEM、
  Edge/Safari、离机 WORM、30 分钟 soak、回滚和证据归档仍未验收。

**v2.18 修订摘要**

- 依赖安全修复：`golang.org/x/net` `v0.60.0 → v0.62.0`，修复
  `GO-2026-6866`、`GO-2026-6860`；`golang.org/x/crypto` `v0.57.0 →
  v0.58.1-0.20261009181718-2f1f834330b2`，关闭 `GO-2026-5932` 的模块级告警。
  `govulncheck v1.9.0` 对全部 34 个根包报告 `No vulnerabilities found`。
- 依赖审计口径：`npm audit --omit=dev --registry=https://registry.npmjs.org` 为 0；
  完整 dev 审计仍为 3 个 `minimatch` moderate，来源为 `openapi-typescript →
  @redocly/openapi-core`，仅用于构建期类型生成，不进入生产 bundle。该风险继续登记，
  不以生产 0 漏洞掩盖工具链风险。
- 质量与性能复验：`make verify`、`make race`、`make capacity-test` 通过；1000 Pod /
  200 工作负载下 P95 为 pods `19.052ms`、workloads `175.893ms`、overview `189.160ms`，
  Heap `28 MiB`。生产构建 LCP `472ms`，Vitest 14/14，三浏览器 mock E2E 45/45。
- 认证关键路径补强：新增 `GET /api/v1/capabilities` Handler 测试，验证登录会话中的 actor、
  `namespace` 查询参数和 RBAC 结果贯通；`auth/http` 覆盖率从 `79.4%` 提升到 `80.9%`。
- 新本地候选 `0.1.3-rc-secdeps-20261103011313`：image ID `31464404…421d26`，
  本地 manifest digest `sha256:835314ae…d7e239`；Trivy Debian 12.15 与 Go binary
  均 0 HIGH/CRITICAL。该 digest 不是 registry `Docker-Content-Digest`。
- 实验网络当时不可达：`enp6s0` 为 `DOWN/NO-CARRIER`，registry 与 Kubernetes API 超时；
  该历史候选的隔离部署、42/42 浏览器、30 分钟 soak 和证据归档当时均为发布阻断项。
- 生产 `dosh`、隔离 Release revision 75、NodePort `32626/32627` 均未修改；本次修订仍不
  提升任何 Release 或商用验收状态。

**v2.17 修订摘要**

- 补齐 `TST-U-003` 的只读 HTTP adapter 覆盖：`nodes/http`、`overview/http`、`network/http`、
  `storage/http` 均达到 `100%` 行覆盖，包含成功 DTO、查询参数、404/403/429/503/500/501
  错误映射，不把数据库或 Kubernetes 依赖带入 Handler 测试。
- 修复 `SEC-U-004` 缺口：workloads app 对无效 kind、不支持扩容/重启的 kind 和非法删除策略
  也记录失败审计；新增负向用例断言 Repository 写调用次数为 0，并补齐 List/Get、Pod 读写、
  Restart、Preview、删除策略和五类资源权限映射。
- 新增 `WL-N-005`：工作负载写操作在校验失败时仍必须产生失败审计，且不得调用 Kubernetes
  写接口；失败审计不得伪装成 denied，除非错误语义确为权限拒绝。
- 修复 Kubernetes adapter 实际缺陷并新增 `WL-N-006`：无 selector 的 CronJob 详情原先会解引用
  nil；现在安全返回空 selector、空 Pod 列表和 CronJob summary，五种工作负载详情均有回归测试。
- 覆盖率复核：domain `100%`、workloads app `99.4%`、overview app `98.9%`、
  config `87.5%`、kube adapter `78.4%`，四个只读模块 HTTP adapter 均为 `100%`。
- 前端复验：Vitest 14/14，Chromium/Firefox/WebKit mock E2E 45/45，bundle 初始 JS gzip
  `340.68 KiB`；后端 `make verify`、`make race` 通过。
- 新本地候选 `0.1.3-rc-commercial-20261103005821`：image ID `sha256:212c24e2…4bf0bb`，
  本地 manifest digest `sha256:646a5d62…bdd0b`；Trivy Debian 12.15 与 Go binary 均
  0 HIGH/CRITICAL。该 digest 不是 registry `Docker-Content-Digest`。
- 网络事实：`enp6s0` 仍为 `DOWN/NO-CARRIER`；`192.168.122.10:5000` 与
  `192.168.122.50:16443` 均超时，因此不能 push、取得 registry digest、更新隔离 Release
  或执行当前候选的真实 Release 浏览器矩阵与 soak。
- 生产 `dosh`、隔离 Release revision 75、NodePort `32626/32627` 均未修改；本次修订不提升
  Release 或商用验收状态。

**v2.16 修订摘要**

- 新增 `AUTH-O-006`：当 `DOSH_OIDC_USERNAME_CLAIM=email` 时，必须同时满足 `email` 非空且
  `email_verified` 显式存在并为 `true`；缺失该字段不得被解释为已验证。
- `internal/platform/oidc/provider.go` 使用 `*bool` 区分“缺失”和 `false`，`provider_test.go`
  新增缺失 `email_verified`、缺失 `email` 两个负向用例；OIDC、auth HTTP 与 auth app 的定向
  `go test -race` 已通过。
- 重新执行并通过 `make verify`、`make race`、Vitest 14/14 和三浏览器 mock E2E 45/45；首次
  WebKit axe 扫描因 Vite 主题样式注入时序而失败、重试通过，已改为在扫描前等待 `dark` class、
  `color-scheme` 和 CSS 变量同时生效，随后定向复跑 6/6、全量复跑 45/45。
- 构建新本地候选 `0.1.3-rc-email-20261103004230`（image ID `074d6208…689d79`，本地 manifest
  digest `sha256:6946452f…2325be`）；Trivy 对 Debian 12.15 与 Go binary 均为 0 HIGH/CRITICAL。
- 明确候选身份边界：本地 manifest digest 不是 registry `Docker-Content-Digest`，必须推送并读取
  registry 响应后才能进入隔离部署。
- 生产 `dosh`、隔离 Release revision 75、NodePort `32626` 均未改动；本次修订不提升任何
  Release 或商用验收状态。

**v2.15 修订摘要**

- 修复 AUTH-O-005 的开放重定向边界：`safeReturnTo` 现在拒绝反斜杠路径、绝对 URL、
  控制字符及解码后控制字符；新增 `/\evil.example`、编码 CRLF、`javascript:` 和普通相对路径测试。
  Chromium/Firefox/WebKit 的既有开放重定向用例同时覆盖 `//evil.example` 与 `/\evil.example`。
- 补齐 TST-U-001 的 app/domain 单元测试：domain `100%`、overview `98.9%`、nodes `100%`、
  configuration `92.9%`、events `100%`、network `100%`、storage `100%`、yaml `80.9%`；
  `httpserver` 与 `requestid` 也达到 `100%` 并覆盖监听失败、TLS 配置和优雅关停。
- 本地门禁：`make verify`、`make race`、Vitest 14/14、mock E2E 45/45、Trivy Debian/Go binary
  0 HIGH/CRITICAL 均通过；原始证据和命令输出见 21.2.15。
- 新本地镜像 tag `0.1.3-rc-sec-20261103002512`，image ID `80086aec0ef0…`，本地 manifest digest
  `sha256:94978f73…17b25a`。该值是本地 manifest digest，不是 registry `Docker-Content-Digest`，
  不得作为发布身份。
- 实验网络 `enp6s0` 仍为 `NO-CARRIER`，Registry 与 Kubernetes API 超时；本轮未部署、未触碰生产，
  真实 Release UX/安全负向复验、完整 OIDC 浏览器矩阵和 30 分钟 soak 仍为阻断项。

**v2.14 修订摘要**

- 统一当前实施台账中的浏览器门禁口径：mock 主套件为 15 条路径 × 3 浏览器 = **45/45**；
  历史修订记录中的 42/42 保留原样，避免用当前结果覆盖历史证据。
- 明确当前 UX 证据边界：本地 45/45 与 axe 通过只证明源码质量；新镜像尚未生成 registry digest，
  真实 Release 的 UX 复验、完整 OIDC 矩阵和 soak 仍为发布阻断项。
- 重申生产约束不变：命名空间 `dosh`、Release `dosh`、默认/生产 NodePort `32626`；
  `32627`/`32628`/`32629` 仅用于隔离 E2E，不得用于生产验收。

**v2.13 修订摘要**

- 新增真实隔离 Release 三浏览器 UX 套件 `web/e2e-oidc/ux.spec.ts`：暗色主题切换/刷新持久化、
  登录页与核心运维页亮暗主题 axe、纯键盘登录→侧栏导航→工作负载详情→扩缩容确认→退出。
- 首次对旧镜像执行真实 UX 套件时，主题和键盘用例通过，axe 在概览页 Warning 徽标发现
  `4.33:1` 严重对比度问题；将浅色 `--warning` 从 `35 92% 33%` 调整为 `35 92% 30%`，
  理论对比度提升至 `5.01:1`，并在 mock 数据中加入 Warning 事件防回归。
- mock E2E 新增纯键盘主路径和本地退出 DELETE 语义，当前三浏览器 45/45；Vitest 14/14；
  `make verify`、`make race`、容量测试、govulncheck、npm audit 和 Trivy 0 HIGH/CRITICAL 均通过。
- 本地产能/性能结果：Pod 列表 P95 `20.54ms`、工作负载 `175.83ms`、Overview `187.75ms`，
  Heap `28 MiB`；生产 LCP `528ms`；initial JS gzip `340.68 KiB`。
- 已构建本地镜像 `0.1.3-rc-ux-20261103000038`（image ID `68e8fcbe63f5…`）。实验网络接口无载波，
  `192.168.122.0/24` 的 registry 与 Kubernetes API 均超时，无法推送、生成 registry digest 或部署；
  因此真实 Release UX 复验、新镜像的 30 分钟 soak 和证据归档仍未关闭，不得提升商用结论。

**v2.12 修订摘要**

- 在隔离 Release `dosh-e2e-oidc/dosh-oidc-e2e` 上完成当前候选
  `0.1.3-rc-oidc-20261102171324@sha256:0be6df25…798ca8` 的 retry、Metrics 三态、
  审计 Webhook/SIGTERM 和 rollback/restore 复验；回滚链路为 revision `72 → 73 → 74 → 75`，
  最终恢复到候选 digest。
- 回填 21.2.13 至 25.9、26.3、26.4、26.9：retry、Metrics、审计和回滚不再沿用旧 digest；
  30 分钟 soak 仍只绑定旧候选，当前 digest 必须按 25.9 第 4 项复跑。
- 明确当前 digest 的剩余发布门禁：真实 Release 的主题/axe/键盘套件、30 分钟 soak、受控证据归档，
  以及企业 IdP、受信 TLS、真实 SIEM、Edge/Safari、CI 真跑和离机 WORM。
- 生产 `dosh` 仍为 App `0.1.2` / Chart `0.1.4`、revision 12、2/2 Ready、NodePort `32626`；
  隔离测试继续使用独立 namespace、Release 和 NodePort，未触碰生产。

**v2.11 修订摘要**

- 校正证据口径：当前候选 `0.1.3-rc-oidc-20261102171324@sha256:0be6df25…798ca8` 只覆盖
  OIDC 三身份 API/RBAC、七类负向浏览器矩阵、审计关联、Token 扫描和本地质量门禁；不得把前一
  完整门禁候选的 retry、Metrics、30 分钟 soak、审计 Webhook/SIGTERM 或 rollback/restore 继承过来。
- 把 21.2.10 和 25.8 明确标记为“前一完整门禁候选”的历史台账，把当前候选证据集中在 21.2.12，
  并新增 25.9 当前候选复验清单，逐项列出待执行命令、通过判据和证据要求。
- 统一浏览器矩阵计数：三浏览器共 33/33，其中每浏览器 4 条正向/RBAC 用例和 7 条负向登录用例；
  删除“六类/五类负向场景”的旧口径。
- 保持 UX-O-001/NFR-U-008 为“源码已实现、本地自动化通过”，在新候选隔离 Release 复验前不得提升。

**v2.10 修订摘要**

- 完成 AUTH-O-005 浏览器级负向矩阵：新增 `DOSH_OIDC_STATE_TTL` 严格配置（1s–30m，默认 10m）、
  测试 IdP `nonce_mismatch` 故障注入，以及缺失/篡改/不匹配/过期 state、nonce mismatch、
  authorization code 重放和开放重定向七条用例。
- 新候选 `0.1.3-rc-oidc-20261102171324`（digest `sha256:0be6df25…798ca8`）在隔离 Release
  `dosh-e2e-oidc/dosh-oidc-e2e` revision 68、NodePort `32627` 上完成 Chromium/Firefox/WebKit
  33/33；18 次回调失败全部产生带 `requestId` 的 denied 登录审计，浏览器/日志/配置 Token 扫描 0 命中。
- 修复扩缩容幂等冲突：当目标副本数已经达成但调用方 `resourceVersion` 已过期时返回成功且不重放写；
  真实并发变更仍返回 409。新增 kube 单元回归测试。
- 本地门禁：`make verify`、`make race`、Vitest 14/14、mock E2E 42/42、生产 LCP 984ms、
  initial JS gzip `340.68 KiB`、npm audit 0、Trivy Debian 与 Go binary 0 HIGH/CRITICAL。
- 证据归档 `/var/lib/dosh/evidence/0be6df2586a3/20261102T172547Z-oidc-negative-matrix-v1`，
  14 个文件，manifest SHA-256 `74470e6a…03efc89cc`，双层校验通过。
- 状态边界：AUTH-O-005 对签名测试 IdP 达到实验室已验收；企业 IdP 未接入。retry、Metrics、
  30 分钟 soak、审计 Webhook/SIGTERM 和 rollback/restore 仍需在新 digest 上复跑，不得沿用旧候选结论。

**v2.9 修订摘要**

- 实现 UX-O-001：新增 `.dark` 语义 token、系统偏好/显式选择解析、主题持久化、无内联脚本的
  首屏主题初始化、AppShell 键盘可操作切换入口，以及 Sonner、CodeMirror 和状态语义色适配。
- 实现 NFR-U-008 首批 axe 门禁：覆盖登录、总览、节点、工作负载详情和 Pod 详情，亮/暗主题各扫描；
  修复 Progress 可访问名称、成功徽标对比度和正文链接可区分性问题。
- 本地验证：Vitest 14/14；Chromium/Firefox/WebKit 全量 42/42；axe 0 serious/critical；
  生产构建通过，initial JS gzip `340.68 KiB`。
- 状态口径：UX-O-001/NFR-U-008 为“源码已实现、本地自动化已通过”；仍需打入新不可变候选、
  在隔离 Release 复跑并归档后才可提升为“实验室已验收”。

**v2.8 修订摘要**

- 修复证据归档完整性链：`summary.txt` 现在先写入并纳入 `manifest.sha256`，manifest 自身摘要
  单独写入 `manifest-digest.txt`，避免摘要文件游离在清单之外。
- 新增 `scripts/test-archive-evidence.sh` 与 `make evidence-check`，覆盖正向归档、摘要覆盖、
  双层校验、凭据内容拒绝、仅含排除文件的目录拒绝和非法 digest 拒绝。
- 以权威候选 digest 重新归档小证据集和完整证据集：小证据集 6 个文件；完整证据集 510 个文件、
  18,096,557 字节，目录 0700、文件 0600，敏感材料扫描 0 命中。
- 记录小证据集 manifest SHA-256 `aa2b81f6…d673f891`、完整证据集 manifest SHA-256
  `6d2b7044…76a1094`；三者均与 `sha256:47983a…312e` 绑定。
- 明确证据边界：本机受控归档已完成；离机 WORM、长期保留、访问审批和销毁策略仍未验收，
  因此不能据此宣称生产合规归档完成。
- 新增 26.10：把 UX-O-001 暗色主题、NFR-U-008 WCAG 2.1 AA 和 AUTH-O-005 浏览器级 OIDC
  负向矩阵落实为步骤、命令、证据和状态提升规则，避免 P1 缺口被 P0 结果掩盖。

**v2.7 修订摘要**

- 新增 1.7“从设计到放行的实施门禁”，把需求冻结、纵向切片、本地门禁、镜像身份、隔离部署、
  功能/授权/浏览器、故障与 soak、回滚、证据归档和生产发布串成 G0–G9 可执行路径。
- 回填当前 digest 的 30 分钟 soak：29 个事件、50,400 个事件帧、28,449 行日志、5 次重连、
  0 应用错误、0 流失败，峰值 28 MiB；覆盖 Pod 删除和应用滚动重启。
- 回填审计 Webhook/SIGTERM 隔离验收：私 CA、Bearer/HMAC、业务成功/拒绝、瞬时/持续 503、
  超时、队列满、只读拒绝和 20 条 SIGTERM 排空；敏感值扫描 0 命中。
- 回填 OIDC rollback/restore：隔离 Release revision 59→60→61→62，每个阶段复验 health/ready、
  三身份权限和 logout；生产仍为 App `0.1.2` / revision 12 / NodePort `32626`。
- 审计与回滚脚本补充镜像 digest 传递和 evidence revision 记录，确保 Helm 部署与结论绑定不可变身份。
- 当时仍未关闭：企业 IdP、受信 TLS、真实 SIEM、Edge/Safari、CI 真跑、受控证据归档和生产发布/回滚；
  v2.8 已补本机受控归档，离机 WORM 仍未关闭。

**v2.6 修订摘要**

- 修复动态集群上下文遗漏：`overview-page.tsx` 的总览面包屑改为读取 `/api/v1/overview`，
  新增 Vitest 与 Playwright 断言，确保任意集群不再出现硬编码 `总览 / kubedo-lab`。
- 修复隔离部署发现的 Helm 配置缺陷：`DOSH_MAX_REQUEST_BODY` 经 `int64` 格式化后渲染为
  `"2097152"`，避免 Helm 将大整数输出为科学计数法导致严格配置解析失败；`make helm-check`
  新增精确整数渲染断言。
- 重新冻结候选 `0.1.3-rc-20261102153511`，digest
  `sha256:47983a7aedf46d683f56982b0f352a684ae81d126b3390eada1b9bae0ba4312e`；Trivy 扫描
  Debian 12.15 与 Go binary 均为 0 HIGH/CRITICAL，扫描时间 `2026-11-02T15:35:26Z`。
- 回填最新本地门禁：`make verify`、`make race`、容量、Vitest 10/10、三浏览器 E2E 30/30、
  bundle `339.78 KiB` 通过。
- 回填隔离 Release `dosh-ui-e2e` revision 2：2/2 Ready、NodePort `32629`、health/ready 200、
  本地登录、动态集群名 `dosh-ui-dynamic-e2e` 和真实三浏览器 3/3 通过。
- 回填同一 digest 的 OIDC API/RBAC 矩阵、三浏览器 12/12、Token 扫描 0 命中、完整 retry 矩阵
  （429/500/502/503/504、写请求仅 1 次、重置、取消）和 Metrics 三态。30 分钟 soak 正在执行，
  审计 Webhook/SIGTERM、回滚、企业 IdP、生产发布和 CI 真跑仍未验收。

**v2.5 修订摘要**

- 冻结当前源码版本为 App/OpenAPI/Web `0.1.3`、Chart `0.1.5`，并记录最新不可变候选
  `0.1.3-rc-20261102152403` 及 registry digest `sha256:a2cf…60f3`；生产仍保持
  `0.1.2/0.1.4` revision 12、2/2 Ready、NodePort `32626`。
- 新增版本一致性门禁 `scripts/check-version.sh`，校验 Makefile、Chart、values、OpenAPI、
  npm package/lock 和 README，并纳入 `make verify` 与 CI；Helm 新增 `image.digest`，只接受
  `sha256:<64 hex>`，正向渲染为 `repo@digest`，非法 digest 在模板阶段失败。
- 回填最新本地证据：`make verify`、`make race`、`make capacity-test` 通过；最新复跑容量 P95 分别为
  Pod 列表 `22.950 ms`、工作负载 `193.397 ms`、Overview `199.418 ms`，Heap `28 MiB`；
  前端 typecheck、Vitest 9/9、三浏览器 E2E 27/27、生产构建和 bundle budget 通过，初始 JS
  gzip `339.78 KiB`，生产 LCP `492 ms`。
- 回填 Trivy 证据：Debian 12.15 与 Go binary 的 HIGH/CRITICAL 均为 0，扫描时间
  `2026-11-02T15:24:55Z`。该结果只证明该摘要对应镜像，不等于运行矩阵已验收。
- 记录隔离验收环境准备状态：namespace `dosh-ui-e2e`、`dosh-auth`/`dosh-tls` Secret 已创建，
  NodePort `32629` 已预留；Helm Release 尚未安装，OIDC/retry/soak/审计/SIGTERM/回滚仍需在
  最新 digest 上复跑。
- 本版不提升商用结论：企业 IdP、受信 TLS、真实 SIEM、Edge/Safari 实机、CI 真跑、生产
  digest 发布与回滚仍未验收。

**v2.4 修订摘要**

- 将阶段 0 的“待补路由测试/规则化门禁”回填为可复现事实：`make arch`、OpenAPI 双向路由测试和
  模块路由归属测试均已执行通过；`check-architecture.sh` 同时覆盖跨模块生产代码 import、越层依赖、
  非 bootstrap Router、`init()` 路由、任意 proxy/通配路由和环境变量集中读取。
- 修正配置验收口径：布尔、整数、浮点、duration 解析失败必须启动失败；`DOSH_COOKIE_SECURE=true`
  但未启用 TLS 必须拒绝启动。新增 `SYS-N-004`、`SYS-N-005` 及对应负向测试。
- 新增并落地 `UX-U-010` / WBS-1.8：顶部栏、关于弹窗和集群健康状态统一读取 `/api/v1/overview`，
  移除实验室集群名、Kubernetes 版本、CNI 和“连接正常”硬编码；失败时显示“集群状态不可用”和重试入口。
- 统一 WBS-0.x 编号与阶段 0 工作清单，避免同一编号同时表示 Makefile、Clean Architecture 和
  OpenAPI；将当前工作树的架构证据独立记录在 21.2.8。
- 本版仍不改变生产与商用结论：生产为 App `0.1.2` / Chart `0.1.4`、revision 12、NodePort `32626`；
  最终候选、企业 IdP、受信 TLS、真实 SIEM、Edge/Safari、CI 真跑和生产 digest 发布仍未验收。

**v2.3 修订摘要**

- 将 chi 路由边界从“代码评审约定”提升为可执行 EARS：新增路由归属、OpenAPI 路径双向覆盖、
  模块独立挂载和禁止绕过 `Mount` 的验收条款，避免路由散落在全局 Router 或 Handler 中。
- 统一本地与集群的验收前提：本地 Go 进程默认 HTTP，Helm 部署默认 HTTPS；本地阶段不再使用
  HTTPS 验收未启用 TLS 的进程。
- 明确第 15.7 节写操作示例使用隔离 Release 和非生产 NodePort，禁止把验收命令指向生产
  `dosh:32626`。

**v2.2 修订摘要**

- 补齐审计 Webhook 的 Cilium 出口：`CiliumNetworkPolicy` 现在为审计接收端单独渲染精确
  `toCIDR`/`toPorts`，审计开启但缺少 `networkPolicy.webhook.ipBlocks` 时 Helm 渲染失败。
- 回填 `scripts/check-helm.sh` 的审计 CNP 正向断言、精确 CIDR/端口断言和“缺 CIDR”负向测试；
  该状态只代表 Helm 渲染已验证，不代表隔离集群投递已验收。
- 新增并校正 `scripts/e2e-audit-webhook.sh`：参数化 OIDC/RBAC fixture、私 CA、Token/HMAC 三种模式、
  业务事件矩阵、503/超时/队列压力、SIGTERM 排空和泄露扫描；超时测试显式切换
  `audit.webhook.timeout=1s` 后再恢复，避免假失败。
- 校正 22.14 的执行命令：明确脚本通过 `DOSH_E2E_*` 环境变量配置，不使用不存在的 CLI 参数；
  当前接收端由宿主机 Go 二进制启动，测试接收端镜像尚未纳入强制门禁。

**v2.1 修订摘要**

- 新增 22.14“审计 Webhook 商用集成实施包”，把私 CA、Bearer-only/HMAC、Cilium 精确出口、
  隔离 Release fixture、业务事件矩阵、接收端故障、队列压力和 SIGTERM 排空拆成可逐项验收的步骤。
- 明确审计配置的 EARS 验收条款与证据格式，要求事件必须绑定 `requestId`、镜像 digest、Helm revision、
  Release 和测试时间；仅通过 `httptest` 或 Helm 渲染不得标记为“已验收”。
- 更新 1.2、1.6、10.1、15.9、22.5、26.6 和 26.9：记录工作树已加入私 CA/Token-only/测试接收端能力；
  该版发布时 Cilium 出口仍未渲染，真实隔离集成也未执行，v2.2 已单独修正该状态。
- 新增 25.7“最新源码重新放行核对单”，把版本冻结、全量门禁、审计集成、OIDC/retry/soak 回归、
  Trivy、生产发布与回滚串成一条不可跳过的新候选放行路径。
- 校正候选口径：旧候选 `0.1.3-rc-1793626263` 早于本轮审计配置改动，其 digest 和门禁结果不能代表
  当前工作树。新版本必须先冻结版本、完成全量门禁、构建不可变镜像并重新执行所有发布门禁。

**v2.0 修订摘要**

- 新增 1.6“实施工作包与验收台账”，把 WBS、设计决策、实现落点、自动验收、集群验收和当前状态放在同一张表内，确保每个步骤都能回答“做什么、在哪里做、怎么验、证据是什么、是否可以放行”。
- 回填 Deployment 创建向导：`POST /api/v1/workloads/deployments`、`/preview`、三步 React 向导、API Server Dry Run、稳定 409 `already_exists`、审计和真实集群创建对账。
- 更新最终候选事实：`0.1.3-rc-1793626263`、digest、隔离 Release、真实 retry 读路径注入和前一候选 OIDC 12/12 证据；未执行项继续保留为“未验收”。
- 修正 soak/retry 脚本可重复性：流重建前重新登录、Pod 路径 namespaced 化、readiness 单次采样、写请求补发 CSRF；修复脚本不等于通过验收，最终复跑仍为发布门禁。
- 不调整生产结论：生产仍为 `local` 单管理员、自签 TLS、App `0.1.2` / Chart `0.1.4`（revision 12），NodePort 保持 `32626`。

**v1.9 修订摘要**

- 安装并验收 Metrics Server `v0.9.1`：独立 namespace `dosh-metrics`、1/1 Running、
  `v1beta1.metrics.k8s.io` Available；隔离测试 Release 完成“可用 → 禁用 → 恢复”三态对账，
  证据为 `/tmp/tmp.MwkrC9s1Mj/metrics-evidence.log`。
- 新增 `internal/platform/kube/retry.go`：只对幂等读请求的 429/500/502/503/504 和可重试网络错误
  做最多 3 次指数退避，尊重 `Retry-After` 和 context cancellation，绝不自动重放写请求。
- 新增 `scripts/e2e-soak.sh`，把 30 分钟长连接、内存上限、事件流、日志流、Pod 删除和滚动重启
  固化为可重复验收脚本；脚本已写入但尚未执行，因此不提升 SOAK 相关需求状态。
- 回填 Metrics 的实验室证据和重试组件证据；生产发布、企业 IdP、受信 TLS、真实 429/5xx 注入、
  真实 SIEM、Edge/Safari 和 CI 真跑仍保持未验收。

**v1.8 修订摘要**

- 回填真实浏览器 OIDC 矩阵：Chromium、Firefox、WebKit 各 3 条用例，共 9/9 通过；WebKit 使用
  官方 Playwright 容器运行，避免宿主 GTK/ICU 依赖造成假失败。
- 回填 Token 泄露扫描：浏览器响应体/URL/DOM/Web Storage/Cookie、dosh 应用日志、
  ConfigMap/Deployment 和 Helm values 均无 JWT 或测试 access token；Kubernetes Secret 存储按设计排除。
- 回填隔离 Release 的 OIDC upgrade → rollback → restore：revision `11 → 12 → 13 → 14`，每个
  revision 都复验 health/ready、viewer/operator/admin API、审计和 RP logout。
- 将 AUTH-O-003、AUTH-O-004、AZ-O-002、SEC-N-004、TST-U-016 和阶段 8 的对应证据提升为
  “实验室已验收”；企业 IdP、生产发布和高风险非功能门禁仍保持未验收。
- 校正 OpenAPI 快照：当前 `api/openapi.yaml` 为 31 个路径、40 个 schema；验收状态和商用放行结论
  只按本节真实证据变化。

**v1.7 修订摘要**

- 回填 2026-11-02 签名测试 IdP 的隔离集群三身份矩阵：Release `dosh-oidc-e2e`、namespace
  `dosh-e2e-oidc`、NodePort `32627`、目标 namespace `dosh-e2e-target`；生产 `dosh`/`32626` 未改动。
- 记录镜像 `192.168.122.10:5000/dosh/dosh:0.1.3-oidc-e2e-1793621036` 及 kubelet 观测 digest
  `sha256:c8fda6d1d8cc5ddc22e4a60ca367f846149c1643b24658716e93bc82313ea839`，并记录
  Helm revision 2、viewer/operator/admin 结果、审计字段和 logout 结果。
- 更新 4.8、10.2、15.10、21.2.2、21.2.3、21.3.1、21.4、22.12、26.1 和风险登记：明确
  `SubjectAccessReview` 已实现，不再保留“当前仍使用 SSAR”的旧结论。
- 新增 1.5：定义每个 WBS 的实施、自动验收、集群验收、证据归档和回滚清理字段，确保每一步都能
  回答“做什么、落在哪里、怎么验、证据是什么”。
- 本版仍不改变商用结论：浏览器 OIDC E2E、企业 IdP、Token 泄露扫描、OIDC 回滚、生产镜像发布、
  受信 TLS、Metrics Server、soak/故障注入、真实 SIEM、Edge/Safari 和 CI 真跑仍未关闭。

**v1.6 修订摘要**

- 更新 1.2、10.2、15.10、21.2.2、22.12 和 26.1：回填 SAR、真实 RSA 签名 provider、OIDC HTTP 全链路、AppShell 退出和 Helm 双模式渲染证据。
- 新增 RP logout 绝对回跳设计：从 `DOSH_OIDC_REDIRECT_URL` 派生同源 `/login`，避免 IdP 把相对路径解析到自身域名。
- 明确本轮只达到“已实现/自动化门禁通过”，真实 IdP 三身份矩阵、已发布镜像、集群升级和回滚仍为未验收。

**v1.5 修订摘要**

- 新增 4.8：OIDC 登录、Kubernetes impersonation、SubjectAccessReview、fail-closed 和最小 RBAC 设计。
- 扩展 AUTH/AZ/SEC/TST 需求：把原来的 OIDC 占位项细化为可逐项验收的 EARS 条目。
- 新增 10.2、15.10、22.12：Helm 双模式契约、OIDC 验收命令和逐步骤实施/缺陷关闭表。
- 新增阶段 8：多用户身份与商用关闭，明确 WBS、产物、退出条件。
- 更新 21.2.2/21.3.1：记录当前工作树门禁和集群 `0.1.2/0.1.4` 事实；所有未执行项保持“未验收”。
- 修正 `0.1.2/0.1.4` 发布核对状态，保留 `0.1.1/0.1.3` 历史证据不覆盖。

### 1.5 每一步的实施与验收定义

本文件中的 WBS 只有同时具备下列字段，才算形成可执行、可审计的交付单元；任何字段只能引用
真实命令输出或已归档的验收记录，不能以口头结论替代。

| 字段 | 必须回答的问题 | 证据落点 |
|---|---|---|
| 需求追踪 | 对应哪些 EARS ID、优先级和拒绝语义 | 第 6–13、16 节 |
| 设计决策 | 端口、依赖方向、数据结构、失败策略和取舍是什么 | 第 4、22 节及 ADR |
| 实施动作 | 修改哪些文件、增加哪些配置/迁移/RBAC | WBS 的文件路径列 |
| 自动验收 | 哪个命令在何种夹具下验证成功与负向分支 | 第 15、21 节命令输出 |
| 集群验收 | 在哪个 namespace、Release、端口和真实依赖上验证 | 第 15、21、25 节 |
| 证据归档 | image tag/digest、Helm revision、requestId、审计日志、截图或报告在哪里 | 第 21、23、25 节 |
| 回滚与清理 | 失败时如何恢复上一 revision、如何删除测试资源且不触碰生产 | 第 19、25 节 |
| 放行结论 | 只能是“未开始/实现中/已实现/已验收”，并写明未关闭缺口 | 第 14、21、24、26 节 |

每个实施步骤的固定执行顺序如下：

1. 先补齐需求 ID、设计约束和验收命令，禁止先写代码再补验收口径。
2. 在最小纵向切片内实现 domain/port → app 用例 → adapter → HTTP/UI，并保持依赖方向。
3. 先跑组件与负向测试，再跑全量 race/vet/架构/契约门禁；失败不得进入集群验收。
4. 在隔离 namespace、独立 Release 和独立 NodePort 上部署候选，禁止直接覆盖生产 `dosh`。
5. 同时验证成功路径、拒绝路径、审计归属、依赖不可达和回滚；只验证成功路径不算通过。
6. 将命令、原始输出摘要、镜像 digest、Helm revision、测试资源清单和结论写入第 21 节。
7. 清理测试资源；若需保留现场，必须写明保留原因、责任人和截止时间。

### 1.6 实施工作包与验收台账

本表是实施执行索引；详细需求和命令仍以第 6–15、22、26 节为准。状态只能按以下口径理解：
“已实现”表示代码和测试已落盘；“实验室已验收”表示指定隔离环境有真实输出；“已验收”表示
最终发布候选和生产目标环境均有完整证据；任何未复跑项不得因脚本已修复而提升状态。

| WBS | 设计决策 | 实施动作与文件落点 | 自动验收 | 集群/人工验收 | 当前状态 |
|---|---|---|---|---|---|
| WBS-0.1 工程基线 | 单二进制同源提供 SPA 与 API，减少独立前端服务 | `go.mod`、`web/`、`Dockerfile`、`Makefile`；`go:embed` 嵌入构建产物 | `make build`、`make web-build`、`go test ./...` | 容器启动后同端口访问 SPA/API | 实验室已验收 |
| WBS-0.2 Clean Architecture | `http → app → domain`；`platform` 实现消费者端口；`bootstrap` 唯一装配根；生产代码禁止跨模块 HTTP import | `internal/domain/*`、`internal/modules/*/{app,http}`、`internal/platform/*`、`internal/bootstrap/*`、`scripts/check-architecture.sh` | `make arch`：检查越层依赖、跨模块 import、环境变量入口和 Router 创建位置 | 架构评审确认无模块互导、无万能 Repository、无 Handler 内 K8s 调用 | 当前候选本地全量门禁已通过；CI 真跑待补 |
| WBS-0.3 chi 路由边界 | chi 只做 transport；每模块 `Mount(chi.Router)`；禁止通配 API 代理；路由与 OpenAPI 双向一致 | `internal/bootstrap/router.go`、各模块 `http/handler.go`、`internal/bootstrap/routes_test.go`、`scripts/check-architecture.sh` | `make arch`、`TestRouteInventoryMatchesOpenAPI`、`TestModuleRoutesAreScoped`、HTTP handler 测试 | 评审每个新增路由均有 OpenAPI、授权、CSRF 和审计 | 当前候选本地路由门禁已通过；CI 真跑待补 |
| WBS-0.4 API 契约 | OpenAPI 3.1 是唯一契约源，前端类型由契约生成 | `api/openapi.yaml`、`web/src/api/schema.d.ts`、`web/src/api/types.ts` | `make api-check`、`npm run typecheck` | 契约评审确认错误码、权限和 409/422 语义 | 实验室已验收 |
| WBS-1.1 配置与启动 | 启动即严格校验；默认 `:32626`；敏感配置只从 Secret 注入 | `internal/platform/config/config.go`、`cmd/dosh/main.go`、Helm values/templates | `go test ./internal/platform/config ./internal/bootstrap` | 日志核对 version/listen/auth/TLS，且无敏感值 | 实验室已验收 |
| WBS-1.2 HTTP 服务与生命周期 | RequestID、恢复、安全头、访问日志、压缩、SPA fallback；长连接不被全局 REST timeout 截断 | `internal/platform/httpserver`、`internal/platform/httpx`、`internal/bootstrap/spa.go` | handler/security header 测试；SSE/日志流集成测试 | `/healthz`、`/readyz`、SPA fallback、401/403/413、日志流 | 实验室已验收 |
| WBS-1.3 CCE 风格前端壳 | 顶部全局栏 + 左侧导航 + 主内容；shadcn token；1024 px 以下折叠；禁止复制社区面板资产 | `web/src/components/layout/*`、`web/src/index.css`、`router.tsx`、各 feature 页面 | `npm run typecheck`、Vitest、bundle check、静态 HTML 注入扫描 | 三浏览器视觉/响应式验收、390 px 导航 | 实验室已验收；Edge/Safari 实机待补 |
| WBS-1.8 动态集群上下文 | `/api/v1/overview` 是集群名、版本、CNI、健康状态的唯一 UI 事实源；加载失败显示不可用，不伪造绿色连接 | `web/src/components/layout/app-shell.tsx`、`web/src/features/overview/use-overview-query.ts`、`overview-page.tsx`、组件/MSW/E2E fixture | Vitest/MSW 覆盖 Healthy、Degraded 和 503；浏览器 E2E 用非实验室名称/版本/CNI 断言数据来自 API | 历史候选在 `dosh-ui-e2e` revision 3 下真实三浏览器 3/3；v2.45 当前候选三浏览器 mock 各 38/38、真实 OIDC 各 16/16 | 实验室已验收（当前候选三浏览器） |
| WBS-2.1 本地认证 | Argon2id 哈希、AES-GCM 会话、HttpOnly/Secure/SameSite、CSRF、登录限速 | `internal/modules/auth/{app,http}`、`dosh-auth` Secret | auth app/http 负向测试、Cookie/CSRF 测试 | 登录、退出、连续失败限速、无 CSRF 写拒绝 | 实验室已验收 |
| WBS-2.2 授权边界 | UI capability 只做展示；app 用例逐次授权；拒绝时 Repository 调用为 0 | `internal/domain/types.go`、`internal/platform/authz`、各 modules app | fake Authorizer/Repository 调用次数断言 | viewer/operator/admin 实际 API 矩阵 | 测试 IdP 实验室已验收；企业 IdP 待补 |
| WBS-3.1 总览与节点 | adapter 统一转换为 domain DTO；Metrics 缺失时显式降级，不伪造 0 | `internal/modules/overview`、`internal/modules/nodes`、`internal/platform/kube/{nodes,metrics}.go` | `go test -race ./internal/modules/... ./internal/platform/kube` | 与 `kubectl get nodes`、`kubectl top` 对账 | 实验室已验收 |
| WBS-3.2 工作负载与 Pod | 白名单 kind、并发 resourceVersion、Pod 日志流、URL 筛选状态 | `internal/modules/workloads`、`internal/platform/kube/{workloads,pods}.go`、`web/src/features/workloads/*` | app/http/kube fake 测试、路由测试、Vitest | 真实集群列表/详情/扩容/重启/删除/日志、409 冲突 | 实验室已验收 |
| WBS-4.1 事件与日志流 | SSE 与日志都采用请求 context 生命周期；客户端断开即释放；不缓存敏感内容 | `internal/modules/events`、workloads logs、`web/src/features/workloads/log-viewer.tsx` | SSE 帧、cancel、断开和首批日志测试 | 临时 ConfigMap 事件、CoreDNS 日志、断线重连 | v2.44 历史候选 30 分钟 soak 实验室已验收；v2.45 未复跑，生产同等并发待复验 |
| WBS-4.7 日志与事件安全回归 | 日志和事件只作为文本渲染；ANSI/OSC/DCS/C0-C1 控制序列跨 chunk 净化；日志不进入浏览器持久存储 | `web/src/features/workloads/{log-sanitizer.ts,log-viewer.tsx}`、`web/src/features/workloads/*.test.*`、`web/e2e/dosh.spec.ts` | `npm test`、`npm run typecheck`、受限容器 Chromium/Firefox Playwright、源码静态扫描 | HTML/`javascript:` OSC 不创建 DOM、不执行脚本；Vitest 46/46、mock 各 34/34 | v2.40 实验室已验收 |
| WBS-4.8 审计历史查询 | 管理员可见当前实例最近 1000 条审计记录；有界环形缓冲、进程内快照；只做排查入口，不替代 stdout/SIEM/WORM | `internal/domain/types.go`、`internal/platform/audit/history.go`、`internal/modules/audithistory/**`、`internal/bootstrap/{app,router,routes_test}.go`、`api/openapi.yaml`、`web/src/features/audit/*`、`web/src/router.tsx` | app/http/audit race 测试；OpenAPI/TS typecheck；Vitest/MSW；mock Chromium/Firefox；真实 OIDC 权限/过滤/分页/排序/400；真实本地认证 | 未认证 401、viewer/operator 403、admin 200；登录、scale、Secret reveal、denied/failure 可查询；重启后清空且有容量上界 | v2.45 当前候选实验室已验收；企业 SIEM、持久化/WORM 与生产发布仍未验收 |
| WBS-4.2 配置与 Secret | 列表只返回元数据；reveal 需敏感读权限、二次确认、`no-store` 和审计 | `internal/modules/configuration`、`platform/kube/configuration.go`、配置页 | 脱敏/reveal/授权/审计测试 | 真实 Secret reveal 与 YAML 脱敏 | 实验室已验收；企业 IdP 待复验 |
| WBS-5.1 安全写操作 | 前端确认 + 后端授权 + CSRF + audit；写请求不自动重试 | `internal/modules/workloads/app/service.go`、`platform/kube/workloads.go`、`scripts/e2e-workload-lifecycle.sh` | scale/restart/delete 成功、400、403、409、审计测试；当前 digest 独立夹具对账 | `make e2e-workload-lifecycle`：预览→创建→扩容→镜像更新→重启→删除，kubectl/审计/清理全部通过 | 当前候选实验室已验收；证据见 21.2.36 |
| WBS-5.8 工作负载镜像更新 | 仅 Deployment/StatefulSet/DaemonSet；容器级镜像 DTO；GET→resourceVersion 校验→typed Update；旧版本且目标镜像不同返回 409，目标已达成时幂等成功；Job/CronJob 501；独立 `updateImage` capability 和 `update-image` 审计 | `internal/domain/{types}.go`、`workloads/app/{service,image}.go`、`workloads/http/handler.go`、`platform/kube/workloads.go`、`api/openapi.yaml`、`web/src/features/workloads/workload-detail-page.tsx` | app/http/kube 定向测试、fake client 冲突/幂等/缺失容器、OpenAPI/TS typecheck、Vitest 51/51、隔离 mock Chromium/Firefox 各 37/37、隔离集群 kubectl 对账 | 隔离集群更新三种工作负载、kubectl 对账、其他容器不变、409 不覆盖、Job/CronJob 501、审计检索 | 已完成当前候选真实隔离集群验收；证据见 21.2.35 |
| WBS-5.9 CronJob 暂停/恢复 | 仅 CronJob；专用 `suspend`/`resume` 用例；独立 `suspendCronJob` capability；GET→resourceVersion 校验→typed Update；目标状态未生效的旧版本返回 409，目标状态已生效时幂等成功；非 CronJob 501；审计动作分别为 `suspend`/`resume` | `internal/domain/types.go`、`workloads/app/{service,suspend}.go`、`workloads/http/handler.go`、`platform/kube/workloads.go`、`api/openapi.yaml`、`web/src/features/workloads/workload-detail-page.tsx` | app/http/kube 定向测试、fake client 冲突/幂等、OpenAPI/TS typecheck、Vitest 51/51、隔离容器 mock Chromium/Firefox 各 37/37、隔离集群 75 秒暂停窗口与恢复调度对账 | 隔离集群暂停→确认不再创建新 Job→恢复、kubectl 对账、已有 Job 不变、409 不覆盖、非 CronJob 501、审计检索 | 已完成当前候选真实隔离集群验收；证据见 21.2.35 |
| WBS-5.2 YAML Dry Run/Apply | 白名单资源；严格单文档并拒绝重复键；服务端 Dry Run 成功后才 apply；拒绝任意 API 透传 | `internal/modules/yaml`、`platform/kube/yaml.go`、`web/src/components/resources/yaml-dialog.tsx` | 解析、多文档/重复键、白名单、越权、Dry Run 失败不写入测试 | ConfigMap/Deployment Dry Run 和 apply 真实对账；当前 digest API 回归 | 当前候选实验室已验收；生产发布门禁未完成 |
| WBS-5.4 Deployment 创建向导 | 专用 DTO 而非任意 YAML；app 校验名称/镜像/端口/资源/Env/Label；先 API Server Dry Run 再创建；重复创建稳定 409 | `internal/domain/deployment.go`、`workloads/app/deployment.go`、`platform/kube/deployment_create.go`、`workloads/http/handler.go`、`web/src/features/workloads/deployment-create-dialog.tsx` | app/http/kube 定向 race 测试；OpenAPI/TS typecheck | operator 创建、viewer 403、非法字段 400、重复 409、kubectl 对账、审计 | 当前候选隔离集群已通过 preview/create、400、409、RBAC 和 kubectl 对账；v2.36 mock Chromium/Firefox 各 28/28 均包含向导创建；历史三浏览器 42/42 不继承 |
| WBS-6.1 Service/Ingress 详情 | 列表可进入详情；Service 展示 EndpointSlice 就绪/目标；Ingress 展示默认后端、TLS Secret 和路由目标状态；依赖缺失不白屏 | `internal/domain/types.go`、`internal/modules/network/{app,http}`、`internal/platform/kube/network.go`、`web/src/features/network/*` | app/http/kube 定向测试、OpenAPI/TS typecheck、Vitest、隔离 Playwright | 真实隔离 API 与 Chromium/Firefox 深链 | v2.36 实验室已验收 |
| WBS-6.2 Metrics | 独立 `dosh-metrics`；探测缓存；实际 usage 与 requests 双轨；三态降级/恢复 | `platform/kube/metrics.go`、domain DTO、总览/节点/Pod 页面、`scripts/install-metrics-server.sh`、`scripts/e2e-metrics.sh` | fake Metrics client 测试 | `v1beta1.metrics.k8s.io` Available、禁用、恢复；`kubectl top` 对账 | v2.45 当前候选实验室已验收（证据见 21.2.40）；生产 Metrics 拓扑待验收 |
| WBS-6.3 StorageClass/PV/PVC 详情 | 列表名称与绑定关系可进入详情；StorageClass 展示参数/回收策略/卷绑定/扩容；PV 展示容量/卷模式/CSI/绑定 PVC；PVC 展示申请/实际容量、绑定 PV 和 conditions；均提供元数据、事件与 YAML | `internal/domain/types.go`、`internal/modules/storage/{app,http}`、`internal/platform/kube/storage.go`、`web/src/features/storage/*`、`web/src/router.tsx` | app/http/kube 定向测试、OpenAPI/TS typecheck、Vitest、隔离 Playwright | 临时 StorageClass/PV/PVC 真实详情/YAML/conditions 对账；清理后 404；Chromium/Firefox 本地认证 | v2.37 实验室已验收 |
| WBS-6.8 命名空间详情 | 列表名称进入详情；展示状态、标签、注解、创建时间、ResourceQuota hard/used、LimitRange min/max/default/defaultRequest；提供事件与 YAML 深链；空配额/限制有明确状态 | `internal/domain/types.go`、`internal/modules/nodes/{app,http}`、`internal/platform/kube/nodes.go`、`deploy/helm/dosh/templates/rbac.yaml`、`web/src/features/namespaces/*`、`web/src/router.tsx` | nodes app/http/kube 单测、OpenAPI/TS typecheck、Helm RBAC 断言、Vitest、隔离 Playwright | 临时命名空间真实配额/限制/YAML 对账；删除后 404；Chromium/Firefox mock 与真实本地认证 | v2.38 实验室已验收 |
| WBS-7.1 自动化门禁 | 组件/负向/race/前端/契约/Helm/容量/安全统一命令化 | `Makefile`、`web/package.json`、`scripts/*`、`.github/workflows/ci.yml` | `gofmt`、`go vet`、`go test -race ./...`、typecheck/test/build/bundle、`make arch/api-check/helm-check` | CI artifact 和分支保护 | 本地全量门禁已通过；GitHub Actions 真跑和分支保护待完成 |
| WBS-7.2 Helm 加固 | 单一模板支持 local/oidc/审计 Webhook；32626、2 副本、PDB、非 root、只读根、NetworkPolicy/CNP；敏感值只从 Secret 注入 | `deploy/helm/dosh/**`、`scripts/check-helm.sh` | `make helm-check`；local/oidc/Cilium/审计正向与非法配置负向断言 | `kubectl -n dosh get deploy,svc,pod,pdb,sa` 与 Pod SecurityContext | 最新 Chart `0.1.6` 已在隔离本地/OIDC/审计 Release 以 digest 部署；生产发布待执行 |
| WBS-7.3 发布与回滚 | 不可变 digest、独立测试 Release、保留上一 revision、禁止测试触碰生产 | `docs/operations-runbook.md`、`scripts/e2e-oidc-rollback.sh`、Helm history | 脚本语法、镜像 digest 校验 | upgrade→rollback→restore、health/login、矩阵复验 | v2.44 历史候选已通过 revision `181→182→183→184`；v2.45 未复跑，生产 digest 发布/回滚未执行 |
| WBS-8.1 OIDC 身份 | Authorization Code + S256 PKCE；state/nonce/verifier；签名/iss/aud/exp；Token 不落地 | `platform/oidc`、`auth/app/oidc.go`、`auth/http/handler.go`、登录页/用户菜单 | provider/auth app/http 负向测试、Token 扫描 | 测试 IdP 登录、logout；当前候选 Chromium/Firefox/WebKit 真实 OIDC 各 16/16 | 测试 IdP + 三浏览器容器实验室已验收；企业 IdP、Safari/Edge 实机未验收 |
| WBS-8.2 Kubernetes 授权 | OIDC 模式 SAR + impersonation；SA 最小权限；SAR 不带 impersonation；失败 fail-closed | `platform/authz`、`platform/kube/{authorization,impersonation}.go`、Helm RBAC | SAR header/object/EvaluationError/Repository 零调用测试 | viewer/operator/admin 三身份 API 与审计矩阵 | 实验室已验收 |
| WBS-8.3 商用关闭 | 企业 IdP、受信 TLS、soak、真实故障注入、SIEM、Edge/Safari、CI、digest 发布 | 第 26 节各实施包与运行手册 | 对应专项脚本/报告 | 目标生产或预发环境验收 | 未完成；禁止标记为完整可商用 |
| WBS-8.11 审计 Webhook 商用闭环 | 审计旁路不阻塞业务；TLS 严格校验；Bearer/HMAC 可组合；有界队列、有限重试、关闭排空；出口最小化 | `internal/platform/audit`、`platform/config`、Helm audit values/templates、CNP、`tools/auditreceiver`、`scripts/e2e-audit-webhook.sh` | audit/config/auditreceiver race tests；`make helm-check` 审计正负向断言 | 隔离 Release 私 CA、双认证、业务事件、失败审计、503/超时/恢复、队列满、SIGTERM 排空；真实 Loki 59 事件/34 requestId/租户隔离 | 当前候选实验室已验收；企业 SIEM 未验收 |

### 1.7 从设计到放行的实施门禁

本节给出执行摘要。每个门禁都必须按顺序完成；前一项没有可复现证据，不得进入下一项。具体需求编号、
文件落点和完整命令分别见第 14、15、22、25 和 26 节。

| 门禁 | 设计方案与本步要做的事 | 必须产物 | 验收命令或检查 | 通过标准 |
|---|---|---|---|---|
| G0 需求与设计冻结 | 为每个用例分配 EARS ID、优先级、错误语义和验收方式；冻结 `http → app → domain` 依赖方向、模块端口、OpenAPI、Helm 契约和隔离测试资源命名；先评审设计再写代码 | 第 6–13 节需求表、`api/openapi.yaml`、第 4 节架构图与 ADR、第 22 节实施包 | `make arch`、`make api-check`、设计评审记录 | 每个 P0 需求都有实现位置和验收判据；路由、DTO、权限和失败语义无歧义；不存在跨模块 HTTP/Kubernetes 依赖 |
| G1 纵向切片实现 | 每个功能按 domain DTO/错误 → app 端口与用例 → kube adapter → HTTP handler → React feature 的顺序实现；先做总览和只读资源，再做日志/事件流，最后做受控写操作 | `internal/domain/*`、`internal/modules/*`、`internal/platform/kube/*`、`web/src/features/*`、契约测试 | 模块定向 `go test -race`、handler 测试、Vitest/MSW、OpenAPI 路由对账 | 成功、拒绝、依赖失败三条路径都有测试；Handler 不直接调用 Kubernetes；无万能 CRUD 端口 |
| G2 本地全量门禁 | 统一执行格式、vet、全量 race、架构、契约、前端类型/单测/构建、Helm 渲染、容量和依赖漏洞检查；修复后从最终源码重新构建，不能复用旧镜像 | `make release-check`、`make verify`、`make race`、Vitest/Playwright 报告、Helm 渲染结果、容量报告、漏洞报告 | `make release-check TAG=<version>`；其串联 `verify → race → Vitest → capacity → E2E → performance → bundle → evidence → image → image-scan` | 全部命令退出码为 0；没有新增 HIGH/CRITICAL；前端预算不退化；路由/TS 类型零漂移 |
| G3 镜像身份与供应链 | 构建最小非 root 镜像，推送后读取 registry 返回的 manifest digest；后续 Helm、扫描和所有 E2E 只接受 `tag@digest`，禁止用可变 tag 作为发布身份 | 候选 tag、64 位 registry digest、二进制版本、Trivy 报告、构建时间 | `podman push`、registry manifest 查询、`make image-scan TAG=<tag>`、`make version-check` | digest 与 tag 对应且不可变；App/Chart/OpenAPI/Web 版本一致；镜像基础层和 Go 二进制无未接受 HIGH/CRITICAL |
| G4 隔离部署与基础运行 | 在独立 namespace、独立 Release、独立 NodePort 部署；设置非实验室集群名；检查 2/2 Ready、PDB、分布、健康检查、登录、SPA/API 同端口和生产快照不变 | Helm revision、Pod/Service 清单、健康与登录输出、生产前后快照 | `helm upgrade --install ... --wait`、`kubectl rollout status`、`curl /healthz /readyz`、登录与 capabilities 调用 | 2/2 Ready；NodePort/namespace 不冲突；Deployment 使用 digest；生产 `dosh` 的 revision、镜像和 `32626` 未变化 |
| G5 功能、授权与浏览器矩阵 | 使用真实集群分别验证 viewer/operator/admin；覆盖列表、详情、日志/事件、创建向导、扩容/重启/删除、Secret reveal、YAML Dry Run 和 409；浏览器不得使用 API mock | API 响应与 kubectl 对账、requestId、审计事件、三浏览器截图/报告、Token 扫描结果 | 第 15.7、15.8、15.10 节脚本与 Playwright；`rg` 扫描日志/URL/DOM/Storage | 成功和拒绝状态符合契约；401/403/400/409 语义准确；无权限时不产生业务写；三个浏览器通过；Token/Secret 泄露 0 命中 |
| G6 故障、指标与 soak | 注入 429/5xx、连接重置、客户端取消、Metrics 三态、SSE/日志断线、Pod 删除和滚动重启；审计 Webhook 覆盖私 CA、双认证、超时、队列满与 SIGTERM 排空 | retry 时间线、Metrics 三态报告、soak 原始日志、审计接收端统计、滚动重启时间线 | 第 15.9、22.13、22.14、26.3、26.4 节脚本 | 读请求最多 3 次且尊重 `Retry-After`；写请求仅 1 次；Metrics 无陈旧值；soak 满足 `app_errors=0`、内存 ≤256 MiB、事件/日志帧 >0、重连 ≥1、流失败 0；审计已入队记录不丢且排空有界 |
| G7 回滚与恢复演练 | 在隔离 Release 执行 upgrade → rollback → restore；每个 revision 复验 health、ready、登录和三身份关键读；记录 Helm history、Pod digest 和恢复耗时 | Helm history、三个 revision 的 Pod 镜像、健康/登录输出、故障与恢复时间线 | `scripts/e2e-oidc-rollback.sh`；`helm history`；`kubectl get pod -o jsonpath` | 三个 revision 均可用；回滚后健康/登录正常；最终恢复到候选 digest；生产和其他 namespace 不受影响 |
| G8 证据归档 | 将 digest、revision、namespace、命令、原始输出、截图/报告和 SHA-256 清单归档；`summary.txt` 必须被 manifest 覆盖，manifest 自身摘要单独锚定；临时 `/tmp` 目录必须在发布前复制到受控存储，并进一步进入离机 WORM/长期保留 | `evidence/<digest>/`、`manifest.sha256`、`manifest-digest.txt`、`summary.txt`、验收单、风险接受记录 | `make evidence-check`、`sha256sum -c manifest.sha256`、`sha256sum -c manifest-digest.txt`、第 23 节模板复核 | 每个结论都能定位到不可变证据；证据与候选 digest/Chart revision 绑定；本机归档、离机 WORM 和访问审批分别记录，不得混写；无证据项保持未验收 |
| G9 生产发布与回滚 | 备份 values/Secret/revision；使用 digest 发布到 `dosh`；保留上一 revision；执行生产烟测、回滚和再恢复 | 生产变更单、发布前后快照、Helm history、业务烟测、回滚记录 | `helm upgrade --wait`、`kubectl rollout status`、健康/登录/核心 API、`helm rollback` | 2/2 Ready、NodePort 32626、核心路径通过；回滚和恢复成功；失败时有明确负责人和停止条件 |

**放行规则**：G0–G8 是进入生产的前提；G9 只能由发布负责人按第 19、25 节执行。任一 P0 门禁失败，
候选必须停止流转并保留现场；若属于夹具或外部依赖问题，必须先证明产品行为未受影响并重新执行，
不能以“重跑后偶然通过”替代根因关闭。

## 2. 目标与边界

### 2.1 产品目标

- 提供一套自研的 Kubernetes 集群管理面板，不采用 Headlamp、Kubernetes Dashboard 等社区面板的 UI。
- 信息架构、页面层级、表格密度和操作习惯参考华为云 CCE，但使用独立的品牌和视觉 token。
- 使用 Go、React、TypeScript 和 shadcn/ui 实现，前后端契约稳定、模块边界清晰。
- 首先服务当前 3 Control Plane + 5 Worker 的实验集群，并具备后续扩展为生产级面板的基础。

### 2.2 首版不包含

- Pod `exec`、端口转发和文件传输。
- 多集群统一管理。
- Helm Release 管理。
- 通用 CRD 编辑器和任意 Kubernetes API 代理。
- Prometheus 长期指标、日志中心和多租户计费。
- 节点 drain、节点池、自动扩缩容策略和集群升级。

### 2.3 当前环境基线

- Kubernetes：`v1.36.5`。
- 节点：3 个 Control Plane + 5 个 Worker，每节点 2 vCPU / 4 GiB。
- CNI：Cilium 1.20.2，kube-proxy replacement。
- K8s API VIP：`192.168.122.50:16443`。
- 镜像仓库：`192.168.122.10:5000`，当前使用自签 TLS。
- 当前没有 Ingress Controller。
- Metrics Server `v0.9.1` 已部署在独立 namespace `dosh-metrics`，1/1 Running；APIService
  `v1beta1.metrics.k8s.io` Available，`kubectl top nodes` 和 `kubectl top pods -A` 可用。
  Metrics Server 不属于 `dosh` Helm Release，不影响生产 App `0.1.2` / Chart `0.1.4` 的版本事实。
- 当前没有 StorageClass、PV 和 PVC。
- 已创建名为 `dosh` 的命名空间，当前 Release 为 `dosh`，已部署 App `0.1.2` / Chart `0.1.4`，Helm revision 12，2/2 Pod Ready。
- NodePort 默认范围是 `30000–32767`，`32626` 可直接使用。
- 3 个 Control Plane 带有 `node-role.kubernetes.io/control-plane:NoSchedule` Taint；应用副本默认调度到 5 个 Worker。
- 镜像仓库 `192.168.122.10:5000` 已可由集群节点拉取；本次镜像大小为 11,230,789 字节。

## 3. EARS 约定

本文档使用以下 EARS 模式：

| 类型 | 含义 | 中文模板 |
|---|---|---|
| U | Ubiquitous | 系统应…… |
| E | Event-driven | 当……时，系统应…… |
| S | State-driven | 当处于……状态时，系统应…… |
| O | Optional | 当启用……能力时，系统应…… |
| N | Unwanted | 如果……，系统应…… |
| C | Complex | 当处于……状态且发生……时，系统应…… |

需求优先级：

| 优先级 | 含义 |
|---|---|
| P0 | 发布阻断项，必须完成 |
| P1 | 首版目标项，未完成需有风险接受记录 |
| P2 | 首版后增强项 |

验收状态：

| 状态 | 含义 |
|---|---|
| 未开始 | 尚未实现 |
| 实现中 | 已开始开发 |
| 已实现 | 代码和自动化测试完成 |
| 已验收 | 通过本文件规定的验收门槛 |

## 4. 架构契约

### 4.1 总体架构

```text
Browser
  │ HTTPS / Same-Origin
  │ OIDC Authorization Code + PKCE（可选）
  ▼
Identity Provider ── discovery / JWKS / token / logout
  │
  ▼
Go BFF + embedded React SPA
  ├── client-go ────────► Kubernetes API（OIDC 模式按请求 impersonation）
  ├── metrics client ───► metrics.k8s.io（可选）
  ├── SSE / stream ─────► Events、Pod Logs
  └── auth / audit ─────► OIDC issuer、SubjectAccessReview、Secret、stdout、可选审计 Webhook
```

### 4.2 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go 1.26、chi、client-go、`k8s.io/metrics` |
| 前端 | React、TypeScript、Vite |
| UI | Tailwind CSS、shadcn/ui、Lucide |
| 路由与数据 | TanStack Router、TanStack Query、TanStack Table |
| 图表与代码 | Recharts、CodeMirror 6 |
| API 契约 | OpenAPI 3.1 作为唯一契约源，生成 TypeScript 类型；Go 侧以 DTO 和契约测试对账 |
| 部署 | Helm、Podman、自建 OCI Registry |
| 测试 | Go testing、Vitest、Testing Library、MSW、Playwright |
| 持续集成 | GitHub Actions；Go/Node/Helm/Playwright/kubeconform/govulncheck/Trivy 固定版本 |

### 4.3 模块结构

```text
cmd/dosh/main.go

internal/
  bootstrap/
    app.go
    router.go
    spa.go
  platform/
    config/
    httpserver/
    httpx/
    requestid/
    audit/
    authz/                 # Local / Kubernetes 授权策略
    oidc/                  # OIDC discovery、JWKS、token exchange、RP logout
    kube/                  # client-go adapter，按资源拆文件
  modules/
    auth/
      app/                 # Argon2id、会话封包、限速、能力判定
      http/                # 登录、Cookie、CSRF、会话上下文
    overview/  { app/, http/ }
    nodes/     { app/, http/ }
    workloads/ { app/, http/ }   # 同时承载 Pod 和日志
    network/   { app/, http/ }
    configuration/ { app/, http/ }
    storage/   { app/, http/ }
    events/    { app/, http/ }
    yaml/      { app/, http/ }

internal/domain/               # 纯业务类型、错误语义、跨模块窄接口，不依赖传输层

api/
  openapi.yaml

web/
  src/
    api/                 # 类型化客户端与 OpenAPI 生成类型
    components/          # ui、layout、data、feedback、resources
    features/            # overview/nodes/workloads/network/configuration/storage/namespaces/events
    routes/
    test/
    router.tsx
    main.tsx

deploy/
  helm/dosh/
```

### 4.3.1 模块职责与端口矩阵

每个模块独占自己的路由前缀和用例边界。`app` 只声明当前用例真正需要的出站方法，适配器必须实现这些
消费者端口；禁止把 `dynamic.Interface`、完整 clientset 或跨模块 Service 直接传入 Handler。

下表描述端口职责而不是强制接口名。当前实现由每个模块的单一 `Repository` 承载本模块所需方法；
只有同一模块出现两组互不相关的用例依赖时，才按用途拆分，不能为了“看起来更 clean”制造空接口。

| 模块 | HTTP 入口（均位于 `/api/v1`） | app 用例职责 | 消费者定义的出站能力 | 实现适配器 |
|---|---|---|---|---|
| `auth` | `GET /auth/config`、`POST/GET/DELETE /session`、`GET /auth/oidc/{login,callback,logout}`、`GET /capabilities` | 登录、OIDC state/nonce/PKCE、退出、会话封包、CSRF 上下文、能力判定、登录限速 | `Authorizer`（由 `platform/authz` 实现）；`OIDCProvider`（由 `platform/oidc` 实现） | `auth/http` + `auth/app` + `platform/authz` + `platform/oidc` |
| `overview` | `GET /overview` | 集群信息、计数、容量和最近事件聚合 | `Repository`：版本、Metrics 探测、CNI 探测、节点、Pod、工作负载、命名空间和事件的最小读取集合 | `platform/kube` |
| `nodes` | `GET /nodes`、`GET /nodes/{name}`、`POST /nodes/{name}/cordon`、`POST /nodes/{name}/uncordon`、`GET /namespaces`、`GET /namespaces/{name}` | 节点与命名空间列表/详情、Conditions、Taints、节点 Pod、Cordon/Uncordon | `Repository`：节点读取、节点调度状态条件写入、节点 Pod 和命名空间读取；`Authorizer` 与 `Auditor` 由 app 用例调用 | `platform/kube` |
| `workloads` | `GET /workloads`、详情、`scale`、`restart`、`DELETE`、`GET /pods`、Pod 详情和日志 | 工作负载读写、Pod 读取、日志流、状态推导、并发版本校验和审计 | `Repository`：工作负载读写、Pod 读取和日志流 | `platform/kube` |
| `events` | `GET /events`、`GET /events/stream` | 事件列表、watch 转换和 SSE 生命周期 | `Repository`：事件列表和 watch | `platform/kube` |
| `network` | `GET /services`、`GET /services/{namespace}/{name}`、`GET /ingresses`、`GET /ingresses/{namespace}/{name}` | Service/Ingress 列表、详情、EndpointSlice 就绪和路由目标状态映射 | `Repository`：Service、EndpointSlice 和 Ingress 读取 | `platform/kube` |
| `configuration` | `GET /configmaps`、`GET /configmaps/{namespace}/{name}`、`GET /secrets`、`POST /secrets/{namespace}/{name}/reveal` | 配置读取、Secret 脱敏、显式 reveal、写权限和审计 | `Repository`：ConfigMap/Secret 读取和显式 reveal | `platform/kube` |
| `storage` | `GET /storageclasses`、`GET /storageclasses/{name}`、`GET /persistentvolumes`、`GET /persistentvolumes/{name}`、`GET /persistentvolumeclaims`、`GET /persistentvolumeclaims/{namespace}/{name}` | 存储资源列表、详情、绑定关系和空状态语义 | `Repository`：StorageClass、PV、PVC 读取 | `platform/kube` |
| `yaml` | `GET /yaml`、`POST /yaml/apply` | 允许列表解析、脱敏、Dry Run、apply 和审计 | `Repository`：YAML 读取和 apply | `platform/kube` |

跨模块复用时，在消费者侧声明最小接口，由 `bootstrap` 注入同一个 `platform/kube` 适配器；禁止模块间
直接 import 对方 `app` 或 `http` 包。端口方法必须对应真实用例和测试，不得预埋通用 CRUD 方法。

端口契约的最小验收是：

```bash
make arch
go list -f '{{.ImportPath}} -> {{join .Imports " "}}' ./internal/modules/... | rg 'internal/modules/.+/(app|http)'
rg -n 'type Repository interface' internal/modules/*/app/service.go
```

前一条必须通过；后两条用于评审接口是否只包含该模块所需方法。新增方法必须对应一个实际用例和测试，
不得为了“以后可能用到”扩大端口。

依赖方向必须为：

```text
adapter/http ──► app ──► domain
adapter/kube ──► app/ports、domain
bootstrap ─────► 所有 adapter，并完成装配
```

### 4.4 架构需求

| ID | 类型 | 优先级 | 需求 | 验收方式 |
|---|---|---|---|---|
| ARC-U-001 | U | P0 | 系统应使用 chi 仅作为最外层 HTTP transport 实现。 | 依赖检查确认 `domain` 与 `app` 不导入 chi。 |
| ARC-U-002 | U | P0 | 系统应保证 `domain` 仅依赖 Go 标准库和模块内纯业务代码，不依赖 client-go、chi、Gin、HTTP 或 Kubernetes API 类型。 | `depguard` 或 `go-arch-lint` 通过。 |
| ARC-U-003 | U | P0 | 系统应保证 `app` 层不导入 chi、`net/http`、client-go 和具体 adapter。 | 架构检查和 CI 通过。 |
| ARC-U-004 | U | P0 | 系统应由各模块消费者定义所需的最小出站接口，禁止建立包含全部 K8s 能力的万能接口。 | 代码评审和接口方法清单。 |
| ARC-U-005 | U | P0 | 系统应仅由 `bootstrap` 完成适配器、用例和 HTTP Handler 的构造与装配。 | 代码评审确认无包级全局依赖注入。 |
| ARC-U-006 | U | P0 | 系统应禁止在 `init()` 中注册路由，禁止使用包级可变全局 Router。 | 静态搜索与代码评审。 |
| ARC-U-007 | U | P0 | 系统应由各模块的 HTTP adapter 提供 `Mount(chi.Router)`，根 Router 仅负责中间件和模块挂载。 | 路由表测试和代码评审。 |
| ARC-U-008 | U | P0 | 系统不应提供可任意访问 Kubernetes API 的透传接口。 | 安全评审确认不存在任意 proxy 路由。 |
| ARC-U-009 | U | P0 | 系统应在 adapter 边界把 Kubernetes 对象转换为模块内部模型，禁止原始 K8s 对象直接进入前端 DTO。 | 类型检查与代码评审。 |
| ARC-U-010 | U | P0 | 系统应以 `api/openapi.yaml` 作为唯一契约源，生成前端类型并通过 TypeScript 编译检查。 | `npm run api:generate && npm run typecheck` 通过。 |
| ARC-U-011 | U | P0 | 系统应提供可重复的路由清单测试：从 chi 路由树收集 `method + pattern`，与 OpenAPI 路径集合做双向差集，差集必须为空。 | `go test ./internal/bootstrap -run RouteInventory -count=1 -v`；差集为 0。 |
| ARC-U-012 | U | P0 | 系统应保证每个资源路由只有一个模块 owner，并由该模块的 `Mount(chi.Router)` 在隔离 Router 上注册；同一路径不得被多个模块重复注册。 | 路由归属测试和模块 `Mount` 单测；重复注册时测试失败。 |
| ARC-U-013 | U | P0 | 系统应通过 `domain.Actor` 和请求上下文传递已认证身份；生产代码中的业务模块不得导入 `auth/http` 或依赖另一模块的 Handler 获取 Actor。 | `make arch` 的跨模块 import 检查；`rg -n 'authhttp\\.ActorFrom|modules/auth/http' internal/modules internal/platform --glob '*.go'` 仅允许测试夹具命中。 |
| ARC-E-001 | E | P0 | 当新增、修改或删除路由时，系统应同步更新 OpenAPI、路由清单测试和至少一个 HTTP 契约测试；三者缺一不得合并。 | 变更评审检查 `api/openapi.yaml`、路由测试和 Handler 测试同时出现。 |
| ARC-N-001 | N | P0 | 如果模块间出现直接依赖另一模块 adapter 的代码，系统应拒绝合并。 | 架构 lint 失败或代码评审阻断。 |
| ARC-N-002 | N | P0 | 如果 HTTP Handler 中出现资源状态推导、扩缩容校验或 Kubernetes 调用，系统应拒绝合并。 | 代码评审清单。 |
| ARC-N-003 | N | P0 | 如果资源路由绕过模块 `Mount` 直接在根 Router 注册、使用通配路径代理 Kubernetes API，或由 Handler 自行创建 Router，系统应拒绝合并。 | 静态搜索、路由归属测试和安全评审共同阻断。 |

### 4.5 chi 路由设计约束

chi 只负责 HTTP 传输层，不允许成为业务耦合点。具体规则如下：

- 每个模块的 `http.Mount(chi.Router)` 只声明本模块的路径和处理函数，不创建新的全局 Router。
- `bootstrap/router.go` 只组装请求 ID、恢复、安全头、访问日志、压缩、认证/CSRF 和模块 `Mount`。
- URL 参数只在 HTTP adapter 中读取；业务校验、状态推导、权限和审计全部位于 `app`。
- 模块之间不互相导入 HTTP handler；跨模块能力通过 `domain` 中的窄接口或各自 app 端口表达。
- 路由名称必须使用显式资源路径，禁止 `/proxy/*`、`/{path:.*}` 或使用用户输入直接访问 Kubernetes API。
- 新增路由必须同步修改 `api/openapi.yaml`，并增加至少一个 HTTP 契约测试。

代码门禁：

```bash
make arch
# scripts/check-architecture.sh 实际检查：
# 1. domain 仅标准库；app 不导入 net/http、chi、k8s.io、platform 或其它模块
# 2. http adapter 不导入 k8s.io、platform/kube、bootstrap 或其它模块
# 3. platform 不反向导入 bootstrap 或模块 http
# 4. 仅 bootstrap/router.go 可创建 chi Router；禁止 init() 路由和 proxy/通配路由
# 5. 环境变量读取仅允许出现在 platform/config 的生产代码
```

该命令必须以 `architecture checks: pass` 结束。`internal/domain` 只允许标准库和本模块纯逻辑；
`internal/modules/*/app` 不允许 HTTP 或 Kubernetes 类型；测试夹具可以 import 认证 HTTP 包构造
上下文，但生产 Handler 必须使用 `domain.ActorFromContext`。

仅靠 import grep 不能证明路由没有变松，因此阶段 0 还必须补齐以下可执行验收：

| 控制点 | 设计规则 | 自动验收 | 失败处理 |
|---|---|---|---|
| 依赖方向 | `http -> app -> domain`；`platform/kube` 只实现 app/domain 端口 | `make arch` 无输出；如引入 `go-arch-lint`/`depguard`，由规则文件拒绝越层 import | CI 阻断，不允许以代码评审替代 |
| 路由归属 | 根 Router 只装配中间件、健康检查和模块 `Mount`；资源路径必须由所属模块注册 | 对每个模块使用独立 `chi.NewRouter()` 执行 `Mount`，断言只出现本模块路径前缀 | 测试失败即视为路由 owner 错误 |
| 契约覆盖 | OpenAPI 中的每个 `method + path` 必须有路由，每个路由也必须进入 OpenAPI；仅 `/healthz`、`/readyz` 和 SPA fallback 等非 API 进程级端点可进入显式白名单 | `TestRouteInventoryMatchesOpenAPI` 做双向差集；白名单必须集中声明并评审 | 任一差集非空即阻断合并 |
| 反例防线 | 禁止 `/proxy/*`、`/{path:.*}`、Handler 内创建 Router、`init()` 注册路由和包级可变 Router | 静态搜索 + 路由归属测试 + 安全评审 | 发现即拒绝合并，不做“先上线再补” |

路由清单测试应使用 `chi.Walk` 生成稳定的 `METHOD /path` 排序列表，避免依赖 map 遍历顺序；流式端点
（SSE、日志）也必须出现在清单中，不能用白名单把已实现路由排除在外。新增白名单只允许用于
非 OpenAPI 的进程级端点，且必须在测试代码中写明原因。

### 4.6 请求与依赖流

```text
HTTP request
  -> chi middleware (request id, recover, security headers, access log)
  -> auth HTTP adapter (session cookie, CSRF)
  -> module HTTP adapter (URL/JSON parsing only)
  -> module app service (validation, authorization, audit)
  -> consumer-owned port interface
  -> platform/kube adapter (K8s object conversion)
  -> JSON DTO / stream
```

模块 app 的端口由消费者定义，例如 `workloads/app.Repository` 只包含工作负载、Pod 和日志所需方法，不暴露通用 `dynamic.Interface` 或万能 Kubernetes client。

### 4.7 ADR-001：采用 chi，但不允许路由层承载业务

**决策**：接受 chi 作为 HTTP transport。chi 本身不会导致架构松散；真正导致松散的是
“把路由、参数校验、权限、资源状态推导和 Kubernetes 调用写在 Handler 里”，以及
“所有模块共用一个不断膨胀的 Router/Client 接口”。

**约束**：

1. `cmd/dosh` 只读取配置、创建 `bootstrap.App` 并处理进程退出。
2. `bootstrap` 是唯一 composition root；它负责构造 adapter、app service 和各模块 Handler。
3. 每个模块的 HTTP adapter 提供自己的 `Mount(chi.Router)`，只允许声明本模块路由。
4. Handler 只做 HTTP 解码、调用 app service、编码 DTO；不得 import client-go。
5. app/domain 不得 import `net/http`、chi 或 `k8s.io/*`；跨模块协作使用消费者定义的端口。
6. repository 接口按用例需要拆分，不建立“万能 Kubernetes repository”。
7. 每个新增写路由必须同时具备 CSRF、app 层授权、审计和 fake client 测试。

**自动化门禁**：

```bash
make arch
rg -n 'Mount\(|Route\(|r\.(Get|Post|Put|Delete)\(' internal/bootstrap internal/modules --glob '*.go'
rg -n 'k8s.io|net/http|go-chi' internal/domain internal/modules/*/app --glob '*.go'
```

第一条必须成功；第二、三条只用于审查路由归属，`domain/app` 的匹配必须为空。若需要
更严格的模块间依赖约束，再把 `make arch` 升级为 `go-arch-lint`/`depguard` 规则集。

### 4.8 多用户身份与 Kubernetes 授权设计（OIDC 模式）

系统支持两种互斥的认证模式，由 `DOSH_AUTH_MODE` 选择：

| 模式 | 身份来源 | Kubernetes 身份 | 适用场景 |
|---|---|---|---|
| `local` | `dosh-auth` 中的本地管理员 | 固定 ServiceAccount | 受控实验环境、单管理员 |
| `oidc` | 企业 IdP 的 OIDC ID Token | 每个请求 impersonate 当前用户和组 | 多用户、生产试点 |

`local` 模式不得被描述为生产多用户方案。生产启用多用户时，`oidc` 模式是目标形态。真实 RSA
签名的测试 IdP 已在隔离集群完成三身份 API/RBAC、三浏览器、Token 扫描和 OIDC 回滚矩阵，但企业
IdP 兼容性和生产发布尚未验收；在生产放行前，仍不得把 `local` 集群描述为多用户生产就绪。

**登录链路**

1. 浏览器请求 `/api/v1/auth/config`。系统只返回模式、IdP 显示名和登录入口，不返回 client secret。
2. 本地模式显示用户名/密码表单；OIDC 模式显示 SSO 按钮。
3. OIDC 登录入口生成随机 `state`、`nonce`、PKCE verifier，将三者与经过校验的 `returnTo` 一起
   使用 AES-GCM 封装到 `dosh_oidc_state` Cookie；Cookie 为 `HttpOnly`、`Secure`、
   `SameSite=Lax`、`Path=/api/v1/auth/oidc`。
4. IdP 回调后，系统从 Cookie 解码并验证过期时间，使用常量时间比较 `state`；再以
   Authorization Code + S256 verifier 兑换 Token。
5. OIDC adapter 校验签名、`iss`、`aud`、`exp`、`nonce`，映射 username 和 groups；当
   username claim 为 `email` 时，`email` 必须非空且 `email_verified` 必须显式为 `true`。
   不把未经验证的 claim 传给 Kubernetes。
6. app 层创建会话，TTL 不晚于 ID Token 的 `exp`；会话 Cookie 继续使用 `HttpOnly`、`Secure`、
   `SameSite=Strict`。ID/access/refresh Token 不进入浏览器 localStorage、URL、日志或审计。
7. 退出时清除会话，并在 IdP 发布 `end_session_endpoint` 时执行 RP-initiated logout。

**授权链路**

```text
HTTP request
  -> auth/http: 验证会话 -> domain.WithActor(ctx, actor)
  -> module app: 构造最小 domain.Access
  -> domain.Authorizer.Require(ctx, actor, access)
  -> platform/authz.Kubernetes
  -> SubjectAccessReview{spec.user, spec.groups, resourceAttributes}
  -> app 仅在 Allowed=true 时调用 Repository
  -> repository -> platform/kube（附带 Impersonate-User/Group）
```

关键约束：

- 授权检查必须发生在业务写操作和敏感读操作之前；`/capabilities` 只用于 UI 展示，不构成安全边界。
- OIDC 模式使用 `SubjectAccessReview`，由 ServiceAccount 显式提交 `spec.user` 和 `spec.groups`，
  以评估目标用户；创建 SAR 的请求使用 `domain.WithoutActor` 移除 impersonation 头。
  该实现已通过组件测试和隔离集群三身份矩阵，但浏览器路径与生产 IdP 仍需单独验收。
- OIDC 模式的 ServiceAccount 只允许 `impersonate` users/groups、`create` subjectaccessreviews，
  以及版本/发现所需的只读权限；不得保留业务资源的 cluster-admin 等价权限。
- impersonation transport 只从 `domain.Actor` 读取身份；禁止从 query、header 或请求体直接获取
  `Impersonate-User`，避免身份伪造。
- 当 SAR 返回错误、`EvaluationError` 非空或依赖不可达时，授权必须 fail-closed，返回 503/403 且不发送
  业务请求；不得回退到本地管理员权限。
- Secret 明文 `get` 应映射为敏感读取；Secret 元数据 `list` 仍可只读；所有拒绝和成功动作都必须审计。
- admin group 只用于产品内部展示/管理能力标记，最终写权限仍以 Kubernetes RBAC 为准；两者不得互相替代。

**会话与失败语义**

| 条件 | 行为 |
|---|---|
| state/nonce 不匹配、state 过期或 Cookie 缺失 | 清除 state Cookie，跳转 `/login?error=oidc`，审计 denied |
| discovery/JWKS/token 校验失败 | 返回 OIDC 登录失败，不创建会话；错误日志不含 token |
| token 缺少 username/subject 或 email 未验证且作为 username | 拒绝登录 |
| 会话过期 | `/api/v1/session` 返回 401；受保护 API 均拒绝 |
| SAR 拒绝 | 403，不调用 Repository |
| SAR 不可用 | 503，不降级为高权限 |
| IdP 未提供 logout endpoint | 只清除本地会话并返回 `/login` |

**clean architecture 端口**

```go
type Authorizer interface {
    Require(ctx context.Context, actor Actor, access Access) error
}

type OIDCProvider interface {
    AuthCodeURL(state, nonce, codeVerifier string) string
    Exchange(ctx context.Context, code, codeVerifier, expectedNonce string) (OIDCIdentity, error)
    EndSessionURL(postLogoutRedirectURL string) string
}
```

`Authorizer` 和 `OIDCProvider` 定义在消费者侧；`platform/authz`、`platform/oidc` 只实现端口。
`auth/app` 负责 state/nonce/PKCE 生命周期、会话策略和 capability 映射，`auth/http` 只负责 Cookie、
重定向和错误跳转。这样 chi 路由仍然只是 transport，不会承载身份或 RBAC 业务。

## 5. 运行时与配置需求

| ID | 类型 | 优先级 | 需求 | 验收方式 |
|---|---|---|---|---|
| SYS-U-001 | U | P0 | 系统应在默认配置下监听 `0.0.0.0:32626`。 | 容器内 `ss -lntp` 显示 32626。 |
| SYS-U-002 | U | P0 | 系统应将全部 Kubernetes 资源部署到 `dosh` 命名空间。 | `kubectl get all -n dosh` 能列出全部工作负载和 Service。 |
| SYS-U-003 | U | P0 | 系统应在同一端口提供 SPA 静态资源、`/api/v1`、`/healthz`、`/readyz` 和可选 `/metrics`。 | HTTPS 请求路径验证。 |
| SYS-U-004 | U | P0 | 系统应通过环境变量或配置文件读取运行参数，并在启动时完成严格校验。 | 非法配置启动失败测试。 |
| SYS-E-001 | E | P0 | 当容器启动时，系统应输出版本、Git SHA、监听地址、认证模式和 TLS 状态，但不得输出密码、Session Key 或 Secret 内容。 | 启动日志检查。 |
| SYS-N-001 | N | P0 | 如果配置缺失或非法，系统应以非零状态退出并输出不含敏感信息的错误。 | 配置测试。 |
| SYS-N-002 | N | P0 | 如果监听端口已被占用，系统应启动失败并使 Pod 进入非 Ready 状态。 | 端口冲突测试。 |
| SYS-O-001 | O | P1 | 当启用 TLS 时，系统应在 32626 端口提供 HTTPS，并对缺失或非法证书启动失败。 | `curl -k https://...` 验证；无证书时启动失败。 |
| SYS-O-002 | O | P2 | 当显式启用开发模式时，系统可允许 HTTP，但启动日志应输出高风险警告。 | 开发模式测试。 |
| SYS-N-003 | N | P0 | 如果 Kubernetes API 暂时不可达，系统应继续提供健康检查并让 readiness 状态反映依赖降级。 | 模拟 API 不可达测试。 |
| SYS-N-004 | N | P0 | 如果布尔、整数、浮点或 duration 类型的环境变量无法解析，系统应拒绝启动并指出变量名，不得静默回退默认值。 | `TestLoadRejectsMalformedEnvironmentValues`；逐项断言错误包含变量名。 |
| SYS-N-005 | N | P0 | 如果 `DOSH_COOKIE_SECURE=true` 但未启用 TLS，系统应拒绝启动，防止安全 Cookie 在 HTTP 环境被错误配置。 | 配置负向测试；错误必须包含 `DOSH_COOKIE_SECURE`。 |

### 5.1 默认配置

| 配置项 | 默认值 | 敏感 | 说明 |
|---|---|---|---|
| `DOSH_LISTEN_ADDR` | `:32626` | 否 | 监听地址 |
| `DOSH_AUTH_MODE` | `token` | 否 | `token`、`local` 或 `oidc`；默认通过 Kubernetes TokenReview 登录 |
| `DOSH_TLS_ENABLED` | `false` | 否 | 服务进程默认 HTTP；Helm 默认显式开启 |
| `DOSH_ADMIN_USERNAME` | 无 | 否 | 本地管理员用户名 |
| `DOSH_ADMIN_PASSWORD_HASH` | 无 | 是 | Argon2id 哈希，不接受明文密码 |
| `DOSH_SESSION_KEY` | 无 | 是 | 至少 32 字节的随机密钥 |
| `DOSH_SESSION_TTL` | `8h` | 否 | 会话有效期 |
| `DOSH_TLS_CERT_FILE` | `/etc/dosh/tls/tls.crt` | 否 | TLS 证书 |
| `DOSH_TLS_KEY_FILE` | `/etc/dosh/tls/tls.key` | 是 | TLS 私钥 |
| `DOSH_COOKIE_SECURE` | 跟随 TLS | 否 | HTTPS 必须为 `true`；HTTP 部署必须显式设为 `false` |
| `DOSH_LOGIN_RATE_LIMIT_MODE` | `memory` | 否 | `local`/`token` 模式可设为 `kubernetes`，使用 Lease 跨副本限速 |
| `DOSH_LOGIN_RATE_LIMIT_NAMESPACE` | 空 | 否 | `kubernetes` 限速模式必填，Helm 默认注入 Release 命名空间 |
| `DOSH_READ_ONLY` | `false` | 否 | 只读模式开关 |
| `DOSH_KUBE_QPS` | `50` | 否 | Kubernetes 客户端 QPS |
| `DOSH_KUBE_BURST` | `100` | 否 | Kubernetes 客户端 Burst |
| `DOSH_REQUEST_TIMEOUT` | `15s` | 否 | 单次 API 超时 |
| `DOSH_MAX_REQUEST_BODY` | `2097152` | 否 | JSON 请求体大小上限（字节） |
| `DOSH_CLUSTER_NAME` | `kubernetes` | 否 | 页面展示的集群名称 |
| `DOSH_LOG_LEVEL` | `info` | 否 | 日志级别 |
| `DOSH_INSECURE_SKIP_VERIFY` | `false` | 否 | 仅用于受控实验环境；生产必须为 `false` |
| `DOSH_AUDIT_WEBHOOK_URL` | 空 | 否 | 可选审计投递地址；为空时只写 stdout |
| `DOSH_AUDIT_WEBHOOK_TOKEN` | 空 | 是 | Webhook Bearer Token；与 HMAC Key 至少配置一个 |
| `DOSH_AUDIT_WEBHOOK_HMAC_KEY` | 空 | 是 | HMAC-SHA256 Key；与 Token 至少配置一个 |
| `DOSH_AUDIT_WEBHOOK_CA_FILE` | 空 | 否 | 企业私有/内部 CA 的 PEM 文件路径；配置后严格使用该信任池校验 Webhook TLS |
| `DOSH_AUDIT_WEBHOOK_TIMEOUT` | `3s` | 否 | 单次投递超时，允许范围 `(0, 30s]` |
| `DOSH_AUDIT_WEBHOOK_QUEUE_SIZE` | `1024` | 否 | 有界队列容量，允许范围 `1–100000` |
| `DOSH_OIDC_ISSUER` | 空 | 否 | OIDC issuer；`oidc` 模式必填，必须是 HTTPS 绝对 URL |
| `DOSH_OIDC_CLIENT_ID` | 空 | 否 | OIDC client ID；`oidc` 模式必填 |
| `DOSH_OIDC_CLIENT_SECRET` | 空 | 是 | OIDC client secret；必须来自 Secret，不写入 values/日志 |
| `DOSH_OIDC_REDIRECT_URL` | 空 | 否 | 回调绝对 URL；必须与 IdP 注册值完全一致 |
| `DOSH_OIDC_SCOPES` | `openid,profile,email,groups` | 否 | 逗号分隔，必须包含 `openid` |
| `DOSH_OIDC_USERNAME_CLAIM` | `email` | 否 | 支持 `sub`、`preferred_username`、`email`、`name`；email 未验证时拒绝 |
| `DOSH_OIDC_GROUPS_CLAIM` | `groups` | 否 | 当前支持 `groups` 数组 |
| `DOSH_OIDC_ADMIN_GROUPS` | 空 | 否 | admin 展示能力组列表；不替代 Kubernetes RBAC |
| `DOSH_OIDC_CA_FILE` | 空 | 否 | 私有 IdP 的自定义 CA 文件路径；证书 mount 后只读 |
| `DOSH_OIDC_TIMEOUT` | `10s` | 否 | discovery、JWKS、token exchange 的 HTTP 超时 |
| `DOSH_TOKEN_REVIEW_AUDIENCE` | 空 | 否 | 可选 TokenReview audience；projected ServiceAccount Token 可显式校验 |

通用配置负向规则：布尔、整数、浮点、duration 解析失败必须使启动失败；`DOSH_CLUSTER_NAME`
不得为空；`DOSH_LOG_LEVEL` 仅允许 `debug/info/warn/error`；`DOSH_REQUEST_TIMEOUT` 必须在
`(0, 5m]`；`DOSH_COOKIE_SECURE=true` 必须同时启用 TLS；`DOSH_COOKIE_SECURE=false` 仅允许在
显式关闭 TLS 时使用；`DOSH_LOGIN_RATE_LIMIT_MODE=kubernetes` 仅允许 `local`/`token` 模式，且
namespace 必须非空。

审计配置负向规则：URL 必须是绝对 `https://` 地址，明文 `http://` 必须拒绝；启用 Webhook 时
Token 与 HMAC Key 至少一个非空；超时和队列大小必须在允许范围内；配置 CA 文件但无法读取、
无法解析或不含有效证书时拒绝启动；队列满时必须记录 `audit_webhook_queue_full`，不得阻塞业务请求。
生产环境应同时启用 Token 与 HMAC，并强制使用 HTTPS；如使用企业私有 CA，必须通过 Secret/ConfigMap
挂载只读 PEM 文件，不能关闭 TLS 校验或使用全局 `DOSH_INSECURE_SKIP_VERIFY` 绕过。

OIDC 配置负向规则：issuer 必须是绝对 HTTPS URL；redirect URL 必须是绝对 URL；scopes 必须包含
`openid`；username/groups claim 必须是受支持值；`oidc` 模式缺少 client ID、redirect URL 或
session key 时拒绝启动；CA 文件存在但无法解析时拒绝启动。OIDC 和其自定义 CA 均为可选能力，
未启用 OIDC 时不得要求 issuer、client 或 CA。错误信息不得包含 client secret。

## 6. 认证与授权需求

### 6.1 Kubernetes Token 认证（默认）

| ID | 类型 | 优先级 | 需求 | 验收方式 |
|---|---|---|---|---|
| AUTH-T-001 | U | P0 | 系统应默认提供 Kubernetes Token 登录：提交非空 Bearer Token，通过 Kubernetes TokenReview 校验后创建会话；OIDC 和本地密码不得成为 Token 模式的启动前提。 | `token_handler_test.go`、真实集群 TokenReview、HTTP/HTTPS 隔离验收。 |
| AUTH-T-002 | U | P0 | 当 TokenReview 成功时，系统应只采用 Kubernetes 返回的 username、groups 和 `system:masters` 标记建立身份，不得信任请求体中的用户名或组。 | fake TokenReview 响应断言、真实 viewer/operator/admin RBAC 矩阵。 |
| AUTH-T-003 | N | P0 | 系统不应把原始 Token 写入 Cookie、浏览器存储、URL、审计或日志；Token 长度超过 16 KiB 时应拒绝。 | 响应/Cookie/日志扫描和超长输入测试。 |
| AUTH-T-004 | U | P0 | 当 `DOSH_LOGIN_RATE_LIMIT_MODE=kubernetes` 时，Token 和本地登录应使用名称空间 Lease 共享失败计数；限速器不可用时 fail-closed。 | 真实 Lease 创建、连续失败测试和配置负向测试。 |
| AUTH-T-005 | U | P1 | 当 Token 属于 ServiceAccount 时，业务请求应能通过 impersonation 和 SubjectAccessReview 执行；Chart 应授予最小 `serviceaccounts/impersonate`。 | 真实 ServiceAccount Token、Helm 渲染和 API 矩阵。 |
| AUTH-T-006 | N | P0 | Token 会话应受 `DOSH_SESSION_TTL` 上限约束；吊销 Token 不会即时撤销已签发 Cookie，运维必须能轮换 Session Key 使会话立即失效。 | 会话过期测试、Session Key 轮换演练和 Runbook 对账。 |
| AUTH-T-007 | U | P0 | 系统应支持 HTTP 与 HTTPS 两种运行方式；HTTPS 证书可选自签，OIDC/审计自定义 CA 不是通用启动前提；HTTP 必须显式关闭 Secure Cookie。 | `scripts/check-helm.sh` 的 HTTP/HTTPS 渲染、TLS/HTTP 实机 curl、配置负向测试。 |

### 6.2 本地与 OIDC 认证

| ID | 类型 | 优先级 | 需求 | 验收方式 |
|---|---|---|---|---|
| AUTH-U-001 | U | P0 | 系统应支持本地管理员账号登录，密码只能以 Argon2id 哈希形式存储。 | 单元测试和 Secret 检查。 |
| AUTH-U-002 | U | P0 | 系统应使用 `HttpOnly`、`Secure`、`SameSite=Strict` 的会话 Cookie。 | HTTP 响应头测试。 |
| AUTH-E-001 | E | P0 | 当登录成功后，系统应创建有效期不超过 8 小时的加密会话 Cookie。 | 登录测试和 Cookie 解码测试。 |
| AUTH-E-002 | E | P0 | 当登录失败时，系统应返回统一错误，不区分用户名不存在和密码错误。 | 接口测试。 |
| AUTH-E-003 | E | P0 | 当用户退出时，系统应使客户端会话 Cookie 失效。 | 退出流程测试。 |
| AUTH-N-001 | N | P0 | 如果连续登录失败超过阈值，系统应实施按 IP 的短时登录限速。 | 限速测试。 |
| AUTH-N-002 | N | P0 | 如果会话过期、签名无效或 CSRF 校验失败，系统应返回 401 或 403，不执行资源操作。 | 安全测试。 |
| AUTH-N-003 | N | P0 | 如果未配置管理员密码哈希或 Session Key，系统应拒绝启动。 | 配置测试。 |
| AUTH-O-001 | O | P1 | 当启用 OIDC 时，系统应使用 Authorization Code + S256 PKCE，并校验 state、nonce、签名、issuer、audience 和过期时间。 | 真实签名 JWT 的 provider 测试、HTTP 回调测试和真实 IdP E2E。 |
| AUTH-O-002 | O | P1 | 当启用 OIDC 时，系统应将经过校验的用户名和组传递给 Kubernetes impersonation 与 SubjectAccessReview，不得信任客户端传入身份。 | impersonation header 测试、SAR 对象断言和审计日志。 |
| AUTH-O-003 | O | P1 | 当启用 OIDC 时，系统应将会话有效期限制在 ID Token 过期时间以内，且不得把任何 Token 写入浏览器可读存储、URL 或日志。 | 会话测试、响应头与日志扫描。 |
| AUTH-O-004 | O | P1 | 当用户从 OIDC 模式退出时，系统应清除本地会话，并在 IdP 支持时执行 RP-initiated logout。 | 真实 IdP logout E2E 和 Cookie 检查。 |
| AUTH-O-005 | O | P1 | 当 OIDC 回调携带非法或过期的 state、nonce 不匹配或开放重定向参数时，系统应拒绝登录并审计 denied。 | 负向集成测试。 |
| AUTH-O-006 | O | P1 | 当 `DOSH_OIDC_USERNAME_CLAIM=email` 时，系统应要求 `email` 非空且 `email_verified` 显式存在并为 `true`；字段缺失、类型错误或值为 `false` 时拒绝创建会话。 | `TestProviderRejectsInvalidTokens` 的缺失、`false` 和缺失 email claim 用例；真实 IdP claim 对账。 |
| AUTH-N-004 | N | P0 | 如果 OIDC discovery、JWKS、token exchange 或签名校验失败，系统应拒绝创建会话，不得回退到本地管理员或匿名访问。 | 故障注入和负向测试。 |

### 6.3 授权

| ID | 类型 | 优先级 | 需求 | 验收方式 |
|---|---|---|---|---|
| AZ-U-001 | U | P0 | 系统应在每个受保护 API 请求上验证会话和 CSRF。 | 接口安全测试。 |
| AZ-U-002 | U | P0 | 系统应在每个写操作用例中执行资源级权限检查，不能只依赖前端按钮隐藏。 | 用例层测试。 |
| AZ-U-003 | U | P0 | 系统应提供 `/api/v1/capabilities` 返回当前用户的只读、扩容、重启、删除、创建等能力。 | 接口测试。 |
| AZ-N-001 | N | P0 | 如果用户无权执行操作，系统应返回 403 且不向 Kubernetes 发送写请求。 | Fake client 调用次数断言。 |
| AZ-N-002 | N | P0 | 如果处于只读模式，系统应拒绝全部写操作。 | 只读模式测试。 |
| AZ-O-001 | O | P1 | 当使用 OIDC 模式时，系统应通过 Kubernetes 原生 RBAC 决定用户权限，并使用携带 `spec.user`/`spec.groups` 的 SubjectAccessReview，而不是依赖可被 impersonation 影响的 SSAR。 | fake clientset SAR 对象断言和真实集群三身份矩阵。 |
| AZ-O-002 | O | P1 | 当使用 OIDC 模式时，系统应从同一 Authorizer 计算 `/capabilities`，并保证 UI 隐藏与 API 拒绝使用同一权限事实。 | capabilities 测试、直接调用无权 API 的 403 测试。 |
| AZ-T-001 | U | P0 | 当使用 Token 模式时，系统应通过 SubjectAccessReview 对 Kubernetes 返回的身份做资源级授权，并仅在该审查通过后以同一身份 impersonate 业务请求。 | SAR 对象断言、真实三身份矩阵和无权写请求不落库测试。 |
| AZ-T-002 | N | P0 | 如果 TokenReview、SubjectAccessReview 或 impersonation 依赖不可用，系统应 fail-closed，不得回退到 dosh ServiceAccount 的业务权限或匿名访问。 | fake client 错误注入、真实 RBAC 缺失负向测试。 |
| AZ-T-003 | U | P0 | 当 Token/OIDC 用户访问审计历史时，系统应通过 `dosh.io/audit-events` 的 `get` SubjectAccessReview 判权，不得只依赖 TokenReview 的 `system:masters` 或前端管理员标记。 | 真实 cluster-admin ServiceAccount 200、viewer 403 和非管理员绑定测试。 |
| AZ-N-003 | N | P0 | 如果授权审查返回错误、EvaluationError 或依赖不可达，系统应 fail-closed，不调用业务 Repository，不得回退到 ServiceAccount 权限。 | fake client 错误注入和调用次数断言。 |

## 7. 领域功能需求

### 7.1 总览

| ID | 类型 | 优先级 | 需求 | 验收方式 |
|---|---|---|---|---|
| OVW-U-001 | U | P0 | 系统应展示集群名称、Kubernetes 版本、CNI 信息和整体运行状态。 | 与 `kubectl get nodes`、`version` 对比。 |
| OVW-U-002 | U | P0 | 系统应展示 Ready 节点数、Pod 数、工作负载数和异常资源数量。 | 与 K8s 实时数据对比。 |
| OVW-U-003 | U | P0 | 系统应展示 CPU 和内存的容量、requests、limits 汇总。 | 聚合结果与脚本计算一致。 |
| OVW-U-004 | U | P0 | 当 Metrics API 可用时，系统应展示集群 CPU/内存实际使用量和利用率，并保留 requests 作为调度容量参考。 | fake Metrics API 测试和真实 Metrics Server 对账。 |
| OVW-E-001 | E | P0 | 当打开总览页时，系统应并行加载各数据源，并在单个数据源失败时返回可用的部分数据。 | 故障注入测试。 |
| OVW-N-001 | N | P0 | 如果 Metrics API 不可用，系统应显示“未安装 Metrics Server”并隐藏实际利用率，不得显示伪造的 0%。 | 当前集群验证。 |
| OVW-N-003 | N | P0 | 如果 Metrics API 调用失败或某个资源没有指标样本，系统应回退到 requests/容量展示，并标记该资源无实际用量。 | fake client 错误注入和页面状态测试。 |
| OVW-N-002 | N | P0 | 如果某类资源列表为空，系统应显示对应空状态而不是空白页面。 | UI 测试。 |

### 7.2 节点

| ID | 类型 | 优先级 | 需求 | 验收方式 |
|---|---|---|---|---|
| NODE-U-001 | U | P0 | 系统应列出节点名称、角色、状态、Internal IP、版本、OS、容器运行时、年龄和资源容量。 | 与 `kubectl get nodes -o wide` 对比。 |
| NODE-U-002 | U | P0 | 系统应区分 Control Plane 和 Worker，并展示 Ready、NotReady、SchedulingDisabled 状态。 | UI 和接口测试。 |
| NODE-U-003 | U | P0 | 系统应展示 CPU、内存、Pod 的容量与已分配量。 | 聚合测试。 |
| NODE-U-004 | U | P0 | 系统应提供节点详情，包括 Conditions、Taints、Labels、Annotations 和该节点上的 Pod。 | 与 K8s 对象对比。 |
| NODE-U-005 | U | P0 | 当 Metrics API 可用时，系统应展示节点实际 CPU/内存使用量；不可用时显示“Metrics 不可用”，并继续展示 requests。 | fake Metrics API 测试和真实集群对账。 |
| NODE-O-001 | O | P2 | 当启用节点运维能力时，系统应提供 Cordon/Uncordon，并在执行前要求确认和审计。 | 受控测试集群验收。 |

**NODE-O-001 当前状态（2026-11-04）**：已实现于 `internal/modules/nodes/app/service.go`、
`internal/modules/nodes/http/handler.go`、`internal/platform/kube/nodes.go`、
`web/src/features/nodes/node-detail-page.tsx` 和 Helm 本地 RBAC。权限使用 Kubernetes
`update nodes` 语义，失败不调用 Repository；成功、拒绝和依赖失败均写审计。新候选在隔离 Release
revision 139 完成 viewer/operator 403、admin 200、`kubectl` 状态对账、Uncordon 恢复和带
requestId 的审计验收，证据见 21.2.27。生产环境节点未执行测试。

### 7.3 工作负载与 Pod

| ID | 类型 | 优先级 | 需求 | 验收方式 |
|---|---|---|---|---|
| WL-U-001 | U | P0 | 系统应支持 Deployment、StatefulSet、DaemonSet、Job、CronJob 的列表和详情。 | 与 `kubectl get` 对比。 |
| WL-U-002 | U | P0 | 系统应展示命名空间、状态、Ready 数、重启次数、镜像、创建时间和关键 Conditions。 | 接口 fixture 测试。 |
| WL-U-003 | U | P0 | 系统应支持按命名空间、状态、名称进行筛选，并保持筛选条件在 URL 中可复现。 | E2E 导航测试。 |
| WL-E-001 | E | P0 | 当用户发起扩容时，系统应校验目标副本数、资源版本和权限后再调用 scale 子资源。 | 用例测试。 |
| WL-E-002 | E | P0 | 当用户发起重启时，系统应通过更新 Pod template 的 `restartedAt` annotation 完成滚动重启。 | 集成测试。 |
| WL-E-003 | E | P0 | 当用户删除工作负载时，系统应要求输入资源名确认，并记录审计日志。 | UI 和接口测试。 |
| WL-E-004 | E | P0 | 当用户通过创建向导提交 Deployment 时，系统应在 app 层完成名称、命名空间、容器名、镜像、副本数、端口、资源、环境变量和标签校验，先执行 Kubernetes API Server Dry Run，成功后再正式创建。 | app/http/kube 测试与真实集群创建对账。 |
| WL-E-005 | E | P1 | 当用户请求 YAML 预览时，系统应返回与正式创建路径同一构造函数生成、但不写入集群的 Deployment 清单。 | preview 与 create 对象形状一致性测试。 |
| WL-E-006 | E | P1 | 当用户暂停或恢复 CronJob 时，系统应校验 CronJob kind、资源版本和 `batch/cronjobs update` 权限，再更新 `spec.suspend`，且不得删除已有 Job/Pod。 | app/http/kube 测试、fake client 冲突/幂等测试与真实集群对账。 |
| WL-N-001 | N | P0 | 如果资源版本冲突，系统应返回 409 并提示刷新后重试。 | 并发测试。 |
| WL-N-002 | N | P0 | 如果扩容值超出允许范围，系统应返回 400，不发送写请求。 | 参数校验测试。 |
| WL-N-003 | N | P0 | 如果 Deployment 已存在，系统应返回 409 和稳定错误码 `already_exists`，不得误报为 resourceVersion 冲突。 | kube 错误映射、HTTP handler 和真实重复创建测试。 |
| WL-N-004 | N | P0 | 如果 Deployment 名称、镜像、资源或标签非法，系统应在 Dry Run 前拒绝，且 Repository 写调用次数为 0。 | app 负向表驱动测试与 fake Repository 调用计数。 |
| WL-N-005 | N | P0 | 如果扩容、重启或删除请求包含无效 kind、不支持的操作或非法删除策略，系统应返回稳定的 400/501 语义，记录失败审计，并且 Repository 写调用次数为 0。 | workloads app 表驱动负向测试、fake Repository 计数和 fake Auditor 断言。 |
| WL-N-006 | N | P0 | 如果工作负载（尤其 CronJob）没有 selector，系统仍应返回安全的工作负载详情，selector 为空、Pod 列表为空且不得 panic。 | Kubernetes fake client 五种工作负载详情回归；CronJob 无 selector 用例。 |
| WL-N-007 | N | P1 | 如果暂停/恢复请求用于非 CronJob、资源版本已陈旧且目标状态未生效，或名称参数非法，系统应分别返回 501、409 或 400，记录失败审计，并且 Repository 写调用次数为 0。 | app 表驱动负向测试、fake Repository 计数、fake client 冲突测试。 |
| POD-U-001 | U | P0 | 系统应列出 Pod 名称、状态、Ready、重启次数、IP、节点、Owner 和年龄。 | 与 K8s 对比。 |
| POD-U-002 | U | P0 | 系统应展示 Pod 容器、镜像、状态、资源 requests/limits 和 Conditions。 | 详情测试。 |
| POD-U-003 | U | P0 | 当 Metrics API 可用时，系统应展示 Pod 实际 CPU/内存使用量；不可用时不得把缺失指标显示成 0。 | fake Metrics API 测试和真实集群对账。 |
| POD-E-001 | E | P0 | 当用户选择容器查看日志时，系统应按容器流式返回日志。 | 流式接口测试。 |

### 7.4 日志与事件

| ID | 类型 | 优先级 | 需求 | 验收方式 |
|---|---|---|---|---|
| LOG-U-001 | U | P0 | 系统应支持选择 Pod、容器、tailLines、timestamps 和 follow 参数。 | 接口与 UI 测试。 |
| LOG-U-002 | U | P0 | 系统应限制单次非流式日志的最大返回量，避免浏览器内存失控。 | 边界测试。 |
| LOG-E-001 | E | P0 | 当日志流建立时，系统应在 2 秒内返回首批可用日志或明确状态。 | 集群集成测试。 |
| LOG-E-002 | E | P0 | 当日志流断开时，前端应显示断开状态，并允许用户手动重连或自动有限重连。 | UI 测试。 |
| LOG-N-001 | N | P0 | 如果容器不存在、Pod 已删除或日志不可读，系统应返回结构化错误并终止流。 | 接口测试。 |
| LOG-N-002 | N | P0 | 如果日志内容包含 HTML 或终端控制字符，系统不得将其作为 HTML 执行。 | 安全测试。 |

**LOG-E-002 / LOG-N-001 当前状态（2026-11-04）**：日志接口返回
`application/x-ndjson; charset=utf-8`。`log` 帧承载 UTF-8 日志块，正常 EOF 以 `end` 终止；响应头
发送后的读取失败以 `error` 帧终止，帧内使用与普通 API 相同的稳定 `code`/安全 `message`，并携带
`requestId`。认证、Pod/容器不存在等响应头前错误继续返回标准 JSON Problem。服务端保持 UTF-8 跨读边界，
前端使用 `web/src/features/workloads/log-stream.ts` 增量解析，`log-viewer.tsx` 显示终止错误、请求 ID
并保留“重新加载”手动重连；日志仍只进入 `<pre>` 文本节点和有界内存，不写入浏览器持久存储。

真实隔离集群通过 `make e2e-log-stream` 验证正常 `log`→`end`、缺失容器 4xx Problem、未认证 401、
删除 Pod 后 follow 以 `end`/`error` 终止，以及 HTML/ANSI 字节不被协议转换丢失。证据见 21.2.37。

| EVT-U-001 | U | P0 | 系统应列出事件类型、Reason、对象、消息、次数、首次和最近发生时间。 | 与 K8s 对比。 |
| EVT-E-001 | E | P0 | 当用户打开事件流时，系统应通过 SSE 推送新事件并支持按命名空间筛选。 | SSE 测试。 |
| EVT-N-001 | N | P1 | 如果事件流中断，客户端应使用退避策略重连，并且不得生成重复事件行。 | 重连测试。 |

### 7.5 服务、路由、配置与存储

| ID | 类型 | 优先级 | 需求 | 验收方式 |
|---|---|---|---|---|
| NET-U-001 | U | P0 | 系统应列出 Service 的名称、命名空间、类型、Cluster IP、External IP、端口和 Endpoint 数量。 | 与 K8s 对比。 |
| NET-U-002 | U | P0 | 系统应列出 Ingress 的 Class、Host、Path、后端和目标状态。 | 与 K8s 对比。 |
| NET-N-001 | N | P0 | 如果集群没有 IngressClass，系统应显示“未安装 Ingress Controller”的明确空状态。 | 当前集群验收。 |
| CFG-U-001 | U | P0 | 系统应列出 ConfigMap 的键数量、数据大小和更新时间，并支持查看非敏感内容。 | 与 K8s 对比。 |
| SCR-U-001 | U | P0 | 系统应在 Secret 列表中仅返回名称、类型、键名、大小和更新时间，不返回值。 | 接口响应测试。 |
| SCR-U-002 | U | P0 | 系统应默认隐藏 Secret 详情中的值，使用显式“查看明文”动作。 | UI 与接口测试。 |
| SCR-E-001 | E | P0 | 当用户查看 Secret 明文时，系统应要求二次确认、执行权限检查并写入审计日志。 | 安全测试。 |
| STO-U-001 | U | P0 | 系统应列出 StorageClass、PV、PVC；无资源时显示空状态。 | 当前集群验收。 |
| STO-U-002 | U | P0 | 当用户打开 StorageClass 详情时，系统应展示 provisioner、回收策略、卷绑定模式、默认标记、扩容、参数、挂载选项、标签、注解和创建时间。 | 隔离 API 与浏览器详情验收。 |
| STO-U-003 | U | P0 | 当用户打开 PV 详情时，系统应展示容量、访问模式、回收策略、状态、StorageClass、卷模式、CSI Driver、mount options 和绑定的 claim。 | 隔离 API 与浏览器详情验收。 |
| STO-U-004 | U | P0 | 当用户打开 PVC 详情时，系统应展示申请/实际容量、访问模式、状态、绑定 PV、StorageClass、卷模式、conditions、标签、注解和创建时间。 | 隔离 API 与浏览器详情验收。 |
| STO-N-001 | N | P0 | 如果没有 StorageClass，系统应提示当前集群不支持动态持久化卷申请。 | 当前集群验收。 |
| STO-N-002 | N | P0 | 如果存储详情请求失败或对象已被删除，系统应显示明确错误状态并支持返回列表，不得白屏。 | handler 测试与浏览器错误态验收。 |
| NS-U-001 | U | P0 | 当用户从命名空间列表进入详情时，系统应展示名称、状态、标签、注解、创建时间和资源治理摘要，并提供可刷新/可分享的深链。 | handler、OpenAPI、浏览器深链与刷新验收。 |
| NS-U-002 | U | P0 | 当命名空间配置 ResourceQuota 时，系统应展示每个配额的资源名、已使用量和硬限制；无配额时显示明确空状态。 | fake client 单测与隔离 API/浏览器验收。 |
| NS-U-003 | U | P0 | 当命名空间配置 LimitRange 时，系统应按限制项类型展示 min、max、default 和 defaultRequest；元数据、事件和 YAML 可查看。 | fake client 单测与隔离 API/浏览器验收。 |
| NS-N-001 | N | P0 | 如果命名空间详情或关联配额/限制读取失败，系统应返回结构化错误且不得白屏；删除后的对象返回 404。 | handler 测试与隔离 API 删除后 404。 |

### 7.6 YAML 查看与编辑

| ID | 类型 | 优先级 | 需求 | 验收方式 |
|---|---|---|---|---|
| YML-U-001 | U | P0 | 系统应提供只读 YAML 查看，并移除 `managedFields` 等无展示价值字段。 | 快照测试。 |
| YML-U-002 | U | P0 | 系统应对 Secret YAML 执行默认脱敏。 | 安全测试。 |
| YML-O-001 | O | P1 | 当启用 YAML 编辑时，系统应先在服务端执行 Dry Run，成功后再提交。 | 集成测试。 |
| YML-O-002 | O | P1 | 系统应只接受单个 YAML 文档；空文档、多文档和重复键必须在授权与应用前拒绝。 | app 负向测试和真实 API 回归。 |
| YML-N-001 | N | P0 | 如果 YAML 包含不允许的资源类型、命名空间越权或解析失败，系统应拒绝提交。 | 负向测试。 |
| YML-N-002 | N | P0 | 如果 Dry Run 失败，系统应返回可定位的字段错误且不修改任何资源。 | 集成测试。 |

## 8. 前端与视觉需求

| ID | 类型 | 优先级 | 需求 | 验收方式 |
|---|---|---|---|---|
| UX-U-001 | U | P0 | 系统应采用顶部全局栏 + 左侧导航 + 主内容区的 CCE 风格布局。 | 视觉验收。 |
| UX-U-002 | U | P0 | 顶部栏应包含产品标识、当前集群、全局资源搜索、事件入口、帮助和用户菜单。 | UI 测试。 |
| UX-U-003 | U | P0 | 左侧导航应包含总览、节点管理、工作负载、服务与路由、配置与密钥、存储、命名空间和事件。 | 导航快照测试。 |
| UX-U-004 | U | P0 | 列表页应统一提供面包屑、标题、筛选区、表格、分页和空/错/加载状态。 | 组件测试和视觉评审。 |
| UX-U-008 | U | P0 | 系统应将列表页的 `q`、`namespace`、`kind` 写入 URL search 参数，并在刷新、分享链接和浏览器前进/后退后恢复筛选条件。 | 路由单元测试和 Playwright URL 持久化 E2E。 |
| UX-U-009 | U | P0 | 当 Metrics API 可用时，总览、节点、Pod 列表和 Pod 详情应展示实际 CPU/内存用量；不可用时应显示明确降级文案。 | 前端组件测试和三浏览器 E2E。 |
| UX-U-010 | U | P0 | 系统应从 `/api/v1/overview` 读取并展示 `cluster.name`、`version`、`cni` 和 `status`；顶部栏、侧栏与关于弹窗不得硬编码集群名、版本、CNI 或连接状态。 | MSW 使用非实验室集群数据断言渲染；请求失败时断言显示“状态不可用”而非绿色正常。 |
| UX-U-005 | U | P0 | 详情页应使用“概览、实例、事件、日志、YAML”标签结构。 | Vitest 深链测试与 Chromium/Firefox Playwright。当前候选已覆盖节点、工作负载和 Pod 详情。 |
| UX-U-006 | U | P0 | 系统应使用统一设计 token：主色 `#0B63CE`、背景 `#F5F7FA`、正文 `#1D2129`、辅助文字 `#6B7785`、成功 `#1F9D55`、警告 `#D97706`、危险 `#D92D20`。 | token 检查和视觉验收。 |
| UX-U-007 | U | P0 | 系统应采用 8 px 卡片圆角、44 px 默认表格行高、1 px 边框和克制阴影。 | 视觉验收。 |
| UX-E-001 | E | P0 | 当窗口宽度小于 1024 px 时，侧边栏应折叠为图标或抽屉，表格应支持横向滚动。 | 响应式测试。 |
| UX-E-002 | E | P0 | 当后端返回 401 时，前端应跳转登录页；返回 403 时应显示无权限状态。 | E2E 测试。 |
| UX-E-003 | E | P0 | 当请求加载时间超过阈值时，系统应显示骨架屏而不是阻塞式空白。 | UI 测试。 |
| UX-N-001 | N | P0 | 如果用户无写权限，系统不得展示可执行按钮，但后端仍必须执行权限校验。 | 权限双层测试。 |
| UX-N-002 | N | P0 | 系统不得使用 `dangerouslySetInnerHTML` 渲染日志、事件、标签或 YAML。 | 静态扫描。 |
| UX-N-003 | N | P0 | 系统不得直接复制 Headlamp、Kubernetes Dashboard 或其他社区面板的页面代码和视觉资产。 | 代码与资产审查。 |
| UX-O-001 | O | P1 | 当用户开启暗色主题时，系统应使用同一语义 token 并保持状态色可辨识。 | 主题快照测试。 |
| UX-O-002 | O | P1 | 当 `authMode=oidc` 时，登录页应显示企业 SSO 登录，用户菜单应显示 IdP 身份，退出应执行 RP-initiated logout。 | 组件测试和真实 IdP E2E。 |
| UX-E-004 | E | P1 | 当 OIDC 回调失败时，登录页应显示可理解的身份认证失败提示，同时清除旧状态且不泄露 IdP 错误细节。 | OIDC 回调失败 E2E。 |
| UX-N-004 | N | P0 | 状态不得只依赖颜色表达，必须同时提供文本或图标。 | 可访问性检查。 |

### 8.1 CCE 风格信息架构

目标不是像素级复制华为云 CCE，而是复用它“集群上下文优先、左侧资源导航、列表密集、详情分签页”
的运维信息架构。产品标识、配色、图标和插画必须自建，不复制社区面板的页面代码或资产。

```text
┌─────────────────────────────────────────────────────────────────────────────┐
│ dosh  |  集群 kubedo-lab  |  全局搜索  |  事件  |  帮助  |  用户/身份        │
├──────────────┬──────────────────────────────────────────────────────────────┤
│ 总览          │ 面包屑 / 页面标题                        主操作按钮          │
│ 节点管理      │ 筛选：名称 / 命名空间 / 状态 / 类型       [刷新] [创建]     │
│ 工作负载      │ ┌──────────────────────────────────────────────────────────┐ │
│ 服务与路由    │ │ 名称 | 命名空间 | 状态 | Ready | CPU/Mem | 年龄 | 操作  │ │
│ 配置与密钥    │ │ ...                                                        │ │
│ 存储          │ └──────────────────────────────────────────────────────────┘ │
│ 命名空间      │ 分页 / 结果总数 / 当前筛选条件                                 │
│ 事件          │                                                              │
└──────────────┴──────────────────────────────────────────────────────────────┘
```

| 区域 | 设计约束 | 交互与验收 |
|---|---|---|
| 顶部全局栏 | 高度 56 px；产品标识、当前集群、全局搜索、事件入口、帮助、用户菜单；集群和身份始终可见 | 390 px 时隐藏次要文字但保留菜单；切换集群/身份不丢失当前 route 语义 |
| 左侧导航 | 默认 208 px；按“可观测 → 资源 → 配置 → 平台”分组；当前项有背景和左侧强调线，不能只用颜色 | 1024 px 以下折叠为图标或抽屉；键盘 Tab/Enter 可完成导航 |
| 主内容区 | 最大宽度不锁死，表格优先横向利用空间；标题、说明、主操作保持同一基线 | 页面首屏不出现无意义大留白；1920 px 和 1280 px 均通过视觉评审 |
| 列表页 | 面包屑 → 标题/描述 → 筛选 → 表格 → 分页；每页统一加载/空/错/无权限状态 | 空状态给出原因和下一步；筛选写入 URL，刷新/分享/前进后退可恢复 |
| 详情页 | 顶部资源名、状态、命名空间和操作；下方固定“概览/实例/事件/日志/YAML”标签 | 标签可深链；危险操作在确认后执行并显示错误 requestId |
| 创建向导 | 三步：基础信息 → 容器与资源 → YAML 预览/确认；步骤状态可见，未通过校验不能前进 | preview 未成功禁用创建；成功跳转详情；400/403/409 有字段/权限/冲突文案 |

### 8.2 视觉 token 与 shadcn 组件映射

| 设计元素 | 规格 | 组件实现 |
|---|---|---|
| 主色 | `#0B63CE`，用于主按钮、选中态和链接；状态色不得复用主色 | Tailwind semantic token；`Button`、`Badge`、`Progress` |
| 背景与正文 | 背景 `#F5F7FA`、卡片 `#FFFFFF`、正文 `#1D2129`、辅助 `#6B7785` | `Card`、`Table`、`Tooltip` |
| 成功/警告/危险 | `#1F9D55` / `#D97706` / `#D92D20`；必须同时配文本或图标 | `StatusBadge` + Lucide 图标 |
| 卡片 | 8 px 圆角、1 px 边框、克制阴影；卡片标题 16 px/600 | shadcn `Card` 封装 |
| 表格 | 默认行高 44 px；表头冻结在视觉层；数值右对齐；操作列固定语义 | TanStack Table + shadcn `Table` |
| 弹窗 | 危险操作用 AlertDialog；创建/编辑用 Dialog；焦点陷阱和 Escape 关闭 | shadcn `Dialog`、`AlertDialog` |
| 表单 | Label 与错误信息关联；输入错误使用 `aria-invalid`；必填标识有文字说明 | `Label`、`Input`、`Textarea`、`Select` |
| 反馈 | 成功用 Toaster；页面级失败用 ErrorState；流断开显示内联状态和重连动作 | `sonner`、`LoadingState`、`EmptyState`、`ErrorState` |
| 图表 | 只用 Recharts，轴和图例文字满足对比度；指标缺失显示“不可用”而非 0 | `MetricCard`、`ResourceBar` |

**视觉验收方法**

1. 用固定 viewport `1440×900`、`1024×768`、`390×844` 各保存总览、工作负载列表、Pod 详情、创建向导和登录页截图。
2. 逐项核对顶部栏、侧栏、标题/操作、筛选、表格密度、状态表达和空/错/加载状态；不接受“只有截图能看、键盘不可用”。
3. 运行 Playwright 主路径和 OIDC 真实浏览器矩阵，确认 401 回登录、403 无权限、URL 恢复、日志断线提示和按钮权限。
4. 通过静态扫描确认没有 `dangerouslySetInnerHTML`、社区面板资产或从外部页面复制的 CSS/JS。
5. 视觉评审未通过时只能标记“UI 实现中”，不能以功能接口通过替代视觉验收。

## 9. API 契约

### 9.1 通用规则

| ID | 类型 | 优先级 | 需求 | 验收方式 |
|---|---|---|---|---|
| API-U-001 | U | P0 | 系统应仅使用 `/api/v1` 下的显式资源接口，不提供任意 K8s API 代理。 | 路由表审查。 |
| API-U-002 | U | P0 | 系统应使用统一的错误结构，包含 `code`、`message`、`requestId` 和可选 `fieldErrors`。 | 契约测试。 |
| API-U-003 | U | P0 | 系统应返回 `X-Request-ID`，并在日志和审计中关联同一请求。 | 集成测试。 |
| API-U-004 | U | P0 | 系统应在列表响应中返回分页、排序和筛选元数据。 | 契约测试。 |
| API-N-001 | N | P0 | 如果请求体超过 2 MiB，系统应返回 413。 | 边界测试。 |
| API-N-002 | N | P0 | 如果客户端请求不支持的排序字段或资源类型，系统应返回 400。 | 负向测试。 |
| API-N-003 | N | P0 | 系统不应在 URL、日志或错误消息中记录 Session Cookie、密码、Secret 值或 Bearer Token。 | 日志与抓包审查。 |

### 9.2 核心路由

| 方法 | 路径 | 用途 | 权限 |
|---|---|---|---|
| `GET` | `/api/v1/auth/config` | 查询认证模式、IdP 显示名和登录入口 | 匿名 |
| `GET` | `/api/v1/auth/oidc/login` | 发起 OIDC Authorization Code + PKCE | 匿名，`oidc` 模式 |
| `GET` | `/api/v1/auth/oidc/callback` | 处理 IdP 回调并建立会话 | 匿名，一次性 state Cookie |
| `GET` | `/api/v1/auth/oidc/logout` | 本地退出并跳转 IdP RP logout | 匿名/已登录，`oidc` 模式 |
| `GET` | `/api/v1/session` | 查询当前会话 | 登录用户 |
| `POST` | `/api/v1/session` | 本地账号登录；仅 `local` 模式挂载 | 匿名，限速 |
| `DELETE` | `/api/v1/session` | 退出 | 登录用户 |
| `GET` | `/api/v1/capabilities` | 查询能力 | 登录用户 |
| `GET` | `/api/v1/overview` | 集群总览 | 只读 |
| `GET` | `/api/v1/nodes` | 节点列表 | 只读 |
| `GET` | `/api/v1/nodes/{name}` | 节点详情 | 只读 |
| `POST` | `/api/v1/nodes/{name}/cordon` | 停止节点调度，不驱逐现有 Pod | 写、`update nodes` |
| `POST` | `/api/v1/nodes/{name}/uncordon` | 恢复节点调度 | 写、`update nodes` |
| `GET` | `/api/v1/namespaces` | 命名空间列表 | 只读 |
| `GET` | `/api/v1/workloads` | 工作负载列表 | 只读 |
| `POST` | `/api/v1/workloads/deployments` | 校验 Dry Run 后创建 Deployment | 写、创建 |
| `POST` | `/api/v1/workloads/deployments/preview` | 生成与创建路径一致的 YAML 预览 | 写能力、无写入 |
| `GET` | `/api/v1/workloads/{kind}/{namespace}/{name}` | 工作负载详情 | 只读 |
| `POST` | `/api/v1/workloads/{kind}/{namespace}/{name}/scale` | 扩缩容 | 写 |
| `POST` | `/api/v1/workloads/{kind}/{namespace}/{name}/restart` | 重启 | 写 |
| `DELETE` | `/api/v1/workloads/{kind}/{namespace}/{name}` | 删除 | 写 |
| `GET` | `/api/v1/pods` | Pod 列表 | 只读 |
| `GET` | `/api/v1/pods/{namespace}/{name}` | Pod 详情 | 只读 |
| `GET` | `/api/v1/pods/{namespace}/{name}/logs` | 日志流 | 只读 |
| `GET` | `/api/v1/events` | 事件列表 | 只读 |
| `GET` | `/api/v1/events/stream` | 事件流 | 只读 |
| `GET` | `/api/v1/services` | Service 列表 | 只读 |
| `GET` | `/api/v1/ingresses` | Ingress 列表 | 只读 |
| `GET` | `/api/v1/configmaps` | ConfigMap 列表 | 只读 |
| `GET` | `/api/v1/secrets` | Secret 元数据列表 | 只读 |
| `POST` | `/api/v1/secrets/{namespace}/{name}/reveal` | 解密查看 | 敏感读 |
| `GET` | `/api/v1/storageclasses` | StorageClass 列表 | 只读 |
| `GET` | `/api/v1/persistentvolumes` | PV 列表 | 只读 |
| `GET` | `/api/v1/persistentvolumeclaims` | PVC 列表 | 只读 |
| `GET` | `/api/v1/yaml` | 只读 YAML | 只读 |
| `POST` | `/api/v1/yaml/apply` | Dry Run 后应用 | 写，可选 |

`SessionResponse` 必须包含 `authMode`，前端不得根据环境变量或按钮状态推断认证模式。
OIDC 模式下不挂载 `POST /api/v1/session`；`DELETE /api/v1/session` 只清除本地会话，
企业 SSO 的完整退出使用 `/api/v1/auth/oidc/logout`。

## 10. 部署与运维需求

| ID | 类型 | 优先级 | 需求 | 验收方式 |
|---|---|---|---|---|
| OPS-U-001 | U | P0 | 系统应通过 Helm Chart 部署到 `dosh` 命名空间。 | `helm list -n dosh`。 |
| OPS-U-002 | U | P0 | 系统应使用名为 `dosh` 的 Deployment、Service 和 Helm Release。 | `kubectl get deploy,svc -n dosh`。 |
| OPS-U-003 | U | P0 | Service 应使用 NodePort `32626`，并转发到 Pod 的 `32626` 端口。 | `kubectl get svc -n dosh`。 |
| OPS-U-004 | U | P0 | 系统应默认部署 2 个无状态副本，并启用跨节点反亲和。 | `kubectl get pod -n dosh -o wide`。 |
| OPS-U-005 | U | P0 | 每个副本应请求 `100m CPU / 128Mi`，限制为 `500m CPU / 512Mi`。 | Deployment 资源字段检查。 |
| OPS-U-006 | U | P0 | Pod 应以非 root 用户运行、只读根文件系统、丢弃全部 Linux capabilities，并使用 `RuntimeDefault` seccomp。 | Pod Security Context 检查。 |
| OPS-U-007 | U | P0 | 系统应配置 startup、readiness 和 liveness Probe。 | 滚动更新测试。 |
| OPS-U-008 | U | P0 | 系统应使用独立 ServiceAccount，并遵循最小权限原则。 | `kubectl auth can-i --list` 审查。 |
| OPS-U-009 | U | P0 | 系统不应依赖 PVC，不创建 StatefulSet 或数据库。 | `kubectl get pvc -n dosh` 为空。 |
| OPS-U-010 | U | P0 | 系统应通过 `dosh` 命名空间中的 Secret 注入管理员密码哈希、Session Key 和 TLS 材料。 | Secret 挂载与启动验证。 |
| OPS-U-011 | U | P0 | 系统应提供 PDB，保证滚动维护时至少 1 个副本可用。 | `kubectl get pdb -n dosh`。 |
| OPS-U-012 | U | P0 | 系统应设置 `allowPrivilegeEscalation: false`、`readOnlyRootFilesystem: true` 和显式 `/tmp` emptyDir。 | Pod spec 检查。 |
| OPS-U-013 | U | P0 | 系统应默认只向 stdout 写审计；仅当显式配置 Webhook 时，才通过 Secret 注入 Token/HMAC，并为出口单独放行目标网段和端口。 | Helm 渲染、Secret 引用和 NetworkPolicy/CNP 检查。 |
| OPS-U-014 | U | P1 | 系统应在 Metrics API 缺失时保持全部主页面可用，不得因可选依赖失败阻断总览、节点和 Pod 页面。 | 当前集群与 fake client 双态验收。 |
| OPS-U-015 | U | P0 | 当以生产候选发布时，系统应允许用 `image.digest` 将镜像固定为 `sha256:<64 hex>`；设置 digest 后 Deployment 必须使用 `repo@digest` 而不是可变 tag。 | `helm template` 正/负向断言；`kubectl get deploy -o jsonpath` 核验镜像身份。 |
| OPS-U-016 | U | P0 | 同一候选的 Makefile、Chart `version/appVersion`、values `image.tag`、OpenAPI、npm package/lock 和 README 版本必须一致。 | `make version-check`。 |
| OPS-E-001 | E | P0 | 当执行 Helm 安装或升级时，系统应输出清晰的成功或失败信息，并支持 `helm rollback`。 | 安装、升级、回滚演练。 |
| OPS-N-001 | N | P0 | 如果 `dosh` 命名空间不存在，安装过程应使用 `--create-namespace` 创建，不得部署到其他命名空间。 | 安装日志和资源位置检查。 |
| OPS-N-002 | N | P0 | 如果端口 32626 被其他 Service 占用，部署应失败并记录冲突原因。 | 冲突测试。 |
| OPS-N-003 | N | P0 | 如果镜像拉取失败或 TLS Secret 无效，Deployment 应保持非 Ready，不得静默回退到不安全模式。 | 故障注入测试。 |
| OPS-O-001 | O | P2 | 当安装 Ingress Controller 后，系统应支持通过 Ingress 暴露，但 NodePort 仍可独立使用。 | Ingress 可选部署测试。 |

### 10.1 Helm 默认值

| 配置 | 默认值 |
|---|---|
| `namespace` | `dosh` |
| `replicaCount` | `2` |
| `image.repository` | `192.168.122.10:5000/dosh/dosh` |
| `image.tag` | `0.1.3`（仅用于本地/开发；生产发布必须同时设置 `image.digest`） |
| `image.digest` | 空；设置后必须匹配 `^sha256:[a-fA-F0-9]{64}$`，并渲染为 `repository@digest` |
| `service.type` | `NodePort` |
| `service.port` | `32626` |
| `service.nodePort` | `32626` |
| `containerPort` | `32626` |
| `resources.requests.cpu` | `100m` |
| `resources.requests.memory` | `128Mi` |
| `resources.limits.cpu` | `500m` |
| `resources.limits.memory` | `512Mi` |
| `tls.mode` | `existingSecret` |
| `tls.secretName` | `dosh-tls` |
| `auth.existingSecret` | `dosh-auth` |
| `auth.mode` | `local`（Chart 已支持 `local` / `oidc` 双模式；生产集群仍使用 `local`，OIDC 仅在隔离测试 Release 部署） |
| `auth.oidc.issuer` | 空；`oidc` 模式必填 |
| `auth.oidc.clientID` | 空；`oidc` 模式必填 |
| `auth.oidc.redirectURL` | 空；必须等于 IdP 注册的回调 URL |
| `auth.oidc.existingSecret` | `dosh-oidc`，保存 client secret |
| `auth.oidc.ca.existingConfigMap` | 空；私有 CA 时指定 |
| `networkPolicy.oidc.ipBlocks` | `[]`；启用 OIDC 且启用 NetworkPolicy 时必须明确 IdP 出口 CIDR |
| `rbac.create` | `true`（显式资源白名单；不含 exec/port-forward） |
| `readOnly` | `false`（可通过 values 改回 `true`） |
| `antiAffinity.required` | `true` |
| `podDisruptionBudget.minAvailable` | `1` |
| `networkPolicy.enabled` | `true` |
| `networkPolicy.ciliumApiServer.autoDetect` | `true`（检测到 `cilium.io/v2` 时自动创建 CNP） |
| `networkPolicy.ciliumApiServer.enabled` | `false`（显式强制模式；自动探测失败时用于覆盖） |
| `audit.webhook.enabled` | `false` |
| `audit.webhook.url` | 空（启用时必填） |
| `audit.webhook.existingSecret` | `dosh-audit` |
| `audit.webhook.tokenKey` | 空；配置时只从 Secret 读取 Bearer Token |
| `audit.webhook.hmacKeyKey` | `hmac-key` |
| `audit.webhook.ca.existingConfigMap` | 空；私有 CA 时指定，只读挂载到 `/etc/dosh/audit/ca.crt` |
| `audit.webhook.ca.key` | `ca.crt`；ConfigMap 中的 CA 键名 |
| `audit.webhook.timeout` | `3s` |
| `audit.webhook.queueSize` | `1024` |
| `networkPolicy.webhook.ipBlocks` | `[]`；启用审计 Webhook 且启用 NetworkPolicy 时必须显式配置，禁止 `0.0.0.0/0` |
| `networkPolicy.webhook.ports` | `[443]`；必须与审计接收端监听端口一致 |

### 10.2 OIDC 部署契约（Chart 已实现；签名测试 IdP 集群矩阵已通过）

`local` 与 `oidc` 必须通过同一组模板路径渲染，不复制两套 Deployment。`auth.mode=oidc` 时：

1. Deployment 注入 `DOSH_AUTH_MODE=oidc`、issuer、client ID、redirect URL、scopes、claim 和 CA 路径；
   client secret 只通过 `secretKeyRef` 注入，不进入 values 明文或 ConfigMap。
2. 删除本地管理员密码环境变量的强依赖；`DOSH_SESSION_KEY` 仍必需，OIDC state 加密复用该密钥。
3. ClusterRole 切换为 OIDC 专用最小权限：impersonate users/groups、create SubjectAccessReview、
   API discovery/version；不得保留工作负载、Secret、节点等业务资源直接读写权限。
4. ServiceAccount 只绑定上述 ClusterRole；业务请求由 impersonation 身份受目标用户 RBAC 约束。
5. NetworkPolicy/CNP 同时放行 Kubernetes API、DNS、OIDC issuer 的 HTTPS 出口；标准 NetworkPolicy
   必须配置明确 CIDR，不能使用 `0.0.0.0/0` 掩盖 IdP 出口。Cilium 可使用 `toFQDNs`，但必须有渲染断言。
6. 健康检查不得依赖 IdP 在线；IdP 不可达时应阻止新登录并返回可解释错误，但 `/healthz` 仍可用，
   `/readyz` 的降级语义必须写清楚，不能假装 IdP 可用。
7. 保留 `auth.mode=local` 的向后兼容渲染；升级前先验证两种模式的 `helm template` 差异。

**验收渲染命令**

```bash
make helm-check
# scripts/check-helm.sh 内部执行并断言：
# local、local+Cilium、oidc、oidc+Cilium 四类正向渲染；
# 缺 IdP CIDR、HTTP issuer、非法 auth.mode 三类负向拒绝；
# client secret 仅 secretKeyRef；OIDC RBAC 无业务资源权限；NodePort 32626；2 副本。
```

通过标准：local 路径不含 OIDC 必需变量且保留本地登录；OIDC 路径不出现业务资源写权限；
client secret 只以 `secretKeyRef` 出现；端口仍为 32626，命名空间仍为 `dosh`；非法 OIDC 配置
在渲染期失败而不是部署后失败。渲染通过不等于企业 IdP 可登录；当前签名测试 IdP 的隔离集群
部署与三身份矩阵见 15.10 和 21.2.3，生产 `dosh` 仍未升级到 OIDC。

## 11. 安全需求

| ID | 类型 | 优先级 | 需求 | 验收方式 |
|---|---|---|---|---|
| SEC-U-001 | U | P0 | 系统应使用 HTTPS，并设置 HSTS、`X-Content-Type-Options: nosniff`、`Referrer-Policy` 和 CSP。 | 响应头测试。 |
| SEC-U-002 | U | P0 | 系统应禁止跨域写请求，并对所有状态变更执行 CSRF 校验。 | 安全测试。 |
| SEC-U-003 | U | P0 | 系统应对 Secret 值和密码进行日志、错误和审计脱敏。 | 日志扫描。 |
| SEC-U-004 | U | P0 | 系统应对登录、退出、Secret 查看和全部写操作记录结构化审计日志。 | 审计测试。 |
| SEC-U-005 | U | P0 | 系统应限制请求体大小、Kubernetes 客户端并发和 API 超时。 | 配置与压力测试。 |
| SEC-U-006 | U | P1 | 当启用审计 Webhook 时，系统应使用 HTTPS、Bearer Token 或 HMAC-SHA256（可同时启用），并通过 `X-Request-ID` 关联请求；HMAC 原文必须为 `<timestamp>.<body>`。 | Webhook 集成测试和抓包验收。 |
| SEC-U-007 | U | P1 | 系统应将审计 Webhook 作为非阻塞旁路：有限重试、有界队列；队列满或最终失败时记录错误日志，但不得静默成功或阻塞业务请求。 | 队列/重试测试和故障注入。 |
| SEC-N-001 | N | P0 | 如果检测到未认证访问受保护 API，系统应返回 401 且不泄露资源是否存在。 | 安全测试。 |
| SEC-N-002 | N | P0 | 如果响应包含 Secret 明文，系统只能通过显式 reveal 接口返回，并禁止缓存。 | 响应头与缓存测试。 |
| SEC-N-003 | N | P0 | 如果配置了通配符 CORS、明文默认密码或关闭 CSRF，系统应拒绝生产模式启动。 | 配置负向测试。 |
| SEC-O-001 | O | P1 | 当启用 NetworkPolicy 时，系统应只允许入口 TCP 32626、DNS 和 Kubernetes API 出口。 | 网络策略测试。 |
| SEC-O-002 | O | P1 | 当同时启用审计 Webhook 和 NetworkPolicy 时，系统应只允许配置的 Webhook IP/CIDR 与端口，不得使用无约束的 `0.0.0.0/0` 作为 Webhook 出口。 | Helm 渲染断言与网络连通性测试。 |
| SEC-O-003 | O | P1 | 当启用 OIDC 时，系统应校验 issuer HTTPS、签名、audience、nonce 和过期时间，并支持通过只读 CA 文件信任私有 IdP。 | 真实签名 JWT、错误 issuer/audience、JWKS 轮换和私有 CA 测试。 |
| SEC-O-004 | O | P1 | 当 OIDC 模式使用最小 RBAC 时，ServiceAccount 不应直接拥有业务资源读写权限，只能 impersonate 用户/组并创建 SAR。 | `kubectl auth can-i --list` 和 Helm 渲染断言。 |
| SEC-O-005 | O | P1 | 当 OIDC 回调重定向时，系统应只允许站内相对路径，拒绝 `//evil.example`、绝对外站 URL 和 CRLF 注入。 | 开放重定向负向测试。 |
| SEC-N-004 | N | P0 | 如果 OIDC secret、ID Token、access token 或 refresh token 出现在日志、审计、URL 或错误响应中，该版本不得发布。 | 日志/响应扫描和代码审查。 |
| SEC-N-005 | N | P0 | 如果发布二进制存在可达 Go 漏洞，或生产依赖存在 HIGH/CRITICAL 漏洞，或镜像扫描存在未接受 HIGH/CRITICAL，该候选不得发布。 | `govulncheck ./...`、`npm audit --omit=dev`、`make image-scan` 与风险接受记录。 |

## 12. 非功能需求

| ID | 类型 | 优先级 | 需求 | 验收方式 |
|---|---|---|---|---|
| NFR-U-001 | U | P0 | 在 1000 个 Pod、200 个工作负载的测试数据下，列表接口 P95 应小于 1 秒。 | 基准测试。 |
| NFR-U-002 | U | P0 | 在 1000 个 Pod 的测试数据下，总览接口 P95 应小于 1.5 秒。 | 基准测试。 |
| NFR-U-003 | U | P0 | 首个日志数据块应在建立连接后 2 秒内返回。 | 集成测试。 |
| NFR-U-004 | U | P0 | 前端首屏在局域网环境下的 LCP 应小于 2.5 秒。 | Lighthouse 或 Playwright 指标。 |
| NFR-U-005 | U | P0 | 初始 JavaScript 资源 gzip 后应小于 500 KiB。 | 构建产物统计。 |
| NFR-U-006 | U | P0 | 后端单副本稳态内存应小于 256 MiB。 | 压力测试和 `kubectl top`。 |
| NFR-U-007 | U | P0 | 系统应支持最近两个主版本 Chrome、Edge、Firefox 和 Safari。 | Playwright 浏览器矩阵。 |
| NFR-U-008 | U | P1 | 核心流程应符合 WCAG 2.1 AA 的基本键盘操作、对比度和可读性要求。 | axe 扫描和人工检查。 |
| NFR-E-001 | E | P0 | 当 Kubernetes API 出现 429 或临时 5xx 时，系统应有限重试并避免形成重试风暴。 | 故障注入测试。 |
| NFR-E-002 | E | P0 | 当 SSE 或日志流断开时，系统应允许重连，且单客户端不得无限累积内存。 | 长连接测试。 |
| NFR-E-003 | E | P1 | 当审计 Webhook 返回 429/5xx 或超时时，系统应最多尝试 3 次并记录最终失败；重试不得阻塞原业务请求。 | `httptest` 故障注入与队列积压测试。 |
| NFR-E-004 | E | P1 | 当进程收到终止信号时，系统应在终止宽限期内停止接收新请求、排空 HTTP 服务并尝试刷新审计队列。 | SIGTERM 集成测试和集群滚动升级验收。 |

## 13. 测试策略

| ID | 类型 | 优先级 | 需求 | 验收方式 |
|---|---|---|---|---|
| TST-U-001 | U | P0 | 每个模块的 app 和 domain 应有单元测试，覆盖成功、校验失败、权限拒绝和依赖失败。 | `go test ./...`。 |
| TST-U-002 | U | P0 | Kubernetes adapter 应使用 fake client 或 envtest 验证对象转换、错误映射和写操作。 | adapter 测试。 |
| TST-U-003 | U | P0 | HTTP adapter 应使用 `httptest` 验证状态码、DTO、错误结构和安全响应头。 | handler 测试。 |
| TST-U-004 | U | P0 | 前端业务组件应使用 Vitest 和 Testing Library 覆盖加载、空、错误、无权限和成功状态。 | `npm test`。 |
| TST-U-005 | U | P0 | 前端应以 MSW 模拟 API，不允许依赖真实集群运行组件测试。 | 测试配置检查。 |
| TST-U-006 | U | P0 | 应使用 Playwright 覆盖登录、总览、节点、工作负载、日志和受控写操作主路径。 | E2E 报告。 |
| TST-U-007 | U | P0 | CI 应执行 `go test -race`、前端类型检查、构建、架构 lint 和 OpenAPI 契约校验。 | CI 绿灯。 |
| TST-U-008 | U | P0 | 发布前应执行 `govulncheck`、前端依赖审计和容器镜像漏洞扫描。 | 安全检查报告。 |
| TST-U-009 | U | P0 | Metrics adapter 应使用 fake Metrics client 覆盖节点、Pod、详情和 API 不可用分支，断言 `hasMetrics` 与实际数值。 | `go test ./internal/platform/kube -run Metrics`。 |
| TST-U-010 | U | P0 | 列表筛选应覆盖 URL 解析、刷新恢复、前进/后退和分享链接；主 E2E 套件必须与性能套件分离。 | 路由单测和 `npm run e2e:container`。 |
| TST-U-011 | U | P1 | 审计 Webhook 应覆盖 Bearer、HMAC、Request ID、重试、非 2xx、队列满、敏感字段不进 payload 和关闭排空。 | `go test -race ./internal/platform/audit`。 |
| TST-U-012 | U | P1 | 配置校验应覆盖审计 URL 非绝对地址、缺少 Token/HMAC、超时越界和队列越界。 | `go test ./internal/platform/config`。 |
| TST-U-013 | U | P1 | OIDC app 应覆盖合法回调、state 篡改/过期、nonce 不匹配、开放重定向、Token 过期限制、admin group 映射和 logout。 | `go test -race ./internal/modules/auth/...`。 |
| TST-U-014 | U | P1 | OIDC provider adapter 应使用真实 RSA 签名的 JWT 覆盖合法 token、错误签名、错误 issuer、错误 audience、过期、JWKS 轮换，以及 email username claim 的 `email_verified` 缺失/`false`/email 缺失分支。 | `go test -race ./internal/platform/oidc -count=1 -v`。 |
| TST-U-015 | U | P1 | Kubernetes 授权应断言 SubjectAccessReview 的 user/groups/resourceAttributes、无 impersonation 头、EvaluationError 和拒绝时 Repository 调用次数为 0。 | `go test -race ./internal/platform/kube ./internal/modules/...`。 |
| TST-U-016 | U | P1 | OIDC 应提供真实 IdP 或本地签名测试 IdP 的浏览器 E2E，覆盖 viewer/operator/admin 三身份读写矩阵。 | Playwright OIDC suite 报告。 |
| TST-N-001 | N | P0 | 如果需求没有对应自动化测试或人工验收记录，该需求不得标记为已验收。 | 追踪矩阵检查。 |
| TST-N-002 | N | P0 | 如果以容器运行 Playwright，系统应禁止与宿主共享 IPC，并配置单 worker、每浏览器独立容器、内存/CPU/PID/shm 上限和硬超时；任一限制缺失时测试门禁应失败。 | `make playwright-check`、运行器 dry-run 和 `podman inspect` 资源检查。 |
| TST-N-003 | N | P0 | 浏览器测试应固定 `retries=0` 并拒绝 `--workers`、`--fully-parallel` 或正数 `--retries` 覆盖，避免失败用例通过重试掩盖并发/资源问题。 | runner dry-run、配置静态检查和 `make playwright-check`。 |

## 14. 分阶段实施计划

### 14.0 阶段状态总览

| 阶段 | 当前状态 | 判定依据 | 进入下一阶段前仍欠缺的证据 |
|---|---|---|---|
| 0 基线与架构护栏 | 本地已验收（CI 真跑待补） | 工程可构建；`make arch` 已规则化检查依赖/模块/Router/环境变量边界；OpenAPI 路由双向对账和模块路由归属测试通过；v2.46 当前候选 `make verify`、全量 race、架构/API/Helm/版本/Playwright 隔离和源码绑定门禁通过；Helm/CI 文件已建立 | GitHub Actions 真跑、分支保护和真实环境差异修复 |
| 1 骨架、配置、前端壳 | 实验室已验收（隔离 Release） | 健康检查、SPA fallback、严格配置校验、32626 端口和动态集群上下文已运行；v2.46 当前候选隔离 OIDC Release 验收时 revision 209、当前复核 revision 210、1/1 Ready，mock/真实 OIDC 三浏览器均通过 | 企业 IdP、Edge/Safari 实机和目标生产环境复验；生产发布 |
| 2 本地认证与权限 | 实验室已验收 | 登录、Cookie、CSRF、只读模式、审计 stdout 已验收 | 生产多用户身份转到阶段 8 |
| 3 只读核心能力 | 实验室已验收 | 总览、节点、工作负载、Pod、日志已与真实集群对账；v2.46 当前候选通过 OIDC 三身份 API/RBAC、Metrics 三态和 mock/真实 OIDC 三浏览器矩阵 | 企业 IdP、Edge/Safari 和生产目标环境复验；生产 Metrics 拓扑 |
| 4 事件、日志、配置和 Secret | 实验室已验收 | SSE、日志流、Secret 脱敏/reveal、YAML 只读已实现；v2.46 当前候选通过三浏览器、隔离 Audit/Reveal、真实 Loki 59 事件/34 requestId 和 30 分钟 soak | 生产/预发同等并发复验；企业 SIEM 保留与告警 |
| 5 安全写操作 | 实验室已验收（增量范围） | dry-run/apply、扩容、409、重启、删除、Deployment 创建向导与审计已实现；v2.46 当前 digest 通过工作负载完整生命周期、Cordon/Uncordon、CronJob 详情和浏览器写操作路径 | 仍需在完整候选变更单中复验闭环；生产发布未执行 |
| 6 网络、存储与监控 | 实验室已验收（增量范围） | Service/Ingress 详情和 StorageClass/PV/PVC 详情已与真实隔离 API 对账，存储 fixture 清理后确认 404；v2.46 当前候选已完成 Metrics 三态和真实 Loki 投递/检索 | 生产 Metrics 拓扑；企业 SIEM/OpenSearch 保留、告警和访问审批 |
| 7 发布加固与验收 | 实验室已验收（生产发布未执行） | `0.1.2/0.1.4` 仍为 revision 12、2/2 Ready；v2.46 当前候选通过 `make verify`、Vitest 62/62、race、rollback/restore、Metrics、retry、Audit/Loki、soak、性能/bundle、govulncheck、npm audit、Trivy 和 599 文件最终归档；离机 WORM 未完成 | 真实 CI 绿灯、受信 TLS、外部商用门禁、digest 生产发布与回滚 |
| 8 多用户身份与商用关闭 | 实验室已验收（外部商用门禁未关闭） | OIDC app/HTTP/adapter、SAR、动态 capabilities、impersonation、前端退出和 Helm 双模式已实现；v2.46 当前候选通过三身份 API/RBAC、真实 OIDC 三浏览器各 16/16、6/6 denied 审计和 Token 扫描 0；企业 IdP、CI、TLS、SIEM、WORM 和生产发布仍未关闭 | 企业 IdP；Edge/Safari 实机；CI 真跑；受信 TLS；企业 SIEM；离机 WORM；digest 生产发布 |

状态口径：**实验室已验收** 只代表受控环境和当前证据集通过；**部分验收** 不代表生产可接受。

### 阶段 0：项目基线与架构护栏

**目标**：建立可编译、可检查、可部署的空壳工程，防止后续架构失控。

**工作内容**

- WBS-0.1：初始化 Go module、React/Vite/TypeScript 工程和目录结构；建立 `Makefile` 统一开发、测试、构建和镜像命令。
- WBS-0.2：用 `scripts/check-architecture.sh` 落实 `http -> app -> domain`、消费者端口、唯一装配根和跨模块 import 禁令。
- WBS-0.3：建立 chi 路由边界：模块 `Mount`、OpenAPI 双向对账、路由 owner 测试和 proxy/通配路由禁令。
- WBS-0.4：建立 `api/openapi.yaml`，定义统一响应、错误、分页和会话模型。
- WBS-0.5：建立 CI，执行格式化、静态检查、单元测试、前端构建和契约生成检查。
- WBS-0.6：建立 Helm Chart 骨架、`dosh` 命名空间参数和 32626 端口配置。
- WBS-0.7：编写最小威胁模型，列出未认证访问、Secret 泄露、CSRF、越权写和 NodePort 暴露风险。
- WBS-0.8：建立路由清单测试：用 `chi.Walk` 收集稳定的 `method + pattern` 清单，与
  `api/openapi.yaml` 做双向差集；对每个模块的 `Mount` 在独立 Router 上验证路径归属。

**产物**

- 可编译的 Go 服务。
- 可构建的 React SPA。
- OpenAPI 骨架。
- 架构 lint 配置。
- Helm Chart 骨架。
- CI 工作流。

**验收门槛**

- `go test ./...`、`go vet ./...`、前端类型检查和构建全部通过。
- `make arch` 输出 `architecture checks: pass`，能阻止 domain/app 越层、跨模块生产 import、
  非 bootstrap Router、`init()` 路由、proxy/通配路由和 `platform/config` 之外的环境变量访问。
- `go test ./internal/bootstrap -run 'TestRouteInventoryMatchesOpenAPI|TestModuleRoutesAreScoped' -count=1 -v`
  通过，路由与 OpenAPI 双向差集为空；新增路由缺少契约测试时 CI 失败。
- `make api-check` 通过，OpenAPI 生成的 TypeScript 类型无漂移。
- `helm lint deploy/helm/dosh` 通过。
- `helm template dosh deploy/helm/dosh -n dosh` 生成的 Service 端口为 32626。
- 代码评审确认不存在全局 Router、`init()` 路由注册和任意 K8s proxy。

**关联需求**：ARC-U-001 至 ARC-U-013、ARC-E-001、ARC-N-001 至 ARC-N-003、SYS-U-001、SYS-U-002、OPS-U-003。

### 阶段 1：后端骨架、配置和前端壳

**目标**：打通从浏览器到 Go 服务，再到 Kubernetes 的最小只读链路。

**工作内容**

- WBS-1.1：实现 `bootstrap` 装配、优雅退出、配置校验和结构化日志。
- WBS-1.2：实现 `/healthz`、`/readyz` 和请求 ID、超时、恢复中间件。
- WBS-1.3：实现 Kubernetes client 工厂、Discovery 缓存和超时配置。
- WBS-1.4：实现前端顶部栏、侧边栏、面包屑、页面容器和设计 token。
- WBS-1.5：实现 TanStack Router 路由树和 TanStack Query Provider。
- WBS-1.6：实现统一加载、空、错误、无权限和断线组件。
- WBS-1.7：用 `go:embed` 提供 SPA，并配置 SPA fallback，不吞掉 `/api` 和探针路径。
- WBS-1.8：让顶部栏、侧栏、关于弹窗和集群健康状态读取 `/api/v1/overview`；定义 loading、Healthy、
  Degraded、Unavailable 和请求失败五种展示，禁止硬编码目标环境事实。

**产物**

- 可运行的后端与前端壳。
- 可访问的健康检查和 `/api/v1/capabilities` 占位实现。
- 设计 token 和基础组件库。

**验收门槛**

- 本地默认 HTTP 运行后 `http://127.0.0.1:32626/healthz` 返回 200；Helm 部署在显式启用 TLS 后，
  `https://<node-ip>:32626/healthz` 返回 200。两套环境不得混用验收前提。
- 未登录访问 `/api/v1/overview` 返回 401。
- 刷新任意前端路由不会出现 404。
- 浏览器开发者工具确认初始 bundle 小于 500 KiB gzip。
- 依赖方向检查通过。
- 将 MSW 的 `/api/v1/overview` 改为非实验室名称、`v9.9.9` 和 `TelemetryCNI` 后，顶部栏、侧栏和
  关于弹窗必须同步展示 API 值；返回 503 时必须展示不可用状态和重试入口，不得仍显示绿色连接。

**关联需求**：SYS-U-001 至 SYS-U-004、SYS-E-001、UX-U-001 至 UX-U-010、OPS-U-006。

### 阶段 2：本地认证与权限能力

**目标**：建立安全默认值和前后端一致的权限控制。

**工作内容**

- WBS-2.1：实现 Argon2id 密码哈希校验和本地管理员登录。
- WBS-2.2：实现加密会话 Cookie、过期、退出和 CSRF 校验。
- WBS-2.3：实现登录限速和统一认证错误。
- WBS-2.4：实现 `/api/v1/capabilities`。
- WBS-2.5：实现前端登录页、会话恢复、401/403 跳转和退出。
- WBS-2.6：实现只读模式，并用测试证明写接口在只读模式下被拒绝。
- WBS-2.7：建立审计事件模型，接入登录、退出和失败尝试。

**产物**

- 本地认证闭环。
- 权限能力接口。
- 登录页与会话守卫。
- 认证审计日志。

**验收门槛**

- 正确密码登录后获得 Secure、HttpOnly、SameSite=Strict Cookie。
- 错误密码不区分原因，连续失败触发限速。
- 缺少 Session Key 或密码哈希时进程拒绝启动。
- 只读模式写接口返回 403，且 fake Kubernetes client 无写调用。
- 审计日志包含时间、用户、动作、结果和 requestId，且无敏感值。

**关联需求**：AUTH-U-001 至 AUTH-N-003、AZ-U-001 至 AZ-N-002、SEC-U-002、SEC-U-004。

### 阶段 3：只读核心能力

**目标**：完成可日常使用的集群总览、节点、命名空间、工作负载和 Pod 页面。

**工作内容**

- WBS-3.1：实现 Overview 模块的并行聚合、部分失败和 Metrics 缺失降级。
- WBS-3.2：实现 Nodes 模块的列表、详情、Conditions、Taints 和节点 Pod。
- WBS-3.3：实现 Workloads 模块的 Deployment、StatefulSet、DaemonSet、Job、CronJob。
- WBS-3.4：实现 Pods 模块的列表和详情。
- WBS-3.5：实现列表筛选、分页和 URL 状态。
- WBS-3.6：实现总览和资源页面。
- WBS-3.7：补充真实集群对账测试，与 `kubectl` 输出比对。

**产物**

- 总览页。
- 节点列表和详情页。
- 工作负载列表和详情页。
- Pod 列表和详情页。
- 导航与状态组件。

**验收门槛**

- 总览中的节点数、Pod 数、工作负载数与 `kubectl` 查询结果一致。
- CPU/内存容量与 requests 汇总与测试脚本一致。
- Metrics API 缺失时页面显示明确提示，不显示伪造利用率；可用时实际 usage 与 `kubectl top` 同量级。
- 节点和 Pod 关键字段与 `kubectl -o wide` 一致。
- 页面刷新后筛选条件可从 URL 恢复。
- app/domain 关键分支测试通过。

**关联需求**：OVW-U-001 至 OVW-N-002、NODE-U-001 至 NODE-U-004、WL-U-001 至 WL-U-003、POD-U-001、POD-U-002、API-U-001 至 API-U-004、NFR-U-001、NFR-U-002。

### 阶段 4：事件、日志、配置和 Secret

**目标**：补齐日常排障所需的可观测与配置查看能力。

**工作内容**

- WBS-4.1：实现事件列表和 SSE 事件流。
- WBS-4.2：实现 Pod 日志选择、tail、follow、重连和错误终止。
- WBS-4.3：实现 ConfigMap 列表和详情。
- WBS-4.4：实现 Secret 元数据、默认脱敏和显式 reveal。
- WBS-4.5：实现只读 YAML 和 Secret YAML 脱敏。
- WBS-4.6：实现日志查看器、事件时间线和前端断线提示。
- WBS-4.7：补充 XSS、ANSI 控制字符和敏感信息泄露测试。
- WBS-4.8：实现管理员审计历史查询；使用有界内存环形缓冲保存当前实例最近 1000 条记录，
  提供 `GET /api/v1/audit-events` 的权限控制、namespace/action/result/关键词过滤、分页和排序，
  并明确该快照不替代 stdout、SIEM、WORM 或重启后的持久保留。
- WBS-4.10：将 Pod 日志升级为有界 NDJSON 帧；`log` 帧保留 UTF-8 日志块，正常 EOF 发 `end`，
  流中失败发带稳定 `code`、安全 `message` 和 `requestId` 的 `error` 并终止；响应头前错误继续使用
  JSON Problem。前端增量解析、显示终止状态并支持手动重连，不改变 `<pre>` 文本渲染和内存上限。

**产物**

- 事件页。
- 日志标签页。
- ConfigMap 与 Secret 页面。
- YAML 查看器。
- 审计展示支持。

**验收门槛**

- SSE 新事件能在 2 秒内显示，断线后可重连。
- 日志流首块在 2 秒内返回，Pod 删除后终止流并显示明确状态。
- 日志流正常结束必须收到 `end`；响应头后的读取失败必须收到结构化 `error` 和 `requestId`，不得输出
  无类型哨兵文本。
- Secret 列表响应不含任何值。
- reveal 请求需要权限、二次确认和审计；响应包含 `Cache-Control: no-store`。
- 日志和事件中的 HTML 不会被执行。

**关联需求**：LOG-U-001 至 LOG-N-002、EVT-U-001 至 EVT-N-001、CFG-U-001、SCR-U-001 至 SCR-E-001、YML-U-001、YML-U-002、API-N-003、SEC-U-003、SEC-N-002。

### 阶段 5：安全写操作

**目标**：在严格确认、权限、并发和审计保护下提供高频运维操作。

**工作内容**

- WBS-5.1：实现工作负载扩容和资源版本校验。
- WBS-5.2：实现滚动重启。
- WBS-5.3：实现删除确认和删除传播策略。
- WBS-5.4：实现 Deployment 创建向导；app 先做领域校验，再由 Kubernetes API Server Dry Run 校验字段，
  最后正式创建；重复资源返回稳定 409。
- WBS-5.5：实现 YAML Dry Run，再按允许列表执行 apply。
- WBS-5.6：实现统一确认弹窗、危险操作视觉和操作结果反馈。
- WBS-5.7：增加写操作审计和 fake client 无越权调用测试。
- WBS-5.8：实现工作负载容器镜像更新；仅允许 Deployment、StatefulSet、DaemonSet，
  使用当前 `resourceVersion` 防止覆盖并发修改，Job/CronJob 明确拒绝，并记录
  `update-image` 审计。前端展示容器级镜像并提供确认弹窗，独立 capability 控制入口。
- WBS-5.9：实现 CronJob 暂停/恢复；仅允许 CronJob，使用当前 `resourceVersion`
  防止覆盖并发修改，非 CronJob 明确拒绝，分别记录 `suspend`/`resume` 审计。
  前端提供状态化确认弹窗，独立 `suspendCronJob` capability 控制入口。

**产物**

- 扩容、重启、删除。
- Deployment 创建向导。
- YAML 编辑与 Dry Run。
- 写操作审计。
- 工作负载镜像更新与并发冲突反馈。
- CronJob 暂停/恢复与并发冲突反馈。

**验收门槛**

- 在临时命名空间 `dosh-e2e` 中完成 Deployment 向导创建、扩容、重启、删除闭环并清理资源。
- 创建向导成功资源与 `kubectl get deployment -o yaml` 对账一致；非法字段不产生写请求；
  viewer 返回 403；重复名称返回 409 且错误码为 `already_exists`；创建与失败均写审计。
- 并发修改触发 409，不覆盖他人变更。
- 无权限和只读模式下不产生 Kubernetes 写请求。
- Dry Run 失败时资源不变。
- 所有写操作在审计中可追踪到用户、请求、资源、动作和结果。
- 镜像更新成功时目标容器镜像生效且其他容器不变；旧 `resourceVersion` 返回 409；
  Job/CronJob 返回 501；无权限或只读模式不产生 Kubernetes 写请求。
- CronJob 暂停后不再创建新 Job，恢复后按调度继续；已有 Job/Pod 不被删除；旧
  `resourceVersion` 且目标状态未生效返回 409；非 CronJob 返回 501；无权限或只读模式
  不产生 Kubernetes 写请求，成功与失败均可检索到对应审计动作。

**关联需求**：WL-E-001 至 WL-N-002、WL-E-006、WL-N-007、YML-O-001 至 YML-N-002、AZ-U-002、AZ-N-001、SEC-U-004、OPS-U-008。

### 阶段 6：网络、存储与监控增强

**目标**：补齐 Service、Ingress、存储入口，并完成 Metrics Server 的可选集成。

**工作内容**

- WBS-6.1：实现 Service 列表和详情。
- WBS-6.2：实现 Ingress 列表和详情；无 IngressClass 时显示明确提示。
- WBS-6.3：实现 StorageClass、PV、PVC 只读页面。
- WBS-6.4：实现 Metrics API 探测与利用率展示。
- WBS-6.5：在隔离测试环境安装 Metrics Server，验证无 Metrics、有 Metrics 和恢复三种状态。
- WBS-6.6：预留 OIDC 与 Ingress 配置入口，但不默认启用。
- WBS-6.7：评估 Cilium Hubble、Hubble Relay 和网络策略展示的后续扩展，不纳入首版。
- WBS-6.8：实现命名空间详情、ResourceQuota 与 LimitRange 展示，补齐 local 模式最小读取 RBAC。

**产物**

- 服务与路由页面。
- 存储页面。
- Metrics 可选展示。
- 依赖缺失状态说明。

**验收门槛**

- 当前无 IngressClass 时页面明确显示未安装状态。
- 当前无 StorageClass 时存储页面显示明确空状态；存在资源时 StorageClass/PV/PVC 可进入详情。
- Metrics Server 不可用时总览正常降级；可用时显示实际 CPU/内存利用率；恢复后无需重启 dosh。
- 所有可选组件失败时不得导致主页面白屏。

**关联需求**：NET-U-001 至 NET-N-001、STO-U-001 至 STO-U-004、STO-N-001 至 STO-N-002、OVW-N-001、NFR-E-001、NFR-E-002、OPS-O-001。

### 阶段 7：发布加固与验收

**目标**：完成可在当前集群稳定运行的正式首版，并建立发布与回滚流程。

**工作内容**

- WBS-7.1：完成 Playwright E2E、race test、依赖漏洞扫描和镜像扫描。
- WBS-7.2：完成 Helm Chart 的默认值、Secret、TLS、PDB、探针和安全上下文。
- WBS-7.3：构建镜像并推送到 `192.168.122.10:5000/dosh/dosh`。
- WBS-7.4：部署到 `dosh` 命名空间，验证 NodePort 32626。
- WBS-7.5：执行滚动升级、失败回滚和重启恢复演练。
- WBS-7.6：记录运行手册、故障排查、证书轮换和 Secret 更新流程。
- WBS-7.7：整理需求追踪矩阵，逐项标记验收结果。

**产物**

- 可部署镜像。
- Helm Release。
- 测试与安全报告。
- 运行手册。
- 验收记录。

**验收门槛**

- `kubectl -n dosh get deploy,svc,pod,pdb,sa` 全部正常。
- `https://<任一节点IP>:32626` 可登录并打开总览。
- `kubectl -n dosh get pvc` 为空。
- Pod 非 root、只读根文件系统、无额外 capabilities。
- 2 个副本分布在 2 个节点，滚动升级期间至少 1 个可用。
- 回滚演练成功，回滚后健康检查和登录正常。
- 全部 P0 需求状态为“已验收”，P1 未完成项有明确记录和风险接受人。

**关联需求**：全部 P0 需求、OPS-U-001 至 OPS-N-003、SEC-U-001 至 SEC-N-003、NFR-U-003 至 NFR-U-008、TST-U-001 至 TST-N-001。

### 阶段 8：多用户身份与商用关闭

**目标**：把本地单管理员升级为 OIDC + Kubernetes RBAC 多用户模式，并关闭阻碍生产试点的高风险缺口。

**工作内容**

- WBS-8.1：在 `domain` 增加 `Actor`、`Access`、`Authorizer` 和 `WithActor`；禁止 domain 依赖 OIDC/client-go。
- WBS-8.2：实现 `platform/authz.Local` 与 `platform/authz.Kubernetes`；写操作、删除、扩缩容、重启、
  Secret reveal、YAML apply 全部在 app 用例中授权。
- WBS-8.3：实现 `platform/oidc` discovery、JWKS、S256 PKCE、nonce、issuer/audience/signature 校验、
  私有 CA 和 RP logout。
- WBS-8.4：实现 `auth/app` 的 state/nonce/verifier 封装、开放重定向防护、会话 TTL 上限和 admin group 映射。
- WBS-8.5：实现 `auth/http` 的 config/login/callback/logout 路由、state/session Cookie 和审计。
- WBS-8.6：在 `platform/kube` 按请求注入 impersonation；把授权评审改为 `SubjectAccessReview`，
  使用 `domain.WithoutActor` 确保 SAR 请求本身不带 impersonation 头。
- WBS-8.7：前端根据 `authMode` 切换登录方式，OIDC 模式退出走 `/api/v1/auth/oidc/logout`；
  所有 MSW/E2E mock 同步 `authMode` 和 `/auth/config`。
- WBS-8.8：Helm 增加 `auth.mode`、OIDC env/Secret/CA、最小 RBAC 和 IdP 出口策略；local 路径保持兼容。
- WBS-8.9：建立签名测试 IdP 和真实 IdP 二选一的 E2E，覆盖 viewer、namespace-operator、admin。
- WBS-8.10：执行商用差距关闭：受信 TLS、Metrics Server、30 分钟 soak、429/5xx、真实 SIEM、
  Edge/Safari 抽样和 CI 真跑。
- WBS-8.11：完成审计 Webhook 商用闭环：私 CA、Bearer/HMAC、Cilium 精确出口、隔离 Release
  业务事件矩阵、接收端故障/恢复、队列压力和 SIGTERM 排空；详细步骤见 22.14。

**产物**

- OIDC 多用户认证与退出。
- 动态 capabilities 与 Kubernetes SubjectAccessReview。
- OIDC 专用最小 RBAC 和 Helm 双模式 values。
- 三身份读写矩阵与真实 IdP 证据。
- 生产试点放行记录。

**验收门槛**

- 本地模式原有登录、CSRF、写操作和安全测试不回归。
- OIDC 模式 `viewer` 读成功、写 403；`operator` 仅授权命名空间可写；`admin` 全量可写。
- 直接调用 API 与 UI 按钮使用同一权限事实；拒绝时不产生业务 Repository/Kubernetes 写请求。
- 错误签名、错误 issuer/audience、nonce mismatch、过期 state、开放重定向全部被拒绝。
- ServiceAccount 无业务资源读写权限，只能 impersonation 和创建 SAR。
- 真实 IdP 登录、logout、Cookie 属性、审计用户名/组和敏感信息扫描通过。
- `helm lint`、local/oidc 双路径渲染和 kubeconform 通过；OIDC 镜像以 digest 部署并保留回滚证据。

**关联需求**：AUTH-O-001 至 AUTH-O-006、AUTH-N-004、AZ-O-001/002、AZ-N-003、SEC-O-003 至 SEC-O-005、
SEC-N-004、TST-U-013 至 TST-U-016、OPS-U-008、RISK-001/005/006/014。

## 15. 验收命令基线

以下命令作为验收参考，具体镜像标签和证书路径按实际版本替换。

### 15.1 代码质量

```bash
test -z "$(gofmt -l cmd internal web/embed.go)"
go vet ./...
go test -race ./...
go test -race ./internal/platform/audit
go test ./internal/platform/config
go test ./internal/platform/kube -run Metrics -count=1 -v
make capacity-test
make lint

cd web
npm run typecheck
npm test
npm run build
npm run bundle:check
make -C .. playwright-check
npm run e2e:container
npm run e2e:performance
```

浏览器 E2E 的通过口径：主配置排除 `performance/**`，Chromium、Firefox、WebKit
当前 15 条主路径在三个浏览器上共 **45/45** 的目标；最近一次完整通过发生在隔离运行器和
API logout 改动之前，当前源码必须重跑后才能沿用该数字。性能 LCP 套件由 `e2e:performance`
独立执行。新增总览面包屑、URL 持久化和动态集群上下文用例后，不得用历史 27/27 或 30/30
替代当前结果。

### 15.2 契约与架构

```bash
make arch
make api-check
go test ./internal/bootstrap -run 'TestRouteInventoryMatchesOpenAPI|TestModuleRoutesAreScoped' -count=1 -v
cd web && npm run typecheck
GOPROXY=https://goproxy.cn,direct go run golang.org/x/vuln/cmd/govulncheck@latest ./...
cd web && npm audit --omit=dev --audit-level=high --registry=https://registry.npmjs.org
make image-scan TAG=<candidate-tag>
```

`make api-check` 会把 `api/openapi.yaml` 生成到临时文件并与提交的
`web/src/api/schema.d.ts` 逐字节比较；命令失败即表示 API 契约与前端类型漂移。
路由清单测试必须同时验证 chi 实际注册路由与 OpenAPI 的双向差集，并验证每个模块的路径归属；
该测试是阶段 0 的交付物，不能用人工浏览路由表替代。
它不依赖 Git 元数据，因此在当前未初始化 `.git` 的交付目录中也可使用。

Playwright 固定为 `@playwright/test 1.64.1` 和
`mcr.microsoft.com/playwright:v1.64.1-noble`。`e2e:container` 在物理宿主默认只调度
Chromium、Firefox 两个独立容器，不依赖宿主机 GUI/GTK/ICU 版本；WebKit 仅在隔离 libvirt
runner 设置 `DOSH_PLAYWRIGHT_ALLOW_WEBKIT=1` 后执行。当前 Fedora 宿主机禁止直接运行 WebKit。
容器入口为 `scripts/run-playwright-container.sh`：默认每浏览器独立容器、`workers: 1`、
私有 shm、cgroup 内存/CPU/PID 限制和硬超时，且禁止 `--ipc=host`。运行前必须执行
`make playwright-check`；不得绕过该运行器直接启动 WebKit。

### 15.3 Helm 静态检查

```bash
helm lint deploy/helm/dosh
helm template dosh deploy/helm/dosh -n dosh \
  --set service.nodePort=32626 \
  --set containerPort=32626 > /tmp/dosh-rendered.yaml
kubeconform /tmp/dosh-rendered.yaml
```

若本机没有 Helm，可在已配置 kubectl 的跳板机执行：

```bash
tar -C deploy/helm -czf /tmp/dosh-chart.tgz dosh
scp /tmp/dosh-chart.tgz root@192.168.122.10:/tmp/
ssh root@192.168.122.10 'rm -rf /tmp/dosh-chart && mkdir -p /tmp/dosh-chart && tar -xzf /tmp/dosh-chart.tgz -C /tmp/dosh-chart && helm lint /tmp/dosh-chart/dosh'
```

### 15.4 部署验收

```bash
helm upgrade --install dosh deploy/helm/dosh \
  --namespace dosh \
  --create-namespace

kubectl -n dosh rollout status deployment/dosh --timeout=180s
kubectl -n dosh get deploy,svc,pod,pdb,sa -o wide
kubectl -n dosh get svc dosh -o jsonpath='{.spec.ports[0].nodePort}'
```

预期结果：

- Deployment 有两个 Ready Pod。
- Service 的 `nodePort` 为 `32626`。
- Pod 分布在至少两个节点。
- 端口输出为 `32626`。

部署前必须在 `dosh` 命名空间准备两个 Secret。Chart 不把明文密钥写入 values 或镜像：

```bash
kubectl create namespace dosh --dry-run=client -o yaml | kubectl apply -f -

printf '%s' '<strong-password>' | ./bin/dosh hash-password > /tmp/admin-password-hash
openssl rand -base64 48 > /tmp/session-key

kubectl -n dosh create secret generic dosh-auth \
  --from-file=admin-password-hash=/tmp/admin-password-hash \
  --from-file=session-key=/tmp/session-key \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n dosh create secret tls dosh-tls \
  --cert=/path/to/tls.crt --key=/path/to/tls.key \
  --dry-run=client -o yaml | kubectl apply -f -
```

证书 SAN 至少应包含实际访问的主机名或全部 Worker Node IP；只访问 `https://<node-ip>:32626` 时，缺失对应 IP SAN 会导致浏览器证书校验失败。

### 15.5 访问验收

```bash
curl -k -I https://192.168.122.100:32626/healthz
curl -k -s https://192.168.122.100:32626/readyz
curl -k -s -o /dev/null -w '%{http_code}\n' \
  https://192.168.122.100:32626/api/v1/overview
```

预期结果：

- `/healthz` 返回 200。
- `/readyz` 返回 200 或明确依赖状态。
- 未登录访问 `/api/v1/overview` 返回 401。

### 15.6 集群资源对账

```bash
kubectl get nodes -o wide
kubectl get pods -A -o wide
kubectl get deploy,sts,ds,job,cronjob -A
kubectl get svc,ingress -A
kubectl get sc,pv,pvc -A
```

验收人员应将 dosh 页面数据与上述命令结果逐项比对，并记录差异原因。

### 15.7 写操作验收

```bash
kubectl create namespace dosh-e2e --dry-run=client -o yaml | kubectl apply -f -
```

先通过向导创建 `dosh-e2e/nginx-ui`，再通过 dosh 依次执行扩容、重启和删除。也可以用同一会话
直接验证 API；以下命令中的 Cookie 与 CSRF token 必须来自当前登录，不得使用生产管理员凭据。

```bash
BASE=https://192.168.122.100:32627
NS=dosh-e2e
NAME=nginx-ui
PAYLOAD='{"name":"'"$NAME"'","namespace":"'"$NS"'","replicas":1,"containerName":"nginx","image":"nginx:alpine","containerPort":80,"cpuRequestMilli":100,"cpuLimitMilli":500,"memoryRequestBytes":134217728,"memoryLimitBytes":268435456,"env":[],"labels":{"app":"dosh-e2e"}}'

# 1. 预览：200 + kind Deployment；不得产生资源
curl -ksS -b /tmp/dosh-cookies -H "X-CSRF-Token: $CSRF" \
  -H 'Content-Type: application/json' -d "$PAYLOAD" \
  "$BASE/api/v1/workloads/deployments/preview" | jq -r '.yaml' | rg 'kind: Deployment|name: nginx-ui'
kubectl -n "$NS" get deployment "$NAME" --ignore-not-found

# 2. 创建：201；再用 kubectl 对账 selector、镜像、端口和资源
curl -ksS -o /tmp/create.json -w '%{http_code}\n' -b /tmp/dosh-cookies \
  -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json' -d "$PAYLOAD" \
  "$BASE/api/v1/workloads/deployments"
kubectl -n "$NS" get deployment "$NAME" -o yaml

# 3. 重复创建：409 + code=already_exists；副本数不得变化
curl -ksS -b /tmp/dosh-cookies -H "X-CSRF-Token: $CSRF" \
  -H 'Content-Type: application/json' -d "$PAYLOAD" \
  "$BASE/api/v1/workloads/deployments" | jq -r '.code'

# 4. 非法字段：400，且带 fieldErrors；kubectl 不得出现新资源
INVALID=${PAYLOAD/nginx:alpine/bad\ image}
curl -ksS -o /tmp/invalid.json -w '%{http_code}\n' -b /tmp/dosh-cookies \
  -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json' -d "$INVALID" \
  "$BASE/api/v1/workloads/deployments"
```

再以 viewer 身份重复创建，预期 403，且目标 namespace 不产生资源；以只读模式重复写操作，预期
同样不产生 Kubernetes 写请求。全部动作在审计中包含 actor/action/resource/namespace/result/requestId。

测试结束执行：

```bash
kubectl delete namespace dosh-e2e --ignore-not-found
```

上例中的 `BASE` 必须指向隔离测试 Release 和非生产 NodePort（当前示例为 `32627`）。如果测试使用
本地认证而不是 OIDC，也必须创建独立 namespace、独立 Release 和未占用的 NodePort；禁止把
`BASE` 改成生产 `https://<node-ip>:32626`，也不得复用生产管理员 Cookie 或 Session Key。

验收通过标准：

- 每个动作在 UI、Kubernetes 状态和审计日志中一致；创建向导产物与 kubectl 对账一致。
- Preview 不写集群；Dry Run 失败时资源不变；重复创建稳定返回 409 `already_exists`。
- 无权限或只读模式不产生写请求。
- 操作失败时显示可定位错误，不静默成功。

### 15.8 发布与回滚验收

```bash
helm -n dosh history dosh
helm -n dosh rollback dosh <previous-revision>
kubectl -n dosh rollout status deployment/dosh --timeout=180s
```

回滚后重新执行健康检查、登录和总览数据对账。

### 15.9 Metrics、URL 状态与审计验收

**Metrics 双态验收**

```bash
# 0. 只有隔离/测试集群才允许安装；脚本固定 namespace `dosh-metrics`
bash scripts/install-metrics-server.sh

# 1. 判断 Metrics API 是否存在
kubectl get apiservice v1beta1.metrics.k8s.io -o jsonpath='{.status.conditions[?(@.type=="Available")].status}{"\n"}'

# 2. 安装 Metrics Server 后，确认节点和 Pod 指标可读
kubectl top nodes
kubectl top pods -A

# 3. 登录后对比 API DTO
curl -ks -b /tmp/dosh-cookies https://192.168.122.100:32626/api/v1/overview | jq '.cluster.metricsAvailable, .capacity.cpuUsageMilli, .capacity.memoryUsageBytes'
curl -ks -b /tmp/dosh-cookies 'https://192.168.122.100:32626/api/v1/pods?namespace=default' | jq '.items[] | {name, hasMetrics, cpuUsageMilli, memoryUsageBytes}'

# 4. 自动执行“可用 → 禁用 → 恢复”三态对账
DOSH_E2E_KEEP_ARTIFACTS=1 DOSH_E2E_SKIP_BUILD=1 \
DOSH_E2E_IMAGE_TAG=<candidate-tag> \
  bash scripts/e2e-metrics.sh
```

通过标准：Metrics 不存在时 `metricsAvailable=false`，页面出现明确降级文案，不能把缺失值显示为 0；
Metrics 存在时节点/Pod 的 `hasMetrics=true`，数值与 `kubectl top` 同量级，页面刷新后不丢失。
注意：fake client 测试只证明转换逻辑，不等于真实 Metrics Server 已验收。

2026-11-02 的隔离测试 Release 已取得以下证据；证据文件为
`/tmp/tmp.MwkrC9s1Mj/metrics-evidence.log`：

```text
available   metricsAvailable=true  cpuUsageMilli=2035 memoryUsageBytes=10395652096 nodes>0 pods.hasMetrics>0
unavailable metricsAvailable=false usage=0 node.hasMetrics=false
recovered   metricsAvailable=true  cpuUsageMilli=2155 memoryUsageBytes=10395906048
```

该结果证明 Metrics adapter、降级路径和探测缓存恢复在测试 Release 上可用；目标生产发布仍需在
受信证书、目标镜像和最终集群拓扑下复跑，不能把测试 Release 结果写成生产验收。

**筛选 URL 持久化验收**

```bash
cd web
npx playwright test -g '筛选条件持久化到 URL'
```

人工步骤：在工作负载页设置 `q=web`、`namespace=default`、`kind=deployment`；复制 URL 到新标签页，
刷新并执行浏览器前进/后退。通过标准：三个参数均保留，表格结果与 URL 一致，空状态和错误状态不被伪造。

**审计 Webhook 验收**

```bash
go test -race ./internal/platform/audit -count=1 -v
go test ./internal/platform/config -count=1 -v
go test -race ./internal/platform/audit ./internal/platform/config ./internal/bootstrap -count=1 -v

helm template dosh deploy/helm/dosh -n dosh \
  --set audit.webhook.enabled=true \
  --set audit.webhook.url=https://siem.example.test/audit \
  --set audit.webhook.tokenKey=token \
  --set audit.webhook.hmacKeyKey= \
  --set audit.webhook.ca.existingConfigMap=dosh-audit-ca \
  --set networkPolicy.webhook.ipBlocks[0]=203.0.113.10/32 \
  > /tmp/dosh-audit.yaml
grep -A8 'name: DOSH_AUDIT_WEBHOOK' /tmp/dosh-audit.yaml
grep -A12 'audit-webhook' /tmp/dosh-audit.yaml
make helm-check
```

通过标准：默认 values 不注入 Webhook；启用后只从 Secret 读取 Token/HMAC；Header 含
`X-Request-ID`、`X-Dosh-Timestamp`、`X-Dosh-Signature`；签名原文为 `timestamp + "." + body`；
3 次失败后记录最终失败；队列满不阻塞请求；SIGTERM 时在宽限期内排空已入队记录。
当前代码已在 HTTP 服务停止后调用 `audit.Logger.Close`，并由组件测试证明已入队记录可排空、关闭后
不再接收新记录；当前候选在隔离 Release 滚动重启前产生的 20 条唯一 requestId 均到达接收端，
未出现 `audit_flush_failed`，该项在实验室范围标记为已验收。生产或预发同等拓扑仍需复跑。
完整逐步骤设计、隔离 Release fixture、故障注入和证据格式见 22.14；普通 NetworkPolicy、Cilium
Webhook 出口和真实接收端必须在同一候选上联合验收，不能只执行本节的组件测试。

**CI 验收**

```bash
gh run list --workflow=ci.yml --limit=5
gh run view <run-id> --log
```

通过标准：`backend`、`frontend`、`e2e`、`performance`、`helm`、`security` 全部成功，
`release-summary` 成功，artifact 包含 Playwright、性能、Trivy 和 Helm 渲染证据。仅本地
YAML 解析通过不算 CI 验收。

### 15.10 OIDC 与 Kubernetes RBAC 验收

**已实现部分的自动化门禁**

```bash
gofmt -w cmd internal web/embed.go
go test -race ./...
go test -race ./internal/modules/auth/... ./internal/platform/authz ./internal/platform/kube -count=1
go test ./internal/platform/config -count=1 -v
make arch
make api-check
cd web && npm run typecheck && npm test && npm run build
```

通过标准：domain/app 无 OIDC/client-go/net/http/chi 依赖；OIDC state、nonce、PKCE、TTL、开放重定向、
impersonation header、SAR 调用和配置负向测试全部通过；OpenAPI 生成类型无漂移。

**Helm 双模式门禁（已实现，已通过本地渲染）**

```bash
make helm-check
```

通过标准：`local` 和 `oidc` 均可渲染；OIDC client secret 只来自 Secret；OIDC ServiceAccount 不含业务资源
写权限；NodePort/容器端口仍为 32626；namespace 仍为 `dosh`；OIDC 缺 IdP CIDR、HTTP issuer
和非法模式均被模板拒绝。Kubeconform 与真实 API Server dry-run 仍属于部署验收，不以 helm template 代替。

**可复现的签名 IdP 集群夹具（已实现）**

夹具入口：`scripts/e2e-oidc.sh`；测试 IdP：`tools/oidctestidp`。脚本执行以下动作：

1. 构建并推送候选镜像，生成短期 CA、IdP TLS 证书和 dosh TLS 证书。
2. 在主机启动真实 RSA 签名的 HTTPS 测试 IdP，提供 discovery、JWKS、authorize、token、health 和
   RP logout endpoint；签发 viewer、operator、admin 三个身份。
3. 创建隔离 namespace `dosh-e2e-oidc`、目标 namespace `dosh-e2e-target`、TLS/Secret/CA ConfigMap、
   viewer/operator/admin RBAC 和目标 Deployment。
4. 以 Helm Release `dosh-oidc-e2e` 安装到隔离 namespace，使用 NodePort `32627`；脚本先断言该端口
   不等于生产 `dosh` 的 `32626`，且生产 Release 不被修改。
5. 通过签名 IdP 完成三个用户的 Authorization Code + PKCE 登录，验证 session、capabilities、直接 API
   读写矩阵、审计用户/组归属和 RP-initiated logout。
6. 脚本退出时停止测试 IdP，但保留 namespace、RBAC 和 Release 供复核；重新执行会重建 IdP 和夹具。

```bash
export PATH=/root/.local/bin:$PATH
bash scripts/e2e-oidc.sh
```

前置条件：可用的 `kubectl`、`helm`、`podman`、`curl`、`openssl`、`jq`、`go`、`make`；测试端口
`32627` 未被占用；集群可从 `192.168.122.10:5000` 拉取镜像。脚本中的 `podman push --tls-verify=false`
只适用于当前自签实验室仓库，生产流水线不得照搬。

**签名测试 IdP 的已通过结果（2026-11-02 历史基线；当前候选见 21.2.12）**

| 验收项 | 原始事实 | 结论 |
|---|---|---|
| 隔离部署 | Release `dosh-oidc-e2e`，namespace `dosh-e2e-oidc`，revision 2 的 API 基线；最新浏览器/回滚证据已到 revision 14，见 21.2.5；生产 `dosh`/`32626` 未改动 | 通过 |
| 镜像可追溯 | `192.168.122.10:5000/dosh/dosh:0.1.3-oidc-e2e-1793621036`；Pod imageID `sha256:c8fda6d1d8cc5ddc22e4a60ca367f846149c1643b24658716e93bc82313ea839` | 通过 |
| viewer | 目标 namespace 读 200；scale 403；`revealSecrets=false`、`scale=false` | 通过 |
| operator | 目标 namespace scale 200；`default` namespace scale 403；Secret reveal 按 RBAC 拒绝 | 通过 |
| admin | 目标 namespace scale 200；nodes 200；capabilities 全开 | 通过 |
| 审计 | 三个用户名和 `dosh-e2e-users` 等组均出现；viewer/越界 operator 的 `result` 为 `denied` | 通过 |
| RP logout | 最终 URL 为 `https://192.168.122.100:32627/login`；随后 `/api/v1/session` 为 401 | 通过 |
| dosh ServiceAccount 最小权限 | impersonate users/groups=yes；create subjectaccessreviews=yes；list pods、patch deployments、get secrets、delete nodes=no | 通过 |

最小权限复核命令如下；`kubectl auth can-i` 对 cluster-scoped 资源打印的 “not namespace scoped”
仅为提示，不是失败：

```bash
sa=system:serviceaccount:dosh-e2e-oidc:dosh-oidc-e2e
kubectl auth can-i impersonate users --as="$sa"
kubectl auth can-i impersonate groups --as="$sa"
kubectl auth can-i create subjectaccessreviews.authorization.k8s.io --as="$sa"
kubectl auth can-i list pods --as="$sa" -n dosh-e2e-target
kubectl auth can-i patch deployments.apps --as="$sa" -n dosh-e2e-target
kubectl auth can-i get secrets --as="$sa" -n dosh-e2e-target
kubectl auth can-i delete nodes --as="$sa"
```

**浏览器验收（已执行，实验室已验收）**

| 身份 | 读取 | 扩缩容/重启/删除 | Secret reveal | YAML apply | 期望 |
|---|---|---|---|---|---|
| `viewer` | 授权命名空间成功 | 直接 API 和 UI 均 403 | 403，UI 显示“无权限” | 403 | 不产生写请求 |
| `operator` | 授权命名空间成功 | `dosh-e2e-target` 成功、其他命名空间 403 | 按 RBAC | 按 RBAC | 审计记录真实用户/组 |
| `admin` | 全量成功 | 全量成功 | 按 RBAC | 按 RBAC | 与 kubectl 结果一致 |

浏览器 E2E 已验证真实页面上的 OIDC 登录、RP logout、Cookie 属性、401 回跳以及按钮隐藏与 API
拒绝一致。v2.45 当前候选 Chromium/Firefox/WebKit 各 16/16 通过（含七类负向登录和 host 12/12、
隔离 WebKit 6/6 denied 审计），Token 扫描均为 0 命中；v2.31 历史 digest 的隔离 rollback/restore
为 revision `128→129→130→131`，历史 Release 曾复验到 revision 138，不继承给 v2.45：

```bash
DOSH_E2E_KEEP_ARTIFACTS=1 DOSH_E2E_SKIP_BUILD=1 \
DOSH_E2E_IMAGE_TAG=<final-candidate-tag> \
  bash scripts/e2e-oidc-browser.sh

DOSH_E2E_KEEP_ARTIFACTS=1 DOSH_E2E_SKIP_BUILD=1 \
DOSH_E2E_IMAGE_TAG=<final-candidate-tag> \
  bash scripts/e2e-oidc-rollback.sh
```

仍未覆盖：企业 IdP 的 claims/私有 CA/JWKS 轮换、state 篡改和 nonce mismatch 的浏览器级注入、
IdP 短暂不可达、SAR 503 fail-closed 真实故障注入、受信 TLS 和 digest 生产发布。因此 OIDC 只能
标记为隔离环境“实验室已验收”，不能整体标记为生产已验收。

**测试资源清理（保留证据后执行）**

```bash
helm -n dosh-e2e-oidc uninstall dosh-oidc-e2e
kubectl delete namespace dosh-e2e-oidc dosh-e2e-target
kubectl delete clusterrole dosh-e2e-viewer dosh-oidc-e2e --ignore-not-found
kubectl delete clusterrolebinding dosh-e2e-viewers dosh-e2e-admins dosh-oidc-e2e --ignore-not-found
```

清理命令不得包含生产 namespace `dosh`，不得包含生产 Release `dosh`，也不得修改 NodePort `32626`。

## 16. 需求追踪矩阵

| 阶段 | 核心需求 | 验收证据 |
|---|---|---|
| 阶段 0 | ARC-*、SYS-U-001、OPS-U-003 | CI、架构 lint、Helm template |
| 阶段 0 增量：chi 路由边界 | ARC-U-007、ARC-U-011、ARC-U-012、ARC-E-001、ARC-N-003 | `TestRouteInventoryMatchesOpenAPI`、`TestModuleRoutesAreScoped`、禁止通配 proxy 的静态检查 |
| 阶段 0 增量：模块解耦 | ARC-U-003、ARC-U-013、ARC-N-001 | `make arch`；生产模块不导入其它模块 HTTP；Actor 只经 `domain.ActorFromContext` 传播 |
| 阶段 1 | SYS-*、UX-U-001 至 UX-U-010 | 健康检查、浏览器壳、bundle 报告、动态集群上下文测试 |
| 增量：动态集群上下文 | UX-U-002、UX-U-010 | MSW 非实验室数据渲染、503 降级、隔离 Release 切换 `DOSH_CLUSTER_NAME` 后不刷新硬编码值 |
| 阶段 2 | AUTH-*、AZ-*、SEC-U-002/004 | 登录、Cookie、限速、审计测试 |
| 阶段 3 | OVW-*、NODE-*、WL-U-*、POD-U-* | 真实集群对账、单元测试、E2E |
| 阶段 4 | LOG-*、EVT-*、CFG-*、SCR-*、YML-U-* | SSE/日志测试、Secret 脱敏测试 |
| 阶段 5 | WL-E-*、WL-N-*、YML-O-*、YML-N-* | `make e2e-workload-lifecycle` 在独立命名空间完成预览、创建、扩容、镜像更新、重启、删除、409、RBAC、审计与 Pod UID 对账；证据见 21.2.36 |
| 阶段 6 | NET-*、STO-*、OVW-N-001、AUTH-O-* | 当前集群空状态、Metrics 双态测试 |
| 阶段 7 | OPS-*、SEC-*、NFR-*、TST-* | Helm 部署、加固检查、回滚、验收记录 |
| 阶段 8 | AUTH-O-*、AZ-O-*、SEC-O-003 至 SEC-O-005、TST-U-013 至 TST-U-016 | v2.45 当前候选的 OIDC 负向测试、SAR 断言、Helm 双路径、三身份 API/RBAC、mock 各 38/38、真实 OIDC 各 16/16 和全量本地门禁已通过；v2.44 历史 digest 的 30 分钟 soak 与 revision `181→184` 回滚/restore 不继承给 v2.45；企业 IdP、Edge/Safari 实机、CI 真跑和生产发布待补 |
| 增量：Metrics | OVW-U-004、NODE-U-005、POD-U-003、TST-U-009 | fake Metrics 测试、真实 Metrics Server 有/无/恢复三态对账、降级文案 |
| 增量：429/5xx 重试 | NFR-E-001 | `retry_test.go` 的幂等/写请求/次数上限/取消/Retry-After 测试；v2.44 历史 digest 完成全矩阵，v2.45 未复跑，证据见 21.2.38 |
| 增量：Deployment 创建向导 | WL-E-004/005、WL-N-003/004、UX-N-001 | domain/app/kube/http 测试；API Server Dry Run；v2.45 当前候选的 mock 各 38/38、真实 OIDC 各 16/16 均包含向导创建；v2.44 历史隔离 API 生命周期对账不继承 |
| 增量：工作负载审计与 adapter | WL-N-005/006、SEC-U-004、TST-U-002/003 | workload app 失败审计、只读 HTTP adapter 和 CronJob 无 selector 测试；v2.44 历史候选已在隔离 Release 通过，v2.45 未重跑专用矩阵，见 21.2.38 |
| 增量：URL 状态 | UX-U-008、TST-U-010 | 路由解析测试、刷新/分享/前进后退 E2E |
| 增量：审计投递 | SEC-U-006/007、OPS-U-013、NFR-E-003/004、TST-U-011/012；实施级 AUDIT-W-* | 22.14：v2.45 当前 digest 通过隔离 Audit/Reveal 的私 CA、Bearer/HMAC、Cilium 出口、业务事件矩阵、故障/恢复、队列、只读拒绝、20/20 SIGTERM 排空和泄露扫描 0；真实 Loki 与真实 SIEM 未在 v2.45 复跑 |
| 增量：CI | TST-U-007/008、OPS-E-001 | GitHub Actions 实跑结果和 artifact |
| 增量：OIDC | AUTH-O-001 至 AUTH-O-006、AUTH-N-004、AZ-O-001/002、AZ-N-003 | 组件测试、真实 RSA 签名测试、HTTP 全链路、Helm 双模式渲染、v2.45 当前候选的三身份 API/RBAC、mock 各 38/38、真实 OIDC 各 16/16、host 12/12 与 WebKit 6/6 denied 审计和 Token 扫描通过；v2.44 历史 rollback/restore 不继承；企业 IdP、Safari/Edge 实机和生产发布未验收 |

## 17. Definition of Done

一个阶段只有同时满足以下条件才算完成：

- 该阶段全部 P0 需求已实现。
- 对应单元、契约和 E2E 测试通过。
- 架构 lint、类型检查、静态检查和构建无阻断错误。
- 文档、OpenAPI 和 Helm values 已同步更新。
- chi 路由清单与 OpenAPI 双向一致，新增资源路由有唯一模块 owner 和 HTTP 契约测试。
- 关键操作具有结构化日志和审计记录。
- 没有新增未登记的全局依赖或跨模块直接调用。
- 已获得至少一名评审人的验收记录。

一个需求只有同时满足以下条件才算“已验收”：

- 有需求编号对应的实现位置说明。
- 有自动化测试或明确的人工验收记录。
- 验收证据包含命令、输出、截图或报告路径，并绑定候选镜像 digest、Helm revision、namespace 和执行时间。
- 已知限制已写入风险登记表。

## 18. 风险与待决事项

| ID | 风险或决策 | 当前建议 | 状态 |
|---|---|---|---|
| RISK-001 | 本地管理员模式使用高权限 ServiceAccount | 首版仅用于受控内网；OIDC 最小 RBAC 的 Helm 渲染和隔离集群三身份矩阵已通过，生产和企业 IdP 仍未切换 | 部分缓解 |
| RISK-002 | 审计持久化 | 已实现 stdout + 可选 Webhook（Bearer/HMAC/私 CA/重试/有界队列/关闭排空）；CNP 出口和隔离 Release 集成通过，v2.33 真实 Loki 实验室完成写入与回查；企业 SIEM 的持久化、告警、访问审批、保留和 WORM 仍待完成 | 部分缓解 |
| RISK-003 | 无 Ingress，只能通过 NodePort 访问 | 首版使用 32626 NodePort，后续再评估 Ingress | 已接受 |
| RISK-004 | 目标生产版本尚未在最终拓扑复验 Metrics | Metrics Server 已部署，隔离测试 Release 三态通过；发布前按 15.9/26.3 复跑并归档 | 实验室已验收，生产复验待完成 |
| RISK-005 | Secret 数据在本地管理员模式下由后端 SA 读取 | 默认脱敏、reveal 审计；OIDC 模式 app 层已按 `get secrets` 授权，仍需真实集群验证 | 部分缓解 |
| RISK-006 | 自签 TLS 需要客户端信任 | 首版可接受浏览器告警，正式部署替换受信证书 | 待确认 |
| RISK-007 | 2 vCPU / 4 GiB 节点资源有限 | 2 副本低 requests，禁止部署重量级监控组件 | 已接受 |
| RISK-008 | CCE 风格不等同于像素级复刻 | 采用其信息架构和密度，视觉 token 自建 | 已确认 |
| RISK-009 | 商用多用户身份是否必须 | 已提升为生产试点 P1 能力；未验收前禁止对外多用户暴露 | 已确认 |
| RISK-010 | 是否允许写操作 | 先实现只读验收，再启用受控写操作 | 待确认 |
| RISK-011 | 审计队列满或进程异常退出可能丢失记录 | v2.45 当前 digest 已通过隔离 Audit/Reveal 的队列压力和 20/20 SIGTERM 排空；v2.44 历史 Loki 回查 20/20 命中但不继承。生产仍需监控 `audit_webhook_queue_full`/`delivery_failed`，异常崩溃仍需企业持久化 SIEM/不可变存储兜底 | 部分缓解 |
| RISK-012 | chi 路由虽已分层，但仅靠 grep 容易被绕过 | 保持模块 `Mount` 和消费者端口；后续引入 `go-arch-lint`/`depguard` 显式规则 | 监控中 |
| RISK-013 | 测试与部署版本漂移 | 候选版本必须同步 Chart、values、OpenAPI、前端 package、README 和验收记录；发布前执行版本一致性脚本 | 待关闭 |
| RISK-014 | CI 文件存在但从未真实运行，工具版本/缓存/权限可能失败 | 将其作为发布 Blocker；首次经 GitHub Actions 绿灯后才可取消人工替代门禁 | 待关闭 |
| RISK-015 | OIDC 缺少企业 IdP 和生产发布证据 | v2.45 当前候选已完成签名 IdP 的 HTTP 全链路、Helm 双模式、隔离集群三身份 API/RBAC、Chromium/Firefox/WebKit E2E、Token 扫描和 denied 审计；v2.44 历史 rollback/restore 不继承。剩余企业 IdP、Edge/Safari 实机和 digest 生产发布，未完成前保持 `local` | 部分缓解 |
| RISK-016 | Kubernetes 授权若使用 `SelfSubjectAccessReview`，会评估错误主体 | 已改为携带 actor user/groups 的 SAR，并加 `domain.WithoutActor` 去除 impersonation 头；组件、隔离集群 API 和浏览器矩阵通过 | 实验室已验收；企业 IdP 待验 |
| RISK-017 | OIDC issuer 出口与最小 RBAC 若实现不当会破坏登录或扩大权限 | Helm 已用显式 CIDR/FQDN 和 impersonate + SAR + discovery 最小 RBAC 渲染断言；隔离集群登录和三身份矩阵已通过，生产出口策略待验证 | 部分缓解 |
| RISK-018 | Kubernetes 429/5xx 重试可能放大写操作或形成重试风暴 | 已限制为幂等读请求、最多 3 次、尊重 `Retry-After`/context；v2.44 历史 digest 完成瞬时/持续故障、写请求只尝试 1 次、连接重置和客户端取消全矩阵，v2.45 未复跑 | 部分缓解；v2.45 与生产故障注入待复验 |
| RISK-019 | 当前候选与前一候选证据混用造成“同源码即同镜像”误判 | 当前发布身份是 `sha256:c7eff34a…3ab06`；源码绑定、镜像二进制、mock/真实 OIDC、Audit/Reveal、容量、性能和安全扫描绑定该 digest；v2.44 `sha256:e4a27d8b…49439` 的历史 rollback/Metrics/retry/Loki/soak 不继承 | v2.45 内部证据链已关闭；v2.45 专项复跑、WebKit 独立 runner 和离机 WORM 仍待完成 |
| RISK-020 | soak 期间测试 IdP 会话过期被误判为应用故障 | 脚本已在每次流重建前重新登录；首个 605 秒退出只作为夹具失败记录，不提升或降低产品状态；最终复跑必须保留原始指标 | 部分缓解 |
| RISK-021 | 源码修复后仍把旧候选 digest 的浏览器、soak、回滚或归档证据当作当前源码证据 | v2.44 `sha256:e4a27d8b…49439` 已标记为历史；v2.45 `sha256:c7eff34a…3ab06` 完成源码绑定、隔离部署、三浏览器、Audit/Reveal、容量、性能和安全扫描，但未复跑 rollback/Metrics/retry/Loki/soak | v2.45 内部证据链已关闭；专项复跑、生产发布和外部合规门禁仍独立 |

## 19. 发布与回滚约束

- 发布包必须包含镜像 tag、Git SHA、Helm Chart 版本和 OpenAPI 版本。
- 发布前必须保存上一版本 Helm revision。
- 任一 P0 安全需求失败时禁止发布。
- 回滚不得修改 `dosh` 命名空间外资源。
- 卸载时默认删除 Helm 管理的资源，不删除用户创建的测试命名空间。
- 证书、Session Key 和密码哈希不写入镜像、Git 仓库或日志。
- 所有生产配置通过 Secret 或受控 values 文件注入。

## 20. 评审入口

以下设计边界已在本轮实施中作出默认决策，后续变更需更新本节和风险登记表：

1. 当前已部署版本保持本地单管理员模式；生产多用户升级到 OIDC + Kubernetes RBAC，属于阶段 8 的 P1 放行项。
2. 首版在只读功能通过后启用扩容、重启、删除和受控 YAML apply；每次写操作都有 CSRF、后端授权和审计。
3. Metrics Server 作为独立运维依赖安装在 `dosh-metrics`，不由 `dosh` Helm Release 管理；页面必须支持真实 usage 和 Metrics 缺失时的 requests 降级，不得伪造 0%。
4. 当前实验环境使用自签 TLS；生产部署必须替换为受信证书。
5. 删除要求输入资源名确认；Secret 明文要求输入 Secret 名确认；扩容和重启使用确认弹窗。
6. 列表筛选状态以 URL 为唯一可分享来源，页面刷新和浏览器历史必须恢复 `q`、`namespace`、`kind`。
7. 审计以 stdout 为必选基线；Webhook 是可选旁路，启用时必须通过 Secret 注入凭据并限制出口网段。
8. chi 仅做 transport；若新增路由导致 Handler 出现业务分支或跨模块 import，应停止实现并先调整端口设计。

## 21. 实施与验收记录

本节分别记录已部署基线和尚未发布的候选改动。候选改动不得覆盖基线证据；发布后新增
“候选版本发布记录”，并保持历史版本可追溯。

### 21.1 交付物

| 交付物 | 路径 | 状态 |
|---|---|---|
| Go BFF 与模块化后端 | `cmd/`、`internal/` | 已实现 |
| React / TypeScript / shadcn 风格 UI | `web/src/` | 已实现 |
| OpenAPI 3.1 契约 | `api/openapi.yaml` | 已实现并生成 TS 类型 |
| 构建与本地门禁 | `Makefile`、`Dockerfile` | 已实现 |
| Helm Chart | `deploy/helm/dosh/` | 生产 `0.1.4` 已部署（revision 12）；当前源码 Chart `0.1.6` 已在多个隔离 Release 以 digest 验证 OIDC、审计、CNP、32626/2 副本和 rollback，生产发布待执行 |
| EARS 文档 | `docs/ears-implementation-plan.md` | 本文件 |
| 运行与安全手册 | `docs/operations-runbook.md` | 已实现 |
| 前端集成测试 | `web/src/app.integration.test.tsx` | v2.45 当前候选 Vitest `62/62` 通过；覆盖登录、总览、节点、工作负载/Pod 日志、OIDC logout、动态集群上下文、总览面包屑、主题、表格可访问性和 overview 降级 |
| Playwright E2E | `web/e2e/dosh.spec.ts`、`web/e2e-local/dynamic-context.spec.ts`、`web/e2e-oidc/oidc.spec.ts`、`web/e2e-oidc/ux.spec.ts`、`scripts/run-playwright-container.sh` | v2.45 当前候选 mock Chromium/Firefox/WebKit 各 38/38、真实 OIDC 各 16/16，均使用受限 runner；宿主级 flock 禁止重叠；WebKit 仅在隔离 libvirt runner 执行 |
| 生产首屏性能 | `web/e2e/performance/lcp.spec.ts`、`web/scripts/check-bundle.mjs` | v2.45 当前候选 LCP `732.0 ms`；初始 JS gzip `350.62 KiB` |
| 容量验收 | `internal/performance/capacity_test.go` | 1000 Pod/200 工作负载，P95 与内存门禁通过 |
| 镜像扫描 | `Makefile#image-scan` | Trivy 固定 digest；HIGH/CRITICAL 0 |
| 后端单元测试 | `internal/**/**_test.go` | app、HTTP handler、Kubernetes fake-client adapter 用例通过；当前行覆盖：domain `100%`、workloads app `99.4%`、kube `78.4%`，nodes/overview/network/storage HTTP 均为 `100%` |
| 工作负载失败审计与 CronJob 详情 | `internal/modules/workloads/app/service.go`、`service_test.go`、`internal/platform/kube/workloads.go`、`workloads_test.go` | 无效操作/非法删除策略均审计；无 selector CronJob 不 panic；当前隔离 Release 失败审计和 CronJob API 抽样均已通过 |
| Deployment 创建向导 | `internal/domain/deployment.go`、`internal/modules/workloads/app/deployment.go`、`internal/platform/kube/deployment_create.go`、`web/src/features/workloads/deployment-create-dialog.tsx` | domain/app/kube/handler 测试通过；当前候选 API 与浏览器矩阵覆盖 Dry Run→Create、YAML preview、403/400/409、审计和 kubectl 对账 |
| Metrics 适配 | `internal/platform/kube/metrics*.go`、`internal/domain/types.go`、`scripts/install-metrics-server.sh`、`scripts/e2e-metrics.sh` | 已实现 Node/Pod/Detail 实际用量和 `hasMetrics` 降级；v2.44 历史 digest 在隔离 Release 完成可用/禁用/恢复三态，证据见 21.2.38；v2.45 未复跑 |
| Kubernetes 有界重试 | `internal/platform/kube/retry.go`、`retry_test.go`、`client.go`、`scripts/e2e-kube-retry.sh` | 仅幂等读请求重试，最多 3 次，尊重 `Retry-After`/context；v2.44 历史 digest 完成完整故障矩阵，证据见 21.2.38；v2.45 未复跑 |
| 列表 URL 状态 | `web/src/lib/list-search.ts`、`web/src/router.tsx`、各列表页 | 已实现 `q/namespace/kind` 持久化；v2.45 当前候选 mock Chromium/Firefox/WebKit 各 38/38、真实 OIDC 各 16/16 覆盖刷新、分享、前进/后退 |
| 审计投递 | `internal/platform/audit/audit.go`、`audit_test.go` | v2.45 当前候选通过隔离 Audit/Reveal 的私 CA、Bearer/HMAC、业务事件、瞬时/持续 503、超时恢复、队列满、只读拒绝和 20/20 SIGTERM 排空；真实 Loki 与真实 SIEM 未在 v2.45 复跑 |
| OIDC 多用户身份 | `internal/modules/auth/app/oidc.go`、`internal/platform/oidc/provider.go`、`internal/platform/authz`、`internal/platform/kube/{authorization,impersonation}.go`、`deploy/helm/dosh/`、`tools/oidctestidp/`、`scripts/e2e-oidc.sh`、`scripts/e2e-oidc-browser.sh`、`scripts/e2e-oidc-rollback.sh`、`web/e2e-oidc/oidc.spec.ts` | v2.45 当前 digest 通过 API/RBAC、Chromium/Firefox/WebKit 各 16/16、host 12/12 与 WebKit 6/6 denied 审计和 Token 扫描；rollback/restore `181→184` 是 v2.44 历史证据，v2.45 未复跑；企业 IdP 和生产发布未验收 |
| OIDC 浏览器 E2E | `web/playwright.oidc.config.ts`、`web/e2e-oidc/oidc.spec.ts` | 无 API mock，连接隔离集群；v2.45 当前 digest Chromium/Firefox/WebKit 各 16/16 通过；WebKit 在隔离 libvirt runner 执行 |
| OIDC 负向矩阵 | `web/e2e-oidc/oidc.spec.ts`、`tools/oidctestidp/main.go`、`internal/platform/config/config.go`、`scripts/e2e-oidc-browser.sh` | v2.45 当前候选 Chromium/Firefox/WebKit 各 16/16 覆盖七类负向场景；`DOSH_OIDC_STATE_TTL` 严格校验；host 两浏览器 12/12、隔离 WebKit 6/6 denied 审计带 `requestId` |
| Token 泄露扫描 | `scripts/e2e-oidc-browser.sh` | 新 digest 的浏览器响应/URL/DOM/Web Storage/Cookie、应用日志、ConfigMap/Deployment、Helm values 均无命中 |
| OIDC 回滚演练 | `scripts/e2e-oidc-rollback.sh` | v2.44 历史 digest 在隔离 Release 通过 revision `181→182→183→184`，每个阶段均复验健康、三身份和 logout；v2.45 未复跑，生产发布与回滚仍未执行 |
| 证据归档工具 | `scripts/archive-evidence.sh`、`scripts/test-archive-evidence.sh` | 已修复摘要覆盖顺序；正向/负向回归通过；v2.45 当前候选 33 文件归档 manifest `e71c466b…64c6` 已通过双层校验；历史归档不替代当前 digest |
| CI 工作流 | `.github/workflows/ci.yml` | 8 个 job 已定义，含独立 policy job；尚未在 GitHub Actions 实跑 |

### 21.2 自动化证据

以下 21.2.1–21.2.11 保留历史门禁与前一候选证据，不得用于当前发布判断。当前 OIDC 候选
`0.1.3-rc-oidc-20261102171324@sha256:0be6df25…798ca8` 的 OIDC 证据见 21.2.12；前一完整门禁候选
`0.1.3-rc-20261102153511@sha256:47983a…312e` 的 soak、审计和 rollback 证据见 21.2.10。历史章节
中的旧 digest、旧 revision 和“待复跑”状态只用于审计轨迹。

| 门禁 | 命令 | 结果 |
|---|---|---|
| Go 格式 | `gofmt -l cmd internal web/embed.go` | 无输出 |
| Go 静态检查 | `go vet ./...` | 通过 |
| Go 测试与竞态 | `go test -race ./...` | 通过；认证、配置、工作负载用例均通过 |
| 前端严格类型检查 | `npm run typecheck` | 通过 |
| 前端集成测试 | `npm test` | 5/5 通过：登录、总览、节点、工作负载/Pod 日志、OIDC logout |
| 前端生产构建 | `npm run build` | 通过；最大异步 chunk 为 editor 144.31 KiB gzip |
| 跨浏览器 E2E | `npm run e2e:container` | 18/18 通过：6 条主路径 × Chromium/Firefox/WebKit；主配置排除 `performance/**`，性能套件独立执行 |
| 容量与 P95 | `make capacity-test` | 1000 Pod/200 工作负载：Pod 列表 P95 21 ms、工作负载 P95 185 ms、总览 P95 199 ms |
| 容量内存 | `make capacity-test` | 请求后 Go heap 28 MiB；低于 256 MiB 门禁 |
| 生产 LCP | `npm run e2e:performance` | 生产构建首屏 LCP 572 ms；低于 2.5 s |
| 初始 JS 预算 | `npm run bundle:check` | 初始 JS gzip 334.54 KiB；低于 500 KiB |
| 架构边界 | `make arch` | 通过；domain/app 无 `k8s.io`、`net/http`、`chi` |
| 危险 HTML 注入检查 | `rg 'dangerouslySetInnerHTML' web/src`、`rg 'innerHTML\s*=' web/src` | 无匹配 |
| OpenAPI 生成 | `npm run api:generate` | 截至 v2.4 为 33 paths / 43 schemas；生成成功且 `make api-check` 无漂移 |
| OpenAPI 漂移检查 | `make api-check` | 临时生成物与 `web/src/api/schema.d.ts` 逐字节一致 |
| Go 漏洞扫描 | `govulncheck ./...` | 调用链 0 漏洞；3 个依赖模块存在未被调用的漏洞公告 |
| 前端生产依赖审计 | `npm audit --omit=dev --audit-level=high` | 生产依赖 `found 0 vulnerabilities`；完整审计另有 3 个 dev-only moderate，来自 `openapi-typescript` 工具链，见 21.4 |
| Helm lint | `helm lint deploy/helm/dosh` | 通过（仅提示未配置 icon） |
| Helm 渲染（非 Cilium） | `helm template ... -n dosh` | NodePort/容器端口 32626、2 副本、PDB、安全上下文、标准 NetworkPolicy 和 `0.1.1` 镜像断言通过 |
| Helm 渲染（Cilium） | `helm template ... -n dosh --api-versions cilium.io/v2` | 自动生成 CNP；出口使用 `toEntities: [kube-apiserver]`，标准 NetworkPolicy 不再包含 `0.0.0.0/0` API 出口 |
| Helm 策略覆盖开关 | `--set networkPolicy.ciliumApiServer.autoDetect=false` / `enabled=true` | 自动探测关闭时 CNP=0 且保留标准 API 出口；强制开启时 CNP=1 且不生成宽泛 API 出口 |
| 镜像构建 | `make image TAG=0.1.1` | 构建并推送成功；registry digest 为 `sha256:a326a48a30195f29c8a5681f7d11d90cdc131e1b937210cd4556f3703080e5a0` |
| 镜像漏洞扫描 | `make image-scan TAG=0.1.1` | Trivy 0.76.0；Debian 层 0、Go 二进制 0 个 HIGH/CRITICAL |

Playwright 用例覆盖：登录后进入总览、节点列表进入详情、工作负载详情扩容并校验
`X-CSRF-Token` 与 `resourceVersion`、Pod 日志流、未认证跳转登录、390 px 移动端导航。
WebKit 在官方容器中执行，证明页面主路径不依赖 Chromium 特有行为；它不等同于对真实
Safari 版本的兼容性认证，发布记录必须保留容器镜像 digest 和测试报告。

说明：首次执行漏洞扫描时，Go 官方代理和当前 npm 镜像的 audit 接口不可用；改用
`GOPROXY=https://goproxy.cn,direct` 与 `--registry=https://registry.npmjs.org` 后完成扫描。
Trivy 镜像为 `ghcr.io/aquasecurity/trivy@sha256:2e6c00d711ab4ae41d8a30c2c1fd3bd3b62704c177bb120f95012e939edfb3df`，
数据库通过 `ghcr.io/aquasecurity/trivy-db:2` 获取，避免公网 Docker Hub 不可用导致结果漂移。

### 21.2.1 候选改动验证快照（未发布）

以下是工作树相对 `0.1.1/0.1.3` 已产生的新改动。它们只代表“已实现”或“局部测试通过”，
不改变第 21.3 节的集群已部署事实。

| 项目 | 已完成 | 已通过 | 尚未完成 |
|---|---|---|---|
| Metrics 实际利用率 | `k8s.io/metrics v0.36.5`、Metrics adapter、Cluster/Node/Pod usage DTO、OpenAPI/TS 类型、总览/节点/Pod 页面 | fake Metrics client 测试、真实 Metrics Server 三态对账、全量门禁和前端 typecheck | 目标发布版本的浏览器回归和生产/预发证据 |
| 429/5xx 重试 | `retryTransport` 统一包装 client-go；只读请求有限重试，写请求不重放 | `go test -race` 定向 8/8；全量 race 待候选版本复跑 | 真实故障注入、页面错误提示和重试风暴观测 |
| 列表 URL 持久化 | `list-search.ts`、6 个列表路由 `validateSearch`、页面 `useSearch/navigate`、URL E2E | 三浏览器 E2E 已覆盖 URL 更新 | 当前 mock 主套件 42/42 通过；隔离 Release 刷新/分享/前进后退人工验收待补 |
| 审计 Webhook | stdout 保留、Bearer、HMAC、Request ID、3 次重试、有界队列、`Flush`/`Close`、`bootstrap.App` 关停排空、Helm values/Secret/出口策略 | `go test -race ./internal/platform/audit ./internal/platform/config`；Helm 手工渲染默认关闭和启用注入 | 真实滚动升级 SIGTERM 证据、最终 Helm 三路径 lint/kubeconform、真实 SIEM 投递 |
| CI | `.github/workflows/ci.yml`，8 个 job 和固定工具版本 | actionlint 与本地 YAML 解析通过 | 真实 GitHub Actions 首次绿灯、artifact 留存、分支保护 |

候选发布的最低证据是：`go test -race ./...`、`make arch`、`make api-check`、前端
typecheck/test/build/bundle、45/45 mock E2E 与 33/33 OIDC E2E、性能、容量、Helm 双路径 lint、镜像扫描全部通过；
缺少任一项时只能继续标记为“候选实现”，不得执行版本冻结。

### 21.2.2 OIDC 候选历史门禁快照（2026-11-02）

本节保留 OIDC 阶段当时的门禁快照，不表示最终候选 `0.1.3-rc-1793626263` 已通过。组件测试中的
`authMode`/`/api/v1/auth/config` mock 只证明契约和 UI 分支，不能替代浏览器真实 IdP 验收；最终
候选状态以 21.2.7 和第 26 节为准。

| 门禁 | 命令 | 当前结果 | 结论 |
|---|---|---|---|
| Go 格式 | `gofmt -w cmd internal tools web/embed.go` | 已执行 | 通过 |
| Go 静态检查 | `go vet ./...` | 通过 | 通过 |
| Go 全量竞态 | `go test -race ./...` | 全部包通过；含 auth app/http、config、kube、workloads、yaml、bootstrap | 通过 |
| 架构边界 | `make arch` | 通过 | 通过 |
| OpenAPI 漂移 | `make api-check` | 生成物与 `web/src/api/schema.d.ts` 一致 | 通过 |
| Helm lint/渲染 | `make helm-check && bash scripts/check-helm.sh` | local、local+Cilium、oidc、oidc+Cilium 通过；缺 IdP CIDR、HTTP issuer、非法模式负向测试通过；端口 32626、2 副本、最小 OIDC RBAC 和 Deployment 文档头断言通过 | 通过 |
| Shell 语法 | `bash -n scripts/e2e-oidc.sh scripts/e2e-oidc-browser.sh scripts/e2e-oidc-rollback.sh` | 通过 | 通过 |
| 前端类型 | `npm run typecheck` | 通过 | 通过 |
| 前端单测 | `npm test -- --run` | 5/5 通过；含 OIDC logout 用户菜单用例 | 通过 |
| 前端构建与预算 | `npm run build && npm run bundle:check` | 通过；初始 JS 335.66 KiB gzip，低于 500 KiB | 通过 |
| OIDC app 测试 | `go test -race ./internal/modules/auth/...` | 合法/非法 state、nonce、开放重定向、logout 绝对回跳等用例通过 | 通过 |
| OIDC provider 测试 | `go test -race ./internal/platform/oidc -count=1 -v` | discovery/JWKS/token exchange、S256 PKCE、real RSA、iss/aud/exp/nonce、错误签名、email 未验证、JWKS 轮换、RP logout endpoint 通过 | 通过 |
| HTTP 全链路 | `go test -race ./internal/modules/auth/http -run TestRealProviderHTTPFlow -count=1 -v` | 本地签名 IdP 的 login→authorize→callback→session→capabilities→logout 通过 | 通过 |
| SAR 目标模型 | `go test -race ./internal/platform/kube -count=1` | `SubjectAccessReview` 的 user/groups/ResourceAttributes、无 impersonation header、无 Actor fail-closed 通过 | 通过 |
| 隔离集群三身份矩阵 | `bash scripts/e2e-oidc.sh` | viewer/operator/admin 的 API 读写、capabilities、审计和 RP logout 通过；证据见 21.2.3 | 通过（API/测试 IdP） |
| 最小 ServiceAccount 权限 | `kubectl auth can-i ... --as=system:serviceaccount:dosh-e2e-oidc:dosh-oidc-e2e` | impersonate users/groups、create SAR=yes；业务资源读写=no | 通过 |
| 浏览器 OIDC E2E | `bash scripts/e2e-oidc-browser.sh` | 前一候选 Chromium/Firefox/WebKit 共 12/12 真实集群用例通过；无 API mock；最终候选待复跑；证据见 21.2.5、21.2.7 | 通过（前一候选） |
| OIDC 回滚 | `bash scripts/e2e-oidc-rollback.sh` | revision 11 → 12 → 13 → 14；升级、回滚、恢复后均通过 health/ready、三身份、审计和 logout | 通过 |
| Token 泄露扫描 | 浏览器响应/URL/DOM/Web Storage/Cookie、Pod 日志、K8s ConfigMap/Deployment、Helm values | 0 命中；Kubernetes Secret 存储按设计排除；证据见 21.2.5 | 通过 |
| 三浏览器回归 | `npm run e2e:container` | 本次未执行；OIDC 真集群矩阵已单独在官方 Playwright 容器内通过 | 待主 E2E 全量复跑 |

结论：OIDC 的 app/HTTP 主链路、真实 RSA 签名校验、动态 capabilities、impersonation、SAR、
RP logout、前端退出、Helm 双模式、隔离集群三身份 API/RBAC、三浏览器真实 E2E、Token 扫描和
upgrade/rollback/restore 已达到“实验室已验收”。该结论仍不覆盖企业 IdP 或生产发布，
**不能整体标记为生产已验收**。

### 21.2.3 签名测试 IdP 集群三身份矩阵证据

**冻结事实**

| 字段 | 值 |
|---|---|
| 执行时间 | 2026-11-02 20:04–20:05（Asia/Shanghai） |
| 命令 | `bash scripts/e2e-oidc.sh` |
| 测试 Release | `dosh-oidc-e2e`，namespace `dosh-e2e-oidc`，revision 2 |
| 目标 namespace | `dosh-e2e-target` |
| dosh endpoint | `https://192.168.122.100:32627` |
| 测试 IdP | `https://192.168.122.1:9443`，进程由脚本 trap 停止 |
| 镜像 tag | `192.168.122.10:5000/dosh/dosh:0.1.3-oidc-e2e-1793621036` |
| 观测 imageID | `192.168.122.10:5000/dosh/dosh@sha256:c8fda6d1d8cc5ddc22e4a60ca367f846149c1643b24658716e93bc82313ea839` |
| 测试副本数 | 1（测试 values 显式设置；Chart 生产默认仍为 2） |
| 生产隔离 | 生产 Release `dosh` revision 12、namespace `dosh`、NodePort `32626` 未变 |

**矩阵结果**

| 身份 | 登录后身份事实 | 允许动作 | 拒绝动作 | 结论 |
|---|---|---|---|---|
| viewer | `viewer@example.test`，组 `dosh-e2e-viewers,dosh-e2e-users` | 读取目标 namespace workloads=200 | scale=403；`revealSecrets=false` | 通过 |
| operator | `operator@example.test`，组 `dosh-e2e-operators,dosh-e2e-users` | 目标 namespace scale=200 | `default` namespace scale=403；Secret reveal 按 RBAC 拒绝 | 通过 |
| admin | `admin@example.test`，组 `dosh-e2e-admins,dosh-e2e-users` | 目标 namespace scale=200；nodes=200；capabilities 全开 | 无越界断言 | 通过 |

**审计与退出原始输出摘要**

```text
user=viewer@example.test   groups=[dosh-e2e-viewers,dosh-e2e-users]   action=scale result=denied
user=operator@example.test groups=[dosh-e2e-operators,dosh-e2e-users] action=scale result=success
user=operator@example.test groups=[dosh-e2e-operators,dosh-e2e-users] action=scale result=denied
user=admin@example.test    groups=[dosh-e2e-admins,dosh-e2e-users]    action=logout result=success
RP logout effective URL: https://192.168.122.100:32627/login
后置 /api/v1/session: HTTP 401
```

**本次证据不覆盖**

- Playwright 或真实浏览器中的企业 SSO 登录按钮、用户菜单、按钮隐藏和 logout 回跳。
- 企业 IdP 的私有 CA 链、claim 差异、JWKS 自动轮换、IdP 短暂不可达和时钟偏差。
- OIDC 候选的 Helm upgrade → rollback → restore，以及恢复后的登录和矩阵复验。
- 浏览器可见响应、URL、localStorage、Pod 日志、Secret/ConfigMap 中的 ID/access/refresh Token 扫描。
- 以 digest 发布生产镜像、升级生产 `dosh`、受信 TLS 和商用放行。

### 21.2.4 OIDC 需求级验收状态

本表把实现、自动化测试和集群证据映射回 EARS 条目。状态为“实验室已验收”的条目只在本节列明
的测试 IdP/隔离集群范围内成立；状态为“已实现”的条目尚缺生产或浏览器证据。

| 需求 | 设计/实现覆盖 | 已有证据 | 当前状态 | 关闭条件 |
|---|---|---|---|---|
| AUTH-O-001 | Authorization Code + S256 PKCE、state、nonce、签名、iss、aud、exp | auth/provider 单测、本地签名 HTTP 全链路、测试 IdP 集群登录 | 实验室已验收 | 企业 IdP 预发验证 |
| AUTH-O-002 | 校验后身份写入 Actor，impersonation + SAR，不信任客户端 header | transport/SAR 单测；三身份 API 与审计矩阵 | 实验室已验收 | 浏览器和预发企业 IdP |
| AUTH-O-003 | 会话 TTL 取 Token `exp` 上限；Token 不进入前端存储/URL/日志 | session 单测、代码审查；浏览器/响应/URL/Web Storage/日志/K8s 非 Secret 载体扫描 0 命中 | 实验室已验收 | 企业 IdP 预发环境复验 |
| AUTH-O-004 | 清除本地会话并调用 RP logout；同源 `/login` 绝对回跳 | 三浏览器真实 logout 按钮；HTTP 集群 logout；后置 session=401 | 实验室已验收 | 企业 IdP logout endpoint 兼容性 |
| AUTH-O-005 | 非法/过期 state、nonce mismatch、code 重放、开放重定向拒绝并审计 | auth/app、HTTP、测试 IdP 与三浏览器负向矩阵 | 实验室已验收（签名测试 IdP） | 企业 IdP 接入后复跑，并归档真实 IdP 证据 |
| AUTH-N-004 | discovery/JWKS/token/签名失败时拒绝会话，不回退 local/匿名 | provider 负向测试、fail-closed 代码路径 | 已实现 | IdP 与 JWKS 故障注入 |
| AZ-O-001 | 使用 `SubjectAccessReview`，显式 user/groups；SAR 不带 impersonation | SAR 单测；真实 RBAC 三身份 API 矩阵 | 实验室已验收 | 企业 groups claim 兼容性 |
| AZ-O-002 | capabilities 与逐项 SAR 使用同一 Authorizer | session/API capabilities 断言；三浏览器按钮隐藏与直接 API 403 对账 | 实验室已验收 | 企业 IdP 组映射 |
| AZ-N-003 | EvaluationError/网络错误 fail-closed，Repository 调用为 0 | fake client 错误注入与调用次数断言 | 已实现 | 真实 API Server 故障注入 |
| SEC-O-003 | HTTPS issuer、签名、aud、nonce、私有 CA | provider 真实 RSA/JWKS/私有 CA 单测；测试 IdP 集群登录 | 实验室已验收 | 企业 IdP 兼容性 |
| SEC-O-004 | SA 仅 impersonate users/groups、create SAR、discovery/version | Helm 渲染断言；`kubectl auth can-i` 最小权限矩阵 | 实验室已验收 | 生产 Release 复核 |
| SEC-O-005 | `returnTo` 仅站内相对路径，拒绝 `//host`、外站和 CRLF | 开放重定向负向测试 | 已实现 | 浏览器端到端注入 |
| SEC-N-004 | Token/secret 不得出现在日志、审计、URL 或错误响应 | 代码路径审查；浏览器响应/URL/DOM/Web Storage/Cookie、应用日志、ConfigMap/Deployment、Helm values 扫描 0 命中；Secret 存储例外已说明 | 实验室已验收 | 企业 IdP 预发复扫与长期证据归档 |

### 21.2.5 OIDC 浏览器、Token 与回滚证据（2026-11-02）

本节记录在隔离 Release `dosh-oidc-e2e` 上对候选镜像完成的最新证据；生产 Release `dosh`、
namespace `dosh`、NodePort `32626` 未改动。

| 字段 | 值 |
|---|---|
| 镜像 tag | `192.168.122.10:5000/dosh/dosh:0.1.3-oidc-e2e-1793622219` |
| kubelet imageID | `192.168.122.10:5000/dosh/dosh@sha256:93a097c6e98b1d6e0fdb40ea28897b6c4eb7c635c23b21638b7a98c96d82abe0` |
| 测试 Release | `dosh-oidc-e2e`，namespace `dosh-e2e-oidc`，最终 revision 14 |
| 目标 namespace | `dosh-e2e-target` |
| 测试 IdP | `https://192.168.122.1:9443`，真实 RSA 签名，短生命周期证书 |
| 生产隔离 | `dosh`/revision 12/NodePort 32626 保持不变 |

**浏览器矩阵**

```bash
DOSH_E2E_KEEP_ARTIFACTS=1 DOSH_E2E_SKIP_BUILD=1 \
DOSH_E2E_IMAGE_TAG=0.1.3-oidc-e2e-1793622219 \
  bash scripts/e2e-oidc-browser.sh
```

本节保留较早候选的 9/9 历史快照；随后新增 Deployment 创建用例，前一候选的最新结果为
**12/12 passed**，最终候选仍需复跑。WebKit 在
`mcr.microsoft.com/playwright:v1.64.1-noble` 容器内通过，避免宿主缺少 GTK/ICU 依赖。
覆盖 viewer 只读与 Secret 无权限、operator 仅目标 namespace 可写、admin 节点读取与
RP-initiated logout；浏览器请求未使用 API mock。

**Token 扫描**

同一脚本对以下载体扫描 JWT 形式和测试 access token：

- 浏览器响应体、URL、DOM、localStorage、sessionStorage、Cookie；
- dosh 应用日志和审计 stdout；
- ConfigMap、Deployment、Helm values。

结果为 `0` 命中。`dosh-e2e-auth` Kubernetes Secret 是设计允许的密钥存储，未作为泄露统计；
应用通过 `secretKeyRef` 注入，未在非 Secret 载体中发现 client secret。

**OIDC 回滚**

```bash
DOSH_E2E_KEEP_ARTIFACTS=1 DOSH_E2E_SKIP_BUILD=1 \
DOSH_E2E_IMAGE_TAG=0.1.3-oidc-e2e-1793622219 \
  bash scripts/e2e-oidc-rollback.sh
```

实际 revision 链路为 `11 (baseline) → 12 (upgrade) → 13 (rollback to 11) → 14 (restore 12)`。
在每个 revision 都通过 `/healthz`、`/readyz`、viewer read=200/write=403、operator
target=200/other=403、admin scale/nodes=200，并确认审计包含三个身份和 denied 事件、RP logout
最终回到同源 `/login` 且后置 `/api/v1/session` 为 401。

证据文件：`/tmp/tmp.ELCZpX59tz/oidc-rollback-evidence.log`；Token 扫描产物：
`/tmp/tmp.Jnc5Wm3ybi/token-leak-scan.log`。该历史候选的临时目录不是长期归档，也没有进入
v2.8 的当前候选归档；不得替代当前 digest 的证据链。

### 21.2.6 Metrics 与 429/5xx 重试证据（2026-11-02）

**Metrics Server**

| 项目 | 证据 |
|---|---|
| 安装位置 | `dosh-metrics/metrics-server:v0.9.1`，不属于生产 `dosh` Release |
| 运行状态 | Deployment `1/1`，Pod `1/1 Running`，0 restarts |
| 聚合层 | `v1beta1.metrics.k8s.io` 的 `Available=True` |
| 集群读取 | `kubectl top nodes` 返回 8 个节点；`kubectl top pods -A` 返回节点与 Pod 指标 |
| 隔离 Release 三态 | `available=true cpu=2035m mem=10395652096`；禁用后 `metricsAvailable=false usage=0 hasMetrics=false`；恢复后 `available=true cpu=2155m mem=10395906048` |
| 证据文件 | `/tmp/tmp.MwkrC9s1Mj/metrics-evidence.log` |
| 结论 | Metrics 功能在隔离测试 Release 上“实验室已验收”；生产 App `0.1.2` 未包含本轮候选改动，目标发布仍需复验 |

**Kubernetes 429/5xx 有界重试**

`internal/platform/kube/retry.go` 通过 `rest.Config.WrapTransport` 包装所有 client-go 客户端；
OIDC impersonation transport 位于其外层。实现约束如下：

- 仅重试 `GET`、`HEAD`、`OPTIONS`；`POST/PUT/PATCH/DELETE` 不自动重放，避免“服务端已成功但响应丢失”造成重复写。
- 重试状态码限定为 429、500、502、503、504；连接错误可重试，但 `context.Canceled` 和
  `context.DeadlineExceeded` 立即返回。
- 最多 3 次尝试，默认退避 100 ms、200 ms，单次上限 2 s；`Retry-After` 同时支持秒数和 HTTP 日期，并被上限截断。
- 每次退避都监听 context；用户取消或请求超时后不得继续重试。

```bash
go test -race ./internal/platform/kube \
  -run 'TestRetry|TestImpersonation' -count=1 -v
```

结果为 8/8 通过，覆盖幂等请求重试、写请求不重试、次数上限、context 取消、`Retry-After`
截断和 impersonation 交互。

前一完整门禁候选 `0.1.3-rc-20261102153511@sha256:47983a…312e` 完成了完整注入：429、500、502、503、504 各连续注入
2 次后恢复 200；持续 429 恰好尝试 3 次并返回 `{"code":"rate_limited"}`；`Retry-After` 实测约
2097 ms。写请求只发送 1 次且副本数不变，连接重置、客户端取消和恢复均通过。当前证据见 21.2.10
和 `/tmp/dosh-e2e-retry-20261102154640`；当前 OIDC 候选 `sha256:0be6df25…798ca8` 仍需按 25.9
第 2 项复跑，生产故障注入也需在发布前复验。

### 21.2.7 历史候选与 Deployment 创建向导证据（2026-11-02，已被 21.2.10 替代）

本节保留旧候选 `0.1.3-rc-1793626263` 的历史事实，不得用于当前发布。前一完整门禁候选
`0.1.3-rc-20261102153511` 的证据见 21.2.10，当前 OIDC 候选见 21.2.12。生产
`dosh`/revision 12/NodePort `32626` 未变。

| 字段 | 值 |
|---|---|
| 最终候选 App 镜像 | `192.168.122.10:5000/dosh/dosh:0.1.3-rc-1793626263` |
| App registry digest | `sha256:8d7242ae694a920e38cced915930b8d158c7edd99ab9823dd0a6ce481b2febbe` |
| 故障代理镜像 | `192.168.122.10:5000/dosh/kube-fault-proxy:kube-fault-proxy-1793624877` |
| 故障代理 digest | `sha256:3b65afeefaeb747d38d4fdd7f59872813a75da3be7c4b7e2492095a663dd1805` |
| OIDC 测试环境 | namespace `dosh-e2e-oidc`、Release `dosh-oidc-e2e`、当前 revision 27、1/1 Ready、NodePort `32627`、目标 namespace `dosh-e2e-target` |
| 浏览器矩阵证据 | 前一候选 `0.1.3-create-1793626058` 为 12/12 passed；最终候选待复跑。临时产物目录 `/tmp/tmp.k6hcJ3CHcu` 仅含 fixture、infra 和 Token 扫描日志，正式证据仍须归档 |
| retry 证据 | `/tmp/tmp.4SwyYilzsC/retry-evidence.log` 及各状态码 JSON；最终候选读路径通过，写路径/取消待复跑 |

**Deployment 创建向导**

| 验收项 | 证据摘要 | 结论 |
|---|---|---|
| operator 创建 | 从工作负载页打开三步向导，预览 YAML 后创建 `e2e-ui-*`，跳转详情并在列表可检索 | 前一候选通过 |
| kubectl 对账 | 名称、namespace、selector、镜像、端口、labels 与向导输入一致 | 前一候选通过 |
| viewer 拒绝 | 直接调用创建 API 返回 403，测试 namespace 未产生资源 | 前一候选通过 |
| 非法输入 | 非法字段返回 400 `fieldErrors`，不发送 Kubernetes 写请求 | 组件测试通过；真实矩阵覆盖 400 |
| 重复创建 | 返回 409，错误码 `already_exists`，不误报 resourceVersion 冲突 | 组件与前一候选真实矩阵通过 |
| 审计 | 创建成功和拒绝均记录 actor、action、resource、namespace、result、requestId | 前一候选通过 |
| 前端权限 | 只有 `capabilities.create=true` 时显示创建按钮；最终后端仍逐次授权 | 组件测试通过 |
| 最终候选复跑 | 至少重跑 viewer/operator/admin、12/12 浏览器、创建/重复/非法和清理 | 待执行 |

**最终候选门禁状态**

| 门禁 | 当前事实 | 结论 |
|---|---|---|
| `gofmt`、`go vet`、全量 `go test -race ./...` | 在 `ErrAlreadyExists` 改动前完成过全量通过；改动后只复跑相关定向测试 | 最终候选全量待复跑 |
| `make arch`、`make api-check`、`make helm-check` | 历史全量通过；Deployment 契约和错误映射改动后需复跑 | 最终候选待复跑 |
| 前端 typecheck、Vitest、build、bundle | 当前 9/9 Vitest、27/27 E2E、构建和 339.78 KiB 初始 JS gzip 通过 | 最终候选待复跑 |
| OIDC 真实浏览器矩阵 | 前一候选 12/12；最终候选未复跑 | 待验收 |
| 最终镜像 Trivy | 尚未对 digest `sha256:8d7242…febbe` 执行 | 未验收 |
| 生产发布/回滚 | 未执行，生产仍为 `0.1.2/0.1.4` revision 12 | 未验收 |

结论：该节只能证明历史候选阶段的实现状态。前一完整门禁候选 `0.1.3-rc-20261102153511` 已在
21.2.10 完成创建向导、浏览器、retry、soak、审计和回滚复验；当前 OIDC 候选只在 21.2.12 完成
OIDC 负向矩阵。不得引用本节或 21.2.10 的旧 digest 作为当前发布证据。

### 21.2.8 架构与路由门禁快照（2026-11-02，未发布）

本节只记录当前工作树的本地门禁，不代表生产候选已重新构建。执行环境仍为 `/root/projects/dosh`；
生产 `dosh`、revision 12 和 NodePort `32626` 未改动。

| 门禁 | 命令 | 本次证据 | 结论 |
|---|---|---|---|
| 架构规则 | `make arch` | `architecture checks: pass` | 通过 |
| 路由/OpenAPI 双向对账 | `go test ./internal/bootstrap -run TestRouteInventoryMatchesOpenAPI -count=1` | `ok dosh/internal/bootstrap`，33 个 OpenAPI path 与 chi 路由差集为空 | 通过 |
| 模块路由归属 | `go test ./internal/bootstrap -run TestModuleRoutesAreScoped -count=1` | 每个模块独立 Router 只注册本模块路径前缀 | 通过 |
| 跨模块生产 import | `make arch` 内 `go list` 规则 | 业务模块生产代码不 import 其它模块 HTTP；`authhttp.ActorFrom` helper 已移除 | 通过 |
| 受限模块 Handler | `go test ./internal/modules/configuration/http ./internal/modules/workloads/http ./internal/modules/yaml/http -count=1` | 三个包均 `ok`，Actor 从 `domain.ActorFromContext` 获取 | 通过 |
| 严格配置负向 | `go test ./internal/platform/config -count=1` | `ok`；覆盖布尔/整数/浮点/duration 解析失败、空集群名、非法日志级别、Cookie Secure 无 TLS | 通过 |
| OpenAPI 漂移 | `make api-check` | 临时生成物与 `web/src/api/schema.d.ts` 逐字节一致；33 paths / 43 schemas | 通过 |
| 动态集群上下文 | `npm run typecheck && npm test -- --run && npm run build && npm run bundle:check && npm run e2e:container` | Vitest 9/9；Chromium/Firefox/WebKit 27/27；初始 JS gzip 339.78 KiB；非实验室集群名/版本/CNI 与 overview 503 均覆盖 | 通过 |
| 门禁脚本自身 | `scripts/check-architecture.sh` | 修正测试文件误报：生产代码仍只允许 `platform/config` 读取环境变量，`*_test.go` 不计入生产依赖 | 通过 |

**后续状态**：本表记录时尚未重新执行全量门禁；随后 21.2.9 已完成 `make verify`、
`make race`、镜像构建/扫描和版本一致性检查。CI 真跑和隔离 Release 回归仍未完成，
因此本表只能支撑“架构实现与本地门禁通过”，不能作为发布放行证据。

### 21.2.9 历史候选与隔离部署准备（2026-11-02，已被 21.2.10 替代）

本节记录候选 `0.1.3-rc-20261102152403` 的历史快照。该镜像早于总览面包屑修复和 Helm
`DOSH_MAX_REQUEST_BODY` 整数修复，已被 21.2.10 的新候选替代；其证据不得用于生产放行。
生产 `dosh`、revision 12、NodePort `32626` 在整个过程中未改动。

**镜像与源码身份**

| 字段 | 值 |
|---|---|
| App/OpenAPI/Web 版本 | `0.1.3` |
| Chart 版本 | `0.1.5`，`appVersion: 0.1.3` |
| 候选 tag | `192.168.122.10:5000/dosh/dosh:0.1.3-rc-20261102152403` |
| registry digest（权威） | `sha256:a2cf193cf12f3465040052cd1f26dcba95766d8a108298a19c968c14873360f3` |
| 本地 image config ID | `sha256:0051e0baae831cfa27222f6c11710c6a4c0ca61ab7e89fdb2106860c852e5a9e` |
| 构建时间 | `2026-11-02T15:24:17Z` |
| 二进制版本 | `dosh 0.1.3 (source-20261102, 2026-11-02T15:24:17Z)` |
| Trivy | Debian 12.15 与 Go binary 均 0 HIGH/CRITICAL；扫描时间 `2026-11-02T15:24:55Z` |
| 不可变性核验 | `curl -ksSI -H 'Accept: application/vnd.oci.image.manifest.v1+json' https://192.168.122.10:5000/v2/dosh/dosh/manifests/0.1.3-rc-20261102152403` 返回 `Docker-Content-Digest: sha256:a2cf…60f3` |

> 注意：`podman image inspect` 的 `.Digest`/`RepoDigests` 是本地平台 manifest 视图，不能替代
> registry 的 `Docker-Content-Digest`。Helm 发布时使用 `image.digest` 固定为 registry 返回的
> `sha256:a2cf…60f3`，不要以 tag 作为发布身份。

**本地门禁**

| 门禁 | 命令 | 结果 |
|---|---|---|
| 全量工程门禁 | `make verify` | 通过（含 `all`、`arch`、`api-check`、`helm-check`、`version-check`） |
| 竞态 | `make race` | 通过 |
| 容量 | `make capacity-test` | 最新复跑 Pod 列表 P95 `22.950 ms`；工作负载 P95 `193.397 ms`；Overview P95 `199.418 ms`；Heap `28 MiB` |
| 版本一致性 | `make version-check` | App `0.1.3`、Chart `0.1.5`，所有版本源一致 |
| Helm digest 正向 | `helm template ... --set image.digest=sha256:<64 hex>` | 渲染为 `repo@sha256:<64 hex>` |
| Helm digest 负向 | `--set image.digest=sha256:not-a-digest` | 模板阶段失败；不接受短 digest 或非法字符 |
| 前端类型/单测 | `npm run typecheck && npm test -- --run` | 通过；Vitest 9/9 |
| 前端浏览器 | `npm run e2e:container` | Chromium/Firefox/WebKit 共 27/27 |
| 前端构建 | `npm run build && npm run bundle:check` | 通过；初始 JS gzip `339.78 KiB`，低于 500 KiB |
| 生产首屏 | `npm run e2e:performance` | LCP `492 ms` |
| 镜像安全 | `make image-scan TAG=0.1.3-rc-20261102152403` | HIGH/CRITICAL 0；DB 版本/扫描时间已在上表记录 |

这些结果构成“本地候选可进入隔离验收”，不构成“生产可发布”。镜像扫描、版本门禁和前端
测试都不能替代真实 Release 的 OIDC、故障注入、长连接、审计和回滚证据。

**隔离 Release 准备**

| 项目 | 状态 |
|---|---|
| namespace `dosh-ui-e2e` | 已创建，未修改生产 `dosh` |
| Secret `dosh-auth`、`dosh-tls` | 已创建；密码哈希、session key 和 TLS 文件存放在受限 `/tmp` 路径，文档不记录敏感值 |
| 预留 NodePort | `32629`；`32628` 保留给其他审计 E2E，禁止占用生产 `32626` |
| Helm Release `dosh-ui-e2e` | 尚未安装 |
| 计划镜像 | `192.168.122.10:5000/dosh/dosh@sha256:a2cf…60f3` |
| 计划集群名 | `runtime.clusterName=dosh-ui-dynamic-e2e` |
| 计划副本 | 2；验收 2/2 Ready、health/ready、登录和 overview |

**下一步硬门禁**：用上述 digest 安装隔离 Release 后，必须依次完成 2/2 Ready、`/healthz` 与
`/readyz`、登录、overview 动态集群名、OIDC 三身份与浏览器矩阵、Deployment 创建/扩缩容/重启/
删除/409/403/400、retry 读/写/重置/取消、30 分钟 soak、审计 Webhook 故障/恢复/SIGTERM、Metrics
三态和 Helm upgrade→rollback→restore。任何一项未在该 digest 上复跑，第 26.9 节不得改为“已验收”。

### 21.2.10 前一完整门禁候选与隔离集群验收（历史，2026-11-02 至 2026-11-03）

本节记录候选 `0.1.3-rc-20261102153511@sha256:47983a…312e` 的完整门禁结果，用于历史回归参考。
它不是当前源码候选；当前候选 `0.1.3-rc-oidc-20261102171324@sha256:0be6df25…798ca8` 的 OIDC
负向矩阵见 21.2.12。生产 `dosh`、revision 12、NodePort `32626` 全程未改动；以下运行证据均绑定
该历史候选的隔离 Release 和 registry digest。

**镜像与版本身份**

| 字段 | 值 |
|---|---|
| App/OpenAPI/Web 版本 | `0.1.3` |
| Chart 版本 | `0.1.5`，`appVersion: 0.1.3` |
| 候选 tag | `192.168.122.10:5000/dosh/dosh:0.1.3-rc-20261102153511` |
| registry digest（权威） | `sha256:47983a7aedf46d683f56982b0f352a684ae81d126b3390eada1b9bae0ba4312e` |
| 本地 image config ID | `sha256:457b572810ad47c22f26c3e4374e32457e985417f683cdd4daeebbc2aaf69ba6` |
| 构建时间 | `2026-11-02T15:35:20Z` |
| 二进制版本 | `dosh 0.1.3 (source-20261102, 2026-11-02T15:35:20Z)` |
| Trivy | Debian 12.15 与 Go binary 均 0 HIGH/CRITICAL；扫描时间 `2026-11-02T15:35:26Z` |

**本地门禁**

| 门禁 | 命令 | 结果 |
|---|---|---|
| 全量工程门禁 | `make verify` | 通过；`api-check`、`arch`、`helm-check`、`version-check` 均通过 |
| 竞态 | `make race` | 通过 |
| 容量 | `make capacity-test` | Pod 列表 P95 `22.950 ms`；工作负载 `193.397 ms`；Overview `199.418 ms`；Heap `28 MiB` |
| 前端单测 | `npm test -- --run` | 10/10 通过，新增总览面包屑动态化用例 |
| 三浏览器 mock E2E | `npm run e2e:container` | Chromium/Firefox/WebKit 30/30 通过 |
| 构建与预算 | `npm run build && npm run bundle:check` | 通过；initial JS gzip `339.78 KiB` |
| Helm 整数渲染 | `make helm-check` | `DOSH_MAX_REQUEST_BODY` 断言为 `"2097152"`；非法 digest 负向失败 |

**隔离本地认证 Release `dosh-ui-e2e`**

| 验收项 | 证据 | 结论 |
|---|---|---|
| Release | `dosh-ui-e2e/dosh-ui-e2e`，revision 2，Chart `0.1.5`，App `0.1.3` | 通过 |
| 镜像 | Deployment 使用 `dosh/dosh@sha256:47983a…312e`，不用可变 tag | 通过 |
| 副本与调度 | 2/2 Ready，PDB `minAvailable=1`，Pod 分布在不同 worker | 通过 |
| NodePort | Service `32626:32629/TCP`；生产仍为 `32626:32626/TCP` | 通过 |
| 健康检查 | `/healthz`、`/readyz` 连续 5 次 200，`/readyz` 中 Kubernetes `ok` | 通过 |
| 登录与权限 | local admin 登录 200，capabilities 全开；未认证 API 401；错误密码 401 | 通过 |
| 动态集群上下文 | `/api/v1/overview` 返回 `dosh-ui-dynamic-e2e`、`v1.36.5`、`Cilium`；真实三浏览器 3/3 验证顶部栏、状态、总览面包屑并完成退出 | 通过 |
| 发现并修复缺陷 | 总览页原硬编码 `总览 / kubedo-lab`，已改为读取 overview；Helm 原将 max body 渲染为科学计数法，已用 `int64` 修正 | 已关闭 |

**OIDC 隔离 Release `dosh-oidc-e2e`**

| 验收项 | 证据 | 结论 |
|---|---|---|
| 镜像身份 | 同一 tag@digest `sha256:47983a…312e` | 通过 |
| 三身份 API/RBAC | viewer 读 200/写 403/Secret 无权限；operator 仅目标 namespace 可写；admin 节点与写操作通过 | 通过 |
| Deployment 创建 | preview 200、viewer 403、非法副本 400 且无资源、operator 201、重复创建 409、kubectl 字段对账 | 通过 |
| 浏览器矩阵 | Chromium/Firefox/WebKit 共 12/12 通过，无 API mock | 通过 |
| Token 泄露 | 浏览器响应/URL/DOM/Web Storage/Cookie、应用日志、ConfigMap/Deployment、Helm values 均 0 命中 | 通过 |
| RP logout | 最终回到 `/login`，旧会话后续请求 401 | 通过 |
| 证据目录 | `/tmp/dosh-e2e-oidc-browser-20261102154448` | 已保留 |

**Kubernetes API 故障注入与 Metrics**

| 验收项 | 证据 | 结论 |
|---|---|---|
| 瞬时 429/500/502/503/504 | 每种状态连续 2 次失败后第 3 次得到 200；代理计数与设计一致 | 通过 |
| 持续 429 | 恰好 3 次尝试，返回 429 `rate_limited`；`Retry-After` 实测约 2097 ms | 通过 |
| 写请求 | 注入 503 时只发送 1 次 PUT，Deployment 副本数保持不变 | 通过 |
| 连接重置 | 两次 reset 后恢复 200 | 通过 |
| 客户端取消 | 客户端 1 s 超时退出，代理只观察到 1 次被取消请求 | 通过 |
| 生产隔离 | 测试前后均确认生产 NodePort `32626`、namespace `dosh`、镜像 `0.1.2` 未变化 | 通过 |
| retry 证据 | `/tmp/dosh-e2e-retry-20261102154640` | 已保留 |
| Metrics 三态 | Available 时 CPU/Memory usage > 0；Metrics Server 缩零后 `metricsAvailable=false` 且无陈旧值；恢复后真实 usage 恢复 | 通过 |
| Metrics 证据 | `/tmp/dosh-e2e-metrics-20261102154802` | 已保留 |

**30 分钟 soak**

| 验收项 | 证据 | 结论 |
|---|---|---|
| 运行时长与并发 | `2026-11-02T23:50:25+08:00` 至 `2026-11-03T00:20:27+08:00`；30 条事件 SSE + 30 条日志流 | 通过 |
| 事件与日志 | 生成 29 个事件；`event_frames=50400`、`log_lines=28449`；事件创建失败 0 | 通过 |
| 内存 | 56 次采样，`max_pod_memory_mi=28`（阈值 256 MiB），首/末样本 24/19 MiB | 通过 |
| 故障与重连 | Pod 删除 1 次、应用滚动重启 1 次、`reconnects=5`、`stream_failures=0`、`app_errors=0` | 通过 |
| 镜像绑定 | 运行记录包含 `0.1.3-rc-20261102153511@sha256:47983a…312e` | 通过 |
| 证据目录 | `/tmp/dosh-e2e-soak-20261102155005/soak-evidence.log` | 已保留 |

**审计 Webhook / SIGTERM 隔离 Release `dosh-audit-e2e`**

| 验收项 | 证据 | 结论 |
|---|---|---|
| 镜像与 Release | 同一 tag@digest；namespace `dosh-e2e-audit`、Release `dosh-audit-e2e`、NodePort `32628`；最终 Helm revision 10 | 通过 |
| 私 CA 与双认证 | 接收端使用独立 CA；Bearer + HMAC 同时校验；基线 Secret reveal 审计事件 `verified=true` | 通过 |
| 单认证兼容 | Token-only 与 HMAC-only 各触发一次投递并确认 requestId | 通过 |
| 业务成功/拒绝矩阵 | scale denied/success、restart、delete 均有对应事件；只读模式扩容返回 403 且审计 `denied`、资源不变 | 通过 |
| 瞬时故障 | 前两次 503、第三次 200；接收端 attempts ≥3、received=1 | 通过 |
| 持续故障与超时 | 持续 503 恰好 3 次且记录 `audit_webhook_delivery_failed`；1.5 s 延迟在 1 s 超时下同样有界，恢复后新事件继续投递 | 通过 |
| 队列压力 | `queueSize=1` 下连续 200 个业务请求均成功；出现 `audit_webhook_queue_full`，业务未被阻塞 | 通过 |
| SIGTERM 排空 | 滚动重启前产生 20 个唯一 requestId，重启后接收端全部收到；未出现 `audit_flush_failed` | 通过 |
| 敏感信息扫描 | 接收端日志、Pod stdout 对 Token、HMAC、Secret 明文扫描均 0 命中 | 通过 |
| 生产隔离 | 测试前后生产 App `0.1.2`、revision 12、NodePort `32626` 未变化 | 通过 |
| 证据目录 | `/tmp/dosh-e2e-audit-20261102162039/audit-webhook-evidence.log` | 已保留 |

**OIDC upgrade / rollback / restore 隔离 Release `dosh-oidc-e2e`**

| 验收项 | 证据 | 结论 |
|---|---|---|
| 镜像与隔离 | 同一 tag@digest；namespace `dosh-e2e-oidc`、Release `dosh-oidc-e2e`、NodePort `32627` | 通过 |
| revision 链 | baseline 59 → upgrade 60 → rollback 61 → restore 62；每次 rollback/restore 都产生新的可追踪 revision | 通过 |
| 基线矩阵 | health 200、ready 200；viewer read 200/write 403、operator target 200/other 403、admin scale/nodes 200、logout 后 401 | 通过 |
| 升级后矩阵 | revision 60 重复同一健康、身份、拒绝与 logout 矩阵 | 通过 |
| 回滚后矩阵 | 回滚到 revision 59、产生 revision 61；health/ready、权限和 logout 无回归 | 通过 |
| 恢复后矩阵 | 恢复到升级 revision 60、产生 revision 62；最终 Pod 使用候选 digest，矩阵无回归 | 通过 |
| 生产隔离 | 演练前后生产 App `0.1.2`、revision 12、NodePort `32626` 未变化 | 通过 |
| 证据目录 | `/tmp/dosh-e2e-rollback-20261102162204/oidc-rollback-evidence.log` | 已保留 |

**原始证据校验和（归档前）**

| 证据 | 路径 | SHA-256 |
|---|---|---|
| soak | `/tmp/dosh-e2e-soak-20261102155005/soak-evidence.log` | `57285fbe711a04ec9af6d89d8342014aa0c5aad15598b19d34d398a4d15b34fd` |
| audit/SIGTERM | `/tmp/dosh-e2e-audit-20261102162039/audit-webhook-evidence.log` | `fddc62ac12908282199e85d8d01ea5328c12cd1d3b08209b43f6f2fa3acb55bd` |
| rollback/restore | `/tmp/dosh-e2e-rollback-20261102162204/oidc-rollback-evidence.log` | `df02aa2ad6f8379b3c78eb714d6753ac8821e467f94899dd29e8dd7cd88aa945` |
| retry | `/tmp/dosh-e2e-retry-20261102154640/retry-evidence.log` | `9b7e9ab55733f1973607b648e0aae7fd684eb136e323b31c84870fd28e339bdb` |
| Metrics | `/tmp/dosh-e2e-metrics-20261102154802/metrics-evidence.log` | `e63fb9512f37f520718b03884b9b01b385c82c21a572663969ee288c5334d0ef` |
| OIDC Token 扫描 | `/tmp/dosh-e2e-oidc-browser-20261102154448/token-leak-scan.log` | `125a5df21ea1ecf647e56e17edba12b48763018a54c4e0ebcc695b1fa5fb7e58` |

上述校验和用于确认原始 `/tmp` 文件在归档前未变化。归档后的正式清单见 21.2.11；原始目录
仍只能作为工作副本，不能替代受控归档。

**历史结论**：该 digest 已达到“隔离集群基础运行、动态上下文、OIDC、创建向导、retry、Metrics、
30 分钟 soak、审计 Webhook/SIGTERM 和 rollback/restore 已验收”；尚不能进入生产，因为企业 IdP、
受信 TLS、真实 SIEM、Edge/Safari、CI 真跑、离机 WORM 归档和生产发布/回滚仍未完成。
该结论只绑定 `sha256:47983a…312e`，不得继承给当前 OIDC 候选。

### 21.2.11 受控证据归档（本地完成，离机待办）

> 本节记录历史 digest 的归档完整性设计。最终候选 `69f30f00…00523` 的归档结果见 21.2.25。

**归档工具设计**

`scripts/archive-evidence.sh` 只归档显式列出的文件或目录；归档前拒绝私钥、证书、Cookie、session、
fixture 元数据、请求头/响应体转储、密码/Secret 文件名，以及正文含 Bearer Token、Cookie 或敏感
环境变量的文件。归档采用同盘临时目录、完成后原子重命名、目录 `0700`、文件 `0600`。

完整性链固定为：

1. 先写 `metadata.txt` 和 `summary.txt`；
2. `manifest.sha256` 覆盖除自身和 `manifest-digest.txt` 外的全部文件，因此包含 `summary.txt`；
3. 计算 manifest 自身 SHA-256，写入 `manifest-digest.txt`；
4. 归档完成后执行 `sha256sum -c manifest.sha256` 和 `sha256sum -c manifest-digest.txt`；
5. manifest 自身摘要另行记录在发布记录或工单中，作为外锚点。

`scripts/test-archive-evidence.sh` 与 `make evidence-check` 已覆盖：摘要被 manifest 覆盖、
manifest-digest 不自引用、双层校验、含 Bearer Token 的文件拒绝、目录仅有被排除文件时拒绝、
非法 candidate digest 拒绝。

**归档结果**

| 项目 | 小证据集 | 完整证据集 |
|---|---|---|
| candidate digest | `sha256:47983a7aedf46d683f56982b0f352a684ae81d126b3390eada1b9bae0ba4312e` | 同左 |
| 归档目录 | `/var/lib/dosh/evidence/47983a7aedf4/20261102T163425Z-final-candidate-v2-20261103` | `/var/lib/dosh/evidence/47983a7aedf4/20261102T163426Z-final-candidate-full-v2-20261103` |
| 证据文件数 | 6 | 510 |
| 证据正文总字节 | 16,938 | 18,096,557 |
| manifest SHA-256 | `aa2b81f63dbb3f27fa03abd6817cf17344de93a12a4e20114418b967d673f891` | `6d2b704469e02fce8fcfb32eca032e1023227c8ca700d1ef21726f8d376a1094` |
| 目录/文件权限 | `0700` / `0600` | `0700` / `0600` |
| `summary.txt` 被 manifest 覆盖 | 是 | 是 |
| 双层校验 | 通过 | 通过 |
| 敏感材料扫描 | 未命中 | 0 个文件命中 |

小证据集包含 soak、审计 Webhook、OIDC rollback、retry、Metrics 和 Token 扫描的摘要日志；
完整证据集包含上述六个原始证据目录的脱敏副本及 `excluded.txt`。此前的
`20261102T162926Z-final-candidate-20261103` 和 `20261102T163038Z-final-candidate-full-20261103`
保留为历史记录；因其 `summary.txt` 未进入 manifest，不作为 v2.8 的正式完整性证据链。

**复核命令**

```bash
make evidence-check
cd /var/lib/dosh/evidence/47983a7aedf4/20261102T163426Z-final-candidate-full-v2-20261103
sha256sum -c manifest.sha256
sha256sum -c manifest-digest.txt
grep -Eq '  \./summary\.txt$' manifest.sha256
```

通过标准：两条 `sha256sum -c` 均无 `FAILED`；`summary.txt` 出现在 manifest；manifest 不含
`manifest-digest.txt` 自身；敏感材料扫描 0 命中。

**状态边界**：本机受控归档已完成；离机 WORM、长期保留期限、访问审批、完整性巡检、销毁流程
尚未验收。生产发布前必须把 manifest 摘要和归档目录 ID 写入变更单，并将归档复制到企业批准的
离机存储。

### 21.2.12 OIDC 负向矩阵候选证据（2026-11-03）

**候选身份**

| 项目 | 值 |
|---|---|
| candidate tag | `192.168.122.10:5000/dosh/dosh:0.1.3-rc-oidc-20261102171324` |
| registry digest | `sha256:0be6df2586a3e54a34c4b8221622a3ccaefaa7224a9e5ed3c718cf5cf0798ca8` |
| Helm Release | `dosh-e2e-oidc/dosh-oidc-e2e`，revision 68 |
| 隔离端点 | `https://192.168.122.100:32627` |
| 副本/端口 | 1/1 Ready（测试环境降副本）；NodePort `32627`，未占用生产 `dosh:32626` |
| 测试 IdP state TTL | `5000ms`；生产默认 `10m`，配置范围 `1s–30m` |
| 归档 | `/var/lib/dosh/evidence/0be6df2586a3/20261102T172547Z-oidc-negative-matrix-v1` |
| manifest SHA-256 | `74470e6a6b4eae8134b404fbb060d5f631fc37cfec6aa4be4cca37403efc89cc` |
| 归档文件数 | 14 |

**AUTH-O-005 负向矩阵**

| 场景 | 浏览器断言 | 结果 |
|---|---|---|
| state Cookie 缺失 | 回调跳转 `/login?error=oidc`、session 401、无 session Cookie | 通过 |
| state Cookie 被篡改 | 解密失败拒绝、无会话、无 Token | 通过 |
| state 参数与 Cookie 不匹配 | 常量时间比较失败、不交换 code | 通过 |
| state 过期 | 超过 `DOSH_OIDC_STATE_TTL` 后拒绝、无会话 | 通过 |
| nonce mismatch | 测试 IdP 签发错误 nonce；真实 JWKS 校验链拒绝 | 通过 |
| authorization code 重放 | 首次成功；清理会话后重放同一 code 返回 `invalid_grant` 并拒绝 | 通过 |
| 开放重定向 `returnTo=//evil.example.test/steal` | 登录后停留同源 `/`，不访问恶意源 | 通过 |

Chromium、Firefox、WebKit 共 `11 test × 3 = 33/33` 通过。每条拒绝路径均断言
`/api/v1/session=401`、无会话 Cookie、无 OIDC 业务写请求、URL 不保留 code/state、DOM/Storage/
response body 不含 JWT 或测试 access token。服务端日志中 callback failure 为 18/18，
`action=login`、`result=denied` 的审计为 18/18，且每条都有非空 `request_id`。

**其他门禁**

| 门禁 | 结果 |
|---|---|
| `make verify` | 通过；gofmt/vet/test/typecheck/build/arch/API/Helm/version 一致 |
| `make race` | 通过 |
| Vitest | 14/14 |
| mock E2E | Chromium/Firefox/WebKit 42/42 |
| 生产构建 LCP | 984ms（观测值 492ms，预算 2.5s） |
| initial JS gzip | `340.68 KiB`，预算 500 KiB |
| npm production audit | 0 vulnerabilities |
| Trivy | Debian 12.15 与 Go binary 均 0 HIGH/CRITICAL |

**复核命令**

```bash
DOSH_E2E_SKIP_BUILD=1 \
DOSH_E2E_IMAGE_TAG=0.1.3-rc-oidc-20261102171324 \
DOSH_E2E_IMAGE_DIGEST=sha256:0be6df2586a3e54a34c4b8221622a3ccaefaa7224a9e5ed3c718cf5cf0798ca8 \
DOSH_E2E_KEEP_ARTIFACTS=1 \
  bash scripts/e2e-oidc-browser.sh

cd /var/lib/dosh/evidence/0be6df2586a3/20261102T172547Z-oidc-negative-matrix-v1
sha256sum -c manifest.sha256
sha256sum -c manifest-digest.txt
```

**状态边界**：该候选证明签名测试 IdP、隔离 Release 和三种浏览器内核下的负向登录关闭；
企业 IdP 的 claims、私有 CA、登出和同一负向矩阵仍未验收。本候选的 retry、Metrics 三态、
审计 Webhook/SIGTERM 和 rollback/restore 已在 21.2.13 完成复验；但 30 分钟 soak 仍只绑定
前一完整门禁候选，真实 Release 的主题/axe/键盘套件和证据受控归档仍未完成，因此本候选仍不能
提升为完整商用放行。

### 21.2.13 当前候选故障、审计与回滚门禁复验（2026-11-03）

**候选身份与隔离边界**

| 项目 | 值 |
|---|---|
| candidate tag | `192.168.122.10:5000/dosh/dosh:0.1.3-rc-oidc-20261102171324` |
| registry digest | `sha256:0be6df2586a3e54a34c4b8221622a3ccaefaa7224a9e5ed3c718cf5cf0798ca8` |
| OIDC Release | `dosh-e2e-oidc/dosh-oidc-e2e`，最终 revision 75，NodePort `32627` |
| 审计 Release | `dosh-e2e-audit/dosh-audit-e2e`，最终 revision 10，NodePort `32628` |
| 生产边界 | `dosh` 保持 App `0.1.2`、revision 12、NodePort `32626`，测试前后未变化 |
| 归档状态 | 下列日志已写入当前 digest 证据目录，但尚未生成受控 manifest/WORM 副本 |

**retry 故障注入**

首次执行在 fixture 切换故障代理后、业务断言前以退出码 7 失败，保留在
`20261102T173748Z-retry-gate` 作为夹具/代理启动瞬态证据，不据此判定产品通过。使用已存在的
代理镜像重跑后通过，证据为
`/var/lib/dosh/evidence/0be6df2586a3/20261102T173942Z-retry-gate-r2/retry-evidence.log`。

| 场景 | 结果 |
|---|---|
| 429/500/502/503/504 | 每种先注入 2 次，随后恢复 200；读请求最多 3 次 |
| 持续 429 | 恰好 3 次后返回 `429` + `code=rate_limited` |
| `Retry-After` | 实测等待 `2098ms` 后恢复 200 |
| 写请求 | `PUT /scale` 仅 1 次；副本数 before/after 均为 2 |
| 连接重置 | 两次失败后重试恢复 200 |
| 客户端取消 | `curl_exit=28`；无额外业务请求 |
| 恢复与隔离 | 恢复后 200；生产 namespace/NodePort 未变化 |

**Metrics 三态**

证据：
`/var/lib/dosh/evidence/0be6df2586a3/20261102T174033Z-metrics-gate/metrics-evidence.log`。

| 状态 | 断言 |
|---|---|
| Available | `metricsAvailable=true`；CPU `2114m`；内存 `10683842560` bytes；nodes 和 pods 均有指标 |
| Unavailable | `metricsAvailable=false`；usage 归零；`hasMetrics=false`；无陈旧值 |
| Recovered | `metricsAvailable=true`；CPU `2159m`；内存 `10682662912` bytes |

Metrics Server 恢复后为 `1/1`，APIService `v1beta1.metrics.k8s.io` 为 Available。

**审计 Webhook / SIGTERM**

证据：
`/var/lib/dosh/evidence/0be6df2586a3/20261102T174204Z-audit-gate/audit-webhook-evidence.log`。

| 场景 | 结果 |
|---|---|
| 私 CA + Bearer/HMAC | 通过；`reveal` 事件带有 Pod/requestId 关联 |
| token-only / HMAC-only | 两种模式均通过 |
| 业务事件 | `scale denied`、`scale success`、`restart`、`delete` 均到达且可关联 |
| 瞬时 503 | 重试后到达 |
| 持续 503 | 3 次尝试失败并记录 `delivery_failed` |
| 超时 | 3 次失败；恢复后的新事件到达 |
| 队列满 | 记录 `audit_webhook_queue_full`，业务请求继续 |
| 只读拒绝审计 | `result=denied` 到达 |
| SIGTERM 排空 | 20 条已入队记录全部到达 |
| 泄露扫描 | `token=0`、`hmac=0`、`secret=0` |

**回滚与恢复**

证据：
`/var/lib/dosh/evidence/0be6df2586a3/20261102T174350Z-rollback-gate/oidc-rollback-evidence.log`。

revision 链路为 `72 (baseline) → 73 (upgrade) → 74 (rollback to 72) → 75 (restore to 73)`。
每个 revision 均通过 `/healthz=200`、`/readyz=200`、viewer `read200/write403`、operator
`target200/other403`、admin `scale200/nodes200`，logout 后 `/api/v1/session=401`；最终 Pod
恢复到当前 candidate digest，生产 `dosh` 未变化。

**门禁结论**

| 需求/门禁 | 当前 digest 结论 | 仍缺 |
|---|---|---|
| Kubernetes 429/5xx 重试 | 实验室已验收 | 生产或预发同等故障注入 |
| Metrics 三态 | 实验室已验收 | 目标生产 Metrics 拓扑 |
| 审计 Webhook/SIGTERM | 实验室已验收（测试 SIEM 接收端） | 真实 SIEM/Loki/OpenSearch |
| OIDC rollback/restore | 实验室已验收 | 生产 digest 发布与回滚 |
| 30 分钟 soak | 待复跑 | 当前 digest 的 0 错误、0 流失败、重连和 RSS 曲线 |
| 主题/axe/键盘 | 待真实 Release 复验 | 三浏览器、亮暗主题、0 serious/critical、键盘主路径 |
| 证据归档 | 原始日志齐备 | manifest、双层 SHA-256、离机 WORM/保留审批 |

### 21.2.14 UX 修复与本地复验（2026-11-03，尚未部署）

本节记录当前工作树相对已部署候选 `sha256:0be6df25…798ca8` 的增量。新镜像尚未推送，
因此以下结果只能证明源码和本地镜像，不能替代隔离 Release 证据。

| 项目 | 结果 |
|---|---|
| 真实 Release 首跑 | 新增 `web/e2e-oidc/ux.spec.ts`；暗色主题和键盘路径通过，axe 在概览页 Warning 徽标发现 `4.33:1` serious 对比度违规 |
| 根因与修复 | 浅色 `--warning` 从 `35 92% 33%` 调整为 `35 92% 30%`；对照计算为 `5.01:1`；mock fixture 增加 Warning 事件 |
| 键盘回归 | mock 新增登录→工作负载→扩缩容→退出主路径，三浏览器 3/3；真实 Release 套件已落盘 |
| Go 门禁 | `make verify`、`make race`、容量测试通过；P95 pods `20.54ms`、workloads `175.83ms`、overview `187.75ms`，Heap `28 MiB` |
| 前端门禁 | Vitest 14/14；mock E2E 45/45；LCP `528ms`；bundle `340.68 KiB` |
| 安全门禁 | govulncheck 0；npm production audit 0；Trivy Debian/Go binary 0 HIGH/CRITICAL |
| 本地镜像 | tag `0.1.3-rc-ux-20261103000038`；image ID `68e8fcbe63f55389c18ac4e5004e7b2b50c9d89a4be7d76f53ee150591b30dfb`；本地 manifest digest `sha256:ea1b2174…db6539`，不是可发布的 registry digest |
| 部署阻断 | 宿主 `enp6s0` 为 `NO-CARRIER`，`192.168.122.10:5000` 和 `192.168.122.50:16443` 超时；无法 push、读取 registry digest 或更新 Helm Release |
| 生产边界 | 生产 `dosh` 与隔离 Release revision 75 均未被本轮修改；无新证据可提升 UX-O-001/NFR-U-008 状态 |

恢复实验网络后，必须先推送该镜像并读取 registry `Docker-Content-Digest`，再在独立
namespace/Release/NodePort 上执行 `ux.spec.ts`、完整 OIDC 矩阵和 30 分钟 soak；不得复用
已部署旧 digest 的 UX 结果。

> 本节镜像已被 21.2.15 的安全修复候选取代；UX 对比度修复仍包含在新候选内，但本节不能证明
> 新 digest 的 Release 行为。

### 21.2.15 开放重定向修复、app 覆盖补强与本地候选（2026-11-03，未部署）

本节记录相对 `0be6df25…798ca8` 的增量。由于 Registry 不可达，所有结论只证明源码、本地测试和
本地镜像，不提升真实 Release 或商用状态。

| 项目 | 结果 |
|---|---|
| 安全缺陷 | `safeReturnTo` 原先只拒绝 `//`、CR/LF 和非 `/` 前缀，未拒绝反斜杠；浏览器会把 `/\evil.example` 规范化成跨站 URL |
| 修复 | 仅接受同源绝对路径；拒绝 `//`、反斜杠、绝对 URL、控制字符及解码后控制字符；保留合法 path/query/fragment |
| 负向回归 | 覆盖 `//evil.example`、`/\evil.example`、编码 CRLF、`https://evil.example`、`javascript:`、无前导斜杠；合法 `/pods?namespace=default#top` 保留 |
| 浏览器套件 | 现有 OIDC 开放重定向用例扩展为同一用例内验证 `//` 与 `/\` 两种输入，浏览器总数不增加 |
| app/domain 覆盖 | domain `100.0%`；overview `98.9%`；nodes/events/network/storage `100.0%`；configuration `92.9%`；yaml `80.9%` |
| 平台覆盖 | `httpserver 100.0%`（监听失败、TLS 错误、优雅关停）；`requestid 100.0%` |
| Go 门禁 | `make verify`、`make race` 通过；新增测试包含成功、依赖失败、权限拒绝和 dry-run 分支 |
| 前端门禁 | Vitest 14/14；mock E2E 45/45；typecheck 通过 |
| 安全扫描 | Trivy Debian 12.15 与 Go binary 均为 0 HIGH/CRITICAL；扫描时间 `2026-11-03T00:25:40Z` |
| 本地镜像 | tag `0.1.3-rc-sec-20261103002512`；image ID `80086aec0ef007d84b4cef65e8a418c263e003cc6987b263e47ff0be9f7b2e59`；本地 manifest digest `sha256:94978f73d9ff98ea7a05c9e9c765f1e9231f67ae82836b1d69c8b9bfb317b25a` |
| Registry 阻断 | `podman push` 20 秒超时；Registry `https://192.168.122.10:5000/v2/` 与 API `192.168.122.50:16443` 均不可达；`enp6s0` carrier 为 `0` |
| 生产边界 | 生产 `dosh`、隔离 Release revision 75、NodePort `32626/32627` 均未修改 |

后续执行顺序不可跳过：推送并记录 registry digest → 在隔离 namespace/Release/NodePort 部署当前
digest → 完整 OIDC 套件（33 条既有用例 + 9 条 UX，目标 42/42）→ 30 分钟 soak → 证据归档。

> 注意：本节镜像早于 21.2.16 的 `email_verified` 修复，不能作为当前源码的发布候选；其全量门禁
> 和 Trivy 结果只保留为历史记录，必须对新构建镜像重新执行。

### 21.2.16 OIDC email 验证修复与定向回归（2026-11-03，未部署）

| 项目 | 结果 |
|---|---|
| 需求 | `AUTH-O-006`：当 `DOSH_OIDC_USERNAME_CLAIM=email` 时，`email` 非空且 `email_verified` 必须显式存在并为 `true` |
| 设计 | `idTokenClaims.EmailVerified` 使用 `*bool` 表示三态；仅当指针非空且值为 `true` 时接受；避免把缺失字段误判为已验证 |
| 实现 | `internal/platform/oidc/provider.go` 在 username 映射前执行 fail-closed 校验，不创建会话、不进入 impersonation |
| 负向测试 | `provider_test.go` 覆盖 `email_verified=false`、缺失 `email_verified`、缺失 `email`；现有非法签名、issuer、audience、过期、nonce 和 JWKS 轮换用例保持通过 |
| 定向门禁 | `go test -race ./internal/platform/oidc ./internal/modules/auth/http ./internal/modules/auth/app -count=1` 通过：三个包均返回 `ok` |
| 全量门禁 | `make verify` 与 `make race` 通过；Vitest 14/14；三浏览器 mock E2E 45/45；E2E 类型检查通过 |
| 可访问性时序 | 首次 WebKit axe 扫描因 Vite 主题样式注入尚未完成而出现 1 个 flaky；新增 `expectThemeApplied`，等待 `dark` class、`color-scheme` 和 `--background` 值一致后再扫描；定向两轮 6/6、全量 45/45 通过 |
| 本地候选 | tag `0.1.3-rc-email-20261103004230`；image ID `sha256:074d6208881b5b9afe2a98fafd77266ee93ec70043b861eb3489adef16889d79`；本地 manifest digest `sha256:6946452fb48d1058a76ca99faeafa3e9b84a61ef7a05c749c4058c4a9c2325be`；构建时间 `2026-11-03T00:42:41Z` |
| 安全扫描 | Trivy Debian 12.15 与 Go binary 均 0 HIGH/CRITICAL；扫描时间 `2026-11-03T00:42:42Z` |
| 候选边界 | 本地 manifest digest 不是 registry `Docker-Content-Digest`；必须推送并读取 registry 响应后才能部署 |
| 待执行 | registry push/digest、隔离 Release 42/42 浏览器矩阵、30 分钟 soak 和证据归档 |
| 生产边界 | 生产 `dosh`、隔离 Release revision 75、NodePort `32626` 均未修改；本项不得提升为“已验收” |

通过标准：email username claim 的任何缺失或未验证形态均不能建立会话；定向与全量 race 测试通过；
新镜像以 registry digest 部署后，真实 IdP/测试 IdP 登录矩阵不回归；审计与日志不出现 Token。

### 21.2.17 v2.17 工作负载审计、HTTP adapter 与 CronJob 健壮性（2026-11-03，未部署）

> 该节镜像 `646a5d62…bdd0b` 已被 21.2.18 的依赖安全候选取代；本节保留源码修复和测试
> 证据，不得再作为当前发布身份。

本节记录相对已部署候选 `0be6df25…798ca8` 的最新源码增量。结论边界只有“源码已实现”和
“本地自动化已通过”；尚未取得 registry digest，也没有在隔离 Release 上执行本候选，
因此不得提升为“实验室已验收”或商用状态。

#### 21.2.17.1 实施级 EARS

| ID | EARS 条款 | 设计决策 | 验收断言 |
|---|---|---|---|
| `WL-N-005` | 如果扩容、重启或删除请求包含无效 kind、不支持的操作或非法删除策略，系统应返回稳定错误语义、写失败审计，且不得调用 Repository 写接口。 | 先校验工作负载 kind，再发起 SAR；不支持操作/非法策略在授权后校验；校验失败统一由 app 私有 `audit` 记录 `result=failure`；权限/只读错误才标记 `denied`。 | 无效 kind 不触发 SAR；DaemonSet/Job/CronJob 扩容、Job/CronJob 重启、非法 propagation 均产生审计；fake Repository 写次数为 0。 |
| `WL-N-006` | 如果工作负载没有 selector，系统仍应返回安全详情，selector 与 Pod 列表为空，且不得 panic。 | `selector` 保持 nil 检查；只有非 nil 时查询 Pod 与汇总重启数；CronJob 不猜测 Pod selector。 | 五种工作负载详情均返回；CronJob 无 selector 分支通过；未知 kind 返回 `ErrUnsupported`。 |
| `TST-U-003` | HTTP adapter 应使用 `httptest` 验证状态码、DTO 和错误语义，不得依赖 Kubernetes。 | Handler 仅负责参数解析、调用 app 端口和 DTO 编码；错误映射集中在模块 HTTP adapter，用 fake app 驱动。 | `nodes/overview/network/storage http` 覆盖成功、查询参数和 403/404/429/503/500/501；`go test -cover` 均为 `100%`。 |
| `SEC-U-004` | 全部写操作都应形成可检索的结构化审计，包括被业务校验拒绝的写请求。 | 审计由 app 层拥有，HTTP 不自行拼审计；失败动作保留 actor、action、resource、namespace、name、result 和 error。 | 审计顺序与写接口调用矩阵一致；拒绝写操作不改变资源。 |
| `TST-U-002` | Kubernetes adapter 应以 fake client 验证对象转换、错误映射和写操作。 | 资源聚合与详情留在 `platform/kube`；测试使用 fake clientset 构造五种工作负载和 Metrics/discovery fixture。 | 列表聚合、排序、搜索、重启数、状态、详情、重启/删除支持矩阵和 CNI/version 失败分支通过。 |

#### 21.2.17.2 逐步骤实施与验收

| # | 步骤 | 设计与实施内容 | 验收命令或判据 | 当前结果 |
|---|---|---|---|---|
| 1 | 冻结候选范围 | 只处理工作负载审计缺口、只读 HTTP adapter 覆盖和 CronJob 详情缺陷；不调整 API 路径、OpenAPI 版本或前端交互。 | `make api-check`、`make version-check` | 通过；App/OpenAPI/Web `0.1.3`、Chart `0.1.5` 未变 |
| 2 | 修复失败审计 | 在 `internal/modules/workloads/app/service.go` 中，于无效 kind、不支持 scale/restart、非法删除策略分支调用统一审计函数；审计语义按错误类型区分 failure/denied。 | `go test ./internal/modules/workloads/app -run 'Delete|Restart|Scale|Audit' -count=1` | 通过；新增用例断言 Repository 零写入、审计结果正确 |
| 3 | 强化 app 用例矩阵 | 扩充 `service_test.go`：List/Get、Pod 列表/详情、重启、预览、删除策略、依赖错误和五类资源权限映射；`deployment_validation_test.go` 覆盖向导 DNS label、镜像、资源、环境变量和标签边界。 | `go test -cover ./internal/modules/workloads/app` | 通过，行覆盖 `99.4%` |
| 4 | 补齐只读 HTTP adapter | 为 nodes/overview/network/storage 分别建立 `httptest` Handler 测试；使用 fake app 端口验证参数、成功 DTO 和错误查询到 `domain` 错误的映射。 | `go test -cover ./internal/modules/{nodes,overview,network,storage}/http` | 四个包均 `100%` |
| 5 | 修复 CronJob 详情 | `platform/kube.GetWorkload` 保留可选 selector；只有 selector 非 nil 时才查询 Pod/重启数，CronJob 返回空 selector；未知 kind fail-closed。 | `go test ./internal/platform/kube -run 'Workload|CronJob' -count=1` | 通过；五种工作负载详情、无 selector CronJob、未知 kind 均覆盖 |
| 6 | 扩展 adapter 回归 | 补充五类工作负载列表聚合、排序、搜索、重启数、状态推导、重启/删除支持矩阵；补充 client/version/CNI/discovery、Pod 日志流和 ConfigMap 详情错误映射。 | `go test -cover ./internal/platform/kube` | 通过，行覆盖 `78.4%` |
| 7 | 执行完整源码门禁 | 格式、vet、全量测试、race、前端类型检查/构建、架构边界、OpenAPI 对账、Helm lint/render 和版本一致性。 | `make verify`、`make race` | 通过；无新增架构违规或契约漂移 |
| 8 | 执行前端门禁 | 运行 Vitest、Chromium/Firefox/WebKit mock E2E 和 bundle budget，确认本轮后端修复未破坏 UI。 | `make web-test`、`make web-e2e-container`、`make web-bundle-check` | Vitest 14/14；mock E2E 45/45；bundle `340.68 KiB`，低于 500 KiB |
| 9 | 构建并扫描不可变候选 | 以固定 App 版本、`Commit=unknown` 和构建时间生成镜像；扫描 Debian 与 Go binary；记录 image ID 和本地 manifest digest。 | `make image TAG=<tag>`、`make image-scan TAG=<tag>`、`podman image inspect` | tag `0.1.3-rc-commercial-20261103005821`；image ID `212c24e2…4bf0bb`；0 HIGH/CRITICAL |
| 10 | 固定发布身份 | 本地 manifest digest 不能替代 registry digest；必须推送后读取 registry manifest 的 `Docker-Content-Digest`，并让 Helm `image.digest` 与之绑定。 | `podman push --tls-verify=false <image>`；`curl -ksSI .../manifests/<tag>` | 阻塞：`192.168.122.10:5000` 超时，尚未取得 registry digest |
| 11 | 隔离 Release 复验 | 在独立 namespace/Release/NodePort 部署 registry digest；执行 33 条既有 OIDC 用例 + 9 条 UX，共三浏览器 42/42；检查审计结果和生产快照不变。 | `DOSH_E2E_IMAGE_TAG=<tag> DOSH_E2E_IMAGE_DIGEST=<registry-digest> bash scripts/e2e-oidc-browser.sh` | 未执行；网络不可达，不得复用旧 digest 的 33/33 结果 |
| 12 | 长连接与故障门禁 | 对当前 registry digest 执行 30 分钟 soak、日志/事件断线重连、Pod 删除和滚动重启；记录 RSS、错误率、流失败和重连次数。 | `DOSH_E2E_IMAGE_TAG=<tag> DOSH_E2E_IMAGE_DIGEST=<registry-digest> bash scripts/e2e-soak.sh` | 未执行；旧候选 soak 仅作历史参考 |
| 13 | 证据归档 | 按 digest 归档 42/42、soak、审计、构建和扫描输出；manifest 覆盖 `summary.txt`，manifest 摘要外锚；排除 token/cookie/secret。 | `make evidence-check`、`sha256sum -c manifest.sha256`、`sha256sum -c manifest-digest.txt` | 未执行；必须先完成第 10–12 步 |
| 14 | 商用放行判断 | 当前候选只有在第 10–13 步通过，且第 26 节企业 IdP、受信 TLS、真实 SIEM、Edge/Safari、CI 真跑、离机 WORM 关闭后，才允许进入生产发布。 | 第 25.10 节核对单与第 26.9 节矩阵 | 未达到；不得宣称“完整可商用” |

#### 21.2.17.3 本地证据快照

| 证据 | 结果 |
|---|---|
| Go 覆盖率 | domain `100.0%`；workloads app `99.4%`；overview app `98.9%`；nodes/network/storage/events app `100.0%`；kube `78.4%`；config `87.5%` |
| HTTP adapter | nodes/http `100.0%`；overview/http `100.0%`；network/http `100.0%`；storage/http `100.0%` |
| 后端门禁 | `make verify`、`make race` 通过；架构、OpenAPI、Helm 和版本一致性门禁通过 |
| 前端门禁 | Vitest 14/14；Chromium/Firefox/WebKit mock E2E 45/45；bundle 初始 JS gzip `340.68 KiB`；生产构建通过 |
| 候选镜像 | `192.168.122.10:5000/dosh/dosh:0.1.3-rc-commercial-20261103005821`；image ID `sha256:212c24e2…4bf0bb`；创建时间 `2026-11-03T00:58:33Z`；本地 manifest digest `sha256:646a5d62…bdd0b` |
| 漏洞扫描 | Trivy `2026-11-03T00:58:58Z`；Debian 12.15 与 Go binary 均 0 HIGH/CRITICAL；secret scanner 无报告项 |
| 网络状态 | `enp6s0` `DOWN/NO-CARRIER`；`192.168.122.10:5000/v2/` 与 `192.168.122.50:16443` 均连接超时；`wlp3s0` 的 `192.168.1.66/24` 不能路由到实验网 |
| 生产边界 | 生产 `dosh` App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626` 未修改；隔离 release revision 75 未修改 |

#### 21.2.17.4 门禁判定

- `WL-N-005`、`WL-N-006`、`TST-U-002`、`TST-U-003` 和 `SEC-U-004` 的本地实现与自动化
  验收已通过，状态为“源码已实现、本地测试通过”。
- 计数类证据不能跨候选继承：旧 digest 的 33/33、soak、审计和回滚结果不能证明当前
  `646a5d62…bdd0b` 的行为。
- 进入隔离 Release 前，必须先把 registry 返回的 digest 写入 Helm 和证据台账；本地
  manifest digest、image ID 和可变 tag 都不是发布身份。
- 若网络恢复后候选构建或扫描结果变化，必须重新从头执行第 7–10 步，并在本节追加新快照，
  不得覆盖本节历史。

### 21.2.18 v2.18 依赖安全加固与全量本地门禁（2026-11-03，未部署）

本节记录相对 21.2.17 的依赖安全增量。它改变最终二进制和镜像层，因此必须重新构建、扫描和
复跑全量门禁；旧的 `646a5d62…bdd0b` 不能作为当前候选。

#### 21.2.18.1 实施级 EARS

| ID | EARS 条款 | 设计决策 | 验收断言 |
|---|---|---|---|
| `SEC-N-005` | 如果发布二进制存在可达 Go 漏洞、生产前端依赖存在 HIGH/CRITICAL，或镜像存在未接受 HIGH/CRITICAL，候选应停止发布。 | Go 依赖使用 `govulncheck` 判定“可达符号”；前端生产依赖和完整 dev 依赖分别审计；镜像使用固定 Trivy 镜像。 | `govulncheck` 0 可达漏洞；生产 npm 0；镜像 Debian/Go binary 0 HIGH/CRITICAL；dev 工具链风险单独登记。 |
| `TST-U-008` | 发布前应执行 Go、npm 和镜像三层安全扫描。 | 依赖升级后必须重跑测试、race、容量和构建，不能只相信扫描结果。 | `make verify`、`make race`、`make capacity-test`、`make web-e2e-container` 全通过。 |
| `NFR-U-005` | 初始 JavaScript 资源 gzip 后应小于 500 KiB。 | 仅升级 Go 间接依赖，不改前端代码；仍执行 bundle budget 防止镜像构建引入回归。 | `make web-bundle-check` 报告 `340.68 KiB`。 |
| `NFR-U-004` | 局域网首屏 LCP 应小于 2.5 秒。 | 用生产构建和 mock API 在 Chromium 测量 LCP，不依赖集群持续可用。 | 当前 LCP `472ms`。 |

#### 21.2.18.2 逐步骤实施与验收

| # | 步骤 | 设计与实施内容 | 验收命令或判据 | 当前结果 |
|---|---|---|---|---|
| 1 | 建立漏洞基线 | 使用 `govulncheck v1.9.0 -show verbose` 扫描所有根包；区分“可达漏洞”和“模块存在但未调用的漏洞”。 | `GOPROXY=https://goproxy.cn,direct go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 -show verbose ./...` | 初始 0 可达漏洞；模块级发现 `x/net` 2 条、`x/crypto` 1 条 |
| 2 | 升级 `x/net` | 将 `golang.org/x/net` 从 `v0.60.0` 升级到修复版本 `v0.62.0`，关闭 `GO-2026-6866` 和 `GO-2026-6860`。 | `go get golang.org/x/net@v0.62.0`、`go mod tidy` | 已升级；`go.sum` 同步 |
| 3 | 升级 `x/crypto` | 将 `golang.org/x/crypto` 从 `v0.57.0` 升级到 `v0.58.1-0.20261009181718-2f1f834330b2`，关闭 `GO-2026-5932` 模块级告警。 | `go get golang.org/x/crypto@v0.58.1-0.20261009181718-2f1f834330b2` | 已升级；Argon2/密码相关测试通过 |
| 4 | 复核 Go 漏洞 | 重跑完整符号扫描，确认无任何模块漏洞告警。 | 同第 1 步 | `No vulnerabilities found` |
| 5 | 前端依赖审计 | 生产依赖使用官方 registry 审计；完整依赖审计单独记录工具链风险。 | `npm audit --omit=dev --audit-level=high --registry=https://registry.npmjs.org`；`npm audit --registry=https://registry.npmjs.org` | 生产 `0 vulnerabilities`；完整审计 3 moderate（dev-only minimatch） |
| 6 | 源码门禁 | gofmt、vet、单测、race、类型检查、构建、架构、OpenAPI、Helm 和版本一致性；补充 capabilities Handler 会话/命名空间测试。 | `make verify`、`make race` | 通过；auth/http 行覆盖 `80.9%` |
| 7 | 容量与性能 | 1000 Pod / 200 工作负载容量门禁；生产构建 LCP 和 bundle budget。 | `make capacity-test`、`make web-performance`、`make web-bundle-check` | P95 `19.052/175.893/189.160ms`；Heap `28 MiB`；LCP `472ms`；bundle `340.68 KiB` |
| 8 | 前端回归 | Vitest 与三浏览器 mock E2E。 | `make web-test`、`make web-e2e-container` | Vitest 14/14；Chromium/Firefox/WebKit 45/45 |
| 9 | 重建候选 | 依赖变化后重新构建二进制和镜像，记录 tag、image ID、创建时间和本地 manifest digest。 | `make image TAG=<tag>`；`podman image inspect` | tag `0.1.3-rc-secdeps-20261103011313`；image ID `31464404…421d26`；本地 manifest `835314ae…d7e239` |
| 10 | 镜像扫描 | 对同一镜像扫描 Debian 12.15、Go binary 和 secret。 | `make image-scan TAG=<tag>` | 0 HIGH/CRITICAL；无 secret 报告项 |
| 11 | 固定发布身份 | 推送后读取 registry `Docker-Content-Digest`；Helm 使用 digest；不得用本地 manifest 或 image ID 代替。 | `podman push`、registry manifest HEAD | 阻塞：实验网不可达；仍未取得 registry digest |
| 12 | 隔离 Release 与商用门禁 | 在独立 Release 完成 42/42、soak、回滚、审计和证据归档；再关闭企业 IdP/TLS/SIEM/Edge/Safari/CI/WORM。 | 25.10 第 4–12 项 | 42/42、soak、回滚、Metrics、retry、动态上下文、审计、CronJob 详情和 15 文件受控归档已通过；外部商用门禁未完成 |

#### 21.2.18.3 证据快照

| 证据 | 结果 |
|---|---|
| Go 依赖版本 | `golang.org/x/net v0.62.0`；`golang.org/x/crypto v0.58.1-0.20261009181718-2f1f834330b2` |
| Go 漏洞 | `govulncheck v1.9.0`：`No vulnerabilities found`；0 可达、0 package、0 module 告警 |
| npm 生产依赖 | 官方 registry：`found 0 vulnerabilities` |
| npm 完整依赖 | 3 moderate，均为 `openapi-typescript → @redocly/openapi-core → minimatch` 构建工具链；不进入生产 bundle |
| 后端门禁 | `make verify`、`make race`、`make capacity-test` 通过；auth/http `80.9%`；无 race，无架构/契约/Helm/版本漂移 |
| 性能 | pods P95 `19.052ms`；workloads P95 `175.893ms`；overview P95 `189.160ms`；Heap `28 MiB`；LCP `472ms` |
| 前端门禁 | Vitest 14/14；三浏览器 mock E2E 45/45；bundle `340.68 KiB` |
| 镜像 | `0.1.3-rc-secdeps-20261103011313`；ID `sha256:31464404…421d26`；创建时间 `2026-11-03T01:13:28Z`；本地 manifest `sha256:835314ae…d7e239` |
| 镜像扫描 | Trivy `2026-11-03T01:13:34Z`：Debian 12.15 与 Go binary 均 0 HIGH/CRITICAL |
| 网络 | `enp6s0` DOWN/NO-CARRIER；registry 和 Kubernetes API 均超时；只能完成本地证据 |
| 生产边界 | 生产 `dosh` 与隔离 Release revision 75 均未修改 |

#### 21.2.18.4 判定

- `SEC-N-005`、`TST-U-008`、`NFR-U-004` 和 `NFR-U-005` 的本地门禁通过；状态为“源码与
  本地镜像已验收”，不是 Release 已验收。
- dev 工具链的 3 个 moderate 继续保留为已知风险。生产依赖 0 漏洞不等于完整工具链 0 漏洞；
  禁止在发布结论中混写。
- 因为依赖变化改变了二进制，21.2.17 的镜像 ID、本地 manifest digest 和 Trivy 结果只保留
  历史价值，不能继承给本候选。
- 以上是 v2.18 当时的状态记录。v2.19 已推送后续候选并取得 registry digest，见 21.2.19；
  revision 80 的隔离 Pod 启动阻断已修复，当前 revision 81 为 1/1 Ready，不得把本节的历史
  “网络恢复后”动作继续当作现状。

### 21.2.19 v2.19 可访问性、Playwright 主机隔离与候选阻断（2026-11-03）

本节记录当前候选 `0.1.3-rc-a11y2-20261103093530` 的可访问性修复、Playwright 主机保护、
真实 Release 定向复验和宿主冻结调查。候选的 registry digest 为
`sha256:45090acfc99e96cbf40d7a2808736c6dbdc043feee3d8a69164917fc0bf680ab`，不得用
image ID、本地 manifest digest 或可变 tag 替代发布身份。

#### 21.2.19.1 实施级 EARS

| ID | EARS 条款 | 设计决策 | 验收断言 |
|---|---|---|---|
| `TST-N-002` | 如果以容器运行 Playwright，系统应禁止与宿主共享 IPC，并配置单 worker、每浏览器独立容器、内存/CPU/PID/shm 上限和硬超时；任一限制缺失时测试门禁应失败。 | 用统一 runner 封装 `podman run`；每个浏览器单独启动；使用私有 shm 和 cgroup 限制；静态检查禁止 `--ipc=host`，并检查两个 Playwright 配置的 `workers: 1`。 | `make playwright-check` 输出 `playwright isolation checks: pass`；dry-run 命令包含全部资源参数；未通过检查时 `make verify` 失败。 |
| `NFR-U-008` | 当用户仅使用键盘或辅助技术操作共享表格、登录和核心运维页时，系统应提供可聚焦的滚动区域、可见焦点、可读名称和 WCAG AA 对比度。 | 表格滚动容器增加 `role="region"`、`aria-label`、`tabIndex=0` 和 focus ring；浅色 muted 文本提高对比度；用 axe、键盘 E2E 和组件测试共同验收。 | 表格组件测试通过；三浏览器 axe 0 serious/critical；键盘主路径 3/3 通过；真实 Release 定向矩阵达到 9/9 后才能关闭。 |
| `TST-U-006` | 系统应使用 Playwright 覆盖登录、总览、节点、工作负载、日志、受控写操作和 OIDC 退出主路径。 | mock 套件与真实 Release 套件分离；真实套件绑定 registry digest 和隔离 Release。 | 当前源码 mock 45/45、真实 UX 9/9、完整 OIDC 42/42 均已通过。 |
| `SEC-N-005` | 如果发布二进制存在可达 Go 漏洞、生产前端依赖存在 HIGH/CRITICAL，或镜像存在未接受 HIGH/CRITICAL，候选应停止发布。 | 依赖升级后重建镜像；Trivy 同时扫描 Debian 与 Go binary；发布使用 registry digest。 | `govulncheck` 0；生产 npm 0；Trivy Debian 12.15 与 Go binary 0 HIGH/CRITICAL。 |

#### 21.2.19.2 逐步骤实施与验收

| # | 步骤 | 设计与实施内容 | 验收命令或判据 | 当前结果 |
|---|---|---|---|---|
| 1 | 建立事故时间线 | 读取 boot 列表、冻结前 boot 尾部、coredump 和硬件资源；区分“硬冻结”“有序重启”和“浏览器进程崩溃”。 | `journalctl --list-boots`；`journalctl -b -2 -o short-iso`；`journalctl -b -1`；`coredumpctl list` | 2026-11-03 09:39:01 最后一条日志为 WebKit 定向套件启动；随后 boot 结束，没有 shutdown、OOM 或 panic 记录。01:16:06 另有一条 `WPEWebProcess` SIGSEGV；19:44–20:46 为另一 boot，20:46 由 `systemd-reboot.service` 有序重启。 |
| 2 | 评估并发假设 | 对照 2 worker 峰值资源和宿主容量；检查旧配置的实际 worker 数。 | 容器峰值与 `free -h`、`nproc` 对比；读取 Playwright 配置 | 两 worker 峰值约 `1.6 GiB`，宿主 `124 GiB`、`36` 核；旧 OIDC 配置已是 1 worker，mock 配置为 2。没有 OOM 证据，无法证明“并发过大”是主因。 |
| 3 | 收紧测试资源边界 | 新增统一 runner：默认单 worker、逐浏览器容器、`6g` 内存、`4` CPU、私有 `1g` shm、`2048` PID、`ulimit core=0`、20 分钟硬超时；默认不共享宿主 IPC。 | `sed -n '1,220p' scripts/run-playwright-container.sh`；`make playwright-check` | runner 和静态门禁已实现；`make playwright-check` 输出 `playwright isolation checks: pass`。 |
| 4 | 统一 worker 配置 | mock 与 OIDC Playwright 配置均设置 `fullyParallel: false`、`workers: 1`；OIDC 保留 `forbidOnly` 和失败证据。 | `rg -n 'workers: 1|fullyParallel' web/playwright*.config.ts`；`make playwright-check` | 两个配置均固定为单 worker；静态检查通过。 |
| 5 | 修复共享表格可访问性 | 为可滚动表格容器增加 `region` 语义、可读名称、键盘焦点和焦点环；新增 Vitest 回归。 | `make web-test`；检查 `web/src/components/ui/table.tsx` 与 `table.test.tsx` | 组件测试通过，Vitest 总计 15/15。 |
| 6 | 修复对比度 | 将浅色 `--muted-foreground` 从 `218 11% 45%` 调整为 `218 11% 42%`，消除 axe 的 `4.44:1` 失败。 | 三浏览器真实 Release axe 用例 | 三浏览器 axe 3/3 通过，0 serious/critical。 |
| 7 | 修复 WebKit 清理导航 | 不再用页面导航执行 logout；改用共享浏览器上下文的 API 请求，断言 302 后打开 `/login`；主题用例超时提高到 60 秒。 | `cd web && npm run typecheck`；WebKit 定向与完整矩阵复跑 | 类型检查通过；WebKit UX 3/3，完整矩阵 14/14 通过。 |
| 8 | 本地质量门禁 | 执行格式、vet、单测、race、架构、契约、Helm、前端单测、类型检查和 bundle budget。 | `make verify`、`make race`、`make web-test`、`make web-bundle-check` | `make verify` 通过（含 `playwright-check`）；`make race` 通过；Vitest 15/15；bundle `340.71 KiB`，低于 500 KiB。 |
| 9 | 构建并扫描候选 | 重建镜像，记录 image ID、本地 manifest 和创建时间；对同一镜像执行 Trivy。 | `make image TAG=0.1.3-rc-a11y2-20261103093530`；`make image-scan TAG=...`；`podman image inspect` | image ID `sha256:7a109763…11c01f`；本地 manifest `sha256:5b027290…9063e3`；创建时间 `2026-11-03T01:34:53Z`；Debian 12.15 与 Go binary 0 HIGH/CRITICAL。 |
| 10 | 固定 registry 身份并部署 | 推送后读取 `Docker-Content-Digest`；在隔离 Release 使用 digest 部署，不触碰生产。 | `curl -ksSI -H 'Accept: application/vnd.oci.image.manifest.v1+json' .../manifests/<tag>`；`helm list -A` | registry digest `sha256:45090acfc99e96cbf40d7a2808736c6dbdc043feee3d8a69164917fc0bf680ab`；`dosh-e2e-oidc` revision 81、1/1 Ready、NodePort `32627`；生产仍为 revision 12。 |
| 11 | 运行真实 Release UX 定向 | 在当前 digest 的隔离 Release 上执行主题、axe 和键盘用例，不使用生产 `32626`；每个浏览器独立受限容器、单 worker。 | `DOSH_PLAYWRIGHT_CONFIG=playwright.oidc.config.ts DOSH_PLAYWRIGHT_HOST_NETWORK=1 DOSH_E2E_BASE_URL=https://192.168.122.100:32627 DOSH_E2E_TARGET_NAMESPACE=dosh-e2e-target bash scripts/run-playwright-container.sh e2e-oidc/ux.spec.ts --project=<browser>` | 9/9：Chromium、Firefox、WebKit 的主题、axe 和键盘用例各 3/3 通过。 |
| 12 | 恢复隔离部署 | 对 `worker-0003` 上 revision 80 的启动失败取证：比较镜像内容层与节点解包快照；缩容到 0，清除损坏镜像/快照并重拉同一 digest，再扩容并重装夹具。 | `crictl inspecti`、层内文件大小/SHA-256、节点快照文件大小；`helm -n dosh-e2e-oidc list`；`kubectl -n dosh-e2e-oidc get pod -o wide` | 内容层二进制 30,699,682 字节且哈希一致；`worker-0003` 快照为 0 字节。缩容、删除损坏镜像/快照并重拉后恢复；revision 81 调度到 `worker-0005`，1/1 Ready。 |
| 13 | 运行完整真实矩阵 | UX 通过后，三浏览器分别启动独立受限容器，顺序执行 `e2e-oidc` 全部 14 条；禁止并行或复用宿主 IPC。 | `DOSH_PLAYWRIGHT_CONFIG=playwright.oidc.config.ts DOSH_PLAYWRIGHT_HOST_NETWORK=1 DOSH_E2E_BASE_URL=https://192.168.122.100:32627 DOSH_E2E_TARGET_NAMESPACE=dosh-e2e-target DOSH_E2E_OIDC_STATE_TTL_MS=5000 bash scripts/run-playwright-container.sh` | Chromium 14/14、Firefox 14/14、WebKit 14/14，总计 42/42；每个容器 `6g` 内存、`4` CPU、私有 `1g` shm、`2048` PID、20 分钟硬超时；宿主全程正常。 |
| 14 | 修复 CI 与候选身份护栏 | CI 删除 `--ipc=host`，应用与本地 runner 相同的资源限制；静态检查覆盖 CI；所有真实集群 E2E 脚本必须转发 registry digest。 | `make playwright-check`；`make e2e-image-check`；`bash -n` 相关脚本 | 两项检查均通过；CI YAML 可解析；仍待 GitHub Actions 真跑。 |

#### 21.2.19.3 证据快照

| 证据 | 结果 |
|---|---|
| 宿主事故 | 09:39 WebKit 运行后硬冻结，无正常关机/OOM/panic 日志；01:16 `WPEWebProcess` SIGSEGV core dump；20:46 为有序 reboot |
| 容量对照 | 2 worker 容器峰值约 `1.6 GiB`；宿主 `124 GiB`、`36` 核；不能把并发认定为已证实主因 |
| runner 静态门禁 | `playwright isolation checks: pass`；每浏览器独立容器，单 worker，私有 shm，内存/CPU/PID/超时限制，禁止宿主 IPC |
| CI 资源隔离 | e2e 按 Chromium/Firefox/WebKit 拆为三个 matrix job，每个 job 独立容器；e2e/performance job 使用私有 shm、`6g` 内存、`4` CPU、`2048` PID、`ulimit core=0` 和 20 分钟超时；无宿主 IPC；actionlint 通过 |
| E2E 候选绑定 | `e2e image binding checks: pass`；rollback、soak、retry、Metrics、audit 均转发 registry digest |
| 候选源码 | 表格 `role="region"`/`aria-label`/`tabIndex=0`/focus ring；muted 对比度修复；Playwright API logout 修复 |
| 本地门禁 | `make verify`、`make race`、容量测试、Vitest 15/15、bundle `340.71 KiB`、govulncheck 0、Trivy 0 HIGH/CRITICAL |
| mock E2E | 当前源码已在隔离 runner 下复跑 45/45：Chromium、Firefox、WebKit 各 15/15，单 worker、私有 shm、cgroup 资源限制 |
| 完整真实矩阵 | 42/42：Chromium 14/14、Firefox 14/14、WebKit 14/14；使用当前 registry digest，最终隔离 Release revision 89 |
| 真实 UX | 9/9：主题 3/3、axe 3/3、键盘 3/3 |
| 候选身份 | image ID `sha256:7a109763…11c01f`；本地 manifest `sha256:5b027290…9063e3`；registry digest `sha256:45090acf…f680ab`；镜像配置 `linux/amd64` |
| 生产复核 | App `0.1.2` / Chart `0.1.4`、revision 12、2/2 Ready、NodePort `32626`、health/ready 200 |
| 隔离部署根因 | `worker-0003` 的镜像解包快照中 `/usr/local/bin/dosh` 为 0 字节；containerd 内容层完整，二进制 30,699,682 字节，SHA-256 `49971fa8…308fa1d`；重拉后恢复 |
| 隔离部署 | `dosh-e2e-oidc` 当前 revision 89、NodePort `32627`、Pod `1/1` Ready；生产 `dosh` 仍为 revision 12 |

#### 21.2.19.4 判定

- `TST-N-002` 的实现、静态门禁和真实运行均通过：隔离 runner 已完成三浏览器 UX 9/9 和完整
  OIDC 42/42，运行时宿主保持正常。
- `NFR-U-008` 的本地自动化通过，真实 Release 定向 9/9 和完整 42/42 均通过，可标记为实验室
  已验收；企业 IdP、Edge/Safari 实机和受信 TLS 仍需单独验收。
- 宿主冻结的根因仍未闭环。禁止在没有维护窗口、主机级监控和资源上限复验的情况下连续运行
  WebKit 或三浏览器矩阵。
- 当前候选的隔离 Pod 已恢复为 revision 81、1/1 Ready；启动阻断来自节点镜像解包快照中的
  0 字节二进制，不是镜像内容层或 `linux/amd64` 配置错误。当前源码 mock E2E 已通过 45/45；
  后续仍需完成 soak、回滚、审计和证据归档。
- 生产 `dosh` 未被本轮修改。当前商用门禁继续包括企业 IdP、受信 TLS、真实 SIEM、Edge/Safari、
  CI 真跑、离机 WORM、完整 Release 矩阵、soak、回滚和证据归档。

> 以上是 v2.19 当时状态。v2.21 已按同一 candidate digest 补齐 soak、回滚、Metrics、retry、
> 动态上下文、审计全矩阵和失败审计；最新结论见 21.2.20，不得继续把本节末尾的“待完成”当作现状。

### 21.2.20 v2.21 当前候选完成性证据补充（2026-11-03）

本节只记录当前候选 `0.1.3-rc-a11y2-20261103093530@sha256:45090acfc99e96cbf40d7a2808736c6dbdc043feee3d8a69164917fc0bf680ab`
在隔离 Release 上的补充验收；生产 `dosh` 未修改。

| 验收项 | 证据路径或命令结果 | 判定 |
|---|---|---|
| 完整 OIDC 浏览器矩阵 | `/var/lib/dosh/e2e-runs/20261103T140741Z-oidc-browser-final/browser-matrix.log`；Chromium/Firefox/WebKit 各 14/14，总计 42/42；Token/Secret 扫描通过 | 通过 |
| mock 浏览器矩阵 | `/var/lib/dosh/e2e-runs/20261103T141240Z-mock-browser-final/mock-browser-matrix.log`；三浏览器各 15/15，总计 45/45；每个浏览器独立受限容器 | 通过 |
| 30 分钟 soak | `/var/lib/dosh/e2e-runs/20261103T132855Z-soak/soak-evidence.log`；29 个事件、144,960 帧、28,413 行日志、5 次重连，`app_errors=0`、`stream_failures=0`、峰值 33 MiB | 通过 |
| 回滚与恢复 | `/var/lib/dosh/e2e-runs/20261103T140102Z-rollback/oidc-rollback-evidence.log`；revision `83 → 84 → 85 → 86`，每阶段 health/ready、viewer/operator/admin RBAC 和 logout 通过 | 通过 |
| Metrics 三态 | `/var/lib/dosh/e2e-runs/20261103T140310Z-metrics/metrics-evidence.log`；Available CPU `2149m` / 内存 `10672955392`，不可用降级为 0，恢复 CPU `2047m` / 内存 `10676428800` | 通过 |
| Kubernetes 429/5xx 重试 | `/var/lib/dosh/e2e-runs/20261103T140432Z-retry/retry-evidence.log`；429/500/502/503/504 各注入 2 次后恢复；持续 429 恰好 3 次；`Retry-After` 约 2097 ms；写请求仅 1 次且副本数不变；重置、取消和恢复通过 | 通过 |
| 动态集群上下文 | `/var/lib/dosh/e2e-runs/20261103T141725Z-dynamic-context-final/dynamic-context-matrix.log`；`dosh-ui-e2e` revision 3、2/2 Ready，Chromium/Firefox/WebKit 共 3/3 | 通过 |
| 审计 Webhook 全矩阵 | `/var/lib/dosh/e2e-runs/20261103T140144Z-audit-webhook/audit-webhook-evidence.log`；私 CA、Token/HMAC、瞬时/持续 503、超时、队列满、只读拒绝、20 条 SIGTERM 排空和泄露扫描通过 | 通过 |
| 失败审计 | `/var/lib/dosh/e2e-runs/20261103T143453Z-audit-webhook-isolated-idp/audit-webhook-evidence.log`；无效 Pod scale、非法 DaemonSet scale、非法 Job restart、非法 delete propagation 分别产生 failure 审计，目标 Deployment 不变，`resource_unchanged=true`、`audit_result=pass` | 通过 |
| 审计夹具端口隔离 | `scripts/e2e-audit-webhook.sh` 使用 `DOSH_E2E_AUDIT_IDP_PORT`（默认 `9445`）并与接收端 `9444`、OIDC `9443` 分离；复跑证据 `/var/lib/dosh/e2e-runs/20261103T143453Z-audit-webhook-isolated-idp/audit-webhook-evidence.log`，并行期间 `dosh-e2e-oidc` 1/1 Ready | 通过 |
| CronJob 无 selector 详情 | `make e2e-cronjob-detail`；证据 `/var/lib/dosh/e2e-runs/20261103T145200Z-cronjob-detail/cronjob-detail.log`。脚本在隔离 Release 创建 suspended CronJob 后 GET `/api/v1/workloads/cronjob/dosh-e2e-target/dosh-cronjob-detail-check` 返回 200、`selector={}`、`pods=[]`、`status=Suspended`，删除后再次 GET 为 NotFound | 通过（可重复） |
| 隔离环境恢复 | `dosh-e2e-oidc` revision 89、1/1 Ready；曾因审计夹具临时 IdP 的私 CA 不同导致重启失败，使用原测试 CA 恢复 | 已恢复，不提升产品结论 |

**判定**：当前候选在实验室隔离范围内已完成功能、RBAC、浏览器、长连接、故障恢复、回滚、
Metrics、retry、动态上下文、审计和本机受控证据归档。仍不得发布到生产：离机 WORM、企业 IdP、
受信公共 CA TLS、真实 SIEM/Loki/OpenSearch、Edge/Safari 实机、GitHub Actions 真跑与分支保护、
生产发布和生产回滚尚未完成。

### 21.2.21 v2.25–v2.27 新候选隔离门禁闭环（2026-11-04）

**候选身份与隔离边界**

| 项目 | 值 |
|---|---|
| candidate tag | `192.168.122.10:5000/dosh/dosh:0.1.3-rc-audit-https-20261103231526` |
| registry digest | `sha256:66874e749391b85f00f3c091a40917642deeb1b742b87bb4f999113ebb6fb264` |
| image ID | `sha256:a5fe8cd61bdf9ca9e4084b143b4c7194efc2b7fd6470501da463f845af7bdac0` |
| local manifest digest | `sha256:7899fa4b2df5e670ed3ceb99d58b1c448d083bdd3944e128f614733c604b2652`；不得冒充 registry digest |
| OIDC Release | `dosh-e2e-oidc/dosh-oidc-e2e`，revision 102，NodePort `32627`，1/1 Ready |
| 生产边界 | `dosh` 保持 App `0.1.2` / Chart `0.1.4` revision 12、2/2 Ready、NodePort `32626` |

**本轮修复**

1. `internal/platform/config/config.go` 对 `DOSH_AUDIT_WEBHOOK_URL` 复用 HTTPS 严格校验；`http://`
   配置在启动阶段失败，错误包含变量名和 `must use https`。
2. `deploy/helm/dosh/templates/deployment.yaml` 在审计 Webhook 启用时要求 URL 非空且以
   `https://` 开头；明文 URL 在 `helm template` 阶段失败。
3. `internal/platform/config/config_test.go` 与 `scripts/check-helm.sh` 增加明文 HTTP 负向断言；
   values 和 EARS 文档同步为强制 HTTPS。
4. 列表筛选 E2E 增加关键词、命名空间、类型、刷新、新标签页分享、前进/后退和空参数清理；
   真实隔离 Release 新增同一持久化抽样。命名空间和类型下拉补充可访问名称。
5. `make image-scan` 增加 `TRIVY_TIMEOUT`（默认 `20m`），避免慢速漏洞库下载被 5 分钟默认超时误判。
6. `scripts/e2e-oidc.sh` 增加夹具自检：启动前用 `ss` 拒绝已占用的 IdP 端口，启动后检查子进程
   存活，健康与 discovery 使用新生成 CA 的 `--cacert` 严格验证。首次 soak 因上次运行遗留的旧
   IdP 占用 `9443` 而失败；该失败未被计为产品通过，清理残留后第二轮完成 30 分钟采样。
7. `make image-scan` 支持 `TRIVY_GODEBUG=http2client=0`（默认）和显式 `TRIVY_DB_FLAGS`。本轮通过
   OCI artifact 预取漏洞库，使用 `--skip-db-update` 对同一候选镜像扫描，并记录数据库 digest。
8. `scripts/run-playwright-container.sh` 增加 WebKit 隔离许可，并修复多项目解析：无
   `--project` 时安全默认 Chromium/Firefox，显式多项目逐容器执行，WebKit 必须显式 opt-in；
   `scripts/check-playwright-isolation.sh` 增加 CI 逐浏览器 matrix、默认双容器和单项目容器断言。
9. `scripts/e2e-oidc-browser.sh` 按 `--project` 数量推导预期 callback failure 数，修复单浏览器
   运行被三浏览器合计 `18` 错误拒绝的问题；显式 `DOSH_E2E_EXPECTED_CALLBACK_FAILURES` 仍可覆盖。

**自动验收**

| 验收项 | 结果 | 证据 |
|---|---|---|
| 全量本地门禁 | `make verify` 通过；App `0.1.3` / Chart `0.1.5`；夹具修复后复跑 | `/var/lib/dosh/e2e-runs/20261103T232100Z-audit-https-url/make-verify-r2.log` |
| Race | `make race` 通过（同一候选源码） | `.../make-race.log` |
| 前端单测 | Vitest 15/15 | `.../web-test.log` |
| 配置负向 | `TestValidateAuditWebhook/non-HTTPS_URL` 通过 | `.../config-https-tests.log` |
| Helm 正负向 | `make helm-check` 通过；`http://siem.example.test/audit` 返回 `must be an absolute https:// URL` | `.../helm-check.log`、`.../helm-http-negative.log` |
| mock Chromium | 15/15，通过筛选 URL 刷新/分享/前进后退用例 | `.../mock-chromium.log` |
| 真实 Release Chromium OIDC | 15/15，含三身份、创建向导、七类负向登录、主题/axe/键盘和筛选 URL | `.../oidc-chromium.log` |
| mock Firefox | 15/15，与 Chromium 同一套件、单 worker 和受限容器 | `/var/lib/dosh/e2e-runs/20261103T162409Z-doc-scope-cleanup/mock-firefox.log` |
| 真实 Release Firefox OIDC | 15/15，含三身份、创建向导、七类负向登录、主题/axe/键盘和筛选 URL；独立 runner 容器 | `.../firefox-oidc-r2.log` |
| Chromium/Firefox denied 审计 | 每浏览器 callback failure `6/6`、denied login 审计 `6/6` 且均带 `request_id` | `.../chromium-oidc-r2/oidc-negative-audit.log`、`.../firefox-oidc-r2/oidc-negative-audit.log` |
| Chromium/Firefox Token 扫描 | 应用日志、ConfigMap/Deployment、Helm values、浏览器 URL/DOM/Web Storage/响应体均 0 命中 | `.../chromium-oidc-r2/token-leak-scan.log`、`.../firefox-oidc-r2/token-leak-scan.log` |
| 审计 Webhook 全矩阵 | 私 CA、Token/HMAC、成功/拒绝、四种失败审计、瞬时/持续 503、超时、队列满、只读拒绝、20 条 SIGTERM 排空和泄露扫描通过 | `/var/lib/dosh/e2e-runs/20261103T232100Z-audit-webhook-newcandidate/audit-webhook-evidence.log` |
| 30 分钟 soak | 30 事件、15,760 事件帧、28,395 行日志、0 创建失败、0 应用错误、5 次重连、0 流失败；总内存峰值 24 MiB、Pod 峰值 24 MiB、56 个采样 | `.../soak-r2/soak-evidence.log` |
| 夹具端口竞态 | 旧 IdP 占用 `9443` 时被启动前检查拒绝；旧失败与修复后通过证据均保留 | `.../fixture-port-preflight.log`、`.../soak-failed-r1-infra.log` |
| 回滚与恢复 | baseline 93 → upgrade 94 → rollback 95 → restore 96；每阶段 health/ready 200、三身份 RBAC 和 logout 通过 | `.../rollback/oidc-rollback-evidence.log` |
| Metrics 三态 | Available CPU `2429m` / 内存 `10650046464`；不可用降级为 0；恢复 CPU `2224m` / 内存 `10655862784` | `.../metrics/metrics-evidence.log` |
| Kubernetes 429/5xx retry | 429/500/502/503/504 各注入 2 次后恢复；持续 429 恰好 3 次；`Retry-After` 约 2093 ms；写请求仅 1 次且副本数不变；重置、取消和恢复通过 | `.../retry/retry-evidence.log` |
| CronJob 无 selector 详情 | GET 返回 200、`selector={}`、`pods=[]`、`status=Suspended`，删除后 NotFound；release revision 99 | `.../cronjob-detail-r2.log`、`/var/lib/dosh/e2e-runs/20261104T001400Z-cronjob-detail/` |
| Trivy | Debian 12.15 与 Go binary 均为 0 HIGH/CRITICAL；数据库更新时间 `2026-11-03T13:13:42Z`，artifact digest `sha256:2fd489b1…37ddc` | `.../trivy-r2.log`、`.../trivy-db-identity.txt` |
| Go/npm 依赖 | `govulncheck` 可达漏洞 0；生产 npm audit `found 0 vulnerabilities` | `.../govulncheck-r2.log`、`.../npm-audit-production.log` |
| 容量与前端预算 | Pod P95 `23.310769ms`、Workload P95 `183.850462ms`、Overview P95 `195.029127ms`、Heap 28 MiB；bundle 340.73 KiB；生产 LCP 532 ms | `.../capacity-test.log`、`.../bundle-check.log`、`.../web-performance.log` |
| 隔离健康检查 | `/healthz=200`、`/readyz=200`（Kubernetes ok，version 0.1.3） | `.../health-final.txt` |
| 生产隔离 | revision 12、App `0.1.2`、NodePort `32626`、2/2 Ready 均未变化 | `.../production-snapshot-final.txt` |
| 受控证据归档 | v7：35 文件，manifest SHA-256 `bc42ef92b684c5434d5b967e1870340c05edf4ea04c285b6e6e1e44c70abf07a`；v8 浏览器补充：34 文件，manifest SHA-256 `882acb50cab00cdbeaf971da6dddb9de1d3103bca46f574f6bbd082843aaa7e4`；均通过双层 SHA-256 和敏感扫描 | `/var/lib/dosh/evidence/66874e749391/20261103T161434Z-final-candidate-gates-v7-20261104/`、`/var/lib/dosh/evidence/66874e749391/20261103T164736Z-firefox-browser-evidence-v1-20261104/` |

**判定**：新 digest `66874e74…fb264` 的隔离门禁已闭环，包括 soak、回滚/恢复、Metrics、retry、
CronJob 详情、安全扫描、依赖审计、容量和 35 文件受控归档。首次 soak 的 IdP 端口竞态是测试夹具
缺陷，已修复并在第二轮通过，失败证据未删除。Chromium 与 Firefox 的 mock/真实 OIDC 均通过，
且各浏览器 `6/6` denied 审计与 token 扫描通过；单浏览器审计期望缺陷已修复。WebKit 因宿主冻结
调查未在本机重跑，必须移到独立 runner/CI。当前仍不得发布到生产：企业 IdP、受信公共 CA TLS、
真实 SIEM/Loki/OpenSearch、Edge/Safari 实机、GitHub Actions 真跑与分支保护、离机 WORM、生产发布
和生产回滚尚未完成。

### 21.2.22 v2.29 SameOrigin 修复与候选复验（2026-11-04）

**候选身份与隔离边界**

| 项目 | 值 |
|---|---|
| candidate tag | `192.168.122.10:5000/dosh/dosh:0.1.3-rc-origin-20261103170401` |
| registry digest | `sha256:03ac9628fe8b19dde1ed7a950de63db9795ec5fc04db6b66b62779f5fe30000c` |
| image ID | `sha256:623841f4d125d3b4d8eb30319b30b82af338b790d4fc64e38ebd5ae196a3d97c` |
| local manifest | `sha256:9e2182aa66a17c73c0add1f3d84082f71e853af668b9937c063a96aa8b8c6a0c`（不作为发布身份） |
| OIDC Release | `dosh-e2e-oidc/dosh-oidc-e2e`，revision 114，1/1 Ready，NodePort `32627` |
| 生产边界 | `dosh` 保持 App `0.1.2`、Chart `0.1.4`、revision 12、2/2 Ready、NodePort `32626` |
| 证据目录 | `/var/lib/dosh/e2e-runs/20261103T170848Z-origin-gate` |

**SEC-U-002 跨域写请求拦截**

| 场景 | 结果 | 证据 |
|---|---|---|
| 外站 `Origin` 写请求 | HTTP 403 | `same-origin-runtime.log` |
| `Sec-Fetch-Site: cross-site` 写请求 | HTTP 403 | 同上 |
| 同源 `Origin` 到达后续路由 | 通过中间件；无本地登录路由时为 405 | 同上 |
| 单元测试 | 同源、跨域、`null`、畸形 Origin、Fetch Metadata、CLI 和 GET 放行全部通过 | `internal/platform/httpx/httpx_test.go` |
| 兼容性边界 | 无浏览器 Origin 元数据的 CLI 请求允许；已认证写操作仍要求 CSRF | `internal/bootstrap/router.go`、`Makefile#verify` |

**候选门禁结果**

| 验收项 | 结果 | 证据 |
|---|---|---|
| 本地门禁 | `make verify`、`make race`、Vitest 15/15 通过 | `origin-verify.log`、`origin-race.log`、`origin-web-test.log` |
| 真实 OIDC 浏览器 | Chromium 15/15、Firefox 15/15；每浏览器 denied 审计 6/6 且带 `request_id`；token 扫描全部 PASS | `chromium-oidc.log`、`firefox-oidc.log`、各自 `oidc-negative-audit.log`、`token-leak-scan.log` |
| 审计 Webhook | 私 CA、Token/HMAC、业务成功/拒绝、四种失败审计、瞬时/持续 503、超时、队列满、只读拒绝、20 条 SIGTERM 排空、泄露扫描 0 命中；revision 10 | `audit-webhook/audit-webhook-evidence.log` |
| Metrics 三态 | Available CPU `2493m` / 内存 `10689167360`；不可用 `metricsAvailable=false`；恢复 CPU `2185m` / 内存 `10681876480` | `metrics/metrics-evidence.log` |
| Kubernetes retry | 429/500/502/503/504 各失败 2 次后恢复；持续 429 恰好 3 次；`Retry-After` 约 2103 ms；写请求仅 1 次且 replicas 2→2；重置、取消和恢复通过 | `retry/retry-evidence.log` |
| rollback/restore | revision 109→110→111→112；每阶段 health/ready、三身份 RBAC 和 logout 通过 | `rollback/oidc-rollback-evidence.log` |
| 30 分钟 soak | 29 个事件、64,260 SSE 帧、28,394 行日志、5 次重连、`app_errors=0`、`stream_failures=0`、峰值 30 MiB、首尾 23/21 MiB | `soak/soak-evidence.log` |
| CronJob 无 selector | HTTP 200、`selector={}`、`pods=[]`、`status=Suspended`、cleanup pass；revision 114 | `cronjob-detail.log` |
| 容量 | Pods P95 `21.016489ms`、Workloads P95 `189.099579ms`、Overview P95 `197.942489ms`，Heap 28 MiB | `capacity.log` |
| 依赖与镜像 | `govulncheck` 0；公共 npm registry `found 0 vulnerabilities`；Trivy Debian/Go binary 0 HIGH/CRITICAL | `govulncheck.log`、`npm-audit.log`、`origin-image-scan.log` |
| 宿主并发事故 | `01:56:28` 与 `09:39:01` 两次中断均在 1 worker；2 worker 的 42/42、45/45 运行正常完成；未发现 OOM/panic/lockup/vmcore | `journalctl -b -3`、`journalctl -b -2`、`docs/operations-runbook.md` |
| 受控证据归档 | 按 candidate digest 归档隔离门禁目录、SameOrigin 源码与文档；凭据、Cookie、session、fixture 证书和请求/响应转储被排除；双层 SHA-256 通过 | `/var/lib/dosh/evidence/03ac9628fe8b/`（manifest 摘要见归档内 `manifest-digest.txt`） |

**判定**：当前候选在隔离集群和 Chromium/Firefox/WebKit 受限 runner 范围内通过功能、授权、
跨域写拦截、审计、Metrics、retry、长连接、回滚、CronJob、容量和安全扫描。企业 IdP、受信公共
CA TLS、真实 SIEM、Edge/Safari 实机、GitHub Actions 真跑与分支保护、离机 WORM、生产发布和
生产回滚仍未验收，不能标记为完整商用放行。

### 21.2.23 v2.30 安全边界修复与本地复验（历史，已被 21.2.24 取代）

**源码与部署边界**

| 项目 | 值 |
|---|---|
| 当前源码 | App/OpenAPI/Web `0.1.3`，Chart `0.1.5` |
| 最近隔离候选（不含 v2.30 修复） | `0.1.3-rc-origin-20261103170401@sha256:03ac9628fe8b19dde1ed7a950de63db9795ec5fc04db6b66b62779f5fe30000c` |
| 隔离 Release | `dosh-e2e-oidc/dosh-oidc-e2e` revision 114，1/1 Ready，NodePort `32627` |
| 生产边界 | `dosh` 保持 App `0.1.2`、Chart `0.1.4`、revision 12、2/2 Ready、NodePort `32626` |
| 新镜像 digest | 未生成；不得用旧 digest 或本地 image ID 代替 |

**实施项**

| 需求/风险 | 实现 | 验收证据 | 结论 |
|---|---|---|---|
| AUTH-E-001 会话有效期 | Session TTL 上限 `8h`；超过上限配置启动失败 | `go test ./internal/platform/config`；全量 `make verify`、`make race` | 通过（组件） |
| TLS Cookie 安全 | 启用 TLS 时强制 `DOSH_COOKIE_SECURE=true` | `TestValidateRequiresSecureCookieWhenTLSIsEnabled`；全量门禁 | 通过（组件） |
| API 缓存防泄漏 | `/api/` 默认 `Cache-Control: no-store`；静态资源不注入 | `internal/platform/httpx` 单测；全量门禁；OpenAPI/类型无漂移 | 通过（组件） |
| 跨源隔离 | 新增 COOP/CORP `same-origin` | `internal/platform/httpx` 安全头单测；全量门禁 | 通过（组件） |
| OIDC logout 认证边界 | logout 移入受保护路由；未认证返回 401；协议同步 | `internal/modules/auth/http` OIDC 单测；`make api-check` | 通过（组件） |
| YAML 审计/应用一致性 | 拒绝空文档、多文档和重复键；仅接受单文档 | `internal/modules/yaml/app` 定向测试；全量 `make verify`、`make race` | 通过（组件） |
| 无效工作负载 kind | 先 `ValidateKind` 再 SAR；返回 `not_supported` 并失败审计 | `internal/modules/workloads/app` 定向测试；全量 `make verify`、`make race` | 通过（组件） |
| 前端与交付形态 | `npm run typecheck`、Vitest、生产构建、bundle 预算 | Vitest 15/15；bundle `340.73 KiB`；`make verify` | 通过（本地） |
| 证据归档门禁 | 当前工作区证据归档结构/摘要规则 | `make evidence-check` | 通过（本地） |

**验证命令结果**

| 命令 | 结果 |
|---|---|
| `make verify` | 通过：gofmt、vet、全量 Go 测试、TypeScript、Vite 构建、架构、OpenAPI、Helm、版本、Playwright 隔离、E2E digest |
| `make race` | 通过：全量 `go test -race ./...` |
| `cd web && npm test -- --run` | 15/15 通过 |
| `cd web && npm run bundle:check` | 初始 JS gzip `340.73 KiB`，低于 500 KiB |
| `bash -n scripts/*.sh` | 通过 |
| `make evidence-check` | 通过 |

**判定**：v2.30 的可在本地关闭的安全边界已修复并由组件/全量门禁证明，但尚未构建新镜像。
因此 Chromium/Firefox OIDC、soak、rollback、Metrics、审计 Webhook 等集群证据仍绑定旧 digest，
不能继承给当前源码。下一步必须先构建并推送新 tag，记录 registry digest，在隔离 Release 用
digest 部署并复跑浏览器、授权、审计和回滚矩阵，之后才可更新候选放行结论。

### 21.2.24 v2.31 最终候选、Playwright 隔离与全矩阵验收（2026-11-04）

**源码、镜像与部署边界**

| 项目 | 值 |
|---|---|
| 当前源码 | App/OpenAPI/Web `0.1.3`，Chart `0.1.5` |
| 最终候选 | `192.168.122.10:5000/dosh/dosh:0.1.3-rc-sec-20261104035000` |
| registry digest | `sha256:69f30f0016516ac9b56ff867f23c8c0039810dcff29179420c12da323cc00523` |
| 本地 image ID | `06927c828cd8759a0e6a51a4788fe874857de2a0abb92c979b6fda4683057e27`（不得作为发布身份） |
| 镜像内二进制 | `/usr/local/bin/dosh` SHA-256 `de3d1c5977f09cafd7df75665378b9cef8fa0314779831ce5dfe519863f386b3`，与工作区 `bin/dosh` 一致 |
| 源码绑定 | 183 个运行时文件；`source_tree_sha256=7a254dbd889c5b520c359ba8ffb54b2e7f50a259da95b2b8629b6955ce1ae38a`；`source-binding.txt` 绑定 tag、digest、image ID、构建日志和 digest 文件 |
| 隔离 Release | `dosh-e2e-oidc/dosh-oidc-e2e` revision 138，1/1 Ready，NodePort `32627`，镜像由 digest 固定；revision 137→138 的 Helm values 无差异 |
| 生产边界 | `dosh` 仍为 App `0.1.2`、Chart `0.1.4`、revision 12、2/2 Ready、NodePort `32626`；health/ready 200，未认证 overview 401 |

**自动化门禁**

| 命令/范围 | 结果 | 证据 |
|---|---|---|
| `make verify` | 通过：gofmt、vet、全量 Go 测试、TypeScript、Vite 构建、架构、OpenAPI、Helm、版本、Playwright 隔离、E2E digest | `.../v231-final/gates/verify.log` |
| `make race` | 通过：全量 `go test -race ./...` | `.../v231-final/gates/race.log` |
| `make capacity-test` | 通过：Pods P95 `20.271482 ms`、Workloads P95 `191.788973 ms`、Overview P95 `202.688339 ms`，heap 28 MiB | `.../v231-final/gates/capacity.log` |
| Vitest | 16/16 通过 | `.../v231-final/gates/web-test.log` |
| Vite bundle | 初始 JS gzip `340.72 KiB`，低于 500 KiB | `.../v231-final/gates/bundle-check.log` |
| mock E2E | Chromium 15/15、Firefox 15/15，均在受限容器内单 worker 执行；WebKit 15/15 在隔离 libvirt runner 执行 | `.../gates/mock-chromium-rerun.log`、`.../gates/mock-firefox.log`、`/var/lib/dosh/e2e-runs/20261104T053000Z-v231-browser-webkit/browser-webkit-mock.log` |
| 生产构建性能 | Chromium production LCP `468.0 ms`，低于 2500 ms | `.../gates/performance-chromium.log` |
| shell 语法与归档门禁 | `bash -n scripts/*.sh`、`make evidence-check` 通过 | `.../gates/evidence-and-shell.log` |
| 镜像扫描 | Debian 12.15 与 Go binary 均 0 HIGH/CRITICAL | `.../v231-final/trivy.log` |
| Go 漏洞 | `No vulnerabilities found.`；govulncheck DB 更新时间 `2026-11-02 22:01:43 UTC` | `.../gates/govulncheck.log` |
| 生产依赖审计 | 公共 npm registry `found 0 vulnerabilities` | `.../gates/npm-audit-prod.log` |

**真实隔离集群矩阵**

| 验收项 | 结果 | 证据目录 |
|---|---|---|
| Chromium 真实 OIDC | 15/15；每浏览器 6/6 denied 审计带 `requestId`；日志/配置/Helm values Token 扫描 0 | `/var/lib/dosh/e2e-runs/20261104T044800Z-v231-browser-chromium` |
| Firefox 真实 OIDC | 15/15；每浏览器 6/6 denied 审计带 `requestId`；日志/配置/Helm values Token 扫描 0 | `/var/lib/dosh/e2e-runs/20261104T045100Z-v231-browser-firefox` |
| WebKit 真实 OIDC | 15/15；每浏览器 6/6 denied 审计带 `requestId`；Token 扫描 0；隔离 libvirt runner | `/var/lib/dosh/e2e-runs/20261104T053000Z-v231-browser-webkit` |
| 审计 Webhook | 私 CA、Bearer/HMAC、双认证、业务成功/拒绝、四种失败审计、瞬时/持续 503、超时、队列满、只读拒绝、20 条 SIGTERM 排空、泄露扫描 0；revision 10 | `/var/lib/dosh/e2e-runs/20261104T035000Z-v231-audit-webhook` |
| CronJob 详情 | HTTP 200、`selector={}`、`pods=[]`、`status=Suspended`、清理通过；revision 127 | `/var/lib/dosh/e2e-runs/20261104T035000Z-v231-cronjob` |
| rollback/restore | revision `128 → 129 → 130 → 131`；每阶段 health/ready、三身份 RBAC、logout 通过 | `/var/lib/dosh/e2e-runs/20261104T035000Z-v231-rollback` |
| Metrics 三态 | 可用 CPU `2295m` / 内存 `10982703104`；不可用 `metricsAvailable=false`；恢复 CPU `2254m` / 内存 `10997035008` | `/var/lib/dosh/e2e-runs/20261104T035000Z-v231-metrics` |
| 429/5xx retry | 429/500/502/503/504 各 2 次后恢复；持续 429 恰好 3 次；`Retry-After` 2133 ms；写请求仅 1 次且副本不变；连接重置、取消和恢复通过 | `/var/lib/dosh/e2e-runs/20261104T035000Z-v231-retry-final` |
| 30 分钟 soak | 29 事件、68,952 事件帧、28,476 日志行、5 次重连、`app_errors=0`、`stream_failures=0`、首/尾 13/18 MiB、峰值 34 MiB | `/var/lib/dosh/e2e-runs/20261104T035000Z-v231-soak` |

**Playwright 宿主隔离与扩容契约修复**

| 项目 | 实现与验收 |
|---|---|
| 并发边界 | 三套 Playwright 配置均为 `workers: 1`、`fullyParallel: false`；runner 逐项目启动独立容器，宿主级 `flock` 禁止重叠运行 |
| 资源边界 | 每容器 `6g` 内存、`4` CPU、私有 `1g` shm、`2048` PID、`ulimit core=0`、20 分钟硬超时，禁止宿主 IPC |
| WebKit 边界 | 仅 `CI=1` 或 `DOSH_PLAYWRIGHT_ALLOW_WEBKIT=1` 时注册；宿主直接 `--project=webkit` 被拒绝；物理宿主仍只运行 Chromium/Firefox，WebKit 仅在隔离 libvirt runner 执行 |
| 扩容写请求 | `scaleWorkload` 只发送 `{replicas}`，不再固定页面加载时的 `resourceVersion`；YAML apply 仍保留版本保护 |
| 回归断言 | mock E2E 与集成测试均断言扩容请求体严格为 `{replicas}`，覆盖 Pod CrashLoop 导致 Deployment `resourceVersion` 持续变化时的成功路径 |

**隔离 WebKit runner 补充验收（2026-11-04）**

| 项目 | 实测值 |
|---|---|
| libvirt domain | `dosh-browser-runner`，8 GiB RAM、4 vCPU、Fedora 44 独立内核；与物理宿主浏览器运行时隔离，验收后已 `virsh destroy`，domain/disk/seed 保留 |
| 浏览器镜像 | `192.168.122.10:5000/dosh/playwright:v1.64.1-noble`，image ID `a0d7b2b49ffa0e99787a00b7ec2af46eca58020082713f002ca9f0f1cfd1a017` |
| 容器边界 | `memory=6g`、`cpus=4`、`shm=1g`、`pids=2048`、`ulimit core=0`、`timeout=20m`，禁用宿主 IPC |
| mock 命令 | `DOSH_PLAYWRIGHT_ALLOW_WEBKIT=1 DOSH_PLAYWRIGHT_IMAGE=192.168.122.10:5000/dosh/playwright:v1.64.1-noble bash scripts/run-playwright-container.sh --project=webkit --retries=0` |
| 真实 OIDC 命令 | `DOSH_PLAYWRIGHT_ALLOW_WEBKIT=1 DOSH_PLAYWRIGHT_CONFIG=playwright.oidc.config.ts DOSH_PLAYWRIGHT_HOST_NETWORK=1 DOSH_E2E_BASE_URL=https://192.168.122.100:32627 bash scripts/run-playwright-container.sh --project=webkit --retries=0` |
| 结果 | mock `15/15`、真实 OIDC `15/15`，均 `workers=1`、`retries=0`；每浏览器 6/6 denied 审计带 `requestId`；Token 扫描 0 |
| 测试修复 | `web/e2e/dosh.spec.ts`、`web/e2e-oidc/ux.spec.ts` 在 axe 前执行 `waitForVisualStability`，等待字体、有限动画和两帧 rAF；修复前定向 WebKit 仅 2/5 通过，修复后 5/5 通过 |
| 证据 | `/var/lib/dosh/e2e-runs/20261104T053000Z-v231-browser-webkit`；受控归档 `v231-webkit-final` |

宿主连续两次日志中断均发生在 `workers=1` 条件下，不能归因于 Playwright 并发。WebKit 在物理宿主上
仍可能触发主机级图形/内核故障，因此后续复验必须继续使用隔离 runner 或 CI，禁止在宿主直接启动。

**判定**：v2.31 最终候选在当前源码、隔离 Release 和允许的浏览器范围内完成全面复验；旧
`03ac9628…00000c` 证据不再使用。Chromium/Firefox/WebKit 的容器矩阵已关闭；WebKit 结果不等同
Safari 实机。企业 IdP、受信公共 CA、真实 SIEM、Edge/Safari 实机、GitHub Actions 真跑与分支保护、
离机 WORM、生产发布和生产回滚仍未验收，因此不得标记为完整商用。

### 21.2.25 v2.31 受控证据归档（本机完成，离机待办）

| 项目 | 值 |
|---|---|
| candidate digest | `sha256:69f30f0016516ac9b56ff867f23c8c0039810dcff29179420c12da323cc00523` |
| 归档目录 | `/var/lib/dosh/evidence/69f30f001651/20261103T205205Z-v231-final` |
| 文件数与大小 | 548 个文件，`55,330,841` bytes |
| manifest SHA-256 | `21d93f2dbca50424e5936180539331e84a914c043f9d1632c474b1560aa4de5b` |
| 双层校验 | `sha256sum -c manifest.sha256` 与 `sha256sum -c manifest-digest.txt` 均通过 |
| 排除与敏感扫描 | 103 个 fixture/header/body/session/certificate 类路径被排除；归档内再次扫描私钥、Bearer、Cookie，0 命中 |
| 原始文档绑定 | 原始 `documentation-binding.txt` 记录了归档生成时的文档哈希；归档后实施文档继续修订，其 ears 哈希已不是当前值，因此不得用原始 binding 证明当前文档 |
| 最终文档绑定 | `/var/lib/dosh/e2e-runs/20261104T035000Z-v231-final/documentation-binding-final.txt` 在文档最终修订后生成，覆盖 README、实施文档和运行手册的最终 SHA-256，并绑定同一 candidate digest |
| 补充归档 | 标签 `v231-final-docs` 归档最终 binding；标签 `v231-final-docs-anchor` 归档包含补充归档路径与 manifest 摘要的锚定副本，目录位于 `/var/lib/dosh/evidence/69f30f001651/` |
| WebKit 补充归档 | `/var/lib/dosh/evidence/69f30f001651/20261103T213047Z-v231-webkit-final`；15 个文件、`1,297,818` bytes，manifest SHA-256 `bc8718419fcd6a8f4b5f11135c028ce3bda847f331d7c594f49d1cc14143b25e`；`sha256sum -c manifest.sha256`、`sha256sum -c manifest-digest.txt` 和私钥/Bearer/Cookie/DOSH 敏感变量扫描均通过 |
| v2.32 runner 修复证据 | `/var/lib/dosh/evidence/69f30f001651/20261103T215305Z-v232-runner-fix`；9 个文件、`553,720` bytes，manifest SHA-256 `5cf7f4bd8f59239e8189a5fa6eb6183da4c742dda7fc7ac0ae80089c80dab18a`；包含默认/显式干跑、WebKit 拒绝、`make verify`、`make playwright-check`、Chromium/Firefox 各 15/15 原始日志和 Playwright 报告 |
| v2.32 文档绑定 | `/var/lib/dosh/e2e-runs/20261104T035000Z-v231-final/documentation-binding-v232-r2.txt` 重新绑定最终 v2.32 的 README、实施文档和运行手册 SHA-256；旧的 `documentation-binding-final.txt` 和中间 `documentation-binding-v232.txt` 保留为历史，不得证明最终 v2.32 文档 |
| v2.32 补充归档 | 标签 `v232-final-docs-r2` 归档最终 v2.32 binding；标签 `v232-final-docs-r2-anchor` 归档锚定副本，记录 base binding SHA-256 和补充归档 manifest；目录位于 `/var/lib/dosh/evidence/69f30f001651/` |
| 最终双层校验 | 原始归档、v2.31 两个补充归档、WebKit 归档、runner 修复归档和 v2.32 两个最终补充归档分别执行 `sha256sum -c manifest.sha256` 和 `sha256sum -c manifest-digest.txt`，均通过 |
| 文档凭据扫描 | 文档不直接进入证据目录，继续采用哈希绑定，避免 README 中的示例变量赋值触发凭据扫描；binding 文件及补充归档敏感扫描 0 命中 |
| 外部保留 | 未复制到企业 WORM/长期不可变存储，也未完成保留审批 |

**判定**：当前 digest 的本机受控证据包完整且可校验，最终文档修订由 `v231-final-docs` 和
`v231-final-docs-anchor` 补充归档闭合，WebKit 结果由 `v231-webkit-final` 补充归档，runner
修复由 `v232-runner-fix` 留证，最终 v2.32 文档由 `v232-final-docs-r2` 和
`v232-final-docs-r2-anchor` 重新绑定同一 candidate digest；离机 WORM、保留审批和证据副本异地
恢复演练仍是生产发布阻断项。

### 21.2.26 v2.33 真实 Loki 投递与检索验收（实验室完成，企业 SIEM 待办）

| 项目 | 值 |
|---|---|
| candidate digest | `sha256:69f30f0016516ac9b56ff867f23c8c0039810dcff29179420c12da323cc00523` |
| Loki 镜像 | `192.168.122.10:5000/dosh/loki:3.5.4`，内部 registry digest `sha256:98242d6a7f2403a1dc35ce256e906f7524a5d9aa166f22221c3aebb38d08643c` |
| 隔离对象 | namespace `dosh-e2e-loki`、`dosh-e2e-audit`；Release `dosh-audit-e2e` revision 10；NodePort `32628` |
| 运行目录 | `/var/lib/dosh/e2e-runs/20261103T221200Z-loki-r4` |
| 受控归档 | `/var/lib/dosh/evidence/69f30f001651/20261103T221524Z-v233-loki-final`；8 个文件、17,861 bytes；manifest SHA-256 `916db0ba1a23f297df3a1491dbb1a8117ad5e4c903070153094f8cc14e3a55a0` |
| 双层校验 | `sha256sum -c manifest.sha256` 与 `sha256sum -c manifest-digest.txt` 均通过；归档内私钥/Bearer/Cookie/DOSH 凭据变量扫描 0 命中 |
| 投递矩阵 | 私 CA、Bearer-only、HMAC-only、双认证、业务成功/拒绝、四类校验失败且资源不变、瞬时 503、持续 503、超时恢复、队列满、只读拒绝、20 条 SIGTERM 排空全部通过 |
| Loki 回查 | `loki_verified events=55 request_ids=34 labels=job/service tenant_isolation=isolated`；无租户查询返回 401；SIGTERM 的 20 个 requestId 全部命中 |
| 泄露扫描 | Token `0`、HMAC `0`、Secret `0` |
| 生产前后 | `dosh` App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626` 未变化 |

实现要点：`tools/auditreceiver` 使用 `AUDIT_LOKI_URL`、`AUDIT_LOKI_CA_FILE`、
`AUDIT_LOKI_TENANT` 和 `AUDIT_LOKI_TIMEOUT` 配置 Loki 后端；接收端在 Bearer/HMAC 校验通过后
推送 Loki，只有 Loki 2xx 才向 dosh 返回成功。`scripts/e2e-audit-webhook.sh` 在
`DOSH_E2E_AUDIT_BACKEND=loki` 时使用 TLS、租户鉴权、非 root、只读根文件系统和 4 GiB
`emptyDir` 启动 Loki，并通过 `kubectl port-forward` 完成真实写入、检索和隔离验证。

**判定**：v2.33 关闭“真实 Loki 可接收、可检索、可关联 requestId”的实验室门禁，不是企业 SIEM
生产验收。当前 Loki 使用私 CA、临时 token、`emptyDir` 和 24h retention，未证明长期持久化、
不可变保留、告警、访问审批、容量 SLA 或离机 WORM；这些仍是生产发布阻断项。

### 21.2.27 v2.34 节点 Cordon/Uncordon 与新候选 API 验收（2026-11-04）

| 项目 | 值 |
|---|---|
| 源码版本 | App/OpenAPI/Web `0.1.4`；Chart `0.1.6` |
| candidate digest | `sha256:0c61ce34873471d85e9d47b957be81de1a970eb166151477468a197045bc5bda` |
| 隔离对象 | Release `dosh-oidc-e2e`，namespace `dosh-e2e-oidc`，最终 revision 149，1/1 Ready，NodePort `32627`；原始浏览器/soak 证据在 revision 146 生成，随后 retry/Metrics 回归推进到 148，CronJob 回归推进到 149 |
| 运行目录 | `/var/lib/dosh/e2e-runs/20261103T223506Z-v234-node-cordon`、`/var/lib/dosh/e2e-runs/20261104T064500Z-v234-webkit-oidc`、`/var/lib/dosh/e2e-runs/20261104T065500Z-v234-rollback`、`/var/lib/dosh/e2e-runs/20261104T065500Z-v234-capacity`、`/var/lib/dosh/e2e-runs/20261104T070000Z-v234-soak`、`/var/lib/dosh/e2e-runs/20261104T073500Z-v234-retry`、`/var/lib/dosh/e2e-runs/20261104T074000Z-v234-metrics`、`/var/lib/dosh/e2e-runs/20261104T074500Z-v234-audit-loki`、`/var/lib/dosh/e2e-runs/20261104T080000Z-v234-cronjob` |
| 本地门禁 | `make verify`、`make race`、Vitest `18/18`、mock Chromium/Firefox/WebKit 各 `16/16`、真实 OIDC Chromium/Firefox/WebKit 各 `15/15`、生产 LCP `516.0 ms`、bundle `341.17 KiB`、`bash -n scripts/e2e-oidc.sh` 通过 |
| 真实 API/RBAC | 签名测试 IdP 三身份矩阵通过；viewer/operator 对 `worker-0005` cordon 均为 403，admin cordon/uncordon 均为 200 |
| 状态对账 | cordon 后 `kubectl` 读回 `.spec.unschedulable=true`，uncordon 后恢复为空/false；脚本退出时再次核对节点可调度 |
| 审计 | `viewer@example.test cordon denied`、`operator@example.test cordon denied`、`admin@example.test cordon success`、`admin@example.test uncordon success`，四条均带 `request_id` |
| 安全扫描 | Trivy Debian `12.15` 与 Go binary 均 0 HIGH/CRITICAL；`govulncheck` 0；公共 registry 生产 npm audit `found 0 vulnerabilities` |
| 容量 | 1000 Pod / 200 工作负载：Pods P95 `21.200745 ms`、Workloads P95 `189.161479 ms`、Overview P95 `205.373542 ms`，请求后 Heap `28 MiB` |
| 长连接 | 30 分钟 soak：29 事件、47,082 事件帧、28,396 日志行、5 次重连、0 应用错误、0 流失败；内存首/末 `9/17 MiB`，峰值 `31 MiB` |
| 回滚/恢复 | 隔离 Release revision `142→143→144→145`；每阶段 health/ready、三身份 RBAC 和 logout 通过；随后 soak 推进到 146，retry/Metrics 回归推进到 148，CronJob 回归推进到 149 |
| 当前 digest 回归 | retry：429/500/502/503/504 各注入 2 次后恢复，持续 429 恰好 3 次，`Retry-After` 2121 ms，写请求仅 1 次且副本数 2→2，连接重置/取消/恢复通过；Metrics：可用 CPU `2757m`、内存 `11165696000`，不可用降级为 0 且无陈旧值，恢复 CPU `2124m`、内存 `11188080640`；Loki：59 条事件、34 个 requestId、租户隔离、20/20 SIGTERM 排空，凭据扫描 0 |
| CronJob 回归 | revision 149：viewer 登录后请求无 selector CronJob，HTTP 200、`selector={}`、`pods=[]`、`status=Suspended`；删除后读回 NotFound |
| 证据归档 | 原始证据 `/var/lib/dosh/evidence/0c61ce348734/20261103T232858Z-v234-final`，41 文件，manifest `458283d1b60dbdded045139b77e7111a112c720cd5e09f2ab6ddbccf7618fdc1`；当前 digest 回归证据 `/var/lib/dosh/evidence/0c61ce348734/20261103T234130Z-v234-current-digest-regression`，12 文件，manifest `769b9730279f5917a54fc2fd6072dbc53223baff36193dc16c7bc4f025a2dec3`；CronJob 证据 `/var/lib/dosh/evidence/0c61ce348734/20261103T234429Z-v234-cronjob-detail`，4 文件，manifest `b6a6c4e47e868737d865668daa3e6cabbdf45e576227b4211a65fc87e3e5ce60`；源码绑定 `binary a27f90d9…f4ccc`、`source_tree 1f9c90ef…8fc7` |
| 生产前后 | `dosh` 仍为 App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626`，未改动 |

实现与验收边界：`Repository.SetNodeScheduling` 只在状态实际变化时更新 Node；app 层先执行
`update nodes` 授权，拒绝时 Repository 零写入；Kube 读取 Pod 计数失败时在写入前返回，避免
“更新成功但接口报失败”的不一致。前端只有 `capabilities.nodeCordon=true` 才显示按钮，执行前必须
通过 AlertDialog 二次确认，成功后刷新 `node` 与 `nodes` 查询。Helm local 模式仅新增
`nodes` 的 `update/patch`，不扩大 namespace 写权限；OIDC 模式继续依赖 impersonation + SAR。

**判定**：NODE-O-001 在“签名测试 IdP + 隔离 Release + Chromium/Firefox/WebKit 容器”范围内实验室
已验收；当前 digest 的 mock/真实 OIDC 浏览器矩阵、Trivy、govulncheck、npm audit、容量、30 分钟
soak、rollback/restore、retry、Metrics 三态、真实 Loki、源码绑定和原始证据归档均已通过。
文档绑定固定为 `/var/lib/dosh/e2e-runs/20261104T080500Z-v234-final-docs-r2/documentation-binding-v234-r2.txt`，
以 `v234-final-docs-r2` 标签归档；锚定副本以 `v234-final-docs-r2-anchor` 标签归档。
生产节点不得用于此测试；企业 IdP、受信 TLS、真实 SIEM、Edge/Safari 实机、CI 真跑、离机 WORM 和
生产发布/回滚仍需独立变更单，因此当前候选仍不构成完整商用放行。

### 21.2.28 v2.35 节点五标签与功能补齐候选验收（2026-11-04）

| 项目 | 值 |
|---|---|
| 源码版本 | App/OpenAPI/Web `0.1.4`；Chart `0.1.6` |
| candidate tag | `0.1.4-rc-nodetabs-20261104T021724Z` |
| registry digest | `sha256:844bd19fceebc9cc249ce7bf12772b3ea13b15b1204c05277adcee6df475b744` |
| image ID | `sha256:a1f7e71dbd532ae78f6eee24eb467c1f8b57dd99195d09832d71da3fbeeab1a7` |
| 隔离对象 | Release `dosh-ui-e2e`，namespace `dosh-ui-e2e`，revision 7，2/2 Ready，NodePort `32629` |
| 运行目录 | `/var/lib/dosh/e2e-runs/20261104T021724Z-candidate-build`；mock E2E 日志位于 `/var/lib/dosh/e2e-runs/20261104T021307Z-node-tabs-feature` |
| 功能范围 | 节点/工作负载/Pod 五标签详情；节点 Pod 日志复用选择器；节点事件按 Kind/Name 过滤；列表分页/排序/筛选元数据；事件 UID 去重和指数退避重连；IngressClass 空状态与 Ingress 目标状态；非流式日志 1 MiB 上限 |
| 本地门禁 | `go test ./...`、`go test -race ./...`、`go vet` 通过；`make arch`、`make api-check`、`make helm-check`、`make version-check`、`make playwright-check` 通过 |
| 前端测试 | Vitest `31/31`；mock Chromium `26/26`、Firefox `26/26`，分别在独立容器串行执行，`workers=1`、`retries=0`；真实本地认证 Chromium `1/1`、Firefox `1/1` |
| 隔离 API | `/healthz` 200、`/readyz` 200、登录 200；节点 `total=8`、`page=1`、`pageSize=2`、`totalPages=4`、sort `name` asc；事件 `total=96`、first UID `0bc85ca0-90fb-4041-ba83-122d86405db8`、`pageSize=5`、`totalPages=20`；不存在 Pod 的对象事件过滤 `total=0`；非法 sort 返回 HTTP 400 `validation_failed` 且字段错误定位 `sort`；节点 YAML 4,838 字节 |
| 容量 | 1000 Pod / 200 工作负载：Pods P95 `21.461803 ms`、Workloads P95 `190.893808 ms`、Overview P95 `206.329650 ms`；容量请求后 Heap `28 MiB`，HeapInuse `31 MiB` |
| 前端构建 | 生产构建通过；initial JavaScript gzip `343.07 KiB`，低于 500 KiB 预算 |
| 安全扫描 | Trivy Debian `12.15` 与 Go binary 均 0 HIGH/CRITICAL，使用预取 DB（`UpdatedAt=2026-11-03T13:13:42Z`，DB SHA-256 `5ec85285…0ed2b`）；`govulncheck` 0；公共 npm registry 生产依赖 0 vulnerabilities |
| 运行时状态 | Deployment generation/observedGeneration `12/12`，2/2 Ready；两个 Pod 均运行同一 registry digest；Service `NodePort 32626:32629` |
| 生产前后 | `dosh` 仍为 App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626`，未改动 |
| 证据归档 | `/var/lib/dosh/evidence/844bd19fceeb/20261104T022727Z-v235-current-candidate`，39 文件，manifest SHA-256 `a4f53483434e9191ac971dbb82628171aece513038f1c072856b4210c7e952ac`，双层校验和敏感文件扫描通过 |
| 未复跑项 | OIDC 浏览器矩阵、WebKit、30 分钟 soak、rollback/restore、生产 LCP、Edge/Safari 实机、企业 IdP、受信 TLS、企业 SIEM、GitHub Actions 真跑和分支保护、离机 WORM、生产发布/回滚 |

**实现落点**：`web/src/features/nodes/node-detail-page.tsx` 使用 URL 驱动的 `overview/pods/events/logs/yaml`
标签；`web/src/features/workloads/pod-logs.tsx` 统一节点与工作负载的 Pod 日志选择；`router.tsx` 为
`/nodes/$name` 增加列表搜索参数校验；Vitest 覆盖节点标签深链和事件 Kind/Name 查询，Playwright 覆盖
实例、日志、事件和 YAML 标签。后端事件筛选仍由 `internal/modules/events/http/handler.go` 解析
`objectKind/objectName`，`internal/platform/kube/events.go` 在 Kubernetes Events 结果上做大小写无关
对象过滤，保持 `http -> app -> domain -> adapter` 边界。

**判定**：v2.35 当前候选在“隔离 Release + 当前源码 + Chromium/Firefox 容器 + 真实本地认证”范围内
功能与本地门禁已验收；这不代表 OIDC、WebKit/Safari、生产环境、长期稳定性或企业外部门禁已验收。

### 21.2.29 v2.36 Service/Ingress 详情候选验收（2026-11-04）

| 项目 | 值 |
|---|---|
| 源码版本 | App/OpenAPI/Web `0.1.4`；Chart `0.1.6` |
| candidate tag | `0.1.4-rc-netdetail-20261104T024228Z` |
| registry digest | `sha256:dde520b65d66cbe45a2abdd085bdfd9037cb5457b6138ee8861912dbf22e0f95` |
| image ID | `sha256:e9d1e15d5f3f0f4e97eaaaa8b1f5bf3ac33bb5ff0323393ea0749d885fa915c9` |
| 本地 manifest digest | `sha256:5e695028920b38dd74230e1744c364d31f269784343c15fdaa47742dc138e8c4`；仅作本地追踪，不得冒充 registry digest |
| 隔离对象 | Release `dosh-ui-e2e`，namespace `dosh-ui-e2e`，revision 8，2/2 Ready，NodePort `32629` |
| 运行目录 | `/var/lib/dosh/e2e-runs/20261104T024228Z-network-detail` |
| 功能范围 | Service 详情与 EndpointSlice 就绪/目标；Ingress 详情、默认后端、TLS Secret、路由目标状态；服务/路由列表进入详情；概览/端点或路由/事件/YAML URL 深链；OpenAPI 与 TS 类型同步 |
| 本地门禁 | `go test ./...`、`go test -race ./...`、`go vet`、`make arch`、`make api-check`、`make helm-check`、`make version-check`、`make playwright-check`；最终 `make verify` 日志和绑定见运行目录 |
| 前端测试 | Vitest `33/33`；mock Chromium `28/28`、Firefox `28/28`，分别在独立容器串行执行，`workers=1`、`retries=0`；真实本地认证 Chromium `1/1`、Firefox `1/1` |
| 隔离 API | `/healthz` 200、`/readyz` 200、本地登录 200；Service `dosh-ui-e2e/dosh` 详情 200、`endpointCount=2`、2 个 Endpoint、`sessionAffinity=None`；临时 Ingress 详情 200、1 条路由、默认后端 `targetStatus=Ready`、TLS Secret `dosh-tls`；Service/Ingress YAML 均 200；临时 Ingress 已清理 |
| 容量 | 1000 Pod / 200 工作负载：Pods P95 `22.260016 ms`、Workloads P95 `188.43142 ms`、Overview P95 `196.955958 ms`；容量请求后 Heap `28 MiB`，HeapInuse `31 MiB` |
| 前端构建 | 生产构建通过；initial JavaScript gzip `344.69 KiB`，低于 500 KiB 预算 |
| 安全扫描 | Trivy Debian `12.15` 与 Go binary 均 0 HIGH/CRITICAL；`govulncheck v1.9.0` 无漏洞；公共 npm registry 生产依赖 0 vulnerabilities |
| 运行时状态 | 隔离 Deployment 2/2 Ready；两个 Pod 均运行同一 registry digest；Service `NodePort 32626:32629` |
| 生产前后 | `dosh` 仍为 App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626`，未改动 |
| 源码/文档绑定 | `deploy/release/bindings/source-binding-v236.txt`、`deploy/release/bindings/documentation-binding-v236.txt`；文件摘要写入 `deploy/release/commercial-gates.json` |
| 证据归档 | 最终 `make verify`、`make release-gate-check`、生产基线复核和本轮运行目录在完成后归档到 `/var/lib/dosh/evidence/dde520b65d66/`；本节不绑定归档摘要，避免文档修订与归档摘要循环依赖 |
| 未复跑项 | OIDC 浏览器矩阵、WebKit、30 分钟 soak、rollback/restore、生产 LCP、Edge/Safari 实机、企业 IdP、受信 TLS、企业 SIEM、GitHub Actions 真跑和分支保护、离机 WORM、生产发布/回滚 |

**实现落点**：`internal/domain/types.go` 定义 `ServiceDetail`/`ServiceEndpoint`/`IngressDetail`/
`IngressTLS`；`internal/modules/network/app` 只声明 repository 端口并委托查询；`internal/modules/network/http`
挂载 `/services/{namespace}/{name}` 与 `/ingresses/{namespace}/{name}`；`internal/platform/kube/network.go`
转换 Service、EndpointSlice、Ingress、Service 端口和就绪 Endpoint，并计算默认后端/路由目标状态；
`web/src/features/network/{service-detail-page,ingress-detail-page}.tsx` 提供 URL 驱动详情页，
`network-page.tsx` 提供列表到详情的链接，`web/src/api/{client,types,schema.d.ts}` 与 OpenAPI 同步。

**判定**：v2.36 当时候选在“隔离 Release + 当前源码 + Chromium/Firefox 容器 + 真实本地认证”范围内
完成 Service/Ingress 详情功能与本地门禁验收；OIDC、WebKit/Safari、生产环境和七个外部商用门禁仍未
关闭，不得据此宣称完整商用放行；其证据不得继承给 v2.37。

### 21.2.30 v2.37 存储详情候选验收（2026-11-04）

| 项目 | 值 |
|---|---|
| 源码版本 | App/OpenAPI/Web `0.1.4`；Chart `0.1.6` |
| candidate tag | `0.1.4-rc-storagedetail-20261104T030457Z` |
| registry digest | `sha256:d75a7eaf2159f8cfeb74b97e288d60ebb572ab02e72f717af75caa73852d5b51` |
| image ID | `sha256:1d7eaac0c6b3705fbd05665abee4f90858480aaf2684e95c5d3dadb6f4054484`；仅作本地追踪，不得冒充 registry digest |
| 隔离对象 | Release `dosh-ui-e2e`，namespace `dosh-ui-e2e`，revision 9，2/2 Ready，NodePort `32629` |
| 运行目录 | `/var/lib/dosh/e2e-runs/20261104T030457Z-storage-detail` |
| 功能范围 | StorageClass 详情（参数、回收策略、卷绑定、扩容、挂载选项）；PV 详情（容量、访问模式、卷模式、CSI、claim 引用、挂载选项）；PVC 详情（申请/实际容量、绑定 PV、volumeMode、conditions）；三类资源事件与 YAML 深链；列表名称和 PVC/PV/StorageClass 关联可进入详情；OpenAPI 与 TS 类型同步 |
| 本地门禁 | `go test ./...`、`go test -race ./...`、`go vet`、`make arch`、`make api-check`、`make helm-check`、`make version-check`、`make playwright-check`；最终 `make verify` 日志和绑定见运行目录 |
| 前端测试 | Vitest `36/36`；mock Chromium `31/31`、Firefox `31/31`，分别在独立受限容器串行执行，`workers=1`、`retries=0`；真实本地认证 Chromium `1/1`、Firefox `1/1`，资源上限 `6g/4 CPU/1g shm/2048 PIDs` |
| 隔离 API | 临时 StorageClass `dosh-e2e-storage`、PV `dosh-e2e-storage-pv`、PVC `dosh-ui-e2e/dosh-e2e-storage-claim` 均创建成功并绑定；三类详情 API 返回参数、`capacityBytes=1073741824`、`requestedCapacityBytes=1073741824`、`volumeMode=Filesystem`、claim 命名空间/名称和 `DoshE2E=True` condition；三类 YAML API 均 200，内容包含 provisioner、绑定和 volumeName。删除 fixture 后三类详情均为 404 |
| 认证隔离 | 仅临时替换隔离 namespace 的 admin 密码哈希以执行真实本地认证；验收后恢复原 Secret 并完成滚动，未归档密码、Cookie、session 或 Secret 内容 |
| 容量 | 1000 Pod / 200 工作负载：Pods P95 `21.418455 ms`、Workloads P95 `193.988339 ms`、Overview P95 `201.956541 ms`；容量请求后 Heap `28 MiB`，HeapInuse `31 MiB` |
| 前端构建 | 生产构建通过；initial JavaScript gzip `345.99 KiB`，低于 500 KiB 预算 |
| 安全扫描 | Trivy Debian `12.15` 与 Go binary 均 0 HIGH/CRITICAL；`govulncheck v1.9.0` 无漏洞；公共 npm registry 生产依赖 0 vulnerabilities |
| 运行时状态 | 隔离 Deployment 2/2 Ready；两个 Pod 均运行同一 registry digest；Service `NodePort 32626:32629` |
| 生产前后 | `dosh` 仍为 App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626`，未改动 |
| 源码/文档绑定 | `deploy/release/bindings/source-binding-v237.txt`、`deploy/release/bindings/documentation-binding-v237.txt`；文件摘要写入 `deploy/release/commercial-gates.json` |
| 证据归档 | 最终 `make verify`、`make release-gate-check`、生产基线复核和本轮运行目录归档到 `/var/lib/dosh/evidence/d75a7eaf2159/`；本节不绑定归档摘要，避免文档修订与归档摘要循环依赖 |
| 未复跑项 | OIDC 浏览器矩阵、WebKit、30 分钟 soak、rollback/restore、生产 LCP、Edge/Safari 实机、企业 IdP、受信 TLS、企业 SIEM、GitHub Actions 真跑和分支保护、离机 WORM、生产发布/回滚 |

**实现落点**：`internal/domain/types.go` 定义 `StorageClassDetail`、`PersistentVolumeDetail` 和
`PersistentVolumeClaimDetail`；`internal/modules/storage/app` 保持 repository 端口最小化；
`internal/modules/storage/http` 挂载三类详情路由；`internal/platform/kube/storage.go` 负责
StorageClass 参数/挂载选项/扩容、PV CSI 与 claim 引用、PVC 申请容量和 conditions 转换；
`web/src/features/storage/storage-detail-page.tsx` 提供 URL 驱动详情，`storage-page.tsx` 提供列表和
关联对象链接，`web/src/router.tsx` 与 OpenAPI/TS 类型同步。

**判定**：v2.37 历史候选在“隔离 Release + 当时源码 + Chromium/Firefox 容器 + 真实本地认证”范围内
完成存储详情功能与本地门禁验收；OIDC、WebKit/Safari、生产环境和七个外部商用门禁仍未关闭，
不得据此宣称完整商用放行或已发布生产。

### 21.2.31 v2.38 命名空间详情与 Playwright 串行隔离（2026-11-04）

| 项目 | 值 |
|---|---|
| 源码版本 | App/OpenAPI/Web `0.1.4`；Chart `0.1.6` |
| candidate tag | `0.1.4-rc-namespacedetail-20261104T034542Z` |
| registry digest | `sha256:1387225664a7927fb872f68b988ec3fe731f8b80de62737d2db880e93322e2e5` |
| image ID | `sha256:11bd8e488375566dd3babafbd670fd1f65e97c82ede581aee225dcefe9cbc77f`；仅作本地追踪，不得冒充 registry digest |
| 隔离对象 | Release `dosh-ui-e2e`，namespace `dosh-ui-e2e`，revision 11，2/2 Ready，NodePort `32629` |
| 运行目录 | `/var/lib/dosh/e2e-runs/20261104T034542Z-v238-namespace-detail-r2` |
| 功能范围 | 命名空间列表进入详情；概览展示状态、标签、注解、创建时间和资源治理摘要；ResourceQuota 展示 hard/used；LimitRange 展示 min/max/default/defaultRequest；提供事件与 YAML 深链；local 模式 ClusterRole 增加 `resourcequotas`、`limitranges` 读取权限 |
| Playwright 隔离 | 所有配置固定 `workers: 1`、`retries: 0`；runner 强制每浏览器独立容器，拒绝 `--workers`、`--fully-parallel` 和正数 `--retries`；资源上限 `6g/4 CPU/1g shm/2048 PIDs`、私有 IPC、20 分钟硬超时、宿主级 `flock` 串行锁 |
| 前端测试 | Vitest `36/36`；mock Chromium `32/32`、Firefox `32/32`；真实本地认证 Chromium `1/1`、Firefox `1/1`；均单 worker、零重试 |
| 隔离 API | 临时命名空间创建 `compute` ResourceQuota（`requests.cpu=2`、`requests.memory=2Gi`、`pods=10`）和 `defaults` LimitRange（Container min `50m`、max `1`、default `500m`、defaultRequest `100m`）；详情 API 200 并返回 hard/used 与限制项；YAML API 200；删除后详情 404 |
| 认证隔离 | 仅临时替换隔离 namespace 的 admin 密码哈希执行真实本地认证；验收后恢复原 Secret 并完成滚动，未归档密码、Cookie、session 或 Secret 内容 |
| 容量 | 1000 Pod / 200 工作负载：Pods P95 `21.820424 ms`、Workloads P95 `193.570532 ms`、Overview P95 `200.636060 ms`；Heap `28 MiB`，HeapInuse `31 MiB` |
| 最终门禁复跑 | `make verify`、`make race`、`make capacity-test`、`make evidence-check`、`make release-gate-schema-check` 均 PASS；容量复跑 Pods P95 `23.134918 ms`、Workloads P95 `189.880657 ms`、Overview P95 `203.084490 ms`，Heap `28 MiB`；`make release-gate-check` 按预期退出 `2`，仅七个外部 pending blocker，无 INVALID；日志位于运行目录 `final-gates/` |
| 前端构建 | 生产构建通过；initial JavaScript gzip `347.09 KiB`，低于 500 KiB 预算 |
| 安全扫描 | Trivy Debian `12.15` 与 Go binary 均 0 HIGH/CRITICAL；`govulncheck v1.9.0` 无漏洞；公共 npm registry 生产依赖 0 vulnerabilities |
| 源码/文档绑定 | `deploy/release/bindings/source-binding-v238.txt`、`deploy/release/bindings/documentation-binding-v238.txt`；文件摘要写入 `deploy/release/commercial-gates.json` |
| 生产前后 | `dosh` 仍为 App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626`，未改动 |
| 未复跑项 | OIDC 浏览器矩阵、WebKit、30 分钟 soak、rollback/restore、生产 LCP、Edge/Safari 实机、企业 IdP、受信 TLS、企业 SIEM、GitHub Actions 真跑和分支保护、离机 WORM、生产发布/回滚 |

**实现落点**：`internal/domain/types.go` 新增 `NamespaceDetail`、`ResourceQuota`、`LimitRange` 和
`LimitRangeItem`；`internal/modules/nodes/app` 的 Repository 端口扩展为返回命名空间详情；
`internal/platform/kube/nodes.go` 聚合 Namespace、ResourceQuota 与 LimitRange 并转换为 domain DTO；
`internal/modules/nodes/http` 保持既有 `/api/v1/namespaces/{name}` 契约；`deploy/helm/dosh/templates/rbac.yaml`
为 local 模式补充最小读取权限；`web/src/features/namespaces/namespace-detail-page.tsx` 提供 URL 驱动详情，
OpenAPI、生成类型和 TypeScript 类型同步。Playwright 的 `workers=1`、`retries=0`、资源上限与覆盖拒绝由
`scripts/run-playwright-container.sh` 和 `scripts/check-playwright-isolation.sh` 双重门禁保证。

**判定**：v2.38 当时候选在“隔离 Release + 当时源码 + Chromium/Firefox 隔离容器 + 真实本地认证”范围内
完成命名空间详情功能与本地门禁验收；OIDC、WebKit/Safari、生产环境和七个外部商用门禁仍未关闭，
不得据此宣称完整商用放行或已发布生产。

### 21.2.32 v2.39 可访问性修复与当前候选复验（2026-11-04）

| 项目 | 值 |
|---|---|
| 源码版本 | App/OpenAPI/Web `0.1.4`；Chart `0.1.6` |
| candidate tag | `0.1.4-rc-a11y-20261104T041000Z` |
| registry digest | `sha256:c62d2d9773839a8b2100f85aef0ac98a2f7df4231e09e9b3ef8d00ef2fbadb61` |
| image ID | `sha256:b63edaeddd2ddbc69b3d1750d35682f28c9d0c388c11006608532f6ab091bd11`；仅作本地追踪，不得冒充 registry digest |
| 二进制 SHA-256 | `40f875036eee8c0e0a51f1dd4c6d6c4836d835caec13c8ea61e375eb10070c73` |
| 源码绑定 | `deploy/release/bindings/source-binding-v239.txt`；文件 SHA-256 `da2e156f996c05391fc3dff58bb58fd379e3f8bf55fb6501a6097e75de78b424`；197 个源码文件，tree `bde87aa16de2f2e7b4ae08802653096eecd29a1ccb1eb406efa5c8e592996214` |
| 隔离 UI Release | `dosh-ui-e2e/dosh-ui-e2e` revision 12、2/2 Ready、NodePort `32629`；两个 Pod 均运行当前 registry digest |
| 隔离 OIDC Release | `dosh-e2e-oidc/dosh-e2e-oidc`；回滚/恢复证据最终 revision 155，NodePort `32627` |
| 主运行目录 | `/var/lib/dosh/e2e-runs/20261104T041000Z-v239-a11y` |
| 缺陷复现 | v2.38 工作负载详情 `Failed` 徽标前景/背景对比度 `4.13:1`，低于 WCAG AA `4.5:1`；Chromium axe 失败，其余 14/15 通过；证据 `/var/lib/dosh/e2e-runs/20261104T040538Z-v238-oidc-browser` |
| 修复范围 | 新增亮色 `--destructive-strong: 0 72% 42%` 与暗色 `0 76% 80%`；危险状态文字使用新 token，危险实色按钮继续使用原 `--destructive`；同步 `badge`、状态反馈、指标卡、登录页和 Deployment 创建确认；新增 `status-badge.test.tsx` |
| 前端门禁 | Vitest `37/37`；生产构建和 bundle 预算通过，initial JavaScript gzip `347.10 KiB`；Playwright mock Chromium/Firefox 各 `32/32`、真实本地认证各 `1/1`，全部 `workers=1`、`retries=0` |
| 真实 OIDC 浏览器 | Chromium `15/15`、Firefox `15/15`；覆盖 viewer/operator/admin、七类负向登录、暗色主题、亮暗 axe、键盘主路径和 logout |
| 负向审计与泄露 | `expected_callback_failures=12`、`callback_failures=12`、带 requestId 的 denied 登录审计 `12/12`；应用日志、ConfigMap/Deployment、Helm values、浏览器 URL/DOM/Storage/响应体 Token 扫描 0 命中 |
| 本地认证与 API | 隔离 Release 真实本地认证 Chromium/Firefox 各 `1/1`；命名空间详情、Deployment 详情、节点详情、Pod 详情 API 均 `200`；测试后恢复原认证 Secret 并滚动完成 |
| 回滚与恢复 | `152 → 153 → 154 → 155`；baseline、upgrade、rollback、restore 每阶段 health/ready 200、三身份 RBAC 和 logout 均通过；证据 `/var/lib/dosh/e2e-runs/20261104T041439Z-v239-oidc-rollback/oidc-rollback-evidence.log` |
| 容量 SLO | 1000 Pod / 200 工作负载：Pods P95 `19.218174 ms`、Workloads P95 `192.169383 ms`、Overview P95 `200.587524 ms`；容量请求后 Heap `28 MiB`、HeapInuse `31 MiB`；日志 `final-gates/capacity.log` |
| 30 分钟 soak | 通过：29 个事件、29 个事件 ConfigMap、40,943 个事件帧、28,479 行日志、5 次重连、0 应用错误、0 事件创建失败、0 流失败；56 次内存采样 `22 → 18 MiB`、峰值 `29 MiB`；覆盖 Pod 删除和应用滚动重启；证据 `/var/lib/dosh/e2e-runs/20261104T041600Z-v239-soak/soak-evidence.log` |
| 安全与静态门禁 | Trivy Debian/Go `0 HIGH/CRITICAL`；govulncheck `0`；npm 公共 registry 生产依赖 `0`；`make race`、`make arch`、`make api-check`、`make helm-check`、`make version-check`、`make playwright-check`、`make e2e-image-check` 和 `actionlint` 通过 |
| 最终门禁 | `make verify`、`make race`、`make capacity-test`、`make evidence-check`、`make release-gate-schema-check` 均 PASS；`make release-gate-check` 按预期退出 `2`，仅七个外部 pending blocker，无 INVALID；容量 Pods P95 `19.218174 ms`、Workloads P95 `192.169383 ms`、Overview P95 `200.587524 ms`，Heap `28 MiB` |
| 生产前后 | `dosh` 仍为 App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626`，未改动 |
| 未复跑项 | WebKit、生产 LCP、Metrics 三态、429/5xx retry、真实 Loki/SIEM、Edge/Safari 实机、企业 IdP、受信 TLS、CI 真跑/分支保护、离机 WORM、生产发布/回滚 |

**实现落点**：`web/src/index.css` 与 `web/tailwind.config.ts` 增加 `destructive-strong` 语义 token；
`web/src/components/ui/badge.tsx`、`web/src/components/feedback/states.tsx`、
`web/src/components/data/metric-card.tsx`、`web/src/routes/login-page.tsx` 和
`web/src/features/workloads/deployment-create-dialog.tsx` 将危险状态文字切换到该 token；
`web/src/components/feedback/status-badge.test.tsx` 固化危险徽标样式，防止后续回归。Playwright
继续保持受限容器、单 worker、零重试和宿主级串行锁，不以提高并发或重试换取测试速度。

**判定**：v2.39 在签名测试 IdP、隔离 Release、Chromium/Firefox 受限容器和真实本地认证范围内
关闭了危险状态对比度缺陷，并完成 OIDC、回滚、soak 和本地门禁复验；WebKit、Edge/Safari 实机、
企业 IdP/TLS/SIEM、CI 真跑、离机 WORM、生产发布/回滚和七个外部商用门禁仍未关闭，因此不得
宣称完整商用放行或已发布生产。

### 21.2.33 v2.40 WBS-4.7 日志与事件安全（2026-11-04）

| 项目 | 值 |
|---|---|
| 源码版本 | App/OpenAPI/Web `0.1.4`；Chart `0.1.6` |
| candidate tag | `0.1.4-rc-logsec-20261104T045837Z` |
| registry digest | `sha256:f866e3ba3448a82feb8fb6a66a77da713ff41e79f90889979024733ed2cacaaf` |
| image ID | `sha256:ffca2f634d1f7bf2d54535ea3341e44b936fd66518b08907bd13765b64d3a26d`；仅作本地追踪，不得冒充 registry digest |
| 二进制 SHA-256 | `fe14e3e5a69b20cd67790828f85c69949bee1c6e007d19bde8686833d4d1dc4c` |
| 源码绑定 | `deploy/release/bindings/source-binding-v240.txt`；文件 SHA-256 `0628dc9173cb9ec82ff42e0348e0490a6882a4c6758e05c12f476135ac3b4b38`；200 个源码文件，tree `b769180ef34f48a5ed3c77cc539dfe96d03cf9d7d1e0ec8aadb07aa5afe67552` |
| 隔离 UI Release | `dosh-ui-e2e/dosh-ui-e2e` revision 13、2/2 Ready、NodePort `32629`；两个 Pod 均运行当前 registry digest |
| 主运行目录 | `/var/lib/dosh/e2e-runs/20261104T045837Z-v240-logsec` |
| 功能实现 | `log-sanitizer.ts` 增量解析 ANSI CSI、OSC、DCS/SOS/PM/APC、单字符 ESC、C0/C1、DEL 与双向显示控制字符；跨 chunk 保持解析状态，保留 LF/TAB，HTML 字符原样保留为文本；`log-viewer.tsx` 使用该解析器并仍以 `<pre>` 文本节点渲染 |
| 单元与组件测试 | `log-sanitizer.test.ts` 7 条、`log-viewer.test.tsx` 2 条；覆盖跨 chunk CSI/OSC/DCS、控制字符、HTML 纯文本、`javascript:` OSC 链接、`localStorage`/`sessionStorage` 不落盘和属性不泄露标记；Vitest `46/46` |
| 浏览器 mock 矩阵 | Chromium `34/34`、Firefox `34/34`，均为单 worker、`retries=0`、每浏览器独立受限容器；新增“Pod 日志 HTML/ANSI 纯文本”和“事件消息不创建可执行 DOM”用例；日志 `playwright-mock.log` |
| 静态安全扫描 | `rg 'dangerouslySetInnerHTML|innerHTML\s*=' web/src` 无产品源码命中；`make playwright-check` PASS |
| 隔离运行 | revision 13 升级成功，2/2 Ready；`/healthz` 200、`/readyz` 200、未认证 `/api/v1/overview` 401；生产 `dosh` revision 12 未改动 |
| 镜像扫描 | Trivy Debian `12.15` 与 Go binary 均 0 HIGH/CRITICAL；日志 `trivy.log` |
| 前端构建 | 生产构建通过；initial JavaScript gzip `347.40 KiB`，低于 500 KiB 预算 |
| 容量 SLO | 1000 Pod / 200 工作负载：Pods P95 `20.442297 ms`、Workloads P95 `186.487355 ms`、Overview P95 `202.276274 ms`；请求后 Heap `28 MiB`、HeapInuse `31 MiB` |
| 本地全量门禁 | `make verify`、`make race`、`make capacity-test`、`make web-test`、`make web-bundle-check` 和 `make release-gate-schema-check` 通过；`actionlint` 历史配置未改动 |
| 依赖审计 | govulncheck `No vulnerabilities found`；公共 npm registry 生产依赖 `found 0 vulnerabilities` |
| 最终发布门禁 | `make release-gate-check` 按预期退出 `2`，仅七个外部 pending blocker，无 INVALID；证据 `final-release-gate.log` |
| LCP | `/var/lib/dosh/e2e-runs/20261104T052011Z-v240-lcp`；受限 Chromium、单 worker，生产构建 LCP `484.0 ms < 2500 ms`，console errors 为空；`exit-code.txt` 为 `0` |
| 真实本地认证 | `/var/lib/dosh/e2e-runs/20261104T052408Z-v240-local-auth-r2`；受限 Chromium `1/1`、Firefox `1/1`，串行执行；测试临时替换 Deployment 实际引用的 `dosh-auth-limiter` Secret，结束后恢复并滚动完成 |
| 真实 OIDC 浏览器 | `/var/lib/dosh/e2e-runs/20261104T052509Z-v240-oidc-browser`；Chromium `15/15`、Firefox `15/15`；`12/12` callback failure、`12/12` 带 requestId denied 审计；日志、ConfigMap/Deployment、Helm values Token 扫描 0 |
| 隔离 WebKit mock | `/var/lib/dosh/e2e-runs/20261104T052940Z-v240-webkit-mock`；隔离 libvirt runner，`34/34`，`workers=1`、`retries=0`；未在物理宿主直接运行 WebKit |
| 隔离 WebKit 真实 OIDC | `/var/lib/dosh/e2e-runs/20261104T053327Z-v240-webkit-oidc`；`15/15`，`6/6` denied 审计带 requestId，Token 扫描 0 |
| 回滚与恢复 | `/var/lib/dosh/e2e-runs/20261104T053630Z-v240-oidc-rollback`；revision `161 → 162 → 163 → 164`，baseline、upgrade、rollback、restore 每阶段 health/ready、三身份 RBAC、logout 均通过 |
| 30 分钟 soak | `/var/lib/dosh/e2e-runs/20261104T053724Z-v240-soak`；30 事件、30 个事件 ConfigMap、53,821 事件帧、28,466 日志行、5 次重连、0 应用错误、0 流失败；57 次内存采样 `22 → 18 MiB`，峰值 `31 MiB`；覆盖 Pod 删除和应用滚动重启 |
| Metrics 三态 | `/var/lib/dosh/e2e-runs/20261104T051156Z-v240-metrics`；可用 CPU `2097m`、内存 `11180830720`；不可用 `metricsAvailable=false`、usage 0；恢复 CPU `2091m`、内存 `11221159936` |
| 429/5xx retry | `/var/lib/dosh/e2e-runs/20261104T051400Z-v240-retry`；429/500/502/503/504 各 2 次失败后恢复；持续 429 恰好 3 次并返回 `rate_limited`；`Retry-After` `2121 ms`；写请求 503 仅 1 次且副本数不变 |
| 真实 Loki 审计 | `/var/lib/dosh/e2e-runs/20261104T051514Z-v240-loki`；59 条事件、34 个 requestId、租户隔离；覆盖私 CA、Bearer/HMAC、瞬时/持续 503、超时、队列满、只读拒绝；20/20 SIGTERM 排空，敏感信息泄露 0 |
| 未复跑项 | Edge/Safari 实机、企业 IdP、受信 TLS、企业 SIEM/OpenSearch、CI 真跑/分支保护、离机 WORM、生产发布/回滚；七个外部商用门禁全部保持 `pending` |

**实现落点**：`web/src/features/workloads/log-sanitizer.ts` 是纯前端增量解析器，
`web/src/features/workloads/log-viewer.tsx` 仅在网络 chunk 解码后调用它；测试不通过放宽渲染方式
规避风险，仍要求日志只进入 `<pre>` 文本节点。`web/e2e/dosh.spec.ts` 的恶意日志和事件夹具同时
验证 DOM 中没有 `img`、没有 OSC 生成的链接、页面脚本变量未被触发，且浏览器存储为空。Playwright
继续由 `scripts/run-playwright-container.sh` 强制 `workers=1`、`retries=0`、私有 IPC、资源上限和
宿主级 `flock`，本轮未提高并发，也未用重试掩盖失败。

**判定**：v2.40 历史 digest 在隔离 Release、Chromium/Firefox/WebKit 受限容器、本地/真实 OIDC、
LCP、Metrics、retry、Loki、soak、rollback/restore 和本地全量门禁范围内关闭 WBS-4.7 及候选实验室验收。
Safari/Edge 实机、企业 IdP、受信 TLS、企业 SIEM、CI 真跑、离机 WORM、生产发布/回滚和七个外部商用门禁
仍未关闭，因此不得宣称完整商用放行或已生产发布。

### 21.2.34 v2.41 WBS-4.8 审计历史查询（2026-11-04）

| 项目 | 结果 |
|---|---|
| 候选 | `0.1.4-rc-audit-20261104T062708Z`，registry digest `sha256:1e16c0fb923b6451c46a676bdf671eef9dcfd21e08a8a093d1193b844b655f94`，image ID `sha256:dc7953cee005287dd4504336776b6ec1b1d01fb9d96075085d68bd8bf28fdaaa` |
| 隔离 UI Release | `dosh-ui-e2e/dosh-ui-e2e` revision 14、2/2 Ready、NodePort `32629`；Deployment revision 44 记录本地认证 Secret 临时替换/恢复后的滚动，Pod 始终运行候选 digest |
| 隔离 OIDC Release | `dosh-e2e-oidc/dosh-oidc-e2e` revision 168、1/1 Ready、NodePort `32627`，运行候选 digest |
| 生产前后 | `dosh` 仍为 App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626`，未改动 |
| 架构 | `audit.Logger` 写 stdout/Webhook 的同时写入有界环形缓冲；`audithistory` 只有 app 用例、Repository 端口和 HTTP transport；chi 仅在模块 `Mount` 注册 `/audit-events`，未形成万能 CRUD 或跨模块直连 |
| API | `GET /api/v1/audit-events`；管理员鉴权；namespace/action/result/q/page/pageSize/sort/order；非法 result 返回 400；响应最多返回当前实例最近 1000 条记录 |
| 真实 OIDC 权限/查询矩阵 | 未认证 401；viewer/operator 403；admin 200；`namespace=dosh-e2e-target,action=scale,result=success,q=operator` 只返回匹配记录；`result=denied` 只返回 denied；`sort=id&order=desc` 分页元数据与顺序正确；`result=unknown` 为 400。登录、Deployment scale/create、节点 Cordon/Denied、失败与拒绝审计均可经 API 查询 |
| 真实 OIDC 浏览器 | Chromium `16/16`、Firefox `16/16`、隔离 WebKit `16/16`；新增“admin 可查询真实审计历史并按 URL 过滤”，并断言 viewer 无审计入口。每个浏览器 `6/6` denied 登录审计带 requestId；应用日志、ConfigMap/Deployment、Helm values Token 扫描 0 命中 |
| 隔离 WebKit mock | `35/35`，在 libvirt `dosh-browser-runner` 内执行；未在物理宿主直接运行 WebKit |
| 本地认证浏览器 | 隔离 `dosh-ui-e2e` 临时替换 `dosh-auth-limiter` 管理员哈希后，Chromium `1/1`、Firefox `1/1`；测试进入审计日志并看到当前管理员登录记录，结束后恢复原 Secret 并完成滚动 |
| 前端/单元 | Vitest `47/47`；`make web-typecheck`、`make web-build`、bundle initial JS gzip `348.62 KiB < 500 KiB`；`go test ./...`、`make race`、容量 SLO 通过 |
| 性能 | 生产构建 LCP `448.0 ms < 2.5 s`，在受限 Chromium 容器中执行 |
| Playwright 隔离 | 所有浏览器均 `workers=1`、`retries=0`、每浏览器独立容器、`6g/4 CPU/1g shm/2048 PIDs`、私有 IPC、20 分钟硬超时和宿主 `flock`；performance 项目也只允许经同一 runner 且必须配合 performance 配置 |
| 运行证据目录 | `/var/lib/dosh/e2e-runs/20261104T063309Z-v241-audit-oidc/`，含集群矩阵、mock Chromium/Firefox、本地认证、真实 OIDC Chromium/Firefox、隔离 WebKit mock/真实 OIDC、LCP、Token/审计扫描与退出码；镜像构建/推送/扫描证据见 `/var/lib/dosh/e2e-runs/20261104T062708Z-v241-audit/`；本机受控归档位于 `/var/lib/dosh/evidence/1e16c0fb923b/` 下的 `*-v241-final` 目录，双层 SHA-256 与凭据模式扫描通过 |
| 内存语义 | 缓冲按进程有界，重启或副本切换后历史可能为空/不同；不提供持久化、跨副本合并、告警或不可变保留，生产合规仍依赖 stdout 采集、企业 SIEM 与 WORM |
| 未复跑/未关闭 | 本候选未重跑 30 分钟 soak、rollback/restore、Metrics 三态、Loki/失败 Webhook 全矩阵；不得从 v2.40 继承。企业 IdP、受信 TLS、企业 SIEM、Edge/Safari 实机、CI 真跑、离机 WORM 和生产发布/回滚仍为 `pending` |

**实现落点**：`internal/platform/audit/history.go` 使用固定容量 slice、写锁和递增 ID，满容量后覆盖最旧记录；
`internal/modules/audithistory/app/service.go` 只允许管理员查询；HTTP handler 完成参数校验、默认 newest-first 和分页；
`web/src/features/audit/audit-page.tsx` 提供 URL 筛选、表格、状态徽标和详情弹窗。内存快照通过 `ctx` 检查取消，
历史写入与 stdout/Webhook 一起在请求路径执行但受固定容量和单把短锁限制；高频并发追加由 race 测试覆盖。

**判定**：v2.41 当前 digest 在隔离 Release、真实本地认证/OIDC、三浏览器受限容器、API 权限/查询矩阵、
容量 SLO、LCP 和静态门禁范围内关闭 WBS-4.8 的实验室功能验收。该功能不是持久审计系统，
也不会因界面可查询而降低 stdout/SIEM/WORM 的商用门禁要求；七个外部商用门禁保持 `pending`。

### 21.2.35 v2.43 WBS-5.8 镜像更新与 WBS-5.9 CronJob 暂停/恢复（2026-11-04）

| 项目 | 结果 |
|---|---|
| 候选 | `0.1.4-rc-cronjobsuspend-20261104T075420Z`，registry digest `sha256:779ecae706eab6d0645e7b486aac1923f07e79e4919a969307cc6eb0d5c33306`，image ID `sha256:4472383799e341e6b5065cfbd236214f89fbcb062ad6582cb02ea7c8b14c5c71` |
| 隔离 UI Release | `dosh-ui-e2e/dosh-ui-e2e` revision 15、2/2 Ready、NodePort `32629`；Deployment 固定 registry digest；`/healthz`、`/readyz` 200，未认证 overview 401 |
| 生产前后 | `dosh` 仍为 App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626`，未改动 |
| 认证夹具 | 使用现有 Session Key 生成的 30 分钟短会话验证 `/api/v1/session` 与 `updateImage`/`suspendCronJob` capability；未替换 `dosh-auth-limiter`、未输出 session key、Cookie 或 CSRF Token |
| 镜像更新 | 隔离 `dosh-e2e-target` 中 Deployment/StatefulSet/DaemonSet 的 `main` 容器均更新到独立测试 tag，rollout 完成且 Pod Ready；Deployment `sidecar` 仍为原镜像，未被连带修改 |
| 并发与幂等 | 旧 `resourceVersion` 加不同镜像返回 409，随后 kubectl 对账确认未覆盖；当前 `resourceVersion` 加同镜像重试返回 200，按幂等成功处理 |
| 不支持范围 | Job 与 CronJob 的镜像更新均返回 501；对 Deployment 调用 suspend 返回 501，且资源不变 |
| CronJob 暂停 | `spec.suspend=true`；暂停后的 75 秒窗口内已有 Job/Pod 的 UID 与 owner 集合完全一致，没有新 Job/Pod |
| CronJob 恢复 | 使用暂停前旧 `resourceVersion` 恢复返回 409 且仍保持 suspended；使用当前版本恢复返回 200，随后新 Job 出现并按调度继续 |
| 审计 | 直接读取两个副本的 `/api/v1/audit-events`：`update-image` 7 条 success 与 4 条 failure、`suspend` 1 条 success 与 1 条 failure、`resume` 1 条 success 与 1 条 failure；成功和失败均带 requestId |
| 工作负载写操作闭环 | `make e2e-workload-lifecycle` 在 `dosh-e2e-lifecycle` / NodePort `32630` 独立夹具完成预览、创建、1→2 扩容、镜像更新、重启和删除；生产 revision/NodePort/镜像指纹前后一致；详见 21.2.36 |
| Playwright 隔离 | `make playwright-check` 通过；runner 强制 `workers=1`、`retries=0`、每浏览器独立容器、私有 IPC、20 分钟硬超时和宿主级 `flock`；默认资源上限为 `6g/4 CPU/1g shm/2048 PIDs`，当前 WebKit 运行进一步限制为 `4g/2 CPU/1g shm/2048 PIDs`，本轮未提高并发或使用重试 |
| 当前源码 WebKit | 在隔离 libvirt runner `dosh-browser-runner`（8 GiB RAM、4 vCPU）的独立容器内完成 mock `37/37`，`workers=1`、`retries=0`，退出码 0；证据目录 `/var/lib/dosh/e2e-runs/20261104T081749Z-v243-webkit-mock/`，日志 SHA-256 `8cc4765a02d01fb1667318570d337a24c9353908147f3ede602f87eb3f56fc9a`。该结果只证明开源 WebKit 引擎路径，不等同于 Edge/Safari 实机 |
| 真实 OIDC 浏览器矩阵 | 当前 digest 在签名测试 IdP、隔离 OIDC Release revision 173 上完成 Chromium `16/16`、Firefox `16/16`、WebKit `16/16`，三浏览器均 `workers=1`、`retries=0`；Chromium/Firefox 在宿主隔离容器、WebKit 在 `dosh-browser-runner` 独立容器内以 `4g/2 CPU/1g shm/2048 PIDs` 串行执行。每浏览器 `6/6` OIDC 负向回调审计带 requestId，应用日志、ConfigMap/Deployment、Helm values 的 JWT/测试 Token 扫描 0 命中 |
| OIDC 浏览器证据 | 宿主矩阵 `/var/lib/dosh/e2e-runs/20261104T091212Z-v243-oidc-browser-host/`（Chromium 16/16、Firefox 16/16，`expected_callback_failures=12`、`denied_login_audits=12`）；WebKit 矩阵 `/var/lib/dosh/e2e-runs/20261104T091633Z-v243-oidc-browser-webkit/`（16/16，`expected_callback_failures=6`、`denied_login_audits=6`）；补充归档 `/var/lib/dosh/evidence/779ecae706ea/20261104T092210Z-v243-oidc-browser-matrix`，10 文件，manifest SHA-256 `e0bea388800f741f1a147dceb9c84bafde676400ef443fc26f1002603116660e` |
| 运行证据目录 | `/var/lib/dosh/e2e-runs/20261104T080036Z-v243-workload-write/`，含 API 响应、kubectl YAML/UID 对账、75 秒暂停观察、审计聚合、清理验证和 `summary.txt`；敏感模式扫描通过 |
| 候选构建证据 | `/var/lib/dosh/e2e-runs/20261104T075420Z-v243-candidate/`，含构建、registry manifest、推送、Trivy 日志、tag/digest/image ID 和源码绑定输出 |
| 源码绑定 | `deploy/release/bindings/source-binding-v243.txt`，209 个源码文件，二进制 `f5263715fec402533e7811618ebd3e4ed40b2e254f33d530d4d3cbddfa4ce439`，source tree `24b7f51a1c9822e2a492d3629c7dbb4f91005dbeb7614c23b5c253d856f73ea7` |
| 本地门禁 | `make verify` 通过；新鲜复跑 `go test -race -count=1 ./...`、Vitest `51/51`、生产构建和 bundle 预算（initial JavaScript gzip `349.54 KiB`），以及 `gofmt`、`go vet`、架构、OpenAPI、Helm、版本、Playwright 隔离、E2E image binding 和 release-gate schema 均通过。release gate 仅因七项外部证据保持 `BLOCKED` |
| 容量与 LCP | 1000 Pod / 200 工作负载：Pods P95 `20.235895 ms`、Workloads P95 `194.467732 ms`、Overview P95 `199.398259 ms`；容量请求后 Heap `28 MiB`、HeapInuse `31 MiB`。生产构建 Chromium LCP `692.0 ms`，低于 2.5 秒。运行目录 `/var/lib/dosh/e2e-runs/20261104T082700Z-v243-internal-gates/` |
| 回滚与恢复 | 隔离 `dosh-e2e-oidc/dosh-oidc-e2e` revision `169 → 170 → 171 → 172`；baseline、upgrade、rollback、restore 每阶段 health/ready 200、viewer read200/write403、operator target200/other403、admin scale200/nodes200、logout 401；最终恢复到 upgraded revision 且 digest 保持当前候选。证据 `/var/lib/dosh/e2e-runs/20261104T082951Z-v243-oidc-rollback/` |
| 30 分钟 soak | 通过：30 个事件、30 个事件 ConfigMap、51,346 个事件帧、28,447 行日志、5 次重连、0 应用错误、0 事件创建失败、0 流失败；57 次内存采样 `23 → 18 MiB`，峰值 `30 MiB`；覆盖 Pod 删除和应用滚动重启。证据 `/var/lib/dosh/e2e-runs/20261104T083133Z-v243-soak/` |
| Metrics 三态 | `/var/lib/dosh/e2e-runs/20261104T092409Z-v243-metrics/`：可用 CPU `2189m`、内存 `11324391424` bytes；Metrics Server 缩到 0 后 `metricsAvailable=false`、usage 为 0 且无陈旧值；恢复后 CPU `2275m`、内存 `11327913984` bytes；脚本 `exit_code=0`、`result=passed` |
| Kubernetes 429/5xx 重试 | `/var/lib/dosh/e2e-runs/20261104T092534Z-v243-kube-retry/`：429/500/502/503/504 各注入 2 次后恢复；持续 429 恰好 3 次并返回 `rate_limited`；`Retry-After` 约 2098 ms；写请求 503 仅 1 次且副本数保持 2；连接重置、客户端取消和恢复通过；生产前后摘要一致；`exit_code=0`、`result=passed` |
| 真实 Loki 审计 | `/var/lib/dosh/e2e-runs/20261104T092947Z-v243-loki/`：私 CA、Bearer-only、HMAC-only、组合认证、业务成功/拒绝、失败审计、瞬时/持续 503、1s 超时、队列满、只读拒绝全部通过；队列满时 200 次业务请求未被阻塞；20/20 SIGTERM 排空；Loki 检索 59 条事件、34 个 requestId、`job/service` 标签和租户隔离通过；Token/HMAC/Secret 泄露扫描均为 0；`exit_code=0`、`result=passed` |
| 受控证据归档 | 主归档 `/var/lib/dosh/evidence/779ecae706ea/20261104T090311Z-v243-current-candidate-lab`，585 个文件、20,451,918 bytes，manifest `f5dbfb5af524ed5fe5778873de1b8098f6cd9c9f69e6494f6a1e4c15d0d0dc15`；OIDC 补充归档 10 文件，manifest `e0bea388800f741f1a147dceb9c84bafde676400ef443fc26f1002603116660e`；Metrics/retry 补充归档 20 文件，manifest `9e58e69dd85b5e36aa502c5e32b7c28f190c9bb88537853e7066eebbad32331e`；真实 Loki 补充归档 9 文件，manifest `7fa723805340ced9942a416e26c7c3ad2d0f2e1194614906ca4cede15e7af587`；workload lifecycle 补充归档 28 文件，manifest `4b5205ba2ed5935de8397e0c7383695c69b62c8cc0e8f62336b4deca1b16022a`。五组归档均完成双层校验和与敏感模式扫描，fixture/证书/请求响应转储已排除；该归档只在本机，不满足离机 WORM |
| 文档绑定 | `deploy/release/bindings/documentation-binding-v243.txt` 绑定 EARS、运行手册、README 和发布说明；`commercial-gates.json` 的 candidate 已切换到当前 tag/digest，七项外部门禁仍为 `pending` |
| 夹具说明 | 首次使用 registry manifest digest 作为 StatefulSet/DaemonSet 目标镜像时，kubelet 无法解析该 digest 引用；改用同一镜像的独立 tag `v243-workload-target` 后全部工作负载 Ready。该失败只记录为测试夹具问题，API 写入、rollback 和 409 行为不受影响 |
| 未关闭 | 离机 WORM、Edge/Safari 实机、企业 IdP、受信 TLS、企业 SIEM、CI 真跑和生产发布/回滚仍未完成。七个外部商用门禁保持 `pending` |

**判定**：v2.43 历史候选在真实隔离 Release、kubectl 对账、两副本审计检索、完整工作负载写操作生命周期、受限串行 Playwright、真实 OIDC 三浏览器矩阵、Metrics 三态、429/5xx 重试、真实 Loki 审计、完整内部静态/容量/LCP/回滚/soak 门禁和本机受控归档范围内完成实验室功能验收。该结论不包含生产发布、Edge/Safari 实机或任何外部商用门禁；不得据此宣称完整可商用或已发布生产。

### 21.2.36 v2.43 WBS-5.1 工作负载写操作端到端闭环（2026-11-04）

**目的**：把此前分散在 OIDC RBAC、Deployment 创建向导、镜像更新和 CronJob 用例中的局部断言，
合并为一条从预览到删除的真实写操作链路，并证明每一步的 API 结果、Kubernetes 状态、Pod 生命周期
和审计记录一致。该用例只使用独立 namespace/Release/NodePort，不接触生产 `dosh`。

| 项目 | 结果 |
|---|---|
| 候选 | `0.1.4-rc-cronjobsuspend-20261104T075420Z@sha256:779ecae706eab6d0645e7b486aac1923f07e79e4919a969307cc6eb0d5c33306` |
| 执行入口 | `make e2e-workload-lifecycle`，实现为 `scripts/e2e-workload-lifecycle.sh`；单进程串行执行，不启动 Playwright |
| 隔离夹具 | `dosh-e2e-lifecycle/dosh-lifecycle-e2e` revision 1、1/1 Ready、NodePort `32630`；目标 namespace `dosh-e2e-lifecycle-target`；测试 IdP `9446`；运行完成后 Release、namespace、ClusterRole/Binding 和 IdP 均已清理 |
| 生产保护 | 前后指纹一致：App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626`；Deployment UID、generation、resourceVersion 和镜像均未改变 |
| Preview | operator 请求返回 200；YAML 包含 Deployment、名称、镜像和环境变量；kubectl 确认未创建资源 |
| 拒绝与非法输入 | viewer 创建返回 403；`replicas=1001` 返回 400；两种请求均未留下 Deployment |
| 创建与冲突 | operator 创建返回 201；kubectl 对账副本数、容器端口、CPU/内存 request/limit、env 和 label；1/1 Ready；重复创建返回 409 且原 UID 不变 |
| 扩容与冲突 | 使用创建时 `resourceVersion` 从 1 扩容到 2，2/2 Ready；再用旧版本请求 3 副本返回 409，2 秒后副本数仍为 2 且 generation 未变化 |
| 镜像更新与旁容器 | 先通过 kubectl 加入稳定 sidecar；operator 使用当前 `resourceVersion` 更新 `app` 容器，2/2 Ready，`app` 为目标测试镜像且 sidecar 不变；旧版本回写源镜像返回 409，目标镜像和 generation 均未被覆盖 |
| 重启 | viewer 返回 403；operator 返回 200；`kubectl.kubernetes.io/restartedAt` 更新且原 Pod UID 全部消失，替换 Pod 全部 Ready |
| 删除 | viewer 返回 403；非法 propagation 返回 400 且资源保留；operator 使用 `Background` 返回 202；Deployment 与 owned Pod 最终消失，详情返回 404 |
| 审计 | 查询目标 namespace 的 20 条审计记录，其中该 Deployment 生命周期记录 13 条：5 success、5 failure、3 denied；覆盖 create/scale/update-image/restart/delete，13/13 均带非空 `requestId` |
| 证据 | `/var/lib/dosh/e2e-runs/20261104T094617Z-v243-workload-lifecycle/evidence`；归档 `/var/lib/dosh/evidence/779ecae706ea/20261104T094801Z-v243-workload-lifecycle`，28 个文件，manifest SHA-256 `4b5205ba2ed5935de8397e0c7383695c69b62c8cc0e8f62336b4deca1b16022a`，双层校验通过 |
| 敏感信息 | 证据扫描未发现 Cookie、CSRF、Session Key、Bearer、私钥或测试 Secret；fixture 元数据中的 Cookie/session 文件未进入归档 |

**判定**：WBS-5.1 的创建、扩容、镜像更新、重启和删除在 v2.43 历史 digest 的隔离集群上形成完整闭环，
并且成功、业务失败和无权限拒绝都能由审计记录关联到同一资源。该结论仍属于实验室验收；生产发布、
企业 IdP、受信 TLS、企业 SIEM、Edge/Safari 实机、CI 真跑和离机 WORM 未完成。

### 21.2.37 v2.44 WBS-4.10 日志流结构化与终止语义（2026-11-04）

**目的**：把日志接口从无类型纯文本提升为可机器解析、可观测结束原因的有界帧协议，补齐
`LOG-E-002`、`LOG-N-001` 和 `NFR-E-002` 在流中错误、请求关联和前端终止状态上的缺口，同时保留
HTML/ANSI 只能作为文本显示的安全边界。

| 项目 | 结果 |
|---|---|
| 候选 | `0.1.4-rc-logframes-20261104T102214Z@sha256:e4a27d8b153b8f96ad52ba5f82f92fa3ddaf22e13bc69432c72d16e8ea449439`，image ID `sha256:1525e8d063388e3f68e5a66a9697c97405527f67fb04fd4cc523a436d76c9f23` |
| 实现落点 | `internal/modules/workloads/http/handler.go`、`internal/platform/httpx/httpx.go`、`web/src/features/workloads/log-stream.ts`、`log-viewer.tsx`、`api/openapi.yaml` |
| 协议 | `application/x-ndjson; charset=utf-8`；`log` 帧承载 UTF-8 日志块；正常 EOF 发 `end`；流中失败发 `error`（稳定 `code`、安全 `message`、`requestId`）并终止；响应头前错误仍为标准 JSON Problem；连接在无 `end`/`error` 时关闭由前端判定为断流 |
| 边界与安全 | 服务端保留最多一个不完整 UTF-8 rune，避免读边界破坏中文；新增跨两次 `Read` 切断“日”字节的 handler 用例；前端按行增量解析，继续经过 ANSI/OSC/DCS/C0-C1 净化器并以 `<pre>` 文本节点渲染；日志缓冲仍限制为 1.5 MB 且不写入浏览器持久存储 |
| 前端状态 | 显示终止错误和请求 ID；无结束帧时显示“日志流意外中断（未收到结束帧）”，保留“重新加载”手动重连；空闲、读取、跟随、结束和错误状态均有可见文本及 `role=status` |
| 单元/集成 | 新增协议分帧、畸形帧、结构化错误、无 `end` 断流、手动重连、跨帧 ANSI 和 HTML 惰性文本测试；Go handler 覆盖 `log`→`end`、跨读取 UTF-8 切分、流中 `dependency_unavailable` 错误帧、`requestId` 和响应头前 `not_found` JSON Problem |
| 本地门禁 | `make verify`、Vitest `56/56`、mock Chromium/Firefox 各 `37/37`、新鲜 `go test -race -count=1 ./...`、容量 SLO（Pods/Workloads/Overview P95 `21.958462/190.456782/208.729352 ms`，Heap `28 MiB`）、LCP `456.0 ms`、bundle `350.02 KiB`；Playwright 全程 `workers=1`、`retries=0`、独立容器和宿主 `flock` |
| 安全扫描 | `govulncheck v1.9.0` 扫描 46 个模块、38 个根包，`No vulnerabilities found`；官方 registry `npm audit --omit=dev` 为 0；新 registry digest 的 Trivy `2026-11-04T10:29:20Z` 对 Debian 12.15 和 Go binary 均为 0 HIGH/CRITICAL |
| 集群夹具 | `make e2e-log-stream` 创建 `dosh-logs-e2e/dosh-e2e-logs` revision 1、2/2 Ready、NodePort `32631`，本地临时管理员 Secret、临时 TLS 和 `dosh-e2e-logs-target`；完成后 namespace、Release、RBAC 和 Secret 全部删除 |
| 集群结果 | 正常非 follow 流以 `end` 结束；缺失容器返回 4xx Problem；未认证返回 401 Problem；删除 Pod 后 follow 流以 `end`/`error` 终止；HTML/ANSI 测试字节未因 JSON 帧转换丢失；`requestId` 非空 |
| 生产保护 | 前后指纹一致：App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626`；生产 UID、generation 和镜像未改变 |
| 证据 | `/var/lib/dosh/e2e-runs/20261104T102214Z-v244-final-log-stream/`；归档 `/var/lib/dosh/evidence/e4a27d8b153b/20261104T103215Z-v244-final-log-stream`，26 个文件，manifest SHA-256 `ce20093711d6e5a8f054aca147faa38aa0f6fd98efd73f42010d2a7c99d070cb`，双层校验和与凭据模式扫描通过 |
| 后续矩阵 | 真实 OIDC Chromium/Firefox/WebKit、rollback/restore、Metrics、retry、真实 Loki、30 分钟 soak、工作负载生命周期和 CronJob 详情已在同一 v2.44 digest 通过，见 21.2.38；Edge/Safari 实机仍属外部门禁 |

**判定**：WBS-4.10 在 v2.44 历史 digest 的真实隔离集群和串行浏览器门禁范围内关闭日志流的结构化终止、
请求关联、UTF-8 安全和手动重连缺口。该结论属于实验室增量验收，不改变外部商用门禁状态，也不代表
生产已升级。

### 21.2.38 v2.44 内部商用验收矩阵复跑（2026-11-04）

**目的**：在日志协议变更后的同一不可变 candidate digest 上复跑此前只绑定 v2.43 的身份、回滚、
Metrics、Kubernetes 重试、真实 Loki、长连接、工作负载写入生命周期和 CronJob 详情，消除跨 digest
继承，同时验证 Playwright 严格串行和资源隔离后的稳定性。

| 项目 | 结果 |
|---|---|
| 候选身份 | `0.1.4-rc-logframes-20261104T102214Z@sha256:e4a27d8b153b8f96ad52ba5f82f92fa3ddaf22e13bc69432c72d16e8ea449439`；源码绑定 `source-binding-v244.txt`，211 文件，SHA-256 `ee2f8869…ec86` |
| 浏览器执行策略 | `workers=1`、`retries=0`；Chromium/Firefox 分别启动独立受限容器，WebKit 在隔离 libvirt runner 内单项目执行；统一内存 `6g`、CPU `4`、私有 `/dev/shm=1g`、PID `2048`、20 分钟硬超时，宿主 `flock` 禁止任何重叠 |
| 真实 OIDC | 隔离签名测试 IdP；Chromium `16/16`、Firefox `16/16`、WebKit `16/16`；三身份 RBAC、Secret 无权限、写按钮权限、审计历史、主题/axe/键盘和 RP logout 全部通过 |
| OIDC 负向与泄露 | 18 条 denied 回调审计（每浏览器 6/6）均带 `requestId`；缺失/篡改/不匹配/过期 state、nonce mismatch、code 重放和开放重定向均拒绝；浏览器 URL/DOM/Web Storage、应用日志、ConfigMap、Deployment 和 Helm values 的 Token 扫描 0 命中 |
| 升级/回滚/恢复 | 隔离 OIDC Release revision `181→182 upgrade→183 rollback→184 restore`；每个阶段均验证 `/healthz`/`/readyz` 200、viewer 只读、operator 目标命名空间可写/其他 403、admin 扩缩容与节点读取、logout 401 |
| Metrics | 可用：CPU `2771m`、内存 `11510038528`；不可用时 `metricsAvailable=false`、usage 归零且无陈旧值；恢复：CPU `2153m`、内存 `11576762368`；均通过真实 Metrics Server 对账 |
| Kubernetes retry | 429/500/502/503/504 各连续失败 2 次后恢复；持续 429 恰好 3 次尝试；`Retry-After` 实测约 `2098 ms`；写请求 503 仅发送 1 次且副本数不变；连接重置、客户端取消和恢复通过 |
| 真实 Loki 审计 | 59 条事件、34 个 requestId；SIGTERM 前 20 个唯一 requestId 全部到达并经 Loki 检索确认（20/20）；租户隔离、私 CA、队列压力、1s 超时/503 矩阵通过；凭据扫描 0 |
| 30 分钟 soak | 30 个事件、50,577 个事件帧、26,577 行日志、5 次重连、`app_errors=0`、`stream_failures=0`；内存 `22→18 MiB`、峰值 30 MiB；覆盖 Pod 删除和应用滚动重启 |
| 工作负载生命周期 | 独立 `dosh-lifecycle-e2e`/NodePort `32630`：preview 不落盘、viewer 403、非法输入 400、创建 201、重复 409、扩容 1→2、旧 resourceVersion 409、镜像更新且 sidecar 不变、重启替换全部 Pod UID、删除后详情 404；13/13 生命周期审计带 requestId |
| CronJob 详情 | revision 188 隔离 Release 上 HTTP 200；无 selector 时 `selector={}`、`pods=[]`；suspended 状态正确；清理后 NotFound |
| 生产保护 | 全程保持 App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626`；生产 UID、generation、镜像和副本未改变 |
| WebKit 隔离复跑 | mock `37/37`、真实 OIDC `16/16`；runner `dosh-browser-runner` 8 GiB/4 vCPU，容器 `6g/4 CPU/1g shm/2048 PIDs`；runner 源码哈希与宿主一致；真实 OIDC 为 6/6 denied 审计带 requestId、Token 扫描 0；完成后停止 runner 并清理临时证书工作目录 |
| 证据 | 综合归档 `/var/lib/dosh/evidence/e4a27d8b153b/20261104T112513Z-v244-internal-matrix-r2`，553 文件、21,266,851 字节，manifest SHA-256 `9f9560d2a7d0e30dd0c163e2ed8a5d674ee93f2bbef7dcd4d1f90c5c569472b9`；WebKit 补充归档 `/var/lib/dosh/evidence/e4a27d8b153b/20261104T114204Z-v244-webkit-r2`，11 文件，manifest SHA-256 `be5d5c1f4a15f041274869a83e24573ccf501ef0a05d9f60da6ccf9c8fc9f653`；均通过双层校验，敏感 fixture/header/body/证书未进入归档 |
| 未覆盖 | WebKit 容器不等同 Safari。Edge/Safari 实机、企业 IdP、受信 TLS、企业 SIEM/保留/告警、GitHub Actions 真跑与分支保护、离机 WORM、生产变更授权仍未关闭 |

**失败诊断隔离**：`20261104T103633Z-v244-oidc-browser` 因默认双浏览器 denied 计数仍硬编码为 18 而失败，
修正脚本为默认 12、显式 WebKit 三浏览器时 18；`20261104T104400Z-v244-rollback` 因测试夹具就绪竞态失败，
改为 60×0.5s 有界 health/ready 循环。上述两个目录只作诊断，不作为通过证据归档。

**判定**：v2.44 历史 digest 的内部验收矩阵已复跑并受控归档，跨 digest 继承缺口关闭。该结论不改变
七个外部商用门禁状态，也不授权生产升级或回滚；WebKit 容器与 Edge/Safari 实机结论必须保持分开表述。

### 21.2.39 v2.45 Secret 明文解码与 Playwright 资源收紧（2026-11-04）

**目的**：修复 Secret“查看明文”把 Kubernetes `data` 字段的 Base64 编码直接当成明文展示的语义
缺陷，同时把浏览器验收固定为资源受限、完全串行和相互隔离的执行方式。物理宿主机故障诊断不再
属于本版本的功能实施范围。

| 项目 | 结果 |
|---|---|
| 候选 | `0.1.4-rc-secretdecode-20261104T120606Z@sha256:c7eff34a80ca1be0de50d12cd27b97d52ca54029329ed7b19e4bcc70c283ab06`，image ID `051c50c635fe655f1943ef7a551a99ed56671bb890383044e042adf6793fedbf` |
| 实现落点 | `web/src/lib/secret-value.ts`、`web/src/lib/secret-value.test.ts`、`web/src/features/configuration/configuration-page.tsx`、`web/src/app.integration.test.tsx`、`web/e2e/dosh.spec.ts` |
| 解码语义 | 有效 Base64 且解码结果为无禁止控制字符的 UTF-8 文本时展示并复制明文；合法 Base64 但非 UTF-8 或含禁止控制字符时标记“Base64（二进制）”并保留原值；非法 Base64 标记“无法解码”并安全回退原始响应；支持 URL-safe 字母表、空白和缺失 padding |
| 安全边界 | 仅在前端已确认 reveal 成功且用户完成名称二次确认后处理响应；不写浏览器持久存储、不记录请求/响应敏感数据；二进制和非法值不会误显示为可读明文；复制按钮复制当前实际展示语义对应的值 |
| 单元/集成 | 新增空白、URL-safe、缺 padding、空值、UTF-8 多行、二进制控制字节、非法 Base64 解析测试；Vitest 集成断言文本值不再显示 Base64、二进制仍显示 Base64；Playwright mock 覆盖 Secret 解码主路径 |
| 本地门禁 | `make verify`、Vitest `62/62`、全量 `go test -race -count=1 ./...`、TypeScript、生产前端构建、mock Chromium/Firefox/WebKit 各 `38/38`、`make playwright-check`、`make evidence-check` 和 `make release-gate-schema-check` 通过；Playwright 使用 `workers=1`、`retries=0`、每浏览器独立容器和宿主 `flock` |
| Playwright 资源 | 默认 `memory=4g`、`cpus=2`、私有 `/dev/shm=1g`、`pids=2048`、20 分钟硬超时；硬上限 `6g/4 CPU/1g shm/2048 PIDs`；CI 矩阵 `max-parallel: 1`，每个浏览器单独容器；禁止 `--workers`、正数 `--retries` 和 `--ipc=host` 覆盖 |
| 源码绑定 | `deploy/release/bindings/source-binding-v245-secretdecode.txt`，213 个源文件，二进制 SHA-256 `68c9ec5d5a8bf8e4bd7934d452fa539693c3b1feae4d6c3b6c0d81012e4bc9cf`，源码树 SHA-256 `74a3a020889c318f2345e12ee4f251582a15200a4542267f741a94f447020040` |
| 真实 OIDC | 隔离 Release `dosh-e2e-oidc/dosh-oidc-e2e` 为 revision 192、1/1 Ready、NodePort `32627`，当前仍保留，镜像固定为候选 digest；Chromium/Firefox/WebKit 各 `16/16`，host 两浏览器 `12/12` denied 审计、WebKit `6/6` denied 审计均带 `requestId`；Token、ConfigMap/Deployment 和 Helm values 扫描 0 命中 |
| 隔离复核 | Audit/Reveal 矩阵通过：真实 Secret reveal 200 且审计 success，viewer 拒绝、私 CA、Token/HMAC、瞬时/持续 503、超时恢复、队列压力、20/20 SIGTERM 排空，token/hmac/secret 泄露扫描均为 0；容量 P95 `20.60/189.37/202.47 ms`、LCP `732.0 ms`、bundle `350.62 KiB`、govulncheck 0、生产 npm 审计 0、Trivy Debian/Go 0 HIGH/CRITICAL |
| 证据 | `/var/lib/dosh/evidence/c7eff34a80ca/20261104T125435Z-v245-internal-matrix`，33 文件、2,837,535 字节，manifest SHA-256 `e71c466b80159f51022f3addd0bcbc0c69393dc22d3f0b9b8bdf6a951c9964c6`，manifest 双层校验通过；凭据、Cookie、Session、证书和原始敏感请求/响应未进入归档 |
| 当前未覆盖 | Edge/Safari 实机、企业 IdP、受信 TLS、企业 SIEM/保留/告警、CI 真跑与分支保护、离机 WORM、生产变更授权和生产升级/回滚尚未完成；v2.44 的历史结果及归档不得继承 |
| 生产保护 | 本轮只构建并验证新候选，未执行生产 Helm 或 Kubernetes 写操作；生产仍为 App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626` |

**判定**：Secret 明文查看的用户可见语义缺陷已修复，并有单元、集成、mock 浏览器和真实集群
Audit/Reveal 覆盖；真实 OIDC 三浏览器矩阵和发布性能/安全门禁已在当前不可变 digest 上通过。
Playwright 并发与资源边界固定为串行隔离。该结论代表当前候选的实验室内部门禁完成，但不改变
七个外部商用门禁状态，也不构成完整商用放行或生产发布授权。

### 21.2.40 v2.45 Metrics 三态复验（2026-11-04）

**目的**：在当前不可变候选 digest 上重新验证真实 `metrics.k8s.io` 的可用地展示、依赖不可用时
降级以及恢复后的自动回补，确认结果不依赖 v2.44 历史证据。

| 项目 | 结果 |
|---|---|
| 候选 | `0.1.4-rc-secretdecode-20261104T120606Z@sha256:c7eff34a80ca1be0de50d12cd27b97d52ca54029329ed7b19e4bcc70c283ab06` |
| 隔离环境 | `dosh-e2e-oidc/dosh-oidc-e2e` revision 193、1/1 Ready、NodePort `32627`；镜像固定为当前候选 digest；生产 `dosh` 未改动 |
| 可用态 | `metricsAvailable=true`；CPU `2152m`；内存 `11501375488` bytes；节点有真实指标；`metrics-demo` Pod 有真实指标 |
| 不可用态 | Metrics Server 临时缩容到 0 后，`metricsAvailable=false`；总览 usage 为 0 且无陈旧值；所有节点 `hasMetrics=false` |
| 恢复态 | Metrics Server 恢复为 1/1 Ready 后，无需重启 dosh，`metricsAvailable=true`；CPU `2141m`；内存 `11518554112` bytes |
| 自动验收 | `bash scripts/e2e-metrics.sh` 退出码 0；`kubectl top` 与 dosh DTO 同量级对账；脚本清理 demo Deployment，Metrics Server 恢复 1/1 |
| 证据 | `/var/lib/dosh/evidence/c7eff34a80ca/20261104T131625Z-v245-metrics-r1`，4 文件，manifest SHA-256 `d2bae3531ddd533e712e2a6fc9e8fb558db74462d89c3a02d20fcc7f393348b8`，双层校验通过；未归档 Cookie、证书、fixture 元数据或敏感请求/响应 |
| 生产保护 | 运行前后生产均为 App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626` |
| 当前未覆盖 | 生产或预发目标 Metrics 拓扑；rollback/restore、429/5xx retry、真实 Loki、30 分钟 soak、工作负载生命周期/CronJob 仍需在 v2.45 复验 |

**判定**：WBS-6.2 的 Metrics 三态在当前 v2.45 隔离候选上已通过实验室验收。该结果不关闭生产
Metrics 拓扑门禁，也不替代其余 v2.45 专项和七个外部商用门禁。

### 21.2.41 v2.45 Kubernetes 429/5xx 有界重试复验（2026-11-04）

**目的**：在当前候选 digest 上通过真实 API Server 前置故障代理复验读请求有限重试、写请求不重放、
`Retry-After` 边界、连接重置和客户端取消，替换 v2.44 历史 retry 证据。

| 项目 | 结果 |
|---|---|
| 候选 | `0.1.4-rc-secretdecode-20261104T120606Z@sha256:c7eff34a80ca1be0de50d12cd27b97d52ca54029329ed7b19e4bcc70c283ab06` |
| 隔离环境 | `dosh-e2e-oidc/dosh-oidc-e2e` revision 194、1/1 Ready、NodePort `32627`；测试代理 `kube-fault-proxy-v245-20261104T131909Z`；生产 `dosh` 未改动 |
| 瞬态故障 | `GET /api/v1/nodes` 分别注入 429、500、502、503、504 各 2 次；每次均在第三次尝试恢复 200，请求计数为故障 2 次、成功 1 次 |
| Retry-After | 429 两次并携带 `Retry-After: 1`，约 `2099 ms` 后恢复 200；未突破单次退避上限 |
| 持续 429 | 持续故障恰好尝试 3 次，最终返回 429 和 `code=rate_limited`，未无限重试 |
| 写请求 | `PUT .../scale` 注入 503，dosh 返回 503；代理仅收到 1 次 PUT，副本数保持 `2→2`，未自动重放 |
| 网络异常 | 连接重置两次后自动恢复 200；客户端在 1 秒后取消，服务端在退避中停止，未继续重试 |
| 环境恢复 | 故障代理删除；dosh 的 `DOSH_KUBE_API_SERVER`/`DOSH_INSECURE_SKIP_VERIFY` 临时环境覆盖已移除并重新滚动；NetworkPolicy/CNP 恢复；Metrics Server 仍 1/1 Ready |
| 证据 | `/var/lib/dosh/evidence/c7eff34a80ca/20261104T132049Z-v245-retry-r1`，5 文件，manifest SHA-256 `27282a7d1986be6cdceaf442f406f230146959bf55e050d071cec7875a95815c`，双层校验通过；Cookie、Token、请求/响应头和 fixture 元数据未进入归档 |
| 生产保护 | 运行前后生产均为 App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626` |
| 当前未覆盖 | 生产/预发同等故障注入；rollback/restore、真实 Loki、30 分钟 soak、工作负载生命周期/CronJob 仍需在 v2.45 复验 |

**判定**：Kubernetes 429/5xx 有界重试在当前 v2.45 隔离候选上已通过实验室验收。生产或预发同等
拓扑仍需在发布变更中复验，本结论不关闭生产发布或七个外部商用门禁。

### 21.2.42 v2.46 节点 Cordon/Uncordon 并发修复与完整内部验收（2026-11-04）

**目的**：修复对运行中节点执行 Cordon/Uncordon 时，完整 Node `Update` 与 kubelet 状态心跳产生
`resourceVersion` 冲突并返回 409 的缺陷。节点调度开关只需修改 `spec.unschedulable`，不应提交
完整 Node 对象。

| 项目 | 结果 |
|---|---|
| 候选 | `0.1.4-rc-nodeuncordon-20261104T132617Z@sha256:2135c2aea864c25975a172d35c7973c1f15c76db266862cd09f7bc0253307bfd`；image ID `sha256:7394e1d6a8436c0b854b63b253d7a085a97dc1d364b3a96b462c9a7b352d25a9` |
| 实现落点 | `internal/platform/kube/nodes.go`、`internal/platform/kube/resources_test.go` |
| 修复语义 | `SetNodeScheduling` 仅在状态变化时发送 `{"spec":{"unschedulable":<bool>}}` JSON merge patch；不再发送完整 Node，不包含 `resourceVersion` 或 `status` |
| 自动回归 | 定向 Kube 测试、全量 `go test -race -count=1 ./...` 通过；测试断言只产生一次 patch、不产生 update，且 patch 不含 `resourceVersion`/`status` |
| 隔离 RBAC 矩阵 | viewer/operator Cordon 均为 403；admin Cordon 与 Uncordon 均为 200，随后 `spec.unschedulable` 恢复预期状态 |
| 隔离环境 | `dosh-e2e-oidc/dosh-oidc-e2e` 验收时 revision 209、当前复核 revision 210、1/1 Ready、NodePort `32627`；镜像固定为当前候选 digest；生产 `dosh` 未改动 |
| Rollback/restore | 隔离 Release revision `198→199→200→201`，每个阶段完成 health/ready、三身份登录/RBAC 和 logout 对账；归档 manifest SHA-256 `91c7b6fed201982d6ec02f48b7ff10640abb27f68b2efd79729e3992e4cc0cfb` |
| Metrics 三态 | 可用态 CPU `2195m`、内存 `11654705112` bytes；Metrics Server 不可用时归零且无陈旧值；恢复后 CPU `2112m`、内存 `11641643008` bytes；归档 manifest SHA-256 `b8e8f886bc87bde3525f6ed8719dcbff83c6c7381b4f6e6aa935ea89032e7500` |
| 429/5xx retry | 五类瞬时故障各两次后恢复；持续 429 恰好尝试 3 次；`Retry-After` 约 2100 ms；写请求仅 1 次且副本保持 `2→2`；归档 manifest SHA-256 `507f38b25aa1ca4f94fec60d70ecd9df2b64b1d1e7ee1bec7f0adfb913c8ae0d` |
| 工作负载生命周期 | preview 不落盘、创建 201、重复创建 409、扩容、镜像更新、重启、删除、授权与 13 条生命周期审计全部通过；归档 manifest SHA-256 `963e3631caf8b78cf2f8437b8cecad7054a9439ef2b3ea0370353abf7ac83d8e` |
| CronJob 详情 | 空 selector 返回 200，`selector={}`、`pods=[]`、`status=Suspended`，删除后 NotFound；归档 manifest SHA-256 `5eb54780274637767d5c3c2b94535fdb98e097188f47cb4a55aae0d2add80729` |
| 本地与前端门禁 | `make verify`、Vitest `62/62`、全量 race、架构/API/Helm/版本/Playwright 隔离/E2E digest 门禁全部通过；TypeScript 与生产构建通过 |
| 容量与首屏 | 容量 P95 Pods/Workloads/Overview `21.418817/193.223848/208.475119 ms`，heap `28 MiB`；生产构建 LCP `716.0 ms`；initial JS gzip `350.62 KiB`，低于 500 KiB 预算 |
| mock 浏览器矩阵 | Chromium/Firefox 在宿主隔离容器、WebKit 在独立 libvirt runner 串行执行，各 `38/38`；全部 `workers=1`、`retries=0`，axe 0 serious/critical |
| 真实 OIDC 浏览器矩阵 | Chromium/Firefox/WebKit 各 `16/16`；每浏览器 6/6 callback failure 与 6/6 denied 登录审计带 requestId；Token/URL/DOM/Web Storage/响应体扫描 0；Release 镜像为候选 digest |
| Audit/Reveal 与真实 Loki | 隔离 Audit/Reveal 覆盖私 CA、Token/HMAC、业务成功/拒绝、失败审计、瞬时/持续 503、超时恢复、队列压力、只读拒绝、20/20 SIGTERM 排空；Loki 59 事件、34 requestId、`job/service` 标签和租户隔离通过；Token/HMAC/Secret 泄露扫描 0 |
| 30 分钟 soak | 30 条事件、60,039 事件帧、26,553 行日志、5 次重连、0 app error、0 stream failure；内存 `24→18 MiB`、峰值 34 MiB；覆盖 Pod 删除和应用滚动重启 |
| 安全扫描 | govulncheck 0；生产 npm audit 0；Trivy Debian 12.15 与 Go binary 0 HIGH/CRITICAL。Trivy 使用固定 cache 的 `--skip-db-update`，数据库更新时间与 SHA-256 已记录在归档 |
| 最终证据归档 | `/var/lib/dosh/evidence/2135c2aea864/20261104T145034Z-v246-final-internal-matrix-r1`，599 文件，manifest SHA-256 `d761698e13819c4491876a7ee6f905ba37f85d589a895ec494215fab3c6aaff9`，双层校验通过 |
| 生产保护 | 运行前后生产均为 App `0.1.2` / Chart `0.1.4` / revision 12 / 2/2 Ready / NodePort `32626` |
| 当前未覆盖 | 企业 IdP、受信公共 CA TLS、生产 SIEM 保留/告警、Edge/Safari 实机、GitHub Actions 真跑与分支保护、离机 WORM、生产变更授权仍为七个外部阻塞；生产发布/回滚未执行 |

**判定**：v2.46 节点调度并发缺陷已修复，当前不可变 digest 的内部验收矩阵已全部完成并受控归档。
该结论关闭实验室内部验收，但不关闭七个外部商用门禁，也不授权生产升级；生产仍保持 revision 12、
App `0.1.2`、NodePort `32626` 和 2/2 Ready。

### 21.3 集群运行证据（`0.1.1/0.1.3` 历史基线，2026-11-02）

| 验收项 | 证据摘要 | 结论 |
|---|---|---|
| Release 名称 | `helm list -n dosh` 显示 Release `dosh` | 通过 |
| Release revision | 最终 revision 10，chart `dosh-0.1.3`，app version `0.1.1` | 通过 |
| 命名空间 | `kubectl -n dosh get deploy,svc,pod,pdb,sa` 全部位于 `dosh` | 通过 |
| 启动版本 | 日志记录 `version=0.1.1`、`commit=local-20261102`、TLS 开启、监听 `:32626` | 通过 |
| 副本与调度 | 2/2 Ready；分别位于 `worker-0003`、`worker-0005` | 通过 |
| NodePort | Service 输出 `32626:32626/TCP` | 通过 |
| 镜像 | `192.168.122.10:5000/dosh/dosh:0.1.1`；kubelet 记录的 digest 与 registry digest 均为 `sha256:a326a48a...3080e5a0` | 通过 |
| HTTPS 与安全头 | HTTP/2 200；HSTS、CSP、nosniff、DENY、Referrer-Policy 均存在 | 通过 |
| 健康检查 | `/healthz` 200；`/readyz` 200 且 Kubernetes `ok` | 通过 |
| 未认证访问 | `/api/v1/overview` 返回 401 统一 Problem | 通过 |
| 登录与 Cookie | 登录 200；Cookie 为 HttpOnly、Secure、SameSite=Strict；响应返回 CSRF token | 通过 |
| CSRF | 无 CSRF 写请求 403 | 通过 |
| 只读 API | overview/nodes/namespaces/workloads/pods/events/services/configmaps/secrets 等均 200 | 通过 |
| 空依赖降级 | ingress/storageclass/PV/PVC 返回空列表，不导致白屏 | 通过 |
| 日志流 | 真实 CoreDNS Pod 日志请求 200，返回文本 | 通过 |
| Events SSE | 连接收到 `: connected`，创建临时 ConfigMap 后收到真实 `event` 帧 | 通过 |
| Secret reveal | 二次确认后 200；`Cache-Control: no-store`；审计记录 success | 通过 |
| SPA fallback | 登录后请求 `/pods` 返回 HTML；未知 `/api/v1/*` 返回 404 Problem | 通过 |
| YAML Dry Run | ConfigMap 与 Deployment dry-run 均 200，且不产生 Kubernetes 资源 | 通过 |
| YAML Apply | ConfigMap 数据实际写入；临时 Deployment 实际创建并 Ready | 通过 |
| 扩容 | 临时 Deployment 1→2；Kubernetes spec 与 readyReplicas 均变为 2 | 通过 |
| 并发冲突 | 使用旧 resourceVersion 扩容返回 409 | 通过 |
| 滚动重启 | 返回 200，`restartedAt` annotation 已写入，rollout 完成 | 通过 |
| 删除 | 返回 202；Deployment 随后消失 | 通过 |
| 测试资源清理 | `dosh-e2e-smoke` 命名空间已删除 | 通过 |
| 安全上下文 | UID/GID 65532、非 root、readOnlyRootFilesystem、drop ALL、RuntimeDefault | 通过 |
| 资源配额 | requests 100m/128Mi，limits 500m/512Mi | 通过 |
| PDB | `minAvailable: 1`，`ALLOWED DISRUPTIONS: 1` | 通过 |
| NetworkPolicy | `dosh` 策略默认拒绝；仅入口 TCP 32626；Cilium 模式下只保留 DNS 出口，不开放 `0.0.0.0/0` API 出口 | 通过 |
| Cilium 策略 | `dosh-api-server` 自动渲染且 Valid；出口仅 `toEntities: kube-apiserver` | 通过 |
| 真实内存 | cAdvisor working set 约 8.2 MiB 与 80.2 MiB，均低于 256 MiB | 通过 |
| 无状态依赖 | `kubectl -n dosh get pvc` 为空 | 通过 |
| 回滚演练 | revision 2（0.1.0）回滚后 health/ready/login 均 200；恢复到 revision 3 配置后生成 revision 5，health/ready/login 仍为 200 | 通过 |
| 可选依赖 | Metrics API、IngressClass、StorageClass 均不存在；页面保持可用并显示空状态 | 通过 |

### 21.3.1 当前集群状态（App `0.1.2` / Chart `0.1.4`）

| 验收项 | 证据摘要 | 结论 |
|---|---|---|
| Helm Release | `helm -n dosh list`：`dosh`，revision 12，`dosh-0.1.4`，app `0.1.2`，`deployed` | 通过 |
| 运行副本 | `kubectl -n dosh get deploy dosh`：2/2 Ready | 通过 |
| 镜像 | `192.168.122.10:5000/dosh/dosh:0.1.2` | 通过 |
| NodePort | `NodePort 32626 32626 http`；两个 Pod 分别位于 `worker-0005`、`worker-0003` | 通过 |
| Metrics Server | `dosh-metrics` 独立安装 `v0.9.1`，1/1 Running，`v1beta1.metrics.k8s.io` Available | 通过（不属于 `dosh` Release） |
| 生产 Metrics 用量 | 生产 `dosh` 仍运行 App `0.1.2`；v2.44 历史 digest 已在隔离测试 Release 通过可用/不可用/恢复三态和真实数值对账；v2.45 未复跑 Metrics 专项，生产目标版本未升级 | v2.44 历史实验室已验收；v2.45 和生产目标版本未验收 |
| 429/5xx 重试 | v2.44 历史 digest 完成 429/500/502/503/504 各 2 次失败后恢复、持续 429 三次、`Retry-After` 约 2098 ms、写请求仅 1 次、重置、取消和恢复；v2.45 未复跑 retry 专项；生产 App `0.1.2` 未包含候选改动 | v2.44 历史实验室已验收；v2.45 和生产目标版本未验收 |
| 生产 OIDC | 生产集群沿用 `local` 认证配置；生产 Release 未切换到 OIDC | 未部署（符合当前放行边界） |
| OIDC 隔离验证 | v2.45 当前 digest 在 `dosh-e2e-oidc` 完成真实 OIDC Chromium/Firefox/WebKit 各 16/16、host 两浏览器 12/12 与隔离 WebKit 6/6 denied 审计、Token 扫描和 logout，Release revision 192 当前仍保留；rollback/restore `181→184`、Metrics/retry/Loki/soak/生命周期矩阵是 v2.44 历史证据，v2.45 未复跑 | 当前候选签名测试 IdP 的三浏览器容器实验室已验收；企业 IdP、Safari/Edge 实机和生产未验收 |

生产集群的完整功能回归和 OIDC 升级仍需在阶段 8 重新执行，不能以历史 `0.1.1/0.1.3` 证据或
隔离测试 Release 的 API 矩阵替代。隔离测试 Release 只证明测试 IdP、impersonation、SAR、审计与
RBAC 的集成路径，不证明生产浏览器体验或企业 IdP 兼容性。

### 21.4 尚未完成或需要生产化补强的项目

> 历史口径说明：本节保留更早候选的完整矩阵结果，用于追溯既有能力。v2.43 的证据只按 digest
> `sha256:779ecae7…33306` 的 21.2.35 解释；v2.44 历史候选的日志、OIDC Chromium/Firefox/WebKit、
> rollback、Metrics/retry/Loki、soak、工作负载生命周期和 CronJob 结果按 21.2.37 与 21.2.38 解释。

| 项目 | 当前状态 | 发布影响 | 下一步 |
|---|---|---|---|
| StorageClass/PV/PVC 详情 | v2.37 前一候选在隔离 Release revision 9 通过三类详情、绑定关系、申请/实际容量、volumeMode、CSI/claim 引用、PVC conditions 和 YAML；v2.38 源码保留该能力但未重跑存储 API fixture；应用测试与前端详情深链通过 | 低（候选） | 在生产目标版本以真实 StorageClass/CSI 配置复核；删除/编辑存储资源仍不在当前只读范围 |
| 动态集群上下文 | AppShell、侧栏、关于弹窗和总览面包屑统一读取 `/api/v1/overview`；v2.39 mock Chromium/Firefox 各 32/32、真实 OIDC 各 15/15 覆盖动态上下文 | 低 | 企业 IdP 与生产环境复验；Edge/Safari 实机 |
| Metrics 实际利用率 | Metrics Server `v0.9.1` 已安装在 `dosh-metrics`；v2.45 当前 digest 通过可用/禁用/恢复三态和真实数值对账（CPU `2152m→0→2141m`），证据见 21.2.40；生产/预发目标拓扑仍未复验 | 低 | 在生产/预发等价 Metrics 拓扑复验，记录目标环境证据 |
| 筛选条件 URL 持久化 | 代码已实现；v2.45 当前候选 mock Chromium/Firefox/WebKit 各 38/38、真实 OIDC 各 16/16，包含关键词、命名空间、类型、刷新、新标签页分享、前进/后退和空参数抽样 | 候选实验室已验收（三浏览器容器） | Edge/Safari 实机复验后归档 |
| Deployment 创建向导 | 已实现专用 DTO、app 校验/授权/审计、API Server Dry Run、YAML 预览、三步向导和稳定 409；当前候选通过 preview/create、400、409、RBAC 和浏览器向导流程 | 低（候选） | 随目标版本发布，并在生产等价环境复核 |
| OIDC + impersonation | app/HTTP/adapter/impersonation/SAR、前端退出和 Helm 双模式已实现；v2.45 当前 digest 通过真实 OIDC Chromium/Firefox/WebKit 各 16/16、host 两浏览器 12/12 与隔离 WebKit 6/6 denied 审计和 Token 扫描 | 当前候选签名测试 IdP 的三浏览器容器实验室已验收；企业 IdP、Edge/Safari 实机未验收 | 接入预发企业 IdP，复验 claims、私有 CA、logout 和同一负向矩阵 |
| OIDC 浏览器与企业 IdP | v2.45 当前 digest 在测试 IdP 通过真实 OIDC Chromium/Firefox/WebKit 各 16/16，覆盖三身份、Secret 无权限、按钮权限、七类负向、审计历史、主题/axe/键盘和 logout | 生产多用户场景为高 | 接入预发企业 IdP，复验私有 CA、claims、logout 和 Token 扫描；补 Edge/Safari 实机 |
| OIDC Token 泄露 | v2.45 当前 digest 的 Chromium/Firefox/WebKit 浏览器 URL/DOM/Web Storage/应用日志/ConfigMap/Deployment/Helm values 扫描 0 命中，host 两浏览器 12/12 与隔离 WebKit 6/6 denied 审计均带 requestId | 当前候选三浏览器容器实验室已验收 | 企业 IdP 和受信 TLS 复跑后更新受控归档 |
| 审计持久化 | stdout 基线 + 可选 Webhook（强制 HTTPS/Bearer/HMAC/私 CA/重试/有界队列/关闭排空）已实现；v2.45 当前 digest 通过隔离 Audit/Reveal 的业务事件、失败审计、瞬时/持续 503、超时恢复、队列满、只读拒绝和 20/20 SIGTERM 排空；真实 Loki 59 条事件/34 个 requestId 与租户隔离是 v2.44 历史证据，v2.45 未复跑 | 当前候选实验室已通过；生产合规仍待企业 SIEM | 接入企业 Loki/OpenSearch/SIEM，验证保留、告警、访问控制和投递 SLA |
| 审计历史查询 | v2.41 新增管理员可见的进程内最近 1000 条环形缓冲、`GET /api/v1/audit-events`、URL 筛选、分页/排序和详情弹窗；真实 OIDC 验证 401/403/200、过滤、排序和非法 result 400 | 仅作当前实例排查，重启/副本切换会丢失或分叉；不能替代持久审计 | 保留 stdout/SIEM/WORM 作为合规事实源；如产品后续要求持久查询，再设计外部存储和保留策略 |
| 审计关停可靠性 | v2.45 当前 digest 的隔离 Audit/Reveal 完成 20/20 SIGTERM 排空，token/hmac/secret 泄露扫描均为 0；v2.44 历史证据中的真实 Loki 检索确认和唯一 requestId 对账不继承，v2.45 未复跑 Loki | 当前候选实验室已验收 | 在生产或预发同等拓扑复跑，并保留进程退出时间与接收端时间线 |
| Kubernetes 429/5xx 重试 | retry transport 已实现；v2.44 历史 digest 完成 429/500/502/503/504 各自两次失败后恢复、持续 429 恰好 3 次、`Retry-After` 约 2098 ms、写请求仅 1 次、连接重置、客户端取消和恢复；v2.45 未复跑 | v2.44 历史实验室已验收；v2.45 待复验 | 在 v2.45 或生产/预发同等拓扑复验归档；保持写请求不自动重试 |
| 长连接与故障注入 | v2.44 历史 digest 完成 30 分钟 soak：30 事件、50,577 事件帧、26,577 日志行、5 次重连、0 应用错误、0 流失败，内存 `22→18 MiB`、峰值 30 MiB；覆盖 Pod 删除和应用滚动重启；v2.45 未复跑 | v2.44 历史实验室已验收；v2.45 待复验，持续故障与容量边界仍需生产级证据 | 在 v2.45 或生产/预发同等并发拓扑复跑；持续故障与容量边界仍需生产级证据 |
| 日志流结构化与断流 | v2.44 历史 digest 使用有界 NDJSON，真实隔离集群通过 `log→end`、缺失容器 4xx、未认证 401、删除 Pod 后 `end/error` 终止和 HTML/ANSI 保真；30 分钟 soak 与真实 OIDC Chromium/Firefox 已在同一 digest 通过；v2.45 未重跑专用日志流矩阵 | v2.44 历史日志专项实验室已验收；v2.45 待复验 | 在 v2.45 隔离集群补跑 WebKit 与日志流专项；生产发布前做目标环境复验 |
| Edge 实机与历史 Safari | 当前候选 Chromium/Firefox/WebKit 已通过；WebKit 为隔离容器引擎，Edge/Safari 未单独启动真实浏览器 | 生产前低 | 在 CI 或设备实验室增加固定版本 Edge/Safari 实机；当前宿主不得重复 WebKit |
| 暗色主题与可访问性 | v2.45 当前 digest 保留危险状态文字对比度修复；Vitest 62/62，mock Chromium/Firefox/WebKit 各 38/38、真实 OIDC Chromium/Firefox/WebKit 各 16/16，axe 0 serious/critical、亮暗主题和键盘主路径通过 | 当前候选三浏览器容器实验室已验收；Edge/Safari 实机未复验 | 企业 IdP、Edge/Safari 实机和生产环境复验后归档证据 |
| OIDC 浏览器负向注入 | v2.45 当前 digest mock Chromium/Firefox/WebKit 各 38/38、真实 OIDC Chromium/Firefox/WebKit 各 16/16 通过；缺失/篡改/不匹配/过期 state、nonce mismatch、code 重放和开放重定向均拒绝；host 两浏览器 12/12 与隔离 WebKit 6/6 denied 审计带 `requestId`；Token 扫描 0 命中 | 当前候选三浏览器容器实验室范围已关闭 | 企业 IdP 和 Edge/Safari 实机接入后复跑同一矩阵，并更新 candidate digest 证据 |
| 受信 TLS 证书 | 当前为自签证书 | 生产高 | 替换为受信 CA，更新 `dosh-tls` |
| CI 流水线 | `.github/workflows/ci.yml` 已建立 8 个 job，包含 policy 门禁和固定工具版本；尚未在 GitHub Actions 真实执行 | 生产高 | 首次实跑全部 job，修复环境差异，启用分支保护并要求 `release-summary` 成功 |
| 前一完整门禁候选 | `0.1.3-rc-20261102153511@sha256:47983a…312e` 已完成 `make verify`、race、容量、前端 10/10、mock E2E 30/30、真实本地 E2E 3/3、OIDC 12/12、retry、Metrics、30 分钟 soak、审计/SIGTERM、rollback/restore 和 Trivy；证据已完成本机受控归档 | 历史候选实验室已验收；生产未发布 | 不能继承给当前 digest；当前候选按 25.10 复跑后，再完成离机 WORM、企业 IdP/TLS/SIEM/Edge/Safari/CI |
| 受控证据归档 | v2.45 当前候选主归档 33 文件，manifest `e71c466b…64c6`，含源码绑定、真实 OIDC、Audit/Reveal、容量、性能和漏洞扫描；Metrics 补充归档 4 文件 `d2bae353…48b8`；retry 补充归档 5 文件 `27282a7d…815c`；v2.44 历史综合归档 553 文件、manifest `9f9560d2…72b9`，含 rollback/restore、Loki、soak、工作负载生命周期和 CronJob 详情；均通过双层校验 | v2.45 当前 digest 的 Metrics/retry 内部矩阵归档已完成；离机 WORM、长期保留和访问审批未完成 | 把原始归档和 manifest 摘要写入变更单并复制到企业批准的 WORM/长期存储 |
| 当前镜像漏洞扫描 | v2.45 当前 digest `sha256:c7eff34a…3ab06` 已通过 Trivy：Debian 与 Go binary 均 0 HIGH/CRITICAL；v2.44 及更早结果只作历史参考 | 当前候选镜像漏洞门禁已关闭 | 生产发布前确认 digest 未变；权限或依赖变化后重新扫描并归档 |
| Go 依赖漏洞 | `golang.org/x/net v0.62.0`、`golang.org/x/crypto v0.58.1-0.20261009181718-2f1f834330b2` 已将 `govulncheck` 降为 0 可达、0 package、0 module 告警 | 本地依赖门禁已关闭 | CI 真跑后保留数据库时间和原始输出；依赖更新必须重跑 |
| 前端开发依赖漏洞 | 完整 `npm audit` 为 3 个 moderate、0 high/critical；均来自 `openapi-typescript` → `@redocly/openapi-core` → `minimatch` 工具链，不进入生产 bundle | 低 | 跟踪上游修复或替换生成器；CI 中同时执行生产依赖 high 门禁和完整审计报告 |

### 21.5 严重等级与发布规则

- **Blocker**：未认证可写、Secret 明文泄露、CSRF 失效、NodePort 非 32626、部署到错误命名空间、Pod 以 root 运行、审计队列无界或阻塞业务、CI 未通过却直接发布。任一存在即禁止发布。
- **Major**：RBAC 子资源缺失、静态资源 404、日志/SSE 无法连接、审计最终失败不可观测、PDB 失效、Pod 无法达到 Ready、无受信 TLS、无 OIDC/impersonation 却对外提供多用户访问。必须在发布前修复或由风险负责人书面接受。
- **Minor**：UI 对齐、目标发布版本的 Metrics 复验、Edge/Safari 抽样缺口、文档措辞。可进入首版后迭代，但必须登记；不得影响 P0 主路径。

## 22. 实施步骤明细与文件落点

本节把“做什么、落在哪里、怎么验收”落实为可执行清单。命令均在仓库根目录执行。

### 22.1 后端领域与端口

**设计**：`domain` 保存统一 DTO、错误语义、能力、审计接口和白名单资源类型；各模块 app 定义自己需要的最小出站接口。

**实施**

1. 在 `internal/domain/types.go` 增加资源 DTO；禁止引入 Kubernetes 类型。
2. 在 `internal/domain/errors.go` 增加稳定错误语义：NotFound、Conflict、Forbidden、Unauthorized、Invalid、ReadOnly、Unavailable、Unsupported、RateLimited。
3. 在 `internal/modules/<module>/app/service.go` 定义 Repository 端口，方法数量只覆盖本模块。
4. 在 app 用例中执行参数校验、权限检查和审计，不直接读写 HTTP 请求或 client-go。

**验收**

```bash
make arch
go test ./internal/...
rg -n 'k8s.io|net/http|chi' internal/domain internal/modules/*/app
```

最后一条命令必须无输出。

### 22.2 Kubernetes adapter

**设计**：`platform/kube` 是唯一 client-go 边界，负责列表、详情、对象转换、错误映射和写操作；
Metrics 通过独立 `k8s.io/metrics` client 读取 `metrics.k8s.io/v1beta1`，探测结果缓存 30 秒，
探测或读取失败时回退到容量/requests，不把缺失数据解释为 0。

**实施**

1. 在 `internal/platform/kube/client.go` 创建 clientset、dynamic client、discovery client、metrics client 和超时。
2. 按资源拆分 `nodes.go`、`workloads.go`、`pods.go`、`events.go`、`network.go`、`configuration.go`、`storage.go`、`yaml.go`；Metrics 转换集中在 `metrics.go`。
3. 在 adapter 内把 K8s 对象转换为 domain DTO，去除 `managedFields`，Secret 默认脱敏。
4. 将所有 API 错误映射到 domain 错误语义。
5. 列表接口批量读取 Node/Pod Metrics；详情接口按资源读取单条指标；使用 `hasMetrics` 显式表达缺失。
6. Deployment 创建由 `deployment_create.go` 统一构造对象：preview 与 create 共用同一构造函数，
   create 先以 `DryRunAll` 提交，再正式创建；AlreadyExists 映射为 `domain.ErrAlreadyExists`。

**验收**

```bash
go test ./internal/platform/kube/...
go test ./internal/platform/kube -run Metrics -count=1 -v
kubectl -n dosh auth can-i list deployments
```

对账时使用第 15.6 节命令，节点、Pod、工作负载数量必须一致。

### 22.3 HTTP 与认证

**设计**：chi 只做 transport；认证 Cookie 策略在 auth HTTP adapter，会话加密、OIDC state/nonce/PKCE 和
capability 映射在 auth app；授权通过消费者定义的 `domain.Authorizer`，所有写路径和敏感读路径执行
CSRF 与 app 权限检查。OIDC 模式下按请求身份 impersonate，Kubernetes RBAC 是最终权限事实。

**实施**

1. `internal/modules/auth/app/session.go` 只负责 AES-GCM 封包/解包和过期校验。
2. `internal/modules/auth/app/oidc.go` 生成并验证 state/nonce/verifier，限制会话 TTL，使用常量时间比较并防开放重定向。
3. `internal/modules/auth/http/handler.go` 设置会话 Cookie（HttpOnly、Secure、SameSite=Strict）和
   OIDC state Cookie（HttpOnly、Secure、SameSite=Lax、Path 限定）；回调失败统一清理状态并审计。
4. `internal/platform/authz` 实现 Local 和 Kubernetes Authorizer；OIDC 模式使用用户身份执行资源检查。
5. `internal/platform/kube` 仅对 OIDC 模式包装 impersonation transport；身份只来自 `domain.Actor`。
6. `bootstrap/router.go` 按顺序安装 RequestID、Recoverer、SecurityHeaders、AccessLog、Compress、Session、CSRF。
7. 每个模块提供 `Mount(chi.Router)`；OpenAPI 同步更新。
8. 模块 Handler 只通过 `domain.ActorFromContext` 获取身份；禁止 import `modules/auth/http` 或复制
   认证上下文 key。认证 adapter 负责把 `domain.Actor` 放入请求上下文，业务模块不感知 Cookie/OIDC。

**验收**

```bash
go test -race ./internal/modules/auth/... ./internal/platform/authz ./internal/platform/kube/... ./internal/platform/config
go test ./internal/modules/configuration/http ./internal/modules/workloads/http ./internal/modules/yaml/http -count=1
make arch
curl -k -i https://<node-ip>:32626/api/v1/overview
curl -k -i -b cookie.txt -H 'X-CSRF-Token: <token>' \
  -X POST https://<node-ip>:32626/api/v1/workloads/deployment/<ns>/<name>/scale
```

预期：未登录 401；无 CSRF 403；成功后审计日志包含 user/action/resource/result/request_id。

### 22.4 前端

**设计**：CCE 风格顶部栏 + 左侧导航 + 主内容区；TanStack Router 负责路由，TanStack Query 负责缓存，shadcn 风格组件统一视觉。

**实施**

1. 在 `web/src/main.tsx` 创建 QueryClient、RouterProvider、Toaster。
2. 在 `web/src/router.tsx` 声明全部显式路由，不允许 wildcard API。
3. 在 `features/` 按领域拆分列表、详情和操作弹窗。
4. 统一使用 LoadingState、EmptyState、ErrorState、StatusBadge 和 DataTable。
5. Secret 明文只放在临时 Dialog；日志使用 `<pre>` 纯文本渲染，禁止 `dangerouslySetInnerHTML`。
6. 列表页使用 `useSearch({ from: ... })` 读取 `q/namespace/kind`，通过 `navigate({ search, replace: true })` 更新 URL，不保留第二份不可分享的筛选状态。
7. 总览/节点/Pod 页面根据 `metricsAvailable` 或 `hasMetrics` 切换实际 usage 与 requests 降级文案，不能在缺失指标时显示 0。
8. 登录页先读取 `/api/v1/auth/config`；`local` 显示原账号表单，`oidc` 显示 SSO 按钮，回调错误显示统一提示。
9. 用户菜单按 `session.authMode` 选择退出路径：本地模式 DELETE `/api/v1/session`，OIDC 模式跳转
   `/api/v1/auth/oidc/logout`；不得在 OIDC 模式仅清本地会话而假装完成企业 SSO 退出。
10. 所有组件测试、MSW 和 Playwright mock 必须包含 `authMode`，不得依赖 TypeScript 可选字段规避契约。
11. 工作负载页按命名空间 `capabilities.create` 显示“创建 Deployment”；向导分为基础信息、容器与资源、
    YAML 预览三步，预览成功前禁用创建按钮；创建成功跳转详情并刷新列表。
12. 顶部栏、侧栏和关于弹窗统一使用 `/api/v1/overview` 的 `cluster.name/version/cni/status`；
    不得把 `kubedo-lab`、`Kubernetes v1.36.5`、`Cilium` 或“集群连接正常”写进组件。
13. 集群状态映射必须同时提供文本与颜色：`Healthy=正常`、`Degraded=部分异常`、
    `Unavailable=不可用`；请求失败显示“状态暂不可用”和重试入口，不能默认绿色正常。

**验收**

```bash
cd web
npm run typecheck
npm test
npm run build
npm run bundle:check
npm run e2e:container
```

并人工检查 1024px 以下侧边栏折叠、表格横向滚动、空状态、错误态、URL 刷新恢复和
Metrics 有/无两态。v2.45 当前候选的 mock 主套件为 Chromium/Firefox/WebKit 各 `38/38`；
隔离集群真实 OIDC 为 Chromium/Firefox/WebKit 各 `16/16`，已包含主题、axe、键盘和七类负向登录。
集群上下文还必须用
MSW 伪造非实验室名称/版本并断言 UI 同步，返回 503 时断言不可用状态；只验证实验室固定文案不算通过。

### 22.5 镜像与 Helm

**实施**

1. `make build` 先构建前端，再把 `web/dist` 嵌入 Go 二进制。
2. `Dockerfile` 使用 `gcr.io/distroless/static-debian12:nonroot`，只复制二进制。
3. `make image TAG=<tag>` 使用 Podman 构建；推送前执行 `podman push --tls-verify=false`（仅自签实验仓库）。
4. Helm Chart 渲染 2 副本、required anti-affinity、PDB、三个探针、SecurityContext、`/tmp` emptyDir 和 TLS Secret 挂载。
5. Secret 由 `dosh-auth`、`dosh-tls` 注入，Chart 不落盘敏感值。
6. 审计 Webhook 默认关闭；启用时 URL 来自 values，Token/HMAC 从 `dosh-audit` Secret 注入，CA 从只读 ConfigMap 注入；`networkPolicy.webhook.ipBlocks` 必须显式配置，Token 与 HMAC 至少配置一个。
7. Cilium 策略默认通过 `.Capabilities.APIVersions.Has "cilium.io/v2"` 自动探测；检测成功时使用 CNP `toEntities: kube-apiserver`，并为 OIDC 与审计 Webhook 分别生成精确 `toCIDR` 出口，禁止 `0.0.0.0/0`。
8. 发布必须通过 `image.digest` 固定镜像；模板只接受 `^sha256:[a-fA-F0-9]{64}$`，渲染结果为 `repository@digest`，不得用可覆盖 tag 作为生产身份。
9. `scripts/check-version.sh` 在本地和 CI 校验 Makefile、Chart、values、OpenAPI、npm package/lock 和 README，任何版本漂移都阻断发布。

**验收**

```bash
make helm-lint
helm template dosh deploy/helm/dosh -n dosh > /tmp/dosh-rendered.yaml
helm template dosh deploy/helm/dosh -n dosh \
  --api-versions cilium.io/v2 > /tmp/dosh-cilium.yaml
helm template dosh deploy/helm/dosh -n dosh \
  --set audit.webhook.enabled=true \
  --set audit.webhook.url=https://siem.example.test/audit \
  --set audit.webhook.tokenKey=token \
  --set audit.webhook.hmacKeyKey= \
  --set audit.webhook.ca.existingConfigMap=dosh-audit-ca \
  --set networkPolicy.webhook.ipBlocks[0]=203.0.113.10/32 \
  > /tmp/dosh-audit.yaml
helm template dosh deploy/helm/dosh -n dosh \
  --set image.digest=sha256:1111111111111111111111111111111111111111111111111111111111111111 \
  | rg 'image: "192.168.122.10:5000/dosh/dosh@sha256:'
helm template dosh deploy/helm/dosh -n dosh --set image.digest=sha256:not-a-digest >/dev/null 2>&1 && exit 1 || true
helm upgrade --install dosh deploy/helm/dosh -n dosh --create-namespace --wait
kubectl -n dosh get deploy,svc,pod,pdb,sa -o wide
```

### 22.6 发布门禁顺序

```text
1. 版本冻结：同步 Makefile/Chart/values/OpenAPI/package/README
2. gofmt + go vet + go test -race + Metrics/audit/config 专项测试
3. make arch + OpenAPI generate + api-check diff + typecheck
4. frontend test + production build + bundle check + 三浏览器 mock E2E(45/45)
5. capacity-test + production LCP
6. govulncheck + npm audit；构建镜像并扫描 HIGH/CRITICAL
7. helm lint + 普通/Cilium/审计 Webhook 三路径 template + kubeconform
8. build image + push + record registry digest + helm upgrade --wait
9. health/login/Metrics 降级/URL 恢复/read/write/log/SSE/secret acceptance
10. audit Webhook（若启用）签名、重试、SIGTERM 排空和真实 SIEM 投递
11. rollback + restore + re-run health/login
12. `make evidence-check` + 归档 digest/revision/日志/截图 + manifest 双层校验
13. CI 全绿并归档 artifact；记录 image tag、digest、Helm revision 和证据

任一 Blocker 或未接受 Major 存在时，停止发布并回滚。
```

### 22.7 CI 流水线实施设计（已建立，待实跑验收）

**目标**：把人工门禁固化为不可跳过的流水线，确保同一提交在固定工具版本下可重复验收。
`.github/workflows/ci.yml` 已按本节建立；但尚未在 GitHub Actions 真实运行，因此只能标记为
“已实现、待验收”，不能声称 CI 已通过。

**前置约束**

- 版本固定：Go `1.26.x`、Node.js `24.x`、Helm `3.22.0`、`@playwright/test 1.64.1`、Playwright `v1.64.1-noble` 容器、Trivy 固定镜像 digest `sha256:2e6c00d711ab4ae41d8a30c2c1fd3bd3b62704c177bb120f95012e939edfb3df`。
- 依赖锁定：使用 `go.sum`、`web/package-lock.json`，CI 使用 `npm ci`，禁止浮动的 `latest` 工具直接进入发布门禁。
- 分片执行：后端、前端、浏览器 E2E、Helm/安全扫描可并行，但发布汇总任务必须等待全部成功。
- 报告留存：JUnit、Playwright HTML、Trivy JSON/SARIF、Helm 渲染 YAML 作为 CI artifact 保留至少 30 天。

**任务拆分**

| Job | 做什么 | 执行命令 | 失败条件 |
|---|---|---|---|
| `backend` | 格式、静态检查、竞态单测、架构边界、容量 SLO | `gofmt -l cmd internal web/embed.go`、`go vet ./...`、`go test -race ./...`、`make arch`、`make capacity-test` | 任一命令非零、格式输出非空、架构 grep 有匹配或 SLO 超限 |
| `frontend` | 契约、类型、集成测试、生产构建、bundle 预算 | `make api-check`、`npm run typecheck`、`npm test`、`npm run build`、`npm run bundle:check` | 生成物漂移、类型/测试/构建失败或初始 JS gzip 超过 500 KiB |
| `e2e` | 固定官方容器跑三浏览器主路径 | `npx playwright test` | 任一条主路径失败；保留 trace、screenshot 和 HTML 报告 |
| `performance` | 生产构建 LCP 门禁 | `npx playwright test --config=playwright.performance.config.ts` | LCP 大于 2.5 秒或测试失败 |
| `helm` | lint、非 Cilium 与 Cilium 双路径渲染、关键字段断言 | `helm lint`、两次 `helm template`、`kubeconform` | 渲染失败、CNP/NetworkPolicy 路径错误、端口/副本/安全上下文断言失败 |
| `security` | Go 调用链、生产依赖、镜像 HIGH/CRITICAL | `govulncheck`、`npm audit --omit=dev --audit-level=high`、构建镜像后运行 Trivy | 调用链漏洞、生产依赖 high/critical 或镜像 high/critical 非零 |
| `release-summary` | 汇总版本、digest、测试与扫描报告 | 读取上游 job 输出并生成 release manifest | 任一依赖 job 未成功或证据缺失 |

**验收门槛**

1. 受保护分支的合并必须要求 `release-summary` 成功，禁止直接跳过失败 job。
2. 同一提交重跑时，版本固定、结果可重复；镜像以不可变 digest 记录，不以可变 tag 代替。
3. `security` 的生产依赖门禁为 0 high/critical；完整 `npm audit` 的 dev-only moderate 必须输出报告并登记，不得静默忽略。
4. CI artifact 中能定位到每个需求证据：E2E 报告、容量结果、LCP、bundle 统计、Trivy 报告和 Helm 渲染文件。
5. 失败时保留日志与报告，修复后重新执行完整流水线，不允许只重跑单个成功 job 后发布。

### 22.8 Metrics 实际利用率实施包

**关联需求**：OVW-U-004、OVW-N-003、NODE-U-005、POD-U-003、UX-U-009、TST-U-009。

**设计**

- Platform adapter 统一从 `metrics.k8s.io/v1beta1` 读取指标；30 秒内复用 discovery 探测结果，避免每次列表都打 API。
- Node 指标按节点名合并；Pod 指标按 `namespace/name` 合并，容器 CPU/内存求和。
- Domain DTO 使用 `cpuUsageMilli`、`memoryUsageBytes` 和 `hasMetrics`。`hasMetrics=false` 表示“没有样本”，不能解释为 0。
- Overview 聚合实际节点用量；节点和 Pod 页面优先展示实际用量，同时保留 requests。

**实施步骤**

1. 增加 `k8s.io/metrics` 依赖并创建 `platform/kube/metrics.go`，在 `client.go` 注入 metrics client 和 30 秒探测缓存。
2. 更新 `domain/types.go` 的 `ClusterCapacity`、`NodeSummary`、`PodSummary`、`PodDetail`。
3. 在 `nodes.go`、`pods.go` 的列表/详情路径合并指标；Metrics 不可用时返回零值并设置 `hasMetrics=false`。
4. 更新 `overview/app` 聚合逻辑、OpenAPI、`web/src/api/types.ts` 和页面展示。
5. 使用 fake Metrics client 覆盖 Node、Pod、详情、探测失败和单个资源缺样本。
6. 将 E2E 总览 mock 的 `metricsAvailable` 设为 `true`，并断言页面显示实际 CPU/内存用量；另保留一个 `false` 降级用例。
7. 安装 Metrics Server 后执行第 15.9 节真实对账。

**验收**

```bash
go test ./internal/platform/kube -run Metrics -count=1 -v
go test ./...
cd web && npm run typecheck && npm test && npm run build
```

通过标准：无 Metrics 时所有页面可用且不显示伪造 0%；有 Metrics 时实际用量与 `kubectl top` 同量级；
OpenAPI 字段与前端类型一致；`hasMetrics` 与页面降级文案一一对应。

**当前状态（2026-11-04）**：`scripts/install-metrics-server.sh` 已在 `dosh-metrics` 安装
Metrics Server `v0.9.1`；v2.45 当前候选已用 `scripts/e2e-metrics.sh` 完成隔离测试 Release 的
可用/禁用/恢复三态，结果见 21.2.40。目标生产版本仍未复验，因此“实验室已验收”不等于
“生产已验收”。

### 22.9 列表筛选 URL 持久化实施包

**关联需求**：WL-U-003、UX-U-008、TST-U-010。

**设计**

- 新增 `web/src/lib/list-search.ts`，把 URL 原始值规范化为 `{ q?, namespace?, kind? }`，空白字符串归一为 `undefined`。
- TanStack Router 对 `/nodes`、`/workloads`、`/pods`、`/events`、`/network`、`/configuration` 使用同一 `validateSearch`。
- 页面只能从 `useSearch` 读取筛选条件，通过 `navigate({ search: { ...search, ...patch }, replace: true })` 写入。
- 表格筛选结果必须从 URL 派生，不允许再维护一个不可分享的同义 React 状态。

**实施步骤**

1. 在路由表添加 `validateSearch: parseListSearch`。
2. 替换各列表页面的本地筛选 state 为 `useSearch` + 路由更新。
3. 增加 mock Playwright 用例：输入关键词、切命名空间/类型，断言 URL；刷新后断言输入和表格状态恢复。
4. 同一用例在新标签页打开当前 URL，验证分享链接；再通过离开页面、浏览器后退/前进验证历史恢复。
5. 在真实隔离 Release 增加同一持久化抽样，避免只靠 mock 验收。

**验收**

```bash
cd web
npx playwright test -g '筛选条件持久化到 URL'
npx playwright test
```

通过标准：6 个列表页都使用同一 schema；无参数时 URL 干净；刷新/分享/前进后退结果一致；
空值不会写成 `?q=` 等噪声。

### 22.10 审计远程投递实施包

**关联需求**：SEC-U-004、SEC-U-006、SEC-U-007、OPS-U-013、NFR-E-003、NFR-E-004、TST-U-011。

**事件模型**

```json
{
  "timestamp": "2026-11-02T12:34:56.789Z",
  "user": "admin",
  "groups": [],
  "action": "scale",
  "resource": "deployment",
  "namespace": "default",
  "name": "web",
  "result": "success",
  "requestId": "req-...",
  "error": "optional"
}
```

**设计**

- stdout 审计永远保留，作为最低可用基线；Webhook 是可选旁路，不改变业务响应。
- 有界 channel 队列满时同步写 `audit_webhook_queue_full` 错误日志，但不阻塞原请求。
- 单条投递最多 3 次，间隔 200 ms、400 ms；每次受 `DOSH_AUDIT_WEBHOOK_TIMEOUT` 约束。
- 启用 Token 时发送 `Authorization: Bearer <token>`；启用 HMAC 时发送 `X-Dosh-Timestamp` 和
  `X-Dosh-Signature: sha256=<hex>`，签名原文为 `<timestamp>.<body>`。所有请求带 `X-Request-ID`。
- 响应必须为 2xx；非 2xx 和网络错误都进入有限重试，最终失败记录明文错误但不得输出 Token 或 HMAC。
- Helm 启用 Webhook 时，Token/HMAC 从 Secret 读取；NetworkPolicy 只放行 `networkPolicy.webhook.ipBlocks`
  与 `ports`，不得复用无约束的 API Server 出口。

**实施步骤**

1. 在 `internal/platform/audit/audit.go` 增加可选 sink，不改变现有 `Logger.Record` 调用方式。
2. 在 `config.Validate` 增加 URL、Token/HMAC、超时和队列边界校验。
3. 在 `bootstrap.App` 保存 audit logger 和可关闭 hook；SIGTERM 时先停止接收请求，再在有界 context 内 `Flush`，最后取消 worker。
4. 新增 `audit_test.go`，覆盖重试、签名、Bearer、Request ID、payload 不含 Secret 和 `Flush`。
5. 在 Helm values、Deployment、NetworkPolicy/CNP 中增加开关和出口配置。
6. 连接真实 SIEM/Loki/OpenSearch 执行端到端验收；只做本地 `httptest` 不算生产验收。

**验收**

```bash
go test -race ./internal/platform/audit -count=1 -v
go test ./internal/platform/config -count=1 -v
helm lint deploy/helm/dosh
```

通过标准：默认部署不出现审计 Webhook 环境变量；启用后健康检查仍通过；签名可由接收端复算；
重试次数和间隔符合设计；队列满和最终失败有可检索日志；SIGTERM 不丢失已入队记录。

### 22.11 配置负向测试与版本一致性

**关联需求**：SYS-N-001、SEC-N-003、TST-U-012、RISK-013。

**实施**

1. 为 `config.Validate` 增加表驱动测试：非绝对 URL、`ftp://`、空 Host、只有 URL 无 Token/HMAC、
   超时为 0/负值/超过 30 s、队列为 0/负值/超过 100000。
2. 增加版本一致性检查脚本，读取 `Makefile VERSION`、`Chart.yaml version/appVersion`、`values.yaml image.tag`、
   `api/openapi.yaml info.version`、`web/package.json version` 和 README 标签，任一处不一致即失败。
3. 发布前用 `rg '0\.1\.1|dosh-0\.1\.3'` 人工复核是否残留旧版本引用。

**验收**

```bash
go test ./internal/platform/config -count=1 -v
rg '0\.1\.1|dosh-0\.1\.3' Makefile api deploy web/package.json README.md docs/ears-implementation-plan.md
```

通过标准：每个非法配置都有稳定错误码/错误文本；候选版本各文件版本号一致；历史发布记录除外。

### 22.12 OIDC 多用户身份实施包（逐步骤设计、工作与验收）

**关联需求**：AUTH-O-001 至 AUTH-O-006、AUTH-N-004、AZ-O-001/002、AZ-N-003、
SEC-O-003 至 SEC-O-005、SEC-N-004、TST-U-013 至 TST-U-016。

**端到端设计目标**

- 浏览器只持有加密会话 Cookie 和短期 state Cookie，不持有可用 Token。
- IdP 负责证明“用户是谁”，Kubernetes RBAC 负责回答“用户能做什么”。
- dosh 的 ServiceAccount 只负责 impersonate 和提交 SAR，不成为绕过 RBAC 的高权限后门。
- app 层是授权决策发生地；Handler、React 和 `capabilities` 都不能替代服务端授权。
- 所有权限拒绝、登录失败、Secret reveal 和写操作都带用户、组、动作、资源、结果和 requestId 审计。

**步骤 1：领域模型与授权端口**

- 设计：`domain` 只定义 `Actor{Username,Groups,Admin}`、`Access{Verb,APIGroup,Resource,Subresource,Namespace,Name}`、
  `Authorizer.Require(ctx, actor, access)` 和 context 存取函数。domain 不 import OIDC、client-go、chi 或 HTTP。
- 实现：`internal/domain/types.go`；新增 `WithoutActor(ctx)`，供 SAR 请求移除 impersonation 身份。
- 验收：`make arch` 通过；`rg 'k8s.io|net/http|chi|oidc' internal/domain internal/modules/*/app` 无输出；
  Actor 不进入 URL 或日志敏感字段。
- 当前状态：Actor/Access/Authorizer 和 `WithoutActor` 已实现；定向 race 测试与 `make arch` 通过。

**步骤 2：app 层授权执行点**

- 设计：每个用例先解析最小 Access，再调用 Authorizer；授权失败不实例化或调用 Repository。读、写、删除、
  scale、restart、Secret reveal 和 YAML apply 分别映射到真实 Kubernetes verb/subresource。
- 实现：`workloads/app/service.go`、`configuration/app/service.go`、`yaml/app/service.go`；read-only 策略仍在
  Authorizer 内统一处理；审计 success/denied。
- 验收：fake client 记录 Repository 调用次数；无权限、只读和 Authorizer 错误时调用次数为 0；
  scale 映射 `deployments/scale` 或 `statefulsets/scale`；restart 映射 `patch`；delete 映射 `delete`。
- 当前状态：已实现并通过单元测试；真实 Kubernetes RBAC 的直接 API 矩阵和三浏览器矩阵均已通过
  （见 21.2.3、21.2.5）。

**步骤 3：OIDC provider adapter**

- 设计：`platform/oidc` 实现 discovery、JWKS 缓存、Authorization Code + S256 PKCE、issuer/audience/
  signature/expiry/nonce 校验、可配置 username/groups claim、私有 CA 和 RP logout。IdP HTTP 客户端有总超时，
  discovery/JWKS 失败不得降级为无签名信任。
- 实现：`internal/platform/oidc/provider.go`；`go-oidc/v3` 和 `golang.org/x/oauth2`；不把 Token 放入错误文本。
- 验收：使用本地 RSA 密钥构造签名 IdP；覆盖合法 token、错误签名、错误 issuer、错误 audience、过期、
  nonce mismatch、JWKS 轮换、email 未验证和不支持 groups claim；请求日志不出现 token/secret。
- 当前状态：adapter 已实现；真实 RSA 签名 IdP 测试已覆盖 discovery、JWKS、S256 PKCE、issuer、audience、
  expiry、nonce、错误签名、email 未验证和 JWKS 轮换并通过。真实企业 IdP 兼容性仍未验收。

**步骤 4：OIDC 应用状态机与会话**

- 设计：`auth/app` 生成高熵 state/nonce/verifier，AES-GCM 封装 `state+nonce+verifier+returnTo+expiry`；
  state 使用常量时间比较；`returnTo` 只允许站内相对路径；会话 TTL 取配置 TTL 与 ID Token `exp` 的较小值；
  admin group 只映射 `Actor.Admin` 展示标记，不扩大 Kubernetes 权限。
- 实现：`internal/modules/auth/app/oidc.go`、`session_service.go`；state key 从 Session Key 派生，至少 32 字节。
- 验收：合法回调建立会话；state 篡改/过期、nonce mismatch、缺少 code、开放重定向全部拒绝；
  会话过期不晚于 Token；`authMode` 正确；错误不泄露 IdP 细节和 secret。
- 当前状态：已实现；app 单元测试覆盖主路径和主要负向分支，真实签名由 provider 测试补齐。

**步骤 5：HTTP 路由、Cookie 和前端切换**

- 设计：`GET /auth/config` 匿名可读；OIDC 模式挂载 login/callback/logout，本地模式只挂载
  `POST /session`；`RequireSession` 同时写入会话和 domain Actor；未登录 API 返回 401 Problem。
- 实现：`internal/modules/auth/http/handler.go`、`internal/bootstrap/router.go`、
  `web/src/routes/login-page.tsx`、`web/src/components/layout/app-shell.tsx`、`web/src/api/client.ts`。
- 验收：OIDC 模式不暴露本地登录提交；state Cookie 属性正确；会话 Cookie 为 Strict；OIDC 退出跳转
  `/auth/oidc/logout`；所有前端测试 mock 包含 `authMode` 和 `/auth/config`。
- 当前状态：后端、登录页、AppShell OIDC 退出和组件测试均已实现。OIDC 模式跳转
  `/api/v1/auth/oidc/logout`，本地模式保留 `DELETE /session`；logout 使用从回调 URL 派生的同源
  绝对 `/login` 回跳。签名测试 IdP 的 HTTP RP logout 和三浏览器登录、退出按钮/回跳均已通过；
  企业 IdP 待验收。

**步骤 6：Kubernetes impersonation**

- 设计：仅在 `AuthMode=oidc` 时包装 transport；每个带 Actor 的请求设置 `Impersonate-User` 和
  `Impersonate-Group`；无 Actor 的内部请求保持 ServiceAccount 身份；不信任客户端 header。
- 实现：`internal/platform/kube/impersonation.go`、`client.go`。
- 验收：有 Actor 时 header 正确、多组去重/忽略空值；无 Actor 时不添加；local 模式不启用包装；
  header 不进入日志；用户输入不能注入额外 header。
- 当前状态：已实现并有 transport 单测。

**步骤 7：SubjectAccessReview 与 capabilities**

- 设计：OIDC 模式下使用 `SubjectAccessReview`，由 ServiceAccount 提交目标 actor 的 username/groups 和
  ResourceAttributes；SAR 请求上下文移除 Actor，避免 impersonation 影响评估。`/capabilities` 与每个用例
  使用同一 Authorizer 和权限映射，UI 只消费结果。
- 实现：`internal/platform/kube/authorization.go`、`internal/platform/authz/authz.go`、
  `internal/modules/auth/app/session_service.go`。
- 验收：捕获 SAR 对象并断言 `Spec.User`、`Spec.Groups`、`Verb`、`Group`、`Resource`、`Subresource`、
  `Namespace`、`Name`；请求无 impersonation 头；`Allowed=false` 返回 403；`EvaluationError`/网络错误
  fail-closed；Repository 调用次数为 0；capabilities 与逐项 SAR 结果一致。
- 当前状态：已实现 `SubjectAccessReview`。SAR 显式携带 actor 的 username/groups 和 ResourceAttributes，
  请求前使用 `domain.WithoutActor` 移除 impersonation；空 Actor fail-closed。组件测试和隔离集群
  viewer/operator/admin API 矩阵及浏览器路径均已通过。

**步骤 8：Helm 与最小 RBAC**

- 设计：`auth.mode=local|oidc` 驱动同一模板；OIDC Secret、CA ConfigMap、issuer/clientID/redirect URL
  和 IdP 出口策略显式配置；OIDC ClusterRole 只包含 impersonate、SAR create、discovery/version。
- 实现：`deploy/helm/dosh/values.yaml`、`templates/deployment.yaml`、`templates/rbac.yaml`、
  `templates/networkpolicy.yaml`、`templates/ciliumnetworkpolicy.yaml`。
- 验收：`helm lint`；local/oidc 双路径 template；client secret 仅 `secretKeyRef`；OIDC 无业务资源
  create/update/delete 权限；NodePort 32626、namespace `dosh` 不变；NetworkPolicy 不放行 `0.0.0.0/0` 到 IdP。
- 当前状态：Chart 已实现并接入 `make helm-check`。local/oidc/Cilium 正向渲染、OIDC Secret/CA、
  最小 RBAC、32626/2 副本和负向配置均已断言通过；隔离集群已完成真实 API Server 部署、OIDC
  登录和 IdP 连通性验证，最小 ServiceAccount 的 `auth can-i` 也符合设计。生产部署和 API Server
  dry-run 仍待完成。

**步骤 9：签名测试 IdP 与三身份 E2E**

- 设计：建立轻量测试 IdP，提供 discovery、JWKS、authorize、token、logout；使用 RSA 私钥真实签名 JWT。
  测试集群创建 viewer、namespace-operator、admin 三个身份/组及对应 RBAC；浏览器与 Pod 使用同一可达地址。
- 实现：`tools/oidctestidp/main.go`、`scripts/e2e-oidc.sh`、隔离 namespace/RBAC fixture；
  `scripts/e2e-oidc-browser.sh`、`web/playwright.oidc.config.ts` 和 `web/e2e-oidc/oidc.spec.ts`
  提供独立真实集群浏览器套件，不改写现有 mock E2E 的语义。
- 验收：viewer 读成功、写 403 且无写请求；operator 仅授权命名空间可写；admin 全量可写；
  三者在审计中区分；state 篡改、nonce mismatch、错误 issuer/audience、开放重定向、logout 均通过。
- 当前状态：签名测试 IdP、真实 Kubernetes RBAC fixture、镜像构建/推送、隔离 Helm 部署、三身份
  API 矩阵、三浏览器 E2E、Token 泄露扫描、审计、HTTP/浏览器 RP logout 和 OIDC rollback/restore
  已通过。真实企业 IdP 尚未完成。

**步骤 10：发布与回滚**

- 设计：OIDC 镜像使用不可变 digest；先部署到测试 namespace，验证 local 回归和 OIDC 矩阵，再升级
  `dosh`；保留上一 revision 以及 local 配置 Secret，支持一键回滚。
- 验收：`helm history`、`kubectl rollout status`、`/healthz`、登录、三身份矩阵、审计、回滚和恢复全部有证据；
  发布记录包含 image digest、Chart version、IdP issuer 和 RBAC fixture 摘要。
- 当前状态：隔离测试 Release 已用候选 tag 完成 upgrade → rollback → restore 并通过矩阵；尚未以
  不可变 digest 发布生产 `dosh`；生产发布与生产回滚仍为**未执行**。

**缺陷关闭顺序**

| 顺序 | 缺陷 | 关闭条件 |
|---|---|---|
| 1 | 使用 SSAR 而非 SAR | 已完成 SAR 对象测试、无 impersonation 和 fail-closed；隔离集群三身份 API/RBAC 矩阵通过 |
| 2 | OIDC provider 无真实签名测试 | 已完成合法/错误签名、issuer、audience、expiry、nonce、JWKS 轮换测试 |
| 3 | Helm 不支持 OIDC/最小 RBAC | 已完成双路径 lint/template 和负向断言；kubeconform/API dry-run 待部署验收 |
| 4 | AppShell OIDC logout 仍走 DELETE session | 已完成组件测试、HTTP 和三浏览器 RP logout 验收 |
| 5 | 无浏览器三身份 E2E | API/RBAC 与 Chromium/Firefox/WebKit 三身份矩阵均已通过 |
| 6 | 未发布/未回滚 | 隔离 Release upgrade/rollback/restore 已通过；仍需 digest 生产发布和生产回滚 |

### 22.13 Kubernetes 429/5xx 有界重试实施包

**关联需求**：NFR-E-001、SEC-U-005、SYS-N-003、TST-U-004、TST-U-006。

**设计决策**

- 重试属于 `platform/kube` 的出站传输适配，不进入 domain、app、HTTP handler 或 React 页面；
  业务用例仍只表达“读取失败/依赖不可用”，不感知重试次数。
- 使用 `rest.Config.WrapTransport` 包装 client-go 的底层 `http.RoundTripper`，使 clientset、
  dynamic、discovery 和 metrics 客户端共享同一策略，避免各 Repository 重复实现。
- 只重试幂等读请求。写请求在响应丢失时重放可能造成重复扩容、重启或删除，因此即使返回 429/5xx
  也只尝试一次，把可定位错误交回 app/UI。
- OIDC impersonation 必须位于 retry 外层：每次实际尝试都应携带当前请求 actor 的
  `Impersonate-User`/`Impersonate-Group`，且 SAR 使用 `domain.WithoutActor` 时不能残留 impersonation 头。
- 重试必须有界。默认 3 次尝试、100 ms 起始退避、2 s 单次上限；`Retry-After` 只作为建议值，
  不能突破上限，也不能忽略 context 取消。
- 重试不得放大请求风暴：当前策略不加入随机 jitter 的并发限流器，依赖 client-go 的 QPS/Burst、
  context deadline 和 3 次上限共同约束；真实注入时必须观测单位时间请求数和 P95 延迟。

**实施动作**

| 文件 | 动作 |
|---|---|
| `internal/platform/kube/retry.go` | 实现 `retryTransport`、状态码判定、网络错误判定、指数退避、`Retry-After` 解析和 context 等待 |
| `internal/platform/kube/client.go` | 在 `NewFromRESTConfig` 中通过 `WrapTransport` 统一安装 retry；OIDC 模式再包裹 impersonation |
| `internal/platform/kube/retry_test.go` | 覆盖 429/503 后成功、写请求不重试、3 次上限、context 取消和 `Retry-After` 截断 |
| 测试夹具/脚本 | 在隔离集群用可控代理或 API Server 前置代理注入 429/5xx、连接重置和慢响应；只允许连接测试 Release |

**组件验收**

```bash
gofmt -l internal/platform/kube
go test -race ./internal/platform/kube \
  -run 'TestRetry|TestImpersonation' -count=1 -v
go test -race ./...
```

通过标准：读请求按状态码有限重试；写请求调用次数始终为 1；重试次数不超过上限；
所有退避在 context 取消后立即停止；`Retry-After` 不超过最大延迟；impersonation 测试不回归。

**集群故障注入验收**

1. 构建包含 retry transport 的候选镜像，部署到隔离 Release；确认生产 `dosh`、`32626` 未改动。
2. 在 Kubernetes API 前置代理上按序注入 429、500、502、503、504 和连接重置；每种故障至少覆盖
   一次“首次失败、后续成功”和一次“持续失败”。
3. 对节点列表、总览、Pod 列表和一次扩容操作分别观测请求序列。读请求最多 3 次；扩容请求最多 1 次，
   且不得因自动重放产生重复写入。
4. 记录网关请求日志、`X-Request-ID`、dosh 响应状态、页面错误文案、延迟和 QPS；确认没有重试风暴，
   持续故障最终返回可定位错误而不是空白页或伪造成功。
5. 在退避中取消浏览器请求，确认服务端 context 尽快结束，goroutine 和连接数回到基线。
6. 保存代理配置、注入时间线、请求计数、截图/日志和 Release revision，写入第 21 节。

**当前状态**：v2.45 当前候选已通过 `scripts/e2e-kube-retry.sh` 完整矩阵：429、500、502、503、504
各连续失败 2 次后恢复 200；持续 429 恰好尝试 3 次并返回 `rate_limited`；`Retry-After` 约 2099 ms；
写请求仅 1 次且副本数保持 `2→2`；连接重置、客户端取消和恢复通过，证据见 21.2.41。生产或预发
同等拓扑仍需复验。

### 22.14 审计 Webhook 商用集成实施包（逐步骤设计、工作与验收）

**关联需求**：SEC-U-004、SEC-U-006、SEC-U-007、SEC-N-003、OPS-U-013、NFR-E-003、NFR-E-004、
TST-U-011、RISK-006。若企业合规要求审计数据不可丢失，还必须追加对应 SIEM 的保留、检索、
告警和访问控制验收；本实施包只关闭 dosh 侧的传输正确性，不替代企业审计系统验收。

**目标**：证明审计事件在“企业私有 CA + Bearer Token/HMAC + 有界队列 + 有限重试 + SIGTERM”
组合下可被真实接收端校验、关联和排空，同时不阻塞业务请求、不泄露凭据。测试接收端可选用 Loki
实验室后端验证真实写入与检索；测试接收端和实验室 Loki 都不能替代企业生产 SIEM/OpenSearch 的
保留、告警、访问审批和不可变存储验收。

#### 22.14.1 实施级 EARS 验收条款

以下条款是 SEC 需求的实施级细化，必须与原需求同时满足。证据必须能关联到候选镜像 digest、
Helm revision、namespace、Release、requestId 和执行时间。

| ID | 类型 | 优先级 | 验收条款 | 最低证据 |
|---|---|---|---|---|
| AUDIT-W-U-001 | U | P0 | 系统应在启用审计 Webhook 时使用标准 TLS 校验；配置私有 CA 后，只信任该 CA 文件中的证书链。 | 私 CA HTTPS 成功；错误 CA 握手失败；代码中无 `InsecureSkipVerify` 开关。 |
| AUDIT-W-O-001 | O | P0 | 当配置 Bearer Token、HMAC Key 或两者时，系统应分别发送 `Authorization`、HMAC 头或二者，且不得把凭据写入 body、日志或审计事件。 | 三种组合的接收端校验和日志扫描。 |
| AUDIT-W-N-001 | N | P0 | 如果 CA 文件不可读、无有效证书、URL 非法或 Token/HMAC 同时缺失，系统应拒绝启动或拒绝渲染，不得退化为明文或匿名投递。 | config、audit、Helm 负向测试；Pod 不进入 Ready。 |
| AUDIT-W-E-001 | E | P0 | 当业务动作产生审计事件时，系统应把 `user/groups/action/resource/namespace/name/result/requestId` 投递给配置的接收端。 | 接收端 JSON、stdout 和 HTTP `X-Request-ID` 三方对账。 |
| AUDIT-W-E-002 | E | P0 | 当发生登录、Secret reveal、扩容、删除、只读拒绝和无权限拒绝时，系统应生成可区分 result 的审计事件；拒绝事件不得伴随 Kubernetes 写操作。 | 业务事件矩阵、副本/Pod 对账、audit 接收端记录。 |
| AUDIT-W-N-002 | N | P0 | 如果 Webhook 返回非 2xx、超时或被限流，系统应在同一业务请求之外有限重试，最终失败可检索，且不得改变业务响应或阻塞请求。 | 503/超时注入、尝试次数、业务延迟和错误日志。 |
| AUDIT-W-S-001 | S | P1 | 当队列达到容量时，系统应丢弃新 Webhook 副本并记录 `audit_webhook_queue_full`，stdout 审计和业务请求仍应成功。 | 小队列 + 慢接收端压测；正常请求延迟和日志。 |
| AUDIT-W-E-003 | E | P0 | 当进程收到 SIGTERM 时，系统应在 HTTP 停止接收新请求后，在有界宽限期内排空已入队审计；超时必须记录并可定位。 | 滚动重启期间无已入队事件丢失；超时分支有日志。 |
| AUDIT-W-U-002 | U | P0 | 启用 NetworkPolicy/Cilium 时，系统应仅放行显式配置的 Webhook CIDR 和端口；不得复用 API Server 的 `0.0.0.0/0` 出口。 | 普通 NetworkPolicy 与 CNP 渲染断言；隔离集群真实投递。 |
| AUDIT-W-N-003 | N | P0 | 如果接收端 Token 或 HMAC 不匹配，接收端应拒绝，dosh 不得把拒绝当作成功；错误日志不得包含原始凭据。 | 401/错误签名测试和日志扫描。 |

#### 22.14.2 端到端设计与边界

```text
Browser / API client
  -> dosh app use case
       -> audit.Logger.Record
            -> stdout JSON（始终保留）
            -> bounded queue -> delivery worker
                 -> HTTPS + 私 CA 校验
                 -> Bearer Token / HMAC-SHA256 / 两者
                 -> X-Request-ID
                 -> 审计接收端（校验 Bearer/HMAC、请求 ID 和故障行为）
                      -> 可选 Loki 实验室后端（仅 Loki 2xx 后向 dosh 确认）
  -> 业务响应在审计旁路之外完成，不等待 Webhook 成功
```

设计约束：

1. Webhook 是旁路。业务成功/失败由 domain、授权和 Kubernetes 调用决定，不能因为接收端不可用而改写业务响应。
2. 审计事件先在内存队列中形成副本，stdout 永远保留；Webhook 队列满或最终失败必须可观测。
3. TLS 信任来自 `DOSH_AUDIT_WEBHOOK_CA_FILE`，不得用全局 `DOSH_INSECURE_SKIP_VERIFY` 替代审计信任配置。
4. Token/HMAC 只从 Kubernetes Secret 注入；CA 只从只读 ConfigMap/Secret 挂载；values、镜像和日志不保存明文。
5. 普通 NetworkPolicy 与 CiliumNetworkPolicy 都必须显式渲染审计 Webhook 出口；CNP 不能只允许 kube-apiserver。
6. 测试接收端是专用夹具，不进入生产 chart，不承载生产审计数据；生产验收必须替换为企业 SIEM/Loki/OpenSearch。
7. 测试资源固定使用独立 namespace/Release/NodePort，任何命令执行前都必须先断言生产 `dosh`、`32626` 和当前 revision 未变化。
8. Loki 实验室后端只能使用受控镜像 digest、私 CA、租户鉴权和临时测试凭据；不得把 `emptyDir`、
   临时 token 或实验室端口转发配置复制到生产 values。

#### 22.14.3 步骤 1：冻结候选与建立隔离 fixture

**设计**：审计能力变更后不能继续沿用旧候选。必须冻结 App、Chart、OpenAPI、web package 和构建标识，
构建新的不可变镜像。集群验收使用独立 `dosh-audit-e2e` namespace、`dosh-audit-e2e` Release、
`32628` NodePort、目标 namespace `dosh-audit-target`；生产 `dosh` 只做只读快照，不执行升级。

**做哪些事**

1. 记录生产发布前快照：Release revision、values hash、Deployment 镜像 digest、Service NodePort、Pod Ready 数。
2. 冻结版本号并同步 `Makefile`、`Chart.yaml`、`values.yaml`、`api/openapi.yaml`、`web/package.json` 和 README。
3. 生成构建标识；镜像 tag 必须可追溯到源码快照，最终以 registry digest 为准。
4. 在隔离环境生成测试 CA、服务端证书、Token 和 HMAC Key；证书 SAN 必须包含测试接收端地址。
5. 创建 `dosh-audit-e2e` namespace、目标 namespace、TLS Secret、认证 Secret 和审计 CA ConfigMap。

**验收**

```bash
kubectl -n dosh get deploy dosh -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
kubectl -n dosh get svc dosh -o jsonpath='{.spec.ports[0].nodePort}{"\n"}'
kubectl -n dosh get pods -l app.kubernetes.io/instance=dosh --no-headers
```

通过标准：生产镜像 digest、revision 12、NodePort `32626`、2/2 Ready 在测试前后完全一致；测试资源
全部位于独立 namespace/Release；TLS 证书 SAN 与测试 URL 匹配；CA、Token、HMAC 不出现在 values 输出。

#### 22.14.4 步骤 2：后端审计传输与配置契约

**设计**：`platform/audit` 负责 URL、TLS、认证、重试、队列和关闭；`platform/config` 负责启动前校验；
`bootstrap` 只装配并在 HTTP 服务停止后调用 `Close`。业务模块只依赖 `audit.Logger.Record`，不得感知
HTTP 传输细节。

**做哪些事**

| 文件 | 动作 | 当前状态 |
|---|---|---|
| `internal/platform/audit/audit.go` | 增加私 CA 加载、TLS transport、Bearer/HMAC、重试、队列、`Flush/Close` | 已实现 |
| `internal/platform/config/config.go` | 增加 `DOSH_AUDIT_WEBHOOK_CA_FILE`、URL/上限/凭据组合校验 | 已实现 |
| `internal/bootstrap/app.go` | 处理 `NewWithConfig` 失败；HTTP 停止后有界关闭审计 | 已实现 |
| `internal/platform/audit/*_test.go` | 覆盖私 CA、错误 CA、Bearer、HMAC、Request ID、Flush/Close、凭据脱敏 | 已实现 |
| `internal/platform/config/config_test.go` | 覆盖非法 URL、缺少凭据、超时/队列边界 | 已实现 |

**自动验收**

```bash
gofmt -l internal/platform/audit internal/platform/config internal/bootstrap
go test -race ./internal/platform/audit ./internal/platform/config ./internal/bootstrap -count=1 -v
```

通过标准：TLS 1.2+；错误 CA 启动失败；Webhook 未启用时不创建后台投递；Bearer-only 不发送 HMAC 头；
HMAC-only 不发送 Authorization；两者并用时全部存在；请求 ID 一致；`Flush/Close` 不丢已入队记录；
日志和错误文本不包含 Token、HMAC、CA 私钥或 Secret 明文。

#### 22.14.5 步骤 3：Helm、Secret、CA 与 Cilium 出口

**设计**：Helm 只负责把外部 Secret/ConfigMap 映射为环境变量和只读卷，不生成或回显凭据。
Token/HMAC 至少一个存在；CA ConfigMap 可选；CA 内容变化应触发 Pod 滚动，因此增加 audit CA checksum
注解。所有 NetworkPolicy/CNP 从 `networkPolicy.webhook` 读取目标 CIDR/端口，未配置 CIDR 时渲染失败。

**做哪些事**

1. `deploy/helm/dosh/values.yaml`：保留 `audit.webhook.tokenKey`、`hmacKeyKey`、`ca.existingConfigMap`、
   `ca.key`、`networkPolicy.webhook.ipBlocks/ports`。
2. `deploy/helm/dosh/templates/deployment.yaml`：Token/HMAC 使用 `secretKeyRef`；CA 使用只读 ConfigMap
   卷和 `DOSH_AUDIT_WEBHOOK_CA_FILE`；增加 audit CA checksum 注解；Token/HMAC 都为空或 URL 非绝对
   HTTPS 时 `fail`。
3. `deploy/helm/dosh/templates/networkpolicy.yaml`：审计启用且 NetworkPolicy 启用时要求显式 CIDR；
   只渲染 `networkPolicy.webhook.ipBlocks` 与 `ports`。
4. `deploy/helm/dosh/templates/ciliumnetworkpolicy.yaml`：新增 `$webhook`；审计启用时渲染独立
   `toCIDR`/`toPorts` 出口；禁止把审计流量并入 API Server 出口。
5. `scripts/check-helm.sh`：新增 `dosh-audit-cilium.yaml` 正向渲染；断言审计 CIDR、端口、
   Token/HMAC/CA 引用；负向断言缺少 CIDR、Token 与 HMAC 都为空、明文 HTTP URL、`0.0.0.0/0`。

**自动验收**

```bash
make helm-check
helm template dosh deploy/helm/dosh -n dosh \
  --api-versions cilium.io/v2 \
  --set audit.webhook.enabled=true \
  --set audit.webhook.url=https://192.168.122.1:9444/audit \
  --set audit.webhook.tokenKey=token \
  --set audit.webhook.hmacKeyKey= \
  --set audit.webhook.ca.existingConfigMap=dosh-audit-ca \
  --set networkPolicy.webhook.ipBlocks[0]=192.168.122.1/32 \
  > /tmp/dosh-audit-cilium.yaml
grep -n -A12 'name: DOSH_AUDIT_WEBHOOK' /tmp/dosh-audit-cilium.yaml
grep -n -A20 'kind: CiliumNetworkPolicy' /tmp/dosh-audit-cilium.yaml
```

通过标准：Secret/CA 只出现引用和只读挂载；Token-only 不出现 HMAC env；HMAC-only 不出现 Token env；
CNP 和普通 NetworkPolicy 均出现审计 CIDR/端口；任何 `0.0.0.0/0` 审计出口、缺少 CIDR 或缺少凭据
都使渲染失败。

当前状态：上述正向渲染与负向拒绝已纳入 `make helm-check`。其中审计 CNP 使用独立
`toCIDR`/`toPorts`，示例断言为 `203.0.113.30/32:443`；这只能证明模板正确，仍需在隔离 Release
上验证 Cilium 实际放行与接收端连通性。

#### 22.14.6 步骤 4：构建测试审计接收端

**设计**：`tools/auditreceiver` 是隔离测试夹具，提供 `/healthz`、`/audit`、`/stats`；支持 TLS、Bearer、
HMAC、`X-Request-ID` 去重和事件计数。为验证重试和恢复，还需增加受控故障模式（503、固定延迟、
凭据拒绝恢复）；故障开关只能通过测试参数/环境变量启用，默认直通。可选 Loki 后端只做真实实验室
写入与检索证据，不能在 Loki 失败前向 dosh 返回成功。

**做哪些事**

1. 扩展 `tools/auditreceiver/main.go`：故障模式、失败次数、延迟和恢复接口；保留 `/stats` 的
   `attempts/received/verified/faultRemaining/authMode/events`，并保证 `/control` 只用于测试。
2. 补齐测试：Token-only、HMAC-only、两者并用、错误签名、错误 Token、重复 Request ID、503 后恢复、
   超时、慢接收端、SIGTERM 优雅退出。
3. 当前验收路径由 `scripts/e2e-audit-webhook.sh` 在宿主机编译并启动 Go 二进制；若后续改为容器化，
   必须补齐 `bin/audit-receiver` 构建产物、镜像 digest 和启动命令后再纳入门禁，不能引用未构建的镜像。
4. 在跳板机绑定 `192.168.122.1:9444` 并以私 CA 启动接收端，使审计出口可被
   `ipBlock` 精确表达；不得用集群内 ClusterIP 绕过出口验证。
5. Loki 后端由 `AUDIT_LOKI_URL`、`AUDIT_LOKI_CA_FILE`、`AUDIT_LOKI_TENANT` 和
   `AUDIT_LOKI_TIMEOUT` 启用；push 时间戳必须是 Unix 纳秒字符串，标签至少包含 `job` 和 `service`。
   单测必须覆盖 Loki 2xx 才确认、Loki 拒绝时不确认、租户头和 CA 校验。

**验收**

```bash
gofmt -l tools/auditreceiver
go test -race ./tools/auditreceiver -count=1 -v
go build -o /tmp/dosh-audit-receiver ./tools/auditreceiver
```

通过标准：无 Token 或错误 Token 返回 401；错误 HMAC 返回 401；正确 Token+HMAC 返回 2xx；重复
Request ID 不重复计数；故障模式可被脚本控制且能恢复；Loki 拒绝时 dosh 不会收到成功确认；
测试进程与证书只存在于隔离验收目录。

#### 22.14.7 步骤 5：隔离 Release 安装与连通性

**设计**：使用独立 namespace/Release/NodePort 安装新候选，审计 URL 指向跳板机私 CA 接收端；
OIDC 测试 IdP 与本审计接收端分别配置精确 CIDR。生产 `dosh` 不参与升级、回滚或 Secret 变更。

**执行顺序**

1. 创建 `dosh-audit-e2e`、`dosh-audit-target`；创建 `dosh-audit-e2e-tls`、`dosh-audit-e2e-auth`、
   `dosh-audit-e2e-oidc`、`dosh-audit-ca` Secret/ConfigMap。
2. 安装候选 Chart：replicaCount 可设为 1 以降低夹具成本，但 PDB/安全上下文/探针保持生产同构；
   NodePort 固定 `32628`。
3. 断言 `/healthz`、`/readyz`、登录、`/api/v1/overview` 可用，且审计接收端收到至少一条登录事件。
4. 检查 Pod 日志中没有 Token、HMAC、CA 私钥、Secret 明文；接收端 TLS 握手成功。
5. 故意配置错误 CA 或错误 Token，确认失败可定位并恢复正确配置；测试过程中不得修改生产 Release。

**验收命令**

```bash
kubectl -n dosh-audit-e2e get deploy,pod,svc,pdb,sa -o wide
kubectl -n dosh-audit-e2e get svc dosh -o jsonpath='{.spec.ports[0].nodePort}{"\n"}'
curl -sS --cacert /tmp/dosh-audit-e2e/ca.crt https://192.168.122.100:32628/healthz
curl -sS --cacert /tmp/dosh-audit-e2e/ca.crt https://192.168.122.1:9444/stats
```

通过标准：测试 Release Ready；NodePort 为 `32628`；健康检查 200；接收端 `/stats` 可读；生产
`dosh` digest、revision、`32626` 和 Ready 数完全未变化。

#### 22.14.8 步骤 6：业务事件与拒绝语义矩阵

**设计**：审计条目必须来自真实业务用例，而不是用手工 POST 伪造。至少覆盖本地/OIDC 登录、Secret
reveal、扩容、重启、删除、无权限写拒绝和只读拒绝；每条事件使用同一 `requestId` 关联 HTTP 响应、
stdout 和接收端。

| 场景 | 期望 result | Kubernetes 副作用 | 审计字段检查 |
|---|---|---|---|
| 管理员登录成功 | success | 无 | user、requestId、timestamp |
| 登录失败 | denied | 无会话 | user/来源可定位，无密码/Token |
| Secret reveal 成功 | success | 只读，不改变 Secret | namespace/name/action，payload 无 Secret 值 |
| viewer reveal/写操作 | denied | 禁止写；Repository 调用为 0 | result=denied，requestId 可关联 403 |
| operator 扩容到 N | success | 副本数恰好改变一次 | action=scale，前后副本数对账 |
| admin 删除测试 Deployment | success | 仅目标 Deployment 消失 | namespace/name/result |
| 只读模式写操作 | denied | 资源不变 | result=denied，无写请求 |

**验收**

```bash
DOSH_E2E_SKIP_BUILD=1 \
DOSH_E2E_IMAGE_TAG=<candidate-tag> \
DOSH_E2E_KEEP_ARTIFACTS=1 \
DOSH_E2E_ARTIFACT_DIR=/tmp/dosh-audit-e2e \
  bash scripts/e2e-audit-webhook.sh

# 真实 Loki 实验室后端（同时保留 artifact）
make e2e-audit-loki
```

脚本不接受上述命令行位置参数，隔离命名空间、Release、NodePort、接收端地址和凭据均由
`DOSH_E2E_AUDIT_*`/`DOSH_E2E_*` 环境变量配置；默认 namespace 为 `dosh-e2e-audit`、Release 为
`dosh-audit-e2e`、NodePort 为 `32628`、接收端为 `https://192.168.122.1:9444`。脚本会自动生成
私 CA、启动接收端和 OIDC fixture，并在退出时清理；`DOSH_E2E_KEEP_ARTIFACTS=1` 保留原始证据。
设置 `DOSH_E2E_AUDIT_BACKEND=loki` 时，脚本额外部署 `dosh-e2e-loki`、通过 port-forward 写入
并回查；该模式要求内部 registry 中的 Loki digest 可用。

通过标准：每条成功/拒绝动作都有对应事件；同一 requestId 在响应、stdout 和接收端一致；
`verified=true`；Token/HMAC/Secret 明文扫描 0 命中；拒绝场景 Kubernetes 资源版本或副本数不变；
测试 Deployment/Pod/ConfigMap 在结束后清理。

#### 22.14.9 步骤 7：接收端故障、重试和队列压力

**设计**：审计故障只能影响审计旁路，不能改变业务成功语义。单条记录最多尝试 3 次，间隔 200 ms、400 ms；
每次受 `DOSH_AUDIT_WEBHOOK_TIMEOUT` 约束；队列满时丢 Webhook 副本并记录 `audit_webhook_queue_full`；
接收端恢复后新事件继续投递，不得需要重启 dosh。

| 注入 | 操作 | 通过标准 |
|---|---|---|
| 前两次 503，第三次 200 | 触发一次可审计读/写动作 | 接收端看到 3 次请求；业务正常；第 3 次成功 |
| 持续 503 | 触发一次动作 | 恰好 3 次尝试；最终失败日志可检索；业务仍返回真实结果 |
| 响应延迟 > 超时 | 触发一次动作 | 每次受超时截断；最多 3 次；无无限等待 |
| 错误 Token/HMAC | 触发一次动作 | 接收端 401；dosh 记录失败；日志不泄露凭据 |
| queueSize=1 + 慢接收端 | 并发触发多条审计事件 | 业务请求不阻塞；出现 `audit_webhook_queue_full`；stdout 仍完整 |
| 接收端恢复 | 再次触发动作 | 新事件恢复投递；`/stats` 增加；无需重启 Pod |

**验收**：保存注入时间线、接收端请求计数、dosh 错误日志、业务 HTTP 状态和 P95 延迟；错误日志中
不得出现 Authorization/HMAC 原文；任何“Webhook 失败但业务显示失败”或“写操作被重试放大”都判失败。

#### 22.14.10 步骤 8：SIGTERM 排空与滚动升级

**设计**：Pod 收到 SIGTERM 后，HTTP server 先停止接收新请求；`bootstrap.App.Run` 再在 10 s 有界
context 内调用 `audit.Logger.Close`，等待已入队记录投递。超过期限记录 `audit_flush_failed` 并退出，
不得无限挂起。滚动升级验收必须使用慢但可完成的接收端，避免把测试夹具超时误判为产品丢数据。

**执行**

1. 将接收端延迟设为 200 ms，队列容量至少 64；通过业务动作产生 20 条带唯一 requestId 的事件。
2. 等待事件进入队列后执行 `kubectl -n dosh-audit-e2e rollout restart deployment/dosh`，记录
   Pod termination 时间、退出码和新 Pod Ready 时间。
3. 对比发送前事件集合与接收端 `/stats`，确认已入队事件全部被接收且无重复业务副作用。
4. 再把接收端设为不可达，重复滚动升级；确认 `audit_flush_failed` 在 10 s 内出现，Pod 不被无限阻塞。
5. 检查 stdout 与接收端均无 Token/HMAC/Secret；记录新 Pod 的 digest 仍为候选 digest。

通过标准：可完成场景无已入队事件丢失；不可达场景有界退出并可定位；滚动升级期间至少一个副本
可用；业务请求不因审计排空被无限挂起；接收端和新 Pod 的 revision/digest 对账一致。

#### 22.14.11 步骤 9：证据归档与状态提升

每个实施动作必须产生一套不可变证据。建议目录结构：

```text
evidence/<candidate-digest>/audit-webhook/
  environment.txt           # cluster/context/namespace/release/nodePort/time
  production-before.json    # 生产 revision/digest/ready/nodePort 快照
  helm-rendered.yaml        # 审计 + Cilium 渲染结果（已脱敏）
  receiver.log              # 接收端启动、TLS、请求与故障注入日志
  dosh-pod.log              # stdout 审计与失败日志（已脱敏）
  events.jsonl              # user/action/resource/result/requestId
  retry-timeline.txt        # 次数、状态码、延迟、最终结果
  sigterm-timeline.txt      # 滚动升级前后事件集合与 Pod 生命周期
  scan.txt                  # Token/HMAC/Secret 泄露扫描结果
  summary.txt               # 归档统计；必须被 manifest.sha256 覆盖
  manifest.sha256           # 除自身和 manifest-digest.txt 外全部文件的 SHA-256
  manifest-digest.txt       # manifest.sha256 自身的 SHA-256（外锚点）
```

**状态提升规则**

- 只有步骤 3–8 的自动和集群证据都通过，才可把 WBS-8.11 从“已实现”提升为“实验室已验收”。
- 只有新候选镜像 digest、最终 Chart、生产或预发企业接收端和回滚证据齐全，才可提升为“已验收”。
- 测试接收端通过不自动等于真实 SIEM/Loki/OpenSearch 通过；真实 SIEM 的保留、检索、告警和访问控制
  仍需按企业合规单独验收。
- 任一步骤失败时保持“未验收”，先区分产品缺陷、测试夹具缺陷和外部依赖故障，不得用重跑掩盖失败。

**归档执行命令**

```bash
make evidence-check
bash scripts/archive-evidence.sh \
  --candidate-digest sha256:<64-lowercase-hex> \
  --label <change-or-release-id> \
  --output-root /var/lib/dosh/evidence \
  <evidence-file-or-directory>...
cd /var/lib/dosh/evidence/<digest-prefix>/<timestamp>-<label>
sha256sum -c manifest.sha256
sha256sum -c manifest-digest.txt
grep -Eq '  \./summary\.txt$' manifest.sha256
```

通过标准：`summary.txt` 被 manifest 覆盖；`manifest-digest.txt` 只锚定 manifest 且不自引用；
两轮校验无 `FAILED`；归档目录权限 `0700`、文件权限 `0600`；私钥、Token、Cookie、session 和
请求/响应敏感转储 0 命中。命令输出的 `manifest_sha256` 必须写入发布记录或变更单；本机归档
不能替代离机 WORM/长期保留。

**当前状态（2026-11-04，v2.45 当前候选；Loki 为 v2.44 历史证据）**

- 已实现：私 CA、Token-only/HMAC-only/组合认证、配置校验、组件级重试/队列/关闭排空、测试接收端、
  可选 Loki push、隔离 E2E 脚本，以及 Cilium 审计精确出口。
- v2.45 已通过：隔离 Audit/Reveal 完成私 CA、Token/HMAC、业务成功/拒绝矩阵、瞬时/持续 503、
  超时恢复、队列满、只读拒绝和 20/20 SIGTERM 排空；token/hmac/secret 泄露扫描 0 命中。
- v2.44 历史 Loki 实验室已通过：真实写入 59 条事件、回查 34 个 requestId，SIGTERM 20/20 命中，
  标签与租户隔离验证通过；v2.45 未复跑真实 Loki，证据见 21.2.38 和受控归档
  `/var/lib/dosh/evidence/69f30f001651/20261103T221524Z-v233-loki-final`。
- 未完成：企业生产 SIEM/OpenSearch 的持久化、告警、访问审批、容量 SLA、长期保留和离机 WORM。
  因此 WBS-8.11 为“dosh 传输与实验室 Loki 已验收”，不是最终“企业审计系统已验收”。

### 22.15 节点 Cordon/Uncordon 实施包（NODE-O-001）

#### 22.15.1 实施级 EARS

| ID | 条件 | 系统行为 | 验收 |
|---|---|---|---|
| NODE-O-001-A | 当已认证用户请求停止节点调度且具备 `update nodes` 权限时 | 系统应把 Node 的 `spec.unschedulable` 置为 `true`，不驱逐现有 Pod，并返回更新后的节点摘要 | fake client、真实 API、kubectl 对账 |
| NODE-O-001-B | 当节点已处于目标调度状态时 | 系统应幂等返回当前摘要，不重复发送 Update，避免无意义 resourceVersion 变化 | kube 单测断言 Update 次数 |
| NODE-O-001-C | 当用户缺少权限或系统处于只读模式时 | 系统应在 Repository 之前拒绝请求，返回 403/只读语义，并记录 `denied` 审计 | app/http 单测、真实 viewer/operator 矩阵 |
| NODE-O-001-D | 当 Kubernetes 读取或写入失败时 | 系统应映射稳定错误语义，记录 `failure` 审计，且不得伪报成功 | app/http/kube 单测 |
| NODE-O-001-E | 当管理员在节点详情页执行 Cordon/Uncordon 时 | 前端应显示当前状态对应的动作、执行前二次确认、提交 CSRF 请求，并在成功后刷新列表和详情 | Vitest、Chromium/Firefox mock E2E |
| NODE-O-001-F | 当 local 模式渲染 RBAC 时 | Chart 只应授予 `nodes` 的 `update/patch`，不得顺带授予 namespace 写权限 | Helm render 审查 |

#### 22.15.2 分层设计

1. `internal/domain/types.go`：增加 `Capabilities.NodeCordon`；不引入 Kubernetes 类型。
2. `internal/modules/nodes/app/service.go`：Repository 增加 `SetNodeScheduling`；Service 注入
   `domain.Authorizer` 与 `domain.Auditor`，提供 `Cordon`/`Uncordon`，统一处理授权、结果和审计。
3. `internal/platform/kube/nodes.go`：实现 Get → Pod 列表 → 条件 Update → DTO/指标组装；复用
   `mapKubeError`，保留 NotFound/Forbidden/Conflict/Unavailable 语义。
4. `internal/modules/nodes/http/handler.go`：挂载两个 POST 路由，返回 `NodeSummary`；全局 Session
   与 CSRF 中间件继续生效。
5. `api/openapi.yaml` 与生成类型：新增两个路径和 `nodeCordon` capability；`make api-check` 必须
   逐字节一致。
6. `web/src/features/nodes/node-detail-page.tsx`：能力门控、状态化按钮、AlertDialog 确认、
   pending 禁用、成功刷新和错误 toast。
7. `deploy/helm/dosh/templates/rbac.yaml`：local 模式为 `nodes` 单独增加写规则；OIDC 模式不改。

#### 22.15.3 执行步骤与验收命令

1. 实现 app/kube/http/OpenAPI/前端/Helm 变更：

```bash
go test ./internal/modules/nodes/... ./internal/platform/kube ./internal/modules/auth/... ./internal/bootstrap
cd web && npm run typecheck && npm test
make verify
make race
```

2. 运行 mock 浏览器矩阵，Chromium/Firefox 均应 `16/16`，且只能单 worker 串行：

```bash
bash scripts/run-playwright-container.sh --project=chromium --retries=0
bash scripts/run-playwright-container.sh --project=firefox --retries=0
```

3. 冻结新 digest 后，在隔离 Release 跑真实 API/RBAC；默认节点必须显式指定且测试后恢复：

```bash
DOSH_E2E_SKIP_BUILD=1 \
DOSH_E2E_IMAGE_TAG=<tag> \
DOSH_E2E_IMAGE_DIGEST=sha256:<digest> \
DOSH_E2E_CORDON_NODE=worker-0005 \
  bash scripts/e2e-oidc.sh
```

4. 保存 `kubectl` 节点状态、revision/digest、三身份响应、审计 `actor/action/resource/result/request_id`
   和生产前后快照；脚本失败时 trap 必须恢复原始调度状态。

#### 22.15.4 回滚与边界

- 功能回滚：回退到上一 Chart/镜像 digest；若节点仍为 `unschedulable=true`，先用 `kubectl uncordon`
  恢复，或以原值执行 merge patch。
- 权限回滚：删除 `nodes` 的 `update/patch` 规则后，local 模式按钮会因 capability 失效而隐藏，
  后端直接返回 403。
- 禁止在生产节点上执行验收；cordon 不驱逐 Pod，但仍会影响后续调度，必须选择受控 worker 并在
  脚本 trap 恢复。
- WebKit 不得在物理宿主直接运行；需要时在 `dosh-browser-runner` 或 CI 执行。

## 23. 验收记录模板

每次发布复制以下模板到变更单或版本记录。尖括号字段必须按本次发布替换；状态只允许填写
`pass`、`fail` 或 `not-run`，不得继承示例、历史候选或上一 revision 的通过状态。

```yaml
release:
  app_version: <app-version>
  chart_version: <chart-version>
  image: <registry>/<repository>:<tag>
  image_digest: sha256:<registry-digest>
  commit: <git-sha-or-build-id>
  helm_revision: <revision>
  namespace: dosh
  node_port: 32626
  verified_at: <RFC3339-time>
  verifier: <name>
gates:
  gofmt: <pass|fail|not-run>
  go_vet: <pass|fail|not-run>
  go_test: <pass|fail|not-run>
  go_race: <pass|fail|not-run>
  web_typecheck: <pass|fail|not-run>
  web_test: <pass|fail|not-run>
  web_build: <pass|fail|not-run>
  arch: <pass|fail|not-run>
  openapi_generate: <pass|fail|not-run>
  openapi_drift: <pass|fail|not-run>
  govulncheck: <pass|fail|not-run>
  npm_audit: <pass|fail|not-run>
  helm_lint: <pass|fail|not-run>
  cluster_smoke: <pass|fail|not-run>
  rbac_scale: <pass|fail|not-run>
  secret_no_store: <pass|fail|not-run>
  logs_stream: <pass|fail|not-run>
  events_sse: <pass|fail|not-run>
  yaml_dry_run: <pass|fail|not-run>
  write_cycle: <pass|fail|not-run>
  rollback: <pass|fail|not-run>
  image_scan: <pass|fail|not-run>
  playwright_e2e: <pass|fail|not-run>
  capacity_slo: <pass|fail|not-run>
  production_lcp: <pass|fail|not-run>
  bundle_budget: <pass|fail|not-run>
  network_policy: <pass|fail|not-run>
  metrics_adapter: <pass|fail|not-run>
  metrics_fallback: <pass|fail|not-run>
  metrics_real_cluster: <pass|fail|not-run>
  metrics_available_disabled_recovered: <pass|fail|not-run>
  kubernetes_retry_unit: <pass|fail|not-run>
  kubernetes_retry_fault_injection: <pass|fail|not-run>
  url_state_persistence: <pass|fail|not-run>
  audit_webhook_signing: <pass|fail|not-run>
  audit_retry_queue: <pass|fail|not-run>
  audit_shutdown_flush: <pass|fail|not-run>
  audit_siem_delivery: <pass|fail|not-run>
  oidc_authorization_code_pkce: <pass|fail|not-run>
  oidc_state_nonce_redirect: <pass|fail|not-run>
  oidc_real_signature_issuer_audience: <pass|fail|not-run>
  oidc_subject_access_review: <pass|fail|not-run>
  oidc_minimal_rbac: <pass|fail|not-run>
  oidc_three_identity_matrix: <pass|fail|not-run>
  oidc_rp_logout: <pass|fail|not-run>
  oidc_token_leak_scan: <pass|fail|not-run>
  ci_github_actions: <pass|fail|not-run>
risks:
  - id: <RISK-ID>
    owner: <name>
    accepted: false
```

## 24. 需求状态口径

本文档中的“已实现”只表示代码存在并通过基础门禁；“已验收”表示同时满足：

- 有自动化或真实集群证据；
- 证据与需求编号对应；
- 失败分支、权限分支和空依赖分支至少被验证一次；
- 没有未登记的 Blocker 或未接受的 Major 风险。

因此，不能因为页面可打开、接口返回 200 或 Helm Pod Ready 就宣称全部 P0 已验收；应以第 21 节的实际状态和第 16 节追踪矩阵为准。

## 25. `0.1.1` App / `0.1.3` Chart 发布执行核对单

本节记录一次可复现的完整发布过程。后续版本应复制本节，替换版本、digest 和时间，
不得覆盖历史记录。

### 25.1 发布身份

| 字段 | 值 |
|---|---|
| Chart | `dosh-0.1.3` |
| App version | `0.1.1` |
| 镜像 | `192.168.122.10:5000/dosh/dosh:0.1.1` |
| Registry digest | `sha256:a326a48a30195f29c8a5681f7d11d90cdc131e1b937210cd4556f3703080e5a0` |
| 构建标识 | `local-20261102` |
| Namespace | `dosh` |
| Helm Release | `dosh` |
| 最终 revision | 10（保留 revision 2/3 的回滚演练；revision 8 引入 NetworkPolicy，revision 9 升级 Chart，revision 10 启用 Cilium 自动探测并收紧 API 出口） |
| Service | `NodePort 32626:32626/TCP` |
| 最终 Pod | `worker-0003`、`worker-0005`，2/2 Ready |
| 验证时间 | 2026-11-02T10:37Z |

### 25.2 执行步骤与验收结果

| 步骤 | 做什么 | 验收动作 | 本次结果 |
|---|---|---|---|
| 1. 冻结版本 | 同步更新 Chart、values、OpenAPI、前端 package 和镜像标签 | `rg '0\\.1\\.1'`，人工确认无旧标签残留 | 通过 |
| 2. 后端门禁 | 格式化、静态检查、单元测试和竞态检查 | `gofmt -l`、`go vet ./...`、`go test -race ./...` | 通过 |
| 3. 架构门禁 | 阻止 domain/app 依赖 HTTP、chi、client-go | `make arch` | 通过 |
| 4. 契约门禁 | 从 OpenAPI 重建 TS 类型并比较漂移 | `make api-check` | 通过 |
| 5. 前端门禁 | 类型检查、集成测试、生产构建 | `npm run typecheck`、`npm test`、`npm run build` | 5/5 测试通过，构建通过 |
| 5a. 浏览器 E2E | 六条主路径在 Chromium、Firefox、WebKit 执行 | `npm run e2e:container` | 18/18 通过 |
| 5b. 容量与前端性能 | 1000 Pod/200 工作负载、生产 LCP、bundle 预算 | `make capacity-test`、`npm run e2e:performance`、`npm run bundle:check` | P95 21/185/199 ms；LCP 572 ms；初始 JS 334.54 KiB |
| 6. 依赖安全 | 扫描 Go 调用链与 npm 生产依赖 | `govulncheck ./...`、`npm audit --omit=dev` | Go 调用链 0；npm 生产依赖 0 |
| 6a. 镜像安全 | 扫描 OS 与 Go 二进制 HIGH/CRITICAL | `make image-scan TAG=0.1.1` | 0 个 HIGH/CRITICAL |
| 7. Helm 静态验收 | lint、渲染并断言端口/副本/安全上下文 | `helm lint`、`helm template ... -n dosh` | 通过 |
| 7a. 网络策略 | 渲染并验证默认拒绝、32626 入口、DNS 出口及 Cilium API Server 实体出口 | `helm template` 双路径、CNP `Valid`、端到端回归 | 通过 |
| 8. 镜像发布 | 构建 distroless 镜像，推送并记录 registry digest | `make image TAG=0.1.1`、`podman push` | 通过 |
| 9. 集群升级 | 在 `dosh` 命名空间执行 `helm upgrade --wait` | `helm version`、`helm history`、`rollout status` | revision 10 升级成功 |
| 10. 运行验收 | 健康、认证、CSRF、只读 API、Secret、日志和 SSE | 第 15.5 至 15.7 节命令 | 全部通过 |
| 11. 写操作验收 | 临时命名空间中 dry-run、apply、扩容、409、重启、删除 | API 结果与 Kubernetes 对账 | 全部通过 |
| 12. 回滚演练 | 回滚到 revision 2，再恢复 revision 3 的配置 | health/ready/login 与镜像核对 | 回滚和恢复均通过 |
| 13. 清理 | 删除临时命名空间和测试对象 | `kubectl get namespace dosh-e2e-smoke` | 无资源 |

### 25.3 发布结论

App `0.1.1` / Chart `0.1.3` 满足受控实验室环境的首版运行门槛：固定端口、固定命名空间、
认证与 CSRF、Secret 保护、日志/SSE、受控写操作、2 副本高可用配置、回滚链路、
跨浏览器 E2E、容量/P95、LCP、bundle 预算、镜像扫描和 NetworkPolicy 均有真实证据。

但本次 `0.1.1/0.1.3` 发布仍**不满足“完整可商用”**。以下事项在发布时未完成，发布结论只能是“实验室内部可用”：

- 未执行 30 分钟长连接 soak、429/5xx 故障注入和 Edge 实机测试；
- 当前为本地单管理员、自签 TLS；该版本审计仅 stdout，未满足生产多用户与合规要求。
- 该版本发布时尚无 CI；工作树虽已加入 `.github/workflows/ci.yml`，但在首次 GitHub Actions 绿灯前仍不能作为发布阻断证据。

### 25.4 下一版本准入门槛

进入生产试点前至少满足：

1. 第 25.3 节列出的未完成项全部关闭，或在风险登记表中由风险负责人逐项书面接受。
2. 将 TST-U-002、TST-U-003、TST-U-006、TST-U-008、TST-U-009、TST-U-010、TST-U-011、TST-U-012 对应门禁接入 CI，并取得真实绿灯。
3. 用受信 CA 替换 `dosh-tls`，并验证证书 SAN 覆盖全部访问入口。
4. 完成 30 分钟长连接 soak、Kubernetes API 429/5xx 故障注入和 Edge 实机浏览器验收。
5. 已完成签名测试 IdP 的三身份 API/RBAC、三浏览器 E2E、Token 扫描、OIDC rollback/restore 和最小
   ServiceAccount 权限核对；仍需完成企业 IdP 兼容性、digest 生产发布和生产回滚。未完成前保持内网、
   单管理员和最小暴露面。

### 25.5 `0.1.2` / `0.1.4` 发布核对单（已部署，证据回填）

本节记录 `0.1.2` / Chart `0.1.4` 的历史发布核对。集群现为 revision 12、2/2 Ready；
未在本次会话重新执行的 E2E、性能、漏洞扫描和回滚不得自动视为通过。OIDC 改动属于新的
未冻结候选。签名测试 IdP 的候选已部署到隔离 Release `dosh-oidc-e2e` 做 API/RBAC 验收，
但没有覆盖生产 `dosh`，也不并入本表冒充生产发布。

| 步骤 | 做什么 | 验收命令/动作 | 通过标准 | 当前状态 |
|---|---|---|---|---|
| 1. 版本冻结 | 同步 `Makefile`、`Chart.yaml`、`values.yaml`、`api/openapi.yaml`、`web/package.json`、README | 版本一致性脚本 + `rg` 人工复核 | App `0.1.2`、Chart `0.1.4` 与 OpenAPI/前端一致 | 已一致（`0.1.2/0.1.4`） |
| 2. 依赖整理 | 执行 `go mod tidy`，确认 `go.sum` 只含实际依赖 | `go mod tidy && go mod verify` | 无意外升级、无缺失校验和 | 待执行 |
| 3. 后端门禁 | 格式化、vet、全量 race、Metrics/重试/审计/配置专项测试 | 第 15.1、15.9 节和 22.13 节命令 | 全部通过；无 race | `go test -race ./...` 通过；retry 定向 race 8/8；发布候选仍需复跑 |
| 4. 架构与契约 | `make arch`、`make api-check`、typecheck | 第 15.2 节命令 | domain/app 无禁止依赖；生成类型无漂移 | 本次全部通过 |
| 5. 前端质量 | Vitest、生产构建、bundle、三浏览器 E2E | 第 15.1 节命令 | 单元测试全过；bundle < 500 KiB；E2E 27/27 | typecheck/9 单测/27 项 E2E/build/bundle 通过 |
| 6. 性能与容量 | LCP、1000 Pod/200 工作负载 P95、内存 | `make capacity-test`、`npm run e2e:performance` | LCP < 2.5 s；P95 达标；heap < 256 MiB | 待执行 |
| 7. 审计补齐 | 验证 HTTP 停止后的有界 `Close` 排空和配置负向测试，并做真实 SIGTERM 演练 | `go test -race ./internal/platform/audit ./internal/platform/config`、`kubectl rollout restart` | 关停不丢已入队记录；关闭后不接收新记录；非法配置拒绝启动 | 代码与组件测试完成；滚动升级 SIGTERM 待验收 |
| 8. Metrics 真实态 | 安装 Metrics Server，执行 15.9 三态对账 | `kubectl top` 与页面/DTO 对比 | usage 同量级；缺失时正确降级 | 隔离测试 Release 已通过；目标发布版本待复验 |
| 9. Helm 双路径 | lint、普通/Cilium/审计 Webhook 三路径渲染、kubeconform | 第 15.3、15.9 节命令 | 端口 32626、namespace `dosh`、CNP/网策正确 | 待最终执行 |
| 10. 安全 | govulncheck、npm audit、构建镜像、Trivy | 第 15.2 节命令 | 无 high/critical 未接受项 | 待执行 |
| 11. 镜像发布 | `make image TAG=0.1.2`，push，记录 digest | `podman inspect` + registry digest | tag 与 digest 可追溯 | 历史已执行；digest 证据待回填 |
| 12. 集群升级 | `helm upgrade --install dosh ... -n dosh --wait` | `helm history`、`rollout status`、`get svc` | revision 12、2/2 Ready、NodePort 32626 | 已执行：revision 12、2/2 Ready；NodePort 待复核 |
| 13. 运行验收 | 健康、登录、Metrics 降级、URL 恢复、审计默认关闭 | 第 15.4–15.9 节 | 全部通过，证据写入 21 节 | 历史功能证据存在；OIDC 仅在隔离 Release 验证，生产仍为 `local` |
| 14. CI 实跑 | 触发 GitHub Actions 8 个 job | `gh run view` | 全绿并保留 artifact | 待执行 |
| 15. 回滚演练 | 保留上一 revision，执行回滚再恢复 | `helm rollback`、health/login | 回滚和恢复均成功 | 历史基线已执行；OIDC 候选仍需在隔离 Release 重新演练 |

发布决策：只有第 1–15 步全部完成，且“商用缺口”要么关闭、要么有具名负责人书面接受，
才可以把候选版本标记为“已发布”。否则继续维持“实验室内部可用”。

### 25.6 旧候选 `0.1.3-rc-1793626263` 历史放行核对单（已失效）

本核对单保留为历史记录，用于说明旧候选在审计 CA/Token 改动之前执行到哪一步。由于当前工作树
已晚于该镜像，表中 digest、retry、浏览器和 soak 结果不能用于发布当前源码；不得按第 6 项 digest
直接升级生产。生产继续保留 App `0.1.2` / Chart `0.1.4` / revision 12 / NodePort `32626`。

| 顺序 | 检查项 | 执行命令或判据 | 通过标准 | 当前状态 |
|---|---|---|---|---|
| 1 | 版本与工作区一致性 | 核对 `Chart.yaml`、`web/package.json`、OpenAPI、镜像 tag 和构建 SHA | 版本来源一致；不存在未登记的候选差异 | 待核验 |
| 2 | Go 全量门禁 | `gofmt -l cmd internal tools web/embed.go`、`go vet ./...`、`go test -race ./...` | 第一条无输出；其余全部通过 | 待复跑 |
| 3 | 架构与契约 | `make arch`、`make api-check` | domain/app 无传输/K8s 依赖；TS 类型零漂移 | 待复跑 |
| 4 | 前端门禁 | `cd web && npm run typecheck && npm test -- --run && npm run build && npm run bundle:check` | 类型、9/9 Vitest、构建、500 KiB 预算通过 | 已通过；最终候选待复跑 |
| 5 | Helm 门禁 | `make helm-check` | local/oidc/Cilium 正向与非法配置负向全部通过；32626、2 副本、最小 RBAC | 待复跑 |
| 6 | 镜像身份 | `podman image inspect 192.168.122.10:5000/dosh/dosh:0.1.3-rc-1793626263 --format '{{.Digest}}'` | 等于 `sha256:8d7242ae694a920e38cced915930b8d158c7edd99ab9823dd0a6ce481b2febbe` | 已记录；待最终确认 |
| 7 | 镜像漏洞 | `make image-scan TAG=0.1.3-rc-1793626263` | HIGH/CRITICAL 0；记录 Trivy/DB 版本和时间 | 待执行 |
| 8 | retry 完整矩阵 | `DOSH_E2E_SKIP_BUILD=1 DOSH_E2E_IMAGE_TAG=0.1.3-rc-1793626263 DOSH_FAULT_PROXY_IMAGE_TAG=kube-fault-proxy-1793624877 bash scripts/e2e-kube-retry.sh` | 429/500/502/503/504 瞬时恢复；持续故障可定位；写请求仅 1 次、副本不变；重置/取消/恢复通过 | 读路径已通过；完整矩阵待复跑 |
| 9 | OIDC 浏览器矩阵 | `DOSH_E2E_SKIP_BUILD=1 DOSH_E2E_IMAGE_TAG=0.1.3-rc-1793626263 bash scripts/e2e-oidc-browser.sh` | viewer/operator/admin 和创建向导共 12/12；Token 扫描 0 命中；生产端口未变 | 前一候选通过；最终候选待复跑 |
| 10 | 30 分钟 soak | `DOSH_E2E_SKIP_BUILD=1 DOSH_E2E_IMAGE_TAG=0.1.3-rc-1793626263 bash scripts/e2e-soak.sh` | `app_errors=0`、`max_pod_memory_mi<=256`、`event_frames>0`、`log_lines>0`、`reconnects>=1`、`stream_failures=0` | 待执行 |
| 11 | 审计与 SIGTERM | 在测试 Release 滚动重启，检查审计端与 stdout | 已入队事件不丢；Request ID 可关联；无 token/secret | 待执行 |
| 12 | 证据归档 | 保存命令输出、fixture、Helm revision、镜像 digest、截图/日志和测试开始/结束时间 | 每项证据与应用 digest、Release、namespace、NodePort 绑定 | 历史候选，未纳入 v2.8 当前候选正式归档 |
| 13 | 生产发布 | 先备份 values/Secret/revision，再用 digest 升级 `dosh` | 2/2 Ready、health/ready 200、登录与只读主路径通过 | 未执行 |
| 14 | 生产回滚 | 保留上一 revision，执行 rollback 后再恢复 | 回滚和恢复均通过 health/ready/login；不影响其他 namespace | 未执行 |

**放行规则**：第 1–12 项任一失败，候选不得进入生产；第 13–14 项任一失败，立即回滚并保留日志。
临时测试目录（如 `/tmp/tmp.k6hcJ3CHcu`、`/tmp/tmp.4SwyYilzsC`）不是长期证据库，发布前必须把
必要内容复制到受控归档位置并记录校验和。

### 25.7 最新源码重新放行核对单

审计 Webhook 私 CA/Token-only 改动后，必须用新的不可变候选重新开始，不能复用 25.6 的镜像身份或
验收结论。当前 v2.31 最终候选冻结值为：

| 字段 | 值 |
|---|---|
| `<candidate-tag>` | `0.1.3-rc-sec-20261104035000` |
| `<candidate-digest>` | `sha256:69f30f0016516ac9b56ff867f23c8c0039810dcff29179420c12da323cc00523` |
| `<chart-version>` | `0.1.5` |
| `<source-snapshot>` | 2026-11-04 工作树，App/OpenAPI/Web `0.1.3`；源码绑定 `source-binding.txt` |

| 顺序 | 阶段 | 做哪些事 | 验收命令或判据 | 放行标准 |
|---|---|---|---|---|
| 1 | 版本冻结 | 同步 Makefile、Chart、values、OpenAPI、web package、README；记录源码快照 | 版本一致性脚本与人工核对 | 所有版本来源一致，无未登记差异 |
| 2 | 后端门禁 | 格式、vet、全量 race、audit/config/auditreceiver 专项 | `gofmt -l ...`、`go vet ./...`、`go test -race ./...` | 格式无输出；测试全绿 |
| 3 | 架构与契约 | domain/app 依赖护栏、OpenAPI 生成与漂移检查 | `make arch`、`make api-check` | 无传输/K8s 依赖进入 app/domain；TS 类型零漂移 |
| 4 | 前端门禁 | 类型、Vitest、构建、bundle budget | `npm run typecheck && npm test -- --run && npm run build && npm run bundle:check` | 全部通过，预算不退化 |
| 5 | Helm 与出口 | local/oidc/audit/Cilium 正向；缺凭据/缺 CIDR/非法配置负向 | `make helm-check` | 32626、2 副本、Secret/CA 引用正确；CNP 审计 CIDR 精确且无 `0.0.0.0/0` |
| 6 | 镜像身份 | 构建并推送新 tag；记录 registry digest；确认不可变 | `podman push`、`podman image inspect <candidate-tag>` | 发布清单记录 `<candidate-digest>`，tag 与 digest 对应 |
| 7 | 镜像安全 | Trivy HIGH/CRITICAL，记录 DB 版本和时间 | `make image-scan TAG=<candidate-tag>` | 无未接受 HIGH/CRITICAL |
| 8 | 审计集成 | 按 22.14 完成私 CA、Token/HMAC、业务事件、故障/恢复、SIGTERM 排空 | `DOSH_E2E_SKIP_BUILD=1 DOSH_E2E_IMAGE_TAG=<candidate-tag> DOSH_E2E_KEEP_ARTIFACTS=1 bash scripts/e2e-audit-webhook.sh` | WBS-8.11 达到“实验室已验收” |
| 9 | OIDC 回归 | 隔离 Release 三身份、创建向导、logout 和七类负向登录 | `scripts/e2e-oidc-browser.sh --project=chromium`、`--project=firefox`；WebKit 在隔离 libvirt runner 执行 | Chromium/Firefox/WebKit 各 15/15；Token 扫描 0 命中；每浏览器 6/6 denied 审计带 requestId |
| 10 | retry 回归 | 读/写故障矩阵、连接重置、取消、恢复 | `scripts/e2e-kube-retry.sh` | 读请求≤3 次；写请求仅 1 次；无重试风暴 |
| 11 | soak | 30 分钟日志/SSE、重连、Pod 删除、滚动重启 | `scripts/e2e-soak.sh` | `app_errors=0`、内存≤256 MiB、事件/日志帧>0、重连≥1、流失败=0 |
| 12 | 回滚 | 在隔离 Release upgrade→rollback→restore，复验 health/login | Helm history 与脚本 | 三个 revision 均可用，生产未受影响 |
| 13 | 证据归档 | 按 22.14.11 归档 digest、revision、原始日志和扫描结果；`summary.txt` 纳入 manifest，manifest 摘要单独锚定 | `make evidence-check`、`sha256sum -c manifest.sha256`、`sha256sum -c manifest-digest.txt` | 每项结论都能定位到不可变证据；本机归档通过双层校验；离机 WORM 另列待办 |
| 14 | 生产发布 | 备份 values/Secret/revision；用 digest 发布到 `dosh` | `helm upgrade --wait`、health/login | 2/2 Ready，NodePort 32626，核心路径通过 |
| 15 | 生产回滚 | 保留上一 revision；执行 rollback 后再恢复 | `helm rollback`、health/login | 回滚和恢复均成功，未影响其他 namespace |

**门禁规则**：第 1–13 项任一失败，候选不得进入生产；第 14–15 项失败立即回滚并保留日志。
任何临时 `/tmp` 证据都必须复制到受控归档位置并记录 SHA-256；本机归档与离机 WORM 分别验收。
没有与 digest 绑定、未通过 manifest 校验或无法追溯到 candidate digest 的证据一律无效。

### 25.8 前一完整门禁候选逐项执行台账（历史）

本表把 25.7 的通用核对单落到候选 `0.1.3-rc-20261102153511@sha256:47983a…312e`。它不代表当前
候选；当前候选的复验步骤见 25.10。状态严格区分“本地通过”“等待隔离集群”“未执行”，只有
最后一列达到放行标准，才允许进入生产。

| # | 步骤 | 设计/操作 | 验收命令或判据 | 历史状态 | 遗留动作 |
|---|---|---|---|---|---|
| 1 | 版本冻结 | App/OpenAPI/npm `0.1.3`；Chart `0.1.5`；README 与 values 同步 | `make version-check` | 通过 | 无；后续任何版本变更必须重新冻结并重建镜像 |
| 2 | 后端门禁 | gofmt、vet、全量 race、配置/审计负向 | `make verify`、`make race` | 通过 | CI 真跑待完成 |
| 3 | 架构与契约 | domain/app/platform 依赖方向、路由归属、OpenAPI 生成 | `make arch`、`make api-check` | 通过 | 新增路由时必须同步契约和路由测试 |
| 4 | 前端门禁 | typecheck、Vitest、mock E2E、构建、bundle budget | `npm run typecheck && npm test -- --run && npm run e2e:container && npm run build && npm run bundle:check` | 通过；10/10、30/30、339.78 KiB | 真实 Release 三浏览器 3/3 已通过；Edge/Safari 实机待补 |
| 5 | Helm 与出口 | local/oidc/audit/Cilium 正向与非法 digest、缺凭据、缺 CIDR 负向 | `make helm-check` | 通过；`image.digest` 严格要求 `sha256:<64 hex>` | 使用 `image.digest` 渲染并部署隔离 Release |
| 6 | 镜像身份 | 推送 tag，读取 registry digest，禁止使用本地 tag 发布 | `curl -ksSI .../manifests/<tag>` + `Docker-Content-Digest` | 通过；`sha256:47983a…312e` | 部署时固定该 digest，不写可变 tag |
| 7 | 镜像安全 | Debian 基础层和 Go binary HIGH/CRITICAL | `make image-scan TAG=<tag>` | 通过；0 HIGH/CRITICAL，扫描时间 `2026-11-02T15:35:26Z` | 发布前按同一镜像 digest 复查数据库时间 |
| 8 | 动态集群上下文 | 隔离 Release 设置 `runtime.clusterName=dosh-ui-dynamic-e2e`，浏览器检查顶部栏、侧栏、关于弹窗和总览面包屑 | 真实浏览器 + `/api/v1/overview` | 通过；`dosh-ui-e2e` revision 2、2/2、三浏览器 3/3 | 企业 IdP/Edge/Safari 另行验收 |
| 9 | OIDC 回归 | viewer/operator/admin、Secret 无权限、创建向导、logout | `scripts/e2e-oidc-browser.sh` | 通过；API 矩阵和浏览器 12/12；Token 扫描 0 命中 | 企业 IdP 待补 |
| 10 | retry 回归 | 429/500/502/503/504 读、写请求仅 1 次、重置、取消、恢复 | `scripts/e2e-kube-retry.sh` | 通过；全部绑定该历史 digest | 当前候选需按 25.9 复跑；生产故障注入复验 |
| 11 | soak | 30 分钟日志/SSE、重连、Pod 删除、滚动重启 | `scripts/e2e-soak.sh` | 通过；29 事件、50,400 事件帧、28,449 日志行、5 次重连、0 错误、0 流失败、峰值 28 MiB | 证据已保留；生产同级复跑待发布前执行 |
| 12 | 审计 Webhook | 私 CA、Bearer/HMAC、业务事件、503/超时/恢复、队列满、SIGTERM | `scripts/e2e-audit-webhook.sh` | 通过；revision 10，20 条 SIGTERM 记录全部到达，泄露扫描 0 命中 | 真实 SIEM 仍需独立验收 |
| 13 | Metrics 三态 | Available → 禁用 → 恢复；真实 usage 与 `kubectl top` 对账 | `scripts/e2e-metrics.sh` | 通过；该历史 digest，usage 真实且降级无陈旧值 | 当前候选需按 25.9 复跑；生产 Metrics 拓扑复验 |
| 14 | 回滚 | 独立 Release `upgrade → rollback → restore`，每次复验 health/login/权限 | Helm history 与 OIDC rollback 脚本 | 通过；revision 59→60→61→62，每阶段矩阵无回归 | 生产发布前按企业 IdP 复跑 |
| 15 | 证据归档 | 汇总命令输出、digest、revision、requestId、测试资源清单和 SHA-256；归档工具纳入 `make evidence-check` | 21.2.11 的 manifest、manifest-digest 和敏感扫描 | 本机受控归档完成：小证据集 6 文件、完整证据集 510 文件通过双层校验；离机 WORM 未完成 | 将归档复制到企业批准的 WORM/长期存储，并记录访问审批与保留期限 |
| 16 | 生产发布 | 备份 values/Secret/revision，以 digest 升级 `dosh` | `helm upgrade --wait`、`/healthz`、`/readyz`、登录 | 未执行；生产 `0.1.2/0.1.4` 保持 2/2 | 仅在第 1–15 项全部通过后执行 |
| 17 | 生产回滚 | 保留上一 revision；rollback 后再恢复 | `helm rollback`、健康检查、登录 | 未执行 | 回滚和恢复均需绑定生产 revision 与 digest |

**历史结论**：候选 `sha256:47983a…312e` 已通过第 1–15 项的本机门禁，包括完整证据归档和双层 SHA-256 校验；离机
WORM/长期保留仍未完成，企业 IdP、受信 TLS、真实 SIEM、Edge/Safari 和 CI 真跑也未验收。
第 16–17 项生产发布/回滚不得执行，直至全部 P0 门禁和外部依赖门禁关闭。

### 25.9 已部署 OIDC 候选复验与放行清单（历史）

本表只针对该历史候选
`0.1.3-rc-oidc-20261102171324@sha256:0be6df2586a3e54a34c4b8221622a3ccaefaa7224a9e5ed3c718cf5cf0798ca8`。
它已通过的 OIDC 负向矩阵、本地门禁、retry、Metrics、审计和回滚不得替代仍未完成的 soak、
真实 Release 主题/axe/键盘以及外部商用门禁。所有命令必须使用隔离 namespace、Release 和
NodePort，禁止指向生产 `dosh:32626`。

```bash
export DOSH_E2E_SKIP_BUILD=1
export DOSH_E2E_KEEP_ARTIFACTS=1
export DOSH_E2E_IMAGE_TAG=0.1.3-rc-oidc-20261102171324
export DOSH_E2E_IMAGE_DIGEST=sha256:0be6df2586a3e54a34c4b8221622a3ccaefaa7224a9e5ed3c718cf5cf0798ca8
```

| # | 步骤 | 设计与操作 | 验收命令或判据 | 当前状态 | 通过标准 |
|---|---|---|---|---|---|
| 1 | 固定候选身份 | 读取 registry digest；Helm 使用 `image.digest`，不用可变 tag；记录 revision、namespace、NodePort、开始/结束时间 | `curl -ksSI .../manifests/0.1.3-rc-oidc-20261102171324`；`helm history`；`kubectl get pod -o jsonpath` | 已固定 `sha256:0be6df25…798ca8` | tag 与 digest 一致；所有 E2E 输出绑定该 digest |
| 2 | retry 回归 | 在隔离 Release 注入 429/500/502/503/504、持续 429、连接重置、客户端取消；写请求不得重放 | `bash scripts/e2e-kube-retry.sh` | 当前 digest 通过；证据 `20261102T173942Z-retry-gate-r2/retry-evidence.log` | 读请求最多 3 次并尊重 `Retry-After`；写请求仅 1 次；恢复后 200；证据含 requestId |
| 3 | Metrics 三态 | Available → 禁用 → 恢复；页面/DTO 与 `kubectl top` 对账，降级时不得显示陈旧值 | `bash scripts/e2e-metrics.sh` | 当前 digest 通过；证据 `20261102T174033Z-metrics-gate/metrics-evidence.log` | 三态符合预期；usage 同量级；恢复后无陈旧值 |
| 4 | 30 分钟 soak | 30 条 SSE、30 条日志流、Pod 删除、滚动重启、重连和内存采样 | `bash scripts/e2e-soak.sh` | 待执行 | 0 应用错误、0 流失败、重连 ≥1、RSS 不持续线性增长且 ≤256 MiB |
| 5 | 审计 Webhook/SIGTERM | 私 CA、Bearer/HMAC、业务成功/拒绝、503/超时/恢复、队列压力、SIGTERM 排空和泄露扫描 | `bash scripts/e2e-audit-webhook.sh` | 当前 digest 通过；证据 `20261102T174204Z-audit-gate/audit-webhook-evidence.log` | 已入队记录不丢；事件带 requestId/digest/revision；无 Token/Secret 泄露 |
| 6 | 回滚与恢复 | 隔离 Release 执行 upgrade → rollback → restore；每个 revision 复验 health/ready、登录、三身份和 logout | `bash scripts/e2e-oidc-rollback.sh` | 当前 digest 通过 revision `72→73→74→75`；证据 `20261102T174350Z-rollback-gate/oidc-rollback-evidence.log` | 三个 revision 均可用；最终恢复到当前 digest；生产和其他 namespace 不变 |
| 7 | 主题与可访问性 | 在当前 Release 复跑暗色主题切换、刷新持久化、键盘主路径和 axe | 三浏览器主题/键盘/axe 套件 | 待执行 | 三浏览器全绿；axe 0 serious/critical；无首屏闪烁和亮色残留 |
| 8 | 隔离部署复核 | 复验 1/1 或目标副本 Ready、PDB、digest、NodePort、SPA/API 同端口和生产快照不变 | `helm status`、`kubectl rollout status`、`curl /healthz /readyz`、`/api/v1/overview` | 已部署最终 revision 75，1/1 Ready | 生产 `dosh` revision 12、2/2、`32626` 未变化 |
| 9 | 证据归档 | 把第 2–8 项输出按当前 digest 归档；manifest 覆盖 `summary.txt`，manifest 摘要外锚 | `make evidence-check`；`sha256sum -c manifest.sha256`；`sha256sum -c manifest-digest.txt` | OIDC 矩阵已归档；retry/Metrics/审计/回滚仅原始日志已生成 | 所有结论可追溯到当前 digest；离机 WORM 另行记录 |
| 10 | 外部商用门禁 | 企业 IdP、受信 CA、真实 SIEM、Edge/Safari、GitHub Actions 真跑与分支保护、离机 WORM | 对应第 26 节验收单 | 未执行 | 全部关闭或有具名负责人、书面风险接受、到期日和回滚措施 |
| 11 | 生产发布与回滚 | 仅在 1–10 全部通过后，备份 values/Secret/revision，以 digest 发布 `dosh`，烟测后回滚再恢复 | `helm upgrade --wait`、`kubectl rollout status`、健康/登录/核心 API、`helm rollback` | 未执行 | 2/2 Ready、NodePort `32626`、核心路径通过；回滚/恢复有完整证据 |

**放行规则**：该历史候选只有完成第 1–9 项并关闭第 10 项全部商用门禁，才允许执行第 11 项。
第 21.2.12 节的 33/33 只能证明 AUTH-O-005 在签名测试 IdP 上关闭，不能证明整个候选可发布。

### 25.10 v2.31 历史候选复验与放行清单（历史）

本节只保留 v2.31 历史清单，不代表当前工作树。当前候选结果以 21.2.37 和 21.2.38 为准；v2.44 已重新执行
OIDC Chromium/Firefox/WebKit、rollback/restore、Metrics、retry、Loki、soak、工作负载生命周期和
CronJob 矩阵。该历史候选为
`0.1.3-rc-sec-20261104035000@sha256:69f30f0016516ac9b56ff867f23c8c0039810dcff29179420c12da323cc00523`，
已绑定隔离 Release `dosh-e2e-oidc/dosh-oidc-e2e` revision 138、NodePort `32627`。历史
`03ac9628…00000c` 的浏览器、soak、rollback 和归档证据不再用于当前源码。所有集群操作仍必须使用
隔离 namespace/Release/NodePort；禁止操作生产 `dosh:32626`，除非有单独书面发布授权。

> v2.43 历史候选增量：`make e2e-workload-lifecycle` 已在独立 NodePort `32630` 夹具完成
> 预览→创建→扩容→镜像更新→重启→删除闭环，viewer/非法输入/旧 `resourceVersion` 分别验证
> 403/400/409，13/13 生命周期审计均带 `requestId`；生产 revision 12、NodePort `32626` 和
> 2/2 Ready 指纹前后不变。详细结果见 21.2.36。本表 v2.31 历史行仍只作追溯，不继承给后续候选；
> v2.44 同矩阵结果见 21.2.38。

> v2.32 说明：第 1–8 项已完成并绑定最终 digest；WebKit 已由隔离 libvirt runner 完成 mock 与
> 真实 OIDC 各 15/15；第 9 项已生成原始、文档和 WebKit 三组受控归档。外部商用门禁和生产发布仍未关闭。

| # | 步骤 | 当前状态 | 通过标准 |
|---|---|---|---|
| 1 | 当前源码本地门禁 | 通过：`make verify`、`make race`、容量、Vitest 16/16、bundle `340.72 KiB`、mock Chromium/Firefox 15/15、LCP 468 ms、`make evidence-check`、shell 语法 | 全部绿灯；证据见 21.2.24 |
| 2 | 构建、推送与 digest 绑定 | 通过：tag `0.1.3-rc-sec-20261104035000`，registry digest `sha256:69f30f00…00523`；镜像二进制与工作区 `bin/dosh` 一致 | tag 仅作标签；Helm 使用 digest；image ID 不作为发布身份 |
| 3 | 安全扫描绑定 | 通过：Trivy Debian/Go binary 0、govulncheck 0、公共 registry 生产 npm 审计 0 | 无未接受 HIGH/CRITICAL；DB 时间和原始输出已留存 |
| 4 | 隔离部署 | 通过：`dosh-e2e-oidc/dosh-oidc-e2e` revision 138、1/1 Ready、NodePort `32627`，digest 固定；137→138 values 无差异 | 生产 revision 12、2/2、`32626` 未变化 |
| 5 | 功能与授权矩阵 | 通过：三身份 API/RBAC、YAML 单文档/重复键、无效 kind、Secret reveal、400/409、跨域写 403、扩容期望态写入 | 成功/拒绝语义正确；无权限请求不产生业务写 |
| 6 | Chromium/Firefox OIDC | 通过：真实 OIDC 各 15/15；每浏览器 6/6 denied 审计；泄露扫描 0 | 证据绑定 `69f30f00…00523`；受限容器单 worker |
| 7 | WebKit | 通过：隔离 libvirt runner mock 15/15、真实 OIDC 15/15，均 `workers=1`、`retries=0`；6/6 denied 审计、Token 扫描 0 | 仅隔离 runner/CI；物理宿主不得复跑；证据归档 `v231-webkit-final` |
| 8 | soak 与 rollback/restore | 通过：30 分钟 soak `app_errors=0`、`stream_failures=0`、峰值 34 MiB；rollback/restore `128→129→130→131` 每阶段通过 | 证据绑定最终 digest；审计 revision 10、CronJob revision 127、Metrics/retry 通过 |
| 9 | 证据归档 | 通过：原始归档 `/var/lib/dosh/evidence/69f30f001651/20261103T205205Z-v231-final`，548 文件，manifest `21d93f2d…de5b`；文档修订与 WebKit 结果分别由 `v231-final-docs`、`v231-final-docs-anchor`、`v231-webkit-final` 补充归档 | 原始与补充归档双层 SHA-256 均通过；WebKit 敏感扫描 0；离机 WORM 仍待完成 |
| 10 | 外部商用门禁 | 未执行 | 企业 IdP、受信 CA、真实 SIEM、Edge/Safari、GitHub Actions 真跑、离机 WORM 全部关闭 |
| 11 | 生产发布与回滚 | 禁止启动 | 前 10 项通过并经授权后执行；2/2 Ready、NodePort `32626`、核心路径和回滚/恢复有证据 |

**v2.29 历史状态（保留追溯，不得继承给 v2.30）**

| # | 步骤 | 设计与操作 | 验收命令或判据 | 当前状态 | 通过标准 |
|---|---|---|---|---|---|
| 1 | 候选冻结与本地门禁 | 确认 App `0.1.3` / Chart `0.1.5` / OpenAPI `0.1.3` 一致；重跑格式、vet、全量测试、race、容量、架构、API、Helm、Playwright 资源隔离、E2E digest 绑定和前端门禁 | `make verify`、`make race`、`make capacity-test`、`make web-test`、`make playwright-check`、`make e2e-image-check`、`make web-bundle-check`、`make web-performance` | 通过：Pods/Workloads/Overview P95 `21.016489/189.099579/197.942489ms`、Heap `28 MiB`、Vitest 15/15、bundle `340.73 KiB`；`make verify`、race、容量、架构、API、Helm、Playwright 和 E2E digest 门禁通过 | 全部绿灯；无新增跨模块依赖、路由漂移或版本漂移 |
| 2 | 发布身份确认 | 只把 tag 当作构建标签；推送后读取 registry `Docker-Content-Digest`；Helm 使用 `image.digest=sha256:<64 hex>` | `podman push`；读取 registry manifest digest | 通过：registry digest `sha256:03ac9628…00000c`；image ID `623841f4…a3d97c`；本地 manifest `sha256:9e2182aa…8c6a0c` 仅作本地标识 | registry digest 与 tag 一致并写入证据；禁止使用 image ID 或本地 manifest digest 冒充发布身份 |
| 3 | 安全扫描绑定 | 对同一不可变镜像执行 Trivy；重跑 govulncheck 和公共 registry 生产 npm 审计；记录 Debian 和 Go binary 结果 | `make image-scan TAG=<tag>`、`govulncheck ./...`、`npm audit --omit=dev --registry=https://registry.npmjs.org` | 通过：Trivy Debian 12.15/Go binary 0；govulncheck 0；生产 npm `found 0 vulnerabilities` | 镜像和可达依赖均 0；原始输出可追溯 |
| 4 | 隔离部署 | 使用独立 namespace、Release、NodePort 和 `image.digest` 部署；设置非生产 `DOSH_CLUSTER_NAME`；不改生产 values/Secret | `helm list -A`；`kubectl -n dosh-e2e-oidc get deploy,svc,pod -o wide`；`kubectl -n dosh-e2e-oidc logs <pod>` | 通过：revision 114、NodePort `32627`、Pod `1/1` Ready；生产 revision 12、2/2、`32626` 未变化 | 目标副本 Ready；PDB 有效；NodePort 不冲突；生产 `dosh` revision/镜像/32626 不变 |
| 5 | 功能与授权矩阵 | 验证 viewer/operator/admin 的列表、详情、日志/事件、Deployment 向导、scale/restart/delete、Secret reveal、YAML 预览和 403/409；同时验证跨域写请求 403；kubectl 对账资源 | 第 15.7、15.10 节脚本；`scripts/e2e-oidc.sh`；`same-origin-runtime.log`；`kubectl get/scale/delete` 对账 | 通过：viewer read 200/scale 403/revealSecrets false；operator target scale 200/other namespace 403；admin scale/nodes 200；Deployment preview/create、400、409、RBAC、跨域 Origin 403 和 cross-site Fetch Metadata 403 通过 | 成功/拒绝语义正确；无权限请求不产生业务写；审计含 requestId 和 result |
| 6 | 工作负载增量验收 | 对当前 digest 验证 CronJob 无 selector 详情不 panic；无效 kind、不支持 scale/restart、非法 propagation 产生失败审计且资源不变 | `make e2e-cronjob-detail`；API 请求 + `kubectl get`；审计接收端查询；`go test ./internal/modules/workloads/app` | 通过：CronJob 返回 200、`selector={}`、`pods=[]`、`status=Suspended`，删除后 NotFound；失败审计矩阵和 Repository 零写单测通过 | 五种工作负载详情可用；失败写操作 Repository 调用为 0；失败审计可检索 |
| 7 | 浏览器与 UX 套件 | 在隔离 Release 执行 Chromium/Firefox 真实 OIDC；WebKit 由隔离 libvirt runner 执行；包含主题、axe、键盘、开放重定向和七类负向登录；必须走受限 runner | `bash scripts/e2e-oidc-browser.sh --project=chromium`、`--project=firefox`；独立 runner 执行 WebKit | Chromium/Firefox/WebKit 真实 OIDC 各 15/15；每浏览器 6/6 denied 审计和 Token 扫描通过；WebKit mock 也 15/15 | Chromium/Firefox/WebKit 全绿；axe 0 serious/critical；Token/Secret 扫描 0；证据绑定 registry digest |
| 8 | 30 分钟 soak | 30 条事件流、30 条日志流、Pod 删除、滚动重启、断线重连和内存采样；同时确认审计队列不阻塞业务 | `DOSH_E2E_IMAGE_TAG=<tag> DOSH_E2E_IMAGE_DIGEST=<registry-digest> DOSH_E2E_SKIP_BUILD=1 DOSH_E2E_KEEP_ARTIFACTS=1 bash scripts/e2e-soak.sh` | 通过：29 事件、64,260 事件帧、28,394 行日志、5 次重连、0 错误、0 流失败、峰值 30 MiB | `app_errors=0`、流失败 0、重连 ≥1、RSS 不持续增长且 ≤256 MiB；证据与当前 digest 绑定 |
| 9 | 回滚与恢复 | 隔离 Release 执行 upgrade → rollback → restore；每个 revision 复验 health/ready、登录、三身份读和 logout | `DOSH_E2E_IMAGE_TAG=<tag> DOSH_E2E_IMAGE_DIGEST=<registry-digest> bash scripts/e2e-oidc-rollback.sh` | 通过：revision `109 → 110 → 111 → 112`；四阶段 health/ready、三身份 RBAC、logout 均通过 | 三个 revision 均可用；最终恢复到当前 digest；生产和其他 namespace 不受影响 |
| 10 | 证据归档 | 归档扫描、Helm revision、API/kubectl 对账、审计、Chromium/Firefox、soak、回滚和可重复 CronJob 输出；`summary.txt` 纳入 manifest，manifest 摘要外锚；扫描并排除 token/cookie/secret | `make evidence-check`；`sha256sum -c manifest.sha256`；`sha256sum -c manifest-digest.txt` | 通过：`/var/lib/dosh/evidence/03ac9628fe8b/` 下的完整当前候选归档，双层 SHA-256 与敏感扫描通过 | 每个结论可定位到 evidence 路径和 registry digest；离机 WORM 另行记录 |
| 11 | 外部商用门禁 | 企业 IdP、受信公共 CA TLS、真实 SIEM/Loki/OpenSearch、Edge/Safari 实机、GitHub Actions 真跑和分支保护、离机 WORM | 第 26.1、26.2、26.5–26.7 节 | 未执行 | 全部关闭；若接受风险，必须有具名负责人、书面接受、到期日和回滚措施 |
| 12 | 生产发布与回滚 | 仅在前 11 项通过后，备份 values/Secret/revision，以 registry digest 发布 `dosh`，烟测后回滚再恢复 | `helm upgrade --wait`、`kubectl rollout status`、健康/登录/核心 API、`helm rollback` | 未执行 | 2/2 Ready、NodePort `32626`、核心路径通过；回滚/恢复有完整证据 |

**放行规则**：v2.31 第 1–8 项已完成，第 9 项完成后才形成完整候选证据包；第 10 项外部合规门禁
尚未执行，第 11 项生产发布/回滚禁止启动。上表 v2.29 行号仅作历史追溯，不得用于当前源码放行。
任何一项失败都必须停止流转、保留现场并回到源码或环境修复，不能通过重跑旧 digest、重启 Pod
或选择性重试掩盖。

## 26. 商用差距关闭计划

本章不是已完成的验收记录，而是进入生产试点前的关闭路径。每一项都必须有独立变更单、
责任人、验证环境、命令输出和回滚方案。

### 26.1 OIDC + Kubernetes impersonation 与 RBAC

**目标**：从“单本地管理员 + 高权限 ServiceAccount”升级为可审计的多用户身份体系。

**设计**

- `auth/app` 管理 Authorization Code + PKCE 状态机；校验 `state`、`nonce` 和会话 TTL，`platform/oidc`
  负责 discovery、JWKS、签名、`iss`、`aud`、`exp` 和时钟偏差。
- 会话只保存经过校验的身份摘要和组；OIDC ID Token 不写入日志、URL 或前端 localStorage。
- 每次 Kubernetes 请求使用用户身份执行 impersonation：`Impersonate-User`、`Impersonate-Group`。
  ServiceAccount 只保留执行 impersonation 所需的窄权限，不保留业务资源的 cluster-admin。
- 在 app 层用携带 `spec.user/groups` 的 SubjectAccessReview 判断读/写/删除能力，并与
  `/capabilities`、按钮隐藏保持一致；SAR 请求必须移除 impersonation 头。
- 审计事件记录最终用户名、组、动作、资源、结果和 requestId，不记录 access token。

**当前实现状态**

| 能力 | 状态 | 证据 | 尚缺 |
|---|---|---|---|
| 领域 Actor/Access/Authorizer | 已实现 | `make arch`、授权单测、`WithoutActor` | 无 |
| app 层资源授权与审计 | 已实现 | workloads/config/yaml 单测；v2.45 当前候选隔离集群三身份 API/RBAC 与 mock Chromium/Firefox/WebKit 各 38/38、真实 OIDC 各 16/16 | 企业 IdP 与生产 RBAC 复核 |
| OIDC app state/nonce/PKCE/session | 已实现 | `auth/app` race 测试；logout 绝对回跳测试；签名 IdP 隔离集群登录 | 企业 IdP 兼容性 |
| OIDC provider adapter | 已实现 | 真实 RSA、JWKS/错误 audience/expiry/轮换测试 | 企业 IdP 兼容性 |
| HTTP config/login/callback/logout | 已实现 | 单元 HTTP 测试 + 本地签名 IdP 全链路；v2.45 当前候选三浏览器真实 OIDC RP logout，host 两浏览器 12/12 与隔离 WebKit 6/6 denied 审计 | 企业 IdP logout 兼容性 |
| 动态 capabilities | 已实现 | session service 测试 + 三身份集群对账 + 浏览器按钮断言 | 企业 IdP 组映射 |
| Kubernetes impersonation | 已实现 | transport 测试 + 隔离集群审计中的真实用户/组 | 生产 RBAC/IdP |
| SAR | 已实现 | SAR user/groups、无 impersonation、fail-closed 测试 + 隔离集群 RBAC 矩阵 | 企业 IdP groups claim 差异 |
| Helm OIDC/最小 RBAC | 已实现 | `make helm-check` 四模式正向 + 三类负向断言 + 隔离集群部署 | API Server dry-run、生产发布 |
| 前端 OIDC 登录/退出 | 已实现 | 登录页 + AppShell logout + v2.45 Vitest 62/62 + mock Chromium/Firefox/WebKit 各 38/38、真实 OIDC 各 16/16 | 企业 IdP、Edge/Safari 实机复验 |

**剩余实施步骤**

1. 已完成：`WithoutActor`、SubjectAccessReview、真实 RSA provider 测试、AppShell OIDC logout、
   Helm 双模式/最小 RBAC/IdP 出口策略，以及本地签名 IdP HTTP 全链路。
2. 已完成测试集群的 viewer/operator/admin RBAC fixture、直接 API 矩阵、最小 SA 权限核对；v2.45 当前候选完成 mock Chromium/Firefox/WebKit 各 38/38、真实 OIDC 各 16/16，WebKit 由隔离 libvirt runner 执行。
3. 接入真实企业 IdP，验证 issuer/audience、JWKS 轮换、私有 CA、IdP 短暂不可达和 RP logout。
4. 已完成隔离 namespace 的登录、退出、审计、拒绝、真实 RBAC、滚动升级和 rollback/restore；仍待企业 IdP 与 SAR 依赖故障注入。
5. 将第 22.12 节缺陷关闭表和 15.10 节证据写入 21 节；全部通过后才可把 OIDC 标为“已验收”。

**已有自动化验收**

```bash
go test -race ./internal/modules/auth/... ./internal/platform/oidc ./internal/platform/authz ./internal/platform/kube/... ./internal/bootstrap
go test ./internal/platform/config -count=1 -v
make arch
make api-check
make helm-check
kubectl auth can-i --list --as=<user> --as-group=<group>
```

通过标准：未认证跳转 OIDC；回调校验失败拒绝登录；viewer 看不到写按钮且直接调用 API 仍 403；
namespace-operator 只能操作授权命名空间；admin 可执行其 RBAC 允许的操作；审计能区分三个用户；
ServiceAccount 无越权写权限；SAR 不可用时不回退到 SA 权限。

#### 26.1.1 浏览器 OIDC E2E（已执行，实验室已验收）

**目标**：在真实浏览器中证明前端登录入口、按钮权限、Secret 无权限状态、logout 和 API 拒绝
与已通过的 API/RBAC 矩阵一致，不能继续用 mock 或 curl 代替。

**执行结果**

- `scripts/e2e-oidc-browser.sh` 复用 `scripts/e2e-oidc.sh` 保持模式的隔离夹具；浏览器只连接
  `https://192.168.122.100:32627`，不连接生产 `dosh`。
- `web/playwright.oidc.config.ts` 关闭 API mock，`ignoreHTTPSErrors` 仅用于测试自签证书；
  三个 project 均在 `mcr.microsoft.com/playwright:v1.64.1-noble` 容器内执行。
- `web/e2e-oidc/oidc.spec.ts` 覆盖 viewer 只读/Secret 无权限、operator 目标 namespace 扩容、
  admin 节点读取与 RP logout，并断言浏览器 URL 不残留 code/state。
- 实际结果：较早候选为 Chromium/Firefox/WebKit 9/9；加入 Deployment 创建用例后的前一候选为
  **12/12 passed**，证据见 21.2.10。历史 OIDC 候选 `0.1.3-rc-oidc-20261102171324` 在此基础上
  增加七类负向登录用例，三浏览器共 **33/33 passed**，证据见 21.2.12。历史候选
  `0.1.3-rc-a11y2-20261103093530` 的 UX 定向为 9/9、完整矩阵为 42/42，但这些结果不继承给
  当前候选。历史候选 `0.1.3-rc-origin-20261103170401` 已完成 SameOrigin 运行时校验和
  Chromium/Firefox 真实 OIDC 各 15/15；v2.34 当前候选进一步完成 mock Chromium/Firefox/WebKit
  各 16/16、真实 OIDC 各 15/15，
  每浏览器 6/6 denied 审计和 Token 扫描通过。企业 IdP、受信 TLS 和生产发布仍未验收。

**断言矩阵**

| 场景 | 浏览器断言 | API/证据断言 |
|---|---|---|
| viewer 登录 | 进入目标 namespace；无扩缩容/重启/删除按钮；Secret 详情显示“无权限”而非明文 | 直接调用 scale/reveal 仍 403；审计为 denied |
| operator 登录 | 目标 namespace 可打开扩容弹窗并成功；其他 namespace 写按钮不可用 | 目标 scale 200、越界 scale 403；审计含组 |
| admin 登录 | 可读取节点、可执行允许的写操作；用户菜单显示 IdP 身份 | capabilities 全开；审计用户名/组正确 |
| logout | 点击退出后最终 URL 为 `/login`，返回浏览器后退不恢复已认证页面 | 新标签请求 `/api/v1/session` 为 401 |
| 未认证 | 直接访问受保护路由被重定向登录 | 受保护 API 返回 401 |
| 权限一致性 | 页面隐藏的动作不能通过手工 fetch 绕过 | 每个隐藏动作均以 403 结束且无 Kubernetes 写请求 |

**复现命令**

```bash
export PATH=/root/.local/bin:$PATH
DOSH_E2E_KEEP_ARTIFACTS=1 DOSH_E2E_SKIP_BUILD=1 \
DOSH_E2E_IMAGE_TAG=0.1.3-oidc-e2e-1793622219 \
  bash scripts/e2e-oidc-browser.sh
```

通过标准：三种身份场景和七类负向回调全部通过，无 API mock；失败时 trace 可复现；viewer 的隐藏
按钮与直接 API 拒绝一致；logout 后旧会话 401；审计包含三个真实用户名和组，所有 denied 事件
带 `requestId`。该套件在历史 OIDC 候选已通过 33/33；v2.34 当前候选 mock Chromium/Firefox/WebKit
各 16/16、真实 OIDC 各 15/15 通过，每浏览器 6/6 denied 审计带 `requestId`。TST-U-016 当前只对“签名测试 IdP +
三浏览器容器”标记实验室已验收；企业 IdP、Edge/Safari 实机和生产受信 TLS 仍需单独验收。

#### 26.1.2 OIDC 回滚演练（已执行，实验室已验收）

**目标**：证明 OIDC 候选在 Helm upgrade 失败或新 revision 不可用时能恢复到已验证版本，并且恢复后
健康检查、登录、三身份矩阵、审计和 logout 仍成立。

**执行结果**：通过 `scripts/e2e-oidc-rollback.sh` 完成实际链路
`11 (baseline) → 12 (upgrade) → 13 (rollback to 11) → 14 (restore 12)`。每个 revision 都重新
通过 `/healthz`、`/readyz`、viewer/operator/admin 权限、审计和 RP logout；证据见 21.2.5。

**历史 OIDC 候选复验（2026-11-03）**：digest `sha256:0be6df25…798ca8` 在隔离 Release
`dosh-e2e-oidc/dosh-oidc-e2e` 上完成 `72 (baseline) → 73 (upgrade) → 74 (rollback to 72)
→ 75 (restore 73)`；四个阶段均通过 health/ready、viewer read 200/write 403、operator
target 200/other 403、admin scale/nodes 200 和 logout 后 401。证据见
`/var/lib/dosh/evidence/0be6df2586a3/20261102T174350Z-rollback-gate/oidc-rollback-evidence.log`。
该结果绑定历史 OIDC 候选，不得继承给 v2.34 当前候选 `0c61ce34…c5bda`；当前隔离 Release 已推进到
revision 149、1/1 Ready，生产 `dosh` 的 digest 发布与回滚仍未执行。

```bash
DOSH_E2E_KEEP_ARTIFACTS=1 DOSH_E2E_SKIP_BUILD=1 \
DOSH_E2E_IMAGE_TAG=<candidate-tag> \
DOSH_E2E_IMAGE_DIGEST=<candidate-digest> \
  bash scripts/e2e-oidc-rollback.sh
```

1. 保持测试 IdP、CA、Secret、RBAC 和目标 namespace 不变；记录当前 revision、image tag/digest 和
   `/healthz`、`/readyz`、登录结果作为基线。
2. 执行一次无害 upgrade（新增测试 annotation 或等价 values 变更），等待
   `kubectl rollout status`；验证 Pod Ready、health/ready、OIDC 登录和至少一个拒绝路径。
3. 执行 `helm rollback dosh-oidc-e2e <基线revision> -n dosh-e2e-oidc --wait`；再次验证 health/ready、
   三身份矩阵、审计和 RP logout。记录 rollback 产生的新 revision，而不是只记录目标 revision。
4. 如需恢复到升级后的配置，再执行一次显式 `helm upgrade` 或 rollback，并重复验收；禁止停留在
   未验证的中间 revision。
5. 验收后清理测试 Release/namespace/RBAC；若保留现场，记录保留原因和期限。

通过标准：`helm history` 能清晰显示基线、升级和回滚 revision；每次恢复后 `/healthz`、`/readyz`
为 200；OIDC 登录和三个身份权限不回归；旧会话不会因回滚被错误接受；审计和 logout 正常。

#### 26.1.3 Token 泄露扫描（已执行，实验室已验收）

**目标**：关闭 SEC-N-004，证明 ID/access/refresh Token 和 client secret 没有进入不该出现的载体。

**执行结果**：`scripts/e2e-oidc-browser.sh` 在真实浏览器矩阵期间扫描浏览器响应体、当前 URL、DOM、
localStorage、sessionStorage、Cookie，以及 dosh 应用日志、ConfigMap、Deployment 和 Helm values；
JWT 与测试 access token 命中数均为 `0`。`dosh-e2e-auth` Secret 作为设计允许的密钥存储被明确排除，
且未发现其内容进入非 Secret 载体。产物见 `/tmp/tmp.Jnc5Wm3ybi/token-leak-scan.log`，21.2.5 有完整记录。

扫描范围必须明确区分“设计上允许的 Secret 存储”和“泄露”：

1. 浏览器：Playwright 捕获所有响应体、URL、`localStorage`、`sessionStorage`、Cookie 名称/属性；
   不得记录 Token 值到失败截图或报告。检查是否存在 JWT 形式 `eyJ...`、code、state、verifier
   或 IdP token 字段。
2. 服务端：扫描 `kubectl -n dosh-e2e-oidc logs deploy/dosh`、审计 stdout、AccessLog 和 HTTP 错误响应；
   允许出现用户名、组、requestId 和动作，不允许 Token 或 client secret。
3. Kubernetes：检查非 Secret 载体（ConfigMap、Deployment env、Helm values、事件）不含 client secret
   或 Token。`dosh-e2e-auth` Secret 中按设计保存 session key 和 client secret，不能把其自身内容当作
   “泄露”；但必须确认它只通过 `secretKeyRef` 注入，没有进入日志或 API DTO。
4. 测试 IdP 日志只用于调试，应从应用泄露统计中排除；否则测试 IdP 自己打印的调试 token 会造成
   假阳性/假阴性。若 IdP 日志确实包含 token，也必须明确记录为测试夹具风险，不能混入产品结论。
5. 保存扫描命令、命中数量、允许的已知例外和审查人。任一非例外命中即阻断发布，修复后重新执行
   完整矩阵，不能只重跑扫描。

通过标准：指定范围内 Token 命中数为 0；Secret 存储例外有明确说明；浏览器响应/URL/Web Storage、
服务端日志、审计、错误响应和 ConfigMap 均无 Token/client secret。通过后才可把 SEC-N-004 标为“已验收”。

### 26.2 受信 TLS 与证书轮换

**目标**：去除生产浏览器证书告警，证明 SAN、链、有效期和轮换流程可用。

**实施步骤**

1. 由受信 CA 签发 `dosh-tls`，SAN 覆盖所有访问域名和必要 Node IP；私钥仅存在于 Secret。
2. 使用 `openssl verify`、`openssl x509 -text` 核对链、SAN、有效期和用途。
3. 在浏览器和 `curl` 不使用 `-k` 的情况下验证 HTTPS、HSTS 和登录。
4. 演练证书更新：创建新 Secret → 滚动重启 → 验证新证书 → 保留旧 revision 可回滚。

**验收**

```bash
openssl verify -CAfile <ca.pem> /tmp/tls.crt
curl -sS -o /dev/null -w '%{http_code}\n' https://<host>:32626/healthz
echo | openssl s_client -connect <host>:32626 -servername <host> 2>/dev/null | openssl x509 -noout -dates -ext subjectAltName
```

通过标准：无 `-k` 返回 200；SAN 覆盖访问入口；证书有效期满足运维策略；轮换期间至少一个副本可用。

### 26.3 Metrics Server 真实安装与用量对账

**目标**：把 fake client 证据升级为真实 `metrics.k8s.io` 验收。

**实施步骤**

1. 在测试集群安装与本 Kubernetes 版本兼容的 Metrics Server，限制资源并配置 TLS/聚合层参数。
2. 等待 `v1beta1.metrics.k8s.io` Available；执行 `kubectl top nodes`、`kubectl top pods -A`。
3. 登录 dosh，对总览、节点详情、Pod 列表、Pod 详情分别比对 CPU/内存实际用量。
4. 临时停止 Metrics Server，确认 `metricsAvailable=false`、页面降级且不出现白屏或伪造 0%。
5. 恢复 Metrics Server，等待 30 秒探测缓存过期后确认自动恢复。

**验收**：真实数值与 `kubectl top` 同量级；有/无/恢复三态均可截图并存证；恢复后无需重启 dosh。

**执行结果（2026-11-03，当前候选实验室）**

- `scripts/install-metrics-server.sh` 在 `dosh-metrics` 安装 `metrics-server:v0.9.1`，
  Pod 1/1 Running，`v1beta1.metrics.k8s.io` Available。
- `scripts/e2e-metrics.sh` 在当前候选
  `0.1.3-rc-oidc-20261102171324@sha256:0be6df25…798ca8` 的隔离 OIDC 测试 Release 上完成三态：
  `available=true / cpu=2114m / mem=10683842560` → `unavailable=false / usage=0 / hasMetrics=false`
  → `recovered=true / cpu=2159m / mem=10682662912`，恢复过程未重启 dosh。
- 证据见 21.2.13 和
  `/var/lib/dosh/evidence/0be6df2586a3/20261102T174033Z-metrics-gate/metrics-evidence.log`。
- 结论：Metrics 适配、降级和恢复对当前候选在实验室范围已验收；生产 App `0.1.2` 没有包含候选改动，
  目标发布版本仍须在生产或预发等价拓扑重新执行本节并写入发布记录。

**执行结果（2026-11-04，v2.45 当前候选）**

- 在 `dosh-e2e-oidc/dosh-oidc-e2e` revision 193 上复跑同一三态流程，镜像固定为
  `0.1.4-rc-secretdecode-20261104T120606Z@sha256:c7eff34a…3ab06`。
- 可用态 CPU `2152m`、内存 `11501375488` bytes；不可用态 `metricsAvailable=false` 且 usage 归零；
  恢复态 CPU `2141m`、内存 `11518554112` bytes，dosh 未重启。
- 证据见 21.2.40 和 `/var/lib/dosh/evidence/c7eff34a80ca/20261104T131625Z-v245-metrics-r1`，
  manifest SHA-256 `d2bae3531ddd533e712e2a6fc9e8fb558db74462d89c3a02d20fcc7f393348b8`。
- 结论：v2.45 候选实验室三态已关闭；生产/预发等价 Metrics 拓扑仍在发布变更中复验。

### 26.4 长连接 soak 与 Kubernetes API 故障注入

**目标**：证明日志/SSE 在 30 分钟连续运行和 429/5xx 场景下不泄漏、不风暴、可恢复。

**实施步骤**

1. 建立 30 分钟测试：至少 30 个并发日志流和 30 个并发 SSE 连接，周期创建测试事件。
2. 每 5 分钟记录进程 RSS、goroutine 数、连接数、队列长度和错误率；结束后保留曲线。
3. 用代理或 fake API 注入 429、500、502、连接重置和慢响应，验证有限重试、退避、取消和用户提示。
4. 执行断线重连、浏览器刷新、Pod 删除、服务端滚动重启和客户端主动取消。
5. 扫描日志确认没有重复事件无限增长、没有敏感信息、没有 goroutine 泄漏。

**验收**：30 分钟后 RSS 不持续线性增长；错误率可解释；重连不重复渲染；429 不形成重试风暴；
取消后连接及时释放；测试报告记录时间、版本、集群、并发数和原始指标。

**自动化入口（v2.44 历史 digest 的 30 分钟 soak 已通过；v2.45 待复跑）**

`scripts/e2e-soak.sh` 复用隔离 OIDC fixture，固定 `DOSH_SOAK_DURATION=1800` 秒，每 30 秒采样
`kubectl top` 内存、每 60 秒创建事件、每 5 分钟重连事件/日志流，并在 1/2 时长删除 Pod、
2/3 时长滚动重启 dosh。脚本默认建立 `DOSH_SOAK_EVENT_STREAMS=30` 条 SSE 和
`DOSH_SOAK_LOG_STREAMS=30` 条日志流；每次流重建前重新登录，避免测试 IdP 会话过期被误判为产品故障；
单 Pod readiness 采用单次采样，事件 ConfigMap 使用专用 label 清理，滚动重启窗口单独分类。

```bash
DOSH_E2E_KEEP_ARTIFACTS=1 \
DOSH_E2E_SKIP_BUILD=1 \
DOSH_E2E_IMAGE_TAG=<candidate-tag> \
  bash scripts/e2e-soak.sh
```

**故障注入执行顺序**

1. 先执行 22.13 的组件测试，构建包含 retry transport 的不可变候选镜像。
2. 在隔离 Release 前置受控代理，对读请求注入 429/500/502/503/504；对扩容等写请求注入同样的
   响应，断言没有自动重放。
3. 记录每种故障的请求次数、退避时间、最终状态、页面提示、Request ID、QPS 与 P95。
4. 注入连接重置和慢响应，并执行客户端取消；确认 context 取消、goroutine/连接回落。
5. 执行 `kubectl rollout restart` 和 Pod 删除，区分“已声明重连的短暂断流”与真正应用 ERROR。
6. 只有并发强度、内存曲线、重连、取消和期望错误全部满足时，才把 NFR-E-001/NFR-E-002 标记为已验收。

**当前状态**：v2.44 历史候选
`0.1.4-rc-logframes-20261104T102214Z@sha256:e4a27d8b…49439` 已完成 30 分钟 soak，覆盖 30 条事件流、
30 条日志流、5 次重连、Pod 删除和应用滚动重启；30 个事件、50,577 个事件帧、26,577 行日志、
0 应用错误、0 流失败，内存 `22→18 MiB`、峰值 30 MiB。v2.45 当前候选已完成 retry 专项：
429/500/502/503/504 各注入 2 次后恢复、持续 429 恰好 3 次、`Retry-After` 约 2099 ms、写请求仅
1 次、连接重置、客户端取消和恢复，证据见 21.2.41。v2.45 当前候选仍未复跑 30 分钟 soak，因此
NFR-E-001/NFR-E-002 中的长连接部分只能保留 v2.44 历史实验室结论；最终仍需在 v2.45 或生产/预发
同等并发拓扑复验后才能关闭目标环境门禁。

### 26.5 Edge 与真实 Safari 兼容性

**目标**：把 Playwright WebKit 证据补充为真实浏览器抽样，不把容器 WebKit 等同于 Safari。

**当前状态（2026-11-04）**：v2.45 当前候选的 Playwright WebKit 已在隔离 libvirt runner 完成 mock `38/38` 与真实
OIDC `16/16`，但这只证明开源 WebKit 引擎路径；Edge 和 macOS/iOS Safari 实机仍未执行。企业发布前
仍需按下述矩阵在真实设备或受控浏览器云复验，并记录版本、截图和 trace。

**实施步骤**

1. 固定最近两个主版本 Edge、Chrome、Firefox、Safari 的版本矩阵和操作系统。
2. 覆盖登录、总览、节点、工作负载筛选、日志流、Secret reveal、扩容确认、390 px 导航。
3. 记录浏览器版本、操作系统、截图、失败 trace 和已知差异。

**验收**：矩阵内浏览器主路径全部通过；若某版本失败，必须有可复现步骤、影响范围和修复排期。
容器 WebKit 结果可以作为回归输入，但不能替代 Edge/Safari 实机放行结论。

### 26.6 真实 SIEM/Loki/OpenSearch 投递

**目标**：证明审计 Webhook 不仅能发出，还能被真实接收端验证、检索和告警。

**前置实施包**：`22.14`。在测试接收端完成私 CA、Bearer/HMAC、业务事件、重试、队列压力和 SIGTERM
排空之前，不进入真实 SIEM 接入；测试接收端通过也不替代本节验收。

**实施步骤**

1. 在测试环境部署受控 Loki/OpenSearch 或接入 SIEM 沙箱，启用 TLS 和鉴权。
2. 配置 `dosh-audit` Secret 的 Token/HMAC 与 `dosh-audit-ca` ConfigMap，启用
   `audit.webhook.enabled`、`audit.webhook.ca.existingConfigMap` 和精确出口 CIDR；同时确认
   NetworkPolicy 与 CiliumNetworkPolicy 都放行该目标，禁止 `0.0.0.0/0`。
3. 执行登录、失败登录、Secret reveal、扩容、重启、删除和只读拒绝，逐条在接收端检索。
4. 注入接收端 503、超时、队列满和重启，验证重试、丢弃告警和恢复。
5. 执行 dosh 滚动升级，验证 SIGTERM 时 `Flush` 不丢已入队事件。

**验收**：每条业务动作在 SIEM 可检索；Request ID 能关联 stdout 与 Webhook；签名校验通过；
Token/HMAC 不出现在日志；重试和最终失败都有告警；SIGTERM 后无已成功入队记录丢失。

**当前状态（2026-11-04，v2.45 当前候选）**：dosh 侧私 CA 和双认证能力已实现；`tools/auditreceiver`
测试夹具已构建，并通过 v2.45 隔离 Audit/Reveal 的私 CA、Token/HMAC、业务成功/拒绝、瞬时/持续 503、
超时恢复、队列压力、SIGTERM 排空和敏感信息扫描矩阵。真实 Loki 仍为 v2.44 历史证据：写入 59 条事件、
回查 34 个 requestId，其中 20/20 SIGTERM 记录命中，标签 `job/service`、无租户 401 和租户隔离通过；
v2.45 未复跑。该结果关闭
“真实 Loki 可接收、可检索”的实验室门禁，但 Loki 当前使用私 CA、临时 token、`emptyDir` 和
24h retention，未完成企业生产 SIEM/OpenSearch 的持久化、告警、访问控制、容量 SLA、长期保留和
离机 WORM，因此本节仍不能标记为完整商用验收。

### 26.7 CI 真跑与分支保护

**目标**：让自动门禁成为唯一可信发布证据，而不是本地命令清单。

**实施步骤**

1. 将仓库接入 GitHub Actions，首次运行全部 8 个 job；修复缓存、权限、容器和版本差异。
2. 保存 Playwright、性能、Trivy、Helm 渲染和容量报告 artifact。
3. 为 `main` 启用分支保护，要求 `release-summary` 成功并禁止 force push。
4. 用一次故意失败的 PR 验证门禁会阻断合并，再恢复。

**验收**：同一提交连续两次全绿；artifact 可下载；失败 job 不能绕过；发布记录引用 CI run URL 和镜像 digest。

### 26.8 产品补齐：Deployment 创建向导

**目标**：降低必须手写 YAML 的门槛，同时保持后端校验和审计。

**实施步骤**

1. 定义创建 DTO：名称、命名空间、镜像、副本、端口、资源 requests/limits、环境变量和标签。
2. app 层校验名称、DNS label、资源范围、镜像格式和命名空间授权；禁止客户端传入任意 YAML 绕过校验。
3. 通过 create use case 调用 Kubernetes API，先 Dry Run，再正式创建；非法字段返回 400
   `fieldErrors`，重复资源返回 409 `already_exists`。
4. 前端提供向导、预览 YAML 和确认步骤；创建成功后跳转详情并刷新列表。
5. 增加 fake client 成功/冲突/无权限/非法字段测试和 E2E 创建流程，最后清理测试命名空间。

**验收**：向导创建的资源与 kubectl 对账一致；非法字段不产生写请求；无权限返回 403；创建和失败均写审计。

**执行结果（2026-11-02，隔离 OIDC Release）**

- 后端已实现 `POST /api/v1/workloads/deployments` 和 `/preview`；domain/app/kube 分层完成，
  app 在授权和领域校验后先执行 API Server Dry Run，再正式创建。
- 前端已实现三步向导：基础信息、容器/资源/环境变量/标签、YAML 预览与确认；创建成功后跳转详情并
  刷新工作负载查询。创建按钮仅在 `capabilities.create=true` 时显示。
- 前一候选 `0.1.3-create-1793626058` 的真实集群矩阵包含 operator 向导创建、viewer 403、
  非法字段 400、重复创建 409 `already_exists`、kubectl 对账和审计，合并 OIDC 浏览器矩阵为 12/12。
- 最终候选 `0.1.3-rc-1793626263` 已包含该功能并构建 digest，但浏览器/创建矩阵尚未在该不可变候选上
  复跑；TRIVY 也尚未执行。因此状态为“已实现 + 前一候选实验室已验收”，不是最终放行。
- 前一完整门禁候选 `0.1.3-rc-20261102153511` 已在隔离 OIDC Release 复验 operator 创建、
  viewer 403、非法字段 400、重复创建 409、kubectl 对账、审计和 12/12 正向浏览器矩阵；这些结果
  不自动继承给当前 OIDC 候选。
- 已部署旧候选 `0.1.3-rc-oidc-20261102171324` 的 33/33 浏览器矩阵包含创建向导正向流程；当前
  候选发布前仍需按 25.10 在 registry digest 上复跑 400/409/对账和审计明细。
- 重复创建曾误报 resourceVersion 冲突，已新增 `ErrAlreadyExists` 和 HTTP 409 映射，并由 kube、
  handler 和真实集群测试覆盖。

### 26.9 商用放行矩阵

> 历史口径说明：下表保留 v2.44、v2.43、v2.41、v2.40 及更早候选的证据，用于追溯产品和门禁能力。
> v2.45 当前候选的实际复跑范围以 21.2.39 至 21.2.41 为准，已覆盖 Secret 解码、Vitest、race、
> mock 三浏览器、真实 OIDC 三浏览器、隔离 Audit/Reveal、Metrics 三态、429/5xx retry、容量、LCP、
> bundle 和漏洞扫描；v2.44 及更早候选的结果只能用于历史追溯，不能继承给 v2.45。
> 七个外部商用门禁仍全部未关闭。

| 能力 | 必须达到的证据 | 当前状态 | 未达到时的处置 |
|---|---|---|---|
| v2.45 当前候选实验室交付 | Secret 明文 Base64 解码、二进制/非法值回退、复制语义；Vitest、race、mock 三浏览器、真实 OIDC 三浏览器、隔离 Audit/Reveal、Metrics 三态、429/5xx retry、容量/性能/安全扫描、串行资源隔离和源码/证据绑定 | `0.1.4-rc-secretdecode-20261104T120606Z@sha256:c7eff34a…3ab06` 已通过 Vitest `62/62`、全量 race、mock Chromium/Firefox/WebKit 各 `38/38`、真实 OIDC 各 `16/16`、Audit/Reveal、Metrics 三态、429/5xx retry、容量 P95 `20.60/189.37/202.47 ms`、LCP 732.0 ms、bundle 350.62 KiB、govulncheck 0、生产 npm 审计 0、Trivy 0 HIGH/CRITICAL、33 文件主证据归档及 Metrics/retry 补充归档；生产未改动。七个外部商用门禁和生产授权仍未完成 | 在七个外部商用门禁、生产授权和授权后的发布/回滚验证完成前，不得宣称完整商用 |
| v2.44 历史候选实验室交付 | WBS-4.10 日志流、真实 OIDC Chromium/Firefox/WebKit、rollback/restore、Metrics 三态、429/5xx retry、真实 Loki、30 分钟 soak、工作负载生命周期、CronJob 详情、串行资源隔离和源码归档 | `0.1.4-rc-logframes-20261104T102214Z@sha256:e4a27d8b…49439` 已通过：OIDC Chromium/Firefox/WebKit 各 `16/16`、18/18 denied 审计、Token 扫描 0、mock 三浏览器各 37/37、revision `181→184`、Metrics 可用/不可用/恢复、五类 429/5xx、Loki 59 事件/34 requestId、20/20 SIGTERM 排空、soak 30 事件/50,577 帧/26,577 行/0 错误、13/13 生命周期审计；综合归档 553 文件和 WebKit 补充归档 11 文件。该结果不继承给 v2.45；生产仍为 App `0.1.2` / Chart `0.1.4` revision 12 | 七个外部商用门禁和生产发布/回滚完成前，不得宣称完整商用 |
| v2.43 历史候选实验室交付 | WBS-5.1 写操作闭环、WBS-5.8 镜像更新、WBS-5.9 CronJob 暂停/恢复、受限串行三浏览器 mock 与真实 OIDC、Metrics 三态、429/5xx retry、真实 Loki 审计、新鲜 race/Vitest/容量/LCP/rollback/soak、源码与文档绑定 | `0.1.4-rc-cronjobsuspend-20261104T075420Z@sha256:779ecae7…33306` 已通过：预览→创建→扩容→镜像更新→重启→删除闭环与 13 条带 requestId 的 success/failure/denied 审计、mock Chromium/Firefox/WebKit 各 37/37、真实 OIDC 三浏览器各 16/16、Vitest 51/51、race、容量 Pods/Workloads/Overview P95 `20.24/194.47/199.40 ms`、LCP `692 ms`、rollback/restore `169→172`、30 分钟 soak 0 错误/0 流失败、Metrics 可用/不可用/恢复、五类 429/5xx 注入、真实 Loki 59 事件/34 requestId 和租户隔离；585 文件主归档及 OIDC（10）、Metrics/retry（20）、Loki（9）、workload lifecycle（28）补充归档已校验。生产仍为 App `0.1.2` / Chart `0.1.4` revision 12，未升级 | 七个外部商用门禁和生产发布/回滚完成前，不得宣称完整商用 |
| v2.41 历史候选实验室交付 | WBS-4.8 审计历史、真实本地认证/OIDC、三浏览器容器矩阵、LCP、容量 SLO、Trivy、源码与文档绑定 | `0.1.4-rc-audit-20261104T062708Z@sha256:1e16c0fb…5f94` 已通过上述实验室矩阵；v2.40 的 Metrics/retry/Loki/soak/rollback 不继承；生产仍为 App `0.1.2` / Chart `0.1.4` revision 12，未升级 | 七个外部商用门禁和生产发布/回滚完成前，不得升级生产或宣称完整商用 |
| v2.34 节点运维候选 | Cordon/Uncordon app 授权与审计、Kube 条件写、OpenAPI/TS、前端二次确认、local RBAC、隔离 Release 状态对账、完整候选门禁 | `0.1.4-rc-node-20261103T223506Z@sha256:0c61ce34…c5bda` 在最终 revision 149 通过 viewer/operator 403、admin 200、kubectl 状态恢复和四条带 requestId 审计；`make verify`、race、Vitest 18/18、mock Chromium/Firefox/WebKit 各 16/16、真实 OIDC 各 15/15、Trivy/govulncheck/npm、容量、30 分钟 soak、rollback/restore `142→145`、retry、Metrics 三态、真实 Loki、CronJob 详情、LCP `516 ms` 和源码绑定均通过；文档绑定完成前不得升级生产 | 外部商用门禁和生产发布/回滚完成前，不得升级生产 |
| v2.31 最终候选源码与交付 | 8h 会话上限、TLS Secure Cookie、API no-store、COOP/CORP、受保护 logout、YAML 单文档/重复键拒绝、kind 先校验后 SAR、扩容期望态写入、源码绑定和新 digest 全矩阵 | `make verify`、`make race`、容量、Vitest 16/16、bundle `340.72 KiB`、mock/真实 Chromium/Firefox/WebKit 各 15/15、soak、rollback、审计、Trivy/govulncheck/npm audit 全部通过 | 候选实验室已验收；企业 IdP、Edge/Safari 实机和外部商用仍待关闭 |
| 身份与授权 | OIDC 三身份 API/RBAC 矩阵 + 浏览器矩阵 + SAR + impersonation 审计 + 企业 IdP | v2.45 当前 digest 在测试 IdP 通过真实 OIDC Chromium/Firefox/WebKit 各 16/16、host 两浏览器 12/12 与隔离 WebKit 6/6 denied 审计、三身份 RBAC、审计历史权限、SAR 和 impersonation 审计；企业 IdP 未完成 | 仅限受控内网单管理员，禁止对外暴露；测试 IdP 矩阵不能替代企业 IdP 验收 |
| TLS | 受信 CA + SAN + 无 `-k` 访问 | 自签证书，未完成 | 仅限实验环境，禁止生产域名接入 |
| Metrics | 真实 Metrics Server 有/无/恢复三态对账 | v2.46 当前 digest 通过 Available CPU `2195m` / 内存 `11654705112`、不可用归零、恢复 CPU `2112m` / 内存 `11641643008`；证据见 21.2.42 | 生产/预发 Metrics 拓扑仍需在发布变更中复验 |
| 长连接 | 30 分钟 soak + 断线重连 + 内存曲线 | v2.46 当前 digest 通过：30 事件、60,039 事件帧、26,553 行日志、5 次重连、0 应用错误、0 流失败，内存 `24→18 MiB`、峰值 34 MiB；覆盖 Pod 删除和应用滚动重启 | 生产/预发同等并发复验后关闭目标环境门禁 |
| 故障恢复 | 429/5xx 注入 + 有限重试 + 回滚 | v2.46 当前 digest 通过五类瞬时故障、持续 429 三次、`Retry-After` 约 2100 ms、写请求仅 1 次，并在当前 digest 完成 rollback/restore `198→201` | 生产/预发仍需同等故障注入 |
| 安全写操作与产品补齐 | 创建/扩容/镜像更新/重启/删除 Dry Run、Cordon/Uncordon、授权、审计、409 对账 + 浏览器矩阵 | v2.46 当前 digest 通过完整写操作闭环：preview 不落盘、viewer 403、创建 201、重复 409、扩容、镜像更新、重启、删除、13 条生命周期审计，以及 Cordon/Uncordon 和 mock/真实三浏览器矩阵 | 生产发布前按 Release 变更单复核 API 对账、Cordon 恢复和审计检索 |
| 动态集群上下文 | 隔离 Release 设置非实验室集群名；顶部栏/侧栏/关于弹窗/总览面包屑与 overview 一致；503 显示不可用 | v2.39 mock Chromium/Firefox 各 32/32、真实 OIDC 各 15/15；历史 mock 45/45 不继承 | 企业 IdP、Edge/Safari 另行验收 |
| 审计 | 私 CA + Token/HMAC + Cilium 出口 + 真实 SIEM 投递 + SIGTERM 排空 | v2.46 当前 digest 通过隔离 Audit/Reveal 的私 CA、双认证、失败审计、瞬时/持续 503、超时恢复、队列压力、只读拒绝、20/20 SIGTERM 排空、泄露扫描 0，以及真实 Loki 59 事件/34 requestId 和租户隔离；生产 SIEM 的持久化、告警、访问审批、保留和离机 WORM 未完成 | 接入企业 SIEM/OpenSearch，完成保留、告警、访问和 WORM 验收后关闭 |
| 证据与审计留存 | candidate digest 绑定 + manifest 覆盖 summary + manifest 摘要外锚 + 最终文档绑定 + 离机 WORM/保留审批 | v2.46 当前候选最终归档 599 文件（manifest `d761698e…aff9`）通过双层校验，并绑定源码与最终文档；离机 WORM 未完成 | 生产发布前把原始归档和 manifest 摘要写入变更单并复制到企业批准存储 |
| CI | GitHub Actions 全绿 + artifact + 分支保护 | 8 个 job、policy 门禁、版本一致性和 digest 渲染已实现，未真跑 | 发布门禁仍为人工，不能称完整可商用 |
| 浏览器 | 矩阵内真实 Edge/Safari 抽样 | v2.46 mock Chromium/Firefox/WebKit 各 38/38、真实 OIDC Chromium/Firefox/WebKit 各 16/16；Edge/Safari 实机未完成 | 补齐 Edge/Safari 实机；不能把 WebKit 容器等同于 Safari |
| P1 体验与无障碍 | 暗色主题 token/切换/持久化 + WCAG 2.1 AA 键盘与 axe 门禁 | v2.46 当前 digest：Vitest 62/62、mock 三浏览器各 38/38、真实 OIDC 三浏览器各 16/16，含主题、axe、键盘，axe 0 serious/critical | UX-O-001/NFR-U-008 三浏览器容器实验室已验收；企业 IdP、Edge/Safari 实机和生产浏览器验收仍需另行关闭 |
| OIDC 负向登录 | state 缺失/篡改/不匹配/过期 + nonce mismatch + code 重放 + 开放重定向 | v2.46 当前 digest 的真实 OIDC Chromium/Firefox/WebKit 各 16/16，七类负向登录通过，每浏览器 6/6 denied 审计带 requestId | 企业 IdP 按同一矩阵复跑并归档后才能关闭商用身份门禁 |

**最终放行条件**：上表全部为“已验收”，或每个未验收项都有具名负责人、书面风险接受、
明确到期日和回滚/隔离措施；否则 `dosh` 仍只能标记为“实验室内部可用”。

### 26.10 P1 主题、可访问性与 OIDC 浏览器负向关闭计划

#### 26.10.1 UX-O-001 暗色主题

**EARS 条款**：当用户选择暗色主题时，系统应使用与亮色模式相同语义的 token 呈现全部页面，
并保持成功、警告、危险、信息状态可辨识；当用户重新加载页面时，系统应保持该主题选择。

**当前状态（2026-11-04）**：已实现 `.dark` 语义 token、系统偏好解析、显式选择和持久化，
AppShell 提供键盘可操作入口；Sonner、CodeMirror 和状态语义色已适配。Vitest 覆盖系统偏好、
显式选择、持久化和非法值回退；v2.45 当前候选的 Chromium/Firefox/WebKit E2E 覆盖切换、刷新保持和
语义 token。源码状态为“v2.45 签名测试 IdP 的三浏览器容器实验室已验收”，企业 IdP、Edge/Safari
实机和生产仍未验收。

**实施步骤（已完成项保留作回归基线）**

1. 在 `web/src/index.css` 为 `.dark` 定义完整语义 token：`background`、`foreground`、`card`、
   `popover`、`primary`、`secondary`、`muted`、`accent`、`destructive`、`border`、`input`、
   `ring` 及 `success`/`warning`/`info` 状态色；页面和组件禁止新增硬编码颜色。
2. 新增主题控制器，优先级为“用户显式选择 → `prefers-color-scheme` → light”；只持久化
   `light|dark` 到本地存储，不保存用户身份或 Token。
3. 在 `main.tsx` React 渲染前应用根节点 `dark` class，避免首屏闪烁；不得引入内联脚本，
   保持现有 CSP 的 `script-src 'self'`。
4. 在 AppShell 用户菜单加入可键盘操作的主题切换项，提供可读标签和当前状态；切换后立即
   更新图表、表格、状态徽标、Dialog、Dropdown 和 YAML 编辑器。
5. 增加 Vitest 用例覆盖系统偏好、显式选择、持久化、非法存储值回退；增加 Chromium/Firefox/
   WebKit E2E：切换主题、刷新保持、退出登录后不泄露身份、关键页面无亮色残留。

**验收命令与判据**

```bash
cd web
npm run typecheck
npm test -- --run
npm run e2e:container
npm run build && npm run bundle:check
```

通过标准：主题 E2E 在获批浏览器矩阵全部通过；刷新后主题保持；无首屏闪烁截图；状态色不只靠
颜色区分；亮/暗两套 token 均通过对比度检查；bundle 预算不退化。v2.45 当前候选已达成 mock
Chromium/Firefox/WebKit 各 `38/38`、真实 OIDC Chromium/Firefox/WebKit 各 `16/16`；initial JS gzip
`350.62 KiB` 仍低于预算。v2.44 的 `37/37` 和 `350.02 KiB` 只作历史追溯。Safari/Edge 实机仍未验收。

#### 26.10.2 NFR-U-008 WCAG 2.1 AA 基础可访问性

**EARS 条款**：当用户仅使用键盘或辅助技术操作登录、总览、工作负载、日志、YAML 编辑和写操作
确认流程时，系统应提供可见焦点、可读标签、正确语义和可感知错误提示。

**当前状态（2026-11-04）**：已引入固定版本 `@axe-core/playwright@4.14.0`/`axe-core@4.14.0`，
axe 覆盖登录、总览、节点、工作负载详情和 Pod 详情，亮暗主题各一次；修复 Progress 名称、
成功徽标对比度、正文链接可区分性、共享表格滚动区域可键盘聚焦和浅色 muted 文本对比度。
v2.39 进一步复现并修复工作负载详情 `Failed` 徽标仅 `4.13:1` 的缺陷：新增
`--destructive-strong` 亮/暗语义 token，危险状态文字切换 token，危险实色按钮保持原样式，并新增
`status-badge.test.tsx`。v2.45 当前 digest 的 Vitest 为 `62/62`，mock Chromium/Firefox/WebKit 各
`38/38`、真实 OIDC Chromium/Firefox/WebKit 各 `16/16`；真实 Release 的亮暗 axe、主题和键盘路径
全部通过，无 serious/critical。当前可标记为“签名测试 IdP 的三浏览器容器实验室已验收”；
企业 IdP、Edge/Safari 实机和生产环境仍需单独验收。

**实施步骤**

1. 引入 axe 扫描（CI 使用固定版本），覆盖登录、总览、节点、工作负载详情、Pod 日志、YAML
   Dry Run、创建向导确认和错误/空状态，亮暗主题各扫描一次。
2. 对 Dialog、Dropdown、Tabs、Table 和 Toast 检查 focus trap、`aria-*`、标题关联和 Escape/
   Enter/Space 行为；图标按钮必须有可读名称。
3. 正文对比度不低于 4.5:1，大字和 UI 边界不低于 3:1；状态必须同时有文字或图标。
4. 增加键盘 E2E：登录 → 侧栏导航 → 打开详情 → 扩容确认 → 退出；断言焦点顺序和可见焦点环。
5. 保留人工检查记录：至少一名评审人使用键盘完成主路径；如企业要求，补充 NVDA/VoiceOver 抽样。

**验收**：axe 在关键页面 0 个 serious/critical；键盘 E2E 通过；无“只能鼠标操作”的阻断；
问题清单与修复证据归档到 candidate digest；未通过项保持 NFR-U-008“未验收”。

#### 26.10.3 AUTH-O-005 浏览器级 OIDC 负向矩阵

**EARS 条款**：当 OIDC 回调包含非法/过期 state、nonce mismatch、重放 code 或开放重定向参数时，
系统应拒绝登录、不得建立会话，并记录不含 Token 的 denied 审计。

**当前状态（2026-11-04）**：已实现 `DOSH_OIDC_STATE_TTL`（默认 `10m`，允许 `1s–30m`）、
测试 IdP `nonce_mismatch` 故障注入和七类浏览器负向用例。v2.45 当前 digest 在隔离签名测试
IdP 上通过真实 OIDC Chromium/Firefox/WebKit 各 `16/16`；负向登录均被拒绝，host 两浏览器 `12/12`
与隔离 WebKit `6/6` denied 审计带 `requestId`，Token 扫描 0 命中。v2.34 候选
`0.1.4-rc-node-20261103T223506Z@sha256:0c61ce34…c5bda` 在隔离 Release revision 149 的
mock Chromium/Firefox/WebKit 各 16/16、真实 OIDC 各 15/15 通过，每浏览器 6/6 denied 审计带
`requestId`，Token 扫描 0 命中；WebKit 由隔离 libvirt runner 执行。历史候选
`0.1.3-rc-oidc-20261102171324@sha256:0be6df25…798ca8` 的 33/33 只保留追溯。
v2.45 结论为“签名测试 IdP 的三浏览器容器实验室已验收”；企业 IdP 接入后必须在当前候选上原样复跑。

本地安全补强进一步拒绝 `returnTo=/\evil.example` 这类浏览器会规范化为跨站地址的反斜杠输入，
并把编码 CRLF、绝对 URL 和 `javascript:` 纳入单元负向回归；任何后续代码变更仍须按同一矩阵
在当前不可变 digest 上重新验收。

**实施步骤**

1. 扩展现有测试 IdP fixture，使其能生成过期 state、错误 nonce、重放 code 和恶意 `redirect_uri`。
2. 在 `web/e2e-oidc/` 增加负向用例；每次失败后断言 URL 回到 `/login`、`/api/v1/session` 为
   401、浏览器 Storage/Cookie 无会话、日志无 code/Token。
3. 对每条负向事件检查审计 `result=denied` 和 requestId 关联；确认没有 Kubernetes 业务写请求。
4. 在 Chromium/Firefox/WebKit 执行矩阵；企业 IdP 接入后按同一矩阵复跑。

**验收**：七类负向场景均拒绝且可审计；三浏览器全绿；无会话固定、开放重定向或 Token 泄露；
证据进入当前 candidate digest 的受控归档。当前三浏览器已通过；企业 IdP 和 Safari/Edge 实机仍需复验。


## 27. v2.47：Kubernetes Token 默认登录与 HTTP/HTTPS 双模式

### 27.1 范围与决策

用户明确要求：OIDC 和自定义 CA 不是 dashboard 的必需项；控制台应能直接使用 Kubernetes 中获取的
Bearer Token 登录，并同时支持 HTTP 和 HTTPS。v2.47 因此把 `token` 设为默认认证模式，把 `local`
和 `oidc` 保留为可选模式，把 TLS 证书和 OIDC CA 降为可选部署输入。

| 决策 | 实施 |
|---|---|
| 默认认证 | `DOSH_AUTH_MODE=token`；Helm `auth.mode=token` |
| Token 校验 | 浏览器提交 Bearer Token → `TokenReview` → Kubernetes username/groups |
| 后续授权 | `SubjectAccessReview` 判定能力；业务请求使用 `Impersonate-User`/`Impersonate-Group` |
| ServiceAccount Token | Chart 增加 `serviceaccounts` 的 `impersonate`，避免业务 API 被 Kubernetes 拒绝 |
| Token 保护 | 不写 Cookie、浏览器存储、URL、审计或日志；最大 16 KiB；失败统一 401 |
| 登录限速 | local/token 均可使用命名空间 Lease 跨副本限速；内存模式仅用于单实例或开发 |
| HTTP | 显式 `tls.enabled=false`、`auth.cookieSecure=false`，探针自动使用 HTTP |
| HTTPS | 显式 `tls.enabled=true`；证书可为自签，受信 CA 非必需 |
| OIDC/CA | 仅在启用 OIDC 时校验 issuer/client/redirect；只有 IdP 使用私 CA 才挂载 CA |
| 商业门禁 | OIDC 和受信 TLS CA 从当前 release manifest 移除；其余 5 项外部门禁保持 `pending` |

### 27.2 EARS 验收条款

v2.47 的详细条款落在 5.1、6.1、6.3：

- `AUTH-T-001`～`AUTH-T-007`：默认 Token 登录、身份不可伪造、Token 不泄露、分布式限速、
  ServiceAccount Token、有限会话和 HTTP/HTTPS 双模式。
- `AZ-T-001`～`AZ-T-002`：Token 身份的 SAR 判权、impersonation 和依赖失败 fail-closed。
- `SYS-N-005`：Secure Cookie 不得在 HTTP 下误用；HTTP 必须显式关闭。

### 27.3 实施步骤与文件落点

1. **认证域**：`internal/modules/auth/app/token.go` 定义 Token 登录用例和 Token 长度校验；
   `token_test.go` 覆盖成功、空白和无效 Token。
2. **Kubernetes 适配器**：`internal/platform/kube/token_review.go` 调用 TokenReview，返回
   username/groups/`system:masters`；`token_review_test.go` 断言 audience、裁剪和拒绝路径。
3. **组合根**：`internal/bootstrap/app.go` 根据模式选择 Local/Kubernetes Authorizer，构造 Token
   服务，并把 Lease 限速器注入 local/token 两种登录服务。
4. **HTTP 适配**：`internal/modules/auth/http/handler.go` 接收 `{token}`，创建加密会话；Token
   只存在于请求体生命周期内。`token_handler_test.go` 校验 Cookie、身份和日志不含 Token。
5. **配置**：`internal/platform/config/config.go` 默认 token；TLS/Cookie 联动；Lease 限速仅允许
   local/token；OIDC 配置只在 OIDC 模式校验。
6. **Helm**：`values.yaml` 默认 token；`deployment.yaml` 有 token audience 与 Lease 限速环境变量；
   `rbac.yaml` 授予 `tokenreviews/create`、`subjectaccessreviews/create`、
   `users/groups/serviceaccounts/impersonate` 和 Lease 限速权限。
7. **前端**：登录页按 `/auth/config` 显示 Kubernetes Token 输入；`api.loginWithToken` 使用同源
   POST；Token 不持久化。Vitest 覆盖登录和存储扫描。
8. **契约**：`api/openapi.yaml` 和生成的 TypeScript schema 增加 `token` 登录模式与请求体。

### 27.4 隔离验收矩阵

候选：`192.168.122.10:5000/dosh/dosh:0.1.5-rc-token-20261105T012750Z`，registry digest
`sha256:c114113009ddad67313f37c288fe4cc6f7d13848de01d22dd68499146744ea2b`。隔离环境：
`dosh-token-e2e/dosh-token-e2e`，NodePort `32628`，生产 `dosh` 未改动。

| 场景 | 结果 |
|---|---|
| HTTP 部署 | `/healthz`、`/readyz` 200；探针 scheme 为 HTTP；无 TLS Secret |
| HTTPS 部署 | 自签证书 SAN 覆盖 Node IP；`/healthz` 200；Cookie 带 `Secure` |
| 有效 Token | 200，返回 `authMode=token`、Kubernetes username/groups 和 CSRF |
| 无效/空白 Token | 空白 Token 400、无效 Token 401，均不设置 Cookie |
| ServiceAccount Token | 200；修复 `serviceaccounts/impersonate` 后业务 API 200 |
| viewer | pods/workloads 只读 200；scale/reveal 403；capabilities 写能力为 false |
| operator | pods/workloads 200；带 CSRF scale 200；无 CSRF 403；kubectl replicas 对账一致 |
| admin | 全部能力 true；节点/命名空间/工作负载/pods 200；带 CSRF 写操作 200 |
| Logout | 204；浏览器 Cookie 清除后 `/session` 返回 401 |
| Token 泄露 | Cookie 和 Pod 日志扫描 0 命中 |
| 分布式限速 | 登录后命名空间生成 `dosh-login-rate-*` Lease |
| 审计历史 | cluster-admin ServiceAccount 的 Token 会话通过 `dosh.io/audit-events` SAR，返回 200 |
| 自动化 | Go 全量/race、Vitest `63/63`、Chromium/Firefox 各 `39/39`、容量 SLO、govulncheck、npm audit 和 Trivy 通过 |
| 证据与绑定 | 源码绑定 `deploy/release/bindings/source-binding-v247-token.txt`；证据归档 `/var/lib/dosh/evidence/c114113009dd/20261105T014514Z-v247-final-token-r1`，23 文件，manifest SHA-256 `734cbdbc10b15e4d48f56665eed24e5e170ec68622ce8a5390b0a92114570005` |

### 27.5 已完成项、未完成项与放行规则

- 当前 digest 已完成 Go 全量、race、Vitest `63/63`、Chromium/Firefox 各 `39/39`、web build、
  bundle `350.93 KiB`、容量 SLO、govulncheck、生产 npm audit、Trivy 和镜像 digest 推送校验。
- 隔离 Release 已完成 HTTP/HTTPS 部署、Helm upgrade/rollback、有效/无效 Token、Cookie `Secure`、
  viewer/operator/admin RBAC、CSRF 拒绝、logout、审计 SAR、ServiceAccount impersonation、
  Kubernetes Lease 限速和 Token 泄露扫描。
- 新 manifest 必须绑定本轮 source/documentation binding；旧 v2.46 及更早 manifest、证据和门禁状态
  均不继承。
- v2.46 及更早的 OIDC、浏览器、soak、性能和归档证据不继承给 v2.47。
- 当前仍有 5 项外部商用门禁：`production-siem-retention-alerting`、`edge-safari-real`、
  `github-actions-branch-protection`、`offsite-worm-retention`、`production-change-authorization`。
  `production-change-authorization` 必须 `passed`，不能用风险接受代替。
- 在上述门禁和生产变更授权完成前，不得升级生产、不得输出“完整商用放行”结论。

## 28. v2.48 服务端证书配置管理（实施中）

本节记录下一候选的服务端证书能力。它不继承 v2.47 的镜像 digest、浏览器、性能或发布证据；完成镜像
构建、隔离部署和全矩阵验收前，不得更新 commercial gates 候选或升级生产。

### 28.1 范围与安全边界

第一版只管理 Dosh 自身的 HTTPS 服务端证书，不通过浏览器上传私钥，不写入 Kubernetes Secret，也不
开放任意文件路径。证书和私钥仍由 Helm 挂载的 `dosh-tls` Secret 提供；Dosh 负责启动校验、元数据
展示、文件变化检测、热加载和显式重载。

证书来源固定为 Kubernetes Secret，不使用 ConfigMap：默认 Secret 为 `dosh-tls`，键为 `tls.crt` 和
`tls.key`；Chart 以 `defaultMode: 0440` 只读挂载到 `/etc/dosh/tls`。Chart 只引用
`tls.existingSecret`，不创建 Secret，也不把私钥写入 values/Release 历史。

- 私钥、PEM 原文、Secret 明文、Session Key 不得进入 API 响应、浏览器存储、审计、日志、错误文本或
  发布证据。
- 证书状态只包含主题、签发者、序列号、SAN、有效期、剩余天数、公钥算法、SHA-256 指纹和加载状态。
- 证书加载失败不得导致运行中的 TLS 监听中断；必须继续使用最后一次成功加载的证书。
- HTTP 模式不得读取证书文件；`GET /api/v1/certificate` 返回 `status=disabled`。
- 显式重载通过 `POST /api/v1/certificate/reload` 执行，必须经过会话、CSRF、只读模式和
  `update dosh.io/server-certificates` 授权检查，并写审计。

### 28.2 EARS 条款

- **CERT-001**：当 Dosh 以 HTTPS 启动时，系统应在开始监听前读取证书和私钥、校验 PEM 与密钥匹配、
  解析叶子证书，并在失败时拒绝启动。
- **CERT-002**：当 HTTPS 证书文件在运行期间发生变化时，系统应在一个轮询周期内原子切换到新证书，
  不要求重启 Pod。
- **CERT-003**：当证书轮询间隔设置为 `0` 时，系统应关闭自动轮询，但仍允许有权限的显式重载。
- **CERT-004**：当新证书或私钥无效时，系统应保留最后一份可用证书继续服务，记录不含私钥的失败
  状态，并在后续周期重试。
- **CERT-005**：当已认证用户读取证书状态时，系统应返回公开证书元数据并按到期时间标记
  `valid`、`expiring`（30 天内）、`expired`、`not_yet_valid` 或 `unavailable`。
- **CERT-006**：当用户请求显式重载但不具备 `update dosh.io/server-certificates`、请求缺少 CSRF
  或实例处于只读模式时，系统应拒绝请求且不触发文件读取。
- **CERT-007**：当显式重载成功、失败或被拒绝时，系统应记录 `action=reload`、
  `resource=server-certificate` 的审计，且不得包含证书或私钥内容。
- **CERT-008**：当 Dosh 以 HTTP 模式运行时，证书状态应为 `disabled`，显式重载应返回
  `501 Not Implemented`。
- **CERT-009**：当 Helm 渲染 HTTPS 部署时，应注入 `DOSH_TLS_RELOAD_INTERVAL`；默认 `30s`，
  允许 `0` 或 `5s`–`24h`，非法值应在启动前失败。
- **CERT-010**：当证书状态渲染到前端时，界面应展示有效期、SAN、指纹和热加载状态，并且只有具备
  重载能力的用户能看到重载按钮。

### 28.3 实施落点

| 文件 | 职责 |
|---|---|
| `internal/domain/types.go` | `ServerCertificate` 非敏感 DTO 与 `reloadCertificate` capability |
| `internal/platform/tlscert/store.go` | 启动加载、PEM/私钥校验、原子证书存储、文件轮询和最后可用证书回退 |
| `internal/platform/httpserver/server.go` | `GetCertificate` 动态提供器接入，避免 `net/http` 预加载静态文件 |
| `internal/modules/certificates/app/service.go` | 状态查询、授权、审计和 `501` 语义 |
| `internal/modules/certificates/http/handler.go` | `GET /api/v1/certificate` 与 `POST /api/v1/certificate/reload` |
| `internal/bootstrap/` | 组合根注入 Store，启动轮询并接入路由 |
| `internal/platform/config/config.go` | `DOSH_TLS_RELOAD_INTERVAL` 默认值与边界校验 |
| `deploy/helm/dosh/` | `tls.reloadInterval` 渲染为环境变量 |
| `web/src/features/configuration/server-certificate-panel.tsx` | 证书状态页签与受权限控制的重载按钮 |
| `api/openapi.yaml` | 证书端点、DTO 和 capability 契约 |

### 28.4 验收步骤

1. **配置与单元测试**
   ```bash
   go test ./internal/platform/tlscert ./internal/modules/certificates/... \
     ./internal/platform/httpserver ./internal/platform/config -count=1
   make arch
   ```
   通过标准：有效证书解析、SAN/指纹正确；证书和私钥不匹配时启动失败；热加载失败保留旧证书；
   自动轮询在文件变化后切换；HTTP 模式返回 disabled；非法轮询间隔被拒绝。

2. **API 与权限**
   ```bash
   go test ./internal/bootstrap ./internal/modules/auth/... -count=1
   cd web && npm run api:check && npm run typecheck && npm test -- --run
   ```
   通过标准：GET/POST 路由与 OpenAPI 一致；未认证 401、无权限 403、只读 403、缺 CSRF 403、
   非法证书 400、HTTP 模式重载 501；审计不含私钥或 PEM。

3. **隔离 HTTPS 热加载**
   - 使用独立 namespace/Release、NodePort 和临时自签证书，禁止使用生产 Secret。
   - 记录证书 A 的 SHA-256 指纹，更新 Secret 为证书 B，等待 kubelet 传播和轮询周期。
   - 通过 `GET /api/v1/certificate` 和实际 TLS 握手确认指纹变为 B，服务无重启、无连接中断。
   - 写入不匹配的证书/私钥或非法 PEM，确认握手仍使用 B，状态显示失败原因。
   - 恢复证书 C，确认下一周期自动恢复为成功状态。
   - 显式调用 reload，核对 `action=reload` 审计和 RBAC 拒绝路径。

4. **发布绑定**
   - 构建并推送新的不可变镜像 digest。
   - 重新生成 source binding、documentation binding、commercial gates candidate 和证据归档。
   - 按外部商用门禁要求复核；在 5 项门禁关闭前不得升级生产或宣称完整商用放行。

### 28.5 当前状态

- 已实现：HTTPS 动态证书提供器、文件变化轮询、最后可用证书回退、只读状态 API、受授权/CSRF/审计
  保护的手动重载、HTTP disabled 响应、前端证书状态页签、Helm 轮询配置、README 与运行手册说明。
- 已通过本轮开发验证：Go 全量测试、证书 Store/HTTP/配置/认证模块测试、Vitest 与前端类型检查；
  待执行 `make arch`、`make api-check`、`make verify`、race、容器化浏览器和隔离集群热加载验收。
- 未完成：新镜像构建/digest、source/documentation binding、commercial gates 候选更新、隔离
  HTTPS 热加载证据和全部外部商用门禁。
