# 阶段 5：不可变发布镜像资产准备审计

> 审计日期：2026-09-08  
> 状态：待集中验收  
> 范围：工程准备和快速静态校验；不包含镜像构建、上传、扫描结果或 Staging 部署。

## 已固定的发布契约

- 发布镜像固定为 API、Worker、Web、Caddy 和 Sandbox 五类，目标平台为 `linux/amd64`。
- 数据库 Migration 复用 API 镜像中的 CLI，不再生成独立发布镜像。
- `docker-bake.hcl` 统一注入版本号和 40 位 Git SHA，生成版本及 SHA 双标签，并声明 SBOM 与 max-mode provenance。
- 五类 Dockerfile 均写入 OCI source、version、revision 和 Apache-2.0 标签。
- Release manifest 模板要求记录五个 digest reference、SBOM、provenance、签名、漏洞扫描与风险接受信息。

## 快速门禁

- `scripts/validate-release-assets.ps1` 离线验证五镜像、Dockerfile、Bake 和 manifest 模板之间的一致性。
- Pull Request 的 `deployment-assets/validate` 使用 `docker buildx bake --call=check` 校验 Dockerfile，不再在工程准备遍完整构建五个镜像。
- 最终 manifest 校验要求 digest 格式正确、引用按 digest 固定、证据文件存在、签名/provenance 已验证，并阻断未接受的 High/Critical 结果。

2026-09-08 本地快速验证结果：

- `scripts/validate-release-assets.ps1`：通过，确认五镜像契约。
- `docker buildx bake release --print`：通过，展开结果为五个 `linux/amd64` 目标、版本/SHA 双标签、SBOM 和 max provenance。
- Docker Engine 28.4.0、Buildx 0.28.0 的 `docker buildx bake release --call=check`：五个目标全部通过且无警告，未执行镜像构建。
- `docker compose config --quiet`：通过，Migration 复用 API 镜像的 CLI。
- `go test ./...`、Gofmt、Vet、Staticcheck、Migration 合约和三个 Go 二进制构建：通过。
- `npm run check`：类型检查、11 个 Vitest 测试和生产构建通过。
- `git diff --check` 与本阶段文件 Secret 模式扫描：通过。

## 凭据边界

- Registry Token 没有默认值，也不写入仓库、脚本或 manifest。
- Registry 登录、镜像上传、签名身份确认和 digest 复核由发布负责人在阶段 9 手动执行。
- 发布证据输出到被忽略的 `.forgeflow/release/<版本>`，不得包含 Secret、原始 Eval Evidence、Private Grader、隐藏测试或数据库 dump。

## 后置到阶段 9 的事项

- 从批准 commit 完整构建五个镜像。
- 生成并验证 SBOM、provenance 和签名。
- 运行 Trivy，处理或正式接受阻断发现。
- 手动上传 Registry，记录 digest，并在干净主机按 digest 拉取。
- 生成最终 Release manifest 并作为 Staging 输入。

## 结论

阶段 5 的工程准备门槛已具备可重复的代码、清单、验证脚本和人工 Runbook。真实镜像尚未构建和上传，因此本阶段状态为“待集中验收”，不能标记为“已完成”。
