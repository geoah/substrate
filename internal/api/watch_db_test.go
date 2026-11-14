package api

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/kinds"
)

func TestWatchRecordsStreamsBookmarkThenChanges(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	srv := httptest.NewServer(env.h)
	defer srv.Close()

	br, stop := startWatch(t, srv, peopleRecords+"&watch=1", env.token)
	defer stop()

	first := readLine(t, br)
	if _, ok := first["bookmark"]; !ok {
		t.Fatalf("first line = %v, want a bookmark", first)
	}

	env.put(t, personKind, "c1", map[string]any{"name": "Ada"})
	// A change of another kind must not appear on this stream.
	env.put(t, "samples.substrate.reamde.dev/tasks/task", "m1", map[string]any{"name": "Call back"})
	env.put(t, personKind, "c2", map[string]any{"name": "Grace"})

	got := readLine(t, br)
	if got["recordId"] != "c1" {
		t.Fatalf("first change = %v", got)
	}
	got = readLine(t, br)
	if got["recordId"] != "c2" {
		t.Fatalf("second change = %v, want the task change filtered out", got)
	}
}

func TestWatchResumesFromCursor(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	head := seedPeople(t, env.ds, 3)
	generation := changesHead(t, env).Generation
	from := head - 2

	br, stop := startWatch(t, srv, fmt.Sprintf("/api/v1/changes?watch=1&from=%d&generation=%s", from, generation), env.token)
	defer stop()

	if bm := readLine(t, br); bm["bookmark"] != float64(from) || bm["generation"] != generation {
		t.Fatalf("bookmark = %v, want the supplied cursor under the repository's generation", bm)
	}
	if got := readLine(t, br); got["seq"] != float64(from+1) || got["recordId"] != "c2" {
		t.Fatalf("first replayed change = %v, want c2 at seq %d", got, from+1)
	}
	if got := readLine(t, br); got["seq"] != float64(from+2) || got["recordId"] != "c3" {
		t.Fatalf("second replayed change = %v, want c3 at seq %d", got, from+2)
	}
}

func TestChangesWithoutWatchIsASinglePage(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	env.put(t, personKind, "c1", map[string]any{"name": "Ada"})

	rec := env.do(t, http.MethodGet, "/api/v1/changes?kinds="+personKind, env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := rec.Header().Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("content-type = %q", got)
	}
	lines := 0
	sc := bufio.NewScanner(rec.Body)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			lines++
		}
	}
	if lines != 2 {
		t.Fatalf("got %d ndjson lines, want bookmark + 1 change", lines)
	}
}

func TestBookmarkCarriesTheGeneration(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	head := seedPeople(t, env.ds, 2)
	generation := changesHead(t, env).Generation
	if generation == "" {
		t.Fatal("the repository's changelog names no generation")
	}

	for name, prefix := range resumeEntryPoints {
		if name == "feed page" {
			rec := env.do(t, http.MethodGet, prefix+"from=0", env.token, nil)
			wantStatus(t, rec, http.StatusOK)
			bm := readLine(t, bufio.NewReader(rec.Body))
			if bm["bookmark"] != float64(0) || bm["generation"] != generation {
				t.Fatalf("%s: bookmark = %v, want seq 0 under %q", name, bm, generation)
			}
			continue
		}
		br, stop := startWatch(t, srv, prefix[:len(prefix)-1], env.token)
		bm := readLine(t, br)
		stop()
		if bm["bookmark"] != float64(head) || bm["generation"] != generation {
			t.Fatalf("%s: bookmark = %v, want the head %d under %q", name, bm, head, generation)
		}
	}
}

// A cursor is a seq under a generation. Above the head, under another
// generation, or with no generation at all, it is refused with the head to
// re-list from, on every door alike, before a byte of stream is written.
func TestResumeCursorIsHeldToTheHead(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	head := seedPeople(t, env.ds, 3)
	generation := changesHead(t, env).Generation

	for name, prefix := range resumeEntryPoints {
		for _, query := range []string{
			fmt.Sprintf("from=%d&generation=%s", head+1, generation), // above the head
			fmt.Sprintf("from=%d&generation=some-other-history", head-1),
			fmt.Sprintf("from=%d", head-1), // no generation: unverifiable
			"from=0&generation=some-other-history",
		} {
			status, got := resumeRefusal(t, srv, prefix+query, env.token)
			if status != http.StatusGone {
				t.Fatalf("%s %s: status %d, want 410", name, query, status)
			}
			wantReset(t, status, got, head, generation)
		}
	}
}

// Seq 0 is the start of every history and names no entry, so it needs no
// generation: `from=0` reads everything, on the page and on the stream.
func TestFromZeroNeedsNoGeneration(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	head := seedPeople(t, env.ds, 2)
	if head >= changeBatch {
		t.Fatalf("head %d: a fresh repository's history no longer fits one page, so this test reads only its first", head)
	}

	rec := env.do(t, http.MethodGet, "/api/v1/changes?from=0", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	var seqs []int64
	sc := bufio.NewScanner(rec.Body)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var row struct {
			Seq *int64 `json:"seq"`
		}
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatalf("decode %q: %v", sc.Text(), err)
		}
		if row.Seq != nil {
			seqs = append(seqs, *row.Seq)
		}
	}
	// Every entry of the history, from the first: the repository's creation,
	// its samples, its token and the two people.
	if int64(len(seqs)) != head || seqs[0] != 1 || seqs[len(seqs)-1] != head {
		t.Fatalf("from=0 read %d rows (%v...), want every seq 1..%d", len(seqs), seqs[:min(len(seqs), 3)], head)
	}
	wantNotRefused(t, env.testEnv, peopleRecords+"&watch=1&from=0", env.token)
}

// The ticket's case: a client saves a cursor into a history, and an OLDER
// copy of the repository's archive (the owner's recovery export, taken
// before the writes the cursor points into) is imported over an emptied
// database. The resume is refused with the new head and generation; the
// client re-lists there and from then on misses nothing, including the writes
// that re-fill the seqs the old cursor named.
func TestRestoredHistoryResetsTheCursorAndLosesNothing(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	ctx := context.Background()
	copyHead := seedPeople(t, env.ds, 3)

	// The older copy: the archive the export route streams, as of copyHead.
	rec := env.do(t, http.MethodGet, "/api/v1/export", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	archive := rec.Body.Bytes()

	for i := range 4 {
		env.put(t, personKind, "later"+strconv.Itoa(i), map[string]any{"name": "Written after the copy"})
	}
	before := changesHead(t, env)
	saved, savedGeneration := copyHead+2, before.Generation
	if before.Seq != copyHead+4 {
		t.Fatalf("head = %d, want the copy's %d and four writes", before.Seq, copyHead)
	}

	// The copy over an emptied database: a second server boots with it as
	// its archive and restores the repository from it, as a restore does.
	root := t.TempDir()
	watchExtract(t, filepath.Join(root, "archive"), archive)
	svc, err := engine.Open(ctx, dbTemplate.Clone(t),
		engine.WithKindsFS(kinds.Seed()),
		engine.WithDataRoot(root),
		engine.WithCredentialKey(dbCredentialKey),
		engine.WithTestTOTPClock(env.clock.now),
		engine.WithLLMProviderSeed(false),
		engine.WithUnitSeed(false),
		engine.WithRecordSeed(false))
	if err != nil {
		t.Fatalf("boot on the restored archive: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	restored, err := svc.Dataset(ctx, env.repository)
	if err != nil {
		t.Fatalf("open the restored repository: %v", err)
	}
	srv := httptest.NewServer(New(Config{Service: svc, Now: env.clock.now, InviteCode: testInviteCode}))
	defer srv.Close()
	after, err := restored.Head(ctx)
	if err != nil {
		t.Fatalf("head after the restore: %v", err)
	}
	if after.Seq != copyHead || after.Generation == savedGeneration {
		t.Fatalf("restored head = %+v, want seq %d under a generation other than %q", after, copyHead, savedGeneration)
	}

	status, reset := resumeRefusal(t, srv, fmt.Sprintf("/api/v1/changes?watch=1&from=%d&generation=%s", saved, savedGeneration), env.token)
	wantReset(t, status, reset, copyHead, after.Generation)

	br, stop := startWatch(t, srv, fmt.Sprintf("/api/v1/changes?watch=1&from=%d&generation=%s", *reset.Head, reset.Generation), env.token)
	defer stop()
	if bm := readLine(t, br); bm["bookmark"] != float64(copyHead) {
		t.Fatalf("bookmark = %v, want the restored head %d", bm, copyHead)
	}
	for i := range 4 {
		if _, err := restored.Put(ctx, substrate.ActorSystem, substrate.PutInput{
			Kind: personKind, ID: "r" + strconv.Itoa(i+1), Properties: map[string]any{"name": "A replacement"},
		}); err != nil {
			t.Fatalf("replacement write %d: %v", i+1, err)
		}
	}
	for i := int64(1); i <= 4; i++ {
		if got := readLine(t, br); got["seq"] != float64(copyHead+i) || got["recordId"] != "r"+strconv.FormatInt(i, 10) {
			t.Fatalf("replacement write = %v, want r%d at seq %d", got, i, copyHead+i)
		}
	}
	// The old cursor stays refused: nothing about the history growing past
	// it makes a cursor from another generation resumable.
	status, reset = resumeRefusal(t, srv, fmt.Sprintf("/api/v1/changes?from=%d&generation=%s", saved, savedGeneration), env.token)
	wantReset(t, status, reset, copyHead+4, after.Generation)
}

// watchExtract unpacks an export archive under root, which it is laid out
// as, holding every entry to repositories/.
func watchExtract(t *testing.T, root string, archive []byte) {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(archive))
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Fatalf("read the archive: %v", err)
		}
		if !filepath.IsLocal(h.Name) || !strings.HasPrefix(h.Name, changelogfile.RepositoriesDir+"/") {
			t.Fatalf("the archive names %s outside repositories/", h.Name)
		}
		target := filepath.Join(root, filepath.FromSlash(h.Name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				t.Fatal(err)
			}
		case tar.TypeReg:
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, data, 0o600); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("the archive carries %s as type %v", h.Name, h.Typeflag)
		}
	}
}

// A `before` continuation is a position in one history too: a client that
// fetched a page, then had an older directory restored under it, must not
// walk on through the replacement's rows. `before=0` is the head of whatever
// history is there and needs no generation.
func TestHistoryContinuationIsHeldToTheGeneration(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	head := seedPeople(t, env.ds, 3)
	generation := changesHead(t, env).Generation

	for _, query := range []string{
		fmt.Sprintf("first=2&before=%d", head),
		fmt.Sprintf("first=2&before=%d&generation=some-other-history", head),
		"first=2&generation=some-other-history",
		fmt.Sprintf("first=2&before=%d&generation=%s", head+1, generation), // above the head
		"first=2&before=-1&generation=" + generation,                       // below every horizon
	} {
		status, got := resumeRefusal(t, srv, "/api/v1/changes?"+query, env.token)
		if status != http.StatusGone {
			t.Fatalf("%s: status %d, want 410", query, status)
		}
		wantReset(t, status, got, head, generation)
	}
	for _, query := range []string{
		"first=2",
		"first=2&before=0",
		fmt.Sprintf("first=2&before=%d&generation=%s", head, generation),
	} {
		rec := env.do(t, http.MethodGet, "/api/v1/changes?"+query, env.token, nil)
		wantStatus(t, rec, http.StatusOK)
	}
}

// The list envelope and the history page carry the generation beside the
// head, so a client can hand either straight to `watch?from=&generation=`.
func TestListAndHistoryCarryTheHandoff(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	head := seedPeople(t, env.ds, 2)
	generation := changesHead(t, env).Generation

	rec := env.do(t, http.MethodGet, peopleRecords, env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[map[string]any](t, rec)
	if page["head"] != float64(head) || page["generation"] != generation {
		t.Fatalf("list envelope head/generation = %v/%v, want %d/%q", page["head"], page["generation"], head, generation)
	}

	rec = env.do(t, http.MethodGet, "/api/v1/changes?first=1", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	history := decodeJSON[map[string]any](t, rec)
	if history["head"] != float64(head) || history["generation"] != generation {
		t.Fatalf("history page head/generation = %v/%v, want %d/%q", history["head"], history["generation"], head, generation)
	}
}
