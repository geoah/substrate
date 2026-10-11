Reviewer: Codex (gpt-6-astra), 2026-10-11, over the range `origin/main..4b4d9e9b`; the text below is its report, verbatim.
Each link to an absolute local path broke `lint:docs`, so each is now its plain link text.

# Observations implementation review, round 1

Do not merge this range yet: the unit parser panics on valid JSON input, accepts dimensionally invalid units, and permits unbounded computation. The observed index has a purge race and an availability-count mismatch, while the providers expose an OAuth client secret to Python and ship an unauthenticated endpoint that persists rejected requests. Existing tests pass, but the missing cases below can corrupt derived results or consume shared server resources.

## Scope and evidence

Reviewed `origin/main..4b4d9e9b`, with the requested `git log --oneline origin/main..4b4d9e9b` and `git diff --stat origin/main...4b4d9e9b`. The diff contains 320 files, 79,930 insertions and 287 deletions. Source reads used `git show 4b4d9e9b:<path>`; every line reference below refers to that commit, even if the linked working file later changes. `CLAUDE.md` points to `AGENTS.md`.

“Reproduced” below means an executed local check. “Inspection” means the finding follows from the cited code; its proposed database or HTTP reproduction was not executed. No repository file or index was changed. The only authored file is this report.

Checks run:

- `go test -count=1 ./internal/vocabulary/ ./internal/observed/ ./kinds/ ./samples/...`: passed; `samples` has no tests.
- `go test -count=1 -run '^TestObservedWriteRefusesAFallingRevision$|^TestRebuildReproducesTheObservedTables$' ./internal/engine/`: passed in 8.651 seconds.
- `go test -count=1 -run '^TestFilterByQuantityConvertsExactly$|^TestFilterByComparatorReadsTheHalfLine$|^TestOrderByQuantityReadsTheBase$' ./internal/engine/`: passed in 7.222 seconds.
- Offline CLI validation reproduced findings 1 and 2. In-memory execution of committed Python source, with small fake hosts, reproduced findings 8, 9 and 10. These checks created no script files.
- The relevant working sources matched `4b4d9e9b` when checked around the test runs. No full engine suite or `mise run test:db` ran.

## Findings

### 1. P1: A zero unit divisor panics instead of failing validation

Location: internal/vocabulary/unitsgrammar.go:388, internal/vocabulary/unitsgrammar.go:88.

The parser accepts `0` as a numeric factor, then evaluates a reciprocal with `big.Rat.Quo`. `m/0` therefore causes `panic: division by zero` before the measure check can reject it. This parser runs inside quantity writes, declaration loading and host unit calls, so malformed input escapes the validation-error contract.

Reproduced with `go run ./cmd/substratectl validate --partial -f -`, supplying this document on stdin:

```json
{"kind":"substrate.reamde.dev/core/kind","metadata":{"id":"review.example.com/review/reading"},"data":{"authority":"review.example.com","package":"review","names":{"singular":"reading"},"properties":{"value":{"type":"quantity","measures":"length","units":["m/0"]}}}}
```

The process printed the panic, with `unitsgrammar.go:88` in the stack. Reject zero numeric factors before evaluation and make evaluation return errors for invalid arithmetic. Add cases for `m/0`, `/0` and nested zero divisors at the declaration and host-call boundaries.

### 2. P1: Dimension exponents overflow and admit incompatible units

Location: internal/vocabulary/unitsgrammar.go:33, internal/vocabulary/unitsgrammar.go:93.

Dimensions use `int8`, but the parser accepts an unrestricted machine-sized exponent and narrows it during multiplication. `m257` becomes length exponent 1, so the measure checker treats metres to the 257th power as ordinary length. Sums of exponents across components can overflow too.

Reproduced by replacing `m/0` with `m257` in finding 1's document. The validator exited 0 and checked the declaration as `measures: length`. Conversion then has a false dimension on which to base its admission decision.

Use checked arithmetic for every dimension multiplication and addition. Reject exponents or composed dimensions beyond a documented bound, rather than narrowing them. Test both a single overflowing exponent and a compound whose individually allowed exponents overflow when combined.

### 3. P1: Purging after removing `observed` can leave permanent index rows

Location: internal/engine/observedindex.go:409, internal/engine/reprojection.go:678, internal/engine/rows.go:705.

`dropObservedOf` skips deletion when the current declaration no longer binds `observed`. Removing that binding deliberately leaves old points until background reprojection reaches each record. If a purge removes the record first, reprojection cannot find it in `records`; there is no foreign key to remove its points. Its points, rollups and latest contribution can survive after the pass finishes, while rebuild removes them.

Reproduction by inspection: write an observed record; delay the reprojection worker; remove the binding; purge the record with `DeleteInput{Purge:true}`; resume and drain reprojection; compare all three tables with a rebuild. The existing binding test drains the worker before doing anything else and misses this interleaving.

Always remove a purged record's existing observed points, regardless of its current declaration, and settle the affected streams. Apply the same rule to tombstones when old projections may remain. An indexed existence check can retain the optimization for ordinary records.

### 4. P1: WHOOP copies the OAuth client secret into function configuration

Location: kinds/providers.substrate.reamde.dev/whoop/settings.yaml:3, kinds/providers.substrate.reamde.dev/whoop/bundle.yaml:3158.

The new setting explicitly asks for a second copy of the OAuth app's client secret so Python can verify HMACs. `resolveFunctionConfig` injects bundle settings into every bundle function, including the existing sync function (internal/engine/invocationconfig.go:83). This bypasses the same file's explicit rule that OAuth client secrets stay in the host because a compromised function dependency must not receive them.

Reproduction by inspection: configure `whoop/webhooksecret` as documented, then inspect the invocation config of `whoopsync` or `whoopwebhook`; `config.settings.webhooksecret` contains the app secret. Log scrubbing does not restore the boundary once network-capable code has the value.

Verify WHOOP signatures in the host, using the existing OAuth credential without exposing it, or provide a narrowly scoped host verification operation. Remove the duplicate-secret setup step. Keep secret redaction and sealing; those mechanisms are not the defect.

### 5. P1: Installing WHOOP enables durable unauthenticated uploads

Location: kinds/providers.substrate.reamde.dev/whoop/triggers.yaml:111, internal/api/webhooks.go:24.

`whoop-webhook` ships enabled without a key. HMAC verification happens only after `admitWebhook` stores the body as a blob and appends a pending delivery (internal/engine/webhooks.go:155). Missing or invalid signatures park that delivery, keeping its blob alive. This works even before the owner supplies a signing secret.

Reproduction by inspection: install WHOOP, then POST distinct unsigned bodies to `/webhooks/<authority>/whoop-webhook`. Each accepted request can retain up to 8 MiB plus ledger/changelog metadata. The eight-request semaphore bounds concurrent HTTP handling, not accumulated parked deliveries or work queued after a 202 response. No health mirror write is required for the resource consumption.

Ship the trigger disabled until authenticated admission is configured. Require a trigger key or host-side HMAC verification before durable storage and dispatch; retain explicit admission and pending-work budgets. Test that invalid signatures produce no blob, ledger entry or invocation.

### 6. P1: A short unit string can monopolize parsing

Location: internal/vocabulary/unitsgrammar.go:394, internal/vocabulary/unitsgrammar.go:80.

An exponent accepted by `strconv.Atoi` drives one rational multiplication per exponent unit. There is no exponent, evaluation-cost or nesting limit. A quantity using `m1000000000` requests a billion multiplications even though its input occupies only 11 bytes; a scaled atom also grows a large rational. Context cancellation is not checked in this loop.

This is a code-inspection reproduction; I deliberately did not execute the expensive input. It contradicts the brief's bounded-grammar requirement and affects the Go host, beyond a Python function's sandbox timeout.

Reject excessive exponents, nesting, component counts and rational sizes before expensive evaluation. Use exponentiation by squaring only inside those limits; it does not itself bound the size of the result. Test prompt rejection through the ordinary write and host unit-call paths.

### 7. P2: Empty trace codes make `observed_latest.points` drift

Location: internal/vocabulary/observed.go:525, internal/engine/observedindex.go:136, internal/engine/observedsettle.go:84.

A packed `codes` list admits `""`. `TracePoints` leaves that point available, and insertion increments the stream count. SQL deletion, rollup scanning and rebuild instead treat a null numeric value plus an empty code as unavailable. Managed stats also count the empty code.

Reproduction by inspection: write a gauge trace with a valid metric, a one-hour period, `offsets: [0, 1]`, and `codes: ["open", ""]`. Its live latest count becomes 2, while rebuild counts 1. Change an unrelated declared property repeatedly: each delete subtracts one available point and each insertion adds two, increasing the count again.

Reject empty codes, as `host.observed.trace` already does, and define availability once for insertion, deletion, stats and rebuild. Add a regression that compares live tables with rebuilt tables after an unrelated patch.

### 8. P2: Garmin selects a deleted activity edit over the live activity

Location: samples/healthgarmin/bundle.yaml:855, especially line 860.

`host.records.get` can return tombstones. The adapter selects `edit or act or record` without checking `deletedAt`, so a deleted edit remains the preferred source. The fallback at line 828 has the same problem when both mirror rows are deleted. The resulting health workout can retain deleted data instead of falling back or disappearing.

Reproduced by executing the committed adapter with a fake host: a live activity and a tombstoned edited activity at `acct/42`, followed by the edit's delete envelope (`record: null`). The adapter passed the deleted edit's properties to `build_workout`.

Treat every fetched tombstone as absent before fallback and selection. Reconcile both candidate origins, and test deleting the edit alone, the original alone, and both, including a replayed delete delivery.

### 9. P2: Google Fit discards an amount's actual end time

Location: samples/healthgooglefit/bundle.yaml:512, samples/healthgooglefit/bundle.yaml:518.

The steps and calories adapter passes only `startTimeNanos` and value into the trace builder. `endTimeNanos` is discarded. An irregular amount point then spans to the next point or the trace's hourly end, so gaps become measured coverage and a span crossing an hour can be shortened.

Reproduced with one step point: 100 steps from `2026-10-07T10:10:00Z` to `10:11:00Z`. The adapter emitted a trace ending at `11:00:00Z`, with `offsets: [600000]` and `values: ["100"]`. The fold derives a 50-minute amount span, not one minute. Coverage and any coverage-based source selection are wrong even though the sum remains 100.

Preserve explicit amount spans. Emit single amount observations where trace spacing cannot express them, or insert interval-end nulls and handle overlaps and period crossings explicitly. Test gaps, adjacent intervals and an interval crossing the hourly boundary.

### 10. P2: WHOOP truncates account ids containing slashes

Location: samples/healthwhoop/bundle.yaml:192, samples/healthwhoop/bundle.yaml:285.

`_ref_id` keeps only the last slash-separated component. Record ids may themselves contain slashes. The account reference `providers.substrate.reamde.dev/whoop/account/team/watch` therefore becomes `watch`, and the adapter constructs the wrong source account reference and output ids. Accounts `team/watch` and `other/watch` can share an adapter source despite being distinct accounts.

Reproduced directly against the committed helper: that reference returned `watch`.

Remove the known kind prefix while preserving the complete remaining id, as the Garmin and Google Fit adapters do. Add account ids with multiple segments to the adapter fixtures; do not narrow the repository's id alphabet to accommodate this helper.

### 11. P2: Quantity ordering puts valid unlisted units after all numeric values

Location: internal/engine/quantity.go:430, internal/engine/quantity.go:442.

The ordering CASE includes only the table's listed units, although write validation admits additional prefixed and compound codes. With `measures: mass`, `Mg` is a valid megagram, but the mass table does not list it (internal/vocabulary/unitstable.go:316). Its order key becomes SQL NULL. The user-facing promise that quantities sort by their value in the measure's base is therefore false for accepted units.

Reproduction by inspection: store `1 Mg` and `2000 kg` on the same quantity property, then order ascending. `2000 kg` precedes the NULL key for `1 Mg`, although 1 Mg is 1000 kg. The existing ordering test exercises listed units.

Derive a stable numeric ordering key for every admitted unit, using the same conversion rules as validation, or explicitly restrict admissible units for ordered properties. Keep cursor and expression-index keys consistent. Do not silently represent an accepted number as NULL.

### 12. P2: Each quantity filter scans the kind's entire stored unit set

Location: internal/engine/quantity.go:265.

Before executing a comparison, `storedQuantityUnits` selects distinct units from every record of the candidate kinds. It ignores the request's time range, other predicates and page limit, and includes tombstones. Repeating a paginated query repeats that discovery scan. The resulting CASE also grows with unit spelling cardinality, which the grammar allows callers to increase.

Reproduction by inspection: populate a large observed kind using a quantity property, then issue a `first: 1` quantity filter over a narrow date range. Capture both SQL statements: unit discovery still reads the unrestricted kind population before the limited query runs.

Use declaration-only unit sets where they are closed. For open grammar support, maintain derived normalized quantity keys or a bounded indexed unit catalogue instead of scanning all records on every request. Measure this path separately from aggregate reads; the observed-index benchmark does not exercise it.

### 13. P2: Rollup recomputation has no bound on one bucket's total points

Location: internal/engine/observedsettle.go:279, internal/engine/observedsettle.go:649.

A live write reads every point in each touched stream bucket into memory while holding the repository's observed lock. The rebuild path doubles its page limit until an entire bucket fits. The 20,000-point trace limit does not bound a bucket containing many records. Repeated individual writes into one dense bucket repeatedly scan its growing contents; cumulative work is quadratic in the points inserted into that bucket.

Reproduction by inspection: write many allowed traces or single observations with the same stream key and timestamps inside one 15-minute bucket, then patch one record and rebuild. Measure rows read, peak memory and time holding `observedLockKey`; all grow with the bucket population, rather than the changed record or nominal page size.

Stream deterministic reductions over bounded pages, including tie order and counter carry, and bound write-side recomputation work. Retain the serialization needed for correctness until an alternative is proven. Benchmark overlapping records and dense single-bucket writes; the current benchmark spreads traces over disjoint hours.

### 14. P2: Adapter outputs omit the separately required adapter version

Location: samples/healthgooglefit/bundle.yaml:526, samples/healthapple/bundle.yaml:750, samples/healthgarmin/bundle.yaml:877, samples/healthwhoop/bundle.yaml:315.

The adapters stamp `revision`, but none stamps an adapter version. Decision 0162 explicitly requires a separate stamp at docs/decisions/0162-an-observed-row-names-its-origin-and-source-and-composes-its-id-from-upstream-data.md:53, as does plan section 13. Neither `docs/observations.md` nor the adapter READMEs identifies this omission as a deferred requirement.

Reproduction by inspection: process the same upstream object before and after changing an adapter. The output identifies its upstream revision but offers no separate adapter-version field or annotation. The record's kind version describes its declaration, not the function that normalized the reading.

Choose a shared output annotation or declared property, stamp it in all four adapters, and document how an adapter upgrade reprocesses unchanged mirrors. Test an adapter-only rewrite while the upstream revision remains constant.

## What is good and should remain

- The fold is the shared point-maintenance path. Tombstone, restore, purge, rebuild and boot-import tests cover ordinary lifecycle transitions; extend them with interleavings and malformed-but-admitted values instead of creating a second maintenance path.
- Migration `0012` copies `0011`'s `ENABLE`/`FORCE ROW LEVEL SECURITY`, repository `USING`/`WITH CHECK`, connection default and app/maintenance grants. I found no cross-repository policy omission in those definitions.
- Ordinary quantity conversion uses rational factors, and SQL comparisons cross-multiply instead of rounding converted operands. The bounded-value comparator branches implement the documented truth table; the selected database tests passed.
- `TraceStats` uses rational accumulation before its final floating conversion. Rollups sort points and groups deterministically. Counter endpoints and increases retain exact decimal arithmetic within their admitted numeric range.
- The held-interval specification builds transitions before filtering codes, and explicit null points end gauge holds. Preserve those semantics and the raw-versus-rollup tests.
- Apple Health ships its receiver disabled; account resolution refuses an ambiguous account instead of guessing. The webhook transport strips credentials and uses constant-time key comparison. WHOOP's HMAC compares the raw body, uses constant-time comparison, and measures freshness against receipt time so queue delay does not invalidate a legitimate delivery.
- The benchmark states its tmpfs, disabled-fsync and single-machine limits. The next-bucket counter recomputation and asynchronous vocabulary reprojection are disclosed in `docs/observations.md`; they are not undisclosed changes.

## Performance and specification limits

The reported 1,377 bytes per point is a material capacity limit, not a production sizing result. At the plan's 17 million points per year, the benchmark itself estimates about 23 GB per year for `observed_points`, before backups and the other stores (docs/plans/observations/implementation/benchmark.md:82). The two wide text-key indexes and repeated stream metadata explain much of that cost. Follow the report's recommendation to shorten derived stream keys before treating this layout as a release commitment; remeasure corrections, WAL and rebuild on durable storage. Do not edit migration `0012` after it lands.

The observed lock is per repository, not global. It serializes background reprojection with live rollup settlement; ordinary writes already serialize through the changelog lock. Removing it alone would reintroduce the concurrent-recompute fault. The stream index supports a fully specified stream and time range; it does not provide a direct efficient metric-wide time range across arbitrary sources. The benchmark documents listing streams first, which should remain an explicit read-path assumption.

Decision 0157 still says that the inter-bucket counter step is computed at read time (docs/decisions/0157-a-counter-is-stored-as-sent-and-differenced-at-read-under-one-of-three-contracts.md:45). The implementation reads the preceding point during settlement (internal/engine/observedsettle.go:303), and `docs/observations.md:525` explicitly discloses the change. Reconcile the decision before calling the implementation fully conformant; do not describe its rollup rows as independent of other records' points. Its correctness now also depends on invalidating the next counter bucket, not just the changed record's buckets.

The midpoint rule for differing trace offsets is deterministic and documented at `docs/observations.md:421`, but it cannot reconstruct an actual offset transition from two endpoint offsets. The SDK's split-at-transition behavior is preferable for adapters that know the transition; the index midpoint is an approximation of recorded-local attribution.

## What I could not verify

- I did not execute the new purge/reprojection race, empty-code rebuild case, unlisted-unit ordering case, or large-bucket stress cases. Their findings are based on explicit code paths and include inputs or interleavings for regression tests.
- I did not run live WHOOP, Apple Health, Garmin or Google Fit integrations, or the provider database/e2e suites. Adapter reproductions used committed source with fake hosts. Upstream availability, signature freshness policy and real payload coverage remain unverified.
- I did not send unauthenticated requests to a running server or inspect actual secret-bearing invocation frames. Security findings follow the admission and configuration code; no real secret was read or printed.
- I did not rerun the performance benchmark, reproduce its storage totals, or measure memory amplification from an 8 MiB body through JSON envelopes and Python. The HTTP semaphore does not itself bound that downstream memory or accumulated work.
- I did not run console tests or browser checks, all linters, formatting checks, migration checks, or declaration-version checks. This review cannot certify those checks or every line of the large provider fixtures and research documents.
- I did not test numeric extremes that overflow the final float64 statistics, every UCUM grammar composition, or every SQL filter combination. Passing the ordinary quantity tests does not establish those limits.
- The aggregate API, CLI and console observation pages are explicitly outside this range. I did not treat their absence as a finding or infer that their future unit, budget and source-policy behavior is implemented here.
- Nonconventional `Merge branch ...` subjects are present in the requested log. The committed `.mise/commitscheck.sh:72` explicitly exempts merge commits, so I did not report them as a failing conventional-commit check.
