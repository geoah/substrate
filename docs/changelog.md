# The changelog and watch

Every committed write appends one entry to the repository's **changelog**, in the
same transaction as the write itself. It is append-only, strictly sequential,
and there is exactly one of it per repository: the source of truth, the change
feed, the audit trail, and the input stream for anything that learns are all
the same changelog. The [records](data-model.md) you read are its fold. Identical
re-writes append nothing, so a re-sync leaves no wake.

One entry, as the wire carries it, for the task created on the
[API page](api.md):

```json
{"seq": 4190,
 "ts": "2026-08-04T10:00:00.183742Z",
 "actor": "api",
 "op": "put",
 "kind": "tasks.substrate.reamde.dev/task",
 "recordId": "kq3v9x2m41pf",
 "payload": {"created": true, "properties": ["title", "dueAt"]}}
```

`actor` names what wrote ([the actor domain](api.md#actors) is closed and
flat), `op` is the mutation that made the row (`put`, `patch`, `delete`,
`link`, `unlink`, `merge`, `split`, plus the engine's own housekeeping), and
`kind` plus `recordId` are the record's full identity. The readable half of
`payload` names what changed rather than repeating it: `created` on first
write, `restored` on an undelete, the list of accepted `properties`, the
resulting `states` when a transition moved one.

Beneath that readable half, the stored payload also carries the write's
**values**, which is what makes the changelog replayable rather than merely
informative: `substratectl repository rebuild` clears the fold and replays the whole
changelog through the same code a live write uses, reproducing the records bit for
bit and appending nothing. Secret-typed values are sealed before they reach the
changelog, and every read surface — REST, GraphQL, and this feed — renders them
`<redacted>`.

Two guarantees consumers may lean on:

- **Sequence order is commit-visibility order**: no reader ever sees entry N
  with N-1 still uncommitted, so resuming from a remembered `seq` misses
  nothing. Gapless resume is a guarantee, not a convention, and it survives a
  restart because the counter is the repository's own `max(seq)`, never process
  state.
- **Causality terminates.** Every delivery-authored entry records the seq that
  caused it, always a strictly smaller number, so a causal chain is finite by
  construction and the engine parks a chain that runs deeper than its cap (16)
  rather than spinning. The link is stored, not published: a change on the wire
  carries `seq`, `ts`, `actor`, `op`, `kind`, `recordId`, and `payload`, and
  nothing else.

## Watching

Any collection, and the changelog itself, streams with `?watch=1`:
newline-delimited JSON (`application/x-ndjson`), opened with a bookmark you can
resume from, so a consumer that disconnects misses nothing:

```http
GET /api/v1/core.substrate.reamde.dev/changes?from=4189&watch=1

{"bookmark": 4189}
{"seq": 4190, "op": "put", "kind": "tasks.substrate.reamde.dev/task",
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
its handler is one it MUST have: re-list, then resume the watch from the fresh
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
- **`rebuild`** replays it, which is what makes the fold disposable and the changelog
  the thing you actually back up ([running a substrate](operations.md)).

The to-do list, its GitHub feed, and anything you build next are all consumers
of one changelog.

Next: [bundles](bundles.md), the closures that install new kinds and the
code that reacts to this feed.
