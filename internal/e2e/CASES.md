# The live end-to-end cases

`mise run test:e2e` drives a LIVE substrate over HTTP exactly as a user's
client would: it registers a fresh user and leaves that user's repository in
place, so a human can open the console (or `substratectl`) and look at what the
run built. Every run writes a markdown report under `.dev/e2e/` naming each
case, what it tests, the steps it took and what came back.

The suite mocks the world, never the substrate: a fake LLM answers the OpenAI
wire and a fake OAuth provider answers the token endpoint, but every substrate
call crosses the real HTTP door of the real binary against the real database.

**The implemented cases are the code.** Each one is registered with its id, its
title and the sentence saying what it tests: `runCase` in `cases_test.go` for
the slice and the stories, `registerCase` in the `extra_*_test.go` files for
the rest, and the run's own report renders all of them. The story-level cases
have their ground rules in [STORIES.md](STORIES.md).

A case a unit suite already pins is not registered here. `internal/api` drives
every route against a hand-written fake, `internal/engine` drives the same
writes against a real Postgres, and `internal/testenv` drives the published
error codes over a real socket against the real engine, so this suite holds
what only a live server, a live database and a real client show.

## The backlog

The cases that are not written yet. `needs` says what a case wants beyond a
plain run: `totp` runs only against the enforced door (`mise run dev:totp`),
`egress` needs `SUBSTRATE_EGRESS_ALLOW` pointed at loopback on the server, and
`dsn` needs the operator hat (`SUBSTRATE_E2E_DSN` and a built `substratectl`).

| id | case | needs |
| --- | --- | --- |
| AUTH-05 | full TOTP registration: enroll a seed, register with a live code, the code that registered cannot also log in | totp |
| AUTH-06 | password change with both factors; the old password stops working; bearer on `/password` is refused 403 | |
| AUTH-07 | TOTP swap: enroll a candidate seed, prove it, the old seed stops working | totp |
| FN-04 | a function's own egress is governed by the sandbox allowlist | egress |
| AGN-03 | a tool round-trip: the stub asks for a tool, the agent runs it, the second turn completes | egress |
| OAU-01 | `oauth/start` answers the consent URL from the account's config; the callback with the HMAC state lands the tokens | egress |
| OAU-02 | a tampered `state` is refused | egress |
| OAU-03 | the refresh grant renews an expiring token on the sweep | egress |
| ERR-02 | the wrong-shape routes answer 405 naming the working spelling (PUT at the collection, POST at an id) | |
| ERR-03 | an unknown collection is a 404 naming it; the body cap is a 413 | |
| ERR-04 | every published error code is reachable over the live door (the conformance suite pins it in-process) | |
| DUR-01 | a server restart loses nothing: the records, the changelog head in the table and the head in the segment files are identical before and after | dsn |
| OPR-02 | `repository list` names the run's repository | dsn |
| OPR-03 | `user reset` re-keys the credential and old tokens die | dsn |
| EMB-01 | an `embed: true` property queues on write; the fake provider's embedding lands | egress |

## Recurrence, and what the calendar cases assume

The substrate stores a recurrence rule and never expands it into rows
(decision 0039). Occurrences reach a reader two ways: a connector explodes
them as `calendarevent` rows pointing at their `calendareventseries`, and
`GET /occurrences` computes the rest from the stored rules at read time
(decision 0043), staying silent inside a series' stamped
[`materializedFrom`, `materializedUntil`) span, where the rows are the truth.
The CAL and OCC cases play that connector exactly the way Google Calendar
behaves, make the range queries a calendar client would, and read the computed
half beside them.
