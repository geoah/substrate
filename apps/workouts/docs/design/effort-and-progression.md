# Effort, progression and the plan around them: proposal (2026-11-05)

The owner passed on another agent's suggestions for better, more flexible workout suggestions
and asked for them to be accommodated, with one change of their own on top: "removing the reps
in reserve and replacing them with reps left will also improve the UI, as it's just the single
number that we can now have like + similar to the KG and things like that, and the other option
should be just like toggles that we can toggle or like drop downs next to them". This page says
what the app already holds for each suggestion, what each one changes, and in what order. The
first phase ships to TestFlight for the owner to try before its pull request opens. Codex Astra
reviewed it; [its findings](#review-codex-astra-2026-11-05) are folded into Phase 1 as written
here.

## The suggestions, against what exists

| Suggestion | What the app holds today | What is missing |
| --- | --- | --- |
| **1. Warm-up-aware supersets.** Warm-up circuits run separately; each machine's preparation sets before its working rounds; warm-ups out of working volume. | A group's warm-ups run before round 1, each member's in turn (DESIGN §4.6). Warm-ups never count as working sets and are out of volume unless a setting puts them in (§4.11). | A group of warm-up exercises (a warm-up circuit) does not run as a circuit: its sets run member by member, A, A, B, B, and the coach is told never to group warm-ups. |
| **2. Actual effort logging.** An optional "reps left" input beside load and reps. Tell muscular fatigue, technique breakdown and pain apart as reasons for stopping. | Every set can carry its effort (`rpe`, on the RIR-based scale, so RPE 8 is 2 reps left) with how it got there (`rpeMethod`: rated, pre-filled from the plan, copied). The plan's target (`set.target.rpeMin`…`rirMax`) and the effort done (`set.rpe`) are already separate fields. `reachedFailure` and `failedRepAttempted` exist. | Effort is entered on an RPE strip (6 to 10 and ½), not as reps left. Nothing records why a set ended. |
| **3. Progression suggestions.** A small load increase when every working set reached the top of its rep range at the intended reserve across two sessions, with the reasoning, accepted by the lifter. | The next-set suggestion (research 03, §11.3) is double progression from the last session alone: it raises the load by one step as soon as one set's estimate passes the top of the range, set by set, and says why in one line. | No two-session rule, no whole-exercise view, no accepting or declining: a heavier load simply appears. |
| **4. Symptoms and recovery feedback.** A quick check-in before training and the next day: soreness, joint or back discomfort, unusual fatigue, tied to the exercise. | `sessionRPE` (how hard the session felt) at Finish; set and slot notes; the coach's `limitation` memories. | No check-in, no record of discomfort, nothing tied to an exercise. |
| **5. Exercise-specific cues.** One short cue beside the set being done; the longer rationale in Markdown, expandable. | Each routine slot has `notes`, shown on the plan and on the exercise being done; as of #1197 they render Markdown and fold past five lines. | No short cue: the notes are the only place, and the whole note shows on the open exercise. |
| **6. Flexible rest and time estimates.** Predict the finish from actual set times, unilateral work, preparation and transitions; easy rest extensions; turn a superset into straight sets. | −15 and +15 on every rest, Skip, a rest picker. "Do as straight sets" (`ungroupAll`) in a group's menu. Every rest the plan implies (§4.7) and every set's actual times (`startedAt`, `restBefore`). | No estimate of the time left. |
| **7. Reviewable programme history.** Routine versions with why each changed; compare performance only across consistent setups, ranges and equipment. | A session keeps the routine's version and its planned sets as they stood; a set copies its target; `setup` keeps a gym's machine apart, and suggestions only read sets of the same setup. The coach writes a dated line in the routine's notes and a `change` memory for each change. | No list of a routine's changes with their reasons; the trends mix setups and rep ranges. |
| **Constraints.** Persistent, clinician-agreed limits, such as no failure sets, that substitutions respect. | The coach's `limitation` memories, as text. | Nothing the app can enforce. |

The other agent's "do not build" list stands: no automatic failure tests, no "junk volume"
label on easy sets, and nothing that infers medical clearance from a session without symptoms.

## Phase 1: in the TestFlight build and this pull request

### 1a. Reps left, a number with − and + (suggestion 2)

The RPE strip goes. In its place, under load and reps, the open set gets a third row built
like them: the number, `reps left`, and `−1` / `+1` under the thumb.

- **Range and meaning.** 0 to 4, where 4 reads `4+`: a capped category (the strip's 6, "could
  do 4 or more"), not a count. A hold or a distance has no reps to leave, so its row says the
  effort in the scale's words (`Hard`, `Very hard`), stepped with Easier and Harder, on the
  same stored value. A CR10 value from an import is never read as reps left.
- **The plan's value is pre-filled, as today.** A set whose plan names an effort opens on it,
  drawn hollow (not yet a rating). Logging it untouched logs it as `prefilled`. A tap on the
  hollow number confirms it as the lifter's rating (`entered`); − or + changes and rates it in
  one tap. The reset chip goes back to the plan's value, unrated; a long press on the number
  clears it, which also stops the pre-fill. A set whose plan names none opens folded, its
  steps starting from last time's rating, else 2 left. A half point from an import or an older
  entry is kept as stored and shown as `1–2` until a step lands on a whole count beside it.
- **Stored as today.** Native entry continues to use the existing RIR-based RPE encoding and
  provenance: `set.rpe` on `rpeScale: rirbased`, where 0 to 3 left is RPE 10 to 7 and `4+` is
  stored as 6, with `rpeMethod` as today. Nothing a reader does changes: the suggestion, e1RM,
  History, the Hevy import and the coach read it as they do. Writing `set.rir` instead was set
  aside for this release: readers already fall back to a rated `rir` (DESIGN §5.2), but a
  second spelling of the number the app writes buys none of them anything. Display text is
  never written back. The target (`set.target.rpeMin` to `rirMax`) and the effort done
  (`set.rpe`) were already separate, which is the other agent's point about target and actual
  RIR.
- **Said as reps left everywhere.** `@ 8` becomes `2 left` on the lines, the island, the Log
  button, PREVIOUS and History; the plan's `RPE 7–8` becomes `2–3 left`; the suggestion's
  "assuming RPE 8" becomes "assuming 2 left". A value with a half point, from an import or an
  older entry, reads `1–2 left`. The Table layout's RPE column and sheet become Left. The
  Finish sheet's session rating stays: it is CR10, a different scale ("how hard was the
  session").

### 1b. Why the set ended, a menu beside reps left (suggestion 2)

Beside the reps-left number, a menu chip, `Why stop?`, holds four reasons and, once one is
chosen, Clear reason. The reason is the lifter's report of the primary reason the set ended,
independent of whether another full rep was possible or attempted: `reachedFailure` and
`failedRepAttempted` stay as they are, and a fatigue stop is never turned into RPE 10 or a
failed rep.

| Choice | Stored | Read as |
| --- | --- | --- |
| Muscular fatigue | `stopReason: fatigue` | a reason, read with the set's effort as before |
| Form broke down | `stopReason: technique` | no evidence of capacity: it never raises a load, and e1RM leaves it out |
| Pain or discomfort | `stopReason: pain` | as technique, and the line shows it in orange |
| Other reason | `stopReason: other` | as technique; the set's notes say what (time, an interruption) |
| (none) | absent | not reported |

A logged line shows the reason as a word after its values (`Form`, `Pain`), History shows it,
and the coach reads it: its instructions say to treat a `pain` stop by its pain traffic light.

### 1c. Try a higher load: a progression offer (suggestion 3)

With **Confirm increases from earlier sessions** on (the default), a load increase that comes
from earlier sessions waits for the lifter. Where the history rule (03 §11.3, and §11.5 from
history) would raise the first working set's load, or a changed rep range would put it above
last time's, the suggestion keeps the load and aims at the top of the range, and the
exercise's block opens with a card: **Try a higher load?**, the reason, and **Use 107.5 kg**
or **Keep 105 kg**. The new load is the engine's, on the exercise's grid; with no grid, or on
plain bodyweight, there is no offer, and an assisted exercise goes up by one step less
assistance.

The reason is two sessions' evidence when it holds, else the engine's own line ("Last time
105 kg × 12 with 4 left, top of range."). The evidence offers the next step even where the
engine would not. It holds when, in each of the exercise's two newest sessions (neither a
deload, which starts the count again; the newer within 28 days of today, the older within 28
days of the newer):

- at least today's planned number of working sets at the session's heaviest working load
  reached the top of today's range (`repsMax`; an open top never qualifies), at the same load
  both times, each on today's target (the same range and effort). Lighter back-off sets
  neither count nor spoil it; drops and mini-sets never count; a left and right pair counts
  by its weaker side and its harder rating;
- each rated set left at least the planned reps (a rating at or below the target's top RPE),
  and none reached failure where the plan left reps. When a set was not rated (the plan's
  value logged untouched, a copy), the reason says so;
- none ended for form, pain or another reason (1b).

The reason then reads "All 3 sets reached 12 reps at 105 kg in both sessions, with at least 2
reps left.", or "… in both sessions. Effort wasn't rated."

- **Use**: the first working set suggests the new load at the bottom of the range ("Going up
  from 105 kg: try 107.5 kg × 8"); the sets after it follow the in-session rule (03 §11.2).
- **Keep**, or no answer: the load stays, aiming at the top of the range.
- **The answer is the session's**, kept in its journal so it survives a relaunch, with Undo.
  Nothing else is written: the sets logged at the new load are the record of it.
- **Increases from today's own sets stay automatic** (03 §11.2), as the setting's name says.
  A set that ended for form, pain or another reason never raises the load, in the session or
  from history, and e1RM leaves it out, so skipping it cannot bring back an older estimate.
- **Off**, the history rule raises loads on its own, as 03 does, and no offer shows; the
  engine's vectors run that way. A deload session shows no offer.

### 1d. Warm-up circuits (suggestion 1)

A group whose members hold only warm-up sets runs as a circuit: round 1 is each member's first
warm-up set, round 2 their second, with the group's transitions between members and its round
rest between rounds (absent, the warm-up rest). A group that mixes stays as it is: every
member's warm-ups (its preparation sets) before round 1. The coach's instructions change from
"warm-up and cool-down slots are never grouped" to "a warm-up circuit is a group of warm-up
slots; keep it apart from the working groups".

### 1e. A cue beside the set (suggestion 5)

A routine slot gets `cue`, one short line such as "controlled return; pause without bouncing".
The open set shows it under its title; the plan shows it on its card; the routine editor edits
it; the slot's notes stay the longer rationale, folded, in Markdown. The coach writes a cue
where it helps.

### 1f. The time it ends (suggestion 6)

The session's title adds when it should end, "12 min · ends ~11:42". From now, it adds what is
left of a running rest or a running timed set, then each pending row in plan order (both sides
of a unilateral set), with its working time and the rest the plan gives after it (DESIGN §4.7:
transitions, round rests, side switches and warm-up rests included; none after the last). It
also adds the mini-sets a myo-rep or rest-pause set will grow (the planned count, else 3). A
timed set works for its goal; any other set for the median of today's sets of its exercise,
else 45 s. A set is measured from the tick before it, less the rest the plan gave. The rest
measured may hold the set's own working time (DESIGN §4.8), so it is not subtracted. A sample
outside 5 s to 5 min is left out. It is an estimate of the plan at today's pace, recomputed on
every change, so a skipped or extended rest moves it. Rest extensions stay −15 and +15; "Do as
straight sets" stays in the group's menu.

## Phase 2: next

- **Check-ins (suggestion 4).** A `checkin` kind: before training (optional, one screen at
  Start) and the next day (one notification the morning after): muscle soreness, joint or back
  discomfort (where, and which exercises), unusual fatigue, each none / mild / moderate / severe,
  and a note, pointing at the session and the exercises. The coach reads them; nothing is read
  as proof of a good workout.
- **Constraints.** A memory gains structured limits the app enforces: `nofailure` (failure and
  AMRAP sets run as "stop at 2 left", the suggestion never pre-fills fewer than that), a minimum
  reps left, and movements to avoid, which the exercise picker leaves out of substitutions.
  `source: clinician` marks one agreed with a clinician; only the owner retires it.
- **Rest.** Longer extensions (+1:00) on a long press; "Do as straight sets" on the group's
  header as well as its menu.

## Phase 3: later

- **Programme history (suggestion 7).** A routine keeps `changes`, each with its date, what
  changed and why, written by the coach and the editor; the routine page lists them, and a
  session shows the change it came after.
- **Consistent comparisons.** Progress charts split by setup, rep range and equipment, and mark
  where the routine's slot changed, so a new machine or range never reads as progress.

## The package

Phase 1 is training version 6 and the coach's version 4, each additive, then training 7 and
the coach's 5 for the review (`other`, and the two descriptions below):

```yaml
# set.yaml, beside reachedFailure
stopReason:
  type: enum
  description: the lifter-reported primary reason for ending the set;
    absent means not reported, and it is independent of whether another
    full rep was possible or attempted
  values:
    - value: fatigue
      label: Muscular fatigue
    - value: technique
      label: Technique breakdown
    - value: pain
      label: Pain or discomfort
    - value: other
      label: Other reason

# routine.yaml, a slot field beside notes
cue:
  type: string
  description: a brief plain-text coaching cue for this exercise in
    this routine slot, shown beside the set being done; longer guidance
    belongs in notes
```

The app asks for training 7 as it is built against it; a repository on an earlier version is
asked to approve the update, as the app already does for a version behind. Phase 2 would be
version 8: the `checkin` kind and the memory's limits.

## Review (Codex Astra, 2026-11-05)

Codex Astra reviewed this proposal against the code after the first TestFlight build. Each
finding, and what Phase 1 does about it:

1. **"2 left" on a plank or a carry has no unit.** Taken: a hold or a distance says the effort
   in the scale's words, with Easier and Harder, on the same stored value (1a).
2. **The strip's one-tap confirmation was lost, and clearing, the empty start and half points
   were unspecified.** Taken: a tap on the hollow number rates it, a long press clears it, the
   reset chip goes back to the plan unrated, an empty set starts from last time's rating else
   2, and a half point is kept until a step lands on a whole count (1a).
3. **The storage rationale was inaccurate, and `4+` is no exact inverse.** Taken: native entry
   keeps the existing RIR-based RPE encoding and provenance, and `4+` is a capped category (1a).
4. **`stopReason` must be independent of failure, and absent must mean not reported.** Taken:
   `fatigue` is "Muscular fatigue" and the description is Astra's (training 7).
5. **An honest escape choice.** Taken: `other`, "Other reason", with the set's notes for the
   detail, read as no evidence of capacity, and the menu's empty choice is "Clear reason". No
   `time` value.
6. **Stop reasons must reach the evidence, not only the offer.** Taken: a form, pain or other
   stop never raises a load, in the session or from history, and e1RM leaves it out; a left
   and right pair takes the limiting side's reason (1c).
7. **A pre-filled target proves no readiness.** Taken: when a set was not rated, the reason
   says "Effort wasn't rated." instead of claiming the reps left.
8. **Comparable sessions and sets.** Taken: the evidence needs today's planned number of sets
   at one load, on today's range and effort, in both sessions; drops and mini-sets never
   count; a left and right pair counts by its weaker side and its harder rating, as the
   suggestion already reads it. Different top and back-off loads count the top set's alone.
9. **Recency, deloads and load modes.** Taken: the older session within 28 days of the newer,
   a deload among the two newest starts the count again, a deload session shows no offer, and
   the exercise's load space gives the next setting: less assistance, more added load, and
   none for plain bodyweight.
10. **The strict gate could stall progression, and its setting said more than it held.**
    Taken: the offer shows whenever the engine would raise the load from earlier sessions, a
    new range included, and the evidence strengthens its reason or offers the step itself.
    The setting is "Confirm increases from earlier sessions"; increases from today's own sets
    stay automatic. The answer is kept in the session's journal.
11. **The card's copy, and tests for the shipped default.** Taken: "Try a higher load?", "Use
    107.5 kg" and "Keep 105 kg"; `ProgressionTests` hold the evidence, the gate, the engine's
    offer, the deload reset and the setting. The research vectors still run on the automatic
    rule.
12. **The cue's owner and persistence.** Taken: the description is Astra's. A session already
    reads its cue from the routine as it was at Start, which it keeps, so a routine changed or
    deleted mid-session moves nothing; a finished session does not show cues, so there is no
    `session.slots[].cue`.
13. **The layout budget.** Already so: reps left is its own row under load and reps, Why stop?
    sits beside it, the cue goes under the title and wraps, and the finish time is the title's
    second line. In the island, reps left is a button beside load and reps that opens its
    steps and Why stop? on a row of their own.
14. **A warm-up circuit's order and its rests must change together.** Already so in the first
    build (DESIGN §4.6, §4.7): rounds and a dedicated rest branch, classified from the group's
    whole plan, so finishing its working sets never turns a mixed group into a circuit.
15. **Timestamps do not measure working time.** Taken: a set's time is the gap less the rest
    the plan gave, never the rest measured; a running rest and a running timed set count what
    is left; mini-sets count as planned; and the title says "ends ~" (1f).
16. **Scope.** Agreed: check-ins, enforced constraints and programme history stay in later
    phases, and a pain stop is not a check-in. The offer no longer replaces the progression
    policy: it confirms the engine's own increases.

## Open questions for the owner

1. **Reps left 0 to 4+, or 0 to 5+?** The strip stopped at "4 or more"; a wider range helps
   high-rep accessories.
2. **The progression default.** Confirm increases from earlier sessions, or keep the automatic
   rule as the default?
