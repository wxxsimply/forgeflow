package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

const dataGovernanceSchemaVersion = "forgeflow.data-governance/v1"

type DataGovernancePolicy struct {
	SchemaVersion    string                   `json:"schemaVersion"`
	PolicyVersion    string                   `json:"policyVersion"`
	ApprovedAt       time.Time                `json:"approvedAt"`
	ApprovalRecordID string                   `json:"approvalRecordId"`
	Residency        DataResidency            `json:"residency"`
	Providers        []ApprovedModelProvider  `json:"providers"`
	Subprocessors    []ApprovedSubprocessor   `json:"subprocessors"`
	UserNotice       DataGovernanceUserNotice `json:"userNotice"`
}

type DataResidency struct {
	PrimaryRegion  string   `json:"primaryRegion"`
	AllowedRegions []string `json:"allowedRegions"`
}

type ApprovedModelProvider struct {
	ID                string   `json:"id"`
	SubprocessorID    string   `json:"subprocessorId"`
	EndpointHosts     []string `json:"endpointHosts"`
	ProcessingRegions []string `json:"processingRegions"`
	DataCategories    []string `json:"dataCategories"`
}

type ApprovedSubprocessor struct {
	ID                string   `json:"id"`
	Purpose           string   `json:"purpose"`
	EndpointHosts     []string `json:"endpointHosts"`
	ProcessingRegions []string `json:"processingRegions"`
	DataCategories    []string `json:"dataCategories"`
}

type DataGovernanceUserNotice struct {
	PrivacyPolicyVersion  string    `json:"privacyPolicyVersion"`
	PrivacyPolicyURL      string    `json:"privacyPolicyUrl"`
	TermsOfServiceVersion string    `json:"termsOfServiceVersion"`
	TermsOfServiceURL     string    `json:"termsOfServiceUrl"`
	PublishedAt           time.Time `json:"publishedAt"`
	ExplicitAcceptance    bool      `json:"explicitAcceptance"`
}

var governanceIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{2,127}$`)
var governanceRegionPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)

func loadDataGovernancePolicy(environment string) (DataGovernancePolicy, string, error) {
	raw, directSet, err := dataGovernancePolicySource()
	if err != nil {
		return DataGovernancePolicy{}, "", err
	}
	if environment == "production" && directSet {
		return DataGovernancePolicy{}, "", fmt.Errorf("FORGEFLOW_DATA_GOVERNANCE_POLICY must be provided through FORGEFLOW_DATA_GOVERNANCE_POLICY_FILE in production")
	}
	if strings.TrimSpace(raw) == "" {
		if environment == "production" {
			return DataGovernancePolicy{}, "", fmt.Errorf("FORGEFLOW_DATA_GOVERNANCE_POLICY_FILE is required in production")
		}
		return DataGovernancePolicy{}, "", nil
	}
	var policy DataGovernancePolicy
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return DataGovernancePolicy{}, "", fmt.Errorf("FORGEFLOW_DATA_GOVERNANCE_POLICY must be valid JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return DataGovernancePolicy{}, "", fmt.Errorf("FORGEFLOW_DATA_GOVERNANCE_POLICY must contain exactly one JSON object")
	}
	if err := validateDataGovernancePolicy(policy); err != nil {
		return DataGovernancePolicy{}, "", err
	}
	digestBytes := sha256.Sum256([]byte(raw))
	digest := hex.EncodeToString(digestBytes[:])
	expected := strings.ToLower(strings.TrimSpace(os.Getenv("FORGEFLOW_DATA_GOVERNANCE_POLICY_SHA256")))
	if environment == "production" && expected == "" {
		return DataGovernancePolicy{}, "", fmt.Errorf("FORGEFLOW_DATA_GOVERNANCE_POLICY_SHA256 is required in production")
	}
	if expected != "" && (!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(expected) || expected != digest) {
		return DataGovernancePolicy{}, "", fmt.Errorf("FORGEFLOW_DATA_GOVERNANCE_POLICY_SHA256 does not match the policy file")
	}
	return policy, digest, nil
}

func dataGovernancePolicySource() (string, bool, error) {
	direct, directSet := os.LookupEnv("FORGEFLOW_DATA_GOVERNANCE_POLICY")
	filePath, fileSet := os.LookupEnv("FORGEFLOW_DATA_GOVERNANCE_POLICY_FILE")
	directSet = directSet && strings.TrimSpace(direct) != ""
	filePath = strings.TrimSpace(filePath)
	fileSet = fileSet && filePath != ""
	if directSet && fileSet {
		return "", false, fmt.Errorf("FORGEFLOW_DATA_GOVERNANCE_POLICY and FORGEFLOW_DATA_GOVERNANCE_POLICY_FILE cannot both be configured")
	}
	if directSet {
		return direct, true, nil
	}
	if !fileSet {
		return "", false, nil
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return "", false, fmt.Errorf("stat FORGEFLOW_DATA_GOVERNANCE_POLICY_FILE: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 64*1024 {
		return "", false, fmt.Errorf("FORGEFLOW_DATA_GOVERNANCE_POLICY_FILE must reference a regular file no larger than 65536 bytes")
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", false, fmt.Errorf("read FORGEFLOW_DATA_GOVERNANCE_POLICY_FILE: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", false, fmt.Errorf("FORGEFLOW_DATA_GOVERNANCE_POLICY_FILE is empty")
	}
	return string(data), false, nil
}

func validateDataGovernancePolicy(policy DataGovernancePolicy) error {
	if policy.SchemaVersion != dataGovernanceSchemaVersion || !governanceIdentifierPattern.MatchString(policy.PolicyVersion) || !governanceIdentifierPattern.MatchString(policy.ApprovalRecordID) || policy.ApprovedAt.IsZero() || policy.ApprovedAt.After(time.Now().UTC().Add(5*time.Minute)) {
		return fmt.Errorf("data governance policy identity is invalid")
	}
	if !governanceRegionPattern.MatchString(policy.Residency.PrimaryRegion) || len(policy.Residency.AllowedRegions) == 0 || !containsString(policy.Residency.AllowedRegions, policy.Residency.PrimaryRegion) || !uniqueRegions(policy.Residency.AllowedRegions) {
		return fmt.Errorf("data governance policy residency is invalid")
	}
	if len(policy.Providers) == 0 || len(policy.Subprocessors) == 0 {
		return fmt.Errorf("data governance policy must register providers and subprocessors")
	}
	subprocessors := map[string]ApprovedSubprocessor{}
	for _, value := range policy.Subprocessors {
		if !governanceIdentifierPattern.MatchString(value.ID) || strings.TrimSpace(value.Purpose) == "" || !uniqueIdentifiers(value.EndpointHosts, true) || !uniqueRegions(value.ProcessingRegions) || !uniqueIdentifiers(value.DataCategories, false) || !allRegionsAllowed(value.ProcessingRegions, policy.Residency.AllowedRegions) {
			return fmt.Errorf("data governance policy subprocessor is invalid")
		}
		if _, exists := subprocessors[value.ID]; exists {
			return fmt.Errorf("data governance policy contains duplicate subprocessor IDs")
		}
		subprocessors[value.ID] = value
	}
	providers := map[string]struct{}{}
	for _, value := range policy.Providers {
		if !governanceIdentifierPattern.MatchString(value.ID) || !governanceIdentifierPattern.MatchString(value.SubprocessorID) || !uniqueIdentifiers(value.EndpointHosts, true) || !uniqueRegions(value.ProcessingRegions) || !uniqueIdentifiers(value.DataCategories, false) || !allRegionsAllowed(value.ProcessingRegions, policy.Residency.AllowedRegions) {
			return fmt.Errorf("data governance policy provider is invalid")
		}
		if _, exists := providers[value.ID]; exists {
			return fmt.Errorf("data governance policy contains duplicate provider IDs")
		}
		subprocessor, exists := subprocessors[value.SubprocessorID]
		if !exists || !allRegionsAllowed(value.ProcessingRegions, subprocessor.ProcessingRegions) || !allStringsAllowed(value.DataCategories, subprocessor.DataCategories) {
			return fmt.Errorf("data governance provider is not covered by its subprocessor")
		}
		providers[value.ID] = struct{}{}
	}
	notice := policy.UserNotice
	if !governanceIdentifierPattern.MatchString(notice.PrivacyPolicyVersion) || !governanceIdentifierPattern.MatchString(notice.TermsOfServiceVersion) || notice.PublishedAt.IsZero() || notice.PublishedAt.After(time.Now().UTC().Add(5*time.Minute)) || !notice.ExplicitAcceptance || !httpsURL(notice.PrivacyPolicyURL) || !httpsURL(notice.TermsOfServiceURL) {
		return fmt.Errorf("data governance policy user notice is invalid")
	}
	return nil
}

func validateProductionDataGovernance(c Config) error {
	policy := c.DataGovernancePolicy
	if c.Environment != "production" {
		return nil
	}
	if c.DataGovernancePolicyDigest == "" {
		return fmt.Errorf("production data governance policy is required")
	}
	if c.AuditBackend != "s3" {
		return fmt.Errorf("production data governance requires FORGEFLOW_AUDIT_BACKEND=s3")
	}
	if strings.TrimSpace(c.OTLPEndpoint) == "" {
		return fmt.Errorf("production data governance requires FORGEFLOW_OTEL_ENDPOINT")
	}
	provider, ok := findProvider(policy.Providers, c.ModelProvider)
	if !ok || !containsString(provider.DataCategories, "model_inference") || !containsHost(provider.EndpointHosts, c.OpenAIBaseURL) {
		return fmt.Errorf("configured model provider or endpoint is not approved by the data governance policy")
	}
	if !httpsURL(c.OpenAIBaseURL) {
		return fmt.Errorf("production model provider endpoint must use HTTPS")
	}
	if err := validateSubprocessorUse(policy, c.DataGovernanceArtifactSubprocessorID, "artifact_storage", c.ArtifactS3Region, c.ArtifactS3Endpoint); err != nil {
		return fmt.Errorf("artifact storage data governance: %w", err)
	}
	if err := validateSubprocessorUse(policy, c.DataGovernanceAuditSubprocessorID, "audit_storage", c.AuditS3Region, c.AuditS3Endpoint); err != nil {
		return fmt.Errorf("audit storage data governance: %w", err)
	}
	if err := validateSubprocessorUse(policy, c.DataGovernanceTelemetrySubprocessorID, "observability", "", c.OTLPEndpoint); err != nil {
		return fmt.Errorf("telemetry data governance: %w", err)
	}
	return nil
}

func validateSubprocessorUse(policy DataGovernancePolicy, id, category, region, endpoint string) error {
	for _, value := range policy.Subprocessors {
		if value.ID != id {
			continue
		}
		if !containsString(value.DataCategories, category) || (region != "" && !containsString(value.ProcessingRegions, region)) {
			return fmt.Errorf("configured subprocessor is not approved for %s", category)
		}
		if endpoint != "" && !containsHost(value.EndpointHosts, endpoint) {
			return fmt.Errorf("configured endpoint is not approved for %s", category)
		}
		return nil
	}
	return fmt.Errorf("configured subprocessor is not registered for %s", category)
}

func findProvider(values []ApprovedModelProvider, id string) (ApprovedModelProvider, bool) {
	for _, value := range values {
		if value.ID == id {
			return value, true
		}
	}
	return ApprovedModelProvider{}, false
}

func uniqueRegions(values []string) bool {
	return len(values) > 0 && uniqueIdentifiers(values, false) && allStringsMatch(values, governanceRegionPattern)
}

func uniqueIdentifiers(values []string, hosts bool) bool {
	if len(values) == 0 {
		return false
	}
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if hosts {
			if !hostValue(value) {
				return false
			}
			value = strings.ToLower(value)
		} else if !governanceIdentifierPattern.MatchString(value) {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func allStringsMatch(values []string, pattern *regexp.Regexp) bool {
	for _, value := range values {
		if !pattern.MatchString(value) {
			return false
		}
	}
	return true
}

func allRegionsAllowed(values, allowed []string) bool { return allStringsAllowed(values, allowed) }

func allStringsAllowed(values, allowed []string) bool {
	for _, value := range values {
		if !containsString(allowed, value) {
			return false
		}
	}
	return true
}

func containsString(values []string, want string) bool { return slices.Contains(values, want) }

func containsHost(hosts []string, rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return false
	}
	return containsString(hosts, strings.ToLower(parsed.Hostname()))
}

func httpsURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != ""
}

func hostValue(value string) bool {
	parsed, err := url.Parse("https://" + value)
	return err == nil && parsed.Hostname() == value && parsed.Port() == ""
}
