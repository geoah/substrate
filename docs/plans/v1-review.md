# Proposal for 1.0: architecture, data model and system design

Status: a review of the tree at `f26d42a6b` (v0.123.1) and the changes it
proposes before 1.0, dated 2026-11-11. Nothing here is implemented. Like
every page under `docs/plans/` it is not a contract: the code that lands is.

## Contents

- [Summary](#summary)
- [What the product has to support](#what-the-product-has-to-support)
- [The proposal at a glance](#the-proposal-at-a-glance)
- [How to read this page](#how-to-read-this-page)
- [How the system works today](#how-the-system-works-today)
- [Findings and proposals](#findings-and-proposals), F1 to F14
- [Defects found in passing](#defects-found-in-passing)
- [What stays](#what-stays)
- [Designs](#designs)
  - [Shared repositories](#shared-repositories)
  - [Several repositories under one API](#several-repositories-under-one-api)
  - [What could be cut](#what-could-be-cut)
- [Work and validation](#work-and-validation)
- [Decision records this would replace or amend](#decision-records-this-would-replace-or-amend)
- [The stability policy](#the-stability-policy)
- [References](#references)
- [Appendix: how the sizes were counted](#appendix-how-the-sizes-were-counted)

## Summary

Substrate is a server that keeps a person's data as typed records, lets
apps, scheduled code and AI agents read and write it, and keeps a history
of what changed. This page asks what must be settled before that server is
called 1.0: which choices get expensive to reverse once people have years
of data in it, apps in stores that talk to it, and packages published for
it. Then it proposes the changes.

**Verdict: not ready to be called 1.0, and close.** The model is sound:
typed records, kinds declared in packages, one ordered list of changes per
repository, isolation enforced by the database. What has to change is what
that model promises, and four assumptions built into it.

**The proposal, in eight lines.**

1. **Say what 1.0 promises**, and put the "stable" label where a client
   can rely on it (F1).
2. **History becomes a log that can be compacted.** The latest state of
   every record is always kept; older entries are kept for as long as a
   setting says. This is how etcd works underneath Kubernetes. Today
   history is complete, permanent and the source of truth, which is why
   nothing can be erased and nothing shrinks (F6 to F9).
3. **A repository gets an id, and its name becomes an alias that can
   change.** A repository's own kinds move to a local namespace, so the
   name is no longer written into the data (F2).
4. **A person, a repository and a membership become three things**, so
   several people can use one repository with their own permissions (F3).
5. **One rule for who may write what**, for people, apps, agents and
   package code alike (F5).
6. **The copy of the data outside Postgres is written after a write is
   confirmed**, to a directory or to S3, instead of before it (F9).
7. **A package says what it needs**: which declaration format, and which
   version of the interface its code is written against (F10, F11).
8. **A set of smaller fixes** to the API and to stored values that are
   cheap only before 1.0 (F12, F14).

[The proposal at a glance](#the-proposal-at-a-glance) lists every decision
as a problem, a proposal and a reason, with six questions still open.

## What the product has to support

This is the brief the proposal is held to. It is wider than what the code
does today.

| Capability | Needed | Today | Proposed |
| --- | --- | --- | --- |
| Several people in one repository, each with their own sign-in and permissions. The creator is the owner. Tens to hundreds of people; not enterprise access control | required | one sign-in per repository, with full access | F3 and [Shared repositories](#shared-repositories) |
| Sharing between repositories, including with a person on another server | required, later | not possible | reserved for in 1.0, built after (F4) |
| One API, and one agent, working over several repositories | wanted | one repository per sign-in | after 1.0 ([design](#several-repositories-under-one-api)) |
| Renaming a repository | required | not possible | F2 |
| Deleting that removes the value; history that does not grow without limit | required | a delete keeps the history; nothing is removed | F6 |
| Evidence that history was not altered | not planned for 1.x | damage is detected; alteration is not | F7 |
| More than one server process for one repository | not required | one process | unchanged |

Today's operations guide lists several of these as deliberate absences:
"No sharing, no second user reading your repository, no cross-repository
query. No erasure, compaction, or retention policy … No signatures and no
hash chain" (`docs/operations.md:1963-1969`). That describes the current
code. It is not the product's limit, and the guide should be rewritten with
this proposal.

## The proposal at a glance

Each row is a decision: the problem, what is proposed, and why. **agreed**
means the owner has agreed; **proposed** awaits approval; **open** is one
of the questions below.

### Open questions

| | Question | Recommendation |
| --- | --- | --- |
| Q1 | Does 1.0 itself ship sign-in by person, with one member, or does that come just after? | Ship the shape in 1.0. Sign-in is what apps in stores freeze |
| Q2 | How long is history kept by default? | Everything for kinds a person writes; 30 days for mirrors of outside services; 7 days for bookkeeping |
| Q3 | If Postgres is lost, how much confirmed work may be lost with it, and do writes stop when the copy falls further behind than that? | Aim for under a minute; keep serving and raise an alarm |
| Q4 | Keep the recovery key, or remove it? | Remove it |
| Q5 | Is a server shared by people who do not know its operator supported in 1.x? | No. One household or one trusted group per server |
| Q6 | What size must one repository reach in 1.x? | Validate at 5 million records, 50 GB kept, 100 people connected |

### What 1.0 promises

| | Problem | Proposal | Why | Status |
| --- | --- | --- | --- | --- |
| 1 | Nothing says what 1.0 promises | A written policy: data and the API are stable; package code and outside publishers are experimental | apps in stores cannot be fixed after a break | proposed |
| 2 | "Stable" sits on a hand-written feature list that no client reads, and it has been broken | Labels move onto packages; route groups keep an entry with a number that only goes up; records and sign-in are listed | one place to look for a kind, one for a route | agreed |
| 3 | Only one API version can be served | `/api/v1` only gains things; a later `/api/v2` is served beside it for a stated period; a kind is never served in two shapes | routes are cheap to serve twice; stored data is not | agreed |
| 4 | 56 decision records say "proposed" while their code is shipped | Accept or reject each before 1.0 | they are the design's memory | proposed |

### History

| | Problem | Proposal | Why | Status |
| --- | --- | --- | --- | --- |
| 5 | Complete history is the source of truth, so it only grows, cannot be erased, and every release must replay all of it | A log that can be compacted: the latest state of every record is always kept; older entries per retention | what etcd does under Kubernetes; it resolves four findings at once | agreed |
| 6 | Nothing says how long history is kept | A `retention` setting on each kind, which a repository can override | a journal and a mirror of someone's inbox need different answers | proposed; defaults are Q2 |
| 7 | Deleting leaves the values in history | A permanent delete compacts that record at once; "forget history" does the same for a live record | erasure becomes an ordinary operation | proposed |
| 8 | Every run of a trigger is kept forever | Short retention for bookkeeping | probably most of the bytes; nobody reads them later | proposed |
| 9 | A client may ask for history that is gone | The existing `compacted` answer, tested in every first-party app | the API already has it | proposed |
| 10 | One rule asks history "did a person ever edit this?" | Read the mark already kept on each property | it must not depend on history that may be gone | proposed |
| 11 | Every write waits for a second copy on disk | Postgres alone confirms a write; a background archiver copies the compacted log, files and secrets to a directory or S3 | there is no complete history left to protect, and the failure states go | agreed; the limits are Q3 |
| 12 | History cannot be shown to be unaltered | Not in 1.x. If ever needed: a signed tree over current records, not a chain over history | a chain and compaction do not mix | proposed |
| 13 | The stored format is the way out and the upgrade path, and nothing pins it | Document it; keep a directory from each release and import it under current code; refuse fields it does not know | otherwise the first failure is found by someone restoring a backup | proposed |

### Names

| | Problem | Proposal | Why | Status |
| --- | --- | --- | --- | --- |
| 14 | The name is the storage id, so it can never change | An opaque repository id; storage, keys and webhook addresses bind to it | an id never needs to change | agreed |
| 15 | A repository cannot be renamed | The name becomes an alias that can change; old names keep resolving for a period | typos, getting a domain, moving hosts | agreed |
| 16 | A person's own kinds carry the repository's name | A local namespace: own kinds are `~/tasks/task` | nothing left to rename; two copies of a sample match | agreed |
| 17 | "Authority" means both a permanent publisher and a renameable repository | Three namespaces: the project's, a publisher's (proven by DNS, permanent) and the local one. Publishing is a deliberate step | removes the conflict between the two | agreed |
| 18 | A short name is completed with whatever address the browser used | Complete it under the configured public host; refuse `localhost` and IP addresses outside development | otherwise `ada.localhost` is permanent | agreed |
| 19 | The project's own name is called a placeholder | `substrate.reamde.dev` is permanent; record who holds the domain; the server knows its own packages without DNS | it is in every repository | agreed |
| 20 | A deleted name is free at once | Hold it for 90 days | links and webhooks still point at it | proposed |
| 21 | The catalog field `tier` is a misleading word with a second meaning | One "add" route; a field that says who keeps the package | one word, one meaning | agreed |

### People and access

| | Problem | Proposal | Why | Status |
| --- | --- | --- | --- | --- |
| 22 | A repository is its one person | A person's identity, a repository, and a membership joining them. Today's case is one member | several people is a requirement | proposed |
| 23 | Where a person's identity lives | In a store on the server, outside any repository | hundreds of members should not each need a repository | proposed |
| 24 | Sign-in names a repository, not a person | Sign-in names a person, who then picks a repository | apps in stores freeze the sign-in request | open: Q1 |
| 25 | A person can only have full access | Owner, or a list of read and write grants over kinds, packages or traits, with Editor and Viewer as presets | the form apps already use | proposed |
| 26 | Private data inside a shared repository | None. A repository is the unit of privacy | avoids a list of people on every record | proposed |
| 27 | History cannot say who made a change | The person is recorded on each history line and each change event | a token can be deleted; a person's id cannot | proposed |
| 28 | `owner` already means "any human" in the vocabulary | Rename that stored value to `person` | frees the word for the role | proposed |
| 29 | "Write only the records you created" | Later | no first use needs it | proposed |
| 30 | Who may write what is checked differently for each sort of writer | One list of control kinds, one table of writers, held by a test; an agent is limited by whoever started it | a member is a person with limited access | proposed |
| 31 | A sign-in never expires and cannot be revoked in bulk | Keep it non-expiring; add "sign out everywhere"; cut every open stream with its token | a leaked token should be stoppable | proposed |

### Sharing

| | Problem | Proposal | Why | Status |
| --- | --- | --- | --- | --- |
| 32 | A reference cannot say which repository its target is in | Reserve a `repository` key in the reference value now | a reserved name cannot be added safely later | proposed |
| 33 | Sharing with a person on another server | After 1.0, through a provider package that mirrors records | existing machinery | proposed |
| 34 | Working across several repositories | A client library first, then a server API | the library also works across servers | proposed |
| 35 | An agent across repositories | Reads by default, writes held for approval; an agent may be a member | text in one repository could steer writes in another | proposed |

### Packages and code

| | Problem | Proposal | Why | Status |
| --- | --- | --- | --- | --- |
| 36 | A package's code has no version to build against | An interface version on the package; the Python release is part of it; the input's shape is pinned by a test | authors need something fixed to write to | proposed |
| 37 | A failed delivery is set aside after an eighth of a second | Tell a temporary failure from a permanent one; retry the temporary one over minutes | a provider that is down for a minute should not need a person | proposed |
| 38 | A schedule that missed runs runs every one of them | Each trigger says; the default is the latest only | a week offline should not mean 500 syncs | proposed |
| 39 | A package cannot say which server it needs | It states the declaration format it needs; the server picks the newest version it can read | one new key should not hide a package from older servers | proposed |
| 40 | May a publisher rename a kind? | No: deprecate it and add another | others have records of it | proposed |
| 41 | Code from outside publishers | Experimental at 1.0 | review and confinement are new | proposed |

### Storage and API details

| | Problem | Proposal | Why | Status |
| --- | --- | --- | --- | --- |
| 42 | A property named `body` is stored specially and skips its checks | Make it an ordinary property | no property name should be special | agreed |
| 43 | Record ids sort by the database's language settings | Plain byte order on the identity columns | the main index depends on the host's locale library | proposed |
| 44 | A time zone value accepts `Local` and the empty string | Refuse both | a stored value should mean the same on every host | proposed |
| 45 | Adding a list of allowed values to a property is not checked against existing records | Count it like any other narrowing | records the kind no longer allows cannot be saved again | proposed |
| 46 | Five API details would freeze as mistakes | Fix them (F12) | each is small now and a break later | proposed |
| 47 | One damaged repository stops the whole server from starting | Set it aside and serve the rest | several people will share a server | proposed |
| 48 | The API is tested against a hand-written copy of the engine | Test it against the real engine | the copy does not implement what the tests claim | proposed |
| 49 | Measured-value checks run in every write, in every repository | Make them cost nothing where unused; the feature stays alpha | it is the largest subsystem, and optional | proposed |
| 50 | The assistant, console layouts, model lists and vendor mappings ship inside the core | Name them as outside the promise | they are product content and will change | proposed |
| 51 | Going back to an older server after an upgrade | Not supported; the way back is a copy taken before | every release would have to be reversible | proposed |
| 52 | Offline use | The apps' queue of pending writes is the answer; separate histories are never merged | one order per repository is what everything rests on | proposed |
| 53 | The recovery key cannot be replaced once lost or leaked | Remove it | what it protects can be obtained again | open: Q4 |
| 54 | A server shared by strangers | Not supported in 1.x | it changes the sandbox, naming and start-up rules | open: Q5 |

## How to read this page

- **Citations.** Every `path:line` was opened by the author, except a
  claim marked †, which a delegated reading pass reported and the author
  did not open again. Treat a † as a lead, not a result.
- **Nothing was run:** no database suite, no live test. A test named here
  exists in the tree; nobody watched it pass.
- **A second reviewer**, another model, opened 139 citations of an earlier
  draft and tested its reasoning; what it found is fixed. The proposals on
  history, names and people were written after that check and have had
  only the author's.
- **Sizes** are lines of Go outside `_test.go` files, counted with `wc -l`
  over the files [the appendix](#appendix-how-the-sizes-were-counted)
  names. They say where the code is, not what it is worth.
- **Estimates** are marked as estimates and give their basis.
- **Other systems** are named for comparison and linked under
  [References](#references). They are described from their specifications;
  none was tested against this code.

## How the system works today

The words below are used throughout. `docs/terms.md` is the full list.

- A **repository** is everything one person has on a server. It has a
  name, such as `ada.example.com`, which is also what that person signs in
  with.
- A **record** is one stored thing: a task, a contact, a calendar event.
- A **kind** says what a record is and which properties it may carry. A
  kind's full name has three parts, `<authority>/<package>/<name>`: for
  example `ada.example.com/tasks/task`.
- An **authority** is the DNS-style name of whoever publishes kinds. A
  **package** is a set of kinds published and versioned together. The
  project's own packages live under `substrate.reamde.dev`, such as
  `substrate.reamde.dev/core`.
- A **reference** is a property that points at another record, stored as
  the target's kind and id.
- The **changelog** is the repository's history: an append-only list of
  every change, each with a sequence number (`seq`). It is written to
  Postgres and to files on disk. The **fold** is the table of current
  records, computed from the changelog.
- A **token** is the credential a client sends. A token belongs to one
  repository. An app's token carries **scopes**: the kinds it may read or
  write.
- A **function** is Python code a package ships; the server runs it in a
  sandbox. A **trigger** runs a function or an agent when a record
  changes, on a schedule, or when a webhook arrives.
- The **dialect** is the set of keys a kind or package declaration may
  use. It is closed: a declaration with a key the server does not know is
  refused.

A worked example. Ada registers `ada.example.com` and imports the `tasks`
sample. She now has a kind `ada.example.com/tasks/task`. She adds a task;
its address is `/api/v1/ada.example.com/tasks/task/01JD8…`. The write
appends line 4812 to her changelog and updates the record's row. Her phone
holds a token, was watching the changelog from line 4811, and is told that
the task changed.

What holds the design together, and should be kept:

| Invariant | Where it is enforced | Held by |
| --- | --- | --- |
| One path from a change to the stored record | `internal/engine/fold.go`, `internal/engine/rebuild.go:121-128` | `foldpath_internal_test.go`, `rebuild_db_test.go` |
| Sequence order is the order clients see, so a resumed client skips nothing | a lock taken at transaction start, `internal/engine/dataset.go:784-797` | `TestChangesCursorWalkSkipsNoSeqUnderConcurrentWriters` |
| One writer process per repository | a database lease (`internal/engine/writerlease.go:33-38`) | `writerlease_db_test.go` |
| A repository reads only its own rows | Postgres row level security on every table, `internal/engine/migrations/0001_init.up.sql:551-577` | `isolation_db_test.go` |
| An app's token reaches only what was decided for it | two route groups (`internal/api/api.go:465-466,542-543`) and a wrapper that must spell out every operation (`internal/api/scoped.go:19-40`) | the compiler; API tests against a fake |
| Secret material never enters the changelog | a secret is stored as a pointer into an encrypted side store (`internal/engine/credentials.go:458-466`, `internal/engine/auth.go:8-12`) | `sealing_db_test.go`† |
| An older server refuses data a newer one wrote | migration hashes and two format stamps (`internal/engine/dialect.go:41-45`) | `migrate_db_test.go`† |
| A declaration's key set is closed; a narrowing change is refused while records hold the old shape | `internal/vocabulary/load.go:827-834`, `internal/engine/schemadiff.go` | `dialect_test.go`, `upgrade_guard_db_test.go`† |

What people will come to depend on, and what each is proposed to be at
1.0:

| Surface | Depended on by | Today | Proposed at 1.0 |
| --- | --- | --- | --- |
| Reading and writing records over HTTP, and the error format | every client | served; not on the feature list | **stable**, listed |
| The change feed and watch | apps, console, triggers | labelled `stable` | stable, after F12 |
| Search; aggregates and units | console, apps | `beta`; `alpha` | as labelled |
| Signing in | every client, apps in stores included | outside `/api/v1`, not listed | stable in its new shape (F3, Q1) |
| App authorization (`/oauth/*`, scopes) | third-party apps | `alpha` | beta |
| Webhook URLs | outside services | not listed | stable, addressed by repository id (F2) |
| The declaration dialect | package authors, every stored repository | closed | stable, with the additions in F2, F4, F6 and F11 |
| The interface a function's code is written against | package authors | no version | beta, versioned (F10) |
| Agents, MCP tools, applets | owner, assistants | `alpha` | experimental; the records they write are still durable |
| The history format and the export | every backup, every way out | called storage, free to change (`docs/changelog.md:55-61`) | public and documented (F8) |
| Postgres tables | nobody | internal | internal; say so |
| The Go interfaces in `internal/substrate` | this module only | "what v1 freezes" (`internal/substrate/dataset.go:19-24`) | internal; remove the sentence |
| The CLI | operators, scripts | no statement | `-o json` and `-o yaml` follow the wire; table output is not promised |

## Findings and proposals

Each starts in plain terms, gives an example, then the evidence, then what
is proposed and why. F6 to F9 are one change seen from four sides.

| | In plain words | Proposal |
| --- | --- | --- |
| [F1](#f1-nothing-defines-what-10-promises-and-the-stable-label-has-already-been-broken) | Nothing defines what 1.0 promises, and the "stable" label has already been broken | a written policy; labels on packages |
| [F2](#f2-a-repositorys-name-does-five-jobs-and-can-never-change) | A repository's name does five jobs and can never change | an id, a name that can change, a local namespace |
| [F3](#f3-one-person-per-repository-is-built-into-sign-in-history-and-the-vocabulary) | One person per repository is built into sign-in, history and the vocabulary | identity, repository and membership |
| [F4](#f4-nothing-can-point-from-one-repository-into-another) | Nothing can point from one repository into another | reserve now, build later |
| [F5](#f5-who-may-write-what-is-enforced-differently-for-each-kind-of-writer) | Who may write what is enforced differently for each kind of writer | one list, one table, one test |
| [F6](#f6-complete-history-is-the-source-of-truth-so-nothing-can-be-erased-and-nothing-shrinks) | Complete history is the source of truth, so nothing can be erased and nothing shrinks | a log that can be compacted |
| [F7](#f7-history-cannot-be-shown-to-be-unaltered) | History cannot be shown to be unaltered | not in 1.x |
| [F8](#f8-the-stored-format-is-the-way-out-and-the-upgrade-path-and-nothing-pins-it) | The stored format is the way out and the upgrade path, and nothing pins it | document it and test old data |
| [F9](#f9-every-write-waits-for-a-second-copy-on-disk) | Every write waits for a second copy on disk | copy after the write is confirmed |
| [F10](#f10-code-in-packages-has-no-version-to-build-against) | Code in packages has no version to build against | an interface version |
| [F11](#f11-a-package-cannot-say-which-server-it-needs) | A package cannot say which server it needs | a stated format, and fallback |
| [F12](#f12-api-details-that-would-freeze-as-they-are) | API details that would freeze as they are | fix five |
| [F13](#f13-operating-limits) | Operating limits: one process, one start-up, shared tables | document; fix two |
| [F14](#f14-value-and-storage-details-that-freeze-into-data) | Value and storage details that freeze into data | fix four |

### F1. Nothing defines what 1.0 promises, and the "stable" label has already been broken

**In plain terms.** A client needs to know which parts of the server it
can build on without being broken by the next release. The server answers
that in two ways, and neither works yet.

The first is the address. Every API route starts with `/api/v1`, and the
API guide says: "Within v1 the REST surface is additive only: fields and
endpoints are added, never removed or narrowed" (`docs/api.md:1007`).

The second is a list. The server publishes a small file at
`/.well-known/substrate/server.json`. A shortened example:

```json
{
  "versions": [{"name": "v1", "status": "served"}],
  "server": {"version": "0.123.1"},
  "registration": {"inviteRequired": false, "totpRequired": false},
  "features": [
    {"name": "triggers", "stability": "stable", "surfaces": ["rest"]},
    {"name": "search",   "stability": "beta",   "surfaces": ["rest"]},
    {"name": "agents",   "stability": "alpha",  "surfaces": ["rest"]}
  ]
}
```

The labels are defined in the code. `alpha`: "may change or be withdrawn".
`beta`: "still moving … A break is announced, never silent". `stable`:
"frozen for v1. Changes are additive only"
(`internal/substrate/stability.go:8-12`).

**Who reads the file.** It is needed, for what a client must know before
it holds a token:

- the CLI and the console read `registration`, to know whether to ask for
  an invite code and a second-factor code
  (`cmd/substratectl/commands/client.go:501-526`,
  `web/console/src/lib/api/discovery.ts:28-39`);
- the apps use it to recognise a substrate when someone types an address
  (`apps/kit/Sources/SubstrateAPI/ServerLocator.swift:10-18`), and compare
  `server.version` with the oldest server they support;
- another substrate reads it when checking that a domain's DNS record
  really points at this server (`internal/api/domain.go:43-52`).

Nothing reads the labels. The console prints them on a settings page
(`web/console/src/pages/console-settings.tsx:244-245`), and the only
feature check in the tree is by name, for one optional feature
(`web/console/src/hooks/use-model-keys.ts:65`).

**What is wrong.**

- **The list has no rule for what earns an entry.** It is written by hand
  ("nothing computes the list", `internal/api/discovery.go:189-191`), and
  its fourteen entries are three different sorts of thing:

  | Sort | Entries | Label today |
  | --- | --- | --- |
  | A group of routes | `triggers`, `functions`, `bundles`, `blobs`, `changefeed` | stable |
  | | `export` | beta |
  | | `units`, `mcp`, `apps`, `applets` | alpha |
  | A mode of one route | `search` | beta |
  | | `aggregate`, `embeddings` | alpha |
  | A whole topic: a kind, a package and routes | `agents` (`internal/api/discovery.go:223`) | alpha |

  Reading and writing a record, the error format and signing in are not on
  it. AI providers and models have no entry of their own: they are records
  of the `llm` package and sit under `agents`
  (`internal/substrate/agent.go:17-27`).
- **Stable features have changed incompatibly.** Two examples:
  - v0.121.0 (2026-11-08) changed a value in the catalog's answer from
    `"tier": "provider"` to `"tier": "published"`. A client comparing
    against `provider` stopped matching. The catalog belongs to `bundles`.
  - v0.112.0 made the `source` property of a trigger required, with the
    note "an older binary cannot open the upgraded core". That was a change
    to a *kind*, `substrate.reamde.dev/core/trigger`. No route changed.
- **The label mixes three things.** The address prefix is an API version.
  The feature list is a statement of maturity. And kinds have versions of
  their own, an increasing number per package (`core` is at 54). "Stable
  triggers" does not say which of the three it means.
- **The pace.** 176 releases were tagged in 89 days. `CHANGELOG.md` lists
  84 of them, and 28 carry a BREAKING section.
- **An earlier freeze did not hold.**
  [Decision 0022](../decisions/0022-rest-is-frozen-graphql-is-a-projection.md)
  declared the REST API "the frozen v1 contract" on 2026-08-17 and was
  replaced three weeks later. Its replacement stood for four days before
  [0079](../decisions/0079-graphql-is-removed-and-the-records-read-is-one-route.md)
  removed a whole set of routes.
- **The documents disagree with the code.** The API has fifteen error codes
  (`internal/api/errors.go:25-57`). The guide says "those thirteen strings
  are the whole closed set" (`docs/api.md:2500`). The console's own list
  lacks one real code and carries one the server never sends
  (`web/console/src/lib/api/types.ts:12-27`).
- **A promise with no audience.** A comment says the whole Go interface
  "is what v1 freezes" (`internal/substrate/dataset.go:19-24`). That
  package is under `internal/`, so nothing outside this repository can use
  it.
- **Decisions marked undecided, with their code shipped.** 56 of the 241
  decision records are still `proposed`. Several describe code on `main`:
  0212; 0213 (`internal/catalog/refresh.go:99-103`); 0220
  (`internal/engine/outsidecode.go:31-46`); and
  [0245](../decisions/0245-a-stale-revision-is-a-validation-refusal-narrowed-by-the-reason-stale-revision.md),
  whose `stale_revision` value clients already receive
  (`internal/substrate/errors.go:139`).

**Why it matters.** Someone writes an app against 1.0 and ships it to an
app store. A 1.1 renames a value in a "stable" answer, with a correct
release note. The app breaks, and its users cannot fix it. The project's
own Apple console app is in the same position: it "carries its own copy of
`web/console/dist`" (`apps/README.md:13-17`).

**Proposal.**

1. **A written policy** ([below](#the-stability-policy)): what each label
   promises, for the API, for the dialect, for stored data and for package
   code.
2. **Three things, each named for what it is.**
   - *The API version* is the prefix. `/api/v1` only gains things. When a
     `/api/v2` comes, both are served side by side for a stated period.
   - *Maturity of kinds* is a label on the **package**: `core` stable,
     `llm` alpha. Today that is a comment in a YAML file
     (`kinds/substrate.reamde.dev/core/core.yaml:34-38`) that no client can
     read.
   - *Maturity of routes* stays on the feature list, one entry per group of
     routes that is not about a kind, each with a label and a **revision**:
     a number that only goes up when the group gains something. A client
     asks for "at least 3".
3. **The list is complete.** Records, errors and signing in get entries.
4. **A kind is never served in two shapes at once.** Kubernetes serves a
   resource at several versions and converts between them, which costs a
   pair of conversion functions per change and a rule that nothing may be
   lost in a round trip. Here the server rewrites stored records to the new
   shape at upgrade, and the guide says so: "There is no Kubernetes-style
   multi-version conversion machinery" (`docs/api.md:1014`). An
   incompatible change to a stable kind is made by adding a new kind or
   property beside the old one.
5. **Housekeeping.** Remove the sentence about the Go interface. Hold the
   guide and the console to the code's error codes with a test. Accept or
   reject every `proposed` record whose code is on `main`.
6. **Then go a stated number of releases** without breaking anything
   labelled stable.

**Why revisions and not semver for features.** The server's own version is
already semver, is in the file, and is what apps compare against. For a
feature, semver's three numbers add nothing: inside `v1` every change is an
addition, and a break is a new prefix. Kinds and packages already use one
increasing number.

### F2. A repository's name does five jobs and can never change

**In plain terms.** When someone registers they choose a name. That one
string then does five different jobs, and it is written into so many
places that it cannot be changed afterwards.

| Job | Example, for the name `ada.example.com` |
| --- | --- |
| 1. What you type to sign in | repository `ada.example.com`, plus a password |
| 2. The id in storage | the directory `repositories/ada.example.com/`, the database key, and the label an encryption key is bound to |
| 3. The namespace of your own kinds | your tasks are `ada.example.com/tasks/task`; every task, every reference to a task and every history line carries that string |
| 4. The address others reach you at | webhook URLs are `/webhooks/ada.example.com/<trigger>`; a DNS record under the name proves you own it |
| 5. The name of your code in history | a package's writes are recorded as `bundle:ada.example.com:tasks` |

**The default name is whatever address the browser used.** Most people will
not own a domain, so they type a short name and the server completes it
with the host the request arrived at
(`internal/api/auth_endpoints.go:84-86`,
`internal/vocabulary/naming.go:215-231`). The configured public address is
not consulted. So:

| You type | You reached the server at | Your permanent name |
| --- | --- | --- |
| `ada` | `https://substrate.example.com` | `ada.substrate.example.com` |
| `ada` | `http://localhost:8080` | `ada.localhost` |
| `ada` | `http://127.0.0.1:5173` | `ada.127.0.0.1` |
| `ada.example.com` | anywhere | `ada.example.com` |

A test pins the rule, including `127.0.0.1:5173` becoming `ada.127.0.0.1`,
and accepts `ada.localhost` as a valid name
(`internal/vocabulary/vocabulary_test.go:3615-3631`). Someone who tries the
server with `docker compose up` and registers as `ada` owns `ada.localhost`
for good.

**Why it cannot change.** The name is the database key, held equal to the
name by a constraint
(`internal/engine/migrations/0001_init.up.sql:62-69`). The directory must
carry the same name (`internal/changelogfile/manifest.go:139-148`). The
repository's encryption key is bound to it
(`internal/engine/dek.go:38-44`), and each stored secret is bound to the
kind of the record that owns it (`internal/engine/credentials.go:48-54`).
Importing a sample rewrites every mention of the sample's name to the
repository's, including inside function source
(`internal/vocabulary/rehome.go:9-21`). An app's token stores its scopes
with the name written out (`internal/scope/scope.go:9-11`).

[Decision 0052](../decisions/0052-the-authority-is-the-repository-id.md)
made the name the id and priced it honestly: "the authority is now truly
permanent … a typo at registration is forever". One move has already been
done by hand: 2,380 records of one package were moved between two names
with a script (`docs/plans/authorities/status.md:52-55`).

**The same word means two opposite things.** An *authority* is the name a
publisher's packages live under. Other repositories install from it and
hold records of its kinds, so it must never change: `substrate.reamde.dev`
is in every repository. A *repository's name* is also an authority, and it
has to be able to change. Today nothing separates the two.

**How AT Protocol separates them.** It uses three names with three
lifetimes:

| | Example | Can it change |
| --- | --- | --- |
| Account id | `did:plc:ewvi7nxzyoun6zhxrhs64oiz` | never. Records are addressed by it |
| Handle | `user.example.com` | yes. A DNS record under the handle points to the id, and the id's document claims the handle |
| Record type | `app.bsky.feed.post` | never. It belongs to whoever controls that domain: the app's publisher, not the user |

Its specification warns that an address written with a handle is "not
durable" and may "point to a record in another repo if the handle is
reused". One difference matters: an AT Protocol user never declares record
types under their own handle, so changing a handle touches no data. Here a
person's own kinds are named after their repository.

**Proposal.**

1. **A repository id.** Minted at creation, random, never shown as a name,
   never changed. The directory, the database key and the encryption keys
   bind to it. So do addresses that must outlive a rename: a webhook URL
   becomes `/webhooks/<id>/<trigger>`. Only repositories get one, and
   people once an identity is separate (F3). An authority needs none: its
   DNS name already is its global identifier. A random id is unique without
   a registry, as record ids already are. It carries a short prefix naming
   how it was made, so an id that can be proven from outside could be added
   if moving between hosts ever needs proof.
2. **The name becomes an alias.** It is what people type and see, and it
   can change. An old name keeps resolving, and stays unavailable to anyone
   else, for 90 days. Finding a repository from its name already works
   through DNS in the apps
   (`apps/kit/Sources/SubstrateAPI/ServerLocator.swift:10-18`).
3. **A local namespace for a repository's own kinds.** Own kinds are
   stored and addressed as `~/tasks/task`, meaning "this repository's". The
   name never enters the data, so a rename touches nothing. `~` already
   means exactly this in an app's scopes (`internal/scope/scope.go:111`).
   In an address it reads `/api/v1/~/tasks/task/01JD8…`. The server answers
   with `~`, and accepts the full name, current or former, as another
   spelling.
4. **Three sorts of namespace, each with its own rule.**

   | Namespace | Example | Owned by | Can it change |
   | --- | --- | --- | --- |
   | The project's | `substrate.reamde.dev` | the project | never |
   | A publisher's | `providers.substrate.reamde.dev`; `ada.example.com` once Ada publishes | whoever proves the domain through DNS | never. A publisher that moves leaves a "moved to" pointer |
   | Local | `~` | the repository | it has no global name, so there is nothing to rename |

5. **Publishing is a deliberate step.** "Publish my `recipes` package as
   `ada.example.com/recipes`" needs DNS proof and moves those kinds from
   `~` to that name. From then on others can install it, and it is as
   permanent as any publisher's package. Renaming a repository never
   touches a publisher's namespace.
6. **The default name.** A short name is completed under the server's
   configured public host. `localhost` and IP addresses are refused outside
   development.
7. **The project's name is permanent.** Remove the word "PLACEHOLDER"
   (`kinds/substrate.reamde.dev/authority.yaml:5`), write down who holds
   the domain, and have the server recognise its own packages without
   asking DNS. Today packages it does not ship are fetched from wherever
   that name's DNS points (`internal/catalog/published.go:62-80`).
8. **Existing data** is rewritten to `~` once, by the same full compaction
   that F6 introduces.

**What this changes elsewhere.**

- *Apps.* An app that stored `ada.example.com/tasks/task` keeps working
  while the old name resolves, and should use `~` for a repository's own
  kinds. This is a change to the wire, which is why it belongs before 1.0.
- *Samples.* Importing a sample copies it into `~`. The copy no longer
  contains the repository's name, and two people's copies of `tasks` are
  both `~/tasks/task` from the same origin (F4).
- *Exports.* An export can be imported under a different name.
- *The catalog.* The field that says how a package arrives is called
  `tier`, with values `published` and `sample`
  (`internal/substrate/catalog.go:9-24,69-71`). The console uses it to
  choose between two calls
  (`web/console/src/components/providers/bundle-actions.tsx:98`). "Tier"
  suggests a ranking that does not exist, and the word already means
  something unrelated: who wrote a property's value (`docs/terms.md:50`).
  One "add" route replaces the two calls, and the field says who keeps the
  package: its publisher, or you.
- *Three accepted decisions bend.* 0098 and 0101 say a kind is always
  written in full; `~` becomes the one short form. 0052 and 0074 are
  replaced.

**Why.** A typo, a person who starts on `localhost` and later gets a
domain, a person who leaves one host for another, and a household
repository named after one person are all ordinary. An id plus a name that
can change handles the first three jobs. The local namespace handles the
other two without rewriting anything, and it is the only approach that
does.

### F3. One person per repository is built into sign-in, history and the vocabulary

**In plain terms.** The product needs several people in one repository,
each with their own sign-in and permissions. Today a repository *is* its
one person. That assumption is in the sign-in request, in every history
line, in the meaning of the word "owner", and in several kinds of data.
None of it is hard to change today. Most of it becomes hard at 1.0, because
it is exactly what apps in stores and published packages will have built
on.

**Example.** Ada wants her partner Bo to see and edit the household's
recipes and shopping lists, and not her journal. Today she has one option:
give Bo her password. There is no second sign-in, no way to say "recipes
yes, journal no", and afterwards history cannot tell which of them changed
what.

**Where the assumption lives.**

| Where | What it says today | Becomes expensive at 1.0 because |
| --- | --- | --- |
| The sign-in request | `{"repository": "ada.example.com", "password": "…"}` (`internal/substrate/auth.go:233-238`). The name of the repository is the name of the person | apps in stores ship with this request |
| The credential | one per repository, stored as a record with the fixed id `self` (`internal/engine/auth.go:5-12,52`) | it is stored data; a migration, not a contract |
| History | a line records `actor`, which program wrote, and `principal`, which token (`internal/changelogfile/entry.go:41-60`). A token can be deleted, and a token record names no person (`kinds/substrate.reamde.dev/core/token.yaml`). No line says who | the line's key set is closed (`internal/changelogfile/entry.go:179-180`): a new key is a format change every older server refuses |
| Permissions for a person | none. Only an app's token can be limited, and a person cannot be handed a limited one: a token minted by hand may carry `["mcp"]` and nothing else (`internal/api/auth_endpoints.go:66-69,372`) | the app and its scopes are the only model clients know |
| The word `owner` | it already means "any human hand", as opposed to installed code (`internal/substrate/tier.go:15-19`). Package authors write `writer: owner` on a property to mean that (`internal/vocabulary/types.go:416-430`) | the dialect is a stable surface. Once "owner" also means the person who created the repository, `writer: owner` reads two ways |
| Data that is really one person's | console preferences are "Repository-owned" (`kinds/substrate.reamde.dev/core/consolepreference.yaml:8-9`); an assistant's memories are facts "the owner keeps" (`kinds/substrate.reamde.dev/llm/memory.yaml:15-18`); one setting names "the owner's own" contact (`kinds/substrate.reamde.dev/core/contact.yaml:32-33`) | with two people these are shared by accident |
| Limits | four chat turns at once per repository (`internal/engine/agents.go:376-379`); no limit per person | internal; free to change |
| Clients | the Chat app asks for full access (`apps/chat/ChatKit/Sources/ChatKit/ChatManifest.swift:35`); an app holds one repository per connection (`docs/terms.md:170`) | apps in stores |

**What is not a problem.** The history itself. It is one ordered list
written by one server process, and several people are simply several
clients of that process.
[Decision 0017](../decisions/0017-the-changelog-is-one-writers-total-order.md)
rules out something different: several servers each writing their own
history and merging them.

**Proposal.** The model in [Shared repositories](#shared-repositories): a
person's identity, a repository, and a membership that joins them. Today's
design is its special case, one person and one repository. For 1.0:

1. **Sign-in names a person.** The person then picks a repository they are
   a member of. Whether 1.0 ships this shape with one member, or labels
   sign-in beta and ships it just after, is Q1; the recommendation is to
   ship it.
2. **The person is recorded** on each history line and each change event,
   left out when the repository has one member.
3. **The stored value `owner` becomes `person`**, in the record of who
   wrote a property and in the dialect's `writer:`. "Owner" is then free to
   mean the role.
4. **F5 is fixed first.** A member is a principal with less than full
   access, and today that is enforced in one layer only.

**Why.** Several people in one repository is a requirement, and the three
things that are costly to change after 1.0 (the sign-in request, the
history line, a word in the dialect) are each small to change before it.

### F4. Nothing can point from one repository into another

**In plain terms.** Sharing between repositories needs three things the
data model cannot express: a reference that says which repository its
target is in, a way to tell that two people's "tasks" are the same sort of
thing, and a way to grant less than a whole collection.

**Examples.**

- *A reference cannot say whose.* A reference is stored as the target's
  kind and id: `{"ref": "substrate.reamde.dev/core/contact/01JD8…"}`. That
  kind is built in, so every repository has contacts of it. Nothing in the
  value says the contact is in Bo's repository and not Ada's. The value has
  two reserved names, `ref` and `target`
  (`internal/vocabulary/ref.go:31-43`), and no third for a repository.
- *Two copies are two kinds.* Ada and Bo each import the `tasks` sample.
  Import copies it under each one's own name
  (`internal/vocabulary/rehome.go:9-21`), so Ada has
  `ada.example.com/tasks/task` and Bo has `bo.example.com/tasks/task`. To
  the server these are unrelated kinds.
- *A record does not say where it came from.* The record on the wire has no
  repository field (`internal/substrate/record.go:12-78`). Ids that a
  provider composes can also collide: one calendar event synced into two
  repositories has the same kind and id in both.
- *A grant covers a kind, not a record.* A scope is `read:` or `write:`
  over a kind, a package or a trait (`internal/scope/scope.go:6-17`). "Share
  this one list" cannot be said.
- *A person cannot easily publish.* A repository cannot install a package
  from its own name (`internal/catalog/published.go:134-140`), and
  publishing means hosting a document by hand
  (`cmd/substratectl/commands/publish.go:30`).

**What is internal and can wait.** Each database connection is pinned to
one repository (`internal/engine/scope.go:171-181`), so a read across two
is two reads merged, never a join. Each repository has its own sequence, so
a merged feed needs a position per repository. Neither is a contract.

**Proposal.**

1. **Reserve a `repository` key in the reference value now**, unused, as
   `target` was. A reserved name added after 1.0 can collide with a
   property a package already declared.
2. **Copies match** once own kinds are local (F2): both are `~/tasks/task`,
   and the import already records which sample a copy came from. A kind
   that two repositories should share by name is published.
3. **A client ignores response fields it does not know** (F12), so a
   repository can be added to a record later.
4. **Build nothing else yet.** The nearest route to sharing is several
   people in one repository (F3). Across servers it is an existing
   mechanism ([below](#sharing-with-someone-on-another-server)).

**Why.** The two things that would be locked at 1.0 are the reference's
shape and the kind names data accumulates under. Both are settled by a
reservation and by F2.

### F5. Who may write what is enforced differently for each kind of writer

**In plain terms.** Four sorts of writer reach a repository: a person with
full access, an app with limited access, an AI agent, and a package's
function. Each is checked in a different place, and they do not protect
the same things. Two kinds of record are especially sensitive, because
writing one changes what code runs: a **trigger** (when code runs) and a
**policy** (which agent writes need approval).

**Example.** The policy code says "bundle-tier actors cannot write the
policy kind at all: a policy an agent could edit is a gate that agent could
open" (`internal/engine/policy.go:23-26`). That refusal exists for agents
(`internal/engine/agentwrite.go:185-188`). A function's writes go through a
different check that only asks whether the kind is in the function's
declared list (`internal/engine/effects.go:134,255-261`). So a function
whose list includes the policy kind, or all of `core`, can write a policy,
or a trigger that runs any installed code. The only defence today is the
person reading that list when they install the package. This was found by
reading; no test exercises it.

**Evidence.**

- The one built-in exception for functions is four kinds that hold sign-in
  material (`internal/vocabulary/function.go:344-361,385-390`). Triggers
  and policies are not on the engine's protected list either
  (`internal/engine/dataset.go:60-69`).
- The rule that a webhook shipped by a package needs a key looks only for
  writers named `bundle:` (`internal/engine/triggers.go:697-765`). A
  function is named `function:`.
- An app may talk to an agent when the app's read scopes cover everything
  the agent reads (`internal/api/scopedagents.go:663-670`). Nothing there
  limits what the agent then writes to what the app may write.
- A full-access token can mint another one with no password
  (`internal/api/api.go:340-345`). Changing the password signs nothing out
  (`internal/engine/auth.go:645-672`). A sign-in token never expires
  ([0143](../decisions/0143-a-token-has-full-access-to-its-repository-and-a-login-token-does-not-expire.md)).
  The auth guide says the opposite, that a leaked token cannot outlive its
  revocation (`docs/auth.md:308-311`); the security policy admits the mint
  (`SECURITY.md:72-77`).
- Only a limited token's open stream is cut when the token is revoked
  (`internal/api/auth.go:144-170`). A full token's stream runs on.
- A full token may claim any writer name that is not reserved
  (`internal/api/auth.go:180-190`), and several decisions read that name†.
  The vocabulary's own definition, "Attribution, never authorization"
  (`docs/terms.md:51`), describes the intent, not the code.

**Proposal.**

1. One list of control kinds: triggers, policies, AI provider settings,
   package settings and secrets, applets, declarations, memberships.
2. One table saying what each sort of writer may do to each, held by a
   test that fails when a writer or a kind is added without a row.
3. An agent that a caller starts is limited by the caller's access as well
   as its own.
4. The writer-name header takes a fixed form.
5. "Sign out everywhere", offered when a password changes. Every stream is
   cut when its token dies. Sign-in tokens stay non-expiring.
6. `docs/auth.md` is corrected.

**Why.** With members (F3) a person can have limited access. If limits are
enforced in one layer and not the others, a viewer can ask an agent to
write for them.

### F6. Complete history is the source of truth, so nothing can be erased and nothing shrinks

**In plain terms.** Today every change is kept forever, and the current
records are *defined* as "what replaying all of history gives". That one
choice has four consequences. A deleted value stays in history. History
only grows. Every future release must be able to replay every old entry.
And every write has to wait for a second copy of history on disk. F7, F8
and F9 are the other three sides of it.

**Example.** A person syncs three years of mail and health data. They
delete one message and choose "delete permanently". The message is gone
from their lists. It is still in the history files, in the history table,
in every export and backup, and the change feed will still serve its text
to anyone who asks for old values. If someone else hosts their server, the
host cannot remove that one message for them, short of deleting the whole
repository.

**"The changelog" is three things.**

1. **An order.** Every change gets a sequence number. Clients, triggers and
   the apps' local copies resume from it.
2. **The write path.** A write is applied by appending an entry and
   updating the record from that entry.
3. **Complete history as the source of truth.** Every entry is kept, and
   the current records are whatever replaying them gives.

The first two are needed and stay. The third is what changes.

**What actually uses old history.** 21 engine files query the history
table, most of them for the newest position or for recent entries.

| Use | What it needs |
| --- | --- |
| Watching and resuming; the apps' local copies | recent entries only. An event names the record and the client fetches it |
| Triggers on record changes | entries until each trigger has handled them. A function is handed the record's current state, not the old one (`internal/runner/protocol.go:223-235`) |
| Before and after values in the activity view | old values. This is the one user-facing feature that reads them, and it already has an answer for "not known" (`internal/engine/changevalues.go:22-28`) |
| "Has a person ever edited this shipped record?" | every past writer of a record (`internal/engine/triggers.go:720-741` is one such read) |
| Rebuild, restore and export | all of it, by design |
| Undo | nothing. There is no undo or "restore this version" today |

**What complete history costs.**

- A permanent delete removes the record's current row and its index rows,
  and nothing from history (`internal/engine/rows.go:738-767`). Two things
  are removed: the record's secrets (`internal/engine/rows.go:728-736`),
  and a file once no record points at it (`internal/engine/gc.go:50-53`).
- Each write is stored three times: the history line on disk, the same
  line in a database table
  (`internal/engine/migrations/0001_init.up.sql:257-280`), and the current
  record. Nothing is ever dropped: the oldest position a client may resume
  from is fixed at zero (`internal/api/watch.go:42-48`). One history
  reached 27 GB eight weeks after the first release
  (`internal/engine/segmentdigest.go:15-18`).
- Bookkeeping is kept forever too. Every time a trigger runs, a run record
  and a delivery entry are written to history
  (`internal/engine/functions.go:38-46`). The current state keeps the last
  twenty runs per trigger
  (`internal/engine/functions.go:57-62,2594-2607`); history keeps all of
  them.
- Renaming one property writes a history entry for every record that has
  it, 10,000 at most in one step
  (`internal/engine/convert.go:104-107`).
- A kind has nowhere to say how long its history should be kept. The keys a
  kind may declare are a closed set with no such key
  (`internal/vocabulary/load.go:827-834`).

**What it buys that nothing else does.** If a bug corrupts the current
records, replaying history with fixed code repairs them. And the record of
what agents and syncs did is complete.

**Proposal: a log that can be compacted.**

The latest state of every record is always kept. Older entries are kept
for as long as a retention setting says. This is how etcd works underneath
Kubernetes: it keeps every revision until told to compact up to one. Its
guide says: "The entire available event history can be watched starting
from the last compaction revision." A client behind that point is told so.
Kubernetes asks it to compact every five minutes. Here the setting is per
kind instead of one for everything.

How it works:

1. **Retention.** A kind declares how long entries about its records are
   kept: everything, a length of time, or nothing. A repository can
   override it. Proposed defaults (Q2): everything for kinds a person
   writes, 30 days for mirrors of outside services, 7 days for bookkeeping
   such as run records.
2. **Compaction.** A background pass replaces a record's entries that are
   past their retention with one **state entry**: the record's whole state
   as of the newest entry it replaces. Sequence numbers are kept, so older
   history has gaps, and nothing is ever renumbered. Decision 0017 already
   requires that, because positions are stored inside data.
3. **A permanent delete compacts that record at once.** Its values leave
   the server's history when it is deleted. "Forget history" does the same
   for a record that stays.
4. **What a client sees.** Nothing new. An event says "this record
   changed" and the client fetches it, so several old changes arriving as
   one is harmless. Deletions are remembered for a period. A client that
   resumes from before that period is told `compacted`, which the API
   already has, and lists again. The oldest such position is already
   published in the discovery file.
5. **What a trigger sees.** A trigger that starts from the beginning sees
   each record once, in its latest state, then everything kept since.
6. **Before and after.** Shown within retention. Older than that, the view
   says the earlier value is not kept, which it can already say.
7. **Repair.** Replaying still works, from the compacted log, through the
   same code. It repairs anything computed from records. It can no longer
   recompute a state from before the compaction.
8. **Formats.** A new release can ask for a full compaction at upgrade,
   which rewrites everything in the current format. Old entries no longer
   have to be readable forever (F8).

What it needs:

- A state entry for each table that is not computed from records: records
  themselves, former ids, annotations, who wrote each property, and the
  delivery tables (`internal/engine/delivery.go:65`). Search, references
  and the measured-value index are computed from records and need none.
- Replay that takes a record's version and its times from a state entry.
  Today it recounts the version and ignores the stored one
  (`internal/engine/fold.go:200-210`).
- A `retention` key on a kind.
- The rule "a person ever edited this" read from the mark already kept per
  property (`internal/engine/migrations/0001_init.up.sql:210-220`) instead
  of from history.
- A compaction pass that is paced and gives way to real work. The
  background check of old files had to learn the same lesson
  (`internal/engine/segmentdigest.go:15-24`).
- Each first-party app tested against `compacted`.

What it gives up: a complete record of every change, for the kinds whose
retention is shorter than "everything"; and repair of a state older than
the last compaction.

One thing no design reaches: a copy already taken. A backup or an export
made before a delete still holds the value. Encrypting each record under
its own key and destroying the key is the only approach that does. It was
considered and left out: every value would be encrypted at rest, and search
still needs the plain text.

**Keys.** The recovery key is written once, at registration
(`internal/engine/auth.go:455`). The key that encrypts secrets is created
once and never replaced (`internal/engine/engine.go:1638`). A lost
recovery key cannot be replaced. A leaked one opens every copy ever taken:
enrolling a new one would not help, because the old copy of the key stays
readable and opens the same secrets (`internal/engine/rewrap.go:3-10`).
The recommendation is to remove the recovery key (Q4, and
[What could be cut](#what-could-be-cut)). If it stays, it needs both a way
to enrol a new one and a way to replace the encryption key.

**Why.** It keeps the order and the single write path, which are the
valuable parts and are built and tested. It turns "how much history" from a
fact of the architecture into a setting. And it is what erasure, bounded
growth, rename (F2) and format changes (F8) each need. Keeping no history
at all, with only a sequence number on each record, was considered: it is
the simplest to explain, and it is a new write path and gives up repair.

**To measure first:** bytes of history by kind on the largest repository.
It decides the default retentions.

### F7. History cannot be shown to be unaltered

**In plain terms.** Each history line carries a checksum of itself. That
catches a damaged disk. It does not catch someone changing a line and its
checksum together, and nothing links one line to the next or signs
anything.

**What exists, and what was removed.** The checksum is the SHA-256 of the
line (`internal/changelogfile/entry.go:113-133`); "it does not chain to the
previous entry and nothing signs it" (`docs/terms.md:45`). An earlier
design had both a chain and signatures. It was removed in
[decision 0050](../decisions/0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md)
for stated reasons: about 2,100 lines of code, a host key needed just to
write, and a pile of machinery to keep the chain valid whenever history was
rewritten.
[0018](../decisions/0018-reseal-rewrites-history-values-in-place.md)
blanked values in place and had to re-compute the chain from that point
on;
[0011](../decisions/0011-chain-transitions-are-repository-scoped-epochs.md)
added a table to record each such break. 0050 also says when to look
again: "if the substrate ever serves a repository whose owner does not
trust the host".

**Who would want proof.**

| Question | Arises when | What would answer it |
| --- | --- | --- |
| Has the operator, or anyone with access to the disk, changed my data? | someone else hosts you | proof the operator cannot forge |
| Did Bo really make this change? | several people share a repository | for a household, the server recording the person (F3) is enough. Real proof needs each person to hold a signing key, which [0010](../decisions/0010-signing-is-per-repository-ed25519-one-way.md) rejected to keep "no user-managed keys" |
| Is this record, received from another server, what its owner wrote? | sharing across servers | a direct, encrypted connection to that server is enough |

**Proposal.** Not in 1.x. Only the first question lacks a cheaper answer,
and it matters once strangers are hosted, which 1.x does not plan (Q5).

If it is ever needed, the right form is a signed tree over the *current*
records, which is what AT Protocol does: each repository is a tree of its
records by content hash, and each new version of the tree is signed. A
chain over history is the wrong form here, because compaction (F6) rewrites
history, and a chain forbids exactly that. Nothing has to be reserved for
it now: a tree over current records sits beside the history, not inside
it.

One thing is worth doing regardless: write the checksum's canonical form
down. Today strings are escaped by "encoding/json's one policy"
(`internal/changelogfile/entry.go:247-251`), which is recorded nowhere
else.
[0009](../decisions/0009-the-chain-hashes-what-postgres-stored.md)
considered the standard form, RFC 8785, and rejected it because that
standard writes numbers as floating point and this store keeps them exact.
That reasoning still holds, so the answer is a written specification with
test vectors (F8), not a switch.

**Why.** It defers a real cost until there is someone to protect, and F6
removes the only thing that would have made it hard to add.

### F8. The stored format is the way out and the upgrade path, and nothing pins it

**In plain terms.** The README promises "you can download everything, as
one file". That file is the repository's history and its files. The same
entries are what a newer server reads when it upgrades. So their format is
the most durable promise the project makes. It is documented as internal,
and nothing tests that an entry written by an older release still reads
correctly.

**Example.** In two years, a 1.4 server imports a directory that a 1.0
server exported. Today the only evidence that works is history that the
1.4 server wrote for itself in a test.

**Evidence.**

- The guide puts the stored effects outside the contract: "a change to how
  the fold spells an effect is a storage change and not an API change"
  (`docs/changelog.md:55-61`).
- No directory from any release is kept in the tree. The test meant to
  cover old data,
  `TestEveryMigrationLeavesAFoldTheRebuildReproduces`
  (`internal/engine/migrationrefold_db_test.go:113`), writes its history
  with the current code.
- A history line refuses a key it does not know
  (`internal/changelogfile/entry.go:179-180`). The effects inside the line
  do not: unknown fields are dropped silently
  (`internal/engine/fold.go:1380-1384`). If a release forgot to raise the
  format stamp, an older server would lose data without an error.
- Going back to an older server stops being possible once a newer one
  stamps the data or changes the database
  (`docs/operations.md:1253-1261`). Limits already exist: a database from
  before v0.70.0 cannot be upgraded in place (`CHANGELOG.md:1436-1452`).
- Code that fixes up old data runs again on every import
  (`internal/engine/repomigrate.go:36-39`), so each such fix must be kept
  for as long as old directories are accepted.
- The upgrade path has a defect no test catches. The checks that guard a
  kind being moved are added to a list at
  `internal/engine/shippedupgrade.go:182`, and the list is replaced at line
  189.

**Proposal.**

1. **Document the format as the export's contract**: the line's keys, the
   canonical form with test vectors, every effect including F6's state
   entries, and the directory layout.
2. **Keep a directory from each release** in the tree, small, covering
   every sort of change. Import each one under the current code and compare
   the records it produces.
3. **Refuse unknown fields inside a line**, as the line itself already
   does.
4. **Say how old a directory may be.** A 1.x server reads what any earlier
   1.x wrote. A release that changes the format compacts fully at upgrade
   (F6), so a fix-up for old data is carried for one major version and no
   longer.
5. **Restore the moved-kind checks.**

**Why.** Without a pinned format, the first incompatibility is found by
someone restoring a backup.

### F9. Every write waits for a second copy on disk

**In plain terms.** Every write goes to two places before the client gets
an answer: Postgres, and a history file on disk. The point is that the
directory alone, with no database, can restore a repository. Postgres
decides whether a write happened and answers every read; the files are a
second copy kept exactly in step. Keeping them exactly in step is what
costs.

**How it works.** The lines are written and flushed without their final
newline; Postgres commits; then the newline is written
(`internal/engine/dataset.go:853-1013`,
[0062](../decisions/0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md)).
The files are opened only by the start-up check, a rebuild, a verify, a
snapshot, a key change and the export. No read of a record or of the change
feed touches them.

**Why it was built.** The operations guide "promises that nothing in the
database is needed to restore a repository, so an acknowledged write could
be absent from every backup" (0062, line 19).

**What it costs.**

- Two flushes to disk on every write, one writer at a time.
- A failure state only this design has: the files fall behind the
  database, the server answers "unavailable, retry" for a write that in
  fact succeeded, and refuses writes until it is restarted
  (`internal/engine/dataset.go:876-885`).
- A start-up check over every repository that stops the whole server if
  one directory is damaged (`internal/engine/repodir.go:256-262`).
- A background check of old files that had to be slowed down. Unthrottled,
  a 27 GB history took 2.3 processor cores for ten minutes and made a
  scheduled sync miss its deadline
  (`internal/engine/segmentdigest.go:15-24`).
- A server tied to a local disk.
- 8,460 lines of code and 11,841 lines of tests.

**What it buys that nothing else does.** If Postgres loses a write it had
confirmed, the directory still has it. That protects *complete* history,
which F6 no longer keeps.

**Proposal: copy after the write is confirmed.**

Postgres alone confirms a write. A background **archiver** copies the
repository to a store with four operations (put, get, list, delete): the
compacted log in pieces, the files, and the secrets, still encrypted. The
store is a directory by default and may be S3-compatible. A new base
replaces older pieces after each compaction.

- **Import and export stay.** A server that finds a directory with no
  matching rows imports it, as today. The export is the same pieces as one
  file.
- **The archive is on by default.** An operator who backs up Postgres may
  turn it off.
- **What is at risk.** If Postgres is lost, what had not been archived is
  lost with it. The target and what happens when it is missed are Q3: the
  recommendation is under a minute, and to keep serving and raise an alarm
  instead of refusing writes.
- **What it must have before it replaces today's design:** a durable mark
  of the last position archived; files and secrets written before the
  piece of history that names them; the delay measured and reported.

Object storage in general cannot append to a file; S3 can in one storage
class only. So today's protocol could not be moved to S3 as it is. What
simplifies things is writing the copy after the commit, and S3 then
becomes one possible destination. Postgres ships its own log this way, and
Litestream does it for SQLite.

**What would go (an estimate, not a list of cuts).** The code for the
synchronous protocol, the stopped-server snapshot and the background check:
in `internal/changelogfile`, half of `writer.go`, and `lock.go`,
`unread.go` and `snapshot.go`; in `internal/engine`, `snapshot.go`,
`segmentdigest.go`, the commit path in `dataset.go` and the last 360 lines
of `repodir.go`. About 2,000 lines, perhaps 3,000, and about 3,500 lines
of tests ([the appendix](#appendix-how-the-sizes-were-counted) names
them). An S3 destination adds code back:
[0075](../decisions/0075-v1-ships-the-fs-blob-backend-alone.md) removed one
that "cost 1,039 lines", including a 172-line hand-written request signer.
A second attempt should use a library.

**Why.** The synchronous copy exists to make sure no confirmed change is
ever missing from history. Once history is compacted that guarantee has
nothing to guard, and what remains is a backup, which may lag. Removing it
also removes the failure state, the start-up dependency and the tie to a
local disk.

### F10. Code in packages has no version to build against

**In plain terms.** A package can ship Python functions. The server runs
each one, handing it an input and an object, `host`, through which it reads
records and returns its writes. That pair is what package authors write
against. It has no version number, the Python version is fixed only inside
the project's own container image, and one function with a memory leak can
take the server down.

**Example.** The image moves from one Python release to the next. A
function in someone's package imported a module the new release dropped.
It stops loading, and every delivery to it is set aside as failed. The
package had no way to say which Python it needed, and its owner finds out
from a failed row.

**Evidence.**

- A function names its `runtime` as `python` or `host`, with no version
  (`internal/vocabulary/function.go:34-62`). The interpreter is pinned only
  in the image, `python3~3.14` (`Dockerfile:190-195`). The protocol between
  the server and the function process has a version that "nothing at
  runtime compares" (`internal/runner/protocol.go:77-83`). That is fine for
  the protocol, since both ends ship together; the author-facing interface
  has none.
- A package header has no key for a minimum server or interface version
  (`internal/vocabulary/bundle.go:275-279`).
- By default the sandbox is "best effort": if the host cannot confine a
  function, it runs anyway (`internal/sandbox/sandbox.go:92-102`). Off
  Linux nothing confines it (`internal/sandbox/sandbox_other.go:10-21`).
  That applies to code the person wrote and code the server ships. Code
  from an outside publisher is already refused unless the host can confine
  it fully (`internal/engine/outsidecode.go:31-46`,
  `internal/sandbox/sandbox.go:484-486`).
- No function has a memory or process limit. One that hits the container's
  limit "takes the substrate down with it" (`docs/functions.md:563-568`).
- Every function in a package receives the live access token of every
  account the package has connected
  (`internal/engine/invocationconfig.go:141-178`).
- A failed delivery is retried twice, 25 ms and then 100 ms later, and
  then set aside (`internal/engine/functions.go:57,67`) until a person
  retries it†.
- A schedule that missed runs, because the server was off or the trigger
  was disabled, runs every one it missed
  (`internal/engine/schedulecursor.go:62-66`).
- A function is given a stable key per delivery
  (`internal/engine/functions.go:3063-3072`), so not repeating an outside
  action after a retry is the function's own job.
  [0125](../decisions/0125-a-provider-write-is-a-callable-that-writes-no-record.md)
  records the consequence: a retried provider write is sent twice.

**Proposal.**

1. **An interface version on the package**, 1 by default, refused when the
   server does not know it.
2. **The Python release is part of that interface**, with a stated support
   window: a release of the server supports the current Python and the one
   before.
3. **The shape of the input is pinned by a test**, as the wire is.
4. **`functions` is labelled beta** until 1 to 3 are done.
5. **Retries.** A temporary failure (the network, a provider answering
   "try later", a timeout) is retried over minutes with growing gaps. A
   permanent one is set aside at once, as today.
6. **Missed schedules.** Each trigger says what to do with runs it missed:
   all of them, or only the latest. The default becomes the latest.
7. **The sandbox.** The server refuses to start in "best effort" mode where
   more than one person can register.
8. **Code from outside publishers is experimental at 1.0.**

**Why.** Package authors need something fixed to write against, and three
of today's behaviours (the eighth-of-a-second retry, replaying every missed
run, running unconfined) are surprising enough that freezing them would be
a mistake.

### F11. A package cannot say which server it needs

**In plain terms.** The dialect is closed: a declaration that uses a key
the server does not know is refused. That is deliberate, and it means a
new key is an event for every server. But a package has no way to state
which dialect it was written for, and when a publisher lists several
versions the server picks the newest and gives up if it cannot read it.

**Example.** A publisher has users on 1.0 and on 1.3. They adopt a key that
1.3 added. Every 1.0 server now reports the package as not published,
including the ones that installed it last month, because the newest version
no longer parses and nothing falls back to the version before.

**Evidence.**
[Decision 0020](../decisions/0020-dialect-keys-are-reserved-not-tolerated.md)
calls a new key "an ecosystem event" because "bundles are shared across
servers". Yet "no dialect is on the wire"
(`internal/engine/dialect.go:41-45`), and the resolver takes the newest
listed version and fails if it does not parse
(`internal/catalog/published.go:94-112`). Separately, only the server's own
packages can rename or move a kind: the door other packages use stores the
request and refuses it (`internal/engine/vocabularywrite.go:1156-1159`).

**Proposal.**

1. A package states the dialect it needs, in its header and in each
   version a publisher lists.
2. The server publishes its dialect in the discovery file.
3. The server installs the newest version it can read.
4. A publisher may not rename a kind: it deprecates the kind and adds
   another. A kind's name is permanent because other repositories hold
   records of it.

Not verified: a change to one package is said not to be checked against
other packages that depend on it, and removing a package is said to have no
check for dependents†. If so, the dependent package stops loading and its
records become unreadable until it is fixed. Worth one test.

**Why.** One new key in one release should not make a package disappear
from every older server.

### F12. API details that would freeze as they are

| # | In plain terms | Evidence | Proposal |
| --- | --- | --- | --- |
| 1 | A write can carry a guard, "only if the record is still at version 3". DELETE takes it in the address (`?ifVersion=3`). PUT and PATCH take it in the body, and silently ignore it in the address, so `PUT …?ifVersion=3` writes with no guard | `internal/api/rest.go:122-167` against `174-195`; the stated rule is "never silence" (`internal/api/rest.go:218-220`) | refuse the parameter by name, as reads already do |
| 2 | An error can follow a write that succeeded: if Postgres confirms and the answer is lost on the way, the client gets a server error† | `internal/engine/dataset.go:979-1001`; [0165](../decisions/0165-a-commit-in-doubt-is-caught-up-by-the-next-write-and-no-write-grant-names-the-owners-auth-kinds.md) | mark it so a client can tell. Accept a retry key on PUT, PATCH and DELETE, which take none today (`internal/api/core.go:32-38`) |
| 3 | "Start listing again" has two different errors: 410 when history was replaced, 422 when the ordering changed underneath, which installing a package or upgrading the server can cause† | `internal/engine/query.go:616-628` | one error, and a version inside the paging token |
| 4 | The server refuses any request field it does not know. That is good for catching mistakes, and it means a newer app's new field fails on an older server | `internal/api/core.go`†, `internal/strictjson`† | keep it. State the two rules that follow: a client ignores response fields it does not know, and checks the feature list before sending a new request field |
| 5 | Signing in is outside `/api/v1` and not on the feature list | the route table† | list it, in the shape F3 gives it |
| 6 | After a permanent delete, a new record at the same id starts again at version 1, so an old guard for version 1 passes | [0072](../decisions/0072-reserve-a-purged-record-id-against-reuse.md) | no change: providers reuse ids, as 0072 says. Document it, and have clients compare creation time |
| 7 | A client cannot write several records as one all-or-nothing step | `docs/terms.md:174` | later; it can be added without breaking anything |
| 8 | No time limit on a request and no rate limit on the API | `cmd/substrated/listen.go:54-61`; limits only on sign-in† | a limit per person, with F3 |

What a client may conclude today:

| After | It may conclude |
| --- | --- |
| A timeout on a PUT at an id it chose, with no guard | Retry. A write that changes nothing adds nothing to history |
| A timeout on a guarded write | Nothing. A retry's "conflict" may be its own first attempt. Read the record and compare |
| A timeout on a create, a function call, a merge or a split, sent with a retry key | Retry with the same key within 24 hours (`internal/engine/idempotency.go:89`) |
| A server error on a write | It may have succeeded |
| A dropped watch | Resume from the saved position: ordered, nothing skipped, possibly a repeat. A 410 means list again. If an operator restores the database from a dump without one extra step, the client resumes silently wrong (`internal/engine/historygeneration.go:8-15`) |

### F13. Operating limits

- **One server process per database.** A second one is refused
  ([0083](../decisions/0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md)),
  and the guide says to keep to one (`docs/operations.md:1100-1103`). So
  there is no standby, and a new version cannot be started beside the old
  one: every upgrade is a short outage†. This stays for 1.x. F9 removes the
  tie to a local disk, which is what a second process would need first.
- **One damaged repository stops the whole server from starting.** The
  start-up check returns at the first repository that fails
  (`internal/engine/repodir.go:256-262`), on purpose. *Proposed:* set the
  damaged one aside and serve the rest. F9 removes most of the check.
- **Writes to one repository happen one at a time**, from the moment each
  starts (`internal/engine/dataset.go:784-797`). A large sync page holds
  that turn until it finishes. A person's write goes ahead of queued
  background writes, but "priority never shortens the transaction already
  holding the lock" (`internal/engine/writegate.go:34-41`). How many writes
  a second this allows is not measured. The one write benchmark is for
  measured values, one at a time
  (`internal/engine/observed_bench_db_test.go:136`,
  `docs/plans/observations/benchmark.md`).
- **Shared tables.** Every repository's records are in one table. Each kind
  adds indexes to it (`internal/engine/indices.go:395-401`). I found
  nothing that removes an index when its kind or repository goes: the only
  removals are of scratch and replaced indexes
  (`internal/engine/indices.go:141,224,240`). One vector index serves every
  repository
  (`internal/engine/migrations/0015_embeddings_vec_index.up.sql:76-77`).
- **Search assumes English** (`internal/engine/rows.go:210-212`), and
  vectors have one fixed size
  ([0026](../decisions/0026-embedding-vectors-are-1536-wide-or-refused.md)).
  Both are computed from the records and can be rebuilt, so both can change
  later.

Where one repository with many people bends first:

1. **Watching.** After a write, every connected client is signalled, in
   batches about 300 ms apart (`internal/engine/dataset.go:1178-1207`), and
   each then runs its own database read (`internal/api/watch.go:204-206`).
   A repository may use at most eight of the server's sixteen connections
   (`internal/engine/scope.go:95-105`). *Proposed:* one reader per
   repository, in memory, feeding all its watchers, as Kubernetes does. The
   API does not change.
2. **Checking tokens.** Every request looks its token up in the database
   (`internal/engine/auth.go:1203-1222`). I found no cache. *Proposed:* a
   cache; the server sees every token change.
3. **The write turn.** People write slowly; one person's large sync is what
   the others feel. *Proposed:* "a person first", not "the owner first".
4. **Per-repository limits** become per person: four chat turns at once
   (`internal/engine/agents.go:376-379`), and a rate limit (F12).

None of this is a public promise, which is the point. The tables are
internal, so serving thousands of repositories is rework behind
`internal/substrate`, not a break.

**What belongs in the core.** The `core` package names vendors: nine
contact mappings for Apple, Google, Slack, GitHub, Linear and Beeper
(`kinds/substrate.reamde.dev/core/contactmappings.yaml`). The built-in
assistant, its prompts, the list of AI model prices and the console's
layouts are product content. *Proposed:* name them as outside the stability
promise, keeping the one rule that protects people: a shipped record is
never refreshed over an edit a person made.

### F14. Value and storage details that freeze into data

| In plain terms | Evidence | Why it is expensive later | Proposal |
| --- | --- | --- | --- |
| The columns holding a record's kind and id sort by the database's language settings. Another table in the same database already forces plain byte order, because the default "orders case and punctuation differently from Go" | `internal/engine/migrations/0001_init.up.sql:86-106`; `internal/engine/migrations/0013_observed_index.up.sql:16-20` | the main index and every paged listing depend on the host's locale library, and Postgres warns that an index built under one version of it can be wrong under the next | plain byte order on those columns |
| A property named `body` is stored in its own column; no other long text is | `internal/engine/fold.go:131-132`; [0041](../decisions/0041-body-is-a-declarable-property-and-fts-is-per-property.md) | see below | an ordinary property |
| A time zone value accepts `Local` and the empty string, and depends on the zone data installed on the host | `internal/engine/validate.go:559-562` | a stored value means something else on another host | refuse both |
| Adding a list of allowed values to an existing property is not treated as a narrowing; only removing values is | `internal/engine/schemadiff.go:544` | records the kind no longer allows are still served, and cannot be saved again | count it |
| A date and time is stored in UTC and the offset it was written with is thrown away. There is no type for "8:00 wherever I am", a time of day, or a date with no year | `internal/engine/validate.go:526-531` | the offset cannot be recovered | no change for 1.0. New types can be added later |
| Writing `null` always removes a property, and also lets a sync fill it back in. "I deliberately have no value here" cannot be stored | `internal/engine/write.go:1062-1068,1375-1386` | a value that reappears after a sync looks like a bug | no change for 1.0 |
| `unique` on a property is accepted and does nothing. The guide says so ("Nothing enforces it yet"), and the code says how it would be switched on | `docs/vocabulary.md:853-857`; `internal/vocabulary/types.go:252-257` | little | no change |
| A change to a kind that must rewrite records is limited to 10,000 records by default, in one step | `internal/engine/convert.go:104-107`; `docs/operations.md:176` | a rename shipped for a large built-in kind is refused at every start | raise it, or do it in steps, before a built-in kind grows that large |

**The `title` and `body` columns.** A record's row has a `title` column and
a `body` column beside the one that holds its other properties. Both are
also separate fields in every history entry
(`internal/engine/fold.go:131-137`), so both are part of the stored format.

*`title` stays.* Every list row, every link and every search result needs
a label for a record without loading its kind. The title is the top search
band (`internal/engine/validate.go:1264-1293`), it names a linked record
(`internal/engine/linkedfrom.go:64`), and lists sort by it
(`internal/engine/query.go:985`). Nearly every shipped kind builds its
title from a template: 179 templates for 180 kinds. Two things to settle.
The template is applied in one place, when a record is written
(`internal/engine/validate.go:1255`), and the result is stored, so changing
a kind's template changes a record's title only when that record is next
written. And
[0016](../decisions/0016-a-kind-titles-itself-from-a-declared-property.md)
left kinds without a template accepting a title typed in directly, which is
the inconsistency it set out to end.

*`body` becomes an ordinary property.* Today it is a storage optimization
that applies to a property only if it happens to be named `body`. Nine
properties are: six in GitHub's package, one each in Linear's and Google's,
one in a clinical kind. The shipped kinds declare 106 long-text properties.
0041 kept the column because long text "reads and orders without
unpacking" the other properties, which is true of every long text and not
of one name. And 0041 records the price: a `body` property skips the normal
checks, so "`required`, `pattern` and `min` on a `body` property are not
enforced".

What the change involves: the value moves in with a record's other
properties, by one migration; the special case in search goes
(`internal/engine/validate.go:1264-1293`); sorting and filtering by `body`
go through the ordinary path (`internal/engine/query.go:985`); the checks
start applying; the nine kinds do not change. Whether long text in general
should be stored apart is an internal choice that can be made later, once
no name is special.

The three time columns (`at`, `endsAt`, `dueAt`) stay: date-range queries
need them. They share the separate write path, with one quirk worth
closing: a time column is cut to microseconds
(`internal/engine/write.go:548-550`) where an ordinary date keeps what it
was given.

## Defects found in passing

Not design findings. Each was found by reading; none was reproduced.

| Where | What |
| --- | --- |
| `internal/engine/shippedupgrade.go:182,189` | The start-up upgrade's checks on a moved kind are added to a list and the list is then replaced, so they never refuse. |
| `internal/api/rest.go:122-167` | F12 row 1. |
| `internal/engine/rows.go:758-759`, `internal/engine/rebuild.go:80-82` | A rebuild replays each permanent delete, which removes that id's search vectors. Nothing in the replay queues new ones (a live write does, `internal/engine/write.go:1414`; the catch-up runs only on import, `internal/engine/repodir.go:812`). A record deleted and created again loses its vectors after a rebuild, where the comment says they are kept. |
| `docs/api.md:2500`, `web/console/src/lib/api/types.ts:12-27`, `internal/api/errors.go:22-24` | Three statements of the list of error codes, none equal to the fifteen in the code. |
| `internal/engine/policy.go:23-26` | A rule stated for all installed code and enforced for agents only (F5). |
| `docs/auth.md:308-311` | Says a leaked token cannot outlive its revocation (F5). |
| `kinds/substrate.reamde.dev/authority.yaml:5` | Calls the project's permanent name a placeholder (F2). |
| Decisions 0050, 0052, 0062 | Each names tests, four in all, that no longer exist. |
| `internal/api/appsetup.go:323-326,359,434-456` | A route apps may call checks for package updates before it applies the app's own limits. An app with any access can make the server fetch a publisher's document and take an update of a package the app has nothing to do with. |

## What stays

The four core designs, and what the proposal leaves alone in each.

- **The changelog.** The order and the single write path stay: every
  change gets a sequence number, and a record is updated from the entry
  that describes the change. Watching, resuming, triggers and the apps'
  local copies stand on that. What changes is that history is no longer
  complete, and no longer has a synchronous copy on disk (F6, F9).
- **The API.** A record's address is its reference; one route lists
  records and refuses what a mode does not support; a client resumes from
  a position and is told where to start again when that position is gone.
  All of that stays. The changes are at the margins (F12), in how own
  kinds are addressed (F2) and in sign-in (F3).
- **Kinds and properties.** One stored form for a reference; defaults
  filled in when a record is written; exact numbers; a closed dialect;
  changes to a kind carried out as ordinary writes behind a confirmation;
  names that never come back once retired. All of that stays.
- **The database.** It is private, which is its best property, and row
  level security is a stronger boundary than care taken in each query.
  Nothing outside may come to depend on the tables.

## Designs

Nothing in this section is built. Each part says what exists and what it
would take.

### Shared repositories

Several people in one repository; the creator is the owner; each other
person has permissions of their own.

#### Three nouns

Today one word does three jobs. Split them:

- **Identity**: a person. A name, a password, an optional second factor. It
  holds no data.
- **Repository**: the data. It has an id and a name (F2).
- **Membership**: a record inside a repository saying that an identity may
  act there, as an owner or with listed grants.

Today's design is the special case: one identity, one repository, and an
owner membership nobody wrote down. So no data has to move: each existing
repository gains one owner.

#### Permissions

- **Owner.** Everything: members, packages and kinds, connected accounts
  and secrets, export, deletion. The creator is the first owner. A
  repository may have several and always has one.
- **Member.** A list of grants in the form apps already use: `read:` or
  `write:` over one kind, a package, a publisher or a trait
  (`internal/scope/scope.go:6-17`). Two presets: Editor is `write:*`,
  Viewer is `read:*`. A wildcard never reaches sign-in material
  ([0080](../decisions/0080-a-kind-grant-may-glob-and-a-glob-never-reaches-auth-material.md)),
  and should not reach F5's control kinds either.
- **Not offered:** a list of people on each record, groups, deny rules,
  inheritance. The rule that replaces them: *a repository is the unit of
  privacy, and a kind is the unit of permission.* Something private from
  the other members belongs in another repository.
- **Later:** "write only the records you created". It needs each record to
  know its creator, which the person on each history line makes possible.
- **Grants are read on each request** from the membership, not copied into
  the token, so a change or a removal takes effect at once.
- **Delegation.** An agent or a function that a member starts runs with
  what both of them may do, never more. Code that runs on its own, from a
  trigger, keeps its own grant as today.

#### Where identity lives

In a store on the server, outside any repository.

- Hundreds of members should not each need a full repository.
- The credential stops being a record of one repository. Today it is one,
  holding two pointers into that repository's encrypted side store
  (`internal/engine/auth.go:5-12`), so changing it writes to the
  repository's history. That is why an operator resetting a password must
  stop the server (`docs/operations.md:1934-1936`). This gain is
  structural, not a matter of security: the password hash was never in
  history.
- The sign-in name stops being the repository's name
  ([0074](../decisions/0074-the-repository-name-is-the-login-name.md)).

The cost: an identity is no longer inside a repository's backup, so the
archive (F9) carries the identity store as well. The alternative, each
person's identity being a personal repository, keeps one concept and costs
a repository per member.

Identity names keep today's DNS-style form, so that another server could
vouch for one later: the person signs in at their own server, which asserts
who they are. IndieAuth and OpenID Connect are the precedents, not a fit as
they stand: IndieAuth names a person by a profile URL, and OpenID Connect
by an issuer and a subject. Two pieces exist here: the app authorization
flow
([0194](../decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)),
and a DNS record under a name that says which server it is on
(`internal/domains/record.go:2-4`). Finding a person's server, checking the
name it asserts and the trust between two servers are all still to design.

#### What exists, and what is missing

| Needed | Today |
| --- | --- |
| A view of a repository limited to a grant | exists, for apps: `internal/api/scoped.go:19-40` |
| A token that names a person | missing (F3) |
| A grant given to a person | missing (F3) |
| Clients that work with less than full access | missing: the Chat app asks for everything, and limited tokens cannot plan a change to kinds, merge records or use the catalog (`internal/api/api.go:542-591`) |
| Delegation limited by the caller | missing (F5) |
| One meaning for `owner` | missing (F3) |

#### What history needs

1. **The person on each line** and on each change event (F3).
2. **Memberships and grants as records,** kept with full retention, so a
   change of permission is in history and in the export.

#### Two people editing at once

No new mechanism is needed, and one policy is. What exists is a familiar
shape: the server decides the order, a write changes only the properties it
names, and a guard can be placed on the record's version
(`internal/engine/write.go:3189-3204`). Two cases follow.

- *Without a guard:* two people who change different properties both
  succeed. Two who change the same property leave the later value, and
  nobody is told.
- *With a guard:* the second writer is refused even when the properties
  differ, because the version belongs to the whole record.

What is left to choose is what a client does when refused: read again and
re-apply, which the apps already queue a refused write for, or ask the
person.

| System | What it shares with this design |
| --- | --- |
| Kubernetes list and watch | a version, a watch from it, and a 410 that says list again |
| CouchDB | a sequence per database, a feed of changes from a position, a revision guard on a write |
| JMAP | a state per type of data, a call for changes since it, and accounts a user may be given access to, each marked read-only or not |
| Replicache | the server decides the order; the client keeps a queue of pending writes and replays it over what the server returns. This is the apps' outbox |

Automerge and Yjs merge simultaneous edits without a server choosing the
order. That is what several independent writers, or two people editing one
text live, would need. Neither is a goal, so with one process ordering
every change neither is required.

#### Sharing with someone on another server

After 1.0. The cheapest form uses what exists: the other person's substrate
is treated as a provider, like Google or Slack. Bo connects an account to
Ada's repository, holding a limited token, and her records are mirrored
into his by the same machinery that mirrors a calendar. Besides the package
it needs: a way to grant a person (F3); grants narrower than a kind, if she
shares less than a whole collection, which is the expensive part because
the filter must hold in every read; and the same kind on both sides, which
means she publishes it (F2).

### Several repositories under one API

After 1.0. Once a person belongs to several repositories, clients and
agents need to work across them.

#### What "across repositories" can mean

- **Reads are merged, never joined** (F4).
- **Each repository has its own order.** A merged stream needs a position
  per repository.
- **A write lands in one repository.** Two histories cannot be written as
  one step, and that should never be promised.
- **A record's identity becomes repository, kind and id** (F4).
- **Local kinds match by name** once F2 is in: `~/tasks/task` in each.
- **Search ranks within one repository**†, so a merged search needs a rule
  for merging.

#### A library, then a server API

| | Client library | Server API |
| --- | --- | --- |
| Credential | a token per repository | one sign-in |
| Change to the server | none | a new surface |
| Reach | any server | one server |
| Cost | N requests and N streams; the logic is written three times, in Swift, TypeScript and Go, unless specified once | it is the one place repositories meet, so the one place data can leak between them |
| Waits on | nothing | identities and memberships |

Both, in that order. The library is the only form that crosses servers. It
keeps a local copy and a position per repository. The CLI already holds
several named connections, one at a time
(`cmd/substratectl/commands/config.go:20-33`).

For the server API, copy JMAP's shape: one sign-in lists the accounts a
user may use, each marked personal or read-only; every call names one
account; one stream carries a state per account. Matrix is the precedent
for one stream, resumed with one token, over many rooms. Three additions,
in order:

1. **A session read:** the repositories this identity may use, with its
   grants in each.
2. **A way to choose a repository:** the sign-in is exchanged for a token
   per repository, in the manner of OAuth token exchange, which keeps
   today's rule that a token implies its repository.
3. **Merged list, search and watch only,** with the repository on every
   record and a position per repository. Everything else stays with one
   repository.

#### An agent across repositories

Three routes, cheapest first:

- **An outside assistant over MCP: today.** The MCP door calls the API with
  the caller's own token (`internal/api/mcp.go:196-228`). Connect once per
  repository and the assistant has both.
- **A `substrate` provider package.** Its account holds a limited token for
  the other repository, and its functions are offered to agents as tools
  ([0198](../decisions/0198-a-bundle-offers-its-functions-and-an-agent-takes-them-by-pattern.md)).
- **A built-in agent**, such as the assistant in a personal repository
  reading the household one. It holds one repository today
  (`internal/engine/agentloop.go:665-666`). It would need tools that take a
  repository; each repository opened as the person who asked, under what
  both the person and the agent may do; the conversation kept in the home
  repository; and a rule for runs no person started: an agent may be a
  member in its own right.

The risk that grows is this. Text read from one repository can instruct an
agent that may write to another, so a planted instruction becomes a path
between two people's data. An agent across repositories reads by default,
and its writes are held for approval.

### What could be cut

The engine is 94,476 lines.

| Area | Lines | Label | Proposal |
| --- | --- | --- | --- |
| Measured values, aggregates, units | 19,812 | alpha | The largest subsystem: a time-series engine with a unit algebra. Make it cost nothing where unused |
| Functions, triggers, sandbox | 17,319 | stable today | Needed. Short retention for what it writes (F6) |
| Agents and AI model adapters | 16,428 | alpha | A second product. Outside the promise |
| Packaging: catalog, publishers, app set-up, sample copying | 10,552 | mixed | One "add" route; copies stop being renamed (F2) |
| The on-disk copy of history | 8,460 | — | F9 |
| Changing kinds while data exists | 7,786 | — | Keep: this is what keeps data durable |
| Limited tokens for apps | 5,259 | alpha | Keep: shared repositories ride on it |
| Mapping and merging records | 4,515 | — | Keep; 14 mappings ship |
| Search and vectors | 4,495 | beta; alpha | Computed from records; free to change |
| Applets | 3,536 | alpha | A screen runtime in the server, days old. Outside the promise |
| MCP | 2,311 | alpha | Gives an outside assistant "everything the console does but the auth material" (`internal/scope/scope.go:37-42`) |
| The stand-in engine used by API tests | 1,900 | — | Replace |

Two alpha subsystems, measured values and agents, are 30% of the engine
(14,722 and 13,923 of 94,476 lines).

1. **The synchronous half of the on-disk copy** (F9).
2. **The stand-in engine.** The API is tested against a hand-written copy
   of the engine's 72 operations, and the tree says what that proves: "The
   fake's Put ignores IfVersion … so 'the engine returns sentinel X' and
   'the API maps sentinel X to status Y' can both pass while the engine
   returns a different sentinel for the request that matters"
   (`internal/testenv/conformance_db_test.go:5-10`). Test the API against
   the real engine.
3. **Measured values, out of every write.** Four of its checks run at the
   end of every write (`internal/engine/dataset.go:801-818`), whether or
   not the repository holds a measurement, and every repository starts with
   216 units and 149 conversions (`AGENTS.md`). Not a deletion: make it
   cost nothing where unused, and keep it alpha.
4. **The renaming of copied samples** (F2).
5. **The recovery key** (Q4). Records are stored unencrypted: "the
   changelog carries every record in the clear"
   (`internal/changelogfile/fsutil.go:8-12`). The encrypted side store
   holds connected-account tokens and package secrets, and with F3 no
   longer the password. Each can be obtained again by reconnecting the
   account. What the recovery key adds is moving hosts without doing that.
   Its costs are a step at registration, a key that cannot be replaced, and
   with several owners, a key each.

## Work and validation

The work the proposal implies, by area. A plan would order it.

| Area | Work | From |
| --- | --- | --- |
| Promises | The stability policy; labels on packages; route groups with revisions; records, errors and sign-in listed; the guide and console held to the code's error codes; every `proposed` record resolved; the operations guide's list of absences rewritten | F1 |
| History | State entries for records, former ids, annotations, property writers and the delivery tables; replay that honours stored versions and times; the `retention` key; the compaction pass; permanent delete and "forget history" compacting at once; the shipped-record rule moved off history; apps tested against `compacted` | F6 |
| The copy | The archiver, with a directory and an S3 destination; the mark of the last position archived; delay reported; the synchronous protocol, the stopped-server snapshot and the background check removed; the identity store included | F9 |
| Format | The format documented with test vectors; a directory per release, imported in a test; unknown fields refused; the moved-kind checks restored | F8 |
| Names | The repository id; storage, keys and webhook addresses bound to it; names as aliases with a hold period; the local namespace on the wire and in storage; one compaction that rewrites existing data; publishing as a step; one "add" route and the renamed catalog field; default-name rule | F2 |
| People | The identity store; memberships; sign-in by person; the person on history lines and events; `owner` renamed to `person`; grants for members | F3 |
| Access | Control kinds and the writer table with its test; delegation limited by the caller; the writer-name form; sign out everywhere; streams cut with their token; `docs/auth.md` | F5 |
| Sharing | The `repository` key reserved in a reference | F4 |
| Packages | Interface version; Python window; pinned input; retries over minutes; missed-schedule setting; dialect stated by a package and fallback to a readable version | F10, F11 |
| API | F12 rows 1 to 5; a limit per person | F12 |
| Operation | A damaged repository set aside at start-up; one reader per repository for watchers; a token cache | F13 |
| Values | Byte order on identity columns; `body` ordinary; time zone check; added value lists counted | F14 |
| Tests | The API tested against the real engine | What could be cut |

Validation before the release:

1. A directory from each release, imported under the current code.
2. Compaction: a repository compacted and replayed gives the same records
   as before; a permanent delete leaves no value in the log.
3. Each first-party app recovering when told history is `compacted`.
4. A start-up with one damaged repository among several: the others serve.
5. The server killed at each step of a write and of an archive pass.
6. The table of writers against control kinds.
7. A list of every route as public, owner-only or limited, where a new
   route fails until it is placed†.
8. Retry keys, guarded PATCH and refused paging tokens, over HTTP against
   the real engine.
9. A rename: every address, app token and webhook still works under the
   old name and the new.
10. Rebuild, import, compaction and archive timed at the sizes Q6 settles.
11. A stated run of releases with no break to anything labelled stable.

## Decision records this would replace or amend

| Record | What changes |
| --- | --- |
| [0017](../decisions/0017-the-changelog-is-one-writers-total-order.md) | Stands: one order, one writer. History is no longer complete |
| [0050](../decisions/0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md) | Replaced: Postgres holds the log; the files are an archive; a finished piece may be replaced |
| [0062](../decisions/0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md) | Replaced: the copy is written after the commit |
| [0064](../decisions/0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md) | Amended: positions stay in the log; run records get short retention |
| [0052](../decisions/0052-the-authority-is-the-repository-id.md) | Replaced: a repository has an id of its own |
| [0074](../decisions/0074-the-repository-name-is-the-login-name.md) | Replaced: a person signs in; a repository's name is an alias |
| [0042](../decisions/0042-every-kind-carries-an-authority.md), [0098](../decisions/0098-a-declaration-names-a-kind-or-trait-in-full.md), [0101](../decisions/0101-a-kind-trait-or-callable-is-named-in-full-on-every-surface.md) | Amended: a kind is named in full, except that `~` is the one short form |
| [0048](../decisions/0048-providers-are-published-samples-are-copied.md), [0070](../decisions/0070-a-copy-is-upgraded-through-its-origin-stamp-and-requires-pins-a-floor.md) | Amended: a sample is copied into the local namespace, not under the repository's name |
| [0041](../decisions/0041-body-is-a-declarable-property-and-fts-is-per-property.md) | Amended: `body` loses its column |
| [0143](../decisions/0143-a-token-has-full-access-to-its-repository-and-a-login-token-does-not-expire.md) | Amended: a member's token is limited; sign out everywhere |
| [0191](../decisions/0191-a-shipped-record-is-held-by-any-hand-but-the-one-that-ships-it.md) | Amended: whether a person edited a shipped record is read from the property mark, not from history |
| [0051](../decisions/0051-a-repository-directory-is-the-backup-unit.md), [0065](../decisions/0065-a-snapshot-is-a-stopped-server-copy-that-records-its-head.md), [0069](../decisions/0069-the-owner-export-is-the-snapshot-streamed-as-a-tar.md) | Replaced: the snapshot and the export become the archive and its format |

## The stability policy

The labels and the notice before a removal are modelled on the Kubernetes
deprecation policy; what a major version may break follows semantic
versioning.

1. **Three labels.** *Stable*: only additions within 1.x; removed only in
   2.0. *Beta*: supported; a break is announced one minor release ahead, in
   the discovery file and in the changelog. *Experimental*: may change or
   go in any release.
2. **Where a label lives.** On a package, for its kinds. On a route group,
   with a revision that only goes up. The API version is the address
   prefix.
3. **Data outranks labels.** The latest state of every record, an
   experimental package's included, survives every upgrade or is carried
   forward by a conversion. A feature may be withdrawn; current data may
   not be lost. History is kept as each kind's retention says.
4. **What "only additions" means, per surface.**
   - The API: an answer may gain fields, and a client ignores fields it
     does not know. A new request field comes with a higher revision that a
     client checks first, because the server refuses what it does not know.
   - The dialect: a new key raises the dialect number; a package states the
     number it needs.
   - The stored format: a new line key or effect raises the format stamp. A
     1.x server reads what any earlier 1.x wrote, and 2.0 reads what the
     last 1.x wrote.
   - The function interface: an interface version, and the current Python
     release and the one before.
5. **Upgrading.** A 1.x server opens any database and archive written by
   the same or an earlier 1.x release, in one step. It refuses one written
   by a later release, as today. Database changes only go forward.
6. **Going back.** Not supported once a newer server has stamped the data
   or changed the database. The way back is a copy taken before. Each
   release says whether it closes the way back.
7. **Deprecation.** Announced in the discovery file and the changelog. For
   a stable feature it is notice ahead of 2.0, never permission to remove
   it within 1.x. A beta feature is served for at least two further minor
   releases after the notice.
8. **Outside the promise, by name.** The database tables, the Go packages,
   the CLI's table output, the built-in assistant, console layouts, model
   lists, vendor mappings and applets.
9. **Nothing on `main` is `proposed`.** A decision record is accepted, or
   the code is not there.

## References

### Decision records cited

| Record | Cited for |
| --- | --- |
| [0009](../decisions/0009-the-chain-hashes-what-postgres-stored.md) | why the standard canonical form was rejected (F7) |
| [0010](../decisions/0010-signing-is-per-repository-ed25519-one-way.md) | signing, and no keys held by users (F7) |
| [0011](../decisions/0011-chain-transitions-are-repository-scoped-epochs.md) | what a rewritten chain cost (F7) |
| [0016](../decisions/0016-a-kind-titles-itself-from-a-declared-property.md) | the `title` column (F14) |
| [0017](../decisions/0017-the-changelog-is-one-writers-total-order.md) | one ordered history; positions stored in data (F3, F6) |
| [0018](../decisions/0018-reseal-rewrites-history-values-in-place.md) | blanking values under a chain (F7) |
| [0020](../decisions/0020-dialect-keys-are-reserved-not-tolerated.md) | a new key is an event for every server (F11) |
| [0022](../decisions/0022-rest-is-frozen-graphql-is-a-projection.md) | an earlier "frozen v1" (F1) |
| [0026](../decisions/0026-embedding-vectors-are-1536-wide-or-refused.md) | vector size (F13) |
| [0041](../decisions/0041-body-is-a-declarable-property-and-fts-is-per-property.md) | the `body` column (F14) |
| [0050](../decisions/0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md) | history files; why the chain was removed (F7) |
| [0052](../decisions/0052-the-authority-is-the-repository-id.md) | the name as the id (F2) |
| [0062](../decisions/0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md) | the synchronous copy (F9) |
| [0064](../decisions/0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md) | bookkeeping in history |
| [0072](../decisions/0072-reserve-a-purged-record-id-against-reuse.md) | reuse of an id (F12) |
| [0074](../decisions/0074-the-repository-name-is-the-login-name.md) | the sign-in name |
| [0075](../decisions/0075-v1-ships-the-fs-blob-backend-alone.md) | the removed S3 store (F9) |
| [0079](../decisions/0079-graphql-is-removed-and-the-records-read-is-one-route.md) | routes removed after a freeze (F1) |
| [0080](../decisions/0080-a-kind-grant-may-glob-and-a-glob-never-reaches-auth-material.md) | what a wildcard never reaches |
| [0083](../decisions/0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md) | one process (F13) |
| [0125](../decisions/0125-a-provider-write-is-a-callable-that-writes-no-record.md) | a retried provider write is sent twice (F10) |
| [0143](../decisions/0143-a-token-has-full-access-to-its-repository-and-a-login-token-does-not-expire.md) | tokens that do not expire (F5) |
| [0165](../decisions/0165-a-commit-in-doubt-is-caught-up-by-the-next-write-and-no-write-grant-names-the-owners-auth-kinds.md) | a write whose confirmation is lost (F12) |
| [0194](../decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md) | the app flow and limited tokens |
| [0198](../decisions/0198-a-bundle-offers-its-functions-and-an-agent-takes-them-by-pattern.md) | functions offered to agents as tools |
| [0212](../decisions/0212-a-package-is-published-by-its-authoritys-document-and-installed-from-it.md), [0213](../decisions/0213-a-published-package-keeps-its-publishers-versions-and-an-upgrade-waits-for-every-connected-build.md), [0220](../decisions/0220-outside-code-is-confined-and-reviewed.md) | proposed records with shipped code (F1); outside code must be confined (F10) |
| [0245](../decisions/0245-a-stale-revision-is-a-validation-refusal-narrowed-by-the-reason-stale-revision.md) | a proposed record already on the wire (F1) |
| 0042, 0048, 0051, 0065, 0069, 0070, 0098, 0101, 0191 | linked in [the table of records this would replace or amend](#decision-records-this-would-replace-or-amend) |

The index of every record is [docs/decisions](../decisions/README.md).

### Other systems

Named for comparison. Each link answered on 2026-11-11. `lint:docs` does
not fetch them.

| System | Source | Cited for |
| --- | --- | --- |
| etcd | [API guarantees: revisions and compaction](https://etcd.io/docs/v3.5/learning/api/) | history kept until a compaction point; a client behind it is told |
| Kubernetes | [kube-apiserver flags](https://kubernetes.io/docs/reference/command-line-tools-reference/kube-apiserver/), `--etcd-compaction-interval` | compaction every five minutes by default |
| Kafka | [log compaction](https://kafka.apache.org/documentation/#compaction) | keeping the latest value per key |
| Kubernetes API | [API concepts](https://kubernetes.io/docs/reference/using-api/api-concepts/) | list and watch from a version; 410 Gone; one reader feeding many watchers |
| Kubernetes versioning | [API versioning](https://kubernetes.io/docs/reference/using-api/), [versions of a custom resource](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definition-versioning/), [feature gates](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/) | several versions of a resource served with conversion; maturity as a separate thing |
| Kubernetes, SemVer | [deprecation policy](https://kubernetes.io/docs/reference/using-api/deprecation-policy/), [semver.org](https://semver.org/) | labels, notice before removal, what a major version may break |
| AT Protocol | [DID](https://atproto.com/specs/did), [handle](https://atproto.com/specs/handle), [AT URI](https://atproto.com/specs/at-uri-scheme), [NSID](https://atproto.com/specs/nsid), [repository](https://atproto.com/specs/repository) | a permanent id, a handle that can change, record types owned by publishers; a signed tree of current records |
| CouchDB | [changes feed](https://docs.couchdb.org/en/stable/api/database/changes.html), [document API](https://docs.couchdb.org/en/stable/api/document/common.html) | a sequence per database; a revision guard on a write |
| JMAP | [RFC 8620](https://www.rfc-editor.org/rfc/rfc8620): accounts (section 1.6.2), the session (2), `/changes` (5.2), `StateChange` (7.1) | one sign-in over several accounts; a state per account |
| Replicache | [how it works](https://doc.replicache.dev/concepts/how-it-works), [source](https://github.com/rocicorp/replicache) | a client queue replayed over the server's order |
| Matrix | [client-server API](https://spec.matrix.org/latest/client-server-api/), `/sync` | one stream over many rooms |
| Automerge, Yjs | [automerge.org](https://automerge.org/), [docs.yjs.dev](https://docs.yjs.dev/) | merging simultaneous edits with no server choosing the order |
| IndieAuth | [specification](https://indieauth.spec.indieweb.org/) | a person named by a URL they control |
| OpenID Connect | [core](https://openid.net/specs/openid-connect-core-1_0.html) | a server vouching for a person |
| OAuth token exchange | [RFC 8693](https://www.rfc-editor.org/rfc/rfc8693) | a sign-in exchanged for a token per repository |
| JSON canonicalization | [RFC 8785](https://www.rfc-editor.org/rfc/rfc8785) | the standard canonical form, and why it does not fit exact numbers |
| Event sourcing patterns | [throw away the key](https://verraes.net/2019/05/eventsourcing-patterns-throw-away-the-key/) | erasing by destroying a key (F6) |
| Postgres | [continuous archiving](https://www.postgresql.org/docs/current/continuous-archiving.html) | shipping a log after the commit |
| Postgres | [ALTER COLLATION](https://www.postgresql.org/docs/current/sql-altercollation.html) | an index and a changed collation version |
| Litestream | [how it works](https://litestream.io/how-it-works/) | copying a log to object storage |
| Amazon S3 | [appending to objects](https://docs.aws.amazon.com/AmazonS3/latest/userguide/directory-buckets-objects-append.html) | append exists in one storage class only |

## Appendix: how the sizes were counted

Each figure is `wc -l` over the named files at `f26d42a6b`. "All of" a
directory means its `*.go` files outside `_test.go`, without its
subdirectories. No file is counted in two rows. Two rows include files that
are not non-test Go, and say so.

| Row | Files | Lines |
| --- | --- | --- |
| Engine | all of `internal/engine` | 94,476 |
| Measured values, aggregates, units | in `internal/engine`: `aggregate*.go` (8,851), `observed*.go` (2,645), `unitgraph.go` and `unitseed.go` (1,465), `quantity.go`, `quantitygraph.go` and `quantitywrite.go` (1,378), `metric.go` and `metricrules.go` (329), `timeunits.go` (54): 14,722. All of `internal/observed` (1,935). In `internal/vocabulary`: `unitgraph.go`, `quantity.go`, `observed.go`, `metric.go`, `rational.go` (3,155) | 19,812 |
| Functions, triggers, sandbox | in `internal/engine`: `functions.go`, `triggers.go`, `delivery.go`, `runner.go`, `effects.go`, `sync.go`, `schedulecursor.go`, `callrun.go`, `invocationconfig.go`, `background.go` (9,038). All of `internal/runner` (3,113), `internal/sandbox` (3,167), `internal/egress` (428). `internal/runner/host.py`, which is Python (1,573) | 17,319 |
| Agents and AI model adapters | in `internal/engine`: the twenty `agent*.go` files (9,595), and `policy.go`, `judge.go`, `approvals.go`, `spend.go`, `operatorspend.go`, `interactions.go`, `llmmodels.go`, `offeredtools.go`, `alerts.go`, `operatorprovider.go`, `keyrefusals.go` (4,328): 13,923. All of `internal/llm` (2,505) | 16,428 |
| Packaging | all of `internal/catalog` (2,994), `internal/authority` (586), `internal/appsetup` (1,957), `internal/authorize` (1,501). In `internal/engine`: `outsidecode.go`, `outsideupgrade.go`, `appowned.go`, `appbuild.go`, `addonconsent.go`, `bundles.go`, `inputs.go`, `settings.go` (2,350). In `internal/vocabulary`: `rehome.go`, `bundle.go` (1,164) | 10,552 |
| The on-disk copy of history | all of `internal/changelogfile` (4,062). In `internal/engine`: `repodir.go`, `snapshot.go`, `export.go`, `rewrap.go`, `verify.go`, `segmentdigest.go`, `scrub.go`, `checksum.go`, `changelogdialect.go` (4,398) | 8,460 |
| The on-disk copy, tests | every `_test.go` of `internal/changelogfile` (4,235). In `internal/engine`, the tests named `repodir*`, `snapshot*`, `export*`, `durable_ack*`, `bootdigest*`, `sealing*`, `manifest*` (7,606) | 11,841 |
| Changing kinds while data exists | in `internal/engine`: `schemadiff.go`, `convert.go`, `rename.go`, `move.go`, `retirement.go`, `upgradeplan.go`, `shippedupgrade.go`, `repomigrate.go`, the five `repomigration_*.go`, `restore.go`, `requiresfloor.go` | 7,786 |
| Limited tokens for apps | in `internal/api`: `scoped.go`, `scopedreads.go`, `scopedcall.go`, `scopedagents.go`, `authorize.go`, `authorizecall.go`, `appsetup.go` (4,367). All of `internal/scope` (892) | 5,259 |
| Mapping and merging records | in `internal/engine`: `mapping.go`, `mappingbackfill.go`, `mappingwhere.go`, `merge.go`, `orphans.go`, `ambiguous.go`, `linkedfrom.go`. In `internal/vocabulary`: `mapping.go`, `mappingsubject.go`, `seededmapping.go`, `suggested.go` | 4,515 |
| Search and vectors | in `internal/engine`: `search.go`, `searchindex.go`, `searchtext.go`, `bm25.go`, `tsquery.go`, `embedprovider.go`, `reprojection.go` | 4,495 |
| Applets | all of `internal/applet` (3,210). `internal/engine/applets.go` and `internal/api/applets.go` (326) | 3,536 |
| MCP | all of `internal/mcp` | 2,311 |
| The stand-in engine | `internal/api/fake_test.go`, `fake_aggregate_test.go`, `fake_window_test.go`, which are test code | 1,900 |

The tests F9 says would go with the synchronous paths: in
`internal/engine`, `durable_ack_db_test.go` (807), `snapshot_db_test.go`
(563), `snapshotbase_db_test.go` (312), `bootdigest_db_test.go` (199); in
`internal/changelogfile`, `txn_test.go` (376), `unread_test.go` (432),
`snapshot_test.go` (100), `writer_test.go` (681). Together 3,470.
