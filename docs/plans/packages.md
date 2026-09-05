# Plan: a kind lives in a package, and a package lives in an authority

Status: design settled with the owner, 2026-09-02; not started. It is a plan,
not a contract: the code that lands is the contract.

## The problem

A kind reference is `{authority}/{name}`, and the only way to group kinds is
to give each group its own authority. The shipped vocabulary does exactly that
with 25 subdomains of one placeholder (`tasks.substrate.reamde.dev`,
`google.bundles.substrate.reamde.dev`), so grouping, ownership, versioning and
the DNS name are one knob. A repository that owns one authority
([0046](../decisions/0046-a-repository-owns-one-authority-chosen-at-registration.md))
has no way to group its kinds at all: every sample it imports lands flat under
`ada.example.com/`, and two samples may never share a kind name.
[#194](https://github.com/geoah/substrate/issues/194) asked whether an
authority may carry path segments instead; the answer is no
([0014](../decisions/0014-authorities-widen-only-outside-the-id-alphabet.md)),
and this plan is the alternative.

## The design

### The reference

A kind reference is `{authority}/{package}/{name}`. The package is required,
a plain word (`[a-z][a-z0-9]*`, the kind-name grammar), and it is the middle
segment everywhere a kind is spelled: a declaration id, a reference pin, a
stored reference value, a REST path, a console route.

```yaml
kind: substrate.reamde.dev/core/kind
metadata:
  id: ada.example.com/tasks/task
data:
  authority: ada.example.com
  package: tasks
  names:
    singular: task
```

- A stored reference value is `{authority}/{package}/{kind}/{id}`:
  `ada.example.com/tasks/task/t1`. The split stays registry-free: the
  authority is the one segment with a dot, the next two are words, the rest
  is the id, whose alphabet is untouched. `/` gains a third job, which 0014
  forbade; the decision record below supersedes that clause and keeps the
  rest of 0014 (no raw `/` in an authority, `%` never in an id).
- A REST path routes by segment count, shifted by one: three segments
  (`/api/v1/{authority}/{package}/{kind}`) is a collection, four is a record.
  The core verbs move with core: `/api/v1/substrate.reamde.dev/core/catalog`.
- A bare name in a declaration still resolves against the declaring package
  (`kind: account` inside `providers.substrate.reamde.dev/google` means
  `providers.substrate.reamde.dev/google/account`); the shorthand of 0042
  survives with one more segment filled in.

### The package as a declaration

A package is declared, like an authority is, and it takes over what the
authority document did for a closure:

```yaml
kind: substrate.reamde.dev/core/package
metadata:
  id: providers.substrate.reamde.dev/google
data:
  authority: providers.substrate.reamde.dev
  description: Google contacts, Gmail and Calendar mirrored into the graph.
  version: 8
```

- The **version unit** is the package. A kind may still pin its own; else it
  takes its package's. The boot upgrade, `kinds:check` and the upgrade
  preview key on it. Two packages under one authority upgrade independently,
  which is what lets `google` and `github` share an authority.
- The **quarantine unit** is the package: a declaration the loader refuses
  parks its package, not its authority.
- The **ownership unit** (`authorizeDeclarationWrite`, `source: builtin |
  published | installed`) is the package.
- A **bundle** owns at least one package and its id is the package it is
  named for (`providers.substrate.reamde.dev/google`). Whether a bundle may
  span two packages is left open; nothing in the tree needs it and the
  loader does not forbid it.
- `requires:` names packages.
- The **authority document** stays as the owner of packages and the thing a
  repository is born with; a registration creates the repository's authority
  record and no package until the user declares or imports one.

### Actors and GraphQL

- Actors add a colon segment, never a slash (meta keys split on
  `<actor>/<name>`): `bundle:<authority>:<package>`,
  `function:<authority>:<package>:<name>`, `agent:<authority>:<package>:<name>`.
  Amends [0025](../decisions/0025-an-actor-carries-the-full-authority.md).
- The GraphQL name of an installed kind is `<Package>_<Kind>` (`Tasks_Task`),
  and only when two authorities install the same package name does the
  authority's first label join it. This retires the last first-label keying
  0014 reserved.

### The shipped tree

Two shipped authorities, plus the samples placeholder:

```
kinds/
  substrate.reamde.dev/
    core/                       the seed: substrate.reamde.dev/core/kind, …/core/bundle, …
  providers.substrate.reamde.dev/
    google/  github/  linear/  notion/  whoop/  beeper/
samples/                        authored as samples.substrate.reamde.dev/<package>/…
  people/  tasks/  calendar/  messaging/  scheduling/  commerce/  fitness/
  food/  health/  journal/  places/  routines/  llm/  notes/  web/  pebble/  firecrawl/
```

A sample imports by rewriting the authority alone; the package stays, so a
user ends up with `ada.example.com/tasks/task` and
`ada.example.com/people/person`, and the collision worry in
[the samples plan](providers-and-samples.md) disappears.

### What it looks like

- URL: `/api/v1/ada.example.com/tasks/task/t1`
- Console: sidebar grouped authority, then package, then kind; the route is
  `/data/ada.example.com/tasks/task`.
- `substratectl get task t1` resolves `task` against the repository's own
  packages first and refuses an ambiguous bare name, naming the two.
- Envelope: `record.kind` is the full reference; nothing else changes shape.
- A webhook URL is `/webhooks/{authority}/{trigger}` (the hostname as the
  repository's outward name, item B of the tidy-up), unchanged by packages.

## What changes

- **Grammar and loader** (`internal/vocabulary`): `KindRef`, `SplitKindRef`,
  `SplitRecordPath`, the reference validator, `Qualified`, every declaration
  id derivation, the `package` document and its keys, bundle `installs` and
  `requires`, the actor mints, `GraphQLName`.
- **Engine**: `authorizeDeclarationWrite`, quarantine, the version unit in
  the seed and boot upgrade, `PlanBundleUpgrade`, projection's kind lookups,
  `vocabularydiff`, a vocabulary dialect bump (`maxVocabularyDialect` 3) so
  an older binary refuses a store written in the new grammar.
- **API**: the route table and `addressed`, the discovery `grammar`, the
  core verb prefix, catalog paths, `types.ts` through the golden file.
- **Console**: routes, the kinds grouping, breadcrumbs, the registry page.
- **The tree**: every document's id and every reference in it, the directory
  layout, `kinds.go`'s embed pattern, `kinds_test.go`, the catalog loader,
  `.mise/kindscheck.sh`.
- **Docs**: `terms.md` gains **package** and its row for **kind** changes;
  `data-model.md`, `vocabulary.md`, `api.md`, `builtin-kinds.md`,
  `bundles.md`, `bundles-catalog.md`, every example; the CLAUDE.md kind
  identity section.
- **Decision records**: one new record for the package (the grammar, the
  version and ownership unit, the two shipped authorities); it supersedes
  the "`/` never gains a third job" clause of 0014 and amends 0025, 0033 and
  0042 by reference. #194 closes on it.

Nothing here needs a table migration: `record_kind` is text. Every stored
kind string changes, so the tree assumes a wiped database, and the dialect
bump is what protects an old one.

## Delivery

The tree cannot half-move: a document in the old grammar does not load under
the new loader and the reverse. So this is one integration branch, reviewed
commit by commit, squashed as one `refactor(vocabulary)!`:

1. Grammar, loader and the `package` document, with tests.
2. Engine: ownership, quarantine, versions, upgrade, dialect.
3. API and console.
4. The tree moved to `kinds/<authority>/<package>/`, `kinds:check` and the
   catalog following.
5. Docs and the decision record.

Order against the rest of the work: after the tidy-up (close #341, relabel
#285, the hostname as the outward name), and before any sample import, so
the import rewrites kind ids of the final shape once.

## Open

- The exact GraphQL disambiguation rule when two authorities install one
  package name.
- Whether the `core/bundle` kind keeps its name or becomes `core/package`
  outright once a bundle owns one package in practice.
