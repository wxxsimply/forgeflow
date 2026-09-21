# 外部 Append-only 审计与受控 Trace Runbook

> 工程基线：PR #88 合并后的 Migration 7
> 当前边界：外部审计与 Trace 适配器、失败关闭和离线验证能力；真实 Bucket、工作负载身份、删除保护、访问审计、查询恢复演练与独立安全评审完成前，`P8-004` 保持 `Open`

## 1. 权威边界

- PostgreSQL `audit_log` 继续服务本地关联和用户生命周期，但它可被数据库管理员修改或删除，不是 Production 不可变证据源。
- `FORGEFLOW_AUDIT_BACKEND=s3` 启用外部权威审计：每个事件写成独立对象，使用条件创建、SHA-256、KMS 和 S3 Object Lock `COMPLIANCE` 保留。
- API 启动时检查 Bucket 可访问且 Object Lock 已启用。检查失败时 API 不启动。
- 登录尝试和所有已认证请求在进入业务 Handler 前写入接收事件。外部写入失败返回 `503`，业务读取或变更不会开始。
- 业务完成后的语义事件同时写 PostgreSQL 和外部后端。即使完成事件写入失败，前置接收事件仍保留操作者、请求 ID、方法、路由和不含查询串的资源路径，供事故调查。
- Trace 是采样诊断数据，不替代零丢失审计。Production OTLP/HTTP 必须使用 HTTPS；认证头只从 `FORGEFLOW_OTEL_HEADERS_FILE` 读取。

## 2. 数据最小化与完整性

外部事件只允许以下信封：Schema、事件 ID、UTC 时间、Actor ID、Action、资源类型/ID、Request ID、源 IP 的 HMAC 假名、受限 Details 和完整性签名。

- 不读取或保存请求 Body、Query、Cookie、Authorization、密码、Session/CSRF Token、模型 Key、DSN、任务正文、Prompt、Patch、源码、工具输出、MFA 动态码或恢复码。
- Details 的键、深度、数量、字符串长度和类型均有上限；敏感键直接丢弃。
- 源 IP 使用独立完整性密钥做 HMAC，不保存原始地址。
- 事件使用 HMAC-SHA-256 签名。恢复时同时验证对象前缀、Content-Type、长度、SHA-256、KMS Key、Object Lock 模式/期限、对象 Metadata 和事件签名。
- 完整性密钥是 32 个随机字节的标准 Base64，只交付 API。丢失密钥会阻断历史恢复验证；不得在日志、工单、Git 或普通备份中保存明文。

## 3. Bucket 与身份契约

批准的审计 Bucket 必须与 Artifact Bucket 分离，并满足：

1. Object Lock 已启用；事件使用 `COMPLIANCE` 模式且保留期不短于 `FORGEFLOW_AUDIT_RETENTION`。
2. 服务端加密固定到独立 KMS Key；Staging 与 Production 的 Bucket、Key 和身份完全分离。
3. API 工作负载只允许目标 Prefix 的创建、读取和列举，以及读取 Object Lock 配置；不授予删除对象、缩短保留期、修改 Bucket Policy、关闭 Object Lock 或管理 KMS Key 的权限。
4. Security/Platform 审核身份与 API 写入身份分离；所有查询和 KMS 解密留下云访问日志。
5. 生命周期只能在批准保留期之后处理对象；复制和恢复目标不得降低保留模式或期限。

## 4. 查询与恢复

只读验证命令使用与 API 相同的工作负载身份或单独批准的审核身份：

```text
forgeflow audit verify --max-events 10000
forgeflow audit query --action approval.approve --max-events 10000 --max-results 200
forgeflow audit query --request-id <request-id>
forgeflow audit query --resource-id <resource-id>
```

`verify` 遍历对象并验证全部完整性控制，只输出事件数量、首末时间和聚合完整性摘要。`query` 只输出已脱敏事件。命令没有删除、覆盖或修改能力；超过扫描/结果上限会失败，不会静默截断。

恢复演练必须在隔离账号或隔离前缀中列举并验证对象，核对事件数量、时间窗口和完整性摘要。原始查询输出、云访问日志和 Bucket 清单属于私有 Evidence，不进入 Git。

## 5. Staging 验收顺序

1. 由 Platform Owner 建立独立、私有、Object-Lock-enabled Bucket 和 KMS Key，配置工作负载身份与访问日志。
2. 在目标主机写入 `audit_integrity_key` 和 `otel_headers`，权限为 `0600`；`otel_headers` 使用 JSON 对象，内部 Collector 可写 `{}`。
3. 填写审计 Bucket/Region/Prefix/KMS/保留期，并保持 `FORGEFLOW_AUDIT_BACKEND=s3`。
4. 启动 API，确认 Bucket 或 Object Lock 配置错误会阻止 Readiness。
5. 分别执行登录、Session、仓库、Run、审批、Artifact 下载、MFA 与账户操作；按 Request ID 查询对应前置和语义事件。
6. 临时撤销测试身份的 Put 权限，确认登录和受保护请求返回 `503` 且业务数据未变化；随后恢复权限并验证服务恢复。
7. 验证敏感请求值不出现在对象、Trace、Collector 日志或云访问日志中。
8. 在隔离环境执行 `audit verify` 和条件查询；验证删除、覆盖、缩短保留期均被拒绝。

## 6. No-Go 条件

以下任一情况立即停止：Object Lock 非 `COMPLIANCE`、保留期可缩短、API 拥有删除/管理权限、KMS 或 Bucket 与 Artifact 共用、外部后端不可用却仍执行请求、敏感数据进入审计/Trace、完整性校验失败、查询无访问日志、恢复数量不一致，或 Production OTLP 使用非 HTTPS。

本 PR 不创建云资源、不写入真实 Secret、不执行部署或外部请求，也不关闭 `P8-004`。
