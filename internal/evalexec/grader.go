package evalexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	fulleval "forgeflow/internal/eval"
)

type graderResult struct {
	CaseID            string          `json:"caseId"`
	Passed            bool            `json:"passed"`
	HiddenTestResults map[string]bool `json:"hiddenTestResults"`
	Failure           string          `json:"failure"`
}

func (c *core) grade(ctx context.Context, evalCase fulleval.Case, workspace string, observation *fulleval.Observation) {
	observationPath, err := c.writeGraderObservation(*observation)
	if err != nil {
		graderFailure(observation, err, workspace, c.options)
		return
	}
	defer os.Remove(observationPath)

	graderContext, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	command := exec.CommandContext(graderContext, "go", "run", "./cmd/grader",
		"--case", evalCase.ID,
		"--workspace", workspace,
		"--observation", observationPath,
		"--manifest", filepath.Join(c.options.GraderRepository, "manifest", "software_v1.json"),
		"--assets", c.options.GraderRepository,
	)
	command.Dir = c.options.GraderRepository
	command.Env = safeEnvironment()
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if graderContext.Err() != nil {
			err = graderContext.Err()
		}
		graderFailure(observation, fmt.Errorf("hidden grader failed: %w: %s", err, truncate(stderr.String(), 1000)), workspace, c.options)
		return
	}
	var result graderResult
	decoder := json.NewDecoder(&stdout)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		graderFailure(observation, fmt.Errorf("decode hidden grader result: %w", err), workspace, c.options)
		return
	}
	if result.CaseID != evalCase.ID {
		graderFailure(observation, fmt.Errorf("hidden grader returned mismatched case id"), workspace, c.options)
		return
	}
	for _, name := range evalCase.HiddenTests {
		observation.HiddenTestResults[name] = result.HiddenTestResults[name]
	}
	if evalCase.ExpectedDecision == fulleval.DecisionImplement {
		observation.Regression = !observation.ExplicitTestsPassed
		for _, name := range evalCase.HiddenTests {
			if !observation.HiddenTestResults[name] {
				observation.Regression = true
			}
		}
	}
	if result.Failure != "" && observation.FailureCode == "" {
		observation.FailureCode = "hidden_grader_failed"
		observation.FailureMessage = redact(result.Failure, workspace, c.options)
	}
}

func (c *core) writeGraderObservation(observation fulleval.Observation) (string, error) {
	directory := filepath.Join(c.options.WorkspaceRoot, ".grader-observations")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(directory, "observation-*.json")
	if err != nil {
		return "", err
	}
	path := file.Name()
	defer func() {
		_ = file.Close()
	}()
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(map[string]any{
		"decision":     observation.Decision,
		"changedFiles": observation.ChangedFiles,
	}); err != nil {
		os.Remove(path)
		return "", err
	}
	if err := file.Sync(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func graderFailure(observation *fulleval.Observation, err error, workspace string, options Options) {
	observation.Outcome = "failed"
	observation.FailureCode = "grader_failed"
	observation.FailureMessage = redact(err.Error(), workspace, options)
	if errorsIsDeadline(err) {
		observation.Outcome = "timed_out"
	}
}

func errorsIsDeadline(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err)
}
