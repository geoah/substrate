---
status: proposed
date: 2026-10-11
decision-makers: George Antoniadis
---

# 0153. A quantity is exact digits and a UCUM code, converted only within its measure

## Context and problem statement

An observed value (a weight, a heart rate, a meter reading, a lab result) is a
number and its unit, and `decimal`
([0012](0012-numbers-are-exact-or-refused.md)) holds only the digits. Every
stored reading is read through a unit table for as long as it lives, so the
stored shape, the table's growth rule and what converts to what are hard to
change after the first row. A UCUM (Unified Code for Units of Measure)
library alone gives wrong answers for people: the NLM reference library
converts `N.m` to `J`, `Gy` to `Sv`, `Hz` to `Bq` and `{beats}/min` to
`{steps}/min`, each a correct UCUM answer
([the units report](../plans/observations/02-units-and-catalogues.md),
[the plan, section 6](../plans/observations.md)).

## Considered options

- A `quantity` datatype read through an engine-owned table of named measures,
  each with a dimension vector
- The same datatype read through a table keyed by dimension vector alone, as
  a UCUM evaluator does
- No datatype: a `decimal` plus a `string` unit, paired by convention
- User-declared units beside the shipped table

## Decision outcome

Chosen: a `quantity` datatype over a closed table of named measures.

A `quantity` is `{value, unit, comparator?}`. `value` is exact digits as a
string in the `decimal` grammar. `unit` is a case-sensitive UCUM code.
`comparator` is one of `<`, `<=`, `>=` and `>`, admitted only where the
property declares `comparator: true`, and says the true value lies in the
half-line it names (a lab's `<0.5 ng/mL`).

The engine owns the unit table, as it owns the currency table
([0140](0140-money-is-an-integer-amount-of-its-currencys-minor-unit.md)). A
**measure** is a named quantity such as `mass`, `energy`, `torque`,
`temperature` or `temperature-difference`: a dimension vector, a base unit, a
scale, its codes, and an exact rational factor from each code to the base
(plus an offset for `Cel` and `[degF]`). The rules:

- Conversion and comparison happen only within one measure. `kg` to
  `[lb_av]` converts exactly; `N.m` to `J` is refused although the two share
  a vector. A code the table lists belongs to its row's measure. A compound
  the UCUM grammar builds from the table's atoms and prefixes
  (`kW.h/(100.km)`) takes its measure from its vector, and is refused when
  that vector matches more than one measure, unless the property's
  declaration names the measure.
- Annotations are part of a unit's identity, although UCUM discards them:
  `{beats}/min` and `{steps}/min` never convert into each other, and a lone
  annotation is a count.
- An alias table on write maps the spellings writers send (`°C`, `µg` under
  both micro signs, `mmHg`, `bpm`, `kWh`, `mcg`) to codes. The stored value
  holds the code only.
- Arbitrary units convert to nothing, with one rule UCUM does not have,
  labelled in the table and the docs as the substrate's own: the same
  arbitrary atom with the same annotation may be rescaled by a metric prefix
  (`m[IU]/L` to `[IU]/L`), because labs send one analyte both ways. `[IU]`
  never reaches `[arb'U]` or a proper unit.
- Logarithmic units (`dB`, `dB[SPL]`, `[pH]`) are stored and compared in
  their own unit, convert to nothing, and refuse `sum`.
- `dBm` has no UCUM spelling and every radio API reports it, so it is a local
  code with a documented definition: `dB[W]` plus 30, exactly.
- The table grows by release only. A code is added, never removed and never
  given a new factor, because stored values are read through it. There are
  no user-declared units: a private count is an annotation (`{cups}`), and a
  private conversion (a cup of rice is 185 g) is a derivation a function
  writes.

A property or a property type names its measure with `measures:`
(`weight: {type: quantity, measures: mass}`) and may narrow the codes it
admits. `dimension:` was refused because UCUM and SI use "dimension" for the
exponent vector, and the table exists because one vector (energy and torque)
is two measures. `kind:` was refused because a kind is what a record is.
"Measure" is the word for a named quantity in the docs and the code.

A vector-only table is a calculator: every store that serves people names
the quantity (Home Assistant's converter classes, Health Connect's unit
classes, QUDT). A convention pair loses the unit wherever a reader forgets
to pair it. User-declared units would make the table per-repository, and a
removed unit would rescale history.

### Consequences

- Good, because a reading and its unit are one value, exact on every door,
  and a conversion is exact rational arithmetic.
- Good, because the conversions UCUM allows and a person would call wrong are
  refused by construction.
- Bad, because a unit outside the table is refused until a release adds it; a
  lab result in such a unit keeps `sourceUnit` and its text, and leaves
  `value` empty.
- Bad, because the prefix rescale and `dBm` are rules no other UCUM
  implementation follows, so an export reader has to be told about them.
- Bad, because `measures:` is a new declaration key
  ([0020](0020-dialect-keys-are-reserved-not-tolerated.md)), and a closure
  that uses it does not load on an older binary.
- Bad, because the engine's table, the console's copy and the host SDK's
  `host.units.*` must agree.

### Confirmation

Tests the work package 1 implementers will write: `TestUnitTableOnlyGrows`
(internal/vocabulary) pins every code, its measure and its factor, and fails
on a removal or a change; `TestQuantityConvertsOnlyWithinAMeasure` refuses
`N.m` to `J`, `Gy` to `Sv` and `{beats}/min` to `{steps}/min`;
`TestArbitraryPrefixRescaleStaysOnItsAtom` and
`TestLogarithmicUnitsConvertToNothing` hold the two special rules;
`TestUCUMConformance` runs UCUM's 529 validation cases and 30 linear
conversions; `TestConsoleUnitsMatchTheEngine` holds the console's copy.

## More information

[The plan, section 6](../plans/observations.md) and
[the units report](../plans/observations/02-units-and-catalogues.md) hold the
research, and [`coretable.py`](../plans/observations/coretable.py)
regenerates the table with exact fractions.
[The implementation brief](../plans/observations/implementation/README.md)
settled `measures:`, the prefix rule and `dBm`. The proposal
([under the hood](../plans/observations/proposal/04-under-the-hood.md)) also
let a metric record name the measure of an ambiguous compound; this record
leaves that out, because the engine reads nothing from a metric
([0156](0156-the-forms-are-gauge-amount-counter-and-summary-and-live-on-the-value.md)).
Revisit if users need units faster than releases can add them.
