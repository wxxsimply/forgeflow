# 阶段 10：Observability 与 Eval

## 已实现范围

- API、Run、Graph Node、Model 和 Tool 的 OpenTelemetry span；HTTP 支持 W3C `traceparent` 传播，业务 `traceId` 作为关联属性保留。
- OTLP HTTP Exporter 为可选配置。未配置 Collector 时使用 no-op Provider，测试和本地开发不会因观测服务缺失而失败。
- `/metrics` 暴露 Prometheus 文本格式的低基数指标，覆盖 HTTP/429、Run 终态与首次通过、恢复、修复、预算耗尽、节点、模型 Token/成本、工具策略结果、审批等待、Queue 深度/租约事件及认证结果。
- 固定 `software/v1` 数据集共 30 Case：8 功能、6 Bug、4 测试、4 安全、3 重构、2 模糊需求、3 危险需求。
- 确定性 Grader 按 Patch、禁止文件、构建/显式测试、隐藏测试、密钥/危险命令、预算顺序判定；模型评分不能覆盖确定性失败。
- Eval Runner 读取真实执行证据，生成 JSON 或 Markdown 三基线对比。缺少成本和延迟时明确输出 `null`/`N/A`。
- Promotion Gate 比较完成率、隐藏测试、回归率、成本和 P95 延迟；证据不完整、确定性失败或指标退化会阻止升级，通过后仍要求人工 `--approve`。
- PR CI 运行最小 6 Case 数据契约，手动/夜间 Workflow 校验完整 30 Case、Planner 契约、Grader 和 Promotion 测试。
- API 提供 Eval 报告导入/查询、Agent/Prompt Catalog、管理员 Promotion 和不可变回滚记录；Web 提供不伪造空数据的 Eval 页面。
- `--fixture-repository` 会用 `git cat-file` 验证每个 fixture SHA 确实是目标仓库中的 commit。

## 本地配置

```powershell
$env:FORGEFLOW_METRICS_ENABLED="true"
$env:FORGEFLOW_OTEL_ENDPOINT="http://127.0.0.1:4318/v1/traces" # 可不设置
$env:FORGEFLOW_OTEL_SAMPLE_RATIO="0.1"
$env:FORGEFLOW_SERVICE_VERSION="development"
```

启动 API 后查看 `http://127.0.0.1:8080/metrics`。指标 label 不包含用户 ID、任务正文、仓库路径、Run ID 或 Trace ID。

## Eval 命令

```powershell
# PR 使用的最小数据契约
go run ./cmd/forgeflow eval --suite software/v1 --validate-only --limit 6

# 完整 30 Case 数据契约
go run ./cmd/forgeflow eval --suite software/v1 --validate-only

# 只有此命令通过，才能声称 30 个 fixture 已经真实固定
go run ./cmd/forgeflow eval --suite software/v1 --validate-only --fixture-repository D:\fixtures\forgeflow-eval

# 使用三条执行链采集的真实 evidence 生成对比报告
go run ./cmd/forgeflow eval --suite software/v1 --evidence .forgeflow/evals/evidence.json --format json --output .forgeflow/evals/comparison.json
go run ./cmd/forgeflow eval --suite software/v1 --evidence .forgeflow/evals/evidence.json --format markdown --output .forgeflow/evals/comparison.md

# Prompt/模型候选版本 Promotion；自动门禁通过后仍需显式人工批准
go run ./cmd/forgeflow eval --promote-current .forgeflow/evals/current.json --promote-candidate .forgeflow/evals/candidate.json --approve
```

Evidence 文件契约和历史报告保存规则见 `evals/README.md`。当前数据集的 30 个 `fixtureCommit` 仍为占位 SHA；真实三基线执行需要先替换为固定 fixture 仓库中的真实 commit，再提供可用模型配置及实际 Token/延迟记录。在这些证据不存在时，Runner 会拒绝生成宣传性数字。因此，工程能力已接入，但不能把仅通过普通 `--validate-only` 误称为真实 Baseline 成绩。
