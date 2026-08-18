# ForgeFlow Incident Review

- Incident ID / severity:
- Start, detection, mitigation, recovery times (UTC):
- Incident commander / responders:
- Affected environment, tenants and Run IDs（禁止粘贴任务正文或 Secret）:
- Customer impact:

## Timeline

按 UTC 顺序记录告警、人工判断、变更、回滚和恢复证据。

## Detection and response

- 哪个指标/日志/用户报告发现问题？
- 告警是否及时、是否包含正确 Runbook？
- 哪些安全边界和预算生效或失效？
- 是否暂停 Worker、撤销 Session/Key、隔离主机或停止新 Run？

## Root cause and contributing factors

区分直接技术原因、流程因素和潜在条件，不归咎个人。说明为何测试、Eval、Promotion 或监控没有提前阻止。

## Data and security assessment

- 是否存在跨租户访问、Secret 暴露、仓库/Artifact/审计篡改？
- 受影响数据范围、保留期限和通知义务是什么？
- 证据如何保存，谁可以访问？

## Recovery validation

- 数据库/Artifact 完整性检查：
- Schema 和 Release manifest：
- 关键 E2E、Eval、告警测试：
- 撤销的临时权限和 Secret：

## Actions

| Action | Owner | Due date | Priority | Verification |
|---|---|---|---|---|

复盘完成前不得关闭 P0/P1；所有临时绕过必须有删除日期和验证人。
