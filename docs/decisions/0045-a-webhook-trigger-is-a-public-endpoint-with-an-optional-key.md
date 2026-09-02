---
status: accepted
date: 2026-09-02
decision-makers: George Antoniadis
---

# 0045. A webhook trigger is a public endpoint with an optional key

## Context and Problem Statement

External services (GitHub, the Pebble Index 01 ring's phone app) push events
by HTTP POST to a URL they are given, with no substrate token and, for most of
them, no way to set a header. The `webhook` arm of
`core.substrate.reamde.dev/trigger` existed but was reachable only through the
authenticated wake, which carries no body. Something had to map an
unauthenticated POST to one repository and one trigger, carry the request into
the invocation, and take a multi-megabyte audio file without pushing it
through the runner protocol as base64.

## Considered Options

- The path names the repository owner and the trigger, and the trigger's own
  `source.webhook.key` is the credential when it declares one
  (`POST /webhooks/{owner}/{trigger}[/{key}]`, also `?key=` or a bearer).
- An opaque capability URL: an engine-minted 256-bit key, the only path
  segment, resolved by a cross-repository lookup on the records table.
- Host-side provider signature verification (GitHub's HMAC) configured on the
  arm, with the secret in the sealed store.
- A synchronous delivery whose function output becomes the HTTP response.

## Decision Outcome

Chosen: the readable path with an optional key, because the URL says what it
reaches, the engine resolves it directly (repository by username, trigger by
id, a constant-time key compare) with no cross-repository query and no minted
state, and a substrate on a private network can run its endpoints open, which
is what the first deployment wants. A key, when set, is the owner's choice
(16 to 128 URL-safe characters) and rides wherever the sender can put it:
providers that only know a URL use the path or `?key=`, clients that can set
headers keep the URL clean.

Provider signatures verify in the callable, not the host: the envelope's
`body` is byte-exact (`text` for UTF-8, `base64` otherwise), and a bundle
input already carries a sealed secret into a function's config. Host-side
verification stays open as an additive arm field.

Delivery is accept-and-dispatch: the door answers `202 {"fire": id}` once the
fire is handed to the background supervisor, because an agent callable may run
for minutes and every sender times out in seconds. A fire that has started is
durable through park-and-advance, and a parked public delivery keeps its
envelope in `trigger_failures.payload` so a retry re-delivers the same request.
The window between the `202` and the fire starting is lost on a crash; a
durable inbox is a later step.

Multipart file parts (a filename, or a media type outside `text/*`) are stored
in the repository's blob store under the `substrate.webhook` actor before the
fire, and the envelope carries the digest. Blob GC counts a digest named by a
parked payload as referenced. Bounds: 1 MiB for an inline body or the inline
parts together, 32 MiB for a multipart request, 16 KiB of headers, eight
deliveries in flight. There is no rate limiter: a miss costs one indexed read,
and a provider's burst (a push storm) must not be the first casualty.

### Consequences

- Every refusal (no such repository, trigger or key, disabled, another
  source) is one `404`, so the endpoint space cannot be probed.
- `webhook: {}` is an OPEN endpoint. A trigger written before this record
  under that spelling becomes reachable without a credential when the server
  upgrades; an owner who wants one sets `source.webhook.key`.
- `request` is a fifth envelope key beside `change`, `record`, `fire` and
  `repository`; the runner protocol moved to version 5 for it, and the Go SDK
  mirrors it as `Envelope.Request`.
- The trigger kind pins its own version (16) for the arm's new field.
- The authenticated wake still delivers a bare fire; it is the owner's verb,
  not a payload channel.
