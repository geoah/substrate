package substrate

import "context"

// Service is the process-wide handle: schema files loaded, database
// connected, the shared schema migrated. One per process.
type Service interface {
	// Repositories lists every repository the substrate holds. It is the
	// control-plane read the background loops enumerate through; there is no
	// control-plane repository and no operator dataset.
	Repositories(ctx context.Context) ([]RepositoryInfo, error)

	// Dataset opens a repository's dataset by its user's username.
	Dataset(ctx context.Context, username string) (Dataset, error)

	// CreateRepository creates a repository and its control-plane row for a
	// username, seeded and open. Registration goes through it; nothing else
	// should, since a repository with no credential has no way in.
	CreateRepository(ctx context.Context, name string) (RepositoryInfo, error)

	// --- the door ---
	//
	// Registration is two calls and one write. BeginRegistration issues a
	// TOTP enrollment for a username and creates NOTHING; Register takes it
	// back with one code and a password, and only then does anything durable
	// exist. An abandoned registration leaves no row, no seed and no record.
	// The invite code gating both is the HTTP layer's (it holds the config).
	BeginRegistration(ctx context.Context, username string) (TOTPEnrollment, error)
	Register(ctx context.Context, in RegisterInput) (TokenInfo, string, error)

	// Login verifies username + password + TOTP and MINTS A TOKEN RECORD,
	// returning its secret exactly once (ruling RB-5: sessions ARE token
	// records; there is no session concept beside them).
	Login(ctx context.Context, in LoginInput) (TokenInfo, string, error)

	// ChangePassword and the TOTP re-enrollment pair carry the
	// password-factor rule: each takes BOTH current factors in
	// its input, so a leaked bearer token can never rotate the account it
	// stole. BeginTOTPReenrollment verifies the current factors and issues a
	// candidate seed without writing; ReenrollTOTP verifies the current
	// factors AND one code from the candidate before the swap.
	ChangePassword(ctx context.Context, in LoginInput, newPassword string) error
	BeginTOTPReenrollment(ctx context.Context, in LoginInput) (TOTPEnrollment, error)
	ReenrollTOTP(ctx context.Context, in LoginInput, newSecret, newCode string) error

	// Authenticate resolves a bearer secret to its dataset and token: one
	// SHA-256 lookup ACROSS repositories, which is what scopes the request to
	// the repository holding the matching token record.
	Authenticate(ctx context.Context, tokenSecret string) (Dataset, TokenInfo, error)

	Close() error
}
