---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0007. The audit lives on the records it concerns, not in an audit kind

## Context and Problem Statement

Judged auto-decisions need an audit the owner can trust after the fact:
which policy text governed, what the judge said, with what confidence, and
what happened. Both adversarial reviews of the interactions plan
([docs/plans/thread-interactions.md](../plans/thread-interactions.md))
proposed a dedicated create-only `policyevaluation` kind; the PR #73 review
asked whether another kind was needed at all, and what would garbage-collect
it.

## Considered Options

- Verdicts as an engine-owned annotation on the request, leaning on the
  changelog and the judge's thread
- A dedicated create-only `policyevaluation` audit kind
- The judge's thread alone, nothing on the request

## Decision Outcome

Chosen: the annotation. Every judge invocation, failures included, writes
`policy/verdict` onto the request (policy id AND its changelog revision,
judge, its thread path, verdict, confidence, rationale, outcome, the request
version read). Three existing surfaces make that a complete audit: the
CHANGELOG already versions every annotation write immutably (a re-judged
request's history holds both verdicts), the judge's THREAD is the full run
record with its cost tallied, and a door-allowed write's changelog payload
names the policy that allowed it. A dedicated kind duplicated the changelog's
immutability with new machinery (a create-only guard, GC, a TTL design) and
bought nothing the records do not already say; the thread alone failed the
"answerable from the request" test the inbox needs.

### Consequences

- Good, because nothing is minted that would need garbage collection, and
  "why did this land without me" is one record read.
- Good, because the immutable history is the changelog, the thing already
  trusted for everything else.
- Bad, because the LIVE annotation shows only the latest verdict; earlier
  ones need a history read, which no console surface renders yet.
- Bad, because annotations are loosely shaped; a reader gets a map, not a
  declared kind.

### Confirmation

internal/engine/judge_db_test.go asserts the annotation's content on every
outcome, including failures; the changelog entry beside each audit write is
appended in the same transaction (internal/engine/judge.go).

## More Information

Landed in PR #83. This reverses the reviews' recommendation on the PR #73
thread, at the owner's direction there. Revisit if audit queries ("every
judged decision this month") become routine: that is the workload a
dedicated kind indexes better.
