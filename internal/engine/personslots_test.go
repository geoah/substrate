package engine

import (
	"slices"
	"testing"
)

// A resume that finds every slot taken is kept with the slots, once per
// thread, and handed back by the give that frees one: the refusal and the
// wait are one step, so a slot freed between them is not missed.
func TestAWaitForASlotIsHandedBackByTheGiveThatFreesOne(t *testing.T) {
	t.Parallel()
	var p personSlots
	if !p.take("ada", 2) || !p.takeOrWait("ada", 2, deferredResume{thread: "t0", resolution: "r0"}) {
		t.Fatal("two slots of two were not free")
	}
	for _, r := range []deferredResume{{"t1", "r1"}, {"t2", "r2"}, {"t1", "r3"}} {
		if p.takeOrWait("ada", 2, r) {
			t.Fatalf("a third slot of two was taken for %+v", r)
		}
	}
	// Another person's give hands back nothing of ada's.
	if !p.take("bo", 2) {
		t.Fatal("bo's slots were not free")
	}
	if waited := p.give("bo"); len(waited) != 0 {
		t.Fatalf("bo's give handed back %+v", waited)
	}
	// One thread waits once, for its first resolution.
	waited := p.give("ada")
	if !slices.Equal(waited, []deferredResume{{"t1", "r1"}, {"t2", "r2"}}) {
		t.Fatalf("ada's give handed back %+v", waited)
	}
	if again := p.give("ada"); len(again) != 0 {
		t.Fatalf("a second give handed the same waits back: %+v", again)
	}
	// What still waits is found by the sweep's drain, whoever it waits for.
	if !p.take("ada", 1) || p.takeOrWait("ada", 1, deferredResume{thread: "t3", resolution: "r4"}) {
		t.Fatal("the one slot of one")
	}
	if all := p.drainWaiting(); !slices.Equal(all, []deferredResume{{"t3", "r4"}}) || len(p.drainWaiting()) != 0 {
		t.Fatalf("the drain handed back %+v", all)
	}
}
