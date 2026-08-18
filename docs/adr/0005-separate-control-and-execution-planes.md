# ADR 0005：控制面与执行面分离

状态：Accepted  
日期：2026-08-11

## 决策

公网入口、Web、API、Session 与 PostgreSQL 属于控制面；模型 Key、仓库 worktree、Tool 和 Sandbox 属于执行面。API 不持有模型 Key、Docker Socket 或通用执行能力。Staging 可在单机 Compose 中使用独立网络和服务模拟边界；Production Worker 必须迁移到专用主机/节点池。

## 原因

Agent 处理不可信仓库和模型输出，其失陷概率与影响均高于普通控制面。拆分后，Worker 或 Sandbox 被攻陷时不能直接读取 Web Session Secret、公开数据库端口或修改反向代理。

## 后果

需要额外的 Queue、Artifact 传输、Secret 分发、Worker drain 和跨平面监控；部署复杂度增加。作为交换，公网 API 不再拥有代码执行控制权，故障和入侵半径更小。
