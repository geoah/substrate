# Plan: split the catalog into providers and samples

Status: settled 2026-09-02, questions answered by the owner (recorded at the
end). Phase 0's third record and phase 2's home authority landed as
[decision record 0046](../decisions/0046-a-repository-owns-one-authority-chosen-at-registration.md).
It is a plan, not a contract: the code that lands is the contract.

## The problem

The catalog (`kinds/`, served through `GET …/catalog`) holds 23 closures in
one tier. Every one installs the same way, lands with `source: installed`,
and is then writable by the repository's token through `/vocabulary/apply`
(`authorizeDeclarationWrite`, `internal/engine/seed.go`, protects only the
seeded `core`). Two different things are being sold as one:

- **Vocabulary a publisher owns.** `google`, `github`, `linear`, `notion`,
  `whoop`, `beeper`: mirror kinds in the provider's own shape,
  sync functions, OAuth metadata. The publisher has to be able to change
  these and ship the migration. The user must not edit them, because a
  changed mirror kind breaks the next sync.
- **Vocabulary the user should own.** `people`, `tasks`, `calendar`,
  `messaging`, `scheduling`, `commerce`, `fitness`, `food`, `health`,
  `journal`, `places`, `routines`, and the worked examples `llm.examples`,
  `notes`, `web`, `pebble`, `firecrawl`. These are starting points. A user who imports
  `tasks` wants to add a property the next day, and nobody upstream should
  be able to change their kind under them.

The two tiers are also welded together. Providers write straight into the
shared vocabulary: Google's sync emits `messaging.substrate.reamde.dev/emailmessage`
and `calendar.substrate.reamde.dev/calendarevent` rows directly, Linear's
`taskprojection` function reads, diffs and patches `tasks.substrate.reamde.dev/task`,
GitHub and Google map their user kinds onto `people.substrate.reamde.dev/person`,
and every one of them lists those authorities under `requires:`. If the user
owns `task`, a provider cannot name it.

## The target model

Two words, two directories, two doors.

**A provider** is a closure a publisher owns. Its authority is real
(`google.bundles.substrate.reamde.dev` today, a DNS name the publisher controls
later). It installs as a copy, like today, and afterwards only the substrate
path (install, upgrade) may write its declarations: the token gets `403` on
`/vocabulary/apply` against a provider authority. The publisher ships each
change with a version bump and the existing upgrade preview
(`PlanBundleUpgrade`) offers it. A provider ships mirror kinds, sync
functions, triggers and OAuth metadata, and NOTHING that names a user kind.
It never lists a sample under `requires:`.

**A sample** is a closure the user copies. In the tree it has no authority of
its own: it is authored against one placeholder authority, and importing it
rewrites every occurrence of that placeholder to the repository's own
authority before the documents reach ordinary admission. What lands is the
user's: `source: installed`, writable through the API, never offered an
upgrade, never touched by the boot upgrade. Importing the same sample twice
is an ordinary re-apply of what the user now owns. A sample may also be read
and never imported: it is the worked example an agent copies from when it
builds the user a kind from scratch.

**A mapping links the two, and the user declares it.** Today a
`recordmapping` must be declared by the authority that owns its `from` kind
(`parseMapping`, `internal/vocabulary/mapping.go`), which means only Linear
can say where a Linear issue projects. Under this plan the user declares
`linear.bundles.substrate.reamde.dev/issue → <mine>/task` themselves. The provider's
part is to leave the door open: a mirror kind declares its subject
reference (`subject: true`) with the target UNPINNED, so the mapping the user
writes is what pins it. The projection ALGORITHM is unchanged (match, shell
mint, recompute where a write above the machine tier survives), but the engine
is not: `Registry.MappingFor` answers with the one mapping of a source kind and
six call sites read it that way, so they move to a (source kind, subject
property) key with the rule.

The word **bundle** stays for the mechanism. Both a provider install and a
sample import land as a `substrate.reamde.dev/core/bundle` record with the
lifecycle verbs it already has. "Provider" and "sample" are the two catalog
tiers, replacing the three curated facets `vocabulary`, `integration` and
`example`.

## What moves where

```
kinds/
  substrate.reamde.dev/core/          the seed, unchanged
  providers.substrate.reamde.dev/     providers stay: one directory per package
    google/  github/  linear/  notion/  whoop/  beeper/
samples/
  people/     tasks/     calendar/   messaging/  scheduling/
  commerce/   fitness/   food/       health/     journal/
  places/     routines/  llm/        notes/      web/        pebble/
  firecrawl/
```

A sample directory is named for the sample, not for an authority, because it
has none. Inside, every document spells the placeholder authority
`samples.substrate.reamde.dev`, which the import rewrites and admission
refuses if it ever arrives unrewritten. Kind names are unique across all
seventeen samples today, so one placeholder authority holds them without
collision, and a sample that references another (`task.assignee` on
`person`) does so under the same placeholder and is rewritten with it.

`kinds/kinds.go` embeds `*.substrate.reamde.dev` today; `samples/` gets its
own embed and its own `Samples() fs.FS`. `kinds:check` (`cmd/vocabularydiff`)
keeps holding providers to a version bump and stops reading samples: a
sample has no upgrade path, so its version is meaningless.

## Phases

Each phase is one PR, or one stack, and each leaves `mise run ci` green.

### 0. Decision records

Three records, written before the code, so the options ruled out stay ruled
out:

1. **The catalog has two tiers.** A provider is published and its
   declarations are immutable to the repository token; a sample is copied
   under the repository's authority and owned by it. Amends 0015, whose
   `example` facet is what a sample means; the `integration` facet goes with
   it.
2. **A mapping is declared by whoever owns its target, and a mirror's
   subject reference is unpinned.** Amends the §6.1 ownership rule in
   `mapping.go`. Today's key is one mapping per source kind; the record
   changes it to one per (source kind, subject property) and keeps the
   bipartite rule.
3. **A repository owns one authority, chosen at registration.** Landed as
   [0046](../decisions/0046-a-repository-owns-one-authority-chosen-at-registration.md):
   `POST /register` takes `authority`, defaulting to `<username>.<request
   host>`, stored on the control-plane row and the repository record.

Records 1 and 2 landed as
[0048](../decisions/0048-providers-are-published-samples-are-copied.md) and
[0049](../decisions/0049-the-owner-of-a-mappings-target-declares-it.md), both
written against the package grammar of record 0047: a sample imports as
`<repository authority>/<package>/<kind>`, so the kind-name collision worry
under "What moves where" is gone. Everything above and below this note is
still written in the pre-0047 two-segment grammar (`<authority>/<kind>`), and
the phases are read against it.

### 1. The tree and the catalog (mechanical, no behavior change at the door)

- Move the seventeen sample directories to `samples/<name>/`, rewrite their
  authorities to the placeholder, add the `samples` package with its embed.
- `catalog.Load` reads both roots; `Bundle` gains `Tier: "provider" |
  "sample"` and loses `Vocabulary`, `Example`, `Integration`. Wire change,
  so `wire.golden.json`, `types.ts` and the console follow.
- Console Registry: two sections, Providers and Samples, replacing the three
  facet filters. A sample card says "import as yours" and previews the
  authority it will land under.
- Docs: `terms.md` gains **provider** and **sample**, retires **vocabulary
  bundle** and the **integration** facet; `bundles-catalog.md` and
  `builtin-kinds.md` split along the same line; `bundles.md` describes the
  two doors.
- `kinds_test.go`: the embed-pattern test covers both roots; `TestShippedBundlesInstallOnTheSeed`
  installs providers on the seed and samples on the seed plus the
  placeholder, so both tiers are proven to admit without a database.

This phase ships with providers still requiring and writing the shared
vocabulary. Nothing breaks for an existing repository: its installed
`tasks.substrate.reamde.dev` stays in its changelog exactly as it stands.

### 2. Sample import

- The home authority is `RepositoryInfo.Authority` (0046), readable on the
  repository record; the import rewrites onto it and nothing else.
- `POST …/catalog/{id}/import` for a sample: load the closure, substitute the
  placeholder authority for the home authority in every document (ids,
  `authority`, reference pins, `installs`, `requires`, function `writes`,
  trigger selectors, mapping `from`/`to`), then hand the result to
  `InstallBundleClosure` unchanged. The substitution is structural (walk the
  decoded documents), not a text replace, and a document that still carries
  the placeholder after the walk is refused.
- `substratectl import <sample>` and `substratectl apply -f` of a sample file
  with `--as <authority>` do the same rewrite client-side.
- The bundle status of an imported sample carries no upgrade offer;
  `PlanBundleUpgrade` answers not-available for a sample id.
- A sample that requires another (`tasks` needs `person`) is refused by
  ordinary admission when the home authority does not declare it, naming the
  sample to import first, exactly as `requires:` works today.

**What phase 2 landed differently, and what phase 4 owes.** Two of the
sentences above turned out to be about a phase 4 tree, not this one.

The placeholder is refused by `Catalog.Import` alone, not by admission. A
closure still spelling `samples.substrate.reamde.dev` after the walk is refused
there, but `POST …/catalog/{id}/install` and `substratectl apply -f` of the
shipped files admit it verbatim, which `authorizeNewPackage` sanctions on
purpose (the equality check is against `substrate.reamde.dev` itself, so the
sibling publisher authorities stay open to a hand apply). That door is not a
loose end: it is what keeps Google, GitHub and Linear installable while they
name `samples.substrate.reamde.dev/<pkg>` under `requires:` and pin
`samples.substrate.reamde.dev/people/person` in their mappings, since an
imported sample lands `<home>/<pkg>` and satisfies neither. The console never
offers it: the Samples section offers "Import as yours" and a provider whose
requirement is missing shows a disabled Install with the hint naming what is
missing.

Phase 4 closes both. When the providers stop requiring and pinning sample
packages, the verbatim install of a sample has no caller left, `install` on a
sample id can be refused naming `import`, and the placeholder refusal can move
down into admission where Q3 wanted it.

### 3. Provider immutability

- The stored `source` enum gains `published` (additive, so an upgrade).
  A provider install writes `source: published` on its authority.
- `authorizeDeclarationWrite` refuses a non-substrate actor on a `published`
  authority with the same `403` it gives `builtin`. Data records of a
  provider's kinds stay writable under the existing bundle-tier rules.
- The upgrade offer stays a person's click in the console; the boot upgrade
  keeps touching `core` alone.
- Existing repositories: an authority already installed with
  `source: installed` from a provider closure is promoted to `published` by
  the next upgrade of that provider, never by the boot upgrade seizing it.

### 4. Decouple providers from the shared vocabulary

This is the breaking half, and it changes what an install of Google or
Linear delivers out of the box. **Landed with phase 5**, in the same PR,
because the two could not be separated: with the mappings gone, a core row
whose reference pins `person` is refused, and mirror and core row rode in one
page, so a gmail sync delivered nothing at all.

- ~~Every provider drops `requires:` on sample authorities.~~ Landed: no
  provider names a sample package, and each installs on a bare repository.
- ~~Google's gmail and calendar syncs stop emitting `emailthread`,
  `emailmessage`, `calendar` and `calendarevent` rows.~~ Landed, and
  `calendareventseries` with them: the mirrors are the whole output, an
  instance carries its master's `recurrence` lines verbatim, and the one-shot
  `contactsidmigration` is deleted (no store old enough to need it opens on
  the package grammar of 0047).
- ~~Linear drops `taskprojection` and its `issue → task` projection.~~ Landed,
  with its trigger; `issue.projectedState` stays declared and deprecated.
- ~~GitHub and Google drop their `→ person` mappings.~~ Landed: every provider
  mapping is gone, because a mapping onto a kind is now the declaration of the
  package that owns that kind.
- ~~Every mirror kind that used to project declares its subject reference
  unpinned.~~ Landed for the five slots that had a mapping
  (`user.person`, `contact.person`, `emailaddress.person`,
  `linear/user.person`, `issue.assignee`). Unpinned and not `required`, so a
  mirror row lands with the slot empty until a mapping fills it.
- The engine blocks that stopped a mapping targeting `emailmessage`
  (required references a shell mint cannot fill) are reworked: a mapping may
  fill a reference on the subject by following the SOURCE's own reference
  through that kind's mapping (a Gmail message's `thread` reaches the user
  email's `thread` through the thread mapping). This is the one piece of
  new engine work and it has its own design note before it is built.

### 5. Mappings declared by the user, and the samples that suggest them

The loader and engine half LANDED; the suggested mappings did not.

- ~~`parseMapping` admits a mapping whose `from` kind lives in another
  authority when the declaring authority owns `to`.~~ Landed, and stricter
  than this bullet: the owner of `to` is the ONLY package that may declare a
  mapping, so `from` is free to live anywhere and the providers ship none. The
  key is (source kind, subject property), with a second rule that one source
  kind reaches a given target through one property; the mapping's `to` is the
  write-time pin on an unpinned slot; and dropping a kind another package's
  mapping names as `from` is refused at every door.
- Each sample ships optional **suggested mappings** for the providers it
  knows: `samples/tasks/mappings/linear.yaml` declares
  `linear…/issue → <placeholder>/task` on `property: task`. The import applies
  a suggested mapping only when its provider authority is installed, and the
  console lists the rest as "install Linear to enable". Because the mapping
  is imported under the home authority it is the user's, and they edit or
  delete it like any of their declarations.
- The Linear read-diff-patch behavior (Linear owns title and url, the user
  owns status) is what the projection's tiers already do: mapped properties
  are recomputed at the bundle tier and an owner write wins. The joint
  ownership comment in `linear/bundle.yaml` becomes the mapping's `map`
  block.

### 6. Signed providers (ticketed, not built here)

A provider is trusted because the binary embeds it. Once providers install
from outside the binary, install must verify that the closure was published
by the authority it names: the closure carries a signature, the authority
publishes its verify key at `https://<authority>/.well-known/substrate/authority.json`,
and the door refuses a closure whose signature does not verify. This depends
on an authority being a name the publisher controls (#194, #285) and on the
remote install path (ticket 011). Filed as
[#339](https://github.com/geoah/substrate/issues/339).

## What this does not do

- It does not prove a repository controls the authority it registered
  (#285, a DNS record the way atproto verifies a handle) or widen the
  authority grammar (0014, #194). 0046 takes the name on the user's word.
- It does not make a provider write back to its service.
- It does not move `core`: the seed stays the seed.

## Questions and the answers (2026-09-02)

1. **Home authority.** Chosen at registration, defaulting to the username
   under the server's host, DNS-valid, permanent. Domain verification comes
   later, atproto-style. Landed as 0046.
2. **One authority for every imported sample.** Yes: `<home>/task`,
   `<home>/person`. The seventeen samples have no kind-name collisions.
3. **Placeholder in the tree.** `samples.substrate.reamde.dev`, refused at
   admission if unrewritten. Phase 2 landed the refusal in `Catalog.Import`
   instead, because the verbatim install is still the door that keeps the
   providers installable; the note under phase 2 has why and what phase 4
   owes.
4. **Provider upgrades.** A click in the console, as today.
5. **The worked examples** (`llm`, `notes`, `web`, `pebble`) are samples.
6. **`firecrawl`** is a sample, not a provider.
7. **Less out of the box after phase 4** is accepted. Samples ship suggested
   mappings for the providers they know (people for Google and GitHub, tasks
   for Linear and Pebble, messaging and calendar for Google).
8. **Existing repositories.** No rehome. The tree assumes fresh repositories
   until v1; the dev box is wiped.
