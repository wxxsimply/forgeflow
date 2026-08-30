# ForgeFlow 阶段 2：真实 Eval Fixture 审计

> 审计日期：2026-08-30  
> 当前状态：进行中（本地技术工作完成，等待人工 GitHub 操作）  
> 数据集：`software/v1`，30 Cases

## 1. 本地交付物

- 公开 Fixture 模板：`evals/fixtures/public/`。
- 可复现生成器：`cmd/forgeflow-fixture-builder`。
- Fixture 锁定清单：`evals/software-v1-fixtures.lock.json`。
- 本地 Fixture Git 仓库：`D:\fixtures\forgeflow-eval-fixtures`。
- 本地私有 Grader Git 仓库：`D:\fixtures\forgeflow-eval-grader`。
- 数据集中的 30 个 `fixtureCommit` 已回填为真实且唯一的 40 位 SHA。

公开 Fixture 仓库当前基线提交为 `6ebdc5d14c69d7867b569cf0e19d34c7b60f3a4f`，包含 30 个 `fixture/software-v1/<case-id>` 分支和 30 个 `software-v1/<case-id>` 标签。私有 Grader 当前提交为 `5942ec84d403e37385203b4c7851d1b92573548a`。

## 2. 隔离设计

- Fixture commit 只包含公开任务起点、公共测试和 `CASE.json`，不包含隐藏测试、Golden 实现、凭据或真实用户数据。
- 隐藏测试和 Golden 覆盖层只存在于独立 Grader 仓库。
- Grader 只在 Agent 执行结束后临时把单项隐藏测试写入完成的 worktree，测试结束立即移除。
- 实现类 Case 不向 Agent 暴露隐藏测试源码；决策类 Case 只读取终态 Observation。
- 真实部署时，Agent Token 必须无权读取 Grader 仓库，也不得写入 Fixture 仓库。

## 3. 验证证据

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

## 4. 复现命令

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

## 5. 必须人工完成的 GitHub 操作

以下操作不得由 Agent 代替：

1. 人工创建 Private 仓库 `forgeflow-eval-fixtures`，不要自动生成 README、License 或 `.gitignore`。
2. 人工审核 `D:\fixtures\forgeflow-eval-fixtures` 后上传 `main`、全部 30 个 Fixture 分支和全部标签。
3. 人工创建另一个 Private 仓库 `forgeflow-eval-grader`，审核后只上传 Grader 仓库；绝不能把它上传到公开 Fixture 仓库。
4. 为 Fixture 仓库配置 Branch/Tag Ruleset：禁止删除和强制更新，变更要求 Pull Request，保护 `fixture/software-v1/*` 和 `software-v1/*`。
5. Agent 凭据只能只读访问 Fixture 仓库，并且不能访问 Grader 仓库；Eval 管理员和隔离 Grader 才能读取 Grader。
6. 对远端做一次干净 clone，重新运行 30 SHA 验证和双向审计，并把远端仓库地址、Ruleset 截图和验证时间补充到本文件。

主 ForgeFlow 仓库中的本阶段改动也必须由仓库所有者人工审核、提交、推送分支并通过 Pull Request 合并。

## 6. 阶段退出状态

- [x] 30 个真实唯一 commit 已生成并回填。
- [x] 占位 SHA 数量为 0。
- [x] 30 个 Case 的公开验证可从干净 clone 启动。
- [x] 本地隐藏测试与 Agent worktree 隔离。
- [x] 25 个实现类和 5 个决策类 Case 的双向 Grader 审计通过。
- [ ] 两个 Private GitHub 仓库已人工创建、审核并上传。
- [ ] Fixture Branch/Tag Ruleset 已启用。
- [ ] Agent 对 Fixture 只读且对 Grader 无访问权限。
- [ ] 远端干净 clone 复验通过。
