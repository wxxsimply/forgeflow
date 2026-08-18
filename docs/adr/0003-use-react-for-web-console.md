# ADR 0003: Use React and TypeScript for the web console

- Status: Accepted for the web phase
- Date: 2026-08-08

## Context

The web console needs authenticated routing, live Run events, graph and timeline visualization, Diff review, approval forms, and evaluation dashboards. The backend remains Go, but browser code still needs a mature UI ecosystem.

## Decision

Build the browser application with React, TypeScript, and Vite. Generate API types or a client from the Go service's OpenAPI contract. Keep authentication and authorization decisions on the server.

## Consequences

- Rich interactive views can use established browser tooling.
- Go remains the only backend and Worker language.
- The repository gains a Node-based frontend toolchain during the web phase.
- Shared truth must come from OpenAPI/JSON Schema rather than manually duplicated enums.

## Alternatives considered

- Go templates with minimal JavaScript: viable for a small admin UI, but less suitable for live graph, Diff, and Trace interactions.
- Full-stack TypeScript: rejected because backend and Worker are deliberately Go.
