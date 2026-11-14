# Declaring kinds

How to shape the owner's vocabulary: packages, kinds, every property type, traits, and changing a kind without breaking its records.

## Where your kinds go

- The owner's packages live under `~`, the repository's local namespace (`~/wine`, `~/wine/bottle`, `authority` `~`), and a new package goes only there. The repository's full name is accepted for `~`; answers say `~`.
- A package is one lowercase word for an area of life (`garden`, `wine`); a kind is one lowercase word for one thing in it (`plant`, `bottle`).
- Read first: `kinds`, then `describe_kind` on every kind you might extend or point at. Extend a kind that fits with optional properties rather than declare a near-duplicate; a new package is for a new area.
- Writable: the owner's own packages and imported samples. Refused: the seeded `substrate.reamde.dev/core`, `/llm`, `/console`, `/observations`, and every published package (an installed provider or catalogue).

## One batch

`apply` takes `documents`, admitted all or none. A package the owner does not have yet needs its package document in the same batch. Leave `version` out: the substrate versions what changes.

```json
[{"kind": "substrate.reamde.dev/core/package",
  "metadata": {"id": "~/wine"},
  "data": {"authority": "~", "package": "wine"}},
 {"kind": "substrate.reamde.dev/core/kind",
  "metadata": {"id": "~/wine/bottle"},
  "data": {"authority": "~", "package": "wine",
   "description": "A bottle of wine the owner has or had.",
   "names": {"singular": "bottle"},
   "displayTemplate": "{name}",
   "properties": {
    "name": {"type": "string", "required": true, "description": "what the label calls it"},
    "style": {"type": "enum", "description": "what kind of wine it is",
              "values": [{"value": "red", "label": "Red"}, {"value": "sparkling", "label": "Sparkling"}]},
    "vintage": {"type": "int", "min": 1800, "description": "the year on the label"},
    "openedOn": {"type": "date", "description": "when it was opened"},
    "giftFrom": {"type": "reference", "kind": "substrate.reamde.dev/core/contact", "description": "who gave it"}}}}]
```

- A kind's id is `<authority>/<package>/<names.singular>`, a package's `<authority>/<package>`. `names` takes `singular` alone.
- Give every kind a heading property (`name`, `summary`, `subject`) and a `displayTemplate` naming it. Never declare `title`, `at`, `endsAt` or `dueAt`: every record carries them already (the last three arrive through the `temporal` trait).
- `displayTemplate` tokens: `{name}`, alternatives `{name|id}`, a hop `{author.name}`, a list head `{emails[]}`.
- Descriptions: one sentence, no newlines; a kind's at most 400 characters, a property's 200. The kind's first sentence says what one record is: lists show it.
- Property names are lowerCamelCase; enum and state values lowercase words.
- A kind's data keys: `authority`, `package`, `names`, `label` (`{singular, plural}` caption), `description`, `displayTemplate`, `properties`, `traits`, `indices` (`[{"properties": ["status", "dueAt"]}]`), `purpose`, `ids`, `retired`, `version`; any other is refused.
- `purpose`: `primary` (the default: a thing the owner browses), `supporting` (a detail of another record), `internal` (machinery).
- `ids`: absent on a non-internal kind means minted: the substrate assigns ids. `{"type": "named"}` only when records are found by a chosen id.

## Property types

Every property: `type`, a `description`, and optionally `displayName`, `required`, `default` (a single scalar only: not a list, object, reference or state), `repeated: true` (a list), `deprecated: true`. A `state` takes only its machine keys beside `description`, `displayName` and `deprecated`.

| type | value | options |
|---|---|---|
| `string` | one line | `pattern`, `values`, `unique`, `fts`, `embed` |
| `text`, `markdown` | prose | `embed: true` (semantic search) |
| `int`, `float`, `decimal` | number; decimal exact, as a string `"19.99"` | `min`, `max` |
| `money` | `{"amount": 1999, "currency": "EUR"}` (minor units) | `min`, `max` |
| `quantity` | `{"value": "72.5", "unit": "kg"}` | `unit` (anchor), `units` (list), `delta`, `comparator`, `min`/`max` as quantities |
| `bool`, `date`, `datetime` | true/false, `2026-11-08`, RFC 3339 instant | |
| `duration` | ISO 8601 `PT45M`, `P2DT3H`, `P1W` (no years or months) | |
| `email`, `url`, `phone`, `timezone` | mailbox, absolute URL, E.164, IANA zone | |
| `recurrence` | RFC 5545 rule `FREQ=WEEKLY;BYDAY=MO` | see guide `traits` |
| `enum` | one of `values` | `values` bare or `{"value", "label"}`; `done`/`dropped` |
| `state` | a machine | `states`, `initial`, `transitions`, `done`, `dropped` |
| `reference` | a pointer at one record | `kind` or `trait` pin, `mustExist`, `onDelete`, `properties`, `inverse` |
| `object` | inline fields | `fields`, `required`, `repeated`, `keyed` |
| `blobref` | a stored file's digest | `mediaTypes` (`["application/pdf", "image/*"]`) |
| `secret` | sealed material, reads redacted | one value, never a list |
| `json` | a shape nobody owns | last resort |

`digest` is the substrate's alone. A `substrate.reamde.dev/core/propertytype` declares a reusable refinement (`base` plus `pattern`, `min`, `max` or `values`).

- **Enum label** when the stored word is not what a person reads: `{"value": "wanted", "label": "Want to read"}`.
- **State**: `{"type": "state", "states": ["todo", "doing", "done"], "initial": "todo", "transitions": [{"from": "todo", "to": "doing"}, {"from": "doing", "to": "done", "stamps": {"completedAt": "now"}}], "done": ["done"]}`. A record's state moves only along a transition, by a `patch`.
- **Reference**: pin `kind` with a full kind reference, or `trait` (any kind binding it), never both. `mustExist: true` refuses a pointer at no record. `onDelete: "cascade"` (single reference only) deletes this record with its referent. Link data goes under its own `properties` (flat scalars and enums only). Link records with a reference, never a copied name.
- **Object**: `{"type": "object", "fields": {"street": {"type": "string"}, "city": {"type": "string"}}}`. Fields nest to 4 levels; a field may not be `json`, `secret`, `digest`, `state` or `blobref`. A field `default` fills objects a write sends.
- `keyed: true` (with optional `keyPattern`) makes a map instead of a list.
- `unique` and `mediaTypes` are stored, not enforced.

## Traits

`traits` beside `properties` binds contracts the substrate acts on, each named in full: `"substrate.reamde.dev/core/temporal(point: dueAt)"`, `"substrate.reamde.dev/core/recurring"`, `"substrate.reamde.dev/core/lifecycle"`. The kind declares every property the trait contracts. Read guide `traits` before binding one.

## Change, never break

- A changed kind is sent whole: the batch replaces its declaration. Start from `describe_kind` and change only what you mean to. A property you leave out is dropped, and its values are nulled.
- Additive admits: a new kind, an optional property, an enum value, a state or transition, removing `required`.
- Narrowing is refused while live records hold the old shape, with the count: a retype or a `repeated`/`keyed` flip, removing a held enum value or state, adding `required` without a `default`, a tighter `pattern`/`min`/`max`/pin, adding `mustExist`.
- Prefer add-and-deprecate: add the new property, mark the old `"deprecated": true` (never also `required`). An enum value takes `deprecated` too.
- `renamedFrom` on a property (`{"type": "string", "renamedFrom": "size"}`) moves every live value to the new name; on an enum value (`{"value": "working", "renamedFrom": "active"}`) it respells stored values.
- `required: true` beside a `default` backfills every record lacking a value. A new `state` property enters every record in its `initial` state.
- `retired` spends names for good: `{"properties": ["size"], "values": {"level": ["low"]}, "states": {"phase": ["archived"]}}` on a kind, `{"kinds": ["gadget"]}` on a package. A retired name is never declared again. Deleting a kind with live records is refused.

## Plan, then apply

1. `apply` with `plan: true` first: it writes nothing and answers the steps with record counts, `lossy` and `blockers`. Fix every error it names and plan again.
2. Apply the same batch only when the owner asked for this change, the plan has no blockers and is not lossy.
3. **Lossy** (a drop that nulls values, or a value rename onto one records already hold) is refused here. Stop and hand the plan to the owner; confirming data loss is theirs (`substratectl apply --allow-data-loss`).

## Mappings and code

- A `substrate.reamde.dev/core/recordmapping` (`from`, `to`, `property`, `match`, `map`) projects a provider's records onto the owner's kind; the package owning `to` declares it. `holdWaitingMappings: true` holds back one whose provider is absent.
- Functions, agents and triggers are declarations through `apply` too: guide topics `agents` and `automation`.
