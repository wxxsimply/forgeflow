# ForgeFlow Eval evidence and baselines

`internal/eval/datasets/software_v1.json` defines the immutable 30-case `software/v1` dataset contract. Version `2026-08-30` points to 30 real, unique commits generated in the dedicated local Fixture repository. The reviewed mapping is recorded in `evals/software-v1-fixtures.lock.json`.

Before collecting evidence, manually upload the Fixture and private Grader repositories, enforce their access controls, clone the Fixture repository into the execution environment, and prove that all selected commits exist:

```powershell
go run ./cmd/forgeflow eval --suite software/v1 --validate-only --fixture-repository D:\fixtures\forgeflow-eval-fixtures
```

Without `--fixture-repository`, `--validate-only` validates only schema, category counts, grader inputs, and budgets. Its output contains `"fixturesVerified": false`; it must never be cited as a completed 30-case run. Local Fixture verification is not a substitute for the pending manual GitHub access-control and clean remote-clone audit.

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
