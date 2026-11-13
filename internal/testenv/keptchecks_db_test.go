package testenv_test

// What the kept directories' comparison (kept_db_test.go) cannot see on its
// own: what a conversion step does to the records a release served, and the
// material the records only name.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
// the person's actor id as keptConvertedActor, on the line, in each effect that
// names it, and in each manager row a resync re-states, and leaves a line
// already respelled as it is.
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
		if m["actor"] == keptPerson {
			m["actor"] = keptConvertedActor
		}
	}
	for _, raw := range effects {
		eff, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		respell(eff)
		rows, _ := eff["rows"].(map[string]any)
		managers, _ := rows["managers"].([]any)
		for _, row := range managers {
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

// keptRespelledManagers is what keptRespellAPIActor does to a record a
// release served, written from the step: every property the person's actor
// id managed is managed by keptConvertedActor, and nothing else moves.
func keptRespelledManagers(rec map[string]any) map[string]any {
	meta, _ := rec["propertyMeta"].(map[string]any)
	for _, row := range meta {
		if m, ok := row.(map[string]any); ok && m["manager"] == keptPerson {
			m["manager"] = keptConvertedActor
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
		why:    "the person's actor id is respelled on every line, so every property it managed is managed by api.converted",
		record: keptRespelledManagers,
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
		if !strings.Contains(d, ".manager:") || !strings.Contains(d, keptConvertedActor) {
			t.Errorf("%s differs where the step did not write: %s", rel.Path, d)
		}
	}
	if differ == 0 {
		t.Fatal("without the step's entry no record differs: the comparison would not see a conversion")
	}
	t.Logf("without the step's entry, %d record(s) differ, each at a manager the step respelled", differ)
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
