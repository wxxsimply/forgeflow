package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadUsesDefaultsForEmptyValues(t *testing.T) {
	for _, key := range []string{
		"FORGEFLOW_ENV",
		"FORGEFLOW_LOG_LEVEL",
		"FORGEFLOW_DATA_DIR",
		"FORGEFLOW_WORKFLOW_MODE",
		"FORGEFLOW_PLANNER_MODE",
		"FORGEFLOW_OPENAI_MAX_RETRIES",
		"FORGEFLOW_PLANNER_MODEL",
		"FORGEFLOW_PLANNER_PROMPT_VERSION",
		"FORGEFLOW_PLANNER_REASONING_EFFORT",
		"FORGEFLOW_PLANNER_MAX_OUTPUT_TOKENS",
		"FORGEFLOW_PLANNER_TIMEOUT",
		"FORGEFLOW_PLANNER_INPUT_USD_PER_MTOK",
		"FORGEFLOW_PLANNER_OUTPUT_USD_PER_MTOK",
		"FORGEFLOW_DEVELOPER_MODEL",
		"FORGEFLOW_DEVELOPER_PROMPT_VERSION",
		"FORGEFLOW_DEVELOPER_REASONING_EFFORT",
		"FORGEFLOW_DEVELOPER_MAX_OUTPUT_TOKENS",
		"FORGEFLOW_DEVELOPER_TIMEOUT",
		"FORGEFLOW_DEVELOPER_CONTEXT_MAX_BYTES",
		"FORGEFLOW_REVIEWER_MODEL",
		"FORGEFLOW_REVIEWER_PROMPT_VERSION",
		"FORGEFLOW_REVIEWER_REASONING_EFFORT",
		"FORGEFLOW_REVIEWER_MAX_OUTPUT_TOKENS",
		"FORGEFLOW_REVIEWER_TIMEOUT",
		"FORGEFLOW_REVIEWER_CONTEXT_MAX_BYTES",
		"FORGEFLOW_SECURITY_MODEL",
		"FORGEFLOW_SECURITY_PROMPT_VERSION",
		"FORGEFLOW_SECURITY_REASONING_EFFORT",
		"FORGEFLOW_SECURITY_MAX_OUTPUT_TOKENS",
		"FORGEFLOW_SECURITY_TIMEOUT",
		"FORGEFLOW_SECURITY_CONTEXT_MAX_BYTES",
		"FORGEFLOW_POSTGRES_ENABLED",
		"FORGEFLOW_POSTGRES_DSN",
		"FORGEFLOW_POSTGRES_DSN_FILE",
		"FORGEFLOW_POSTGRES_MAX_OPEN_CONNS",
		"FORGEFLOW_POSTGRES_MAX_IDLE_CONNS",
		"FORGEFLOW_POSTGRES_CONN_MAX_LIFETIME",
		"FORGEFLOW_POSTGRES_PING_TIMEOUT",
		"FORGEFLOW_ARTIFACT_ROOT",
		"FORGEFLOW_ARTIFACT_MAX_BYTES",
		"FORGEFLOW_WORKER_LEASE_TTL",
		"FORGEFLOW_WORKER_HEARTBEAT_INTERVAL",
		"FORGEFLOW_WORKER_POLL_INTERVAL",
		"FORGEFLOW_WORKER_METRICS_ADDRESS",
		"FORGEFLOW_BOOTSTRAP_ADMIN_PASSWORD_FILE",
		"OPENAI_API_KEY_FILE",
		"FORGEFLOW_DOCKER_ENABLED",
		"FORGEFLOW_DOCKER_BINARY",
		"FORGEFLOW_SANDBOX_WORKSPACE_ROOT",
		"FORGEFLOW_SANDBOX_IMAGE",
		"FORGEFLOW_SANDBOX_CPUS",
		"FORGEFLOW_SANDBOX_MEMORY",
		"FORGEFLOW_SANDBOX_PIDS_LIMIT",
		"FORGEFLOW_SANDBOX_TMPFS_BYTES",
		"FORGEFLOW_SANDBOX_MAX_OUTPUT_BYTES",
		"FORGEFLOW_SANDBOX_TIMEOUT",
	} {
		t.Setenv(key, "")
	}

	configuration, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if configuration.Environment != "development" {
		t.Fatalf("Environment = %q", configuration.Environment)
	}
	if configuration.PlannerMode != "mock" {
		t.Fatalf("PlannerMode = %q", configuration.PlannerMode)
	}
	if configuration.WorkflowMode != "planning" {
		t.Fatalf("WorkflowMode = %q", configuration.WorkflowMode)
	}
	if configuration.DockerEnabled {
		t.Fatal("Docker must be disabled by default")
	}
	if configuration.DeveloperPromptVersion != "developer/v1" || configuration.DeveloperTimeout != 5*time.Minute {
		t.Fatalf("developer defaults = prompt %q timeout %s", configuration.DeveloperPromptVersion, configuration.DeveloperTimeout)
	}
	if configuration.ReviewerPromptVersion != "reviewer/v1" || configuration.ReviewerMaxOutputTokens != 3_000 || configuration.ReviewerTimeout != 2*time.Minute {
		t.Fatalf("reviewer defaults = %+v", configuration)
	}
	if configuration.SecurityPromptVersion != "security/v1" || configuration.SecurityContextMaxBytes != 512*1024 || configuration.SecurityTimeout != 2*time.Minute {
		t.Fatalf("security defaults = %+v", configuration)
	}
	if configuration.PostgresEnabled || configuration.PostgresMaxOpenConns != 20 || configuration.WorkerLeaseTTL != 30*time.Second {
		t.Fatalf("stage seven defaults = %+v", configuration)
	}
	if configuration.PlannerModel != "gpt-5.6" || configuration.PlannerPromptVersion != "planner/v1" || configuration.PlannerTimeout.String() != "2m0s" {
		t.Fatalf("planner defaults = model %q prompt %q timeout %s", configuration.PlannerModel, configuration.PlannerPromptVersion, configuration.PlannerTimeout)
	}
}

func TestLoadReadsSecretsFromFiles(t *testing.T) {
	directory := t.TempDir()
	dsnPath := filepath.Join(directory, "postgres_dsn")
	keyPath := filepath.Join(directory, "openai_key")
	passwordPath := filepath.Join(directory, "bootstrap_password")
	values := map[string]string{
		dsnPath:      "postgres://forgeflow:secret@db/forgeflow?sslmode=disable\n",
		keyPath:      "test-key\n",
		passwordPath: "a-long-bootstrap-password\n",
	}
	for path, value := range values {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("FORGEFLOW_POSTGRES_ENABLED", "true")
	t.Setenv("FORGEFLOW_POSTGRES_DSN_FILE", dsnPath)
	t.Setenv("OPENAI_API_KEY_FILE", keyPath)
	t.Setenv("FORGEFLOW_BOOTSTRAP_ADMIN_EMAIL", "admin@example.com")
	t.Setenv("FORGEFLOW_BOOTSTRAP_ADMIN_PASSWORD_FILE", passwordPath)
	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.PostgresDSN != "postgres://forgeflow:secret@db/forgeflow?sslmode=disable" || configuration.OpenAIAPIKey != "test-key" || configuration.BootstrapAdminPassword != "a-long-bootstrap-password" {
		t.Fatal("secrets were not loaded from files")
	}
}

func TestLoadRejectsAmbiguousOrInvalidSecretFiles(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "direct")
	t.Setenv("OPENAI_API_KEY_FILE", filepath.Join(t.TempDir(), "key"))
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted direct and file secret together")
	}
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a missing secret file")
	}
}

func TestLoadRequiresDSNWhenPostgresEnabled(t *testing.T) {
	t.Setenv("FORGEFLOW_POSTGRES_ENABLED", "true")
	t.Setenv("FORGEFLOW_POSTGRES_DSN", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted PostgreSQL without a DSN")
	}
}

func TestLoadRejectsInvalidAssessmentConfiguration(t *testing.T) {
	t.Setenv("FORGEFLOW_REVIEWER_TIMEOUT", "0s")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted an invalid reviewer timeout")
	}
	t.Setenv("FORGEFLOW_REVIEWER_TIMEOUT", "2m")
	t.Setenv("FORGEFLOW_SECURITY_CONTEXT_MAX_BYTES", "1048577")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted an oversized security context")
	}
}

func TestLoadRequiresPinnedSandboxImageWhenDockerEnabled(t *testing.T) {
	t.Setenv("FORGEFLOW_DOCKER_ENABLED", "true")
	t.Setenv("FORGEFLOW_SANDBOX_IMAGE", "forgeflow:latest")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted an unpinned sandbox image")
	}
}

func TestLoadParsesPlannerConfiguration(t *testing.T) {
	t.Setenv("FORGEFLOW_PLANNER_MAX_OUTPUT_TOKENS", "2048")
	t.Setenv("FORGEFLOW_PLANNER_TIMEOUT", "45s")
	t.Setenv("FORGEFLOW_PLANNER_INPUT_USD_PER_MTOK", "1.25")
	t.Setenv("FORGEFLOW_PLANNER_OUTPUT_USD_PER_MTOK", "8")
	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.PlannerMaxOutputTokens != 2048 || configuration.PlannerTimeout.String() != "45s" || configuration.PlannerInputUSDPerMTok != 1.25 || configuration.PlannerOutputUSDPerMTok != 8 {
		t.Fatalf("configuration = %+v", configuration)
	}
}

func TestLoadRejectsInvalidPlannerConfiguration(t *testing.T) {
	t.Setenv("FORGEFLOW_PLANNER_TIMEOUT", "forever")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted an invalid planner timeout")
	}
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	t.Setenv("FORGEFLOW_LOG_LEVEL", "verbose")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted an invalid log level")
	}
}
