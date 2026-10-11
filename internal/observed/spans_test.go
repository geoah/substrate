package observed

import (
	"testing"
	"time"
)

// A priority list resolves per stored interval: the higher source wins where
// it holds, the lower fills its gaps, and the total time is the union's, so
// it does not depend on how the range is later cut into buckets.
func TestPrioritizeFillsTheGapsOfTheHigherSource(t *testing.T) {
	strap := []Point{held(gauge(10, 120), dur(10)), held(gauge(40, 130), dur(10))}
	watch := []Point{held(gauge(0, 60), dur(60))}
	w := window(0, 60)
	got := Prioritize([][]Span{Spans(strap, w.From, w.To, 0), Spans(watch, w.From, w.To, 0)})
	if ms := HeldMs(got[0]); ms != dur(20).Milliseconds() {
		t.Errorf("the strap holds %d ms, want 20 minutes", ms)
	}
	if ms := HeldMs(got[1]); ms != dur(40).Milliseconds() {
		t.Errorf("the watch fills %d ms, want the 40 minutes the strap left", ms)
	}
	all := append(append([]Span(nil), got[0]...), got[1]...)
	twa, _ := TWAOf(all)
	if want := (120*10 + 130*10 + 60*40) / 60.0; !near(twa, want) {
		t.Errorf("twa = %v, want %v", twa, want)
	}
	// Cutting the resolved spans at 15 minutes changes nothing.
	var parts int64
	for start := 0.0; start < 60; start += 15 {
		parts += HeldMs(Clip(all, at(start), at(start+15)))
	}
	if parts != dur(60).Milliseconds() {
		t.Errorf("the 15-minute parts hold %d ms, want the whole hour", parts)
	}
	if !Covered(got[0], at(12)) || Covered(got[0], at(25)) {
		t.Error("Covered misreads the strap's spans")
	}
}

// The window reducers are the span reducers over Held: one implementation.
func TestWindowReducersAreTheSpanReducers(t *testing.T) {
	points := []Point{coded(0, "closed"), coded(20, "open"), coded(25, "closed")}
	w := window(0, 60)
	if got, want := TimeInState(points, w)["open"], TimeInStateOf(Spans(points, w.From, w.To, 0))["open"]; got != want || got != 5*time.Minute {
		t.Errorf("open = %v and %v, want 5m", got, want)
	}
}
