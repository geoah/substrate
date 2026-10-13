package synth

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// A second generate into the same directory replaces the first: `load`
// posts every delivery it finds, so a file the second run did not write
// would load a second life beside the first.
func TestWriteOutputReplacesAnEarlierRun(t *testing.T) {
	dir := t.TempDir()
	write := func(o Options) []string {
		t.Helper()
		p, err := NewPersona(o)
		if err != nil {
			t.Fatal(err)
		}
		out, err := AppleHealth{}.Emit(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := WriteOutput(dir, AppleHealth{}, out); err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, d := range out.Deliveries {
			names = append(names, d.Name)
		}
		return names
	}
	write(Options{Start: NewDate(2026, time.September, 12), End: NewDate(2026, time.September, 14)})
	stray := filepath.Join(dir, "applehealth", "deliveries", "0009_stray.json")
	if err := os.WriteFile(stray, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := write(Options{Start: NewDate(2026, time.October, 1), End: NewDate(2026, time.October, 2)})

	files, err := filepath.Glob(filepath.Join(dir, "applehealth", "deliveries", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, filepath.Base(f))
	}
	if !slices.Equal(got, []string{"0001_" + want[0] + ".json", "0002_" + want[1] + ".json"}) {
		t.Fatalf("after the second run the directory holds %v, want only the second run's %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "applehealth", "setup.json")); err != nil {
		t.Fatalf("the second run's setup.json: %v", err)
	}
}
