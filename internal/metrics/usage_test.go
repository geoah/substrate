package metrics

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// withUsageLabels runs the test under a label policy and restores the
// default after it.
func withUsageLabels(t *testing.T, anonymize bool, secret string) {
	t.Helper()
	SetUsageLabels(anonymize, []byte(secret))
	t.Cleanup(func() { SetUsageLabels(true, nil) })
}

func TestUsageLabelsInClear(t *testing.T) {
	withUsageLabels(t, false, "k")
	if got := Repository("ada.example.com"); got != "ada.example.com" {
		t.Errorf("Repository = %q", got)
	}
	for _, name := range []string{"ada.example.com/tasks/task", "other.example.org/x/y", "substrate.reamde.dev/core/kind"} {
		if got := Name("ada.example.com", name); got != name {
			t.Errorf("Name(%q) = %q", name, got)
		}
	}
	if got := Provider("ada.example.com", "my-ollama"); got != "my-ollama" {
		t.Errorf("Provider = %q", got)
	}
	if got := Model("ada.example.com", "ft:gpt-4o:ada-corp:x", false); got != "ft:gpt-4o:ada-corp:x" {
		t.Errorf("Model = %q", got)
	}
}

func TestUsageLabelsAnonymized(t *testing.T) {
	withUsageLabels(t, true, "k")
	repo := Repository("ada.example.com")
	if !strings.HasPrefix(repo, "repo:") || strings.Contains(repo, "ada") {
		t.Errorf("Repository = %q, want a repo: hash", repo)
	}
	if Repository("ada.example.com") != repo {
		t.Error("the same repository hashed twice to two labels")
	}
	// The shipped vocabulary is the product's and stays readable.
	for _, name := range []string{
		"substrate.reamde.dev/core/kind",
		"providers.substrate.reamde.dev/google/gmailmessage",
		"samples.substrate.reamde.dev/tasks/task",
		"substrate.reamde.dev/assistant/moss",
	} {
		if got := Name("ada.example.com", name); got != name {
			t.Errorf("Name(%q) = %q, want it in clear", name, got)
		}
	}
	// An owner's own names hash without their authority, so two owners'
	// copies of one sample read as one name.
	own := Name("ada.example.com", "ada.example.com/tasks/task")
	if !strings.HasPrefix(own, "own:") || strings.Contains(own, "task") {
		t.Errorf("own name = %q", own)
	}
	if other := Name("bob.example.com", "bob.example.com/tasks/task"); other != own {
		t.Errorf("one package under two authorities hashed to %q and %q", own, other)
	}
	if ext := Name("ada.example.com", "bob.example.com/tasks/task"); !strings.HasPrefix(ext, "ext:") {
		t.Errorf("another authority's name = %q", ext)
	}
	// A lookalike authority is not the publisher's.
	if got := Name("ada.example.com", "evilsubstrate.reamde.dev/x/y"); !strings.HasPrefix(got, "ext:") {
		t.Errorf("lookalike authority = %q, want hashed", got)
	}
	if Provider("ada.example.com", "anthropic") != "anthropic" || !strings.HasPrefix(Provider("ada.example.com", "my-ollama"), "own:") {
		t.Errorf("Provider = %q, %q", Provider("ada.example.com", "anthropic"), Provider("ada.example.com", "my-ollama"))
	}
	// A model only a public list or an operator names is in clear; an
	// owner's may carry their organization, a fine-tune's id does.
	if got := Model("ada.example.com", "claude-opus-5-5", true); got != "claude-opus-5-5" {
		t.Errorf("public Model = %q", got)
	}
	if got := Model("ada.example.com", "ft:gpt-4o:ada-corp:x", false); !strings.HasPrefix(got, "own:") || strings.Contains(got, "ada") {
		t.Errorf("private Model = %q", got)
	}
	// The key is what makes a hash: another key, another label.
	SetUsageLabels(true, []byte("other"))
	if Repository("ada.example.com") == repo {
		t.Error("two keys hashed a repository to one label")
	}
}

func TestActorClass(t *testing.T) {
	for actor, want := range map[string]string{
		"person": "person", "app": "app", "function": "function", "agent": "agent",
		"package": "package", "system": "system", "": "other", "console": "other",
	} {
		if got := ActorClass(actor); got != want {
			t.Errorf("ActorClass(%q) = %q, want %q", actor, got, want)
		}
	}
}

// gauges reads every series of one family as label-string -> value.
func gauges(t *testing.T, family string) map[string]float64 {
	t.Helper()
	families, err := Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for _, f := range families {
		if f.GetName() != family {
			continue
		}
		for _, m := range f.GetMetric() {
			out[labelString(m)] = m.GetGauge().GetValue()
		}
	}
	return out
}

func labelString(m *dto.Metric) string {
	var parts []string
	for _, l := range m.GetLabel() {
		parts = append(parts, l.GetName()+"="+l.GetValue())
	}
	return strings.Join(parts, ",")
}

func TestInventoryCollector(t *testing.T) {
	withUsageLabels(t, false, "k")
	since := time.Unix(1_700_000_000, 0)
	unregister := RegisterInventory("ada.example.com", func() Inventory {
		return Inventory{
			Records: map[string]int64{"ada.example.com/tasks/task": 3, "substrate.reamde.dev/core/kind": 40},
			Packages: []PackageInventory{
				{Package: "ada.example.com/tasks", Origin: "sample", From: "samples.substrate.reamde.dev/tasks", Kinds: 2},
				{Package: "ada.example.com/garden", Origin: "own", Kinds: 1},
				{Package: "substrate.reamde.dev/core", Origin: "builtin", Kinds: 30},
			},
			Since: since,
		}
	})
	defer unregister()

	records := gauges(t, "substrate_repository_records")
	if records["kind=ada.example.com/tasks/task,repository=ada.example.com"] != 3 {
		t.Errorf("records = %v", records)
	}
	packages := gauges(t, "substrate_repository_package_kinds")
	for want, n := range map[string]float64{
		// A sample is named by the package it was imported from.
		"origin=sample,package=samples.substrate.reamde.dev/tasks,repository=ada.example.com": 2,
		"origin=own,package=ada.example.com/garden,repository=ada.example.com":                1,
		"origin=builtin,package=substrate.reamde.dev/core,repository=ada.example.com":         30,
	} {
		if packages[want] != n {
			t.Errorf("packages[%s] = %v, want %v (all: %v)", want, packages[want], n, packages)
		}
	}
	if got := gauges(t, "substrate_repository_first_change_timestamp_seconds")["repository=ada.example.com"]; got != float64(since.Unix()) {
		t.Errorf("first change = %v", got)
	}
}

// Two names that anonymize to one label sum into one series rather than
// failing the scrape with a duplicate.
func TestInventoryCollectorSumsLabelsAlike(t *testing.T) {
	withUsageLabels(t, true, "k")
	unregister := RegisterInventory("ada.example.com", func() Inventory {
		return Inventory{Records: map[string]int64{"ada.example.com/tasks/task": 3}, Packages: []PackageInventory{
			{Package: "ada.example.com/tasks", Origin: "sample", From: "samples.substrate.reamde.dev/tasks", Kinds: 2},
			{Package: "ada.example.com/tasks2", Origin: "sample", From: "samples.substrate.reamde.dev/tasks", Kinds: 2},
		}}
	})
	defer unregister()
	packages := gauges(t, "substrate_repository_package_kinds")
	want := "origin=sample,package=samples.substrate.reamde.dev/tasks,repository=" + Repository("ada.example.com")
	if packages[want] != 4 {
		t.Errorf("packages = %v, want %s = 4", packages, want)
	}
	for labels := range gauges(t, "substrate_repository_records") {
		if strings.Contains(labels, "ada.example.com") {
			t.Errorf("an anonymized series names the repository: %s", labels)
		}
	}
}

// Every repository's inventory is published at once: the registry tells
// collectors apart by their descriptors, and a second repository must not
// take the first one's place, nor its close drop the other's.
func TestInventoryCollectorsOfTwoRepositoriesStandTogether(t *testing.T) {
	withUsageLabels(t, false, "k")
	read := func(n int64) func() Inventory {
		return func() Inventory { return Inventory{Records: map[string]int64{"substrate.reamde.dev/core/contact": n}} }
	}
	closeA := RegisterInventory("a.example.com", read(1))
	closeB := RegisterInventory("b.example.com", read(2))
	defer closeB()
	records := gauges(t, "substrate_repository_records")
	if records["kind=substrate.reamde.dev/core/contact,repository=a.example.com"] != 1 ||
		records["kind=substrate.reamde.dev/core/contact,repository=b.example.com"] != 2 {
		t.Fatalf("records = %v, want both repositories", records)
	}
	closeA()
	records = gauges(t, "substrate_repository_records")
	if _, ok := records["kind=substrate.reamde.dev/core/contact,repository=a.example.com"]; ok {
		t.Errorf("a closed repository is still published: %v", records)
	}
	if records["kind=substrate.reamde.dev/core/contact,repository=b.example.com"] != 2 {
		t.Errorf("closing one repository dropped the other: %v", records)
	}
}

// The spool gauges hold every repository at once too.
func TestSpoolCollectorsOfTwoRepositoriesStandTogether(t *testing.T) {
	closeA := RegisterWebhookSpool("spool-a.example.com", func() SpoolStats { return SpoolStats{Entries: 1} })
	defer closeA()
	closeB := RegisterWebhookSpool("spool-b.example.com", func() SpoolStats { return SpoolStats{Entries: 2} })
	defer closeB()
	entries := gauges(t, "substrate_webhook_spool_entries")
	if entries["repository=spool-a.example.com"] != 1 || entries["repository=spool-b.example.com"] != 2 {
		t.Errorf("spool entries = %v, want both repositories", entries)
	}
}

// A copy's origin is whatever its apply claimed, so only a shipped sample's
// id is shown; any other stays behind the package's own label.
func TestInventoryCollectorHashesAClaimedOrigin(t *testing.T) {
	withUsageLabels(t, true, "k")
	unregister := RegisterInventory("claim.example.com", func() Inventory {
		return Inventory{Packages: []PackageInventory{
			{Package: "claim.example.com/payroll", Origin: "sample", From: "private.example.com/payroll", Kinds: 1},
		}}
	})
	defer unregister()
	for labels := range gauges(t, "substrate_repository_package_kinds") {
		if strings.Contains(labels, "payroll") || strings.Contains(labels, "private.example.com") {
			t.Errorf("an anonymized series names a claimed origin: %s", labels)
		}
	}
}

// Past its budget a repository's new names share one label, and a deleted
// repository's series and budget go.
func TestNameBudgetAndForget(t *testing.T) {
	withUsageLabels(t, false, "k")
	const repo = "budget.example.com"
	for i := range maxNamesPerRepository {
		if got := Name(repo, fmt.Sprintf("%s/p/k%d", repo, i)); got == overflowLabel {
			t.Fatalf("name %d overflowed inside the budget", i)
		}
	}
	if got := Name(repo, repo+"/p/one-too-many"); got != overflowLabel {
		t.Errorf("a name past the budget = %q, want %q", got, overflowLabel)
	}
	if got := Name(repo, repo+"/p/k0"); got != repo+"/p/k0" {
		t.Errorf("a name already admitted = %q", got)
	}
	if got := Name(repo, "substrate.reamde.dev/core/contact"); got != "substrate.reamde.dev/core/contact" {
		t.Errorf("a shipped name past the budget = %q", got)
	}

	Changes.WithLabelValues(repo, repo+"/p/k0", "put", "api").Inc()
	AuthEvents.WithLabelValues(repo, "repository_deleted").Inc()
	ForgetRepository(repo)
	if n := Changes.DeletePartialMatch(prometheus.Labels{"repository": repo}); n != 0 {
		t.Errorf("%d changes series outlived ForgetRepository", n)
	}
	if !AuthEvents.DeleteLabelValues(repo, "repository_deleted") {
		t.Error("ForgetRepository dropped the deletion event it should keep")
	}
	if got := Name(repo, repo+"/p/one-too-many"); got == overflowLabel {
		t.Error("the budget outlived ForgetRepository")
	}
}
