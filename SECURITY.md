# Security Policy

Do not disclose suspected vulnerabilities, credentials, private repositories, or sensitive Run artifacts in a public issue.

After the repository is hosted on GitHub, use its Security tab to submit a private vulnerability report. Until private reporting is configured, contact the repository owner through a private channel and provide only the minimum sanitized reproduction data.

The project is pre-1.0. Security fixes are applied to the latest `main` branch; no older release line is currently maintained.

Deployment trust boundaries and known production blockers are documented in [docs/threat-model.md](docs/threat-model.md). Operators must follow [docs/security-review.md](docs/security-review.md) before every Staging or Production promotion. Never attach raw logs, task bodies, repository content, backup files, or Run artifacts to a vulnerability report; provide redacted hashes and the minimum reproduction instead.
