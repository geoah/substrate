# Review brief: geoah/substrate PR #1100, Substrate Workouts

> **Where this came from.** Part of the review of geoah/substrate#1100 on 2026-11-01, at
> head `f9eeb332`, kept verbatim ([the index](../../README.md#reviews)). Every major it raised was
> fixed before the merge; [fixes.md](fixes.md) goes finding by finding. Line numbers
> refer to `f9eeb332`, not to `main`.

You are reviewing, not fixing. Do not edit files, commit, push, or comment on GitHub.

## What to review

The checkout is `/Users/geoah/src/github.com/geoah/substrate-wt-workouts`, branch `feat/apple-workouts`, head
`f9eeb332`. Its base is `b001925b` (origin/main). See the PR with `gh pr view 1100 --repo geoah/substrate`, and
the diff with `git -C <checkout> diff b001925b..HEAD`.

The PR does three things:

1. **Server: a new scope verb, `declare:~/<package>`** (commit a30e6764). It lets an app token apply only that one
   package's package, bundle and kind documents, and read its version, in the repository's own `~` authority. Files:
   `internal/scope/scope.go`, `internal/api/scoped.go`, `internal/api/authorize.go`, `internal/api/api.go`, the tests,
   `web/console/src/lib/app-scopes.ts`, `docs/apps.md`, and decision record
   `docs/decisions/*-keeps-its-own-package-under-a-declare-scope.md` (0203).
2. **The app's data model**: `apps/apple/SubstrateWorkouts/Package/*.yaml` (training package: exercise, routine,
   session, set, setup), installed by the app under `~/training`. Its long rationale is
   `apps/apple/SubstrateWorkouts/Package/README.md`.
3. **The app**: `apps/apple/SubstrateWorkouts` (SwiftUI, iOS 27, the Rows session layout),
   `SubstrateWorkoutsWidgets` (Live Activity), and `apps/apple/WorkoutsKit`. WorkoutsKit has two modules:
   `WorkoutsCore` (ported unchanged in substance from an earlier app, Spotter, 356 tests) and `WorkoutsSync`, which is
   new: `TrainingPackage.swift` (install, upgrade, newer-version handling), `RemoteCatalog.swift` (server routines
   and exercises, cached offline), `Outbox.swift` and `OutboxSink.swift` (a persistent outbox that writes a
   finished session and its sets), `Rehome.swift`, `WorkoutsAPI.swift` and `WorkoutsManifest.swift` (the OAuth
   app manifest). The app's coordinator is in `SubstrateWorkouts/Sources` (search for `SubstrateLink`).

Substrate's conventions are in the checkout's `AGENTS.md` and `docs/`. Read `docs/apps.md` for the app
authorization model and `docs/data-model.md` for kinds, packages, references and revisions.

## Where the risk is

Spend effort in this order. Points 1-3 matter far more than style:

1. **Security of the `declare:` verb.** Can a token with `declare:~/training` write or alter anything outside that
   package? Cases to check: other packages, other authorities, functions, triggers, agents, mappings, records, a
   narrowing or destructive change, a bundle that pulls in other packages, a kind document named into another
   package, path tricks in ids, and `requires`. Does the approval page tell the owner truthfully what they grant?
   Does it fit the existing scope grammar and the approval flow (#1084, #1088, #1099), or does it open a hole?
2. **Data safety in sync.** Check that:
   - the outbox survives the app being killed mid-flush, with no lost or duplicated sessions and sets;
   - the order is right: the session before its sets, chain heads before members;
   - `ifVersion` and conflict retries behave correctly;
   - deleting a session locally versus on the server works;
   - signing out with items still queued, or switching servers, is handled;
   - when the server holds a newer package version, the app stops writing and nothing is lost;
   - the app's own package version check cannot overwrite an owner's edits.
3. **Correctness of the auth integration**: sign-in, token storage, refresh, revocation, and the state the
   app shows in each case, compared with how SubstrateChat and SubstrateSync do it.
4. **The data model as an app-owned package**: does `~/training` under a per-repository authority work for kind
   references, `requires: substrate.reamde.dev/health`, and the agent writing routines? Note anything that will hurt
   later, such as kind names that differ per repository or migrations.
5. **Everything else**: the port (renames, leftovers from Spotter), the docs, the tests (what is untested that
   should be), the build and release wiring in `project.yml` and the mise tasks.

Verify claims by reading code and running tests where it helps (`go test ./internal/scope/ ./internal/api/`, `cd
apps/apple/WorkoutsKit && swift test`). A private server built from this branch may still be running on
127.0.0.1:18087 with Postgres on 55439. You may read it, but never touch port 8081, the container
`substrate-dev-db`, or any production host.

## Report

Write your report to `REPORT_PATH`. Number each finding and give for each:

- severity: blocker, major, minor or nit;
- `file:line`;
- what is wrong;
- a concrete failure scenario;
- the fix you suggest.

Put blockers and majors first. Then list what you checked and found sound, so the fixer knows what not to
re-check. Keep it evidence-based: no finding without a code reference.
