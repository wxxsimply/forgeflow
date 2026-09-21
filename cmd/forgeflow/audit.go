package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"

	"forgeflow/internal/apperror"
	"forgeflow/internal/audit"
	"forgeflow/internal/config"
)

func runAudit(ctx context.Context, args []string, configuration config.Config) error {
	if len(args) == 0 || (args[0] != "verify" && args[0] != "query") {
		return apperror.New(apperror.CodeValidation, "audit requires exactly one command: verify or query")
	}
	set := flag.NewFlagSet("audit "+args[0], flag.ContinueOnError)
	maxEvents := set.Int("max-events", 10_000, "maximum events scanned")
	maxResults := set.Int("max-results", 200, "maximum matching events returned by query")
	action := set.String("action", "", "exact action filter")
	requestID := set.String("request-id", "", "exact request id filter")
	resourceID := set.String("resource-id", "", "exact resource id filter")
	if err := set.Parse(args[1:]); err != nil {
		return err
	}
	if *maxEvents <= 0 || *maxEvents > 1_000_000 || *maxResults <= 0 || *maxResults > 10_000 {
		return apperror.New(apperror.CodeValidation, "audit scan limits are invalid")
	}
	options, err := auditOptions(configuration)
	if err != nil {
		return err
	}
	store, err := audit.NewS3Store(ctx, options)
	if err != nil {
		return err
	}
	if err := store.Check(ctx); err != nil {
		return apperror.Wrap(err, apperror.CodeTransient, "cli.audit.check", "audit backend readiness check failed")
	}

	cursor := ""
	scanned := 0
	matches := []audit.Event{}
	digest := sha256.New()
	var first, last string
	for {
		page, err := store.Recover(ctx, cursor, 500)
		if err != nil {
			return apperror.Wrap(err, apperror.CodeInternal, "cli.audit.recover", "audit recovery verification failed")
		}
		for _, event := range page.Events {
			scanned++
			if scanned > *maxEvents {
				return apperror.New(apperror.CodeBudget, "audit scan exceeded --max-events")
			}
			_, _ = digest.Write([]byte(event.Integrity))
			if first == "" {
				first = event.OccurredAt.Format("2006-01-02T15:04:05.999999999Z07:00")
			}
			last = event.OccurredAt.Format("2006-01-02T15:04:05.999999999Z07:00")
			if args[0] == "query" && auditMatches(event, *action, *requestID, *resourceID) {
				if len(matches) >= *maxResults {
					return apperror.New(apperror.CodeBudget, "audit query exceeded --max-results; narrow the filters")
				}
				matches = append(matches, event)
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if args[0] == "query" {
		return printJSON(map[string]any{"items": matches, "scanned": scanned})
	}
	return printJSON(map[string]any{"status": "verified", "events": scanned, "firstOccurredAt": first, "lastOccurredAt": last, "integrityDigest": hex.EncodeToString(digest.Sum(nil))})
}

func auditOptions(configuration config.Config) (audit.S3Options, error) {
	if configuration.AuditBackend != "s3" {
		return audit.S3Options{}, apperror.New(apperror.CodeValidation, "FORGEFLOW_AUDIT_BACKEND=s3 is required for audit commands")
	}
	return audit.S3Options{
		Bucket: configuration.AuditS3Bucket, Region: configuration.AuditS3Region, Endpoint: configuration.AuditS3Endpoint,
		Prefix: configuration.AuditS3Prefix, KMSKeyID: configuration.AuditS3KMSKeyID, UsePathStyle: configuration.AuditS3UsePathStyle,
		Retention: configuration.AuditRetention, IntegrityKey: configuration.AuditIntegrityKey,
	}, nil
}

func auditMatches(event audit.Event, action, requestID, resourceID string) bool {
	return (action == "" || event.Action == action) && (requestID == "" || event.RequestID == requestID) && (resourceID == "" || event.ResourceID == resourceID)
}
