# Units and conversions

A `quantity` value stores exact digits and a unit id: `{value: "72.4", unit:
kg}`. The units, and the conversions between them, are records of two core
kinds, `substrate.reamde.dev/core/unit` and
`substrate.reamde.dev/core/conversion`. Every repository holds its own: it is
seeded with [the shipped units](#the-shipped-units) at registration, and it
adds, edits and deletes units and conversions like any other records. The
server converts a value between two units by chaining the conversions that
join them, so a repository that needs a unit nobody shipped (a lab's assay
unit, a cup of rice) writes one record and stores values in it the same day
([decision 0186](decisions/0186-units-and-conversions-are-core-records-and-a-value-converts-through-the-repositorys-graph.md)).

[The data model](data-model.md#property-types) says how a property declares a
quantity, and [observations](observations.md) how a metric names its units.

## How a value names its unit

A stored quantity holds the unit record's id. A write may send the id, the
unit's `code` or one of its `aliases`; the server stores the id. It tries
them in that order, first as written and then normalized: both micro signs
(U+00B5 and U+03BC) read as `u`, and `²` and `³` as `2` and `3`, so `μg/m³`
is stored as `ug/m3`. After a bare `10`, a superscript is a power: `10³/μL`
is stored as `1e3/uL`. A spelling no unit answers is refused, naming the
spelling.

The ids, codes, aliases and their normalized forms are unique across the
repository's live units. A transaction that leaves two units answering one
spelling is refused.

## Unit records

| Property | Type | Meaning |
| --- | --- | --- |
| `name` | string, required | what the unit is called, such as `kilogram` |
| `symbol` | string | what a title prints after a value (`°C` for `Cel`); the id where absent, nothing where empty (the unit `1`) |
| `code` | string | the [UCUM](https://ucum.org) spelling, for interchange (`[lb_av]` for `lb`); a writer may send it |
| `aliases` | strings | other spellings a writer may send (`°C`, `celsius`) |
| `scale` | `level` or `powerlevel` | absent for a unit whose values add and average; `level` for a logarithmic unit whose values do not add (`dB`, `pH`); `powerlevel` for the level of a power, whose values neither add nor average (`dBm`) |
| `turn` | string | one full circle in the unit (`360` for `deg`), exact digits or a fraction; a `circular` value needs a unit with one |

The record id is what a value stores, so it fits the record id alphabet
(`kg`, `1/min`, `mg/dL`, `percent`). A title keeps the symbol it was written
with: editing a symbol changes the titles of later writes, never stored ones.
Search indexes a value's digits and its unit id, never the symbol.

## Conversion records

A conversion joins two units, `from` and `to`, each a reference to a unit
record, in one of two forms.

**Linear**: `factor` and an optional `offset`, each an exact rational
written as a string, as digits (`"0.45359237"`) or a fraction (`"5/9"`,
`"1/0.3048"`). It means `to = from x factor + offset`. The factor is above
zero. A number in place of the string is refused, because a float has
already lost digits. A linear conversion converts both ways, exactly.

**Expression**: `expr` and `inverse`, each a [CEL](https://cel.dev)
expression over the double `value` that returns a double: standard CEL, with
no extensions. `expr` converts from `from` to `to`, and `inverse` back.
Nothing checks that the inverse undoes the expression; its author owns that.
An expression's answer is never exact.

A conversion from a unit to itself is refused, and so is one whose `from` or
`to` names no unit: a transaction that deletes a unit deletes its
conversions with it.

## How the server converts

The linear conversions divide the units into **linear components**: two
units are in one component when a chain of linear conversions joins them.
Each component has a **root**, its smallest unit id in byte order, and every
unit holds an exact map to its root, `root = value x factor + offset`. Two
units of one component convert by composing two maps, so `lb` converts to
`st` though no shipped conversion names both.

A cycle of linear conversions has to compose to the identity. A conversion
that closes a cycle with another answer (a direct `st` to `kg` of `6.35`
beside `st` to `lb` to `kg`) is refused, naming both routes, so every route
gives one answer. All units of one component share one `scale`, so a level
never joins a unit whose values add.

Expression conversions join components and must form a forest over them: at
most one route of expressions between any two components. A value crosses at
most four expressions on its way.

What converts where:

- Inside one linear component, a value converts exactly, in rational
  arithmetic: 155 `lb` is 70.30681735 `kg` to the last digit. An answer
  with no finite decimal expansion (1 `degF` is -155/9 `Cel`) is rounded to
  20 significant digits and marked inexact.
- Across components, only REST (`GET /api/v1/units/convert`) and function
  code (`host.units.convert`) convert one value, along the expression route.
  The console's display converts inside a linear component only. Filters,
  ordering, bounds, a metric's accepted units and the aggregate read cross
  linear conversions only, because a sum, a mean or a comparison survives
  an affine map and not an arbitrary function.
- Two units no route joins do not convert, and a read that would need them
  to is refused, naming both.

**Differences.** A difference converts by the factor alone, with every
offset dropped: a deviation of 0.3 `Cel` is 0.54 `degF`, where a temperature
of 0.3 `Cel` is 32.54 `degF`. An observed value marks a difference with
`delta: true` on its row, and a quantity property with `delta: true` on its
declaration. A difference never crosses an expression conversion.

**Circular units.** A component holding a unit with a `turn` holds only
offset-free conversions, and its turns agree: the `turn` of `to` is the
`factor` times the `turn` of `from`. Every unit of such a component reads its
turn from it, so `arcmin` turns at 21600. `rad` keeps no conversion to
`deg`, because no rational number states pi; it declares its own turn, 2 pi
to 40 places.

## Add a unit

A unit is a record like any other, applied with `bin/substratectl apply -f`
or the console. Its kind is core's, whatever the repository's authority:

```yaml
kind: substrate.reamde.dev/core/unit
metadata:
  id: cup_rice
data:
  properties:
    name: cup of rice
    symbol: cup
```

Values may be written in `cup_rice` at once. Until a conversion joins it to
another unit, it converts to nothing but itself. A unit with a UCUM spelling
names it as `code`, and the spellings writers send as `aliases`:

```yaml
kind: substrate.reamde.dev/core/unit
metadata:
  id: lb
data:
  properties:
    name: pound
    symbol: lb
    code: "[lb_av]"
    aliases:
      - lbs
      - pound
      - pounds
```

`lb` ships in the seed, without those three aliases. Writing the record as
above adds them, and the seed leaves the edited record alone from then on
([seeding](#seeding)).

## Add a conversion

A linear conversion, with and without an offset:

```yaml
kind: substrate.reamde.dev/core/conversion
metadata:
  id: st-lb
data:
  properties:
    from:
      ref: substrate.reamde.dev/core/unit/st
    to:
      ref: substrate.reamde.dev/core/unit/lb
    factor: "14"
---
kind: substrate.reamde.dev/core/conversion
metadata:
  id: degF-Cel
data:
  properties:
    from:
      ref: substrate.reamde.dev/core/unit/degF
    to:
      ref: substrate.reamde.dev/core/unit/Cel
    factor: "5/9"
    offset: "-160/9"
```

The shipped conversions take the id `<from>-<to>`; any record id will do.
An expression conversion:

```yaml
kind: substrate.reamde.dev/core/conversion
metadata:
  id: cup_rice-g
data:
  properties:
    from:
      ref: substrate.reamde.dev/core/unit/cup_rice
    to:
      ref: substrate.reamde.dev/core/unit/g
    expr: "value * 185.0"
    inverse: "value / 185.0"
```

This one could be a factor of `"185"`. As an expression it lets a person
see a cup of rice in grams, while filters, sums and a metric's accepted
units keep cups and grams apart, since a cup of rice weighs what its rice
weighs.

## Add a metric

A metric, a record of a kind binding core's `metric` trait (the
`observations` sample's `metric`, under the repository's own authority after
the import), names its default `unit` and the `units` a value may be written
in, each a reference to a unit record:

```yaml
kind: ada.example.com/observations/metric
metadata:
  id: health/body_mass
data:
  properties:
    name: Body weight
    unit:
      ref: substrate.reamde.dev/core/unit/kg
    units:
      - ref: substrate.reamde.dev/core/unit/kg
      - ref: substrate.reamde.dev/core/unit/lb
      - ref: substrate.reamde.dev/core/unit/st
    form: gauge
    hold: PT0S
    rollups:
      - 15m
      - 1h
      - 1d
```

Without `units`, the metric admits every unit in `unit`'s linear component.
With them, it admits `unit` and the listed units. Each listed unit converts to
`unit` through linear conversions. A value whose unit the metric does not
admit is refused, and so is a metric edit, restore or deletion that leaves a
stored value of it, tombstones included, with a unit the metric no longer
admits.

Each weigh-in is stored as written, `72.4 kg`, `159.2 lb` or `11.4 st`.
A filter converts its operand into each unit the rows use, so this one
matches every weigh-in under 159.61 `lb`, whatever unit it was written in:

```json
{"kinds": ["ada.example.com/observations/observation"],
 "properties": {
   "metric": {"eq": "ada.example.com/observations/metric/health/body_mass"},
   "value": {"lt": {"value": "159.61", "unit": "lb"}}}}
```

An aggregate read answers in the metric's `unit` unless it names another
`unit` of the same component, here `"unit": "lb"`
([the API](api.md#the-aggregate-read)).

A quantity property on any other kind declares the same two keys, as unit
ids or any spelling of them: `unit`, whose linear component every value's
unit belongs to, and `units`, the list that narrows it
([the data model](data-model.md#property-types)).

## Edits and deletions

A stored quantity keeps its digits and its unit id. No write of a unit or a
conversion rewrites a stored value, and nothing stores a converted number,
so a corrected factor corrects every later answer at once.

A unit is **used** when a stored value names it: a quantity anywhere in a
record (nested fields, lists and link properties included), a trace's `unit`,
a metric's `unit` or `units`, or a declaration's `unit`, `units`, bounds or
default. Tombstoned records count, because a restore brings their values
back. At the end of each transaction the server refuses, with `409
conflict`:

- deleting a used unit;
- leaving two units that were in one linear component apart while both are
  used, or one is used and the other is the `unit` a declaration or a metric
  anchors to. That covers deleting a conversion, deleting an unused unit
  that joined two used ones, retargeting a conversion and replacing a linear
  form with an expression.

A graph problem (two units answering one spelling, an inconsistent cycle, a
broken circular rule, a limit) is `422 validation`, naming the record.
Changing a factor passes.

## Seeding

Registration writes [the shipped units](#the-shipped-units) in the creation
transaction, through the changelog like every other write. Every open of the
repository runs the same seed again, before anything that reads a quantity:

- a shipped record the repository lacks is written;
- a shipped record only the seed has written is refreshed to the shipped
  version;
- a record any other hand has written or deleted (a person at any door, an
  agent, a function, a bundle) is left as it was left, a collected tombstone
  included, so an edited unit stops receiving shipped corrections;
- a shipped record that would contradict the repository's own graph (a
  spelling another unit answers, a cycle with another answer, a split of used
  units) is skipped and logged. The open never fails for one.

`GET /api/v1/units` lists the skipped records as `unseeded`, each with its
reason, until a later open seeds them.

## Limits

Each limit is refused by name:

| Limit | Value |
| --- | --- |
| units per repository | 2,000 |
| conversions per repository | 4,000 |
| aliases per unit | 32 |
| bytes in a unit id, code or alias | 128 |
| bytes in an expression or its inverse | 1,024 |
| digits in each part of a factor, offset or turn | 64 |
| expression conversions on one route | 4 |
| bits in the numerator or denominator of a unit's map to its root | 4,096 |
| digits in a converted value | 1,000 |

## Reading the graph

- `GET /api/v1/units` answers the repository's units, each with its root and
  exact map to it, its conversions, the graph `revision` and `unseeded`.
  `GET /api/v1/units/convert` converts one value ([the API](api.md#units-and-conversions)).
- Function code converts with `host.units.convert`, resolves a spelling with
  `host.units.canonical` and asks `host.units.converts(a, b)` whether two
  units convert exactly ([functions](functions.md#the-sdk)).
- The console reads `GET /api/v1/units` and caches it by revision, and
  fetches it again when a unit or conversion record changes.

Every write of a unit or conversion record moves the revision. A read uses
the graph of the revision its snapshot holds, and a list cursor whose filter
or order converted a quantity is refused after the revision moves.

## The shipped units

The seed is `internal/engine/unitseed/units.yaml` and `conversions.yaml`,
edited by hand. Most ids are UCUM codes; a code that does not fit the record
id alphabet takes a short id and stays the unit's `code`, so every UCUM
spelling still resolves (`[lb_av]` is `lb`, `%` is `percent`, `/min` is
`1/min`, `{count}` is `count`). Unit codes are from UCUM 2.2, copyright
Regenstrief Institute, Inc., used under the UCUM license. `dBm` is the
substrate's own code; UCUM has no spelling for it.

The seed keeps apart what a person needs apart: energy (`J`) and torque
(`N.m`), absorbed dose (`Gy`) and dose equivalent (`Sv`), a count and a
percent, NGSP HbA1c (`percent`) and IFCC HbA1c (`mmol/mol`), `mg/dL` and
`mmol/L` (which takes a molar mass), and a vendor's `score` and anything
else. An assay unit converts only to the same assay under another prefix
(`mIU/L` to `IU/L`). A level converts to nothing but itself.

<!-- BEGIN shipped units: written by TestUnitsPage in internal/engine/unitseed from units.yaml and conversions.yaml; rewrite with go test ./internal/engine/unitseed/ -run TestUnitsPage -update, never by hand. -->

216 units and 149 conversions, in 62 groups. A group is a heading in the seed
files and nothing more: two units convert when conversions join them, whatever
group they sit in. In each unit's row, `root = value x factor + offset` is its
exact map to the root of its linear component.

### mass

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `kg` | kilogram | kg | `kg` |  | `g` | 1000 |  |  |
| `g` | gram | g | `g` |  | `g` | 1 |  |  |
| `mg` | milligram | mg | `mg` |  | `g` | 0.001 |  |  |
| `ug` | microgram | μg | `ug` | `mcg` | `g` | 0.000001 |  |  |
| `ng` | nanogram | ng | `ng` |  | `g` | 0.000000001 |  |  |
| `t` | tonne | t | `t` |  | `g` | 1000000 |  |  |
| `lb` | pound | lb | `[lb_av]` |  | `g` | 453.59237 |  |  |
| `oz` | ounce | oz | `[oz_av]` |  | `g` | 28.349523125 |  |  |
| `st` | stone | st | `[stone_av]` |  | `g` | 6350.29318 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `g-kg` | `g` | `kg` | 0.001 |  |
| `mg-kg` | `mg` | `kg` | 0.000001 |  |
| `ug-kg` | `ug` | `kg` | 0.000000001 |  |
| `ng-kg` | `ng` | `kg` | 0.000000000001 |  |
| `t-kg` | `t` | `kg` | 1000 |  |
| `lb-kg` | `lb` | `kg` | 0.45359237 |  |
| `oz-kg` | `oz` | `kg` | 0.028349523125 |  |
| `st-kg` | `st` | `kg` | 6.35029318 |  |

### length

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `m` | meter | m | `m` |  | `cm` | 100 |  |  |
| `km` | kilometer | km | `km` |  | `cm` | 100000 |  |  |
| `cm` | centimeter | cm | `cm` |  | `cm` | 1 |  |  |
| `mm` | millimeter | mm | `mm` |  | `cm` | 0.1 |  |  |
| `nm` | nanometer | nm | `nm` |  | `cm` | 0.0000001 |  |  |
| `in` | inch | in | `[in_i]` |  | `cm` | 2.54 |  |  |
| `ft` | foot | ft | `[ft_i]` |  | `cm` | 30.48 |  |  |
| `yd` | yard | yd | `[yd_i]` |  | `cm` | 91.44 |  |  |
| `mi` | mile | mi | `[mi_i]` |  | `cm` | 160934.4 |  |  |
| `nmi` | nautical mile | nmi | `[nmi_i]` |  | `cm` | 185200 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `km-m` | `km` | `m` | 1000 |  |
| `cm-m` | `cm` | `m` | 0.01 |  |
| `mm-m` | `mm` | `m` | 0.001 |  |
| `nm-m` | `nm` | `m` | 0.000000001 |  |
| `in-m` | `in` | `m` | 0.0254 |  |
| `ft-m` | `ft` | `m` | 0.3048 |  |
| `yd-m` | `yd` | `m` | 0.9144 |  |
| `mi-m` | `mi` | `m` | 1609.344 |  |
| `nmi-m` | `nmi` | `m` | 1852 |  |

### area

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `m2` | square meter | m² | `m2` |  | `cm2` | 10000 |  |  |
| `cm2` | square centimeter | cm² | `cm2` |  | `cm2` | 1 |  |  |
| `har` | hectare | ha | `har` |  | `cm2` | 100000000 |  |  |
| `ft2` | square foot | ft² | `[sft_i]` |  | `cm2` | 929.0304 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `cm2-m2` | `cm2` | `m2` | 0.0001 |  |
| `har-m2` | `har` | `m2` | 10000 |  |
| `ft2-m2` | `ft2` | `m2` | 0.09290304 |  |

### volume

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `m3` | cubic meter | m³ | `m3` |  | `L` | 1000 |  |  |
| `L` | liter | L | `L` |  | `L` | 1 |  |  |
| `dL` | deciliter | dL | `dL` |  | `L` | 0.1 |  |  |
| `mL` | milliliter | mL | `mL` |  | `L` | 0.001 |  |  |
| `gal` | US gallon | gal | `[gal_us]` |  | `L` | 3.785411784 |  |  |
| `qt` | US quart | qt | `[qt_us]` |  | `L` | 0.946352946 |  |  |
| `pt` | US pint | pt | `[pt_us]` |  | `L` | 0.473176473 |  |  |
| `cup` | US cup | cup | `[cup_us]` |  | `L` | 0.2365882365 |  |  |
| `floz` | US fluid ounce | fl oz | `[foz_us]` |  | `L` | 0.0295735295625 |  |  |
| `tbsp` | US tablespoon | tbsp | `[tbs_us]` |  | `L` | 0.01478676478125 |  |  |
| `tsp` | US teaspoon | tsp | `[tsp_us]` |  | `L` | 0.00492892159375 |  |  |
| `gal_br` | imperial gallon | gal (UK) | `[gal_br]` |  | `L` | 4.54609 |  |  |
| `floz_br` | imperial fluid ounce | fl oz (UK) | `[foz_br]` |  | `L` | 0.0284130625 |  |  |
| `cup_m` | metric cup | cup (metric) | `[cup_m]` |  | `L` | 0.24 |  |  |
| `tbsp_m` | metric tablespoon | tbsp (metric) | `[tbs_m]` |  | `L` | 0.015 |  |  |
| `tsp_m` | metric teaspoon | tsp (metric) | `[tsp_m]` |  | `L` | 0.005 |  |  |
| `ft3` | cubic foot | ft³ | `[cft_i]` |  | `L` | 28.316846592 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `L-m3` | `L` | `m3` | 0.001 |  |
| `dL-m3` | `dL` | `m3` | 0.0001 |  |
| `mL-m3` | `mL` | `m3` | 0.000001 |  |
| `gal-m3` | `gal` | `m3` | 0.003785411784 |  |
| `qt-m3` | `qt` | `m3` | 0.000946352946 |  |
| `pt-m3` | `pt` | `m3` | 0.000473176473 |  |
| `cup-m3` | `cup` | `m3` | 0.0002365882365 |  |
| `floz-m3` | `floz` | `m3` | 0.0000295735295625 |  |
| `tbsp-m3` | `tbsp` | `m3` | 0.00001478676478125 |  |
| `tsp-m3` | `tsp` | `m3` | 0.00000492892159375 |  |
| `gal_br-m3` | `gal_br` | `m3` | 0.00454609 |  |
| `floz_br-m3` | `floz_br` | `m3` | 0.0000284130625 |  |
| `cup_m-m3` | `cup_m` | `m3` | 0.00024 |  |
| `tbsp_m-m3` | `tbsp_m` | `m3` | 0.000015 |  |
| `tsp_m-m3` | `tsp_m` | `m3` | 0.000005 |  |
| `ft3-m3` | `ft3` | `m3` | 0.028316846592 |  |

### duration

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `s` | second | s | `s` |  | `a` | 1/31557600 |  |  |
| `ms` | millisecond | ms | `ms` |  | `a` | 1/31557600000 |  |  |
| `us` | microsecond | μs | `us` |  | `a` | 1/31557600000000 |  |  |
| `min` | minute | min | `min` |  | `a` | 1/525960 |  |  |
| `h` | hour | h | `h` | `hr` | `a` | 1/8766 |  |  |
| `d` | day | d | `d` |  | `a` | 4/1461 |  |  |
| `wk` | week | wk | `wk` |  | `a` | 28/1461 |  |  |
| `mo` | mean Julian month | mo | `mo` |  | `a` | 1/12 |  |  |
| `a` | mean Julian year | a | `a` |  | `a` | 1 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `ms-s` | `ms` | `s` | 0.001 |  |
| `us-s` | `us` | `s` | 0.000001 |  |
| `min-s` | `min` | `s` | 60 |  |
| `h-s` | `h` | `s` | 3600 |  |
| `d-s` | `d` | `s` | 86400 |  |
| `wk-s` | `wk` | `s` | 604800 |  |
| `mo-s` | `mo` | `s` | 2629800 |  |
| `a-s` | `a` | `s` | 31557600 |  |

### temperature

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `K` | kelvin | K | `K` |  | `Cel` | 1 | -273.15 |  |
| `Cel` | degree Celsius | °C | `Cel` | `degC`, `°C`, `℃` | `Cel` | 1 |  |  |
| `degF` | degree Fahrenheit | °F | `[degF]` | `°F`, `℉` | `Cel` | 5/9 | -160/9 |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `Cel-K` | `Cel` | `K` | 1 | 273.15 |
| `degF-K` | `degF` | `K` | 5/9 | 45967/180 |

### energy

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `J` | joule | J | `J` |  | `Btu` | 50000000/52752792631 |  |  |
| `kJ` | kilojoule | kJ | `kJ` |  | `Btu` | 50000000000/52752792631 |  |  |
| `MJ` | megajoule | MJ | `MJ` |  | `Btu` | 50000000000000/52752792631 |  |  |
| `cal` | thermochemical calorie | cal | `cal` |  | `Btu` | 209200000/52752792631 |  |  |
| `kcal` | kilocalorie | kcal | `kcal` |  | `Btu` | 209200000000/52752792631 |  |  |
| `Cal` | nutrition Calorie | Cal | `[Cal]` |  | `Btu` | 209200000000/52752792631 |  |  |
| `W.h` | watt-hour | Wh | `W.h` | `Wh` | `Btu` | 180000000000/52752792631 |  |  |
| `kW.h` | kilowatt-hour | kWh | `kW.h` | `kWh` | `Btu` | 180000000000000/52752792631 |  |  |
| `eV` | electronvolt | eV | `eV` |  | `Btu` | 801088317/5275279263100000000000000000000 |  |  |
| `Btu` | British thermal unit | Btu | `[Btu_IT]` |  | `Btu` | 1 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `kJ-J` | `kJ` | `J` | 1000 |  |
| `MJ-J` | `MJ` | `J` | 1000000 |  |
| `cal-J` | `cal` | `J` | 4.184 |  |
| `kcal-J` | `kcal` | `J` | 4184 |  |
| `Cal-J` | `Cal` | `J` | 4184 |  |
| `W.h-J` | `W.h` | `J` | 3600 |  |
| `kW.h-J` | `kW.h` | `J` | 3600000 |  |
| `eV-J` | `eV` | `J` | 0.0000000000000000001602176634 |  |
| `Btu-J` | `Btu` | `J` | 1055.05585262 |  |

### power

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `W` | watt | W | `W` |  | `Btu/h` | 180000000000/52752792631 |  |  |
| `mW` | milliwatt | mW | `mW` |  | `Btu/h` | 180000000/52752792631 |  |  |
| `kW` | kilowatt | kW | `kW` |  | `Btu/h` | 180000000000000/52752792631 |  |  |
| `MW` | megawatt | MW | `MW` |  | `Btu/h` | 180000000000000000/52752792631 |  |  |
| `hp` | horsepower | hp | `[HP]` |  | `Btu/h` | 7397940627/2907500 |  |  |
| `kcal/d` | kilocalorie per day | kcal/d | `kcal/d` |  | `Btu/h` | 26150000000/158258377893 |  |  |
| `kcal/h` | kilocalorie per hour | kcal/h | `kcal/h` |  | `Btu/h` | 209200000000/52752792631 |  |  |
| `Btu/h` | British thermal unit per hour | Btu/h | `[Btu_IT]/h` |  | `Btu/h` | 1 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `mW-W` | `mW` | `W` | 0.001 |  |
| `kW-W` | `kW` | `W` | 1000 |  |
| `MW-W` | `MW` | `W` | 1000000 |  |
| `hp-W` | `hp` | `W` | 745.69987158227022 |  |
| `kcal/d-W` | `kcal/d` | `W` | 523/10800 |  |
| `kcal/h-W` | `kcal/h` | `W` | 523/450 |  |
| `Btu/h-W` | `Btu/h` | `W` | 52752792631/180000000000 |  |

### apparent-power

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `V.A` | volt-ampere | VA | `V.A` | `VA` | `V.A` | 1 |  |  |

### reactive-power

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `var` | volt-ampere reactive | var | `V.A{reactive}` |  | `var` | 1 |  |  |

### pressure

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `Pa` | pascal | Pa | `Pa` |  | `Pa` | 1 |  |  |
| `hPa` | hectopascal | hPa | `hPa` |  | `Pa` | 100 |  |  |
| `kPa` | kilopascal | kPa | `kPa` |  | `Pa` | 1000 |  |  |
| `bar` | bar | bar | `bar` |  | `Pa` | 100000 |  |  |
| `mbar` | millibar | mbar | `mbar` |  | `Pa` | 100 |  |  |
| `atm` | standard atmosphere | atm | `atm` |  | `Pa` | 101325 |  |  |
| `mmHg` | millimeter of mercury | mmHg | `mm[Hg]` |  | `Pa` | 133.322 |  |  |
| `cmH2O` | centimeter of water | cmH₂O | `cm[H2O]` | `cmAq` | `Pa` | 98.0665 |  |  |
| `psi` | pound per square inch | psi | `[psi]` |  | `Pa` | 8896443230521/1290320000 |  |  |
| `inHg` | inch of mercury | inHg | `[in_i'Hg]` |  | `Pa` | 3386.3788 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `hPa-Pa` | `hPa` | `Pa` | 100 |  |
| `kPa-Pa` | `kPa` | `Pa` | 1000 |  |
| `bar-Pa` | `bar` | `Pa` | 100000 |  |
| `mbar-Pa` | `mbar` | `Pa` | 100 |  |
| `atm-Pa` | `atm` | `Pa` | 101325 |  |
| `mmHg-Pa` | `mmHg` | `Pa` | 133.322 |  |
| `cmH2O-Pa` | `cmH2O` | `Pa` | 98.0665 |  |
| `psi-Pa` | `psi` | `Pa` | 8896443230521/1290320000 |  |
| `inHg-Pa` | `inHg` | `Pa` | 3386.3788 |  |

### speed

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `m/s` | meter per second | m/s | `m/s` |  | `ft/s` | 1250/381 |  |  |
| `km/h` | kilometer per hour | km/h | `km/h` | `km/hr`, `kmph` | `ft/s` | 3125/3429 |  |  |
| `mph` | mile per hour | mph | `[mi_i]/h` | `mi/hr` | `ft/s` | 22/15 |  |  |
| `kn` | knot | kn | `[kn_i]` |  | `ft/s` | 11575/6858 |  |  |
| `ft/s` | foot per second | ft/s | `[ft_i]/s` |  | `ft/s` | 1 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `km/h-m/s` | `km/h` | `m/s` | 5/18 |  |
| `mph-m/s` | `mph` | `m/s` | 0.44704 |  |
| `kn-m/s` | `kn` | `m/s` | 463/900 |  |
| `ft/s-m/s` | `ft/s` | `m/s` | 0.3048 |  |

### pace

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `s/m` | second per meter | s/m | `s/m` |  | `min/km` | 50/3 |  |  |
| `min/km` | minute per kilometer | min/km | `min/km` |  | `min/km` | 1 |  |  |
| `s/km` | second per kilometer | s/km | `s/km` |  | `min/km` | 1/60 |  |  |
| `min/mi` | minute per mile | min/mi | `min/[mi_i]` |  | `min/km` | 15625/25146 |  |  |
| `s/100m` | second per 100 meters | s/100 m | `s/(100.m)` |  | `min/km` | 1/6 |  |  |
| `s/100yd` | second per 100 yards | s/100 yd | `s/(100.[yd_i])` |  | `min/km` | 625/3429 |  |  |
| `s/500m` | second per 500 meters | s/500 m | `s/(500.m)` |  | `min/km` | 1/30 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `min/km-s/m` | `min/km` | `s/m` | 0.06 |  |
| `s/km-s/m` | `s/km` | `s/m` | 0.001 |  |
| `min/mi-s/m` | `min/mi` | `s/m` | 625/16764 |  |
| `s/100m-s/m` | `s/100m` | `s/m` | 0.01 |  |
| `s/100yd-s/m` | `s/100yd` | `s/m` | 25/2286 |  |
| `s/500m-s/m` | `s/500m` | `s/m` | 0.002 |  |

### acceleration

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `m/s2` | meter per second squared | m/s² | `m/s2` |  | `gn` | 20000/196133 |  |  |
| `gn` | standard gravity | gn | `[g]` |  | `gn` | 1 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `gn-m/s2` | `gn` | `m/s2` | 9.80665 |  |

### frequency

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `Hz` | hertz | Hz | `Hz` |  | `Hz` | 1 |  |  |
| `kHz` | kilohertz | kHz | `kHz` |  | `Hz` | 1000 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `kHz-Hz` | `kHz` | `Hz` | 1000 |  |

### rate

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `1/s` | per second | /s | `/s` |  | `1/d` | 86400 |  |  |
| `1/min` | per minute | /min | `/min` | `bpm`, `count/min`, `rpm` | `1/d` | 1440 |  |  |
| `1/h` | per hour | /h | `/h` |  | `1/d` | 24 |  |  |
| `1/d` | per day | /d | `/d` |  | `1/d` | 1 |  |  |
| `1/wk` | per week | /wk | `/wk` |  | `1/d` | 1/7 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `1/min-1/s` | `1/min` | `1/s` | 1/60 |  |
| `1/h-1/s` | `1/h` | `1/s` | 1/3600 |  |
| `1/d-1/s` | `1/d` | `1/s` | 1/86400 |  |
| `1/wk-1/s` | `1/wk` | `1/s` | 1/604800 |  |

### count

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `count` | count | {count} | `{count}` |  | `count` | 1 |  |  |
| `1` | one |  | `1` |  | `1` | 1 |  |  |

### fraction

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `percent` | percent | % | `%` |  | `1` | 0.01 |  |  |
| `ppth` | part per thousand | ‰ | `[ppth]` |  | `1` | 0.001 |  |  |
| `ppm` | part per million | ppm | `[ppm]` |  | `1` | 0.000001 |  |  |
| `ppb` | part per billion | ppb | `[ppb]` |  | `1` | 0.000000001 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `percent-1` | `percent` | `1` | 0.01 |  |
| `ppth-1` | `ppth` | `1` | 0.001 |  |
| `ppm-1` | `ppm` | `1` | 0.000001 |  |
| `ppb-1` | `ppb` | `1` | 0.000000001 |  |

### amount-fraction

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `mmol/mol` | millimole per mole | mmol/mol | `mmol/mol` |  | `mmol/mol` | 1 |  |  |

### score

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `score` | score | score | `{score}` |  | `score` | 1 |  |  |

### amount-of-substance

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `mol` | mole | mol | `mol` |  | `mmol` | 1000 |  |  |
| `mmol` | millimole | mmol | `mmol` |  | `mmol` | 1 |  |  |
| `umol` | micromole | μmol | `umol` |  | `mmol` | 0.001 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `mmol-mol` | `mmol` | `mol` | 0.001 |  |
| `umol-mol` | `umol` | `mol` | 0.000001 |  |

### amount-concentration

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `mol/L` | mole per liter | mol/L | `mol/L` |  | `mmol/L` | 1000 |  |  |
| `mmol/L` | millimole per liter | mmol/L | `mmol/L` | `mmol<180.15588000005408>/L`, `mmol<180.1558800000541>/L` | `mmol/L` | 1 |  |  |
| `umol/L` | micromole per liter | μmol/L | `umol/L` |  | `mmol/L` | 0.001 |  |  |
| `nmol/L` | nanomole per liter | nmol/L | `nmol/L` |  | `mmol/L` | 0.000001 |  |  |
| `pmol/L` | picomole per liter | pmol/L | `pmol/L` |  | `mmol/L` | 0.000000001 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `mmol/L-mol/L` | `mmol/L` | `mol/L` | 0.001 |  |
| `umol/L-mol/L` | `umol/L` | `mol/L` | 0.000001 |  |
| `nmol/L-mol/L` | `nmol/L` | `mol/L` | 0.000000001 |  |
| `pmol/L-mol/L` | `pmol/L` | `mol/L` | 0.000000000001 |  |

### mass-concentration

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `g/L` | gram per liter | g/L | `g/L` |  | `g/L` | 1 |  |  |
| `g/dL` | gram per deciliter | g/dL | `g/dL` |  | `g/L` | 10 |  |  |
| `mg/dL` | milligram per deciliter | mg/dL | `mg/dL` |  | `g/L` | 0.01 |  |  |
| `mg/L` | milligram per liter | mg/L | `mg/L` |  | `g/L` | 0.001 |  |  |
| `ug/L` | microgram per liter | μg/L | `ug/L` |  | `g/L` | 0.000001 |  |  |
| `ng/mL` | nanogram per milliliter | ng/mL | `ng/mL` |  | `g/L` | 0.000001 |  |  |
| `pg/mL` | picogram per milliliter | pg/mL | `pg/mL` |  | `g/L` | 0.000000001 |  |  |
| `ug/dL` | microgram per deciliter | μg/dL | `ug/dL` |  | `g/L` | 0.00001 |  |  |
| `g/m3` | gram per cubic meter | g/m³ | `g/m3` |  | `g/L` | 0.001 |  |  |
| `mg/m3` | milligram per cubic meter | mg/m³ | `mg/m3` |  | `g/L` | 0.000001 |  |  |
| `ug/m3` | microgram per cubic meter | μg/m³ | `ug/m3` |  | `g/L` | 0.000000001 |  |  |
| `kg/m3` | kilogram per cubic meter | kg/m³ | `kg/m3` |  | `g/L` | 1 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `g/dL-g/L` | `g/dL` | `g/L` | 10 |  |
| `mg/dL-g/L` | `mg/dL` | `g/L` | 0.01 |  |
| `mg/L-g/L` | `mg/L` | `g/L` | 0.001 |  |
| `ug/L-g/L` | `ug/L` | `g/L` | 0.000001 |  |
| `ng/mL-g/L` | `ng/mL` | `g/L` | 0.000001 |  |
| `pg/mL-g/L` | `pg/mL` | `g/L` | 0.000000001 |  |
| `ug/dL-g/L` | `ug/dL` | `g/L` | 0.00001 |  |
| `g/m3-g/L` | `g/m3` | `g/L` | 0.001 |  |
| `mg/m3-g/L` | `mg/m3` | `g/L` | 0.000001 |  |
| `ug/m3-g/L` | `ug/m3` | `g/L` | 0.000000001 |  |
| `kg/m3-g/L` | `kg/m3` | `g/L` | 1 |  |

### number-concentration

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `1/L` | per liter | /L | `/L` |  | `1/L` | 1 |  |  |
| `1/uL` | per microliter | /μL | `/uL` |  | `1/L` | 1000000 |  |  |
| `1e3/uL` | thousand per microliter | 10³/μL | `10*3/uL` |  | `1/L` | 1000000000 |  |  |
| `1e6/uL` | million per microliter | 10⁶/μL | `10*6/uL` |  | `1/L` | 1000000000000 |  |  |
| `1e9/L` | billion per liter | 10⁹/L | `10*9/L` |  | `1/L` | 1000000000 |  |  |
| `1e12/L` | trillion per liter | 10¹²/L | `10*12/L` |  | `1/L` | 1000000000000 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `1/uL-1/L` | `1/uL` | `1/L` | 1000000 |  |
| `1e3/uL-1/L` | `1e3/uL` | `1/L` | 1000000000 |  |
| `1e6/uL-1/L` | `1e6/uL` | `1/L` | 1000000000000 |  |
| `1e9/L-1/L` | `1e9/L` | `1/L` | 1000000000 |  |
| `1e12/L-1/L` | `1e12/L` | `1/L` | 1000000000000 |  |

### catalytic-concentration

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `U/L` | enzyme unit per liter | U/L | `U/L` |  | `U/L` | 1 |  |  |

### catalytic-activity

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `kat` | katal | kat | `kat` |  | `U` | 60000000 |  |  |
| `U` | enzyme unit | U | `U` |  | `U` | 1 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `U-kat` | `U` | `kat` | 1/60000000 |  |

### arbitrary

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `IU` | international unit | IU | `[IU]` |  | `IU` | 1 |  |  |
| `IU/L` | international unit per liter | IU/L | `[IU]/L` |  | `IU/L` | 1 |  |  |
| `mIU/L` | milli-international unit per liter | mIU/L | `m[IU]/L` |  | `IU/L` | 0.001 |  |  |
| `IU/mL` | international unit per milliliter | IU/mL | `[IU]/mL` |  | `IU/L` | 1000 |  |  |
| `arbU` | arbitrary unit | arb. U | `[arb'U]` |  | `arbU` | 1 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `mIU/L-IU/L` | `mIU/L` | `IU/L` | 0.001 |  |
| `IU/mL-IU/L` | `IU/mL` | `IU/L` | 1000 |  |

### illuminance

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `lx` | lux | lx | `lx` |  | `lx` | 1 |  |  |

### luminous-flux

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `lm` | lumen | lm | `lm` |  | `lm` | 1 |  |  |

### luminous-intensity

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `cd` | candela | cd | `cd` |  | `cd` | 1 |  |  |

### luminance

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `cd/m2` | candela per square meter | cd/m² | `cd/m2` |  | `cd/m2` | 1 |  |  |

### irradiance

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `W/m2` | watt per square meter | W/m² | `W/m2` |  | `W/m2` | 1 |  |  |

### level

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `dB` | decibel | dB | `dB` |  | `dB` | 1 |  | scale `level` |

### sound-pressure-level

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `dBSPL` | decibel of sound pressure level | dB SPL | `dB[SPL]` | `dBASPL` | `dBSPL` | 1 |  | scale `powerlevel` |

### power-level

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `dBW` | decibel-watt | dBW | `dB[W]` |  | `dBW` | 1 |  | scale `powerlevel` |
| `dBm` | decibel-milliwatt | dBm | `dBm` |  | `dBm` | 1 |  | scale `powerlevel` |

### acidity

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `pH` | pH | pH | `[pH]` |  | `pH` | 1 |  | scale `level` |

### electric-potential

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `V` | volt | V | `V` |  | `V` | 1 |  |  |
| `mV` | millivolt | mV | `mV` |  | `V` | 0.001 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `mV-V` | `mV` | `V` | 0.001 |  |

### electric-current

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `A` | ampere | A | `A` |  | `A` | 1 |  |  |
| `mA` | milliampere | mA | `mA` |  | `A` | 0.001 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `mA-A` | `mA` | `A` | 0.001 |  |

### electric-resistance

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `Ohm` | ohm | Ω | `Ohm` |  | `Ohm` | 1 |  |  |
| `kOhm` | kiloohm | kΩ | `kOhm` |  | `Ohm` | 1000 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `kOhm-Ohm` | `kOhm` | `Ohm` | 1000 |  |

### electric-charge

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `C` | coulomb | C | `C` |  | `A.h` | 1/3600 |  |  |
| `A.h` | ampere-hour | Ah | `A.h` |  | `A.h` | 1 |  |  |
| `mA.h` | milliampere-hour | mAh | `mA.h` |  | `A.h` | 0.001 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `A.h-C` | `A.h` | `C` | 3600 |  |
| `mA.h-C` | `mA.h` | `C` | 3.6 |  |

### electric-conductivity

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `uS/cm` | microsiemens per centimeter | μS/cm | `uS/cm` |  | `uS/cm` | 1 |  |  |

### information

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `bit` | bit | bit | `bit` |  | `By` | 0.125 |  |  |
| `By` | byte | B | `By` |  | `By` | 1 |  |  |
| `kBy` | kilobyte | kB | `kBy` |  | `By` | 1000 |  |  |
| `MBy` | megabyte | MB | `MBy` |  | `By` | 1000000 |  |  |
| `GBy` | gigabyte | GB | `GBy` |  | `By` | 1000000000 |  |  |
| `KiBy` | kibibyte | KiB | `KiBy` |  | `By` | 1024 |  |  |
| `MiBy` | mebibyte | MiB | `MiBy` |  | `By` | 1048576 |  |  |
| `GiBy` | gibibyte | GiB | `GiBy` |  | `By` | 1073741824 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `By-bit` | `By` | `bit` | 8 |  |
| `kBy-bit` | `kBy` | `bit` | 8000 |  |
| `MBy-bit` | `MBy` | `bit` | 8000000 |  |
| `GBy-bit` | `GBy` | `bit` | 8000000000 |  |
| `KiBy-bit` | `KiBy` | `bit` | 8192 |  |
| `MiBy-bit` | `MiBy` | `bit` | 8388608 |  |
| `GiBy-bit` | `GiBy` | `bit` | 8589934592 |  |

### data-rate

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `bit/s` | bit per second | bit/s | `bit/s` |  | `By/s` | 0.125 |  |  |
| `kbit/s` | kilobit per second | kbit/s | `kbit/s` |  | `By/s` | 125 |  |  |
| `Mbit/s` | megabit per second | Mbit/s | `Mbit/s` |  | `By/s` | 125000 |  |  |
| `By/s` | byte per second | B/s | `By/s` |  | `By/s` | 1 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `kbit/s-bit/s` | `kbit/s` | `bit/s` | 1000 |  |
| `Mbit/s-bit/s` | `Mbit/s` | `bit/s` | 1000000 |  |
| `By/s-bit/s` | `By/s` | `bit/s` | 8 |  |

### angle

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `deg` | degree | ° | `deg` |  | `arcmin` | 60 |  | turn `360` |
| `arcmin` | minute of arc | ′ | `'` |  | `arcmin` | 1 |  | turn `21600` |
| `arcsec` | second of arc | ″ | `''` |  | `arcmin` | 1/60 |  | turn `1296000` |
| `rad` | radian | rad | `rad` |  | `rad` | 1 |  | turn `6.2831853071795864769252867665590057683943` |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `arcmin-deg` | `arcmin` | `deg` | 1/60 |  |
| `arcsec-deg` | `arcsec` | `deg` | 1/3600 |  |

### volume-flow

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `L/min` | liter per minute | L/min | `L/min` |  | `L/h` | 60 |  |  |
| `L/h` | liter per hour | L/h | `L/h` |  | `L/h` | 1 |  |  |
| `mL/min` | milliliter per minute | mL/min | `mL/min` |  | `L/h` | 0.06 |  |  |
| `m3/h` | cubic meter per hour | m³/h | `m3/h` |  | `L/h` | 1000 |  |  |
| `gal/min` | US gallon per minute | gal/min | `[gal_us]/min` |  | `L/h` | 227.12470704 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `L/h-L/min` | `L/h` | `L/min` | 1/60 |  |
| `mL/min-L/min` | `mL/min` | `L/min` | 0.001 |  |
| `m3/h-L/min` | `m3/h` | `L/min` | 50/3 |  |
| `gal/min-L/min` | `gal/min` | `L/min` | 3.785411784 |  |

### mass-flow

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `g/s` | gram per second | g/s | `g/s` |  | `g/s` | 1 |  |  |

### force

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `N` | newton | N | `N` |  | `N` | 1 |  |  |
| `lbf` | pound-force | lbf | `[lbf_av]` |  | `N` | 4.4482216152605 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `lbf-N` | `lbf` | `N` | 4.4482216152605 |  |

### torque

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `N.m` | newton meter | N·m | `N.m` |  | `N.m` | 1 |  |  |
| `lbf.ft` | pound-force foot | lbf·ft | `[lbf_av].[ft_i]` |  | `N.m` | 1.3558179483314004 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `lbf.ft-N.m` | `lbf.ft` | `N.m` | 1.3558179483314004 |  |

### activity-concentration

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `Bq/m3` | becquerel per cubic meter | Bq/m³ | `Bq/m3` |  | `Bq/m3` | 1 |  |  |
| `pCi/L` | picocurie per liter | pCi/L | `pCi/L` |  | `Bq/m3` | 37 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `pCi/L-Bq/m3` | `pCi/L` | `Bq/m3` | 37 |  |

### absorbed-dose

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `Gy` | gray | Gy | `Gy` |  | `Gy` | 1 |  |  |
| `mGy` | milligray | mGy | `mGy` |  | `Gy` | 0.001 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `mGy-Gy` | `mGy` | `Gy` | 0.001 |  |

### dose-equivalent

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `Sv` | sievert | Sv | `Sv` |  | `Sv` | 1 |  |  |
| `mSv` | millisievert | mSv | `mSv` |  | `Sv` | 0.001 |  |  |
| `uSv` | microsievert | μSv | `uSv` |  | `Sv` | 0.000001 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `mSv-Sv` | `mSv` | `Sv` | 0.001 |  |
| `uSv-Sv` | `uSv` | `Sv` | 0.000001 |  |

### dose-equivalent-rate

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `uSv/h` | microsievert per hour | μSv/h | `uSv/h` |  | `uSv/h` | 1 |  |  |

### radioactivity

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `Bq` | becquerel | Bq | `Bq` |  | `Bq` | 1 |  |  |
| `kBq` | kilobecquerel | kBq | `kBq` |  | `Bq` | 1000 |  |  |
| `MBq` | megabecquerel | MBq | `MBq` |  | `Bq` | 1000000 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `kBq-Bq` | `kBq` | `Bq` | 1000 |  |
| `MBq-Bq` | `MBq` | `Bq` | 1000000 |  |

### body-mass-index

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `kg/m2` | kilogram per square meter | kg/m² | `kg/m2` |  | `kg/m2` | 1 |  |  |

### oxygen-uptake-rate

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `mL/kg/min` | milliliter per kilogram per minute | mL/(kg·min) | `mL/(kg.min)` | `mL/(kg*min)`, `ml/(kg·min)` | `MET` | 2/7 |  |  |
| `MET` | metabolic equivalent | MET | `[MET]` |  | `MET` | 1 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `MET-mL/kg/min` | `MET` | `mL/kg/min` | 3.5 |  |

### specific-power

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `W/kg` | watt per kilogram | W/kg | `W/kg` |  | `W/kg` | 1 |  |  |

### specific-energy

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `kcal/kg` | kilocalorie per kilogram | kcal/kg | `kcal/kg` |  | `kcal/kg` | 1 |  |  |

### energy-per-distance

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `kW.h/100km` | kilowatt-hour per 100 kilometers | kWh/100 km | `kW.h/(100.km)` |  | `W.h/km` | 10 |  |  |
| `W.h/km` | watt-hour per kilometer | Wh/km | `W.h/km` |  | `W.h/km` | 1 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `W.h/km-kW.h/100km` | `W.h/km` | `kW.h/100km` | 0.1 |  |

### fuel-consumption

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `L/100km` | liter per 100 kilometers | L/100 km | `L/(100.km)` |  | `L/100km` | 1 |  |  |

### fuel-economy

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `mpg` | mile per US gallon | mpg | `[mi_i]/[gal_us]` |  | `km/L` | 48000/112903 |  |  |
| `km/L` | kilometer per liter | km/L | `km/L` |  | `km/L` | 1 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `km/L-mpg` | `km/L` | `mpg` | 112903/48000 |  |

### precipitation-rate

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `mm/h` | millimeter per hour | mm/h | `mm/h` |  | `in/h` | 5/127 |  |  |
| `in/h` | inch per hour | in/h | `[in_i]/h` |  | `in/h` | 1 |  |  |

| Conversion | From | To | Factor | Offset |
| --- | --- | --- | --- | --- |
| `in/h-mm/h` | `in/h` | `mm/h` | 25.4 |  |

### counts the shipped samples write

| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `steps` | steps | steps | `{steps}` |  | `steps` | 1 |  |  |
| `floors` | floors | floors | `{floors}` |  | `floors` | 1 |  |  |

<!-- END shipped units -->
