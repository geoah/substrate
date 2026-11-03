package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The domain proof lives on the repository record (decision 0208): a start
// mints one and a second start keeps it, a verify stamps the record and
// spends it, and a generic write cannot forge either property.
func TestADomainProofIsMintedOnceAndSpentByAVerify(t *testing.T) {
	ctx := context.Background()
	_, ds := newCoreDataset(t)

	first, err := ds.StartDomainVerification(ctx)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(first.Proof) != 10 || first.VerifiedAt != nil {
		t.Fatalf("start = %+v, want a 10-character proof and no stamp", first)
	}
	again, err := ds.StartDomainVerification(ctx)
	if err != nil || again.Proof != first.Proof {
		t.Fatalf("second start = %+v, %v; want the pending %q", again, err, first.Proof)
	}
	if st, err := ds.DomainState(ctx); err != nil || st.Proof != first.Proof {
		t.Fatalf("state = %+v, %v", st, err)
	}

	if _, err := ds.MarkDomainVerified(ctx, "notthisone"); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("a mark of a proof that is not pending = %v, want ErrConflict", err)
	}
	at, err := ds.MarkDomainVerified(ctx, first.Proof)
	if err != nil {
		t.Fatalf("mark: %v", err)
	}
	// A second check of the proof the first one spent stamps nothing.
	if _, err := ds.MarkDomainVerified(ctx, first.Proof); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("a second mark of a spent proof = %v, want ErrConflict", err)
	}
	st, err := ds.DomainState(ctx)
	if err != nil || st.Proof != "" || st.VerifiedAt == nil || !st.VerifiedAt.Equal(at) {
		t.Fatalf("after mark, state = %+v, %v; want no proof and the stamp %v", st, err, at)
	}
	next, err := ds.StartDomainVerification(ctx)
	if err != nil || next.Proof == "" || next.Proof == first.Proof || next.VerifiedAt == nil {
		t.Fatalf("start after a verify = %+v, %v; want a fresh proof beside the stamp", next, err)
	}

	id := ds.Repository().ID
	for _, props := range []map[string]any{
		{"domainVerifiedAt": "2030-01-01T00:00:00Z"},
		{"domainProof": "forged0000"},
	} {
		if _, err := ds.Patch(ctx, substrate.ActorAPI, "substrate.reamde.dev/core/repository", id,
			substrate.PatchInput{Properties: props}); err == nil {
			t.Errorf("a generic patch wrote %v", props)
		}
	}
}
