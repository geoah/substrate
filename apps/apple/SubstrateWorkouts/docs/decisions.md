# Decisions

Every product, design and data decision behind Substrate Workouts: what was decided, who
decided it, when, and why, with the source. "The owner" is the person the app is built for,
who owns the substrate it writes to. "The lead" is the agent that led each build. The lead
took the decisions marked as its own on the owner's behalf, and each stays open to the
owner. Dates are UTC.

The decisions that shape the server, rather than this app, are decision records in
[`docs/decisions/`](../../../../docs/decisions/README.md): the app's package and its
`declare:` scope are
[0203](../../../../docs/decisions/0203-an-app-keeps-its-own-package-under-a-declare-scope.md).

## Contents

- [What the app is for (owner)](#what-the-app-is-for-owner)
- [The name](#the-name)
- [The session screen](#the-session-screen)
- [The data model, first draft (lead)](#the-data-model-first-draft-lead)
- [The data model after the reviews](#the-data-model-after-the-reviews)
- [The build spec's own decisions](#the-build-specs-own-decisions)
- [Moving into substrate](#moving-into-substrate)
- [After the reviews of #1100](#after-the-reviews-of-1100)
- [Open questions](#open-questions)

## What the app is for (owner)

### P1. An iPhone app for strength sessions, with its data on substrate

- **Decided by** the owner, 2026-10-30.
- **What:** build a substrate-based workout app for the iPhone, with a Liquid Glass UI. A
  watch app is a side note for later and is design only for now. The data should be
  consistent with Apple Health and other systems, ideally a well-known format "like FHIR",
  so that history from other providers can be brought in later.
- **Why:** the owner's own request. The research found that no standard goes below the
  session, so the app owns its model and maps to HealthKit, Health Connect, FIT, Hevy and
  Strong field by field ([research 01 §0](research/01-formats-and-standards.md#0-the-answer),
  [proposal 087](history/087-spotter-proposal.md#the-format-verdict)).
- **Source:** the owner's first message, 2026-10-30.

### P2. Routines are written by the console's agent; the app runs them

- **Decided by** the owner, 2026-10-30.
- **What:** for the first iteration the substrate console and its agent create the
  workouts. The app browses routines, starts sessions from them and runs them in the gym.
  Editing a routine in the app is out of scope ([DESIGN.md §1.2](../DESIGN.md#12-what-v1-does-not-do)).
- **Why:** the owner was "happy to use substrate console and the agent to create the
  workouts for the first iteration".
- **Source:** the owner's first message, 2026-10-30.

### P3. What a session must support

- **Decided by** the owner, 2026-10-30.
- **What:** going through the exercises in any order, supersets, myo-reps, notifications
  when rest is over, different rest times inside a superset than after it, and RPE. Also
  suggestions for the next set's reps and weight, from the sets just done and from earlier
  sessions. The owner attached 13 screenshots of Hevy "as inspiration for the features".
- **Source:** the owner's first message, 2026-10-30. The research turned this into
  [39 requirements](research/README.md#requirements).

### P4. Leave Apple's own strength features alone for now

- **Decided by** the owner, 2026-10-31.
- **What:** do not link to a Strength workout started on an Apple Watch. The app writes its
  own strength workout for each session to Apple Health.
- **Why:** "hm, I didn't even know apple had a strength thing, let's ignore this for now,
  we'll come back to it later". This answered the lead's first question (research Q23,
  proposal 087's question 1).
- **Source:** owner, 2026-10-31. Recorded in
  [proposal 087](history/087-spotter-proposal.md#open-questions-for-the-owner).

### P5. Keep the stand-in Day 3 and the partial routine lists

- **Decided by** the owner, 2026-10-31.
- **What:** the screenshots hide Day 3's title and cut each routine card after two lines.
  The bundled routines keep the stand-in name "Day 3" and the exercises the cards show.
- **Why:** the owner's answer to the question was "whatever".
- **Source:** owner, 2026-10-31. Recorded in proposal 087's question 18 and in
  [`examples/routines.yaml`](examples/routines.yaml).

## The name

### N1. Spotter

- **Decided by** the lead, 2026-10-30.
- **What:** the first build was called Spotter, with the bundle id `dev.rxtx.spotter`.
- **Why:** the lead flagged its pick to the owner so that it could change early: "Renaming
  means a new bundle id, so better decided early".
- **Source:** the lead's report to the owner, 2026-10-31. Also geoah/rxtx.dev#112.

### N2. Substrate Spotter, with Spotter on the home screen

- **Decided** 2026-11-01, in rxtx.dev commit `11acaa78`.
- **Why:** "Every substrate app carries the family's prefix in its full name; the home
  screen keeps the short word, which iOS would otherwise squeeze." The commit does not say
  who asked for it.

### N3. Substrate Workouts

- **Decided by** the owner, 2026-11-01.
- **What:** the app is renamed Substrate Workouts as it moves into substrate. Its bundle id
  is `dev.reamde.substrate.workouts`, its Live Activity's is
  `dev.reamde.substrate.workouts.widgets`, and the core package is `WorkoutsKit`.
- **Source:** owner, 2026-11-01 ("rename the app to Substrate Workouts"); geoah/substrate#1100.

## The session screen

The options, with their screens and rationales, are in [design/](design/README.md).

### U1. A Table, close to Hevy's, for the first build

- **Decided by** the lead, 2026-10-30.
- **What:** each exercise is a card holding a table of sets (previous, weight, reps, RPE,
  a tick), as Hevy and Strong lay them out ([design/table.md](design/table.md)).
- **Why:** it is the layout of the owner's reference app, built from the teardown of the
  owner's Hevy screenshots ([research 06](research/06-app-teardown.md)).
- **Source:** [DESIGN.md §7.6](../DESIGN.md#76-the-session-cover), geoah/rxtx.dev#112.

### U2. Fix what Codex Astra's UI review found

- **Decided by** the owner, who asked for the review on 2026-10-31 ("have you done any
  reviews of the ui and ux with codex astra?"). The lead fixed every finding.
- **What:** among the fixes, Start no longer hides under the tab bar, suggested values are
  readable, the Finish summary matches what Save writes, and timed sets can be rated.
  There were also larger RPE steppers, VoiceOver labels and two-row layouts at large text
  sizes.
- **Source:** [reviews/ui-2026-10-31-astra.md](reviews/ui-2026-10-31-astra.md),
  geoah/rxtx.dev#113.

### U3. Stop copying Hevy

- **Decided by** the owner, 2026-10-31.
- **What:** design a more intuitive session screen that shows "in a nicer way what I should
  be doing, previous things etc.", with Codex Astra's help.
- **Why:** "you've copy pasted too much from the hevy screenshots, consider inovating a bit
  more."
- **Source:** owner, 2026-10-31.

### U4. No large text; changing reps, weight and RPE must be fast

- **Decided by** the owner, 2026-10-31.
- **What:** Codex Astra's three directions (Beat, Route, Bench), shown as Beat with Route's
  session map, were rejected
  ([design/large-text.md](design/large-text.md)). The second round was told to use normal
  iPhone text sizes and show several sets at once. Weight, reps and effort change in place:
  one tap to start, one tap per step, one tap to log, and no keyboard for ordinary changes.
- **Why:** "not a fan of any of these, too much large text, hard to make changes to my
  reps/weight/rpe -- try again".
- **Source:** owner, 2026-10-31; the second round's brief in
  [design/README.md](design/README.md#the-second-round).

### U5. Rows

- **Decided by** the owner, 2026-10-31.
- **What:** option C, Rows, of the four second-round options (Rows, Chips, Direct, Set
  list). Each set is one line, and the set to do now opens in place with steppers, an RPE
  strip and a Log button that says what it saves. It is the default layout; the Table stays
  as the second one (Settings > Session > Layout), a choice the lead made in
  geoah/rxtx.dev#118.
- **Why:** Rows had the fastest single changes, the clearest "do this now", and supersets
  that read top to bottom in the order they are done
  ([design/rows/](design/rows/README.md)). The owner said: "let's get fable and astra to
  review the data model, figure out if there are improvements to make it more standardized
  or flexible, then start with option c (rows) and let's get it implemented."
- **Source:** owner, 2026-10-31; [DESIGN.md §7.6a](../DESIGN.md#76a-the-rows-layout-the-default).

### U6. Rows fills in the planned RPE, and the record says so

- **Decided** with U5 and the data-model review, 2026-10-31.
- **What:** Rows shows the plan's RPE already selected, so one tap logs it. The set records
  that value as `rpeMethod: prefilled`, and the suggestion engine, PRs and exports read a
  pre-filled value as no rating ("assuming RPE 8"). Tapping any value makes it `entered`.
- **Why:** the comparison page's one open question was whether a pre-filled RPE risks
  logging an effort nobody thought about. Recording provenance keeps the one-tap log
  honest ([D1](#d1-rpe-provenance)).
- **Source:** [design/README.md](design/README.md#the-rpe-pre-fill),
  [reviews/data-model-2026-10-31/DECISIONS.md](reviews/data-model-2026-10-31/DECISIONS.md),
  [DESIGN.md §4.14](../DESIGN.md#414-rpe-provenance-and-the-pre-fill).

### U7. Any order, nothing timed but a timed set, and a three-part island

- **Decided by** the owner, 2026-11-02, after the first session on the Rows screen.
- **What:** a set is never "started" and nothing counts up for it: only a timed set (a
  plank) has Start, a running clock and Stop. Any set in any exercise opens with a tap and
  logs in any order; the suggested next set is only the one open when nothing else is.
  There is no "… is up" page after a Log. The bottom capsule becomes an island of up to
  three parts, each shown only when it applies: the rest, the next set (compact, with Log),
  and the set after it on one line. The RPE strip must fit inside the card, "+ RPE" on one
  line, no rest lines inside a superset, and the minimised session bar must not cover
  Start and must say more than a timer.
- **The lead's choices**, open to the owner: the island docks the next set only while its
  editor is scrolled out of view, so it is never on screen twice; the RPE control is five
  whole stops and a `½` toggle; the title keeps the session's length, to the minute and in
  grey ("24 min"), so it does not read as a set's timer; the minimised bar shows the rest
  beside the session's name while one runs, otherwise the next set with its values, and
  drops the session clock; passed-over sets carry no tag.
- **Why:** the owner said "I don't want to have to start a workout that is not timed … I
  want to just freely move between exercises and mark them as done", and asked for the
  island's "three parts that should be shown/hidden as they are appropriate".
- **Source:** owner, 2026-11-02;
  [DESIGN.md §7.6a](../DESIGN.md#76a-the-rows-layout-the-default).

### U8. After an out-of-order Log, "Then" follows the plan from that set

- **Decided by** the owner, 2026-11-02, answering the question the reviews of #1110 raised.
- **What:** after a set is logged out of order, the suggestion and the island's "Then …"
  line carry on with the plan from the set just logged (§4.6 rule 2), not from the set the
  plan was on. A set passed over earlier is not pulled forward: it comes back when the plan
  wraps round to it. Unchanged from #1110.
- **What stays in order:** the links of one set's chain (a second side, a drop, a cluster's
  next part, a myo mini-set) and a running timed set, which keeps the editor until it stops
  ([U7](#u7-any-order-nothing-timed-but-a-timed-set-and-a-three-part-island)). "Then …"
  no longer opens anything: [U9](#u9-then-only-shows-the-set-a-set-menu-on-every-set-and-rpe-in-the-island).
- **Why:** the owner moves freely between exercises and wants the plan to follow them, not
  to keep sending them back to what they skipped.
- **Source:** owner, 2026-11-02;
  [DESIGN.md §7.6a](../DESIGN.md#76a-the-rows-layout-the-default).

### U9. "Then" only shows the set; a set menu on every set; and RPE in the island

- **Decided by** the owner, 2026-11-02, using the pre-release built from e4cd635e.
- **What:**
  - A tap on the island's "Then …" takes the list to that set and marks it for a moment. It
    does not open it or change the open set: "I click it and it moves me to that, but I don't
    know what I need to do now. It shouldn't select the next one, it should just take me to
    it, I can select it if I want." Since it never opens anything, any set in the list is a
    target (a second side, a drop, a cluster's next part); U8's "only when it can open" goes.
  - Every done set has the `…` the open set has, and Delete set lives there on every set,
    with the undo toast in place of a question. A done set can be unticked from its `…` or
    with a tap on its green check; it opens again with its values, as Undo after a Log now
    does, and the toast logs it again.
  - The island's docked set shows its RPE, a tap opening the stops in the island, and Log is
    a check alone. **The − and + stay**: the owner, the same day, on the pre-release that
    traded them for tap-to-type numbers to fit the RPE on the values' row: "why did you
    remove the +/- from the current set island? that was useful. add it back". The name keeps
    its own line; under it the values with their steppers, the RPE and Log share one row
    where they fit, and where they do not the values take the first row and the RPE the
    second, with Log at its right edge. The steppers never give way to the RPE on any width.
  - The RPE stops in the panel carry the label "RPE".
  - "To failure" is no longer words inside the reps field. The header says it once, and the
    failure choice stays the set kind in the `…`, tagged on the logged line. What is written
    is unchanged.
- **Why:** the owner's words, from the pre-release: "Completed sets don't have … button",
  "'Delete set' should be in the set's … button", "Completed sets should have a clear/undo set
  button", "Can we fit RPE into the current workout island? Maybe make the log button just an
  icon and reduce the spacing on the other items?", "'To failure' isn't great as an
  element/ux", "On the active set panel the RPE row doesn't say this is for RPE".
- **Source:** owner, 2026-11-02;
  [DESIGN.md §7.6a](../DESIGN.md#76a-the-rows-layout-the-default).

## The data model, first draft (lead)

Taken by the lead on 2026-10-30 from the research, for the owner to overturn. They are listed
in full, each with the alternative it set aside, in
[proposal 087, "Decisions taken for the owner"](history/087-spotter-proposal.md#decisions-taken-for-the-owner),
and restated in [DESIGN.md §1.3](../DESIGN.md#13-decisions-from-the-lead). In short:

| #   | Decision                                                                                                                                                          | Still true?                                                                    |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| 1   | The package is a draft outside Mneme's tree, applied nowhere                                                                                                      | No: the app's own `~/training` ([T3](#t3-the-package-is-the-apps-own-training)) |
| 2   | Kinds `exercise`, `routine` ("Workout routine"), `session` ("Training session"), `set`; a folder is a string plus a position                                      | Yes, with `setup` added ([D4](#d4-a-setup-kind))                               |
| 3   | One record per performed set; the session embeds its ordered slots, group values stored once                                                                     | Yes                                                                            |
| 4   | Effort is `rpe` as entered, an optional `rir`, a scale tag                                                                                                        | Yes, with provenance and ranges ([D1](#d1-rpe-provenance), [D3](#d3-effort-targets-as-ranges)) |
| 5   | Load is a quantity in the unit entered, a load mode beside it, assistance as its own positive quantity                                                           | Yes                                                                            |
| 6   | Rests are [research 02 §3](research/02-training-methods.md#3-grouping)'s slots, defaults from §4.3 and §4.4, warm-up rest 45 s                                    | Yes                                                                            |
| 7   | Myo-reps use Fagerli's stop rule; each mini-set is its own record in a chain                                                                                      | Yes                                                                            |
| 8   | A drop set is a chain with no rest inside, 20 % lighter, rounded down                                                                                             | Yes                                                                            |
| 9   | Suggestions are [research 03 Part B](research/03-progression.md#part-b-specification) exactly                                                                     | Yes                                                                            |
| 10  | PREVIOUS is the last time the exercise was done anywhere, on the same setup                                                                                       | Yes; the setup became a kind ([D4](#d4-a-setup-kind))                          |
| 11  | Session RPE is optional, asked at Finish                                                                                                                          | Yes                                                                            |
| 12  | AlarmKit waits for V1.1                                                                                                                                           | Yes                                                                            |
| 13  | Keep the screen awake: a setting, on by default                                                                                                                  | Yes                                                                            |
| 14  | One `HKWorkoutBuilder` workout per session at Finish, keyed by the session id; the app never writes `health/workout`                                             | Yes                                                                            |
| 15  | V1 keeps its data on the phone; live sync is V2                                                                                                                   | No: sync is built ([T5](#t5-the-app-installs-and-upgrades-its-package-itself)) |

## The data model after the reviews

On 2026-10-31 the owner asked Fable and Codex Astra to review the data model, "figure out if
there are improvements to make it more standardized or flexible". The lead took the decisions
below from the two reports, before any real set was written. The package was still a draft,
so it stayed at version 1. The full list, with where the two reviews differed and why each
choice was made, is
[reviews/data-model-2026-10-31/DECISIONS.md](reviews/data-model-2026-10-31/DECISIONS.md). The
reports are [fable.md](reviews/data-model-2026-10-31/fable.md) and
[astra.md](reviews/data-model-2026-10-31/astra.md).

### D1. RPE provenance

`set.rpeMethod` and `set.rirMethod`: `entered`, `prefilled`, `copied`, `imported`. Tapping a
value makes it `entered`, even when it equals the plan, and equality never decides
provenance. A pre-filled value reads as no rating. **Why:** without it, history can never
tell a one-tap log from a rating (both reviews).

### D2. How a set was completed

`set.completionMethod`: `individual`, `bulk` (Finish's "mark them done as shown"),
`imported`. **Why:** a bulk completion confirms no single value (Astra).

### D3. Effort targets as ranges

`rpeMin`/`rpeMax` and `rirMin`/`rirMax` in the planned set and `set.target`, as reps already
were. **Why:** nested names can never be renamed later (Fable; Astra agreed).

### D4. A setup kind

A fifth kind, `setup`: one station at one gym, with its own load steps. `set.setup` references
it, with `mustExist`, and deleting a setup never cascades. **Why:** a string can never become
a reference once stored, and one gym's machine is not another's (both).

### D5. Relative loads

`loadPercent` and `loadReference` on the planned set; `referenceLoad` on the target.
`target.load` is always the resolved absolute load (both).

### D6. The load's meaning copied onto each set

`loadBasis`, `implements`, `laterality`, `repsBasis` and `bodyShare`. **Why:** editing the
exercise must never reinterpret history (Astra).

### D7. The full prescription on the set

`tempo`, `timerMode`, `myoStopRule`, `dropReps` and `intraSetPause` in `set.target` (Astra).

### D8. The plan on the session

`session.slots[].sets[]`: the planned sets as resolved at Start, written at Finish (Astra).
A slot with no set done is still dropped at Finish, so its plan goes with it. The Rows-build
review left that open ([reviews/rows-build-2026-10-31.md](reviews/rows-build-2026-10-31.md)),
and it is a known gap of geoah/rxtx.dev#118.

### D9. The effort scale stated

`rpeScale: rirbased` on every set the app writes. "RIR = 10 − RPE" holds only on the RIR-based
scales, not on CR10 (Astra).

### D10. Time and rest provenance

`set.timeBasis` and `set.restMethod` (Astra).

### D11. Sport

`session.sport`, using `health/workout`'s values (the app writes `strength`), plus
`sportSource`. `activityType` stays, as the Apple Health save's setting (Astra, with Fable's
"keep activityType").

### D12. `timezone`, not `timeZone`

Spelled as core's `recurring` trait and the Apple Health provider spell it (Fable).

### D13. A chain's head must exist

`set.continues` is `mustExist`, and the writer puts a chain's head before its members
(Astra).

### A1 to A3. In the app's code

The codec reads substrate's `{ref}` objects (A1). It keeps unknown fields at every level, so
an older app never erases newer ones (A2). The app writes every new field, honours D1, D3 and
D5, and never sends a session RPE of 0 as Apple Health's effort score (A3).

### Documentation and later

The mapping tables were corrected from both reports, with a code-system registry and rules for
a second writer. Conditioning groups, cardio machines, programs, derived PRs and 1RMs,
import-source preservation, fractional seconds and per-set `source` are reserved, not built.
See [`Package/README.md`](../Package/README.md#later-reserved-not-built).

### Checked after the build

A data reviewer and a UI reviewer checked the build of these decisions and of Rows. They
raised 20 findings, 19 fixed and 1 partly fixed
([reviews/rows-build-2026-10-31.md](reviews/rows-build-2026-10-31.md)).

## The build spec's own decisions

[DESIGN.md §1.4](../DESIGN.md#14-decisions-taken-in-this-spec) lists 15 smaller decisions the
spec's author (the lead) took on 2026-10-30, each explained where it lands. Among them: four
tabs; the bundled example sessions are test data, never history; an RPE keypad from 6 to 10
in half steps; a rest after an exercise's last set; timed sets that run past zero and complete
only on Stop; and Finish dropping exercises with no set done. They are open to the owner like
the rest.

## Moving into substrate

### T1. The app moves into substrate, signs in with the app authorization flow, and is reviewed

- **Decided by** the owner, 2026-11-01.
- **What:** "move this project into a pr on the substrate repo, move to option c, and
  integrate with substrate's new auth system, and rename the app to Substrate Workouts,
  let's get a build working, deploy it on rxtx apps, open pr with screenshots, and the run
  reviews with astra and fable to start fixing issues."
- **How:** the app signs in through SubstrateKit's app authorization
  ([0194](../../../../docs/decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md),
  [0202](../../../../docs/decisions/0202-an-apps-approval-sets-up-what-it-needs-and-its-token-delivers-to-its-webhook.md)).
  It asks for `declare:~/training`, `write:~/training/*`, two Health reads, and a `package`
  detail that installs `substrate.reamde.dev/health`, which the package requires.
- **Source:** owner, 2026-11-01; geoah/substrate#1100.

### T2. The kinds belong to the app, not to substrate

- **Decided by** the owner, 2026-11-01.
- **What:** "We want the new kinds to be installed and managed by the app, should not come
  with substrate". The lead had started the training kinds as a first-party catalogue under
  `kinds/`; that was reverted.
- **Why:** the kinds are the app's, and change with its releases. A first-party catalogue
  would make the server ship and version them
  ([0203](../../../../docs/decisions/0203-an-app-keeps-its-own-package-under-a-declare-scope.md)).
- **Source:** owner, 2026-11-01; 0203; geoah/substrate#1100.

### T3. The package is the app's own `~/training`

- **Decided by** the lead, 2026-11-01, in 0203.
- **What:** the package lives under the repository's own authority, so what lands is
  `<authority>/training`. On the phone the kinds are `~/training/…`, and `Rehome` swaps `~`
  for the authority at the server's edge.
- **Why:** that is where an owner's own vocabulary and imported samples already live
  ([0046](../../../../docs/decisions/0046-a-repository-owns-one-authority-chosen-at-registration.md),
  0048). The owner owns and edits it, and nothing is claimed under an authority the app
  does not own.
- **Source:** 0203; geoah/substrate#1100.

### T4. A `declare:~/<package>` scope

- **Decided by** the lead, 2026-11-01. The owner merged it with #1100 on 2026-11-02; 0203's
  status is still `proposed`.
- **What:** an app's token may apply the `package`, `bundle` and `kind` declarations of the
  one package of the repository's own authority that the scope names, and read that
  package's rows. It may not declare functions, triggers, agents, mappings or traits, and it
  may make no lossy change.
- **Why:** it is the smallest change that lets an app own its vocabulary while the owner
  approves exactly what it may touch. The alternatives failed: the request cannot carry the
  manifests (about 90 KB against a 2 KB bound on its details), a fetched URL would make the
  server dial out, and the owner rejected the first-party catalogue.
- **Source:** [0203](../../../../docs/decisions/0203-an-app-keeps-its-own-package-under-a-declare-scope.md),
  [`docs/apps.md`](../../../../docs/apps.md#keep-a-package-of-your-own).

### T5. The app installs and upgrades its package itself

- **Decided by** the lead, 2026-11-01.
- **What:** at sign-in, at launch and on returning to the foreground, the app reads the
  package's version. A missing package is installed and an older one re-applied; the same
  version needs nothing. If the repository holds a newer version, the app writes nothing,
  keeps sessions on the phone and says to update the app.
- **Source:** geoah/substrate#1100; [README.md](../README.md#signing-in-and-sync).

### T6. Sync through an outbox; never write `health/workout`

- **Decided by** the lead: the seams in the 2026-10-30 spec
  ([DESIGN.md §2.5](../DESIGN.md#25-the-seam-where-v2-plugs-in)), built in #1100.
- **What:** a persistent outbox sends the session first, then its sets heads-first, then
  Finish under `ifVersion`, then the Health link. Server routines are cached for the gym, and
  server sessions join local history. The app saves its own workout to Apple Health and never
  writes `substrate.reamde.dev/health/workout`; Substrate Sync mirrors the Health workout.
- **Why:** a session must survive being offline, and one workout must never be written twice
  (proposal 087's decision 14).
- **Source:** geoah/substrate#1100; [README.md](../README.md#signing-in-and-sync).

## After the reviews of #1100

On 2026-11-01 Codex Astra and Fable reviewed #1100, as the owner asked (T1). The lead fixed
every major before the merge; on 2026-11-02 the owner asked "Has the app been reviewed? If
so, merge the pr into main", and it merged as `d3642e82`. The fixes, finding by finding, are
in [reviews/pr-1100-2026-11-01/fixes.md](reviews/pr-1100-2026-11-01/fixes.md). The ones that
are decisions:

- **A `declare:` apply only adds.** A kind that drops a property, field, enum value, state,
  transition or trait is refused with 409, whoever added it. So is a bundle that does not
  list everything the stored bundle lists. A narrowing waits for the owner.
- **A declared bundle lists its kinds and nothing else.** `modules`, `oauth2` and `inputs` are
  refused with 403, and so are a kind's `indices` and a property's `embed`.
- **An owner's kind edit stops the app's next upgrade** (`.edited`) until the owner chooses
  "Update the Package Anyway" ([O1](#o1-update-the-package-anyway)).
- **A write the server refuses is parked**, holding only its session, and listed in Settings
  with Retry and Discard.
- **Deletes are respected both ways.** A session the server dropped after this phone sent it
  is deleted on the phone and never re-uploaded. A session deleted on the phone is purged on
  the server and never imported.
- **Each sign-in is one account**, with its own outbox, catalogue and package check. A sync
  run stops publishing once its account is no longer current.

## Open questions

### Open since #1100

The four questions the fixer left for the owner on 2026-11-01. **All four are open.**

#### O1. Update the Package Anyway

"Update the Package Anyway" lets the owner apply the app's package over their own kind edits.
Edits to labels and descriptions are replaced; additions survive, because the server refuses
removals. Keep it, or require the owner to settle the package in the console?

#### O2. The apply race, and an expected-version check

The guard reads the stored package outside the engine's transaction, and
`/vocabulary/apply` takes no expected version. An owner's edit that lands between the app's
read and its apply is not seen. Closing that needs an expected-version precondition on
`/vocabulary/apply`, a server change not made in #1100. 0203 and `docs/apps.md` state the gap.

#### O3. Tombstones apply on every server

The phone's tombstones are phone-wide, not per repository: a session deleted on the phone is
purged in every repository it reaches.

#### O4. Re-upload of sessions imported by the old build

Sessions the old build imported carry no record versions. Until this build's first sync
records them, a session the server deletes would be uploaded again, once.

### Still open from proposal 087

Proposal 087's questions 2 to 17 were not answered, apart from what later decisions settled.
Question 7 is partly settled by D4, and question 14 by T1 (the app has its own token). See
[the list](history/087-spotter-proposal.md#open-questions-for-the-owner). Questions 1 and 18
were answered (P4, P5).
