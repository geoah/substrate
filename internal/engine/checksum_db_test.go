package engine_test

// The checksum, end to end: every committed entry carries the SHA-256 of its
// canonical line, the same value the segment file format computes; verify
// recomputes the lot from the stored rows and names every damaged or missing
// seq; and a credential key that does not open the store's DEK wraps is
// refused at boot.

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

type verifier interface {
	VerifyRepository(ctx context.Context, username string) (engine.VerifyReport, error)
}

// rawDB opens the DSN's own superuser connection: the tamperer's seat, which
// bypasses row level security on purpose.
func rawDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newDatasetWithDSN is newDataset with the DSN kept: these tests need the
// tamperer's seat beside the front door.
func newDatasetWithDSN(t *testing.T, opts ...engine.Option) (substrate.Service, substrate.Dataset, string) {
	t.Helper()
	svc, dsn := newService(t, opts...)
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	importVocabulary(t, ds, "tasks")
	return svc, ds, dsn
}

func mustVerify(t *testing.T, svc substrate.Service, username string) engine.VerifyReport {
	t.Helper()
	report, err := svc.(verifier).VerifyRepository(context.Background(), username)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return report
}

func findingContaining(report engine.VerifyReport, substr string) bool {
	for _, f := range report.Findings {
		if strings.Contains(f, substr) {
			return true
		}
	}
	return false
}

// The table and the file format agree by construction: the `hash` a write
// stamps is exactly what changelogfile.Encode computes over the row as
// Postgres stored it, so the segment file and the table can be compared entry
// by entry. This test rebuilds the entry from the row's own columns, the way
// a boot check or an import will.
func TestChecksumStampedOnWriteMatchesTheFileEncoding(t *testing.T) {
	t.Parallel()
	_, ds, dsn := newDatasetWithDSN(t)
	// Every row is checked, the seed's vocabulary declarations included, so
	// the payloads carry nested keys and integer versions: jsonb respells
	// both, and canonicalization has something to do.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind:       "samples.substrate.reamde.dev/tasks/task",
		Properties: map[string]any{"name": "checksummed"},
	})
	db := rawDB(t, dsn)
	rows, err := db.Query(`
		SELECT seq, ts, actor, principal, op, record_id, kind, payload::text, caused_by, hash
		FROM changelog ORDER BY seq`)
	if err != nil {
		t.Fatalf("read the changelog: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var n int
	for rows.Next() {
		var e changelogfile.Entry
		var ts time.Time
		var causedBy sql.NullInt64
		var payload, hash []byte
		if err := rows.Scan(&e.Seq, &ts, &e.Actor, &e.Principal, &e.Op, &e.RecordID, &e.Kind, &payload, &causedBy, &hash); err != nil {
			t.Fatalf("scan: %v", err)
		}
		e.TS = ts.UTC()
		e.CausedBy, e.CausedByOK = causedBy.Int64, causedBy.Valid
		e.Payload = json.RawMessage(payload)
		line, sum, err := changelogfile.Encode(e)
		if err != nil {
			t.Fatalf("seq %d: encode: %v", e.Seq, err)
		}
		if len(hash) != 32 {
			t.Fatalf("seq %d carries a %d-byte checksum, want 32", e.Seq, len(hash))
		}
		if sum != [32]byte(hash) {
			t.Fatalf("seq %d: the stamped checksum is not what the file format computes\nline: %s", e.Seq, line)
		}
		// The line decodes back to the same checksum, so a reader of the
		// file arrives at the value the table holds.
		if _, got, err := changelogfile.Decode(line); err != nil || got != sum {
			t.Fatalf("seq %d: the encoded line does not decode to its own checksum: %v", e.Seq, err)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	if n == 0 {
		t.Fatal("the changelog is empty; the test proves nothing")
	}
}

// Verify names damage by seq: an edited row whose checksum no longer matches,
// a row with no checksum, and a deleted row that reads as a gap.
func TestVerifyNamesDamageBySeq(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	for _, name := range []string{"one", "two", "three"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind:       "samples.substrate.reamde.dev/tasks/task",
			Properties: map[string]any{"name": name},
		})
	}
	if report := mustVerify(t, svc, "geoah"); !report.OK || report.Head != report.Entries {
		t.Fatalf("a fresh repository does not verify: %+v", report)
	}
	db := rawDB(t, dsn)
	var head int64
	if err := db.QueryRow(`SELECT max(seq) FROM changelog`).Scan(&head); err != nil {
		t.Fatalf("read the head: %v", err)
	}
	// Edit the head's actor in place: the checksum stops matching.
	if _, err := db.Exec(`UPDATE changelog SET actor = 'somebody-else' WHERE seq = $1`, head); err != nil {
		t.Fatalf("edit the head: %v", err)
	}
	// Strip the checksum off the entry below it.
	if _, err := db.Exec(`UPDATE changelog SET hash = NULL WHERE seq = $1`, head-1); err != nil {
		t.Fatalf("strip a checksum: %v", err)
	}
	// Delete the one below that: a gap.
	if _, err := db.Exec(`DELETE FROM changelog WHERE seq = $1`, head-2); err != nil {
		t.Fatalf("delete an entry: %v", err)
	}
	report := mustVerify(t, svc, "geoah")
	if report.OK {
		t.Fatalf("a damaged changelog verified: %+v", report)
	}
	seq := func(v int64) string { return strconv.FormatInt(v, 10) }
	for _, want := range []string{
		"seq " + seq(head) + ": checksum mismatch",
		"seq " + seq(head-1) + ": no checksum",
		"seq " + seq(head-1) + " follows " + seq(head-3) + ": the sequence has a gap",
	} {
		if !findingContaining(report, want) {
			t.Errorf("no finding contains %q: %+v", want, report.Findings)
		}
	}
}

// A credential key that does not open what the database already holds is
// refused AT BOOT, not discovered one repository at a time. This is the shape
// a lost key volume produces: a fresh key minted over a store full of
// repositories nothing can now unwrap, on a server that would otherwise
// listen and report healthy.
func TestAWrongCredentialKeyIsRefusedAtBoot(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	ctx := context.Background()
	mustPut(t, ds, owner, substrate.PutInput{
		Kind:       "samples.substrate.reamde.dev/tasks/task",
		Properties: map[string]any{"name": "sealed"},
	})
	db := rawDB(t, dsn)
	var original []byte
	if err := db.QueryRow(`SELECT dek FROM repositories LIMIT 1`).Scan(&original); err != nil {
		t.Fatalf("read the stored DEK wrap: %v", err)
	}
	_ = svc.Close()

	// Bound-sealed framing ('a'), then bytes this credential key cannot open:
	// what every repository looks like to a host holding the wrong key.
	if _, err := db.Exec(
		`UPDATE repositories SET dek = decode('61' || repeat('ff', 60), 'hex')`); err != nil {
		t.Fatalf("spoil the stored wrap: %v", err)
	}
	_, err := engine.Open(ctx, dsn,
		engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"),
		engine.WithDataRoot(t.TempDir()),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err == nil {
		t.Fatal("a host whose key opens nothing in this database booted anyway")
	}
	if !strings.Contains(err.Error(), "does not open the DEK") {
		t.Fatalf("the boot refusal does not name the cause: %v", err)
	}

	// And it is a refusal about the KEY, not a bricked store: the original
	// wrap back, the same host boots and the repository verifies.
	if _, err := db.Exec(`UPDATE repositories SET dek = $1`, original); err != nil {
		t.Fatalf("restore the stored wrap: %v", err)
	}
	svc2, err := engine.Open(ctx, dsn,
		engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"),
		engine.WithDataRoot(t.TempDir()),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err != nil {
		t.Fatalf("reopen with the original wrap restored: %v", err)
	}
	t.Cleanup(func() { _ = svc2.Close() })
	if report := mustVerify(t, svc2, "geoah"); !report.OK {
		t.Fatalf("the store did not survive the refused boot: %+v", report.Findings)
	}
}
