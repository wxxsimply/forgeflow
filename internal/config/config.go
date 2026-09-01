package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment              string
	LogLevel                 string
	ServiceVersion           string
	OTLPEndpoint             string
	OTELSampleRatio          float64
	MetricsEnabled           bool
	HTTPAddress              string
	HTTPCookieSecure         bool
	HTTPCookieDomain         string
	HTTPAllowedOrigins       []string
	RepositoryRoots          []string
	SessionTTL               time.Duration
	SessionIdleTTL           time.Duration
	BootstrapAdminEmail      string
	BootstrapAdminPassword   string
	DataDir                  string
	WorkflowMode             string
	PlannerMode              string
	ModelProvider            string
	OpenAIAPIKey             string
	OpenAIBaseURL            string
	OpenAIOrganization       string
	OpenAIProject            string
	OpenAIMaxRetries         int
	PlannerModel             string
	PlannerPromptVersion     string
	PlannerReasoningEffort   string
	PlannerMaxOutputTokens   int
	PlannerTimeout           time.Duration
	PlannerInputUSDPerMTok   float64
	PlannerOutputUSDPerMTok  float64
	DeveloperModel           string
	DeveloperPromptVersion   string
	DeveloperReasoningEffort string
	DeveloperMaxOutputTokens int
	DeveloperTimeout         time.Duration
	DeveloperContextMaxBytes int
	ReviewerModel            string
	ReviewerPromptVersion    string
	ReviewerReasoningEffort  string
	ReviewerMaxOutputTokens  int
	ReviewerTimeout          time.Duration
	ReviewerContextMaxBytes  int
	SecurityModel            string
	SecurityPromptVersion    string
	SecurityReasoningEffort  string
	SecurityMaxOutputTokens  int
	SecurityTimeout          time.Duration
	SecurityContextMaxBytes  int
	PostgresEnabled          bool
	PostgresDSN              string
	PostgresMaxOpenConns     int
	PostgresMaxIdleConns     int
	PostgresConnMaxLifetime  time.Duration
	PostgresPingTimeout      time.Duration
	ArtifactRoot             string
	ArtifactMaxBytes         int
	WorkerLeaseTTL           time.Duration
	WorkerHeartbeatInterval  time.Duration
	WorkerPollInterval       time.Duration
	WorkerMetricsAddress     string
	EnforceActiveReleases    bool
	DockerEnabled            bool
	DockerBinary             string
	SandboxWorkspaceRoot     string
	SandboxImage             string
	SandboxCPUs              string
	SandboxMemory            string
	SandboxPIDsLimit         int
	SandboxTmpfsBytes        int
	SandboxMaxOutputBytes    int
	SandboxTimeout           time.Duration
}

func Load() (Config, error) {
	otelSampleRatio, err := envFloat("FORGEFLOW_OTEL_SAMPLE_RATIO", 0.1)
	if err != nil {
		return Config{}, err
	}
	metricsEnabled, err := envBool("FORGEFLOW_METRICS_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	httpCookieSecure, err := envBool("FORGEFLOW_HTTP_COOKIE_SECURE", true)
	if err != nil {
		return Config{}, err
	}
	sessionTTL, err := envDuration("FORGEFLOW_SESSION_TTL", 24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	sessionIdleTTL, err := envDuration("FORGEFLOW_SESSION_IDLE_TTL", 30*time.Minute)
	if err != nil {
		return Config{}, err
	}
	maxRetries, err := envInt("FORGEFLOW_OPENAI_MAX_RETRIES", 2)
	if err != nil {
		return Config{}, err
	}
	maxOutputTokens, err := envInt("FORGEFLOW_PLANNER_MAX_OUTPUT_TOKENS", 4_000)
	if err != nil {
		return Config{}, err
	}
	plannerTimeout, err := envDuration("FORGEFLOW_PLANNER_TIMEOUT", 120*time.Second)
	if err != nil {
		return Config{}, err
	}
	inputPrice, err := envFloat("FORGEFLOW_PLANNER_INPUT_USD_PER_MTOK", 0)
	if err != nil {
		return Config{}, err
	}
	outputPrice, err := envFloat("FORGEFLOW_PLANNER_OUTPUT_USD_PER_MTOK", 0)
	if err != nil {
		return Config{}, err
	}
	dockerEnabled, err := envBool("FORGEFLOW_DOCKER_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	sandboxPIDs, err := envInt("FORGEFLOW_SANDBOX_PIDS_LIMIT", 128)
	if err != nil {
		return Config{}, err
	}
	sandboxTmpfs, err := envInt("FORGEFLOW_SANDBOX_TMPFS_BYTES", 64*1024*1024)
	if err != nil {
		return Config{}, err
	}
	sandboxOutput, err := envInt("FORGEFLOW_SANDBOX_MAX_OUTPUT_BYTES", 2*1024*1024)
	if err != nil {
		return Config{}, err
	}
	sandboxTimeout, err := envDuration("FORGEFLOW_SANDBOX_TIMEOUT", 10*time.Minute)
	if err != nil {
		return Config{}, err
	}
	developerMaxOutput, err := envInt("FORGEFLOW_DEVELOPER_MAX_OUTPUT_TOKENS", 16_000)
	if err != nil {
		return Config{}, err
	}
	developerTimeout, err := envDuration("FORGEFLOW_DEVELOPER_TIMEOUT", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	developerContextBytes, err := envInt("FORGEFLOW_DEVELOPER_CONTEXT_MAX_BYTES", 128*1024)
	if err != nil {
		return Config{}, err
	}
	reviewerMaxOutput, err := envInt("FORGEFLOW_REVIEWER_MAX_OUTPUT_TOKENS", 3_000)
	if err != nil {
		return Config{}, err
	}
	reviewerTimeout, err := envDuration("FORGEFLOW_REVIEWER_TIMEOUT", 2*time.Minute)
	if err != nil {
		return Config{}, err
	}
	reviewerContextBytes, err := envInt("FORGEFLOW_REVIEWER_CONTEXT_MAX_BYTES", 512*1024)
	if err != nil {
		return Config{}, err
	}
	securityMaxOutput, err := envInt("FORGEFLOW_SECURITY_MAX_OUTPUT_TOKENS", 3_000)
	if err != nil {
		return Config{}, err
	}
	securityTimeout, err := envDuration("FORGEFLOW_SECURITY_TIMEOUT", 2*time.Minute)
	if err != nil {
		return Config{}, err
	}
	securityContextBytes, err := envInt("FORGEFLOW_SECURITY_CONTEXT_MAX_BYTES", 512*1024)
	if err != nil {
		return Config{}, err
	}
	postgresEnabled, err := envBool("FORGEFLOW_POSTGRES_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	postgresMaxOpen, err := envInt("FORGEFLOW_POSTGRES_MAX_OPEN_CONNS", 20)
	if err != nil {
		return Config{}, err
	}
	postgresMaxIdle, err := envInt("FORGEFLOW_POSTGRES_MAX_IDLE_CONNS", 5)
	if err != nil {
		return Config{}, err
	}
	postgresLifetime, err := envDuration("FORGEFLOW_POSTGRES_CONN_MAX_LIFETIME", 30*time.Minute)
	if err != nil {
		return Config{}, err
	}
	postgresPingTimeout, err := envDuration("FORGEFLOW_POSTGRES_PING_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	artifactMaxBytes, err := envInt("FORGEFLOW_ARTIFACT_MAX_BYTES", 64*1024*1024)
	if err != nil {
		return Config{}, err
	}
	workerLeaseTTL, err := envDuration("FORGEFLOW_WORKER_LEASE_TTL", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	workerHeartbeat, err := envDuration("FORGEFLOW_WORKER_HEARTBEAT_INTERVAL", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	workerPoll, err := envDuration("FORGEFLOW_WORKER_POLL_INTERVAL", 500*time.Millisecond)
	if err != nil {
		return Config{}, err
	}
	governanceEnforceActiveReleases, err := envBool("FORGEFLOW_GOVERNANCE_ENFORCE_ACTIVE_RELEASES", false)
	if err != nil {
		return Config{}, err
	}
	dataDirectory := envOrDefault("FORGEFLOW_DATA_DIR", ".forgeflow")
	bootstrapAdminPassword, err := envOrFile("FORGEFLOW_BOOTSTRAP_ADMIN_PASSWORD")
	if err != nil {
		return Config{}, err
	}
	openAIAPIKey, err := envOrFile("OPENAI_API_KEY")
	if err != nil {
		return Config{}, err
	}
	postgresDSN, err := envOrFile("FORGEFLOW_POSTGRES_DSN")
	if err != nil {
		return Config{}, err
	}
	configuration := Config{
		Environment: envOrDefault("FORGEFLOW_ENV", "development"), LogLevel: envOrDefault("FORGEFLOW_LOG_LEVEL", "info"),
		ServiceVersion: strings.TrimSpace(os.Getenv("FORGEFLOW_SERVICE_VERSION")), OTLPEndpoint: strings.TrimSpace(os.Getenv("FORGEFLOW_OTEL_ENDPOINT")),
		OTELSampleRatio: otelSampleRatio, MetricsEnabled: metricsEnabled,
		HTTPAddress:      envOrDefault("FORGEFLOW_HTTP_ADDRESS", "127.0.0.1:8080"),
		HTTPCookieSecure: httpCookieSecure, HTTPCookieDomain: strings.TrimSpace(os.Getenv("FORGEFLOW_HTTP_COOKIE_DOMAIN")),
		HTTPAllowedOrigins: splitCSV(os.Getenv("FORGEFLOW_HTTP_ALLOWED_ORIGINS")),
		RepositoryRoots:    splitCSV(envOrDefault("FORGEFLOW_REPOSITORY_ROOTS", ".")),
		SessionTTL:         sessionTTL, SessionIdleTTL: sessionIdleTTL,
		BootstrapAdminEmail:    strings.TrimSpace(os.Getenv("FORGEFLOW_BOOTSTRAP_ADMIN_EMAIL")),
		BootstrapAdminPassword: bootstrapAdminPassword,
		DataDir:                dataDirectory, WorkflowMode: envOrDefault("FORGEFLOW_WORKFLOW_MODE", "planning"), PlannerMode: envOrDefault("FORGEFLOW_PLANNER_MODE", "mock"),
		ModelProvider: envOrDefault("FORGEFLOW_MODEL_PROVIDER", "openai"),
		OpenAIAPIKey:  strings.TrimSpace(openAIAPIKey), OpenAIBaseURL: envOrDefault("FORGEFLOW_OPENAI_BASE_URL", "https://api.openai.com/v1"),
		OpenAIOrganization: strings.TrimSpace(os.Getenv("OPENAI_ORGANIZATION")), OpenAIProject: strings.TrimSpace(os.Getenv("OPENAI_PROJECT")),
		OpenAIMaxRetries: maxRetries, PlannerModel: envOrDefault("FORGEFLOW_PLANNER_MODEL", "gpt-5.6"),
		PlannerPromptVersion:   envOrDefault("FORGEFLOW_PLANNER_PROMPT_VERSION", "planner/v1"),
		PlannerReasoningEffort: envOrDefault("FORGEFLOW_PLANNER_REASONING_EFFORT", "medium"),
		PlannerMaxOutputTokens: maxOutputTokens, PlannerTimeout: plannerTimeout,
		PlannerInputUSDPerMTok: inputPrice, PlannerOutputUSDPerMTok: outputPrice,
		DeveloperModel:           envOrDefault("FORGEFLOW_DEVELOPER_MODEL", "gpt-5.6"),
		DeveloperPromptVersion:   envOrDefault("FORGEFLOW_DEVELOPER_PROMPT_VERSION", "developer/v1"),
		DeveloperReasoningEffort: envOrDefault("FORGEFLOW_DEVELOPER_REASONING_EFFORT", "medium"),
		DeveloperMaxOutputTokens: developerMaxOutput, DeveloperTimeout: developerTimeout,
		DeveloperContextMaxBytes: developerContextBytes,
		ReviewerModel:            envOrDefault("FORGEFLOW_REVIEWER_MODEL", "gpt-5.6"),
		ReviewerPromptVersion:    envOrDefault("FORGEFLOW_REVIEWER_PROMPT_VERSION", "reviewer/v1"),
		ReviewerReasoningEffort:  envOrDefault("FORGEFLOW_REVIEWER_REASONING_EFFORT", "medium"),
		ReviewerMaxOutputTokens:  reviewerMaxOutput, ReviewerTimeout: reviewerTimeout, ReviewerContextMaxBytes: reviewerContextBytes,
		SecurityModel:           envOrDefault("FORGEFLOW_SECURITY_MODEL", "gpt-5.6"),
		SecurityPromptVersion:   envOrDefault("FORGEFLOW_SECURITY_PROMPT_VERSION", "security/v1"),
		SecurityReasoningEffort: envOrDefault("FORGEFLOW_SECURITY_REASONING_EFFORT", "medium"),
		SecurityMaxOutputTokens: securityMaxOutput, SecurityTimeout: securityTimeout, SecurityContextMaxBytes: securityContextBytes,
		PostgresEnabled: postgresEnabled, PostgresDSN: strings.TrimSpace(postgresDSN),
		PostgresMaxOpenConns: postgresMaxOpen, PostgresMaxIdleConns: postgresMaxIdle,
		PostgresConnMaxLifetime: postgresLifetime, PostgresPingTimeout: postgresPingTimeout,
		ArtifactRoot: envOrDefault("FORGEFLOW_ARTIFACT_ROOT", filepath.Join(dataDirectory, "artifacts")), ArtifactMaxBytes: artifactMaxBytes,
		WorkerLeaseTTL: workerLeaseTTL, WorkerHeartbeatInterval: workerHeartbeat, WorkerPollInterval: workerPoll,
		WorkerMetricsAddress:  envOrDefault("FORGEFLOW_WORKER_METRICS_ADDRESS", "127.0.0.1:9091"),
		EnforceActiveReleases: governanceEnforceActiveReleases,
		DockerEnabled:         dockerEnabled, DockerBinary: envOrDefault("FORGEFLOW_DOCKER_BINARY", "docker"),
		SandboxWorkspaceRoot: envOrDefault("FORGEFLOW_SANDBOX_WORKSPACE_ROOT", filepath.Join(dataDirectory, "workspaces")),
		SandboxImage:         strings.TrimSpace(os.Getenv("FORGEFLOW_SANDBOX_IMAGE")),
		SandboxCPUs:          envOrDefault("FORGEFLOW_SANDBOX_CPUS", "1.0"), SandboxMemory: envOrDefault("FORGEFLOW_SANDBOX_MEMORY", "512m"),
		SandboxPIDsLimit: sandboxPIDs, SandboxTmpfsBytes: sandboxTmpfs, SandboxMaxOutputBytes: sandboxOutput, SandboxTimeout: sandboxTimeout,
	}
	if err := configuration.Validate(); err != nil {
		return Config{}, err
	}
	return configuration, nil
}

func (c Config) Validate() error {
	if !oneOf(c.Environment, "development", "test", "staging", "production") {
		return fmt.Errorf("FORGEFLOW_ENV must be development, test, staging, or production")
	}
	if !oneOf(c.LogLevel, "debug", "info", "warn", "error") {
		return fmt.Errorf("FORGEFLOW_LOG_LEVEL must be debug, info, warn, or error")
	}
	if c.OTELSampleRatio < 0 || c.OTELSampleRatio > 1 {
		return fmt.Errorf("FORGEFLOW_OTEL_SAMPLE_RATIO must be between 0 and 1")
	}
	if strings.TrimSpace(c.HTTPAddress) == "" {
		return fmt.Errorf("FORGEFLOW_HTTP_ADDRESS cannot be empty")
	}
	if c.Environment == "production" && !c.HTTPCookieSecure {
		return fmt.Errorf("FORGEFLOW_HTTP_COOKIE_SECURE must be true in production")
	}
	if c.Environment == "production" && len(c.HTTPAllowedOrigins) == 0 {
		return fmt.Errorf("FORGEFLOW_HTTP_ALLOWED_ORIGINS is required in production")
	}
	if len(c.RepositoryRoots) == 0 {
		return fmt.Errorf("FORGEFLOW_REPOSITORY_ROOTS must contain at least one path")
	}
	if c.SessionTTL < time.Minute || c.SessionTTL > 90*24*time.Hour || c.SessionIdleTTL < time.Minute || c.SessionIdleTTL > c.SessionTTL {
		return fmt.Errorf("session expiry configuration is invalid")
	}
	if (c.BootstrapAdminEmail == "") != (c.BootstrapAdminPassword == "") {
		return fmt.Errorf("bootstrap admin email and password must be configured together")
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return fmt.Errorf("FORGEFLOW_DATA_DIR cannot be empty")
	}
	if !oneOf(c.WorkflowMode, "planning", "development") {
		return fmt.Errorf("FORGEFLOW_WORKFLOW_MODE must be planning or development")
	}
	if c.WorkflowMode == "development" && (c.PlannerMode != "openai" || strings.TrimSpace(c.OpenAIAPIKey) == "" || !c.DockerEnabled) {
		return fmt.Errorf("development workflow requires FORGEFLOW_PLANNER_MODE=openai, OPENAI_API_KEY, and FORGEFLOW_DOCKER_ENABLED=true")
	}
	if !oneOf(c.PlannerMode, "mock", "openai") {
		return fmt.Errorf("FORGEFLOW_PLANNER_MODE must be mock or openai")
	}
	if !oneOf(c.ModelProvider, "openai", "deepseek") {
		return fmt.Errorf("FORGEFLOW_MODEL_PROVIDER must be openai or deepseek")
	}
	if strings.TrimSpace(c.OpenAIBaseURL) == "" {
		return fmt.Errorf("FORGEFLOW_OPENAI_BASE_URL cannot be empty")
	}
	if c.OpenAIMaxRetries < 0 || c.OpenAIMaxRetries > 5 {
		return fmt.Errorf("FORGEFLOW_OPENAI_MAX_RETRIES must be between 0 and 5")
	}
	if strings.TrimSpace(c.PlannerModel) == "" {
		return fmt.Errorf("FORGEFLOW_PLANNER_MODEL cannot be empty")
	}
	if strings.TrimSpace(c.PlannerPromptVersion) == "" {
		return fmt.Errorf("FORGEFLOW_PLANNER_PROMPT_VERSION cannot be empty")
	}
	if !oneOf(c.PlannerReasoningEffort, "none", "low", "medium", "high", "xhigh", "max") {
		return fmt.Errorf("FORGEFLOW_PLANNER_REASONING_EFFORT is invalid")
	}
	if c.PlannerMaxOutputTokens <= 0 || c.PlannerMaxOutputTokens > 128_000 {
		return fmt.Errorf("FORGEFLOW_PLANNER_MAX_OUTPUT_TOKENS must be between 1 and 128000")
	}
	if c.PlannerTimeout < time.Second || c.PlannerTimeout > 10*time.Minute {
		return fmt.Errorf("FORGEFLOW_PLANNER_TIMEOUT must be between 1s and 10m")
	}
	if c.PlannerInputUSDPerMTok < 0 || c.PlannerOutputUSDPerMTok < 0 {
		return fmt.Errorf("planner token prices cannot be negative")
	}
	if strings.TrimSpace(c.DeveloperModel) == "" || strings.TrimSpace(c.DeveloperPromptVersion) == "" {
		return fmt.Errorf("developer model and prompt version cannot be empty")
	}
	if !oneOf(c.DeveloperReasoningEffort, "none", "low", "medium", "high", "xhigh", "max") {
		return fmt.Errorf("FORGEFLOW_DEVELOPER_REASONING_EFFORT is invalid")
	}
	if c.DeveloperMaxOutputTokens <= 0 || c.DeveloperMaxOutputTokens > 128_000 {
		return fmt.Errorf("FORGEFLOW_DEVELOPER_MAX_OUTPUT_TOKENS must be between 1 and 128000")
	}
	if c.DeveloperTimeout < time.Second || c.DeveloperTimeout > 10*time.Minute {
		return fmt.Errorf("FORGEFLOW_DEVELOPER_TIMEOUT must be between 1s and 10m")
	}
	if c.DeveloperContextMaxBytes <= 0 || c.DeveloperContextMaxBytes > 1024*1024 {
		return fmt.Errorf("FORGEFLOW_DEVELOPER_CONTEXT_MAX_BYTES must be between 1 and 1048576")
	}
	if err := validateAssessmentAgent("REVIEWER", c.ReviewerModel, c.ReviewerPromptVersion, c.ReviewerReasoningEffort, c.ReviewerMaxOutputTokens, c.ReviewerTimeout, c.ReviewerContextMaxBytes); err != nil {
		return err
	}
	if err := validateAssessmentAgent("SECURITY", c.SecurityModel, c.SecurityPromptVersion, c.SecurityReasoningEffort, c.SecurityMaxOutputTokens, c.SecurityTimeout, c.SecurityContextMaxBytes); err != nil {
		return err
	}
	if c.PostgresEnabled && strings.TrimSpace(c.PostgresDSN) == "" {
		return fmt.Errorf("FORGEFLOW_POSTGRES_DSN is required when PostgreSQL is enabled")
	}
	if c.PostgresMaxOpenConns <= 0 || c.PostgresMaxIdleConns < 0 || c.PostgresMaxIdleConns > c.PostgresMaxOpenConns {
		return fmt.Errorf("PostgreSQL connection pool limits are invalid")
	}
	if c.PostgresConnMaxLifetime < time.Second || c.PostgresPingTimeout < time.Second || c.PostgresPingTimeout > time.Minute {
		return fmt.Errorf("PostgreSQL connection timing is invalid")
	}
	if strings.TrimSpace(c.ArtifactRoot) == "" || c.ArtifactMaxBytes <= 0 {
		return fmt.Errorf("artifact storage configuration is invalid")
	}
	if c.WorkerLeaseTTL < time.Second || c.WorkerLeaseTTL > time.Hour || c.WorkerHeartbeatInterval <= 0 || c.WorkerHeartbeatInterval >= c.WorkerLeaseTTL || c.WorkerPollInterval <= 0 {
		return fmt.Errorf("worker lease timing is invalid")
	}
	if strings.TrimSpace(c.WorkerMetricsAddress) == "" {
		return fmt.Errorf("FORGEFLOW_WORKER_METRICS_ADDRESS cannot be empty")
	}
	if filepath.Base(c.DockerBinary) != c.DockerBinary || strings.ContainsAny(c.DockerBinary, "\x00\r\n") {
		return fmt.Errorf("FORGEFLOW_DOCKER_BINARY must be a bare executable name")
	}
	if strings.TrimSpace(c.SandboxWorkspaceRoot) == "" {
		return fmt.Errorf("FORGEFLOW_SANDBOX_WORKSPACE_ROOT cannot be empty")
	}
	if c.DockerEnabled && !sandboxImagePattern.MatchString(c.SandboxImage) {
		return fmt.Errorf("FORGEFLOW_SANDBOX_IMAGE must be pinned by sha256 when Docker is enabled")
	}
	if c.SandboxPIDsLimit <= 0 || c.SandboxTmpfsBytes <= 0 || c.SandboxMaxOutputBytes <= 0 {
		return fmt.Errorf("sandbox resource limits must be positive")
	}
	if c.SandboxTimeout < time.Second || c.SandboxTimeout > 10*time.Minute {
		return fmt.Errorf("FORGEFLOW_SANDBOX_TIMEOUT must be between 1s and 10m")
	}
	return nil
}

func validateAssessmentAgent(role, model, prompt, reasoning string, maxOutput int, timeout time.Duration, contextBytes int) error {
	if strings.TrimSpace(model) == "" || strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("%s model and prompt version cannot be empty", strings.ToLower(role))
	}
	if !oneOf(reasoning, "none", "low", "medium", "high", "xhigh", "max") {
		return fmt.Errorf("FORGEFLOW_%s_REASONING_EFFORT is invalid", role)
	}
	if maxOutput <= 0 || maxOutput > 128_000 {
		return fmt.Errorf("FORGEFLOW_%s_MAX_OUTPUT_TOKENS must be between 1 and 128000", role)
	}
	if timeout < time.Second || timeout > 10*time.Minute {
		return fmt.Errorf("FORGEFLOW_%s_TIMEOUT must be between 1s and 10m", role)
	}
	if contextBytes <= 0 || contextBytes > 1024*1024 {
		return fmt.Errorf("FORGEFLOW_%s_CONTEXT_MAX_BYTES must be between 1 and 1048576", role)
	}
	return nil
}

var sandboxImagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._/:@-]*@sha256:[a-f0-9]{64}$`)

func envOrDefault(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		if normalized := strings.TrimSpace(value); normalized != "" {
			return normalized
		}
	}
	return fallback
}

func envOrFile(key string) (string, error) {
	direct, directSet := os.LookupEnv(key)
	filePath, fileSet := os.LookupEnv(key + "_FILE")
	directSet = directSet && direct != ""
	filePath = strings.TrimSpace(filePath)
	fileSet = fileSet && filePath != ""
	if directSet && fileSet {
		return "", fmt.Errorf("%s and %s_FILE cannot both be configured", key, key)
	}
	if directSet {
		return direct, nil
	}
	if !fileSet {
		return "", nil
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("read %s_FILE: %w", key, err)
	}
	if !info.Mode().IsRegular() || info.Size() > 64*1024 {
		return "", fmt.Errorf("%s_FILE must reference a regular file no larger than 65536 bytes", key)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read %s_FILE: %w", key, err)
	}
	value := strings.TrimRight(string(data), "\r\n")
	if value == "" {
		return "", fmt.Errorf("%s_FILE is empty", key)
	}
	return value, nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func envInt(key string, fallback int) (int, error) {
	value, exists := os.LookupEnv(key)
	if !exists || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func envFloat(key string, fallback float64) (float64, error) {
	value, exists := os.LookupEnv(key)
	if !exists || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number: %w", key, err)
	}
	return parsed, nil
}

func envBool(key string, fallback bool) (bool, error) {
	value, exists := os.LookupEnv(key)
	if !exists || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", key, err)
	}
	return parsed, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	value, exists := os.LookupEnv(key)
	if !exists || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", key, err)
	}
	return parsed, nil
}

func splitCSV(value string) []string {
	result := []string{}
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}
