# GitHub 发布流水线

Dosh 的正式发布由 [`.github/workflows/release.yml`](../.github/workflows/release.yml) 驱动。推送
`v*` 标签或在 GitHub Actions 页面手动指定已有标签后，流水线会构建并发布 Linux 二进制、多架构
容器镜像和 Helm Chart。

## 触发方式

推荐先更新版本并提交：

```bash
# Makefile VERSION、Chart.yaml appVersion、OpenAPI、npm 版本保持一致
make version-check

git tag -a v0.1.7 -m "Dosh v0.1.7"
git push origin v0.1.7
```

也可以在 GitHub 的 **Actions → Release → Run workflow** 中填写已有标签。手动运行会 checkout
对应标签，不依赖当前默认分支的内容。

## 工作流检查项

发布流水线使用 GitHub 托管 `ubuntu-24.04` runner。`verify` 作业先执行版本一致性、Go 单测、
前端单测和 Helm Chart 校验；全部通过后，`release` 作业才获得 `contents: write` 与
`packages: write` 权限并执行以下步骤：

1. 校验标签版本与 `Makefile VERSION`、`Chart.yaml appVersion`、OpenAPI、npm 版本一致。
2. 安装前端依赖并生成 `web/dist`。
3. 交叉编译 Linux `amd64` 和 `arm64` 静态二进制。
4. 使用 Buildx 构建并推送 `linux/amd64`、`linux/arm64` 多架构镜像。
5. 执行 `helm lint`、打包 Chart，并推送到 GHCR OCI Registry；遇到 GHCR 短暂一致性导致的
   `failed to perform "Tag" ... not found` 时会以 10s/20s 退避重试，最多 3 次。
6. 创建或更新 GitHub Release，上传二进制、压缩包、Chart 和 SHA256 校验和。

工作流使用仓库自动生成的 `GITHUB_TOKEN`，需要以下权限：

- `contents: write`：创建 Release 和上传附件；
- `packages: write`：推送容器镜像与 Helm OCI Chart。

如果组织的 Package 策略限制了 `GITHUB_TOKEN`，需要在仓库设置中允许 Actions 写入 Packages，
或改用具备 `write:packages` 权限的 Personal Access Token。

## 发布产物

| 类型 | 位置 | 命名 |
|---|---|---|
| Linux x86-64 二进制 | GitHub Release 附件 | `dosh_<version>_linux_amd64` |
| Linux ARM64 二进制 | GitHub Release 附件 | `dosh_<version>_linux_aarch64` |
| Linux x86-64 压缩包 | GitHub Release 附件 | `dosh_<version>_linux_amd64.tar.gz` |
| Linux ARM64 压缩包 | GitHub Release 附件 | `dosh_<version>_linux_aarch64.tar.gz` |
| 校验和 | GitHub Release 附件 | `SHA256SUMS` |
| 镜像引用 | GitHub Release 附件 | `container-image.txt` |
| 容器镜像 | GHCR | `ghcr.io/kube-dosh/dosh:<version>` |
| 容器镜像 | GHCR | `ghcr.io/kube-dosh/dosh:latest`（稳定版） |
| Helm Chart 文件 | GitHub Release 附件 | `dosh-<chart-version>.tgz` |
| Helm OCI Chart | GHCR | `oci://ghcr.io/kube-dosh/charts/dosh:<chart-version>` |

预发布版本（版本号包含 `-`）只发布版本标签，不更新 `latest`。

## 使用发布产物

### 容器镜像

```bash
docker pull ghcr.io/kube-dosh/dosh:0.1.7
```

GHCR 镜像公开前需要在 GitHub Package 设置中将可见性调整为 Public。

### Helm Chart

```bash
helm install dosh oci://ghcr.io/kube-dosh/charts/dosh \
  --version 0.1.9 \
  --namespace dosh \
  --create-namespace
```

也可以下载 Release 中的 `dosh-0.1.9.tgz` 后直接安装：

```bash
helm install dosh ./dosh-0.1.9.tgz --namespace dosh --create-namespace
```

### 二进制

```bash
sha256sum -c SHA256SUMS
tar -xzf dosh_0.1.7_linux_amd64.tar.gz
./dosh
```

生产环境应使用 `DOSH_TLS_ENABLED=true` 并通过 Secret 挂载证书，不要使用默认明文 HTTP 配置。

## 本地验证

不连接 GHCR 和 GitHub Releases 时，可以只生成并校验发布产物：

```bash
RELEASE_TAG=v0.1.7 SKIP_PUBLISH=1 bash scripts/release-github.sh
```

也可以分阶段执行：

```bash
RELEASE_TAG=v0.1.7 SKIP_WEB_BUILD=1 RELEASE_STAGE=artifacts bash scripts/release-github.sh
RELEASE_TAG=v0.1.7 RELEASE_STAGE=image bash scripts/release-github.sh
RELEASE_TAG=v0.1.7 RELEASE_STAGE=publish bash scripts/release-github.sh
```

`RELEASE_STAGE=image` 需要 Docker Buildx、`GH_TOKEN` 和 `GITHUB_ACTOR`；
`RELEASE_STAGE=publish` 需要 `gh`、Helm 和已有 `dist/` 产物。

## 验证发布结果

```bash
gh release view v0.1.7
docker buildx imagetools inspect ghcr.io/kube-dosh/dosh:0.1.7
helm show chart oci://ghcr.io/kube-dosh/charts/dosh --version 0.1.9
```
