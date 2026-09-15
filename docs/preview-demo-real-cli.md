# 独立真实任务的离线操作入口

记录日期：2026-09-15；基线：PR #66，`51e7d639a43f58c5d5d269409da8dd8b789e35db`。

入口是 `scripts/preview_demo_real.py`。它用于准备、核对和记录批准，不会发送模型请求，也不读取 API Key。`test` 是唯一执行候选代码的命令，必须单独批准，使用已有无网络容器沙箱。

这不是网页自动改代码功能。网站不变，本轮没有真实任务、付费调用或真实候选测试。

## 1. 六个命令分别做什么

| 命令 | 做什么 | 是否改变状态 |
| --- | --- | --- |
| `prepare` | 保存任务、四文件快照、价格、人民币额度和请求参数 | 创建新任务；不覆盖旧任务、不调用模型 |
| `inspect` | 核对请求状态、预算、候选测试收据 | 不发请求、不执行候选 |
| `plan` | 查看具体价格、预算、模型、超时和计划摘要 | 不记录批准 |
| `approve` | 保存针对指定计划的确认记录 | 记录批准；仍不发送请求或预留费用 |
| `review` | 查看已归档候选的代码差异和测试批准摘要 | 只审查，不测试 |
| `test` | 使用对应候选批准摘要进行一次隔离测试 | 消耗候选执行批准，保存测试结果 |

不存在 `send`、`--api-key`、自定义提供商或模型覆盖参数。错误参数只返回固定错误码，不回显可能误填的密钥。不要把真实密钥传给这个脚本或写进任务、价格及批准参考文件。

## 2. 准备文件

需要操作者事先创建一个私有目录，放在源码仓库之外，并设置合适访问权限。脚本不会创建上级目录、修改 Windows ACL 或扫描其他任务目录。

- 任务文件：UTF-8，最多 20,000 字节，内容需人工核对。
- 演示源码：只能包含 `README.md`、`go.mod`、`greeting.go`、`greeting_test.go`，总计最多 128 KiB，仍须符合公开 demo 模块约定。
- 价格文件：最多 4 KiB 的严格 JSON，明确模型、三类 token 单价、汇率假设、有效期和官方来源；不接受多余字段、重复键、浮点金额或自动补报价。
- 批准参考文件：UTF-8 单行文本，正文最多 256 字节，可有一个末尾换行；例如引用本轮用户确认记录。不是密码文件，也不是身份签名。

价格结构如下。`null` 必须由操作者在核对后填写，原样使用会被拒绝；不提供可能被误用的合成报价：

```json
{
  "model": null,
  "input_nano_usd_per_million": null,
  "cached_nano_usd_per_million": null,
  "output_nano_usd_per_million": null,
  "fx_micro_cny_per_usd": null,
  "valid_from": null,
  "valid_until": null,
  "source": "https://api-docs.deepseek.com/quick_start/pricing/"
}
```

单价单位为“每百万 token 的纳美元”，1 美元 = 1,000,000,000 纳美元；汇率用“人民币/美元”的百万分之一单位。有效期为 Unix 秒，最长一天，必须覆盖计划的请求超时。模型名遵循现有协议白名单；参数校验不代表已核实最新价格或模型限制。

`--rmb-fen` 使用整数分，例如 100 表示 1 元，但本说明不授权该金额。预算不足会拒绝创建，不会自动加钱或降低保守预留。

## 3. 使用顺序

先看帮助：

```powershell
python -B scripts/preview_demo_real.py --help
python -B scripts/preview_demo_real.py prepare --help
```

下面是占位示意，不能原样复制执行；每一步报错就停止：

```text
prepare --root <已存在的私有目录> --task-id <32位小写十六进制编号> --source <公开demo目录> --task-file <任务文件> --price-file <已核对价格文件> --rmb-fen <本次明确预算分>
plan --root <同一目录> --task-id <同一编号>
approve --root <同一目录> --task-id <同一编号> --approve-plan-sha256 <planSha256> --reference-file <本轮确认记录文件>
inspect --root <同一目录> --task-id <同一编号>
```

这些子命令都要加上 `python -B scripts/preview_demo_real.py` 前缀。准备时可指定 `--max-output-tokens` 与 `--timeout`；它们进入固定计划，之后不能临时覆盖。

`plan` 默认不显示正文；确需核对全部待发送内容时，显式加 `--show-payload`。该输出包含任务和源码，不要发布到 GitHub、公共日志或聊天。修改原始文件不会改变已归档任务；计划过期仍可查看，但 `priceWindowValid:false`，不能批准。

`approve` 是本地操作记录，不证明操作者身份、不核实参考文本真假，也不代替当前对话中的发送/收费授权。此 CLI 不会消费它去请求模型；未来付费入口仍须受控凭据加载和本轮明确授权。

只有归档已有合法候选时才能继续 `review` 和 `test`：

```text
review --root <同一目录> --task-id <同一编号>
test --root <同一目录> --task-id <同一编号> --approve-candidate-sha256 <review中的approvalSha256>
```

`review` 的 Diff 也属于私有输出。请求的 `planSha256` 不能代替候选的 `approvalSha256`。`test` 仅接受受信任的本地缓存镜像和 1～120 秒超时；复用既有非 root、只读源码、无网络沙箱，不自动拉镜像。请先确认隔离环境，不要为了演示在公网服务开放任意命令。

## 4. 怎样判断成功

- 顶层 `state:completed` 仅表示请求结果和合法提案已归档，不代表代码通过测试。
- 候选结果在 `candidate.evidence` 中：未执行是 `reviewed`，已完成有 `result.passed`。
- 测试不通过时退出码是 1，`checksPassed:false`；即使顶层请求状态为 `completed`，也不能写成任务成功。
- 未知、中断或已经消耗的批准不自动重试；不要删目录、换编号或复制数据库规避检查。
- `modelCalls:0` 指本次 CLI 调用没有请求模型，不表示历史费用为零；历史记录看预算和响应摘要，费用仍是估算而非账单。

## 5. 本轮验证与边界

新增 17 项 CLI 检查；完整快速集合 207 项，206 项通过，1 项 Windows 符号链接权限跳过，约 19.08 秒。

测试全部使用临时公开模板、合成任务/价格/响应；发送函数、内部付费执行函数和真实沙箱默认禁止调用。候选流程仅使用模拟沙箱，验证了归档输入、独立批准、一次性消费、失败及中断后的结果。另检查价格文件格式、参数脱敏、过期核对、白名单和硬链接拒绝。

未读取真实凭据、发起真实请求、执行真实候选或修改服务器。仍缺受控凭据加载与付费发送入口、当前任务和费用确认、实际账单核验与需求验收。当前停在人工提交 PR，不需要部署。
