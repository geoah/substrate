# The live end-to-end cases

The case list for `mise run test:e2e` is the code. Each case is one
`runCase(id, title, tests, fn)` in `cases_test.go` (the slice and the stories)
or one `registerCase(order, id, title, tests, fn)` in a group file, and the
`tests` argument is that case's description, which the run copies into its
report. This page holds what those arguments do not say: what the suite is
for, what each precondition means, and how the ids are kept.
[docs/testing.md](../../docs/testing.md) covers running the suite, the
environment it reads and the report it writes.

The suite mocks the world, never the substrate: a fake LLM answers the OpenAI
wire and a fake OAuth provider answers the token endpoint, but every substrate
call crosses the real HTTP door of the real binary against the real database.
A case a unit suite already pins is not here. `internal/api` drives every
route against a hand-written fake, `internal/engine` drives the same writes
against a real Postgres, and `internal/testenv` drives the published error
codes over a real socket in one process, so these cases are what only a live
server, a live database and a real client show. ERR-04 is the one overlap on
purpose: it asks the shipped binary for every code the conformance suite asks
the in-process engine for, and it reads the list from the same source,
`internal/api`'s `code*` declarations.

Five preconditions gate individual cases and steps, and the `test:e2e` task
sets every one it can:

- `totp`: the enforced door, which AUTH-05 and AUTH-07 need and skip without:
  `SUBSTRATE_INSECURE_DISABLE_TOTP=false mise run test:e2e`, or a server of
  your own under `mise run dev:totp`. AUTH-06 and OPR-03 run on either door
  and carry a live code when the door asks for one. ISO-01's second
  registration skips against the enforced door instead, because a second user
  there needs an enrolled seed of its own.
- `egress`: loopback allowed on the server, which is what lets it reach the
  test's own stubs. `SUBSTRATE_EGRESS_ALLOW` governs the server's own dials
  (the agents' completions, the embeddings queue, the OAuth token endpoint)
  and `SUBSTRATE_SANDBOX_EGRESS_ALLOW` a function body's (FN-04). The OAuth
  cases also need the facility on, which is `SUBSTRATE_OAUTH_CALLBACK_URL`;
  they skip with that name when `oauth/start` says it is off.
- `dsn`: the operator hat, so `SUBSTRATE_E2E_DSN`, `SUBSTRATE_E2E_CTL` and the
  credential key those commands read (`SUBSTRATE_E2E_CREDENTIAL_KEY`). A step
  that needs it and does not have it records SKIPPED in the report instead of
  asserting.
- `restart`: `SUBSTRATE_E2E_STOP` and `SUBSTRATE_E2E_START`, shell commands
  that stop and start the server under test. DUR-01 restarts the server, and
  OPR-03 stops it because `user reset` needs the writer lock the running
  server holds; both skip without the pair. Each checks that `/healthz` stops
  answering after the stop, so a hook that does nothing fails the case rather
  than passing it, and a case that fails while the server is down starts it
  again for the cases after it.
- `uv`: `uv` on PATH and a reachable package index, which BUN-07 needs because
  a provider's body declares its dependencies in a PEP 723 block and the
  runner resolves them with `uv sync --script` before the body runs. Neither
  is arrangeable from here, so the case SKIPs with the reason rather than
  reading an offline machine as a broken sandbox.

An id is `<AREA>-<NN>`, where the area names the surface the case drives, and
an id is permanent: a case that moves keeps it, so a six-month-old report
still names the same behavior. `registerCase` takes an order too, and each
group file owns one hundred-block of that space (`extra_test.go` lists the
blocks), so files never renumber each other.

[STORIES.md](STORIES.md) is the other half of the list: the story-level cases,
whole scenarios that compose many endpoint-level cases into one coherent
repository.
