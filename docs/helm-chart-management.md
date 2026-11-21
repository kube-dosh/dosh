# Helm Chart 仓库与受控安装

Dosh 的 Helm 能力分为两条路径：

1. **已安装 Release 运维**：浏览 Helm Secret 中的 Release、Values、渲染清单、历史版本和 Diff，并执行受控回滚或增量 Values 升级。
2. **Chart 仓库与安装**：管理员登记受控 Chart 仓库，用户从固定目录选择 Chart 和版本，先审阅安装计划，再执行安装或升级。

本文重点说明第二条路径。它不是任意 Chart 市场：Dosh 不抓取任意 URL，也不会让浏览器直接访问远程仓库。

## 1. 安全模型

### 受控仓库

- Chart 仓库只能由具备 `create dosh.io/helm-repositories` 权限的身份登记。
- 仓库必须提供标准 `index.yaml`，支持 HTTPS 与显式启用的 HTTP。
- 生产环境应使用 HTTPS。明文 HTTP 仅允许管理员在隔离网络使用，并会在目录和计划中显示风险警告。
- Chart 包 URL 必须与仓库保持同一主机；HTTPS 仓库禁止降级到 HTTP。
- URL 重定向也限制在同一主机，最多 5 次。
- 仓库地址不得包含用户名、密码、查询参数或 Fragment，避免凭据进入日志和浏览器历史。

### 凭据与 CA

- 支持匿名、用户名/密码、Bearer Token 三种认证方式。
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

1. 管理员登记仓库后，Dosh 立即尝试同步 `index.yaml`。
2. 同步失败不会删除仓库配置，但会在仓库卡片和 Chart 目录中显示 warning。
3. 目录支持按仓库筛选，并搜索 Chart 名称与说明。
4. 版本按语义化版本倒序排列；无法解析为 SemVer 的版本使用字符串倒序。
5. 安装计划固定 Chart 名称、版本和包摘要，不会在后台自动升级到最新版本。

Chart 详情展示 README、默认 Values、Schema、依赖、维护者和来源链接。README、Values 和 Schema 从 Chart 包读取，不从浏览器直接请求第三方地址。

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

- 只允许升级状态为 `deployed` 的 Release。
- 同样要求计划摘要与 Release 名称确认。
- Values 以本次提交的完整 Values 为准；计划页会展示服务端实际合并结果。

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
- 同一 Release 并发升级目前依赖 Helm 自身的存储锁与控制面并发行为；后续版本会增加 per-release 操作锁。
- 仓库凭据不会下发浏览器，因此编辑页无法再次显示原始用户名、密码、Token 或 CA。
- HTTP 仓库属于高风险配置。生产环境应使用 HTTPS、受控 CA 与最小权限仓库账号。

## 7. 排障

### `index.yaml` 同步失败

检查仓库根路径是否可通过 `GET <repository>/index.yaml` 访问；确认认证方式和 CA 正确；查看仓库卡片的最近同步消息。

### `chart package digest mismatch`

仓库 `index.yaml` 中记录的 digest 与下载包不一致，可能由缓存、代理或供应链篡改导致。不要绕过该错误，应检查仓库发布流程和 CDN 缓存。

### `install plan changed or expired`

Chart、Values、目标资源或 Release revision 已变化。重新生成计划并再次审阅。

### `forbidden` 或权限预检失败

确认当前身份具备目标命名空间中的 Secret 与所有渲染资源权限。可使用“权限检查”页面生成对应的 `kubectl auth can-i` 命令。

### 仓库主机变更被拒绝

这是防止旧凭据被发送到新主机的保护。重新录入认证信息，或显式清除已保存认证/CA 后再保存。
