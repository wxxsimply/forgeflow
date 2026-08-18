package repository

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"forgeflow/internal/apperror"
)

const defaultGitOutputLimit = 8 * 1024 * 1024

type gitRunner struct {
	maxOutput int
}

func newGitRunner(maxOutput int64) gitRunner {
	if maxOutput <= 0 || maxOutput > defaultGitOutputLimit {
		maxOutput = defaultGitOutputLimit
	}
	return gitRunner{maxOutput: int(maxOutput)}
}

func (r gitRunner) run(ctx context.Context, directory string, args ...string) ([]byte, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	commandArgs := append([]string{"--no-optional-locks", "-C", directory}, args...)
	command := exec.CommandContext(ctx, "git", commandArgs...)
	command.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"LC_ALL=C",
	)
	stdout := newCappedBuffer(r.maxOutput)
	stderr := newCappedBuffer(64 * 1024)
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, apperror.Wrap(ctx.Err(), apperror.CodeTimeout, "repository.git", "git command timed out or was cancelled")
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", safeGitOperation(args), message)
	}
	if stdout.exceeded {
		return nil, apperror.New(apperror.CodePolicyDenied, "git output exceeds the configured limit")
	}
	return stdout.Bytes(), nil
}

type cappedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{limit: limit}
}

func (b *cappedBuffer) Write(data []byte) (int, error) {
	originalLength := len(data)
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.exceeded = true
		return originalLength, nil
	}
	if len(data) > remaining {
		b.exceeded = true
		data = data[:remaining]
	}
	_, _ = b.buffer.Write(data)
	return originalLength, nil
}

func (b *cappedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *cappedBuffer) String() string { return b.buffer.String() }

func safeGitOperation(args []string) string {
	if len(args) == 0 {
		return "command"
	}
	if len(args) > 2 {
		args = args[:2]
	}
	return strings.Join(args, " ")
}
