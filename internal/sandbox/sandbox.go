package sandbox

import (
	"context"
	"sync"
	"time"
)

type Request struct {
	RunID         string
	Image         string
	WorkspacePath string
	WorkingDir    string
	Program       string
	Args          []string
	Environment   map[string]string
	Timeout       time.Duration
}

type Result struct {
	ExitCode    int           `json:"exitCode"`
	Stdout      string        `json:"stdout"`
	Stderr      string        `json:"stderr"`
	Duration    time.Duration `json:"duration"`
	Truncated   bool          `json:"truncated"`
	ContainerID string        `json:"containerId,omitempty"`
}

type Runner interface {
	Run(context.Context, Request) (Result, error)
}

type FakeRunner struct {
	mu       sync.Mutex
	Results  []Result
	Errors   []error
	Requests []Request
	calls    int
}

func (f *FakeRunner) Run(_ context.Context, request Request) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Requests = append(f.Requests, request)
	index := f.calls
	f.calls++
	if index < len(f.Errors) && f.Errors[index] != nil {
		return Result{}, f.Errors[index]
	}
	if index >= len(f.Results) {
		return Result{}, nil
	}
	return f.Results[index], nil
}

func (f *FakeRunner) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

var _ Runner = (*FakeRunner)(nil)
