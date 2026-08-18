package planner

import (
	"context"

	"forgeflow/internal/domain"
)

type Mock struct{}

func (Mock) CreatePlan(_ context.Context, input Input) (Result, error) {
	return Result{Plan: domain.ExecutionPlan{
		Summary: "为任务制定受控实施计划：" + input.Task,
		Assumptions: []string{
			"当前阶段只生成计划，不修改目标仓库",
			"实际文件范围需要在仓库检查节点中确认",
		},
		FilesLikelyAffected: []string{},
		Steps: []domain.PlanStep{
			{
				ID: "inspect", Description: "检查仓库结构、项目约束和现有测试",
				AcceptanceCriteria: []string{"已确认技术栈", "已识别禁止修改区域"}, DependsOn: []string{},
			},
			{
				ID: "implement", Description: "在隔离工作区内实现最小范围变更",
				AcceptanceCriteria: []string{"变更满足任务要求", "未修改治理配置"}, DependsOn: []string{"inspect"},
			},
			{
				ID: "verify", Description: "运行白名单测试并独立审查 Diff",
				AcceptanceCriteria: []string{"测试命令通过", "安全与审查门禁通过"}, DependsOn: []string{"implement"},
			},
		},
		Risks: []domain.PlanRisk{
			{Level: domain.RiskMedium, Description: "尚未读取目标仓库，文件范围和测试命令仍需确认"},
		},
		TestStrategy: []string{"运行项目已配置的测试命令", "为变更补充针对性回归测试"},
	}}, nil
}
