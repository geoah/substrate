package engine_test

// The boot check digests every finished segment of a repository's changelog,
// and the repository's first open used to digest every one of them again
// while each request for the repository waited on it: 6.5 minutes twice on
// a 16 GB history (issue 761). The first open now takes the boot check's
// Log, so across the two each finished segment is read once, and both say
// where they are while they read.

import (
	"context"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// digestCounter counts, per finished segment, the digests the directory
// checks of one repository read.
type digestCounter struct {
	repository string
	mu         sync.Mutex
	counts     map[string]int
}

func (c *digestCounter) hook(repository, segment string) {
	if repository != c.repository {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[segment]++
}

func (c *digestCounter) snapshot() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return maps.Clone(c.counts)
}

// finishedSegments names the finished segments of a changelog directory.
func finishedSegments(t *testing.T, dir string) []string {
	t.Helper()
	segs, err := changelogfile.Segments(changelogfile.ChangelogDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range segs {
		if s.Finished {
			names = append(names, s.Name)
		}
	}
	return names
}

func TestTheFirstOpenDigestsNoSegmentTheBootCheckDid(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// damage is what happens to the directory while the server is down.
		damage func(t *testing.T, dir string)
	}{
		{name: "a clean restart", damage: func(*testing.T, string) {}},
		{
			// The last transaction's segment gone: the boot check catches the
			// file up from the table, and that segment is new to the Log it
			// opened before the catch-up.
			name: "a restart that catches the file up",
			damage: func(t *testing.T, dir string) {
				names := finishedSegments(t, dir)
				last := filepath.Join(changelogfile.ChangelogDir(dir), names[len(names)-1])
				for _, path := range []string{last, changelogfile.SidecarName(last)} {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// One-byte segments: every transaction finishes its segment, so
			// the directory holds only finished ones.
			svc, ds, dsn := newDatasetWithDSN(t, engine.WithChangelogSegmentBytes(1))
			for _, name := range []string{"one", "two", "three"} {
				mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": name}})
			}
			root := engine.DataRootOf(svc)
			dir := repoDirOf(t, svc, ds)
			id := repositoryIDOf(t, ds)
			_ = svc.Close()
			finished := finishedSegments(t, dir)
			if len(finished) < 4 {
				t.Fatalf("%d finished segments; the test wants several", len(finished))
			}
			tc.damage(t, dir)

			counter := &digestCounter{repository: id, counts: map[string]int{}}
			var logs lockedLog
			svc2 := reopenWith(t, dsn, root,
				engine.WithChangelogSegmentBytes(1),
				engine.WithTestProgressEvery(0),
				engine.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))),
				engine.WithTestDigestHook(counter.hook))
			atBoot := counter.snapshot()
			if len(atBoot) == 0 {
				t.Fatal("the boot check digested no segment; the test proves nothing")
			}
			if _, err := svc2.Dataset(context.Background(), id); err != nil {
				t.Fatalf("the first open: %v", err)
			}
			total := counter.snapshot()
			for _, name := range finished {
				if n := total[name]; n != 1 {
					t.Errorf("segment %s was digested %d times across the boot check (%d) and the first open, want once",
						name, n, atBoot[name])
				}
			}

			out := logs.String()
			for _, msg := range []string{
				"substrate: boot check: checking the changelog segments",
				"substrate: open: checking the changelog segments",
			} {
				lines := linesWith(out, `msg="`+msg+`"`)
				if len(lines) == 0 {
					t.Errorf("no progress line for %q\n%s", msg, out)
					continue
				}
				last := lines[len(lines)-1]
				for _, want := range []string{"repository=" + id, "segment=", "segments=", "totalSegments=", "bytes=", "totalBytes=", "progress=true"} {
					if !strings.Contains(last, want) {
						t.Errorf("the last %q line lacks %s: %s", msg, want, last)
					}
				}
				if got, want := attrOf(last, "segments"), attrOf(last, "totalSegments"); got == "" || got != want {
					t.Errorf("the last %q line is at segment %s of %s: %s", msg, got, want, last)
				}
				if got, want := attrOf(last, "bytes"), attrOf(last, "totalBytes"); got == "" || got != want {
					t.Errorf("the last %q line is at byte %s of %s: %s", msg, got, want, last)
				}
				if n, err := strconv.Atoi(attrOf(last, "totalSegments")); err != nil || n < len(finished)-1 {
					t.Errorf("the last %q line counts %s segments, the directory has at least %d: %s", msg, attrOf(last, "totalSegments"), len(finished)-1, last)
				}
			}
		})
	}
}

// attrOf is the value of one `key=value` attribute of a text-handler line.
func attrOf(line, key string) string {
	for _, field := range strings.Fields(line) {
		if v, ok := strings.CutPrefix(field, key+"="); ok {
			return v
		}
	}
	return ""
}
