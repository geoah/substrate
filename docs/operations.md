# Running a substrate

The substrate is one Go binary and one Postgres database. It serves the
[API](api.md) and the [console](console.md) on one port, runs its own
background loops in-process, and needs nothing else to be useful. This page is
how to stand one up and look after it.

## What it needs

- **Postgres**, with the `vector` and `pgcrypto` extensions available. The
  binary runs its own migration at boot, creates the two roles isolation rests
  on (`substrate_app`, bound by row level security, and `substrate_maint`,
  which bypasses it for registration and cross-repository lookups), and enables
  the extensions, so the DSN it starts with must be allowed to do those things.
- **One port**. The service serves the API under `/api`, the authentication
  endpoints beside it, and the console at `/`.
- **Nothing else.** Search, the change feed, the function runner, and the OAuth
  facility are all in the one process; the image also carries `python3`, the Go
  toolchain, and `uv`, because [functions](functions.md) run as child
  processes of the substrate. It carries `substratectl` too, so the operator
  commands below run inside the container.

Everything lives in one Postgres schema. Repositories are separated by a
`repository` column plus `FORCE ROW LEVEL SECURITY` keyed on the authenticated
token's repository — enforced by the database, not by discipline in the query
layer, and it fails closed.

## Configuration

There is no settings surface: configuration is the environment, read once at
boot.

| Variable                       | Default                                | What it does                                                                                              |
| ------------------------------ | -------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `DATABASE_URL`                 | required                               | The one Postgres holding every repository.                                                                |
| `PORT`                         | `8080`                                 | The port served.                                                                                          |
| `LOG_LEVEL`                    | `info`                                 | `debug`, `info`, `warn`, `error`.                                                                         |
| `WEB_DIR`                      | —                                      | The built console, served at `/`. Empty disables static serving.                                          |
| `SUBSTRATE_INVITE_CODE`        | — (unset: registration is off)         | The one way in. See below.                                                                                  |
| `SUBSTRATE_CREDENTIAL_KEY`     | — (required)                           | Seals the sealed store, which holds every secret-typed property's material (AES-256-GCM), and the per-repository changelog signing seeds. Signing is mandatory and the seed may never sit unsealed beside the signatures it mints, so a host without this key refuses to boot, with no exception. A host whose key does not open the signing seeds the database already holds refuses to boot too, naming the repositories: that is a wrong key or a database from somewhere else, and nothing here can be re-keyed. `repository reseal` upgrades unsealed payloads once it is set. |
| `SUBSTRATE_INSECURE_DISABLE_TOTP` | `false`                             | **Local development only.** Stops verifying the second factor, so a password is the whole credential: see [the local TOTP-off switch](auth.md#the-second-factor-can-be-switched-off-locally). Boots with a warning, and `GET /.well-known/substrate/server.json` says so. |
| `SUBSTRATE_OAUTH_STATE_KEY`    | —                                      | Signs OAuth flow state. Unset mints a random key per boot, with a warning: flows in progress break on restart. |
| `SUBSTRATE_OAUTH_CALLBACK_URL` | —                                      | The one redirect URI every provider app registers.                                                        |
| `SUBSTRATE_CONSOLE_URL`        | —                                      | The console origin the OAuth return-page posts to and falls back to redirecting into. Empty is local dev. |
| `SUBSTRATE_SANDBOX`            | `best-effort`                          | How hard to confine function bodies: `off`, `best-effort`, or `enforce` (refuse to run a body unconfined). |
| `SUBSTRATE_BLOB_STORE`         | `postgres`                             | Where blob bytes live: `postgres` (the `blobs` column), `fs` (a directory) or `s3` (a bucket). See [the blob store](#the-blob-store). |
| `SUBSTRATE_BLOB_FS_ROOT`       | —                                      | `fs` only: the root directory, one subdirectory per repository. Must be absolute, and must outlive the container.                    |
| `SUBSTRATE_BLOB_S3_ENDPOINT`   | —                                      | `s3` only: the service URL, scheme included (`https://s3.us-east-1.amazonaws.com`, or a self-hosted endpoint).                       |
| `SUBSTRATE_BLOB_S3_BUCKET`     | —                                      | `s3` only: the bucket. It must be PRIVATE — the bytes are stored as they arrived.                                                    |
| `SUBSTRATE_BLOB_S3_REGION`     | `us-east-1`                            | `s3` only: the region the request is signed for.                                                                                     |
| `SUBSTRATE_BLOB_S3_ACCESS_KEY_ID` / `SUBSTRATE_BLOB_S3_SECRET_ACCESS_KEY` | — | `s3` only: the credentials every request is signed with. `SUBSTRATE_BLOB_S3_SESSION_TOKEN` beside them for temporary ones.        |
| `SUBSTRATE_BLOB_S3_PREFIX`     | —                                      | `s3` only: a key prefix, for a bucket this substrate shares with something else.                                                     |
| `SUBSTRATE_BLOB_S3_PATH_STYLE` | `true`                                 | `s3` only: address the bucket as a path segment rather than a subdomain. Self-hosted endpoints want it; AWS accepts it.               |

`SUBSTRATE_CREDENTIAL_KEY` is the one that must be backed up beside the
database: without it, sealed material is unreadable.

## There is no LLM configuration

The server takes no LLM endpoint, no key and no embedding model. Completions
and embeddings alike are bought through a repository's own
[`llmprovider`](agents.md#providers) records, which carry the wire, the
endpoint, the key and (for embeddings) the model. The process holds no bearer,
so no host-wide key can reach a repository-chosen endpoint.

What that means for an operator:

- A fresh repository has no agents and no semantic search until its owner
  writes a provider row. Nothing seeds one; the
  [LLM example bundle](bundles-catalog.md#llm-example) ships two ready to key.
- Semantic search runs against the one row that declares `embedModel`, and
  hybrid search returns its lexical arm alone until that row exists.
- Every stored vector names the row and the model that produced it. Change
  either and the older vectors stop being searched, which is deliberate: cosine
  distance between two models' vectors is not a distance. Run
  `substratectl --dsn … repository reembed <username>` to queue their
  replacement, or `POST
  /api/v1/core.substrate.reamde.dev/embeddings/reembed` from the repository's
  own token. Both write queue rows; the server's drain loop buys the vectors a
  batch at a time, so an interrupted re-embed resumes by itself.
- A gateway swapped behind an unchanged row and model name is invisible to the
  provenance columns, so that case takes `reembed --all`.

## The blob store

A blob is two halves: a **manifest**, which is an ordinary record keyed by the
content digest, and the **bytes**. The manifest is always in Postgres and is
always the truth. `SUBSTRATE_BLOB_STORE` says where the bytes go.

| Backend            | Where the bytes are            | Backup                             |
| ------------------ | ------------------------------ | ---------------------------------- |
| `postgres`         | the `blobs` column             | the database dump, and nothing else |
| `fs`               | `<root>/<repository>/<digest>` | the dump **plus** the root          |
| `s3`               | `<prefix><repository>/<digest>` in the bucket | the dump **plus** the bucket |

`postgres` is the default and the simplest thing that works: one dump is a
whole backup, row level security is what separates two repositories, and the
bytes and the manifest commit in one transaction. What it costs is that every
uploaded byte goes through WAL and into the backup of a database whose value is
the changelog, which is why the other two exist.

**Isolation stops being the database's job.** On `fs` and `s3` the repository
is half of every key, and it comes from the authenticated token's repository,
never from the request: a read resolves the manifest first, under row level
security, and only then fetches bytes. So a caller cannot reach another
repository's blob by guessing a digest. But anything that can read the root
directory or the bucket can read every repository's blobs: the store is as
trusted as the database. Give the fs root to the substrate's user alone (it
creates directories `0700` and files `0600`), and keep the bucket private, with
credentials only this substrate holds.

**Blob bytes are never sealed, on any backend.** The sealed store covers
secret-typed properties; `blobs.bytes`, an object on disk and an object in a
bucket are all stored exactly as they arrived, and no credential key is
involved in reading any of them
([0031](decisions/0031-blob-bytes-outside-postgres-are-stored-plaintext.md)).
Whoever holds the dump, the root or the bucket holds every attachment in the
clear. For encryption at rest, put it under the store: disk encryption for
Postgres or the `fs` root, the bucket's own server-side encryption for `s3`.

**An upload becomes two steps, and a crash between them is cheap.** Outside
Postgres the bytes cannot commit with the manifest, so the manifest is written
`pending` first, then the bytes, then `stored`
([0030](decisions/0030-a-blob-outside-postgres-settles-after-its-bytes.md)). A
manifest only ever says `stored` once the store confirms the bytes, so no read
ever meets a blob whose bytes are missing. What a crash leaves is a `pending`
manifest, which the sweep collects with anything else nobody references.
Deleting works the same way in reverse: the manifest is tombstoned, then the
object is deleted, and an object left behind by a failure is reaped by a later
sweep that lists the store.

**Switching backends is a migration, not a setting.** A server whose configured
store is not where the bytes actually are refuses to boot, rather than serving
404s for half the blobs. Move them first, with the server stopped:

```
SUBSTRATE_BLOB_STORE=fs SUBSTRATE_BLOB_FS_ROOT=/var/lib/substrate/blobs \
  DATABASE_URL=… substratectl blobs migrate --from postgres
```

It moves one repository at a time and deletes each object from the source only
once the target holds it, so an interrupted run is finished by running it
again. `--dry-run` counts what would move; a username moves that user alone.
Then start the server with the same `SUBSTRATE_BLOB_STORE`. Going back is the
same command with `--from` and `--to` swapped.

The 64 MiB cap on one upload and the absence of range reads are the contract,
not the backend: neither changes with the store.

## The function sandbox

Function bodies are third-party code, and the substrate confines them with
Landlock, seccomp and rlimits: see [the sandbox](functions.md#the-sandbox) for
what each layer closes. Two things an operator needs to know:

**Check the boot log.** The substrate reports the sandbox once at startup,
naming the kernel's actual Landlock ABI. If a layer is missing the line is an
ERROR, not a warning, because a confinement that silently does less than it
claims is worse than none. A real deployment should run `SUBSTRATE_SANDBOX=enforce`,
which turns that into a refusal to run bodies at all.

**Both layers work in a stock container**: Docker's and containerd's default
seccomp profiles permit the `landlock_*` and `seccomp` syscalls, and neither
needs a capability. What does **not** work in a stock container is anything built
on user namespaces or cgroup delegation: `CLONE_NEWUSER` is denied by the
default profile and `/sys/fs/cgroup` is mounted read-only, which is why the
sandbox has no memory or process-count ceiling. Do not add `--privileged` to
try to get one.

## The invite code

`SUBSTRATE_INVITE_CODE` is the only way a user gets created. Set it, register,
then unset it and restart: with it unset, registration is closed
(`501 unsupported`). Registration is rate-limited (paced, with no failure
lockout) whether or not the code is set ([users and tokens](auth.md)).

There is no admin user and no operator password. Everything privileged happens
on the box, through the DSN.

## What happens at boot

- The migration runs, under an advisory lock, and the roles and Postgres
  extensions are ensured.
- Each repository is opened the first time something touches it. Opening
  rebuilds its kind registry **from its own stored declaration records** —
  nothing on the serving path reads the binary's embedded tree.
- **The chain backfill runs first**: a repository whose changelog predates
  entry hashes gets them at its first open, oldest-first in bounded chunks,
  before anything else writes, and the backfill records a `backfill` chain
  epoch naming where attested history begins
  ([the chain](changelog.md#the-chain)). A large history takes proportional
  time, once, and the open logs progress.
- **Shipped vocabulary is upgraded, per repository, in one transaction**: the
  first open under a new binary appends the version diff to that repository's
  changelog under the `substrate` actor
  ([the boot-time upgrade](vocabulary.md#how-the-vocabulary-reaches-a-repository)).
- Persisted function bodies re-warm in the background. One that no longer
  prepares logs an error naming the function, and its deliveries park rather
  than the repository failing.

Four loops then run in-process: the trigger dispatcher every 5 seconds, garbage
collection every 5 minutes, OAuth refresh and finalizer processing every
minute, and — when an embedder is configured — the embed-queue drain every
minute. Each enumerates repositories and opens each one through the same
row-level-security-bound pool a request uses.

Keep it to **one replica**. The watch signal and the trigger dispatcher are
in-process, and two dispatchers would serialize on compare-and-swap rather than
scale. The chain leans on the same shape: one writer process per database, so
an older binary can never append unhashed entries behind a newer one's back
(`repository verify` reports exactly that state if it ever happens).

## Upgrading the binary

**Snapshot the database before you deploy.** A repository's first open under a
new binary may promote its stored
[vocabulary dialect](vocabulary.md#vocabulary-evolution-and-the-dialect-contract),
and the promotion this binary carries rewrites every declaration row the
repository holds. That is the one step of an upgrade a rollback cannot undo, so
the dump you take beforehand is the only way back.

**A dialect promotion is one transaction, and it is one-way.** Each repository
carries a monotonic dialect integer. When a binary's maximum is above the stored
one, the first open of that repository runs the promotion and stamps the new
number **inside the same transaction as the row rewrite**, indivisibly: a crash
leaves the store wholly on the old dialect and the next open tries again, and a
store can never hold new rows under an old stamp.

**Downgrading after that open is impossible without a restore.** An older binary
meeting the newer stamp refuses to open the repository. That is the named
refusal, which the API surfaces as `503 unavailable` with a `Retry-After`, never
as an invalid token, so a store the binary cannot serve is diagnosable rather
than mysterious. That refusal is the *good* outcome: it exists because the older
binary would otherwise misread the migrated rows. Rolling the image back is
therefore not a fix; restoring the pre-upgrade dump is. Rolling *forward* to a
binary whose maximum covers the stamp still is.

**The promotion refuses rather than guesses.** It translates every declaration
row this repository holds, and if one installed closure no longer parses under
the new binary it fails the open, logging the authority and the reason, instead
of migrating the rest: stamping a store with one un-migrated row would leave two
encodings in it, and no reader could tell which one it was holding. The repair is
to re-install that bundle (or open once under the binary that wrote it) before
the new binary migrates the repository. A repository whose only trouble is a
tightened contract is a different case, below.

[Quarantine](vocabulary.md#quarantine) is that other case: a binary that tightens
a contract quarantines each installed bundle whose stored closure no longer
admits, rather than bricking the repository, and re-installing the bundle (or a
later open under a binary that relaxed the contract) clears the marker.
Quarantine is a state a migrated repository may reach, never one it may be
migrated in.

**A migration this binary does not recognize stops the boot.** The runner
records each migration's sha256 as it applies it, and every boot compares the
recorded hashes against the files the binary carries. A difference means the
database applied a migration whose text has changed since, so the binary
refuses before applying anything pending: a new migration must not land on a
schema its predecessors did not build. The refusal names every migration that
diverges, with both hashes, rather than the first one it meets.

A released binary never triggers this, because a landed migration is never
edited. What does trigger it is a database migrated by a build from a branch
that was still revising its migration. Throw such a database away:
`mise run dev:wipe` for a development one, a restore from a dump a matching
binary wrote for anything else. There is no repair, because two branch
revisions of one migration can differ in any way at all.

The one sanctioned exception is a migration corrected before it landed.
`supersededSHA256` in `internal/engine/migrate.go` names the hash the branch
file had, and a later migration adds whatever that revision lacked, so a
database carrying the old hash boots and catches up. `0007_signed_from_positive`
is the only such catch-up: it adds the CHECK constraint that `0005` gained four
minutes before it merged.

## Backups

**A backup is the changelog plus blobs plus sealed, as one unit.** Under the
default `postgres` blob store all three live in the one database, so an
ordinary consistent dump of that database is a complete backup. The sealed rows
encrypt under each repository's own
data-encryption key, which exists in two wraps: the control-plane
`repositories.dek` column holds it wrapped under `SUBSTRATE_CREDENTIAL_KEY`
(the host's half), and the repository's `recoverykey` record holds it
wrapped to the user's age recipient (the user's half). Either the host key
or the user's recovery identity opens a backup; losing both makes the
sealed rows inert.

**The host's half is a real wrap on anything this release wrote.** Changelog
signing is mandatory, so a host without `SUBSTRATE_CREDENTIAL_KEY` refuses to
boot and cannot create or open a repository at all. A store an earlier,
keyless build wrote is the exception and the dangerous one: its
`repositories.dek` holds the key in the clear beside the rows it opens, so
the dump alone is every secret in that repository, and nothing re-wraps that
column today ([#230](https://github.com/geoah/substrate/issues/230)).

**What is sealed is the sealed store, and nothing else.** Blob bytes are stored
as they arrived on every backend — the `blobs.bytes` column included — so a
dump of a substrate on the default backend carries every attachment in the
clear, whatever the credential key is doing. The changelog and the records
folded from it are plaintext for the same reason. Encrypt the backup itself, or
the storage under it; the substrate does not.

**On `fs` or `s3` the dump is half the backup.** The blob bytes are outside the
database, so the second artifact is the fs root or the bucket, and it has to be
copied with its own step:

- **`fs`**: back up the whole root (`rsync`, a snapshot of the volume, a
  tarball). Objects are immutable and named by their content digest, so an
  incremental copy is exact and a partially copied file is detectable by
  hashing it.
- **`s3`**: the bucket is the artifact. Turn on versioning or replication, or
  copy the prefix to a second bucket. A provider's durability is not a backup:
  a delete this substrate issues is a delete.

**Take the blob copy FIRST, then the dump.** Bytes settle before the manifest
does, so a store copied before the dump can hold objects the dump has no
manifest for, which the sweep collects harmlessly. The other order can leave a
`stored` manifest whose bytes were never copied, and that is a blob the restore
cannot serve.

What you do **not** have to back up separately is the fold. The records table
and its indexes are derived; the changelog is the truth.

## Operator recovery

Operator commands (the "operator hat" of
[substratectl](substratectl.md#two-hats)) speak to Postgres directly over the
DSN and hold no token. They need `--dsn` (or `DATABASE_URL`), and refuse
before touching anything without one.

```
DATABASE_URL=… substratectl repository list
DATABASE_URL=… substratectl repository inspect ada
DATABASE_URL=… substratectl repository verify ada
DATABASE_URL=… substratectl repository rebuild ada
SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… substratectl repository reseal ada
SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… substratectl user reset ada
```

**On the compose deployment, run them inside the container.** Both runtime
images carry `substratectl` beside the server, because `compose.yaml`
publishes no Postgres port and the DSN resolves nowhere else. The container
already holds `DATABASE_URL` and `SUBSTRATE_CREDENTIAL_KEY` in its
environment, so neither is repeated on the command line:

```
docker compose exec substrate substratectl repository list
docker compose exec substrate substratectl repository verify ada
docker compose exec substrate substratectl user reset ada
```

`reseal` and `user reset` refuse on a deployment left at the out-of-the-box
default, because that default sets no `SUBSTRATE_CREDENTIAL_KEY` and both write
sealed material. Set the key in the environment compose reads, then
`docker compose up -d`, which recreates the container around the new value.
Not `docker compose restart`: that restarts the process the container already
has, with the environment it was created with, so both commands go on refusing
and nothing says why.

Publishing the Postgres port to reach the same commands from the host is a
worse trade: it exposes the database to everything that can reach the host, and
the exec path needs nothing open at all.

- **`repository list`** reads the one control-plane table: one row per user.
- **`repository inspect <username>`** reports the repository id, the username,
  when it was created, the changelog head and entry count, live and tombstoned record
  counts, and the declaration versions per authority. It is the first thing to
  run when something looks wrong.
- **`repository verify <username>`** walks the whole chain in one read-only
  snapshot: it recomputes every entry's hash from the stored bytes, checks
  every signature the signing state requires, checks the chain epochs, and
  reports either the verified head `(seq, hash)` or every finding by seq and
  name. An all-zero signature is a finding wherever it sits: at or after the
  activation seq it is a stripped signature, and below it (history a
  pre-signing release wrote, which nothing sanctioned can sign after the
  fact) it gets one line naming the count. A repository that never activated
  at all is a finding too. It never backfills or repairs the repository it judges (opening the
  engine still applies pending schema migrations, as every operator command
  does), and run beside an in-flight first-open backfill it can report
  transient unhashed entries — re-run once the open settles. **Write the
  head down somewhere else and pass it back**: `--expect-head seq:hash`,
  `--expect-public-key` and `--expect-signed-from` turn your out-of-band
  knowledge into enforced findings, and a pinned head is the only way to
  catch a truncated tail. Run it before and after a Postgres major upgrade,
  and on every restored backup. Exits nonzero on any finding.
- **`repository rebuild <username>`** replays the whole changelog into a fresh fold,
  in one transaction, under that repository's own lock. It reproduces the fold
  bit for bit and appends nothing, so it is safe to run on a healthy
  repository. **It verifies the chain first** and refuses to install history
  that does not check out; `--force-unverified` overrides that, says so in its
  output, and is for the day you have decided the bytes are what you have.
  It does not touch blobs or sealed rows — those were never in the
  changelog — and it leaves runtime state (trigger cursors, OAuth flows) alone,
  because a cursor is a consumer's position in the changelog, not a fold of it.
- **`repository reseal <username>`** moves every legacy secret value into
  the sealed store and re-points record rows and the changelog's stored
  payloads at the refs; it also upgrades sealed-store payloads written while
  the server ran without `SUBSTRATE_CREDENTIAL_KEY`. Run it once after
  upgrading past the release that moved secrets into the store: the
  changelog is append-only, so plaintext it already carries can be removed by
  nothing else. Values-only and idempotent, and it refuses until the server
  has opened the repository once under the upgraded binary. Because it is the
  one sanctioned rewrite of history, it VERIFIES THE CHAIN FIRST and refuses
  over history that does not check out (a reseal would otherwise launder
  tampering into fresh hashes and signatures), then re-chains (and re-signs)
  every entry from the first rewritten seq and records a `reseal` chain
  epoch naming the old head and the new: a head you wrote down before a
  reseal will not match after, and the epoch is what explains it. On a
  signed repository it refuses without the signing key.
- **`blobs migrate`** moves blob bytes from one store to another, one
  repository at a time, and is the only way across a
  `SUBSTRATE_BLOB_STORE` change: see [the blob store](#the-blob-store). It
  writes no records and appends no changelog entries, because the manifest
  never moves.
- **`user reset <username>`** is the answer to a user who has lost both
  factors. It writes fresh sealed material and a new credential record and
  prints a fresh TOTP enrollment. The data is untouched; the account gets new
  keys. There is no self-serve recovery, deliberately.

**A lost second factor is an operator's job, and only an operator's.** Every
credential-change endpoint requires the current TOTP code, and no route resets
a credential from the recovery key: the recovery key opens the sealed store's
data-encryption key, not the login. So `user reset`, run on the box or through
`docker compose exec`, is the whole of the escape from a lockout in v1. A
deployment nobody can exec into is a deployment where a lost authenticator is
permanent.

The signing seed has one copy outside the database: registration hands it to
the user, once ([the chain](changelog.md#the-chain)). With it the user can
derive the public key and verify a backup's signatures with no server, and
feed `verify --expect-public-key` from their own record of it. It does not
soften the credential-key rule: a lost credential key still stops writes on an
activated repository, and no path consumes a user-provided seed today (an
operator re-seal from that copy is a possible follow-up, not built).

Two rules keep operator commands honest, and they explain the output: the CLI
opens the engine with an empty registry, so an operator command can never
overwrite a repository's stored vocabulary with the declarations compiled into
the CLI's own build; and its reads assume the `substrate_app` role, because
row level security does
not bind a superuser and an operator DSN usually is one — without that,
`inspect` would count every repository's rows and report them as one user's.

## What it does not do

No sharing, no second user reading your repository, no cross-repository query.
No erasure, compaction, or retention policy: the changelog keeps everything, and the
horizon stays 0. The chain and its signatures are tamper EVIDENCE, not tamper
proofing, and never evidence against the host operator, who holds the database
and the credential key alike ([what the chain proves](changelog.md#the-chain)).
Each of those is a deliberate absence, not an oversight.

Next: [the live tests](testing.md), the one suite that talks to real LLM
providers.
