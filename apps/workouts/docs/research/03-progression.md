# Spotter: suggesting the next set

> **Where this came from.** Written on 2026-10-30 in rxtx.dev, as `research/workouts/`, for
> the app then called Spotter (Substrate Workouts now); the version here is the one the
> app's second build used (geoah/rxtx.dev#118). It was carried into this repository on
> 2026-11-02 as written, with two changes. The links resolve inside this repository. The
> draft kind paths `mneme.rxtx.dev/training/<kind>` now read as the app-owned
> `~/training/<kind>` they became (decision record [0203](../../../../docs/decisions/0203-an-app-keeps-its-own-package-under-a-declare-scope.md)). Where the
> text names the package home it recommended, a Mneme package `mneme.rxtx.dev/training`,
> that name is left as written: the home changed later, and the reasons are in
> [decisions.md](../decisions.md). Mentions of Mneme, rxtx.dev paths or
> `apps/apple/spotter` describe the plan at the time. The package now lives in
> [`Package/`](../../package/README.md), and the app's spec is [DESIGN.md](../../DESIGN.md).

Researcher slice 03 (workouts). Date: 2026-10-30.

The question: how should Spotter suggest the load and reps of the next set, from the sets already done in this session and from earlier sessions of the same exercise? Part A is the research. Part B is a deterministic specification with test vectors that a Swift engineer can implement without reading anything else.

Evidence markers:

- **[web]** checked today on the web or in a primary artefact (a paper, a vendor help page, source code on GitHub, the owner's screenshots).
- **[src]** checked in source code or documents in this machine's checkouts.
- **[recalled]** from memory, not re-checked today.
- **[unverified]** I tried and could not confirm it, or only a secondary source has it.

Every number in the test vectors was computed by hand and again by a scratch script (kept outside the repository). Both agree.

## Summary

- Spotter estimates a one-rep maximum (e1RM) from each working set with the RPE percentage chart attributed to Tuchscherer (Reactive Training Systems). Effort enters as reps in reserve: `n = reps + (10 − RPE)`. The chart is a single curve in `n`, published for `n` from 1 to 16. Beyond 16 Spotter uses an Epley-shaped tail.
- The e1RM is an internal exchange rate between load and reps, not a claim about a true maximum. Formulas disagree by 10 % or more above about 10 reps, and the reps-to-%1RM curve differs by exercise. The errors mostly cancel when the reference set and the suggested set have similar reps, which is the normal case.
- The first working set of an exercise is suggested from the same set of the previous session (same position among working sets), plus one rep. When the rep goal passes the top of the range, the load goes up one equipment step and the reps follow from the e1RM. When the goal falls below the range, the load goes down to the heaviest step that gives the bottom of the range at the target effort.
- Every later set in the session is suggested from the last working set done today, after a small fatigue allowance. The load changes only when the predicted reps leave the rep range. One in-session suggestion raises the load by at most one step or 10 %, whichever is larger.
- Missing RPE is normal. A set that met its rep target is assumed to have been done at its target RPE. A set that fell short is assumed to have been taken to failure. With no RPE anywhere, the engine behaves like double progression while the sets stay in the range (one more rep at the same load, then one step up). It is not plain double progression: a set short of the range lowers the load (V7), and after a step up the reps come from the e1RM, not the bottom of the range (V1).
- Warm-ups, drop segments and myo-rep mini-sets never feed the e1RM. Drops are 20 % down, rounded down. Myo-rep mini-sets follow Fagerli's stop rule. Timed sets add 5 s per session. Bodyweight and assisted exercises use the lifter's body mass to form an effective load.
- The output is one load, one rep count (or a duration), and one short sentence, for example "Last time 35 kg × 13 at RPE 9.5; try 35 kg × 14".

## 0. What this builds on

The substrate research slice `docs/plans/health-data/02-workouts.md` (substrate main `ffd7b967`, dated 2026-10-08) covers platforms and formats. For progression it has one row: e1RM as Epley `w × (1 + r/30)` or Brzycki `w × 36/(37 − r)`, or RPE-adjusted through an RPE table (Tuchscherer; Zourdos et al. 2016; Helms et al. 2016) (section 5 of that document) [src]. It also records what a set carries in Hevy (`weight_kg`, `reps`, `duration_seconds`, `rpe` limited to 6, 7, 7.5, 8, 8.5, 9, 9.5, 10, and the set types normal, warmup, dropset, failure), that Strong's CSV has no unit column, and that Hevy and Strong have no side field for unilateral work (sections 1.9 and 3) [src]. I re-checked what I lean on: the two formulas match the published forms (section 1.1), and the owner's Hevy screenshot shows the RPE keypad with 6, 7, 7.5, 8, 8.5, 9, 9.5 and 10, with "6 Moderate Effort, Could do 4 more reps" [web: owner's screenshot]. That document has no progression model, no RPE chart values, no fatigue, rounding or history rules. This slice adds them.

Sibling documents written today in this directory: `02-training-methods.md` (set structures, rest, effort, and Fagerli's own myo-rep rules read from the Internet Archive) [src], `05-substrate-fit.md` (the record shapes; a `set` with `setType`, `load` as a quantity in kg or lb, `reps`, `duration`, `rpe`) [src] and `06-app-teardown.md` (what each app shows) [src]. Where this slice depends on them it says so.

## Part A: research

## 1. Estimating a one-rep maximum from a set

### 1.1 The classical formulas

With `w` the load and `r` the reps to failure [web: Wikipedia, cross-checked against the Fitbod preprint's literature review]:

- Epley (1985): `1RM = w × (1 + r/30)`. It came from a poundage chart in a University of Nebraska manual, not from a study [web: Marzagao 2026 review].
- Brzycki (1993): `1RM = w × 36/(37 − r)`, about `w / (1.0278 − 0.0278 r)`. A practitioner article without a reported sample [web].
- Lombardi (1989): `1RM = w × r^0.10` [web].
- Mayhew et al. (1992): `1RM = w / (0.522 + 0.419 e^(−0.055 r))`, fitted on 435 college students on the bench press [web].
- O'Conner et al. (1989): `1RM = w × (1 + 0.025 r)` [web].
- Wathen (1994, often spelled Wathan): `1RM = w / (0.488 + 0.538 e^(−0.075 r))` [web].
- Lander (1985): `1RM = w / (1.013 − 0.0267123 r)` [web].

What each implies, as the percentage of 1RM that can be lifted for `r` reps to failure (computed from the formulas above):

| Reps to failure                        | 1     | 3    | 5    | 8    | 10   | 12   | 15   | 20   |
| -------------------------------------- | ----- | ---- | ---- | ---- | ---- | ---- | ---- | ---- |
| Epley                                  | 96.8  | 90.9 | 85.7 | 78.9 | 75.0 | 71.4 | 66.7 | 60.0 |
| Brzycki                                | 100.0 | 94.4 | 88.9 | 80.6 | 75.0 | 69.4 | 61.1 | 47.2 |
| Lombardi                               | 100.0 | 89.6 | 85.1 | 81.2 | 79.4 | 78.0 | 76.3 | 74.1 |
| Mayhew                                 | 91.9  | 87.7 | 84.0 | 79.2 | 76.4 | 73.9 | 70.6 | 66.1 |
| O'Conner                               | 97.6  | 93.0 | 88.9 | 83.3 | 80.0 | 76.9 | 72.7 | 66.7 |
| Wathen                                 | 98.7  | 91.8 | 85.8 | 78.3 | 74.2 | 70.7 | 66.3 | 60.8 |
| Lander                                 | 98.6  | 93.3 | 87.9 | 79.9 | 74.6 | 69.2 | 61.2 | 47.9 |
| RTS chart, Spotter's `p` (section 1.4) | 100.0 | 92.2 | 86.3 | 78.6 | 73.9 | 68.0 | 59.9 | 52.6 |

From 5 to 10 reps the seven formulas span 5 to 6 points. At 1 and 3 reps they span 7 to 8 points, mostly because Mayhew is low there. At 20 reps they span 47 to 74 % (computed).

### 1.2 Accuracy by rep range

- The formulas "greatly diverge after about 10 reps", and an estimate "may vary by 10% or more from the actual 1RM" [web: Wikipedia].
- LeSuer et al. (1997) tested seven equations on 67 untrained students. Correlations with measured 1RM were above 0.95, but all equations underestimated the deadlift [web: as summarised in the Marzagao 2026 review].
- Reynolds et al. (2006), 70 adults aged 18 to 69: a 5RM predicted best (R² = 0.993 bench press, 0.974 leg press). Accuracy fell at 10RM and 20RM. Body measurements added nothing [web: same review].
- Mayhew et al. (2008), 103 women: equations were more accurate with fewer than 10 reps to failure [web: same review].
- Nuzzo et al. (2024), a meta-regression of 952 reps-to-failure tests from 269 studies: exercise was the only moderator that mattered. Sex, age and training status had little effect. The main model gives about 5 reps at 90 % and about 15 at 70 %. The bench press gives about 4, 9 and 14 reps at 90, 80 and 70 %. The leg press gives about 9, 13 and 19. The between-person SD is about 2.5 reps at 80 % and 4.4 at 60 % [web: PMC full text]. For comparison, the RTS chart puts 90, 80 and 70 % at `n` = 3.7, 7.5 and 11.3 (computed). The chart gives fewer reps than the meta-analysis at every one of these percentages, even against the bench press table (about 4, 8.8 and 14.1). Against the main model the gap grows from about 1.3 reps at 90 % to 3.7 at 70 % (computed).
- Marzagao (2026), a preprint by a Fitbod employee, used 303,494 near-failure sets from 14,966 users and 388 exercises. Sets came from Fitbod logs: AMRAP-flagged sets, or sets with fewer reps than an earlier set at the same load in the same workout. Only the first such set per exercise per workout was kept, to limit within-session fatigue. There was no measured 1RM. The criterion was consistency: different load and rep combinations from one person, one exercise and one 14-day window should give the same estimate. The proposed formula `1RM = w × (1 + (r − 1)^0.85 / (−2.55 + 4.58 ln w))` (w in kg) was 17 to 22 % more consistent than Brzycki, Epley, Wathen and Mayhew. 91 % of the gain came from the weight-dependent factor [web]. Its consequence: one more rep is worth more at light loads. At 10 kg the formula puts 10 reps at 55 % of 1RM. At 100 kg it puts 10 reps at 74 % (computed). The study excluded bodyweight and assisted exercises, because the logged weight is only the added part [web]. It is not peer reviewed.

Takeaway for Spotter: absolute e1RM values depend on the model and the exercise. Treat e1RM as an exchange rate between reps and load for one exercise, used near the rep counts it was estimated from. Show e1RM trends, not e1RM as a true max.

### 1.3 RPE and reps in reserve

- Zourdos et al. (2016) introduced the resistance-training RPE scale anchored on reps in reserve: RPE 10 = 0 RIR, RPE 9 = 1 RIR, and so on (J Strength Cond Res 30(1):267-275) [web: repository record]. Helms et al. (2016, Strength Cond J 38(4):42-49) described how to apply it [web: bibliographic record; full text not reachable today].
- On this scale, `RIR = 10 − RPE` from RPE 6 up. Hevy shows exactly that text: "6 ... Could do 4 more reps" [web: owner's screenshot]. wger stores RIR and derives RPE as 10 − RIR (`02-training-methods.md` section 5) [src].
- Accuracy. Halperin et al. (2022), 12 studies and 414 people: lifters underpredicted their reps to failure by 0.95 reps on average (95 % CI 0.17 to 1.73). Predictions were better closer to failure and with 12 or fewer reps [web]. Steele et al. (2017), 141 people: underprediction across experience levels, a trend to better accuracy with experience [web]. Refalo et al. (2024), trained lifters at 75 % 1RM bench: mean error 0.65 ± 0.78 reps [web: as summarised in the Marzagao review].
- Consequence: a logged RPE is usually slightly too high (the lifter had a little more in reserve than they said). An e1RM from a logged RPE is therefore slightly low. That bias is on the safe side for suggestions.

### 1.4 The RTS chart

The chart gives the percentage of 1RM for a number of reps at an RPE. Two independent reproductions agree cell for cell: sensAI (reps 1 to 12, RPE 10 to 6) and Fitness Volt (reps 1 to 10, RPE 10 to 6.5) [web]. RTS's own publication (Tuchscherer's Reactive Training Manual, 2008) was not reachable [recalled for the original]. A check of all 108 cells shows the chart is one curve in `n = reps + 10 − RPE` (computed): every cell with the same `n` has the same value. The values, as fractions of 1RM:

| n    | 1     | 1.5   | 2     | 2.5   | 3     | 3.5   | 4     | 4.5   | 5     | 5.5   | 6     |
| ---- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- |
| p(n) | 1.000 | 0.978 | 0.955 | 0.939 | 0.922 | 0.907 | 0.892 | 0.878 | 0.863 | 0.850 | 0.837 |

| n    | 6.5   | 7     | 7.5   | 8     | 8.5   | 9     | 9.5   | 10    | 10.5  | 11    |
| ---- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- |
| p(n) | 0.824 | 0.811 | 0.799 | 0.786 | 0.774 | 0.762 | 0.751 | 0.739 | 0.723 | 0.707 |

| n    | 11.5  | 12    | 12.5  | 13    | 13.5  | 14    | 14.5  | 15    | 15.5  | 16    |
| ---- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- |
| p(n) | 0.694 | 0.680 | 0.667 | 0.653 | 0.640 | 0.626 | 0.613 | 0.599 | 0.586 | 0.572 |

Example: 5 reps at RPE 8 is `n = 7`, 81.1 % of 1RM.

How others use it:

- OpenPowerlifting's RPE calculator fits the chart with a quadratic up to `n − 1 = 2.92` and a line after it, and refuses `n − 1 ≥ 16`. Its source says of the linear part: "Otherwise it's just a line, since Tuchscherer just guessed" [web: plsource `static/rpe-calc/index.html`].
- Liftosaur uses that fit for `Weight_rpeMultiplier`, clamps reps to 24, returns 0.5 for `n − 1 ≥ 16`, and assumes RPE 10 when a set has no RPE (`Weight_getOneRepMax(weight, reps, rpe ?? 10)`) [web: liftosaur `src/models/weight.ts`].
- Juggernaut AI's help describes a target the same way: 5 reps at RPE 8 means a load that leaves 2 more reps [web: JTS help "All about RPE and RIR"].

### 1.5 The curve Spotter uses, and why

- For `1 ≤ n ≤ 16`: the chart values above, with linear interpolation between half steps. These are the published numbers, not a fit, so a test can check them exactly.
- For `n > 16`: `p(n) = 0.572 × 46 / (30 + n)`. This is Epley's shape `1/(1 + n/30)` scaled to meet the chart at `n = 16`. It falls more slowly than continuing the chart's line, which would reach zero near `n = 37`. It gives 52.6 % at `n = 20` and 43.9 % at `n = 30`.
- Sets with `n > 30` are not used for e1RM. They fall back to a reps-only rule.

Why not one of the formulas: the chart is the standard that RPE-based programs and apps use, it includes effort by construction, and its values are published. Its known weakness is the steep slope from `n = 10` to 16 (about 2.8 points of 1RM per rep, 3.8 to 4.9 % of the working load per rep). The data in section 1.2 are all shallower there: Marzagao's formula loses 1.8 points per rep at 10 kg and 1.5 at 100 kg between 10 and 16 reps, Nuzzo's main model about 2 points per rep from 90 to 70 %, and the leg press 1.7 from 80 to 70 % (computed). Light loads differ from the chart mainly below 10 reps, where Marzagao's 10 kg curve falls about 5 points per rep against the chart's 2.9 (computed). The slope only matters when the suggestion changes the rep count a lot. Design implication 13 lists a per-exercise calibration as later work.

## 2. Choosing a load for a target of reps at an RPE

The direct inversion: `load = e1RM × p(reps + 10 − RPE)`, then round to the equipment. For a fixed load, the inverse gives reps: `reps = floor(p⁻¹(load / e1RM) − (10 − RPE))`.

Published rules for adjusting from the set just done:

- Helms et al. (2018), an 8-week trial of RPE-based against percentage-based loading in trained men. The RPE group chose loads to land in a target RPE range. Between sets: "for every 0.5 RPE above or below the upper or lower RPE threshold, respectively, load was decreased or increased by 2 %", and "each missed repetition was considered a full RPE point". The percentage group added 2.5 % of 1RM in weeks 3, 5 and 7 only if all sets and reps were completed, and cut load 4 % per missed rep on the next set. Both groups gained strength and muscle. The RPE group had a small, uncertain advantage in 1RM [web: PMC full text]. 2 % per half RPE point is 4 % per rep, close to the chart's slope at 5 to 15 reps.
- RTS "load drops" and "repeats": after a top set at a target RPE, either drop the load by a percentage and do sets until the RPE climbs back, or repeat the top set's load until the RPE reaches a cap. "Fatigue percent" is the planned drop in performance from the top set [web, secondary: All Things Gym and forum summaries; RTS manual not reached].
- Boostcamp's Auto Progression: when no set reaches the bottom of the range, it "estimates a weight you can hit for that many reps and suggests it instead"; estimated weights are "rounded down to your step size" [web].

## 3. Fatigue inside a session

At a fixed load, reps fall from set to set, more with short rest and when sets go to failure.

Rahimi (2005), 4 sets of squats to failure at 85 % 1RM, mean reps per set [web: J Sports Sci Med 4(4):361-366, PMC]:

| Rest between sets | Set 1 | Set 2 | Set 3 | Set 4 |
| ----------------- | ----- | ----- | ----- | ----- |
| 1 min             | 7.70  | 4.80  | 3.50  | 2.20  |
| 2 min             | 7.35  | 5.40  | 4.45  | 3.20  |
| 5 min             | 7.35  | 6.70  | 6.00  | 4.65  |

- Senna et al. (2011), 5 sets at 10RM loads on bench press, leg press, chest fly and leg extension with 1, 3 or 5 min rest: reps fell from the second set with 1 min rest and from the third set with 3 and 5 min. RPE rose across sets in all conditions [web: PubMed abstract].
- Willardson (2006, review): 3 to 5 min rest keeps reps up for strength work; 1 to 2 min produces meaningful declines [web: as summarised in the Marzagao review].
- No 1RM equation accounts for set order. The Fitbod preprint handles it by keeping only the first near-failure set per workout [web].

Reading these together: sets taken to failure lose about 1 to 3 reps per set with 1 to 2 min rest, and 0.6 to 1.4 reps with 5 min. Sets stopped 2 or more reps short lose less, and their RPE rises instead [web for the data, my inference for the size]. Spotter re-estimates from the set just done, which already includes the fatigue so far, and subtracts a small allowance for the next set's extra fatigue: 1 rep after a failure set, half a rep after RPE 9 or 9.5, nothing at RPE 8.5 or below. These sizes are a heuristic, not a measured constant (open question 5).

## 4. Rounding to what the gym has

- Liftosaur's defaults, in lb: barbell, trap bar and Smith machine 45 lb bar with plates, EZ bar 20 lb, dumbbells 5 to 100 lb in 5 lb steps, kettlebells 10 to 40 lb, cable stack 10 to 200 lb by 10. In kg: bar 20 kg (EZ 10 kg) with 20, 10, 5, 2.5, 1.25 and 0.5 kg plates; dumbbells 2 to 10 kg by 2, then 12.5 to 50 kg by 2.5; kettlebells 4, 8, 12, 16 and 24 kg. Rounding is "Down" by default ("never goes heavier than the program says"), with "Nearest" as an option. Equipment lists are per gym and can have their own unit, for "a kg stack in a lb gym". Assisted machines round the assistance so the set is "never easier than the program says". With no body weight logged, Liftosaur uses 80 kg [web: Liftosaur docs, equipment-and-gyms].
- Boostcamp: step size "Default is twice your smallest plate; set per exercise from 1 to 25 lb or 0.5 to 15 kg" [web].
- Hevy: the warm-up calculator uses "your preferred weight plate and dumbbell rounding", adjustable to the weights you have [web: Hevy workout settings page].
- Alpha Progression accounts for "the available increments" [web].
- 1 lb = 0.45359237 kg exactly, by the 1959 international agreement [web: Wikipedia].

Practical step sizes: a barbell moves by twice the smallest plate (2.5 kg with 1.25 kg plates, 5 lb with 2.5 lb plates, 1 kg with 0.5 kg fractional plates). Dumbbells move by one dumbbell step (2 or 2.5 kg, 5 lb). Stacks move by one pin (5 kg or 10 lb is common; some stacks have 2.5 kg add-on plates). The owner should set the step per exercise, as Boostcamp allows.

Storage precision matters. 190 lb is 86.18255 kg. Stored with two decimals (86.18 kg) it converts back to 189.99 lb. Spotter must store the load as entered with its unit (the `quantity` in `05-substrate-fit.md` does that) or keep at least four decimals of kg.

## 5. Progression models

- **Linear progression**: add a fixed load after every successful session. Liftosaur's `lp(5lb, 2, 0, 10lb, 3)` adds 5 lb after 2 successes and drops 10 lb after 3 failures [web: Liftosaur progressions doc]. Boostcamp uses it for fixed-rep sets [web].
- **Double progression**: work in a rep range at one load; when every set reaches the top, add one step and restart at the bottom. Liftosaur `dp(5lb, 8, 12)` [web]; Boostcamp Auto Progression, where "the weakest set sets the next rep target", "the next session adds one step of weight and resets reps to the bottom of the range", and a logged RPE above the prescribed RPE holds the weight ("Leaving RPE blank never holds it") [web]; Hevy Trainer, which raises the load only after the top of the range on all sets [web: Hevy page].
- **Sum of reps**: Liftosaur `sum(30, 5lb)` adds load when the reps of all sets total 30 [web].
- **RPE autoregulation**: the load follows the RPE of the sets (section 2). Juggernaut AI suggests a weight range from maxes or estimated maxes, lets warm-up performance pick within it, and adjusts "in real-time" from top-set and back-off RPEs [web: JTS help].
- **Percentage programs**: loads are percentages of a tested or estimated 1RM or a training max; Boostcamp programs support "% of 1RM" sets and exclude them from Auto Progression [web].
- **Myo-reps** (Fagerli): an activation set close to failure, then mini-sets of 3 to 5 reps after 3 to 5 deep breaths. Fagerli's stop rule: "End the Myo-rep set when you lose 1 rep from the initial or 5 mini-sets", with `20 +4+4+4+3` as a correct example [web: `02-training-methods.md` section 2.6, from his 2012 article]. A total-reps variant keeps doing mini-sets until a prescribed total. Progression: when the activation set reaches its target reps, add load; StrengthLog's example goes `13 + 4 + 4 + 4 + 3`, then `14 + 4 + 4 + 4 + 4`, then `15 + 5 + 5 + 4 + 4`, then more load [web]. Secondary sources size the mini-set at about a quarter of the activation reps [unverified].
- **Drop sets**: Sødal et al. (2023) cite a load reduction "at around 20–25 %" as common; the included studies used 20 % per drop (Fink, Angleri), 30 % or 20 % per drop in two arms (Fisher), and 20 % then 10 to 15 % (Varović) [web: PMC full text]. No hypertrophy difference against traditional sets [web].
- **Timed sets**: Liftosaur's example progression is `setTime[1] = completedSetTime[1] + 5` (add 5 s) [web]. Hevy shows the previous duration in the PREVIOUS column (01:02 and 00:53 for plank in the owner's screenshot) [web: owner's screenshot].
- **Bodyweight and assisted**: the work is done against body mass plus added load, or body mass minus assistance. Liftosaur models a share of body weight ("lower it for push-ups"), "Bodyweight − assistance" for assisted machines, and reads body weight from Apple Health [web]. Hevy has the exercise types `bodyweight_reps` and `bodyweight_assisted_reps` [src: substrate 02-workouts.md].
- **Unilateral**: Liftosaur logs reps per side, adds both sides for volume, and uses their average for e1RM [web]. RP Hypertrophy's advice is to do the weaker side first and match it with the stronger side (`02-training-methods.md` section 2.10) [src].

## 6. What the apps actually do

- **Hevy**: the PREVIOUS column shows the values of the same row last time (warm-up row against warm-up row, set 2 against set 2, drop against drop) [web: owner's screenshot]. A setting picks the source: "Previous performance on the exercise, regardless of when you did it and as part of what routine", or "from the last time you did that exercise as part of your current routine" [web: Hevy workout settings page]. Tapping a previous value to copy it is not documented in Hevy's settings page or help [unverified; `06-app-teardown.md` agrees]. The free app prescribes nothing. The paid Hevy Trainer gives "a recommended starting weight if you've logged the exercises at least once" and raises the load only after "the upper end of the rep range ... on all prescribed sets" [web: Hevy Trainer page]. Which e1RM formula Hevy uses is not documented [unverified].
- **Strong**: calculates a one-rep max, a warm-up calculator and a plate calculator [web: App Store listing]. No documented suggestion engine [web: App Store listing; `06-app-teardown.md` agrees].
- **Alpha Progression**: "a precise weight and rep target for every set", "calibrated to your actual performance", accounting for "the available increments", combining "changes in both weight and repetitions". "RIR is not required for Alpha Progression's progression algorithm" [web: vendor comparison page, 2026-09-15]. The exact rules are not published.
- **RP Hypertrophy**: the help page on progressions is behind a bot check today. `06-app-teardown.md` records from it: weight up "a few percentage points" a week, a rep added when the next weight step is too big, and set counts from pump, soreness and workload ratings [src: sibling document, marked web there].
- **Juggernaut AI**: weight ranges from (estimated) maxes, warm-ups to pick within the range, RPE adjustments set to set, readiness questions before each session [web: JTS help].
- **Fitbod**: weights from an e1RM "using established prediction equations (such as the Epley formula)", RIR "directly influence[s] how aggressively Fitbod loads your next session", periodic "Max Effort Days" (AMRAP last set) recalibrate the estimate, and new exercises are seeded "from aggregate data across millions of similar users" [web: Fitbod algorithm page, modified 2026-06-09].
- **Liftosaur**: open source (AGPL). Progressions are explicit scripts run after the workout: `lp`, `dp`, `sum`, or custom code with state variables; an `update:` script can change the remaining sets during the workout [web: docs and source].
- **Boostcamp**: Auto Progression (Pro) as in section 5; estimated 1RM by Brzycki from "every logged set of 1 to 12 reps", warm-ups excluded; RPE-based estimates in Pro analytics [web].

Two patterns stand out. Loggers show last time and let the lifter beat it. Coaches prescribe a number. The owner wants the coach's number shown in the logger's calm way (`06-app-teardown.md` finding 5 says the same) [src].

## 7. Which history to use

- **Last session, same position.** This is Hevy's PREVIOUS and the double-progression reference. It matches set order, so fatigue is comparable: set 2 is compared with set 2. It explains itself in one sentence.
- **Best recent e1RM with recency weighting, or a trend.** Fitbod-style. More stable against a bad day, harder to explain, and it hides the number the lifter is trying to beat. Spotter uses it only as an outlier guard and for charts.
- **Missing RPE.** Options: assume failure (Liftosaur's 1RM code), assume the target (neutral), or infer from the rep shortfall. Spotter combines the last two: met the target means "at target effort", fell short means "to failure". With no RPE anywhere the engine behaves like double progression while the sets stay in the range, which is what a Hevy user without RPE expects. It departs from plain double progression in two places: a set short of the range lowers the load instead of repeating it (V7, open question 3), and after a step up the reps come from the e1RM rather than resetting to the bottom of the range (V1).
- **Outliers.** A typed "500" for "50" spoils suggestions (a Hevy App Store review quoted in `06-app-teardown.md`) [src]. A wrong unit is a 2.2× error. A mistyped RPE is a 5 to 15 % error. Spotter drops sets whose e1RM is outside 0.5× to 1.5× of the recent median, and caps how far one suggestion can move the load.
- **Long breaks.** Bosquet et al. (2013), a meta-analysis of 103 studies: training cessation reduced strength, with a dose-response on duration and larger losses over 65 [web: abstract]. A secondary summary says maximal strength was mostly unaffected up to 28 days [web, secondary: Stronger by Science]. Spotter stops adding reps after 28 days off and lowers the e1RM by 5 % (29 to 90 days) or 10 % (more than 90 days). The sizes are a heuristic.
- **Cold start.** Hevy Trainer needs one logged session; Boostcamp marks an exercise with no logged session "Not until after the first session"; Fitbod seeds from population data [web]. Spotter uses the routine's load if it has one, else asks the lifter to pick a load and then adapts from set 1.
- **Same movement, different machines or gyms.** Stack weights are not comparable between machines (pulley ratios, cams, friction). Liftosaur keeps equipment per gym and lets a stack have its own unit [web]. Spotter keys history by exercise and setup: free weights share history across gyms, machines and cables do not unless the owner links them.

## Part B: specification

## 8. Scope and principles

- One pure function: `suggest(pending set, exercise config, today's sets, history, body mass, now) → Suggestion?`. No randomness, no I/O, no clock other than `now`. It runs on iPhone and later on Apple Watch.
- It is re-run whenever anything changes (a set is logged, edited or deleted, the routine changes). Completed sets are never changed by it.
- All loads inside the engine are in the exercise's unit (kg or lb). History in another unit is converted first.
- Integers are exact. Intermediate doubles are compared with the tolerances given (1e-6 before a `floor` of reps, 1e-9 for grid snapping). Tests compare loads, reps, durations and strings exactly; intermediate numbers to 1e-4.

## 9. Inputs

### 9.1 Exercise configuration (per exercise and setup)

| Field                              | Type                                          | Default      | Meaning                                                   |
| ---------------------------------- | --------------------------------------------- | ------------ | --------------------------------------------------------- |
| `unit`                             | kg or lb                                      | owner's unit | unit of every load of this exercise                       |
| `mode`                             | external, bodyweightAdded, bodyweightAssisted | external     | how the load field is read                                |
| `bodyShare`                        | Double 0 to 1                                 | 1.0          | share of body mass moved (bodyweight modes)               |
| `base`                             | Double                                        | see below    | lightest loadable value (empty bar)                       |
| `step`                             | Double                                        | see below    | load step                                                 |
| `maxLoad`                          | Double?                                       | nil          | heaviest loadable value, or most assistance or added load |
| `canAddLoad`                       | Bool                                          | true         | bodyweightAdded only: false for plain bodyweight          |
| `defaultRepsMin`, `defaultRepsMax` | Int                                           | 8, 12        | rep range when the plan has none                          |
| `defaultRPE`                       | Double                                        | 8            | target RPE when the plan has none                         |
| `repGain`                          | Int                                           | 1            | reps added per session at an unchanged load               |
| `dropPercent`                      | Double                                        | 0.20         | load cut per drop                                         |
| `timeStep`                         | Int seconds                                   | 5            | duration added per session                                |
| `myoMaxMiniSets`                   | Int                                           | 5            | mini-set cap                                              |
| `myoStopRule`                      | repDrop, belowTarget                          | repDrop      | Fagerli's rule, or stop below the mini-set target         |

Default `base` and `step` by equipment: barbell 20 kg and 2.5 kg, or 45 lb and 5 lb; dumbbell 0 and 2 kg, or 0 and 5 lb; machine or cable stack 0 and 5 kg, or 0 and 10 lb; kettlebell 0 and 4 kg, or 0 and 5 lb; bodyweight added load 0 and 2.5 kg, or 0 and 5 lb; assisted machine assistance 0 and 5 kg, or 0 and 10 lb. The owner can change each per exercise.

### 9.2 A done set

- `kind`: warmup, normal, failure, drop, myoActivation, myoMini, timed. (In `05-substrate-fit.md` terms: `setType` plus the segment for drops and mini-sets.)
- `setup`: the setup key it was done on.
- `load`: Double? in the stored unit, with `unit`. For bodyweightAdded it is the added load (0 for none); for bodyweightAssisted it is the assistance.
- `reps`: Int?, or `repsLeft` and `repsRight` for unilateral sets.
- `duration`: Int? seconds (timed sets).
- `rpe`: Double? from 1 to 10 in any step. If the store keeps RIR, pass `10 − RIR`.
- `reachedFailure`: Bool.
- `target`: the plan values in force when the set was done: `repsMin`, `repsMax`, `rpe`, `load`, `durationMin`, `durationMax` (all optional).
- `completedAt`: Date. `position`: its order within the exercise in that session.

### 9.3 A past session

`start` (Date), `bodyMass` (kg, optional: the HealthKit body mass in force that day), and its done sets of this exercise in order.

### 9.4 The pending set

Its `kind`, its working ordinal `k` (1 for the first working set, counting normal, failure, myoActivation and timed sets only), its plan `target` (as above, plus `dropPercent`, `myoMiniReps`, `myoTotalReps`), and for drops and mini-sets a link to the parent set.

### 9.5 Output

```swift
struct Suggestion {
    var load: Double?          // exercise unit; added load or assistance in bodyweight modes; nil = lifter picks
    var reps: Int?             // nil for timed sets and drops (to failure)
    var duration: Int?         // seconds, timed sets
    var targetRPE: Double?
    var stop: Bool             // true: no further mini-set
    var reason: Reason
    var text: String           // the explanation shown next to the set
}
enum Reason {
    case warmupAsLastTime, historyAddRep, historyLoadUp, historyLoadDown, historyNewRange, historyAtMax,
         sessionKeep, sessionUp, sessionDown, sessionCoarse, sessionAtMax,
         coldStartRoutineLoad, coldStartPickLoad, myoMini, myoStop, drop, dropAtMinimum,
         timedHistory, timedSession, timedLoadUp, timedColdStart
}
```

## 10. Building blocks

### 10.1 The percentage curve

```swift
// P[i] = fraction of 1RM at n = 1 + 0.5 * i  (RTS chart, section 1.4)
let P: [Double] = [1.000, 0.978, 0.955, 0.939, 0.922, 0.907, 0.892, 0.878, 0.863, 0.850,
                   0.837, 0.824, 0.811, 0.799, 0.786, 0.774, 0.762, 0.751, 0.739, 0.723,
                   0.707, 0.694, 0.680, 0.667, 0.653, 0.640, 0.626, 0.613, 0.599, 0.586, 0.572]
let tailA = 0.572 * 46.0   // 26.312

func p(_ n: Double) -> Double {
    if n <= 1 { return 1 }
    if n <= 16 {
        let i = Int(floor((n - 1) * 2))            // 0...30
        let a = 1 + 0.5 * Double(i)
        if i == 30 || n == a { return P[i] }
        return P[i] + (P[i + 1] - P[i]) * (n - a) / 0.5
    }
    return tailA / (30 + n)
}

func pInv(_ q: Double) -> Double {                 // inverse of p
    if q >= 1 { return 1 }
    if q >= 0.572 {
        for i in 0..<30 where P[i] >= q && q >= P[i + 1] {
            return 1 + 0.5 * Double(i) + 0.5 * (P[i] - q) / (P[i] - P[i + 1])
        }
    }
    return tailA / q - 30
}
```

### 10.2 Effective RPE of a done set

1. If `rpe` is logged: clamp it to 6 ... 10. (Below 6 the RIR estimate is poor; treating it as 6 under-states the e1RM, which is the safe side.)
2. Else if `kind == failure` or `reachedFailure`: 10.
3. Else if `target.repsMin` exists and `reps < target.repsMin`: 10. (It fell short; assume it went to failure.)
4. Else: `target.rpe`, else the exercise's `defaultRPE`.

`logged` is true only in case 1. The explanation shows "at RPE x" only when logged.

### 10.3 Reps of a set

`reps`, or for unilateral sets `min(repsLeft, repsRight)` (the weaker side). If only one side is present, that side. A set is **usable** when it is not deleted, its reps are at least 1 (or its duration at least 1 s for timed sets), and in the external mode its load is above 0.

### 10.4 Effective load

- external: `L = load`.
- bodyweightAdded: `L = bodyShare × BW + added`.
- bodyweightAssisted: `L = bodyShare × BW − assistance`.

`BW` is body mass converted to the exercise unit. For a past set use that session's `bodyMass`, else today's. If no body mass is known at all, a bodyweight exercise has no effective load and uses the reps-only rule (11.5).

### 10.5 The load grid

All rules work on effective loads `L` and a grid `G` of loadable effective loads:

- external: `base + k × step`, `k ≥ 0`, up to `maxLoad`.
- bodyweightAdded: `bodyShare × BW + k × step`, `k ≥ 0`, up to `maxLoad` of added load. If `canAddLoad` is false, `G` has the single point `k = 0`.
- bodyweightAssisted: `bodyShare × BW − k × step`, `k ≥ 0`, while the point is above 0 and `k × step ≤ maxLoad` of assistance.

Operations (round every returned point to 6 decimals):

- `floorG(x)`: the heaviest point `≤ x + 1e-9`; if none, the lightest point. For the external grid: `k = floor((x − base) / step + 1e-9)`, clamped to the allowed `k`. Rounding down means never heavier than the computed load; in the assisted mode it means more assistance. (Liftosaur's "Down" does the opposite on assisted machines: it rounds the assistance down so the set is "never easier than the program says", section 4.)
- `next(L)`: the lightest point `> L + 1e-9`, or nil.
- `prev(L)`: the heaviest point `< L − 1e-9`, or nil.

Display converts back: external `L`; added `L − bodyShare × BW`; assistance `bodyShare × BW − L`.

### 10.6 e1RM and predicted reps

For a usable done set with effective load `L`, reps `r` and effective RPE `e`:

- `n = r + 10 − e`. If `n > 30`, the set has no e1RM (use 11.5).
- `E = L / p(n)`.

Predicted reps at load `L'` and target RPE `R`: `repsAt(L', E, R) = max(0, floor(pInv(L' / E) − (10 − R) + 1e-6))`.

At the set's own load this gives back `floor(r + R − e)`: one RPE point equals one rep.

### 10.7 Selecting evidence

- **Working sets** are `normal`, `failure`, `myoActivation` and `timed` sets. Warm-ups, drops and myo mini-sets are never evidence for working sets, never counted in ordinals, and never change an e1RM.
- **Today's evidence `W`**: the most recently completed usable working set of this exercise and setup today, if any (timed sets only for timed sets, and the reverse).
- **History**: past sessions with this exercise and setup, newest first, excluding today's session.
- **Plausibility**: for a working set in session `S`, take the sessions other than `S` among the five newest within 180 days of `now`. If there are at least 3, let `M` be the median of their session-best e1RMs. The set is implausible when its e1RM is above `1.5 M` or below `0.5 M`. Implausible sets are skipped as references. With fewer than 3 such sessions, every usable set is plausible.
- **Reference `Pref` for working ordinal `k`**: in the newest session that has a usable, plausible working set, take its `k`-th working set if that one is usable and plausible; else its nearest earlier usable, plausible working set; else its last usable, plausible working set. The session's `start` gives the gap `days` (whole local calendar days from `start` to `now`).

### 10.8 Today's target for the pending set

`lo = target.repsMin ?? defaultRepsMin`, `hi = target.repsMax ?? defaultRepsMax` (if `lo > hi`, swap), `R = 10` for a `failure` pending set, else `target.rpe ?? defaultRPE`.

## 11. Rules

The pending set's kind picks the rule: a warm-up uses 11.1, a drop 11.7, a myo mini-set 11.6, a timed set 11.8. Every other working set (normal, failure, myo activation) uses 11.2 if today has evidence `W`, else 11.3 if history has a reference, else 11.4. Rule 11.5 replaces 11.2 or 11.3 when the set has no effective load or `n > 30`.

### 11.1 Warm-up

Find, in the newest session of history, the warm-up with the same warm-up ordinal. If it exists, suggest its load and reps unchanged (`warmupAsLastTime`). Else no suggestion. Warm-ups never affect any other rule.

### 11.2 Working set, with evidence from today (in-session)

Applies when `W` exists. Let `Lw` be its effective load, `e` its effective RPE, `n = rW + 10 − e`.

1. Fatigue allowance `F`: 1.0 if `e = 10`; 0.5 if `9 ≤ e < 10`; 0 otherwise.
2. `E = Lw / p(max(1, n − F))`. If `n > 30`, use 11.5 instead.
3. `r0 = repsAt(Lw, E, R)`.
4. If `lo ≤ r0 ≤ hi`: suggest `(Lw, r0)` (`sessionKeep`).
5. If `r0 > hi` (too easy):
   - If `next(Lw)` is nil: suggest `(Lw, hi)` (`sessionAtMax`).
   - `L = next(floorG(E × p(hi + 1 + 10 − R)))`, or `next(Lw)` if that is nil. This is the lightest load at which the predicted reps are no more than `hi`.
   - `L = max(L, next(Lw))`, then `L = min(L, max(next(Lw), floorG(1.10 × Lw)))`. One suggestion never raises the load by more than one step or 10 %, whichever is larger.
   - `r = min(repsAt(L, E, R), hi)`. If `r < lo`, the step is too coarse: suggest `(Lw, hi)` (`sessionCoarse`). Else suggest `(L, r)` (`sessionUp`).
6. If `r0 < lo` (too hard):
   - `L = floorG(E × p(lo + 10 − R))`, the heaviest load that still gives `lo` reps at `R`. If `L ≥ Lw`, use `prev(Lw)` (or `Lw` if nil).
   - Suggest `(L, max(1, min(repsAt(L, E, R), hi)))` (`sessionDown`).

If the grid has a single point (plain bodyweight), skip 4 to 6 and suggest `(Lw, max(1, r0))` (`sessionKeep`).

There is no rep gain inside a session.

### 11.3 Working set, from history

Applies when `W` does not exist and `Pref` does. Let `LP` be the effective load of `Pref` with the body mass of its session, `LPnow` the effective load of the same setting with today's body mass (equal for external loads), `rP` its reps and `e` its effective RPE.

1. `n = rP + 10 − e`; if `n > 30`, use 11.5. `E = LP / p(n)`. `g = repGain`.
2. Long break: if `days > 90`: `E = 0.90 E`, `g = 0`. Else if `days > 28`: `E = 0.95 E`, `g = 0`.
3. If `lo − 2 ≤ rP ≤ hi + 2` (the same rep scheme):
   - `goal = repsAt(LPnow, E, R) + g`.
   - If `lo ≤ goal ≤ hi`: suggest `(LPnow, goal)` (`historyAddRep`).
   - If `goal > hi`: if `next(LPnow)` is nil, suggest `(LPnow, hi)` (`historyAtMax`). Else `L = next(LPnow)` (exactly one step) and suggest `(L, min(max(repsAt(L, E, R), lo), hi))` (`historyLoadUp`). The reps never go below `lo`, as in classic double progression.
   - If `goal < lo`: `L = floorG(E × p(lo + 10 − R))`; if `L ≥ LPnow`, use `prev(LPnow)` (or `LPnow` if nil). Suggest `(L, max(1, min(repsAt(L, E, R), hi)))` (`historyLoadDown`). No rep gain.
4. Otherwise (the rep scheme changed): `mid = (lo + hi) / 2` (integer division). `L = floorG(E × p(mid + 10 − R))`. Suggest `(L, max(1, min(repsAt(L, E, R), hi)))` (`historyNewRange`).

In step 3, if the grid has a single point (plain bodyweight), suggest `(LPnow, max(1, goal))` with no upper cap (`historyAddRep`).

### 11.4 Working set, cold start

Applies when neither `W` nor `Pref` exists.

- If `target.load` exists: suggest `(floorG(effective target load), lo)` (`coldStartRoutineLoad`).
- Else: suggest `load = nil`, `reps = lo` (`coldStartPickLoad`). The text asks for a load the lifter could lift `floor(lo + 10 − R)` times.

A routine load is only a seed. Once history exists, history wins (open question 8).

### 11.5 Reps-only rule

Used when a working set has no effective load (body mass unknown in a bodyweight mode, or an external load of 0) or `n > 30`. Work on settings (the added load, assistance or load as logged) instead of effective loads, with the same step.

- In-session: `r0 = floor(rW + R − e − F + 1e-6)`. Cross-session: `goal = floor(rP + R − e + 1e-6) + g` (with the long-break `g`).
- In range: keep the setting and suggest that rep count.
- Above `hi`: if a heavier setting exists (one step more added load, one step less assistance, one step more load) and is allowed, suggest it with `lo` reps. Else keep the setting with the predicted reps, uncapped.
- Below `lo`: if a lighter setting exists, suggest it with `lo` reps. Else keep the setting with `max(1, predicted)` reps.

Reasons and texts are those of 11.2 and 11.3.

### 11.6 Myo-rep mini-sets

The activation set is a working set (kind `myoActivation`) and follows 11.2 to 11.4. For mini-set `j` (1-based) after an activation set with load `LA` and reps `rA`, with earlier mini-sets `m1 ... m(j−1)`:

1. Target `t1 = target.myoMiniReps ?? clamp(round(rA / 4), 3, 5)`, rounding halves up. For `j ≥ 2` with `myoStopRule = repDrop`, the target is `m1` (the first mini-set's reps); with `belowTarget` it stays `t1`.
2. Stop (`myoStop`, `stop = true`) if any of these holds:
   - `j − 1 ≥ myoMaxMiniSets`;
   - `repDrop`: some earlier mini-set `mi` with `i ≥ 2` has `mi < m1`;
   - `belowTarget`: some earlier mini-set has fewer reps than `t1`;
   - `target.myoTotalReps` is set and `m1 + ... + m(j−1) ≥ myoTotalReps`.
3. Else suggest `(LA, target)` (`myoMini`). With `myoTotalReps`, the reps are `min(target, myoTotalReps − sum so far)`.

Mini-sets do not change the e1RM. The next session's activation set progresses like any working set.

### 11.7 Drop sets

For a drop after a parent (the working set or the previous drop) with load `Lparent` (external or added load only):

- `d = target.dropPercent ?? dropPercent`. `L = floorG(Lparent × (1 − d))`. If `L ≥ Lparent`, use `prev(Lparent)`.
- If no lighter point exists: no drop (`dropAtMinimum`, `stop = true`).
- Else suggest `(L, reps = nil)`: to failure (`drop`).

Drops do not change the e1RM and do not count as working sets.

### 11.8 Timed sets

Duration targets `tLo = target.durationMin`, `tHi = target.durationMax` (both optional). Let `TP` be the reference timed set from history (same ordinal rule as 10.7) with duration `tP`.

1. With `TP`: gain `s = timeStep`, or 0 if `TP` has a logged RPE above `R`, or 0 if `days > 28`. `goal = tP + s`. If `tLo` exists, `goal = max(goal, tLo)`. If `tHi` exists and `goal > tHi`: if the exercise takes load (`mode == external`, or bodyweightAdded with `canAddLoad`), suggest the next load step with `duration = tLo ?? tP` (`timedLoadUp`); else `goal = tHi`. Suggest the reference load (if any) and `goal` (`timedHistory`).
2. Without `TP`: `duration = tLo` if set, else nil (`timedColdStart`).
3. In-session cap: if a timed working set of this exercise was completed earlier today with duration `tW`, the suggested duration is `min(duration from step 1 or 2, tW)` (`timedSession` when the cap changes it). The lifter is not asked to hold longer than they just did.

Timed sets have no e1RM.

## 12. Explanation strings

Formatting:

- Load: at most two decimals, trailing zeros removed, a space, the unit: `35 kg`, `92.5 kg`, `1.25 kg`, `190 lb`.
- A set: `{load} × {reps}` with U+00D7. Bodyweight: `BW × 10`; added load: `BW + 2.5 kg × 9`; assistance: `BW − 30 kg × 12` (U+2212). Timed: `1:02` (m:ss); with load: `10 kg for 1:02`.
- RPE: at most one decimal, trailing zero removed: `9.5`, `8`.
- Ranges use an en dash: `10–12`.

Templates, where `{prev}` is the reference set formatted, followed by `at RPE {e}` only when its RPE was logged, followed by `({days} days ago)` only when `days > 28`:

| Reason               | Text                                                                                                                                                                                     |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| historyAddRep        | `Last time {prev}; try {new}`                                                                                                                                                            |
| historyLoadUp        | `Last time {prev}, top of range; try {new}`                                                                                                                                              |
| historyLoadDown      | `Last time {prev}, below target; try {new}`                                                                                                                                              |
| historyAtMax         | `Last time {prev}, at the heaviest load; try {new}`                                                                                                                                      |
| historyNewRange      | `Last time {prev}; for {lo}–{hi} reps try {new}` (or `for {lo} reps` when equal)                                                                                                         |
| sessionKeep          | `Set {k}: {prev}; try {new}`                                                                                                                                                             |
| sessionUp            | `Set {k}: {prev}, easier than RPE {R}; try {new}`                                                                                                                                        |
| sessionDown          | `Set {k}: {prev}, harder than RPE {R}; try {new}`                                                                                                                                        |
| sessionCoarse        | `Set {k}: {prev}; next step too big, try {new}`                                                                                                                                          |
| sessionAtMax         | `Set {k}: {prev}, at the heaviest load; try {new}`                                                                                                                                       |
| coldStartRoutineLoad | `First time; routine load, try {new}`                                                                                                                                                    |
| coldStartPickLoad    | `First time: pick a load for about {floor(lo + 10 − R)} reps; do {lo}`                                                                                                                   |
| warmupAsLastTime     | `Warm-up, as last time: {new}`                                                                                                                                                           |
| myoMini              | `Mini-set {j}: {new}`                                                                                                                                                                    |
| myoStop              | `Stop: mini-set {i} got {mi}, first was {m1}` (repDrop), `Stop: mini-set {i} got {mi}, under {t1}` (belowTarget), `Stop: {n} mini-sets done` (cap), `Stop: {total} reps reached` (total) |
| drop                 | `Drop {d}% from {parent load}: {load} to failure`                                                                                                                                        |
| dropAtMinimum        | `No lighter load for a drop`                                                                                                                                                             |
| timedHistory         | `Last time {prev}; try {new}`                                                                                                                                                            |
| timedLoadUp          | `Last time {prev}, top of range; try {new}`                                                                                                                                              |
| timedSession         | `Set {k} held {tW}; try {new}`                                                                                                                                                           |
| timedColdStart       | `First time: hold for {tLo}` or `First time: hold as long as form allows`                                                                                                                |

In session texts, `{k}` is the working ordinal of `W`. In myo texts, `{i}` is the first mini-set that triggered the stop.

## 13. Edge cases

- **Zero reps** (a failed attempt): the set is not usable. It is still shown, and the next working set uses the previous usable one.
- **RPE out of range**: clamp to 6 ... 10 for estimation. Fractional values such as 8.25 work through interpolation.
- **RPE logged on a set that fell short**: the logged RPE wins.
- **Load above `maxLoad` or below `base`**: the grid clamps. At the top, the reasons `historyAtMax` and `sessionAtMax` keep the load and suggest `hi`.
- **History in another unit**: convert with 1 lb = 0.45359237 kg and round to 0.01 before use. Store loads as entered (section 4).
- **Several sessions on one day**: order by `start`; today's own session is never history.
- **Edits**: the engine is pure; editing or deleting a done set changes the next suggestion and nothing else.
- **Exercise replaced mid-session**: the new exercise uses its own history. Sets done on the old exercise stay with it.
- **Supersets**: no effect on the suggestion. Rest rules live in `02-training-methods.md` section 3.
- **AMRAP sets** (kind failure with an open top): treat `hi` as unbounded for the suggestion (no clamp to `hi`).
- **Unilateral with one side logged**: use that side.
- **Body mass**: from HealthKit's latest body mass on or before the session day; with none, bodyweight modes use 11.5.
- **Implausible today's sets**: today's sets are not checked for plausibility. The 10 % cap only limits a raise above the logged load; it does not catch a typo in that load. A logged "500" for "50" makes the next suggestion keep 500 (`sessionKeep`) until the set is edited, so entry validation (design implication 10) is the guard here.
- **Machine or gym changes**: a different setup has its own history. A first session on a new machine is a cold start unless the owner links the setups.

## 14. Test vectors

Defaults unless a vector says otherwise: `defaultRPE` 8, `repGain` 1, gap 7 days, no other history, body mass constant, `myoStopRule` repDrop, `myoMaxMiniSets` 5, `dropPercent` 0.20, `timeStep` 5 s. Grids: barbell kg is `base 20, step 2.5`; stack kg is `base 0, step 5`; dumbbell kg is `base 0, step 2`; barbell lb is `base 45, step 5`. "Target" is the pending set's target; "stored target" is the target recorded on the history set.

**V1. First set from history, top of range reached (load up).** Chest press (machine), stack kg (the exercise in the owner's screenshot). History: set 1 was 35 × 15, no RPE, stored target 10–15 at RPE 9.5. Pending: working set 1, target 10–15 at RPE 9.5. Nothing done today.

- Effective RPE 9.5 (met the target, so assume the target). `n = 15.5`, `p = 0.586`, `E = 35 / 0.586 = 59.726962`.
- `repsAt(35) = floor(15.5 − 0.5) = 15`; goal `15 + 1 = 16 > 15`, so load up one step to 40.
- `q = 40 / 59.726962 = 0.669714`; `pInv = 12 + 0.5 × (0.680 − 0.669714) / 0.013 = 12.395604`; reps `floor(12.395604 − 0.5) = 11`; clamp to 10–15 gives 11.
- Expected: load 40, reps 11, `historyLoadUp`, "Last time 35 kg × 15, top of range; try 40 kg × 11".

**V2. First set from history, add a rep (the owner's example).** Same exercise. History: set 2 was 35 × 13 at RPE 9.5 (logged), stored target 10–15 at RPE 9.5. Pending: working set 2, target 10–15 at RPE 9.5. No working set done today.

- `n = 13.5`, `p = 0.640`, `E = 54.6875`. `repsAt(35) = floor(13.5 − 0.5) = 13`; goal 14, inside 10–15.
- Expected: load 35, reps 14, `historyAddRep`, "Last time 35 kg × 13 at RPE 9.5; try 35 kg × 14".

**V3. In-session, one point easier than the target.** Bench press, barbell kg, sets of 8–12 at RPE 8. Today set 1: 60 × 10 at RPE 7. Pending: set 2, same target.

- `F = 0`, `n = 13`, `E = 60 / 0.653 = 91.883614`, `r0 = floor(13 − 2) = 11`, inside the range.
- Expected: load 60, reps 11, `sessionKeep`, "Set 1: 60 kg × 10 at RPE 7; try 60 kg × 11".

**V4. In-session, much easier than the target (load up).** Same, but set 1 was 60 × 12 at RPE 6.

- `F = 0`, `n = 16`, `E = 60 / 0.572 = 104.895105`, `r0 = 14 > 12`.
- `E × p(15) = 104.895105 × 0.599 = 62.832168`; `floorG = 62.5`; `next = 65`. `next(60) = 62.5`; cap `max(62.5, floorG(66) = 65) = 65`; `L = 65`.
- `q = 65 / 104.895105 = 0.619667`; `pInv = 14 + 0.5 × (0.626 − 0.619667) / 0.013 = 14.243590`; reps `floor(12.243590) = 12`, inside 8–12.
- Expected: load 65, reps 12, `sessionUp`, "Set 1: 60 kg × 12 at RPE 6, easier than RPE 8; try 65 kg × 12".

**V5. In-session, harder than the target (load down).** Squat, barbell kg, sets of 5 at RPE 8 (`lo = hi = 5`). Today set 1: 100 × 5 at RPE 9.5.

- `F = 0.5`; `n = 5.5`, so `n − F = 5.0`; `E = 100 / 0.863 = 115.874855`; `r0 = floor(5 − 2) = 3 < 5`.
- `E × p(7) = 115.874855 × 0.811 = 93.974508`; `floorG = 92.5`.
- `q = 0.798275`; `pInv = 7.5 + 0.5 × (0.799 − 0.798275) / 0.013 = 7.527885`; reps `floor(5.527885) = 5`.
- Expected: load 92.5, reps 5, `sessionDown`, "Set 1: 100 kg × 5 at RPE 9.5, harder than RPE 8; try 92.5 kg × 5".

**V6. In-session fatigue allowance.** Bench press, barbell kg, sets of 8–12 at RPE 8. Today set 1: 60 × 10 at RPE 9.

- `F = 0.5`; `n = 11`, so `n − F = 10.5`; `E = 60 / 0.723 = 82.987552`; `r0 = floor(10.5 − 2) = 8`, inside the range.
- Expected: load 60, reps 8, `sessionKeep`, "Set 1: 60 kg × 10 at RPE 9; try 60 kg × 8".

**V7. Missed reps last time, no RPE.** Barbell row, barbell kg. History: set 1 was 40 × 6, no RPE, stored target 8–12 at RPE 8. Pending: set 1, target 8–12 at RPE 8.

- Effective RPE 10 (fell short of 8). `n = 6`, `E = 40 / 0.837 = 47.789725`. `repsAt(40) = floor(6 − 2) = 4`; goal 5 < 8.
- `E × p(10) = 47.789725 × 0.739 = 35.316607`; `floorG = 35`.
- `q = 0.732375`; `pInv = 10 + 0.5 × (0.739 − 0.732375) / 0.016 = 10.207031`; reps `floor(8.207031) = 8`.
- Expected: load 35, reps 8, `historyLoadDown`, "Last time 40 kg × 6, below target; try 35 kg × 8".

**V8. Warm-up sets are ignored.** V1 plus: the history session also had a warm-up 25 × 15 at RPE 6 before its working sets, and today a warm-up 25 × 15 at RPE 6 is already completed.

- 8a. Pending working set 1: the warm-ups are not `W` and not ordinal 1. Expected: identical to V1 (load 40, reps 11, same text).
- 8b. Pending warm-up 1 (before it is done): expected load 25, reps 15, `warmupAsLastTime`, "Warm-up, as last time: 25 kg × 15".

**V9. Myo-rep mini-sets.** Machine lateral raise, stack kg. Today: activation set 25 × 17 at RPE 9 done. No `myoMiniReps` in the plan, so `t1 = clamp(round(17 / 4 = 4.25) = 4, 3, 5) = 4`.

- 9a. No mini-sets yet. Expected: load 25, reps 4, `myoMini`, "Mini-set 1: 25 kg × 4".
- 9b. Mini-sets 4, 4, 4 done. Target for `j ≥ 2` is `m1 = 4`. Expected: load 25, reps 4, `myoMini`, "Mini-set 4: 25 kg × 4".
- 9c. Mini-sets 4, 4, 4, 3 done. Mini-set 4 has 3 < 4. Expected: `stop = true`, `myoStop`, "Stop: mini-set 4 got 3, first was 4".
- 9d. Mini-sets 5, 5, 5, 5, 5 done. Expected: `stop = true`, `myoStop`, "Stop: 5 mini-sets done".
- Rounding check: `rA = 18` gives `round(4.5) = 5`; `rA = 13` gives 3; `rA = 21` gives 5.

**V10. Drop set.** Seated row (machine), stack kg. The parent set 35 × 8 at RPE 10 is done.

- 10a. `35 × 0.8 = 28`; `floorG(28) = 25`. Expected: load 25, reps nil, `drop`, "Drop 20% from 35 kg: 25 kg to failure".
- 10b. A second drop after 25: `25 × 0.8 = 20`; `floorG(20) = 20`. Expected: load 20, reps nil, "Drop 20% from 25 kg: 20 kg to failure".

**V11. Timed set.** Plank, bodyweight, no duration range, no RPE. History: set 1 held 62 s, set 2 held 53 s (the owner's screenshot).

- 11a. Pending set 1: `62 + 5 = 67`. Expected: duration 67, `timedHistory`, "Last time 1:02; try 1:07".
- 11b. Pending set 2, set 1 not yet done: `53 + 5 = 58`. Expected: duration 58, "Last time 0:53; try 0:58".
- 11c. Pending set 2, today set 1 held 55 s: `min(58, 55) = 55`. Expected: duration 55, `timedSession`, "Set 1 held 0:55; try 0:55".

**V12. Bodyweight with added load.** Pull-up, bodyweightAdded, `bodyShare 1.0`, body mass 80 kg, `canAddLoad`, step 2.5 kg. History: set 1 was BW × 10 at RPE 9 (logged), stored target 6–10 at RPE 9. Pending: set 1, target 6–10 at RPE 9.

- Effective load 80. `n = 11`, `E = 80 / 0.707 = 113.154173`. `repsAt(80) = floor(11 − 1) = 10`; goal 11 > 10, so one step to 82.5.
- `q = 82.5 / 113.154173 = 0.729094`; `pInv = 10 + 0.5 × (0.739 − 0.729094) / 0.016 = 10.309570`; reps `floor(9.309570) = 9`.
- Expected: added load 2.5 (effective 82.5), reps 9, `historyLoadUp`, "Last time BW × 10 at RPE 9, top of range; try BW + 2.5 kg × 9".

**V13. Assisted.** Assisted pull-up machine, bodyweightAssisted, body mass 80 kg, assistance step 5 kg. History: set 1 was assistance 30 × 12 at RPE 8, stored target 8–12 at RPE 8. Pending: set 1, target 8–12 at RPE 8.

- Effective load `80 − 30 = 50`. `n = 14`, `E = 50 / 0.626 = 79.872204`. `repsAt(50) = 12`; goal 13 > 12, so one step heavier: assistance 25, effective 55.
- `q = 55 / 79.872204 = 0.688600`; `pInv = 11.5 + 0.5 × (0.694 − 0.6886) / 0.014 = 11.692857`; reps `floor(9.692857) = 9`.
- Expected: assistance 25, reps 9, `historyLoadUp`, "Last time BW − 30 kg × 12 at RPE 8, top of range; try BW − 25 kg × 9".

**V14. No history.** Bench press, barbell kg, pending set 1, target 8–12 at RPE 8.

- 14a. The routine gives 50 kg. Expected: load 50, reps 8, `coldStartRoutineLoad`, "First time; routine load, try 50 kg × 8".
- 14b. No routine load. `floor(8 + 10 − 8) = 10`. Expected: load nil, reps 8, `coldStartPickLoad`, "First time: pick a load for about 10 reps; do 8".

**V15. Pounds, with history stored in kg.** Bench press, unit lb, barbell lb. History: set 1 stored as 83.9146 kg × 8 at RPE 8 (entered as 185 lb), stored target 6–8 at RPE 8. Pending: set 1, target 6–8 at RPE 8.

- `83.9146 / 0.45359237 = 185.0000255`, rounded to 0.01: 185.
- `n = 10`, `E = 185 / 0.739 = 250.338295`. `repsAt(185) = 8`; goal 9 > 8, so one step to 190.
- `q = 190 / 250.338295 = 0.758973`; `pInv = 9 + 0.5 × (0.762 − 0.758973) / 0.011 = 9.137592`; reps `floor(7.137592) = 7`.
- Expected: load 190, reps 7, `historyLoadUp`, "Last time 185 lb × 8 at RPE 8, top of range; try 190 lb × 7".

**V16. Long break.** Barbell kg. History: set 1 was 60 × 10 at RPE 8, stored target 8–12 at RPE 8, 45 days ago. Pending: set 1, target 8–12 at RPE 8.

- `E = 60 / 0.680 × 0.95 = 83.823529`, `g = 0`.
- `q = 60 / 83.823529 = 0.715789`; `pInv = 10.5 + 0.5 × (0.723 − 0.715789) / 0.016 = 10.725329`; `repsAt = floor(8.725329) = 8`; goal 8.
- Expected: load 60, reps 8, `historyAddRep`, "Last time 60 kg × 10 at RPE 8 (45 days ago); try 60 kg × 8".

**V17. New rep range.** Barbell kg. History: set 1 was 100 × 5 at RPE 8, stored target 5 at RPE 8. Pending: set 1, target 10–12 at RPE 8.

- `5 < 10 − 2`, so the scheme changed. `E = 100 / 0.811 = 123.304562`. `mid = 11`. `E × p(13) = 123.304562 × 0.653 = 80.517879`; `floorG = 80`.
- `q = 0.648800`; `pInv = 13 + 0.5 × (0.653 − 0.6488) / 0.013 = 13.161538`; reps `floor(11.161538) = 11`.
- Expected: load 80, reps 11, `historyNewRange`, "Last time 100 kg × 5 at RPE 8; for 10–12 reps try 80 kg × 11".

**V18. Unilateral.** Bulgarian split squat, dumbbell kg (load per dumbbell). History: set 1 was 20 kg, left 10, right 9, at RPE 8, stored target 8–12 at RPE 8. Pending: set 1, same target.

- Reps `min(10, 9) = 9`. `n = 11`, `E = 20 / 0.707 = 28.288543`. `repsAt(20) = floor(11 − 2) = 9`; goal 10.
- Expected: load 20, reps 10 (each side), `historyAddRep`, "Last time 20 kg × 9 at RPE 8; try 20 kg × 10".

**V19. Outlier skipped.** Seated row (machine), stack kg, target 10–15 at RPE 8. History, newest first: S1 (3 days ago) set 1 = 350 × 12 at RPE 8 (a typo for 35, the only working set); S2 (10 days ago) set 1 = 35 × 12 at RPE 8; S3 (17 days ago) set 1 = 35 × 11 at RPE 8; S4 (24 days ago) set 1 = 30 × 14 at RPE 8.

- e1RMs: S1 `350 / 0.626 = 559.105431`; S2 `35 / 0.626 = 55.910543`; S3 `35 / 0.653 = 53.598775`; S4 `30 / 0.572 = 52.447552`.
- For S1's set, the others are S2, S3 and S4: median `53.598775`, upper bound `80.398162`. 559.1 is above it, so S1's set is implausible. S1 has no other working set.
- For S2's set, the others are S1, S3 and S4: median `53.598775`; 55.91 lies inside `26.799387 ... 80.398162`. S2 is the reference.
- `n = 14`; `repsAt(35) = floor(14 − 2) = 12`; goal 13, inside 10–15.
- Expected: load 35, reps 13, `historyAddRep`, "Last time 35 kg × 12 at RPE 8; try 35 kg × 13".

## 15. Pseudocode for the two main rules

```swift
func inSession(W: DoneSet, k: Int, lo: Int, hi: Int, R: Double, grid: Grid) -> (Double, Int, Reason) {
    let e = effectiveRPE(W); let Lw = effectiveLoad(W); let rW = reps(W)
    let n = Double(rW) + 10 - e
    let F = e >= 10 ? 1.0 : (e >= 9 ? 0.5 : 0.0)
    let E = Lw / p(max(1, n - F))
    let r0 = repsAt(Lw, E, R)
    if grid.isSinglePoint { return (Lw, max(1, r0), .sessionKeep) }
    if (lo...hi).contains(r0) { return (Lw, r0, .sessionKeep) }
    if r0 > hi {
        guard let nx = grid.next(Lw) else { return (Lw, hi, .sessionAtMax) }
        var L = grid.next(grid.floor(E * p(Double(hi + 1) + 10 - R))) ?? nx
        L = max(L, nx)
        L = min(L, max(nx, grid.floor(1.10 * Lw)))
        let r = min(repsAt(L, E, R), hi)
        return r < lo ? (Lw, hi, .sessionCoarse) : (L, r, .sessionUp)
    }
    var L = grid.floor(E * p(Double(lo) + 10 - R))
    if L >= Lw - 1e-9 { L = grid.prev(Lw) ?? Lw }
    return (L, max(1, min(repsAt(L, E, R), hi)), .sessionDown)
}

func fromHistory(P: DoneSet, days: Int, lo: Int, hi: Int, R: Double, grid: Grid, repGain: Int) -> (Double, Int, Reason) {
    let e = effectiveRPE(P); let rP = reps(P)
    var E = effectiveLoadThen(P) / p(Double(rP) + 10 - e)
    var g = repGain
    if days > 90 { E *= 0.90; g = 0 } else if days > 28 { E *= 0.95; g = 0 }
    let LP = effectiveLoadNow(P)
    if rP >= lo - 2 && rP <= hi + 2 {
        let goal = repsAt(LP, E, R) + g
        if grid.isSinglePoint { return (LP, max(1, goal), .historyAddRep) }
        if (lo...hi).contains(goal) { return (LP, goal, .historyAddRep) }
        if goal > hi {
            guard let L = grid.next(LP) else { return (LP, hi, .historyAtMax) }
            return (L, min(max(repsAt(L, E, R), lo), hi), .historyLoadUp)
        }
        var L = grid.floor(E * p(Double(lo) + 10 - R))
        if L >= LP - 1e-9 { L = grid.prev(LP) ?? LP }
        return (L, max(1, min(repsAt(L, E, R), hi)), .historyLoadDown)
    }
    let mid = (lo + hi) / 2
    let L = grid.floor(E * p(Double(mid) + 10 - R))
    return (L, max(1, min(repsAt(L, E, R), hi)), .historyNewRange)
}
```

`n > 30` and missing effective loads route to the reps-only rule (11.5) before these functions run.

## 16. Known limitations

- One curve for every exercise. Above about 10 reps the published data lose fewer %1RM per rep than the chart says, heavy loads most of all; light isolation work loses more below 10 reps (sections 1.2 and 1.5).
- The fatigue allowance ignores the rest actually taken. Rahimi's data show rest length matters (section 3).
- RPE below 7 is poorly estimated by lifters; the engine trusts it down to 6.
- History matching by working ordinal assumes the lifter keeps a similar set count and order. Reordered or added sets fall back to the nearest earlier ordinal.
- The engine does not plan deloads, set counts or weekly volume.

## Design implications for Spotter

1. **Copy the plan onto each set.** The engine needs the target in force when a set was done (`repsMin`, `repsMax`, `rpe`, `load`, `durationMin`, `durationMax`). Without it, missing-RPE inference and the "same rep scheme" test fall back to defaults. The `set` sketch in `05-substrate-fit.md` has no target fields yet.
2. **Store loads as entered, with their unit.** The `quantity` with `kg` and `lb` in `05-substrate-fit.md` fits. Two-decimal kg is not enough to round-trip lb (section 4).
3. **Keep a setup key and a gym on sets or sessions.** Machine history must not mix across machines. The owner needs a way to say "same as" for two setups.
4. **Snapshot body mass per session** (from HealthKit) for bodyweight and assisted exercises, so old e1RMs stay right when body mass changes.
5. **Per-exercise settings live on the exercise record** in substrate (unit, base, step, max, mode, body share, defaults, rep gain, drop percent, time step, myo rules), so the console agent can set them when it writes routines.
6. **Routines written by the agent need fields for**: rep range, target RPE, set kind (warm-up, normal, failure, drop, myo), drop percent, myo mini-set reps, myo total reps, duration range. Section 9.4 lists them.
7. **The engine is a separate Swift package** with no UIKit, HealthKit or network dependency. It runs on iPhone and Apple Watch and takes history as plain values. The app fetches the last six sessions of an exercise and setup within 180 days, which is one list read by exercise and time (`02-workouts.md` section 6 item 5 pattern).
8. **Show two things per set row**: the PREVIOUS value (last time's same row, as in Hevy) and the suggestion with its text. Tap fills the row. The Live Activity's "Next: set 2 of 4" reads the same suggestion.
9. **RPE entry**: 6 to 10 in half steps with the RIR text, optional per set. A missing RPE is a valid state and degrades to a close relative of double progression (section 7), not to a guess.
10. **Validate inputs at entry.** A set whose e1RM is above 1.5× or below 0.5× of the recent median should ask "did you mean ...?" before it reaches history. The outlier guard in section 10.7 is the second line, not the first.
11. **Importers** (Hevy, Strong CSV) bring sets without targets and often without RPE, and Strong's CSV has no unit (`02-workouts.md` section 1.9). The importer must ask for the unit; imported sets use the exercise defaults as their target.
12. **e1RM for charts and records** should use only sets with `n ≤ 12`, where the estimates are most accurate (section 1.2). That is a display rule; the suggestion engine uses what it has.
13. **Calibrate later from the owner's own data**: fit the reps-to-load curve per exercise with the consistency criterion of the Fitbod preprint (same person, exercise and two-week window), and learn the fatigue allowance from consecutive sets at the same load. Both can replace constants without changing the interface.

## Open questions for the owner

1. **Defaults when a routine says nothing**: a rep range of 8–12 at RPE 8 (2 RIR)? Your Hevy logs show RPE 9.5 sets; if you train that close to failure, should the default be 9?
2. **Load up from history by one step only**, with in-session sets doing the rest, or let history jump several steps when the last session was very easy?
3. **Missed reps**: cut the load to reach the bottom of the range at the target RPE (proposed, V7), or repeat the same load once before cutting (classic double progression)?
4. **Change the load inside a session?** Proposed: yes, when the predicted reps leave the range, with a 10 % cap upward. The alternative is pure double progression: one load for all sets, only reps change.
5. **Fatigue allowance**: 1 rep after failure, half a rep after RPE 9 to 9.5, none below. Accept, or start at zero and learn it from your sets?
6. **PREVIOUS source**: the last session with this exercise anywhere (proposed), or the last time inside the same routine (Hevy's second option)?
7. **Gyms and machines**: should machine and cable history be per gym? How do you want to name setups and link two machines as "the same"?
8. **Routine loads**: seed only (proposed), or binding (for percentage programs written by the agent)?
9. **Body mass and body share**: use the latest HealthKit body mass on or before the session day? What share should push-ups and dips use?
10. **Myo-reps**: Fagerli's stop rule (stop when a mini-set loses a rep from the first) or stop below a fixed target? Mini-set target from a quarter of the activation reps, or always set in the routine?
11. **Drops**: 20 % per drop rounded down (V10a lands at 25 kg from 35 kg, 29 % lighter), or round to the nearest step?
12. **Long breaks**: stop adding reps after 28 days and cut 5 % (29 to 90 days) or 10 % (beyond)?
13. **Timed sets**: add 5 s per session, or 10 %? Hold at the top of the range, or keep adding?
14. **Rounding**: always down (proposed, as Liftosaur's default for external loads; on assisted machines Spotter's rule gives more assistance where Liftosaur gives less), or to the nearest step?
15. **Warm-ups**: copy last time's warm-ups (proposed), or generate a percentage ramp from the first working load like Hevy's warm-up calculator?

## Sources

Research on effort, e1RM and fatigue:

- Marzagao T. A Weight-Dependent 1RM Prediction Equation Optimized on 303,494 Near-Failure Sets Across 388 Exercises. arXiv 2603.17495, 2026 (preprint, Fitbod). [arxiv.org/abs/2603.17495](https://arxiv.org/abs/2603.17495)
- One-repetition maximum (formula table). [en.wikipedia.org/wiki/One-repetition_maximum](https://en.wikipedia.org/wiki/One-repetition_maximum)
- Nuzzo JL, Pinto MD, Nosaka K, Steele J. Maximal number of repetitions at percentages of the one repetition maximum. Sports Med 2024;54(2):303-321. [PMC10933212](https://pmc.ncbi.nlm.nih.gov/articles/PMC10933212/)
- Zourdos MC et al. Novel resistance training-specific rating of perceived exertion scale measuring repetitions in reserve. J Strength Cond Res 2016;30(1):267-275. [repository record](https://abacus.universidadeuropea.com/entities/publication/d9d0aaaf-d4cf-49fc-8f9c-896d6f3f886d)
- Helms ER et al. Application of the repetitions in reserve-based rating of perceived exertion scale for resistance training. Strength Cond J 2016;38(4):42-49. [repository record](https://ro.ecu.edu.au/ecuworkspost2013/2114)
- Helms ER et al. RPE vs. percentage 1RM loading in periodized programs matched for sets and repetitions. Front Physiol 2018;9:247. [PMC5877330](https://pmc.ncbi.nlm.nih.gov/articles/PMC5877330/)
- Halperin I et al. Accuracy in predicting repetitions to task failure in resistance exercise: a scoping review and exploratory meta-analysis. Sports Med 2022;52:377-390. [PubMed 34542869](https://pubmed.ncbi.nlm.nih.gov/34542869/)
- Steele J et al. Ability to predict repetitions to momentary failure is not perfectly accurate, though improves with resistance training experience. PeerJ 2017;5:e4105. [peerj.com/articles/4105](https://peerj.com/articles/4105)
- Rahimi R. Effect of different rest intervals on the exercise volume completed during squat bouts. J Sports Sci Med 2005;4(4):361-366. [PMC3899651](https://pmc.ncbi.nlm.nih.gov/articles/PMC3899651/)
- Senna G et al. The effect of rest interval length on multi and single-joint exercise performance and perceived exertion. J Strength Cond Res 2011;25(11):3157-3162. [PubMed 21993029](https://pubmed.ncbi.nlm.nih.gov/21993029/)
- Sødal LK et al. Effects of drop sets on skeletal muscle hypertrophy: a systematic review and meta-analysis. Sports Med Open 2023;9:66. [PMC10390395](https://pmc.ncbi.nlm.nih.gov/articles/PMC10390395/)
- Bosquet L et al. Effect of training cessation on muscular performance: a meta-analysis. Scand J Med Sci Sports 2013;23(3). [record](https://ekoizpen-zientifikoa.ehu.eus/documentos/5f1cda5129995265e44d37a4?lang=en), secondary summary: [Stronger by Science on detraining](https://www.strongerbyscience.com/detraining/)

The RPE chart and its implementations:

- sensAI RPE converter (full chart, reps 1 to 12, RPE 6 to 10). [sensai.fit/tools/rpe](https://www.sensai.fit/tools/rpe)
- Fitness Volt, the Tuchscherer RPE chart explained (reps 1 to 10, RPE 6.5 to 10). [fitnessvolt.com](https://fitnessvolt.com/rpe-training/guides/tuchscherer-chart-explained)
- OpenPowerlifting RPE calculator source. [gitlab.com/openpowerlifting/plsource](https://gitlab.com/openpowerlifting/plsource/-/blob/ba5194be6daa08d082bb1b7959d6f47b82e7802c/static/rpe-calc/index.html)
- Liftosaur `src/models/weight.ts`. [github.com/astashov/liftosaur](https://github.com/astashov/liftosaur/blob/master/src/models/weight.ts)
- RTS load drops and repeats, secondary: [All Things Gym](https://www.allthingsgym.com/mike-tuchscherer-auto-regulation-fundamentals-fatigue-percentages/)

Apps:

- Hevy: [workout settings](https://www.hevyapp.com/features/workout-settings/), [Hevy Trainer](https://www.hevyapp.com/features/workout-plan-generator/), [Previous Workout Values help article](https://help.hevyapp.com/hc/en-us/articles/34105442929943) (bot check today; content via search extract)
- Strong: [App Store listing](https://apps.apple.com/gb/app/id464254577)
- Alpha Progression: [Alpha Progression vs Hevy](https://alphaprogression.com/en/blog/alpha-progression-vs-hevy)
- RP Hypertrophy: [Hypertrophy App Progressions](https://help.rpstrength.com/hc/en-us/articles/43864744488599-Hypertrophy-App-Progressions) (bot check today; see `06-app-teardown.md`)
- Juggernaut AI: [How JuggernautAI is individualized](https://help.jtsstrength.com/en/articles/3-how-juggernautai-is-individualized-to-you), [All about RPE and RIR](https://help.jtsstrength.com/en/articles/2-all-about-rpe-and-rir)
- Fitbod: [The Fitbod algorithm](https://fitbod.me/blog/fitbod-algorithm)
- Liftosaur: [progressions](https://github.com/astashov/liftosaur/blob/master/docs/features/progressions.md), [equipment and gyms](https://github.com/astashov/liftosaur/blob/master/docs/features/equipment-and-gyms.md), [set types](https://github.com/astashov/liftosaur/blob/master/docs/features/set-types.md), [timed sets](https://github.com/astashov/liftosaur/blob/master/docs/features/timed-sets.md)
- Boostcamp: [auto progression](https://www.boostcamp.app/features/auto-progression), [personal records](https://www.boostcamp.app/features/personal-records)

Methods:

- StrengthLog, Myo-reps (2026-05-19). [strengthlog.com/myo-reps](https://www.strengthlog.com/myo-reps/)
- Units: [International yard and pound](https://en.wikipedia.org/wiki/International_yard_and_pound)

In this repository and in substrate:

- substrate `docs/plans/health-data/02-workouts.md` (main `ffd7b967`), read from `/tmp/spotter-substrate-main`.
- `research/workouts/02-training-methods.md`, `research/workouts/05-substrate-fit.md`, `research/workouts/06-app-teardown.md` (sibling slices, 2026-10-30).
- The owner's Hevy screenshots (Log Workout with PREVIOUS column, RPE keypad, plank timed sets).
