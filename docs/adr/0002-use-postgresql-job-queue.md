# ADR 0002: Use PostgreSQL as the first durable job queue

- Status: Accepted for the server phase
- Date: 2026-08-08

## Context

The Worker needs durable jobs, leases, retries, heartbeat, recovery, and an atomic relationship between Run state, events, and scheduling. Introducing a separate broker too early creates another consistency boundary and operational dependency.

## Decision

Use PostgreSQL for Run state, events, checkpoints, an outbox, and the first Worker queue. Workers will lease jobs transactionally, using row locks such as `FOR UPDATE SKIP LOCKED`, a lease expiration, and an idempotent node-execution record.

## Consequences

- State transition and job publication can share one transaction.
- Development and backup operations have fewer moving parts.
- Queue depth and throughput remain bounded by the database; metrics must establish when this becomes a constraint.
- A dedicated broker may be introduced later behind `JobQueue` only after measured need.

## Alternatives considered

- Redis queue: deferred because it introduces dual-write consistency and another persistence system.
- RabbitMQ or Kafka: deferred because MVP throughput does not justify their operational cost.
- In-memory channels: rejected for server use because jobs would be lost on restart.
