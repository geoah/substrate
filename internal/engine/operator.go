package engine

import (
	"context"

	"github.com/geoah/substrate/internal/substrate"
)

// Operator is the engine as substratectl holds it: the service plus the
// operator hat's methods. Those methods are off substrate.Service on purpose,
// so nothing reachable from the network can call them; only OpenOperator
// hands them out. The assertion below fails the build if *service drifts from
// a signature substratectl calls.
type Operator interface {
	substrate.Service
	// ResetUser replaces a repository's password and TOTP seed (auth.go).
	ResetUser(ctx context.Context, repository, newPassword string) (substrate.TOTPEnrollment, error)
	// RebuildRepository replays a repository's changelog into its fold
	// (rebuild.go).
	RebuildRepository(ctx context.Context, repository string) (RebuildReport, error)
	// VerifyRepository checks a repository's files against its fold
	// (verify.go).
	VerifyRepository(ctx context.Context, repository string) (VerifyReport, error)
	// SnapshotRepository copies a repository's directory under destRoot
	// (snapshot.go).
	SnapshotRepository(ctx context.Context, repository, destRoot string) (SnapshotReport, error)
	// RotateHistoryGeneration mints a new history generation
	// (historygeneration.go).
	RotateHistoryGeneration(ctx context.Context, repository string) (RotateReport, error)
}

var _ Operator = (*service)(nil)
