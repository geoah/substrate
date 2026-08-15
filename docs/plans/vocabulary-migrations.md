# Plan: vocabulary migrations: kind changes and the records they strand

Status: proposal with options, researched from the code and from prior art,
hardened through an independent Codex design pass on the same brief (which
converged on the same recommendation; its surviving additions are integrated
and the declined ones recorded), a Codex adversarial review of the draft, and
a code-grounded subagent verification of every citation. This is a plan, not
a contract: the code that lands is the contract, and every shape here is a
sketch the implementer holds to the house rules.

## What this is

Kinds are versioned by an incremental integer, and the machinery around that
integer (the boot upgrade, the catalog preview, `kinds:check`) works. What
does not exist is any answer to the question the version raises: when a kind's
definition changes, what happens to the records already written against the
old one? Today the answer is "nothing may happen": every narrowing is refused
while live records hold the old shape, every guard message ends in an
imperative aimed at a human ("null it on them first", "backfill them first"),
and no verb in the system performs any of those imperatives. This plan:

1. Maps where the code stands, with citations.
2. Surveys how the field solves the same problem, and which of those
   solutions fit a system whose changelog is the truth.
3. Lays out the options, with what each allows, forbids, and costs across the
   data, the changelog, the APIs, and every consumer.
4. Recommends a composite and phases it into PRs.

## Where the code stands

The claims the design is built on, verified against the tree:

- **Narrowings are refused against live rows, not blanket-refused.**
  `internal/engine/schemadiff.go` classifies each narrowing as a message plus
  a count query; `narrowingGuards` (schemadiff.go:265) emits a guard only when
  the count of affected live records is positive. Dropping a property nobody
  carries admits cleanly. Both doors (`/vocabulary/apply` and the boot
  upgrade) run the same classifier, the boot door over a deliberately smaller
  kind set (`internal/engine/vocabularywrite.go:535`,
  `internal/engine/seed.go:199`, `classifyNarrowingsExcept`).
- **The fold is registry-free ("SCHEMA-FREE" is fold.go's own word for it).**
  Deltas carry values; folding consults the kind definition for exactly one
  thing, the weighted search bands, and falls back when the kind is unknown
  (`internal/engine/fold.go:28-32`, foldFTS at fold.go:299).
  `RebuildRepository` replays the whole changelog without revalidating
  records against any definition (`internal/engine/rebuild.go:103-137`; its
  lone refusal, a pre-resync merge or split entry, is unrelated to kinds), so
  a rebuild cannot fail because a kind changed, and it faithfully reproduces
  rows the current definition would refuse.
- **The changelog is never rewritten.** Migration `0004` says it outright
  ("The changelog is NOT rewritten (append-only, the truth)",
  internal/engine/migrations/0004_declaration_version_int.up.sql:7-9). The
  one sanctioned exception is `reseal` (`internal/engine/reseal.go:1-18`),
  which rewrites secret values in place: no entry added, removed, or
  reordered, no seq moves.
- **Nothing records which kind version a record was written against.**
  `records.version` is the optimistic-concurrency counter, not a kind
  version (internal/engine/migrations/0001_init.up.sql:60). No column on
  `records` or `changelog` names the definition in force at write time, so
  "which rows predate version 7" is unanswerable, and compatibility can only
  be decided by counting live rows at change time, which is exactly what the
  guards do.
- **`renamedFrom` is reserved, not acted on.** It parses, validates, and
  stores, and the classifier refuses a rename exactly like a drop
  (schemadiff.go:114-121; docs/vocabulary.md documents the reservation).
- **A boot-upgrade refusal is silent to everyone but the server's log.** The
  upgrade is skipped and the refusal logged (`internal/engine/seed.go:201-239`);
  nothing surfaces on the API, the console, or the authority row, so a
  repository can trail the binary's vocabulary indefinitely without its owner
  knowing.
- **Edges are not diffed at all** (noted at schemadiff.go:233-234). Dropping
  an edge declaration lands without a guard.
- **The version plumbing itself is done.** stored+1 on content change
  (`resolveDeclarationVersions`, vocabularywrite.go:555), never-downgrade on
  boot (seed.go:168-181), `kinds:check` refusing an unbumped change in the
  tree (`cmd/vocabularydiff`), the catalog preview running the install door's
  own staging and guards read-only (`internal/engine/upgradeplan.go:173-196`).

The consequence of the first two points together is the observation this whole
plan turns on: **the substrate does not have event sourcing's upcasting
problem.** In classic event stores the fold interprets old events through
current code, so old shapes must be lifted (upcast) or the replay breaks.
Here the fold replays results, not intentions, so old deltas fold forever and
rebuild determinism is untouchable by vocabulary changes. The only real
problem is narrower: live rows hold shapes the new definition refuses, and
nothing in the system can move them.

## What the field does

Condensed from the systems that matter, with what each would mean here.
(Sources: Kubernetes CRD versioning docs and kube-storage-version-migrator;
the Avro specification's resolution rules and Confluent's compatibility
modes; the protobuf language guide; Axon Framework's upcasters and Greg
Young's "Versioning in an Event Sourced System"; Datomic's schema-change
rules; MongoDB's schema-versioning pattern; Stripe's "APIs as infrastructure"
versioning post; Fowler's ParallelChange.)

**Tolerant/weak reading (Avro, protobuf, Datomic, document stores).** Readers
match by name, treat missing as default, ignore extras, and never reuse or
retype a slot; renames are aliases; removal is deprecation. Datomic is the
purest statement ("growth is always additive"; retyping is simply never
allowed; a rename makes the old name a permanent alias) and it is the closest
philosophical fit to the guards this repo already has. Its known cost is the
attribute graveyard: deprecated properties never leave, and nothing but
naming discipline marks them.

**Versioned types with edge conversion (Kubernetes CRDs, Stripe).** Storage
and core speak one current shape; declared converters translate at the API
boundary per request, chained across versions. Kubernetes demonstrates the
failure mode to avoid: its converters are out-of-process webhooks, so a down
converter takes down every read of the kind, and flipping the storage version
rewrites nothing (a separate, chronically-skipped migrator walks and rewrites
every object). Stripe keeps it in-process and admits the other limit: pure
request/response transforms cannot express behavioral change, and the chain
grows forever.

**Upcasting on read (Axon, Greg Young).** Every stored event carries a
revision; an ordered chain of pure functions lifts old revisions to current
form during retrieval; the store is never rewritten; snapshots are versioned
caches, invalidated and refolded rather than migrated. This is the right
answer when the fold interprets old entries through current code, which, per
the observation above, this fold deliberately does not.

**Copy-and-transform (event-store copy-replace, storage-version migrators,
Rails/Django-style imperative migrations).** Rewrite the corpus, pay once,
delete old code. Young's chapter on why not to update events applies to this
changelog verbatim: immutability, consumers holding positions, audit. The
sharpest local translation: `watch` resumption tokens are positions in the
changelog, and any rewrite that moves seq numbers strands every consumer.
Imperative migration scripts have the additional mismatch that they mutate
the fold without touching the truth, which is precisely the drift
`fold.go` exists to prevent.

**Expand/contract (ParallelChange)** is the workflow under all of the above:
add the new shape, migrate readers and data, remove the old only when nothing
holds it. The guards already enforce the contract half (a removal is refused
until the data has moved); what is missing is any machinery for the migrate
half.

One precondition the field agrees on regardless of strategy: **stamp every
stored entry with the version that wrote it.** Avro can only resolve because
every payload names its writer schema; Axon can only upcast because every
event carries a revision. Nothing smarter than counting live rows is ever
possible here until changelog entries carry the kind version in force when
they were written.

## Principles

- **The changelog is the truth, so a migration is writes.** Data moves the
  way data has always moved: deltas appended through the one write path,
  folded by the one fold. A migration that touches `records` directly, or
  rewrites history, is wrong by the same rule everything else is.
- **Rebuild determinism is not negotiable.** Replay must produce the same
  rows before, during, and after a migration, from nothing but the changelog.
  This rules out fold-time upcasting (the fold would depend on the binary's
  chain) and rules in appended migration writes (replay includes them).
- **Guards stay; migrations satisfy them, they never bypass them.** A
  narrowing admits when the count reaches zero. A migration is the sanctioned
  way to reach zero, run in the same transaction and re-counted before the
  new definition lands. No flag skips a guard.
- **Declared verbs, not arbitrary code.** A migration step is one of a closed
  set of verbs the engine implements and can preview, count, and refuse. Not
  CEL, not a function body: an open-ended transform cannot be proven total or
  deterministic, and a step that consults anything outside the row is a
  rebuild hazard by definition. (The verbs act on the fold's current rows and
  emit ordinary deltas; replay replays the deltas, never re-runs the verbs,
  so determinism survives even a buggy verb.)
- **Identity is never reused.** A dropped kind's name, a renamed-away
  property's slot: retired, permanently. Protobuf's `reserved` is the
  precedent; reusing a slot with different meaning is the one corruption no
  tolerant reader can catch.
- **Meaning changes are new kinds.** Every mechanism here moves shape. When
  the semantics change ("duration" becomes "deadline"), declare a new
  property or a new kind and deprecate the old; no verb rewrites meaning.

## The options

### Option A: status quo, sharpened (add-and-deprecate forever)

Keep exactly what exists: additive changes admit, narrowings are refused
while live rows hold the old shape, and humans move data by hand through the
normal API before retrying the change. Sharpen the edges: surface boot-upgrade
refusals (on the authority row and the console), diff edges, and document the
deprecation discipline (a `deprecated: true` marker on properties, so the
graveyard is at least labeled).

- **Allows:** everything additive; deprecation as the answer to every rename
  and removal.
- **Forbids:** effectively all narrowings on any kind with data, because "the
  human moves the data by hand" does not scale past toy counts and has no
  transactional tie to the definition change (the hand-move and the apply
  race against live writers).
- **Cost:** Datomic's graveyard without Datomic's aliases. Bundle authors
  can never rename, retype, or clean up; every mistake in a shipped kind is
  permanent surface area. `renamedFrom` stays a lie in the grammar.
- **Verdict:** the floor, not the answer. Its sharpenings (refusal
  surfacing, edge diffing, `deprecated`) are worth landing regardless of
  what else does.

### Option B: version stamps and tolerant reads (the enabler)

Stamp the kind version in force on changelog entries at write time (a
`kind_version` column; historical entries stay null, meaning "before
stamping"), and expose it on `substrate.Change` and the watch stream. The
semantics are deliberately narrow, because an entry is not always one
record's delta: the stamp is defined for entries whose effect writes a
record's properties (the version of that record's kind, per the registry the
write validated against), and it is absent on entries that only bump, edge,
or annotate, which change no shape. A record row carries the stamp of the
last property-writing delta folded into it, maintained by the fold from the
delta rather than looked up, so rebuild stays registry-free.

This changes no behavior by itself, and Option C does not depend on it (C
selects rows by their live shape, not their stamp). What it buys: "which
rows predate v7" answered cheaply, an upgrade preview that can say "these 40
records were written against three different definitions", watch consumers
holding the writer version the way Avro readers hold the writer schema, and
the hard precondition for any future edge conversion (Option E). It is cheap
now and impossible to retrofit later (history without stamps stays unstamped
forever). The stamp is server-owned end to end: clients never submit it, the
write path stamps from the registry in force, and it reads back under
`status` (which the envelope already reserves for the server), never under
`data`.

- **Allows/forbids:** nothing new; pure instrumentation.
- **Cost:** one column on two tables, one field on the wire (golden file
  regenerated, console `types.ts` follows), a fold change kept
  registry-free.
- **Verdict:** do it first, regardless of everything else.

### Option C: migration verbs, executed as changelog appends (recommended)

A kind document (and a bundle's closure) may declare **migrations**: ordered
steps, each keyed to the single version that introduces it (a step moves
`N-1` to `N`, never `5` to `8`; crossing several versions composes the steps
in order, which is Axon's chain rule and the reason a repository that skipped
versions still converges). A published step is immutable, the way a landed
SQL migration is (`internal/engine/migrate.go` pins hashes for exactly this
reason). Immutability is per repository and changelog-backed: the
migration's own writes record that its steps ran here, so this repository
refuses an edited step under the same version; across repositories it is the
tree's and the catalog's discipline (`kinds:check` and content-hashed bundle
releases), because no repository can know what another one executed. Each
step is one of a closed verb set. When a definition change lands (either
door: `/vocabulary/apply` or the boot upgrade), one transaction runs the
canonical sequence: stage the candidate, project the incoming declarations,
run the steps for every version being crossed as record writes appended to
the changelog through the one write path, let mapping recompute settle, then
re-run the narrowing counts and validate the touched rows against the
incoming definition. If a count is still positive or a touched row does not
admit, the whole transaction rolls back and the guard message names what
remains.

Projection comes first because a plain write validates against the live
registry, which inside the transaction is still the old one: the published
pointer swaps only after commit (vocabularywrite.go:338-341) and
`coerceProps` refuses an undeclared property (validate.go:33-43). The
existing hatches are narrower than this needs: `putKind` swaps the candidate
kind into coercion only (write.go:146-168) and `writeReg` reaches only the
FTS bands, while mappings, trigger admission, reference resolution, and
bundle ownership still consult the live registry mid-write. The executor
therefore needs a transaction-scoped registry that every registry consumer
inside the batch reads, an extension of the hatch that already exists rather
than a new idea, but real work the phasing has to own.

The initial verb set, each mechanically previewable (the preview is a count
query, exactly like a guard):

- **`rename {from, to}`**: move a property's value on every live row that
  carries it. This is `renamedFrom` finally acted on; the declaration keeps
  `renamedFrom` as the permanent alias marker and the retired name becomes
  reserved.
- **`backfill {property, value}`**: set a constant (or the property's
  declared default) on every live row lacking the property. This is the verb
  behind "becomes required while N live records lack it".
- **`null {property}`**: remove a property's value from every live row. The
  verb behind "dropped while N live records still carry it". Also the
  contract half of a retype: drop the old values explicitly rather than
  reinterpreting them.
- **`remap {property, from, to}`**: rewrite one enum value to another on
  every row holding it. The verb behind "removes value(s) while N live
  records hold one". The mapping must be total over the values being
  removed, or the step refuses. **States are excluded**: a state moves by
  transition, not by assignment (validate.go:50-52, write.go:702), and a
  transition fires its declared actions and notifications, so a mechanical
  remap would either bypass the state machine or run its side effects on
  every row. State renames and removals keep today's refusal until they get
  a design of their own.

`null` and any value-collapsing `remap` are **lossy** (from the fold's point
of view: the old values stay in the changelog forever, and no surface may
imply erasure). Lossiness is judged over the composed plan, not per step: two
individually-clean remaps that land distinct old values on one surviving
value have collapsed a distinction, and a remap onto a value the enum keeps
is lossy for the same reason, so the check is injectivity across the whole
plan against removed and retained values alike. A lossy plan never runs
without an explicit yes, and the yes is bound to what was previewed: the
confirmation carries the plan's hash and the changelog seq it was computed
at, and execution refuses if either moved (a bare `--allow-data-loss` would
otherwise authorize whatever the data has since become). The console's
confirmation and the CLI flag both speak that contract. Lossless steps
(`rename`, `backfill`, injective `remap`) need no gate.

Explicitly not in the set, initially: arbitrary retype coercions (string to
int "where parseable" is partial, and partial verbs turn migrations into
row-by-row failures), cross-record moves, edge rewrites, anything computed
from another property. Each can be added later as its own verb with its own
preview once a real bundle needs it; the set accretes the way the vocabulary
does.

Honesty about coverage: the classifier refuses more classes than these four
verbs satisfy (container flips, reference-target narrowing, key-pattern
tightening, nested object fields, states, edges: schemadiff.go:103-252).
The verbs cover the imperatives the guards actually print today (null it,
backfill it, the rename that is refused as a drop, the enum value); every
other class keeps its refusal, with add-and-deprecate as the standing answer,
until its verb earns its way in. Any rule that requires "a covering step"
(the `kinds:check` extension below) is therefore defined per narrowing
class, with "unsupported: expand and deprecate instead" as a legitimate
verdict, not a gap in the rule.

Mechanics that matter:

- **Actor and cause.** Migration writes land under the door's actor:
  `substrate` at boot (`substrate.ActorSystem` already names the boot-time
  vocabulary upgrade as its own, actor.go:31-36), `bundle:{name}` on an
  install or catalog upgrade, and the caller's actor on a hand-applied
  change. Cause is an explicit marker, not `caused_by`: that column belongs
  to function causality (set only when a function's effects apply,
  dataset.go:290-294, and it feeds causal-depth enforcement) and it is not
  on the wire anyway (`substrate.Change` has no cause field, and the
  changelog readers do not select it). Migration entries instead carry the
  step's identity (kind, target version, verb) where the wire can see it,
  beside Option B's `kind_version`, so watch consumers and the console can
  attribute the burst without a join.
- **The verbs are engine verbs, not scripted puts, because the side stores
  are real.** A secret property's material lives in `sealed`, and clearing
  the old name deletes its row immediately (write.go:1327-1335), so a naive
  set-new-then-clear-old rename erases the very value the new name must keep:
  `rename` re-points the sealed ref, or refuses on secret properties in v1.
  Embedding vectors and their queue rows key on the property name and survive
  both rebuilds and record writes (0001_init.up.sql:170-196,
  rebuild.go:33-35), so `rename` and `null` retire the old property's vectors
  explicitly or they stay searchable forever. Property offers need nothing
  (mapping recompute regenerates them from the records), and annotations are
  untouched (their keys are opinion namespaces, not property names).
- **Managers move with the value, verbatim.** `property_managers` is a fold
  table, and an ordinary write would not move a row on rename: it would
  delete the old and claim the new under the migration's own actor, at a tier
  that depends on how that actor resolves (rows.go:609-660), and an
  owner-tier claim permanently blocks mapping recompute from refilling the
  property. So `rename` carries each row's existing manager and tier
  unchanged, and `backfill` claims at machine tier at most, never owner,
  whichever door ran it.
- **One transaction, and the activation gap it does not close by itself.**
  Projection, steps, recompute, re-count, validation: all or nothing, so a
  failed migration leaves the old definition active and nothing visible.
  But atomicity in Postgres is not atomicity of activation: today the
  registry pointer publishes after commit and watchers are signalled at
  commit (vocabularywrite.go:338-344, dataset.go:321), and the vocabulary
  mutex serializes only vocabulary writers, so an ordinary record write can
  resolve the old kind, wait out the migration's row locks, and land an
  old-shaped value after the final re-count. Closing this is part of the
  work, not an aside: every record write holds a registry-generation token
  from kind resolution through commit, the migration takes it exclusively,
  and the new registry publishes before watchers and triggers are signalled.
  Without that, "never observable mid-migration" is a wish.
- **Record mappings are part of the plan, not a bystander.** A write to a
  mapping source recomputes its target, and clearing a mapped property
  invites recompute to refill it from the surviving sources
  (write.go:1051-1067), so a `null` on a mapped property that does not also
  change the mapping converges back to nonzero and the migration refuses.
  That is correct behavior, and the plan has to say it: a step over a mapped
  property is only satisfiable when the closure changes the mapping in the
  same batch, the executor runs recompute after the steps so the counts see
  the settled state, and the preview counts mapped dependents (and their
  deliveries) as part of the blast radius, not just the rows the verb names.
- **A declared ceiling, not unbounded ambition.** The executor refuses a
  plan past an explicit limit, and the limit measures work, not just rows:
  projected changelog entries, side-store operations, mapped dependents, and
  trigger deliveries all scale the transaction and the post-commit burst,
  so the preview estimates them and the ceiling binds on the estimate. The
  refusal prints the expand/contract alternative (add the new shape,
  backfill through ordinary API writes at the application's pace, then apply
  the contraction, whose migration is now cheap). At personal scale the
  limit will rarely bind; when it does, atomicity is worth more than
  one-click convenience, and chunked execution stays deliberately unbuilt
  until a real repository hurts.
- **The boot upgrade runs lossless steps and stays skip-on-refusal for the
  rest.** Boot may run `rename`, `backfill`, and injective `remap`
  unattended (they are exactly as safe as the projection itself). A lossy
  step, a missing step for a narrowing, or a still-positive count refuses,
  skips,
  and now *surfaces*: the refusal is recorded on the authority row and shown
  by the console (Option A's sharpening), and the owner resolves it through
  the catalog door, which shows the plan, the counts, and the lossy gate
  before running it.
- **Preview.** `PlanBundleUpgrade` already stages the install and runs guard
  counts read-only; it extends to list the declared steps, the rows each
  would touch, and the estimated blast radius above. The console's upgrade
  offer shows "runs 4 migration steps touching 61 records" instead of a
  blocked chip; `substratectl` prints the same plan. One posture change
  rides along: today a failed preview silently costs the bundle its upgrade
  offer (internal/api/catalog.go:68-72), which for a malformed migration
  declaration means the owner sees nothing at all. A preview failure becomes
  a visible blocker with the error attached, not an absence.
- **`kinds:check` learns one rule:** a tree change that introduces a
  narrowing of a supported class must ship a covering step, and a narrowing
  of an unsupported class is named as "expand and deprecate instead", so a
  shipped bundle can never put a repository into a refuse-and-skip state it
  has no sanctioned way out of. Not free: the classifier lives inside
  `internal/engine` with a count query welded to every narrowing
  (schemadiff.go:40-65), and `cmd/vocabularydiff` imports only
  `internal/vocabulary`, so classification must first split from counting
  before the tree check can reuse it.
- **Retired identity persists as declaration data.** "Never reused" needs
  somewhere to live: admission today compares current against candidate and
  remembers nothing. A rename leaves `renamedFrom` on the survivor; a
  dropped property or enum value joins a `reserved` list on its kind, and a
  dropped kind joins its authority's, protobuf's `reserved` as declaration
  data, so the prune itself carries the reservation and admission can refuse
  a revenant name in any later candidate.

Effects, surface by surface:

- **The data:** migrated rows change like any write: OCC `version`
  increments, `ifVersion` holders conflict honestly, FTS re-bands, and the
  verbs settle the side stores as above.
- **The changelog:** grows by the migration's deltas; nothing is rewritten;
  rebuild replays definition change and migration alike and lands on the
  same rows. `reseal` remains the lone values-only exception.
- **Watch consumers:** see an attributable burst of ordinary changes; resume
  positions never move. (This is the decisive argument against
  copy-and-replace, which invalidates every consumer's position.)
- **Triggers:** migration deltas are ordinary changelog entries, so record
  triggers deliver them like any change (functions.go:1186-1197): a step
  touching N rows is N deliveries per matching trigger, and on the boot door
  that burst begins at open. The preview's touched-row count is therefore
  also a delivery estimate, and it belongs in the plan the console shows.
- **Functions and agents:** a bundle's closure carries its vocabulary and
  the code that reads it, and the declarations upgrade atomically (function
  bodies compile before the transaction and the runner reconciles after
  publish, so the code follows within the same apply, not the same instant).
  Cross-bundle readers of a renamed property see it vanish; that is
  expand/contract's migrate phase, and the `deprecated` marker plus a
  deprecation window in the bundle's own versioning is the discipline (the
  engine cannot enumerate external readers, and pretending otherwise is how
  contract phases get skipped).
- **REST and GraphQL:** the served shape follows the registry, immediately.
  A renamed property renames the GraphQL field; a client pinned to the old
  name breaks at upgrade time, visibly, rather than reading silently-stale
  data. One cheap courtesy is worth building with the rename verb: the
  GraphQL layer can serve the retired name as a deprecated alias for as long
  as `renamedFrom` names it, reads-only, so generated clients get a window
  instead of a cliff. Full edge conversion (Option E) is the eventual answer
  for clients that cannot move in step; nothing here forecloses it.
- **The console and CLI:** read the registry as today; the upgrade offer
  and `apply` dry-run gain the plan rendering.

- **Verdict:** this is the recommendation. It gives the guards' commonest
  imperatives their missing verbs (the rest keep their refusal and the
  add-and-deprecate answer), and it is the only option in which migration,
  refusal, and definition change share one transaction and one truth.

### Option D: upcasting at fold time

Stamp entries (Option B), keep the changelog polyglot forever, and lift old
shapes to current form inside the fold via per-kind, per-version chains, the
Axon model. Rejected for the core, deliberately:

- It breaks the fold's one invariant: replay would consult the registry (or
  a chain registry) and rebuild output would depend on which binary replays.
  The registry-free fold is why rebuild is trustworthy; spending that to
  avoid appending writes is a bad trade.
- The chain is code that lives forever, runs on every rebuild, and is
  testable only against a corpus of every historical shape.
- Its one real advantage over Option C (the store never grows by migration
  writes) buys nothing at personal scale.

What survives from it: the stamp (Option B), and the snapshot rule already
holds (records are a cache of the changelog; `RebuildRepository` is the
invalidation path).

### Option E: edge conversion (served versions for API clients)

Stripe/Kubernetes-style: the store and the registry speak current-only;
declared, in-process converters serve requests pinned to older kind versions.
Deferred, not rejected:

- Today's client population is the console and `substratectl` (which move
  with the server) and per-repository agents and functions (which move with
  their bundle). Nobody needs a pinned old shape yet.
- It is only buildable on top of Options B and C anyway (converters need the
  stamp, and a sane converter chain needs the store converged on current
  shape, which is what C's migrations do).
- When it comes, it is in-process transforms declared next to the kind, never
  webhooks: Kubernetes is the cautionary tale that an out-of-process
  converter makes vocabulary evolution an availability problem.

### Option F: copy-and-transform (rewrite the changelog)

Read the old changelog, transform, write a new one, cut over. Forbidden as a
product mechanism: it desynchronizes every watch consumer's position, erases
the audit property, and contradicts the append-only contract the whole engine
is built on. The one place its shape may someday be honest is a break-glass
*operator* ceremony (the `reseal` precedent: values-only, entry count and seq
preserved, verified by comparing folds before cutover), and this plan
deliberately does not design it.

## What the reviews changed

The independent Codex pass, run on the same brief before seeing this draft,
arrived at the same composite: stamps, a closed deterministic verb set, eager
execution appended through the one write path in one transaction, upcasting
and lazy migration rejected for the same reasons. Adopted from it:
adjacent-only immutable steps, the `lossy` gate, validating touched rows
against the incoming definition rather than only re-counting, the ceiling
with the expand/contract refusal message, the lossless/lossy boot split, and
the deprecated GraphQL alias window. Declined, with reasons:

- **A baseline changelog event stamping pre-existing history.** Null already
  means "before stamping", the fold needs no mapping to interpret it, and a
  synthetic entry that exists only to annotate the past is the kind of
  changelog write nothing folds.
- **Transaction-boundary metadata on watch (begin/commit markers, safe-resume
  hints).** Real, but heavier than the problem: migration entries carry
  their step's identity, positions stay valid throughout, and a consumer
  that needs atomic application of a burst is a consumer the watch protocol
  does not promise that to today anywhere else either. Revisit if a real
  consumer hurts.
- **An idempotency key on the migration request.** The vocabulary apply is
  already conditional (`metadata.ifVersion` against each document's own
  stored row, `checkSchemaCAS`) and a re-run of a landed migration finds
  zero affected rows; a dedicated key is machinery without a failure mode to
  its name.
- **Write-time forward projection of stale writes.** Writes validate against
  the active definition, as today; accepting old-shaped writes and lifting
  them is edge conversion (Option E), deferred whole rather than half-built
  into the write path.

The code-grounded verification pass then reshaped Option C's mechanics in
five load-bearing places, all integrated above: projection-first ordering
(the live registry inside the transaction is still the old one); states
excluded from `remap` (a state moves by transition, and transitions fire
declared side effects); the secret-property case on `rename` (a naive clear
deletes the sealed material the new name needs); the honest side-store list
(sealed and embeddings need explicit handling, offers regenerate, annotations
are unrelated, and manager rows move verbatim rather than being re-claimed at
the migration actor's tier); and the trigger delivery burst named as an
effect beside the watch burst.

The Codex adversarial review of the draft (with the code in hand) then found
the holes that reshaped it hardest, every one integrated above: the
activation gap and the registry-generation token that closes it (atomicity in
Postgres was standing in for atomicity of activation); the transaction-scoped
registry (the projection's hatch covers coercion and FTS only, while
mappings, triggers, and references still read the live registry mid-write);
mapping recompute breaking the row-local model (a `null` on a mapped property
refills unless the closure changes the mapping too); verb coverage stated
honestly per narrowing class instead of "every guard message its missing
verb"; `caused_by` left to function causality and migration attribution made
an explicit wire-visible marker; lossy confirmation bound to a plan hash and
changelog seq; plan-level (not per-step) lossiness with an injectivity check;
identity reservations persisted as declaration data; per-repository (not
global) step immutability; the ceiling measuring work rather than rows;
preview failures surfacing instead of silently costing the offer; edge
diffing rephased from guard to preview warning until edge verbs exist; and
Option B's stamp given exact semantics and decoupled from C. Its verdict
matched the draft's own claim for itself: directionally sound, not
implementable as first written.

## What is never allowed

The refusals, stated once, so every option above is read against them:

- Rewriting, reordering, or deleting changelog entries. `reseal` stays the
  lone sanctioned mutation, values-only, seq-preserving.
- A migration (or anything else) writing `records` directly. The fold is the
  only writer.
- Fold-time consultation of the registry beyond the FTS bands. Replay
  determinism outranks storage elegance.
- Skipping a guard. Migrations reach zero; they never waive the count.
- Downgrades, anywhere the version integer is compared.
- Reusing identity: a retired kind name, property slot, or enum value never
  returns with a different meaning.
- Non-deterministic or externally-dependent migration verbs (no clock, no
  network, no other record).
- Semantic rewrites dressed as migrations: a meaning change is a new
  property or kind, deprecating the old.

## Phasing

1. **PR 1 (enablers, no behavior change):** stamp `kind_version` on new
   property-writing changelog entries and records; surface boot-upgrade
   refusals on the authority row and the console; classify edge changes as
   preview warnings (a guard would create refusals nothing can yet satisfy);
   add the `deprecated` property marker and the `reserved` lists. Golden
   file and `types.ts` follow.
2. **PR 2 (the ground the verbs stand on):** the transaction-scoped
   registry (every registry consumer inside the batch reads the candidate)
   and the registry-generation token that closes the activation gap, with
   the publish-before-signal ordering. This is engine-invariant work with
   its own tests and no verb in sight, and it is the PR most likely to find
   what this plan missed.
3. **PR 3 (the lossless verbs):** `rename`, `backfill`, injective `remap`;
   declaration grammar on the kind document; the canonical sequence
   (stage, project, migrate, recompute, re-count, validate) in both doors;
   `renamedFrom` becomes the alias record of an executed rename; the engine
   suite grows an evolution corpus (every verb, both doors, mapped
   properties, secrets, managers, rebuild-after-migration equality,
   guard-still-positive rollback). Lossy verbs do not run yet.
4. **PR 4 (the surfaces, and lossy behind them):** `PlanBundleUpgrade`
   lists steps, touched rows, and the work estimate; preview failures become
   visible blockers; console and `substratectl` render the plan; the lossy
   confirmation contract (plan hash plus changelog seq) lands with `null`
   and collapsing `remap` behind it; `kinds:check` refuses a supported-class
   tree narrowing without a covering step and names the unsupported classes.
5. **Deferred, tracked as issues:** further verbs on demand (retype
   coercions, computed backfills, edge and state rewrites), edge conversion
   (Option E), any lazy or chunked execution mode, watch transaction
   metadata if a real consumer hurts.

## Risks

- **A buggy verb writes wrong values durably.** Mitigated, not eliminated:
  verbs are engine code under the evolution corpus, previews show counts
  before execution, and the writes are ordinary deltas (attributable,
  rebuild-stable, and correctable by further writes). The failure mode is a
  bad backfill, not a corrupted history.
- **The verb set is a treadmill.** Every real bundle upgrade will want one
  more verb. Held by the same discipline as the vocabulary: a closed set
  that accretes deliberately, with "add-and-deprecate instead" as the
  standing alternative the guards keep viable.
- **Cross-bundle readers break on rename.** True, and unfixable in the
  engine (it cannot enumerate readers). The `deprecated` marker, the preview,
  and bundle-author discipline are the honest mitigation; Option E is the
  eventual escape for API clients.
- **Transaction size at the tail.** A migration touching every record runs
  in one transaction: write amplification, WAL, and lock time all scale with
  the repository, and mapped dependents and trigger deliveries scale the
  aftermath. The work-measuring ceiling bounds it, the refusal teaches the
  expand/contract alternative, and chunked execution stays deliberately
  unbuilt until a real repository hurts.
- **The activation gap is the sharpest correctness risk.** Until every
  record write holds the registry-generation token, a racing writer can land
  an old-shaped value after the final re-count, and the migration's
  guarantee quietly does not hold. That is why the serialization work is its
  own PR ahead of any verb, not a bullet inside one.
