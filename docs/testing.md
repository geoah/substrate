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
  runner keeps alive.

```bash
mise run test:db                                        # a container per binary
SUBSTRATE_TEST_DATABASE_URL="$(mise run dev:dsn)" mise run test:db
```

`internal/testenv` layers a whole substrate on top of that: a real engine, a
real HTTP listener and a real token, which is what the end-to-end cases drive.

**Run the engine suite on its own when you are working in it.** `go test ./...`
across the whole tree starves the container, and the cases then fail with
`connection refused`, which reads like a code failure and is not one. That is
why `test:db` passes `-p 1`, one test binary at a time: each binary provisions
its database once, including `CREATE EXTENSION`, which Postgres will not do
concurrently. A `*_db_test.go` failure that looks arbitrary usually is, so
confirm it alone before believing it:

```bash
go test ./internal/engine/...          # ~4 minutes, and the answer you can trust
go test ./internal/engine/ -run TestFold -v
```

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

## Coverage

```bash
mise run test:coverage     # both halves, then the total
go tool cover -html=coverage.out
```

There is **no threshold and no badge**, deliberately. A number that fails a
build teaches people to write tests that move the number. What the profile is
for is the opposite question: which paths does nothing exercise at all. CI
keeps `coverage.out` as an artifact on every run, so a reviewer can answer
"is that new branch covered" without running anything.

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
`internal/substrate/wire_test.go` reflects over the structs and writes the field
names they serialize; the vitest beside the golden asserts the TypeScript
carries exactly those keys, using `Record<keyof T, true>` maps that `tsc`
refuses to compile if they are missing a key or carry a spare one.

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
chain works end to end against them — are what the LIVE suite is for.

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
cost tallies, the child's `parent` edge, the tool result in the transcript, the
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
SUBSTRATE_LLM_TEST_OPENAI_MODEL=gpt-4.1-nano mise run test:llm
SUBSTRATE_LLM_TEST_ANTHROPIC_MODEL=claude-3-5-haiku-latest mise run test:llm
```

Both halves honor the same two variables.

### Why it stays out of the default suites

Every live case is named `TestLive…`. One name, used twice: `test:llm` selects
the set with `-run '^TestLive'`, and `test:db` excludes it with
`-skip '^TestLive'`. On top of that every live case skips under `-short`, which
is what `test:short` runs.

So `mise run test` spends nothing even on a machine whose environment is full
of keys — the live suite runs when you ask for it by name, and never as a side
effect of running the tests.

## What CI runs

Seven jobs, each one `mise run ci:<job>`, defined once in `.mise.toml` so the
pipeline is reproducible on a laptop:

| Job | Task | Is |
| --- | ---- | -- |
| lint | `ci:lint` | formatting, every linter, the release config, the kinds version guard |
| cross compile | `ci:cross` | build and vet for linux and darwin, amd64 and arm64 |
| go test | `ci:go` | both halves, with the coverage profile kept as an artifact |
| race | `ci:race` | the short suite under `-race` |
| audit | `ci:audit` | govulncheck and pnpm audit |
| console | `ci:console` | typecheck, lint, format, test, build |
| image builds | `ci:image` | the image builds from a clean tree |

CodeQL runs beside them in its own workflow, on a schedule as well as on
changes, because its queries change even when the code does not.
