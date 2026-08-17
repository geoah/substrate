package sandboxtest

import "testing"

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
