package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
	"forgeflow/internal/policy"
	"forgeflow/internal/repository"
)

const objectSchema = `{"type":"object","additionalProperties":false}`

const listFilesInputSchema = `{"type":"object","properties":{"path":{"type":"string"},"recursive":{"type":"boolean"}},"additionalProperties":false}`
const readFileInputSchema = `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`
const searchCodeInputSchema = `{"type":"object","properties":{"query":{"type":"string"},"caseSensitive":{"type":"boolean"},"extensions":{"type":"array","items":{"type":"string"}}},"required":["query"],"additionalProperties":false}`
const inspectGitInputSchema = `{"type":"object","properties":{"baseRevision":{"type":"string"}},"additionalProperties":false}`
const fileListOutputSchema = `{"type":"object","properties":{"entries":{"type":"array","items":{"type":"object"}},"truncated":{"type":"boolean"}},"required":["entries","truncated"]}`
const fileContentOutputSchema = `{"type":"object","properties":{"path":{"type":"string"},"size":{"type":"integer"},"sha256":{"type":"string"},"text":{"type":"string"}},"required":["path","size","sha256","text"]}`
const searchOutputSchema = `{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"},"line":{"type":"integer"},"preview":{"type":"string"}},"required":["path","line","preview"]}}`
const projectRulesOutputSchema = `{"type":"object","properties":{"documents":{"type":"array","items":{"type":"object"}}},"required":["documents"]}`
const gitStatusOutputSchema = `{"type":"object","properties":{"root":{"type":"string"},"requestedRevision":{"type":"string"},"baseCommit":{"type":"string"},"headCommit":{"type":"string"},"clean":{"type":"boolean"}},"required":["root","requestedRevision","baseCommit","headCommit","clean"]}`

type repositoryTool struct {
	spec     Spec
	analyze  func(json.RawMessage) (policy.Metadata, error)
	execute  func(context.Context, CallContext, json.RawMessage) (any, error)
	validate func(json.RawMessage) error
}

func (t repositoryTool) Spec() Spec { return t.spec }

func (t repositoryTool) Analyze(input json.RawMessage) (policy.Metadata, error) {
	return t.analyze(input)
}

func (t repositoryTool) Execute(ctx context.Context, call CallContext, input json.RawMessage) (json.RawMessage, error) {
	value, err := t.execute(ctx, call, input)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, apperror.Wrap(err, apperror.CodeInternal, "tool.repository.encode", "repository tool output could not be encoded")
	}
	return encoded, nil
}

func (t repositoryTool) ValidateOutput(output json.RawMessage) error { return t.validate(output) }

type listFilesInput struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

type readFileInput struct {
	Path string `json:"path"`
}

type searchCodeInput struct {
	Query         string   `json:"query"`
	CaseSensitive bool     `json:"caseSensitive"`
	Extensions    []string `json:"extensions"`
}

type inspectGitInput struct {
	BaseRevision string `json:"baseRevision"`
}

type projectRulesOutput struct {
	Documents []repository.FileContent `json:"documents"`
}

type gitStatusOutput struct {
	Root              string `json:"root"`
	RequestedRevision string `json:"requestedRevision"`
	BaseCommit        string `json:"baseCommit"`
	HeadCommit        string `json:"headCommit"`
	Clean             bool   `json:"clean"`
}

func RegisterRepositoryTools(registry *Registry, limits repository.Limits, inspector repository.RepositoryInspector) error {
	if registry == nil || inspector == nil {
		return apperror.New(apperror.CodeValidation, "repository tool dependencies are required")
	}
	tools := []Tool{
		newListFilesTool(limits),
		newReadFileTool(limits),
		newSearchCodeTool(limits),
		newReadProjectRulesTool(limits),
		newInspectGitStatusTool(inspector),
	}
	for _, candidate := range tools {
		if err := registry.Register(candidate); err != nil {
			return err
		}
	}
	return nil
}

func newListFilesTool(limits repository.Limits) Tool {
	decode := func(input json.RawMessage) (listFilesInput, error) {
		var request listFilesInput
		if err := decodeStrict(input, &request); err != nil {
			return request, err
		}
		if strings.TrimSpace(request.Path) == "" {
			request.Path = "."
		}
		return request, nil
	}
	return repositoryTool{
		spec: repositorySpec("list_files", "List files below a repository-relative directory.", listFilesInputSchema, fileListOutputSchema, 2*1024*1024),
		analyze: func(input json.RawMessage) (policy.Metadata, error) {
			request, err := decode(input)
			return policy.Metadata{Paths: []string{request.Path}}, err
		},
		execute: func(ctx context.Context, call CallContext, input json.RawMessage) (any, error) {
			request, err := decode(input)
			if err != nil {
				return nil, err
			}
			reader, err := repository.NewSafeFileReader(call.Workspace.Path, limits)
			if err != nil {
				return nil, err
			}
			return reader.ListFiles(ctx, request.Path, request.Recursive)
		},
		validate: validateJSON[repository.FileList],
	}
}

func newReadFileTool(limits repository.Limits) Tool {
	decode := func(input json.RawMessage) (readFileInput, error) {
		var request readFileInput
		if err := decodeStrict(input, &request); err != nil {
			return request, err
		}
		if strings.TrimSpace(request.Path) == "" {
			return request, apperror.New(apperror.CodeValidation, "read_file path is required")
		}
		return request, nil
	}
	return repositoryTool{
		spec: repositorySpec("read_file", "Read one UTF-8 repository file with its digest.", readFileInputSchema, fileContentOutputSchema, limits.MaxReadBytes+64*1024),
		analyze: func(input json.RawMessage) (policy.Metadata, error) {
			request, err := decode(input)
			return policy.Metadata{Paths: []string{request.Path}}, err
		},
		execute: func(ctx context.Context, call CallContext, input json.RawMessage) (any, error) {
			request, err := decode(input)
			if err != nil {
				return nil, err
			}
			reader, err := repository.NewSafeFileReader(call.Workspace.Path, limits)
			if err != nil {
				return nil, err
			}
			return reader.ReadFile(ctx, request.Path)
		},
		validate: validateJSON[repository.FileContent],
	}
}

func newSearchCodeTool(limits repository.Limits) Tool {
	decode := func(input json.RawMessage) (searchCodeInput, error) {
		var request searchCodeInput
		if err := decodeStrict(input, &request); err != nil {
			return request, err
		}
		if strings.TrimSpace(request.Query) == "" {
			return request, apperror.New(apperror.CodeValidation, "search_code query is required")
		}
		for _, extension := range request.Extensions {
			if !strings.HasPrefix(extension, ".") || strings.ContainsAny(extension, `/\\\x00`) {
				return request, apperror.New(apperror.CodeValidation, "search_code extensions must use values such as .go")
			}
		}
		return request, nil
	}
	return repositoryTool{
		spec: repositorySpec("search_code", "Search repository text without invoking a shell.", searchCodeInputSchema, searchOutputSchema, 2*1024*1024),
		analyze: func(input json.RawMessage) (policy.Metadata, error) {
			_, err := decode(input)
			return policy.Metadata{Paths: []string{"."}}, err
		},
		execute: func(ctx context.Context, call CallContext, input json.RawMessage) (any, error) {
			request, err := decode(input)
			if err != nil {
				return nil, err
			}
			reader, err := repository.NewSafeFileReader(call.Workspace.Path, limits)
			if err != nil {
				return nil, err
			}
			return reader.SearchCode(ctx, request.Query, repository.SearchOptions{
				CaseSensitive: request.CaseSensitive,
				Extensions:    request.Extensions,
			})
		},
		validate: validateJSON[[]repository.SearchMatch],
	}
}

func newReadProjectRulesTool(limits repository.Limits) Tool {
	type noInput struct{}
	decode := func(input json.RawMessage) error {
		var request noInput
		return decodeStrict(input, &request)
	}
	return repositoryTool{
		spec: repositorySpec("read_project_rules", "Read repository AGENTS.md instruction files in stable path order.", objectSchema, projectRulesOutputSchema, 1024*1024),
		analyze: func(input json.RawMessage) (policy.Metadata, error) {
			return policy.Metadata{Paths: []string{"."}}, decode(input)
		},
		execute: func(ctx context.Context, call CallContext, input json.RawMessage) (any, error) {
			if err := decode(input); err != nil {
				return nil, err
			}
			reader, err := repository.NewSafeFileReader(call.Workspace.Path, limits)
			if err != nil {
				return nil, err
			}
			listed, err := reader.ListFiles(ctx, ".", true)
			if err != nil {
				return nil, err
			}
			paths := make([]string, 0)
			for _, entry := range listed.Entries {
				if !entry.IsDir && !entry.IsSymlink && strings.EqualFold(filepath.Base(entry.Path), "AGENTS.md") {
					paths = append(paths, entry.Path)
				}
			}
			sort.Strings(paths)
			if len(paths) > 32 {
				paths = paths[:32]
			}
			documents := make([]repository.FileContent, 0, len(paths))
			for _, path := range paths {
				content, err := reader.ReadFile(ctx, path)
				if err != nil {
					return nil, err
				}
				documents = append(documents, content)
			}
			return projectRulesOutput{Documents: documents}, nil
		},
		validate: validateJSON[projectRulesOutput],
	}
}

func newInspectGitStatusTool(inspector repository.RepositoryInspector) Tool {
	decode := func(input json.RawMessage) (inspectGitInput, error) {
		var request inspectGitInput
		if err := decodeStrict(input, &request); err != nil {
			return request, err
		}
		if strings.TrimSpace(request.BaseRevision) == "" {
			request.BaseRevision = "HEAD"
		}
		return request, nil
	}
	return repositoryTool{
		spec: repositorySpec("inspect_git_status", "Inspect repository revision and cleanliness without returning file contents.", inspectGitInputSchema, gitStatusOutputSchema, 64*1024),
		analyze: func(input json.RawMessage) (policy.Metadata, error) {
			_, err := decode(input)
			return policy.Metadata{Paths: []string{"."}}, err
		},
		execute: func(ctx context.Context, call CallContext, input json.RawMessage) (any, error) {
			request, err := decode(input)
			if err != nil {
				return nil, err
			}
			summary, err := inspector.Inspect(ctx, domain.RepositoryRef{Path: call.Workspace.Path, BaseRevision: request.BaseRevision})
			if err != nil {
				return nil, err
			}
			return gitStatusOutput{
				Root: summary.Root, RequestedRevision: summary.RequestedRevision,
				BaseCommit: summary.BaseCommit, HeadCommit: summary.HeadCommit, Clean: summary.Clean,
			}, nil
		},
		validate: validateJSON[gitStatusOutput],
	}
}

func repositorySpec(name, description, inputSchema, outputSchema string, maxOutput int64) Spec {
	return Spec{
		Name: name, Version: "v1", Description: description,
		InputSchema: json.RawMessage(inputSchema), OutputSchema: json.RawMessage(outputSchema),
		Risk: domain.RiskLow, Timeout: 30 * time.Second, MaxOutputBytes: maxOutput,
	}
}

func decodeStrict(input json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return apperror.Wrap(err, apperror.CodeValidation, "tool.input.contract", "tool input does not match its contract")
	}
	if err := requireEOF(decoder); err != nil && err != io.EOF {
		return apperror.Wrap(err, apperror.CodeValidation, "tool.input.contract", "tool input contains trailing data")
	}
	return nil
}

func validateJSON[T any](output json.RawMessage) error {
	var value T
	if err := json.Unmarshal(output, &value); err != nil {
		return err
	}
	return nil
}
