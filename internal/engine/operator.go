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
	// ResetUser replaces an identity's password and TOTP seed, or gives a
	// repository no identity may open one, named createAs (auth.go).
	ResetUser(ctx context.Context, name, newPassword, createAs string) (substrate.TOTPEnrollment, error)
	// AddPerson lets an identity open a repository, holding roles: a person
	// actor of theirs there, then the server's index of it (people.go).
	// Nothing on the network adds a person.
	AddPerson(ctx context.Context, identityName, repository string, roles ...string) error
	// SetPersonRoles replaces the roles a person of a repository holds, and
	// refuses to leave the repository without an admin (people.go). Nothing
	// on the network changes a role.
	SetPersonRoles(ctx context.Context, identityName, repository string, roles ...string) error
	// RebuildRepository replays a repository's changelog into its fold
	// (rebuild.go).
	RebuildRepository(ctx context.Context, repository string) (RebuildReport, error)
	// VerifyRepository checks a repository's files against its fold
	// (verify.go).
	VerifyRepository(ctx context.Context, repository string) (VerifyReport, error)
	// VerifyRepositoryWith is VerifyRepository with its options
	// (verify.go).
	VerifyRepositoryWith(ctx context.Context, repository string, opts VerifyOptions) (VerifyReport, error)
	// RotateHistoryGeneration mints a new history generation
	// (historygeneration.go).
	RotateHistoryGeneration(ctx context.Context, repository string) (RotateReport, error)
	// ReindexObserved queues the background re-derivation of a repository's
	// observed index (observedreindex.go).
	ReindexObserved(ctx context.Context, repository string) (ReindexReport, error)
	// ArchiveReport, ResolveArchive, RemoveArchive and ImportArchive are the
	// operator's hand in the archive (archiveops.go).
	ArchiveReport(ctx context.Context, repository string) (ArchiveReport, error)
	ResolveArchive(ctx context.Context, repository string, keepDatabase bool) (ArchiveResolution, error)
	RemoveArchive(ctx context.Context, repository, archiveID string) (int, error)
	ImportArchive(ctx context.Context, repository, archiveID, name string) (Repository, error)
}

var _ Operator = (*service)(nil)
