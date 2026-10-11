Reviewer: Codex (gpt-6-astra), 2026-10-11, over the range `561fc287..ed10691c`; the text below is its report, verbatim.
Each link to an absolute local path broke `lint:docs`, so each is now its plain link text.

# Observations implementation review, round 3

Do not merge this range yet: webhook upgrades can execute previously unsigned deliveries and retain the OAuth secret copy exposed to functions. The adapter rename blocks existing installations, Google Fit can discard an amount, and the console has reproducible chart and request failures. The existing tests pass, but they do not cover these upgrade transitions or several actual aggregate response shapes.

## Scope and evidence

Reviewed `561fc287..ed10691c`, including both requested Git summaries, the earlier reviews, the implementation brief, the API and observations documentation, decisions 0045, 0076, 0097, 0162 and 0164, and the console rules. All line references below refer to `ed10691c`. HEAD matched that commit and the working tree was clean during review.

“Reproduced” means an executed check; “inspection” means the cited code establishes the case, but I did not execute the complete scenario. Additional Go regressions used Linux memory-backed files and `go test -overlay`, without changing repository files or the index. The only authored filesystem file is this report. Round 2's aggregate findings are outside this fix range and are not cleared by this review.

## Findings

### 1. P1: Retrying an old unsigned webhook bypasses the new signature

Location: internal/engine/webhooks.go:150, internal/engine/functions.go:3581, WHOOP bundle.yaml:3114.

Only new HTTP admissions call `checkWebhookSignature`. A stored delivery carries no evidence that this check passed; retry and pending-delivery resumption invoke the current callable directly. Before this change, WHOOP stored unsigned requests and rejected them inside Python. After upgrading, that Python check is gone, so those old requests become executable. A forged `workout.deleted` can reach the deletion branch without an upstream read.

Reproduced with an in-memory engine test: admit an unsigned body on an unsigned trigger using `ReceiveWebhookHeld`, add a valid signature declaration and secret, then call `RetryTriggerFailure`. The result was `ran=1`, and the callable echoed `{"n":"unsigned-before-upgrade"}`. Inspection of `runPendingWebhooks` shows the same missing check on automatic resumption; the full WHOOP upgrade/delete scenario was not executed.

Fix: distinguish durably verified deliveries from legacy deliveries before removing callable verification. Reject or explicitly retire legacy unsigned deliveries, or verify their retained raw body and signature against their original receipt time. Preserve delayed retries of requests that were actually verified; checking their timestamp against the retry clock would wrongly reject them. Test both parked failures and pending deliveries across the upgrade.

### 2. P1: WHOOP upgrades leave the old OAuth secret copy injectable

Location: WHOOP bundle.yaml:128, deleted `kinds/providers.substrate.reamde.dev/whoop/settings.yaml`, internal/engine/settings.go:182.

Removing the shipped secret document does not remove an installed secret record. `InstallBundleClosure` only upserts the supplied data documents, while `bundleSettings` still injects every live secret under the bundle's prefix. An owner who followed the old setup instructions therefore continues to expose the client secret as `config.settings.webhooksecret`, including to `whoopsync`.

The in-memory upgrade test installed the old WHOOP manifests, filled `providers.substrate.reamde.dev/whoop/webhooksecret` with a fake value, and installed the current manifests. The old record remained readable after installation. The injection consequence follows from the unchanged prefix scan at `settings.go:196`; no function configuration containing a real credential was inspected.

Fix: remove or clear this specific obsolete secret through an explicit upgrade operation and changelog write. Document the cleanup for existing installations and test that the upgraded bundle's resolved settings omit it. Deleting the source YAML fixes fresh installs only.

### 3. P2: Renaming the adapters prevents existing imports from upgrading

Location: samples/healthwhoop/bundle.yaml:48, internal/engine/vocabularywrite.go:490, internal/engine/bundles.go:326. The other three adapter renames have the same structure.

The new closure removes `whoophealth` while its installed trigger still references it. Upgrade guards inspect the existing triggers before the closure's data upserts can retarget them to `adaptwhoop`, so supplying the corrected trigger in the same import does not resolve the dependency.

Reproduced by importing the `561fc287` healthwhoop manifests, then importing the current catalog entry. Admission failed with `function geoah.example.com/healthwhoop/whoophealth is referenced by live trigger(s) [healthwhoop-on-mirror]`. The whole upgrade rolls back. Fresh-import tests and tests that change only the `ADAPTER` constant miss this transition.

Fix: retain the existing callable identities, or stage an additive rename that installs the new callable, rewires triggers, and only then removes the old callable. If this is an intentional breaking upgrade, ship exact migration steps and test them for all four adapters.

### 4. P2: Coded timelines discard every `timeInState` answer

Location: web/console/src/lib/observations.ts:893, observations-timeline.tsx:564, internal/engine/aggregateread.go:1395.

The request correctly sends bare `timeInState`, but the server returns each state's value with a structured reducer, such as `{"timeInState":["rem"]}`. The chart searches for the exact key `timeInState`; the returned key is `timeInState:rem`. Every series point becomes null, so the default sleep-stage chart draws no bars despite receiving durations.

Reproduced by passing `seriesOf` one row with `group.code="rem"` and `values=[{"reducer":{"timeInState":["rem"]},"value":"1800","unit":"s","code":"rem"}]`. `seriesOf(rows, "timeInState", "code")` returned a null plotted value.

Fix: resolve an all-states request against the returned reducer family and the row's code. Keep state identity when matching values, and add a component test using the engine's actual serialized reducer shape.

### 5. P2: Google Fit amounts from two sources overwrite one another

Location: samples/healthgooglefit/bundle.yaml:516.

The new cross-hour observation id includes the dataset, metric and start millisecond, but omits the point's source. The same function distinguishes sources for hourly traces. Two cross-hour points from different devices with the same start consequently occupy one `desired` dictionary key, and the second silently replaces the first.

Reproduced by executing the committed adapter in memory with two amount points in one dataset: both span `00:59:55Z` to `01:00:05Z`, with distinct `originDataSourceId` values and amounts 10 and 20. `_traces` produced one observation, containing the second source and 20. This was an adapter reproduction with a fake host, not a live Google Fit response.

Fix: include source identity in the composed observation id, using the same source distinction as the trace path. Preserve sufficient upstream timestamp precision to avoid collisions from truncation, and test corrections and per-origin deletion with both sources present.

### 6. P2: Valid chart values can hang or crash the observations page

Location: web/console/src/lib/chart.ts:46.

`niceTicks` increments a floating-point value until it exceeds the domain, without checking whether adding the step changes that value. At a large magnitude, the step can be smaller than one representable increment, making the render loop infinite. Very small values instead produce a `toFixed` precision above JavaScript's limit.

Reproduced against the committed TypeScript: `niceDomain([1e20, 1e20 + 16384])` exceeded a 300 ms VM timeout because the loop did not advance. `niceDomain([1e-101, 2e-101])` threw `toFixed() digits argument must be between 0 and 100`. These numbers can be written as ordinary decimal strings accepted by `CanonicalDecimal`; the record sparkline converts them directly to numbers.

Fix: enumerate a bounded number of ticks by integer index, verify representable progress, and use bounded precision or exponent formatting. Provide a finite fallback domain when rounding collapses the endpoints. Add both cases to the scale tests.

### 7. P2: The global `st` alias changes a UCUM volume into a mass

Location: internal/vocabulary/unitstable.go:543, internal/vocabulary/units.go:340.

UCUM defines the case-sensitive code `st` as the stere, equal to `m3`. The new global alias maps it to `[stone_av]`, a mass. The collision guard passes because it asks this repository's partial parser, whose atom table does not contain the stere. [UCUM specification, table 21](https://ucum.org/ucum).

The existing alias test explicitly expects `CanonicalUnit("st") == "[stone_av]"`, and that suite passed. Thus a UCUM input `1 st` is interpreted as one stone rather than one cubic metre. I found no evidence that an already admitted local value changes meaning: the previous local parser did not implement `st`.

Fix: keep HealthKit's stone spelling in the Apple adapter beside its foot spelling, and reserve global `st` for the stere. Check alias collisions against the UCUM atom inventory as well as the locally implemented grammar. The other requested spellings, `in`, `oz`, `IU`, `hr` and `cmAq`, did not expose another case-sensitive collision in this review.

### 8. P2: The new adapter property ships without declaration versions

Location: samples/observations/observation.yaml:131, samples/observations/bundle.yaml:32, samples/health/bundle.yaml:25.

`observation`, `trace`, `sleep` and `workout` gain `adapter`, but neither their own versions nor their inherited package versions change. Both packages still ship version 1. The adapter packages also remain at version 1 despite changed declarations and removed callable names. The `ADAPTER` string is output provenance; it does not replace declaration versions.

Inspection reproduction: import observations and health from `561fc287`, then request their upgrade previews under `ed10691c`. `upgradeplan.go:145` compares declaration versions, so these same-id additions do not appear as newer declarations. An existing installation can miss the properties required by the new adapters, even though a fresh import succeeds.

Fix: bump the observations and health package versions for these multi-kind changes, bump the adapter packages, and raise dependency floors where the new code requires `adapter`. Test an upgrade from the base commit, including the catalog offer. This finding concerns repositories that already imported the earlier branch manifests; it does not assert that those manifests reached a public release.

### 9. P2: Timeline lines bridge buckets with no measurements

Location: web/console/src/lib/observations.ts:817, web/console/src/lib/observations.ts:910, timeline-chart.tsx:413.

The request omits `fill`, so the aggregate can omit empty buckets. `seriesOf` creates buckets only from returned rows, and the line renderer connects adjacent array entries without checking the time between their windows. Missing time therefore appears as a continuous line, even when the gauge's hold has expired.

Reproduced with two returned hourly rows, `[00:00,01:00)` and `[03:00,04:00)`, values 10 and 20. `seriesOf` produced two adjacent non-null points, with no break for the missing hours; `LineSeries` connects them. A null row within an existing bucket is handled correctly, but a wholly absent bucket is not.

Fix: request `fill:"null"` within the existing bucket budget, or insert explicit breaks between nonadjacent returned windows. Test a sparse answer where every series lacks the same intervening bucket.

### 10. P2: More than 200 stream groups disable discovery and filtering

Location: web/console/src/hooks/use-observations.ts:68, web/console/src/pages/observations.tsx:164, observations-timeline.tsx:407.

Every observations page first requests all latest groups with the default `maxGroups:200`. Filters apply only after that request succeeds. When the repository exceeds the limit, selecting a metric or source cannot narrow the failing discovery request, and the timeline derives no metric options. It treats a failed latest query as ready and can display “Nothing has been measured yet” instead of the error.

Inspection reproduction: create 201 distinct metric/source/about groups and open `/observations`, then `/observations/timeline`. The engine refuses at `aggregatelatest.go:76`. Adding a metric URL filter leaves `latestRead()` unchanged. The streams error surface also displays only `error.message`, omitting parameter details carried in `ApiError.problems`.

Fix: narrow discovery server-side from the chosen filters, or enumerate metrics first and fetch bounded groups in partitions. Surface discovery failures and their named problems on both pages. Increasing the limit alone only moves the failure to the next ceiling.

### 11. P2: Record timeline links discard subsecond boundaries

Location: web/console/src/lib/observations.ts:527, called by `laneRead` at line 826.

`recordTimelineSearch` preserves a record's millisecond window, but `isoWithOffset` serializes only whole seconds. Opening a subsecond observation or trace can therefore produce an empty aggregate range, while other windows include or exclude unintended points.

Reproduced: formatting `2026-09-01T00:00:00.123Z` and `2026-09-01T00:00:00.456Z` returned `2026-09-01T00:00:00+00:00` for both. The resulting request has equal `gte` and `lt` and is refused instead of showing the record's window.

Fix: preserve milliseconds in the serialized instant, or use `toISOString()` since calendar timezone is already declared separately. Test the complete record-link-to-request path with a window shorter than one second.

## What is good and should not change

The new HTTP admission order is correct for fresh requests: resolve a runnable trigger, check its optional key, verify the declared signature, then spool and append. Wrong keys retain the 404 response; signature failures receive a terse 401. Missing headers, malformed encodings, stale or future timestamps, missing or empty secrets, and multipart requests fail before request storage in the exercised engine paths. OAuth client resolution also rejects an empty client secret.

Keep the closed signature key set, denied credential headers, distinct timestamp and signature headers, exactly one `{body}`, and the requirement that a configured timestamp is actually signed. Seconds and milliseconds are distinguished before comparing the bounded skew. The HMAC consumes `req.Body` directly and uses `hmac.Equal`; the API reads non-multipart bytes without JSON normalization. Verification does not concatenate another body-sized message. The 8 MiB inline limit and eight-request semaphore bound this buffering; multipart parsing has its separate 32 MiB cap before rejection by a signed trigger.

Fresh WHOOP deliveries no longer need signature headers or the client secret in Python. The new provider tests exercise that behavior and the host verification path. Preserve this boundary while fixing legacy deliveries and settings.

Round 1 finding 8's Garmin tombstone handling now reads both activity candidates as live-or-absent and reconciles both origins. Its deletion and replay tests passed. Finding 10's WHOOP account-id fix removes the known kind prefix instead of splitting at the last slash; the slash-id test passed. Finding 9's Google Fit gap end and cross-hour span tests passed, although finding 5 above exposes a new identity loss. Finding 14's adapter stamps work with equal revisions, and the four adapter-only rewrite tests passed; keep that separation between upstream revision and code version.

The console uses `/api/v1/records` through the existing authenticated request helper. Copied curl commands contain `$SUBSTRATE_TOKEN`, not the session token. I found no new request path outside `/api` or session-token disclosure. The request builder uses the documented field names and full record paths, summary lanes default to `from:"summaries"`, and unit options are constrained by the measure table. Backend default budgets remain active, and the timeline rejects obviously excessive bucket estimates locally; this is bounded failure, not an unbounded successful query.

Null numeric values are excluded from chart arithmetic rather than converted to zero. Empty data, ordinary single values, and negative domains passed direct checks. Trace point times use millisecond offsets or `every` multiplied by ordinal; explicit null points break the sparkline. `unavailable` rows suppress plotted values, synthetic rows are labelled in the tooltip/table, and `exact:false` is disclosed in technical mode. Lane errors display 422 problems; discovery errors need the correction above.

Identity components retain full references with wrapping. New chart components use semantic colour variables with separate light and dark definitions. The type-floor and existing text-contrast tests passed as part of the console suite. Parsing the committed YAML confirmed 10 provider bundles and 16 sample bundles, and the catalog table's kind/function/data-record/agent counts matched the listed bundles.

## Replay behavior and verification limits

A valid signed request can be replayed within its skew window. Each admission gets a new fire id; the signature implementation stores no replay nonce and WHOOP does not deduplicate `trace_id`. WHOOP updates fetch current upstream state and repeated deletes target the same mirror id, so the existing repeated-delivery tests demonstrate record convergence, but repeated admissions still consume ledger, invocation and upstream work. Timestamp freshness is not duplicate suppression, and other signed callables may have non-idempotent effects. No replay-flood or heap benchmark ran.

The signature parser intentionally allows a secret record that does not yet exist and fails closed at delivery. However, its secret-reference check only checks the core-kind prefix and a nonempty suffix, so malformed record ids are not all refused at declaration time. Tightening that syntax would improve write-time feedback; I did not find a signature bypass through it.

No browser rendering, live provider account, production upgrade, full engine/provider suite, or `mise run test:db` ran. The passing console suite uses mocks and jsdom, not a live aggregate server; it emitted scroll/canvas/localStorage environment warnings. The existing contrast tests cover text and interface tokens, not a measured audit of every new chart series. The CSS itself discloses three light-mode series below 3:1 and provides legends and tables; I did not verify the palette's stated colour-vision measurements.

I did not execute the 201-stream UI scenario or a catalog-preview regression for the unchanged health/observations versions. Those findings are based on the request builders and version-comparison code. The legacy secret's continued injection follows from the prefix-based resolver; the upgrade check verified persistence, not a captured plaintext runner frame. The exact WHOOP legacy deletion and automatic pending-resumption scenarios remain inspection cases, supported by the executed generic retry bypass.

Checks completed:

- `go test ./internal/vocabulary/ ./internal/api/ ./kinds/ ./samples/... ./internal/catalog/`: passed; catalog took 145.745 seconds. This authorized catalog command includes database-backed catalog tests in this checkout.
- `go test -count=1 -run 'TestWebhook' ./internal/engine/`: passed in 27.864 seconds.
- `go test -count=1 -run 'TestWhoopWebhook|TestHealthGoogleFitKeepsAnAmountsEnd|TestHealth.*Adapter|TestHealthGarmin.*(Delete|Tombstone)|TestHealthWhoop.*Slash' ./internal/providertest/`: passed in 77.834 seconds.
- `mise run console:test`: 164 files and 2,694 tests passed.
- `go vet ./internal/vocabulary/ ./internal/api/ ./kinds/ ./samples/... ./internal/catalog/` and `go build ./cmd/...`: passed.
- In-memory Go overlays reproduced unsigned queued delivery after adding a signature, legacy secret persistence, and the blocked healthwhoop rename. In-memory TypeScript/Python checks reproduced the coded-chart mismatch, numeric chart failures, Google Fit collision, sparse-series gap, and subsecond truncation.
- `git diff --check 561fc287 ed10691c`: passed. Repository files and the index remained unchanged.

## Fix notes

Added after the review by the agent that fixed it; the report above is
unchanged. Each fix is one commit whose title starts with the change and
whose body ends "Review round 3, finding N".

- Finding 1: the door stores `request.signatureVerified: true` in the
  stored envelope the delivery ledger already keeps, so no migration was
  needed. A stored request without the mark is verified from its kept body
  and headers against `fire.at` before a retry, a resume or an agent rerun
  runs it, and parks naming the signature when it does not verify.
- Finding 2: only a development install can hold the record, because
  nothing on this branch was released; the WHOOP README's upgrade note tells
  its owner to delete it, and a test pins that the closure ships no setting.
- Finding 3: no change, because the adapter callables were renamed on this
  branch before any release, so no repository holds the earlier names to
  upgrade from.
- Finding 8: no change, because the `adapter` property was added on this
  branch before any release, so the shipped package versions are the first
  versions any repository receives.
- Finding 7: `testdata/ucum/atoms.tsv` lists the 312 atom codes of UCUM
  2.2's `ucum-essence.xml`, fetched from the UCUM repository because
  neither the essence file nor the functional suite (which never writes
  `st`) was in the tree. Checked against it, `st` (the stere) was the one
  alias that is a UCUM expression; `in`, `oz`, `IU`, `hr` and `cmAq` are
  not UCUM codes, alone or as a prefix and a metric atom.
