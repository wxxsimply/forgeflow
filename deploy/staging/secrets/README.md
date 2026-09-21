# Staging secrets

Create these files directly on the Staging host. Never commit their contents, copy them into an image, or paste them into chat, Issues, CI logs, or release evidence:

- `postgres_password`: random PostgreSQL password used by PostgreSQL only.
- `postgres_dsn`: `postgres://forgeflow:<url-escaped-password>@postgres:5432/forgeflow?sslmode=disable`; only Migration, API, and Worker receive it.
- `alert_webhook_url`: HTTPS incident-management webhook used by Alertmanager only.
- `openai_api_key`: only when `compose.openai.yaml` is enabled; only Worker receives it.
- `bootstrap_admin_password`: only during the one-time bootstrap override; it must not exist during normal operation.
- `mfa_encryption_key`: standard Base64 encoding of exactly 32 random bytes; only API receives it. Keep the same value across API replicas and restarts. Losing or replacing it without re-enrollment locks every enrolled administrator.
- `audit_integrity_key`: standard Base64 encoding of exactly 32 random bytes; only API receives it. It signs external audit envelopes and must be recoverable separately from the audit Bucket.
- `otel_headers`: a JSON object of approved OTLP/HTTP authentication headers shared only with API and Worker. Use `{}` for the internal Collector. Never put trace tokens directly in the environment template.

On Linux, the deployment account must own this directory and every file must be a regular, non-symlink file with mode `0600`. Do not use `chmod 777`. Docker Compose grants each secret only to services that explicitly declare it.

Registry credentials are not Compose secrets. Log in interactively on the host with a dedicated read-only pull token, keep the generated Docker credential store outside this repository, and revoke/rotate the token after suspected exposure. Never place a registry token in `staging.env`, a Release manifest, or shell history.

Bootstrap lifecycle:

1. Create `bootstrap_admin_password` immediately before the first bootstrap deployment.
2. Deploy with `compose.bootstrap.yaml`, verify the administrator can log in, and log out.
3. Run `scripts/staging-bootstrap-cleanup.ps1 -ConfirmRemoval`; it deletes the exact non-symlink file, recreates API without the bootstrap overlay, and verifies API health metadata.
4. Confirm the file is absent and future deployments do not use `-IncludeBootstrap`.

Generate the MFA key directly on the target host with `openssl rand -base64 32 > mfa_encryption_key`, set mode `0600`, and never print it. The current engineering foundation does not provide online key re-encryption; key rotation therefore remains a Production acceptance item and must not be improvised with direct database edits.

Generate `audit_integrity_key` the same way. Before deployment, a Platform Owner must independently confirm that the audit Bucket has Object Lock enabled and that the API workload can only create/read/list objects under its prefix; it must not have delete, retention-shortening, KMS administration, or Bucket policy permissions.

Rotation order is always: create replacement → update only the consuming service → recreate and verify it → revoke/delete the old value. Evidence records names, timestamps, and results only, never secret values.
