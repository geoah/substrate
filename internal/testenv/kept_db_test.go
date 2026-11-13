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
// boot's own (the `substrate` actor), and none may move a value an owner, an
// app, a function or an agent set: the upgrade never takes a value from the
// hand that wrote it (decision 0191). Any other difference fails, naming the
// record.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testenv"
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

	// The import writes into the data root, so it gets a copy.
	root := t.TempDir()
	copyKept(t, filepath.Join(dir, "repositories"), filepath.Join(root, "repositories"))
	e := testenv.Start(t, append([]testenv.Option{
		testenv.WithoutRegistration(), testenv.WithRepository(fixture.Repository),
		testenv.WithDataRoot(root), testenv.WithCredentialKey(fixture.CredentialKey),
	}, opts...)...)
	e.Token = fixture.Token

	upgraded := keptUpgrade(t, e, want.Head)
	got := keptRead(t, e)
	kindVersion := keptKindVersions(got)
	dropped := keptConvert(&want, conversions, keptDialect(t, dir, fixture.Repository))

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
			for _, d := range p.touched.check(p.release, p.now, kindVersion) {
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
		for _, d := range touched.check(nil, now.Read, kindVersion) {
			diffs = append(diffs, fmt.Sprintf("%s: %s", path, d))
		}
	}
	diffs = append(diffs, keptTriggerDiffs(t, e, wantTriggers, keptCursors(t, filepath.Join(dir, "repositories", fixture.Repository)))...)
	sort.Strings(diffs)
	if len(diffs) > 0 {
		t.Fatalf("the directory %s kept imports under the current code into %d difference(s) no entry after its head explains:\n  %s",
			fixture.Tag, len(diffs), strings.Join(diffs, "\n  "))
	}
	t.Logf("%s: %d records compared, %d of them rewritten by the %d entries the boot appended after head %d",
		fixture.Tag, len(want.Records), len(upgraded), keptAppended(upgraded), want.Head)
	return keptImport{e: e, fixture: fixture, got: got, upgraded: upgraded}
}

// keptTouch is what the entries after the release's head did to one record,
// in seq order.
type keptTouch struct {
	entries []keptEntry
	created bool
	purged  bool
}

type keptEntry struct {
	seq      int64
	ts       time.Time
	actor    string
	affected substrate.AffectedRecord
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

// The hands a shipped write may not take a value from: everything but the
// substrate itself and the packages it ships (decision 0191).
func ownersHand(manager string) bool {
	return manager != string(substrate.ActorSystem) && !strings.HasPrefix(manager, "bundle:")
}

// check holds the record the current code serves to the release's (nil when
// the release had none) with the entries applied, nothing taken from what the
// current code serves: each moved property takes its `after`, and its
// manager row the entry's actor at the machine tier, stamped with the entry's
// time; the version and the times follow the last entry that moved the
// version; the kind version is the one the boot's write validated against,
// the kind's as the current code declares it (kindVersion); and everything
// else is the release's.
func (k *keptTouch) check(release, now map[string]any, kindVersion func(kind string) json.Number) []string {
	var out []string
	want := map[string]any{"properties": map[string]any{}, "propertyMeta": map[string]any{}}
	if release != nil {
		want = keptClone(release)
	}
	props, _ := want["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
		want["properties"] = props
	}
	meta, _ := want["propertyMeta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	wrote := false
	for _, e := range k.entries {
		if e.actor != string(substrate.ActorSystem) {
			out = append(out, fmt.Sprintf("seq %d after the head was written by %q, not by the boot", e.seq, e.actor))
		}
		for _, p := range e.affected.Properties {
			for _, name := range []string{p.RenamedFrom, p.Name} {
				if name == "" {
					continue
				}
				if m, ok := meta[name].(map[string]any); ok && ownersHand(fmt.Sprint(m["manager"])) {
					out = append(out, fmt.Sprintf("seq %d moved %s, which %v set", e.seq, name, m["manager"]))
				}
			}
			if p.RenamedFrom != "" {
				delete(props, p.RenamedFrom)
				delete(meta, p.RenamedFrom)
			}
			if p.After == nil {
				delete(props, p.Name)
				delete(meta, p.Name)
				continue
			}
			props[p.Name] = p.After
			row, _ := meta[p.Name].(map[string]any)
			if row == nil {
				row = map[string]any{}
			}
			row["manager"] = e.actor
			row["tier"] = string(substrate.TierMachine)
			row["updatedAt"] = e.ts.UTC().Format(time.RFC3339Nano)
			meta[p.Name] = row
			wrote = true
		}
		if e.affected.Version > 0 {
			want["version"] = json.Number(fmt.Sprint(e.affected.Version))
			want["updatedAt"] = e.ts.UTC().Format(time.RFC3339Nano)
			if release == nil && k.created {
				want["createdAt"] = keptFirstTS(k)
			}
		}
	}
	if len(meta) > 0 {
		want["propertyMeta"] = meta
	} else {
		delete(want, "propertyMeta")
	}
	if wrote {
		want["kindVersion"] = kindVersion(fmt.Sprint(want["kind"]))
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
				k.entries = append(k.entries, keptEntry{seq: row.Seq, ts: row.TS, actor: string(row.Actor), affected: a})
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
func keptCursors(t *testing.T, repoDir string) map[string]int64 {
	t.Helper()
	log, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(repoDir))
	if err != nil {
		t.Fatalf("open the kept changelog: %v", err)
	}
	out := map[string]int64{}
	err = log.Walk(func(e changelogfile.Entry) error {
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

// keptTriggerDiffs holds each trigger's state to the release's: what the
// delivery ledger folded (the acknowledged cursor, the last fire, the parked
// failures), not what the server keeps in memory or the clock and the head
// moved since (decision 0064; TriggerStatus.LastPassAt).
func keptTriggerDiffs(t *testing.T, e *testenv.Env, want keptTriggers, acked map[string]int64) []string {
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
// current code serves: a manager row at another tier or time, another kind
// version, a value no entry wrote, a write that is not the boot's, or one
// that moved a value an owner set, each fails. It needs no database.
func TestKeptCheckRefusesWhatTheEntriesDoNotSay(t *testing.T) {
	t.Parallel()
	const t0, t1 = "2026-11-13T00:00:00Z", "2026-11-13T01:00:00Z"
	ts, err := time.Parse(time.RFC3339, t1)
	if err != nil {
		t.Fatal(err)
	}
	release := keptDecode(t, []byte(`{"id":"x","kind":"a.example.com/p/k","labels":{},"version":1,"kindVersion":1,
		"createdAt":"`+t0+`","updatedAt":"`+t0+`","properties":{"shipped":"1","owned":"2"},
		"propertyMeta":{"shipped":{"manager":"substrate","tier":"machine","updatedAt":"`+t0+`"},
		                "owned":{"manager":"api","tier":"owner","updatedAt":"`+t0+`"}}}`))
	touch := func(actor, property string) *keptTouch {
		return &keptTouch{entries: []keptEntry{{seq: 10, ts: ts, actor: actor, affected: substrate.AffectedRecord{
			Kind: "a.example.com/p/k", ID: "x", Version: 2,
			Properties: []substrate.PropertyChange{{Name: property, Before: "1", After: "3"}},
		}}}}
	}
	kindVersion := func(string) json.Number { return "4" }
	now := func(edit func(m map[string]any)) map[string]any {
		m := keptClone(release)
		m["version"], m["updatedAt"], m["kindVersion"] = json.Number("2"), t1, json.Number("4")
		m["properties"].(map[string]any)["shipped"] = "3"
		m["propertyMeta"].(map[string]any)["shipped"] = map[string]any{"manager": "substrate", "tier": "machine", "updatedAt": t1}
		if edit != nil {
			edit(m)
		}
		return m
	}
	if d := touch("substrate", "shipped").check(release, now(nil), kindVersion); len(d) != 0 {
		t.Fatalf("the record the entry describes was refused: %v", d)
	}
	meta := func(m map[string]any) map[string]any {
		return m["propertyMeta"].(map[string]any)["shipped"].(map[string]any)
	}
	for name, c := range map[string]struct {
		touch *keptTouch
		now   map[string]any
	}{
		"a manager row at another tier":  {touch("substrate", "shipped"), now(func(m map[string]any) { meta(m)["tier"] = "owner" })},
		"a manager row stamped elsewhen": {touch("substrate", "shipped"), now(func(m map[string]any) { meta(m)["updatedAt"] = t0 })},
		"another manager":                {touch("substrate", "shipped"), now(func(m map[string]any) { meta(m)["manager"] = "api" })},
		"another kind version":           {touch("substrate", "shipped"), now(func(m map[string]any) { m["kindVersion"] = json.Number("1") })},
		"a value no entry wrote":         {touch("substrate", "shipped"), now(func(m map[string]any) { m["properties"].(map[string]any)["owned"] = "9" })},
		"a write that is not the boot's": {touch("api", "shipped"), now(nil)},
		"a write over the owner's value": {touch("substrate", "owned"), now(nil)},
	} {
		if d := c.touch.check(release, c.now, kindVersion); len(d) == 0 {
			t.Errorf("%s compared equal", name)
		}
	}
}
