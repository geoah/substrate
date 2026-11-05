# Effort, progression and the plan around them: proposal (2026-11-05)

The owner passed on another agent's suggestions for better, more flexible workout suggestions
and asked for them to be accommodated, with one change of their own on top: "removing the reps
in reserve and replacing them with reps left will also improve the UI, as it's just the single
number that we can now have like + similar to the KG and things like that, and the other option
should be just like toggles that we can toggle or like drop downs next to them". This page says
what the app already holds for each suggestion, what each one changes, and in what order. The
first phase ships to TestFlight for the owner to try before its pull request opens.

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

- **Range and meaning.** 0 to 4, where 4 reads `4+` (the strip's 6, "could do 4 or more").
  On a timed or distance set the row reads `left` (in the tank), with the same words for
  VoiceOver ("could do 2 more", "nothing left").
- **The plan's value is pre-filled, as today.** A set whose plan names an effort opens on it,
  drawn hollow (not yet a rating). Logging it untouched logs it as `prefilled`; a tap on − or +
  makes it the lifter's rating (`entered`). A reset chip goes back to the plan's value; a set
  whose plan names none opens empty, with `+` starting at the plan's or last time's value.
- **Stored as today.** The value stays in `set.rpe` on the RIR-based scale (`rpeScale:
  rirbased`), where RPE = 10 − reps left exactly, with `rpeMethod` as today. Nothing a reader
  does changes: the suggestion, e1RM, History, the Hevy import and the coach read it as they
  do. Writing `set.rir` instead was weighed and set aside: on this scale it is the same number,
  and every reader would then have to read two fields. The target (`set.target.rpeMin` to
  `rirMax`) and the effort done (`set.rpe`) were already separate, which is the other agent's
  point about target and actual RIR.
- **Said as reps left everywhere.** `@ 8` becomes `2 left` on the lines, the island, the Log
  button, PREVIOUS and History; the plan's `RPE 7–8` becomes `2–3 left`; the suggestion's
  "assuming RPE 8" becomes "assuming 2 left". A value with a half point, from an import or an
  older entry, reads `1–2 left`. The Table layout's RPE column and sheet become Left. The
  Finish sheet's session rating stays: it is CR10, a different scale ("how hard was the
  session").

### 1b. Why the set ended, a menu beside reps left (suggestion 2)

Beside the reps-left number, a menu chip, `Why stop?`, holds three choices and none:

| Choice | Stored | Read as |
| --- | --- | --- |
| Muscles gave out | `stopReason: fatigue` | the set is evidence of what the muscles could do |
| Form broke down | `stopReason: technique` | the reps done are not a ceiling; no progression from it |
| Pain or discomfort | `stopReason: pain` | as technique, and the line shows it in orange |
| (none) | absent | the plan's reps reached, or not said |

A logged line shows the reason as a word after its values (`form`, `pain`), History shows it,
and the coach reads it: its instructions say to treat a `pain` stop by its pain traffic light.

### 1c. Ready to go up: a progression offer (suggestion 3)

An exercise is **ready to go up** when, in each of its two newest sessions (same exercise and
setup, not a deload, the newer within 28 days):

- every working set reached the top of its planned rep range (`repsMax`; a range of one count
  is its count), at one load, the same in both sessions;
- each did so with at least the planned reps left (a rating of reps left at or above the
  plan's lowest, `10 − rpeMax`). A set logged with the plan's value untouched counts as at the
  plan, and the reasoning says "as planned" rather than claiming a rating;
- no set ended for form or pain (1b), and none reached failure unplanned.

The exercise's block then opens with a card above its first working set: **Ready to go up**,
the reasoning ("Last 2 sessions: every working set reached 12 reps with 2 or more left at
105 kg"), and two buttons, **Use 107.5 kg** and **Stay at 105 kg**. The new load is one step
of the exercise's grid (no grid: no offer).

- **Use**: the first working set suggests the new load at the bottom of the range
  ("Going up from 105 kg: try 107.5 kg × 8"); the sets after it follow the in-session rule
  (03 §11.2) as now.
- **Stay**, or no answer: the suggestion keeps the load and aims at the top of the range.
- **When the offer is not met**, the history rule no longer raises the load on its own: where
  03 §11.3 would load up after one session, the app keeps the load at the top of the range
  ("Last time 105 kg × 12, top of range; try 105 kg × 12") until the offer is accepted. Going
  down, in-session changes and every other rule stay as 03 specifies them, and the engine's
  vectors still run with the automatic rule. A setting, **Progression: Ask before going up**
  (default) or **Automatic**, keeps the old behaviour one switch away.

Nothing is written for an answer: the sets logged at the new load are the record of it.

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

### 1f. The time left (suggestion 6)

The session's header adds the time left, "about 32 min left", from the pending sets: each
set's working time (today's median for that exercise from its rests and ticks, else 45 s; a
timed set's goal) plus the rest the plan gives after it (§4.7: transitions, round rests, side
switches and warm-up rests included). It is recomputed on every change, so a skipped or
extended rest moves it. Rest extensions stay −15 and +15; "Do as straight sets" stays in the
group's menu.

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

Phase 1 is training version 6 and the coach's version 4, each additive:

```yaml
# set.yaml, beside reachedFailure
stopReason:
  type: enum
  description: why the set ended before more reps, where the lifter said; absent when it
    ended at the plan's reps or nobody said
  values:
    - value: fatigue
      label: Muscular fatigue
    - value: technique
      label: Technique breakdown
    - value: pain
      label: Pain or discomfort

# routine.yaml, a slot field beside notes
cue:
  type: string
  description: one short line shown beside the set being done, such as controlled return;
    pause without bouncing
```

The app asks for training 6 as it is built against it; a repository on 5 is asked to approve
the update, as the app already does for a version behind. Phase 2 would be version 7: the
`checkin` kind and the memory's limits.

## Open questions for the owner

1. **Two sessions, or the newest alone with confirmation?** The offer waits for two; a
   beginner progressing every session may want one.
2. **Reps left 0 to 4+, or 0 to 5+?** The strip stopped at "4 or more"; a wider range helps
   high-rep accessories.
3. **The progression default.** Ask before going up, or keep the automatic rule as the default?
