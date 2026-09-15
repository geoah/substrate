package vocabulary_test

// A label/annotation key is `<namespace>/<name>`, both halves lowercase, and
// the grammar is deliberately narrower than a declared name's camelCase:
// `feedbackNote` and `feedbacknote` as two keys nobody can tell apart is a
// trap. That makes the REFUSAL the whole of what a writer has to go on, so
// MetaKeyProblem names the half and the character at fault rather than
// blaming the namespace for every failure (issue 548).

import (
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

func TestMetaKeyProblemAdmitsLegalKeys(t *testing.T) {
	for _, key := range []string{
		"owner/starred",
		"api/tier",
		"mneme/feedbacknote",
		"mneme/feedback-note",
		"mneme/feedback_note",
		"connector:gmail/state",
		"function:web.bundles.example.com:harvest/synced",
		"bundle:samples.substrate.reamde.dev:web/installed",
		"a/b",
	} {
		if !vocabulary.ValidMetaKey(key) {
			t.Errorf("%q should be a legal key", key)
		}
		if problem := vocabulary.MetaKeyProblem(key); problem != "" {
			t.Errorf("%q is legal but was diagnosed: %s", key, problem)
		}
	}
}

// One case per rule the grammar holds. The want strings are the words a
// writer reads, so a message that stops naming the rule fails here.
func TestMetaKeyProblemNamesTheRule(t *testing.T) {
	for _, tc := range []struct {
		key  string
		want []string
	}{
		{"tier", []string{`"tier" must be a namespaced key ("<actor>/<name>")`}},
		{"", []string{`"" must be a namespaced key`}},
		{"a/b/c", []string{"carries 2 slashes", "a key carries one"}},
		{"/starred", []string{"the namespace before the slash is empty"}},
		{"owner/", []string{"the name after the slash is empty"}},
		// The case the issue was filed about: the key IS namespaced, and the
		// message has to say which half is wrong and what to write instead.
		{"mneme/feedbackNote", []string{
			`"mneme/feedbackNote"`, "the name after the slash is lowercase",
			`"feedbacknote"`, `"feedback-note"`,
		}},
		// Nothing to hyphenate: one suggestion, not two of the same word.
		{"mneme/Note", []string{"the name after the slash is lowercase", `"note"`}},
		{"Mneme/starred", []string{"the namespace before the slash is lowercase", `"mneme"`}},
		{"mneme/feedback note", []string{"the name after the slash may not carry \" \"", "digits"}},
		{"mneme/feedback+note", []string{"the name after the slash may not carry \"+\""}},
		{"mneme/feed/back", []string{"carries 2 slashes"}},
		{"mneme/feedback:note", []string{"the name after the slash may not carry \":\""}},
		{"1mneme/starred", []string{
			"the namespace before the slash starts with \"1\"", "lowercase letter",
		}},
		{"mneme/1st", []string{"the name after the slash starts with \"1\"", "lowercase letter"}},
		{"-mneme/starred", []string{"the namespace before the slash starts with \"-\""}},
		{"mne me/starred", []string{"the namespace before the slash may not carry \" \""}},
		{"mneme/naïve", []string{"the name after the slash may not carry \"ï\""}},
	} {
		problem := vocabulary.MetaKeyProblem(tc.key)
		if problem == "" {
			t.Errorf("%q should be refused", tc.key)
			continue
		}
		for _, want := range tc.want {
			if !strings.Contains(problem, want) {
				t.Errorf("MetaKeyProblem(%q) = %q, want it to carry %q", tc.key, problem, want)
			}
		}
	}
}

// Every refusal opens with the key quoted, because both call sites — the
// engine's write path and the declaration loader — prefix it with their own
// context and add nothing about the key itself.
func TestMetaKeyProblemOpensWithTheKey(t *testing.T) {
	for _, key := range []string{
		"tier", "a/b/c", "/starred", "owner/", "mneme/feedbackNote", "Mneme/x", "mneme/1st",
	} {
		if problem := vocabulary.MetaKeyProblem(key); !strings.HasPrefix(problem, `"`+key+`"`) {
			t.Errorf("MetaKeyProblem(%q) = %q, want it to open with the key", key, problem)
		}
	}
}
