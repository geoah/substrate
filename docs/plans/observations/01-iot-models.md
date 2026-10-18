# How IoT, home automation and sensor systems model observations

Research slice 01 for the v2 question: should the health model (`metric`,
`observation` with a `form` of instant, interval, counter or summary, a packed
`trace`, a `source`, a `quantity` value type with UCUM units, a derived samples
index with 15-minute rollups, an aggregate read) widen to every observed
quantity. Researched 2026-10-08.

How to read the provenance marks:

- **checked (source)**: read in the system's own source code, schema or spec
  on 2026-10-08. Home Assistant facts come from the `dev` branch of
  `home-assistant/core`, downloaded and grepped. Copies are under
  `/tmp/health-research/v2/raw-iot/`.
- **checked (docs)**: read on the vendor's documentation page.
- **checked (secondary)**: read in an open-source client or decoder, because
  the primary document is behind a login or renders only with JavaScript.
- **unverified (recalled)**: from memory, not confirmed on 2026-10-08.

Three facts changed recently, and older material gets them wrong:

- Home Assistant now has a fifth state class, `measurement_angle`, and a
  circular mean for wind direction (`StatisticMeanType.CIRCULAR`).
- Smartcar deprecates its v2 Vehicles API on 2026-12-01 in favour of v3
  "signals".
- In May 2026 the US EPA AQI technical document (EPA-403/B-26-003) kept the
  2024 PM2.5 breakpoints, so "Good" is 0.0 to 9.0 ug/m3, not 0.0 to 12.0.

## Contents

1. Home Assistant in detail
2. The observation standards: OGC SensorThings, OMS (ISO 19156), W3C SOSA/SSN
3. Device protocols: Matter, Zigbee, HomeKit, Google SDM, Z-Wave, Sparkplug B, HA MQTT discovery
4. Buildings and energy
5. Vehicles, weather and environment
6. Time-series conventions and what the Home Assistant exporters lose
7. Deliverable (a): the five shapes, side by side
8. Deliverable (b): the catalogue of measured quantities
9. Deliverable (c): categorical observations and time in state
10. Deliverable (d): what a shared model must absorb that health data does not need
11. Deliverable (e): does `metric / observation / trace / source / quantity` fit?

---

## 1. Home Assistant

Home Assistant (HA) has the most worked-out model of this whole field, and the
most homes run it, so this section is the longest.

### 1.1 The entity state

Every entity holds one current **state object**. Source: checked (docs),
https://www.home-assistant.io/docs/configuration/state_object/.

| Field | Meaning |
| --- | --- |
| `entity_id` | `<domain>.<object_id>`, for example `sensor.kitchen_temperature`. This is the identity, and a user can rename it. |
| `state` | The value, always a **string**: `21.5`, `on`, `home`, `unavailable`, `unknown`. Stored as `String(MAX_LENGTH_STATE_STATE)`, which is 255 characters (checked (source), `recorder/db_schema.py`). |
| `attributes` | A free map: `unit_of_measurement`, `device_class`, `state_class`, `friendly_name`, `last_reset`, `options` (the allowed values of an enum sensor), plus anything the integration adds. |
| `last_changed` | "When the state last changed, in UTC. Not updated when only the attributes change." |
| `last_updated` | "When the state or the attributes last changed." |
| `last_reported` | "When the entity last reported its state ... Updated even when neither the state nor the attributes changed." Added in 2024. |
| `context` | Links the states and events that belong together, such as an automation and what it caused. |

Provenance has three levels, held in registries:

- The **entity registry** keeps a stable `unique_id`, written by the
  integration, separate from the renameable `entity_id`.
- The **device registry** keeps `identifiers`, `manufacturer`, `model`,
  `sw_version` and `via_device`.
- **Areas** (rooms) attach to a device, and an entity may override its
  device's area.

The registry model is unverified (recalled) as a whole, but the MQTT discovery
keys in section 3.7 confirm the device fields.

### 1.2 `device_class`, `state_class` and units

`SensorStateClass` has four values. Source: checked (source),
`homeassistant/components/sensor/const.py`, and checked (docs),
https://developers.home-assistant.io/docs/core/entity/sensor/.

| `state_class` | Meaning (quoted) | Long-term statistics kept |
| --- | --- | --- |
| `measurement` | "a measurement in present time, not a historical aggregation such as statistics or a prediction of the future" | `mean`, `min`, `max`; the mean is time-weighted |
| `measurement_angle` | the same, "for angles measured in degrees", and only the unit `°` is allowed | a **circular** time-weighted mean, with `mean_weight` |
| `total` | "a total amount that can both increase and decrease, for example, a net energy meter" | `state` and `sum` (see 1.4) |
| `total_increasing` | "a monotonically increasing positive total which periodically restarts counting from 0" | `state` and `sum` |

`device_class` names the quantity, and a closed table fixes which units and
state classes each device class may use. Every device class with its units
and state classes, from `DEVICE_CLASS_UNITS` and `DEVICE_CLASS_STATE_CLASSES`
in `sensor/const.py` (checked (source)), is in section 8.

The unit is a free display string chosen from an HA enum, such as `°C`,
`μg/m³`, `m³` or `kWh`. It is not UCUM. HA converts within a unit class
through `UNIT_CONVERTERS`. Each unit class has a converter (for example
`PressureConverter`, `EnergyConverter`, `TemperatureConverter`) and a target
unit from the user's unit system or the entity's
`suggested_unit_of_measurement`. Conversion changes the displayed state, and
the recorder stores the converted value (unverified (recalled) for "stores
converted"). The statistics read takes a `units` map per unit class and
converts on read (checked (source), `recorder/websocket_api.py`, `UNIT_SCHEMA`).

Precision is a display hint. `suggested_display_precision` sets the number of
decimals, and `UNITS_PRECISION` defines a default per device class: temperature
0.1 K, power 1 W, energy 1 Wh, wind speed 1 mm/s (checked (source)).

Non-numeric sensors use the device classes `enum` (with `options`),
`timestamp` and `date`. Two-valued states have their own domain,
`binary_sensor`, whose device classes are:

> battery, battery_charging, carbon_monoxide, cold, connectivity, door,
> garage_door, gas, glass_break, heat, light, lock, moisture, motion, moving,
> occupancy, opening, plug, power, presence, problem, running, safety, smoke,
> sound, tamper, update, vibration, window

Its states are `on` and `off`; the device class gives them their meaning
(`door` on means open). Source: checked (source),
`binary_sensor/const.py`.

### 1.3 The recorder tables

Source: checked (source), `recorder/db_schema.py`.

- **`states_meta`** holds `metadata_id` and `entity_id`. It interns entity ids.
- **`states`** holds one row per state write:
  - `state_id`, `metadata_id`, `state` (string)
  - `last_changed_ts`, `last_updated_ts`, `last_reported_ts` (float epoch
    seconds)
  - `old_state_id`, a pointer to the previous row, which makes the rows a
    linked list
  - `attributes_id`
  - context ids and `origin_idx`
- **`state_attributes`** holds `attributes_id`, `hash` and `shared_attrs`.
  Attribute JSON is deduplicated by hash.
- **`statistics_meta`** holds one row per statistic:
  - `id` and `statistic_id`. This is either an entity id, or
    `<source>:<name>` for external statistics, validated by
    `^(?!.+__)(?!_)[\da-z_]+(?<!_):(?!_)[\da-z_]+(?<!_)$`.
  - `source` (`recorder` or an integration domain)
  - `unit_of_measurement` and `unit_class`
  - `has_sum`
  - `mean_type`: `NONE`, `ARITHMETIC` or `CIRCULAR`. It replaces `has_mean`,
    which the code comment says "will be removed in 2026.4".
  - `name`
- **`statistics`** (hourly) and **`statistics_short_term`** (5-minute) share
  one column set:
  - `metadata_id`, `created_ts`, `start_ts`
  - `mean`, `mean_weight`, `min`, `max`
  - `last_reset_ts`, `state`, `sum`

  All values are `DOUBLE`.

### 1.4 How statistics are compiled, and how counters reset

Source: checked (source), `sensor/recorder.py` (`compile_statistics`,
`reset_detected`, `_time_weighted_arithmetic_mean`,
`_time_weighted_circular_mean`).

- **`measurement`**: every 5 minutes the recorder computes a time-weighted mean
  of the states in the window. A state is held until the next one ("no
  interpolation of values between state changes"), and the state current at
  the window start counts too. It also records min and max. An hourly row is
  built from the twelve 5-minute rows.
- **`measurement_angle`**: the same, but the mean is a circular mean, with a
  `mean_weight` so that hourly rows can merge the 5-minute rows correctly.
- **`total` and `total_increasing`**: the rows store `state` (the reading at
  the end of the period) and `sum`. Per the docs, `sum` is the "grand total
  since statistics for the sensor was first compiled, offset by the sensor's
  first valid state". The code keeps a running `_sum` and adds
  `new_state - old_state` for every state change.
  - **Resets on `total`**: a reset is only what `last_reset` says. "When
    `last_reset` changes, the zero-point will be set to 0." Without a
    `last_reset`, the first value is the zero point.
  - **Resets on `total_increasing`**: `reset_detected()` returns true when
    `fstate < 0.9 * previous_fstate`. A dip of less than 10% only logs a
    warning (`warn_dip`), and a negative value is refused. After a reset the
    new reading is the increase since zero, so `_sum` grows by the new value.
  - **What a reader gets**: an energy dashboard reads `sum` differences
    between hours (the `change` statistic type) and never touches raw
    readings.
- **External statistics**: an integration can write hourly statistics rows
  directly, with no entity behind them, through `async_add_external_statistics`
  or the `recorder/import_statistics` command. The utility integration
  `opower` writes `opower:<id>_energy_consumption`, `_energy_return`,
  `_energy_cost` and `_energy_compensation` with
  `mean_type=StatisticMeanType.NONE, has_sum=True` (checked (source),
  `opower/coordinator.py`). This is how a utility's 15-minute or hourly
  interval data enters HA. It lands as a summary row, never as raw states.

### 1.5 Retention

- **Default retention**: `purge_keep_days` defaults to **10**, and the recorder
  commits every 5 s (`DEFAULT_COMMIT_INTERVAL = 5`) (checked (source),
  `recorder/__init__.py`).
- **What is purged**: per the docs, "The `states` and `statistics_short_term`
  tables are automatically purged after a predefined period (default is 10
  days). The long-term statistics table is never purged" (checked (docs),
  https://data.home-assistant.io/docs/statistics/).
- **The result**: raw history is a 10-day window, and the forever record is one
  row per sensor per hour with mean, min and max, or state and sum.

### 1.6 Read APIs

All checked (source).

- **REST `GET /api/history/period/<start>`**: query parameters
  `filter_entity_id` (required), `end_time`, `minimal_response`,
  `no_attributes`, `significant_changes_only`. Source: `history/__init__.py`.
- **WebSocket `history/history_during_period` and `history/stream`**:
  `start_time`, `end_time`, `entity_ids`, `include_start_time_state` (default
  true, so a step function can be drawn from the window start),
  `significant_changes_only` (default true), `minimal_response`,
  `no_attributes`.
- **WebSocket `recorder/statistics_during_period`**:
  - parameters `start_time`, `end_time`, `statistic_ids`, `period`, `units`,
    `types`
  - `period` takes `5minute`, `hour`, `day`, `week`, `month` or `year`
  - `types` is a subset of `change`, `last_reset`, `max`, `mean`, `min`,
    `state`, `sum`
  - day, week and month buckets are reduced from hourly rows in the server's
    **local time zone** (`dt_util.get_default_time_zone()` in
    `recorder/statistics.py`)
- **Other recorder commands**: `recorder/statistic_during_period` (one
  statistic over an arbitrary window, `types` among `max`, `mean`, `min`,
  `change`), `recorder/list_statistic_ids` (filter `statistic_type` of `sum`
  or `mean`), `recorder/import_statistics`, `recorder/change_statistics_unit`,
  `recorder/update_statistics_metadata`.

### 1.7 Helpers that derive values

Each of these is a separate entity HA computes from another. Each one is a
pattern a shared model has to express. All checked (source).

- **`history_stats`** computes **time in state**. It takes `state` (one or
  more), `start`, `end`, `duration` and `min_state_duration`, and outputs one
  of three types: `time` (hours), `ratio` (%) or `count` (number of
  transitions).
- **`utility_meter`** turns a cumulative counter into a resetting one. `cycle`
  takes `quarter-hourly`, `hourly`, `daily`, `weekly`, `monthly`,
  `bimonthly`, `quarterly` or `yearly`, or a `cron` pattern. It also supports
  `tariffs`, and `reset` and `calibrate` services.
- **`integration`** (Riemann sum) turns power into energy. `method` is
  `trapezoidal`, `left` or `right`, with `unit_prefix`, `unit_time` and
  `max_sub_interval`.

---

## 2. The observation standards

### 2.1 OGC SensorThings API 1.1 (STA)

Source: checked (docs), https://docs.ogc.org/is/18-088/18-088.html.

| Entity | Properties | Links |
| --- | --- | --- |
| `Thing` | `name`, `description`, `properties` | many `Location`, many `Datastream`, many `HistoricalLocation` |
| `Location` | `name`, `description`, `encodingType`, `location` (GeoJSON), `properties` | many `Thing` |
| `HistoricalLocation` | `time` | one `Thing`, many `Location`; this is how a moving thing's track is kept |
| `Datastream` | `name`, `description`, `observationType` (URI), `unitOfMeasurement` (`{name, symbol, definition}`), `observedArea`, `phenomenonTime` (an interval), `resultTime` (an interval), `properties` | exactly one `Thing`, one `Sensor`, one `ObservedProperty` |
| `Sensor` | `name`, `description`, `encodingType`, `metadata` (for example a SensorML document) | many `Datastream` |
| `ObservedProperty` | `name`, `definition` (URI), `description` | many `Datastream` |
| `Observation` | `phenomenonTime` (an instant **or a period**), `resultTime`, `result` ("may be any type"), `resultQuality`, `validTime`, `parameters` | one `Datastream`, one `FeatureOfInterest` |
| `FeatureOfInterest` | `name`, `description`, `encodingType`, `feature` (GeoJSON) | many `Observation`; when a client omits it, the server generates one from the `Thing`'s location |
| `MultiDatastream` (extension) | `multiObservationDataTypes[]`, `unitOfMeasurements[]`; one observation's `result` is an array | the same three links |

`observationType` values come from O&M:

- `OM_Measurement`: a number with a unit
- `OM_CategoryObservation`: a categorical value
- `OM_CountObservation`: an integer count
- `OM_TruthObservation`: a boolean
- `OM_Observation`: anything else

The **Datastream** is the key idea. It is one stream of observations of one
property, by one sensor, on one thing, in one unit, and it is the unit of
storage and query. The units are a free triple of name, symbol and a
definition URI, usually a UCUM or QUDT URI. STA also has a `dataArray`
encoding for bulk reads and writes.

STA 2.0 was out for public comment from 2025-12-19 to 2026-01-18. It aligns
with OData 4.01 and the 2023 OMS model (checked (docs),
https://www.ogc.org/requests/ogc-seeks-public-comment-on-proposed-ogc-sensorthings-api-version-2-0/).
I did not read the draft's data model changes. That it merges
MultiDatastream into Datastream and adds a Deployment entity is unverified
(recalled).

### 2.2 OMS, ISO 19156:2023

Source: checked (docs), search summaries of https://docs.ogc.org/as/20-082r4/20-082r4.html
(OGC Abstract Specification Topic 20). The full text was not read.

An observation has:

- `phenomenonTime`, `resultTime` and `validTime`
- a `procedure` and an `observer`
- an `observedProperty`
- a `result`
- a feature of interest, which since 2023 comes in two kinds:
  - the **proximate feature of interest** is what the sensor actually touched,
    such as a sample or a room's air
  - the **ultimate feature of interest** is what the user cares about, such as
    the house, the river or the patient

`ObservableProperty` as a class name is deprecated in the 2023 edition in
favour of the observed property as a role.

### 2.3 W3C SOSA/SSN

Source: checked (docs), https://www.w3.org/TR/vocab-ssn/ (2017
Recommendation) and https://www.w3.org/TR/vocab-ssn-2023/. A 2023 Edition
working draft is dated 2026-09-04.

SOSA classes:

| Class | Definition |
| --- | --- |
| `sosa:Observation` | "Act of carrying out an (Observation) Procedure to estimate or calculate a value of a property" |
| `sosa:Sensor` | "Device, agent (including humans), or software (simulation)" |
| `sosa:ObservableProperty` | "An observable quality ... of a FeatureOfInterest" |
| `sosa:FeatureOfInterest` | "The thing whose property is being estimated" |
| `sosa:Result` | the result of an observation |
| `sosa:Sample` | "representative of a FeatureOfInterest" |
| `sosa:Platform` | hosts sensors |
| `sosa:Procedure` | how an observation is made |
| `sosa:Actuation` | changes the state of the world |

The properties are `madeBySensor`, `observedProperty`,
`hasFeatureOfInterest`, `hasResult`, `hasSimpleResult`, `resultTime`,
`phenomenonTime`, `usedProcedure`, `isHostedBy` and `isSampleOf`.

SOSA mandates no unit vocabulary. In practice units come from QUDT
(`qudt:numericValue`, `qudt:unit`) or from OM.

The `ssn-system` module describes a sensor's capability: `Accuracy`,
`Precision`, `Resolution`, `MeasurementRange`, `Frequency`, `Latency`,
`DetectionLimit`, `Drift` and `BatteryLifetime`.

The 2023 edition adds:

- `ObservationCollection`: "members share a common value for one or more
  property", which is a Datastream by another name
- `sosa:hasUltimateFeatureOfInterest`
- `hasFeatureOfInterest` on a `Deployment`

### 2.4 What the standards separate

All three keep four things apart:

1. **The property**: "air temperature". It is an `ObservedProperty` (STA) or an
   `ObservableProperty` (SOSA). This corresponds to the health plan's `metric`.
2. **The sensor**: "the Aqara sensor serial X". It is a `Sensor` (STA) or a
   `sosa:Sensor` hosted on a `Platform`. This corresponds to the health plan's
   `source`.
3. **The thing observed**: "the kitchen", "the car", "the tomato plant".
   - In STA this is the `FeatureOfInterest` per observation, and the `Thing`
     that owns the datastream.
   - In OMS 2023 it is split into proximate and ultimate.
   - **The health plan has no equivalent.** Its subject is always the user,
     implicitly.
4. **The stream**: the Datastream, or the SOSA 2023 ObservationCollection,
   which binds 1 to 3 together with a unit. The health plan's `trace` is a
   time-sliced piece of such a stream, keyed by (metric, source, period). It
   has no persistent stream record.

They also keep two times apart:

- `phenomenonTime` is when the property had the value. It may be a period.
- `resultTime` is when the result became available. For a forecast it comes
  before the phenomenon. For a lab result it comes after.

---

## 3. Device protocols

Section sources: a research agent read the connectedhomeip XML and the CSA
Matter specification PDFs (Matter 1.0 and 1.3 application cluster, 1.0 and
1.4 core), a mirror of the ZCL revision 8 PDF, HAP-NodeJS, the Google Device
Access docs, zwave-js source and the Eclipse Sparkplug specification and
proto. Every row below was checked unless marked.

Short names used in this section:

- **MXML**: https://github.com/project-chip/connectedhomeip/tree/master/src/app/zap-templates/zcl/data-model/chip/
- **MACL13**: https://csa-iot.org/wp-content/uploads/2024/05/matter-application-cluster-specification.pdf
- **MCORE**: https://csa-iot.org/wp-content/uploads/2022/11/22-27349-001_Matter-1.0-Core-Specification.pdf
- **ZCL8**: https://etc.athom.com/zigbee_cluster_specification.pdf, a mirror of
  ZCL 07-5123 revision 8

### 3.1 Matter

**Instant readings** are scaled integers or floats on a cluster attribute
named `MeasuredValue`, with optional `MinMeasuredValue`, `MaxMeasuredValue`
and `Tolerance`. Every one is nullable, and null means unknown.

| Cluster | Encoding |
| --- | --- |
| Temperature Measurement `0x0402` | int16, `100 x °C`, from -273.15 to 327.67 °C, resolution 0.01 °C |
| Relative Humidity `0x0405` | uint16, `100 x %`, from 0 to 10000 |
| Illuminance `0x0400` | uint16, `10000 x log10(lux) + 1`, covering 1 lx to 3.576 Mlx; 0 means too dark to measure. The value is logarithmic, so it is not linear in lux. |
| Pressure `0x0403` | int16, `10 x kPa`; an extended `ScaledValue = 10^Scale x Pa`, where `Scale` is an int8 |
| Flow `0x0404` | uint16, `10 x m³/h` |
| Concentration family | float32 `MeasuredValue` plus a `MeasurementUnit` enum (PPM, PPB, PPT, MGM3, UGM3, NGM3, PM3, BQM3, DBPM, PCFT) and a `MeasurementMedium` (air, water, soil). Covers CO `0x040C`, CO2 `0x040D`, NO2, O3, PM2.5 `0x042A`, PM1, PM10, TVOC `0x042E`, formaldehyde and radon. |
| Electrical Power Measurement `0x0090` (Matter 1.3) | int64 in **mV, mA, mW, mvar, mVA**: `Voltage`, `RMSVoltage`, `ActiveCurrent`, `ActivePower` (mandatory), `ReactivePower`, `ApparentPower`, `Frequency`, `PowerFactor` |
| Power Source | `BatPercentRemaining` is in **half percent**, 0 to 200; `BatVoltage` is in mV |

**Counters and intervals** come from the Electrical Energy Measurement cluster
`0x0091` (MXML; MACL13 §2.12). It has `CumulativeEnergyImported`,
`CumulativeEnergyExported`, `PeriodicEnergyImported` and
`PeriodicEnergyExported`. Each is an `EnergyMeasurementStruct`:

| Field | Type |
| --- | --- |
| `Energy` | mWh, up to 2^62 |
| `StartTimestamp`, `EndTimestamp` | `epoch_s` |
| `StartSystime`, `EndSystime` | `systime_ms`, milliseconds since boot |
| `ApparentEnergy`, `ReactiveEnergy` | newer additions |

- **Cumulative**: the value is the total at `EndTimestamp`, and
  `StartTimestamp` "SHALL be omitted".
- **Periodic**: the value is the amount between Start and End.
- **Reset**: `CumulativeEnergyReset` records when the cumulative values were
  last zero (`ImportedResetTimestamp` and siblings).
- **No clock**: a device without UTC sends only systime, and a client
  reconciles it later. The protocol itself carries the distinction between a
  counter with a known reset epoch and an interval amount.

**Summaries**: the concentration clusters have `PeakMeasuredValue` and
`AverageMeasuredValue`, each with a window of up to 604800 s (7 days). The
Electrical Power Measurement cluster has `Ranges`, which carry min and max
with their timestamps.

**Categorical states**:

- Boolean State `StateValue` (contact) with a `StateChange` event
- Occupancy Sensing `Occupancy` bitmap with an `OccupancyChanged` event
- concentration `LevelValue` (Unknown, Low, Medium, High, Critical)
- Power Source `BatChargeLevel` (OK, Warning, Critical)

**Updates**:

- A subscription carries `MinIntervalFloor` and `MaxIntervalCeiling` in
  seconds. The device reports on change, gated by the minimum interval, and an
  empty report is a keepalive (MCORE §8.5).
- An attribute with the **Changes Omitted** quality is never pushed.
- An attribute with the **Quieter Reporting** quality reports only under its
  documented conditions. The energy attributes report no more often than once
  a second and no less often than once a minute (MACL13 §2.12.6).

**Identity** is node, endpoint, cluster and attribute.

### 3.2 Zigbee Cluster Library

Matter inherited most of its measurement clusters from ZCL, with the same
scalings:

| Cluster | Encoding |
| --- | --- |
| Temperature `0x0402` | int16 `100 x °C`; `0x8000` means unknown |
| Humidity `0x0405` | uint16 `100 x %` |
| Illuminance `0x0400` | the same log formula as Matter |
| Pressure `0x0403` | `10 x kPa`, with `ScaledValue` |

**Electrical Measurement `0x0B04`** reports `RMSVoltage` (uint16) and
`ActivePower` (int16, where positive means delivered to the premises) in raw
units. A separate multiplier and divisor attribute pair applies to each
family, for example `ACPowerMultiplier` and `ACPowerDivisor`, and 0 is
invalid. The value only has meaning after reading two more attributes.

**Metering `0x0702`** (ZCL8 §10.4):

- **The counter**: `CurrentSummationDelivered` is a **uint48** counter, with
  `CurrentSummationReceived` for export.
- **Demand**: `InstantaneousDemand` (int24).
- **Daily summaries**: `CurrentDayConsumptionDelivered` and
  `PreviousDayConsumptionDelivered`.
- **Units**: `UnitofMeasure` is an enum (0 kWh and kW, 1 m³ and m³/h, 2 ft³,
  3 ccf, 4 US gal, 5 imperial gal, 6 BTU, 7 litres, 0x0C MJ, 0x0D kvar and
  kvarh; 0x80 and up are the same in BCD). A `Multiplier` and `Divisor`
  "must be applied against all summation, consumption and demand values".
- **Resets**: there is no generic summation-reset command.

**Reporting** is configured per attribute with three settings (ZCL8 §2.5.7):

- **minimum interval** (s)
- **maximum interval** (s); 0xffff stops reporting
- **reportable change**, measured from the last reported value

A discrete attribute reports on every change.

### 3.3 Apple HomeKit (HAP)

Source: checked (secondary), HAP-NodeJS `CharacteristicDefinitions.ts`, which
is generated from Apple's metadata.

| Characteristic | Format | Unit | Range, step | Values |
| --- | --- | --- | --- | --- |
| Current Temperature | float | celsius | 0 to 100, step 0.1 (HAP-NodeJS overrides the minimum to -270) | |
| Current Relative Humidity | float | percentage | 0 to 100, step 1 | |
| Current Ambient Light Level | float | lux | 0.0001 to 100000 | |
| Carbon Dioxide Level | float | none declared (ppm by name) | 0 to 100000 | |
| PM2.5 Density | float | none declared (ug/m3 by convention, unverified) | 0 to 1000 | |
| VOC Density | float | none declared | 0 to 1000 | |
| Air Quality | uint8 | | | 0 unknown, 1 excellent, 2 good, 3 fair, 4 inferior, 5 poor |
| Battery Level | uint8 | % | | |
| Status Low Battery | uint8 | | | |
| Contact Sensor State | uint8 | | | 0 detected (closed), 1 not detected (open) |
| Lock Current State | uint8 | | | 0 unsecured, 1 secured, 2 jammed, 3 unknown |
| Occupancy Detected | uint8 | | | 0 not detected, 1 detected |

- **Updates**: the `pr` and `ev` permissions mean a controller reads, or
  subscribes and receives change events. HAP has **no history at all**.
- **Eve's history**: Eve adds a custom history service (UUID
  `E863F007-...`). Its fakegato implementation averages measurements per
  10-minute slot, and logs door, motion and switch states as change entries,
  repeated every 10 minutes. Only Eve's app reads that service (checked
  (secondary), https://github.com/simont77/fakegato-history).

### 3.4 Google Smart Device Management (Nest)

Source: checked (docs), https://developers.google.com/nest/device-access/.

| Trait | Fields |
| --- | --- |
| `sdm.devices.traits.Temperature` | `ambientTemperatureCelsius` (number) |
| `sdm.devices.traits.Humidity` | `ambientHumidityPercent` |
| `ThermostatHvac` | `status` (OFF, HEATING, COOLING) |
| `ThermostatMode` | `mode` and `availableModes` (HEAT, COOL, HEATCOOL, OFF) |
| `Connectivity` | `status` (ONLINE, OFFLINE) |

**Events** arrive over Cloud Pub/Sub and carry `eventId`, a `timestamp`, and
`resourceUpdate.traits`, which holds only the fields that changed. They arrive
unordered, and the docs say "use the timestamp". There is no history API and
no counter. The temperature page carries a notice that Pub/Sub topics are
being discontinued by 2026-11-30; the details were not read.

### 3.5 Z-Wave

Source: checked (secondary), zwave-js `MeterCC.ts`, `MultilevelSensorCC.ts`,
`Meters.ts`, `Sensors.ts`, `Scales.ts` and `Primitive.ts`. The Z-Wave Alliance
PDFs were not read.

**Value encoding**: one byte packs precision (3 bits), scale (2 bits) and size
(3 bits). Then come 1 to 4 bytes of a big-endian signed integer, and the value
is `int / 10^precision`. The device chooses its own decimal precision **per
report**.

**Multilevel Sensor**: a sensor type plus a scale code. For example:

| Type | Quantity | Scales |
| --- | --- | --- |
| `0x01` | air temperature | 0 °C, 1 °F |
| `0x03` | illuminance | 0 %, 1 lux |
| `0x05` | humidity | 0 %, 1 g/m3 |
| `0x11` | CO2 | ppm |
| `0x1B` | UV | UV index |
| `0x23` | PM2.5 | 0 mol/m3, 1 ug/m3 |

**Meter CC**:

- **Meter types and scales**:
  - electric `0x01`: kWh, kVAh, W, pulse count, V, A, power factor, kvar,
    kvarh
  - gas `0x02`: m³, ft³, pulse
  - water `0x03`: m³, ft³, US gal, pulse
- **Rate type**: unspecified, consumed (import) or produced (export).
- **Previous value**: from version 2, a report carries **Delta Time**
  (uint16; 0xffff means unknown) and a **Previous Value**. One report then
  holds an interval amount as well as the counter.
- **Reset**: `MeterCCReset` resets the counter, and newer versions accept a
  target value.

**Notification CC** carries categorical events. For example, Access Control
`0x06` has event `0x16` "Window/door is open" and `0x17` "Window/door is
closed".

### 3.6 MQTT Sparkplug B 3.0

Sources: checked (source), https://github.com/eclipse-tahu/tahu/blob/master/sparkplug_b/sparkplug_b.proto,
and the specification chapters at https://github.com/eclipse-sparkplug/sparkplug.

- **Payload**: `timestamp`, `metrics[]`, `seq`, `uuid` and `body`.
- **Metric fields**:

  | Field | Notes |
  | --- | --- |
  | `name` | |
  | `alias` | uint64 |
  | `timestamp` | ms since epoch; "the time at which the value of a metric was captured", required on every metric |
  | `datatype` | an enum: Int8 to UInt64, Float, Double, Boolean, String, DateTime, Text, UUID, DataSet, Bytes, File, Template, PropertySet, and arrays |
  | `is_historical` | "should not be considered a real time/current value" |
  | `is_transient` | "should not be stored in a historian" |
  | `is_null` | |
  | `metadata` | |
  | `properties` | a PropertySet |
  | value | a oneof |

- **Topics**: `spBv1.0/<group_id>/<NBIRTH|DBIRTH|NDATA|DDATA|...>/<edge_node_id>/[<device_id>]`.
- **Birth and data messages**: a BIRTH carries every metric's name, alias,
  datatype and current value. DATA carries the alias and the changed values,
  which is **report by exception**.
- **Properties**: the only property the spec defines is `Quality` (0 BAD, 192
  GOOD, 500 STALE). `engUnit`, `engLow` and `engHigh` are absent from the spec
  text; they are an Ignition convention (unverified (recalled)).
- **No units or counters**: Sparkplug has no units and no counter or reset
  semantics. A counter is a plain metric.

### 3.7 Home Assistant MQTT discovery

Source: checked (docs), https://www.home-assistant.io/integrations/mqtt/ and
https://www.home-assistant.io/integrations/sensor.mqtt/.

- **Topic**: `<prefix>/<component>/[<node_id>/]<object_id>/config`, where the
  prefix defaults to `homeassistant`. An empty payload deletes the entity.
- **Device-based discovery**: the component is `device`, and the payload has
  `dev`, `o` (origin) and `cmps`. Each component needs `p` (platform) and
  `unique_id`.
- **Sensor keys**:
  - `state_topic`, `value_template`, `unique_id`
  - `device_class`, `state_class`, `unit_of_measurement`
  - `suggested_display_precision`
  - `expire_after`: "number of seconds after the sensor's state expires if
    it's not updated"
  - `force_update`: send update events even when the value has not changed
  - `last_reset_value_template`
  - `json_attributes_topic`, `availability`
- **Device keys**: `identifiers`, `connections`, `manufacturer`, `model`,
  `model_id`, `name`, `sw_version`, `hw_version`, `serial_number`,
  `suggested_area`, `via_device`.
- **The device fixes the semantics**: the publisher declares `state_class` and
  `device_class` once, at discovery, and HA applies them to every later value.

---

## 4. Buildings and energy

Section sources: a research agent read the Haystack docs, the Brick source on
GitHub, the OpenADR 3 schemas, the DSMR 5.0.2 P1 PDF, the Victron VRM OpenAPI
and D-Bus wiki, the Enphase v4 OpenAPI, the Tesla Fleet API docs and
`vehicle_data.proto`, the PyEmVue source and the OpenESPI XSD.

### 4.1 Project Haystack

Source: checked (docs), https://project-haystack.org/doc/docHaystack/Points,
`.../lib-phIoT/hisMode`, `.../docHaystack/Meters` and `.../docHaystack/Ops`.

**Point tags**:

- **Role**: `point`, plus one of `sensor` (input), `cmd` (output) or `sp`
  (setpoint).
- **Value kind**: `kind` is `Number` (with a `unit`), `Bool` or `Str` (an
  enumeration).
- **Live value and history**: `cur` (the point has a live value) and `his`
  (the point has a history).
- **Quantity**: `temp`, `humidity`, `power`, `energy`, `elec` and so on.
  Haystack 4 defines these as `quantity` defs.
- **Containment**: `siteRef`, `equipRef`, `spaceRef`.

**`hisMode`** declares the semantics of history samples, and the tag is
required on every `his` point:

| `hisMode` | Meaning |
| --- | --- |
| `sampled` | the timestamp is the sample time; linear interpolation is assumed |
| `cov` | the value holds from its timestamp, inclusive, until the next one, exclusive |
| `consumption` | "Integral data ... logged at the end of the interval", start exclusive and end inclusive |

**`hisTotalized`**: "a continuous stream of totalization. History data reads
and should be normalized by computing the delta." The page does not mention
rollover.

**Meters**: `ac-elec-meter`, with `siteMeter` or `submeterOf`. `import` and
`export` name the flow direction.

**Units**: from Haystack's own unit database, with a name and symbol aliases
(`kilowatt` / `kW`, `celsius` / `°C`, and the separate delta unit
`celsius_degrees` / `Δ°C`).

**`hisRead`**: returns a grid of `ts` and `val`, with `hisStart` inclusive and
`hisEnd` exclusive in the point's own time zone.

### 4.2 Brick

Source: checked (source), https://github.com/BrickSchema/Brick, `bricksrc/`.

- **Point classes**: `Sensor`, `Setpoint`, `Command`, `Status`, `Alarm`,
  `Parameter`. `Energy_Usage_Sensor` "Measures the total amount of energy used
  over some period of time".
- **Quantity**: `brick:hasQuantity` is a sub-property of
  `qudt:hasQuantityKind`. `brick:hasSubstance` names the medium.
  `brick:hasUnit` takes a QUDT unit (`unit:KiloW`, `unit:KiloW-HR`).
- **Containment and flow**: `isPointOf` (equipment or location), `hasLocation`,
  `feeds`. Meters have `meters` / `isMeteredBy` and `hasSubMeter`.
- **Aggregation**: `brick:aggregate [ brick:aggregationFunction "max" ;
  brick:aggregationInterval "RP1D" ]`. The function is one of max, min, count,
  mean, sum, median or mode.
- **Where the values live**: in a separate database, linked by
  `ref:hasExternalReference [ a ref:TimeseriesReference ; ref:hasTimeseriesId
  ... ; ref:storedAt ... ]`.
- **What it lacks**: Brick has no totalized-versus-delta flag and no rollover
  semantics.

### 4.3 OpenADR 3

Source: checked (source), https://github.com/oadr3-org/openadr3-schemas and the
https://github.com/grid-coordination/openadr3-specification mirror.

- **Report structure**: a report holds `resources[]`, each with
  `intervals[]`. An interval holds `{id, intervalPeriod{start, duration},
  payloads[{type, values[]}]}`.
- **`payloadType`**:
  - `READING`: "An instantaneous data point, as from a meter. Same as pulse
    count"
  - `USAGE`: "Energy usage over an interval"
  - `DEMAND`: power over an interval
  - also `DELTA_USAGE`, `BASELINE`, `OPERATING_STATE`, `SETPOINT` and others
- **`readingType`**: `DIRECT_READ`, `ESTIMATED`, `SUMMED`, `MEAN`, `PEAK`,
  `FORECAST`, `AVERAGE`, `NORMAL` or `ERROR`.
- **Units**: an enum: `KWH`, `KW`, `THERMS`, `VOLTS`, `AMPS`, `CELSIUS`,
  `PERCENT`, `KVARH` and others.

### 4.4 DSMR P1 smart meter telegram (Netherlands 5.0.2; Belgian eMUCS)

Sources: checked (docs), https://www.netbeheernederland.nl/sites/default/files/2024-02/dsmr_5.0.2_p1_companion_standard.pdf;
the Belgian codes checked (secondary) in
https://github.com/ndokter/dsmr_parser/blob/master/dsmr_parser/obis_references.py.

- **Transport**: a text telegram once a second at 115200 baud, closed with a
  CRC16.
- **Timestamp**: `0-0:1.0.0(YYMMDDhhmmssX)`, where X is `S` in summer time and
  `W` in winter time. The offset is a flag, not a number.
- **Meter identity**: `0-0:96.1.1`.

| OBIS code | Meaning | Shape |
| --- | --- | --- |
| `1-0:1.8.1`, `1-0:1.8.2` | delivered to the client, tariff 1 and 2, `F9(3,3)` kWh | **cumulative counter per tariff** |
| `1-0:2.8.1`, `1-0:2.8.2` | delivered by the client (export) | cumulative counter |
| `0-0:96.14.0` | current tariff (`0001`, `0002`) | categorical |
| `1-0:1.7.0`, `1-0:2.7.0` | actual power in and out, kW at 1 W resolution | instant |
| `1-0:32.7.0`, `52.7.0`, `72.7.0` | voltage per phase, V | instant |
| `1-0:31.7.0`, `51.7.0`, `71.7.0` | current per phase, A | instant |
| `0-0:96.7.21`, `96.7.9` | number of power failures, long power failures | event counter |
| `0-n:24.2.1(TST)(12785.123*m3)` | the gas meter on M-Bus channel n: **the last 5-minute value with its own capture time**, temperature-corrected | cumulative counter, stamped by the sub-meter |
| BE `1-0:1.4.0`, `1-0:1.6.0(TST)(kW)`, `0-0:98.1.0` | Belgian capacity tariff: current average demand, the month's peak demand with its time, a 13-month history of peaks | summary rows (max over 15 minutes, per month; the 15-minute window is unverified (recalled)) |

Notes on these counters:

- **Rollover**: the spec says nothing about it. `F9(3,3)` implies a ceiling of
  999999.999 kWh.
- **Meter swap**: shows up only as a new `96.1.1`.
- **Gas timing**: the gas reading is stamped by the gas meter, not by the
  telegram, so one telegram carries two clocks.

### 4.5 Victron

Sources: checked (docs), https://vrm-api-docs.victronenergy.com/ (the stats
command) and https://github.com/victronenergy/venus/wiki/dbus.

- **VRM `GET /installations/{idSite}/stats`**:
  - parameters `start`, `end`, `type`, `interval` and `attributeCodes[]`
  - `type` includes `live_feed`, `kwh` ("Energy (kWh) counters"),
    `consumption`, `solar_yield` and `custom`
  - `interval` takes `15mins`, `hours`, `2hours`, `days`, `weeks`, `months`
    or `years`
  - the response maps each attribute code to `[timestamp, mean, min, max]`
    rows, plus `totals`
- **D-Bus paths** use SI units, except kWh for energy and °C for temperature:
  - battery: `/Dc/Battery/Soc` (%), `/Dc/Battery/Power`
  - grid: `/Ac/Power` (W); `/Ac/Energy/Forward` and `/Ac/Energy/Reverse` (kWh
    counters)
  - solar charger: `/Yield/User` ("user resettable") beside `/Yield/System`
    ("not resettable"). One device exposes two counters with different reset
    rules.

### 4.6 Enphase v4

Source: checked (source), https://developer-v4.enphase.com/swagger/spec/System_API.json.

- **`telemetry/production_micro`**: `intervals[]{end_at, devices_reporting,
  powr (average W), enwh (Wh)}` at `5mins`, `15mins`, `day` or `week`
  granularity.
- **`telemetry/consumption_meter`**: 15-minute `enwh`.
- **`telemetry/battery`**: `charge{enwh}`, `discharge{enwh}`,
  `soc{percent}`.
- **`rgm_stats`**: 15-minute `wh_del` and `curr_w`, "listed by their end
  times".
- **`production_meter_readings`**: a cumulative `value` in Wh with `read_at`.
- **`energy_lifetime`**: a daily Wh array, in which zeros at the end mean "not
  yet reported".
- **Partial intervals**: `devices_reporting` marks an interval that not every
  device reported into. That is a completeness flag on an interval amount.

### 4.7 Tesla

Sources: checked (docs), https://developer.tesla.com/docs/fleet-api/endpoints/energy
and `.../fleet-telemetry`; checked (source),
https://github.com/teslamotors/fleet-telemetry/blob/main/protos/vehicle_data.proto.
Field lists cross-checked in HA's `teslemetry` fixtures.

- **Powerwall `live_status`**: `solar_power`, `battery_power`, `load_power`,
  `grid_power` (W), `percentage_charged`, `grid_status`, `island_status` and
  `timestamp`.
  - The sign convention is not documented.
  - The fixture's arithmetic implies a positive `battery_power` means
    discharging. That is an inference, not a documented fact.
- **`calendar_history?kind=energy&period=...`**: per-period Wh flows such as
  `solar_energy_exported`, `grid_energy_imported`,
  `battery_energy_imported_from_solar` and
  `consumer_energy_imported_from_grid`. These are **directional flow amounts
  between nodes**, not one meter.
- **Vehicle Fleet Telemetry**:
  - `Payload{data[]{key: Field, value}, created_at, vin, is_resend}`.
  - `Field` values include `VehicleSpeed`, `Odometer`, `Soc`, `BatteryLevel`,
    `EstBatteryRange`, `Location`, `TpmsPressureFl`, `InsideTemp`,
    `OutsideTemp`, `ChargeState` and `Gear`.
  - A field is emitted when `interval_seconds` has passed **and** the value
    changed.
  - `minimum_delta` (meters) gates `Location`.
  - `is_resend` marks a redelivery.

### 4.8 Emporia Vue

Sources: checked (secondary), https://github.com/magico13/PyEmVue and
https://github.com/magico13/ha-emporia-vue.

- **Scales**: `1S`, `1MIN`, `15MIN`, `1H`, `1D`, `1W`, `1MON`, `1Y`.
- **Usage**: every value is an energy **amount per scale bucket**, per
  `deviceGid` and `channelNum`, where mains is `1,2,3`.
- **Power is derived**: the HA integration computes W as kWh x 60000 at `1MIN`
  and kWh x 3600000 at `1S`. The device exposes no instant reading.
- **Resets**: the daily and monthly HA sensors reset at midnight and at the
  start of the billing cycle.

### 4.9 Green Button / NAESB ESPI

Source: checked (source), the OpenESPI `espi.xsd` (ESPI 4.0) at
https://github.com/GreenButtonAlliance/OpenESPI-GreenButton-Java.

This is the most explicit counter model in the survey.

- **`UsagePoint`**: a metering point.
- **`MeterReading`**: links one `ReadingType` and many `IntervalBlock`s.
- **`ReadingType`** fields:
  - **`uom`**: 72 Wh, 38 W, 29 V, 5 A, 33 Hz, 23 degC, 42 m3, 119 ft3, 169
    therm
  - **`powerOfTenMultiplier`**: the actual value is `value x
    10^powerOfTenMultiplier`
  - **`kind`**: 12 energy, 37 power, 8 demand
  - **`commodity`**: 1 electricity, 7 natural gas, 9 potable water
  - **`flowDirection`**: 1 forward, 19 reverse, 4 net, 20 total
  - **`intervalLength`**: seconds, for example 900
  - **`accumulationBehaviour`**:
    - `1` bulkQuantity: a dial register that "will roll over upon reaching a
      maximum dial value"
    - `3` cumulative
    - `4` deltaData: "The difference between the value at the end of the
      prescribed interval and the beginning"
    - `6` indicating: a held needle value
    - `12` instantaneous
    - `9` summation
    - `13` latchingQuantity
    - `14` boundedQuantity
- **`IntervalReading`**: `timePeriod{start, duration}`, `value` (Int48), `cost`
  (hundred-thousandths of the currency) and `ReadingQuality[]`. Quality codes
  include 0 valid, 7 manually edited, 8 and 9 estimated, 11 derived, 14 raw,
  17 validated, 19 revenue quality.
- **`UsageSummary`**: billing-period rows such as `billLastPeriod`,
  `overallConsumptionLastPeriod`, `peakDemand` and `ratchetDemand`.

A typical utility interval feed is `uom 72, multiplier 0, accumulationBehaviour
4, intervalLength 900`: 15-minute Wh amounts stamped with start and duration.

### 4.10 Power, counter and interval across energy systems

| System | Instant power | Cumulative counter | Interval amount | Timestamp of an interval |
| --- | --- | --- | --- | --- |
| Home Assistant | `device_class: power`, `measurement` | `energy`, `total_increasing` | `energy`, `total` with `last_reset`, or external statistics | start of the hour (`start_ts`) |
| Matter | `ActivePower` mW | `CumulativeEnergyImported` mWh, with reset timestamps | `PeriodicEnergyImported` | start and end |
| ZCL | `ActivePower`, `InstantaneousDemand` | `CurrentSummationDelivered` uint48 | partial profile interval, day consumption | interval start |
| Z-Wave | Meter scale W | Meter scale kWh | previous value plus delta time | none, a delta only |
| Haystack | `power` point, `sampled` or `cov` | `hisTotalized` | `hisMode: consumption` | **end** |
| Brick | `*_Power_Sensor` | (none) | `aggregate sum` | n/a |
| OpenADR 3 | `DEMAND` | `READING` | `USAGE` | start and duration |
| DSMR P1 | `1.7.0` | `1.8.x` per tariff | (none) | reading time |
| Victron | `/Ac/Power` | `/Ac/Energy/*` | VRM stats rows | bucket start (unverified) |
| Enphase | `powr`, `curr_w` | meter `value` Wh | `enwh`, `wh_del` | **end** (`end_at`) |
| Tesla energy | `*_power` | (none) | `calendar_history` | period |
| Emporia | derived | (none) | per-scale `usage` | bucket start |
| ESPI | `kind 37`, accumulation 6 or 12 | accumulation 1 (bulk) | accumulation 4 (delta) | start and duration |

---

## 5. Vehicles, weather and environment

Section sources: a research agent read the vendor docs, live API calls to
Open-Meteo and api.weather.gov on 2026-10-08, the Traccar source, open-source
ANT+ decoders and the EPA AirNow technical document.

### 5.1 OBD-II Mode 01

Source: checked (secondary), https://en.wikipedia.org/wiki/OBD-II_PIDs.

| PID | Quantity | Formula, unit |
| --- | --- | --- |
| `0x05` | coolant temperature | `A - 40` °C |
| `0x0C` | engine speed | `(256A + B) / 4` rpm |
| `0x0D` | vehicle speed | `A` km/h |
| `0x2F` | fuel level | `100A / 255` % |
| `0x5B` | hybrid battery remaining | `100A / 255` % |
| `0xA6` | odometer | 32-bit value / 10, km, resolution 0.1 km, a lifetime counter |

A response holds the PID echo and the data bytes only, so the **reader stamps
the time**.

### 5.2 Smartcar

Source: checked (docs), https://smartcar.com/docs/api-reference/.

- **v2 endpoints**:
  - odometer: `distance` (km)
  - fuel: `percentRemaining`, `amountRemaining` (L), `range` (km)
  - battery: `percentRemaining` **as a 0 to 1 fraction** (the example is
    `0.3`), `range`
  - charge: `state` (`CHARGING`, `FULLY_CHARGED`, `NOT_CHARGING`),
    `isPluggedIn`
  - tires: `frontLeft` and the others, in kPa
  - location: `latitude`, `longitude`
- **Units**: a request header, `SC-Unit-System: metric|imperial`.
- **Times**: `SC-Data-Age` is when the vehicle recorded the value, and
  `SC-Fetched-At` is when Smartcar fetched it.
- **v3 signals**: `{code: "tractionbattery-stateofcharge", body: {unit:
  "percent", value: 78}, meta: {oemUpdatedAt, retrievedAt}}`. A webhook
  delivers all subscribed signals on every trigger.
- **Deprecation**: v2 is deprecated on 2026-12-01.
- **Three clocks per value**: the OEM's, Smartcar's fetch, and delivery.

### 5.3 Traccar

Source: checked (source), `Position.java`, `Event.java`, `DistanceHandler.java`
and `openapi.yaml` at https://github.com/traccar/traccar.

- **Position fields**:
  - identity: `deviceId`, `protocol`
  - three clocks: `serverTime`, `deviceTime`, `fixTime`
  - `valid`, `outdated`
  - `latitude`, `longitude`, `altitude` (m), **`speed` in knots**, `course`
    (°), `accuracy`, `address`, `network`, `geofenceIds`
  - a free `attributes` map
- **Attribute keys**:
  - battery and power: `batteryLevel` (%), `battery` (V), `power` (V)
  - odometers: `odometer`, `obdOdometer`, `tripOdometer`
  - distance: `distance` (m since the previous position), `totalDistance` (m,
    which the **server** accumulates)
  - engine and motion: `ignition`, `motion`, `hours` (engine hours), `rpm`,
    `coolantTemp`
  - fuel: `fuel` (L), `fuelLevel` (%), `fuelUsed`
  - other: `alarm`, `sat`, `hdop`, `rssi`
- **Events**: the edges of categorical states, such as `ignitionOn`,
  `ignitionOff`, `deviceMoving`, `deviceStopped`, `geofenceEnter`,
  `geofenceExit`, `deviceOverspeed`, `deviceFuelDrop`, `deviceOnline` and
  `deviceOffline`.
- **Reports**: `summary` (with `daily`), `trips`, `stops`, `route`. Each row
  carries `distance`, `averageSpeed`, `maxSpeed`, `spentFuel`,
  `startOdometer`, `endOdometer`, `startTime` and `endTime`. These are
  **sessions**, built as end minus start of counters.

### 5.4 ANT+ FE-C and LEV

Source: checked (secondary), the openant, ant-plus and FortiusANT decoders on
GitHub. The ANT+ profiles sit behind a login.

- **FE-C page 16**:
  - elapsed time: 8 bits at 0.25 s, so it **wraps every 64 s**
  - distance: 8 bits at 1 m, so it **wraps every 256 m**
  - speed: uint16 at 0.001 m/s
- **FE-C page 0x19**: accumulated power is a 16-bit counter.
- **LEV**: speed is 12 bits at 0.1 km/h; the odometer is 3 bytes at 0.01 km,
  a lifetime counter.
- **What the receiver must do**: unwrap every counter by watching every
  message. A gap longer than one wrap loses data. Messages carry no clock.

### 5.5 Open-Meteo

Sources: checked (docs), https://open-meteo.com/en/docs, and a live call on
2026-10-08.

- **Hourly variables**: `temperature_2m` (°C), `relative_humidity_2m` (%),
  `precipitation` (mm), `wind_speed_10m` (km/h), `wind_direction_10m` (°),
  `surface_pressure` (hPa), `uv_index` (unit `""`).
- **Time semantics differ per variable**: precipitation is the "preceding hour
  sum", and temperature and wind are instants at the hour.
- **Response shape**: columnar `hourly{time[], var[]}` plus `hourly_units`.
  The times are local wall time with no offset, beside `utc_offset_seconds`.
- **Units**: request parameters `temperature_unit`, `wind_speed_unit` (`kmh`,
  `ms`, `mph`, `kn`) and `precipitation_unit`.
- **Daily summaries**: `temperature_2m_max`, `temperature_2m_min`,
  `precipitation_sum`, over the local day of the requested `timezone`.
- **Identity**: a grid cell, not a station. The returned coordinates differ
  from the requested ones.
- **Forecasts**: forecast values are predictions; their result time comes
  before their phenomenon time.

### 5.6 NWS api.weather.gov

Sources: checked (docs), https://api.weather.gov/openapi.json, and a live
observation call.

- **Every quantity is a `QuantitativeValue`**: `{unitCode, value (number or
  null), qualityControl}`.
- **`unitCode`** is `wmoUnit:degC`, `wmoUnit:km_h-1`, `wmoUnit:Pa`,
  `wmoUnit:percent`, `wmoUnit:degree_(angle)` or `wmoUnit:mm`.
  - A bare code or `uc:` means UCUM.
  - The spec marks the namespaced forms deprecated.
- **Interval amounts**: `precipitationLastHour`, `precipitationLast3Hours`,
  `precipitationLast6Hours`.
- **Summaries**: `maxTemperatureLast24Hours`, `minTemperatureLast24Hours`.
- **Identity**: `stationId` plus `timestamp`.
- **Quality**: `qualityControl` carries MADIS flags such as V and Z.
- **Missing values**: `value: null` with the unit still present.

### 5.7 Netatmo

Sources: checked (secondary), the Netatmo PHP SDK and the pyatmo fixtures. The
dev.netatmo.com pages render only with JavaScript.

- **Identity**: the station and module `_id`s are MAC addresses. Module `type`
  is one of NAMain, NAModule1 (outdoor), NAModule2 (wind), NAModule3 (rain)
  or NAModule4 (extra indoor module).
- **`dashboard_data`**:
  - `time_utc`
  - climate: `Temperature`, `Humidity`, `CO2`, `Noise` (dB), `Pressure`,
    `AbsolutePressure` (mbar)
  - day summaries: `min_temp`, `max_temp`, `date_min_temp`, `date_max_temp`
  - trends: `temp_trend` (up, down, stable)
  - wind: `WindStrength`, `WindAngle`, `GustStrength`, `GustAngle`
  - rain: `Rain` (mm in the last 5 minutes), `sum_rain_1` (the last hour),
    `sum_rain_24` (the current local day, reset at midnight)
- **`getmeasure`**:
  - `scale` takes `max`, `30min`, `1hour`, `3hours`, `1day`, `1week` or
    `1month`
  - types include `min_temp`, `max_temp`, `sum_rain` and `date_max_gust`
  - an aggregated row is stamped at the **middle of its bucket** unless
    `real_time` is set
  - `limit` is 1024

### 5.8 Air quality

**PurpleAir**. Source: checked (docs), https://community.purpleair.com/t/api-fields-descriptions/4652.

- **Identity**: `sensor_index`.
- **PM2.5 (ug/m3)**: `pm2.5_atm`, `pm2.5_cf_1` and `pm2.5_alt`, each averaging
  laser channels A and B.
- **Averages**: `pm2.5_10minute` through `pm2.5_1week`, which PurpleAir calls
  "pseudo-averaged". These are rolling values, not fixed windows.
- **Housing readings**: `temperature` (°F, measured inside the housing,
  "about 8F higher than ambient"), `humidity`, `pressure` (mbar).
- **Quality**: `confidence` (0 to 100, from agreement between channels A and B)
  and `channel_flags`.

**AirGradient** local API. Source: checked (docs),
https://github.com/airgradienthq/arduino/blob/master/docs/local-server.md.

- **Fields**: `pm01`, `pm02`, `pm10`, `pm02Compensated`, `pm003Count`, `rco2`
  (ppm), `atmp` (°C), `atmpCompensated`, `rhum`, `tvocIndex`, `noxIndex`,
  `tvocRaw`, `wifi` (dBm), `serialno`, `boot`.
- **Time**: the payload carries **no timestamp**.
- **Raw and corrected values**: the API reports a raw value and a compensated
  value side by side.

**US EPA AQI**. Source: checked (docs), EPA-403/B-26-003,
https://document.airnow.gov/technical-assistance-document-for-the-reporting-of-daily-air-quailty.pdf.

- **Breakpoints**: a piecewise-linear index of the 24-hour PM2.5 average.
  Good is 0.0 to 9.0, Moderate 9.1 to 35.4, and an AQI of 500 is 325.4
  ug/m3.
- **NowCast**: a weighted 12-hour average.
- **What it is**: a **derived index of a window**, not a reading.

**Sensirion VOC Index**. Source: checked (docs),
https://sensirion.com/media/documents/02232963/6294E043/Info_Note_VOC_Index.pdf.

- The index runs from 1 to 500. 100 is "a moving average over the past 24
  hours", and the device adapts its gain to the last 24 hours.
- **The same number means different things on different devices and
  different days.** It is a relative, device-defined index.

### 5.9 Personal weather stations

**Weather Underground upload**. Source: checked (secondary), a mirror of the
old WU protocol page at
https://raw.githubusercontent.com/ccgruber/updateweatherstation/master/pws_protocol.txt.

- **Request**: `GET .../updateweatherstation.php?ID&PASSWORD&dateutc=now|YYYY-MM-DD HH:MM:SS`.
- **Units**: imperial only.
- **Fields**: `tempf`, `humidity`, `baromin` (inHg), `windspeedmph`,
  `winddir` (°), `windgustmph`, `rainin` (**"accumulated rainfall in the past
  60 min"**, a rolling window), `dailyrainin` ("so far today in local time"),
  `solarradiation` (W/m2), `UV`.
- **Time**: `dateutc=now` means the server stamps the value.

**Ecowitt custom upload**. Source: checked (secondary),
https://gist.githubusercontent.com/tsmx/70d06c6e4cfc65b6d7d23c7f9c75b17c/raw.

- **Request**: a form POST with `PASSKEY`, `stationtype`, `dateutc`,
  `tempinf`, `tempf`, `baromrelin`, `baromabsin` and the wind fields.
- **Rain fields**: `rainratein`, `hourlyrainin`, `eventrainin`,
  `dailyrainin`, `weeklyrainin`, `monthlyrainin`, `yearlyrainin`,
  `totalrainin`.
  - **One gauge reports eight accumulators**, each with its own reset rule.
  - Daily, weekly and monthly reset at local period starts, `eventrainin`
    resets after a dry spell, and `totalrainin` resets only manually. These
    reset rules are unverified (recalled).

**Davis WeatherLink v2**. Sources: checked (docs),
https://weatherlink.github.io/v2-api/ and
https://weatherlink.github.io/weatherlink-live-local-api/.

- **Record type**: `data_structure_type` says what a record is: 10 current, 11
  archive, and so on.
- **Current conditions**:
  - `ts` (device) and `generated_at` (server)
  - `temp` (°F), `hum`, `wind_speed_last`, `wind_dir_last`,
    `wind_speed_avg_last_10_min`, `rain_rate_last_in`
  - rain counters `rainfall_daily_in`, `_monthly_`, `_year_`, `rain_storm_in`
- **Rain units**: every rain field exists in clicks, in and mm. `rain_size`
  defines a click as 0.01 in, 0.2 mm, 0.1 mm or 0.001 in. The device counts
  **tips of a bucket**, a quantised unit.
- **Archive records**: `arch_int` (300 s), with `ts` at the **end** of the
  interval. A query selects `(start, end]`.

---

## 6. Time-series conventions and what the Home Assistant exporters lose

### 6.1 OpenTelemetry metrics

Sources: checked (docs), https://opentelemetry.io/docs/specs/otel/metrics/data-model/
and https://opentelemetry.io/docs/specs/semconv/general/metrics/.

- **Point kinds**:
  - `Sum`, with an `AggregationTemporality` of delta or cumulative and an
    `IsMonotonic` flag
  - `Gauge`: "a sampled value at a given time"
  - `Histogram`, `ExponentialHistogram`
  - `Summary`: legacy quantiles that "cannot always be merged"
- **Temporality**: a delta point covers `(T(n-1), T(n)]`, and a cumulative
  point repeats its start time over `(T0, T(n)]`.
- **Resets**: shown by `StartTimeUnixNano` changing. When the start equals the
  time, a new sequence begins with "a reset at an unknown start time". **The
  start time is the reset epoch, carried on every point.**
- **Units**: "Units should follow the Unified Code for Units of Measure", in
  the case-sensitive form.
  - Dimensionless utilisation is `1`.
  - Counts use curly-brace annotations such as `{packet}`.
  - Durations use `s`.
- **Names**: "SHOULD NOT include the units in the metric name". The `_total`
  suffix and unit suffixes are added only when a metric is translated to
  Prometheus.

### 6.2 Prometheus

Source: checked (docs), https://prometheus.io/docs/practices/naming/.

- **Base units**: seconds, celsius, meters, bytes, ratio, volts, amperes,
  joules, grams.
- **Suffixes**: names carry the unit as a plural suffix, and counters end in
  `_total`.
- **Resets**: a counter reset is any decrease. `rate()` and `increase()` treat
  a drop as a reset to zero (unverified (recalled); standard PromQL
  behaviour).
- **No reset time**: there is no reset timestamp unless created-timestamp
  support is enabled (unverified (recalled)).

### 6.3 InfluxDB line protocol

Source: checked (docs), https://docs.influxdata.com/influxdb/v2/reference/syntax/line-protocol/.

- **Syntax**: `<measurement>[,<tag>=<v>...] <field>=<v>[,...] [<timestamp ns>]`.
- **Tags**: indexed strings.
- **Fields**: typed as float, integer (`i`), uinteger (`u`), string or
  boolean.
- **Point identity**: measurement, tag set and timestamp. A duplicate merges
  its fields.
- **Semantics**: none. There is no unit, no counter flag and no temporality.

### 6.4 The Home Assistant to Prometheus exporter

Source: checked (source), `homeassistant/components/prometheus/__init__.py`.

- **Metric name**: `sensor_<device_class>_<unit>`, for example
  `sensor_temperature_celsius` or `sensor_energy_kwh`. The unit string is
  lowercased, and `/` becomes `_per_`.
- **Labels**: `entity`, `domain`, `friendly_name`, plus area.
- **What it loses**:
  - **Every sensor value is a `Gauge`, including `total_increasing` energy.**
    The only `Counter` is the `state_change` counter. `state_class` and
    `last_reset` are dropped, so the counter semantics and the reset epoch are
    gone.
  - **Fahrenheit is converted to Celsius, and the unit map sends `°F` to
    `celsius`.** This is the one place the exporter normalises units. Every
    other unit is kept as written, so the same quantity in `W` and `kW`
    becomes two metric names.
  - **An `enum` sensor becomes one 0/1 gauge per option.** Time in state then
    has to be rebuilt from scrapes at the scrape interval. The change times
    are lost.
  - **A value is the latest state at scrape time.** Changes between scrapes
    are lost, and `last_changed` is exported only as a separate
    `last_updated_time_seconds` gauge.

### 6.5 The Home Assistant to InfluxDB exporter

Source: checked (source), `homeassistant/components/influxdb/__init__.py` and
`const.py`.

- **Measurement**: by default **the unit string** (`DEFAULT_MEASUREMENT_ATTR =
  "unit_of_measurement"`). All `°C` sensors share the measurement `°C`. The
  alternatives are `domain__device_class` and `entity_id`.
- **Tags**: `domain` and `entity_id` (the object id).
- **Fields**: `value` (float, when the state parses) and `state` (string).
  Every attribute becomes a field: a float when it parses, otherwise
  `<key>_str`.
- **Time**: the event's `time_fired`, so the write time stands in for the
  measurement time.
- **What it loses**:
  - `state_class` and `last_reset` survive only as untyped fields
    (`state_class_str`, `last_reset_str`), and nothing downstream reads them.
  - The device class is dropped from the fields when it is used in the
    measurement name.
  - The unit is dropped when the unit is the measurement name.
  - A rename of the `entity_id` starts a new series.
  - The statistics (`mean`, `sum`) are not exported at all. Only raw states
    are.

**The lesson for substrate.** Both exporters keep the number and lose its
meaning: the counter flag, the reset epoch, the time weighting, the enum's
change times. That meaning is exactly the `form` and `statistic` fields the
health plan puts on every stored value. The exporters are the failure that
principle 6 of the health plan ("a stored value carries its own meaning")
prevents.

---

## 7. Deliverable (a): the five shapes, side by side

The shapes:

- **I**: instant sample
- **A**: interval amount
- **C**: cumulative counter with resets
- **S**: categorical state
- **R**: summary or statistic row

| System | I: instant | A: interval amount | C: counter and reset | S: categorical | R: summary |
| --- | --- | --- | --- | --- | --- |
| Home Assistant | `state` string with `state_class: measurement`; the time is `last_changed` | `total` with `last_reset` = period start (for example a daily-resetting meter); or external statistics | `total_increasing`: a drop below 90% of the previous value is a reset; `total` + `last_reset` gives an explicit epoch | `binary_sensor` (on, off) or `enum` sensor with `options`; time in state via `history_stats` | `statistics` (hourly) and `statistics_short_term` (5 min): `mean`, `min`, `max` or `state`, `sum`, `last_reset_ts` |
| OGC STA / OMS / SOSA | `Observation` with an instant `phenomenonTime`, `OM_Measurement` | the same with a **period** `phenomenonTime` | no counter concept; a counter is a measurement whose meaning is in the `ObservedProperty` definition | `OM_CategoryObservation`, `OM_TruthObservation` | no row type; a "derived" observation whose procedure is the statistic |
| Matter | `MeasuredValue`, scaled int or float, no time on the value | `PeriodicEnergy*` with start and end | `CumulativeEnergy*` + `CumulativeEnergyReset` timestamps | Boolean State, Occupancy, `LevelValue` | concentration `PeakMeasuredValue` and `AverageMeasuredValue` with a window; power `Ranges` |
| Zigbee ZCL | `MeasuredValue`; `ActivePower` with multiplier and divisor | `CurrentDayConsumptionDelivered`, partial profile interval | `CurrentSummationDelivered` uint48, no generic reset | discrete attributes (on/off, IAS zone) | `PreviousDayConsumptionDelivered` |
| HomeKit | characteristic float with unit and range | (none) | (none) | uint8 enums (contact, lock, occupancy, air quality) | (none); Eve's 10-minute history only |
| Google SDM | `ambientTemperatureCelsius` in a trait event | (none) | (none) | `ThermostatHvac.status`, `ThermostatMode.mode`, `Connectivity.status` | (none) |
| Z-Wave | Multilevel Sensor value + scale + precision | Meter v2: previous value + delta time | Meter (kWh, m³, pulse); `MeterCCReset` | Notification CC events (door open, closed) | (none) |
| Sparkplug B | metric value + `timestamp` | (convention only) | (convention only) | Boolean or String metric | (none); `is_historical` marks back-filled values |
| Haystack | `his` point, `hisMode: sampled` | `hisMode: consumption`, stamped at the **end** | `hisTotalized`; the reader takes deltas | `kind: Bool` or `Str` with `hisMode: cov` | (none) native; `hisRead` with rollup in SkySpark (unverified) |
| Brick | `*_Sensor` | `Energy_Usage_Sensor` | (none) | `Status` points | `brick:aggregate {aggregationFunction, aggregationInterval}` |
| OpenADR 3 | `READING` | `USAGE`, `DEMAND` | `READING` as pulse count | `OPERATING_STATE` | `readingType` `MEAN`, `PEAK`, `SUMMED` |
| DSMR P1 | `1.7.0`, `32.7.0`, `31.7.0` | (none) | `1.8.1`, `1.8.2`, `2.8.x`, gas `24.2.1` with its own time | `96.14.0` tariff | BE `1.6.0` monthly peak, `98.1.0` history |
| Victron | D-Bus `/Ac/Power` | VRM stats row | `/Ac/Energy/Forward`; `Yield/User` (resettable) vs `Yield/System` | system state enums (unverified) | VRM `[ts, mean, min, max]`, `totals` |
| Enphase | `powr` (average W of the interval) | `enwh`, `wh_del` stamped at `end_at` | `production_meter_readings.value` Wh | (none) | `energy_lifetime` daily array |
| Tesla energy / vehicle | `live_status` W; telemetry `VehicleSpeed` | `calendar_history` flows | `Odometer` | `ChargeState`, `Gear`, `grid_status` | `calendar_history` per period |
| Emporia | (derived only) | per-scale `usage` kWh | (none) | (none) | `1D`, `1MON` scale values |
| ESPI / Green Button | accumulation 12 instantaneous, 6 indicating | accumulation 4 deltaData, `timePeriod{start, duration}` | accumulation 1 bulkQuantity, "will roll over" | (none) | `UsageSummary` |
| OBD-II | PIDs `0x0C`, `0x0D`, `0x05` | (none) | `0xA6` odometer | (none); status PIDs (unverified) | (none) |
| Smartcar | v3 signals, `oemUpdatedAt` | (none) | odometer `TraveledDistance` | `charge.state`, `isPluggedIn` | (none) |
| Traccar | `Position` (lat, lon, speed in knots...) | `distance` since the previous position | `totalDistance` (server), `odometer`, `hours` | `ignition`, `motion` + `ignitionOn` and `ignitionOff` events | `summary`, `trips`, `stops` reports |
| ANT+ | speed, power, HR | (none) | 8- and 16-bit wrapping counters (64 s, 256 m, 65536) | FE state (in use...) | (none) |
| Open-Meteo | `temperature_2m` at the hour | `precipitation` = the preceding hour | (none) | `weather_code` (WMO code, unverified) | `daily` `_max`, `_min`, `_sum` |
| NWS | `QuantitativeValue` per field | `precipitationLast{1,3,6}Hours` | (none) | `textDescription` (unverified) | `max/minTemperatureLast24Hours` |
| Netatmo | `dashboard_data` | `Rain` (the last 5 min), `sum_rain_1` | `sum_rain_24`, reset at local midnight | `temp_trend` | `getmeasure` scales; `min_temp`, `max_temp` with `date_*` |
| WU / Ecowitt | `tempf`, `winddir` | `rainin` (a rolling 60 min), `hourlyrainin` | `dailyrainin`, `weeklyrainin`, ..., `totalrainin` (calendar and event resets) | (none) | `maxdailygust` |
| Davis | current `temp`, `wind_speed_last` | archive `rainfall_in` per `arch_int`, stamped at the end | `rainfall_daily`, `_monthly`, `_year`, `rain_storm` | (none) | archive `_hi`, `_lo`, `wind_dir_of_prevail` |
| PurpleAir / AirGradient | `pm2.5_atm`, `rco2` | (none) | (none) | (none) | `pm2.5_10minute` and the other rolling pseudo-averages; AQI as a derived index |
| OpenTelemetry | `Gauge` | `Sum`, delta temporality | `Sum`, cumulative, monotonic; reset = new `StartTimeUnixNano` | (none); an enum is modelled as 0/1 gauges | `Histogram`, `Summary` |
| Prometheus | gauge | (none) | counter `_total`; a reset is any decrease | `_info`, or a 0/1 gauge per state | summary, histogram |
| InfluxDB | field | field | field | string field | (none); computed by query |

**Units, provenance, identity and updates:**

| System | Units | Provenance (device, integration, sensor) | Identity of a stream | Update semantics |
| --- | --- | --- | --- | --- |
| Home Assistant | display strings from enums, converted within a unit class | integration (`platform`), device registry, area | `unique_id` (stable), `entity_id` (renameable), statistics `source:statistic_id` | on change, plus `last_reported` on repeat; `force_update`; `expire_after` |
| OGC STA | `{name, symbol, definition URI}` per Datastream | `Sensor`, `Thing`, `Location` | `Datastream` id | any; `resultTime` separate |
| Matter | fixed scaling per attribute; concentration unit enum | node, endpoint, Basic Information | node, endpoint, cluster, attribute | subscription with min and max interval; Quieter Reporting |
| ZCL | fixed scaling; metering `UnitofMeasure` + multiplier and divisor | IEEE address, endpoint (unverified) | IEEE address, endpoint, cluster, attribute | min and max interval + reportable change |
| HomeKit | fixed per characteristic | accessory id, instance id (unverified) | aid, iid | `ev` notifications |
| Google SDM | fixed per trait (Celsius, percent) | `enterprises/.../devices/...` | device name + trait | Pub/Sub events, unordered |
| Z-Wave | scale code per report, precision per report | home id, node, endpoint (unverified) | node, endpoint, CC, type, scale | unsolicited or polled |
| Sparkplug B | none in the spec; `engUnit` by convention | group, edge node, device | metric name or alias | report by exception, BIRTH carries all values |
| Haystack | unit database names and symbols | `equipRef`, `siteRef`, `spaceRef` | point `id` | `cur`, `his` |
| Brick | QUDT | `isPointOf`, `hasLocation` | point IRI + `hasTimeseriesId` | external |
| DSMR | unit inside the value `(…*kWh)` | meter `96.1.1`, M-Bus channel | meter id + OBIS code | every 1 s; gas every 5 min |
| ESPI | numeric `uom` + `powerOfTenMultiplier` | `UsagePoint` | `MeterReading` + `ReadingType` | batch, by interval |
| NWS | `wmoUnit:` codes, or UCUM | station | `stationId` + `timestamp` | per observation |
| OpenTelemetry | UCUM, case-sensitive | resource attributes | name + attributes | delta or cumulative export |

---

## 8. Deliverable (b): the catalogue of non-health measured quantities

**Columns:**

- **Dimension**: the physical dimension, using the health plan's term.
- **HA device class and units**: checked (source), `sensor/const.py`.
- **Matter / ZCL**: section 3.
- **Other systems**: the units seen in sections 3 to 5.
- **UCUM code**: the code the engine's table would need. The health plan's
  table is about 100 codes, mostly clinical.
  - A **bold** code is one that table does not already cover. That is my
    judgement of the health catalogue; I did not check it line by line.
  - "n/a" means UCUM has no fitting atom, and an annotation or a
    package-defined index is needed.

| Quantity | Dimension | HA device class: units | Matter / ZCL | Other systems | UCUM code |
| --- | --- | --- | --- | --- | --- |
| Air temperature | temperature (affine) | `temperature`: °C, °F, K | `100 x °C` int16 | HomeKit °C float 0.1; Z-Wave °C or °F per report; WU, Ecowitt, Davis °F; NWS `wmoUnit:degC`; OBD coolant `A-40` °C; PurpleAir °F (housing) | `Cel`, `[degF]`, `K` |
| Temperature difference | temperature-difference | `temperature_delta`: °C, °F, K | | Haystack `Δ°C` | `K`, `Cel` with delta |
| Relative humidity | dimensionless ratio | `humidity`: % | `100 x %` uint16 | everywhere % | `%` |
| Absolute humidity | mass concentration | `absolute_humidity`: g/m³, mg/m³ | | Z-Wave g/m³ | **`g/m3`** |
| Moisture (soil) | ratio | `moisture`: % | | | `%` |
| Atmospheric pressure | pressure | `atmospheric_pressure`, `pressure`: Pa, hPa, kPa, mbar, cbar, bar, mmHg, inHg, inH₂O, psi, atm | `10 x kPa`, or `10^Scale x Pa` | Netatmo mbar; WU, Ecowitt inHg (relative vs absolute); NWS Pa; Open-Meteo hPa; PurpleAir mbar | **`Pa`, `hPa`, `kPa`, `mbar`, `bar`, `[in_i'Hg]`, `[psi]`, `atm`**; `mm[Hg]` exists |
| Tyre pressure | pressure | (`pressure`) | | Smartcar kPa; Tesla `TpmsPressureFl` (bar, unverified) | `kPa`, **`bar`**, **`[psi]`** |
| Illuminance | illuminance | `illuminance`: lx | `10000 log10(lx)+1` (log scale) | HomeKit lux 0.0001 to 100000; Z-Wave % or lux | **`lx`** |
| Irradiance (solar) | power per area | `irradiance`: W/m², BTU/(h⋅ft²) | | WU `solarradiation` W/m² | **`W/m2`** |
| UV index | dimensionless index | (none; generic) | | WU `UV`; Open-Meteo `uv_index` unit `""`; Z-Wave UV index | n/a: **`{index}`** annotation |
| CO2 | amount fraction | `carbon_dioxide`: ppm | float + unit enum (PPM...) | HomeKit ppm; Netatmo, AirGradient `rco2` ppm | **`[ppm]`** |
| CO | amount fraction or mass concentration | `carbon_monoxide`: ppb, ppm, mg/m³, μg/m³ | float + enum | | **`[ppb]`, `[ppm]`, `mg/m3`, `ug/m3`** |
| PM1, PM2.5, PM4, PM10 | mass concentration | `pm1`, `pm25`, `pm4`, `pm10`: μg/m³ | float + enum | HomeKit `PM2.5 Density`; PurpleAir, AirGradient μg/m³; Z-Wave μg/m³ or mol/m³ | **`ug/m3`** |
| Particle count | number concentration | (none) | | AirGradient `pm003Count` (per 0.1 L, unverified) | **`/dL`** or `{particles}/dL` |
| VOC concentration | mass concentration or fraction | `volatile_organic_compounds`: μg/m³, mg/m³; `volatile_organic_compounds_parts`: ppm, ppb | TVOC float + enum | HomeKit VOC Density | **`ug/m3`, `[ppb]`** |
| VOC index, NOx index | relative device index | (generic, no unit) | | Sensirion 1 to 500; AirGradient `tvocIndex`, `noxIndex` | n/a: an index metric, not a unit |
| AQI | derived index | `aqi`: no unit | `LevelValue` enum | HomeKit Air Quality 0 to 5; EPA AQI 0 to 500 | n/a: an index; HomeKit's is categorical |
| NO2, NO, N2O, O3, SO2 | fraction or mass concentration | ppb, ppm, μg/m³ | float + enum | | **`[ppb]`, `ug/m3`** |
| Radon | activity concentration | `radon`: Bq/m³, pCi/L | float + enum | | **`Bq/m3`, `pCi/L`** |
| Formaldehyde | concentration | (none in HA) | HCHO cluster | | as CO |
| Sound level | level (log) | `sound_pressure`: dB, dBA | | Netatmo `Noise` dB | `dB[SPL]` exists in UCUM (unverified); A-weighting has no UCUM atom, so `dB` + annotation **`{A}`** |
| Signal strength | level (log, power ref.) | `signal_strength`: dB, dBm | | AirGradient `wifi` dBm; Traccar `rssi` | `dB[mW]` (UCUM, unverified) |
| Battery level | ratio | `battery`: % | **half-percent** 0 to 200 | HomeKit uint8 %; Netatmo `battery_percent`; Smartcar **fraction 0 to 1** | `%`, `1` |
| Active power | power | `power`: mW, W, kW, MW, GW, TW, BTU/h | `ActivePower` mW int64; ZCL W x mult / div | DSMR kW; Victron W; Enphase W; Tesla W; ESPI uom 38 | **`W`, `kW`, `mW`, `[Btu_IT]/h`** |
| Apparent power | power | `apparent_power`: mVA, VA, kVA | mVA | | **`V.A`** |
| Reactive power | power | `reactive_power`: mvar, var, kvar | mvar | ZCL kvar; ESPI uom 63 | UCUM has no `var` (unverified); use **`V.A`** + `{reactive}` |
| Power factor | ratio | `power_factor`: %, none | int x 10000 (unverified) | Z-Wave meter scale | `1`, `%` |
| Energy | energy | `energy`: J, kJ, MJ, GJ, mWh, Wh, kWh, MWh, GWh, TWh, cal, kcal, Mcal, Gcal, thm | mWh int64 | DSMR kWh `F9(3,3)`; Enphase Wh; Tesla Wh; Victron kWh; ESPI uom 72 Wh x 10^n; ZCL kWh x mult / div | **`W.h`, `kW.h`**, `J`, `kcal`, **`[thm_us]`** (unverified) |
| Reactive energy | energy | `reactive_energy`: varh, kvarh | mvarh | ESPI uom 73 | as reactive power |
| Energy storage (battery capacity) | energy | `energy_storage` | | Tesla `EnergyRemaining` | `kW.h` |
| State of charge | ratio | `battery` % | | Tesla `Soc`, `BatteryLevel`; Victron `/Dc/Battery/Soc`; Enphase `soc.percent`; Smartcar fraction | `%` |
| Voltage | electric potential | `voltage`: μV, mV, V, kV, MV | mV int64 | DSMR V; ZCL V x mult / div | **`V`, `mV`** |
| Current | electric current | `current`: μA, mA, A | mA int64 | DSMR A; Traccar | **`A`, `mA`** |
| Frequency | frequency | `frequency`: mHz to GHz | int64 (mHz, unverified) | ESPI uom 33 | **`Hz`** |
| Gas volume (meter) | volume | `gas`: CCF, ft³, m³, L, MCF | | DSMR gas m³ (temperature-corrected NL, uncorrected BE); Z-Wave gas m³, ft³; ZCL m³, ft³, ccf, mcf | **`m3`, `[cft_i]`**, `L` |
| Water volume (meter) | volume | `water`: CCF, ft³, m³, gal, L, MCF | | Z-Wave water m³, ft³, US gal; DSMR water m³ | **`m3`, `[gal_us]`**, `L` |
| Volume flow | volume per time | `volume_flow_rate`: m³/h, m³/min, m³/s, ft³/min, L/h, L/min, L/s, gal/h, gal/min, gal/d, mL/s | `10 x m³/h` | | **`m3/h`, `L/min`** |
| Precipitation depth | length | `precipitation`: in, mm, cm | | Netatmo mm; WU, Ecowitt in; Davis clicks (0.01 in or 0.2 mm), in, mm; NWS `wmoUnit:mm`; Open-Meteo mm | `mm`, **`[in_i]`** |
| Precipitation intensity | length per time | `precipitation_intensity`: in/d, in/h, mm/d, mm/h | | Ecowitt `rainratein`; Davis `rain_rate_last_in` | **`mm/h`, `[in_i]/h`** |
| Wind speed | speed | `wind_speed`: Beaufort, ft/s, in/s, m/min, m/s, km/h, kn, mph, mm/s | | Netatmo km/h; WU mph; NWS km/h; Open-Meteo km/h (or chosen); Traccar knots | **`m/s`, `km/h`, `[kn_i]`, `[mi_i]/h`**; Beaufort is an ordinal scale, not a unit |
| Wind direction | plane angle (circular) | `wind_direction`: °, state class `measurement_angle` | | degrees everywhere; no source sent compass points | **`deg`** with circular statistics |
| Speed (vehicle) | speed | `speed`: as wind, plus volumetric flux | | OBD km/h; Traccar knots; Tesla `VehicleSpeed` (mph, unverified); ANT+ 0.001 m/s; LEV 0.1 km/h | as wind |
| Course or heading | angle (circular) | (none) | | Traccar `course` | `deg` |
| Distance, odometer | length | `distance`: mm, cm, m, km, in, ft, yd, mi, nmi | | OBD 0.1 km; Smartcar km; Traccar m; LEV 0.01 km; ANT+ wrapping m | `m`, `km`, **`[mi_i]`, `[nmi_i]`** |
| Range (EV, fuel) | length | `distance` | | Smartcar, Tesla `EstBatteryRange` | as distance |
| Fuel level | ratio or volume | (`volume_storage`) | | OBD `100A/255` %; Smartcar % + L; Traccar `fuel` L, `fuelLevel` % | `%`, `L`, **`[gal_us]`** |
| Fuel used, fuel rate | volume, volume per time | (`volume`, `volume_flow_rate`) | | Traccar `fuelUsed` L, `fuelConsumption` L/h | `L`, `L/h` |
| Energy per distance | energy per length | `energy_distance`: kWh/100km, Wh/km, mi/kWh, km/kWh | | | **`W.h/km`**; mi/kWh is the reciprocal dimension and needs a conversion rule |
| Engine speed | frequency (rotational) | (none) | | OBD rpm; Traccar `rpm` | **`{rpm}`** or `/min` |
| Engine hours, runtime | duration | `duration`: d, h, min, s, ms, μs | | Traccar `hours` (ms) | `h`, `s` |
| Altitude | length | `distance` | | Traccar m; Netatmo `place.altitude` | `m` |
| Position | geographic point | (`device_tracker` latitude and longitude attributes) | | Traccar, Smartcar, Tesla `Location` | not a quantity: a coordinate pair (see section 11) |
| Conductivity (soil, water) | conductivity | `conductivity`: S/cm, mS/cm, μS/cm | | | **`S/cm`, `uS/cm`** |
| pH | dimensionless | `ph`: none | | | `[pH]` |
| Weight (scale, hopper) | mass | `weight`: g, kg, mg, μg, oz, lb, st | | | `kg`, `[lb_av]` (already in the health plan) |
| Data size, data rate | information | `data_size`, `data_rate`: bit to YiB, bit/s to GiB/s | | | **`By`, `bit`, `By/s`** |
| Monetary (energy cost) | money | `monetary`: ISO 4217, state class `total` | | ESPI `cost` (hundred-thousandths); Opower cost statistics | not a quantity: substrate's `money` type |
| Area | area | `area`: m², cm², km², ... ac, ha | | | `m2`, **`[acr_us]`, `har`** |
| Blood glucose | (health) | `blood_glucose_concentration`: mg/dL, mmol/L | | | (already in the health plan) |

What the catalogue shows:

1. **Every HA device class except `aqi`, `ph`, `enum`, `date` and `timestamp`
   has a physical dimension.** The health plan's `dimension` plus a unit
   table covers them. The table needs roughly 60 more codes: power, energy,
   electrical units, pressure variants, flow, concentration in ppm and ppb,
   imperial length and volume, speed.
2. **Three quantities are not linear-convertible units:**
   - Beaufort, which is ordinal
   - decibel levels, which are logarithmic, with a reference (SPL, mW) and a
     weighting (A)
   - Matter's log-encoded illuminance, which a canonicaliser decodes to lux
     before storage
3. **Three are indices with no unit:**
   - AQI, an absolute piecewise formula of a window
   - the VOC and NOx indices, relative to the device's own 24 hours
   - HomeKit's 0 to 5 air quality, which is categorical

   These belong with the health plan's vendor scores: a metric with an
   `algorithm` and no cross-vendor equivalence.
4. **Two are not scalars:** geographic position (a point) and wind velocity,
   which is speed plus a circular direction.
5. **HA's unit spellings are not UCUM** (`μg/m³`, `m³`, `°C`, `inH₂O`,
   `BTU/h`, `thm`). Neither are Haystack's, ZCL's enums, ESPI's numeric codes,
   OpenADR's `KWH` or WMO's `km_h-1`. Each source vocabulary needs an alias
   table, and those tables belong in the adapter packages, not the engine.
   The engine keeps one alias table for common input forms, as the health
   plan already proposes.

---

## 9. Deliverable (c): categorical observations and time in state

### 9.1 How systems store categorical states

| System | Representation | Stored as | Time in state |
| --- | --- | --- | --- |
| Home Assistant | `binary_sensor` (`on`, `off`, + `device_class` meaning); `enum` sensor with `options`; domain states (`lock`: locked, unlocked, jammed...; `climate.hvac_action`; `person`: home, not_home, zone name) | `states` rows, one per change; `last_changed` is the edge | `history_stats` (`time` h, `ratio` %, `count`) computed on read from `states`. **Nothing in long-term statistics**: enum and binary sensors get no hourly rows, so after 10 days time in state is gone unless a `history_stats` sensor was recording a number all along (unverified for "no rows": inferred from `DEVICE_CLASS_STATE_CLASSES[ENUM] = set()`) |
| Matter | Boolean State `StateValue` + `StateChange` event; Occupancy bitmap + `OccupancyChanged`; Door Lock `LockState` (unverified) | attribute (current) + event log with event numbers | none on the device |
| ZCL | discrete attributes, reported on every change | attribute | none |
| HomeKit | uint8 enum characteristic | current value | none; Eve logs change entries and repeats them every 10 minutes |
| Google SDM | trait fields `ThermostatHvac.status`, `Connectivity.status` | events with `timestamp`, unordered | none |
| Z-Wave | Notification CC events (door open `0x16`, closed `0x17`) and Binary Sensor | events | none |
| Haystack | `kind: Bool` or `Str` point with `hisMode: cov` | (ts, val) on change; the value holds until the next ts | by reading `cov` history; SkySpark has `hisFoldings` (unverified) |
| OGC STA / O&M | `OM_TruthObservation`, `OM_CategoryObservation` (`result` = a term URI) | one observation per reading | none defined |
| Traccar | `ignition`, `motion` attributes on positions **and** `ignitionOn`, `ignitionOff`, `deviceMoving`, `deviceStopped` events | both levels and edges | `summary` reports engine hours from the `hours` counter; `trips` and `stops` are state sessions |
| Tesla telemetry | `ChargeState`, `DetailedChargeState`, `Gear` enum fields, sent on change | field + `created_at` | none |
| DSMR | `96.14.0` tariff | every telegram (1 s) | the utility derives it from the tariff registers (separate counters per tariff) |
| Prometheus | one 0/1 gauge per option (HA exporter) or an `_info` metric | sampled at scrape | `avg_over_time(gauge[1d])` x duration, at scrape resolution |
| OpenTelemetry | no categorical point; a 0/1 gauge per state, or an attribute | | |

### 9.2 What time in state needs

1. **Edges, not samples.** A state holds from its change time until the next
   change. Haystack's `cov` mode says exactly that. HA's statistics treat a
   numeric state the same way ("no interpolation"). Sampling a state on a
   clock (Prometheus, Eve's 10-minute repeat) loses the edge times and gives
   time in state only at the sampling resolution.
2. **The state at the window start.** Time in state over `[t0, t1)` needs the
   last change before `t0`. HA's history API has `include_start_time_state`
   for this, and its default is true.
3. **A staleness rule.** A door sensor whose battery died reports `closed`
   forever. HA has `expire_after` (MQTT) and the `unavailable` state, and
   Sparkplug has `STALE` quality and DEATH messages. Time in state needs a
   maximum hold, or an explicit `unavailable` state that counts as its own
   bucket.
4. **A closed vocabulary per metric.** HA's `options`, HomeKit's fixed enum
   values and Matter's enums all do this. Aggregates group by the value, so
   the value set must be stable and versioned. That is the health plan's
   `retired` and `renamedFrom` machinery for enum values.
5. **Rollup by state.** A 15-minute rollup row for a categorical stream needs
   `duration_ms` per state value and `transitions`, not `mean` or `m2`.
   `ratio`, `durationIn` and `count` of entries then come from rollups. This
   is a different rollup row shape from the numeric one.
6. **Edges as events.** Several systems (Traccar, Matter events, Z-Wave
   notifications) send the edge as an event, and for momentary things (a
   doorbell press, a motion trigger) there is no lasting state at all. A
   motion sensor's "detected" is a level with a vendor-defined hold time
   (Matter `HoldTime`). A doorbell press is a point event with no duration.
   The model needs both: a state stream, and plain events counted per bucket.

---

## 10. Deliverable (d): what a shared model must absorb

| Difference | Health data today | Home, energy, vehicle, weather data | Evidence |
| --- | --- | --- | --- |
| **Counters are primary, and they reset** | rare (Terra steps, FIT distance); the health plan differences them at ingest and keeps no `counter` form in the index | the main form of every energy, gas and water meter, odometer and rain gauge. Resets come in at least six kinds: (1) none, for lifetime counters (OBD odometer, Ecowitt `totalrainin`); (2) a meter swap or manual reset (DSMR new `96.1.1`, `MeterCCReset`, Victron `Yield/User`); (3) a register rollover at a dial maximum (ESPI bulkQuantity; ANT+ 8-bit wraps at 64 s and 256 m); (4) calendar resets in **local** time (`dailyrainin`, Davis monthly and yearly, Netatmo `sum_rain_24`, HA `utility_meter` cycles); (5) event resets (Ecowitt `eventrainin`, Davis `rain_storm`); (6) an unknown reset detected by a drop (HA's `< 0.9 x previous` heuristic, Prometheus "any decrease") | sections 1.4, 3.1, 3.5, 4, 5 |
| **A reset epoch is metadata on the value** | n/a | OTel `StartTimeUnixNano`, HA `last_reset`, Matter `CumulativeEnergyReset`. A counter value without its epoch cannot be differenced across a reset without a heuristic | 6.1, 1.4, 3.1 |
| **Rolling-window amounts overlap** | rare | WU `rainin` (past 60 min, every upload), NWS `precipitationLast3Hours`, PurpleAir `pm2.5_10minute`, Davis `wind_speed_avg_last_10_min`. These are summaries over a trailing window that moves with every reading. Summing them double counts; they are summaries with a `period` of "the trailing N minutes" | 5.6, 5.8, 5.9 |
| **Interval timestamps differ by convention** | mostly start and end | stamped at the **end** (Enphase `end_at`, Haystack `consumption`, Davis archive `ts`), at start and duration (ESPI, OpenADR), at the **middle** (Netatmo getmeasure without `real_time`), or as "the preceding hour" (Open-Meteo). Canonicalisation must turn all of them into `[at, endsAt)` | 4.1, 4.6, 5.5, 5.7, 5.9 |
| **Device-defined precision and scaling** | vendor JSON in floats or decimal strings | Matter and ZCL int16 centidegrees, half-percent battery, a log-encoded lux value, mW/mV/mA int64, ZCL multiplier and divisor attributes, Z-Wave precision per report, ESPI `powerOfTenMultiplier`, Davis rain clicks, OBD `100A/255`. All of this is decoding that belongs in the adapter, and the decoded value's real resolution (0.01 °C, 0.2 mm per tip) is a property of the source, not the number | 3, 4.9, 5.1, 5.9 |
| **Many sources per metric, at once, in different places** | a few sources (watch, ring, strap), all observing one person; a priority picks one | a house has tens of sensors. Sixty is plausible for an HA install, but I found no survey figure, so it is unverified. Ten temperature sensors in ten rooms are **ten different subjects**, not ten competing sources for one truth. A priority policy is wrong here; grouping by subject is right | 2.4 |
| **The subject is not the user** | always the user, implicitly | a room, a house, a car, a meter point, a plant, a battery, a grid connection, a weather grid cell. A subject can also be indirect: an outdoor sensor observes "outside the house"; Open-Meteo observes a grid cell near you. This is SOSA/STA's feature of interest, and OMS 2023's proximate and ultimate split | 2 |
| **Location and moving subjects** | GPS on workouts only | vehicles and trackers emit positions (Traccar, Smartcar, Tesla `Location`), and a sensor can be moved between rooms. STA keeps `Location` and `HistoricalLocation` on the `Thing`; HA keeps an area on the device. The subject of a stream can change over time | 2.1, 5.3 |
| **Sampling on change, not on a clock** | mixed; CGM and HR are roughly periodic | most home protocols report on change, gated by a minimum interval (Matter subscriptions, ZCL reportable change, Sparkplug report by exception, Tesla `interval_seconds` + change, HA states). A quiet sensor writes nothing for hours. That makes (a) the time-weighted mean, not the sample mean, the correct average; (b) the state at the window start necessary; (c) a regular-interval packed trace a poor fit, so the irregular `offsets` form becomes the common case | 1.4, 3, 4.7 |
| **Several clocks per value** | device time, sometimes upload time | device time, fix time and server time (Traccar); OEM, fetch and delivery time (Smartcar); systime since boot with no UTC (Matter); none at all, so the reader stamps it (OBD, ANT+, AirGradient, WU `dateutc=now`); the telegram time plus the gas meter's own time (DSMR); a DST flag rather than an offset (DSMR `S`/`W`) | 3.1, 4.4, 5 |
| **Retention and resolution tiers** | the plan keeps everything raw; retention is "a setting for later" | HA purges raw states and 5-minute statistics after 10 days and keeps hourly mean, min and max, or state and sum, **forever**. A per-circuit energy monitor at 1 s is 86,400 samples per circuit per day; 16 circuits is about 1.4 million per day (my arithmetic). That is 30 times the health plan's heavy user (45,000 per day). Users accept that raw home data expires and only rollups are permanent | 1.5, 4.8 |
| **Forecasts beside observations** | none | Open-Meteo forecasts, OpenADR `FORECAST`, Tesla and Enphase production forecasts. The phenomenon time is in the future, and the result time is when the forecast was issued. HA's `measurement` explicitly excludes "a prediction of the future" | 1.2, 2.1, 5.5 |
| **Direction and sign conventions** | n/a | import vs export and charge vs discharge. Separate counters per direction (DSMR `1.8` vs `2.8`, ESPI `flowDirection`, Matter `Imported` vs `Exported`, Z-Wave rate type), or a signed power whose sign each vendor defines differently (Tesla `battery_power` positive = discharging is inferred; ZCL positive = delivered to the premises). Flows between nodes (Tesla `battery_energy_imported_from_solar`) are edges of a graph, not readings of one meter | 4 |
| **Tariff and time-of-use partitions** | n/a | DSMR keeps one counter per tariff, ESPI has `tou` and `consumptionTier`, HA `utility_meter` has `tariffs`. One physical quantity is split into partitions by a categorical | 4.4, 4.9 |
| **Quality flags and completeness** | sometimes (lab interpretation) | NWS `qualityControl`, Sparkplug `Quality` (BAD, GOOD, STALE), ESPI `ReadingQuality` (estimated, validated, revenue quality), PurpleAir `confidence`, Enphase `devices_reporting`, Sparkplug `is_historical`. An estimated utility interval replaced later by a validated one is a routine revision | 3.6, 4.6, 4.9, 5.6, 5.8 |
| **Circular quantities** | none | wind direction, vehicle course. HA added `measurement_angle` and a circular mean with `mean_weight`. An arithmetic mean of 350° and 10° is 180°, which is wrong | 1.2, 1.4 |
| **Relative and derived indices** | vendor scores (readiness, recovery) | AQI from a 24-hour window; Sensirion VOC index relative to the device's own last 24 hours; HomeKit 0 to 5 air quality. The same treatment as vendor scores fits | 5.8 |
| **Raw and corrected values side by side** | lab vs derived molar | AirGradient `pm02` vs `pm02Compensated`, PurpleAir `cf_1` vs `atm` vs `alt`, NL gas corrected vs BE uncorrected. Two metrics, or one metric with a `method`, linked by `derivedFrom` | 4.4, 5.8 |

---

## 11. Deliverable (e): does the health model fit?

**Short answer: yes for the value layer, no for the identity layer.** The
`quantity` type, the `form` of a value, the packed `trace`, the derived index
and the aggregate read all carry over. Every system surveyed reduces to
instants, interval amounts, counters, categorical states and summary rows.
The `metric` record is HA's `device_class` and STA's `ObservedProperty`, and
`source` is STA's `Sensor`.

What the health model lacks is in the identity layer: **what was observed**,
and **the stream that binds metric, source and subject**. Below that come four
value-level additions: counter epochs, categorical values, hold and
interpolation semantics, and circular statistics.

### 11.1 What maps directly

| Health plan | Home Assistant | OGC STA / SOSA | Haystack / Brick | Notes |
| --- | --- | --- | --- | --- |
| `metric` | `device_class` (+ the allowed units and state classes) | `ObservedProperty` | quantity tags, `brick:hasQuantity` | The catalogue grows by about 60 records (section 8). A metric's `dimension` replaces HA's `DEVICE_CLASS_UNITS`, and its default `form` replaces `DEVICE_CLASS_STATE_CLASSES` |
| `source` | device registry + integration | `Sensor` (+ `Platform`) | `equipRef`, `brick:Point` | Add `resolution`, `tolerance` and `range` from `ssn-system` and Matter's `Tolerance` as optional source facts |
| `observation` | one `states` row | `Observation` | `hisRead` row | as is |
| `trace` | a run of `states` rows | `dataArray` of one `Datastream` | `hisRead` grid | the irregular `offsets` form is the common case here |
| `form: instant` | `measurement` | `OM_Measurement`, instant `phenomenonTime` | `hisMode: sampled` or `cov` | needs a declared hold rule (11.3) |
| `form: interval` | `total` + `last_reset` per period; external statistics | period `phenomenonTime` | `hisMode: consumption` | canonicalise end-stamped and middle-stamped buckets to `[at, endsAt)` |
| `form: counter` | `total_increasing`, `total` | (none) | `hisTotalized` | must become storable and indexable (11.3) |
| `form: summary` | `statistics` rows | (none) | `brick:aggregate` | needs several statistics per row, and trailing windows (11.3) |
| `quantity` (UCUM) | display units + converters | UCUM or QUDT URI | Haystack units, QUDT | one engine table, with alias tables in adapters |
| 15-minute rollups | 5-minute `statistics_short_term` + hourly `statistics` | (none) | (none) | HA proves the design: one row per stream per bucket with mean, min, max, or state and sum |
| aggregate read | `recorder/statistics_during_period` (`period`, `types`, `units`) | OData `$filter` only, no aggregation (unverified for STA 2.0) | `hisRead` + folds | substrate's read is richer than HA's (zones, source policy, budgets) |

### 11.2 What must be added to the identity layer

**1. A subject (feature of interest).**

- **The change**: an optional `subject` reference on `observation` and
  `trace`, pointing at any record: a `place` (room, house), a `vehicle`, a
  `plant`, a `meter point`, a `person`.
- **The default**: absent means the repository's user, which keeps every
  health record valid unchanged.
- **Identity**: the subject is part of a trace's identity: (metric, source,
  subject, period).
- **Which subject**: the ultimate one, in OMS 2023 terms. The proximate
  feature, such as "the air at the sensor", stays implicit.
- **Why**: without it, "average temperature of the kitchen this winter" has no
  key. The kitchen's sensor may be replaced in March, and a source-keyed
  series breaks there.
- **Policy follows from it**: the source policy becomes "priority among
  sources **for the same subject**". Ten room sensors are ten subjects and are
  never deduplicated against each other.

**2. A stream record.**

- **The pattern**: STA's Datastream, SOSA 2023's ObservationCollection, HA's
  `statistics_meta` and Haystack's point all have one. It is one persistent
  record for "this metric, from this source, about this subject, in this unit,
  with this form and these reset semantics".
- **What it would carry**: the things the health plan currently repeats on
  every trace and observation, such as `unit`, `form` and the counter's reset
  rule. It also carries the things that belong to neither metric nor source,
  such as `stateClass`, `options`, the sign convention, the tariff partition
  and the display name.
- **The cost**: the health plan's principle 6 puts meaning on the value so
  that "editing a metric record cannot change what an indexed number means".
  A mutable stream record would weaken that.
- **The way through**: keep `form`, `unit` and the reset epoch **on every
  value**, as the health plan does. Let the stream hold only identity,
  defaults and navigation. Traces and observations reference it, and the
  derived index keys on it.
- **Naming** is an open question for the owner: `series` is taken and
  `channel` is used for waveform channels. Candidate words are `stream` and
  `datastream`.

**3. Location.**

- **Fixed subjects**: a subject record carries its own location (a `place`
  with an address or coordinates, nested places for room in house).
- **Moving subjects**: a vehicle's position is itself a stream, a trace with
  `lat` and `lon` channels or a `geopoint` value type. It is not a field on
  every observation.
- **Moved sensors**: STA's `HistoricalLocation` covers a sensor that moves
  between rooms. In substrate that is a time-ranged `source` to `subject`
  assignment, or a new stream after the move.

**4. A grouping layer for navigation.**

- HA's device classes are flat, while Haystack and Brick have hierarchies
  (air quality > particulate > PM2.5; electrical > power > active power).
- The health plan's `metric.group` covers this, as long as the package ships
  the groups for the new domains.

### 11.3 What must be added to the value layer

**5. Counters as a stored and indexed form.**

- **The problem**: the health plan says a counter is "differenced into
  `interval` amounts at ingest" and has no reducers. That works for Terra
  steps. It fails for a home: the counter **is** the truth (DSMR `1.8.1`,
  ESPI bulk, Matter cumulative, odometers), the interval amounts are derived
  from it, and how they are derived depends on reset handling.
- **Proposed fields on a counter value or trace**:
  - `epoch`: the reset time or epoch id, after OTel `StartTimeUnixNano`, HA
    `last_reset` and Matter `CumulativeEnergyReset`
  - an optional `wrap` modulus (`256` m for ANT+, the dial maximum for ESPI
    bulk)
  - `resetRule`: `none`, `explicit` (the epoch changes), `drop` (HA's 10%
    heuristic, declared and versioned) or `calendar` (with its zone and cycle)
- **The engine's job**: compute `increase` per bucket within an epoch, as the
  one legal reducer besides `last`, and project the increases into the
  interval index. Then "kWh per day" is a sum of interval rows whether the
  source sent a counter or amounts.
- **The rule**: the counter stays on the record, and the differences are
  derived, so a fix to the reset rule re-derives them. This matches HA, which
  keeps `state` and `sum` and computes `change` on read.

**6. Categorical values.**

- **The form**: a fifth `form`, `state`, whose value is a code from a closed
  set declared on the metric or stream (HA `options`). Booleans are a
  two-value set with names (`open` / `closed`, not `on` / `off`).
- **Hold**: `state` always holds until the next value, as Haystack `cov`
  does.
- **Reducers**: `durationIn` (per value), `ratio`, `count` (entries into a
  value), `last` and `first`.
- **Rollups**: a categorical rollup row holds per-value durations and a
  transition count.
- **Events**: point events with no duration (doorbell press, `geofenceEnter`)
  are `count`-only and need no new form. They are observations with
  `form: instant` and no value, or the existing record kinds.
- **What exists today**: the health plan's `durationIn` with a hold rule
  covers numeric thresholds only. The lab plan's `codedValue` is the right
  value shape, but it is not indexed.

**7. Hold and interpolation semantics per stream.**

- **The three modes**: `step` (HA, Haystack `cov`), `linear` (Haystack
  `sampled`) or `none` (independent samples, such as a weighing).
- **Staleness**: a `maxHold` duration (HA `expire_after`) after which the
  value becomes unknown.
- **What the time-weighted mean needs**: (a) the value carried into each
  rollup bucket from before it; (b) `first` and `last` in the rollup row,
  which the health plan already has; (c) the hold mode.
- **The rollup consequence**: `covered_ms` must mean "time the value was
  known", not "time between first and last sample".

**8. Summary rows with several statistics, and trailing windows.**

- **Several statistics**: an HA statistics row, a VRM stats row and a
  Netatmo getmeasure row each carry mean, min and max (or state and sum)
  together. One observation per statistic is 3 to 5 records per stream per
  hour. A summary **trace** with one array per statistic, for one period
  width, fits better and gives the "keep hourly rollups forever, raw for N
  days" policy a stored form.
- **Trailing windows**: rolling-window summaries (`rainin`, `pm2.5_10minute`)
  need `period: {trailing: PT1H}`, so the aggregate read never sums them.

**9. Circular statistics.**

- **The flag**: `deg` gets a `circular: true` dimension flag.
- **The reducers**: `avg` becomes the circular mean (HA's `mean_weight`
  approach is mergeable across rollups); `min`, `max`, `sum` and `stddev` are
  refused.

**10. Forecasts.**

- **The choice**: either keep forecasts out of the observation model, as
  mirrors only, or add `predicted: true` with an `issuedAt` (STA
  `resultTime`).
- **Recommendation**: keep them out of the samples index by default, because
  HA's `measurement` definition and every aggregate assume observed values.
- This is an open question for the owner.

**11. Quality.**

- **The field**: an optional `quality` enum on a value: `valid`, `estimated`,
  `questionable`, `stale`, `validated`.
- **Revisions**: an estimated utility interval later replaced by a validated
  one is an ordinary trace revision, which the health plan already supports.

**12. Direction and partitions.**

- **No new mechanism**: these are metric choices: separate metrics for import
  and export, a declared `sign` on net power metrics, and tariff as a
  categorical **label** on an interval amount.
- **The engine cost**: the aggregate read needs `groupBy` on a declared
  categorical property of the stream or value. The health plan caps
  `groupBy` cardinality but does not define it.

### 11.4 What does not need to change

- **`quantity` as `{value, unit, comparator?}` with exact digits.** Scaled
  integers decode exactly into decimals (centidegrees, `powerOfTenMultiplier`,
  Z-Wave precision), so exactness costs nothing.
- **The 15-minute rollup grain with the offset in the key.** HA uses 5-minute
  and hourly grains, and 15 minutes is also the utility interval (ESPI 900,
  Enphase, the Belgian peak window). The local-time calendar resets
  (`dailyrainin`, monthly meters) make the offset key necessary, not optional.
- **Mirrors in the vendor's units, canonicalisation in adapter functions.**
  Every protocol here needs decoding (scaling, multiplier and divisor, log
  lux, rain clicks, knots, a fraction battery), and none of it belongs in the
  engine.
- **Budgets on the aggregate read.** At home-energy rates of 1.4 million
  samples per day, `maxSamples` is the limit users will meet first.

### 11.5 Recommendation

Widen the model. Do not fork it. Rename the package from `health` to a neutral
observation package, with `health` and `home` as metric catalogues on top.
Make the identity-layer additions (subject, stream) and the counter and
`state` forms part of the **first** engine decision record, not later phases,
because each one changes the samples index key or its row shape:

- the subject goes into the trace identity and the index key
- the state rollup row is a different shape from the numeric one
- counter epochs change what the interval projection derives

Adding them after the index ships means a reprojection of every repository.
The circular flag, quality, forecasts and `groupBy` can follow.
