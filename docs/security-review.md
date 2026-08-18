# Staging Security Review Checklist

每次 Staging Promotion 记录审核人、UTC 时间、Release manifest 和证据链接。

- [ ] `staging-preflight.ps1` 通过；只有 Caddy 发布端口。
- [ ] API 未挂载 OpenAI Key、Docker Socket 或可写仓库。
- [ ] Worker Secret 最小化；Bootstrap Secret 已删除。
- [ ] 第三方与 Sandbox 镜像在 Production 前固定 SHA-256 digest。
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
