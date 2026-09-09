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
| `SUBSTRATE_DATA_ROOT`          | required                               | The directory every repository's files live under: `repositories/<authority>/` with the manifest, the changelog segments, the sealed store's files and (on the `fs` blob store) the blob bytes. See [the repository directory](#the-repository-directory). It must be an absolute path, it must outlive the container, and a host without one refuses to boot, naming the variable. |
| `SUBSTRATE_CHANGELOG_SEGMENT_BYTES` | `268435456`                       | The size past which the active changelog segment rotates: the writer fsyncs, writes the finished file's `.sha256` sidecar and opens the next segment. At least 1 MiB. |
| `SUBSTRATE_CONVERSION_CEILING` | `10000`                                | The most live records one declaration change (a vocabulary apply, a provider upgrade, the boot upgrade) may rewrite in its transaction ([vocabulary evolution](vocabulary.md#backfilling-and-remapping)). A plan above it is refused and the previews list the refusal; `0` removes the ceiling. |
| `SUBSTRATE_CREDENTIAL_KEY`     | required                               | Wraps each repository's data-encryption key (DEK), which encrypts the sealed store: every secret-typed property's material, the password hash, the TOTP seed and stored provider tokens (AES-256-GCM). It is key material, not a passphrase: base64 of exactly 32 bytes, the AES-256 key itself. Generate one with `openssl rand -base64 32`; a host whose key is empty or any other shape refuses to boot, naming the variable (ADR [0024](decisions/0024-the-credential-key-is-key-material-not-a-passphrase.md)). A host whose key does not open the wrapped DEKs the store already holds refuses to boot too, naming each repository, the id of the key its wrap was written under and the id of the key this host holds (`repositories.dek_key_id`: 16 hex digits of a one-way hash over the key, never the key): that is a wrong key or a store from somewhere else. No command re-wraps a live repository's DEK under another host key; a copied directory moves between keys through `repository rewrap` ([restore without the credential key](#restore-without-the-credential-key)). |
| `SUBSTRATE_INSECURE_DISABLE_TOTP` | `false`                             | **Local development only.** Stops verifying the second factor, so a password is the whole credential: see [the local TOTP-off switch](auth.md#the-second-factor-can-be-switched-off-locally). Boots with a warning, and `GET /.well-known/substrate/server.json` says so. |
| `SUBSTRATE_OAUTH_STATE_KEY`    | —                                      | Signs OAuth flow state. Unset mints a random key per boot, with a warning: flows in progress break on restart. |
| `SUBSTRATE_OAUTH_CALLBACK_URL` | —                                      | The one redirect URI every provider app registers.                                                        |
| `SUBSTRATE_CONSOLE_URL`        | —                                      | The console origin the OAuth return-page posts to and falls back to redirecting into. Empty is local dev. |
| `SUBSTRATE_SANDBOX`            | `best-effort`                          | How hard to confine function bodies: `off`, `best-effort`, or `enforce` (refuse to run a body unconfined). |
| `SUBSTRATE_SANDBOX_EGRESS_ALLOW` | —                                   | A comma-separated list of CIDRs (or bare addresses) a network body may reach despite the private-range block. A body that declares `permissions.network` reaches the public internet but not the deployment's own loopback, link-local or RFC1918 ranges, so a local provider (a loopback Ollama) needs its address listed here. Empty blocks every private range. |
| `SUBSTRATE_EGRESS_ALLOW`       | —                                      | A comma-separated list of CIDRs (or bare addresses) the SERVER may dial for a repository-chosen URL despite the private-range block. An `llmprovider` row's `baseURL` is written by the repository owner, so the engine confines its completion and embedding dials to public destinations, refusing the deployment's own loopback, link-local, RFC1918 and CGNAT ranges at connect time (issue #241). A local provider (a loopback Ollama) needs its address listed here. Empty blocks every private range. This is the server's own dials; `SUBSTRATE_SANDBOX_EGRESS_ALLOW` is the separate escape for a function body's dials. |
| `SUBSTRATE_BLOB_STORE`         | `fs`                                   | Where blob bytes live: `fs` (under the repository directory in the data root) or `s3` (a bucket). See [the blob store](#the-blob-store). |
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

Every repository owns one directory under the data root, named by its
authority, which is the repository's id everywhere
([decision 0052](decisions/0052-the-authority-is-the-repository-id.md)), and
that directory is the truth on disk and the unit a backup copies
([decision 0051](decisions/0051-a-repository-directory-is-the-backup-unit.md)):

```
$SUBSTRATE_DATA_ROOT/
  repositories/
    ada.example.com/                # one per repository, named by its authority
      repository.json               # the manifest: format, authority, createdAt, changelogDialect, vocabularyDialect, the wrapped DEK, dekKeyId
      snapshot.json                 # only in a snapshot, or a directory restored from one: the head seq and checksum the copy holds, and the blobs it needs
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
The directory is `repositories/ada.example.com/` for the repository whose
authority is `ada.example.com`, so a person finds it by name; `repository.json`
carries that authority, and nothing else names the repository. It also carries the DEK wrapped
under `SUBSTRATE_CREDENTIAL_KEY`, the same bytes as the `repositories.dek`
column, so a copy restored onto a host with the same key opens without
anything else. The key itself is never in the directory; `dekKeyId` names it
(16 hex digits of a one-way hash over the key), so a host holding another key
is told which key the directory wants. Every file under `sealed/` is
ciphertext under that DEK and nothing else: the server refuses a plain
payload and never tries the host key on one, so a recovery through the
recovery key is complete
([decision 0059](decisions/0059-a-marked-repository-refuses-plain-and-host-key-sealed-payloads.md)).

The manifest is also the directory's record of what a binary must understand
to read it: `changelogDialect` is the repository's
[changelog dialect](changelog.md#the-dialect-a-changelog-is-written-in) and
`vocabularyDialect` its
[vocabulary dialect](vocabulary.md#vocabulary-evolution-and-the-dialect-contract),
each the same number the repository's stamp holds. The server rewrites the
manifest when a stamp moves: at the open that stamps the vocabulary dialect,
and in the first write a new binary appends, which claims the changelog
dialect and writes the manifest before it commits or appends. So a copy of the
directory never holds segments its manifest understates, at any instant
between a new binary's first write and the next restart. There is one manifest
format, `format` 1; a manifest naming any other is refused rather than guessed
at.

Postgres is the commit point and the live index, and the directory is written
first: the repository's one writer stages the write's sealed files under
`sealed/<file>.json.pending` and its changelog lines, every byte but the last
line's newline, in the active segment and fsyncs them; the write commits to
the `changelog` table; the writer then renames the pending files into place
and writes that newline. A write is acknowledged only after all of it. A
pending sealed file and a transaction without its final newline are not the
directory's records: every reader skips them, and whether Postgres committed
them is the boot catch-up's to decide from the table, which writes a committed
one out again and drops an uncommitted one
([0062](decisions/0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md)).
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
  `substratectl --dsn … repository reembed <repository>` to queue their
  replacement, or `POST
  /api/v1/embeddings/reembed` from the repository's
  own token. Both write queue rows; the server's drain loop buys the vectors a
  batch at a time, so an interrupted re-embed resumes by itself.
- A gateway swapped behind an unchanged row and model name is invisible to the
  provenance columns, so that case takes `reembed --all`.
- A repository restored from its directory queues every embeddable property
  by itself, because the vectors were never in the directory; see
  [Backups](#backups).

## The blob store

A blob is two halves: a **manifest**, which is an ordinary record keyed by the
content digest, and the **bytes**. The manifest is always in Postgres and is
always the truth. `SUBSTRATE_BLOB_STORE` says where the bytes go.

| Backend            | Where the bytes are                                        | Backup                                  |
| ------------------ | ---------------------------------------------------------- | --------------------------------------- |
| `fs`               | `$SUBSTRATE_DATA_ROOT/repositories/<authority>/blobs/<digest>` | the repository directory, and nothing else |
| `s3`               | `<prefix><authority>/<digest>` in the bucket               | the directory **plus** the bucket        |

`fs` is the default: the bytes sit in the repository directory beside the
changelog, so one copy of the directory is a whole backup. `s3` is for a
deployment whose disk cannot hold the attachments, and it makes the backup two
artifacts. Those are the two stores; any other value of
`SUBSTRATE_BLOB_STORE` refuses the boot by name rather than falling back to
the default.

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

**Pick the store before the first upload.** `SUBSTRATE_BLOB_STORE` is read at
boot and nothing moves bytes between the two stores: a server pointed at a
store the bytes are not in serves a 404 for every blob, and a 404 reads like a
deletion. Changing it on a substrate that already holds blobs means copying
`<data root>/repositories/<authority>/blobs/` into the bucket (or back) by
hand, with the server stopped.

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
- **The sealed store has one key.** Every payload is bound-framed ciphertext
  under the repository's DEK, from its first write, so a read refuses a plain
  payload and never tries the host key, naming the framing it found and the
  key it expected
  ([decision 0059](decisions/0059-a-marked-repository-refuses-plain-and-host-key-sealed-payloads.md)).
  `repository inspect` shows the id of the host key the DEK is wrapped under.
  A payload that does not open under the DEK refuses that open, naming the
  ref: nothing can recover its material, so delete the row from `sealed` (and
  its file under `sealed/`) and have the user re-enter the secret it held (a
  provider token: reconnect the account; the login credential: `user reset`),
  then open again.
- **The data root is reconciled with the `repositories` table**, directory
  by directory and row by row, before anything else writes. Five cases: a
  directory and a row whose heads and last checksums agree open; a table ahead
  of its file (a crash between the commit and the newline that ends the
  transaction in the file, or an unfinished transaction at the end of the
  active segment, which the open cuts whole: one the process died before
  committing, or the prefix a torn write left)
  has the missing entries appended to the file, whole transactions at a time,
  and its sealed files rewritten from the table; a file
  ahead of its table, or a directory with no row, is **imported**, which
  creates the row from `repository.json`, stamps the repository with the
  `changelogDialect` and `vocabularyDialect` the manifest recorded, loads
  `sealed/` into the table, inserts the missing entries with their checksums
  and folds them through `fold.go` (this is the restore path, and the only
  one). A manifest whose `changelogDialect` or `vocabularyDialect` is above
  the binary's maximum **refuses the boot** with the same named error the open
  gives ("the changelog speaks a newer dialect than this binary can replay",
  "the store speaks a newer schema dialect than this binary"); so does a
  manifest carrying no wrapped DEK, and one whose DEK does not open every
  file under `sealed/`, named by file. Each refusal
  comes before the row is created, so a refused directory reserves its
  authority for nothing and leaves no row for a later boot to export an
  empty repository from. The import writes
  an `import_progress` row before its first batch of entries commits and
  deletes it in the transaction that commits the last fold pass. A boot that
  dies in between leaves the row, and the next boot check resumes the import
  from the table's head: no entry is inserted twice and nothing is appended.
  Until then a read-only open of the repository refuses; a seq present in
  both with different checksums, a line whose `sum` does not verify or a
  finished segment whose sidecar does not match **refuses the boot**, naming
  the repository and the seq or the file, and repairs nothing (one refusal an
  operator reads, rather than a repository half-open beside the others); a
  row with no directory has its directory written out from the tables, once,
  which is how a repository whose directory was moved away gets one back. A
  directory under `repositories/` named by an authority (`ada.example.com`)
  with no row and no `repository.json` is logged and skipped: nothing says
  whose it is, so it is neither imported nor deleted. Any other entry under
  `repositories/` (a `tmp`, a `Backup-2026`, anything whose name is not an
  authority) refuses the boot and names the entry; move it out of the data
  root.
- **Shipped vocabulary is upgraded, per repository, in one transaction**: the
  first open under a new binary appends the version diff to that repository's
  changelog under the `substrate` actor
  ([the boot-time upgrade](vocabulary.md#how-the-vocabulary-reaches-a-repository)).
- Persisted function bodies re-warm in the background. One that no longer
  prepares logs an error naming the function, and its deliveries park rather
  than the repository failing.

Five loops then run in-process: the trigger dispatcher every 5 seconds, garbage
collection every 5 minutes, OAuth refresh and finalizer processing every
minute, the resolution sweep (the recovery path for a resume that a restart or
a lost lease dropped) every 2 minutes, and the embed-queue drain every minute,
whether or not any repository holds an embedding provider yet. The GC sweep
also drops `idempotency_keys` rows past their 24 hour retention
([idempotency and retries](api.md#idempotency-and-retries)). Each enumerates
repositories and opens each one through the same row-level-security-bound pool
a request uses.

Keep it to **one replica**. The watch signal and the trigger dispatcher are
in-process, and two dispatchers would serialize on compare-and-swap rather than
scale. The segment files lean on the same shape: one writer process per data
root. The writer holds an exclusive advisory lock on
`<repository>/changelog/.lock` for as long as the repository is open, so a
second process that opens a repository for writing is refused with a named
error instead of appending behind the first one's back.

## Upgrading the binary

**Take a backup before you deploy** ([backups](#backups): the data root and a
database dump, together). An upgrade that applies a schema migration closes
the rollback for the whole database, and the copy you take beforehand is the
only way back.

**Each repository carries two dialect stamps, and each refuses a binary that
is behind it.** The
[vocabulary dialect](vocabulary.md#vocabulary-evolution-and-the-dialect-contract)
says what shape the stored declaration rows are in, and the
[changelog dialect](changelog.md#the-dialect-a-changelog-is-written-in) says
what a binary must understand to replay the entries. A binary whose maximum is
below a stored stamp refuses to open that repository, by name, and the API
surfaces the refusal as `503 unavailable` with a `Retry-After`, never as an
invalid token, so a store the binary cannot serve is diagnosable rather than
mysterious. Nothing is rewritten and there is no promotion step: a fresh
repository is stamped at the binary's maximum at its first open, and the
changelog's claim is written by the first transaction a binary appends, so a
new binary that opened a repository and wrote nothing leaves that stamp alone.

**Rolling the image back is only safe while both stamps and the schema still
fit.** An older binary refuses a repository stamped above its maxima and a
database holding a migration it does not carry (below), so the way back from
either is to restore the copy taken before the upgrade. Rolling *forward* to a
binary whose maxima cover the stamps always works.

[Quarantine](vocabulary.md#quarantine) is what a tightened contract does: a
binary that narrows what a declaration may say quarantines each installed
bundle whose stored closure no longer admits, rather than bricking the
repository. Re-installing the bundle, or a later open under a binary that
relaxed the contract, clears the marker.

**A migration this binary does not carry stops the boot.** The runner records
each migration's version, name and sha256 in `schema_migrations` as it applies
it, and every boot reads that table back before applying anything. A recorded
version the binary does not embed means a newer binary migrated the database,
and the binary refuses to open it: every step after the runner (the orphan
sweep, the declared indexes, the data root import) writes to the schema, and
an older binary does not know the shape it would be writing to. The refusal
names each such row by its recorded name, which is the migration file's name
(`0016_something`); the tree's history says which release added that file. The
repair is to run that release or a later one, or to restore the database from
the copy taken before the upgrade. This is the database's own downgrade
refusal, beside the two per-repository ones above, and it closes the rollback
even when no repository was written: a new binary that carries a migration
applies it at its first boot. The operator commands that open the engine run
the same runner, so an older `substratectl repository verify`, `repository
rebuild`, `repository reembed` or `user reset` refuses the same database;
`repository list` and `inspect` read the tables directly and do not.

**A migration this binary does not recognize stops the boot too.** The same
read compares the recorded hashes against the files the binary carries. A
difference means the database applied a migration whose text has changed
since, so the binary refuses before applying anything pending: a new migration
must not land on a schema its predecessors did not build. The refusal names
every migration that diverges, with both hashes, rather than the first one it
meets. A pending migration numbered below one the database already recorded
refuses for the same reason: the runner applies in order, and that migration
would land on a schema its successors already changed.

A released binary never triggers this, because a landed migration is never
edited. What does trigger it is a database migrated by a build from a branch
that was still revising its migration. Throw such a database away:
`mise run dev:wipe` for a development one, a restore from a backup a matching
binary wrote for anything else. There is no repair, because two branch
revisions of one migration can differ in any way at all.

## Backups

**A backup is the data root plus the credential key, kept apart.** Every
repository's directory under `$SUBSTRATE_DATA_ROOT/repositories/` holds its
changelog, its sealed store and (on the `fs` blob store) its blob bytes
([the repository directory](#the-repository-directory)), and nothing in the
database is needed to bring it back: the `changelog` table is an index of the
files, the `records` table is their fold, and both are rebuilt on import. A
copy of the directory and the key that opens its sealed files is a complete
backup. A write is acknowledged only once its changelog lines and sealed files
are on disk, so a copy taken after a response holds every write the server
acknowledged; a write the directory could not take is refused and rolled back
([0062](decisions/0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md)).

**Copy the root at any moment, then verify the copy.** Finished segments and
blobs never change, the active segment only grows, and the manifest, the
sidecars and the sealed files are replaced atomically, so a copy taken
mid-write is usually consistent or short by its last transaction, which the
importer cuts whole (every line names the seq its transaction ends at, so a
prefix of one is never taken for history, and a transaction still missing its
final newline, like a `.pending` file under `sealed/`, is a write the
directory has not committed, which the importer ignores). Three windows remain: a copy that reads a segment while the
server finishes it can hold the segment with a sidecar that does not match
yet; a copy that reads `sealed/` before `changelog/` can hold a line whose
sealed file it missed; and a copy that reads `blobs/` before `changelog/`
can hold a blob manifest marked `stored` whose bytes it missed, because an
upload writes the bytes first and the `stored` manifest after
([the blob store](#the-blob-store)), and `rsync` reads `blobs/` before
`changelog/`. So a copy is a backup once `repository verify` passes
on it (boot a scratch server over the copy with an empty database, which
imports it, then verify with `SUBSTRATE_CREDENTIAL_KEY` set); one that fails is
retaken. Verify reads every `stored` blob's bytes and hashes them, holds every
live record's secret reference to a sealed file and opens every sealed file
under the key, so a copy that missed one of those files fails after the
import, where the files alone could not tell. It proves the files
are undamaged, not that their replay is the fold they came from: an entry
written before this fix that removed a record's last label replays with the
label back, on import as on rebuild ([the caveat under `repository
rebuild`](#operator-recovery)). A cron running this is enough:

```
rsync -a --delete "$SUBSTRATE_DATA_ROOT"/ backup-host:/srv/substrate-backup/
```

**A snapshot is a copy with a recorded point, taken with the server stopped.**
`repository snapshot <repository> <destination root>` writes
`<destination root>/repositories/<authority>/`, the layout a data root has,
verified before and after: it takes the repository's writer lock (a running
server refuses it), runs the whole `repository verify` including the blob
hashes and the sealed files opened under `SUBSTRATE_CREDENTIAL_KEY` (which it
requires), refuses on any finding, copies the manifest, every segment and
sidecar, every committed sealed file and, on the `fs` blob store, the bytes
of every `stored` blob, each hashed against its digest on the way, verifies
the copy's changelog and sealed files, and writes `snapshot.json` last. That
file names the point: the head seq, that entry's checksum and when the copy
was taken, plus the blob store, the digests the copy needs and, under `s3`,
where they are. A directory carrying one is a copy that finished; the boot
ignores the file, and `repository verify` on the restored repository prints
the point and checks that the entry it names is in the files with that
checksum ([decision 0065](decisions/0065-a-snapshot-is-a-stopped-server-copy-that-records-its-head.md)).
A destination that already holds a directory for the repository is refused;
a snapshot is a fresh copy, never a merge over an older one. The copy is
built under a dot-prefixed temporary directory beside `repositories/` and
renamed into place once `snapshot.json` is on disk, so a snapshot that fails
leaves nothing at the destination and the same destination takes the retry.
The copy holds what the fold needs and nothing else: a pending upload, a
tombstoned blob's bytes and a staged sealed file are not copied. Run it with
the binary the server runs, as with `rebuild`: it opens the repository the
way the server does, so a newer `substratectl` stamps the source with its own
dialects and the older server then refuses the repository. The lock it takes
is the changelog writer's, held from the server's first open of the
repository until it exits, so a snapshot cannot slip between two
transactions of a running server; a server that has not opened the
repository yet holds nothing, and its first open fails with the lock named
until the snapshot finishes.

```
SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository snapshot ada /srv/substrate-backup/2026-09-08
```

**Under the `s3` blob store the objects are the second half of the snapshot.**
The bytes stay in the bucket, and `snapshot.json` lists them: `blobLocation`
is the repository's object prefix (`s3://<bucket>/<prefix><authority>/`) and
`blobs` every digest a `stored` manifest names, so each object is the
location plus a digest. Copy them with the directory, with the bucket's own
tooling, and copy them back into the bucket the restored server is configured
with before the boot that imports the directory; `repository verify` then
reads each one out of the bucket and hashes it, and names every object that
is missing or is not its digest's bytes. Under `fs` the bytes are in the
copy's `blobs/` and `blobLocation` is empty.

**An owner downloads the same snapshot from a running server.**
`GET /api/v1/export`, or `substratectl export`, streams the repository as a
tar laid out as a data root: `repositories/<authority>/` with
`repository.json`, `changelog/` (every finished segment with its sidecar and
the active segment cut at the point), `sealed/`, `blobs/` and, as the last
entry, `snapshot.json` recording the head seq and checksum the archive holds
([decision 0069](decisions/0069-the-owner-export-is-the-snapshot-streamed-as-a-tar.md)).
The bearer token is the whole credential: a token already reads every record
and blob the archive carries, and the sealed files in it are ciphertext under
the repository's DEK. The server pins the point under the repository's writer
lock, which every commit holds from its first byte to its final newline, and
streams the files afterwards, so writes go on during the download and the
archive still holds one committed state. It carries no host key:
`repository.json` keeps the DEK wrapped under this server's
`SUBSTRATE_CREDENTIAL_KEY`, ciphertext that opens nothing without the key and
lets a same-key restore boot with nothing else, and the `recoverykey` record
in the changelog holds the DEK wrapped to the owner's recovery key, which is
what opens the archive anywhere else. The blob bytes ride in the archive
whatever store the server runs, `s3` included, so `snapshot.json` records
`fs` and no location: an export is self-contained, because its owner has no
bucket. Restoring an export onto an `s3` host takes one more step: upload
the extracted `blobs/*` to the bucket under the repository's prefix
(`<prefix><authority>/<digest>`) before the boot that imports the directory,
or `repository verify` names every blob whose bytes the bucket lacks. One
export streams per repository at a time; a second request while one is
running answers `409 conflict`. An archive that ends before `snapshot.json` was cut short;
`substratectl export` refuses and removes one, and the server aborts the
response rather than finish a tar it could not complete.

```
substratectl export                       # writes <authority>-<head>.tar, never over an existing file
tar -x -C "$SUBSTRATE_DATA_ROOT" -f ada.example.com-1234.tar   # on a stopped server with the same key, then boot
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
`recoverykey` record, and `substratectl repository rewrap` opens a copy with it
([restore without the credential key](#restore-without-the-credential-key)),
so losing the host key leaves the sealed files inert only for a user who also
lost the recovery key.

**A database dump is optional, and it is not a restore.** The tables hold
nothing the directory lacks except the runtime state named below. Take one beside the
copy before an upgrade, because a dump plus the matching directory is the
fastest way back to a known state, but a fresh database and the directory are
enough.

**Restore.** Stop the server. Copy the repository directories into a fresh
server's data root (an export extracts straight into it: `tar -x -C
"$SUBSTRATE_DATA_ROOT" -f ada.example.com-1234.tar`), set the same
`SUBSTRATE_CREDENTIAL_KEY`, and boot: a
directory with no row in `repositories` is imported, which creates the row from
its manifest, loads `sealed/` into the table, inserts every changelog entry
with its checksum and folds them through `fold.go`. The import is the same
replay `repository rebuild` runs, so a label clear an old entry lost comes
back here too (the caveat below). Then verify each one, with the key in the
environment so every sealed file is opened; a directory that came from a
snapshot prints the recorded point (`recovery point: seq N, checksum …`) and
the head it came back at is that seq:

```
rsync -a ./substrate-backup/repositories/ "$SUBSTRATE_DATA_ROOT"/repositories/
SUBSTRATE_DATA_ROOT=… SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… substrate   # imports at boot
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository list
SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository verify ada     # once per repository
```

**Restoring a dump takes one more step.** A change cursor is a seq under the
repository's history generation
([the changelog](changelog.md#watching)), and the restore has to change that
generation whenever the history clients saved cursors against is not the one
that comes back. The import above does it by itself: creating the row from the
manifest mints a new generation. A database dump does not: the row comes back
with the generation the dump held, whether the matching directory is restored
beside it or the directory is written from the tables. So after any restore
that starts from a dump, boot once so the boot check lands (a directory ahead
of the dump is imported into the table, one behind it is written from the
table), stop the server, and rotate each repository:

```
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository rotate-generation ada
```

Every client then re-lists once at its next resume instead of continuing past
writes the restored history never had. A restart and `repository rebuild`
change nothing here.

Each directory under `repositories/` is one repository, named by its
authority: `./substrate-backup/repositories/ada.example.com/` is Ada's, and
its `repository.json` names the authority the operator commands take.

**An import that dies is resumed, not served.** The boot marks the repository
in `import_progress` before the first changelog entry lands and clears the
mark only when the last fold pass commits. A boot that dies in between (the
entries all inserted but not folded, or folded once without the references
and the weighted search index the second pass adds) leaves the mark. The next
boot check finishes the import and logs `resuming an interrupted import`:
entries already in the table are not inserted again, and nothing is appended.
The server runs that check when it starts, and so do `repository rebuild` and
`user reset`, which open the engine the same way, so each resumes the import
before its own work. A read-only process runs no boot check: opening the
repository there refuses with `the import of the repository directory has not
completed`, and `repository verify` reports the unfinished import as a finding
while the files and the rows still verify. A repository a release before this
one served empty after such a crash is repaired with `repository rebuild`.

A directory whose files do not verify (a bad `sum`, a sidecar that does not
match) refuses the boot with the repository and the seq or the file named;
move that directory out of the root or restore it from an older copy, then
boot again. A directory whose `repository.json` carries a DEK the host's
`SUBSTRATE_CREDENTIAL_KEY` does not open refuses the boot the same way, naming
the repository and the variable, because importing it would create a
repository no login could open. Under the `s3` blob store the bucket is the
second artifact: copy the objects `snapshot.json` lists back into the bucket
(above), or the manifests come back `stored` with no bytes behind them, which
`repository verify` names one blob at a time.

### Restore without the credential key

A directory copied to a host whose `SUBSTRATE_CREDENTIAL_KEY` is not the one
it was written under is opened with the user's recovery key instead: the
`AGE-SECRET-KEY-1…` line kept at registration or at `recovery enroll`. An
export is such a copy once extracted anywhere (`tar -x -C /srv/restore -f
ada.example.com-1234.tar`). Run `repository rewrap` on the copy, in its
restore location, with the new host's key in the environment. It reads the last `recoverykey` record out of the
changelog files, opens its `sealedKey` with the recovery key, checks that the
data-encryption key it recovered opens every file under `sealed/`, wraps that
key under `SUBSTRATE_CREDENTIAL_KEY` and rewrites `repository.json`. Then move
the directory under the data root and boot, which imports it as above.

```
SUBSTRATE_CREDENTIAL_KEY=… substratectl repository rewrap /srv/restore/repositories/ada.example.com --identity-file ./recovery.key
mv /srv/restore/repositories/ada.example.com "$SUBSTRATE_DATA_ROOT"/repositories/
SUBSTRATE_DATA_ROOT=… SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… substrate   # imports at boot
```

**The destination database must hold no row for the repository.** The boot
imports a directory that has no `repositories` row and creates the row from
the manifest; a directory that has a row is reconciled from the row, and the
boot writes the row's wrap back over `repository.json`. So the rewrap is for
a fresh database, or one this repository was never imported into; rewrapping
in place under a live root changes nothing the next boot keeps.

The command takes no database and no server, and prints neither the recovery
key nor the data-encryption key. Without `--identity-file` it reads the
recovery key from stdin: `--identity-stdin` for a script, a prompt that does
not echo otherwise; the key is never an argument. The file may be the one
`age-keygen -o` writes, comment lines included. It refuses a directory with
no `repository.json`, one whose changelog holds no `recoverykey` record (the
repository never enrolled one, so only the key it was written under opens it),
one with no files under `sealed/` (a registered repository seals at least its
login credential, so the copy is incomplete), and one whose `sealed/` files
the recovered key does not open. Every refusal comes before anything is
written, so a refused rewrap leaves the directory as it was. The manifest is
written under the changelog writer lock, which a server holds once it has
opened the repository; a server that has not opened it yet holds nothing, so
stop the server rather than rely on the refusal. The rewrap revokes nothing: a
copy taken before it still opens under the old host key.

**What comes back, and what does not.** Trigger state is in the directory:
every cursor advance, schedule fire, parked failure and paged-drain page is a
`delivery` changelog entry
([decision 0064](decisions/0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md)),
folded back into the trigger tables on import with the rest of the changelog,
and so is every webhook request the door answered `202` to whose fire had not
settled: the server's first trigger dispatcher pass over the restored
repository runs each one under its original fire id, with the body read back
from the blob store
([decision 0068](decisions/0068-an-accepted-webhook-is-a-pending-entry-in-the-delivery-ledger.md)).
A record trigger comes back at the last delivery it acknowledged, or at its
last edit if that is later, and the next pass re-reads the rows after it,
which matched nothing under the source that scanned them, so nothing is
delivered twice and nothing an older source skipped is delivered late; a schedule trigger comes back at the occurrence it last fired
and fires the ones it missed, oldest first, at most ten per pass; a parked
failure keeps the id `…/parked` listed, so a saved retry still names it, and
a parked drain resumes from its last committed page. On an import of a newer
directory over an older database dump the entries fold over the dump's rows,
so the triggers land where the directory says, not where the dump did.

Runtime state is not in the directory: embedding vectors (queued again,
below), OAuth flows in flight, a record trigger's scan position past rows
that matched nothing, and the `Idempotency-Key` rows, so a retry carrying a
key from before the restore runs its operation again. A consent flow in flight is started again: it is a nonce
and a PKCE verifier with an expiry, and the callback fails once, so the user
starts the flow over. A user's tokens are records, so they come back.
Change cursors that clients saved (the console's tail, `substratectl watch
--from`, an integration's bookmark) are refused once after an import: the row
comes back with a new history generation, and a resume under the old one
answers `410 compacted` naming the head to re-list from ([the
changelog](changelog.md#frames-and-the-horizon)). A dump keeps the row's
generation, which is what `repository rotate-generation` above is for. A
restart and a rebuild keep it too, and neither costs a client its cursor. The
alternatives beside a property (`propertyMeta.alternatives`) are not in the
directory either, and need not be: the import derives them again from the
records it folded, values, actors and stamps alike
([reading provenance](projection.md#reading-provenance-propertymeta)).

**The import queues the embeddable properties and the drain buys the vectors
again.** In the transaction that completes it, the import compares the vectors
the database already holds with the records it folded: a vector for a record
or a value that is gone is deleted, a vector
bought by the current provider and model for unchanged text is kept,
and every property without a current vector is queued (the boot logs `import
queued the repository's embeddable properties` with the count). Into an empty
database that is every property; a newer directory restored over an older
database dump queues only what changed, so it does not re-buy the repository.
The drain loop then buys the vectors a batch at a time once the repository's
`llmprovider` row resolves; with no such row the queue rows wait for one.
Until the first vectors land, a `semantic` search refuses with the
`unavailable` code and the number of properties still queued; from then on
every `semantic` and `hybrid` answer carries `pending`, the number still
queued, so a client can tell a ranking over a partial index from a full one.
The new vectors come from new provider calls, so a ranking may differ from
before the copy. `reembed` is not part of a restore; it is for a row
re-pointed at another model.

**Encrypt the copy.** The changelog and the blobs are plaintext in the
directory, on the backup host and in the dump alike. The substrate does not
encrypt the storage under it; do that yourself.

## Operator recovery

Operator commands (the "operator hat" of
[substratectl](substratectl.md#two-hats)) speak to Postgres and the data root
directly and hold no token. They need `--dsn` (or `DATABASE_URL`) and
`SUBSTRATE_DATA_ROOT`, and refuse before touching anything without them.

**Four of them run beside a live server; five need it stopped; one takes no
database.** `repository list`, `repository inspect`, `repository verify` and
`repository reembed` open the engine read-only, so they run no boot check and
append nothing: `verify` reports an unfinished final transaction or a table
ahead of its file as a finding instead of repairing it, and `reembed` writes
queue rows, which are not changelog entries. `repository rebuild`,
`repository rotate-generation`, `repository snapshot` and `user reset` open the
repository as its changelog writer, and a running server holds that lock: the
command refuses, naming the lock, until the server is stopped.
`repository rewrap` acts on a copied directory
before any boot has imported it, so it needs `SUBSTRATE_CREDENTIAL_KEY` and
the directory, and no DSN.

```
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository list
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository inspect ada
SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository verify ada
SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository snapshot ada /srv/substrate-backup/2026-09-08
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository rebuild ada
DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository rotate-generation ada
SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl user reset ada
SUBSTRATE_CREDENTIAL_KEY=… substratectl repository rewrap ./repositories/ada.example.com --identity-file ./recovery.key
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

- **`repository list`** reads the one control-plane table: one row per repository.
- **`repository inspect <repository>`** reports the authority (the repository's
  id, and the name of its directory), when it was created, the changelog head in the table and the head in the
  segment files with the segment count, live and tombstoned record counts,
  and the declaration versions per package. Two heads that differ are the gap
  the next boot closes. It is the first thing to run when something looks
  wrong.
- **`repository verify <repository>`** walks the segment files: every line's
  `sum`, every finished segment's sidecar, the seq order, and both heads
  against each other. It then holds the side stores to the fold: every blob
  whose manifest says `stored` is read out of the configured blob store and
  hashed against its digest, every secret reference a live record holds
  (by the repository's own declarations, as a replay loads them, so the
  records of a package the loader parked are not walked) must have its
  sealed file, and with
  `SUBSTRATE_CREDENTIAL_KEY` in the environment every sealed file is opened
  under the repository's key; without the key the files are compared with
  the rows and the report says nothing was opened. A directory that is a
  snapshot, or was restored from one, carries `snapshot.json`: the recorded
  point is printed and the entry it names must be in the files with the
  recorded checksum. It reports the head `(seq, checksum)` or every finding
  by seq, digest, ref or file name, never repairs the repository it judges
  (opening the engine still applies pending schema migrations, as every
  operator command does), and exits nonzero on any finding. It is safe beside
  a running server; a finding about the heads taken mid-write can be a
  transaction in flight, and one about a blob can be an upload the sweep
  just collected, so run it twice before believing one. Run it on every
  restored copy and before and after a Postgres major upgrade. It proves the
  files are undamaged, agree with the table and hold what the fold needs; it
  does not prove who wrote them
  ([the checksum](changelog.md#the-checksum-and-the-segment-files)).
- **`repository snapshot <repository> <destination root>`** writes a verified
  copy of the repository directory at
  `<destination root>/repositories/<authority>/` with `snapshot.json`
  recording the head seq and checksum the copy holds
  ([backups](#backups)). It needs `SUBSTRATE_CREDENTIAL_KEY`, runs the whole
  `verify` first and refuses on any finding, refuses a destination that
  already holds the repository, and refuses beside a running server, because
  it opens the repository as its changelog writer so nothing lands while it
  copies (and a server that opens the repository first while it runs meets
  the same lock). The copy is built beside the destination and renamed into
  place last, so a failed snapshot leaves nothing there. Run it with the
  server's binary, as with `rebuild`. Under `s3` it lists the objects the
  copy needs instead of copying them.
- **`repository rebuild <repository>`** replays the segment files into a fresh
  fold, in one transaction, under that repository's own lock, after running
  the same check the boot runs. It reproduces the fold bit for bit and appends
  nothing, so it is safe to run on a healthy repository, and it is the proof
  that the directory alone reproduces the records. The search index (`fts`)
  is reproduced too, with one limit: the replay indexes every row under the
  kind declarations in force at the end, and a kind edit that changes what
  its records index re-indexes the kind's rows in the same apply, so the two
  agree from that apply on. Rows indexed before this re-indexing existed,
  under a declaration that has since changed, keep the old bands until a
  rebuild or the next such edit of their kind; a search over them can return
  a hit the rebuilt repository does not, or miss one it does. It does not
  touch blobs or sealed files, which were never in the changelog. It replays the delivery ledger with the rest of the
  fold: each trigger's cursor lands at the last delivery it acknowledged, its
  parked failures and a paged drain's resume row come back, and the next pass
  re-reads the rows after the cursor, which deliver nothing. OAuth flows in
  flight are left alone. Stop the server
  first: it opens the repository as its changelog writer and refuses while
  the server holds the lock.
- **`user reset <repository>`** is the answer to a user who has lost both
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
