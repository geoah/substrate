# Spotter app teardown: what the app must do in the gym

> **Where this came from.** Written on 2026-10-30 in the owner's private repository, as `research/workouts/`, for
> the app then called Spotter (Substrate Workouts now); the version here is the one the
> app's second build used (the private repository's #118). It was carried into this repository on
> 2026-11-02 as written, with two changes. The links resolve inside this repository. The
> draft kind paths `mneme.example.org/training/<kind>` now read as the app-owned
> `~/training/<kind>` they became (decision record [0203](../../../../docs/decisions/0203-an-app-keeps-its-own-package-under-a-declare-scope.md)). Where the
> text names the package home it recommended, a Mneme package `mneme.example.org/training`,
> that name is left as written: the home changed later, and the reasons are in
> [decisions.md](../decisions.md). Mentions of Mneme, private repository paths or
> `apps/apple/spotter` describe the plan at the time. The package now lives in
> [`Package/`](../../package/README.md), and the app's spec is [DESIGN.md](../../DESIGN.md).

Research slice 06. Date: 2026-10-30.

The question: what must Spotter do, screen by screen, to be good in the gym? This document reads the owner's 13 Hevy screenshots in detail, checks what Hevy does behind them, surveys other strength apps, and turns all of it into a prioritised feature list, the flows the app needs, the pain points to avoid, and a translation of each Hevy pattern into iOS 27 Liquid Glass.

It builds on `/tmp/spotter-substrate-main/docs/plans/health-data/02-workouts.md` (dated 2026-10-08), which covers platforms, APIs, file formats, exercise taxonomies and metrics. That document has almost nothing on app UX. Where this one leans on it, it says so by path and section.

How to read the evidence markers:

- **[web]** checked today on the web or in a primary artefact (vendor help centre, App Store listing, Apple documentation).
- **[sdk]** checked in the installed iOS 27.2 SDK `.swiftinterface` or header files.
- **[src]** checked in source code or manifests on this machine.
- **[shot]** seen in one of the owner's screenshots. Where the reading is an inference from the picture, it says "inferred".
- **[recalled]** from memory and not re-checked today.
- **[unverified]** tried and could not confirm, or the only source is secondary or a competitor's marketing.

## 0. Findings that shape the design

1. **The rest timer is the product.** Every app checked for it (Hevy, Strong, Boostcamp, Fitbod, Liftosaur, Setgraph) starts it on "set done" [web], and the loudest complaints are about timers that drift, freeze, fire while the app still shows time left, or sit at 0:00 after rest ends ([shot] and App Store reviews, section 2.8). The timer must be computed from an absolute end time and shown by system views that tick on their own.
2. **Hevy has no notion of rest inside a superset versus after it.** Each exercise carries one rest value. The documented workaround is to set rest off on all but the last exercise of the group [web]. The owner asked for exactly the thing Hevy lacks. Of the apps checked, only Liftosaur documents two values: a global superset rest, overridden per set in the program text [web: Liftoscript docs]. Drop sets suppress Hevy's timer by a hidden rule [web].
3. **Phone and watch sync is where workout apps lose data.** It is the most common topic in recent low-rated reviews: 90 of the 189 Strong reviews rated 3 stars or less mention the watch or sync, and 8 of Hevy's 11 [web: App Store reviews, section 2.8]. Fitbod's own help centre says its watch timer pauses when the screen dims [web]. Spotter should have one source of truth on the phone and treat any watch surface as a remote.
4. **Myo-reps are rare as a native set type.** RP Hypertrophy has "myorep" and "myorep match" set types logged as total reps [web]. MacroFactor Workouts has an "M" myo set that groups several entries into one set [web]. Liftosaur expresses myo-reps in its program syntax [web]. Two small apps, OpenLift and Better Reps, also have them [web: vendor blog, App Store]. Hevy, Strong, Boostcamp, Alpha Progression and Setgraph do not list them [web].
5. **Progression hints split into two schools.** Loggers (Hevy, Strong, Setgraph) show what you did last time and let you beat it [web]. Coaches (Alpha Progression, RP, Fitbod, Gravl, Liftosaur) prescribe the next load and reps [web]. The owner wants hints from both the current session and past sessions, which is the coach school, shown in the logger's calm way. Alpha Progression already does both: its per-set target uses earlier workouts and the sets done today, and changes from set to set [web: App Store].
6. **iOS 27 gives Spotter most of Hevy's chrome as system parts**: a tab bar accessory for the in-progress bar, glass toolbars, menus that morph from their button, inset sheets, Live Activities on the Lock Screen, Dynamic Island and Apple Watch, and drag reordering outside `List` [sdk].

## 1. The Hevy screenshots, screen by screen

The 13 images, in the order the owner listed them:

| #  | File (suffix)   | Shows                                                     |
| -- | --------------- | --------------------------------------------------------- |
| 1  | `df16821d….png` | Workout tab with routines and the in-progress bar         |
| 2  | `f9b17250….png` | "You have a workout in progress" dialog                   |
| 3  | `a1d6ebb9….png` | Log Workout: header, stats, timed sets, reps and RPE sets |
| 4  | `1ffa0cbd….png` | A timed set running                                       |
| 5  | `b3255080….png` | Exercise menu and the Superset badge                      |
| 6  | `9125b25f….png` | A completed set and the rest timer bar                    |
| 7  | `8c5a842a….png` | Warm-up set, drop set, RPE keypad, Disconnected banner    |
| 8  | `6ac9b643….png` | RPE 6 selected                                            |
| 9  | `c00dc90f….png` | Set type sheet                                            |
| 10 | `a1fec1bc….png` | Per-exercise rest timer picker                            |
| 11 | `0e071c5f….png` | Reorder exercises                                         |
| 12 | `2b870607….jpg` | Live Activity and the "Rest time is over!" notification   |
| 13 | `36b12902….png` | Apple Watch showing the rest countdown                    |

Images 1 to 11 are one session: duration reads 11 s at 13:42 and 13 min 48 s at 13:56, which agrees with the clock [shot].

### 1.1 Workout tab (image 1)

- The large title is "Workout" with a chevron. The chevron opens a menu that switches between the Workout page and Hevy's paid "Trainer" page [web: Hevy help "How Hevy Trainer Settings Work"]. A yellow "PRO" badge sits top right [shot].
- A full-width "Start Empty Workout" row with a plus icon comes first [shot].
- A "Routines" header has a folder-with-plus button. Under it are two half-width buttons, "New Routine" and "Explore" (the routine library) [shot].
- Routines sit in a collapsible folder row: a disclosure triangle, the folder name with a count in brackets ("Full Body 3x Week (Back-Friendly) (4)"), and a "…" menu [shot].
- Each routine is a card: bold title, a "…" menu, the exercise names joined by commas and truncated at two lines, then a full-width blue "Start Routine" button. Each exercise name carries its equipment in brackets: "Leg Press (Machine)", "Hip Thrust (Barbell)" [shot].
- **The in-progress bar** floats above the tab bar: a chevron-up button, a green dot, "Workout 11s" (elapsed time), the current exercise name ("Leg Press (Machine)"), and a red trash button at the right [shot]. The trash discards the session from the home screen in one tap.
- The tab bar is a Liquid Glass bar with Home, Workout and Profile [shot]. One recent App Store review praises "the liquid glass on the app" [web].
- The Dynamic Island in images 1 to 11 shows another app's activity, a countdown from 17m to 3m with a green check. Hevy's own island view is not visible in these screenshots [shot, inferred].

### 1.2 The conflict dialog (image 2)

Tapping Start on another routine while a session is open shows a centred alert [shot]:

- Title: "You have a workout in progress"
- Message: "If you start a new workout, your old workout will be permanently deleted."
- Buttons, stacked: "Resume workout in progress" (filled blue, the default), "Start new workout" (red text, destructive), "Cancel".

There is no "save the old one first" option. Starting a new session can only destroy the old one. Hevy allows one active session at a time [shot, inferred].

### 1.3 Log Workout: header, stats strip, exercise card (images 3, 5, 6)

- **Toolbar**: a round chevron-down button (minimise back to the tab view, leaving the in-progress bar), the title "Log Workout", a round alarm-clock button, and a blue "Finish" button [shot]. The clock button opens extra tools, including a countdown "Timer" [web: Hevy help "The duration exercise inline timer goes up and not down, why?"].
- **Stats strip**: "Duration" (blue, tappable), "Volume" in kg, "Sets", a front and back muscle figure that fills in blue as muscles are trained, and an "×" button whose purpose the screenshots do not show [shot]. Tapping the duration pauses or resumes the workout timer; the watch app cannot pause [web: Hevy help "How to I adjust duration and pause a workout?"].
- A thin blue line under the header grows during the session: almost nothing at 1 set and 1 min 26 s, about an eighth of the width at 11 sets and 13 min 27 s [shot]. Hevy does not document it. A sets-done fraction is unlikely, since 11 sets would then be about an eighth of the plan; elapsed time fits better [inferred, unverified].
- **Exercise card**: a thumbnail with the target muscles in red, the name in blue (it opens the exercise's history, charts and records [web: Hevy help "Set Records vs Personal Records"]), and a vertical "…" menu. Under the name are two chips: "Rest: 15s" or "Rest: OFF" or "Rest: 1min 30s" with a stopwatch icon, and "Note" with a note-plus icon [shot].
- **Set grid columns change with the exercise type** [shot]:
  - timed exercise (Plank): SET, PREVIOUS, TIME, and a check column;
  - reps-only (Crunch): SET, PREVIOUS, REPS, RPE, check;
  - weighted (Chest Press): SET, PREVIOUS, KG (with a barbell glyph), REPS, RPE, check.
- **Ghost values**: the input fields show grey placeholder numbers ("15", "35"). Ticking the check commits them as logged values in black bold text [shot]. Hevy calls these the routine values, separate from PREVIOUS; saving a workout rewrites them from what was logged unless "Update Routine Values" is turned off [web: Hevy help "Previous Workout Values Vs. Routine Values"]. Tapping a PREVIOUS value to copy it into the row is not in Hevy's help [unverified].
- "+ Add Set" is a full-width grey button under each grid [shot].

### 1.4 Timed sets (images 3 and 4)

- A timed row shows PREVIOUS "01:02", a blue play button, and an input pre-filled "01:02" in grey [shot].
- Tapping play turns it into a pause button and the field counts **up** in blue: "00:02" two seconds in [shot]. Hevy's help confirms the play button is a stopwatch, not a countdown to the target, and that users expect otherwise often enough to need an article. For a countdown, Hevy points to the clock button's separate Timer, then asks you to type the time into the set by hand [web: Hevy help "The duration exercise inline timer goes up and not down, why?"].
- The completed plank logged "01:01" against a previous of "01:02" [shot].

### 1.5 A completed set and the rest timer bar (image 6)

- A completed row turns light green, its values turn black and bold, the inputs lose their borders, and the check becomes a filled green square [shot].
- The rest bar docks at the bottom of the screen: a thin blue progress line that shrinks, then "-15", a large "00:11", "+15", and a blue "Skip" [shot]. The plank's rest was 15 s, so ticking set 1 started it [shot].
- ±15 changes only the running countdown; changing the exercise's rest needs the Rest chip [web: Hevy help "Rest Timer & Default Rest Timer"].

### 1.6 Warm-up and drop sets, PREVIOUS with RPE, the RPE keypad, Disconnected (images 7 and 8)

- **Set labels**: "W" in amber for warm-up, "1" and "2" for working sets, "D" in blue for a drop set. Only normal sets get a number; the drop set after set 2 is not numbered [shot].
- **PREVIOUS carries RPE**: "25kg x 15", "35kg x 15", "35kg x 13 @ 9.5 rpe", "35kg x 8 @ 9.5 rpe" [shot]. Hevy documents the format ("50lbs x 10 @ 8.5 RPE") [web: Hevy help "RPE vs RIR"]. The rows line up by position: the D row shows last session's drop set [shot, inferred].
- **The warm-up started the full rest timer**: the W set is ticked and the bar shows 01:22 of the exercise's 1 min 30 s [shot]. There is no separate warm-up rest in Hevy's settings list [web: Hevy help "Workout Settings Preferences"].
- **Volume counts warm-ups**: the strip reads 375 kg, which is the single warm-up set (25 × 15); the other sets that day were bodyweight or timed [shot, inferred]. Hevy has a setting for whether warm-ups count toward sets, volume and records [web: Hevy help "Workout Settings Preferences"].
- **The RPE keypad replaces the keyboard** [shot]:
  - a "?" help button, a large value ("0" until chosen), "Select RPE", and "Slide or tap to rate your effort";
  - a slider with stops 6, 7, 7.5, 8, 8.5, 9, 9.5, 10 (no 6.5), matching the values Hevy's API accepts (`02-workouts.md` section 1.9);
  - with 6 chosen: "6", "Moderate Effort", "Could do 4 more reps";
  - a right-hand column: a keyboard-switch button, "+", "−", and a large green check.
- Hevy's scale stops at 6 on purpose: values below are "incredibly difficult to estimate" [web: Hevy help "How to Use RPE"]. Each value maps to reps in reserve: 6 is 4+, 7 is 3, 7.5 is 2 or 3, 8 is 2, 8.5 is 1 or 2, 9 is 1, 9.5 is "maybe 1", 10 is none [web: same article]. Hevy has no separate RIR field [web: Hevy help "RPE vs RIR"].
- The rest timer keeps counting while the keypad is open (01:22, then 01:16) [shot].
- **"Disconnected"**: a strip under the toolbar with a spinner and the word "Disconnected" [shot]. Logging carries on underneath it: the warm-up was ticked and rated while it showed [shot]. Hevy does not document the banner; it probably reports the Apple Watch live sync link or the server connection [unverified].

### 1.7 Set type sheet (image 9)

Tapping a set's label opens a bottom sheet titled "Select Set Type" [shot]:

- "W Warm Up Set", "1 Normal Set", "F Failure Set" (F in red), "D Drop Set" (D in blue), each with a "?" help button;
- "Remove Set" in red with an "×".

Hevy's rules for these types [web: Hevy help "Set Types in Hevy Explained"]:

- a drop set is "reps until failure, then drop the weight … without rest"; the rest timer "will not start if the next set is a dropset";
- for a failure set, log the last completed rep; zero reps cannot be entered.

### 1.8 Exercise menu and the Superset badge (image 5)

- An exercise in a superset carries a purple "Superset" capsule under its name [shot]. Each superset in a workout gets its own colour [web: Hevy help "The Complete Guide to Supersets"].
- The "…" menu opens a bottom sheet: "Reorder Exercises", "Replace Exercise", "Add To Superset", and "Remove Exercise" in red [shot]. Other documented entries are "Remove From Superset" and "Add Warm Up Sets" [web: Hevy help, supersets and warm-up calculator articles].

### 1.9 Per-exercise rest picker (image 10)

- Tapping the Rest chip opens a sheet titled "Rest Timer" with the exercise name ("Chest Press (Machine)") as subtitle, a sliders button top right, a wheel in 5-second steps (1min 15s, 1min 20s, …), and a blue "Done" [shot].
- The range is 5 s to 5 min, or off [web: Hevy feature page "workout rest timer"].
- A new default rest applies only to exercises that have never had a rest set by hand [web: Hevy help "Rest Timer & Default Rest Timer"].

### 1.10 Reorder (image 11)

"Reorder Exercises" collapses the session to a list of thumbnails and names; one row ("Bird Dog") is lifted with a shadow while dragged [shot]. Sets are hidden while reordering [shot].

### 1.11 Live Activity and the rest-over notification (image 12)

On the Lock Screen or in Notification Centre [shot]:

- top row: the Hevy glyph, "Workout", and elapsed time "16 min, 22 secs";
- a round exercise thumbnail, "Chest Press (Machine)", and "Next: set 2 of 4 (35 kg x 15 reps)";
- a thin progress line, then "-15s", "0:00", "+15s", and a blue "Skip".

Under it is a notification marked "TIME SENSITIVE": "Hevy", "Rest time is over!", "1m ago" [shot]. Time Sensitive is a notification interruption level apps can set (`UNNotificationInterruptionLevel`, `timeSensitiveSetting`) [sdk].

Three details matter:

1. **The activity is stale.** A minute after rest ended it still shows 0:00 with -15s, +15s and Skip, instead of switching to "do set 2 now" [shot].
2. **Numbering disagrees with the app.** "Set 2 of 4" is the row the app labels "1": the activity counts rows including the warm-up, the app numbers only working sets [shot].
3. Hevy says the activity can also mark a set done, shows sets done in the current exercise, and lets you adjust rest in 15-second steps [web: Hevy help "How to Use Hevy's Live Activity"]. No complete-set button is visible during rest [shot].

### 1.12 Apple Watch (image 13)

The watch shows an "×", "Hevy", a blue timer glyph, "0:33" in blue, and an "Open on iPhone" button [shot]. This is most likely the iPhone Live Activity mirrored to the watch, not Hevy's own watch app:

- Apple's guide says Live Activities appear "on a paired Mac or Apple Watch", and the app chooses whether tapping one on the watch opens the iPhone app or the watch app [web: Apple, "Displaying live data with Live Activities"].
- Without a watch-specific layout, watchOS reuses the Dynamic Island's compact leading and trailing views, which fits a glyph on the left and a time on the right [recalled].
- An app opts into its own small watch layout with `supplementalActivityFamilies([.small])`, and a view reads `activityFamily` to tell `.small` from `.medium` [sdk, WidgetKit, iOS 18+].

Hevy also has a real watch app with "Sync Routines" and an "Apple Watch Live Sync" toggle [web: Hevy help "Hevy Apple Watch Sync Issues"].

### 1.13 Small inconsistencies not to copy

- Four duration formats on screen at once: "11s", "2min 10s", "16 min, 22 secs", "0:33" [shot].
- Set numbering: the app shows W, 1, 2, D; the Live Activity says "set 2 of 4" for the row labelled 1 [shot].
- A stale 0:00 Live Activity after rest ends [shot].
- A warm-up starts the full working rest [shot].
- Warm-up volume counts toward session volume in the owner's settings; it is a Hevy setting whose default is not documented [shot, inferred; web].
- One-tap discard from the home screen, next to the bar you tap to resume [shot].

## 2. Survey of other strength apps

Apps checked: Hevy, Strong, Boostcamp, Alpha Progression, RP Hypertrophy, Fitbod, Liftosaur, Gravl, Setgraph, MacroFactor Workouts, OpenLift, Better Reps. Vendor pages are first-party claims; competitor comparisons are marked.

### 2.1 Progression hints

| App                 | What the lifter sees                                   | Basis                                                                                                                                                                                    |
| ------------------- | ------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Hevy                | PREVIOUS column per row; ghost values from the routine | last session, "any workout" or "same routine" [web]                                                                                                                                      |
| Hevy Trainer (paid) | suggested weights                                      | raises load once the top of a rep range is hit on all sets [unverified: competitor's page]                                                                                               |
| Strong              | previous values                                        | none prescribed [web: App Store]                                                                                                                                                         |
| Setgraph            | previous values, a swipe to repeat a set               | none prescribed [web: App Store]                                                                                                                                                         |
| Alpha Progression   | "the weight and reps to go for" before every set       | earlier workouts and the sets done today, so targets change set to set; optional RIR; "closest weight below" your plates [web: App Store, vendor blog]                                   |
| RP Hypertrophy      | target weight and reps per set, set count per week     | weight up "a few percentage points" a week; adds a rep when the next weight step is too big; sets from pump, soreness and workload ratings [web: RP help "Hypertrophy App Progressions"] |
| Fitbod              | full prescription per session                          | history, RIR 0 to 4+, periodic "Max Effort" days; deliberately varies intensity [web: Fitbod help "How does Fitbod decide…"]                                                             |
| Gravl               | next session prescription                              | estimated 1RM from history; reps before weight; easy or hard set rating [unverified: secondary reviews]                                                                                  |
| Liftosaur           | targets from a program script                          | built-in linear (`lp`), double (`dp(5lb, 8, 12)`) and rep-sum (`sum(30, 5lb)`) progressions, or custom code [web: Liftoscript docs]                                                      |
| Boostcamp           | program targets                                        | Pro "Auto Progression" holds the weight when logged RPE is above target [web: Boostcamp RPE page]                                                                                        |

What users say about hints:

- A paying Hevy user wants "more interactive recommendations when it comes to increasing weights or reps" [web: App Store review, 2026-10-19].
- The same user asks for **input validation**: one "500" typed for "50" spoils records, charts and future suggestions [web: same review].
- Fitbod's suggestions are criticised as erratic ("13 reps at 50 pounds", then "15 reps for 40 pounds" days later) [unverified: review aggregator].
- Strong's 6.0 update pre-filled today's sets with real values, so users had to delete inputs one by one [web: App Store reviews, 2025-05]. Hevy's grey ghost values avoid this [shot].

### 2.2 Plate and warm-up calculators

- **Hevy**: a "Calculator" button appears above the keyboard when you type a barbell, EZ-bar or short-bar weight. It shows which plates to load, accepts custom bars and plates, and suggests the closest weight it can build ("Closest possible weight is 135kg") [web: Hevy plate calculator page, help article]. The warm-up calculator inserts percentage sets from the exercise menu, with plate and dumbbell rounding [web: Hevy help]. The default percentages are only in an image; the settings article mentions a 40% set [web], and secondary sources say 40% × 5, 60% × 5, 80% × 3 [unverified].
- **Liftosaur**: shows "plates per side" inside the set row, and rounds every target to the bar, plates and fixed weights of the gym you are in, with one equipment list per gym [web: Liftosaur features].
- **Alpha Progression**: plate calculator with "the closest weight below if your plates don't add up"; warm-ups added automatically, scaled to the exercise [web: App Store].
- **Boostcamp**: plates per side with custom bars and plates, "plus generated warm-up sets" [web: Boostcamp rest timer page].
- **Fitbod**: auto warm-ups only when the equipment allows a useful ramp; none for bodyweight, band, core, assisted or timed work [web: Fitbod help "Warm-Up Sets"]. "Normalize Weights" picks one weight for all exercises in a superset so you do not change plates [web: Fitbod help "Circuits & Supersets"].
- **RP**: tells users not to log warm-ups at all, because the app treats every logged set as a working set [web: RP help "Warm-ups and Starting Weights"].
- **Strong**: has both calculators [web: App Store].

### 2.3 PR badges and exercise history

- **Hevy** separates personal records from set records [web: Hevy help "Set Records vs Personal Records"]:
  - personal records depend on the exercise type: heaviest weight, best e1RM, best set volume and best session volume for weight-and-reps; best time for duration; most reps for bodyweight;
  - a PR triggers a live banner during the session (a setting with its own volume) and a gold medal on the set;
  - set records are a per-rep-count table of heaviest weight with no banner.
- **Hevy's e1RM** divides the load by a rep-to-percentage table (10 reps is 75%, 30+ reps is 50%) [web: Hevy help "Understanding Your Estimated One-Rep Max"]. The prior research lists the Epley and Brzycki formulas and RPE-adjusted tables (`02-workouts.md` section 5).
- **Charts**: Hevy has "graphs for every exercise" [web: App Store]; Strong "graphs for Volume and 1RM Progression" [web: App Store]; Liftosaur max weight, volume and e1RM per exercise, long-press a point to see the workout behind it [web: Liftosaur features]; Setgraph "celebrates it the moment you log the set" [web: App Store].
- **Assisted exercises** earn PRs for reps only and ignore the assistance load, so a set with less assistance earns nothing while more reps with more assistance earn a PR [web: Hevy help "Set Records vs Personal Records"]. PR kinds must match the exercise type.

### 2.4 Supersets

- **Hevy**: built from the exercise menu ("Add To Superset"), any number of exercises, one colour per group. "Smart Superset Scrolling" moves the screen to the next exercise in the group after each tick and back to the first after the last [web: Hevy help "The Complete Guide to Supersets"]. Rest stays per exercise. To get "no rest inside, rest after the round", you "set a rest timer after the final exercise" [web: Hevy help "Circuits, Intervals, and Supersets Explained"] and keep the timer off for movements done back to back in a superset [web: Hevy feature page "workout rest timer"].
- **Strong**: "Supersets/Grouped Exercises" [web: App Store]. One reviewer: "it doesn't seem like it should be that hard to program a superset that has multiple exercises and then a rest timer at the end! is that not the point of a superset???" [web: App Store review, 2025-12-19].
- **Fitbod**: groups have their own heading with "Log All Exercises", "Duplicate", "Normalize Weights", "Unbundle"; long-press drags exercises in and out of a group [web: Fitbod help "Circuits & Supersets"]; the RIR prompt waits until every set in the group is done, not each round [web: Fitbod help "Reps in Reserve"].
- **Liftosaur**: `superset: A` in the program text; the screen moves to the next exercise after every set [web: Liftosaur docs and features]. Sets in a superset use a global "superset default rest timer" (Me → Timers) unless a per-set timer overrides it. With the default at 15 s, `Squat / 3x8 / superset: A`, `Deadlift / 3x8 / superset: A`, `Bent Over Row / 3x8 180s / superset: A` rests 15 s after squat and deadlift and 180 s after the row [web: Liftoscript docs].
- **RP Hypertrophy**: no superset function; do the sets back to back and use notes [web: RP help "Logging Weights, Reps, Sets"].

Only Liftosaur documents a separate "rest between exercises in the group" and "rest after the round", and its in-group value is one global setting, not one per group. Hevy and RP have no such pair, Fitbod's help does not mention rest for groups, and the Strong reviewer above could not find one [web]. Spotter should have both, set per group in the routine.

### 2.5 Myo-reps and myo-rep match

The method: an activation set near failure, then mini-sets of 3 to 5 reps at the same load, 20 to 30 s apart (or 5 to 6 deep breaths), until you can no longer get 3 reps. The term is credited to Borge Fagerli [web: Barbell Medicine "Myo-reps"]. Stop rules vary between coaches [web: secondary sources]. In **myo-rep match**, after the first set you do as many mini-sets as needed to match its reps, with the same short rests [web: RP help].

| App                                                  | Native?   | How                                                                                                                                                                        |
| ---------------------------------------------------- | --------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| RP Hypertrophy                                       | yes       | per-set type "regular", "myorep" or "myorep match"; 5 to 15 s pauses; log the total reps in one row [web: RP help]                                                         |
| MacroFactor Workouts                                 | yes       | set type "M": "multiple brief rest-pause efforts at the same weight … grouped together" as one set; drop sets ("D") are grouped the same way [web: MacroFactor help 317]   |
| Liftosaur                                            | by syntax | `Bench Press / 1x12 20s 60%, 5x5 20s 60%`: separate sets with 20 s rest [web: Liftoscript docs]                                                                            |
| OpenLift                                             | yes       | activation set and mini-sets shown as one group, each mini-set labelled with its rest ("+15s"); default 3 mini-sets of 4 reps, 15 s apart [web: vendor blog, staging site] |
| Better Reps                                          | yes       | a "Myo Set Timer" [web: App Store]                                                                                                                                         |
| Hevy, Strong, Boostcamp, Alpha Progression, Setgraph | no        | set types listed are warm-up, normal or working, failure, drop [web]                                                                                                       |

Two logging shapes exist: one row with total reps (RP), or one set with child entries (MacroFactor, OpenLift). The second keeps the mini-set detail, which a next-set suggestion needs ("last time 15 + 5 + 4 + 3").

### 2.6 Rest timer UX

- **Start**: automatic on "set done" everywhere checked: Hevy, Strong, Boostcamp, Fitbod, Liftosaur, Setgraph [web].
- **Duration source**: Boostcamp lets you set it per exercise in 5-second steps up to 60 min, or off; the per-exercise value beats the default [web: Boostcamp rest timer page]. Liftosaur takes rest from the program, per set if needed [web].
- **Adjusting**: Hevy and Hevy's Live Activity use ±15 s and Skip [shot, web]. Fitbod: a change during a workout affects only the current timer [web: Fitbod help "Rest Timer"].
- **Off the screen**:
  - Live Activity on the Lock Screen and in the Dynamic Island: Hevy, Boostcamp, Alpha Progression, Setgraph, Liftosaur [web], and Strong only since 6.5.0 ("New: Live Activity and Dynamic Island support for rest timers", released 2026-08-12 per the page's version history markup) [web: App Store]. A Strong reviewer who had paid for a lifetime licence left for Hevy over this one feature [web: App Store review, 2026-04-24].
  - Liftosaur adjusts rest "on the lock screen, through headphones or on Apple Watch", and its notification names the next set and the plates [web: Liftosaur features].
- **Sound**: Hevy offers five timer sounds and separate volumes for the timer, the set tick and the PR alert [web: Hevy help].
- **Failures**: Fitbod's watch timer "appears frozen" because it does not support Always On [web: Fitbod help "Apple Watch"]. Strong reviewers report a push saying time is up while the app still shows the timer running, timers stopping at random, and pause not pausing the rest timer [web: App Store reviews, 2025-11 to 2026-01]. One Hevy reviewer says tapping the timer icon scrolls a long session to the top [web: App Store review, 2026-10-16].

### 2.7 Apple Watch companions

| App               | Watch role                    | Notes                                                                                                                                                                                     |
| ----------------- | ----------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Hevy              | companion with "Live Sync"    | log sets, rest timer, heart rate [web: App Store]; cannot pause from the watch [web: help]; recent reviews report unpairing and sync failures on iOS 27 [web: App Store reviews, 2026-10] |
| Strong            | "with or without your iPhone" | [web: App Store]; the 2025 6.0 rewrite made the watch the control point and drew many reviews about lost sets, forced watch start and a separate watch sign-in [web: App Store reviews]   |
| Fitbod            | companion only                | start, swap exercises and save on the phone; log sets, rest and heart rate on the watch; no Always On [web: Fitbod help]                                                                  |
| Liftosaur         | full logging (paid)           | Digital Crown input, AMRAP and RPE prompts, timed sets, heart rate [web: features]                                                                                                        |
| Gravl             | full logging (paid)           | [unverified: secondary]                                                                                                                                                                   |
| Setgraph          | hand-off                      | "Start a session on iPhone and Apple Watch takes it over" [web: App Store]                                                                                                                |
| Boostcamp         | none                          | "Boostcamp doesn't have an Apple Watch app today"; the Lock Screen activity is the substitute [web: Boostcamp rest timer page]                                                            |
| Alpha Progression | none                          | "does not offer an equivalent dedicated watch app" [web: vendor blog]                                                                                                                     |

### 2.8 What reviewers and users complain about

Read from the 500 most recent US App Store reviews each for Hevy and Strong (fetched today; Hevy's span 2026-10-13 to 2026-10-29, Strong's 2025-05-20 to 2026-10-28), Fitbod's help centre, and secondary roundups [web]:

1. **Watch and phone disagree and data is lost.** Examples:
   - "the app will regularly lose sets you have already saved" (Strong, 2026-05-26);
   - "outdated watch data overwrote the data on my phone" (Strong, 2026-03-03);
   - "Watch had all sets completed, App did not … I guess it chose the App's version" (Strong, 2026-06-25);
   - "every other day watch says app is uninstalled" (Hevy, 2026-10-14).
2. **Being forced to drive the session from one device**, and a separate sign-in on the watch (Strong reviews, 2025 and 2026).
3. **Rest timers that lie**: notification fires while the app shows time left; timers freeze; pause ignored (Strong). A stale 0:00 (Hevy, [shot]).
4. **Routines rewritten behind your back**: skipping a set deletes it from the template next time (Strong 6.0). Hevy updates routine values on save unless you turn it off [web: Hevy help "Previous Workout Values Vs. Routine Values"].
5. **Pre-filled values that must be deleted** (Strong 6.0), as opposed to placeholders.
6. **No guard against typos** that ruin records and suggestions (Hevy, 2026-10-19).
7. **Laggy number entry**: "nearly every time I enter in the weight or reps … it lags" (Strong, 2026-04-01). **Overheating** on iOS 27: "Shouldn't be that resource intensive to run a timer and let me input a lift" (Hevy, 2026-10-26).
8. **Exercise search that needs the exact word order** ("dumbbell incline press" finds nothing; "incline dumbbell bench press" does) (Strong, 2026-02-04).
9. **Bigger controls that show less**: "Everything is bigger now and I can't see as much of my workout at a glance"; mis-taps delete set timers (Strong, 2025-05-28).
10. **Friction at the end**: "confirm twice … that I want to end my workout" (Strong, 2025-05-28).
11. **Blocking sync UI**: "nobody likes to see the white bar at the top that says 'please wait while we sync'" (Strong, 2025-11-01).
12. **Social and paywall clutter**: the Discover feed called "a waste" (Hevy, 2026-10-17); routine caps on free tiers (both apps). Not relevant to a private app, except as a reminder to leave such things out.

## 3. Prioritised feature list

"V1" means needed for the first real gym session. Each line gives the reason.

### V1: must-have for gym use

1. **Routine list from substrate, grouped by folder, with last-done date**: the owner writes routines in the console, so the app only browses them.
2. **Routine preview before start**: shows the exercises, set counts, targets and rest, so the owner can check before committing.
3. **Start from routine and start empty**: the two entry points every app has, and the owner asked for both.
4. **One active session, saved to disk on every change**: a crash, a reboot or a dead network must never lose a set (section 2.8, item 1).
5. **In-progress bar above the tab bar, and resume on launch**: you leave the session screen to pick music or reply to a message, then come back.
6. **Conflict dialog with Resume, Finish and Start, Discard and Start, Cancel**: Hevy's only way forward destroys the old session.
7. **Exercise cards with set rows whose columns follow the exercise type** (load × reps, reps only, bodyweight plus load, assisted, duration, distance): one grid does not fit planks and presses.
8. **Ghost values committed by the check, plus tap-PREVIOUS-to-copy**: one tap per set when nothing changed, and no deleting pre-filled numbers.
9. **Set kinds: warm-up, working, failure, drop (as a chain under a set), myo (activation plus mini-sets), myo-match**: the owner asked for warm-ups, drop sets and myo-reps by name.
10. **RPE per set, 6 to 10 in half steps, with the reps-in-reserve text**: the owner asked for RPE, and the text is what makes it usable mid-set.
11. **Rest timer with per-exercise rest, separate rest inside a superset and after the round, short myo rest, warm-up rest, and no rest inside a drop chain**: this is the owner's explicit requirement and Hevy's biggest gap.
12. **±15 s, Skip, and a time-sensitive notification at rest end that names the next set**: you are not looking at the phone when rest ends.
13. **Live Activity on the Lock Screen and in the Dynamic Island, with countdown, next set, ±15 s, Skip and Done, and no stale 0:00**: Strong lost users for lacking it, and Hevy's goes stale.
14. **Timed sets with a countdown to the target (stopwatch as an option), and a sound and haptic at the end**: the owner asked for timed sets; Hevy's count-up stopwatch needs a help article.
15. **Supersets and circuits: group, label A1 and A2, auto-advance focus to the next exercise in the round, count rounds**: the owner asked for supersets.
16. **Any-order logging with an "Up next" pointer you can override by tapping any set**: the owner asked for any order; the pointer keeps the Live Activity honest.
17. **Edit during session: add, remove, replace and reorder exercises; add and remove sets; change set kind; notes; per-set rest override**: plans meet a busy gym.
18. **Next-set suggestion from this session and earlier sessions, shown as ghost values with a one-line reason**: the owner asked for it; the reason makes it trustworthy.
19. **Outlier guard**: confirm a load more than about 50% off the last set or the last session, since one typo poisons records and suggestions (section 2.8, item 6).
20. **Finish: a summary with editable times and a decision on unticked sets, one confirmation, a write to substrate and to Apple Health, and a retry queue**: the owner asked for the Health write; the queue makes it safe offline.
21. **Discard with one confirmation, reachable only from inside the session**: never a one-tap trash next to the resume target.
22. **Offline-first sync with a quiet status glyph**: gyms have poor signal, and Hevy's "Disconnected" strip and Strong's "please wait" bar push content around.
23. **Keep the screen awake while a session runs (a setting)**: Hevy offers it [web], and unlocking for every set is the most common small irritation.

### V1.1

1. **Exercise detail: history list, top-set and e1RM chart, set records table**: "what did I do last month" is the next question after "what did I do last time".
2. **PR detection with a live banner and a badge on the set**: cheap once history exists, and motivating; kinds follow the exercise type (section 2.3).
3. **Plate calculator, and plates per side shown in the row for barbell work**: matters once loads climb; Liftosaur's inline display beats a separate screen.
4. **Equipment profile per gym** (bars, plates, machine steps, dumbbell steps): suggestions and warm-ups must round to loads that exist.
5. **Warm-up generator** (percentage ramp, rounded to the equipment): saves typing on the first lift of the day.
6. **Live Activity layout for Apple Watch** (`.small` family): the watch shows the countdown with no watch app at all [sdk].
7. **Audio cue at rest end over headphones, ducking the music**: phone in pocket, music playing (`AVAudioSession` category option `duckOthers`) [sdk].
8. **Edit a finished session in the app**: typos are found on the walk home; until then, the console is the fallback.
9. **Superset load normalisation** (Fitbod's idea): one pair of dumbbells for a whole superset.
10. **Session RPE at finish**, sent to Health as the workout effort score if the Health slice agrees (`02-workouts.md` section 1.7 lists `workoutEffortScore`; the iOS 27.2 SDK has `HKQuantityTypeIdentifierWorkoutEffortScore` and `relateWorkoutEffortSample:withWorkout:activity:completion:` [sdk]).

### Later

1. **Native watchOS app** (log from the wrist as a remote of the phone session): the owner said watch is design-only for now, and section 2.8 shows how hard sync is.
2. **Heart rate during the session** through an iPhone workout session: `HKWorkoutSession` can be created on iOS 26+ with an associated builder [sdk]; whether it should run, and where heart rate comes from, belongs to the HealthKit slice.
3. **Routine editing in the app**, including "save today's changes back to the routine": routines are written in the console for now.
4. **Program progression rules** (linear, double, RPE-based, percentage of e1RM) attached to routines: the coach school, once the logger is trusted.
5. **Weekly sets per muscle and volume trends**: Hevy's statistics pages [web]; useful, not needed in the gym.
6. **Import from Hevy (API or CSV) and Strong (CSV)**: covered in `02-workouts.md` section 1.9.
7. **Siri and Shortcuts intents** ("log 8 reps"), and a Control Center control for "rest +15 s": hands-free polish.
8. **Loud rest alarms through AlarmKit** (iOS 26+ `AlarmManager` timers with system countdown and alert UI) [sdk]: an opt-in for people who miss notifications; heavy for every rest.

## 4. Flows

### 4.1 Start from a routine

1. Workout tab, then tap a routine card. The preview shows exercises in order, superset groups, set kinds and targets, rest values, and when it was last done.
2. Tap **Start**. If a session is already open, show the conflict dialog (4.3).
3. The session opens full-screen with every planned set as a ghost row. Ghost values come from the suggestion (4.8), falling back to the routine's target, then to last time.
4. The Live Activity starts with elapsed time and "Up next".

### 4.2 Start empty

1. Tap **Start empty**. The session opens with an "Add exercise" button.
2. The picker searches by tokens in any order ("db incline press" finds "Incline Bench Press (Dumbbell)"), shows recent and frequent exercises first, and filters by muscle and equipment.
3. A new exercise gets the rest and rows from the last time it was done, or one empty working row.

### 4.3 Resume and the conflict dialog

- Resume from the tab bar accessory, from the Live Activity (deep link to the focused set), or automatically on launch if a session is open.
- The conflict dialog names the open session ("Day 1, started 13:42, 11 sets logged") and offers:
  - **Resume** (default);
  - **Finish it and start new** (saves the old one);
  - **Discard it and start new** (destructive);
  - **Cancel**.

### 4.4 Any-order navigation

- All exercises are cards in one scroll, so any set can be ticked at any time.
- **Up next** is computed after every change, in this order:
  1. the next entry in the current drop or myo chain;
  2. the next exercise in the current superset round;
  3. the next set of the same exercise;
  4. the first unfinished set of the next exercise that has one.
- Tapping a row or its field makes it the focus; Up next then follows from there. The Live Activity and the notification always describe Up next.
- A jump menu in the toolbar lists exercises with done/total counts and scrolls to one.
- With superset auto-advance on, a tick scrolls the next exercise in the round into view (Hevy's smart scrolling [web]); the scroll never jumps on any other action (the Hevy review in section 2.6).

### 4.5 Rest rules

| Just completed                                           | Timer                                                       | Next                       |
| -------------------------------------------------------- | ----------------------------------------------------------- | -------------------------- |
| warm-up set                                              | warm-up rest (default short, settable)                      | next row                   |
| working or failure set, not in a superset                | the exercise's rest (set override wins)                     | next set of the exercise   |
| set of a superset exercise that is not last in the round | rest inside the group (default 0, so no timer, focus moves) | next exercise in the round |
| set of the last exercise in the round                    | rest after the round                                        | first exercise, next round |
| drop entry with more entries planned                     | none                                                        | next drop entry            |
| myo activation or mini-set with more planned             | myo rest (default 15 to 20 s)                               | next mini-set              |
| myo-match mini-set, reps not yet matched                 | myo rest                                                    | another mini-set           |
| last set of the session                                  | none                                                        | prompt to finish           |

The notification text follows the row:

- "Rest over. Chest Press, set 1: 35 kg × 15" (after the warm-up, the row Hevy's Live Activity called "set 2 of 4");
- "Go: Lat Pulldown (A2), round 2";
- "Round 3: Bench Press (A1), 60 kg × 8".

### 4.6 Finish

1. Tap **Finish**.
2. If sets are unticked, one dialog asks: drop the unticked sets, or mark them done as shown. There is no second "are you sure".
3. The summary shows title, start and end (editable, since people forget to finish), duration, working sets, volume (warm-ups per the owner's choice), PRs, and notes.
4. Tap **Save**. The session is written to substrate and an Apple Health strength workout is queued. Both retry until they succeed; the history row shows a pending glyph meanwhile.
5. The Live Activity ends with a short summary, and pending notifications are cancelled.

### 4.7 Discard

From the session's menu: **Discard workout**, then a confirmation that names what will be lost ("11 sets"). The Live Activity ends and pending notifications are cancelled. Whether a discarded session leaves a trace in substrate is an open question (section 8).

### 4.8 Suggestions for the next set

This is a proposal, not something found in the survey.

- **From earlier sessions**: PREVIOUS matches working sets by their index among working sets, not by row position. This avoids Hevy's misalignment when warm-ups or drop sets differ between sessions [shot, inferred]. The source is "last time anywhere" or "last time in this routine", a choice Hevy also offers [web].
- **For the first set**: double progression against the routine's rep range and target RPE:
  - all working sets last time at the top of the range, at or under the target RPE: one equipment step up;
  - any set below the range, or RPE above target: same load;
  - otherwise same load, aim one rep higher.
  - If the next step is too big for the equipment, add a rep instead (RP's rule [web]).
- **Within the session**: after each working set, re-estimate with an RPE-adjusted e1RM (`02-workouts.md` section 5 lists the RPE tables) and adjust the next set's ghost load by at most one step, with a reason line ("Set 1 was 14 reps at RPE 9.5 against a target of 8: same load, aim for 12"). The arithmetic: RPE 9.5 leaves about 0.5 reps in reserve, so about 14.5 reps were possible; RPE 8 leaves 2, giving 12.5, rounded down to 12.
- **Never auto-commit**: a suggestion is a ghost value; the check accepts it.

### 4.9 Edit during the session

Exercise menu: Replace, Add to superset or Remove from superset, Add warm-ups, Rest, Note, Move up or down, Remove. Set label menu: Warm-up, Working, Failure, Drop, Myo, Myo-match, Remove. Edits change the session only. Any change to the routine happens in the console until in-app routine editing exists.

## 5. Pain points to avoid

1. **Two sources of truth.** Keep the session on the phone. A future watch app sends commands such as "set 7 done with 35 × 15" that apply idempotently, and never ships whole snapshots that can overwrite newer data (section 2.8, item 1).
2. **Timers computed by counting.** Store the rest's end as a `Date`. Draw it with `Text(timerInterval:countsDown:)` and `ProgressView(timerInterval:)`, which tick without app code [sdk]. Schedule the notification for that instant, and reschedule it on ±15 and Skip. This also avoids burning CPU on a per-second timer (the Hevy overheating review).
3. **A stale Live Activity.** Update it when rest ends. If the app is suspended, set the content's `staleDate` to the rest's end so the view can switch on `context.isStale` [sdk: `ActivityContent.staleDate`, `isStale`; web: Apple guide]. Whether the switch renders on time without the app running needs a device test [unverified]. An iPhone workout session would keep the app running [sdk for the API; recalled for the background behaviour].
4. **Superset rest by workaround.** Model the two rests explicitly (4.5).
5. **Hidden rules**, such as Hevy's "no timer before a drop set". Show the rule: a drop chain renders as one indented block, and the bar says "No rest: drop to 25 kg".
6. **Mismatched numbering.** Use one label scheme everywhere: W1, 1, 2, 2a (drop), 3 M+3 (myo).
7. **Rewriting the routine on save, or pre-filling real values.** Use ghost values only. Routine changes are explicit.
8. **Destructive one-taps**: Hevy's home-screen trash and "permanently deleted" dialog.
9. **Blocking or jumpy sync banners.** Use a toolbar glyph and a pending count.
10. **Scroll jumps** on unrelated taps, and density lost to oversized controls (section 2.8, items 7 and 9). Keep 44 pt targets [recalled for the HIG minimum] but do not pad the grid beyond that.
11. **Strict search.** Match tokens in any order, plus aliases (DB for dumbbell, RDL).
12. **Silent typos.** Use the outlier guard.
13. **Warm-ups polluting stats by default.** Make it a visible choice.
14. **Inconsistent duration formats.** Use one formatter for all durations.

## 6. Hevy patterns in iOS 27 Liquid Glass

Apple's rule: Liquid Glass "forms a distinct functional layer for controls and navigation elements", standard bars, sheets, popovers and controls adopt it automatically, and apps should "avoid overusing Liquid Glass effects" on custom controls [web: Apple, "Adopting Liquid Glass"]. Half sheets are inset and turn more opaque at full height. Action sheets originate from the element that opened them. Section headers use title case, not all capitals [web: same page]. So the set grid, cards and charts stay content, and only floating controls are glass.

| Hevy pattern                                     | Spotter on iOS 27                                                                                                                           | Layer                    | API                                                                                                        |
| ------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------ | ---------------------------------------------------------------------------------------------------------- |
| Tab bar (Home, Workout, Profile)                 | system tab bar: Workout, History, Exercises; minimises on scroll                                                                            | glass (system)           | `TabView`, `tabBarMinimizeBehavior(.onScrollDown)` [sdk]                                                   |
| In-progress bar with trash                       | tab bar accessory: elapsed time, current exercise, rest countdown when running; tap opens the session; no trash                             | glass (system)           | `tabViewBottomAccessory`, reads `tabViewBottomAccessoryPlacement` (`.inline` or `.expanded`) [sdk, iOS 26] |
| "Workout ⌄" title switcher                       | not needed; a title menu if a second mode appears                                                                                           | glass                    | `toolbarTitleMenu` [sdk]                                                                                   |
| Start Empty Workout row                          | toolbar "+" button                                                                                                                          | glass                    | toolbar item [sdk]                                                                                         |
| Routine folders and cards with big Start buttons | grouped list, collapsible sections, card rows; tap opens the preview                                                                        | content                  | `List`, `Section(isExpanded:)` [sdk]                                                                       |
| Start Routine                                    | one prominent Start in the preview's bottom toolbar                                                                                         | glass                    | `.buttonStyle(.glassProminent)` [sdk]                                                                      |
| Conflict alert                                   | confirmation dialog anchored to Start                                                                                                       | system glass             | `confirmationDialog` [sdk]                                                                                 |
| Log Workout toolbar (minimise, clock, Finish)    | leading minimise; trailing group: tools menu, then Finish (prominent)                                                                       | glass                    | toolbar items, `ToolbarSpacer` [sdk]                                                                       |
| Duration, Volume, Sets, muscle map               | small header at the top of the scroll; elapsed time also in the accessory                                                                   | content                  | none                                                                                                       |
| Disconnected strip                               | toolbar glyph with a pending count; a popover explains                                                                                      | glass (toolbar)          | toolbar item                                                                                               |
| Exercise card, Rest and Note chips               | card in the scroll; rest and note as plain buttons inside it                                                                                | content                  | `.buttonStyle(.bordered)` [sdk]                                                                            |
| Exercise "…" bottom sheet                        | menu that morphs from the button                                                                                                            | glass (system)           | `Menu` [web: Apple, buttons "morph into menus"]                                                            |
| Set label and type sheet                         | menu from the label: W, working, F, D, M, match, Remove (destructive)                                                                       | glass (system)           | `Menu`                                                                                                     |
| Set rows, check button, green done state         | grid rows; tinted when done; haptic on tick                                                                                                 | content                  | `sensoryFeedback` [sdk]                                                                                    |
| Swipe a row to delete (not in the screenshots)   | swipe actions on rows outside a `List`                                                                                                      | content                  | `swipeActions(edge:…)`, `swipeActionsContainer()` [sdk, iOS 27]                                            |
| Custom keypad with +, −, check                   | system number pad plus a keyboard toolbar: −, +, plates, Next                                                                               | glass (keyboard bar)     | `ToolbarItemGroup(placement: .keyboard)` [sdk]                                                             |
| RPE keypad and slider                            | a short inset sheet or keyboard-height panel: 6 to 10 with half steps and the reps-in-reserve line; optional RIR wording                    | glass sheet              | `presentationDetents` [sdk]                                                                                |
| Rest bar (-15, time, +15, Skip)                  | floating bar at the bottom of the session: countdown and progress plus three glass buttons; when no rest runs it becomes the "Up next" chip | glass                    | `safeAreaBar(edge: .bottom)`, `GlassEffectContainer`, `glassEffect(_:in:)`, `glassEffectID` [sdk, iOS 26]  |
| Rest wheel sheet                                 | menu of presets (Off, 30 s, 60 s, 90 s, 2 min, 3 min, Custom…); Custom opens a medium sheet with a wheel                                    | glass                    | `Menu`, `presentationDetents([.medium])` [sdk]                                                             |
| Purple Superset capsule                          | coloured bracket on the left of grouped cards, labels A1 and A2, round counter                                                              | content                  | none                                                                                                       |
| Reorder Exercises list                           | sheet with a compact list and drag handles                                                                                                  | content in a glass sheet | `reorderable()` or `reorderContainer` [sdk, iOS 27], or `List` edit mode                                   |
| Timed set play button                            | play in the row; while running, the row and the bottom bar both count down                                                                  | content plus glass bar   | `Text(timerInterval:)` [sdk]                                                                               |
| Live Activity (Lock Screen)                      | exercise, Up next with load and reps, countdown, ±15 s, Skip, Done; after rest ends: "Go: set 2"                                            | system                   | ActivityKit, `LiveActivityIntent` buttons [sdk]                                                            |
| Dynamic Island                                   | compact: glyph and countdown; expanded: Up next and buttons                                                                                 | system                   | ActivityKit                                                                                                |
| Watch "Open on iPhone" view                      | a designed `.small` layout: countdown ring and Up next                                                                                      | system                   | `supplementalActivityFamilies([.small])` [sdk]                                                             |
| "Rest time is over!"                             | time-sensitive notification that names the next set                                                                                         | system                   | `interruptionLevel = .timeSensitive` [sdk]; needs the Time Sensitive capability [recalled]                 |

## 7. Design implications for Spotter

1. **Build the session engine first and the screens second.** One local session document holds exercises, groups, sets, kinds, timestamps, Up next and the rest's end. It is persisted on every change and derives everything else: the grid, the accessory, the Live Activity, notifications, and later the watch.
2. **Rest is a function of (set kind, group position, routine settings, overrides)**, not one number per exercise. The routine format the console agent writes must carry exercise rest, warm-up rest, rest inside a group, rest after a round, and myo rest.
3. **The set model needs chains.** A drop set and a myo set are one logical set with ordered entries (MacroFactor's shape [web]). This keeps "set 2" meaning one thing to the lifter, the Live Activity and the suggestion engine. RP's "total reps in one row" can be derived from it; the reverse cannot.
4. **Every set needs `completedAt`**, and the session needs start, end and pause spans. Rest actually taken, Health's start and end, and the "fix the time at finish" step all depend on them. FIT and Health Connect store per-set start times (`02-workouts.md` section 3).
5. **Suggestions are ghost values with reasons, driven by equipment steps.** A per-gym equipment profile can arrive in V1.1. V1 still needs a default load step per exercise, or suggestions will propose loads that do not exist.
6. **Glass only on the floating layer**: toolbar, tab bar accessory, rest bar, menus, sheets. The grid stays dense content with 44 pt targets.
7. **The Live Activity is a V1 surface, not polish.** Design its states (working, resting, rest over, timed set running, finished) at the same time as the session screen. Use `staleDate` or background runtime so "rest over" never shows 0:00 with live buttons.
8. **Watch, when it comes, is a remote.** The `.small` Live Activity layout gives a watch countdown in V1.1 for little work. A watch app later sends idempotent set events to the phone's session. It never starts its own session record or asks for a separate sign-in.
9. **Expect the workout back from Apple Health.** Substrate's Apple Health provider receives what the Health Auto Export app posts and writes each workout as a `workout` mirror row keyed `applehealth/<account>/<id>`, "the id HealthKit gives the workout"; "Workouts carry no `source`" [src: `kinds/providers.substrate.reamde.dev/applehealth/README.md`, `bundle.yaml`]. Its adapter then writes a `substrate.reamde.dev/health/workout`, maps "traditional strength training" and "functional strength training" to `strength`, and fills `source` from the label most common on the workout's heart rate, step, distance and energy rows, which names a device such as the watch, not the app that wrote the workout [src: same `bundle.yaml`, `workout_outputs`]. A Spotter session written to Health will return as a second record. The data-model slice needs a join (time overlap plus sport `strength`) or a marker.
10. **Naming in substrate.** In Mneme v6, `routine` already means a recurring habit (floss, stretch), and `routinelog.evidence` can point at "the workout that satisfied the training routine" [src: `svc/mneme-v6/kinds/health/routine.yaml`, `routinelog.yaml`]. Mneme v4 first called the gym template `routine` and renamed it `workouttemplate` when the habit scheduler took the word [src: same comment]. Mneme v6 itself carried `mneme.example.org/health/workouttemplate`, `workoutset` and `exercise` from T-068 until T-110 deleted them as empty kinds on 2026-10-19 (commit `311b7fee`); `svc/mneme-v6/docs/not-ported.md` still lists them [src: git history]. They are prior art for the data-model slice. The UI can say "Routines"; the kind should not be named `routine`. Substrate's first-party `substrate.reamde.dev/health/workout` already has `sport: strength`, duration, energy and heart rate, but no sets [src: `/tmp/spotter-substrate-main/kinds/substrate.reamde.dev/health/workout.yaml`].
11. **Offline first.** Writes go to a local queue and the UI never waits for the server. The only sync UI is a glyph.

## 8. Open questions for the owner

1. **Discard**: delete the session completely, or keep an "abandoned" record in substrate?
2. **One session at a time**: is that enough, or should a second device (iPad) see and edit the open session live?
3. **Units and equipment**: kg only? Which gyms, bars, plate sets and dumbbell steps should the first equipment profile hold?
4. **Effort scale**: RPE 6 to 10 like Hevy, RIR, or both shown (RPE stored, RIR wording)? Allow values below 6?
5. **Warm-ups**: count toward volume, set counts and PRs? Should warm-ups have their own default rest, and what should it be?
6. **PREVIOUS**: last time anywhere, or last time in the same routine (Hevy's two modes)?
7. **Superset rests**: what defaults do you want for rest inside a group and after a round? Should round rest apply after the warm-up round?
8. **Myo-reps**: log every mini-set as its own entry (proposed), or total reps only? Which stop rule should the suggestion use: fewer than 3 reps, or matching a target?
9. **Timed sets**: countdown to target by default, with the stopwatch as an option?
10. **Rest end alert**: is a time-sensitive notification with sound and haptic enough, or do you want an AlarmKit-style alarm that sounds in silent mode?
11. **Suggestions**: show only (ghost values), or also pre-fill the routine's next targets in substrate for the console agent to read?
12. **Edits during a session**: should they ever propose a change to the routine in substrate (for example, a "suggest change" record the console agent reviews)?
13. **Apple Health**: write only the workout, or also a session effort score and heart rate (which needs a phone or watch workout session)? How should the Health copy be kept from duplicating the substrate record?
14. **Exercise catalogue and pictures**: where should names, muscles and images come from? `02-workouts.md` section 3 notes free-exercise-db is public domain (Unlicense); wger's code is AGPL and each exercise record carries its own licence, such as CC-BY-SA 4.0.
15. **PR celebrations**: a banner and medal like Hevy, or quieter?
16. **Keep awake**: on by default during a session?

## Sources

Owner's screenshots (local, read today): the 13 attachments the owner shared, listed in section 1 and not kept in this repository.

Prior research and local source:

- `/tmp/spotter-substrate-main/docs/plans/health-data/02-workouts.md` (sections 1.7, 1.9, 3, 5)
- `/tmp/spotter-substrate-main/kinds/providers.substrate.reamde.dev/applehealth/README.md` and `bundle.yaml`
- `/tmp/spotter-substrate-main/kinds/substrate.reamde.dev/health/workout.yaml`
- `svc/mneme-v6/kinds/health/routine.yaml`, `svc/mneme-v6/kinds/health/routinelog.yaml`, `svc/mneme-v6/docs/not-ported.md`; commit `311b7fee` (T-110) and its parent's `svc/mneme-v6/kinds/health/workouttemplate.yaml`
- iOS 27.2 SDK: `SwiftUI`, `SwiftUICore`, `WidgetKit`, `ActivityKit`, `AlarmKit`, `AppIntents` `.swiftinterface` files; `HealthKit/HKWorkoutSession.h`, `HKTypeIdentifiers.h` and `HKHealthStore.h`; `UserNotifications/UNNotificationContent.h` and `UNNotificationSettings.h`; `AVFAudio/AVAudioSessionTypes.h`

Hevy:

- [Live Activity](https://help.hevyapp.com/hc/en-us/articles/35649846517399)
- [Previous Workout Values](https://help.hevyapp.com/hc/en-us/articles/36011896355479)
- [Previous Workout Values vs Routine Values](https://help.hevyapp.com/hc/en-us/articles/34105442929943)
- [Warm Up Calculator](https://help.hevyapp.com/hc/en-us/articles/35650921359639)
- [Circuits, Intervals, and Supersets](https://help.hevyapp.com/hc/en-us/articles/36954623739415)
- [Supersets and Smart Superset Scrolling](https://help.hevyapp.com/hc/en-us/articles/35650286563095)
- [Rest Timer & Default Rest Timer](https://help.hevyapp.com/hc/en-us/articles/35385404949143)
- [Inline timer goes up, not down](https://help.hevyapp.com/hc/en-us/articles/35379662997783)
- [Set Types](https://help.hevyapp.com/hc/en-us/articles/34896293707927)
- [How to Use RPE](https://help.hevyapp.com/hc/en-us/articles/35687721776663)
- [RPE vs RIR](https://help.hevyapp.com/hc/en-us/articles/34490600233111)
- [Set Records vs Personal Records](https://help.hevyapp.com/hc/en-us/articles/38279531346455)
- [PRs and Set Records Explained](https://help.hevyapp.com/hc/en-us/articles/35649367857175)
- [Live PR Notifications](https://help.hevyapp.com/hc/en-us/articles/36012016405655)
- [Plate Calculator](https://help.hevyapp.com/hc/en-us/articles/34518876511383)
- [Adjust duration and pause](https://help.hevyapp.com/hc/en-us/articles/34513981310615)
- [Apple Watch Sync Issues](https://help.hevyapp.com/hc/en-us/articles/33996260919703)
- [Estimated 1RM](https://help.hevyapp.com/hc/en-us/articles/36954464726167)
- [Hevy Trainer Settings](https://help.hevyapp.com/hc/en-us/articles/43572343844247)
- [Hevy Trainer Explained](https://help.hevyapp.com/hc/en-us/articles/38385724273047)
- [150-Set Cap](https://help.hevyapp.com/hc/en-us/articles/34896183826455)
- [Statistics Explained](https://help.hevyapp.com/hc/en-us/articles/35702030346903)
- [Workout Settings Preferences](https://help.hevyapp.com/hc/en-us/articles/33882110558743)
- Feature pages: [workout settings](https://www.hevyapp.com/features/workout-settings/), [rest timer](https://www.hevyapp.com/features/workout-rest-timer/), [supersets](https://www.hevyapp.com/features/what-are-supersets/), [warm-up calculator](https://www.hevyapp.com/features/warm-up-set-calculator/), [plate calculator](https://www.hevyapp.com/features/weight-plate-calculator/)
- [App Store listing](https://apps.apple.com/us/app/hevy-workout-tracker-gym-log/id1458862350) and [recent reviews feed](https://itunes.apple.com/us/rss/customerreviews/page=1/id=1458862350/sortby=mostrecent/json)

Strong:

- [App Store listing](https://apps.apple.com/us/app/strong-workout-tracker-gym-log/id464254577)
- [Recent reviews feed](https://itunes.apple.com/us/rss/customerreviews/page=1/id=464254577/sortby=mostrecent/json)

Fitbod:

- [Apple Watch](https://help.fitbod.me/hc/en-us/articles/360006499194-Apple-Watch)
- [Circuits & Supersets](https://help.fitbod.me/hc/en-us/articles/360006427813-Circuits-Supersets)
- [Rest Timer](https://help.fitbod.me/hc/en-us/articles/360006340194-Rest-Timer)
- [Reps in Reserve](https://help.fitbod.me/hc/en-us/articles/360033133174)
- [Warm-Up Sets](https://help.fitbod.me/hc/en-us/articles/360006337634-Warm-Up-Sets)
- [How does Fitbod decide my sets, reps and weight?](https://help.fitbod.me/hc/en-us/articles/43489869175063)

RP Hypertrophy:

- [Logging Weights, Reps, Sets](https://help.rpstrength.com/hc/en-us/articles/43828601644055-Logging-Weights-Reps-Sets)
- [Rest Times](https://help.rpstrength.com/hc/en-us/articles/43549598219799-Rest-Times)
- [Hypertrophy App Progressions](https://help.rpstrength.com/hc/en-us/articles/43864744488599-Hypertrophy-App-Progressions)
- [Warm-ups and Starting Weights](https://help.rpstrength.com/hc/en-us/articles/43549480893591-Warm-ups-and-Starting-Weights)
- [RIR](https://help.rpstrength.com/hc/en-us/articles/43549386649751-RIR-Reps-in-Reserve)

MacroFactor:

- [Logging Different Types of Sets](https://help.macrofactorapp.com/en/articles/317-logging-different-types-of-sets)
- [What are myo-reps, drop sets and failure sets](https://help.macrofactorapp.com/en/articles/379-what-are-myo-reps-drop-sets-and-failure-sets)

Liftosaur:

- [Features](https://www.liftosaur.com/features)
- [Liftoscript](https://www.liftosaur.com/doc/liftoscript)

Boostcamp:

- [Rest timer](https://www.boostcamp.app/features/rest-timer)
- [RPE logging](https://www.boostcamp.app/features/rpe-logging)

Alpha Progression:

- [App Store listing](https://apps.apple.com/us/app/gym-workout-alpha-progression/id1462277793)
- [Alpha Progression vs Hevy (vendor blog)](https://alphaprogression.com/en/blog/alpha-progression-vs-hevy)

Setgraph:

- [App Store listing](https://apps.apple.com/us/app/setgraph-gym-workout-tracker/id1209781676)

OpenLift and Better Reps:

- [OpenLift myo-reps post](https://openlift-app-website-stage.pages.dev/blog/myo-reps-in-openlift) (the vendor's blog, on a staging domain)
- [Better Reps on the App Store](https://apps.apple.com/us/app/-/id6749870012)

Secondary, used only where marked [unverified]:

- [Gravl review roundup](https://www.sensai.fit/blog/gravl-app-review-2026)
- [Fitbod user reviews aggregator](https://justuseapp.com/en/app/1041517543/fitbod-gym-fitness-planner/reviews)

Method:

- [Barbell Medicine, "Myo-reps"](https://www.barbellmedicine.com/blog/myo-reps/)

Apple:

- [Adopting Liquid Glass](https://developer.apple.com/documentation/technologyoverviews/adopting-liquid-glass)
- [Displaying live data with Live Activities](https://developer.apple.com/documentation/activitykit/displaying-live-data-with-live-activities)
