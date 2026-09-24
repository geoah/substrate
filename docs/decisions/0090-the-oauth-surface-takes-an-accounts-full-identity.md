---
status: accepted
date: 2026-09-16
decision-makers: George Antoniadis
amended-by: 0102
---

# 0090. The OAuth surface takes an account's full identity

## Context and Problem Statement

`POST /api/v1/oauth/start` took a BARE record id and searched it across every
kind implementing the `accountconfig` trait. A repository with two providers
whose accounts are both called `owner` — which is the obvious thing to call
them — got `conflict: id owner names an account record of more than one type …
address it by full identity` and no way to do that: the body had no field for
a kind and `kind/id` was not parsed. The only repair was renaming a record
(geoah/substrate#574, found by Mneme v6, whose runner now mints
`owner-<provider>` ids to work around it). The callback had the same search in
it, one step later.

## Considered Options

- Add a `kind` field beside `record` in the request body
- Let `record` carry the full identity `<kind>/<id>`, keeping the bare id for
  the single-hit case
- Keep the search and make the error honest ("rename one of them")

## Decision Outcome

Chosen: the second. `record` takes `<kind>/<id>` — the RECORD PATH, the same
spelling a reference value stores, a REST path carries and `substratectl`
prints — and resolves it exactly, with no reference to any other kind. A bare
id still resolves through the trait-wide search, because it is what a
single-provider repository types and what every existing client sends; two
kinds holding it is still a conflict, but the refusal now names both full
identities, and naming one is a repair the surface performs.

A second field was rejected because the wire would then have two ways to say
one thing, and the console's golden-file contract would grow an optional key
that is meaningless without the other. The path is one string, and it is the
spelling everything else already uses.

**The callback follows the FLOW ROW, not the id.** `start` already stored the
account's `(kind, id)` in `oauth_flows` beside the sealed PKCE verifier, so
the callback reads the identity the consent was begun for instead of searching
the state's bare id. The signed state is unchanged — it still carries the id,
and `CompleteOAuth` still answers with it — so no client and no stored state
moves; what changed is that an ambiguity can no longer reach the exchange. The
row is read, not consumed, at that point: a consent that fails before the
exchange (a disabled bundle, an unresolved client input) leaves the state
usable, exactly as it was.

### Consequences

- Good, because two providers may name their accounts the same thing, which is
  what a repository holding google, slack and github naturally does.
- Good, because the error names a repair the caller can perform, in the form
  the surface takes.
- Good, because the callback stops re-deriving something it already knows: the
  binding made at `start` is the one the exchange uses.
- Bad, because the parameter now has two shapes, and a record id carrying a
  dot in its first segment before a slash could in principle read as a kind
  reference. It cannot today: an id is one path segment
  ([0033](0033-the-path-grammar-has-no-separators.md)) and holds no `/`, so
  the two forms do not overlap.
- Bad, because a bare id is still ambiguous in a repository that has two, so
  the failure did not go away — it became answerable.

### Confirmation

`TestOAuthStartTakesTheAccountsFullIdentity` (`internal/engine`): two account
kinds each holding `owner`, the bare id refused with both identities in the
message, the full identity starting a consent, the callback landing on the
account it was started for and leaving the other untouched, and a full
identity naming no row answering not-found rather than searching.

## More Information

Issue [#574](https://github.com/geoah/substrate/issues/574); the ask is K in
geoah/mneme-v6 `docs/upstream.md`. `substratectl bundle connect` takes either
form because it passes its argument through.
