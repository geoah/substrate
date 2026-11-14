# The changelog and watch

Every committed write appends one entry to the repository's **changelog**, in the
same transaction as the write itself. It is append-only, strictly sequential,
and there is exactly one of it per repository: the source of truth, the change
feed, the audit trail, and the input stream for triggers and providers are
all the same changelog. The [records](data-model.md) you read are its fold. Identical
re-writes append nothing, so a re-sync leaves no wake.

One entry, as the wire carries it, for the task created on the
[API page](api.md):

```json
{"seq": 4190,
 "ts": "2026-08-04T10:00:00.183742Z",
 "actor": "6nk6ekkczdgh399nt2pqpccjz0",
 "token": "01jbx9k0token000000000000a",
 "op": "put",
 "kind": "samples.substrate.reamde.dev/tasks/task",
 "recordId": "01jbx9m2qz7k4c5v8n3w6t1hrd",
 "payload": {"created": true, "properties": ["name", "dueAt"]},
 "affected": [{"kind": "samples.substrate.reamde.dev/tasks/task", "id": "01jbx9m2qz7k4c5v8n3w6t1hrd", "version": 1}],
 "hash": "5f0c…64 hex chars…9a1d"}
```

`actor` is the id of the [actor record](api.md#actors) that wrote the entry:
here the person. `onBehalfOf` is the id of the one it acted for, present only
where it acted for someone: an app always acts for the person, and a
function or an agent acts for the person when a person's request started it.
`token` is the id of the token record the write stood behind, present only
where one did: the person's sign-in, or the app's token. The server derives
all three from the bearer secret and the code that runs, and no caller names
any of them. Two sign-ins of one person are one actor and two tokens. A
write no token stands behind (the seed, the boot upgrade, a background
worker, a sync on a schedule, registration and login) carries no `token`.
The entry's checksum covers the three. The token is not a secret, since it is a
token record's id, which the repository's own reader can already list, but
it is the owner's to read: a change an app's token or an assistant's reads
carries no `token`. `op` is the mutation
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
A paged-drain page names the drain's cursor by `cursorSha256` and
`cursorBytes` instead of carrying it, and a park carries it whole
([0141](decisions/0141-a-paged-drain-names-its-cursor-by-hash-and-a-park-carries-it-whole.md)).
Every read here skips those rows, and a resume cursor still moves past them.

Beneath the wire row, the stored entry also carries the write's **values** as
the fold effects it applied, under the payload key `fold`, which is what makes
the changelog replayable rather than merely informative: `substratectl
repository rebuild` replays the changelog through the same fold code
([running a substrate](operations.md#operator-recovery)). The effects never
reach the wire: no response carries `fold`. They are the
[stored format](stored-format.md#the-effects) instead, which every export
carries and every later release reads, so a change to how an effect is spelled
raises the changelog dialect and ships a conversion. A
secret-typed value never reaches the changelog at all: the effect carries an
opaque ref into the sealed store, the material lives there encrypted, and a
read of the record renders the property `<redacted>`.

## The change event

A row's `affected` list is the public event: every record the entry moved,
one element per record, in the order the entry touched them.

```json
{"seq": 4207, "op": "merge", "kind": "samples.substrate.reamde.dev/people/person", "recordId": "p1",
 "payload": {"winner": "p1", "loser": "p2",
             "moved": {"formerIds": [], "marks": [], "labels": {}, "annotations": {},
                       "applied": ["email"], "overwritten": []}},
 "affected": [
   {"kind": "samples.substrate.reamde.dev/people/person", "id": "p1", "version": 6},
   {"kind": "samples.substrate.reamde.dev/people/person", "id": "p2", "version": 3, "deleted": true}]}
```

- `kind` and `id` are the record's full identity, the same pair a `GET
  /api/v1/{kind}/{id}` takes.
- `version` is the version the record reached in this entry, the number a
  read of the record returns until its next change. It is absent in two
  cases: a purge (a `gc` entry: the record has no version afterwards), and a
  `patch` that wrote only an annotation, which moves no version (a judge's
  policy verdict on a patch request, or the conflict note an accept that
  lost leaves on its request).
- `deleted` is `true` when the entry tombstoned or purged the record. A read
  of a tombstoned record still answers, with `deletedAt` set; a purged one is
  `not_found`.
- `state` is `true` where the entry holds the record's **state entry**: a
  compaction replaced the record's earlier entries with the record's whole
  state as of this one, which keeps its seq, time and actor
  ([the stored format](stored-format.md#state-entries)). A client reads it as
  any change: the record changed, and the version is its own. A `put` or a
  `patch` whose state stands for the record's creation reads as the creation
  (`op` `put`, `payload.created`), so a reader that starts from the beginning
  meets each record once, created in its latest state. Since revision 3 of
  the `changefeed` feature.

The list holds the addressed record and any other the entry moved: a `merge`
names the winner and the tombstoned loser, a `split` the loser and the
rewritten winner, a `gc` entry the record it purged and, under
`reason: owner_collected`, each record the cascade tombstoned. An
annotation-only `patch` names its addressed `(kind, recordId)` alone.

What the event promises: a client that fetches each affected record as the
stream names it, and drops the ones the stream says are deleted, holds a
current copy of the repository, and a copy already at the named version or
past it need not fetch. What it does not promise: a record's history. A
record's current value is read off the record, never off the stream, and a
past version cannot be reconstructed from public rows
([decision 0061](decisions/0061-a-change-event-names-the-affected-records-and-clients-fetch-them.md)).
The [trigger envelope](functions.md#triggers) makes the same promise in the
same way: it ships the record's current state beside the change.

### Values, on request

A read that passes `values=1` gets, on each affected record, what the entry
did to each property it moved, before and after, in name order:

```json
{"kind": "samples.substrate.reamde.dev/tasks/task", "id": "01jbx9m2qz7k4c5v8n3w6t1hrd", "version": 4,
 "properties": [
   {"name": "description", "before": "Numbers from finance first."},
   {"name": "priority", "before": "high", "after": "urgent"},
   {"name": "url", "after": "https://tracker.example.com/1"}]}
```

`before` is absent where the record held no value (a property the entry
added, or any property of a creation), and `after` is absent where the entry
cleared it. The values are the ones a read of the record renders, under the
same names: the declared properties, `title` where the kind does not render
it from a template, the instants, and each state. A
sensitive property reads `<redacted>` on both sides, exactly as a record read
renders it, and a kind the repository no longer declares carries no values.

The redaction fails closed, because the history outlives the declaration it
was written under and only the current one is consulted. A property value is
shown only where the kind declares that name now (or declares it as a
property's `renamedFrom`), with a datatype that is not sensitive, and the
value is not shaped like what a secret or a digest stores. So a secret that
was renamed, dropped or retyped, or whose kind was redeclared, still reads
`<redacted>` in every entry that sealed it, and so does any property the kind
no longer declares at all.
A delete, a tombstone or a purge moves no property, so its record carries
none.

A property rename (a declaration's `renamedFrom`, applied as one `patch` per
record) reads as one change, not a removal and an addition: the entry names
the new property with `"renamedFrom"` set to the old name, `before` is the
value the record held under the old name and `after` the value under the new
one, and the old name carries no change of its own
([decision 0114](decisions/0114-a-change-row-reads-a-property-rename-as-one-move.md)).
The pairing reads the entry's own `payload.renamed`, so every rename already
written reads this way.

Nothing is stored for this: `after` is the entry's own effect, and `before`
is derived by walking the record's earlier entries newest first (its own, and
any merge or split naming it) to the last one that set or cleared the
property, stopping at the record's creation. The walk checks the record's
versions run unbroken. Where it cannot answer (a gap in them, an entry written
before entries held values, a history with no creation, or a previous write
further back than the read's budget of 4096 earlier entries, which every
record on the page shares) the property carries `"beforeUnknown": true` and
no `before`, which is not the same as "there was none". A state entry lists
every value its record holds, each `beforeUnknown`: the values before it are
not kept, and a walk from a later entry stops at it
([decision 0135](decisions/0135-a-change-row-carries-before-and-after-values-on-request-derived-at-read.md)).
The walk costs a read per record on the page, so a client that does not ask
pays nothing and gets the same rows without `properties`. Every mode of
`/changes` honours it: the history page, the forward read and the watch.

Two guarantees consumers may lean on:

- **Sequence order is commit-visibility order**: no reader ever sees entry N
  with N-1 still uncommitted, so resuming from a remembered `seq` misses
  nothing. A compacted history has gaps where entries were replaced, and a
  record whose changes after the position were replaced is met at its state
  entry, once, so a resume still misses no record. Resume is a guarantee, not a convention, and it survives a
  restart because the counter is the repository's own `max(seq)`, never process
  state. A seq is a position in one **history generation** ([watching](#watching)),
  and a resume is held to it, so a cursor saved before an older copy was
  restored is refused rather than resumed against numbering it never saw.
- **Causal chains are finite.** Every delivery-authored entry records the seq
  that caused it, always strictly smaller, so a chain cannot loop, and the
  engine parks a chain deeper than its cap (16) rather than spinning. The link is stored, not published: a change on the wire
  carries `seq`, `ts`, `actor`, `onBehalfOf` and `token` where set, `op`,
  `kind`, `recordId`, `payload`, `affected` and the entry's `hash` (below);
  since revision 2 of the `changefeed` feature, `actor` is an actor record's
  id and `onBehalfOf` and `token` are beside it. A row of `/changes` adds
  `triggers` ([watching](#watching)).

## Change verbs

`op` above is the changelog's own word for a write. Three other properties name
the same act in their own vocabulary, and a reader who assumes they agree
writes a rule that matches nothing. Here is one act across all four:

| The act                       | Changelog `op`                                     | `recordpatchpolicy.selector.ops` | `recordpatchrequest.op` | `trigger.source.record.ops` |
| ----------------------------- | -------------------------------------------------- | -------------------------------- | ----------------------- | --------------------------- |
| a record comes into existence | `put`, with `created: true` in the payload         | `put`, `patch`                   | `create`                | `create`                    |
| an existing record changes    | `put`, `patch`, `merge`, `split`                   | `put`, `patch`                   | `patch`                 | `update`                    |
| a record goes away            | `delete`, and `gc` on a sweep or a purge           | `delete`                         | `delete`                | `delete`                    |

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

- **The changelog says what landed.** `llm/message.changes.op`, the ops an agent
  turn's dispatch wrote, is this column verbatim except `gc`, which is the
  collector's own pass and not any dispatch's write.
- **A policy selector says what the agent called**: the verb behind the write,
  matched before anything looks at the target. Three routes reach it, a `write`
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
- **A run summary's `verb` says what a sentence about the row would**
  ([run summaries](#run-summaries)): the trigger's `create` and `update`, plus
  `restore` for a `put` that brought a tombstone back, and `delete`, `merge`,
  `split` and `gc` kept apart, because each reads as a different sentence.

The request's `create` is not the changelog's `put`: accepting one mints the
record when the id is free and conflicts when a record that does not match the
proposal already holds it, where a `put` overwrites.

<!-- The anchor keeps decision records 0009, 0010 and 0011 resolving: they link
#the-chain, and an accepted record's body may not change. -->
<a id="the-chain"></a>

## The checksum and the archive

Every entry carries a SHA-256 **checksum** of its own canonical line, stamped
by the writing transaction into the `changelog.hash` column and returned on the
wire as `hash`. The checksum covers the entry's own bytes and nothing else: it
does not chain to the previous entry, and nothing signs it.
`substratectl repository verify` holds the table's rows to their seqs and
transactions, holds every line of the archive to the checksum the table
stamped for its seq, and names each seq that disagrees; with
`--recanonicalize` it also recomputes each row's checksum from its stored
columns ([running a substrate](operations.md#operator-recovery)).

The Postgres `changelog` table is the history: the change feed, the triggers
and the causal walk read it, and every fold, a rebuild and an import alike,
is folded from it. A write is confirmed when its transaction commits there,
and nothing else is written before its answer
([decision 0262](decisions/0262-postgres-alone-confirms-a-write-and-an-archive-is-copied-after-the-commit.md)).
Its copy is the repository's **archive**
([the archive](operations.md#the-archive)), which a background pass writes
behind the commits: the same entries as pieces of newline-delimited JSON,
one object per line, keys sorted, the checksum in `sum`
([pieces](stored-format.md#pieces)):

```json
{"actor":"6nk6ekkczdgh399nt2pqpccjz0","kind":"samples.substrate.reamde.dev/tasks/task","op":"put","payload":{"created":true,"fold":[{"…":"…"}],"properties":["name","dueAt"]},"recordId":"01jbx9m2qz7k4c5v8n3w6t1hrd","seq":4190,"sum":"sha256:5f0c…","token":"01jbx9k0token000000000000a","ts":"2026-08-04T10:00:00.183742Z","txn":4191}
```

`sum` is `sha256:` plus the hex digest of the same line with the `sum` key
absent, and the `hash` column and the wire field hold the same 32 bytes.
`causedBy` appears only on an entry a delivery caused, `onBehalfOf` only where
the actor acted for another and `token` only where a token stood behind the
write ([the stored format](stored-format.md#the-line)). `txn` is the seq of the
last entry of the transaction that appended the line: a transaction of one
entry has `txn` equal to `seq`, and a merge, which writes the loser's
tombstone and the winner's entry in one commit, gives both lines the second
one's seq. The `changelog.txn` column holds the same value. A piece holds
whole transactions, so a transaction never crosses two, and a reader holds
every line to its `sum`, its seq to its place and the piece to the seqs its
name says: a piece that is not that run is a named refusal, never a tail to
cut.

Once a write is ready to commit, a client that disconnects no longer stops
it: the server gives the commit a 30-second budget of its own. A commit
whose answer is lost may have applied, and the error says so
([what a client may conclude](api.md#what-a-client-may-conclude-after-a-failure)).
For a record write that is the whole of it. A vocabulary change whose
commit answer is lost leaves the server unsure which declarations it holds,
so the repository refuses every write with `503 unavailable` and a message
that names the restart, until the server restarts and loads what Postgres
committed.

Every line carries `txn`; one without it is a named refusal
([decision 0057](decisions/0057-a-changelog-line-names-its-transaction-and-an-unfinished-one-is-cut-whole.md)).

What the checksum proves: an entry is undamaged, in the table and in the
archive, and the two agree. What it does not prove: authenticity. Whoever
can write the database or the archive can rewrite a line and its `sum`
together, and no checksum sees that. The `hash` a consumer reads on the wire
is a **receipt** for the entry it came with, not a proof: the wire payload is
redacted, so the consumer cannot recompute it, and checking a receipt means
running `repository verify` on the repository.

## The dialect a changelog is written in

Each repository carries a **changelog dialect**: a monotonic integer naming the
ops and fold effects a binary must understand to replay its entries. The
effects are the [stored format](stored-format.md), never the wire
([the change event](#the-change-event) is what a reader gets), so the dialect
names what a binary replays, never what a client parses. The claim
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
7 today, the ops, fold effects and entry frame this release writes: the
intent to erase a record's history, which is what dialect 7 added, every
property's mark held as its own `mark` and `unmark` effects
([decision 0284](decisions/0284-a-propertys-mark-stores-whether-sync-may-replace-it-apart-from-where-its-value-came-from.md)),
which is what dialect 5 added, the state entries a compaction writes, which
is what dialect 6 added, every
entry linking its actor, the one it acted for and its token as records
([decision 0270](decisions/0270-every-hand-that-writes-is-an-actor-record.md)),
which is what dialect 3 added, the state entries a compaction writes, which
is what dialect 4 added, the record
ops and the `delivery` op with the seven effects a trigger's bookkeeping
replays through
([decision 0064](decisions/0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md)),
every entry naming its transaction
([decision 0057](decisions/0057-a-changelog-line-names-its-transaction-and-an-unfinished-one-is-cut-whole.md)),
every `record` delta carrying the kind version that wrote the row
([decision 0060](decisions/0060-a-record-carries-the-kind-version-that-last-wrote-it.md))
and every `manager` effect carrying the stamp a property rename moved with it
([decision 0063](decisions/0063-a-property-rename-is-ordinary-record-writes.md)).
Dialect 2 spells nothing dialect 1 did not: it is a history
[conversion step 2](stored-format.md#conversions) has emptied of the recovery
wrap. Dialect 4 carries a record's `body` among its properties, never as a
delta field of its own
([decision 0273](decisions/0273-body-is-an-ordinary-property-and-no-property-name-is-stored-apart.md)).
Dialect 6 adds the two state entries a compaction writes, `state` and
`ledger`
([decision 0296](decisions/0296-a-records-history-is-compacted-into-a-state-entry-in-the-line-of-its-newest-write.md)).
The next number is spent when a writer learns a spelling an older binary's
fold would refuse or misread, and a change to a shape the history already
holds ships a [conversion step](stored-format.md#conversions) to it, which
the boot check runs once over each repository below it.
No dialect is on the wire at all ([discovery](api.md#discovery)), and neither
are the entries written in one. The archive carries it, as `changelogDialect`
in its manifest and as the directory its pieces sit under, so an archive never
holds lines in a dialect its manifest understates
([the stored format](stored-format.md#the-manifest)).

`repository rebuild` reads the stamp again, under the changelog lock and
before it touches anything: it replays every entry, and a process that opened
the repository before another raised the stamp would be interpreting entries
in a spelling it does not know.

A repository written by a newer binary cannot be served by an older one again:
downgrading means restoring the copy taken before the upgrade, the same as for
the vocabulary dialect
([upgrading the binary](operations.md#upgrading-the-binary)).

## Watching

The records route, narrowed to a set of kinds, and the changelog itself
stream with `?watch=1`:
newline-delimited JSON (`application/x-ndjson`), opened with a bookmark you can
resume from, so a consumer that disconnects misses nothing:

```http
GET /api/v1/changes?from=4189&generation=7f3a0c2e9b1d4e6f&watch=1

{"bookmark": 4189, "generation": "7f3a0c2e9b1d4e6f"}
{"seq": 4190, "op": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
 "recordId": "01jbx9m2qz7k4c5v8n3w6t1hrd", "actor": "6nk6ekkczdgh399nt2pqpccjz0",
 "token": "01jbx9k0token000000000000a",
 "affected": [{"kind": "samples.substrate.reamde.dev/tasks/task", "id": "01jbx9m2qz7k4c5v8n3w6t1hrd", "version": 1}]}
```

Without `from`, the stream opens at the current head and tails forward; the
bookmark is the seq it opened at and the generation that seq belongs to, so
remembering the pair is all a resume needs.

The **history generation** is an opaque string the server mints for a
repository when it registers, and mints again when its history is restored
into a database that holds no row for it, which is what a
[restore](operations.md#backups) into a fresh database is. A database dump
keeps the row and its generation, so a restore that starts from a dump runs
`substratectl repository rotate-generation` once per repository. A restart and
`repository rebuild` keep it. A seq is a position in one generation only: an older copy of the archive
restored into an emptied database restarts the numbering below whatever
cursors clients saved, and a bare seq cannot tell the two histories apart. So
`from` or `before` above 0 carries the `generation` it was read under, and the server
refuses a cursor under another generation, above the head, or with no
generation at all, with the `compacted` error [below](#frames-and-the-horizon).
`from=0` and `before=0` name no entry, so they need none.

The same endpoint pages backward through history with `before=`, and filters
the same way in watch and history modes alike. Every filter parameter is plural
and takes a repeated parameter or a comma-separated list: `kinds`, `ops`,
`actors`, and their negations `excludeKinds`, `excludeOps`, `excludeActors`.
`actors` and `excludeActors` take [actor record](api.md#actors) ids, and
match a row by its `actor`.
`q` matches free text across the row's kind, actor, record id and payload
text. `values=1` narrows nothing: it adds each record's
[before and after values](#values-on-request) to the rows. Scoping the feed to one record takes **both** `recordId` and
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
on it at all. A `GET /api/v1/records?watch=1` tail stays plain rows.

This is the other half of the list-to-watch handoff: a list response carries
the changelog `head` seq at its snapshot and the `generation` it belongs to, so
listing and then opening `watch?from={head}&generation={generation}`
misses nothing and double-sees nothing.

### Run summaries

`runs=1` turns the history page into run summaries: consecutive rows of the
filtered feed that share an actor, a kind, a verb and a token, each with its
count, so a client says "added 60 tasks" from one read
([0126](decisions/0126-the-changes-read-summarizes-runs-and-never-ends-a-page-inside-one.md)).
The verb is `create` for a `put` whose payload carries `created: true`,
`restore` for one carrying `restored: true`, `update` for any other `put` and
every `patch`, and the op itself for `delete`, `merge`, `split` and `gc`.
Every filter above applies first, so a row the filter drops never breaks a
run.

```
GET /api/v1/changes?runs=1&first=2

{"runs": [
  {"actor": "5fz1hzkw85g0a9ytnkzrj7tzth", "kind": "samples.substrate.reamde.dev/tasks/task", "verb": "update",
   "count": 3, "records": 1, "recordId": "01jbx9m2qz7k4c5v8n3w6t1hrd",
   "newestSeq": 4190, "oldestSeq": 4188, "newestTs": "…", "oldestTs": "…",
   "token": "01jbx9k0token000000000000c", "tokenName": "Chat"},
  {"actor": "6nk6ekkczdgh399nt2pqpccjz0", "kind": "samples.substrate.reamde.dev/tasks/task", "verb": "create",
   "count": 60, "records": 60,
   "newestSeq": 4187, "oldestSeq": 4128, "newestTs": "…", "oldestTs": "…",
   "token": "01jbx9k0token000000000000a", "tokenName": "laptop"}],
 "cursor": 4128, "head": 4190, "generation": "7f3a0c2e9b1d4e6f"}
```

A run splits on the token, so two of the person's sign-ins are two runs, and
so are two assistants on one device, which are one local app's actor
([0230](decisions/0230-an-assistant-reaches-a-substrate-through-mcp-with-a-token-that-works-there-alone.md)).
On the owner's reads a run whose writes stood behind a token carries it:
`token`, its id, and `tokenName`, the app's claimed name or the token's
label, a revoked token's included. An app's and an assistant's reads carry
neither.

`first` counts runs (default 50, at most 500), and a page never ends inside
one: the server reads each run to the row that breaks it, so the oldest run's
`count` is exact. `records` is the distinct record ids the run touched, and
`recordId` is set when that is one. `cursor` is the oldest run's `oldestSeq`
when more rows lie below, passed as the next `before` with the `generation`,
and absent at the bottom. A run's time span is `newestTs` to `oldestTs`; the
server does not split a run by time. `runs` takes no `watch` and no `from`,
and a value other than `1` is a `bad_request`. The read costs what its runs
hold: a run of 50,000 rows is 50,000 rows read.

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

A stream also ends cleanly, with no error frame, when the server shuts down:
every row it sent is a row the client holds, so the client resumes with the
`from` and `generation` it last read. A client that leaves a frame unread for
60 seconds is disconnected.

The changelog has a **horizon**: the oldest seq still resumable. Requesting `from=`
a seq below the horizon is a `compacted` error (HTTP 410), and so is a cursor
the changelog cannot verify: above the head, under another history generation,
or with no generation beside a `from` above 0. The problem object carries the
current `head` and `generation`, so a consumer that has fallen too far behind,
or resumes after a restore, is told plainly instead of silently missing rows,
and its handler is one it **must** have: re-list, then resume the watch from
the head and generation the response names.

The horizon is **each repository's own**, because what moves it is: the
[compaction pass](operations.md#the-compaction-pass) forgets a record purged
longer ago than `SUBSTRATE_DELETIONS_KEPT` (30 days by default), every entry
of it, and a consumer that resumed from below the line the deletion was on
would never hear of it. So the horizon moves to that line, a `from=` below it
is answered `410 compacted` ("seq N is below the repository's horizon H: a
deletion before it was forgotten"), and a history page names it as `horizon`
(revision 4 of the `changefeed` feature), absent while it is 0. A compaction
that replaces a record's entries with its state entry leaves the horizon
where it was: several changes arriving as one is harmless, because a client
fetches the record either way. No deletion past an enabled record trigger's
cursor is forgotten. A record trigger that is not enabled holds nothing
back: before the pass forgets a deletion past its cursor it starts the
trigger over from the beginning, once, so it meets each live record once in
its latest state and a forgotten one not at all, and a replay into the
stretch below the horizon starts from the beginning too. [API discovery](api.md#discovery) reports `changelog.horizon` for the
server as a whole, the floor below which no repository resumes, and it is 0:
a repository's horizon is read where it is used, on its own change pages and
in the refusal. Retention is a deployment **policy**, not a wire guarantee:
the wire promises a resume that misses no record from any seq at or above the
horizon, and the horizon is where policy lives.

## Who consumes it

- **Triggers** ride it: every record-sourced trigger owns a cursor, and each
  row is checked against every enabled trigger. The feed reports each trigger's
  stance on each row ([functions](functions.md)), and the trigger records
  where it stands as `delivery` entries in the same changelog, which the feed
  never shows.
- **Watchers**: the stream above, and `substratectl watch` is that stream in a
  terminal ([substratectl](substratectl.md)). Providers reconcile from it.
- **The console's History page** is the same feed, paged backward through
  history and filtered ([web console](console.md#history)).
- **`rebuild`** replays it from the table, which is what makes the fold
  disposable; the archive is its copy, and the thing a restore reads
  ([running a substrate](operations.md#backups)).

The to-do list, its GitHub feed, and anything you build next are all consumers
of one changelog.

Next: [bundles](bundles.md), the closures that install new kinds and the
code that reacts to this feed.
