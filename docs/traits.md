# Traits and interfaces

Some properties mean the same thing on every kind that carries them. "When
does this sit on the timeline" is one question whether the record is a
calendar event, a dose log, or a task. A **trait** declares such a set of
typed properties once; a kind binds it with one line under `traits:`; and
everything that keys on the trait, the cross-kind queries and the host
behaviors below, covers the kind from that moment
with no further wiring. Host behavior keys on the trait a kind binds, never
on what the kind is called.

This page is the worked tour: what a trait declares, how to write your own,
and what binding buys on the query side.
[The data model](data-model.md#traits) introduces traits in the flow of the
whole model; this page goes deeper on the same ground.

## A trait is a record

`substrate.reamde.dev/core/trait` is a kind like any other, so every declared
trait is a record in your repository: it lists, it GETs, it carries a
`version` the engine maintains and a `source` that says whether it was seeded
(`builtin`), arrived with a provider (`published`) or with anything else a
repository installed (`installed`). Its identity is
`{authority}/{package}/{name}`, and a `traits:` entry is that identity, with
the variant after it where the trait has one
(`substrate.reamde.dev/core/temporal(range)`). That is how a kind binds core's
`temporal` without redeclaring it, and how it binds any other package's trait.
There is no bare-name shorthand: `temporal(range)` is refused, naming every
full spelling the repository declares under the word
([0098](decisions/0098-a-declaration-names-a-kind-or-trait-in-full.md)).

A trait contracts **presence and datatype only**. It carries no cardinality
(a binding kind adds its own `repeated: true`) and no state values (each
binding kind declares its own machine). That is deliberate: the trait
is the shared question, and each kind keeps its own answer's shape.
Core's `observed`, `revised`, `metric`, `source`, `sourcelink` and `result`
are the exceptions, because the engine reads each property's shape. The
loader refuses a binding kind whose `observed` `offsets`, `values`, `codes` or
`origins` is not `repeated: true`, whose `metric` `codeSet` or `rollups` is
not a list, whose `source` `priorities` is not `keyed: true`, or whose
`sourcelink` `sources` is not a list, and one
whose other properties of those traits are lists or keyed maps. `result`
contracts four object properties as `json`, which a declared `object` meets,
and the loader holds their fields.

## Declaring one

A shipped example, verbatim (core's `recurring`, in
[core.yaml](../kinds/substrate.reamde.dev/core/core.yaml)):

```yaml
kind: substrate.reamde.dev/core/trait
metadata:
  id: substrate.reamde.dev/core/recurring
data:
  authority: substrate.reamde.dev
  package: core
  description: "a repeat rule the substrate stores and never expands, with the
    instants it adds, the instants it skips and the zone a time-of-day rule
    resolves in"
  properties:
    recurrence: recurrence
    rdates: datetime
    exdates: datetime
    timezone: timezone
```

`properties` maps a name to a datatype, nothing more. A kind binds it with:

```yaml
traits:
  - substrate.reamde.dev/core/recurring
```

and must then declare those four properties with those datatypes (the
admission checks), plus whatever shape of its own it wants on top:
`task` marks `rdates` and `exdates` `repeated: true` and keeps its due date;
`calendareventseries` keeps its summary and its calendar. A contract may name
`reference` as a datatype (core's `override` does, for `recurrenceOf`): the
binding kind's own declaration then carries the pin and the `onDelete`.

Two of core's traits ask for a third: `recurring` and `override` each expect
`temporal` on the same kind, because a rule with no anchor names no instants
and an override with no `at` of its own sits nowhere. The loader does not
refuse the pair (a repository that copied the `scheduling` sample before
`recurring` moved to core holds exactly that shape, and must keep booting);
a kind that binds either without `temporal` is simply not on the timeline,
and no window read computes from it.

Two refinements exist, both introduced by core's `temporal` and covered in
[the data model](data-model.md#traits): a trait may declare **variants**
(`temporal` is a `point` or a `range`, bound as `temporal(range)`), and a
binding may **rename** where a property lands (`temporal(point: dueAt)` is
how a task's moment is its due date while the task still answers every
temporal query). A trait without variants takes a rename of the one property
it contracts, as [`lifecycle(status: state)`](#a-kinds-status-lifecycle) binds
a kind whose status is called `state`; core's other traits refuse it, since
the engine reads their properties by name
([record 0226](decisions/0226-a-kinds-status-is-the-core-lifecycle-trait-and-names-its-done-and-dropped-states.md)).

## Declaring your own

A trait is a vocabulary declaration, so it enters through the same doors as a
kind: `substratectl apply -f` over the manifest files, the batch
`POST /api/v1/vocabulary/apply`, or a [bundle](bundles.md) that ships it.
One batch, three documents, because every declaration lives in a package and
the batch must open with that package's manifest. A package of your own lives
in the repository's local namespace, `~`
([vocabulary](vocabulary.md#the-declarable-kinds)):

```yaml
kind: substrate.reamde.dev/core/package
metadata:
  id: ~/kitchen
data:
  authority: "~"
  package: kitchen
  version: 1
---
kind: substrate.reamde.dev/core/trait
metadata:
  id: ~/kitchen/perishable
data:
  authority: "~"
  package: kitchen
  description: a thing that stops being good at an instant
  properties:
    expiresAt: datetime
    opened: bool
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ~/kitchen/ingredient
data:
  authority: "~"
  package: kitchen
  description: one thing in the pantry
  names:
    singular: ingredient
  displayTemplate: "{name}"
  traits:
    - ~/kitchen/perishable
  properties:
    name:
      type: string
      required: true
    expiresAt:
      type: datetime
      description: when this stops being good
    opened:
      type: bool
      description: whether the packaging is open
```

From the moment that batch commits, `ingredient` answers every
`perishable`-keyed read below, alongside any other kind, yours or a
bundle's, that binds the same trait:

```http
GET /api/v1/records?filter={"implements":"~/kitchen/perishable"}

→ {"records": [{"id": "oat-milk", "kind": "~/kitchen/ingredient",
                "properties": {"name": "Oat milk", "title": "Oat milk",
                               "expiresAt": "2026-09-04T00:00:00Z", "opened": true}}, …],
   "head": 4207, "generation": "…"}
```

## What binding buys: the queries

**The `implements` filter.** The one generic list, `GET /api/v1/records`,
narrows to a trait's implementors across every package. Alone it means every
implementor in the repository; beside `kinds` it intersects, never unions; and
it composes with the rest of the [filter grammar](api.md#the-filter-grammar),
so a trait's own properties filter and order like any kind's:

```http
GET /api/v1/records?filter={"implements":"temporal",
                             "properties":{"at":{"gte":"2026-08-17T00:00:00Z",
                                                 "lt":"2026-08-19T00:00:00Z"}}}
                    &orderBy=at
```

Run against a repository that added the `tasks` and `calendar` samples,
which land in its local namespace, `~`, that query answers all three kinds in
one ordered page; each record is the flat wire shape, trimmed here:

```json
{"id": "t9", "kind": "~/tasks/task",
 "properties": {"name": "Buy milk", "dueAt": "2026-08-17T06:00:00Z"}}
{"id": "x-cal-standup-20260817", "kind": "~/calendar/calendarevent",
 "properties": {"summary": "Standup", "at": "2026-08-17T09:30:00Z", "endsAt": "2026-08-17T09:45:00Z"}}
{"id": "x-tasklog-tue", "kind": "~/tasks/tasklog",
 "properties": {"status": "done", "at": "2026-08-18T06:20:00Z"}}
```

`temporal` is the one trait whose properties are hot storage columns: `at` and
`endsAt` filter and order off the columns, and a
[window read](api.md#the-window-read) (a filter bounding `at` on both ends,
as above) takes a `temporal(point: dueAt)` kind's slot from its `dueAt`
column, which is why the task lists beside the event. A filter with one bound
or none reads the `at` column alone. Every other trait's properties are the
ordinary declared properties its implementors carry under the trait's names,
so the trait's own
contracted properties are the ones to filter on; a coincidentally shared extra
property is one kind's, not the trait's.

State machines get the same treatment one level down: every state property
filters through `properties` like any other, so "everything with a `status`
of `open`, anywhere" is one query over every kind that declares one.

**The trait endpoint.** `GET
/api/v1/substrate.reamde.dev/core/trait/{id}/implementors` lists the kinds
that bind a trait, each with its declaration; their records are the
`implements` filter above, which is how the console lists every provider's
`accountconfig` accounts. An app's token is answered the implementors its
grant reads.

**An app's trait scope.** An app may ask for every implementor at once,
`read:implements(<trait>)` or `write:implements(<trait>)`: its token reaches
what `*` would among the kinds that bind the trait when each request is
checked, so a kind that binds it later is covered from then on
([scopes](apps.md#scopes),
[0225](decisions/0225-an-apps-scope-may-name-a-trait-and-covers-what-star-covers-among-the-kinds-that-bind-it.md)).

## Where traits do work beyond queries

The trait-not-kind rule is what lets the host build behavior nothing has to
opt into twice:

- **`temporal`** backs the hot columns, so every implementor's time window
  reads are indexed, orderable, and cheap.
- **`recurring`** and **`override`** feed the [window
  read](api.md#the-window-read): a records list whose filter bounds `at` on
  both ends computes every series' occurrences beside the rows, minus its
  `exdates` and minus every slot an override claims
  ([decision 0081](decisions/0081-the-window-read-computes-occurrences-and-recurring-is-core.md)).
  Bind `recurring` on a kind of your own and the same read covers it the day
  it binds; bind `override` too and one occurrence can be moved or edited as
  an ordinary record. The `scheduling` sample's **`occurrencelog`** is how a
  slot gets its done-or-skipped mark: a temporal record of its own in the
  same window.
- **`observed`** marks a kind whose records hold observed values: one value
  per record (`observed(single)`: a reading, a vendor score, a lab result) or
  many values packed into one record over a fixed period (`observed(packed)`:
  an hour of heart rate). Every write of an implementor, a patch included, is
  held to the rules the trait's property list cannot state: `form` is
  `gauge`, `amount`, `counter` or `summary`; a single record holds `value` or
  `code`, never both, except an amount, which may carry its category in
  `code`; a packed record's `offsets` line up with its
  `values` or `codes` inside `[at, endsAt)`; `hold` goes only on a gauge and
  `resetRule` only on a counter; a `revision` never falls. The substrate
  computes a packed record's `stats` from its values and drops any a writer
  sends. The trait's property list never grows: a later need is a second
  trait bound beside it
  ([decision 0154](decisions/0154-an-observed-value-binds-the-core-observed-trait-which-never-gains-a-property.md)).
  An implementor's `metric` reference pins a kind that binds `metric`, or
  `trait: substrate.reamde.dev/core/metric`.
  [Observations](observations.md) has every rule and the sample kinds that
  bind it.
- **`metric`** marks the catalogue entry an observed value names. The
  write rules admit a value in its `unit` and `units`, references to [unit
  records](units.md), the aggregate read takes its `unit` and `bucketPlace`,
  the observed index keeps the rollup tiers its `rollups` lists, and a reader
  shows its `name`, `form`, `codeSet`, `displayPrecision` and `category`.
  Every write is held to the repository's units and to the tier list
  ([observations](observations.md#the-metric-and-source-contracts)).
- **`source`** marks the device, app, service or lab a value came from. Its
  `priorities`, keyed by a metric's full record path, are the order the
  aggregate read's priority policy takes when a request names none.
- **`sourcelink`** marks a record naming, in `sources`, the sources that are
  one device reaching the repository by several routes. Under the aggregate
  read's priority policy a linked device answers each metric and `about`
  from its highest-ranked source alone
  ([observations](observations.md#one-device-by-several-routes)).
- **`result`** marks a single observed kind that holds a laboratory's result
  shapes: `codedValue`, `ratio` and `text` beside `value`, of which a record
  holds exactly one, with the lab's `interpretation` and `referenceRanges`.
  On a kind that does not bind it those names are ordinary properties.
- **`revised`** marks a kind an adapter rewrites from an upstream that
  numbers its versions, such as the `health` catalogue's `sleep` and
  `workout` and the `labs` catalogue's `labreport`. A write of an implementor whose `revision` is below the stored
  one, or that removes it, is refused, the rule `observed` holds for observed
  values, so a stale delivery never reverts a newer result. Its `origin` is
  the provider record the adapter built the row from
  ([decision 0162](decisions/0162-an-observed-row-names-its-origin-and-source-and-composes-its-id-from-upstream-data.md)).
- **`accountconfig`** and **`oauth2`** are how the substrate's OAuth
  facility recognizes a provider account and its client credentials,
  whatever the bundle called its kinds; [bundles](bundles.md) puts them to
  work.
- **`sync`** is how the substrate recognizes the synchronization a
  function drives on an account, whatever the bundle called its streams.
  Bind it beside `accountconfig` and three things happen without the bundle
  doing them: the trigger dispatcher stamps a record-sourced delivery's start
  (`syncState: running`, `lastSyncStartedAt`), its finish (`ok` and
  `lastSyncDurationMs`, in the transaction that commits the body's last
  effects, clearing an older `syncError` and `syncErrorAt` when the run ends
  `ok`) and its park (`erroring`, `syncError`, `syncErrorAt`) onto the
  record under the callable's own actor, when the callable is a function of
  the package that declares the kind; a record whose owner set
  `syncPaused` has those deliveries skipped; and `GET /api/v1/sync/status`
  lists the record, with the OAuth facility's sign-in beside the trait's
  properties, joined with the record triggers on its kind, which the
  Accounts on the console's provider pages and `substratectl sync status`
  read. The body
  owns the rest through its effects: `syncMessage`, `lastSyncedAt`,
  `syncProgress` (`{phase, done, total, pending}`), `syncStreams` (a map of
  stream name to `{cursor, lastAt, pending, state, message, requestedAck}`,
  merged per stream: a write replaces the entries it names, a null entry
  removes one, and the rest stand)
  and `syncRequestedAck`, the acknowledgement of the owner's
  `syncRequestedAt`. A write by the kind's own package that moves
  `lastSyncedAt` and leaves `syncState` at `ok` clears an error pair older
  than the new `lastSyncedAt`, so a schedule-fired run that recovers clears
  it too. An OAuth reconnect clears the error pair and, on an account that
  has synced before and is not paused, stamps `syncRequestedAt` in the same
  patch as the new `tokenStatus`, so the account's on-request trigger runs
  the sync that says whether the new grant works
  ([decision 0176](decisions/0176-a-reconnect-requests-a-sync.md)).
  `syncState` is a string, not a machine: `never`, `running`, `ok`,
  `erroring` or `throttled`
  ([decision 0085](decisions/0085-a-sync-is-a-core-trait-the-dispatcher-stamps.md)).
  [Connections](bundles.md#connections) has the worked example and the
  migration from a bundle's own status strings.

## A kind's status: `lifecycle`

Core's `lifecycle` is for a kind whose records move through states (a task,
an issue, a chore), so an app works over all of them at once
([record 0226](decisions/0226-a-kinds-status-is-the-core-lifecycle-trait-and-names-its-done-and-dropped-states.md)).
It contracts one property, `status: enum`, which an enum or a state machine
meets, and the status lists the values that close a record in `done:` and
`dropped:` ([the reserved keys](vocabulary.md#the-reserved-keys)); every
other value is open. A binding whose status lists no done value is refused,
so no app guesses.

```yaml
traits:
  - "substrate.reamde.dev/core/lifecycle(status: state)"
properties:
  state:
    type: enum
    values: [open, closed]
    done:
      - closed
```

The status may go by another name: `lifecycle(status: state)` binds a kind
whose status is called `state`. To work over every implementor, an app:

- reads each implementor's declaration for the property, its values and
  the two lists;
- reads a kind's open records as those whose status is in neither list. A
  record with no status is open;
- reads the open values the status lists in `proposed:` as suggestions that
  wait for the owner, offering to accept one (a move to an open value in none
  of the lists) or dismiss it (a move to a dropped value)
  ([record 0247](decisions/0247-a-status-names-the-open-states-a-record-waits-in-until-its-owner-accepts-it.md));
- filters each implementor on its own, under that kind's name for the
  status. A renamed status is not called `status`, and a machine's state is
  stored apart from an enum's value;
- writes an enum's status as any of its values;
- moves a machine's status only by a patch along one of its transitions,
  which a put never makes, so it offers a record the moves its current
  state has.

Binding gives the status its meaning, not a way to write it. A write
follows the kind's own rules, so a status its provider's sync owns stays
read-only to an app. A kind binds only where its status says what closed
the work: GitHub's issue does not, since its `state` reads "closed as not
planned" as closed, and binds once its sync derives a finer status.

An app's [trait scope](apps.md#scopes),
`write:implements(substrate.reamde.dev/core/lifecycle)`, reaches every kind
that binds it.
