# VPA 垂直治理

Dosh 通过 `autoscaling.k8s.io/v1` 管理 VerticalPodAutoscaler（VPA），用于查看资源建议、维护更新策略与容器级资源上下界。该能力依赖集群已安装 VPA CRD 和 VPA 控制器；Dosh 不负责自动安装控制器。

## 1. 功能范围

- 按命名空间列出 VPA，展示目标类型/名称、更新模式、minReplicas、容器策略、推荐值、条件与推荐时间。
- 创建和编辑 VPA，支持 `Off`、`Initial`、`Recreate`、`Auto`、`InPlaceOrRecreate` 更新模式。
- 目标支持 `apps/v1` 的 Deployment、StatefulSet、DaemonSet、ReplicaSet。
- 容器策略支持 `Off`/`Auto`、`RequestsAndLimits`/`RequestsOnly`、CPU/内存 `minAllowed`/`maxAllowed`，以及受控资源 CPU/内存。
- 删除必须携带 `resourceVersion`，防止删除用户未看到的更新版本。

## 2. 写入流程

创建或编辑时 Dosh 执行两步请求：

1. 使用 `dryRun=All` 调用 API Server，提前触发 CRD schema、准入 Webhook 和字段校验。
2. Dry Run 成功后执行真实 Create/Update。

编辑使用列表返回的 `resourceVersion` 做乐观并发控制；如果 VPA 已被其他客户端修改，更新返回 `409`，页面应刷新后重新填写。创建同样会检查同名对象冲突。

Dosh 为创建的 VPA 添加 `app.kubernetes.io/managed-by=dosh` 标签，但删除不依赖该标签，也不会批量删除非 Dosh 创建的 VPA。

## 3. 推荐状态

Dosh 将 VPA 状态归一为：

- `Ready`：已返回至少一个容器推荐值。
- `Pending`：VPA 正在加载历史或尚未生成推荐值。
- `Degraded`：`RecommendationProvided=False` 等明确失败条件。

推荐字段包括 `lowerBound`、`target`、`upperBound` 与 `uncappedTarget`。VPA 不直接暴露“推荐生成时间”；页面展示的是 `RecommendationProvided` 条件的最近转换时间。

## 4. CRD 缺失与 RBAC

如果集群未安装 `autoscaling.k8s.io/v1` CRD，列表、创建、编辑和删除返回 `501 Unsupported`，并显示中文安装提示。这不影响 HPA 与 PDB 页面。

Dosh 需要以下 Kubernetes RBAC：

```yaml
- apiGroups: ["autoscaling.k8s.io"]
  resources: ["verticalpodautoscalers"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

用户侧还需要相应 SubjectAccessReview 权限；Dosh 会在读取和写入前校验，并将成功、失败与拒绝写入审计日志。

## 5. 运维建议

- 建议先使用 `Off` 观察推荐值，再切换到 `Initial` 或 `Auto`。
- `Auto` 会驱逐并重建 Pod 以应用资源变更；有状态工作负载应评估中断风险并配合 PDB。
- `InPlaceOrRecreate` 需要较新的 VPA 控制器与 Kubernetes 版本支持；不支持时由 API Server 拒绝。
- VPA 与 HPA 同时控制 CPU/内存可能相互冲突。常见组合是 HPA 管副本数，VPA 仅在 `Off` 模式提供建议。
- 推荐值依赖 Metrics Server、历史采样和控制器健康状态；长期 `Pending` 时应检查 VPA recommender、Metrics API 与目标工作负载是否存在。

## 6. 故障排查

| 现象 | 处理 |
|---|---|
| 页面提示未安装 CRD | 安装 VPA CRD/控制器，确认 API 组为 `autoscaling.k8s.io/v1` |
| 创建失败 409 | 同名 VPA 已存在，改为编辑或使用新名称 |
| 编辑失败 409 | 刷新页面，基于最新 `resourceVersion` 重新提交 |
| 长期 Pending | 检查 Metrics Server、VPA recommender 日志、目标是否持续运行 |
| Degraded | 展开条件查看 reason/message，重点检查 `RecommendationProvided=False` |
| 403 | 检查 Role/ClusterRole 与 SubjectAccessReview 结果 |
