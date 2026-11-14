package testenv_test

// THE KEPT DIRECTORIES (docs/stored-format.md, decision 0260). Each release
// keeps one repository directory its own server wrote (testdata/kept/<tag>,
// made by `mise run release:keep <tag>`), with every record that server
// served and every trigger's state. This imports each one under the current
// code, the way a restore does (a directory with no row, at boot), and holds
// what the current code serves to what the release served.
//
// The current code may differ on purpose in two ways only. The conversion
// chain rewrites history in place before anything folds it, and appends
// nothing: what each step does to the records a release served is said
// beside it, in keptConversions, written from what the step rewrites and
// never read from what the current code serves, and the release's records
// are taken through those entries before they are compared. And the boot
// appends after the release's head: the shipped-vocabulary upgrade and the
// record seed bring the seeded packages to this tree's versions. So the
// comparison is exact for every record nothing after the head touched, and a
// record an entry after the head touched must equal the release's record,
// converted, with those entries' values applied. Every entry after the head must be the
// boot's own (the system's actor record), and none may move a value an owner, an
// app, a function or an agent set: the upgrade never takes a value from the
// hand that wrote it (decision 0191). Any other difference fails, naming the
// record.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testenv"
	"github.com/geoah/substrate/internal/vocabulary"
)

const keptRoot = "testdata/kept"

type keptFixture struct {
	Tag           string `json:"tag"`
	Repository    string `json:"repository"`
	Password      string `json:"password"`
	Token         string `json:"token"`
	CredentialKey string `json:"credentialKey"`
}

type keptRecords struct {
	Head    int64        `json:"head"`
	Records []keptRecord `json:"records"`
}

// keptRecord is one record the list named: `read` is what a GET of its path
// answered, and `listed` the list's row, kept where the GET answered another
// record (a merge's loser reads as its winner).
type keptRecord struct {
	Path   string         `json:"path"`
	Read   map[string]any `json:"read"`
	Listed map[string]any `json:"listed,omitempty"`
}

type keptTriggers struct {
	Statuses []map[string]any            `json:"statuses"`
	Parked   map[string][]map[string]any `json:"parked"`
}

func TestKeptDirectories(t *testing.T) {
	entries, err := os.ReadDir(keptRoot)
	if err != nil {
		t.Fatalf("read %s: %v", keptRoot, err)
	}
	var tags []string
	for _, e := range entries {
		if e.IsDir() {
			tags = append(tags, e.Name())
		}
	}
	if len(tags) == 0 {
		t.Fatalf("%s keeps no directory: the stored format is held by nothing a release wrote", keptRoot)
	}
	for _, tag := range tags {
		t.Run(tag, func(t *testing.T) {
			k := checkKeptDirectory(t, filepath.Join(keptRoot, tag), keptConversions)
			for _, p := range keptMaterial(t, k.e, k.fixture, filepath.Join(keptRoot, tag)) {
				t.Error(p)
			}
		})
	}
}

// checkKeptDirectory imports the directory dir keeps and holds what the
// current code serves to what the release served, the release's records
// taken through conversions first; opts are the environment's.
func checkKeptDirectory(t *testing.T, dir string, conversions map[int]keptConversion, opts ...testenv.Option) keptImport {
	var fixture keptFixture
	readKept(t, filepath.Join(dir, "fixture.json"), &fixture)
	var want keptRecords
	readKept(t, filepath.Join(dir, "records.json"), &want)
	var wantTriggers keptTriggers
	readKept(t, filepath.Join(dir, "triggers.json"), &wantTriggers)
	if want.Head == 0 || len(want.Records) == 0 {
		t.Fatalf("%s/records.json holds no records", dir)
	}

	// The import writes into the data root, so it gets a copy. A release from
	// before the archive kept its repository directory and one after it keeps
	// its archive (decision 0277); the boot restores from either.
	root := t.TempDir()
	if _, err := os.Stat(filepath.Join(dir, "archive")); err == nil {
		copyKept(t, filepath.Join(dir, "archive"), filepath.Join(root, "archive"))
	} else {
		copyKept(t, filepath.Join(dir, changelogfile.RepositoriesDir), filepath.Join(root, changelogfile.RepositoriesDir))
	}
	refusals := newKeptRefusals()
	e := testenv.Start(t, append([]testenv.Option{
		testenv.WithoutRegistration(), testenv.WithRepository(fixture.Repository),
		testenv.WithDataRoot(root), testenv.WithCredentialKey(fixture.CredentialKey),
		testenv.WithEngineOptions(engine.WithLogger(slog.New(refusals))),
	}, opts...)...)
	e.Token = fixture.Token

	upgraded := keptUpgrade(t, e, want.Head)
	keptAnnotations(t, e, fixture.Repository, want.Head, upgraded)
	got := keptRead(t, e)
	kindVersion := keptKindVersions(got)
	hands := keptHands(got)
	dialect := keptDialect(t, dir, fixture.Repository)
	dropped := keptConvert(&want, conversions, dialect, fixture.Repository)
	keptConvertTriggers(&wantTriggers, conversions, dialect, fixture.Repository)

	var diffs []string
	seen := map[string]bool{}
	for _, rel := range want.Records {
		path := rel.Path
		seen[path] = true
		now, ok := got[path]
		touched := upgraded[path]
		switch {
		case !ok && touched != nil && touched.purged:
			continue
		case dropped[path]:
			diffs = append(diffs, fmt.Sprintf("%s: a conversion step left no record, and it is served now", path))
			continue
		case !ok:
			diffs = append(diffs, fmt.Sprintf("%s: served by %s, missing now", path, fixture.Tag))
			continue
		}
		if (rel.Listed == nil) != (now.Listed == nil) {
			diffs = append(diffs, fmt.Sprintf("%s: a GET of it answered another record under one binary and not the other", path))
			continue
		}
		// The read is the record the GET answered, the listed row the path's
		// own: each is held to what was written after the head to its record.
		type pair struct {
			release, now map[string]any
			touched      *keptTouch
		}
		pairs := []pair{{rel.Read, now.Read, upgraded[keptPath(rel.Read)]}}
		if rel.Listed != nil {
			pairs = append(pairs, pair{rel.Listed, now.Listed, touched})
		}
		for _, p := range pairs {
			if p.touched == nil {
				if d := keptDiff(p.release, p.now); d != "" {
					diffs = append(diffs, fmt.Sprintf("%s: nothing after the release's head wrote it, and it differs: %s", path, d))
				}
				continue
			}
			for _, d := range p.touched.check(p.release, p.now, kindVersion, hands) {
				diffs = append(diffs, fmt.Sprintf("%s: %s", path, d))
			}
		}
	}
	for path, now := range got {
		if seen[path] {
			continue
		}
		touched := upgraded[path]
		if touched == nil || !touched.created {
			diffs = append(diffs, fmt.Sprintf("%s: served now, not by %s, and no entry after the head created it", path, fixture.Tag))
			continue
		}
		for _, d := range touched.check(nil, now.Read, kindVersion, hands) {
			diffs = append(diffs, fmt.Sprintf("%s: %s", path, d))
		}
	}
	// A webhook address the server gives out names the repository by its
	// id (decision 0261); the release gave it out by name.
	ds, err := e.Service.Dataset(context.Background(), fixture.Repository)
	if err != nil {
		t.Fatal(err)
	}
	// A directory from before repository ids takes one at the boot, before
	// its import (decision 0261): it is under the id, and nothing is under
	// the name. Its history moved to the table (decision 0262): what stays
	// is what Postgres does not hold, its blobs and its spool.
	dirByID := filepath.Join(root, "repositories", ds.Repository().ID)
	if _, err := os.Stat(dirByID); err != nil {
		t.Errorf("the imported directory is not under its id %s: %v", ds.Repository().ID, err)
	}
	if changelogfile.HasHistory(dirByID) {
		t.Errorf("the imported directory still holds its history beside the table")
	}
	if _, err := os.Stat(filepath.Join(root, "repositories", fixture.Repository)); !os.IsNotExist(err) {
		t.Errorf("a directory is still named %s: %v", fixture.Repository, err)
	}
	byID := strings.NewReplacer("/webhooks/"+fixture.Repository+"/", "/webhooks/"+ds.Repository().ID+"/")
	diffs = append(diffs, keptTriggerDiffs(t, e, wantTriggers, keptCursors(t, dir, fixture.Repository), byID)...)
	sort.Strings(diffs)
	if len(diffs) > 0 {
		t.Fatalf("the directory %s kept imports under the current code into %d difference(s) no entry after its head explains:\n  %s",
			fixture.Tag, len(diffs), strings.Join(diffs, "\n  "))
	}
	// The release's repository ends on what this binary ships: an upgrade the
	// boot refused leaves every record served as above and the declarations
	// behind, which the comparison cannot see.
	if lines := refusals.lines(); len(lines) > 0 {
		t.Errorf("%s: the boot refused the shipped upgrade:\n  %s", fixture.Tag, strings.Join(lines, "\n  "))
	}
	for _, d := range keptSeededVersions(t, e, "fresh.example.com") {
		t.Errorf("%s: %s", fixture.Tag, d)
	}
	for _, p := range keptMarksWithoutValues(t, e.DSN, ds.Repository().ID) {
		t.Error(p)
	}
	t.Logf("%s: %d records compared, %d of them rewritten by the %d entries the boot appended after head %d",
		fixture.Tag, len(want.Records), len(upgraded), keptAppended(upgraded), want.Head)
	keptSignsIn(t, e, fixture)
	return keptImport{e: e, fixture: fixture, got: got, upgraded: upgraded}
}

// keptRefusals records the shipped upgrades the engine refused, and passes
// every line on to the default handler.
type keptRefusals struct {
	slog.Handler
	mu   *sync.Mutex
	seen *[]string
}

func newKeptRefusals() keptRefusals {
	return keptRefusals{Handler: slog.Default().Handler(), mu: &sync.Mutex{}, seen: &[]string{}}
}

func (r keptRefusals) Handle(ctx context.Context, rec slog.Record) error {
	if strings.Contains(rec.Message, "REFUSED to upgrade") {
		line := rec.Message
		rec.Attrs(func(a slog.Attr) bool {
			line += " " + a.String()
			return true
		})
		r.mu.Lock()
		*r.seen = append(*r.seen, line)
		r.mu.Unlock()
	}
	return r.Handler.Handle(ctx, rec)
}

func (r keptRefusals) WithAttrs(attrs []slog.Attr) slog.Handler {
	r.Handler = r.Handler.WithAttrs(attrs)
	return r
}

func (r keptRefusals) WithGroup(name string) slog.Handler {
	r.Handler = r.Handler.WithGroup(name)
	return r
}

func (r keptRefusals) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(*r.seen)
}

// keptSeededVersions holds the repository e opens to the seeded packages this
// binary ships, each at the version a repository registered now under the
// name fresh holds, and answers each difference.
func keptSeededVersions(t *testing.T, e *testenv.Env, fresh string) []string {
	t.Helper()
	now := e.RegisterUser(fresh, "a password for the fresh repository")
	want, got := keptBuiltinVersions(t, now), keptBuiltinVersions(t, e)
	if len(want) == 0 {
		t.Fatal("a repository registered now holds no seeded package: the check reads nothing")
	}
	var out []string
	for _, id := range slices.Sorted(maps.Keys(want)) {
		if got[id] != want[id] {
			out = append(out, fmt.Sprintf("seeded package %s is at version %q, and this binary ships %q", id, got[id], want[id]))
		}
	}
	return out
}

// keptBuiltinVersions is each seeded package's stored version, by the
// package record's id.
func keptBuiltinVersions(t *testing.T, e *testenv.Env) map[string]string {
	t.Helper()
	q := url.Values{"filter": {`{"kinds":["substrate.reamde.dev/core/package"]}`}, "first": {"500"}}
	var page struct {
		Records []struct {
			ID         string         `json:"id"`
			Properties map[string]any `json:"properties"`
		} `json:"records"`
	}
	e.MustJSON(http.MethodGet, "/api/v1/records?"+q.Encode(), nil, &page)
	out := map[string]string{}
	for _, r := range page.Records {
		if r.Properties["source"] == "builtin" {
			out[r.ID] = fmt.Sprint(r.Properties["version"])
		}
	}
	return out
}

// keptSignsIn holds the person of a release's repository to the sign-in they
// had: a directory from before identities takes one at the boot, named after
// the repository and holding the password the release kept (decision 0274),
// and the token it mints reaches that repository and names that person.
func keptSignsIn(t *testing.T, e *testenv.Env, fixture keptFixture) {
	t.Helper()
	status, raw := e.Login(fixture.Repository, fixture.Password, "")
	if status != http.StatusCreated {
		t.Fatalf("%s: the release's person does not sign in with its password: %d %s", fixture.Tag, status, raw)
	}
	var minted substrate.MintedToken
	if err := json.Unmarshal(raw, &minted); err != nil {
		t.Fatal(err)
	}
	if minted.Repository != fixture.Repository || minted.Identity != fixture.Repository {
		t.Fatalf("%s: the sign-in reached %q as %q, want %q as its own name", fixture.Tag, minted.Repository, minted.Identity, fixture.Repository)
	}
	if status, raw, _ := e.DoRaw(http.MethodGet, "/api/v1/substrate.reamde.dev/core/credential/self", nil, nil); status == http.StatusOK {
		t.Fatalf("%s: the credential record is still served after the boot: %d %s", fixture.Tag, status, raw)
	}
}

// keptTouch is what the entries after the release's head did to one record,
// in seq order.
type keptTouch struct {
	entries []keptEntry
	created bool
	purged  bool
	// annotations is each annotation effect of those entries, which the
	// change feed does not carry, read from the directory's own lines.
	annotations []keptAnnotation
}

type keptAnnotation struct {
	key   string
	value any // nil clears it
}

type keptEntry struct {
	seq      int64
	ts       time.Time
	actor    string
	affected substrate.AffectedRecord
	// written is the properties the entry's payload says it wrote, each of
	// which takes a mark; a state moves without one.
	written map[string]bool
}

func keptAppended(m map[string]*keptTouch) int {
	seqs := map[int64]bool{}
	for _, t := range m {
		for _, e := range t.entries {
			seqs[e.seq] = true
		}
	}
	return len(seqs)
}

// The values a shipped write may not take: a pinned one, and one set by any
// hand but the system and the packages (decision 0191). A mark names its
// hand by its actor record (decision 0270), so hands holds each record's
// type.
func ownersMark(m map[string]any, hands map[string]string) bool {
	if m["pinned"] == true {
		return true
	}
	setBy, _ := m["setBy"].(map[string]any)
	ref, _ := setBy[vocabulary.ReferenceValueKey].(string)
	if ref == "" {
		return false
	}
	switch hands[ref[strings.LastIndex(ref, "/")+1:]] {
	case "system", "package":
		return false
	}
	return true
}

// keptHands is the type of every actor record the current code serves, by
// id.
func keptHands(got map[string]keptRecord) map[string]string {
	out := map[string]string{}
	for _, rec := range got {
		if rec.Read["kind"] != vocabulary.CoreKind(vocabulary.DocActor) {
			continue
		}
		props, _ := rec.Read["properties"].(map[string]any)
		out[fmt.Sprint(rec.Read["id"])], _ = props["type"].(string)
	}
	return out
}

// check holds the record the current code serves to the release's (nil when
// the release had none) with the entries applied, nothing taken from what the
// current code serves: each moved property takes its `after`, and each one
// the entry wrote (its payload's `properties`; a state moves without one) the
// mark the boot's direct write leaves, set by the system, not pinned and
// derived from nothing, at the entry's time; the version and the times follow the last entry that moved the
// version; the kind version is the one the boot's write validated against,
// the kind's as the current code declares it (kindVersion); and everything
// else is the release's.
func (k *keptTouch) check(release, now map[string]any, kindVersion func(kind string) json.Number, hands map[string]string) []string {
	var out []string
	var want map[string]any
	if release != nil {
		want = keptClone(release)
	} else {
		first := k.entries[0].affected
		want = map[string]any{
			"id": first.ID, "kind": first.Kind,
			"properties": map[string]any{}, "labels": map[string]any{}, "marks": map[string]any{},
		}
	}
	props, _ := want["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
		want["properties"] = props
	}
	marks, _ := want["marks"].(map[string]any)
	if marks == nil {
		marks = map[string]any{}
	}
	wrote := false
	for _, e := range k.entries {
		if e.actor != vocabulary.ActorID(substrate.ActorSystem) {
			out = append(out, fmt.Sprintf("seq %d after the head was written by %q, not by the boot", e.seq, e.actor))
		}
		for _, p := range e.affected.Properties {
			for _, name := range []string{p.RenamedFrom, p.Name} {
				if name == "" {
					continue
				}
				if m, ok := marks[name].(map[string]any); ok && ownersMark(m, hands) {
					out = append(out, fmt.Sprintf("seq %d moved %s, which %v set", e.seq, name, m["setBy"]))
				}
			}
			if p.RenamedFrom != "" {
				delete(props, p.RenamedFrom)
				delete(marks, p.RenamedFrom)
			}
			if p.After == nil {
				delete(props, p.Name)
				delete(marks, p.Name)
				continue
			}
			props[p.Name] = p.After
			wrote = true
			if !e.written[p.Name] {
				continue
			}
			m := map[string]any{
				"setBy": map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocActor), e.actor)},
				"setAt": e.ts.UTC().Format(time.RFC3339Nano),
			}
			if old, ok := marks[p.Name].(map[string]any); ok && old["alternatives"] != nil {
				m["alternatives"] = old["alternatives"]
			}
			marks[p.Name] = m
		}
		if e.affected.Version > 0 {
			want["version"] = json.Number(fmt.Sprint(e.affected.Version))
			want["updatedAt"] = e.ts.UTC().Format(time.RFC3339Nano)
			if e.affected.Deleted {
				want["deletedAt"] = e.ts.UTC().Format(time.RFC3339Nano)
			}
			if release == nil && k.created {
				want["createdAt"] = keptFirstTS(k)
			}
		}
	}
	if len(marks) > 0 {
		want["marks"] = marks
	} else {
		delete(want, "marks")
	}
	if len(k.annotations) > 0 {
		ann, _ := want["annotations"].(map[string]any)
		if ann == nil {
			ann = map[string]any{}
		}
		for _, a := range k.annotations {
			if a.value == nil {
				delete(ann, a.key)
			} else {
				ann[a.key] = a.value
			}
		}
		if len(ann) > 0 {
			want["annotations"] = ann
		} else {
			delete(want, "annotations")
		}
	}
	if wrote {
		want["kindVersion"] = kindVersion(fmt.Sprint(want["kind"]))
		// The title is derived storage (decision 0016), a function of the
		// properties held above and the kind's template, and a change's
		// values do not carry it.
		if title, ok := now["properties"].(map[string]any)["title"]; ok {
			props["title"] = title
		}
	}
	if d := keptDiff(want, now); d != "" {
		out = append(out, "differs from the release's record with the boot's entries applied: "+d)
	}
	return out
}

func keptFirstTS(k *keptTouch) string {
	return k.entries[0].ts.UTC().Format(time.RFC3339Nano)
}

// keptKindVersions is each kind's version as the current code declares it:
// the `version` of its `core/kind` record, which is the kind's own pin, else
// its package's.
func keptKindVersions(got map[string]keptRecord) func(kind string) json.Number {
	versions := map[string]json.Number{}
	for _, r := range got {
		if r.Read["kind"] != "substrate.reamde.dev/core/kind" {
			continue
		}
		props, _ := r.Read["properties"].(map[string]any)
		if v, ok := props["version"].(json.Number); ok {
			versions[fmt.Sprint(r.Read["id"])] = v
		}
	}
	return func(kind string) json.Number { return versions[kind] }
}

// keptUpgrade reads every entry the current code appended after the
// release's head, with each property's before and after, and groups them by
// the record they moved.
func keptUpgrade(t *testing.T, e *testenv.Env, head int64) map[string]*keptTouch {
	t.Helper()
	var page struct {
		Head       int64  `json:"head"`
		Generation string `json:"generation"`
	}
	e.MustJSON(http.MethodGet, "/api/v1/records?first=1", nil, &page)
	if page.Head < head {
		t.Fatalf("the import's head is %d, below the %d the release served: entries were lost", page.Head, head)
	}
	out := map[string]*keptTouch{}
	for from := head; from < page.Head; {
		q := url.Values{"from": {fmt.Sprint(from)}, "generation": {page.Generation}, "values": {"1"}}
		status, raw := e.Do(http.MethodGet, "/api/v1/changes?"+q.Encode(), nil)
		if status != http.StatusOK {
			t.Fatalf("read the changes after %d: %d %s", from, status, raw)
		}
		sc := bufio.NewScanner(bytes.NewReader(raw))
		sc.Buffer(make([]byte, 1<<20), 64<<20)
		advanced := false
		for sc.Scan() {
			var row substrate.ChangeRow
			if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
				t.Fatalf("decode a change row: %v (%s)", err, sc.Bytes())
			}
			if row.Seq == 0 {
				continue // the bookmark frame
			}
			from, advanced = row.Seq, true
			for _, a := range row.Affected {
				path := a.Kind + "/" + a.ID
				k := out[path]
				if k == nil {
					k = &keptTouch{}
					out[path] = k
				}
				if created, _ := row.Payload["created"].(bool); created && row.RecordID == a.ID && row.Kind == a.Kind {
					k.created = k.created || len(k.entries) == 0
				}
				if a.Deleted && a.Version == 0 {
					k.purged = true
				}
				written := map[string]bool{}
				if names, ok := row.Payload["properties"].([]any); ok && row.RecordID == a.ID && row.Kind == a.Kind {
					for _, n := range names {
						written[fmt.Sprint(n)] = true
					}
				}
				k.entries = append(k.entries, keptEntry{seq: row.Seq, ts: row.TS, actor: row.Actor, affected: a, written: written})
			}
		}
		if err := sc.Err(); err != nil {
			t.Fatalf("read the changes after %d: %v", from, err)
		}
		if !advanced {
			t.Fatalf("the changes after %d stopped short of the head %d", from, page.Head)
		}
	}
	return out
}

// keptAnnotations adds to each record the annotation effects of the entries
// after the release's head, read from the history the import left in the
// changelog table (decision 0262): an effect the change feed leaves out, and
// one the boot writes (repository migration 7's mark).
func keptAnnotations(t *testing.T, e *testenv.Env, repository string, head int64, upgraded map[string]*keptTouch) {
	t.Helper()
	db, err := engine.OpenScopedDB(e.DSN, repository, engine.RoleApp)
	if err != nil {
		t.Fatalf("open the repository's pool: %v", err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.QueryContext(context.Background(),
		`SELECT seq, payload::text FROM changelog WHERE seq > $1 ORDER BY seq`, head)
	if err != nil {
		t.Fatalf("read the changelog after %d: %v", head, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var seq int64
		var payload []byte
		if err := rows.Scan(&seq, &payload); err != nil {
			t.Fatal(err)
		}
		var p struct {
			Fold []struct {
				Kind  string          `json:"kind"`
				Ref   string          `json:"ref"`
				ID    string          `json:"id"`
				Key   string          `json:"key"`
				Value json.RawMessage `json:"value"`
			} `json:"fold"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			t.Fatalf("seq %d: %v", seq, err)
		}
		for _, f := range p.Fold {
			if f.Kind != "annotation" {
				continue
			}
			a := keptAnnotation{key: f.Key}
			if len(f.Value) > 0 {
				if err := json.Unmarshal(f.Value, &a.value); err != nil {
					t.Fatalf("seq %d: %v", seq, err)
				}
			}
			path := f.Ref + "/" + f.ID
			if upgraded[path] == nil {
				upgraded[path] = &keptTouch{}
			}
			upgraded[path].annotations = append(upgraded[path].annotations, a)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the changelog after %d: %v", head, err)
	}
}

// keptRead reads every record the current code serves, live and tombstoned,
// the way the release's were read: one GET of each path the list names, and
// the list's row where the GET answered another record.
func keptRead(t *testing.T, e *testenv.Env) map[string]keptRecord {
	t.Helper()
	out := map[string]keptRecord{}
	for _, filter := range []string{"{}", `{"deleted":true}`} {
		after := ""
		for {
			q := url.Values{"filter": {filter}, "first": {"500"}, "withAnnotations": {"1"}}
			if after != "" {
				q.Set("after", after)
			}
			var page struct {
				Records []json.RawMessage `json:"records"`
				Cursor  string            `json:"cursor"`
			}
			e.MustJSON(http.MethodGet, "/api/v1/records?"+q.Encode(), nil, &page)
			for _, raw := range page.Records {
				row := keptDecode(t, raw)
				kind, id := fmt.Sprint(row["kind"]), fmt.Sprint(row["id"])
				status, body := e.Do(http.MethodGet, "/api/v1/"+kind+"/"+url.PathEscape(id), nil)
				if status != http.StatusOK {
					t.Fatalf("GET %s/%s: %d %s", kind, id, status, body)
				}
				rec := keptRecord{Path: kind + "/" + id, Read: keptDecode(t, body)}
				if fmt.Sprint(rec.Read["id"]) != id {
					rec.Listed = row
				}
				out[rec.Path] = rec
			}
			if page.Cursor == "" {
				break
			}
			after = page.Cursor
		}
	}
	return out
}

// keptCursors is each record trigger's acknowledged cursor as the kept
// history holds it: the seq of its last `cursor` effect. A trigger's served
// cursor may stand past it, at a scan position the server keeps beside the
// ledger and an import does not carry (fold.go, "THE WRITES OUTSIDE THE FOLD
// PATH").
func keptCursors(t *testing.T, dir, repository string) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	err := keptWalk(dir, repository, func(e changelogfile.Entry) error {
		var p struct {
			Fold []struct {
				Kind string      `json:"kind"`
				ID   string      `json:"id"`
				Seq  json.Number `json:"seq"`
			} `json:"fold"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		for _, f := range p.Fold {
			if f.Kind != "cursor" {
				continue
			}
			n, err := f.Seq.Float64()
			if err != nil {
				return err
			}
			out[f.ID] = int64(n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the kept changelog: %v", err)
	}
	return out
}

// keptWalk reads the kept history in seq order from whichever layout the
// release kept: the pieces its archive's manifest chains, or the segments of
// a directory from before the archive.
func keptWalk(dir, repository string, fn func(changelogfile.Entry) error) error {
	repos := filepath.Join(dir, "archive", changelogfile.RepositoriesDir)
	ids, err := os.ReadDir(repos)
	if os.IsNotExist(err) {
		log, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(filepath.Join(dir, changelogfile.RepositoriesDir, repository)))
		if err != nil {
			return err
		}
		return log.Walk(fn)
	}
	if err != nil {
		return err
	}
	if len(ids) != 1 {
		return fmt.Errorf("the kept archive holds %d repositories, want 1", len(ids))
	}
	repoDir := filepath.Join(repos, ids[0].Name())
	m, _, err := changelogfile.ReadArchiveManifestDir(repoDir)
	if err != nil {
		return err
	}
	if m.Authority != repository {
		return fmt.Errorf("the kept archive is %s's, want %s's", m.Authority, repository)
	}
	chain, err := m.Chain()
	if err != nil {
		return err
	}
	for _, p := range chain {
		f, err := os.Open(filepath.Join(repoDir, m.Archive, filepath.FromSlash(p.Key)))
		if err != nil {
			return err
		}
		err = changelogfile.ReadPiece(f, p.First, p.Last, func(e changelogfile.Entry, _ []byte, _ [32]byte) error { return fn(e) })
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// keptTriggerDiffs holds each trigger's state to the release's: what the
// delivery ledger folded (the acknowledged cursor, the last fire, the parked
// failures), not what the server keeps in memory or the clock and the head
// moved since (decision 0064; TriggerStatus.LastPassAt).
func keptTriggerDiffs(t *testing.T, e *testenv.Env, want keptTriggers, acked map[string]int64, address *strings.Replacer) []string {
	t.Helper()
	unstored := []string{"head", "lag", "lastPassAt", "lastDeliveredAt", "health", "pending", "cursor"}
	var got struct {
		Items []map[string]any `json:"items"`
	}
	e.MustJSON(http.MethodGet, "/api/v1/substrate.reamde.dev/core/trigger/status", nil, &got)
	byID := map[string]map[string]any{}
	for _, s := range got.Items {
		byID[fmt.Sprint(s["id"])] = s
	}
	var out []string
	for _, w := range want.Statuses {
		id := fmt.Sprint(w["id"])
		g, ok := byID[id]
		if !ok {
			out = append(out, fmt.Sprintf("trigger %s: served by the release, missing now", id))
			continue
		}
		delete(byID, id)
		// Only a record trigger serves a cursor.
		if seq, ok := acked[id]; ok && w["kind"] == "record" {
			released := keptInt(w["cursor"])
			if released < seq || keptInt(g["cursor"]) != seq {
				out = append(out, fmt.Sprintf("trigger %s: cursor %d now and %d served by the release, the history acknowledging %d",
					id, keptInt(g["cursor"]), released, seq))
			}
		}
		w, g = maps.Clone(w), maps.Clone(g)
		if path, ok := w["webhookPath"].(string); ok {
			w["webhookPath"] = address.Replace(path)
		}
		for _, k := range unstored {
			delete(w, k)
			delete(g, k)
		}
		if d := keptDiff(w, g); d != "" {
			out = append(out, fmt.Sprintf("trigger %s status: %s", id, d))
		}
		var parked struct {
			Items []map[string]any `json:"items"`
		}
		e.MustJSON(http.MethodGet, "/api/v1/substrate.reamde.dev/core/trigger/"+url.PathEscape(id)+"/parked", nil, &parked)
		if d := keptDiff(map[string]any{"parked": want.Parked[id]}, map[string]any{"parked": parked.Items}); d != "" {
			out = append(out, fmt.Sprintf("trigger %s parked: %s", id, d))
		}
	}
	for id := range byID {
		out = append(out, fmt.Sprintf("trigger %s: served now and not by the release", id))
	}
	return out
}

func keptInt(v any) int64 {
	n, _ := keptNormal(v).(json.Number)
	i, _ := n.Int64()
	return i
}

// keptDiff names the first path at which two decoded records differ, or "".
func keptDiff(want, got any) string {
	want, got = keptNormal(want), keptNormal(got)
	if reflect.DeepEqual(want, got) {
		return ""
	}
	return keptFirstDiff("", want, got)
}

func keptFirstDiff(at string, want, got any) string {
	wm, wok := want.(map[string]any)
	gm, gok := got.(map[string]any)
	if wok && gok {
		keys := slices.Sorted(maps.Keys(wm))
		for k := range gm {
			if _, ok := wm[k]; !ok {
				keys = append(keys, k)
			}
		}
		for _, k := range keys {
			if !reflect.DeepEqual(wm[k], gm[k]) {
				return keptFirstDiff(at+"."+k, wm[k], gm[k])
			}
		}
	}
	w, _ := json.Marshal(want)
	g, _ := json.Marshal(got)
	return fmt.Sprintf("at %s: release %s, now %s", strings.TrimPrefix(at, "."), w, g)
}

// keptNormal re-decodes a value so a json.Number, a float and a string of
// the same text compare as the wire spells them.
func keptNormal(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return v
	}
	return out
}

func keptClone(m map[string]any) map[string]any {
	out, _ := keptNormal(m).(map[string]any)
	return out
}

func keptPath(rec map[string]any) string {
	return fmt.Sprint(rec["kind"]) + "/" + fmt.Sprint(rec["id"])
}

func keptDecode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

func readKept(t *testing.T, path string, into any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(into); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

// copyKept copies a kept tree byte for byte, with the modes a server's own
// directory has.
func copyKept(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatalf("copy %s: %v", src, err)
	}
}

// The comparison of a record the boot rewrote takes nothing from what the
// current code serves: a mark pinned or set at another time, another kind
// version, a value no entry wrote, a write that is not the boot's, or one
// that moved a value an owner set, each fails. It needs no database.
func TestKeptCheckRefusesWhatTheEntriesDoNotSay(t *testing.T) {
	t.Parallel()
	const t0, t1 = "2026-11-13T00:00:00Z", "2026-11-13T01:00:00Z"
	system, person := vocabulary.ActorID(substrate.ActorSystem), vocabulary.ActorID(substrate.ActorPerson)
	hands := map[string]string{system: "system", person: "person"}
	link := func(id string) string {
		return `{"ref":"` + vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocActor), id) + `"}`
	}
	ts, err := time.Parse(time.RFC3339, t1)
	if err != nil {
		t.Fatal(err)
	}
	release := keptDecode(t, []byte(`{"id":"x","kind":"a.example.com/p/k","labels":{},"version":1,"kindVersion":1,
		"createdAt":"`+t0+`","updatedAt":"`+t0+`","properties":{"shipped":"1","owned":"2"},
		"marks":{"shipped":{"setBy":`+link(system)+`,"setAt":"`+t0+`"},
		         "owned":{"pinned":true,"setBy":`+link(person)+`,"setAt":"`+t0+`"}}}`))
	touch := func(actor, property string) *keptTouch {
		return &keptTouch{entries: []keptEntry{{seq: 10, ts: ts, actor: actor, written: map[string]bool{property: true}, affected: substrate.AffectedRecord{
			Kind: "a.example.com/p/k", ID: "x", Version: 2,
			Properties: []substrate.PropertyChange{{Name: property, Before: "1", After: "3"}},
		}}}}
	}
	kindVersion := func(string) json.Number { return "4" }
	now := func(edit func(m map[string]any)) map[string]any {
		m := keptClone(release)
		m["version"], m["updatedAt"], m["kindVersion"] = json.Number("2"), t1, json.Number("4")
		m["properties"].(map[string]any)["shipped"] = "3"
		m["marks"].(map[string]any)["shipped"] = keptDecode(t, []byte(`{"setBy":`+link(system)+`,"setAt":"`+t1+`"}`))
		if edit != nil {
			edit(m)
		}
		return m
	}
	if d := touch(system, "shipped").check(release, now(nil), kindVersion, hands); len(d) != 0 {
		t.Fatalf("the record the entry describes was refused: %v", d)
	}
	marked := func() *keptTouch {
		k := touch(system, "shipped")
		k.annotations = []keptAnnotation{{key: "substrate/mark", value: float64(2)}}
		return k
	}
	mark := func(v string) func(m map[string]any) {
		return func(m map[string]any) { m["annotations"] = map[string]any{"substrate/mark": json.Number(v)} }
	}
	if d := marked().check(release, now(mark("2")), kindVersion, hands); len(d) != 0 {
		t.Fatalf("the annotation the entry set was refused: %v", d)
	}
	meta := func(m map[string]any) map[string]any {
		return m["marks"].(map[string]any)["shipped"].(map[string]any)
	}
	for name, c := range map[string]struct {
		touch *keptTouch
		now   map[string]any
	}{
		"a mark pinned":                  {touch(system, "shipped"), now(func(m map[string]any) { meta(m)["pinned"] = true })},
		"a mark set elsewhen":            {touch(system, "shipped"), now(func(m map[string]any) { meta(m)["setAt"] = t0 })},
		"another hand":                   {touch(system, "shipped"), now(func(m map[string]any) { meta(m)["setBy"] = keptDecode(t, []byte(link(person))) })},
		"another kind version":           {touch(system, "shipped"), now(func(m map[string]any) { m["kindVersion"] = json.Number("1") })},
		"a value no entry wrote":         {touch(system, "shipped"), now(func(m map[string]any) { m["properties"].(map[string]any)["owned"] = "9" })},
		"an annotation no entry wrote":   {touch(system, "shipped"), now(mark("2"))},
		"another annotation value":       {marked(), now(mark("3"))},
		"a write that is not the boot's": {touch(person, "shipped"), now(nil)},
		"a write over the owner's value": {touch(system, "owned"), now(nil)},
	} {
		if d := c.touch.check(release, c.now, kindVersion, hands); len(d) == 0 {
			t.Errorf("%s compared equal", name)
		}
	}
}
