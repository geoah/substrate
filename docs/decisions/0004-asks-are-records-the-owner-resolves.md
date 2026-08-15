---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0004. Asks are batch records only the owner resolves

## Context and Problem Statement

Agents need to ask the user things mid-thread (yes/no, pick-one, pick-many,
several at once), and the answer has to reach the model, the console and
every GraphQL consumer alike. The interactions plan
([docs/plans/thread-interactions.md](../plans/thread-interactions.md))
surveyed how harnesses and protocols shape this; the substrate's own
constraint is that the changelog is the truth and an inbox must be a query.

## Considered Options

- One `llminteraction` record per BATCH, resolved by one CAS'd transition
- One record per question, answers as separate records
- In-flight prompts (MCP-elicitation style), nothing persisted

## Decision Outcome

Chosen: one record per batch, and the resolving transition is the OWNER's
alone. The batch (at most 8 questions, each with at most 32 options,
yes/no options MATERIALIZED into the stored row) is frozen at creation;
`answers` ride only the patch that performs the answering transition and
validate against stored option values; a bundle-tier actor never resolves an
interaction, whatever its emit says; the `thread` reference is stamped by
the loop's `ask` dispatch and generic creates cannot exist at all. A chat
message never answers an ask: authorization stays a record, not an
inference. Per-batch beat per-question because a single respondent answers
a batch as a unit and the thread resumes once; persistence beat in-flight
prompts because resolutions arrive hours later and must survive restarts.

No free text and no partial answers in v1; `dismissed` is the honest
decline, and both extensions are additive if usage demands them.

### Consequences

- Good, because "what did the user authorize" is always a record with a
  frozen question and a validated answer.
- Good, because the resolution rides decision 0003's primitive: no second
  resume machinery.
- Bad, because agents cannot collect prose through an ask; prose belongs in
  chat, which costs a turn.
- Bad, because the owner-only rule and the loop-only thread stamp are
  hardcoded engine guards, exactly the kind of per-kind code the marker was
  supposed to reduce; asks earned theirs the way requests did.

### Confirmation

internal/engine/agentinteraction_db_test.go: the ask→answer→resume loop,
dismissal, the admission gauntlet (bad batches land nothing), and
`TestOnlyTheOwnerResolvesAnInteraction` including a mutate-armed foil.

## More Information

Landed in PR #80 (kind `llminteraction`, built-in `ask`, core v7). The
naming follows the review of PR #73: generic on purpose, beside
`llmthread`/`llmmessage`.
