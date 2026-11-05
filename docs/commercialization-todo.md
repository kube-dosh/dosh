# Dosh 商用化必做项 TODO

状态基线：2026-11-05，候选 `0.1.5-rc-token-20261105T012750Z@sha256:c114113009ddad67313f37c288fe4cc6f7d13848de01d22dd68499146744ea2b`。
本清单是发布阻断项，不是功能建议。任一项未关闭时，不得升级生产、不得输出“完整商用放行”结论。

状态标记：`[ ]` 未完成，`[~]` 进行中，`[x]` 已通过门禁。

## 当前阻断总览

- [ ] `production-siem-retention-alerting`：生产 SIEM 投递、租户隔离、保留、告警和访问审批。
- [ ] `edge-safari-real`：真实 Microsoft Edge 与 macOS/iOS Safari 验收。
- [ ] `github-actions-branch-protection`：真实 GitHub Actions 全绿、artifact 留存和 `main` 分支保护。
- [ ] `offsite-worm-retention`：离机 WORM 证据留存和访问审批。
- [ ] `production-change-authorization`：正式变更单、维护窗口、回滚责任人和独立授权；必须 `passed`，不能风险接受。

## 1. 生产 SIEM

- [ ] 提供 SIEM 测试租户、HTTPS 接收地址、索引/租户名称和允许的投递协议。
- [ ] 通过安全渠道提供鉴权材料：本地 Secret 名称、临时凭证获取方式或安全的 Secret 注入流程；禁止在聊天、文档或证据中写入 Token/密码。
- [ ] 提供日志保留期限、检索权限、告警规则、告警接收人和访问审批人。
- [ ] 如使用私 CA，提供 CA 证书的 Secret/ConfigMap 名称；公共 CA 不需要。
- [ ] 验证成功投递、认证失败、依赖暂时不可用、恢复和 SIGTERM 排空。
- [ ] 归档 SIEM 查询结果、事件 ID、`requestId` 关联、保留策略、告警触发/恢复记录和访问审批。
- [ ] 指定独立 `owner`、`approver`，并回填 gate 证据及有效期。

完成标准：生产日志可在 SIEM 持久检索；租户和访问权限隔离；保留、告警、失败恢复和审批证据齐全。

## 2. Edge/Safari 实机

- [ ] 提供 Windows 实机或受控桌面，安装真实 Microsoft Edge 最近两个主版本。
- [ ] 提供 macOS 实机或受控桌面，安装 Safari 最近两个主版本。
- [ ] 提供 iOS/iPadOS 实机或企业设备云中的 Safari，最近两个主版本。
- [ ] 提供候选地址的访问方式、测试账号、设备清单、OS/浏览器版本和测试时间窗。
- [ ] 执行登录、总览、节点、工作负载、Pod 日志、Secret、主题、键盘和无障碍关键路径。
- [ ] 归档截图/录屏、测试结果、版本信息以及控制台/网络错误日志。
- [ ] 指定独立 `owner`、`approver`；WebKit 容器结果不能替代该项。

完成标准：真实 Edge 与 macOS/iOS Safari 均访问同一候选 digest，无阻断性兼容问题，证据可复核。

## 3. GitHub Actions 与分支保护

- [ ] 提供 GitHub 组织/仓库 URL、默认分支和 Actions 配置权限。
- [ ] 确定使用 GitHub 托管 Runner 还是自托管 Runner；自托管需具备镜像仓库和测试环境网络。
- [ ] 通过 GitHub Secret 或临时授权提供 Registry/测试环境凭据，不共享长期 Token。
- [ ] 真实运行 `backend`、`frontend`、`e2e`、`performance`、`helm`、`security`、`policy`、`release-summary` 八个 job。
- [ ] 归档 run URL/ID、job 日志、artifact 名称/保留期限和 `release-summary` 结果。
- [ ] 启用 `main` 分支保护或 Ruleset，要求 PR、必需状态检查、禁止直推，并提供配置导出。
- [ ] 指定独立 `owner`、`approver`，并回填 gate 证据及有效期。

完成标准：同一提交在真实 GitHub Actions 全绿，artifact 可按保留策略读取，`main` 保护已实际生效。

## 4. 离机 WORM 留存

- [ ] 提供企业批准的离机对象存储 Endpoint、Bucket、Region 和 Prefix。
- [ ] 启用 Object Lock、合规模式或等价不可变策略，并确定保留期限。
- [ ] 确认版本控制、服务端加密、MFA Delete/Legal Hold、访问日志和读取审批。
- [ ] 通过临时角色或专用写入凭证提供上传权限；禁止提供长期主账号密钥。
- [ ] 上传 v2.47 主证据归档及最终门禁归档，记录对象 Key、ETag、SHA-256 和上传时间。
- [ ] 验证不可删除/不可覆盖，并完成受控读取和访问审批。
- [ ] 指定独立 `owner`、`approver`，并回填 gate 证据及有效期。

完成标准：证据位于生产物理主机之外、受 WORM 保护、可按审批检索，且 manifest 摘要可校验。

## 5. 生产变更授权

- [ ] 提供正式变更单号、变更负责人、独立审批人和明确授权文件。
- [ ] 提供 RFC3339 维护窗口开始/结束时间、发布目标和回滚负责人。
- [ ] 提供回滚方案、回滚联系人和发布失败处置流程。
- [ ] 在生产变更前生成只读 baseline，并确认与批准版本一致。
- [ ] 使用 `production-change-authorization` gate，状态必须为 `passed`；禁止 `risk-accepted`。
- [ ] 生成 release plan，执行生产升级、发布后检查和回滚演练/验证。
- [ ] 归档授权、baseline、plan、执行日志、发布后检查和回滚证据。

完成标准：变更票、窗口、授权人、发布和回滚证据均绑定同一候选 digest；`make release-gate-check` 返回 `0`。

## 门禁证据格式

```json
{
  "id": "gate-id",
  "status": "passed",
  "owner": "责任人",
  "approver": "独立审批人",
  "verifiedAt": "2026-11-10T10:00:00+08:00",
  "expiresAt": "2027-11-10T10:00:00+08:00",
  "evidence": [
    {
      "description": "证据说明",
      "path": "/var/lib/dosh/evidence/...",
      "sha256": "sha256:<64位小写十六进制>"
    }
  ]
}
```

- `owner` 与 `approver` 必须不同。
- `expiresAt` 必须晚于 `verifiedAt`，且评审时不能过期。
- 证据必须位于 `/var/lib/dosh` 下或使用绝对 HTTPS URL；本地文件必须记录 SHA-256。
- 可选 `risk-accepted` 仅适用于前四项，且必须提供风险接受文件、缓释措施、回滚计划和有效期。

## 最终放行门禁

- [ ] `make release-gate-check` 返回 `0`。
- [ ] `make report`/release report 记录所有门禁、证据哈希、审批人和阻断项为零。
- [ ] 生产 baseline、release plan、升级、发布后检查和回滚全部完成。
- [ ] 证据完成离机 WORM 留存并可按审批读取。
- [ ] 更新发布记录和运维手册，但不得继承未绑定当前 digest 的历史结论。

## 当前环境预览说明

隔离预览部署只用于查看 UI 和功能效果，不代表生产发布，也不关闭任何外部门禁。生产 `dosh` 必须保持当前已记录 baseline，直到第 5 项变更授权通过。

- 预览 Release：`dosh-preview/dosh-preview`，revision `1`，1/1 Ready。
- NGINX 预览地址：`https://192.168.1.66:32630` 或 `https://192.168.122.1:32630`（自签证书，浏览器需确认继续访问）。
- 集群直连地址：`https://192.168.122.100:32630` 或 `https://192.168.122.200:32630`。
- 登录模式：Kubernetes Token；临时预览管理员 Token 文件为 `/tmp/dosh-preview-admin-token`（8 小时有效，权限 `0600`）。
- 重新生成预览 Token：`kubectl -n dosh-preview create token dosh-preview-admin --duration=8h`。
- 生产 `dosh` 未升级：App `0.1.2` / Chart `0.1.4` / revision `12` / 2/2 Ready / NodePort `32626`。
