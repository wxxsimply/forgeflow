# ForgeFlow 实施手册完成后的分阶段任务路线图

> 文档版本：2026-08-19
> 前置文档：`FORGEFLOW_GO_IMPLEMENTATION_GUIDE.md`  
> 当前审计：`docs/completion-audit.md`

## 1. 文档目标

本文件用于指导 ForgeFlow 在主体功能开发完成后，继续完成 Git 基线、GitHub 托管、真实 Eval、Prompt 发布治理、Staging 验收、Production 准备和 `v1.0.0` 发布。

必须遵守以下边界：

1. GitHub 上的仓库创建、代码提交、代码上传、Secrets 配置、分支保护、Pull Request 合并和 Release 发布，全部由仓库所有者**手动执行**。
2. Agent 或自动化不得自行执行 `git add`、`git commit`、`git push`、创建 GitHub 仓库、合并 Pull Request 或发布 Release。
3. `OPENAI_API_KEY`、数据库密码、Session Secret、Webhook、TLS 私钥和服务器 SSH Key 不得上传到 GitHub。
4. 没有真实执行证据时，不得把数据契约验证写成真实 Eval 成绩。
5. 没有完成公网 Staging 验收时，不得标记为 Production Ready。

---

## 2. 当前起点

在开始本路线图前，项目应至少满足：

- [x] Graph Runtime、Repository Harness、Agent、Policy、Tool 和 Judge 已实现。
- [x] PostgreSQL、Queue、API、登录、RBAC 和 Web 控制台已实现。
- [x] Eval 数据结构、Grader、报告、治理 API 和 Eval 页面已实现。
- [x] Staging Compose、HTTPS、监控、告警、备份和回滚资产已建立。
- [x] Go 测试、Vet、Staticcheck、govulncheck 和前端检查通过。
- [x] 项目已有首个 Git commit 和可解析的 `HEAD`（`11473a7`）。
- [x] 代码已由仓库所有者手动上传到 GitHub。
- [ ] 30 个 Eval Case 已绑定真实 fixture commit。
- [ ] 三种模式的真实 Eval 报告已生成。
- [ ] 公网 Staging 已完成验收。

因此，下一步不是继续增加普通页面，而是关闭最后的发布门禁。

---

## 3. 阶段划分与推进规则

后续工作拆分为 10 个阶段。阶段必须按顺序完成；上一阶段的退出门槛没有全部通过时，不得开始下一阶段的发布性操作。

| 阶段 | 名称 | 核心结果 | 主要执行位置 |
|---|---|---|---|
| 0 | 本地封板审查 | 工作区可安全提交，全部本地门禁通过 | 本地 |
| 1 | Git 与 GitHub 基线 | 首个 commit、远端仓库、分支保护和 CI 可用 | 本地 + GitHub，必须手动 |
| 2 | 真实 Eval Fixture | 30 个真实 commit 和隔离隐藏测试可重复运行 | 独立 Fixture 环境 |
| 3 | 三基线 Eval | 90 次受控执行和真实对比报告 | Eval 执行环境 |
| 4 | Prompt/模型治理闭环 | Promotion、Worker 一致性校验和回滚通过 | ForgeFlow + 数据库 |
| 5 | 不可变发布镜像 | 五类镜像、SBOM、扫描结果和 digest | 本地/CI + Registry，上传必须手动 |
| 6 | 真实 Staging 部署 | HTTPS Staging 全链路可运行 | Staging 服务器 |
| 7 | 运维与安全验收 | 告警、恢复、安全、回滚和 Demo 全部签署 | Staging 服务器 |
| 8 | Production 准备 | 架构、安全、SLO、值班和数据制度获批 | Production 准备环境 |
| 9 | v1.0.0 发布 | 签名 Tag、GitHub Release 和发布证据 | GitHub，必须手动 |

每个阶段统一包含五类信息：

1. **进入条件**：开始该阶段前必须已经满足的条件。
2. **阶段任务**：该阶段要完成的实现或操作。
3. **人工操作**：必须由仓库所有者、发布负责人或运维人员手动执行的动作。
4. **阶段交付物**：完成后必须留下的代码、报告、配置或审计证据。
5. **退出门槛**：全部勾选后才允许进入下一阶段。

状态只允许使用：`未开始`、`进行中`、`阻塞`、`已完成`。不得因为代码已经写好，就把需要真实环境验证的阶段标记为已完成。

---

## 4. 阶段 0：本地封板审查

> 当前状态：已完成（2026-08-19 仓库所有者已确认第三方依赖使用条件，全部退出门槛通过）
> 进入条件：`FORGEFLOW_GO_IMPLEMENTATION_GUIDE.md` 中可在本地完成的工程任务已完成。  
> 本阶段不包含：Git commit、GitHub 上传和服务器部署。

### 4.1 检查工作区

在项目根目录运行：

```powershell
git status --short
git rev-parse --verify HEAD
./scripts/verify.ps1
```

当前第一次执行 `git rev-parse --verify HEAD` 预计会失败，因为还没有首个 commit。创建 commit 前应重点检查：

- `.env` 没有被跟踪。
- `.forgeflow/` 没有被跟踪。
- `deploy/staging/staging.env` 没有被跟踪。
- `deploy/staging/secrets/` 中只有说明文件可以被跟踪。
- `web/node_modules/`、`web/dist/`、`bin/` 和测试产物没有被跟踪。
- 没有真实密码、API Key、Cookie、私钥和私有仓库内容。

建议执行敏感文件核对：

```powershell
git check-ignore .env .forgeflow deploy/staging/staging.env web/node_modules web/dist bin
git status --ignored --short
```

### 4.2 阶段交付物

- 本地验证日志或人工检查记录：`docs/stage-0-seal-audit.md`。
- 确认过的待提交文件清单。
- Secret、运行数据和构建产物排除结果。

### 4.3 阶段 0 退出门槛

- [x] `scripts/verify.ps1` 通过。
- [x] 技术审查确认候选提交清单中没有 Secret、运行数据和构建产物。
- [x] `.gitignore`、`.dockerignore`、高置信度 Secret 扫描和第三方依赖许可证清单已完成技术检查。
- [x] 仓库所有者已人工阅读最终待提交文件，并确认可以提交（2026-08-18）。
- [x] 仓库所有者已选择 Apache-2.0，根目录许可证及项目元数据已更新。
- [x] 仓库所有者已阅读 `docs/third-party-dependency-review.md` 并确认第三方依赖使用条件（2026-08-19）。

---

## 5. 阶段 1：手动建立 Git 和 GitHub 基线

> 当前状态：进行中（2026-08-30 Linux CI 和 OpenTelemetry Collector 配置校验已通过；`deployment-assets` 又暴露出默认 Docker driver 不支持 attestation，本地已改用 Buildx docker-container builder 和 OCI 输出；等待人工上传并重跑，`main` Ruleset 仍待配置）
> 进入条件：已满足（阶段 0 于 2026-08-19 完成）。
> 人工操作：本阶段所有提交、建仓、上传和 GitHub Settings 操作都必须手动完成。

### 5.1 手动创建首次 Git commit

> **必须手动操作：以下命令由仓库所有者在本地终端执行。不要让 Agent 自动提交。**

先预览文件，再暂存：

```powershell
git status --short
git add --all
git diff --cached --stat
git diff --cached --check
git status --short
```

确认暂存内容不含敏感信息后，手动提交：

```powershell
git commit -m "feat: establish ForgeFlow engineering baseline"
git branch -M main
git log -1 --oneline
```

如果暂存了不应提交的文件，先取消该文件暂存并完善 `.gitignore`，不要带着问题提交。

### 5.2 手动创建 GitHub 仓库

> **必须手动操作：在 GitHub 网页创建仓库。**

建议设置：

- 仓库名：`forgeflow`。
- 初始可设为 Private；安全审查完成后再决定是否 Public。
- 不要让 GitHub 自动创建 README、License 或 `.gitignore`，避免与本地首次提交冲突。
- 不要在仓库描述、Issue 或 Wiki 中填写密钥和服务器地址。

### 5.3 手动上传到 GitHub

> **必须手动上传：由仓库所有者执行 remote 和 push。本文档不会授权 Agent 自动上传。**

HTTPS 示例：

```powershell
git remote add origin https://github.com/<你的账号或组织>/forgeflow.git
git remote -v
git push -u origin main
```

SSH 示例：

```powershell
git remote add origin git@github.com:<你的账号或组织>/forgeflow.git
git remote -v
git push -u origin main
```

上传完成后，在 GitHub 网页人工确认：

- [x] 最新已上传 commit SHA 与本地 `HEAD` 一致（`0d633da`；当前另有 Buildx/OCI 修复待提交）。
- [x] `.env`、`.forgeflow`、Secret 文件和构建产物没有出现在已上传提交中。
- [x] Actions 已识别 `ci.yml`、`deployment.yml` 和 `eval.yml`。
- [x] README 和文档可以正常浏览。

如果错误上传 Secret，不要只删除最新文件；应立即撤销并轮换 Secret，然后按事故流程清理 Git 历史。

### 5.4 手动配置 GitHub 仓库治理

> **必须手动操作：在 GitHub Settings 中配置。**

为 `main` 配置 Branch Protection 或 Ruleset：

- 禁止直接推送到 `main`。
- 必须通过 Pull Request 合并。
- 至少一名审查人批准。
- 安全、Prompt、Policy、Auth、Migration 和 Sandbox 修改不得由作者自批。
- 要求 CI、Web、PostgreSQL Integration 和 Deployment Assets 检查通过。
- `deployment-assets` 必须对所有指向 `main` 的 Pull Request 运行，避免路径过滤使必需检查永久等待。
- 禁止强制推送和删除 `main`。
- 建议要求线性历史和对话已解决。

同时手动启用：

- Dependabot alerts。
- Dependabot security updates。
- Secret scanning 和 Push protection（仓库计划支持时）。
- Code scanning；如后续添加 CodeQL，先在 PR 中审核 Workflow 权限。

### 5.5 GitHub Secrets

默认 PR CI 不需要真实 OpenAI Key。只有确实建立受保护的手动 Staging Workflow 后，才在 GitHub Environment 中手动添加 Secret。

建议使用名为 `staging` 的 GitHub Environment，并启用人工审批。可配置的 Secret 名称包括：

- `STAGING_SSH_HOST`
- `STAGING_SSH_USER`
- `STAGING_SSH_KEY`
- `REGISTRY_USERNAME`
- `REGISTRY_TOKEN`

不建议把 `OPENAI_API_KEY` 和数据库主密码直接交给通用 CI。优先让服务器从专用 Secret Manager 获取。

### 5.6 阶段交付物

- 本地首个 commit SHA。
- GitHub 主仓库地址和与本地一致的远端 commit SHA。
- GitHub 基线审计记录：`docs/stage-1-github-baseline-audit.md`。
- `main` Ruleset/Branch Protection 配置截图或审计记录。
- 必需 CI 检查清单。
- GitHub Environment 和 Secret 所有者记录；不得记录 Secret 值。

### 5.7 阶段 1 退出门槛

- [x] 本地存在可解析的 `HEAD`（`0d633da`）。
- [x] 代码已由仓库所有者手动上传到 GitHub。
- [x] 最新已上传 commit 的远端 SHA 与本地 `HEAD` 一致。
- [x] GitHub 上不存在 Secret、运行数据、二进制和私有 Evidence。
- [ ] `main` 禁止直接推送并要求 Pull Request。
- [x] 最新 `ci` 在 GitHub Linux Runner 上全部通过，包括 Race Detector、浏览器 E2E 和 PostgreSQL Integration（Run `32163866073`）。
- [x] `deployment-assets` 已对所有指向 `main` 的 Pull Request 运行（`a0751a5`）。
- [x] OpenTelemetry 配置修复已人工上传并通过 Collector 0.157.0 校验（`0d633da`）。
- [ ] Buildx/OCI attestation 修复已人工上传，且 `deployment-assets` 手动重跑通过。
- [x] Dependabot alerts、Dependabot security updates、Secret scanning 和 Push protection 已确认启用。

---

## 6. 阶段 2：建立真实 Eval Fixture

> 当前状态：未开始  
> 进入条件：阶段 1 完成，主仓库已受保护且 CI 通过。  
> 本阶段目标：把 30 条任务定义变成 30 个可执行、不可变、可重复验证的真实 Case。

当前 `software/v1` 有 30 条任务定义，但 `fixtureCommit` 是占位 SHA。首先应建立独立 fixture 仓库。

### 6.1 Fixture 仓库设计

建议新建独立 Private 仓库 `forgeflow-eval-fixtures`，包含：

- 30 个可重复解析的真实 Git commit。
- 每个 commit 对应确定的初始代码状态。
- 公共测试和构建配置。
- 不向 Agent 暴露的隐藏测试不得放在 Agent 可读取的 worktree 中。
- 每个 Case 的预期决策、预算、允许文件和禁止文件。
- Fixture 版本变更记录。

隐藏测试建议放在另一个受控目录或 Private grader 仓库，由 Eval 执行器在 Agent 完成后挂载或复制；Agent 上下文不得包含隐藏测试源码。

### 6.2 创建 30 个真实 commit

每个 Case 需要完成：

1. 创建最小可运行的初始项目状态。
2. 确认公共测试初始状态符合 Case 设计。
3. 编写隐藏测试并验证它能区分正确和错误实现。
4. 创建不可变 Git commit。
5. 记录完整 40 位 SHA。
6. 使用干净 clone 重复运行一次。

完成后，把 `internal/eval/datasets/software_v1.json` 中的占位 SHA 替换为真实 SHA。

### 6.3 手动上传 Fixture 到 GitHub

> **必须手动上传：fixture 仓库的 commit 和隐藏测试仓库由所有者手动创建、审核和推送。**

不要让 ForgeFlow Agent 修改或推送 Eval fixture。上传后应在 GitHub 手动配置：

- 仓库保持 Private。
- 禁止目标 Agent 使用的 Token 读取隐藏测试仓库。
- `main` 开启保护。
- Fixture commit 不允许重写历史。
- 只有 Eval 管理员有写权限。

### 6.4 验证 Fixture

```powershell
go run ./cmd/forgeflow eval `
  --suite software/v1 `
  --validate-only `
  --fixture-repository D:\fixtures\forgeflow-eval
```

验收标准：

- [ ] 30 个 SHA 全部能由 `git cat-file` 解析为 commit。
- [ ] 没有占位 SHA。
- [ ] Agent 无法读取隐藏测试。
- [ ] 每个 Case 能在干净 worktree 中重复运行。
- [ ] Fixture 仓库没有真实用户数据和 Secret。

### 6.5 阶段交付物

- Private Fixture 仓库和访问控制记录。
- 30 个真实 40 位 commit SHA。
- 更新后的 `software_v1.json`。
- 隐藏测试隔离设计和 Grader 运行说明。
- 一份干净 clone 的重复验证记录。

### 6.6 阶段 2 退出门槛

- [ ] `--fixture-repository` 对全部 30 个 Case 验证通过。
- [ ] 占位 SHA 数量为 0。
- [ ] 30 个 Case 均能从干净 worktree 启动。
- [ ] 隐藏测试不在 Agent 可读目录和模型上下文中。
- [ ] ForgeFlow Agent 对 Fixture 和隐藏测试仓库没有写权限。
- [ ] Fixture 变更必须经过人工 Pull Request 审查。

---

## 7. 阶段 3：完成并运行真实三基线 Eval

> 当前状态：未开始  
> 进入条件：阶段 2 的 30 个 Fixture 全部验证通过。  
> 本阶段目标：完成执行器，运行三种模式并生成不含虚构数据的真实对比报告。

现有 CLI 能验证数据和根据 evidence 生成报告，但还需要自动执行三条真实链路并采集 evidence。

### 7.1 建议新增接口

在 `internal/eval` 中增加类似契约：

```go
type BaselineExecutor interface {
    Execute(ctx context.Context, evalCase Case, mode Mode) (Observation, error)
}

type EvidenceRecorder interface {
    Append(ctx context.Context, evidence Evidence) error
}
```

分别实现：

1. `single_agent`：一个 Agent 在同等模型和预算下完成任务。
2. `planner_developer`：Planner + Developer，不启用完整 Reviewer/Security/Judge。
3. `forgeflow`：完整工作流、审批、测试、Reviewer、Security、Judge 和 Repair。

### 7.2 执行器必须保证

- 每次执行从 fixture commit 建立全新 worktree。
- 三种模式使用相同任务、模型系列、最大预算和运行环境。
- 不复用上一种模式生成的文件和上下文。
- 测试结果来自实际退出码。
- 隐藏测试在 Agent 完成后由受信任 Grader 执行。
- 记录模型、Prompt、Policy、Tool、Git SHA、Token、成本和延迟。
- 超时、崩溃、拒绝和人工介入都必须形成 Observation，不能丢弃失败样本。
- Evidence 原子写入，断点恢复不会重复计费或重复统计。
- Evidence 中的路径、任务正文和模型输出在上传前按数据策略脱敏。

### 7.3 建议 CLI

完成后提供类似命令：

```powershell
go run ./cmd/forgeflow eval execute `
  --suite software/v1 `
  --fixture-repository D:\fixtures\forgeflow-eval `
  --modes single_agent,planner_developer,forgeflow `
  --output .forgeflow\evals\evidence.json
```

然后生成报告：

```powershell
go run ./cmd/forgeflow eval `
  --suite software/v1 `
  --evidence .forgeflow\evals\evidence.json `
  --format markdown `
  --output .forgeflow\evals\comparison.md
```

### 7.4 Eval 验收

- [ ] 三种模式各完成 30 个 Case，共 90 次受控执行。
- [ ] 没有缺失成本和 P95 延迟数据。
- [ ] 报告包含完成率、隐藏测试通过率、回归率、人工介入率、成本和延迟。
- [ ] 报告能追溯到精确 Git、模型、Prompt、Policy 和 Tool 版本。
- [ ] 不把结构验证结果伪装成模型成绩。
- [ ] 由人工审核报告并签署 Promotion 结论。

真实 Evidence 默认保存在 `.forgeflow/evals`，不要上传原始 Evidence 到 GitHub。只允许把经过人工脱敏的汇总报告手动提交到专门的 release-report 目录。

### 7.5 阶段交付物

- 三种 `BaselineExecutor` 实现及单元/集成测试。
- 可断点恢复的 Evidence 采集器。
- 90 次执行的原始私有 Evidence。
- JSON 和 Markdown 对比报告。
- 人工签署的 Eval 审核结论。

### 7.6 阶段 3 退出门槛

- [ ] 三种模式各 30 个 Case 均有终态 Observation。
- [ ] 失败、拒绝、超时和人工介入没有被排除在统计外。
- [ ] 成本和延迟指标完整，没有用估算值冒充真实值。
- [ ] Grader 在 Agent 工作区外运行隐藏测试。
- [ ] 报告可追溯至准确 Git、模型、Prompt、Policy 和 Tool 版本。
- [ ] 脱敏汇总报告已人工批准；原始 Evidence 未上传 GitHub。

---

## 8. 阶段 4：完善 Prompt 和模型发布治理

> 当前状态：未开始  
> 进入条件：阶段 3 已生成并人工签署真实基线报告。  
> 本阶段目标：让数据库治理记录、镜像内版本和 Worker 实际运行版本保持一致。

当前 Promotion/rollback API 会保存治理记录，但还需要保证运行中的 Worker 与 Active Release 一致。

### 8.1 需要补充的能力

- Worker 启动时读取或校验每个 Agent 的 Active Prompt Release。
- Worker 配置的 Prompt version 和 SHA 与数据库 Active Release 不一致时，Readiness 必须失败。
- Checkpoint 恢复继续校验原 Prompt、Policy、Tool 和模型版本。
- 新 Prompt 必须保留旧版本文件，确保可以真正回滚。
- Promotion 不得直接热替换正在执行的 Run。
- 模型升级与 Prompt 升级使用同样的 Eval 门禁。
- Promotion 和 rollback 写入审计日志、操作者、原因和 Eval Run ID。

### 8.2 发布顺序

1. 新增版本化 Prompt 文件，禁止原地修改 production 版本。
2. 提交候选代码并通过 PR CI。
3. 在受控 Eval 环境运行三基线或候选对照。
4. Admin 人工批准 Promotion。
5. Drain Worker。
6. 部署包含候选和回滚版本的镜像。
7. 校验 Worker Readiness 与 Active Release 一致。
8. 恢复流量。

### 8.3 回滚验收

- [ ] 回滚目标仍嵌入当前或回滚镜像。
- [ ] 回滚产生新的不可变 release 记录，不覆盖历史。
- [ ] Worker 重启后加载目标版本。
- [ ] 已运行中的 Run 不会混用两个 Prompt 版本。
- [ ] 回滚演练有时间、操作者和验证结果。

### 8.4 阶段交付物

- Worker Active Release Readiness 校验。
- Prompt 和模型 Promotion/rollback 审计记录。
- 候选版本与当前版本的 Eval 对照报告。
- 一次完整的 Promotion 和 rollback 演练记录。

### 8.5 阶段 4 退出门槛

- [ ] Worker 版本与 Active Release 不一致时 Readiness 失败。
- [ ] Promotion 不会修改正在执行的 Run 所绑定版本。
- [ ] 旧 Prompt 仍嵌入可回滚镜像。
- [ ] Checkpoint 恢复校验 Prompt、模型、Policy 和 Tool 版本。
- [ ] Promotion 和 rollback 都经过人工批准并可审计。

---

## 9. 阶段 5：手动构建并上传不可变发布镜像

> 当前状态：未开始  
> 进入条件：阶段 4 治理闭环和回滚演练通过。  
> 人工操作：Registry 登录、镜像上传和 digest 确认必须由发布负责人手动执行。

建议使用 GHCR 或独立 Registry。发布时必须使用不可变版本和 digest，不得只依赖 `latest`。

### 9.1 手动登录 Registry

> **必须手动操作：Registry 登录和镜像上传由发布负责人执行。不要把 Token 写入脚本或仓库。**

以 GHCR 为例：

```powershell
$registryUser = Read-Host "GitHub user"
$registryToken = Read-Host "GHCR token" -AsSecureString
# 使用安全方式把 token 交给 docker login，避免出现在命令历史和日志中。
```

Token 只授予需要的包权限，不要使用个人主密码。

### 9.2 手动构建和上传

> **必须手动上传到 Registry。首次正式发布前不要启用无人值守自动发布。**

发布负责人应手动构建以下镜像：

- `forgeflow-api`
- `forgeflow-worker`
- `forgeflow-web`
- `forgeflow-caddy`
- `forgeflow-sandbox`

要求：

- 使用版本号和 Git SHA 双标签。
- 生成 SBOM 和 provenance。
- 运行镜像漏洞扫描。
- 上传后记录 digest。
- Staging Compose 使用 `image@sha256:<digest>`。
- 扫描存在高危可达漏洞时禁止部署。

镜像上传完成后，把 digest 写入 Release manifest；不要把 Registry Token 写入 manifest。

### 9.3 阶段交付物

- API、Worker、Web、Caddy、Sandbox 五类镜像。
- 每个镜像的版本标签、Git SHA 标签和 digest。
- SBOM、provenance、签名及漏洞扫描结果。
- 不含凭据的 Release manifest。

### 9.4 阶段 5 退出门槛

- [ ] 五类镜像均由同一个批准 commit 构建。
- [ ] 所有 Staging 镜像引用均固定到 digest。
- [ ] 可达高危/严重漏洞数量为 0，或有正式风险接受记录。
- [ ] Registry 凭据没有写入仓库、镜像层、日志和 manifest。
- [ ] 在干净主机上可以按 digest 拉取全部镜像。

---

## 10. 阶段 6：部署真实 Staging

> 当前状态：未开始  
> 进入条件：阶段 5 的镜像、扫描和 digest 全部通过。  
> 本阶段目标：建立真实 HTTPS 环境并完成产品全链路 smoke test。

### 10.1 基础设施准备

准备：

- 一台专用 Staging 主机。
- Docker Engine 和 Compose v2。
- 域名和 DNS A/AAAA 记录。
- 只开放公网 `80/443`；SSH 限制来源。
- 独立 PostgreSQL 数据卷和 Artifact 数据卷。
- Registry 只读拉取凭据。
- OpenAI Key、数据库密码和 Alert webhook 的安全存储。
- 备份目标和异地副本。

### 10.2 手动获取代码

> **必须手动操作：服务器上的代码由运维人员从已审核的 GitHub commit/tag 获取。**

不要在服务器直接开发或修改源码。使用已签署 tag 或 commit SHA：

```bash
git clone https://github.com/<owner>/forgeflow.git
cd forgeflow
git checkout <approved-tag-or-commit>
git status --short
```

### 10.3 配置 Secret

根据 `deploy/staging/secrets/README.md` 创建 Secret 文件，并设置最小权限。禁止：

- 把 Secret 提交到 GitHub。
- 在聊天、Issue 或普通日志中粘贴 Secret。
- 把 OpenAI Key 注入 API 或 Web。
- 让 Worker 读取不需要的数据库管理密码。

### 10.4 Preflight 与发布

```powershell
Copy-Item deploy/staging/staging.env.example deploy/staging/staging.env
./scripts/staging-preflight.ps1 -RequireDigests
./scripts/staging-release.ps1 -Release 0.12.0-rc.1 -IncludeBootstrap -ConfirmDeploy
```

首次管理员成功登录后，立即删除 Bootstrap Password。启用完整模型和 Sandbox 时：

```powershell
./scripts/staging-release.ps1 -Release 0.12.0-rc.2 -IncludeOpenAI -ConfirmDeploy
```

### 10.5 Staging 验收

- [ ] HTTPS 证书可信，HTTP 自动跳转 HTTPS。
- [ ] API、Worker、PostgreSQL、Prometheus、Alertmanager 和 OTLP 不暴露公网端口。
- [ ] API 不持有 OpenAI Key 和 Docker endpoint。
- [ ] Worker 通过 mTLS 连接 sandbox-engine。
- [ ] 任务容器非 root、只读根文件系统、无网络、零 capabilities。
- [ ] 原仓库不被修改。
- [ ] 登录、创建 Run、计划审批、Patch 审批、测试、报告下载全链路通过。
- [ ] Viewer、Operator、Admin 权限符合预期。
- [ ] 日志中没有 Cookie、密码、API Key、任务正文和完整源码。

### 10.6 阶段交付物

- Staging 域名、批准版本和部署时间记录。
- Release manifest 和健康检查结果。
- 登录到报告下载的全链路证据。
- 网络端口、Secret 边界和 Sandbox mTLS 验证记录。

### 10.7 阶段 6 退出门槛

- [ ] 本节 Staging 验收项全部通过。
- [ ] API、Worker、Web 和数据库 Migration 版本一致。
- [ ] 至少一个完整 Development Run 在真实 Sandbox 内完成。
- [ ] 原 Fixture 仓库在运行前后保持不变。
- [ ] Bootstrap Secret 已删除，普通运行不再依赖 Bootstrap。

---

## 11. 阶段 7：运维和安全演练

> 当前状态：未开始  
> 进入条件：阶段 6 的真实 Staging 全链路通过。  
> 本阶段目标：证明故障、安全事件和版本失败时可以发现、恢复和回滚。

### 11.1 告警

```powershell
./scripts/staging-alert-test.ps1
```

验证：

- API/Worker Down。
- 高错误率。
- Queue backlog。
- Budget exhaustion。
- Tool policy denial。
- Login/Rate limit spike。

每条告警必须到达真实值班渠道并包含 Runbook 链接，但不得包含 Secret 和任务正文。

### 11.2 备份恢复

```powershell
./scripts/staging-backup.ps1
./scripts/staging-restore-drill.ps1 -BackupFile <backup.dump> -ConfirmRestore
```

验收：

- checksum 正确。
- 恢复目标是隔离数据库。
- Migration 版本正确。
- 恢复后的登录、Run、审批和报告链路通过。
- 已记录 RPO、RTO 和恢复耗时。

### 11.3 安全演练

```powershell
./scripts/staging-security-drill.ps1
```

至少验证：

- 路径穿越和符号链接逃逸被拒绝。
- Shell 元字符和未知命令被拒绝。
- Agent 不能修改 Policy、Prompt、Judge、Governance 和 Eval。
- 宿主 Docker Socket 没有挂载。
- Sandbox 无公网网络。
- Secret 不进入模型上下文和日志。

### 11.4 版本回滚

```powershell
./scripts/staging-rollback.ps1 -Manifest <previous-release.json> -ConfirmRollback
```

验证旧镜像、Schema 兼容性、Prompt Active Release 和 Worker Readiness。应用回滚不能自动执行 Down Migration。

### 11.5 Demo

```powershell
./scripts/demo-staging.ps1 `
  -BaseUri https://<staging-domain> `
  -Email <demo-user> `
  -Password (Read-Host -AsSecureString) `
  -RepositoryPath /repositories/demo
```

按照 `docs/demo.md` 在 3～5 分钟内完成一次可重复演示，并保存脱敏结果。

### 11.6 阶段交付物

- 告警投递截图或事件记录。
- 备份 checksum、隔离恢复记录和 RPO/RTO 实测值。
- 安全边界测试结果。
- 回滚前后 Release manifest 与健康检查结果。
- 3～5 分钟 Demo 脱敏记录。

### 11.7 阶段 7 退出门槛

- [ ] 告警实际到达值班渠道。
- [ ] 备份能够恢复到隔离数据库并通过业务 E2E。
- [ ] Sandbox、路径、命令、Secret 和治理边界演练全部通过。
- [ ] 应用版本可以回滚，且没有自动执行 Down Migration。
- [ ] Demo 可由另一名人员按文档重复完成。
- [ ] 未通过项已经阻止 Production 推进，而不是以备注代替门禁。

---

## 12. 阶段 8：Production 前必须补充的能力

> 当前状态：未开始  
> 进入条件：阶段 7 的运维与安全证据全部签署。  
> 本阶段目标：把单机 Staging 工程提升为可持续运营的 Production 方案。

当前 Compose 定位为单机 Staging。Production 不应直接照搬。

### 12.1 Production 架构

- 控制面和执行面部署在不同主机或隔离网络。
- Worker 使用专用节点，不与公网 API 共用 Docker daemon。
- PostgreSQL 使用受管服务或具备高可用、PITR 和监控的集群。
- Artifact 从单机文件存储迁移到具备加密、生命周期和访问审计的对象存储。
- Secret 使用云 Secret Manager 或 Vault，不长期依赖普通文件。
- API 至少两个实例并支持健康检查和滚动升级。
- Worker 支持 drain、容量限制和按队列扩缩容。
- 备份必须异地复制并定期恢复演练。

### 12.2 Production 安全

- 完成独立安全审查和 Threat Model 更新。
- 镜像使用 digest、签名、SBOM 和漏洞门禁。
- 配置 WAF、DDoS 防护、访问日志和管理端 MFA。
- 定义数据分类、保留期、删除和用户导出流程。
- 定义 OpenAI 数据发送范围和用户告知机制。
- 完成日志脱敏和审计日志防篡改设计。
- 对 Prompt Injection、供应链、依赖投毒和越权做专项测试。

### 12.3 SLO 和容量

发布前定义：

- API 可用性目标。
- Run 排队和完成延迟目标。
- Worker 容量与最大并发。
- 单 Run Token、成本、时长、文件和 Diff 上限。
- 告警阈值和错误预算。
- Staging 与 Production 的 RPO/RTO。

### 12.4 合规和用户功能

根据实际用户和地区补充：

- 隐私政策和服务条款。
- 用户数据删除和导出。
- 开源许可证声明。
- 第三方模型和依赖清单。
- 安全问题报告渠道。
- 管理员操作审计和保留策略。

### 12.5 阶段交付物

- Production 架构和数据流图。
- 安全评审、Threat Model 更新和风险清单。
- SLO、错误预算、容量模型、RPO/RTO 和扩缩容方案。
- 值班表、事件升级路径和变更审批流程。
- 隐私、保留、删除、导出、许可证和第三方清单。

### 12.6 阶段 8 退出门槛

- [ ] 控制面、执行面、数据库、对象存储和 Secret 边界已获批准。
- [ ] Production 不直接复用单机 Staging 的信任模型。
- [ ] 独立安全评审无未处置的阻断问题。
- [ ] SLO、容量、值班、RPO/RTO 和数据政策有明确负责人。
- [ ] Production 发布和回滚方案已经过评审。

---

## 13. 阶段 9：手动发布 v1.0.0

> 当前状态：未开始  
> 进入条件：阶段 0～8 全部标记为已完成且证据可追溯。  
> 人工操作：版本提交、签名 Tag、Tag 上传和 GitHub Release 必须由发布负责人手动执行。

只有以下条件全部满足，才允许发布：

- [ ] 首次 Git commit 和 GitHub 主仓库已建立。
- [ ] GitHub 分支保护和必需 CI 已启用。
- [ ] 30 个真实 fixture commit 已验证。
- [ ] 三基线报告已生成并人工签署。
- [ ] Prompt/模型 Promotion 与 rollback 演练通过。
- [ ] Staging HTTPS 全链路通过。
- [ ] Sandbox 安全边界通过真实 smoke test。
- [ ] 告警、备份恢复和版本回滚通过。
- [ ] Demo 可重复执行。
- [ ] govulncheck、Staticcheck、Race CI、前端和数据库检查全部通过。
- [ ] Production 架构、安全、值班、RPO/RTO 和数据政策已批准。

### 13.1 手动创建版本提交和 Tag

> **必须手动操作：版本提交和 Tag 由发布负责人执行。**

```powershell
git checkout main
git pull --ff-only
./scripts/verify.ps1
git status --short
git tag -s v1.0.0 -m "ForgeFlow v1.0.0"
git push origin v1.0.0
```

如果没有配置签名 Tag，应先建立组织认可的签名方案，不要直接降低发布要求。

### 13.2 手动发布 GitHub Release

> **必须手动上传和发布：在 GitHub Releases 页面由发布负责人操作。**

Release 应包含：

- 对应 commit 和签名 tag。
- 主要功能和已知限制。
- Migration 版本和升级步骤。
- 镜像名称、版本、digest 和签名验证方法。
- SBOM。
- 脱敏后的 Eval 汇总报告。
- 回滚步骤。

禁止上传：

- 原始 Eval Evidence。
- 用户任务或私有仓库内容。
- Secret、Cookie、数据库 dump 和私钥。
- 未经审核的运行日志。

### 13.3 阶段交付物

- 签名的 `v1.0.0` Tag。
- GitHub Release、Release Notes 和升级/回滚说明。
- 镜像 digest、签名、SBOM 和脱敏 Eval 报告。
- 最终验收签署记录。

### 13.4 阶段 9 退出门槛

- [ ] GitHub Tag 与批准 commit 完全一致。
- [ ] Release 资产不包含 Secret、原始 Evidence、数据库 dump 和私有源码。
- [ ] 发布镜像 digest 与 Staging 验收镜像一致。
- [ ] 升级、Migration、回滚和已知限制说明完整。
- [ ] 发布后健康检查和监控正常。

---

## 14. 按阶段创建下一批 Issue

在阶段 1 建立 GitHub 仓库后，由仓库所有者**手动创建**以下 Issue，并分别加入对应 Milestone：

### 阶段 2 Milestone：Eval Fixture

1. `eval: create immutable 30-case fixture repository`
2. `eval: execute hidden grader outside agent workspace`

### 阶段 3 Milestone：三基线 Eval

1. `eval: implement isolated baseline executor interface`
2. `eval: implement single-agent baseline adapter`
3. `eval: implement planner-developer baseline adapter`
4. `eval: implement full ForgeFlow baseline adapter`
5. `eval: persist resumable evidence with cost and latency`

### 阶段 4 Milestone：治理闭环

1. `governance: enforce active prompt release at worker readiness`
2. `governance: add model release and rollback records`

### 阶段 5～7 Milestone：Staging 验收

1. `security: run real sandbox boundary smoke test`
2. `ops: deploy first public HTTPS staging release`
3. `ops: verify alert delivery and on-call runbooks`
4. `ops: complete isolated backup restore drill`
5. `ops: complete immutable image rollback drill`

### 阶段 8～9 Milestone：Production 与发布

1. `ops: approve production architecture and SLO`
2. `security: complete independent production review`
3. `docs: record signed v1.0.0 acceptance evidence`

每个 Issue 必须包含：

- 验收标准。
- 测试方法。
- 安全影响。
- 数据与隐私影响。
- 明确的不在本 Issue 内的范围。
- 是否需要手动 GitHub、Registry 或服务器操作。

---

## 15. 阶段状态跟踪表

执行过程中只维护下表，不跨阶段并行执行发布操作：

| 阶段 | 状态 | 负责人 | 开始日期 | 完成日期 | 证据位置 |
|---|---|---|---|---|---|
| 0 本地封板审查 | 已完成 | 仓库所有者 | 2026-08-18 | 2026-08-19 | `docs/stage-0-seal-audit.md`、`docs/third-party-dependency-review.md` |
| 1 Git 与 GitHub 基线 | 进行中 | 仓库所有者 | 2026-08-18 |  | `docs/stage-1-github-baseline-audit.md` |
| 2 真实 Eval Fixture | 未开始 | 待填写 |  |  |  |
| 3 三基线 Eval | 未开始 | 待填写 |  |  |  |
| 4 Prompt/模型治理 | 未开始 | 待填写 |  |  |  |
| 5 不可变发布镜像 | 未开始 | 待填写 |  |  |  |
| 6 真实 Staging | 未开始 | 待填写 |  |  |  |
| 7 运维与安全验收 | 未开始 | 待填写 |  |  |  |
| 8 Production 准备 | 未开始 | 待填写 |  |  |  |
| 9 v1.0.0 发布 | 未开始 | 待填写 |  |  |  |

当前处于**阶段 1：Git 与 GitHub 基线**。阶段 0 已全部完成；阶段 1 已完成人工首次提交和上传，下一步由仓库所有者手动提交并上传 Go 1.26.6 修复，等待必需 CI 通过，再配置并确认 `main` Ruleset、Secret scanning 和依赖安全功能。阶段 1 全部门槛通过前不要进入 Eval、服务器部署或版本发布。
