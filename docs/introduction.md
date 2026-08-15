# Understanding the substrate

The substrate is a self-hosted datastore for one person's digital life:
messages, mail, calendar events, people, tasks, notes, media. You run it, on
your own machine, against your own Postgres. It is the _system of record_
personal software builds on, instead of every app keeping its own silo.

The problem it answers is fragmentation. Every question that crosses two
apps (_"what did Alex ask me before this meeting?"_) is an integration
project, because nothing shares naming, nothing shares change notification,
and nothing offers a safe way for semi-trusted automation to write.

The design borrows deliberately from Kubernetes, the most battle-tested
answer to "many semi-trusted programs cooperating over shared typed state":

- the substrate is the **API server**: typed records, declared validation,
  an ordered change feed;
- every application and integration is a **controller**: it watches, decides,
  and writes back through the same public API;
- behavior lives in **declarations** (kinds, states, mappings, functions),
  not in bespoke endpoints. The write API is seven generic mutations,
  forever: `put`, `patch`, `delete`, `link`, `unlink`, `merge`, `split`;
- a closure of those declarations installs and uninstalls as one unit, a
  **bundle**, which is how a provider integration or an automation reaches
  the data without a substrate code change.

Kinds ship under the `substrate.reamde.dev` authority domain, and everywhere
in these pages "substrate" means the service you run.

Everything on these pages builds one running example: **a to-do list**.

## The core terms

Like Kubernetes, the whole system is rules about a handful of primitives.

| Term           | What it is                                                                                                                                                   |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **User**       | A human principal: a username, a password, and a TOTP second factor. One hard-coded invite code admits one. A user owns exactly one repository.              |
| **Repository** | Everything a user has: one append-only changelog, the records computed from it, and the blobs and sealed secrets beside them. Nothing crosses from one to another. |
| **Changelog**        | The repository's append-only, strictly sequential list of every change. It is the source of truth: replaying it rebuilds the records ([data model](data-model.md)). |
| **Record**     | One instance of a kind, and the only thing there is. Tasks, people, tokens, and kind declarations are all records. Its identity is the pair `(kind, id)`.    |
| **Kind**       | What a record is: `task` when it is yours alone, `tasks.substrate.reamde.dev/task` when an authority publishes it. A kind declares its properties and its edges.        |
| **Property**   | A named, typed value slot on a record: `title`, `dueAt`. Declared on the kind, validated on every write.                                                     |
| **Edge**       | A named, directed link from one record to another, and the only traversable link between records. Declared on the kind it points from.               |

Two supporting words appear constantly. An **authority** is the DNS name that
publishes a kind and decides who may change its declaration. An **actor** is
_what_ wrote a record — the console, `substratectl`, a function — recorded on every
change as attribution.

## What one substrate holds

One user, one repository, no sharing. A token has full access to the
repository it was minted in, and there is no second repository to reach: no
cross-repository read, no cross-repository watch, no cross-repository search.
The isolation is enforced by Postgres row level security keyed on the
authenticated token's repository, not by discipline in the query layer.

The changelog carries its own tamper evidence: every entry is hash-chained to
the one before it, and a repository can additionally sign every entry with its
own key ([the chain](changelog.md#the-chain)). What that buys, honestly: an
edit, reorder or splice is detectable and named by seq; it is still not
evidence against the operator of the machine it runs on, who holds the
database and the keys alike.

## Reading order

The pages ahead, in reading order:

- **[Getting started](getting-started.md)**: register, log in, and write your
  first record.
- **Core**: the [data model](data-model.md) (the repository, its changelog, records,
  kinds, and the envelope everything reads and writes as),
  [vocabulary as records](vocabulary.md) (the declarable kinds, admission, and how a
  vocabulary evolves without breaking the data underneath it), and
  [projection](projection.md) (how a dozen source records describe one subject).
- **API**: [the API](api.md) (REST, filters, pagination, mutations, errors),
  [users and tokens](auth.md), [GraphQL and search](graphql-and-search.md), and
  [the changelog and watch](changelog.md).
- **Bundles**: [bundles](bundles.md), the installable unit, the [functions and SDK](functions.md) they ship, the
  [agents](agents.md) that run an LLM loop, and the
  [catalog](bundles-catalog.md) of what ships today.
- **Tools**: the [substratectl](substratectl.md) command line, the [web console](console.md),
  and [running a substrate](operations.md) of your own.
- **Reference**: the [built-in kinds](builtin-kinds.md), authority by
  authority.

Next: [getting started](getting-started.md), from an empty substrate to a task
you can read back.
