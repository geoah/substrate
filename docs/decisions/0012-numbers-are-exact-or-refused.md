---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis (via the issue-108 agent session)
---

# 0012. Numbers are exact or refused

## Context and Problem Statement

Every JSON door in and out of storage rides float64: REST bodies decode
without `UseNumber` and the jsonb read-back does too. That gave the substrate
three number gaps, each cheapest to close before rows accumulate
([issue #108](https://github.com/geoah/substrate/issues/108)): money stored as
binary float (commerce's `totalAmount`/`unitPrice`), an `int` that silently
corrupts past 2^53, and a `duration` that read Go syntax only while the
calendar and health vocabularies want days and weeks.

## Considered Options

- Decimal: a new string-carried datatype; or `UseNumber` end to end so JSON
  numbers stay exact; or document "use a string" as a convention
- Int: enforce the 2^53 - 1 bound as the contract; or `json.Number` end to end
- Duration: Go's grammar only; ISO 8601 only; or both accepted with one stored
  form

## Decision Outcome

Chosen: exactness rides in strings, and anything that would round is refused
at the door.

`decimal` is a built-in datatype whose value is a string of exact digits
(`"19.99"`), canonicalized but never rescaled, refused as a bare JSON number.
Filters and ordering cast it `::numeric`, and the bound of a range filter
travels as its own digits. `UseNumber` end to end was rejected because the
float64 doors are legion (every handler, the jsonb read-back, the SDKs) and
one missed door corrupts silently; a string cannot be rounded by any of them.

`int` keeps its float64 ride and the bound becomes the ENFORCED contract: a
safe integer, |value| <= 2^53 - 1, refused beyond on every input shape
(a `json.Number` is read by its own spelling so an oversized one refuses
rather than rounds). 2^53 itself is out: 2^53+1 arrives AS 2^53, so a
boundary value cannot prove it was not corrupted. A bigger count is a
`decimal`.

`duration` is ISO 8601, the ONE grammar in and out (per review on #161: Go's
own `47m12s` syntax is refused, not accepted beside it, because two grammars
for one word is how "duration" stops meaning anything). Accepted: weeks, days
and a time part (`PT47M12S`, `P2DT3H`, `P1W`); stored: one deterministic
decomposition into days and a time part (`PT36H` stores as `P1DT12H`, `P2W`
as `P14D`). ISO years and months are refused by name: neither has a fixed
length, and a duration here is exact time (a day is exactly 24h, a week
168h).

Commerce moved its two money properties to `decimal` in the same change
(authority version 3), and `llmprovider`'s pricing fields moved with them
(kind version 4, per the same review: break it now rather than after rows
accumulate). A repository already holding float rows keeps the old
declarations, loudly, until the rows are migrated: the boot upgrade refuses
the retype rather than the repository.

### Consequences

- Good, because money and big counts can never be silently rounded, and the
  refusal messages say what to write instead.
- Good, because a stored duration reads as one grammar and one spelling
  whatever was authored.
- Bad, because Go-syntax durations already stored anywhere (dev databases,
  scripts) are refused on their next write: the retired spelling must be
  rewritten by hand.
- Bad, because a decimal is unreadable to arithmetic without parsing: GraphQL
  serves it as a String, function code must parse it, and `min`/`max` bounds
  (declared as floats) compare through big.Rat.
- Bad, because ints that used to be accepted (past 2^53) are now refused: an
  observable narrowing, taken deliberately before the freeze makes it
  impossible.
- Bad, because a day is pinned to exactly 24h: a nominal calendar day (DST,
  leap seconds) needs a different datatype if anyone ever needs one.

### Confirmation

`TestCoerceIntIsASafeInteger`, `TestCoerceDecimalIsExact` and
`TestCoerceDurationIsISO8601Only` (internal/engine/validate_internal_test.go)
hold the three contracts; `TestOrderAndFilterByADecimalPropertyCompareNumerically`
(internal/engine/order_numeric_db_test.go) holds the `::numeric` comparison
path against Postgres.

## More Information

The `llmprovider` pricing retype means a live deployment whose provider rows
hold float prices keeps the version 3 declaration until those rows are
rewritten (quote the two numbers), and its cost stamps keep working meanwhile:
the loop's price read takes the decimal string and tolerates a legacy float.
The cost stamp itself stays float math on purpose; it is an estimate, not a
ledger.
