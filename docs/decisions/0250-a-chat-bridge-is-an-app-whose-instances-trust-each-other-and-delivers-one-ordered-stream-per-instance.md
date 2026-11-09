---
status: proposed
date: 2026-11-09
decision-makers: George Antoniadis
---

# 0250. A chat bridge is an app whose instances trust each other, and delivers one ordered stream per instance with an applied watermark

## Context and Problem Statement

The chat bridge ([plan](../plans/chat-bridge.md), D2 and D5) is a program
outside the server that signs in to chat networks as the owner and must put
what happens there into the repository, in order and without loss across
downtime. It runs beside the server, at home or on a Mac, often behind NAT.
The webhook door answers `202` once a request is spooled and runs each fire
on its own (`internal/engine/webhookspool.go`, `fireDrained`), so neither
the order of two deliveries nor whether one applied can be read off the
door.

## Considered Options

- The server's functions poll a REST surface the bridge serves, as the
  Beeper provider polls Beeper Desktop.
- The bridge writes records directly through the records API with its
  token.
- The bridge delivers batches to a webhook function, each row ordered by a
  sequence number of its own.
- The bridge is an app that delivers one batch at a time to a webhook
  function, ordered by an applied watermark on its instance's row.

## Decision Outcome

Chosen: the last.

**The bridge is an app.** It gets its token through the authorization flow
([0194](0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)),
and the approval installs `providers.substrate.reamde.dev/chatbridge` and
keys and turns on its `chatbridge-events` webhook
([0202](0202-an-apps-approval-sets-up-what-it-needs-and-its-token-delivers-to-its-webhook.md)).
Because the package's function writes every kind of the package, the token
holds write scopes over all of them
([0215](0215-code-an-approval-installs-reaches-no-further-than-the-scopes-it-approves.md)):
`write:providers.substrate.reamde.dev/chatbridge/*` and the `instance` kind
by name, which no glob reaches. The bridge writes records only through the
webhook's function; that is the program's rule, not the token's limit.

**Its instances trust each other.** An instance id in a batch is the
bridge's claim, not something the server authenticates, so every instance
the owner approves for one repository reaches every account of the package.
An instance acts only on the accounts it runs by convention between
instances of the same owner.

**One ordered stream per instance.** The `instance` row carries
`appliedSeq`. The function skips every event at or below it, so a resent
batch writes nothing; fails the delivery when the batch's `first` is past
`appliedSeq + 1`, so the retry waits for the batch before it; applies the
rest in `seq` order; and moves `appliedSeq` to `last` in the same write,
guarded by the version it read, so two fires of one batch cannot both
apply. The bridge sends the next batch once the row shows the last one
applied, which the change feed tells it. An event the function cannot apply
is skipped and counted on the instance row, so one bad event does not stall
the stream; a malformed batch does, on purpose. A bridge that lost its store
is a new instance and starts its sequence again.

A REST surface the server polls costs the server reaching the bridge (a
private address in the sandbox's egress allowance, a tailnet for a bridge at
home) and gives no answer to an agent's request needing review. Direct
writes would split one network event across several requests with no order
between them. A sequence per row cannot order a partial event against a
whole one, and lets a deleted row come back.

### Consequences

- Good, because the bridge dials out: the server opens no port to it and
  keeps only its token's digest, so a bridge at home behind NAT works.
- Good, because order and application need no engine change: the watermark
  is an ordinary row, guarded by the version the function read.
- Good, because a resent or hand-retried batch is a no-op, and a fire that
  parks is on the trigger's parked list, which the token's `webhook:` scope
  reads.
- Bad, because one batch in flight bounds the stream to one batch per
  delivery round trip.
- Bad, because a batch that fails for its own content stalls its instance
  until the bridge or the package is fixed.
- Bad, because nothing the server checks keeps one instance off another's
  accounts, and the token could write any kind of the package directly.
- Bad, because the function's reads see committed rows and not its staged
  effects, so it reduces each batch's events in memory before writing.
- Bad, because deleting an instance row stalls its bridge: its next batch
  continues a sequence the server no longer holds.

### Confirmation

`TestChatBridgeResentAndReversedBatches` (a resent batch, a gap, an overlap
and two fires of one batch racing), `TestChatBridgeBadEventSkipped` and
`TestChatBridgeGoldenBatches` in `internal/providertest`;
`TestChatBridgeInstallAdmitsTheGateWithTheKinds` for the trigger shipped
off and keyless.

## More Information

The contract is the package's
[README](../../kinds/providers.substrate.reamde.dev/chatbridge/README.md).
Reopen when instances need isolating from each other (a scope per instance),
or when one batch in flight is too slow for a network's history.
