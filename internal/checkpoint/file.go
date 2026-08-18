package checkpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
)

var runIDPattern = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

var ErrNotFound = errors.New("checkpoint not found")
var ErrConflict = errors.New("checkpoint version conflict")

var fileStoreLocks sync.Map

type FileStore struct {
	directory string
	mutex     *sync.Mutex
}

func NewFileStore(directory string) *FileStore {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		absolute = filepath.Clean(directory)
	}
	lock, _ := fileStoreLocks.LoadOrStore(absolute, &sync.Mutex{})
	return &FileStore{directory: directory, mutex: lock.(*sync.Mutex)}
}

func (s *FileStore) Save(ctx context.Context, state *domain.RunState, expectedVersion int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if err := os.MkdirAll(s.directory, 0o755); err != nil {
		return fmt.Errorf("create checkpoint directory: %w", err)
	}
	currentVersion, exists, err := s.currentVersion(state.RunID)
	if err != nil {
		return err
	}
	if state.Version != expectedVersion || (exists && currentVersion != expectedVersion) || (!exists && expectedVersion != 0) {
		return ErrConflict
	}

	snapshot := *state
	snapshot.Version = expectedVersion + 1
	data, err := json.MarshalIndent(&snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode checkpoint: %w", err)
	}
	data = append(data, '\n')

	temporary, err := os.CreateTemp(s.directory, state.RunID+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary checkpoint: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)

	if _, err = temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write checkpoint: %w", err)
	}
	if err = temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync checkpoint: %w", err)
	}
	if err = temporary.Close(); err != nil {
		return fmt.Errorf("close checkpoint: %w", err)
	}
	if err = renameWithTransientRetry(temporaryName, s.path(state.RunID)); err != nil {
		return fmt.Errorf("commit checkpoint: %w", err)
	}
	state.Version = snapshot.Version
	return nil
}

func renameWithTransientRetry(source, target string) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if err = os.Rename(source, target); err == nil {
			return nil
		}
		if !errors.Is(err, os.ErrPermission) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	return err
}

func (s *FileStore) Load(_ context.Context, runID string) (*domain.RunState, error) {
	if !runIDPattern.MatchString(runID) {
		return nil, apperror.New(apperror.CodeValidation, "invalid run id")
	}
	data, err := os.ReadFile(s.path(runID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read checkpoint: %w", err)
	}
	var state domain.RunState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode checkpoint: %w", err)
	}
	if state.NodeExecutions == nil {
		state.NodeExecutions = map[string]domain.NodeExecution{}
	}
	if state.PendingBranches == nil {
		state.PendingBranches = map[string]domain.BranchState{}
	}
	if state.ToolCallAudits == nil {
		state.ToolCallAudits = []domain.ToolCallAudit{}
	}
	if state.AssessmentErrors == nil {
		state.AssessmentErrors = map[string]string{}
	}
	if state.JudgeDecisions == nil {
		state.JudgeDecisions = []domain.JudgeDecision{}
	}
	return &state, nil
}

func (s *FileStore) currentVersion(runID string) (int64, bool, error) {
	data, err := os.ReadFile(s.path(runID))
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read current checkpoint version: %w", err)
	}
	var current struct {
		Version int64 `json:"version"`
	}
	if err := json.Unmarshal(data, &current); err != nil {
		return 0, false, fmt.Errorf("decode current checkpoint version: %w", err)
	}
	return current.Version, true, nil
}

func (s *FileStore) path(runID string) string {
	return filepath.Join(s.directory, runID+".json")
}
