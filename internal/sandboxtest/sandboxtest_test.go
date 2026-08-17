package sandboxtest

import "testing"

// sizes reads both sets the way Run does.
func sizes() (int, int) {
	mu.Lock()
	defer mu.Unlock()
	return len(guarded), len(asserted)
}

// A case that stops at a later precondition (the uid cannot make a device
// node, the probe will not build) proved nothing, and counting it at the guard
// would let CI report a confinement that never happened.
func TestCaseCountsWhatReachedItsAssertions(t *testing.T) {
	g, a := sizes()
	t.Run("reaches its assertions", func(t *testing.T) {
		Case(t)
	})
	t.Run("skips after its guard", func(t *testing.T) {
		Case(t)
		t.Skip("a precondition this machine does not meet")
	})
	gotG, gotA := sizes()
	if gotG-g != 2 {
		t.Errorf("guarded grew by %d, want 2", gotG-g)
	}
	if gotA-a != 1 {
		t.Errorf("asserted grew by %d, want 1", gotA-a)
	}
}

// -count=N and -cpu=1,2 run the same case again under the same name, and the
// gate holds the number of cases to an exact one, so recording a repeat as a
// second case would fail a run that is doing exactly what it was asked.
func TestCaseCountsOneNameOnce(t *testing.T) {
	g, _ := sizes()
	t.Run("one case, run twice", func(t *testing.T) {
		Case(t)
		Case(t)
	})
	if gotG, _ := sizes(); gotG-g != 1 {
		t.Errorf("guarded grew by %d for one case recorded twice, want 1", gotG-g)
	}
}

// The variable decides whether a whole CI job can pass with every confinement
// case skipped, so what counts as set is worth pinning: unset and "0" are the
// developer default, anything else is the gate.
func TestRequired(t *testing.T) {
	for value, want := range map[string]bool{"": false, "0": false, "1": true, "true": true} {
		t.Setenv(EnvRequire, value)
		if got := Required(); got != want {
			t.Errorf("%s=%q: Required() = %v, want %v", EnvRequire, value, got, want)
		}
	}
}
