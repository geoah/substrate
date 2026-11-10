# Review for 1.0: architecture, data model and system design

Status: a review, written 2026-11-10 against `f26d42a6b` (v0.123.1). Finding
F11 and Part 2 were added the same day, after the owner asked what could be
cut, whether the file mirror is needed, and how shared repositories and one
API over several repositories would look. It recommends and implements
nothing, and like every page under `docs/plans/` it is not a contract: the
code that lands is.

## Contents

- [How to read it](#how-to-read-it)
- [Verdict](#verdict)
- Part 1, the review
  - [Who it is for](#who-it-is-for)
  - [The model today, and what holds it](#the-model-today-and-what-holds-it)
  - [Findings](#findings), F1 to F11
  - [Defects found in passing](#defects-found-in-passing)
  - [Direct verdicts](#direct-verdicts)
- Part 2, direction after 1.0
  - [What could be cut](#what-could-be-cut)
  - [Shared repositories](#shared-repositories)
  - [Several repositories under one API](#several-repositories-under-one-api)
  - [What Part 2 asks of 1.0](#what-part-2-asks-of-10)
- Part 3, policy and gate
  - [A proposed stability policy](#a-proposed-stability-policy)
  - [The release gate](#the-release-gate)
  - [Questions only the owner can answer](#questions-only-the-owner-can-answer)
- [References](#references)
- [Appendix: how the sizes were counted](#appendix-how-the-sizes-were-counted)

## How to read it

- **Part 1 reviews what exists. Part 2 sketches designs.** Nothing in Part 2
  is built. Where it describes today's code it cites it; where it proposes,
  it says so.
- **Citations.** Every `path:line` was opened during the review, except a
  claim marked †, which a delegated reading pass reported and I did not open
  again. Treat a † as a lead, not a result.
- **Nothing was run:** no database suite, no live test. A test named here
  exists in the tree; I did not watch it pass.
- **Sizes** are lines of Go outside `_test.go` files at the reviewed commit,
  counted with `find <dir> -maxdepth 1 -name '*.go' ! -name '*_test.go' |
  xargs cat | wc -l` over the files each row names. They say where the code
  is, not what it is worth.
- **Estimates** are marked as estimates and give their basis.
- **Other systems** are named for comparison and linked under
  [References](#references). I describe them from their specifications; none
  was tested against this code.
- A finding has a **kind**: *demonstrated* (the code path shows it), *risk*
  (plausible, not shown), *judgment* or *question*. It has a **class**: *fix
  before 1.0*, *decide and document*, *safe to evolve later* or *remove*.
- **An independent review** by a second model (`gpt-6-astra`) read this
  page on 2026-11-10 and its thirteen findings are applied. It opened 139
  citations: 129 supported their sentence, and the ten that did not are
  corrected. It re-checked fourteen dagger claims: twelve held, one was
  wrong and one half wrong. Both are corrected, and a claim I then opened
  myself has lost its dagger. Like this page, that review ran nothing.

## Verdict

**Blocked today by a short list, none of it a redesign. Behind that list:
ready with explicit limits.**

The core is sound and should be kept as it is: a changelog of value-carrying
deltas with one fold path, seq order equal to visibility order, isolation
held by Postgres row level security, a closed declaration dialect whose
conversions are ordinary record writes, and a scoped surface that fails to
compile when a new verb is not decided. Most of what follows is about the
promises around that core, not the core.

Four things stand between it and a 1.0:

1. **Nothing says what 1.0 promises, and `stable` has not meant stable.**
   28 of the 84 releases since v0.70.0 carry a BREAKING section, the latest
   two days before this review, inside a feature stamped "frozen for v1"
   (F1).
2. **Six things are far cheaper to settle before 1.0 than after:** the
   repository's identity (F2), a pinned stored format (F3), a version on the
   function body ABI (F5), a dialect floor on a package (F6), the collation
   of the identity columns and the `body` property (both F10).
3. **Three deferred decisions become promises by silence:** what deleting
   means and how history ever shrinks (F4), whether every writer is held to
   the same authority (F7), and a handful of API margins (F8).
4. **One piece of built machinery deserves a decision before it is
   promised:** the synchronous file mirror of the changelog (F11).

The limits a 1.0 can state and keep: one person per repository, one server
process per database, Linux for confined code, no erasure yet, and agents,
MCP, applets and third-party code as experimental surfaces over durable
records.

**After 1.0**, shared repositories and one API over several repositories fit
without changing the changelog's shape (Part 2). They need an identity that
is separate from the repository, one enforcement path, and eight things
settled now ([the list](#what-part-2-asks-of-10)).

[The release gate](#the-release-gate) puts every decision, fix and
validation this page asks for in one list.

## Part 1: the review

### Who it is for

The README states the product: "a private home for everything one person
keeps", run "on a machine you choose", by yourself or by "someone you trust".

| | |
| --- | --- |
| **Users** | One person per repository. An operator, who is often the same person. App authors, through scoped tokens. Package publishers, through authority documents. Assistants, through MCP. |
| **Workloads** | Hand-written records beside provider mirrors (mail, calendar, messages, health samples); a console, four apps, a CLI; a few agents and scheduled functions. Observed, on the one deployment: a repository of 620k records (`internal/engine/migrations/0018_records_matching_kinds.up.sql:10`), and a 27 GB history on 2026-10-06, eight weeks after the first tag (`internal/engine/segmentdigest.go:15-18`). |
| **Non-goals, as written** | "No sharing, no second user reading your repository, no cross-repository query. No erasure, compaction, or retention policy … No signatures and no hash chain … Each of those is a deliberate absence" (`docs/operations.md:1963-1969`). One replica (`docs/operations.md:1100-1103`). No offline multi-writer ([0017](../decisions/0017-the-changelog-is-one-writers-total-order.md)). |

#### What one user per repository costs

It buys a great deal: no access-control lists, one isolation predicate
(`repository`) that Postgres enforces, a tier model with three values, and an
auth model a person can hold in their head.

It costs three things, none of which needs enterprise scale to be felt:

- **A household cannot share a collection.** Two people who keep one
  calendar or one recipe box keep two copies, and no reference crosses a
  repository. Each person's provider sync runs separately.
- **There is already more than one principal, and each is modelled in a
  different place.** A scoped app token is held by the API's
  `scopedDataset`; an agent by `agentWriteDataset`; a function's effects by
  its emit list; an assistant's MCP token by a fourth wrapper. "No roles"
  is true of people and false of writers, and the writers do not protect
  the same kinds (F7). This is the cost that matters now.
- **A second person later is a changelog dialect event, not a migration.**
  Stored data names doors, not people: the credential's id is the literal
  `self` (`internal/engine/auth.go:52`), a token record declares no subject
  (`kinds/substrate.reamde.dev/core/token.yaml`), and the stored tier
  `owner` means "the human hand"
  (`internal/substrate/tier.go:15-19`). Adding a subject is additive,
  because the line's key set is closed and gated
  (`internal/changelogfile/entry.go:179-180`), so an older binary refuses
  rather than misreads.

Verdict: keep it for 1.0. The owner has since said shared repositories are
wanted after 1.0. [Shared repositories](#shared-repositories) is that
design, and it treats today's model as its special case.

### The model today, and what holds it

| Invariant | Where it is enforced | Held by |
| --- | --- | --- |
| One path from changelog to `records`; a rebuild reproduces the fold | `internal/engine/fold.go`, `internal/engine/rebuild.go:121-128` | `foldpath_internal_test.go`, `rebuild_db_test.go` |
| A write is durable in the segment before Postgres commits; an unfinished transaction is cut whole | `internal/engine/dataset.go:853-1013` | `internal/changelogfile/txn_test.go`, `durable_ack_db_test.go`, `repodir_db_test.go` |
| Seq order is visibility order, so a cursor skips nothing | changelog lock taken at transaction start, `internal/engine/dataset.go:784-797` | `TestChangesCursorWalkSkipsNoSeqUnderConcurrentWriters` |
| One writer per repository | `flock` on the directory (`internal/changelogfile/lock.go:35-48`), a session advisory lease (`internal/engine/writerlease.go:33-38`) | `writerlease_db_test.go` |
| A repository reads only its own rows | `FORCE ROW LEVEL SECURITY` on every scoped table, `internal/engine/migrations/0001_init.up.sql:551-577` | `isolation_db_test.go` |
| An app's token reaches only what was decided for it | two route groups (`internal/api/api.go:465-466,542-543`) and a dataset that embeds nothing (`internal/api/scoped.go:19-40`) | the compiler; fake-backed API tests |
| Secret material never enters the changelog | a secret is stored as a ref into the sealed store (`internal/engine/credentials.go:458-466`, `internal/engine/auth.go:8-12`) | `sealing_db_test.go`† |
| An older binary refuses a newer store | migration hashes, two dialect stamps (`internal/engine/dialect.go:41-45`) | `migrate_db_test.go`† |
| A declaration's key set is closed; a narrowing is refused while live rows hold the old shape | `internal/vocabulary/load.go`†, `internal/engine/schemadiff.go` | `dialect_test.go`, `upgrade_guard_db_test.go`† |
| Wire field names match the console's mirror; every error code has a real-engine case | `internal/substrate/wire_test.go`, `internal/testenv/conformance_db_test.go` | `TestWireGolden`, `TestPublishedErrorCodesEndToEnd` |

What people would come to depend on, and what each should be at 1.0:

| Surface | Depended on by | Today | Recommended at 1.0 |
| --- | --- | --- | --- |
| Record path, the five mutations, the records list, the error envelope | every client | served; no discovery entry | **stable**, as a named `records` feature |
| Change feed and watch | apps, console, triggers | `stable` | stable, after F8 |
| Search; aggregate, window, units | console, apps | `beta`; `alpha` | as stamped |
| The auth doors (`/register`, `/login`, `/tokens`, `/password`, `/totp`) | every client, store-shipped apps included | outside `/api/v1`, unlisted | stable, listed |
| App authorization (`/oauth/*`, scopes) | third-party apps | `alpha` | beta, or 1.0 says third-party apps are experimental |
| Webhook URL shape | outside services | unlisted | stable: it is registered elsewhere |
| Declaration dialect | package authors, every stored repository | closed; vocabulary dialect 2 | stable, with a package floor (F6) |
| Function body ABI | package authors | unversioned; `functions: stable` covers its REST verbs | beta until versioned (F5) |
| Agents, MCP tools, applets | owner, assistants | `alpha` | experimental; their records are still durable |
| Segment lines, fold effects, manifest, export tar | every backup, every exit | "a storage change and not an API change" (`docs/changelog.md:55-61`); `export: beta` | public and documented; read by every later 1.x, and by 2.0 from the last 1.x (F3) |
| Postgres tables | nobody | internal | internal; say so |
| `internal/substrate` interfaces | this module | "what v1 freezes" (`internal/substrate/dataset.go:19-24`) | internal; remove the sentence |
| `substratectl` | operators, scripts | no statement; the config file has no version (`cmd/substratectl/commands/config.go:20-33`) | `-o json` and `-o yaml` follow the wire; table output and the operator hat are not promised |
| Authority document, `_substrate` TXT | publishers | [0209](../decisions/0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md) accepted, [0212](../decisions/0212-a-package-is-published-by-its-authoritys-document-and-installed-from-it.md) proposed | experimental until F2's continuity question is answered |

### Findings

F1 to F10 are in priority order, and F1 prices every other one: the "later"
cost of each is whatever 1.0 turns out to promise. F11 was added after the
review and is placed last, not ranked.

| | Finding | Class |
| --- | --- | --- |
| [F1](#f1-v1-has-no-scope-and-stable-has-not-meant-stable) | "v1" has no scope, and `stable` has not meant stable | fix before 1.0 |
| [F2](#f2-names-are-forever-the-repositorys-and-the-projects) | Names are forever | fix two parts before 1.0; decide the rest |
| [F3](#f3-the-stored-format-is-the-exit-and-the-upgrade-path-and-nothing-pins-it) | Nothing pins the stored format | fix before 1.0 |
| [F4](#f4-deleting-does-not-erase-history-only-grows-and-keys-never-rotate) | Deleting does not erase; keys never rotate | decide and document |
| [F5](#f5-the-function-body-abi-is-unversioned-and-under-isolated) | The function body ABI | fix before 1.0 |
| [F6](#f6-a-package-cannot-say-which-dialect-it-needs-and-only-the-binary-can-rename-a-kind) | No dialect floor on a package | fix the floor before 1.0 |
| [F7](#f7-authority-is-enforced-per-writer-class-and-the-classes-protect-different-kinds) | Authority differs by writer class | fix before 1.0 |
| [F8](#f8-api-margins-that-would-freeze-as-they-are) | API margins | mostly fix before 1.0 |
| [F9](#f9-operating-limits-one-process-one-boot-shared-tables) | Operating limits | decide and document |
| [F10](#f10-value-semantics-and-storage-details-that-freeze-into-data) | Value semantics and storage details | mixed |
| [F11](#f11-the-synchronous-file-mirror-costs-more-than-it-buys) | The synchronous file mirror | decide before 1.0 |

#### F1. "v1" has no scope, and `stable` has not meant stable

*Demonstrated. Fix before 1.0.*

**Evidence.**

- `stable` is defined as "frozen for v1. Changes are additive only"
  (`internal/substrate/stability.go:8-12`), and `triggers`, `functions`,
  `bundles`, `blobs` and `changefeed` carry it
  (`internal/api/discovery.go:192-206`). No entry covers records, the record
  path, the mutations, pagination, errors or the auth doors. The surface a
  third party uses, `apps`, is alpha (`internal/api/discovery.go:227-231`).
- 176 tags in 89 days. `CHANGELOG.md` lists 84 releases and 28 BREAKING
  sections. v0.121.0 (2026-11-08) changed the catalog's `tier` value on the
  wire, under `bundles: stable`. v0.112.0 made `trigger.source` required,
  with "an older binary cannot open the upgraded core", under
  `triggers: stable`.
- [0022](../decisions/0022-rest-is-frozen-graphql-is-a-projection.md)
  declared REST "the frozen v1 contract" on 2026-08-17 and was superseded
  three weeks later. Its successor stood for four days before
  [0079](../decisions/0079-graphql-is-removed-and-the-records-read-is-one-route.md)
  removed GraphQL and the per-kind routes.
- `internal/substrate/dataset.go:19-24` says the whole `Dataset` interface
  "is what v1 freezes", citing a superseded record, for a package nothing
  outside this module can import.
- The error set is "closed" at 15 constants (`internal/api/errors.go:25-57`);
  `docs/api.md:2500` says thirteen; the console's union lacks `parked` and
  carries `unsupported`, which no handler emits
  (`web/console/src/lib/api/types.ts:12-27`).
- 56 of 241 decision records are `proposed`, and several describe code on
  `main`: 0212; 0213, whose held releases are in the refresh
  (`internal/catalog/refresh.go:99-103`); 0220, whose confinement rule is in
  `internal/engine/outsidecode.go:31-46`; and
  [0245](../decisions/0245-a-stale-revision-is-a-validation-refusal-narrowed-by-the-reason-stale-revision.md),
  whose `stale_revision` is already on the wire
  (`internal/substrate/errors.go:139`). Four tests that accepted records
  0050, 0052 and 0062 name as their confirmation are no longer in the tree.

**Scenario.** An app author reads discovery. The route they read records
through is not listed, the flow they sign in through is alpha, and the
documented error set is short by two. They ship to a store against 1.0. A
1.1 renames an enum value in a stable feature with a correct
`BREAKING CHANGE:` footer, as v0.121.0 did. The Apple console app is in the
same position: it "carries its own copy of `web/console/dist`"
(`apps/README.md:13-17`), and the only guard is a minimum server version.

**Recommendation.** Adopt a written policy (proposed in Part 3). Re-stamp
honestly: add `records` as stable, demote `functions` until F5, decide
`apps`. Delete the interface-freeze sentence. Hold `docs/api.md` and the
console's union to the code's error set with a test. Accept or reject every
`proposed` record whose code is on `main`. Then run a soak (the gate).

**Cost.** Now: a page, a test, a few stamps and a quiet period. Later: each
break lands on clients the project does not ship.

#### F2. Names are forever: the repository's and the project's

*Demonstrated (a, b); risk (c, d). Fix before 1.0 for (a) and the seam in
(b); decide and document the rest.*

**(a) The default name is taken from the request's `Host` header.** A bare
label is completed under `r.Host` (`internal/api/auth_endpoints.go:84-86`,
`internal/vocabulary/naming.go:215-231`), and `SUBSTRATE_PUBLIC_URL` is not
consulted. A test pins `127.0.0.1:5173` to `ada.127.0.0.1`, and
`ada.localhost` is a valid authority
(`internal/vocabulary/vocabulary_test.go:3615-3631`). Someone who registers
`ada` through `docker compose up` owns `ada.localhost` permanently: it is
the directory, the primary key, and the first segment of every kind they
import. Moving the server behind a real hostname changes what a bare `ada`
resolves to, so only the full old name signs in.

**(b) The name is the id, in every stored line, and nothing is relative.**
`repositories` carries `CHECK (id = authority)`
(`internal/engine/migrations/0001_init.up.sql:62-69`); the manifest must name its directory
(`internal/changelogfile/manifest.go:139-148`);
the DEK wrap binds `dek\0<authority>` (`internal/engine/dek.go:38-44`);
imported samples are rehomed, so the user's own kinds and every reference to
them carry the name; a token stores its scopes with `~` resolved
(`internal/scope/scope.go:9-11`).
[0052](../decisions/0052-the-authority-is-the-repository-id.md) priced this
itself: "the authority is now truly permanent … a typo at registration is
forever", with a reopen trigger of "domain verification binding the
authority to something the server can check". That landed as
[0208](../decisions/0208-a-repository-proves-its-name-with-a-txt-record-after-registration.md)
and the question was not reopened. One move has already been paid for by
hand: 2,380 records of one package went between authorities with a script
(`docs/plans/authorities/status.md:52-55`).

A smaller change cannot deliver rename. The name is inside every changelog
line, so a rename either rewrites history, which the per-line checksum
permits mechanically and the "a finished segment never changes" rule
forbids, or moves every kind forward, which `move.go` does only for seeded
packages at boot.

**(c) Name reuse.** A deleted name is free at once
([0236](../decisions/0236-an-owner-may-delete-their-repository-and-the-name-is-freed.md)),
and DNS proof is optional and read by nothing but its own facility: outside
tests, `domainVerifiedAt` appears only in the three `domain.go` files and
the wire struct.
Registration refuses only names under `substrate.reamde.dev`
(`internal/engine/engine.go:1779-1792`). On a shared server any registrant
may take `google.com`, or the name of a repository deleted yesterday, and
with it the path third parties still post webhooks to. Old tokens and
cursors do not carry over†.

**(d) The project's own name is permanent data and a live trust root.**
`substrate.reamde.dev` is in every repository's seeded kinds, references,
actors and sealed bindings. The file that declares it still says "the name
is a PLACEHOLDER" (`kinds/substrate.reamde.dev/authority.yaml:5`), while
packages the binary does not ship resolve through its DNS
(`internal/catalog/published.go:62-80`). Changing it later is a `movedFrom`
per kind, with the old kinds declared forever
(`kinds/substrate.reamde.dev/core/core.yaml:102-115` is the precedent).

For any publisher, trust is a TXT record and an HTTPS document read again
each time, and an additive release installs unattended while the document's
URL is unchanged (`internal/catalog/refresh.go:76-113`). A lapsed domain's
next owner inherits that. 0209 defers telling the two apart.

**Recommendation.**

1. Complete a bare label under the configured public host, and refuse an IP
   literal or `localhost` outside the dev flag.
2. Mint an immutable opaque id into the manifest and the row now, keyed by
   nothing, and stop promising `id == authority` on the wire. That is the
   whole seam; it costs a manifest key now and a format event later.
3. Say plainly whether rename exists in 1.x. My recommendation: not in 1.x,
   and by forward move when it comes.
4. Decide the project's permanent name and who holds its DNS. Pin
   first-party trust in the binary, and a first-seen document digest or key
   on each published package row.
5. On a server with more than one person, hold a deleted name for a period
   and require proof for names outside the server's host.

**Cost.** Now: a naming rule, a manifest key and a column; unestimated,
but each touches one place. Later: every repository carries a name nobody
can
correct, and the id seam needs the dialect event anyway.

#### F3. The stored format is the exit and the upgrade path, and nothing pins it

*Risk, with demonstrated gaps. Fix before 1.0.*

**Evidence.**

- The README promises "you can download everything, as one file". That file
  is the repository directory, and `docs/changelog.md:55-61` puts its
  effects outside the contract: "a change to how the fold spells an effect
  is a storage change and not an API change".
- No data root from any release is in the tree: `git ls-files` holds no
  `.ndjson`. `TestEveryMigrationLeavesAFoldTheRebuildReproduces`
  (`internal/engine/migrationrefold_db_test.go:113`) seeds its history with
  the current binary, so it proves a SQL migration leaves fold rows alone,
  not that an old changelog replays.
- A line refuses an unknown key (`internal/changelogfile/entry.go:179-180`);
  the effects inside its payload do not (`internal/engine/fold.go:1380-1384`
  decodes without refusing unknown fields). A forgotten dialect bump is
  silent data loss on an older binary.
- A replay recounts `version` and never checks it against the stored one
  (`internal/engine/fold.go:200-210`).
- The checksum's canonical form is "encoding/json's one policy" for strings
  (`internal/changelogfile/entry.go:247-251`), written down nowhere else.
- Rollback closes once a newer binary stamps a dialect or applies a
  migration (`docs/operations.md:1253-1261`). Floors already exist: a database from before v0.70.0 cannot upgrade in place
  (`CHANGELOG.md:1436-1452`). Repository migrations run again on every
  import (`internal/engine/repomigrate.go:36-39`), so each is carried for
  as long as old data roots are accepted.
- The boot upgrade holds a defect no test catches:
  `internal/engine/shippedupgrade.go:182` appends the kind-move guards to
  `st.refused`, and line 189 assigns over them.

**Scenario.** A 1.4 binary imports a directory exported under 1.0. The only
evidence that works is history the 1.4 binary wrote for itself.

**Recommendation.** At each release, generate and commit a small data root
that covers every op, a merge and split, a purge, a conversion, a sealed
value and a blob, with its expected fold. Import every released fixture
under the current binary and compare. Decode effects strictly. Document the
format as the export's contract: line keys, canonical form, manifest,
effect kinds. State the upgrade floor and how long a repository migration
is carried. None of this depends on F11: the format stays whichever way the
directory is written.

**Cost.** Now: one fixture generator and one test. Later: the first replay
that diverges is found by someone restoring a backup.

#### F4. Deleting does not erase, history only grows, and keys never rotate

*Demonstrated. Decide and document before 1.0; the mechanisms are safe to
evolve later.*

**Evidence.**

- A purge deletes fold rows and nothing else
  (`internal/engine/rows.go:738-767`). The segments and the `changelog`
  table keep every value, and the change feed derives before and after
  values from entries alone (`internal/engine/changevalues.go:13-20`).
- Every write is stored three times: the segment line, the `changelog`
  row with its full `payload` (`internal/engine/migrations/0001_init.up.sql:257-280`) and the fold.
  The horizon is the constant 0 (`internal/api/watch.go:42-48`).
- Positions escape into data: `causedBy`, a message's `changes`, trigger
  cursors. 0017 names this itself, so any compaction must move a horizon
  and never renumber.
- The recovery key has one writer, at registration
  (`internal/engine/auth.go:455`). Nothing rotates the DEK: `newDEK` has one
  caller, repository creation (`internal/engine/engine.go:1638`). Host-key
  rotation is an offline rewrap
  ([0054](../decisions/0054-a-repository-moves-between-host-keys-through-an-offline-rewrap.md)).

**Scenario.** A person syncs mail and health data for three years, deletes
one message and purges it. It remains in the segment files, the table,
every export and every backup, and `values=1` on the change feed still
serves it. If someone else hosts them, the operator cannot honour a request
to remove one item short of deleting the repository. Separately, a leaked
recovery identity opens every backup ever taken, and cannot be replaced.

**What is already in place.** The docs call each absence deliberate, and
the wire has the escape: a `compacted` refusal that names its replacement,
and a horizon in discovery that is "policy, not a wire guarantee". New line
shapes are gated by the changelog dialect. Dropping the hash chain
([0050](../decisions/0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md))
is what makes redaction mechanically possible: a line and its `sum` can be
rewritten together.

**Options.** (A) Keep "no erasure" and say so wherever the product says
delete. (B) Redaction: an op that blanks the values of one record's earlier
entries in place, keeping seq, kind, id and time. (C) Checkpoint compaction:
a fold snapshot at seq N becomes the base, earlier segments go, the horizon
becomes N.

**Recommendation.** Decide now that 1.x does (B) for erasure and then (C)
for growth, and word the promise to match: a purge removes a record from
the fold at once, its history goes by redaction, and a client must handle
`compacted`. Prove today that the first-party apps survive a horizon above
0, since the contract already demands it. Measure bytes per kind on the
large repository before deciding whether run records belong in permanent
history ([What could be cut](#what-could-be-cut) proposes they do not).

On keys, loss and compromise are different problems. For a *lost* recovery
key, add re-enrolment under both factors: one more `recoverykey` write, and
F2's portability depends on it. That does nothing for a *leaked* one. The
old wrap stays in history and opens the same DEK
(`internal/engine/rewrap.go:3-10`), so compromise needs the DEK rotated and
the sealed store encrypted again, and even that cannot reach a copy already
taken. The other answer is to remove the recovery key, which Part 2 prices.

**Cost.** Now: a decision, a paragraph and one drill. Later: the same
mechanisms, plus every client that never met a `compacted`.

#### F5. The function body ABI is unversioned and under-isolated

*Demonstrated. Fix before 1.0: a version, an honest stamp, safer defaults.*

**Evidence.**

- `runtime:` is an unversioned enum, `python` or `host`
  (`internal/vocabulary/function.go:34-62`). The interpreter is pinned only
  in the image, `python3~3.14` (`Dockerfile:190-195`). The wire between
  server and body has a version that "nothing at runtime compares"
  (`internal/runner/protocol.go:77-83`), which is fine for the wire and
  leaves the author-facing surface, `main(input, host)`, with none.
- A bundle header has no key for a minimum server or SDK
  (`internal/vocabulary/bundle.go:275-279`).
- The sandbox default is `best-effort` (`internal/sandbox/sandbox.go:92-102`)
  and off Linux nothing confines a body
  (`internal/sandbox/sandbox_other.go:10-21`). That holds for code the
  owner writes and code the binary ships. Code from an outside authority is
  already refused unless the host confines it fully, short of an insecure
  override (`internal/engine/outsidecode.go:31-46`,
  `internal/sandbox/sandbox.go:484-486`). For every body there is no memory
  or process ceiling: one that reaches the container's limit "takes the
  substrate down with it" (`docs/functions.md:563-568`).
- Every function of a bundle receives every account's live token
  (`internal/engine/invocationconfig.go:141-178`).
- A delivery gets three tries, 25 ms and then 100 ms apart, and then parks
  (`internal/engine/functions.go:57,67`), and a parked record or webhook
  delivery then waits for a person†. A schedule fires every occurrence it
  missed, however long the gap
  (`internal/engine/schedulecursor.go:62-66`).
- A body is handed a stable key per delivery
  (`internal/engine/functions.go:3063-3072`), so exactly-once toward an
  outside service is the body's job.
  [0125](../decisions/0125-a-provider-write-is-a-callable-that-writes-no-record.md)
  records the consequence for provider writes: a retried call posts twice.
  Under `kinds/` the key is named only in two comments.

**Scenario.** The image moves to the next Python minor. A third-party body
that imported a module the new minor dropped stops preparing, and its
deliveries park. The package had no way to say what it needed, and its
owner learns from a parked row.

**Recommendation.** Put an SDK version on the function or the bundle, 1 by
default, refused when unknown. Make the Python minor part of the ABI with a
support window. Hold the shape of `input` and `config` with a golden file,
as the wire is held. Stamp `functions` beta until then. Before the delivery
semantics freeze, separate a transient failure from a deterministic one and
back the transient off for minutes, not milliseconds. Refuse `best-effort`
at boot where registration is open to anyone but the operator: outside
code is already held to full confinement, and a registrant's own code is
not.

**Cost.** Now: one reserved key and a golden test. Later: the key is a
dialect event across every server a package is installed on.

#### F6. A package cannot say which dialect it needs, and only the binary can rename a kind

*Demonstrated. Fix the floor before 1.0; decide and document the rename.*

**Evidence.**
[0020](../decisions/0020-dialect-keys-are-reserved-not-tolerated.md) calls a
new dialect key "an ecosystem event" because "bundles are shared across
servers". Yet "no dialect is on the wire" (`internal/engine/dialect.go:41-45`),
a package header cannot state one, and the resolver picks the newest listed
version and fails if that closure does not parse
(`internal/catalog/published.go:94-112`). One release that uses a new key
makes the package unavailable to every older server, with no fallback to a
version they can read. `movedFrom` is performed only at boot for seeded
packages; the apply door stores it and refuses
(`internal/engine/vocabularywrite.go:1156-1159`).

**Scenario.** A publisher supports people on 1.0 and 1.3. They adopt a key
1.3 added. Every 1.0 server now answers "not published" for a package it
installed last month.

**Recommendation.** Add a minimum vocabulary dialect to the package header
and to each version entry of an authority document, serve the server's
maximum in discovery, and resolve to the newest version a server can read.
Reserve the key now, inert, in the way 0020 prescribes. Say whether a
publisher may ever rename a kind. If not, "a kind's name is forever:
deprecate and add" is a fair rule, written down.

Related, not re-verified: a change to one package is not checked against
the packages that pin, bind or require it, and uninstall has no dependents
check†. If so, the dependent package is quarantined at the next open and
its records become unreadable until it is admitted again. Worth one test.

#### F7. Authority is enforced per writer class, and the classes protect different kinds

*Demonstrated by reading; no test exercises it. Fix before 1.0.*

**Evidence.**

- `internal/engine/policy.go:23-26` says "bundle-tier actors cannot write
  the policy kind at all". The refusal exists for agents only
  (`internal/engine/agentwrite.go:185-188`). A function's effects are held
  by `emitAllows` alone (`internal/engine/effects.go:134,255-261`), whose
  only carve-out is the four auth kinds
  (`internal/vocabulary/function.go:344-361,385-390`). `trigger` and
  `recordpatchpolicy` are not system kinds
  (`internal/engine/dataset.go:60-69`).
- So a function whose `writes` names or globs over `core` may write a
  policy, or a trigger. The rule that a shipped webhook needs a key looks
  only for `bundle:` hands (`internal/engine/triggers.go:697-765`), which a
  `function:` actor is not. The defence today is the owner reading the
  declared grants at install.
- An app's token reaches an agent when its read scopes cover what the agent
  reads (`internal/api/scopedagents.go:663-670`). Nothing there bounds the
  agent's writes by the app's write scopes.
- A full token mints another with no factor
  (`internal/api/api.go:340-345`). A password change revokes nothing
  (`internal/engine/auth.go:645-672`). A sign-in token does not expire
  ([0143](../decisions/0143-a-token-has-full-access-to-its-repository-and-a-login-token-does-not-expire.md)).
  `docs/auth.md:308-311` says the factor rule keeps a leaked token from
  outliving its revocation; `SECURITY.md:72-77` concedes the mint.
- Only a scoped token's stream is re-checked against revocation and expiry
  (`internal/api/auth.go:144-170`); a full token's watch runs on.
- A full token may claim any unreserved actor string
  (`internal/api/auth.go:180-190`), and tiers and shipped-record refresh
  read the actor†. "Attribution, never authorization" (`docs/terms.md:51`)
  describes the intent, not the code.

**Recommendation.** One list of control kinds (triggers, policies, provider
rows, settings and secrets, applets, declarations) and one table of what
each writer class may do to each, held by a matrix test. Bound an agent a
caller invokes by the caller's grant as well as its own. Close the actor
header to a grammar. Add "sign out everywhere" and offer it on a password
change. Lease every stream. Correct `docs/auth.md`.

**Cost.** Now: a table, a test, and the refusals the test will demand, at
least the policy and trigger kinds for a function's effects and the bound
on a delegated agent. Later: the gap is found by the first published
package that reaches for it. Shared repositories raise the price
again, because a member is a principal with less than full access
([Shared repositories](#shared-repositories)).

#### F8. API margins that would freeze as they are

*Demonstrated unless marked. Mostly fix before 1.0; each is contained to
one handler or one rule.*

| # | What | Evidence | Recommendation |
| --- | --- | --- | --- |
| 1 | PUT and PATCH ignore query parameters, so `?ifVersion=3` writes unguarded, while DELETE takes its guard exactly there | `internal/api/rest.go:122-167` against `174-195`; the stated rule is "never silence" (`internal/api/rest.go:218-220`) | refuse by name, as the reads do |
| 2 | A failed write's status does not tell its fate. A commit in doubt answers the commit's own error, a 500†, and may have landed; a mirror failure after commit answers 503 with `Retry-After` and has landed | `internal/engine/dataset.go:876-885,979-1001`; [0165](../decisions/0165-a-commit-in-doubt-is-caught-up-by-the-next-write-and-no-write-grant-names-the-owners-auth-kinds.md) | give both a `reason`; accept `Idempotency-Key` on PUT, PATCH and DELETE, which today take none (`internal/api/core.go:32-38`). F11 would remove the second state; the first is Postgres's own and stays |
| 3 | "Start the walk again" has two answers: 410 for another history generation, 422 when the order's rendered text changed, which a vocabulary install or an upgrade can cause mid-walk† | `internal/engine/query.go:616-628` | one code, and a version inside the cursor |
| 4 | Requests are decoded strictly, so every new optional request field is a refusal on an older server, and the only capability signal is the feature list | `internal/api/core.go`†, `internal/strictjson`† | keep strictness; state that a client must ignore unknown response keys and gate new request fields on a feature entry |
| 5 | The doors apps sign in through are unversioned and unlisted | route table, `internal/api/api.go`† | list them; promise them |
| 6 | `ifVersion` can pass after a purge and re-create, because `version` restarts at 1 | [0072](../decisions/0072-reserve-a-purged-record-id-against-reuse.md) | 0072's reasoning holds for provider ids; document it, and have clients compare `createdAt` |
| 7 | No multi-record atomic write for a client, though the apps keep batches that "stand or fall together" (`docs/terms.md:174`) | — | *safe to evolve later*: additive |
| 8 | No request deadline and no rate limit on `/api/v1` | `cmd/substrated/listen.go:54-61`; limits only on the auth doors† | operational; decide and document |

What a client may conclude today:

| After | It may conclude |
| --- | --- |
| A timeout on a PUT at its own id, no `ifVersion` | Retry. A merge that changes nothing appends nothing. |
| A timeout on a write under `ifVersion` | Nothing. A retry's 409 may be its own first attempt. Read and compare. |
| A timeout on `POST /records`, a call, a merge or a split, with a key | Retry with the same key within 24 hours (`internal/engine/idempotency.go:89`). A restore from the data root alone forgets every key. |
| A 500 on a write | It may have landed. |
| A watch reconnect | Ordered, gapless, at least once from the saved seq under the same generation; a 410 means list again. A database restored from a dump keeps its generation unless the operator rotates it (`internal/engine/historygeneration.go:8-15`), and resumes silently wrong. |

#### F9. Operating limits: one process, one boot, shared tables

*Fits the intended workload. Decide and document; isolate the boot before
recommending a shared server.*

- **One process per database.** A second writer is refused
  ([0083](../decisions/0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md)),
  so there is no replica and every upgrade is downtime. Right for one person;
  say it is the 1.x shape.
- **One damaged repository refuses the whole boot.** The boot check returns
  on the first repository that errors
  (`internal/engine/repodir.go:256-262`), by stated intent. After boot,
  failure is isolated per repository and per package†. For a household
  server this is the wrong way round: quarantine the one, serve the rest.
- **A write serializes from its start**, not from its first append
  (`internal/engine/dataset.go:784-797`), then pays two fsyncs and a commit with no group
  commit. A sync page holds the lock for its whole apply; owner writes jump
  the queue, but "priority never shortens the transaction already holding
  the lock" (`internal/engine/writegate.go:34-41`). Throughput
  under concurrent writers is unmeasured. The one write benchmark is for
  observed values, a put at a time
  (`internal/engine/observed_bench_db_test.go:136`,
  `docs/plans/observations/benchmark.md`).
- **Shared tables.** Each kind's reference properties become partial indexes
  on the one `records` table (`internal/engine/indices.go:395-401`), and
  imported samples are rehomed per repository, so the count grows with
  repositories times kinds. The only `DROP INDEX` statements are for scratch
  and replaced builds (`internal/engine/indices.go:141,224,240`). One vector index serves
  every repository
  (`internal/engine/migrations/0015_embeddings_vec_index.up.sql:76-77`).
- **Language and width.** Search stems English
  (`internal/engine/rows.go:210-212`) and vectors are 1536 wide
  ([0026](../decisions/0026-embedding-vectors-are-1536-wide-or-refused.md)).
  Both are derived and rebuildable, so both are *safe to evolve later*.

None of this is a public promise, which is the point: the tables are
internal, so a hosted service with thousands of repositories is rework
behind `internal/substrate`, not a break. It stays that way only if no
runbook teaches an operator to query the tables. Part 2 lists where one
repository with many members bends first
([Tens or hundreds of people](#tens-or-hundreds-of-people)).

**Core versus packages.** `core` currently names vendors: nine contact
mappings for Apple, Google, Slack, GitHub, Linear and Beeper
(`kinds/substrate.reamde.dev/core/contactmappings.yaml`). The seeded
assistant, its prompts, the model price list and the console's views are
product content. They should sit outside the stability promise explicitly,
with the one rule that already protects the owner: a shipped record is
never refreshed over an owner's edit.

#### F10. Value semantics and storage details that freeze into data

*Mixed; each row says which.*

| What | Evidence | Why it is expensive later | Class |
| --- | --- | --- | --- |
| `records.kind`, `id` and `title` take the database's collation; the observed index already pins `"C"` because "the database's own collation … orders case and punctuation differently from Go" | `internal/engine/migrations/0001_init.up.sql:86-106`; `internal/engine/migrations/0013_observed_index.up.sql:16-20` | the primary key and every keyset walk depend on the host's locale library, and Postgres warns that an index built under one collation version can be wrong under the next; repinning rebuilds the largest index | fix before 1.0 |
| A property named `body` is stored in its own column, and no other long text is | `internal/engine/fold.go:131-132`; [0041](../decisions/0041-body-is-a-declarable-property-and-fts-is-per-property.md) | see below | fix before 1.0 |
| A `datetime` is stored in UTC and its written offset is discarded; there is no zoned, time-of-day or partial-date type | `internal/engine/validate.go:526-531` | the offset cannot be recovered from history; a birthday or "08:00 wherever I am" has no honest type | decide and document; reserve the datatypes |
| `timezone` accepts whatever `time.LoadLocation` does, which includes `Local` and the empty string and depends on the host's zone data | `internal/engine/validate.go:559-562` | stored values stop meaning the same thing on another host | fix before 1.0 |
| `null` always deletes, and a clear also releases the property's manager, so a mapping may refill it and "explicitly none" cannot be stored | `internal/engine/write.go:1062-1068,1375-1386` | a cleared value that reappears after a sync reads as a bug; changing it changes every client | decide and document |
| `unique` is reserved and enforced by nothing. The docs say so ("Nothing enforces it yet"), and the code names how enforcement would arrive: a count of violating rows, then a partial unique index | `docs/vocabulary.md:853-857`; `internal/vocabulary/types.go:252-257` | little: the reservation is honest and has a route | safe to evolve later |
| A conversion rewrites at most 10,000 records by default, in one transaction (`SUBSTRATE_CONVERSION_CEILING`; 0 lifts it) | `internal/engine/convert.go:104-107`; `docs/operations.md:176` | under the default, a shipped rename on a large seeded kind is refused at every boot, leaving a new binary on old declarations | safe to evolve later; raise before it bites |
| A value set added to an existing property is not counted as a narrowing; only removed values are | `internal/engine/schemadiff.go:544` | records the declaration no longer admits are served and cannot be re-applied | fix before 1.0 |

**The `title` and `body` columns.** Both are columns of `records`, and both
are fields of every changelog delta beside the three time columns
(`internal/engine/fold.go:131-137`), so both are part of the stored format.

*`title` earns its column.* Every list row, link and search hit needs a
label without loading the kind: it is search band A
(`internal/engine/validate.go:1264-1293`), the join that names an inbound
link (`internal/engine/linkedfrom.go:64`) and an order key
(`internal/engine/query.go:985`). 179 `displayTemplate` declarations ship
for 180 kind documents. Two things to settle. The template is rendered in
one place, on the write path (`internal/engine/validate.go:1255`), and the result is stored
as a value in the entry, so a changed template reaches a record only at its
next write; I found no pass that renders stored titles again. And
[0016](../decisions/0016-a-kind-titles-itself-from-a-declared-property.md)
left a kind without a template taking a written title, which is the split
it set out to end.

*`body` does not.* It is a storage optimization selected by a property's
name. Nine properties are named `body` (six in GitHub's bundle, one each in
Linear's and Google's, one in `clinical/communication`), against 106
declarations of a `text` or `markdown` datatype under `kinds/` and
`samples/`. 0041 kept the column because "body is up to thousands of
characters of prose on the kinds that carry it" and "a hot column reads and
orders without unpacking jsonb" (line 50). That motive is real: Postgres
stores a `jsonb` value as one datum, compresses it or moves it out of line
once its row passes about 2 kB, and reads the whole datum to read one key.
But it holds for every long text, not for one name. And 0041 records the price itself: "a declared
`body` bypasses `coerceProps` … so `required`, `pattern` and `min` on a
`body` property are not enforced" (line 60).

Options: make `body` an ordinary property, which is the smallest change; or
store every `text` and `markdown` property out of line, by datatype. Either
way a delta's `body` field must be read forever for old entries, which is
why this is cheaper before 1.0 than after. The three time columns are a
different case: range reads need them indexed. They share the second write
path, though, with its own differences: a time column is truncated to
microseconds (`internal/engine/write.go:548-550`) where an ordinary
`datetime` keeps what it was given, and a changed binding is classified by
no narrowing check†. Those are worth closing with the same care.

#### F11. The synchronous file mirror costs more than it buys

*Judgment, on demonstrated costs. Added after the review. Decide before
1.0, because it sets the durability sentence operators will hold you to.*

**What it is.** Every write goes to Postgres and to a segment file before
it is acknowledged, so the directory alone restores a repository. The
transaction's lines are written and fsynced without their last newline,
Postgres commits, then the newline lands
(`internal/engine/dataset.go:853-1013`,
[0062](../decisions/0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md)).
Postgres is the commit point and serves every read. The callers that open
segment files are the boot check, `rebuild`, `verify`, `snapshot`, `rewrap`
and the export; no record read and no change-feed read touches them.

**Why it was built.** 0062's context: `docs/operations.md` "promises that
nothing in the database is needed to restore a repository, so an
acknowledged write could be absent from every backup" (line 19). Appending
after the commit had that gap. The decision closed it by making the
directory synchronous.

**What it costs.**

- Two file fsyncs on every write, under one mutex, with no group commit.
- A state only the mirror creates: a directory latched behind its table
  until a restart, which answers 503 for a write that landed (F8 row 2).
  The commit whose answer is lost is Postgres's own and exists with or
  without the mirror; the mirror adds a catch-up to it.
- A boot check over every repository that refuses the whole server on one
  damaged directory (F9).
- A digest pass over history that had to be paced. Unpaced, 27 GB took 2.3
  cores for ten minutes and parked a sync; it now hashes 8 MiB a second by
  default and pauses while a body or an agent runs
  (`internal/engine/segmentdigest.go:15-24`).
- Immutable local segments, which is what makes redaction and compaction
  awkward (F4).
- A server pinned to a local disk.
- 8,460 lines and 11,841 lines of tests. `internal/changelogfile` is 4,062;
  `repodir.go`, `snapshot.go`, `export.go`, `rewrap.go`, `verify.go`,
  `segmentdigest.go`, `scrub.go`, `checksum.go` and `changelogdialect.go`
  in the engine are 4,398.

**What it buys that nothing else does.** If Postgres loses a commit it
acknowledged, the directory still has it. Everything else the directory
gives (a per-person copy, an exit, restore without a dump, checksums) does
not need it to be synchronous.

**Options.**

| | A. Keep | B. Follower directory | C. Object-store archive |
| --- | --- | --- | --- |
| Commit | Postgres and file, together | Postgres | Postgres |
| The copy | written before the acknowledgement | appended after the commit, by the catch-up that already exists (`internal/engine/repodir.go:546`) | finished, immutable segments, blobs and sealed objects written through a small put, get, list and delete interface: a directory by default, S3-compatible as an option |
| Lost if Postgres is lost | nothing acknowledged | what the follower had not appended. A target, not a bound: under a second when healthy | what was not yet archived. A target, not a bound: seconds to minutes when healthy, and unbounded while the destination is down |
| The latched directory state | stays | goes | goes |
| A commit whose answer is lost | stays | stays | stays |
| Server state outside Postgres | data root | data root | none, with S3 |
| Erasure and compaction | hard | hard | old history can leave Postgres; a redaction replaces an object |

A portable object store has no append: S3 offers one only in one storage
class, so the prepare, commit, newline protocol cannot run against the
interface this would be written to. S3 does not simplify the mirror;
writing the copy after the commit does, and S3 then becomes a possible
destination. The
pattern is old: Postgres ships its own write-ahead log this way, Litestream
does it for SQLite, and Delta Lake keeps a table's log as ordered immutable
files in object storage.

**What would go (an estimate, not a cut list).** The code whose job is the
synchronous protocol, the stopped-server snapshot and the digest pass. In
`internal/changelogfile`: the prepare and abort halves of `writer.go`,
`lock.go`, `unread.go` and `snapshot.go`. In `internal/engine`:
`snapshot.go`, `segmentdigest.go`, the commit path in `dataset.go` and the
last 360 lines of `repodir.go`. That is about 2,000 lines, perhaps 3,000
with the reconcile cases and the sealed-file staging. The tests of exactly those
paths are about 3,500 lines (`durable_ack_db_test.go`, the snapshot and
digest suites, `txn_test.go`, `unread_test.go`, `writer_test.go`), plus part
of `repodir_db_test.go`'s 2,103. The line format, the checksums, `verify`,
import and export stay. An S3 backend adds code back:
[0075](../decisions/0075-v1-ships-the-fs-blob-backend-alone.md) removed one
that "cost 1,039 lines", 834 of them in `internal/blobbytes` including a
172-line hand-written signer, and a 135-line MinIO harness. A second
attempt should use an SDK.

**Recommendation.** C is the direction I would take, with the directory as
the default destination. It keeps the product promise (your data as a
directory you can copy and import), removes the latched state, and is the
form F4's redaction and compaction want. It is a direction, not yet a
design. Before it could replace 0062 it needs:

- a durable watermark: the last seq known to be archived, with its blobs
  and sealed objects written before the segment that names them;
- lag that is measured and reported;
- a stated answer when lag passes the promised bound: refuse writes, or
  keep serving and alert. That choice is the durability promise;
- a superseding record, and `docs/operations.md` no longer saying nothing
  in the database is needed.

**Cost.** Now: the work above, on machinery that is built and tested, so it
is not free and not urgent. Later: the synchronous promise is in every
operator's backup procedure, and weakening it is a break.

### Defects found in passing

Not design findings. Each was found by reading and none was reproduced.

| Where | What |
| --- | --- |
| `internal/engine/shippedupgrade.go:182,189` | The boot upgrade's kind-move guards are appended and then assigned over, so they never refuse. |
| `internal/api/rest.go:122-167` | F8 row 1. |
| `internal/engine/rows.go:758-759`, `internal/engine/rebuild.go:80-82` | A rebuild replays each purge, which deletes the embeddings of that id. The fold never enqueues an embedding (the live write does, `internal/engine/write.go:1414`), and the reconcile runs only on import (`internal/engine/repodir.go:812`). A record purged and created again loses its vectors after a rebuild, where the comment says they are kept. |
| `docs/api.md:2500`, `web/console/src/lib/api/types.ts:12-27`, `internal/api/errors.go:22-24` | Three statements of the closed error set, none equal to the fifteen constants. |
| `internal/engine/policy.go:23-26` | The header states a rule the effects path does not enforce (F7). |
| `docs/auth.md:308-311` | Claims a leaked token cannot outlive its revocation (F7). |
| `kinds/substrate.reamde.dev/authority.yaml:5` | Calls a live trust root a placeholder (F2). |
| Decisions 0050, 0052, 0062 | Confirmation names four tests that no longer exist. |
| `internal/api/appsetup.go:323-326,359,434-456` | `POST /authorizations/check` is on the scoped surface. It unwraps the scoped dataset and refreshes any held package below a floor the request names, before the token's own reach is applied, so a token of any scope can cause a publisher fetch and an additive upgrade of a package it does not name. |

### Direct verdicts

**The changelog.** It is three things, and the answer differs for each.

- *An ordered sequence of changes per repository: needed.* Watch, resume,
  triggers, the list-to-watch handoff and the apps' caches stand on it.
- *Full values, with everything derived replayable from them: valuable, and
  the most valuable design in the tree.* Agents and syncs write most of the
  data here, so this is the audit trail, the before-and-after and the proof
  that a backup replays. Rows plus an audit table would give those up. What
  is too broad is "forever, for every kind": every trigger delivery writes a
  run record and a delivery entry into permanent history
  (`internal/engine/functions.go:38-46`), and nothing can be erased (F4).
  Keep it as truth; give bookkeeping kinds retention.
- *The synchronous file mirror: decide it.* I would reduce it, once the
  design F11 asks for is written.

**The API.** The resource model is right and should stay: the record path
is the reference, one records route refuses per mode, the cursor is a seq
under a generation and a refusal names its replacement, the list hands its
head to the watch, and keys settle in the effect's transaction. It is not
ready to freeze as written, for F1's reason first and F8's second.

**Kinds and properties.** Sound: one stored reference shape, defaults
materialized at write, exact numbers, a closed dialect with reservations,
conversions as ordinary writes bound to a confirmed plan, retired names
that never return. The gaps are at the margins of evolution: no dialect
floor (F6), kind identity that only the binary can move, datatype changes
limited to three safe cases with a manual expand, copy and contract for the
rest, and the details in F10. None needs a redesign.

**The database.** Private, which is its best property. Row level security
under a role that cannot bypass it is a stronger boundary than convention
in each query. Pin the collation of the identity columns, make `body`
ordinary, enforce the minimum Postgres version at boot instead of in a
doc†, and keep the tables out of every operator procedure. Everything else
in F9 can change without anyone outside noticing.

## Part 2: direction after 1.0

Nothing here is built. Each section says what exists, what it would take,
and what it asks of 1.0.

### What could be cut

The review said where the promises are loose. This asks the other question:
what is here that 1.0 does not need. Sizes are as defined under
[How to read it](#how-to-read-it); the engine is 94,476 lines. Two rows
step outside that definition and say so: `host.py` is Python, and the API
fake is test code.
[The appendix](#appendix-how-the-sizes-were-counted) lists the files behind
each row.

| Area | Files counted | Lines | Stamp | View |
| --- | --- | --- | --- | --- |
| Observed values, aggregates, units | engine `aggregate*`, `observed*`, `unit*`, `quantity*`, `metric*`, `timeunits.go`; `internal/observed`; five vocabulary files | 19,812 | alpha | The largest subsystem: a time-series engine with a unit algebra. Reduce its reach, below. |
| Functions, triggers, runner, sandbox | ten engine files; `internal/runner`, `internal/sandbox`, `internal/egress`; and `host.py`, 1,573 lines of Python | 17,319 | stable (F5) | Needed. Trim what it writes to history. |
| Agents and model adapters | engine `agent*` and eleven supporting files; `internal/llm` | 16,428 | alpha | A second product. Outside the promise. |
| Packaging | `internal/catalog`, `authority`, `appsetup`, `authorize`; eight engine files; `rehome.go`, `bundle.go` | 10,552 | mixed | Two install doors, and the copying blocks sharing. |
| File mirror | `internal/changelogfile`; nine engine files | 8,460 | — | F11. |
| Declaration evolution | `schemadiff`, `convert`, `rename`, `move`, `retirement`, `restore`, `requiresfloor`, and the upgrade and repository-migration files | 7,786 | — | Earns its place: this is what makes data durable across declarations. |
| Scoped surface | seven API files; `internal/scope` | 5,259 | alpha | Earns it, and sharing rides on it. |
| Mapping and merge | eleven files | 4,515 | — | Earns it; 14 mappings ship. |
| Search and embeddings | seven engine files | 4,495 | beta; alpha | Derived; free to change. |
| Applets | `internal/applet`, two files | 3,536 | alpha | A UI runtime in the server, days old. Not core. |
| MCP | `internal/mcp` | 2,311 | alpha | Small, and it gives an outside assistant "everything the console does but the auth material" (`internal/scope/scope.go:37-42`). |
| API fake | three `_test.go` files, so test code | 1,900 | — | Replace, below. |

Two alpha subsystems, observed values and agents, are 30% of the engine's
own lines (14,722 and 13,923 of 94,476).

Candidates, in the order I would take them:

1. **Run records and delivery bookkeeping, out of permanent history.**
   "Every settled delivery attempt writes one `run` record", and "every
   motion is a fold effect on a `delivery` changelog entry"
   (`internal/engine/functions.go:38-46`). That is
   [0064](../decisions/0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md)'s
   property that a restore resumes its deliveries, and it is worth keeping
   for cursors. Run records are different: the fold keeps the newest twenty
   per trigger, with the newest success and any park that still stands
   (`internal/engine/functions.go:57-62,2594-2607`), and history keeps
   every one. Give the kind real retention, or move runs to a plain table.
2. **The synchronous half of the file mirror** (F11).
3. **The API fake.** It mirrors a 72-method interface by hand, and the tree
   says what it proves: "The fake's Put ignores IfVersion … so 'the engine
   returns sentinel X' and 'the API maps sentinel X to status Y' can both
   pass while the engine returns a different sentinel for the request that
   matters" (`internal/testenv/conformance_db_test.go:5-10`). Keep the
   import boundary; test the API's semantics against the real engine.
4. **Observed values, out of every write and out of the promise.** Four
   unit and observed checks run at the end of every write transaction
   (`internal/engine/dataset.go:801-818`), whether or not the repository
   holds a measurement, and every repository is born with 216 units and 149
   conversions (`AGENTS.md`). Not a deletion: make it cost nothing where it
   is unused, and keep it alpha.
5. **Agents and applets, outside the promise.** F9 says the same of the
   seeded assistant.
6. **One install door.** A published package installs under its
   publisher's name; a sample is copied under the repository's. The copy is
   what makes each person's `tasks/task` a different kind, which is the
   main obstacle to sharing vocabularies
   ([below](#sharing-with-someone-on-another-server)).
7. **The recovery key, and the DEK wrapped twice.** Records are plaintext
   at rest: "the changelog carries every record in the clear"
   (`internal/changelogfile/fsutil.go:8-12`). The sealed store protects
   provider tokens, the password hash and the second-factor seed, and each
   can be obtained again: reconnect the account, reset the password. What
   the recovery key adds is a move between hosts without doing that. Its
   costs are a ceremony at registration, a key that cannot be rotated (F4),
   and the rewrap tooling. Removing it is the opposite answer to F4's
   re-enrolment; the owner should pick one.

### Shared repositories

The owner's requirement, as given: several people in one repository; the
creator is the full owner; each other person has permissions of their own;
well defined, and not enterprise access control.

#### Three nouns

Today one word does three jobs. Split them:

- **Identity**: a person. A name, a password, an optional second factor. It
  holds no data.
- **Repository**: the data, named as today.
- **Membership**: a record inside a repository saying that an identity may
  act there, as an owner or with listed grants.

Today is the special case: one identity, one repository, the same name, and
an owner membership nobody wrote down. That is why this can arrive after
1.0 without moving data. Each existing repository gains one implicit owner.

#### Permissions

- **Owner.** Everything: members, packages and declarations, provider
  accounts and secrets, export, deletion. The creator is the first owner; a
  repository may have several, and always has one.
- **Member.** A list of grants in the grammar apps already use: `read:` and
  `write:` over one kind, a package, an authority or a trait
  (`internal/scope/scope.go:6-17`). Two presets: Editor is `write:*`, Viewer
  is `read:*`. A glob never reaches auth material
  ([0080](../decisions/0080-a-kind-grant-may-glob-and-a-glob-never-reaches-auth-material.md)),
  and should not reach F7's control kinds either.
- **Own.** One qualifier on a write grant: only the records this member
  created. It answers "each person edits their own posts" without a list
  per record. It needs each record to carry its creator, which the subject
  below provides.
- **Not offered:** access lists per record, groups, deny rules,
  inheritance. The rule that replaces them: *a repository is the unit of
  privacy, and a kind is the unit of permission.* Something private from
  other members belongs in another repository.
- **Grants are read per request** from the membership, not copied into the
  token, so a change or a removal takes effect at once.
- **Delegation.** An agent or a function a member invokes runs with the
  intersection of its own grant and the member's. A run no person invoked,
  a trigger's, keeps the callable's own grant as today.

#### Where identity lives

I recommend a server-level store, outside any repository's changelog.

- Hundreds of members should not each need a seeded repository.
- The credential stops being a record of one repository. Today it is one,
  holding two refs into that repository's sealed store
  (`internal/engine/auth.go:5-12`), so changing it is a changelog entry,
  which is why an operator's reset needs the server stopped
  (`docs/operations.md:1934-1936`). This is a structural gain, not a
  security one: the hash and the seed were never in history.
- The login name stops being the repository's name
  ([0074](../decisions/0074-the-repository-name-is-the-login-name.md)),
  which loosens F2: a person could change their login without touching a
  repository.

The cost is that an identity is no longer inside a repository's backup, so
it needs its own export. The alternative, each person's identity being a
personal repository, keeps one concept and one backup and costs a
repository per member.

Keep identity names in today's DNS-style grammar, so that another server
could vouch for one later: the person signs in at their own server, which
asserts who they are. IndieAuth and OpenID Connect are the precedents, not
a fit as they stand: IndieAuth names a person by a profile URL, and OpenID
Connect by an issuer and a subject. Two pieces exist here: the app
authorization flow
([0194](../decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)),
and the `server=` field of a name's `_substrate` record, which says where
it is served (`internal/domains/record.go:2-4`). Finding a person's server,
validating the name it asserts and the trust between two servers are all
still to design.

#### What exists, and what is missing

| Needed | Today |
| --- | --- |
| A dataset held to a grant | exists: `scopedDataset` (`internal/api/scoped.go:19-40`), behind its own route group (`internal/api/api.go:465-466`) |
| A token that names a person | missing: a login takes a repository, a password and a code (`internal/substrate/auth.go:330-339`), and the credential's id is `self` (`internal/engine/auth.go:52`) |
| A grant given to a person | missing: a scoped token comes only from the app flow; a hand-minted one may carry `["mcp"]` alone (`internal/api/auth_endpoints.go:66-69,372`) |
| Clients that work with less than full access | missing: Chat asks for `full` (`apps/chat/ChatKit/Sources/ChatKit/ChatManifest.swift:35`), and the scoped group leaves out vocabulary plans, merge, split and the catalog (`internal/api/api.go:542-591`) |
| Delegation bounded by the caller | missing (F7) |
| One meaning for `owner` | missing: it is a tier, "the human hand" (`internal/substrate/tier.go:15-19`), and a declaration value, `writer: owner` (`internal/vocabulary/types.go:416-430`) |

#### What the changelog needs

No structural change. It is one total order written by one process, and
many people are many clients of that one writer. 0017 ruled out
"multi-writer" in another sense, replicas that merge histories, and that is
still not needed.

1. **A subject on each line:** who. A line has `actor`, which door or which
   code, and `principal`, a token id, and a token record can be deleted.
   The key set is closed (`internal/changelogfile/entry.go:179-180`), so
   this is a changelog dialect event unless the key is reserved now.
2. **Memberships and grants are records,** so a permission change is in
   history and in the export.
3. **A change event carries the subject,** so a client can say who changed
   something. The feed is already cut to what a token reads.
4. **Each record would carry its creator,** taken from the subject of the
   entry that created it, for the `own` qualifier. This is new: nothing
   derives it today.

#### Concurrency, and the patterns it already follows

No new mechanism is needed, and one policy is. What exists is a well-known
shape: a server that decides the order, a merge per property, and a version
precondition on the whole record (`internal/engine/write.go:3189-3204`).
Two cases follow.

- *Without `ifVersion`:* two people who change different properties both
  land. Two who change the same property leave the later value, and nobody
  is refused.
- *With `ifVersion`:* the second writer is refused even when the properties
  differ, because the version is the record's.

What is left to choose is what a client does with that 409: read again and
re-apply, which the apps' outbox already holds a refused write back for, or
ask the person.

| System | What it shares with this design |
| --- | --- |
| Kubernetes list and watch | a resource version, a watch from it, and a 410 that says list again |
| CouchDB | a per-database sequence, a `_changes` feed from `since`, a `_rev` precondition |
| JMAP | a state string per type, a changes call from it, and accounts a user may be given access to, each marked read-only or not |
| Replicache | a server-authoritative order, a client queue of pending writes replayed over what the server returns: the apps' outbox |

Automerge and Yjs merge concurrent edits of structured data and of text
without a server choosing the order. That is what offline multi-writer, or
two people editing one text live, would need. 0017 rules out the first and
the second is not a goal, so with one process ordering every change
neither is required.

#### Tens or hundreds of people

Still one repository, one writer process and no replica. It bends in this
order:

1. **Watch fan-out.** A commit signals every subscriber, in batches about
   300 ms apart (`internal/engine/dataset.go:1178-1207`), and each watcher
   then runs its
   own read (`internal/api/watch.go:204-206`). A repository takes at most
   eight of sixteen connections (`internal/engine/scope.go:95-105`). The
   remedy is one in-memory tail per repository fanned out to its watchers,
   as Kubernetes' API server does. The wire does not change.
2. **Token lookup.** Every request finds its token with a query across
   repositories on the maintenance pool
   (`internal/engine/auth.go:1203-1222`). I found no cache in front of it.
   The server sees every token write, so a cache can be exact.
3. **The write lock.** Human write rates are far below it. One member's
   provider sync holding it for a page is what the others feel, so "owner
   first" (`internal/engine/writegate.go:34-41`) becomes "interactive
   first".
4. **Caps that are per repository become per member:** four chat turns at
   once (`internal/engine/agents.go:376-379`), and no rate limit on the API
   (F8).

#### Sharing with someone on another server

The cheapest form uses what exists: the other person's substrate is a
provider. Bob connects an account to Ada's repository, holding a scoped
token, and her records mirror into his through the sync machinery. It
needs three things besides the package:

- **A way to grant a person**, as above.
- **Grants narrower than a kind**, if she shares less than a whole
  collection. This is the expensive part: a row filter has to hold in list,
  search, aggregate, window, expand, referencing and the feed.
- **The same kind on both sides.** Her copied `tasks/task` is not his. He
  would install her package as published, and today a repository cannot
  install from its own authority (`internal/catalog/published.go:134-140`)
  and a publisher hosts its document by hand
  (`cmd/substratectl/commands/publish.go:30`).

It also sharpens F4: once mirrored, her data is in his permanent history.

### Several repositories under one API

Once a person belongs to several repositories, clients and agents need to
work across them.

#### What "across repositories" can mean here

- **Reads fan out; they cannot join.** A connection is pinned to one
  repository and unpinned on release (`internal/engine/scope.go:171-181`).
- **Each repository has its own order.** A merged stream needs a cursor per
  repository, and there is no global order.
- **A write lands in one repository.** There is no transaction across two
  changelogs, and none should be promised.
- **A record's identity becomes repository, kind and id.** The wire record
  has no repository field (`internal/substrate/record.go:12-78`), and ids a
  provider composes can collide: one calendar event synced into two.
- **Kinds differ per repository,** so "all my tasks everywhere" works by
  trait, not by kind.
- **Search ranks per repository**†, so a merged search needs a merge rule.

#### A library or a server API

| | Client library | Server API |
| --- | --- | --- |
| Credential | a token per repository | one identity session |
| Server change | none | a new surface |
| Reach | any server | one server |
| Cost | N round trips and N streams; the logic is written three times, in Swift, TypeScript and Go, unless it is specified once | the one place repositories meet, so the one place a leak can happen |
| Blocked on | nothing | identities and memberships: a token belongs to one repository today |

Both, in that order. The library is the only form that crosses servers, and
it needs no change to what the server speaks. It is not free: it keeps a
cache and a cursor per repository, and it has to reconcile vocabularies
that differ. The CLI already holds several named
contexts, one at a time (`cmd/substratectl/commands/config.go:20-33`); an
app holds one repository per link (`docs/terms.md:170`).

For the server API I would copy JMAP's shape: a session lists the accounts
a user may use, each marked personal or read-only, every call names one
account, and one push carries a state per account. JMAP stops there; the
grants proposed above are this design's own. Matrix's `/sync` is the precedent for
one stream, resumed by one token, over many rooms. Three additions, in
order:

1. **A session read:** the repositories this identity may use, with its
   grants in each.
2. **A repository selector for the existing API.** An exchange that returns
   a token per repository, in the manner of OAuth token exchange, keeps
   "the token implies the repository". A path prefix has precedent too: a
   webhook URL already names the repository
   (`internal/api/api.go:352-353`).
3. **Fan-out for list, search and watch only,** with the repository on
   every record and a cursor that is a map. Everything else stays
   single-repository.

#### An agent across repositories

Three routes, cheapest first:

- **An outside assistant over MCP: today.** The door calls the REST API
  with the caller's own `Authorization` header
  (`internal/api/mcp.go:196-228`), and no tool takes a repository argument. Connect one endpoint per repository token and the
  client tells them apart.
- **A `substrate` provider package.** Its account holds a scoped token for
  the other repository, and its functions are offered to agents as tools
  ([0198](../decisions/0198-a-bundle-offers-its-functions-and-an-agent-takes-them-by-pattern.md)).
  Existing machinery, and it crosses servers.
- **A native agent**, such as the assistant in one repository reading the
  household one. The loop holds one dataset
  (`internal/engine/agentloop.go:665-666`). It would need tools that take a
  repository; a dataset per repository opened as the invoking person, under
  the intersection of grants; the thread kept in the home repository; each
  repository's kinds in the prompt; and a rule for runs no person invoked.
  I would let an agent be a member in its own right.

The risk that grows is the confused deputy. Text read from one repository
can instruct an agent that may write to another, so a prompt injection
becomes a path between people's data. Cross-repository agent access should
default to read-only, with writes held for approval.

### What Part 2 asks of 1.0

Nothing in Part 2 needs building before 1.0. Eight things keep it possible.
Items 1, 5, 6 and 7 are fixes Part 1 already asks for; 2 and 3 are
reservations; 4 and 8 are decisions.

1. One enforcement path for a principal with a grant, and delegation
   bounded by the caller (F7).
2. Reserve `subject` in the changelog line's key set, inert, as 0020
   prescribes for dialect keys.
3. Reserve a key in the reference value for "which repository", as `target`
   was reserved (`internal/vocabulary/ref.go:31-43`). A reference to a
   published kind cannot say whose record it means.
4. Settle the word `owner` before a role takes it.
5. Stop promising that the login name is the repository's name, or that
   `id == authority` (F2).
6. Fix default names (F2): `ada.localhost` cannot be shared with.
7. State that a client ignores response keys it does not know (F8), so a
   repository can be added to a record.
8. Keep the rule that every write targets exactly one repository.

## Part 3: policy and gate

### A proposed stability policy

The tiers and the notice before a removal are modelled on the Kubernetes
deprecation policy; what a major version may break follows semantic
versioning.

1. **Three tiers, and what each promises.** *Stable*: additive only within
   1.x; removed only in 2.0. *Beta*: supported; a break is announced one
   minor release ahead in discovery and the changelog. *Experimental*: may
   change or go in any release.
2. **Durability is not scoped by tier.** A record of any kind, an
   experimental package's included, survives every upgrade or is carried by
   a conversion. A surface may be withdrawn; data may not be.
3. **What additive means, per surface.**
   - Wire: a response may gain fields, and a client must ignore keys it
     does not know. A new request field ships with a feature entry a client
     gates on, because servers refuse what they do not know.
   - Dialect: a new key raises the vocabulary dialect; a package states the
     dialect it needs.
   - Stored format: a new line key or effect raises the changelog dialect.
     A binary reads every dialect at or below its own, throughout 1.x, and
     2.0 reads what the last 1.x wrote.
   - Function ABI: an SDK version, and a supported Python minor range.
4. **Upgrade.** A 1.x binary opens any database and data root written by
   the same or an earlier 1.x release, in one step. It refuses one written
   by a later release, as today. Migrations only go forward. A repository
   migration is carried until 2.0.
5. **Rollback.** Not supported past a first boot that stamps or migrates.
   The way back is the snapshot taken before. Each release says whether it
   closes rollback.
6. **Deprecation.** Announced in discovery and the changelog. For a stable
   surface it is notice ahead of 2.0 and never a licence to remove it
   within 1.x. A beta surface is served for at least two further minor
   releases after the notice.
7. **Outside the promise, by name.** The Postgres tables, the Go packages,
   table output of the CLI, the seeded assistant, console layouts, model
   lists and vendor mappings.
8. **Nothing on `main` is `proposed`.** A record is accepted or the code is
   not there.

### The release gate

Decisions:

1. The scope of 1.0 and the policy above, or another (F1).
2. The project's permanent name and who holds it (F2).
3. Repository identity: the opaque id, the name completion, the stance on
   rename and reuse (F2).
4. What a delete promises, and the order of redaction and compaction (F4).
5. The function ABI's version and its Python window; the retry policy that
   will freeze (F5).
6. The package dialect floor; whether a publisher may rename a kind (F6).
7. Whether third-party code and third-party publishing are experimental at
   1.0.
8. Whether a server shared by strangers is supported, and with what.
9. The file mirror: keep it synchronous, or supersede 0062 (F11).
10. `body`: ordinary, or every long text out of line (F10).
11. The reservations Part 2 asks for, and the word `owner`.
12. The recovery key: re-enrolment, or removal (F4).

Fixes. Each is implementation work, and each waits on the decision above
where one applies:

| Finding | Fix |
| --- | --- |
| F1 | A `records` feature in discovery; honest stamps; the interface-freeze sentence removed; one test holding the docs and the console to the code's error set; every `proposed` record with code on `main` accepted or rejected. |
| F2 | Bare labels completed under the configured host; an opaque id in the manifest and the row; a first-seen digest on a published package. |
| F3 | Strict decoding of effects; the format documented; the kind-move guards restored at `internal/engine/shippedupgrade.go:189`. |
| F5 | An SDK version key; a golden file for `input` and `config`; a transient-failure backoff; `best-effort` refused where registration is open. |
| F6 | A dialect floor on the package header and in the authority document, and the server's maximum in discovery. |
| F7 | The control-kind refusals for every writer class; delegation bounded by the caller; a grammar for the actor header; sign out everywhere; a lease on every stream; `docs/auth.md` corrected. |
| F8 | Rows 1 to 5. |
| F9 | A damaged repository quarantined at boot, if more than one person shares a server. |
| F10 | `"C"` collation on the identity columns; `body`; `timezone`; added value sets counted as narrowings. |
| Part 2 | `subject` reserved in the line's key set; a repository key reserved in the reference value. |

Targeted validation:

1. Released data-root fixtures, imported under the current binary (F3).
2. A boot with one damaged repository among several: the others serve.
3. A process-kill crash test at each step of `commitAndMirror`; today's
   tests use fault seams (`internal/engine/dataset.go:957-977`).
4. The writer-class matrix over the control kinds (F7).
5. A route roster: every route is public, owner-only or scoped, and a new
   one fails until it is placed†.
6. Idempotent replay, PATCH under `ifVersion` and cursor refusals over HTTP
   against the real engine; today the fake holds them†.
7. A horizon above 0: each first-party app recovers from `compacted`.
8. An export restored on a fresh host with only the recovery key.
9. Rebuild, import and snapshot timed at a million records and 30 GB, so
   the limits are numbers.
10. A soak: a stated run of releases with no break to a stable surface.

### Questions only the owner can answer

Answered since the review:

- *A second person: a second repository, or a second principal in one?* A
  second principal, with permissions per person
  ([Shared repositories](#shared-repositories)).
- *Replicas?* No: one repository, one writer process, tens or hundreds of
  people.
- *Object storage?* Open to it if it simplifies (F11).

Still open:

1. Is 1.0 a promise to strangers who build apps and packages, or to
   yourself and the people you host? Most of the gate follows from this.
2. Is `substrate.reamde.dev` the permanent name, and who holds it if you
   stop?
3. Will you run a server for people you do not know? That decides the
   sandbox default, name reuse, boot isolation and whether "no erasure" is
   tenable.
4. Is "every change is kept" a feature you would defend to someone asking
   you to remove one thing, or today's limit?
5. Should a repository ever be renamed or handed to another host under a
   new name? If never, say so; I would still mint the id.
6. Is a one-way upgrade acceptable for the life of 1.x?
7. Is Python the only body runtime for 1.x, and is its minor version part
   of the contract?
8. What size must one repository reach in 1.x: is 10 million records or
   100 GB in scope?
9. Are the assistant, the console's layouts and the model lists part of
   what 1.0 promises, or product that ships beside it?
10. 0017 rules out offline multi-writer and the apps keep an outbox. Is
    that the permanent answer for a product that lives on phones?
11. If Postgres is lost, how many seconds of acknowledged writes may go
    with it, and should writes stop when the copy falls further behind
    than that? Those two answers decide F11.
12. Does an identity live on the server, or is it a personal repository?
13. Is "write only your own records" needed at the start, or do kind grants
    suffice?
14. May an agent be a member of a repository in its own right?

## References

### Decision records cited

| Record | Cited for |
| --- | --- |
| [0016](../decisions/0016-a-kind-titles-itself-from-a-declared-property.md) | the `title` column (F10) |
| [0017](../decisions/0017-the-changelog-is-one-writers-total-order.md) | one writer's total order; positions that escape into data (F4, Part 2) |
| [0020](../decisions/0020-dialect-keys-are-reserved-not-tolerated.md) | reserving a key before it is used (F6, Part 2) |
| [0022](../decisions/0022-rest-is-frozen-graphql-is-a-projection.md) | an earlier "frozen v1" (F1) |
| [0026](../decisions/0026-embedding-vectors-are-1536-wide-or-refused.md) | vector width (F9) |
| [0041](../decisions/0041-body-is-a-declarable-property-and-fts-is-per-property.md) | the `body` column (F10) |
| [0050](../decisions/0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md) | segment files; the dropped chain (F4, F11) |
| [0052](../decisions/0052-the-authority-is-the-repository-id.md) | the name as the id (F2) |
| [0054](../decisions/0054-a-repository-moves-between-host-keys-through-an-offline-rewrap.md) | key rotation (F4) |
| [0062](../decisions/0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md) | the synchronous mirror (F11) |
| [0064](../decisions/0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md) | delivery bookkeeping in history (Part 2) |
| [0072](../decisions/0072-reserve-a-purged-record-id-against-reuse.md) | id reuse after a purge (F8) |
| [0074](../decisions/0074-the-repository-name-is-the-login-name.md) | the login name (Part 2) |
| [0075](../decisions/0075-v1-ships-the-fs-blob-backend-alone.md) | the removed S3 backend (F11) |
| [0079](../decisions/0079-graphql-is-removed-and-the-records-read-is-one-route.md) | the routes removed after a freeze (F1) |
| [0080](../decisions/0080-a-kind-grant-may-glob-and-a-glob-never-reaches-auth-material.md) | what a glob never reaches (Part 2) |
| [0083](../decisions/0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md) | one writer (F9) |
| [0125](../decisions/0125-a-provider-write-is-a-callable-that-writes-no-record.md) | a retried provider write posts twice (F5) |
| [0143](../decisions/0143-a-token-has-full-access-to-its-repository-and-a-login-token-does-not-expire.md) | tokens that do not expire (F7) |
| [0165](../decisions/0165-a-commit-in-doubt-is-caught-up-by-the-next-write-and-no-write-grant-names-the-owners-auth-kinds.md) | the commit in doubt (F8) |
| [0194](../decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md) | the app flow and scoped tokens (Part 2) |
| [0198](../decisions/0198-a-bundle-offers-its-functions-and-an-agent-takes-them-by-pattern.md) | functions offered as tools (Part 2) |
| [0208](../decisions/0208-a-repository-proves-its-name-with-a-txt-record-after-registration.md) | DNS proof of a name (F2) |
| [0209](../decisions/0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md) | publisher verification (F2) |
| [0212](../decisions/0212-a-package-is-published-by-its-authoritys-document-and-installed-from-it.md) | published packages (F1, F2) |
| [0213](../decisions/0213-a-published-package-keeps-its-publishers-versions-and-an-upgrade-waits-for-every-connected-build.md) | a proposed record whose code is on `main` (F1) |
| [0220](../decisions/0220-outside-code-is-confined-and-reviewed.md) | a proposed record whose code is on `main`; outside code must be confined (F1, F5) |
| [0236](../decisions/0236-an-owner-may-delete-their-repository-and-the-name-is-freed.md) | name reuse (F2) |
| [0245](../decisions/0245-a-stale-revision-is-a-validation-refusal-narrowed-by-the-reason-stale-revision.md) | a proposed record already on the wire (F1) |

### Other systems

Named for comparison. Each link was reachable on 2026-11-10; none is fetched
by `lint:docs`.

| System | Source | Cited for |
| --- | --- | --- |
| Kubernetes API | [API concepts](https://kubernetes.io/docs/reference/using-api/api-concepts/) | list and watch from a resource version; 410 Gone; a watch cache |
| Kubernetes, SemVer | [deprecation policy](https://kubernetes.io/docs/reference/using-api/deprecation-policy/), [semver.org](https://semver.org/) | stability tiers, notice before removal, what a major version may break |
| CouchDB | [changes feed](https://docs.couchdb.org/en/stable/api/database/changes.html), [document API](https://docs.couchdb.org/en/stable/api/document/common.html), [replication protocol](https://docs.couchdb.org/en/stable/replication/protocol.html) | a per-database sequence; a revision precondition on a document update |
| JMAP | [RFC 8620](https://www.rfc-editor.org/rfc/rfc8620): accounts (section 1.6.2), the session resource (2), `/changes` (5.2), the `StateChange` object (7.1) | one session over several accounts, each marked personal or read-only; a state per account |
| Replicache | [how it works](https://doc.replicache.dev/concepts/how-it-works) | a client queue replayed over the server's order |
| Matrix | [client-server API](https://spec.matrix.org/latest/client-server-api/), `/sync` | one stream over many rooms |
| Automerge, Yjs | [automerge.org](https://automerge.org/), [docs.yjs.dev](https://docs.yjs.dev/) | merging concurrent edits with no server choosing the order; not needed here |
| IndieAuth | [specification](https://indieauth.spec.indieweb.org/) | a person named by a profile URL; clients that register nowhere |
| OpenID Connect | [core](https://openid.net/specs/openid-connect-core-1_0.html) | a server vouching for a person, named by issuer and subject |
| OAuth token exchange | [RFC 8693](https://www.rfc-editor.org/rfc/rfc8693) | a session exchanged for a token per repository |
| Postgres | [continuous archiving](https://www.postgresql.org/docs/current/continuous-archiving.html) | shipping a log after the commit |
| Postgres | [TOAST](https://www.postgresql.org/docs/current/storage-toast.html), [JSON types](https://www.postgresql.org/docs/current/datatype-json.html) | how a large `jsonb` value is stored and read |
| Postgres | [ALTER COLLATION](https://www.postgresql.org/docs/current/sql-altercollation.html), [collation support](https://www.postgresql.org/docs/current/collation.html) | indexes and a changed collation version |
| Litestream | [how it works](https://litestream.io/how-it-works/) | replicating a log to object storage |
| Delta Lake | [protocol](https://github.com/delta-io/delta/blob/master/PROTOCOL.md) | a log as ordered immutable files in object storage |
| Amazon S3 | [appending to objects](https://docs.aws.amazon.com/AmazonS3/latest/userguide/directory-buckets-objects-append.html) | append exists in one storage class only |

## Appendix: how the sizes were counted

Each figure is `wc -l` over the named files at `f26d42a6b`. "All of" a
directory means its `*.go` files outside `_test.go`, without its
subdirectories. No file is counted in two rows.

| Row | Files | Lines |
| --- | --- | --- |
| Engine | all of `internal/engine` | 94,476 |
| Observed values, aggregates, units | in `internal/engine`: `aggregate*.go` (8,851), `observed*.go` (2,645), `unitgraph.go` and `unitseed.go` (1,465), `quantity.go`, `quantitygraph.go` and `quantitywrite.go` (1,378), `metric.go` and `metricrules.go` (329), `timeunits.go` (54): 14,722. All of `internal/observed` (1,935). In `internal/vocabulary`: `unitgraph.go`, `quantity.go`, `observed.go`, `metric.go`, `rational.go` (3,155) | 19,812 |
| Functions, triggers, runner, sandbox | in `internal/engine`: `functions.go`, `triggers.go`, `delivery.go`, `runner.go`, `effects.go`, `sync.go`, `schedulecursor.go`, `callrun.go`, `invocationconfig.go`, `background.go` (9,038). All of `internal/runner` (3,113), `internal/sandbox` (3,167), `internal/egress` (428). `internal/runner/host.py` (1,573) | 17,319 |
| Agents and model adapters | in `internal/engine`: the twenty `agent*.go` files (9,595), and `policy.go`, `judge.go`, `approvals.go`, `spend.go`, `operatorspend.go`, `interactions.go`, `llmmodels.go`, `offeredtools.go`, `alerts.go`, `operatorprovider.go`, `keyrefusals.go` (4,328): 13,923. All of `internal/llm` (2,505) | 16,428 |
| Packaging | all of `internal/catalog` (2,994), `internal/authority` (586), `internal/appsetup` (1,957), `internal/authorize` (1,501). In `internal/engine`: `outsidecode.go`, `outsideupgrade.go`, `appowned.go`, `appbuild.go`, `addonconsent.go`, `bundles.go`, `inputs.go`, `settings.go` (2,350). In `internal/vocabulary`: `rehome.go`, `bundle.go` (1,164) | 10,552 |
| File mirror | all of `internal/changelogfile` (4,062). In `internal/engine`: `repodir.go`, `snapshot.go`, `export.go`, `rewrap.go`, `verify.go`, `segmentdigest.go`, `scrub.go`, `checksum.go`, `changelogdialect.go` (4,398) | 8,460 |
| File mirror, tests | every `_test.go` of `internal/changelogfile` (4,235). In `internal/engine`, the tests named `repodir*`, `snapshot*`, `export*`, `durable_ack*`, `bootdigest*`, `sealing*`, `manifest*` (7,606) | 11,841 |
| Declaration evolution | in `internal/engine`: `schemadiff.go`, `convert.go`, `rename.go`, `move.go`, `retirement.go`, `upgradeplan.go`, `shippedupgrade.go`, `repomigrate.go`, the five `repomigration_*.go`, `restore.go`, `requiresfloor.go` | 7,786 |
| Scoped surface | in `internal/api`: `scoped.go`, `scopedreads.go`, `scopedcall.go`, `scopedagents.go`, `authorize.go`, `authorizecall.go`, `appsetup.go` (4,367). All of `internal/scope` (892) | 5,259 |
| Mapping and merge | in `internal/engine`: `mapping.go`, `mappingbackfill.go`, `mappingwhere.go`, `merge.go`, `orphans.go`, `ambiguous.go`, `linkedfrom.go`. In `internal/vocabulary`: `mapping.go`, `mappingsubject.go`, `seededmapping.go`, `suggested.go` | 4,515 |
| Search and embeddings | in `internal/engine`: `search.go`, `searchindex.go`, `searchtext.go`, `bm25.go`, `tsquery.go`, `embedprovider.go`, `reprojection.go` | 4,495 |
| Applets | all of `internal/applet` (3,210). `internal/engine/applets.go` and `internal/api/applets.go` (326) | 3,536 |
| MCP | all of `internal/mcp` | 2,311 |
| API fake | `internal/api/fake_test.go`, `fake_aggregate_test.go`, `fake_window_test.go` | 1,900 |

The tests F11 says would go with the synchronous paths: in
`internal/engine`, `durable_ack_db_test.go` (807), `snapshot_db_test.go`
(563), `snapshotbase_db_test.go` (312), `bootdigest_db_test.go` (199); in
`internal/changelogfile`, `txn_test.go` (376), `unread_test.go` (432),
`snapshot_test.go` (100), `writer_test.go` (681). Together 3,470.
