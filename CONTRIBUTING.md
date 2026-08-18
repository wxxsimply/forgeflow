# Contributing to ForgeFlow

## Branch strategy

`main` is the protected, releasable branch. Create short-lived branches from the latest `main`:

- `feat/<issue>-<name>` for capabilities
- `fix/<issue>-<name>` for defects
- `docs/<issue>-<name>` for documentation
- `chore/<issue>-<name>` for maintenance

Open a pull request back to `main`. Do not commit generated Run data, credentials, local IDE settings, binaries, or artifacts. Prefer squash merging so one reviewed issue becomes one clear main-branch commit.

## Commit messages

Use a small conventional prefix: `feat:`, `fix:`, `test:`, `docs:`, `refactor:`, `build:`, or `chore:`. Explain why the change is needed; do not describe unverifiable success.

## Required checks

Windows:

```powershell
./scripts/verify.ps1
```

Linux/macOS with Make:

```bash
make verify
```

Every pull request needs measurable acceptance criteria, relevant tests, a security-impact statement, and an explicit out-of-scope section. Changes to Prompt, Policy, Model configuration, Graph behavior, authentication, sandboxing, or migrations require focused regression coverage.

Run `make race` on a supported Go platform when changing concurrency. The required Linux CI job always runs the race detector; some local Windows Go installations may not provide a working race runtime.

## Review policy

- The author must not approve their own security-sensitive change.
- Do not merge while required CI checks fail.
- Never weaken a deterministic gate merely to make an Agent or Eval pass.
- Prompt and model upgrades must use a new version and retain a rollback target.
