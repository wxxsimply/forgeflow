# 独立真实任务的受控单次发送入口

记录日期：2026-09-16；基线：PR #68，`afa2d05aaac49168d7023770aabb516209cc53ab`。

入口是 `scripts/preview_demo_real.py`。它用于准备、核对、记录批准，并在全部保护条件满足后执行一次可能收费的 DeepSeek 请求。批准后必须先让归档的原始快照通过一次受限沙箱基线，才能读取密钥或发送；`test` 执行候选代码时仍须单独批准，并复用该基线固定的镜像摘要和超时。

这不是网页自动改代码功能。网站不变，本轮只完成代码与离线模拟验证，没有读取真实密钥、发送真实请求或执行真实候选。

## 1. 九个命令分别做什么

| 命令 | 做什么 | 是否改变状态 |
| --- | --- | --- |
| `prepare` | 保存任务、四文件快照、价格、人民币额度和请求参数 | 创建新任务；不覆盖旧任务、不调用模型 |
| `inspect` | 核对请求状态、预算、候选测试收据 | 不发请求、不执行候选 |
| `plan` | 查看具体价格、预算、模型、超时和计划摘要 | 不记录批准 |
| `approve` | 保存针对指定计划的确认记录 | 记录批准；仍不发送请求或预留费用 |
| `sandbox-check` | 用已缓存镜像对归档的原始四文件快照执行固定 Go 基线 | 通过后保存绑定计划、快照、镜像摘要、超时和沙箱配置的收据 |
| `credential-check` | 检查私有密钥文件的路径、权限、稳定性和格式 | 不保存密钥、不调用模型、不改变任务状态 |
| `send` | 复核沙箱收据及固定镜像仍可用，再使用已批准的精确计划发起唯一一次请求 | 在读取密钥前检查 Docker/镜像；联网前持久化请求次数与最高费用预留 |
| `review` | 查看已归档候选的代码差异和测试批准摘要 | 只审查，不测试 |
| `test` | 使用对应候选批准摘要进行一次隔离测试 | 消耗候选执行批准，保存测试结果 |

不存在 `--api-key`、环境变量密钥、自定义提供商/模型覆盖或重试参数。错误参数只返回固定错误码，不回显可能误填的密钥或路径。不要把真实密钥放进命令行、任务、价格、批准参考文件或 Git。

## 2. 准备文件

需要操作者事先创建一个私有任务目录，放在源码仓库之外，并设置合适访问权限。脚本不会创建上级目录、修改 Windows ACL 或扫描其他任务目录。每次命令都会复核该目录不是链接/重解析点：POSIX 下须由当前用户所有且组/其他用户无权限（通常为 `0700`）；Windows 下只允许目录所有者、SYSTEM 和 Administrators 拥有敏感访问权限。

- 任务文件：UTF-8，最多 20,000 字节，内容需人工核对。
- 演示源码：只能包含 `README.md`、`go.mod`、`greeting.go`、`greeting_test.go`，总计最多 128 KiB，仍须符合公开 demo 模块约定。
- 价格文件：最多 4 KiB 的严格 JSON，明确模型、三类 token 单价、汇率假设、有效期和官方来源；不接受多余字段、重复键、浮点金额或自动补报价。
- 批准参考文件：UTF-8 单行文本，正文最多 256 字节，可有一个末尾换行；例如引用本轮用户确认记录。不是密码文件，也不是身份签名。
- 密钥文件：必须位于源码仓库和私有任务目录之外，是单一硬链接的普通文件，不得经过符号链接/重解析点。内容为 16～512 个 ASCII 字母、数字、下划线、点或连字符，可有一个末尾换行。POSIX 下通常设为 `0600`；Windows ACL 只允许文件所有者、SYSTEM 和 Administrators。`credential-check` 可在不发请求的情况下验证这些条件。

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
python -B scripts/preview_demo_real.py sandbox-check --help
python -B scripts/preview_demo_real.py send --help
```

下面是占位示意，不能原样复制执行；每一步报错就停止：

```text
prepare --root <已存在的私有目录> --task-id <32位小写十六进制编号> --source <公开demo目录> --task-file <任务文件> --price-file <已核对价格文件> --rmb-fen <本次明确预算分>
plan --root <同一目录> --task-id <同一编号>
approve --root <同一目录> --task-id <同一编号> --approve-plan-sha256 <planSha256> --reference-file <本轮确认记录文件>
sandbox-check --root <同一目录> --task-id <同一编号> --approve-plan-sha256 <同一planSha256> --image <已缓存可信镜像> --timeout <1至120秒>
credential-check --root <同一目录> --task-id <同一编号> --key-file <私有密钥文件>
send --root <同一目录> --task-id <同一编号> --approve-plan-sha256 <同一planSha256> --key-file <同一私有密钥文件>
inspect --root <同一目录> --task-id <同一编号>
```

这些子命令都要加上 `python -B scripts/preview_demo_real.py` 前缀。准备时可指定 `--max-output-tokens` 与 `--timeout`；它们进入固定计划，之后不能临时覆盖。

`plan` 默认不显示正文；确需核对全部待发送内容时，显式加 `--show-payload`。该输出包含任务和源码，不要发布到 GitHub、公共日志或聊天。修改原始文件不会改变已归档任务；计划过期仍可查看，但 `priceWindowValid:false`，不能批准。

`approve` 是本地操作记录，不证明操作者身份、不核实参考文本真假，也不代替当前对话中的发送/收费授权。执行 `send` 前仍须重新确认当前模型和价格有效期、允许发送的数据、恰好一次调用及人民币费用上限；历史 Eval 授权不能复用。`send` 会消费已批准任务：请求一旦开始或结果不确定，就不能靠重启、换命令或自动重试再次发送。

`credential-check` 只表示文件当前满足本地保护条件，不表示批准付费。它会把密钥短暂读入当前进程内存后立即丢弃引用，不保存内容。拥有本机足够权限的进程仍可能读取内存，因此这不是硬件密钥库。

`sandbox-check` 必须在 `approve` 后运行，只使用归档快照，不读取密钥、不调用模型、不拉取镜像。沙箱固定为非 root、源码只读、根文件系统只读、无网络、能力全部移除、资源受限，并只执行 `go test -mod=readonly -count=1 -timeout 30s ./...`。失败或清理状态不确定时不保存收据，可以在修复本地 Docker 后重试；通过后收据不能覆盖。`readyForPaidExecution:true` 只表示持久化前置条件齐全，`send` 仍会在读取密钥前确认 Docker 可访问且同一镜像摘要仍存在。

只有归档已有合法候选时才能继续 `review` 和 `test`：

```text
review --root <同一目录> --task-id <同一编号>
test --root <同一目录> --task-id <同一编号> --approve-candidate-sha256 <review中的approvalSha256>
```

`review` 的 Diff 也属于私有输出。请求的 `planSha256` 不能代替候选的 `approvalSha256`。`test` 不再接受临时镜像或超时覆盖，只能复用基线收据中的固定镜像摘要和超时；仍不自动拉镜像。请先确认隔离环境，不要为了演示在公网服务开放任意命令。

## 4. 怎样判断成功

- 顶层 `state:completed` 仅表示请求结果和合法提案已归档，不代表代码通过测试。
- 候选结果在 `candidate.evidence` 中：未执行是 `reviewed`，已完成有 `result.passed`。
- 测试不通过时退出码是 1，`checksPassed:false`；即使顶层请求状态为 `completed`，也不能写成任务成功。
- 未知、中断或已经消耗的批准不自动重试；不要删目录、换编号或复制数据库规避检查。
- `modelCalls` 是任务账本持久化的累计请求次数；`modelCallAttempted` 只表示当前这次 CLI 运行是否越过传输边界。未知结果必须按可能已经收费处理。费用字段仍是根据用量和人工价格计算的保守估算，不是提供商账单。

## 5. 本轮验证与边界

加入发送前沙箱收据与固定镜像门禁后，完整快速集合共 226 项：224 项通过，2 项因当前 Windows 平台不适用而跳过，约 19.25 秒。

仓库级 `scripts/verify.ps1` 也已通过：Go 格式、测试、`go vet`、迁移嵌入和三个二进制构建，以及 Web OpenAPI 生成、TypeScript 检查、23 项 Vitest 和 Vite 构建均无错误。

测试全部使用临时公开模板、合成任务/价格/响应、假密钥和模拟 Docker；网络发送及真实沙箱默认禁止调用。新增覆盖批准前拒绝基线、失败不留收据、旧任务无表兼容、收据防覆盖/防篡改、缺少收据或镜像不可用时在读取密钥前阻断，以及候选只能复用固定镜像摘要和超时。

本机仅确认 Docker 28.4.0 客户端存在；`desktop-linux` 服务未启动，因此没有检查本地镜像或运行真实容器，隔离环境仍未验收。未读取真实凭据、发起真实请求、执行真实候选或修改服务器。真正执行前仍须启动 Docker、完成一次真实 `sandbox-check`，并人工核对最新模型/价格/汇率、密钥权限、发送范围、一次调用及费用上限。当前停在人工提交 PR，不需要部署。
