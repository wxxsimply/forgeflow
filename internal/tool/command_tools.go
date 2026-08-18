package tool

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
	"forgeflow/internal/policy"
	"forgeflow/internal/sandbox"
)

type CommandInput struct {
	Program             string   `json:"program"`
	Args                []string `json:"args"`
	WorkingDir          string   `json:"workingDir"`
	EnvAllow            []string `json:"envAllow"`
	TimeoutMilliseconds int64    `json:"timeoutMilliseconds"`
}

const commandInputSchema = `{"type":"object","properties":{"program":{"type":"string"},"args":{"type":"array","items":{"type":"string"}},"workingDir":{"type":"string"},"envAllow":{"type":"array","items":{"type":"string"}},"timeoutMilliseconds":{"type":"integer","minimum":1,"maximum":600000}},"required":["program"],"additionalProperties":false}`
const commandOutputSchema = `{"type":"object","properties":{"exitCode":{"type":"integer"},"stdout":{"type":"string"},"stderr":{"type":"string"},"duration":{"type":"integer"},"truncated":{"type":"boolean"},"containerId":{"type":"string"}},"required":["exitCode","stdout","stderr","duration","truncated"]}`

type commandTool struct {
	spec             Spec
	runner           sandbox.Runner
	image            string
	fixedEnvironment map[string]string
}

func RegisterCommandTools(registry *Registry, runner sandbox.Runner, image string) error {
	if registry == nil || runner == nil || strings.TrimSpace(image) == "" {
		return apperror.New(apperror.CodeValidation, "command tool registry, sandbox runner, and image are required")
	}
	for _, name := range []string{"run_test", "run_static_check"} {
		candidate := &commandTool{
			spec: Spec{
				Name: name, Version: "v1", Description: "Run an allowlisted command in an isolated, network-disabled sandbox.",
				InputSchema: json.RawMessage(commandInputSchema), OutputSchema: json.RawMessage(commandOutputSchema),
				Risk: domain.RiskMedium, Timeout: 11 * time.Minute, MaxOutputBytes: 4 * 1024 * 1024,
			},
			runner: runner, image: image, fixedEnvironment: map[string]string{"CI": "true"},
		}
		if err := registry.Register(candidate); err != nil {
			return err
		}
	}
	return nil
}

func (t *commandTool) Spec() Spec { return t.spec }

func (t *commandTool) Analyze(input json.RawMessage) (policy.Metadata, error) {
	request, err := decodeCommandInput(input)
	if err != nil {
		return policy.Metadata{}, err
	}
	return policy.Metadata{
		Paths: []string{request.WorkingDir},
		Command: &policy.Command{
			Program: request.Program, Args: request.Args, WorkingDir: request.WorkingDir, EnvAllow: request.EnvAllow,
		},
	}, nil
}

func (t *commandTool) Execute(ctx context.Context, call CallContext, input json.RawMessage) (json.RawMessage, error) {
	request, err := decodeCommandInput(input)
	if err != nil {
		return nil, err
	}
	environment := make(map[string]string, len(request.EnvAllow))
	for _, name := range request.EnvAllow {
		value, exists := t.fixedEnvironment[name]
		if !exists {
			return nil, apperror.New(apperror.CodePolicyDenied, "command requested an environment variable that is not available")
		}
		environment[name] = value
	}
	result, err := t.runner.Run(ctx, sandbox.Request{
		RunID: call.RunID, Image: t.image, WorkspacePath: call.Workspace.Path,
		WorkingDir: request.WorkingDir, Program: request.Program, Args: append([]string(nil), request.Args...),
		Environment: environment, Timeout: time.Duration(request.TimeoutMilliseconds) * time.Millisecond,
	})
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, apperror.Wrap(err, apperror.CodeInternal, "tool.command.encode", "sandbox result could not be encoded")
	}
	return encoded, nil
}

func (t *commandTool) ValidateOutput(output json.RawMessage) error {
	var result sandbox.Result
	return json.Unmarshal(output, &result)
}

func decodeCommandInput(input json.RawMessage) (CommandInput, error) {
	var request CommandInput
	if err := decodeStrict(input, &request); err != nil {
		return request, err
	}
	request.Program = strings.TrimSpace(request.Program)
	if request.Program == "" {
		return request, apperror.New(apperror.CodeValidation, "command program is required")
	}
	if request.WorkingDir == "" {
		request.WorkingDir = "."
	}
	if len(request.Args) > 128 || len(request.EnvAllow) > 16 {
		return request, apperror.New(apperror.CodeValidation, "command contains too many arguments or environment names")
	}
	if request.TimeoutMilliseconds <= 0 {
		request.TimeoutMilliseconds = int64((10 * time.Minute) / time.Millisecond)
	}
	if request.TimeoutMilliseconds > int64((10*time.Minute)/time.Millisecond) {
		return request, apperror.New(apperror.CodeValidation, "command timeout exceeds ten minutes")
	}
	seenEnvironment := make([]string, 0, len(request.EnvAllow))
	for _, name := range request.EnvAllow {
		if strings.TrimSpace(name) != name || name == "" || slices.Contains(seenEnvironment, name) {
			return request, apperror.New(apperror.CodeValidation, "command environment allowlist is invalid")
		}
		seenEnvironment = append(seenEnvironment, name)
	}
	return request, nil
}
