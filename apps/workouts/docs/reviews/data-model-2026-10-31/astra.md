# Independent review: Spotter training data model

> **Where this came from.** Part of the data-model review of 2026-10-31, kept verbatim
> ([the index](../../README.md#reviews)). It reviewed the draft package `mneme.example.org/training`
> in the private repository's `research/workouts/substrate/training/`, the package that became the
> app's own `~/training` in [`Package/`](../../../package/README.md). The worktree paths
> and `/tmp` paths it cites were on the build Mac and are not in this repository.

Reviewed 2026-10-31. Repository: a worktree of the owner's private repository. Paths below are relative to that tree unless prefixed `/tmp/spotter-substrate-main`. No repository files were changed.

## Verdict

Keep the four kinds and the separation between catalogue, prescription and performance. The draft is substantially better suited to strength logging than adopting FHIR, FIT or a vendor schema wholesale. The principal problems are **semantic information lost at capture time**, not missing exercise types: a prefilled RPE becomes indistinguishable from a deliberate rating; mutable catalogue settings reinterpret old loads; and the recorded plan is incomplete. Fix those before collecting more real history. Before V2 writes, fix the codec and concurrency contract. Broader programming and conditioning features can largely be additive.

I independently ran the supplied offline checker with the five training declarations, health's three declarations and all four example files: **10 declaration documents in two packages passed; 158 record envelopes passed**. This does not establish record-property validity, cross-record consistency or runtime safety; the tool expressly leaves those to the server. I inspected the supplied substrate source snapshot rather than contacting production. It has no Git metadata, so its claimed production commit (`ffd7b967`, training README:1155) could not independently be verified with Git.

## Prioritised changes

### 1. Capture RPE acquisition provenance — do before real data

**Evidence:** `research/workouts/substrate/training/set.yaml:223` stores only the value, RIR and scale. `Model/PerformedSet.swift:31` mirrors this. The new Rows behaviour in the brief contradicts the declaration's claim that RPE is “as entered”. `research/workouts/03-progression.md:294` treats any nonmissing RPE as logged evidence. `Present/SuggestionInputs.swift:204` passes the stored value directly into that evidence. Swift paths here and below are under `apps/apple/spotter/SpotterKit/Sources/SpotterCore/`.

**Exact change:** keep `rpe`, `rir`, `rpeScale` and `target.rpe`; add top-level optional `rpeEntry` and `rirEntry` enums, with no default:

```yaml
rpeEntry:
  type: enum
  description: how this effort value was acquired; absent means unknown
  values:
    - {value: entered, label: Explicitly rated}
    - {value: accepted, label: Accepted from the displayed plan}
    - {value: copied, label: Copied from another performance}
    - {value: imported, label: Imported without entry provenance}
# rirEntry has the same declaration for a stored rir.
```

These are acquisition categories, not confidence scores. `accepted` means the user completed a set while the displayed RPE was still the plan's prefill. It does not mean the app measured exertion or the user separately confirmed it. If suggestions/defaults later prefill RPE, add `rpePrefillSource` (`plan`, `suggestion`, `default`) rather than silently broadening “plan”. Keep inferred algorithmic effort out of the observed fields entirely.

**Write rules:**

- Plan 8, tap Done untouched → `rpe: "8", rpeEntry: accepted, target.rpe: "8"`.
- Explicitly select 8, even though 8 was already displayed → `rpeEntry: entered`. Numerical equality must not determine provenance.
- Select 9 → `rpe: "9", rpeEntry: entered`; preserve `target.rpe: "8"`.
- Clear rating → no `rpe` and no `rpeEntry`; leave the target unchanged.
- Copy PREVIOUS → `copied`, not `entered`. A subsequent explicit rating changes it to `entered`.
- Import a Hevy rating → `imported` plus `rpeScale: hevy`; do not assert how the person entered it in Hevy.
- Finish's bulk “mark done” must not mark prefilled effort as explicitly rated. Ideally add a separate `completionMethod: individual | bulk | imported` for this path, because loads and reps can also have been accepted in bulk (`SpotterKit/API.md`, Finish sheet).

Carry this state through row editing, local events, undo/re-tick, persistence, record codec and history. Retain it per chain member; folding must not give an accepted value the provenance of an explicitly rated sibling.

**Consumer policy:** separately compute an effective effort for suggestions. An accepted target may still be the fallback, as today, but cannot become independent confirmation that the target was achieved. Explain it as “assuming RPE 8”, not “at RPE 8”. PR/e1RM analysis should distinguish estimated effort from reported effort. A summary of chain effort should use compatible scales and expose whether its input was inferred.

**Alignment / later cost:** consistent with the distinction between planned targets and performed values in Health Connect, and the source-vs-normalised distinction in substrate's observations/labs. No cited standard supplies this exact UI-provenance enum; it is a necessary local extension. Adding the fields later is cheap; recovering which historic taps accepted defaults is generally impossible. Do not backfill old values as `entered` merely because they differ from the plan, or as `accepted` merely because they match.

### 2. Snapshot the meaning of recorded loads and repetitions — do before real data

**Evidence:** `exercise.yaml:114–143` owns laterality, load basis and implement count. `training/README.md:702–710` explicitly recomputes old volume using their current values. A pair of 20 kg dumbbells and one 20 kg dumbbell currently depend on a mutable catalogue entry to remain distinguishable. Changing an exercise from alternating/per-side counting also reinterprets historical reps. The examples explicitly make implement count a judgement call (`examples/exercises.yaml:38`).

**Exact change:** retain exercise defaults, but copy resolved measurement semantics onto each set:

```yaml
# New optional top-level set fields; require writers to populate for new native data.
loadBasis:    # enum total | perimplement | perside, same values as exercise
implements:   # int, min 1
laterality:   # enum bilateral | unilateral | alternating
repsBasis:    # enum total | perside
bodyShare:    # decimal with the exercise's bounds, when bodyweight is used
```

Also snapshot `measurement` if clients continue using that field to choose historical presentation/calculation. This is evidence about the performed movement, not derived tonnage. Resolve it once when logging; changing today's defaults should not change old facts. An intentional historic correction should be an audited patch. Keep muscle attribution as a versioned derived policy unless historical muscle classifications are specifically required.

For alternating movements, preserve the source's count convention. A source's 21 total lunges cannot losslessly become a single integer per-side count. Use `reps: 21, repsBasis: total`; record separate side members when actual left/right counts are known. Do not infer which side did the extra rep.

**Alignment / later cost:** helps Strong/Hevy/FIT adapters preserve what a weight/count means, though none gives a universal load-basis convention. Substrate's metric compatibility concept similarly recognises that equal units do not imply comparable measures (`observations/metric.yaml:142`). The new fields are additive, but truthful backfill after catalogue edits is not.

### 3. Preserve the full prescription that was actually used — do before real data for performed targets; do soon for V2 session snapshots

**Evidence:** `routine.yaml:340–419` has timer mode, tempo, myo stop rule, drop reps and intra-set pause; `set.yaml:289–384`'s target omits several of them. `session.yaml:45–49` deliberately lacks unperformed planned sets, while `session.yaml:168–190` falls back to the current routine for unchanged rests/groups. `routineVersion` cannot retrieve an old routine (`training/README.md:70–74`).

**Exact change:** add the missing effective fields to `set.target`: `tempo`, `timerMode`, `myoStopRule`, `dropReps`, `intraSetPause`, plus relevant side rules if they govern this set. Keep `restPlanned` as the resolved rest that preceded this performance; do not confuse it with the original plan's rest after the set.

At Start, persist the resolved `session.slots[].sets[]` planned-set snapshot, resolved slot rests and group values, retaining original keys and `routineVersion`. Include incomplete/omitted planned slots in that snapshot. If the current session display prunes empty slots at Finish, keep a separate immutable plan snapshot rather than pruning the only evidence. New routine changes affect new sessions. A user-approved amendment during a session changes the future prescription, while completed sets keep their targets.

This fits the four-level limit without inventing nested program trees. It does not require immutable routine versions as a new kind immediately. It does require the nested-field preservation in item 7.

**Alignment / later cost:** Health Connect separates planned blocks/steps from performed segments; FIT separates workout steps from activity sets. Local data needs both the link and its historical meaning. Adding snapshots later is schema-cheap but cannot recover missed sets, original tempo or the rest defaults used in an old session. V1's local session state mitigates resume risk, not long-term export/history loss (`proposals/087-spotter.md:207–212`).

### 4. Make effort scale handling explicit and scale-aware — do before interpreting imported effort

**Evidence:** `set.yaml:234–242` mixes source brands (`hevy`, `strong`) with a scale (`cr10`); absence means Spotter's RIR scale. `training/README.md:691` says unconditionally “10 − rpe”. `Present/SuggestionInputs.swift:204–206` ignores `rpeScale` entirely.

**Exact change:** retain existing brand-specific tags where wording matters, add an explicit native `rirbased` tag, and write it on new Spotter sets. Document a registry of scale semantics and conversion policy. Keep `rpe` as decimal, never integer. CR10 must not go through `10 − rpe` to infer repetitions in reserve. When both RIR and RPE exist, use the native RIR for RIR-dependent calculations and retain both source values; flag contradictions rather than silently overwriting one.

If supporting Borg 6–20, widen `rpe.max` to 20 and add `borg620`, with conditional bounds enforced by the training writer. Until then preserve unsupported source values in the import origin, not squeezed into 0–10. Do not make the old implicit scale mean “unknown” retroactively; reserve absent-as-legacy-native and use explicit unknown metadata for uncertain imports.

**Alignment / later cost:** Health Connect's segment effort is documented as CR10 in the research's pinned alpha source (`01-formats-and-standards.md:438–447`). [LOINC 82289-0](https://loinc.org/82289-0) describes perceived relative intensity, not a reps-in-reserve calibration; its code does not make all RPE scales interchangeable. Enum widening is cheap. Correcting PRs/suggestions after mixing scales is more costly; recovering an unstored scale may be impossible.

### 5. Distinguish captured times and rest from inferred placeholders — do before importing or claiming measured rest

**Evidence:** `set.yaml:18–25` puts untimed imported sets at session start even though `at` otherwise means set end. `examples/sessions.yaml:68` defines rest as tick-to-Skip/first-touch, including pauses. `Engine/SessionEngine.swift:296` uses the rest clock's end, not necessarily the exercise start. `startedAt` is generally absent on repetition sets.

**Exact change:** retain the useful temporal point, but add `timeBasis: completion | session | estimated` (no backfilled default), and `restMethod: start | interaction | imported | estimated`. For measured starts, write `startedAt`. Add optional `restStartedAt`, `restEndedAt` if exact REST segments are to be exported. Otherwise explicitly treat `restBefore` as a UI-derived estimate and omit exact rest intervals from exports.

A later set's completion minus `restBefore` does not locate the preceding rest: it includes the duration of the set. Nor does an out-of-order slot's previous row identify the chronologically preceding set. Session-start fallback timestamps must not become zero-duration Health Connect exercise segments.

**Alignment / later cost:** FIT and Health Connect have real interval/event semantics. Preserve their supplied times and fractional durations; use display rounding only. `Codec/ISODuration.swift:39–44` currently rounds fractions to whole seconds, so it cannot losslessly round-trip FIT millisecond durations (`01-formats-and-standards.md:483`). Change imported/stored duration representation to exact decimal seconds (or preserve raw duration alongside UI seconds) before enabling such imports. Adding metadata later is cheap; exact unknown intervals cannot be reconstructed.

### 6. Replace ad hoc setup identity before collecting multi-gym history — do soon

**Evidence:** `set.yaml:124–131` gives `setup` a free string such as a gym name, yet exercise defaults own the equipment/load grid. `training/README.md:784–785` defers per-setup settings. A gym name is not a unique machine: a single gym can have two chest presses with different stacks or pulley ratios.

**Exact change:** introduce `training/setup` with stable ID, `name`, optional `location` reference, an exercise reference, machine/attachment/seat notes, and optional `loadBase`, `loadStep`, `loadMax`, and available-load quantities. Prefer `set.setup: reference(kind: training/setup)` before data uses the existing string. If already deployed, add `equipmentSetup` beside the string and migrate; do not simply change its type on live rows. Never cascade deletion of a setup to training history. Carry setup through routine slots and session slots as a default; sets record the actual setup.

Add optional `resistanceLabel` (e.g. a particular band) and eventually `resistance`/configuration references. Do not translate a band colour, an uncalibrated plate number or a machine “level” into kilograms. Positive `assistance` remains right for a calibrated assistance stack.

**Alignment / later cost:** source-device provenance (`observations/source`) is distinct from exercise apparatus; don't reuse the logging device as the machine. FIT/Hevy exercise codes alone do not establish machine equivalence. Retyping a populated setup field is expensive; adding setup kinds while leaving old strings costs a migration but is feasible. Old unrecorded apparatus identity cannot be recovered reliably.

### 7. Make the codec a lossless reader of the actual server representation — do soon, before any V2 writes

Two concrete mismatches undermine the claim that model-to-record round trips future data:

1. `Codec/Properties.swift:197–240` accepts only string references, including nested ones. Substrate serves a reference as an object containing `ref`; strings are an authored write shorthand (`/tmp/spotter-substrate-main/docs/data-model.md:117–126`). `Codec/FlatRecord.swift:31` passes properties through without normalising them. **Change:** accept canonical `{ref: "kind/id"}` (and decorated reference objects) recursively, retaining the target and any declared link fields; keep string support for bundled manifests. Test fixtures must include actual REST response shapes, not only YAML-derived strings.
2. `Codec/Properties.swift:3–7,245–260` explicitly discards unknown nested fields. A new `target.tempo`, percentage load prescription or `session.slots[].sets` snapshot can disappear when an older app re-encodes a record. Required unknown group enums make the record unreadable; unknown defaulted enums can become `straight`/`working`. **Change:** retain `extra` in every nested model or preserve the raw record and patch only the owned fields. Treat unknown execution semantics as unsupported for running the plan, not as a normal straight set.

Also align quantity decoding: `set.distance` has `unit: m` and no `units`, which admits other convertible units on substrate, whereas `PropertyReader.length` accepts only `m` (`Properties.swift:154`). Either normalise accepted units through the unit graph at the API boundary while preserving source quantities, or explicitly narrow the declaration to `[m]` before data exists if metre-only storage is intended. Do not call the current reader a validator for everything the server admits.

**Alignment / later cost:** this is direct substrate compatibility. It can wait while V1 only writes its own bundled shape, as the codec comment says. It cannot wait until after old clients have erased new fields; additive schema evolution is only safe with additive-capable readers.

### 8. Specify cross-record invariants and a real multi-writer contract — do soon, before V2 / agent edits

**Evidence:** `set.session` and `set.exercise` are `mustExist`, but `continues` is not (`set.yaml:95–112,158–169`). No declaration proves that a slot key belongs to that session or that a chain stays in one session. `training/README.md:555–585` admits equal revisions and protects session PATCHes, not all set edits. Substrate's `revised` contract only rejects a lower revision (`/tmp/spotter-substrate-main/docs/traits.md:264–270`).

**Exact changes:** add `mustExist: true` to `set.continues` and require head-before-member outbox ordering. In the common training write path validate unique slot/group/planned-set keys, existing group membership, set-slot membership, consistent chain session/role/ordinal, no self-link/cycle, unique chain positions, and valid ranges/times. Permit different exercises inside an intentional mechanical drop only under an explicit rule; don't accidentally ban the extension by requiring every chain exercise to match.

Use `ifVersion` for every edit of an existing set, routine and exercise, as well as sessions. Maintain the distinction between server record version (concurrency token) and `revision` (upstream delivery order). Device A and B can both produce revision 7, so monotone integers alone do not resolve conflicts. Until a shared-write protocol exists, define one owning session writer and explicit takeover. Add per-set `source`/`adapter` when multiple devices can contribute; session provenance alone cannot identify them.

A console/coach/agent routine edit should be a proposal against a specific base record version, accepted through substrate's existing proposal/write surface. It should never mutate the targets of an already-performed set. Avoid a second uncoordinated approval lifecycle inside the training kinds.

**Later cost:** additive fields are cheap; conflict semantics and writer validation must precede enabling multiple writers. Historical chains with invalid ownership or overwritten equal-revision edits are hard to repair. A cascade collects ownership, not a transaction across all session writes; interrupted multi-record writes still need reconciliation.

### 9. Make imports reproducible without destroying identity — do soon, before the first importer

**Evidence:** `training/README.md:741–748` composes Hevy set IDs from positional indices and purges the parent before reimport. `set.yaml:385` provides `origin`, but source values beyond `sourceType` have no guaranteed preservation contract. The requirements ask to retain source values (`research/workouts/README.md`, requirement 39).

**Exact change:** keep an immutable import source record/blob (`session.file: blobref`, following first-party `health/workout.yaml:125`, or `origin` pointing to a source payload record). Store source account, external workout identity, source modification token, and adapter version there. Scope imported IDs by account as well as provider. Preserve upstream string timestamps/units and unmapped fields in the source payload.

Reconcile the session's complete desired set inventory: update matching stable source IDs, explicitly remove properties no longer supplied, and delete obsolete sets. Where a source supplies only indices, document that these are positions, not permanent set identities; preserve local annotations separately or require conflict resolution on source reorder. Do not promise stable identity that the source does not have.

Parent purge does **not** fully reset children immediately: substrate tombstones cascading children for the sweep, and PUT can restore those rows with old properties (`/tmp/spotter-substrate-main/docs/api.md:358–391`). Therefore a reimport before sweep can retain stale omitted properties; after sweep it can behave differently. It also leaves a failure window in which the entire session is absent. If replacement is retained, explicitly reconcile/purge the children and record the source version/import state, rather than relying on GC timing.

**Alignment / later cost:** mirrors substrate's `origin`, `adapter`, `file` and source-value preservation patterns, including labs' unmappable values (`labs/labresult.yaml:6–13`). Raw provenance needs capturing on day one of importing; a future schema cannot recover discarded source data.

### 10. Separate canonical sport from HealthKit export configuration — do soon

**Evidence:** `session.activityType` only admits four HealthKit activities (`session.yaml:126–134`), while first-party `health/workout.yaml:32–63` has `sport` and `sportSource`. A rowing interval can be represented as a set, but an imported rowing session cannot honestly be assigned one of those four Apple types.

**Exact change:** add `session.sport` using the first-party enum and `sportSource: string`. Rename the draft's `activityType` to `healthKitActivityType` in routine/session now if the field will remain an Apple save option; otherwise retain it for compatibility and document its export-only role. A canonical strength session can still export as traditional or functional strength. Retain native imported activity codes in origin rather than coercing them to strength.

Use optional session `duration` and `distance` for **source-reported** summary values, matching first-party types, when needed. Do not fabricate session duration from set durations or claim rest/pause-subtracted time equals every provider's moving time.

**Later cost:** adding sport is cheap. Renaming a nested or widely populated field is not; deciding the semantic boundary now prevents Apple-specific assumptions spreading to importers and other clients.

### 11. Add expressive prescriptions at the boundary where they are needed — do soon for percent/RIR plans; later for conditioning/programs

The current model already handles RIR targets, timer holds, carries, positive assistance, multiple rests and basic circuits. Do not describe those as absent. The gaps are composition and prescription semantics:

- **Percent load:** add planned-set `loadPrescription` object with `mode: absolute | percentmax | effort | relative`, `percent`, `referenceLoad` (quantity), `referenceSet` (optional reference) and `referenceMethod` (e.g. tested maximum vs training max). This is at depth 3 with scalar fields at depth 4. Copy the resolved reference and prescribed load to `set.target`. “80%” without the reference maximum/date/method is not reproducible. Keep `dropPercent` for drops; don't overload it as percent of 1RM.
- **Effort ranges:** add `rpeMin/rpeMax` and `rirMin/rirMax` to planned sets and targets, or a depth-compatible range object; define precedence/exclusivity with exact `rpe`/`rir`. A 2–3 RIR plan should not require an arbitrary midpoint.
- **Tempo:** present string pattern `routine.yaml:354–358` only supports one digit or X per phase. Later replace with four phase fields supporting a duration or explosive marker if >9-second phases are needed; retain original tempo text on import. Snapshot the current string now (item 3).
- **Conditioning:** adding `emom` to `groupKind` alone is insufficient. Add group `execution` (`rounds`, `emom`, `amraptime`, `intervals`), `timeLimit`, `interval`, `workDuration`, `recoveryDuration`, and explicit performed `round`/`intervalIndex` on sets. Tabata can be a parameterised interval group, not a bespoke set type. Cardio machines additionally need optional source-native resistance setting and performance targets such as pace/power; no need to manufacture reps/load for them.
- **Programs:** a new `program`/block layer should reference routines with ordering/week/day, deload designation and progression policy/version. Keep reusable routine templates separate from schedule occurrences. Do not turn the display `folder` string into a mesocycle, or nest a whole program inside already-deep routine slots.

**Alignment / later cost:** Health Connect blocks/steps distinguish completion goals from performance targets; FIT workout-step repeats offer useful adapter patterns. These are additive except tempo restructuring. Preserve plan snapshots and raw imports now so later engines can reinterpret without losing original intent. A generic programming language or full program kind is unnecessary for the current V1.

## Mapping-table corrections and qualifications

These concern `research/workouts/substrate/training/README.md:809–868`; they are not a request to rename every field to a vendor spelling.

1. **`target.rpe` → Health Connect `AmrapTarget` is wrong** (line 850). `AmrapTarget` says to do as many repetitions as possible, often with a duration completion goal. Map `termination: amrap` to it; map an actual effort target to `RateOfPerceivedExertionTarget`, with explicit RIR-scale/CR10 limitations and integer rounding. [Android API](https://developer.android.com/reference/androidx/health/connect/client/records/ExercisePerformanceTarget.AmrapTarget).
2. **FIT pauses cannot be located from elapsed minus timer time** (line 816). That difference only gives aggregate non-timer time. Use timestamped timer start/stop Event messages to reconstruct spans; if missing, keep only an aggregate duration and mark intervals unknown. [Garmin activity-file guide](https://developer.garmin.com/fit/articles/file-types/activity.html), [encoding guide](https://developer.garmin.com/fit/articles/cookbook/encoding_activity_files.html).
3. **`name` → `HKMetadataKeyWorkoutBrandName` is not a semantic title mapping** (line 818). It is a workout brand name, as the table partly acknowledges with “closest”. Mark native title “none”; a custom metadata key may carry Spotter's session name for cooperating consumers. Do not use a changing session title as a brand. [Apple documentation](https://developer.apple.com/documentation/healthkit/hkmetadatakeyworkoutbrandname?changes=latest_minor&language=objc).
4. **`order` → Health Connect `setIndex` is not one-to-one** (line 832). Multiple segments may share an index; Spotter `order` advances for every chain member. Import/export through logical set identity and a slot-local index map, preserving original segment order separately. The pinned source research already explains this (`01-formats-and-standards.md:442–447`).
5. **`target.plannedSet` → FIT `wkt_step_index` is not an arbitrary string conversion** (line 851). It is a numeric cross-reference to an exported workout step. Build a map from the full planned identity (routine/snapshot, slot key, set key) to a FIT step index. `s1` appears in many slots and is not globally unique. Retain source index in origin for imports with no corresponding routine.
6. **Session ID → FIT `file_id` serial/time is a composite file identity, not a session UUID field** (line 813). A FIT activity can include multiple sessions. Namespace imported identity by file identity plus session index, and preserve Spotter's ID separately if round-trip identity is required. [Garmin activity-file guide](https://developer.garmin.com/fit/articles/file-types/activity.html).
7. **`revision` → Hevy `updated_at` needs an adapter rule** (line 814). A source datetime is not an integer revision counter. Preserve the timestamp and derive a documented monotone revision or use importer-controlled revisions; do not parse it as a direct field rename.
8. **`restBefore` → REST interval and `startedAt/at` → Health Connect segment bounds require known instants** (lines 843–844). A duration plus an unknown set start is insufficient. This mapping must be conditional, not “lossless”.
9. **Session `activityType` row is a default-only mapping** (line 817), not a mapping of every enum choice to strength. HIIT should not blindly export as strength in Health Connect/FIT.
10. **The prose's unconditional RIR conversion is wrong for CR10** (`README.md:691,865–868`); see item 4. FHIR export should preserve the scale too. LOINC 82289-0 is not evidence that a half-step RIR-based strength score equals a general-intensity score.

Two related documentation overclaims should be softened: finding no LOINC/SNOMED code in a search is not proof that none exists in all editions (`research/workouts/README.md:25`, formats research:225–244); and “no standard carries strength down to the set” should mean **none of the evaluated standards carries all required semantics**, since FIT and Health Connect plainly carry set-level subsets.

## What should stay

- **One record per performed set/bout, embedded session slots/groups.** This fits top-level filtering and aggregation without making every row reorder a multi-record rewrite. Keep names `exercise`, `routine`, `session`, `set`; no universal interchange terminology justifies churn.
- **Separate role, structure, termination and outcome.** Keep completed reps, `reachedFailure` and `failedRepAttempted` distinct. Neither an AMRAP target nor a plan to reach failure proves the outcome occurred.
- **Positive assistance and exact quantities.** Keep `load`, not `weightKg`; keep decimals as exact strings in substrate, `kg`/`lb` unit IDs and metre quantities. Export `lb` using the unit record's UCUM `[lb_av]` code, not the literal local ID. Reps remain integer counts, not floating-point weights or a generic metric blob.
- **Raw RPE and RIR coexist.** Do not collapse them into one inferred canonical RIR. Add provenance and scale-aware interpretation instead.
- **Stable UUIDv7 IDs and stable plan keys.** Do not derive identity from exercise names or mutable row positions. Source-composed IDs are right when the upstream actually has stable identity.
- **Cascading session ownership and head-to-member ownership**, with validation and explicit discard policy. Keep exercise/routine/setup deletion noncascading to historical sets; ordinary catalogue cleanup must not erase training history.
- **First-party names `about`, `source`, `origin`, `adapter`, `revision`, `notes`, temporal traits and offsets.** Keep `session.workout` as a link to the health mirror and avoid writing a duplicate first-party workout from Spotter.
- **No speculative indices.** Current reference reads use substrate's reference index (`training/README.md:491–529`); declaring compound indices the planner does not use only increases writes. Benchmark actual history reads after adding provenance/setup filters. Never add an index on quantity just to copy a familiar SQL design.
- **Derived PRs, e1RM, tonnage and muscle volume remain derived.** If cached later, key by algorithm/policy version and input revisions, record evidence/scale eligibility, and invalidate on edits/deletes. No need to turn them into mutable facts on every set now.
- **Keep exercise `codes[]` shaped as `{system, code, version, display}`.** It matches `observations/metric.yaml:38–62` and the coding structure in `clinical/condition.yaml:29–47`. Use a documented registry of short system names and their export URIs; translate to proper URIs for [FHIR Coding](https://hl7.org/fhir/R5/datatypes.html#Coding). Treat Health Connect's broad segment categories as potentially broader mappings, not guaranteed identity with every equipment-specific lift. User/account-specific Strong names and custom Hevy IDs need source scoping. Never auto-merge exercises because two records export to the same broad code.

## Standards boundary and evolution

**FHIR:** use an adapter, not a remodel. The [PGHD Workout profile](https://build.fhir.org/ig/HL7/personal-health-record-format-ig/en/StructureDefinition-pghd-workout.html) is an evolving ballot/CI profile, and the repository research identifies it as R4-based (`01-formats-and-standards.md:188–223`). Do not call an R5 export conformant to that R4 package without validation. Session → Observation and local set components/members are reasonable export shapes; pin the package/version and validate the generated Bundle. Substrate's clinical resource modelling is good precedent for genuine semantic correspondence, not grounds to rename set `load` to clinical `value` or bind training sets to the observation traits.

**LOINC/SNOMED/UCUM:** use verified codes where their meaning fits, retain local codes for strength-specific observations, and expose UCUM at export. A strength-training advice/intervention SNOMED code is not the identity of a performed barbell squat. Generic exertion terminology must carry scale context.

**HealthKit/WorkoutKit:** retain session-level exports and explicit pause events; do not fabricate per-rep metrics or energy. WorkoutKit targets cannot substitute for the strength plan in the evaluated SDK (`01-formats-and-standards.md:399–419`).

**Open mHealth / IEEE 1752.1:** the research describes useful activity/time/distance summaries but no full strength-set semantics (`01-formats-and-standards.md:281–300`). Keep these as summary exports; “movements” is not enough to preserve a load basis or chain. The primary IEEE schema redirect was inaccessible during this review, so I have not independently recertified its latest revision.

**schema.org:** [ExercisePlan](https://schema.org/ExercisePlan) and [ExerciseAction](https://schema.org/ExerciseAction) are reasonable publication metadata. Their generic repetitions/rest/workload vocabulary does not solve execution or sync; notably workload is about energy/output, not a reason to rename lifted mass.

**Evolution:** optional fields/new enum values are generally cheap; a type change, a nested-field rename, requiredness or narrower bounds is not. The supplied loader rejects `renamedFrom` on object fields (`/tmp/spotter-substrate-main/internal/vocabulary/load.go:2508`) and caps nesting at four (`types.go:415–420`). Narrowing is not categorically forbidden after the first record: it is refused when live values violate the new shape, and conversion work has a ceiling. The default 10,000 ceiling counts conversion work, not a guaranteed two-year allowance (`/tmp/spotter-substrate-main/docs/bundles.md:170–203`). Chains and imports can accelerate growth; multiple rewrite steps may touch a record more than once.

Decide **capture semantics, load/repetition basis, setup identity and stable nested target names now**. New program kinds, importer metadata, interval parameters and indices can arrive later. Once data exists, use expand-and-contract through service writes for incompatible changes, with tolerant clients deployed first. Preserve raw source data and honest “unknown” values rather than backfilling precision that was never recorded.
