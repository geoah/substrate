package changelogfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// A conversion rewrites a repository's changelog line by line into a newer
// changelog dialect (the engine's conversion chain, decision 0260). The
// converted lines are written into a directory beside the live one,
// `changelog.converting-<dialect>`, and take the live one's place only after
// the table holds the same lines: the live directory moves to
// `changelog.previous`, the converted one takes its name, and the previous
// one is removed. The engine decides whether the table took a conversion;
// this file is the directory half, and SettleConversion finishes what a crash
// left of it.
const (
	convertingPrefix = ChangelogSubdir + ".converting-"
	previousSubdir   = ChangelogSubdir + ".previous"
)

// ConvertingDir is the directory a conversion to dialect writes the converted
// changelog into, beside the live one.
func ConvertingDir(repoDir string, dialect int) string {
	return filepath.Join(repoDir, convertingPrefix+strconv.Itoa(dialect))
}

// PreviousDir is where the live changelog waits while a converted one takes
// its name.
func PreviousDir(repoDir string) string { return filepath.Join(repoDir, previousSubdir) }

// Converting lists the dialects of the conversion directories a repository
// directory holds, in order: none, unless a conversion was cut short.
func Converting(repoDir string) ([]int, error) {
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return nil, err
	}
	var out []int
	for _, e := range entries {
		rest, ok := strings.CutPrefix(e.Name(), convertingPrefix)
		if !ok || !e.IsDir() {
			continue
		}
		d, err := strconv.Atoi(rest)
		if err != nil || d < 1 {
			return nil, fmt.Errorf("changelogfile: %s is not a conversion directory this package writes", e.Name())
		}
		out = append(out, d)
	}
	sort.Ints(out)
	return out, nil
}

// DiscardConverting removes the conversion directory for dialect, and nothing
// else.
func DiscardConverting(repoDir string, dialect int) error {
	if err := os.RemoveAll(ConvertingDir(repoDir, dialect)); err != nil {
		return err
	}
	return syncDir(repoDir)
}

// holdsHistory reports whether dir lists a segment. A changelog directory
// the boot created empty, or none at all, holds none.
func holdsHistory(dir string) (bool, error) {
	list, err := Segments(dir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return len(list) > 0, err
}

// SwapConverted makes the conversion to dialect the repository's changelog:
// the live directory moves to `changelog.previous`, the converted one takes
// its name, and the previous one is removed, each step durable before the
// next. The caller has established that the table holds the converted lines.
// Called after a crash between the two renames, when the live name holds no
// history (it is gone, or the boot made it again empty), it finishes from the
// second rename.
func SwapConverted(repoDir string, dialect int) error {
	live := ChangelogDir(repoDir)
	converted := ConvertingDir(repoDir, dialect)
	previous := PreviousDir(repoDir)
	if _, err := os.Stat(converted); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("changelogfile: no conversion to dialect %d under %s to swap in", dialect, repoDir)
	} else if err != nil {
		return err
	}
	held, err := holdsHistory(live)
	if err != nil {
		return err
	}
	if held {
		if _, err := os.Stat(previous); err == nil {
			return fmt.Errorf("changelogfile: %s holds a live changelog and a previous one; settle the previous one first", repoDir)
		}
		if err := os.Rename(live, previous); err != nil {
			return err
		}
	} else if err := os.RemoveAll(live); err != nil {
		return err
	}
	if err := syncDir(repoDir); err != nil {
		return err
	}
	if err := os.Rename(converted, live); err != nil {
		return err
	}
	if err := syncDir(repoDir); err != nil {
		return err
	}
	if err := os.RemoveAll(previous); err != nil {
		return err
	}
	return syncDir(repoDir)
}

// SettleConversion finishes what a conversion cut short left in repoDir,
// before anything reads the changelog. taken reports whether the table holds
// the conversion to a dialect. A conversion the table took is swapped in. One
// it did not take is discarded, and where the swap had already moved the live
// changelog to `changelog.previous` (a directory copied from a server that
// stopped between the renames, restored where no table took it), the previous
// changelog is moved back first, so the history is never left in neither
// place. A previous changelog beside a live one that holds history is the
// leftover of a finished swap, and goes.
func SettleConversion(repoDir string, taken func(dialect int) bool) error {
	dialects, err := Converting(repoDir)
	if err != nil {
		return err
	}
	for _, d := range dialects {
		if taken(d) {
			if err := SwapConverted(repoDir, d); err != nil {
				return err
			}
			continue
		}
		if err := restorePrevious(repoDir); err != nil {
			return err
		}
		if err := DiscardConverting(repoDir, d); err != nil {
			return err
		}
	}
	if err := restorePrevious(repoDir); err != nil {
		return err
	}
	if err := os.RemoveAll(PreviousDir(repoDir)); err != nil {
		return err
	}
	return syncDir(repoDir)
}

// restorePrevious moves `changelog.previous` back to the live name when the
// live name holds no history.
func restorePrevious(repoDir string) error {
	previous := PreviousDir(repoDir)
	if _, err := os.Stat(previous); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	live := ChangelogDir(repoDir)
	held, err := holdsHistory(live)
	if err != nil || held {
		return err
	}
	if err := os.RemoveAll(live); err != nil {
		return err
	}
	if err := os.Rename(previous, live); err != nil {
		return err
	}
	return syncDir(repoDir)
}
