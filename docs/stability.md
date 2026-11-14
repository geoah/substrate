# Stability

This page says what an app, a script or a package can build on: which parts
of a substrate a release may change, and how much warning comes first. It is
written for a person writing an app against the API or publishing a package
of kinds.

Every promise here is one of three stability labels, and a label sits in two
places: on a **package**, for its kinds, and on a **feature**, a named group
of the API's routes. The promises are written for the 1.x releases. The
server is below 1.0 today, and
[the last section](#what-is-not-in-force-yet) lists each clause the code
does not hold yet.

## The three labels

A label is a promise about **change**, not about quality. Everything that
carries one is served and works today.

| Label | What it promises |
| ----- | ---------------- |
| `stable` | It only gains things within 1.x, and it is removed only in 2.0. |
| `beta` | It is supported. A break is announced one minor release ahead, in [discovery](api.md#discovery) and in `CHANGELOG.md`. |
| `experimental` | It may change or go in any release. |

One example of each:

- **`stable`.** The `records` feature is stable: the records route,
  `GET /api/v1/records`, and the record path. A 1.x release may add a query
  parameter to the list, or a field to the JSON of a record. It does not
  rename `properties` or `cursor`, or change what a `filter` that works
  today selects.
- **`beta`.** The `auth` feature is beta: registering, signing in and
  minting tokens. A break of it is announced the way any is: the release
  before says so in the feature's `notice` and in `CHANGELOG.md`, as its
  entry said "Before 1.0, sign-in names a person, who then picks a
  repository." until revision 3 made it so
  ([decision 0274](decisions/0274-a-person-signs-in-as-an-identity-beside-the-repositories-and-picks-the-repository.md)).
- **`experimental`.** The `agents` feature and the
  `substrate.reamde.dev/llm` package are experimental: the chat and call
  routes, and the thread and message kinds a chat is stored in. The events a
  chat streams, or a property of a thread, may change in the next release
  with no notice ahead. An app that depends on one compares `server.version`
  in discovery with the release it was tested against before it relies on a
  newer server.

## Where a label is read

### On a package, for its kinds

A package states its label in its header, under `stability`:

```yaml
kind: substrate.reamde.dev/core/package
metadata:
  id: ~/garden
data:
  authority: "~"
  package: garden
  stability: beta
  version: 3
```

The header is a record like any other, so a client reads the label where it
reads the package's description:

```http
GET /api/v1/substrate.reamde.dev/core/package/substrate.reamde.dev%2Fcore

→ {"id": "substrate.reamde.dev/core",
   "kind": "substrate.reamde.dev/core/package",
   "properties": {"stability": "stable", …}, …}
```

- **A kind carries no label of its own.** Its package's label is the one
  place to look.
- **A package that states none reads as `experimental`**, the weakest
  promise. Nothing is stored for an absent key, so a package that never said
  is told apart from one that wrote `experimental`. Any other value is
  refused when the document is loaded, naming the three.
- **The label is the publisher's statement.** The server stores it and
  refuses no write over it. What the server enforces is the same for every
  package, whatever its label: a declaration change that would strand live
  records is refused
  ([vocabulary evolution](vocabulary.md#vocabulary-evolution-and-the-dialect-contract)).
- **Changing the label is a change to the package.** Through
  `POST /api/v1/vocabulary/apply` it moves the package to its next version,
  and dropping the key from the header clears the label from the record.

Of the packages the binary ships, `substrate.reamde.dev/core` states
`stable`, the other seeded packages (`llm`, `console`, `observations`,
`assistant` and `applets`) state `experimental`, and the catalogues, the
providers and the samples state none
([built-in kinds](builtin-kinds.md)). The console shows a package's label on
its page, as **Stability**, with Technical details on.

### On a feature, for its routes

A feature is one entry of the `features` list in
`GET /.well-known/substrate/server.json`, which answers before a client holds
a token:

```json
{"name": "records", "stability": "stable", "revision": 1, "surfaces": ["rest"]}
{"name": "auth", "stability": "beta", "revision": 3, "surfaces": ["rest"]}
```

- **`stability`** is the feature's label.
- **`revision`** is a number that starts at 1 and only goes up. It goes up
  when the feature gains something a client may want to check for before it
  uses it: a route, a parameter, a request field, a response field.
- **`notice`** is present only where the server announces a coming break or
  a deprecation.

Every route the server registers is in exactly one feature, so no route is
without a label. The list is not repeated on this page, because the server
is the place it is true: read it from the server you talk to.
[Discovery](api.md#discovery) describes every key of an entry and names the
features, and the console shows the list under Settings, Developer, with
Technical details on.

The two labels answer different questions about one request. Reading a
thread through the records route uses a stable feature and an experimental
package: the route, its parameters and the record's top-level fields only
gain things, and the properties inside the thread are the `llm` package's to
change.

### The API version is the address prefix

Every route of the versioned API starts with `/api/v1`, and discovery lists
the prefix under `versions` with the status `served`. The prefix is the API's
version. A feature's label says how its routes may change under that prefix.

## Data outranks labels

A label covers a shape, never the data. The latest state of every record, an
experimental package's included, survives every upgrade: either the record is
untouched, or a conversion that ships with the release carries it to the new
shape. A feature may be withdrawn. The records written through it are not
lost, and they stay readable through the records route.

History is the changelog entries older than a record's latest state. It is
kept for as long as the kind's retention says: a kind states `all`, `none` or
a length of time, its package header may state it for its kinds, and a
repository may override it per kind
([the reserved keys](vocabulary.md#the-reserved-keys)). Nothing drops an
entry by it yet ([not in force yet](#what-is-not-in-force-yet)).

## Only additions, surface by surface

### The API

`/api/v1` only gains things: routes, parameters, request fields and response
fields. Nothing a stable feature serves is renamed, removed or narrowed under
the prefix. Two rules for a client follow from that, and from the server
refusing any request field it does not know with `400 bad_request`:

1. **Ignore a response field you do not know.** A later release may add one
   to any answer.
2. **Check a feature's revision before you send something that revision
   added.** A newer app's new request field fails on an older server, so the
   app asks first. If a release adds a field to the body of
   `POST /api/v1/records`, `records` goes to revision 2, and an app sends
   the field only to a server that lists `records` at 2 or later. A server
   from before revisions lists none, which meets no minimum.

The first-party clients each carry that check: `hasFeature(doc, name,
atLeast)` in the console and in the web app kit, and
`discovery.serves(name, atLeast:)` in the Swift kit.

A break to the API is a new prefix. A later `/api/v2` is served beside
`/api/v1` for a period that the release adding it states, and `versions`
lists both.

### Kinds

A kind is never served in two shapes at once. The server holds one shape of
each kind, and a declaration change that moves a value (a renamed property, a
remapped enum value) rewrites the stored records in the same step
([vocabulary evolution](vocabulary.md#vocabulary-evolution-and-the-dialect-contract)).
Nothing converts a record between versions as it is read.

So an incompatible change to a stable kind is made beside it: a new property,
or a new kind, with the old one marked `deprecated: true` and kept. A kind is
never renamed, whatever its package's label: other repositories hold records
of it under its name, and every door a published package comes through
refuses a `movedFrom` that would move them
([vocabulary evolution](vocabulary.md#vocabulary-evolution-and-the-dialect-contract)).

### Declarations

The keys a declaration may carry are a closed set, and a server refuses a
document carrying a key it does not know. A new key raises the vocabulary
dialect number, which a server publishes in discovery as
`vocabulary.dialect`. A package states the number it needs, in its header and
beside each version its publisher lists, so an older server installs the
newest version it can read and refuses a newer one by that number
([the dialect](vocabulary.md#vocabulary-evolution-and-the-dialect-contract)).

### Stored data

A repository's archive, which the server writes behind its commits and the
recovery export hands out, has a written format: the manifest and its mark,
the changelog's lines and their checksum, every effect a line carries, and
the layout of its pieces and files
([the stored format](stored-format.md)). Its key sets are closed, so a server
refuses a key it does not know rather than drop it. A new op, effect or key
raises the
[changelog dialect](changelog.md#the-dialect-a-changelog-is-written-in). The
archive, the directory under the data root and every table are keyed by the
repository's id, never by its name, so a change of name moves no stored data
([decision 0261](decisions/0261-a-repository-is-keyed-by-a-minted-id-and-its-name-keeps-every-other-job.md)). A
1.x server reads an archive any earlier 1.x release wrote, converting it once
where a shape changed, and 2.0 reads what the last 1.x release wrote. Each release
keeps a directory its own server wrote, and every later release is tested
importing it ([kept directories](stored-format.md#kept-directories)).

### A package's code

A function is written against an interface: the input it is handed, and the
`host` object it reads and writes through. The interface is labelled `beta`,
and it carries a version: a package names the one its code is written
against in its header, under `interface`, and a server refuses a package
naming a version it does not run. A release of the server supports the
current Python release and the one before. The image carries the current
one, and [the interface](functions.md#the-interface) says what holds the one
before.

Code from outside publishers is `experimental` at 1.0: the functions a
package installed from another authority's document ships, whatever its own
label says. How an app's approval reviews that code, and how the server
confines it, may change in any release
([code from outside authorities](functions.md#code-from-outside-authorities)).
No field in discovery or on a package record carries this label: a package
installed from a publisher's document carries `publishedFrom`, and that is
the code it covers. The console and `substratectl` say so where a person
decides, from what the catalog and an approval already carry (a publisher,
and functions in the closure, the packages a take brings first included):
an app's approval page, the package's page, the providers listing beside
Add and the review of an update show "Code: experimental" beside the label
the publisher states, which stays as the publisher wrote it, and
`substratectl install` prints it before it installs and `substratectl
catalog` in its CODE column.

## Upgrading

A 1.x server opens any database, data root and archive written by the same
or an earlier 1.x release, in one step: there is no release in between to stop at.
It refuses one written by a later release, by name
([upgrading the binary](operations.md#upgrading-the-binary)).

Database migrations only go forward, and none undoes another. A change to a
stored shape ships with a conversion that runs once for each repository, at
the boot check, before the repository is served
([conversions](stored-format.md#conversions)). The server never reads two
shapes of the same thing.

## Going back

Going back to an older server is not supported once a newer one has stamped a
repository or migrated the database. The older server refuses both. The way
back is a copy taken before the upgrade: the database, the data root and the
archive, restored together ([backups](operations.md#backups)).

Each release says whether it closes the way back. Four changes close it: a
database migration, a repository migration, a dialect number, and a seeded
declaration that uses a key or a value an older server refuses. A change of
one of those says so in its upgrade steps, which land under BREAKING CHANGES
in the release's section of `CHANGELOG.md`, and the release checklist holds
each release to it ([releasing](releasing.md#cutting-a-release)).

## Deprecation

A deprecation is announced in discovery, as the feature's `notice`, and in
`CHANGELOG.md`.

- For a **stable** feature or package it is notice ahead of 2.0. It is never
  permission to remove the thing within 1.x.
- A **beta** feature is served for at least two further minor releases after
  the notice.

Inside a declaration, `deprecated: true` marks a property, a reference or one
enum value a client should stop offering
([the reserved keys](vocabulary.md#the-reserved-keys)). It removes nothing.

## Decisions

A choice that binds later code is written down as a
[decision record](decisions/README.md), and a record is `accepted` or
`rejected` before its code is in a release: no record is `proposed` while
the code it describes is on `main`.

## Offline use

An app that has to work without a connection keeps the writes it could not
send in a queue on the device, and sends them in order when the server
answers again. The app kit calls that queue the outbox
([terms](terms.md#in-an-app)). To the server each is an ordinary write: one
that carries the version it was made against (`ifVersion`) is refused when
the record has moved on meanwhile, and one that carries none lands on top of
what is there.

Which writes an app guards, and what it does with a refusal, is the app's
own. The kits guard an edit a person made on what they saw, and give a
refused one back to that person to retry, discard or resolve. They do not
guard everything: a queued delete wins over a change made meanwhile, and a
change the app made by itself is sent again on top of the newer record.

That is all the offline support there is. A repository has one changelog in
one order, and the server never merges a second history, written somewhere
else, into it.

## Who may share a server

A server is for one household, or for people who trust each other and whoever
runs it. Each person has a repository of their own and no request reads
across two, but the server is not built to hold strangers apart. A server for
people who do not know its operator is not supported in 1.x.
[Running a substrate](operations.md#who-may-share-a-server) says what assumes
that trust.

## Outside the promise, by name

Any release may change these, whatever label stands near them:

- **The database tables.** Postgres is the server's own storage. Read the
  data through the API or the
  [recovery export](operations.md#backups).
- **The Go packages.** Nearly all are under `internal/`, which nothing
  outside this repository can import, and the rest (`kinds`, `samples` and
  the commands under `cmd/`) are for this repository's own binaries.
- **The CLI's table output.** A script reads `substratectl … -o json` or
  `-o yaml`. The columns of a table are for a person, and may change.
- **The built-in assistant**: the agents of the seeded
  `substrate.reamde.dev/assistant` package, and their prompts.
- **Console layouts**: every shipped record of
  `substrate.reamde.dev/console/recordview` and
  `substrate.reamde.dev/console/collectionview`, whichever package ships it.
  The `console` package ships the collection views. Record views ship in
  `health`, in `labs` and in two providers, and one may lay out a kind of
  another package: `labs` ships a view of the seeded `metric` kind.
- **Model lists**: the `llm/model` records the binary writes and keeps
  current, with the prices they carry, and the three keyless `llm/provider`
  rows a repository is created with, which the binary creates and never
  replaces.
- **Vendor mappings**: the nine contact mappings `core` ships, which map the
  people of Apple, Google, Slack, GitHub, Linear and Beeper onto
  `core/contact`.
- **Applets**: the `applets` package and its routes, both labelled
  `experimental`.

What becomes of a person's own changes depends on what the shipped thing is:

- **A shipped record** (a layout, a model record, a provider row) is written
  again or replaced by a newer one only while nobody but the server has
  written it. One a person edited or deleted stays as they left it.
- **A shipped declaration** (the assistant's agents with their prompts, and
  the contact mappings) cannot be edited or deleted through the API: its
  package changes only with the server, and a release that ships a newer
  version of it replaces the stored one. A person shapes the assistant with
  [instruction records](agents.md#owner-instructions), or copies an agent
  into a package of their own and changes the copy.

Two parts of `substrate.reamde.dev/core` are outside its `stable` label and
are experimental:

- the `substrate.reamde.dev/core/agent` kind, with the `llm` package and the
  `agents` feature it runs on ([agents](agents.md));
- the judge half of `substrate.reamde.dev/core/recordpatchpolicy`: `judge`
  and the properties that configure it, through `mode`
  ([the policy door](agents.md#the-policy-door)). The half that selects
  writes and names an action is covered.

## What is not in force yet

The code does not hold these clauses today:

- **The server is below 1.0, and a release may still break anything
  labelled here, `stable` included.** The labels say what 1.0 will promise.
  Until then a break ships as it does today, as an entry under BREAKING
  CHANGES in the release's section of `CHANGELOG.md` with the steps to
  follow, and with no notice a release ahead.
- **History is complete.** A kind states its retention and a repository may
  override it, and nothing prunes or compacts a changelog by it yet:
  `changelog.horizon` in discovery is 0.
