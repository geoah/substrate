package engine_test

// The long walks say where they are (issue 745): verify's two passes, a
// rebuild and the boot import each log a progress line, at info and marked
// `progress=true`, naming the segment, the seq and the bytes read so far.
// The interval is zero here, so a short history prints what a two-hour one
// prints every thirty seconds.

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

func TestVerifyRebuildAndImportSayWhereTheyAre(t *testing.T) {
	t.Parallel()
	var logs lockedLog
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	// One-byte segments: every transaction rotates, so the walks cross
	// segment boundaries and the segment named in the lines moves.
	svc, ds, _ := newDatasetWithDSN(t,
		engine.WithChangelogSegmentBytes(1),
		engine.WithTestProgressEvery(0),
		engine.WithLogger(logger))
	for _, name := range []string{"one", "two", "three"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind:       "samples.substrate.reamde.dev/tasks/task",
			Properties: map[string]any{"name": name},
		})
	}
	head := maxSeq(t, ds)
	id := repositoryIDOf(t, ds)
	if report := mustVerify(t, svc, testdb.Repository(t)); !report.OK {
		t.Fatalf("the repository does not verify: %+v", report)
	}
	if _, err := svc.(rebuilder).RebuildRepository(context.Background(), testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	svc2, err := engine.OpenForTest(t, context.Background(), engine.MigratedDSN(t),
		engine.WithKindsDir(engine.SeedKindsDir),
		engine.WithDataRoot(root2),
		engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithTestProgressEvery(0),
		engine.WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	_ = svc2.Close()

	out := logs.String()
	for _, msg := range []string{
		"substrate: verifying the changelog files",
		"substrate: verifying the changelog table against the files",
		"substrate: replaying the changelog into the fold",
		"substrate: importing the changelog rows",
	} {
		lines := linesWith(out, `msg="`+msg+`"`)
		if len(lines) == 0 {
			t.Errorf("no progress line for %q\n%s", msg, out)
			continue
		}
		last := lines[len(lines)-1]
		for _, want := range []string{"repository=" + id, "segment=", "seq=" + strconv.FormatInt(head, 10), "bytes=", "progress=true"} {
			if !strings.Contains(last, want) {
				t.Errorf("the last %q line lacks %s: %s", msg, want, last)
			}
		}
		// The file walk ticks per entry, so its lines cross segments (every
		// transaction here is one); the paged passes tick per page of 500,
		// which this history fits in, so their one line is at the head.
		if first := lines[0]; msg == "substrate: verifying the changelog files" &&
			(segmentOf(first) == "" || segmentOf(first) >= segmentOf(last)) {
			t.Errorf("%q did not move across segments: first %s, last %s", msg, first, last)
		}
	}
}

func linesWith(out, substr string) []string {
	var found []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, substr) {
			found = append(found, line)
		}
	}
	return found
}

// segmentOf is the `segment=` value of one text-handler line.
func segmentOf(line string) string {
	_, after, ok := strings.Cut(line, "segment=")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(after, " ")
	return name
}
