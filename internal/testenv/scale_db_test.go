package testenv_test

// THE SCALE HARNESS (validation item 10 of the 1.0 review): it generates one
// repository's history of a size it is told, opens a number of watching
// connections on it, and times the operations whose cost grows with the
// history: an archive pass, an export, a rebuild, the conversion at a start
// and the import of the archive into an empty database. Compaction is timed
// where this binary has a pass to run.
//
// It runs only when SUBSTRATE_SCALE_BYTES names the bytes of history to
// generate, and it is a measurement, not a check: it fails only when an
// operation fails, and the numbers go to the log, and to the file
// SUBSTRATE_SCALE_REPORT names, one JSON object a line. What size a run is
// held to, on which host, and what counts as passing are not the harness's
// to say. Against the shared test container the cluster is on tmpfs with
// durability off, which proves the harness and times nothing a deployment
// would see; SUBSTRATE_TEST_DATABASE_URL points it at a real server.
//
//	SUBSTRATE_SCALE_BYTES        history to generate, in bytes of line payload
//	SUBSTRATE_SCALE_BLOB_BYTES   blob bytes beside it (0)
//	SUBSTRATE_SCALE_LINE_BYTES   the body each record carries (2048)
//	SUBSTRATE_SCALE_WRITERS      writers generating it at once (8)
//	SUBSTRATE_SCALE_WATCHERS     connections watching while it is timed (100)
//	SUBSTRATE_SCALE_REPORT       a file to append the report to

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/testenv"
	"github.com/geoah/substrate/internal/vocabulary"
)

const scaleAuthority = "scale.example.com"

// scaleDeletionsKept is the deletion period the harness's compaction pass
// runs with: a server keeps every deletion unless one is set, and the
// harness times the forgetting too.
const scaleDeletionsKept = 30 * 24 * time.Hour

func scaleVocabulary() string {
	ref := scaleAuthority + "/scale"
	return `
kind: substrate.reamde.dev/core/package
metadata:
  id: ` + ref + `
data:
  authority: ` + scaleAuthority + `
  package: scale
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ` + ref + `/note
data:
  authority: ` + scaleAuthority + `
  package: scale
  names:
    singular: note
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      required: true
      description: what the note is called
    body:
      type: text
      description: what the note says
    follows:
      type: reference
      kind: ` + ref + `/note
      description: the note it follows
`
}

// scaleEnv reads one setting, falling back to def.
func scaleEnv(t *testing.T, name string, def int64) int64 {
	t.Helper()
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v < 0 {
		t.Fatalf("%s=%q is not a size", name, raw)
	}
	return int64(v)
}

// scalePhase is one timed operation.
type scalePhase struct {
	Phase      string  `json:"phase"`
	Seconds    float64 `json:"seconds,omitempty"`
	WALBytes   int64   `json:"walBytes,omitempty"`
	TempBytes  int64   `json:"tempBytes,omitempty"`
	HeapPeakMB float64 `json:"heapPeakMB,omitempty"`
	Writes     int     `json:"writesBeside,omitempty"`
	WriteP50Ms float64 `json:"writeP50Ms,omitempty"`
	WriteP99Ms float64 `json:"writeP99Ms,omitempty"`
	WriteMaxMs float64 `json:"writeMaxMs,omitempty"`
	Watchers   int     `json:"watchers,omitempty"`
	Bytes      int64   `json:"bytes,omitempty"`
	Note       string  `json:"note,omitempty"`
}

// scaleWords is the vocabulary a body is drawn from: prose-sized words, so
// the search bands are what a person's notes make.
var scaleWords = strings.Fields(`the a of and to in that it is was for on as with by at from this which
or be are not have had but they were all one their there been has more when will would who what so
up out about into than them can only other new some could time these two may then first any like
now such our over even most made after also did many before must through back years where much your
way well down should because each just those people how too little state good very make world still
own see men work long get here between both life being under never day same another know while last
might us great old year off come since against go came right used take three himself few house use
during without again place around however home small found thought went say part once general high
upon school every does got united left number course war until always away something fact though
water less public put think almost hand enough far took head yet government system better set told
nothing night end why called didnt eyes find going look asked later knew point next program city`)

// scaleID is writer w's nth note.
func scaleID(w, n int) string {
	return vocabulary.DerivedMintedID("scale", strconv.Itoa(w), strconv.Itoa(n))
}

func scaleBody(n int) string {
	var b strings.Builder
	for b.Len() < n {
		i, _ := rand.Int(rand.Reader, big.NewInt(int64(len(scaleWords))))
		b.WriteString(scaleWords[i.Int64()])
		b.WriteByte(' ')
	}
	return b.String()
}

func TestScaleTimings(t *testing.T) {
	target := scaleEnv(t, "SUBSTRATE_SCALE_BYTES", 0)
	if target == 0 {
		t.Skip("set SUBSTRATE_SCALE_BYTES to the bytes of history to generate and time")
	}
	blobTarget := scaleEnv(t, "SUBSTRATE_SCALE_BLOB_BYTES", 0)
	lineBytes := int(scaleEnv(t, "SUBSTRATE_SCALE_LINE_BYTES", 2048))
	writers := int(scaleEnv(t, "SUBSTRATE_SCALE_WRITERS", 8))
	watchers := int(scaleEnv(t, "SUBSTRATE_SCALE_WATCHERS", 100))
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	root := t.TempDir()
	key := testenv.MintCredentialKey()
	start := func(opts ...testenv.Option) *testenv.Env {
		return testenv.Start(t, append([]testenv.Option{
			testenv.WithDSN(dsn), testenv.WithDataRoot(root), testenv.WithCredentialKey(key),
			testenv.WithEngineOptions(engine.WithArchiveInterval(24 * time.Hour)),
		}, opts...)...)
	}
	e := start(testenv.WithRepository(scaleAuthority))
	// The registered owner's token, for a server the harness starts again.
	token := e.Token
	e.ApplyVocabularyYAML(scaleVocabulary())
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var report []scalePhase
	note := func(p scalePhase) {
		t.Helper()
		raw, _ := json.Marshal(p)
		t.Logf("scale: %s", raw)
		report = append(report, p)
	}

	// The history: notes created, rewritten and now and then deleted, each
	// following another, by writers at once, until the lines carry target
	// bytes; then blobs to blobTarget.
	began := time.Now()
	ds, err := e.Service.Dataset(ctx, scaleAuthority)
	if err != nil {
		t.Fatal(err)
	}
	kind := scaleAuthority + "/scale/note"
	var written, made atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for w := range writers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for n := 0; written.Load() < target; n++ {
				id := scaleID(w, n)
				props := map[string]any{"name": id, "body": scaleBody(lineBytes)}
				if n > 0 {
					props["follows"] = map[string]any{"ref": kind + "/" + scaleID(w, n-1)}
				}
				if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: kind, ID: id, Properties: props}); err != nil {
					errs <- fmt.Errorf("put %s: %w", id, err)
					return
				}
				written.Add(int64(lineBytes))
				made.Add(1)
				switch {
				case n%7 == 3:
					if _, err := ds.Patch(ctx, substrate.ActorPerson, kind, id, substrate.PatchInput{Properties: map[string]any{"body": scaleBody(lineBytes)}}); err != nil {
						errs <- fmt.Errorf("patch %s: %w", id, err)
						return
					}
					written.Add(int64(lineBytes))
				case n%31 == 5:
					if _, err := ds.Delete(ctx, substrate.ActorPerson, kind, id, substrate.DeleteInput{}); err != nil {
						errs <- fmt.Errorf("delete %s: %w", id, err)
						return
					}
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var blobs int64
	for blobs < blobTarget {
		data := []byte(scaleBody(1 << 20))
		if _, err := ds.PutBlob(ctx, substrate.ActorPerson, substrate.BlobUpload{Name: fmt.Sprintf("blob-%d.txt", blobs), MediaType: "text/plain"}, data, ""); err != nil {
			t.Fatalf("blob: %v", err)
		}
		blobs += int64(len(data))
	}
	var id string
	var lines, lineBytesTotal, dbBytes int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM repositories WHERE authority = $1`, scaleAuthority).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(octet_length(payload::text)), 0), pg_database_size(current_database())
		FROM changelog WHERE repository = $1`, id).Scan(&lines, &lineBytesTotal, &dbBytes); err != nil {
		t.Fatal(err)
	}
	note(scalePhase{
		Phase: "generate", Seconds: time.Since(began).Seconds(), Bytes: lineBytesTotal,
		Note: fmt.Sprintf("%d lines, %d notes, %d blob bytes, database %d bytes", lines, made.Load(), blobs, dbBytes),
	})

	// How many watchers are open while a phase runs: all of them from their
	// opening to their end, none after.
	var openWatchers atomic.Int64

	// Each timed operation: the database's WAL and temp bytes, the process's
	// peak heap, and, while the server is up, a write every 100ms beside it
	// and the watchers held open.
	measure := func(phase string, live bool, fn func() (int64, error)) {
		t.Helper()
		lsn := func() string {
			var s string
			if err := db.QueryRowContext(ctx, `SELECT pg_current_wal_lsn()::text`).Scan(&s); err != nil {
				t.Fatal(err)
			}
			return s
		}
		temp := func() int64 {
			var n int64
			if err := db.QueryRowContext(ctx, `SELECT temp_bytes FROM pg_stat_database WHERE datname = current_database()`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		walFrom, tempFrom := lsn(), temp()
		stop := make(chan struct{})
		var heapPeak atomic.Uint64
		var sampler sync.WaitGroup
		sampler.Add(1)
		go func() {
			defer sampler.Done()
			tick := time.NewTicker(100 * time.Millisecond)
			defer tick.Stop()
			for {
				var ms runtime.MemStats
				runtime.ReadMemStats(&ms)
				if ms.HeapInuse > heapPeak.Load() {
					heapPeak.Store(ms.HeapInuse)
				}
				select {
				case <-stop:
					return
				case <-tick.C:
				}
			}
		}()
		var latencies []time.Duration
		var mu sync.Mutex
		if live {
			sampler.Add(1)
			go func() {
				defer sampler.Done()
				tick := time.NewTicker(100 * time.Millisecond)
				defer tick.Stop()
				for n := 0; ; n++ {
					select {
					case <-stop:
						return
					case <-tick.C:
					}
					at := time.Now()
					status, body, err := besideWrite(e, "/api/v1/"+kind+"/"+vocabulary.DerivedMintedID("scale", "beside", phase, strconv.Itoa(n)))
					if err != nil || status/100 != 2 {
						t.Errorf("a write beside %s: %d %s %v", phase, status, body, err)
						return
					}
					mu.Lock()
					latencies = append(latencies, time.Since(at))
					mu.Unlock()
				}
			}()
		}
		at := time.Now()
		n, err := fn()
		took := time.Since(at)
		close(stop)
		sampler.Wait()
		if err != nil {
			t.Fatalf("%s: %v", phase, err)
		}
		var wal int64
		if err := db.QueryRowContext(ctx, `SELECT pg_wal_lsn_diff(pg_current_wal_lsn(), $1::pg_lsn)::bigint`, walFrom).Scan(&wal); err != nil {
			t.Fatal(err)
		}
		p := scalePhase{
			Phase: phase, Seconds: took.Seconds(), WALBytes: wal, TempBytes: temp() - tempFrom,
			HeapPeakMB: float64(heapPeak.Load()) / (1 << 20), Bytes: n,
		}
		if live {
			p.Watchers = int(openWatchers.Load())
			slices.Sort(latencies)
			if k := len(latencies); k > 0 {
				p.Writes = k
				p.WriteP50Ms = float64(latencies[k/2].Microseconds()) / 1000
				p.WriteP99Ms = float64(latencies[k*99/100].Microseconds()) / 1000
				p.WriteMaxMs = float64(latencies[k-1].Microseconds()) / 1000
			}
		} else {
			p.Note = "the server is not up while it runs: no write beside it, no watcher"
		}
		note(p)
	}

	// The watchers, held open over every timed operation the server is up
	// for; each must still be reading when the last of them ends.
	filter, _ := json.Marshal(map[string]any{"kinds": []string{kind}})
	watchCtx, endWatch := context.WithCancel(ctx)
	// Ended and waited for however the test ends, a Fatal before the
	// operations among the ways, so no reader outlives it.
	var readers sync.WaitGroup
	t.Cleanup(func() {
		endWatch()
		readers.Wait()
	})
	var delivered atomic.Int64
	var opened sync.WaitGroup
	for range watchers {
		resp := e.Request(watchCtx, http.MethodGet, "/api/v1/records?watch=1&filter="+url.QueryEscape(string(filter)), nil, nil)
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			t.Fatalf("open a watch: %d", resp.StatusCode)
		}
		opened.Add(1)
		readers.Add(1)
		go func(resp *http.Response) {
			defer readers.Done()
			defer func() { _ = resp.Body.Close() }()
			first := true
			// A stream that ends before its first frame still counts as
			// opened, or the wait below never returns.
			defer func() {
				if first {
					opened.Done()
				}
			}()
			sc := bufio.NewScanner(resp.Body)
			sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
			for sc.Scan() {
				if first {
					opened.Done()
					first = false
					continue
				}
				delivered.Add(1)
			}
		}(resp)
	}
	opened.Wait()
	openWatchers.Store(int64(watchers))

	archivePass := ds.(interface{ ArchivePass(context.Context) error })
	measure("archive", true, func() (int64, error) { return 0, archivePass.ArchivePass(ctx) })
	measure("export", true, func() (int64, error) {
		x, err := ds.Export(ctx)
		if err != nil {
			return 0, err
		}
		return x.WriteTo(io.Discard)
	})
	measure("rebuild", true, func() (int64, error) {
		_, err := e.Service.(engine.Operator).RebuildRepository(ctx, id)
		return 0, err
	})
	endWatch()
	readers.Wait()
	openWatchers.Store(0)
	if delivered.Load() == 0 {
		t.Error("no watcher was delivered a write while the operations ran")
	}
	e.Stop()

	// The conversion at a start: the stamp one dialect below this binary's,
	// so the boot walks the chain's last step over every line, and the
	// import-progress marker set, so the fold is rebuilt from the history as
	// a conversion that rewrote lines rebuilds it, in two replays. The lines
	// are in this binary's shape already and none is rewritten, and a step
	// that rewrites none rebuilds nothing on its own (decision 0309): the
	// marker is what makes the phase time the replays, which are what grows
	// with the history.
	for _, q := range []string{
		`UPDATE changelog_dialect SET dialect = dialect - 1 WHERE repository = $1`,
		`INSERT INTO import_progress (repository, file_head) SELECT $1, max(seq) FROM changelog WHERE repository = $1`,
	} {
		if _, err := db.ExecContext(ctx, q, id); err != nil {
			t.Fatal(err)
		}
	}
	measure("conversion", false, func() (int64, error) {
		e = start(testenv.WithoutRegistration(), testenv.WithRepository(scaleAuthority))
		return 0, nil
	})
	e.Stop()

	// The import: the archive restored into a database that holds nothing of
	// it, as a restore or a move to another host does.
	restoredDSN := testdb.NewSchema(t)
	store, err := archive.NewDir(filepath.Join(root, "archive"))
	if err != nil {
		t.Fatal(err)
	}
	// Timed to the restored repository's first open; its server's stop, and
	// the archive pass that stop runs, are not the import's. Its compaction
	// passes run when the harness says, below.
	var restored *testenv.Env
	measure("import", false, func() (int64, error) {
		restored = testenv.Start(t, testenv.WithDSN(restoredDSN), testenv.WithDataRoot(t.TempDir()), testenv.WithCredentialKey(key),
			testenv.WithoutRegistration(), testenv.WithRepository(scaleAuthority),
			testenv.WithEngineOptions(engine.WithArchive(store), engine.WithArchiveInterval(24*time.Hour),
				engine.WithCompactionInterval(-1),
				engine.WithTestCompactionRetention(func(string) (bool, time.Duration) { return false, 0 }),
				engine.WithDeletionsKept(scaleDeletionsKept),
				engine.WithTestCompactionClock(func() time.Time { return time.Now().Add(scaleDeletionsKept + time.Hour) })))
		_, err := restored.Service.Dataset(ctx, scaleAuthority)
		return 0, err
	})

	// The compaction pass, on the restored repository: it came in from the
	// archive without the write path's index, as an upgraded or restored
	// deployment's history does, so its first passes derive the index before
	// they compact. Every kind keeps nothing past a record's latest state, the
	// operator's door's work below, so the two compare, and the pass's clock
	// stands past a deletion period the harness sets, so it times the
	// forgetting a deployment that sets SUBSTRATE_DELETIONS_KEPT runs. A
	// server runs one pass an interval, each bounded by its budget, so the
	// passes this takes, at the default interval, are how long a server's own
	// passes take to clear the history.
	restored.Token = token
	e = restored
	rds, err := restored.Service.Dataset(ctx, scaleAuthority)
	if err != nil {
		t.Fatal(err)
	}
	compactionPass := rds.(interface {
		CompactionPass(context.Context) (engine.CompactionPassReport, error)
	})
	type passRun struct {
		engine.CompactionPassReport
		wall time.Duration
	}
	var passes []passRun
	measure("compaction pass", true, func() (int64, error) {
		for {
			at := time.Now()
			r, err := compactionPass.CompactionPass(ctx)
			if err != nil {
				return 0, err
			}
			passes = append(passes, passRun{r, time.Since(at)})
			if r.Finished {
				return 0, nil
			}
			if r.Indexed && r.Records == 0 && r.Removed == 0 && r.Forgotten == 0 && r.Erased == 0 {
				return 0, fmt.Errorf("pass %d did nothing and did not finish", len(passes))
			}
		}
	})
	var indexing, compacting time.Duration
	var removed int
	for i, r := range passes {
		if r.Indexed {
			compacting += r.wall
		} else {
			indexing += r.wall
		}
		removed += r.Removed
		note(scalePhase{Phase: fmt.Sprintf("compaction pass %d", i+1), Seconds: r.wall.Seconds(), Note: fmt.Sprintf(
			"indexed %v, %d records compacted in %d kind(s), %d entries removed, %d deletions forgotten, finished %v",
			r.Indexed, r.Records, len(r.Kinds), r.Removed, r.Forgotten, r.Finished)})
	}
	perSecond := 0.0
	if compacting > 0 {
		perSecond = float64(removed) / compacting.Seconds()
	}
	note(scalePhase{Phase: "compaction passes at the default interval", Note: fmt.Sprintf(
		"%d pass(es), one each %s, so %s: %.1fs in passes that ended still deriving the index, %.1fs in passes that compacted, "+
			"%d entries removed, %.0f a second of their time",
		len(passes), engine.DefaultCompactionInterval, time.Duration(len(passes))*engine.DefaultCompactionInterval,
		indexing.Seconds(), compacting.Seconds(), removed, perSecond)})
	restored.Stop()

	// The compaction, last, because it rewrites the history every phase
	// above replays: every record's history up to the head folded into its
	// state entry, as the operator's door runs it (`substratectl repository
	// compact`) and the compaction pass will. The server is up and a write
	// goes beside it; the watchers are closed by then.
	e = start(testenv.WithoutRegistration(), testenv.WithRepository(scaleAuthority))
	e.Token = token
	measure("compaction", true, func() (int64, error) {
		_, err := e.Service.(interface {
			CompactRepository(context.Context, string, int64) (engine.CompactReport, error)
		}).CompactRepository(ctx, id, 0)
		return 0, err
	})
	e.Stop()

	if path := os.Getenv("SUBSTRATE_SCALE_REPORT"); path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()
		for _, p := range report {
			raw, _ := json.Marshal(p)
			if _, err := fmt.Fprintf(f, "%s\n", raw); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// besideWrite is one write of a note beside a timed operation. It has no
// timeout of its own: a repository rebuild holds the changelog's lock for
// its whole length and every write to the repository waits for it, and how
// long a write waited there is what the phase reports.
func besideWrite(e *testenv.Env, path string) (int, []byte, error) {
	raw, err := json.Marshal(map[string]any{"properties": map[string]any{"name": "beside"}})
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, e.URL+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, body, err
}
