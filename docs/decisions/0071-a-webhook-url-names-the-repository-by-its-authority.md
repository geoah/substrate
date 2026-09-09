---
status: accepted
date: 2026-09-09
decision-makers: George Antoniadis
---

# 0071. A webhook URL names the repository by its authority

## Context and Problem Statement

This record is written after the fact, from PR
[#345](https://github.com/geoah/substrate/pull/345) (`e513acee`, merged
2026-09-05) and the route in `internal/api/api.go`.
[0045](0045-a-webhook-trigger-is-a-public-endpoint-with-an-optional-key.md)
chose a readable webhook path and wrote it as
`POST /webhooks/{owner}/{trigger}[/{key}]`, where `owner` was the username,
so a login identifier sat in every URL a provider registers. #345 moved the
segment to the authority and left 0045's body frozen, saying
[0046](0046-a-repository-owns-one-authority-chosen-at-registration.md)
governed the path. 0046 is now superseded by
[0052](0052-the-authority-is-the-repository-id.md), which never mentions
webhooks, so nothing in the corpus says what the path is.

## Considered Options

- The path names the repository by its authority:
  `POST /webhooks/{authority}/{trigger}[/{key}]`.
- Keep the username in the path, as 0045 wrote it.

## Decision Outcome

Chosen: the authority. The route is
`POST /webhooks/{authority}/{trigger}` and
`POST /webhooks/{authority}/{trigger}/{key}` (`internal/api/api.go`), resolved
by `repositoryByAuthority`; `webhookPath` (`internal/engine/webhooks.go`)
prints the same URL into a trigger's status. The authority is the name a
repository publishes under and, since 0052, its id, so an outward surface
carries it; the username stays the login identifier and appears in no URL.

Everything else in 0045 stands: the readable path over a capability URL, the
trigger's own `source.webhook.key` as the optional credential (trailing
segment, `?key=` or a bearer header), signature verification in the callable,
and the one `404` for every refusal. A request that still names the username
gets that `404`, and so does an authority whose repository will not open, with
the reason logged, so a `500` cannot tell a prober which authorities exist.

### Consequences

- Good, because a provider's URL carries the repository's public name and not
  its login credential's other half.
- Good, because one lookup (`repositoryByAuthority`) resolves the segment; the
  authority is the repository id, so there is no second column to consult.
- Bad, because every webhook URL registered before #345 had to be re-pointed
  (`feat(api)!`).
- Bad, because the authority is permanent (0052), so a webhook URL is too: a
  repository cannot move its endpoints to another name.
- Neutral: the delivery envelope's `repository` carries `authority` beside
  `owner`, which stays, and the CLI context stores the authority `register`
  answered with so it can print the URL.

### Confirmation

`TestWebhookDelivery` (internal/engine/webhooks_db_test.go) delivers by
authority, refuses the username with the door's `404` under "every refusal is
not found", and asserts the status row's `webhookPath` under "status carries
the path". `TestWebhookRefusesARepositoryThatWillNotOpen` holds the second
`404`. `TestWebhookAcceptsAndHandsOverTheRequest` and
`TestWebhookKeyFromPathQueryOrHeader` (internal/api/webhooks_test.go) pin the
route shape and the three key positions.

## More Information

Amends 0045: only the path's first segment changes, from the username to the
authority; the rest of that record stands, as does the amendment
[0068](0068-an-accepted-webhook-is-a-pending-entry-in-the-delivery-ledger.md)
made to its last consequence. Reopen trigger: a repository that must change
its authority, which is 0052's reopen trigger too.
