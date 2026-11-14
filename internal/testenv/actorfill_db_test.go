package testenv_test

// THE ACTOR RECORDS A REPOSITORY FROM BEFORE ACTORS LINKS (decision 0270,
// engine/actorfill.go). A release's repository, imported and upgraded under
// the current code, links an actor record from every line, change event,
// mark and stored reference to a hand, and holds every one of them, of the
// type the hand is: its person, the system, each package, function and agent
// it ever declared, disabled where the declaration is gone.

import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/testenv"
	"github.com/geoah/substrate/internal/vocabulary"
)

// actorRefIn is a link to an actor record inside a payload or a record, and
// actorIDIn a mark's setBy or onBehalfOf.
var (
	actorRefIn = regexp.MustCompile(`"ref":"` + regexp.QuoteMeta(vocabulary.CoreKind(vocabulary.DocActor)) + `/([^"]+)"`)
	actorIDIn  = regexp.MustCompile(`"(?:setBy|onBehalfOf)":"([^"]+)"`)
)

// actorHand is what the actor record of one id must be: the type of its
// hand, and whether the hand is gone.
type actorHand struct {
	typ  string
	gone bool
}

// keptHandsOf is every hand the release's repository held, by its actor
// record's id, from the records it serves, a declaration's tombstone among
// them.
func keptHandsOf(got map[string]keptRecord) map[string]actorHand {
	hands := map[string]actorHand{
		vocabulary.ActorID(substrate.ActorPerson): {typ: substrate.ActorTypePerson},
		vocabulary.ActorID(substrate.ActorSystem): {typ: substrate.ActorTypeSystem},
	}
	add := func(key substrate.Actor, typ string, gone bool) {
		id := vocabulary.ActorID(key)
		if h, ok := hands[id]; ok && !h.gone {
			gone = false
		}
		hands[id] = actorHand{typ: typ, gone: gone}
	}
	for _, rec := range got {
		kind, id := fmt.Sprint(rec.Read["kind"]), fmt.Sprint(rec.Read["id"])
		gone := rec.Read["deletedAt"] != nil
		switch kind {
		case vocabulary.CoreKind("package"):
			if authority, pkg := vocabulary.SplitPackageRef(id); authority != "" {
				add(substrate.BundleActor(authority, pkg), substrate.ActorTypePackage, gone)
			}
		case vocabulary.CoreKind("function"), vocabulary.CoreKind("agent"):
			authority, pkg, name := vocabulary.SplitKindRef(id)
			if authority == "" {
				continue
			}
			if kind == vocabulary.CoreKind("function") {
				add(substrate.FunctionActor(authority, pkg, name), substrate.ActorTypeFunction, gone)
			} else {
				add(substrate.AgentActor(authority, pkg, name), substrate.ActorTypeAgent, gone)
			}
		}
	}
	return hands
}

// keptLinkedActors is every actor id the repository links, with where: each
// line's actor and the one it acted for, and every link and mark in a line,
// as the export carries the history; each change event's; and every link and
// mark a record serves.
func keptLinkedActors(t *testing.T, e *testenv.Env, got map[string]keptRecord) map[string]string {
	t.Helper()
	linked := map[string]string{}
	link := func(where, id string) {
		if id != "" {
			linked[id] = where
		}
	}
	status, raw, _ := e.DoRaw(http.MethodGet, "/api/v1/export", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("the export: %d %s", status, raw)
	}
	lines := 0
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(hdr.Name, "/"+changelogfile.ChangelogSubdir+"/") || !strings.HasSuffix(hdr.Name, ".ndjson") {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
			l, _, err := changelogfile.Decode(line)
			if err != nil {
				t.Fatalf("%s: %v", hdr.Name, err)
			}
			lines++
			where := fmt.Sprintf("line %d", l.Seq)
			link(where+" actor", l.Actor)
			link(where+" onBehalfOf", l.OnBehalfOf)
			for _, m := range actorRefIn.FindAllSubmatch(l.Payload, -1) {
				link(where+" link", string(m[1]))
			}
			for _, m := range actorIDIn.FindAllSubmatch(l.Payload, -1) {
				link(where+" mark", string(m[1]))
			}
		}
	}
	if lines == 0 {
		t.Fatal("the export carried no history line")
	}
	q := url.Values{"first": {"500"}}
	for {
		var page substrate.ChangePage
		e.MustJSON(http.MethodGet, "/api/v1/changes?"+q.Encode(), nil, &page)
		for _, c := range page.Changes {
			link(fmt.Sprintf("change %d actor", c.Seq), c.Actor)
			link(fmt.Sprintf("change %d onBehalfOf", c.Seq), c.OnBehalfOf)
		}
		if page.Cursor == 0 {
			break
		}
		q = url.Values{"first": {"500"}, "before": {fmt.Sprint(page.Cursor)}, "generation": {page.Generation}}
	}
	for path, rec := range got {
		body, err := json.Marshal(rec.Read)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range actorRefIn.FindAllSubmatch(body, -1) {
			link(path+" link", string(m[1]))
		}
	}
	return linked
}

// holdsEveryActorItLinks holds the repository e serves to the actor record
// of every id it links: one it serves, of its hand's type, disabled where
// the hand is gone.
func holdsEveryActorItLinks(t *testing.T, e *testenv.Env, got map[string]keptRecord) int {
	t.Helper()
	hands := keptHandsOf(got)
	linked := keptLinkedActors(t, e, got)
	for id, where := range linked {
		if !vocabulary.MintedID(id) {
			t.Errorf("%s links %q, which is no actor record's id", where, id)
			continue
		}
		h, ok := hands[id]
		if !ok {
			t.Errorf("%s links the actor %s, which no hand the repository held derives", where, id)
			continue
		}
		status, body := e.Do(http.MethodGet, "/api/v1/"+vocabulary.CoreKind(vocabulary.DocActor)+"/"+id, nil)
		if status != http.StatusOK {
			t.Errorf("%s links the actor %s (a %s), which the repository serves no record of: %d %s", where, id, h.typ, status, body)
			continue
		}
		var rec struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(body, &rec); err != nil {
			t.Fatal(err)
		}
		if typ := rec.Properties["type"]; typ != h.typ {
			t.Errorf("the actor %s that %s links is a %v, want a %s", id, where, typ, h.typ)
		}
		if disabled := rec.Properties["disabled"] == true; disabled != h.gone {
			t.Errorf("the actor %s that %s links is disabled %t, and its hand is gone %t", id, where, disabled, h.gone)
		}
	}
	return len(linked)
}

func TestAnImportedKeptDirectoryHoldsEveryActorItLinks(t *testing.T) {
	k := checkKeptDirectory(t, filepath.Join(keptRoot, "v0.123.1"), keptConversions)
	n := holdsEveryActorItLinks(t, k.e, k.got)
	t.Logf("%d actors linked, each held", n)
}

// A REPOSITORY THAT RAN MIGRATION 9 WITHOUT ITS ACTOR RECORDS. A binary from
// before the fill imported the release's repository: its ledger says
// migration 9 ran, which wrote none of the records its history links, and
// nothing since wrote them. The next open under this binary writes each,
// a rebuild gives the same fold, and the open after writes nothing.
func TestARepositoryThatRanMigration9WithoutItsActorRecordsGetsThemAtTheNextOpen(t *testing.T) {
	dir := filepath.Join(keptRoot, "v0.123.1")
	var fixture keptFixture
	readKept(t, filepath.Join(dir, "fixture.json"), &fixture)
	root := t.TempDir()
	copyKept(t, filepath.Join(dir, changelogfile.RepositoriesDir), filepath.Join(root, changelogfile.RepositoriesDir))
	dsn := testdb.NewSchema(t)
	start := func(opts ...engine.Option) *testenv.Env {
		e := testenv.Start(t, testenv.WithoutRegistration(), testenv.WithRepository(fixture.Repository),
			testenv.WithDSN(dsn), testenv.WithDataRoot(root), testenv.WithCredentialKey(fixture.CredentialKey),
			testenv.WithEngineOptions(opts...))
		e.Token = fixture.Token
		return e
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	e := start(engine.WithTestActorFillOff())
	ds, err := e.Service.Dataset(ctx, fixture.Repository)
	if err != nil {
		t.Fatal(err)
	}
	id := ds.Repository().ID
	var ran bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM repository_migrations WHERE repository = $1 AND version = 9)`, id).Scan(&ran); err != nil || !ran {
		t.Fatalf("migration 9 ran %t (%v), want its ledger row", ran, err)
	}
	missing := vocabulary.ActorID(substrate.BundleActor(fixture.Repository, "tasks"))
	if status, _ := e.Do(http.MethodGet, "/api/v1/"+vocabulary.CoreKind(vocabulary.DocActor)+"/"+missing, nil); status != http.StatusNotFound {
		t.Fatalf("the tasks package's actor answers %d before the fill, want the 404 a binary from before it left", status)
	}
	e.Stop()

	e = start()
	n := holdsEveryActorItLinks(t, e, keptRead(t, e))
	head := changesHead(t, e)
	ds, err = e.Service.Dataset(ctx, fixture.Repository)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := func() []byte {
		t.Helper()
		raw, err := ds.(interface {
			FoldSnapshot(context.Context) ([]byte, error)
		}).FoldSnapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	if _, err := e.Service.(interface {
		RebuildRepository(context.Context, string) (engine.RebuildReport, error)
	}).RebuildRepository(ctx, id); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := snapshot(); !bytes.Equal(before, after) {
		t.Fatal("a rebuild of the filled repository gives another fold")
	}
	e.Stop()

	e = start()
	if again := changesHead(t, e); again != head {
		t.Fatalf("the open after the fill wrote %d lines, want none", again-head)
	}
	t.Logf("%d actors linked, each held after the next open", n)
}

// changesHead is the repository's history head, as the change feed names it.
func changesHead(t *testing.T, e *testenv.Env) int64 {
	t.Helper()
	var page substrate.ChangePage
	e.MustJSON(http.MethodGet, "/api/v1/changes?first=1", nil, &page)
	return page.Head
}
