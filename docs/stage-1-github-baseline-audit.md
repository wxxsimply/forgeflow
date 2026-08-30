# ForgeFlow 阶段 1：Git 与 GitHub 基线审计

> 审计日期：2026-08-30
> GitHub 仓库：https://github.com/wxxsimply/forgeflow
> 当前状态：进行中
> 进入条件：已满足（阶段 0 于 2026-08-19 完成）

## 1. 已完成事实

- 本地 `main` 已有可解析的最新提交：`589256a0b910d272f91e2f5bb989b3ce789e4348`。
- `origin/main` 与该提交一致，仓库所有者已完成人工上传。
- GitHub 已识别 `ci`、`deployment-assets` 和 `full-eval-contract` 三个工作流。
- GitHub 上的 README 可访问。
- 已上传提交来自通过 Secret、运行数据和构建产物排除检查的候选文件。

## 2. CI 现状

首次 `ci` 运行：

- Run ID：`32147400648`
- 提交：`11473a7eaff97af1e1f92725dba3a49ef71488ed`
- 结论：失败
- 失败原因：Go 1.26.5 标准库出现新的安全公告，`govulncheck` 要求升级到 Go 1.26.6。

Go 1.26.6 修复及验证：

- `go.mod`、构建镜像和文档基线已升级到 Go 1.26.6。
- `go test -timeout 300s ./...` 通过。
- `go vet ./...` 通过。
- Staticcheck 2026.1 通过。
- govulncheck 1.6.0 结果为 0 个可达漏洞。
- `./scripts/verify.ps1` 完整验证通过，包括 Go 测试、构建、Migration、OpenAPI 客户端生成、TypeScript、前端测试和生产构建。
- `git diff --check` 通过。

仓库所有者已人工创建提交 `6b67b09` 并上传。对应 GitHub Actions `ci` Run `32161234214` 已成功，以下 Linux Job 全部通过：

- `Go verification`：格式、测试、最小 Eval、Race Detector、Vet、Staticcheck、govulncheck、Migration 和构建全部通过。
- `Web verification`：类型检查、单元测试、构建、OpenAPI 差异和 Chromium E2E 全部通过。
- `PostgreSQL integration`：真实 PostgreSQL 服务下的 Migration 和集成测试通过。

最新 `main` 的 `ci` Run `33305651867` 也已成功，继续覆盖上述三项必需检查。

## 3. GitHub 治理现状

只读检查结果：

- Active Ruleset `forgeflow`（ID `21852556`）已应用到默认分支 `main`。
- `main` 必须通过 Pull Request 合并，禁止删除和强制推送，并要求线性历史及解决全部审查对话。
- 必需检查为 `Go verification`、`Web verification`、`PostgreSQL integration` 和 `validate`，且分支必须保持最新。
- 当前为单维护者模式，所需批准数为 0；所有者自己的 Pull Request 可在四项检查通过后合并。引入协作者后应改为至少 1 名非作者批准。
- Secret scanning：已启用。
- Push protection：已启用。
- Dependabot alerts：已启用，REST API 返回 HTTP 204。
- Dependabot security updates：已启用。
- Code scanning：尚待仓库所有者确认或配置。

为避免路径过滤导致某些 Pull Request 永远等不到必需检查，`deployment-assets` 已改为对所有指向 `main` 的 Pull Request 运行。仓库所有者已人工上传提交 `a0751a5`，其 `ci` Run `32163866073` 通过。

本地使用 actionlint 1.7.7 检查更新后的 `deployment.yml`，结果通过。

## 4. 必须人工完成的操作

### 4.1 提交并上传 Go 1.26.6 修复（已完成）

仓库所有者已于 2026-08-19 手动执行提交和上传，提交为 `6b67b09`，远端 CI 已通过。

### 4.2 手动运行 Deployment Assets

在 GitHub Actions 页面选择 `deployment-assets`，点击 **Run workflow**，分支选择 `main`。也可以由仓库所有者手动执行：

```powershell
gh workflow run deployment.yml --repo wxxsimply/forgeflow --ref main
```

等待运行结束，确认 Compose、Caddy、Prometheus、Alertmanager、OpenTelemetry Collector 和五类镜像构建全部通过。

首次手动运行 `32163878250` 已结束但失败。失败发生在 OpenTelemetry Collector 配置校验：0.157.0 不再接受旧的 `service.telemetry.metrics.address`。该问题已在提交 `0d633da` 中迁移为 `metrics.readers.pull.exporter.prometheus`，继续暴露 `0.0.0.0:8888`；后续 Run `33305238725` 已通过 Collector 配置校验。

Run `33305238725` 随后在镜像构建阶段失败：默认 Docker driver 配合 `--load` 不支持 provenance/SBOM attestations。修复已改用 `docker/setup-buildx-action@v3` 的 docker-container builder，并将五类构建结果输出为 OCI archive。

仓库所有者已人工上传修复提交 `589256a`。后续 `deployment-assets` Run `33305656504` 成功，Compose、Caddy、Prometheus、Alertmanager、OpenTelemetry Collector 校验及五类带 attestation 的 OCI 镜像构建均通过。

### 4.3 配置仓库治理

GitHub `Settings` 中的 `main` Ruleset 已创建并启用。Dependabot alerts、Dependabot security updates、Secret scanning 和 Push protection 已启用；仍需确认或配置 Code scanning。

### 4.4 通过首个受保护 Pull Request 修复依赖告警

Dependabot 当前报告 13 个打开的告警，全部来自直接运行时依赖 `golang.org/x/crypto v0.51.0`，对应公告均在 `v0.52.0` 修复。本地分支 `codex/security-xcrypto-0.52` 已完成依赖升级，等待仓库所有者人工提交、推送并创建 Pull Request。

该 Pull Request 是 Ruleset 的首次实战验证：四项必需检查全部成功后，仓库所有者可以自行 Squash merge，无需邀请其他协作者。合并后应重新检查 Dependabot 页面，确认 13 个告警全部关闭。

## 5. 阶段退出检查

- [x] 本地存在可解析的 `HEAD`。
- [x] 首次代码已由仓库所有者手动上传。
- [x] 已上传提交的远端 SHA 与本地 `HEAD` 一致。
- [x] README 与三个工作流可在 GitHub 访问。
- [x] Go 1.26.6 修复已人工提交并上传（`6b67b09`）。
- [x] 最新提交的必需 CI 全部通过（Run `33305651867`）。
- [x] `deployment-assets` 手动运行通过（Run `33305656504`）。
- [x] `deployment-assets` 已对所有指向 `main` 的 Pull Request 运行，避免必需检查因路径过滤缺失（`a0751a5`）。
- [x] `main` 已禁止直接推送并要求 Pull Request（Ruleset `21852556`）。
- [x] Dependabot alerts、Dependabot security updates、Secret scanning 和 Push protection 已确认启用。
- [ ] `golang.org/x/crypto v0.52.0` 已通过受保护 Pull Request 合并，13 个 Dependabot 告警已关闭。
