package engine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// stateOf is what a process that writes nothing leaves as it was: every
// table of the schema, each as a digest of its rows, with its grants; every
// sequence of the schema at its last value, since a nextval leaves no row;
// the two roles, their attributes and their memberships; and the data root,
// every path under it with its mode and, for a file, its size and the hash
// of its bytes, or that it is not there. It does not cover the schema's other
// objects (functions, views, policies), the cluster's other roles, or
// anything outside the data root.
type stateOf struct {
	tables    map[string]string
	sequences []string
	roles     []string
	root      []string
}

func observe(t *testing.T, db *sql.DB, root string) stateOf {
	t.Helper()
	s := stateOf{tables: map[string]string{}}
	rows, err := db.Query(`SELECT c.relname, coalesce(c.relacl::text, '') FROM pg_class c
		WHERE c.relnamespace = current_schema()::regnamespace AND c.relkind IN ('r', 'p') ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	type table struct{ name, acl string }
	var tables []table
	for rows.Next() {
		var tb table
		if err := rows.Scan(&tb.name, &tb.acl); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, tb)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	if len(tables) == 0 {
		t.Fatal("the schema holds no table")
	}
	for _, tb := range tables {
		var digest string
		if err := db.QueryRow(`SELECT coalesce(md5(string_agg(t::text, E'\n' ORDER BY t::text)), '') FROM ` + tb.name + ` t`).Scan(&digest); err != nil {
			t.Fatalf("read %s: %v", tb.name, err)
		}
		s.tables[tb.name] = digest + " " + tb.acl
	}
	seqs, err := db.Query(`SELECT sequencename || ' ' || coalesce(last_value::text, 'unused') FROM pg_sequences
		WHERE schemaname = current_schema() ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	for seqs.Next() {
		var line string
		if err := seqs.Scan(&line); err != nil {
			t.Fatal(err)
		}
		s.sequences = append(s.sequences, line)
	}
	if err := errors.Join(seqs.Err(), seqs.Close()); err != nil {
		t.Fatal(err)
	}
	roles, err := db.Query(`
		SELECT r.rolname || ' ' || r.rolsuper || r.rolinherit || r.rolcreaterole || r.rolcreatedb || r.rolcanlogin || r.rolbypassrls ||
		       ' member of ' || coalesce((SELECT string_agg(g.rolname, ',' ORDER BY g.rolname) FROM pg_auth_members m
		                                  JOIN pg_roles g ON g.oid = m.roleid WHERE m.member = r.oid), '') ||
		       ' members ' || coalesce((SELECT string_agg(u.rolname, ',' ORDER BY u.rolname) FROM pg_auth_members m
		                                JOIN pg_roles u ON u.oid = m.member WHERE m.roleid = r.oid), '')
		FROM pg_roles r WHERE r.rolname IN ($1, $2) ORDER BY r.rolname`, roleApp, roleMaint)
	if err != nil {
		t.Fatal(err)
	}
	for roles.Next() {
		var line string
		if err := roles.Scan(&line); err != nil {
			t.Fatal(err)
		}
		s.roles = append(s.roles, line)
	}
	if err := errors.Join(roles.Err(), roles.Close()); err != nil {
		t.Fatal(err)
	}
	if len(s.roles) != 2 {
		t.Fatalf("the roles are %v", s.roles)
	}
	if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
		s.root = []string{"not there"}
		return s
	} else if err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		entry := fmt.Sprintf("%s %v", rel, info.Mode())
		switch {
		case info.Mode().IsRegular():
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			entry += fmt.Sprintf(" %d %x", info.Size(), sha256.Sum256(raw))
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entry += " -> " + target
		}
		s.root = append(s.root, entry)
		return nil
	}); err != nil {
		t.Fatalf("read the data root: %v", err)
	}
	return s
}

func (s stateOf) same(t *testing.T, scenario string, after stateOf) {
	t.Helper()
	for table, digest := range s.tables {
		if after.tables[table] != digest {
			t.Errorf("%s changed the table %s, its rows or its grants", scenario, table)
		}
	}
	if len(after.tables) != len(s.tables) {
		t.Errorf("%s left %d tables, there were %d", scenario, len(after.tables), len(s.tables))
	}
	if !reflect.DeepEqual(after.sequences, s.sequences) {
		t.Errorf("%s moved a sequence:\n before %v\n after  %v", scenario, s.sequences, after.sequences)
	}
	if !reflect.DeepEqual(after.roles, s.roles) {
		t.Errorf("%s changed the roles:\n before %v\n after  %v", scenario, s.roles, after.roles)
	}
	if !reflect.DeepEqual(after.root, s.root) {
		t.Errorf("%s changed the data root:\n before %v\n after  %v", scenario, s.root, after.root)
	}
}

func storedStamp(t *testing.T, db *sql.DB, repository string) int {
	t.Helper()
	var d int
	if err := db.QueryRow(`SELECT dialect FROM vocabulary_dialect WHERE repository = $1`, repository).Scan(&d); err != nil {
		t.Fatalf("read the declaration format stamp: %v", err)
	}
	return d
}

// A READ-ONLY START AND OPEN WRITE NOTHING (issue #828): no row of any table,
// no grant, no role, and nothing at or under the data root, which need not be
// there; nor do they provision the roles. A repository stamped below this
// binary's declaration format opens as stored, its stamp left, so a server
// of the release before still opens it. So does one with a shipped upgrade
// pending and a stored closure this binary quarantines, through
// `repository history`'s read (HistoryByKind). With a repository migration
// pending the open is refused, as before, and the refusal no longer comes
// after the stamp.
func TestAReadOnlyOpenLeavesTheStampAndEveryRowAsStored(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := MigratedDSN(t)
	var provisioned atomic.Int32
	provision := func(o *options) {
		o.provisionRoles = func(ctx context.Context, db *sql.DB) error {
			provisioned.Add(1)
			return ensureRoles(ctx, db)
		}
	}
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(t.TempDir()), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds), provision)
	if err != nil {
		t.Fatal(err)
	}
	if provisioned.Load() != 1 {
		t.Fatalf("the writer's start provisioned the roles %d times, want once", provisioned.Load())
	}
	info, err := svc.CreateRepository(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	id := info.ID
	// Its first open by the writer runs its repository migrations; a
	// package of its own is what this binary will quarantine below.
	ds, err := svc.Dataset(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest("~/notes", 0),
		vocabulary.KindManifest("~/notes", map[string]any{"singular": "note"}, map[string]any{
			"properties": map[string]any{"text": map[string]any{"type": "string"}},
		}),
		vocabulary.AgentManifest("~/notes", "helper", map[string]any{
			"description": "an agent this binary will quarantine", "prompt": "You help.",
			"provider": "anthropic", "model": "claude-x",
		}),
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	below := maxVocabularyDialect - 1
	if _, err := raw.Exec(`UPDATE vocabulary_dialect SET dialect = $2 WHERE repository = $1`, id, below); err != nil {
		t.Fatalf("stamp the repository below the binary: %v", err)
	}
	// readOnly starts a read-only engine over root, runs read on it, and
	// holds everything to what it was before the start.
	readOnly := func(scenario, root string, read func(*service) error) error {
		t.Helper()
		before := observe(t, raw, root)
		provisioned.Store(0)
		// Deferred, so it runs on a Fatalf as well: a start that writes and
		// then fails is held to what it found.
		defer func() {
			before.same(t, scenario, observe(t, raw, root))
			if got := storedStamp(t, raw, id); got != below {
				t.Errorf("%s left the stamp at %d, want %d as stored", scenario, got, below)
			}
			if n := provisioned.Load(); n != 0 {
				t.Errorf("%s provisioned the roles %d times, want none", scenario, n)
			}
		}()
		ro, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithCredentialKey(TestCredentialKey),
			WithKindsFS(SeedKinds), WithDirectoryReadOnly(), provision)
		if err != nil {
			t.Fatalf("%s: start read-only: %v", scenario, err)
		}
		err = read(ro.(*service))
		if cerr := ro.Close(); cerr != nil {
			t.Fatalf("%s: stop: %v", scenario, cerr)
		}
		return err
	}
	open := func(s *service) error {
		_, err := s.Dataset(ctx, id)
		return err
	}

	if err := readOnly("an open over a data root", t.TempDir(), open); err != nil {
		t.Fatalf("a read-only open of a repository stamped %d, below %d: %v", below, maxVocabularyDialect, err)
	}
	if err := readOnly("an open over no data root", filepath.Join(t.TempDir(), "not-there"), open); err != nil {
		t.Fatalf("a read-only open with no data root: %v", err)
	}

	// A shipped upgrade pending, one seeded kind stored a version behind the
	// binary's, and a closure of the repository's own that no longer parses,
	// its agent stored with a key this binary does not know in place of its
	// provider and model: a writer's open would upgrade the one and mark the
	// other quarantined.
	if res, err := raw.Exec(`UPDATE records SET props = jsonb_set(props, '{version}', to_jsonb((props->>'version')::int - 1))
		WHERE repository = $1 AND kind = $2 AND id = $3 AND deleted_at IS NULL`, id, kindKind, "substrate.reamde.dev/core/contact"); err != nil {
		t.Fatalf("store the contact kind a version behind: %v", err)
	} else if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("stored the contact kind a version behind on %d rows", n)
	}
	if res, err := raw.Exec(`UPDATE records SET props = (props - 'provider' - 'model') || '{"llm": "cheap"}'::jsonb
		WHERE repository = $1 AND kind = $2 AND id = $3 AND deleted_at IS NULL`, id, kindAgent, "~/notes/helper"); err != nil {
		t.Fatalf("store the agent with a key this binary does not know: %v", err)
	} else if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("stored the agent with an unknown key on %d rows", n)
	}
	var history []HistoryKind
	if err := readOnly("`repository history` over a pending upgrade and a closure to quarantine", t.TempDir(), func(s *service) error {
		var err error
		history, err = s.HistoryByKind(ctx, id)
		return err
	}); err != nil {
		t.Fatalf("`repository history` read-only over a pending upgrade and a closure to quarantine: %v", err)
	}
	if len(history) == 0 {
		t.Error("`repository history` read no kind")
	}

	// A repository migration pending: refused as before.
	var version int
	var name string
	if err := raw.QueryRow(`DELETE FROM repository_migrations WHERE repository = $1
		AND version = (SELECT max(version) FROM repository_migrations WHERE repository = $1) RETURNING version, name`, id).Scan(&version, &name); err != nil {
		t.Fatalf("unrecord the newest repository migration: %v", err)
	}
	if err := readOnly("an open with a repository migration pending", t.TempDir(), open); err == nil || !strings.Contains(err.Error(), "pending repository migration") {
		t.Fatalf("a read-only open with a repository migration pending: %v, want it refused", err)
	}

	// The scenario above is what it says: a writer's open of the same
	// repository quarantines the closure the read-only one only logged.
	if _, err := raw.Exec(`INSERT INTO repository_migrations (repository, version, name) VALUES ($1, $2, $3)`, id, version, name); err != nil {
		t.Fatalf("record the repository migration again: %v", err)
	}
	writer, err := OpenForTest(t, ctx, dsn, WithDataRoot(t.TempDir()), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	if _, err := writer.Dataset(ctx, id); err != nil {
		t.Fatalf("the writer's open: %v", err)
	}
	var quarantined bool
	if err := raw.QueryRow(`SELECT coalesce((props->>$4)::boolean, false) FROM records
		WHERE repository = $1 AND kind = $2 AND id = $3 AND deleted_at IS NULL`,
		id, kindPackage, "~/notes", propPackageQuarantined).Scan(&quarantined); err != nil {
		t.Fatal(err)
	}
	if !quarantined {
		t.Error("a writer's open did not quarantine ~/notes, so the read-only scenario read through nothing to quarantine")
	}
}
