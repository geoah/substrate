package engine

import "testing"

// TestARecordWalkIsHeldOncePerTrigger: one walk per trigger at a time in a
// process, other triggers unaffected, and a walking entry survives the
// prune a pass runs over triggers it no longer loads (decision 0182).
func TestARecordWalkIsHeldOncePerTrigger(t *testing.T) {
	t.Parallel()
	ds := &dataset{}
	release, ok := ds.startRecordWalk("a")
	if !ok {
		t.Fatal("the first walk of a was refused")
	}
	if _, ok := ds.startRecordWalk("a"); ok {
		t.Fatal("a second walk of a started beside the first")
	}
	releaseB, ok := ds.startRecordWalk("b")
	if !ok {
		t.Fatal("a walk of b was refused while a walked")
	}
	releaseB()

	ds.pruneActivity(nil)
	if ds.activity["a"] == nil {
		t.Fatal("the prune dropped a trigger that is walking")
	}
	if ds.activity["b"] != nil {
		t.Fatal("the prune kept a trigger nothing walks or delivers")
	}
	if _, ok := ds.startRecordWalk("a"); ok {
		t.Fatal("a second walk of a started after the prune")
	}

	release()
	again, ok := ds.startRecordWalk("a")
	if !ok {
		t.Fatal("a walk of a was refused after the first was released")
	}
	again()
	ds.pruneActivity(nil)
	if ds.activity["a"] != nil {
		t.Fatal("the prune kept a released trigger")
	}
}
