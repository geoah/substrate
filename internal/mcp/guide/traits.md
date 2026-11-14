# Traits

What a trait is, how a kind binds one, and what each core trait gives a kind: the timeline, repeats, status, accounts, sync and observations.

## What a trait is

- A trait is a contract several kinds share: a set of property names and datatypes. A kind that binds it is covered by everything the substrate does for that trait (the timeline, window reads, sync stamping, the observation rules) and by every cross-kind read over it.
- A trait is a record of `substrate.reamde.dev/core/trait`. Read one in full before binding it: `query` with `kind` `substrate.reamde.dev/core/trait` and `id` the trait reference (`substrate.reamde.dev/core/recurring`).
- Records of every kind that binds a trait: `query` with `filter` `{"implements": "substrate.reamde.dev/core/temporal"}`. It intersects with `kinds`. `describe_kind` shows a kind's `traits`.

## Binding

A kind lists its traits beside `properties`, each named in full (a bare `temporal` is refused):

```json
{"traits": ["substrate.reamde.dev/core/temporal(point: dueAt)",
           "substrate.reamde.dev/core/recurring",
           "substrate.reamde.dev/core/lifecycle"]}
```

- `trait(variant)` picks a variant where the trait has them: `temporal(point)`, `temporal(range)`, `observed(single)`, `observed(packed)`. A trait with variants refuses a binding without one.
- `temporal(point: dueAt)` moves the point onto the `dueAt` column. `lifecycle(status: state)` binds a status property named `state`. No other core trait takes a rename.
- The kind declares every non-column property the trait contracts, with that datatype (a `state` meets `enum`, an `object` meets `json`), plus its own descriptions and shape. A missing one refuses the batch, naming it.
- `at`, `endsAt` and `dueAt` are columns every record carries: never declare them. Write them as ordinary properties of a kind that binds `temporal`.

## For the owner's kinds

### substrate.reamde.dev/core/temporal

- Puts each record on the timeline. `temporal(point)` uses `at`; `temporal(range)` uses `at` and `endsAt` (an event, a stay); `temporal(point: dueAt)` uses `dueAt` for when something is due.
- `dueAt` exists only through that binding: the trait's own record lists `at` and `endsAt` alone.
- Bind it when the owner will ask what happens when. A `query` filter bounding `at` on both ends (`gte`/`gt` and `lt`/`lte`) is a window read; a `point: dueAt` kind answers it from `dueAt`.

### substrate.reamde.dev/core/recurring

- A repeat on a fixed calendar. The kind declares all four: `recurrence` (type `recurrence`, an RFC 5545 rule such as `FREQ=WEEKLY;BYDAY=MO` or `FREQ=MONTHLY;BYMONTHDAY=1`), `rdates` and `exdates` (type `datetime`, `repeated: true`: occurrences added and skipped), `timezone` (type `timezone`, where a time-of-day rule resolves).
- Bind `temporal` beside it: the record's `at` (or `dueAt`) is the first occurrence. Without `temporal` the kind loads but no window read expands it.
- The substrate stores the rule and never expands it into records. A window read answers each occurrence inside the window, computed: `computed: true`, `version` 0, id `<seriesId>_<slot>` (slot in UTC, `YYYYMMDDTHHMMSSZ`). The series record itself is not on that page.
- Cancel one occurrence by adding its slot to `exdates`. Put no RDATE or EXDATE lines in `recurrence`.
- Bind it for what keeps to the calendar whether or not it was done: a bill on the 1st, a class every Monday, a pill at 9:00. `samples.substrate.reamde.dev/tasks/task` binds it with `temporal(point: dueAt)`.
- Not for "again N days after the last time" (watering, a haircut, an oil change): that is no calendar rule. Bind `temporal(point: dueAt)` for when it is next due, declare the interval in days (`int`) and when it was last done, and move `dueAt` on when it is done. When the owner has not said which of the two it is, ask.

### substrate.reamde.dev/core/override

- One occurrence of a series, moved or edited, as an ordinary record. The kind declares `recurrenceOf` (a `reference`, pinned at the series kind or `"trait": "substrate.reamde.dev/core/recurring"`) and `originalAt` (`datetime`, the slot it replaces), and binds `temporal` too.
- When the series' kind binds it, a computed occurrence carries both, and a `write` `put` of that envelope at its id makes it an override. Do not cascade an owner's override on its series (`onDelete`).

### substrate.reamde.dev/core/lifecycle

- A kind whose records move through states (a task, a chore). It contracts `status`, an `enum` or a `state`, single-valued, which must list at least one value under `done` (and may list `dropped`); every other value is open, and a record with no status is open.
- A `state` status moves only by a `patch` along a declared transition. Binding gives the status meaning to apps, not a way to write it.

## For providers and observations

Most owners never bind these: they come with installed providers and catalogues. Read them to understand records you meet.

- **`substrate.reamde.dev/core/accountconfig`**: one connected account. `tokenRef` (`secret`), `tokenStatus`, `grantedScopes`, written only by the OAuth facility (`writer: system`). The owner connects accounts on the console's Providers page.
- **`substrate.reamde.dev/core/oauth2`**: the OAuth client credentials, `clientId` and `clientSecret` (`secret`), on a provider's client input.
- **`substrate.reamde.dev/core/sync`**: the sync a provider's function drives on an account. Twelve properties: `syncState` (`never`, `running`, `ok`, `erroring`, `throttled`), `syncMessage`, `lastSyncedAt`, `lastSyncStartedAt`, `lastSyncDurationMs`, `syncRequestedAt`, `syncRequestedAck`, `syncPaused`, `syncProgress`, `syncError`, `syncErrorAt`, `syncStreams`. A person writes `syncRequestedAt` (ask for a run) and `syncPaused` (`writer: person`), which an assistant's token, writing as its app, may not; the rest are the sync function's (`writer: [function, agent]`). `triggers` with `action` `sync` reads every account's sync state, and `wake` runs a sync's trigger now.
- **`substrate.reamde.dev/core/observed`**: measured values. `observed(single)` holds one value per record (a reading, a lab result); `observed(packed)` many values over a period (an hour of heart rate). Bound beside `temporal(range)`; every write is held to rules (`form` is `gauge`, `amount`, `counter` or `summary`; `value` or `code`; a `revision` never falls). Its `metric` reference pins a kind binding `metric`, or that trait.
- **`substrate.reamde.dev/core/metric`**: what a value is a value of: `name`, `unit` and `units` (references to `substrate.reamde.dev/core/unit` records), `form`, `codeSet`, `displayPrecision`, `category`, `bucketPlace`, `rollups`.
- **`substrate.reamde.dev/core/source`**: the device, app or lab a value came from; `priorities` (`keyed: true`) ranks it per metric for the aggregate read.
- **`substrate.reamde.dev/core/sourcelink`**: `sources`, a list of source records that are one device reaching the repository by several routes.
- **`substrate.reamde.dev/core/result`**: a lab's result shapes beside `value`: `codedValue`, `ratio`, `text`, `interpretation`, `referenceRanges`.
- **`substrate.reamde.dev/core/revised`**: a record rewritten from a versioned upstream; a write that lowers or removes `revision` is refused. `origin` is the provider record it was built from.

Guide `observations` covers measured values, metrics and the aggregate read.
