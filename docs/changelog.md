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
`kind` plus `recordId` are the record's full identity. The readable half of
`payload` names what changed rather than repeating it: `created` on first
write, `restored` on an undelete, the list of accepted `properties`, the
resulting `states` when a transition moved one.

Beneath that readable half, the stored payload also carries the write's
**values**, which is what makes the changelog replayable rather than merely
informative: `substratectl repository rebuild` replays the changelog through
the same fold code ([running a substrate](operations.md#operator-recovery)). A secret-typed value never reaches the changelog at
all: the delta carries an opaque ref into the sealed store, the material lives
there encrypted, and every read surface — REST, GraphQL, and this feed —
renders the property `<redacted>`.

Two guarantees consumers may lean on:

- **Sequence order is commit-visibility order**: no reader ever sees entry N
  with N-1 still uncommitted, so resuming from a remembered `seq` misses
  nothing. Gapless resume is a guarantee, not a convention, and it survives a
  restart because the counter is the repository's own `max(seq)`, never process
  state.
- **Causal chains are finite.** Every delivery-authored entry records the seq
  that caused it, always strictly smaller, so a chain cannot loop, and the
  engine parks a chain deeper than its cap (16) rather than spinning. The link is stored, not published: a change on the wire
  carries `seq`, `ts`, `actor`, `op`, `kind`, `recordId`, `payload`, and the
  entry's `hash` (below), and nothing else.

## Change verbs

`op` above is the changelog's own word for a write. Three other properties name
the same act in their own vocabulary, and a reader who assumes they agree
writes a rule that matches nothing. Here is one act across all four:

| The act                       | Changelog `op`                                     | `recordpatchpolicy.selector.ops` | `recordpatchrequest.op` | `trigger.source.record.ops` |
| ----------------------------- | -------------------------------------------------- | -------------------------------- | ----------------------- | --------------------------- |
| a record comes into existence | `put`, with `created: true` in the payload         | `put`, `patch`                   | `create`                | `create`                    |
| an existing record changes    | `put`, `patch`, `merge`, `split`                   | `put`, `patch`                   | `patch`                 | `update`                    |
| a record goes away            | `delete`, and `gc` on the collector's pass         | `delete`                         | `delete`                | `delete`                    |

Merge and split are the two rows the table cannot hold. A merge tombstones the
loser under the winner's single `merge` entry, so `OpOf` reads that entry as an
`update` on the winner and no trigger delivers a delete class for the loser
(reads of the loser's id forward to the winner, so nothing is lost). An accepted
split reverses it, restoring the loser under one `split` entry, again read as an
`update`, so a record can return to existence with no create class delivered.

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
{"actor":"api","kind":"samples.substrate.reamde.dev/tasks/task","op":"put","payload":{"created":true,"properties":["name","dueAt"],"values":{"…":"…"}},"principal":"k7…","recordId":"kq3v9x2m41pf","seq":4190,"sum":"sha256:5f0c…","ts":"2026-08-04T10:00:00.183742Z"}
```

`sum` is `sha256:` plus the hex digest of the same line with the `sum` key
absent, and the `hash` column and the wire field hold the same 32 bytes.
`causedBy` appears only on an entry a delivery caused. A segment is named by
the seq of its first line (`000000000000001.ndjson`); the highest-numbered one
is the active segment and grows. When it passes
`SUBSTRATE_CHANGELOG_SEGMENT_BYTES` (256 MiB by default) the writer finishes
it with a `.sha256` sidecar holding the digest of the whole file and opens the
next. A finished segment never changes.

Postgres commits first and the file follows, so a crash can leave the file one
transaction behind; the server appends the missing entries at the next boot
([what happens at boot](operations.md#what-happens-at-boot)). A reader accepts
exactly one kind of damage, a torn final line in the active segment, which it
discards. A bad `sum`, a seq that does not follow the previous one, or a
finished segment whose sidecar does not match is a named refusal, not a
repair.

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
ops and fold effects a binary must understand to replay its entries. The claim
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
1 today, and a repository's stored dialect is never on the wire: what
[API discovery](api.md#discovery) reports is the binary's maximum.

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
GET /api/v1/changes?from=4189&watch=1

{"bookmark": 4189}
{"seq": 4190, "op": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
 "recordId": "kq3v9x2m41pf", "actor": "api"}
```

Without `from`, the stream opens at the current head and tails forward; the
bookmark is the seq it opened at, so remembering it is all a resume needs.

The same endpoint pages backward through history with `before=`, and filters
the same way in watch and history modes alike. Every filter parameter is plural
and takes a repeated parameter or a comma-separated list: `kinds`, `ops`,
`actors`, and their negations `excludeKinds`, `excludeOps`, `excludeActors`.
`q` matches free text across the row's kind, actor, record id and payload
text. Scoping the feed to one record takes **both** `recordId` and
`recordKind`, because an id alone names no record; either one without the
other is a `bad_request`. A singular guess (`kind=`, `op=`, `actor=`) is
refused naming the plural rather than silently answering with the whole
unfiltered feed.

History returns one JSON body (`{"changes": […], "cursor": <seq>}`)
newest-first; `cursor` is the continuation, the oldest seq the page consumed,
which you pass as the next `before` (omitted when the walk is exhausted), so
the client never computes the boundary itself. The watch resumes from a
transparent `from={seq}`. (Note the continuation rule from
[the API](api.md#pagination): the changelog uses transparent `from` and `before`
seqs, because a seq is a real ordinal; opaque `after` cursors are for list
pagination.)

Every row this endpoint returns also carries `triggers`: each runnable enabled
[trigger](functions.md#triggers)'s stance on that row — `pending`,
`processed`, or `parked` with its error — omitted where the trigger cannot fire
on it at all. Per-collection watches stay plain rows.

This is the other half of the list-to-watch handoff: a list response carries
the changelog `head` seq at its snapshot, so paging a collection and then opening
`watch?from={head}` misses nothing and double-sees nothing.

## Frames and the horizon

The ndjson framing is pinned so a client can parse a stream unambiguously:

- A line **with** a `seq` is a change row.
- A line **without** a `seq` is a **control frame**, keyed by its single key.
  The opening `{"bookmark": N}` is one such frame, and the idle heartbeat `{}`,
  sent every 30 seconds, is another. The stream may also end with a **terminal
  error frame** carrying the one [problem object](api.md#errors), so a
  mid-stream failure is legible rather than a dropped connection:

```json
{"error": {"code": "internal", "message": "…"}}
```

The changelog has a **horizon**: the oldest seq still resumable. Requesting `from=`
a seq below the horizon is a `compacted` error (HTTP 410), so a consumer that
has fallen too far behind is told plainly instead of silently missing rows, and
its handler is one it **must** have: re-list, then resume the watch from the fresh
head. The horizon is reported in [API discovery](api.md#discovery), and it is
**0 today**: nothing prunes or compacts the changelog, so replaying any consumer from
any seq, zero included, is possible. Retention is a deployment **policy**, not
a wire guarantee: the wire promises gapless resume from any seq at or above the
horizon, and the horizon is where policy lives.

## Who consumes it

- **Triggers** ride it: every record-sourced trigger owns a cursor, and each
  row is checked against every enabled trigger. The feed reports each trigger's
  stance on each row ([functions](functions.md)).
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
