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

// A page converts on every core and comes back in seq order: each line as
// the step left it, flagged changed only where its checksum moved, and a walk
// stops at the first line in seq order that does not convert, as a walk a
// line at a time would.
func TestAPageConvertsInOrderAndStopsAtItsFirstFailure(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC)
	actor := vocabulary.ActorID(substrate.ActorSystem)
	var page []checksumRow
	for seq := int64(1); seq <= 2*rebuildBatch; seq++ {
		e := changelogfile.Entry{Seq: seq, Txn: seq, TS: ts, Actor: actor, Op: "put", Kind: "k.example.com/p/k", RecordID: fmt.Sprintf("r%d", seq), Payload: []byte(`{}`)}
		_, sum, err := changelogfile.Encode(e)
		if err != nil {
			t.Fatal(err)
		}
		page = append(page, checksumRow{entry: pendingEntry{
			Seq: e.Seq, TS: e.TS, Actor: e.Actor, Op: e.Op, RecordID: e.RecordID, Kind: e.Kind, Txn: e.Txn, PayloadText: e.Payload,
		}, hash: sum[:]})
	}
	step := conversionStep{Dialect: 9, Name: "test"}
	// Every third line gains a property; the rest stay as they were.
	rewrite := func(e *changelogfile.Entry) error {
		if e.Seq%3 == 0 {
			e.Payload = []byte(`{"seen":true}`)
		}
		return nil
	}
	lines := convertPage(page, step, rewrite)
	if len(lines) != len(page) {
		t.Fatalf("%d lines came back from a page of %d", len(lines), len(page))
	}
	for i, l := range lines {
		want := int64(i + 1)
		if l.err != nil || l.e.Seq != want {
			t.Fatalf("line %d is seq %d (%v), want seq %d", i, l.e.Seq, l.err, want)
		}
		if l.changed != (want%3 == 0) {
			t.Errorf("seq %d: changed %v, want %v", want, l.changed, want%3 == 0)
		}
		if _, sum, _ := changelogfile.Encode(l.e); sum != l.sum {
			t.Errorf("seq %d: the checksum is not the line's", want)
		}
	}

	failing := func(e *changelogfile.Entry) error {
		if e.Seq >= 400 && e.Seq%7 == 0 {
			return fmt.Errorf("refused")
		}
		return nil
	}
	lines = convertPage(page, step, failing)
	var first error
	for _, l := range lines {
		if l.err != nil {
			first = l.err
			break
		}
	}
	if first == nil || !strings.Contains(first.Error(), "at seq 406:") {
		t.Errorf("the first failure in seq order is %v, want seq 406's", first)
	}

	moving := func(e *changelogfile.Entry) error { e.Txn++; return nil }
	if l := convertPage(page[:1], step, moving)[0]; l.err == nil || !strings.Contains(l.err.Error(), "moved seq 1") {
		t.Errorf("a step that moves a line's txn converts (%v), want it refused", l.err)
	}
}

// A step that rewrites no line is walked only where no step before it in the
// same conversion was, so every conversion holds every row to its checksum
// once and no more often than its rewriting steps need.
func TestAStepThatRewritesNoLineIsWalkedOnlyFirst(t *testing.T) {
	t.Parallel()
	for stored, want := range map[int]int{1: 5, 2: 4, 5: 1, 6: 1, 7: 1} {
		steps := pendingConversions(conversionSteps, stored)
		if got := walkedSteps(steps); got != want {
			t.Errorf("a history at dialect %d walks %d of its %d steps, want %d", stored, got, len(steps), want)
		}
	}
	if got := walkedSteps(pendingConversions(conversionSteps, maxChangelogDialect)); got != 0 {
		t.Errorf("a history at the chain's dialect walks %d steps, want none", got)
	}
}
