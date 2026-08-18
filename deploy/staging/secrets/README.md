# Staging secrets

Create these files directly on the staging host. Never commit their contents:

- `postgres_password`: random PostgreSQL password.
- `postgres_dsn`: `postgres://forgeflow:<url-escaped-password>@postgres:5432/forgeflow?sslmode=disable` (TLS is terminated at the private-container boundary; PostgreSQL is not published).
- `alert_webhook_url`: HTTPS incident-management webhook.
- `openai_api_key`: only when `compose.openai.yaml` is enabled.
- `bootstrap_admin_password`: only during the one-time bootstrap override; remove it immediately afterward.

On Linux set mode `0600` and restrict the directory to the deployment account. Docker Compose grants each secret only to services that explicitly declare it.
