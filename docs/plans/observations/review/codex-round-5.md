Reviewer: Codex (gpt-6-astra), 2026-10-11, over the range `ed10691c...2179ed60^2` at HEAD `2179ed60`; the text below is its report, verbatim.
The report links no absolute local path, so no link needed turning into plain text for `lint:docs`.

# Observations fixes review, round 5

The fixes close the unsigned-webhook retry bypass and repair the reported chart failures, but Google Fit still discards some same-time readings. Discovery still has two filtering failures, and legacy signature verification rounds the receipt time enough to change decisions near `maxSkew`. All authorized checks passed; the remaining cases below need coverage before this review can clear the changes.

## Scope

Reviewed `ed10691c...2179ed60^2`, the second-parent commit log, round 3, its committed fix notes, and the relevant callers at `2179ed60`. HEAD was `2179ed60b59511ec560eec101e8604b2e753445f`; the working tree and index were clean before and after verification. No repository file was edited, no ref was checked out, and no stash was used.

“Reproduced” below means executed against committed source in memory, with the stated stubs. “Inspection” means traced through source, without executing the complete application scenario. Reversion judgments assume the new test remains while its corresponding implementation change is reverted.

## Round 3 findings

1. **Partly:** `webhooks.go:168`, `webhooksignature.go:392`, `functions.go:2022`, and `agents.go:118` enforce stored verification before function and agent delivery; the new unsigned-retry/resume and delayed-signed-retry tests catch the original bypass, but omit millisecond receipt boundaries (new finding 2).
2. **Fixed for the stated development-only scope:** WHOOP `README.md:49` gives the exact secret deletion command; `TestWhoopFunctionsReceiveNoWebhookSecretSetting` verifies both callables before insertion, with a stale record, and after deletion, but does not test an upgrade or fail if the README instruction disappears.
3. **Not fixed, accepted under the stated unreleased-branch premise:** the adapter rename remains incompatible with earlier branch imports; no rename migration or regression test was added, and this review does not assert an affected released installation.
4. **Fixed:** `observations.ts:968` matches bare `timeInState` to the row's structured state reducer; the new helper and timeline component tests use the engine's serialized shape and fail with the old matcher, with no regression found in this path.
5. **Partly:** healthgooglefit `bundle.yaml:518` includes source and nanoseconds in cross-hour amount IDs, and the new two-source test fails without that change; `_single` still collides, and the new test does not cover corrections, source removal, or submillisecond distinctions (new finding 1).
6. **Fixed for hangs and exceptions:** `chart.ts:56` bounds tick enumeration and label precision; both original numeric inputs now finish, and the new cases catch their former failures, but large close values still receive identical labels (new finding 5).
7. **Fixed:** `unitstable.go:545` no longer aliases `st`, Apple maps it locally at `bundle.yaml:191`, and the inventory, canonical-unit, and Apple tests fail if those changes are independently reverted; the upstream comparison found no missing atom or metric flag.
8. **Not fixed, accepted under the stated unreleased-branch premise:** declaration versions remain unchanged and no earlier-branch upgrade test was added; they are defensible as first-release versions if no supported installation received the preceding declarations.
9. **Fixed:** `observations.ts:994` inserts empty buckets between non-adjacent windows; helper and rendered-path tests fail without it, and checks with synthetic null, synthetic zero, and unavailable rows preserved their distinct meanings.
10. **Partly:** discovery narrows server-side, partitions metrics without truncation, and displays named refusal problems; tests pin those changes, but catalogue errors still block explicit metric reads and active facet menus lose alternative values (new findings 3 and 4).
11. **Fixed:** `observations.ts:533` retains milliseconds; the new component test follows a `.123Z` to `.456Z` record link through to the aggregate request and fails with the old formatter, with no regression found.

## New findings

### 1. P2: Google Fit single readings still overwrite another source

Location: `samples/healthgooglefit/bundle.yaml:496` and `:504`.

The unchanged `_single` path computes `slug` but omits it from both ordinary and blood-pressure observation IDs. It also truncates the upstream start to milliseconds. The cross-hour fix therefore leaves the same data-loss mechanism in weight, height, body fat, oxygen saturation, glucose, and pressure readings.

Reproduced by executing the committed adapter with a fake context: one `com.google.weight` dataset containing two points at `1791629995000000000`, origins `scale-a` and `scale-b`, and values 70 and 80 produces only `googlefit/day/body_mass/1791629995000`, holding `scale-b` and 80. The analogous pressure input, 120/80 and 140/90, produces two component observations instead of four, both from the second source. This is an existing gap, not a regression introduced by `_crossing`.

Include the source slug and full start nanoseconds in both `_single` ID constructors. Add tests that retain both sources, correct one without changing the other's identity, remove one without deleting the other, and retain distinct starts within one millisecond. The new cross-hour test never enters `_single`.

### 2. P2: Legacy webhook verification changes the receipt-time boundary

Location: `internal/engine/webhooksignature.go:407`, using `internal/runner/protocol.go:282`.

The retry correctly avoids the retry clock, but `FireEnvelope` serializes `fire.at` with `time.RFC3339`, discarding fractional seconds from the actual admission time. Millisecond signature timestamps are supported, so the new comparison is not equivalent to the original admission comparison.

Failing input: an unmarked, correctly signed request received at `2026-10-11T12:00:00.900Z`, with signed timestamp `12:00:01.500Z` and subsequently declared `maxSkew: PT1S`. Its real receipt skew is 0.6 seconds; retry reads `fire.at=12:00:00Z`, measures 1.5 seconds, and refuses it. Conversely, a signature timestamp of `11:59:59.500Z` is 1.4 seconds old at receipt but passes the reconstructed 0.5-second check. These arithmetic cases were reproduced; a database-backed boundary case was not run.

Preserve the precise receipt instant with `RFC3339Nano` going forward and use the original admission's recorded receipt time for legacy deliveries where available. Define the fallback for already truncated envelopes explicitly rather than treating them as exact. Add tests on both sides of `PT1S` with millisecond timestamps; the current helper signs whole Unix seconds and cannot catch this error.

### 3. P2: A catalogue refusal hides successful filtered reads

Location: `web/console/src/hooks/use-observations.ts:109` and `:143`.

Even when the URL selects one metric, the hook also requests every metric under the source/about filters. Any error from that broader catalogue becomes the whole discovery error. Both pages consequently hide valid selected streams or timeline lanes.

Failing input: 2,001 metrics with one stream each, then open a timeline link selecting one metric. The catalogue exceeds its 2,000-group cap, while the selected metric's stream read fits. Executing the committed hook with stubbed query results reproduced `discovery.isError=true` despite a successful selected-metric query. The server's refusal for the catalogue follows from `aggregatelatest.go:76`; no 2,001-metric database was created.

When metrics are explicitly selected, let their successful reads render and report catalogue failure separately beside the picker. Add a test where only the catalogue fails. Existing refusal tests fail every discovery request, so they do not detect this dependency.

### 4. P2: Reopening a source filter hides sources users can add

Location: `web/console/src/pages/observations.tsx:138` and `web/console/src/pages/observations-timeline.tsx:267`.

The source and about menus derive their options from streams already narrowed by those same filters. This changes the behavior of the existing multiple-selection controls: after a source selection settles, reopening its active menu offers only the selected source. The narrowing is intentional in the fix description, but it removes the ordinary way to add a second source for comparison.

Failing interaction by inspection: start with sources A and B for one metric, select A, close the menu, wait for the narrowed response, then reopen Source to add B. B is absent on both pages. Clearing the filter first restores the options; initial selection through the Add filter popup can retain its captured option list, so the failure specifically concerns the active menu after reopening. About has the same dependency.

Build each facet's options without applying that facet's own selection, while retaining the other filters, or provide a bounded search that can retrieve additional values. Add a select-A, reopen, add-B component test. The new tests exercise metric options, whose separate catalogue avoids this problem, but not source/about options. This is a regression from the prior unfiltered option set.

### 5. P3: Close large chart values get identical axis labels

Location: `web/console/src/lib/chart.ts:143`.

The tick fix terminates correctly for the original large-magnitude input, but `tickLabels(niceDomain([1e20, 1e20 + 16384]).ticks)` still returns `['100000000000B', '100000000000B']`. Reproduced against the committed TypeScript. The two endpoints plot at opposite ends of the axis with the same printed value, so the reader cannot tell their difference.

Choose compact-label precision from the scaled tick spacing, then fall back to sufficiently precise exponent labels if labels collide. Extend the large-magnitude test to require distinct labels; it currently checks only that formatting does not throw. This is a remaining formatting defect, not a new failure of bounded enumeration.

## Webhook trust and persistence checks

`admitWebhook` checks the optional key before the signature, captures `at`, and constructs the request map itself. Only the declared-signature branch writes the boolean mark. A request admitted through a key-only trigger remains unmarked and, after a signature is added, must verify its retained bytes and headers or park. That key-only transition follows from inspection; the new transition test uses a trigger without a key.

The stored fallback loads a blob body when necessary, decodes base64 for binary bodies, preserves text bytes, lowercases retained header names, and rejects multipart. It compares the signature timestamp to stored `fire.at`, not `retryFire`'s current `at` or the later failure's `ParkedAt`. The passing delayed-retry test waits past `PT2S`, accepts both marked and valid unmarked deliveries, and rejects a wrong-secret delivery. The precision exception is finding 2 above.

I found no production caller that can forge `request.signatureVerified`. A body, query parameter, or header with that spelling remains inside its respective nested field. Host effects accept only closed record-mutation shapes (`effects.go:81`); they cannot append a delivery envelope. Host function calls construct `runner.Input` with arguments and config, without a caller-supplied envelope (`runner.go:443`). The provider e2e runner calls HTTP APIs, and Garmin's scenario posts fixture body bytes and selected headers through `/webhooks/...` (`scenario.py:80`). `Stepper.SetEnvelope` can fabricate a runner envelope, but its callers are provider tests, it has no HTTP route, and it does not write a webhook delivery ledger entry.

The mark survives `parkedEnvelope`'s request-map copy and the existing JSON delivery fold, so no SQL migration is needed. Function and agent inputs retain the field. This persistence conclusion is from inspection, not a new rebuild test. The new delayed test alone does not pin the mark because it retains verifying headers; existing signed-admission tests with no retained signature headers would fail if the mark were removed while the delivery check remained. No new test directly asserts callable visibility or attempts marker forgery.

## Adapter, units, and console checks

The WHOOP note correctly identifies the lingering copy and supplies `substratectl delete substrate.reamde.dev/core/secret providers.substrate.reamde.dev/whoop/webhooksecret`. Deletion uses the ordinary record path. The new test proves the copy remains injectable until deletion, then disappears from both `whoopsync` and `whoopwebhook`; it does not claim automatic cleanup. Given the supplied development-only scope, the manual operation resolves finding 2. Reverting the documentation alone would leave that test green.

An in-memory extraction of the current upstream [UCUM 2.2 essence file](https://raw.githubusercontent.com/ucum-org/ucum/main/ucum-essence.xml) matched all 312 vendored codes and metric flags exactly. `st` is an atom for stere; local canonicalization now refuses it because the local parser does not implement it. `ft` parses as femto + tonne. `in`, `oz`, `IU`, `hr`, `cmAq`, `lb`, `mi`, and `count` are neither case-sensitive atoms nor valid prefix-plus-metric-atom spellings. The local aliases map them to `[in_i]`, `[oz_av]`, `[IU]`, `h`, `cm[H2O]`, `[lb_av]`, `[mi_i]`, and `{count}` respectively; Apple overrides `ft` and `st` for its vendor meanings.

The atom file contains copyright, license reference, warranty disclaimer, and license URL; NOTICE identifies the upstream source and extraction. Those cover the attribution items and follow the published appendix in the [UCUM license](https://unitsofmeasure.org/license). This checks notice adequacy, not a legal determination about permission for every form of derived redistribution.

Discovery has at most 32 **stream** reads, plus the initial metric catalogue and metadata/title reads. It does not stop at 256 metrics: 257 produce 29 batches of up to 9; 1,000 produce 32 of up to 32; 2,000 produce 32 of up to 63. Executed checks retained every input metric. A partition exceeding 200 groups still fails visibly with its named problems; the partitioning does not guarantee success for every stream distribution. The existing “more than 200 streams” component test actually returns six fixture streams and refuses only requests without a metric filter, so it pins request narrowing, not a real high-cardinality server response.

The gap insertion leaves returned rows intact and adds a bucket with `rows: []`, not a fabricated `synthetic` server row. Executed `seriesOf` checks produced `[10, null, 20]` for absent windows, synthetic-null rows, and unavailable rows; a synthetic zero produced `[10, 0, 20]`. This agrees with `aggregatefill.go:55` and `:96`, where synthetic null has no numeric value and synthetic zero has `"0"`. The page still sends no `fill`.

Additional chart checks finished for negative-only domains, zero-width domains, subnormal values, `1e308`, and non-finite input. `niceTicks(0, Infinity)` returns no ticks; `niceDomain([Infinity])` filters it and returns `[0,1]`. These checks establish bounded arithmetic, not correct plotting for every representable extreme; for example, subtracting opposite-sign values near `1e308` still overflows the unchanged linear scale.

## Checks and limits

Completed successfully:

- `go test ./internal/vocabulary/ ./internal/api/ ./kinds/`.
- `go test -count=1 -run 'TestWebhook' ./internal/engine/` (26.764 seconds).
- `go test -count=1 -run 'TestWhoopFunctionsReceiveNoWebhookSecretSetting|TestHealthGoogleFitKeepsTwoSourcesAmountsAcrossAnHourApart|TestHealthAppleReadsHealthKitUnits' ./internal/providertest/` (13.664 seconds).
- `mise run console:test`: 164 files, 2,708 tests passed; jsdom emitted scroll/canvas/localStorage warnings.
- `go vet ./internal/vocabulary/ ./internal/api/ ./kinds/` and `go build ./cmd/...`.
- `git diff --check ed10691c 2179ed60` and final clean-tree checks.
- In-memory source execution for Google Fit collisions, chart edge values, discovery batching, the catalogue-error dependency, and gap-row semantics; upstream UCUM comparison without saving a download.

No full database suite, live provider account, browser session, production upgrade, or webhook-ledger rebuild was run. The new retry tests cover function deliveries; the agent guard and automatic interrupted-agent rerun were traced but not separately exercised. No test was added or edited, so reversion sensitivity is reasoned from assertions and implementation, not demonstrated by mutation testing. Findings 3 and 8 from round 3 remain conditional on the supplied claim that earlier branch manifests reached no supported installation; release history and external installations were not independently audited.
