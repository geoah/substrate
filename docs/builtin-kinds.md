# Built-in kinds

Every repository is seeded at creation with **`core.substrate.reamde.dev` and nothing
else** — the substrate's own machinery, including the delivery plumbing and the
agent runtime's data. Everything else is a **vocabulary bundle you import**: the
shipped vocabulary (people, tasks, messaging, calendar, media) ships in the
binary and installs on request, exactly the way an
[bundle](bundles.md) does, under the same `bundle:<name>` actor. So a
brand-new repository has no `person` kind until you ask for one.

Importing is one action per bundle, and a bundle that maps ONTO another
authority says so in its `requires:` — installing `google` before `people`
is refused, naming what to import first. Nothing installed ever redefines a
shipped kind. Every declaration is queryable in your own repository
(`substratectl kinds`, or `GET …/core.substrate.reamde.dev/kinds`), descriptions included,
so this page is the map, not the source of truth.

Kinds are named `<authority>/<name>`. The tables below give the name; the
heading gives the authority.

The tables for core are [below](#coresubstratereamdedev); what follows first is the
vocabulary you import.

## people.substrate.reamde.dev — imported

| Kind           | What it is                                                                                                                              |
| -------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `person`       | One human, one record: every edge that means "a person" lands here. What a single source holds about them is its own record, mapped on. |
| `organization` | An org a person belongs to: employer, workspace, publisher.                                                                             |

`person` carries a two-state `prominence` machine: `utility` at birth, `known`
once something promotes it (an address-book sync, or you). Search ranks
`utility` people below every `known` match
([search](graphql-and-search.md#search)).

## messaging.substrate.reamde.dev — imported

| Kind                  | What it is                                                             |
| --------------------- | ---------------------------------------------------------------------- |
| `conversation`        | A DM, a group chat, or a channel.                                      |
| `conversationmessage` | One message in a conversation; outbound ones walk the delivery states. |
| `emailthread`         | One mail thread.                                                       |
| `emailmessage`        | One mail message in a thread.                                          |

## calendar.substrate.reamde.dev — imported

| Kind                  | What it is                                                              |
| --------------------- | ----------------------------------------------------------------------- |
| `calendar`            | One provider calendar.                                                  |
| `calendarevent`       | Always a concrete occurrence; integrations explode series into these.   |
| `calendareventseries` | The recurring definition, RRULE and exceptions, never on the timeline.  |
| `transcript`          | A meeting transcript, pointed at the occurrence that actually happened. |

## tasks.substrate.reamde.dev — imported

| Kind      | What it is              |
| --------- | ----------------------- |
| `task`    | Something to do.        |
| `project` | What tasks group under. |

## media.substrate.reamde.dev — imported

| Kind              | What it is                                                           |
| ----------------- | -------------------------------------------------------------------- |
| `book`            | The work: what an author wrote, independent of any edition you hold. |
| `bookedition`     | One edition: the paperback on the shelf, the epub, the audiobook.    |
| `bookseries`      | An ordered collection a book belongs to, in any format.              |
| `movie`           | One film.                                                            |
| `moviecollection` | A franchise, not a shelf.                                            |
| `musicalbum`      | One album or release: the pressing you have.                         |
| `musictrack`      | One track on an album: the recording itself.                         |
| `podcast`         | One podcast feed.                                                    |
| `podcastepisode`  | One episode; its point on the timeline is the publish date.          |
| `tvseries`        | One television series.                                               |
| `tvseason`        | One season of a series; season 0 is where specials go.               |
| `tvepisode`       | One episode; its point on the timeline is the air date.              |

Media ships the one built-in [record mapping](projection.md), `bookeditionwork`,
which carries an edition's structure onto its work.

## core.substrate.reamde.dev

The substrate's own machinery, declared as kinds so it lists, reads, and
filters exactly like vocabulary. This is the whole of what a fresh repository
speaks:

| Kind                 | What it is                                                                                                                                          |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `repository`         | The repository describing itself from the inside: its id, the owning username, and a lifecycle state.                                               |
| `credential`         | The singleton holding your auth material by reference into the sealed store ([users and tokens](auth.md)).                                         |
| `token`              | One bearer credential: label, optional expiry, coarse last-used, and the hash of its secret.                                                        |
| `actor`              | One declared actor, the name writes are attributed to, and the tier it writes at.                                                                   |
| `agent`              | One declared agent: an LLM-loop callable ([agents](agents.md)).                                                                                    |
| `blob`               | One content-addressed blob: the manifest is the metadata, the bytes live in the byte store; the digest is the id.                                   |
| `recordmerge`        | One performed merge and what it moved ([merges](projection.md#merges)).                                                                            |
| `recordsplit`        | The undo of one merge, likewise performed on creation.                                                                                              |
| `recordmergerequest` | A proposed merge, performed when its decision is accepted.                                                                                          |
| `recordpatchrequest` | A proposed create, patch, or delete, applied when its decision is accepted ([the patch request sibling](projection.md#the-patch-request-sibling)). |

The delivery machinery is core's too, declared as data kinds so a trigger is
console-editable and changelog-visible like anything else
([functions](functions.md#triggers)):

| Kind      | What it is                                                                                                             |
| --------- | ---------------------------------------------------------------------------------------------------------------------- |
| `trigger` | One binding of a source (a record subscription, a schedule, or a webhook) to one callable, owning the delivery cursor. |
| `run`     | One settled trigger delivery attempt, the run ledger's row.                                                            |

So is the agent runtime's data. **Agents are alpha**, so these three are a
preview, unfrozen at v1 and not part of the frozen core:

| Kind         | What it is                                                       |
| ------------ | ---------------------------------------------------------------- |
| `llm`        | One model row: provider, base URL, model, pricing, optional key. |
| `llmthread`  | One agent run's conversation state, written as the loop runs.    |
| `llmmessage` | One turn in a thread, with its tool-call audit.                  |

The nine [declarable kinds](vocabulary.md#the-declarable-kinds) — `authority`,
`kind`, `propertytype`, `trait`, `recordmapping`, `function`, `agent`,
`bundle`, `actor` — live in core too, and so do the four shipped traits:
`temporal`, which puts a record on the timeline, and the `bundleconfig`,
`accountconfig`, and `oauth2` interfaces the OAuth facility recognizes.

Back to [substrate.reamde.dev](../README.md), or reread the [introduction](introduction.md) with the pieces
in place.
