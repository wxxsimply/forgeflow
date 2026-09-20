# 管理员 TOTP MFA Runbook

> 适用版本：Migration 7 及以上
> 当前边界：应用内管理员 MFA 工程实现；真实 HTTPS Staging、独立安全评审、密钥轮换和 break-glass 演练完成前，`P8-003` 保持 `Open`

## 安全模型

- 管理员使用密码加 TOTP 动态码登录，也可使用一次性恢复码。
- TOTP 使用 RFC 6238 的 HMAC-SHA1、30 秒周期和 6 位数字，允许前后一个时间片的时钟偏差；已接受时间片原子记录，不能重放。
- TOTP 密钥使用 API 专属 32 字节密钥和 AES-256-GCM 加密，用户 ID 作为附加认证数据。数据库不保存明文密钥。
- 10 个恢复码仅在确认绑定后返回一次；数据库仅保存 HMAC-SHA-256 摘要，每个恢复码原子消费一次。
- `FORGEFLOW_ADMIN_MFA_REQUIRED=true` 时，未绑定管理员的密码登录只建立受限会话。该会话仅可读取当前身份、设置/确认 MFA 或注销；所有控制面端点失败关闭。
- 一旦管理员完成绑定，即使强制开关随后被关闭，该账号登录仍必须提供第二因子。

Operator 仍属于 Production 必须 MFA 的特权角色，但不在本 PR 的应用内管理员 MFA 授权范围内。真实 Production 若启用 Operator，必须由批准的 IAP 强制 MFA，或通过后续独立 PR 扩展并评审应用 MFA；否则保持 No-Go。

## API 与审计

- `GET /api/v1/account/mfa`：读取当前管理员绑定/强制状态。
- `POST /api/v1/account/mfa/setup`：要求当前密码，签发 10 分钟有效的待确认密钥。
- `POST /api/v1/account/mfa/confirm`：验证动态码，启用 MFA、提升当前 Session、撤销其他 Session，并仅本次返回恢复码。
- `POST /api/v1/auth/login`：已绑定管理员必须在 `secondFactor` 提交动态码或恢复码。

设置和启用分别写入 `auth.mfa.setup` 与 `auth.mfa.enable` 审计；Secret、动态码和恢复码不得进入日志、Trace、Issue、聊天或公开 Evidence。

## Staging 启用顺序

1. 应用 Migration 7；不得对未升级数据库启动新 API。
2. 在目标主机生成 `deploy/staging/secrets/mfa_encryption_key`，权限设为 `0600`。文件内容必须是恰好 32 个随机字节的标准 Base64。
3. 保持 `FORGEFLOW_ADMIN_MFA_REQUIRED=true`，按 digest 重建 API。确认 API 仅通过 HTTPS 入口访问。
4. 管理员使用密码登录；未绑定时应只能进入“数据与账户”页面，其他 API 返回 `403`。
5. 输入当前密码开始绑定，在身份验证器中录入密钥，提交动态码确认。
6. 离线保存 10 个恢复码；不得截图进工单或提交 Git。确认其他旧 Session 已撤销。
7. 依次验证：缺少/错误/重放动态码被拒绝，下一时间片动态码成功，恢复码首次成功且第二次失败，重启 API 后仍可登录。

## Break-glass 与停止条件

- 首选离线恢复码；每次使用必须触发人工告警，并在 24 小时内复核登录审计和 Session。
- 丢失身份验证器和全部恢复码时，不允许临时关闭环境变量、清空数据库字段或签发旁路 Session。必须由 Security Owner 与 Release Approver 通过私有系统批准受控恢复方案。
- 密钥文件丢失、密文无法解密、审计写入异常、时间同步漂移、旧 Session 未撤销或任一 MFA 旁路都立即 No-Go。
- 当前版本没有在线密钥轮换或自助停用 MFA；完成密钥轮换和 break-glass 实演前，不得把 `P8-003` 标记为 `Verified Closed`。
