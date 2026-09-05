package vocabulary_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

// bnPackage renders a minimal one-kind bundle closure in the given package,
// shipping a kind called `widget`.
func bnPackage(pkg string) string { return bnPackageKind(pkg, "widget") }

// bnPackageKind is the same closure with the kind named, so two authorities can
// publish a package of one name without claiming one GraphQL name.
func bnPackageKind(pkg, kind string) string {
	authority, name, _ := strings.Cut(pkg, "/")
	return `kind: substrate.reamde.dev/core/package
metadata:
  id: ` + pkg + `
data:
  authority: ` + authority + `
  package: ` + name + `
  version: 1
---
kind: substrate.reamde.dev/core/bundle
metadata:
  id: ` + pkg + `
data:
  authority: ` + authority + `
  package: ` + name + `
  description: one kind, so the closure is whole
  installs:
    - ` + pkg + `/` + kind + `
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ` + pkg + `/` + kind + `
data:
  authority: ` + authority + `
  package: ` + name + `
  names: {singular: ` + kind + `}
  properties:
    name: {type: string}
`
}

func loadBnPackages(bodies ...string) (*vocabulary.Registry, error) {
	fsys := fstest.MapFS{}
	for i, body := range bodies {
		fsys[string(rune('a'+i))+".yaml"] = &fstest.MapFile{Data: []byte(body)}
	}
	return vocabulary.LoadFS(fsys)
}

// A BUNDLE IS ITS PACKAGE (decision 0047): the document's id is the package
// identity, and any legal authority may publish one. The bundle's name is the
// package's own word, whatever the labels behind it say.
func TestABundleIsThePackageItIsNamedFor(t *testing.T) {
	for _, pkg := range []string{
		"samples.substrate.reamde.dev/llm",
		"tools.example.com/harvest",
		"scraper.example.com/scraper",
	} {
		r, err := loadBnPackages(bnPackage(pkg))
		if err != nil {
			t.Fatalf("%s: %v", pkg, err)
		}
		g, ok := r.PackageByName(pkg)
		if !ok || g.Bundle == nil {
			t.Fatalf("%s: no bundle", pkg)
		}
		_, name, _ := strings.Cut(pkg, "/")
		if g.Bundle.Name != name {
			t.Errorf("%s: name %q, want %q", pkg, g.Bundle.Name, name)
		}
		if g.Bundle.Identity() != pkg {
			t.Errorf("%s: identity %q", pkg, g.Bundle.Identity())
		}
	}
}

// A bundle document whose id is not its package is refused, naming the id it
// must carry: the closure and the thing that installs it are one identity.
func TestABundleIDIsRefusedWhenItIsNotThePackage(t *testing.T) {
	const pkg = "tools.example.com/harvest"
	body := strings.Replace(bnPackage(pkg),
		"kind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: "+pkg+"\n",
		"kind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: "+pkg+"/harvest\n", 1)
	_, err := loadBnPackages(body)
	if err == nil || !strings.Contains(err.Error(), "metadata.id must be") {
		t.Fatalf("a bundle id that is not its package must refuse: %v", err)
	}
}

// A package name is one lowercase word: it is the bundle's name, the actor an
// install writes under and every installed kind's GraphQL prefix, none of
// which admit a hyphen.
func TestAPackageNameIsOneWord(t *testing.T) {
	_, err := loadBnPackages(bnPackage("tools.example.com/my-llm"))
	if err == nil || !strings.Contains(err.Error(), "data.package") {
		t.Fatalf("a hyphenated package name must refuse: %v", err)
	}
}

// Two authorities may publish a package of one name. An install writes under
// `bundle:<authority>:<package>` (records 0025 and 0047), so the two are two
// writers: two attributions, two sets of manager rows, and neither one's writes
// read as the other's trigger echo.
func TestTwoAuthoritiesMayShareAPackageName(t *testing.T) {
	one := "samples.substrate.reamde.dev/llm"
	two := "tools.example.com/llm"
	r, err := loadBnPackages(bnPackageKind(one, "widget"), bnPackageKind(two, "gadget"))
	if err != nil {
		t.Fatalf("a shared package name must load: %v", err)
	}
	g1, _ := r.PackageByName(one)
	g2, _ := r.PackageByName(two)
	if g1.Bundle.Name != "llm" || g2.Bundle.Name != "llm" {
		t.Fatalf("the names must still be shared: %q and %q", g1.Bundle.Name, g2.Bundle.Name)
	}
	a1, a2 := vocabulary.PackageActor(one), vocabulary.PackageActor(two)
	if a1 == a2 {
		t.Fatalf("two packages share the actor %q", a1)
	}
	if a1 != "bundle:samples.substrate.reamde.dev:llm" || a2 != "bundle:tools.example.com:llm" {
		t.Fatalf("actors %q and %q — an actor carries the full authority and the package", a1, a2)
	}
}

// TWO AUTHORITIES SHARING A PACKAGE NAME KEEP TWO GRAPHQL NAMES. The base name
// of an installed kind is `<Package>_<Kind>`; when the package name is claimed
// by two authorities, the authority's first label joins it, for every kind of
// both packages, so a name never depends on load order.
func TestGraphQLNamesDisambiguateBySharedPackageName(t *testing.T) {
	kinds := []vocabulary.GraphQLKind{
		{Identity: "samples.substrate.reamde.dev/tasks/task", Source: vocabulary.SourceInstalled},
		{Identity: "samples.substrate.reamde.dev/people/person", Source: vocabulary.SourceInstalled},
		{Identity: "substrate.reamde.dev/core/token", Source: vocabulary.SourceBuiltin},
	}
	names := vocabulary.GraphQLNames(kinds)
	if got := names["samples.substrate.reamde.dev/tasks/task"]; got != "Tasks_Task" {
		t.Errorf("task = %q, want Tasks_Task", got)
	}
	if got := names["substrate.reamde.dev/core/token"]; got != "Token" {
		t.Errorf("token = %q, want the bare Token", got)
	}
	kinds = append(kinds, vocabulary.GraphQLKind{
		Identity: "acme.example.com/tasks/task", Source: vocabulary.SourceInstalled,
	})
	names = vocabulary.GraphQLNames(kinds)
	if got := names["samples.substrate.reamde.dev/tasks/task"]; got != "Samples_Tasks_Task" {
		t.Errorf("shipped task = %q, want Samples_Tasks_Task", got)
	}
	if got := names["acme.example.com/tasks/task"]; got != "Acme_Tasks_Task" {
		t.Errorf("acme task = %q, want Acme_Tasks_Task", got)
	}
	if got := names["samples.substrate.reamde.dev/people/person"]; got != "People_Person" {
		t.Errorf("a package nobody shares must keep its name: %q", got)
	}
}

// One GraphQL name is still one kind: two SHIPPED packages declaring one kind
// name both want the bare singular, and the second is refused by name.
func TestOneGraphQLNameIsStillOneKind(t *testing.T) {
	_, err := loadBnPackages(
		bnPackageKind("one.example.com/alpha", "widget"),
		bnPackageKind("two.example.com/beta", "widget"),
	)
	if err == nil || !strings.Contains(err.Error(), "graphql name") {
		t.Fatalf("one GraphQL name claimed twice must refuse: %v", err)
	}
}

// EVERY DECLARATION NAMES ITS PACKAGE. A document carrying an authority and no
// package belongs to no group the loader can version, own or quarantine, so it
// is refused by name rather than filed under its authority.
func TestADeclarationWithoutAPackageIsRefused(t *testing.T) {
	_, err := loadBnPackages(`kind: substrate.reamde.dev/core/kind
metadata:
  id: tools.example.com/widget
data:
  authority: tools.example.com
  names: {singular: widget}
`)
	if err == nil || !strings.Contains(err.Error(), "data.authority and data.package are required") {
		t.Fatalf("a declaration with no package must refuse: %v", err)
	}
}

// AN AUTHORITY DECLARES NO MEMBERS. The authority row says who publishes and
// nothing else — the version, the origin and the quarantine mark are the
// package's (decision 0047) — so it loads alone and carries no closure.
func TestAnAuthorityRowOwnsPackagesAndDeclaresNothing(t *testing.T) {
	r, err := loadBnPackages(`kind: substrate.reamde.dev/core/authority
metadata:
  id: tools.example.com
data:
  description: the tools this publisher ships
`, bnPackage("tools.example.com/harvest"))
	if err != nil {
		t.Fatalf("an authority row must load beside its packages: %v", err)
	}
	g, ok := r.PackageByName("tools.example.com")
	if !ok || !g.IsAuthority() {
		t.Fatalf("authority row = %+v", g)
	}
	if len(g.KindOrder) != 0 || g.Bundle != nil {
		t.Fatalf("an authority row declares no members: %+v", g)
	}
	if got := r.PackagesOf("tools.example.com"); len(got) != 1 || got[0] != "tools.example.com/harvest" {
		t.Fatalf("packages of the authority = %v", got)
	}
}

// A PACKAGE HEADER SAYS ITS IDENTITY TWICE, and the two must agree: the id is
// the group key everything downstream buckets by, and the keys are what a row
// reads back as, so a document that spells them apart would load as one package
// and read back as another.
func TestAPackageHeaderIDAndKeysMustAgree(t *testing.T) {
	body := `kind: substrate.reamde.dev/core/package
metadata:
  id: tools.example.com/harvest
data:
  authority: other.example.com
  package: harvest
  version: 1
`
	_, err := loadBnPackages(body)
	if err == nil || !strings.Contains(err.Error(), "but metadata.id says") {
		t.Fatalf("a header whose keys disagree with its id must refuse: %v", err)
	}
}
