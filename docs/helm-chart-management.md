# Helm Chart 仓库与受控安装

Dosh 的 Helm 能力分为两条路径：

1. **已安装 Release 运维**：浏览 Helm Secret 中的 Release、Values、渲染清单、历史版本和 Diff，并执行受控回滚或增量 Values 升级。
2. **Chart 仓库与安装**：管理员登记受控 Chart 仓库，用户从固定目录选择 Chart 和版本，先审阅安装计划，再执行安装或升级。

本文重点说明第二条路径。它不是任意 Chart 市场：Dosh 不抓取任意 URL，也不会让浏览器直接访问远程仓库。

## 1. 安全模型

### 受控仓库

- Chart 仓库只能由具备 `create dosh.io/helm-repositories` 权限的身份登记。
- 支持两类仓库：提供标准 `index.yaml` 的 HTTP/HTTPS 仓库，以及指向单个 Chart 的 `oci://` 仓库。
- HTTP/HTTPS 仓库的 `index.yaml` 可以把具体版本指向 `oci://registry/.../chart:tag`（Bitnami 已采用该格式）。Dosh 会按 OCI 方式拉取该版本；仓库凭据只发送给仓库原主机，不会泄露给 `index.yaml` 指定的其他 OCI registry。
- OCI 地址必须指向一个 Chart 仓库路径，例如 `oci://registry.example.com/team/charts/nginx`。地址不能带 `:tag`，版本由 OCI Tag 表示。
- 一个 OCI 仓库配置只对应一个 Chart；Dosh 不会把整个 OCI namespace 或 registry 下的多个 Chart 自动聚合到目录中。
- 生产环境应使用 HTTPS。明文 HTTP 仅允许管理员在隔离网络使用，并会在目录和计划中显示风险警告。
- 对于 HTTP/HTTPS 仓库，Chart 包 URL 可以是 `index.yaml` 发布的同主机相对路径，也可以是其他主机的绝对 HTTPS 地址（例如 Kubernetes Autoscaler 指向 GitHub Releases）。跨主机地址禁止降级到 HTTP，且 Dosh 不会把仓库凭据发送给其他主机。
- HTTP/HTTPS 仓库的 URL 最多跟随 5 次重定向；HTTPS 仓库禁止降级到 HTTP，OCI registry 重定向必须继续使用 HTTPS。
- 仓库地址不得包含用户名、密码、查询参数或 Fragment，避免凭据进入日志和浏览器历史。

### 凭据与 CA

- HTTP/HTTPS 与 OCI 仓库均支持匿名、用户名/密码、Bearer Token 三种认证方式。
- OCI 认证使用 Helm registry 客户端和 ORAS 的 Registry v2 Bearer/Basic 流程；凭据按仓库主机注入，不写入浏览器或 Chart 包。
- OCI Tag 必须符合 SemVer，版本列表按语义化版本倒序；无法解析为 SemVer 的 Tag 会被忽略。
- 凭据保存在控制面 Secret `dosh-helm-repository-credentials` 中，不返回浏览器。
- 自定义 CA 与认证凭据分开标记。匿名仓库也可以使用自定义 CA。
- 编辑仓库时，页面只显示“已保存认证/CA”；必须显式勾选替换才能录入新值。
- 更换仓库主机时，Dosh 拒绝静默沿用旧凭据。管理员必须重新录入凭据或显式清除。
- 勾选“跳过 TLS 证书校验”会降低供应链安全性，仅建议用于临时隔离环境。

### 仓库与操作存储

控制面命名空间由 `preferencesNamespace` 配置。默认使用 Dosh 所在命名空间。

| 对象 | 类型 | 用途 |
|---|---|---|
| `dosh-helm-repositories` | ConfigMap | 仓库目录、默认仓库、启用状态、最近同步结果 |
| `dosh-helm-repository-credentials` | Secret | Basic/Bearer Token 与自定义 CA PEM |
| `dosh-helm-operation-<hash>` | ConfigMap | 每个安装、升级、卸载和回滚操作对应一条记录，最多保留 500 条 |
| `dosh-helm-operation-lock-<hash>` | ConfigMap | 每个 Release 或仓库同步任务独立的跨副本操作锁；正常释放后立即回收，崩溃遗留的过期锁由后台清理 |
| `dosh-helm-chart-operations` | ConfigMap | 旧版本聚合操作记录的只读兼容入口 |

仓库目录使用 Kubernetes `resourceVersion` 做乐观并发控制。多个管理员同时编辑时，过期页面会收到冲突而不是覆盖最新配置。操作记录超过 500 条时自动淘汰最旧的已完成记录，不会删除运行中或等待中的记录。

## 2. 权限要求

| 能力 | Kubernetes RBAC / Dosh 权限 |
|---|---|
| 浏览仓库与 Chart | `list dosh.io/helm-repositories` |
| 新增、修改、删除、同步仓库 | 对应 `create` / `update` / `delete dosh.io/helm-repositories` |
| 同步单个 Chart 包 | `update dosh.io/helm-repositories` |
| 生成计划 | `list secrets`，以及目标命名空间 `create`、`update secrets` |
| 安装 / 升级 | 目标命名空间 `create`、`update secrets`，以及计划中所有资源的 `create` 或 `update`/`patch` |
| 卸载 | `delete secrets`、`update secrets` 与目标资源删除权限 |
| 回滚 | `update secrets` 与目标资源更新权限 |
| Helm test | 目标命名空间 Secrets、Pods 与 batch Jobs 的 `list`、`create`、`update`、`delete` |

Dosh 还会逐项对渲染出的资源执行 SubjectAccessReview。计划中包含 Hook 和 CRD 时也会纳入预检。任何一项权限不足都会在写入前失败。

所有 Helm SDK 操作显式使用当前用户 impersonation，避免后台使用 Dosh ServiceAccount 绕过用户 RBAC 读取或写入 Secret。

Dosh 的全局只读模式同样阻止安装、升级、卸载和回滚。

## 3. Chart 目录与版本固定

1. 管理员登记 HTTP/HTTPS 仓库后，Dosh 立即尝试同步 `index.yaml`。
2. 管理员登记 OCI 仓库后，Dosh 通过 Registry v2 Tag API 读取该 Chart 的 SemVer 版本，并拉取最新版本读取元数据。
3. 同步失败不会删除仓库配置，但会在仓库卡片和 Chart 目录中显示 warning。
4. 目录支持按仓库筛选，并搜索 Chart 名称与说明。
5. HTTP 版本按语义化版本倒序排列，无法解析为 SemVer 的版本使用字符串倒序；OCI 仅保留 SemVer Tag 并按语义化版本倒序。
6. 安装计划固定 Chart 名称、版本和包摘要，不会在后台自动升级到最新版本。
7. HTTP 仓库的成功 `index.yaml` 不设置基于时间的过期策略。启用 `persistence` 时，索引会原子写入 `${DOSH_DATA_DIR}/cache/helm-repositories/<缓存键>/index.yaml`，Dosh 重启后优先复用该文件；未启用持久化时才退化为进程内缓存。创建、修改或手动同步仓库时主动失效磁盘与内存缓存并重新拉取，不依赖页面访问触发刷新。

8. Chart 包按“仓库 + Chart 名称 + 版本”持久化到 `${DOSH_DATA_DIR}/cache/helm-charts/`。详情页打开、安装计划和安装操作会优先复用本地 `.tgz`；读取时校验请求指定的 SHA-256，摘要不匹配的缓存会被删除并重新下载。

9. Chart 详情页的“同步 Chart 包”只刷新当前最新版本的 Chart 包、README、默认 Values 和 Schema，不会重新同步仓库索引。该操作会修改共享缓存，因此需要 `update dosh.io/helm-repositories` 权限；同一仓库、同一 Chart 的跨副本同步由独立操作锁串行化。需要发现新 Chart 或新版本时，仍应使用仓库管理页的“同步”。

10. 仓库可以单独启用定时同步，默认关闭。启用后的默认间隔为 3 小时，可配置范围为 5 分钟至 7 天；最近 20 次同步结果保存在仓库目录中。多个 Dosh 副本通过控制面操作锁避免同一仓库被重复同步。同步失败后按 5 分钟起步的指数退避重试，最长不超过管理员配置的正常同步周期；成功一次后重置失败次数和退避状态。

11. 持久缓存达到 `HELM_CACHE_MAX_BYTES` 后，后台维护任务每 10 分钟检查一次容量并优先淘汰最旧的 Chart 包，再淘汰索引文件。管理员仍可在仓库管理页立即执行清理。

安装、升级、卸载和回滚在完成计划与授权校验后会立即返回 `running` 操作记录，实际 Helm 执行在服务端后台继续。前端通过 `/api/v1/helm/operations/{id}` 轮询状态，不依赖单次 HTTP 请求完成整个 Helm 生命周期，避免浏览器或网关超时取消 `--atomic` 安装。

安装和升级的操作记录同时保存计划中的资源列表。后台执行期间每 3 秒读取一次资源真实状态，并在资源处于等待或失败状态时补充最近的相关 Kubernetes 事件（例如 PVC 因没有 StorageClass 而无法绑定）。前端逐项展示 `Kind`、`namespace/name`、状态和原因，避免只显示笼统的“等待资源就绪”。

安装、升级、卸载和回滚统一展示在“Helm 操作中心”。操作中心支持按命名空间、状态和关键字筛选，展示资源级进度、错误和警告，并对运行中的操作提供取消入口。操作执行期间每 30 秒写入心跳；跨副本取消通过心跳记录传递。Dosh 重启或执行器崩溃后，超过 2 分钟没有心跳的运行中操作会被自动收敛为失败；已经提交到 Kubernetes 的资源不会自动回退。受 Helm SDK 限制，安装和升级可以中断执行上下文，卸载与回滚可能要等当前 Helm 步骤返回后才能结束。

操作锁会随操作心跳续期，避免 Helm 超时上限高于锁 TTL 时出现“旧操作仍在执行、新操作已获锁”的并发写入。

Chart 详情展示 README、默认 Values、Schema、依赖、维护者和来源链接。README、Values 和 Schema 从 Chart 包读取，不从浏览器直接请求第三方地址。

生成安装或升级计划时，服务端使用 `values.schema.json` 校验合并后的 Values。校验失败会返回字段路径和原因，并阻止进入可执行计划，不需要用户额外执行一次 Helm CLI。

计划渲染使用当前集群 Discovery 返回的 `Capabilities.APIVersions` 与 Kubernetes 版本，而不是只使用 Helm 内置默认值，避免 Chart 的版本分支在“计划”和“实际执行”之间出现差异。目标命名空间已经存在时，Dosh 还会使用当前用户身份逐项提交 `dryRun=All` 的 server-side apply，以便提前触发 ResourceQuota、LimitRange、Admission Webhook 和不可变字段等准入校验。若命名空间要由本次安装创建，或 Chart 同时创建 CRD 与其 Custom Resource，则无法在计划阶段完成对应资源的 dry-run，计划会返回明确 warning。

## 4. 安装计划与 planDigest

生成计划是只读操作，但会真实渲染 Chart、使用集群 Discovery 能力集、执行资源发现、RBAC 预检和可执行的 server-side dry-run。

计划包含：

- 目标仓库、Chart、版本、包 SHA-256；
- 目标命名空间与 Release 名称；
- 当前 Helm revision；
- 脱敏后的合并 Values；
- 脱敏后的渲染 Manifest；
- 每个资源的 API 版本、Kind、命名空间、名称、create/update/delete 动作、是否存在；
- Hook 与 CRD 数量；
- 仓库、HTTP、Hook、CRD、准入预检等 warning；
- `planDigest`。

执行安装或升级时，客户端必须回传 `planDigest`。服务端会重新下载 Chart、重新渲染并计算摘要；如果 Chart 包、Values、目标资源、当前 Release revision 或仓库配置发生变化，执行会被拒绝，必须重新审阅计划。

升级计划会比较当前 Release Manifest 与目标 Manifest，并按照 Helm 的资源身份规则（Group、Kind、命名空间、名称）列出目标清单中已不存在、实际升级会被删除的对象。删除项同样会进入 RBAC 预检，并展示在操作中心进度中。带有 `helm.sh/resource-policy=keep` 的资源不会列出为删除，计划会返回 warning。

若计划中的 create/update 目标已经存在，Dosh 会检查 Helm 所有权标签和注解。资源属于另一个 Release、完全未受 Helm 管理，或所有权元数据不完整时，计划会以冲突失败，而不是等到 Helm 执行阶段才报错。

执行请求还必须输入 Release 名称作为二次确认。

### 高级执行选项

安装、升级、卸载和回滚支持受限的 Helm 执行策略：

- 超时时间：默认 600 秒，范围 30–3600 秒。
- 历史版本上限：默认 10，范围 1–100。
- Wait、WaitForJobs、Atomic、CleanupOnFail、DisableHooks、SkipCRDs。
- 安装时可选 CreateNamespace；卸载时可选 KeepHistory。
- 关闭 Wait 时，Atomic 与 CleanupOnFail 会由前端自动关闭或由服务端拒绝，避免产生 Helm 不接受的参数组合。
- 执行策略会进入计划摘要并随操作记录持久化；计划生成后修改策略必须重新生成计划。

## 5. 安装、升级、卸载与回滚

### 安装

- 默认只允许安装到已存在且不属于 Dosh 控制面的命名空间；显式开启“自动创建命名空间”时，允许目标命名空间尚未存在，并由 Helm 在安装时创建。
- Release 名称和命名空间必须通过 Helm/Kubernetes 校验。
- 固定 Chart 版本和包摘要。
- Helm Hook 按 Helm 生命周期执行；CRD 已存在时 Helm 不会自动升级。

### 升级

- Dosh 提供两条升级路径：
  - **Chart 目录升级**：在 Chart 详情打开“安装 / 升级”，选择目标命名空间和已有 Release，并选择目标 Chart 版本。该路径使用下载后的完整 Chart 包，支持在升级时改变 Chart 版本和已打包的子 Chart。
  - **Release 详情更换 Chart 版本**：直接从已启用仓库选择目标 Chart 与版本，沿用服务端保存的当前 Release Values 作为基础，再叠加本次 Values Override。目标 Chart 名称与当前 Release 不同时会显示迁移风险提示。
  - **Release 详情增量 Values 升级**：复用 Helm Secret 中当前 revision 的 Chart，仅合并增量 Values。它适合改副本数、镜像参数等配置变更，不更换 Chart 包。
- 允许升级状态为 `deployed`、`failed` 或 `superseded` 的 Release。`failed`/`superseded` 可以直接重新升级修复，不需要先卸载；`pending-*` 仍会被拒绝，以避免与正在执行的 Helm 操作冲突。
- 同样要求计划摘要与 Release 名称确认。
- 两条升级路径都会先读取服务端保存的当前 Release Values，再深度合并本次提交的增量覆盖；计划页展示实际合并结果。Chart 目录升级因此可以更换 Chart 版本而不重置现网配置。
- Release 详情升级会提交预览时的 `currentRevision`；如果预览后 Release 已被其他操作推进，Dosh 会拒绝执行并要求重新预览。
- Release 详情更换 Chart 版本会锁定目标仓库、Chart 名称、版本、包摘要、Values、目标资源和当前 revision；执行前任何一项变化都会拒绝写入。
- 两条升级路径最终都使用 Helm 原生升级：按 Hook 生命周期执行 Hook、清理目标清单中已移除的对象，并以 Atomic / CleanupOnFail 控制失败回滚。操作中心会持续展示资源级进度。

### 卸载

- 需要 Release 名称确认。
- Helm 管理的普通资源会被删除。
- PVC、CRD、外部资源和目标清单外资源默认保留，返回 warning 提示后续人工清理。
- 卸载会清除 Helm 历史记录，不能通过 Helm rollback 恢复；操作中心会持续展示执行进度。

### 回滚

- 支持指定 revision 回滚。
- 使用 Helm 原生回滚并等待完成。
- 操作记录保存目标 revision、结果 revision 和错误信息。

### Helm test

- 使用 Helm SDK 执行声明为 `helm.sh/hook: test` 的 Hook。
- 返回每个测试 Hook 的状态、时间、组合后的 Pod 日志（最多 128 KiB）以及无 test Hook 的警告。
- 测试失败作为已完成结果返回，不会丢失 Hook 状态和日志；只有权限、Release 不存在或基础设施错误才返回 API 错误。

操作记录用于审计和排障，不替代 Kubernetes 审计日志。

## 6. 已知边界

- 当前不提供任意公开 Chart 市场搜索；仓库必须由管理员登记。
- 当前不在 UI 中执行 CRD 升级或级联删除 PVC。
- Chart 依赖必须在 Chart 包中已打包；Dosh 不会递归拉取任意依赖仓库。
- OCI 仓库当前只指向单个 Chart，不聚合 namespace 下的多个 Chart，也不读取 OCI Referrers、Cosign 签名或 SBOM 证明。
- Release 详情更换 Chart 版本要求目标 Chart 已存在于启用仓库并完成索引同步；当前 Chart 不在任何启用仓库时，只能继续使用增量 Values 升级或先登记对应仓库。
- Release 详情增量 Values 升级仍无法处理 Release Secret 中未保留子 Chart 模板的依赖型 Release；这类 Release 应通过 Chart 目录升级到完整 Chart 包，或在后续版本中使用支持依赖恢复的升级路径。
- Chart 目录与 Release 详情发起的安装、升级、卸载和回滚均使用跨副本 per-release 操作锁，并进入统一操作中心。
- 仓库凭据不会下发浏览器，因此编辑页无法再次显示原始用户名、密码、Token 或 CA。
- 明文 HTTP 仓库属于高风险配置。生产环境应使用 HTTPS 或 OCI over HTTPS、受控 CA 与最小权限仓库账号。

## 7. 排障

### `index.yaml` 同步失败

检查仓库根路径是否可通过 `GET <repository>/index.yaml` 访问；确认认证方式和 CA 正确；查看仓库卡片的最近同步消息。

### OCI 仓库同步失败

检查 `oci://` 地址是否指向单个 Chart、是否遗漏或误包含 `:tag`，以及 registry 是否允许匿名或当前 Basic/Bearer 凭据访问 `GET /v2/<repository>/tags/list`。自建 registry 使用私有 CA 时，请在仓库配置中提供 CA PEM；仅临时环境才应跳过 TLS 校验。

### `index.yaml` 中的 OCI 包无法下载

检查 `index.yaml` 的 `urls` 是否为完整的 `oci://registry/path/chart:tag`，并确认 Dosh 能访问该 registry。若 OCI registry 与仓库主机不同，Dosh 不会把仓库用户名、密码或 Token 转发给该 registry；请使用可匿名访问的公共 registry，或为该 registry 单独配置受控访问路径。

### `chart package digest mismatch`

仓库 `index.yaml` 中记录的 digest 与下载包不一致，可能由缓存、代理或供应链篡改导致。不要绕过该错误，应检查仓库发布流程和 CDN 缓存。

### `install plan changed or expired`

Chart、Values、目标资源或 Release revision 已变化。重新生成计划并再次审阅。

### `forbidden` 或权限预检失败

确认当前身份具备目标命名空间中的 Secret 与所有渲染资源权限。可使用“权限检查”页面生成对应的 `kubectl auth can-i` 命令。

### 仓库主机变更被拒绝

这是防止旧凭据被发送到新主机的保护。重新录入认证信息，或显式清除已保存认证/CA 后再保存。

## 8. 主流 Helm 管理基线对照

| 基线能力 | Dosh 当前实现 |
|---|---|
| 仓库管理 | HTTP/HTTPS `index.yaml` 与 OCI Chart 仓库；增删改、启用/停用、默认仓库、最近同步状态、可选定时同步（默认关闭、默认 3 小时） |
| 目录与检索 | 跨仓库 Chart 目录、按仓库筛选、名称/说明搜索、SemVer 倒序版本列表 |
| Chart 元数据 | README、默认 Values、values.schema.json、依赖、维护者、主页/源码、Deprecated 与摘要 |
| 安装 | 固定版本与包摘要、Values 编辑、Schema 校验、资源/CRD/Hook/RBAC 预检、名称确认、审计 |
| 升级 | Chart 目录与 Release 详情均可更换 Chart 版本；Release 详情同时支持基于当前 Chart 的增量 Values 升级；计划摘要防止执行期间漂移 |
| 历史与恢复 | revision 历史、values/manifest Diff、受控回滚、卸载 |
| 凭据与网络安全 | Basic/Bearer、自定义 CA、TLS 校验开关、HTTP 降级保护、凭据不回传浏览器 |

不属于当前基线、但可作为后续增强的能力包括：OCI 签名与证明验证、仓库级 Chart 缓存预热，以及依赖仓库的在线递归解析。
