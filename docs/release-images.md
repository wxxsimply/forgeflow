# ForgeFlow 不可变镜像发布手册

本手册只用于阶段 9 的人工发布窗口。阶段 5 只验证构建定义和清单契约，不构建、不登录 Registry、不上传镜像。发布负责人必须从同一个已批准且工作区干净的 Git commit 构建全部五类镜像。

## 1. 固定发布输入

五类镜像固定为 `forgeflow-api`、`forgeflow-worker`、`forgeflow-web`、`forgeflow-caddy` 和 `forgeflow-sandbox`，目标平台固定为 `linux/amd64`。数据库 Migration 复用 API 镜像内的 ForgeFlow CLI，不创建第六个发布镜像。

在阶段 9 人工确认以下变量：

```powershell
$env:REGISTRY = "ghcr.io/wxxsimply"
$env:RELEASE = "1.0.0"
$env:GIT_COMMIT = git rev-parse HEAD
git status --short
docker buildx bake release --print
docker buildx bake release --call=check
./scripts/validate-release-assets.ps1
```

`GIT_COMMIT` 必须是批准的 40 位 SHA，工作区必须干净。不得使用 `latest`，不得在验收过程中更换代码、Prompt、Policy、Tool、Fixture、Grader 或 Migration。

## 2. 人工登录、构建和上传

Registry 登录必须由发布负责人手动完成，凭据只在安全提示中输入，不得写入脚本、环境模板、日志或 Release manifest。确认登录后人工执行：

```powershell
docker buildx bake release --push
```

Bake 计划会为每个镜像生成版本标签和 `sha-<40位提交>` 标签，并附加 BuildKit SBOM 与 max-mode provenance。上传失败时停止，不得只发布成功的部分。

## 3. 记录 digest 和供应链证据

逐个读取 Registry 返回的真实 digest，并只使用 `image@sha256:<64位十六进制>` 作为部署引用。为每个 digest 生成和验证证据：

```powershell
$evidenceDir = ".forgeflow/release/$env:RELEASE"
New-Item -ItemType Directory -Force $evidenceDir

# 对五个真实 digest 分别执行；REFERENCE 必须替换为实际 image@sha256 值。
syft REFERENCE -o cyclonedx-json="$evidenceDir/IMAGE.sbom.cdx.json"
trivy image --format json --output "$evidenceDir/IMAGE.trivy.json" REFERENCE
trivy image --severity HIGH,CRITICAL --exit-code 1 REFERENCE
cosign sign --yes REFERENCE
cosign verify --certificate-identity CREATOR_IDENTITY --certificate-oidc-issuer OIDC_ISSUER REFERENCE
cosign verify-attestation --type slsaprovenance --certificate-identity CREATOR_IDENTITY --certificate-oidc-issuer OIDC_ISSUER REFERENCE
cosign verify-attestation --type spdxjson --certificate-identity CREATOR_IDENTITY --certificate-oidc-issuer OIDC_ISSUER REFERENCE
```

`CREATOR_IDENTITY` 和 `OIDC_ISSUER` 必须替换为发布负责人实际采用且经组织批准的签名身份约束。把命令输出保存到对应的 provenance 和 signature 证据文件。默认门禁为 Critical=0 且 High=0；任何例外必须先完成风险接受记录，且不能以备注代替审批。

## 4. Release manifest

复制 `deploy/release/release-manifest.template.json` 到被 `.gitignore` 排除的 `.forgeflow/release/<版本>/release-manifest.json`，替换全部占位符：

- `release`、`gitCommit` 和 UTC `createdAt`。
- 五个版本标签、Git SHA 标签、digest 和完整 digest reference。
- SBOM、provenance、签名和 Trivy 报告状态及路径。
- Critical/High 数量；存在例外时加入风险接受记录路径。

验证最终清单：

```powershell
./scripts/validate-release-assets.ps1 -Manifest .forgeflow/release/1.0.0/release-manifest.json
```

未通过验证的清单不能进入 Staging。

## 5. 干净主机拉取

在不复用构建缓存的干净 Linux 主机上，按清单中的五个 digest reference 分别执行 `docker pull` 和 `docker image inspect`。确认 revision/version 标签、目标架构和 digest 与清单一致。随后再把这些引用写入 Staging 环境配置。

## 6. 人工边界

- Registry 登录、`--push`、签名身份确认和 digest 复核必须由发布负责人手动执行。
- Agent 不执行 Registry 上传，也不保存 Token。
- 原始 Eval Evidence、Private Grader、隐藏测试、数据库 dump 和凭据不得进入镜像或发布证据。
- 完整构建、扫描、签名、上传和干净主机拉取统一留到阶段 9。
