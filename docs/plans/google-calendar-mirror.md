# Plan: recurrence is core, the window read returns the timeline, and the Google mirror is a copy of the calendar

Status: settled and landed 2026-09-14. Written from a review of
`calendarsync` in
[the Google bundle](../../kinds/providers.substrate.reamde.dev/google/bundle.yaml)
and of what Google's API promises, reshaped the same day on the owner's
direction (recurrence is not a calendar feature and not a second read), then
reworked against two independent reviews of that shape, Codex's and a
Fable-model one (section 9 records what each found and what changed). Section
2 landed as
[decision record 0080](../decisions/0080-the-window-read-computes-occurrences-and-recurring-is-core.md),
which supersedes 0043. It is a plan, not a contract: the code that landed is
the contract, and where the two differ the code is right.

## 1. The problem, twice

**The calendar half.** The Google stream mirrors Google's `singleEvents=true`
expansion: one `event` row per concrete occurrence inside a window that opens
`backfillDepth` back and closes 365 days ahead, and a recurring master is
never a row. Nothing past the horizon exists anywhere and the window is only
re-chosen after a 410. A series edit (delete the series, "this and
following", a split) leaves instance rows that only go if Google enumerates
each as `cancelled`, which it promises for exceptions and deleted single
events and for nothing else. A cleared field survives on the mirror because a
put merges and the sync writes only present values. No series record exists.

**The substrate half.** The same shape is wanted by a medication schedule
that runs five years, gets shifted for two weeks of travel, loses a dose that
was forgotten and has a week replaced when a doctor changes it; by a routine;
by a recurring task. Decision 0039 stores the rule and never expands it into
rows, which is right. Decision 0043 then put the expansion in a SEPARATE read
(`GET /api/v1/occurrences`), made a computed occurrence "not a record" and
assigned the merge of rows and occurrences to every consumer. That is the part
that is wrong: the one read that answers "what is on my timeline this week",
the records read over `temporal` with an `at` window, answers without the
occurrences. The separate read is also broken today: it filters on the shipped
sample spelling of the `recurring` trait
(`internal/api/occurrences.go`), which a sample import rehomes onto the
repository's authority (`internal/vocabulary/rehome.go`), so in a real
repository it matches nothing.

One fix serves both halves.

## 2. Two core traits, and the window read that honours them

### The traits

Two traits join core beside `temporal`, both **contract-style**: like the
`scheduling` sample's `recurring` today, they name properties the binding kind
declares itself (`bindCapability`, `internal/vocabulary/load.go`), so the
kind keeps every knob (`repeated`, `onDelete`, `description`) and no name is
reserved anywhere. (The first draft folded six trait-supplied properties into
`temporal`; the loader has no such shape, only `at`/`endsAt`/`dueAt` are
column-backed, and reserving `timezone` or `recurrence` would refuse core's own
`trigger.schedule` fields and the Google `calendar` mirror. Section 8.)

**`substrate.reamde.dev/core/recurring`**, the series half:

| Property | Type | Meaning |
| --- | --- | --- |
| `recurrence` | `recurrence` | the RFC 5545 rule. The kind's bound temporal slot (`at`, or `dueAt` on a `temporal(point: dueAt)` binding) is `DTSTART`, the first occurrence; on a `range`, `endsAt − at` is every occurrence's duration |
| `rdates` | datetime, repeated | instants the rule does not produce but the record occurs at anyway |
| `exdates` | datetime, repeated | instants the rule produces that do not occur. **An exdate is how one occurrence is cancelled** |
| `timezone` | timezone | the zone the rule's wall clock resolves in, so a daily 09:00 survives DST |

**`substrate.reamde.dev/core/override`**, the exception half:

| Property | Type | Meaning |
| --- | --- | --- |
| `recurrenceOf` | reference, `trait: recurring` | the series this record replaces one occurrence of. The kind chooses `onDelete`: a provider mirror cascades (Google deletes a series' exceptions with it); a user-owned kind should NOT, because the medication split below deletes or shortens the old series and the user's hand-written overrides must outlive it |
| `originalAt` | datetime | the slot the rule produced, which this record replaces. iCalendar's `RECURRENCE-ID` |

Both expect `temporal` on the same kind. (The loader does not refuse the pair
without it, as this plan first said: a repository that imported the
`scheduling` sample before the move holds a series kind bound to the bare
name and no `temporal`, and refusing it would park that package at the next
boot. Such a kind is simply not on the timeline until the sample's upgrade
adds the anchor.) A record with `recurrence` or `rdates` set is a **series**.
A record with `recurrenceOf` set is an **override**: one occurrence of a
series that was moved, edited or given more detail, stored as an ordinary
record. A record with neither is a plain event. A kind may bind both traits
(a medication schedule holds its own overrides) or one (a provider mirror
splits them, section 4). That is iCalendar's whole model, master, `EXDATE`,
override, and it is Google's.

The `scheduling` sample's `recurring` is retired into core's, on the owner's
ruling. A repository that imported `scheduling` holds its own `recurring`, so
a bare `traits: [recurring]` would turn ambiguous at the next boot; the rule
to add, and record: **a bare trait name that core declares resolves to
core's**, a repository copy of the same name is shadowed and the boot logs
it. The properties are the same four, so every existing binding keeps
working. The shipped `scheduling` sample then drops the trait in a version
bump, offered through the ordinary sample upgrade; `occurrencelog` stays a
sample trait, a log marks a slot and does not replace it.

### The window read

A **window read** is the records list (no `q`, no `watch`, no `referencing`)
whose filter bounds `at` with `gte` and/or `lt`, over `implements: temporal`
or over kinds that bind it. It answers, ordered by `at` ascending, with:

- every plain event and every override in the kinds in play whose bound
  slot is in the window, as rows (start-in-window, as the `at` predicate
  means today; span overlap is not this plan's). "Bound slot" fixes a gap
  the window read has today: the `at` filter reads the `at` column alone
  (`recordColumns`, `condProp` in `engine/query.go`), while a
  `temporal(point: dueAt)` kind such as `tasks/task` keeps its slot in
  `due_at`, so tasks never enter a window. `Window` applies the bound to
  each kind's own slot column;
- every **computed occurrence** of every series in the kinds in play: the
  rule from its anchor, plus `rdates`, minus `exdates`, minus every slot an
  override claims through `recurrenceOf` + `originalAt`, cut to the window;
- and NOT the series row itself: a series is on the timeline only through its
  occurrences. Without an `at` bound (a plain list, a get by id, a watch, a
  ranked read) a series is an ordinary row with a rule on it and nothing is
  computed.

**Three predicates, stated separately** (Codex, finding 5). *Candidates*:
series are every record of the kinds in play that passes every filter arm
but the `at` bound (`kinds`, `implements`, `labels`, the other `properties`,
deleted-ness: a timeline filtered to one label shows no occurrences of a
series without it) and matches an indexed predicate "`recurrence` set or
`rdates` non-empty" (a partial expression index over `props`, no new column,
so the fold is untouched), enumerated WHOLE; more than a budget (10,000) is
a `validation` error naming it, never a silently short list. A series whose
rule carries an `UNTIL` before the window is skipped before expansion, so a
series that ended years ago costs a parse and not a walk from `DTSTART`
(`COUNT` still walks; the iteration budget bounds it). *Suppression*: for those series,
every override row with `recurrenceOf` pointing at one of them (the reverse
reference read, `expand.go`) and `originalAt` in the window, whatever its
kind and whether or not its own `at` left the window, suppresses its slot.
*Output*: rows and occurrences pass the caller's filter; an override that
moved out of the window still suppresses but is not returned.

**One snapshot** (finding 6). `substrate.Dataset` gains one read,
`Window(ctx, q)`, that the engine answers inside one transaction: the row
page, the candidate series and the suppressing overrides together. The API
layer expands and merges; the engine still holds no expander (0039 and 0043's
confirmations stand). `internal/api`'s fake implements it like every other
method.

**A provable page** (finding 4). The cursor is the existing opaque cursor
(filter binding, generation and head as today, `keyset` in `query.go`) whose
key is `(at, kind, id)` of the last emitted item. For a page of `first`
items after it: the engine returns the next `first` rows by that key
(`rowBound` = the `at` of the last, or `to` when fewer came); each series is
expanded from the cursor to `rowBound`, capped at `first` slots
(`seriesBound_i` = its last slot when the cap hit, else `rowBound`); the
emit bound is the least of them all; every item at or below the bound is
merged by key and the first `first` are emitted. Every source is complete
below the bound, so the page is ordered and complete, and each page moves the
cursor at least one item. `orderBy` is `at` in either direction and nothing
else on a window read (a `bad_request` names the rule). Descending is the
dear direction: the expander walks forward, so a descending page expands
each series over `[from, bound)` and takes its tail, and a long window pays
its whole expansion per page; ascending is the cheap one. A series whose
rule blows the expander's iteration budget lands in a new `problems` slot on
the page (a golden-file change) and the merge goes on.

**The computed envelope** (findings 7 and 8). A computed occurrence is served
as a record so every consumer renders it unchanged: the series' `kind`; the
id `<seriesId>_<slot>` with the slot as `YYYYMMDDTHHMMSSZ` (`_` is in the id
alphabet, `naming.go`; the suffix is 17 characters, so a series id longer
than 111 is refused at write for a kind binding `recurring`; two slots within
one second of each other in one series are refused the same way); the series'
properties with the bound temporal slot (and `endsAt`) moved to the
occurrence, `recurrence`/`rdates`/`exdates` REMOVED and `recurrenceOf`/
`originalAt` filled in, so the shape is an override's and never both;
`version` 0; the series' `createdAt`/`updatedAt`; and a new top-level
`computed: true` on the wire (`substrate.Record`, `omitempty`, so the golden
and `types.ts` follow), which the CLI and console project under the
envelope's server-owned `status`. `GET` at a computed id answers the same
envelope when the prefix names a live series whose rule produces the slot,
so `get -o yaml | apply -f` is a complete path. `PUT` at that id is an
ordinary write: it **materializes the occurrence as an override** when the
kind binds `override` (the series' own kind, or one the caller names), which
is the one gesture for "move this one", "edit this one", "attach something
to this one"; a kind whose ids are server-assigned because a mapping points
at it refuses, as it refuses every caller-chosen id (an exemption for the
materializing put, which names a slot of an existing record rather than a
subject, is a later record if it is ever needed). `DELETE` at a computed id
is refused naming the gesture that cancels: adding the slot to the series'
`exdates`. Changing a stretch is iCalendar's split, `UNTIL` on the old rule
and a new series for the stretch. Every one of those is an ordinary write,
and the read is right afterwards.

Two rules this touches, acknowledged. **Decision 0014** freezes the id
alphabet and reserves new PATH separators to characters outside it; the
computed id changes neither: `SplitRecordPath` still sees one id segment,
and the `_` inside it is a per-kind lookup rule (parsed only on a miss, for
a kind binding `recurring`, and a stored record at that id always wins),
not a grammar the registry-free split has to know. The new record says so;
0014 stands. **The mapping rule** in `internal/vocabulary/mapping.go` refuses
a reference onto any kind that is a mapping's source ("pin it at the
mapping's `to`"), so while one kind's `recurrenceOf` names another, that
other kind cannot become a mapping source. For Google that means no mapping
may take `google/series` as its `from`; a repository that wants its own
series kind derives it with a function, which is the layer the first draft
already deferred.

**Rule semantics the expander must hold** (finding 12), as tests in
`internal/occurrence`: the anchor is always an occurrence unless exdated
(RFC 5545 `DTSTART`), including on a rule-less, `rdates`-only series;
`COUNT` counts generated slots before exclusions, so exdating one of three
leaves two; the duration is kept in the rule's zone's WALL clock, so an
all-day series that spans local midnight to local midnight keeps doing so
across DST rather than turning into 23 or 25 hours; an `RDATE` in `PERIOD`
form is not representable as a datetime and is reported in `problems`, not
guessed; `exdates`, `rdates` and `originalAt` match a rule's slot by exact
instant (`UnixNano` in `Expand`), so a writer resolves all of them in the
SAME zone as the rule, or an all-day series doubles every exception. An override whose
`originalAt` the rule no longer produces (the rule was edited) is a real
record and stays on the timeline; nothing in the read deletes.

`GET /api/v1/occurrences` is removed. Its one feature not carried over, the
inline log marker, is the log row itself: an `occurrencelog` binds
`temporal(point: scheduledAt)` and lands in the same window on its own.

**The medication case, end to end.** One `medicationschedule` binding
`temporal(point)`, `recurring` and `override`: `at` = first dose,
`recurrence: FREQ=DAILY;UNTIL=…`, `timezone`. Travelling two weeks in
another zone: exdate the fourteen slots and write a two-week series in the new
zone, or override each slot with its shifted `at`. A forgotten dose: its log
row says so and the slot stays (absence means missed, 0039). A week of a
different dose: exdates for the seven and a one-week series carrying the new
dose. Nothing here is calendar-specific and nothing needs a second read.

## 3. What this supersedes and what it touches

- **Decision 0043** (occurrences in a separate read, not records, consumer
  merges) is superseded by the new record. 0039's body is not touched (it is
  frozen); the new record states in its own prose how 0039 continues to
  hold, the substrate still never STORES an expansion, and 0039 may carry
  the optional `amended-by` key. 0079 (one records route) is what this
  builds on, not against; 0063 is untouched, because nothing here renames a
  property (section 4 deprecates instead); 0014 stands (section 2).
- **Core** (`kinds/substrate.reamde.dev/core/core.yaml`) declares the two
  traits; the loader gains the bare-name shadowing rule and the
  requires-`temporal` check; the engine gains `Window`, the partial index and
  the id-length check; the API gains the merge and `computed`; core's package
  version is bumped. On the wire, `Record` gains `computed` and the page
  envelope gains `problems`, both optional; the golden file and `types.ts`
  follow.
- **Samples**: `tasks/task` rebinds from the sample trait to core's by doing
  nothing (the bare name now resolves to core's); `calendar/calendareventseries`
  is redundant (a `calendarevent` with a rule is the series) and the sample
  retires it when it wants; `scheduling` drops `recurring`. Only `calendar`,
  `scheduling` and `tasks` exist in this tree; the health, routines and
  fitness kinds an earlier draft named do not.
- **Docs**: `data-model.md#traits` and `traits.md` describe the two traits
  and the series/override/plain trichotomy; `api.md` documents the window
  read, `computed`, the `_` id, the `at`-only ordering and the candidate
  budget; `terms.md` gains **series**, **override** and **occurrence**;
  `bundles-catalog.md` loses the horizon paragraph.

## 4. The Google mirror

Two kinds in the Google package, written by `calendarsync` alone (record 0049
holds). One kind was the first draft's answer and the loader refuses it:
`event`'s live `recurrence` is a repeated string of verbatim lines, the trait
wants one `recurrence`-typed rule under the same name, a `renamedFrom` may
not coexist with a re-declaration of the old name (`load.go`, the rename
checks), and a retype is a narrowing the upgrade refuses while live rows hold
the old shape. A fresh kind has no history to fight.

**`providers.substrate.reamde.dev/google/series`** (new) binds
`temporal(range)` and `recurring`: `account` (→ `account`, cascade,
required), `calendar` (→ `calendar` mirror, cascade), `calendarId`,
`eventId`, `icalUID`, `recurrence` (the `RRULE`), `rdates`, `exdates`,
`timezone`, `recurrenceLines` (the `recurrence` array verbatim, for
provenance and for regenerating the iCalendar exactly), `cancelledSlots`
(datetime, repeated: the `originalAt` of every cancelled exception Google
sent, kept for the series' lifetime as Google's own contract asks), the
master's own `summary`, `description`, `location`, `status`, `meetingURL`,
`eventType`, `transparency`, `visibility`, `organizer`, `attendees`,
`providerUpdatedAt`, `allDay`, `raw`, `syncGeneration`;
`displayTemplate: "{summary|eventId}"`. `at`/`endsAt` are `DTSTART`/`DTEND`.

**`providers.substrate.reamde.dev/google/event`** (changed) binds
`temporal(range)` and `override`, and holds single events and modified
exceptions: gains `recurrenceOf` (→ `series`, `onDelete: cascade`, no
`mustExist`: an exception may land a page before its master), `originalAt`,
and `calendar` (→ `calendar`, cascade). `startAt`, `endAt` and `recurrence`
are `deprecated: true` and no longer written (`at`/`endsAt` are the trait's
columns; a declared name cannot rename into a column, `reservedProps`), and
the legacy rows that hold them are retracted on the first run below.
`recurringEventId` and `originalStartTime` stay verbatim beside the trait's
two. Every optional property the item lacks is written `null`, which deletes
it (`write.go`, the put merge), so a cleared field clears.

| Google sends | The mirror writes |
| --- | --- |
| an `Event` with `recurrence` (a master) | put `series`: the rule, `rdates`, `at`/`endsAt`, `timezone`, `recurrenceLines`, and `exdates` = its `EXDATE` instants ∪ the stored row's `cancelledSlots`. Recomputed on every master write from those two sources, so a removed `EXDATE` is honoured and a cancellation is never lost |
| an `Event` with `recurringEventId` + `originalStartTime`, not cancelled (a modified exception) | put `event` with `recurrenceOf` → the series, `originalAt` = the slot, its own `at`/`endsAt`. Nothing is written on the series: the read suppresses the slot through the override itself |
| the same, `cancelled` | delete the `event` row if live; put the series with the slot added to `cancelledSlots` (a shell row if the master has not landed yet; the master's own write completes it, a put merges) |
| a plain `Event` | put `event` |
| a `cancelled` master | delete the `series` if live, and delete every `event` whose `recurrenceOf` names it in the same delivery (the cascade also collects them at the next GC; the explicit delete keeps the timeline right in between) |
| a `cancelled` plain event | delete the `event` if live |

**Ids and zones.** The series row id is the derived id of Google's master id
as today (`host.ids.external`, about sixty characters). An exception's row
id and the computed occurrence's id are both `<series row id>_<slot>`, the
slot from `originalStartTime`, so the exception Google sends for a slot and
the occurrence the read computed for it share an id string, in their two
kinds. One zone resolves everything about a series: the master's own
`start.timeZone` when the host can load it, else the calendar's, else UTC,
and that same zone resolves its `EXDATE`/`RDATE` lines (the body's dormant
`_ical_dates` helper, revived: `TZID=`, `VALUE=DATE`, comma lists, `Z`;
`VALUE=PERIOD` reported and skipped), its exceptions' `originalAt` and its
`cancelledSlots`, because the read matches slots by exact instant.

**The walk.** `singleEvents=false`, `showDeleted=true`, `maxResults=250`, and
on a full read `timeMin=floor` with NO `timeMax`: masters are one row each,
so nothing pages forever and no horizon exists. One sync token per calendar
committed from the final page; a 410 and only a 410 drops to a full re-read
under a fresh generation, followed by a sweep of this calendar (`event` rows
with `at ≥ floor` and every `series` row) deleting what the generation did
not stamp; per-account isolation and origin pinning as today. The purge phase
goes: a deleted calendar cascades through the `calendar` reference.

**The first run after the upgrade.** The stored token was minted under
`singleEvents=true` and is undefined to reuse; the instance rows the old walk
wrote would never be re-stamped. The calendar mirror gains `syncWalk`,
stamped `masters` by the new body when the sweep completes, and a token
without it is treated as absent: the first run is the full read under a fresh
generation. Its sweep is wider than the steady-state one, once: every `event`
row carrying `recurringEventId` and no `recurrenceOf` is a legacy instance
and is retracted AT ANY DATE (below the floor too, or its master's computed
past would double it), and the generation sweep covers the rest. A run that
fails before the sweep completes leaves `syncWalk` unstamped and repeats.

**Faithful, testably.** From one calendar's rows a function emits an
iCalendar file: one `VEVENT` per plain event, one per series with its
verbatim lines, one per override with `RECURRENCE-ID` = `originalAt`, and
`EXDATE`s from `exdates`. Fixture in, same calendar out, is phase 2's
acceptance test. And a window read over the two kinds for any month in 2028
is the calendar as Google would draw it, from local rows alone.

## 5. Phases

| # | Work | Size | Depends on |
| --- | --- | --- | --- |
| 0 | Spike against the real API (section 6) | XS | — |
| 1 | Core `recurring` and `override`; bare-name shadowing; requires-`temporal`; `Window` on the dataset and the fake; the merge, `computed`, `GET` of a computed id, the id-length rule; expander semantics tests; `/occurrences` removed; the decision record; docs, golden, `types.ts`; `scheduling` drops its trait | XL | — |
| 2 | The Google mirror: `series`, the `event` changes, the walk, `cancelledSlots`, ids, `syncWalk` and the one-time legacy sweep, the regenerate-the-iCalendar test, package bump | L | 0, 1 |
| 3 | Retire the stale sentences (`bundles-catalog.md`'s horizon paragraph, `originalStartTime`'s "moved" wording), and `calendareventseries` if the sample no longer wants it | S | 2 |

Phase 1 is the substrate half and is worth doing on its own: after it, a
medication schedule appears on the timeline the day it is written.

## 6. The spike (phase 0)

Four behaviours the walk leans on that Google's reference does not state,
each checkable in an hour with a scratch calendar and the owner's token,
recorded in the PR that lands phase 2, and each with the fallback it decides:

1. With `singleEvents=false` and `timeMin`, is a master returned when its
   occurrences fall after `timeMin` but its `DTSTART` is before it? If not:
   the first read of a calendar walks masters without `timeMin` (they are one
   row each) and singles with it.
2. Does a deleted series arrive in a delta as one `cancelled` master? If not:
   a master absent from a full re-read is swept by generation, as planned,
   and the delta path relies on the exceptions' cancellations alone.
3. After "this and following → delete", do the exceptions past the new
   `UNTIL` arrive `cancelled`, or are they orphaned? If orphaned: they stay,
   as the real Google records they are, and the timeline shows them; the
   walk grows no second expander to judge them (a Fable finding), and the
   read does not hide records (section 2).
4. Does an incremental delta honour the initial `timeMin`? Either answer
   works; it decides whether the past edge is Google's or the sweep's.
5. Does Google ever SHRINK a master's `EXDATE` lines (an undo, a client
   PATCH)? The recompute-from-lines rule in section 4 handles it if so; the
   spike says whether the case is real.
6. Does Google treat `DTSTART` as an instance when the rule does not
   produce it? The expander always does (RFC 5545); a mismatch would be a
   doubled or missing first occurrence on the mirror.

## 7. Alternatives considered

- **A separate agenda read, or a CLI that merges the two halves.** The first
  draft; rejected by the owner: it leaves the timeline read wrong, and every
  future consumer rebuilds the merge.
- **Six trait-supplied properties folded into `temporal`.** The second
  draft; the loader has no such shape (variants carry only their own
  properties, and only three names are column-backed), and reserving the six
  names would refuse core's `trigger.schedule` fields and the Google
  `calendar` mirror's `timezone`. Contract-style traits are what exists.
- **Expand in the engine.** Would supersede 0039's confirmation for no gain;
  the API layer sees the same rows through `Window`.
- **Materialize occurrence rows on a schedule.** What 0039 rules out, and the
  horizon problem in another coat.
- **Keep `singleEvents=true` and reconcile per master.** Fights Google's
  model: a paged API call per touched master, a horizon to roll forward, the
  future still somebody else's computation.
- **One Google kind.** Blocked by the `recurrence` retype under live rows
  (section 4). Revisit if the loader ever gains a binding-level rename for
  contract properties.
- **Fetch the window whole under a cap, then cut.** The second draft's
  pagination; a capped candidate set can drop the series with the earliest
  slot and repeat the same prefix forever. Replaced by the bounded merge.

## 8. Risks

- **The loader work is the long pole.** Two traits with a requires rule, the
  bare-name shadowing, and `Window` are three new mechanisms in the
  vocabulary and engine layers before any calendar code runs. Phase 1 is XL
  for that reason and ships in its own PRs.
- **Shadowing changes what a live binding resolves to** on the next boot for
  any repository that imported `scheduling`. The four properties are the
  same, so rows are unaffected; the decision record says so and the boot
  logs each shadowed binding.
- **Per-page expansion cost.** Each page expands every candidate series from
  the cursor to the row bound; a calendar with hundreds of series pays that
  per page. The iteration budget bounds one rule, the candidate budget bounds
  the set, and a page that hits neither is still O(series × slots-in-page).
- **Window size.** A five-year window is legal and pages; nothing is
  truncated, it is just many pages.
- **Google exceptions whose master fell outside `timeMin`.** The
  `recurrenceOf` reference dangles until a full read; the override row is
  correct on its own.
- **GC delay.** A cancelled master's exceptions are deleted explicitly by the
  sync, so the timeline does not wait for GC; a user-written series deleted
  through the API leaves its overrides until the next GC, as any cascade does.

## 9. Review log

Codex reviewed the second draft (2026-09-14) and reported fourteen findings.
How each landed:

| Finding | Resolution |
| --- | --- |
| Trait-supplied optional properties are not declarable | Contract-style traits (section 2); verified in `bindCapability` |
| Reserving six names breaks `trigger.schedule` and `calendar.timezone` | Nothing is reserved |
| Existing bindings do not all anchor at `at` | The anchor is the kind's bound temporal slot, `dueAt` included; `startsAt` goes with `calendareventseries` |
| A capped candidate fetch cannot paginate correctly | Whole enumeration under a budget, bounded k-way merge |
| No separate candidate, suppression and output predicates | Stated separately; overrides suppress whatever their kind or position |
| Three reads lose the snapshot and cursor guarantees | `Window` in one transaction; the existing opaque cursor |
| Computed identity is neither collision-safe nor universally writable | `_` is legal (the draft was wrong); 111-character bound; one-second slot rule; mapping targets refuse |
| Copying the series envelope makes something both series and override | The rule properties are removed on the way out; `computed` is a wire field, `status` a projection |
| One-bump renames are blocked | No renames: `series` is a fresh kind, `event` deprecates |
| The exdates union loses provenance | `EXDATE` from the lines, `cancelledSlots` retained, override suppression derived at read |
| `syncWalk` and cascade do not complete the cleanup | Legacy rows retracted at any date by their shape; explicit exception deletes; `syncWalk` stamped after the sweep |
| DTSTART, COUNT, all-day durations, PERIOD, `originalAt` zone | The expander semantics list, as tests |
| Orphan handling and `timeMin` have no fallbacks | Each spike item names its fallback |
| 0079, 0063, the core version bump, the sample inventory | Section 3 |

A Fable-model review of the same draft ran in parallel and reported seven
groups of findings. Where it agreed with Codex (trait-supplied properties,
reserved names, the renames, `status` on the wire, exceptions outside the
computed id space, the ever-growing union, cascade, DTSTART, all-day
durations, `problems`) the resolutions above cover it. What it added:

| Finding | Resolution |
| --- | --- |
| 0014 reserves new separators to characters outside the id alphabet | Acknowledged in section 2: no path separator changes, the `_` is a per-kind lookup on a miss, a stored record wins; 0014 stands |
| The `at` filter reads the `at` column, so `temporal(point: dueAt)` kinds never enter a window | `Window` applies the bound to each kind's slot column (a pre-existing gap, fixed in phase 1) |
| A reference onto a mapping's source kind is refused | Stated as a limitation: `google/series` cannot be a mapping source; derive with a function |
| Every filter arm must reach the series query | Candidates pass every arm but the `at` bound |
| Descending order must survive | `at` in either direction; descending pays the window's expansion per page |
| Series ended years ago walk from `DTSTART` on every read | `UNTIL` before the window skips expansion |
| Slot matching is exact-instant, so one zone must resolve rule, `EXDATE`s and exceptions | The zone rule in section 4, and the expander semantics list |
| Dropping orphaned Google exceptions needs a second expander in Python | Orphans stay as records; spike 3's fallback changed |
| Cascade on `recurrenceOf` would delete a user's overrides with a split series | The kind chooses; mirrors cascade, user kinds should not |
| 0039 cannot be "amended in prose" | The new record carries the prose; 0039's body is untouched |
| Does Google shrink `EXDATE` lines; is `DTSTART` an instance | Spike items 5 and 6 |

Its remaining suggestion, exempting the materializing put from the
server-assigned-id rule on mapping targets, is deferred to a later record.
