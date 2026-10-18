# Built-in kinds

This page maps the vocabulary this binary ships, package by package: the three
packages a repository is seeded with, `substrate.reamde.dev/core`,
`substrate.reamde.dev/llm` and `substrate.reamde.dev/console`, the LLM sample copied onto the repository's own
authority at registration, and the other sample packages it can import. A kind is named `<authority>/<package>/<name>`;
the tables give the name, and each heading gives the package as the tree
spells it, under the placeholder authority an import rewrites. Which door a
package takes, what an import rewrites and how an upgrade is offered are in
[bundles](bundles.md#the-two-doors); the provider packages, and the five
samples this page does not table (notes, llm, readinglist, firecrawl and pebble, whose
kinds are described beside the functions and agents that write them), are in
the [bundles catalog](bundles-catalog.md). Every declaration is queryable in
your own repository (`substratectl kinds`, or `GET /api/v1/records` with
`substrate.reamde.dev/core/kind` in `filter.kinds`), descriptions included, so this page
is the map and the repository is the source of truth.

The tables for core are [below](#substratereamdedevcore); what follows first is
the vocabulary you import.

## samples.substrate.reamde.dev/people (a sample)

| Kind           | What it is                                                                                                                              |
| -------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `person`       | One human, one record: every pointer that means "a person" lands here. What a single source holds about them is its own record, mapped on. |
| `organization` | An org a person belongs to: employer, workspace, publisher.                                                                             |
| `team`         | A working group finer than an organization: members, leads, and nesting through `parent`.                                               |

This sample also ships four **suggested mappings** onto its own `person`, from
GitHub's `user`, Google's `contact` and `emailaddress`, and Linear's `user`.
Each is admitted only where you already hold the
provider it reads, and reported `waiting` for that provider otherwise
([suggested mappings](bundles.md#suggested-mappings)). Installing the provider
afterwards does not land it: import this sample again, and then a GitHub
identity and the same human's address-book contact converge on one record of
yours.

`person` carries a two-state `prominence` machine: `utility` at birth, `known`
once something promotes it (an address-book sync, or you). Search ranks
`utility` people below every `known` match
([search](api.md#search)). Its `pronouns` are free text, never
an enum, and empty means unknown: a surface rendering a person without a
value says so and falls back to they/them.

## samples.substrate.reamde.dev/messaging (a sample)

| Kind                  | What it is                                                             |
| --------------------- | ---------------------------------------------------------------------- |
| `conversation`        | A DM, a group chat, or a channel.                                      |
| `conversationmessage` | One message in a conversation; outbound ones walk the delivery states. |
| `emailthread`         | One mail thread.                                                       |
| `emailmessage`        | One mail message in a thread.                                          |

## samples.substrate.reamde.dev/scheduling (a sample)

One trait and no kinds. `calendar` and `tasks` both `require` it, and `tasklog`
binds its trait across packages, the way `task` binds core's `temporal`,
`recurring` and `override` ([traits](traits.md)).

| Trait           | What it is                                                                                                                         |
| --------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `occurrencelog` | The done-or-skipped mark against one occurrence of a recurring record: `status`, `scheduledAt`, `details`. Absence means missed.   |

## samples.substrate.reamde.dev/calendar (a sample)

| Kind                  | What it is                                                              |
| --------------------- | ----------------------------------------------------------------------- |
| `calendar`            | One provider calendar.                                                  |
| `calendarevent`       | Always a concrete occurrence; providers explode series into these.      |
| `calendareventseries` | The recurring definition, RRULE and exceptions, never on the timeline.  |
| `transcript`          | A meeting transcript, pointed at the occurrence that actually happened. |

## samples.substrate.reamde.dev/tasks (a sample)

| Kind      | What it is                                                                  |
| --------- | ---------------------------------------------------------------------------- |
| `task`    | Something to do, with priority and an optional repeat rule seeded off `dueAt`. |
| `project` | What tasks group under: a name, a lifecycle, a summary.                       |
| `tasklog` | The done-or-skipped mark against one occurrence of a recurring task.          |

It ships one **suggested mapping** too, from Linear's `issue` onto its own
`task`: matched on the issue's URL, carrying the heading and the link, and
never `status`, which is a state and moves only through its own transitions.
Like every suggested mapping it lands only where the Linear provider is already
installed, and importing this sample again is what lands it afterwards
([suggested mappings](bundles.md#suggested-mappings)).

## samples.substrate.reamde.dev/observations (a sample)

Measured values and where they came from, the base every observing sample
requires. [Observations](observations.md) has every property and the rules a
write is held to.

| Kind          | What it is                                                                                                   |
| ------------- | ------------------------------------------------------------------------------------------------------------ |
| `metric`      | Something that can be measured, binding core's `metric`: its names, codes in other systems, unit, rollup tiers and how its values are read. |
| `observation` | One measured value, binding core's `observed(single)`: a number with its unit, or a code.                   |
| `trace`       | The values of one metric from one source over one fixed period, binding `observed(packed)`.                  |
| `source`      | The device, app, service or lab a value came from, binding core's `source`: its rank per metric.            |

It ships six metric records several domains share: `air_temperature`,
`relative_humidity`, `energy`, `power`, `illuminance` and
`atmospheric_pressure`.

## samples.substrate.reamde.dev/health (a sample)

A person's body, sleep, training and blood tests, on top of `observations`,
which it requires.

| Kind        | What it is                                                                                            |
| ----------- | ----------------------------------------------------------------------------------------------------- |
| `analyte`   | The lab facts about one blood test metric: LOINC parts, molar mass, conversions, a fallback range.   |
| `sleep`     | One night or nap: its span, time in each stage, efficiency and the device's scores.                   |
| `workout`   | One session: sport, span, distance, active and total energy, heart rate and time in each zone.        |
| `labreport` | One report from a lab, with its status and the original document.                                     |
| `labresult` | One analyte's result, binding `observed(single)` and `result`, with the lab's range, flag, specimen and status. |

It ships a catalogue of 59 metric records with ids `health/<name>`, such as
`health/heart_rate` and `health/ldl_cholesterol`, each with its LOINC,
HealthKit, Health Connect, Google Fit, WHOOP, Garmin or Withings codes. It
ships 18 `analyte` records, one for each metric of the nine lab pairs (one
analyte in mass and in molar units, and HbA1c as NGSP % and IFCC mmol/mol).
Its function `convertlab`, fired by its trigger `health-on-labresult` on every
write of a `labresult`, writes the result again in the other metric of its
pair ([lab results in both unit systems](observations.md#lab-results-in-both-unit-systems)).
It ships two record views, the record pages of a
sleep and a workout ([what the health sample adds](observations.md#what-the-health-sample-adds)).

## substrate.reamde.dev/core

The substrate's own machinery, declared as kinds so it lists, reads, and
filters exactly like vocabulary. This is the whole of what a fresh repository
speaks:

| Kind                 | What it is                                                                                                                                          |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `repository`         | The repository describing itself from the inside: the `authority` it owns, which is its id (`name` is deprecated and holds the same value), and a `lifecycle` state (`active`, `suspended`, `deleting`). |
| `credential`         | The one record (id `self`) holding your auth material by reference into the sealed store ([users and tokens](auth.md)).                                         |
| `recoverykey`        | The one record (id `self`) holding the age recipient the user enrolled and the repository's data-encryption key wrapped to it; only the user's age identity opens the wrap.  |
| `token`              | One bearer credential: label, optional expiry, and the hash of its secret.                                                                          |
| `actor`              | One declared actor, the name writes are attributed to, and the tier it writes at.                                                                   |
| `agent`              | One declared agent: an LLM-loop callable ([agents](agents.md)).                                                                                    |
| `blob`               | One content-addressed blob: the manifest is the metadata, the bytes live in the byte store; the digest is the id.                                   |
| `recordmerge`        | One performed merge and what it moved ([merges](projection.md#merges)).                                                                            |
| `recordsplit`        | The undo of one merge, likewise performed on creation.                                                                                              |
| `recordmergerequest` | A proposed merge, performed when its decision is accepted.                                                                                          |
| `recordpatchrequest` | A proposed create, patch, or delete, applied when its decision is accepted ([the patch request sibling](projection.md#the-patch-request-sibling)). |
| `recordpatchpolicy`  | An owner's standing rule for the writes of agents and functions: a `selector` (kinds, ops, agents, functions) and an `action` of `allow`, `gate` or `refuse`, and on a narrow allow the gate it `overrides` ([the policy door](agents.md#the-policy-door)). |
| `setting`            | One configuration value a bundle needs, at `<bundle id>/<name>`: `value` as a string, held on write to a `type` of `string`, `url`, `int`, `bool` or `enum` with `values` ([settings](bundles.md#settings)). |
| `secret`             | The same, with `value` typed `secret`: sealed at rest, injected only into its own bundle's functions, never read back.                               |
| `consolepreference`  | The console's preferences for the repository, shared across sessions: navigation (`collapsed`, `favorites`; `sidebarOpen` is deprecated), layout (`recordWidth`, `tableWidth`, `density`, `technicalDetails`, `theme`) and each collection's saved `views`. |

The delivery machinery is core's too, declared as data kinds so a trigger is
console-editable and changelog-visible like anything else
([functions](functions.md#triggers)):

| Kind      | What it is                                                                                                             |
| --------- | ---------------------------------------------------------------------------------------------------------------------- |
| `trigger` | One binding of a source (a record subscription, a schedule, or a public webhook endpoint) to one callable, owning the delivery cursor. |
| `triggerrun` | One trigger delivery attempt, written after it settles: the delivery ledger's row. Parked runs stay until retried away; the rest are pruned to the newest few per trigger. A direct call of a networked function writes one too, with mode `call` and no trigger, never pruned. |
| `alert` | One ongoing problem the substrate found, such as a trigger whose deliveries keep parking: a `key` its id derives from, a `level`, a `summary`, the last error as `detail`, the records it is `about`, when it was first and last seen, a `count` and a `state` of `open` or `resolved`. The engine opens, updates and resolves it ([decision 0148](decisions/0148-an-ongoing-problem-is-one-alert-record-written-under-the-callable-it-is-about.md)). |

The ten [declarable kinds](vocabulary.md#the-declarable-kinds) (`authority`,
`package`, `kind`, `propertytype`, `trait`, `recordmapping`, `function`,
`agent`, `bundle`, `actor`) live in core too, and so do the eleven shipped
traits: `temporal`, which puts a record on the timeline; `recurring` and
`override`, the repeat rule and the moved occurrence ([traits](traits.md));
`sync`, the state a provider's function reports on one connected account;
`accountconfig` and `oauth2`, which the OAuth facility recognizes;
`observed`, which marks a kind whose records hold observed values and holds
every write of one to the trait's rules; `revised`, which refuses a
rewrite whose `revision` is below the stored one; `metric` and `source`,
what a reader takes from the metric and the source an observed value
names; and `result`, a laboratory's result shapes beside
a number. Core also
ships four host functions as `function` records (`query`, `write`, `propose`,
`ask`), the built-ins an agent calls ([agents](agents.md)).

## substrate.reamde.dev/llm

The second package the binary seeds: the agent runtime's own data, out of core
so core is not everything (decision record
[0077](decisions/0077-the-llm-kinds-live-in-their-own-seeded-package.md)). It
is seeded exactly as core is, so a fresh repository holds these four kinds
without importing anything. **Agents are alpha**, so all four are a preview,
unfrozen at v1 and not part of the frozen core:

| Kind          | What it is                                                                                       |
| ------------- | ------------------------------------------------------------------------------------------------ |
| `provider`    | One place completions are bought: `label`, `wire` (enum), `baseURL`, `apiKey`, `embedModel`, `headers` and `pricing` (repeated objects), `defaults` (object). |
| `thread`      | One agent run's conversation state, written as the loop runs — its `provider` and `model` included. |
| `message`     | One turn in a thread, with its tool-call audit.                                                  |
| `interaction` | One batch of questions an agent asked the user, waiting in the thread it came from; answering or dismissing it is one reviewed owner transition that resumes the agent. Landed by the `ask` built-in. |

The `agent` kind itself stays in core: it is one of the declarable kinds, and a
manifest document is a record of a core kind whatever package it describes.

## substrate.reamde.dev/console

The third package the binary seeds: what the console shows, as records, so a
sample's kinds are presented from data and no console code names them
(decision record
[0166](decisions/0166-a-record-page-layout-is-a-record-in-a-seeded-console-package.md)).
It is seeded as core and llm are, and a repository created before it receives
it at its next open.

| Kind         | What it is                                                                                       |
| ------------ | ------------------------------------------------------------------------------------------------ |
| `recordview` | How the console lays out the record page of one kind: `appliesTo`, a reference to the kind's declaration; `name` and `intro`; the `window` it reads, by the names of the record's own properties; properties to `fold` away from the sheet; and `sections`, each one block: `heading`, `text`, `properties`, `related`, `chart` or `spans` (record views). |

A sample ships its views as records; a re-import replaces a view the owner
never wrote and leaves one the owner edited or deleted as the owner left it
([the two doors](bundles.md#the-two-doors)).
`consolepreference` stays in core.

Back to [the documentation index](README.md).
