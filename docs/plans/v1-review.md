# Review for 1.0: architecture, data model and system design

Status: a review of the tree at `f26d42a6b` (v0.123.1), dated 2026-11-11. It
recommends and implements nothing, and like every page under `docs/plans/`
it is not a contract: the code that lands is.

## Contents

- [Summary](#summary)
- [What the product has to support](#what-the-product-has-to-support)
- [How to read this page](#how-to-read-this-page)
- [How the system works today](#how-the-system-works-today)
- [Findings](#findings), F1 to F14
- [Defects found in passing](#defects-found-in-passing)
- [Verdicts on the four core designs](#verdicts-on-the-four-core-designs)
- [Designs this review proposes](#designs-this-review-proposes)
  - [Shared repositories](#shared-repositories)
  - [Several repositories under one API](#several-repositories-under-one-api)
  - [What could be cut](#what-could-be-cut)
- [Decisions to make](#decisions-to-make)
- [Fixes and validation](#fixes-and-validation)
- [A proposed stability policy](#a-proposed-stability-policy)
- [References](#references)
- [Appendix: how the sizes were counted](#appendix-how-the-sizes-were-counted)

## Summary

Substrate is a server that keeps one person's data as typed records, with a
full history of every change, and lets apps, scheduled code and AI agents
read and write it. This page asks what must be settled before that server
is called 1.0: which choices get expensive to reverse once people have
years of data in it, apps in stores that talk to it, and packages published
for it.

**Verdict: not ready to be called 1.0, and close.** The core design is
sound and should stay. Nothing below is a redesign. What is missing is in
three groups.

1. **Nothing says what 1.0 promises.** The server labels some features
   "stable", and features with that label have changed incompatibly, most
   recently on 2026-11-08 (F1).
2. **The design assumes one person, one repository, one name, forever.**
   The product needs several people in one repository, sharing between
   repositories, and probably the ability to rename one. None is possible
   today, and a few details would make each of them much harder after 1.0
   than before (F2, F3, F4, F5).
3. **Some storage choices are only cheap to change now.** How a deletion
   reaches history, how a history line is checksummed, how the on-disk copy
   is written, and a handful of smaller details (F6 to F9, F14).

**One question sits under three of the findings: may history be
rewritten?** Renaming a repository, erasing a deleted value and shrinking
old history all rewrite stored history. A hash chain or signatures, which
would prove history was not altered, forbid rewriting it. Today the format
allows rewriting and proves nothing. Both doors can stay open, but only if
two format details change before 1.0 (F7). After 1.0, picking one closes
the other.

The findings, in priority order:

| | In plain words | Before 1.0 |
| --- | --- | --- |
| [F1](#f1-nothing-defines-what-10-promises-and-the-stable-label-has-already-been-broken) | Nothing defines what 1.0 promises, and the "stable" label has already been broken | fix |
| [F2](#f2-a-repositorys-name-does-five-jobs-and-can-never-change) | A repository's name does five jobs and can never change | decide, and fix two parts |
| [F3](#f3-one-person-per-repository-is-built-into-sign-in-history-and-the-vocabulary) | One person per repository is built into sign-in, history and the vocabulary | decide, and shape the contract |
| [F4](#f4-nothing-can-point-from-one-repository-into-another) | Nothing can point from one repository into another | reserve |
| [F5](#f5-who-may-write-what-is-enforced-differently-for-each-kind-of-writer) | Who may write what is enforced differently for each kind of writer | fix |
| [F6](#f6-deleting-does-not-erase-and-history-only-grows) | Deleting does not erase, and history only grows | decide |
| [F7](#f7-history-cannot-be-shown-to-be-unaltered-and-two-format-details-would-make-that-hard-to-add) | History cannot be shown to be unaltered, and two format details would make that hard to add | fix the two details |
| [F8](#f8-the-stored-format-is-the-way-out-and-the-upgrade-path-and-nothing-pins-it) | The stored format is the way out and the upgrade path, and nothing pins it | fix |
| [F9](#f9-the-on-disk-copy-of-history-is-written-synchronously-and-costs-more-than-it-buys) | The on-disk copy of history is written synchronously, and costs more than it buys | decide |
| [F10](#f10-code-in-packages-has-no-version-to-build-against) | Code in packages has no version to build against | fix |
| [F11](#f11-a-package-cannot-say-which-server-it-needs) | A package cannot say which server it needs | fix |
| [F12](#f12-api-details-that-would-freeze-as-they-are) | API details that would freeze as they are | fix |
| [F13](#f13-operating-limits) | Operating limits: one process, one boot, shared tables | decide and document |
| [F14](#f14-value-and-storage-details-that-freeze-into-data) | Value and storage details that freeze into data | mixed |

[Decisions to make](#decisions-to-make) lists every choice this page asks
for, with its options and a recommendation, ready to be answered one by
one.

## What the product has to support

This is the brief the review is held to. It is wider than what the code
does today.

| Capability | Needed | Today |
| --- | --- | --- |
| Several people in one repository, each with their own sign-in and their own permissions. The creator is the owner. Tens to hundreds of people, not enterprise access control | required | one sign-in per repository, with full access |
| Sharing between repositories, including with a person on another server | required, later | not possible |
| One API, and one agent, working over several repositories | wanted | one repository per sign-in |
| Renaming a repository | to decide | not possible |
| Deleting that removes the value everywhere it is stored; history that does not grow without limit; retention per kind of data | to decide | a delete keeps the history; nothing is ever removed |
| Evidence that history was not altered: a hash chain, signatures | to decide | damage is detected; alteration is not |
| More than one server process for one repository | not required | one process |

Today's operations guide lists several of these as deliberate absences:
"No sharing, no second user reading your repository, no cross-repository
query. No erasure, compaction, or retention policy … No signatures and no
hash chain" (`docs/operations.md:1963-1969`). This review reads that as a
description of the current code, not as a constraint on the product. For
each capability it asks two things: does 1.0 need it, and has any choice
already made it hard?

The short answer to the second question:

| Capability | What would lock it out if 1.0 froze today's design | Finding |
| --- | --- | --- |
| Several people in one repository | a sign-in contract that names a repository and not a person; history lines that cannot say who; the word `owner` already meaning "any human" in the vocabulary | F3, F5 |
| Sharing between repositories | a reference that cannot say which repository it points into; two people's copies of the same collection being unrelated kinds | F4 |
| One API over several repositories | nothing, provided clients are told to ignore response fields they do not know | F12 |
| Renaming | the name being the storage id; and, later, any hash chain over lines that carry the name | F2, F7 |
| Erasure and bounded history | a checksum that covers the values it would erase; a promise that finished history files never change; no place in a kind to declare retention | F6, F7 |
| Evidence of unaltered history | the same checksum; a canonical form only Go can reproduce | F7 |
| More than one process | nothing; F9 makes it easier | F13 |

None of these is locked today. Several would be at 1.0.

## How to read this page

- **Citations.** Every `path:line` was opened by the author, except a
  claim marked †, which a delegated reading pass reported and the author
  did not open again. Treat a † as a lead, not a result.
- **Nothing was run:** no database suite, no live test. A test named here
  exists in the tree; nobody watched it pass.
- **A second reviewer**, another model, opened 139 citations of an earlier
  draft and tested its reasoning. What it found is fixed here. F3, F4, F7,
  the rename options in F2, the erasure options in F6 and the list of
  decisions were written after that check and have had only the author's.
- **Sizes** are lines of Go outside `_test.go` files, counted with `wc -l`
  over the files [the appendix](#appendix-how-the-sizes-were-counted)
  names. They say where the code is, not what it is worth.
- **Estimates** are marked as estimates and give their basis.
- **Other systems** are named for comparison and linked under
  [References](#references). They are described from their specifications;
  none was tested against this code.
- A finding has a **kind**: *demonstrated* (the code path shows it), *risk*
  (plausible, not shown) or *judgment*.

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

What holds the design together:

| Invariant | Where it is enforced | Held by |
| --- | --- | --- |
| One path from changelog to `records`; a rebuild reproduces the fold | `internal/engine/fold.go`, `internal/engine/rebuild.go:121-128` | `foldpath_internal_test.go`, `rebuild_db_test.go` |
| A write is on disk before Postgres commits; an unfinished transaction is cut whole | `internal/engine/dataset.go:853-1013` | `internal/changelogfile/txn_test.go`, `durable_ack_db_test.go`, `repodir_db_test.go` |
| Sequence order is the order clients see, so a resumed client skips nothing | a lock taken at transaction start, `internal/engine/dataset.go:784-797` | `TestChangesCursorWalkSkipsNoSeqUnderConcurrentWriters` |
| One writer process per repository | a file lock (`internal/changelogfile/lock.go:35-48`) and a database lease (`internal/engine/writerlease.go:33-38`) | `writerlease_db_test.go` |
| A repository reads only its own rows | Postgres row level security on every table, `internal/engine/migrations/0001_init.up.sql:551-577` | `isolation_db_test.go` |
| An app's token reaches only what was decided for it | two route groups (`internal/api/api.go:465-466,542-543`) and a wrapper that must spell out every operation (`internal/api/scoped.go:19-40`) | the compiler; API tests against a fake |
| Secret material never enters the changelog | a secret is stored as a pointer into an encrypted side store (`internal/engine/credentials.go:458-466`, `internal/engine/auth.go:8-12`) | `sealing_db_test.go`† |
| An older server refuses data a newer one wrote | migration hashes and two format stamps (`internal/engine/dialect.go:41-45`) | `migrate_db_test.go`† |
| A declaration's key set is closed; a narrowing change is refused while records hold the old shape | `internal/vocabulary/load.go:827-834`, `internal/engine/schemadiff.go` | `dialect_test.go`, `upgrade_guard_db_test.go`† |

What people will come to depend on, and what each should be at 1.0:

| Surface | Depended on by | Today | Recommended at 1.0 |
| --- | --- | --- | --- |
| Reading and writing records over HTTP, and the error format | every client | served; not on the feature list | **stable**, as a named feature |
| The change feed and watch | apps, console, triggers | labelled `stable` | stable, after F12 |
| Search; aggregates and units | console, apps | `beta`; `alpha` | as labelled |
| Signing in (`/register`, `/login`, `/tokens`, `/password`, `/totp`) | every client, apps in stores included | outside `/api/v1`, not on the list | **not frozen until F3 is decided** |
| App authorization (`/oauth/*`, scopes) | third-party apps | `alpha` | beta, or 1.0 says third-party apps are experimental |
| Webhook URLs | outside services | not on the list | stable: they are registered elsewhere |
| The declaration dialect | package authors, every stored repository | closed | stable, with F11's floor and the reservations in F3, F4 and F6 |
| The interface a function's code is written against | package authors | no version | beta until versioned (F10) |
| Agents, MCP tools, applets | owner, assistants | `alpha` | experimental; the records they write are still durable |
| The history line format and the export | every backup, every way out | called storage, free to change (`docs/changelog.md:55-61`) | public and documented, once F6, F7 and F9 are decided (F8) |
| Postgres tables | nobody | internal | internal; say so |
| The Go interfaces in `internal/substrate` | this module only | "what v1 freezes" (`internal/substrate/dataset.go:19-24`) | internal; remove the sentence |
| The CLI | operators, scripts | no statement | `-o json` and `-o yaml` follow the wire; table output is not promised |

## Findings

In priority order. Each starts in plain terms, gives an example, then the
evidence.

### F1. Nothing defines what 1.0 promises, and the "stable" label has already been broken

*Demonstrated. Fix before 1.0.*

**In plain terms.** A client needs to know which parts of the server it
can build on without being broken by the next release. The server answers
that in two ways, and neither works yet.

The first is the URL prefix. Every API address starts with `/api/v1`, and
the API guide says: "Within v1 the REST surface is additive only: fields
and endpoints are added, never removed or narrowed" (`docs/api.md:1007`).

The second is a list. The server publishes a small document at
`/.well-known/substrate/server.json` naming its features and labelling each
one. A shortened example:

```json
{
  "versions": [{"name": "v1", "status": "served"}],
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

**What is wrong.**

- **The list leaves out what clients use most.** Five features are labelled
  stable: `triggers`, `functions`, `bundles`, `blobs` and `changefeed`
  (`internal/api/discovery.go:192-206`). Reading and writing a record,
  listing records, the error format and signing in are not on the list at
  all. The flow a third-party app signs in through, `apps`, is alpha
  (`internal/api/discovery.go:227-231`).
- **Stable features have changed incompatibly.** Two examples:
  - v0.121.0 (2026-11-08) changed a value in the catalog's answer from
    `"tier": "provider"` to `"tier": "published"`. A client comparing
    against `provider` stopped matching. The catalog belongs to the
    `bundles` feature, labelled stable.
  - v0.112.0 made the `source` property of a trigger required, with the
    note "an older binary cannot open the upgraded core". `triggers` is
    labelled stable.
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
  are the whole closed set" (`docs/api.md:2500`). The web console's own
  list lacks one real code, `parked`, and carries one, `unsupported`, that
  the server never sends (`web/console/src/lib/api/types.ts:12-27`).
- **A promise with no audience.** A comment says the whole Go interface
  "is what v1 freezes" (`internal/substrate/dataset.go:19-24`). That
  package is under `internal/`, so nothing outside this repository can use
  it.
- **Decisions marked undecided, with their code shipped.** 56 of the 241
  decision records are still `proposed`. Several describe code that is on
  `main`: 0212; 0213 (`internal/catalog/refresh.go:99-103`); 0220
  (`internal/engine/outsidecode.go:31-46`); and
  [0245](../decisions/0245-a-stale-revision-is-a-validation-refusal-narrowed-by-the-reason-stale-revision.md),
  whose `stale_revision` value clients already receive
  (`internal/substrate/errors.go:139`). Four tests that accepted records
  0050, 0052 and 0062 name as their proof no longer exist.

**Why it matters.** Someone writes an app against 1.0 and ships it to an
app store. A 1.1 renames a value in a "stable" answer, with a correct
release note. The app breaks, and its users cannot fix it. The project's
own Apple console app is in the same position: it "carries its own copy of
`web/console/dist`" (`apps/README.md:13-17`), so an old build talks to a
newer server, guarded only by a minimum server version.

**Recommendation.** Write down what 1.0 promises
([a proposal](#a-proposed-stability-policy)). Put reading and writing
records on the feature list as stable. Label honestly: `functions` is not
stable until F10 is fixed, and signing in should not be frozen until F3 is
decided. Remove the sentence about the Go interface. Hold the guide and the
console to the code's list of error codes with a test. Accept or reject
every `proposed` record whose code is on `main`. Then go a stated number of
releases without breaking anything labelled stable.

### F2. A repository's name does five jobs and can never change

*Demonstrated. Decide before 1.0 whether a repository can be renamed; fix
the default name and add a storage id either way.*

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
for good. Every kind they import is `ada.localhost/…`. If the server later
moves behind a real hostname, typing `ada` resolves to a different name,
and only the full old name signs in.

**Why it cannot change.** The name is the database key, held equal to the
name by a constraint
(`internal/engine/migrations/0001_init.up.sql:62-69`). The directory must
carry the same name (`internal/changelogfile/manifest.go:139-148`). The
repository's encryption key is bound to it
(`internal/engine/dek.go:38-44`), and each stored secret is bound to the
kind of the record that owns it (`internal/engine/credentials.go:48-54`).
Importing a sample rewrites every mention of the sample's name to the
repository's, including inside function source
(`internal/vocabulary/rehome.go:9-21`), so the name is in the user's own
code. An app's token stores its scopes with the name written out
(`internal/scope/scope.go:9-11`). Nothing anywhere is stored relative to
"this repository".

[Decision 0052](../decisions/0052-the-authority-is-the-repository-id.md)
made the name the id and priced it honestly: "the authority is now truly
permanent … a typo at registration is forever". It named when to look
again: "domain verification binding the authority to something the server
can check". That exists now
([0208](../decisions/0208-a-repository-proves-its-name-with-a-txt-record-after-registration.md)),
and the question was not looked at again. One move has already been done by
hand: 2,380 records of one package were moved between two names with a
script (`docs/plans/authorities/status.md:52-55`).

**Is "never" acceptable?** I do not think so, for three ordinary cases: a
typo; a person who starts on `localhost` or on a host's name and later gets
a domain; and a person who leaves one host for another. With several
people in a repository (F3) a fourth appears: a household repository named
after one person.

**What a rename has to do**, job by job:

| Job | To rename it |
| --- | --- |
| 1. Sign-in name | Trivial once a person's identity is separate from the repository (F3): the person renames their login, and the repository is untouched |
| 2. Storage id | Stop using the name as the id. Mint an opaque id, and bind the directory, the database key and the encryption keys to that |
| 3. Namespace of own kinds | The hard one: the string is inside every record, reference and history line. See the options below |
| 4. Address | Keep the old name as an alias, as a merged record keeps its former ids. Webhooks registered elsewhere keep working |
| 5. Code names in history | Follows job 3 |

**Options for job 3.**

| | Option | What it takes | What it costs |
| --- | --- | --- | --- |
| A | Never rename | nothing | the three cases above stay unfixable |
| B | Rename the login only | F3's identity split | kinds keep the old string. Cosmetic, until names are shown or shared |
| C | Offline rewrite: export, replace the name in every line, import under the new name | the pieces exist: the walk that replaces a name everywhere (`internal/vocabulary/rehome.go`), the tool that re-binds keys (`internal/engine/rewrap.go`), and import at boot. Clients list again afterwards | the server is down for the length of the history. It rewrites every line's checksum, so it stops being possible if history is ever chained or signed (F7) |
| D | Store "this repository" as a placeholder. Lines hold `~/tasks/task`; the server writes the full name on the way out | a translation at the storage boundary and one migration of existing data. Not estimated | touches the read and write path. Function source that spells the full name still needs option C's walk, as ordinary writes |

D is the only option that survives a hash chain, and it has a second
benefit: an export could be imported under a different name, which is what
moving between hosts needs.

One thing no option can do: rename a name that other repositories have
installed packages from. A publisher's name is like a domain in a link.
The remedy is a "moved to" pointer, not a rename.

**The project's own name has the same problem.** `substrate.reamde.dev` is
in every repository's built-in kinds, references and history. The file
that declares it still calls it "a PLACEHOLDER"
(`kinds/substrate.reamde.dev/authority.yaml:5`). Meanwhile packages the
server does not ship are fetched from wherever that name's DNS points
(`internal/catalog/published.go:62-80`). Changing it later means moving
each kind, with the old kinds declared forever
(`kinds/substrate.reamde.dev/core/core.yaml:102-115` is the precedent).

**Names can be taken.** A deleted name is free at once
([0236](../decisions/0236-an-owner-may-delete-their-repository-and-the-name-is-freed.md)).
DNS proof is optional, and nothing but its own screens reads the result:
outside tests, `domainVerifiedAt` appears only in the three `domain.go`
files and the wire struct. Registration refuses only names under
`substrate.reamde.dev` (`internal/engine/engine.go:1779-1792`). On a server
several people use, anyone may register `google.com`, or the name of a
repository deleted yesterday, and with it the address outside services
still post to.

A publisher's packages are trusted by a DNS record and a document fetched
again each time, and a release that only adds things installs with nobody
asked while the document's address is unchanged
(`internal/catalog/refresh.go:76-113`). Whoever next owns a lapsed domain
inherits that.

**Recommendation.**

1. Complete a short name under the server's configured public host, and
   refuse `localhost` and IP addresses outside development.
2. Mint an opaque repository id now and bind storage to it (job 2). This
   is needed by every rename option but A.
3. Build option C as the 1.0 answer to a typo or a move. It needs no format
   change.
4. Decide option D together with F7. If history may one day be chained, D
   is what keeps rename possible.
5. Decide the project's permanent name and who holds its DNS.
6. On a shared server, hold a deleted name for a period, and require DNS
   proof for names outside the server's own host.

### F3. One person per repository is built into sign-in, history and the vocabulary

*Demonstrated. Decide the model before 1.0, and shape the contract to it
even if the feature ships later.*

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
| Keys | one recovery key and one wrapped encryption key, both with the id `self` (`internal/engine/auth.go:52,57`) | stored data |
| Limits | four chat turns at once per repository (`internal/engine/agents.go:376-379`); no limit per person | internal; free to change |
| Clients | the Chat app asks for full access (`apps/chat/ChatKit/Sources/ChatKit/ChatManifest.swift:35`); an app holds one repository per connection (`docs/terms.md:170`) | apps in stores |

**What is not a problem.** The history itself. It is one ordered list
written by one server process, and several people are simply several
clients of that process. Nothing about its shape has to change.
[Decision 0017](../decisions/0017-the-changelog-is-one-writers-total-order.md)
rules out something different: several servers each writing their own
history and merging them.

**Recommendation.** Adopt the model in
[Shared repositories](#shared-repositories): a person's identity, a
repository, and a membership that joins them. Today's design is its special
case, one person and one repository with the same name. Then, before 1.0:

1. Do not label sign-in stable in its current shape. Either ship the new
   shape with one member, or label it beta.
2. Add `subject`, the person, to the history line's key set. It can be
   left out when it is the repository's only person, so it costs nothing
   until a second person exists.
3. Rename the stored `owner` tier, for example to `person`, so the word is
   free for the role.
4. Fix F5 first. A member is a principal with less than full access, and
   today that is enforced in one layer only.

### F4. Nothing can point from one repository into another

*Demonstrated. Reserve before 1.0; build later.*

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
  the server these are unrelated kinds. An app asking for "tasks" in both
  has to know both names, or ask by a shared trait.
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

**What would lock it in.** Two things. The reference's shape is part of the
stored format and the dialect: a third reserved name added later can
collide with a property a package already declared. And data accumulates
under per-repository kind names for as long as copying is the default.

**Recommendation.** Reserve a name in the reference value for "which
repository" now, unused, as `target` was. Decide how copied collections
relate (decision D16). Make it a rule that a client ignores response fields
it does not know (F12), so a repository can be added to a record. Build
nothing else yet: the nearest route to sharing is several people in one
repository (F3), and across servers it is an existing mechanism
([below](#sharing-with-someone-on-another-server)).

### F5. Who may write what is enforced differently for each kind of writer

*Demonstrated by reading; no test exercises it. Fix before 1.0.*

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
person reading that list when they install the package.

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

**Why it matters more with F3.** A member of a shared repository is a
person with limited access. If limits are enforced in one layer and not the
others, a viewer can ask an agent to write for them.

**Recommendation.** One list of control kinds (triggers, policies, AI
provider settings, package settings and secrets, applets, declarations),
and one table saying what each sort of writer may do to each, held by a
test. An agent a caller starts is limited by the caller's access as well as
its own. Restrict the writer-name header to a fixed form. Add "sign out
everywhere". Cut every stream when its token dies. Correct `docs/auth.md`.

### F6. Deleting does not erase, and history only grows

*Demonstrated. Decide before 1.0: the choice shapes the stored format.*

**In plain terms.** Deleting a record removes it from the current state.
Its values stay in history, on disk and in the database, and can still be
read back. Nothing ever shrinks. Keys that protect secrets are never
changed.

**Example.** A person syncs three years of mail and health data. They
delete one message and choose "delete permanently". The message is gone
from their lists. It is still in the history files, in the history table,
in every export and backup, and the change feed will still serve its text
to anyone who asks for old values. If someone else hosts their server, the
host cannot remove that one message for them, short of deleting the whole
repository.

**Evidence.**

- A permanent delete removes the record's current row and its index rows,
  and nothing from history (`internal/engine/rows.go:738-767`). The change
  feed computes before and after values from history alone
  (`internal/engine/changevalues.go:13-20`).
- Two things *are* removed. A deleted record's secrets go with it
  (`internal/engine/rows.go:728-736`), and a file with no record left
  pointing at it is collected (`internal/engine/gc.go:50-53`).
- Each write is stored three times: the history line on disk, the same
  line in a database table
  (`internal/engine/migrations/0001_init.up.sql:257-280`), and the current
  record. Nothing is ever dropped: the oldest position a client may resume
  from is fixed at zero (`internal/api/watch.go:42-48`).
- Bookkeeping is kept forever too. Every time a trigger runs, a run record
  and a delivery entry are written to history
  (`internal/engine/functions.go:38-46`). The current state keeps the last
  twenty runs per trigger
  (`internal/engine/functions.go:57-62,2594-2607`); history keeps all of
  them.
- A kind has nowhere to say how long its history should be kept. The keys a
  kind may declare are a closed set with no such key
  (`internal/vocabulary/load.go:827-834`).
- The recovery key is written once, at registration
  (`internal/engine/auth.go:455`). The key that encrypts secrets is created
  once and never replaced (`internal/engine/engine.go:1638`). Moving a
  repository to a host with a different key is an offline step
  ([0054](../decisions/0054-a-repository-moves-between-host-keys-through-an-offline-rewrap.md)).

**What already helps.** The API already has an answer for "that part of
history is gone": an error named `compacted` that tells the client where to
start again, and the server already publishes the oldest position it keeps.
New history-line shapes are gated by a format stamp. And because history
lines are not chained to each other
([0050](../decisions/0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md)),
one line can be rewritten without touching the rest.

**What stands in the way.**

- The rule that "a finished segment … never changes"
  (`docs/terms.md:44`). Any erasure that reaches history breaks it.
- Positions are stored inside data: a line names the earlier line that
  caused it, and a chat message names the changes it made. So history can
  lose its oldest part, but can never be renumbered. Decision 0017 says
  this itself.
- Copies already taken. No server-side erasure reaches a backup or an
  export that was made before it. Only one option below does.
- The checksum, if history is ever chained (F7).

**Options for erasure.**

| | Option | Reaches | Cost |
| --- | --- | --- | --- |
| A | Keep "delete does not erase", and say so wherever the product says delete | nothing | honest, and a real liability for mail and health data hosted for someone else |
| B | Redaction: on request, blank the values in a record's earlier history lines, in place. Position, kind, id and time stay | the server's own history | needs finished history files to be replaceable. The record's id stays, and an id can itself be personal: a provider may compose one from an address |
| C | Keep values out of history for chosen kinds: the line holds a pointer, the value lives beside it, and erasing deletes the value. Secrets already work this way | the server's own history | replaying history then needs the side store. It is a per-kind choice a package declares |
| D | Encrypt each record's values under its own key and erase by deleting the key | everything, including copies already taken | every value encrypted at rest; a key per record to manage. Search still needs the plain text in the database |

**Options for growth.** Retention: a kind declares how long its history is
kept, and bookkeeping kinds declare "not at all". Compaction: the current
state at some position becomes the new start of history, everything before
it is dropped, and clients below that position are told `compacted`.

**Recommendation.**

1. Decide what delete promises (decision D23). My recommendation is B for
   anything, on request, with C available to a kind that asks for it.
2. Reserve a `retention` key on a kind now, unused. Take run records and
   delivery entries out of permanent history.
3. Plan compaction for after 1.0, and prove now that the first-party apps
   recover from `compacted`, since the contract already allows it.
4. Stop promising that finished history files never change.
5. Keys: a *lost* recovery key needs a way to enrol a new one. A *leaked*
   one needs more: the old copy of the key stays in history and still
   opens everything (`internal/engine/rewrap.go:3-10`), so the encryption
   key itself has to be replaced and the secrets encrypted again. Even that
   cannot reach a copy already taken. The alternative is to remove the
   recovery key ([What could be cut](#what-could-be-cut)).

### F7. History cannot be shown to be unaltered, and two format details would make that hard to add

*Judgment on demonstrated facts. Not needed in 1.0; fix the two details
before it.*

**In plain terms.** Each history line carries a checksum of itself. That
catches a damaged disk. It does not catch someone changing a line and its
checksum together, and nothing links one line to the next or signs
anything. Whether that matters depends on who must be trusted, and the
brief changes the answer.

**What exists, and what was removed.** The checksum is the SHA-256 of the
line's canonical form (`internal/changelogfile/entry.go:113-133`); "it does
not chain to the previous entry and nothing signs it" (`docs/terms.md:45`).
An earlier design had both a chain and signatures. It was removed in
decision 0050 for stated reasons: about 2,100 lines of code, a host key
needed just to write, and a pile of machinery to keep the chain valid
whenever history was rewritten. Two of the removed records show the cost.
[0018](../decisions/0018-reseal-rewrites-history-values-in-place.md)
blanked values in place and then had to re-compute the chain from that
point on.
[0011](../decisions/0011-chain-transitions-are-repository-scoped-epochs.md)
added a table of "epochs" to record each such break. 0050 also says when to
look again: "if the substrate ever serves a repository whose owner does not
trust the host".

**Who would want proof, under the brief.**

| Question | Arises when | What would answer it |
| --- | --- | --- |
| Has the operator, or anyone with access to the disk, changed my history? | someone else hosts you | A chain, plus a witness the operator does not control. Clients already remember a position to resume from. If they also remembered the chain value at that position, every phone would be a witness, with no keys to manage. Certificate Transparency rests on the same idea: observers remember a log's head and check that later heads extend it |
| Did Bo really make this change? | several people share a repository | For a household, the server recording the person (F3) is enough. Real proof needs each person to hold a signing key. [0010](../decisions/0010-signing-is-per-repository-ed25519-one-way.md) rejected keys held by users, to keep "no user-managed keys" |
| Is this record, received from another server, what its owner wrote? | sharing across servers | A direct, encrypted connection to that server is enough. Signatures matter only if data is relayed through a third party |

**Is it needed for 1.0?** No. The first question is the only one with no
cheaper answer, and it matters once strangers are hosted.

**What would make it hard to add later.**

1. **The checksum covers the values.** A chain is built from each line's
   checksum. If a line's values are later blanked (F6), or its names
   rewritten (F2 option C), its checksum changes and the chain breaks from
   there on. That is the cost 0018 and 0011 paid. The remedy is a
   two-level checksum: one digest of the values, and the line's checksum
   computed over the line's other fields plus that digest. Values can then
   be removed while the line's checksum, and any chain over it, stays
   valid. A short value could be guessed from its digest, so the digest
   needs a random salt that is discarded with the value. Git stores a
   commit this way: the commit names its content by digest and does not
   contain it.
2. **Only Go can reproduce the canonical form.** Strings are escaped by
   "encoding/json's one policy"
   (`internal/changelogfile/entry.go:247-251`), which is written down
   nowhere else. A phone that is to check a chain must compute the same
   bytes.
   [0009](../decisions/0009-the-chain-hashes-what-postgres-stored.md)
   considered the standard form, RFC 8785, and rejected it because that
   standard writes numbers as floating point and this store keeps them
   exact. That reasoning still holds. So the remedy is a written
   specification of the current form with test vectors, not a switch.
3. **Rewriting and proving are opposed.** Every mechanism chosen for
   rename, erasure and compaction should be one that leaves checksums
   alone: placeholder names (F2 option D), the two-level checksum, and a
   moved start instead of renumbering.

**Recommendation.** Do not build a chain or signatures for 1.0. Before 1.0,
change the checksum to two levels and write the canonical form down. Both
are format changes that cost a migration now and a permanent second code
path later. Decide who must be trusted (decision D26).

### F8. The stored format is the way out and the upgrade path, and nothing pins it

*Risk, with demonstrated gaps. Fix before 1.0, after F6, F7 and F9 are
decided.*

**In plain terms.** The README promises "you can download everything, as
one file". That file is the repository's directory: its history lines and
its files. The same lines are what a newer server reads when it upgrades.
So their format is the most durable promise the project makes. It is
documented as internal, and nothing tests that a line written by an older
release still reads correctly.

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
  with the current code. It shows that a database migration leaves records
  alone, not that old history replays.
- A history line refuses a key it does not know
  (`internal/changelogfile/entry.go:179-180`). The effects inside the line
  do not: unknown fields are dropped silently
  (`internal/engine/fold.go:1380-1384`). If a release forgot to raise the
  format stamp, an older server would lose data without an error.
- A replay recounts each record's `version` and never compares it with the
  stored one (`internal/engine/fold.go:200-210`).
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

**Recommendation.** At each release, generate and commit a small directory
covering every sort of change, with the records it should produce. Import
every released directory under the current code and compare. Refuse unknown
fields inside a line. Document the format as the export's contract. Say how
old a directory may be and how long a fix-up is carried.

### F9. The on-disk copy of history is written synchronously, and costs more than it buys

*Judgment on demonstrated costs. Decide before 1.0: it sets the durability
sentence operators will hold the project to.*

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
The files are opened only by the boot check, a rebuild, a verify, a
snapshot, a key change and the export. No read of a record or of the change
feed touches them.

**Why it was built.** The operations guide "promises that nothing in the
database is needed to restore a repository, so an acknowledged write could
be absent from every backup" (0062, line 19). An earlier version wrote the
files after the commit and had that gap.

**What it costs.**

- Two flushes to disk on every write, one writer at a time.
- A failure state only this design has: the files fall behind the
  database, the server answers "unavailable, retry" for a write that in
  fact succeeded, and refuses writes until it is restarted (F12).
- A boot check over every repository that stops the whole server if one
  directory is damaged (F13).
- A background check of old files that had to be slowed down. Unthrottled,
  a 27 GB history took 2.3 processor cores for ten minutes and made a
  scheduled sync miss its deadline
  (`internal/engine/segmentdigest.go:15-24`).
- History files that may not change, which is what makes erasure and
  compaction awkward (F6).
- A server tied to a local disk.
- 8,460 lines of code and 11,841 lines of tests.

**What it buys that nothing else does.** If Postgres loses a write it had
confirmed, the directory still has it. Everything else the directory gives
(a per-person copy, a way out, a restore without a database dump,
checksums) does not need it to be written synchronously.

**Options.**

| | A. Keep | B. Follow | C. Archive |
| --- | --- | --- | --- |
| How the copy is written | before the answer | appended to the directory right after the commit, by the catch-up that already exists (`internal/engine/repodir.go:546`) | finished, unchanging pieces of history, with files and secrets, written to a store with four operations: put, get, list, delete. A directory by default; S3-compatible as an option |
| Lost if Postgres is lost | nothing confirmed | what had not been appended. A target, not a guarantee: under a second when healthy | what had not been archived. A target, not a guarantee: seconds to minutes when healthy, unlimited while the destination is down |
| The "fell behind" failure | stays | goes | goes |
| A write whose confirmation is lost in transit | stays | stays | stays: it is Postgres's own |
| State on the server besides Postgres | the directory | the directory | none, with S3 |
| Erasure and compaction | hard | hard | old history can leave Postgres; an erasure replaces one piece |

Object storage in general cannot append to a file; S3 can in one storage
class only. So the synchronous protocol cannot be moved to S3 as it is.
What simplifies things is writing the copy after the commit, and S3 then
becomes one possible destination. Postgres ships its own log this way,
Litestream does it for SQLite, and Delta Lake keeps a table's log as
ordered unchanging files in object storage.

**What would go (an estimate, not a list of cuts).** The code for the
synchronous protocol, the stopped-server snapshot and the background check:
in `internal/changelogfile`, half of `writer.go`, and `lock.go`,
`unread.go` and `snapshot.go`; in `internal/engine`, `snapshot.go`,
`segmentdigest.go`, the commit path in `dataset.go` and the last 360 lines
of `repodir.go`. About 2,000 lines, perhaps 3,000, and about 3,500 lines
of tests ([the appendix](#appendix-how-the-sizes-were-counted) names
them). The line format, the checksums, verify, import and export stay. An
S3 destination adds code back:
[0075](../decisions/0075-v1-ships-the-fs-blob-backend-alone.md) removed one
that "cost 1,039 lines", including a 172-line hand-written request signer.
A second attempt should use a library.

**Recommendation.** C is the direction I would take, with the directory as
the default destination. It is a direction, not yet a design. Before it
could replace 0062 it needs:

- a durable mark of the last position archived, with files and secrets
  written before the piece of history that names them;
- the delay measured and reported;
- a stated answer when the delay passes the promised limit: refuse writes,
  or keep serving and raise an alarm. That choice *is* the durability
  promise;
- a new decision record, and an operations guide that no longer says
  nothing in the database is needed.

### F10. Code in packages has no version to build against

*Demonstrated. Fix before 1.0.*

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
  retries it†. A provider that is down for a minute fails every delivery
  in that minute for good.
- A schedule that missed runs, because the server was off or the trigger
  was disabled, runs every one it missed
  (`internal/engine/schedulecursor.go:62-66`).
- A function is given a stable key per delivery
  (`internal/engine/functions.go:3063-3072`), so not repeating an outside
  action after a retry is the function's own job.
  [0125](../decisions/0125-a-provider-write-is-a-callable-that-writes-no-record.md)
  records the consequence: a retried provider write is sent twice. In the
  shipped packages the key is mentioned only in two comments.

**Recommendation.** Put an interface version on the function or the
package, 1 by default, refused when unknown. Make the Python release part
of that interface, with a stated support window. Pin the shape of the
input with a test. Label `functions` beta until then. Before delivery
behaviour freezes, tell a temporary failure from a permanent one and retry
the temporary one over minutes. Refuse to start in "best effort" mode on a
server where anyone but the operator can register.

### F11. A package cannot say which server it needs

*Demonstrated. Fix before 1.0.*

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

**Recommendation.** Let a package state the dialect it needs. Publish the
server's dialect in the discovery document. Pick the newest version the
server can read. Reserve the key now, unused, as 0020 prescribes. State
whether a publisher may ever rename a kind; "a kind's name is permanent:
deprecate it and add another" is a fair rule if written down.

Not verified: a change to one package is said not to be checked against
other packages that depend on it, and removing a package is said to have no
check for dependents†. If so, the dependent package stops loading and its
records become unreadable until it is fixed. Worth one test.

### F12. API details that would freeze as they are

*Demonstrated unless marked. Fix before 1.0; each is contained to one
handler or one rule.*

| # | In plain terms | Evidence | Recommendation |
| --- | --- | --- | --- |
| 1 | A write can carry a guard, "only if the record is still at version 3". DELETE takes it in the address (`?ifVersion=3`). PUT and PATCH take it in the body, and silently ignore it in the address, so `PUT …?ifVersion=3` writes with no guard | `internal/api/rest.go:122-167` against `174-195`; the stated rule is "never silence" (`internal/api/rest.go:218-220`) | refuse the parameter by name, as reads already do |
| 2 | Two errors can follow a write that succeeded. If Postgres confirms and the answer is lost, the client gets a server error†. If the on-disk copy fails after the commit, the client gets "unavailable, retry" | `internal/engine/dataset.go:876-885,979-1001`; [0165](../decisions/0165-a-commit-in-doubt-is-caught-up-by-the-next-write-and-no-write-grant-names-the-owners-auth-kinds.md) | mark both so a client can tell. Accept a retry key on PUT, PATCH and DELETE, which take none today (`internal/api/core.go:32-38`). F9 removes the second case |
| 3 | "Start listing again" has two different errors: 410 when history was replaced, 422 when the ordering changed underneath, which installing a package or upgrading the server can cause† | `internal/engine/query.go:616-628` | one error, and a version inside the paging token |
| 4 | The server refuses any request field it does not know. That is good for catching mistakes, and it means a newer app's new field fails on an older server | `internal/api/core.go`†, `internal/strictjson`† | keep it. State the two rules that follow: a client ignores response fields it does not know, and checks the feature list before sending a new request field |
| 5 | Signing in is outside `/api/v1` and not on the feature list | the route table† | decide with F3, then list it |
| 6 | After a permanent delete, a new record at the same id starts again at version 1, so an old guard for version 1 passes | [0072](../decisions/0072-reserve-a-purged-record-id-against-reuse.md) | 0072's reasoning holds: providers reuse ids. Document it, and have clients compare creation time |
| 7 | A client cannot write several records as one all-or-nothing step, though the apps group writes that "stand or fall together" (`docs/terms.md:174`) | — | can be added later without breaking anything |
| 8 | No time limit on a request and no rate limit on the API | `cmd/substrated/listen.go:54-61`; limits only on sign-in† | needed before many people share a server (F13) |

What a client may conclude today:

| After | It may conclude |
| --- | --- |
| A timeout on a PUT at an id it chose, with no guard | Retry. A write that changes nothing adds nothing to history |
| A timeout on a guarded write | Nothing. A retry's "conflict" may be its own first attempt. Read the record and compare |
| A timeout on a create, a function call, a merge or a split, sent with a retry key | Retry with the same key within 24 hours (`internal/engine/idempotency.go:89`). A restore from the directory alone forgets every key |
| A server error on a write | It may have succeeded |
| A dropped watch | Resume from the saved position: ordered, nothing skipped, possibly a repeat. A 410 means list again. If an operator restores the database from a dump without one extra step, the client resumes silently wrong (`internal/engine/historygeneration.go:8-15`) |

### F13. Operating limits

*Fits one person per server. Decide and document; two items need fixing
before several people share one.*

- **One server process per database.** A second one is refused
  ([0083](../decisions/0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md)),
  and the guide says to keep to one (`docs/operations.md:1100-1103`). So
  there is no standby, and a new version cannot be started beside the old
  one: every upgrade is a short outage†. Acceptable; say it is the 1.x
  shape.
- **One damaged repository stops the whole server from starting.** The boot
  check returns at the first repository that fails
  (`internal/engine/repodir.go:256-262`), on purpose. Once running, a
  failure stays with its repository†. On a server several people use this
  is the wrong way round: set the damaged one aside and serve the rest.
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
  adds indexes to it (`internal/engine/indices.go:395-401`), and because
  imported samples are copied per repository, the number grows with
  repositories times kinds. I found nothing that removes an index when its
  kind or repository goes: the only removals are of scratch and replaced
  indexes (`internal/engine/indices.go:141,224,240`). One vector index
  serves every repository
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
   (`internal/engine/scope.go:95-105`). The fix is one reader per
   repository, in memory, feeding all its watchers, as Kubernetes does. The
   API does not change.
2. **Checking tokens.** Every request looks its token up in the database
   (`internal/engine/auth.go:1203-1222`). I found no cache.
3. **The write turn.** People write slowly; one person's large sync is what
   the others feel.
4. **Per-repository limits** should become per person: four chat turns at
   once (`internal/engine/agents.go:376-379`), and no rate limit (F12).

None of this is a public promise, which is the point. The tables are
internal, so serving thousands of repositories is rework behind
`internal/substrate`, not a break. It stays that way only if no operations
guide teaches anyone to query the tables.

**What belongs in the core.** The `core` package names vendors: nine
contact mappings for Apple, Google, Slack, GitHub, Linear and Beeper
(`kinds/substrate.reamde.dev/core/contactmappings.yaml`). The built-in
assistant, its prompts, the list of AI model prices and the console's
layouts are product content. They should be named as outside the stability
promise, keeping the one rule that protects people: a shipped record is
never refreshed over an edit its owner made.

### F14. Value and storage details that freeze into data

*Mixed; each row says which.*

| In plain terms | Evidence | Why it is expensive later | Before 1.0 |
| --- | --- | --- | --- |
| The columns holding a record's kind and id sort by the database's language settings. Another table in the same database already forces plain byte order, because the default "orders case and punctuation differently from Go" | `internal/engine/migrations/0001_init.up.sql:86-106`; `internal/engine/migrations/0013_observed_index.up.sql:16-20` | the main index and every paged listing depend on the host's locale library. Postgres warns that an index built under one version of it can be wrong under the next. Changing it later rebuilds the largest index | fix |
| A property named `body` is stored in its own column; no other long text is | `internal/engine/fold.go:131-132`; [0041](../decisions/0041-body-is-a-declarable-property-and-fts-is-per-property.md) | see below | fix |
| A date and time is stored in UTC and the offset it was written with is thrown away. There is no type for "8:00 wherever I am", a time of day, or a date with no year | `internal/engine/validate.go:526-531` | the offset cannot be recovered from history | decide; reserve the types |
| A time zone value accepts `Local` and the empty string, and depends on the zone data installed on the host | `internal/engine/validate.go:559-562` | a stored value means something else on another host | fix |
| Writing `null` always removes a property, and also lets a sync fill it back in. "I deliberately have no value here" cannot be stored | `internal/engine/write.go:1062-1068,1375-1386` | a value that reappears after a sync looks like a bug; changing this changes every client | decide |
| `unique` on a property is accepted and does nothing. The guide says so ("Nothing enforces it yet"), and the code says how it would be switched on | `docs/vocabulary.md:853-857`; `internal/vocabulary/types.go:252-257` | little | can wait |
| A change to a kind that must rewrite records is limited to 10,000 records by default, in one step | `internal/engine/convert.go:104-107`; `docs/operations.md:176` | under the default, a rename shipped for a large built-in kind is refused at every start | can wait; raise before it bites |
| Adding a list of allowed values to an existing property is not treated as a narrowing; only removing values is | `internal/engine/schemadiff.go:544` | records the kind no longer allows are still served, and cannot be saved again | fix |

**The `title` and `body` columns.** A record's row has a `title` column and
a `body` column beside the one that holds its other properties. Both are
also separate fields in every history line
(`internal/engine/fold.go:131-137`), so both are part of the stored format.

*`title` earns its column.* Every list row, every link and every search
result needs a label for a record without loading its kind. The title is
the top search band (`internal/engine/validate.go:1264-1293`), it names a
linked record (`internal/engine/linkedfrom.go:64`), and lists sort by it
(`internal/engine/query.go:985`). Nearly every shipped kind builds its
title from a template: 179 templates for 180 kinds. Two things to settle.
The template is applied in one place, when a record is written
(`internal/engine/validate.go:1255`), and the result is stored, so changing
a kind's template changes a record's title only when that record is next
written. And
[0016](../decisions/0016-a-kind-titles-itself-from-a-declared-property.md)
left kinds without a template accepting a title typed in directly, which is
the inconsistency it set out to end.

*`body` does not.* It is a storage optimization that applies to a property
only if it happens to be named `body`. Nine properties are: six in GitHub's
package, one each in Linear's and Google's, one in a clinical kind. The
shipped kinds declare 106 long-text properties. 0041 kept the column
because long text "reads and orders without unpacking" the other
properties. The motive is real: Postgres stores the other properties as one
value and reads all of it to read any of it. But that is true of every long
text, not of one name. And 0041 records the price: a `body` property skips
the normal checks, so "`required`, `pattern` and `min` on a `body`
property are not enforced".

Options: make `body` an ordinary property, the smallest change; or store
every long-text property separately, by type. Either way old history lines
with a `body` field must be readable forever, which is why this is cheaper
before 1.0. The three time columns (`at`, `endsAt`, `dueAt`) are different:
date-range queries need them. They share the separate write path, though,
with its own quirks: a time column is cut to microseconds
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
| `kinds/substrate.reamde.dev/authority.yaml:5` | Calls the project's live name a placeholder (F2). |
| Decisions 0050, 0052, 0062 | Each names tests, four in all, that no longer exist. |
| `internal/api/appsetup.go:323-326,359,434-456` | A route apps may call checks for package updates before it applies the app's own limits. An app with any access can make the server fetch a publisher's document and take an additive update of a package the app has nothing to do with. |

## Verdicts on the four core designs

**The changelog.** It is three things.

- *An ordered list of changes per repository: needed.* Watching, resuming,
  triggers and the apps' local copies all stand on it.
- *Full values in that list, with everything else computed from it: the
  most valuable design in the tree.* Agents and syncs write most of the
  data, so this is the record of what they did and the way to undo it. What
  is too broad is "forever, for every kind" (F6).
- *The synchronous copy on disk: decide it* (F9). I would reduce it.

**The API.** The model is right and should stay: a record's address is its
reference, one route lists records and refuses what a mode does not
support, a client resumes from a position and is told where to restart when
that position is gone. It is not ready to freeze as written, for F1's
reason first and F12's second.

**Kinds and properties.** Sound: one stored form for a reference, defaults
filled in when a record is written, exact numbers, a closed dialect that
reserves keys before using them, changes to a kind carried out as ordinary
writes behind a confirmation, and names that never come back once retired.
The gaps are F11, F4's reservations, and the details in F14.

**The database.** Private, which is its best property. Row level security
is a stronger boundary than care taken in each query. Fix the sort order
of the identity columns, make `body` ordinary, check the minimum Postgres
version at start-up instead of in a guide†, and keep the tables out of
every operator procedure.

## Designs this review proposes

Nothing in this section is built. Each part says what exists and what it
would take.

### Shared repositories

Several people in one repository; the creator is the owner; each other
person has permissions of their own.

#### Three nouns

Today one word does three jobs. Split them:

- **Identity**: a person. A name, a password, an optional second factor. It
  holds no data.
- **Repository**: the data.
- **Membership**: a record inside a repository saying that an identity may
  act there, as an owner or with listed grants.

Today's design is the special case: one identity, one repository, the same
name, and an owner membership nobody wrote down. So no data has to move:
each existing repository gains one owner.

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
- **Own.** One addition to a write grant: only the records this member
  created. It answers "each person edits their own posts" without a list on
  every record. It needs each record to know its creator.
- **Not offered:** a list of people on each record, groups, deny rules,
  inheritance. The rule that replaces them: *a repository is the unit of
  privacy, and a kind is the unit of permission.* Something private from
  the other members belongs in another repository.
- **Grants are read on each request** from the membership, not copied into
  the token, so a change or a removal takes effect at once.
- **Delegation.** An agent or a function that a member starts runs with
  what both of them may do, never more. Code that runs on its own, from a
  trigger, keeps its own grant as today.

#### Where identity lives

I recommend a store on the server, outside any repository.

- Hundreds of members should not each need a full repository.
- The credential stops being a record of one repository. Today it is one,
  holding two pointers into that repository's encrypted side store
  (`internal/engine/auth.go:5-12`), so changing it writes to the
  repository's history. That is why an operator resetting a password must
  stop the server (`docs/operations.md:1934-1936`). This gain is
  structural, not a matter of security: the password hash was never in
  history.
- The sign-in name stops being the repository's name
  ([0074](../decisions/0074-the-repository-name-is-the-login-name.md)),
  which makes job 1 of F2 trivial.

The cost: an identity is no longer inside a repository's backup, so it
needs its own. The alternative is that each person's identity is a personal
repository. That keeps one concept and one backup, and costs a repository
per member.

Keep identity names in today's DNS-style form, so that another server
could vouch for one later: the person signs in at their own server, which
asserts who they are. IndieAuth and OpenID Connect are the precedents, not
a fit as they stand: IndieAuth names a person by a profile URL, and OpenID
Connect by an issuer and a subject. Two pieces exist here: the app
authorization flow
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

1. **The person on each line** (F3).
2. **Memberships and grants as records,** so a change of permission is in
   history and in the export.
3. **The person on each change event,** so a client can say who changed
   something. The feed is already cut to what a token may read.
4. **Each record would carry its creator,** taken from the line that
   created it, for the `own` grant. This is new: nothing computes it today.

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
text live, would need. Decision 0017 rules out the first and the second is
not a goal, so with one process ordering every change neither is required.

#### Sharing with someone on another server

The cheapest form uses what exists: the other person's substrate is treated
as a provider, like Google or Slack. Bo connects an account to Ada's
repository, holding a limited token, and her records are mirrored into his
by the same machinery that mirrors a calendar. Besides the package it
needs: a way to grant a person (F3); grants narrower than a kind, if she
shares less than a whole collection, which is the expensive part because
the filter must hold in every read; and the same kind on both sides (F4).

It also sharpens F6: once mirrored, her data is in his permanent history.

### Several repositories under one API

Once a person belongs to several repositories, clients and agents need to
work across them.

#### What "across repositories" can mean

- **Reads are merged, never joined** (F4).
- **Each repository has its own order.** A merged stream needs a position
  per repository.
- **A write lands in one repository.** Two histories cannot be written as
  one step, and that should never be promised.
- **A record's identity becomes repository, kind and id** (F4).
- **Kinds differ per repository,** so "all my tasks everywhere" works by
  trait.
- **Search ranks within one repository**†, so a merged search needs a rule
  for merging.

#### A library or a server API

| | Client library | Server API |
| --- | --- | --- |
| Credential | a token per repository | one sign-in |
| Change to the server | none | a new surface |
| Reach | any server | one server |
| Cost | N requests and N streams; the logic is written three times, in Swift, TypeScript and Go, unless specified once | it is the one place repositories meet, so the one place data can leak between them |
| Waits on | nothing | identities and memberships: a token belongs to one repository today |

Both, in that order. The library is the only form that crosses servers, and
it needs no change to what the server speaks. It is not free: it keeps a
local copy and a position per repository, and has to reconcile kinds that
differ. The CLI already holds several named connections, one at a time
(`cmd/substratectl/commands/config.go:20-33`).

For the server API, copy JMAP's shape: one sign-in lists the accounts a
user may use, each marked personal or read-only; every call names one
account; one stream carries a state per account. JMAP stops there; the
grants above are this design's own. Matrix is the precedent for one stream,
resumed with one token, over many rooms. Three additions, in order:

1. **A session read:** the repositories this identity may use, with its
   grants in each.
2. **A way to choose a repository.** Exchanging the sign-in for a token per
   repository, in the manner of OAuth token exchange, keeps today's rule
   that a token implies its repository. A path prefix has precedent too: a
   webhook URL already names the repository
   (`internal/api/api.go:352-353`).
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
  Existing machinery, and it crosses servers.
- **A built-in agent**, such as the assistant in a personal repository
  reading the household one. It holds one repository today
  (`internal/engine/agentloop.go:665-666`). It would need tools that take a
  repository; each repository opened as the person who asked, under what
  both the person and the agent may do; the conversation kept in the home
  repository; each repository's kinds in its instructions; and a rule for
  runs no person started. I would let an agent be a member in its own
  right.

The risk that grows is this. Text read from one repository can instruct an
agent that may write to another, so a planted instruction becomes a path
between two people's data. An agent across repositories should read by
default, with its writes held for approval.

### What could be cut

The findings say where promises are loose. This asks the other question:
what is here that 1.0 does not need. The engine is 94,476 lines.

| Area | Lines | Label | View |
| --- | --- | --- | --- |
| Measured values, aggregates, units | 19,812 | alpha | The largest subsystem: a time-series engine with a unit algebra. Reduce its reach, below |
| Functions, triggers, sandbox | 17,319 | stable (F10) | Needed. Trim what it writes to history |
| Agents and AI model adapters | 16,428 | alpha | A second product. Outside the promise |
| Packaging: catalog, publishers, app set-up, sample copying | 10,552 | mixed | Two ways to install, and the copying works against sharing (F4) |
| The on-disk copy of history | 8,460 | — | F9 |
| Changing kinds while data exists | 7,786 | — | Earns its place: this is what keeps data durable |
| Limited tokens for apps | 5,259 | alpha | Earns it, and shared repositories ride on it |
| Mapping and merging records | 4,515 | — | Earns it; 14 mappings ship |
| Search and vectors | 4,495 | beta; alpha | Computed from records; free to change |
| Applets | 3,536 | alpha | A screen runtime in the server, days old. Not core |
| MCP | 2,311 | alpha | Gives an outside assistant "everything the console does but the auth material" (`internal/scope/scope.go:37-42`) |
| The stand-in engine used by API tests | 1,900 | — | Replace, below |

Two alpha subsystems, measured values and agents, are 30% of the engine
(14,722 and 13,923 of 94,476 lines).

Candidates, in the order I would take them:

1. **Run records and delivery bookkeeping, out of permanent history** (F6).
   [0064](../decisions/0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md)
   put them there so a restored repository resumes where it was. That is
   worth keeping for positions. It is not worth keeping every run forever.
2. **The synchronous half of the on-disk copy** (F9).
3. **The stand-in engine.** The API is tested against a hand-written copy
   of the engine's 72 operations, and the tree says what that proves: "The
   fake's Put ignores IfVersion … so 'the engine returns sentinel X' and
   'the API maps sentinel X to status Y' can both pass while the engine
   returns a different sentinel for the request that matters"
   (`internal/testenv/conformance_db_test.go:5-10`). Test the API against
   the real engine.
4. **Measured values, out of every write and out of the promise.** Four of
   its checks run at the end of every write
   (`internal/engine/dataset.go:801-818`), whether or not the repository
   holds a measurement, and every repository starts with 216 units and 149
   conversions (`AGENTS.md`). Not a deletion: make it cost nothing where
   unused, and keep it alpha.
5. **Agents and applets, outside the promise.**
6. **One way to install** (decision D16).
7. **The recovery key.** Records are stored unencrypted: "the changelog
   carries every record in the clear"
   (`internal/changelogfile/fsutil.go:8-12`). The encrypted side store
   holds connected-account tokens, the password hash and the second-factor
   seed, and each can be obtained again: reconnect the account, reset the
   password. What the recovery key adds is moving hosts without doing that.
   Its costs are a step at registration, a key that cannot be replaced
   (F6), and with several owners, a key each.

## Decisions to make

Each has options and a recommendation. Where one depends on another, it
says so. "Answer" is left open to be filled in.

**Settled by the brief:** several people per repository with their own
permissions; sharing between repositories, later; one API and one agent
over several repositories; one server process per repository; tens to
hundreds of people in one repository; no enterprise access control.

### Scope

**D1. Who is 1.0 a promise to?**
- A. To anyone who builds an app or a package: the API, the dialect and the
  function interface are all stable.
- B. To people who keep data and to apps: data and API are stable;
  packages with code and third-party publishing are experimental.
- C. To people who keep data only: nothing is lost on upgrade; every
  interface may still change with notice.
- *Recommended: B.* Answer:

**D2. Which capabilities ship in 1.0, and which are only kept possible?**

| Capability | Recommended | Answer |
| --- | --- | --- |
| Several people in one repository | the model and contract in 1.0, with one member; inviting a second soon after | |
| Sharing across repositories and servers | after 1.0; reserve now (F4) | |
| One API over several repositories | after 1.0 | |
| Rename | an offline tool in 1.0 (F2 option C) | |
| Erasure on request | after 1.0; format ready in 1.0 | |
| Retention for bookkeeping kinds | in 1.0 | |
| Compaction | after 1.0 | |
| Chain or signatures | not planned; format ready in 1.0 | |

**D3. The stability policy.** Adopt
[the proposal](#a-proposed-stability-policy), or change it. Answer:

### Identity and names

**D4. Where does a person's identity live?**
- A. In a store on the server, outside any repository.
- B. Each person's identity is a personal repository.
- *Recommended: A.* Answer:

**D5. How much of the identity model does 1.0 carry?**
- A. All of it, with one member: sign-in names a person, a membership
  record exists for the creator, history lines carry the person.
- B. Reservations only: the history-line key and the `owner` rename; today's
  sign-in stays and is labelled beta.
- C. Nothing; sign-in is frozen as it is.
- *Recommended: A if D2 puts several people near 1.0, else B. Not C.*
  Answer:

**D6. What can a member's permissions say?**
- A. Owner, or a list of read and write grants over kinds, packages and
  traits, with Editor and Viewer as presets.
- B. Three fixed roles only: owner, editor, viewer.
- C. A list of people on each record.
- *Recommended: A.* Answer:

**D7. "Write only the records you created": in the first version?**
- A. Yes. B. Later.
- *Recommended: B, unless a first use needs it.* Answer:

**D8. Can part of a repository be private to one member?**
- A. No. A repository is the unit of privacy; private things go in another
  repository.
- B. A kind can be marked private to its creator.
- C. Per record.
- *Recommended: A.* Answer:

**D9. Can a repository be renamed?** (F2)
- A. Never.
- B. The sign-in name only.
- C. Also the kinds' namespace, by an offline rewrite.
- D. Also the namespace, by storing "this repository" as a placeholder.
- *Recommended: B and C for 1.0. D if D26 keeps a chain possible.* Answer:

**D10. Mint an opaque repository id and bind storage to it?**
- A. Yes, before 1.0. B. No.
- *Recommended: A.* Answer:

**D11. Default names.** Complete a short name under the configured public
host, and refuse `localhost` and IP addresses outside development?
- A. Yes. B. Keep today's behaviour.
- *Recommended: A.* Answer:

**D12. A deleted repository's name.**
- A. Free at once (today).
- B. Held for a period.
- C. Never reused.
- *Recommended: B on a server more than one person uses.* Answer:

**D13. The project's permanent name.**
- A. Keep `substrate.reamde.dev`, and write down who holds the domain.
- B. Move to another name before 1.0.
- Answer:

**D14. The word `owner`.**
- A. Rename the stored tier, for example to `person`; `owner` becomes the
  role.
- B. Keep the tier; call the role something else, such as `admin`.
- *Recommended: A.* Answer:

### Sharing

**D15. Reserve a name in a reference for "which repository"?**
- A. Yes, now, unused. B. No.
- *Recommended: A.* Answer:

**D16. How do two people's copies of a collection relate?**
- A. They do not (today). Reads across repositories go by trait.
- B. Samples install under the publisher's name, like other packages: the
  same kind everywhere, and a person's own changes become additions.
- C. Keep copying, and record on each copy which kind it came from, so
  tools can treat copies as the same sort of thing.
- *Recommended: C.* Answer:

**D17. Sharing with a person on another server.**
- A. Not planned.
- B. Through a provider package that mirrors records.
- C. A protocol of its own between servers.
- *Recommended: B, after shared repositories.* Answer:

**D18. Working across repositories.**
- A. A client library only.
- B. A library first, then a server API.
- C. A server API only.
- *Recommended: B.* Answer:

**D19. An agent across repositories.**
- A. Reads by default; writes are held for approval.
- B. Whatever the person may do.
- And: may an agent be a member of a repository in its own right?
- *Recommended: A, and yes.* Answer:

### Access

**D20. Sign-in tokens.**
- A. Never expire (today), with "sign out everywhere" added.
- B. Expire and renew.
- *Recommended: A.* Answer:

**D21. A server shared by people who do not know the operator.**
- A. Supported: requires the full sandbox, proof of a name, and a damaged
  repository set aside at start-up.
- B. Not supported in 1.x: one household per server.
- Answer:

**D22. Code and packages from outside publishers at 1.0.**
- A. Supported. B. Experimental.
- *Recommended: B.* Answer:

### History

**D23. What does delete promise?** (F6)
- A. Removal from the current state only (today), said plainly.
- B. On request, the values in history are blanked too.
- C. Chosen kinds keep their values out of history from the start.
- D. Each record's values are encrypted, and erased by destroying a key.
- *Recommended: B, with C available per kind.* Answer:

**D24. Retention.** Reserve a `retention` key on a kind, and keep run
records and delivery entries out of permanent history?
- A. Yes. B. No.
- *Recommended: A.* Answer:

**D25. Compaction.**
- A. Never: history is complete forever.
- B. An owner or operator may set how much history is kept; clients are
  told when what they ask for is gone.
- *Recommended: B, after 1.0.* Answer:

**D26. Who must be trusted about history?** (F7)
- A. The operator (today). Nothing is proven.
- B. Changes by the operator should be detectable: a chain, with clients as
  witnesses.
- C. Each person's changes should be provable: signing keys held by
  people.
- *Recommended: A for 1.0, keeping B possible.* Answer:

**D27. Change the checksum to two levels, and write the canonical form
down?** (F7)
- A. Yes, before 1.0. B. No.
- *Recommended: A. It is what keeps D23, D9 and D26 independent.* Answer:

**D28. The recovery key.**
- A. Keep, and add a way to enrol a new one.
- B. Keep, add that, and add replacing the encryption key.
- C. Remove it.
- Answer:

### Storage and operation

**D29. The on-disk copy of history.** (F9)
- A. Synchronous (today).
- B. Appended right after the commit.
- C. Archived in pieces to a directory or to S3.
- And: how many seconds of confirmed writes may be lost with Postgres, and
  do writes stop when the copy falls further behind than that?
- *Recommended: C.* Answer:

**D30. The `body` column.**
- A. Make `body` an ordinary property.
- B. Store every long-text property separately.
- C. Keep it.
- *Recommended: A.* Answer:

**D31. Dates and times.** Reserve types for a time in a named zone, a time
of day and a date with no year?
- A. Yes. B. No.
- Answer:

**D32. "Deliberately empty".** Can a property be stored as explicitly
having no value, so a sync does not fill it back?
- A. Yes. B. No (today).
- Answer:

**D33. The function interface.** (F10)
- A version key on the function, or on the package?
- Is the Python release part of the interface, and for how long is one
  supported?
- A missed schedule: run every missed time (today), only the latest, or let
  each trigger say?
- Answer:

**D34. May a publisher rename a kind?** (F11)
- A. Never: deprecate it and add another.
- B. Yes, through the same move the server's own packages use.
- *Recommended: A.* Answer:

**D35. Upgrades.** Is a one-way upgrade acceptable throughout 1.x, with the
way back being a snapshot taken before?
- A. Yes. B. No: a release must be reversible for some period.
- Answer:

**D36. Size.** What must one repository reach in 1.x? Ten million records
or 100 GB? How many people at once?
- Answer:

**D37. What is outside the promise by name:** the assistant and its
prompts, console layouts, AI model lists, vendor mappings in `core`,
measured values, applets.
- A. All outside. B. Name the exceptions.
- *Recommended: A.* Answer:

**D38. The cuts:** run records out of history; the stand-in engine
replaced; measured values out of every write.
- A. All three before 1.0. B. Name which.
- Answer:

**D39. Offline.** The apps queue writes and replay them. Is that the
permanent answer, with no merging of separate histories?
- A. Yes. B. No.
- *Recommended: A.* Answer:

## Fixes and validation

Work that follows from the findings. Each waits on the decision named
where one applies.

| Finding | Fix | Waits on |
| --- | --- | --- |
| F1 | Records on the feature list; honest labels; the Go-interface sentence removed; a test holding the guide and the console to the code's error codes; every `proposed` record with shipped code accepted or rejected; the operations guide's list of absences rewritten | D1, D3 |
| F2 | Short names completed under the configured host; an opaque repository id; the offline rename tool | D9, D10, D11 |
| F3 | `subject` in the history line's key set; the `owner` tier renamed; sign-in shaped or labelled | D5, D14 |
| F4 | A reserved name in the reference value | D15 |
| F5 | The control-kind refusals for every sort of writer; delegation limited by the caller; a fixed form for the writer-name header; sign out everywhere; every stream cut with its token; `docs/auth.md` corrected | — |
| F6 | A reserved `retention` key; run records and delivery entries out of permanent history; a way to enrol a new recovery key | D23, D24, D28 |
| F7 | The two-level checksum; the canonical form written down with test vectors | D27 |
| F8 | Unknown fields inside a line refused; the format documented; the moved-kind checks restored at `internal/engine/shippedupgrade.go:189` | D27, D29 |
| F10 | An interface version; a pinned input shape; retries over minutes for temporary failures; "best effort" refused where registration is open | D33, D21 |
| F11 | A dialect floor on a package and in a publisher's document; the server's dialect in discovery | — |
| F12 | Rows 1 to 4 | — |
| F13 | A damaged repository set aside at start-up | D21 |
| F14 | Byte-order sorting on the identity columns; `body`; the time zone check; added value lists counted as narrowings | D30 |

Validation to run before the release:

1. A directory from each release, imported under the current code (F8).
2. A start-up with one damaged repository among several: the others serve.
3. The server killed at each step of a write. Today's tests inject failures
   at marked points instead (`internal/engine/dataset.go:957-977`).
4. The table of writers against control kinds (F5).
5. A list of every route as public, owner-only or limited, where a new
   route fails until it is placed†.
6. Retry keys, guarded PATCH and refused paging tokens, over HTTP against
   the real engine. Today the stand-in holds them†.
7. Each first-party app recovering when told history is `compacted`.
8. An export restored on a fresh host with only the recovery key, if D28
   keeps it.
9. Rebuild, import and snapshot timed at a million records and 30 GB, so
   the limits are numbers.
10. A stated run of releases with no break to anything labelled stable.

## A proposed stability policy

The labels and the notice before a removal are modelled on the Kubernetes
deprecation policy; what a major version may break follows semantic
versioning.

1. **Three labels.** *Stable*: only additions within 1.x; removed only in
   2.0. *Beta*: supported; a break is announced one minor release ahead, in
   discovery and in the changelog. *Experimental*: may change or go in any
   release.
2. **Data outranks labels.** A record of any kind, an experimental
   package's included, survives every upgrade or is carried forward by a
   conversion. A feature may be withdrawn; data may not be lost.
3. **What "only additions" means, per surface.**
   - The API: an answer may gain fields, and a client ignores fields it
     does not know. A new request field comes with a feature entry that a
     client checks first, because the server refuses what it does not know.
   - The dialect: a new key raises the dialect number; a package states the
     number it needs.
   - The stored format: a new line key or effect raises the format stamp. A
     server reads every stamp at or below its own, throughout 1.x, and 2.0
     reads what the last 1.x wrote.
   - The function interface: an interface version, and a supported range of
     Python releases.
4. **Upgrading.** A 1.x server opens any database and directory written by
   the same or an earlier 1.x release, in one step. It refuses one written
   by a later release, as today. Database changes only go forward. A
   fix-up for old data is carried until 2.0.
5. **Going back.** Not supported once a newer server has stamped the data
   or changed the database. The way back is the snapshot taken before. Each
   release says whether it closes the way back.
6. **Deprecation.** Announced in discovery and the changelog. For a stable
   feature it is notice ahead of 2.0, never permission to remove it within
   1.x. A beta feature is served for at least two further minor releases
   after the notice.
7. **Outside the promise, by name.** The database tables, the Go packages,
   the CLI's table output, the built-in assistant, console layouts, model
   lists and vendor mappings.
8. **Nothing on `main` is `proposed`.** A decision record is accepted, or
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
| [0020](../decisions/0020-dialect-keys-are-reserved-not-tolerated.md) | reserving a key before it is used (F11) |
| [0022](../decisions/0022-rest-is-frozen-graphql-is-a-projection.md) | an earlier "frozen v1" (F1) |
| [0026](../decisions/0026-embedding-vectors-are-1536-wide-or-refused.md) | vector size (F13) |
| [0041](../decisions/0041-body-is-a-declarable-property-and-fts-is-per-property.md) | the `body` column (F14) |
| [0050](../decisions/0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md) | history files; why the chain was removed (F6, F7) |
| [0052](../decisions/0052-the-authority-is-the-repository-id.md) | the name as the id (F2) |
| [0054](../decisions/0054-a-repository-moves-between-host-keys-through-an-offline-rewrap.md) | changing a host's key (F6) |
| [0062](../decisions/0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md) | the synchronous copy (F9) |
| [0064](../decisions/0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md) | bookkeeping in history (What could be cut) |
| [0072](../decisions/0072-reserve-a-purged-record-id-against-reuse.md) | reuse of an id (F12) |
| [0074](../decisions/0074-the-repository-name-is-the-login-name.md) | the sign-in name (Shared repositories) |
| [0075](../decisions/0075-v1-ships-the-fs-blob-backend-alone.md) | the removed S3 store (F9) |
| [0079](../decisions/0079-graphql-is-removed-and-the-records-read-is-one-route.md) | routes removed after a freeze (F1) |
| [0080](../decisions/0080-a-kind-grant-may-glob-and-a-glob-never-reaches-auth-material.md) | what a wildcard never reaches (Shared repositories) |
| [0083](../decisions/0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md) | one process (F13) |
| [0125](../decisions/0125-a-provider-write-is-a-callable-that-writes-no-record.md) | a retried provider write is sent twice (F10) |
| [0143](../decisions/0143-a-token-has-full-access-to-its-repository-and-a-login-token-does-not-expire.md) | tokens that do not expire (F5) |
| [0165](../decisions/0165-a-commit-in-doubt-is-caught-up-by-the-next-write-and-no-write-grant-names-the-owners-auth-kinds.md) | a write whose confirmation is lost (F12) |
| [0194](../decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md) | the app flow and limited tokens (Shared repositories) |
| [0198](../decisions/0198-a-bundle-offers-its-functions-and-an-agent-takes-them-by-pattern.md) | functions offered to agents as tools |
| [0208](../decisions/0208-a-repository-proves-its-name-with-a-txt-record-after-registration.md) | DNS proof of a name (F2) |
| [0212](../decisions/0212-a-package-is-published-by-its-authoritys-document-and-installed-from-it.md) | a proposed record with shipped code (F1) |
| [0213](../decisions/0213-a-published-package-keeps-its-publishers-versions-and-an-upgrade-waits-for-every-connected-build.md) | a proposed record with shipped code (F1) |
| [0220](../decisions/0220-outside-code-is-confined-and-reviewed.md) | a proposed record with shipped code; outside code must be confined (F1, F10) |
| [0236](../decisions/0236-an-owner-may-delete-their-repository-and-the-name-is-freed.md) | a deleted name is free (F2) |
| [0245](../decisions/0245-a-stale-revision-is-a-validation-refusal-narrowed-by-the-reason-stale-revision.md) | a proposed record already on the wire (F1) |

### Other systems

Named for comparison. Each link answered on 2026-11-11, except Replicache's
own site, which answered the day before and timed out; its source is linked
beside it. `lint:docs` does not fetch them.

| System | Source | Cited for |
| --- | --- | --- |
| Kubernetes API | [API concepts](https://kubernetes.io/docs/reference/using-api/api-concepts/) | list and watch from a version; 410 Gone; one reader feeding many watchers |
| Kubernetes, SemVer | [deprecation policy](https://kubernetes.io/docs/reference/using-api/deprecation-policy/), [semver.org](https://semver.org/) | labels, notice before removal, what a major version may break |
| CouchDB | [changes feed](https://docs.couchdb.org/en/stable/api/database/changes.html), [document API](https://docs.couchdb.org/en/stable/api/document/common.html) | a sequence per database; a revision guard on a write |
| JMAP | [RFC 8620](https://www.rfc-editor.org/rfc/rfc8620): accounts (section 1.6.2), the session (2), `/changes` (5.2), `StateChange` (7.1) | one sign-in over several accounts; a state per account |
| Replicache | [how it works](https://doc.replicache.dev/concepts/how-it-works), [source](https://github.com/rocicorp/replicache) | a client queue replayed over the server's order |
| Matrix | [client-server API](https://spec.matrix.org/latest/client-server-api/), `/sync` | one stream over many rooms |
| Automerge, Yjs | [automerge.org](https://automerge.org/), [docs.yjs.dev](https://docs.yjs.dev/) | merging simultaneous edits with no server choosing the order |
| IndieAuth | [specification](https://indieauth.spec.indieweb.org/) | a person named by a URL they control |
| OpenID Connect | [core](https://openid.net/specs/openid-connect-core-1_0.html) | a server vouching for a person |
| OAuth token exchange | [RFC 8693](https://www.rfc-editor.org/rfc/rfc8693) | a sign-in exchanged for a token per repository |
| Certificate Transparency | [RFC 6962](https://www.rfc-editor.org/rfc/rfc6962) | clients as witnesses of an append-only log |
| JSON canonicalization | [RFC 8785](https://www.rfc-editor.org/rfc/rfc8785) | the standard canonical form, and why it does not fit exact numbers |
| Git | [Git objects](https://git-scm.com/book/en/v2/Git-Internals-Git-Objects) | a record that names its content by digest |
| Event sourcing patterns | [forgettable payloads](https://verraes.net/2019/05/eventsourcing-patterns-forgettable-payloads/), [throw away the key](https://verraes.net/2019/05/eventsourcing-patterns-throw-away-the-key/) | erasure options C and D (F6) |
| Postgres | [continuous archiving](https://www.postgresql.org/docs/current/continuous-archiving.html) | shipping a log after the commit |
| Postgres | [TOAST](https://www.postgresql.org/docs/current/storage-toast.html), [JSON types](https://www.postgresql.org/docs/current/datatype-json.html) | how a large value is stored and read |
| Postgres | [ALTER COLLATION](https://www.postgresql.org/docs/current/sql-altercollation.html) | an index and a changed collation version |
| Litestream | [how it works](https://litestream.io/how-it-works/) | copying a log to object storage |
| Delta Lake | [protocol](https://github.com/delta-io/delta/blob/master/PROTOCOL.md) | a log as ordered unchanging files |
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
