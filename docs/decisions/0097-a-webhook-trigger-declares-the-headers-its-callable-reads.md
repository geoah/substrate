---
status: accepted
date: 2026-09-22
decision-makers: George Antoniadis
---

# 0097. A webhook trigger declares the headers its callable reads

## Context and Problem Statement

[0064](0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md)
narrowed a webhook request to a closed allowlist of header names before it
entered the changelog, and
[0068](0068-an-accepted-webhook-is-a-pending-entry-in-the-delivery-ledger.md)
made that narrowed copy the bytes every fire runs, so the list decides what a
callable sees and not just what history keeps. The list lived in the engine
(`parkedHeaderNames`): the five headers that describe the body, plus the
names GitHub, Stripe, Slack, Linear and Standard Webhooks sign with, plus
`x-index-*`, `x-audio-size` and `x-pebble-mode` for the Pebble sample. Every
new sender meant editing that map and shipping a binary, and every
repository's callables received every other repository's providers' headers.
The Pebble work put a sample's private header names in the engine, which is
where the cost became obvious.

## Considered Options

- Keep the global allowlist and add each provider's names to it, as today.
- A prefix pattern per provider (`x-index-*`, `x-github-*`), matched at
  delivery.
- The trigger record declares the names: `source.webhook.headers`, validated
  at write time.

## Decision Outcome

Chosen: the per-trigger declaration, because the record that registers the
endpoint is the one thing that knows which headers its callable reads, and it
is data a user writes rather than a constant a release ships. `parseTrigger`
admits `source.webhook.headers` as at most 32 names matching
`^[A-Za-z0-9-]{1,64}$`, lowercases them, and refuses a duplicate. A fire
carries those names and the five that describe the body (`content-type`,
`content-length`, `content-encoding`, `user-agent`, `date`), and nothing
else; the same set narrows the parked copy, so a retry delivers what the
first fire did. The engine's list shrinks to the body's five
(`bodyHeaderNames`), and every provider name leaves it.

A prefix pattern was rejected for the reason 0064 gave for the closed list:
`x-index-*` also matches a header a sender calls `x-index-secret`, and a
pattern cannot be read to say what will arrive.

Declaring `authorization`, `proxy-authorization`, `cookie` or `set-cookie` is
refused at write time by name. The API layer already drops those before the
service sees them, so admitting the declaration would write a promise no fire
could keep.

### Consequences

- Bad, because it is a breaking change with no compatibility default: a
  webhook trigger written before this, with `webhook: {}` or
  `webhook: {key: …}`, now delivers the body-describing headers alone. A
  callable that read `x-github-event` reads nothing until its trigger
  declares the name. There is no deprecation window, because a window means
  running both rules and the point is that exactly one thing decides.
- Good, because what a callable receives is readable on the record that
  registers it, next to the key and the callable, instead of in a Go map in
  the engine.
- Good, because a sample or a provider bundle ships its provider's header
  names the way it ships its kinds, and adding a sender no longer edits the
  engine or waits for a release.
- Good, because a repository receives only the headers its own triggers
  named: a substrate that takes one Pebble webhook does not pass Stripe's
  signature headers into a function body or into its changelog.
- Bad, because a header the owner forgot to declare is a silent absence at
  the callable, and the symptom is a body reading an empty string rather
  than a refusal at the door.
- The door still never forwards the credential headers, whatever a record
  says, and the query string is still dropped whole (0064).

### Confirmation

`TestParkedHeadersKeepOnlyWhatAReplayNeeds` and
`TestWebhookHeaderDeclarationIsAdmittedAtParse` in
`internal/engine/delivery_internal_test.go` hold the kept set and the
validation. `TestWebhookDelivery` drives one request at two triggers, one
declaring `x-pebble-mode` and one declaring nothing, and asserts the
callable sees two different header sets.

## More Information

Amends 0064's paragraph on `parkedHeaderNames` and the allowlist 0068 records
the request through; neither is superseded, because the ledger shape, the
pending entry and the blob-referenced body are unchanged. Worth reopening if
a provider's header set becomes large enough that listing it per trigger is
copied prose, at which point the bundle, not the engine, is where a named set
would live.
