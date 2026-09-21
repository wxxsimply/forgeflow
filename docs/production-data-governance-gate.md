# Production 数据治理配置门禁

> 状态：P8-005 的工程基础。它不代表 Provider、法定主体、Region、子处理者或法律文本已经获批；这些值必须由授权流程在私有位置维护。

## 1. 启动前拒绝规则

FORGEFLOW_ENV=production 时，API 和 Worker 都必须加载 FORGEFLOW_DATA_GOVERNANCE_POLICY_FILE，并且文件的 SHA-256 必须等于 FORGEFLOW_DATA_GOVERNANCE_POLICY_SHA256。直接把 Policy 写进环境变量会被拒绝。

文件必须声明：

- schemaVersion 为 forgeflow.data-governance/v1、Policy 版本、RFC3339 格式的批准 UTC 时间和私有批准记录 ID；
- 主 Region 与允许 Region；
- 当前模型 Provider、HTTPS API 主机、处理 Region、数据类别和它对应的子处理者；
- Artifact、append-only audit、Trace 各自使用的子处理者、端点主机、Region 和数据类别；
- 已发布的隐私政策/服务条款版本、HTTPS URL、发布时间，以及 explicitAcceptance=true。

应用会拒绝未登记的模型 Provider/端点、未允许的 Artifact/Audit Region、未登记的 Trace 端点、错误的数据类别、无用户告知版本、无显式接受要求、哈希不匹配或缺失的配置。

## 2. 私有 Policy 文件

Policy 不得包含 API Key、客户数据、合同正文或内部系统访问凭据。应以 0600 常规文件从 Secret Manager 挂载到 API 与 Worker。批准后的原始文件、签署材料、法务意见和数据流登记存放在授权的私有系统，不提交 Git。

计算精确文件指纹：

    (Get-FileHash -Algorithm SHA256 -LiteralPath .\data-governance-policy.json).Hash.ToLowerInvariant()

文件任意字节（包括空白）变更都会改变指纹，必须重新经过 Data Owner / Product Owner / Legal 的批准并更新部署配置。

## 3. 与运行时配置的绑定

| 运行时路径 | 必须登记的 Policy 项 | 环境变量绑定 |
|---|---|---|
| 模型调用 | Provider ID、HTTPS 主机、Region、model_inference | FORGEFLOW_MODEL_PROVIDER、FORGEFLOW_OPENAI_BASE_URL |
| Artifact | Region、artifact_storage | FORGEFLOW_DATA_GOVERNANCE_ARTIFACT_SUBPROCESSOR_ID |
| 不可变审计 | Region、audit_storage | FORGEFLOW_DATA_GOVERNANCE_AUDIT_SUBPROCESSOR_ID |
| OTLP Trace | HTTPS 主机、observability | FORGEFLOW_DATA_GOVERNANCE_TELEMETRY_SUBPROCESSOR_ID |

Production 同时要求 FORGEFLOW_AUDIT_BACKEND=s3 和非空 HTTPS FORGEFLOW_OTEL_ENDPOINT。这不会替代 Bucket/IAM、Trace 后端、实际用户接受记录和恢复演练。

## 4. 验收与 No-Go

上线前在隔离环境用最终 Secret 文件启动 API 与 Worker，分别验证批准配置能够启动，并验证下列任一改动导致启动前失败：未登记 Provider 主机、Region、Trace 主机、子处理者 ID、Policy SHA-256、隐私/条款版本或显式接受标识。

即使配置门禁通过，P8-005 仍为 Open，直到私有 Provider/Region/数据驻留/子处理者决策、公开生效的隐私/条款、用户接受记录和适用的跨境/保留材料完成独立复核。
