# Units

<!-- Written by TestUnitsPage from internal/vocabulary/unitstable.go; rewrite with
     go test ./internal/vocabulary/ -run TestUnitsPage -update, never by hand. -->

Every unit a `quantity` value may carry, grouped by **measure**: the named kind of
quantity (mass, temperature, rate) inside which a value converts and compares.
[The data model](data-model.md#property-types) says what a quantity value is and how a
property declares one. This page is the table itself: 57 measures and 206 codes
(210 rows, because a few codes sit in two measures).

A code is a case-sensitive UCUM code. The codes listed here are the ones the table
names; the grammar also reads any compound or prefixed code over the same atoms
(`kg/h`, `GJ`, `fL`), whose measure is found from its prefixes and its
dimension. The table only grows: a code, a measure or a factor is never removed
or changed, because a stored value is read through it.

Each factor is exact, a rational number: a value converts to the measure's base as
`value x factor + offset`. "none" marks a code no rational factor reaches: a
level, an arbitrary unit, and `rad`, which is 180/pi degrees. Such a code converts
to nothing but itself.

Unit codes are from UCUM 2.2, copyright Regenstrief Institute, Inc., used under the
UCUM license. `dBm` is the substrate's own code; UCUM has no spelling for it.

## Measures

The dimension is over the nine base quantities: length (L), mass (M), time (T),
electric current (I), temperature (Θ), amount of substance (N), luminous
intensity (J), plane angle (A) and information (B). Two measures may share a
dimension (energy and torque); that is why a measure has a name.

| Measure | Scale | Base | Dimension | Shown in | Codes |
| --- | --- | --- | --- | --- | --- |
| [mass](#mass) | ratio | `kg` | M | `kg` | `kg`, `g`, `mg`, `ug`, `ng`, `t`, `[lb_av]`, `[oz_av]`, `[stone_av]` |
| [length](#length) | ratio | `m` | L | `m` | `m`, `km`, `cm`, `mm`, `nm`, `[in_i]`, `[ft_i]`, `[yd_i]`, `[mi_i]`, `[nmi_i]` |
| [area](#area) | ratio | `m2` | L2 | `m2` | `m2`, `cm2`, `har`, `[sft_i]` |
| [volume](#volume) | ratio | `m3` | L3 | `L` | `m3`, `L`, `dL`, `mL`, `[gal_us]`, `[qt_us]`, `[pt_us]`, `[cup_us]`, `[foz_us]`, `[tbs_us]`, `[tsp_us]`, `[gal_br]`, `[foz_br]`, `[cup_m]`, `[tbs_m]`, `[tsp_m]`, `[cft_i]` |
| [duration](#duration) | ratio | `s` | T | `min` | `s`, `ms`, `us`, `min`, `h`, `d`, `wk`, `mo`, `a` |
| [temperature](#temperature) | affine | `K` | Θ | `Cel` | `K`, `Cel`, `[degF]` |
| [temperature-difference](#temperature-difference) | difference | `K` | Θ | `K` | `K`, `Cel`, `[degF]` |
| [energy](#energy) | ratio | `J` | L2 M T-2 | `kJ` | `J`, `kJ`, `MJ`, `cal`, `kcal`, `[Cal]`, `W.h`, `kW.h`, `eV`, `[Btu_IT]` |
| [power](#power) | ratio | `W` | L2 M T-3 | `W` | `W`, `mW`, `kW`, `MW`, `[HP]`, `kcal/d`, `kcal/h`, `[Btu_IT]/h` |
| [apparent-power](#apparent-power) | ratio | `V.A` | L2 M T-3 | `V.A` | `V.A` |
| [reactive-power](#reactive-power) | ratio | `V.A{reactive}` | L2 M T-3 | `V.A{reactive}` | `V.A{reactive}` |
| [pressure](#pressure) | ratio | `Pa` | L-1 M T-2 | `hPa` | `Pa`, `hPa`, `kPa`, `bar`, `mbar`, `atm`, `mm[Hg]`, `cm[H2O]`, `[psi]`, `[in_i'Hg]` |
| [speed](#speed) | ratio | `m/s` | L T-1 | `km/h` | `m/s`, `km/h`, `[mi_i]/h`, `[kn_i]`, `[ft_i]/s` |
| [pace](#pace) | ratio | `s/m` | L-1 T | `min/km` | `s/m`, `min/km`, `s/km`, `min/[mi_i]`, `s/(100.m)`, `s/(100.[yd_i])`, `s/(500.m)` |
| [acceleration](#acceleration) | ratio | `m/s2` | L T-2 | `m/s2` | `m/s2`, `[g]` |
| [frequency](#frequency) | ratio | `Hz` | T-1 | `Hz` | `Hz`, `kHz` |
| [rate](#rate) | ratio | `/s` | T-1 | `/min` | `/s`, `/min`, `/h`, `/d`, `/wk`, `{count}/min` |
| [count](#count) | count | `1` | 1 | `{count}` | `{count}`, `1` |
| [fraction](#fraction) | ratio | `1` | 1 | `%` | `1`, `%`, `[ppth]`, `[ppm]`, `[ppb]`, `mmol/mol` |
| [amount-of-substance](#amount-of-substance) | ratio | `mol` | N | `mmol` | `mol`, `mmol`, `umol` |
| [amount-concentration](#amount-concentration) | ratio | `mol/m3` | L-3 N | `mmol/L` | `mol/L`, `mmol/L`, `umol/L`, `nmol/L`, `pmol/L` |
| [mass-concentration](#mass-concentration) | ratio | `kg/m3` | L-3 M | `mg/dL` | `g/L`, `g/dL`, `mg/dL`, `mg/L`, `ug/L`, `ng/mL`, `pg/mL`, `ug/dL`, `g/m3`, `mg/m3`, `ug/m3`, `kg/m3` |
| [number-concentration](#number-concentration) | ratio | `/m3` | L-3 | `10*9/L` | `/L`, `/uL`, `10*3/uL`, `10*6/uL`, `10*9/L`, `10*12/L` |
| [catalytic-concentration](#catalytic-concentration) | ratio | `mol/(s.m3)` | L-3 T-1 N | `U/L` | `U/L` |
| [catalytic-activity](#catalytic-activity) | ratio | `mol/s` | T-1 N | `U` | `kat`, `U` |
| [arbitrary](#arbitrary) | arbitrary | none | none | `[IU]` | `[IU]`, `[IU]/L`, `m[IU]/L`, `[IU]/mL`, `[arb'U]` |
| [illuminance](#illuminance) | ratio | `lx` | L-2 J A2 | `lx` | `lx` |
| [luminous-flux](#luminous-flux) | ratio | `lm` | J A2 | `lm` | `lm` |
| [luminous-intensity](#luminous-intensity) | ratio | `cd` | J | `cd` | `cd` |
| [luminance](#luminance) | ratio | `cd/m2` | L-2 J | `cd/m2` | `cd/m2` |
| [irradiance](#irradiance) | ratio | `W/m2` | M T-3 | `W/m2` | `W/m2` |
| [level](#level) | logarithmic | `dB` | none | `dB` | `dB` |
| [sound-pressure-level](#sound-pressure-level) | logarithmic | `dB[SPL]` | none | `dB[SPL]` | `dB[SPL]` |
| [power-level](#power-level) | logarithmic | `dB[W]` | none | `dBm` | `dB[W]`, `dBm` |
| [acidity](#acidity) | logarithmic | `[pH]` | none | `[pH]` | `[pH]` |
| [electric-potential](#electric-potential) | ratio | `V` | L2 M T-3 I-1 | `V` | `V`, `mV` |
| [electric-current](#electric-current) | ratio | `A` | I | `A` | `A`, `mA` |
| [electric-resistance](#electric-resistance) | ratio | `Ohm` | L2 M T-3 I-2 | `Ohm` | `Ohm`, `kOhm` |
| [electric-charge](#electric-charge) | ratio | `C` | T I | `mA.h` | `C`, `A.h`, `mA.h` |
| [electric-conductivity](#electric-conductivity) | ratio | `S/m` | L-3 M-1 T3 I2 | `uS/cm` | `uS/cm` |
| [information](#information) | ratio | `bit` | B | `By` | `bit`, `By`, `kBy`, `MBy`, `GBy`, `KiBy`, `MiBy`, `GiBy` |
| [data-rate](#data-rate) | ratio | `bit/s` | T-1 B | `Mbit/s` | `bit/s`, `kbit/s`, `Mbit/s`, `By/s` |
| [angle](#angle) | ratio | `deg` | A | `deg` | `deg`, `'`, `''`, `rad` |
| [volume-flow](#volume-flow) | ratio | `m3/s` | L3 T-1 | `L/min` | `L/min`, `L/h`, `mL/min`, `m3/h`, `[gal_us]/min` |
| [mass-flow](#mass-flow) | ratio | `kg/s` | M T-1 | `g/s` | `g/s` |
| [force](#force) | ratio | `N` | L M T-2 | `N` | `N`, `[lbf_av]` |
| [torque](#torque) | ratio | `N.m` | L2 M T-2 | `N.m` | `N.m`, `[lbf_av].[ft_i]` |
| [activity-concentration](#activity-concentration) | ratio | `Bq/m3` | L-3 T-1 | `Bq/m3` | `Bq/m3`, `pCi/L` |
| [dose-equivalent-rate](#dose-equivalent-rate) | ratio | `Sv/s` | L2 T-3 | `uSv/h` | `uSv/h` |
| [body-mass-index](#body-mass-index) | ratio | `kg/m2` | L-2 M | `kg/m2` | `kg/m2` |
| [oxygen-uptake-rate](#oxygen-uptake-rate) | ratio | `m3/(kg.s)` | L3 M-1 T-1 | `mL/(kg.min)` | `mL/(kg.min)`, `[MET]` |
| [specific-power](#specific-power) | ratio | `W/kg` | L2 T-3 | `W/kg` | `W/kg` |
| [specific-energy](#specific-energy) | ratio | `J/kg` | L2 T-2 | `kcal/kg` | `kcal/kg` |
| [energy-per-distance](#energy-per-distance) | ratio | `J/m` | L M T-2 | `kW.h/(100.km)` | `kW.h/(100.km)`, `W.h/km` |
| [fuel-consumption](#fuel-consumption) | ratio | `m3/m` | L2 | `L/(100.km)` | `L/(100.km)` |
| [fuel-economy](#fuel-economy) | ratio | `m/m3` | L-2 | `km/L` | `[mi_i]/[gal_us]`, `km/L` |
| [precipitation-rate](#precipitation-rate) | ratio | `m/s` | L T-1 | `mm/h` | `mm/h`, `[in_i]/h` |

The scales: `ratio` converts by a factor; `affine` by a factor and an offset (an
absolute temperature); `difference` by the factor alone (a temperature
difference: 0.3 `Cel` is 0.54 `[degF]`); `logarithmic` converts to nothing but
itself; `arbitrary` converts only to the same assay unit under another metric
prefix (`m[IU]/L` to `[IU]/L`, a substrate extension that UCUM does not make);
`count` converts only between units counting the same annotation.

## Codes by measure

### mass

| Code | Symbol | Factor to `kg` | Offset |
| --- | --- | --- | --- |
| `kg` | kg | 1 |  |
| `g` | g | 1/1000 |  |
| `mg` | mg | 1/1000000 |  |
| `ug` | μg | 1/1000000000 |  |
| `ng` | ng | 1/1000000000000 |  |
| `t` | t | 1000 |  |
| `[lb_av]` | lb | 45359237/100000000 |  |
| `[oz_av]` | oz | 45359237/1600000000 |  |
| `[stone_av]` | st | 317514659/50000000 |  |

### length

| Code | Symbol | Factor to `m` | Offset |
| --- | --- | --- | --- |
| `m` | m | 1 |  |
| `km` | km | 1000 |  |
| `cm` | cm | 1/100 |  |
| `mm` | mm | 1/1000 |  |
| `nm` | nm | 1/1000000000 |  |
| `[in_i]` | in | 127/5000 |  |
| `[ft_i]` | ft | 381/1250 |  |
| `[yd_i]` | yd | 1143/1250 |  |
| `[mi_i]` | mi | 201168/125 |  |
| `[nmi_i]` | nmi | 1852 |  |

### area

| Code | Symbol | Factor to `m2` | Offset |
| --- | --- | --- | --- |
| `m2` | m² | 1 |  |
| `cm2` | cm² | 1/10000 |  |
| `har` | ha | 10000 |  |
| `[sft_i]` | ft² | 145161/1562500 |  |

### volume

| Code | Symbol | Factor to `m3` | Offset |
| --- | --- | --- | --- |
| `m3` | m³ | 1 |  |
| `L` | L | 1/1000 |  |
| `dL` | dL | 1/10000 |  |
| `mL` | mL | 1/1000000 |  |
| `[gal_us]` | gal | 473176473/125000000000 |  |
| `[qt_us]` | qt | 473176473/500000000000 |  |
| `[pt_us]` | pt | 473176473/1000000000000 |  |
| `[cup_us]` | cup | 473176473/2000000000000 |  |
| `[foz_us]` | fl oz | 473176473/16000000000000 |  |
| `[tbs_us]` | tbsp | 473176473/32000000000000 |  |
| `[tsp_us]` | tsp | 157725491/32000000000000 |  |
| `[gal_br]` | gal (UK) | 454609/100000000 |  |
| `[foz_br]` | fl oz (UK) | 454609/16000000000 |  |
| `[cup_m]` | cup (metric) | 3/12500 |  |
| `[tbs_m]` | tbsp (metric) | 3/200000 |  |
| `[tsp_m]` | tsp (metric) | 1/200000 |  |
| `[cft_i]` | ft³ | 55306341/1953125000 |  |

### duration

Mo and a are UCUM's mean Julian month (30.4375 d) and year (365.25 d), never a calendar month or year.

| Code | Symbol | Factor to `s` | Offset |
| --- | --- | --- | --- |
| `s` | s | 1 |  |
| `ms` | ms | 1/1000 |  |
| `us` | μs | 1/1000000 |  |
| `min` | min | 60 |  |
| `h` | h | 3600 |  |
| `d` | d | 86400 |  |
| `wk` | wk | 604800 |  |
| `mo` | mo | 2629800 |  |
| `a` | a | 31557600 |  |

### temperature

An absolute temperature: Cel and [degF] convert with their offsets.

| Code | Symbol | Factor to `K` | Offset |
| --- | --- | --- | --- |
| `K` | K | 1 |  |
| `Cel` | °C | 1 | 5463/20 |
| `[degF]` | °F | 5/9 | 45967/180 |

### temperature-difference

A difference or deviation: the same codes, the offsets suppressed.

| Code | Symbol | Factor to `K` | Offset |
| --- | --- | --- | --- |
| `K` | K | 1 |  |
| `Cel` | °C | 1 |  |
| `[degF]` | °F | 5/9 |  |

### energy

Cal is the thermochemical calorie (4.184 J); [Cal] is the nutrition Calorie, a kcal.

| Code | Symbol | Factor to `J` | Offset |
| --- | --- | --- | --- |
| `J` | J | 1 |  |
| `kJ` | kJ | 1000 |  |
| `MJ` | MJ | 1000000 |  |
| `cal` | cal | 523/125 |  |
| `kcal` | kcal | 4184 |  |
| `[Cal]` | Cal | 4184 |  |
| `W.h` | Wh | 3600 |  |
| `kW.h` | kWh | 3600000 |  |
| `eV` | eV | 801088317/5000000000000000000000000000 |  |
| `[Btu_IT]` | Btu | 52752792631/50000000 |  |

### power

| Code | Symbol | Factor to `W` | Offset |
| --- | --- | --- | --- |
| `W` | W | 1 |  |
| `mW` | mW | 1/1000 |  |
| `kW` | kW | 1000 |  |
| `MW` | MW | 1000000 |  |
| `[HP]` | hp | 37284993579113511/50000000000000 |  |
| `kcal/d` | kcal/d | 523/10800 |  |
| `kcal/h` | kcal/h | 523/450 |  |
| `[Btu_IT]/h` | Btu/h | 52752792631/180000000000 |  |

### apparent-power

Volt-amperes: the same factor as the watt and never converted into it.

| Code | Symbol | Factor to `V.A` | Offset |
| --- | --- | --- | --- |
| `V.A` | VA | 1 |  |

### reactive-power

Var: UCUM has no code, so it is the annotated volt-ampere.

| Code | Symbol | Factor to `V.A{reactive}` | Offset |
| --- | --- | --- | --- |
| `V.A{reactive}` | var | 1 |  |

### pressure

| Code | Symbol | Factor to `Pa` | Offset |
| --- | --- | --- | --- |
| `Pa` | Pa | 1 |  |
| `hPa` | hPa | 100 |  |
| `kPa` | kPa | 1000 |  |
| `bar` | bar | 100000 |  |
| `mbar` | mbar | 100 |  |
| `atm` | atm | 101325 |  |
| `mm[Hg]` | mmHg | 66661/500 |  |
| `cm[H2O]` | cmH₂O | 196133/2000 |  |
| `[psi]` | psi | 8896443230521/1290320000 |  |
| `[in_i'Hg]` | inHg | 8465947/2500 |  |

### speed

| Code | Symbol | Factor to `m/s` | Offset |
| --- | --- | --- | --- |
| `m/s` | m/s | 1 |  |
| `km/h` | km/h | 5/18 |  |
| `[mi_i]/h` | mph | 1397/3125 |  |
| `[kn_i]` | kn | 463/900 |  |
| `[ft_i]/s` | ft/s | 381/1250 |  |

### pace

The reciprocal of a speed: a mean pace is total time over total distance.

| Code | Symbol | Factor to `s/m` | Offset |
| --- | --- | --- | --- |
| `s/m` | s/m | 1 |  |
| `min/km` | min/km | 3/50 |  |
| `s/km` | s/km | 1/1000 |  |
| `min/[mi_i]` | min/mi | 625/16764 |  |
| `s/(100.m)` | s/100 m | 1/100 |  |
| `s/(100.[yd_i])` | s/100 yd | 25/2286 |  |
| `s/(500.m)` | s/500 m | 1/500 |  |

### acceleration

| Code | Symbol | Factor to `m/s2` | Offset |
| --- | --- | --- | --- |
| `m/s2` | m/s² | 1 |  |
| `[g]` | gn | 196133/20000 |  |

### frequency

| Code | Symbol | Factor to `Hz` | Offset |
| --- | --- | --- | --- |
| `Hz` | Hz | 1 |  |
| `kHz` | kHz | 1000 |  |

### rate

Events per time; an annotated rate ({beats}/min) converts only to the same annotation.

| Code | Symbol | Factor to `/s` | Offset |
| --- | --- | --- | --- |
| `/s` | /s | 1 |  |
| `/min` | /min | 1/60 |  |
| `/h` | /h | 1/3600 |  |
| `/d` | /d | 1/86400 |  |
| `/wk` | /wk | 1/604800 |  |
| `{count}/min` | count/min | 1/60 |  |

### count

A count of things, named by an annotation alone ({steps}); two annotations never convert.

| Code | Symbol | Factor to `1` | Offset |
| --- | --- | --- | --- |
| `{count}` | count | 1 |  |
| `1` |  | 1 |  |

### fraction

A part of a whole; mmol/mol (HbA1c IFCC) never converts to an NGSP %, which a metric says.

| Code | Symbol | Factor to `1` | Offset |
| --- | --- | --- | --- |
| `1` |  | 1 |  |
| `%` | % | 1/100 |  |
| `[ppth]` | ‰ | 1/1000 |  |
| `[ppm]` | ppm | 1/1000000 |  |
| `[ppb]` | ppb | 1/1000000000 |  |
| `mmol/mol` | mmol/mol | 1/1000 |  |

### amount-of-substance

| Code | Symbol | Factor to `mol` | Offset |
| --- | --- | --- | --- |
| `mol` | mol | 1 |  |
| `mmol` | mmol | 1/1000 |  |
| `umol` | μmol | 1/1000000 |  |

### amount-concentration

| Code | Symbol | Factor to `mol/m3` | Offset |
| --- | --- | --- | --- |
| `mol/L` | mol/L | 1000 |  |
| `mmol/L` | mmol/L | 1 |  |
| `umol/L` | μmol/L | 1/1000 |  |
| `nmol/L` | nmol/L | 1/1000000 |  |
| `pmol/L` | pmol/L | 1/1000000000 |  |

### mass-concentration

| Code | Symbol | Factor to `kg/m3` | Offset |
| --- | --- | --- | --- |
| `g/L` | g/L | 1 |  |
| `g/dL` | g/dL | 10 |  |
| `mg/dL` | mg/dL | 1/100 |  |
| `mg/L` | mg/L | 1/1000 |  |
| `ug/L` | μg/L | 1/1000000 |  |
| `ng/mL` | ng/mL | 1/1000000 |  |
| `pg/mL` | pg/mL | 1/1000000000 |  |
| `ug/dL` | μg/dL | 1/100000 |  |
| `g/m3` | g/m³ | 1/1000 |  |
| `mg/m3` | mg/m³ | 1/1000000 |  |
| `ug/m3` | μg/m³ | 1/1000000000 |  |
| `kg/m3` | kg/m³ | 1 |  |

### number-concentration

Things counted per volume, such as blood cells: 10*9/L and 10*3/uL are one value.

| Code | Symbol | Factor to `/m3` | Offset |
| --- | --- | --- | --- |
| `/L` | /L | 1000 |  |
| `/uL` | /μL | 1000000000 |  |
| `10*3/uL` | 10³/μL | 1000000000000 |  |
| `10*6/uL` | 10⁶/μL | 1000000000000000 |  |
| `10*9/L` | 10⁹/L | 1000000000000 |  |
| `10*12/L` | 10¹²/L | 1000000000000000 |  |

### catalytic-concentration

Enzyme activity per volume: U is 1 umol/min, a proper unit.

| Code | Symbol | Factor to `mol/(s.m3)` | Offset |
| --- | --- | --- | --- |
| `U/L` | U/L | 1/60000 |  |

### catalytic-activity

| Code | Symbol | Factor to `mol/s` | Offset |
| --- | --- | --- | --- |
| `kat` | kat | 1 |  |
| `U` | U | 1/60000000 |  |

### arbitrary

Defined by an assay: converts only to the same atom and annotation under another metric prefix.

| Code | Symbol | Factor to `base` | Offset |
| --- | --- | --- | --- |
| `[IU]` | IU | none |  |
| `[IU]/L` | IU/L | none |  |
| `m[IU]/L` | mIU/L | none |  |
| `[IU]/mL` | IU/mL | none |  |
| `[arb'U]` | arb. U | none |  |

### illuminance

| Code | Symbol | Factor to `lx` | Offset |
| --- | --- | --- | --- |
| `lx` | lx | 1 |  |

### luminous-flux

| Code | Symbol | Factor to `lm` | Offset |
| --- | --- | --- | --- |
| `lm` | lm | 1 |  |

### luminous-intensity

| Code | Symbol | Factor to `cd` | Offset |
| --- | --- | --- | --- |
| `cd` | cd | 1 |  |

### luminance

| Code | Symbol | Factor to `cd/m2` | Offset |
| --- | --- | --- | --- |
| `cd/m2` | cd/m² | 1 |  |

### irradiance

| Code | Symbol | Factor to `W/m2` | Offset |
| --- | --- | --- | --- |
| `W/m2` | W/m² | 1 |  |

### level

A ratio in decibels against no stated reference: never converted, never summed.

| Code | Symbol | Factor to `dB` | Offset |
| --- | --- | --- | --- |
| `dB` | dB | none |  |

### sound-pressure-level

20 lg(p/20 uPa) dB.

| Code | Symbol | Factor to `dB[SPL]` | Offset |
| --- | --- | --- | --- |
| `dB[SPL]` | dB SPL | none |  |

### power-level

DB[W] is 10 lg(P/1 W) dB; dBm is 10 lg(P/1 mW) dB, a substrate code because UCUM has none.

| Code | Symbol | Factor to `dB[W]` | Offset |
| --- | --- | --- | --- |
| `dB[W]` | dBW | none |  |
| `dBm` | dBm | none |  |

### acidity

| Code | Symbol | Factor to `[pH]` | Offset |
| --- | --- | --- | --- |
| `[pH]` | pH | none |  |

### electric-potential

| Code | Symbol | Factor to `V` | Offset |
| --- | --- | --- | --- |
| `V` | V | 1 |  |
| `mV` | mV | 1/1000 |  |

### electric-current

| Code | Symbol | Factor to `A` | Offset |
| --- | --- | --- | --- |
| `A` | A | 1 |  |
| `mA` | mA | 1/1000 |  |

### electric-resistance

| Code | Symbol | Factor to `Ohm` | Offset |
| --- | --- | --- | --- |
| `Ohm` | Ω | 1 |  |
| `kOhm` | kΩ | 1000 |  |

### electric-charge

| Code | Symbol | Factor to `C` | Offset |
| --- | --- | --- | --- |
| `C` | C | 1 |  |
| `A.h` | Ah | 3600 |  |
| `mA.h` | mAh | 18/5 |  |

### electric-conductivity

| Code | Symbol | Factor to `S/m` | Offset |
| --- | --- | --- | --- |
| `uS/cm` | μS/cm | 1/10000 |  |

### information

| Code | Symbol | Factor to `bit` | Offset |
| --- | --- | --- | --- |
| `bit` | bit | 1 |  |
| `By` | B | 8 |  |
| `kBy` | kB | 8000 |  |
| `MBy` | MB | 8000000 |  |
| `GBy` | GB | 8000000000 |  |
| `KiBy` | KiB | 8192 |  |
| `MiBy` | MiB | 8388608 |  |
| `GiBy` | GiB | 8589934592 |  |

### data-rate

| Code | Symbol | Factor to `bit/s` | Offset |
| --- | --- | --- | --- |
| `bit/s` | bit/s | 1 |  |
| `kbit/s` | kbit/s | 1000 |  |
| `Mbit/s` | Mbit/s | 1000000 |  |
| `By/s` | B/s | 8 |  |

### angle

Rad is 180/pi deg, which no rational number states, so rad converts to nothing but itself.

| Code | Symbol | Factor to `deg` | Offset |
| --- | --- | --- | --- |
| `deg` | ° | 1 |  |
| `'` | ′ | 1/60 |  |
| `''` | ″ | 1/3600 |  |
| `rad` | rad | none |  |

### volume-flow

| Code | Symbol | Factor to `m3/s` | Offset |
| --- | --- | --- | --- |
| `L/min` | L/min | 1/60000 |  |
| `L/h` | L/h | 1/3600000 |  |
| `mL/min` | mL/min | 1/60000000 |  |
| `m3/h` | m³/h | 1/3600 |  |
| `[gal_us]/min` | gal/min | 157725491/2500000000000 |  |

### mass-flow

| Code | Symbol | Factor to `kg/s` | Offset |
| --- | --- | --- | --- |
| `g/s` | g/s | 1/1000 |  |

### force

| Code | Symbol | Factor to `N` | Offset |
| --- | --- | --- | --- |
| `N` | N | 1 |  |
| `[lbf_av]` | lbf | 8896443230521/2000000000000 |  |

### torque

The same dimension as energy, and never converted into it.

| Code | Symbol | Factor to `N.m` | Offset |
| --- | --- | --- | --- |
| `N.m` | N·m | 1 |  |
| `[lbf_av].[ft_i]` | lbf·ft | 3389544870828501/2500000000000000 |  |

### activity-concentration

| Code | Symbol | Factor to `Bq/m3` | Offset |
| --- | --- | --- | --- |
| `Bq/m3` | Bq/m³ | 1 |  |
| `pCi/L` | pCi/L | 37 |  |

### dose-equivalent-rate

| Code | Symbol | Factor to `Sv/s` | Offset |
| --- | --- | --- | --- |
| `uSv/h` | μSv/h | 1/3600000000 |  |

### body-mass-index

| Code | Symbol | Factor to `kg/m2` | Offset |
| --- | --- | --- | --- |
| `kg/m2` | kg/m² | 1 |  |

### oxygen-uptake-rate

One MET is 3.5 mL/(kg.min).

| Code | Symbol | Factor to `m3/(kg.s)` | Offset |
| --- | --- | --- | --- |
| `mL/(kg.min)` | mL/(kg·min) | 1/60000000 |  |
| `[MET]` | MET | 7/120000000 |  |

### specific-power

| Code | Symbol | Factor to `W/kg` | Offset |
| --- | --- | --- | --- |
| `W/kg` | W/kg | 1 |  |

### specific-energy

| Code | Symbol | Factor to `J/kg` | Offset |
| --- | --- | --- | --- |
| `kcal/kg` | kcal/kg | 4184 |  |

### energy-per-distance

| Code | Symbol | Factor to `J/m` | Offset |
| --- | --- | --- | --- |
| `kW.h/(100.km)` | kWh/100 km | 36 |  |
| `W.h/km` | Wh/km | 18/5 |  |

### fuel-consumption

The reciprocal of a fuel economy, never converted into one.

| Code | Symbol | Factor to `m3/m` | Offset |
| --- | --- | --- | --- |
| `L/(100.km)` | L/100 km | 1/100000000 |  |

### fuel-economy

| Code | Symbol | Factor to `m/m3` | Offset |
| --- | --- | --- | --- |
| `[mi_i]/[gal_us]` | mpg | 48000000000/112903 |  |
| `km/L` | km/L | 1000000 |  |

### precipitation-rate

| Code | Symbol | Factor to `m/s` | Offset |
| --- | --- | --- | --- |
| `mm/h` | mm/h | 1/3600000 |  |
| `[in_i]/h` | in/h | 127/18000000 |  |

## Aliases

A writer may send these spellings; the stored value always holds the code. Both
micro signs (U+00B5 and U+03BC) read as `u` and the superscript digits ² and ³
as `2` and `3` anywhere in a code, so `μg/m³` is stored as `ug/m3`.

| Alias | Code |
| --- | --- |
| `Cal` | `[Cal]` |
| `IU` | `[IU]` |
| `VA` | `V.A` |
| `Wh` | `W.h` |
| `bpm` | `{beats}/min` |
| `cmAq` | `cm[H2O]` |
| `count` | `{count}` |
| `count/min` | `{count}/min` |
| `dBASPL` | `dB[SPL]` |
| `degC` | `Cel` |
| `degF` | `[degF]` |
| `hr` | `h` |
| `in` | `[in_i]` |
| `inHg` | `[in_i'Hg]` |
| `kWh` | `kW.h` |
| `km/hr` | `km/h` |
| `kmph` | `km/h` |
| `lb` | `[lb_av]` |
| `mL/(kg*min)` | `mL/(kg.min)` |
| `mcg` | `ug` |
| `mi` | `[mi_i]` |
| `mi/hr` | `[mi_i]/h` |
| `ml/(kg·min)` | `mL/(kg.min)` |
| `mmHg` | `mm[Hg]` |
| `mmol<180.15588000005408>/L` | `mmol/L` |
| `mmol<180.1558800000541>/L` | `mmol/L` |
| `oz` | `[oz_av]` |
| `ppb` | `[ppb]` |
| `ppm` | `[ppm]` |
| `rpm` | `{rev}/min` |
| `var` | `V.A{reactive}` |
| `yd` | `[yd_i]` |
| `°C` | `Cel` |
| `°F` | `[degF]` |
| `℃` | `Cel` |
| `℉` | `[degF]` |
