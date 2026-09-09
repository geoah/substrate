# The changelog and watch

Every committed write appends one entry to the repository's **changelog**, in the
same transaction as the write itself. It is append-only, strictly sequential,
and there is exactly one of it per repository: the source of truth, the change
feed, the audit trail, and the input stream for triggers and integrations are
all the same changelog. The [records](data-model.md) you read are its fold. Identical
re-writes append nothing, so a re-sync leaves no wake.

One entry, as the wire carries it, for the task created on the
[API page](api.md):

```json
{"seq": 4190,
 "ts": "2026-08-04T10:00:00.183742Z",
 "actor": "api",
 "op": "put",
 "kind": "samples.substrate.reamde.dev/tasks/task",
 "recordId": "kq3v9x2m41pf",
 "payload": {"created": true, "properties": ["name", "dueAt"]},
 "affected": [{"kind": "samples.substrate.reamde.dev/tasks/task", "id": "kq3v9x2m41pf", "version": 1}],
 "hash": "5f0c…64 hex chars…9a1d"}
```

`actor` names what wrote ([the actor domain](api.md#actors) is closed) and is
the caller's own claim; the entry also stores a **principal**
beside it, the id of the token the door resolved from the bearer secret,
which no caller can name. Two tokens writing as `api` are one actor and two
principals. The entry's checksum covers it, and it is not a secret: it is a
token record's id, which the repository's own reader can already list. It has
no field of its own on the wire; a write's recorded effects name it beside
each property manager they set. A write no token stands behind — the seed, the
boot upgrade, a background worker, registration and login — carries an empty
principal. `op` is the mutation
that made the row (`put`, `patch`, `delete`, `merge`, `split`, plus the
engine's own housekeeping), and
`kind` plus `recordId` are the record's full identity. `payload` names what
changed rather than repeating it: `created` on first write, `restored` on an
undelete, the list of accepted `properties`, the resulting `states` when a
transition moved one, `winner` and `loser` on a merge or a split, and
`renamed` (old name to new) on the `patch` a property rename writes per record
([vocabulary](vocabulary.md#vocabulary-evolution-and-the-dialect-contract)).
`affected` is the change event ([below](#the-change-event)).
One op is stored and never served: `delivery`, the engine's own entry
carrying a trigger's cursor advance, schedule fire, parked failure,
paged-drain page or accepted webhook request, appended in the transaction
that commits the effects it acknowledges
([decision 0064](decisions/0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md),
[0068](decisions/0068-an-accepted-webhook-is-a-pending-entry-in-the-delivery-ledger.md)).
Every read here skips those rows, and a resume cursor still moves past them.

Beneath the wire row, the stored entry also carries the write's **values** as
the fold effects it applied, under the payload key `fold`, which is what makes
the changelog replayable rather than merely informative: `substratectl
repository rebuild` replays the changelog through the same fold code
([running a substrate](operations.md#operator-recovery)). The effects never
reach the wire: no REST or GraphQL response carries `fold`, so a change to how
the fold spells an effect is a storage change and not an API change. A
secret-typed value never reaches the changelog at all: the effect carries an
opaque ref into the sealed store, the material lives there encrypted, and a
read of the record renders the property `<redacted>`.

## The change event

A row's `affected` list is the public event: every record the entry moved,
one element per record, in the order the entry touched them.

```json
{"seq": 4207, "op": "merge", "kind": "samples.substrate.reamde.dev/people/person", "recordId": "p1",
 "payload": {"winner": "p1", "loser": "p2",
             "moved": {"formerIds": [], "managers": {}, "labels": {}, "annotations": {},
                       "applied": ["email"], "overwritten": []}},
 "affected": [
   {"kind": "samples.substrate.reamde.dev/people/person", "id": "p1", "version": 6},
   {"kind": "samples.substrate.reamde.dev/people/person", "id": "p2", "version": 3, "deleted": true}]}
```

- `kind` and `id` are the record's full identity, the same pair a `GET
  /api/v1/{kind}/{id}` takes.
- `version` is the version the record reached in this entry, the number a
  read of the record returns until its next change. It is absent on a purge
  (a `gc` entry: the record has no version afterwards) and on every entry
  written before this change, whose effects recorded no version.
- `deleted` is `true` when the entry tombstoned or purged the record. A read
  of a tombstoned record still answers, with `deletedAt` set; a purged one is
  `not_found`.

The list holds the addressed record and any other the entry moved: a `merge`
names the winner and the tombstoned loser, a `split` the loser and the
rewritten winner, a `gc` entry the record it purged and, under
`reason: owner_collected`, each record the cascade tombstoned. An entry that
recorded no record effect names its addressed `(kind, recordId)` with no
version.

What the event promises: a client that fetches each affected record as the
stream names it, and drops the ones the stream says are deleted, holds a
current copy of the repository, and a copy already at the named version or
past it need not fetch. What it does not promise: the values a change wrote,
or a record's history. A property value is read off the record, never off the
stream, and a past version cannot be reconstructed from public rows
([decision 0061](decisions/0061-a-change-event-names-the-affected-records-and-clients-fetch-them.md)).
The [trigger envelope](functions.md#triggers) makes the same promise in the
same way: it ships the record's current state beside the change.

Two guarantees consumers may lean on:

- **Sequence order is commit-visibility order**: no reader ever sees entry N
  with N-1 still uncommitted, so resuming from a remembered `seq` misses
  nothing. Gapless resume is a guarantee, not a convention, and it survives a
  restart because the counter is the repository's own `max(seq)`, never process
  state. A seq is a position in one **history generation** ([watching](#watching)),
  and a resume is held to it, so a cursor saved before an older directory was
  restored is refused rather than resumed against numbering it never saw.
- **Causal chains are finite.** Every delivery-authored entry records the seq
  that caused it, always strictly smaller, so a chain cannot loop, and the
  engine parks a chain deeper than its cap (16) rather than spinning. The link is stored, not published: a change on the wire
  carries `seq`, `ts`, `actor`, `op`, `kind`, `recordId`, `payload`,
  `affected` and the entry's `hash` (below); a row of `/changes` adds
  `triggers` ([watching](#watching)).

## Change verbs

`op` above is the changelog's own word for a write. Three other properties name
the same act in their own vocabulary, and a reader who assumes they agree
writes a rule that matches nothing. Here is one act across all four:

| The act                       | Changelog `op`                                     | `recordpatchpolicy.selector.ops` | `recordpatchrequest.op` | `trigger.source.record.ops` |
| ----------------------------- | -------------------------------------------------- | -------------------------------- | ----------------------- | --------------------------- |
| a record comes into existence | `put`, with `created: true` in the payload         | `put`, `patch`                   | `create`                | `create`                    |
| an existing record changes    | `put`, `patch`, `merge`, `split`                   | `put`, `patch`                   | `patch`                 | `update`                    |
| a record goes away            | `delete`, and `gc` on the collector's pass         | `delete`                         | `delete`                | `delete`                    |

Merge and split are the two rows the table cannot hold, because each one
changes two records under one entry. A merge tombstones the loser under the
winner's single `merge` entry, so `OpOf` reads that entry as an `update` on the
winner and no trigger delivers a delete class for the loser (reads of the
loser's id forward to the winner, so nothing is lost). An accepted split
reverses it, restoring the loser under one `split` entry addressed to the
loser, again read as an `update`, so a record can return to existence with no
create class delivered. Both payloads name the pair as `winner` and `loser`,
which is what lets the `recordId` scope return the entry to either record
([watching](#watching)).

Each column answers a different question.

- **The changelog says what landed.** `llmmessage.changes.op`, the ops an agent
  turn's dispatch wrote, is this column verbatim except `gc`, which is the
  collector's own pass and not any dispatch's write.
- **A policy selector says what the agent called**: the verb behind the write,
  matched before anything looks at the target. Three routes reach it, a `mutate`
  tool call, a function tool's write effect (one gated effect holds the whole
  batch) and a `propose` carrying a `create`, `patch` or `delete`, which is
  matched as the `put`, `patch` or `delete` it stands for and consults the
  policy's judge only. Policy evaluation only ever sees those three verbs, so a
  function effect's `merge` and `split` are bounded by the emit ceiling alone
  and no selector matches them.
- **A request's `op` says what accepting will do.** The gate resolves the verb
  against the target as it converts the write, and it resolves `put` and
  `patch` the same way: with no live target the request reads `op: create` and
  names the record by `targetKind` and `targetId`, and with one it reads
  `op: patch` and carries the `target` reference. So a selector matching either verb
  can produce `op: create` on the request it gates, and gating `patch` is not
  edit-only review. A selector written `ops: [create]` is refused.
- **A trigger says what happened to the record**, whatever verb the writer
  used. A `put` that created the row is a `create`, a `put` over a live row is
  an `update`, and every op that is neither a create nor a delete is an
  `update`.

The request's `create` is not the changelog's `put`: accepting one mints the
record when the id is free and conflicts when a record that does not match the
proposal already holds it, where a `put` overwrites.

<!-- The anchor keeps decision records 0009, 0010 and 0011 resolving: they link
#the-chain, and an accepted record's body may not change. -->
<a id="the-chain"></a>

## The checksum and the segment files

Every entry carries a SHA-256 **checksum** of its own canonical line, stamped
by the writing transaction into the `changelog.hash` column and returned on the
wire as `hash`. The checksum covers the entry's own bytes and nothing else: it
does not chain to the previous entry, and nothing signs it.
`substratectl repository verify` recomputes every entry's checksum and names
the first seq whose stored bytes no longer produce it
([running a substrate](operations.md#operator-recovery)).

The Postgres `changelog` table is the live index: the change feed, the triggers
and the causal walk read it. The truth on disk is the repository's directory
under `SUBSTRATE_DATA_ROOT`
([the repository directory](operations.md#the-repository-directory)), where
`changelog/` holds the same entries as newline-delimited JSON segment files,
one object per line, keys sorted, the checksum in `sum`:

```json
{"actor":"api","kind":"samples.substrate.reamde.dev/tasks/task","op":"put","payload":{"created":true,"fold":[{"…":"…"}],"properties":["name","dueAt"]},"principal":"k7…","recordId":"kq3v9x2m41pf","seq":4190,"sum":"sha256:5f0c…","ts":"2026-08-04T10:00:00.183742Z","txn":4191}
```

`sum` is `sha256:` plus the hex digest of the same line with the `sum` key
absent, and the `hash` column and the wire field hold the same 32 bytes.
`causedBy` appears only on an entry a delivery caused. `txn` is the seq of the
last entry of the transaction that appended the line: a transaction of one
entry has `txn` equal to `seq`, and a merge, which writes the loser's
tombstone and the winner's entry in one commit, gives both lines the second
one's seq. The `changelog.txn` column holds the same value. A segment is named
by the seq of its first line (`000000000000001.ndjson`); the highest-numbered
one is the active segment and grows. When it passes
`SUBSTRATE_CHANGELOG_SEGMENT_BYTES` (256 MiB by default) the writer finishes
it with a `.sha256` sidecar holding the digest of the whole file and opens the
next. A finished segment never changes. One transaction is one append, and
the segment rotates only after an append, so a transaction never crosses a
segment, however large it is.

The transaction's lines are written and fsynced before Postgres commits,
without the newline that ends the last line; that newline lands after the
commit and is the commit marker. A sealed value is staged as a pending file
the same way and renamed into place after. A directory that cannot be written
before the commit rolls the write back with `503 unavailable`
(`ErrDirectoryWrite`); a crash between the commit and the newline leaves an
unfinished transaction the next boot completes from Postgres
([what happens at boot](operations.md#what-happens-at-boot)). A reader accepts
exactly one kind of damage, an unfinished transaction at the end of the active
segment: a torn last line and, before it, the complete lines that carry the
same `txn`. It cuts them together, back to the last line that ends its
transaction, so half a commit never opens as history; the server then appends
the transaction again from the table, and a restore from a copy of the
directory loses that transaction whole, never in part. A bad `sum`, a seq
that does not follow the previous one, a line whose `txn` does not fit the
transaction around it, or a finished segment whose sidecar does not match is
a named refusal, not a repair.

Segments written by v0.46.0 through v0.51.0 carry no `txn`. A reader takes such a
line as a transaction of its own, which is how those releases read it: no
boundary was recorded, and none is reconstructed. A directory this release
writes does not open under either of them
([decision 0057](decisions/0057-a-changelog-line-names-its-transaction-and-an-unfinished-one-is-cut-whole.md)).

What the checksum proves: an entry is undamaged, in the file and in the table,
and the two agree. What it does not prove: authenticity. Whoever can write the
database or the directory can rewrite a line and its `sum` together, and no
checksum sees that
([decision 0050](decisions/0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md)).
The `hash` a consumer reads on the wire is a **receipt** for the entry it
came with, not a proof: the wire payload is redacted, so the consumer cannot
recompute it, and checking a receipt means running `repository verify` on the
repository.

One value survives from before the door threaded the **principal**: entries
written then keep the string `invalid`. No write path produces it now; the
principal an entry carries is the token id the door verified.

## The dialect a changelog is written in

Each repository carries a **changelog dialect**: a monotonic integer naming the
ops and fold effects a binary must understand to replay its entries. The
effects are storage ([the change event](#the-change-event) is what a reader
gets), so the dialect names what a binary replays, never what a client parses. The claim
rides the append: the first transaction a binary appends with writes the stamp
alongside its entries, so the stamp covers every entry and no store is barred
over entries nobody wrote. Opening only reads it, and a binary whose maximum is
below the stored one refuses to open that repository, with the named error "the
changelog speaks a newer dialect than this binary can replay". A request
carrying a token then gets `503 repository temporarily unavailable` rather than
an invalid-token 401, exactly like the
[vocabulary dialect](vocabulary.md#vocabulary-evolution-and-the-dialect-contract)
that governs stored declaration rows; sign-in, which opens the repository to
mint a token, fails as an internal error.

The refusal is the point. Without it an old binary opens a store it cannot
replay, serves it for weeks, and fails only when somebody runs `repository
rebuild`, the day the changelog had to be replayable. The changelog dialect is
6 today: 1 was the changelog while `link` and `unlink` were ops, 2 the
changelog after references replaced them, 3 the entry that names its
transaction (`txn`, covered by the checksum), which a dialect 2 binary would
silently re-stamp away at boot
([decision 0057](decisions/0057-a-changelog-line-names-its-transaction-and-an-unfinished-one-is-cut-whole.md)),
and 4 the `record` delta that carries `kindVersion`, the kind declaration
version that wrote the row
([decision 0060](decisions/0060-a-record-carries-the-kind-version-that-last-wrote-it.md)),
which a dialect 3 binary would drop at replay, folding every record to 0 with
nothing refusing, and 5 the `manager` effect that carries `updatedAt`, the
stamp of a manager row a property rename moved
([decision 0063](decisions/0063-a-property-rename-is-ordinary-record-writes.md)),
which a dialect 4 binary would drop the same way, stamping the replay's own
time, and 6 the delivery ledger: the `delivery` op and the seven fold
effects a trigger's bookkeeping replays through
([decision 0064](decisions/0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md)).
A repository's stored dialect is not on the wire, and
neither are the entries written in it: what
[API discovery](api.md#discovery) reports is the binary's maximum. The dialect
is in the repository directory, as `changelogDialect` in `repository.json`,
which the transaction that records the claim rewrites before it appends, so a
copy of the directory never holds segments its manifest understates
([the repository directory](operations.md#the-repository-directory)).

`repository rebuild` reads the stamp again, under the changelog lock and
before it touches anything: it replays every entry, and a process that opened
the repository before another raised the stamp would be interpreting entries
in a spelling it does not know.

A repository written by a newer binary cannot be served by an older one again:
downgrading means restoring the copy taken before the upgrade, the same as for
a vocabulary promotion
([upgrading the binary](operations.md#upgrading-the-binary)).

## Watching

Any collection, and the changelog itself, streams with `?watch=1`:
newline-delimited JSON (`application/x-ndjson`), opened with a bookmark you can
resume from, so a consumer that disconnects misses nothing:

```http
GET /api/v1/changes?from=4189&generation=7f3a0c2e9b1d4e6f&watch=1

{"bookmark": 4189, "generation": "7f3a0c2e9b1d4e6f"}
{"seq": 4190, "op": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
 "recordId": "kq3v9x2m41pf", "actor": "api",
 "affected": [{"kind": "samples.substrate.reamde.dev/tasks/task", "id": "kq3v9x2m41pf", "version": 1}]}
```

Without `from`, the stream opens at the current head and tails forward; the
bookmark is the seq it opened at and the generation that seq belongs to, so
remembering the pair is all a resume needs.

The **history generation** is an opaque string the server mints for a
repository when it registers, and mints again when its directory is imported
into a database that holds no row for it, which is what a
[restore](operations.md#backups) into a fresh database is. A database dump
keeps the row and its generation, so a restore that starts from a dump runs
`substratectl repository rotate-generation` once per repository. A restart and
`repository rebuild` keep it. A seq is a position in one generation only: an older copy of the directory
imported over an emptied database restarts the numbering below whatever
cursors clients saved, and a bare seq cannot tell the two histories apart. So
`from` or `before` above 0 carries the `generation` it was read under, and the server
refuses a cursor under another generation, above the head, or with no
generation at all, with the `compacted` error [below](#frames-and-the-horizon).
`from=0` and `before=0` name no entry, so they need none.

The same endpoint pages backward through history with `before=`, and filters
the same way in watch and history modes alike. Every filter parameter is plural
and takes a repeated parameter or a comma-separated list: `kinds`, `ops`,
`actors`, and their negations `excludeKinds`, `excludeOps`, `excludeActors`.
`q` matches free text across the row's kind, actor, record id and payload
text. Scoping the feed to one record takes **both** `recordId` and
`recordKind`, because an id alone names no record; either one without the
other is a `bad_request`. The scope also returns a `merge` or `split` entry
whose payload names the id as `winner` or `loser`. Such a row's own `recordId`
is the other side of the pair (the winner on a `merge`, the loser on a
`split`), and the row's `affected` list names both, so a client re-fetches
each record the list names rather than the row's `(kind, recordId)` alone. The
scope does not follow the winner's later writes under a merged-away id. A
singular guess (`kind=`, `op=`, `actor=`) is
refused naming the plural rather than silently answering with the whole
unfiltered feed.

History returns one JSON body
(`{"changes": […], "cursor": <seq>, "head": <seq>, "generation": "…"}`)
newest-first; `cursor` is the continuation, the oldest seq the page consumed,
which you pass as the next `before` (omitted when the walk is exhausted), so
the client never computes the boundary itself, and `head` with `generation`
is the same watch handoff a list page carries. The watch resumes from a
transparent `from={seq}`. (Note the continuation rule from
[the API](api.md#pagination): the changelog uses transparent `from` and `before`
seqs, because a seq is a real ordinal; opaque `after` cursors are for list
pagination.)

Every row this endpoint returns also carries `triggers`: each runnable enabled
[trigger](functions.md#triggers)'s stance on that row — `pending`,
`processed`, or `parked` with its error — omitted where the trigger cannot fire
on it at all. Per-collection watches stay plain rows.

This is the other half of the list-to-watch handoff: a list response carries
the changelog `head` seq at its snapshot and the `generation` it belongs to, so
paging a collection and then opening `watch?from={head}&generation={generation}`
misses nothing and double-sees nothing.

## Frames and the horizon

The ndjson framing is pinned so a client can parse a stream unambiguously:

- A line **with** a `seq` is a change row.
- A line **without** a `seq` is a **control frame**, keyed by its field.
  The opening `{"bookmark": N, "generation": "…"}` is one such frame, and the
  idle heartbeat `{}`,
  sent every 30 seconds, is another. The stream may also end with a **terminal
  error frame** carrying the one [problem object](api.md#errors), so a
  mid-stream failure is legible rather than a dropped connection:

```json
{"error": {"code": "internal", "message": "…"}}
```

The changelog has a **horizon**: the oldest seq still resumable. Requesting `from=`
a seq below the horizon is a `compacted` error (HTTP 410), and so is a cursor
the changelog cannot verify: above the head, under another history generation,
or with no generation beside a `from` above 0. The problem object carries the
current `head` and `generation`, so a consumer that has fallen too far behind,
or resumes after a restore, is told plainly instead of silently missing rows,
and its handler is one it **must** have: re-list, then resume the watch from
the head and generation the response names. The horizon is reported in
[API discovery](api.md#discovery), and it is
**0 today**: nothing prunes or compacts the changelog, so replaying any consumer from
any seq, zero included, is possible. Retention is a deployment **policy**, not
a wire guarantee: the wire promises gapless resume from any seq at or above the
horizon, and the horizon is where policy lives.

## Who consumes it

- **Triggers** ride it: every record-sourced trigger owns a cursor, and each
  row is checked against every enabled trigger. The feed reports each trigger's
  stance on each row ([functions](functions.md)), and the trigger records
  where it stands as `delivery` entries in the same changelog, which the feed
  never shows.
- **Watchers**: the stream above, and `substratectl watch` is that stream in a
  terminal ([substratectl](substratectl.md)). Integrations reconcile from it.
- **The console's events page** is the same feed, paged backward through
  history and filtered ([web console](console.md)).
- **`rebuild`** replays it from the segment files, which is what makes the
  fold disposable and the repository directory the thing you back up
  ([running a substrate](operations.md#backups)).

The to-do list, its GitHub feed, and anything you build next are all consumers
of one changelog.

Next: [bundles](bundles.md), the closures that install new kinds and the
code that reacts to this feed.
