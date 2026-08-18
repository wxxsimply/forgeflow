package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"forgeflow/internal/apperror"
)

type DockerConfig struct {
	Enabled        bool
	Binary         string
	WorkspaceRoot  string
	AllowedImages  []string
	CPUs           string
	Memory         string
	PIDsLimit      int
	TmpfsSizeBytes int64
	MaxOutputBytes int64
	MaxTimeout     time.Duration
}

type DockerRunner struct {
	enabled        bool
	binary         string
	workspaceRoot  string
	allowedImages  map[string]struct{}
	cpus           string
	memory         string
	pidsLimit      int
	tmpfsSizeBytes int64
	maxOutputBytes int64
	maxTimeout     time.Duration
}

func NewDockerRunner(configuration DockerConfig) (*DockerRunner, error) {
	binary := strings.TrimSpace(configuration.Binary)
	if binary == "" {
		binary = "docker"
	}
	if filepath.Base(binary) != binary || strings.ContainsAny(binary, "\x00\r\n") {
		return nil, apperror.New(apperror.CodeValidation, "Docker binary must be a bare executable name")
	}
	root, err := canonicalDirectory(configuration.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	allowedImages := make(map[string]struct{}, len(configuration.AllowedImages))
	for _, image := range configuration.AllowedImages {
		if !pinnedImagePattern.MatchString(image) {
			return nil, apperror.New(apperror.CodeValidation, "sandbox images must be pinned by sha256 digest")
		}
		allowedImages[image] = struct{}{}
	}
	if configuration.CPUs == "" {
		configuration.CPUs = "1.0"
	}
	cpus, err := strconv.ParseFloat(configuration.CPUs, 64)
	if err != nil || cpus <= 0 || math.IsNaN(cpus) || math.IsInf(cpus, 0) {
		return nil, apperror.New(apperror.CodeValidation, "sandbox CPU limit is invalid")
	}
	if configuration.Memory == "" {
		configuration.Memory = "512m"
	}
	if !memoryLimitPattern.MatchString(configuration.Memory) {
		return nil, apperror.New(apperror.CodeValidation, "sandbox memory limit is invalid")
	}
	if configuration.PIDsLimit <= 0 {
		configuration.PIDsLimit = 128
	}
	if configuration.TmpfsSizeBytes <= 0 {
		configuration.TmpfsSizeBytes = 64 * 1024 * 1024
	}
	if configuration.MaxOutputBytes <= 0 {
		configuration.MaxOutputBytes = 2 * 1024 * 1024
	}
	if configuration.MaxTimeout <= 0 {
		configuration.MaxTimeout = 10 * time.Minute
	}
	return &DockerRunner{
		enabled: configuration.Enabled, binary: binary, workspaceRoot: root, allowedImages: allowedImages,
		cpus: configuration.CPUs, memory: configuration.Memory, pidsLimit: configuration.PIDsLimit,
		tmpfsSizeBytes: configuration.TmpfsSizeBytes, maxOutputBytes: configuration.MaxOutputBytes,
		maxTimeout: configuration.MaxTimeout,
	}, nil
}

func (r *DockerRunner) Run(ctx context.Context, request Request) (Result, error) {
	if !r.enabled {
		return Result{}, apperror.New(apperror.CodePolicyDenied, "Docker sandbox execution is disabled")
	}
	args, containerName, err := r.BuildArgs(request)
	if err != nil {
		return Result{}, err
	}
	timeout := request.Timeout
	if timeout <= 0 || timeout > r.maxTimeout {
		timeout = r.maxTimeout
	}
	runContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(runContext, r.binary, args...)
	outputBudget := &sharedOutputBudget{remaining: r.maxOutputBytes}
	stdout := newLimitedBuffer(outputBudget)
	stderr := newLimitedBuffer(outputBudget)
	command.Stdout = stdout
	command.Stderr = stderr
	startedAt := time.Now()
	err = command.Run()
	duration := time.Since(startedAt)
	result := Result{ExitCode: exitCode(err), Stdout: stdout.String(), Stderr: stderr.String(), Duration: duration, Truncated: stdout.truncated || stderr.truncated, ContainerID: containerName}
	if runContext.Err() != nil {
		r.cleanupContainer(containerName)
		return result, apperror.Wrap(runContext.Err(), apperror.CodeTimeout, "sandbox.docker.run", "sandbox command timed out or was cancelled")
	}
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return result, nil
		}
		return result, apperror.Wrap(err, apperror.CodeTransient, "sandbox.docker.run", "Docker sandbox could not be started")
	}
	return result, nil
}

func (r *DockerRunner) BuildArgs(request Request) ([]string, string, error) {
	if _, allowed := r.allowedImages[request.Image]; !allowed {
		return nil, "", apperror.New(apperror.CodePolicyDenied, "sandbox image is not in the trusted digest allowlist")
	}
	workspace, err := filepath.Abs(request.WorkspacePath)
	if err != nil {
		return nil, "", apperror.Wrap(err, apperror.CodeValidation, "sandbox.workspace", "sandbox workspace path is invalid")
	}
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil || filepath.Clean(workspace) == filepath.Clean(r.workspaceRoot) || !withinRoot(r.workspaceRoot, workspace) {
		return nil, "", apperror.New(apperror.CodePolicyDenied, "sandbox workspace escapes the managed root")
	}
	if strings.Contains(workspace, ",") {
		return nil, "", apperror.New(apperror.CodePolicyDenied, "sandbox workspace path cannot contain a mount separator")
	}
	workingDir, err := resolveWorkingDirectory(workspace, request.WorkingDir)
	if err != nil {
		return nil, "", err
	}
	if !programPattern.MatchString(request.Program) || shellProgram(request.Program) {
		return nil, "", apperror.New(apperror.CodePolicyDenied, "sandbox program must be a bare executable name")
	}
	for _, argument := range request.Args {
		if strings.ContainsRune(argument, 0) {
			return nil, "", apperror.New(apperror.CodePolicyDenied, "sandbox command contains an invalid argument")
		}
	}
	containerName := "forgeflow-" + randomSuffix()
	containerWorkdir := "/workspace"
	if relative, _ := filepath.Rel(workspace, workingDir); relative != "." {
		containerWorkdir += "/" + filepath.ToSlash(relative)
	}
	args := []string{
		"run", "--rm", "--name", containerName,
		"--network", "none", "--read-only", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges", "--user", "10001:10001",
		"--cpus", r.cpus, "--memory", r.memory, "--pids-limit", strconv.Itoa(r.pidsLimit),
		"--tmpfs", fmt.Sprintf("/tmp:rw,noexec,nosuid,size=%d", r.tmpfsSizeBytes),
		"--mount", "type=bind,src=" + workspace + ",dst=/workspace,rw",
		"--workdir", containerWorkdir,
	}
	keys := make([]string, 0, len(request.Environment))
	for key := range request.Environment {
		if !environmentNamePattern.MatchString(key) || secretEnvironmentName(key) {
			return nil, "", apperror.New(apperror.CodePolicyDenied, "sandbox environment contains a forbidden variable")
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if strings.ContainsAny(request.Environment[key], "\x00\r\n") {
			return nil, "", apperror.New(apperror.CodePolicyDenied, "sandbox environment contains an invalid value")
		}
		args = append(args, "--env", key+"="+request.Environment[key])
	}
	args = append(args, request.Image, request.Program)
	args = append(args, request.Args...)
	return args, containerName, nil
}

func (r *DockerRunner) cleanupContainer(containerName string) {
	cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(cleanupContext, r.binary, "rm", "-f", containerName)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	_ = command.Run()
}

func canonicalDirectory(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", apperror.New(apperror.CodeValidation, "sandbox workspace root is required")
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", apperror.Wrap(err, apperror.CodeValidation, "sandbox.root", "sandbox workspace root is inaccessible")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", apperror.New(apperror.CodeValidation, "sandbox workspace root must be a directory")
	}
	return filepath.Clean(resolved), nil
}

func resolveWorkingDirectory(workspace, relative string) (string, error) {
	if relative == "" {
		relative = "."
	}
	if filepath.IsAbs(relative) || strings.ContainsRune(relative, 0) {
		return "", apperror.New(apperror.CodePolicyDenied, "sandbox working directory must be relative")
	}
	cleaned := filepath.Clean(relative)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", apperror.New(apperror.CodePolicyDenied, "sandbox working directory escapes the workspace")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(workspace, cleaned))
	if err != nil || !withinRoot(workspace, resolved) {
		return "", apperror.New(apperror.CodePolicyDenied, "sandbox working directory is inaccessible or escapes the workspace")
	}
	return filepath.Clean(resolved), nil
}

func withinRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}

func randomSuffix() string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value[:])
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	budget    *sharedOutputBudget
	truncated bool
}

type sharedOutputBudget struct {
	mu        sync.Mutex
	remaining int64
}

func newLimitedBuffer(budget *sharedOutputBudget) *limitedBuffer {
	return &limitedBuffer{budget: budget}
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	original := len(data)
	b.budget.mu.Lock()
	defer b.budget.mu.Unlock()
	if b.budget.remaining <= 0 {
		b.truncated = true
		return original, nil
	}
	if int64(len(data)) > b.budget.remaining {
		data = data[:b.budget.remaining]
		b.truncated = true
	}
	_, _ = b.buffer.Write(data)
	b.budget.remaining -= int64(len(data))
	return original, nil
}

func (b *limitedBuffer) String() string { return b.buffer.String() }

var pinnedImagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._/:@-]*@sha256:[a-f0-9]{64}$`)
var programPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
var environmentNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
var memoryLimitPattern = regexp.MustCompile(`^[1-9][0-9]*(?:[kKmMgG])?$`)

func shellProgram(program string) bool {
	switch strings.ToLower(program) {
	case "sh", "bash", "zsh", "cmd", "cmd.exe", "powershell", "powershell.exe", "pwsh":
		return true
	default:
		return false
	}
}

func secretEnvironmentName(name string) bool {
	upper := strings.ToUpper(name)
	for _, fragment := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "CREDENTIAL", "COOKIE"} {
		if strings.Contains(upper, fragment) {
			return true
		}
	}
	return false
}

var _ Runner = (*DockerRunner)(nil)
