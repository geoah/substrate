package engine_test

// The webhook door answers once the request is in the repository's spool
// (decision 0167): a sealed, fsynced file the drain records in the delivery
// ledger and deletes after its commit. A request answered and never drained
// survives a stop, a snapshot and an export, and is recorded once however
// many drains find its file. None of these tests is parallel:
// BlobUploadGrace and the spool's bounds are process-wide.

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// spoolDirOf is the repository's spool directory under root.
func spoolDirOf(t *testing.T, root, id string) string {
	t.Helper()
	dir, err := changelogfile.RepoDir(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return changelogfile.SpoolDir(dir)
}

// spooledFiles reads every file in the spool directory, whatever its name.
func spooledFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return out
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = data
	}
	return out
}

// admissions counts the delivery entries that recorded fid as an accepted
// request, read from the tamperer's seat.
func admissions(t *testing.T, dsn, fid string) int {
	t.Helper()
	var n int
	if err := rawDB(t, dsn).QueryRow(`
		SELECT count(*) FROM changelog WHERE op = 'delivery'
		  AND jsonb_path_exists(payload, '$.** ? (@.fireId == $fid && @.lastError == $pending)',
		      jsonb_build_object('fid', $1::text, 'pending', $2::text))`, fid, engine.WebhookPendingError).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// awaitSpoolEmpty waits for the drain to delete every spooled request.
func awaitSpoolEmpty(t *testing.T, ds substrate.Dataset, dir string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		entries, _, _ := engine.SpoolStatsOf(ds)
		if entries == 0 && len(spooledFiles(t, dir)) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the spool still holds %d requests: %v", entries, spooledFiles(t, dir))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// noSpoolBeforeTheGrace sets the blob upload grace to zero for one test, so
// a sweep would collect a blob nothing holds.
func noSpoolBeforeTheGrace(t *testing.T) {
	prev := engine.BlobUploadGrace
	engine.BlobUploadGrace = 0
	t.Cleanup(func() { engine.BlobUploadGrace = prev })
}

// The process stops right after the 202: the request is in the spool and in
// no table, its file holds none of the request's bytes in the clear, its
// metrics count it, and the first dispatcher pass of the next process drains
// it and fires it once under the fire id the sender was given.
func TestASpooledWebhookSurvivesAStopBeforeItsDrain(t *testing.T) {
	noSpoolBeforeTheGrace(t)
	for name, shape := range heldRequests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			svc, ds, dsn, root, _ := newHeldHookDataset(t, true)
			id := ds.Repository().ID
			fid, err := engine.ReceiveWebhookSpooled(ctx, svc, testdb.Repository(t), "hook-held", "", shape.req)
			if err != nil {
				t.Fatalf("receive: %v", err)
			}
			if failures, err := ds.TriggerFailures(ctx, "hook-held"); err != nil || len(failures) != 0 {
				t.Fatalf("a spooled request is in the ledger already: %+v, %v", failures, err)
			}
			if n := admissions(t, dsn, fid); n != 0 {
				t.Fatalf("a spooled request has %d delivery entries", n)
			}
			dir := spoolDirOf(t, root, id)
			files := spooledFiles(t, dir)
			if len(files) != 1 {
				t.Fatalf("the spool holds %d files, want 1", len(files))
			}
			for _, data := range files {
				for _, absent := range append(shape.absent, "x-github-event") {
					if bytes.Contains(data, []byte(absent)) {
						t.Fatalf("the spooled file carries %q in the clear", absent)
					}
				}
			}
			entries, size, oldest := engine.SpoolStatsOf(ds)
			if entries != 1 || size <= 0 || oldest.IsZero() || time.Since(oldest) < 0 {
				t.Fatalf("spool stats = %d entries, %d bytes, oldest %v", entries, size, oldest)
			}

			if err := svc.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
			svc2 := mustReopen(t, dsn, root)
			ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
			if err != nil {
				t.Fatalf("reopen the repository: %v", err)
			}
			dispatch(t, ds2)
			resumedHook(t, ds2, fid, shape.name, shape.blob)
			awaitSpoolEmpty(t, ds2, dir)
			if err := svc2.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
			if n := admissions(t, dsn, fid); n != 1 {
				t.Fatalf("delivery entries recording %s = %d, want 1", fid, n)
			}
			if n := okRunsAfterClose(t, dsn, fid); n != 1 {
				t.Fatalf("OK runs under %s = %d, want 1", fid, n)
			}
		})
	}
}

// A file the drain recorded and whose deletion never happened (a stop
// between the commit and the delete, or a copy taken between them) is
// drained again by the next process, which finds the fire in the changelog,
// deletes the file and records nothing: one delivery entry and one run.
func TestADrainThatFindsARecordedRequestOnlyDeletesItsFile(t *testing.T) {
	noSpoolBeforeTheGrace(t)
	ctx := context.Background()
	shape := heldRequests["a json body"]
	svc, ds, dsn, root, _ := newHeldHookDataset(t, true)
	id := ds.Repository().ID
	fid, err := engine.ReceiveWebhookSpooled(ctx, svc, testdb.Repository(t), "hook-held", "", shape.req)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	dir := spoolDirOf(t, root, id)
	kept := spooledFiles(t, dir)
	if len(kept) != 1 {
		t.Fatalf("the spool holds %d files, want 1", len(kept))
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// The first drain records, fires and deletes.
	svc2 := mustReopen(t, dsn, root)
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	dispatch(t, ds2)
	resumedHook(t, ds2, fid, shape.name, shape.blob)
	awaitSpoolEmpty(t, ds2, dir)
	if err := svc2.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// The file comes back, as a stop before its deletion would have left it.
	for name, data := range kept {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	svc3 := mustReopen(t, dsn, root)
	ds3, err := svc3.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	dispatch(t, ds3)
	awaitSpoolEmpty(t, ds3, dir)
	if failures, err := ds3.TriggerFailures(ctx, "hook-held"); err != nil || len(failures) != 0 {
		t.Fatalf("the second drain left entries: %+v, %v", failures, err)
	}
	if err := svc3.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if n := admissions(t, dsn, fid); n != 1 {
		t.Fatalf("delivery entries recording %s = %d, want 1", fid, n)
	}
	if n := okRunsAfterClose(t, dsn, fid); n != 1 {
		t.Fatalf("OK runs under %s = %d, want 1", fid, n)
	}
}

// At either bound the door refuses with ErrWebhookSpoolFull, an unavailable
// (503 with Retry-After at the door), writes nothing, and takes requests
// again once the drain has made room.
func TestTheWebhookDoorRefusesWhileTheSpoolIsFull(t *testing.T) {
	ctx := context.Background()
	shape := heldRequests["a json body"]
	svc, ds, _, root, _ := newHeldHookDataset(t, true)
	dir := spoolDirOf(t, root, ds.Repository().ID)

	engine.WithWebhookSpoolBounds(t, 1, 1<<30)
	if _, err := engine.ReceiveWebhookSpooled(ctx, svc, testdb.Repository(t), "hook-held", "", shape.req); err != nil {
		t.Fatalf("the first request: %v", err)
	}
	_, err := engine.ReceiveWebhookSpooled(ctx, svc, testdb.Repository(t), "hook-held", "", shape.req)
	if !errors.Is(err, engine.ErrWebhookSpoolFull) || !errors.Is(err, substrate.ErrUnavailable) {
		t.Fatalf("a request past the entry bound = %v, want the spool-full unavailable", err)
	}
	if n := len(spooledFiles(t, dir)); n != 1 {
		t.Fatalf("the refused request left %d files, want 1", n)
	}
	dispatch(t, ds)
	awaitSpoolEmpty(t, ds, dir)
	if _, err := engine.ReceiveWebhookSpooled(ctx, svc, testdb.Repository(t), "hook-held", "", shape.req); err != nil {
		t.Fatalf("a request after the drain: %v", err)
	}

	engine.WithWebhookSpoolBounds(t, 100, 16)
	if _, err := engine.ReceiveWebhookSpooled(ctx, svc, testdb.Repository(t), "hook-held", "", shape.req); !errors.Is(err, engine.ErrWebhookSpoolFull) {
		t.Fatalf("a request past the byte bound = %v, want the spool-full unavailable", err)
	}
}

// The spooled file carries no credential the request arrived with: not an
// undeclared header's value or name, not the query, and not the body, which
// it holds only sealed under the repository's DEK.
func TestASpooledWebhookHoldsNoCredentialInTheClear(t *testing.T) {
	ctx := context.Background()
	svc, ds, _, root, _ := newHeldHookDataset(t, true)
	req := substrate.WebhookRequest{
		Method: "POST", ContentType: "application/json",
		Headers: map[string]string{
			"content-type":   "application/json",
			"x-github-event": "held",
			"x-api-token":    "tok-in-a-header-7f3a",
		},
		Query: map[string][]string{"token": {"tok-in-the-query-91c2"}},
		Body:  []byte(`{"say":"tok-in-the-body-5be0"}`),
	}
	if _, err := engine.ReceiveWebhookSpooled(ctx, svc, testdb.Repository(t), "hook-held", "", req); err != nil {
		t.Fatalf("receive: %v", err)
	}
	files := spooledFiles(t, spoolDirOf(t, root, ds.Repository().ID))
	if len(files) != 1 {
		t.Fatalf("the spool holds %d files, want 1", len(files))
	}
	for name, data := range files {
		for _, secret := range []string{"tok-in-a-header-7f3a", "x-api-token", "tok-in-the-query-91c2", "tok-in-the-body-5be0", `"say"`} {
			if bytes.Contains(data, []byte(secret)) || bytes.Contains([]byte(name), []byte(secret)) {
				t.Fatalf("spool/%s carries %q in the clear", name, secret)
			}
		}
	}
}

// A snapshot of the stopped server and an owner's export both carry a
// request answered and not drained, and the server restored from either into
// an empty database drains it and fires it once.
func TestASpooledWebhookRestoresFromASnapshotAndAnExport(t *testing.T) {
	noSpoolBeforeTheGrace(t)
	shape := heldRequests["a multipart request"]
	restore := func(t *testing.T, root, fid string) {
		t.Helper()
		svc2 := mustReopen(t, engine.MigratedDSN(t), root)
		ds2, err := svc2.Dataset(context.Background(), testdb.Repository(t))
		if err != nil {
			t.Fatalf("open the restored repository: %v", err)
		}
		dispatch(t, ds2)
		resumedHook(t, ds2, fid, shape.name, shape.blob)
		awaitSpoolEmpty(t, ds2, spoolDirOf(t, root, ds2.Repository().ID))
	}

	t.Run("export", func(t *testing.T) {
		ctx := context.Background()
		svc, ds, _, _, _ := newHeldHookDataset(t, true)
		fid, err := engine.ReceiveWebhookSpooled(ctx, svc, testdb.Repository(t), "hook-held", "", shape.req)
		if err != nil {
			t.Fatalf("receive: %v", err)
		}
		root, _, m := exportToRoot(t, ds)
		if n := len(m.Spool); n != 1 {
			t.Fatalf("the export carries %d spooled requests, want 1", n)
		}
		if err := svc.Close(); err != nil {
			t.Fatal(err)
		}
		restore(t, root, fid)
	})
}

// The service refuses a part name or filename carrying a NUL before it
// writes a spool file, so the door never answers 202 for a request no row
// can store.
func TestNULSpoolPartRefusedAtTheDoor(t *testing.T) {
	ctx := context.Background()
	svc, ds, _, root, _ := newHeldHookDataset(t, true)
	dir := spoolDirOf(t, root, ds.Repository().ID)
	poison := substrate.WebhookRequest{
		Method: "POST", ContentType: "multipart/form-data",
		Parts: []substrate.WebhookPart{{Name: "bad\x00name", Value: "x"}},
	}

	if _, err := engine.ReceiveWebhookSpooled(ctx, svc, testdb.Repository(t), "hook-held", "", poison); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("the door spooled a NUL part name: %v", err)
	}
	filed := poison
	filed.Parts = []substrate.WebhookPart{{Name: "f", Filename: "evil\x00.txt", Data: []byte("x")}}
	if _, err := engine.ReceiveWebhookSpooled(ctx, svc, testdb.Repository(t), "hook-held", "", filed); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("the door spooled a NUL filename: %v", err)
	}
	if files := spooledFiles(t, dir); len(files) != 0 {
		t.Fatalf("a refused request left files: %v", files)
	}
}

// A spooled request whose admission is refused for its content (here a part
// name carrying a NUL, which no jsonb row stores, spooled by a binary before
// the door refused it) is set aside as `.refused` and the drain goes on: the
// request spooled after it is recorded and fired.
func TestNULSpoolPoisonIsSetAside(t *testing.T) {
	ctx := context.Background()
	svc, ds, dsn, root, _ := newHeldHookDataset(t, true)
	dir := spoolDirOf(t, root, ds.Repository().ID)
	poison := substrate.WebhookRequest{
		Method: "POST", ContentType: "multipart/form-data",
		Parts: []substrate.WebhookPart{{Name: "bad\x00name", Value: "x"}},
	}
	bad, err := engine.SpoolWebhookUnchecked(ctx, ds, "hook-held", poison)
	if err != nil {
		t.Fatalf("spool the poison: %v", err)
	}
	shape := heldRequests["a json body"]
	good, err := engine.ReceiveWebhookSpooled(ctx, svc, testdb.Repository(t), "hook-held", "", shape.req)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	dispatch(t, ds)
	resumedHook(t, ds, good, shape.name, shape.blob)

	deadline := time.Now().Add(30 * time.Second)
	for {
		files := spooledFiles(t, dir)
		aside := 0
		for name := range files {
			if strings.HasSuffix(name, "-"+bad+".webhook"+changelogfile.RefusedSpoolSuffix) {
				aside++
			}
		}
		if len(files) == 1 && aside == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the spool holds %v, want only the poison set aside", files)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if entries, _, _ := engine.SpoolStatsOf(ds); entries != 0 {
		t.Fatalf("the spool index still counts %d requests", entries)
	}
	if n := admissions(t, dsn, bad); n != 0 {
		t.Fatalf("the poison has %d delivery entries", n)
	}
}
