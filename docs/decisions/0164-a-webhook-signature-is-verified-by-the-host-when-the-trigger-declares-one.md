---
status: proposed
date: 2026-10-11
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
endpoint with no key kept every unsigned POST, up to 8 MiB each, parked.

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
- `signatureHeader`, and an optional `timestampHeader` carrying Unix time in
  seconds, or milliseconds from 10^11 up.
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

`admitWebhook` checks the key first, as before, then the signature, in
constant time, then spools and appends. A missing or wrong signature, a
missing or stale timestamp, a multipart request (the door keeps no raw bytes
of one) or a secret that does not resolve is refused, and nothing is stored.
On a trigger with no key the refusal is the door's one `404`, so a POST
cannot tell that the repository has the trigger installed. On a keyed
trigger, where the request has already matched the key, it is `401` with the
body `webhook signature refused`, which tells the owner's sender that the
signature is what failed. An unresolvable secret is logged for the operator,
at most once a minute per trigger because any sender reaches that check; the
other reasons are not logged.

A callable verifies only what the host cannot: a scheme outside the block,
such as a prefixed header (`sha256=…`) or a signature list (`t=…,v1=…`).
This supersedes 0045's paragraph on provider signatures in part, and the
rest of 0045 stands.

### Consequences

- Good, because a request the provider did not sign leaves no blob, no ledger
  entry and no invocation, so an open endpoint with a declared signature
  ships enabled: it fails closed until its secret resolves.
- Good, because the OAuth client secret signs WHOOP's webhooks without a copy
  reaching any function.
- Bad, because a keyless trigger's signature refusal is the same `404` as a
  missing trigger, so an owner whose provider reports `404` has to read the
  server log, or the provider's signature setup, to tell a wrong URL from a
  wrong secret. A keyed trigger answers `401` instead, which says no more
  than the key already proved.
- Bad, because GitHub's `sha256=` prefix and Stripe's `t=,v1=` header do not
  fit the block yet; each is a later additive field.
- Bad, because a `secret` record under a bundle's id prefix is still injected
  into that bundle's functions (0076), so a signing secret meant for the host
  alone lives outside every bundle's prefix.

### Confirmation

`TestWebhookSignatureIsCheckedBeforeAnythingIsStored`,
`TestWebhookSignatureIsRefusedAtWriteTime` and
`TestWebhookSignatureUnderTheOAuthClientSecret` in `internal/engine`;
`TestWebhookSignatureDeclarationIsAdmittedAtParse` for the parse;
`TestWebhookSignatureRefusalIsATerse401` in `internal/api`; and
`TestWhoopWebhookThroughTheDoor` in `internal/providertest`.

## More Information

Amends [0045](0045-a-webhook-trigger-is-a-public-endpoint-with-an-optional-key.md),
which named host-side verification as an open additive field. Worth reopening
if a second algorithm or a structured signature header arrives with a
provider that needs it.
