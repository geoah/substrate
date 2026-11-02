# Review of the Rows build and the data-model decisions (2026-10-31)

The second build in rxtx.dev (geoah/rxtx.dev#118) applied the
[data-model decisions](data-model-2026-10-31/DECISIONS.md) to the training package and
the app's core, then built the [Rows](../design/rows/README.md) session layout. Before
the PR opened, the same workflow ran two independent reviewers, one reading the data
(the decisions D1 to D13 and A1 to A3 against the package, examples, codec and engine)
and one reading the Rows UI, and then a fixer. Between them they raised 20 findings,
numbered F1 to F20.

The workflow kept only each reviewer's summary verdict, not its full numbered list, so
what follows is those verdicts and the fixer's account, verbatim. Paths are the rxtx.dev
worktree's at the time: `apps/apple/spotter/SpotterKit` became
`apps/apple/WorkoutsKit` here, and `research/workouts/substrate` became
[`Package/`](../../Package/README.md) and [`examples/`](../examples/README.md).

## Data reviewer (13 findings)

Most decisions are implemented. The package declarations, examples, codec and engine write paths mostly match DECISIONS.md, but there are four major issues. In the Table layout, the RPE sheet's equality check keeps a confirmed copied or prefilled RPE from becoming `entered`. The examples claim a D8 "planned set not done" case that is not in the data. `target(from:)` copies unknown planned-set fields into `set.target`, where the loader refuses them. The README's volume rule for alternating `perside` reps contradicts the code, and `repsBasis` is never read.

Status by decision:
- D1: partial (the Table RPE sheet; docs say nothing about `copied`; unknown future methods read as ratings).
- D2: done.
- D3: done, with two leftover `rir` references in the docs.
- D4: done (V1 writes no setup, by design).
- D5: partial (only bodymass percentages resolve, and this is undocumented in the package).
- D6: done on write; the volume read rule contradicts the code.
- D7: done, apart from the planned-set `extra` copied into the target.
- D8: partial (the false example claim; a wholly skipped slot drops its plan).
- D9: partial (a cr10 RPE is read as RIR-based).
- D10: done (minor nuance on timed sets' `restMethod`).
- D11, D12, D13: done.
- A1: done.
- A2: partial (target `extra` leak; snapshot strictness; some execution enums still default silently).
- A3: done (a session RPE of 0 is never sent).
- Documentation items: done (mapping-table corrections, code systems registry, the softened standards claim, multi-writer rules), with the wording nits listed.

Checks:
- No middots in added lines or new files.
- No nesting beyond level 3.
- No keys the loader rejects in the declarations or examples.
- No chain member is written before its head.

Verification I ran (read-only; nothing in the tree or git was changed):
1. `/tmp/spotter-bin/substratectl validate -f` on training/{bundle,exercise,routine,session,set,setup}.yaml, health/{health,sleep,workout}.yaml and examples/{exercises,routines,sessions,coverage}.yaml: exit 0, 11 declaration documents in 2 packages, 163 records envelope-checked.
2. `uv run --with pyyaml apps/apple/spotter/scripts/examples.py --check`: exit 0.
3. `swift test` in apps/apple/spotter/SpotterKit: 348 tests in 57 suites passed.
4. My own consistency script, /tmp/spotter-datareview-check/check.py and setups.py (outside the tree), over all examples. It found no violations of these rules:
   - `rpeMethod` appears exactly when `rpe` does, and `restMethod` exactly when `restBefore` does.
   - The load-meaning fields equal the exercise's.
   - No inverted ranges, and no leftover `rpe`/`rir` in a target, routine or snapshot.
   - Snapshot keys equal the routine's.
   - Every chain head comes before its members.
   - Each setup's exercise matches its sets' exercise.
   - Every session has `sport` and `timezone`.
   - The one prefilled RPE equals its target's top.
   - Counts: 58 entered, 1 prefilled and 3 imported ratings across 111 sets.
5. The `sport` enum in session.yaml matches health/workout's 19 values exactly.

## UI reviewer (7 findings)

The Rows screen is in good shape: no blockers, two major findings and five minor ones. Both builds and all tests pass.

#### Majors
- With a per-set rest override, a superset connector reads "0:00, straight to A2" while a 0:45 rest counts down. A scratch test reproduced it.
- VoiceOver does not read the time left on the rest capsule.

#### Checked and correct
- **Log values and provenance.** The Log label and the tick both read the same `commitValues`, which carry the plan's RPE as `prefilled` only while Rows is selected.
- **Pre-filled RPE.** It becomes `entered` only through `enteringRPE` (a cell tap). Steppers, reset chips, keypad entries and keypad chips never change the RPE. A tap on the filled cell clears it and stops the pre-fill. Fix mode sends `entered` only after a tap, and changing reps leaves a pre-filled value pre-filled.
- **Undo, then Log again.** A scratch test showed it re-logs `@ 8 prefilled`, and the engine accepts it.
- **Steppers.** They use the exercise's grid step. V1 sessions carry no setup, so this matches "the exercise's or setup's load step".
- **Rounds.** They are correct with unequal member set counts ("Round 3 of 3" with A1 alone, then the block rest).
- **Chains.** They show no rows that do not exist; a mini-set that is not made yet appears in words, without values.
- **Timers.** They tick through `Text(timerInterval:)` and `TimelineView`, with reserved widths.
- **Capsule.** It reads the same `state.countdown` as the coordinator, flips at `nextChange`, and the Live Activity code is unchanged.
- **Out of order and fixing.** Both behave as designed: "later" tags, Go back, a neutral fix panel, the rest keeps running, and Delete asks first.
- **Glass.** It is used only on the capsule, the keypad and the undo toast.
- **Concurrency.** The app builds in Swift 6 mode with MainActor default isolation and no warnings.

**The Table layout** is unchanged apart from the intended parts. Its tick still plays `.tick` and it pre-fills nothing. `SetMenuItems`, `GroupMenuItems` and `ExerciseMenuItems` were moved out of the Table's views word for word. One change you will notice in the Table comes from a data-model decision, not from this work: PREVIOUS now copies last time's RPE, marked `copied`.

#### Verification
1. `cd /Users/geoah/.t3/worktrees/rxtx.dev/t3-9e79ae4f/apps/apple/spotter/SpotterKit && swift test`: 348 tests in 57 suites passed.
2. `cd /Users/geoah/.t3/worktrees/rxtx.dev/t3-9e79ae4f/apps/apple/spotter && mise x xcodegen -- xcodegen generate && xcodebuild -project Spotter.xcodeproj -scheme Spotter -destination 'generic/platform=iOS Simulator' -derivedDataPath /tmp/spotter-uireview-dd build`: BUILD SUCCEEDED, 0 warnings. I used a fresh derived-data folder in /tmp instead of `build/dd` so I would not collide with other agents; it was a full clean build.
3. The same command with `-destination 'generic/platform=iOS'`: BUILD SUCCEEDED, 0 warnings.
4. Scratch Swift Testing cases in a throwaway copy of SpotterKit under /tmp (now deleted) covered three things:
   - undo then Log again;
   - the superset connector against a per-set override (reproduced the major finding);
   - unequal rounds.

#### Not done
- I did not drive the simulator; the findings come from reading the code and the scratch tests.
- The VoiceOver finding follows from how `.combine` plus `.accessibilityLabel` behaves; I did not hear it with VoiceOver on a device.

I edited no files in the tree and did not touch git state, geoah.me, port 8081 or substrate-dev-db. `xcodegen` regenerated `Spotter.xcodeproj`, which git ignores.

## The fixer's report

All 20 findings are addressed: 19 are fully fixed and F17 is partly fixed, with docs corrected to match. Every logic change has a SpotterKit test, except F4 (no logic change) and the view-only fixes.

SpotterKit code:
- `SetRowPresentation.rpePatch(for:)`: the RPE sheet's Done now rates the value shown.
- `set.target` no longer copies a planned set's unknown fields.
- `ratedRPE` and the new `unratedRPE` ignore unknown methods and scales that are not RIR-based; doneSets keeps the method of the field the value came from; a cleared rpe drops its method.
- `PlannedSet` reads a legacy `rpe`/`rir` from an old journal.
- Codec: the session snapshot is lenient; exercise laterality and myoStopRule are strict.
- `restMethod` is `start` when a timed set's play ended the rest.
- Rows connectors honour a per-set rest override; the capsule's session clock stops while paused (`RowsNowCapsule.pausedAt`).

App views:
- RPESheet.swift uses `rpePatch`.
- The rest capsule's VoiceOver value reads the time left.
- One least scroll for superset hand-offs, the entry check's Edit and Finish's way back.
- RPE strip at full panel width with a two-row fallback on narrow phones; 44 pt keypad chips and value target.
- Summary and connector text wraps further at large sizes.
- Undo after an entry-check answer.

Data and docs:
- 9 example records changed to `restMethod: start`; fixtures regenerated with examples.py.
- Examples docs: the lumbar stretch and not-exercised claims, the RIR and pre-filled wording.
- Package README, set.yaml and session.yaml: volume rule, provenance reader rules, unresolved percentage targets, dropped empty slots, restMethod.
- DESIGN.md: rule 7, §2.3, §3.5, §4.11, §4.14, §7.6a. API.md updated to match.

### Left open or partly fixed

F17 partial: the done mini-set chips are still at least 26 pt wide (44 pt tall hit area). Making them 44 pt wide would either widen the gaps between chips visibly or make their hit areas overlap. On 390–402 pt iPhones the RPE cells are 39–40 pt wide, because the owner's one-row strip is kept there; only 375 pt phones and accessibility sizes switch to two rows. DESIGN §7.6a and API.md now state these widths.
F2: only the smallest fix was applied, so no example shows a planned set that was not done. It is now listed as not exercised. The better fix (delete set 01a17d98…, recompute glute bridge 1's restBefore, update the 163/65/24 counts and the replay tests) is deferred.
F4: volume keeps the code's rule (reps as logged, repsBasis not read), so an imported alternating set counted in total reps counts twice what Spotter's per-side count of the same work would. This is documented, and V1 imports nothing.
F8: e1rm, onerm and trainingmax percentages stay unresolved in V1. This is now documented; the optional resolution of e1rm from history is not built.
F1: the Table's RPE cell still draws copied and prefilled values like ratings (the optional part), so the Table looks unchanged as the lead decision requires.
Group `pairing` stays lenient on purpose, with the reason given in DESIGN rule 7. The reviewer had suggested making it strict.
DECISIONS.md under /tmp still says D8 keeps planned sets 'including ones never performed'. It was not edited because it is outside this task's files.
