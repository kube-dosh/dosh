# Codeberg 发布流水线

Dosh 的正式发布由 [`.forgejo/workflows/release.yml`](../.forgejo/workflows/release.yml) 驱动。推送
`v*` 标签或从已有标签手动触发后，流水线会构建并发布 Linux 双架构产物、多架构容器镜像和 Helm
Chart。

## 触发方式

推送标签：

```bash
git tag -a v0.1.5 -m "Dosh v0.1.5"
git push origin v0.1.5
```

也可以在 Codeberg 的 **Actions → Release → Run workflow** 中填写已有标签，例如 `v0.1.5`。手动
运行必须选择对应标签作为工作流 ref，脚本会校验输入标签与当前 checkout 的标签一致。

## 运行前置条件

1. 在仓库 **Settings → Units → Overview** 中启用 Actions。
2. 注册带有 `docker` 标签的 Forgejo Runner。工作流使用
   `golang:1.26.10-bookworm` 作为任务容器，不需要 runner 暴露 Docker daemon。
3. Runner 需要能够访问 `nodejs.org`、`get.helm.sh`、GitHub Releases、`gcr.io` 和
   `codeberg.org`。
4. 发布版本的 `Makefile VERSION`、`Chart.yaml appVersion` 和 Git 标签必须一致。Chart 自身的
   `version` 可以独立递增。

工作流默认使用 Forgejo 为当前运行自动创建的 `FORGEJO_TOKEN` 创建 Release、上传附件和发布包。
如果实例策略不允许自动 Token 写 Package，可配置仓库 Secret `CODEBERG_TOKEN`，至少授予：

- 仓库写权限；
- Package 写权限。

## 发布产物

| 类型 | 位置 | 命名 |
|---|---|---|
| Linux x86-64 二进制 | Codeberg Release 附件 | `dosh_<version>_linux_amd64` |
| Linux ARM64 二进制 | Codeberg Release 附件 | `dosh_<version>_linux_aarch64` |
| Linux x86-64 压缩包 | Codeberg Release 附件 | `dosh_<version>_linux_amd64.tar.gz` |
| Linux ARM64 压缩包 | Codeberg Release 附件 | `dosh_<version>_linux_aarch64.tar.gz` |
| 校验和 | Codeberg Release 附件 | `SHA256SUMS` |
| 镜像引用 | Codeberg Release 附件 | `container-image.txt` |
| 容器镜像 | Codeberg Container Registry | `codeberg.org/aido/dosh:<version>` |
| 容器镜像 | Codeberg Container Registry | `codeberg.org/aido/dosh:latest`（稳定版） |
| Helm Chart 文件 | Codeberg Release 附件 | `dosh-<chart-version>.tgz` |
| Helm Chart 仓库 | Codeberg Helm Registry | `https://codeberg.org/api/packages/aido/helm` |

容器镜像使用 `ko` 直接构建并推送，不依赖 Docker daemon。镜像包含：

- `linux/amd64`
- `linux/arm64`

基础镜像固定为 `gcr.io/distroless/static-debian12:nonroot`，运行用户为 `65532:65532`。预发布版本
（版本号包含 `-`）只发布版本标签，不更新 `latest`。

## 使用发布产物

### 容器镜像

```bash
docker pull codeberg.org/aido/dosh:0.1.5
```

### Helm Chart

```bash
helm repo add dosh https://codeberg.org/api/packages/aido/helm
helm repo update
helm install dosh dosh/dosh --version 0.1.7
```

也可以在 Release 页面下载 `dosh-0.1.7.tgz` 后直接安装：

```bash
helm install dosh ./dosh-0.1.7.tgz
```

### 二进制

```bash
sha256sum -c SHA256SUMS
tar -xzf dosh_0.1.5_linux_amd64.tar.gz
./dosh
```

生产环境应使用 `DOSH_TLS_ENABLED=true` 并通过 Secret 挂载证书，不要使用默认明文 HTTP 配置。

## 本地验证

不连接 Codeberg Registry 时，可只生成并检查发布产物：

```bash
RELEASE_TAG=v0.1.5 SKIP_PUBLISH=1 bash scripts/release-codeberg.sh
```

产物位于 `dist/`。该模式不会登录 Registry、创建 Release 或上传 Helm Chart。

完整发布脚本还会执行：

- `scripts/check-version.sh`；
- 前端生产构建；
- Linux `amd64`、`arm64` 交叉编译；
- ELF 静态链接产物打包；
- `helm lint` 和 `helm package`；
- 双架构镜像推送；
- Release 附件上传；
- Helm Registry 发布。

## 重跑和版本规则

- 同一标签重跑会更新 Release 说明，并替换同名 Release 附件。
- 容器版本标签和 `latest` 会由新构建覆盖。
- Helm Registry 对已存在的相同 Chart 版本返回 `409` 时，流水线将其视为“已经发布”；需要重新
  发布内容时必须递增 `Chart.yaml version`，不要覆盖已发布的版本号。

## 验证发布结果

```bash
curl -fsS https://codeberg.org/api/v1/repos/aido/dosh/releases/tags/v0.1.5

docker buildx imagetools inspect codeberg.org/aido/dosh:0.1.5

helm repo add dosh https://codeberg.org/api/packages/aido/helm
helm repo update
helm search repo dosh/dosh --versions
```

