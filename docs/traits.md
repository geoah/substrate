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
refuse the pair (a repository that imported the `scheduling` sample before
`recurring` moved to core holds exactly that shape, and must keep booting);
a kind that binds either without `temporal` is simply not on the timeline,
and no window read computes from it.

Two refinements exist, both introduced by core's `temporal` and covered in
[the data model](data-model.md#traits): a trait may declare **variants**
(`temporal` is a `point` or a `range`, bound as `temporal(range)`), and a
binding may **rename** where a property lands (`temporal(point: dueAt)` is
how a task's moment is its due date while the task still answers every
temporal query).

## Declaring your own

A trait is a vocabulary declaration, so it enters through the same doors as a
kind: `substratectl apply -f` over the manifest files, the batch
`POST /api/v1/vocabulary/apply`, or a [bundle](bundles.md) that ships it.
One batch, three documents, because every declaration lives in a package and
the batch must open with that package's manifest:

```yaml
kind: substrate.reamde.dev/core/package
metadata:
  id: pantry.example/kitchen
data:
  authority: pantry.example
  package: kitchen
  version: 1
---
kind: substrate.reamde.dev/core/trait
metadata:
  id: pantry.example/kitchen/perishable
data:
  authority: pantry.example
  package: kitchen
  description: a thing that stops being good at an instant
  properties:
    expiresAt: datetime
    opened: bool
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: pantry.example/kitchen/ingredient
data:
  authority: pantry.example
  package: kitchen
  description: one thing in the pantry
  names:
    singular: ingredient
  displayTemplate: "{name}"
  traits:
    - perishable
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
GET /api/v1/records?filter={"implements":"perishable"}

→ {"records": [{"id": "oat-milk", "kind": "pantry.example/kitchen/ingredient",
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

Run against a repository (authority `ada.example.com`) that imported the
`tasks` and `calendar` samples, that query answers all three kinds in one
ordered page; each record is the flat wire shape, trimmed here:

```json
{"id": "t9", "kind": "ada.example.com/tasks/task",
 "properties": {"name": "Buy milk", "dueAt": "2026-08-17T06:00:00Z"}}
{"id": "x-cal-standup-20260817", "kind": "ada.example.com/calendar/calendarevent",
 "properties": {"summary": "Standup", "at": "2026-08-17T09:30:00Z", "endsAt": "2026-08-17T09:45:00Z"}}
{"id": "x-tasklog-tue", "kind": "ada.example.com/tasks/tasklog",
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
that bind a trait; their records are the `implements` filter above, which is
how the console lists every provider's `accountconfig` accounts.

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
  effects) and its park (`erroring`, `syncError`, `syncErrorAt`) onto the
  record under the callable's own actor; a record whose owner set
  `syncPaused` has its deliveries skipped; and `GET /api/v1/sync/status`
  lists the record joined with the record triggers on its kind, which the
  Accounts on the console's provider pages and `substratectl sync status`
  read. The body
  owns the rest through its effects: `syncMessage`, `lastSyncedAt`,
  `syncProgress` (`{phase, done, total, pending}`), `syncStreams` (a map of
  stream name to `{cursor, lastAt, pending, state, message, requestedAck}`)
  and `syncRequestedAck`, the acknowledgement of the owner's
  `syncRequestedAt`.
  `syncState` is a string, not a machine: `never`, `running`, `ok`,
  `erroring` or `throttled`
  ([decision 0085](decisions/0085-a-sync-is-a-core-trait-the-dispatcher-stamps.md)).
  [Connections](bundles.md#connections) has the worked example and the
  migration from a bundle's own status strings.
