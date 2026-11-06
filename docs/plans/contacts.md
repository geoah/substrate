# Contacts: one core kind for everyone the owner deals with

Status: proposal, 2026-11-06. Line numbers are against `main` at `b9cf1ffc2`.
Nothing here is implemented, and no decision record is written yet. Two design
reviews of earlier drafts are folded in; what each changed is listed at the
end.

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
- **The people sample is retired.** A repository that imported it is
  migrated by its owner's agent, following the steps in the release notes.
  Mneme is the testbed.
- `jobTitle`, not `title`: `title` is reserved on every kind
  (`internal/vocabulary/load.go:1336`, decision 0016).

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
  non-internal kind a minted-id collection. The migration below carries the
  sample's existing 12-character ids; that stays legal, because the move
  writes as the engine and PR 1199's guard judges only new creates.

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

## Console views

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
provider is not installed, or while the repository holds a mapping from the
same source onto a kind named `people/person` under any authority.** The
second condition keeps a repository that still runs the people sample, or a
fork of it such as Mneme's, from minting a duplicate contact beside every
person it already links. It names the old target on purpose: a mapping from
`github/user` onto an owner's own `developer` kind is legal beside the core
one (0049 item 2) and must not hold it. Nothing is stored: the registry
recomputes which mappings are held whenever it is built, at every open and
after every install, apply and move. The other way out (each provider
declares its own mapping onto `core/contact`, amending 0049 and the loader
check) spreads the same rule over every provider.

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

## Migration

### What the move must do

A repository that imported `samples.substrate.reamde.dev/people` holds
`<authority>/people/person`, `organization` and `team`. The sample ships four
mappings onto `person` (`samples/people/bundle.yaml`), and the calendar,
messaging and tasks samples pin `person` in their references (for example
`samples/calendar/calendarevent.yaml:85`, `samples/tasks/task.yaml:137`) and
require the people sample in their bundles.

| From | To |
| --- | --- |
| `person` | `contact`, `contactType: person`, same id |
| `person.name`, `emails`, `phones`, `pronouns`, `details`, `prominence` | same names |
| `person.displayName` | `nickname`, only where it differs from `name` |
| `person.memberOf` (organizations) | `memberOf`, pointing at the moved organizations |
| the sample's enum of how a person relates (six values, `unknown` included) | `memberOf` a group contact per value in use (Friends, Acquaintances, Family, Coworkers, Public figures); `unknown` and absent add nothing |
| `organization` | `contact`, `contactType: organization`; `domain` becomes a `urls` entry, `https://` prefixed |
| `team` | `contact`, `contactType: group`; `groupEmail` to `emails`, `details` kept; each member gets `memberOf` it, leads with `role: lead`; `parent` becomes the group's own `memberOf` |
| each mirror's `person` slot | its `contact` slot, same subject |
| the four people mappings | deleted |
| the core mappings onto `contact` | activated |
| calendar, messaging and tasks declarations pinning `person` | their new versions, pinning `core/contact` |

`movedFrom` (decision 0078) cannot do this. It moves only between kinds of
one authority, and an imported sample lives under the repository's own. It
also refuses an old kind that a mapping names or that declares a subject
reference, a dropped property, and a reference declaration left pinned at the
old kind; every one of those is in the table. An agent writing new records
cannot keep the ids either once PR 1199 lands.

**Proposed: a move operation the owner's agent calls**, built on 0078's
machinery: same ids, manager rows, labels and history carried, references
repointed and the old rows tombstoned, all in one transaction, with a preview
first. Beyond 0078 it:

- is requested with the owner's token, not run by the boot upgrade;
- takes a property map: rename, drop, set a constant, and turn a dropped
  enum's values into `memberOf` a named group;
- deletes the mappings onto the old kind, carries each mirror's slot to the
  new one, and activates the new kind's held mappings;
- admits the new versions of the declarations that pin the old kind in the
  same transaction, which 0078 refused at the apply door because that door
  publishes its candidate registry before any rewrite runs.

Turning teams into memberships is ordinary writes the agent makes after the
move. Classifying minted contacts as bots or groups is too.

### Retiring the sample

The catalog has no "retired" state for a sample; decision 0055 retires names
inside a declared package. The sample is deleted from `samples/`, as 0192 did
for the observation samples, so it can no longer be imported. About 100 Go
and console files name the people sample (tests, fixtures, docs examples);
each moves to `core/contact` in the same change.

### The release note

The release that ships the kind carries this `BREAKING CHANGE:` footer, which
release-please prints under BREAKING CHANGES. Command names are placeholders
until the operation exists; the PR that builds it replaces them.

```text
BREAKING CHANGE: `substrate.reamde.dev/core/contact` replaces the people
sample. `samples.substrate.reamde.dev/people` (person, organization, team) is
removed from the catalog. A repository that imported it keeps working, but
gets no contact mappings until it is migrated. To migrate, have your agent:
1. Take a backup: `substratectl export -o backup.tar`.
2. Re-pin every package of your own that references
   `<authority>/people/person`, `organization` or `team` to
   `substrate.reamde.dev/core/contact`, in a new version; step 4 refuses
   while one still pins the old kinds.
3. Preview: `substratectl contacts migrate --preview`. It lists every person,
   organization and team row, what each property becomes, the groups it
   creates for the how-they-relate values, and every reference it repoints.
4. Apply: `substratectl contacts migrate`. Rows keep their ids; calendar,
   messaging and tasks records point at contacts; the contact mappings start.
5. For each former team, add `memberOf` to its members (`role: lead` for its
   leads).
6. Read the contacts with no `contactType` and set `bot` or `group` where the
   address is a bot, a list or a shared mailbox.
Rollback: the move cannot be undone in place. Ask your server's operator to
restore `backup.tar` as docs/operations.md "Restore" describes: into a data
root whose database holds no row for this repository, then `repository
verify`. Extracting it over a live repository is not a rollback.
```

### Mneme, the testbed

Mneme forked the sample, so its migration is the same operation with its own
property map, driven by its agent. Before the move, the agent transitions
every `notaperson` row (the new kind refuses the state) to `hidden` or
`utility` and records which were `notaperson`, so it can set `contactType`
after. What Mneme has, and where each piece goes:

| Mneme | After |
| --- | --- |
| `person` name, emails, phones, pronouns, details | the same properties on `contact` |
| `person.displayName` | `nickname` only where it differs from `name`: Mneme's Google, Apple and Linear mappings write the full name there, and GitHub's writes the login |
| the enum of how a person relates | group contacts and `memberOf`, by the move's property map |
| `prominence` `utility`, `known`, `hidden` | the same states |
| `prominence: notaperson` | `contactType: bot` or `group`, decided per contact by the agent; not every one is a bot |
| `memberOf`, `role`, `employer` | `memberOf`, `jobTitle`, `organizationName` |
| `birthday` | `dates.birthday` |
| `location`, `timezone` | no home on the contact; stay on Mneme's own kind |
| `lastContactAt`, `lastContactVia`, `cadenceDays`, `owedByOwner`, `owedToOwner` | a Mneme kind with one record per contact, written by `standing` |
| `organization` | a contact of type `organization` |
| `team` | a contact of type `group`; `organization` becomes its `memberOf`, `groupEmail` an `emails` entry |
| `commitment`, `forgotten`, `voiceprint` | stay Mneme kinds; their `person` references re-pin to `core/contact` |
| `standing`, `teamsfromwork`, `owneridentity`, `promotecontact`, `promoteapplecontact` | stay Mneme functions, rewritten against `contact` |
| eight mappings onto `person` (Apple, Beeper, GitHub, Google contact, Google address, Linear, Notion, Slack) | deleted by the move; core's mappings cover all but Notion's |
| 39 files naming `people/person` (capture, memory, surfaces, rollup, reflection, commerce, tasks) | re-pinned before the move |

Mneme loses nothing it cannot keep in its own kinds. What moves out of the
person row is what only Mneme computes.

## Decisions this needs

Each is a record in [docs/decisions](../decisions/README.md) before the
implementation merges:

1. `contact` is a core kind with `purpose: primary`; the console and the
   generic merge treat it as owner data.
2. Core declares mappings from provider kinds, held back while the provider
   is absent or a mapping from the same source targets a `people/person`
   kind, recomputed whenever the registry is built.
3. A kind move on the owner's request, with a property map, the mappings and
   pinning declarations carried in the same transaction (extends 0078).
4. A collection's shipped saved views are records of a seeded console kind
   (a sibling of 0166).
5. The people sample is removed from the catalog.

## Steps

1. Decision records 1, 2 and 3, and the move operation with its preview,
   tested against a repository that imported the people sample.
2. In one release, with the footer above: the `contact` kind in core with
   its three exceptions (console, shipped-purpose test, merge); the held
   mappings; calendar, messaging and tasks sample versions that pin
   `core/contact` and no longer require people; the people sample removed
   (decision record 5). They ship together because a fresh repository
   importing tasks would otherwise import people, and its mappings would hold
   the core ones for a migration it never needed. A repository that imported
   the old samples is offered their upgrade, which waits for the migration:
   the new pin is a narrowing while records still point at `person`.
3. Decision record 4: the record seed, the `console/collectionview` kind, the
   console changes and the six contact views.
4. Mneme migrated by its agent against a copy of the owner's repository
   first, then the live one.

## Open questions

1. Whether the sample migration runs only when the agent asks (this plan) or
   also by itself at boot as a repository migration (decision 0099).
2. Whether `lastContactAt` belongs on the contact now or waits for a second
   app that needs it.
3. Which contact is the owner. Matching `substrate` against the
   repository's name is not enough, since two contacts may carry it, and a
   `contact` reference on the core `repository` record is not writable:
   `repository` is a system kind that takes only lifecycle transitions
   (`internal/engine/dataset.go:60`, `internal/engine/write.go:640`). Mneme
   keeps deciding it with `owneridentity`, which writes a merge request.

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
  the rollback.
