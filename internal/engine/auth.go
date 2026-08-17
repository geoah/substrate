package engine

// THE DOOR.
//
// A user is a `repositories` row, and everything else about them is a RECORD
// in the repository they own:
//
//   - `core.substrate.reamde.dev/credential`, singleton id `self` — the username plus
//     two refs into the sealed store. The material itself (an argon2id
//     password hash, a TOTP seed) is NEVER in the changelog and never in a record's
//     data, so the changelog carries an audit trail — "the credential changed at T"
//     — and nothing crackable.
//   - `core.substrate.reamde.dev/token`, one per token — a label, the SHA-256 of the
//     secret and an optional expiry. Nothing records use, so authenticating
//     writes nothing. Sessions ARE these records; login mints one and hands
//     back its secret once.
//
// THE MAINT-POOL PATHS ARE ENUMERATED HERE AND NOWHERE ELSE. Authentication
// cannot start from a repository scope — a login knows a username and a
// bearer knows a hash — so exactly four reads run on the BYPASSRLS pool with
// an explicit `repository` predicate: `repositoryByUsername` (login),
// `credentialOf` + `authMaterial` (the factors), and `tokenByHash` (every
// authenticated request). Everything they lead to runs scoped.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	// kindCredential is the singleton auth record and credentialID its only
	// id: one repository, one user, one credential.
	kindCredential = "core.substrate.reamde.dev/credential"
	credentialID   = "self"

	// kindRecoveryKey is the singleton recovery record: the age recipient the
	// user enrolled and the repository's DEK wrapped to it.
	kindRecoveryKey = "core.substrate.reamde.dev/recoverykey"
	recoveryKeyID   = "self"

	// The sealed-store refs the credential record points at are namespaced so
	// a connector credential and a password hash can never collide on one.
	sealedAuthPrefix = "auth:"

	// minPasswordLength is the only password policy v1 has. Length is the
	// property that matters and the one a user can act on; a composition rule
	// would be theater in front of an argon2id hash.
	minPasswordLength = 12
	maxPasswordLength = 1024
)

// --- password hashing (argon2id) -------------------------------------------

// The argon2id parameters, stored WITH each hash so raising them later
// verifies old hashes unchanged. RFC 9106's second recommended profile:
// 64 MiB, three passes, four lanes.
const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// hashPassword renders a password as the PHC-style string the sealed row
// holds: the parameters, the salt and the digest, all in one line.
func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum)), nil
}

// verifyPassword checks a password against a stored hash in constant time. A
// malformed or unreadable stored hash verifies FALSE — it never errors into a
// path that could be read as success.
func verifyPassword(stored, password string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory, times uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &times, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, times, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyPasswordHash keeps a login for an unknown user as expensive as one for
// a known user: the same argon2id work against a hash no caller can produce,
// so the response time is not an existence oracle.
var dummyPasswordHash = func() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic("substrate/engine: no entropy for the password timing equalizer: " + err.Error())
	}
	h, err := hashPassword(hex.EncodeToString(raw))
	if err != nil {
		panic("substrate/engine: no argon2id for the password timing equalizer: " + err.Error())
	}
	return h
}()

// --- the sealed auth material ----------------------------------------------

// totpMaterial is the sealed TOTP payload: the seed and the last consumed
// step. They live together because they are one fact — a code is one-time —
// and because keeping the replay counter out of the changelog is what stops a login
// from writing an entry.
type totpMaterial struct {
	Secret string `json:"secret"`
	Step   int64  `json:"step"`
}

// authMaterial is a user's factors as the login path needs them: the stored
// password hash, the TOTP seed and step, and the refs they came from.
type authMaterial struct {
	passwordRef  string
	passwordHash string
	totpRef      string
	totp         totpMaterial
}

// newAuthRef mints an unguessable sealed-store ref. Rotation writes a NEW ref
// rather than overwriting one, so the credential record's own data moves and
// the changelog records a real change — the audit trail RB-5 asks for.
func newAuthRef(kind string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return sealedAuthPrefix + kind + ":" + hex.EncodeToString(raw), nil
}

// credentialOf reads a repository's credential record on the MAINTENANCE pool
// — one of the four enumerated cross-scope reads, because a login has no
// repository scope yet. It returns the two sealed refs.
func (s *service) credentialOf(ctx context.Context, repoID string) (username, passwordRef, totpRef string, err error) {
	var props []byte
	err = s.maint.QueryRowContext(ctx, `
		SELECT props FROM records
		WHERE repository = $1 AND kind = $2 AND id = $3 AND deleted_at IS NULL`,
		repoID, kindCredential, credentialID).Scan(&props)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", fmt.Errorf("%w: no credential", substrate.ErrAuth)
	}
	if err != nil {
		return "", "", "", err
	}
	var m map[string]any
	if err := json.Unmarshal(props, &m); err != nil {
		return "", "", "", err
	}
	str := func(k string) string { v, _ := m[k].(string); return v }
	return str("username"), str("passwordRef"), str("totpRef"), nil
}

// authMaterialOf opens both sealed rows for a repository, on the maintenance
// pool for the same reason credentialOf is there.
func (s *service) authMaterialOf(ctx context.Context, repoID string) (authMaterial, error) {
	var m authMaterial
	_, passwordRef, totpRef, err := s.credentialOf(ctx, repoID)
	if err != nil {
		return m, err
	}
	m.passwordRef, m.totpRef = passwordRef, totpRef
	hash, err := s.openSealed(ctx, repoID, passwordRef)
	if err != nil {
		return m, err
	}
	m.passwordHash = string(hash)
	raw, err := s.openSealed(ctx, repoID, totpRef)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(raw, &m.totp); err != nil {
		return m, fmt.Errorf("substrate/engine: unreadable totp material: %w", err)
	}
	return m, nil
}

// openSealed reads and unseals one row by ref, on the maintenance pool. The
// payload opens under the repository's DEK, with the host-key fallback for
// material sealed before DEKs existed.
func (s *service) openSealed(ctx context.Context, repoID, ref string) ([]byte, error) {
	if ref == "" {
		return nil, fmt.Errorf("%w: credential incomplete", substrate.ErrAuth)
	}
	var payload []byte
	err := s.maint.QueryRowContext(ctx,
		`SELECT payload FROM sealed WHERE repository = $1 AND ref = $2`, repoID, ref).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: credential material missing", substrate.ErrAuth)
	}
	if err != nil {
		return nil, err
	}
	dek, err := s.repoDEK(ctx, repoID)
	if err != nil {
		return nil, err
	}
	return openWithFallback(payload, dek, s.credKey)
}

// consumeTOTPStep spends a code by recording its step on the sealed TOTP row.
// The row is taken FOR UPDATE and the step re-read under that lock, so two
// logins racing on one code cannot both win it — a code is one-time, which is
// the whole reason the step lives beside the seed. It writes no changelog entry: a
// replay counter is not a change to the credential.
func (s *service) consumeTOTPStep(ctx context.Context, repoID, ref string, to int64) (bool, error) {
	tx, err := s.maint.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var payload []byte
	err = tx.QueryRowContext(ctx,
		`SELECT payload FROM sealed WHERE repository = $1 AND ref = $2 FOR UPDATE`,
		repoID, ref).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	dek, err := s.repoDEK(ctx, repoID)
	if err != nil {
		return false, err
	}
	raw, err := openWithFallback(payload, dek, s.credKey)
	if err != nil {
		return false, err
	}
	var m totpMaterial
	if err := json.Unmarshal(raw, &m); err != nil {
		return false, err
	}
	if to <= m.Step {
		return false, nil
	}
	m.Step = to
	sealed, err := s.sealRepoPayload(dek, mustJSON(m))
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sealed SET payload = $3, updated_at = now() WHERE repository = $1 AND ref = $2`,
		repoID, ref, sealed); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic("substrate/engine: marshaling sealed material: " + err.Error())
	}
	return raw
}

// --- registration ----------------------------------------------------------

// BeginRegistration issues a TOTP enrollment for a username and writes
// NOTHING: the caller holds the seed and proves possession by returning it
// with one code (Register). An abandoned registration therefore leaves no
// durable trace at all — no row to expire, no pending state to sweep.
func (s *service) BeginRegistration(ctx context.Context, username string) (substrate.TOTPEnrollment, error) {
	if !vocabulary.ValidRepositoryName(username) {
		return substrate.TOTPEnrollment{},
			fmt.Errorf("%w: username %q must match [a-z][a-z0-9]{1,29}", substrate.ErrValidation, username)
	}
	return newEnrollment(username)
}

func newEnrollment(username string) (substrate.TOTPEnrollment, error) {
	seed, err := NewTOTPSecret()
	if err != nil {
		return substrate.TOTPEnrollment{}, err
	}
	return substrate.TOTPEnrollment{Secret: seed, URI: TOTPEnrollmentURI(username, seed)}, nil
}

// Register creates the user: ONE creation act
// writes the seeded repository, the sealed material, the credential record and
// the first token — so a registration ends logged in — and the control-plane
// row that makes the user exist is the last thing written
// (createSeededRepository holds the atomicity story). A failed registration
// creates nothing.
func (s *service) Register(ctx context.Context, in substrate.RegisterInput) (substrate.RegisterResult, error) {
	var zero substrate.RegisterResult
	if err := validPassword(in.Password); err != nil {
		return zero, err
	}
	seed, step, err := s.registrationSeed(in)
	if err != nil {
		return zero, err
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		return zero, err
	}
	// The recovery pair: a client that generated its own identity sends only
	// the recipient and the identity never touches the server; a bare
	// registration asks the server to mint the pair, and the identity is
	// returned once below, never stored. Parsed BEFORE anything durable
	// exists, so a bad recipient refuses cleanly.
	out := substrate.RegisterResult{RecoveryPublicKey: in.RecoveryPublicKey}
	if out.RecoveryPublicKey == "" {
		identity, publicKey, err := generateRecoveryIdentity()
		if err != nil {
			return zero, err
		}
		out.RecoveryKey, out.RecoveryPublicKey = identity, publicKey
	}
	if _, err := wrapDEKToRecipient(make([]byte, 32), out.RecoveryPublicKey); err != nil {
		return zero, fmt.Errorf("%w: %w", substrate.ErrValidation, err)
	}
	label := in.Label
	if label == "" {
		label = "login"
	}
	_, signKey, err := s.createSeededRepository(ctx, in.Username, func(t *txn) error {
		// Fresh account: no prior credential to compare against, so no CAS.
		if err := t.writeCredential(credentialWrite{
			username: in.Username, passwordHash: hash,
			totp: totpMaterial{Secret: seed, Step: step},
		}); err != nil {
			return err
		}
		if err := t.writeRecoveryKey(out.RecoveryPublicKey); err != nil {
			return err
		}
		var terr error
		out.Token, out.Secret, terr = t.mintToken(label, nil)
		return terr
	})
	if err != nil {
		return zero, err
	}
	// The PUBLIC key, and only ever that: it is what verifies a signature, so
	// it is the whole of what a user needs, and it is worth handing over
	// because a pin read back out of the same database an attacker rewrote
	// proves nothing. The seed stays sealed under the credential key, where
	// the only signer keeps it.
	if signKey != nil {
		out.SigningPublicKey = hex.EncodeToString(signKey.Public().(ed25519.PublicKey))
	}
	return out, nil
}

// registrationSeed resolves the second factor a registration commits with: the
// caller's own enrollment, proved by one of its codes. The code proves the
// enrollment landed in an authenticator before the account exists — a user
// cannot register themselves out of their own account — and the step it matched
// is stored as consumed, so the code that registered cannot also log in.
//
// With the factor disabled (WithInsecureDisableTOTP) no code is asked for, and
// a registration that sends no seed at all gets a freshly minted one: the
// credential still HAS a second factor, sealed and unused, for the day the flag
// comes off.
func (s *service) registrationSeed(in substrate.RegisterInput) (string, int64, error) {
	if s.totpDisabled && in.TOTPSecret == "" {
		seed, err := NewTOTPSecret()
		return seed, 0, err
	}
	seed, err := normalizeTOTPSecret(in.TOTPSecret)
	if err != nil {
		return "", 0, fmt.Errorf("%w: the totp secret is not base32: %w", substrate.ErrValidation, err)
	}
	key, err := decodeTOTPSecret(seed)
	if err != nil || len(key) < totpMinSeedBytes {
		return "", 0, fmt.Errorf("%w: the totp secret must decode to at least %d bytes",
			substrate.ErrValidation, totpMinSeedBytes)
	}
	if s.totpDisabled {
		return seed, 0, nil
	}
	step, ok := totpVerify(key, in.TOTPCode, nowUTC(), 0)
	if !ok {
		return "", 0, fmt.Errorf("%w: that code does not match the enrollment", substrate.ErrAuth)
	}
	return seed, step, nil
}

// EnrollRecoveryKey wraps the repository's DEK to an age recipient and
// writes the recoverykey singleton, CREATE-ONLY: a repository from before
// recovery keys enrolls one here, and rotation is deliberately not v1. An
// empty publicKey asks the server to mint the pair; the identity returns
// once and is never stored.
//
// It carries the PASSWORD-FACTOR RULE, both current factors in the input: a
// bearer token is not evidence here. Enrollment permanently claims the
// repository's only recovery slot and hands out an offline decryption key,
// which is materially more than the repository API can ever do, so a stolen
// token must not be enough.
//
// The same transaction re-keys the whole sealed store under the DEK: a
// pre-DEK repository's payloads sat under the host key, and the recovery
// promise (a backup plus this key, no host involved) is only true once
// nothing in the store needs the host to open.
func (s *service) EnrollRecoveryKey(ctx context.Context, in substrate.LoginInput, publicKey string) (identity, recipient string, err error) {
	repo, _, err := s.verifyFactors(ctx, in)
	if err != nil {
		return "", "", err
	}
	if publicKey == "" {
		if identity, publicKey, err = generateRecoveryIdentity(); err != nil {
			return "", "", err
		}
	}
	if _, err := wrapDEKToRecipient(make([]byte, 32), publicKey); err != nil {
		return "", "", fmt.Errorf("%w: %w", substrate.ErrValidation, err)
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return "", "", err
	}
	ref := eref{Kind: kindRecoveryKey, ID: recoveryKeyID}
	err = ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		if err := t.lockRecord(ref); err != nil {
			return err
		}
		row, err := t.loadRow(ref, false)
		if err != nil {
			return err
		}
		if row != nil && row.DeletedAt == nil {
			return fmt.Errorf("%w: a recovery key is already enrolled; rotation is not yet supported", substrate.ErrConflict)
		}
		if err := t.writeRecoveryKey(publicKey); err != nil {
			return err
		}
		_, err = t.rekeySealedStore()
		return err
	})
	if err != nil {
		return "", "", err
	}
	return identity, publicKey, nil
}

// writeRecoveryKey wraps the repository's DEK to the enrolled age recipient
// and writes the recoverykey singleton: the changelog-borne half of the
// recovery story, ciphertext only the user's identity opens.
func (t *txn) writeRecoveryKey(publicKey string) error {
	wrapped, err := wrapDEKToRecipient(t.ds.dek, publicKey)
	if err != nil {
		return err
	}
	_, err = t.put(substrate.PutInput{
		Kind: kindRecoveryKey, ID: recoveryKeyID,
		Properties: map[string]any{
			"algorithm": recoveryAlgorithm,
			"publicKey": publicKey,
			"sealedKey": base64.StdEncoding.EncodeToString(wrapped),
		},
	})
	return err
}

func validPassword(p string) error {
	switch {
	case len(p) < minPasswordLength:
		return fmt.Errorf("%w: the password must be at least %d characters", substrate.ErrValidation, minPasswordLength)
	case len(p) > maxPasswordLength:
		return fmt.Errorf("%w: the password must be at most %d characters", substrate.ErrValidation, maxPasswordLength)
	}
	return nil
}

// --- login and the password-factor rule ------------------------------------

// verifyFactors is the ONE place both factors are checked, so /login, the
// password change and the TOTP re-enrollment cannot drift apart (ruling
// RB-6 needs them identical). It answers the SAME error for an unknown user,
// a wrong password and a wrong code, and does the same argon2id and HMAC work
// on every path so the timing says nothing either.
func (s *service) verifyFactors(ctx context.Context, in substrate.LoginInput) (Repository, authMaterial, error) {
	authErr := fmt.Errorf("%w: bad username, password or code", substrate.ErrAuth)
	repo, rerr := s.repositoryByUsername(ctx, in.Username)
	material := authMaterial{passwordHash: dummyPasswordHash, totp: totpMaterial{}}
	key := dummyTOTPKey
	if rerr == nil {
		if got, err := s.authMaterialOf(ctx, repo.ID); err == nil {
			material = got
			if k, err := decodeTOTPSecret(got.totp.Secret); err == nil && len(k) >= totpMinSeedBytes {
				key = k
			}
		}
	}
	passwordOK := verifyPassword(material.passwordHash, in.Password)
	step, codeOK := totpVerify(key, in.TOTPCode, nowUTC(), material.totp.Step)
	// The dev escape hatch (WithInsecureDisableTOTP): the code is still
	// evaluated above, so the work and the timing are unchanged, and only the
	// VERDICT is dropped. Nothing is consumed either — a step spent here would
	// invalidate codes the enrolled authenticator is still showing, which is
	// what would make turning the factor back on a lockout.
	if s.totpDisabled {
		if rerr != nil || !passwordOK {
			return Repository{}, authMaterial{}, authErr
		}
		return repo, material, nil
	}
	if rerr != nil || !passwordOK || !codeOK {
		return Repository{}, authMaterial{}, authErr
	}
	// The code is spent BEFORE anything is handed out, under the sealed row's
	// own lock, so two requests racing on one code cannot both win.
	won, err := s.consumeTOTPStep(ctx, repo.ID, material.totpRef, step)
	if err != nil {
		return Repository{}, authMaterial{}, err
	}
	if !won {
		return Repository{}, authMaterial{}, authErr
	}
	// The verified material is returned so a credential change writes from the
	// SAME read it authenticated against — the refs are the compare-and-swap
	// baseline, and the just-consumed step is what the seed carries forward.
	material.totp.Step = step
	return repo, material, nil
}

// Login verifies both factors and mints a token record: the session IS that
// record, so a logout is a delete and a revoke is the same
// write from somewhere else.
func (s *service) Login(ctx context.Context, in substrate.LoginInput) (substrate.TokenInfo, string, error) {
	var zero substrate.TokenInfo
	repo, _, err := s.verifyFactors(ctx, in)
	if err != nil {
		return zero, "", err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return zero, "", err
	}
	label := in.Label
	if label == "" {
		label = "login"
	}
	return ds.MintToken(ctx, label, nil)
}

// ChangePassword rewrites the password hash. It takes the CURRENT password
// and code in its input and nothing else: a bearer token is not evidence
// here, which is what keeps a leaked token's blast radius the data rather
// than the account.
func (s *service) ChangePassword(ctx context.Context, in substrate.LoginInput, newPassword string) error {
	if err := validPassword(newPassword); err != nil {
		return err
	}
	repo, material, err := s.verifyFactors(ctx, in)
	if err != nil {
		return err
	}
	// argon2 stays OUTSIDE the write's lock; the verified refs ride in as the
	// compare-and-swap baseline so a concurrent rotation is a conflict, not a
	// silent overwrite.
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return err
	}
	// The second factor is unchanged, so the seed carries over — with its
	// consumed step, or the code just spent could be replayed at the new
	// credential. The write reconciles that step against anything a concurrent
	// login advanced.
	return ds.rewriteCredential(ctx, credentialWrite{
		username: in.Username, passwordHash: hash, totp: material.totp,
		expectPasswordRef: material.passwordRef, expectTotpRef: material.totpRef, casEnabled: true,
	})
}

// BeginTOTPReenrollment verifies the current factors and issues a candidate
// seed, writing nothing: an abandoned re-enrollment cannot lock a user out of
// their own account.
func (s *service) BeginTOTPReenrollment(ctx context.Context, in substrate.LoginInput) (substrate.TOTPEnrollment, error) {
	if _, _, err := s.verifyFactors(ctx, in); err != nil {
		return substrate.TOTPEnrollment{}, err
	}
	return newEnrollment(in.Username)
}

// ReenrollTOTP swaps the second factor: the current factors AND one code from
// the candidate seed, so the swap cannot land on a seed nobody holds.
func (s *service) ReenrollTOTP(ctx context.Context, in substrate.LoginInput, newSecret, newCode string) error {
	seed, err := normalizeTOTPSecret(newSecret)
	if err != nil {
		return fmt.Errorf("%w: the totp secret is not base32: %w", substrate.ErrValidation, err)
	}
	key, err := decodeTOTPSecret(seed)
	if err != nil || len(key) < totpMinSeedBytes {
		return fmt.Errorf("%w: the totp secret must decode to at least %d bytes",
			substrate.ErrValidation, totpMinSeedBytes)
	}
	step, ok := totpVerify(key, newCode, nowUTC(), 0)
	if !ok {
		return fmt.Errorf("%w: that code does not match the new enrollment", substrate.ErrAuth)
	}
	repo, material, err := s.verifyFactors(ctx, in)
	if err != nil {
		return err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return err
	}
	// The password is unchanged, so its hash carries over untouched — a
	// re-enrollment must not need the plaintext to keep it. The NEW seed starts
	// at the code just proven, and the verified refs are the CAS baseline so a
	// concurrent password change cannot be silently reverted by this write.
	return ds.rewriteCredential(ctx, credentialWrite{
		username: in.Username, passwordHash: material.passwordHash,
		totp:              totpMaterial{Secret: seed, Step: step},
		expectPasswordRef: material.passwordRef, expectTotpRef: material.totpRef, casEnabled: true,
	})
}

// Resetter is the operator hat's reset seam. It is off substrate.Service on
// purpose, so `substratectl user reset` asks for it by shape; naming it here
// and asserting it below means a changed signature fails the build instead of
// leaving the command to refuse at runtime, on the box, to a user who has
// already lost their authenticator.
type Resetter interface {
	ResetUser(ctx context.Context, username, newPassword string) (substrate.TOTPEnrollment, error)
}

var _ Resetter = (*service)(nil)

// ResetUser is the operator's door for a user who lost both factors: fresh
// sealed material and a new credential record, an ordinary changelog entry
// attributed to the substrate. It is off substrate.Service on purpose —
// `substratectl user reset` on the box is the only caller (B8), and nothing
// reachable from the network resets an account.
func (s *service) ResetUser(ctx context.Context, username, newPassword string) (substrate.TOTPEnrollment, error) {
	var zero substrate.TOTPEnrollment
	if err := validPassword(newPassword); err != nil {
		return zero, err
	}
	repo, err := s.repositoryByUsername(ctx, username)
	if err != nil {
		return zero, err
	}
	enrollment, err := newEnrollment(username)
	if err != nil {
		return zero, err
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return zero, err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return zero, err
	}
	// The operator's reset is authoritative and rare — no compare-and-swap: it
	// deliberately overwrites whatever is there for a user who lost both factors.
	if err := ds.rewriteCredential(ctx, credentialWrite{
		username: username, passwordHash: hash,
		totp: totpMaterial{Secret: enrollment.Secret},
	}); err != nil {
		return zero, err
	}
	return enrollment, nil
}

// credentialWrite is a pending credential rewrite in its UNSEALED form. The
// password hash is precomputed (argon2id is expensive and must not run under
// the credential lock); the TOTP is sealed INSIDE the write, after its step is
// reconciled with what is stored. When casEnabled, the write asserts the
// credential still points at the refs the caller verified against — a
// concurrent rotation is a CONFLICT, not a silent overwrite that undoes it.
type credentialWrite struct {
	username     string
	passwordHash string
	totp         totpMaterial
	// expectPasswordRef/expectTotpRef are the refs the verification read. When
	// casEnabled they are the compare-and-swap baseline; a mismatch means
	// another rotation landed first, so this one refuses.
	expectPasswordRef string
	expectTotpRef     string
	casEnabled        bool
}

// errCredentialConflict marks a credential rewrite that lost a race with a
// concurrent one: the refs it verified against are no longer current, so it
// REFUSES rather than overwriting the winner. The caller re-proves the factors
// and retries — the alternative is silently undoing someone else's rotation.
var errCredentialConflict = fmt.Errorf("%w: the credential changed concurrently — re-authenticate and retry", substrate.ErrConflict)

// writeCredential is the transaction half: under the per-repository credential
// lock, it compare-and-swaps on the current refs (when asked), reconciles the
// TOTP step so it can never regress, seals the fresh material and swaps the
// record's two refs — deleting the old sealed rows in the same transaction. It
// is used on its own (a rotation) and inside the creation transaction
// (registration), which is why it is a txn method and not a dataset one.
func (t *txn) writeCredential(cw credentialWrite) error {
	if err := t.lockKey("credential"); err != nil {
		return err
	}
	var curP, curT sql.NullString
	if err := t.row(`SELECT props->>'passwordRef', props->>'totpRef' FROM records
		WHERE kind = $1 AND id = $2`, kindCredential, credentialID).Scan(&curP, &curT); err != nil &&
		!errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if cw.casEnabled && (curP.String != cw.expectPasswordRef || curT.String != cw.expectTotpRef) {
		// A rotation landed between our verification and this write. Refuse
		// rather than overwrite it — the caller re-authenticates and retries.
		return errCredentialConflict
	}
	// Step monotonicity: a concurrent login may have consumed a LATER code on
	// the same seed after our read, advancing the stored step under its own row
	// lock. Never write a step below what is stored for the same secret, or a
	// spent code could be replayed at the new credential.
	step := cw.totp.Step
	if curT.String != "" {
		if raw, err := t.openSealedRef(curT.String); err == nil {
			var cur totpMaterial
			if json.Unmarshal(raw, &cur) == nil && cur.Secret == cw.totp.Secret && cur.Step > step {
				step = cur.Step
			}
		}
	}
	// Seal fresh material now, under the lock: AES-GCM is cheap; only argon2id
	// (already done by the caller) is kept outside. New refs on every write are
	// the audit trail RB-5 asks for.
	passwordRef, err := newAuthRef("password")
	if err != nil {
		return err
	}
	totpRef, err := newAuthRef("totp")
	if err != nil {
		return err
	}
	sealedPassword, err := t.ds.sealPayload([]byte(cw.passwordHash))
	if err != nil {
		return err
	}
	sealedTOTP, err := t.ds.sealPayload(mustJSON(totpMaterial{Secret: cw.totp.Secret, Step: step}))
	if err != nil {
		return err
	}
	for ref, payload := range map[string][]byte{passwordRef: sealedPassword, totpRef: sealedTOTP} {
		if _, err := t.exec(`
			INSERT INTO sealed (ref, record_kind, record_id, payload, updated_at)
			VALUES ($1, $2, $3, $4, now())`,
			ref, kindCredential, credentialID, payload); err != nil {
			return fmt.Errorf("substrate/engine: seal credential material: %w", err)
		}
	}
	if _, err := t.put(substrate.PutInput{
		Kind: kindCredential, ID: credentialID,
		Properties: map[string]any{
			"username": cw.username, "passwordRef": passwordRef, "totpRef": totpRef,
		},
	}); err != nil {
		return err
	}
	for _, ref := range []string{curP.String, curT.String} {
		if ref == "" {
			continue
		}
		if _, err := t.exec(`DELETE FROM sealed WHERE ref = $1`, ref); err != nil {
			return err
		}
	}
	return nil
}

// openSealedRef reads and unseals one sealed row by ref, INSIDE the
// transaction and FOR UPDATE, so the credential rewrite reconciles the TOTP
// step against the freshest stored value. The row lock is what serializes this
// read with a concurrent login's consumeTOTPStep (which also takes it FOR
// UPDATE): without it, a login could advance the step between this read and the
// swap's DELETE, and the new credential would regress to the stale step.
func (t *txn) openSealedRef(ref string) ([]byte, error) {
	var payload []byte
	if err := t.row(`SELECT payload FROM sealed WHERE ref = $1 FOR UPDATE`, ref).Scan(&payload); err != nil {
		return nil, err
	}
	return t.ds.openPayload(payload)
}

// rewriteCredential is writeCredential's outer half: one transaction on the
// repository's own pool, attributed to the substrate.
func (ds *dataset) rewriteCredential(ctx context.Context, cw credentialWrite) error {
	return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		return t.writeCredential(cw)
	})
}

// --- tokens ----------------------------------------------------------------

// tokenPrefix namespaces bearer secrets so leak scanners (gitleaks, GitHub
// secret scanning) match them with no false positives. The secret is
// `substrate_tok_<hex>` and NOTHING else: no username segment, because a token
// names its repository by being FOUND in it (ruling RB-2's one-repository
// model made the routing hint pointless, and a secret that carries a username
// leaks one).
const tokenPrefix = "substrate_tok_"

// MintToken writes one token record and returns its secret, shown exactly
// once. The record holds the SHA-256 and never the secret; the secret is 20
// random bytes, so no KDF is needed to make guessing hopeless.
func (ds *dataset) MintToken(ctx context.Context, label string, expiresAt *time.Time) (substrate.TokenInfo, string, error) {
	var info substrate.TokenInfo
	var secret string
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		var err error
		info, secret, err = t.mintToken(label, expiresAt)
		return err
	})
	if err != nil {
		return substrate.TokenInfo{}, "", err
	}
	return info, secret, nil
}

// mintToken is the transaction half, so a registration can mint its first
// token in the same commit that creates the repository.
func (t *txn) mintToken(label string, expiresAt *time.Time) (substrate.TokenInfo, string, error) {
	var zero substrate.TokenInfo
	if label == "" {
		return zero, "", fmt.Errorf("%w: a token needs a label", substrate.ErrValidation)
	}
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return zero, "", err
	}
	secret := tokenPrefix + hex.EncodeToString(raw)
	props := map[string]any{"label": label, "hash": hashToken(secret)}
	if expiresAt != nil {
		props["expiresAt"] = expiresAt.UTC().Format(time.RFC3339)
	}
	ent, err := t.put(substrate.PutInput{Kind: kindToken, Properties: props})
	if err != nil {
		return zero, "", err
	}
	return substrate.TokenInfo{
		ID: ent.ID, Label: label, Created: ent.CreatedAt, ExpiresAt: expiresAt,
	}, secret, nil
}

// Tokens lists the repository's live token records, newest first. Metadata
// only: the hash is a secret-typed property and never leaves the store.
func (ds *dataset) Tokens(ctx context.Context) ([]substrate.TokenInfo, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id, props->>'label', props->>'expiresAt', created_at
		FROM records
		WHERE kind = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC, id`, kindToken)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []substrate.TokenInfo
	for rows.Next() {
		var info substrate.TokenInfo
		var label, expires sql.NullString
		if err := rows.Scan(&info.ID, &label, &expires, &info.Created); err != nil {
			return nil, err
		}
		info.Label = label.String
		info.ExpiresAt = parseStamp(expires)
		out = append(out, info)
	}
	return out, rows.Err()
}

func parseStamp(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s.String)
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
}

func hashToken(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// Authenticate resolves a bearer secret to its dataset and token. THE HASH
// LOOKUP IS WHAT SCOPES THE REQUEST: one query across every
// repository on the maintenance pool finds the token RECORD, and the
// repository holding it is the repository the request runs in. There is no
// repository in the URL and none in the secret; a deleted record or an
// expired stamp is simply no match, which is why revoking is a delete.
func (s *service) Authenticate(ctx context.Context, tokenSecret string) (substrate.Dataset, substrate.TokenInfo, error) {
	var zero substrate.TokenInfo
	rest, ok := strings.CutPrefix(tokenSecret, tokenPrefix)
	if !ok || rest == "" {
		return nil, zero, fmt.Errorf("%w: malformed token", substrate.ErrAuth)
	}
	if _, err := hex.DecodeString(rest); err != nil {
		return nil, zero, fmt.Errorf("%w: malformed token", substrate.ErrAuth)
	}
	repoID, info, err := s.tokenByHash(ctx, hashToken(tokenSecret))
	if err != nil {
		return nil, zero, err
	}
	repo, err := s.repositoryByID(ctx, repoID)
	if err != nil {
		if errors.Is(err, substrate.ErrNotFound) {
			return nil, zero, fmt.Errorf("%w: unknown token", substrate.ErrAuth)
		}
		return nil, zero, err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		// A well-formed token whose real repository could NOT be opened is a
		// service condition, not a bad credential: surface it so
		// the API maps it to a 5xx instead of a misleading "invalid token".
		return nil, zero, fmt.Errorf("open repository %s: %w", repo.Username, err)
	}
	return ds, info, nil
}

// tokenByHash is the cross-repository lookup — the fourth and last
// maint-pool path. It matches on the jsonb containment operator so the
// records props index serves it rather than a scan of every repository.
func (s *service) tokenByHash(ctx context.Context, hash string) (string, substrate.TokenInfo, error) {
	var zero substrate.TokenInfo
	var repoID string
	var info substrate.TokenInfo
	var label, got, expires sql.NullString
	err := s.maint.QueryRowContext(ctx, `
		SELECT repository, id, props->>'label', props->>'hash', props->>'expiresAt', created_at
		FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props @> jsonb_build_object('hash', $2::text)`,
		kindToken, hash).
		Scan(&repoID, &info.ID, &label, &got, &expires, &info.Created)
	if errors.Is(err, sql.ErrNoRows) {
		return "", zero, fmt.Errorf("%w: unknown token", substrate.ErrAuth)
	}
	if err != nil {
		return "", zero, err
	}
	if subtle.ConstantTimeCompare([]byte(got.String), []byte(hash)) != 1 {
		return "", zero, fmt.Errorf("%w: unknown token", substrate.ErrAuth)
	}
	info.Label = label.String
	// Expiry is SERVER-ENFORCED here: a token past its stamp authenticates as
	// a spent credential, so no dataset is ever handed out for it. An
	// unreadable stamp fails closed for the same reason.
	if expires.Valid && expires.String != "" {
		exp, perr := time.Parse(time.RFC3339, expires.String)
		if perr != nil {
			return "", zero, fmt.Errorf("%w: token expiry unreadable", substrate.ErrAuth)
		}
		if nowUTC().After(exp) {
			return "", zero, fmt.Errorf("%w: token expired", substrate.ErrAuth)
		}
		info.ExpiresAt = &exp
	}
	return repoID, info, nil
}
