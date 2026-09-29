package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A vocabulary batch is one request the server answers only when the whole
// batch is admitted, which on a large repository is minutes (issue 720).
// While it is in flight, apply prints what it sent and how long it has
// waited to stderr once per interval; stdout carries the summary alone.

// progressBatch is three declarations in two packages.
const progressBatch = `kind: substrate.reamde.dev/core/package
metadata:
  id: ada.example.com/people
data:
  authority: ada.example.com
  package: people
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ada.example.com/people/person
data:
  authority: ada.example.com
  package: people
  names:
    singular: person
---
kind: substrate.reamde.dev/core/package
metadata:
  id: ada.example.com/places
data:
  authority: ada.example.com
  package: places
  version: 1
`

// lockedBuffer is a stderr the progress printer and the test read at once.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// runProgress is h.run with the progress interval set, a stderr the caller
// keeps reading after the command returns, and a clock that moves 40 seconds
// on every read, so the elapsed time each line prints is fixed.
func (h *harness) runProgress(every time.Duration, errOut *lockedBuffer, args ...string) (string, error) {
	h.t.Helper()
	var out bytes.Buffer
	var reads atomic.Int64
	a := newApp("test")
	a.in, a.out, a.errOut = h.stdin, &out, errOut
	a.now = func() time.Time { return testNow.Add(time.Duration(reads.Add(1)) * 40 * time.Second) }
	a.configPath = h.configPath
	a.progressEvery = every
	root := a.rootCommand()
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

func (f *fakeSubstrate) holdVocabulary(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vocabularyDelay = d
}

func writeProgressBatch(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "batch.yaml")
	if err := os.WriteFile(file, []byte(progressBatch), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestApplyPrintsProgressToStderrWhileTheBatchRuns(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	file := writeProgressBatch(t)

	// The answer inside one interval: the summary, and nothing on stderr.
	wantStdout, quiet := h.mustRun("apply", "-f", file)
	if quiet != "" {
		t.Fatalf("a fast apply printed to stderr: %q", quiet)
	}
	if !strings.Contains(wantStdout, "applied") {
		t.Fatalf("the fast apply printed no summary: %q", wantStdout)
	}

	// The answer held for many intervals.
	h.fake.holdVocabulary(300 * time.Millisecond)
	var errOut lockedBuffer
	stdout, err := h.runProgress(10*time.Millisecond, &errOut, "apply", "-f", file)
	if err != nil {
		t.Fatalf("apply: %v\nstderr:\n%s", err, errOut.String())
	}
	if stdout != wantStdout {
		t.Fatalf("stdout moved while the batch was held:\n got %q\nwant %q", stdout, wantStdout)
	}
	stderr := errOut.String()
	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("stderr carries %d progress lines over 30 intervals, want at least 2:\n%s", len(lines), stderr)
	}
	for i, want := range []string{
		"applying 3 documents in 2 packages, 40s elapsed",
		"applying 3 documents in 2 packages, 1m20s elapsed",
	} {
		if lines[i] != want {
			t.Errorf("progress line %d = %q, want %q", i+1, lines[i], want)
		}
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "applying 3 documents in 2 packages, ") || !strings.HasSuffix(line, " elapsed") {
			t.Errorf("stderr carries a line that is not progress: %q", line)
		}
	}

	// The printer stopped with the answer: nothing lands after the command
	// returned, however long the caller keeps the stream open.
	time.Sleep(100 * time.Millisecond)
	if after := errOut.String(); after != stderr {
		t.Fatalf("progress printed after apply returned:\n%s", strings.TrimPrefix(after, stderr))
	}
}

// --allow-data-loss previews the batch first, and the preview is the same
// wait: its lines name the preview, then the apply's name the apply.
func TestApplyAllowDataLossPrintsProgressForThePreview(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	file := writeProgressBatch(t)
	wantStdout, _ := h.mustRun("apply", "-f", file, "--allow-data-loss")

	h.fake.holdVocabulary(200 * time.Millisecond)
	var errOut lockedBuffer
	stdout, err := h.runProgress(10*time.Millisecond, &errOut, "apply", "-f", file, "--allow-data-loss")
	if err != nil {
		t.Fatalf("apply --allow-data-loss: %v\nstderr:\n%s", err, errOut.String())
	}
	if stdout != wantStdout {
		t.Fatalf("stdout moved while the batch was held:\n got %q\nwant %q", stdout, wantStdout)
	}
	stderr := errOut.String()
	previewing := strings.Index(stderr, "previewing 3 documents in 2 packages, ")
	applying := strings.Index(stderr, "applying 3 documents in 2 packages, ")
	if previewing < 0 || applying < 0 || previewing > applying {
		t.Fatalf("stderr does not show the preview's wait, then the apply's:\n%s", stderr)
	}
}

func TestBatchShapeCountsDocumentsAndPackages(t *testing.T) {
	decl := func(authority, pkg string) map[string]any {
		return map[string]any{"data": map[string]any{"authority": authority, "package": pkg}}
	}
	for _, tc := range []struct {
		docs []map[string]any
		want string
	}{
		{[]map[string]any{decl("ada.example.com", "people")}, "1 document in 1 package"},
		{[]map[string]any{decl("ada.example.com", "people"), decl("ada.example.com", "people"), decl("ada.example.com", "places")}, "3 documents in 2 packages"},
		// An authority document names no package.
		{[]map[string]any{decl("ada.example.com", "")}, "1 document"},
	} {
		if got := batchShape(tc.docs); got != tc.want {
			t.Errorf("batchShape = %q, want %q", got, tc.want)
		}
	}
}
