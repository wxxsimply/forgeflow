# 本地 DeepSeek 真实开发（单机密钥）

这是**只在克隆者自己的 Windows 电脑上运行**的受控开发流程，不是公网发布，也不是“每位网站用户各自填写密钥”。当前线上个人预览继续保持 `planning/mock`。本地 API、Worker、Web 只监听 `127.0.0.1`；PostgreSQL 只映射到 `127.0.0.1:55432`。Worker 使用本机 Docker Desktop 运行无网络的命令沙箱，因此不要把本地端口转发到公网或让陌生用户登录。

## 准备

需要 PowerShell 7、Go（见 `go.mod`）、Node.js/npm 和处于 Linux 容器模式且显示 Engine running 的 Docker Desktop。使用**仅供本机、余额不超过人民币 5 元、关闭自动充值**的 DeepSeek API 密钥；密钥属于你的 DeepSeek 账户，调用会产生费用。`/user/balance` 预检和应用估算不能保证精确的单次或每日账单上限。模型标识和价格可能变化，真正付费前复核[官方模型及价格](https://api-docs.deepseek.com/quick_start/pricing/)。

在一个干净的 Git 克隆中运行：

```powershell
git clone https://github.com/wxxsimply/forgeflow.git
cd forgeflow
pwsh -NoProfile -File ./scripts/local-deepseek.ps1 -Action init
notepad .forgeflow\local\secrets\deepseek_api_key
```

把你的密钥作为**文件中唯一一行**保存，不要添加引号。此文件位于被 Git 忽略的 `.forgeflow/local/secrets/`，只由本地 Worker 读取；API、Web、仓库源码、Git 提交和聊天里都不应出现真实密钥。初始化脚本还生成本地 PostgreSQL 与管理员密码，不会覆盖已有文件。

## 启动与使用

```powershell
pwsh -NoProfile -File ./scripts/local-deepseek.ps1 -Action start
```

首次启动会构建本地命令沙箱与 Go 程序、安装 Web 依赖、启动私有 PostgreSQL、迁移数据库和创建本地管理员。打开 <http://127.0.0.1:5173>，使用 `admin@forgeflow.local` 登录；密码从 `.forgeflow/local/secrets/bootstrap_admin_password` 在你自己的电脑上读取。本机模式不开放自助注册。登记仓库时，直接复制启动输出中的 `Allowed repository path` 绝对路径；该仓库必须保持干净，再创建任务并按页面要求批准计划、审查补丁。**提交任务时就可能产生规划调用费用**；仓库源码和任务片段会发送给 DeepSeek。修改仅进入受管 Git worktree，仍需你审查最终产物。

启动脚本固定 `deepseek-flash`、单轮最多 4 次模型调用、零自动重试，并在每次付费调用前检查该密钥所属账户的余额。余额查询失败、不是仅人民币余额、没有余额或余额超过 5 元时会拒绝调用。**不要用高余额主账户密钥测试。** 本机完整流程仍可能因模型输出、网络、依赖或测试失败而不能完成任务；本项目不承诺任意需求都可自动交付。

```powershell
pwsh -NoProfile -File ./scripts/local-deepseek.ps1 -Action status
pwsh -NoProfile -File ./scripts/local-deepseek.ps1 -Action stop
```

`stop` 会停止本地 API、Worker、Web 和 PostgreSQL 容器，但保留数据库卷、密钥文件与日志。排错日志位于 `.forgeflow/local/logs/`，不要公开上传原始日志。若 Docker Desktop 未启动、选定仓库不干净、密钥文件为空或占用端口，脚本会停止而不启用付费 Worker。

本脚本默认只允许本地管理员操作当前克隆；可通过 `-RepositoryPath <干净的 Git 仓库绝对路径>` 改为另一个**你信任的**本地仓库。它不是面向任意注册用户的多租户执行环境，也不应复制到现有小内存云服务器上线。
