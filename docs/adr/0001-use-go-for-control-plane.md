# ADR 0001: Use Go for the ForgeFlow control and execution plane

- Status: Accepted
- Date: 2026-08-08

## Context

ForgeFlow needs a CLI, HTTP API, durable Worker, graph runtime, subprocess control, and concurrent evaluation. The first specification named TypeScript, but the active implementation and project direction use Go.

## Decision

Implement the CLI, API, Worker, domain, graph, policy, tool, repository, sandbox, persistence, and evaluation services in one Go module until independent release boundaries are proven. Keep external systems behind small interfaces owned by their callers.

## Consequences

- One language and toolchain cover the control and execution plane.
- Go contexts, subprocess APIs and concurrency primitives fit Worker lifecycle needs.
- Browser UI still requires web technology and is governed by ADR 0003.
- The team must avoid premature package fragmentation and interface abstractions without real callers.

## Alternatives considered

- TypeScript for all backend services: rejected because it conflicts with the chosen implementation direction.
- Multiple Go modules from the start: rejected until API, Worker, and shared libraries need independent versioning.
