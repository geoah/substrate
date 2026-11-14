---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0315. A settled run says its cause from a closed set, and a refusal says its reason

## Context and Problem Statement

A run that failed kept `status: error` and a free-text `reason`, which was
the provider's answer. A stream that failed after its `200` ended with
`{"kind":"error","error":"<text>"}`, the same shape for a failed turn, a
reader that fell behind, a sign-in that ended and a fault of the server. A
provider's failure on the call route answered `422 validation`, which says
the caller's input was wrong. So clients searched the vendor's prose for
"rate limit" and "invalid api key" to choose a sentence, and could offer no
button. The plan is pull request #1363, section 3.

## Considered Options

- Keep `reason` as the one fact, and let each client classify it.
- Add sentences written by the server for the person to read.
- Add `cause`, one value from a closed set, beside `status` and `reason`.

## Decision Outcome

Chosen: `cause`, because the server knows which failure happened and the
client owns the words. Sentences from the server would be one language and
one tone for every client.

**The thread.** Every settle writes `cause` beside `status` and `reason`,
and `retryAfter`, whole seconds, where a provider named a wait. A run that
finishes clears them. The set (`substrate.AgentCauses`): for `error`,
`provider_key_refused`, `provider_rate_limited`, `provider_billing`,
`provider_overloaded`, `provider_unavailable`, `provider_rejected`,
`model_unavailable`, `context_too_long`, `allowance_spent`, `interrupted`,
`write_failed`, `internal`; for `overbudget`, `turn_limit`,
`tool_call_limit`, `deadline`; for `stopped`, `requested`, `shutdown`. The
`done` event's result and the `error` event of a failed turn carry the same
value. `cause` is on the thread, not on a message: a thread has one run at a
time, and a list reads threads.

**Where it is decided.** Each wire in `internal/llm` sorts its vendor's
answer in one function, by HTTP status and the error's `type` and `code` as
the vendor documents them, and a call no answer came back from by what the
connection said. The context overflow keeps its phrase list, which knows
more spellings than one vendor's code. Nothing above the wire reads prose.

**What the set leaves out, and adds.** `provider_key_missing` is not a
cause: a row with no key is refused before any thread opens, so nothing
could produce it. `provider_rejected` is added: a vendor's refusal of the
request as it stands can be told apart, and trying again does not help.
A stop is `requested`, whoever asked, since `reason` says which type of
actor.

**Refusals before the stream opens** keep their status and gain an envelope
`reason`: `thread_busy` and `turn_cap` on `conflict`, `spend_cap` on
`guard`, `provider_key_missing` on `validation`. The operator's allowance
and a refused key keep the reasons they had, `quota` and `key_refused`. A
provider's failure on the call route answers `503 unavailable` with the
cause as its reason and a `Retry-After`; `context_too_long` and
`provider_rejected` stay `422 validation`, with the reason, because the
same request meets the same answer.

**The four error events.** Only a failed turn's carries `cause`. A token
that no longer stands is `code: auth`; a reader that fell behind is
`code: unavailable`; a fault of the server is `code: internal`.

**What `reason` may hold.** The provider's answer with the key scrubbed, for
a row with the repository's own key. For a row the operator pays for, the
substrate's sentence for the cause, and the answer goes to the log: it
names the operator's endpoint, which is the deployment's.

### Consequences

- Good, because one switch in each client gives every failure a sentence
  and a button, and a new vendor phrasing changes one function in one wire.
- Good, because a caller of the call route can tell its own bad input from
  a provider that is down.
- Bad, because the set is closed: a new cause is a wire change, and an
  older client shows what the `status` alone says for it.
- Bad, because `cause` is a string property, not an enum. An enum value is
  one lowercase word and these are spelled like the API's reasons, so the
  kind does not hold the set; the engine and
  `TestAgentCausesMatchTheGolden` do.
- Bad, because a call route client that treated a provider failure as `422`
  now meets `503` and may retry it, as `Retry-After` tells it to.
- Bad, because an operator-paid failure's thread no longer holds the
  vendor's request id, which is in the server's log alone.

### Confirmation

`internal/llm/failure_test.go` (each vendor's documented error shapes
through both wires); `internal/engine/agentcause_db_test.go`;
`TestAFailedAgentCallAnswersByItsCause` and
`TestAStreamsErrorEventSaysWhatEnded` in `internal/api/agents_test.go`;
`TestAgentCausesMatchTheGolden` and the console's
`agentcauses.golden.test.ts`, which hold the console's union and its
sentences to the set.

## More Information

Revisit when a kind's enum values may carry an underscore: `cause` then
becomes an enum and the kind holds the set.
