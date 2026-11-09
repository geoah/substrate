---
status: proposed
date: 2026-11-09
decision-makers: George Antoniadis
---

# 0252. What the owner or an agent does in a chat is a `chataction` the bridge carries out, behind a shipped gate

## Context and Problem Statement

[0125](0125-a-provider-write-is-a-callable-that-writes-no-record.md) made a
provider's write a callable that writes no record, and named its cost: the
policy door and `confirmation: always` act only on record effects, so an
agent granted such a callable sends as the owner unasked. It said to reopen
"when a write must be queued or approved before it is sent, which is where
the outbox kind fits". The chat bridge ([plan](../plans/chat-bridge.md), D4
and D7) needs both: an agent's send reviewed by default, and an action
carried out once by a program the server cannot call, since the bridge dials
out ([0250](0250-a-chat-bridge-is-an-app-whose-instances-trust-each-other-and-delivers-one-ordered-stream-per-instance.md)).

## Considered Options

- A callable per action, as 0125 has it.
- The same callables, each declaring `confirmation: always`.
- An outbox kind, `chataction`, which the owner or an agent writes and the
  bridge carries out, with a shipped policy gating agents' writes of it.

## Decision Outcome

Chosen: the outbox kind. This amends 0125 for the chat bridge; Beeper's
writes stay callables, and the chat bridge's mirror rows keep 0125's one
writer, the webhook function.

**One record per action.** Send, react, remove a reaction, edit, delete and
mark read are each a `chataction`. Its request properties are whoever writes
it; `allowNewChat` (a first message to a stranger) and `cancelRequestedAt`
are `writer: owner`, as are the account's settings the bridge obeys
(`syncPaused`, `downloadMedia`, the send limits, `unlinkRequestedAt`): only
the owner's unscoped hand writes them, never an app token, a function or an
agent. The outcome (`attempt`, `claimedVersion`, `result`, the error) is
`writer: connector`, written by the function from the bridge's reports.
`state` moves `queued` to `sending`, `failed`, `expired` or `cancelled`,
and `sending` to `done`, `failed`, `uncertain` or `expired`. The bridge
claims an action with a fresh attempt before anything leaves, carries out
the version it claimed, reports `uncertain` rather than resending when it
cannot tell whether the network took it, and a report for another attempt
changes nothing.

**A shipped gate.** The package ships the `recordpatchpolicy`
`chatbridge-agent-actions`, gating every agent write of a `chataction`
(put, patch and delete, directly or through a tool), so each lands as a
change request for the owner. The owner lets one agent do one thing unasked
with an allow naming the gate
([0108](0108-an-allow-outranks-a-gate-only-by-naming-it.md)). Six tools are
offered to agents, each writing one action under an id minted from the
call's idempotency key, and none declares `confirmation: always`, which
would hold the owner's own calls too and which no allow lifts.

A callable cannot be reviewed and needs the server to reach the bridge.
`confirmation: always` holds the owner along with the agents, for good.

### Consequences

- Good, because every agent action waits for the owner by default, and the
  owner lifts that per agent and per tool.
- Good, because an action's whole life is a record: the console shows it,
  the owner cancels it, and a retried tool call names the same one.
- Good, because the bridge needs no inbound reach: it follows the change feed
  for queued actions and reports through its webhook.
- Bad, because an agent cannot hold a conversation unasked until the owner
  writes an allow for each thing it may do.
- Bad, because the gate is a record the owner may edit or delete, and an
  install leaves an edited one alone: a deleted gate ungates every agent.
- Bad, because a state machine takes no `writer:`, so nothing at the write
  stops the owner moving `state`; the console never does, and the function
  applies a report only for the attempt the row carries.
- Bad, because the bridge trusts the record, not who wrote it: an app's
  reads carry no provenance, so the rule that only the owner allows a new
  chat is the write door's alone.
- Bad, because an `uncertain` action needs the owner to decide, and a retry
  is a new action.

### Confirmation

`TestChatBridgeInstallAdmitsTheGateWithTheKinds` (the gate lands in the
install's apply and survives the owner's edit), `TestChatBridgeActionLife`,
`TestChatBridgeStaleResult`, `TestChatBridgeToolsWriteOneQueuedAction` and
`TestBodyHelpersChatBridge` in `internal/providertest`; the policy door's
own tests (`internal/engine/agentpolicy_db_test.go`) hold what a gate does
to an agent's tool call.

## More Information

The action protocol is the package's
[README](../../kinds/providers.substrate.reamde.dev/chatbridge/README.md),
"Actions". Reopen when an action must be held by something the owner cannot
edit, or when a network's actions need more than one record each.
