package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/geoah/substrate/internal/substrate"
)

// Only a refusal of the request's own content sets a spooled request aside;
// every other admission failure, every other class 22 code included, keeps
// it in place for the drain's retry (decision 0206 amending 0167).
func TestSpoolRefusedClassifiesOnlyContentRefusals(t *testing.T) {
	t.Parallel()
	pg := func(code string) error {
		return fmt.Errorf("admit webhook: %w", &pgconn.PgError{Code: code})
	}
	cases := []struct {
		name    string
		err     error
		refused bool
	}{
		{"validation", fmt.Errorf("blob: %w", substrate.ErrValidation), true},
		{"untranslatable character", pg("22P05"), true},
		{"character not in repertoire", pg("22021"), true},
		{"invalid text representation", pg("22P02"), false},
		{"numeric out of range", pg("22003"), false},
		{"string too long", pg("22001"), false},
		{"sql json error", pg("22030"), false},
		{"connection reset", fmt.Errorf("admit webhook: %w", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}), false},
		{"unexpected eof", fmt.Errorf("admit webhook: %w", io.ErrUnexpectedEOF), false},
		{"serialization failure", pg("40001"), false},
		{"canceled", context.Canceled, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := spoolAdmissionRefused(c.err) != ""; got != c.refused {
			t.Errorf("%s: set aside = %v, want %v (err %v)", c.name, got, c.refused, c.err)
		}
	}
	if errors.Is(pg("22P05"), substrate.ErrValidation) {
		t.Fatal("a SQLSTATE error must not read as ErrValidation, or the test proves nothing")
	}
}
