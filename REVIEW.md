# Pre-release review: the state of the design and the last work before people get this

Date: 2026-08-16. Scope: the whole system as of `main` (8c9ed45). Method: seven
categories, each reviewed independently by two models (Claude Opus 5 and Codex
gpt-5.6-sol) against the code, findings merged here and cross-referenced
against the 69 open issues. The merged sections below carry every finding
with its evidence (the raw per-reviewer reports live in the review session's
gitignored `.dev/review/`, not in this tree). Where the two reviewers
disagreed on severity the section says so and gives the adjudicated priority.

Priorities, as settled with the owner after the reports landed: **P0** must
land before this goes to people, **P1** before it is called stable, **P2** is
optional. Findings marked *(both)* were reported independently by both
reviewers. The per-category sections below keep each reviewer's original
priority; where that differs from the agreed list at the top, the top wins.

Sections: [1 Architecture](#1-architecture-and-protocol) ·
[2 Security](#2-security) ·
[3 Data and migrations](#3-data-model-storage-and-migrations) ·
[4 Code](#4-code-structure-and-quality) · [5 Testing](#5-testing) ·
[6 Docs and naming](#6-documentation-and-naming) ·
[7 DX and operations](#7-developer-experience-and-operations)

## The verdict

The core is genuinely good and every reviewer said so independently: one
changelog order, one fold, value-exact hashing, RLS-enforced isolation, an
auth stack built the hard way, a disciplined codebase with a test suite that
attacks its own chain. Nothing found asks for a redesign. What is not ready
is everything at the edges: the shipped deployment undoes the cryptography,
the docs promise recovery and rollback paths that do not exist, the REST
grammar has a data-losing bug, several shapes about to freeze (actors, edge
properties, `required`, `account`, the error-code set) are wrong or
unenforced, and there is no license, no backup procedure, and no reachable
operator hat in the artifact people would actually run. The right posture is
exactly what was asked: stop adding features (defer the vocabulary-migrations
program, the agent features, scoped tokens), delete the scaffolding (~18k
lines: the `kinds:gen` pipeline, the dialect-1 rung, the insecure signing
switch, the seed disclosure), and spend the remaining effort on the P0 list
below. One feature is the exception, on the owner's call: blob bytes move out
of Postgres, because the backup story depends on it.

## Decisions taken while reviewing (2026-08-16)

Settled in discussion after the reports landed. Each needs an ADR before it
is built; they are recorded here so the reasoning is not lost.

1. **Actors stay engine-derived, never registered or self-declared.** A
   bundle or function cannot name its own actor: the string drives the policy
   door and trigger echo suppression, so it must be verified, and registration
   only moves the collision to install time. Derive from the full identity:
   `bundle:<authority>` and `function:<authority>:<name>`, with the colon as
   the separator because the slash is reserved by the `<actor>/<name>`
   metadata-key grammar. Retire `connector:` as a second spelling of
   `bundle:`. Kubernetes composes `system:serviceaccount:<ns>:<name>` the same
   way and for the same reason; its self-declared `managedFields` manager is
   the counter-example that proves the rule, because nothing security-critical
   reads it. (P0 item 8)
2. **Drop plurals.** A kind's plural is a second name for a name that already
   exists, unique per authority and frozen at release. Removing it makes the
   URL and the reference value the same string, which they are not today
   (`.../people/abc` versus `people.substrate.reamde.dev/person/abc`).
   atproto's collection segment is the NSID itself for the same reason.
3. **The path grammar changes once, not three times.** The verb separator,
   the plural removal and the repository-local dispatch all break the same
   URLs; they land as one ADR and one migration. Target shape:
   `/api/v1/{authority}/{name}/{id}` for records, a reserved segment for
   verbs, so that a verb added after v1 can never steal an id. (P0 item 7)
4. **Delete the direct function call route.** `POST
   .../functions/{name}/call` has one consumer in the tree
   (`substratectl function call`), writes no run record, re-runs effects on
   retry, and drops the function's own logs. `triggers/{id}/run` already
   gives function authors a debug path through the real delivery machinery,
   with a run record. Functions exist to react to records.

5. **Agents get no bespoke API.** Their whole state is already records
   (`llmthread`, `llmmessage`, `llminteraction`, `run`, `agent`), the
   continuation already runs off `notifies` (ADR 0003) and asks are already
   records (ADR 0004). Invocation is a record write; reads are REST, GraphQL
   and `watch`. Delete `agents/{name}/call` and `agents/{name}/chat`. The one
   thing records cannot express is token-level output of an in-flight
   generation, because the changelog is hashed and permanent so partial
   tokens can never go in it; **token streaming is not a v1 requirement**, so
   the console renders messages as they commit. If real use later demands it,
   it arrives as a generic ephemeral-progress channel keyed on a record, never
   as an agent-shaped route. #71 (GraphQL reference inverses) stops being an
   agent problem and becomes what it is: a gap in the general query surface.

6. **`required` means required, and `default` is applied at write.** The
   vocabulary is a contract the server keeps, not documentation clients
   interpret: every other part of the system (the narrowing guards, the fold,
   the GraphQL projection, the policy door) already treats it that way, and a
   decorative `required` was the odd one out. 107 property declarations use
   `required` today and only links enforce it; 14 declare a `default` that
   nothing applies. Four consequences to write into the ADR:

   - **The record must satisfy the rule after the write**, not "the patch must
     carry the property". A patch that leaves a required property present is
     fine; one that would clear it is refused.
   - **A default materializes into the stored value and into the changelog
     delta at write time.** It is never filled in at read, because a value
     that exists only on the way out is derived data and the fold stops being
     the truth. Changing a default later therefore does not change any
     existing record, which is the correct behavior.
   - **Adding `required` to an existing property stays refused** while live
     records lack a value, exactly as `schemadiff` refuses it now. A default
     does not rescue it, because defaults do not backfill. That is the
     add-and-deprecate path working as intended; bulk backfill stays with the
     deferred migration verbs.
   - **Enforcement cannot turn on while violators exist.** Before it ships,
     scan for records that violate their kind's `required`, and check the
     provider mirror kinds for properties the provider does not always send,
     which would start refusing syncs.

7. **Add `beta` and re-stamp every feature honestly.** `StabilityAlpha` and
   `StabilityStable` are the only two values, so `features()` hard-codes
   `stable` for everything except agents, at v0.8.0, including surfaces this
   review is about to delete or restructure. Add a third value, and stamp
   `stable` only where the shape is genuinely frozen for v1. On the current
   decisions that means roughly: `changefeed` and `blobs` stable once the
   `/changes` continuation marker lands; `triggers` and `bundles` beta until
   the reserved verb segment and the status fold ship; `functions` beta (the
   direct call route is being deleted); `search` beta and `embeddings` beta or
   absent, since neither has a REST route and the embedding store has no model
   identity; `agents` stays alpha. Compute the list from the seams actually
   present on the dataset rather than hard-coding it, so a feature cannot be
   advertised without a route. `docs/agents.md` stops telling readers to build
   on a frozen core, and `docs/graphql-and-search.md` states that the GraphQL
   schema is a per-repository projection and is not frozen at all.
   (P0 item 15, #129)

Also filed: [#194](https://github.com/geoah/substrate/issues/194), whether an
authority may ever contain path segments (raw `/`), as research rather than
release work.

**One decision still open**, surfaced while reconciling the tracker rather
than by the reviewers: **#116**, the policy `selector.kinds` grammar. It is
exact-match only, while the trigger source (its closest analog) already admits
`<authority>/*` and `*`. The first policy anybody writes is "gate everything
an agent writes under `tasks.*`", which today is either an empty list
(everything) or an enumerated snapshot that silently misses the next installed
kind. Retrofitting wildcards into stored exact-match strings changes behavior
on live data silently, so it is a freeze decision, on the surface that carries
the product's safety claim. The cheap answer is to adopt the trigger source's
existing grammar rather than invent one. Its second half, marking `action`
required so the engine stops silently skipping actionless rules, falls out of
decision 6.

## The v1 API surface, after reduction

The aim is the smallest surface that can be frozen. Everything below is one
decision list; the path-grammar ADR (decision 3) carries it.

**Delete** (no replacement, the capability exists elsewhere or is not v1):

- `POST {core}/functions/{name}/call`: `triggers/{id}/run` is the debug path.
- `POST {core}/agents/{name}/call`, `POST {core}/agents/{name}/chat`.

**Fold into records** (the operation stays, the bespoke route goes):

- `POST {core}/recordmerges`, `POST {core}/recordsplits`: `recordmerge` and
  `recordsplit` are shipped kinds; creating the record is the operation, and
  the collision that makes those collections unreachable disappears. Cheap.
- `GET {core}/bundles/status`, `GET {core}/bundles/{id}/status`, and the
  trigger status verb: the envelope's server-owned `status` key already
  carries computed state. Cheap.
- `POST {core}/bundles/{id}/bind`: an input binding is an edge; writing it
  is a record write. Cheap.

**Do not fold, despite looking foldable.** The bundle lifecycle verbs read
like state edits and are not. `DisableBundle` does patch `disabled: true`,
but under the exclusive `lifecycleFence`, so that every admitted invocation
has committed before it returns; a plain `PATCH` takes no fence and would
silently drop the drain. `EnableBundle` adds a precondition (it refuses
during or after an interrupted purge). `UninstallBundle` is not a property
write at all: it is an `applyVocabularyBatch` with `replaceAuthorities` and a
guard that tears down the authority's callable triggers first. `PurgeBundle`
takes the fence, guards on the bundle being blocked, writes a `purging`
marker, runs an ordered multi-phase teardown (accounts first, the OAuth
client input's kind last so revocation runs against a live client), returns a
count, and is resumable through the standing marker.

Folding those would require transitions that carry a concurrency barrier,
transitions that execute a multi-document vocabulary batch, and transitions
that start a resumable job and return a count: new engine machinery, for no
gain. Uninstall and purge are verbs in the same sense `vocabulary/apply` is a
verb (one of them is one). Keep all four under the reserved segment; revisit
disable and enable only if a fence-taking state transition is ever built for
another reason.

**Audit before freezing** (defensible either way, decide once):

- `GET {core}/traits/{id}/implementors` and `/records`: both are queries. If
  the filter grammar can express "kinds implementing trait X" they are
  collection reads, not routes.
- `POST {core}/catalog/{id}/install`: installing is applying the closure
  through the same admission path as `vocabulary/apply`; it may be that verb
  with a catalog reference rather than its own.
- The trigger verbs `replay` and `wake`: genuine operations (a cursor reset,
  an immediate scan), but they are the template for every future verb, so
  they should be the ones that prove the reserved segment works.

**Keep, irreducibly:**

- Record CRUD, `incoming`, edge link/unlink (the restructured record tree).
- `GET {core}/changes` and the watch stream: the log is not a collection.
- `POST /graphql`: a query transport.
- `POST {core}/vocabulary/apply`: the multi-document transaction boundary.
- `PUT`/`GET /blobs/{digest}`: bytes, ranges, `413`.
- `GET {core}/catalog`, `GET {core}/catalog/{id}`: shipped in the binary,
  not repository records.
- `POST {core}/oauth/start` and `GET {core}/oauth/callback`: an external
  protocol; the callback carries no bearer by design.
- The auth routes (`register`, `login`, `password`, `totp`, `tokens`,
  `recovery/enroll`), which should also gain a version prefix or be fully
  listed in discovery's `endpoints` block.

## The P0 list

Agreed with the owner on 2026-08-16, going through the review's original 21
items one at a time. Five were demoted to P1 (TLS, LICENSE, backup and
restore, the version gates, the narrowing race) and one was promoted from the
deferred set (blob storage, because backup depends on it). Section links carry
the evidence.

**Deployment**

1. Compose mints a credential key into a named volume rather than booting
   without one, sets `SUBSTRATE_SANDBOX=enforce`, and moves the `postgres`
   password and `let-me-in` invite code to a laptop-only override. Delete
   `SUBSTRATE_INSECURE_ALLOW_INVALID_SIGNATURES` outright: the env var, the
   config field, the engine option and the keyless branch in `settleChain`,
   so unsigned history is impossible rather than discouraged. Placeholder
   signatures stay readable only below `signed_from_seq`. (#175, widened)
2. The credential key gets a format floor (32 bytes in one exact encoding,
   refused at boot with the accepted shape named in the error), a
   `substratectl` command that generates one so the operator is never inventing
   key material by hand, and an operator rotation verb that re-wraps every DEK
   and signing seed in one transaction. #133's key id rides in the sealed
   frame as part of it. (#99 split, #133 folded in)
3. `substratectl` ships inside both runtime images, so
   `docker compose exec substrate substratectl --dsn "$DATABASE_URL" …`
   reaches `verify`, `reseal` and `user reset` with no exposed Postgres port.
   For v1 the operator hat is the only lockout escape, stated plainly in the
   docs rather than fixed with a new recovery route. (untracked)
4. Blob bytes move out of Postgres into a filesystem store, with an S3
   backend behind the same interface. This is a prerequisite for the backup
   procedure: a database dump must not carry blob bytes, and the backup doc
   must tell the operator where the bytes live and that they are backed up
   separately. (#97, promoted from the deferred set)
5. `SECURITY.md` with a private reporting address. No LICENSE and no
   `CONTRIBUTING.md` in this pass.

**What freezes**

6. The path-grammar ADR and its one migration: reserved verb segment, plurals
   dropped, the repository-local dispatch settled, the silent-create
   dispatch bug fixed, and the API surface reduction (delete, fold, keep)
   below carried with it. One break, then freeze. (#131 split: OpenAPI after)
7. Actor identity: `bundle:<authority>` and `function:<authority>:<name>`,
   derived by the engine from the full identity, `connector:` retired.
   (untracked; #51 partial)
8. `required` is enforced and `default` applies at write, with the four
   consequences in decision 6 above. Fold #170's datetime bound into the same
   validation pass. (untracked, #170)
9. Refuse a non-empty edge `props` map on the generic link surfaces while it
   is still free, reserve `edges.<rel>.properties`, `unique` and `deprecated`
   in the same loader change, and add edge removal, `many` and target changes
   to the narrowing classifier. (#110, #111)
10. `account` stops naming two things. Both mechanisms are correct and stay:
    the required `ownerRef` edge on the provider-sync containers (which is
    what makes disconnect cascade) and the provenance string the connector
    stamps on synced rows (deliberately not a reference, since retyping is a
    refused narrowing). Rename the provenance property so one word is not two
    shapes, while the only store that exists is the owner's and the rename is
    nearly free. A user-authored calendar or conversation is not a v1 kind;
    the shipped kinds are honestly named provider containers. (#124, rewritten:
    its "choose one representation" framing is wrong)
11. Embeddings store a provider and model fingerprint, it becomes part of the
    cache key so a mismatch re-embeds, and an operator verb clears and
    refills the queue for a repository. (#98 split; the per-repository
    provider design stays deferred)
12. Add a `beta` stability value and stamp every feature honestly, computing
    the list from the seams present on the dataset rather than hard-coding
    it. (#129)
13. The extension tier gets named optional interfaces in
    `internal/substrate` and `var _` assertions in `internal/engine`.
    (untracked)

**The process**

14. Detached goroutines (judge, thread resume, function prep): one spawn
    helper with a WaitGroup and shutdown context, `recover` in both families,
    `defer tx.Rollback()` in `inTx`. (untracked)
15. Release workflows publish only commits with a green `ci` for that exact
    commit; manual tagging and direct tag-push publishing go, and `latest`
    triggers from `ci` completion. (untracked)
16. `SUBSTRATE_TEST_REQUIRE_SANDBOX=1` turns the confinement skips into
    failures, set in `ci:go` and `ci:race`; the plain skip stays the laptop
    default. (untracked)
17. One conformance file under `internal/testenv` drives the record lifecycle
    over real HTTP against the real engine, asserting one case per published
    error code. The fake stays for handler and failure-injection tests.
    (untracked)

**The docs**

18. The truth-in-docs pass, last, so each page describes the finished system
    once: the quickstart and signing claims, the rollback claim (which now
    says downgrade is unsupported, since the gates are P1), the recovery
    claim, the backfill claim, the `recordpatchpolicy` section, the fifth
    host function, `data-model.md`'s `title` guidance, `gated` in the closed
    error set, and the reference drift.

### Where each P0 item lives

Filed on 2026-08-16, all on the `v1.0.0` milestone with `priority/p0`. The
milestone holds 22 P0 and 14 P1 issues and nothing else; everything deferred
past stable was taken off it.

| Item | Issue |
| --- | --- |
| Compose mints a key, sandbox enforced, insecure flag deleted | [#175](https://github.com/geoah/substrate/issues/175) |
| Credential key floor, generator, rotation (#133 folded in) | [#99](https://github.com/geoah/substrate/issues/99) |
| `substratectl` in both runtime images | [#211](https://github.com/geoah/substrate/issues/211) |
| Blob bytes out of Postgres (filesystem, then s3) | [#97](https://github.com/geoah/substrate/issues/97) |
| SECURITY.md | [#213](https://github.com/geoah/substrate/issues/213) |
| Path grammar: verb segment, no plurals, no silent create | [#202](https://github.com/geoah/substrate/issues/202) |
| Actor identity on the full authority | [#203](https://github.com/geoah/substrate/issues/203) |
| `required` enforced, `default` at write | [#204](https://github.com/geoah/substrate/issues/204), [#170](https://github.com/geoah/substrate/issues/170) |
| Edge properties refused, dialect keys reserved | [#111](https://github.com/geoah/substrate/issues/111), [#110](https://github.com/geoah/substrate/issues/110) |
| `account` names two things (**needs human review**) | [#124](https://github.com/geoah/substrate/issues/124) |
| Embeddings model fingerprint and reindex verb | [#98](https://github.com/geoah/substrate/issues/98) |
| A `beta` stability value, computed feature list | [#205](https://github.com/geoah/substrate/issues/205) |
| Extension-tier interfaces and assertions | [#206](https://github.com/geoah/substrate/issues/206) |
| Goroutine lifecycle and `defer Rollback` | [#207](https://github.com/geoah/substrate/issues/207) |
| Releases publish only green commits | [#208](https://github.com/geoah/substrate/issues/208) |
| Sandbox tests fail instead of skip | [#209](https://github.com/geoah/substrate/issues/209) |
| Real-engine HTTP conformance suite | [#210](https://github.com/geoah/substrate/issues/210) |
| Truth-in-docs pass | [#212](https://github.com/geoah/substrate/issues/212) |
| Delete the pre-release scaffolding | [#217](https://github.com/geoah/substrate/issues/217) |
| Policy `selector.kinds` grammar (**needs a yes or no**) | [#116](https://github.com/geoah/substrate/issues/116) |

The three demoted items are [#214](https://github.com/geoah/substrate/issues/214)
(TLS and security headers), [#215](https://github.com/geoah/substrate/issues/215)
(LICENSE) and [#216](https://github.com/geoah/substrate/issues/216) (backup and
restore, blocked on #97), all `priority/p1` on the same milestone. Closed
during the pass: #103, #158, #47 and #129 (folded into #205).

## Deletions (do these, they are negative work)

- The `kinds:gen` pipeline (~15k lines nothing imports), and with it
  `internal/kinddialect`, unless its consumers get wired instead.
- The dialect-1 promotion rung (3,367 lines reading a shape no release
  wrote); consider folding migrations 0002-0006 into 0001 before the first
  tag.
- The insecure-signatures switch and the registration-response signing seed
  (return the public key).
- The three deleted routes: `functions/{name}/call`, `agents/{name}/call`,
  `agents/{name}/chat`, and the folded ones (merge, split, bundle status,
  bundle bind).
- The dead vocabulary in `internal/substrate` and the 277 dead-word
  identifiers; the 85 citations of deleted documents.

## Suggested order

1. Deployment items 1-3 and 5, plus 14 and 15. Small, independent, and
   everything after is tested against an honest deployment.
2. Item 4 (blobs out of Postgres), which unblocks the P1 backup procedure.
3. The freeze work: item 6 carries 7 and the surface reduction; 8-13 are
   mechanical now that the decisions are made.
4. Items 16 and 17, then the deletions, which shrink what the docs describe.
5. Item 18 last.

## The P1 list: before this is called stable

The five demoted P0s come first, because each is a promise the docs or the
wire currently make.

1. A TLS and exposure contract: loopback default, one documented reverse-proxy
   termination path, a refusal to bind publicly in cleartext without an
   explicitly named setting, and the security-header middleware (HSTS, CSP
   `frame-ancestors 'none'`, nosniff, no-referrer) with a nonce for the OAuth
   return page. Until it lands, the docs say remote exposure is unsupported.
2. A LICENSE, with `org.opencontainers.image.licenses` set and the file in
   every release archive.
3. The backup and restore procedure, after item 4 above: exact dump and
   restore commands, the credential key stored separately from the dump, the
   blob store named as a separate artifact, a restore ending in
   `repository verify`, and one tested round trip. (#137's restore tool and
   #136's export half fold in here.)
4. The version gates at open: refuse a newer `schema_migrations` version, a
   newer changelog dialect or an unknown chain domain; no-migrate opens for
   operator reads; rebuild refuses unreplayable legacy entries before
   deleting a fold table. (#104 widened, #159's writer gate folded, #106)
5. The narrowing race: the registry-dependency lock taken shared on every
   record write that resolves a kind. (#150 invariant 2)

Then, grouped by theme:

**Wire and contract.** Operational lists get the page envelope (#127); the
gated `409` carries a structured `heldAs` and validation problems get
structure (#128); `POST` retries stop duplicating records and re-running
effects (#130); the watch cursor guarantees and ndjson control frames get
documented, with the `/changes` continuation marker and the GraphQL horizon
and `hash` gaps fixed (#144, #127); `wire.golden.json` widens to every shape
the console mirrors, recording required versus optional, and the known `Page`
and `BundleStatus` drift is corrected (#131 split); the request id reaches the
error envelope, an `X-Request-Id` header and one completion log line per
request; the auth routes get a version prefix or a complete discovery
`endpoints` block.

**Engine correctness.** `recordScan.finish` returns its JSON errors and the
`rows.Err()` checks land in `gc.go` and `search.go`; declared indexes are
named from their definition rather than their ordinal, and orphans are
dropped; constraint tightening (`pattern`, `min`, `max`) counts live
violations like other narrowings; `fts` leaves the byte-for-byte fold snapshot
and the rebuild is documented as the sanctioned reindex (#105); the rule for a
migration that edits `records` outside the fold is written and tested (#106);
signing key rotation (#147).

**Vocabulary.** Property descriptions stop being jargon in console form help
(#49); the core-kind declaration gaps (#51); the renames before they freeze
(#125); the `occurrencelog` and `recurring` trait extraction (#122); provider
timestamp names and types (#143); `permissions.network` becomes the honest
boolean grant it already is (#50, rewritten); trigger label properties (#118);
`run` stores references rather than bare strings (#117); the `deprecated` and
retirement markers that #146 shrinks to once #110 lands.

**Code and tests.** The dead vocabulary rename in `internal/substrate` and the
277 identifiers; a scheduled `-race` job over `./internal/engine/...`; the
sleep-based lock-order barriers replaced with a deterministic wait; tests for
`internal/oauthflow`'s state HMAC, `internal/config`, and one `gql.BuildSchema`
over the shipped registry; the `test:db` package allowlist guarded; `errcheck`
enabled; the 85 citations of deleted documents removed.

**Operations and DX.** The env contract settled and fully documented, with
`SUBSTRATE_SANDBOX` moved into `Config` and the prefix rule decided (#164,
rescoped); compose selects a published image tag with the source build behind
an override, and the release archives and image are named in the docs; a
readiness endpoint that pings Postgres, with the container healthcheck moved
to it; the function cache persisted in compose and retired installation
directories evicted; function logs returned from a direct invocation path and
kept on the `run` row for parked deliveries; `apply -f <dir>`; ADR #134
(repository id is identity) and a paragraph for #106; #138 reduced to a
one-line record, plus a default expiry on login-minted tokens.

## Open-issue dispositions

**Do now (the P0 set):** #175 (widened to the compose default), #99 (split:
the key floor and the rotation verb), #133 (folded into that rotation), #97
(promoted: blobs out of Postgres), #131 (split: the path grammar now),
#110, #111, #124 (rewritten per item 10), #98 (split: the model fingerprint
now), #129, #170 (folded into the validation pass), #51 (the actor half only).

**Do before stable (P1):** the five demoted items above (#104, #159's writer
gate, #106, #150 invariant 2, plus the untracked TLS, LICENSE and backup
work), then #127, #128, #130, #144, #137 (+#136's export half), #138, #147,
#134, #49, #51, #125, #122, #143, #164, #50, #118, #117, #146 (shrunk), #105,
#142 (its bundle-state bullet rides with the status fold in P0 item 10: three
independent booleans can express states the verbs never produce, and ADR 0019
wants one state property; the rest of #142 is additive).

**Defer past stable:** #151, #152, #155, #156, #157 (the vocabulary-migrations
verbs), #153, #148, #135, #136's delete half, #55, #54, #67, #68, #47 (close
into #68), #69, #70, #71, #74, #75, #76 (the agent surface), #6 (rescope to
the scheduled job), #77 (the mechanical gate only), #159's backfill half.

**Close:** #103 (wontfix per ADR 0017; three reviewers concur), #158
(wontfix), #166 (strike the stale reseal bullet, fold the rest), #53 (rescope
to the `vocabularydiff` added-declaration gate and the surviving test-only
symbols; six of the nine it names are already gone). The trackers #100, #107,
#112, #119, #126, #132, #164, #165 get updated to the dispositions above or
closed as their children resolve.

**New tickets needed** (untracked work above): the path-grammar ADR and
migration, actor identity, `required`/`default` enforcement, the `beta`
stability value, the extension-tier interfaces, the detached-goroutine
lifecycle, the release-pipeline gate, the sandbox test gate, the HTTP
conformance suite, the truth-in-docs pass, `substratectl` in the image, the
credential-key generator, TLS and security headers, LICENSE, the backup
procedure, the request-id correlation, the readiness endpoint, and the
scaffolding deletions.

---

## 1. Architecture and protocol

State: the center is sound and both reviewers said so in the same words. One
changelog order, one fold, RLS-backed isolation, a value-exact hash preimage,
one closed error-code set, strict JSON everywhere. What is not ready is the
surface around it: the REST grammar has live bugs, the extension tier has no
declared contract, the vocabulary declares constraints the write path does not
enforce, and discovery is a hardcoded constant.

### [P0] The REST path grammar has live bugs and must be settled before it freezes *(both)*

- `POST /{plural}/{id}` against a repository-local kind resolves the
  collection, discards the id, and creates a record with a random id,
  answering `201` (`internal/api/rest.go:52-65` dispatches on "authority
  contains a dot", but `POST` is bound unconditionally to `createInCollection`,
  `internal/api/api.go:284`). The mirror: `PUT /{authority}/{plural}` is a
  create with a random id. A client that thinks it is upserting silently
  accumulates duplicates. This loses user data today (Opus).
- A published record with id `incoming` is unaddressable; `recordmerges`,
  `recordsplits`, `triggers/status`, `bundles/status` and the `blobs`/`graphql`
  plurals shadow real collections, and every verb added after v1 removes ids
  from the addressable space (both). `incoming` is valid under the record-id
  grammar (`internal/vocabulary/naming.go:35`).

Fix: route all verbs through the `addressed` dispatch (`405` for a `POST` to a
record path), and decide the record/verb separator now, even if the answer is
a reserved segment. Related: #131 (correct direction, but it misses both the
silent-create bug and the `incoming` collision; split the path-grammar decision
out as its own pre-freeze item).

### [P0] The extension tier has no declared contract: ten runtime type assertions, no compile-time check (Opus)

`internal/substrate/dataset.go:12-20` says triggers, bundles, agents, blobs,
vocabulary apply and the change feed are not on the interface; `internal/api`
reaches them through ten unexported structural interfaces (~27 methods) with
zero `var _` assertions, and the API tests pin the degraded `501` branch.
Rename one engine method and an endpoint family disappears at runtime with a
green suite. Fix: move the interfaces into `internal/substrate` as named
optional extension interfaces and assert them in `internal/engine`. About a
day of work, no behavior change. Untracked.

### [P0] `required` and `default` on scalar properties are declared but never enforced, while admission refuses the transition (Opus)

`internal/vocabulary/types.go:170-175` admits it ("a form-level hint");
`checkRequiredPointers` (`internal/engine/write.go:1183-1240`) enforces
`required` for references and edges only; `default:` is applied by nobody.
Yet `schemadiff.go:231-262` refuses adding `required` as a narrowing, so the
system enforces the transition to a rule it never enforces. 30 shipped scalar
`required: true` declarations are advisory. Pick one before freeze: enforce
(and apply `default` at write), or rename the key to what it is and delete the
narrowing guards for it. Related: #118, #51 (both assume enforcement).

### [P0] Rebuild silently skips legacy entries without `payload.fold` (Codex)

`foldOpsOf` treats a missing `fold` field as an empty op list
(`internal/engine/fold.go:779`) and `foldRefuses` rejects only legacy
merge/split entries, so a rebuild of a repository holding v0 entries commits a
fold that silently omits those records. Fix: preflight replayability before
deleting fold tables, refuse unreplayable repositories, and add the changelog
dialect floor (one mechanism with the version gates in section 3). Related:
#104 (expand past unknown effects to cover v0 entries), #106, #100.

### [P0] Actor identity: callables collide on local names, and `connector:` is a dead word being hashed into history *(both)*

A function's actor is `function:<local-name>` (`internal/vocabulary/function.go:206`),
agents share the namespace, and two authorities with the same function name
share attribution and suppress each other's triggers (Codex). Separately,
`connector:<first-label>` is minted as an actor
(`internal/vocabulary/ref.go:194-199`, four shipped bundles declare one)
although `docs/terms.md` retired the word (Opus). Actor strings are hashed
into the chain (`chain.go:83`), so both spellings become permanent the moment
real repositories accumulate history. Fix before release: collapse the machine
hands to `bundle:` and `function:`, and key callable actors on the full
identity (or a stable encoding of it), not the local name. Related: #51
(names the declaration half only), #77, ADR 0014.

### [P1] `permissions.network` declares per-host patterns and enforces all or nothing *(both; Codex ranked P0)*

Any non-empty list grants full egress including loopback
(`internal/runner/policy.go:101-106`); the declaration reads as least
privilege and is not. Both reviewers converge on the same fix: make it an
honest boolean grant now, add host policy only when an egress proxy can
enforce it. Breaking once third-party bundles declare patterns, so it should
land before bundles are advertised; adjudicated P1 because bundles today are
first-party. Related: #50 (insufficient as written; do not freeze a hostname
grammar before enforcement exists).

### [P1] Discovery is a hardcoded constant and wrong in both directions *(both)*

`features()` (`internal/api/discovery.go:150-161`) advertises `search` and
`embeddings` as stable with no REST route, omits half the routed surface, and
the deprecated-alias fields are never emitted. Clients are told to branch on
this. Fix: compute it from the seams present on the dataset (trivial once the
extension interfaces are named), delete or route the phantom entries. Related:
#129 (too narrow; extend to embeddings, the static list, the dead alias
fields).

### [P1] `GET /changes` is three modes with three content types, and GraphQL's changelog obeys none of the rules (Opus)

The bare forward read truncates at 500 rows with no continuation marker;
the history page clamps silently; GraphQL `changelog` skips the retention
horizon and drops `hash`. The changelog is the read third parties integrate
against first. Related: #127 (add the marker and GraphQL gaps), #144.

### [P1] Operational responses have no named wire shapes *(both)*

Catalog, bundles, tokens and friends return anonymous endpoint-specific maps;
the gated `409` names the created request only in message text; POST retries
duplicate function effects. The semantics must be fixed in the v1 contract
even where implementation is additive. Related: #126, #127 (blocker per
Codex), #128, #129, #130 all accurate.

### [P1] One narrowing in any shipped authority silently blocks the boot upgrade for every authority (Opus)

`upgradeShippedVocabulary` (`internal/engine/seed.go:139-247`) refuses the
whole batch on one guard hit, surfaced only as one `log.Error`. Scope the
guard per authority and record the refusal where somebody sees it. Related:
#146 bullet 2 (buried in the migrations tracker; independent of it).

### [P1] `.substrate.reamde.dev` is load-bearing admission logic, and bundle identity keys on the authority's first label (Opus)

`OrgDomainSuffix` grants builtin status (bare GraphQL names) to anyone using
the placeholder domain, while a real third-party domain gets second-class
treatment; bundle names must be globally unique on the first label. Make
"vocabulary bundle" a declared property, key bundle identity on the full
authority. Related: ADR 0014, #55 item 3.

### [P2] Record the SDK compatibility stance (`ProtocolVersion` moves with the server); version or discover the auth routes; add `Head()` to `Dataset` (Opus)

All three are cheap now and awkward later. Untracked.

## 2. Security

State: the auth core is the strongest code in the tree (argon2id with
parameters in the hash, one constant-work `verifyFactors` chokepoint, TOTP
consumed under a row lock, RLS with FORCE, layered Landlock+seccomp sandbox,
a secret scrubber at the runner boundary, no secret in any log). The gap is
the deployment: the shipped defaults undo the cryptography, and the operator
contract (TLS, key handling, recovery) is unstated or untrue.

### [P0] The shipped compose boots keyless: plaintext DEKs and a permanently unsigned changelog *(both)*

`compose.yaml` defaults `SUBSTRATE_CREDENTIAL_KEY` to empty and
`SUBSTRATE_INSECURE_ALLOW_INVALID_SIGNATURES` to `true`; the README presents
`docker compose up` as the whole install. Keyless, `sealCredential` stores the
DEK effectively in plaintext beside the ciphertext it protects
(`internal/engine/credentials.go:207-216`): one `pg_dump` yields password
hash, TOTP seed, OAuth refresh tokens and every secret property. The unsigned
half is irreversible: history written with placeholder signatures stays
unsigned forever. `docs/operations.md:170-178`'s backup claim is false in the
shipped configuration. Fix: compose mints or requires a key
(`${SUBSTRATE_CREDENTIAL_KEY:?}`), then delete the insecure switch outright.
Related: #175 (widen it: it names neither the compose default nor the
plaintext DEK).

### [P0] The host credential key: no format floor, bare SHA-256, and no rotation path *(both; Opus P0, Codex P1)*

`deriveCredentialKey` is one `sha256.Sum256` over any string; compose says
"Any string works"; the wrap is written once (`adoptDEK`, `WHERE dek IS NULL`)
and nothing can ever re-wrap, so the value is set-once-forever and a weak or
leaked key has no path back. Fix: refuse non-key-material at boot (32 bytes,
one exact encoding, or a real KDF over a declared passphrase), and add an
operator verb that re-wraps every DEK and signing seed under a new key in one
transaction. Fold #133 (key id in the sealed frame) into this work; it is the
enabling byte. Related: #99 (split these two findings out of it), #147.

### [P0] There is no TLS or exposure contract *(both; Codex P0, Opus P1)*

`ListenAndServe` only; no doc requires a terminator; compose shows how to
publish the port. Login passwords, TOTP codes, bearer tokens, the signing seed
and the recovery identity all travel cleartext for an operator who follows the
instructions. Fix: document one supported TLS termination path as mandatory,
default the bind to loopback, and refuse public cleartext binding without an
explicitly named insecure setting. Untracked.

### [P1] No security headers, and the console keeps a full-power bearer in `localStorage` *(both)*

Zero CSP / `X-Frame-Options` / `Referrer-Policy` / HSTS anywhere
(`internal/api/api.go:121` middleware chain); the console is framable with
full authority; auth responses carry no `no-store`. One middleware fixes it;
the OAuth return page's inline script needs a nonce. Untracked.

### [P1] Server timeouts and the two unauthenticated pressure points *(both)*

Only `ReadHeaderTimeout` is set: body-Slowloris holds connections
indefinitely, idle keep-alives are never reaped, and eight slow blob uploads
occupy every slot. The OAuth callback is unauthenticated, database-touching
and unrate-limited. The auth limiter has no IP-only bucket, so one host can
run 32 concurrent Argon2 verifications at 64 MiB each, a ~2 GiB allocation
burst per refill interval (verified against `internal/api/ratelimit.go:46-50`
and the argon2id parameters; Codex ranked this P0, adjudicated P1 because it
is availability-only and the fix is a small semaphore plus an IP bucket).
Untracked.

### [P1] Function isolation gaps: shared uv cache, shared PID namespace, no resource limits (Codex; Opus rated the sandbox core strong)

`provisionPolicy` grants dependency-install code (PEP 517 build backends)
write access to the shared uv cache (`internal/runner/policy.go:158-186`,
verified); a hostile package can poison another function's cached environment
and inherit its grants. Bodies share the server's PID namespace and UID with
no memory or process-count limit. Adjudicated P1 while all bundles are
first-party; it becomes P0 the moment installing third-party bundles is part
of the pitch. Also: compose does not set `SUBSTRATE_SANDBOX=enforce`, so the
shipped default is `best-effort`, which degrades to unconfined execution
behind one log line on kernels without Landlock (both; fix in compose now).
Untracked; #50 does not cover it.

### [P1] The recovery promise has no tool *(both)*

`OpenPayloadWithKey` has no caller outside tests; there is no restore command;
the console never sends `recoveryPublicKey`, so the server always mints the
age identity and returns the private half over HTTP. The documented "backup
plus recovery key is complete recovery" is untestable by a user. Fix: one
`substratectl repository restore` that consumes a dump plus the age identity
(and rewraps under a new host key), and browser-side generation or an honest
sentence in the register flow. Related: #137 (upgrade "record as a stance" to
"fix"), fold the export half of #136 in.

### [P1] Login tokens never expire, record no use, and a bearer is arbitrary code execution (Opus)

Any token can `POST /vocabulary/apply` a bundle with inline function source
and invoke it. Give login-minted tokens a default expiry (the field and
enforcement exist), then write #138's ADR stating what "full access" includes.
Related: #138, #132.

### [P2] Smaller items

- Registration disclosure of the Ed25519 private seed serves no consumer;
  return the public key instead *(both)*. Related: ADR 0010, #147.
- Compose's remaining weak defaults: `postgres` password, `let-me-in` invite
  code baked in (Opus).
- `--token` puts a bearer in argv; give it the `--*-stdin` twin the house rule
  requires (Opus).
- Two runner error returns bypass the scrubber (`internal/engine/runner.go:168,342`);
  wrap for symmetry (Opus).
- Sealed payloads carry no AAD binding them to their row, so same-key
  ciphertexts can be swapped between records by a database attacker; bind
  repository/ref/kind/id as GCM additional data when the sealed-frame
  versioning (#133) lands (Codex; P1 there, P2 here because the attacker
  already holds the database and the swap is within one repository).

## 3. Data model, storage and migrations

State: the storage core can mostly freeze as it stands (RLS enforced by
Postgres, one fold write path, value-exact hashing, exact numbers). The holes
are at the seams: version gates, the narrowing race, derived stores with no
invalidation story, and a declaration dialect that is closed without reserved
keys. The vocabulary-migrations program (#146-#158, #165) is over-built for a
pre-1.0 single-user server; only one invariant from it must land now.

### [P0] Nothing refuses a store this binary cannot serve *(both)*

Three missing gates, one mechanism: (1) the migration runner never rejects a
`schema_migrations` version above the binary's, so a rollback serves a store
whose writes fail as raw not-null violations (migrations 0005/0006), while
`docs/operations.md:137-146` promises a refusal that only covers the
vocabulary dialect; (2) no per-repository changelog dialect, so an old binary
opens a changelog it cannot replay and fails only at rebuild (#104); (3) the
chain domain (`substrate/changelog/v1`) is versioned by name with no record of
where a repository's entries switched. Also: operator commands presented as
reads (`repository verify`) call `engine.Open` and apply migrations
(`cmd/substratectl/commands/operator.go:62`); give operator reads a no-migrate
open. Fix all as one gate at open with the `503 Retry-After` shape the
vocabulary ladder already has. Related: #104 (too narrow), #159's
writer-gate bullet (fold in), #103.

### [P0] A record write can commit a value the vocabulary apply just proved absent *(both, and both categories)*

Ordinary writes take no registry lock; only trigger writes take the shared
side. A write can validate against the old kind, block, and commit after the
narrowing count, storing a value the new declaration refuses; reads never
re-validate and the next apply is then permanently refused. This is a defect
in the guards that exist today. Fix: hold the registry-generation token from
kind resolution through commit on every record write (#150 invariant 2),
independent of the rest of the migrations program. Related: #150 (pull this
invariant forward; the transaction-scoped registry half can wait).

### [P0] Edge properties are accepted, stored and validated by nothing *(both; Opus P1, Codex P0)*

Three doors write arbitrary `props` onto edges; the dialect cannot declare
them; edge drops and target changes are entirely unguarded by the diff. No
engine writer passes non-empty props today, so refusing them is free now and
breaking later; freezing "silently accepted, never validated" is the worst
option. Fix: refuse non-empty props on the generic link surfaces, reserve
`edges.<rel>.properties`, add edge changes to the diff. Adjudicated P0 because
the refusal is only free before users hold data. Related: #111, #110 (one
loader change).

### [P0] Embeddings carry no model identity and nothing re-embeds *(both; the existing p0 label on #98 is right)*

The `embeddings` table stores no model; a model change silently mixes vector
spaces (cosine distance across models is not a distance); rebuild preserves
the vectors; there is no repair verb. Fix the minimum before release: a model
column treated as part of the cache key, plus an operator verb that clears and
re-enqueues. Alternatively (Codex) ship lexical search only. The
per-repository provider design in #98 can follow. Related: #98 (split it),
#164.

### [P1] The declaration dialect is closed with no reserved keys and no deprecation marker (Opus)

The stated evolution strategy is add-and-deprecate and there is no `deprecated`
marker; `unique` is the constraint users hit in week one; every added key is a
coordinated event. Land `unique`, `deprecated` and the edge-properties
reservation as one loader change (admitted, inert), and decide `x-` tolerance
in the same change. Related: #110, #111, #146 (shrink it).

### [P1] Constraint tightening (`pattern`, `min`, `max`) skips the live-value check (Codex)

`schemadiff.go` explicitly omits them, so a declaration can tighten while
existing records hold values new writes reject, permanently. Count violations
for changed bounds; treat a changed regex as narrowing when live values exist.
Related: #107 (too broad), file the focused child.

### [P1] `fts` is inside the byte-for-byte rebuild claim but computed from the current registry *(both)*

Drop `fts::text` from the fold snapshot and document the rebuild as the
sanctioned reindex. Related: #105 (accurate; take its first option).

### [P1] Declared indexes are named by position, so editing one silently keeps the old index *(both; Opus P2, Codex P1)*

`CREATE INDEX IF NOT EXISTS` with a name derived from the ordinal: change the
declaration and the statement is a no-op; remove it and the index survives.
The file's own contract is "filterable ≡ indexed ≡ declared". Key the name on
the definition and drop orphans. Untracked.

### [P1] Migration 0004 edits `records` outside the fold; the rule is unwritten and untested *(both)*

Rebuild under an empty operator registry restores pre-0004 values; write the
rule (a fold-table migration is idempotent under refold or paired with a
changelog re-statement) and add the upgrade-plus-rebuild fixture. Related:
#106 (accurate; add the test).

### [P1] Year-`0000` datetimes are accepted and break the whole collection read *(both; elevate #170 from P2)*

Five-line bound in `validate.go`; one bad row makes every filtered or ordered
read of the collection fail. Related: #170.

### [P2] Smaller items

- The repository-scoped table list is hand-copied three times and the test
  copy is already stale (missing `chain_epochs`); derive it from
  `information_schema` (Opus).
- The changelog never compacts and rebuild is one transaction; do not build
  compaction, but write the growth model down and measure one real repository
  before release (Opus).
- `kind_version` stamps (#146) must precede migration verbs but not release
  (Codex).

### The vocabulary-migrations program *(both)*

Defer the program (#146, #151, #152, #155, #156, #157, #158, #165) past
release; the narrowing guards plus add-and-deprecate already refuse the
dangerous cases, and no verb is needed while every client moves with the
server. Pull forward only: the registry-generation token (P0 above) and the
`deprecated`/reserved-key markers (P1 above). Also: all eight issues cite
`docs/plans/vocabulary-migrations.md`, which is not on `main`; land the plan
or strip the citations.

## 4. Code structure and quality

State: unusually disciplined for the size (69k non-test Go lines against 65k
of test, `go vet` clean, zero TODO markers, the api/engine boundary genuinely
held, a console with zero `any`). Two things spoil it: roughly 15,000 lines
exist that nothing consumes, and the project's own declared bug class (dead
vocabulary) is densest in the file that calls itself the frozen contract. One
genuine correctness hazard. Codex independently re-found the unsigned boot
mode and the embeddings gap from this angle (sections 2 and 3) and the
version-gate and `fts` findings (section 3), which raises confidence in all
four.

### [P0] Detached goroutines run LLM calls and writes outside the shutdown barrier, with no `recover` *(both; Opus P0, Codex P1)*

`judgeRequest` (`internal/engine/judge.go:74`), `resumeNotifiedThread`
(`internal/engine/agentdecision.go:238-245`) and function preparation
(`internal/engine/runner.go:549-566`) run on `context.Background()`,
uncounted and unwaited; `svc.Close()` closes the pools underneath them. There
is exactly one `recover()` in the whole non-test tree and chi's Recoverer
does not cover these goroutines, so a panic in the 6,600-line agent machinery
kills the process. `inTx` has no `defer tx.Rollback()`, so a panic leaks an
open transaction from an 8-connection pool. This is not only the alpha agent
surface: `resumeNotifiedThread` is driven by `notifies`, the one resolution
primitive (ADR 0003). Fix: one spawn helper on the service with a WaitGroup
and shutdown context, `recover`-and-log in both families, `defer Rollback`.
Untracked (not covered by #69).

### [P1] The `kinds:gen` pipeline is ~15,000 lines (17% of the tree) that nothing imports (Opus)

`internal/corekinds` (9,010 generated lines), `cmd/kindsgen`,
`internal/kinddialect` (a second YAML reader kept only to feed it) and the
generated TypeScript module have no importer outside their own tests, while
`record-schema.ts` hand-copies regexes out of the generated Go. A CI gate
holds all of it green. Wire the consumers or delete the pipeline; deleting
`internal/corekinds` also removes the only reason `internal/kinddialect`
exists. Related: #53 item 4 is a symptom.

### [P1] `internal/substrate`, the frozen contract, is written in the dead vocabulary *(both)*

`typ`/`srcType` parameters, `TypesImplementing`, "extension tier", "schema
apply", "relationship", "capability", and a doc comment pointing at
`PutInput.Type`, a field that does not exist. 277 dead-word identifier
occurrences tree-wide; `vocabularywrite.go` still holds
`putSchemaRecord`/`patchSchemaRecord` and comments citing a renamed file.
Mechanical rename, no wire change, and it is the surface being frozen.
Related: #53 (covers `internal/vocabulary` symbols only).

### [P1] Pre-release scaffolding should be deleted before the first tag, not carried forever (Opus)

The dialect-1 promotion rung (3,367 lines) reads a stored shape no release
ever wrote; every post-release install carries it forever and can never reach
it. Promote the owner's own stores, delete the rung, and consider folding
migrations 0002-0006 into 0001 while no released binary pins them. Same class
as #175 (which is agreed as pre-v1 by all four reports that touched it).

### [P1] Engine reads discard corruption and iteration errors *(both)*

`recordScan.finish` (`internal/engine/rows.go:106-131`) ignores JSON decode
errors for states, props and labels, so a malformed column reads back as an
empty record and the next patch writes that emptiness through the fold as
history. `gc.go:38-63` and `search.go:252-272` skip `rows.Err()` and can act
on a partial victim list. Small fixes; the exposure is exactly the
records-edited-outside-the-fold class that #106 documents.

### [P2] Smaller items

- `txn.apply` is 544 lines coupling every write invariant, with four kinds
  special-cased inline; both reviewers say keep one transaction and one entry
  point and extract ordered phase helpers (Codex ranked P1; adjudicated P2
  because the pipeline is load-bearing, heavily commented, and a refactor of
  the most important code in the tree is regression risk to schedule
  deliberately, starting with the cheap kind-admission extraction).
- `errcheck` is disabled on a stale justification; enabling costs 16 findings,
  all in the CLI (Opus).
- 85 comments cite deleted documents (`MODEL §11.4`, `FORMAT.md §1`); delete
  the citation or state the constraint, and add a docscheck rule against new
  ones (Opus).
- Test-only production APIs in `internal/vocabulary` (`Manifest`,
  `ParseManifest`, `EdgeRefs`, the write-only `SourceYAML` fields) *(both)*;
  `vocabularydiff` still skips added declarations, which is the one real CI
  gate hole in #53 *(both)*: rescope #53 to that item plus the surviving
  symbols and close the rest.
- Console: one duplicated query-error block at 13 sites (~300 lines that
  should be one component), `bundle-detail.tsx` (1,581 lines) and
  `change-request-detail.tsx` (1,246) should become directories,
  `merge-request-detail.tsx` has no test (Opus).
- `internal/gql` holds a process-wide mutex across the schema build and
  re-marshals every kind on every request for the cache key; key on a
  generation counter (Opus).
- Three stream-parsing paths in the console cast instead of parsing (`as
  unknown as AgentEvent`); zod is already a dependency (Opus).

## 5. Testing

State: the suite is strong where the hardest promises live (chain tampering,
fold containment, auth atomicity, narrowing guards; `-count=1` everywhere;
the short half runs in 9 seconds). The weaknesses are at the seams: the HTTP
contract is tested only against a hand-written fake, the sandbox tests can
skip silently, the race detector never sees the engine, and the release
pipeline can publish commits that never passed CI.

### [P0] Release workflows can publish commits that never passed CI (Codex)

`version.yml` accepts a manual dispatch without checking a successful `ci`
run; `release.yml` accepts manual dispatches and hand-pushed `v*` tags and
publishes; `latest.yml` publishes the moving image on every push to `main`
before that commit's CI result exists. A red commit can become a tagged
release and image. Fix: gate every publish path on a green `ci` for the exact
commit. Untracked.

### [P0] Nothing proves the sandbox tests ran (Opus)

The confinement tests skip when Landlock or seccomp is absent, on macOS they
compile away, and no CI job asserts a minimum count ran. An image or kernel
change turns the tests holding the isolation promise from passing to skipping
with a green build. Fix: `SUBSTRATE_TEST_REQUIRE_SANDBOX=1` turns the skips
into failures; set it in `ci:go` and `ci:race`. Untracked.

### [P0] No test drives the real engine through the real HTTP handler *(both; Opus P0, Codex P1)*

`internal/api` tests run against the 900-line fake only (by design); the fake
never produces `ErrGated`, a real `409 conflict` or a `ValidationError`, so
the engine-sentinel-to-status composition is untested, and #170 is an existing
case where it already breaks (`500` for a client error). `internal/testenv`
exists for exactly this and holds four sandbox tests. Fix: one conformance
file driving the record lifecycle over real HTTP with one case per published
error code. Related: #128, #170, #126.

### [P1] The engine never runs under `-race` *(both, also flagged in category 4)*

`test:race` is `-short`, which skips all 109 engine db test files, where the
concurrency lives. A scheduled (non-blocking) `go test -race -p 1
./internal/engine/...` job closes the class. Untracked.

### [P1] `wire.golden.json` pins 8 of the ~34 shapes the console mirrors *(both, also flagged in categories 1 and 4)*

Existing drift is already visible (`Page.head`/`total`, `BundleStatus`
optionality). Widen the golden to every mirrored shape, record
required-versus-optional, fix the two mismatches; independent of #131's path
decision, so split it out and do it first. Related: #131.

### [P1] Lock-order tests can pass before contenders reach the barrier (Codex)

Sleep-based barriers (300-400 ms) in the engine's lock-order tests turn them
into scheduler-timing tests on a slow runner. Replace with a deterministic
wait on `pg_stat_activity`. Untracked.

### [P2] Smaller items

- The `test:db` package allowlist is hand-maintained; three `internal/runner`
  cases currently run in no CI job (Opus).
- No fuzz targets anywhere; `FuzzParseFilter`, `FuzzSplitRecordPath`,
  `FuzzCanonicalJSON` are cheap and run their corpus in ordinary `go test`
  *(both)*.
- `internal/oauthflow` (the state HMAC), `internal/gql` (schema build over the
  shipped kinds) and `internal/config` have no tests of their own (Opus).
- The rebuild snapshot equality never covers a bundle install plus a sealed
  property write (Opus).
- `docs/testing.md` overclaims twice (testenv "end-to-end cases", console
  "every module has a test") (Opus).
- The live LLM suite runs in no scheduled job; a weekly budgeted workflow is
  the remainder of #6 (Opus P1, Codex P2, category 4 says defer; adjudicated
  P2, post-release).

## 6. Documentation and naming

State: unusually good for pre-release (21 pages, ~6,200 lines, both linters
green, almost every spot-checked claim true including the hard ones: the
property-type table, the id grammar, the filter operators, the chain's
honesty about what signatures prove). The failures are three specific kinds:
claims of stability the tracker plans to break, a quickstart that produces
the insecure configuration while the second page a newcomer reads states
signing as fact, and the safety mechanism the introduction sells being
documented nowhere. LICENSE is filed under section 7.

### [P0] The wire and the docs call this surface "stable" and "the frozen core" at v0.8.0 with breaking retrofits open (Opus)

`internal/api/discovery.go:151-160` stamps triggers, functions, bundles,
blobs, changefeed, search and embeddings `stability: stable` on the wire
("Everything the frozen core serves is stable"), and the stability enum has
only `alpha` and `stable`; `docs/agents.md:9` tells readers to build on the
frozen core. Meanwhile #127/#128/#131 are breaking response-shape and path
changes, and `search`/`embeddings` have no route at all (#129). The stamp is
the one thing an integrator reads before committing code. Fix: add a `beta`
value and report it for everything not actually frozen, and state in
`docs/graphql-and-search.md` that the GraphQL schema is a per-repository
projection, not frozen. Related: #126-#131.

### [P0] Truth-in-docs: four load-bearing promises the code does not keep *(both)*

- `README.md:19-26`: "nothing to configure before the first run" produces
  plaintext secrets and a permanently unsignable changelog;
  `docs/introduction.md:59-61` states signing as unconditional fact. The
  quickstart must mint a credential key and the introduction must state the
  condition (the code half is the section 2 P0).
- `docs/operations.md:121-144` promises an older binary refuses a newer
  store; no such gate exists (the code half is the section 3 P0). Until the
  gate lands, mark downgrade unsupported.
- `docs/operations.md:164-176` and three other pages promise the recovery
  identity opens a backup independently of the host key; no tool performs a
  restore (#137). Say so until it exists, or a user treats the host key as
  expendable and loses everything.
- `docs/operations.md:95` "bounded chunks" for the backfill: only reads are
  paged, the commit is one repository-wide transaction (also section 7).

### [P0] The policy door is undocumented, and two pages count four host functions where five ship (Opus)

`recordpatchpolicy` appears in exactly one table cell across all of `docs/`;
no page explains selector/action, the most-restrictive-wins rule, or that
policies never run for owner writes; ADRs 0005/0006 describe it but no
reader-facing page does. The `ask` host function (`hostfunctions.yaml:188`)
is the fifth built-in; `docs/functions.md:180` and `docs/agents.md:129` say
four; `llminteraction` is absent from `docs/builtin-kinds.md`, whose "these
three are a preview" heads a table missing an agent-runtime kind. The
introduction sells the substrate as "a safe way for semi-trusted automation
to write"; the mechanism that makes that true is unfindable. Related: #117
(carries the count in a buried clause; too small a home).

### [P0] Two vocabulary shapes freeze wrong if untouched (Codex)

- `account` is a required owner edge on the shared calendar and messaging
  kinds but a string on every provider mirror; the retype after release is a
  refused narrowing. #124 is accurate and sufficient; do it before the
  freeze.
- `docs/data-model.md:258-265` teaches kind authors to declare `title`,
  which the loader rejects as reserved (`load.go:1042-1053`), and its stamp
  rule contradicts `docs/vocabulary.md` and the shipped `task` kind. Replace
  both passages with the vocabulary.md/ADR 0016 rules.

### [P1] The error-code set the API doc calls closed is not closed (Opus)

`gated` reaches the wire and is missing from the table that says "nothing
else appears in `error.code`"; it is exactly the code an agent-safety client
must handle. `bad_request` maps to 405 and 413 as well as 400; `incoming`
lists carry no `head` despite the "every list" claim; GraphQL `Change` has no
`hash` despite `docs/changelog.md`'s claim; `/healthz` is undocumented.
Related: #144 (extend), #128.

### [P1] Naming debts that freeze at release *(both)*

- `connector` is in the wire's actor grammar and four doc pages, and
  `docs/terms.md` neither defines nor retires it (the code half is the
  section 1 actor-identity P0). Decide, then commit both halves.
- The env namespace is half-prefixed (`PORT`, `LOG_LEVEL`, `WEB_DIR`,
  `DATABASE_URL` bare, everything else `SUBSTRATE_*`), the operator hat
  accepts an undocumented `SUBSTRATE_DATABASE_URL` second spelling, and
  `SUBSTRATE_SANDBOX` lives outside `Config`. Settle the prefix rule now;
  renames after people write unit files are unstageable. Related: #164 (the
  naming half is cheap and should not wait behind #98).
- The domain-kind naming sweeps (#125 renames, #51's 41 gaps, #143
  timestamp names, #122 trait extraction) should land before the kinds they
  touch are declared stable, or those kinds should sit out the stable set
  per ADR 0015.
- #49 (property descriptions are console form help text) is user-facing
  surface; do before release.

### [P1] Getting-started and reference drift *(both)*

`docs/getting-started.md` uses `$TOKEN` without ever assigning it, so the
first copy-paste HTTP call fails auth (Codex). One correction pass over the
reference pages (Opus): the LLM example bundle's counts are wrong in
`bundles-catalog.md` (0 kinds/3 agents vs 1/6 shipped), `console.md` says
four record tabs where five render and misses two routes, `substratectl.md`
omits `version`, the `SS_*` aliases and `watch --kinds`, and says `$EDITOR`
where the code prefers `SUBSTRATE_EDITOR`. Prefer prose that does not count.

### [P2] Smaller items

- Write ADR #134 (repository id is identity) and a paragraph for #106;
  downgrade #138 to a one-liner since `docs/auth.md:111-114` already states
  it *(both)*.
- `docs/plans/` ships three design plans that `docs/README.md` does not list
  and docscheck does not reach; eight open issues cite a fourth plan file
  that is not on `main` (also section 3). Decide whether plans ship, and fix
  the citations (Opus).
- `internal/runner/runner.go:635` names a ghost `SUBSTRATE_OPERATOR_OTP`
  that nothing reads; delete the comment (Opus).
- The README quickstart hand-enumerates eight bundle files because `apply -f`
  takes no directory (the CLI half is section 7's item); nothing checks the
  list against the tree (Opus).

## 7. Developer experience and operations

State: the contributor loop is the strongest part of the repo and close to
finished (every CI job is one mise task that runs identically on a laptop,
toolchains pinned and cross-checked, actions pinned to SHAs, a careful dev
script, CLI error rendering better than most commercial tools). What is
missing is everything between "the code is good" and "somebody else runs
this": legal permission to use it, a backup procedure, a reachable operator
hat, and a way to obtain the artifacts that are already being published.

### [P0] The repository has no LICENSE *(both)*

No `LICENSE`, `COPYING` or `NOTICE` anywhere; release archives ship only
`README.md`; the OCI labels set no `org.opencontainers.image.licenses`. A
public repo with no license grants no rights at all, and every artifact
already on ghcr.io inherits that. Add the license, the OCI label, a short
`SECURITY.md` (private vulnerability contact) and a three-line
`CONTRIBUTING.md`. The cheapest P0 in this review. Untracked.

### [P0] There is no backup or restore procedure *(both; Opus P0, Codex P1)*

`docs/operations.md:162-176` states the invariant (one consistent dump is
complete) and gives no command; `pg_dump`/`pg_restore` appear nowhere in the
tree; nothing says the credential key must be stored separately from the dump
or that a restore ends with `repository verify`. An untested backup is not a
recovery path, and this is a personal data server whose pitch is owning the
data. Fix: a Backups section with the two commands against the shipped
compose, the credential-key sentence, the restore sequence ending in verify,
and one tested round trip. Untracked (#137 is the user-side half only).

### [P0] The operator hat is unreachable in the deployment the README ships (Opus)

Neither image carries `substratectl` and compose publishes no Postgres port,
so `repository verify/rebuild/reseal` and `user reset` cannot run at all.
Three concrete failures: a lost authenticator is a permanent lockout (every
credential change requires the current TOTP and there is no recovery-key
reset route); a restored backup cannot be verified; and the documented path
off the keyless default (`repository reseal`) is impossible. Fix: one more
`COPY --from=build` puts `substratectl` in the image and `docker compose exec`
reaches everything. Related: #153 and #137 are adjacent; neither names this.

### [P1] The published artifacts are named in no document, and compose builds from source *(both)*

`ghcr.io/geoah/substrate` appears nowhere in README or docs although two
workflows push it; `compose.yaml` is `build: .`, so there is no pull, no pin,
no rollback and no upgrade command (`docs/operations.md` explains upgrade
semantics without commands); the CLI release archives and `checksums.txt` are
mentioned nowhere; there is no `.env.example`. Fix: `image:` with a tag
variable (source build behind an override file), a two-line upgrade section
(snapshot, pull, up, verify), `.env.example`, and a README pointer to the
releases page. Related: #164 (this is its missing fourth item).

### [P1] A function's own log lines never leave the server process *(both)*

`host.log()`/`print()` are captured, scrubbed, returned in the response frame,
then written to the server slog and dropped; direct calls return
`{output, effects}` only and create no run record. This is the bundle
author's whole debugging loop on any substrate they do not operate. Fix:
return scrubbed logs on the direct-call response and keep the last N lines on
the run row for parked deliveries; both additive. Untracked.

### [P1] A 500 carries no correlation id and there is no request log *(both)*

`middleware.RequestID` is installed and never read; every unmapped error is
`{"code":"internal"}` plus a bare `request failed` log with no method, path,
status or id. Fix: id in the error envelope and an `X-Request-Id` header, one
completion log line per request. Makes early-adopter bug reports actionable.
Related: #128 is the natural PR to carry it.

### [P2] Smaller items

- `/healthz` never touches the database, and both images point their
  healthcheck at it; add a readiness route (Codex P1, Opus P2; adjudicated P2
  for a single-user server, but do it when touching the deployment files).
- Function artifact caches: not persisted in compose (an upgrade re-resolves
  every Python function against PyPI) and never evicted (retired installation
  directories accumulate until the disk fills). One item: a named volume plus
  deletion of retired directories *(both, complementary halves)*.
- `apply -f` accepts files only; `-f dir/` is the expected shape and the
  README's ten-line quick start is the demonstration (Opus).
- `SUBSTRATE_SANDBOX` is read with a bare `os.Getenv` outside
  `internal/config` and missing from the README's table; move it into
  `Config` (Opus; #164).
- Base images are mutable tags with unversioned APKs and SBOM/provenance are
  disabled in goreleaser; pin digests and publish an SBOM (Codex).
- `docs/operations.md:95` says the chain backfill runs in bounded chunks;
  only the reads are paged, the commit is repository-wide. Correct the
  sentence now, defer the bounded design with #159 (Codex).
