package engine

import (
	"testing"
	"time"
)

// The time a turn thought runs from its first streamed piece of reasoning to
// its last, the span the console's live count measures too; a one-shot
// completion keeps the text and no time.
func TestReasoningMillisSpansTheFirstPieceToTheLast(t *testing.T) {
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	var r turnReasoning
	r.add("let me ", t0)
	r.add("think", t0.Add(1500*time.Millisecond))
	r.add(" more", t0.Add(4*time.Second))
	r.settle("ignored: the stream already carried it")
	props := r.stamp(map[string]any{})
	if props["reasoning"] != "let me think more" || props["reasoningMillis"] != int64(4000) {
		t.Fatalf("props = %v", props)
	}

	var once turnReasoning
	once.add("one piece", t0)
	if got := once.stamp(map[string]any{})["reasoningMillis"]; got != int64(0) {
		t.Fatalf("a single piece took %v", got)
	}

	var oneShot turnReasoning
	oneShot.settle("whole")
	props = oneShot.stamp(map[string]any{})
	if _, timed := props["reasoningMillis"]; timed || props["reasoning"] != "whole" {
		t.Fatalf("one-shot props = %v", props)
	}
}
