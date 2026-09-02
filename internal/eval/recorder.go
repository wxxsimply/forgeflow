package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// BaselineExecutor owns all mode-specific execution. A returned error is
// reserved for infrastructure failures that happened before a billable model
// call. Terminal case failures must be represented by Observation.
type BaselineExecutor interface {
	Execute(context.Context, Case, Configuration) (Observation, error)
}

type EvidenceRecorder interface {
	Append(context.Context, Evidence) error
}

type FileRecorder struct {
	Path string
}

func (r FileRecorder) Load() (EvidenceFile, error) {
	data, err := os.ReadFile(r.Path)
	if errors.Is(err, os.ErrNotExist) {
		return EvidenceFile{Runs: []Evidence{}}, nil
	}
	if err != nil {
		return EvidenceFile{}, fmt.Errorf("read eval evidence: %w", err)
	}
	return DecodeEvidence(data)
}

func (r FileRecorder) Append(ctx context.Context, evidence Evidence) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := r.Load()
	if err != nil {
		return err
	}
	replaced := false
	for index := range file.Runs {
		if file.Runs[index].Configuration.Mode == evidence.Configuration.Mode {
			file.Runs[index] = evidence
			replaced = true
			break
		}
	}
	if !replaced {
		file.Runs = append(file.Runs, evidence)
	}
	slices.SortFunc(file.Runs, func(a, b Evidence) int {
		return strings.Compare(string(a.Configuration.Mode), string(b.Configuration.Mode))
	})
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode eval evidence: %w", err)
	}
	return atomicWrite(r.Path, append(data, '\n'))
}

type ResumableOptions struct {
	Dataset        Dataset
	Configurations []Configuration
	Executor       BaselineExecutor
	Recorder       FileRecorder
	Now            func() time.Time
	MaxNewCases    int
	OnCompleted    func(Mode, string, int, int)
}

func RunResumable(ctx context.Context, options ResumableOptions) (EvidenceFile, error) {
	if err := ValidateDataset(options.Dataset); err != nil {
		return EvidenceFile{}, err
	}
	if options.Executor == nil || strings.TrimSpace(options.Recorder.Path) == "" {
		return EvidenceFile{}, fmt.Errorf("eval executor and evidence output path are required")
	}
	if len(options.Configurations) == 0 {
		return EvidenceFile{}, fmt.Errorf("at least one eval mode is required")
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	file, err := options.Recorder.Load()
	if err != nil {
		return EvidenceFile{}, err
	}
	newCases := 0
	for _, configuration := range options.Configurations {
		evidence, err := resumeEvidence(file, options.Dataset, configuration, now())
		if err != nil {
			return EvidenceFile{}, err
		}
		seen := make(map[string]struct{}, len(evidence.Observations))
		for _, observation := range evidence.Observations {
			seen[observation.CaseID] = struct{}{}
		}
		for _, evalCase := range options.Dataset.Cases {
			if _, exists := seen[evalCase.ID]; exists {
				continue
			}
			if options.MaxNewCases > 0 && newCases >= options.MaxNewCases {
				return options.Recorder.Load()
			}
			if err := ctx.Err(); err != nil {
				latest, loadErr := options.Recorder.Load()
				if loadErr != nil {
					return EvidenceFile{}, loadErr
				}
				return latest, err
			}
			observation, err := options.Executor.Execute(ctx, evalCase, configuration)
			if err != nil {
				return EvidenceFile{}, fmt.Errorf("execute %s/%s before terminal evidence: %w", configuration.Mode, evalCase.ID, err)
			}
			if observation.CaseID == "" {
				observation.CaseID = evalCase.ID
			}
			if observation.CaseID != evalCase.ID {
				return EvidenceFile{}, fmt.Errorf("executor returned case %q for %q", observation.CaseID, evalCase.ID)
			}
			evidence.Observations = append(evidence.Observations, observation)
			slices.SortFunc(evidence.Observations, func(a, b Observation) int { return strings.Compare(a.CaseID, b.CaseID) })
			if err := options.Recorder.Append(ctx, evidence); err != nil {
				return EvidenceFile{}, err
			}
			file, err = options.Recorder.Load()
			if err != nil {
				return EvidenceFile{}, err
			}
			newCases++
			if options.OnCompleted != nil {
				options.OnCompleted(configuration.Mode, evalCase.ID, len(evidence.Observations), len(options.Dataset.Cases))
			}
		}
	}
	return options.Recorder.Load()
}

func resumeEvidence(file EvidenceFile, dataset Dataset, configuration Configuration, observedAt time.Time) (Evidence, error) {
	for _, existing := range file.Runs {
		if existing.Configuration.Mode != configuration.Mode {
			continue
		}
		if existing.Dataset != dataset.Name {
			return Evidence{}, fmt.Errorf("existing %s evidence belongs to dataset %q", configuration.Mode, existing.Dataset)
		}
		left, _ := json.Marshal(existing.Configuration)
		right, _ := json.Marshal(configuration)
		if string(left) != string(right) {
			return Evidence{}, fmt.Errorf("existing %s evidence configuration changed; use a new output path", configuration.Mode)
		}
		return existing, nil
	}
	return Evidence{Dataset: dataset.Name, Configuration: configuration, ObservedAt: observedAt.UTC(), Observations: []Observation{}}, nil
}

func atomicWrite(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create eval evidence directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".forgeflow-evidence-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary eval evidence: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	var renameErr error
	for attempt := 0; attempt < 5; attempt++ {
		if renameErr = os.Rename(temporaryName, path); renameErr == nil {
			return nil
		}
		if !errors.Is(renameErr, os.ErrPermission) {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	return fmt.Errorf("commit eval evidence: %w", renameErr)
}
