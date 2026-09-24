---
status: accepted
date: 2026-09-24
decision-makers: George Antoniadis
---

# 0102. The OAuth surface takes the full identity only

## Context and Problem Statement

[0090](0090-the-oauth-surface-takes-an-accounts-full-identity.md) made
`POST /api/v1/oauth/start` accept an account's record path,
`<authority>/<package>/<kind>/<id>`, and kept the bare id beside it:
`findAccountRef` searched the id across every kind implementing the
`accountconfig` trait and refused as a conflict when two held it. That is the
search [0101](0101-a-kind-trait-or-callable-is-named-in-full-on-every-surface.md)
removed from every other surface: a bare `owner` named one record in a
repository with one provider and was refused in a repository with two, so the
same client script meant different things in different repositories. The
console, `substratectl bundle connect` and the providers' end-to-end runner
all still sent the bare form (geoah/substrate#574).

## Considered Options

- Keep 0090 as it stands: the path resolves exactly, and the bare id is
  searched and refused only where two kinds hold it.
- Refuse a bare id everywhere, the refusal listing the record paths the
  repository holds under it.

## Decision Outcome

Chosen: refuse the bare id. `findAccountRef` takes a record path and nothing
else. A value that is not one is a validation refusal in the shape a bare kind
gets: `"owner" is a bare record id, and an account is named in full as
<authority>/<package>/<kind>/<id>; this repository holds
providers.substrate.reamde.dev/google/account/owner,
providers.substrate.reamde.dev/slack/account/owner`, the list in kind order and
absent when no live account carries the id. The signed state carries the
path, the callback holds the flow row's `(kind, id)` to it as a path, and
`CompleteOAuth` answers the path, so the return page posts and redirects with
it. The console builds the path from the account's kind and id,
`substratectl bundle connect` takes it as its one argument, and the runner
sends it.

Keeping the single-hit case lost because it is the shorthand 0101 refused on
every other door: a name that resolves by search means different things in
different repositories, and a client that typed the bare form against one
repository is refused by the next. One grammar on the wire, carried by every
client, is what a script can hold.

No stored state moves. `oauth_flows` has carried `record_kind` and
`record_id` since 0090, and a signed state lives `oauthflow.StateTTL`, fifteen
minutes: a consent begun under the previous binary carries the bare id, fails
the callback's compare, and is started again.

### Consequences

- Good, because the OAuth surface names an account the way every other
  surface names a record, and a repository that gains a second provider
  breaks no client of the first.
- Good, because the refusal hands back the exact string to paste.
- Good, because the ambiguity branch and its conflict message are gone: one
  form, one lookup.
- Bad, because every client that sent a bare id is refused until it sends the
  path.
- Bad, because a consent in flight across the deploy fails once and is
  started again.

### Confirmation

`TestOAuthStartTakesTheAccountsFullIdentity` and `TestOAuthRoundTrip`
(`internal/engine`): a bare id held by two kinds and one held by a single
account are both refused with `ErrValidation` and the paths listed, the path
starts a consent, and the callback answers the path.
`TestOAuthStartTakesTheRecordPathOnly` (`internal/api`) holds the door's
three answers, and `TestOAuthCallbackSuccessReturnPage` holds the return
page's path.

## More Information

Amends [0090](0090-the-oauth-surface-takes-an-accounts-full-identity.md):
the bare-id half of its outcome no longer holds; the path form, the flow-row
binding and the rest stand. Applies
[0101](0101-a-kind-trait-or-callable-is-named-in-full-on-every-surface.md) to
the one surface it left out. Issue
[#574](https://github.com/geoah/substrate/issues/574).
