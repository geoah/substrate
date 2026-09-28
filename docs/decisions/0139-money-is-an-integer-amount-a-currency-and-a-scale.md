---
status: proposed
date: 2026-09-28
decision-makers: George Antoniadis (via the money-datatype agent session)
---

# 0139. Money is an integer amount, a currency and a scale

## Context and Problem Statement

A price needs its currency beside it. `decimal`
([0012](0012-numbers-are-exact-or-refused.md)) holds the exact digits and
nothing else, so every kind that stores money declares a second property for
the currency and every reader has to know to pair them. The stored shape of a
built-in datatype is hard to change once rows hold it, so the shape and the
comparison rules are settled here.

## Considered Options

- A `money` datatype stored as `{amount, currency, decimals}`, the amount an
  integer count of minor units
- A `money` datatype stored as `{amount: "19.99", currency}`, the amount a
  decimal string whose fraction is the scale
- No datatype: `decimal` plus a `string` currency, paired by convention

## Decision Outcome

Chosen: `{amount, currency, decimals}`, with `amount` an integer count of
minor units, `currency` an ISO 4217 code of three capital letters and
`decimals` an integer from 0 to 18. `{amount: 1999, currency: EUR, decimals:
2}` is 19.99 EUR. The amount is an integer, so it survives every float64 door
exactly under the `int` bound 0012 already enforces, and no door has to
parse a string to do arithmetic on it. The scale is its own member, never
derived from the currency, because one currency is priced at several scales
(fuel at 3 decimals, a ledger at 2). The decimal-string option carries the
same information but makes every consumer parse digits, and the convention
option leaves a price without its currency wherever a reader forgets the
pairing.

A value is stored as written: 1990 at 2 decimals is not rescaled to 199 at 1,
as a `decimal` keeps `"19.90"`. A `min`/`max` bounds the exact number the value
denotes. A filter's operand is a money value and the comparison holds within
the operand's currency, because an amount compared across currencies answers
nothing. An ordering is one key per term, so it sorts by the exact number
across currencies, and a list that needs one currency filters on it.

### Consequences

- Good, because a price and its currency are one value that no write can
  split.
- Good, because the value is exact on every door, and filters and ordering
  compare exact numbers across scales (1000 at 2 equals 10000 at 3).
- Bad, because an amount is bounded by 2^53 - 1 minor units: at 18 decimals
  that is about 0.009 of the unit, so a wei-denominated balance does not fit.
- Bad, because two spellings of one amount (1990 at 2, 19900 at 3) are two
  stored values: jsonb equality, `contains` on a list and the reserved
  `unique` see them as different.
- Bad, because an ordering over mixed currencies interleaves them by number.
- Bad, because the currency is held to a grammar, not to the ISO list, so a
  well-formed code nobody assigned is admitted.

### Confirmation

`TestCoerceMoneyIsThreeExactMembers` (internal/engine/validate_internal_test.go)
holds the shape; `TestFilterByMoneyComparesWithinACurrency`,
`TestOrderByMoneyComparesTheExactNumber` and
`TestRaisingAMoneyMinIsRefusedOverSmallerRows`
(internal/engine/money_db_test.go) hold the comparisons against Postgres. The
console's copy of the checks is `web/console/src/lib/money.ts`, tested beside
it.

## More Information

Revisit if a deployment needs amounts past 2^53 minor units: the widening is a
string-carried amount, which is a new stored spelling and so a migration.
