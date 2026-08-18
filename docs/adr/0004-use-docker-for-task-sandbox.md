# ADR 0004: Use Docker as the first task sandbox

- Status: Accepted for the repository execution phase
- Date: 2026-08-08

## Context

ForgeFlow executes untrusted repository tests and build commands. A Git worktree protects the original branch but does not isolate processes, network, resources, host files, or credentials.

## Decision

Run task commands in short-lived Docker containers on dedicated Worker hosts. Use a non-root user, no network by default, explicit workspace mounts, fixed image digests, and CPU, memory, PID, disk, and timeout limits. The public API process must never receive the Docker socket.

## Consequences

- Worktree and process isolation become separate, testable controls.
- Worker hosts require hardened Docker operations, image lifecycle management, cleanup, and monitoring.
- Mounting the host Docker socket into a public-facing service is prohibited.
- Stronger isolation such as microVMs can replace the adapter later if threat or tenancy requirements increase.

## Alternatives considered

- Host subprocesses only: rejected because they do not provide an adequate security boundary.
- Kubernetes immediately: deferred until workload and operational data justify it.
- MicroVMs immediately: deferred due to implementation cost, while preserving an adapter boundary for later migration.
