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
	// release's (a copy it may change), or nil where the converted history
	// holds no record. A nil record leaves every record as it was.
	record func(rec map[string]any) map[string]any
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
func keptConvert(want *keptRecords, conversions map[int]keptConversion, dialect int) map[string]bool {
	dropped := map[string]bool{}
	for _, d := range slices.Sorted(maps.Keys(conversions)) {
		c := conversions[d]
		if d <= dialect || c.record == nil {
			continue
		}
		kept := want.Records[:0]
		for _, rec := range want.Records {
			rec.Read = c.record(keptClone(rec.Read))
			if rec.Listed != nil {
				rec.Listed = c.record(keptClone(rec.Listed))
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

// keptBlobText is the bytes the release attached as a blob (cmd/keptdir).
const keptBlobText = "the report the kept directory attaches"

// keptMaterial holds what the records name and the comparison cannot see,
// since the release served a secret as <redacted> and a blob by its digest:
// the release's password signs in and another does not, the blob the
// release attached reads back as its bytes under its digest, and every
// sealed file the release wrote opens under the data key, with every secret
// a live record holds held to one. It names each problem.
func keptMaterial(t *testing.T, e *testenv.Env, fixture keptFixture, dir string) []string {
	t.Helper()
	var out []string
	if status, raw := e.Login(fixture.Repository, fixture.Password, ""); status != http.StatusCreated {
		out = append(out, fmt.Sprintf("the release's password does not sign in: %d %s", status, raw))
	}
	// The sign-in door paces a name, so the other password goes to the
	// service behind it.
	if _, _, err := e.Service.Login(context.Background(), substrate.LoginInput{
		Repository: fixture.Repository, Password: fixture.Password + " but another",
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

	sealed, err := os.ReadDir(filepath.Join(dir, "repositories", fixture.Repository, "sealed"))
	if err != nil {
		t.Fatal(err)
	}
	// Every sealed file is held by one live ref: the password's and the two
	// secrets'.
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
func keptRespelledMarks(rec map[string]any) map[string]any {
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
	keptConvert(&want, keptConversions, keptDialect(t, dir, k.fixture.Repository))
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
func keptLinkActors(rec map[string]any) map[string]any {
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
func keptMarks(rec map[string]any) map[string]any {
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
	for _, stage := range []string{
		engine.ImportAfterRow, engine.ImportAfterSealed, engine.ImportAfterBatch,
		engine.ConversionBeforeCommit, engine.ConversionAfterCommit, engine.ImportAfterFirstFold,
		engine.ImportBeforeCleanup, engine.CleanupSetAside, engine.CleanupPartly,
	} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			copyKept(t, filepath.Join(dir, changelogfile.RepositoriesDir), filepath.Join(root, changelogfile.RepositoriesDir))
			dsn := testdb.NewSchema(t)
			keptStartStoppedAt(t, dsn, root, fixture.CredentialKey, stage)
			checkKeptDirectoryIn(t, dir, root, keptConversions, testenv.WithDSN(dsn))
		})
	}
}

// keptActorSpelling is a prefixed hand's retired spelling as a whole JSON
// string; a label or annotation key keeps its `<actor>/<name>` namespace and
// carries a slash, which this never matches. keptHandAsText is a key that
// names a hand, holding a string: a stored attribution is a link, an object,
// so a string under one of the three is the retired spelling.
var (
	keptActorSpelling = regexp.MustCompile(`"(?:function|agent|bundle|app|mapping|policy|connector):[^"/]*"`)
	keptHandAsText    = regexp.MustCompile(`"(declaredBy|createdBy|caller)":"([^"]*)"`)
	keptHandOnALine   = regexp.MustCompile(`"(actor|triggeredBy)":"([^"]*)"`)
	keptActorRef      = regexp.MustCompile(`"ref":"` + regexp.QuoteMeta(vocabulary.CoreKind(vocabulary.DocActor)) + `/([^"]+)"`)
)

// AN IMPORTED KEPT DIRECTORY HOLDS NO ACTOR AS TEXT (validation item 1 of the
// 1.0 review). The kept release's repository, imported and converted under
// the current code, is read where a client reads it: its history as the
// export carries it, every change event the feed serves, and every record
// with its marks. No line, event or value names a hand by its spelling, a
// line's token is never `principal`, and every hand is named by an actor
// record's id. That each id resolves to a record of its hand's type is the
// actor fill's test (actorfill_db_test.go).
func TestAnImportedKeptDirectoryHoldsNoActorAsText(t *testing.T) {
	k := checkKeptDirectory(t, filepath.Join(keptRoot, "v0.123.1"), keptConversions)
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
	textual := func(kind string) bool {
		// A record of the actor kind holds its own name, and a declaration's
		// text is its own.
		return kind == vocabulary.CoreKind(vocabulary.DocActor) || vocabulary.IsDeclarationRef(kind)
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
			if textual(e.Kind) {
				continue
			}
			if m := keptActorSpelling.Find(e.Payload); m != nil {
				t.Errorf("%s (%s %s) holds the spelling %s", where, e.Op, e.Kind, m)
			}
			for _, m := range keptHandAsText.FindAllSubmatch(e.Payload, -1) {
				t.Errorf("%s (%s %s) names a hand by %s %q, not a link", where, e.Op, e.Kind, m[1], m[2])
			}
			for _, m := range keptHandOnALine.FindAllSubmatch(e.Payload, -1) {
				link(where+" "+string(m[1]), string(m[2]))
			}
			for _, m := range keptActorRef.FindAllSubmatch(e.Payload, -1) {
				link(where+" link", string(m[1]))
			}
			if bytes.Contains(e.Payload, []byte(`"principal"`)) {
				t.Errorf("%s carries principal in its payload", where)
			}
		}
	}
	if lines == 0 {
		t.Fatal("the export carried no history line")
	}

	// Every change event the feed serves.
	events := 0
	q := url.Values{"first": {"500"}}
	for {
		var page substrate.ChangePage
		k.e.MustJSON(http.MethodGet, "/api/v1/changes?"+q.Encode(), nil, &page)
		for _, c := range page.Changes {
			events++
			link(fmt.Sprintf("change %d actor", c.Seq), c.Actor)
			link(fmt.Sprintf("change %d onBehalfOf", c.Seq), c.OnBehalfOf)
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
		if textual(kind) {
			continue
		}
		if m := keptActorSpelling.Find(body); m != nil {
			t.Errorf("%s holds the spelling %s", path, m)
		}
		for _, m := range keptHandAsText.FindAllSubmatch(body, -1) {
			t.Errorf("%s names a hand by %s %q, not a link", path, m[1], m[2])
		}
		if kind == vocabulary.CoreKind("package") {
			if props, _ := rec.Read["properties"].(map[string]any); props["actors"] != nil {
				t.Errorf("%s still lists actors: %v", path, props["actors"])
			}
		}
	}

	t.Logf("%d history lines, %d change events and %d records, naming a hand by an id %d times", lines, events, len(k.got), linked)
}
