# Synthetic provider data

`internal/synth` generates data for one invented person in the exact shapes
the shipped provider functions read, and `cmd/synth` writes it to disk and
loads it into a running substrate. The demo substrate, the console
screenshots and the contract tests in `internal/providertest` all start from
it, so a change to a provider's shape that the generator does not follow
fails a test, not a demo.

Nothing here came from anybody's body. The recorded fixtures under
`internal/providere2e/fixtures/` and `internal/providertest/testdata/` are
the shapes; this is the volume.

## The person

`synth.Persona` is a deterministic model of a body over a date range: every
value derives from the seed and a key, so two runs agree to the byte and four
emitters run independently and still describe one life. The default
(`synth.DefaultOptions`) is Ada, Athens, the 90 days ending 2026-10-11, with a
cold from 2026-08-26 to 2026-08-29 (resting heart rate up, HRV down, sleep
longer and broken, no training) and a trip to London from 2026-09-20 to
2026-09-24 (the zone moves with her). She trains six days most weeks (an easy
run, intervals, strength, a long run that grows with the block, yoga, a ride
every other week), weighs in four mornings a week, takes her blood pressure
on Sundays and has two lab panels.

`Persona.Day(d)` is the ground truth for one local day: the night that ended
that morning with its stages, the workouts with their heart-rate curves, the
heart rate at any instant, steps per bucket, energy, stress and Body Battery
in Garmin's terms, recovery and strain in WHOOP's, the weigh-in and the
reading if there was one. An emitter samples it at the resolution its
provider sends and adds its own device noise with `Persona.Noisy`, keyed by
provider, so a watch reads two beats off another watch and two scales
disagree by a few hundred grams.

## The emitters

| Emitter | Writes | How it reaches a substrate |
| --- | --- | --- |
| `whoop` | `recordings/`: the WHOOP v2 profile, body measurement, and the cycle, recovery, sleep and workout collections, 25 records a page, later pages named for their `nextToken`; `e2e.json` for the runner | `synth load` runs the provider e2e runner, which drives the real `whoopsync` over the recordings |
| `googlefit` | `recordings/`: the Fit REST v1 data source list (17 derived and 15 raw streams), one recording per derived stream per UTC day with points (`..._datasets_<startNs>-<endNs>__limit-1000.json`) and one empty recording per stream for every other day, the windowed session list, the two-page session feed and the userinfo address; `e2e.json` for the runner | `synth load` runs the provider e2e runner, which drives the real `googlefitsync` over the recordings, then imports `healthgooglefit` and replays it |
| `applehealth` | `deliveries/`: one Health Auto Export request per local day (Time Grouping Minutes, Export Version 2) to `applehealth-webhook`, with the app's headers | `synth load` installs the provider, writes the `iphone` account, imports `healthapple`, arms the trigger and posts them one at a time |
| `manual` | `labs.yaml`: two `labreport` records, 22 `labresult` records, the lab and the hand-entry `source` records, and one weigh-in typed in on a hotel scale | `synth load` writes them through `PUT /api/v1/...`; `substratectl apply -f` does the same |

`googlefit` samples the persona the way a Pixel Watch 2 syncs to Fit. Heart
rate is one point every 5 minutes with the watch's noise, and one a minute
during workouts. Steps are 15-minute deltas about 5 percent high. Energy
expended is hourly. Sleep stages are written as `com.google.sleep.segment`
points, and oxygen saturation is one nightly average at wake. A Withings
Body+ writes weigh-ins and body fat four mornings a week, and a Withings BPM
Connect blood pressure and a pulse on Sundays. Height is entered once, and
activity segments, active minutes and heart minutes cover every bout.
Sessions are one sleep per night and nap and the Fit app's rides, strength
and yoga (codes 1, 80 and 100), plus Strava's runs under Strava ids.
Distance is written but never served, because the bundle asks for no
location scope. The 90-day default is 1,212 files (19.7 MB). The
`healthgooglefit` adapter turns it into about 38,800 observed points under
the sources `googlefit/<account>/google-pixel-watch-2.8d1f3c7a`,
`withings-body.4a91be20`, `withings-bpm-connect.7c03d5e1`, `manual`,
`com-strava` and `com-google-android-apps-fitness`.

`applehealth` writes what the Health Auto Export app POSTs at the end of
each local day. Heart rate comes from "Ada's Apple Watch" about every 5
minutes as one-minute `Min`/`Avg`/`Max` rows, with a spread during
workouts. Steps come per 5-minute bucket, from "Ada's iPhone" by day and the
watch before 07:00, after 22:00 and during runs. Distance, flights, and
active and basal energy come per hour, in km and kJ. Each night carries
resting heart rate, three SDNN readings, respiratory rate every 15 minutes
and SpO2 every 10, without a `source` key, as the app sends them. Weight and
body fat come on weigh-in mornings and blood pressure on Sundays, from
"Withings". The night is one aggregated `sleep_analysis` row, and each
workout carries the app's activity name and `heartRateData` every 60 s.
Dates are `yyyy-MM-dd HH:mm:ss Z` in the day's zone, `+0300` at home and
`+0100` in London. The 90-day default is 90 deliveries (5.8 MB, about
56,000 points); the `healthapple` adapter turns them into about 10,500
hourly traces, about 930 observations, 90 `health/sleep` and one
`health/workout` per workout, under four sources:
`applehealth/iphone/apple-watch`, `.../iphone`, `.../withings` and
`.../health` for rows without a source or with merged labels.

`whoop` writes WHOOP's scored summaries and never a sample series, because
WHOOP's API has none. One physiological cycle per local day, from that
morning's wake to the next, the last one still open; one recovery per
cycle, scoring the night that opened it; one sleep per night and per nap
with the stage totals in milliseconds; one workout per persona workout
under WHOOP's sport name (`sport_id` is not written: WHOOP stopped sending
it after 2025-09-01 and the adapter ignores it). Cycle heart rate samples
the persona's curve every 5 minutes, and the workout zone durations come
from each workout's 30-second series. The 90-day default is 17 recordings
(226 KB); the `healthwhoop` adapter turns them into about 1,200
observations, 95 `health/sleep` and 62 `health/workout` records under one
source, `whoop/<account id>`, and no traces.

Recordings for a mock-driven provider are named by `synth.RecordingName`,
the Go form of the rule in `internal/providere2e/runner/mockserver.py`
(`recording_test.go` pins it against names the Python produced), so the
provider e2e runner serves them unchanged. A webhook provider's requests are
saved as the `{"request": {...}}` envelopes the provider tests hold under
`testdata/`, one file per delivery in posting order, with the trigger they go
to.

## The command

```bash
mise run synth:generate                              # every emitter -> .dev/synth/<provider>/
mise run synth:generate -- -providers manual -out /tmp/synth
mise run synth:generate -- -from 2026-09-01 -to 2026-09-30 -seed other
SUBSTRATE_TOKEN=… mise run synth:load -- -server http://127.0.0.1:8080 -authority ada.example.com
```

`generate` is pure: the same flags write the same bytes. `load` takes a
provider's directory in three steps. A `recordings/` directory goes through
the provider e2e runner (`internal/providere2e/runner/e2e.py`, so `python3`
and `bin/substratectl` are needed): the bundle is installed rewired at a
mock serving the recordings, the account connects through the mock's OAuth
stub, the sync drains, and the provider's schedule is switched off because
the mock dies with the run. Then `setup.json`, which the emitter declared
(`synth.Setup`): a push provider is installed from the catalog, the
`observations` and `health` samples and the adapter sample are imported,
and the records the deliveries name (the account) are written; an adapter
imported after the runner landed rows is replayed from the start of the
changelog. Last, a `deliveries/` directory is posted to
`/webhooks/<authority>/<trigger>` one request at a time, the trigger armed
with `-key` first, waiting for the trigger to settle before the next (the
door holds eight in flight); a parked delivery stops the load and names the
trigger to read. The bearer comes from `SUBSTRATE_TOKEN`, never from argv.

## What holds it to spec

- `internal/synth`'s own tests: determinism, the life's invariants (a sleep
  is four to twelve hours, a heart rate stays between 40 and the maximum,
  the ill days walk less and train not at all), the recording names.
- `internal/providertest` drives the emitted data through the real
  functions over the real engine: a delivery through `applehealthingest`, a
  push through `garminpush`, a sync through the loopback fake over the
  generated recordings, then the adapter samples. Those run under
  `mise run test:db:rest`.
