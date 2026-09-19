---
status: accepted
date: 2026-09-16
decision-makers: George Antoniadis
---

# 0091. A parked delivery is retried or forgotten, and both end the row

## Context and Problem Statement

The automation API offered `list` and `retry` over a trigger's parked
deliveries and nothing else. Mneme v6 parked twenty deliveries of one trigger
under a body it then fixed; `substratectl trigger retry` answered "retried" for
every one of them, twice, and every row was still there at attempt 1
(geoah/substrate#579). Two things were wrong. A retry whose `when` guard no
longer matched returned before the transaction it would have settled in, so a
settled delivery left its row parked and the caller was told it had been
retried. And there was no verb at all for a delivery that can never be made
again — an uninstalled callable, a deleted record, work that landed by another
route — so a repository that had ever parked could never read `parked == 0`.

## Considered Options

- Make `retry` delete the row whenever it does not park again, and add nothing
- Add a `forget` verb only, and leave `retry`'s skip as it was
- Both: `retry` settles the row however the delivery settles, and `forget`
  removes a row without running anything
- Expire parked rows on age

## Decision Outcome

Chosen: both, as two verbs with one rule each.

**A retry RUNS the delivery, and the row ends when the delivery settles.**
Effects settle it; a guard that no longer matches settles it too, because a
skip is a settled delivery everywhere else in the dispatcher (a dispatched skip
writes its run row and advances the cursor). Only a delivery that FAILS again
re-parks, one attempt older, which is what the row is for.

**A forget RUNS NOTHING and resolves nothing.** `DELETE
…/trigger/{id}/parked/{failureId}`, `substratectl trigger forget`. It does not
ask whether the callable resolves — the row it exists for is precisely the one
whose callable is gone — and it retires the failure through the same unpark
fold a delivered retry writes, so a rebuild and a restore agree the delivery
is over. It takes the running claim first, so a forget cannot race a retry
that is already running the same failure.

Age-based expiry was rejected: a parked delivery is work somebody has to
decide about, and a clock deciding silently is how the twenty rows would have
become invisible instead of answered. Retry-deletes-always was rejected for the
same reason from the other side — a delivery that failed again must stay.

### Consequences

- Good, because `parked == 0` is reachable on any repository, which is what a
  scenario, a dashboard and an operator all want to assert.
- Good, because "retried" now means the row is gone or the row failed again,
  and the caller can tell which without re-reading the list.
- Bad, because an operator can now delete a delivery that would have succeeded.
  It is deliberate and it is why the two verbs are separate words: retry is the
  one that tries.
- Bad, because a forgotten delivery leaves no run record. It writes an unpark
  entry in the changelog and nothing else — a retry writes no run record
  either, so the two agree, and the changelog is where the history is.

### Confirmation

`internal/engine`'s `TestARetryTheGuardNowSkipsRetiresTheParkedRow` parks a
delivery, makes its guard false, retries, and asserts no row is left;
`TestAParkedDeliveryCanBeForgotten` tears the callable down under a parked row,
watches the retry refuse, forgets it, and asserts a second forget is a
not-found.

## More Information

Issue [#579](https://github.com/geoah/substrate/issues/579); the ask is P in
geoah/mneme-v6 `docs/upstream.md`. The ledger the rows live in is
[0064](0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md),
and an accepted webhook's pending entry is
[0068](0068-an-accepted-webhook-is-a-pending-entry-in-the-delivery-ledger.md) —
a forget of one of those drops the request without running it, which is the
same judgement about the same row.
