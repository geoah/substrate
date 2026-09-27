# Running one locally

A local substrate is the binary from this tree and a Postgres container beside
it. `mise run dev` starts both: Postgres on `:5433` and the server on `:8080`,
with no invite code (set `SUBSTRATE_INVITE_CODE` in your shell to test the
gate). It serves the API alone until `web/console/dist`
exists, because the dev task passes `WEB_DIR` only when that directory is
there: run `mise run console:build` once and the console is at `/` from the
next start, or `mise run console:dev` to serve it on `:5173`. The binary runs
from the tree rather than an image, so a change is a restart and not a
rebuild.

The console's dev server forwards everything that is not the console to a
substrate: the API (`/api`, `/healthz`, `/.well-known`) and the auth door at
the root (`/login`, `/register`, `/password`, `/totp`, `/tokens`), those five
only for a request that does not ask for HTML, so `/login` and `/register`
still load as console pages. The target is `http://localhost:8080` unless
`VITE_PROXY_SUBSTRATE` names another, so the same task runs this tree's
console against any substrate, a server moved by `SUBSTRATE_DEV_PORT`
included:

```bash
VITE_PROXY_SUBSTRATE=http://localhost:8081 mise run console:dev
VITE_PROXY_SUBSTRATE=https://substrate.example.com mise run console:dev
```

The state lives in two places, which is why throwing it away is a task and not
an `rm`: `.dev/` in the tree holds the data root, the pid, the log and the
credential key, and Postgres keeps its own files in the docker volume
`substrate-dev-db-data`. Both ports move when a second worktree already holds
them: `SUBSTRATE_DEV_PORT` for the server and `SUBSTRATE_DEV_DB_PORT` for the
database. `mise tasks` lists the whole family; these are the ones a day needs:

```bash
mise run dev            # foreground; dev:up is the same in the background
mise run dev:totp       # the same substrate with the second factor ENFORCED
mise run dev:status     # the container, this tree's database, the server, the console, the URLs
mise run dev:restart    # rebuild and restart; the data stays
mise run dev:stop       # stop this tree's server; the shared Postgres and the data stay
mise run dev:logs
mise run dev:wipe       # DROP this tree's database and DELETE .dev/
mise run dev:wipe:all   # dev:wipe, then remove the container and its volume
mise run dev:dsn        # the DSN the operator hat takes as --dsn
```

## One database per tree

The container is shared by every checkout and worktree on the box, and the
DATABASE INSIDE IT IS NOT: it is named `substrate_<the tree directory's name>`
— `substrate_substrate` for a checkout at `src/substrate`,
`substrate_issue_554` for a worktree named `issue-554`. Every start and
`mise run dev:status` print the name, and `mise run dev:dsn` names it in the
DSN, so an operator hat pointed at the wrong tree is visible rather than
silently productive.

It is one per tree because one shared database failed once (#539): three
servers on one database run three boot upgrades, three garbage collections and
three trigger dispatchers over one set of repositories, each writing a data
root of its own. A server now refuses to open a repository another process
holds
([one writer per repository](operations.md#one-writer-per-repository-and-a-second-is-refused)),
so the arrangement fails loudly instead of drifting — but the database being
separate is what keeps it from arising.

Two caveats. TWO TREES WITH THE SAME DIRECTORY NAME SHARE A DATABASE, because
the name is the directory's and nothing else: name worktrees distinctly, or set
`SUBSTRATE_DEV_DB_NAME` to something of your own. And the HTTP port is still
`8080` for every tree, so the second `mise run dev` refuses rather than bind
over the first and tells you to set `SUBSTRATE_DEV_PORT`.

## What the dev tasks set

- `SUBSTRATE_DATA_ROOT` is `.dev/data`, so each repository's directory (its
  manifest, changelog segments, sealed files and blobs) lands there.
- `SUBSTRATE_CREDENTIAL_KEY` is minted once into `.dev/credential.key` and
  every start reuses it. It wraps each repository's data-encryption key, and
  an operator command that writes sealed material reads the same file.
  `mise run dev:status` prints both paths. `dev:wipe` removes `.dev/` whole,
  that file included, so the next start mints a new key: it goes with the
  sealed material it wrapped.
- `SUBSTRATE_INVITE_CODE` is whatever your shell holds, and empty by default,
  so the register door reads none. `mise run test:e2e` sets `let-me-in` for
  its own run.
- `SUBSTRATE_INSECURE_DISABLE_TOTP` is `true` on every `dev*` task except
  `dev:totp`, so registering and signing in are a repository name and a
  password. The engine still mints and seals a TOTP seed, and
  `GET /.well-known/substrate/server.json` reports which door is running
  (`registration.totpRequired`), which is what the console and `substratectl`
  read before asking anybody for a code. Test a change to the door under
  `mise run dev:totp`, where the factor is enforced, and never by setting the
  variable against anything else
  ([the local switch](auth.md#the-second-factor-can-be-switched-off-locally)).

## The database, the data root and the key are wiped together

`mise run dev:wipe` drops this tree's database and removes the data root and
`.dev/` around it, and it has to: a data root that outlives its database is
imported at the next boot, and a database that outlives its root is written
back out ([what happens at boot](operations.md#what-happens-at-boot)).
Registration is one-shot per repository and there is no unregister, so testing
the door a second time means wiping first. `mise run dev:reset` is the wipe and
a fresh start in one.

It stops at this tree. The container and its volume hold every other
checkout's database, so `dev:wipe` terminates the backends on its own database,
drops it, and leaves the container running — which is also why `mise run
dev:stop` stops the server and not Postgres. `mise run dev:wipe:all` is the
one that removes the container and the volume, and it takes every tree's dev
database with it.

A container that is gone does not mean the database is: `docker rm` without
`-v` leaves the named volume, and the volume is where the database lives. So
`dev:wipe` starts the container back up to drop the database when it finds the
volume still there, and treats the database as absent only when neither is.
Otherwise it would remove the data root and the credential key while leaving
the database behind, and the next start would reattach it under a freshly
minted key that opens none of its repositories.

## Your first user, and the operator hat

Register against the local address (no invite code is asked for), then read
[getting started](getting-started.md) from there:

```bash
bin/substratectl register --server http://localhost:8080 --repository ada
```

The operator commands take a DSN and a data root instead of a token: `--dsn`
(or `DATABASE_URL`), which `mise run dev:dsn` prints, and
`SUBSTRATE_DATA_ROOT`, which every operator command that opens the repository
directory refuses to run without, naming the variable (`repository list`
reads the database alone). The dev tasks export that variable into the server they
start, never into your shell, so the command needs it itself.
`mise run dev:status` prints the two lines to copy: the data root, and the
credential key the commands that write sealed material read.

```bash
SUBSTRATE_DATA_ROOT="$PWD/.dev/data" \
  bin/substratectl --dsn "$(mise run dev:dsn)" repository list
```

[The operator commands](operations.md#operator-recovery) is what each one does,
and which of them need the server stopped.

## Running the binary by hand

Three variables have no default: `DATABASE_URL`, `SUBSTRATE_DATA_ROOT` and
`SUBSTRATE_CREDENTIAL_KEY`. `SUBSTRATE_INVITE_CODE` is optional, and unset
the register door reads none. `PORT` is `8080` and `WEB_DIR` names a built console
to serve at `/` ([configuration](operations.md#configuration) is the full
table). Postgres needs the `vector` and `pgcrypto` extensions available, and
the DSN must be allowed to create them and the two roles isolation rests on.

Function bodies are confined as far as the kernel allows: `SUBSTRATE_SANDBOX`
defaults to `best-effort`, which runs a body unconfined where a layer is
missing and logs what it got at boot. macOS has none of the layers
([platforms](functions.md#platforms)). A deployment runs `enforce`
([the function sandbox](operations.md#the-function-sandbox)).

Next: [running a substrate](operations.md), the deployment and what it takes to
look after one.
