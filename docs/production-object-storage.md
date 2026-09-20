# Production Artifact 对象存储

本文说明 ForgeFlow 的 S3/S3-compatible Artifact 后端、一次性迁移和真实环境验收边界。代码实现不等于关闭 `P8-001`；只有真实 Bucket、KMS、IAM、版本控制、生命周期、访问审计、迁移和恢复证据全部通过，风险才能由独立审核人标记为 `Verified Closed`。

## 1. 安全模型

- PostgreSQL 只保存 Artifact ID、Run ID、不可猜 storage key、SHA-256、大小、Content-Type 和小型属性。
- 对象键固定为 `<prefix>/tenants/<owner>/runs/<run>/<artifact>/<sha256>`；owner、Run、Artifact 和摘要都必须与 metadata 及对象自定义 metadata 一致。
- 写入先在权限为 `0600` 的受管 spool 文件中执行大小限制和 SHA-256，再以 `If-None-Match: *`、SHA-256 checksum 和显式服务端加密上传。
- Production 配置只接受 `aws:kms` 和显式 KMS key；API/Worker 启动时执行 Bucket preflight，失败时不启动。
- 读取同时校验配置前缀、tenant/run key、对象 metadata、大小、加密方式、checksum header（如后端返回）及流式 SHA-256。
- API 在 Run owner/admin 授权通过后读取正文，并在向客户端发送前完成全部完整性验证。
- 删除先删除对象，再删除 PostgreSQL metadata。对象删除成功但 metadata 删除失败时，重试会再次执行幂等对象删除。
- 版本化 Bucket 的普通删除会产生 delete marker；完整用户级联删除必须枚举并删除全部对象版本、保留删除清单和审计，这属于下一项数据删除编排，不得以当前 `Store.Delete` 代替。
- 不配置静态 Access Key 环境变量；优先使用工作负载身份、实例角色或批准的 SDK 默认凭据链。

## 2. 配置

| 变量 | Production 要求 |
|---|---|
| `FORGEFLOW_ARTIFACT_BACKEND` | 必须为 `s3` |
| `FORGEFLOW_ARTIFACT_S3_BUCKET` | 已批准且禁止公网访问的 Bucket |
| `FORGEFLOW_ARTIFACT_S3_REGION` | 与数据驻留记录一致 |
| `FORGEFLOW_ARTIFACT_S3_ENDPOINT` | AWS S3 可留空；自定义后端必须为 HTTPS |
| `FORGEFLOW_ARTIFACT_S3_PREFIX` | 每个环境独立，例如 `production/artifacts` |
| `FORGEFLOW_ARTIFACT_S3_SPOOL_DIR` | 加密磁盘上的临时目录，不得被 API 与 Worker 跨主机共享 |
| `FORGEFLOW_ARTIFACT_S3_SSE` | 必须为 `aws:kms` |
| `FORGEFLOW_ARTIFACT_S3_KMS_KEY_ID` | 批准且由 GetObject 原样返回的稳定 key 标识；AWS 推荐使用 key ARN，读取时会精确核对 |
| `FORGEFLOW_ARTIFACT_S3_USE_PATH_STYLE` | AWS S3 保持 `false`；仅兼容后端按需启用 |
| `FORGEFLOW_ARTIFACT_MAX_BYTES` | 单个 Artifact 上限，默认 64 MiB |

API 身份至少需要 Bucket preflight 与指定 prefix 的读取权限；Worker 身份需要 Bucket preflight，并在开始持久化 Artifact 后仅获得指定 prefix 的写入权限；迁移身份临时需要源 metadata 读写、目标 Put/Get/Delete 和 KMS 权限。三者不得共享管理员凭据。

## 3. 一次性迁移

迁移是外部写入，必须单独获得环境、Run、owner、费用和回退授权。先备份数据库与源 Artifact 目录，不得对在线 Production 直接试跑。

配置 PostgreSQL、源 `FORGEFLOW_ARTIFACT_ROOT` 和目标 S3 参数后，逐个 Run 执行：

```powershell
go run ./cmd/forgeflow artifact migrate --run <run-uuid>
```

租户 owner 从 Run 的数据库记录读取，不接受人工输入，避免对象进入错误的租户前缀。

迁移流程：

1. 从 PostgreSQL 列出该 Run 的 metadata。
2. 从 FileStore 读取并验证源文件的大小和 SHA-256。
3. 上传到 tenant/run key，并重新读取验证对象 metadata、KMS、大小和 SHA-256。
4. 使用旧 storage key 作为 CAS 条件原子切换 PostgreSQL。
5. CAS 失败时若 metadata 已由并发迁移切到同一目标则按已完成处理；否则保留已验证目标供安全重试，避免误删并发迁移将要引用的对象；成功时保留本地源文件作为限时回退副本。
6. 重跑时验证已迁移对象并报告 `alreadyMigrated`，不会重复改写 metadata。

迁移完成后不得立即删除 FileStore。按批准的回退窗口完成清单核对、API 下载、跨租户拒绝和恢复演练，再通过单独删除流程清理源副本。

## 4. 失败与恢复

| 故障点 | 行为 |
|---|---|
| spool/超限失败 | 不请求对象存储，不写 metadata |
| Put 明确失败 | 若目标对象可完整读取并验证，视为响应丢失后的成功；否则保持失败 |
| metadata Insert 失败 | 补偿删除新对象；同时报告原错误和清理错误 |
| 迁移 storage-key CAS 失败 | 保留源文件、旧 metadata 和已验证目标对象；报告 storage key，重跑时安全复用；若并发迁移已切到同一目标则按成功恢复 |
| Get metadata/大小/KMS/SHA 不一致 | 拒绝返回正文 |
| Delete 对象失败 | 保留 metadata，允许安全重试 |
| Delete metadata 失败 | 对象已删除；重试幂等删除对象后再次删除 metadata |
| Bucket preflight 失败 | API/Worker 启动失败 |

若 metadata Insert 的补偿删除失败，或迁移 CAS 失败后保留了目标对象，使用错误中记录的 storage key 调查并安全重试；不得手工修改 PostgreSQL 来掩盖不一致。

## 5. 真实环境验收

至少保存以下私有证据及 SHA-256：

- Bucket public-access block、Object Ownership、versioning/Object Lock、生命周期、访问日志和跨故障域/Region 策略；
- KMS key policy、rotation、API/Worker/迁移身份的最小权限与拒绝测试；
- 同 owner 不同 Run、不同 owner、伪造 key、伪造 metadata 和越权下载拒绝；
- 正常 Put/Get/Delete、同尺寸篡改、超限、响应丢失、数据库失败与补偿清理；
- 逐 Run 迁移、中断后重跑、CAS 冲突、源文件保留及回退；
- 数据库恢复点与对象版本/清单一致性，以及实测 RPO/RTO。

未完成以上真实验收前，`P8-001` 保持 `Open`，不得把本 PR 的单元测试描述为 Production 通过。

