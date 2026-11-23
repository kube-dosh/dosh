# NFS 挂载卷

Dosh 的核心配置仍保存在 Kubernetes ConfigMap/Secret 中，但 Helm 仓库的 `index.yaml` 缓存会写入这个共享卷。Dosh 重启或重建 Pod 后会优先复用 NFS 上的缓存，只有管理员同步仓库或缓存失效时才重新拉取远端索引。

## 架构

```text
Dosh Pod A ─┐
            ├─ PVC dosh-data (RWX) ─ Ganesha NFSv4 ─ hostPath /srv/dosh-nfs
Dosh Pod B ─┘
```

- NFS 服务：`dosh-storage/dosh-nfs`，单副本，固定运行在 `worker-0001` (`192.168.122.200`)。
- PV/PVC：`dosh-data`，20 GiB，`ReadWriteMany`，回收策略 `Retain`。
- Dosh 挂载点：`/var/lib/dosh`。

固定节点是当前实验环境约束，不是高可用方案。商用环境应替换为具备后端冗余的 NFS、CephFS 或云文件存储。

## 部署

```bash
kubectl apply -f deploy/nfs/nfs-server.yaml
kubectl apply -f deploy/nfs/pv-pvc.yaml
kubectl -n dosh get pvc dosh-data
```

Helm 使用已有 PVC：

```yaml
persistence:
  enabled: true
  existingClaim: dosh-data
  mountPath: /var/lib/dosh
```

启用 `persistence` 时，Helm Chart 会把 PVC 挂载到 `mountPath`，并把同一路径注入为 `DOSH_DATA_DIR`。当前 Helm 缓存的相对路径是：

```text
${DOSH_DATA_DIR}/cache/helm-repositories/<仓库缓存键>/index.yaml
${DOSH_DATA_DIR}/cache/helm-charts/<仓库缓存键>/<Chart 缓存键>/<版本缓存键>.tgz
```

仓库索引与 Chart 包都使用临时文件加原子重命名写入，文件权限为 `0640`，目录权限为 `0750`。Chart 包读取时会按调用方提供的 SHA-256 校验；详情页“同步 Chart 包”会删除当前版本的本地包并重新下载。多副本共享同一 PVC 时，一个副本完成仓库同步后，其他副本会通过索引文件修改时间与大小感知更新。Dosh 不会在重启时自动执行 ConfigMap 数据迁移，也不会把仓库凭据写入该目录。

## 验证

```bash
kubectl -n dosh get pod -l app.kubernetes.io/name=dosh -o wide
kubectl -n dosh get pvc dosh-data
kubectl -n dosh-storage get pod,sts
```

预期 PVC 为 `Bound`，NFS Pod 为 `1/1 Running`，两个 Dosh Pod 正常 Ready。

可以在 NFS Pod 中确认挂载后端：

```bash
kubectl -n dosh-storage exec dosh-nfs-0 -- sh -c 'ls -ld /export'
```

确认 Helm 仓库索引与 Chart 包已落盘：

```bash
kubectl -n dosh exec deploy/dosh -- \
  find /var/lib/dosh/cache/helm-repositories -name index.yaml -ls
kubectl -n dosh exec deploy/dosh -- \
  find /var/lib/dosh/cache/helm-charts -name '*.tgz' -ls
```

正常结果应至少包含一个非空 `index.yaml`；打开过 Chart 详情后还应出现对应 `.tgz`。重启 Dosh Pod 后再次进入 Helm Chart 目录或详情页，不应立即触发远端索引或 Chart 包下载；管理员在仓库页面执行“同步”或在 Chart 详情页执行“同步 Chart 包”时才会主动刷新。

## 恢复

PV 使用 `Retain`。误删 PVC 时，PV 和宿主机 `/srv/dosh-nfs` 数据仍保留。恢复时重新创建 PVC 并确认绑定到原 PV 即可。
