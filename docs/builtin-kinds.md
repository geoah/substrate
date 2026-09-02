# Built-in kinds

Every repository is seeded at creation with **`core.substrate.reamde.dev` and nothing
else** — the substrate's own machinery, including the delivery plumbing and the
agent runtime's data. Everything else is a **vocabulary bundle you import**:
people, tasks, messaging, calendar, and the mneme-ported health, fitness,
routines, journal, places, food and commerce. Each ships in the binary and
installs on request, exactly the way a [bundle](bundles.md) does, under the
same `bundle:<authority>` actor. So a brand-new repository has no `person` kind
until you ask for one.

Importing is one action per bundle, and a bundle that maps onto another
authority says so in its `requires:` — installing `google` before `people`
is refused, naming what to import first. Nothing installed ever redefines a
shipped kind. Every declaration is queryable in your own repository
(`substratectl kinds`, or `GET …/core.substrate.reamde.dev/kind`), descriptions included,
so this page is the map, not the source of truth.

Kinds are named `<authority>/<name>`. The tables below give the name; the
heading gives the authority.

The tables for core are [below](#coresubstratereamdedev); what follows first is the
vocabulary you import.

## people.substrate.reamde.dev — imported

| Kind           | What it is                                                                                                                              |
| -------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `person`       | One human, one record: every pointer that means "a person" lands here. What a single source holds about them is its own record, mapped on. |
| `organization` | An org a person belongs to: employer, workspace, publisher.                                                                             |
| `team`         | A working group finer than an organization: members, leads, and nesting through `parent`.                                               |

`person` carries a two-state `prominence` machine: `utility` at birth, `known`
once something promotes it (an address-book sync, or you). Search ranks
`utility` people below every `known` match
([search](graphql-and-search.md#search)). Its `pronouns` are free text, never
an enum, and empty means unknown: a surface rendering a person without a
value says so and falls back to they/them.

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

| Kind      | What it is                                                                  |
| --------- | ---------------------------------------------------------------------------- |
| `task`    | Something to do, with priority and an optional repeat rule seeded off `dueAt`. |
| `project` | What tasks group under: a name, a lifecycle, a summary.                       |
| `tasklog` | The done-or-skipped mark against one occurrence of a recurring task.          |

Seven further vocabulary bundles are ported from mneme v4. The recurring
kinds share one stance: a schedule stores an RFC 5545 RRULE the substrate
never expands, an occurrence exists only when a log records it, and "missed"
is computed from absence, never stored.

## health.substrate.reamde.dev — imported

| Kind                    | What it is                                                                    |
| ----------------------- | ----------------------------------------------------------------------------- |
| `observation`           | The definition of one tracked measure; with an RRULE it is a habit.           |
| `observationlog`        | One recorded value, typed by its observation's `valueKind`.                   |
| `medication`            | One medication at one strength and form; a new strength is a new record.      |
| `medicationschedule`    | When and how much: a dose, an RRULE, a span. No recurrence means as-needed.   |
| `medicationschedulelog` | One dose, done or skipped; absence in the logs is what missed means.          |
| `bloodtest`             | One blood draw; markers worth tracking are observationlogs pointing at it.    |

## fitness.substrate.reamde.dev — imported

| Kind              | What it is                                                              |
| ----------------- | ------------------------------------------------------------------------ |
| `exercise`        | One movement in the catalog; performances are workoutsets pointing here. |
| `workout`         | One session; with an RRULE it is the recurring plan.                     |
| `workoutset`      | One set: which exercise, how heavy, how many, how it felt.               |
| `workouttemplate` | A reusable session plan built from the catalog.                          |
| `workoutlog`      | The done-or-skipped mark against a recurring workout's occurrence.       |

## routines.substrate.reamde.dev — imported

| Kind         | What it is                                                                  |
| ------------ | ---------------------------------------------------------------------------- |
| `routine`    | The generic recurring obligation, with a window for how literally its time-of-day is meant. |
| `routinelog` | One occurrence, done or skipped.                                             |

## journal.substrate.reamde.dev — imported

| Kind           | What it is                                                              |
| -------------- | ------------------------------------------------------------------------ |
| `journalentry` | One day's reflection; its timeline anchor is the day written about.      |
| `note`         | Anything else written down; `audience` declares who it is for, `status` its handled lifecycle. |

## places.substrate.reamde.dev — imported

| Kind    | What it is                                                          |
| ------- | -------------------------------------------------------------------- |
| `place` | Somewhere worth remembering; what happened there points at it.       |

## food.substrate.reamde.dev — imported

| Kind     | What it is                                                            |
| -------- | ---------------------------------------------------------------------- |
| `recipe` | Instructions for one dish; cooking it is a meal pointing here.         |
| `meal`   | One sitting on the timeline; nutrition numbers are observationlogs whose `derivedFrom` is the meal. |

## commerce.substrate.reamde.dev — imported

| Kind        | What it is                                                            |
| ----------- | ---------------------------------------------------------------------- |
| `order`     | One purchase; the lifecycle stamps `shippedAt` and `deliveredAt` itself. |
| `orderitem` | One line inside an order; the order owns it.                           |
| `currency`  | A property type: an ISO 4217 code, kept separate from the amount.      |

## core.substrate.reamde.dev

The substrate's own machinery, declared as kinds so it lists, reads, and
filters exactly like vocabulary. This is the whole of what a fresh repository
speaks:

| Kind                 | What it is                                                                                                                                          |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `repository`         | The repository describing itself from the inside: its id, the owning username, and a lifecycle state.                                               |
| `credential`         | The one record (id `self`) holding your auth material by reference into the sealed store ([users and tokens](auth.md)).                                         |
| `recoverykey`        | The one record (id `self`) holding the age recipient the user enrolled and the repository's data-encryption key wrapped to it; only the user's age identity opens the wrap.  |
| `token`              | One bearer credential: label, optional expiry, coarse last-used, and the hash of its secret.                                                        |
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
| `run`     | One settled trigger delivery attempt, the run ledger's row.                                                            |

So is the agent runtime's data. **Agents are alpha**, so these four are a
preview, unfrozen at v1 and not part of the frozen core:

| Kind             | What it is                                                                                       |
| ---------------- | ------------------------------------------------------------------------------------------------ |
| `llmprovider`    | One place completions are bought: `wire` (enum), `baseURL`, `apiKey`, `headers` and `pricing` (repeated objects), `defaults` (object). |
| `llmthread`      | One agent run's conversation state, written as the loop runs — its `provider` and `model` included. |
| `llmmessage`     | One turn in a thread, with its tool-call audit.                                                  |
| `llminteraction` | One batch of questions an agent asked the user, waiting in the thread it came from; answering or dismissing it is one reviewed owner transition that resumes the agent. Landed by the `ask` built-in. |

The nine [declarable kinds](vocabulary.md#the-declarable-kinds) — `authority`,
`kind`, `propertytype`, `trait`, `recordmapping`, `function`, `agent`,
`bundle`, `actor` — live in core too, and so do the three shipped traits:
`temporal`, which puts a record on the timeline, and the `accountconfig` and
`oauth2` interfaces the OAuth facility recognizes.

Back to [the README](../README.md), or reread the [introduction](introduction.md) with the pieces
in place.
