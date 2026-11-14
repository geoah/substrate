package testenv_test

// What the kept directories' comparison (kept_db_test.go) cannot see on its
// own: what a conversion step does to the records a release served, and the
// material the records only name.

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/testenv"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

// keptImport is what checkKeptDirectory imported and read.
type keptImport struct {
	e        *testenv.Env
	fixture  keptFixture
	got      map[string]keptRecord
	upgraded map[string]*keptTouch
}

// keptConversion is what one conversion step does to the records a release
// served, as the record a GET of it answers.
type keptConversion struct {
	// why says what the step rewrites, and so why the release's records
	// change as record says, or why they do not.
	why string
	// record is the record as the converted history folds it, given the
	// release's (a copy it may change) and the name the release served it
	// under, or nil where the converted history holds no record. A nil
	// record leaves every record as it was.
	record func(rec map[string]any, repository string) map[string]any
	// trigger is a trigger's status, or one of its parked rows, as the
	// converted history serves it, given the release's (a copy it may
	// change). A nil trigger leaves them as they were.
	trigger func(row map[string]any, repository string) map[string]any
}

// keptConversions is every conversion step this binary carries, by the
// dialect it converts to. A step that lands adds its entry here, with its
// why, even one that changes nothing a release served:
// TestEveryConversionStepSaysWhatItDoesToAKeptDirectory refuses a chain
// with a step this table does not name.
var keptConversions = map[int]keptConversion{
	2: {why: "drops sealedKey from what each line of core/recoverykey sets; the one record those lines fold to is purged by repository migration 8 after the head, which the appended entries explain, so no record a release served changes here"},
	3: {
		why: "link_actors (decision 0270) rewrites every spelling of a hand the engine stored into its actor record: " +
			"a property's manager and an alternative's actor become the id, a package's declaredBy, a blob's createdBy " +
			"and a call run's caller a link to the record, a run's and a manager row's principal its token, " +
			"and a package's actors list goes",
		record: keptLinkActors,
	},
	4: {why: "move_body_into_properties (decision 0273) moves a record delta's body into set, and a cleared one into del; " +
		"a release served a record's body among its properties already, so no record it served changes here, and a body " +
		"no declaration served is dropped by repository migration 11 after the head, which the appended entries explain"},
	5: {
		why: "marks (decision 0284) rewrites each manager row into the property's mark: an owner or bundle tier pins " +
			"and keeps its hand as setBy, a machine tier pins nothing and keeps its hand only where the row was its own " +
			"direct write, every mark keeps the row's time as setAt, an alternative names its source as derivedFrom, " +
			"a split's kept result holds marks where it held manager rows, and a package's actors, which link_actors took out, keeps no mark",
		record: keptMarks,
	},
	6: {
		why:     "moves the repository's own kinds to the local namespace: a kind, a declaration's or a callable's id, a reference, the named positions of the substrate's own records, and a split's kept effects read `~/` where the release served the repository's name and a slash, the open renders again a title a template draws from a name that moved, and the actor record id of each of its own hands is the id of the same hand under `~`; nothing else moves",
		record:  keptLocalNamespace,
		trigger: keptLocalTrigger,
	},
	// conversion_0007_state_entries: it raises the stamp to the dialect
	// that may hold state entries and rewrites no line, since no history
	// written before it holds one, so every record reads as it did.
	7: {why: "state_entries rewrites no line: a history written before it holds no state entry"},
	// conversion_0008_erasures: it raises the stamp to the dialect that may
	// hold an erasure's intent and rewrites no line.
	8: {why: "erasures rewrites no line: a history written before it holds no erase effect"},
}

// keptDialect is the changelog dialect the kept directory's history is in.
func keptDialect(t *testing.T, dir, repository string) int {
	t.Helper()
	m, err := changelogfile.ReadManifest(filepath.Join(dir, "repositories", repository))
	if err != nil {
		t.Fatalf("read the kept manifest: %v", err)
	}
	return max(m.ChangelogDialect, 1)
}

// keptConvert takes the release's records through every step of
// conversions above dialect, in order, and returns the paths a step left no
// record at. A record's path follows what the step makes of it.
func keptConvert(want *keptRecords, conversions map[int]keptConversion, dialect int, repository string) map[string]bool {
	dropped := map[string]bool{}
	for _, d := range slices.Sorted(maps.Keys(conversions)) {
		c := conversions[d]
		if d <= dialect || c.record == nil {
			continue
		}
		kept := want.Records[:0]
		for _, rec := range want.Records {
			rec.Read = c.record(keptClone(rec.Read), repository)
			if rec.Listed != nil {
				rec.Listed = c.record(keptClone(rec.Listed), repository)
			}
			own := rec.Read
			if rec.Listed != nil {
				own = rec.Listed
			}
			if own == nil {
				dropped[rec.Path] = true
				continue
			}
			rec.Path = keptPath(own)
			kept = append(kept, rec)
		}
		want.Records = kept
	}
	return dropped
}

// keptConvertTriggers takes the release's trigger statuses and parked rows
// through every step of conversions above dialect, in order.
func keptConvertTriggers(want *keptTriggers, conversions map[int]keptConversion, dialect int, repository string) {
	for _, d := range slices.Sorted(maps.Keys(conversions)) {
		c := conversions[d]
		if d <= dialect || c.trigger == nil {
			continue
		}
		for i, s := range want.Statuses {
			want.Statuses[i] = c.trigger(keptClone(s), repository)
		}
		for id, rows := range want.Parked {
			for i, r := range rows {
				rows[i] = c.trigger(keptClone(r), repository)
			}
			want.Parked[id] = rows
		}
	}
}

// keptLocalNamespace is step 3 on what a release served, written from what
// the step moves (internal/engine/localnamespace.go's header), never read
// from what the current code serves. A string the step moves reads `~/`
// where it read the repository's name and a slash: a record's kind; an id
// that is a name (the id alphabet holds no dot, so an id under the name is
// a declaration's or a callable's); a reference, its kind and its id alike;
// and the strings at the positions keptLocalPositions lists. Every other
// string stays, the repository's name in a person's own text included.
func keptLocalNamespace(rec map[string]any, repository string) map[string]any {
	local := func(s string) string {
		if rest, ok := strings.CutPrefix(s, repository+"/"); ok {
			return "~/" + rest
		}
		return s
	}
	// moved is every name a reference or a named position of the record
	// moved, by its old spelling: a title rendered from one moves with it.
	moved := map[string]string{}
	ref := func(s string) string {
		parts := strings.SplitN(s, "/", 4)
		if len(parts) < 4 {
			to := local(s)
			if to != s {
				moved[s] = to
			}
			return to
		}
		to := local(strings.Join(parts[:3], "/")) + "/" + local(parts[3])
		if to != s {
			moved[s] = to
			if id := local(parts[3]); id != parts[3] {
				moved[parts[3]] = id
			}
		}
		return to
	}
	kind, _ := rec["kind"].(string)
	id, _ := rec["id"].(string)
	for _, key := range []string{"kind", "id"} {
		if v, ok := rec[key].(string); ok {
			rec[key] = local(v)
		}
	}
	// A declaration's own name moved with its id, and its title is drawn
	// from it.
	if to := local(id); to != id {
		moved[id] = to
	}
	// A declaration of the repository's own: its authority, and every name
	// in it under the repository's name, but the author's text.
	if strings.HasPrefix(id, repository+"/") {
		props, _ := rec["properties"].(map[string]any)
		if props["authority"] == repository {
			props["authority"] = "~"
		}
		if floors, ok := props["requiresAtLeast"].(map[string]any); ok {
			moved := map[string]any{}
			for pkg, v := range floors {
				moved[local(pkg)] = v
			}
			props["requiresAtLeast"] = moved
		}
		var names func(k string, v any) any
		names = func(k string, v any) any {
			switch x := v.(type) {
			case string:
				if !keptAuthorText[k] {
					return local(x)
				}
			case map[string]any:
				for key, y := range x {
					x[key] = names(key, y)
				}
			case []any:
				for i, y := range x {
					x[i] = names(k, y)
				}
			}
			return v
		}
		names("", props)
	}
	var walk func(v any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			if r, ok := x["ref"].(string); ok && len(x) <= 2 {
				x["ref"] = ref(r)
			}
			for k, y := range x {
				x[k] = walk(y)
			}
		case []any:
			for i, y := range x {
				x[i] = walk(y)
			}
		}
		return v
	}
	if props, ok := rec["properties"].(map[string]any); ok {
		walk(props)
		for _, path := range keptLocalPositions[kind] {
			keptAt(props, path, func(s string) string {
				to := local(s)
				if to != s {
					moved[s] = to
				}
				return to
			})
		}
		// A split's kept effects move as a line's: the record each lands
		// on, and the kinds its resync names.
		result, _ := props["result"].(map[string]any)
		effects, _ := result["fold"].([]any)
		for _, raw := range effects {
			eff, _ := raw.(map[string]any)
			if r, ok := eff["ref"].(string); ok {
				eff["ref"] = local(r)
			}
			for _, x := range append(keptList(eff["scope"]), keptList(keptMap(eff["rows"])["marks"])...) {
				if a, ok := x.(map[string]any); ok {
					if k, ok := a["kind"].(string); ok {
						a["kind"] = local(k)
					}
				}
			}
		}
		// The open renders again a title its kind's template draws from a
		// name the record moved (localtitles.go): a function's from its
		// authority, a trigger's and a run's from their callable. A title
		// drawn from text, an alert's from its summary, stays, and so does a
		// tombstone's, which is what it was when the record went.
		if title, ok := props["title"].(string); ok && len(moved) > 0 && keptTitledFromNames[kind] && rec["deletedAt"] == nil {
			olds := slices.Collect(maps.Keys(moved))
			slices.SortFunc(olds, func(a, b string) int { return len(b) - len(a) })
			for _, old := range olds {
				title = strings.ReplaceAll(title, old, moved[old])
			}
			props["title"] = title
		}
	}
	keptLocalActors(rec, repository)
	return rec
}

// keptTitledFromNames are the shipped kinds whose template renders a name: an
// authority, or a callable a record references.
var keptTitledFromNames = map[string]bool{
	vocabulary.CoreKind(vocabulary.DocFunction): true,
	"substrate.reamde.dev/core/trigger":         true,
	"substrate.reamde.dev/core/triggerrun":      true,
}

func keptList(v any) []any {
	l, _ := v.([]any)
	return l
}

func keptMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// keptLocalActors moves the actor record ids a record served for the
// repository's own hands, spelled with its name, to the ids of the same hands
// under `~`: a mark's hands, a split's kept marks, and a link to an actor
// record. Each id is read back to its hand as step 3 derived it.
func keptLocalActors(rec map[string]any, repository string) {
	move := func(id string) string {
		key, ok := vocabulary.ActorKeyOf(id)
		if !ok {
			return id
		}
		for _, prefix := range []string{substrate.BundleActorPrefix, substrate.FunctionActorPrefix, substrate.AgentActorPrefix} {
			if rest, ok := strings.CutPrefix(string(key), prefix+repository+":"); ok {
				return vocabulary.ActorID(substrate.Actor(prefix + "~:" + rest))
			}
		}
		return id
	}
	actor := vocabulary.CoreKind(vocabulary.DocActor) + "/"
	link := func(v any) {
		ref, _ := v.(map[string]any)
		path, _ := ref[vocabulary.ReferenceValueKey].(string)
		if id, ok := strings.CutPrefix(path, actor); ok {
			ref[vocabulary.ReferenceValueKey] = actor + move(id)
		}
	}
	// A property's mark, as a record serves it: its hands are links.
	marks, _ := rec["marks"].(map[string]any)
	for _, raw := range marks {
		if m, ok := raw.(map[string]any); ok {
			link(m["setBy"])
			link(m["onBehalfOf"])
		}
	}
	props, _ := rec["properties"].(map[string]any)
	for _, key := range []string{"declaredBy", "createdBy", "caller"} {
		link(props[key])
	}
	// A split's kept result holds marks as a line does, its hands ids.
	result, _ := props["result"].(map[string]any)
	effects, _ := result["fold"].([]any)
	for _, raw := range effects {
		eff, _ := raw.(map[string]any)
		if eff == nil || eff["kind"] != "mark" {
			continue
		}
		for _, key := range []string{"setBy", "onBehalfOf"} {
			if id, ok := eff[key].(string); ok {
				eff[key] = move(id)
			}
		}
	}
}

// keptLocalTrigger is step 3 on a trigger's status or parked row a release
// served: the callable it names, and every reference in it, as
// keptLocalNamespace moves them in a record.
func keptLocalTrigger(row map[string]any, repository string) map[string]any {
	callable, ok := row["callable"].(string)
	row = keptLocalNamespace(map[string]any{"properties": row}, repository)["properties"].(map[string]any)
	if ok {
		row["callable"] = keptLocalNamespace(map[string]any{"kind": callable}, repository)["kind"]
	}
	return row
}

// keptLocalPositions are the name positions of the substrate's own records
// a release served, by kind, as the step lists them (recordNamePositions,
// and a declaration's name keys): each path walks into objects by key and
// through lists, and the strings at its end move.
var keptLocalPositions = map[string][][]string{
	"substrate.reamde.dev/core/triggerrun": {{"callable"}},
	"substrate.reamde.dev/core/trigger":    {{"source", "record", "kinds"}},
}

// keptAuthorText is the keys of a declaration whose strings are its
// author's text, which nothing rewrites.
var keptAuthorText = map[string]bool{"description": true, "source": true, "prompt": true, "displayTemplate": true}

// keptAt applies fold to the strings at path in v.
func keptAt(v any, path []string, fold func(string) string) any {
	switch x := v.(type) {
	case []any:
		for i, y := range x {
			x[i] = keptAt(y, path, fold)
		}
		return x
	case map[string]any:
		if len(path) == 0 {
			return x
		}
		if y, ok := x[path[0]]; ok {
			x[path[0]] = keptAt(y, path[1:], fold)
		}
		return x
	case string:
		if len(path) == 0 {
			return fold(x)
		}
	}
	return v
}

// keptBlobText is the bytes the release attached as a blob (cmd/keptdir).
const keptBlobText = "the report the kept directory attaches"

// keptMaterial holds what the records name and the comparison cannot see,
// since the release served a secret as <redacted> and a blob by its digest:
// another password than the release's does not sign in (keptSignsIn proved
// the release's), the blob the release attached reads back as its bytes
// under its digest, and every sealed file the release wrote for a record
// opens under the data key, with every secret a live record holds held to
// one. It names each problem.
func keptMaterial(t *testing.T, e *testenv.Env, fixture keptFixture, dir string) []string {
	t.Helper()
	var out []string
	// The sign-in door paces a name, and keptSignsIn signed in through it,
	// so the other password goes to the service behind it.
	if _, err := e.Service.Login(context.Background(), substrate.LoginInput{
		Identity: fixture.Repository, Password: fixture.Password + " but another",
	}); !errors.Is(err, substrate.ErrAuth) {
		out = append(out, fmt.Sprintf("another password than the release's: %v, want an auth refusal", err))
	}

	var file struct {
		Properties struct {
			Data struct {
				Digest string `json:"digest"`
			} `json:"data"`
		} `json:"properties"`
	}
	e.MustJSON(http.MethodGet, "/api/v1/"+fixture.Repository+"/attach/file/00000000000000000000000009", nil, &file)
	digest := file.Properties.Data.Digest
	status, data, _ := e.DoRaw(http.MethodGet, "/api/v1/blobs/"+url.PathEscape(digest), nil, nil)
	sum := sha256.Sum256(data)
	switch {
	case status != http.StatusOK:
		out = append(out, fmt.Sprintf("the blob %s the release attached answers %d", digest, status))
	case string(data) != keptBlobText || digest != "blob-sha256-"+hex.EncodeToString(sum[:]):
		out = append(out, fmt.Sprintf("the blob %s the release attached reads back as %d other bytes", digest, len(data)))
	}

	files, err := os.ReadDir(filepath.Join(dir, "repositories", fixture.Repository, "sealed"))
	if err != nil {
		t.Fatal(err)
	}
	// The password's file was the sign-in record's, which the person's
	// identity holds now (decision 0274): the record and its sealed row are
	// purged at the first open. Every other file is held by one live ref.
	var sealed []string
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(dir, "repositories", fixture.Repository, "sealed", f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var rec struct {
			RecordKind string `json:"recordKind"`
		}
		if err := json.Unmarshal(raw, &rec); err != nil {
			t.Fatalf("sealed file %s: %v", f.Name(), err)
		}
		if rec.RecordKind != "substrate.reamde.dev/core/credential" {
			sealed = append(sealed, f.Name())
		}
	}
	report, err := e.Service.(engine.Operator).VerifyRepository(context.Background(), fixture.Repository)
	switch {
	case err != nil:
		out = append(out, fmt.Sprintf("verify the imported repository: %v", err))
	case !report.OK || report.SealedRows != len(sealed) || report.SealedOpened != len(sealed) || report.SecretRefs != len(sealed):
		out = append(out, fmt.Sprintf("the release's %d sealed file(s): verify holds %d, opened %d, %d secret ref(s), findings %v",
			len(sealed), report.SealedRows, report.SealedOpened, report.SecretRefs, report.Findings))
	}
	return out
}

// Every conversion step the binary carries says what it does to a release's
// records, and nothing names a step the chain lacks. It needs no database.
func TestEveryConversionStepSaysWhatItDoesToAKeptDirectory(t *testing.T) {
	t.Parallel()
	top := engine.MaxChangelogDialect()
	for d := 2; d <= top; d++ {
		if c, ok := keptConversions[d]; !ok || c.why == "" {
			t.Errorf("the conversion step to dialect %d has no entry with a why in keptConversions: say what it does to the records a release served, or that it leaves them as they were", d)
		}
	}
	for d := range keptConversions {
		if d < 2 || d > top {
			t.Errorf("keptConversions names dialect %d, which no step of the chain converts to", d)
		}
	}
}

// keptPerson is the person's actor record id, the actor the link_actors
// step gives every write the API made, which the test's conversion respells.
var keptPerson = vocabulary.ActorID(substrate.ActorPerson)

// keptConvertedActor is what the test's conversion respells the person's
// actor id as.
const keptConvertedActor = "api.converted"

// keptRespellAPIActor is a conversion step no release carries: it respells
// the person's actor id as keptConvertedActor, on the line, in each mark that
// names it as setBy, and in each mark row a resync re-states, and leaves a
// line already respelled as it is.
func keptRespellAPIActor(e *changelogfile.Entry) error {
	if e.Actor == keptPerson {
		e.Actor = keptConvertedActor
	}
	dec := json.NewDecoder(bytes.NewReader(e.Payload))
	dec.UseNumber()
	var p map[string]any
	if err := dec.Decode(&p); err != nil {
		return err
	}
	effects, _ := p["fold"].([]any)
	respell := func(m map[string]any) {
		if m["setBy"] == keptPerson {
			m["setBy"] = keptConvertedActor
		}
	}
	for _, raw := range effects {
		eff, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		respell(eff)
		rows, _ := eff["rows"].(map[string]any)
		marks, _ := rows["marks"].([]any)
		for _, row := range marks {
			if m, ok := row.(map[string]any); ok {
				respell(m)
			}
		}
	}
	out, err := json.Marshal(p)
	if err != nil {
		return err
	}
	e.Payload = out
	return nil
}

// keptRespelledMarks is what keptRespellAPIActor does to a record a release
// served, written from the step: every property the person's actor id set is
// set by keptConvertedActor, and nothing else moves.
func keptRespelledMarks(rec map[string]any, _ string) map[string]any {
	marks, _ := rec["marks"].(map[string]any)
	was := map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocActor), keptPerson)}
	for _, row := range marks {
		if m, ok := row.(map[string]any); ok && reflect.DeepEqual(m["setBy"], was) {
			m["setBy"] = map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocActor), keptConvertedActor)}
		}
	}
	return rec
}

// The comparison holds a release's records through a conversion: with a
// chain one step longer than the binary's, the step's entry makes the kept
// directory compare exact, and without it exactly the rows the step
// rewrote differ (finding 3 of step 3's second review).
func TestAKeptDirectoryComparesThroughAConversion(t *testing.T) {
	dir := filepath.Join(keptRoot, "v0.123.1")
	chain := maps.Clone(keptConversions)
	chain[engine.MaxChangelogDialect()+1] = keptConversion{
		why:    "the person's actor id is respelled on every line, so every property it set is set by api.converted",
		record: keptRespelledMarks,
	}
	k := checkKeptDirectory(t, dir, chain, testenv.WithEngineOptions(engine.WithTestConversionStep("respell_the_api_actor", keptRespellAPIActor)))

	var want keptRecords
	readKept(t, filepath.Join(dir, "records.json"), &want)
	keptConvert(&want, keptConversions, keptDialect(t, dir, k.fixture.Repository), k.fixture.Repository)
	differ := 0
	for _, rel := range want.Records {
		if k.upgraded[rel.Path] != nil || k.upgraded[keptPath(rel.Read)] != nil {
			continue
		}
		d := keptDiff(rel.Read, k.got[rel.Path].Read)
		if d == "" {
			continue
		}
		differ++
		if !strings.Contains(d, ".setBy") || !strings.Contains(d, keptConvertedActor) {
			t.Errorf("%s differs where the step did not write: %s", rel.Path, d)
		}
	}
	if differ == 0 {
		t.Fatal("without the step's entry no record differs: the comparison would not see a conversion")
	}
	t.Logf("without the step's entry, %d record(s) differ, each at a mark the step respelled", differ)
}

// Each of keptMaterial's checks fails on a copy of the kept directory whose
// material changed: a blob whose bytes changed, a blob gone, and a sealed
// secret gone. A sealed file that does not open under the data key refuses
// the import itself (finding 4 of step 3's second review).
func TestKeptMaterialChecksFailOnAChangedCopy(t *testing.T) {
	dir := filepath.Join(keptRoot, "v0.123.1")
	var fixture keptFixture
	readKept(t, filepath.Join(dir, "fixture.json"), &fixture)
	only := func(t *testing.T, sub, prefix string) string {
		t.Helper()
		matches, err := filepath.Glob(filepath.Join(sub, prefix+"*"))
		if err != nil || len(matches) == 0 {
			t.Fatalf("the kept copy holds no %s under %s (%v)", prefix, sub, err)
		}
		return matches[0]
	}
	copyOf := func(t *testing.T) (root, repoDir string) {
		t.Helper()
		root = t.TempDir()
		copyKept(t, filepath.Join(dir, "repositories"), filepath.Join(root, "repositories"))
		return root, filepath.Join(root, "repositories", fixture.Repository)
	}
	for _, c := range []struct {
		name, want string
		damage     func(t *testing.T, repoDir string)
	}{
		{"a blob's bytes changed", "blob", func(t *testing.T, repoDir string) {
			path := only(t, filepath.Join(repoDir, "blobs"), "blob-")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data[0] ^= 0x20
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"a blob gone", "blob", func(t *testing.T, repoDir string) {
			if err := os.Remove(only(t, filepath.Join(repoDir, "blobs"), "blob-")); err != nil {
				t.Fatal(err)
			}
		}},
		{"a sealed secret gone", "sealed", func(t *testing.T, repoDir string) {
			if err := os.Remove(only(t, filepath.Join(repoDir, "sealed"), "secret-")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			root, repoDir := copyOf(t)
			c.damage(t, repoDir)
			e := testenv.Start(t, testenv.WithoutRegistration(), testenv.WithRepository(fixture.Repository),
				testenv.WithDataRoot(root), testenv.WithCredentialKey(fixture.CredentialKey))
			e.Token = fixture.Token
			problems := keptMaterial(t, e, fixture, dir)
			found := false
			for _, p := range problems {
				found = found || strings.Contains(p, c.want)
			}
			if !found {
				t.Fatalf("the checks found nothing of %s: %v", c.name, problems)
			}
		})
	}
	t.Run("a sealed secret that does not open", func(t *testing.T) {
		root, repoDir := copyOf(t)
		// Each secret's sealed file swapped for the other's: both are
		// ciphertext under the data key, each bound to its own ref.
		secrets, err := filepath.Glob(filepath.Join(repoDir, "sealed", "secret-*"))
		if err != nil || len(secrets) != 2 {
			t.Fatalf("the kept copy holds %d sealed secrets (%v), want 2", len(secrets), err)
		}
		var payloads [2]map[string]any
		for i, path := range secrets {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &payloads[i]); err != nil {
				t.Fatal(err)
			}
		}
		for i, path := range secrets {
			swapped := maps.Clone(payloads[i])
			swapped["payload"] = payloads[1-i]["payload"]
			raw, err := json.Marshal(swapped)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		svc, err := engine.Open(context.Background(), testdb.NewSchema(t), engine.WithKindsFS(kinds.Seed()),
			engine.WithDataRoot(root), engine.WithCredentialKey(fixture.CredentialKey))
		if err == nil {
			_ = svc.Close()
			t.Fatal("a directory whose sealed secrets do not open under their refs imported")
		}
		if !strings.Contains(err.Error(), "does not open") {
			t.Fatalf("the import's refusal does not say the sealed store does not open: %v", err)
		}
	})
}

// keptLinkActors is link_actors on a record a release served, written from
// the step (internal/engine/conversion_0003_link_actors.go, its header's
// positions). A release named each hand by its spelling, and every spelling
// a server served is one of the engine's own forms, which the step reads by
// substrate.CanonicalActor and turns into the id of the hand's actor record
// (vocabulary.ActorID).
func keptLinkActors(rec map[string]any, _ string) map[string]any {
	meta, _ := rec["propertyMeta"].(map[string]any)
	for _, raw := range meta {
		m, _ := raw.(map[string]any)
		if m == nil {
			continue
		}
		if v, ok := m["manager"]; ok {
			m["manager"] = keptActorID(v)
		}
		alts, _ := m["alternatives"].([]any)
		for _, a := range alts {
			if am, ok := a.(map[string]any); ok {
				am["actor"] = keptActorID(am["actor"])
			}
		}
	}
	props, _ := rec["properties"].(map[string]any)
	if props == nil {
		return rec
	}
	link := func(key string) {
		if v, ok := props[key]; ok {
			props[key] = keptActorLink(v)
		}
	}
	switch rec["kind"] {
	case vocabulary.CoreKind("package"):
		delete(props, "actors")
		link("declaredBy")
	case vocabulary.CoreKind("blob"):
		link("createdBy")
	case vocabulary.CoreKind("triggerrun"):
		link("caller")
		if principal, ok := props["principal"]; ok {
			delete(props, "principal")
			props["token"] = principal
		}
	case vocabulary.CoreKind("recordmerge"):
		if moved, ok := props["moved"].(map[string]any); ok {
			keptManagerRows(moved["managers"])
		}
	case vocabulary.CoreKind("recordsplit"):
		result, _ := props["result"].(map[string]any)
		effects, _ := result["fold"].([]any)
		for _, raw := range effects {
			eff, _ := raw.(map[string]any)
			switch {
			case eff == nil:
			case eff["kind"] == "manager":
				keptManagerRows([]any{eff})
			case eff["kind"] == "resync":
				if rows, ok := eff["rows"].(map[string]any); ok {
					keptManagerRows(rows["managers"])
				}
			}
		}
	}
	return rec
}

// keptManagerRows links each manager row's actor and moves its `principal`
// to `token` where a token stood.
func keptManagerRows(v any) {
	rows, _ := v.([]any)
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		row["actor"] = keptActorID(row["actor"])
		if principal, ok := row["principal"]; ok {
			delete(row, "principal")
			if s, _ := principal.(string); s != "" {
				row["token"] = s
			}
		}
	}
}

func keptActorID(spelled any) any {
	s, ok := spelled.(string)
	if !ok || s == "" {
		return spelled
	}
	return vocabulary.ActorID(substrate.CanonicalActor(substrate.Actor(s)))
}

func keptActorLink(spelled any) any {
	id, ok := keptActorID(spelled).(string)
	if !ok || id == "" {
		return spelled
	}
	return map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocActor), id)}
}

// keptMarks is the marks step on a record a release served, written from the
// step (internal/engine/conversion_0005_marks.go, its header's rules), after
// link_actors made each hand an id. A row the release served at the machine
// tier was either its hand's own direct write or a recompute crediting a
// source's writer, and a recompute's row named the source its value came
// from: the step keeps the hand of the first and not of the second. No
// converted mark names a derivedFrom, and none names onBehalfOf but an app's
// own direct write's, which a served record does not show: the kept
// directories hold no write of an app.
func keptMarks(rec map[string]any, _ string) map[string]any {
	meta, _ := rec["propertyMeta"].(map[string]any)
	delete(rec, "propertyMeta")
	marks := map[string]any{}
	for name, raw := range meta {
		row, _ := raw.(map[string]any)
		if row == nil {
			continue
		}
		// The release served a package's `actors` with the manager row its
		// write left on it. Step 3 takes the list out of every package line
		// and step 5 the row with it (takenOutBelow), so no mark stands on a
		// value no record holds.
		if rec["kind"] == vocabulary.CoreKind("package") && name == "actors" {
			continue
		}
		m := map[string]any{}
		_, recomputed := row["source"]
		if pinned := row["tier"] != string(substrate.TierMachine); pinned || !recomputed {
			if pinned {
				m["pinned"] = true
			}
			if id, _ := row["manager"].(string); id != "" {
				m["setBy"] = map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(vocabulary.CoreKind(vocabulary.DocActor), id)}
			}
		}
		if at, ok := row["updatedAt"]; ok {
			m["setAt"] = at
		}
		if alts, ok := row["alternatives"].([]any); ok {
			var out []any
			for _, a := range alts {
				am, _ := a.(map[string]any)
				if am == nil {
					continue
				}
				alt := map[string]any{"value": am["value"], "updatedAt": am["updatedAt"]}
				if src, ok := am["source"].(string); ok && src != "" {
					alt["derivedFrom"] = map[string]any{vocabulary.ReferenceValueKey: src}
				}
				out = append(out, alt)
			}
			m["alternatives"] = out
		}
		marks[name] = m
	}
	if len(marks) > 0 {
		rec["marks"] = marks
	}
	if rec["kind"] != vocabulary.CoreKind("recordsplit") {
		return rec
	}
	props, _ := rec["properties"].(map[string]any)
	result, _ := props["result"].(map[string]any)
	effects, _ := result["fold"].([]any)
	for i, raw := range effects {
		eff, _ := raw.(map[string]any)
		switch {
		case eff == nil:
		case eff["kind"] == "manager":
			// A kept result is no line, so its effects are no line's own
			// direct write.
			effects[i] = keptMarkOfRow(eff, true)
		case eff["kind"] == "resync":
			rows, _ := eff["rows"].(map[string]any)
			managers, ok := rows["managers"].([]any)
			if !ok {
				continue
			}
			var marks []any
			for _, r := range managers {
				if row, ok := r.(map[string]any); ok {
					marks = append(marks, keptMarkOfRow(row, false))
				}
			}
			delete(rows, "managers")
			if len(marks) > 0 {
				rows["marks"] = marks
			}
		}
	}
	return rec
}

// keptMarkOfRow is the mark a manager effect (effect) or a resync's row
// becomes in a kept result: its place, pinned and set by its hand above the
// machine tier, and its time.
func keptMarkOfRow(row map[string]any, effect bool) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"ref", "id", "property"} {
		if v, ok := row[key]; ok {
			out[key] = v
		}
	}
	actor, _ := row["actor"].(string)
	switch {
	case effect && actor == "":
		out["kind"] = "unmark"
		return out
	case effect:
		out["kind"] = "mark"
	default:
		if v, ok := row["kind"]; ok {
			out["kind"] = v
		}
	}
	if row["tier"] != string(substrate.TierMachine) {
		out["pinned"] = true
		if actor != "" {
			out["setBy"] = actor
		}
	}
	if at, ok := row["updatedAt"]; ok {
		out["setAt"] = at
	}
	return out
}

// keptMarksWithoutValues holds the rule the marks keep (decision 0284): a
// mark never outlives its value. It names every mark of the repository (by
// its id) whose record the fold does not hold, or whose record holds no
// value of the property, in its properties or in the column that backs a
// title or a time, as a pin or a release reads it (marks.go markAction).
func keptMarksWithoutValues(t *testing.T, dsn, repository string) []string {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.QueryContext(context.Background(), `
		SELECT m.record_kind, m.record_id, m.property, m.pinned, r.id IS NOT NULL, coalesce(r.deleted_at IS NOT NULL, false)
		FROM property_marks m
		LEFT JOIN records r ON r.repository = m.repository AND r.kind = m.record_kind AND r.id = m.record_id
		WHERE m.repository = $1 AND (r.id IS NULL OR NOT (
			coalesce(r.props ? m.property, false)
			OR (m.property = $2 AND coalesce(r.title, '') <> '')
			OR (m.property = $3 AND r.at IS NOT NULL)
			OR (m.property = $4 AND r.ends_at IS NOT NULL)
			OR (m.property = $5 AND r.due_at IS NOT NULL)))
		ORDER BY 1, 2, 3`,
		repository, substrate.PropTitle, substrate.PropAt, substrate.PropEndsAt, substrate.PropDueAt)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for rows.Next() {
		var kind, id, property string
		var pinned, held, deleted bool
		if err := rows.Scan(&kind, &id, &property, &pinned, &held, &deleted); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%s/%s holds a mark on %s (pinned %t) and no value under it (the record in the fold %t, deleted %t)",
			kind, id, property, pinned, held, deleted))
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	return out
}

// A HISTORY FROM BEFORE THE MARKS KEEPS THEM THROUGH A REBUILD. The kept
// directory's lines hold manager effects, which the conversion rewrites into
// marks (decision 0284): a rebuild of the converted history gives back the
// property_marks and property_offers rows the import's fold holds, byte for
// byte, pinned and unpinned marks among them. The rebuild test of every SQL
// migration stages its stores in the binary's own dialect, so this is the
// test of a history converted from before this step.
func TestAConvertedHistorysMarksSurviveARebuild(t *testing.T) {
	k := checkKeptDirectory(t, filepath.Join(keptRoot, "v0.123.1"), keptConversions)
	db, err := sql.Open("pgx", k.e.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	var id string
	if err := db.QueryRowContext(ctx, `SELECT id FROM repositories WHERE authority = $1 OR id = $1`, k.fixture.Repository).Scan(&id); err != nil {
		t.Fatal(err)
	}
	read := func() (string, int, int) {
		t.Helper()
		var b strings.Builder
		pinned, all := 0, 0
		for _, q := range []string{
			`SELECT record_kind, record_id, property, pinned::text, set_by, on_behalf_of, derived_from::text, set_at::text
			 FROM property_marks WHERE repository = $1 ORDER BY record_kind, record_id, property`,
			`SELECT record_kind, record_id, property, source, coalesce(value::text, ''), updated_at::text, '', ''
			 FROM property_offers WHERE repository = $1 ORDER BY record_kind, record_id, property, source`,
		} {
			rows, err := db.QueryContext(ctx, q, id)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var c [8]string
				if err := rows.Scan(&c[0], &c[1], &c[2], &c[3], &c[4], &c[5], &c[6], &c[7]); err != nil {
					t.Fatal(err)
				}
				fmt.Fprintln(&b, strings.Join(c[:], "\x1f"))
				if strings.HasPrefix(q, "SELECT record_kind, record_id, property, pinned") {
					all++
					if c[3] == "true" {
						pinned++
					}
				}
			}
			if err := errors.Join(rows.Err(), rows.Close()); err != nil {
				t.Fatal(err)
			}
		}
		return b.String(), pinned, all
	}
	before, pinned, all := read()
	if pinned == 0 || pinned == all {
		t.Fatalf("the converted history holds %d marks, %d of them pinned: the test needs both kinds", all, pinned)
	}
	rebuilder, ok := k.e.Service.(interface {
		RebuildRepository(context.Context, string) (engine.RebuildReport, error)
	})
	if !ok {
		t.Fatal("the service does not rebuild")
	}
	if _, err := rebuilder.RebuildRepository(ctx, id); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after, _, _ := read(); after != before {
		t.Fatal("a rebuild of the converted history does not give back the marks and offers the import's fold holds")
	}
	for _, p := range keptMarksWithoutValues(t, k.e.DSN, id) {
		t.Errorf("after the rebuild: %s", p)
	}
	t.Logf("%d marks, %d of them pinned, survive the rebuild", all, pinned)
}

// errKeptKilled is the fault a start stopped at one step of the import
// returns, as a process killed there stops.
var errKeptKilled = errors.New("the process died here")

// keptStartStoppedAt starts the engine over root and dsn, the kept
// directory's copy and a database that holds none of it, and stops the start
// at stage of the import, as a process killed there would; what the start
// committed before it stays.
func keptStartStoppedAt(t *testing.T, dsn, root, key, stage string) {
	t.Helper()
	fired := false
	svc, err := engine.Open(context.Background(), dsn,
		engine.WithKindsFS(kinds.Seed()), engine.WithDataRoot(root), engine.WithCredentialKey(key),
		engine.WithTestImportFault(16, func(s string) error {
			if s == stage && !fired {
				fired = true
				return errKeptKilled
			}
			return nil
		}))
	if err == nil {
		_ = svc.Close()
	}
	if !fired || !errors.Is(err, errKeptKilled) {
		t.Fatalf("the start did not die at %q: reached %v, %v", stage, fired, err)
	}
}

// THE SHIPPED CHAIN STOPPED AT EACH STEP OF THE KEPT DIRECTORY'S IMPORT. The
// kept directory's history converts through every step this binary carries
// as it is imported: its rows go in, the chain rewrites them in one
// transaction under the changelog lock (conversion.go), and they fold. A
// start killed at each durable step of that import, the conversion's own
// included, leaves what the next start finishes into the repository a start
// that was never killed serves: the comparison with what the release served
// holds, as it does uncut.
func TestTheKeptDirectorysImportStoppedAtEachStepIsFinishedByTheNextStart(t *testing.T) {
	dir := filepath.Join(keptRoot, "v0.123.1")
	var fixture keptFixture
	readKept(t, filepath.Join(dir, "fixture.json"), &fixture)
	var release keptRecords
	readKept(t, filepath.Join(dir, "records.json"), &release)
	// The history an uncut import of the same directory converts the kept
	// lines to, seq and checksum: what a cut one must finish to, line for
	// line. The served records alone would not show a line lost that no
	// record still reflects; a line doubled cannot be stored, its seq being
	// the table's key.
	uncut := t.TempDir()
	copyKept(t, filepath.Join(dir, changelogfile.RepositoriesDir), filepath.Join(uncut, changelogfile.RepositoriesDir))
	uncutDSN := testdb.NewSchema(t)
	want, lines := keptHistory(t, checkKeptDirectoryIn(t, dir, uncut, keptConversions, testenv.WithDSN(uncutDSN)), uncutDSN, release.Head)
	if lines != release.Head {
		t.Fatalf("the uncut import holds %d of the release's %d lines", lines, release.Head)
	}
	for _, stage := range []string{
		engine.ImportAfterRow, engine.ImportAfterSealed, engine.ImportAfterBatch,
		engine.ConversionBeforeCommit, engine.ConversionAfterCommit, engine.RefoldAfterFirstReplay, engine.RefoldBeforeCommit,
		engine.ImportBeforeCleanup, engine.CleanupSetAside, engine.CleanupPartly,
	} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			copyKept(t, filepath.Join(dir, changelogfile.RepositoriesDir), filepath.Join(root, changelogfile.RepositoriesDir))
			dsn := testdb.NewSchema(t)
			keptStartStoppedAt(t, dsn, root, fixture.CredentialKey, stage)
			k := checkKeptDirectoryIn(t, dir, root, keptConversions, testenv.WithDSN(dsn))
			if got, n := keptHistory(t, k, dsn, release.Head); got != want {
				t.Errorf("the finished import's history up to the release's head is not the uncut one's: %d lines, want %d, or a line's checksum differs", n, lines)
			}
		})
	}
}

// keptHistory is the imported repository's changelog lines up to head, seq
// and checksum, and how many there are. The database holds others beside it
// (the comparison registers a fresh one), so it is read by the repository's
// id.
func keptHistory(t *testing.T, k keptImport, dsn string, head int64) (string, int64) {
	t.Helper()
	ds, err := k.e.Service.Dataset(context.Background(), k.fixture.Repository)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var lines string
	var n int64
	if err := db.QueryRow(`SELECT coalesce(string_agg(seq::text || ':' || encode(hash, 'hex'), ' ' ORDER BY seq), ''), count(*)
		FROM changelog WHERE repository = $2 AND seq <= $1`, head, ds.Repository().ID).Scan(&lines, &n); err != nil {
		t.Fatal(err)
	}
	return lines, n
}

// keptActorSpelling is a prefixed hand's retired spelling as a whole JSON
// string, anywhere in a line or a record: the free pattern for text that is
// not at one of the engine's positions. A label or annotation key keeps its
// `<actor>/<name>` namespace and carries a slash, which this never matches.
// keptActorRef is a link to an actor record.
var (
	keptActorSpelling = regexp.MustCompile(`"(?:function|agent|bundle|app|mapping|policy|connector):[^"/]*"`)
	keptActorRef      = regexp.MustCompile(`"ref":"` + regexp.QuoteMeta(vocabulary.CoreKind(vocabulary.DocActor)) + `/([^"]+)"`)
)

// The places beside the engine's own position tables (engine.ActorPositions)
// where a hand stands: a line's actor and the one it acted for, the retired
// `principal` on a line and among a trigger run's properties, and a
// package's retired `actors` list.
const (
	keptLineActor      = "line/actor"
	keptLineOnBehalfOf = "line/onBehalfOf"
	keptLinePrincipal  = "line/principal"
	keptRunPrincipal   = "triggerrun/principal"
	keptPackageActors  = "package/actors"
)

// keptNeverWrote are the positions the kept release never wrote a hand at,
// each with why: the scan of the directory as kept must find a hand as text
// at every other position, and finding one at a position listed here fails,
// so the list says what the fixture does not exercise, and no more.
var keptNeverWrote = map[string]string{
	keptLineOnBehalfOf:             "v0.123.1 kept the hand behind an app's write as `principal`; onBehalfOf is conversion step 3's (link_actors)",
	keptRunPrincipal:               "the kept history holds no trigger run",
	"payload/triggeredBy":          "no line of the kept history is a write a trigger's fire made",
	"payload/moved/marks/setBy":    "marks are conversion step 5's (decision 0284); v0.123.1 wrote manager rows",
	"mark/setBy":                   "marks are conversion step 5's (decision 0284); v0.123.1 wrote manager rows",
	"mark/onBehalfOf":              "marks are conversion step 5's (decision 0284); v0.123.1 wrote manager rows",
	"resync/rows/marks/setBy":      "marks are conversion step 5's (decision 0284); v0.123.1's resyncs carried manager rows",
	"resync/rows/marks/onBehalfOf": "marks are conversion step 5's (decision 0284); v0.123.1's resyncs carried manager rows",
	vocabulary.CoreKind("recordmerge") + "/moved/marks/setBy": "marks are conversion step 5's (decision 0284); v0.123.1's merges moved manager rows",
	vocabulary.CoreKind("triggerrun") + "/caller":             "the kept history holds no trigger run; `caller` appears only in the run kind's own declaration",
}

// keptLinked is whether the value at a position names a hand by its actor
// record: a bare id, or a reference value at an actor record where the
// engine writes one. keptAsText is whether it names one any other way. An
// empty value names nobody, and is neither.
func keptLinked(p engine.ActorPosition) bool {
	if !p.Ref {
		s, ok := p.Value.(string)
		return ok && vocabulary.MintedID(s)
	}
	m, ok := p.Value.(map[string]any)
	if !ok {
		return false
	}
	ref, _ := m[vocabulary.ReferenceValueKey].(string)
	id, ok := strings.CutPrefix(ref, vocabulary.CoreKind(vocabulary.DocActor)+"/")
	return ok && vocabulary.MintedID(id)
}

func keptAsText(p engine.ActorPosition) bool {
	if s, ok := p.Value.(string); ok && s == "" {
		return false
	}
	return !keptLinked(p)
}

// keptActorOwn are the fields of an actor record that spell its own hand.
var keptActorOwn = []string{"name", "key"}

// keptWithoutOwnText is what a record of kind holds, props, with every field
// that may rightly spell a hand left out: of a declaration, the values the
// dialect classes as its author's text, code or prompt
// (vocabulary.WithoutAuthorText, the engine's own table); of an actor record,
// its own name and key. A field is spared, never a kind: every other value
// of every record is held to spell no hand.
func keptWithoutOwnText(kind string, props map[string]any) any {
	switch {
	case vocabulary.IsDeclarationRef(kind):
		return vocabulary.WithoutAuthorText(kind, props)
	case kind == vocabulary.CoreKind(vocabulary.DocActor):
		out := maps.Clone(props)
		for _, k := range keptActorOwn {
			delete(out, k)
		}
		return out
	}
	return props
}

// keptFreePayload is a line's payload, or a change event's, as the free
// spelling pattern reads it: every record effect's set held to
// keptWithoutOwnText, and an actor record's title left out.
func keptFreePayload(t *testing.T, p map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var cp map[string]any
	if err := json.Unmarshal(raw, &cp); err != nil {
		t.Fatal(err)
	}
	effects, _ := cp["fold"].([]any)
	for _, e := range effects {
		ef, _ := e.(map[string]any)
		delta, _ := ef["delta"].(map[string]any)
		if delta == nil {
			continue
		}
		ref, _ := ef["ref"].(string)
		if set, ok := delta["set"].(map[string]any); ok {
			delta["set"] = keptWithoutOwnText(ref, set)
		}
		if ref == vocabulary.CoreKind(vocabulary.DocActor) {
			delete(delta, "title")
		}
	}
	out, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// keptFreeRecord is a record a read serves, as the free spelling pattern
// reads it: its properties held to keptWithoutOwnText, and an actor
// record's title left out.
func keptFreeRecord(t *testing.T, kind string, read map[string]any) []byte {
	t.Helper()
	cp := maps.Clone(read)
	if props, ok := cp["properties"].(map[string]any); ok {
		cp["properties"] = keptWithoutOwnText(kind, props)
	}
	if kind == vocabulary.CoreKind(vocabulary.DocActor) {
		delete(cp, "title")
	}
	out, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// keptSetsIn reports whether a payload sets property on a record of kind.
// A declaration that declares a property of that name is not a record of
// the kind, so the trigger run's own declaration of `principal` is no hit.
func keptSetsIn(p map[string]any, kind, property string) bool {
	effects, _ := p["fold"].([]any)
	for _, raw := range effects {
		ef, _ := raw.(map[string]any)
		if ef == nil || ef["ref"] != kind {
			continue
		}
		delta, _ := ef["delta"].(map[string]any)
		set, _ := delta["set"].(map[string]any)
		if _, ok := set[property]; ok {
			return true
		}
	}
	return false
}

// AN IMPORTED KEPT DIRECTORY HOLDS NO ACTOR AS TEXT (validation item 1 of the
// 1.0 review). The positions are the engine's own (engine.ActorPositions:
// the tables the local namespace's conversion walks), with a line's actor
// and the one it acted for, the retired `principal` and a package's retired
// `actors` beside them; no kind is exempt from a position, and the free
// spelling pattern alone spares an actor record's name and a declaration's
// body.
//
// The scan is proven on the directory AS KEPT: over v0.123.1's own lines,
// read through changelogfile.Decode before any import, it finds a hand as
// text at every position but those keptNeverWrote names with why, and none
// at those. Over the repository the current code imports from it (its
// history as the export carries it, every change event the feed serves,
// payload included, and every record a read serves) it finds none at any
// position, and every hand it finds is an actor record's id. That each id
// resolves to a record of its hand's type is the actor fill's test
// (actorfill_db_test.go).
func TestAnImportedKeptDirectoryHoldsNoActorAsText(t *testing.T) {
	dir := filepath.Join(keptRoot, "v0.123.1")
	var fixture keptFixture
	readKept(t, filepath.Join(dir, "fixture.json"), &fixture)

	// The directory as kept.
	asText := map[string]int{}
	spelled := 0
	segments, err := filepath.Glob(filepath.Join(dir, changelogfile.RepositoriesDir, fixture.Repository, changelogfile.ChangelogSubdir, "*.ndjson"))
	if err != nil || len(segments) == 0 {
		t.Fatalf("the kept directory's segments: %v (%d)", err, len(segments))
	}
	for _, name := range segments {
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
			e, _, err := changelogfile.Decode(line)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if e.Actor != "" && !vocabulary.MintedID(e.Actor) {
				asText[keptLineActor]++
			}
			if e.OnBehalfOf != "" && !vocabulary.MintedID(e.OnBehalfOf) {
				asText[keptLineOnBehalfOf]++
			}
			if e.HasPrincipal {
				asText[keptLinePrincipal]++
			}
			var p map[string]any
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				t.Fatalf("line %d: %v", e.Seq, err)
			}
			for _, pos := range engine.ActorPositionsInPayload(p) {
				if keptAsText(pos) {
					asText[pos.Name]++
				}
			}
			if keptSetsIn(p, vocabulary.CoreKind("triggerrun"), "principal") {
				asText[keptRunPrincipal]++
			}
			if keptSetsIn(p, vocabulary.CoreKind("package"), "actors") {
				asText[keptPackageActors]++
			}
			if keptActorSpelling.Match(keptFreePayload(t, p)) {
				spelled++
			}
		}
	}
	names := []string{keptLineActor, keptLineOnBehalfOf, keptLinePrincipal, keptRunPrincipal, keptPackageActors}
	for _, p := range engine.ActorPositions() {
		names = append(names, p.Name)
	}
	for _, name := range names {
		why, never := keptNeverWrote[name]
		switch {
		case never && asText[name] > 0:
			t.Errorf("the kept directory holds a hand as text at %s %d time(s), which keptNeverWrote says it never wrote (%s)", name, asText[name], why)
		case !never && asText[name] == 0:
			t.Errorf("the kept directory holds no hand as text at %s: the scan of it is not seen to fire on old data; name it in keptNeverWrote with why", name)
		}
	}
	for name := range keptNeverWrote {
		if !slices.Contains(names, name) {
			t.Errorf("keptNeverWrote names %s, which is no position the scan walks", name)
		}
	}
	if spelled == 0 {
		t.Error("the kept directory spells no hand in free text: the free pattern is not seen to fire on old data")
	}
	t.Logf("the directory as kept: a hand as text by position %v, spelled in free text on %d lines", asText, spelled)

	// The repository the current code imports from it.
	k := checkKeptDirectory(t, dir, keptConversions)
	linked := 0
	link := func(where, id string) {
		t.Helper()
		if id == "" {
			return
		}
		if !vocabulary.MintedID(id) {
			t.Errorf("%s holds %q, which is no actor record's id", where, id)
			return
		}
		linked++
	}
	positions := func(where string, ps []engine.ActorPosition) {
		t.Helper()
		for _, pos := range ps {
			switch {
			case keptAsText(pos):
				t.Errorf("%s holds a hand as text at %s: %v", where, pos.Name, pos.Value)
			case keptLinked(pos):
				linked++
			}
		}
	}
	free := func(where, kind string, raw []byte) {
		t.Helper()
		if m := keptActorSpelling.Find(raw); m != nil {
			t.Errorf("%s (%s) holds the spelling %s outside a field that may spell a hand", where, kind, m)
		}
	}

	// The history, as the export carries it.
	status, raw, _ := k.e.DoRaw(http.MethodGet, "/api/v1/export", nil, nil)
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
			e, _, err := changelogfile.Decode(line)
			if err != nil {
				t.Fatalf("%s: %v", hdr.Name, err)
			}
			lines++
			where := fmt.Sprintf("line %d", e.Seq)
			link(where+" actor", e.Actor)
			link(where+" onBehalfOf", e.OnBehalfOf)
			if e.HasPrincipal {
				t.Errorf("%s carries `principal`", where)
			}
			var p map[string]any
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				t.Fatalf("%s: %v", where, err)
			}
			positions(where, engine.ActorPositionsInPayload(p))
			if keptSetsIn(p, vocabulary.CoreKind("triggerrun"), "principal") {
				t.Errorf("%s sets a trigger run's `principal`", where)
			}
			if keptSetsIn(p, vocabulary.CoreKind("package"), "actors") {
				t.Errorf("%s sets a package's `actors`", where)
			}
			free(where, e.Kind, keptFreePayload(t, p))
		}
	}
	if lines == 0 {
		t.Fatal("the export carried no history line")
	}

	// Every change event the feed serves, its payload included.
	events := 0
	q := url.Values{"first": {"500"}}
	for {
		var page substrate.ChangePage
		k.e.MustJSON(http.MethodGet, "/api/v1/changes?"+q.Encode(), nil, &page)
		for _, c := range page.Changes {
			events++
			where := fmt.Sprintf("change %d", c.Seq)
			link(where+" actor", c.Actor)
			link(where+" onBehalfOf", c.OnBehalfOf)
			link(where+" token", c.Token)
			positions(where, engine.ActorPositionsInPayload(c.Payload))
			if keptSetsIn(c.Payload, vocabulary.CoreKind("triggerrun"), "principal") {
				t.Errorf("%s sets a trigger run's `principal`", where)
			}
			free(where, c.Kind, keptFreePayload(t, c.Payload))
		}
		if page.Cursor == 0 {
			break
		}
		q = url.Values{"first": {"500"}, "before": {fmt.Sprint(page.Cursor)}, "generation": {page.Generation}}
	}
	// The feed serves the record changes, not every line (a trigger's
	// bookkeeping is a line and no event), so the two counts differ.
	if events == 0 {
		t.Fatal("the feed served no event")
	}

	// Every record as a single read serves it, its marks among it.
	for path, rec := range k.got {
		kind := fmt.Sprint(rec.Read["kind"])
		body, err := json.Marshal(rec.Read)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range keptActorRef.FindAllSubmatch(body, -1) {
			link(path, string(m[1]))
		}
		props, _ := rec.Read["properties"].(map[string]any)
		positions(path, engine.ActorPositionsInRecord(kind, props))
		marks, _ := rec.Read["marks"].(map[string]any)
		positions(path, engine.ActorPositionsInMarks(marks))
		if kind == vocabulary.CoreKind("triggerrun") && props["principal"] != nil {
			t.Errorf("%s still holds `principal`: %v", path, props["principal"])
		}
		if kind == vocabulary.CoreKind("package") && props["actors"] != nil {
			t.Errorf("%s still lists actors: %v", path, props["actors"])
		}
		free(path, kind, keptFreeRecord(t, kind, rec.Read))
	}

	t.Logf("imported: %d history lines, %d change events and %d records, naming a hand by an id %d times", lines, events, len(k.got), linked)
}

// The walker of a read's marks refuses a hand left as text at a mark's
// setBy or onBehalfOf, as a read serves them, and takes a link. It needs no
// database.
func TestAReadsMarkHoldingAHandAsTextIsRefused(t *testing.T) {
	t.Parallel()
	link := map[string]any{"ref": vocabulary.CoreKind(vocabulary.DocActor) + "/" + vocabulary.DerivedMintedID("kept", "hand")}
	marks := map[string]any{
		"name":  map[string]any{"setBy": "owner", "onBehalfOf": link},
		"phone": map[string]any{"setBy": link, "pinned": true},
	}
	var text, linked []string
	for _, p := range engine.ActorPositionsInMarks(marks) {
		switch {
		case keptAsText(p):
			text = append(text, fmt.Sprintf("%s=%v", p.Name, p.Value))
		case keptLinked(p):
			linked = append(linked, p.Name)
		}
	}
	if len(text) != 1 || text[0] != "mark/setBy=owner" {
		t.Errorf("the walker found %v as text, want the one textual setBy", text)
	}
	if len(linked) != 2 {
		t.Errorf("the walker found %v linked, want the onBehalfOf and the other setBy", linked)
	}
}

// The free spelling pattern spares a field, never a kind, and the fields
// are the engine's: a declaration's writer is no text, so a hand left
// spelled there is found; a function's source is code, so one there is not;
// an actor record spares its own name and title and nothing else. It needs
// no database.
func TestTheFreeSpellingSparesAFieldNeverAKind(t *testing.T) {
	t.Parallel()
	hand := "bundle:kept.example.com:people"
	found := func(raw []byte) bool { return keptActorSpelling.Match(raw) }
	declaration := keptFreeRecord(t, vocabulary.CoreKind(vocabulary.DocKind), map[string]any{
		"properties": map[string]any{
			"description": "written by " + hand,
			"properties":  map[string]any{"name": map[string]any{"type": "string", "writer": hand, "description": hand}},
		},
	})
	if !found(declaration) {
		t.Error("a hand spelled in a declared property's writer is spared")
	}
	function := keptFreeRecord(t, vocabulary.CoreKind(vocabulary.DocFunction), map[string]any{
		"properties": map[string]any{"source": "return '" + hand + "'", "description": hand},
	})
	if found(function) {
		t.Error("a hand spelled in a function's source or description is found")
	}
	actor := vocabulary.CoreKind(vocabulary.DocActor)
	own := keptFreeRecord(t, actor, map[string]any{"title": hand, "properties": map[string]any{"name": hand, "type": "bundle"}})
	if found(own) {
		t.Error("an actor record's own name and title are not spared")
	}
	other := keptFreeRecord(t, actor, map[string]any{"properties": map[string]any{"name": hand, "publisher": hand}})
	if !found(other) {
		t.Error("an actor record spares a field beside its own name")
	}
	line := keptFreePayload(t, map[string]any{"fold": []any{map[string]any{
		"kind": "record", "ref": vocabulary.CoreKind(vocabulary.DocKind),
		"delta": map[string]any{"set": map[string]any{"properties": map[string]any{"name": map[string]any{"writer": hand}}}},
	}}})
	if !found(line) {
		t.Error("a hand spelled in a declaration line's writer is spared")
	}
}
