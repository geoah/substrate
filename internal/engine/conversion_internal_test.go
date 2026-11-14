package engine

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The chain this binary carries is numbered from 2 with no gap, converts to
// the dialect the binary stamps, and has one file per step named for it,
// `conversion_NNNN_<name>.go`, the way the repository migrations do
// (TestRepositoryMigrationsMatchTheirFiles).
func TestTheConversionChainIsNumberedAndFiled(t *testing.T) {
	t.Parallel()
	if err := checkConversionSteps(conversionSteps); err != nil {
		t.Fatal(err)
	}
	if got := chainDialect(conversionSteps); got != maxChangelogDialect {
		t.Fatalf("the chain converts to dialect %d and the binary stamps %d: a step raises maxChangelogDialect to its own dialect", got, maxChangelogDialect)
	}
	files, err := filepath.Glob("conversion_[0-9][0-9][0-9][0-9]_*.go")
	if err != nil {
		t.Fatal(err)
	}
	named := map[string]bool{}
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			named[f] = true
		}
	}
	for _, s := range conversionSteps {
		f := conversionFile(s)
		if !named[f] {
			t.Errorf("step %d (%s) has no file %s", s.Dialect, s.Name, f)
		}
		delete(named, f)
	}
	for f := range named {
		t.Errorf("%s names no step of conversionSteps", f)
	}
}

func conversionFile(s conversionStep) string {
	return fmt.Sprintf("conversion_%04d_%s.go", s.Dialect, s.Name)
}

// checkConversionSteps refuses a chain a step could not be found in by its
// number: one that starts anywhere but 2, skips, repeats a name or has a step
// that rewrites nothing.
func TestAConversionChainIsRefusedOutOfOrder(t *testing.T) {
	t.Parallel()
	line := func(*changelogfile.Entry) error { return nil }
	for name, steps := range map[string][]conversionStep{
		"starts at 3":    {{Dialect: 3, Name: "a", Line: line}},
		"skips one":      {{Dialect: 2, Name: "a", Line: line}, {Dialect: 4, Name: "b", Line: line}},
		"repeats a name": {{Dialect: 2, Name: "a", Line: line}, {Dialect: 3, Name: "a", Line: line}},
		"has no line":    {{Dialect: 2, Name: "a"}},
	} {
		if err := checkConversionSteps(steps); err == nil {
			t.Errorf("a chain that %s was admitted", name)
		}
	}
	ok := []conversionStep{{Dialect: 2, Name: "a", Line: line}, {Dialect: 3, Name: "b", Line: line}}
	if err := checkConversionSteps(ok); err != nil {
		t.Fatalf("a chain numbered 2, 3 was refused: %v", err)
	}
	if got := chainDialect(ok); got != 3 {
		t.Fatalf("the chain converts to %d, want 3", got)
	}
}

// A repository runs the steps above its stamp, and an unstamped changelog
// holds dialect-1 lines, so it runs them all.
func TestARepositoryRunsTheStepsAboveItsStamp(t *testing.T) {
	t.Parallel()
	line := func(*changelogfile.Entry) error { return nil }
	steps := []conversionStep{{Dialect: 2, Name: "a", Line: line}, {Dialect: 3, Name: "b", Line: line}}
	for stored, want := range map[int]string{0: "2 (a), 3 (b)", 1: "2 (a), 3 (b)", 2: "3 (b)", 3: ""} {
		if got := conversionNames(pendingConversions(steps, stored)); got != want {
			t.Errorf("stamped %d: pending %q, want %q", stored, got, want)
		}
	}
}

// The table keeps a line's token in one column whatever key the line held it
// under, so the walk reads the key from the row's checksum: a row from before the links
// reaches the step with its `principal`, an empty one included, which is how
// the actor links tell a line to convert; a row this binary wrote reaches it
// with its `token`. A row whose checksum no frame reproduces reaches no step:
// the walk would stamp whatever the step left with a checksum of its own.
func TestTheWalkHandsAStepTheLineInTheFrameItWasWritten(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	row := func(e changelogfile.Entry) checksumRow {
		_, sum, err := changelogfile.Encode(e)
		if err != nil {
			t.Fatal(err)
		}
		return checksumRow{entry: pendingEntry{
			Seq: e.Seq, TS: e.TS, Actor: e.Actor, OnBehalfOf: e.OnBehalfOf, Token: lineToken(e),
			Op: e.Op, RecordID: e.RecordID, Kind: e.Kind, Txn: e.Txn, PayloadText: e.Payload,
		}, hash: sum[:]}
	}
	base := changelogfile.Entry{Seq: 7, Txn: 7, TS: ts, Op: "put", Kind: "k.example.com/p/k", RecordID: "r", Payload: []byte(`{}`)}
	tombstone := editedLine(base, func(e *changelogfile.Entry) {
		e.Op, e.Payload = "delete", []byte(`{"fold":[{"kind":"tombstone","ref":"k.example.com/p/k","id":"r"}]}`)
	})
	for name, c := range map[string]struct {
		line                changelogfile.Entry
		principal, token    string
		carriesPrincipalKey bool
	}{
		"before the links, behind a token": {line: editedLine(base, func(e *changelogfile.Entry) { e.Actor, e.Principal, e.HasPrincipal = "api", "tok", true }), principal: "tok", carriesPrincipalKey: true},
		"before the links, behind none":    {line: editedLine(base, func(e *changelogfile.Entry) { e.Actor, e.HasPrincipal = "substrate", true }), carriesPrincipalKey: true},
		"a tombstone before the links":     {line: editedLine(tombstone, func(e *changelogfile.Entry) { e.Actor, e.Principal, e.HasPrincipal = "api", "tok", true }), principal: "tok", carriesPrincipalKey: true},
		"linked, behind a token":           {line: editedLine(base, func(e *changelogfile.Entry) { e.Actor, e.Token = vocabulary.ActorID(substrate.ActorPerson), "tok" }), token: "tok"},
		"linked, behind none":              {line: editedLine(base, func(e *changelogfile.Entry) { e.Actor = vocabulary.ActorID(substrate.ActorSystem) })},
	} {
		got, err := walkedLine(row(c.line))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got.HasPrincipal != c.carriesPrincipalKey || got.Principal != c.principal || got.Token != c.token {
			t.Errorf("%s: the walk hands principal %q (key %v) and token %q, want %q (key %v) and %q",
				name, got.Principal, got.HasPrincipal, got.Token, c.principal, c.carriesPrincipalKey, c.token)
		}
		_, want, _ := changelogfile.Encode(c.line)
		if _, sum, _ := changelogfile.Encode(got); sum != want {
			t.Errorf("%s: the line the walk hands does not reproduce the row's checksum", name)
		}

		damaged := row(c.line)
		damaged.hash = slices.Clone(damaged.hash)
		damaged.hash[0] ^= 0xff
		if _, err := walkedLine(damaged); err == nil || !strings.Contains(err.Error(), "seq 7:") {
			t.Errorf("%s, its checksum damaged: the walk hands it to a step (%v), want it refused naming seq 7", name, err)
		}
	}
}

func editedLine(e changelogfile.Entry, edit func(*changelogfile.Entry)) changelogfile.Entry {
	edit(&e)
	return e
}
