# Review: Substrate Workouts, PR #1100

> **Where this came from.** Part of the review of geoah/substrate#1100 on 2026-11-01, at
> head `f9eeb332`, kept verbatim ([the index](../../README.md#reviews)). Every major it raised was
> fixed before the merge; [fixes.md](fixes.md) goes finding by finding. Line numbers
> refer to `f9eeb332`, not to `main`.

Reviewed head `f9eeb3327c177a5a25e643af7bcdabc798a2d544` against `b001925b` in `/Users/geoah/src/github.com/geoah/substrate-wt-workouts`. Findings below are code-path findings; no production writes or GitHub comments were made.

## Findings

1. **Major — deleting during a flush can remove an unrelated write or crash.**
   **Location:** `apps/apple/WorkoutsKit/Sources/WorkoutsSync/Outbox.swift:207–219` (also `drop`, lines 176–179).
   `flush` retains an array index across `await send(...)`. Swift actors are reentrant at that suspension: `drop(session:)` can remove that entry and shift the array. On resumption, success/stale handling removes whatever now occupies that index; refusal handling similarly edits the wrong entry.
   **Scenario:** a slow session PUT is in flight; History deletion drops that session's queue and appends a purge. The PUT returns and the flush removes the purge without sending it, or removes the next session's write. If the array was emptied by a discard/reset, indexing traps. The `flushing` flag only excludes another flush, not these mutations.
   **Suggested fix:** identify acknowledgements and errors by immutable entry UUID after each suspension, and explicitly coordinate in-flight writes with cancellation/deletion so a late successful create is followed by the intended purge. Test deletion/reset while an HTTP response is suspended, not just before/after a flush.

2. **Major — history exchange resurrects deleted sessions in both directions.**
   **Location:** `apps/apple/SubstrateWorkouts/Sources/App/SubstrateLink.swift:224–234`; `apps/apple/SubstrateWorkouts/Sources/App/AppModel.swift:200–205,230–240`.
   Exchange imports all finished remote sessions before examining pending deletes, and treats every local session absent from the remote live list as a new upload. There is no durable acknowledgement/deletion ledger distinguishing a never-uploaded session from a previously synced one.
   **Scenario A:** delete a synced session while offline, leaving a queued purge. On reconnect, `runSync` imports the still-live server copy before flushing the purge. The purge then succeeds but the local copy remains. On the next sync, its absence remotely causes a new PUT/sets/Finish plan and restores the deleted session on the server.
   **Scenario B:** purge a previously synced session on the server. The phone still has its history file and queues it as missing on its next exchange, recreating it.
   **Suggested fix:** persist per-repository sync identity and tombstones, exclude pending deletes from import, and reconcile remote deletion separately from first upload. Merely moving flush ahead of exchange does not solve server-side deletion or the next-sync resurrection.

3. **Major — an old sync continues against mutable state after sign-out or account replacement.**
   **Location:** `apps/apple/SubstrateWorkouts/Sources/App/SubstrateLink.swift:185–201` (lifecycle at `begin`/`end`, lines 131–157).
   `runSync` captures the old API, but after suspension assigns shared `package`, reads the current `remote`/`outbox`, and finally invokes `flush()` using the current session's API. Neither `begin` nor `end` invalidates the task; `sync()` for a newly adopted session simply waits for the existing task.
   **Scenario:** server A's package check is slow. Sign out and sign into B while it is pending. A's result can mark B writable, populate B's catalogue cache using A's API, and reach B's outbox before B's package has been checked. Signing out alone can also leave a late catalogue refresh displaying A's data again.
   **Suggested fix:** bind each task to an immutable session generation and its API, catalogue, and outbox. Invalidate on account change and check generation after suspensions before publishing state or writing. Ensure the new account runs its own package check; cancellation alone must not be relied on for already-completed requests.

4. **Major — package version alone does not detect owner edits to kinds.**
   **Location:** `apps/apple/WorkoutsKit/Sources/WorkoutsSync/TrainingPackage.swift:94–114`; relevant engine behavior: `internal/engine/vocabularywrite.go:1147–1149,1221–1259`.
   The installer assumes an owner's schema edit moves the package version. The engine explicitly excludes kind documents from package `needsBump`; a kind edit advances that kind's own version instead. Therefore the package can still read v1 while its routine/session kind has been modified. A later shipped package v2 is treated as an ordinary upgrade and re-applies the bundled definitions.
   **Scenario:** the owner customizes a kind's labels/description at package v1. Its kind version advances but package v1 remains. A v2 app replaces those customizations with its bundled values; this is not a lossy record conversion and will not require confirmation. The same check also calls the package current while kind-level validation changes are already ahead of the app. Additionally, `ensure` has no atomic expected-version guard between its GET and apply, so an edit in that interval is not protected either.
   **Suggested fix:** compare the installed declaration closure against a known baseline (including individual kind versions/content), and condition upgrades on that baseline atomically at the server. Refuse or explicitly reconcile owner-modified declarations rather than treating the package integer as proof of ownership/content.

5. **Major — failed persistence is silently reported as a successfully queued write.**
   **Location:** `apps/apple/WorkoutsKit/Sources/WorkoutsSync/Outbox.swift:168–170,292–299` (load fallback at line 163).
   `enqueue` and deletion/reset mutations have no failure result because `save` swallows every filesystem/encoding error. Initial load also silently converts any unreadable existing outbox into an empty queue. This breaks the sink's promise that writes are durable before returning.
   **Scenario:** a user deletes an uploaded workout offline when storage is full. The local history file is removed, the purge is only in memory, and the app reports no storage failure. Killing the app loses that purge, and the next history exchange imports the remote workout again. An unreadable queue can similarly lose pending deletes or Health-link patches without warning.
   **Suggested fix:** propagate persistence errors, preserve/quarantine unreadable files, and make callers retain a durable pending operation until the outbox confirms its disk write. Do not advertise sent/queued success while only memory contains the operation. Test disk-write and decode failure paths.

6. **Major — server edits to existing history never reach the phone.**
   **Location:** `apps/apple/SubstrateWorkouts/Sources/App/AppModel.swift:230–240`.
   `importServerSessions` skips every ID already held locally, regardless of server record version/revision or changed sets. The fetched remote versions are therefore discarded for the overwhelmingly common case of a workout already on this phone.
   **Scenario:** the owner corrects load/reps in a session's sets using the server or another device. Every later sync fetches the correction but skips the session; History, PREVIOUS and suggestions continue using the old load indefinitely.
   **Suggested fix:** merge remote session/set revisions into existing history, retaining device-only Health metadata and protecting pending local writes. Add a test where an already-imported session changes remotely and the history index updates.

7. **Minor — starter copying bypasses the newer-package write stop.**
   **Location:** `apps/apple/SubstrateWorkouts/Sources/App/SubstrateLink.swift:244–250`; `apps/apple/SubstrateWorkouts/Sources/Views/Routines/RoutinesView.swift:84–93`.
   Unlike `flush`, `copyStarterRoutines` never checks `package.writable`. The button disables only while copying.
   **Scenario:** the cached server catalogue is empty and the next package check reports a newer version. The UI says to update the app, but “Add the Starter Routines” still sends exercise/routine PUTs using the old schema.
   **Suggested fix:** gate this entry point and its button on the same verified current-package state as all other writes, and preserve that account/package check across the operation.

## Checked and found sound

- `declare` request parsing admits only a concrete home-relative package; grants match an exact resolved package and require the repository authority. Other packages, authorities, globs and kind-shaped requests are rejected. The scoped apply allowlist excludes functions, triggers, agents, mappings, traits and data records.
- The engine checks declaration IDs against their declared package (`internal/vocabulary/load.go`, `bundle.go`); changing `data.authority`/`data.package` does not provide a second identity. Bundle `installs` is checked against the declared closure. `requires` checks installed vocabulary rather than automatically installing arbitrary packages through this door.
- Confirmed/origin-bearing vocabulary apply remains forbidden to scoped datasets; ordinary apply still goes through engine ownership and conversion admission. State-machine `applyDiff`/`applyMerge` transitions also explicitly reject ordinary scoped writes (`internal/engine/write.go:958–970`), so adding a state machine is not an obvious transitive-write escape.
- Approval wording describes keeping the named package's kinds and reserves changes that lose record values to the owner. No verified cross-package privilege escape was found in this review; this is not a proof of exhaustive security coverage.
- Outbox planning puts the session before sets and chain heads before members, omits create-time status, then finishes through a guarded PATCH. Fixed record IDs and the revision guard support normal replay; the existing 409 retry reads a fresh envelope version. Those paths are covered by unit tests, but concurrent queue mutation and disk failures are not.
- Account-specific outbox directories preserve queued entries on ordinary sign-out, and auth uses the existing SubstrateKit OAuth/PKCE, session storage and deferred revocation surfaces. App tokens intentionally have no refresh; renewal reauthorizes, consistent with `docs/apps.md`.
- Training references are rehomed at the API edge, while external health references stay external. The health package is requested during authorization before training installation. Per-repository training kind names are intentional and documented.

## Validation and scope

- Read PR metadata, the server/security diff, relevant engine declaration/write guards, app authorization/data-model docs, WorkoutsSync implementation/tests, and app coordinator/history integration.
- `mise exec -- go test ./internal/scope/ ./internal/api/` passed.
- `mise exec -- swift test` in `apps/apple/WorkoutsKit` exited 0; Core reported 356 tests passing. The live-server suite was skipped without its environment.
- No new tests were added, no simulator UI or release/archive verification was performed, and the full 64k-line port was not exhaustively reviewed. The findings prioritize new sync and declaration integration as requested.
- Git working tree remained clean after review/test execution. Only this requested report was written.

PR https://github.com/geoah/substrate/pull/1100, head f9eeb3327c177a5a25e643af7bcdabc798a2d544, checks and reviews: local Go and Swift suites passed; GitHub check/review status not assessed.
