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
| `SUBSTRATE_DATA_ROOT`          | required                               | The directory every repository's files live under: `repositories/<id>/` with the manifest, the changelog segments, the sealed store's files and (on the `fs` blob store) the blob bytes. See [the repository directory](#the-repository-directory). It must be an absolute path, it must outlive the container, and a host without one refuses to boot, naming the variable. |
| `SUBSTRATE_CHANGELOG_SEGMENT_BYTES` | `268435456`                       | The size past which the active changelog segment rotates: the writer fsyncs, writes the finished file's `.sha256` sidecar and opens the next segment. At least 1 MiB. |
| `SUBSTRATE_CREDENTIAL_KEY`     | required                               | Wraps each repository's data-encryption key (DEK), which encrypts the sealed store: every secret-typed property's material, the password hash, the TOTP seed and stored provider tokens (AES-256-GCM). It is key material, not a passphrase: base64 of exactly 32 bytes, the AES-256 key itself. Generate one with `openssl rand -base64 32`; a host whose key is empty or any other shape refuses to boot, naming the variable (ADR [0024](decisions/0024-the-credential-key-is-key-material-not-a-passphrase.md)). A host whose key does not open the wrapped DEKs the store already holds refuses to boot too, naming the repositories: that is a wrong key or a store from somewhere else, and nothing here can be re-keyed. |
| `SUBSTRATE_INSECURE_DISABLE_TOTP` | `false`                             | **Local development only.** Stops verifying the second factor, so a password is the whole credential: see [the local TOTP-off switch](auth.md#the-second-factor-can-be-switched-off-locally). Boots with a warning, and `GET /.well-known/substrate/server.json` says so. |
| `SUBSTRATE_OAUTH_STATE_KEY`    | —                                      | Signs OAuth flow state. Unset mints a random key per boot, with a warning: flows in progress break on restart. |
| `SUBSTRATE_OAUTH_CALLBACK_URL` | —                                      | The one redirect URI every provider app registers.                                                        |
| `SUBSTRATE_CONSOLE_URL`        | —                                      | The console origin the OAuth return-page posts to and falls back to redirecting into. Empty is local dev. |
| `SUBSTRATE_SANDBOX`            | `best-effort`                          | How hard to confine function bodies: `off`, `best-effort`, or `enforce` (refuse to run a body unconfined). |
| `SUBSTRATE_SANDBOX_EGRESS_ALLOW` | —                                   | A comma-separated list of CIDRs (or bare addresses) a network body may reach despite the private-range block. A body that declares `permissions.network` reaches the public internet but not the deployment's own loopback, link-local or RFC1918 ranges, so a local provider (a loopback Ollama) needs its address listed here. Empty blocks every private range. |
| `SUBSTRATE_EGRESS_ALLOW`       | —                                      | A comma-separated list of CIDRs (or bare addresses) the SERVER may dial for a repository-chosen URL despite the private-range block. An `llmprovider` row's `baseURL` is written by the repository owner, so the engine confines its completion and embedding dials to public destinations, refusing the deployment's own loopback, link-local, RFC1918 and CGNAT ranges at connect time (issue #241). A local provider (a loopback Ollama) needs its address listed here. Empty blocks every private range. This is the server's own dials; `SUBSTRATE_SANDBOX_EGRESS_ALLOW` is the separate escape for a function body's dials. |
| `SUBSTRATE_BLOB_STORE`         | `fs`                                   | Where blob bytes live: `fs` (under the repository directory in the data root) or `s3` (a bucket). `postgres` is refused at boot. See [the blob store](#the-blob-store). |
| `SUBSTRATE_BLOB_S3_ENDPOINT`   | —                                      | `s3` only: the service URL, scheme included (`https://s3.us-east-1.amazonaws.com`, or a self-hosted endpoint).                       |
| `SUBSTRATE_BLOB_S3_BUCKET`     | —                                      | `s3` only: the bucket. It must be PRIVATE — the bytes are stored as they arrived.                                                    |
| `SUBSTRATE_BLOB_S3_REGION`     | `us-east-1`                            | `s3` only: the region the request is signed for.                                                                                     |
| `SUBSTRATE_BLOB_S3_ACCESS_KEY_ID` / `SUBSTRATE_BLOB_S3_SECRET_ACCESS_KEY` | — | `s3` only: the credentials every request is signed with. `SUBSTRATE_BLOB_S3_SESSION_TOKEN` beside them for temporary ones.        |
| `SUBSTRATE_BLOB_S3_PREFIX`     | —                                      | `s3` only: a key prefix, for a bucket this substrate shares with something else.                                                     |
| `SUBSTRATE_BLOB_S3_PATH_STYLE` | `true`                                 | `s3` only: address the bucket as a path segment rather than a subdomain. Self-hosted endpoints want it; AWS accepts it.               |

`SUBSTRATE_CREDENTIAL_KEY` is the one that must be backed up apart from the
data root: without it, sealed material is unreadable
([backups](#backups)).

## The repository directory

Every repository owns one directory under the data root, named by its id, and
that directory is the truth on disk and the unit a backup copies
([decision 0051](decisions/0051-a-repository-directory-is-the-backup-unit.md)):

```
$SUBSTRATE_DATA_ROOT/
  repositories/
    <repository id>/
      repository.json               # the manifest: id, username, authority, createdAt, changelogDialect, the wrapped DEK
      changelog/
        000000000000001.ndjson      # a segment, named by its first seq; the highest is the active one
        000000000000001.ndjson.sha256   # the digest of a finished segment
        000000000482113.ndjson
      blobs/
        blob-sha256-<hex>           # the bytes, content addressed (the fs blob store)
      sealed/
        secret-<hex>.json           # one file per sealed row: the ref with ':' as '-'
```

The changelog segments are newline-delimited JSON, one entry per line, each
line carrying its own SHA-256 checksum
([the checksum and the segment files](changelog.md#the-checksum-and-the-segment-files)).
`repository.json` carries the username, so `grep -l '"username": "ada"'
$SUBSTRATE_DATA_ROOT/repositories/*/repository.json` finds a person's
directory, and the DEK wrapped under `SUBSTRATE_CREDENTIAL_KEY`, the same
bytes as the `repositories.dek` column, so a copy restored onto a host with
the same key opens without anything else. The key itself is never in the
directory.

Postgres is the commit point and the live index: every write commits to the
`changelog` table first, and the repository's one writer then appends the same
entries to the active segment and mirrors the `sealed` table to `sealed/`.
Blob bytes go straight to `blobs/`. At boot the server compares every
directory with every `repositories` row ([what happens at boot](#what-happens-at-boot)).

The root is as private as its mode. The server creates directories `0700` and
files `0600`; give the root to the substrate's user alone, because anything
that can read it reads every repository's changelog and blobs in the clear
([0031](decisions/0031-blob-bytes-outside-postgres-are-stored-plaintext.md)).
Only the sealed files are ciphertext. Encrypt the volume for encryption at
rest; the substrate does not.

## There is no LLM configuration

The server takes no LLM endpoint, no key and no embedding model. Completions
and embeddings alike are bought through a repository's own
[`llmprovider`](agents.md#providers) records, which carry the wire, the
endpoint, the key and (for embeddings) the model. The process holds no bearer,
so no host-wide key can reach a repository-chosen endpoint.

What that means for an operator:

- A fresh repository has no agents and no semantic search until its owner
  writes a provider row. Nothing seeds one; the
  [LLM sample bundle](bundles-catalog.md#llm-sample) ships two ready to key.
- Semantic search runs against the one row that declares `embedModel`, and
  hybrid search returns its lexical arm alone until that row exists.
- Every stored vector names the row and the model that produced it. Change
  either and the older vectors stop being searched, which is deliberate: cosine
  distance between two models' vectors is not a distance. Run
  `substratectl --dsn … repository reembed <username>` to queue their
  replacement, or `POST
  /api/v1/embeddings/reembed` from the repository's
  own token. Both write queue rows; the server's drain loop buys the vectors a
  batch at a time, so an interrupted re-embed resumes by itself.
- A gateway swapped behind an unchanged row and model name is invisible to the
  provenance columns, so that case takes `reembed --all`.

## The blob store

A blob is two halves: a **manifest**, which is an ordinary record keyed by the
content digest, and the **bytes**. The manifest is always in Postgres and is
always the truth. `SUBSTRATE_BLOB_STORE` says where the bytes go.

| Backend            | Where the bytes are                                        | Backup                                  |
| ------------------ | ---------------------------------------------------------- | --------------------------------------- |
| `fs`               | `$SUBSTRATE_DATA_ROOT/repositories/<id>/blobs/<digest>`    | the repository directory, and nothing else |
| `s3`               | `<prefix><repository id>/<digest>` in the bucket           | the directory **plus** the bucket        |

`fs` is the default: the bytes sit in the repository directory beside the
changelog, so one copy of the directory is a whole backup. `s3` is for a
deployment whose disk cannot hold the attachments, and it makes the backup two
artifacts. `postgres`, the `blobs` bytea column, is not a runtime store any
more: a server configured with it refuses to boot, and so does a server whose
`blobs` table still holds byte rows, naming
`substratectl blobs migrate --from postgres` as the way out.

**Isolation is not the database's job here.** The repository is half of every
key, and it comes from the authenticated token's repository, never from the
request: a read resolves the manifest first, under row level security, and only
then fetches bytes. So a caller cannot reach another repository's blob by
guessing a digest. But anything that can read the data root or the bucket can
read every repository's blobs: the store is as trusted as the database. Keep
the bucket private, with credentials only this substrate holds.

**Blob bytes are never sealed, on any backend.** The sealed store covers
secret-typed properties; an object on disk and an object in a bucket are stored
exactly as they arrived, and no credential key is involved in reading either
([0031](decisions/0031-blob-bytes-outside-postgres-are-stored-plaintext.md)).
Whoever holds the directory or the bucket holds every attachment in the clear.
For encryption at rest, put it under the store: disk encryption for the data
root, the bucket's own server-side encryption for `s3`.

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
404s for half the blobs. Move them first, with the server stopped. A store
upgraded from a release that kept bytes in the database runs this once:

```
SUBSTRATE_BLOB_STORE=fs SUBSTRATE_DATA_ROOT=/var/lib/substrate \
  DATABASE_URL=… substratectl blobs migrate --from postgres
```

It moves one repository at a time and deletes each object from the source only
once the target holds it, so an interrupted run is finished by running it
again. `--dry-run` counts what would move; a username moves that user alone.
Then start the server with the same `SUBSTRATE_BLOB_STORE`. Moving between
`fs` and `s3` is the same command with `--from` and `--to` naming them.

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
- **The data root is reconciled with the `repositories` table**, directory
  by directory and row by row, before anything else writes. Five cases: a
  directory and a row whose heads and last checksums agree open; a table ahead
  of its file (a crash between commit and append) has the missing entries
  appended to the file and its sealed files rewritten from the table; a file
  ahead of its table, or a directory with no row, is **imported**, which
  creates the row from `repository.json`, loads `sealed/` into the table,
  inserts the missing entries with their checksums and folds them through
  `fold.go` (this is the restore path, and the only one); a seq present in
  both with different checksums, a line whose `sum` does not verify or a
  finished segment whose sidecar does not match **refuses the boot**, naming
  the repository and the seq or the file, and repairs nothing (one refusal an
  operator reads, rather than a repository half-open beside the others); a
  row with no directory has its directory written out from the tables, once,
  which is how a store from a release before the data root gets one. A
  directory with no row and no `repository.json` is logged and left alone:
  nothing says whose it is, so it is neither imported nor deleted.
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
scale. The segment files lean on the same shape: one writer process per data
root. The writer holds an exclusive advisory lock on
`<repository>/changelog/.lock` for as long as the repository is open, so a
second process that opens a repository for writing is refused with a named
error instead of appending behind the first one's back.

## Upgrading the binary

**Take a backup before you deploy** ([backups](#backups): the data root and a
database dump, together). A repository's first open under a new binary may
promote its stored
[vocabulary dialect](vocabulary.md#vocabulary-evolution-and-the-dialect-contract),
and the promotion this binary carries rewrites every declaration row the
repository holds. That is the one step of an upgrade a rollback cannot undo, so
the copy you take beforehand is the only way back.

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
therefore not a fix; restoring the pre-upgrade copy is. Rolling *forward* to a
binary whose maximum covers the stamp still is.

**The changelog carries a dialect of its own, and it refuses the same way.**
Beside the vocabulary stamp each repository carries a
[changelog dialect](changelog.md#the-dialect-a-changelog-is-written-in): what a
binary must understand to replay its entries. A binary claims it in the first
transaction it appends with, so an older binary meeting a newer stamp refuses
the open instead of serving a history it could not rebuild, while a new binary
that opened a repository and wrote nothing leaves the rollback open. Nothing is
rewritten and there is no promotion step: a changelog is append-only, so old
entries keep the spelling they were written in.

**The promotion refuses rather than guesses.** It translates every declaration
row this repository holds, and if one installed closure no longer parses under
the new binary it fails the open, logging the package and the reason, instead
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
`mise run dev:wipe` for a development one, a restore from a backup a matching
binary wrote for anything else. There is no repair, because two branch
revisions of one migration can differ in any way at all.

The one sanctioned exception is a migration corrected before it landed.
`supersededSHA256` in `internal/engine/migrate.go` names the hash the branch
file had, and a later migration adds whatever that revision lacked, so a
database carrying the old hash boots and catches up. `0007_signed_from_positive`
is the only such catch-up: it adds the CHECK constraint that `0005` gained four
minutes before it merged.

## Backups

**A backup is the data root plus the credential key, kept apart.** Every
repository's directory under `$SUBSTRATE_DATA_ROOT/repositories/` holds its
changelog, its sealed store and (on the `fs` blob store) its blob bytes
([the repository directory](#the-repository-directory)), and nothing in the
database is needed to bring it back: the `changelog` table is an index of the
files, the `records` table is their fold, and both are rebuilt on import. A
copy of the directory and the key that opens its sealed files is a complete
backup.

**Copy the root at any moment, then verify the copy.** Finished segments and
blobs never change, the active segment only grows, and the manifest, the
sidecars and the sealed files are replaced atomically, so a copy taken
mid-write is usually consistent or short by one torn last line, which the
importer discards. Two windows remain: a copy that reads a segment while the
server finishes it can hold the segment with a sidecar that does not match
yet, and a copy that reads `sealed/` before `changelog/` can hold a line whose
sealed file it missed. So a copy is a backup once `repository verify` passes
on it (boot a scratch server over the copy with an empty database, which
imports it, then verify); one that fails is retaken. A cron running this is
enough:

```
rsync -a --delete "$SUBSTRATE_DATA_ROOT"/ backup-host:/srv/substrate-backup/
```

On the compose deployment the root is the `substrate-data` volume mounted at
`/var/lib/substrate`, so copy it out of the container, or point the volume at a
host directory the backup already covers:

```
docker compose cp substrate:/var/lib/substrate ./substrate-backup
```

**Keep `SUBSTRATE_CREDENTIAL_KEY` somewhere the copy is not.** Every
`repository.json` carries the repository's data-encryption key wrapped under
it, and every file in `sealed/` is ciphertext under that DEK. The directory
without the key is every record and every attachment in the clear and no
secret; the key beside the directory is every secret too. On compose the key
is the `substrate-keys` volume (`/keys/credential.key`) unless the environment
sets one. The user's own recovery key wraps the same DEK in the repository's
`recoverykey` record, but no shipped command opens a backup from it yet
([#137](https://github.com/geoah/substrate/issues/137)), so losing the host key
leaves the sealed files inert.

**A database dump is optional, and it is not a restore.** The tables hold
nothing the directory lacks except runtime state (below). Take one beside the
copy before an upgrade, because a dump plus the matching directory is the
fastest way back to a known state, but a fresh database and the directory are
enough.

**Restore.** Stop the server. Copy the repository directories into a fresh
server's data root, set the same `SUBSTRATE_CREDENTIAL_KEY`, and boot: a
directory with no row in `repositories` is imported, which creates the row from
its manifest, loads `sealed/` into the table, inserts every changelog entry
with its checksum and folds them through `fold.go`. Then verify each one:

```
rsync -a ./substrate-backup/repositories/ "$SUBSTRATE_DATA_ROOT"/repositories/
SUBSTRATE_DATA_ROOT=… SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… substrate   # imports at boot
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository list
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository verify ada     # once per repository
```

A directory whose files do not verify (a bad `sum`, a sidecar that does not
match) refuses the boot with the repository and the seq or the file named;
move that directory out of the root or restore it from an older copy, then
boot again. A directory whose `repository.json` carries a DEK the host's
`SUBSTRATE_CREDENTIAL_KEY` does not open refuses the boot the same way, naming
the repository and the variable, because importing it would create a
repository no login could open. Under the `s3` blob store the bucket is the
second artifact: restore it too, or the manifests come back `stored` with no
bytes behind them.

**What does not come back.** Runtime state is not in the directory: trigger
cursors, paged cursors, embeddings and OAuth flows in flight. On an import
into an empty database triggers start at the head, so a delivery that had not
settled before the copy does not run. On an import of a newer directory over
an older database dump the cursors the dump holds stay where they were, so
every entry since the dump is delivered again. Embeddings are re-bought by the
drain loop; a consent flow in flight is started again. A user's tokens are
records, so they come back.

**Encrypt the copy.** The changelog and the blobs are plaintext in the
directory, on the backup host and in the dump alike. The substrate does not
encrypt the storage under it; do that yourself.

## Operator recovery

Operator commands (the "operator hat" of
[substratectl](substratectl.md#two-hats)) speak to Postgres and the data root
directly and hold no token. They need `--dsn` (or `DATABASE_URL`) and
`SUBSTRATE_DATA_ROOT`, and refuse before touching anything without them.

**Three of them run beside a live server; two need it stopped.** `repository
list`, `repository inspect` and `repository verify` read: `verify` opens the
engine read-only, so it runs no boot check, appends nothing and reports a torn
tail or a table ahead of its file as a finding instead of repairing it.
`repository rebuild` and `user reset` write, so each opens the repository as
its changelog writer, and a running server holds that lock: the command
refuses, naming the lock, until the server is stopped.

```
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository list
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository inspect ada
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository verify ada
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository rebuild ada
SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl user reset ada
```

**On the compose deployment, run them inside the container.** Both runtime
images carry `substratectl` beside the server, because `compose.yaml`
publishes no Postgres port and the DSN resolves nowhere else. The container
already holds `DATABASE_URL`, `SUBSTRATE_DATA_ROOT` and
`SUBSTRATE_CREDENTIAL_KEY` in its environment, so none is repeated on the
command line:

```
docker compose exec substrate substratectl repository list
docker compose exec substrate substratectl repository verify ada
docker compose exec substrate substratectl user reset ada
```

Publishing the Postgres port to reach the same commands from the host is a
worse trade: it exposes the database to everything that can reach the host, and
the exec path needs nothing open at all.

- **`repository list`** reads the one control-plane table: one row per user.
- **`repository inspect <username>`** reports the repository id, the username,
  when it was created, the changelog head in the table and the head in the
  segment files with the segment count, live and tombstoned record counts,
  and the declaration versions per package. Two heads that differ are the gap
  the next boot closes. It is the first thing to run when something looks
  wrong.
- **`repository verify <username>`** walks the segment files: every line's
  `sum`, every finished segment's sidecar, the seq order, and both heads
  against each other. It reports the head `(seq, checksum)` or every finding
  by seq or file name, never repairs the repository it judges (opening the
  engine still applies pending schema migrations, as every operator command
  does), and exits nonzero on any finding. It is safe beside a running
  server; a finding about the heads taken mid-write can be a transaction in
  flight, so run it twice before believing one. Run it on every restored
  copy and before and after a Postgres major upgrade. It proves the files
  are undamaged and agree with the table; it does not prove who wrote them
  ([the checksum](changelog.md#the-checksum-and-the-segment-files)).
- **`repository rebuild <username>`** replays the segment files into a fresh
  fold, in one transaction, under that repository's own lock, after running
  the same check the boot runs. It reproduces the fold bit for bit and appends
  nothing, so it is safe to run on a healthy repository, and it is the proof
  that the directory alone reproduces the records. It does not touch blobs or
  sealed files, which were never in the changelog, and it leaves runtime
  state (trigger cursors, OAuth flows) alone, because a cursor is a
  consumer's position in the changelog, not a fold of it. Stop the server
  first: it opens the repository as its changelog writer and refuses while
  the server holds the lock.
- **`blobs migrate`** moves blob bytes from one store to another, one
  repository at a time, and is the only way across a
  `SUBSTRATE_BLOB_STORE` change: see [the blob store](#the-blob-store). It
  writes no records and appends no changelog entries, because the manifest
  never moves.
- **`user reset <username>`** is the answer to a user who has lost both
  factors. It writes fresh sealed material and a new credential record and
  prints a fresh TOTP enrollment. The data is untouched; the account gets new
  keys. There is no self-serve recovery, deliberately. Like `rebuild` it
  needs the server stopped, because the credential record is a changelog
  entry.

`user reset` refuses on a deployment whose `SUBSTRATE_CREDENTIAL_KEY` the
container was not created with, because it writes sealed material. Set the key
in the environment compose reads, then `docker compose up -d`, which recreates
the container around the new value. Not `docker compose restart`: that restarts
the process the container already has, with the environment it was created
with, so the command goes on refusing and nothing says why.

**A lost second factor is an operator's job, and only an operator's.** Every
credential-change endpoint requires the current TOTP code, and no route resets
a credential from the recovery key: the recovery key opens the sealed store's
data-encryption key, not the login. So `user reset`, run on the box or through
`docker compose exec`, is the whole of the escape from a lockout in v1. A
deployment nobody can exec into is a deployment where a lost authenticator is
permanent.

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
horizon stays 0. No signatures and no hash chain: the per-entry checksum
catches corruption, and nothing stored is evidence against the host operator,
who holds the database, the data root and the credential key alike
([the checksum](changelog.md#the-checksum-and-the-segment-files)).
Each of those is a deliberate absence, not an oversight.

Next: [the live tests](testing.md), the one suite that talks to real LLM
providers.
