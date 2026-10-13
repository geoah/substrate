package synth

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// span is one night or one workout as an emitter wrote it.
type span struct{ start, end time.Time }

// TestEmittersAgreeOnTheNightsAndWorkouts holds the four emitters to one
// life: each writes every night and every workout the persona has, within a
// minute of the persona's own bounds, so no source says asleep where
// another says awake.
func TestEmittersAgreeOnTheNightsAndWorkouts(t *testing.T) {
	t.Parallel()
	p, err := NewPersona(Options{Start: NewDate(2026, time.September, 12), End: NewDate(2026, time.September, 14)})
	if err != nil {
		t.Fatal(err)
	}
	var nights, workouts []span
	for _, d := range p.Days() {
		day := p.Day(d)
		nights = append(nights, span{day.Sleep.Start, day.Sleep.End})
		for _, w := range day.Workouts {
			workouts = append(workouts, span{w.Start, w.End})
		}
	}
	for name, got := range map[string][2][]span{
		"whoop":       whoopSpans(t, p),
		"googlefit":   fitSpans(t, p),
		"applehealth": ahSpans(t, p),
		"garmin":      garminSpans(t, p),
	} {
		for what, pair := range map[string][2][]span{"night": {nights, got[0]}, "workout": {workouts, got[1]}} {
			want, have := pair[0], pair[1]
			if len(have) != len(want) {
				t.Errorf("%s writes %d %ss, the persona has %d", name, len(have), what, len(want))
				continue
			}
			sort.Slice(have, func(i, j int) bool { return have[i].start.Before(have[j].start) })
			for i := range want {
				if d := have[i].start.Sub(want[i].start).Abs(); d > time.Minute {
					t.Errorf("%s's %s starts %s, the persona's %s", name, what, have[i].start.UTC(), want[i].start.UTC())
				}
				if d := have[i].end.Sub(want[i].end).Abs(); d > time.Minute {
					t.Errorf("%s's %s ends %s, the persona's %s", name, what, have[i].end.UTC(), want[i].end.UTC())
				}
			}
		}
	}
}

func whoopSpans(t *testing.T, p *Persona) [2][]span {
	t.Helper()
	out, err := Whoop{}.Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	var r [2][]span
	for _, f := range out.Files {
		which := -1
		switch {
		case strings.HasPrefix(f.Path, "recordings/GET_v2_activity_sleep"):
			which = 0
		case strings.HasPrefix(f.Path, "recordings/GET_v2_activity_workout"):
			which = 1
		}
		if which < 0 {
			continue
		}
		var page whoopPage
		if err := json.Unmarshal(f.Body, &page); err != nil {
			t.Fatal(err)
		}
		for _, rec := range page.Records {
			if rec["nap"] == true {
				continue
			}
			r[which] = append(r[which], span{whoopInstant(t, rec["start"]), whoopInstant(t, rec["end"])})
		}
	}
	return r
}

func fitSpans(t *testing.T, p *Persona) [2][]span {
	t.Helper()
	out, err := GoogleFit{}.Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	var r [2][]span
	for _, f := range out.Files {
		if f.Path != "recordings/"+RecordingName("GET", fitAPI+"/sessions") {
			continue
		}
		var list struct {
			Session []map[string]any `json:"session"`
		}
		if err := json.Unmarshal(f.Body, &list); err != nil {
			t.Fatal(err)
		}
		ms := func(v any) time.Time {
			n, _ := strconv.ParseInt(v.(string), 10, 64)
			return time.UnixMilli(n)
		}
		for _, s := range list.Session {
			which := 1
			if s["activityType"] == float64(fitSleeping) {
				if s["name"] != "Sleep" {
					continue
				}
				which = 0
			}
			r[which] = append(r[which], span{ms(s["startTimeMillis"]), ms(s["endTimeMillis"])})
		}
	}
	return r
}

func ahSpans(t *testing.T, p *Persona) [2][]span {
	t.Helper()
	_, out := ahEmit(t, p.Options)
	var r [2][]span
	for _, d := range out.Deliveries {
		doc := ahDecode(t, d)
		for _, m := range doc.Data.Metrics {
			if m.Name != "sleep_analysis" {
				continue
			}
			for _, row := range m.Data {
				r[0] = append(r[0], span{ahWhen(t, row["inBedStart"]), ahWhen(t, row["inBedEnd"])})
			}
		}
		for _, w := range doc.Data.Workouts {
			r[1] = append(r[1], span{ahWhen(t, w["start"]), ahWhen(t, w["end"])})
		}
	}
	return r
}

func garminSpans(t *testing.T, p *Persona) [2][]span {
	t.Helper()
	_, _, deliveries, bodies := garminEmit(t, p.Options)
	var r [2][]span
	for i := range deliveries {
		for _, e := range bodies[i]["sleeps"] {
			start := gInt(t, e, "startTimeInSeconds")
			end := start
			for _, list := range e["sleepLevelsMap"].(map[string]any) {
				for _, seg := range list.([]any) {
					end = max(end, gInt(t, seg.(map[string]any), "endTimeInSeconds"))
				}
			}
			r[0] = append(r[0], span{time.Unix(start, 0), time.Unix(end, 0)})
		}
		for _, e := range bodies[i]["activities"] {
			start := gInt(t, e, "startTimeInSeconds")
			r[1] = append(r[1], span{time.Unix(start, 0), time.Unix(start+gInt(t, e, "durationInSeconds"), 0)})
		}
	}
	return r
}
