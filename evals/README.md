# ForgeFlow Eval evidence and baselines

`internal/eval/datasets/software_v1.json` defines the immutable 30-case `software/v1` dataset contract. At present its `fixtureCommit` values are explicit placeholder SHAs; the dataset shape is usable for contract tests, but it is **not yet an executable baseline suite**.

Before collecting evidence, create a dedicated fixture repository, replace every placeholder with a real immutable commit, and prove that all selected commits exist:

```powershell
go run ./cmd/forgeflow eval --suite software/v1 --validate-only --fixture-repository D:\fixtures\forgeflow-eval
```

Without `--fixture-repository`, `--validate-only` validates only schema, category counts, grader inputs, and budgets. Its output contains `"fixturesVerified": false`; it must never be cited as a completed 30-case run.

Real executor observations should be stored outside source control while they may contain repository details, then passed to:

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
