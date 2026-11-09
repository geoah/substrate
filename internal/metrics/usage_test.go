package metrics

import (
	"strings"
	"testing"
	"time"

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
	if got := Provider("my-ollama"); got != "my-ollama" {
		t.Errorf("Provider = %q", got)
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
	if Provider("anthropic") != "anthropic" || !strings.HasPrefix(Provider("my-ollama"), "own:") {
		t.Errorf("Provider = %q, %q", Provider("anthropic"), Provider("my-ollama"))
	}
	// The key is what makes a hash: another key, another label.
	SetUsageLabels(true, []byte("other"))
	if Repository("ada.example.com") == repo {
		t.Error("two keys hashed a repository to one label")
	}
}

func TestActorClass(t *testing.T) {
	for actor, want := range map[string]string{
		"console": "console", "substratectl": "cli", "api": "api",
		"substrate": "system", "substrate.oauth": "oauth", "substrate.webhook": "webhook",
		"app:reamde.dev":                            "app",
		"function:substrate.reamde.dev:core:x":      "function",
		"agent:substrate.reamde.dev:assistant:moss": "agent",
		"bundle:core":                               "bundle",
		"mneme-v6-contacts":                         "other",
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
