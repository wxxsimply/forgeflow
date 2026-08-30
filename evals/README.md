# ForgeFlow Eval evidence and baselines

`internal/eval/datasets/software_v1.json` defines the immutable 30-case `software/v1` dataset contract. Version `2026-08-30` points to 30 real, unique commits generated in the dedicated local Fixture repository. The reviewed mapping is recorded in `evals/software-v1-fixtures.lock.json`.

The Fixture and private Grader repositories have completed their remote access-control audit. Before a paid run, use clean local clones and prove that every selected commit still exists:

```powershell
go run ./cmd/forgeflow eval --suite software/v1 --validate-only --fixture-repository D:\fixtures\forgeflow-eval-fixtures
```

Without `--fixture-repository`, `--validate-only` validates only schema, category counts, grader inputs, and budgets. Its output contains `"fixturesVerified": false`; it must never be cited as a completed 30-case run.

After manually committing the exact executor implementation, collect all three baselines with the same model, reasoning setting, prices, timeouts, fixture repository and execution host:

```powershell
$env:OPENAI_API_KEY="load from a secret manager"
go run ./cmd/forgeflow eval execute `
  --suite software/v1 `
  --fixture-repository D:\fixtures\forgeflow-eval-fixtures `
  --grader-repository D:\fixtures\forgeflow-eval-grader `
  --modes single_agent,planner_developer,forgeflow `
  --input-usd-per-million <current-real-input-price> `
  --cached-input-usd-per-million <current-real-cached-input-price> `
  --output-usd-per-million <current-real-output-price> `
  --output .forgeflow\evals\evidence.json
```

The command refuses an uncommitted main or Grader worktree. Each case starts from a fresh fixture worktree; model calls are stateless across modes; explicit tests use actual process exit codes; the trusted Grader runs from outside the Agent worktree. A terminal failure, refusal, timeout or approval request is still recorded. Evidence is atomically replaced after each case, so rerunning the same command resumes without repeating completed billable cases. `--limit 1` may be used for a paid smoke run; the resulting partial file is not a baseline report.

Real executor observations remain private under `.forgeflow/evals`, then are passed to:

```powershell
go run ./cmd/forgeflow eval --suite software/v1 --evidence .forgeflow/evals/evidence.json --format json --output .forgeflow/evals/comparison.json
go run ./cmd/forgeflow eval --suite software/v1 --evidence .forgeflow/evals/evidence.json --format markdown --output .forgeflow/evals/comparison.md
```

A comparison evidence file must contain exactly three runs: `single_agent`, `planner_developer`, and `forgeflow`. Each run records all 30 observations plus the exact model, Prompt, Policy, Tool, and Git versions. Missing cost or latency remains `null`/`N/A`; it is never synthesized.

Promotion compares the ForgeFlow row from a single report or comparison report. Passing automatic thresholds is necessary but not sufficient; `--approve` records the required human decision:

```powershell
go run ./cmd/forgeflow eval --promote-current .forgeflow/evals/current.json --promote-candidate .forgeflow/evals/candidate.json --approve
```

Do not commit evidence containing task bodies, repository paths, credentials, or proprietary source. Commit only reviewed, redacted release reports when a historical baseline is intentionally published.
