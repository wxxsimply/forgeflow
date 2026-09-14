# 阶段 C：个人预览更新操作单

## 1. 当前结论

2026-09-14 已核对：PR #52 合并为 `6156165e0ea501093e3e31d5cffedd5118aeecfd`，Go、Web、PostgreSQL 和部署资产四项检查全部通过。之前的中文界面 PR #51 与 SNI 修复 PR #50 也已合并，不需要重复提交它们。

本轮从 PR #52 的合并版本新建 `codex/preview-rollout-preflight` 分支，只增加部署前后检查和操作说明。**没有更新服务器，没有付费调用，也没有执行真实改代码任务。**

当前两个限制：

- SSH 返回 `Connection closed by 39.102.136.31 port 22`，线上版本、目录、任务和证书状态本轮未核实。这个提示本身不能确定是安全组、网络还是 SSH 配置问题，不据此修改安全组。
- 本机 Docker Engine 当前不可连接；离线检查通过，真实 Docker 模板测试留给本 PR 的 CI。没有把跳过的检查写成通过。

## 2. 新的只读预检工具

文件：`scripts/preview_rollout_preflight.py`。服务器需有 Python 3.10+、Git、Docker CLI 及读取指定容器的权限。工具总计最多运行约 90 秒，单条命令最多 8 秒。

它不会安装软件、编辑环境文件、部署镜像、停止服务、取消任务或清理数据库。它也不会打印环境文件、模型密钥、任务描述、原始日志或数据库连接串。

检查范围：

- 源码提交是否是人工指定版本、工作区是否干净。
- ForgeFlow 五个指定服务是否健康，API/Caddy 是否保留公网 HTTPS overlay，是否误用 Bootstrap。
- API/Worker 是否仍为用户 `10001:10001`，是否保持模拟模式和关闭真实 Docker 执行。
- API 仓库挂载是否只读，Worker 是否只挂载已知受控仓库根。
- 内部服务是否没有发布宿主机端口，Caddy 是否仅发布 443 和回环 8080，证书是否保存在持久卷。
- 旧 pending 请求、未结束任务、待处理队列和未发布通知的数量是否为零。只查计数，不自动处理。
- 更新后再检查：是否切换到专用演示根，API 是否能实际读取演示仓库，API/Web 运行版本及 Worker 镜像版本标识是否匹配目标提交。

返回码 `0`：本次只读检查通过；`1`：失败或证据不完整；`2`：参数错误。即使返回 `0`，也不意味着部署已获授权、服务正式可用或云安全组已验证。

输出包含主机路径和镜像标识，仍应保存在私有运维记录中。不要直接将完整输出复制到公开 PR。

## 3. 现在需要的人工操作：先提交本 PR

本机 PowerShell，每步成功后再执行下一步：

```powershell
cd D:\Code\forgeflow
git branch --show-current
git status --short
```

分支应为 `codex/preview-rollout-preflight`。然后：

```powershell
git add FORGEFLOW_NEXT_STAGE_PLAN.md FORGEFLOW_PERSONAL_PREVIEW_PLAN.md
git add docs/preview-reliability-handoff.md docs/preview-rollout-plan.md
git add scripts/preview_rollout_preflight.py scripts/test_preview_rollout_preflight.py
git add .github/workflows/deployment.yml
git diff --cached --check
git diff --cached --stat
git commit -m "ops: add read-only preview rollout preflight"
git push -u origin codex/preview-rollout-preflight
gh pr create --base main --head codex/preview-rollout-preflight --title "ops: add read-only preview rollout preflight" --body "增加只读部署预检和离线测试，更新 PR #52 合并记录，明确专用演示目录、HTTPS 保留、版本核对及回退步骤。未修改服务器，未调用付费模型。"
gh pr checks --watch
```

不要使用 `git add .`，不要提交面试文档和 PDF。不要重新打开已合并的 #52。新 PR 需要人工创建并合并；其 CI 结果不能用 #52 的通过记录代替。

## 4. 合并后再做：恢复 SSH 并确认变更窗口

这些不是现在已经执行的操作，也不要未获确认就复制后面的部署命令。

- 用户在可信网络验证 `ssh forgeflow@39.102.136.31` 能登录。失败时只诊断，不重置密码，不开放全网 SSH。
- 用户确认允许短时停止 ForgeFlow API/Worker，并明确不影响 TTS、不删除数据库和证书卷。
- 在私有记录中保存当前源码提交、API/Worker/Web 镜像名称及不可变 image ID、原挂载路径、Compose 文件组合和备份位置。
- 核对当前数据库迁移版本与目标版本；本轮预检代码和 PR #52 没有新增迁移，但必须确认线上实际基线，不能假定服务器早已同步。
- 在确认前不要把模型密钥上传服务器，也不要开启真实执行。

## 5. 更新顺序与命令边界

以下命令在服务器 `/srv/forgeflow/app` 使用。本轮新 PR 合并后，先核对最新合并 SHA，再填写 `CANDIDATE_COMMIT`，不能固定沿用旧的 #52 SHA。

```bash
cd /srv/forgeflow/app
CANDIDATE_COMMIT=填入本轮合并后的40位提交编号
python3 -B scripts/preview_rollout_preflight.py --expected-commit "$CANDIDATE_COMMIT" --phase before
```

执行前，源码必须已在人工确认下同步到该候选版本。预检失败时停止。尤其是旧 pending 或未结束任务，要逐条人工核对，不能删表、换请求标识重试或批量取消来制造“通过”。

统一使用基础和 HTTPS 两个 Compose 文件：

```bash
ffcompose() {
  docker compose --env-file deploy/personal-preview/preview.env \
    -f deploy/personal-preview/compose.yaml \
    -f deploy/personal-preview/compose.public-ip.yaml "$@"
}
```

经过变更确认后，按下列顺序进行；任何失败都停下核查，不继续复制下一步：

1. 私下备份原 `preview.env`，记录原镜像标识；不要把备份加入 Git。先验证可回退所需旧镜像仍在本机，不清理镜像。
2. 执行 `sh scripts/prepare-preview-demo.sh --check`，确认固定新目录无冲突后，再人工执行 `sudo sh scripts/prepare-preview-demo.sh --apply`。旧仓库目录保持不变。
3. 在私有环境文件设置本轮唯一的 `FORGEFLOW_RELEASE` 标签、完整 `FORGEFLOW_GIT_COMMIT` 和新路径 `FORGEFLOW_REPOSITORY_PATH=/srv/forgeflow/preview-repositories`，保留现有 HTTPS 参数。不要覆盖旧版本镜像标签。
4. 执行 `ffcompose config --quiet`。在服务仍运行时先执行 `ffcompose build api worker web`，构建失败不进入停机步骤。构建可能耗时，安排在用户确认的窗口。
5. 在维护窗口阻止新的业务操作，再执行 `ffcompose stop -t 90 api worker`。这不停止数据库、Caddy 或 TTS。仅凭之前计数为零不够，停止后仍需只读复核数据库的 pending、运行中任务、队列和 Outbox 计数；出现新增工作则暂停并人工处理。
6. 确认无需数据库迁移后，执行 `ffcompose up -d --no-deps --no-build api worker web`。不能省略 `--no-deps`，不能运行全项目 `down`、`down -v`、卷删除或清理镜像命令。
7. 等待三个服务健康后，运行下面的 `after` 检查。失败时进入回退核对，不开始真实任务。

```bash
python3 -B scripts/preview_rollout_preflight.py --expected-commit "$CANDIDATE_COMMIT" --phase after
```

注意：预检是一次瞬时快照，不是维护锁；脚本不会替用户停止入口或持有数据库锁。源码同步、备份、环境文件编辑、构建和重启均需在后续获得明确确认后执行。

## 6. 回退步骤

只在已记录的旧镜像和环境备份确实存在时进行：

1. 暂停新任务，并停止本次更新的 ForgeFlow API/Worker。
2. 恢复原 `preview.env`；若更新过程中修改了部署配置，也恢复已记录的配置版本，不能覆盖无关本地变更。
3. 使用原有两个 overlays，确认 `ffcompose config --quiet` 通过，核对 image ID 确为原版本，再执行 `ffcompose up -d --no-deps --no-build api worker web`。
4. 重新检查健康、HTTPS 和登录。没有数据库迁移时不回滚数据库；保留数据库、证书、Artifacts、Workspaces 和新演示目录供排查。

旧后端会重新引入旧去重风险；回退后保持新任务暂停，不能宣称问题已解决。

## 7. 浏览器人工验收

- HTTPS 直接访问公网 IP 无证书警告，登录和注销正常。
- 登记 `/repositories/demo`，默认基线 `main`，不再出现权限错误。
- 创建模拟任务，检查审批及结果页面；确认长中文输入会得到正确提示。
- 在维护后允许任务进入，再做这一轮模拟检查；此时有未结束任务可能使预检的空闲检查失败，不应据此删除任务。
- 公网 8080 仍不可访问，SSH 仍限制可信来源，TTS 服务保持原状。

这些结果必须由用户实际确认。模拟通过后仍然不等于真实自动改代码通过；阶段 D 的模型、数据范围、预算和隔离环境授权需单独确认。

## 8. 本轮检查记录

- 已核实 #52 合并状态及四项检查成功；新 PR 尚未提交。
- 8 项离线单元测试通过，耗时约 0.01 秒；覆盖故障阻断、版本、仓库根、端口、旧请求计数和输出保护。
- 新增的真实 Docker 模板测试本机未运行，因为 Docker Engine 不可用；已接入 CI，复用现有 Caddy 校验步骤拉取的镜像，只创建不启动的临时容器，不挂载宿主机文件。
- 实际命令入口在本机 Docker 缺失时返回阻断状态，未泄露原始错误输出；工作流 YAML 解析和测试步骤顺序检查通过，修改及新增文件的空白格式检查通过。
- 没有重跑全量 Go/Web/Eval 测试，本轮不修改应用业务代码。
- SSH 连通失败，线上预检和部署仍未完成。
