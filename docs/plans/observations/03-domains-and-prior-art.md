# 03. Which domains are observations, which are not, and what general personal data stores learned

Research for the question: should the health proposal's model (`metric`, `observation` with a `form`,
packed `trace`, `source`, `quantity` with UCUM units, the derived `samples` index with rollups, and the
`aggregate` read; `docs/plans/health-data.md` sections 1, 4, 5, 6, 10) widen from health to every
observed quantity a person accumulates?

Date: 2026-10-08. "Checked" means fetched or searched today and read from the source named. "Recalled"
means from memory and not checked today. "Estimate" means arithmetic on checked numbers. Anything else
unverifiable is marked "unverified". Working files (extracted PDF text) are under
`/tmp/health-research/v2/raw-domains/`.

## Summary

- Most candidate domains are observations in the standards' sense: a value of a property of some thing
  at some time, by some procedure. The health model already has four of the five parts (property =
  `metric`, procedure = `source`, phenomenon time = `at`/`endsAt`, result = `quantity`). It lacks the
  fifth: **the thing observed**. Every domain outside health observes something other than the user (a
  room, a car, a plant, a child, an account, a station, a place). It also lacks **location**,
  **result time**, **quality** and a **categorical value**.
- Adding `subject` and `location` serves about half the list. The rest needs four more, small and
  concrete: a `state` form for categorical step functions (door open, away from keyboard, charging), a
  `counter` form that is stored and survives resets (meters, odometers), a `geopoint` value type with a
  packed `track` for positions, and a coded or ordinal value on `observation` for self-reports and
  habits. Circular mean for angles and a currency dimension are two more, smaller.
- Four domains are not observations and should stay typed record domains: spending (transactions),
  media consumption (scrobbles, reading sessions), parcel shipping (a state machine), and raw app usage
  events (ActivityWatch spans). Their counts and sums are aggregates over typed records, which the
  proposal's `property` selector already plans. One domain does not fit at all: computer and
  infrastructure metrics, by volume and label cardinality.
- Prior art splits cleanly. The systems that accepted anything (HAT's "any well-formed JSON", Solid's
  RDF documents, Open Humans' files, Nomie's note strings, Google Fit's custom data types) pushed all
  meaning onto each reader, and the one with a platform behind it (Google Fit) deprecated custom types
  because only the app that wrote them could read them. The systems that are alive and loved stayed
  narrow on one axis: Exist.io on grain (one value per local day), Apple Health on subject (always the
  user), Home Assistant on purpose (one home, declared value semantics per series), ActivityWatch and
  Dawarich on domain. Gyroscope started as "track everything" and now positions itself on "your mind,
  your food and your fitness".
- Recommendation: widen the **engine primitives** (value types, forms, trace, samples index, aggregate
  read, a `subject` reference) to every domain, keep the **metric catalogue and the session kinds**
  per domain package (`health`, `home`, `vehicle`, `place`/`weather`, `finance`), and keep event
  domains typed. This is "generic scalars, typed sessions", which the health proposal already chose
  inside health (its section 6, "Generic plus typed"), applied one level up.

## Part 1. Candidate domains

Vocabulary used below, from the health proposal section 5.1: `instant` (a reading at a moment),
`interval` (an amount over `[at, endsAt)`), `counter` (a cumulative total), `summary` (a statistic over
a stated period). Two shapes are added here because the domains need them: **state** (a categorical
value held until the next change: a door, a presence, a charging state) and **event** (a thing that
happened, with its own identity and payload: a scrobble, a transaction, a diaper change).

### 1.1 Home and building sensors

- **What the data is.** Temperature, humidity, CO2, VOC, PM2.5, illuminance (instant); presence,
  motion, door and window contact, lock state (state); power in W (instant); energy in kWh, gas and
  water in m3 (counter that resets or is replaced); solar production power (instant) and energy
  (counter). Home Assistant's sensor contract names exactly this split: `state_class` is
  `measurement` ("a measurement in present time, not a historical aggregation"), `total` ("can both
  increase and decrease, for example, a net energy meter") or `total_increasing` ("a monotonically
  increasing positive total which periodically restarts counting from 0"), with `last_reset` marking
  when a meter cycle began. Checked: https://developers.home-assistant.io/docs/core/entity/sensor/
- **Units.** Home Assistant's device classes carry fixed unit lists: CO2 in ppm, PM in µg/m³, energy
  in J through TWh plus cal and thm, pressure in a dozen units, and `monetary` with an ISO 4217
  currency as its unit. Checked, same page. All but `monetary` are UCUM-expressible; `ppm` is `[ppm]`
  in UCUM (recalled).
- **Subject.** The room, the meter, the inverter, the home. Never the user.
- **Location.** Static per subject (the home); a per-observation location is not needed.
- **Rate and volume.** Typically every 10 s to 5 min per sensor, or on change. Estimate: 30 entities
  at one reading a minute is 43,200 samples a day, the same order as the health proposal's heavy
  wearable user (45,000 a day). Home Assistant keeps raw states 10 days by default and keeps
  statistics forever: "The `states` and `statistics_short_term` tables are automatically purged after
  a predefined period (default is 10 days)". Checked: https://data.home-assistant.io/docs/statistics/
- **Identity.** An entity id (`sensor.living_room_co2`); maps to (metric, subject, source).
- **Sources.** Home Assistant (REST and websocket, external statistics import), Zigbee2MQTT, utility
  APIs, inverter clouds (Enphase, SolarEdge; not checked today).
- **Fit.** High for numeric channels with `subject`. The `counter` form must be **stored**, not only
  differenced at ingest as the health proposal says, because the meter reading is the fact and the
  consumption is derived; reset handling (`last_reset`, a meter swap) belongs in the form's rules.
  State channels need the new `state` form.
- **Adds:** `subject`; `state` form; stored `counter` with resets; an `enum`/coded value for states.

### 1.2 Vehicle telemetry

- **What the data is.** TeslaMate's `positions` row is a good primary sample of what one car emits:
  `date`, `latitude`, `longitude`, `elevation`, `speed`, `power`, `odometer`, three battery range
  estimates, `battery_level`, `usable_battery_level`, battery heater flags, `outside_temp`,
  `inside_temp`, climate settings and flags, and four tyre pressures `tpms_pressure_fl/fr/rl/rr`, each
  row belonging to a car and optionally a drive. Checked:
  https://raw.githubusercontent.com/teslamate-org/teslamate/main/elixir/lib/teslamate/log/position.ex
- **Shapes.** Speed, power, state of charge, temperatures, tyre pressures: instant. Odometer: a counter
  that never resets. Charging state, gear, locked: state. Drives and charges: sessions with summary
  values (distance, energy added, cost), exactly like a workout.
- **Subject.** The vehicle; for tyre pressure, a position on it (front left) as a qualifier or four
  metrics.
- **Location.** Every sample carries a position. Location is per observation here.
- **Rate and volume.** TeslaMate users report "roughly 1 GB of data is gathered per car and 30,000
  km" with streaming on, 95% of it in `positions`; one user has over 9 million rows after 60,000 km,
  another 13 million rows for two cars over 1.5 years. Checked by search:
  https://github.com/teslamate-org/teslamate/discussions/2437 and
  https://github.com/teslamate-org/teslamate/issues/573 . Estimate: 9 M rows over 60,000 km is about
  150 rows per km, about 1 to 2 rows a second while driving, with about 25 channels per row.
- **Fit.** Medium to high. Each channel is a trace with `subject` = the car; drives and charges are
  typed session kinds (`trip`, `charge`), not observations. One drive hour is about 25 traces (one per
  channel), which is acceptable but wasteful; a **multi-channel trace** sharing one time axis (OGC
  SensorThings' `MultiDatastream`, FIT's `record` message) would make it one record.
- **Adds:** `subject`; per-sample position (a `geopoint` channel or a `track`); optional multi-channel
  trace; typed `trip` and `charge` session kinds in a `vehicle` package.

### 1.3 Weather and air quality at a location

- **What the data is.** Temperature, humidity, pressure, wind speed and direction, precipitation, UV,
  AQI and pollutant concentrations. OpenAQ v3 organises locations, sensors, parameters (PM2.5, PM10,
  SO2, NO2, CO, O3, BC, relative humidity, temperature) and measurements with hourly, daily and yearly
  aggregations. Checked: https://docs.openaq.org/about/about
- **Subject.** A place (the feature of interest is the location itself), observed by a station or a
  model.
- **Shapes.** Instant for station readings; summary for hourly and daily aggregates (OpenAQ ships
  both); interval for precipitation; **forecasts**, where the result time (when the forecast was
  issued) precedes the phenomenon time (the hour it predicts). Wind direction is an angle: Home
  Assistant added `mean_type` `CIRCULAR` beside `ARITHMETIC` in 2025 because an arithmetic mean of 350
  and 10 degrees is wrong. Checked:
  https://developers.home-assistant.io/blog/2025/10/16/recorder-statistics-api-changes/
- **Rate and volume.** Hourly per place per variable; small.
- **Fit.** High, and high value as context for health (heat and sleep, pollen and symptoms). Exist.io
  ships weather attributes as daily values beside mood and sleep for exactly this reason (section
  2.1).
- **Adds:** `subject` pointing at a place; location on the subject; `resultTime` (or `issuedAt`) and a
  forecast marker; circular mean for angles.

### 1.4 Location history and GPS tracks

- **What the data is.** OwnTracks' `location` message: `lat`, `lon`, `tst` (epoch seconds of the
  fix), `acc` (metres), `alt`, `vac`, `vel` (km/h), `cog` (course), `batt`, `conn`, `tid`,
  `inregions`, `SSID`/`BSSID`, and `t` (what triggered the report). Plus `waypoint` (a named region)
  and `transition` (enter or leave). Checked: https://owntracks.org/booklet/tech/json/
- **Derived layers.** Dawarich stores points, trips, visits (suggested by clustering, confirmed by the
  user), places and areas, and statistics (countries, cities, distance); it imports Google Takeout,
  OwnTracks, Strava, GPX, GeoJSON and photo EXIF, on PostGIS. Checked:
  https://github.com/Freika/dawarich . Its changelog records performance work on a `raw_data` column
  on `points` and on rendering "millions of points". Checked by search:
  https://dawarich.app/docs/changelog/
- **Is lat/lon a quantity?** No. A position is one value in a coordinate reference system, not two
  independent quantities: averaging latitude and longitude separately is wrong across the antimeridian
  and meaningless as a statistic; distance is geodesic, not a difference; the useful filters are
  spatial (within a radius, inside a polygon, near a place), not ordered comparisons. The health
  proposal's catalogue lists `latitude` and `longitude` as instant metrics in degrees (section 10,
  "workout streams"); that works for storage and fails for every question asked of a track.
- **Subject.** The person (or the phone, or the car).
- **Rate and volume.** Passive phone reporting is minutes apart; active recording is 1 Hz. Volumes of
  millions of points over years are routine (Dawarich, TeslaMate above).
- **Identity.** (device, timestamp).
- **Fit.** Medium. Positions want a `geopoint` value type (`{lat, lon, alt?, accuracy?}`, WGS84) and a
  packed **track** (a trace whose values are geopoints, whose managed stats are a bounding box, a
  length and a duration, never a mean). Visits and places are records (sessions with a reference to a
  place). Distance per day is a derived interval observation. Location is the most sensitive data the
  store would hold after health; the deletion decision the health proposal defers applies here first.
- **Adds:** `geopoint` type; `track` (packed positions) or a trace with a geopoint value; a spatial
  filter operator; `accuracy` as quality.

### 1.5 Personal finance

- **What the data is.** Plaid's `balances` object: `available`, `current`, `limit`,
  `iso_currency_code` or `unofficial_currency_code`, and `last_updated_datetime` (only for one
  institution). Checked: https://plaid.com/docs/api/accounts/ . Plaid serves the current balance, not
  history (the fetched page's summary says so; the exact wording is unverified), so a balance history
  exists only if the store snapshots it.
- **Is money an observation?** A balance is: a value of a property (balance) of a subject (an account)
  at a time, from a source (the bank). A price is: a value of a security at a time. Net worth is a
  derived summary. Spending is not: a transaction is an event with identity, a counterparty, a
  category and a description, and the CONTEXT note says it is "already a records domain".
- **Units.** Currencies are not UCUM units, and conversion between them is not a fixed factor: the
  rate is itself a time series. Home Assistant gives `monetary` sensors an ISO 4217 unit and does not
  convert between them (checked, device-class list). Substrate's `money` is already
  `{amount (minor units), currency}` and refuses cross-currency comparison.
- **Rate.** One balance per account per day is the useful grain; prices daily close.
- **Fit.** Medium. Balances fit an observation with `subject` = account and `form: summary`
  (`statistic: last`, `period: day`) if the value can be money. Spending stays typed and is aggregated
  through the proposal's `property` selector ("money per month over an expenses kind", section 5.5).
- **Adds:** a currency dimension (each ISO 4217 code its own non-convertible unit) or `money` admitted
  as an observation value; `subject` = account.

### 1.6 Screen time, app usage and productivity

- **ActivityWatch.** Buckets carry `id`, `created`, `name`, `type`, `client`, `hostname`; "It is
  recommended to have one bucket per watcher and host. A bucket should always receive data from the
  same source." Events are `timestamp`, `duration` and `data`, "a JSON object, the schema of this
  depends on the event type" (`currentwindow`, `afkstatus`, `web.tab.current`,
  `app.editor.activity`). Heartbeats merge "adjacent events with identical data (within a `pulsetime`
  window)". "All timestamps are stored as UTC. Timezone information (UTC offset) is currently
  discarded." Checked: https://docs.activitywatch.net/en/latest/buckets-and-events.html
- **RescueTime.** The analytic API returns rows of (date, time spent in seconds, people,
  productivity -2 to 2) grouped by `resolution_time`, with "minute" meaning five-minute buckets, the
  finest available. Checked by search: https://www.rescuetime.com/rtx/developers
- **Apple Screen Time.** Not exportable: the Device Activity report extension runs in a sandbox that
  blocks network requests and shared storage, by design. Checked by search:
  https://developer.apple.com/forums/thread/727958 and
  https://developer.apple.com/forums/thread/817516
- **Shapes.** Raw: categorical intervals with a payload (app, window title, URL). Derived: minutes per
  app or category per bucket, which are interval amounts grouped by a dimension.
- **Fit.** Low for the raw events (high cardinality strings, sensitive titles, no numeric value), good
  for the derived amounts **if** an observation can carry a bounded grouping dimension (the app or the
  category). Otherwise each app becomes its own metric, which is cardinality in a different place.
- **Adds:** a typed `activityspan` kind for raw events; `state` form for AFK; a bounded `dimension`
  (OpenTelemetry attributes, Prometheus labels) on derived interval observations, with a cardinality
  cap.

### 1.7 Sleep and mood journaling, self-reports

- **What the data is.** Exist.io's value types include "Scale (1-9 as integer)" (code 8), boolean,
  duration in minutes, time of day as minutes from midnight or midday, percentage, string. Checked:
  https://developer.exist.io/reference/object_types/ . Nomie's trackers are tick, value, timer, range,
  picker and note (checked: https://www.nomie.app/). HealthKit added `HKStateOfMind` in iOS 18 with
  `kind`, `valence`, `labels` and `associations` properties (property names and platform checked via
  Apple's documentation JSON,
  https://developer.apple.com/tutorials/data/documentation/healthkit/hkstateofmind.json ; the valence
  range of -1 to 1 is recalled).
- **Shapes.** Ordinal (Likert) values, coded labels, booleans, free text; a few a day.
- **Fit.** High through `observation` if the value is not only a quantity. The health proposal puts
  coded values only on `labresult`. Averaging an ordinal scale is statistically contested but is what
  every tracker does (Exist averages mood); the engine should allow `avg` on an ordinal value and label
  it.
- **Adds:** coded value and ordinal value (with declared min, max and anchors) on `observation`.

### 1.8 Nutrition and food logs

- **Shapes.** A meal is a session; each food item has nutrient amounts; the amounts are interval
  amounts attributed to the meal time. Already in the health catalogue (section 10, "hydration and
  nutrition", about 40 nutrients) and in the planned `food` sample.
- **Fit.** High, already covered by health. Needs `derivedFrom` from item to nutrient observation,
  which the proposal has.
- **Adds:** nothing new.

### 1.9 Pet and plant sensors, aquariums

- **Data.** Soil moisture (%), light (lx), soil temperature, conductivity, every 15 to 60 minutes per
  plant sensor (recalled from Mi Flora class devices; unverified). Aquarium temperature and pH. Pet
  weight and activity from pet wearables (unverified product details).
- **Subject.** The plant, the tank, the pet. A pet's weight is a health observation with a non-human
  subject (FHIR allows `Observation.subject` to be many resource types; see Part 3).
- **Fit.** High with `subject`; niche value.
- **Adds:** `subject`.

### 1.10 Garden and weather stations

- **Data.** Temperature, humidity, pressure, wind speed and direction, rainfall, UV, solar radiation;
  every 1 to 5 minutes (recalled for Ecowitt and Netatmo class stations; unverified). Rainfall is a
  counter with a daily reset or an interval amount.
- **Subject.** The station's place.
- **Fit.** High: section 1.3 plus 1.1. The same adds (subject, circular mean, counter resets).

### 1.11 3D printers and machines

- **Data.** OctoPrint's temperature model is `actual`, `target` (null when unset) and `offset` per
  tool and for the bed; job progress has `completion`, `printTime`, `printTimeLeft` with a
  `printTimeLeftOrigin`, and filament length in mm and volume in cm³. Checked:
  https://docs.octoprint.org/en/master/api/datamodel.html . Prusa Connect not checked.
- **Shapes.** Temperatures are instant, with a **setpoint** beside each actual value; a print job is a
  session with summary amounts.
- **Subject.** The printer; the tool or bed as a qualifier.
- **Fit.** Medium; low value for most people. Setpoint versus actual is a pair (FHIR's `component`
  shape, or two metrics).
- **Adds:** `subject`; a typed `printjob` session kind in its own package.

### 1.12 Computers and infrastructure; network speed tests

- **Prometheus.** "Every time series is uniquely identified by its metric name and optional key-value
  pairs called labels"; a sample is a float64 (or native histogram) and a millisecond timestamp.
  Checked: https://prometheus.io/docs/concepts/data_model/ . InfluxDB's line protocol is measurement,
  indexed tag set, field set (float, integer, uinteger, string, boolean) and timestamp, and warns that
  high-cardinality tags hurt performance and storage. Checked:
  https://docs.influxdata.com/influxdb/v2/reference/syntax/line-protocol/
- **Volume.** Estimate: node exporter on one host exposes several hundred series (unverified count);
  at a 15 s scrape each series is 5,760 samples a day, so 500 series is about 2.9 M samples a day per
  host, 60 times the heavy wearable user. Labels are open-ended.
- **Speed tests.** Ookla's CLI JSON has `ping.latency` and `ping.jitter` in ms,
  `download.bandwidth` and `upload.bandwidth` in bytes per second, `packetLoss` in percent, plus
  `isp`, `interface`, `server` and a result URL. Checked by search:
  https://gist.github.com/itsChris/6f7a5d59b408f0cb774bf2570137a0ef . A few a day.
- **Fit.** Infrastructure metrics: no. They belong in a TSDB; a personal store might keep a daily
  summary (uptime, disk used). Speed tests: yes, trivially: an observation of a connection
  (`subject`), with components (latency, jitter, down, up, loss) like blood pressure's two numbers.
- **Adds:** for speed tests, `subject`; nothing else.

### 1.13 Media consumption

- **Last.fm.** A scrobble is `artist`, `track`, `timestamp` (when it started), optional `album`,
  `duration`, `mbid`, at most 50 per batch. Checked: https://www.last.fm/api/show/track.scrobble . A
  track is scrobbled when it is "longer than 30 seconds" and "has been played for at least half its
  duration, or for 4 minutes (whichever occurs earlier.)" Checked: https://www.last.fm/api/scrobbling
- **Reading.** Pages read per session (interval amount) or the current page (a position, like an
  odometer of a book).
- **Fit.** Low as observations. A scrobble is an event with identity and references (track, artist,
  album); "plays per day" is `count` over a typed kind through the `property` path. Exist.io ships
  `tracks` as a daily integer attribute (checked by search,
  https://developer.exist.io/reference/object_types/), which is the derived view.
- **Adds:** none to the observation model; a typed `scrobble` kind, and the aggregate read's
  `property` path must support `count` per calendar bucket over any kind with a time anchor.

### 1.14 Crypto and stock portfolios

- **Shapes.** A holding is an amount of an instrument (shares, coins) per account at a time (a state
  snapshot); a price is an instant or daily summary observation of the instrument in a currency; a
  portfolio value is derived. Trades are events (typed).
- **Units.** "Shares" are dimensionless counts; UCUM writes such things as annotations (`{share}`,
  recalled), which are unit 1. Prices are currency per share. Crypto needs many decimals (18 for ether
  amounts in wei, recalled), which `decimal` handles exactly.
- **Subject.** The instrument (price), the account (holding).
- **Fit.** Medium; the same currency question as 1.5. High value to a minority.
- **Adds:** `subject`; currency units; exact decimals (present).

### 1.15 Baby tracking

- **Data.** Baby Buddy's models are DiaperChange, Feeding, Sleep, TummyTime, Weight, plus temperature,
  height, head circumference, pumping and notes. Checked by search:
  https://github.com/babybuddy/babybuddy and https://github.com/babybuddy/babybuddy/pull/1146 .
  Feeding fields (start, end, type, method, amount) are recalled, not checked.
- **Shapes.** Feeds and sleeps are sessions; diapers are categorical events (wet, solid, both);
  weight, length and head circumference are instant measurements with derived growth percentiles;
  temperature instant.
- **Subject.** The child, a `person` record that is not the repository's owner. This is the clearest
  case that `subject` is needed **inside health**: one parent's repository holds two children's data.
- **Fit.** High with `subject` and a coded value.
- **Adds:** `subject` (pinned to a person); coded value.

### 1.16 Habit trackers

- **Data.** Loop stores one entry per day per habit; a habit is yes/no or measurable, with schedules
  such as "3 times per week". Checked: https://github.com/iSoron/uhabits . Exist.io returns "every
  single day" with nulls, dates "local to the user". Checked:
  https://developer.exist.io/reference/object_types/
- **Shapes.** A boolean or a count per local calendar date. There is no instant: the date is the
  identity.
- **Fit.** High as a `summary` observation with `period: day` keyed by the local date, if the value
  may be boolean. The proposal keys summaries by `at`/`endsAt` and `periodId`; a local date as the
  identity should be first class (it is how Exist, Loop, Oura and Garmin dailies all key).
- **Adds:** boolean value; local-date identity for daily summaries.

### 1.17 Meters, odometers and shipping

- **Meter and odometer readings** (gas, water, electricity read by hand monthly; car mileage logs):
  counter readings at irregular times. Consumption between two readings is a derived interval amount,
  prorated across months. A meter replacement is a reset. Fit: high with a stored `counter` form.
- **Parcel shipping**: a parcel moves through states (label created, in transit, delivered) with
  events at places. That is a state machine on a typed record (substrate's `state` type), not a
  quantity. Fit: none as an observation.

### 1.18 Part 1 at a glance

| Domain | Shapes | Subject | Location | Rate per day (per subject) | Identity | Fit |
| --- | --- | --- | --- | --- | --- | --- |
| Home sensors and energy | instant, state, counter | room, meter, home | static on subject | 1,000 to 10,000 per channel | entity id | high |
| Vehicle telemetry | instant, counter, state; sessions | vehicle | per sample | up to about 7,000 rows an hour of driving, about 25 channels (estimate) | (car, ts) | medium to high |
| Weather and air quality | instant, summary, interval, forecast | place | the subject | 24 to 288 per variable | (place, variable, hour) | high |
| Location history | position | person or device | is the value | 100s passive, 3,600 an hour recording | (device, ts) | medium (needs geopoint) |
| Finance balances and prices | summary (last), instant | account, instrument | none | 1 per account | (account, date) | medium |
| Spending | event | (account) | sometimes | 1 to 20 | transaction id | not an observation |
| Screen time, app usage | categorical intervals; derived interval | device | none | 100s to 1,000s of events | (bucket, ts) | low raw, medium derived |
| Mood, journaling | ordinal, coded, text | person | optional | 1 to 10 | (metric, ts) | high with coded values |
| Nutrition | session; interval amounts | person | optional | 10s | (meal, nutrient) | high (in health) |
| Pets, plants, aquarium | instant | pet, plant, tank | static | 24 to 1,440 | (subject, metric, ts) | high |
| Weather station | instant, interval, counter | station place | static | 288 to 1,440 per variable | (station, variable, ts) | high |
| 3D printer | instant with setpoint; session | printer | none | 0 to 43,200 while printing | (printer, ts) | medium |
| Infrastructure metrics | instant, counter, histogram | host, process | none | millions per host | name + labels | no |
| Speed tests | instant with components | connection | optional | 1 to 24 | (connection, ts) | high, low value |
| Media consumption | event | person | none | 10s | scrobble | not an observation |
| Portfolio | summary, instant | account, instrument | none | 1 per instrument | (instrument, date) | medium |
| Baby tracking | session, coded event, instant | child (a person) | none | 10s | (child, metric, ts) | high with subject |
| Habits | boolean or count per local day | person | none | 1 per habit | (habit, local date) | high with boolean |
| Meter readings | counter | meter, car | none | 1 a month to 1 a day | (meter, ts) | high with stored counter |
| Parcel shipping | state machine | parcel | per event | few | tracking number | not an observation |

## Part 2. Systems that tried to be the general store

### 2.1 Exist.io: one value per local day, typed

- **Model.** An attribute has `group`, `template`, `name`, `label`, `priority`, `manual`, `active`,
  `value_type` (integer, decimal, string, duration in minutes, time of day from midnight, percentage,
  time of day from midday, boolean, scale 1 to 9) and `service`; values are date/value pairs; "All
  dates are local to the user" and reads return "every single day" with nulls. Checked:
  https://developer.exist.io/reference/object_types/
- **Templates** span domains: `productive_min` (productivity, minutes), `money_spent` (finance, float
  in "user's local currency unit"), `weather_temp_max` (weather, Celsius), `tracks` (media, integer),
  `checkins` (location, integer). Checked by search, same reference.
- **Source conflict.** "Only one service may have ownership of any user attribute at any given time";
  releasing passes ownership to another capable service or deactivates the attribute. Checked:
  https://developer.exist.io/reference/attribute_ownership/
- **Correlations.** Objects carry `attribute`, `attribute2`, `value` (-1 to 1), `p`, `stars`, and a
  plain-English `second_person` sentence (checked, object types page). Exist also checks the next day
  (drinks today against sleep tonight). Checked by search:
  https://kb.exist.io/article/55-will-exist-find-correlations-across-multiple-days . The statistic
  used (Pearson or other) and the minimum sample are unverified.
- **Lesson.** Exist is the most successful cross-domain store in this list, and it is cross-domain
  **because** it is narrow on grain: every attribute is reduced to one typed value per local day, so
  weather, money, music and mood join on the date with no units, no subjects and no time zones in the
  join. It refuses intraday data entirely. Source policy is per attribute, not per sample. For
  substrate: the daily summary layer (`form: summary`, `period: day`, local-date identity) is the
  cross-domain join; the dense layers can stay per domain.

### 2.2 ActivityWatch: buckets of events with a data dict

- **Model.** Section 1.6: bucket per watcher and host with a declared `type`; event = timestamp,
  duration, data dict; heartbeat merging; UTC only, offset discarded. Checked:
  https://docs.activitywatch.net/en/latest/buckets-and-events.html
- **Lesson.** Generic in payload, narrow in domain (computer activity), and typed by bucket type. The
  discarded offset is the same mistake the health research found elsewhere: a local day cannot be
  rebuilt later. Heartbeat merging is a write-side compression that a packed trace does differently.

### 2.3 Home Assistant as a general store

- **Model.** Three layers: `states` (every change of every entity, string state plus attributes,
  purged after 10 days by default); `statistics_short_term` (5-minute aggregates); `statistics`
  (hourly). A measurement series keeps time-weighted `mean`, `min`, `max`; a metered series keeps
  `state` at the end of the hour, `sum` ("hourly grand total since statistics for the sensor was first
  compiled") and `last_reset_ts`. `statistics_meta` has `statistic_id`, `source`, units, `has_mean`,
  `has_sum`; statistics are "either derived from entities ... or imported from an external source".
  Checked: https://data.home-assistant.io/docs/statistics/
- **2025 change.** The statistics API now requires a `unit_class` naming the unit converter and
  replaces the boolean `has_mean` with `mean_type` of `NONE`, `ARITHMETIC` or `CIRCULAR`. Checked:
  https://developers.home-assistant.io/blog/2025/10/16/recorder-statistics-api-changes/
- **Lesson.** Home Assistant is the closest working analogue to the proposal: raw states are short
  lived, derived statistics are kept forever, and the value's semantics are declared per series
  (`state_class`, `device_class`, unit, `last_reset`). It took years to learn that the semantics must
  be explicit: the 2025 change replaced implicit unit handling and a boolean with named classes, which
  is the same correction the health reviews made ("the unit belongs on each stored value"; the form on
  the value). Categorical states never enter long-term statistics, so "how long was the door open last
  year" is not answerable after 10 days. Money is a sensor with an ISO 4217 unit and no conversion.

### 2.4 Grafana plus InfluxDB (or Prometheus) as a personal store

- **Model.** Measurement, indexed tags, typed fields, timestamp (InfluxDB); metric name plus labels,
  float64 plus ms timestamp (Prometheus). Checked, section 1.12.
- **Lesson.** Popular in self-hosting and QS circles (community prevalence is recalled, not measured).
  It stores numbers fast and carries no semantics: no unit, no form, no sessions, no subject other
  than a label. Each dashboard re-encodes the meaning, and tag cardinality is the failure mode both
  vendors document. Substrate's samples index is a TSDB-shaped projection; the semantics are what
  substrate adds on top, and they are the reason to build it rather than ship InfluxDB.

### 2.5 Apple Health: wide on metrics, narrow on subject

- **Model.** Typed sample classes (quantity, category, correlation, workout) keyed by identifiers from
  a closed list, with `HKUnit` (recalled; detailed in report 04). The list has grown beyond physiology
  into exposure: `environmentalAudioExposure`, `headphoneAudioExposure`,
  `environmentalSoundReduction`, `timeInDaylight`, `uvExposure`, `waterTemperature`,
  `underwaterDepth`, `numberOfAlcoholicBeverages`, `bloodAlcoholContent`. Checked:
  https://developer.apple.com/tutorials/data/documentation/healthkit/hkquantitytypeidentifier.json .
  Mood arrived as `HKStateOfMind` in iOS 18 (checked, section 1.7).
- **Lesson.** Apple widened the metric list and never the subject: environmental sound is "audio
  exposure", the dose at the person, not the loudness of a room. Every sample is about the user. That
  keeps every aggregate meaningful without a subject column, and it is why HealthKit cannot hold a
  child's weight or a room's CO2. Apps cannot add identifiers, so they stuff extra meaning into
  metadata (recalled, unverified).

### 2.6 Google Fit, then Health Connect

- **Model.** Data types with field lists (instantaneous and aggregate), data sources (device, app,
  stream), and custom data types "only accessible to the defining app". Checked:
  https://developers.google.com/fit/datatypes . "Custom data types are deprecated; use standard data
  types instead." Checked by search: https://developers.google.com/fit/rest/v1/reference/users/dataSources
  . "The Google Fit APIs, including the Google Fit REST API, will be deprecated in 2026. As of May 1,
  2024, developers cannot sign up". Checked, datatypes page.
- **Lesson.** The one platform that offered a generic, user-extensible type system took it back,
  because a type only its writer understands is not shared data. Health Connect, the successor, is a
  fixed set of typed record classes (report 04). Substrate's answer, a `metric` record anyone can read
  with codes into public systems, avoids Google's problem only if metrics are catalogued and coded;
  an uncoded user metric is Google's custom type again.

### 2.7 Fitbit

- Typed per-domain endpoints (activities, body, foods, sleep, heart) with fixed fields (recalled). The
  Web API shuts down 2026-10-30 in favour of the Google Health API (from report 04, checked there).
  Narrow and typed; not a general store.

### 2.8 Open Humans

- **Model.** Projects are data sources; members' data are files with metadata, downloaded per source
  (`ohproj-download` "sorting data into subdirectories according to source"). Checked by search:
  https://pypi.org/project/open-humans-api . The foundation is a 501(c)(3) and the main repository had
  commits in February 2025 (checked by search, https://github.com/openhumans). The membership and data
  set counts in its 2019 paper were not readable today (https://pubmed.ncbi.nlm.nih.gov/31241153/).
- **Lesson.** File-level generality makes ingestion trivial and defers every semantic decision to the
  analyst. It works for a research platform where analysts are the users; it gives an application
  nothing to aggregate.

### 2.9 HAT (Hub of All Things), now Dataswyft

- **Model.** Applications write only in their own `namespace`; an `endpoint` names a collection inside
  it (`/api/v2.6/data/rumpel/locations`); a record is `endpoint`, `recordId` (UUID) and `data`, "plain
  Data APIs consume any JSON"; combinators, bundles and data debits govern what third parties
  receive. Checked: https://docs.dataswyft.com/build/dataswyft-one-apis/data-api . Current commercial
  status unverified.
- **Lesson.** Any JSON per namespace is storage, not a model: the data debit (a consented bundle for
  a buyer) became the product, and meaning lived in each writer's JSON. Exactly the trap substrate's
  declared kinds avoid; the lesson for this question is to keep the observation value types declared
  and closed.

### 2.10 Solid pods

- **Model.** RDF documents in Linked Data Platform containers, WebID identity, per-resource access
  control. Leigh Dodds (2024): a pod "is essentially a document store that supports capturing
  additional metadata about the stored resources. But it doesn't provide me with a way to query or
  search over the data"; "Solid doesn't natively understand that data"; to interoperate "I'll need to
  work on that with those other developers". Checked: https://blog.ldodds.com/2024/03/12/baffled-by-solid/
- **Lesson.** Universal semantics (RDF) did not produce shared schemas; with no query and no
  aggregation, a pod cannot answer "average CO2 in the bedroom last month". Generality of format is not
  generality of use.

### 2.11 Personal knowledge graphs

- The 2024 survey by Skjæveland, Balog et al. defines a PKG as "a resource of structured information
  about entities personally related to its user, their attributes and the relations between them";
  finds "no clear consensus on the meaning of a PKG"; expects temporal facts to exceed OWL 2 DL; and
  concludes "the biggest obstacles ... are not of a technical nature". Checked:
  https://arxiv.org/abs/2304.09572 (text extracted to `raw-domains/pkg-survey.txt`). The survey
  treats quantified self as adjacent; numeric time series are outside what PKG work models.
- **Lesson.** Substrate is already a PKG in this sense (records and references). Graph models are for
  entities and relations; dense quantities need the separate projection the health proposal designs.

### 2.12 Fluxtream and BodyTrack (CMU CREATE Lab)

- **Model.** The datastore handles "high volumes of numeric and text time series, with precomputed
  level-of-detail summarizations"; data is per device and channel, and it "will only hold one value
  for a device.channel at a particular time". Checked: https://github.com/BodyTrack/datastore .
  Self-reports came from Mymee, whose concepts were "topics" and "observations, which represent a
  particular sensation or action relating to a given topic at a particular point in time", each with
  an optional value, comment and photo. Checked: Anne Wright, "Reflections on the BodyTrack Project"
  (2016), https://www.ri.cmu.edu/pub_files/2016/6/bodytrack-cmu-tech-report-160615.pdf (text in
  `raw-domains/bodytrack.txt`).
- **What they learned.** The report's lessons are about practice, not schema: the value came from
  coaching people to look at a headache channel beside sleep and activity channels on one timeline.
  The UI moved from one shared graph, where "each time a new channel was added, all of the existing
  ones would typically need to be adjusted and rescaled", to one track per channel. Connectors pulled
  "the full set of historical data" on connect. Funding was mostly one foundation (Heinz Endowments).
  Whether fluxtream.org still runs is unverified.
- **Lesson.** device.channel with level-of-detail tiles is the samples index plus rollups, built in
  2010. It held every numeric source. What it lacked was what the health reviews added: form, unit on
  the value, sessions.

### 2.13 Zenobase, Nomie, Loop, Gyroscope

- **Zenobase**: buckets of events with typed fields (timestamp, tag, rating, count, distance, weight)
  through a REST API. Checked by search: https://blog.zenobase.com/post/99071925137 and
  https://wiki.openhumans.org/wiki/Tracking_with_Zenobase . Current status unverified.
- **Nomie**: "all tracker, people and context data being stored as a string in the log.note field",
  parsed in real time, so "Today I walked #miles(5) #mood(8)" becomes `{miles: 5, mood: 8}`. Checked by
  search: https://www.dailynomie.com/%F0%9F%93%96-docs/%F0%9F%93%96-docs-overview/ . Shut down
  2023-02-01: "Nomie has never been financially viable ... Nomie 6 only brought in a total of $500 this
  year." Checked: https://www.nomie.app/ . Lesson: text as the truth is maximally generic and
  user-friendly, and every reader must re-parse it; the death was economic, not technical.
- **Loop Habit Tracker**: one entry per day per habit, yes/no or measurable, CSV and SQLite export.
  Checked: https://github.com/iSoron/uhabits . Narrow and still maintained.
- **Gyroscope**: began in 2014 as April Zero, a general life dashboard; its ten-year post says what
  matters is "your mind, your food and your fitness" and centres on coaching. Checked:
  https://gyrosco.pe/announcements/gyroscope-turns-10/ . Removed from Google Play in July 2023, alive
  on iOS (checked by search, https://www.appbrain.com/app/gyroscope/com.gyroscope.gyroscope). Lesson:
  the most visible "track everything" product narrowed to health.

### 2.14 Location: Dawarich and OwnTracks

- Section 1.4. Both are single-domain, typed, and alive. Dawarich runs on PostGIS and spent its
  performance work on a wide `raw_data` column and on rendering millions of points. Lesson: positions
  need spatial types and a raw-versus-derived split.

### 2.15 HPI and Dogsheep: per-source, deliberately no common schema

- **HPI** (karlicoss): one Python module per source returning typed objects; "While databases are
  efficient and easy to read from, often they aren't flexible enough to fit your data." Checked:
  https://github.com/karlicoss/HPI
- **Dogsheep** (Simon Willison): one `*-to-sqlite` tool per source, explored in Datasette, "no
  enforced common schema" (summary of the page). Checked: https://dogsheep.github.io/
- **Lesson.** The power users' answer is mirrors in the source's own shape plus code that reads them,
  which is substrate's mirror rule. Neither attempts cross-source aggregation in the store.

### 2.16 Wolfram's personal analytics

- Stephen Wolfram's 2012 post analysed 22 years of email, keystrokes, meetings, phone calls and (since
  2010) steps. Checked by search:
  https://writings.stephenwolfram.com/2012/03/the-personal-analytics-of-my-life/ . Every plot is a
  count of timestamped events by time of day and by date (recalled from the post). Lesson: the most
  cited personal analytics work needed events and counts by local time, not quantities with units. The
  `property` path over typed event kinds (count per calendar bucket, time-of-day histogram) serves it.

### 2.17 Mozilla Rally, Memex, Memento, life logs

- **Mozilla Rally** (2021) was a data donation platform for research studies, not a store; its
  repository was archived 2023-04-21. Checked by search: https://github.com/mozilla-rally and
  https://blog.mozilla.org/data/2021/05/05/announcing-mozilla-rally/
- **Memex** (WorldBrain) is a browser history and annotation tool; **Memento Database** is a
  user-defined-fields database app. Neither models quantities (recalled; not checked today).
- **Timeline-style life logs** (Google Maps Timeline, moved to on-device storage in 2024 with web
  Timeline retired; recalled, unverified): location and visits, a single domain.

### 2.18 Part 2 at a glance

| System | Generic schema chosen | Outcome | Why |
| --- | --- | --- | --- |
| Exist.io | typed value per attribute per local day | alive, cross-domain | narrow grain; one owner per attribute |
| ActivityWatch | bucket of events with data dict | alive, single domain | typed by bucket; offset discarded |
| Home Assistant | states plus 5-min and hourly statistics, declared state class | alive, the closest analogue | semantics declared per series; had to make unit class and mean type explicit in 2025 |
| InfluxDB / Prometheus | measurement, tags, fields | alive as infrastructure | no semantics; cardinality |
| Apple Health | closed list of typed identifiers, user as only subject | alive | wide metrics, fixed subject |
| Google Fit | data types plus custom types | custom types deprecated, API retired 2026 | only the writer could read a custom type |
| Open Humans | files per source | alive, research niche | semantics deferred to analysts |
| HAT / Dataswyft | any JSON per namespace | status unverified | storage without a model |
| Solid | RDF documents | low adoption (per critics) | no query, no shared schemas |
| Fluxtream / BodyTrack | device.channel series with LOD tiles | dormant (unverified) | value was coaching; funding |
| Zenobase | buckets of typed events | unverified | |
| Nomie | text notes parsed for trackers | shut 2023 | not financially viable |
| Loop | one entry per day per habit | alive | narrow |
| Gyroscope | general life dashboard | narrowed to health coaching | value in health |
| Dawarich, OwnTracks | positions, visits, places | alive | single domain, spatial types |
| HPI, Dogsheep | per-source typed, no common schema | alive (hobby scale) | mirror shape, code reads it |

## Part 3. The design question

### 3.1 The five-part shape in the standards

| Standard | What (property) | Of what (subject / feature of interest) | By what (procedure, sensor) | When | Result, unit, quality | Source |
| --- | --- | --- | --- | --- | --- | --- |
| SOSA/SSN (W3C) | `observedProperty` (ObservableProperty, "an observable quality ... of a FeatureOfInterest") | `hasFeatureOfInterest` ("the thing whose property is being estimated") | `madeBySensor` ("device, agent (including humans), or software"), `usedProcedure` | `phenomenonTime` ("the time that the Result ... applies"), `resultTime` ("when the Observation activity was completed") | `hasResult`, `hasSimpleResult` | checked https://www.w3.org/TR/vocab-ssn/ |
| OGC SensorThings 1.1 | Datastream's ObservedProperty | FeatureOfInterest per Observation; Thing with Location and HistoricalLocation separately | Datastream's Sensor | `phenomenonTime`, `resultTime`, `validTime` | `result`, `resultQuality`, `parameters`; **unit and observationType on the Datastream**; MultiDatastream for array results | checked https://docs.ogc.org/is/18-088/18-088.html |
| ISO 19156:2023 OMS | observedProperty | featureOfInterest, plus sample features "according to their spatial, material ... or statistical nature" | procedure; observer, host and deployment (recalled) | phenomenon, result, valid times | result, resultQuality | checked https://www.ogc.org/standards/om/ ; v3 class names recalled |
| FHIR R5 Observation | `code` | `subject` (Patient, Group, Device, **Location**, Organization, Procedure, Practitioner, Medication, Substance, BiologicallyDerivedProduct, NutritionProduct) and `focus` (any, "when it is not the patient of record") | `device`, `method`, `performer` | `effective[x]` ("physiologically relevant time"), `issued` | `value[x]`, `dataAbsentReason`, `interpretation`, `referenceRange`, `component`; no location element | checked https://hl7.org/fhir/R5/observation.html |
| OMOP CDM 5.4 MEASUREMENT | `measurement_concept_id` | `person_id` only | `provider_id`, visit; device via other tables | `measurement_datetime` | `value_as_number`, `value_as_concept_id`, `operator_concept_id`, `unit_concept_id`, `range_low/high` | checked https://ohdsi.github.io/CommonDataModel/cdm54.html |
| Open mHealth / IEEE 1752.1 | one schema per measure | the user in the header (recalled) | `acquisition_provenance` (source, modality) | `effective_time_frame` | value with unit; `descriptive_statistic` | report 04 |
| OpenTelemetry metrics | metric name | Resource attributes (the producer, conflated with the thing) | instrumentation scope | `time_unix_nano`, `start_time_unix_nano` | point kind Gauge, Sum (delta or cumulative, monotonic flag), Histogram, ExponentialHistogram, Summary; unit | checked https://opentelemetry.io/docs/specs/otel/metrics/data-model/ |
| HL7 v2 OBX | OBX-3 observation identifier | the PID segment (patient) | OBX-17 method, OBX-18 equipment | OBX-14 date/time of the observation | OBX-5 value, OBX-6 units, OBX-7 reference range, OBX-8 abnormal flags, OBX-11 result status | recalled, unverified |

Two things the standards agree on that matter here. Every one of them has a subject slot, and the
general ones (SOSA, SensorThings, OMS) make it a first-class feature that can be a place, a sample or
a device. And every one separates **when the property had the value** from **when the result was
known** (`resultTime`, `issued`, OBX-14 versus report time).

### 3.2 The health proposal against the five parts

| Part | Health proposal | Status |
| --- | --- | --- |
| Property | `metric` record with `codes`, `dimension`, default form, compatibility class | present, richer than most (compatibility class, form) |
| Subject | implicit: the repository's user | **missing** |
| Procedure | `source` (device, app, account, lab), `recordingMethod`, `algorithmVersion` on vendor metrics, `conversion` stamp | present |
| Phenomenon time | `at`, `endsAt`, `utcOffset`, period and `periodId` for summaries | present, ahead of the standards on local time |
| Result time | `revision`; `issuedAt` only on `labreport` | **missing on observation and trace** |
| Result | `quantity` with comparator; `components`; coded only on `labresult` | **coded, ordinal and boolean values missing on observation** |
| Quality | `bounded`, `normalizationFailure` | **accuracy and quality flags missing** (GPS `acc`, estimated, interpolated) |
| Value semantics | `form` (instant, interval, counter, summary) and `statistic` | present and ahead: only OTel (point kinds) and Home Assistant (`state_class`, `mean_type`) carry this |
| Location | none (latitude and longitude as metrics) | **missing** |

### 3.3 Is adding subject and location enough?

No. Adding `subject` and `location` serves home sensors (numeric), weather and air quality, pets,
plants, stations, speed tests, and baby tracking's measurements. The other domains in Part 1 that fit
need these too, each tied to a domain that needs it:

1. **`subject`**: a reference, optional, absent meaning the repository's owner, so every health record
   is unchanged. A reference in substrate is typed, so it should pin to a new core **trait** (call it
   `observable`) that `room`, `vehicle`, `plant`, `place`, `account`, `person` and the like bind. That
   is how `source.account` already pins to `accountconfig`. The samples index key gains `subject`; the
   source priority resolves per (metric, subject); rollups key on it.
2. **Location**, split the way SensorThings splits it: a fixed location belongs on the **subject**
   (the room's home, the station's place), and a moving one is **per sample** (car, phone). The second
   needs a `geopoint` value type and a packed `track`; per-sample latitude and longitude as two
   quantity metrics cannot answer spatial questions.
3. **`state` form** for categorical step functions: door, presence, AFK, charging state, gear. Legal
   reducers: `durationIn` per value, `transitions`, `last`, `count`. Home Assistant shows the cost of
   leaving it out: its categorical history is purged after 10 days.
4. **Stored `counter` with resets**: meters, odometers, rain gauges. Keep the reading as the fact,
   derive interval amounts. Rules for a reset (`last_reset`; OTel's start time) and a replacement.
5. **Coded, ordinal and boolean values** on `observation`: mood, habits, diapers, symptoms. The
   proposal's "exactly one value shape" rule for `labresult` generalises.
6. **`resultTime`** (or `issuedAt`) on observation and trace, plus a `forecast` marker or a separate
   kind for forecasts: weather forecasts and late-arriving vendor recomputations.
7. **Circular mean** for angles (wind direction, course over ground, heading): a reducer property of
   the dimension `angle`, as Home Assistant's `mean_type: CIRCULAR`.
8. **Currency** as dimensions that do not convert (Home Assistant's `monetary`), or `money` admitted
   as an observation value; only if finance balances and prices are in scope.
9. **Bounded grouping dimensions** on derived observations (app name for screen time, tyre position,
   printer tool), with a cardinality cap; or a rule that a dimension becomes a subject or a metric.
   This is the riskiest addition (Prometheus and InfluxDB both document cardinality as the failure)
   and could wait.
10. **Multi-channel traces** sharing one time axis (vehicle rows, weather station rows, FIT records),
    as SensorThings `MultiDatastream`: optional, an efficiency, not a semantic need.
11. **Quality**: `accuracy` (a quantity) and a small flag set (estimated, interpolated, manual).
12. **A volume ceiling stated as scope**: infrastructure metrics (millions of samples a day per host,
    open label sets) are out of scope, and saying so in the plan prevents someone building a node
    exporter provider.

### 3.4 Generic "everything is an observation" versus typed domain packages

**What generic costs, with systems that regretted it.**

- Google Fit's custom data types were deprecated because "custom data types are only readable by the
  application that created them" (checked, section 2.6). A user-defined `metric` without public codes
  is the same thing.
- HAT accepted "any JSON" and Solid any RDF; neither gives a reader a query or an aggregate (2.9,
  2.10). Open Humans' files defer everything to the analyst (2.8). Nomie's text-as-truth died for
  other reasons but made every reader a parser (2.13).
- Home Assistant had to replace implicit unit handling and a boolean mean flag with explicit
  `unit_class` and `mean_type` in 2025 (2.3): generic series with implicit semantics produced wrong
  statistics for angles and ambiguous unit conversion.
- Label and tag stores (Prometheus, InfluxDB) fail on cardinality when the generic dimension is open.
- Clinical EAV tables (the OpenMRS `obs` table; OMOP's split of MEASUREMENT and OBSERVATION, where
  the boundary confuses mappers) are the usual cited regret in health informatics; recalled, not
  checked today.
- A specific risk for substrate: a generic `observation` with `subject` pointing at anything turns
  "average temperature" into a question with no default answer (body, room, outside, water), so every
  read must name metric **and** subject. Apple avoided this by never allowing a second subject.

**What typed costs, with systems that regretted it.**

- Closed type lists stop new metrics: Google Fit added custom types because its list was closed, then
  withdrew them; HealthKit apps put extra meaning into metadata because they cannot add identifiers
  (recalled, unverified); Exist.io began with templates and later added custom attributes (the
  `exclude_custom` filter shows both exist; history recalled).
- One kind per metric does not scale: the health proposal itself rejects 310 kinds for 70 wearable
  metrics and 240 markers (section 6, "Generic plus typed").
- Typed silos cannot be joined without a common time and value model; Dogsheep and HPI accept that
  and give up cross-source aggregation (2.15).

**Where the line falls.** The evidence points at the same split the health proposal uses inside
health, applied across domains:

- **Generic, in core**: the value types (`quantity`, `geopoint`, coded and ordinal values), the forms
  (`instant`, `interval`, `counter`, `summary`, `state`), the `samples` trait and `trace`, the derived
  index and rollups, the `aggregate` read, and the `observable` trait that subjects bind. These are
  engine semantics; getting them once is the point.
- **Typed, per domain package**: the metric catalogue (health ships about 310 metrics; a `home`
  package ships temperature, humidity, CO2, energy and the rest with Home Assistant device classes as
  codes), the subject kinds (`room`, `vehicle`, `place`, `plant`, `account`), and the session kinds
  (`sleep`, `workout`, `trip`, `charge`, `printjob`, `feed`).
- **Typed and not observations**: transactions, scrobbles, reading sessions, parcels, app usage
  spans. Their numbers are reached by the `property` aggregate path.

One open structural question for the owner: whether `observation`, `trace`, `metric` and `source`
live in the `health` sample (and other domains import health to get them) or move to a shared
`observations` package that `health`, `home` and `vehicle` each depend on. The second keeps a home
user from importing lab markers; the packages plan would decide whether a sample may depend on
another sample (not checked today).

## Ranking: domains by fit and value

Fit: how much of the model works unchanged plus the named additions. Value: how often a person would
want it and how much it adds to questions across domains. Volume is per subject per day.

| Rank | Domain | Fit | Value | Volume | Additions it needs |
| --- | --- | --- | --- | --- | --- |
| 1 | Home sensors and energy | high | high | 10^3 to 10^4 per channel | subject (observable trait), `state` form, stored counter with resets, coded state value |
| 2 | Weather and air quality at places | high | high (context for health) | 10^2 | subject = place, location on subject, `resultTime` and forecast marker, circular mean |
| 3 | Baby tracking (and any second person) | high | high for those users | 10^1 | subject = person, coded value |
| 4 | Mood, journaling, habits | high | high | 10^0 to 10^1 | coded, ordinal and boolean values, local-date identity for daily summaries |
| 5 | Meter and odometer readings | high | medium | 10^0 | stored counter with resets |
| 6 | Vehicle telemetry | medium to high | medium to high | about 10^4 to 10^5 rows while driving, 25 channels | subject, per-sample geopoint, optional multi-channel trace, typed `trip` and `charge` |
| 7 | Location history | medium | high | 10^2 to 10^4 | `geopoint`, `track`, spatial filter, accuracy, the deletion decision |
| 8 | Garden and weather stations | high | medium | 10^2 to 10^3 | subject, circular mean, counter resets |
| 9 | Pets, plants, aquariums | high | low to medium | 10^1 to 10^3 | subject |
| 10 | Nutrition | high | high | 10^1 | none (already in health) |
| 11 | Finance balances and portfolio prices | medium | medium | 10^0 per account | currency dimension or money as a value, subject = account or instrument |
| 12 | Speed tests | high | low | 10^0 to 10^1 | subject; components exist |
| 13 | Screen time (derived minutes) | low to medium | medium | 10^2 to 10^3 events | typed span kind, `state` form, bounded grouping dimension |
| 14 | 3D printers and machines | medium | low | 0 to 10^4 while printing | subject, setpoint pairs, typed `printjob` |
| not an observation | Spending, media consumption, parcel shipping | n/a | high (spending) | 10^1 | typed kinds; `property` aggregate path with `count` and `sum` per calendar bucket |
| out of scope | Computer and infrastructure metrics | no | low in a personal store | 10^6 per host | none; keep in a TSDB, import daily summaries at most |

## Model additions, and the domains each serves

| Addition | Serves | Size (judgement) |
| --- | --- | --- |
| `subject` reference pinned to a core `observable` trait, absent = owner; in the samples index key and source policy | home, weather, vehicle, pets and plants, stations, baby, finance, speed tests, printers | medium (index key and policy change) |
| Location on the subject (static) | home, weather, stations, plants | small (a property on subject kinds) |
| `geopoint` value type and packed `track`, spatial filter | location history, vehicle, workouts (replaces latitude and longitude metrics) | large |
| `state` form with `durationIn`, `transitions`, `last` | home (doors, presence), vehicle (charging), screen time (AFK) | medium |
| Stored `counter` form with reset and replacement rules | energy, gas, water, odometer, rain gauge | medium |
| Coded, ordinal and boolean values on `observation` | mood, habits, baby (diapers), symptoms, home states | medium |
| Local-date identity for daily summaries | habits, Exist-style daily values, vendor dailies | small |
| `resultTime` / `issuedAt` and a forecast marker | weather forecasts, late vendor recomputations, labs | small |
| Circular mean for an `angle` dimension | wind direction, heading, course over ground | small |
| Currency dimensions (non-convertible) or money as a value | finance balances, portfolio prices | small to medium |
| `accuracy` and quality flags | location, any estimated or interpolated value | small |
| Bounded grouping dimension with a cardinality cap | screen time, tyre position, printer tool | medium, risky; defer |
| Multi-channel trace on one time axis | vehicle, weather station, FIT records | medium; optional efficiency |
| Stated volume ceiling and out-of-scope list | infrastructure metrics | none (a sentence in the plan) |
