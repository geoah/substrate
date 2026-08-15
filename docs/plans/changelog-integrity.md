# Plan: changelog entry hashes, and optionally signatures

Status: design hardened through two adversarial Codex reviews against the
code. The first pass's twelve findings are integrated and marked where they
changed the design; the second pass confirmed most resolutions and added four
more (a `caused_by` preimage collision, the epoch table's plane, mandatory
backfill provenance, and the durability of the canonical number form), all
integrated. This is a plan, not a contract: the code that lands is the
contract, and every shape here is a sketch the implementer holds to the house
rules.

## What this is

The changelog is the truth: an append-only, strictly sequential, per-repository
sequence that the records table folds. Today nothing attests to it. A row
altered by hand in Postgres, a corrupted restore, or a splice of one
repository's history into another is invisible: the fold replays whatever the
table holds and calls the result the truth.

This plan adds tamper evidence in two tiers:

1. **A hash chain**: every entry carries a SHA-256 hash over its own content
   and the previous entry's hash. Any in-place edit, reorder, insert or
   cross-repository splice breaks the chain at the first touched seq, and a
   verifier names it.
2. **Signatures** (later, optional, off by default): a per-repository Ed25519
   key signs each entry's hash, so an attacker with database access but no
   host credential key cannot rewrite history and quietly re-chain it.

## What the code says today (the constraints)

These are the facts the design has to live with, found in the tree:

- **One append path.** `appendChange` (internal/engine/rows.go) is the single
  `INSERT INTO changelog` in the tree, with `seq = max(seq)+1` computed under
  a per-repository transactional advisory lock (`changelogLockKey`) held to
  commit. Appends per repository are fully serialized, so a chain head is
  stable for the whole transaction.
- **The last entry mutates before commit.** `settleFold`
  (internal/engine/fold.go) appends late fold effects onto the LAST appended
  entry's payload via `jsonb_set`, after the insert, inside the same
  transaction. A hash finalized at insert time would be stale by commit.
- **Reseal is a sanctioned rewrite.** `ResealRepository`
  (internal/engine/reseal.go) rewrites historical payload bytes, values-only,
  to move legacy secrets into the sealed store. Its own comment calls it "the
  one sanctioned mutation of history". A hash chain must be re-computed over
  what reseal rewrote, and the design must say so out loud.
- **No operation deletes individual entries from a live repository.** The only
  statements that touch existing rows are `settleFold`'s payload merge and
  reseal's rewrite. Repository erasure and the orphan sweep drop a
  repository's WHOLE changelog with everything else
  (internal/engine/repositories.go, `repositoryScopedTables`), which is
  deletion of the repository, not of history within one.
- **Payload numbers are not floats.** Integer-typed properties coerce to
  `int64` (internal/engine/validate.go), `changeProps` embeds `int64` seqs in
  payloads (internal/engine/rows.go), and reseal decodes with `json.Number`
  precisely because float64 does not preserve stored numbers
  (`decodeNumberPreserving`, internal/engine/reseal.go). Postgres `jsonb`
  stores numbers as arbitrary-precision `numeric`. Any canonicalization based
  on ES6/IEEE-754 number rendering (RFC 8785 JCS) therefore has a domain
  mismatch with what this system actually stores. This killed the draft's JCS
  design; see "the canonical form" below.
- **`jsonb` normalizes.** Key order, whitespace and number lexemes are
  re-rendered by Postgres: the bytes Go inserts are not the bytes a verifier
  reads back. The hash must be over what was STORED, not over what was sent.
- **Timestamps already round-trip.** `nowUTC()` truncates to microseconds, the
  precision `timestamptz` stores, so `ts` read back equals `ts` written.
- **The wire is not the store.** `redactChangePayload`
  (internal/engine/changefeed.go) blanks sensitive values in the payload as it
  leaves the engine; the stored row is untouched. An external watch consumer
  cannot recompute entry hashes from what it receives. Full verification is a
  server-side (operator) act by design.
- **A rebuild replays, it never writes the changelog.** `changelogPage`
  (internal/engine/rebuild.go) already reads every entry `ORDER BY seq`, so
  chain verification can ride along, but it projects into `substrate.Change`,
  which drops `caused_by` and decodes the payload into float64 maps.
  Verification needs its own stored-entry read (raw `payload::text`,
  `caused_by`, `hash`, `sig`), not the public `Change` shape.
- **The credential wrap has a plaintext fallback.** `sealCredential`
  (internal/engine/credentials.go) emits plain framing when no host key is
  configured, and `openCredential` ACCEPTS plain framing even when one is.
  Fine for the DEK's threat model; fatal for a signing key, which exists
  precisely to resist a database-only attacker. Signing must refuse to enable,
  and refuse to load a key, without sealed framing.
- **Registration and erasure are the changelog's lifecycle.** The vocabulary
  upgrade pattern this plan borrows for backfill is per-repository work at
  FIRST OPEN in a process (`upgradeShippedVocabulary`,
  internal/engine/seed.go), not a global boot migration.

## Threat model, honestly

- **Hash chain alone** detects accidental corruption, a botched restore, and
  casual tampering (an `UPDATE` in psql). It does NOT stop an attacker with
  full database write access: they can rewrite an entry and re-chain
  everything after it, because the chain needs no secret. It also cannot
  detect truncation of the tail on its own; the head hash has to be compared
  against something remembered elsewhere (a client's last-seen head, a backup,
  a signed checkpoint).
- **Signatures** raise the bar to "database access AND the host credential
  key". An attacker with both is the host operator, and no in-database scheme
  defends against the party who runs the database. Likewise, a public key read
  out of the same mutable database proves only internal consistency: the trust
  anchor is the public key pinned OUTSIDE the database (printed at activation,
  saved by the operator) plus remembered heads. All of this gets documented,
  not hidden.
- **Out of scope**: user-held keys signing writes at the client. That is the
  "no keys, no signatures" stance of the model paragraph, and this plan keeps
  it: no user ever manages key material for this feature. What changes is that
  the word "unsigned" in the model paragraph stops being true and gets
  updated.

## Design

### The columns

```sql
ALTER TABLE changelog
  ADD COLUMN hash bytea CHECK (hash IS NULL OR octet_length(hash) = 32),
  ADD COLUMN sig  bytea CHECK (sig  IS NULL OR octet_length(sig)  = 64),
  ADD CONSTRAINT changelog_sig_needs_hash CHECK (sig IS NULL OR hash IS NOT NULL),
  ADD CONSTRAINT changelog_caused_by_prior CHECK (caused_by IS NULL OR (caused_by >= 1 AND caused_by < seq));
```

Nullable at birth for backfill; after a repository's backfill completes, a
NULL hash anywhere in it is a verification FAILURE, not a gap to shrug at
(see backfill). The CHECKs exist because the application role holds UPDATE on
the table: malformed lengths must be impossible to store, and the verifier
treats them as findings, never as library errors. The `caused_by` CHECK also
pins in the store an invariant the docs already promise (the cause is always
strictly smaller).

### The canonical form: hash what Postgres stored

The draft used RFC 8785 (JCS). That was wrong here: JCS renders numbers as
ES6 doubles, and this changelog holds `int64` properties, `json.Number`
lexemes and `numeric`-backed values a double cannot represent. Instead, the
canonical payload is derived FROM THE STORED VALUE, so round-trip identity
holds by construction:

- At write time, the entry's statement returns the stored rendering:
  `INSERT ... RETURNING seq, payload::text` (and the settle update likewise
  `RETURNING payload::text`).
- `canonicalJSON(text)`: decode with `json.Decoder.UseNumber()`, re-marshal
  with sorted map keys, one fixed string-escaping policy, and every number
  lexeme rewritten into a VALUE-EXACT decimal normal form (second review):
  sign, significant digits, and a normalized exponent, computed with integer
  string operations, never through a float. `1.50`, `1.5` and `15e-1` all
  canonicalize to the same bytes because they are the same value. Well under
  a hundred lines, stdlib only, frozen with test vectors, and the same
  function runs at write and at verify on input from the same source: the
  `jsonb` column's text rendering.

The value-exact normal form is what makes the format durable. Postgres
`numeric` guarantees the VALUE of a stored number; HOW `numeric_out` prints
it (display scale, trailing zeros) is a rendering detail a major upgrade
could in principle change, and a canonical form that depended on it would
strand every historical hash. Depending only on the value reduces the
assumption to "jsonb keeps storing numbers as exact decimals", which is its
documented data model. A round-trip test still covers adversarial inputs
(around ±2^53, long fractions, trailing zeros, exponent inputs, -0, values
inserted through raw SQL), and the operations doc still names a Postgres
major upgrade as a "run `verify` before and after" moment.

### The preimage and the chain

SHA-256 over a length-framed byte string, not over a JSON object, so no
parser ambiguity exists for the scalar fields:

```
frame := "substrate/changelog/v1" ||
         len(repository) || repository ||
         be64(seq)                      ||
         len(ts)         || ts          ||   -- RFC3339, UTC, microseconds, fixed form
         len(actor)      || actor       ||
         len(op)         || op          ||
         len(record_id)  || record_id   ||
         len(kind)       || kind        ||
         caused_by                      ||   -- 0x00 when NULL, else 0x01 || be64(value)
         len(canon)      || canon       ||   -- canonicalJSON(stored payload text)
         prev                                -- 32 raw bytes; zeros for seq 1
hash  := sha256(frame)
```

- Every `len(...)` is a fixed big-endian uint32 (second review: widths and
  endianness are part of the format, not an implementation choice).
- `caused_by` carries a presence byte (second review): encoding NULL as a
  bare zero would collide with a stored zero, letting an `UPDATE` flip one to
  the other without moving the hash. The CHECK above forbids zero anyway;
  the presence byte makes the preimage injective regardless.
- `repository` in the preimage makes a cross-repository splice fail even
  where seqs line up.
- The version tag makes a future preimage change (v2) explicit rather than a
  silent fork.
- Genesis: `prev` for seq 1 is 32 zero bytes. The chain is per repository.

### The write path

Hashes are stamped at settle time, when payloads are final:

- `appendChange` keeps its INSERT, gains `RETURNING payload::text`, and the
  transaction records per entry: seq, the stored payload text, and its
  position in the chain. The chain's `prev` for the transaction's first entry
  is read under the advisory lock (joining the query that already computes
  `max(seq)`); later entries chain off in-memory hashes.
- `settleFold` keeps its `jsonb_set` merge (one owner of the merge semantics
  is nice but not load-bearing once the hash is computed from the stored
  text), gains `RETURNING payload::text`, and replaces the recorded text for
  that one entry. Only the transaction's LAST entry (`t.maxSeq`) can be
  touched this way, so the recompute is local: no downstream hash exists yet.
- A new `settleChain` step, after `settleFold` and before commit, computes
  each recorded entry's hash in seq order and stamps it:
  `UPDATE changelog SET hash = $2 [, sig = $3] WHERE seq = $1`, checking
  `RowsAffected == 1` per statement.

Transaction-state invariants, explicit (review finding): the txn owns copies
of EVERY preimage field per recorded entry (seq, ts, actor, op, record id,
kind, caused_by, payload text; never a caller's mutable map), tracks the
in-transaction chain head, and retains the last entry's `prev` for the
settle-time recompute. Tests cover multiple appends in one transaction,
effects folded between appends, and late effects after the final append.

Cost: one SHA-256 (microseconds) plus one small UPDATE per entry, and, when
signing is on, one Ed25519 signature (~30 microseconds). The advisory lock
already serializes appends per repository, so no new contention appears.

### Verification

- `bin/substratectl --dsn "$DATABASE_URL" repository verify <username>`:
  operator hat, walks the chain start to head over the STORED shape (raw
  payload text, `caused_by`, `hash`, `sig`), recomputes every hash, checks
  every signature the signing state requires (below), and reports either the
  first finding or the verified head `(seq, hash)` plus coverage. Findings
  are a closed taxonomy, each named: hash mismatch at seq N, interior NULL
  hash, malformed hash/sig length, seq gap (seqs are `max+1`, so any gap is a
  finding), missing signature after activation, bad signature, unhashed
  legacy prefix (before backfill), rechain epoch present (informational, see
  reseal). It never repairs and never backfills.
- `RebuildRepository` verifies BEFORE it folds: pass one walks the chain
  read-only (it must page the stored shape anyway), pass two clears and
  replays. Tampered history refuses to become the live fold. An explicit
  `--force-unverified` (separate, unmistakable flag; interface change to the
  operator seam noted) rebuilds anyway, returns the first finding, and the
  report states the fold is built from unverified history.
- The verified head `(seq, hash)` is the thing worth writing down elsewhere.
  Comparing heads across time is what turns "internally consistent" into
  "the same history I saw yesterday".

### Backfill

Per repository, at FIRST OPEN under the new binary (the real vocabulary
upgrade pattern), before the repository serves writes: oldest-first, chunked
into bounded transactions, each chunk under the repository's changelog lock,
resumable at the first NULL hash (the chain makes hashed rows a prefix and
NULLs a suffix by construction). Blocking that repository's first open is the
price of never having an unhashed head to chain onto; the cost is I/O-bound,
not hash-bound (review finding: the draft's "seconds" claimed the hash cost
and ignored the row churn), so the open logs progress per chunk and the
operations doc says a large history takes proportional time once.

Two stated deployment assumptions, documented rather than engineered around:

- **One writer process per database.** That is this system's deployment shape
  (one `substrated`). A rolling two-version deployment could let an old
  binary append unhashed rows after a new one backfilled; the verifier
  reports exactly that (interior NULLs), and the operations doc says: stop
  the old binary first.
- Backfilled hashes attest forward from the moment of backfill, nothing more.
  If history was already tampered with, the backfill notarizes the tampered
  bytes. Because a backfilled hash is byte-identical to a contemporaneous one,
  the backfill MUST record its own `chain_epochs` row (second review: this
  was an open question; it is not optional): reason `backfill`, `old_head`
  absent, `new_head` the head it produced. `verify` reports where attested
  history begins from that row rather than pretending the past was witnessed.

### Reseal, and rechain epochs

Reseal already runs as one transaction holding the changelog lock. It gains
one step: after rewriting payloads, it re-chains from the first rewritten seq
to the head. Because that invalidates every downstream head or receipt anyone
remembered (review finding: an ephemeral report field cannot tell a later
verifier "sanctioned rewrite" from "attack"), the transition is made durable:
a `chain_epochs` table records `(at, from_seq, old_head, new_head, reason)`,
written in the reseal transaction. The table is REPOSITORY-SCOPED (second
review): the reseal transaction runs on the RLS'd application pool, which
cannot atomically reach a control-plane table, so epochs carry a
`repository` column, join `repositoryScopedTables` (RLS, erasure, the orphan
sweep, the residue check), and live in the user plane like the history they
describe. `verify` lists epochs, and a remembered head that matches an
epoch's `old_head` is explained; one that matches nothing is not. A receipt
for an INDIVIDUAL pre-reseal entry is gone for good; the epoch explains the
break, it cannot reconcile per-entry receipts, and the docs say so. When the
repository has signed entries, reseal re-signs everything it re-chained, and
REFUSES to run if the signing key is unavailable (a reseal that strips
signature validity is not sanctioned). Epoch rows are signed too, when
signing is on.

This stays the design's one honest concession: the party who may rewrite
history (the operator, holding the credential key) is the same party the
scheme cannot defend against. The epoch makes the rewrite auditable instead
of silent.

### Signatures (later phase, off by default)

- **Key**: per-repository Ed25519, minted at activation exactly like
  `adoptDEK` (compare-and-swap on NULL) into `repositories.signing_key`,
  wrapped under the host credential key, with two hard rules the DEK does not
  have (review finding): activation REFUSES without a configured credential
  key (no plain framing is ever written for a signing key), and the loader
  REFUSES a plain-framed or wrong-length key rather than falling back. The
  public key is printed once at activation for out-of-band pinning, readable
  via the operator hat, and exposed on the repository.
- **Activation is durable and one-way** (review finding: otherwise
  `UPDATE changelog SET sig = NULL` is an undetectable downgrade). The
  repository row records `signed_from_seq`. From that seq on, a missing or
  invalid signature is a verification FAILURE, and the engine refuses to
  append unsigned (a lost credential key stops writes rather than silently
  shedding the guarantee; recovery is an explicit operator command that
  records a chain epoch). The environment toggle
  (`SUBSTRATE_CHANGELOG_SIGNING=true`) only selects whether repositories
  activate at next open; it never deactivates. `signed_from_seq` alone is
  still mutable database state (second review), so activation also writes a
  SIGNED `chain_epochs` row (reason `activate`, carrying the public key and
  `signed_from_seq`), and the line printed at activation for out-of-band
  pinning is the pair `(public key, signed_from_seq)`, not the key alone. A
  database attacker can rewrite all of it together; the pinned pair is what
  catches them.
- **What is signed**: the entry hash. `sig = ed25519(key, hash)`. Signing the
  hash keeps verification two-phase: chain first (no key needed), signatures
  second (public key only).
- **Not chosen, and why**:
  - HMAC-SHA256 keyed from the DEK: verification requires the secret, so a
    dump-plus-public-key audit is impossible.
  - A host-wide signing key: one key for every repository breaks the
    per-repository plane separation for no gain.
  - Signed checkpoints only (sign every Nth head): per-entry cost is already
    negligible, and checkpoints add a second artifact. A checkpoint endpoint
    can still be added later on top for external anchoring.

### The wire

`Change` gains `hash` as a HEX STRING field (a Go `[]byte` would marshal as
base64; the wire field is `string`, omitted while a legacy entry is still
unhashed). `sig` stays off the wire until a consumer exists; the payload is
redacted on the wire anyway, so no external consumer can fully verify today,
and the hash rides as a receipt checkable against `verify` output. The
receipt caveat from the reseal section applies and is documented: a receipt
outlives its epoch only as an entry in the epoch record.

Wire mechanics (review finding: the draft's procedure was wrong): `Change` is
NOT currently in `wireTypes` (internal/substrate/wire_test.go), and the
console's `ChangeRow` sits outside the golden guard. Adding the field means
adding `Change` to `wireTypes`, regenerating `wire.golden.json`, and holding
`ChangeRow` in `types.ts` to it. If the public key lands on `RepositoryInfo`,
that shape enters the golden set too.

### Docs and contract

- `docs/changelog.md` gains a section: what the hash covers, what a verifier
  can and cannot conclude, epochs, the operator `verify` command.
- `docs/operations.md`: backfill expectations, the Postgres-upgrade
  checkpoint, the single-writer assumption, key activation and pinning.
- The model paragraph's "unsigned" (CLAUDE.md, and anywhere the docs repeat
  it) is updated: the changelog is hashed, optionally signed by the server,
  and still free of user-managed keys.
- `docs/terms.md` needs no new words: "hash", "signature", "chain" and
  "epoch" are used in their plain senses.

## Libraries

- **SHA-256, Ed25519**: `crypto/sha256`, `crypto/ed25519`. Stdlib.
- **Canonical JSON**: none needed; `canonicalJSON` is ~20 lines of stdlib
  (`UseNumber` decode, sorted-key marshal) over Postgres's own rendering.
- **Considered and rejected**: RFC 8785 JCS
  (`github.com/cyberphone/json-canonicalization`), rejected for the numeric
  domain mismatch above, not for the dependency. `golang.org/x/mod/sumdb/tlog`
  (Merkle tree, O(log n) inclusion and consistency proofs) and
  transparency-style designs (Trillian, sigsum) earn their weight when an
  untrusted party needs efficient proofs over someone else's history, for
  example future substrate-to-substrate sync; a linear chain is the right
  size for one repository verified by its own operator, and the entries stay
  the truth, so a tree can always be computed later.
  `golang.org/x/mod/sumdb/note` is the natural checkpoint format if external
  anchoring is ever added.

## Phases

Smaller than the draft's two (review finding), each independently green:

1. **Preimage and codec**: `canonicalJSON`, the frame, adversarial codec
   tests (raw-SQL-inserted numbers included). No schema change.
2. **Schema and chain writes**: migration 0005 (columns + CHECKs),
   `appendChange`/`settleFold` RETURNING, `settleChain`, tests for
   multi-append transactions and late effects.
3. **Backfill and verify**: first-open backfill, `repository verify`, the
   finding taxonomy, reseal re-chain with `chain_epochs`.
4. **Enforcement and wire**: rebuild verifies-then-folds (`--force-unverified`
   escape hatch), `Change.hash` on the wire with the golden dance, docs.
5. **Signatures**: activation, `signed_from_seq`, key rules, verify coverage,
   public-key read path, docs. Only after 1 through 4 have settled.

Test matrix beyond unit tests (review finding: the fuzz test alone is too
narrow): db tests for every preimage field's tamper detection, payload
mutation, row deletion, seq gaps, reordering, tail truncation, interrupted
and resumed backfill, concurrent append during chunked backfill, reseal
invalidating a previously observed head and recording the epoch, signature
removal, unsigned append after activation, malformed key material.

## Open questions

1. Should `verify` also live behind the user hat (an API route returning the
   head, coverage and epochs, not a full re-walk)? Cheap and useful for the
   console; deferred unless wanted now.
2. Does the console surface the head anywhere (an "integrity" line on a
   settings page)? Cosmetic, later at the earliest.
3. `repository verify` as its own verb (proposed: rebuild rewrites the fold,
   verify promises read-only), or folded into rebuild flags?
4. Whether to accept the rolling-deploy hazard permanently (the second review
   notes it is stated, not solved). The single-writer deployment shape makes
   it theoretical today; a minimum-writer-version gate in the schema is the
   known fix if that ever changes.
