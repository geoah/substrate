# Using a substrate from an agent

This page is for a program that acts on a person's behalf with a token: an
LLM agent, a coding agent, an MCP client, a script. It says what to read
before the first write, how to read and write correctly, what each error
means, and what never to do. The agents the substrate runs itself (the
`agent` kind, its tools, the chat wire) are on [agents](agents.md); their
tool cards state the same rules in a shorter form.

Everything here is the public API. There is no privileged path for an agent,
and a token has the whole repository: what follows is discipline, not
permission.

## Before the first request

**Discovery is unauthenticated.** `GET /.well-known/substrate/server.json`
answers before you hold a token. Read three things from it: `grammar`, which
spells the kind reference (`<authority>/<package>/<name>`), the record path
and the records route this deployment expects; `features`, the list a client
reads instead of probing a route, each with a `stability` (`stable`, `beta`,
`alpha`) and its `surfaces`; and `registration`, which says whether an
invite code and a second factor are required
([discovery](api.md#discovery)).

**The token comes from the owner.** Every request carries
`Authorization: Bearer <token>`. The owner mints one for you with
`substratectl token create --label <name>` and revokes it from the same
place; a token record is what they see in their token list. Do not log in
with the owner's password, and never take a password as a command line
argument: every `substratectl` prompt has a `--*-stdin` twin for the
headless case ([users and tokens](auth.md)).

**Do not set `X-Substrate-Actor`.** A request that names no actor is
attributed to `api`, which is what the substrate knows about you. The header
names a door (`console`, `substratectl`), and the machine actors are refused
to any request ([actors](api.md#actors)).

## Learn the vocabulary before writing

A kind is a declaration, and the declaration is a record you can read.
Nothing in a repository is addressable without its kind, because an id is
unique per kind, never per repository.

1. **List the kinds.** `substratectl kinds` prints them (`-o json` for a
   program). Over HTTP the same list is the records route filtered to the
   declaration kind:

   ```http
   GET /api/v1/records?filter={"kinds":["substrate.reamde.dev/core/kind"]}
   ```

2. **Read one declaration.** A declaration's id is its kind reference, so
   the slashes in it are percent-encoded to stay one path segment:

   ```http
   GET /api/v1/substrate.reamde.dev/core/kind/ada.example.com%2Ftasks%2Ftask
   ```

   The record's `properties` carry the whole declaration. Read `properties`
   (each declared property with its `type`, and for a `state` property its
   `states` and `transitions`), `traits`, `displayTemplate`, `names.singular`
   and `version` ([vocabulary as records](vocabulary.md)).

3. **Read the traits list literally.** A binding such as
   `temporal(point: dueAt)` gives the kind a `dueAt` property that is written
   and read like any other, and `temporal(range)` gives it `at` and
   `endsAt`. The trait document itself declares only `at` and `endsAt`:
   `dueAt` exists solely through the binding, so a kind without that exact
   binding has no `dueAt`, and a write naming it is refused with the
   binding quoted ([traits](traits.md)).

4. **Find the heading property.** A record's `title` is derived from the
   kind's `displayTemplate`. Write the property the template names (`name`,
   `summary`, `subject`), never `title`.

5. **Know what a fresh repository holds.** Registration seeds the core
   vocabulary, the `llm` package with three keyless provider rows and the
   LLM sample. Every other kind arrives by import: the owner installs a
   catalog package from the console's Registry page, with
   `substratectl import <authority>/<package>`, or with
   `POST /api/v1/catalog/<authority>%2F<package>/import`. An import lands
   under the owner's own authority, so the kind you then write is
   `<owner authority>/tasks/task`, not the catalog's id
   ([getting started](getting-started.md)).

## Reading

`GET /api/v1/records` is every read, in three modes told apart by their
parameters, and each mode refuses the parameters it does not honor by name
([the records route](api.md#the-records-route)):

- **List** (`filter`, `orderBy`, `first`, `after`, `expand`): the general
  read. The filter is one JSON document, URL-encoded, with `kinds`,
  `properties` (one condition per property: `eq`, `in`, `prefix`, `match`,
  `gt`, `gte`, `lt`, `lte`, `contains`, `exists`), `labels`, `deleted` and
  `referencing`. `expand=a,b` names reference properties and the page
  carries their targets under `included`, keyed by `<kind>/<id>`.
- **Ranked** (`q`, `mode`, `first`, `filter.kinds`): search. It returns
  `records` in rank order with `scores`, and no cursor.
- **Watch** (`watch=1`, `filter.kinds`, `from`, `generation`): the tail,
  below.

One record is `GET /api/v1/<authority>/<package>/<kind>/<id>`, and
`substratectl get <kind> <id> -o yaml` prints the same record as an envelope
that `apply -f` accepts unchanged.

**Page with the cursor, not with an offset.** A list answers
`{records, cursor?, head, generation}`. Pass `first` (default 50, at most
500) and resend the returned `cursor` as `after`; a page without a `cursor`
is the last one. The cursor is opaque and is bound to the filter and order
it was minted for. `offset` exists for a numbered page bar and is not
stable under concurrent writes, so a walk that must see every row once uses
`after` ([pagination](api.md#pagination)).

**Ids come from results.** Never compose or guess an id, and always carry
the kind beside it. A `mustExist` reference to an id that does not exist is
a `422`, not a `404`.

## Writing

The write surface is five mutations, for every actor, forever
([the five mutations](api.md#the-five-mutations)):

| Mutation | Route | Rule |
| --- | --- | --- |
| `put` | `PUT /api/v1/<authority>/<package>/<kind>/<id>` | Create or update. It merges and never prunes: a property the body omits is left alone. |
| create with a minted id | `POST /api/v1/records` with `kind` in the body | The server assigns the id and answers `201`. A body carrying an `id` is refused. |
| `patch` | `PATCH` at the record path | Edit in place. A `null` value deletes a key. A state moves only through a patch, and only along a declared transition. |
| `delete` | `DELETE` at the record path | A tombstone, explicit and never implied by an omitted property. |
| `merge`, `split` | `POST /api/v1/merge`, `POST /api/v1/split` | The owner's. An agent proposes rather than merges. |

The rules that follow from the model:

- **Property values sit under `properties`.** The body of a put or patch is
  `{"properties": {...}}`, with `labels` and `annotations` beside it. A
  property written straight onto the body is refused.
- **A reference is a string.** A link to another record is an ordinary
  property whose value is `"<kind>/<id>"`. The read serves it back as an
  object with `ref`; the string form is what you write.
- **Hold a write to the version you read.** `put` and `patch` take
  `ifVersion` in the body and `delete` takes `?ifVersion=`. A stale version
  is `409 conflict` and changes nothing: re-read, decide again, retry with
  the new version. This is the read-then-write primitive; use it whenever
  your write depends on what you read.
- **Name a retry.** A write you may repeat carries an `Idempotency-Key`
  header, so a timeout followed by a retry is one effect
  ([idempotency and retries](api.md#idempotency-and-retries)).
- **A secret goes only into a secret-typed property.** It reads back
  redacted from every surface, so do not try to read it back, and do not
  copy it into a note, a description or a log line.
- **The CLI is the same API.** `substratectl apply -f` is `put`,
  `substratectl patch <kind> <id> --state status=done` is a transition, and
  `substratectl delete` is the tombstone ([substratectl](substratectl.md)).

## What an error means

Every error is one shape, and the code set is closed
([errors](api.md#errors)):

```json
{"error": {"code": "validation", "message": "…", "problems": ["…"],
           "problemDetails": [{"path": "…", "message": "…"}]}}
```

| Code | HTTP | What you do |
| --- | --- | --- |
| `validation` | 422 | The body is wrong: an undeclared property, a malformed value, a reference to a record that does not exist. Read `problemDetails[].path`, fix that input, and only then retry. Do not retry the same body. |
| `conflict` | 409 | Your `ifVersion` is stale, or a put addressed a former id. Re-read the record and decide again. |
| `guard` | 403 | The state transition is not declared, or the operation is protected. Pick a declared transition; do not force it. |
| `lossy` | 403 | The change would drop values and needs the owner's confirmation. Stop and hand the plan to the owner. |
| `forbidden` | 403 | The token may not do this at all. Stop. |
| `auth` | 401 | The token is missing, revoked or expired. Stop and ask the owner for a new one; do not try a password. |
| `not_found` | 404 | No such kind, or no such record at that address. Check the kind reference first. |
| `compacted` | 410 | Your changelog cursor is below the horizon or under another history generation. List again and resume from the new `head`. |
| `rate_limited`, `unavailable` | 429, 503 | Wait the `Retry-After` the response carries, then retry the same request. |
| `bad_request` | 400 | A parameter the mode does not honor, or a malformed request. Fix the request. |
| `internal`, `function_failed` | 500 | The server, or a function's body, faulted. Report it; do not loop. |

## Following changes

A list carries the changelog `head` and its `generation`. Open the tail from
that pair and nothing is missed or seen twice:

```http
GET /api/v1/changes?from=4189&generation=7f3a0c2e9b1d4e6f&watch=1
```

The stream is newline-delimited JSON: a `bookmark` frame first, then one
frame per change with `seq`, `op`, `kind`, `recordId`, `actor` and the
`affected` records. An empty object is a heartbeat, and a frame carrying
`error` is the last one. Store the last `seq` with its `generation` and
resume from the pair; a `from` above zero without its `generation` is
refused. `GET /api/v1/records?watch=1&filter={"kinds":[…]}` is the
same stream narrowed to kinds. Re-fetch each record `affected` names rather
than trusting the frame alone ([the changelog and watch](changelog.md)).

## Declaring vocabulary

An agent may declare kinds when the owner has asked for it. A declaration
is a record write through `POST /api/v1/vocabulary/apply`, which is where
`substratectl apply -f` sends a document whose kind is a core declaration
kind ([vocabulary as records](vocabulary.md)):

- Every kind names an `authority` and a `package`, and a package document
  declares the package first. The owner's authority is the one you may
  declare into.
- The engine maintains `version` through the API: a changed definition lands
  at the stored version plus one. Do not hand-bump.
- Additive changes upgrade cleanly: a new kind, a new optional property, a
  new enum value, a new state. A narrowing (dropping or retyping a property,
  removing a value, adding `required`) is refused while live records hold
  the old shape, so prefer adding and marking the old one `deprecated: true`.
- `title`, `at`, `endsAt` and `dueAt` are never declared under
  `properties`. The temporal ones come from a trait binding; the title is
  derived.
- A bundle document replaces its authority whole: a batch that carries one
  must carry the entire closure, or what it omits is pruned.

## When the server upgrades

`server.version` in discovery is the release you are talking to. Keep the
version you last worked against, and when discovery reports a newer one,
read the upgrade notes of every release in between before the next write.
Each release on the [releases page](https://github.com/geoah/substrate/releases)
opens with them, grouped as breaking changes, deprecations, features and
fixes, and a `## What to do` under each break lists the steps in order.
`GET https://api.github.com/repos/geoah/substrate/releases` returns the same
text as `body`, one release per entry. In a checkout of this repository,
`mise run changelog` renders every release at once, and the notes themselves
are the files under [docs/changes](changes/README.md).

## Never

- Never read, list, export or repeat the contents of
  `substrate.reamde.dev/core/token`, `/credential`, `/secret` or
  `/recoverykey`. They are the owner's auth material; nothing an agent does
  needs them.
- Never write a key or a password anywhere but a secret-typed property, and
  never pass one as a command line argument.
- Never invent an id, address a record without its kind, or address a
  collection by a plural, and never by a bare name
  (`get <authority>/tasks/task`, not `get task` or `get tasks`).
- Never retry a `422` unchanged, force a `403`, or loop on a `500`.
- Never write `title` on a kind that declares its own heading property, and
  never declare a reserved property to make a write pass.
- Never merge or split records; propose the change to the owner instead.
- Never write around the API. The changelog is the truth and the API is
  the only writer of it.

## Where the contract is

[The API](api.md) is the wire, [the data model](data-model.md) the envelope
and the property types, [the changelog](changelog.md) the feed,
[substratectl](substratectl.md) the CLI, and [agents](agents.md) the loop
the substrate runs on your behalf. Where a page and the code disagree, the
code is right and the page changes.
