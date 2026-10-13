package synth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Whoop is the WHOOP v2 developer API as the provider e2e runner's mock
// serves it: the member's profile and body measurement, and the cycle,
// recovery, sleep and workout collections, 25 records a page, newest first,
// each page after the first named for the `nextToken` the page before it
// answered (the mock drops `limit` and `start`, which are pagination
// parameters, and keeps `nextToken`, which is not). The shapes are the
// recorded fixtures' under internal/providere2e/fixtures/whoop and the
// fields `whoopsync` reads.
//
// One cycle per local day, from that morning's wake to the next morning's;
// the last day's cycle is the current one, with no `end`. Each cycle's
// recovery scores the night that ended at the cycle's start, and that night
// names the cycle in `cycle_id`; a nap belongs to its day's cycle. WHOOP
// sends summaries only, so nothing here is a sample series. A sick day's nap
// is UNSCORABLE and carries no score, and the range's first three
// recoveries are calibrating.
type Whoop struct{}

func init() { register(Whoop{}) }

// Name is the emitter's name.
func (Whoop) Name() string { return "whoop" }

const (
	// whoopUserID is the member, the same id the recorded fixtures use for
	// the same Ada.
	whoopUserID = 4021873
	// whoopPageSize is WHOOP's page cap and the sync's `limit`.
	whoopPageSize = 25
	// whoopMockPort is the port providers/whoop/e2e.json pins: the rewired
	// function body names it, so it must not move between runs.
	whoopMockPort = 48116
)

// whoopCalibrationDays is how many days a new strap calibrates over before
// WHOOP scores a recovery against the member's own baseline; the strap is
// new on the range's first day.
const whoopCalibrationDays = 3

// The four collections, in the order the sync walks them.
const (
	whoopCyclePath    = "/v2/cycle"
	whoopRecoveryPath = "/v2/recovery"
	whoopSleepPath    = "/v2/activity/sleep"
	whoopWorkoutPath  = "/v2/activity/workout"
)

// whoopSports is the persona's sport as WHOOP's `sport_name`. `sport_id` is
// not written: WHOOP documents it as absent on records after 2025-09-01 and
// the adapter does not read it.
var whoopSports = map[Sport]string{
	Running: "running", Cycling: "cycling", Strength: "weightlifting", Yoga: "yoga", Walking: "walking",
}

// whoopRecord is one collection record and the instant it sorts by.
type whoopRecord struct {
	at  time.Time
	doc map[string]any
}

// Emit writes recordings/ and e2e.json.
func (Whoop) Emit(p *Persona) (*Output, error) {
	e := &whoopEmitter{p: p, days: p.Days()}
	out := &Output{Setup: Setup{Samples: []string{"samples.substrate.reamde.dev/healthwhoop"}}}
	add := func(name string, body any) {
		out.Files = append(out.Files, File{Path: "recordings/" + name, Body: JSON(body)})
	}
	add(RecordingName("GET", "/v2/user/profile/basic"), map[string]any{
		"user_id": whoopUserID, "email": p.Person.Email,
		"first_name": p.Person.FirstName, "last_name": p.Person.LastName,
	})
	body := map[string]any{
		"height_meter":   whoopDec(p.Person.HeightM, 4),
		"max_heart_rate": p.Person.MaxHR,
	}
	for _, d := range e.days {
		if kg := p.Day(d).WeightKg; kg != 0 {
			body["weight_kilogram"] = whoopDec(kg, 4)
			break
		}
	}
	add(RecordingName("GET", "/v2/user/measurement/body"), body)

	sleeps, nights := e.sleeps()
	for _, c := range []struct {
		path string
		rows []whoopRecord
	}{
		{whoopCyclePath, e.cycles()},
		{whoopRecoveryPath, e.recoveries(nights)},
		{whoopSleepPath, sleeps},
		{whoopWorkoutPath, e.workouts()},
	} {
		for _, f := range whoopPages(c.path, c.rows) {
			add(f.name, f.body)
		}
	}
	out.Files = append(out.Files, File{Path: "e2e.json", Body: JSON(whoopE2E(p))})
	return out, nil
}

// whoopE2E is providers/whoop/e2e.json for this data: the recorded one's
// hosts, config, triggers, continuation and port, every collection on, and
// a cold walk from WHOOP's fixed epoch, so no generated record falls before
// the first window whatever range was generated and whenever it is loaded.
func whoopE2E(p *Persona) map[string]any {
	return map[string]any{
		"_generated": fmt.Sprintf("internal/synth (whoop.go): %s to %s, seed %q. The runner reads this in place of the recorded e2e.json.",
			p.Options.Start, p.Options.End, p.Options.Seed),
		"_hosts": "WHOOP serves the OAuth dance and the data API from one host under two prefixes, so the rewire names the prefixes.",
		"hosts":  []any{"https://api.prod.whoop.com/developer", "https://api.prod.whoop.com/oauth"},
		"config": map[string]any{"clientId": "e2e-client-id", "clientSecret": "e2e-client-secret"},
		"account": map[string]any{
			"enabledProfile": true, "enabledCycles": true, "enabledRecovery": true,
			"enabledSleep": true, "enabledWorkouts": true,
			"syncFrequency": "hourly", "backfillDepth": "all",
		},
		"_triggers":      "whoop-on-connect drives the first sync, whoop-on-request a re-drive; whoop-scheduled stays off so a catch-up fire cannot race the drain.",
		"triggers":       []any{"whoop-on-connect", "whoop-on-request"},
		"_settleSeconds": "a ceiling, not a delay: the 90-day drain takes about 10 s, and in a repository other loaders write to the runner waits out the whole ceiling, because the lag it waits on counts their entries too.",
		"settleSeconds":  300,
		"continueRounds": 20,
		"continue":       map[string]any{"property": "syncRequestedAt", "value": "now"},
		"mockPort":       whoopMockPort,
	}
}

type whoopEmitter struct {
	p    *Persona
	days []Date
}

// --- the helpers -------------------------------------------------------------

func (e *whoopEmitter) noisy(what string, key any, v, sd float64) float64 {
	return e.p.Noisy(fmt.Sprintf("whoop|%s|%v", what, key), v, sd)
}

// intn is a deterministic integer in [lo, hi].
func (e *whoopEmitter) intn(lo, hi int, key ...any) int {
	return lo + e.p.rng(append([]any{"whoop"}, key...)...).IntN(hi-lo+1)
}

func (e *whoopEmitter) float(key ...any) float64 {
	return e.p.rng(append([]any{"whoop"}, key...)...).Float64()
}

// jit gives an instant WHOOP's millisecond precision, deterministically.
func (e *whoopEmitter) jit(t time.Time, key ...any) time.Time {
	return t.Truncate(time.Second).Add(time.Duration(e.intn(0, 999, append([]any{"ms"}, key...)...)) * time.Millisecond)
}

// whoopStamp is an instant as WHOOP spells it: UTC, milliseconds, `Z`.
func whoopStamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// whoopTZD is the UTC offset at t in its own zone, as a Time Zone Designator.
func whoopTZD(t time.Time) string { return t.Format("-07:00") }

// whoopDec is a float as WHOOP sends one: at most places decimals, and a
// whole number still spelled with `.0` (`"recovery_score": 55.0`).
func whoopDec(v float64, places int) json.Number {
	s := strconv.FormatFloat(v, 'f', places, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return json.Number(s)
}

// whoopUUID is a v4-shaped UUID derived from the key, so a sleep or a
// workout keeps its id across runs.
func whoopUUID(p *Persona, key ...any) string {
	h := sha256.Sum256([]byte(p.Options.Seed + "|whoop|" + fmt.Sprint(key...)))
	b := h[:16]
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	x := hex.EncodeToString(b)
	return x[:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:]
}

func whoopMs(d time.Duration) int64 { return d.Milliseconds() }

// wake is the instant the night ending on d ended: a cycle boundary.
func (e *whoopEmitter) wake(d Date) time.Time { return e.jit(e.p.Day(d).Sleep.End, "wake", d) }

// cycleID is WHOOP's integer id for the cycle starting on d, rising daily.
func (e *whoopEmitter) cycleID(d Date) int {
	return 735464000 + e.p.Index(d)*122400 + e.intn(0, 2500, "cycleid", d)
}

// last reports whether d is the range's last day, whose cycle is open.
func (e *whoopEmitter) last(d Date) bool { return d == e.p.Options.End }

// --- the collections ---------------------------------------------------------

func (e *whoopEmitter) cycles() []whoopRecord {
	var out []whoopRecord
	for _, d := range e.days {
		day := e.p.Day(d)
		start := e.wake(d)
		var end, updated, spanEnd time.Time
		if e.last(d) {
			// The current cycle: scored up to the evening before the sync.
			updated = e.jit(d.At(day.Zone, 22, 47), "openupd", d)
			spanEnd = updated
		} else {
			end = e.wake(d.Add(1))
			updated = end.Add(time.Duration(e.intn(25, 200, "cycupd", d))*time.Minute +
				time.Duration(e.intn(0, 999, "cycupdms", d))*time.Millisecond)
			spanEnd = end
		}
		var sum, n float64
		peak := 0
		for t := start; t.Before(spanEnd); t = t.Add(5 * time.Minute) {
			v := e.p.HR(t)
			sum += float64(v)
			n++
			peak = max(peak, v)
		}
		for _, w := range day.Workouts {
			for _, s := range w.HRSeries(30 * time.Second) {
				peak = max(peak, int(s.Value))
			}
		}
		hours := spanEnd.Sub(start).Hours()
		kcal := float64(day.BMRKcal())*hours/24 + float64(day.ActiveKcal())
		rec := map[string]any{
			"id":              e.cycleID(d),
			"user_id":         whoopUserID,
			"created_at":      whoopStamp(start.Add(7*time.Second + time.Duration(e.intn(0, 999, "cyccre", d))*time.Millisecond)),
			"updated_at":      whoopStamp(updated),
			"start":           whoopStamp(start),
			"timezone_offset": whoopTZD(start.In(day.Zone)),
			"score_state":     "SCORED",
			"score": map[string]any{
				"strain":             whoopDec(math.Min(21, math.Max(0.1, e.noisy("strain", d, day.Strain(), 0.3))), 4),
				"kilojoule":          whoopDec(kcal*4.184*(1+e.noisy("kj", d, 0, 0.03)), 3),
				"average_heart_rate": int(math.Round(sum/math.Max(1, n) + e.noisy("cycavg", d, 0, 1))),
				"max_heart_rate":     int(math.Round(math.Min(float64(e.p.Person.MaxHR+3), float64(peak)+e.noisy("cycmax", d, 0, 1.5)))),
			},
		}
		if !e.last(d) {
			rec["end"] = whoopStamp(end)
		}
		out = append(out, whoopRecord{start, rec})
	}
	return out
}

func (e *whoopEmitter) recoveries(nights map[Date]string) []whoopRecord {
	var out []whoopRecord
	for _, d := range e.days {
		day := e.p.Day(d)
		s := day.Sleep
		created := e.wake(d).Add(14*time.Second + time.Duration(e.intn(0, 999, "reccre", d))*time.Millisecond)
		updated := created.Add(time.Duration(e.intn(15, 150, "recupd", d))*time.Minute +
			time.Duration(e.intn(0, 999, "recupdms", d))*time.Millisecond)
		spo2 := math.Round(math.Min(99.9, e.noisy("spo2", d, s.SpO2Avg, 0.4))*16) / 16
		out = append(out, whoopRecord{created, map[string]any{
			"cycle_id":    e.cycleID(d),
			"sleep_id":    nights[d],
			"user_id":     whoopUserID,
			"created_at":  whoopStamp(created),
			"updated_at":  whoopStamp(updated),
			"score_state": "SCORED",
			"score": map[string]any{
				"user_calibrating":   d.Before(e.p.Options.Start.Add(whoopCalibrationDays)),
				"recovery_score":     whoopDec(float64(day.Recovery), 0),
				"resting_heart_rate": whoopDec(math.Round(e.noisy("rhr", d, float64(s.RestingHR), 1)), 0),
				"hrv_rmssd_milli":    whoopDec(math.Max(12, e.noisy("hrv", d, s.HRVRMSSDMs, 2.5)), 6),
				"spo2_percentage":    whoopDec(spo2, 4),
				"skin_temp_celsius":  whoopDec(33.6+s.SkinTempDevC+e.noisy("skin", d, 0, 0.1), 2),
			},
		}})
	}
	return out
}

// need is WHOOP's sleep need for the night ending on d, by component, in ms.
func (e *whoopEmitter) need(d Date) map[string]any {
	baseline := int64(27120000 + int(120000*math.Sin(float64(e.p.Index(d))/11)))
	prev := e.p.Day(d.Add(-1))
	asleep := int64(prev.Sleep.AsleepS()) * 1000
	var napMs int64
	for _, n := range prev.Naps {
		napMs += whoopMs(n.Duration())
	}
	return map[string]any{
		"baseline_milli":                baseline,
		"need_from_sleep_debt_milli":    int64(math.Max(0, float64(baseline-asleep)*0.45)),
		"need_from_recent_strain_milli": int64(math.Max(0, prev.Strain()-7) * 170000),
		"need_from_recent_nap_milli":    -int64(float64(napMs) * 0.9),
	}
}

func whoopNeedTotal(need map[string]any) int64 {
	var t int64
	for _, v := range need {
		t += v.(int64)
	}
	return t
}

// whoopClockMin is the minute of the day at t in its own zone.
func whoopClockMin(t time.Time) int { return t.Hour()*60 + t.Minute() }

// consistency is WHOOP's sleep consistency: how close this night's bedtime
// and wake time sit to the night before's.
func (e *whoopEmitter) consistency(d Date) float64 {
	s, prev := e.p.Day(d).Sleep, e.p.Day(d.Add(-1)).Sleep
	gap := func(a, b time.Time) float64 {
		x := math.Abs(float64(whoopClockMin(a) - whoopClockMin(b)))
		return math.Min(x, 1440-x)
	}
	delta := gap(s.Start, prev.Start) + gap(s.End, prev.End)
	return math.Max(35, math.Min(98, math.Round(96-0.35*delta+e.noisy("cons", d, 0, 3))))
}

// sleeps is every night and nap, and the night ending on each date by id,
// which is what that date's recovery names.
func (e *whoopEmitter) sleeps() ([]whoopRecord, map[Date]string) {
	var out []whoopRecord
	nights := map[Date]string{}
	for _, d := range e.days {
		day := e.p.Day(d)
		s := day.Sleep
		id := whoopUUID(e.p, "sleep", d, "night")
		nights[d] = id
		start, end := e.jit(s.Start, "sleepstart", d), e.wake(d)
		inBed := whoopMs(end.Sub(start))
		tot := s.StageTotals()
		awake := max(0, int64(tot[Awake])*1000+int64(e.noisy("awakeshift", d, 0, 120)*1000))
		deep := max(0, int64(tot[Deep])*1000+int64(e.noisy("deepshift", d, 0, 240)*1000))
		rem := int64(tot[REM]) * 1000
		var noData int64
		if e.float("nodatap", d) < 0.15 {
			noData = int64(e.intn(30, 400, "nodata", d)) * 1000
		}
		light := inBed - awake - deep - rem - noData
		asleep := light + deep + rem
		need := e.need(d)
		wakes, rems := 0, 0
		for _, st := range s.Stages {
			switch st.Stage {
			case Awake:
				wakes++
			case REM:
				rems++
			}
		}
		disturbances := max(0, wakes-1) + e.intn(3, 9, "dist", d)
		if day.Ill {
			disturbances += 4
		}
		created := end.Add(10*time.Second + time.Duration(e.intn(0, 999, "slcre", d))*time.Millisecond)
		updated := created.Add(time.Duration(e.intn(60, 200, "slupd", d))*time.Minute +
			time.Duration(e.intn(0, 999, "slupdms", d))*time.Millisecond)
		out = append(out, whoopRecord{start, map[string]any{
			"id": id, "cycle_id": e.cycleID(d), "user_id": whoopUserID,
			"created_at": whoopStamp(created), "updated_at": whoopStamp(updated),
			"start": whoopStamp(start), "end": whoopStamp(end),
			"timezone_offset": whoopTZD(start), "nap": false, "score_state": "SCORED",
			"score": map[string]any{
				"stage_summary": map[string]any{
					"total_in_bed_time_milli":          inBed,
					"total_awake_time_milli":           awake,
					"total_no_data_time_milli":         noData,
					"total_light_sleep_time_milli":     light,
					"total_slow_wave_sleep_time_milli": deep,
					"total_rem_sleep_time_milli":       rem,
					"sleep_cycle_count":                rems,
					"disturbance_count":                disturbances,
				},
				"sleep_needed":                 need,
				"respiratory_rate":             whoopDec(math.Round(e.noisy("resp", d, s.RespRate, 0.25)*1024)/1024, 7),
				"sleep_performance_percentage": whoopDec(math.Min(100, math.Round(float64(asleep)/float64(whoopNeedTotal(need))*100)), 0),
				"sleep_consistency_percentage": whoopDec(e.consistency(d), 0),
				"sleep_efficiency_percentage":  whoopDec(float64(asleep)/float64(inBed)*100, 7),
			},
		}})
		for k, nap := range day.Naps {
			ns, ne := e.jit(nap.Start, "napstart", d, k), e.jit(nap.End, "napend", d, k)
			nc := ne.Add(11*time.Second + time.Duration(e.intn(0, 999, "napcre", d, k))*time.Millisecond)
			nu := nc.Add(time.Duration(e.intn(20, 60, "napupd", d, k)) * time.Minute)
			rec := map[string]any{
				"id": whoopUUID(e.p, "sleep", d, "nap", k), "cycle_id": e.cycleID(d), "user_id": whoopUserID,
				"created_at": whoopStamp(nc), "updated_at": whoopStamp(nu),
				"start": whoopStamp(ns), "end": whoopStamp(ne),
				"timezone_offset": whoopTZD(ns), "nap": true, "score_state": "SCORED",
			}
			// A sick day's nap is restless enough that WHOOP cannot score it:
			// the record carries no score.
			if day.Ill {
				rec["score_state"] = "UNSCORABLE"
				out = append(out, whoopRecord{ns, rec})
				continue
			}
			bed := whoopMs(ne.Sub(ns))
			napAwake := 240000 + int64(e.noisy("napawake", fmt.Sprint(d, k), 0, 40)*1000)
			napLight := bed - napAwake
			rec["score"] = map[string]any{
				"stage_summary": map[string]any{
					"total_in_bed_time_milli":          bed,
					"total_awake_time_milli":           napAwake,
					"total_no_data_time_milli":         0,
					"total_light_sleep_time_milli":     napLight,
					"total_slow_wave_sleep_time_milli": 0,
					"total_rem_sleep_time_milli":       0,
					"sleep_cycle_count":                0,
					"disturbance_count":                e.intn(0, 2, "napdist", d, k),
				},
				"sleep_needed":                 need,
				"respiratory_rate":             whoopDec(math.Round(e.noisy("napresp", fmt.Sprint(d, k), nap.RespRate, 0.25)*1024)/1024, 7),
				"sleep_performance_percentage": whoopDec(math.Round(float64(napLight)/float64(whoopNeedTotal(need))*100), 0),
				"sleep_consistency_percentage": whoopDec(float64(e.intn(55, 80, "napcons", d, k)), 0),
				"sleep_efficiency_percentage":  whoopDec(float64(napLight)/float64(bed)*100, 7),
			}
			out = append(out, whoopRecord{ns, rec})
		}
	}
	return out, nights
}

// zones is the time in each of WHOOP's six heart rate zones (below 50 %,
// then 10 % bands of the member's max), from the workout's 30-second series
// with the strap's own noise, scaled to the workout's length.
func (e *whoopEmitter) zones(w *Workout, d Date, k int, length int64) map[string]any {
	r := e.p.rng("whoop", "zones", d, k)
	var acc [6]int64
	for _, s := range w.HRSeries(30 * time.Second) {
		f := (s.Value + r.NormFloat64()*2) / float64(e.p.Person.MaxHR)
		z := 0
		switch {
		case f >= 0.9:
			z = 5
		case f >= 0.8:
			z = 4
		case f >= 0.7:
			z = 3
		case f >= 0.6:
			z = 2
		case f >= 0.5:
			z = 1
		}
		acc[z] += 30000
	}
	var total, top int64
	for _, a := range acc {
		total += a
	}
	big := 0
	for i := range acc {
		acc[i] = acc[i] * length / max(1, total)
		top += acc[i]
		if acc[i] > acc[big] {
			big = i
		}
	}
	acc[big] += length - top
	names := []string{"zone_zero_milli", "zone_one_milli", "zone_two_milli", "zone_three_milli", "zone_four_milli", "zone_five_milli"}
	out := map[string]any{}
	for i, n := range names {
		out[n] = acc[i]
	}
	return out
}

func (e *whoopEmitter) workouts() []whoopRecord {
	var out []whoopRecord
	for _, d := range e.days {
		for k, w := range e.p.Day(d).Workouts {
			start, end := e.jit(w.Start, "wostart", d, k), e.jit(w.End, "woend", d, k)
			created := end.Add(time.Duration(e.intn(60, 300, "wocre", d, k))*time.Second +
				time.Duration(e.intn(0, 999, "wocrems", d, k))*time.Millisecond)
			updated := created.Add(time.Duration(e.intn(10, 70, "woupd", d, k))*time.Minute +
				time.Duration(e.intn(0, 999, "woupdms", d, k))*time.Millisecond)
			key := fmt.Sprint(d, k)
			avg := int(math.Round(e.noisy("woavg", key, float64(w.AvgHR), 1.2)))
			mx := max(avg+5, int(math.Round(math.Min(float64(e.p.Person.MaxHR+2), e.noisy("womax", key, float64(w.MaxHR), 1.5)))))
			recorded := whoopDec(100, 0)
			if e.float("worec", d, k) >= 0.8 {
				recorded = whoopDec(float64(e.intn(94, 99, "worecpct", d, k)), 0)
			}
			score := map[string]any{
				"strain":             whoopDec(math.Min(21, math.Max(0, e.noisy("wostrain", key, w.Strain, 0.3))), 4),
				"average_heart_rate": avg,
				"max_heart_rate":     mx,
				"kilojoule":          whoopDec(float64(w.Kcal)*4.184*(1+e.noisy("wokj", key, 0, 0.04)), 5),
				"percent_recorded":   recorded,
				"zone_durations":     e.zones(w, d, k, whoopMs(end.Sub(start))),
			}
			if w.DistanceM > 0 {
				lo, hi := 18.0, 95.0
				if w.Sport == Cycling {
					lo, hi = 120, 340
				}
				score["distance_meter"] = whoopDec(e.noisy("wodist", key, w.DistanceM, w.DistanceM*0.012), 8)
				score["altitude_gain_meter"] = whoopDec(lo+(hi-lo)*e.float("woalt", d, k), 7)
				score["altitude_change_meter"] = whoopDec(-3+6*e.float("wochg", d, k), 7)
			}
			out = append(out, whoopRecord{start, map[string]any{
				"id": whoopUUID(e.p, "workout", d, k), "user_id": whoopUserID,
				"created_at": whoopStamp(created), "updated_at": whoopStamp(updated),
				"start": whoopStamp(start), "end": whoopStamp(end),
				"timezone_offset": whoopTZD(start), "sport_name": whoopSports[w.Sport],
				"score_state": "SCORED", "score": score,
			}})
		}
	}
	return out
}

// --- the pages ---------------------------------------------------------------

type whoopFile struct {
	name string
	body map[string]any
}

// whoopToken is the `next_token` page n answers: base64 of a digest, kept to
// letters and digits so the recording name holds it verbatim.
func whoopToken(path string, n int) string {
	for salt := 0; ; salt++ {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", path, n, salt)))
		tok := base64.StdEncoding.EncodeToString(h[:12])
		if !strings.ContainsAny(tok, "+/=") {
			return tok
		}
	}
}

// whoopPages splits a collection into pages of 25, newest first: page one is
// the bare path, each later page is named for the token the page before it
// answered, and the last page answers no token.
func whoopPages(path string, rows []whoopRecord) []whoopFile {
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].at.After(rows[j].at) })
	var out []whoopFile
	prev := ""
	for i := 0; i == 0 || i < len(rows); i += whoopPageSize {
		chunk := rows[i:min(i+whoopPageSize, len(rows))]
		records := make([]any, 0, len(chunk))
		for _, r := range chunk {
			records = append(records, r.doc)
		}
		body := map[string]any{"records": records}
		if i+whoopPageSize < len(rows) {
			body["next_token"] = whoopToken(path, len(out)+1)
		}
		name := RecordingName("GET", path)
		if prev != "" {
			name = RecordingName("GET", path, Pair{"nextToken", prev})
		}
		out = append(out, whoopFile{name, body})
		prev, _ = body["next_token"].(string)
	}
	return out
}
