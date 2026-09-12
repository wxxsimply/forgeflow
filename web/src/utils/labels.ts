// Translate presentation labels only; keep API values, IDs and evidence unchanged.
function lookup(labels: Record<string, string>, value?: string): string {
  return value ? labels[value] ?? value : '—';
}

export function roleLabel(value?: string): string {
  return lookup({ admin: '管理员', operator: '操作员', viewer: '只读用户' }, value);
}

export function riskLabel(value: string): string {
  return lookup({ low: '低风险', medium: '中风险', high: '高风险', critical: '严重风险' }, value);
}

export function approvalStatusLabel(value: string): string {
  return lookup({ pending: '待处理', approved: '已批准', rejected: '已拒绝' }, value);
}

export function actionLabel(value?: string): string {
  return lookup({ plan: '执行计划审批', plan_approval: '执行计划审批', apply_patch: '应用代码补丁',
    tool: '工具调用审批', tool_call: '工具调用审批', judge_security: '安全审查审批',
    pass: '通过', pass_with_approval: '审批后通过', repair: '修复', human_review: '人工复核', fail: '失败',
    allow: '允许', deny: '拒绝', require_approval: '需要审批', approval: '需要审批' }, value);
}

export function nodeLabel(value?: string): string {
  return lookup({ start: '开始', planner: '制定计划', 'validate-plan': '校验计划', 'plan-approval': '计划审批',
    'prepare-workspace': '准备工作区', implement: '实现任务', evaluate: '执行评估', judge: '综合判断',
    reviewer: '代码审查', security: '安全审查', repair: '修复问题', end: '结束' }, value);
}

export function eventLabel(value: string): string {
  return lookup({ run_created: '任务已创建', node_started: '节点开始执行', node_completed: '节点执行完成',
    node_interrupted: '节点执行中断', node_failed: '节点执行失败', approval_requested: '已请求审批',
    approval_resolved: '审批已处理', status_changed: '状态已更新', node_retrying: '节点正在重试',
    node_reused: '已复用节点结果', budget_exhausted: '预算已耗尽', cancellation_requested: '已请求取消',
    run_cancelled: '任务已取消', pause_requested: '已请求暂停', run_paused: '任务已暂停',
    run_resumed: '任务已恢复', parallel_completed: '并行执行完成', tool_call_started: '工具开始调用',
    tool_call_completed: '工具调用完成', tool_call_denied: '工具调用被拒绝', tool_call_failed: '工具调用失败' }, value);
}

export function agentLabel(value?: string): string {
  return lookup({ planner: '规划智能体', developer: '开发智能体', reviewer: '审查智能体',
    security: '安全智能体', judge: '判断智能体', agent: '智能体' }, value);
}

export function agentRoleLabel(value: string): string {
  return lookup({ 'bounded planning': '受约束的任务规划', 'approved implementation': '经审批的代码实现',
    'independent review': '独立代码审查', 'independent security review': '独立安全审查' }, value);
}

export function invocationStatusLabel(value?: string): string {
  return lookup({ pending: '等待中', started: '已开始', running: '执行中', completed: '已完成',
    succeeded: '成功', success: '成功', failed: '失败', denied: '已拒绝', allowed: '已允许',
    approved: '已批准', rejected: '已拒绝', cancelled: '已取消', error: '出错', timeout: '超时' }, value);
}

export function artifactKindLabel(value: string): string {
  return lookup({ plan: '执行计划', patch: '代码补丁', diff: '代码差异', report: '执行报告',
    test: '测试结果', test_output: '测试输出', review: '代码审查', security: '安全审查',
    log: '日志', trace: '调用记录', checkpoint: '检查点', evidence: '执行证据' }, value);
}

export function errorMessage(error: Error): string {
  return /[\u3400-\u9fff]/u.test(error.message) ? error.message : '操作失败，请检查网络后重试。';
}
