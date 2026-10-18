# Units, dimensions and metric catalogues for a domain-neutral `quantity`

Research for the revised health plan (`docs/plans/health-data.md`, section 5.2), 2026-10-08.
Question: should `quantity` and its engine-owned unit table serve every observed quantity
(home, energy, weather, vehicle, devices), not only health, and what should the core unit
table and its notion of "dimension" be.

**How this was checked.** Everything marked "checked" was downloaded or run today. The working
files are kept in `/tmp/health-research/v2/units/`:

- `ucum-essence.xml` (UCUM 2.2, revision date 2024-06-17) and `ucum.html` (the specification),
  from https://github.com/ucum-org/ucum, parsed with a script.
- `UcumFunctionalTests.xml` from https://github.com/FHIR/Ucum-java (`src/test/resources/`).
- `@lhncbc/ucum-lhc` 7.1.10 (the NLM JavaScript UCUM library) installed and run against every
  code in the proposed core table and against the hard cases (`check.js`, `c2.js`).
- QUDT v3.5.2 (released 2026-09-22) unit, quantity kind and dimension vector vocabularies,
  from https://github.com/qudt/qudt-public-repo, counted with grep and awk.
- Home Assistant `homeassistant/util/unit_conversion.py`, `homeassistant/const.py` and
  `homeassistant/components/sensor/const.py` from the `dev` branch of
  https://github.com/home-assistant/core.
- Health Connect unit classes from https://github.com/androidx/androidx
  (`health/connect/connect-client/src/main/java/androidx/health/connect/client/units/`).
- Pint `pint/default_en.txt` from https://github.com/hgrecco/pint.
- Fantom/Haystack `etc/sys/units.txt` from https://github.com/fantom-lang/fantom.
- CF Standard Name Table version 95 (2026-09-16) from
  https://cfconventions.org/Data/cf-standard-names/current/src/cf-standard-name-table.xml.
- Matter cluster XML from https://github.com/project-chip/connectedhomeip
  (`src/app/zap-templates/zcl/data-model/chip/`).
- `coretable.py`, which computes every factor in section 1.10 with exact rational arithmetic
  (Python `fractions`) from the defining constants, and decides "finite decimal or not".

Anything else is labelled "recalled" or "unverified".

## 0. The answers in one page

1. **Widen it.** Nothing in section 5.2 of the plan is health-specific except the examples and
   the `metric` compatibility class. UCUM already covers home, energy, weather, vehicle and
   device units; the extra rules a domain-neutral table needs are the same ones health needs
   (affine temperature, temperature difference, logarithmic levels, counts, arbitrary units).
   Health becomes one catalogue of metrics among several.
2. **Dimension: use a named quantity kind, each carrying a dimension vector.** A pure
   UCUM-style dimension vector cannot tell energy from torque, frequency from activity,
   absorbed dose from dose equivalent, a fraction from a count, or a temperature from a
   temperature difference. The UCUM reference library run today converted `N.m` to `J`,
   `Gy` to `Sv`, `Hz` to `Bq`, `%` to `{beats}` (0.01) and `{beats}/min` to `{steps}/min`
   without complaint. Every system that ships to end users and holds up (Home Assistant,
   Health Connect, Haystack, QUDT, JSR 385, Wolfram, CF conventions) separates at least some
   same-vector quantities by name. Section 2.4 has the reasons and the shape.
3. **Core table size: 198 UCUM codes in 54 quantity kinds** (201 rows, because `K`, `Cel` and
   `[degF]` appear under both `temperature` and `temperature-difference`). Every code
   validates in ucum-lhc 7.1.10. 29 rows have a factor with no finite decimal expansion
   (`km/h`, `[kn_i]`, `[psi]`, `[degF]`, every per-minute and per-hour rate, `kcal/d`, mile
   pace) and one pair is irrational (`deg` to `rad`, a factor of pi). Section 1.10 lists them.
4. **Three corrections to the plan's section 5.2**, each checked:
   - Prefix scaling of a metric arbitrary unit is not allowed by UCUM. Section 25 says any
     term involving an arbitrary unit is not comparable with any other arbitrary term, and
     ucum-lhc refuses `m[IU]/L` to `[IU]/L`. Allowing it is a deliberate extension the plan
     must name as one (section 1.5).
   - UCUM annotations are semantically void: a conformant parser discards them. A substrate
     that wants `{beats}/min` and `{steps}/min` kept apart must make the annotation part of
     identity itself (section 3.4).
   - Several aliases the plan names are not UCUM and need the alias table: `mmHg`, `mcg`,
     `bpm`, `rpm`, `kWh`, `Wh`, `Cal`, `ppm`, `dBm`, `var`, `°C`, `µg` are all refused by
     ucum-lhc; stone is `[stone_av]`, not `[st_av]`.
5. **The UCUM conformance suite does not test temperature, logarithmic or arbitrary-unit
   conversion.** It has 573 cases (529 validation, 9 display-name, 30 conversion,
   2 multiplication, 3 division) and none convert `Cel`, `[degF]`, `B` or `[pH]`. Substrate
   needs its own cases for every special unit it ships.
6. **A non-health seed catalogue holds about 150 metrics** (home climate, air quality,
   electricity, water and gas, weather, vehicle, devices); section 4.9 lists 150 with canonical
   unit, quantity kind and default form. With per-pollutant and per-phase variants it reaches
   about 200.
7. **Currency stays out.** QUDT models 202 currencies as units and Haystack lists them in its
   unit file; both then need special rules because exchange rates change and are not
   conversion factors. Substrate's `money` type is already right.

## 1. UCUM in depth

### 1.1 Release and files (checked)

UCUM 2.2 (June 2024) is the current release. The source of truth is `ucum-source.xml`;
`ucum-essence.xml` is the developer file with prefixes and units only; `ucum.html` is the
specification; `ucum-cs.units` and `ucum-ci.units` are plain-text tables
(https://github.com/ucum-org/ucum/blob/main/docs/artifacts.md). Version 2.2 added `[NTU]`
and `[FNU]` (turbidity) and updated CODATA constants (Avogadro, Boltzmann, elementary charge,
electron and proton mass, G) (https://github.com/ucum-org/ucum/blob/main/CHANGELOG.md).

Counts in `ucum-essence.xml` 2.2:

| item | count |
| --- | --- |
| prefixes | 24 (`Y` to `y`, plus binary `Ki`, `Mi`, `Gi`, `Ti`) |
| base units | 7 |
| defined unit atoms | 305 |
| marked `isMetric="yes"` (prefixable) | 89 |
| marked special (non-ratio scale, defined by a function) | 21 |
| marked arbitrary | 41 |

The prefix list stops at yotta and yocto: the 2022 CGPM prefixes ronna, quetta, ronto and
quecto (`R`, `Q`, `r`, `q`) are not in UCUM 2.2, and binary prefixes stop at `Ti`.

### 1.2 Base units and the dimension vector (checked)

UCUM's seven base units are not the SI's seven:

| UCUM base | dim letter | property | SI difference |
| --- | --- | --- | --- |
| `m` | L | length | same |
| `s` | T | time | same |
| `g` | M | mass | SI base is `kg` |
| `rad` | A | plane angle | SI treats angle as dimensionless |
| `K` | C | temperature | same |
| `C` | Q | electric charge | SI base is the ampere; UCUM defines `A` as `C/s` |
| `cd` | F | luminous intensity | same |

There is no base for amount of substance: `mol` is defined in the essence as
`6.02214076 x 10*23`, a pure number. Strictly, `mmol/L` is therefore commensurable with `/L`
and with `10*9/L` (a cell count). Implementations disagree: ucum-lhc 7.1.10 refused
`mmol/L` to `/L` and `10*9/L` to `nmol/L` when run today, because it tracks moles separately
so that it can ask for a molecular weight. A substrate table should give amount of substance
its own vector component, as the SI does, and say so.

Specification section 19 notes that "most practically used units contain exponents of -4 to
+4", so 4 bits per component suffice; a dimension vector fits in one 32-bit integer.

### 1.3 Metric and non-metric atoms (checked)

Only atoms marked `isMetric="yes"` take a prefix. The 89 metric atoms are:

`mol sr Hz N Pa J W A V F Ohm S Wb Cel T H lm lx Bq Gy Sv l L ar t bar u eV pc [c] [h] [k]
[eps_0] [mu_0] [e] [m_e] [m_p] [G] [g] [ly] gf Ky Gal dyn erg P Bi St Mx G Oe Gb sb Lmb ph Ci
R RAD REM cal_[15] cal_[20] cal_m cal_IT cal_th cal tex m[H2O] m[Hg] eq osm g% kat U [iU] [IU]
Np B B[SPL] B[V] B[mV] B[uV] B[10.nV] B[W] B[kW] st mho bit By Bd`, plus the 7 base units.

Consequences for everyday codes:

- `mm[Hg]`, `cm[H2O]`, `kcal`, `kW.h`, `mbar`, `hPa`, `dL`, `kBy`, `MiBy`, `dB`, `dB[SPL]`,
  `mCel` and `m[IU]` are legal because their atoms are metric.
- `%`, `[ppm]`, `min`, `h`, `d`, `wk`, `[lb_av]`, `[in_i]`, `deg`, `atm`, `[psi]`, `[HP]`,
  `[degF]`, `[pH]`, `[Cal]` and `bit_s` are non-metric: `kmin`, `k[lb_av]` and `m%` are
  invalid.
- `Cel` is marked metric although section 22 says special units "are therefore non-metric";
  the essence follows the 1995 CCU decision that SI prefixes may be used with the degree
  Celsius. `B`, `Np` and the `B[...]` family are metric for the same reason (so `dB` exists),
  while `bit_s` (the logarithmic bit) is not.
- `[Cal]` is `kcal_th` (4184 J exactly) and `cal` is `cal_th` (4.184 J). Food energy from
  every wearable is kilocalories, so `kcal` and `[Cal]` are equal and both should be accepted.
- `[stone_av]` exists (14 `[lb_av]`); there is no `[st_av]`.
- `mo` and `a` are the mean Julian month (30.4375 d) and year (365.25 d). They are not calendar
  months or years.

### 1.4 Special units and their functions (checked)

A special unit is a triple (proper unit, f, f inverse) and may carry a scale factor alpha from
a prefix (specification sections 21 to 23). The essence names the function; the arithmetic is
defined by implementations. The functions below are from ucum-lhc's
`source/ucumFunctions.js` (https://github.com/lhncbc/ucum-lhc), which follows Schadow's Java
reference implementation.

| code | function name in essence | defined on | conversion (x is the value in the proper unit) |
| --- | --- | --- | --- |
| `Cel` | `Cel` | `1 K` | t = x - 273.15 |
| `[degF]` | `degF` | `5 K/9` (Rankine steps) | t = x - 459.67, where x is in units of 5/9 K; so T(K) = (t + 459.67) x 5/9 |
| `[degRe]` | `degRe` | `5 K/4` | Reaumur |
| `[pH]` | `pH` | `1 mol/l` | pH = -lg(x) |
| `Np` | `ln` | `1` | ln(x) |
| `B` | `lg` | `1` | lg(x); `dB` is `B` with prefix d, so 10 lg(x) |
| `B[SPL]` | `lgTimes2` | `2 x 10*-5.Pa` (20 uPa) | 2 lg(p / 20 uPa) bel; `dB[SPL]` = 20 lg(p / 20 uPa) |
| `B[V]`, `B[mV]`, `B[uV]`, `B[10.nV]` | `lgTimes2` | the named voltage | 2 lg(U / U0) bel (a field quantity) |
| `B[W]`, `B[kW]` | `lg` | `1 W`, `1 kW` | lg(P / P0) bel (a power quantity) |
| `bit_s` | `ld` | `1` | log2(x): information content of x states |
| `%[slope]` | `100tan` | `1 deg` | 100 tan(angle) |
| `[p'diop]` | `tanTimes100` | `1 rad` | prism diopter |
| `[m/s2/Hz^(1/2)]` | `sqrt` | `m2/s4/Hz` | amplitude spectral density |
| `[hp'_X]`, `[hp'_C]`, `[hp'_M]`, `[hp'_Q]` | `hpX` and so on | `1` | retired homeopathic potencies |

Checks run today with ucum-lhc: `37 Cel` to `[degF]` gives `98.59999999999997`;
`100 [degF]` to `Cel` gives `37.77777777777783`; `1 dB[SPL]` to `Pa` gives `2.244e-5`;
`[pH]` to `mol/L` is refused; `1 B` to `1` gives `10`; `1 bit_s` to `bit` gives `2`. The
floating-point tails are the case for the plan's exact rational rule.

There is no `dB[mW]` (dBm, the radio signal unit Home Assistant and every Wi-Fi API reports):
ucum-lhc refuses `dB[mW]` and `dBm`. Substrate needs either `dB[W]` with a stated offset
(dBm = dB[W] + 30, exact), a local code, or an annotation. This is the one common everyday
unit UCUM cannot spell.

### 1.5 Arbitrary units (checked)

41 atoms are arbitrary: `[IU]`, `[iU]`, `[arb'U]`, `[USP'U]`, the allergen, infectivity and
assay units (`[CFU]`, `[PFU]`, `[TCID_50]`, `[AU]`, `[BAU]`, `[ELU]`, `[EU]`, `[FEU]`...),
and the homeopathic potencies. Only `[iU]` and `[IU]` are metric, so `m[IU]` and `k[IU]` are
legal codes.

Specification section 24: arbitrary units "are not of any specific dimension and are not
commensurable with any other unit". Section 25: "Any term involving arbitrary units, is
itself an arbitrary unit and is not comparable with any other arbitrary unit or term."
Section 24 also records that before version 1.7 arbitrary units were treated as
dimensionless, "but as an effect the semantics ... made all arbitrary units commensurable".

ucum-lhc follows section 25 literally: `5 [IU]` to `m[IU]` fails with "Attempt to convert to
arbitrary unit", and so do `m[IU]/L` to `[IU]/L` and `[IU]/L` to `m[IU]/mL`. So `isMetric`
makes `m[IU]/L` a legal code without making it convertible.

The plan's section 5.2 says prefix scaling of a metric arbitrary unit "is allowed where UCUM
marks the atom metric". That is a substrate extension, not UCUM. It is arithmetically safe
(same atom, same assay, a power of ten) and labs do send TSH in `m[IU]/L` and `u[IU]/mL`
for the same analyte, so the extension is useful. The plan should label it as an extension,
restrict it to the same arbitrary atom with the same annotation, and keep a test that proves
`[IU]` never converts to `[arb'U]` or to a non-arbitrary unit. This is an owner decision.

`U` (enzyme unit, `umol/min`) and `kat` are proper units of catalytic activity, not arbitrary.

### 1.6 Annotations (checked)

Specification section 6: the full ASCII range 33 to 126 may appear inside `{...}`;
"Annotations do not contribute to the semantics of the unit but are meaningless by
definition. Therefore, any fully conformant parser must discard all annotations"; an
annotation without a leading symbol means the unit `1`; braces must not nest. Section 50
repeats that `{...}` alone is equivalent to `1`.

What this means in practice, from ucum-lhc today: `1 %` to `{beats}` succeeds (0.01) and
`60 {beats}/min` to `{steps}/min` succeeds (60). A conformant UCUM engine will happily
compare heart rate with cadence. Substrate must therefore add its own rule (section 3.4).

The functional tests also pin grammar edge cases: `{a}rad2{b}` and `{|}1` are invalid (an
annotation cannot start a symbol when followed by more), `rad2{錠}` is invalid (no Unicode).

### 1.7 Grammar and case (checked)

- 7-bit ASCII only (section 3). Terminal symbols use characters 33 to 126 except
  `" ( ) + - . / = [ ] { }`. Square brackets mark atoms that would otherwise collide
  (`[in_i]`, `[pi]`, `[degF]`); the apostrophe separates words inside brackets
  (`[in_i'Hg]`).
- Two code systems: case-sensitive (c/s, the one FHIR, OpenTelemetry and every modern system
  use) and case-insensitive (c/i, uppercase, for legacy systems). They are "incompatible" and
  must not be mixed. Substrate should accept c/s only.
- Operators: `.` multiplies, `/` divides (left to right, so `kg/m/s` is kg per m per s), a
  leading `/` means reciprocal (`/min`), exponents are trailing signed integers (`m2`,
  `s-1`), parentheses nest, positive integers are factors (`100.km`, `10*3/uL`).
- A digit run cannot be glued to an atom: `g/12h` is invalid, `g/(12.h)` is valid (functional
  tests 1-263 to 1-265).
- The formal grammar is at https://github.com/ucum-org/ucum/blob/main/docs/formal-grammar.md.

Common spellings refused by ucum-lhc 7.1.10 today, with the UCUM code: `mmHg` (`mm[Hg]`),
`mcg` (`ug`), `µg` (`ug`), `°C` (`Cel`), `Cal` (`[Cal]`), `ppm` (`[ppm]`), `bpm` (`/min` or
`{beats}/min`), `rpm` (`/min` or `{rev}/min`), `kWh` (`kW.h`), `Wh` (`W.h`), `var` and `VAR`
(no UCUM code for reactive power), `dBm` (none), `[st_av]` (`[stone_av]`). The alias table on
input has to cover these; the stored value is always the code.

### 1.8 The conformance suite (checked)

The UCUM functional tests are maintained by Grahame Grieve and licensed under the Eclipse
Public License (https://github.com/ucum-org/ucum/blob/main/docs/functional-tests.md). The
XML lives in the FHIR Java implementation:
https://github.com/FHIR/Ucum-java/blob/master/src/test/resources/UcumFunctionalTests.xml
(last history entry 3 February 2021; file last committed 2025-04-22). A second file,
`UcumFunctionalTests.2.xml`, is an older variant.

| section | cases in `UcumFunctionalTests.xml` |
| --- | --- |
| validation | 529 (490 valid, 39 invalid) |
| displayNameGeneration | 9 |
| conversion | 30 |
| multiplication | 2 |
| division | 3 |
| **total** | **573** |

The 30 conversions are all linear (`mm` to `m`, `[in_i]` to `cm`, `m[Hg]` to base, `[ly]`,
`[mu_0]`, `4.[pi].10*-7.N`). None convert `Cel`, `[degF]`, `B`, `[pH]` or an arbitrary unit.
Case 3-118 converts `6.30 [in_i]` to `0.160 m` and case 3-119 `6.300 [in_i]` to `16.0 cm`:
the suite expects significant digits to carry through, which matters for section 3.11.

Substrate should run the 529 validation cases against its parser (to accept every valid code
it ships and refuse the invalid ones), the 30 conversions for linear units in its table, and
its own cases for each special unit, each arbitrary rule, each annotation rule and each
quantity-kind refusal.

### 1.9 Exactness

UCUM's definitions are decimal numbers and products of other units, so every linear unit
built only from decimal definitions has a rational factor. Two places break that:

- Anything involving `[pi]`: `deg`, `'`, `''`, `gon`, `circ`, `sph`, `[mu_0]`, `[cml_i]`.
  Degree to radian is irrational; a conversion between them must round and say so. Choosing
  `deg` as the stored base for plane angle keeps every everyday angle exact (headings, wind
  direction, slope), and only a request for radians rounds.
- Rational factors whose reduced denominator has a prime other than 2 and 5 have no finite
  decimal: every per-minute, per-hour and per-day rate (1/60, 1/3600, 1/86400), `km/h`
  (5/18 m/s), `[kn_i]` (463/900 m/s), `[psi]` (8896443230521/1290320000 Pa, because the inch
  squared has 127 squared in the denominator), `[degF]` (5/9 K, offset 45967/180 K),
  `kcal/d` (523/10800 W), and mile pace (625/16764 s/m).

Implementations that use floating point leak this: ucum-lhc gives `1 [foz_us]` =
`29.573529562500013 mL` (exact value 29.5735295625); Health Connect hard-codes
`kcal/day` = `0.0484259259` W (exact 523/10800 = 0.0484259259259...), ounces as
`28.34952` g (exact 28.349523125) and glucose mg/dL as `1/18.0` of mmol/L; QUDT stores the
Fahrenheit multiplier as `0.5555555555555555555555555555555556`. Storing each factor as a
numerator and denominator over the defining constants (as `coretable.py` does) removes all of
these.

### 1.10 The proposed core table: 198 codes in 54 quantity kinds

Built from these exact definitions (all checked in UCUM 2.2): `[in_i]` = 0.0254 m,
`[lb_av]` = 0.45359237 kg, `[g]` = 9.80665 m/s2, `[gal_us]` = 231 `[in_i]3`,
`[gal_br]` = 4.54609 L, `cal` = 4.184 J, `[Btu_IT]` = 1.05505585262 kJ,
`m[Hg]` = 133.3220 kPa, `m[H2O]` = 9.80665 kPa, `eV` = 1.602176634e-19 J, `Ci` = 3.7e10 Bq.

Choices behind the table:

- Base units are the SI coherent unit of each kind, except plane angle (`deg`, so the common
  case stays exact) and information (`bit`).
- `K`, `Cel` and `[degF]` appear twice: under `temperature` (affine, with offset) and under
  `temperature-difference` (linear, offset suppressed). The code is the same; the declared
  kind decides. This is how Home Assistant, Health Connect and CF conventions do it (section 3.1).
- Annotated counts and rates are one row each (`{count}`, `{count}/min`) standing for a rule:
  any annotation alone is a count, any annotation over `/min` is a rate. The annotation is
  stored and compared (section 3.4) but adds no row.
- Logarithmic, arbitrary and pH rows have no factor: they are stored and compared in their
  own unit only.
- `mo` and `a` are included only because labs and FHIR send them; they are flagged as mean
  Julian lengths. A calendar duration belongs in substrate's existing `duration` type.
- Left out on purpose: the cgs and natural units, troy and apothecary weights, US survey
  lengths, dry volumes, typesetting units, homeopathic potencies, `cal_IT`, `[Btu_th]`,
  `B[V]`, `B[mV]`, `B[uV]`, `[smoot]`. Each can be added later; the table only grows.

"Factor to base" is exact, and a value converts to the base as value x factor + offset (so
`[degF]` gives K = F x 5/9 + 45967/180, which is (F + 459.67) x 5/9); "finite decimal" means the factor can be written as a terminating
decimal; "rational, no finite decimal" means it cannot, and a converted value must round.

**mass** (9)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `kg` | 1 kg |  | finite decimal |  |
| `g` | 0.001 kg |  | finite decimal |  |
| `mg` | 0.000001 kg |  | finite decimal |  |
| `ug` | 0.000000001 kg |  | finite decimal |  |
| `ng` | 0.000000000001 kg |  | finite decimal |  |
| `t` | 1000 kg |  | finite decimal |  |
| `[lb_av]` | 0.45359237 kg |  | finite decimal |  |
| `[oz_av]` | 0.028349523125 kg |  | finite decimal |  |
| `[stone_av]` | 6.35029318 kg |  | finite decimal |  |

**length** (10)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `m` | 1 m |  | finite decimal |  |
| `km` | 1000 m |  | finite decimal |  |
| `cm` | 0.01 m |  | finite decimal |  |
| `mm` | 0.001 m |  | finite decimal |  |
| `nm` | 0.000000001 m |  | finite decimal |  |
| `[in_i]` | 0.0254 m |  | finite decimal |  |
| `[ft_i]` | 0.3048 m |  | finite decimal |  |
| `[yd_i]` | 0.9144 m |  | finite decimal |  |
| `[mi_i]` | 1609.344 m |  | finite decimal |  |
| `[nmi_i]` | 1852 m |  | finite decimal |  |

**area** (4)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `m2` | 1 m2 |  | finite decimal |  |
| `cm2` | 0.0001 m2 |  | finite decimal |  |
| `har` | 10000 m2 |  | finite decimal |  |
| `[sft_i]` | 0.09290304 m2 |  | finite decimal |  |

**volume** (17)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `m3` | 1 m3 |  | finite decimal |  |
| `L` | 0.001 m3 |  | finite decimal |  |
| `dL` | 0.0001 m3 |  | finite decimal |  |
| `mL` | 0.000001 m3 |  | finite decimal |  |
| `[gal_us]` | 0.003785411784 m3 |  | finite decimal |  |
| `[qt_us]` | 0.000946352946 m3 |  | finite decimal |  |
| `[pt_us]` | 0.000473176473 m3 |  | finite decimal |  |
| `[cup_us]` | 0.0002365882365 m3 |  | finite decimal |  |
| `[foz_us]` | 0.0000295735295625 m3 |  | finite decimal |  |
| `[tbs_us]` | 0.00001478676478125 m3 |  | finite decimal |  |
| `[tsp_us]` | 0.00000492892159375 m3 |  | finite decimal |  |
| `[gal_br]` | 0.00454609 m3 |  | finite decimal |  |
| `[foz_br]` | 0.0000284130625 m3 |  | finite decimal |  |
| `[cup_m]` | 0.00024 m3 |  | finite decimal |  |
| `[tbs_m]` | 0.000015 m3 |  | finite decimal |  |
| `[tsp_m]` | 0.000005 m3 |  | finite decimal |  |
| `[cft_i]` | 0.028316846592 m3 |  | finite decimal |  |

**duration** (9)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `s` | 1 s |  | finite decimal |  |
| `ms` | 0.001 s |  | finite decimal |  |
| `us` | 0.000001 s |  | finite decimal |  |
| `min` | 60 s |  | finite decimal |  |
| `h` | 3600 s |  | finite decimal |  |
| `d` | 86400 s |  | finite decimal |  |
| `wk` | 604800 s |  | finite decimal |  |
| `mo` | 2629800 s |  | finite decimal | UCUM mean Julian month 30.4375 d; not a calendar month |
| `a` | 31557600 s |  | finite decimal | UCUM Julian year 365.25 d; not a calendar year |

**temperature** (3)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `K` | 1 K |  | finite decimal |  |
| `Cel` | 1 K | 273.15 | finite decimal | affine |
| `[degF]` | 5/9 K | 45967/180 | rational, no finite decimal | affine; K = (F + 459.67) x 5/9 |

**temperature-difference** (3)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `K` | 1 K |  | finite decimal |  |
| `Cel` | 1 K |  | finite decimal | offset suppressed |
| `[degF]` | 5/9 K |  | rational, no finite decimal | offset suppressed |

**energy** (10)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `J` | 1 J |  | finite decimal |  |
| `kJ` | 1000 J |  | finite decimal |  |
| `MJ` | 1000000 J |  | finite decimal |  |
| `cal` | 4.184 J |  | finite decimal | UCUM cal = cal_th |
| `kcal` | 4184 J |  | finite decimal |  |
| `[Cal]` | 4184 J |  | finite decimal | nutrition Calorie = kcal_th |
| `W.h` | 3600 J |  | finite decimal |  |
| `kW.h` | 3600000 J |  | finite decimal |  |
| `eV` | 0.0000000000000000001602176634 J |  | finite decimal | exact since 2019 SI |
| `[Btu_IT]` | 1055.05585262 J |  | finite decimal |  |

**power** (9)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `W` | 1 W |  | finite decimal |  |
| `mW` | 0.001 W |  | finite decimal |  |
| `kW` | 1000 W |  | finite decimal |  |
| `MW` | 1000000 W |  | finite decimal |  |
| `[HP]` | 745.69987158227022 W |  | finite decimal | mechanical horsepower |
| `kcal/d` | 523/10800 W |  | rational, no finite decimal | basal metabolic rate (Health Connect) |
| `kcal/h` | 523/450 W |  | rational, no finite decimal |  |
| `[Btu_IT]/h` | 52752792631/180000000000 W |  | rational, no finite decimal | HVAC |
| `V.A` | 1 W |  | finite decimal | apparent power: distinct kind, same factor |

**pressure** (10)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `Pa` | 1 Pa |  | finite decimal |  |
| `hPa` | 100 Pa |  | finite decimal |  |
| `kPa` | 1000 Pa |  | finite decimal |  |
| `bar` | 100000 Pa |  | finite decimal |  |
| `mbar` | 100 Pa |  | finite decimal |  |
| `atm` | 101325 Pa |  | finite decimal |  |
| `mm[Hg]` | 133.322 Pa |  | finite decimal | UCUM conventional 133.322 Pa |
| `cm[H2O]` | 98.0665 Pa |  | finite decimal |  |
| `[psi]` | 8896443230521/1290320000 Pa |  | rational, no finite decimal |  |
| `[in_i'Hg]` | 3386.3788 Pa |  | finite decimal |  |

**speed** (5)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `m/s` | 1 m/s |  | finite decimal |  |
| `km/h` | 5/18 m/s |  | rational, no finite decimal |  |
| `[mi_i]/h` | 0.44704 m/s |  | finite decimal |  |
| `[kn_i]` | 463/900 m/s |  | rational, no finite decimal |  |
| `[ft_i]/s` | 0.3048 m/s |  | finite decimal |  |

**pace** (7)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `s/m` | 1 s/m |  | finite decimal |  |
| `min/km` | 0.06 s/m |  | finite decimal |  |
| `s/km` | 0.001 s/m |  | finite decimal |  |
| `min/[mi_i]` | 625/16764 s/m |  | rational, no finite decimal |  |
| `s/(100.m)` | 0.01 s/m |  | finite decimal |  |
| `s/(100.[yd_i])` | 25/2286 s/m |  | rational, no finite decimal |  |
| `s/(500.m)` | 0.002 s/m |  | finite decimal |  |

**acceleration** (2)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `m/s2` | 1 m/s2 |  | finite decimal |  |
| `[g]` | 9.80665 m/s2 |  | finite decimal |  |

**frequency** (2)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `Hz` | 1 /s |  | finite decimal |  |
| `kHz` | 1000 /s |  | finite decimal |  |

**rate** (6)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `/s` | 1 /s |  | finite decimal |  |
| `/min` | 1/60 /s |  | rational, no finite decimal |  |
| `/h` | 1/3600 /s |  | rational, no finite decimal |  |
| `/d` | 1/86400 /s |  | rational, no finite decimal |  |
| `/wk` | 1/604800 /s |  | rational, no finite decimal |  |
| `{count}/min` | 1/60 /s |  | rational, no finite decimal | any {annotation}/min, e.g. {beats}/min, {steps}/min |

**count** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `{count}` | 1 1 |  | finite decimal | any {annotation} alone is a count; the annotation is stored, ignored in conversion |

**fraction** (6)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `1` | 1 1 |  | finite decimal |  |
| `%` | 0.01 1 |  | finite decimal |  |
| `[ppth]` | 0.001 1 |  | finite decimal |  |
| `[ppm]` | 0.000001 1 |  | finite decimal |  |
| `[ppb]` | 0.000000001 1 |  | finite decimal |  |
| `mmol/mol` | 0.001 1 |  | finite decimal | HbA1c IFCC; a fraction, not convertible to NGSP % |

**amount** (3)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `mol` | 1 mol |  | finite decimal |  |
| `mmol` | 0.001 mol |  | finite decimal |  |
| `umol` | 0.000001 mol |  | finite decimal |  |

**amount-concentration** (5)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `mol/L` | 1000 mol/m3 |  | finite decimal |  |
| `mmol/L` | 1 mol/m3 |  | finite decimal |  |
| `umol/L` | 0.001 mol/m3 |  | finite decimal |  |
| `nmol/L` | 0.000001 mol/m3 |  | finite decimal |  |
| `pmol/L` | 0.000000001 mol/m3 |  | finite decimal |  |

**mass-concentration** (12)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `g/L` | 1 kg/m3 |  | finite decimal |  |
| `g/dL` | 10 kg/m3 |  | finite decimal |  |
| `mg/dL` | 0.01 kg/m3 |  | finite decimal |  |
| `mg/L` | 0.001 kg/m3 |  | finite decimal |  |
| `ug/L` | 0.000001 kg/m3 |  | finite decimal |  |
| `ng/mL` | 0.000001 kg/m3 |  | finite decimal |  |
| `pg/mL` | 0.000000001 kg/m3 |  | finite decimal |  |
| `ug/dL` | 0.00001 kg/m3 |  | finite decimal |  |
| `g/m3` | 0.001 kg/m3 |  | finite decimal |  |
| `mg/m3` | 0.000001 kg/m3 |  | finite decimal |  |
| `ug/m3` | 0.000000001 kg/m3 |  | finite decimal |  |
| `kg/m3` | 1 kg/m3 |  | finite decimal |  |

**catalytic-concentration** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `U/L` | 1/60000 mol/(s.m3) |  | rational, no finite decimal | 1 U = 1 umol/min |

**catalytic-activity** (2)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `kat` | 1 mol/s |  | finite decimal |  |
| `U` | 1/60000000 mol/s |  | rational, no finite decimal |  |

**arbitrary** (5)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `[IU]` | n/a  |  | n/a | commensurable only with itself under one assay |
| `[IU]/L` | n/a  |  | n/a | commensurable only with itself under one assay |
| `m[IU]/L` | n/a  |  | n/a | commensurable only with itself under one assay |
| `[IU]/mL` | n/a  |  | n/a | commensurable only with itself under one assay |
| `[arb'U]` | n/a  |  | n/a | commensurable only with itself under one assay |

**illuminance** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `lx` | 1 lx |  | finite decimal |  |

**luminous-flux** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `lm` | 1 lm |  | finite decimal |  |

**luminous-intensity** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `cd` | 1 cd |  | finite decimal |  |

**luminance** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `cd/m2` | 1 cd/m2 |  | finite decimal |  |

**irradiance** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `W/m2` | 1 W/m2 |  | finite decimal |  |

**level** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `dB` | n/a  |  | n/a | log10, ratio unspecified; never converted, never summed |

**sound-pressure-level** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `dB[SPL]` | n/a  |  | n/a | special: 20 lg(p/20 uPa) dB |

**power-level** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `dB[W]` | n/a  |  | n/a | special: 10 lg(P/1 W) dB |

**acidity** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `[pH]` | n/a  |  | n/a | special: -lg(a(H+)/(mol/L)) |

**electric-potential** (2)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `V` | 1 V |  | finite decimal |  |
| `mV` | 0.001 V |  | finite decimal |  |

**electric-current** (2)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `A` | 1 A |  | finite decimal |  |
| `mA` | 0.001 A |  | finite decimal |  |

**electric-resistance** (2)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `Ohm` | 1 Ohm |  | finite decimal |  |
| `kOhm` | 1000 Ohm |  | finite decimal |  |

**electric-charge** (3)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `C` | 1 C |  | finite decimal |  |
| `A.h` | 3600 C |  | finite decimal |  |
| `mA.h` | 3.6 C |  | finite decimal |  |

**electric-conductivity** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `uS/cm` | 0.0001 S/m |  | finite decimal |  |

**information** (8)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `bit` | 1 bit |  | finite decimal |  |
| `By` | 8 bit |  | finite decimal |  |
| `kBy` | 8000 bit |  | finite decimal |  |
| `MBy` | 8000000 bit |  | finite decimal |  |
| `GBy` | 8000000000 bit |  | finite decimal |  |
| `KiBy` | 8192 bit |  | finite decimal |  |
| `MiBy` | 8388608 bit |  | finite decimal |  |
| `GiBy` | 8589934592 bit |  | finite decimal |  |

**data-rate** (4)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `bit/s` | 1 bit/s |  | finite decimal |  |
| `kbit/s` | 1000 bit/s |  | finite decimal |  |
| `Mbit/s` | 1000000 bit/s |  | finite decimal |  |
| `By/s` | 8 bit/s |  | finite decimal |  |

**angle** (4)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `deg` | 1 deg |  | finite decimal |  |
| `'` | 1/60 deg |  | rational, no finite decimal |  |
| `''` | 1/3600 deg |  | rational, no finite decimal |  |
| `rad` | n/a  |  | n/a | 180/pi deg: irrational |

**volume-flow** (5)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `L/min` | 1/60000 m3/s |  | rational, no finite decimal |  |
| `L/h` | 1/3600000 m3/s |  | rational, no finite decimal |  |
| `mL/min` | 1/60000000 m3/s |  | rational, no finite decimal |  |
| `m3/h` | 1/3600 m3/s |  | rational, no finite decimal |  |
| `[gal_us]/min` | 0.0000630901964 m3/s |  | finite decimal |  |

**mass-flow** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `g/s` | 0.001 kg/s |  | finite decimal |  |

**force** (2)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `N` | 1 N |  | finite decimal |  |
| `[lbf_av]` | 4.4482216152605 N |  | finite decimal |  |

**torque** (2)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `N.m` | 1 N.m |  | finite decimal |  |
| `[lbf_av].[ft_i]` | 1.3558179483314004 N.m |  | finite decimal |  |

**activity-concentration** (2)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `Bq/m3` | 1 Bq/m3 |  | finite decimal |  |
| `pCi/L` | 37 Bq/m3 |  | finite decimal | 1 pCi/L = 37 Bq/m3 |

**dose-equivalent-rate** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `uSv/h` | 1/3600000000 Sv/s |  | rational, no finite decimal |  |

**body-mass-index** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `kg/m2` | 1 kg/m2 |  | finite decimal | same dimension as areal density, different kind |

**oxygen-uptake-rate** (2)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `mL/(kg.min)` | 1/60000000 m3/(kg.s) |  | rational, no finite decimal |  |
| `[MET]` | 7/120000000 m3/(kg.s) |  | rational, no finite decimal | 1 MET = 3.5 mL/min/kg |

**specific-power** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `W/kg` | 1 W/kg |  | finite decimal |  |

**specific-energy** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `kcal/kg` | 4184 J/kg |  | finite decimal |  |

**energy-per-distance** (2)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `kW.h/(100.km)` | 36 J/m |  | finite decimal |  |
| `W.h/km` | 3.6 J/m |  | finite decimal |  |

**fuel-consumption** (1)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `L/(100.km)` | 0.00000001 m3/m |  | finite decimal | reciprocal of mpg; not a factor |

**fuel-economy** (2)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `[mi_i]/[gal_us]` | 48000000000/112903 m/m3 |  | rational, no finite decimal | reciprocal of L/100km |
| `km/L` | 1000000 m/m3 |  | finite decimal |  |

**precipitation-rate** (2)

| code | factor to base | offset | exactness | note |
| --- | --- | --- | --- | --- |
| `mm/h` | 1/3600000 m/s |  | rational, no finite decimal |  |
| `[in_i]/h` | 127/18000000 m/s |  | rational, no finite decimal |  |

Summary of the table: 201 rows, 198 distinct codes, 54 quantity kinds. 29 rows have a
rational factor with no finite decimal; 9 rows have no factor at all (5 arbitrary,
3 logarithmic, `[pH]`); `rad` is the one irrational factor. Every code was validated with
ucum-lhc 7.1.10 (`/tmp/health-research/v2/units/check.js`); spot conversions agreed with the
exact values (`[psi]` 6894.75729316836 Pa, `[HP]` 745.6998715822702 W, `[kn_i]` 1.852 km/h,
`[stone_av]` 6.35029318 kg, `pCi/L` 37 Bq/m3, `[MET]` 3.5 mL/(kg.min),
`kW.h/(100.km)` 36 J/m).

The kind names below are working names for this proposal, not an existing vocabulary.

## 2. Other unit systems and what they got right

### 2.1 Comparison

| system | unit identity | "dimension" notion | same-vector quantities told apart? | affine temperature | temperature difference | logarithmic | user-defined units | exactness |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| UCUM 2.2 | ASCII code, case-sensitive grammar | 7-component vector (m, s, g, rad, K, C, cd) | no; annotations are void | special units with functions | none: no delta unit | `B`, `Np`, `B[SPL]`, `B[W]`, `bit_s` as special units | `{annotation}` only; new atoms by UCUM request | decimal definitions; implementations use floats |
| QUDT v3.5.2 | IRI (`unit:N-M`), plus `qudt:ucumCode` | named quantity kinds plus 8-position vector `A E L I M H T D` | yes: `quantitykind:Torque` and `quantitykind:Energy` share `qkdv:A0E0L2I0M1H0T-2D0` | `conversionMultiplier` plus `conversionOffset` | `quantitykind:TemperatureDifference`, but only `unit:K` maps to it | `unit:B`, `unit:DeciB` and kinds for levels | any graph may add units and kinds | decimals, Fahrenheit multiplier rounded to 34 digits |
| SI brochure, NIST SP 811 | symbols | quantity first, unit second | yes: Hz vs Bq, N m vs J, Gy vs Sv by rule | Celsius temperature t = T - 273.15 K | "a temperature difference ... is the same when expressed in either degrees Celsius or in kelvins" | outside the SI | n/a | exact by definition |
| Pint | names, symbols, aliases in a text file | named dimensions in brackets, derived by formula (`[torque] = [force] * [length]`) | no: `[torque]` is an alias of the energy dimension | `; offset:` in the definition | automatic `delta_degC`, `delta_degF` | `; logbase: 10; logfactor: 10` (dB, dBm, dBW, Np) | `define()` or a definitions file; new base dimensions allowed | floats by default |
| Unitful.jl | Julia types | dimensions as types | no | `@affineunit`, `AffineError` on `+` and `*` | differences of affine quantities return absolute units | separate logarithmic types | `@unit` plus `register` | rationals where possible (`5//9 K`) |
| GNU units (recalled) | names | vector of primitive units | no | nonlinear function units `tempC(x)` with a domain | `degC` is a difference unit | function units | user definition file | floats |
| Frink (recalled) | names | vector | no | `C[x]`, `F[x]` functions; bare `degC` means a difference | yes, by the same split | function units | user file | exact rationals and big integers |
| Home Assistant (dev, checked) | display strings (`kWh`, `°C`, `μg/m³`) | 31 converter classes and 62 sensor device classes | yes: `TemperatureConverter` vs `TemperatureDeltaConverter` share units; per-gas converters | special-cased converter | `temperature_delta` device class | `dB`, `dBm` stored as display strings, never converted | none; integrations pick from enums | floats |
| Health Connect (checked) | typed classes (`Energy`, `Mass`...) | 11 classes | yes: `Temperature` vs `TemperatureDelta`; `Power` includes `kcal/day` | class method | `TemperatureDelta` | none | none | floats, rounded constants |
| Apple HKUnit (checked list, grammar recalled) | unit strings (`count/min`, `mmol<180.15588000005408>/L`) | unit families by constructor | partly: `count` and `percent` scalars; `moleUnit(withMolarMass:)` | `degreeCelsius()`, `degreeFahrenheit()` | no separate difference unit in the constructor list | `decibelAWeightedSoundPressureLevel()`, `decibelHearingLevel()` | unit math only | doubles |
| Google Health API v4 (from 01-wearables.md) | unit in the field name (`weightGrams`, `distance.millimeters`) | per field | per field | n/a | n/a | n/a | none | integers in small units |
| OpenTelemetry semconv | UCUM c/s plus `{annotation}` | none; name carries meaning | by metric name | `Cel` | n/a | n/a | any annotation | n/a |
| SensorThings API 1.1 | `{name, symbol, definition}` per Datastream, "SHOULD follow UCUM" | `ObservedProperty` (a URI) | by `ObservedProperty` | by unit | n/a | n/a | any | n/a |
| Project Haystack (Fantom `units.txt`) | name plus symbols (`kilowatt_hour`, `kWh`), Unicode allowed | 60 named quantity sections, each with a dimension string | yes: `energy`, `apparent energy`, `reactive energy` share `kg1*m2*sec-2` | scale plus offset | `temperature differential` section (`Δ°C`, `Δ°F`) | none | none | decimals |
| Wolfram Language | names (`"DegreesCelsius"`) | quantity variables | partly | separate absolute units | `"DegreesCelsiusDifference"`; `KelvinsDifference` folds into `Kelvins` | `"Decibels"` | `IndependentUnit` | exact rationals |
| JSR 385 / Indriya | Java `Unit<Q>` | 33 quantity interfaces in `javax.measure.quantity` | yes at type level: `RadiationDoseAbsorbed` and `RadiationDoseEffective` are separate interfaces | `Units.CELSIUS` as shifted unit | unverified | unverified | `AlternateUnit`, `ProductUnit` | `Number` incl. rationals in Indriya (unverified) |
| CF conventions 1.11 and later | UDUNITS strings | standard name (5,097 in table v95) plus canonical units | by standard name | `degree_Celsius` | `units_metadata = "temperature: difference"`, `"temperature: on_scale"` or `"temperature: unknown"` | none | n/a | n/a |

Sources: UCUM as above; QUDT https://github.com/qudt/qudt-public-repo (v3.5.2); SI
brochure via NIST SP 330-2019
https://nvlpubs.nist.gov/nistpubs/SpecialPublications/NIST.SP.330-2019.pdf; NIST SP 811
chapters 4 and 7 https://www.nist.gov/pml/special-publication-811; Pint
https://github.com/hgrecco/pint/blob/master/pint/default_en.txt and
https://pint.readthedocs.io/en/stable/user/nonmult.html,
https://pint.readthedocs.io/en/stable/advanced/defining.html; Unitful.jl
https://github.com/PainterQubits/Unitful.jl/blob/master/docs/src/temperature.md and
`conversion.md`, `newunits.md`; GNU units definition line `tempC(x) units=[1;K]
domain=[-273.15,) range=[0,) x K + stdtemp ; (tempC +(-stdtemp))/K` seen in mirrors of
`definitions.units` (the savannah server timed out today); Home Assistant
https://github.com/home-assistant/core; Health Connect
https://github.com/androidx/androidx/tree/androidx-main/health/connect/connect-client/src/main/java/androidx/health/connect/client/units;
HKUnit https://developer.apple.com/documentation/healthkit/hkunit (constructor list read
from the documentation JSON today); OpenTelemetry
https://opentelemetry.io/docs/specs/semconv/general/metrics/; SensorThings
https://docs.ogc.org/is/18-088/18-088.html; Haystack
https://github.com/Project-Haystack/haystack-defs/blob/master/src/docHaystack/doc/Units.fandoc
and https://github.com/fantom-lang/fantom/blob/master/etc/sys/units.txt; Wolfram
https://reference.wolfram.com/language/tutorial/TemperatureUnits.html; JSR 385
https://github.com/unitsofmeasurement/unit-api (`src/main/java/javax/measure/quantity`); CF
https://cfconventions.org/Data/cf-conventions/cf-conventions-1.11/cf-conventions.html.

### 2.2 Details worth keeping

**QUDT (checked counts).** The v3.5.2 vocabularies hold 2,937 unit entries (85 marked
deprecated), 1,226 quantity kinds (98 deprecated) and 273 dimension vectors; 2,435 units
carry a `qudt:ucumCode`; 202 are currencies (`unit:CCY_*`). The brief's "about 1,600 units
and 800 quantity kinds" describes an older release. A QUDT unit lists every quantity kind it
may express (`unit:N-M` has `unitForQuantityKind` Torque and MomentOfForce, not Energy). Its
weak spot is temperature difference: only `unit:K` is declared for
`quantitykind:TemperatureDifference`, and compound units such as `unit:A-PER-DEG_C` or
`unit:BTU_IT-PER-LB-DEG_F` silently mean a difference while `unit:DEG_C` alone carries the
273.15 offset.

**SI and NIST.** SP 811 section 7.4: "it is incorrect to attach letters or other symbols to
the unit in order to provide information about the quantity". Section 7.5: write "the Pb
content is 5 ng/L", not "5 ng Pb/L". Section 7.10.2 allows `%` but forbids "% (by volume)";
section 7.10.3 says ppm, ppb and ppt are "not acceptable for use with the SI". Chapter 4: the
special names hertz and becquerel, newton metre and joule, gray and sievert exist so that
"the values of several different quantities are expressed in the same SI unit" can still be
told apart. This is the SI's own case for quantity kinds over dimension vectors. In
practice, every consumer system still uses ppm and ppb (CO2, VOC), so the table keeps
`[ppm]` and `[ppb]`.

**Pint.** The definition file format (`name = definition = symbol = alias`, `[dimension] =
formula`, `@group`, `@system`, `@context`) is the best-known way to let users add units in
text. A Pint context is a named, parameterised conversion between dimensions:
`@context(mw=0,volume=0,solvent_mass=0) chemistry` converts `[substance] / [volume]` to
`[mass] / [volume]` by `value * mw`. That is exactly the plan's "derivation with a formula
and parameters, stamped on the derived observation" for glucose and HbA1c; Pint just runs it
on read. Pint's Fahrenheit offset is written exactly (`offset: 233.15 + 200 / 9`), and it
refuses ambiguous arithmetic with offset units (`OffsetUnitCalculusError`) unless
`autoconvert_offset_to_baseunit` is set.

**Unitful.jl.** `@affineunit °F "°F" (45967//100)Ra`: the affine unit is defined as an
absolute unit plus an exact rational offset. `32°F + 1°F` and `32°F * 2` throw `AffineError`.
"Exact conversions between units are respected where possible."

**Home Assistant.** The converter class is the quantity kind. `TemperatureConverter` and
`TemperatureDeltaConverter` accept the same three unit strings and differ only in whether
the offset applies. Each gas has its own converter (`CarbonMonoxideConcentrationConverter`,
`NitrogenDioxideConcentrationConverter`, `OzoneConcentrationConverter`,
`SulphurDioxideConcentrationConverter`) because ppb to ug/m3 needs the molar mass and an
assumed 20 C and 1 atm; `BloodGlucoseConcentrationConverter` uses the glucose molar mass.
`SpeedConverter` also converts precipitation intensity (`mm/h`, `in/d`) and Beaufort, which
shows the cost of a dimension-only grouping. `AMBIGUOUS_UNITS` maps U+00B5 (micro sign) to
U+03BC (Greek mu) in display strings, a problem UCUM's ASCII `u` avoids. Each device class
has a default display precision (`UNITS_PRECISION`, for example energy in Wh with 0 decimals,
temperature in K with 1).

**Health Connect.** One class per kind, `Temperature` and `TemperatureDelta` separate,
`Power` with `WATTS` and `KILOCALORIES_PER_DAY` (basal metabolic rate). All constants are
rounded doubles (above).

**OpenTelemetry.** "Instruments for utilization metrics (that measure the fraction out of a
total) are dimensionless and SHOULD use the default unit `1`"; integer counts use only a
curly-brace annotation (`{packet}`, `{process}`), not `1{packet}`; other units "SHOULD be
specified using the UCUM case sensitive (c/s) variant"; durations use `s`; non-prefixed
`By` rather than `MiBy`. The unit never goes in the metric name.

**SensorThings.** The unit sits on the `Datastream` (the series), not on each observation:
`"unitOfMeasurement": {"name": "degree Celsius", "symbol": "°C", "definition":
"http://unitsofmeasure.org/ucum.html#para-30"}`, and a count datastream has null unit
fields. Substrate's plan stores the unit per value, which costs bytes but lets one series
mix sources.

**Haystack.** Sixty named quantity sections, each with a dimension string, and the rule
"all the data associated with a given site should exclusively use either the SI metric
system or the US customary system". Its `temperature` and `temperature differential`
sections are the cleanest statement of the split.

**CF conventions.** Since 1.11, any variable whose units involve temperature should carry
`units_metadata` with `temperature: on_scale`, `temperature: difference` or
`temperature: unknown`. A third state, "unknown", is worth copying for imported data whose
source did not say.

### 2.3 Hz, Bq, J, N.m, Gy, Sv: what a dimension vector alone does

Run today in ucum-lhc 7.1.10: `1 N.m` to `J` = 1, `1 Gy` to `Sv` = 1, `1 Hz` to `Bq` = 1,
`1 %` to `{beats}` = 0.01, `60 {beats}/min` to `{steps}/min` = 60. Each is a correct UCUM
answer and a wrong answer for a user.

### 2.4 Recommendation: named quantity kinds, each with a dimension vector

Use a closed, engine-owned table of **named quantity kinds**. Each kind has:

| field | example (`energy`) | example (`temperature`) |
| --- | --- | --- |
| name | `energy` | `temperature` |
| dimension vector | L2 M1 T-2 | Theta1 |
| base unit | `J` | `K` |
| scale | `ratio` | `affine` (one of `ratio`, `affine`, `difference`, `logarithmic`, `arbitrary`, `count`) |
| units | the codes in section 1.10 | `K`, `Cel`, `[degF]` |
| default display unit | `kJ` (owner choice) | `Cel` |

Reasons:

1. **The vector cannot separate quantities this project needs apart.** In the core table
   alone: energy vs torque (L2 M T-2), frequency vs rate vs activity (T-1), temperature vs
   temperature difference, fraction vs count (both dimensionless), body mass index vs areal
   density (M L-2), mass concentration vs density (M L-3), power vs apparent power (`W` vs
   `V.A`), HbA1c `mmol/mol` vs any other fraction, absorbed dose vs dose equivalent. Each pair
   must never be summed together or converted into each other.
2. **The kind is where the rules live.** Affine or not, logarithmic or not, which reducers are
   legal (no `sum` of temperatures, no `avg` of decibels without converting to power), the
   default display unit, the precision default. A vector has nowhere to put these.
3. **The field agrees.** Home Assistant, Health Connect, Haystack, QUDT, JSR 385, Wolfram and
   CF all name the quantity; the ones that use only vectors (UCUM, Pint, Unitful, GNU units)
   are calculators, not stores.
4. **The vector still earns its place.** Validating that a unit belongs to a kind is a vector
   comparison, so a code outside the core table (`kg/m/s`) can still be checked against a
   kind without listing it. It also lets a future aggregation compute derived kinds
   (energy / duration = power) and refuse nonsense (energy + torque).

Vector components: the SI seven (L, M, T, I, Theta, N, J) plus plane angle and information.
Angle as its own component follows UCUM (`rad` is a base unit there) and keeps `deg` from
silently converting to `1`. Information as a component keeps `By` from converting to `%`,
which UCUM allows today (`bit` is dimensionless; ucum-lhc converts `bit` to `1`). Count and
fraction stay dimensionless and are separated by kind.

A unit may belong to several kinds (`Cel` to `temperature` and `temperature-difference`;
`1` to `count` and `fraction`). Conversion and comparison are allowed only within one kind.
A kind is never inferred from a unit when a declaration names it.

Naming collision to settle: substrate already uses the word **kind** for record kinds, so the
declaration key cannot be `kind`. The plan's `dimension: mass` can stay as the key, with the
documented meaning "a named quantity kind, not a vector"; `measures: mass` reads better and
avoids the clash with UCUM's meaning of dimension. Owner decision.

## 3. The hard cases, each with the rule the best systems use

### 3.1 Temperature and temperature difference

Rule (Health Connect, Home Assistant, Haystack, Wolfram, CF, Pint, Unitful): two kinds, one
set of unit codes. A `temperature` value converts affinely; a `temperature-difference` value
converts by the factor only (0.3 Cel of deviation is 0.54 `[degF]`, not 32.54). Operations
(Wolfram, Pint, Unitful):

- temperature minus temperature gives a temperature difference;
- temperature plus difference gives a temperature;
- temperature plus temperature, and temperature times a scalar, are refused (Pint, Unitful)
  or done in kelvin (Wolfram); refuse, because a store has no reason to do either;
- `avg`, `min`, `max` of temperatures are fine (an average is a weighted sum divided by the
  weights, which stays on the scale); `sum` is refused; a standard deviation is a difference.

Stored value: the source's number and code, never converted to kelvin. For imported data
whose source does not say whether a value is absolute or relative, copy CF's third state
(`unknown`): store it, refuse conversion to Fahrenheit.

### 3.2 Logarithmic units (dB, pH)

Rule (UCUM special units, Pint logarithmic units, Home Assistant): a level is stored and
compared in its own unit and never converted by a factor. Three consequences:

- `dB` without a reference is a bare ratio; `dB[SPL]` (20 uPa) and `dB[W]` (1 W) carry their
  reference. Two levels with different references are different kinds.
- Arithmetic mean of decibels is wrong for sound exposure; the energy-equivalent level
  (`Leq` = 10 lg(mean(10^(L/10)))) is the right reducer. Offer `avg` only as an explicit
  "arithmetic mean of levels" and a separate `energyMean` reducer if sound matters.
- `[pH]` orders inversely to acidity; `min` and `max` are legal, `sum` is not, `avg` is the
  mean of pH values (common in practice, physically a geometric mean of activity). Say so in
  the response.

`dBm` is not expressible in UCUM (section 1.4). Options: accept `dBm` as an alias for a local
code, or store `dB[W]` and add 30. Recommend a local code with a documented definition,
because every Wi-Fi, Bluetooth and cellular API reports dBm.

### 3.3 Percent and fractions

Rules:

- SP 811: `%` is the number 0.01, and "% (by volume)" is wrong: the qualifier belongs to the
  quantity, not the unit. OpenTelemetry: utilisation is a fraction with unit `1` (0 to 1).
- Apple's 0 to 1 versus everyone else's 0 to 100 (plan section 2.3) is exactly `1` versus `%`
  under one `fraction` kind: same quantity, factor 100. The table handles it.
- "Percent of what" belongs to the metric, not the unit: SpO2, body fat, battery,
  relative humidity, CPU utilisation and HbA1c NGSP are each a metric whose unit is `%` (or
  `1`), with the denominator in the metric's definition. Converting between them is
  forbidden by the metric, not the unit.
- A percent change (deviation) is a fraction-difference; it is rare enough to leave as a
  separate metric.
- `%` and a count must never convert (ucum-lhc converts `%` to `{beats}`); kind separation
  enforces it.

### 3.4 Counts and dimensionless units with annotations

Rules (OpenTelemetry, UCUM section 6): a count is written as an annotation alone
(`{steps}`, `{beats}`); a rate is the annotation over time (`{beats}/min`); an unannotated
`/min` is a plain rate. Recommended substrate rule, since UCUM itself discards annotations:

- the annotation is part of a stored unit's identity: `{beats}/min` and `{steps}/min` compare
  unequal and never convert into each other;
- an annotated unit converts to the same annotation with a different time base
  (`{beats}/min` to `{beats}/s` is fine);
- the bare form matches only the bare form; a metric declares which one it uses, and an alias
  maps `bpm` to the metric's form;
- annotations are limited to UCUM's ASCII 33 to 126, no nesting, and should be lower-case
  plural nouns (OpenTelemetry says the grammatical number must match).

This keeps UCUM codes on the wire and adds one rule UCUM leaves to the application.

### 3.5 Currency and money

Currency is not a unit here. Reasons: an exchange rate changes by the minute and is a
market observation, not a definition; a conversion needs a date and a source; minor-unit
exponents differ by currency; and substrate already has `money` with integer minor units.
QUDT (202 `unit:CCY_*` entries) and Haystack (a `currency` section with no dimension) both
carry currencies as units and both then exclude them from conversion, which is the argument
for keeping them apart. Home Assistant has a `monetary` device class with state class `total`
only, and no converter.

A price per unit (`EUR/kW.h`, `USD/[gal_us]`) is a money divided by a quantity. If needed,
it is a `money` plus a `per` quantity unit (`{amount, currency, per: "kW.h"}`), not a UCUM
code. Unverified as a design; flagged for the owner.

### 3.6 Angles

Rules: UCUM makes angle a base dimension; the SI treats it as dimensionless; Home Assistant
has a separate state class `measurement_angle` for wind direction, "currently only degrees".
Recommend: angle is its own kind with base `deg` (exact for everyday data), `rad` converts
with rounding, and a **direction** (wind, heading, bearing) is a circular quantity: `avg` of
359 and 1 is 0, not 180. Directions need a circular mean (mean of unit vectors) or must refuse
`avg`. Home Assistant's separate state class exists for this reason.

### 3.7 Time of day and duration

A duration is a quantity (`s`, `min`, `h`). A time of day, a date, an instant and a calendar
period are not quantities: they belong to substrate's `datetime`, `date` and ISO 8601
`duration` types. Rules:

- "bedtime 23:30" is a time of day, not 23.5 `h`; averaging bedtimes needs a circular mean
  across midnight (same problem as angles).
- `mo` and `a` in UCUM are mean Julian lengths; a calendar month is not 30.4375 days. Accept
  them only where a source sends them (labs), and never use them to bucket time.
- Sleep latency, HRV (`ms`), workout duration and engine run time are durations and convert
  freely.

### 3.8 Cumulative and rate

kW.h is energy, kW is power; steps are a count, steps per minute a rate. The rule every good
system uses is that the form of the value, not the unit, decides the reducer:

| system | the split |
| --- | --- |
| Home Assistant | state class `measurement` vs `total` vs `total_increasing` (a meter that resets) |
| OpenTelemetry | Gauge vs Counter vs UpDownCounter |
| Matter | `CumulativeEnergyImported` vs `PeriodicEnergyImported` attributes |
| HealthKit | cumulative vs discrete aggregation style |
| plan section 5.1 | `instant`, `interval`, `counter`, `summary` |

The plan's four forms map one-to-one: `instant` is a gauge or measurement, `interval` is a
periodic amount, `counter` is `total_increasing` or an OpenTelemetry cumulative Counter, and
`summary` is a vendor statistic. A counter is differenced into intervals; a reset (value goes
down) starts a new run, as Home Assistant's `total_increasing` does. The kind never decides
this: weight is `instant`, but Home Assistant also allows `total` for weight (a feed scale's
dispensed food).

### 3.9 Pace and other inverse quantities

Pace (`min/km`) is time per length, the reciprocal of speed. It is a kind of its own
(`pace`), linear within itself (`min/km` to `min/[mi_i]` is a factor of 1.609344), but its
relation to speed is 1/x, not a factor. Fuel consumption (`L/(100.km)`, dimension L2) and
fuel economy (`[mi_i]/[gal_us]`, L-2) are the same pair. ucum-lhc refuses
`L/(100.km)` to `[mi_i]/[gal_us]` ("cannot be converted"), which is correct for a factor
table. Rules:

- each of the pair is a kind with its own units;
- a reciprocal conversion is allowed only between two kinds declared as reciprocals (pace and
  speed, consumption and economy), and only on single values;
- `avg` of paces is not the pace of the average speed; a reader that wants the session pace
  divides total time by total distance. Refuse `avg` across pace values by default or label
  it "mean of paces".

### 3.10 Custom and user-defined units

QUDT lets any graph add a unit with a multiplier and quantity kinds; Pint lets a user
`define()` a unit or a whole dimension; Unitful uses `@unit` and `register`; UCUM allows
nothing but annotations and asks for new atoms by request. Recommendation for substrate:

- no user-defined units in v1; the table is engine-owned, closed and only grows, like the
  currency table;
- a user who needs a private count uses an annotation (`{cups}`, `{pills}`), which the
  annotation rule already handles without a table entry;
- a user who needs a private conversion (a cup of rice is 185 g) has a derivation, not a
  unit: a function writes a second value stamped with the formula, as the plan does for
  glucose. This is Pint's context idea, run at write time.

If user-defined units come later, declare them as records under the user's authority with
an exact rational factor to a core unit of one kind, never a new kind or a new dimension.

### 3.11 Precision and significant digits

Rules seen: the UCUM tests carry significant digits (`6.30 [in_i]` to `0.160 m`); Home
Assistant stores per-device-class display precision; FHIR `Quantity.value` is a decimal whose
precision is significant (recalled). Recommendation, consistent with the plan's exact
`value` string:

- store the digits as sent (`"72.50"` keeps its trailing zero; precision is information);
- a converted output in another unit is rounded to a precision derived from the input's
  significant digits (the UCUM test behaviour), and the response says it rounded;
- a filter compares exactly (cross-multiplied rationals), never on a rounded value;
- display precision is a property of the metric or the request, never of the stored value.

### 3.12 Localised display symbols and the ASCII code

UCUM codes are ASCII (`Cel`, `ug`, `[degF]`, `mm[Hg]`, `m3`); people read `°C`, `µg`, `℉`,
`mmHg`, `m³`. Rules:

- store the UCUM c/s code only; generate the display symbol from a per-code table (UCUM
  carries a `printSymbol` per atom in the essence; for example `Cel` prints as `°C`);
- the input alias table maps display strings to codes, including both micro signs: U+00B5
  (MICRO SIGN) and U+03BC (GREEK SMALL LETTER MU), which Home Assistant normalises in
  `AMBIGUOUS_UNITS`; `℉` (U+2109) and `℃` (U+2103) compatibility characters; superscript
  digits (`m²`, `m³`); `mcg` (pharmacy for microgram);
- choose display units per user (the plan's `unitpreferences`), never per stored value.

## 4. Metric catalogues outside health

### 4.1 Home Assistant sensor device classes (checked)

The `SensorDeviceClass` enum in `homeassistant/components/sensor/const.py` (dev branch, read
today) has 62 members; 58 are numeric (the other four are `date`, `enum`, `timestamp`,
`uptime`). Each numeric class has an allowed unit set (`DEVICE_CLASS_UNITS`), allowed state
classes (`DEVICE_CLASS_STATE_CLASSES`) and, for 41 of them, a converter
(`UNIT_CONVERTERS`). Selected rows:

| device class | allowed units (display strings) | state classes | converter |
| --- | --- | --- | --- |
| `temperature` | °C, °F, K | measurement | TemperatureConverter |
| `temperature_delta` | °C, °F, K | measurement | TemperatureDeltaConverter |
| `humidity`, `moisture`, `battery` | % | measurement | none |
| `energy` | J, kJ, MJ, GJ, mWh, Wh, kWh, MWh, GWh, TWh, cal, kcal, Mcal, Gcal, thm | total, total_increasing | EnergyConverter |
| `energy_storage` | as energy | measurement | EnergyConverter |
| `power` | mW, W, kW, MW, GW, TW | measurement | PowerConverter |
| `apparent_power`, `reactive_power`, `reactive_energy` | VA, var, varh families | measurement or total | own converters |
| `gas`, `water` | m³, ft³, CCF, MCF, L (water also gal) | total, total_increasing | VolumeConverter |
| `volume_flow_rate` | m³/h, m³/min, m³/s, ft³/min, L/h, L/min, L/s, gal/h, gal/min, gal/d, mL/s | measurement | VolumeFlowRateConverter |
| `precipitation` | mm, cm, in | all | DistanceConverter |
| `precipitation_intensity` | mm/h, mm/d, in/h, in/d | measurement | SpeedConverter |
| `wind_speed`, `speed` | m/s, km/h, mph, kn, ft/s, in/s, m/min, mm/s, Beaufort (speed also takes mm/h and in/d) | measurement | SpeedConverter |
| `wind_direction` | ° | measurement_angle | none |
| `atmospheric_pressure`, `pressure` | mPa, Pa, hPa, kPa, bar, cbar, mbar, mmHg, inHg, inH₂O, psi, atm | measurement | PressureConverter |
| `co2` | ppm | measurement | none |
| `co` | ppb, ppm, mg/m³, µg/m³ | measurement | CarbonMonoxideConcentrationConverter |
| `pm1`, `pm25`, `pm4`, `pm10` | µg/m³ | measurement | none |
| `volatile_organic_compounds` / `_parts` | µg/m³, mg/m³ / ppb, ppm | measurement | MassVolume / UnitlessRatio |
| `radon` | Bq/m³, pCi/L | measurement | RadiationConcentrationConverter |
| `illuminance` | lx | measurement | none |
| `irradiance` | W/m², BTU/(h⋅ft²) | measurement | none |
| `sound_pressure` | dB, dBA | measurement | none |
| `signal_strength` | dB, dBm | measurement | none |
| `voltage`, `current` | V and mV ... / A and mA | measurement | converters |
| `data_size`, `data_rate` | bit to YiB / bit/s to GiB/s | all / measurement | Information / DataRate |
| `distance`, `duration`, `area`, `weight`, `volume` | the usual families | all or total | converters |
| `ph`, `aqi` | none (unitless) | measurement | none |
| `monetary` | any ISO 4217 code | total | none |
| `blood_glucose_concentration` | mg/dL, mmol/L | measurement | BloodGlucoseConcentrationConverter |
| `energy_distance` | kWh/100km, Wh/km, mi/kWh, km/kWh | measurement | EnergyDistanceConverter |

Home Assistant's split of `energy` (a total) from `energy_storage` (a battery's current
content, a measurement) and `volume` from `volume_storage` is the cumulative versus level
distinction of section 3.8, carried in the class name.

### 4.2 Brick and Haystack point types

Brick (https://brickschema.org) classifies building points (sensor, setpoint, command,
status, alarm, parameter) and attaches QUDT quantity kinds (`brick:hasQuantity`) and units
(`brick:hasUnit`). The class count in Brick 1.4 was not verified today (unverified: several
hundred point classes). Haystack tags points (`point`, `sensor`, `sp`, `cmd`) with
quantity-like markers (`temp`, `humidity`, `co2`, `power`, `energy`, `flow`, `pressure`) and
a `unit` from its 60-section database. Both are building-automation oriented (air handlers,
chillers, VAV boxes); a home seed needs a small fraction: zone temperature, humidity, CO2,
setpoints, valve and damper positions, meter readings.

### 4.3 SensorThings ObservedProperty

SensorThings does not ship a catalogue: an `ObservedProperty` is a name, a definition URI and
a description, and deployments point the URI at a vocabulary (QUDT quantity kinds, the CF
standard names, or their own). The lesson is the shape (metric = named URI-identified
property; unit on the series), not a list.

### 4.4 OpenTelemetry system and hardware metrics (checked)

System metrics include `system.cpu.utilization` (`1`), `system.cpu.time` (`s`, Counter),
`system.cpu.frequency` (`Hz`), `system.memory.usage` and `.limit` (`By`),
`system.memory.utilization` (`1`), `system.disk.io` (`By`, Counter),
`system.filesystem.usage` and `.utilization`, `system.network.io` (`By`, Counter),
`system.network.packet.count` (`{packet}`), `system.uptime` (`s`, Gauge),
`system.process.count` (`{process}`). Hardware metrics are `hw.energy` (`J`, Counter;
"recommended to report hw.energy instead of hw.power"), `hw.power` (`W`), `hw.errors`
(`{error}`) and `hw.status` over 14 component types (battery, CPU, fan, GPU, power supply,
temperature, voltage and others), with type-specific metrics such as fan speed and battery
charge (recalled from the hardware pages). Sources:
https://opentelemetry.io/docs/specs/semconv/system/system-metrics/,
https://opentelemetry.io/docs/specs/semconv/hardware/common/.

### 4.5 Matter cluster attributes (checked from the SDK XML)

Matter measurement clusters in `connectedhomeip`: Temperature, Pressure, Flow, Relative
Humidity, Illuminance, Electrical Power, Electrical Energy, and ten concentration clusters
(CO, CO2, NO2, O3, PM2.5, PM1, PM10, formaldehyde, total VOC, radon) plus smoke. The
concentration clusters share `MeasurementUnitEnum`: PPM, PPB, PPT, MGM3, UGM3, NGM3, PM3
(particles per cubic metre), BQM3, and newer DBPM and PCFT; and a `LevelValueEnum` (unknown,
low, medium, high, critical) for devices that only report a level. Electrical Energy has
`CumulativeEnergyImported`, `CumulativeEnergyExported`, `PeriodicEnergyImported`,
`PeriodicEnergyExported`; Electrical Power has active, reactive and apparent current and
power, RMS voltage, frequency, power factor and harmonics. Values are scaled integers in
fixed units (recalled from the Matter 1.3 specification: temperature in 0.01 °C, relative
humidity in 0.01 %, pressure in 0.1 kPa, illuminance as `10000 x log10(lux) + 1`; energy in
mWh, power in mW). The illuminance encoding is logarithmic, so a Matter adapter must decode
before writing `lx`.

### 4.6 OBD-II PIDs (checked against the Wikipedia table)

Service 01 of SAE J1979 defines about 160 PIDs (https://en.wikipedia.org/wiki/OBD-II_PIDs).
Useful for a personal vehicle catalogue, each with its native unit: calculated engine load
(`04`, %), coolant temperature (`05`, °C, A - 40), fuel pressure (`0A`, kPa), intake manifold
pressure (`0B`, kPa), engine speed (`0C`, rpm, (256A + B) / 4), vehicle speed (`0D`, km/h),
intake air temperature (`0F`, °C), mass air flow (`10`, g/s), throttle position (`11`, %),
run time since start (`1F`, s), distance with MIL on (`21`, km), fuel tank level (`2F`, %),
distance since codes cleared (`31`, km), barometric pressure (`33`, kPa), control module
voltage (`42`, V), ambient air temperature (`46`, °C), engine oil temperature (`5C`, °C),
engine fuel rate (`5E`, L/h), odometer (`A6`, km, value / 10). EV state of charge and
battery data come from manufacturer APIs, not standard PIDs.

### 4.7 Meteorological variables (CF and WMO)

The CF Standard Name Table version 95 (2026-09-16) has 5,097 entries and 600 aliases.
Canonical units for the everyday weather set (checked): `air_temperature` K,
`dew_point_temperature` K, `relative_humidity` 1, `air_pressure_at_mean_sea_level` Pa,
`surface_air_pressure` Pa, `wind_speed` and `wind_speed_of_gust` m s-1,
`wind_from_direction` degree, `precipitation_amount` kg m-2, `precipitation_flux`
kg m-2 s-1, `lwe_precipitation_rate` m s-1, `thickness_of_snowfall_amount` m,
`surface_downwelling_shortwave_flux_in_air` W m-2, `visibility_in_air` m,
`ultraviolet_index` 1, `cloud_area_fraction` 1, and mass concentrations of PM2.5 and ozone
in kg m-3. Two lessons: CF states precipitation as mass per area (kg m-2), while every
consumer weather API states depth (mm); 1 mm of liquid water is 1 kg m-2 only by assuming
water's density, so this is a derivation, not a unit conversion. And CF's names encode the
"of what" (`wind_speed_of_gust`) that a unit cannot. WMO's BUFR tables and the WIGOS metadata
standard define the station-observation equivalents (not checked today).

### 4.8 Air-quality standards (WHO 2021)

The WHO global air quality guidelines (2021) state levels in ug/m3 for PM2.5 (annual 5,
24-hour 15), PM10 (annual 15, 24-hour 45), NO2 (annual 10, 24-hour 25), O3 (peak season
8-hour 60, 8-hour 100), SO2 (24-hour 40) and mg/m3 for CO (24-hour 4)
(https://www.ncbi.nlm.nih.gov/pmc/articles/PMC11009431/,
https://www.iqair.com/newsroom/2021-who-air-quality-guidelines; the NO2 24-hour, O3 8-hour,
SO2 and CO figures are recalled, not checked against the WHO PDF today). Consumer sensors
report gases in ppb or ppm, so a gas metric's canonical unit should be ug/m3 to match the
guideline, with ppb accepted and converted by a derivation at a stated temperature and
pressure (Home Assistant uses 20 C and 1 atm), never by the unit table. An AQI is a
dimensionless index whose formula differs by country (US EPA, EU CAQI, UK DAQI): one metric
per scheme.

### 4.9 A seed catalogue: about 150 non-health metrics

Columns: metric id (working name), canonical UCUM unit, quantity kind (section 1.10 names),
default form (plan section 5.1). Sources are the catalogues above; HA = Home Assistant
device class, OT = OpenTelemetry, M = Matter, OBD = PID, CF = standard name.

**Home climate and comfort (14)**

| metric | unit | kind | form | source |
| --- | --- | --- | --- | --- |
| indoor-air-temperature | `Cel` | temperature | instant | HA temperature, M, Haystack |
| indoor-relative-humidity | `%` | fraction | instant | HA humidity, M |
| indoor-absolute-humidity | `g/m3` | mass-concentration | instant | HA absolute_humidity |
| dew-point-indoor | `Cel` | temperature | instant | Haystack |
| thermostat-heating-setpoint | `Cel` | temperature | instant | Haystack sp, M thermostat |
| thermostat-cooling-setpoint | `Cel` | temperature | instant | Haystack sp |
| hvac-supply-air-temperature | `Cel` | temperature | instant | Brick, Haystack |
| temperature-difference-supply-return | `K` | temperature-difference | instant | HA temperature_delta |
| floor-surface-temperature | `Cel` | temperature | instant | HA |
| illuminance-indoor | `lx` | illuminance | instant | HA, M |
| sound-level-indoor | `dB[SPL]` | sound-pressure-level | instant | HA sound_pressure |
| occupancy-count | `{persons}` | count | instant | M occupancy, Brick |
| hvac-runtime | `h` | duration | interval | Haystack, thermostat APIs |
| valve-position | `%` | fraction | instant | Brick, Haystack |

**Air quality (16)**

| metric | unit | kind | form | source |
| --- | --- | --- | --- | --- |
| co2-concentration | `[ppm]` | fraction | instant | HA co2, M |
| pm1-concentration | `ug/m3` | mass-concentration | instant | HA pm1, M |
| pm2.5-concentration | `ug/m3` | mass-concentration | instant | HA pm25, M, CF, WHO |
| pm4-concentration | `ug/m3` | mass-concentration | instant | HA pm4 |
| pm10-concentration | `ug/m3` | mass-concentration | instant | HA pm10, M, WHO |
| particle-count-pm2.5 | `/m3` | number-concentration (add to table) | instant | M PM3 |
| tvoc-mass-concentration | `ug/m3` | mass-concentration | instant | HA volatile_organic_compounds |
| tvoc-ratio | `[ppb]` | fraction | instant | HA volatile_organic_compounds_parts, M |
| co-concentration | `[ppm]` | fraction | instant | HA co, M |
| no2-concentration | `ug/m3` | mass-concentration | instant | HA, M, WHO |
| o3-concentration | `ug/m3` | mass-concentration | instant | HA, M, WHO |
| so2-concentration | `ug/m3` | mass-concentration | instant | HA, WHO |
| formaldehyde-concentration | `ug/m3` | mass-concentration | instant | M |
| radon-activity-concentration | `Bq/m3` | activity-concentration | instant | HA radon, M |
| aqi-us-epa | `{index}` | count (index) | instant | HA aqi |
| aqi-eu-caqi | `{index}` | count (index) | instant | regional |

**Electricity (22)**

| metric | unit | kind | form | source |
| --- | --- | --- | --- | --- |
| grid-energy-imported | `kW.h` | energy | counter | HA energy total_increasing, M CumulativeEnergyImported |
| grid-energy-exported | `kW.h` | energy | counter | M CumulativeEnergyExported |
| energy-imported-period | `kW.h` | energy | interval | M PeriodicEnergyImported, utility APIs |
| energy-exported-period | `kW.h` | energy | interval | M PeriodicEnergyExported |
| active-power | `W` | power | instant | HA power, M ActivePower |
| apparent-power | `V.A` | apparent-power (add to table) | instant | HA apparent_power, M |
| reactive-power | `V.A{reactive}` (no UCUM `var`) | reactive-power (add) | instant | HA reactive_power, M |
| reactive-energy | `V.A.h{reactive}` | reactive-energy (add) | counter | HA reactive_energy |
| power-factor | `1` | fraction | instant | HA power_factor, M |
| rms-voltage | `V` | electric-potential | instant | HA voltage, M |
| rms-current | `A` | electric-current | instant | HA current, M |
| grid-frequency | `Hz` | frequency | instant | HA frequency, M |
| solar-power-produced | `W` | power | instant | inverter APIs |
| solar-energy-produced | `kW.h` | energy | counter | inverter APIs |
| solar-irradiance | `W/m2` | irradiance | instant | HA irradiance, CF |
| battery-storage-energy | `kW.h` | energy | instant | HA energy_storage |
| battery-storage-state-of-charge | `%` | fraction | instant | HA battery |
| battery-storage-power | `W` | power | instant | inverter APIs |
| ev-charger-power | `W` | power | instant | charger APIs |
| ev-charge-session-energy | `kW.h` | energy | interval | charger APIs |
| device-energy (per plug) | `kW.h` | energy | counter | smart plugs, HA |
| device-power (per plug) | `W` | power | instant | smart plugs |

**Water, gas, heating (12)**

| metric | unit | kind | form | source |
| --- | --- | --- | --- | --- |
| water-meter-total | `m3` | volume | counter | HA water |
| water-consumed-period | `L` | volume | interval | utility APIs |
| water-flow-rate | `L/min` | volume-flow | instant | HA volume_flow_rate, M flow |
| water-temperature-hot | `Cel` | temperature | instant | HA |
| water-pressure | `bar` | pressure | instant | HA pressure |
| gas-meter-total | `m3` | volume | counter | HA gas |
| gas-consumed-period | `m3` | volume | interval | utility APIs |
| gas-energy-period | `kW.h` | energy | interval | utility bills (calorific conversion is a derivation) |
| heat-meter-energy | `kW.h` | energy | counter | district heating meters |
| boiler-flow-temperature | `Cel` | temperature | instant | heat pumps, OpenTherm |
| heat-pump-cop | `1` | fraction (ratio) | summary | heat pump APIs |
| tank-level | `%` | fraction | instant | HA volume_storage |

**Weather, outdoor (25)**

| metric | unit | kind | form | source |
| --- | --- | --- | --- | --- |
| air-temperature | `Cel` | temperature | instant | CF air_temperature |
| apparent-temperature | `Cel` | temperature | instant | weather APIs |
| dew-point-temperature | `Cel` | temperature | instant | CF |
| relative-humidity | `%` | fraction | instant | CF (unit 1) |
| sea-level-pressure | `hPa` | pressure | instant | CF air_pressure_at_mean_sea_level |
| station-pressure | `hPa` | pressure | instant | CF surface_air_pressure |
| wind-speed | `m/s` | speed | instant | CF wind_speed |
| wind-gust | `m/s` | speed | instant | CF wind_speed_of_gust |
| wind-direction | `deg` | angle (direction) | instant | CF wind_from_direction |
| precipitation-amount | `mm` | length | interval | HA precipitation (CF uses kg m-2) |
| precipitation-rate | `mm/h` | precipitation-rate | instant | HA precipitation_intensity |
| precipitation-probability | `%` | fraction | instant | forecast APIs |
| snowfall-depth | `cm` | length | interval | CF thickness_of_snowfall_amount |
| snow-depth | `cm` | length | instant | CF surface_snow_thickness |
| global-horizontal-irradiance | `W/m2` | irradiance | instant | CF |
| uv-index | `{index}` | count (index) | instant | CF ultraviolet_index |
| cloud-cover | `%` | fraction | instant | CF cloud_area_fraction |
| visibility | `km` | length | instant | CF visibility_in_air |
| sunshine-duration | `h` | duration | interval | CF duration_of_sunshine |
| outdoor-illuminance | `lx` | illuminance | instant | HA |
| soil-temperature | `Cel` | temperature | instant | CF |
| soil-moisture | `%` | fraction | instant | HA moisture |
| lightning-strike-count | `{strikes}` | count | interval | station APIs |
| lightning-distance | `km` | length | instant | station APIs |
| pollen-count | `/m3` | number-concentration | instant | pollen APIs |

**Vehicle (24)**

| metric | unit | kind | form | source |
| --- | --- | --- | --- | --- |
| odometer | `km` | length | counter | OBD A6, car APIs |
| trip-distance | `km` | length | interval | car APIs |
| vehicle-speed | `km/h` | speed | instant | OBD 0D |
| engine-speed | `{rev}/min` | rate | instant | OBD 0C |
| engine-load | `%` | fraction | instant | OBD 04 |
| throttle-position | `%` | fraction | instant | OBD 11 |
| coolant-temperature | `Cel` | temperature | instant | OBD 05 |
| intake-air-temperature | `Cel` | temperature | instant | OBD 0F |
| engine-oil-temperature | `Cel` | temperature | instant | OBD 5C |
| ambient-air-temperature-vehicle | `Cel` | temperature | instant | OBD 46 |
| mass-air-flow | `g/s` | mass-flow | instant | OBD 10 |
| intake-manifold-pressure | `kPa` | pressure | instant | OBD 0B |
| barometric-pressure-vehicle | `kPa` | pressure | instant | OBD 33 |
| fuel-level | `%` | fraction | instant | OBD 2F |
| fuel-rate | `L/h` | volume-flow | instant | OBD 5E |
| fuel-added | `L` | volume | interval | fuel logs |
| fuel-consumption | `L/(100.km)` | fuel-consumption | summary | trip computers |
| electric-consumption | `kW.h/(100.km)` | energy-per-distance | summary | HA energy_distance |
| battery-voltage-12v | `V` | electric-potential | instant | OBD 42 |
| ev-state-of-charge | `%` | fraction | instant | car APIs |
| ev-range | `km` | length | instant | car APIs |
| tyre-pressure | `kPa` | pressure | instant | TPMS via car APIs |
| engine-run-time | `s` | duration | interval | OBD 1F |
| vehicle-acceleration | `m/s2` | acceleration | instant | telematics |

**Devices and computing (20)**

| metric | unit | kind | form | source |
| --- | --- | --- | --- | --- |
| device-battery-level | `%` | fraction | instant | HA battery, phones, wearables |
| device-battery-charge | `mA.h` | electric-charge | instant | OT hw battery, phones |
| device-battery-temperature | `Cel` | temperature | instant | OT hw |
| device-battery-cycles | `{cycles}` | count | counter | phones, laptops |
| cpu-utilization | `1` | fraction | instant | OT system.cpu.utilization |
| cpu-temperature | `Cel` | temperature | instant | OT hw temperature |
| cpu-frequency | `Hz` | frequency | instant | OT system.cpu.frequency |
| memory-usage | `By` | information | instant | OT system.memory.usage |
| disk-usage | `By` | information | instant | OT system.filesystem.usage |
| disk-io | `By` | information | counter | OT system.disk.io |
| network-io | `By` | information | counter | OT system.network.io |
| network-throughput | `bit/s` | data-rate | instant | HA data_rate |
| wifi-signal-strength | `dBm` (local code) | power-level | instant | HA signal_strength |
| device-uptime | `s` | duration | instant | OT system.uptime |
| device-power-draw | `W` | power | instant | OT hw.power |
| device-energy | `J` | energy | counter | OT hw.energy |
| fan-speed | `{rev}/min` | rate | instant | OT hw fan (recalled) |
| screen-time | `min` | duration | interval | phone OS reports |
| phone-unlocks | `{unlocks}` | count | interval | phone OS reports |
| printer-pages | `{pages}` | count | counter | printers |

**Appliances, garden, pool and pets (17)**

| metric | unit | kind | form | source |
| --- | --- | --- | --- | --- |
| fridge-temperature | `Cel` | temperature | instant | appliance APIs |
| freezer-temperature | `Cel` | temperature | instant | appliance APIs |
| oven-temperature | `Cel` | temperature | instant | SensorThings example, appliances |
| washer-cycle-energy | `kW.h` | energy | interval | appliance APIs |
| washer-cycle-water | `L` | volume | interval | appliance APIs |
| dishwasher-cycle-duration | `min` | duration | interval | appliance APIs |
| robot-vacuum-area-cleaned | `m2` | area | interval | HA area |
| coffee-cups-brewed | `{cups}` | count | counter | appliance APIs |
| plant-soil-moisture | `%` | fraction | instant | HA moisture |
| plant-soil-conductivity | `uS/cm` | electric-conductivity | instant | HA conductivity |
| plant-light | `lx` | illuminance | instant | plant sensors |
| pool-water-temperature | `Cel` | temperature | instant | pool controllers |
| pool-ph | `[pH]` | acidity | instant | HA ph |
| pool-orp | `mV` | electric-potential | instant | pool controllers |
| pool-free-chlorine | `mg/L` | mass-concentration | instant | pool testers |
| pet-food-dispensed | `g` | mass | interval | HA weight (total) |
| aquarium-temperature | `Cel` | temperature | instant | sensors |

Total in these tables: 150 metrics. With per-phase electricity (three phases times voltage,
current, power), per-zone climate templates (bedroom, living room), extra pollutants (NH3,
H2S, NO) and forecast variants, a broad seed reaches about 200. The table forces five kinds
not yet in section 1.10, which brings the core table to about 203 codes: `/m3`
(number-concentration), `V.A` as its own `apparent-power` kind, reactive power and reactive
energy (no UCUM code: an annotated `V.A{reactive}` or a local code), and `dBm`
(power-level, local code).

### 4.10 What the catalogues teach the `metric` record

1. The metric, not the unit, carries "of what" (CF `wind_speed_of_gust`, HA `pm25`, OBD
   `05`), the denominator of a percent, and the molar mass a ppb to ug/m3 derivation needs.
2. The metric carries a default form, and the form can differ from the kind's usual one (HA
   allows `total` for `weight`).
3. The metric carries a default display precision (HA `UNITS_PRECISION`).
4. Directions (wind, heading) and times of day need a circular flag on the metric, because
   `avg` is wrong for them.
5. A metric may name several accepted source units but stores one canonical unit per
   series view; the value keeps the source unit (plan section 5.2) and the read converts.
