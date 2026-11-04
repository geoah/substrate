# Review: geoah/substrate PR #1100, Substrate Workouts

> **Where this came from.** Part of the review of geoah/substrate#1100 on 2026-11-01, at
> head `f9eeb332`, kept verbatim ([the index](../../README.md#reviews)). Every major it raised was
> fixed before the merge; [fixes.md](fixes.md) goes finding by finding. Line numbers
> refer to `f9eeb332`, not to `main`.

Reviewer: Fable. Checkout a worktree of this repository, head `f9eeb332`, base `b001925b`. Review only; nothing was edited, committed or posted.

What ran:

- `go test ./internal/scope/ ./internal/api/`: pass.
- `cd apps/apple/WorkoutsKit && swift test`: 356 tests pass; `--filter WorkoutsSyncTests` runs 23 tests in 6 suites (Sign-in, Outbox, Codec, Rehome, The package), all pass; the Live server suite is skipped without `WORKOUTS_E2E_SERVER`, as designed.

No blocker. Five majors, the first three in sync data safety and the package model, the fourth and fifth in the `declare:` admission. Findings are numbered; blockers and majors first, then minors and nits, then what was checked and found sound.

---

## Major

### 1. major — The outbox flush removes entries by index across `await`s, so a concurrent `drop`, `reset` or `enqueue` corrupts or crashes it

`apps/apple/WorkoutsKit/Sources/WorkoutsSync/Outbox.swift:193-216` (the `flush` loop), `:176-187` (`drop`, `reset`).

`Outbox` is an actor. `flush` keeps `index` into `file.entries`, awaits `send(entry, api)` (a network round trip), then does `file.entries.remove(at: index)`. Actors are reentrant at `await`, so `drop(session:)`, `reset()` and `enqueue` run between the send and the removal and mutate the same array.

Failure scenarios:

- Session B's writes sit at the head of the queue (held from an earlier refusal). The user finishes session A; `flush` is awaiting A's session `PUT` at index 5. Meanwhile the user deletes B from History: `SubstrateLink.deleted` calls `outbox.drop(session: B)`, removing B's five entries. A's `PUT` returns; `remove(at: 5)` now removes A's fifth set, not A's `PUT`. The set is gone from the queue and never reaches the server; the `PUT` is re-sent next run (harmless). One set of a logged session is silently lost.
- `reset()` during a flush (it is public and intended for sign-out) empties `file.entries`; the in-flight send completes and `remove(at: index)` traps on an empty array. `reset()` is unused today, which is the only reason this does not crash now.
- `drop` in the middle of the `.hold` branch (`file.entries[index].attempts += 1`) indexes a shifted array and marks the wrong entry.

Fix: remove and update entries by identity, never by position: `file.entries.removeAll { $0.id == entry.id }`, and look the entry up by id before `attempts += 1`. Iterate over a snapshot of ids taken at the start, re-reading each entry from `file.entries` before sending (skip if gone). Have `reset()` bump a generation counter the loop checks after each `await` and stops on. Add a test that calls `drop` while a send is suspended on a scripted server.

### 2. major — A session the owner deletes on the server is recreated by the phone at the next sync

`apps/apple/SubstrateWorkouts/Sources/App/SubstrateLink.swift:221-237` (`exchangeHistory`), `apps/apple/SubstrateWorkouts/Sources/App/AppModel.swift:230-250` (`importServerSessions`).

`exchangeHistory` queues every entry of the phone's history that is neither on the server's finished list nor already queued. Nothing records that a session was once acknowledged by the server. The server's deletions are therefore read as "the server lacks it" and undone.

Failure scenario: the owner deletes a wrongly logged session in the console (soft delete or purge). The phone still holds it in `history`. At the next launch `ServerHistory.sessions()` does not return it, `queued` does not contain it, so `OutboxPlan.finished` is enqueued and the session and all its sets are `PUT` again. The owner deletes it again; it comes back again, forever. The same happens to a session imported from the server earlier (`importServerSessions`) and then deleted there: it is now in the phone's history and gets re-exported. Nothing in the brief's "deleting a session on the server" path works.

Fix: keep an acknowledgement on the history entry (for example `HistoryLocal.sentTo: [repositoryKey: version]` set when the create lands, and for imported sessions at import) and only queue sessions never acknowledged by this repository. Separately, when a session the phone has marked as sent is absent from the server, treat it as the owner's deletion: remove it locally or mark it, never re-send. A test for "deleted on the server stays deleted" belongs beside `discard`.

### 3. major — A `declare:` apply with a bundle document replaces the package wholesale, so an owner's additions that hold no data yet are dropped without any confirmation, and the app's version check cannot see the owner's kind edits

Server: `internal/engine/vocabularywrite.go:886-894` (a bundle document makes the batch a whole-package replacement), `:963-965` ("a replacement starts from the incoming set alone"), `internal/engine/convert.go:76-78` ("a step touching no live record is not a step", so only live values make a plan lossy), `internal/engine/vocabularywrite.go:1147-1153` (`needsBump` skips kind documents: a changed kind moves its own version, not the package's). App: `apps/apple/WorkoutsKit/Sources/WorkoutsSync/TrainingPackage.swift:108-116` compares only the package row's `version`. Package: `apps/apple/SubstrateWorkouts/Package/training.yaml` ships a bundle document, so every app apply is a replacement.

The decision and docs promise more than the engine delivers: decision 0203 line 59-61 ("an upgrade may only widen; a narrowing waits for the owner") and `docs/apps.md:325` ("an app widens its package and never narrows it"). The lossy confirmation only protects values live records hold. A kind or property with zero rows is pruned freely.

Failure scenarios:

- The owner adds an optional `notes` property to `~/training/exercise` in the console, or adds a kind `~/training/bodyweight`. No records use it yet. Editing a kind document does not move the package row's version (`needsBump` excludes `DocKind`), so the app still reads `.current`. The next app release ships `version: 2`; the app sees `.behind`, applies its bundle, and the owner's property and kind are deleted. No confirmation, no changelog warning beyond the apply itself. Decision 0203's "Bad" bullet at line 89 says the owner's edit "moves its version past the app's", which is not what the engine does for kind edits.
- If the owner's added kind does hold rows, the dropped-kind guard refuses the batch. `PackageInstaller.ensure()` throws, `note()` records the server's words, `package` stays nil and `syncStatus` is `.failed`. The app never writes again (flush is gated on `package?.writable`) until the owner removes their kind or hand-edits the package. Nothing is lost, but the app is wedged with a message about a guard the owner cannot act on from the phone.

Fix, one of:

- Server: under a `declare:` grant, refuse a batch whose candidate drops any declaration the stored package holds (kind, property, enum value, state), not just one with live rows. That makes "never narrows" true. It is a few lines in `scopedDataset.ApplyVocabularyDocuments` or an engine flag the scoped surface sets.
- App: before applying, `GET` the package's kind rows and refuse to apply if any stored kind or property is absent from the shipped manifests, surfacing "your package has changes this app does not know" instead of overwriting.
- Either way, correct decision 0203 line 89 and `docs/apps.md:325`, and either drop the bundle document from the shipped batch (an apply without a bundle merges rather than replaces, `vocabularywrite.go:961-970`) or document why the replacement is wanted.

### 4. major — The `declare:` admission lets an app ship a bundle document carrying inline Python modules and an OAuth provider block

`internal/api/scoped.go:975-997` (`declarable` admits any `bundle` document of the package, checking only its id), `internal/vocabulary/bundle.go:258` (`bundleDataKeys` includes `modules`, `oauth2`, `inputs`), `:176-186` (`Modules` are inline source; `oauth2` is a provider flow's endpoints), `internal/engine/runner.go:130-131` (modules are loaded into every function of that package).

The decision's security argument (0203 line 52-56: "no code can be declared") holds for functions, triggers and agents, but a bundle document is code-adjacent: `modules:` is stored Python source and `oauth2:` turns the package into an OAuth provider whose `authorizationEndpoint` and `tokenEndpoint` the app chooses.

Failure scenario: today the modules are inert because the app cannot declare a function to import them. But the owner later adds a function to `~/training` in the console (the natural place, it is "their" package); it runs with the app-supplied modules on `sys.path`. Separately, a bundle with an `oauth2` block and a kind binding the `oauth2` trait makes `~/training` a connectable provider. On a later re-authorization the app asks for an `account` detail of its own package; the approval page describes connecting an account of "your Training package" and the owner's browser is sent to the app-chosen authorization endpoint. Neither is a break-in today; both are surface the scope's promise says it does not have.

Fix: in `declarable`, for a `bundle` document allow only `authority`, `package`, `description`, `requires`, `requiresAtLeast` and `installs` in `data`, refusing `modules`, `oauth2` and `inputs` with a 403 naming the key. Add the cases to `TestAnAppDeclaresOnlyThePackageItsScopeNames`. Say in 0203 and `docs/apps.md` that a declared bundle is a closure listing only.

### 5. major — A write the server refuses with a plain 4xx is held forever, holds its whole session, and the user has no way out

`apps/apple/WorkoutsKit/Sources/WorkoutsSync/Outbox.swift:272-283` (`classify`: anything 4xx that is not a 409, 429 or the one 422 text is `.hold`), `apps/apple/SubstrateWorkouts/Sources/Views/Settings/SubstrateSection.swift` (no action for `.refused` beyond "Send Now"), `apps/apple/SubstrateWorkouts/Sources/Views/Routines/RoutinesView.swift:38-50` (the banner shows `.refused` with no action).

Failure scenarios:

- The owner purges a session on the server while its Finish `PATCH` or Health-link `PATCH` is still queued (phone was offline). `knownVersion` reads nil, the `PATCH` goes without `ifVersion`, the server answers 404, `classify` holds it. Every later flush retries, fails, and reports "Your substrate refused a change: not found" with no button to discard it.
- An exercise added on the phone is refused by validation (a name over `max`, a pattern). Its entry has `session: nil`, so it does not hold anything, but every set naming it fails `mustExist` and holds that session. Each session using the exercise is stuck, forever, across launches.

Fix: map a 404 on a `PATCH` to `.drop` (the record the phone is finishing no longer exists, so the local copy is the owner's problem, not the queue's) and a 404 on `GET` inside `knownVersion` to the same. Surface a per-entry or per-session "Discard waiting changes" action in Settings when `state` is `.refused`, with the server's words. Consider `ServerError.isPermanent` (`SubstrateKit/API/Errors.swift:36`) which already exists for exactly this classification.

---

## Minor

### 6. minor — The app's status check is refreshed but never read, so the kit's issues (access ending, approval changed, server too old) are invisible

`apps/apple/SubstrateWorkouts/Sources/App/SubstrateLink.swift:43` (`status` created), `apps/apple/SubstrateWorkouts/Sources/App/SubstrateWorkoutsApp.swift:58` (`status.refresh()`), and no reader of `status.status?.issues` anywhere in `SubstrateWorkouts/Sources`. Compare `apps/apple/SubstrateChat/Sources/App/AppModel.swift:186-190`, which shows `issues`, and `apps/apple/SubstrateSync/Sources/App/SubstrateSyncApp.swift:35,115`, which shows `SessionExpiredView`.

Scenario: the token expires in two days; `AppStatusCheck` reports `.accessEndsSoon`; the app says "Up to date". The owner changes the approval on the server (removes `declare:`); the check reports `.needsApproval(changes:)`; the app still says ready until a write fails. Revocation itself is caught at the next sync (401 on the package read), so sign-out state is correct, only late.

Fix: derive `SyncStatus` from `status.status?.issues` too (rank `accessEnded` above everything, then `needsApproval`, `accessEndsSoon`), as Chat does.

### 7. minor — "Sign In Again" after an expired token signs in without replacing the old session

`apps/apple/SubstrateWorkouts/Sources/Views/Settings/SubstrateSection.swift:42`, `apps/apple/SubstrateWorkouts/Sources/Views/Routines/RoutinesView.swift:49` open the connect sheet, which calls `signIn(probe)` → `adopt(session)` (`SubstrateLink.swift:77,101`) with `replacing: nil`, while `reauthorize()` passes `replacing: old`. The old session is left in the Keychain and not queued for revocation; `unrevoked` will not list it either. It is also possible to sign in to a different server from that button, which leaves the first repository's outbox queued without any mention. Fix: route both buttons through a path that passes the old session as `replacing:` and, when the chosen server differs, say so.

### 8. minor — List reads truncate silently

`apps/apple/WorkoutsKit/Sources/WorkoutsSync/WorkoutsAPI.swift:33-43` stops paging when `out.count >= limit` (default 5000, sets 20000 in `OutboxSink.swift` `ServerHistory`). A repository with more sets than that gets a history whose last sessions have no sets, and `exchangeHistory` then sees those sessions as "held" and does nothing; the phone's copy of a session with sets could be considered complete while the server's is being read truncated. Fix: either page until the cursor ends (the list is filterable by session) or fetch sets per session batch, and log when the limit is hit.

### 9. minor — A Finish `PATCH` dropped under the revision rule leaves the session unfinished on the server and the phone re-queues it at every sync

`Outbox.swift:280` drops a 422 "below the stored revision"; `SubstrateLink.swift:233` re-queues any history entry the server does not list as finished. Scenario: the console edits the session (revision 5) before the phone's Finish (revision 2) arrives; Finish is dropped; the server row is still `status` default; the phone's history says finished; every sync re-enqueues create, sets and Finish, each dropped again. No data loss, but a permanent background loop and misleading "Sending N changes". Fix: after a drop, read the record and reconcile (take the server's revision and resend Finish on top, or mark the local entry as "server-owned").

### 10. minor — An unreadable `outbox.json` becomes an empty queue, and a failed save is swallowed

`Outbox.swift:163` (`try? decode ... ?? File()`), `:299` (`save()` swallows the error). `WorkoutsFiles.setAside` exists for exactly this case and is used by the journal; the outbox does not use it. Scenario: a future format change or a truncated file after a failed atomic rename on a full disk; every queued session is silently forgotten. Fix: set the unreadable file aside and surface it in `OutboxState`; log save failures and raise them into `SyncStatus`.

### 11. minor — `embed:` and `indices:` under a `declare:` grant spend the owner's resources

`internal/vocabulary/load.go:1803` (`embed` is a property key), `:807-809` (`indices` is a kind key), `internal/engine/vocabularywrite.go:541-550` (indexes are built on the admin pool at apply). A declared `embed: true` on a text property enqueues embeddings for every record the app writes, against the owner's embedding provider; declared indexes build Postgres indexes. The owner approving "install and update the kinds of your Training package" is not told. Low risk, worth a line in the sentence or a refusal of `embed` under the scope, since `scope.go:85-86` lists "spends the owner's LLM budget" as a reason packages are unwritable.

### 12. minor — The approval row for `declare:` and for `write:~/training/*` before install says "Nothing yet: no kind in your repository matches"

`web/console/src/components/authorize/scope-row.tsx:21-24,160`. The declare sentence is right (`app-scopes.ts:214-216`), but the "Covers today" block under it lists no kinds and the write row for a package that does not exist yet shows the same empty list with "Also covers matching kinds you add later". The owner approves writing a package they are told is empty. Fix: for a `declare` verb skip `KindList`, and for a write glob over a package a `declare` scope in the same request names, say "the kinds the app will install".

---

## Nit

### 13. nit — `scopes_supported` carries a template, not a scope

`internal/api/authorize.go:131`. Acknowledged in 0203. A strict RFC 8414 client comparing requested scopes against the list will not match `declare:~/training`. Consider `declare:~/*` as the advertised form with the doc saying one package only, or omit it.

### 14. nit — `OutboxSink` is used only by tests; the app has its own `LinkSink`

`apps/apple/WorkoutsKit/Sources/WorkoutsSync/OutboxSink.swift:9` versus `SubstrateLink.swift:304-319`. The two implement the same seam with slightly different gating (`LinkSink` drops events when not signed in; `OutboxSink` always queues). Keep one, or make `LinkSink` wrap `OutboxSink` so the tested code is the shipped code.

### 15. nit — Spotter leftovers in docs

`apps/apple/WorkoutsKit/API.md:24` (`adapter: "spotter@<version>"`), `apps/apple/SubstrateWorkouts/Package/README.md:288,1254` (`spotter@0.1.0` as the example adapter) while the app writes `substrate-workouts@…` (`SessionController.swift:140-141`) and `HistoryStore.swift:179` migrates on that prefix. The Swift sources are clean (zero hits).

### 16. nit — `Rehome.swap` rewrites any string value, including free text

`apps/apple/WorkoutsKit/Sources/WorkoutsSync/Rehome.swift` swaps every string equal to or prefixed by `~/training` or `<authority>/training`, in notes and names too. Harmless in practice; a per-key approach (kind, reference values, document `authority`/ids) would be exact.

### 17. nit — `copyStarter` writes directly, outside the outbox

`apps/apple/WorkoutsKit/Sources/WorkoutsSync/RemoteCatalog.swift:74-88`. A failure halfway leaves half the starter set; a retry skips what is present, so it recovers, but a refusal is only logged via `note`.

---

## Checked and found sound

Server, the `declare:` verb:

- Grammar (`internal/scope/scope.go:217-231, 272-279, 374-380`): a request admits only `declare:~/<package>`; `declare:*`, `~/*`, a kind, a bare word and a foreign authority are refused before the page. `Resolve` stores the authority form, `CheckStored` refuses `~`. `DeclaresIn` requires the package's authority to equal the repository's, so a stored `declare:other.example.com/x` (if one were ever minted) is dead at use. `Covers` does not let `write:*` or any glob cover a `declare`, and a `declare` covers only itself. `Reads`/`Writes` ignore the verb. Tests cover all of this.
- Admission (`internal/api/scoped.go:955-997`): default-deny on document kind (functions, triggers, agents, mappings, traits, property types, authorities, actors all fall to `default`); a kind's package is taken from `metadata.id`, and the loader independently requires `metadata.id` to equal `data.authority/data.package` for kinds (`load.go:672, 857-859`) and the package identity for bundles (`bundle.go:322-324`), so `data.*` cannot point elsewhere; `a/b/c/d` ids fail `ValidCamel`; `..` and `%` are outside `reID`. A batch mixing its package with another is refused whole. `confirm` and `origin` route to `ApplyVocabularyDocumentsWith`, which the scoped dataset refuses (`scoped.go:1008`), and `/vocabulary/plan` is refused; both tested. The engine's `authorizeDeclarationWrite` and `authorizeNewPackage` (`engine/seed.go:67-118`) still run under the app actor, so `builtin`, `published` and `substrate.reamde.dev/*` are closed.
- `requires` is a precondition checked at load (`bundle.go:720-745`), never an install, so a bundle cannot pull another package in. `movedFrom` on the API door stores the key and refuses any implied move (`engine/move.go:112-120` `userDoorMoveGuards`), so an app cannot move another package's rows into its own. `holdWaitingMappings` only ever removes mapping documents, which `declarable` would refuse anyway.
- Reads: the scoped `KindByRef` resolves the core `package` and `bundle` kinds only when a declare scope is held, and `Get` admits only the row whose id `DeclaresIn` names (`scoped.go:387-392, 425-431`); `List` of package rows stays gated. `NamesPackage` admits a declare scope to the set-up check for its own package only.
- Approval page: `scopeKinds` returns an empty list for a declare scope, `scopeInternal` does not panic on a package id, `Allowed` works for a renewal; `declareSentence` and the fixture are in place; `ScopeVerb` and `ACTIONS` include `declare`. The only verb switches in the console are `agents`/`decide` (`scope-row.tsx`), so a declare row renders through the kinds branch (see finding 12).

App, sync data safety:

- Write order: session `PUT` without `status`, sets heads-first (`headsFirst` handles cycles by sending the rest), then Finish and Health `PATCH`es under `ifVersion` with monotonic `revision`s; the package README's equal-revision rule makes a replay of any one entry after a kill-between-commit-and-save idempotent. Each removal is saved before the next send, so at most one entry replays.
- 409 handling: the version is forgotten, re-read, and the patch re-sent once on top; a second 409 holds and retries next run. `purge` accepts 404 and retries without `ifVersion` on 409. A refusal holds only its own session; other sessions flow.
- Offline, 429 and 5xx stop the run with nothing removed; 401 → `.signedOut`, 403 `insufficient_scope` → `.needsApproval`, both surfaced with actions. `TransportError` with no status is offline.
- Discard of an in-progress session queues nothing unless the server already holds the session (`versions` key), in which case a purge follows; delete from History drops and purges; tested.
- `SubstrateClient` retries only idempotent methods (`SubstrateClient.swift:156`: GET, HEAD, PUT, DELETE), so a `PATCH` is never duplicated by the transport.
- Package versioning: `.missing`/`.behind` apply, `.current` no-op, `.newer` stops writes, catalog refresh and the exchange (`runSync:195`), and `flush` is gated on `writable`, so nothing is written under a newer package and the queue is kept. A 403 at apply reads as `.needsApproval`. The decision's claim that a changed declaration lands at the next version is what `resolveDeclarationVersions` does; an explicit higher version in the shipped package document is honored (`vocabularywrite.go:1177-1178`), so shipping `version: 2` works as the README says.
- Sign-out keeps the per-server outbox directory (`directory(for:)` keys on host, port and repository), so a later sign-in to the same repository flushes it and the exchange's `queued` check prevents double-queuing; a different repository gets its own directory. `SessionStore.signOut` revokes before forgetting; a server that cannot be reached leaves the session and shows `signOutProblem`.
- Rehome: `authority: "~"` and every `~/training` id and reference are swapped outbound; the `substrate.reamde.dev/health/workout` reference is untouched. Tested.
- Files: `WorkoutsFiles.protectedWriting` is `.atomic` plus until-first-unlock protection, and the journal and history stores set unreadable files aside.

Package and data model:

- `set.session`, `set.exercise`, `set.setup`, `set.continues` are `mustExist`; `session.routine` deliberately is not; `session.workout` points into `health`, which the approval's `package` detail installs first. Kind names differ per repository, acknowledged in 0203; the app rehomes at the edge; an agent writes routines under the repository's own name, which is the console's name too.
- `workouts-package.py --check` is wired into `apple:test:workouts` (`.mise/apple.sh:587`), so `training.json` and `kinds.json` cannot drift from the YAML; the codec drift test reads `kinds.json`.

Build and release:

- `apps/apple/project.yml`: the app target excludes `Package/**`, `*.md`, `docs/**`; the widget target links no WorkoutsKit and shares `Sources/Shared`; `SUBSTRATE_WORKOUTS_CLIENT_ID` defaults to empty with the loopback id as in Sync and Chat. `build:workouts`, `test:workouts`, `release:workouts` exist in `.mise/apple.sh` and `apps/apple/README.md:571-590` documents them.

Tests, what is covered and what is not:

- Covered: plan order, flush order and `ifVersion`, 409 retry, 422 drop, offline persistence across relaunch, per-session hold, 401 stop, discard and delete, exercise-first, install/upgrade/newer, needs-approval, manifest contents, too-old server, status ranking; server-side grammar and admission.
- Not covered and should be: concurrent `drop`/`reset` during a flush (finding 1); a session deleted on the server staying deleted (finding 2); a `declare:` apply over a package the owner extended (finding 3); a bundle with `modules`/`oauth2` under the scope (finding 4); a 404 on a queued `PATCH` (finding 5); list pagination past the limit (finding 8); the `.failed` state when the apply is guard-refused.
