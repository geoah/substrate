# Web console

The console is the substrate in a browser: a single-page app served by the
substrate itself at `/`, talking to the same public surface as every other
client. It reads the whole repository and writes through the same public verbs,
so nothing it does needs an endpoint no other client has.

Signing in is the same exchange [substratectl](substratectl.md) makes: username, password,
and the current 6-digit code ([users and tokens](auth.md)). The console then
holds a token exactly like a script does: a session is its
[token record](auth.md#tokens), which is why logging out revokes it. A
substrate that is open for registration
also serves a registration page at `/register`, with the invite code as its
first field.

Four destinations — Overview, Changelog, Registry and Agents — with the
account behind the session menu.

## Overview and data

The home page is an **Overview**: recent activity, anything waiting on you, and
a count per kind that doubles as the way in. Following one opens that kind's
collection at `/data/{authority}/{kind}`.

A kind opens on two tabs — its **Records**, a filterable and pageable
collection, and its **Definition**, the declaration rendered as the manifest it
is. From the records tab you can create one, and from a record you can edit it:
either way the editor is the same surface, and it goes out as the ordinary
`put`.

## The record editor

Creating and editing a record are two **lenses over one document**, and the
document is the apply-able [envelope](data-model.md#the-envelope).

- **Form**, the default, is composed from the declaration: one control per
  declared property, carrying its description and a worked example. An enum is
  a dropdown of what the kind admits, a `state` offers its machine's states, a
  `reference` picks a record of the kind it points at, a `secret` is write-only
  (a read serves `<redacted>`, and leaving the field blank keeps the sealed
  value), and a `json` property gets a JSON editor. Host-managed properties
  (a declared `writer:` that is not the owner) are never offered.
- **YAML** is the expert lens: the whole envelope in a code editor that knows
  the kind. **Completion** offers what may be written where the cursor is (the
  envelope's keys, the declared properties with their datatype and one-liner
  and never one already written, an enum's admitted values, a state machine's
  states, the kinds a reference may name), **diagnostics** underline a refused value on
  the line it sits on and mark it in the gutter, and **hovering** a property
  line shows what the kind says about it. There is a formatter, and the tint
  is the manifest view's own colours.

Both lenses edit the same text, so switching loses nothing and a hand-written
comment survives being edited on the form. Everything is checked against the
declaration **as you type** — the datatypes, required properties, unknown keys,
the shape of a reference value, and the two rules that belong to the write rather than
the value: a `put` may not move a state (that transition is a `patch`), and the
id in the document is not a rename. Problems key to their line, the gutter
marks them, and Save is barred while an error stands.

A record opens on five tabs:

- **Properties**: the declared properties rendered by type, the read view the
  editor opens from.
- **Manifest**: the [envelope](data-model.md#the-envelope), with every kind
  reference and every record reference rendered as a link you can follow.
- **Activity**: this record's own slice of [the changelog](changelog.md), with the
  actor on every row and each trigger's delivery state beside it.
- **Graph**: what this record points at and what points back, as a tree you
  drill into. Outgoing pointers read
  straight off the record's references; inbound ones are grouped and paged as the API pages
  them, each group headed by the name the **declaration** gives that side
  (`messages · llmmessage`, from `inverse:`) rather than the raw property name, which
  is the same link as the *other* record spells it. A member expands in
  place into its own graph, so a thread → its messages → the record a tool
  wrote is three clicks without leaving the page.
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

A proposed [merge](projection.md#merge-requests) is an ordinary record, so the
queue is its collection — `core.substrate.reamde.dev/recordmergerequest` in
the data nav, with the pending pile also on the overview. Opening one shows the
matcher's evidence, a field-by-field comparison of the two records, and accept
or reject. Accepting is an ordinary state transition, and performing the merge
is what that transition does.

## Change requests

A [gated](agents.md#the-policy-door) agent write lands as a
`core.substrate.reamde.dev/recordpatchrequest` instead of applying, and its
queue is that collection in the data nav, with the pending pile also on the
overview. Opening one at `/change-requests/{id}` shows the proposed create,
patch or delete and its rationale, and accept or reject; accepting is the state
transition that applies the change.

## Registry

The [catalog](bundles-catalog.md): every bundle the binary ships,
available and installed together, with an Integration badge on the ones that
connect a provider and a quarantine badge on one that needs re-installing.
The **All / Vocabulary / Integrations / Examples** filter narrows the list:
*Vocabulary* is kinds and nothing else, *Integrations* connect an external
provider, and *Examples* are the worked ones — the LLM example that installs
the provider rows an agent needs, the notes example that shows an agent
calling functions and a sub-agent, and the URL harvester that shows triggers
and a change request end to end.
Installing shows what the closure added. An installed bundle carries its
lifecycle verbs — disable, enable, uninstall, and the purge that a refused
uninstall points you at — and its connections: one row per configured provider
account, where the [OAuth consent flow](bundles.md#the-oauth-facility)
starts and where a connection's token status is visible.

## Agents

**Agents** lists the declared [agents](agents.md) with the provider and model
each resolves, and opens a chat against one.

A chat is a thread, and a thread is a run. The left rail is this agent's
threads, newest first, selected through `?thread=` so a conversation is
linkable; **New** opens an empty one. The transcript is rebuilt from the
`llmmessage` records the loop wrote, not from the browser's memory, so a
reload shows the same conversation — and every tool call is a card that says
whether it is running, settled or failed and expands to the request it sent
and the response it got, both as formatted JSON. While a run streams, the same
cards fill in live and are replaced by the stored rows when it settles.

The [`llmprovider`](agents.md#providers) rows are **not** on this page: an agent
names a provider by id, and that pointer reads on the agent's own record.
They live under Data → `core.substrate.reamde.dev` → llmproviders, and
[setting a key](agents.md#setting-or-rotating-the-key) is an ordinary record
edit.

[Triggers](functions.md#triggers) have no section of their own: they are
ordinary records, so `core.substrate.reamde.dev/trigger` in the data nav is
the list, and one trigger's record page is the trigger.

## Account

Behind the session menu, beside logging out:

- **Account** shows who you are, and holds the two credential changes: change
  your password, and replace your authenticator. Both ask for your current
  password and code in the form, because
  [the password-factor rule](auth.md#the-credential-and-the-password-factor-rule)
  refuses a bearer token here.
- **Tokens** lists every token in the repository — label, created, expiry —
  and mints and revokes them. It is also the sessions page: every
  browser and every script that holds access is one of these rows, and
  revoking one is deleting it.

Next: [running a substrate](operations.md), the deployment underneath all of
this.
