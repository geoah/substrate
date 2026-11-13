package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
)

// The chain this binary carries is numbered from 2 with no gap, converts to
// the dialect the binary stamps, and has one file per step named for it,
// `conversion_NNNN_<name>.go`, the way the repository migrations do
// (TestRepositoryMigrationsMatchTheirFiles).
func TestTheConversionChainIsNumberedAndFiled(t *testing.T) {
	t.Parallel()
	if err := checkConversionSteps(conversionSteps); err != nil {
		t.Fatal(err)
	}
	if got := chainDialect(conversionSteps); got != maxChangelogDialect {
		t.Fatalf("the chain converts to dialect %d and the binary stamps %d: a step raises maxChangelogDialect to its own dialect", got, maxChangelogDialect)
	}
	files, err := filepath.Glob("conversion_[0-9][0-9][0-9][0-9]_*.go")
	if err != nil {
		t.Fatal(err)
	}
	named := map[string]bool{}
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			named[f] = true
		}
	}
	for _, s := range conversionSteps {
		f := conversionFile(s)
		if !named[f] {
			t.Errorf("step %d (%s) has no file %s", s.Dialect, s.Name, f)
		}
		delete(named, f)
	}
	for f := range named {
		t.Errorf("%s names no step of conversionSteps", f)
	}
}

func conversionFile(s conversionStep) string {
	return fmt.Sprintf("conversion_%04d_%s.go", s.Dialect, s.Name)
}

// checkConversionSteps refuses a chain a step could not be found in by its
// number: one that starts anywhere but 2, skips, repeats a name or has a step
// that rewrites nothing.
func TestAConversionChainIsRefusedOutOfOrder(t *testing.T) {
	t.Parallel()
	line := func(*changelogfile.Entry) error { return nil }
	for name, steps := range map[string][]conversionStep{
		"starts at 3":    {{Dialect: 3, Name: "a", Line: line}},
		"skips one":      {{Dialect: 2, Name: "a", Line: line}, {Dialect: 4, Name: "b", Line: line}},
		"repeats a name": {{Dialect: 2, Name: "a", Line: line}, {Dialect: 3, Name: "a", Line: line}},
		"has no line":    {{Dialect: 2, Name: "a"}},
	} {
		if err := checkConversionSteps(steps); err == nil {
			t.Errorf("a chain that %s was admitted", name)
		}
	}
	ok := []conversionStep{{Dialect: 2, Name: "a", Line: line}, {Dialect: 3, Name: "b", Line: line}}
	if err := checkConversionSteps(ok); err != nil {
		t.Fatalf("a chain numbered 2, 3 was refused: %v", err)
	}
	if got := chainDialect(ok); got != 3 {
		t.Fatalf("the chain converts to %d, want 3", got)
	}
}

// A repository runs the steps above its stamp, and an unstamped changelog
// holds dialect-1 lines, so it runs them all.
func TestARepositoryRunsTheStepsAboveItsStamp(t *testing.T) {
	t.Parallel()
	line := func(*changelogfile.Entry) error { return nil }
	steps := []conversionStep{{Dialect: 2, Name: "a", Line: line}, {Dialect: 3, Name: "b", Line: line}}
	for stored, want := range map[int]string{0: "2 (a), 3 (b)", 1: "2 (a), 3 (b)", 2: "3 (b)", 3: ""} {
		if got := conversionNames(pendingConversions(steps, stored)); got != want {
			t.Errorf("stamped %d: pending %q, want %q", stored, got, want)
		}
	}
}

// The conversion directories a crash may leave are found by their dialect,
// and a swap cut short after its first rename is finished by calling it
// again.
func TestASwapCutShortIsFinishedAgain(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	live := changelogfile.ChangelogDir(repo)
	write := func(dir, body string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "000000000000001.ndjson"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(live, "old")
	write(changelogfile.ConvertingDir(repo, 2), "new")
	if got, err := changelogfile.Converting(repo); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("Converting = %v, %v; want [2]", got, err)
	}
	// The moment between the two renames: the live directory moved away.
	if err := os.Rename(live, filepath.Join(repo, "changelog.previous")); err != nil {
		t.Fatal(err)
	}
	if err := changelogfile.SwapConverted(repo, 2); err != nil {
		t.Fatalf("finish the swap: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(live, "000000000000001.ndjson"))
	if err != nil || string(raw) != "new" {
		t.Fatalf("the live changelog holds %q (%v), want the converted lines", raw, err)
	}
	entries, err := os.ReadDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != changelogfile.ChangelogSubdir {
		t.Fatalf("the repository directory holds %v after the swap, want the changelog alone", entries)
	}
}

// What a crash leaves is settled by whether the table took the conversion,
// and the history is never left in neither place: a conversion the table did
// not take is discarded with the live changelog moved back, even when the
// boot made the live name again, empty, before the settle ran.
func TestSettlingAConversionKeepsOneHistory(t *testing.T) {
	t.Parallel()
	write := func(t *testing.T, dir, body string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "000000000000001.ndjson"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// midSwap is a directory a server left between the swap's two renames,
	// as the next boot finds it: the live name made again, empty.
	midSwap := func(t *testing.T) string {
		t.Helper()
		repo := t.TempDir()
		write(t, changelogfile.PreviousDir(repo), "old")
		write(t, changelogfile.ConvertingDir(repo, 2), "new")
		if err := os.MkdirAll(changelogfile.ChangelogDir(repo), 0o700); err != nil {
			t.Fatal(err)
		}
		return repo
	}
	holds := func(t *testing.T, repo, want string) {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(changelogfile.ChangelogDir(repo), "000000000000001.ndjson"))
		if err != nil || string(raw) != want {
			t.Fatalf("the live changelog holds %q (%v), want %q", raw, err, want)
		}
		entries, err := os.ReadDir(repo)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != changelogfile.ChangelogSubdir {
			t.Fatalf("the repository directory holds %v after the settle, want the changelog alone", entries)
		}
	}
	for _, c := range []struct {
		name  string
		repo  func(t *testing.T) string
		taken bool
		want  string
	}{
		{"between the renames, taken", midSwap, true, "new"},
		{"between the renames, restored where no table took it", midSwap, false, "old"},
		{"before the swap, not taken", func(t *testing.T) string {
			repo := t.TempDir()
			write(t, changelogfile.ChangelogDir(repo), "old")
			write(t, changelogfile.ConvertingDir(repo, 2), "new")
			return repo
		}, false, "old"},
		{"before the swap, taken", func(t *testing.T) string {
			repo := t.TempDir()
			write(t, changelogfile.ChangelogDir(repo), "old")
			write(t, changelogfile.ConvertingDir(repo, 2), "new")
			return repo
		}, true, "new"},
		{"after the swap, before the previous one went", func(t *testing.T) string {
			repo := t.TempDir()
			write(t, changelogfile.ChangelogDir(repo), "new")
			write(t, changelogfile.PreviousDir(repo), "old")
			return repo
		}, true, "new"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			repo := c.repo(t)
			if err := changelogfile.SettleConversion(repo, func(int) bool { return c.taken }); err != nil {
				t.Fatalf("settle: %v", err)
			}
			holds(t, repo, c.want)
		})
	}
}
