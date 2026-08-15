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
 "kind": "tasks.substrate.reamde.dev/task",
 "recordId": "kq3v9x2m41pf",
 "payload": {"created": true, "properties": ["title", "dueAt"]},
 "hash": "5f0c…64 hex chars…9a1d"}
```

`actor` names what wrote ([the actor domain](api.md#actors) is closed and
flat) and is the caller's own claim; the entry also stores a **principal**
beside it, the id of the token the door resolved from the bearer secret,
which no caller can name. Two tokens writing as `api` are one actor and two
principals. The principal is stored and hashed, not published: it stays out of
the wire shape below, and `substratectl repository verify` walks it with the
rest of the entry. A write no token stands behind — the seed, the boot
upgrade, a sync worker — carries an empty principal. `op` is the mutation that
made the row (`put`, `patch`, `delete`,
`link`, `unlink`, `merge`, `split`, plus the engine's own housekeeping), and
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

## The chain

Every entry carries a SHA-256 **hash** over its own stored content and the
previous entry's hash, stamped by the writing transaction. An in-place edit, a
reorder, an insert or a splice of one repository's history into another breaks
the chain at the first touched seq, and `substratectl repository verify` names
it. The hash covers what Postgres stored — the verifier recomputes it from the
same bytes later — with the payload's numbers canonicalized by VALUE, so how a
Postgres version happens to render a number can never strand a historical
hash.

Every repository also **signs** every entry — signing is mandatory. Each
repository mints its own Ed25519 key at its first open (sealed under
`SUBSTRATE_CREDENTIAL_KEY`, which the server refuses to boot without): a
brand-new repository is signed from seq 1, and a store upgraded from an
earlier release activates on its first open after the backfill. Activation is
durable and one-way: from the activation seq forward, a placeholder or
invalid signature is a verification failure, and a host that cannot sign
refuses to append rather than quietly shedding the guarantee. The activation
moment logs the `(public key, signed_from_seq)` pair — pin it outside the
database; it is what a verifier ultimately trusts.

Registration hands the repository's signing seed to the user exactly once:
the registration response carries it in hex beside the recovery key,
`substratectl register` offers it the same 1Password save, and the console
shows it once. This is disclosure, not client-side signing
([decision 0010](decisions/0010-signing-is-per-repository-ed25519-one-way.md)
keeps the server the only signer): the server's own copy stays sealed under
the credential key, and no later call can produce the seed again. Holding it,
the user can derive the public key and check the signatures on a dump or a
backup with no server involved. The copy also widens who can sign: the seed
plus database write access forges history exactly as the credential key
does, so it belongs in a password manager, guarded like the recovery key
beside it. A possible follow-up, not built: an operator
path that re-seals a user-provided seed after a lost credential key, which
today stops writes with no recovery.

Two placeholder values exist, and both are hashed like any other value, so
neither can be edited later without breaking the chain. Entries written
before signing existed keep the **all-zero signature** forever — an
append-only log cannot be signed after the fact, so `verify` counts them
(`placeholderSigs`) below the activation seq and names them as findings at or
after it. Entries written before the door threaded the **principal** keep the
string `invalid`, which no write path stamps any more. A keyless host may
run ONLY under `SUBSTRATE_INSECURE_ALLOW_INVALID_SIGNATURES=true`, never
activates signing, writes placeholder signatures on everything, and `verify`
names that state as a finding. The placeholders and the switch are pre-v1
scaffolding, tracked to be removed before v1
([#175](https://github.com/geoah/substrate/issues/175)).

What this proves, honestly:

- **The hash chain alone** catches accidental corruption, a botched restore,
  and casual tampering. It does not stop an attacker with full database write
  access, who can rewrite an entry and re-chain everything after it: the
  chain needs no secret. It also cannot see a truncated tail by itself — only
  a **remembered head** can, which is why `verify` prints the head
  `(seq, hash)`, the operations doc tells you to write it down, and
  `verify --expect-head seq:hash` turns the comparison into an enforced
  finding instead of an eyeballed one.
- **Signatures** raise the bar to "database access AND the credential key" —
  PROVIDED the verifier is pinned: everything in the database, the public
  key included, is rewritable by whoever holds the database, so an unpinned
  verify proves internal consistency and a pinned one
  (`--expect-public-key`, `--expect-signed-from`) proves it against what you
  knew. Whoever holds both the database and the credential key is the host
  operator, and no in-database scheme defends against the party who runs the
  database.
- The `hash` on the wire is a **receipt**, not a proof: the wire payload is
  redacted, so a consumer cannot recompute it. Checking a receipt means
  handing it to `repository verify --expect-head`.

Three events legitimately move or begin the chain, and each records a **chain
epoch** the verifier CHECKS as well as lists: the **backfill** that stamps
history written before the chain existed (at the repository's first open
under a chain-aware binary, atomically with its hashes), a **reseal**'s
sanctioned rewrite (which VERIFIES FIRST — a reseal over tampered history
refuses rather than laundering it — then re-chains and, when signing is on,
re-signs everything after the first rewritten entry), and signing
**activation** (whose epoch must be signed and agree with the durable mark,
or it is a finding). A pinned head that stopped matching either matches a
reseal epoch's recorded old head — reported, so you re-pin — or it is a
plain finding.

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
- **`rebuild`** replays it, which is what makes the fold disposable and the changelog
  the thing you actually back up ([running a substrate](operations.md)).

The to-do list, its GitHub feed, and anything you build next are all consumers
of one changelog.

Next: [bundles](bundles.md), the closures that install new kinds and the
code that reacts to this feed.
