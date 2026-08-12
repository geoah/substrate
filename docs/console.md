# Web console

The console is the substrate in a browser: a single-page app served by the
substrate itself at `/`, talking to the same public surface as every other
client. It reads the whole repository and writes through the same public verbs,
so nothing it does needs an endpoint no other client has.

Signing in is the same exchange [substratectl](substratectl.md) makes: username, password,
and the current 6-digit code ([users and tokens](auth.md)). The console then
holds a token exactly like a script does — a session _is_ that token record,
which is why logging out revokes it. A substrate that is open for registration
also serves a registration page at `/register`, with the invite code as its
first field.

Six destinations — Overview, Changelog, Registry, Triggers, Agents and Merge
requests — with the account behind the session menu.

## Overview and data

The home page is an **Overview**: recent activity, anything waiting on you, and
a count per kind that doubles as the way in. Following one opens that kind's
collection at `/data/{authority}/{plural}`.

A kind opens on two tabs — its **Records**, a filterable and pageable
collection, and its **Definition**, the declaration rendered as the manifest it
is. From the records tab you can create one: the form is composed from the
declaration, and it goes out as the ordinary `put`.

A record opens on four tabs:

- **Manifest**: the [envelope](data-model.md#the-envelope), with every kind
  reference and every edge target rendered as a link you can follow.
- **Activity**: this record's own slice of [the changelog](changelog.md), with the
  actor on every row and each trigger's delivery state beside it.
- **Incoming**: the edges pointing *at* this record, paged separately as the
  API pages them.
- **Provenance**: which actor wrote each property, and at which
  [tier](terms.md#truth-and-derivation).

A merged-away record says so and points at its canonical winner; a tombstoned
one says so too. An authority also has a page of its own, listing every record
of every kind it declares in one table.

## Changelog

[The changelog](changelog.md), newest first, one row per committed change, expanded
in place to its payload. Filters cover kind, actor, op, and free text, and the
same view tails live. It is the audit trail and the debugging surface in one,
because there is only one changelog.

## Merge requests

The queue of proposed [merges](projection.md#merge-requests): each with the
matcher's evidence, a field-by-field comparison of the two records, and accept
or reject. Accepting is an ordinary state transition, and performing the merge
is what that transition does.

## Bundles

The [catalog](bundles-catalog.md): every bundle the binary ships,
available and installed together, with an Integration badge on the ones that
connect a provider and a quarantine badge on one that needs re-installing.
Installing shows what the closure added. An installed bundle carries its
lifecycle verbs — disable, enable, uninstall, and the purge that a refused
uninstall points you at — and its connections: one row per configured provider
account, where the [OAuth consent flow](bundles.md#the-oauth-facility)
starts and where a connection's token status is visible.

## Triggers and agents

**Triggers** lists every [trigger](functions.md#triggers) in the repository with
its delivery state: where its cursor stands, what it last ran, and what parked
if anything did. **Agents** lists the declared [agents](agents.md) and opens a
chat against one.

## Account

Behind the session menu, beside logging out:

- **Account** shows who you are, and holds the two credential changes: change
  your password, and replace your authenticator. Both ask for your current
  password and code in the form, because
  [the password-factor rule](auth.md#the-credential-and-the-password-factor-rule)
  refuses a bearer token here.
- **Tokens** lists every token in the repository — label, created, last used,
  expiry — and mints and revokes them. It is also the sessions page: every
  browser and every script that holds access is one of these rows, and
  revoking one is deleting it.

Next: [running a substrate](operations.md), the deployment underneath all of
this.
