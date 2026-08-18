# ForgeFlow 3～5 分钟演示

## 演示前

1. 使用只含演示代码的 Git fixture，确认挂载路径在 `/repositories` 下且有 `HEAD`。
2. `staging-preflight.ps1`、备份、健康检查和告警 webhook 测试通过。
3. 准备 Admin/Operator 演示账号，不使用生产账号或真实私有仓库。
4. 浏览器提前打开 HTTPS 登录页、Run 列表和只读监控视图。

## 演示脚本

**0:00–0:40 安全边界。** 展示 HTTPS、登录和角色；说明 API 没有模型 Key/Docker，Worker 与数据库不暴露公网，原仓库工作文件不会被直接修改。

**0:40–1:30 创建 Run。** 登记 fixture 仓库，创建一个小任务；展示稳定 Idempotency-Key、预算和 `waiting_for_plan_approval`。

**1:30–2:20 人工审批。** 查看计划、风险、文件范围和 Evidence，批准后说明审批绑定 Run 版本、输入摘要、workspace 和 Policy 版本，旧批准不能复用。

**2:20–3:30 受控执行。** 展示 Graph、Node Trace、模型 Token/成本、Tool allow/deny、实际测试退出码、Reviewer/Security 独立结果。指出 Sandbox 默认无网络且具有 CPU/内存/PID/超时上限。

**3:30–4:20 交付证据。** 展示 Diff、Patch SHA-256、测试 Evidence、最终报告；再次检查原 fixture 工作区无变化。

**4:20–5:00 运维。** 展示 Prometheus Alert、备份 checksum、Release manifest 和“数据库不兼容时阻止应用回滚”的流程。

## 可重复 API 验证

```powershell
$password = Read-Host "Demo password" -AsSecureString
./scripts/demo-staging.ps1 -BaseUri https://forgeflow-staging.example.com -Email demo@example.com -Password $password -RepositoryPath /repositories/demo
```

脚本依次验证健康、登录、Repository、Run、审批 ETag、终态和报告，并只输出 Run ID、状态和耗时。演示失败时停止，不批准失败 Run，也不切换到真实仓库掩盖问题。
