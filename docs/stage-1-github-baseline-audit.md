# ForgeFlow 阶段 1：Git 与 GitHub 基线审计

> 审计日期：2026-08-18
> GitHub 仓库：https://github.com/wxxsimply/forgeflow
> 当前状态：进行中
> 进入条件：已满足（阶段 0 于 2026-08-19 完成）

## 1. 已完成事实

- 本地 `main` 已有可解析的首个提交：`11473a7eaff97af1e1f92725dba3a49ef71488ed`。
- `origin/main` 与该提交一致，仓库所有者已完成人工首次上传。
- GitHub 已识别 `ci`、`deployment-assets` 和 `full-eval-contract` 三个工作流。
- GitHub 上的 README 可访问。
- 已上传提交来自通过 Secret、运行数据和构建产物排除检查的候选文件。

## 2. CI 现状

首次 `ci` 运行：

- Run ID：`32147400648`
- 提交：`11473a7eaff97af1e1f92725dba3a49ef71488ed`
- 结论：失败
- 失败原因：Go 1.26.5 标准库出现新的安全公告，`govulncheck` 要求升级到 Go 1.26.6。

本地修复已完成：

- `go.mod`、构建镜像和文档基线已升级到 Go 1.26.6。
- `go test -timeout 300s ./...` 通过。
- `go vet ./...` 通过。
- Staticcheck 2026.1 通过。
- govulncheck 1.6.0 结果为 0 个可达漏洞。
- `./scripts/verify.ps1` 完整验证通过，包括 Go 测试、构建、Migration、OpenAPI 客户端生成、TypeScript、前端测试和生产构建。
- `git diff --check` 通过。

修复仍在本地工作区，尚未由仓库所有者人工提交和上传，因此远端 CI 还没有新的成功证据。

## 3. GitHub 治理现状

只读检查结果：

- `main` Branch Protection API 返回“Branch not protected”。
- 仓库 Ruleset 列表为空。
- Secret scanning、Push protection、Dependabot 和 Code scanning 尚待仓库所有者在 Settings 页面人工确认。

在 Ruleset 生效前不要开始阶段 2。建议 Ruleset 至少要求：

- 只允许通过 Pull Request 合并到 `main`。
- 至少 1 名审查人批准并要求解决全部对话。
- 禁止强制推送和删除 `main`。
- 要求线性历史。
- 将 `Go verification`、`Web verification`、`PostgreSQL integration` 和 `deployment-assets / validate` 设为必需检查；检查名称以 GitHub 页面实际显示为准。

## 4. 必须人工完成的操作

### 4.1 提交并上传 Go 1.26.6 修复

仓库所有者应先阅读差异，再手动执行：

```powershell
git status --short
git diff --check
git diff
git add Dockerfile deploy/sandbox/Dockerfile go.mod README.md FORGEFLOW_GO_IMPLEMENTATION_GUIDE.md FORGEFLOW_POST_IMPLEMENTATION_ROADMAP.md docs/completion-audit.md docs/stage-0-seal-audit.md docs/third-party-dependency-review.md docs/stage-1-github-baseline-audit.md
git diff --cached --check
git commit -m "build: upgrade Go toolchain to 1.26.6"
git push origin main
```

### 4.2 等待并检查 GitHub Actions

上传后在 Actions 页面确认新提交的 `ci` 全部通过，包括 Linux Race Detector。随后手动运行 `deployment-assets`，确认五类镜像和部署配置校验通过。

### 4.3 配置仓库治理

在 GitHub `Settings` 中手动创建 `main` Ruleset/Branch Protection，并启用仓库计划支持的 Dependabot alerts、Dependabot security updates、Secret scanning、Push protection 和 Code scanning。

## 5. 阶段退出检查

- [x] 本地存在可解析的 `HEAD`。
- [x] 首次代码已由仓库所有者手动上传。
- [x] 已上传提交的远端 SHA 与本地 `HEAD` 一致。
- [x] README 与三个工作流可在 GitHub 访问。
- [ ] Go 1.26.6 修复已人工提交并上传。
- [ ] 新提交的必需 CI 全部通过。
- [ ] `deployment-assets` 手动运行通过。
- [ ] `main` 已禁止直接推送并要求 Pull Request。
- [ ] 分支保护和依赖/Secret 安全功能已人工确认。
