# ForgeFlow 阶段 2：真实 Eval Fixture 审计

> 审计日期：2026-08-30  
> 当前状态：已完成（2026-08-30；采用 Private + Archived 等效不可变控制）
> 数据集：`software/v1`，30 Cases

## 1. 本地交付物

- 公开 Fixture 模板：`evals/fixtures/public/`。
- 可复现生成器：`cmd/forgeflow-fixture-builder`。
- Fixture 锁定清单：`evals/software-v1-fixtures.lock.json`。
- 本地 Fixture Git 仓库：`D:\fixtures\forgeflow-eval-fixtures`。
- 本地私有 Grader Git 仓库：`D:\fixtures\forgeflow-eval-grader`。
- 数据集中的 30 个 `fixtureCommit` 已回填为真实且唯一的 40 位 SHA。

公开 Fixture 仓库当前基线提交为 `6ebdc5d14c69d7867b569cf0e19d34c7b60f3a4f`，包含 30 个 `fixture/software-v1/<case-id>` 分支和 30 个 `software-v1/<case-id>` 标签。私有 Grader 当前提交为 `5942ec84d403e37385203b4c7851d1b92573548a`。

## 2. 远端仓库状态

- Fixture：https://github.com/wxxsimply/forgeflow-eval-fixtures（Private）。
- Grader：https://github.com/wxxsimply/forgeflow-eval-grader（Private）。
- Fixture 远端 `main`、30 个 Case 分支和 30 个固定标签均已上传；远端 `main` SHA 与本地一致。
- Grader 远端 `main` SHA 与本地一致。
- 两个仓库的 GitHub Actions 均已关闭，减少隐藏源码进入通用 CI 的风险。
- 已从两个 Private 远端重新执行干净 clone；正式 30 SHA 验证和 Grader 双向审计再次通过。

GitHub Ruleset API 对两个 Private 仓库均返回 HTTP 403：当前账号必须升级 GitHub Pro 或将仓库改为 Public 才能启用该功能。仓库所有者明确决定不升级 Pro，且 Grader 绝不能改为 Public，因此改用 GitHub Free 可执行的等效控制：两个仓库均已 Archived。GitHub 归档后，代码、提交、分支、标签和权限对所有用户只读；Fixture 写入 dry-run 已返回 HTTP 403。新数据集版本必须先由所有者解除归档，完成人工文件/SHA/隐藏测试审计后重新归档。

Fixture 仓库另配置了 `ForgeFlow Eval Fixture Readonly` Deploy Key（GitHub Key ID `161754241`，ED25519 指纹 `SHA256:JcGiXXpNrY1BqrixsDUJykzVcMkFlIEdNWB1s95QMaA`）。通过 `ssh.github.com:443` 验证该密钥可以读取 Fixture、不能写入，并且无法访问 Grader。私钥不在任何 Git 仓库中，后续只允许作为 Staging/Eval 执行环境的受保护 Secret 注入。

## 3. 隔离设计

- Fixture commit 只包含公开任务起点、公共测试和 `CASE.json`，不包含隐藏测试、Golden 实现、凭据或真实用户数据。
- 隐藏测试和 Golden 覆盖层只存在于独立 Grader 仓库。
- Grader 只在 Agent 执行结束后临时把单项隐藏测试写入完成的 worktree，测试结束立即移除。
- 实现类 Case 不向 Agent 暴露隐藏测试源码；决策类 Case 只读取终态 Observation。
- 真实部署时，Agent Token 必须无权读取 Grader 仓库，也不得写入 Fixture 仓库。

## 4. 验证证据

ForgeFlow 正式 CLI 验证结果：

```json
{
  "checkedCases": 30,
  "dataset": "software/v1",
  "fixturesVerified": true,
  "status": "valid",
  "totalCases": 30,
  "version": "2026-08-30"
}
```

私有 Grader 双向审计结果：

```json
{
  "dataset": "software/v1",
  "cases": 30,
  "baselineRejected": 25,
  "goldenAccepted": 25,
  "decisionPairsPassed": 5,
  "passed": true
}
```

该结果证明：25 个实现类原始 Fixture 均无法绕过对应隐藏测试，私有正确实现均能通过；5 个澄清、拒绝或人工审批类 Case 均能拒绝错误决策并接受正确决策。30 个 Case 还分别从干净 clone 执行了公开验证命令。

## 5. 复现命令

重新生成时目标目录必须不存在，生成器不会覆盖现有仓库：

```powershell
go run ./cmd/forgeflow-fixture-builder `
  --fixture-repository D:\fixtures\forgeflow-eval-fixtures `
  --update-dataset
```

验证数据集中的全部 SHA：

```powershell
go run ./cmd/forgeflow eval `
  --suite software/v1 `
  --validate-only `
  --fixture-repository D:\fixtures\forgeflow-eval-fixtures
```

在私有 Grader 仓库运行双向审计：

```powershell
cd D:\fixtures\forgeflow-eval-grader
go run ./cmd/grader `
  --verify-suite `
  --fixture-repository D:\fixtures\forgeflow-eval-fixtures
```

## 6. GitHub 操作状态

1. [x] 两个 Private 仓库已创建且没有错误的初始化提交。
2. [x] Fixture `main`、全部 30 个 Case 分支和全部标签已上传。
3. [x] Grader 仅上传到独立 Private 仓库。
4. [x] 两个仓库的 Actions 已关闭。
5. [x] 远端干净 clone 的 30 SHA 验证和双向审计已通过。
6. [x] 两个仓库已 Archived，以全仓库只读替代 GitHub Free 不支持的 Private Ruleset。
7. [x] Fixture Deploy Key 已验证为只读且无法访问 Grader；私钥未进入 Git 仓库。

主 ForgeFlow 仓库中的本阶段改动也必须由仓库所有者人工审核、提交、推送分支并通过 Pull Request 合并。

## 7. 阶段退出状态

- [x] 30 个真实唯一 commit 已生成并回填。
- [x] 占位 SHA 数量为 0。
- [x] 30 个 Case 的公开验证可从干净 clone 启动。
- [x] 本地隐藏测试与 Agent worktree 隔离。
- [x] 25 个实现类和 5 个决策类 Case 的双向 Grader 审计通过。
- [x] 两个 Private GitHub 仓库已创建、审核并上传。
- [x] 两个 Private 仓库均已 Archived，分支、标签、代码和权限只读；写入 dry-run 被 HTTP 403 拒绝。
- [x] Agent 使用的仓库级凭据对 Fixture 只读，且对 Grader 无访问权限。
- [x] 远端干净 clone 复验通过。
