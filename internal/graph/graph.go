package graph

import (
	"context"
	"time"

	"forgeflow/internal/domain"
)

type ResultType string

const (
	ResultCompleted      ResultType = "completed"
	ResultInterrupted    ResultType = "interrupted"
	ResultRetryableError ResultType = "retryable_error"
	ResultFatalError     ResultType = "fatal_error"
)

type Result struct {
	Type     ResultType
	State    *domain.RunState
	Approval *domain.ApprovalRequest
	Err      error
}

type NodePolicy struct {
	Timeout     time.Duration
	MaxAttempts int
	Backoff     time.Duration
	Retryable   func(error) bool
	ReplaySafe  bool
}

func (p NodePolicy) normalized() NodePolicy {
	if p.Timeout <= 0 {
		p.Timeout = 30 * time.Second
	}
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 1
	}
	if p.Backoff < 0 {
		p.Backoff = 0
	}
	return p
}

type Node interface {
	ID() string
	Policy() NodePolicy
	IdempotencyKey(*domain.RunState) string
	Execute(context.Context, *domain.RunState) Result
}

type NodeFunc struct {
	NodeID          string
	ExecutionPolicy NodePolicy
	Key             func(*domain.RunState) string
	Run             func(context.Context, *domain.RunState) Result
}

func (n NodeFunc) ID() string { return n.NodeID }

func (n NodeFunc) Policy() NodePolicy { return n.ExecutionPolicy.normalized() }

func (n NodeFunc) IdempotencyKey(state *domain.RunState) string {
	if n.Key != nil {
		return n.Key(state)
	}
	return n.NodeID
}

func (n NodeFunc) Execute(ctx context.Context, state *domain.RunState) Result {
	return n.Run(ctx, state)
}

type Edge struct {
	From string
	To   string
	When func(*domain.RunState) bool
}

type Definition struct {
	EntryNodeID string
	Nodes       []Node
	Edges       []Edge
}
