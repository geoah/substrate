# Testing

What the suites are, how to run them, and which one to reach for. There is a
lot of test code here: roughly as much as there is source, and the database
suites are where most of the behaviour is actually pinned down.

## The suites, and what each is for

| Suite | Task | Wants | Roughly |
| ----- | ---- | ----- | ------- |
| Short | `mise run test:short` | nothing | seconds |
| Database | `mise run test:db` | Docker, or a DSN | ~2 minutes |
| Both | `mise run test` | the same | ~2 minutes |
| Race | `mise run test:race` | nothing | ~1 minute |
| Coverage | `mise run test:coverage` | the same as `test` | ~2 minutes |
| Console | `mise run console:test` | pnpm | seconds |
| Live | `mise run test:llm` | provider keys, money | ~1 minute |
| End-to-end | `mise run test:e2e` | Docker; leaves data | ~2 minutes |

`mise run test` is the one to run before pushing. `mise run ci` is the whole
pipeline as CI runs it, including the linters, the console and an image build.

**The short suite** is everything that needs no database: the API against its
hand-written fake (`internal/api/fake_test.go`), the CLI's commands, the
vocabulary, the sandbox, the runner's protocol. It is fast enough to run on
every save.

**The database suites** are the engine, the catalog and the whole-server
harness. They are the ones that hold the real contracts, because the changelog,
the fold and admission are only themselves against Postgres.

## The database suites

A file named `*_db_test.go` wants a database. It gets one of two ways, and does
not care which:

- **A container of its own.** `internal/testdb` starts a pgvector container and
  hands out one DSN per test binary. This is what happens on a laptop, and it
  needs nothing but Docker.
- **A server you point it at.** Set `SUBSTRATE_TEST_DATABASE_URL` and `testdb`
  uses that instead. This is how CI runs, against a service container the
  runner keeps alive. Two requirements, both refused with a message rather
  than worked around: the URL is a `postgres://` URL, and its role has
  `CREATEDB`, because the engine suite copies a migrated template database
  per test. The template also needs the `vector` and `pgcrypto` extensions,
  and `vector` is not a trusted extension, so either the role is a superuser
  or you install both into `template1` once (connect to `template1` and
  `CREATE EXTENSION` each), after which every new database inherits them and
  `CREATEDB` is enough. The server is not changed unless
  `SUBSTRATE_TEST_DATABASE_DISPOSABLE=true` says it may be, in which case
  `testdb` turns off `fsync`, `synchronous_commit` and `full_page_writes` with
  `ALTER SYSTEM` and a reload, the way it does on its own container; that adds
  a third requirement, a role allowed to run both, and the refusal names the
  variable when it is not. Every
  database the run makes is dropped when the binary exits (`testdb.Main`), and a
  `sub_tpl_*` or `sub_test_*` database older than six hours with nothing
  connected is dropped at the next run's start, so a killed binary does not
  accumulate them.

```bash
mise run test:db                                        # a container per binary
SUBSTRATE_TEST_DATABASE_URL="$(mise run dev:dsn)" mise run test:db
```

`internal/testenv` layers a whole substrate on top of that: a real engine, a
real HTTP listener and a real token, which is what the end-to-end cases drive.
It also holds the release acceptance drill, `TestReleaseAcceptanceDrill`
(`acceptance_db_test.go`): one repository holding every state a restore has
to bring back (changed kinds, a cleared label, an attachment, sealed values,
conflicting mapping offers, a merge and a split, a purge, settled, pending and
parked deliveries, a parked webhook, a paged drain parked mid-cursor), stopped,
snapshotted with `repository snapshot`, rewrapped under another credential
key with the recovery key and imported into an empty schema, then compared
record by record and resumed. Its later stages kill the import at batch
boundaries, take a second repository through the oldest directory format the
reader accepts, and resume a saved change cursor against the replaced
history. It runs with the rest of `test:db` and takes about ten seconds.

```bash
go test -count=1 -run TestReleaseAcceptanceDrill -v ./internal/testenv/
```

**Run the engine suite on its own when you are working in it.** `go test ./...`
across the whole tree starves the container, and the cases then fail with
`connection refused`, which reads like a code failure and is not one. That is
why `test:db` passes `-p 1`, one test binary at a time: each binary provisions
its database once, including `CREATE EXTENSION`, which Postgres will not do
concurrently. A `*_db_test.go` failure that looks arbitrary usually is, so
confirm it alone before believing it:

```bash
mise run test:db:engine                # about 70 s on 16 cores; the answer you can trust
go test ./internal/engine/ -run TestFold -v
```

The engine package is about 850 top-level tests: about 70 s of wall time on a 16
core machine (measured 2026-09-08, down from 134 s the same day; the section
below says where the time went), and longer on a 4 vCPU CI runner (six to
eight minutes before that change; a shard's log says what it is now), so the
comment above is this machine's number, not a promise.

`test:db:engine` is the engine package with `test:db`'s flags, and it is also
the task CI shards: with `SHARD` and `SHARDS` in the environment it runs one
slice of the package (`SHARD=3 SHARDS=8 mise run test:db:engine`), which is
how a red shard is reproduced by number. `test:db:rest` is every other
database package. The cut is described under [What CI runs](#what-ci-runs).

### The engine fixture, and where the time goes

Almost every engine test opens its own service, creates a repository and
imports the sample vocabulary. Four things the harness does keep that under
a minute and a half on 16 cores; each was measured on its own, and
`docs/testing.md` is the one place that explains them (the code comments
point here).

**A migrated template database, copied per test.** `engine.Open` runs once
per binary on a template (`migratedTemplate` in
`internal/engine/export_test.go`, a `testdb.Template`) with no repository, so
the template holds the recorded migrations, the roles' grants and the
shipped indexes and nothing else; `engine.MigratedDSN(t)` hands each test a
`CREATE DATABASE ... TEMPLATE` copy. `engine.Open` still runs every boot step
on the copy and skips only the DDL. A copy beside an empty data root is
exactly a fresh install: nothing on either side. Before this, every test
migrated a fresh schema, and the migration runner's advisory lock was
keyed on one constant, so the parallel suite ran its migrations one test at
a time: 90% of Postgres's time in a run was that lock. The lock is keyed on
`current_schema()` now, like the engine's other three (no effect on a
deployment, one schema per database), which is what the packages still on
`testdb.NewSchema` (catalog, testenv, substratectl) get. The from-empty
migration still runs three times per engine binary: the template build,
`TestRepositoryProvisioningAndProjections` and
`TestAssertPoolPrincipalRejectsSuperuser`.

**One opener.** `engine.OpenForTest(t, ctx, dsn, opts...)` is `engine.Open`
with the shipped core kinds (`engine.CoreKindsDir`), the binary's credential
key and the test's TOTP clock; every test open goes through it, and a
caller's options win where they name the same thing. The clock
(`engine.ClockOf(t)`, keyed on the full test name and forgotten when the
test ends) is what `waitStep` advances by one `engine.TOTPPeriod` where it
used to sleep through a real 30 second window.

**The container, and how it is reached.** The pgvector container `testdb`
starts runs with `fsync=off`, `synchronous_commit=off` and
`full_page_writes=off` on its command line: it dies with the binary, and
`DROP DATABASE` forces a checkpoint that fsync makes slow. `testdb` connects
to the container's own IP where the host can route to it, else the published
port: the published port is docker-proxy, one process relaying every
connection, and it was the queue every test waited in (98 s to 84 s). Each
test drops its copy in its cleanup and `testdb.Main` drops what is left after
`m.Run` (a dropper goroutine off the tests' path measured no gain: 69 to
80 s against 67 s). CI's service containers keep their data directory on a
tmpfs (`--tmpfs` in the job's `options`) and take no command line, so the
jobs set `SUBSTRATE_TEST_DATABASE_DISPOSABLE=true` and `testdb` applies the
same three settings through `ALTER SYSTEM`. The one test that starts a
container of its own (`TestOpenFailsClosedWithoutSafeRoles`) passes
`testdb.DurabilityOff()`, and a dev database `mise run dev` creates carries
the flags on its command line (one created before this keeps the image's
defaults until `dev:wipe` recreates it).

**The data roots on tmpfs.** Every changelog write fsyncs
([0062](decisions/0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md)),
and sixteen repositories fsyncing one ext4 journal serialize on it (84 s to
67 s). `testdb.Main` puts `TMPDIR`, and with it every `t.TempDir()`, under
`/dev/shm` when that is a tmpfs with at least 512 MB free, and says so once
on stderr. A container's 64 MB `/dev/shm` falls back to the default;
`TMPDIR=/tmp` opts out.

To see what a run spent, capture it as JSON once and read it:

```bash
go test -count=1 -p 1 -skip '^TestLive' -json ./internal/engine/... > timing.json
mise run test:timing -- timing.json     # per-package wall, then the 30 slowest tests
```

### Testing the boot upgrade

A test for boot-upgrade behavior (`upgrade_guard_db_test.go`, whose harness
patches a copy of the shipped tree and opens the same database twice) must bump
the version of the DECLARATION it patches, not only its package's: a kind that
pins a `data.version` of its own keeps whatever it stands at when the package
moves, so the upgrade under test never runs and the case passes vacuously.
`pinVersion` is the helper for it, and `bumpPackageVersion` alone is enough only
for a declaration that pins no version. Run any new case against the un-fixed
code once; a boot-upgrade test that passes both ways is proving nothing.

## The race detector

```bash
mise run test:race
```

The short suite under `-race`. The engine runs background loops and the runner
reaps process groups through `Setpgid` and `syscall.Kill`, so concurrency bugs
here are real rather than theoretical. This is the cheap gate; when you have
touched the engine's goroutines specifically, run the expensive one by hand:

```bash
go test -race ./internal/engine/...
```

## The confinement cases

`internal/sandbox` and `internal/runner` assert what a function body cannot do:
read the substrate's environment out of `/proc`, write outside its work dir,
reach another installation's scratch, open a socket its manifest did not
declare. All of it rests on Landlock and seccomp, so where the kernel offers
neither, those cases skip.

That skip is also how the promise disappears quietly: an image change, a
distribution `lsm=` change or a runner upgrade removes the confinement, every
case goes from passing to skipping, and the build stays green.
`SUBSTRATE_TEST_REQUIRE_SANDBOX=1` closes that. The guards fail instead of
skipping, and `internal/sandboxtest` counts, so a run that skipped its way to
zero cases cannot exit 0 either. Each package's `TestMain` declares how many
cases guard on the confinement, ten in `internal/runner` and four in
`internal/sandbox`, and the count is held to that **exactly**: adding an
eleventh case, or deleting one of the ten, fails until the number moves with it. A
case that passes the guard and then skips on a precondition of its own (a uid
that cannot make a device node, a probe that will not build) counts as guarded
but not as asserted, and at least one case must have asserted.

```bash
SUBSTRATE_TEST_REQUIRE_SANDBOX=1 go test -count=1 -short ./internal/sandbox/... ./internal/runner/...
```

`-short` because `TestGoBuildAndInvoke` compiles a body with the host
toolchain, which is slow and has a VCS-stamping failure mode of its own; every
confinement case runs in the short suite, which is also the only half of
`ci:go` that reaches `internal/runner`.

`ci:go`, `ci:race` and `ci:coverage` set the variable; `mise run test` does
not, so a laptop with Landlock left out of its `lsm=` list still runs the rest
of the suite. On macOS there is no Landlock and no seccomp at all, so do not
set it there. That those three tasks still set it, and still reach the suite
through `run` rather than a `depends` entry (which mise gives an environment
of its own), is what `mise run lint:sandboxgate` holds: this gate's own
failure mode is a green build, so it gets a guard.

A `-run` or `-skip` relaxes the exact count, because a filter can only shrink
the set, but it does not lift the gate: with the variable set, a filter that
leaves no confinement case to run is a failure rather than a quiet pass.

## Coverage

```bash
mise run test:coverage     # both halves, then the total
go tool cover -html=coverage.out
```

There is **no threshold and no badge**, deliberately. A number that fails a
build teaches people to write tests that move the number. What the profile is
for is the opposite question: which paths does nothing exercise at all. CI
keeps `coverage.out` as an artifact of every push to `main` (the `coverage`
job), so a reviewer can answer "is that new branch covered" without running
anything. A PR's runs carry no profile: the PR jobs are sharded, and merging
their partial profiles would buy nothing a push to `main` cannot wait for.

## The console

```bash
mise run console:test      # vitest
mise run ci:console        # typecheck, lint, format, test, build
```

Vitest with jsdom, beside the code it covers: every module in
`src/lib/api/` has a `.test.ts` next to it, and the pages have component tests.

### The wire drift guard

`web/console/src/lib/api/types.ts` is written **by hand** to mirror the Go
structs in `internal/substrate`. Nothing generates it, so the two halves can
disagree silently, and a renamed Go field used to stay invisible until
something broke in a browser.

`wire.golden.json` is the contract they meet at. The Go test
`internal/substrate/wire_test.go` reflects over the structs and writes, per
shape, the field names they serialize and whether each is required: `true` for
a field the server always writes (`null` included), `false` for one tagged
`omitempty` or `omitzero`, which a reader meets absent. The vitest beside the
golden asserts the TypeScript carries exactly those keys with exactly that
optionality, using `Shape<T>` maps that `tsc` refuses to compile if they are
missing a key, carry a spare one, or mark a `?` key `true`. The same vitest
reads every module under `web/console/src/lib/api/` and refuses an exported
interface that is in neither the golden nor its list of client-only shapes,
each with a reason, so a new mirror is pinned or explained wherever it lives.

A response an API handler builds as a bare `map[string]any` cannot be pinned:
a handler names its reply as a struct in `internal/substrate` first
(`TriggerRan`, `BundlePurged`, `WebhookAccepted`) and adds it to `wireTypes`.

So a Go field that moves fails the Go test first:

```bash
go test ./internal/substrate/ -run TestWireGolden -update   # accept the new shape
```

and then fails the console's test until `types.ts` and its key map agree with
it. Adding a shape to `wireTypes` in that Go test is a deliberate act: it
commits the console to tracking it.

## The live tests

Almost every suite here is hermetic: the agent loop is driven by an in-process
fake that speaks the OpenAI wire, and the engine's database cases run against a
throwaway Postgres container. Two things a fake cannot prove — that the wire
adapters still match what the providers actually accept, and that a whole agent
chain works end to end against them — are what the **live** suite is for.

It buys real completions with real keys, so it runs only when those keys are in
the environment and **skips** otherwise. `mise run test` and CI never need one.

### What it covers

**The adapters** (`internal/llm/live_test.go`), once per wire — OpenAI's own
endpoint and Anthropic's:

- a one-shot completion: content back, and a usage tally the loop's cost
  accounting depends on;
- a tool-call round trip: the model asks for a tool with parseable arguments,
  the answer goes back as a tool turn, and the second completion carries the
  result and no further calls;
- streaming: the deltas, concatenated, are exactly the settled content;
- and on the Anthropic wire, two consecutive user turns — the role-alternation
  fold, which a replayed thread history hits and which fails as a 400 forever
  if it ever regresses.

**The chain** (`internal/engine/agents_live_db_test.go`), one case through the
real engine with no fakes anywhere: two `llmprovider` records (one per wire), a
deterministic function tool, an OpenAI-backed sub-agent, and an
Anthropic-backed root agent that calls the tool, delegates to the sub-agent and
settles. What it asserts is durable state — the thread rows and their token and
cost tallies, the child's `parent` reference, the tool result in the transcript, the
roll-up onto the root — never the model's prose, which is nobody's to promise.

### Running it

```bash
mise run test:llm     # both halves; without keys, everything skips and it exits 0
```

The engine half also wants a database, like every other `*_db_test.go`: Docker
for the pgvector container, or `SUBSTRATE_TEST_DATABASE_URL` pointing at one.
Without keys the skip happens first, so no container is started.

### Keys

`mise` reads a **gitignored** `.mise.local.toml` beside `.mise.toml` — or in
any parent directory of your checkout, which is the tidier place if you keep
several worktrees:

```toml
[env]
OPENAI_API_KEY = "..."
ANTHROPIC_API_KEY = "..."
```

That file is in `.gitignore` and stays there. **Key material never goes into a
committed file** — not a test fixture, not a doc, not a default. Nothing in the
suite prints a key, and nothing should start.

Each case names the variable it wanted when it skips, so a partial setup is
legible: with only `OPENAI_API_KEY` the OpenAI adapter cases run, the Anthropic
ones skip, and the chain — which needs both — skips too.

### Cost, and the models

The suite is deliberately cheap: tiny prompts, `maxTokens` capped at 256, and
the smallest tool-calling model on each wire — `gpt-4.1-mini` and
`claude-haiku-4-5`. A whole pass costs well under a cent. It is still real
money, so it is not something to loop.

Either default can be re-pointed without touching the code, which is also how a
key that lacks one of those models is made green:

```bash
SUBSTRATE_TEST_OPENAI_MODEL=gpt-4.1-nano mise run test:llm
SUBSTRATE_TEST_ANTHROPIC_MODEL=claude-3-5-haiku-latest mise run test:llm
```

Both halves honor the same two variables.

### Why it stays out of the default suites

Every live case is named `TestLive…`, and that one name is used twice:
`test:llm` selects the set with `-run '^TestLive'`, and `test:db` excludes it with
`-skip '^TestLive'`. On top of that every live case skips under `-short`, which
is what `test:short` runs.

So `mise run test` spends nothing even on a machine whose environment is full
of keys — the live suite runs when you ask for it by name, and never as a side
effect of running the tests.

## The end-to-end suite

`mise run test:e2e` drives the dev substrate over HTTP exactly as a client
would: it rebuilds and restarts the server so the binary is this tree's,
registers a fresh throwaway user through the real registration flow, and
walks the implemented cases in
[internal/e2e/CASES.md](../internal/e2e/CASES.md). Two things distinguish it
from every other suite:

- **It leaves its data.** The user and repository the run builds stay in
  place, so a human can sign into the console (the report has the
  credentials) or point `substratectl` at the server and review what the
  suite actually did. `mise run dev:wipe` is the cleanup; each run registers
  a new repository name, so runs never collide.
- **It writes a report.** Every run writes `.dev/e2e/report-<repository>.md`:
  each case, what it tests, every step it took with the answering status, the
  result, and an appendix showing the repository as it was left, changelog
  included.

The test itself (`internal/e2e`) skips wherever `SUBSTRATE_E2E_SERVER` is
unset, so `mise run test` and CI never touch it, the same shape as the live
suite's key gate.

The suite reads its whole environment from those variables, so it runs against
any substrate, not only the dev one: `SUBSTRATE_E2E_SERVER` is the base URL,
`SUBSTRATE_E2E_INVITE` the invite code (default `let-me-in`),
`SUBSTRATE_E2E_DSN` and `SUBSTRATE_E2E_CTL` the operator hat the `dsn` cases
need, `SUBSTRATE_E2E_CREDENTIAL_KEY` the key those commands read, and
`SUBSTRATE_E2E_REPORT_DIR` where the report lands. Point them at a throwaway
server of your own when the shared dev stack is somebody else's.

`SUBSTRATE_E2E_TIMEOUT` bounds one exchange and defaults to 30s. Raise it on
a loaded machine: a write this client abandons can wedge the repository
([issue 516](https://github.com/geoah/substrate/issues/516)), and every case
after it then fails for a reason that is not the code's.

A case a unit suite already pins is not in the list: `internal/api` against
its fake, `internal/engine` against a real Postgres, and
`internal/testenv`'s conformance table over a real socket hold those, and
CASES.md holds what only a live server shows.

## What CI runs

Every job is one `mise run ci:<job>`, defined once in `.mise.toml` so the
pipeline is reproducible on a laptop:

| Job | Task | Is | Runs |
| --- | ---- | -- | ---- |
| lint | `ci:lint` | formatting, every linter, the release config, and the two diff guards: the `kinds/` and `samples/` version bump (`kinds:check`) and the write-once files (`frozen:check`) | always |
| cross compile | `ci:cross` | build and vet for linux and darwin, amd64 and arm64 | always |
| changes | `ci:changes` | reads the diff against the base branch and answers `go=true` or `go=false`: does any changed file reach a Go test? | always |
| go test | `ci:go` | the short suite, then every database package but the engine (`test:db:rest`) | when `go=true` |
| engine 1/8 to 8/8 | `ci:engine` | one shard of the engine package each (`test:db:engine` with `SHARD` and `SHARDS`) | when `go=true` |
| go gate | (in the workflow) | the one check to require: red unless `changes` succeeded and `go test` and every shard succeeded or were skipped by its answer | always |
| coverage | `ci:coverage` | the whole suite, unsharded, with the coverage profile kept as an artifact | push to `main` |
| race | `ci:race` | the short suite under `-race` | always |
| audit | `ci:audit` | govulncheck and pnpm audit | always |
| console | `ci:console` | typecheck, lint, format, test, build | always |
| image builds | `ci:image` | the image builds from a clean tree | always |

CodeQL runs beside them in its own workflow, on a schedule as well as on
changes, because its queries change even when the code does not.

### The database suite, cut for the runner

The engine package alone is 380 to 500 seconds on the 4 vCPU runner, and
under contention it has passed Go's 10 minute default with no test failing.
CI therefore runs it as eight matrix jobs. `.mise/engineshard.sh` lists the
package's top-level tests with `go test -list`, sorts the names and gives
shard `k` every eighth name starting from the `k`th, as one `-run` regex
anchored at both ends. Every test lands in exactly one shard by construction,
a new test lands in one without anybody editing a list, and the same tree cuts
the same way on every machine, so `SHARD=3 SHARDS=8 mise run test:db:engine`
reruns exactly what shard 3 ran. The shard count is written once, as the
`shard:` matrix in `.github/workflows/ci.yml`; the job name and `SHARDS` both
read `strategy.job-total`.

The short suite rides in `go test` with the four small database packages
rather than on a runner of its own: together they are about a minute of test
time, and a job's setup (checkout, toolchain, cache, service container) is
half of that again.

`changes` is the path gate. `.mise/changescheck.sh` diffs the merge base with
the PR's base branch against the tree and answers `go=false` only when every
changed file matches a pattern nothing a Go test reads: `docs/`,
`web/console/` (except `wire.golden.json` and `record-schema.ts`, which Go
tests read), `.github/` other than `ci.yml`, `*.md`, the root linter configs,
`Dockerfile.release` and `compose.yaml`. `Dockerfile` and `.goreleaser.yaml`
count as relevant because `internal/build` reads them. `kinds/` and `samples/`
are embedded whole, so any file under them counts, and an unmatched file
counts, because a needless run is cheaper than a red test merged green. The diff is read with `--no-renames`,
so a Go file moved onto an inert path is seen on both sides, and a base the
script cannot resolve fails the job rather than answering `false`. `go test`
and the engine shards carry `if: needs.changes.outputs.go == 'true'`. A push
to `main` answers `true` without diffing, and runs `coverage` besides.

`go gate` is the check to require. GitHub counts a skipped job as passing for
a required check, the gated jobs are skipped both when `changes` answers
`false` and when `changes` itself fails, and a skipped matrix is one `engine`
job rather than eight named shards, so a ruleset naming `go test` or a shard
would let a PR whose gate crashed merge untested. `go gate` runs after all of
them with `if: always()` and fails unless `changes` succeeded and `go test`
and the engine matrix each succeeded or were skipped. On a docs-only PR it is
green with no suite run; on a Go PR it is the suite's verdict.

A shard runs under `-timeout 12m` inside a 15 minute job, so a hang dies by
Go's timeout with a goroutine dump rather than by the runner's with nothing.
The script also refuses to run if a package under `internal/engine/` has
grown tests of its own, since the shards run only the root package and
`test:db` runs the tree.

`mise run lint:ci` (`.mise/cicheck.sh`, part of `lint`) holds both scripts:
each path-gate scenario is a throwaway git repository with the verdict it
must give, including the unresolvable base that must fail, and the shard
partition (`.mise/shardselect.sh`) is run over a fixed list that the eight
shards together must reproduce exactly once.

`mise run test`, `test:db` and `test:coverage` are untouched by the cut: each
is still the whole suite, sequential, on one machine, and `mise run ci` runs
`ci:coverage` in place of `ci:go` and the shards, because it is the same tests
once with the profile.

Next: the [built-in kinds](builtin-kinds.md), the vocabulary a repository
can import.
