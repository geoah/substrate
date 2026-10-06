package engine_test

// The boot check digested every finished segment of a repository's
// changelog, and the repository's first open digested every one of them
// again, while each request for the repository waited on it: 6.5 minutes
// twice on a 16 GB history (issues 761 and 825). Neither digests one now:
// both read each finished segment's first and last lines and take the rest
// on its sidecar's word (the last segment listed excepted), and the server
// digests each of the rest once, after the open, saying where it is while it
// reads.

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
	"time"

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

func TestTheServerDigestsEachSegmentOnceAfterTheOpen(t *testing.T) {
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
			// The last segment listed has no next one to hold its end to, so
			// the boot digests it whole; every other one it reads at its
			// first and last lines.
			if atBoot := counter.snapshot(); len(atBoot) > 1 {
				t.Fatalf("the boot check digested %v; it digests only the last segment listed", atBoot)
			}
			if _, err := svc2.Dataset(context.Background(), id); err != nil {
				t.Fatalf("the first open: %v", err)
			}
			// The digest runs behind the open: wait for every finished
			// segment, then hold each to one read.
			deadline := time.Now().Add(30 * time.Second)
			for {
				total := counter.snapshot()
				done := true
				for _, name := range finished {
					if total[name] == 0 {
						done = false
					}
				}
				if done || time.Now().After(deadline) {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			deadline = time.Now().Add(30 * time.Second)
			for !strings.Contains(logs.String(), "every finished changelog segment matches its sidecar") && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			total := counter.snapshot()
			for _, name := range finished {
				if n := total[name]; n != 1 {
					t.Errorf("segment %s was digested %d times, want once, after the open", name, n)
				}
			}

			out := logs.String()
			for _, msg := range []string{
				"substrate: boot check: checking the changelog segments",
				"substrate: open: checking the changelog segments",
				"substrate: digesting the changelog segments the open did not read",
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
				// The background digest leaves out the segments that were
				// last when the boot and the open listed them: each digested
				// those whole.
				floor := len(finished) - 1
				if strings.Contains(msg, "did not read") {
					floor = len(finished) - 2
				}
				if n, err := strconv.Atoi(attrOf(last, "totalSegments")); err != nil || n < floor {
					t.Errorf("the last %q line counts %s segments, the directory has at least %d: %s", msg, attrOf(last, "totalSegments"), floor, last)
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
