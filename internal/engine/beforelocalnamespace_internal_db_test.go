package engine

// A REPOSITORY AS MAIN HELD IT BEFORE THE LOCAL NAMESPACE (decision record
// 0259). testdata/before-local-namespace is what main's own code wrote over
// the kept v0.123.1 directory before this step: history at dialect 5, an
// actor record under every hand that carried the repository's name (its
// packages, the functions of `auto` and `lab`, the lab's agent, and `gone`,
// a function deleted since, whose hand is disabled), the actor fill's mark
// set, a function's write linking its hand by the old id, and
// own kinds in a declaration, a reference, a setting's id and a retention
// override. actors.json is the actor records main held. Restored from its
// archive and started under the whole chain, it converts once.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const beforeLocalNamespace = "testdata/before-local-namespace"

// openBeforeLocalNamespace restores the fixture into a database of its own
// and opens its repository; reopen starts the same database and data root
// again.
func openBeforeLocalNamespace(t *testing.T) (ds *dataset, name, root string, reopen func() *dataset) {
	t.Helper()
	return openConverted(t, beforeLocalNamespace, "data", "")
}

// openConverted copies dir's from (its data root, or the part of one) into a
// data root of its own at to, and starts it over a database of its own, as
// openBeforeLocalNamespace does.
func openConverted(t *testing.T, dir, from, to string) (ds *dataset, name, root string, reopen func() *dataset) {
	t.Helper()
	ctx := context.Background()
	raw, err := os.ReadFile(filepath.Join(dir, "fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Repository    string `json:"repository"`
		CredentialKey string `json:"credentialKey"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	root = t.TempDir()
	if err := copyTree(filepath.Join(dir, from), filepath.Join(root, to)); err != nil {
		t.Fatal(err)
	}
	dsn := MigratedDSN(t)
	var svc substrate.Service
	open := func() *dataset {
		t.Helper()
		if svc != nil {
			_ = svc.Close()
		}
		svc, err = OpenForTest(t, ctx, dsn, WithKindsFS(SeedKinds), WithDataRoot(root), WithCredentialKey(fixture.CredentialKey))
		if err != nil {
			t.Fatalf("start over the fixture: %v", err)
		}
		d, err := svc.Dataset(ctx, fixture.Repository)
		if err != nil {
			t.Fatalf("open %s: %v", fixture.Repository, err)
		}
		return d.(*dataset)
	}
	t.Cleanup(func() {
		if svc != nil {
			_ = svc.Close()
		}
	})
	return open(), fixture.Repository, root, open
}

var (
	actorPathRe = regexp.MustCompile(`substrate\.reamde\.dev/core/actor/([0-9a-z]{26})`)
	actorIDRe   = regexp.MustCompile(`"(?:setBy|onBehalfOf|triggeredBy|actor)":\s?"([0-9a-z]{26})"`)
)

// linkedActors is every actor record id the repository's history, fold and
// marks link: a line's actor and on-behalf-of, an id a line's payload holds
// at a mark, a trigger's cause or an effect, and every reference to an
// actor record in a line or a row (a `heldTo` among them).
func linkedActors(t *testing.T, ds *dataset) map[string]string {
	t.Helper()
	ctx := context.Background()
	out := map[string]string{}
	scan := func(q, where string, ids func(string) []string) {
		t.Helper()
		rows, err := ds.db.QueryContext(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			for _, id := range ids(s) {
				if _, seen := out[id]; !seen {
					out[id] = where
				}
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	}
	one := func(s string) []string {
		if s == "" {
			return nil
		}
		return []string{s}
	}
	in := func(s string) []string {
		var ids []string
		for _, m := range actorPathRe.FindAllStringSubmatch(s, -1) {
			ids = append(ids, m[1])
		}
		for _, m := range actorIDRe.FindAllStringSubmatch(s, -1) {
			ids = append(ids, m[1])
		}
		return ids
	}
	scan(`SELECT actor FROM changelog`, "a line's actor", one)
	scan(`SELECT coalesce(on_behalf_of, '') FROM changelog`, "a line's on-behalf-of", one)
	scan(`SELECT payload::text FROM changelog`, "a line's payload", in)
	scan(`SELECT set_by FROM property_marks`, "a mark's setBy", one)
	scan(`SELECT on_behalf_of FROM property_marks`, "a mark's onBehalfOf", one)
	scan(`SELECT props::text FROM records WHERE deleted_at IS NULL`, "a row's reference", in)
	return out
}

// THE ACTOR RECORDS ACROSS THE CONVERSION. Every actor id the converted
// repository links has a live record; no hand has two; each record main
// held is there under the id of its hand under `~`, with its type, its name
// under `~` and its `disabled` as main held them; and a start after writes
// no line.
func TestARepositoryOnMainKeepsEveryActorItLinksAcrossTheLocalNamespace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, name, _, reopen := openBeforeLocalNamespace(t)

	records := map[string]map[string]any{}
	rows, err := ds.db.QueryContext(ctx, `SELECT id, props FROM records WHERE kind = $1 AND deleted_at IS NULL`, kindActor)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			t.Fatal(err)
		}
		var props map[string]any
		if err := json.Unmarshal(raw, &props); err != nil {
			t.Fatal(err)
		}
		records[id] = props
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}

	for id, where := range linkedActors(t, ds) {
		if _, ok := records[id]; !ok {
			t.Errorf("%s links the actor record %s, which the repository does not hold", where, id)
		}
	}
	hands := map[substrate.Actor][]string{}
	for id, props := range records {
		key, ok := vocabulary.ActorKeyFrom(props)
		if !ok {
			t.Errorf("the actor record %s says no hand: %v", id, props)
			continue
		}
		hands[key] = append(hands[key], id)
		if strings.Contains(string(key), name) {
			t.Errorf("the actor record %s is the hand %s, which spells the repository's name", id, key)
		}
	}
	for key, ids := range hands {
		if len(ids) > 1 {
			slices.Sort(ids)
			t.Errorf("the hand %s has %d actor records: %v", key, len(ids), ids)
		}
	}

	raw, err := os.ReadFile(filepath.Join(beforeLocalNamespace, "actors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var before map[string]map[string]any
	if err := json.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}
	moved, disabledMoved := 0, 0
	for id, was := range before {
		key, ok := vocabulary.ActorKeyFrom(was)
		if !ok {
			t.Errorf("main's actor record %s says no hand: %v", id, was)
			continue
		}
		want := id
		if local, own := localHand(key, name); own {
			want, key = vocabulary.ActorID(local), local
		}
		if want != id {
			moved++
			if was["disabled"] == true {
				disabledMoved++
			}
		}
		now, ok := records[want]
		if !ok {
			t.Errorf("main's actor record %s (%s) is not held as %s", id, key, want)
			continue
		}
		for _, field := range []string{"type", "disabled"} {
			if now[field] != was[field] {
				t.Errorf("the actor record %s's %s is %v, main held %v", want, field, now[field], was[field])
			}
		}
		wantName, _ := was["name"].(string)
		if rest, own := strings.CutPrefix(wantName, name+"/"); own {
			wantName = vocabulary.Local + "/" + rest
		}
		if now["name"] != wantName {
			t.Errorf("the actor record %s's name is %v, want %q", want, now["name"], wantName)
		}
	}
	if moved == 0 || disabledMoved == 0 {
		t.Fatalf("%d of main's actor records moved, %d of them disabled: the fixture proves nothing about a hand that carried the name, or one that is gone", moved, disabledMoved)
	}

	var lines int64
	var hashes string
	read := func(d *dataset) {
		t.Helper()
		if err := d.db.QueryRowContext(ctx, `SELECT count(*), coalesce(string_agg(encode(hash, 'hex'), ' ' ORDER BY seq), '') FROM changelog`).Scan(&lines, &hashes); err != nil {
			t.Fatal(err)
		}
	}
	read(ds)
	n, h := lines, hashes
	read(reopen())
	if lines != n || hashes != h {
		t.Errorf("a start after the conversion wrote to the history: %d lines, then %d", n, lines)
	}
}

// nameFound is one place that spells a value the scan looks for: a row of a
// table, a stored Idempotency-Key outcome, or a file under the data root.
// kind and id are the record a row or a line's effect is about, where it is
// about one, and value is the JSON the scan reads paths in.
type nameFound struct {
	where, kind, id string
	// deleted is a fold row that is a tombstone.
	deleted bool
	value   any
	text    string
}

// namesLeft lists every place under the repository's database and data root
// that spells one of values: every column of every table, a stored outcome,
// and every file, by its contents and its name. A history line and a fold
// row are read effect by effect, record by record, so an allowance can name
// the kind and the path.
func namesLeft(t *testing.T, ds *dataset, root string, values ...string) []nameFound {
	t.Helper()
	ctx := context.Background()
	var out []nameFound
	holds := func(s string) bool {
		for _, v := range values {
			if strings.Contains(s, v) {
				return true
			}
		}
		return false
	}
	tables, err := ds.db.QueryContext(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_type = 'BASE TABLE' ORDER BY table_name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for tables.Next() {
		var n string
		if err := tables.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := errors.Join(tables.Err(), tables.Close()); err != nil {
		t.Fatal(err)
	}
	scan := func(table, q string, row func(text string)) {
		t.Helper()
		rows, err := ds.db.QueryContext(ctx, q)
		if err != nil {
			t.Fatalf("scan %s: %v", table, err)
		}
		for rows.Next() {
			var text string
			if err := rows.Scan(&text); err != nil {
				t.Fatal(err)
			}
			if holds(text) {
				row(text)
			}
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			t.Fatal(err)
		}
	}
	line := func(where string, raw []byte) {
		var l struct {
			Payload struct {
				Fold []map[string]any `json:"fold"`
			} `json:"payload"`
		}
		_ = json.Unmarshal(raw, &l)
		rest := string(raw)
		for _, ef := range l.Payload.Fold {
			b, _ := json.Marshal(ef)
			if !holds(string(b)) {
				continue
			}
			kind, _ := ef["ref"].(string)
			id, _ := ef["id"].(string)
			out = append(out, nameFound{where: where, kind: kind, id: id, value: ef, text: string(b)})
			rest = strings.Replace(rest, string(b), "", 1)
		}
		var whole map[string]any
		if json.Unmarshal(raw, &whole) == nil {
			if p, ok := whole["payload"].(map[string]any); ok {
				delete(p, "fold")
			}
			kind, _ := whole["kind"].(string)
			id, _ := whole["recordId"].(string)
			if b, _ := json.Marshal(whole); holds(string(b)) {
				out = append(out, nameFound{where: where + " (beside its effects)", kind: kind, id: id, value: whole, text: string(b)})
			}
		}
	}
	for _, table := range names {
		switch table {
		case "changelog":
			scan(table, `SELECT json_build_object('seq', seq, 'actor', actor, 'onBehalfOf', on_behalf_of, 'kind', kind, 'recordId', record_id, 'payload', payload)::text FROM changelog`, func(text string) {
				line("changelog", []byte(text))
			})
		case "records":
			// `fts` is derived storage of the row's title and text, and the
			// scan reads those.
			scan(table, `SELECT json_build_object('kind', kind, 'id', id, 'title', title, 'properties', props, 'row', to_jsonb(x) - 'props' - 'title' - 'fts')::text FROM records x`, func(text string) {
				var r map[string]any
				_ = json.Unmarshal([]byte(text), &r)
				kind, _ := r["kind"].(string)
				id, _ := r["id"].(string)
				row, _ := r["row"].(map[string]any)
				out = append(out, nameFound{where: "records", kind: kind, id: id, deleted: row["deleted_at"] != nil, value: r, text: text})
			})
		default:
			scan(table, `SELECT to_jsonb(x)::text FROM `+table+` x`, func(text string) {
				var r map[string]any
				_ = json.Unmarshal([]byte(text), &r)
				kind, _ := r["record_kind"].(string)
				id, _ := r["record_id"].(string)
				out = append(out, nameFound{where: table, kind: kind, id: id, value: r, text: text})
			})
		}
	}
	rows, err := ds.db.QueryContext(ctx, `SELECT operation || ' ' || key, convert_from(outcome, 'UTF8') FROM idempotency_keys WHERE outcome IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var key, text string
		if err := rows.Scan(&key, &text); err != nil {
			t.Fatal(err)
		}
		if holds(text) {
			out = append(out, nameFound{where: "idempotency outcome " + key, text: text})
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if holds(e.Name()) {
			out = append(out, nameFound{where: "file " + rel, text: "its name"})
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !holds(string(raw)) {
			return nil
		}
		if strings.HasSuffix(rel, ".ndjson") {
			for i, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				if holds(l) {
					line(rel+":"+strconv.Itoa(i+1), []byte(l))
				}
			}
			return nil
		}
		var v any
		_ = json.Unmarshal(raw, &v)
		out = append(out, nameFound{where: rel, value: v, text: string(raw)})
		return nil
	})
	if err != nil {
		t.Errorf("a file under the data root does not read: %v", err)
	}
	return out
}

// nameStays is the closed list of what may spell the repository's name
// after the conversion, each one kind and one path:
//
//   - The records whose job is to record the name, and their history: the
//     repository's own record (`name`, `authority`, and the one an older
//     release keyed by the name, deleted since decision 0261, by its id), its
//     first person's actor record's `name`, the login credential an older
//     release kept (`repository`, decision 0274), and an authority row,
//     which describes the publisher it is named for, by its id.
//   - The author's text: a function's `source`, an agent's `prompt`, a
//     kind's `description`.
//   - A title its writer gave, on a kind that renders none from a template.
//   - What the engine wrote at the time: an alert's `summary`, `detail` and
//     title, a run's `reason`, a delivery's `lastError`, and a title in a
//     line, which is what the engine rendered then.
//   - What a function was handed: a delivery's envelope says
//     `repository.authority`, which is the name.
//   - A paged delivery's `chain` key an older release keyed by the name.
//   - The archive's manifest: its `authority` and its people's names.
func nameStays(name string, authored map[string]bool, f nameFound) []string {
	if strings.HasSuffix(f.where, ".json") && strings.Contains(f.where, "/manifests/") {
		return pathsSpelling(f.value, name, "", func(path string) bool {
			return path == "authority" || path == "identities[].name"
		})
	}
	inLine := strings.HasPrefix(f.where, "changelog") || strings.Contains(f.where, ".ndjson")
	return pathsSpelling(f.value, name, "", func(path string) bool {
		return nameAt(name, f.kind, f.id, inLine, f.deleted, authored, path)
	})
}

// nameAt is nameStays for one path of a record of kind and id, in a line or
// in the fold.
func nameAt(name, kind, id string, inLine, deleted bool, authored map[string]bool, path string) bool {
	for _, prefix := range []string{"delta.set.", "properties.", "row."} {
		path = strings.TrimPrefix(path, prefix)
	}
	if path == "delta.title" {
		path = "title"
	}
	switch {
	// A title its writer gave, a title in a line, which is what the engine
	// rendered then, and a tombstone's, which is what it was when it went.
	case path == "title" && (authored[kind] || inLine || deleted):
		return true
	case kind == kindRepository:
		return path == "name" || path == "authority" || path == "id" || path == "record_id" || path == "recordId" || path == "title"
	case kind == vocabulary.CoreKind(vocabulary.DocAuthority) && id == name:
		return path == "id" || path == "record_id" || path == "recordId" || path == "title"
	case kind == kindActor && id == actorID(substrate.ActorPerson):
		return path == "name" || path == "title"
	case kind == kindCredential:
		return path == "repository" || path == "title"
	case kind == kindFunction:
		return path == "source"
	case kind == kindAgent:
		return path == "prompt"
	case kind == kindKind:
		return path == "description"
	case kind == kindAlert:
		return path == "summary" || path == "detail" || path == "title"
	case kind == typeTriggerRun:
		return path == "reason" || (inLine && path == "title")
	case kind == typeTrigger:
		return path == "failure.lastError" || path == "failure.payload.repository.authority" || path == "page.chain" ||
			(inLine && path == "title")
	case inLine:
		return path == "title"
	}
	return false
}

// pathsSpelling lists the paths in v where a key or a string spells name
// and ok does not allow it. A list's items share its path, with `[]`.
func pathsSpelling(v any, name, at string, ok func(string) bool) []string {
	var out []string
	switch t := v.(type) {
	case string:
		if strings.Contains(t, name) && !ok(at) {
			out = append(out, at)
		}
	case map[string]any:
		for k, x := range t {
			path := k
			if at != "" {
				path = at + "." + k
			}
			if strings.Contains(k, name) && !ok(path) {
				out = append(out, path+" (its key)")
			}
			out = append(out, pathsSpelling(x, name, path, ok)...)
		}
	case []any:
		for _, x := range t {
			out = append(out, pathsSpelling(x, name, at+"[]", ok)...)
		}
	}
	return out
}

// oldHandIDs are the actor record ids the repository's own hands had while
// they carried its name: each `~` hand the repository's actor records hold,
// spelled with name.
func oldHandIDs(t *testing.T, ds *dataset, name string) []string {
	t.Helper()
	rows, err := ds.db.QueryContext(context.Background(), `SELECT props::text FROM records WHERE kind = $1`, kindActor)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var props map[string]any
		_ = json.Unmarshal([]byte(raw), &props)
		key, ok := vocabulary.ActorKeyFrom(props)
		if !ok {
			continue
		}
		for _, prefix := range []string{substrate.BundleActorPrefix, substrate.FunctionActorPrefix, substrate.AgentActorPrefix} {
			if rest, own := strings.CutPrefix(string(key), prefix+vocabulary.Local+":"); own {
				out = append(out, vocabulary.ActorID(substrate.Actor(prefix+name+":"+rest)))
			}
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	return out
}

// THE NAME AFTER THE CONVERSION. A repository main held, and the kept
// v0.123.1 directory converted from its first dialect in one start, each
// converted, titled and archived again, spell the name in no table and no
// file under the data root but where nameStays says, and hold no actor id
// their own hands had under the name anywhere, besides what they were
// restored from.
func TestAConvertedRepositorySpellsItsNameOnlyWhereItRecordsIt(t *testing.T) {
	t.Parallel()
	check := func(t *testing.T, ds *dataset, name, root, restoredFrom string) {
		t.Helper()
		if err := ds.ArchivePass(context.Background()); err != nil {
			t.Fatal(err)
		}
		old := oldHandIDs(t, ds, name)
		if len(old) == 0 {
			t.Fatal("the repository holds no hand of its own, which proves nothing about their ids")
		}
		authored := map[string]bool{}
		for _, k := range ds.registry().Kinds() {
			if k.Template == nil {
				authored[k.Identity] = true
			}
		}
		left := 0
		for _, f := range namesLeft(t, ds, root, append([]string{name}, old...)...) {
			rel := strings.TrimPrefix(f.where, "file ")
			if strings.HasPrefix(rel, restoredFrom) {
				continue
			}
			text := f.text
			if len(text) > 300 {
				text = text[:300] + "…"
			}
			for _, id := range old {
				if strings.Contains(f.text, id) {
					t.Errorf("%s holds the old actor id %s: %s", f.where, id, text)
				}
			}
			if !strings.Contains(f.text, name) {
				continue
			}
			left++
			if f.value == nil {
				t.Errorf("%s spells the name in what the scan cannot read by path: %s", f.where, text)
				continue
			}
			if bad := nameStays(name, authored, f); len(bad) > 0 {
				t.Errorf("%s (%s/%s) spells the name at %v", f.where, f.kind, f.id, bad)
			}
		}
		if left == 0 {
			t.Fatal("the name is nowhere, not even in the repository's own record: the scan read nothing")
		}
	}
	t.Run("main", func(t *testing.T) {
		t.Parallel()
		ds, name, root, _ := openBeforeLocalNamespace(t)
		// The archive the repository was restored from, which another
		// database wrote: the one generation the fixture holds.
		entries, err := os.ReadDir(filepath.Join(beforeLocalNamespace, "data", "archive", "repositories", ds.info.ID))
		if err != nil {
			t.Fatal(err)
		}
		var gen string
		for _, e := range entries {
			if e.Name() != "manifests" {
				gen = e.Name()
			}
		}
		check(t, ds, name, root, filepath.Join("archive", "repositories", ds.info.ID, gen))
	})
	t.Run("v0.123.1", func(t *testing.T) {
		t.Parallel()
		kept := filepath.Join("..", "testenv", "testdata", "kept", "v0.123.1")
		ds, name, root, _ := openConverted(t, kept, "repositories", "repositories")
		// The directory it was imported from, as the release left it.
		check(t, ds, name, root, filepath.Join("repositories", name))
	})
}

// THE TITLES ACROSS THE CONVERSION. A title its writer gave (a memo of a kind
// that renders none, naming the note it is about) is byte for byte the same
// in the line and in the fold; a title the engine rendered from a template
// keeps in its line what it rendered then, and the record's current one is
// rendered again from the record as it reads now: a trigger's and a run's
// read `~` where they read the name, and spell it nowhere.
func TestTitlesAcrossTheLocalNamespace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, name, _, _ := openBeforeLocalNamespace(t)
	memo := vocabulary.DerivedMintedID("lab", "memo")
	note := name + "/lab/note/" + vocabulary.DerivedMintedID("lab", "first")
	var current string
	if err := ds.db.QueryRowContext(ctx, `SELECT title FROM records WHERE kind = '~/memos/memo' AND id = $1`, memo).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current != "Notes about "+note {
		t.Errorf("the memo's title is %q, want its writer's, byte for byte", current)
	}
	var lineTitle string
	if err := ds.db.QueryRowContext(ctx, `SELECT payload->'fold'->0->'delta'->>'title' FROM changelog WHERE record_id = $1 ORDER BY seq LIMIT 1`, memo).Scan(&lineTitle); err != nil {
		t.Fatal(err)
	}
	if lineTitle != "Notes about "+note {
		t.Errorf("the memo's line titles it %q, want its writer's, byte for byte", lineTitle)
	}
	rows, err := ds.db.QueryContext(ctx, `SELECT kind, id, title FROM records WHERE kind IN ($1, $2) AND deleted_at IS NULL`, typeTrigger, typeTriggerRun)
	if err != nil {
		t.Fatal(err)
	}
	local := 0
	for rows.Next() {
		var kind, id, title string
		if err := rows.Scan(&kind, &id, &title); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(title, name) {
			t.Errorf("%s/%s is titled %q, which spells the name", kind, id, title)
		}
		if strings.Contains(title, "~/auto/") {
			local++
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	if local == 0 {
		t.Error("no trigger or run is titled under ~, which proves nothing about the rendered titles")
	}
	var lines int
	if err := ds.db.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE kind IN ($1, $2) AND payload->'fold'->0->'delta'->>'title' LIKE '%' || $3 || '%'`,
		typeTrigger, typeTriggerRun, name).Scan(&lines); err != nil {
		t.Fatal(err)
	}
	if lines == 0 {
		t.Error("no line of a trigger or a run keeps the title it was rendered with, so the conversion edited a line's title")
	}
}
