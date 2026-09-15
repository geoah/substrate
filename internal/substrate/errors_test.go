package substrate_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A failed accept is one refusal with one code. AcceptConflictError matches
// ErrConflict so every caller that already knew that sentinel keeps working,
// and it hides the inner refusal it was built from: were it to unwrap, the
// API's own ordering would answer a no-op accept 422 and a vanished target
// 404, and neither is what the caller asked for (#553).
func TestAcceptConflictErrorIsOneConflict(t *testing.T) {
	err := error(&substrate.AcceptConflictError{
		Reason: "the diff applied no change — the target already matches the proposed values",
	})

	if !errors.Is(err, substrate.ErrConflict) {
		t.Fatal("a failed accept must match ErrConflict")
	}
	for _, sentinel := range []error{
		substrate.ErrValidation, substrate.ErrNotFound, substrate.ErrGuard, substrate.ErrForbidden,
	} {
		if errors.Is(err, sentinel) {
			t.Fatalf("a failed accept must not match %v", sentinel)
		}
	}
	var ve *substrate.ValidationError
	if errors.As(err, &ve) {
		t.Fatal("a failed accept must not surface per-field validation problems")
	}
	if strings.Contains(err.Error(), "version conflict") {
		t.Fatalf("the message claims a version conflict: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "applied no change") {
		t.Fatalf("the message drops its reason: %q", err.Error())
	}
}
