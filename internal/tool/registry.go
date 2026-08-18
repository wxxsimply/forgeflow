package tool

import (
	"encoding/json"
	"regexp"
	"sort"
	"sync"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
)

type Registry struct {
	mu     sync.RWMutex
	tools  map[string]Tool
	sealed bool
}

func NewRegistry() *Registry { return &Registry{tools: map[string]Tool{}} }

func (r *Registry) Register(candidate Tool) error {
	if candidate == nil {
		return apperror.New(apperror.CodeValidation, "cannot register a nil tool")
	}
	specification := candidate.Spec()
	if !toolNamePattern.MatchString(specification.Name) || !toolVersionPattern.MatchString(specification.Version) {
		return apperror.New(apperror.CodeValidation, "tool name or version is invalid")
	}
	if specification.Description == "" || !json.Valid(specification.InputSchema) || !json.Valid(specification.OutputSchema) {
		return apperror.New(apperror.CodeValidation, "tool description and valid schemas are required")
	}
	if specification.Risk != domain.RiskLow && specification.Risk != domain.RiskMedium && specification.Risk != domain.RiskHigh {
		return apperror.New(apperror.CodeValidation, "tool risk is invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sealed {
		return apperror.New(apperror.CodeConflict, "tool registry is sealed")
	}
	if _, exists := r.tools[specification.Name]; exists {
		return apperror.New(apperror.CodeConflict, "tool name is already registered")
	}
	r.tools[specification.Name] = candidate
	return nil
}

func (r *Registry) Seal() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sealed = true
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	candidate, exists := r.tools[name]
	return candidate, exists
}

func (r *Registry) Specs() []Spec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Spec, 0, len(r.tools))
	for _, candidate := range r.tools {
		result = append(result, candidate.Spec())
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result
}

var toolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)
var toolVersionPattern = regexp.MustCompile(`^v[1-9][0-9]*$`)
