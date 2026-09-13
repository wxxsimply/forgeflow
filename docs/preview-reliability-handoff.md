# 个人预览修复交接

## 范围与停止点

本轮在本地准备修复，不修改服务器、不调用模型、不提交或推送 GitHub。上线前先人工合并，再确认服务器变更。

## 仓库权限方案

旧目录 `/srv/forgeflow/repositories` 的用户编号与容器 `10001:10001` 不匹配。为避免修改旧目录或其中其他仓库，准备一个新的专用根目录：`/srv/forgeflow/preview-repositories`。

- 根目录和 `demo` 子目录由 `10001:10001` 所有，权限 `750`；文件 `640`。
- API 沿用只读挂载；Worker 沿用可写挂载，但只挂载这个专用演示根目录。
- Worker 的 Git worktree 创建需要更新演示仓库的 `.git/worktrees` 元数据，所以不能简单把 Worker 仓库根也设为只读。工作文件应写入独立 worktree，演示仓库主工作树与原有其他仓库不能被改变。
- 不添加通配符 `safe.directory`，不改为 root 运行，不开放公网 8080，不启用真实 Docker 执行。

脚本固定目标路径，只复制四个公开模板文件；默认只检查，遇到符号链接、不符合预期的目录或初始化残留则停止，不递归覆盖。重复运行不会重置已有演示代码。

## 合并后由用户确认的服务器步骤

1. 记录实际运行镜像和配置版本，备份受控环境文件；备份可能含敏感信息，只能保存在服务器私有位置。
2. 在已更新到合并提交的 `/srv/forgeflow/app` 执行 `sh scripts/prepare-preview-demo.sh --check`。
3. 确认作用范围后执行 `sudo sh scripts/prepare-preview-demo.sh --apply`。这只初始化本地演示 Git 仓库，不涉及 GitHub。
4. 在私有 `preview.env` 将 `FORGEFLOW_REPOSITORY_PATH` 改成 `/srv/forgeflow/preview-repositories`。旧值记入私有回退记录。
5. 核对本机实际使用的基础和 HTTPS overlay 配置，使用新版本镜像只更新 ForgeFlow API、Worker、Web。不要照搬只包含基础配置的启动命令，以免丢失 HTTPS 设置；不要运行全项目 `down -v`。
6. API 容器内以默认用户检查 `id`、`ls /repositories/demo` 以及 `git -C /repositories/demo rev-parse HEAD`。
7. 网页登记路径填 `/repositories/demo`，默认基线填 `main`；完成登录、模拟任务、审批和注销验证。

数据库结构不变，不需要新增迁移。真实模型和代码执行仍关闭。

回退：恢复原环境文件中的挂载路径及旧镜像，按相同 overlays 仅重建 ForgeFlow 服务。旧目录从未改动，不需要恢复旧目录权限。新演示目录保留以便检查，不自动删除；回退到旧代码也会重新引入旧去重风险，应避免继续创建任务。

## 去重修复及旧记录处理

新请求的去重记录、任务、Checkpoint（进度存档）和 Outbox（队列通知）在同一数据库事务中提交；失败一起回滚，相同请求重试读取已存在的任务。

已完成请求的标识有效期保持 24 小时，过期后可以用于新任务。这个保证不是永久去重，也不能防止用户换一个标识主动再次提交。

旧版本可能已留下没有任务关联的 `pending` 记录。新代码会明确拒绝自动重用，包括已过期的旧 pending。不能靠删除记录或换标识盲目重试，否则可能执行两次。

部署前暂停新的任务创建并由管理员在受控环境中检查旧 pending：核对所有者、时间、任务和执行记录。能够证明对应唯一任务时再制定关联修复；能证明未创建任务时才考虑删除对应单条记录；无法确认时保持阻断。本文不提供批量清理命令，也不宣称已修复现有线上历史记录。

## 检查记录

执行日期：2026-09-13 夜间至 09-14。本地分支 `codex/preview-reliability`，基于 `93c36fe`，结果对应本轮待提交工作区，不是线上版本。

- `go test ./internal/httpapi ./internal/checkpoint ./internal/application ./internal/postgres -count=1 -timeout=4m -v`：通过。使用本轮新建的本机 PostgreSQL 17 临时数据库；四个包报告耗时分别约 4.6、0.4、25.3、2.9 秒，不包含编译时间。
- `go vet ./internal/httpapi ./internal/checkpoint ./internal/application ./internal/controlplane ./internal/postgres`：通过。
- 前端类型检查及 `WorkflowPages.test.tsx`、`taskInput.test.ts`：13 项通过，测试耗时 7.23 秒。
- 演示模板 `go test ./... -count=1 -timeout=1m`：通过，0.58 秒。
- 无网络临时容器验证：默认检查不写入、首次初始化、重复执行、拒绝符号链接和错误所有者均通过；UID 10001 能创建独立 worktree。
- 额外使用只读卷挂载验证：UID 10001 能读 Git 仓库和源码，尝试写入被只读文件系统拒绝。
- OpenAPI 类型重新生成完成；本轮未修改数据库结构或 Compose 文件。
- 上述检查分批执行，每次均未达到 10 分钟上限；这不是全量 CI 的耗时承诺。没有执行根项目全量 `go test ./...`、完整 CI、生产构建、线上部署、浏览器验收或真实模型调用。

临时容器使用已有本地应用镜像验证权限，不代表新后端镜像已经构建或部署。上线时仍须部署本轮合并后的代码。

## 人工 GitHub 提交步骤

在本机 `D:\Code\forgeflow` 的 PowerShell 执行。不要使用 `git add .`，避免把未提交的面试资料一起带入。

```powershell
git branch --show-current
git status --short
git add FORGEFLOW_NEXT_STAGE_PLAN.md docs/preview-reliability-handoff.md
git add internal/application/service.go internal/checkpoint/postgres.go internal/checkpoint/idempotency.go internal/controlplane/postgres.go
git add internal/httpapi/server.go internal/httpapi/integration_test.go internal/httpapi/idempotency_test.go internal/httpapi/openapi.yaml
git add web/src/pages/NewRunPage.tsx web/src/WorkflowPages.test.tsx web/src/utils/taskInput.ts web/src/utils/taskInput.test.ts web/src/api/schema.d.ts
git add scripts/prepare-preview-demo.sh scripts/test-preview-demo.sh
git add examples/preview-demo/README.md examples/preview-demo/go.mod examples/preview-demo/greeting.go examples/preview-demo/greeting_test.go
git diff --cached --check
git diff --cached --stat
git commit -m "fix: harden preview task creation and repository setup"
git push -u origin codex/preview-reliability
gh pr create --base main --head codex/preview-reliability --title "fix: harden preview task creation and repository setup" --body "修复中文任务字节限制和原子请求去重；增加受限演示仓库初始化脚本、故障回滚测试及交接文档。未部署服务器，未调用付费模型。"
gh pr checks --watch
```

每一步成功后再执行下一步。若分支名不是 `codex/preview-reliability`，或暂存清单出现无关文件，先停止核对；推送失败时不要继续创建 PR。GitHub 检查未通过或存在合并冲突时不要强制合并，也不要绕过必需检查。

PR 检查通过后由用户在网页合并，并告知“已合并”。下一步才是确认新版本部署、专用演示目录切换及浏览器验证。
