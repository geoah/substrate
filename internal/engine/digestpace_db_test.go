package engine_test

// The background digest of a changelog (segmentdigest.go) yields to the work
// the server is for. On a four-core arm64 box a 27 GB history took 2.3
// cores for ten minutes after the open, a function that takes 5 s alone took
// 45 s beside it and met its deadline, and a scheduled sync parked
// (2026-10-06). The digest now hashes at a capped rate and pauses while any
// function body or agent loop runs. The bound these tests hold is the one
// decision 0146 records.

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	digestPackage  = "digest.test.dev/pace"
	digestNoteType = digestPackage + "/note"
	digestNapFn    = digestPackage + "/nap"
	// digestNoteBytes is the prose one note carries, and digestHistoryMB the
	// history the fixture writes unless SUBSTRATE_DIGEST_TEST_MB says more:
	// enough segments for a digest to be in progress while a function runs.
	digestNoteBytes = 512 << 10
	digestHistoryMB = 16
	// digestTestRate is the cap the tests open under: 2 MiB per second, so
	// a 16 MiB history is eight seconds of digest.
	digestTestRate = 2 << 20
)

// digestConnector is a kind that holds prose, for bulk, and a function that
// sleeps for `args.seconds` and returns.
func digestConnector() enginetest.Manifest {
	return enginetest.Manifest{Name: "pace", Authority: digestPackage, Manifests: []map[string]any{
		vocabulary.PackageManifest(digestPackage, 0),
		vocabulary.ActorManifest(digestPackage, vocabulary.PackageActor(digestPackage)),
		vocabulary.KindManifest(digestPackage, map[string]any{"singular": "note"}, map[string]any{
			"properties": map[string]any{"prose": map[string]any{"type": "text", "fts": false}},
		}),
		vocabulary.FunctionManifest(digestPackage, "nap", map[string]any{
			"description": "sleeps for args.seconds, then returns",
			"runtime":     vocabulary.RuntimePython,
			"source": "import time\n" +
				"def main(input, host):\n" +
				"    time.sleep(float((input.get(\"args\") or {}).get(\"seconds\") or 0))\n" +
				"    return {}\n",
			"timeout":     "PT30S",
			"permissions": map[string]any{"writes": []any{digestNoteType}},
		}),
	}}
}

// paceSamples is what the digest pace hook records: when each read ended
// and how many bytes the digest had read by then.
type paceSamples struct {
	mu      sync.Mutex
	samples []paceSample
}

type paceSample struct {
	at    time.Time
	bytes int64
}

func (p *paceSamples) hook(_ string, bytes int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.samples = append(p.samples, paceSample{at: time.Now(), bytes: bytes})
}

// readBetween is how many bytes the digest read between two instants: the
// bytes of the last sample before `to` less the bytes of the last sample
// before `from`.
func (p *paceSamples) readBetween(from, to time.Time) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	var before, until int64
	for _, s := range p.samples {
		if s.at.Before(from) {
			before = s.bytes
		}
		if s.at.Before(to) {
			until = s.bytes
		}
	}
	return until - before
}

func (p *paceSamples) last() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.samples) == 0 {
		return 0
	}
	return p.samples[len(p.samples)-1].bytes
}

// digestHistory writes a repository whose changelog holds several finished
// segments of prose and the nap function, closes the service and answers
// what a reopen needs plus the bytes the reopen's digest reads.
func digestHistory(t *testing.T) (dsn, root, repo string, unread int64) {
	t.Helper()
	ctx := context.Background()
	dsn, root, repo = engine.MigratedDSN(t), t.TempDir(), testdb.Repository(t)
	svc, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(root), engine.WithChangelogSegmentBytes(4<<20))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}
	ds, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := enginetest.Install(ctx, ds, owner, digestConnector()); err != nil {
		t.Fatalf("install the connector: %v", err)
	}
	mb := digestHistoryMB
	if v, err := strconv.Atoi(os.Getenv("SUBSTRATE_DIGEST_TEST_MB")); err == nil && v > 0 {
		mb = v
	}
	prose := strings.Repeat("the quick brown fox jumps over the lazy dog ", digestNoteBytes/44)
	for i := range mb << 20 / digestNoteBytes {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: digestNoteType, ID: "n" + strconv.Itoa(i), Properties: map[string]any{"prose": prose},
		})
	}
	dir := repoDirOf(t, svc, ds)
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	// What the reopen's digest reads is what an open that trusts the
	// sidecars leaves unread: every finished segment but the last listed,
	// which the open reads whole, or every one when an active segment
	// follows them.
	log, err := changelogfile.OpenWith(changelogfile.ChangelogDir(dir), changelogfile.OpenOptions{TrustSidecars: true, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if log.Unread() < 2 {
		t.Fatalf("%d segments owed to the digest; the fixture wants several", log.Unread())
	}
	return dsn, root, repo, log.UnreadBytes()
}

// nap calls the sleeping function and answers how long the call took.
func nap(t *testing.T, ds substrate.Dataset, seconds float64) time.Duration {
	t.Helper()
	began := time.Now()
	if _, _, err := ds.CallFunction(context.Background(), substrate.ActorAPI, digestNapFn, map[string]any{"seconds": seconds}); err != nil {
		t.Fatalf("nap: %v", err)
	}
	return time.Since(began)
}

// medianNap is the median of n calls of the function with nothing to do.
func medianNap(t *testing.T, ds substrate.Dataset, n int) time.Duration {
	t.Helper()
	took := make([]time.Duration, 0, n)
	for range n {
		took = append(took, nap(t, ds, 0))
	}
	slices.Sort(took)
	return took[n/2]
}

func waitDigest(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	select {
	case <-engine.SegmentDigestDone(ds):
	case <-time.After(2 * time.Minute):
		t.Fatal("the digest did not return within two minutes")
	}
}

// While a function body runs, the digest reads no more than the megabyte it
// had in hand when the body started: a cap alone would let it hash on
// beside the body for the body's whole run.
func TestTheDigestPausesWhileAnInvocationRuns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, repo, unread := digestHistory(t)
	samples := &paceSamples{}
	var logs lockedLog
	svc, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(root),
		engine.WithChangelogSegmentBytes(4<<20), engine.WithDigestBytesPerSecond(digestTestRate),
		engine.WithTestDigestPaceHook(samples.hook),
		engine.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	// The body's first call prepares its process; the digest pauses for
	// that too, so the history is still mostly unread when the timed call
	// begins.
	nap(t, ds, 0)
	if got := samples.last(); got >= unread {
		t.Fatalf("the digest read all %d bytes before the timed call; raise the history or lower the cap", unread)
	}
	const sleep = 3.0
	began := time.Now()
	took := nap(t, ds, sleep)
	ended := time.Now()
	if took < time.Duration(sleep*float64(time.Second)) {
		t.Fatalf("the nap of %gs returned after %s", sleep, took)
	}
	// One read may be in flight when the body starts and one may end as
	// it returns; an unpaused digest at the cap reads three times that.
	const bound = 2 << 20
	if read := samples.readBetween(began, ended); read > bound {
		t.Fatalf("the digest read %d bytes while a %gs body ran, want at most %d: it did not pause", read, sleep, bound)
	}
	waitDigest(t, ds)
	out := logs.String()
	for _, want := range []string{
		`msg="substrate: digesting the finished changelog segments behind the open"`,
		"bytesPerSecond=" + strconv.Itoa(digestTestRate),
		`msg="substrate: every finished changelog segment matches its sidecar"`,
		"paused=",
		"took=",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the log lacks %s\n%s", want, out)
		}
	}
}

// A function invoked while the digest is in progress takes no longer than
// one invoked after it: the median of five stays within twice the quiet
// median plus 250 ms, the bound decision 0146 records. The digest's own
// progress is held to the cap meanwhile, and it finishes.
func TestAnInvocationIsNotSlowedByTheDigest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, repo, unread := digestHistory(t)
	samples := &paceSamples{}
	svc, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(root),
		engine.WithChangelogSegmentBytes(4<<20), engine.WithDigestBytesPerSecond(digestTestRate),
		engine.WithTestDigestPaceHook(samples.hook))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	nap(t, ds, 0)
	const calls = 5
	during := medianNap(t, ds, calls)
	if got := samples.last(); got >= unread {
		t.Fatalf("the digest read all %d bytes before the timed calls ended; raise the history or lower the cap", unread)
	}
	began := time.Now()
	waitDigest(t, ds)
	digest := time.Since(began)
	quiet := medianNap(t, ds, calls)
	bound := 2*quiet + 250*time.Millisecond
	t.Logf("median invocation: %s during the digest, %s after it (bound %s); the rest of the digest took %s",
		during.Round(time.Microsecond), quiet.Round(time.Microsecond), bound.Round(time.Millisecond),
		digest.Round(time.Millisecond))
	if during > bound {
		t.Fatalf("an invocation beside the digest took %s, more than %s (twice the quiet %s plus 250ms)",
			during.Round(time.Millisecond), bound.Round(time.Millisecond), quiet.Round(time.Millisecond))
	}
	if got := samples.last(); got != unread {
		t.Fatalf("the digest read %d bytes, want the %d it owed", got, unread)
	}
}
