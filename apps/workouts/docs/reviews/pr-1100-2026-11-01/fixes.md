# Fixes for the reviews of #1100 (2026-11-01)

Codex Astra ([astra.md](astra.md)) and Fable ([fable.md](fable.md)) reviewed
geoah/substrate#1100 at `f9eeb332`, from the brief in [BRIEF.md](BRIEF.md). Their majors
overlap: Astra raised six and one minor, Fable five majors, seven minors and five nits.
All majors were fixed on the branch before it merged (as `d3642e82` on 2026-11-02), in
four commits:

| Commit     | Subject                                                                                                                       |
| ---------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `a7723525` | fix(api): hold a declare scope to additions and a bundle that only lists its kinds                                            |
| `50742b51` | fix(apple): make Substrate Workouts' sync safe across deletes, sign-outs and an owner's package edits                         |
| `22b23a47` | docs(apple): say how Substrate Workouts syncs history and keeps its package after the review fixes                           |
| `492044ec` | test(apple): check the history exchange against a live server, quiet after a write and a delete read as the owner's          |

## Finding by finding

From the fixer's report, 2026-11-01.

### Astra

| # | Finding | Status |
|---|---|---|
| 1 | Index-based flush breaks on a concurrent drop or reset | Fixed, 50742b51. Entries are found by id after every await, and `reset` bumps a generation the flush checks. A purge asked for while the create is in flight goes after it. Writes queued mid-flush go in the same run. |
| 2 | Deleted sessions come back | Fixed, 50742b51. The outbox keeps per-repository record versions (what this phone sent to or read from that repository), plus phone-wide tombstones. A session the server dropped after this phone sent it is deleted on the phone and never re-uploaded. A session deleted on the phone is purged on the server, and pending deletes are never imported. |
| 3 | An old sync runs on after sign-out or an account switch | Fixed, 50742b51. Each sign-in is a `SyncAccount` with its own API, outbox, catalogue and package check. A run checks after every suspension that its account is still current, and `begin` cancels the old run. |
| 4 | Package version alone misses owner kind edits | Fixed, a7723525 + 50742b51. The app records each kind row's version after its apply and stops at `.edited` before an upgrade over an owner's change. The server refuses any removal. The race between the app's read and its apply is **not closed**: `/vocabulary/apply` takes no expected version. This is stated in 0203 and docs/apps.md. |
| 5 | Persistence failures are swallowed | Fixed, 50742b51. Saves throw `OutboxNotSaved` and roll back. An unreadable file is set aside and listed. History delete writes its tombstone first. |
| 6 | Server edits never reach the phone | Fixed, 50742b51. A record at an unseen version, or a set added or removed, is merged in. The phone keeps its Health state, and a session with a write waiting is left alone. |
| 7 (minor) | Starter copy ignores `writable` | Fixed, 50742b51. The copy is gated on the current account's package, and the button is disabled with the reason. |

### Fable

| # | Status |
|---|---|
| 1, 2 | Fixed (same as Astra 1, 2) |
| 3 | Fixed, a7723525. The scoped apply refuses with 409 a kind that drops a property, field, enum value, state, transition or trait. It also refuses a bundle that doesn't list everything the stored bundle lists. The docs and 0203 are corrected. |
| 4 | Fixed, a7723525. Bundle keys are whitelisted, so `modules`, `oauth2` and `inputs` are refused with 403. Tests added; the approval wording, docs and 0203 match. |
| 5 | Fixed, 50742b51. Refused writes are parked and listed in Settings with Retry and Discard. A guarded PATCH of a record that is gone is parked rather than sent blind. |
| 6 | Fixed. Access ended or ending, approval changed and server too old now reach the status. |
| 7 | Partly fixed. Sign In Again now replaces and revokes the old session. Skipped: no message when the user picks a different server. |
| 8 | Fixed. List reads go to the end of the list. |
| 9 | Fixed. An unfinished server copy gets Finish again, above the server's revision. |
| 10 | Fixed (same as Astra 5) |
| 11 | Fixed. `embed` and `indices` are refused under `declare:`. |
| 12 | Partly fixed. A `declare` row no longer shows "Covers today". Skipped: the write row before install still lists no kinds. |
| 13 | Skipped. The `scopes_supported` template is already acknowledged in 0203. |
| 14 | Fixed. The app now ships `OutboxSink` through `SyncAccount`, so the tested code is the shipped code. |
| 15 | Fixed. Spotter leftovers removed from the docs. |
| 16 | Skipped. Harmless in practice, and doing it per key is a larger change. |
| 17 | Partly fixed. The starter copy is gated and still idempotent on retry, but doesn't go through the outbox. |

## The fixes as the PR describes them

Verbatim from the "Review fixes" section of #1100's description.

Codex Astra and Fable reviewed `f9eeb332` (6 + 5 majors, overlapping). All majors are fixed in `a7723525` (server), `50742b51` (app), `22b23a47` (docs) and `492044ec` (live test).

### Server, `declare:` (a7723525)
- A declared bundle may carry only `authority`, `package`, `description`, `requires`, `requiresAtLeast`, `installs`; `modules` (inline code), `oauth2`, `inputs` and anything else refuse the batch with 403, as do a kind's `indices` and a property's `embed` (they spend the owner's database and provider).
- The scoped apply only adds: a kind document that leaves out a stored property, field, enum value, state, transition or trait, whoever added it, is refused with 409, and so is a bundle (which replaces its package whole) that doesn't list every declaration the stored bundle lists. A package holding kinds without a bundle can't be replaced by one.
- The token reads its own package's kind rows, so the app can record kind versions. Approval wording (console + SubstrateKit), `docs/apps.md` and 0203 now say exactly this.
- **Not closed:** `/vocabulary/apply` takes no expected version, and the guard reads the stored package outside the engine's transaction, so an owner edit landing between the app's read and its apply is not seen. Stated in 0203 and the docs.

### App (50742b51)
- **Outbox:** entries found by immutable id after every await, never by index; `reset` bumps a generation the flush checks; a purge asked for while the session's create is in flight goes after it at the version it answered; writes queued mid-flush go in the same run. Saves throw (`OutboxNotSaved`) and roll back instead of being swallowed; an unreadable `outbox.json` is set aside and listed. A write the server refuses for itself (404 on a purged session, validation) is **parked**, holds only its session, and is listed in Settings → "Refused by Your Substrate" with Retry and Discard.
- **History:** planned by `HistoryExchange` from the outbox's per-repository record versions (what this phone sent to or read from that repository) and phone-wide tombstones (`deleted-sessions.json`, written before the history file goes). A session the server dropped after this phone sent it is deleted on the phone, never re-uploaded; a session deleted on the phone is purged on the server and never imported; server edits (a record at an unseen version, a set added or removed) are merged, keeping the Health state, unless a write is waiting; a Finish refused as stale is sent again above the server's revision; list reads go to the end of the list.
- **Accounts:** each sign-in is a `SyncAccount` (its own API, outbox, catalogue, package check); a run checks after every suspension that its account is still current before it publishes or writes; Sign In Again replaces and revokes the old session.
- **Package ownership:** the installer records each kind row's version after its apply (`package.json`) and, before an upgrade, stops at `.edited` naming the kinds the owner changed, until they tap "Update the Package Anyway"; the server's 409 reads as `.refused` in its words. Neither state writes.
- **Writes gated:** "Add the Starter Routines" and every write path require the current account's package at this build's version; the button is disabled with the reason. The kit's status issues (access ended or ending, approval changed, server too old) now reach the status.

**Tests:** `TestADeclaredBundleListsItsKindsAndNothingElse`, `TestAnAppsApplyNeverRemovesWhatThePackageHolds`; WorkoutsSync 23 → 41 tests (drop, discard, reset and enqueue while a send is held on the wire; disk and decode failures; parking; the exchange; owner edits). `LiveServerTests` on a fresh repository now also upgrades the package through the real guard, has the owner add a property (the next upgrade stops at `.edited`, and applying anyway is refused naming it), checks the exchange is quiet after the outbox's own writes, and reads an owner's purge as a removal.

**Release:** `0.113.1-pre.20261101.224123+492044ec`.

**Skipped:** Fable #7 (part: no message when Sign In Again picks a different server), #12 (part: the write row before install still lists no kinds), #13 (`scopes_supported` template, acknowledged in 0203), #16 (`Rehome` swaps free text), #17 (starter copy writes outside the outbox; now gated, idempotent on retry).

## Left for the owner

The fixer's four questions. None is decided yet; [decisions.md](../../decisions.md#open-questions)
carries them as open.

- **Update anyway:** "Update the Package Anyway" lets the owner apply over their own kind edits. Edits to labels and descriptions get replaced; additions survive because the server refuses removals. Keep it, or require the owner to settle the package in the console?
- **The apply race:** closing it needs an expected-version precondition on `/vocabulary/apply`. That's a server change I didn't make.
- **Tombstones are phone-wide,** not per repository: a session deleted on the phone is purged in every repository it reaches.
- **Sessions imported by the old build:** they carry no record versions. Until this build's first sync records them, one the server deletes would be re-uploaded once.
