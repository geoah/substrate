---
status: accepted
date: 2026-09-28
decision-makers: George Antoniadis
---

# 0140. Money is an integer amount of its currency's minor unit

## Context and Problem Statement

A price needs its currency beside it. `decimal`
([0012](0012-numbers-are-exact-or-refused.md)) holds the exact digits and
nothing else, so every kind that stores money declares a second property for
the currency and every reader has to know to pair them. The stored shape of a
built-in datatype is hard to change once rows hold it, so the shape and the
comparison rules are settled here.

## Considered Options

- `{amount, currency}`, the amount an integer count of the currency's ISO
  4217 minor unit
- `{amount, currency, decimals}`, the scale stored beside the amount
- `{amount: "19.99", currency}`, the amount a decimal string
- No datatype: `decimal` plus a `string` currency, paired by convention

## Decision Outcome

Chosen: `{amount, currency}`. `amount` is an integer count of minor units and
`currency` an ISO 4217 code; the code's ISO 4217 minor unit places the decimal
point. `{amount: 1999, currency: EUR}` is 19.99 EUR and `{amount: 1999,
currency: JPY}` is 1999 yen. Payment APIs carry money the same way, as an
integer of minor units beside a code, and the minor unit is a fact about the
currency, so storing it per value only adds a field that can disagree with
the currency. The integer survives every float64
door exactly under the `int` bound 0012 enforces. A stored scale (the second
option) was built first and dropped on review: it admitted two spellings of
one amount and made every writer choose a number the currency already
answers. A decimal string makes every consumer parse digits, and the
convention option leaves a price without its currency wherever a reader
forgets the pairing.

The engine owns the code-to-minor-unit table (`internal/vocabulary/money.go`):
every code the ISO 4217 maintenance agency's list one gives a minor unit
(checked against the list published 2026-09-17), plus the codes withdrawn
since, because a price recorded in one is still a price. Codes with no minor
unit are left out. The table only grows: a code is added, never removed and
never given a new minor unit, because a stored amount is read through it. A
removed code would leave its rows unwritable, and a changed minor unit would
rescale every amount in that currency on read without a write; either is a
migration of the rows. The console carries a copy, held equal by a test. A `min`/`max` bounds the exact number
the value denotes. A filter's operand is a money value and the comparison
holds within its currency, where one minor unit is one minor unit. An
ordering is one key per term, so it sorts by the exact number across
currencies, and a list that needs one currency filters on it.

### Consequences

- Good, because a price and its currency are one value that no write can
  split, and each amount has one spelling.
- Good, because the value is exact on every door.
- Bad, because a price finer than its currency's minor unit (fuel at 1.899
  EUR) does not fit; it is a `decimal` plus a currency.
- Bad, because a code outside the table (BTC, a private ledger's unit) is
  refused, and a new ISO 4217 code is a code change to both tables.
- Bad, because if ISO 4217 changes a currency's minor unit, the table keeps
  the old one, and the currency's rows need a migration before it can move.
- Bad, because a withdrawn code stays admitted for new writes too.
- Bad, because an ordering over mixed currencies interleaves them by number.

### Confirmation

`TestCoerceMoneyIsAnAmountAndACurrency` (internal/engine/validate_internal_test.go)
holds the shape; `TestFilterByMoneyComparesWithinACurrency`,
`TestOrderByMoneyComparesTheExactNumber` and
`TestRaisingAMoneyMinIsRefusedOverSmallerRows`
(internal/engine/money_db_test.go) hold the comparisons against Postgres.
`TestCurrencyTableOnlyGrows` (internal/vocabulary) pins every code and its
minor unit, fails on a removal or a change, and fails on a new code until it
is pinned. `TestConsoleCurrenciesMatchTheEngine` holds the console's table to
the engine's.

## More Information

Revisit if a deployment needs sub-minor-unit prices or amounts past 2^53
minor units: either is a new stored member, and so a migration.
