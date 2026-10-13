package engine_test

import (
	"testing"

	"github.com/geoah/substrate/internal/testdb"
)

// A kind binding core's `revised` holds its `revision` the way an observed
// kind does (decision 0162): a stale delivery of a night's sleep is refused
// rather than reverting the night to an older upstream version, an equal
// revision is admitted, and a stored one is never removed.
func TestRevisedKindRefusesAFallingRevision(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	sleep := testdb.Repository(t) + "/health/sleep"
	night := map[string]any{
		"at": "2026-10-09T22:40:00+03:00", "endsAt": "2026-10-10T06:55:00+03:00",
		"timeAsleep": "PT7H20M", "revision": 5,
	}
	if _, err := r.put(t, sleep, "n", night); err != nil {
		t.Fatalf("put: %v", err)
	}
	night["timeAsleep"] = "PT7H25M"
	if _, err := r.put(t, sleep, "n", night); err != nil {
		t.Errorf("an equal revision: %v", err)
	}
	night["revision"] = 3
	night["timeAsleep"] = "PT3H"
	_, err := r.put(t, sleep, "n", night)
	refused(t, "a lower revision", err, "props.revision: 3 is below the stored revision 5")
	if got := r.get(t, sleep, "n").Properties["timeAsleep"]; got != "PT7H25M" {
		t.Errorf("the stale write landed: timeAsleep %v", got)
	}
	_, err = r.patch(t, sleep, "n", map[string]any{"revision": nil})
	refused(t, "removing the revision", err, "props.revision: the stored revision 5 is never removed")
}
