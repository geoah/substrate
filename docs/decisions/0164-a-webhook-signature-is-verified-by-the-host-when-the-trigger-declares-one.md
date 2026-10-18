---
status: accepted
date: 2026-10-18
decision-makers: George Antoniadis
---

# 0164. A webhook's signature is verified by the host when the trigger declares one

## Context and Problem Statement

[0045](0045-a-webhook-trigger-is-a-public-endpoint-with-an-optional-key.md)
put provider signature checks in the callable. The WHOOP provider showed what
that costs, in the 2026-10-11 review of the observations build (findings 4
and 5). WHOOP signs with the OAuth app's client secret, which the host never
hands a function, so the bundle shipped a `webhooksecret` setting asking the
owner for a second copy, and bundle settings are injected into every function
of the bundle, the sync included. And the door stored each body as a blob and
appended a pending delivery before the function could refuse it, so an
endpoint with no key kept every unsigned POST, up to the body cap each, parked.

## Considered Options

- Keep the check in the callable, and ship the trigger disabled until the
  owner sets a key.
- A host function a callable calls to verify a request against a named
  secret.
- The trigger declares the provider's scheme in `source.webhook.signature`,
  and the door verifies it before anything is stored.

## Decision Outcome

Chosen: the declaration, because it is the only option that refuses an
unsigned request before the door writes anything, and it keeps the signing
secret in the host. A key does neither for a trigger that ships in a
provider bundle: a key written in the bundle's public YAML is no secret, and
nothing mints one at install, so a keyed shipped trigger would ship
disabled. A host function runs after the delivery is stored, which is the
cost to remove.

The block, validated where `key` and `headers` are (`parseTrigger`):

- `algorithm`: `hmac-sha256`, the one the door computes.
- `signatureHeader`, and an optional `timestampHeader` carrying Unix time
  in decimal digits with no leading zero, with `timestampUnit` (`seconds`
  or `milliseconds`) required beside it. The unit is declared, never read
  off the value's size: under `{timestamp}{body}`, a 10-digit seconds
  header and a 13-digit milliseconds header can split the same signed
  bytes two ways, so a sender could move digits between the header and the
  body under one MAC. With the unit fixed and no leading zero, every
  timestamp inside `maxSkew` has one length and the message splits one way.
- `maxSkew`: an ISO duration from `PT1S` to `PT1H`, `PT5M` when absent,
  measured against the door's receipt time.
- `message`: a template over `{timestamp}` and `{body}`, the body
  byte-exact; `{body}` when absent. It names `{timestamp}` exactly when a
  `timestampHeader` is declared, because an unsigned timestamp proves
  nothing.
- `encoding`: `hex` or `base64`.
- `secret`: `substrate.reamde.dev/core/secret/<id>`, or `oauthClientSecret`
  for the client secret of the callable's bundle's OAuth client input,
  which the host resolves itself. `oauthClientSecret` on a callable whose
  bundle declares no `oauth2` block is refused at write time.

The door checks the key first, as before, then the signature, in constant
time, and only then writes the request to the repository's spool (decision
0167), whose drain stages its blobs and appends its entry. A missing or wrong signature, a
missing or stale timestamp, a multipart request (the door keeps no raw bytes
of one) or a secret that does not resolve is refused, and nothing is stored.
On a trigger with no key the refusal is the door's one `404`, so a POST
cannot tell that the repository has the trigger installed; for the same
reason the door answers a body it cannot read or bound there with that
`404` too (`CheckWebhook`'s `SignatureOnly`). On a keyed
trigger, where the request has already matched the key, it is `401` with the
body `webhook signature refused`, which tells the owner's sender that the
signature is what failed. An unresolvable secret is logged for the operator,
by a fixed reason and never the error its resolution met, at most once a
minute per trigger because any sender reaches that check; the other reasons
are not logged. A store that fails while the door reads the secret refuses
nothing: the door answers `503` with `Retry-After`, stores nothing, and
checks the sender's retry afresh.

The MAC covers the declared message alone: its literals, the timestamp
header's value as sent, and the body. A header the trigger keeps through
`headers` reaches the callable outside it.

The door remembers each signature it admitted under a declaration that signs
a timestamp, per trigger, until that timestamp leaves `maxSkew`, after which
the door would refuse the request as stale anyway. A repeat inside the window
is the same delivery: the door answers `202` with the first request's fire id
and stores nothing, or `503` with `Retry-After` while the first one is still
being stored. The record is the server process's memory, at most 4096
signatures per repository, the one closest to leaving its window forgotten
first; a restart forgets all of them. A declaration with no
`timestampHeader` has no window, so the door admits every repeat of a
correctly signed body as a new delivery.

A stored request is held to the signature its trigger declares when it runs,
not only when it arrived. The door writes `request.signatureVerified: true`
on a request it verified before it stores it, and only the door writes that
key: a sender's request becomes `method`, `contentType`, `headers`, `query`
and `body` or `parts`, never a key of the request map. The callable reads
the mark, which vouches for the body and the timestamp alone. The
dispatcher's resume of a pending delivery, a retry by hand and the rerun of
an interrupted agent delivery each check the stored request first. A marked
request runs, however long after `maxSkew`. An unmarked one, stored while the
trigger declared no signature, is checked from its kept body and headers,
its timestamp measured against when the door received it (`fire.at`, or for
an envelope whose `fire.at` names only the whole second, the receipt its
admission entry recorded), never against the clock of the run. One that does
not verify parks at once with an error starting `webhook signature refused
at delivery`, and the callable never runs it. A store fault during that
check is an ordinary transient failure and is retried.

A callable verifies only what the host cannot: a scheme outside the block,
such as a prefixed header (`sha256=…`) or a signature list (`t=…,v1=…`).
This supersedes 0045's paragraph on provider signatures in part, and the
rest of 0045 stands.

### Consequences

- Good, because a request the provider did not sign leaves no spool file, no
  blob, no ledger entry and no invocation, so an open endpoint with a declared signature
  ships enabled: it fails closed until its secret resolves.
- Good, because the OAuth client secret signs WHOOP's webhooks without a copy
  reaching any function.
- Bad, because a keyless trigger's signature refusal is the same `404` as a
  missing trigger. The server logs a secret that does not resolve and no
  other refusal, so a configured but wrong secret, a missing signature or a
  stale timestamp looks exactly like a wrong URL, in the answer and in the
  log. An owner whose provider reports `404` checks the URL and the
  provider's signature setup by hand. A keyed trigger answers `401`
  instead, which says no more than the key already proved.
- Bad, because the timestamp bounds admission only. A marked request parked
  for a month runs when a hand retries it, and a repeat inside the window
  after a restart, or after the record forgot it under load, is admitted as
  a new delivery. A callable that must run once per event deduplicates by
  the provider's event id, and one behind a body-only declaration always
  does.
- Bad, because GitHub's `sha256=` prefix and Stripe's `t=,v1=` header do not
  fit the block yet; each is a later additive field.
- Bad, because a `secret` record under a bundle's id prefix is still injected
  into that bundle's functions (0076), so a signing secret meant for the host
  alone lives outside every bundle's prefix.

### Confirmation

In `internal/engine`:

- the refusals and their two answers:
  `TestWebhookSignatureIsCheckedBeforeAnythingIsStored`;
- the secret's two sources and both callable kinds:
  `TestWebhookSignatureIsRefusedAtWriteTime`,
  `TestWebhookSignatureUnderTheOAuthClientSecret` and
  `TestAnAgentWebhookChecksItsBundlesClientSecret`;
- the store fault that refuses nothing:
  `TestWebhookSigningSecretStoreFaultIsNotARefusal`;
- the paced log of an unresolvable secret:
  `TestWebhookUnresolvableSecretIsLoggedOncePerMinute`;
- the replay record: `TestWebhookSignatureRepeatIsOneDelivery` and
  `TestWebhookReplaysHoldASignatureForItsWindow`;
- stored requests:
  `TestWebhookSignatureDeclaredLaterRefusesAStoredUnsignedRequest`,
  `TestWebhookSignatureStoredRequestRunsPastItsSkew`,
  `TestWebhookSignatureStoredRequestSkewIsMeasuredToTheMillisecond`,
  `TestWebhookSignatureMarkSurvivesAParkAndARetry` and
  `TestStoredBodyOnlySignatureNeedsNoReceipt`;
- the parse and the MAC: `TestWebhookSignatureDeclarationIsAdmittedAtParse`,
  `TestWebhookSignatureMatchesKnownVectors` (RFC 4231) and
  `TestWebhookSignaturePresentedAtItsEdges`;
- the declared timestamp unit: `TestWebhookTimestampReadsTheDeclaredUnit`
  and `TestWebhookSignatureMessageSplitsOneWay`.

In `internal/api`, `TestWebhookSignatureRefusalIsATerse401` holds the `401`
body and `TestWebhookKeylessSignedTriggerHidesTransportRefusals` the `404`
a keyless signed trigger gives a request it cannot read. WHOOP is the first
provider to ship a signature declaration, and its webhooks land with
`TestWhoopWebhookThroughTheDoor` (internal/providertest), which runs the
shipped declaration through the door.

## More Information

Amends [0045](0045-a-webhook-trigger-is-a-public-endpoint-with-an-optional-key.md),
which named host-side verification as an open additive field. Worth reopening
if a second algorithm or a structured signature header arrives with a
provider that needs it.
