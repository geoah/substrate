# Web console

The console is the substrate in a browser: a single-page app served by the
substrate itself at `/`, talking to the same public surface as every other
client. It reads the whole repository and writes through the same public verbs,
so nothing it does needs an endpoint no other client has.

Signing in is the same exchange [substratectl](substratectl.md) makes: the repository, the password,
and the current 6-digit code ([users and tokens](auth.md)). The console then
holds a token exactly like a script does: a session is its
[token record](auth.md#tokens), which is why logging out revokes it. A
substrate that is open for registration
also serves a registration page at `/register`, with the invite code as its
first field.

Five destinations — Overview, Changelog, Registry, Connections and Agents —
with the account behind the session menu, and **Search** one keystroke away: ⌘K opens
the palette, which jumps to a page or a kind, or hands what you typed to the
[Search page](#search) as a records query.

## Overview and data

The home page is an **Overview**: recent activity, anything waiting on you, and
a count per kind that doubles as the way in. Following one opens that kind's
collection at `/data/{authority}/{package}/{kind}`: a data address is the kind
reference, segment for segment.

A kind opens on two tabs — its **Records**, a filterable and pageable
collection, and its **Definition**, the declaration rendered as the manifest it
is. From the records tab you can create one, and from a record you can edit it:
either way the editor is the same surface, and it goes out as the ordinary
`put`.

The records tab narrows two ways, and both travel in the URL so a view can be
shared. **Filters** are one control per property, offered only for the
properties the server will filter. Typing into a text property (`string`,
`text`, `markdown`) is a full-text `match` on that property's own words, in
the [search grammar](api.md#the-search-grammar): every word must appear,
`lay*` is a word prefix, `"a phrase"` keeps words together, `-word` excludes,
`a OR b` takes either, and a leading `=` asks for the exact value instead. An
email, URL or phone takes the exact value, or a trailing `*` for starts-with;
a state or an enum offers its values; a comma means any of. The **search box**
beside the filters is the same grammar against every text the kind indexes at
once (the filter's `search` arm), composed with the filters and the sort, so
the table stays a table: the rows that match, in the order you chose, paged
like any other list.

Collapsed authorities and packages, the desktop sidebar state, and favorite
kinds are saved in the repository's `core/consolepreference` record. Stars add
kinds to **Favorites** above Data; up and down controls reorder them. Updates
use version preconditions and retry against fresh state after a conflict.

## Search

`/search` is the [ranked read](api.md#search) as a page: a query in the
[search grammar](api.md#the-search-grammar), the kind to narrow to (or every
kind), and the hits best first, each with the full kind reference and id and
its raw per-arm score labelled — `words` is the lexical rank, `meaning` the
embedding similarity. How to rank is the reader's choice and it sticks:
**Words** (the default: full-text over every indexed text, free, and it
answers on every repository), **Words + meaning** (the fused hybrid ranking,
which falls back to words alone where no embeddings provider is configured)
or **Meaning** alone (embedding similarity over the properties that opted in,
which needs a provider and spends an embedding call per search). A search the
server refuses — a mode with no provider behind it, a query with no word in it
— shows the server's own problem, verbatim. When the semantic index is still
being built, the page says how many values are pending beside the ranking.

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
  editor opens from, using the same labels and descriptions with bordered values.
- **Manifest**: the [envelope](data-model.md#the-envelope), with every kind
  reference and every record reference rendered as a link you can follow.
- **Graph**: separate incoming references, outgoing references, and mapped or
  merged sources. Groups show the full kind reference and a labelled reference
  property. A member expands in place into its own graph. Source grouping reads
  the installed mapping declarations; merge history includes former record ids.
- **Activity**: this record's own slice of [the changelog](changelog.md), with the
  full actor on every row. Mapping writes distinguish value sources, the engine
  committing the write, and the initiating actor when recorded. Expanded changes
  show the raw payload immediately.
- **Provenance**: which actor wrote each property, and at which
  [tier](terms.md#truth-and-derivation).

A merged-away record says so and points at its canonical winner; a tombstoned
one says so too. The two segments above a kind each have a page of their own:
`/data/{authority}` tables every kind that authority publishes, package by
package, and `/data/{authority}/{package}` tables the one package's kinds.

## Changelog

[The changelog](changelog.md), newest first, one row per committed change, expanded
in place to its payload and the records it moved. Filters cover kind, actor, op, and free text, and the
same view tails live. It is the audit trail and the debugging surface in one,
because there is only one changelog.

## Merge requests

A proposed [merge](projection.md#merge-requests) is an ordinary record, so the
queue is its collection — `substrate.reamde.dev/core/recordmergerequest` in
the data nav, with the pending pile also on the overview. Opening one shows the
matcher's evidence, a field-by-field comparison of the two records, and accept
or reject. Accepting is an ordinary state transition, and performing the merge
is what that transition does.

## Change requests

A [gated](agents.md#the-policy-door) agent write lands as a
`substrate.reamde.dev/core/recordpatchrequest` instead of applying, and its
queue is that collection in the data nav, with the pending pile also on the
overview. Opening one at `/change-requests/{id}` shows the proposed create,
patch or delete and its rationale, and accept or reject; accepting is the state
transition that applies the change.

## Registry

The [catalog](bundles-catalog.md): every bundle the binary ships, in the two
tier sections
([0048](decisions/0048-providers-are-published-samples-are-copied.md)), taken
and untaken together, with a quarantine badge on one that needs re-installing.
**Providers** are the packages a publisher owns (Google, GitHub, Linear,
WHOOP, Notion, Beeper) and their row's button is *Install*, under the
authority that publishes them; the upgrade offer lands here. **Samples** are
the vocabulary to copy (people, tasks, calendar, and the worked LLM, notes,
reading-list and pebble examples) and their button is *Import as yours*, with the row
previewing the identity it will land under (`ada.example.com/tasks`) before it
is pressed. A held copy is offered *Upgrade* too, through the import door,
when the binary ships the sample at a newer version than the copy was taken
at; where the copy was edited since, the dialog says the edits are replaced
before it sends the preview's confirmation
([0070](decisions/0070-a-copy-is-upgraded-through-its-origin-stamp-and-requires-pins-a-floor.md)).
A requirement the repository holds below the closure's `requiresAtLeast`
floor disables the button, naming both versions. A bundle applied outside
the shipped catalog has no tier and is listed on its own.
Taking one shows what the closure added. An installed bundle carries its
lifecycle verbs — disable, enable, uninstall, and the purge that a refused
uninstall points you at — and its connections: one row per configured provider
account, where the [OAuth consent flow](bundles.md#the-oauth-facility)
starts and where a connection's token status is visible.

## Connections

**Connections** is the operations surface over every provider account
([Connections](bundles.md#connections)): one page, read from the native
`accountconfig` records the provider bundles ship and the core `sync` trait
they bind ([0085](decisions/0085-a-sync-is-a-core-trait-the-dispatcher-stamps.md)),
and every read an existing route. It has two halves.

**Providers** lists every installed bundle whose closure declares an account
kind, plus every bundle the catalog calls a provider, with its lifecycle
badge, whether its client credentials are set (the `oauth2`-trait client, or
a token provider's config; "credentials missing" is what the Registry row's
setup step means), its accounts counted by token status, and the two doors:
*Set up* or *Edit* opens the config record's form, whose secret fields are
write-only and say `set` or `not set` beside their names, never a value;
*Add account* creates one. **Accounts** is one row per account across all
providers: a health dot (broken when the grant or the sync is erroring,
attention when pending, paused or throttled, idle when connected and never
synced), the provider, the account by its `email` or `displayName`, the
token status with the granted scopes on hover, the sync state chip with the
sync's own message in full, the last run as relative time, the cadence, the
backfill depth, and the parked and lagging deliveries of the triggers on its
kind. The row's verbs are the four a Connection takes: **Connect** or
**Reconnect** starts the [OAuth consent](bundles.md#the-oauth-facility) and
opens the URL it mints at click time; **Sync now** stamps `syncRequestedAt`
and wakes the on-request triggers; **Pause** and **Resume** flip
`syncPaused`; **Edit** changes the toggles, frequency and depth through the
ordinary record dialog; **Disconnect** deletes the record. The page follows
the [change feed](changelog.md) for the account kinds and the run ledger, so
a sync's state moves without a reload.

Opening a row is the **account detail**: the trait rendered whole (state,
message, last run and its duration, the request and whether it was served, a
progress bar from `syncProgress`, one row per stream from `syncStreams`, the
last error), the record's other properties with cursors and queues shown as
counts rather than their bytes, the record triggers on the kind with cursor,
head, lag, last fire, parked and pending from `…/trigger/status` and a
*Wake* and *Run* each, the newest `triggerrun` rows of those triggers with
status and error text, the parked deliveries with a *Retry* each, and the
bundle's mirror kinds with their live row counts. The same renderer is a
**Sync** tab on the record page of any kind binding the trait.

## Agents

**Agents** lists the declared [agents](agents.md) with the provider and model
each resolves, and opens a chat against one.

A chat is a thread, and a thread is a run. The left rail is this agent's
threads, newest first, selected through `?thread=` so a conversation is
linkable; **New** opens an empty one. The transcript is rebuilt from the
`llm/message` records the loop wrote, not from the browser's memory, so a
reload shows the same conversation — and every tool call is a card that says
whether it is running, settled or failed and expands to the request it sent
and the response it got, both as formatted JSON. While a run streams, the same
cards fill in live and are replaced by the stored rows when it settles.

The [`llm/provider`](agents.md#providers) rows are **not** on this page: an agent
names a provider by id, and that pointer reads on the agent's own record.
They live under Data → `substrate.reamde.dev/llm` → providers, and
[registering one](agents.md#registering-a-provider) is an ordinary record
write.

[Triggers](functions.md#triggers) have no section of their own: they are
ordinary records, so `substrate.reamde.dev/core/trigger` in the data nav is
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

Next: [running one locally](running-locally.md), the substrate on your own
machine.
