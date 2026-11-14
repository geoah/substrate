package api

// An error that follows a commit that may have landed carries the reason
// in_doubt, on the status its cause decides, and its cause's text never
// reaches a 500.

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestAWriteInDoubtCarriesItsReason(t *testing.T) {
	t.Parallel()
	lost := fmt.Errorf("%w: %w", substrate.ErrWriteInDoubt, errors.New("driver: bad connection to the database"))
	status, p := problemFor(lost)
	if status != http.StatusInternalServerError || p.Code != codeInternal || p.Reason != substrate.ReasonInDoubt || p.Message != inDoubtMessage {
		t.Fatalf("a lost commit answer maps to %d %+v, want 500 internal in_doubt with the fixed message", status, p)
	}

	latched := fmt.Errorf("%w: %w", substrate.ErrWriteInDoubt,
		fmt.Errorf("%w: the directory fell behind", substrate.ErrRestartRequired))
	status, p = problemFor(latched)
	if status != http.StatusServiceUnavailable || p.Code != codeUnavailable || p.Reason != substrate.ReasonInDoubt {
		t.Fatalf("a commit that latched the repository maps to %d %+v, want 503 unavailable in_doubt", status, p)
	}

	// The same causes without the mark keep their answers.
	if status, p := problemFor(errors.New("driver: bad connection")); status != http.StatusInternalServerError || p.Reason != "" {
		t.Fatalf("an unmarked fault maps to %d %+v, want 500 with no reason", status, p)
	}
}
