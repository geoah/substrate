package commands

// The repository's door of `publish` (--as) prints the server's plan and
// changes nothing; --yes publishes, and a refusal is printed in the server's
// words with what to run next. `release` prints the release the server wrote.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const fakePublishRecord = "v=substrate1 doc=https://substrate.example.com/published/rnd-0123/ada.example.com"

func (f *fakeSubstrate) handlePublish(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	var in substrate.PublishInput
	_ = json.Unmarshal(f.lastBody["package"], &in.Package)
	_ = json.Unmarshal(f.lastBody["authority"], &in.Authority)
	_ = json.Unmarshal(f.lastBody["confirm"], &in.Confirm)
	_, name := vocabulary.SplitPackageRef(in.Package)
	published := in.Authority + "/" + name
	blockers := append([]string{}, f.publishBlockers...)
	plan := substrate.PublishPlan{
		Package: in.Package, Published: published,
		Kinds: []string{published + "/recipe", published + "/step"}, Packages: []string{"~/mealplans"},
		Records: 12, References: 3, Blockers: blockers,
		Name: "_substrate." + in.Authority, Record: fakePublishRecord,
		DocumentURL: "https://substrate.example.com/published/rnd-0123/" + in.Authority,
		Proven:      f.publishProven,
	}
	if !plan.Proven {
		plan.Reason = `no TXT record opening with "v=substrate1" at ` + plan.Name
	}
	switch {
	case !in.Confirm:
		writeJSON(w, http.StatusOK, plan)
	case !plan.Proven:
		writeError(w, http.StatusForbidden, "forbidden", "publishing "+in.Package+" as "+published+" needs its authority's proof: "+plan.Reason, nil)
	case len(blockers) > 0:
		writeError(w, http.StatusConflict, "guard", strings.Join(blockers, "; "), nil)
	default:
		plan.Release = &substrate.Release{Package: published, Version: 1, SHA256: strings.Repeat("a", 64), ReleasedAt: testNow}
		writeJSON(w, http.StatusOK, plan)
	}
}

func (f *fakeSubstrate) handleRelease(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	var pkg string
	_ = json.Unmarshal(f.lastBody["package"], &pkg)
	if f.releaseUnchanged {
		writeError(w, http.StatusConflict, "conflict", pkg+" has not changed since version 1, its last release", nil)
		return
	}
	writeJSON(w, http.StatusOK, substrate.Release{Package: pkg, Version: 2, SHA256: strings.Repeat("b", 64), Dialect: 3, ReleasedAt: testNow})
}

func TestPublishAsPrintsThePlanAndChangesNothing(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	out, _ := h.mustRun("publish", "~/recipes", "--as", "ada.example.com")
	if got := h.lastRequest(); got != "POST "+pathPublish {
		t.Fatalf("publish --as hit %q, want the publish route", got)
	}
	if _, confirmed := h.fake.lastBody["confirm"]; confirmed {
		t.Errorf("publish without --yes sent confirm: %s", h.fake.lastBody["confirm"])
	}
	for _, want := range []string{
		"published as:", "ada.example.com/recipes",
		"~/recipes/recipe -> ada.example.com/recipes/recipe",
		"records that move:", "12", "references repointed:", "3",
		"packages that follow:", "~/mealplans",
		"This is permanent: ~/recipes leaves ~ for good", "It cannot be unpublished",
		"_substrate.ada.example.com", "TXT", `"` + fakePublishRecord + `"`,
		"DNS: not found yet: no TXT record",
		"Nothing was changed.", "substratectl publish '~/recipes' --as ada.example.com --yes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the plan lacks %q:\n%s", want, out)
		}
	}

	h.fake.mu.Lock()
	h.fake.publishBlockers = []string{"~/recipes/recipe: 2 records hold a value the published kind refuses"}
	h.fake.mu.Unlock()
	out, _ = h.mustRun("publish", "~/recipes", "--as", "ada.example.com")
	if !strings.Contains(out, "blocked by:\n  - ~/recipes/recipe: 2 records") || !strings.Contains(out, "Clear what blocks it") {
		t.Errorf("the plan with a blocker:\n%s", out)
	}
}

func TestPublishAsYesPublishesOnceTheAuthorityIsProven(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	_, _, err := h.run("publish", "~/recipes", "--as", "ada.example.com", "--yes")
	if err == nil {
		t.Fatal("publish --yes before DNS shows the record succeeded")
	}
	var rendered bytes.Buffer
	renderError(&rendered, err)
	for _, want := range []string{
		"error: the publish was refused",
		"needs its authority's proof: no TXT record",
		"hint: nothing was changed; run `substratectl publish '~/recipes' --as ada.example.com` for the plan",
	} {
		if !strings.Contains(rendered.String(), want) {
			t.Errorf("the refusal lacks %q:\n%s", want, rendered.String())
		}
	}

	h.fake.mu.Lock()
	h.fake.publishProven = true
	h.fake.mu.Unlock()
	out, _ := h.mustRun("publish", "~/recipes", "--as", "ada.example.com", "--yes")
	if string(h.fake.lastBody["confirm"]) != "true" {
		t.Errorf("publish --yes sent confirm %s, want true", h.fake.lastBody["confirm"])
	}
	for _, want := range []string{
		"published ~/recipes as ada.example.com/recipes",
		"first release:", "version:", "1", strings.Repeat("a", 64), "2026-08-02T12:00:00Z",
		"substratectl publish ada.example.com/recipes --release",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the publish lacks %q:\n%s", want, out)
		}
	}

	out, _ = h.mustRun("publish", "~/recipes", "--as", "ada.example.com", "-o", "json")
	var plan substrate.PublishPlan
	if err := json.Unmarshal([]byte(out), &plan); err != nil || plan.Published != "ada.example.com/recipes" || !plan.Proven {
		t.Errorf("publish -o json = %+v (%v):\n%s", plan, err, out)
	}
}

// An unquoted ~/recipes reaches the CLI as the home directory's recipes, and
// the repository's own name is another spelling of ~: both are sent as ~.
func TestPublishAsSendsThePackageAsTheServerNamesIt(t *testing.T) {
	t.Setenv("HOME", "/home/ada")
	h := newHarness(t)
	h.writeConfig()
	for _, arg := range []string{"/home/ada/recipes", "geoah/recipes", "~/recipes"} {
		h.mustRun("publish", arg, "--as", "ada.example.com")
		if got := string(h.fake.lastBody["package"]); got != `"~/recipes"` {
			t.Errorf("publish %s sent package %s, want ~/recipes", arg, got)
		}
	}
}

func TestPublishCheckAndAsAreExclusive(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"publish", "~/recipes", "--as", "ada.example.com", "--check"}, "--check and --as are exclusive"},
		{[]string{"publish", "~/recipes", "--as", "ada.example.com", "--version", "2"}, "--version belongs to --check"},
		{[]string{"publish", "example.com/notes", "--yes"}, "--yes belongs to --as <authority>"},
		{[]string{"publish", "example.com/notes", "--release", "--as", "ada.example.com"}, "--release is a form of its own"},
		{[]string{"publish", "example.com/notes", "--release", "--version", "2"}, "--version belongs to --check, not to --release"},
		{[]string{"publish", "example.com/notes"}, "publish requires --as <authority>"},
	} {
		_, _, err := h.run(tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err = %v, want %q", tc.args, err, tc.want)
		}
	}
	if len(h.fake.requests) != 0 {
		t.Errorf("a refused flag set still sent %v", h.fake.requests)
	}
}

func TestPublishReleasePrintsTheReleaseOrTheRefusal(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	out, _ := h.mustRun("publish", "ada.example.com/recipes", "--release")
	if got := h.lastRequest(); got != "POST "+pathRelease || string(h.fake.lastBody["package"]) != `"ada.example.com/recipes"` {
		t.Fatalf("publish --release hit %q with %s", got, h.fake.lastBody["package"])
	}
	for _, want := range []string{
		"wrote release 2 of ada.example.com/recipes",
		"sha256:", strings.Repeat("b", 64), "dialect:", "3", "released:", "2026-08-02T12:00:00Z",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("release lacks %q:\n%s", want, out)
		}
	}

	h.fake.mu.Lock()
	h.fake.releaseUnchanged = true
	h.fake.mu.Unlock()
	_, _, err := h.run("publish", "ada.example.com/recipes", "--release")
	var rendered bytes.Buffer
	renderError(&rendered, err)
	for _, want := range []string{
		"error: the release was refused",
		"has not changed since version 1",
		"hint: no release was written: change the package's declarations",
	} {
		if !strings.Contains(rendered.String(), want) {
			t.Errorf("the refusal lacks %q:\n%s", want, rendered.String())
		}
	}

	before := len(h.fake.requests)
	_, _, err = h.run("publish", "~/recipes", "--release")
	if err == nil || !strings.Contains(err.Error(), "~/recipes is not published yet") {
		t.Errorf("publish ~/recipes --release: err = %v, want it refused as not published", err)
	}
	if len(h.fake.requests) != before {
		t.Errorf("publish ~/recipes --release still sent %v", h.fake.requests[before:])
	}
}

// A package published under the repository's own name is released by that
// name: the CLI never folds it to ~, so the release reaches the server.
func TestReleaseOfAPackagePublishedUnderTheRepositorysOwnName(t *testing.T) {
	h := newHarness(t)
	h.writeConfigAuthority("ada.example.com")
	h.mustRun("publish", "ada.example.com/recipes", "--release")
	if got := h.lastRequest(); got != "POST "+pathRelease || string(h.fake.lastBody["package"]) != `"ada.example.com/recipes"` {
		t.Fatalf("release under the repository's own name hit %q with %s", got, h.fake.lastBody["package"])
	}
}

// `release` is the marks' word: the package's release is a form of publish,
// and each command's help names the other, so a person who types the wrong
// one is sent to the right one.
func TestReleaseAndPublishEachNameTheOther(t *testing.T) {
	h := newHarness(t)
	release, _ := h.mustRun("release", "--help")
	if !strings.Contains(release, "substratectl publish <authority>/<package> --release") {
		t.Errorf("release --help does not name publish --release:\n%s", release)
	}
	publish, _ := h.mustRun("publish", "--help")
	if !strings.Contains(publish, "use substratectl release") {
		t.Errorf("publish --help does not name release:\n%s", publish)
	}
}
