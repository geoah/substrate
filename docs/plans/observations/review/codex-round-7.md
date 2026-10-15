Reviewer: Codex (gpt-6-astra), 2026-10-11, over the ranges `9b4a6bb5..f7c73a30` and `f7c73a30..c83975c0`; the text below is its report, verbatim.
Each link to an absolute local path broke `lint:docs`, so each is now its plain link text.

# Observations review, round 7

Do not approve `c83975c0` yet: Google Fit still overwrites readings from same-model devices, failed facet reads silently remove choices, and several large aggregate helpers still delay cancellation. The receipt-time, selected-metric rendering, axis-label, recorded-local carry, and two test-gap fixes are correct by inspection, with no numerical or sort-order regression found. The permitted checks I ran passed; database-backed assertions and rebuild behavior were reviewed from code only.

## Scope

Reviewed the round 5 and 6 reports, both specified second-parent commit logs and diffs, and the resulting source at `c83975c0397f7a995b12f800d53b487dd003b8c4`. The working tree and index matched that commit before and after verification. No checkout, stash, repository edit, database test, Docker command, or Postgres operation was performed.

Locations below refer to that commit. Executed reproductions used committed source in memory with stated stubs. Test sensitivity to reverting a fix is reasoned from the assertions, not demonstrated by editing the implementation.

## Status of the previous findings

- Round 5.1, Google Fit reading IDs: **partly fixed**. `samples/healthgooglefit/bundle.yaml:477,504,512,524` shares the source-slug/full-nanosecond constructor across ordinary, pressure and cross-hour readings; the new correction/removal test pins distinct slugs, but same-model devices still collide (finding 1).
- Round 5.2, receipt precision: **fixed**. `internal/runner/protocol.go:286` preserves fractions and `internal/engine/webhooksignature.go:433` recovers the original admission receipt; envelope/unit tests and the millisecond database fixture cover the intended branches, subject to finding 4's timing weakness.
- Round 5.3, catalogue errors hiding chosen metrics: **fixed**. `web/console/src/hooks/use-observations.ts:152` separates `metricListError` from discovery when metrics are explicit; both page tests refuse only the catalogue and assert visible chosen data plus the named problem.
- Round 5.4, active facet menus losing alternatives: **partly fixed**. `web/console/src/lib/observations.ts:908` omits each facet's own selection; both pages now test select, close, reopen and add for Source and About, but failed facet reads recreate the missing-options symptom silently (finding 3).
- Round 5.5, identical axis labels: **fixed**. `web/console/src/lib/chart.ts:143` derives suffix precision from spacing and falls back to exponents; tests require distinct large-value labels, ordinary compact labels and an actual division-collision fallback.
- Round 6.1, recorded-local gauge carry: **fixed**. `internal/engine/aggregateread.go:149,212` checks the carried value before raw-read budgeting; `TestAggregateRecordedLocalDayCarriedAtAnOddOffset` compares both paths and explicitly requires `840/30` and `960/810` seconds for offsets `+1/-1`.
- Round 6.2, cancellation inside reducers/helpers: **partly fixed**. The `...Context` reducers, point/span copies, priority subtraction and merge, and sort comparisons now check cancellation; the new tests exercise these checks, but other whole-input helper passes remain unchecked (finding 2).
- Round 6 test gap, cumulative `maxSamples`: **fixed**. `internal/engine/aggregate_paths_db_test.go:387` requires the `1 + 1 + 3 = 5` charge to fail at four before coverage SQL and succeed at five with the expected sum and coverage.
- Round 6 test gap, M4 `exact`: **fixed**. `internal/engine/aggregate_paths_db_test.go:1079` asserts `cols.Exact` alongside the converted extrema; retaining correct digits while returning `exact: false` now fails.

## New and remaining findings

### 1. P2, blocking: Google Fit overwrites same-model devices

Location: bundle.yaml:387, used by `_reading_id` at line 481.

`source_for` constructs device identity from manufacturer and model, discarding `device.uid`. Two physical devices therefore supply the same slug to the repaired reading constructor. Hashing a long ID preserves that collision because the input strings are already identical.

Executed failing input: datasource origins `raw:com.google.weight:com.example.scale:uid-a` and `raw:com.google.weight:com.example.scale:uid-b`, with device UIDs `uid-a` and `uid-b`, both manufacturer `Withings`, model `Body`; one dataset `day` contains values 70 and 80 at `1791629995000000000`. Executing committed `_Ctx.source_for` and `_single` produced one observation, `googlefit/day/withings-body/body_mass/1791629995000000000`, holding 80. The first reading is lost. Blood pressure and cross-hour amounts use the same lossy source identity.

Include stable device identity, or a digest of the full upstream origin, in the reading key; keep the manufacturer/model text for display. Add same-model/different-UID fixtures covering initial delivery, correction and removal, including a hashed key. The new test uses different application names, so it cannot catch this case. This is a remaining data-loss case, not a regression introduced by the new hash helper.

### 2. P2, blocking: Aggregate helpers still process whole inputs after cancellation

Locations: spans.go:198, called by aggregateread.go:1396; additional gaps at `aggregateread.go:960,1023,1046` and `internal/observed/cancel.go:45,77`.

`amountCoverage` still calls plain `UnionMs`, which copies every extent, performs an ordinary stable sort, and walks the result without a context. A metric read of one amount stream with 100,000 quarter-hour rows, one output bucket and `sum`, canceled just after the cell check, still processes all 100,000 extents before observing cancellation. This counterexample follows the call path; no database reproduction was run.

Other unbounded passes remain: selecting and filtering a summary bucket's members, summing all joined coverage intervals, copying all intervals in `cover`, initializing the new sort's complete index array, and scanning already-visited permutation entries. These are outside the two exceptions named in `docs/api.md:1367`.

Add a cancellable `UnionMs` path and checks in the remaining explicit loops; chunk explicit bulk copies or document their limits. Extend controlled-context tests to coverage and summary selection, and count visits in sort initialization and permutation scans. Existing sort tests count comparisons, which cannot expose unchecked index-only loops. The reducer and priority fixes are effective, but the broader cancellation claim remains incomplete.

### 3. P2, blocking: Facet-query failures hide available choices without an error

Location: use-observations.ts:292, particularly lines 319 and 352; the cap is requested by `web/console/src/lib/observations.ts:922`.

`useFacetValues` reads query data but discards both queries' errors, loading state and retry functions. If a query fails, the hook offers only values from the currently selected streams, with no indication that alternatives failed to load.

Failing input: one metric has 2,001 sources, and the URL selects source A. The filtered stream read succeeds, but the Source facet omits A's filter and exceeds its 2,000-group cap. Reopening Source shows only A. Executing the committed hook with a refused source-query result and a successful A stream reproduced `sources: ['A']`; the returned object exposes only `sources`, `abouts` and `nameOf`. A transient network failure with just A and B causes the same behavior. About has the same gap.

Expose a per-facet error and retry beside the affected control while preserving the selected data. For cap refusals, offer bounded search/narrowing or explicitly explain why the complete list is unavailable. Add tests that refuse only a facet query. Current reopen tests cover successful facet responses only. This is an incomplete error path in the new facet implementation.

### 4. P3, follow-up: The receipt-boundary test depends on admission timing

Location: webhooks_signature_db_test.go:511, using `atNextFraction` at line 484.

The fixture signs relative to the scheduled `.900` instant, before repository/trigger reads reach the actual admission clock. A delay of 100 ms moves admission into the next second, where truncation no longer flips the two expected decisions; a delay beyond 1.6 seconds can make the supposedly inside request genuinely outside `PT1S`.

Use an injected admission clock or assert and control the actual recorded receipt relative to the signed timestamp. The runner and pure receipt tests are deterministic, but this database test does not unconditionally pin both boundary decisions. No timing failure was observed because database tests were prohibited.

## Identity, persistence and regression checks

The new reading constructor meets the requested shape and preserves full start nanoseconds in the ID, even though the observation timestamp itself retains only microseconds. I executed the committed Python `Ids` implementation with `_reading_id`: a 128-byte readable key stays readable, a 129-byte key becomes 42 ASCII bytes, repeated long inputs produce the same ID, and different source slugs produce different hashes. The alphabet gate ensures any unhashed result is ASCII, so Python character length does not admit an oversized multibyte ID. Distinct starts one nanosecond apart also produced distinct IDs. These checks do not repair the source-slug collision above.

Sleep and workout IDs use the mirror session ID, not a rounded start instant; distinct session rows therefore remain distinct even at the same time. The provider's session key includes the account and upstream session ID. Sleep stages use a unique ordinal within each session, so they do not have `_single`'s overwrite at an identical instant. Removing an earlier stage can renumber later stages, an existing identity limitation outside this fix.

Webhook admission uses one `at := nowUTC()` for signature verification, `FireEnvelope`, and the pending failure's `ParkedAt` (`webhooks.go:149,162,175`). `nowUTC` truncates to microseconds, matching `docs/functions.md`. The fallback reads that failure from the admission changelog payload at the original failure ID, checks trigger/fire identity and the pending marker, and accepts its timestamp only within the envelope's named second. It does not use the changelog transaction timestamp or a later retry's `ParkedAt`.

Retries retain the original failure ID. Rebuild replays the stored delivery operations and their IDs/timestamps (`delivery.go:90`, `rebuild.go:278`); the admission changelog remains available. Thus rebuilding should preserve legacy fallback behavior. The new test truncates only the SQL failure payload, not its changelog entry, and does not directly pin a genuinely legacy envelope through rebuild. Missing admission entries and mismatched seconds intentionally fall back to the start of the named second, as documented.

Apple Health's parser accepts fractional digits, Garmin uses `datetime.fromisoformat`, and Pebble passes the fallback fire timestamp through unchanged. I found no fractional-time incompatibility in these readers.

The facet reads use latest-group discovery, not raw sample reduction, with 2,000 groups per facet. They add two aggregate query shapes per settled filter state outside the 32 stream-query limit, plus reference-title reads. An initially unselected timeline can first query broad facets and then query again when its default metric becomes known, so “two extra requests per page” is not a literal lifetime request bound. The main discovery retry does not refetch failed facets.

The sort uses stable 1,024-element runs, chooses the left run on equality, then applies the resulting permutation by cycles. Its passing standard-library comparison test includes repeated keys and lengths on both sides of the run boundary. Context reducers retain the prior filtering, arithmetic order, interpolation and hold calculations; plain forms delegate to those implementations. I found no changed numerical result. The new sort allocates two integer arrays for large inputs; no memory benchmark was run.

`TestAggregateStopsInsideAReducer` cancels after the outer reducer check and tests a point copy, a percentile using cached points, and a duration reducer using cached spans. The unit tests separately cancel after counted internal checks. Together they pin cancellation inside the changed reducers, but not every helper or every sort phase. The carry test would fail without the planning change because it compares the normal and forced-raw paths and independently asserts both daily durations.

## Checks and limits

Passed:

- `go test ./internal/observed/ ./internal/substrate/ ./internal/api/ ./internal/runner/ ./internal/vocabulary/`.
- `go build ./...`.
- `go vet ./internal/observed/ ./internal/substrate/ ./internal/api/ ./internal/runner/ ./internal/vocabulary/ ./internal/engine/`.
- `mise run console:test`: 164 files, 2,717 tests; jsdom emitted scroll/canvas/localStorage warnings.
- `git diff --check` for both specified fix ranges.
- In-memory executions of Google Fit identity cases, actual Python ID hashing, failed-facet fallback, and compact/exponent chart labels.

No `internal/engine`, `internal/providertest`, `internal/providere2e` or `internal/catalog` tests ran. I did not execute the carry, cumulative-budget, M4 or webhook database fixtures; rebuild persistence, their expected failure on reversion, and the remaining aggregate cancellation cases are inspection conclusions. No live provider, browser session, production upgrade, cancellation-latency benchmark or memory benchmark was exercised. This report is the only authored file.
