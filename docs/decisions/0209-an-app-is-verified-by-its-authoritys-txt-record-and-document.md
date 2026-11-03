---
status: accepted
date: 2026-11-02
decision-makers: George Antoniadis
---

# 0209. An app is verified by its authority's TXT record and document

## Context and Problem Statement

The approval page, the actor `app:<host>` and "Managed by" name an app by the
host of its `client_id` URL
([0194](0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md),
[0195](0195-an-app-talks-to-agents-decides-their-requests-and-tails-the-feed-it-reads.md)).
Serving a file from a host proves nothing about who controls the name: a
document on `pastebin.com` makes an app `pastebin.com`'s, and any program on
a device can send a loopback app's `client_id` and be shown as that app.
This is decision 3, decision 4's verified rule and the attribution section of
the plan in [PR #1145](https://github.com/geoah/substrate/pull/1145)
(`docs/plans/authorities.md`). It amends 0194 and 0195 (the actor) and 0204
(full access for an unverified app).

## Considered Options

- The `client_id`'s host, as today.
- A file at the authority's `/.well-known/` path listing the `client_id`.
- A `_substrate.<authority>` TXT record naming an authority document that
  lists the `client_id`, claimed by the client document.
- The client ids in the TXT record itself.
- A signed software statement or client attestation.

## Decision Outcome

Chosen: the TXT record and the authority document, because only whoever
controls a name's DNS can publish the record, every file after it may be
hosted anywhere, and a publisher adds one record once rather than one per
app. A `/.well-known/` file proves control of a web server, not the name;
client ids in DNS fit three or four to a record; attestation needs a backend
nothing else here needs.

An app is verified when every step holds: its `client_id` is `https`; its
client document claims authority A with `substrate_authority`; A's one
`v=substrate1` record carries `doc=`; the document there, held to its digest
when the record pins `sha256=`, names A and lists the exact `client_id`; and
the `redirect_uri` is `https` on A or a name under A. A loopback `client_id`
or redirect is never verified. Anything else is unverified, with the failing
step shown to the owner, and may still ask for `full`, which the page marks
as handing an unconfirmed program a token equal to the password.

The actor follows the proof: `app:<authority>` when verified, `app:local`
for a loopback `client_id` or redirect, `app:<client id host>` otherwise.
The token records the verified authority and whether the access went to a
loopback address. A package's owner under
[0205](0205-an-app-owns-the-package-it-declares.md) stays its client id, and
the host-only claim compares the client id's host, never the actor.
An approval checks the publisher again before the code is issued, and the
token exchange checks it again before the token is minted. A publisher that
no longer verifies the way the page showed refuses both, so a token never
carries a publisher the owner did not approve. A token whose actor moved
keeps writing label and annotation keys under `app:<client id host>`, the
namespace it had before.

`SUBSTRATE_INSECURE_AUTHORITY_DOCS` reads named authorities' documents from
files in place of DNS and the fetch, to test a document before it is
published. Like the other `SUBSTRATE_INSECURE_` variables it is an
operator's opt-in with a boot warning, and nothing limits it to
development. So it shows wherever it acts: the approval page says, in its
everyday view, that a file on this server verified the publisher and not
DNS, and the token records `appAuthorityFromFile`.

### Consequences

- Good, because the page names a publisher only when the publisher's DNS
  says so, and a verified app's client document may live on a CDN.
- Good, because a loopback impersonator no longer writes under the real
  app's name.
- Bad, because the proof is only as good as the resolver the server uses;
  few zones are signed.
- Bad, because a publisher's apps share one actor, `app:<authority>`, and an
  app's actor changes when it becomes verified; the token still names the
  exact client.
- Bad, because every loopback app writes as one actor, `app:local`.

### Confirmation

`TestAnAppIsVerifiedOnlyByItsAuthoritysRecordAndDocument` in
`internal/authorize` holds the verification matrix;
`TestAVerifiedAppIsShownByItsAuthorityAndWritesAsIt` and its neighbours in
`internal/api` hold the page and the actor;
`TestAPackageOwnerIsTheClientIDWhateverActorItWritesAs` and
`TestAVerifiedAppKeepsItsPackagesAndItsAgent` hold ownership.

## More Information

A lapsed domain's apps become unverified at their next approval; telling a
new owner of the domain from the old one is
[#1144](https://github.com/geoah/substrate/issues/1144).
