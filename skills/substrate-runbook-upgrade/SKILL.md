---
name: substrate-runbook-upgrade
description: >-
  Upgrade a substrate server, and everything built on it, from the release it
  runs to a newer one: the deployment, the repositories it holds, the kinds a
  user declared, imported samples, installed providers, functions, agents,
  API clients and scripts. Use when asked to upgrade, update or move to a new
  substrate release, when a substrate release is out and code depends on the
  server, or when a server reports a version the client code was not written
  against. Reads the upgrade notes of every release in the range, writes a
  plan, takes a backup, deploys, verifies, and takes the upgrades the catalog
  offers.
---

# Upgrade substrate

This runbook moves a substrate deployment and what runs on it from one
release to a newer one. It is written for the person or agent that builds on
substrate, not for substrate's own developers. Follow the steps in order,
show the user what each one found, and change nothing until the user
approves the plan in step 3.

Substrate is one Go binary and one Postgres database. A **repository** is
everything one user has; its name is its **authority**, the DNS-style name
that kinds are declared under (`ada.example.com/tasks/task`). Every piece of
upgrade guidance below comes from the upgrade notes and the docs at
<https://github.com/geoah/substrate>. When this runbook and a note disagree,
the note for the release wins.

## What an upgrade changes

| Layer | What changes it | Reversible |
| --- | --- | --- |
| Postgres tables | the new binary's schema migrations, at its first boot | no: an older binary refuses a database holding a migration it does not carry |
| Each repository's data, the user's own declarations included | repository migrations, at the repository's first open under the new binary, as ordinary changelog writes | no: the repository's ledger and dialect stamps refuse an older binary |
| The seeded packages (`substrate.reamde.dev/core`, `substrate.reamde.dev/llm`) | the boot upgrade, at each repository's first open, unless a guard refuses it | no |
| Installed providers (`providers.substrate.reamde.dev/<name>`) | the user: the catalog offers the upgrade, and `substratectl install` takes it | no |
| Imported samples (rehomed to `<authority>/<package>`) | the user: the catalog offers the upgrade, and `substratectl import` takes it | no |
| Kinds the user declared | the user; a binary that tightens a contract quarantines the package instead of refusing the repository | yes, by fixing and re-applying the package |
| API clients, scripts, agents, `substratectl` | the user, following each note's `What to do` | yes |

The first three rows happen by themselves once the new binary boots and opens
each repository. None of them can be undone in place, and the only way back
is the backup taken in step 5.

## Run operator commands with the server's own binary

The operator commands (`substratectl repository …`, `substratectl user
reset`) open the engine, and opening the engine applies the schema
migrations the CLI carries. A `substratectl` newer than the server therefore
migrates the database under the old server and closes the rollback before
the backup exists. An older one refuses a database the new server migrated.
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

## Step 1: Find where you are

**Work out which role you are in.** An *operator* runs the server: they can
change the image, stop the process, and reach `DATABASE_URL`,
`SUBSTRATE_DATA_ROOT` and `SUBSTRATE_CREDENTIAL_KEY`. A *user* holds only a
token for a server somebody else runs. A user asks the operator for the
server's part (the server backup in step 5, the deploy in step 6,
`repository verify` in step 7) and does the rest.

**Read the running version.** Discovery is unauthenticated:

```bash
curl -fsS "$SUBSTRATE_SERVER/.well-known/substrate/server.json" | jq '.server'
substratectl version    # the CLI's own version; step 9 installs the matching one
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
(`/webhooks/<authority>/<trigger-id>`).

An operator also records how the server runs (compose, the image tag, or a
binary), every process that opens the same database, and the repositories it
holds:

```bash
docker compose exec substrate substratectl repository list    # or DATABASE_URL=… SUBSTRATE_DATA_ROOT=… substratectl repository list
```

**Show the user:** the current version, the target version, the role, and
the inventory.

## Step 2: Read every upgrade note in the range

Read the notes of every release after the current one, up to and including
the target. Read all of them, oldest first: releases ship several times a
day, so a range often spans dozens, and any one of them can hold a break.

**Where the notes are.** Each release page carries its notes. Today they sit
between two markers above the commit list; where a page has no markers, the
body is the notes.

```bash
# every release tag, newest first
gh release list --repo geoah/substrate --limit 500 --json tagName -q '.[].tagName'

# one release's notes
gh release view v0.105.0 --repo geoah/substrate --json body -q .body |
  sed -n '/<!-- upgrade-notes:start -->/,/<!-- upgrade-notes:end -->/p'
```

Without `gh`, the same bodies are at
`https://api.github.com/repos/geoah/substrate/releases?per_page=100&page=N`,
in the `body` field of each entry. A release whose page lists only commits
has no notes; read its commit list for subjects with `!` before the colon,
because each of those is a break.

**How to read a note.** Each note is a break, a deprecation, a feature or a
fix. A break or a deprecation ends in a `What to do` section whose steps are
written to be followed literally. For each note, decide:

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

**Read the operations docs at the target tag** for the mechanisms the notes
assume:
<https://github.com/geoah/substrate/blob/main/docs/operations.md#upgrading-the-binary>
(swap `main` for the target tag). It says what the boot does, which boots
refuse, and why a rollback closes.

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
1. [user] <each pre-deploy `What to do` step, with its note>
2. [user] Back up: `substratectl export` (step 5)
3. [operator] Stop the server; copy the data root; dump the database (step 5)

Deploy
4. [operator] Image `ghcr.io/geoah/substrate:0.105.0`; env changes: none

After the deploy
5. [operator] `repository verify` every repository (step 7)
6. [user] Take the provider, then the sample upgrades `substratectl catalog` offers
7. [user] Client changes: <each change, with the file it touches>

Skipped notes
- v0.103.0 "<heading>": the user has no Slack account
```

Wait for the user's approval. A note whose `What to do` does not match what
you found in step 1 is a question for the user; do not guess at it.

## Step 4: Do the pre-deploy steps

The old server is still running. Carry out every plan step marked for before
the deploy. Typical ones:

- Rewrite the records a note says would block an upgrade. The boot upgrade
  never runs a lossy step, so a shipped change that would remove values from
  live records is refused until those records are rewritten.
- Add what a note says a record must now declare, for example the headers
  a webhook trigger's callable reads (`source.webhook.headers`).

## Step 5: Take a backup

The first boot of the new binary closes the rollback, so the backup comes
last before the deploy, after every pre-deploy write.

**User.** Download the recovery export while the server still runs:

```bash
substratectl export            # writes <authority>-<head>.tar, refuses to keep a truncated archive
```

The export opens anywhere with the recovery key the user saved at
registration. Ask the user to confirm they still have that key; do not ask
them to show it to you.

**Operator.** Stop every server process that opens the database, and leave
them stopped until step 6. Then copy the data root and dump the database, as
one pair:

```bash
rsync -a "$SUBSTRATE_DATA_ROOT"/ /srv/substrate-backup/<date>/
pg_dump "$DATABASE_URL" > /srv/substrate-backup/<date>.sql
```

On the compose deployment Postgres publishes no port, so dump it through its
container (`-T`, so no terminal mangles the dump). `docker compose cp` reads
a stopped container:

```bash
docker compose stop substrate
docker compose cp substrate:/var/lib/substrate ./substrate-backup
docker compose exec -T postgres pg_dump -U postgres substrate > ./substrate-backup.sql
```

Never run `docker compose down -v`: it deletes the database, the data root
and the key volumes.

Confirm that the user holds a copy of `SUBSTRATE_CREDENTIAL_KEY` stored
apart from the data copy. On compose it is `/keys/credential.key` in the
`substrate-keys` volume, unless the environment sets one. Without it the
secrets in the copy (provider credentials, API keys, the login credential)
cannot be opened. Never print the key, copy it into a file, or paste it into
a message.

**Show the user:** where the backup is and what it holds.

## Step 6: Deploy the new binary

Pin the image to the target tag (`ghcr.io/geoah/substrate:0.105.0`). On
compose, replace `build: .` or the old tag with that `image:`. Apply every
env var change the notes list.

Start exactly one server process, and only after every old one has stopped.
A rolling deploy that starts the new process beside the old one is refused,
because a repository has one writer. Watch the log until the server is
serving: a long boot import logs `still booting` every 10 seconds, and that
is progress, not a hang.

When the server refuses, it says what it refused. Do not work around a
refusal by editing tables or files:

| The log or the API says | Meaning | Do |
| --- | --- | --- |
| `the database applied migrations this binary does not carry` | a newer binary already migrated this database | deploy that release or a later one, or restore the backup |
| `migration(s) this database applied are not the ones this binary carries` | the database was migrated by a different build of the same migration | restore the backup; there is no repair |
| `the repository recorded migrations this binary does not carry` | a newer binary already ran repository migrations on it | deploy that release or a later one |
| `the changelog speaks a newer dialect than this binary can replay`, or `the store speaks a newer schema dialect than this binary` | the binary is older than the repository | deploy a newer binary |
| `another process is this repository's writer`, or `another process holds the changelog writer lock` | a second server process opened the same repositories | stop every other process, then start one |
| a refusal naming each repository and two key ids | this host's `SUBSTRATE_CREDENTIAL_KEY` is not the key the repositories were sealed under | set the original key |
| the API answers `503 unavailable` with `Retry-After` for one repository | that repository's open was refused; the server log names it and why | fix what the log names, then restart |
| `REFUSED to upgrade a repository's shipped vocabulary`, with the guard lines under `refused` | the seeded packages stay at their stored version; the lines name the kind, the property and the records to rewrite | rewrite those records, then restart the server |
| `function body failed to prepare at repository open` | that function's deliveries park; the repository still serves | re-apply a working function (a provider's arrives with its upgrade in step 8), then retry the parked deliveries |

## Step 7: Verify

**Operator.** Check the server, then verify every repository with the new
server's `substratectl` (see above):

```bash
curl -fsS "$SUBSTRATE_SERVER/healthz"
curl -fsS "$SUBSTRATE_SERVER/.well-known/substrate/server.json" | jq -r '.server.version'   # the target tag
docker compose exec substrate sh -c \
  'SUBSTRATE_CREDENTIAL_KEY="${SUBSTRATE_CREDENTIAL_KEY:-$(cat /keys/credential.key)}" exec substratectl repository verify <repository>'
```

`repository verify` walks the segment files and opens every sealed file. It
takes no lease, so it is safe beside the running server.

**User.**

```bash
substratectl catalog
curl -fsS -H "Authorization: Bearer $SUBSTRATE_TOKEN" "$SUBSTRATE_SERVER/api/v1/vocabulary/upgrade" | jq
substratectl get substrate.reamde.dev/core/package -o yaml   # look for quarantined: true
substratectl trigger status
substratectl sync status
```

- **`lands at restart`** on a seeded package in `catalog`: the upgrade is
  admitted but runs only at a repository's first open. Ask the operator to
  restart the server.
- **`blocked`**: the guard lines under the table name the kind, the property
  and the count of live records in the old shape. Rewrite those records.
  Then a seeded package needs a server restart, and a provider or sample
  needs its `install` or `import` again (step 8). A plan that rewrites more
  records than `SUBSTRATE_CONVERSION_CEILING` (10000 by default) is blocked
  too; the operator may raise the ceiling.
- **`quarantined: true`** on a package: the new binary does not admit its
  stored declarations, and `quarantineReason` says why. Its kinds refuse
  writes and its functions do not run until a valid closure is re-applied.
  For the user's own package, fix the declarations and `substratectl apply`
  them. For a provider or sample, install or import it again (step 8). If
  the marker stays, show the user the `quarantineReason`.
- **Parked deliveries or a new last error** on a trigger, or a sync account
  in an error state: read the error before you retry anything. `substratectl
  trigger parked <id>` lists them, and `substratectl trigger retry <id>
  <parked-id>` retries one.

**Show the user:** each check and its result.

## Step 8: Take the upgrades the catalog offers

`substratectl catalog` lists each provider or sample with a newer shipped
version as a motion (`16 -> 17`). Read the conversion steps printed under the
table before you take one. Take the providers first, then the samples,
because a sample's suggested mapping can wait on a provider's upgrade:

```bash
substratectl install providers.substrate.reamde.dev/github   # a provider: re-running install is the upgrade
substratectl import samples.substrate.reamde.dev/tasks       # a sample: importing again is the upgrade
```

A sample can require another at a minimum version (`tasks` requires `people`
4 and `scheduling` 2). When an import is refused naming such a floor, import
the sample it names first.

Four cases are refused:

- **A blocked upgrade**: a dropped kind, a narrowed property or a removed
  callable that live records or triggers still use, or a plan above the
  conversion ceiling. Clear the blocker (step 7); no flag does.
- **A lossy plan**, on either command: a step removes values from live
  records.
- **An `edited copy`**, on `import`: a sample the user changed since the
  import. A re-import replaces the package whole, so the edits are lost.
- **A missing floor**, on `import`: see above.

A lossy plan and an edited copy clear only with `--allow-data-loss`. Show the
user the steps or the edits that would be lost, and pass the flag only after
they say yes. The old values stay in the changelog either way.

Then carry out the notes aimed at the user's own packages (a new key to
declare, a deprecated one to replace) with `substratectl apply -f`. A change
that removes values from live records is refused the same way, and
`substratectl apply --allow-data-loss` takes the same explicit yes.

## Step 9: Update clients

- **Install the matching `substratectl`.** Each release attaches
  `substrate_<version>_<os>_<arch>.tar.gz`, for example
  `substrate_0.105.0_linux_amd64.tar.gz`. `substratectl version` confirms it.
- **Change the client code.** Carry out every `What to do` step aimed at
  clients: routes, request and response fields, kind references, agent tool
  names.
- **Run the user's tests** against the upgraded server, and show the
  results.

## When it goes wrong

- **Roll forward.** A release that migrated the schema, stamped a newer
  dialect, ran a repository migration or shipped a new declaration key
  leaves the database or its repositories unreadable to an older binary.
  Assume the target did at least one of these. Deploying a binary at or
  above the target always works.
- **To go back, restore the backup with the old binary.** Stop the server.
  Create a fresh, empty database and restore the dump into it. Empty the
  data root and restore the data copy into it, so no segment the new binary
  wrote survives. Boot the old server, then stop it. Run `substratectl
  repository rotate-generation <repository>` for each repository with the
  old server's `substratectl`, and start the server again. The rotation makes
  clients re-list instead of resuming change cursors from a history that no
  longer exists. The
  [backups](https://github.com/geoah/substrate/blob/main/docs/operations.md#backups)
  section has the full procedure.
- **A user's export** restores through the operator. It goes under the data
  root of a stopped server whose database holds no row for that repository,
  a fresh database for example. A database that still holds the row
  reconciles the older files forward from the table instead of rolling back.

## What not to do

- Never deploy without reading every note in the range.
- Never deploy the `latest` tag, or roll an image back over a database the
  new binary has booted on.
- Never run an operator command with a `substratectl` from another release
  than the server's.
- Never edit `schema_migrations`, `repository_migrations`, the `records`
  table or the files under the data root by hand to get past a refusal.
- Never pass `--allow-data-loss` without the user's explicit yes to the
  steps it confirms.
- Never print, store or send `SUBSTRATE_CREDENTIAL_KEY`, a token, a password
  or the recovery key.
