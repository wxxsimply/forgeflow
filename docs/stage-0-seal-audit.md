# ForgeFlow 阶段 0：本地封板审查记录

> 审查日期：2026-08-18  
> 对应路线图：`FORGEFLOW_POST_IMPLEMENTATION_ROADMAP.md` 阶段 0  
> 结论：已完成。技术检查、Apache-2.0 接入、所有者人工复核和第三方依赖条件确认均已通过。确认材料见 `docs/third-party-dependency-review.md`。

## 1. 审查范围

- 工作区：`D:\Code\forgeflow`
- Git 状态：仓库已初始化，但尚无首个 commit 和可解析的 `HEAD`。
- 候选提交文件：`git ls-files --others --exclude-standard` 返回 252 个文件。
- 本阶段未执行 `git add`、`git commit`、`git push` 或任何 GitHub 上传。

## 2. 忽略规则检查

以下路径已由 `.gitignore` 明确排除并通过 `git check-ignore -v` 验证：

- `.env`
- `.forgeflow/`
- `.cache/`
- `bin/`
- `web/node_modules/`
- `web/dist/`
- `deploy/staging/staging.env`
- `deploy/staging/secrets/*`

`deploy/staging/secrets/` 当前只有允许提交的 `README.md`，没有真实 Secret 文件。

候选文件名扫描只发现预期的示例/说明文件：

- `.env.example`
- `deploy/staging/secrets/README.md`

## 3. Secret 技术扫描

对全部候选提交文件进行了高置信度模式扫描，检查：

- OpenAI `sk-...` Key
- GitHub PAT/Token
- AWS Access Key
- PEM/OpenSSH Private Key
- JWT

结果：`HIGH_CONFIDENCE_SECRET_SCAN=PASS`，未发现匹配项。

这项扫描不能替代仓库所有者对最终暂存内容的人工检查。首次提交前仍必须运行：

```powershell
git add --all
git diff --cached --stat
git diff --cached --check
git status --short
```

随后由仓库所有者人工阅读暂存清单；发现异常时必须先取消暂存并修正 `.gitignore`。

## 4. 完整本地验证

执行：

```powershell
./scripts/verify.ps1
```

结果：通过。

验证覆盖：

- Go 格式检查。
- 全部 Go 包测试。
- `go vet`。
- Migration 配对测试。
- CLI、API、Worker 二进制构建。
- OpenAPI TypeScript 客户端重新生成。
- TypeScript 类型检查。
- 3 个前端测试文件、11 个测试用例。
- Web Production Build。

最终输出：`ForgeFlow verification passed.`

## 5. 第三方依赖许可证检查

### Go 依赖

对 `go list -m all` 返回的第三方模块检查模块缓存中的 `LICENSE`/`NOTICE` 文件：

| 许可证类别 | 模块数量 |
|---|---:|
| Apache-2.0 | 15 |
| BSD-style | 12 |
| MIT | 8 |
| ISC | 1 |

直接依赖的主要许可证：

- `github.com/jackc/pgx/v5`：MIT
- OpenTelemetry Go 模块：Apache-2.0
- `golang.org/x/crypto`：BSD-style

### Web 依赖

根据 `web/package-lock.json` 中已安装包的许可证字段，共检查 178 个 package entry：

| 许可证 | 数量 |
|---|---:|
| MIT | 140 |
| MPL-2.0 | 14 |
| Apache-2.0 | 9 |
| ISC | 5 |
| BSD-2-Clause | 2 |
| BSD-3-Clause | 2 |
| MIT-0 | 2 |
| CC0-1.0 | 1 |
| MIT OR CC0-1.0 | 1 |
| BlueOak-1.0.0 | 1 |
| Python-2.0 | 1 |

需要在发布第三方 Notices 时重点保留和复核的非 MIT/BSD 依赖包括 `@axe-core/playwright`、`axe-core`、`lightningcss`（MPL-2.0）、`@playwright/test`/TypeScript（Apache-2.0）、`lru-cache`（BlueOak-1.0.0）和 `argparse`（Python-2.0）。

本记录是工程清点，不构成法律意见。正式公开或商业发布前，应由仓库所有者或法务确认 Notice、源码提供和再分发义务。

## 6. 当前阻塞项

### 6.1 项目级许可证已解决

仓库所有者于 2026-08-18 选择 Apache License 2.0。已完成：

1. 根目录添加来自 Apache 官方文本的 `LICENSE`。
2. README 增加许可证说明。
3. `web/package.json` 和 `web/package-lock.json` 增加 `Apache-2.0` SPDX 标识。

项目许可证选择已解决。正式发布前仍需确认第三方 Notices 和再分发条件；本工程清点不构成法律意见。

### 6.2 首次提交和上传已完成

仓库所有者已于 2026-08-18 确认人工检查待提交文件，并已人工创建首个 commit、配置远端和上传到 GitHub。后续 Go 1.26.6 CI 修复仍需由所有者再次人工提交和上传。

## 7. 阶段 0 完成条件

只有以下事项由仓库所有者完成后，才能把阶段 0 从“阻塞”改为“已完成”：

- [x] 已选择并添加 Apache License 2.0。
- [x] 已阅读 `docs/third-party-dependency-review.md` 并确认第三方依赖使用条件（2026-08-19）。
- [x] 已人工阅读最终待提交清单，确认可以提交（2026-08-18）。

阶段 0 已于 2026-08-19 完成，现进入阶段 1。阶段 1 的后续 commit、GitHub 上传和仓库设置仍必须由仓库所有者手动执行。
