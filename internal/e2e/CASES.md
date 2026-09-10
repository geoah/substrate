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
codes over a real socket, so these cases are what only a live server, a live
database and a real client show.

Three preconditions gate individual cases and steps, and the `test:e2e` task
sets what it can:

- `totp`: the enforced door, `mise run dev:totp`, which every case that
  proves a live code needs. ISO-01's second registration skips against that
  door instead, because a second user there needs an enrolled seed of its own.
- `egress`: `SUBSTRATE_EGRESS_ALLOW` pointed at loopback on the server, which
  is what lets a function or an agent reach the test's own stubs.
- `dsn`: the operator hat, so `SUBSTRATE_E2E_DSN`, `SUBSTRATE_E2E_CTL` and the
  credential key those commands read. A step that needs it and does not have
  it records SKIPPED in the report instead of asserting.

An id is `<AREA>-<NN>`, where the area names the surface the case drives, and
an id is permanent: a case that moves keeps it, so a six-month-old report
still names the same behavior. `registerCase` takes an order too, and each
group file owns one hundred-block of that space (`extra_test.go` lists the
blocks), so files never renumber each other.
[Issue 520](https://github.com/geoah/substrate/issues/520) holds the ids that
have no case yet.

[STORIES.md](STORIES.md) is the other half of the list: the story-level cases,
whole scenarios that compose many endpoint-level cases into one coherent
repository.
