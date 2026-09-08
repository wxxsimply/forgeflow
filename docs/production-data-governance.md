# ForgeFlow Production 数据治理制度

> 状态：阶段 8 工程基线；法定主体、服务地区和数据驻留在阶段 9 上线前批准
> Data Owner 对分类、保留、删除、导出和子处理者负责；Security Owner 对访问与审计负责

## 1. 数据分类

| 等级 | ForgeFlow 示例 | 允许位置 | 禁止事项 |
|---|---|---|---|
| Restricted | 密码、Session/CSRF、模型 Key、DSN、webhook、私钥、隐藏测试、Private Grader | Secret Manager、专用 Hash/KMS、受限私有系统 | 进入模型、日志、Git、Issue、普通 Artifact 或公开支持渠道 |
| Confidential | 用户仓库源码、任务正文、Patch、测试输出、Run Evidence、账号邮箱 | 批准 Region 的数据库/对象存储/临时 Sandbox；传输与静态加密 | 跨租户、无期限保留、发送给未告知的第三方 |
| Internal | Release manifest、脱敏审计、聚合成本、内部 Run ID | 私有运维/遥测/发布系统 | 公开时与用户或私有仓库重新关联 |
| Public | README、公开文档、公开 Release、许可证 | GitHub/Public CDN | 混入以上三类数据 |

分类向上继承：包含任一更高等级字段的记录按更高等级处理。未知数据默认为 Confidential；疑似凭据立即按 Restricted 隔离和轮换。

## 2. 最小收集与用途

ForgeFlow 只为以下用途处理数据：认证与授权、执行用户明确创建的 Run、生成和验证 Patch、人工审批、费用/安全预算、故障与安全审计、用户请求的导出/删除。不得将用户仓库或任务用于训练通用模型、广告、出售数据或与无关账户合并，除非重新取得明确授权并更新公开政策。

模型 Provider 只接收完成当前 Run 所需的任务描述、仓库规则和有大小上限的源码/证据片段。发送前执行 Secret 检测和最小化；不发送密码、Key、Cookie、Private Grader、隐藏测试源码或无关 Fixture。Provider、模型、Prompt 版本、发送类别、Token、成本和时间进入脱敏审计。

## 3. 默认保留计划

以下是上线前需由 Data Owner 和法务确认的工程默认值；合同或法律要求更短时采用更短期限，Legal Hold 只能由授权流程创建。

| 数据 | 默认在线保留 | 删除方式 | 备份尾期 |
|---|---:|---|---:|
| 未完成 Run / Checkpoint / Queue | 30 天 | 取消后删除工作区和孤儿 Job | ≤ 35 天 |
| 已完成 Run metadata | 90 天 | owner scope 级联/匿名化 | ≤ 35 天 |
| Patch、测试、报告 Artifact | 30 天 | 删除对象版本并保留删除审计 | ≤ 35 天 |
| 临时 worktree / Sandbox | Run 终态后 24 小时内 | 安全清理；不得作为备份来源 | 不备份 |
| Session | 到期/撤销后 7 天内清理 Hash | 数据库删除 | ≤ 35 天 |
| 认证/管理员/审批/发布审计 | 365 天 | 到期归档或不可逆匿名化 | ≤ 35 天 |
| 安全事故证据 | 事故关闭后 2 年或法定期限 | Security + Legal 双人批准 | 按案件策略 |
| 聚合、不可关联指标 | 13 个月 | 时间序列生命周期策略 | 不含原始正文 |

备份中的逻辑删除通过不可变删除清单传播；备份到期前不为单条记录重写不可变介质，但任何恢复都必须在重新开放访问前重放删除清单。

## 4. 用户导出流程

1. 通过已认证 Session 接收请求；高风险或失去账号访问时执行额外身份验证。
2. Data Owner 创建不可猜 request ID，记录用户、范围、UTC、截止时间和审批人，不记录密码。
3. 仅查询 owner-scoped 用户、Repository metadata、Run、审批、事件和 Artifact；Secret、内部检测规则、其他租户、Private Grader/隐藏测试和内部安全证据不导出。
4. 生成结构化 JSON manifest 和原始用户 Artifact，逐项记录 SHA-256、大小、内容类型与时间。
5. 使用短时、单次、HTTPS 下载或加密离线交付；下载 URL 不进入普通日志，默认 24 小时过期。
6. 记录交付审计并删除临时导出包。

当前没有自助导出端点。初始邀请制阶段可由经批准的双人运维流程执行；开放自助注册或规模化 Production 前必须实现 owner-scoped 导出 API/Job、速率限制和集成测试。

## 5. 用户删除流程

1. 验证身份、租户范围、Legal Hold 和未完成 Run；先撤销 Session、API/仓库/Provider 凭据并阻止新 Job。
2. 取消/终止用户 Run，等待 Worker checkpoint，清理 worktree 和任务容器。
3. 写入不可变删除 request ID；删除 Artifact 对象版本，再在事务中删除/匿名化业务 metadata。
4. 保留最小不可关联审计：request ID、执行时间、数据类别、结果和审批人；不保留任务/源码。
5. 将删除标记加入备份恢复重放清单，并在备份尾期结束后验证不可恢复。
6. 向用户确认完成范围、例外类别和备份尾期，不披露内部安全信息。

当前代码没有完整的用户级联删除编排。Production 用户数据接入前必须实现 dry-run、双人确认、租户边界、幂等、部分失败恢复和审计测试；在实现前只能使用可整体销毁的专用测试租户。

## 6. 访问、审计和 Legal Hold

- 人员访问按职责和工单临时授权；Production 数据库与对象存储禁止共享账号。
- 支持/开发人员默认看不到源码、任务正文和 Artifact；诊断优先使用 Run ID、摘要、大小、状态和哈希。
- Break-glass 访问要求 MFA、时间限制、实时告警和 24 小时内复核。
- Legal Hold 必须包含依据、范围、Owner、批准人、开始/复核/到期日；到期自动回到正常保留策略。
- 每季度复核权限、长期 Secret、导出/删除请求、超期数据和子处理者变化。

## 7. 第三方和跨境

上线前由 Data Owner 维护私有数据流登记：云托管、数据库、对象存储、监控、告警、邮件和模型 Provider，包括主体、Region、数据类别、用途、保留、删除、跨境机制和事故通知承诺。公开隐私政策列出适用类别；合同/法务材料保存在授权系统，不提交密钥或客户数据。

未批准 Region、子处理者或跨境路径时，系统必须拒绝对应 Production 配置，不以用户点击普通服务条款替代必要授权。

## 8. 阶段 9 验证

- owner-scoped 导出无 IDOR，包内容与 manifest/hash 一致，下载过期。
- 删除流程可重试且不会误删其他租户；对象、metadata、workspace、Session 和恢复删除清单一致。
- 保留策略对数据库、对象存储、Telemetry 和备份生效。
- Provider 实际发送数据不超出告知和授权范围；Secret/隐藏测试检测为 0。
- 数据 Region、子处理者、隐私/条款版本和用户接受记录可审计。

上述证据未完成时，数据制度只能视为工程准备，不代表法律或 Production 验收完成。
