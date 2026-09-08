# Staging secrets

Create these files directly on the Staging host. Never commit their contents, copy them into an image, or paste them into chat, Issues, CI logs, or release evidence:

- `postgres_password`: random PostgreSQL password used by PostgreSQL only.
- `postgres_dsn`: `postgres://forgeflow:<url-escaped-password>@postgres:5432/forgeflow?sslmode=disable`; only Migration, API, and Worker receive it.
- `alert_webhook_url`: HTTPS incident-management webhook used by Alertmanager only.
- `openai_api_key`: only when `compose.openai.yaml` is enabled; only Worker receives it.
- `bootstrap_admin_password`: only during the one-time bootstrap override; it must not exist during normal operation.

On Linux, the deployment account must own this directory and every file must be a regular, non-symlink file with mode `0600`. Do not use `chmod 777`. Docker Compose grants each secret only to services that explicitly declare it.

Registry credentials are not Compose secrets. Log in interactively on the host with a dedicated read-only pull token, keep the generated Docker credential store outside this repository, and revoke/rotate the token after suspected exposure. Never place a registry token in `staging.env`, a Release manifest, or shell history.

Bootstrap lifecycle:

1. Create `bootstrap_admin_password` immediately before the first bootstrap deployment.
2. Deploy with `compose.bootstrap.yaml`, verify the administrator can log in, and log out.
3. Run `scripts/staging-bootstrap-cleanup.ps1 -ConfirmRemoval`; it deletes the exact non-symlink file, recreates API without the bootstrap overlay, and verifies API health metadata.
4. Confirm the file is absent and future deployments do not use `-IncludeBootstrap`.

Rotation order is always: create replacement → update only the consuming service → recreate and verify it → revoke/delete the old value. Evidence records names, timestamps, and results only, never secret values.
