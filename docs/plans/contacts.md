# Contacts: one core kind for everyone the owner deals with

Status: proposal, 2026-11-06. Line numbers are against `main` at
`b9cf1ffc2`. Nothing here is merged: step 1 is
[#1210](https://github.com/geoah/substrate/pull/1210), with decision record
0229. Two design reviews of earlier drafts are folded in; what each changed
is listed at the end.

## The problem

No kind every repository holds can stand for "someone or something I deal
with". `samples.substrate.reamde.dev/people/person` is a sample: an import
rehomes it to `<authority>/people/person`, so its name differs per repository
and no provider, app or other substrate can name it. Mneme forked it into
`mneme.rxtx.dev/people/person` and then had to add what it lacked:

- a `notaperson` prominence state, because the mappings minted a person for
  every bot, mailing list and shared mailbox;
- `employer` and `role` strings, because `memberOf` is a reference and the
  company string an address book holds had nowhere to go;
- a `hidden` state for a forgotten person.

The owner wants one kind with a fixed name that is the anchor for a person, a
business, a group or a bot; that the Apple, Google, Slack, GitHub, Linear and
Beeper mirrors link to; and that names the party's own substrate, for linking
and sharing between substrates later.

## Settled with the owner (2026-11-06)

- **One kind, `substrate.reamde.dev/core/contact`**, in core. A package with
  a single kind was rejected, and no plan names a second kind that belongs
  with contacts: the sample's `organization` and `team` become contacts of
  type `organization` and `group`.
- **The address-book card shape**: flat name parts, the card's company and
  job-title strings, a type, a prominence. Every address and account stays in
  a list on the contact; there is no per-address kind.
- **The field naming the party's substrate is `substrate`**, a hostname.
- **How a person relates to the owner is group membership**: a contact of
  type `group` named Family, Friends or Coworkers, and `memberOf` it. The
  sample's enum of how a person relates is not carried over.
- **A key/value place for dates** on the contact.
- **Shipped views**: the Contacts collection opens on People, with tabs for
  the other types.
- **The people sample is retired.** Nothing migrates a repository off it:
  the release note says the official contacts kind exists and that users
  should move to it (third round, below). It is not a break.
- `jobTitle`, not `title`: `title` is reserved on every kind
  (`internal/vocabulary/load.go:1336`, decision 0016).

Settled in a second round, the same day:

- **No `lastContactAt` on the contact.** It stays on Mneme's own kind.
- **The owner's own contact is `substrate.reamde.dev/core/contact/me`** (see
  [The owner's own contact](#the-owners-own-contact)).
- **The shipped views are built separately**, as
  [#1208](https://github.com/geoah/substrate/issues/1208): the
  `console/collectionview` kind, its seed and the console's tabs. The
  contacts work adds the six contact views once both land.

Settled in a third round, the same day:

- **No migration.** There is no move operation, no `substratectl contacts
  migrate` and no repository migration. The release note says that
  `substrate.reamde.dev/core/contact` is the official contacts kind and that
  users should move to it; a repository that imported the people sample
  keeps its people kinds and records, and moves when its owner chooses.
- **Removing the people sample is not a break.** Nothing a repository holds
  changes; only importing the sample by name stops working. The change is a
  `feat:` whose title is the note.

## Prior art

| Source | Party types | Name | Accounts | Dates |
| --- | --- | --- | --- | --- |
| vCard 4 (RFC 6350, 6473) | `KIND`: individual, group, org, location, application | `FN` formatted, required, plus `N` parts | `EMAIL`, `TEL`, `IMPP`, `URL` | `BDAY`, `ANNIVERSARY`, year optional |
| JSContact (RFC 9553) | `kind`: individual, group, org, location, device, application | `name.full` plus `name.components` | `emails`, `phones`, `onlineServices` | `anniversaries` (birth, death, wedding), partial dates |
| schema.org | `Person`, `Organization` | `name`, `givenName`, `familyName` | `email`, `telephone`, `sameAs` URLs | `birthDate` |
| Apple `CNContact` (mirrored as `apple/contact`) | `contactType`: person, organization | flat `givenName`, `familyName`, `nickname`, `organizationName`, `jobTitle` | `emailAddresses`, `phoneNumbers`, `socialProfiles` | `birthday` and labelled `dates`, year optional |
| Google People (mirrored as `google/contact`) | none | `names[]` | `emailAddresses`, `phoneNumbers`, `urls` | `birthdays`, `events` |
| ActivityPub | Person, Organization, Group, Application, Service | `name` | the actor URL | none |

Every format types the party with one field, keeps a formatted name shown
as-is with optional parts beside it, allows a date without a year, and treats
an account on a service as a pointer. In substrate the provider mirror
(`slack/user`, `github/user`) already is that pointer: a recordmapping fills
its subject reference (decisions 0049, 0096).

## The kind

```yaml
kind: substrate.reamde.dev/core/kind
metadata:
  id: substrate.reamde.dev/core/contact
data:
  authority: substrate.reamde.dev
  package: core
  description: >-
    Someone or something you deal with: a person, a business, a group or a
    bot. One record each; every account it has on a service (a Google
    contact, a Slack member, a GitHub user) is that provider's record
    pointing here.
  purpose: primary
  names:
    singular: contact
  label:
    singular: Contact
    plural: Contacts
  displayTemplate: "{name|nickname|organizationName|emails[]|phones[]|substrate}"
  properties:
    contactType:
      type: enum
      description: what the contact is; absent means not yet known
      values:
        - person
        - organization
        - group
        - bot
    prominence:
      type: state
      description: utility until an address book or the owner promotes it to
        known; hidden is the owner forgetting it
      states: [utility, known, hidden]
      initial: utility
      transitions:
        - from: utility
          to: known
        - from: known
          to: utility
        - from: utility
          to: hidden
        - from: known
          to: hidden
        - from: hidden
          to: utility
        - from: hidden
          to: known
    name:
      type: string
      description: the full name to show, as the source or the owner wrote it
    givenName:
      type: string
      description: the first name
    familyName:
      type: string
      description: the last name
    nickname:
      type: string
      description: what the owner calls them
    organizationName:
      type: string
      description: the company as the card says it; memberOf is the record
    jobTitle:
      type: string
      description: the job title as the card says it
    memberOf:
      type: reference
      kind: substrate.reamde.dev/core/contact
      repeated: true
      mustExist: true
      description: the organizations and groups this contact belongs to
      properties:
        role:
          type: string
          description: the role held there, such as a title or lead
        since:
          type: date
          description: the day the membership started
    emails:
      type: email
      repeated: true
      description: every address seen for this contact
    phones:
      type: string
      repeated: true
      description: numbers as providers send them, not E.164-only
    urls:
      type: url
      repeated: true
      description: web pages about this contact, such as a homepage or profile
    substrate:
      type: string
      pattern: "^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$"
      description: the contact's own substrate repository name, such as
        ada.example.org
    pronouns:
      type: string
      description: free text; empty means unknown, so use they/them
    dates:
      type: object
      keyed: true
      keyPattern: camel
      description: named days, such as birthday or anniversary; the year may be
        absent
      fields:
        year:
          type: int
          min: 1
        month:
          type: int
          required: true
          min: 1
          max: 12
        day:
          type: int
          required: true
          min: 1
          max: 31
    details:
      type: markdown
      embed: true
      description: what is worth remembering about this contact, as prose
```

Choices worth a sentence each:

- **`contactType`, not `type`.** `type` is a dead word in
  [terms](../terms.md) (`type` became kind), and on a substrate record "the
  contact's type" reads as its kind. `contactType` is also Apple's field name.
  A mapping checks only that both ends are enums
  (`internal/vocabulary/mapping.go:541`), so Apple's two values land on the
  four-value target unchanged.
- **No `notaperson` state.** Mneme used it for what `contactType: bot` and
  `contactType: group` now say. Prominence is only how visible a contact is.
- **No `location` or `device` type** until something in a repository deals
  with one.
- **Flat names.** A mapping writes top-level properties only: `map` keys and
  `match[].to` name one property (`internal/vocabulary/mapping.go:300`,
  `:315`), so an object-typed name could not be filled from Apple or Google.
- **`displayTemplate` picks, it never joins.** A `|` alternative selects the
  first that renders (docs/vocabulary.md, "A `displayTemplate` is literal
  text and tokens"), so `name` must be written whole. A writer that has only
  parts composes `name` itself and never overwrites one already written.
- **`substrate` is unverified.** It says where to find the contact's
  substrate (`_substrate.<name>`, decision 0208), not that the contact owns
  it. Changing the pattern later is a narrowing, refused while a stored value
  fails it, so the first pattern must be right.
- **Ids.** Contacts are minted by the server. PR 1199 (open) would make every
  non-internal kind a minted-id collection, so a person or an agent creates a
  contact without choosing its id; the engine still writes `contact/me`.

### Making a core kind visible

Every core kind is machinery today, and three places assume it:

- The console's `kindPurpose` returns `internal` for every core, llm and
  console kind, whatever the declaration says
  (`web/console/src/lib/definition.ts:46`), and the everyday sidebar hides
  the system group.
- `TestEveryShippedPurposeIsValidAndTheSeedIsInternal` refuses a non-internal
  core kind not listed in `seededOwnerData` (`kinds/kinds_test.go:350`).
- The generic merge refuses every built-in kind of core, llm and console
  (`guardMergeType`, `internal/engine/merge.go:287`, through
  `SubstrateStatePackage`, `internal/vocabulary/document.go:53`). Duplicate
  contacts must merge.

Each needs an exception for `core/contact`, with the same shape as the one
`llm/memory` already has in the test. The console's check reads the declared
purpose for those kinds instead of the package.

### Dates

`dates` is a keyed map: the key names the day (`birthday`, `anniversary`,
`metOn`) and the value is a year-optional date in Apple's shape, so a
birthday saved without a year stays without one. Three limits decide what
belongs in it:

- **A write replaces the whole map** (docs/api.md, "`PATCH` semantics are
  pinned"). Two writers updating different keys race, and the property has
  one manager row, so the map cannot say that the birthday came from the
  owner and the anniversary from an agent.
- **A keyed map is outside the filter grammar and search**
  (docs/data-model.md, "Keyed maps"). "Birthdays this week" is a client-side
  scan, never a query.
- **No mapping can fill it from Apple.** Apple's `birthday` is a single
  object and its `dates` a labelled list; a mapping copies a whole property
  of a matching shape and cannot turn a label into a key.

So `dates` holds days the owner or an agent records and that rarely change.
**Last contact does not belong in it.** Mneme writes it hourly from messages,
it is what a list sorts by, and it would race the owner's own edits. It stays
on Mneme's own kind (see Mneme below); if a second app needs it, it becomes a
top-level `lastContactAt` datetime on the contact, which is filterable.

One reviewer preferred a top-level `birthday: date` because it filters. It
cannot hold a birthday without a year, and the owner asked for one place for
any named day, so this plan keeps the map.

### Groups

A group is a contact with `contactType: group`, and membership is `memberOf`
with an optional `role`. One shape covers four cases:

| Group | Example | Filled by |
| --- | --- | --- |
| The owner's own grouping | Family, Friends, Coworkers | the owner or an agent |
| A provider's label | a Google contact label | a function (see below) |
| A team | Platform team; leads carry `role: lead` | the owner, or Mneme's `teamsfromwork` |
| A list or shared mailbox | `eng-all@example.com` | the owner or an agent marking a minted contact |

A Google label cannot become `memberOf` through a mapping. The reference sits
at `memberships[].contactGroupMembership.contactGroup`, two object levels
down, and a map path reaches one (`internal/vocabulary/mapping.go:134`).
`google/contactgroup` can still map onto a group contact (`where:` on
`groupType` keeps the user's own labels); filling each member's `memberOf`
is a function's job.

### The owner's own contact

The owner is the contact with the id `me`. The engine writes
`substrate.reamde.dev/core/contact/me` when it creates a repository
(`contactType: person`, `prominence: known`, `substrate` set to the
repository's name), and at the first open of a repository created before the
kind existed. It follows the pattern of the three keyless `llm/provider` rows
(`internal/engine/seedproviders.go`): written only where no hand but the
engine's ever wrote or deleted the record, so an owner's edit stands and a
deleted `me` stays deleted.

- **Why an id and not a property.** A flag or a matching `substrate` value
  can be held by two contacts; an id cannot. A `contact` reference on the
  core `repository` record is not writable: `repository` is a system kind that
  takes only lifecycle transitions (`internal/engine/dataset.go:60`,
  `internal/engine/write.go:640`).
- **A merge keeps it reachable.** A read by a former id returns the winner
  (docs/projection.md, "Former ids resolve to the winner"), so merging `me`
  with the owner's Google contact leaves `contact/me` resolving to the
  merged record whichever side wins.
- **The id is legal under PR 1199.** That PR makes `contact` a minted-id
  kind (it is not `internal` and declares no `ids`), and the engine is one of
  the writers that keep an id they choose; a person or an agent could not
  create `me`.

Mneme's `owneridentity` then proposes merging each connected account's
identity into `contact/me`, instead of choosing a winner between two persons.

## Console views

Built separately as [#1208](https://github.com/geoah/substrate/issues/1208);
the contacts work ships only the six view records once it lands.

The Contacts collection opens on a People tab, with tabs beside it for the
other types. The console has no way to ship such tabs today. A saved view (a
named filter, sort, column set and grouping on a collection's tab strip) lives
only in the owner's `consolepreference/navigation` record, written by the
console (`kinds/substrate.reamde.dev/core/consolepreference.yaml:69`). A
record view (`console/recordview`) lays out one record's page, not a list
(decision 0166).

**Proposed: shipped saved views are records of a new seeded console kind**,
`substrate.reamde.dev/console/collectionview`, following 0166's reasoning for
record views: a package ships them as data, the owner may edit them, and the
console names no kind of its own accord. A record holds what a saved view
holds, with the filter in the API's grammar rather than the console's private
filter tokens:

| Property | Holds |
| --- | --- |
| `appliesTo` | reference to the kind declaration the view belongs to |
| `name` | the tab's name |
| `where` | `json`, one filter condition per property, as `recordmapping.where` (decision 0118) |
| `sort` | `property:asc` or `property:desc` |
| `columns` | the column ids, in order |
| `group` | the property the rows are grouped by |
| `default` | `bool`; the tab a bare address opens on when the browser remembers nothing for the collection |

The console shows the shipped views after **All** and before the owner's own
saved views, in id order. The views shipped for `core/contact`:

| Tab | `where` | Notes |
| --- | --- | --- |
| People | `{"contactType": {"eq": "person"}, "prominence": {"eq": "known"}}` | `default: true` |
| Organizations | `{"contactType": {"eq": "organization"}}` | |
| Groups | `{"contactType": {"eq": "group"}}` | |
| Bots | `{"contactType": {"eq": "bot"}}` | |
| To sort | `{"contactType": {"exists": false}}` | contacts minted from an address no one has typed yet |
| Hidden | `{"prominence": {"eq": "hidden"}}` | |

People shows only `known` contacts, because most minted contacts are
addresses seen once in mail: in Mneme's use, about half of all persons came
from a mail address alone. The views ship in the console package's
directory, since the console package is seeded with core and owns the kind
they are records of.

"Default" here means the tab a collection opens on. Making Contacts the
console's landing page instead of Home is a separate choice, not proposed.

What building this takes, beyond the kind:

- **A record seed.** The seed builds packages from declarations; the only
  shipped records core carries today are units, written by their own seed
  (`internal/engine/unitseed.go`, decision 0186). The views need that seed
  generalised to any shipped record, written after the declarations land,
  with 0186's rule that an owner's edit to a shipped record holds. (A
  package's own `recordview` records reach a repository through a catalog
  install, which a seeded package never takes.)
- **An `exists` filter in the console.** The console's filter operators are
  `eq`, `contains`, `prefix` and `match` (`web/console/src/lib/filters.ts:27`),
  so "To sort" could neither show in the toolbar nor light its tab. The
  console gains `exists` in its filter tokens and its URL form; the API
  already takes it.
- **One conversion each way** between a view's `where` and the console's
  `field~op~value` tokens, which the tab strip, the URL and `localStorage`
  all use.
- **Remembering All.** A collection restores its last filters from
  `localStorage`, and it stores no filter for All, the same as for a
  collection never opened, so choosing All would reopen People next time
  (`web/console/src/components/kind-browse.tsx:224`). The stored state gains
  the chosen view's id, All included.

`collectionview` passes `kinds:check`: the console package has one other
kind, so it has no family whose ending the name could copy.

## What fills it

**Mappings.** The loader refuses a mapping onto a kind outside the declaring
package (`internal/vocabulary/mapping.go:263`), and decision 0049 says
providers ship no mappings. So core declares every mapping onto `contact`.
Core is seeded before any provider is installed, and nothing holds back a
seeded mapping whose source is absent: decision 0106 is an opt-in of the apply
door, the catalog prunes on its own path, and mapping resolution refuses an
absent source.

**Proposed: core ships the mappings, and each is held back while its
provider is not installed.** Nothing is stored: the registry recomputes which
mappings are held whenever it is built, at every open and after every install
and apply. A repository that still runs the people sample, or a fork of it
such as Mneme's, gets both: its own mapping fills the mirror's `person` slot
and the core one fills the `contact` slot, which 0049 item 2 allows (one
mapping per source kind and subject property). Its contacts fill in beside
its people, so moving to contacts is a matter of switching what it reads. The
other way out (each provider declares its own mapping onto `core/contact`,
amending 0049 and the loader check) needs the same hold in every provider.

| From | Match | Notes |
| --- | --- | --- |
| `apple/contact` | `emailAddresses[].value` to `emails`, then `displayName` to `name` | maps the name parts, `contactType`, `organizationName`, `jobTitle` |
| `google/contact` | `emailAddresses[].value` to `emails` | |
| `google/contactgroup` | none, mints | `where:` user labels only |
| `google/emailaddress` | `address` to `emails` | an address seen in mail; no name |
| `slack/user` | `profile.email` to `emails`, then `realName` to `name` | |
| `slack/bot` | none, mints | |
| `github/user` | `email` to `emails` | |
| `linear/user` | `email` to `emails` | |
| `beeper/user` | as Mneme's `mappings.beeper.yaml` | |

Each mapping's subject slot on the mirror is named `contact`.

**Writers a mapping cannot be.** A mapping cannot move a state
(`internal/vocabulary/mapping.go:536`), translate enum values or write a
constant. So:

- Promotion to `known` when an address book links a contact is a function,
  as Mneme's `promotecontact` and `promoteapplecontact` are today. It never
  demotes and never touches a `hidden` contact.
- `contactType: bot` from `slack/bot` or from `github/user.type` (`Bot`),
  `organization` from `github/user.type` (`Organization`) and `group` from
  `google/contactgroup` need a writer too.
- A Google label's members need a function to write `memberOf`.

For the first version these stay with apps (Mneme already has the
promotion). Making them declarative (a mapping that sets a constant,
translates values or moves the subject's state on link) is new dialect and
gets its own record.

## Retiring the people sample

There is no migration. `samples.substrate.reamde.dev/people` (`person`,
`organization`, `team` and its four mappings) is deleted from `samples/`, as
0192 did for the observation samples, so it can no longer be imported. The
calendar, messaging and tasks samples get new versions that pin
`substrate.reamde.dev/core/contact` where they pinned `people/person` (for
example `samples/calendar/calendarevent.yaml:85`,
`samples/tasks/task.yaml:137`) and no longer require the people sample.
About 100 Go and console files name the people sample (tests, fixtures, docs
examples); each moves to `core/contact` in the same change.

A repository that imported the people sample keeps its
`<authority>/people/*` kinds, records and mappings: they are its own copies,
and nothing deletes or rewrites them. Its imported calendar, messaging and
tasks samples are offered their new versions, and that upgrade is refused
while records still point at `person`, because the new pin is a narrowing.
The owner moves those references to contacts first, by hand or through an
agent, or keeps the old versions.

### The release note

The change is not a break. Nothing a repository holds changes: its imported
people kinds stay, and an imported sample is only ever offered an upgrade
(docs/bundles.md), never given one. The one thing that stops working is
importing the people sample by name, which a script or an old walkthrough
might do.

release-please prints a `feat:` title under Added and no body, so the title is
the note:

```text
feat(samples): replace the people sample with the core contact kind
```

The body says the rest, for anyone who opens the pull request:
`substrate.reamde.dev/core/contact` is the official contacts kind; move to
it; a repository that imported the people sample keeps its people kinds and
records; its calendar, messaging and tasks samples are offered their new
versions, which wait until no record of theirs points at a person.

### Mneme

Mneme forked the sample and moves to `core/contact` on its own schedule. The
core mappings fill its contacts beside its people from the first release.
Where each part of Mneme's people package would go:

| Mneme | On `core/contact` |
| --- | --- |
| `person` name, emails, phones, pronouns, details | the same properties |
| `person.displayName` | `nickname` only where it differs from `name`: Mneme's Google, Apple and Linear mappings write the full name there, and GitHub's writes the login |
| the enum of how a person relates | group contacts and `memberOf` |
| `prominence` `utility`, `known`, `hidden` | the same states |
| `prominence: notaperson` | `contactType: bot` or `group`, per contact; not every one is a bot |
| `memberOf`, `role`, `employer` | `memberOf`, `jobTitle`, `organizationName` |
| `birthday` | `dates.birthday` |
| `location`, `timezone` | no home on the contact; Mneme's own kind |
| `lastContactAt`, `lastContactVia`, `cadenceDays`, `owedByOwner`, `owedToOwner` | a Mneme kind with one record per contact, written by `standing` |
| `organization` | a contact of type `organization` |
| `team` | a contact of type `group`; `organization` becomes its `memberOf`, `groupEmail` an `emails` entry |
| `commitment`, `forgotten`, `voiceprint` | Mneme kinds whose `person` reference re-pins to `core/contact` |
| `standing`, `teamsfromwork`, `owneridentity`, `promotecontact`, `promoteapplecontact` | Mneme functions, rewritten against `contact` |
| eight mappings onto `person` (Apple, Beeper, GitHub, Google contact, Google address, Linear, Notion, Slack) | core's mappings cover all but Notion's |

## Decisions this needs

Each is a record in [docs/decisions](../decisions/README.md) before the
implementation merges:

1. `contact` is a core kind with `purpose: primary`; the console and the
   generic merge treat it as owner data, and the owner's own contact is
   `contact/me`, written by the engine.
2. Core declares mappings from provider kinds, held back while the provider
   is absent, recomputed whenever the registry is built.
3. A collection's shipped saved views are records of a seeded console kind
   (a sibling of 0166), written by #1208.
4. The people sample is removed from the catalog, with no migration.

## Steps

Each step is one pull request against `main`. A merge releases nothing, so
steps 1 to 3 reach a release together; each is safe on `main` alone.

1. Decision record 1. The `contact` kind in core with its three exceptions
   (console, shipped-purpose test, merge) and the `contact/me` seed
   ([#1210](https://github.com/geoah/substrate/pull/1210)). Additive:
   nothing maps onto the kind yet.
2. Decision record 2 and the core mappings, held while their provider is
   absent.
3. Decision record 4, under the title above: calendar, messaging and tasks
   sample versions that pin `core/contact` and no longer require people, and
   the people sample removed.
4. The six contact views, after #1208 lands.

## Reviews

- **Astra (gpt-6-astra), first draft.** `title` is reserved; mappings cannot
  write states, constants or translated values; nothing holds back a seeded
  mapping whose provider is absent; Apple's yearless birthdays rule out a
  plain `date`; the mapping graph is bipartite, which ruled out a separate
  handle kind between mirror and contact.
- **Fable (claude-fable-5-1), second draft.** Corrected here: the Google label
  path is two levels deep and no mapping reaches it; providers cannot declare
  mappings onto core without a loader change; the console hides every core
  kind; `repository` is not writable; flow mappings fail `lint:yaml`; 0078's
  other refusals; the order of steps, which would have minted duplicates; the
  release note's order and rollback; the Mneme table. Not taken: replacing
  `dates` with a top-level `birthday: date` (see Dates). A second pass on
  the console views and the rewrite added: the record seed, the console's
  `exists` filter and remembered All, the narrower hold rule, shipping the
  sample repins in the same release as the kind, and an operator restore as
  the rollback. The owner then dropped the migration, which removed the
  move operation and the hold on the people sample's mappings.
