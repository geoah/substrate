# Provider end-to-end suite

`go test ./internal/providere2e/` proves that each shipped provider bundle
under `kinds/providers.substrate.reamde.dev/` still syncs: it starts a
substrate, installs the bundle, connects an account, drives the sync against
recorded upstream traffic and asserts the rows that land.

The suite needs a database, `python3` and `uv`. It skips under `-short` and
wherever any of the three is missing, exactly like the other database suites.

## What one run does

`TestProviderE2E` starts one substrate for the whole run: it builds
`./cmd/substrated` and `./cmd/substratectl` into a temp directory, takes a
throwaway Postgres schema from `internal/testdb`, mints a credential key, and
starts the server on a free loopback port with the OAuth facility on and
loopback egress allowed. It stops the server at the end and fails the run if
it did not exit cleanly.

Then one subtest per provider, sequentially. Each one registers a fresh
repository through `substratectl register` (the CLI config lives in the test's
temp directory, so nothing touches yours) and hands `runner/e2e.py` the
server, the authority, the repository's bearer, the `substratectl` it just
built and `fixtures/<provider>`. The runner:

1. starts `runner/mockserver.py` over the recordings, on the port
   `providers/<provider>/e2e.json` pins;
2. copies the bundle to a temp directory and rewires the copy at the mock: the
   `oauth2:` endpoints, the API base in the sync body, and the mock's host
   added to `permissions.network`. The files in git are never edited;
3. applies the rewired copy with `substratectl apply`;
4. writes the config record and the account record with every feature on;
5. completes the OAuth dance against the mock's stub, or writes the token when
   `e2e.json` says `"auth": "token"`;
6. waits for the on-connect trigger's runs to settle, then for the account's
   own `lastSyncedAt`;
7. runs `providers/<provider>/scenario.py`, which holds the assertions;
8. prints what the run touched: recordings hit, recordings missed, mirror rows
   by kind, trigger runs.

A non-zero exit from the runner fails the subtest. Everything the runner and
the scenario printed goes to `t.Log` and to `.dev/providere2e/<provider>.log`;
the server's own log is `.dev/providere2e/substrated.log`. That directory is
gitignored and is cleared at the start of each run.

`internal/providertest` is the other half of this. It drives one callable
through the engine with a hand-written fake, in seconds; this drives the whole
closure through a real server over recorded traffic, in minutes.

## Layout

```
e2e_test.go                     the cases and the provider table
harness_test.go                 the server, the door, the toolchain
runner/e2e.py                   the runner: one provider, start to finish
runner/mockserver.py            the file-lookup mock over a recordings directory
providers/google/drive_requests.py  the Drive request constants google's scenario reads
providers/<p>/e2e.json          what the runner cannot guess: hosts, triggers, waits
providers/<p>/scenario.py       the provider's own assertions
providers/github/mock.py        a forwarder to mockserver that github's scenario imports
providers/github/restricted.json  the recorded 403 github's scenario injects
providers/persona.public.json   the fixture cast, which two scenarios check the recordings against
fixtures/<p>/                   the recordings, one JSON file per request
```

The fixtures are here and not under `kinds/` because `kinds/` is embedded
whole into the binary.

`runner/e2e.py` takes four roots from the environment, each defaulting to the
layout above: `SUBSTRATE_E2E_ROOT` (this directory), `SUBSTRATE_E2E_REPO` (the
checkout), `SUBSTRATE_E2E_BUNDLES`
(`kinds/providers.substrate.reamde.dev`) and `SUBSTRATE_E2E_VOCAB` (`samples`,
scanned for record mappings that read a provider's kinds).

## Running one provider

```
SUBSTRATE_TEST_DATABASE_URL='postgres://postgres:postgres@127.0.0.1:5433/postgres?sslmode=disable' \
  go test -count=1 -timeout 60m -run 'TestProviderE2E/whoop' -v ./internal/providere2e/
```

Without `SUBSTRATE_TEST_DATABASE_URL` the suite starts its own pgvector
container. The timeout matters: the whole run is tens of minutes, and Go's
default of ten would kill it mid-provider.

## Why the harness rewrites PATH

The engine confines the `uv` resolve and every function body with Landlock,
and a Landlock grant names a resolved file. A version manager's shim is a
symlink into a tree the grant does not cover, so a substrate whose PATH puts
mise first fails every provider install with `uv sync: exit status 126`, or
with `permission denied` on
`.../uv-cache/environments-v2/.../bin/python` when the virtual environment was
built on the shimmed interpreter.

So `toolchain()` in `harness_test.go` resolves `python3` to the first system
interpreter outside any version manager's tree, resolves `uv` to the real
binary (`mise which uv`, then `EvalSymlinks`), and puts both directories at
the front of the PATH the server is given. `internal/runner/pyhost.go` pins
`uv sync --python` to whatever `python3` resolves to, which is why the
interpreter and not only `uv` has to be pinned.

The harness also points the server's `XDG_CACHE_HOME` at a directory keyed by
that interpreter. The runner's python scratch and uv cache live under
`os.UserCacheDir()`, and an environment built on a different interpreter fails
at exec rather than missing the cache, which is a much harder failure to read.

## The mock ports

`e2e.json` pins one port per provider, 48111 to 48117, so the rewired function
body is identical run to run: the engine serves the previous apply's body, and
a moving port makes the first drain of every run call a dead mock. A case
whose port is busy waits thirty seconds and then fails naming the port. The
harness will not pick another port, because a second listener would serve the
wrong recordings.

Two runs of this suite cannot share a machine.

## The fixtures

The recordings, the scenarios and the runner came from the mneme-v6 tree,
where the bundles were developed and where the pulls they were cut from live.
Re-cutting them is out of scope here: it needs the owner's real accounts, the
pull scripts and the pseudonym map, none of which are in this repository.

Each fixture set carries an `audit.py` that checks the recordings for real
identity. Five of the seven (whoop, linear, notion, github, google) scan
against `tools/persona.local.json` and the raw pull in the mneme tree, and
both of those are gitignored there, so they report `no real corpus` and cannot
run from either checkout. The two that need no corpus were run on 2026-09-22
and both printed `FINDINGS: 0`: beeper (0 filenames, 0 leaves, 0 map keys, 0
prose) and slack (0 filenames, 0 leaves, 0 prose bodies).
