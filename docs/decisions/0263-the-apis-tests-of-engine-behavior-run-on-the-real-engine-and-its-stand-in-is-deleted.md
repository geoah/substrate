---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0263. The API's tests of engine behavior run on the real engine, and its stand-in is deleted

## Context and Problem Statement

`internal/api` was tested against `internal/api/fake_test.go`, a
hand-written copy of every method of `substrate.Service` and
`substrate.Dataset`. The copy did not do what the tests claimed of it: its
`Put` ignored `IfVersion` and it never built a validation error, so "the
engine returns sentinel X" and "the API maps sentinel X to status Y" could
both pass while the engine answered a different sentinel
(`internal/testenv/conformance_db_test.go`). The 1.0 plan (pull request
1313, row 49, step 4) asks for the API to be tested against the real engine.
The house rule said `internal/api` never imports the engine, test files
included.

## Considered Options

- Keep the fake, and hold it to the engine with conformance cases in
  `internal/testenv`.
- Move the API's suites to `internal/testenv`, which already starts a whole
  substrate over a socket.
- Keep the suites in `internal/api`, the ones that claim engine behavior in
  `*_db_test.go` files on the real engine, and the ones about the handler
  alone on a stub that implements almost nothing.

## Decision Outcome

Chosen: the suites stay in `internal/api`. A test whose assertion depends on
what the engine does with a request (what a write stores or refuses, a
guard, validation, paging, the changelog, watch, search, a token's reach)
runs the handler on the real engine, in a `*_db_test.go` file, on a copy of a
migrated template database (`engine_db_test.go`). A test about the handler
alone (routing, decoding, headers, the envelope for a given sentinel, the
feature table, the auth middleware) runs in the short suite on
`stub_test.go`'s stub: a service that accepts one bearer and a dataset that
answers who it is, with every other method a nil interface that panics. A
fault the engine cannot be made to produce is a small type in the test that
needs it, wrapping one method. The fake is deleted. Only the `*_db_test.go`
files of `internal/api` may import the engine.

Conformance cases over a copy keep two implementations of 72 methods and
test that they agree, which is the work the copy already failed at. Moving
the suites to `internal/testenv` would rewrite every test against a socket
and lose the package's own helpers and its unexported configuration, for no
gain in what is proven: the handler and the engine are the same code either
way.

### Consequences

- Good, because what an API test asserts about a write, a read or a refusal
  is what the shipped engine does.
- Good, because a method added to `substrate.Dataset` no longer needs a
  second implementation in a test file.
- Good, because a handler test cannot lean on an answer it did not write:
  the stub panics where the fake answered.
- Bad, because the API's engine tests need Postgres, and those that run a
  function body need Linux; they are no longer part of the short suite or
  of `test:race`.
- Bad, because a test author has to decide which half a test belongs to.
- Bad, because the rule that kept the engine out of `internal/api` now has
  an exception for its database test files.

### Confirmation

The depguard rule `api-never-imports-engine` (`.golangci.yml`) refuses the
engine in every file of `internal/api` but its `*_db_test.go` files. The CI
jobs `db api 1/N` run those files, cut by `.mise/dbshard.sh`, and `go gate`
requires them. Nothing refuses a second hand-written copy of the interface:
that is held by review only.

## More Information

[docs/testing.md](../testing.md) says which half a test belongs in and what
the fixture costs. Records 0161 and 0219 name the fake as the place their
contracts were held; those tests now run on the engine.
