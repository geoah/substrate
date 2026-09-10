# Running one locally

A local substrate is the binary from this tree and a Postgres container beside
it. `mise run dev` starts both: Postgres on `:5433`, the server on `:8080`,
invite code `let-me-in`, and every piece of state under `.dev/`. It serves the
API alone until `web/console/dist` exists, because the dev task passes
`WEB_DIR` only when it does: run `mise run console:build` once and the console
is at `/` from the next start, or `mise run console:dev` to serve it on `:5173`
proxying `/api` to `:8080`. It runs the binary rather than an image, so a
change is a restart and not a rebuild. `mise tasks` lists the whole family;
these are the ones a day needs:

```bash
mise run dev            # foreground; dev:up is the same in the background
mise run dev:totp       # the same substrate with the second factor ENFORCED
mise run dev:status     # the database, the server, the console, the URLs
mise run dev:restart    # rebuild and restart; the data stays
mise run dev:logs
mise run dev:wipe       # DELETE the database and .dev/data
mise run dev:dsn        # the DSN the operator hat takes as --dsn
```

## What the dev tasks set

- `SUBSTRATE_DATA_ROOT` is `.dev/data`, so each repository's directory (its
  manifest, changelog segments, sealed files and blobs) lands there.
- `SUBSTRATE_CREDENTIAL_KEY` is minted once into `.dev/credential.key` and
  every start reuses it. It wraps each repository's data-encryption key, and
  an operator command that writes sealed material reads the same file.
  `mise run dev:status` prints both paths.
- `SUBSTRATE_INVITE_CODE` is `let-me-in`.
- `SUBSTRATE_INSECURE_DISABLE_TOTP` is `true` on every `dev*` task except
  `dev:totp`, so registering and signing in are a repository name and a
  password. The engine still mints and seals a TOTP seed, and
  `GET /.well-known/substrate/server.json` reports which door is running
  (`registration.totpRequired`), which is what the console and `substratectl`
  read before asking anybody for a code. Test a change to the door under
  `mise run dev:totp`, where the factor is enforced, and never by setting the
  variable against anything else
  ([the local switch](auth.md#the-second-factor-can-be-switched-off-locally)).

## The database and the data root are wiped together

`mise run dev:wipe` removes both, and it has to: a data root that outlives its
database is imported at the next boot, and a database that outlives its root is
written back out ([what happens at boot](operations.md#what-happens-at-boot)).
Registration is one-shot per repository and there is no unregister, so testing
the door a second time means wiping first. `mise run dev:reset` is the wipe and
a fresh start in one.

## Your first user, and the operator hat

Register against the local address and the invite code above, then read
[getting started](getting-started.md) from there:

```bash
bin/substratectl register --server http://localhost:8080 --repository ada
```

The operator commands take the DSN and the data root instead of a token, and
both have to be in the environment of the command itself: the dev tasks export
`SUBSTRATE_DATA_ROOT` into the server they start, not into your shell, and an
operator command refuses without it, naming the variable. `mise run dev:status`
prints the two export lines to copy, one for the data root and one for the
credential key that the commands writing sealed material need:

```bash
SUBSTRATE_DATA_ROOT="$PWD/.dev/data" \
  bin/substratectl --dsn "$(mise run dev:dsn)" repository list
```

[The operator commands](operations.md#operator-recovery) is what each one does,
and which of them need the server stopped.

## Running the binary by hand

Four variables, and the rest have working defaults: `DATABASE_URL`,
`SUBSTRATE_DATA_ROOT`, `SUBSTRATE_CREDENTIAL_KEY` and
`SUBSTRATE_INVITE_CODE`. `PORT` is `8080` and `WEB_DIR` names a built console
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
