# Decision records

A **decision record** is one page about one choice: what forced it, what else
was on the table, what was chosen, and what that costs. It is dated, it is
short, and once it is accepted it does not change. This directory is the
corpus, and its point is that a choice already made and a choice already ruled
out can both be found before somebody proposes them again.

These are contributor-facing history, not reader documentation. That single
distinction decides most of what follows.

> The substrate has its own `decision`: the verdict on a proposed mutation,
> from the engine's decision loop. It is a different thing entirely. The word
> here always means the document.

## This is a repository-specific adaptation of MADR

The template is [MADR 4.0.0](https://adr.github.io/madr/) trimmed, not MADR.
The departures, so nobody reads the difference as a mistake:

- **Four statuses, no more**: `proposed`, `accepted`, `rejected`,
  `superseded`. MADR leaves the set open. `deprecated` is deliberately absent,
  because nothing here needs the distinction between deprecated and
  superseded, and a fifth value nobody can define is a lifecycle nobody can
  follow.
- **`superseded-by:`** is a frontmatter key here, checked by the linter.
  **`amended-by: NNNN`** is an optional one the linter ignores: a later
  record changed part of this one in prose without superseding it, and the
  key points a reader at it. The successor still names what it amends in its
  own body; the key is a convenience, never the source of truth.
- **Confirmation is expected**, not optional. A decision with nothing holding
  it is a decision that rots quietly, and writing "None: this is held by
  review only" is the honest version of that.

Nothing else is added: no tool, no generated site, no path governance, no
review schedule. The corpus is Markdown in git, and the one docs linter holds
its shape.

## What earns a record

A decision record is for a choice that is hard to reverse, shapes what other
code may do, and whose reasoning is not already written down somewhere it will
be found. All three, not any one.

Not a record, and these are the common mistakes:

- Anything the code already says. The code is the contract.
- A bug fix, a rename, a library bump, a refactor.
- A choice with one live option. That is not a decision, it is a consequence.
- A rule `AGENTS.md` already states along with its reason. A record is written
  only where the rule is there and the reasoning is not.

Those are commits. A good commit message carries them.

### The neighbours

Three overlapping documentation genres is how a practice dies, so the borders
are drawn here:

- **`docs/plans/*.md`** propose future work and change while the work is in
  flight. They already hold settled design, rejected alternatives and risks. A
  plan does not get a decision record for every choice inside it. One is
  written only for a choice that binds code beyond the plan's own work, and it
  links to the plan instead of restating its alternatives.
- **`AGENTS.md`** states the rule an agent must follow, in one line, and stays
  the operative text. A record explains a rule; it never replaces it. A house
  rule may link its record.
- **`docs/*.md`** describe the system as built. A record describes a moment.

## Names and numbers

Files are `NNNN-kebab-title.md`, four digits. `template.md` is the template
and `README.md` is this page; everything else in this directory is a record.

The number is the next free one, counting the numbers other branches have
already taken as well as `main`'s. Numbers are permanent once merged to
`main`. `mise run decisions:check` refuses a record whose number `main`
already uses, or that another branch pushed in the last 30 days added first,
and names the next number nobody holds
([0124](0124-a-decision-number-is-checked-against-every-live-branch-before-the-merge.md)).
Run it after `git fetch --prune` (a deleted branch's stale ref still reads as
a claim) and before citing the number anywhere. CI runs it on pull requests
and on `main`, so a pushed branch without a pull request counts as a claim for
others but is not checked itself. There is no contiguity contract. Gaps are
legal, so the linter checks the format and uniqueness and nothing more, and
nobody should read the sequence as a count.

## Statuses and lifecycle

- `proposed` becomes `accepted` or `rejected`.
- A record is `accepted` in the pull request that lands its code, so no record
  is `proposed` while the code it describes is on `main`. A record written
  ahead of its code stays `proposed` until that pull request.
- `accepted` becomes `superseded`.
- `rejected` and `superseded` are terminal.
- `date` is the date of the last status change, and it is the only frontmatter
  field that changes when the status does.

## Superseding, not editing

Once a record is `accepted` or `rejected`, its Context, Considered Options,
Decision Outcome and Consequences are frozen. Reversing a decision means a NEW
record. The old one gets exactly two frontmatter edits, `status: superseded`
and `superseded-by: NNNN`, plus its `date`.

Only one direction of the link is stored: the superseded record points forward
to its successor. The successor names what it replaces in prose, in its
Context or its More Information. Storing both directions would be a second
source of truth with nothing keeping the two in sync, and one direction makes
a cycle impossible, because the linter requires the successor to be
`accepted`.

Mechanical repair is allowed. A link whose target was renamed may be
repointed, and one whose target is gone may be reduced to its text, because
`lint:docs` runs lychee over the whole tree and a dead link is a bug wherever
it lives. Repairing a link does not touch the decision, and git holds the
original either way; `frozen:check` compares bodies with every link reduced to
its text for exactly this reason, so the words are frozen and the targets are
not.

## A decision record can rot, and that is expected

The code can move under an accepted record and leave it silently wrong. This
directory does not solve that and does not claim to. The code, the tests and
`AGENTS.md` govern what is true NOW; a record is historical and is never
authoritative about the present. Keeping the corpus honest means one thing
only: when a decision is reversed, the record that held it is superseded.

For the same reason these pages are not held to the live words in
[terms.md](../terms.md), and `docscheck.sh`'s vocabulary rule deliberately
does not reach them. A record written today used today's words. A vocabulary
change two years from now must not force an edit into a frozen document, and a
record that says what it said in 2026 is doing its job.

## What the linter holds

`mise run lint:docs` refuses:

1. A file here that is not `NNNN-kebab-title.md` (`README.md` and
   `template.md` excepted).
2. Two records with the same number.
3. A record without a frontmatter block at line 1 carrying a `status:` from
   the four and a `date:` as `YYYY-MM-DD`.
4. A record this index does not list.
5. A `superseded` record with no `superseded-by:`, or one naming a record that
   does not exist, is itself, or is not `accepted`. Also a record that carries
   `superseded-by:` without being superseded.
6. An index row whose status disagrees with the record's frontmatter.

`mise run frozen:check` holds the one rule the files alone cannot show, because
it is about what CHANGED: the body of an accepted record, everything below the
frontmatter, may not be rewritten, its link targets excepted. A record that gets edited as opinion moves
stops being evidence of what was decided and why, and superseding it is how the
corpus says so. The frontmatter is deliberately outside the comparison, so
marking a record superseded edits `status:` and `superseded-by:` and trips
nothing. It reads the diff against the base branch rather than the files, which
is why it sits beside `kinds:check` in the `lint` job instead of inside
`lint:docs`.

`mise run decisions:check` holds the rule no single tree can show: a number
another branch took first. It reads the other branches on `origin`, so it sits
in the same job for the same reason.

Not held, on purpose: whether the record is under two pages, whether the
options were seriously considered, and whether Confirmation names a real test.
Those are a reviewer's, which is where `docscheck.sh` already draws its line.

## The index

| #                                              | Decision                                      | Status   |
| ---------------------------------------------- | --------------------------------------------- | -------- |
| [0001](0001-record-decisions-in-the-repo.md)   | Record decisions in the repo                  | accepted |
| [0002](0002-declaration-versions-are-integers.md) | Declaration versions are integers the API maintains | accepted |
| [0003](0003-notifies-is-the-one-resolution-primitive.md) | A transition's `notifies:` marker is the one resolution primitive | accepted |
| [0004](0004-asks-are-records-the-owner-resolves.md) | Asks are batch records only the owner resolves | accepted |
| [0005](0005-the-policy-door-is-deterministic-and-the-judge-recommends.md) | The policy door is deterministic; the judge only ever recommends | accepted |
| [0006](0006-voluntary-proposals-stay-self-acceptable.md) | Voluntary proposals stay self-acceptable; only gated requests refuse bundle decisions | accepted |
| [0007](0007-the-audit-lives-on-the-records-it-concerns.md) | The audit lives on the records it concerns, not in an audit kind | accepted |
| [0008](0008-keep-the-loop-do-not-adopt-adk-go.md) | Keep the agent loop; do not adopt ADK for Go | accepted |
| [0009](0009-the-chain-hashes-what-postgres-stored.md) | The changelog chain hashes what Postgres stored, with value-exact numbers | superseded |
| [0010](0010-signing-is-per-repository-ed25519-one-way.md) | Changelog signing is a per-repository Ed25519 key with one-way activation | superseded |
| [0011](0011-chain-transitions-are-repository-scoped-epochs.md) | Sanctioned chain transitions are recorded as repository-scoped chain epochs | superseded |
| [0012](0012-numbers-are-exact-or-refused.md)   | Numbers are exact or refused                  | accepted |
| [0013](0013-state-machines-say-done-and-abandoned.md) | State machines say `done` and `abandoned`; a domain word survives only by decision | superseded |
| [0014](0014-authorities-widen-only-outside-the-id-alphabet.md) | Authorities may widen only with characters the id alphabet excludes | accepted |
| [0015](0015-unproven-kinds-stay-out-of-the-stable-set.md) | Unproven kinds stay out of the stable vocabulary set | accepted |
| [0016](0016-a-kind-titles-itself-from-a-declared-property.md) | A kind titles itself from a declared property, never the built-in slot | accepted |
| [0017](0017-the-changelog-is-one-writers-total-order.md) | The changelog is one writer's total order per repository | accepted |
| [0018](0018-reseal-rewrites-history-values-in-place.md) | Reseal rewrites history's values in place, so entry bytes are not identity | superseded |
| [0019](0019-a-lifecycle-is-a-state-machine-only-where-the-substrate-owns-it.md) | A lifecycle is a state machine only where the substrate owns it | accepted |
| [0020](0020-dialect-keys-are-reserved-not-tolerated.md) | Dialect keys are reserved by name; there is no tolerated `x-` namespace | accepted |
| [0022](0022-rest-is-frozen-graphql-is-a-projection.md) | REST is the frozen v1 contract; GraphQL is a derived projection | superseded |
| [0023](0023-a-sealed-payload-is-bound-to-its-address.md) | A sealed payload is bound to the address it was written at | accepted |
| [0024](0024-the-credential-key-is-key-material-not-a-passphrase.md) | `SUBSTRATE_CREDENTIAL_KEY` is key material, not a passphrase | accepted |
| [0025](0025-an-actor-carries-the-full-authority.md) | An actor carries the full authority, and `connector:` is retired | superseded |
| [0026](0026-embedding-vectors-are-1536-wide-or-refused.md) | Embedding vectors are 1536 wide or the provider row is refused | accepted |
| [0027](0027-an-edge-outlives-a-tombstone-and-dies-with-a-purge.md) | An edge needs its target at write, outlives a tombstone, and dies with a purge | superseded |
| [0030](0030-a-blob-outside-postgres-settles-after-its-bytes.md) | A blob outside Postgres settles after its bytes, behind a pending manifest | accepted |
| [0031](0031-blob-bytes-outside-postgres-are-stored-plaintext.md) | Blob bytes outside Postgres are stored plaintext | accepted |
| [0032](0032-an-owner-pointer-may-be-a-reference-and-a-mirror-account-is-one.md) | An owner pointer may be a reference, and a mirror's `account` is one | superseded |
| [0033](0033-the-path-grammar-has-no-separators.md) | The path grammar carries no separators; a record's URL is its reference | accepted |
| [0034](0034-a-reference-may-pin-a-trait-not-only-a-kind.md) | A reference may pin a trait, not only a kind | accepted |
| [0035](0035-a-network-body-connect-is-filtered-by-destination.md) | A network-granted body's connect is filtered by destination | accepted |
| [0036](0036-operational-lists-are-a-page-envelope.md) | Operational lists answer with an `{items, cursor?}` envelope | accepted |
| [0037](0037-gated-is-an-agent-loop-verdict-not-a-wire-code.md) | `gated` is an agent-loop verdict, not a wire code | accepted |
| [0038](0038-a-network-entry-is-a-bare-host-or-cidr-destination.md) | A `permissions.network` entry is a bare host or CIDR destination | accepted |
| [0039](0039-the-substrate-stores-a-recurrence-rule-and-never-expands-it.md) | The substrate stores a recurrence rule and never expands it | accepted |
| [0040](0040-the-four-occurrence-logs-say-done.md) | The four occurrence logs all say `done`; `medicationschedulelog` loses its `taken` exception | accepted |
| [0041](0041-body-is-a-declarable-property-and-fts-is-per-property.md) | Body is a declarable, column-backed property, and FTS is per-property | accepted |
| [0042](0042-every-kind-carries-an-authority.md) | Every kind carries an authority; the URL disambiguates by segment count | accepted |
| [0043](0043-occurrences-expand-at-read-in-the-api-layer.md) | Occurrences expand at read, in the API layer, never in the engine | superseded |
| [0044](0044-a-reference-is-the-only-link-between-records.md) | A reference is the only link between records; `data.edges` is removed | accepted |
| [0045](0045-a-webhook-trigger-is-a-public-endpoint-with-an-optional-key.md) | A webhook trigger is a public endpoint with an optional key | accepted |
| [0046](0046-a-repository-owns-one-authority-chosen-at-registration.md) | A repository owns one authority, chosen at registration and defaulting to the server's host | superseded |
| [0047](0047-a-kind-lives-in-a-package.md) | A kind lives in a package, and a package lives in an authority | accepted |
| [0048](0048-providers-are-published-samples-are-copied.md) | Providers are published packages; samples are copied and owned | accepted |
| [0049](0049-the-owner-of-a-mappings-target-declares-it.md) | The owner of a mapping's target declares it and pins the subject | accepted |
| [0050](0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md) | The changelog is checksummed segment files per repository, and Postgres indexes it | superseded |
| [0051](0051-a-repository-directory-is-the-backup-unit.md) | A repository directory under the data root is the backup unit | superseded |
| [0052](0052-the-authority-is-the-repository-id.md) | The authority is the repository id | superseded |
| [0053](0053-rest-is-supported-all-of-graphql-is-preview.md) | REST is the supported interface; all of GraphQL is preview | superseded |
| [0054](0054-a-repository-moves-between-host-keys-through-an-offline-rewrap.md) | A repository moves between host keys through an offline rewrap of its manifest | superseded |
| [0055](0055-a-retired-name-is-declared-and-never-inferred-from-a-prune.md) | A retired name is a declared reservation, never inferred from a prune | accepted |
| [0056](0056-a-change-cursor-is-a-seq-under-a-history-generation.md) | A change cursor is a seq under a history generation | accepted |
| [0057](0057-a-changelog-line-names-its-transaction-and-an-unfinished-one-is-cut-whole.md) | A changelog line names its transaction, and an unfinished transaction is cut whole | accepted |
| [0058](0058-a-graphql-name-always-carries-the-authority.md) | A non-core kind's GraphQL name always carries its full authority | superseded |
| [0059](0059-a-marked-repository-refuses-plain-and-host-key-sealed-payloads.md) | A repository marked DEK-only refuses plain and host-key-sealed payloads | accepted |
| [0060](0060-a-record-carries-the-kind-version-that-last-wrote-it.md) | A record carries the kind version that last wrote it | accepted |
| [0061](0061-a-change-event-names-the-affected-records-and-clients-fetch-them.md) | A change event names the affected records with their versions, and clients fetch them | accepted |
| [0062](0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md) | A write is on disk before its commit, and its final newline is the commit marker | superseded |
| [0063](0063-a-property-rename-is-ordinary-record-writes.md) | A property rename is ordinary record writes, never a fold-time conversion | accepted |
| [0064](0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md) | Trigger bookkeeping is a delivery ledger folded from the changelog | accepted |
| [0065](0065-a-snapshot-is-a-stopped-server-copy-that-records-its-head.md) | A snapshot is a stopped-server copy that records its head and lists what it needs | superseded |
| [0066](0066-a-backfill-and-an-enum-remap-are-ordinary-record-writes.md) | A backfill and an enum remap are ordinary record writes, composed with the rename | accepted |
| [0067](0067-a-lossy-conversion-runs-only-with-a-confirmation-bound-to-its-preview.md) | A lossy conversion runs only with a confirmation bound to its preview | accepted |
| [0068](0068-an-accepted-webhook-is-a-pending-entry-in-the-delivery-ledger.md) | An accepted webhook request is a pending entry in the delivery ledger before its `202` | superseded |
| [0069](0069-the-owner-export-is-the-snapshot-streamed-as-a-tar.md) | The owner's export is the snapshot, streamed as a tar under the bearer token | superseded |
| [0070](0070-a-copy-is-upgraded-through-its-origin-stamp-and-requires-pins-a-floor.md) | A sample copy is upgraded through its origin stamp, and `requiresAtLeast` pins a floor under a requirement | accepted |
| [0071](0071-a-webhook-url-names-the-repository-by-its-authority.md) | A webhook URL names the repository by its authority | accepted |
| [0072](0072-reserve-a-purged-record-id-against-reuse.md) | Reserve a purged record id so a later write cannot take it | rejected |
| [0073](0073-idempotency-keys-live-in-a-postgres-table-not-the-changelog.md) | Idempotency keys live in a Postgres table, not the changelog | accepted |
| [0074](0074-the-repository-name-is-the-login-name.md) | The repository name is the login name, and `username` is gone | superseded |
| [0075](0075-v1-ships-the-fs-blob-backend-alone.md) | v1 ships the `fs` blob backend alone, and `SUBSTRATE_BLOB_STORE` is gone | accepted |
| [0076](0076-a-bundle-ships-its-settings-as-core-setting-and-secret-records.md) | A bundle ships its settings as core `setting` and `secret` records | accepted |
| [0077](0077-the-llm-kinds-live-in-their-own-seeded-package.md) | The LLM kinds live in their own seeded package, `substrate.reamde.dev/llm` | accepted |
| [0078](0078-a-kind-move-is-ordinary-record-writes.md) | A kind move is ordinary record writes: `movedFrom` carries the rows and repoints every reference | accepted |
| [0079](0079-graphql-is-removed-and-the-records-read-is-one-route.md) | GraphQL is removed, and the records read is one route | accepted |
| [0080](0080-a-kind-grant-may-glob-and-a-glob-never-reaches-auth-material.md) | A kind grant may glob, and a glob never reaches auth material | accepted |
| [0081](0081-the-window-read-computes-occurrences-and-recurring-is-core.md) | The window read computes occurrences, and `recurring` and `override` are core traits | accepted |
| [0082](0082-a-record-meets-a-new-state-machine-in-its-initial-state.md) | A record meets a new state machine in its initial state | accepted |
| [0083](0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md) | A repository has one writer, and a second is refused at open | accepted |
| [0084](0084-a-records-list-pages-by-offset-beside-the-keyset-cursor.md) | A records list pages by offset beside the keyset cursor | accepted |
| [0085](0085-a-sync-is-a-core-trait-the-dispatcher-stamps.md) | A sync is a core trait, and the dispatcher stamps it around a delivery | accepted |
| [0086](0086-the-head-of-a-repeated-source-is-spelled-with-brackets.md) | The head of a repeated source is spelled `[]`, in a display template and in `merge: first` | accepted |
| [0087](0087-an-unresolved-source-parks-instead-of-minting.md) | A source that offers nothing, or that a probe cannot tell apart, parks instead of minting | accepted |
| [0088](0088-a-single-record-read-carries-its-inbound-mapping-owned-links.md) | A single-record read carries its inbound mapping-owned links | accepted |
| [0089](0089-a-suggested-mapping-fits-a-source-that-declares-nothing.md) | A suggested mapping fits a source kind that declares nothing under its word | accepted |
| [0090](0090-the-oauth-surface-takes-an-accounts-full-identity.md) | The OAuth surface takes an account's full identity | accepted |
| [0091](0091-a-parked-delivery-is-retried-or-forgotten.md) | A parked delivery is retried or forgotten, and both end the row | accepted |
| [0092](0092-an-orphaned-mapping-target-is-marked-and-collected-on-request.md) | An orphaned mapping target is marked always and collected only on request | accepted |
| [0093](0093-a-guarded-write-may-declare-that-losing-is-normal.md) | A guarded write may declare that losing its race is normal | accepted |
| [0094](0094-propertymeta-names-the-source-record-behind-each-manager-and-alternative.md) | `propertyMeta` names the source record behind each manager and alternative | accepted |
| [0095](0095-a-reference-may-pin-a-mapping-source-kind.md) | A reference may pin a mapping's source kind | accepted |
| [0096](0096-a-mapping-synthesises-its-subject-slot.md) | A mapping synthesises its subject slot on the source kind | accepted |
| [0097](0097-a-webhook-trigger-declares-the-headers-its-callable-reads.md) | A webhook trigger declares the headers its callable reads | accepted |
| [0098](0098-a-declaration-names-a-kind-or-trait-in-full.md) | A declaration names a kind or trait in full | accepted |
| [0099](0099-a-repository-migration-is-code-the-boot-runs-once-and-records.md) | A repository migration is code the boot runs once and records | accepted |
| [0100](0100-a-requires-floor-binds-a-copy-not-a-hand-declared-package.md) | A `requiresAtLeast` floor binds a copy, not a hand-declared package | accepted |
| [0101](0101-a-kind-trait-or-callable-is-named-in-full-on-every-surface.md) | A kind, trait or callable is named in full on every surface | accepted |
| [0102](0102-the-oauth-surface-takes-the-full-identity-only.md) | The OAuth surface takes the full identity only | accepted |
| [0103](0103-an-ambiguous-probe-follows-its-mappings-policy-and-a-probed-value-never-spreads.md) | An ambiguous probe follows its mapping's `onAmbiguous`, and a probed value never spreads to a second target | accepted |
| [0104](0104-a-break-ships-an-upgrade-note-under-docs-changes.md) | A break ships an upgrade note under docs/changes | rejected |
| [0105](0105-a-lossy-confirmation-binds-to-what-the-plan-affects.md) | A lossy confirmation binds to what the plan affects | accepted |
| [0106](0106-the-apply-door-holds-back-a-mapping-whose-provider-is-absent.md) | The apply door holds back a mapping whose provider is absent, on request | accepted |
| [0107](0107-an-apply-links-the-sources-its-mappings-left-unlinked.md) | An apply links the sources its mappings left unlinked | accepted |
| [0108](0108-an-allow-outranks-a-gate-only-by-naming-it.md) | An allow outranks a gate only by naming it | accepted |
| [0109](0109-a-package-row-names-the-actor-that-declared-it.md) | A package row names the actor that declared it | accepted |
| [0110](0110-a-schedule-trigger-passes-declared-arguments-to-its-function.md) | A schedule trigger passes declared arguments to its function | accepted |
| [0111](0111-a-function-and-an-agent-list-read-is-the-window-read.md) | A function's and an agent's list read is the window read | accepted |
| [0113](0113-a-manager-row-holds-at-its-actors-live-machine-tier.md) | A manager row holds at the machine tier once its actor is declared there | superseded |
| [0114](0114-a-change-row-reads-a-property-rename-as-one-move.md) | A change row reads a property rename as one move | accepted |
| [0115](0115-an-owner-adjusts-a-change-request-on-the-accepting-write.md) | An owner adjusts a change request on the accepting write | accepted |
| [0116](0116-a-probe-folds-case-only-when-it-declares-fold-case.md) | A probe folds case only when it declares `fold: case` | accepted |
| [0117](0117-a-kind-may-declare-its-display-label.md) | A kind may declare its display label, singular and plural | accepted |
| [0118](0118-a-mapping-where-narrows-its-sources-in-the-filter-grammar.md) | A mapping's `where` narrows its sources in the filter grammar | accepted |
| [0119](0119-a-direct-call-of-a-networked-function-writes-a-run-row.md) | A direct call of a networked function writes a run row | accepted |
| [0120](0120-a-map-rule-reaches-a-mirrors-subject-through-the-targets-pin.md) | A map rule reaches a mirror's subject through the target's pin | accepted |
| [0121](0121-a-function-body-runs-an-agent-under-permissions-agents.md) | A function body runs an agent under `permissions.agents` | accepted |
| [0122](0122-a-delete-may-purge-so-a-put-starts-the-record-over.md) | A delete may purge, so the next put starts the record over | accepted |
| [0123](0123-a-trigger-read-names-its-kinds.md) | A record trigger's changelog read names its kinds | accepted |
| [0124](0124-a-decision-number-is-checked-against-every-live-branch-before-the-merge.md) | A decision number is checked against every live branch before the merge | accepted |
| [0125](0125-a-provider-write-is-a-callable-that-writes-no-record.md) | A provider write is a callable that writes no record | accepted |
| [0126](0126-the-changes-read-summarizes-runs-and-never-ends-a-page-inside-one.md) | The changes read summarizes runs and never ends a page inside one | accepted |
| [0130](0130-the-console-serves-four-things-and-its-navigation-follows-them.md) | The console serves four things, and its navigation follows them | accepted |
| [0131](0131-the-console-writes-for-two-readers-behind-one-switch.md) | The console writes for two readers, and one switch tells them apart | accepted |
| [0132](0132-console-preferences-follow-the-person-and-a-window-fact-stays-in-the-browser.md) | Console preferences follow the person, and a window's own facts stay in the browser | accepted |
| [0133](0133-a-kind-declares-its-purpose.md) | A kind declares its purpose | accepted |
| [0134](0134-the-records-list-counts-its-filtered-set-on-request.md) | The records list counts its filtered set on request | accepted |
| [0135](0135-a-change-row-carries-before-and-after-values-on-request-derived-at-read.md) | A change row carries before and after values on request, derived at read | accepted |
| [0136](0136-search-ranks-by-bm25f-and-a-kinds-purpose.md) | Search ranks by BM25F and a kind's purpose | accepted |
| [0137](0137-a-record-link-in-markdown-is-a-substrate-url-of-its-record-path.md) | A record link in Markdown is a `substrate://` URL of its record path | accepted |
| [0138](0138-a-compacted-thread-keeps-its-rows-and-replays-a-summary-message-over-the-range-it-covers.md) | A compacted thread keeps its rows and replays a `summary` message over the range it covers | accepted |
| [0139](0139-an-agent-declares-its-purpose.md) | An agent declares its purpose | accepted |
| [0140](0140-money-is-an-integer-amount-of-its-currencys-minor-unit.md) | Money is an integer amount of its currency's minor unit | accepted |
| [0141](0141-a-paged-drain-names-its-cursor-by-hash-and-a-park-carries-it-whole.md) | A paged drain names its cursor by hash, and a park carries it whole | accepted |
| [0142](0142-a-schedule-fire-that-settles-retires-its-triggers-older-parked-fires.md) | A schedule fire that settles retires its trigger's older parked fires | accepted |
| [0143](0143-a-token-has-full-access-to-its-repository-and-a-login-token-does-not-expire.md) | A token has full access to its repository, and a login token does not expire | accepted |
| [0144](0144-a-restoring-put-completes-the-conversions-a-tombstone-missed.md) | A restoring put completes the conversions a tombstone missed | accepted |
| [0145](0145-a-release-is-a-pull-request-that-dates-the-changelog.md) | A release is a pull request that dates the changelog, and a merge to main releases nothing | accepted |
| [0146](0146-a-finished-segment-is-read-at-its-end-lines-and-a-snapshot-links-what-its-base-holds.md) | A finished segment is read at its first and last lines before serving, and a snapshot links what its base holds | accepted |
| [0147](0147-a-pass-runs-its-schedule-triggers-in-a-lane-beside-its-record-triggers.md) | A pass runs its schedule triggers in a lane beside its record triggers | accepted |
| [0148](0148-an-ongoing-problem-is-one-alert-record-written-under-the-callable-it-is-about.md) | An ongoing problem is one alert record, written under the callable it is about | accepted |
| [0149](0149-a-spend-cap-holds-an-agent-trigger-a-third-state-beside-running-and-parked.md) | A spend cap holds an agent trigger, a third state beside running and parked | accepted |
| [0150](0150-a-schedule-lane-fires-its-due-triggers-in-parallel-one-fire-per-trigger.md) | A schedule lane fires its due triggers in parallel, one fire per trigger | accepted |
| [0151](0151-an-agent-delivery-a-restart-interrupted-is-rerun-once-at-the-first-pass.md) | An agent delivery a restart interrupted is rerun once, at the first pass after the restart | accepted |
| [0152](0152-a-hand-retry-writes-a-run-row-and-the-newest-ok-run-outlives-retention.md) | A hand retry writes a run row, and the newest ok run outlives retention | accepted |
| [0153](0153-a-quantity-is-exact-digits-and-a-ucum-code-converted-only-within-its-measure.md) | A quantity is exact digits and a UCUM code, converted only within its measure | superseded |
| [0154](0154-an-observed-value-binds-the-core-observed-trait-which-never-gains-a-property.md) | An observed value binds the core `observed` trait, which never gains a property | accepted |
| [0155](0155-an-observed-value-names-what-it-is-about-with-an-about-reference.md) | An observed value names what it is about with an `about` reference | accepted |
| [0156](0156-the-forms-are-gauge-amount-counter-and-summary-and-live-on-the-value.md) | The forms are `gauge`, `amount`, `counter` and `summary`, and they live on the value | accepted |
| [0157](0157-a-counter-is-stored-as-sent-and-differenced-at-read-under-one-of-three-contracts.md) | A counter is stored as sent and differenced at read under one of three contracts | accepted |
| [0158](0158-a-gauge-carries-a-hold-a-null-ends-it-and-its-mean-is-time-weighted.md) | A gauge carries a hold, a null ends it, and its mean is time-weighted | accepted |
| [0159](0159-the-base-observation-kinds-live-in-a-sample-every-domain-sample-requires.md) | The base observation kinds live in a sample every domain sample requires | superseded |
| [0160](0160-the-observed-index-keeps-a-segment-per-record-and-a-rollup-serves-only-exact-answers.md) | The observed index keeps a segment per record, and a rollup serves only exact answers | accepted |
| [0161](0161-the-aggregate-read-is-a-mode-of-the-records-route-checked-before-it-runs.md) | The aggregate read is a mode of the records route, checked before it runs | accepted |
| [0162](0162-an-observed-row-names-its-origin-and-source-and-composes-its-id-from-upstream-data.md) | An observed row names its origin and source, and composes its id from upstream data | accepted |
| [0163](0163-positions-forecasts-and-money-stay-out-of-the-observed-index.md) | Positions, forecasts and money stay out of the observed index | accepted |
| [0164](0164-a-webhook-signature-is-verified-by-the-host-when-the-trigger-declares-one.md) | A webhook's signature is verified by the host when the trigger declares one | accepted |
| [0165](0165-a-commit-in-doubt-is-caught-up-by-the-next-write-and-no-write-grant-names-the-owners-auth-kinds.md) | A commit in doubt is caught up by the next write, and no write grant names the owner's auth kinds | accepted |
| [0166](0166-a-record-page-layout-is-a-record-in-a-seeded-console-package.md) | A record page's layout is a record in a seeded console package | accepted |
| [0167](0167-the-webhook-door-answers-after-a-sealed-spool-write.md) | The webhook door answers `202` after a sealed spool write, and a drain records the request | accepted |
| [0168](0168-a-provider-s-live-events-ship-as-an-opt-in-bundle-that-requires-it.md) | A provider's live events ship as an opt-in bundle that requires the provider, with its webhook off | accepted |
| [0169](0169-a-chat-turn-runs-detached-from-its-client-and-its-thread-is-attached-by-id.md) | A chat turn runs detached from its client, and its thread is attached by id | accepted |
| [0170](0170-a-chat-thread-is-titled-by-its-first-message-then-by-its-model-and-an-owner-rename-holds.md) | A chat thread is titled by its first message, then by its model, and an owner rename holds | accepted |
| [0171](0171-a-reasoning-delta-is-its-own-event-and-a-turn-keeps-a-bounded-summary-never-the-chain.md) | A reasoning delta is its own event, and a turn keeps a bounded summary, never the chain | accepted |
| [0172](0172-an-agent-names-the-kinds-it-searches-and-its-prompt-lists-them.md) | An agent names the kinds it searches, and its prompt lists them | accepted |
| [0173](0173-the-embed-drain-packs-values-into-capped-requests-and-sets-a-refused-one-aside.md) | The embed drain packs values into capped requests and sets a refused one aside | accepted |
| [0174](0174-the-semantic-arm-is-indexed-approximate-and-bounded.md) | The semantic arm is indexed, approximate and bounded | accepted |
| [0175](0175-a-record-view-draws-tiles-trends-and-tables-from-one-batched-read-per-block.md) | A record view draws tiles, trends and tables from one batched read per block, and a band is view data | accepted |
| [0176](0176-a-reconnect-requests-a-sync.md) | A reconnect requests a sync | accepted |
| [0177](0177-a-failing-alert-needs-a-standing-park-and-ends-with-the-last-one-or-its-trigger.md) | A failing alert needs a standing park, and ends with the last one or its trigger | accepted |
| [0178](0178-a-sync-delivery-that-settles-ok-retires-the-accounts-older-parks-of-the-same-callable.md) | A sync delivery that settles ok retires the account's older parks of the same callable | accepted |
| [0179](0179-the-console-waits-fifteen-minutes-for-a-requested-sync-to-start.md) | The console waits fifteen minutes for a requested sync to start | accepted |
| [0180](0180-a-refresh-stores-its-rotated-token-and-an-hour-of-refusals-parks-the-credential.md) | A refresh stores its rotated token, and an hour of refusals parks the credential | accepted |
| [0181](0181-a-cached-prompt-token-is-priced-at-its-cache-rate.md) | A cached prompt token is priced at its cache rate | accepted |
| [0182](0182-a-record-trigger-is-walked-by-one-caller-at-a-time.md) | A record trigger is walked by one caller at a time | accepted |
| [0183](0183-an-unreachable-beeper-desktop-throttles-the-sync-and-throttled-reads-as-waiting.md) | An unreachable Beeper Desktop throttles the sync, and throttled reads as waiting | accepted |
| [0184](0184-a-record-view-grows-by-adding-a-field-never-by-retyping-one.md) | A record view grows by adding a field, never by retyping one | accepted |
| [0185](0185-a-table-draws-its-results-as-tiles-and-every-tile-takes-one-size.md) | A table draws its results as tiles, and every tile takes one size | accepted |
| [0186](0186-units-and-conversions-are-core-records-and-a-value-converts-through-the-repositorys-graph.md) | Units and conversions are core records, and a value converts through the repository's graph | accepted |
| [0187](0187-linked-sources-are-one-device-read-from-their-highest-ranked-source.md) | Linked sources are one device, read from their highest-ranked source | accepted |
| [0188](0188-the-observations-page-folds-a-metrics-sources-into-one-row-and-keeps-channels-apart.md) | The Observations page folds a metric's sources into one row, and keeps channels apart | accepted |
| [0189](0189-the-observation-source-kind-stays-in-the-observations-sample.md) | The observation source kind stays in the observations sample | superseded |
| [0190](0190-a-blobref-declares-the-media-types-of-its-bytes.md) | A blobref declares the media types of its bytes | accepted |
| [0191](0191-a-shipped-record-is-held-by-any-hand-but-the-one-that-ships-it.md) | A shipped record is held by any hand but the one that ships it | accepted |
| [0192](0192-observations-is-seeded-and-the-well-known-catalogues-are-first-party-packages.md) | Observations is seeded, and the well-known catalogues are first-party packages | accepted |
| [0193](0193-the-clinical-record-is-a-first-party-catalogue-shaped-on-fhir.md) | The clinical record is a first-party catalogue shaped on FHIR | accepted |
| [0194](0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md) | An app gets a scoped token through a registration-free authorization flow | accepted |
| [0195](0195-an-app-talks-to-agents-decides-their-requests-and-tails-the-feed-it-reads.md) | An app talks to agents, decides their requests and tails the feed it reads | accepted |
| [0196](0196-a-turn-carries-its-own-context-rendered-once-and-stored-on-its-row.md) | A turn carries its own context, rendered once and stored on its row | accepted |
| [0197](0197-an-agent-pins-standing-context-into-its-prompt-once-per-thread.md) | An agent pins standing context into its prompt once per thread | accepted |
| [0198](0198-a-bundle-offers-its-functions-and-an-agent-takes-them-by-pattern.md) | A bundle offers its functions, and an agent takes them by pattern | accepted |
| [0200](0200-a-background-tool-returns-a-handle-and-its-result-resumes-the-thread.md) | A background tool returns a handle, and its result resumes the thread | accepted |
| [0201](0201-a-continued-thread-replays-its-tool-exchanges.md) | A continued thread replays its tool exchanges | accepted |
| [0202](0202-an-apps-approval-sets-up-what-it-needs-and-its-token-delivers-to-its-webhook.md) | An app's approval sets up what it needs, and its token delivers to its webhook | accepted |
| [0203](0203-an-app-keeps-its-own-package-under-a-declare-scope.md) | An app keeps its own package under a declare scope | superseded |
| [0204](0204-an-app-may-ask-for-full-access-and-its-owner-approves-it-whole.md) | An app may ask for full access, and its owner approves it whole | accepted |
| [0205](0205-an-app-owns-the-package-it-declares.md) | An app owns the package it declares | superseded |
| [0206](0206-a-spooled-webhook-request-refused-for-its-own-content-is-set-aside.md) | A spooled webhook request refused for its own content is set aside as `.refused` | accepted |
| [0207](0207-an-app-declares-one-agent-held-to-its-own-reach.md) | An app declares one approved agent, held to a live approval at every run | superseded |
| [0208](0208-a-repository-proves-its-name-with-a-txt-record-after-registration.md) | A repository proves its name with a TXT record after registration | accepted |
| [0209](0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md) | An app is verified by its authority's TXT record and document | accepted |
| [0210](0210-a-token-declares-new-packages-only-under-its-repositorys-authority.md) | A token declares new packages only under its repository's authority | accepted |
| [0211](0211-the-console-groups-collections-by-package.md) | The console groups collections by package | accepted |
| [0212](0212-a-package-is-published-by-its-authoritys-document-and-installed-from-it.md) | A package is published by its authority's document and installed from it | accepted |
| [0213](0213-a-published-package-keeps-its-publishers-versions-and-an-upgrade-waits-for-every-connected-build.md) | A published package keeps its publisher's versions, and an upgrade waits for every connected build | accepted |
| [0214](0214-an-apps-approval-installs-published-packages-and-declare-and-the-agent-detail-are-retired.md) | An app's approval installs published packages, and `declare:` and the `agent` detail are retired | accepted |
| [0215](0215-code-an-approval-installs-reaches-no-further-than-the-scopes-it-approves.md) | Code an approval installs reaches no further than the scopes it approves | accepted |
| [0216](0216-an-add-on-package-waits-for-the-provider-it-extends-under-the-approval-already-given.md) | An add-on package waits for the provider it extends, under the approval already given | accepted |
| [0217](0217-a-published-agent-takes-its-provider-through-a-bundle-input-and-declares-its-daily-cap.md) | A published agent takes its provider through a bundle input and declares its daily cap | accepted |
| [0218](0218-the-approval-page-writes-a-packages-settings-and-secrets-as-the-owner.md) | The approval page writes a package's settings and secrets as the owner | accepted |
| [0219](0219-an-apps-token-reads-windows-aggregates-semantic-search-blobs-and-the-feeds-history-over-the-kinds-it-reads-and-calls-the-functions-its-call-scope-names.md) | An app's token reads windows, aggregates, semantic search, blobs and the feed's history over the kinds it reads, and calls the functions its call scope names | accepted |
| [0220](0220-outside-code-is-confined-and-reviewed.md) | Code from an outside authority reaches only the hosts it declares, waits for review when it changes, and needs a sandbox that confines it | accepted |
| [0221](0221-first-party-apps-publish-under-substrate-reamde-dev.md) | First-party apps publish under substrate.reamde.dev, and the binary wins for what it ships | accepted |
| [0222](0222-swift-is-the-language-of-the-apps-shared-code-and-android-runs-it-through-the-swift-sdk-for-android.md) | Swift is the language of the apps' shared code, and Android runs it through the Swift SDK for Android | accepted |
| [0223](0223-a-screen-is-a-controller-over-a-pure-engine-and-presenter-and-android-receives-its-presentations-over-one-fixed-bridge.md) | A screen is a controller over a pure engine and presenter, and Android receives its presentations over one fixed bridge | accepted |
| [0224](0224-an-app-is-one-folder-named-once-by-its-app-json-and-the-kits-layering-is-checked.md) | An app is one folder named once by its app.json, and the kit's layering is checked | accepted |
| [0225](0225-an-apps-scope-may-name-a-trait-and-covers-what-star-covers-among-the-kinds-that-bind-it.md) | An app's scope may name a trait, and covers what `*` covers among the kinds that bind it | accepted |
| [0226](0226-a-kinds-status-is-the-core-lifecycle-trait-and-names-its-done-and-dropped-states.md) | A kind's status is the core `lifecycle` trait, and names its done and dropped states | accepted |
| [0228](0228-a-person-an-app-or-an-agent-creates-a-record-under-a-minted-id.md) | A person, an app or an agent creates a record under a minted id, a UUIDv7 in 26 Crockford characters | accepted |
| [0229](0229-a-collections-shipped-saved-views-are-records-of-a-seeded-console-kind.md) | A collection's shipped saved views are records of a seeded console kind | accepted |
| [0230](0230-an-assistant-reaches-a-substrate-through-mcp-with-a-token-that-works-there-alone.md) | An assistant reaches a substrate through MCP, with a token that works there alone | accepted |
| [0231](0231-a-contact-is-a-core-kind-of-the-owners-data-and-a-setting-names-the-owners-own.md) | A contact is a core kind of the owner's data, and a setting names the owner's own | accepted |
| [0232](0232-core-maps-the-providers-people-onto-contacts-and-a-seeded-mapping-waits-instead-of-refusing.md) | Core maps the providers' people onto contacts, and a seeded mapping waits instead of refusing | accepted |
| [0233](0233-the-providers-page-lists-every-outside-service-the-substrate-uses-a-key-or-a-sign-in-for.md) | The Providers page lists every outside service the substrate uses a key or a sign-in for | accepted |
| [0234](0234-firecrawl-is-a-provider-whose-tools-are-offered-to-agents.md) | Firecrawl is a provider whose tools are offered to agents and may run on the operator's key | accepted |
| [0235](0235-an-empty-key-on-an-operator-listed-provider-row-runs-on-the-operators-transport-under-a-daily-allowance.md) | An empty key on an operator-listed provider row runs on the operator's transport, under a daily allowance | accepted |
| [0236](0236-an-owner-may-delete-their-repository-and-the-name-is-freed.md) | An owner may delete their repository, and the name is freed | accepted |
| [0237](0237-a-second-factor-is-the-owners-choice-and-sign-in-asks-for-the-code-only-of-an-account-that-has-one.md) | A second factor is the owner's choice, and sign-in asks for the code only of an account that has one | accepted |
| [0238](0238-the-catalog-tier-says-who-keeps-a-package-and-what-it-is-for-is-read-from-its-declarations.md) | The catalog tier says who keeps a package, and what it is for is read from its declarations | accepted |
| [0239](0239-an-agent-declares-kinds-through-the-vocabulary-tool-under-its-writes.md) | An agent declares kinds through the vocabulary tool, under its writes | accepted |
| [0240](0240-an-agent-changes-long-text-by-exact-replacement-against-the-version-it-read.md) | An agent changes long text by exact replacement against the version it read | accepted |
| [0241](0241-the-assistant-ships-with-the-binary-and-the-owner-shapes-it-through-instruction-records.md) | The assistant ships with the binary, and the owner shapes it through instruction records | accepted |
| [0242](0242-a-model-a-provider-offers-is-a-record-and-an-agent-may-run-on-a-list-of-them.md) | A model a provider offers is a record, and an agent may run on a list of them | accepted |
| [0243](0243-an-applet-is-a2ui-screens-the-server-runs-and-the-console-gives-applets-a-place.md) | An applet is A2UI screens the server runs, seeded in every repository, and the console gives applets a place | accepted |
| [0244](0244-an-applet-names-its-runtime-and-a2ui-is-the-one-the-server-runs.md) | An applet names its runtime, and a2ui is the one the server runs | accepted |
| [0245](0245-a-stale-revision-is-a-validation-refusal-narrowed-by-the-reason-stale-revision.md) | A stale revision is a validation refusal narrowed by the reason `stale_revision` | accepted |
| [0246](0246-a-declared-property-a-derived-only-token-shadows-is-spelled-with-a-leading-dot.md) | A declared property a derived-only token shadows is spelled with a leading dot | accepted |
| [0247](0247-a-status-names-the-open-states-a-record-waits-in-until-its-owner-accepts-it.md) | A status names the open states a record waits in until its owner accepts it | accepted |
| [0248](0248-a-webhook-trigger-a-publisher-ships-is-enabled-only-with-a-key-or-a-signature.md) | A webhook trigger a publisher ships is enabled only with a key or a signature | accepted |
| [0254](0254-an-agents-call-of-a-function-that-declares-approval-waits-for-the-owner-in-chat.md) | An agent's call of a function that declares approval waits for the owner in chat | accepted |
| [0255](0255-a-providers-writes-ride-toggles-of-their-own-and-a-seeded-secretary-takes-the-providers-tools-by-pattern.md) | A provider's writes ride toggles of their own, and a seeded secretary takes the providers' tools by pattern | accepted |
| [0256](0256-a-stability-label-sits-on-a-package-and-on-a-feature-and-a-feature-carries-a-revision.md) | A stability label sits on a package and on a feature, and a feature carries a revision | accepted |
| [0257](0257-a-kind-move-is-checked-only-while-its-old-kind-holds-rows-and-may-add-optional-properties.md) | A kind move is checked only while its old kind holds rows, and may add optional properties | accepted |
| [0258](0258-one-catalog-route-adds-a-package-and-keptby-says-who-keeps-it.md) | One catalog route adds a package, and `keptBy` says who keeps it | accepted |
| [0259](0259-a-repositorys-own-kinds-live-in-the-local-namespace.md) | A repository's own kinds live in the local namespace `~` | accepted |
| [0260](0260-the-stored-format-is-a-contract-each-release-keeps-a-directory-and-history-converts-forward-once.md) | The stored format is a contract, each release keeps a directory, and history converts forward once | accepted |
| [0261](0261-a-repository-is-keyed-by-a-minted-id-and-its-name-keeps-every-other-job.md) | A repository is keyed by a minted id, and its name keeps every other job | accepted |
| [0262](0262-postgres-alone-confirms-a-write-and-an-archive-is-copied-after-the-commit.md) | Postgres alone confirms a write, and an archive is copied after the commit | accepted |
| [0263](0263-the-apis-tests-of-engine-behavior-run-on-the-real-engine-and-its-stand-in-is-deleted.md) | The API's tests of engine behavior run on the real engine, and its stand-in is deleted | accepted |
| [0264](0264-a-repositorys-secrets-travel-with-its-host-key-or-are-left-behind-by-the-operators-choice.md) | A repository's secrets travel with its host key, or are left behind by the operator's choice | accepted |
| [0265](0265-a-package-names-the-function-interface-its-code-is-written-against.md) | A package names the function interface its code is written against | accepted |
| [0266](0266-a-temporary-failure-is-retried-over-minutes-and-the-body-says-which-it-is.md) | A temporary failure is retried over minutes, and the body says which it is | accepted |
| [0267](0267-a-schedule-trigger-says-what-it-does-with-runs-it-missed-and-runs-the-latest-by-default.md) | A schedule trigger says what it does with runs it missed, and runs the latest by default | accepted |
| [0268](0268-a-new-declaration-key-raises-the-dialect-and-a-package-states-the-dialect-it-needs.md) | A new declaration key raises the dialect, and a package states the dialect it needs | accepted |
| [0269](0269-a-publisher-may-not-rename-a-kind-and-code-from-outside-publishers-is-experimental.md) | A publisher may not rename a kind, and code from outside publishers is experimental | accepted |
| [0270](0270-every-hand-that-writes-is-an-actor-record.md) | Every hand that writes is an actor record, and a write links three of them | accepted |
| [0271](0271-an-error-that-may-follow-a-writes-commit-says-so-and-a-write-of-a-named-record-takes-a-retry-key.md) | An error that may follow a write's commit says so, and a write of a named record takes a retry key | accepted |
| [0272](0272-signing-out-everywhere-ends-every-token-the-person-holds-and-every-stream-ends-with-its-token.md) | Signing out everywhere ends every token the person holds, and every stream ends with its token | accepted |
| [0273](0273-body-is-an-ordinary-property-and-no-property-name-is-stored-apart.md) | Body is an ordinary property, and no property name is stored apart | accepted |
| [0274](0274-a-person-signs-in-as-an-identity-beside-the-repositories-and-picks-the-repository.md) | A person signs in as an identity beside the repositories, and picks the repository | accepted |
| [0275](0275-what-a-person-may-do-is-the-roles-they-hold-and-nothing-is-called-owner.md) | What a person may do is the roles they hold, and nothing is called owner | accepted |
| [0276](0276-one-check-holds-every-hand-that-writes-and-one-list-names-the-control-kinds.md) | One check holds every hand that writes, and one list names the control kinds | accepted |
| [0277](0277-the-archive-is-pieces-blobs-and-sealed-values-and-its-manifest-is-the-mark.md) | The archive is pieces, blobs and sealed values, and its manifest is the mark | accepted |
| [0278](0278-the-export-is-the-archive-as-one-file-and-the-snapshot-is-gone.md) | The export is the archive as one file, and the snapshot is gone | accepted |
| [0279](0279-the-archive-holds-against-a-written-contract.md) | The archive holds against a written contract, and the database's record decides whether an archive is its own | accepted |
| [0281](0281-a-repository-the-start-finds-damaged-is-set-aside-in-memory-and-the-rest-serve.md) | A repository the start finds damaged is set aside in memory, and the rest serve | accepted |
| [0282](0282-a-kind-declares-how-long-its-history-is-kept-and-a-repository-may-override-it.md) | A kind declares how long its history is kept, and a repository may override it | accepted |
| [0283](0283-trigger-run-records-keep-a-week-of-history-and-positions-stay-in-the-log.md) | Trigger run records keep a week of history, and positions stay in the log | accepted |
| [0284](0284-a-propertys-mark-stores-whether-sync-may-replace-it-apart-from-where-its-value-came-from.md) | A property's mark stores whether sync may replace it apart from where its value came from | accepted |
| [0290](0290-a-propertys-writer-names-types-of-actor.md) | A property's writer names types of actor | accepted |
| [0294](0294-every-route-is-public-for-an-admin-or-held-to-its-rules.md) | Every route is public, for an admin, or held to its rules | accepted |
| [0295](0295-a-persons-token-is-held-to-that-persons-roles.md) | A person's token is held to that person's roles | accepted |
| [0309](0309-a-conversion-rebuilds-the-fold-only-where-a-line-changed-or-a-step-says-so.md) | A conversion rebuilds the fold only where a line changed or a step says so | accepted |
