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
value derives from the seed and a key, so two runs agree to the byte and the
emitters run independently and still describe one life. The default
(`synth.DefaultOptions`) is Ada, Athens, the 90 days ending 2026-10-11, with a
cold from 2026-08-26 to 2026-08-29 (resting heart rate up, HRV down, sleep
longer and broken, no training) and a trip to London from 2026-09-20 to
2026-09-24 (the zone moves with her). A range of other days keeps the cold and
the trip on their dates whether or not it holds a day of them, so `-from
2026-09-01 -to 2026-09-30` is the same September, and a range starting
2026-09-25 still scores its first night against the trip's last day. Her fitness progresses over a fixed training block,
2026-07-14 to 2026-10-11, and the WHOOP cycle ids count days from its start,
so two ranges that share a day give it the same values and ids, and loading
both updates its records instead of adding a second set. The one exception is
the strap: it is new on the range's first day, so the first three recoveries
of a range are calibrating. She trains six days most weeks (an easy run,
intervals, strength, a long run that grows with the block, yoga, a ride every
other week), weighs in four mornings a week, takes her blood pressure on
Sundays, checks her fasting glucose on a home meter on Wednesday and Saturday
mornings, and has three lab panels: two from an Athens lab in SI units and
one from a US lab in mg/dL, ng/mL and %, on the metrics' mass twins and the
NGSP HbA1c (`health/hba1c`).

`Persona.Day(d)` is the ground truth for one local day: the night that ended
that morning with its stages, the workouts with their heart-rate curves, the
heart rate at any instant (the night that begins before the day ends
included), steps per bucket, energy, recovery and strain in WHOOP's terms,
the weigh-in, the blood pressure and
glucose readings if there were any. An emitter samples it at the resolution
its provider sends and adds its own device noise with `Persona.Noisy`, keyed
by provider, so a watch reads two beats off another watch and two scales
disagree by a few hundred grams.

The persona holds the rules every emitter would otherwise repeat:

- A local day ends where the next one starts, so the day of the flight out
  lasts 26 hours, the last day in London 22, and no instant belongs to two
  days or to none.
- A workout starts 15 to 35 minutes after the night ends at the earliest, so
  no night overlaps a workout.
- A weigh-in and a blood pressure or glucose reading happen after waking and
  outside every workout and the half hour after it.
- Yoga has its own heart-rate shape; every other session warms up from
  below its average and cools down below it.
- A step is `synth.StrideM` (0.74 m) and burns `synth.KcalPerStep`
  (0.04 kcal) for every emitter.

A `Persona` caches the days it builds and is not safe for concurrent use;
build one per goroutine.

## The emitters

| Emitter | Writes | How it reaches a substrate |
| --- | --- | --- |
| `whoop` | `recordings/`: the WHOOP v2 profile, body measurement, and the cycle, recovery, sleep and workout collections, 25 records a page, later pages named for their `nextToken`; `e2e.json` for the runner | `synth load` runs the provider e2e runner, which drives the real `whoopsync` over the recordings |
| `applehealth` | `deliveries/`: one Health Auto Export request per local day (Time Grouping Minutes, Export Version 2) to `applehealth-webhook`, with the app's headers | `synth load` installs the provider, writes the `iphone` account, imports `healthapple`, arms the trigger and posts them one at a time |
| `manual` | `labs.yaml`: three `labreport` records, 30 `labresult` records, the two labs' and the hand-entry `source` records, and one weigh-in typed in on a hotel scale on the trip's third day, when the range holds it | `synth load` writes them through `PUT /api/v1/...` after importing the samples; `substratectl apply -f` does the same |

`applehealth` writes what the Health Auto Export app POSTs at the end of
each local day. Heart rate comes from "Ada's Apple Watch" about every 5
minutes as one-minute `Min`/`Avg`/`Max` rows, with a spread during
workouts. Steps come per 5-minute bucket, from "Ada's iPhone" by day and the
watch before 07:00, after 22:00 and during runs; on about one day in four the
watch also counts the walks to work, to lunch and home, and both devices'
rows are sent for the same buckets, so a reader has to choose between two
sources. Distance, flights, and active and basal energy come one row per
minute that has any, as the app sends them at Time Grouping Minutes: basal
energy every minute, distance and active energy over each step bucket's five
minutes and each workout's minutes, a flight at the minute climbed; in km and
kJ, each bucket counted once. Each night carries resting heart rate,
three SDNN readings, respiratory rate every 15 minutes, SpO2 every 10 and the
sleeping wrist temperature, without a `source` key, as the app sends them.
The watch sends a VO2 max estimate after each run. Weight, body fat and BMI
come on weigh-in mornings and blood pressure on Sundays, from "Withings", and
glucose in mg/dL on meter mornings, from "Contour". The height is typed into
the phone once, in centimeters, on the first morning. The night is one
aggregated `sleep_analysis` row, and each workout carries the app's activity
name and `heartRateData` every 60 s. Dates are `yyyy-MM-dd HH:mm:ss Z` in the
day's zone, `+0300` at home and `+0100` in London. The 90-day default is 90
deliveries (29 MB, about 331,000 rows), written under five sources:
`applehealth/iphone/apple-watch`, `.../iphone`, `.../withings`, `.../contour`
and `.../health` for rows without a source or with merged labels.

`whoop` writes WHOOP's summaries and never a sample series, because WHOOP's
API has none. One physiological cycle per local day, from the onset of the
night that ends that morning to the onset of the next night, as WHOOP
defines a cycle, the last one still open; one recovery per cycle, scoring
the night that opened it, the first three calibrating; one sleep per night
and per nap with the stage totals in milliseconds, a sick day's nap
`UNSCORABLE` with no score; one workout per persona workout under WHOOP's
sport name (`sport_id` is not written: WHOOP stopped sending it after
2025-09-01 and the adapter ignores it). Cycle heart rate samples the
persona's curve every 5 minutes, and the workout zone durations come from
each workout's 30-second series. The 90-day default is 17 recordings and
`e2e.json` (0.2 MB); the `healthwhoop` adapter writes one source,
`whoop/<account id>`, and no traces.

`manual` writes the hand-entered records. Each lab result names the metric
it lands on and a LOINC code that metric lists, with reference range bounds
as quantities. The US lab's results land on the mass twins (for example
`health/ldl_cholesterol_mass` in mg/dL), and the health sample's
`convertlab` function writes each paired result once more in the other unit
system. The hotel weigh-in is held for the day, as the catalogue's body
gauges are.

Recordings for a mock-driven provider are named by `synth.RecordingName`,
the Go form of the rule in `internal/providere2e/runner/mockserver.py`
(`recording_test.go` pins it against names the Python produced and reads the
server's parameter sets from the file), so the provider e2e runner finds every
one. A webhook provider's requests are saved as the `{"request": {...}}` envelopes the
provider tests hold under `testdata/`, one file per delivery in posting
order, with the trigger they go to.

## The command

```bash
mise run synth:generate                              # every emitter -> .dev/synth/<provider>/
mise run synth:generate -- -providers manual -out /tmp/synth
mise run synth:generate -- -from 2026-09-01 -to 2026-09-30 -seed other
SUBSTRATE_TOKEN=… SYNTH_WEBHOOK_KEY=… mise run synth:load -- -server http://127.0.0.1:8080 -authority ada.example.com
```

`generate` is pure: the same flags write the same bytes, and the zones are
compiled in, so a host's zone database does not change them. It replaces
the directory of each provider it writes under `-out`, so nothing of an
earlier run's output for that provider is left. A provider that a narrower
`-providers` leaves out keeps its earlier directory, and `load` without
`-providers` loads it too, so a run that changes the range or the seed names
every provider, or a fresh `-out`.

`load` takes each provider's directory in four steps. A `recordings/`
directory goes through the provider e2e runner
(`internal/providere2e/runner/e2e.py`, so `python3` and `bin/substratectl` are
needed): the bundle is installed rewired at a mock serving the recordings,
the account connects through the mock's OAuth stub, the sync drains, and the
provider's schedule is switched off because the mock dies with the run.
The runner tears down a provider installed from the catalog with
`bundle purge --yes` before it applies its copy, and overwrites the client
config and reconnects the `e2e` account of a copy installed otherwise. So
`load` first refuses a repository whose provider package is
catalog-installed, that holds an account of the provider's account kind
(named by `accountKind` in the generated `e2e.json`) other than `e2e`, or
that holds a client config (`configKind`) whose `clientId` is not the
`e2e-client-id` the runner writes. An `e2e` account is reused only beside
such a config: its id alone does not show synth connected it. Then
`setup.json`, which the emitter declared (`synth.Setup`): a push provider is
installed from the catalog, the `observations` and `health` samples and the
adapter sample are imported, and the records the deliveries name (the
account) are written. The catalog says whether the repository holds each
package: one held at the version the server ships is skipped, an older one is
upgraded, and an adapter imported or upgraded after the runner landed rows is
replayed from the start of the changelog. Then every YAML document file
(`labs.yaml`) is applied; a file generated for another authority is refused
before anything is written, naming the `-authority` to generate it with.
Last, a `deliveries/` directory is posted to
`/webhooks/<authority>/<trigger>` one request at a time, waiting for the
trigger's status to show nothing pending or in flight before the next. The
status counts a request only once the server's spool drain has admitted it,
so a drain slower than the first status read lets the next post go early.

The webhook key comes from `-key` or `SYNTH_WEBHOOK_KEY`, never from the
source, and a load that posts without one is refused before its first
request. `load` arms each trigger with it; a trigger that already has
another key is refused unless `-replace-key` says to replace it, and a
trigger it found disabled is disabled again once its deliveries are posted.
A post the door answers 503 (busy) or that loses its connection is tried
again, after the door's `Retry-After`, up to six times. A delivery that
parks stops the load and names the trigger to read; a delivery an earlier
run parked does not. A stop at a trigger or a delivery (a key refused, a
trigger that cannot be read, a post refused or lost, a run that does not
settle, a park) names the `-resume PROVIDER/NNNN` that takes the load up
again: the providers before it and its own setup are skipped. A stop in a
provider's setup (the runner, an install, an import, a record) names none;
the next load starts over, and skips the packages the repository already
holds at the shipped version.
The bearer comes from `SUBSTRATE_TOKEN`, never from argv, and is sent to the
API alone, never to the webhook door, and an error the load prints spells
the webhook key as `<webhook key>`. An interrupted load interrupts the
runner, which leads a process group of its own so a Ctrl-C reaches it once,
and waits up to a minute while the runner quiesces the account, stops its
mock and removes its workdir; then it removes the temporary config that
holds the token.

## What holds it to spec

- `internal/synth`'s own tests: determinism, the life's invariants (a sleep
  is four to twelve hours, a heart rate stays between 40 and the maximum,
  the ill days walk less and train not at all, days tile time, no night
  overlaps a workout, readings happen awake), the recording names and the
  mock server's parameter sets, a writer that replaces an earlier run, and
  every emitter agreeing on every night and workout within a minute.
- `cmd/synth`'s tests drive `load` against a fake substrate: the import
  order, the authority check, the catalog read, the refusal of a
  repository holding provider data synth did not write, the webhook key and
  its absence from a printed error, the retries, `-resume`, the token kept
  off the webhook door, the absolute `-ctl` and recordings path the runner
  is handed, and an interrupt the runner cleans up after.
- `internal/providertest` drives the emitted data through the real
  functions over the real engine: a delivery through `applehealthingest`, a
  sync through the loopback fake over the
  generated recordings, the hand-entered records through `convertlab`, then
  the adapter samples by their own triggers. Those run under
  `mise run test:db:rest`.
