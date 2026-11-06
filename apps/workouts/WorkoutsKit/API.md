# WorkoutsCore: the public API by screen

A map for the app's engineers: what to call, from where. The spec is
[`DESIGN.md`](../DESIGN.md); doc comments in the sources say the rest. Everything here is `Sendable`, pure
unless it says it touches a file, over Foundation and the kit's portable libraries. The views compute no rule: they send commands and render
presentations.

## The loop (§2.2, §6.1)

`SessionController` is the only writer, a `Controller` on the kit's runtime (`SubstrateRuntime`), kept by
`WorkoutsApp` (WorkoutsSync) under the kit's `LinkController`. Its value carries what it presents with (the
`Catalog`, the `HistoryIndex`, the `WorkoutsSettings`); a view sends a `SessionCommand` to
`WorkoutsApp.sessionPath` and draws the `SessionScreenPresentation` it publishes. Per command the runtime runs, in
this order:

1. `events(for:in:)`: an `EventEnvelope(id:at:event:)` with a UUIDv7 id (`UUIDv7.make(at:using:)`, its random bits
   from the environment's id source), the instant `.flooredToSecond` and clamped to the previous event's, and the
   `SessionEvent` with every id it needs pre-minted.
2. `apply`: `SessionEngine.apply(envelope, to: state) throws(EngineError) -> SessionState`. On a throw: the refusal
   and a `.warning` cue in the presentation, nothing else; a refused Start also ends `starting`.
3. The journal: `SessionJournalFile`, the kit's `JournalFile<EventEnvelope, SessionState>` at
   `active-session.json`, as `journaling(of:)` says: Start `.restart`, an edit `.append`, Discard and `finished`
   `.clear`, `finishRequested` and `finishFailed` `.skip`.
4. `present`: `SessionPresenter.present(state, catalog:, history:, settings:, now:) -> SessionPresentation`, with the
   transient state (the entry check, the last refusal, each Save's outcome by its receipt). What the device does once
   is a `Signal` (SubstrateRuntime): `cue` (a `SessionCue`, held back while the journal cannot be written, so Rows'
   Log never says a set is kept that is not), `cover` (open or close it) and `scroll` (a `ScrollTarget`). The app
   follows each with `PresentationModel.onSignal`, once per raise.
5. `effects(of:on:presented:)`, on the live path alone: the notification and the Live Activity through the kit's
   `Notifications` and `OngoingActivities`, the history write, the Health queue through `WorkoutsServices`. The
   countdown's alert is `showsWhileActive: false` while the cover is on screen (`SessionCommand.coverVisible`), so it
   is quiet in front of the rest bar; `SessionController.command(for:)` turns a tap on it, read from the kit's
   `Notifications.responses()`, into `.reveal`, the cover scrolled to Up next, as the Live Activity's tap is.

At open the runtime folds the journal through `apply` (a cache that differs from the fold is written again; a file
that does not fold is set aside as `active-session.unreadable-<seconds>.json` and the presenter is told), then applies
`.opened(replayed:)`, whose effects restore the alert and the Live Activity and check a replayed session against
History. Finish is `finishRequested` (the records from `SessionEngine.records(state, adapter:)`), whose effect writes
`HistoryStore.write(HistoryEntry(records:))` and then the Health queue entry, then `finished` (or `finishFailed`). A
`FinishRequest` carries a `receipt`, and `SessionScreenPresentation.finishOutcomes[receipt]` answers it: nil inside
when saved, the reason when the file was not written or the engine refused (`finishRefused`), so the sheet waits for
its own Save and never reads the last one's words. At
each foreground, `HealthQueueFile.lost(in: history)` names the pending sessions the queue lost; `enqueue(missing:at:)`
queues them again, after `setAsideIfUnreadable(at:)`.

Event `Codable` shapes must keep decoding old journals: a new field is optional or decodes with a default; anything
else raises `journalFormat`.

The presentation's `nextChange` is a tick (`event(atTick:)`): when a countdown ends, and when "Rest over" gives way to
the Up next chip.

### What the controller fills from history

| Event field                                     | Call                                                                         |
| ----------------------------------------------- | ---------------------------------------------------------------------------- |
| `Start.rowIDs`                                  | `SessionEngine.rowIDCount(for: routine, exercises:)` ids                     |
| `Start.firstSides[slot]` (`weakerfirst`)        | `Previous.weakerSide(of: exercise, history:)`                                |
| `AddExercise.roles`, `.rests`, `.firstSide`     | `Previous.lastShape(of: exercise, history:)`, `Previous.weakerSide(...)`     |
| `CompleteSet.values` (tick, Live Activity Done) | `presentation.row(id)?.commitValues` (nil means the tick is disabled)        |
| `stopTimedSet` values                           | `row.stopValues`: the typed duration only, so the engine fills the time run  |
| `CompleteSet.noLighterLoad`                     | `SuggestionInputs.noLighterLoad(afterTicking:with:in:history:now:calendar:)` |
| `startTimedSet.target`                          | `row.timer == .ready(target:)`: the shown duration (typed, else the ghost)   |
| PREVIOUS tap (`editSet`)                        | `row.previous?.copy`, a `SetValuesPatch` with a rated RPE as `copied`        |
| RPE tapped (`editSet`)                          | `SetValuesPatch.enteringRPE(value)` on every tap, even of the value shown    |
| RPE cleared (`editSet`)                         | `SetValuesPatch.clearingRPE`: no rating, and no pre-fill on a pending row    |
| `Finish` (Save in the Finish sheet)             | `FinishPresenter.projection(...)`, then `projection.event(start:end:...)`    |
| Entry check before a tick, Stop or Done (§5.7)  | `EntryCheck.warning(for:values:in:history:now:calendar:) -> EntryWarning?`   |

`EntryWarning` holds `title`, `message`, `useTitle` and `correctedLoad`, `keepTitle` and `editTitle`.
`corrected(_:measurement:)` swaps in the corrected load.

## Session cover (§7.6) and accessory (§7.5)

Each screen reads one `SessionPresentation`:

- **Toolbar**: `title`. The jump menu lists `cards`, each with its `progress` ("2 of 3 done").
- **Discard**: `discardQuestion`.
- **Header**: `header`. `clockStart` and `clockPausedAt` feed `Text(timerInterval:)`. It also has `durationText`
  ("Paused" while paused), `volumeText` and `setsText` ("3 of 40"). The labels are `SessionHeaderStats.durationTitle`,
  `volumeTitle` and `setsTitle` ("Working sets": warm-ups never count).
- **Blocks**: a plain card, or a `GroupHeader` with its member cards. The header has `title` ("Superset A: 2 rounds,
  1:00 after each round"), `colorIndex` (0 to 3) and `members`.
- **`ExerciseCard`**:
  - `name`, `groupLabel` ("A1"), `restChip` ("Rest 1:30", "Then 0:00 to A2", "Round rest 1:00").
  - `rests`: each `SlotRestOption` has `kind`, `text` and `defaultText` ("Default (2:00)"). The picker's presets are
    `SlotRestOption.presets`.
  - `notes` (the gym's) and `planNotes` (the routine's).
  - The grid: `columns` and `columnTitles` (SET, PREVIOUS, KG, REPS, RPE, tick).
  - The keyboard steps: `loadStep` and `timeStep`.
  - `removeQuestion`; `isLocal` ("On this iPhone").
- **`SetRowPresentation`**:
  - `label`, a `SetLabel` with `short` (W, 2, 2a, M, 1L), `spoken`, `failureBadge` and `roleText` (Top, Back-off).
  - `state`: pending, upNext, locked, done or timing.
  - `previous`: a `PreviousSet` with `text` ("35 kg × 13 @ 9.5"), `deload` and `copy`.
  - `cells`: each `GridCell` has `text`, `isGhost` (grey), `isHint` ("≈15", "to failure"), `isMissing` (outline)
    and `entered` (what the field edits). VoiceOver: `accessibilityLabel` ("Leg Press (Machine), set 1, load in
    kilograms") and `accessibilityValue(draft:)` (the draft, the typed or done value, "suggested 40", "≈15, a hint",
    "required, empty", "empty"). Pass `draft` only when the field has one: an empty draft, a value cleared while
    typing, reads as the empty field shows.
  - `rpeText`, with `rpeAccessibilityLabel` ("Plank, set 1, RPE") and `rpeAccessibilityValue` ("not rated"). These
    show the RPE the row holds, never the pre-fill.
  - `rpeEntry`, a `RowRPE` for the Rows screen (below): `.none`, `.prefill(x)` (the plan's value, shown apart from a
    value the owner gave) or `.value(x, method)`; `rpePrefillText` ("8") when it is a pre-fill.
  - Rows with no RPE column (timed, distance): `canRateEffort` offers `SetRowPresentation.rateEffortTitle`
    ("Rate effort", the RPE sheet, `editSet`) and `effortText` ("RPE 8") is a small line under the row.
  - `timer`, a `TimedSetState` (ready, running or done). `.ready(target:)` is what Play sends; a running timer is
    overtime only once `now` passes `endsAt`.
  - `reason`, set only on Up next. `suggestion?.text` feeds the popover on other rows.
  - `stopText` ("Stop: mini-set 4 got 3, first was 4").
  - `canTick`, `missing`; `kind` with `allowedKinds` for the set-kind menu.
- **Rest bar**: `restBar`, a `RestBarPresentation`:
  - `.upNext(line)`
  - `.resting(RestCountdownPresentation)`, with `label`, `endsAt` and `next`
  - `.restOver(text)`
  - `.go(text)`
  - `.timedSet(TimedSetBar)`
  - `.allDone`
  - `.empty`: no exercise yet; a tap opens the exercise picker
- **Up next**: `upNext`, an `UpNextPresentation` with `line`, `values`, `exerciseWithGroup` and `round`.
- **Accessory**: `accessory`, with `title`, `detail` ("Chest Press (Machine), set 2"), `isPaused` and `restEndsAt`.
- **Start conflict (§7.4)**: `SessionPresentation.conflictTitle`. The message is `summary`; the Discard step asks
  `discardBeforeStartQuestion`.
- **RPE sheet**:
  - `RPEScaleText.values`, `headline(_:)` and `reserve(_:measurement:)` (nil for timed and distance sets)
  - `emptyHeadline` and `emptyReserve`
  - Done and Clear send `row.rpePatch(for: value)` (nil for Clear) when it is not nil: Done rates the value shown,
    even a copied or pre-filled one, and sends nothing only when the row already holds it as `entered`.
- **Set-kind and rest labels**: `SetKind.label`, `SlotRestKind.label`, `RestSlot.label`, `GroupKind.label` and
  `Pairing.label`.
- **Rows layout**: `rowsLayout`, the same session for the Rows screen (next section). The Table screen reads the
  parts above.

## The Rows layout (§7.6)

The Rows screen reads `presentation.rowsLayout`, a `RowsLayout`. It comes from the same `SessionPresenter.present` call
as the Table screen's parts, so both read one presentation and send the same commands. While Rows is shown, build the
presenter with `prefillsRPE: true`: then the RPE strip shows the plan's value and Log commits it.

Every control carries a `RowsAction`. Map each one to its `SessionCommand` and send it:

| `RowsAction`                                                        | Command                                                  |
| ------------------------------------------------------------------- | -------------------------------------------------------- |
| `.tick(row)`                                                        | `.tick(row)`: commits `row.commitValues`, entry check on |
| `.touch(row)`                                                       | `.touchRow(row)`: do it now; Go back                     |
| `.edit(row, patch)`                                                 | `.edit(row, patch)`: a fix's Save                        |
| `.removeSet(row)`                                                   | `.removeSet(row)`: Delete set, End myo-reps here         |
| `.addSet(slot:role:)`                                               | `.addSet(slot:role:)`                                    |
| `.startTimedSet(row)`, `.stopTimedSet(row)`, `.cancelTimedSet(row)` | the timed-set commands of the same name                  |
| `.adjustRest(±15)`, `.skipRest`                                     | `.adjustRest(±15)`, `.skipRest`                          |

Steppers, reset chips, keypad chips and RPE cells carry a `SetValuesPatch` instead: send `.edit(row, patch)` on the
open row; apply it to the draft when fixing (below).

**Blocks.** `blocks` are in display order, one per `ExerciseBlock`:

- `title` ("Leg Press (Machine)", "Superset A"), `subtitle` ("6–10 reps at RPE 8, rest 2:30", "Glute Bridge and Bird
  Dog, 2 rounds") and `colorIndex`.
- `items`, in the order the sets are done (a group's members interleaved by round).
- `addSets`, the `+ Add set` buttons: one per member in a group ("Add A1 set").
- `isFinished` and `summary`. A finished block shows `summary.text` ("110 kg × 8, 7, 8") and `summary.change` ("+5 kg",
  green when `isGain`) on one line until it is opened. VoiceOver reads `summary.sentence`.
- `slots`. The block's `…` menu reads `presentation.card(slot)` for its rests, notes and the rest of the menu.

**Items.** Each `RowsItem` is one of these:

- `.set(RowsSetItem)`, one set (below).
- `.warmups(RowsWarmupFold)`. Once every warm-up is done they fold into one line: `title` ("Warm-ups"), `values`
  ("40 kg × 12, 70 kg × 6") and `comparison` ("as last time"). Unfolding shows `sets`.
- `.round(RowsRoundHeader)`, a group's `title` ("Round 1 of 2"), with `isDone`. A group has no rest lines between its
  sets: the rest shows in the island, as for a plain exercise.
- `.chainHeader(RowsChainHeader)`, above a myo, myo-match, rest-pause or cluster set: "Set 2 as myo-reps, 0:15 between
  mini-sets".
- `.miniSets(RowsMiniSets)`, a chain's done mini-sets as chips:
  - `title` ("Mini-sets"), `chips` (`text` "4"; a tap fixes that row from `sets`).
  - `comparison` ("as last time", "first time") and, once the chain has ended, `stopText`.

**A set line** (`RowsSetItem`):

- `state`: `done`, `now` (open in place), `next` (pending, muted; a tap opens it, in any order) or `locked`.
- `role`, which sets the colour: orange for `warmup` and `drop`. Also `isChained`: the line hangs off the one above.
- `title` ("Set 1", "Warm-up 2", "Activation", "Drop", "Mini-set 3", "Set 1 left"). In a group, the exercise's name
  with `badge` ("A1").
- `values` ("110 kg × 8", "16 reps", "11 per side", "0:58 hold", "25 kg to failure"). Tint them when `valuesChanged`.
- `rpe` ("@ 8") on done rows; `rpeIsPrefilled` when it was the plan's value logged as shown.
- `tags` ("+ drop", "to failure", "top set", "warm-up"), each with a `style`. `collapsedTag` ("up next") goes
  on the open row only while another row's fix is open and this one is drawn collapsed.
- `last` ("last 105 × 10 @ 9.5") on pending rows.
- On done rows, `changes` and `verdict`. Each change is green when `isGain`, else grey: `+5 kg`, `−2 reps`, or one
  word, "as last time" or "first time". The verdict's `symbol` is `↗`, `=` or `↘`; it is green only for `better`.
- `sentence`: the row as VoiceOver reads it.
- `tap`: `.fix` (open `fix`), `.doNow(action)` (send it) or `.none`.
- `editor`, on the open row only. `fix`, on done rows.

**The editor** (`RowsEditor`), open in place:

- `mode`: `log` for the set to do now, `fix` for a done one. `tint`: `accent`, `orange` (warm-up, drop) or `neutral`
  (fix).
- The header: `title` and `badge`, `note` and `trailingNote` ("no rest").
  - `note.kind` is `reason` (a sparkle before the suggestion's words) or `status` ("of 2", "to failure", "at 6 kg",
    "logged at 12:06"). There is no "after the rest": a set waits for nothing.
  - It is `running` (a red dot) for "holding".
- `fields`, each a `RowsField`:
  - the big number: `value`, else `placeholder` ("to failure", "≈15").
  - `unit` ("kg", "reps", "per side"), then `last` ("last 105") at a fixed position.
  - When `isChanged`, tint the value and show `reset` (`↺` before its `text`: "10", or "was 10" when fixing).
  - `isMissing`: outline it.
  - `minus` and `plus`: `title` "−5" and "+5", `patch`. Nil when the step would go below 0, or below 1 rep.
  - `keypad`, the glass keypad behind the dotted underline:
    - `title`, and `chips` with their `chipsTitle` ("5 kg steps, tap one to set it"). A chip applies at once and
      closes the pad.
    - `allowsDecimal` gives the `.` key.
    - Every key applies `patch(typed)`, which is nil while the text is no value; then, and on Cancel, apply
      `restoring(values)` with the row's values from before the pad opened. Done only closes the pad.
  - `accessibilityLabel` and `accessibilityValue` ("9, changed from 10").
- `rpe`, a `RowsRPEStrip`: five whole `cells`, 6 to 10, and `half`, the `½` that adds or takes off the half point.
  - Each cell has `value`, `text`, `isHalf`, `style` and `patch`. The cell holding the value stands for it, half point
    and all (`text` "8.5").
  - `style` is `.outlined` (hollow, shown but no rating: the plan's pre-fill, or a pre-filled or copied value) or
    `.filled` (a rating). A tap on an outlined cell rates it; a tap on the filled cell clears it.
  - `half`: `text` ("½"), `isOn`, and `patch`, the value with the half added or taken off, rated; nil with no value
    and at 10.
  - `meaning` ("RPE 8.5: could do 1 more rep, maybe 2", on a cell and on `half`) goes in the header for a moment after
    a tap.
  - `isCollapsed`: show `addTitle` ("RPE", after a plus symbol) until the owner opens the strip.
- `timer`, a `RowsTimer`, on a timed row:
  - `phase`: `ready`, or `running(startedAt:endsAt:)` (the big clock counts up from `startedAt`).
  - The track: `trackEnd`, with `goal` and `goalText` above it and `last` and `lastText` below it. Place them with
    `fraction(_:)`.
  - `caption(elapsed:)` ("0:21 to beat last time").
- The buttons: `primary`, `secondary` and `delete` (when fixing), each a `RowsButton` with `isEnabled`, plus
  `cancelTitle` (when fixing).
  - `primary` is Log ("Log 110 kg × 8 @ 8", exactly what the tick commits), or Start, Stop and log, or Save.
  - `secondary` is the log without the timer beside Start, Cancel beside Stop, or "End myo-reps here".

**Fixing a done row.** A tap on a done row opens its fix in place. Keep `draft = item.fix.logged` as view state and
render `fix.editor(draft:)`. Apply every patch to the draft: `draft = draft.applying(patch)`. Save sends
`fix.save(draft)`. Cancel drops the draft and sends nothing. The rest keeps running throughout.

**The island** (`island`, Liquid Glass, at the bottom) replaces the rest bar on this screen. It stacks up to three
parts; draw `island.parts(editorInView:)`, top to bottom:

- `rest`, a `RowsRestPart`, while a rest, pause or switch counts down: from `startedAt` to `endsAt`, with `totalText`
  ("of 2:30"), `title` ("Rest", "Round rest", "Pause"; VoiceOver's label), `minus`, `plus` and `skip`.
- `next`, a `RowsNextPart`: the open set compact. `exercise`, `badge`, `label` ("Set 2", "Round 1", "Drop"), `tint`,
  the editor's `fields` (steppers and keypads as in the editor), `rpe` ("@ 8") and `primary` ("Log", "Start" or "Stop",
  its `accessibilityLabel` the editor's full title). `timerStartedAt` while a timed set runs. `parts` leaves it out
  while `editorInView`: the list shows the set then.
- `after`, a `RowsAfterPart`: the set after the next one. `label` ("Then"), `text` ("Calf Press (Machine), set 1,
  60 kg × 12") and `action` (`.touch(row)`); `row` and `action` are nil for a mini-set not made yet.
- `end`: `.allDone(title:detail:)` with Finish, or `.empty`, where a tap opens the exercise picker.

`isCounting` (a rest or a timed set) keeps the island on a page pushed inside the cover.

The rules the presenter applies:

- Sets are done in any order: every pending, unlocked row is `next` and opens with `.doNow(.touch(row))`. Nothing marks
  a passed-over row.
- A drop that cannot be done yet is a `+ drop` tag on its set.
- The verdict compares estimated one-rep maxes on the suggestion engine's curve. It adjusts for RPE when both sets
  carry a rating, and counts within 1 % as the same. With no estimate, the changes decide when they all point one way.
- Nothing counts up: there is no "is up" moment after a Log and no clock since the last set. The rest part shows only
  while a rest runs (not a timed set's own countdown); a timed set's clock is in its editor and in `next`.

## RPE on the Rows screen (§4.14)

The Rows screen shows the plan's RPE pre-filled on each pending row, and one tap logs the set with it. Every RPE a
set record carries says how it got there (`rpeMethod`), so the screen must send what the owner did, never infer it
from the value:

- **Turn the pre-fill on**: build the presenter with `PresenterSettings(..., prefillsRPE: true)`. Then every pending
  row's `commitValues`, the Live Activity's Done and "Mark them done as shown" (`UntickedRow.values`) carry the row's
  pre-fill as `rpeMethod: .prefilled` when the row holds no RPE of its own. Off, nothing is pre-filled.
- **Show it**: `row.rpeEntry`. A `.prefill(8)` is the plan's value, not yet the owner's: draw it apart from a
  `.value(8, .entered)`. `.value(_, .copied)` came from PREVIOUS; on a done row `.value(_, .prefilled)` was accepted.
  The Rows editor's `RowsRPEStrip` does this for you: outlined for no rating, filled for a rating, each cell with its
  patch.
- **Accept it**: the tick. Send `commitValues` as today. Outside the presenter,
  `state.acceptingRPEPrefill(values, row:)` does the same, and `state.rpePrefill(ofRow:)` and `state.rpe(ofRow:)` give
  the raw answers.
- **Enter a value**: `.edit(row, .enteringRPE(value))` on every tap in the RPE control, also when the value equals the
  pre-fill or the copied value; the RPE sheet must not skip a tap because the value is unchanged
  (`SetRowPresentation.rpePatch(for:)` decides).
- **Clear it**: `.edit(row, .clearingRPE)`: no rating, and the pre-fill stays off on that row until a value is tapped.
- **Untick**: a `prefilled` RPE goes back to being the pre-fill; an entered or copied one stays.
- **Refusals**: `EngineError.invalidEffort` when an event claims `prefilled` for a value that is not the row's pre-fill,
  `prefilled` in an edit, or `imported`.

## Records and the codec (§3)

- `RecordCodec` reads and writes five kinds; `decode(_:)` returns `DecodedRecord` (`exercise`, `routine`, `session`,
  `set`, `setup`). References decode from substrate's `{"ref": "<kind>/<id>"}` and from the bare path, at every level.
- Every nested type keeps unknown fields in `extra` and writes them back. An unknown execution enum (a routine's
  planned set's `structure`, `role`, `termination`, `timerMode`, `myoStopRule`, `loadReference`; a slot's `sideOrder`;
  an exercise's `laterality` and `myoStopRule`; a set's `role` and `structure`) throws `CodecError.unsupported`: list
  the record as unreadable, never run it. A session's plan snapshot is never run: an unknown value there stays in the
  planned set's `extra`. A planned set's unknown fields never reach `set.target`.
- `SessionEngine.records(state, adapter:)` writes on each set: `rpeMethod`, `rpeScale: rirbased`, `completionMethod`,
  `timeBasis`, `restMethod` (`start` when a timed set's play ended the rest, else `interaction`), the load meaning
  (`loadBasis`, `implements`, `bodyShare`, `laterality`, `repsBasis`) and
  the full `target`; on the session: `sport: strength`, `timezone` and each slot's plan snapshot (`slots[].sets`).
- Readers take ratings only: `PerformedSet.ratedRPE` (and `SetValues.ratedRPE`), on an RIR-based scale and with a
  method this build knows; `unratedRPE` is a pre-filled or copied value with its method; `loggedRPE` is the value
  whatever its method. `SetTarget.effortTop` and `PlannedSet.effortTop` are the top of an effort range.
- Volume reads each set's own load meaning: `Counting.volume(sets, exercises:...)` falls back to the exercise only for
  a set written before the fields existed.
- `Setup` and `Exercise.on(setup)` (a setup's load grid over the exercise's); V1 writes none. `HistoryIndex.history(of:
  setup:)` takes a `SetupID?`.

## Live Activity and notification (§9.1, §9.2)

`presentation.liveActivity` is a `LiveActivityContent`, which the session sends the kit's `OngoingActivities` as a
`LiveActivityPayload` (the content, the controller's revision, and after Finish the summary) and the app reads into
`WorkoutsActivityAttributes.ContentState`:

- `title`, `clockStart`, `clockPausedAt`
- `exercise` (empty when all sets are done), `groupLabel`
- `next` ("Set 2: 35 kg × 14" or "All sets done")
- `nextRowID` (Done's row, or Stop's while a timer runs)
- `canComplete`: Done can tick that row as shown, or Stop can stop the timed set; false when a value must be typed
  first or §5.7's entry check would ask, so the widget shows neither
- `countdown`: a `LiveActivityCountdown` whose `kind` has the same raw values as the app's `Countdown.Kind`
- `setsDone`, `setsPlanned`

`presentation.notification` is a `CountdownNotification` with `title`, `body` and `fireDate`, which the session
schedules through the kit's `Notifications` only when `fireDate` is in the future.
`SessionPresenter.finishedSummary(records)` gives "Saved: 46 min, 24 sets".

## The app's root under the link (WorkoutsSync)

`WorkoutsApp` is a `LinkedApp`: the link's snapshot arrives as `.linked`, and a sync step's 401 or 403 goes back up
as a `LinkReport`. Its state holds the catalogue, History, the settings and the signed-in account's standing; its
commands (`WorkoutsApp.Command`) are the tabs' changes, a sheet that waits naming a receipt answered in
`WorkoutsPresentation.outcomes`. A sync step names the `AccountRef` it ran for, and one for an account no longer
signed in changes nothing. What an event changed (an account that moved, a package that became writable, a conflict
the outbox did not hold) is read in `effects(of:on:from:presented:)` from the state before and after, never kept as a
flag in the state. `flushing` is set only where a sync runs (a writable package), so `until { !flushing }` ends. The
session's events reach the app only where they are handed off: `finished`, `discard`, and an engine event that adds
an exercise made on the phone.

### Transitional, not patterns

Two shapes here are on their way out, and new code does not copy them:

- The settings flow one way: the app's `AppModel` (on the main actor) keeps them and sends each change as
  `WorkoutsApp.Command.settings`. The runtime's `WorkoutsApp` should own them, with the app's model reading the
  presentation.
- `SessionScreenPresentation.state` carries the engine's `SessionState` out to the views, for
  `FinishPresenter.summary` to work the Finish sheet's numbers out as its draft moves. A presentation carries what a
  view draws; the summary belongs in the presenter, with the draft as a command. Each account's files are a `WorkoutsFolder` from `WorkoutsFolders` (in the environment,
so the link flushes the same `Outbox` the app queues into): `Outbox.workouts(directory:)`, the kit's outbox over
`outbox.json` with `Outbox.workoutsUpgrade` for a format 1 file (respelled, with nothing it knew about `~/training`
kept: no version, chain or guard, and no session write, which the history exchange derives again) and the routine and session helpers in
`WorkoutsOutbox.swift`.

## Finish sheet (§7.7)

`FinishPresenter.summary(state, presentation:, policy:, start:, end:, healthAllowed:)` returns a `FinishSummary` for a
`PendingSetPolicy` (`.drop` or `.markDone`, the dialog's choice; `.drop` when nothing is unticked):

- `start` and `end`, clamped, with `latestStart` and `earliestEnd` for the pickers
- `durationText`
- `rows`, as `LabelledValue`s: Working sets, Volume, Exercises, as Save will write them under the policy
- `untickedTitle` ("3 sets are not ticked") and `untickedMessage` (the dialog's message, "2 have a value missing and
  are left out either way.", or nil)
- `pendingText` ("11 unticked sets are saved as shown; 2 are left out, a value is missing."), a line under the rows
- `savedLine`, and `projection`

Save: `FinishPresenter.projection(state, presentation:, policy:, end:, recordID:)` gives a `FinishProjection`
(`savedSets`, `markedSets`, `incompleteSets`, `droppedSets`, `workingSets`, `volume`, `exercises`,
`exercisesInSession`, `unticked`). It runs the engine's own `finish` on a copy, so the sheet and the saved record cannot
disagree; send `.finish(projection.event(start:end:sessionRPE:notes:utcOffset:))`. There is no summary without a
policy: one that counted ticked rows only showed numbers Save would not write.

The dialog's buttons are `FinishSummary.dropTitle`, `markDoneTitle` and `cancelTitle`. `SessionRPEText.word(_:)`
gives the CR10 words.

## Routines (§7.2, §7.3)

- `BundledCatalog(localExercises:).load()` returns a `Catalog` with `exercises`, `routines` and `unreadable`.
  `updates()` yields after `addLocalExercise(_:)`.
- `RoutinePresenter.list(catalog, history:, settings:)` returns a `RoutineListPresentation`. It has `sections`, each
  with its folder `title`, `count` ("4 routines") and `routines`. Each card has `exercises` and `lastDone`. The list
  also has `unreadable` ("2 routines could not be read") and `emptyText`.
- `RoutinePresenter.preview(routine, catalog:, history:, settings:)` returns a `RoutinePreviewPresentation`. It has
  `lastDone` and `blocks`. A block has a group `header` ("Superset A, rest 1:00 after each round"). Each slot has a
  `groupLabel` and `lines` ("2 sets of 10–15 reps at RPE 8, from 90 kg", "3 sets of 8–12 reps at RPE 7–8, from
  12 kg", "3 sets of 5 reps, from 80% of training max", "Set 2 is a drop set: 1 drop", "Rest 1:30").
- `RoutineEditor(routine)` edits a routine through its typed fields, keeping every unknown field. It reads the routine
  as `blocks` (a plain slot, or a group and its members) and has `rename`, `setFolder`, `setNotes`, `moveBlocks`,
  `moveMembers`, `moveSets`, `groupWithNext`, `leaveGroup`, `ungroup`, `updateGroup`, `addExercise`, `removeBlocks`,
  `removeMembers`, `removeSlot`, `update`, `addSet`, `removeSets` and `updateSet`. `patch(codec:)` is the PATCH body
  (what changed, `null` for a cleared property), `problem` what stops a save, and `duplicate(_:taken:)` a copy at a
  new slug. `RoutineDrafts` keeps one editor on disk.
- `Outbox.edit(routine:patch:baseVersion:codec:)` queues a guarded routine PATCH; a `409` parks it with `conflict`,
  which `resolve(_:keepMine:)` answers. `waitingRoutines(codec:)` is what `RemoteCatalog.refresh(_:hiding:waiting:)`
  lays over the server's routines, and `RemoteCatalog.store(routine:)` keeps an edit in the cache at once.

## History (§7.8)

- `HistoryStore(directory:).loadAll()` returns a `HistoryLoad` with `entries` and `unreadable`. Build a
  `HistoryIndex(entries.map(\.records))` for everything else. Files are format 2; a format 1 file is read through
  `HistoryMigration.fromFormat1` and written back as format 2 on its next save.
- `HistoryStore` also has `delete(_:)` and `recordHealthSave(_:status:healthKitWorkout:error:at:)`.
- Apple Health's effort score is `session.healthEffortScore`: nil when the session RPE was skipped or is a CR10 0,
  which has no effort score (Apple's runs from 1).
- `HistoryPresenter.sections(entries, exercises: catalog.exercises, settings:)` returns `[HistorySection]`. Each
  section has a month `title`. Each row has `title`, `date` ("Tue 27 Oct, 13:30"), `summary`
  ("46 min, 24 sets, 3,120 kg") and `health`. Use `emptyTitle`, `emptyMessage` and `deleteQuestion` for the copy.
- `HistoryPresenter.detail(entry, exercises:, settings:)` returns a `SessionDetailPresentation`:
  - `when`: "Tue 27 Oct 2026, 13:30 to 14:16 (46 min)"
  - `sessionRPE`, `healthText`, `canRetryHealth`
  - `blocks`, each exercise with its `sets`, which carry `label`, `text` and `rest`
  - `muscles` ("Chest 2, Triceps 1"), `notes`

## Exercises (§7.9) and the picker (§7.6)

- `ExercisePresenter.list(exercises, history:, query:, settings:)` returns sections A to Z.
  `ExercisePresenter.picker(..., now:)` adds "Recent" first.
- Each `ExerciseRow` has `equipment` (`Exercise.equipmentText`: "Bodyweight" or nothing, never "None"), `lastDone`
  and `isLocal`. `ExerciseRow.localText` is "On this iPhone".
- `ExerciseSearch.matches(exercise, query:)` matches tokens in any order, ignoring case and diacritics. It knows
  db, bb, kb, rdl and ohp.
- `ExercisePresenter.detail(exercise, history:, settings:)` returns an `ExerciseDetailPresentation`. It has `facts`
  (`LabelledValue`s, stacked label over value; the grid as "Load: 5 kg steps") and `sessions`, whose lines put a chain on one line. It also has
  `trend`, an `ExerciseTrend`:
  - `metric`: oneRepMax, longestHold, mostReps or none
  - `title`, and `points` with `hollow` for deloads
  - `points(lastMonths:now:calendar:)`
  - `hasTrend(_:)`; under two points show `ExerciseTrend.notEnough`
- New exercise: `ExerciseID.slug(for:taken:)`, `MeasurementType.label`, `Equipment.label`, `Laterality.label` and
  `RestClass.label`.

## Settings (§7.10)

`SessionSettings` is snapshotted into `Start`. It holds `defaultUnit`, `warmupRest` and `setRest`
(`SetRestDefaults`, a subscript by `RestClass?`), plus `restAfterLastSet` and `countWarmupsInVolume`.
`PresenterSettings(calendar:defaultUnit:countWarmupsInVolume:prefillsRPE:)` serves the non-session screens, and with
`prefillsRPE` turns on the Rows screen's RPE pre-fill. Use
`Catalog.unreadable` and `HistoryLoad.unreadable` for "Records that could not be read".

## Formatting (§4.13)

Never format by hand. `Format` provides:

- `duration` ("1:02:05"), `rest` ("Off" for 0), `overtime` ("+0:05"), `minutes` ("46 min")
- `mass` ("92.5 kg"), `load(_:mode:)` ("BW + 2.5 kg"), `set(_:measurement:rpe:repsHint:)` ("35 kg × 13 @ 9.5")
- `rpe`, `range` ("10–12"), `volume` ("3,120 kg"), `count` ("1 set")
- `rpeRange` ("7–8"), `effort(rpeMin:rpeMax:rirMin:rirMax:)` ("RPE 7–8", "2–3 reps in reserve"), `loadReference`
  ("training max")
- `day`, `dayAndTime`, `time` and `month`, on an explicit zone

Grid headers come from `GridColumn.title(unit:)`. Copy is English. Inline stats are joined with commas, never a
middot.
