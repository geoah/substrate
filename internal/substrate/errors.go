package substrate

import (
	"errors"
	"fmt"
)

// Sentinel errors. The engine wraps these (errors.Is-matchable) with
// human-readable detail; the API layer maps them to status codes.
var (
	ErrNotFound   = errors.New("substrate: not found")
	ErrConflict   = errors.New("substrate: version conflict") // CAS mismatch
	ErrGuard      = errors.New("substrate: operation not allowed here")
	ErrValidation = errors.New("substrate: validation failed")
	ErrForbidden  = errors.New("substrate: forbidden") // e.g. foreign label namespace, system type write
	ErrAuth       = errors.New("substrate: authentication failed")
)

// ValidationError carries per-field detail for the UI's YAML editor.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%v: %v", ErrValidation, e.Problems)
}
func (e *ValidationError) Unwrap() error { return ErrValidation }
