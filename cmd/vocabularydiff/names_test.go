package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// The naming rules are tested against the SHIPPED trees and docs/terms.md,
// not a fixture vocabulary: the families and the dead words are the real
// ones, so a test here fails when the tree grows a name the gate would
// refuse, or terms.md gains a word a shipped name carries.

func shippedTerms(t *testing.T) terms {
	t.Helper()
	md, err := os.ReadFile(filepath.Join("..", "..", "docs", "terms.md"))
	if err != nil {
		t.Fatal(err)
	}
	vocab, err := parseTerms(string(md))
	if err != nil {
		t.Fatal(err)
	}
	return vocab
}

func shippedTree(t *testing.T, name string) *tree {
	t.Helper()
	tr, err := loadTree(filepath.Join("..", "..", name))
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return tr
}

// kindDoc is the smallest kind declaration of one id.
func kindDoc(id string) string {
	authority, pkg, name := vocabulary.SplitKindRef(id)
	return fmt.Sprintf(`kind: substrate.reamde.dev/core/kind
metadata:
  id: %s
data:
  authority: %s
  package: %s
  names:
    singular: %s
`, id, authority, pkg, name)
}

// withKinds is the shipped kinds/ tree plus one kind document per id.
func withKinds(t *testing.T, ids ...string) *tree {
	t.Helper()
	tr := shippedTree(t, "kinds")
	for _, id := range ids {
		if err := tr.loadFile(id+".yaml", []byte(kindDoc(id))); err != nil {
			t.Fatal(err)
		}
	}
	return tr
}

// withoutKinds copies a tree's declarations minus the named kinds, which is
// all nameViolations reads.
func withoutKinds(tr *tree, ids ...string) *tree {
	decls := make(map[string]decl, len(tr.decls))
	for k, d := range tr.decls {
		decls[k] = d
	}
	for _, id := range ids {
		delete(decls, declKey(vocabulary.DocKind, id))
	}
	return &tree{decls: decls}
}

func TestDeadWordsAreReadOffTerms(t *testing.T) {
	vocab := shippedTerms(t)
	got := map[string]string{}
	for _, d := range vocab.dead {
		got[d.word] = d.replacement
	}
	for _, want := range []string{"entity", "type", "group", "log", "edge", "relationship", "tenant", "identity", "integration", "configtype", "singleton"} {
		if _, ok := got[want]; !ok {
			t.Errorf("terms.md's dead word %q was not read: %v", want, got)
		}
	}
	// The live word after each arrow is never read as dead.
	for _, live := range []string{"record", "kind", "authority", "trait", "vocabulary", "changelog", "bundle", "reference", "provider", "input"} {
		if _, ok := got[live]; ok {
			t.Errorf("the live word %q was read as dead: %v", live, got)
		}
	}
	if got["entity"] != "record" || got["edge"] != "reference" {
		t.Errorf("a replacement was misread: entity → %q, edge → %q", got["entity"], got["edge"])
	}
	// A narrowing for a word terms.md does not list narrows nothing, and would
	// hide that the word left the page.
	for word := range honestCompounds {
		if _, ok := got[word]; !ok {
			t.Errorf("honestCompounds narrows %q, which terms.md does not list as dead", word)
		}
	}
	// The live words are the one-word replacements the tables define, so
	// `nothing` (what tenant and identity became) is not one.
	if want := "authority bundle changelog input kind provider record reference trait vocabulary"; strings.Join(sortedStrings(vocab.live), " ") != want {
		t.Errorf("live words %v, want %s", vocab.live, want)
	}
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func TestParseDeadWordsRefusesAPageWithoutThem(t *testing.T) {
	if _, err := parseTerms("# Terms\n\nNo such paragraph.\n"); err == nil {
		t.Fatal("a page with no dead words parsed")
	}
	if _, err := parseTerms("# Terms\n\nDead words, and what replaced them: none.\n"); err == nil {
		t.Fatal("a paragraph naming no word parsed")
	}
}

// Every kind in both shipped trees, checked as if it were the one the diff
// adds: the gate passes the tree as it stands.
func TestShippedTreesPassTheNameGate(t *testing.T) {
	vocab := shippedTerms(t)
	for _, name := range []string{"kinds", "samples"} {
		tr := shippedTree(t, name)
		checked := 0
		for _, k := range sortedKeys(tr.decls) {
			d := tr.decls[k]
			if d.kind != vocabulary.DocKind {
				continue
			}
			checked++
			if got := nameViolations(withoutKinds(tr, d.id), tr, vocab); len(got) != 0 {
				t.Errorf("%s: shipped kind %s fails the name gate: %v", name, d.id, got)
			}
		}
		if checked < 10 {
			t.Fatalf("%s: only %d kinds checked; the tree did not load", name, checked)
		}
	}
}

func TestWritePolicyBesideRecordPatchPolicyIsRefused(t *testing.T) {
	const id = "substrate.reamde.dev/core/writepolicy"
	got := nameViolations(shippedTree(t, "kinds"), withKinds(t, id), shippedTerms(t))
	if len(got) != 1 {
		t.Fatalf("want one violation, got %v", got)
	}
	for _, want := range []string{id, `ends in "policy"`, "substrate.reamde.dev/core/recordpatchpolicy", `stem "recordpatch"`} {
		if !strings.Contains(got[0], want) {
			t.Fatalf("the violation does not say %q: %s", want, got[0])
		}
	}
}

func TestDeadWordInANewKindIsRefused(t *testing.T) {
	for _, tc := range []struct{ id, word, where string }{
		{"substrate.reamde.dev/core/recordentity", "entity", "name"},
		{"substrate.reamde.dev/core/entities", "entity", "name"},
		{"substrate.reamde.dev/core/relationshiplink", "relationship", "name"},
		{"substrate.reamde.dev/core/type", "type", "name"},
		{"substrate.reamde.dev/core/recordtype", "type", "name"},
		{"substrate.reamde.dev/core/kindgroups", "group", "name"},
		{"substrate.reamde.dev/core/incomingreference", "incoming", "name"},
		{"substrate.reamde.dev/integrations/thing", "integration", "package"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			got := nameViolations(shippedTree(t, "kinds"), withKinds(t, tc.id), shippedTerms(t))
			wantViolation(t, got, fmt.Sprintf("kind %s carries the dead word %q in its %s", tc.id, tc.word, tc.where))
		})
	}
}

func TestAnHonestCompoundPasses(t *testing.T) {
	// A narrowed word inside a compound that no live word completes is the
	// review's to read, not the gate's.
	for _, id := range []string{
		"substrate.reamde.dev/core/plurality",
		"substrate.reamde.dev/core/knowledge",
		"substrate.reamde.dev/core/blobtype",
	} {
		if got := nameViolations(shippedTree(t, "kinds"), withKinds(t, id), shippedTerms(t)); len(got) != 0 {
			t.Errorf("%s: %v", id, got)
		}
	}
}

func TestAKindOwnedNameIsNotOffTheFamily(t *testing.T) {
	// A name starting with another kind of the package is spelled after its
	// own owner.
	for _, id := range []string{
		"substrate.reamde.dev/core/blobpolicy",
		"substrate.reamde.dev/core/repositoryexportrequest",
	} {
		if got := nameViolations(shippedTree(t, "kinds"), withKinds(t, id), shippedTerms(t)); len(got) != 0 {
			t.Errorf("%s: %v", id, got)
		}
	}
	// The same ending under no owner is off the family, and so is one under
	// the shorter `record` stem, which drops the `patch` the family spells.
	for _, tc := range []struct{ id, suffix string }{
		{"substrate.reamde.dev/core/exportrequest", "request"},
		{"substrate.reamde.dev/core/recordwritepolicy", "policy"},
	} {
		got := nameViolations(shippedTree(t, "kinds"), withKinds(t, tc.id), shippedTerms(t))
		wantViolation(t, got, fmt.Sprintf("kind %s ends in %q", tc.id, tc.suffix))
	}
}

func TestAKindStemMakesNoFamily(t *testing.T) {
	// Google's calendarevent, calendarseries and calendarsync share the stem
	// `calendar`, which is a kind: each is a thing OF a calendar, so a
	// gmailsync beside them is the sync of something else.
	const id = "providers.substrate.reamde.dev/google/gmailsync"
	if got := nameViolations(shippedTree(t, "kinds"), withKinds(t, id), shippedTerms(t)); len(got) != 0 {
		t.Fatalf("gmailsync beside calendarsync is refused: %v", got)
	}
}

func TestARenameIsHeldToTheFamilyItLeaves(t *testing.T) {
	vocab := shippedTerms(t)
	base := shippedTree(t, "kinds")
	const policy, request = "substrate.reamde.dev/core/recordpatchpolicy", "substrate.reamde.dev/core/recordpatchrequest"

	// recordpatchpolicy renamed away while recordpatchrequest stays.
	head := withoutKinds(withKinds(t, "substrate.reamde.dev/core/writepolicy"), policy)
	wantViolation(t, nameViolations(base, head, vocab), `substrate.reamde.dev/core/writepolicy ends in "policy"`)

	// The whole family renamed: no member is left for the new names to join.
	head = withoutKinds(withKinds(t, "substrate.reamde.dev/core/writepolicy", "substrate.reamde.dev/core/writerequest"), policy, request)
	if got := nameViolations(base, head, vocab); len(got) != 0 {
		t.Fatalf("a family renamed whole is held to its old stem: %v", got)
	}
}

func TestKindFamilies(t *testing.T) {
	words := func(ws ...string) map[string]bool {
		out := map[string]bool{}
		for _, w := range ws {
			out[w] = true
		}
		return out
	}
	for _, tc := range []struct {
		name  string
		kinds []string
		words map[string]bool
		want  string
	}{
		{
			"a stem and the stem inside it",
			[]string{"recordpatchpolicy", "recordpatchrequest", "recordsplit"},
			words("record", "patch", "policy", "request", "split"),
			"record recordpatch",
		},
		{
			// `ation` is no word, so information is not inform plus a word.
			"an ending that is no word",
			[]string{"information", "informant"},
			words("inform", "ant"),
			"",
		},
		{
			"a stem that is no word",
			[]string{"xqzvwkpolicy", "xqzvwkrequest"},
			words("policy", "request"),
			"",
		},
		{
			"a stem that is a kind",
			[]string{"conversation", "conversationmessage", "conversationthread"},
			words("conversation", "message", "thread"),
			"",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range tc.kinds {
				tc.words[k] = true
			}
			var stems []string
			for _, f := range kindFamilies(tc.kinds, tc.words) {
				stems = append(stems, f.stem)
			}
			if got := strings.Join(stems, " "); got != tc.want {
				t.Fatalf("stems %q, want %q", got, tc.want)
			}
		})
	}
}
