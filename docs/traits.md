# Traits and interfaces

Some properties mean the same thing on every kind that carries them. "When
does this sit on the timeline" is one question whether the record is a
calendar event, a dose log, or a task. A **trait** declares such a set of
typed properties once; a kind binds it with one line under `traits:`; and
everything that keys on the trait, the cross-kind queries, the GraphQL
interfaces, and the host behaviors below, covers the kind from that moment
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
`{authority}/{package}/{name}`, and traits resolve **across packages**: a kind
binds core's `temporal` without redeclaring it, by bare name while that name is
unique and by full identity always.

A trait contracts **presence and datatype only**. It carries no cardinality
(a binding kind adds its own `repeated: true`) and no state values (each
binding kind declares its own machine). That is deliberate: the trait
is the shared question, and each kind keeps its own answer's shape.

## Declaring one

A shipped example, verbatim
([samples/scheduling/recurring.yaml](../samples/scheduling/recurring.yaml)):

```yaml
kind: substrate.reamde.dev/core/trait
metadata:
  id: samples.substrate.reamde.dev/scheduling/recurring
data:
  authority: samples.substrate.reamde.dev
  package: scheduling
  description: "a repeat rule the substrate stores and never expands, with
    the dates it adds, the dates it skips and the zone a time-of-day rule
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
  - recurring
```

and must then declare those four properties with those datatypes (the
admission checks), plus whatever shape of its own it wants on top:
`medicationschedule` marks `rdates` and `exdates` `repeated: true` and adds a
dose; `calendareventseries` adds its `startsAt` anchor.

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

```graphql
{ records(filter: {implements: "perishable"}) {
    nodes { id title ... on Perishable { expiresAt opened } } } }
```

```json
{ "id": "oat-milk", "title": "Oat milk",
  "expiresAt": "2026-09-04T00:00:00Z", "opened": "true" }
```

(`opened` reads back as the string `"true"`: a derived interface resolves its
fields as strings, the caveat pinned below.)

## What binding buys: the queries

**The `implements` filter.** The one generic list query narrows to a trait's
implementors, across every package. Alone it means every implementor in the
repository; beside `kinds` it intersects, never unions. Over REST it is the
same filter grammar on any collection read; repository-wide it is GraphQL's
`records`:

```graphql
{
  records(filter: {implements: "temporal",
                   properties: {at: {gte: "2026-08-17T00:00:00Z",
                                     lt:  "2026-08-19T00:00:00Z"}}}
          orderBy: [{property: "at"}]) {
    nodes { id kind title ... on Temporal { at } }
  }
}
```

**The trait endpoints.** `GET
/api/v1/substrate.reamde.dev/core/trait/{id}/implementors` lists the kinds
that bind a trait, and `.../trait/{id}/records` pages every record of every
implementor, which is what the console's connections view over
`accountconfig` accounts is.

## The GraphQL interfaces

Every trait that carries properties becomes a GraphQL **interface**, built
mechanically at schema build (`internal/gql/schema.go`); a pure marker trait
adds none. The interface's name is the trait's local name, TitleCased:
`temporal` is `Temporal`, `recurring` is `Recurring`, `perishable` above
would be `Perishable`. Each kind's generated object then implements the
interfaces for the traits it binds, which is what makes one inline fragment
span every implementor:

```graphql
nodes { id kind title ... on Temporal { at } }
```

Run against a repository holding medication schedules, dose logs and calendar
events, that query answers all three in one ordered page:

```json
{ "at": "2026-08-17T06:00:00Z", "id": "levothyroxine-daily",    "kind": "samples.substrate.reamde.dev/health/medicationschedule" }
{ "at": "2026-08-17T09:30:00Z", "id": "x-cal-standup-20260817", "kind": "samples.substrate.reamde.dev/calendar/calendarevent" }
{ "at": "2026-08-18T06:20:00Z", "id": "x-occ-dose-tue",         "kind": "samples.substrate.reamde.dev/health/medicationschedulelog" }
```

Where the interface's fields come from has two arms:

- **`temporal` is special.** Its `at` and `endsAt` are hot storage columns,
  so the interface carries them as real `DateTime` fields, resolved off the
  columns whatever name the binding chose (a task's `dueAt` still answers
  `... on Temporal { at }`).
- **Every other trait's interface** derives its fields from the properties
  its implementors share, resolved as strings. The trait's own contracted
  properties are always in that set, so they are the fields to rely on;
  a coincidentally shared extra property can appear and later vanish as
  implementors come and go.

State machines get the same treatment one level down: every distinct
state-property name becomes a `Has…` interface (`HasStatus`,
`HasProminence`) carrying the state and its stamp timestamps, so "everything
with a status, anywhere" is also one query.
[GraphQL and search](graphql-and-search.md#generated-names-and-scalars) pins
the naming determinism and the collision refusals.

## Where traits do work beyond queries

The trait-not-kind rule is what lets the host build behavior nothing has to
opt into twice:

- **`temporal`** backs the hot columns, so every implementor's time window
  reads are indexed, orderable, and cheap.
- **`recurring`** feeds the occurrences read: `GET
  /api/v1/occurrences?from=&to=` computes every implementor's rule instants
  in a window
  ([decision 0043](decisions/0043-occurrences-expand-at-read-in-the-api-layer.md)),
  and its partner **`occurrencelog`** is how a computed slot gets its
  done-or-skipped mark. Bind `recurring` on a kind of your own and the same
  read covers it.
- **`accountconfig`** and **`oauth2`** are how the substrate's OAuth
  facility recognizes a provider account and its client credentials,
  whatever the bundle called its kinds; [bundles](bundles.md) puts them to
  work.
