package engine

// Benchmarks of the engine's pure hot paths, the ones every write, every
// dispatcher look or every mapped read runs, so a change to one has a
// before and an after (`mise run bench`). None needs a database.

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/vocabulary"
)

// BenchmarkDueFires is a schedule's due occurrences, a year after the
// rule's anchor, for each rule the cursor is held to a fresh walk on: cold
// is the first walk after an open, from the anchor, which every look and
// every status read paid before the cursor (#874); warm is a walk resumed
// from the cursor.
func BenchmarkDueFires(b *testing.B) {
	now := time.Date(2026, 10, 13, 12, 0, 30, 0, time.UTC)
	anchor := now.Add(-365 * 24 * time.Hour)
	after := now.Add(-90 * time.Second)
	for _, rule := range scheduleCursorRules {
		src := &scheduleSource{Recurrence: rule.recurrence, Timezone: rule.timezone}
		b.Run(rule.recurrence+"/cold", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				ds := &dataset{}
				if _, err := ds.dueFires("t", src, anchor, after, now, scheduleOwedCap); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(rule.recurrence+"/warm", func(b *testing.B) {
			ds := &dataset{}
			if _, err := ds.dueFires("t", src, anchor, after, now, scheduleOwedCap); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ds.dueFires("t", src, anchor, after, now, scheduleOwedCap); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// benchRow is a stored row of a record-shaped kind: scalars, a nested
// object, a list and a reference among props props.
func benchRow(props int) *erow {
	p := make(map[string]any, props)
	for i := range props {
		switch i % 5 {
		case 0:
			p["name"+strconv.Itoa(i)] = fmt.Sprintf("value %d with some prose in it", i)
		case 1:
			p["count"+strconv.Itoa(i)] = float64(i)
		case 2:
			p["tags"+strconv.Itoa(i)] = []any{"alpha", "beta", "gamma", float64(i)}
		case 3:
			p["address"+strconv.Itoa(i)] = map[string]any{"street": "1 Main St", "city": "Athens", "zip": strconv.Itoa(10000 + i)}
		case 4:
			p["owner"+strconv.Itoa(i)] = map[string]any{"ref": "ada.example.com/people/person/p" + strconv.Itoa(i)}
		}
	}
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	return &erow{
		ID: "r1", Kind: "ada.example.com/tasks/task", Title: "a task",
		States: map[string]string{"status": "open"}, At: &at,
		Props: p, Labels: map[string]any{"team": "core"},
	}
}

// BenchmarkDiffRow is the delta every write measures between the stored row
// and the written one: a forty-property row with one property changed.
func BenchmarkDiffRow(b *testing.B) {
	before := benchRow(40)
	after := before.clone()
	after.Props["name0"] = "a new value"
	b.ReportAllocs()
	for b.Loop() {
		if d := diffRow(before, after); d == nil || len(d.Set) != 1 {
			b.Fatalf("delta %+v, want the one property", d)
		}
	}
}

// BenchmarkDeriveRefs is a write's refs rows: a kind with a scalar
// reference, a repeated one of fifty and a list of objects each holding a
// reference.
func BenchmarkDeriveRefs(b *testing.B) {
	const target = "ada.example.com/people/person"
	ref := func() *vocabulary.Property {
		return &vocabulary.Property{Datatype: vocabulary.DatatypeReference, To: target}
	}
	seen := ref()
	seen.Repeated = true
	kind := &vocabulary.Kind{
		Identity:  "ada.example.com/tasks/task",
		PropOrder: []string{"owner", "seen", "steps"},
		Props: map[string]*vocabulary.Property{
			"owner": ref(),
			"seen":  seen,
			"steps": {Datatype: vocabulary.DatatypeObject, Repeated: true, Fields: map[string]*vocabulary.Property{"by": ref()}},
		},
	}
	seenValues := make([]any, 50)
	steps := make([]any, 20)
	for i := range seenValues {
		seenValues[i] = map[string]any{"ref": target + "/p" + strconv.Itoa(i)}
	}
	for i := range steps {
		steps[i] = map[string]any{"by": map[string]any{"ref": target + "/s" + strconv.Itoa(i)}}
	}
	props := map[string]any{
		"owner": map[string]any{"ref": target + "/ada"},
		"seen":  seenValues,
		"steps": steps,
	}
	b.ReportAllocs()
	for b.Loop() {
		if rows := deriveRefs(kind, props); len(rows) != 71 {
			b.Fatalf("%d refs rows, want 71", len(rows))
		}
	}
}

// BenchmarkSelectValueUnion is a union property's value on a mapped record:
// three sources' lists of forty items each, overlapping, deduplicated item
// against item.
func BenchmarkSelectValueUnion(b *testing.B) {
	cands := make([]contribution, 3)
	for c := range cands {
		items := make([]any, 40)
		for i := range items {
			items[i] = map[string]any{"address": fmt.Sprintf("user%d@example.com", i+c*20), "label": "work"}
		}
		cands[c] = contribution{actor: "source" + strconv.Itoa(c), value: items}
	}
	b.ReportAllocs()
	for b.Loop() {
		v, _ := selectValue(true, cands)
		if items, ok := v.([]any); !ok || len(items) != 80 {
			b.Fatalf("union %v, want 80 distinct items", v)
		}
	}
}
