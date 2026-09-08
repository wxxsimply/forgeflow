# 阶段 6 Staging 部署准备审计

> 审计日期：2026-09-08  
> 基线 commit：`f708c2020821962d0724034d7a9e8c12d2d4b200`  
> 结论：工程准备门槛通过，状态为 `待集中验收`；公网部署仍未执行

## 1. 本阶段完成内容

- Staging Compose 改为只接受不可变镜像输入，服务器不再包含任何 `build` 配置。
- API、Worker、Web、Caddy、Sandbox 以及 PostgreSQL、OTel、Prometheus、Alertmanager、DIND 均由环境模板显式提供 digest 引用。
- Migration 复用 API 镜像内的 CLI，Preflight 强制 Migration/API 镜像一致。
- Preflight 校验源码 SHA、仓库父目录挂载、Secret 文件、Release manifest、Compose 网络/端口、只读根文件系统、镜像 digest 和凭据隔离；Acceptance 针对实际 Fixture 仓库校验 Git HEAD 与执行前后状态。
- Release 脚本先校验再拉取，使用 `--no-build` 部署；OpenAI 模式把已审核 Sandbox 镜像导入隔离 DIND，不暴露宿主 Docker Socket。
- API、Worker 和 Web 的 Release/Git SHA 元数据纳入发布门禁；Worker `/readyz` 同时承担 Prompt/model Active Release 门禁。
- 新增公网 HTTPS/browser E2E 验收脚本，记录脱敏证据并比较 Fixture 仓库执行前后的 commit/状态。
- 新增 Bootstrap 清理脚本，验证登录、删除一次性 Secret、移除 overlay、重建 API 并检查健康元数据。
- 基础设施、DNS、端口、存储、Secret、Registry、备份和访问控制要求记录在 `docs/stage-6-staging-infrastructure.md`。

## 2. 快速验证记录

| 检查 | 结果 | 说明 |
|---|---|---|
| `scripts/validate-staging-assets.ps1` | 通过 | PowerShell 解析、Compose 无服务端构建、digest 模板、Release/E2E/Bootstrap/Web 元数据契约 |
| `scripts/staging-preflight.ps1 -RequireDigests -AllowPlaceholderDigests` | 通过 | 使用临时非敏感 CI 输入渲染基础、OpenAI 和 Bootstrap 三种 Compose；测试文件随后删除 |
| 非 CI 占位 digest 负向检查 | 按预期拒绝 | `AllowPlaceholderDigests` 仅允许显式 CI Release，且不能与正式 manifest 同用 |
| PowerShell Parser | 通过 | Preflight、Release、Acceptance、Bootstrap cleanup 均无语法错误 |
| `npm run check` | 通过 | TypeScript、11 个 Vitest 测试和生产构建；Staging Playwright 配置可被类型检查 |
| `go test -timeout 300s ./...`、`go vet ./...` | 通过 | Go 工程基线未回归 |
| Release contract、Bake print、Buildx check | 通过 | 五类 Dockerfile 静态检查无警告，未构建完整镜像 |

上述检查均为 10 分钟以内的工程快速检查，没有拉取完整发布镜像、调用付费模型或连接公网 Staging。

## 3. 阶段 9 后置项

- 创建真实主机、DNS、TLS、存储、Registry 只读凭据和 Secret。
- 使用已签名/审核的真实五镜像 Release manifest 按 digest 部署。
- 执行真实 Sandbox Run、HTTPS 浏览器全链路、RBAC、端口和日志检查。
- 保存部署时间、镜像 digest、健康数据、Fixture 不变性及 Bootstrap 删除 Evidence。
- 阶段 7 的告警、恢复、安全、回滚、负载和 Demo 演练完成前，不得声明 Production Ready。

## 4. 审计结论

阶段 6 的代码、配置、脚本、清单和快速校验均已就绪，可以进入阶段 7 工程准备。真实公网 Staging 与完整镜像证据被明确保留到阶段 9；在这些后置项完成前，阶段 6 只能保持 `待集中验收`。
