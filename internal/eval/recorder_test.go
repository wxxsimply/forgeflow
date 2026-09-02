package eval

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

type baselineExecutorFunc func(context.Context, Case, Configuration) (Observation, error)

func (f baselineExecutorFunc) Execute(ctx context.Context, evalCase Case, configuration Configuration) (Observation, error) {
	return f(ctx, evalCase, configuration)
}

func TestRunResumableWritesEachCaseOnce(t *testing.T) {
	dataset, err := Load(SoftwareV1)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	executor := baselineExecutorFunc(func(_ context.Context, evalCase Case, _ Configuration) (Observation, error) {
		calls++
		value := passingObservation(evalCase, true)
		value.Outcome = "completed"
		return value, nil
	})
	configuration := evidenceFor(dataset, ModeSingleAgent, true).Configuration
	path := filepath.Join(t.TempDir(), "private", "evidence.json")
	options := ResumableOptions{Dataset: dataset, Configurations: []Configuration{configuration}, Executor: executor, Recorder: FileRecorder{Path: path}, MaxNewCases: 1, Now: func() time.Time { return time.Unix(10, 0) }}
	file, err := RunResumable(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(file.Runs) != 1 || len(file.Runs[0].Observations) != 1 {
		t.Fatalf("calls=%d file=%+v", calls, file)
	}
	file, err = RunResumable(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(file.Runs[0].Observations) != 2 {
		t.Fatalf("resume calls=%d observations=%d", calls, len(file.Runs[0].Observations))
	}
}

func TestFileRecorderRejectsChangedConfiguration(t *testing.T) {
	dataset, err := Load(SoftwareV1)
	if err != nil {
		t.Fatal(err)
	}
	configuration := evidenceFor(dataset, ModeSingleAgent, true).Configuration
	path := filepath.Join(t.TempDir(), "evidence.json")
	recorder := FileRecorder{Path: path}
	if err := recorder.Append(context.Background(), Evidence{Dataset: dataset.Name, Configuration: configuration, ObservedAt: time.Unix(1, 0), Observations: []Observation{}}); err != nil {
		t.Fatal(err)
	}
	configuration.GitCommit = "0000000000000000000000000000000000000002"
	_, err = RunResumable(context.Background(), ResumableOptions{Dataset: dataset, Configurations: []Configuration{configuration}, Executor: baselineExecutorFunc(func(context.Context, Case, Configuration) (Observation, error) { return Observation{}, nil }), Recorder: recorder})
	if err == nil {
		t.Fatal("expected changed configuration to be rejected")
	}
}

func TestFileRecorderRejectsChangedReasoningEffort(t *testing.T) {
	dataset, err := Load(SoftwareV1)
	if err != nil {
		t.Fatal(err)
	}
	configuration := evidenceFor(dataset, ModeSingleAgent, true).Configuration
	configuration.ReasoningEffort = "low"
	path := filepath.Join(t.TempDir(), "evidence.json")
	recorder := FileRecorder{Path: path}
	if err := recorder.Append(context.Background(), Evidence{Dataset: dataset.Name, Configuration: configuration, ObservedAt: time.Unix(1, 0), Observations: []Observation{}}); err != nil {
		t.Fatal(err)
	}
	configuration.ReasoningEffort = "medium"
	_, err = RunResumable(context.Background(), ResumableOptions{Dataset: dataset, Configurations: []Configuration{configuration}, Executor: baselineExecutorFunc(func(context.Context, Case, Configuration) (Observation, error) { return Observation{}, nil }), Recorder: recorder})
	if err == nil {
		t.Fatal("expected changed reasoning effort to be rejected")
	}
}

func TestFileRecorderRejectsChangedCostBudget(t *testing.T) {
	dataset, err := Load(SoftwareV1)
	if err != nil {
		t.Fatal(err)
	}
	configuration := evidenceFor(dataset, ModeSingleAgent, true).Configuration
	configuration.MaxTotalCostUSD = 1
	configuration.PriorCostUSD = 0.1
	path := filepath.Join(t.TempDir(), "evidence.json")
	recorder := FileRecorder{Path: path}
	if err := recorder.Append(context.Background(), Evidence{Dataset: dataset.Name, Configuration: configuration, ObservedAt: time.Unix(1, 0), Observations: []Observation{}}); err != nil {
		t.Fatal(err)
	}
	configuration.MaxTotalCostUSD = 2
	_, err = RunResumable(context.Background(), ResumableOptions{Dataset: dataset, Configurations: []Configuration{configuration}, Executor: baselineExecutorFunc(func(context.Context, Case, Configuration) (Observation, error) { return Observation{}, nil }), Recorder: recorder})
	if err == nil {
		t.Fatal("expected changed cost budget to be rejected")
	}
}
