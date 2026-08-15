---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0008. Keep the agent loop; do not adopt ADK for Go

## Context and Problem Statement

Building the interaction model raised the question of replacing
`internal/engine/agentloop.go` with Google's ADK for Go, which promises
runners, sessions, workflow agents, callbacks and an ecosystem. The full
capability matrix and fit assessment (adk-go v2.2.0, 2026-08) are in the
plan ([docs/plans/thread-interactions.md](../plans/thread-interactions.md)).

## Considered Options

- Keep the loop; steal the patterns (callback seams, compaction design)
- Migrate the loop onto adk-go
- Adopt adk-go for orchestration only, keeping the loop for turns

## Decision Outcome

Chosen: keep the loop. Everything hard in it is exactly what ADK has no
expression for — the transcript as records through the changelog fold under
row-level security, emit ceilings intersected per sub-agent hop, read and
turn and cost budgets, the per-thread lease, idempotency keys from stable
delivery identity — so a migration keeps all of that as adapters and deletes
nothing. The two features that would justify the dependency, context
compaction and provider maturity, are python-only today, and the GORM
session store is a second source of truth outside the changelog, which this
architecture cannot accept. Orchestration-only still drags the
Runner/Session model in, so it is the same decision wearing a smaller hat.

### Consequences

- Good, because the invariants that define the substrate stay in one loop
  this repo owns and tests.
- Good, because ADK's genuinely good ideas (before/after model and tool
  callback seams, adk-python's compaction shape) were taken as designs, at
  zero dependency cost.
- Bad, because the workflow/graph engine is real and expensive to
  hand-build; deterministic multi-agent pipelines will cost this repo real
  work when they matter (issue #75 carries it).
- Bad, because ecosystem gravity compounds monthly, and the re-evaluation
  cost grows with every feature the loop gains.

### Confirmation

None: this is held by review only. Issue #75 records the re-evaluation
triggers.

## More Information

Reopen when either trigger fires: deterministic pipelines become core while
hand-building the executor is measurably the expensive path, or adk-go gains
compaction plus first-party Anthropic support.
