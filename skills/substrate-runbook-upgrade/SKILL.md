---
name: substrate-runbook-upgrade
description: >-
  Upgrade a substrate server, and everything built on it, from the release it
  runs to a newer one: the deployment, the repositories it holds, the kinds a
  user declared, imported samples, installed providers, functions, agents,
  API clients and scripts. Use when asked to upgrade, update or move to a new
  substrate release, when a substrate release is out and code depends on the
  server, or when a server reports a version the client code was not written
  against. Reads the changelog section of every release in the range, writes a
  plan, takes a verified backup, deploys, verifies, and takes the upgrades the
  catalog offers.
---

# Upgrade substrate

This runbook moves a substrate deployment and what runs on it from one
release to a newer one. It is written for the person or agent that builds on
substrate, not for substrate's own developers. Follow the steps in order,
show the user what each one found, and change nothing until the user
approves the plan in step 3.

Substrate is one Go binary and one Postgres database. A **repository** is
everything one user has; its name is its **authority**, the DNS-style name
the user signs in with (`ada.example.com`). The kinds the user declares and
the samples they add live in the repository's local namespace, `~`
(`~/tasks/task`), whatever the repository is called. Every piece of
upgrade guidance below comes from the changelog and the docs at
<https://github.com/geoah/substrate>. When this runbook and a note disagree,
the note for the release wins.

## What an upgrade changes

| Layer | What changes it | Reversible |
| --- | --- | --- |
| Postgres tables | the new binary's schema migrations, at its first boot | no: an older binary refuses a database holding a migration it does not carry |
| Each repository's data, the user's own declarations included | repository migrations, at the repository's first open under the new binary, as ordinary changelog writes | no: the repository's ledger and dialect stamps refuse an older binary |
| Where each repository's history is kept | the first boot of a release with the archive, from a release before it: the history moves from the data root into the database, and the server starts copying each repository to its archive (step 6) | no: an older binary refuses the migration that boot applies, and reads no archive |
| The seeded packages (`substrate.reamde.dev/core`, `…/llm`, `…/console`, `…/observations`) | the boot upgrade, at each repository's first open, unless a guard refuses it | no |
| Installed providers and catalogues (`providers.substrate.reamde.dev/<name>`, `substrate.reamde.dev/health`, `…/labs`, `…/environment`) | the user: the catalog offers the upgrade, and `substratectl add` takes it | no |
| Added samples (copied to `~/<package>`) | the user: the catalog offers the upgrade, and `substratectl add` takes it | no |
| Kinds the user declared | the user; a binary that tightens a contract quarantines the package instead of refusing the repository | yes, by fixing and re-applying the package |
| API clients, scripts, agents, `substratectl` | the user, following each note's `What to do` | yes |

The first four rows happen by themselves once the new binary boots and opens
each repository. None of them can be undone in place, and the only way back
is the backup taken in step 5.

## Run operator commands with the server's own binary

The writing operator commands (`substratectl repository rebuild`,
`reindex`, `rotate-generation`, and `repository snapshot` on a release
before the archive) open the engine the way the server does, and that open
applies the schema migrations the CLI carries. A `substratectl` newer than
the server therefore migrates the database and closes the rollback before
the backup exists. `repository verify`, `repository reembed` and
`substratectl user reset` open read-only and apply nothing: a newer
one refuses a database it would migrate, naming the migrations it carries.
An older one of any of them refuses a database the new server migrated.
`repository list` and `repository inspect` read the tables directly and are
safe with any version.

Run every other operator command with the `substratectl` of the release the
server runs at that moment. The server image carries it. On the compose
deployment, run it inside the container. The entrypoint gives only the
server process the minted key, so read it from `/keys/credential.key` when
the environment has none:

```bash
docker compose exec substrate sh -c \
  'SUBSTRATE_CREDENTIAL_KEY="${SUBSTRATE_CREDENTIAL_KEY:-$(cat /keys/credential.key)}" exec substratectl repository verify <repository>'
```

Outside compose, use the `substratectl` from the same release archive as the
server binary. The user's commands (everything that speaks HTTP with a
token) migrate nothing.

## Keep secrets out of the conversation

One secret matters in an upgrade, and the operator owns it:

- **`SUBSTRATE_CREDENTIAL_KEY`** unwraps the key every repository on the host
  is sealed under, and nothing else does. A backup restored without it is
  refused, or, by the operator's choice, comes back without its secrets:
  every connected account to reconnect, every API key to enter again, and
  the owner's password to reset. The operator confirms it is stored in
  their own secret storage, apart from the data backups.

A user who registered on an older release was handed a second key at
registration, an `AGE-SECRET-KEY-1…` line. The release that removes it
changes this for them: the key opens nothing on the upgraded server, no
command reads it, and the upgrade drops the copy of the repository's data
key it opened from the repository's history. A backup taken before that
upgrade still carries the copy, so the old key stays as secret as the
backups it opens. Tell the user so, and plan no restore around it.

Ask the operator to confirm; never ask to see the key. Never put a secret, a
token or a password into a message, the plan, a log, or a file in a code
repository.

## Step 1: Record the version, the target and the inventory

**Work out which role you are in.** An *operator* runs the server: they can
change the image, stop the process, and reach `DATABASE_URL`,
`SUBSTRATE_DATA_ROOT`, the archive `SUBSTRATE_ARCHIVE` names on a release
that has one, and `SUBSTRATE_CREDENTIAL_KEY`. A *user* holds only a
token for a server somebody else runs. A user asks the operator for the
server's part (the server backup in step 5, the deploy in step 6,
`repository verify` in step 7) and does the rest.

**Read the running version.** Discovery is unauthenticated:

```bash
curl -fsS "$SUBSTRATE_SERVER/.well-known/substrate/server.json" | jq '.server'
substratectl version    # the CLI's own version; step 6 switches it to the target's
```

- A release reports its tag (`v0.85.0`).
- The image's `latest` tag is the tip of `main` and reports a `git describe`
  version (`v0.85.0-4-g1a2b3c4`). That build is past `v0.85.0` and runs part
  of the next release, so start its range at `v0.85.0`.
- A build nobody stamped reports `dev`. `compose.yaml` builds from the tree
  (`build: .`), so a compose deployment usually does. Run `git describe
  --tags` in the checkout it was built from, or ask the user.

**Pick the target.** Use the newest release unless the user names one:

```bash
gh release view --repo geoah/substrate --json tagName -q .tagName
```

Deploy a release tag, never `latest`.

**List what the user has built.** Use the user's context (`SUBSTRATE_SERVER`
and `SUBSTRATE_TOKEN`, or a `substratectl login`):

```bash
substratectl catalog                                   # shipped packages: held here, versions, offered upgrades, blockers
substratectl get substrate.reamde.dev/core/package -o yaml   # every package: source, origin stamp, quarantine state
substratectl kinds                                     # every installed kind
substratectl bundle list                               # bundles and their lifecycle state
substratectl trigger status                            # delivery wiring, lag, parked deliveries
substratectl sync status                               # connected provider accounts
```

Then search the user's own code for what an upgrade note can break: routes
under `/api/v1/`, `substratectl` invocations, kind references
(`<authority>/<package>/<name>`), agent tool names, and webhook URLs
(`/webhooks/<repository>/<trigger-id>`, the repository by its id or its name).

An operator also records how the server runs (compose, the image tag, or a
binary), every process that opens the same database, the repositories it
holds, and whether the current release has the archive: `repository list`
prints an `ARCHIVED` column on a release that does, and none on a release
before it.

```bash
docker compose exec substrate substratectl repository list    # or DATABASE_URL=… substratectl repository list
```

Two kinds of upgrade follow from that, and step 5 and "When it goes wrong"
differ between them: **(a)** from a release before the archive, which still
has `repository snapshot`, and **(b)** from a release that already has the
archive.

**Show the user:** the current version, the target version, the role, the
kind of upgrade, (a) or (b), and the inventory.

## Step 2: Read every release's changelog section in the range

Read the section of every release after the current one, up to and including
the target. Read all of them, oldest first: a range often spans several
releases, and any one of them can hold a break.

**Where the sections are.** `CHANGELOG.md` on `main`, one `## [X.Y.Z]`
heading per release, newest first. Read it from `main`, not from the target
tag: the file was folded from older notes on 2026-09-29, and tags before
that carry no changelog or a stub.

```bash
# the whole file
curl -fsSL https://raw.githubusercontent.com/geoah/substrate/main/CHANGELOG.md

# one release's section, as the release page carries it
gh release view v0.105.0 --repo geoah/substrate --json body -q .body
```

Without `gh`, the same bodies are at
`https://api.github.com/repos/geoah/substrate/releases?per_page=100&page=N`,
in the `body` field of each entry.

**How to read a section.** It opens with `⚠ BREAKING CHANGES`, one entry
per break with its steps as sub-bullets, written to be followed literally,
then `Added` and `Fixed`. Releases before v0.70.0 are not listed: a database
from before it cannot be upgraded in place (below). Each break entry is a
note below. For each one, decide:

1. **Does it apply?** Check it against the step 1 inventory. A note about a
   provider the user never installed does not apply. Record why you skipped
   it.
2. **When does it run?** Before the deploy (records to rewrite, headers to
   declare, other servers to stop), at the deploy (env vars, image,
   database), or after it (upgrades to take, client code to change).
3. **Who does it?** The operator, the user, or the server by itself. Some
   notes say there is nothing to do because a repository migration rewrites
   the rows at first boot.
4. **Is it one-way?** Mark every note that says a rollback is not possible
   or that every server must be upgraded first.

**Read "Upgrading the binary" in `docs/operations.md` at the target tag**
for the mechanisms the notes assume:
`https://github.com/geoah/substrate/blob/<target tag>/docs/operations.md#upgrading-the-binary`.
It says what the boot does, which boots refuse, and why a rollback closes.

**Two floors exist.** A database migrated before `v0.70.0` cannot be
upgraded in place: the `v0.70.0` note moves the data root onto an empty
database instead. A data root written by `v0.68.0` or earlier has no upgrade
path at all. If the current version is below either floor, stop and show the
user that release's note.

## Step 3: Write the plan and get approval

Write one plan and show it to the user before you change anything. Order it
by when each step runs:

```markdown
Upgrade v0.101.0 -> v0.105.0 (operator and user)

One-way: v0.105.0 (a kind declares `purpose`; no rollback past it)

Before the deploy, with the old server running
1. [user] Export; download the v0.105.0 `substratectl` beside the current one (step 4)
2. [user] <each pre-deploy `What to do` step, with its note>
3. [user] Export again (step 5)
4. [operator] Stop the server; back up every repository (a snapshot, or a copy of the archive) and dump the database (step 5)

Deploy
5. [operator] Image `ghcr.io/geoah/substrate:0.105.0`; env changes: none
6. [user] Switch to the v0.105.0 `substratectl` (step 6)

After the deploy
7. [operator] `repository verify` every repository (step 7)
8. [user] Take the published, then the sample upgrades `substratectl catalog` offers
9. [user] Client changes: <each change, with the file it touches>

Skipped notes
- v0.103.0 "<heading>": the user has no Slack account
```

Wait for the user's approval. A note whose `What to do` does not match what
you found in step 1 is a question for the user; do not guess at it.

## Step 4: Do the pre-deploy steps

The old server is still running, and the current `substratectl` is the one
to use.

1. **Export first**, so the values the pre-deploy writes replace have a copy
   outside the changelog: `substratectl export`.
2. **Download the target's `substratectl`** into its own directory, and keep
   using the current one until step 6. Each release attaches
   `substrate_<version>_<os>_<arch>.tar.gz` and a `checksums.txt`:

   ```bash
   mkdir -p ~/substratectl-v0.105.0 && cd ~/substratectl-v0.105.0
   gh release download v0.105.0 --repo geoah/substrate \
     --pattern 'substrate_0.105.0_linux_amd64.tar.gz' --pattern checksums.txt
   sha256sum --check --ignore-missing checksums.txt && tar -xzf substrate_0.105.0_linux_amd64.tar.gz
   ```

3. **Carry out every plan step marked for before the deploy.** Typical ones:
   - Rewrite the records a note says would block an upgrade. The boot
     upgrade never runs a lossy step, so a shipped change that would remove
     values from live records is refused until those records are rewritten.
   - Add what a note says a record must now declare, for example the
     headers a webhook trigger's callable reads (`source.webhook.headers`).

## Step 5: Take a verified backup

The first boot of the new binary closes the rollback, so the backup comes
last before the deploy, after every pre-deploy write, and it is verified
before anything else happens.

**User.** Download the recovery export again while the server still runs:

```bash
substratectl export            # writes <authority>-<head>.tar, refuses to keep a truncated archive
```

**Operator.** First, with the server still running, run `repository verify`
for every repository with the current `substratectl`: it reads every byte
and takes no lease. Then stop every server process that opens the database,
and leave them stopped until step 6. Back up every repository the way the
current release can.

**(a) From a release before the archive: a snapshot.** Take a snapshot of
every repository with the old server's `substratectl`; releases with the
archive have no `snapshot` command. `repository snapshot` verifies the
repository before it copies anything, reads the copy back, and records the
point it holds. It refuses a destination that already holds the repository,
so use a fresh directory per upgrade.

Pass the previous upgrade's snapshot root as `--base`. The snapshot then
hard-links every finished changelog segment and blob that snapshot holds
unchanged, and verifies and copies only what was written since: seconds to
a few minutes for a history of tens of gigabytes, where a snapshot without
a base reads and copies every byte, which takes hours. The base must be on
the same filesystem as the new snapshot, and it must be a snapshot this
command wrote. Keep the newest snapshot: it is the next upgrade's base while
the target is also a release before the archive. The first snapshot has no
base and is the slow one; if the operator has never taken one, say so in the
plan and give it its own window.

```bash
SUBSTRATE_CREDENTIAL_KEY=… DATABASE_URL=… SUBSTRATE_DATA_ROOT=… \
  substratectl repository snapshot <repository> /srv/substrate-backup/<date> \
    --base /srv/substrate-backup/<previous date>    # once per repository; omit --base the first time
```

The output names the base and how many segments and blobs it linked. A
snapshot that linked nothing from a base it was given read everything: check
that the base is the previous snapshot of the same repository. With a base,
the snapshot compares the database's changelog table with the files only
past what the base holds, which is why the `repository verify` before the
stop matters.

**(b) From a release with the archive: a copy of the archive.** A server
stopped cleanly runs one last archive pass over each repository it has open,
bounded at 30 seconds, so its archive is at the head it stopped at. Check
that before you copy: `repository inspect` reads the tables and runs while
the server is stopped, and its `archive:` line must say `0 behind the head`
and `0 blob(s) missing`. `repository list` shows the same mark in its
`ARCHIVED` column. A close whose last pass did not finish logs `the archive
pass at the close did not finish`.

```bash
DATABASE_URL=… substratectl repository inspect <repository>    # once per repository
```

An archive behind its head lost that last pass to a store that did not
answer in time. Start the server, fix the store if the log says `an archive
pass failed`, wait until `inspect` says `0 behind the head`, and stop it
again. A blob missing from it is one whose bytes the server itself lost: no
pass can copy it, `repository verify` names it, and the backup holds
everything else. Then copy the archive whole into a fresh directory per upgrade: the
directory `SUBSTRATE_ARCHIVE` names (`<SUBSTRATE_DATA_ROOT>/archive` when it
is unset), or the bucket prefix it names, with the store's own tools.

```bash
cp -a "$SUBSTRATE_DATA_ROOT"/archive /srv/substrate-backup/<date>/archive    # SUBSTRATE_ARCHIVE unset
```

A database dump is optional in both cases. A snapshot restores alone, and so
does the archive: the boot restores each repository into an empty database
(see "When it goes wrong"). A dump makes the restore faster and costs
downtime now, since it reads the whole database; it restores only beside the
data root it was taken with, where the blob bytes are. Take one when the
window allows it, as a private file that exists only once the dump
succeeded:

```bash
( umask 077
  pg_dump "$DATABASE_URL" > /srv/substrate-backup/<date>.sql.tmp &&
    mv /srv/substrate-backup/<date>.sql.tmp /srv/substrate-backup/<date>.sql )
```

Where the data root, the archive and the database sit on a filesystem with
snapshots (ZFS, LVM, a cloud volume), a snapshot of those volumes taken
while the server is stopped is an equally good backup and takes seconds. Use
it in place of the dump, or of both, when the operator prefers it; say which
in the plan.

On the compose deployment the server runs as uid 65532 and Postgres
publishes no port, so the backup runs in a one-off container of the same
image with a directory that uid owns mounted in, and the dump runs in the
`postgres` container. The data root is `/var/lib/substrate`, and the archive
is under it unless the environment sets `SUBSTRATE_ARCHIVE`:

```bash
docker compose stop substrate
sudo install -d -m 700 -o 65532 -g 65532 ./substrate-backups    # once; every backup lands under it, so each snapshot links the last
# (a) a release before the archive
docker compose run --rm -v "$PWD/substrate-backups:/backup" --entrypoint /bin/sh substrate -c \
  'SUBSTRATE_CREDENTIAL_KEY="${SUBSTRATE_CREDENTIAL_KEY:-$(cat /keys/credential.key)}" exec substratectl repository snapshot <repository> /backup/<date> --base /backup/<previous date>'
# (b) a release with the archive
docker compose run --rm --entrypoint substratectl substrate repository inspect <repository>
docker compose run --rm -v "$PWD/substrate-backups:/backup" --entrypoint /bin/sh substrate -c \
  'mkdir /backup/<date> && cp -a /var/lib/substrate/archive /backup/<date>/archive'
( umask 077    # the optional dump
  docker compose exec -T postgres sh -c 'pg_dump -U postgres "$POSTGRES_DB"' > ./substrate-backup-<date>.sql.tmp &&
    mv ./substrate-backup-<date>.sql.tmp ./substrate-backup-<date>.sql )
```

Never run `docker compose down -v`: it deletes the database, the data root
and the key volumes.

A `repository verify` or a snapshot that reports a finding means the backup
is not one. Stop and show the user the finding; do not deploy. One finding
has a known cause and a fix: `an orphan: no live or tombstoned record holds
the ref` on a `sealed` ref, from a release whose open does not erase them
yet, is material an older purge left that nothing reads. Start the server,
let its GC sweep erase them (about five minutes after boot), stop it again
and retake the backup. Then confirm that the operator holds
`SUBSTRATE_CREDENTIAL_KEY` apart from the backup (on compose it is
`/keys/credential.key` in the `substrate-keys` volume, unless the
environment sets one).

**Show the user:** where the backup is, what it holds, and the point each
snapshot recorded or the mark each archive holds.

## Step 6: Deploy the new binary and switch the CLI

Pin the image to the target tag (`ghcr.io/geoah/substrate:0.105.0`) and
apply every env var change the notes list. Start exactly one server process,
and only after every old one has stopped. A rolling deploy that starts the
new process beside the old one is refused, because a repository has one
writer.

On compose, replace `build: .` or the old tag with that `image:`, then:

```bash
docker compose pull substrate
docker compose up -d substrate      # recreates the container with the new image and environment
docker compose logs -f substrate
```

Not `docker compose restart`: it keeps the environment the container was
created with.

Watch the log until the server is serving: a long boot import logs `still
booting` every 10 seconds, and that is progress, not a hang. Then switch the
user's `substratectl` to the one downloaded in step 4; every later step
uses it.

**(a) The first boot after a release before the archive moves the history.**
Its boot check logs `substrate: boot check started` with
`directoriesWithHistory`, the repositories whose history is still under the
data root. For each one it holds `<data root>/repositories/<id>/` to the
database's changelog table, takes in any line the directory holds past the
table's head, and removes the directory's `repository.json`, `changelog/` and
`sealed/`, keeping `blobs/` and `spool/`; the log says `substrate:
repository checked` with `action=history moved to the table`. The changelog
table is the history from then on. The server then copies every repository to
its archive: `<data root>/archive` unless `SUBSTRATE_ARCHIVE` names another
directory or a bucket, or is `off`. The first pass over a repository copies
its whole history, paced by `SUBSTRATE_ARCHIVE_BYTES_PER_SECOND` (8 MiB a
second by default, about an hour for 27 GB) and paused while functions and
agents run; the server serves meanwhile, and `repository list` shows
`ARCHIVED` catching up. `SUBSTRATE_DIGEST_BYTES_PER_SECOND` and
`SUBSTRATE_CHANGELOG_SEGMENT_BYTES` are no longer read: remove them from the
environment. That boot also applies migration 0022, so from here the way back
is the step 5 snapshot and the old binary ("When it goes wrong").

When the server refuses, it says what it refused. Do not work around a
refusal by editing tables or files:

| The log or the API says | Meaning | Do |
| --- | --- | --- |
| `the database applied migrations this binary does not carry` | a newer binary already migrated this database | deploy that release or a later one, or restore the backup |
| `migration(s) this database applied are not the ones this binary carries` | the database was migrated by a different build of the same migration | restore the backup; there is no repair |
| `the repository recorded migrations this binary does not carry` | a newer binary already ran repository migrations on it | deploy that release or a later one |
| `the changelog speaks a newer dialect than this binary can replay`, or `the store speaks a newer schema dialect than this binary` | the binary is older than the repository | deploy a newer binary |
| `another process is this repository's writer` | a second server process opened the same repositories | stop every other process, then start one |
| `is not the key the DEK wrap of repository` | this host's `SUBSTRATE_CREDENTIAL_KEY` is not the key the repository was sealed under | set the original key |
| the API answers `503 unavailable` with `Retry-After` for one repository | that repository's open was refused; the server log names it and why | fix what the log names, then restart |
| `REFUSED to upgrade a repository's shipped vocabulary`, with the guard lines under `refused` | the seeded packages stay at their stored version; the lines name the kind, the property and the records to rewrite | rewrite those records, then restart the server |
| `upgrading a repository's shipped vocabulary from the embedded tree`, and no `upgraded` line within a minute | the upgrade's transaction is still running; every request on that repository waits for it. It re-derives no stored record's indexes, so a minute is long unless the notes name a conversion (a rename, a backfill, a remap), which rewrites the records it names | wait for the `upgraded` line; if it does not come, show the user the line, the time since it, and whether the notes name a conversion |
| `re-deriving the indexes of the kinds a declaration change reshaped` | the server is re-deriving the refs rows and `fts` of the rows a shipped kind's new declaration reshapes, behind the open; the repository serves meanwhile, and the line names the kinds | nothing; `re-derived the indexes of the kinds a declaration change reshaped` says it finished, and a stop before that is resumed at the next open |
| `function body failed to prepare at repository open` | that function's deliveries park; the repository still serves | re-apply a working function (a provider's arrives with its upgrade in step 8), then retry the parked deliveries |
| `two histories of the repository disagree`, at the first boot after a release before the archive | a repository's directory under the data root and the database's changelog table hold different lines under one seq; the boot refuses before it removes anything | stop and show the user; the fix is the restore below, from the step 5 backup |
| `an archive pass failed; the server goes on serving, and the archive falls behind until a pass finishes` | the archive's store did not take a pass: unreachable, refused the credentials, or full. Writes go on; `substrate_archive_failed_passes` and `substrate_archive_delay_seconds` grow, and `ARCHIVED` in `repository list` falls behind | fix what the error names (the store, or the `SUBSTRATE_ARCHIVE` settings); `the archive is written again` says a pass finished |
| `the archive holds history this database does not` | the archive's head is past the table's, or its line at that head is not the table's: the database was restored from an older copy, or the archive is another history's. The pass refuses to write over it; the server serves and the delay grows | restart the server: the boot takes in an archive ahead of its table. One the boot logs as `does not continue its history` is left alone; show the user, and move it out of the store only with their yes |
| `the archive cannot be listed; the boot goes on without restoring or comparing anything from it` | the store did not answer at boot; nothing was restored from it | fix the store; the passes catch the archive up once it answers |
| `boot check: the archive of repository <id> in <store>: …` | restoring a repository from the archive failed: the credential key does not open it, it names a dialect above the binary, or a piece is damaged | set the key it was written under, or restore that repository from another copy; moving `repositories/<id>/` out of the archive boots without it |

## Step 7: Verify

**Operator.** Check the server, then verify every repository with the new
server's `substratectl`:

```bash
curl -fsS "$SUBSTRATE_SERVER/healthz"
curl -fsS "$SUBSTRATE_SERVER/.well-known/substrate/server.json" | jq -r '.server.version'   # the target tag
docker compose exec substrate sh -c \
  'SUBSTRATE_CREDENTIAL_KEY="${SUBSTRATE_CREDENTIAL_KEY:-$(cat /keys/credential.key)}" exec substratectl repository verify <repository>'
```

`repository verify` walks the changelog table (no gap in the seqs, every
transaction whole, every checksum stamped), holds every line of the archive's
pieces to the checksum the table stamped for its seq, opens every sealed
value under the key, and hashes every blob against its digest. It takes no
lease, so it is safe beside the running server. An archive behind the table
is not a finding: it is copied after the commits, and right after an upgrade
from a release before the archive its first passes are still running. Once
they finish, each repository's `archive:` line in `repository inspect` says
`0 behind the head`; check that before the next backup counts on the
archive.

**User.**

```bash
substratectl catalog                                         # seeded packages first, then published packages and samples
substratectl get substrate.reamde.dev/core/package -o yaml   # look for quarantined: true
substratectl trigger status
substratectl sync status
```

- **`lands at restart`** on a seeded package in `catalog`: the upgrade is
  admitted but runs only at a repository's first open. Ask the operator to
  restart the server.
- **`blocked`**: the guard lines under the table name the kind, the property
  and the count of live records in the old shape. Rewrite those records.
  Then a seeded package needs a server restart, and a published package or
  a sample needs its `add` again (step 8). A plan that rewrites more
  records than `SUBSTRATE_CONVERSION_CEILING` (10000 by default) is blocked
  too; the operator may raise the ceiling.
- **`quarantined: true`** on a package: the new binary does not admit its
  stored declarations, and `quarantineReason` says why. Its kinds refuse
  writes and its functions do not run until a valid closure is re-applied.
  For the user's own package, fix the declarations and apply them
  (`substratectl apply -f <package.yaml>`). For a published package or a
  sample, add it again (step 8). If the marker stays, show the user
  the `quarantineReason`.
- **Parked deliveries or a new last error** on a trigger, or a sync account
  in an error state: read the error before you retry anything. `substratectl
  trigger parked <id>` lists them, and `substratectl trigger retry <id>
  <parked-id>` retries one.

**Show the user:** each check and its result.

## Step 8: Take the upgrades the catalog offers

`substratectl catalog` lists each published package (a provider or a catalogue
such as `substrate.reamde.dev/health`, `publisher` in its KEPT BY column)
or sample with a newer shipped version as a motion (`16 -> 17`). Read the
conversion steps printed under the table before you take one. Take the
published packages first, then the samples, because a sample's suggested
mapping can wait on a provider's upgrade:

```bash
substratectl add providers.substrate.reamde.dev/github   # a published package: adding it again is the upgrade
substratectl add samples.substrate.reamde.dev/tasks      # a sample: adding it again is the upgrade
```

A sample can require another at a minimum version (`tasks` requires `people`
4 and `scheduling` 2). An add refused with a `requiresAtLeast` line names
the sample and the version it needs: add that one first.

Four cases are refused:

- **A blocked upgrade**: a dropped kind, a narrowed property or a removed
  callable that live records or triggers still use, or a plan above the
  conversion ceiling. Clear the blocker (step 7); no flag does.
- **A lossy plan**, on either package: a step removes values from live
  records.
- **An `edited copy`**, on a sample: one the user changed since adding it.
  Adding it again replaces the copy whole, so the edits are lost.
- **A `requiresAtLeast` floor**, on a sample: see above.

A lossy plan and an edited copy clear only with `--allow-data-loss`. Show the
user the steps or the edits that would be lost, and pass the flag only after
they say yes. The old values stay in the changelog either way.

Then carry out the notes aimed at the user's own packages (a new key to
declare, a deprecated one to replace) with `substratectl apply -f
<package.yaml>`. A change that removes values from live records is refused
the same way, and `substratectl apply -f <package.yaml> --allow-data-loss`
takes the same explicit yes.

## Step 9: Update the client code

- **Change the client code.** Carry out every `What to do` step aimed at
  clients: routes, request and response fields, kind references, agent tool
  names.
- **Run the user's tests** against the upgraded server, and show the
  results.

## When it goes wrong

- **Roll forward by planning again.** A release that migrated the schema,
  stamped a newer dialect, ran a repository migration or shipped a new
  declaration key leaves the database or its repositories unreadable to an
  older binary, so assume the target did at least one of these. A newer
  release than the target is a new range: go back to step 2 for the releases
  it adds and follow their notes before deploying it.
- **Going back is the user's call.** Show them the refusal and the backup,
  and restore only after they say yes. Either restore boots the old binary on
  a fresh, empty database and lets the boot bring each repository back from
  the step 5 backup:
  1. Stop the server.
  2. Move the upgraded data root aside, and keep it until the restore is
     verified: `mv "$SUBSTRATE_DATA_ROOT" "$SUBSTRATE_DATA_ROOT.upgraded-<date>"`.
     Where `SUBSTRATE_ARCHIVE` names a directory or a bucket outside the data
     root, move the upgraded archive aside the same way (another directory,
     or another prefix): it holds what the new binary wrote.
  3. Put the backup back, owned by the user the server runs as (uid 65532 in
     the image). **(a)** The snapshots go under a fresh data root: the old
     release reads no archive, and refuses the database the new binary
     migrated (migration 0022), which is why the database is a fresh one.
     **(b)** The archive copy goes where the old release's
     `SUBSTRATE_ARCHIVE` points (`<data root>/archive` when it is unset, or
     the bucket prefix):

     ```bash
     install -d -m 700 -o 65532 -g 65532 "$SUBSTRATE_DATA_ROOT"
     cp -a /srv/substrate-backup/<date>/repositories "$SUBSTRATE_DATA_ROOT"/   # (a) the snapshots
     cp -a /srv/substrate-backup/<date>/archive "$SUBSTRATE_DATA_ROOT"/        # (b) the archive, SUBSTRATE_ARCHIVE unset
     chown -R 65532:65532 "$SUBSTRATE_DATA_ROOT"
     ```

  4. Create a fresh, empty database and point `DATABASE_URL` at it. Keep
     the upgraded one until the restore is verified.
  5. Start the old release's server with the same `SUBSTRATE_CREDENTIAL_KEY`.
     **(a)** It imports every repository directory under the data root.
     **(b)** It restores every repository the archive holds and the
     database does not, and copies its blobs and spool back under the data
     root.
  6. Run `repository verify` for every repository with the old release's
     `substratectl`. A snapshot prints the point it recorded; an archive's
     repository comes back at the head its manifest names.
  7. Only then, and with the user's yes, delete the upgraded data root,
     archive and database.

  Clients re-list once, because a restore starts a new history generation.
  On compose the data root and the database are the `substrate-data` and
  `substrate-db` volumes, so a restore needs a second volume and a second
  database: write the `compose.yaml` edits into the plan and show them to
  the user first. "Backups" in `docs/operations.md`, at the tag you restore
  to, has the full procedure, the dump path included.
- **A user's export** restores through the operator, the same way, into a
  stopped server whose database holds no row for that repository. An export
  from a release with the archive is the archive as one tar: extract it into
  the archive the server reads (`tar -x -C "$SUBSTRATE_DATA_ROOT"/archive -f
  <authority>-<head>.tar` when `SUBSTRATE_ARCHIVE` is unset). An export from a
  release before the archive, like a snapshot, is a directory: extract it
  under the data root, where it lands as `repositories/<id>/`, and the boot
  imports it. A database that still holds the row keeps its own history: an
  older export or archive changes nothing, and only lines past the table's
  head are taken in.

## What not to do

- Never deploy without reading every note in the range.
- Never deploy the `latest` tag, or roll an image back over a database the
  new binary has booted on.
- Never deploy on a backup that did not verify.
- Never run an operator command with a `substratectl` from another release
  than the server's.
- Never edit `schema_migrations`, `repository_migrations`, the `records`
  table, the files under the data root or the archive by hand to get past a
  refusal.
- Never pass `--allow-data-loss` without the user's explicit yes to the
  steps it confirms.
- Never put `SUBSTRATE_CREDENTIAL_KEY`, a token or a password into a
  message, the plan, a log or a file in a code repository.
