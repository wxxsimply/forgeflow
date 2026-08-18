# 阶段 6：Reviewer、Security 与 Judge

阶段 6 已把真实测试后的质量与安全门禁接入开发 Graph。该阶段只完成控制面能力，不增加 HTTP API、登录、数据库或部署。

## 执行链路

```text
run_test
   -> parallel-assessments
      -> reviewer（独立快照）
      -> security（独立快照 + 确定性新增行扫描）
   -> assessment-join
   -> judge
      -> pass
      -> repair（受 MaxRepairs 与 MaxIterations 限制）
      -> human_review（绑定 InputSHA256、WorkspaceID、judge/v1）
      -> fail
```

Reviewer 与 Security 只接收任务、批准计划、`AGENTS.md`、最终 Diff 和真实测试证据。上下文类型没有 Developer 的 Implementation、私有推理或未批准文件字段。

## 确定性门禁

Judge 不调用模型。它按程序规则检查：

1. 评审分支是否成功并返回严格结果；
2. 测试退出码是否为 0；
3. 是否存在确认的 blocking review finding；
4. 是否存在确认的 high/critical security finding；
5. 是否修改保护文件或超出批准计划；
6. 文件数、Diff 字节、Diff 行数和运行预算是否超限；
7. 未确认的高影响问题是否已经由人工批准当前证据摘要。

安全 Agent 的模型结果会与确定性扫描结果合并。确定性扫描只检查 Diff 新增行，当前覆盖私钥、AWS Key、硬编码凭据、Shell 命令、0777 权限和 `0.0.0.0/0`/`::/0`。模型返回空 findings 也不能删除这些结果。

## Repair 约束

Repair 复用 Developer，但只额外提供当前 Diff、失败的真实测试证据、阻断 Review finding、高风险 Security finding 和剩余预算。批准文件范围不变；默认最多一次 Repair。

## 验收

自动化测试覆盖：

- 真实 `go test` 失败后一次 Repair；
- 阻断 Review 被 Judge 拦截；
- 模型判定通过时，确定性安全漏洞仍被拦截；
- Reviewer/Security 分支失败或超时形成明确失败结果；
- Reviewer 上下文不包含 Developer 私有上下文；
- 未确认高风险进入人工审批，且篡改摘要后审批不可复用；
- Repair 次数耗尽后失败；
- 原 Git 仓库保持不变。
