# Sealed store review: keys, sealing, secret read paths, write path, recovery

Date: 2026-08-17. Scope: the sealed store as one system, for
[#99](https://github.com/geoah/substrate/issues/99), read against `main` at
`1c134b4`. Method: one reader, working through the five areas #99 names,
reading the code rather than the docs. Every finding below says whether it was
**verified** by reading the code path end to end or is **unconfirmed**.

Nothing here is fixed in this branch. Each finding is filed as its own issue,
listed in [Findings and their issues](#findings-and-their-issues).

A second pass re-checked every load-bearing claim against the code and
confirmed each finding's mechanism. It corrected three things, now folded in:
`rekeySealedStore` does break under the AAD binding (2.1),
`core.substrate.reamde.dev/credential` is a shipped kind with two secret
properties (4.1), and 5.2 is latent rather than live.

## What the sealed store is

Three layers, added in this order, all still live.

1. **The host credential key.** `SUBSTRATE_CREDENTIAL_KEY` is one operator-set
   string. `deriveCredentialKey` (`internal/engine/credentials.go:240`) turns
   it into a 32-byte AES key. It is the control plane's only key.
2. **The per-repository DEK.** `newDEK` (`internal/engine/dek.go:24`) mints 32
   random bytes per repository. The control-plane `repositories.dek` column
   holds it wrapped under the host key; the repository's `recoverykey` record
   holds it wrapped to the user's age recipient. Every payload in the `sealed`
   table seals under the DEK (`dataset.sealPayload`, `dek.go:106`).
3. **Secret-typed properties as refs.** A property whose datatype is `secret`
   stores a `secret:<32 hex>` ref; the material lives in one `sealed` row owned
   by `(record_kind, record_id)`. The changelog and the `records` fold carry
   the ref only. `storeSecretProps` (`internal/engine/write.go:1353`) is the
   one path in; `openSecretValue` (`credentials.go:327`) is the one path out.

The framing is one marker byte: `'p'` for plaintext, `'s'` for
`nonce||ciphertext` under AES-256-GCM (`credentials.go:32`). The stated
invariant is that the two planes never mix: the user plane (changelog, sealed,
blobs) is recoverable with the age identity alone, and nothing host-keyed sits
inside a repository.

`Sensitive()` (`internal/vocabulary/types.go:386`) is `secret` plus `digest`,
and it is what every read surface keys off to redact.

## 1. The keys

### 1.1 `deriveCredentialKey` is one unsalted SHA-256, with no length floor (verified)

`deriveCredentialKey` (`credentials.go:240`) is `sha256.Sum256([]byte(key))`.
No salt, no iterations, no memory cost, no minimum length, and nothing
validates the string: `internal/config/config.go:41` declares it with
`default:""` and no `Validate` method exists in that package.
`SUBSTRATE_CREDENTIAL_KEY=hunter2` is therefore a valid AES-256 key.

Attacker capability assumed: a copy of the database, and nothing else. The
`repositories.dek` column, the `sealed` table and the signing seeds are all in
it. The attacker guesses a passphrase, hashes it once, unwraps a DEK, and
checks whether a sealed row's GCM tag verifies. One SHA-256 plus one AES-GCM
open per guess is a few hundred nanoseconds on a GPU-free machine.

Cost: every provider token, every `apiKey`, the argon2id password hash, the
TOTP seed and every repository's changelog signing seed, for every user on the
host, from a dump plus a dictionary. Against a real 32-byte key the derivation
is fine; the defect is that nothing makes the operator supply one.

Filed as [#229](https://github.com/geoah/substrate/issues/229), with
[ADR 0024](../decisions/0024-the-credential-key-is-key-material-not-a-passphrase.md)
for the choice between demanding key material and stretching.

### 1.2 The shipped `compose.yaml` boots keyless, and the dump alone opens everything (verified)

**Fixed after this review.** `SUBSTRATE_INSECURE_ALLOW_INVALID_SIGNATURES` no
longer exists in any form, and `compose.yaml` no longer defaults
`SUBSTRATE_CREDENTIAL_KEY` to empty: it mints 32 random bytes into its keys
volume on first start and reads them back on every later one, so a host with no
credential key refuses to boot instead of running keyless. The rest of this
section is the state at the date above.

`compose.yaml:42` defaulted `SUBSTRATE_CREDENTIAL_KEY` to empty and
`compose.yaml:49` defaulted `SUBSTRATE_INSECURE_ALLOW_INVALID_SIGNATURES` to
`true`. Signing is otherwise mandatory and a keyless host refuses to open a
repository (`internal/engine/engine.go:527`), so that second default was what
let the first one run.

On a keyless host `sealCredential` (`credentials.go:207`) returns
`'p' || raw`, so `repositories.dek` holds the DEK in the clear.
`openCredential` and `openWithFallback` (`dek.go:138`) return `payload[1:]`
for a `'p'`-marked payload unconditionally, whatever the host's key state.
`sealPayload` still refuses to store a repository payload unsealed
(`dek.go:114`), so the `sealed` rows are genuinely ciphertext, but their key
is beside them in plaintext.

The boot warning is one `slog.Warn` line (`engine.go:272`).

Attacker capability assumed: a copy of the database.

Cost: a substrate started this way holds its DEKs in plaintext, so a stolen
dump is every secret. How much that is worth turns on who runs compose, and
the repository is not consistent about it. `compose.yaml:1` calls itself "a
whole substrate on this machine" and `AGENTS.md` calls the keyless signing
default pre-v1 scaffolding tied to
[#175](https://github.com/geoah/substrate/issues/175), both of which read as
local-only. But `README.md:18` puts `docker compose up` under "Quick start"
with "that is the whole thing" and "nothing to configure before the first
run", and `docs/operations.md`, the page on running a substrate, never
mentions compose at all, so nothing tells a reader who followed the quick
start that they are on a development artifact. Registration is one-shot per
user and there is no unregister, so the database somebody registers into is
the one they keep.

What makes this worth fixing regardless of that argument is the documentation.
`docs/operations.md:186-194` describes a backup as sealed rows plus two wraps
of the DEK and says "either the host key or the user's recovery identity opens
a backup", which reads as a claim that the dump alone does not. That claim is
false in the keyless state, the Backups section never mentions that state, and
the only runtime signal is one `slog.Warn` nobody reads twice. `README.md:119`
is honest ("unset implies plaintext and a warning"); `docs/operations.md` is
not.

I kept this at p0 on that basis: not because compose is a production artifact,
which it does not claim to be, but because the documented first-run path
produces a silently keyless substrate and the operations page tells the
operator the opposite. Either half is cheap to fix.

Filed as [#230](https://github.com/geoah/substrate/issues/230).

### 1.3 What a stolen backup gets, stated plainly (verified)

Everything below assumes one consistent `pg_dump` and no other access.

| Host state | What the dump alone yields |
| --- | --- |
| No credential key (compose default) | The DEK in plaintext, so every sealed payload, every signing seed, every password hash and TOTP seed. |
| Passphrase credential key | The same, after an offline dictionary attack at one SHA-256 and one AES-GCM open per guess. |
| 32 bytes of real key material | Nothing: the sealed rows are inert, and the age-wrapped DEK on the `recoverykey` record needs the user's identity. |

The dump and the key normally live in one blast radius (one compose file, one
environment), which is a deployment property this repository cannot fix. It
can state it, and 1.2 is where it should be stated.

## 2. The sealing itself

### 2.1 `sealWith` binds no additional data (verified)

`sealWith` (`credentials.go:177`) calls `aead.Seal(out, nonce, raw, nil)`. The
fourth argument is the AEAD's additional data and it is nil at every call site:
`sealCredential` (the DEK wrap), `sealRepoPayload`, `dataset.sealPayload`, and
the reseal migration's `rekeySealedStore`. A ciphertext is therefore bound to
its key and to nothing else: not to its `ref`, not to the `record_kind` and
`record_id` that own it, not to the repository.

The read side does not close the gap. `openSecretValue` (`credentials.go:333`)
resolves a ref with `SELECT payload FROM sealed WHERE ref = $1` and no owner
predicate. Ownership is checked only on the WRITE side, by
`sealedRefOf` (`credentials.go:310`), which is `WHERE ref = $1 AND record_kind
= $2 AND record_id = $3`. That single check is the whole containment.

Attacker capability assumed: write access to the `sealed` table without the
key. A SQL-injection write, a database role that is not the server's, a DBA, or
a restored backup the attacker can edit and re-attach.

Cost: such an attacker copies the payload of the credential record's
`passwordRef` row into a `secret:` ref owned by a record they can read through
the runner's secret injection (`internal/engine/invocationconfig.go:172`
resolves every non-OAuth secret property to material for a function body), and
the substrate decrypts it for them. Binding `ref || record_kind || record_id`
as AAD turns that from a read into a decryption failure.

One thing breaks under AAD and has to be fixed first. `rekeySealedStore`
(`reseal.go:456`) decides a payload needs no work with
`payload[0] == credSealed && openWith(dekAEAD, …)` (`reseal.go:488-492`), and
`openWith` passes nil additional data, so a bound payload matches neither
half: the migration would re-key every already-correct row and then fail to
open it. The rest is clear: rotation mints a fresh ref and deletes the old row,
and `RebuildRepository` never touches `sealed`. The DEK wrap in
`repositories.dek` has no ref and needs its own binding string. Details, the
framing-byte question and the reseal prerequisite are in
[ADR 0023](../decisions/0023-a-sealed-payload-is-bound-to-its-address.md).

Filed as [#231](https://github.com/geoah/substrate/issues/231).

### 2.2 Nonces: random 96-bit, no counter (verified, not a defect)

`sealWith` draws a fresh 96-bit nonce from `crypto/rand` per seal. The risk is
birthday collision under one key, and the key is per repository. The write
volume that would matter is on the order of 2^32 seals against one DEK; the
paths that seal repeatedly are the OAuth refresh loop (one seal per token
refresh per account), secret rotation (one per write of a changed value) and
`rekeySealedStore` (one per row, once per reseal). None of them approaches it.

The reasoning was not written down anywhere, which is what #99 asked for. It is
written down here and nothing further is needed.

### 2.3 The host-key fallback keeps the two-planes invariant aspirational (verified)

`openWithFallback` (`dek.go:134`) tries the DEK, then the host key. That second
branch exists for payloads sealed before DEKs. `rekeySealedStore` re-keys them,
and it runs both from `repository reseal` and from `EnrollRecoveryKey`
(`auth.go:470`), so after either one the branch should be dead.

Nothing asserts that it is. There is no per-repository marker recording "this
repository holds no legacy payloads", no counter of fallback opens, and no
test that a resealed repository never takes the second branch. For any
repository that predates DEKs, "the user plane is recoverable with the age
identity alone" is a claim held by a migration having been run, not by
anything the code checks. A payload that quietly stays host-keyed is a payload
the recovery identity cannot open, and the user would find out at recovery
time.

This overlaps [#133](https://github.com/geoah/substrate/issues/133) (sealed
payloads carry no key id), which proposes the same shape of fix from the other
direction. I commented there rather than filing a duplicate.

### 2.4 `credPlain` payloads open unconditionally (verified)

Both `openCredential` (`credentials.go:223`) and `openWithFallback`
(`dek.go:138`) return `payload[1:]` for a `'p'` first byte with no check on
whether the host has a key. A host running with a correct credential key still
hands back plaintext for any row whose first byte an attacker sets to `'p'`.

Attacker capability assumed: the same `sealed`-table write as 2.1, so this
adds nothing an attacker with that access does not already have. It matters as
a downgrade: after a keyed host has resealed everything, a `'p'` row is
indistinguishable from legacy. Refusing plain framing once a repository is
marked legacy-free is the same marker 2.3 wants, which is why both belong to
[#133](https://github.com/geoah/substrate/issues/133).

## 3. Where a secret can surface

The redaction set is larger and more disciplined than #99's "roughly twenty
call sites" suggests, and it fails closed in the two places where failing open
would be worst. What follows is every path a property value can reach, and
what each one does.

### 3.1 The paths that redact, confirmed (verified)

| Path | Where | What it does |
| --- | --- | --- |
| REST and GraphQL record reads | `internal/engine/validate.go:1068` (`redactProps`, called from `recordOf`) | Replaces the value with `<redacted>`. GraphQL resolvers (`internal/gql/schema.go:785`) read the already-built `substrate.Record`, so they inherit it. |
| Property offers and alternatives | `internal/engine/query.go:396` | Redacts, and fails closed to `<redacted>` when the kind does not resolve. |
| Filtering | `internal/engine/query.go:832` | Refuses the query with `ErrValidation`. |
| Ordering | `internal/engine/query.go:1254` | Refuses. |
| Full-text search | `internal/engine/validate.go:1000` (`ftsBands`) | Excludes. `internal/vocabulary/load.go:1690` also forces `FTS = false` at parse time and `load.go:1695` stops an explicit `fts: true` from re-enabling it. |
| Snippets | `internal/engine/validate.go:965` | Excludes. |
| Embedding text | `internal/vocabulary/load.go:1691` | Forces `Embed = false`; a sensitive property never reaches the embed input. |
| Title templates, own kind | `internal/vocabulary/load.go:1008` refuses at load; `internal/engine/validate.go:701` renders empty at runtime for legacy declarations | Two layers, deliberately. |
| Title templates, edge target | `internal/engine/validate.go:912` (`targetProp`) | Renders empty, with a comment saying the loader cannot check across kinds. |
| The watch and change feed | `internal/engine/changefeed.go:150` (`redactChangePayload`) | Redacts, and for a kind that no longer resolves redacts **every string** in the delta. |
| Record mappings | `internal/vocabulary/mapping.go:351` | Refuses a mapping rule whose source is sensitive. |
| Change-request diffs | `internal/engine/write.go:1843` | Refuses a diff carrying a raw value for a sensitive property. |
| `get -o yaml` | Same as REST: the CLI renders the `substrate.Record` the API returned. |
| The console | `web/console/src/components/record/properties.tsx:179` renders the sentinel; `web/console/src/lib/record-schema.ts:684` returns `""` for a secret | Never holds material. |
| `slog` | No engine log call takes a property value. The one credential-related line is the keyless boot warning (`engine.go:272`), which carries no value. |

### 3.2 A reference target's sensitive property is not skipped (verified)

This is the gap. `titleResolver` has two cross-record hops and only one of
them checks:

- `targetProp` (`internal/engine/validate.go:900`) resolves the target kind and
  returns `""` for a sensitive property. Its comment says why: "the loader
  cannot check an edge target's property".
- `referenceProp` (`internal/engine/validate.go:765`) does the same hop for a
  `reference`-typed property and has no such check. It reads
  `row.Props[prop]` off the referent and returns `scalarString(v)`.

`{ref.prop}` reaches it: `validate.go:881` dispatches a dotted token whose head
is a reference-typed property to `r.reference(rel, prop)`, which calls
`referenceProp`. The loader's own refusal (`load.go:1008`) only inspects the
kind's own properties, so nothing refuses the declaration either.

Attacker capability assumed: the ability to declare a kind, which any
repository owner has through `/vocabulary/apply`.

Cost: the rendered value lands in `records.title`, which is FTS band A, is
returned unredacted on every read surface, and rides the change feed. For a
`secret` property the leaked value is the opaque `secret:` ref, which is not
material. For a `digest` property (`token.hash` today) the leaked value is the
digest itself, which is exactly what `Sensitive()` exists to keep off the wire.
No shipped declaration spells such a token, so this is a latent bug rather
than a live leak, and it becomes live the day a new `digest` property arrives.

Filed as [#232](https://github.com/geoah/substrate/issues/232).

Two asymmetries inside that set, neither a live leak:

- The embedding text build (`internal/engine/search.go:323-327`) gates on
  `p.Embed` alone and never asks `Sensitive()`, while its neighbours
  `ftsBands` and `snippetOf` ask at runtime as well as at load. The loader
  forces `Embed = false` for a sensitive property (`load.go:1691`) and the
  explicit `embed:` flag is read before that line, so nothing can re-enable it,
  and a stored declaration is re-parsed through the same loader. The defense is
  real; it is one layer where its neighbours have two.
- The change feed's `?q=` filter matches **pre-redaction** bytes:
  `changefeed.go:73-89` runs `payload::text ILIKE '%…%'` in SQL, and
  `redactChangePayload` shapes only what comes back. The code comment reasons
  about this and is right for a resealed store: a secret's delta value is an
  opaque ref, and it names legacy plaintext as the one exception. The residual
  is that nothing marks a repository as reseal-clean, so an operator cannot
  tell which repositories still answer that oracle. That is the same missing
  marker as 2.3, and it is what I raised on
  [#133](https://github.com/geoah/substrate/issues/133).

### 3.3 The deliberate raw readers (verified, correct today)

Four places read material on purpose.

- `agentloop.go:122` reads the raw `llmprovider` row for the provider API key,
  with a comment saying so.
- `invocationconfig.go:172` (`injectedRecordConfig`) resolves every secret
  property to material for a function body, in the same pass that arms the
  scrubber, so the injected view and the scrub list cannot disagree.
  `isOAuthFacilitySecret` omits `clientSecret` and `tokenRef` entirely.
- The runner injects that config and scrubs everything on the way out
  (`internal/engine/runner.go`, fourteen call sites).
- `scrub.go` replaces injected values by exact match in logs, errors, outputs,
  effect targets and continuation cursors, refusing the whole invocation
  rather than persisting a redaction marker into user data.

Each is correct. Two observations rather than findings:

- The raw read is a bare SQL read at `agentloop.go` and a `Secret()` branch at
  `invocationconfig.go`, not a named function. #99 asks whether a future reader
  can end up raw by accident. It can: `SELECT props FROM records` returns refs,
  and any new caller that then calls `openSecretValue` is a raw reader with
  nothing marking it as one. A named `openForInjection` that also registers
  with the scrubber would make the set enumerable. I did not file this: it is a
  refactor with no defect behind it, and it belongs with whatever closes 2.1.
- `scrubbedError` (`scrub.go:167`) scrubs `Error()` but `Unwrap()` returns the
  unscrubbed cause. Whether any caller renders the cause directly is
  **unconfirmed**; I did not find one, and `fmt.Errorf("%w")` renders the
  scrubbed text.

### 3.4 Blob bytes are not sealed (verified, out of scope by decision)

`blobs.bytes` is plaintext. A secret pasted into an uploaded file is plaintext
in the database and stays plaintext when
[#97](https://github.com/geoah/substrate/issues/97) moves blob bytes to a
filesystem or S3 backend. This is a deliberate scope line, not an oversight:
the sealed store seals declared secret-typed values, and a blob is opaque
bytes with no declaration saying what is in it. It should be written into
`docs/operations.md` beside the backup claim rather than left implied, which is
part of [#230](https://github.com/geoah/substrate/issues/230).

## 4. The write path

### 4.1 Two secret properties on one record can alias one sealed row (verified)

`storeSecretProps` treats an accepted value naming an existing sealed row **of
the same record** as a carried ref and stores it verbatim
(`write.go:1393-1399`). The check is `sealedRefOf`, which matches on
`(ref, record_kind, record_id)` and nothing else. Two secret properties on one
record can therefore hold the same ref.

Rotation then erases both. Writing a new value to property `a` takes
`beforeRef("a")`, stores the new material under a fresh ref, and runs
`DELETE FROM sealed WHERE ref = $1` on the old one (`write.go:1430`). Property
`b` still names the deleted ref, and `openSecretValue` answers
`secret ref has no sealed row`. Nothing detects the aliasing at write time and
nothing warns at rotation time.

The cross-record case is closed: `sealedRefOf` refuses a ref owned by a
different record, so one record cannot adopt another's material. That is the
containment working, and it is worth saying explicitly because everything else
in this section depends on it.

Attacker capability assumed: the ability to write two secret properties on one
record. One shipped kind declares two, `core.substrate.reamde.dev/credential`
with `passwordRef` and `totpRef`
(`kinds/core.substrate.reamde.dev/credential.yaml:32,35`); the bundle files
spread their two across separate kinds. The bug cannot fire on `credential`:
`forbidSystemKind` refuses the generic write surface for it, and the auth
machinery writes the two properties with distinct material through its own
path and never aliases them. So no shipped kind exposes two
attacker-writable secret properties, and reproducing this needs a
repository-local kind or an installed bundle's kind that declares two.

Cost: silent loss of secret material on an ordinary rotation, unrecoverable
because the sealed row is gone. A second effect is that a same-record alias
sidesteps `isOAuthFacilitySecret`: a kind implementing `accountconfig` with a
second secret property beside `tokenRef` can point that property at the
`tokenRef` row and have the runner resolve the OAuth access token into a
function body, which `invocationconfig.go:158` says must never happen. That
path gains a bundle nothing it does not already receive as a resolved token
today, so I rate it hardening rather than a live escalation, and note it as
the reason the aliasing is worth refusing rather than tolerating.

Filed as [#233](https://github.com/geoah/substrate/issues/233).

### 4.2 The re-paste no-op is an equality oracle with an observable effect (verified)

`storeSecretProps` compares an accepted plaintext against the current material
and, on a match, keeps the existing ref and drops the property from the
accepted list (`write.go:1407-1411`). The compare itself is
`subtle.ConstantTimeCompare`, which closes the timing channel and not the
observable-effect channel: a matching guess mints no delta, bumps no version
and takes no manager attribution, and a wrong guess does all three. The caller
reads the answer off the record's version.

`query.go:1049` reasons about the read-side oracle (filter and order are
refused for exactly this reason) and has a comment saying so. The write side
has no such comment.

Attacker capability assumed: write access to the property without read access
to it. Under the current model a token has full repository access, so the
realistic case is an actor whose writes are gated (an agent, a bundle) probing
a value the read surface redacts from it.

Cost: one bit per guess, confirming a guessed secret without touching the
provider that would otherwise log the attempt. This is narrow, and closing it
means giving up the re-paste no-op, which exists so that `get -o yaml | apply
-f` does not churn a secret's attribution. The honest resolution is probably a
comment recording the trade rather than a behavior change, which is what the
issue asks for.

Filed as [#234](https://github.com/geoah/substrate/issues/234).

### 4.3 DELETE is not erasure (verified)

Rotation and accepted-deletion both `DELETE FROM sealed`, and
`credentials.go:248-265` and `write.go:1348-1350` both lean on that as erasure:
"rotation erases material rather than retiring it into an immutable log". The
row is gone from the live heap. The old ciphertext survives in the MVCC dead
tuple until `VACUUM`, in the WAL until the segment is recycled, in any physical
replica, and in every backup taken before the rotation.

That does not undo the design, which is about keeping material out of the
append-only changelog, and it succeeds at that. It does mean the erasure claim
is scoped to "the live table" and not to "the database", and neither the code
comments nor `docs/operations.md` say so.

Cost: an operator who rotates a leaked key and restores a week-old backup
restores the leaked key with it, and nothing in the docs warned them.

Filed as [#235](https://github.com/geoah/substrate/issues/235).

### 4.4 Orphaned sealed rows outside the OAuth teardown (verified)

`credentials.go:262` names this as known future work. Confirming the shape:
`DELETE FROM sealed` runs in exactly three places, and none covers a record's
hard delete. `deleteCredentialsFor` (`credentials.go:135`) is called only from
the OAuth finalizer (`oauth.go:594`). `hardDelete` (`internal/engine/rows.go:725`),
which the GC path calls (`gc.go:79`), removes the record and its dependents and
does not touch `sealed`. An ordinary `delete` is a tombstone and keeps its
props, so the ref survives with its row; a GC of that tombstone drops the ref
and leaves the row.

Cost: encrypted material addressed by nothing, invisible to `repository
inspect`, uncounted by `reseal`, and never erased. It is not a leak while the
DEK holds, and it is exactly what a future "delete my account" story
(issue [#136](https://github.com/geoah/substrate/issues/136)) has to sweep.

Filed as [#236](https://github.com/geoah/substrate/issues/236).

## 5. Recovery and the operator hat

### 5.1 The age identity is never stored or logged (verified)

`generateRecoveryIdentity` (`dek.go:205`) returns the identity string to its
caller and nothing else. `Register` (`auth.go:343-349`) and
`EnrollRecoveryKey` (`auth.go:438-476`) put it in the result struct; the API
returns it once (`internal/api/auth_endpoints.go:218`, `:416`). No log call, no
column, no error string carries it. `writeRecoveryKey` (`auth.go:482`) stores
only the recipient and the age-wrapped DEK.

A client-supplied recipient is validated before anything depends on it:
`auth.go:351` and `auth.go:448` both call `wrapDEKToRecipient` against a
throwaway 32 zero bytes purely to make `age.ParseX25519Recipient` refuse a bad
recipient early, before the repository is created or the enrollment
transaction opens. This one is clean.

### 5.2 Enrollment is one-shot for the life of the repository (verified)

`EnrollRecoveryKey` refuses when a live `recoverykey` record exists
(`auth.go:464`), and the generic surface can neither write it
(`forbidSystemKind`) nor delete it (`write.go:2403` refuses a delete for every
kind in `systemKinds`, and `dataset.go:56` lists `kindRecoveryKey`). So there
is no rotation, no re-wrap, and no path at all for a user who loses the
identity. [#137](https://github.com/geoah/substrate/issues/137) tracks that.

What #137 does not carry, and what a rotation design has to: **a past recipient
can never be revoked by re-wrapping.** `sealedKey` is declared `type: string`
on `core.substrate.reamde.dev/recoverykey`, not `type: secret`. It is an
ordinary property value in an append-only changelog, and the reseal migration
rewrites only secret-typed properties (`resealChangelog`'s `isSecret` filter,
`reseal.go:354`). Every historical `sealedKey` therefore stays in the log
forever, and each one is the repository's DEK wrapped to a recipient that was
valid when it was written. Anyone holding a superseded identity plus a dump
still opens every payload. Revoking a compromised recovery identity means
rotating the DEK and re-sealing every payload under it, not re-wrapping.

**This is latent, and the issue is scoped to say so.** Because enrollment is
one-shot and no rotation path exists, a repository has exactly one recipient
and no superseded recipient can exist yet. That single identity opening the
DEK is recovery working. The finding is a constraint on the rotation #137 asks
for, and the work it names is to write the constraint down before that
rotation is designed, so it carries `priority/p2` and no milestone, matching
#137.

Filed as [#237](https://github.com/geoah/substrate/issues/237).

### 5.3 The recovery claim is proven end to end (verified)

#99 asks whether a test walks the whole path or only exercises
`OpenPayloadWithKey` in isolation. It walks the whole path.
`TestRegistrationEnrollsRecoveryKey`
(`internal/engine/recovery_db_test.go:52`) mints the age identity client-side,
registers with only the recipient, reads the `recoverykey` record's
`sealedKey`, unwraps it with `age.Decrypt` under the identity, opens a second
raw `sql.DB` against the same DSN, writes an `llmprovider` with an `apiKey`,
reads the ref out of `records.props` and the payload out of `sealed`, and calls
`OpenPayloadWithKey` with the identity-recovered DEK and no host key. Two more
tests cover the server-minted pair and the pre-DEK legacy migration
(`recovery_db_test.go:155`, `:211`).

Two gaps, neither worth a ticket:

- The test asserts the control-plane `dek` column is non-empty and that the
  recovered key is 32 bytes, but the comment at `recovery_db_test.go:114` says
  "the compare is direct" and no compare follows. The later
  `OpenPayloadWithKey` proves the same thing functionally, so the comment is
  wrong and the test is not.
- Nothing walks a full `pg_dump` and restore. That is
  [#216](https://github.com/geoah/substrate/issues/216)'s (no backup or restore
  procedure), and the payload-level proof above is the part this review was
  asked about.

No new test was added in this branch. The one #99 asked for already exists.

### 5.4 The operator hat is DSN-only (verified)

`ResealRepository`, `RebuildRepository`, `RebuildRepositoryUnverified`,
`VerifyRepositoryPinned` and `ResetUser` are reachable from exactly one place:
the interface assertions in `cmd/substratectl/commands/operator.go:235-254`,
called from `repository.go` and `user.go`. `internal/api` never imports
`internal/engine` (the house rule), and none of these names appears in
`internal/api` or in `internal/substrate`'s `Service` interface, so no HTTP
route can reach them. `operator.go:79-90` refuses without
`SUBSTRATE_CREDENTIAL_KEY` unless an explicit unsealed flag is passed, and the
operator commands refuse without a DSN.

`ResealRepository` additionally refuses without a credential key
(`reseal.go:64`), refuses until the boot-time vocabulary upgrade has run
(`reseal.go:75`), refuses when signing is active but the key is unavailable
(`reseal.go:82`), and verifies the whole chain inside its own transaction
before rewriting anything (`reseal.go:140-150`), with no force path. This is
the most carefully gated thing in the sealed store.

## Findings and their issues

| # | Finding | Issue | Kind |
| --- | --- | --- | --- |
| 1.1 | `deriveCredentialKey` is one unsalted SHA-256 with no length floor | [#229](https://github.com/geoah/substrate/issues/229) | bug, p0 |
| 1.2 | `compose.yaml` boots keyless, so the dump alone opens every sealed row | [#230](https://github.com/geoah/substrate/issues/230) | bug, p0 |
| 2.1 | `sealWith` binds no additional data | [#231](https://github.com/geoah/substrate/issues/231) | bug, p1 |
| 2.3, 2.4 | Host-key fallback and `credPlain` never turn off | [#133](https://github.com/geoah/substrate/issues/133) (commented, not duplicated) | existing |
| 3.2 | `referenceProp` skips the sensitive check `targetProp` has | [#232](https://github.com/geoah/substrate/issues/232) | bug, p1 |
| 3.4 | Blob bytes are not sealed | [#97](https://github.com/geoah/substrate/issues/97), scope stated in [#230](https://github.com/geoah/substrate/issues/230) | existing |
| 4.1 | Two secret properties on one record alias one sealed row | [#233](https://github.com/geoah/substrate/issues/233) | bug, p1 |
| 4.2 | The re-paste no-op is a write-side equality oracle | [#234](https://github.com/geoah/substrate/issues/234) | bug, p2 |
| 4.3 | DELETE is not erasure against MVCC, WAL and backups | [#235](https://github.com/geoah/substrate/issues/235) | bug, p2 |
| 4.4 | Hard delete orphans sealed rows | [#236](https://github.com/geoah/substrate/issues/236) | bug, p2 |
| 5.2 | A past recovery recipient can never be revoked (latent: no rotation exists) | [#237](https://github.com/geoah/substrate/issues/237) | bug, p2 |

Two decisions came out of this and are recorded:

- [ADR 0023](../decisions/0023-a-sealed-payload-is-bound-to-its-address.md):
  bind `ref || record_kind || record_id` as AAD, behind a framing byte. It
  names `rekeySealedStore` as a prerequisite: the migration decides idempotency
  with an AAD-blind open and has to learn the new framing first.
- [ADR 0024](../decisions/0024-the-credential-key-is-key-material-not-a-passphrase.md):
  demand base64 of 32 bytes and refuse anything else. Stretching is rejected
  for taking the operator's judgement as an input, not for being ineffective:
  argon2id would raise the per-guess cost by roughly a factor of a million
  even with a salt the attacker holds.

## What was not settled

- Whether any caller renders a `scrubbedError`'s unwrapped cause, which would
  print an unscrubbed secret (3.3). I found none; I did not prove there is
  none.
- Whether an installed bundle can in practice declare a second secret property
  on an `accountconfig` kind and get the OAuth facility to write `tokenRef`
  onto the same record (4.1). The write path admits it; I did not build the
  bundle that does it.
- Whether the `sealed` table's row-level security scopes every read in this
  review's threat model. I read the queries, not the policies, and treated
  cross-repository isolation as out of scope.
- The blast radius question in 1.3 is a deployment property. This review says
  what the code guarantees; it does not say what any particular deployment
  does.
