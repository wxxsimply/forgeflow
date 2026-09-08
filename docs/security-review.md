# Staging Security Review Checklist

每次 Staging Promotion 记录审核人、UTC 时间、Release manifest 和证据链接。阶段 7 只执行本地合成检查；下列真实环境项目仍须在阶段 9 逐项签署。

阶段 7 快速检查：

```powershell
./scripts/validate-operations-assets.ps1
./scripts/staging-security-drill.ps1 -DryRun
```

`-DryRun` 会运行 Staging 静态契约，以及 Repository 路径/符号链接、Tool Patch、命令 Policy、Sandbox、Secret 扫描、Governance 和 Config 测试，不连接 Staging。阶段 9 的真实边界检查必须显式使用 `-IncludeOpenAI -ConfirmLiveDrill`，并另行保留真实 task container 和日志脱敏证据。

- [ ] `staging-preflight.ps1` 通过；只有 Caddy 发布端口。
- [ ] API 未挂载 OpenAI Key、Docker Socket 或可写仓库。
- [ ] Worker Secret 最小化；Bootstrap Secret 已删除。
- [ ] ForgeFlow、第三方与 Sandbox 的所有受控环境镜像均固定 SHA-256 digest。
- [ ] `go test ./...`、race CI、`go vet`、前端检查、E2E 和最小 Eval 通过。
- [ ] 依赖漏洞扫描和 SBOM 无未接受的 Critical/High。
- [ ] 路径穿越、命令注入、CSRF、IDOR 和 Prompt Injection 回归通过。
- [ ] HTTPS 证书、HSTS、CSP、Cookie Secure 和 Allowed Origin 验证。
- [ ] PostgreSQL/监控/Worker 未公开；API 无外网出口。
- [ ] 最新备份 checksum 正确，隔离恢复演练在周期内成功。
- [ ] 告警 webhook 已实测，主/备值班联系人有效。
- [ ] 当前与候选 Eval 报告通过 Promotion Gate，人工批准已记录。
- [ ] 回滚镜像仍可拉取，目标版本与当前 Schema 兼容。

任何未勾选项必须有风险接受人和到期时间。Production 不接受“稍后补”作为长期例外。
