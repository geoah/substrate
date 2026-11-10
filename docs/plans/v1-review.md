# Review for 1.0: architecture, data model and system design

Status: a review, written 2026-11-10 against `f26d42a6b` (v0.123.1). It
recommends and implements nothing, and like every page under `docs/plans/`
it is not a contract: the code that lands is.

## How to read it

- Every `path:line` below was opened during the review, except a claim
  marked †, which a delegated reading pass reported and I did not open
  again. Treat a † as a lead, not a result.
- Nothing was run: no database suite, no live test. A test named here exists
  in the tree; I did not watch it pass.
- A finding has a **kind**: *demonstrated* (the code path shows it), *risk*
  (plausible, not shown) or *question*. It has a **class**: *fix before 1.0*,
  *decide and document*, *safe to evolve later* or *remove*.

## Verdict

**Blocked today by a short list, none of it a redesign. Behind that list:
ready with explicit limits.**

The core is sound and should be kept as it is: a changelog of value-carrying
deltas with one fold path, a write that is on disk before its commit, seq
order equal to visibility order, isolation held by Postgres row level
security, a closed declaration dialect whose conversions are ordinary record
writes, and a scoped surface that fails to compile when a new verb is not
decided. Most of what follows is about the promises around that core, not
the core.

Three things stand between it and a 1.0:

1. **Nothing says what 1.0 promises, and `stable` has not meant stable.**
   28 of the 84 releases since v0.70.0 carry a BREAKING section, the latest
   two days before this review, inside a feature stamped "frozen for v1"
   (F1).
2. **Five seams are cheap today and permanent afterwards:** the repository's
   identity (F2), a pinned stored format (F3), a version on the function body
   ABI (F5), a dialect floor on a package (F6) and the collation of the
   identity columns (F10).
3. **Three deferred decisions become promises by silence:** what deleting
   means and how history ever shrinks (F4), whether every writer is held to
   the same authority (F7), and a handful of API margins (F8).

The limits a 1.0 can state and keep: one person per repository, one server
process per database, Linux for confined code, no erasure yet, and agents,
MCP, applets and third-party code as experimental surfaces over durable
records.

## Who it is for

The README states the product: "a private home for everything one person
keeps", run "on a machine you choose", by yourself or by "someone you trust".

| | |
| --- | --- |
| **Users** | One person per repository. An operator, who is often the same person. App authors, through scoped tokens. Package publishers, through authority documents. Assistants, through MCP. |
| **Workloads** | Hand-written records beside provider mirrors (mail, calendar, messages, health samples); a console, four apps, a CLI; a few agents and scheduled functions. Observed, on the one deployment: a repository of 620k records (`internal/engine/migrations/0018_records_matching_kinds.up.sql:10`), and a 27 GB history on 2026-10-06, eight weeks after the first tag (`internal/engine/segmentdigest.go:15-18`). |
| **Non-goals, as written** | "No sharing, no second user reading your repository, no cross-repository query. No erasure, compaction, or retention policy … No signatures and no hash chain … Each of those is a deliberate absence" (`docs/operations.md:1963-1969`). One replica (`docs/operations.md:1100-1103`). No offline multi-writer ([0017](../decisions/0017-the-changelog-is-one-writers-total-order.md)). |

### What one user per repository costs

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
  `self`†, a token record has no subject†, the stored tier `owner` means
  "any human door". Adding a subject is additive, because the line's key
  set is closed and gated (`internal/changelogfile/entry.go:179-180`), so
  an older binary refuses rather than misreads. History written before it
  stays attributed to a door and a deletable token id.

Verdict: keep it. It is *safe to evolve later*, provided F7 is fixed and
nobody promises that `principal` identifies a person.

## The model today, and what holds it

| Invariant | Where it is enforced | Held by |
| --- | --- | --- |
| One path from changelog to `records`; a rebuild reproduces the fold | `internal/engine/fold.go`, `rebuild.go:121-128` | `foldpath_internal_test.go`, `rebuild_db_test.go` |
| A write is durable in the segment before Postgres commits; an unfinished transaction is cut whole | `internal/engine/dataset.go:853-1013` | `internal/changelogfile/txn_test.go`, `durable_ack_db_test.go`, `repodir_db_test.go` |
| Seq order is visibility order, so a cursor skips nothing | changelog lock taken at transaction start, `dataset.go:784-797` | `TestChangesCursorWalkSkipsNoSeqUnderConcurrentWriters` |
| One writer per repository | `flock` on the directory (`internal/changelogfile/lock.go:35-48`), a session advisory lease (`internal/engine/writerlease.go`†) | `writerlease_db_test.go` |
| A repository reads only its own rows | `FORCE ROW LEVEL SECURITY` on every scoped table, `internal/engine/migrations/0001_init.up.sql:551-577` | `isolation_db_test.go` |
| An app's token reaches only what was decided for it | two route groups (`internal/api/auth.go:223-235`) and a dataset that embeds nothing (`internal/api/scoped.go:19-40`) | the compiler; fake-backed API tests |
| Secret material never enters the changelog | sealed refs, bound to their address (`internal/engine/dek.go:38-44`) | `sealing_db_test.go`† |
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
| Data root, segment lines, fold effects, export tar | every backup, every exit | "a storage change and not an API change" (`docs/changelog.md:55-61`); `export: beta` | public and documented, read forever (F3) |
| Postgres tables | nobody | internal | internal; say so |
| `internal/substrate` interfaces | this module | "what v1 freezes" (`internal/substrate/dataset.go:19-24`) | internal; remove the sentence |
| `substratectl` | operators, scripts | no statement; the config file has no version (`cmd/substratectl/commands/config.go:20-33`) | `-o json` and `-o yaml` follow the wire; table output and the operator hat are not promised |
| Authority document, `_substrate` TXT | publishers | [0209](../decisions/0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md) accepted, [0212](../decisions/0212-a-package-is-published-by-its-authoritys-document-and-installed-from-it.md) proposed | experimental until F2's continuity question is answered |

## Findings

In priority order. F1 prices every other one: the "later" cost of each is
whatever 1.0 turns out to promise.

### F1. "v1" has no scope, and `stable` has not meant stable

*Demonstrated. Fix before 1.0.*

**Evidence.**

- `stable` is defined as "frozen for v1. Changes are additive only"
  (`internal/substrate/stability.go:8-12`), and `triggers`, `functions`,
  `bundles`, `blobs` and `changefeed` carry it
  (`internal/api/discovery.go:192-206`). No entry covers records, the record
  path, the mutations, pagination, errors or the auth doors. The surface a
  third party uses, `apps`, is alpha (`discovery.go:227-231`).
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
  `main`: 0212, 0213, 0220, and
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

**Recommendation.** Adopt a written policy (proposed below). Re-stamp
honestly: add `records` as stable, demote `functions` until F5, decide
`apps`. Delete the interface-freeze sentence. Hold `docs/api.md` and the
console's union to the code's error set with a test. Accept or reject every
`proposed` record whose code is on `main`. Then run a soak (the gate).

**Cost.** Now: a page, a test, a few stamps and a quiet period. Later: each
break lands on clients the project does not ship.

### F2. Names are forever: the repository's and the project's

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
(`0001_init.up.sql:62-69`); the manifest must equal the directory name†;
the DEK wrap binds `dek\0<authority>` (`internal/engine/dek.go:38-44`);
imported samples are rehomed, so the user's own kinds and every reference to
them carry the name; scopes store `~` expanded†.
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
and DNS proof is optional and read by nothing but its own endpoints†.
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

**Cost.** Now: small. Later: every repository carries a name nobody can
correct, and the id seam needs the dialect event anyway.

### F3. The stored format is the exit and the upgrade path, and nothing pins it

*Risk, with demonstrated gaps. Fix before 1.0.*

**Evidence.**

- The README promises "you can download everything, as one file". That file
  is the repository directory, which `docs/changelog.md:55-61` calls
  storage, free to change.
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
  (`fold.go:200-210`).
- The checksum's canonical form is "encoding/json's one policy" for strings
  (`entry.go:247-251`), written down nowhere else.
- Rollback closes at first boot (`docs/operations.md:1253-1261`). Floors
  already exist: a database from before v0.70.0 cannot upgrade in place
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
is carried.

**Cost.** Now: one fixture generator and one test. Later: the first replay
that diverges is found by someone restoring a backup.

### F4. Deleting does not erase, history only grows, and keys never rotate

*Demonstrated. Decide and document before 1.0; the mechanisms are safe to
evolve later.*

**Evidence.**

- A purge deletes fold rows and nothing else
  (`internal/engine/rows.go:738-767`). The segments and the `changelog`
  table keep every value, and the change feed derives before and after
  values from entries alone (`internal/engine/changevalues.go:13-20`).
- Every write is stored three times: the segment line, the `changelog`
  row with its full `payload` (`0001_init.up.sql:257-280`) and the fold.
  The horizon is the constant 0 (`internal/api/watch.go:42-48`).
- Positions escape into data: `causedBy`, a message's `changes`, trigger
  cursors. 0017 names this itself, so any compaction must move a horizon
  and never renumber.
- The recovery key has one writer, at registration
  (`internal/engine/auth.go:455`). Nothing rotates the DEK†. Host-key
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
history. Add recovery-key re-enrolment under both factors before 1.0: it is
one more `recoverykey` write, and F2's portability depends on it.

**Cost.** Now: a decision, a paragraph and one drill. Later: the same
mechanisms, plus every client that never met a `compacted`.

### F5. The function body ABI is unversioned and under-isolated

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
  and off Linux a body runs unconfined (`sandbox_other.go:10-21`). There is
  no memory or process ceiling: a body that reaches the container's limit
  "takes the substrate down with it" (`docs/functions.md:563-568`).
- Every function of a bundle receives every account's live token
  (`internal/engine/invocationconfig.go:141-178`).
- A delivery gets three tries inside about 125 ms and then parks
  (`internal/engine/functions.go:57,67`), and a parked record or webhook
  delivery then waits for a person†. A schedule fires every occurrence it
  missed, however long the gap
  (`internal/engine/schedulecursor.go:62-66`).
- A body is handed a stable key per delivery (`functions.go:3063-3072`), so
  exactly-once toward an outside service is the body's job
  ([0125](../decisions/0125-a-provider-write-is-a-callable-that-writes-no-record.md)).
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
at boot where registration is open to anyone but the operator.

**Cost.** Now: one reserved key and a golden test. Later: the key is a
dialect event across every server a package is installed on.

### F6. A package cannot say which dialect it needs, and only the binary can rename a kind

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

### F7. Authority is enforced per writer class, and the classes protect different kinds

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
each writer class may do to each, held by a matrix test. Close the actor
header to a grammar. Add "sign out everywhere" and offer it on a password
change. Lease every stream. Correct `docs/auth.md`.

**Cost.** Now: a table and a test. Later: the gap is found by the first
published package that reaches for it.

### F8. API margins that would freeze as they are

*Demonstrated unless marked. Mostly fix before 1.0; each is small.*

| # | What | Evidence | Recommendation |
| --- | --- | --- | --- |
| 1 | PUT and PATCH ignore query parameters, so `?ifVersion=3` writes unguarded, while DELETE takes its guard exactly there | `internal/api/rest.go:122-167` against `174-195`; the stated rule is "never silence" (`rest.go:218-220`) | refuse by name, as the reads do |
| 2 | A failed write's status does not tell its fate. A commit in doubt answers the commit's own error, a 500†, and may have landed; a mirror failure after commit answers 503 with `Retry-After` and has landed | `internal/engine/dataset.go:876-885,979-1001`; [0165](../decisions/0165-a-commit-in-doubt-is-caught-up-by-the-next-write-and-no-write-grant-names-the-owners-auth-kinds.md) | give both a `reason`; accept `Idempotency-Key` on PUT, PATCH and DELETE, which today take none (`internal/api/core.go:32-38`) |
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

### F9. Operating limits: one process, one boot, shared tables

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
  (`dataset.go:784-797`), then pays two fsyncs and a commit with no group
  commit. A sync page holds the lock for its whole apply; owner writes jump
  the queue but cannot shorten it†. Throughput is unmeasured: there is no
  write benchmark†.
- **Shared tables.** Each kind's reference properties become partial indexes
  on the one `records` table (`internal/engine/indices.go:395-401`), and
  imported samples are rehomed per repository, so the count grows with
  repositories times kinds. The only `DROP INDEX` statements are for scratch
  and replaced builds (`indices.go:141,224,240`). One vector index serves
  every repository†.
- **Language and width.** Search stems English
  (`internal/engine/rows.go:210-212`) and vectors are 1536 wide
  ([0026](../decisions/0026-embedding-vectors-are-1536-wide-or-refused.md)).
  Both are derived and rebuildable, so both are *safe to evolve later*.

None of this is a public promise, which is the point: the tables are
internal, so a hosted service with thousands of repositories is rework
behind `internal/substrate`, not a break. It stays that way only if no
runbook teaches an operator to query the tables.

**Core versus packages.** `core` currently names vendors: nine contact
mappings for Apple, Google, Slack, GitHub, Linear and Beeper†. The seeded
assistant, its prompts, the model price list and the console's views are
product content. They should sit outside the stability promise explicitly,
with the one rule that already protects the owner: a shipped record is
never refreshed over an owner's edit.

### F10. Value semantics that freeze into data

*Mixed; each row says which.*

| What | Evidence | Why it is expensive later | Class |
| --- | --- | --- | --- |
| `records.kind`, `id` and `title` take the database's collation; the observed index already pins `"C"` because "the database's own collation … orders case and punctuation differently from Go" | `0001_init.up.sql:86-106`; `0013_observed_index.up.sql:16-20` | the primary key and every keyset walk depend on the host's locale library; repinning rebuilds the largest index | fix before 1.0 |
| A `datetime` is stored in UTC and its written offset is discarded; there is no zoned, time-of-day or partial-date type | `internal/engine/validate.go:526-531` | the offset cannot be recovered from history; a birthday or "08:00 wherever I am" has no honest type | decide and document; reserve the datatypes |
| `timezone` accepts whatever `time.LoadLocation` does, which includes `Local` and the empty string and depends on the host's zone data | `validate.go:559-562` | stored values stop meaning the same thing on another host | fix before 1.0 |
| `null` always deletes, and a clear also frees the property for a mapping to refill, so "explicitly none" cannot be stored† | `internal/engine/write.go`† | a cleared value that reappears after a sync reads as a bug; changing it changes every client | decide and document |
| `unique` is parsed and inert; the docs show it deduplicating people† | `internal/vocabulary/types.go`†, `docs/vocabulary.md`† | enforcing it later meets stored declarations and data that already violate it | remove from the docs, or implement before 1.0 |
| A conversion rewrites at most 10,000 records, in one transaction | `internal/engine/convert.go:104-107` | a shipped rename on a large seeded kind is refused at every boot, leaving a new binary on old declarations | safe to evolve later; raise before it bites |
| A value set added to an existing property is not counted as a narrowing; only removed values are | `internal/engine/schemadiff.go:544` | records the declaration no longer admits are served and cannot be re-applied | fix before 1.0 |

## Defects found in passing

Not design findings. Each was found by reading and none was reproduced.

| Where | What |
| --- | --- |
| `internal/engine/shippedupgrade.go:182,189` | The boot upgrade's kind-move guards are appended and then assigned over, so they never refuse. |
| `internal/api/rest.go:122-167` | F8 row 1. |
| `internal/engine/rows.go:758-759`, `rebuild.go:80-82` | A rebuild replays each purge, which deletes the embeddings of that id. The fold never enqueues an embedding (the live write does, `write.go:1414`), and the reconcile runs only on import (`repodir.go:812`). A record purged and created again loses its vectors after a rebuild, where the comment says they are kept. |
| `docs/api.md:2500`, `web/console/src/lib/api/types.ts:12-27`, `internal/api/errors.go:22-24` | Three statements of the closed error set, none equal to the fifteen constants. |
| `internal/engine/policy.go:23-26` | The header states a rule the effects path does not enforce (F7). |
| `docs/auth.md:308-311` | Claims a leaked token cannot outlive its revocation (F7). |
| `kinds/substrate.reamde.dev/authority.yaml:5` | Calls a live trust root a placeholder (F2). |
| Decisions 0050, 0052, 0062 | Confirmation names four tests that no longer exist. |
| `internal/api/appsetup.go`† | `POST /authorizations/check` is said to unwrap the scoped dataset and trigger a publisher refresh for a package the token does not name. Not re-read. |

## Direct verdicts

**The changelog.** Justified, and the most valuable design in the tree, as
a *model*: value-carrying deltas, one fold, seq as the one order. Watch,
resume, triggers, the delivery ledger, conversions, before-and-after values
and restore all stand on it, and rows plus an audit table would give up the
gapless cursor and the proof that a backup replays. The *file mirror* is the
half worth being precise about. Postgres is the commit point and serves
every read; the files cost two fsyncs per write, a reconcile surface of
about 1,700 lines in `repodir.go`, the commit-in-doubt states and an
all-or-nothing boot. They buy the one thing a shared table under row level
security cannot: a per-person backup and exit that needs no database. For a
product whose promise is "the directory is your data", keep it. Describe it
as what it is, a synchronous mirror that a restore treats as authoritative,
and spend the effort on F3 and F4.

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
rest, and the value semantics in F10. None needs a redesign.

**The database.** Private, which is its best property. Row level security
under a role that cannot bypass it is a stronger boundary than convention
in each query. Pin the collation of the identity columns, enforce the
minimum Postgres version at boot instead of in a doc†, and keep the tables
out of every operator procedure. Everything else in F9 can change without
anyone outside noticing.

## A proposed stability policy

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
   - Stored format: a new line key or effect raises the changelog dialect;
     a binary reads every dialect at or below its own, for the life of the
     major version.
   - Function ABI: an SDK version, and a supported Python minor range.
4. **Upgrade.** Any 1.x binary opens any database and data root written by
   1.0 or later, in one step. Migrations only go forward. A repository
   migration is carried until 2.0.
5. **Rollback.** Not supported past a first boot that stamps or migrates.
   The way back is the snapshot taken before. Each release says whether it
   closes rollback.
6. **Deprecation.** Announced in discovery and the changelog; a stable
   surface is served for at least two further minor releases after.
7. **Outside the promise, by name.** The Postgres tables, the Go packages,
   table output of the CLI, the seeded assistant, console layouts, model
   lists and vendor mappings.
8. **Nothing on `main` is `proposed`.** A record is accepted or the code is
   not there.

## The release gate

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

Targeted validation:

1. Released data-root fixtures, imported under the current binary (F3).
2. A boot with one damaged repository among several: the others serve.
3. A process-kill crash test at each step of `commitAndMirror`; today's use
   fault seams†.
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

## Questions only the owner can answer

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
6. When a second person arrives, is the answer a second repository and
   sharing between them, or a second principal in one? It decides whether a
   subject is reserved now.
7. Is a one-way upgrade acceptable for the life of 1.x?
8. Is Python the only body runtime for 1.x, and is its minor version part
   of the contract?
9. What size must one repository reach in 1.x: is 10 million records or
   100 GB in scope?
10. Are Moss, the console's layouts and the model lists part of what 1.0
    promises, or product that ships beside it?
11. 0017 rules out offline multi-writer and the apps keep an outbox. Is
    that the permanent answer for a product that lives on phones?
