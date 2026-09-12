# Built-in kinds

This page maps the vocabulary this binary ships, package by package: the
`substrate.reamde.dev/core` package a repository is seeded with, and the
sample packages it can import. A kind is named `<authority>/<package>/<name>`;
the tables give the name, and each heading gives the package as the tree
spells it, under the placeholder authority an import rewrites. Which door a
package takes, what an import rewrites and how an upgrade is offered are in
[bundles](bundles.md#the-two-doors); the provider packages, and the five
samples this page does not table (notes, llm, web, firecrawl and pebble, whose
kinds are described beside the functions and agents that write them), are in
the [bundles catalog](bundles-catalog.md). Every declaration is queryable in
your own repository (`substratectl kinds`, or
`GET …/substrate.reamde.dev/core/kind`), descriptions included, so this page
is the map and the repository is the source of truth.

The tables for core are [below](#substratereamdedevcore); what follows first is
the vocabulary you import.

## samples.substrate.reamde.dev/people (a sample)

| Kind           | What it is                                                                                                                              |
| -------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `person`       | One human, one record: every pointer that means "a person" lands here. What a single source holds about them is its own record, mapped on. |
| `organization` | An org a person belongs to: employer, workspace, publisher.                                                                             |
| `team`         | A working group finer than an organization: members, leads, and nesting through `parent`.                                               |

This sample also ships five **suggested mappings** onto its own `person`, from
GitHub's `user`, Google's `contact` and `emailaddress`, and Linear's `user` and
an issue's `assignee`. Each is admitted only where you already hold the
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

Traits only, no kinds. `calendar` and `tasks` `require` it and bind its two
traits across packages, the way every package binds core's `temporal`
([traits](traits.md)).

| Trait           | What it is                                                                                                                         |
| --------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `recurring`     | An RFC 5545 RRULE the substrate stores and never expands: `recurrence`, `rdates`, `exdates`, `timezone`.                           |
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

## substrate.reamde.dev/core

The substrate's own machinery, declared as kinds so it lists, reads, and
filters exactly like vocabulary. This is the whole of what a fresh repository
speaks:

| Kind                 | What it is                                                                                                                                          |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `repository`         | The repository describing itself from the inside: its id, the authority it owns (the same value, and its `name` too), and a lifecycle state.        |
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
| `recordpatchpolicy`  | An owner's standing rule for an agent's writes: a `selector` (kinds, ops, agents) and an `action` of `allow`, `gate` or `refuse` ([the policy door](agents.md#the-policy-door)). |

The delivery machinery is core's too, declared as data kinds so a trigger is
console-editable and changelog-visible like anything else
([functions](functions.md#triggers)):

| Kind      | What it is                                                                                                             |
| --------- | ---------------------------------------------------------------------------------------------------------------------- |
| `trigger` | One binding of a source (a record subscription, a schedule, or a public webhook endpoint) to one callable, owning the delivery cursor. |
| `triggerrun` | One trigger delivery attempt, written after it settles: the delivery ledger's row. Parked runs stay until retried away; the rest are pruned to the newest few per trigger. |

So is the agent runtime's data. **Agents are alpha**, so these four are a
preview, unfrozen at v1 and not part of the frozen core:

| Kind             | What it is                                                                                       |
| ---------------- | ------------------------------------------------------------------------------------------------ |
| `llmprovider`    | One place completions are bought: `label`, `wire` (enum), `baseURL`, `apiKey`, `embedModel`, `headers` and `pricing` (repeated objects), `defaults` (object). |
| `llmthread`      | One agent run's conversation state, written as the loop runs — its `provider` and `model` included. |
| `llmmessage`     | One turn in a thread, with its tool-call audit.                                                  |
| `llminteraction` | One batch of questions an agent asked the user, waiting in the thread it came from; answering or dismissing it is one reviewed owner transition that resumes the agent. Landed by the `ask` built-in. |

The ten [declarable kinds](vocabulary.md#the-declarable-kinds) — `authority`,
`package`, `kind`, `propertytype`, `trait`, `recordmapping`, `function`,
`agent`, `bundle`, `actor` — live in core too, and so do the three shipped traits:
`temporal`, which puts a record on the timeline, and the `accountconfig` and
`oauth2` interfaces the OAuth facility recognizes.

Back to [the documentation index](README.md).
