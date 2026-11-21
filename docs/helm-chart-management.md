# Helm Chart 仓库与受控安装

Dosh 的 Helm 能力分为两条路径：

1. **已安装 Release 运维**：浏览 Helm Secret 中的 Release、Values、渲染清单、历史版本和 Diff，并执行受控回滚或增量 Values 升级。
2. **Chart 仓库与安装**：管理员登记受控 Chart 仓库，用户从固定目录选择 Chart 和版本，先审阅安装计划，再执行安装或升级。

本文重点说明第二条路径。它不是任意 Chart 市场：Dosh 不抓取任意 URL，也不会让浏览器直接访问远程仓库。

## 1. 安全模型

### 受控仓库

- Chart 仓库只能由具备 `create dosh.io/helm-repositories` 权限的身份登记。
- 支持两类仓库：提供标准 `index.yaml` 的 HTTP/HTTPS 仓库，以及指向单个 Chart 的 `oci://` 仓库。
- OCI 地址必须指向一个 Chart 仓库路径，例如 `oci://registry.example.com/team/charts/nginx`。地址不能带 `:tag`，版本由 OCI Tag 表示。
- 一个 OCI 仓库配置只对应一个 Chart；Dosh 不会把整个 OCI namespace 或 registry 下的多个 Chart 自动聚合到目录中。
- 生产环境应使用 HTTPS。明文 HTTP 仅允许管理员在隔离网络使用，并会在目录和计划中显示风险警告。
- 对于 HTTP/HTTPS 仓库，Chart 包 URL 必须与仓库保持同一主机；HTTPS 仓库禁止降级到 HTTP。
- HTTP/HTTPS 仓库的 URL 重定向也限制在同一主机，最多 5 次；OCI registry 重定向必须继续使用 HTTPS。
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
| `dosh-helm-chart-operations` | ConfigMap | 最近 100 条安装、升级、卸载和回滚操作记录 |

仓库目录使用 Kubernetes `resourceVersion` 做乐观并发控制。多个管理员同时编辑时，过期页面会收到冲突而不是覆盖最新配置。

## 2. 权限要求

| 能力 | Kubernetes RBAC / Dosh 权限 |
|---|---|
| 浏览仓库与 Chart | `list dosh.io/helm-repositories` |
| 新增、修改、删除、同步仓库 | 对应 `create` / `update` / `delete dosh.io/helm-repositories` |
| 生成计划 | `list secrets`，以及目标命名空间 `create`、`update secrets` |
| 安装 / 升级 | 目标命名空间 `create`、`update secrets`，以及计划中所有资源的 `create` 或 `update`/`patch` |
| 卸载 | `delete secrets`、`update secrets` 与目标资源删除权限 |
| 回滚 | `update secrets` 与目标资源更新权限 |

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

Chart 详情展示 README、默认 Values、Schema、依赖、维护者和来源链接。README、Values 和 Schema 从 Chart 包读取，不从浏览器直接请求第三方地址。

生成安装或升级计划时，服务端使用 `values.schema.json` 校验合并后的 Values。校验失败会返回字段路径和原因，并阻止进入可执行计划，不需要用户额外执行一次 Helm CLI。

## 4. 安装计划与 planDigest

生成计划是只读操作，但会真实渲染 Chart 并执行资源发现和 RBAC 预检。

计划包含：

- 目标仓库、Chart、版本、包 SHA-256；
- 目标命名空间与 Release 名称；
- 当前 Helm revision；
- 脱敏后的合并 Values；
- 脱敏后的渲染 Manifest；
- 每个资源的 API 版本、Kind、命名空间、名称、create/update 动作、是否存在；
- Hook 与 CRD 数量；
- 仓库、HTTP、Hook、CRD 等 warning；
- `planDigest`。

执行安装或升级时，客户端必须回传 `planDigest`。服务端会重新下载 Chart、重新渲染并计算摘要；如果 Chart 包、Values、目标资源、当前 Release revision 或仓库配置发生变化，执行会被拒绝，必须重新审阅计划。

执行请求还必须输入 Release 名称作为二次确认。

## 5. 安装、升级、卸载与回滚

### 安装

- 仅允许安装到已存在且不属于 Dosh 控制面的命名空间。
- Release 名称和命名空间必须通过 Helm/Kubernetes 校验。
- 固定 Chart 版本和包摘要。
- Helm Hook 按 Helm 生命周期执行；CRD 已存在时 Helm 不会自动升级。

### 升级

- Dosh 提供两条升级路径：
  - **Chart 目录升级**：在 Chart 详情打开“安装 / 升级”，选择目标命名空间和已有 Release，并选择目标 Chart 版本。该路径使用下载后的完整 Chart 包，支持在升级时改变 Chart 版本和已打包的子 Chart。
  - **Release 详情增量 Values 升级**：复用 Helm Secret 中当前 revision 的 Chart，仅合并增量 Values。它适合改副本数、镜像参数等配置变更，不用于更换 Chart 版本。
- 两条路径都只允许升级状态为 `deployed` 的 Release。
- 同样要求计划摘要与 Release 名称确认。
- Chart 目录升级以本次提交的完整 Values 为准；Release 详情升级从当前 Release 的 Values 深度合并增量值，计划页会展示服务端实际合并结果。

### 卸载

- 需要 Release 名称确认。
- Helm 管理的普通资源会被删除。
- PVC、CRD、外部资源和目标清单外资源默认保留，返回 warning 提示后续人工清理。

### 回滚

- 支持指定 revision 回滚。
- 使用 Helm 原生回滚并等待完成。
- 操作记录保存目标 revision、结果 revision 和错误信息。

操作记录用于审计和排障，不替代 Kubernetes 审计日志。

## 6. 已知边界

- 当前不提供任意公开 Chart 市场搜索；仓库必须由管理员登记。
- 当前不在 UI 中执行 CRD 升级或级联删除 PVC。
- Chart 依赖必须在 Chart 包中已打包；Dosh 不会递归拉取任意依赖仓库。
- OCI 仓库当前只指向单个 Chart，不聚合 namespace 下的多个 Chart，也不读取 OCI Referrers、Cosign 签名或 SBOM 证明。
- Release 详情中的增量 Values 升级无法更换 Chart 版本；需要更换版本时请从 Chart 目录选择目标版本并发起升级。Helm Secret 不保留子 Chart 模板，Dosh 不会在 Release 详情路径中伪造子 Chart。
- 同一 Release 并发升级目前依赖 Helm 自身的存储锁与控制面并发行为；后续版本会增加 per-release 操作锁。
- 仓库凭据不会下发浏览器，因此编辑页无法再次显示原始用户名、密码、Token 或 CA。
- 明文 HTTP 仓库属于高风险配置。生产环境应使用 HTTPS 或 OCI over HTTPS、受控 CA 与最小权限仓库账号。

## 7. 排障

### `index.yaml` 同步失败

检查仓库根路径是否可通过 `GET <repository>/index.yaml` 访问；确认认证方式和 CA 正确；查看仓库卡片的最近同步消息。

### OCI 仓库同步失败

检查 `oci://` 地址是否指向单个 Chart、是否遗漏或误包含 `:tag`，以及 registry 是否允许匿名或当前 Basic/Bearer 凭据访问 `GET /v2/<repository>/tags/list`。自建 registry 使用私有 CA 时，请在仓库配置中提供 CA PEM；仅临时环境才应跳过 TLS 校验。

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
| 仓库管理 | HTTP/HTTPS `index.yaml` 与 OCI Chart 仓库；增删改、启用/停用、默认仓库、最近同步状态 |
| 目录与检索 | 跨仓库 Chart 目录、按仓库筛选、名称/说明搜索、SemVer 倒序版本列表 |
| Chart 元数据 | README、默认 Values、values.schema.json、依赖、维护者、主页/源码、Deprecated 与摘要 |
| 安装 | 固定版本与包摘要、Values 编辑、Schema 校验、资源/CRD/Hook/RBAC 预检、名称确认、审计 |
| 升级 | Chart 目录升级可更换 Chart 版本；Release 详情支持基于当前 Chart 的增量 Values 升级；计划摘要防止执行期间漂移 |
| 历史与恢复 | revision 历史、values/manifest Diff、受控回滚、卸载 |
| 凭据与网络安全 | Basic/Bearer、自定义 CA、TLS 校验开关、HTTP 降级保护、凭据不回传浏览器 |

不属于当前基线、但可作为后续增强的能力包括：OCI 签名与证明验证、仓库级 Chart 缓存预热、Release 详情直接选择新 Chart 版本、并行 Release 操作锁，以及依赖仓库的在线递归解析。
