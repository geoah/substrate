package engine

// THE DOOR.
//
// A person is an IDENTITY, a control-plane row beside the repositories
// (identities.go, decision 0274): the name they sign in with, the argon2id
// password hash and the second factor's seed, sealed under the identity's own
// key and never in any repository's history, and the rows that say which
// repositories the identity opens. A sign-in names the person, who picks one
// of those repositories. What the person does there is records of the
// repository:
//
//   - `substrate.reamde.dev/core/token`, one per token — a label, the SHA-256
//     of the secret, an optional expiry and the identity it acts for.
//     Nothing records use, so authenticating writes nothing. Sessions ARE
//     these records; login mints one and hands back its secret once.
//
// THE MAINT-POOL PATHS ARE ENUMERATED HERE AND NOWHERE ELSE. Authentication
// cannot start from a repository scope — a login knows a person's name and a
// bearer knows a hash — so these reads run on the BYPASSRLS pool:
// `identityByName` and `identityRepositories` (login), `identityMaterial`
// (the factors), and `tokenByHash` (every authenticated request, joined to
// its repositories row so one round trip answers both, with an explicit
// `repository` predicate). Everything they lead to runs scoped.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	// The sealed-store refs a credential record pointed at, namespaced so a
	// connector credential and a password hash could never collide on one. A
	// repository not opened since decision 0274 still holds them, and verify
	// reads them as the secret refs they are.
	sealedAuthPrefix = "auth:"

	// minPasswordLength is the only password policy v1 has. Length is the
	// property that matters and the one a user can act on; a composition rule
	// would be theater in front of an argon2id hash.
	minPasswordLength = 8
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

// argonSlots bounds the argon2id derivations running at once, process-wide.
// Each holds argonMemory (64 MiB) for about 300 ms, and a login runs one
// before it knows whether the repository exists, so the 32 attempts the
// global auth bucket admits at once would hold 2 GiB without it. Every
// derivation waits on the same slots, the unknown repository's included, so
// the queue says nothing about which repositories exist.
var argonSlots = make(chan struct{}, max(1, min(4, runtime.GOMAXPROCS(0)/2)))

// argonIDKey is the derivation argonKey runs, a variable so a test can count
// how many run at once.
var argonIDKey = argon2.IDKey

// argonKey is argon2.IDKey under one of argonSlots. A caller that leaves
// while it waits for a slot gets its context's error and derives nothing.
func argonKey(ctx context.Context, password, salt []byte, times, memory uint32, threads uint8, keyLen uint32) ([]byte, error) {
	select {
	case argonSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-argonSlots }()
	return argonIDKey(password, salt, times, memory, threads, keyLen), nil
}

// hashPassword renders a password as the PHC-style string the sealed row
// holds: the parameters, the salt and the digest, all in one line.
func hashPassword(ctx context.Context, password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum, err := argonKey(ctx, []byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum)), nil
}

// verifyPassword checks a password against a stored hash in constant time. A
// malformed or unreadable stored hash, or a caller that left before an argon2id
// slot freed, verifies FALSE: it never errors into a path that could be read
// as success.
func verifyPassword(ctx context.Context, stored, password string) bool {
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
	got, err := argonKey(ctx, []byte(password), salt, times, memory, threads, uint32(len(want)))
	if err != nil {
		return false
	}
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
	h, err := hashPassword(context.Background(), hex.EncodeToString(raw))
	if err != nil {
		panic("substrate/engine: no argon2id for the password timing equalizer: " + err.Error())
	}
	return h
}()

// --- the factors -------------------------------------------------------------

// totpMaterial is the sealed TOTP payload: the seed and the last consumed
// step. They live together because they are one fact — a code is one-time —
// and because keeping the replay counter beside the seed is what lets a
// sign-in spend a code without writing anything else.
type totpMaterial struct {
	Secret string `json:"secret"`
	Step   int64  `json:"step"`
}

// authMaterial is a person's factors as the factor check reads them off their
// identity (identities.go): the password hash and the second factor, and the
// `updated_at` they were read at, which is the compare-and-swap baseline of a
// credential change. An empty seed is an identity without a second factor.
type authMaterial struct {
	identity     string
	name         string
	passwordHash string
	totp         totpMaterial
	version      time.Time
}

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic("substrate/engine: marshaling sealed material: " + err.Error())
	}
	return raw
}

// --- registration ----------------------------------------------------------

// BeginRegistration issues a TOTP enrollment for an identity and writes
// NOTHING: the caller holds the seed and proves possession by returning it
// with one code (Register). An abandoned registration therefore leaves no
// durable trace at all — no row to expire, no pending state to sweep.
func (s *service) BeginRegistration(ctx context.Context, identity string) (substrate.TOTPEnrollment, error) {
	if err := validIdentityName(identity); err != nil {
		return substrate.TOTPEnrollment{}, err
	}
	return newEnrollment(identity)
}

// newEnrollment issues a seed whose authenticator entry is labeled with the
// name the person signs in with.
func newEnrollment(identity string) (substrate.TOTPEnrollment, error) {
	seed, err := NewTOTPSecret()
	if err != nil {
		return substrate.TOTPEnrollment{}, err
	}
	return substrate.TOTPEnrollment{Secret: seed, URI: TOTPEnrollmentURI(identity, seed)}, nil
}

// Register creates the person and their repository in ONE creation act: the
// seeded repository, its first token, and, with the control-plane row that
// makes the repository exist, the identity and the row that lets it open the
// repository (createSeededRepositoryFor holds the atomicity story). So a
// registration ends signed in, and a failed one creates nothing.
func (s *service) Register(ctx context.Context, in substrate.RegisterInput) (substrate.RegisterResult, error) {
	var zero substrate.RegisterResult
	if in.Repository == "" {
		in.Repository = in.Identity
	}
	if err := validIdentityName(in.Identity); err != nil {
		return zero, err
	}
	if err := validPassword(in.Password); err != nil {
		return zero, err
	}
	seed, step, err := s.registrationSeed(in)
	if err != nil {
		return zero, err
	}
	// A cheap early no before the argon2id work and the creation: the unique
	// index is the truth, and the insert beside the repository's row is
	// what a race loses on.
	if _, err := s.identityByName(ctx, s.maint, in.Identity); err == nil {
		return zero, errIdentityTaken(in.Identity)
	} else if !errors.Is(err, substrate.ErrNotFound) {
		return zero, err
	}
	hash, err := hashPassword(ctx, in.Password)
	if err != nil {
		return zero, err
	}
	s.afterProof("register")
	var out substrate.RegisterResult
	person, err := s.newIdentityRow(in.Identity, hash, totpMaterial{Secret: seed, Step: step})
	if err != nil {
		return zero, err
	}
	label := in.Label
	if label == "" {
		label = "login"
	}
	out.Repository, out.Identity = in.Repository, in.Identity
	repo, err := s.createSeededRepositoryFor(ctx, in.Repository, &person, func(t *txn) error {
		var terr error
		out.Token, out.Secret, terr = t.mintToken(substrate.MintTokenInput{Label: label, Identity: person.ID})
		return terr
	})
	if err != nil {
		return zero, err
	}
	// The operator's install list runs inside the registration, so a person
	// signs in to the providers the operator gives them already there.
	s.openForOperatorInstalls(ctx, repo)
	countAuth(repo.Authority, "registered")
	return out, nil
}

// registrationSeed resolves the second factor a registration commits with: the
// caller's own enrollment, proved by one of its codes. The code proves the
// enrollment landed in an authenticator before the account exists — a user
// cannot register themselves out of their own account — and the step it matched
// is stored as consumed, so the code that registered cannot also log in.
//
// A registration that skips the factor commits none, and an empty seed is what
// an identity reads as "no factor". The skip is explicit, so a client that
// lost its enrollment on the way is refused rather than registered without one.
// With the factor disabled (WithInsecureDisableTOTP) no code is asked for, and
// a registration that sends no seed has none either: a seed nobody holds would
// lock the account out the day the flag comes off.
func (s *service) registrationSeed(in substrate.RegisterInput) (string, int64, error) {
	if in.SkipTOTP {
		if in.TOTPSecret != "" || in.TOTPCode != "" {
			return "", 0, fmt.Errorf("%w: skipTotp registers without a second factor, so it takes no totpSecret or totpCode",
				substrate.ErrValidation)
		}
		return "", 0, nil
	}
	if in.TOTPSecret == "" {
		if s.totpDisabled {
			return "", 0, nil
		}
		return "", 0, fmt.Errorf("%w: send the enrollment's totpSecret and one totpCode from it, or skipTotp",
			substrate.ErrValidation)
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
	step, ok := totpVerify(key, in.TOTPCode, s.now(), 0)
	if !ok {
		return "", 0, fmt.Errorf("%w: that code does not match the enrollment", substrate.ErrAuth)
	}
	return seed, step, nil
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

// validIdentityName is the grammar a sign-in name is held to: a repository
// name's, so it keeps the DNS-style form another server could one day vouch
// for (decision 0274).
func validIdentityName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w: a sign-in needs a name: a hostname you control, such as ada.example.com", substrate.ErrValidation)
	case !vocabulary.ValidRepositoryAuthority(name):
		return fmt.Errorf("%w: sign-in name %q must be a lowercase DNS-style name with at least two labels (ada.example.com)", substrate.ErrValidation, name)
	case len(name) > vocabulary.MaxIDLen:
		return fmt.Errorf("%w: sign-in name %q is longer than %d bytes", substrate.ErrValidation, name, vocabulary.MaxIDLen)
	}
	return nil
}

// --- sign-in and the password-factor rule ----------------------------------

// afterProof runs the factor doors' test seam, once a request has proved
// the factors and before it acts.
func (s *service) afterProof(door string) {
	if s.testAfterProof != nil {
		s.testAfterProof(door)
	}
}

// errBadFactors is the one answer every refused factor gets: an unknown
// name, a wrong password and a wrong code are indistinguishable.
var errBadFactors = fmt.Errorf("%w: bad identity, password or code", substrate.ErrAuth)

// checkFactors is the ONE place both factors are checked, so the sign-in, the
// password change and the TOTP re-enrollment cannot drift apart (ruling RB-6
// needs them identical). It answers the SAME error for an unknown identity, a
// wrong password and a wrong code, and does the same argon2id and HMAC work on
// every path so the timing says nothing either.
//
// The one other answer is ErrTOTPRequired: the password is right, the person
// has a second factor and no code came. It is what lets a client ask for the
// password first and the code only of a person who has one, and it is given
// only to a caller who already proved the password.
//
// It spends nothing: the step the code matched is returned, and spendCode
// spends it once the caller is sure to go on, so a sign-in refused for want
// of a repository leaves the code good for the request sent again. -1 is
// nothing to spend.
func (s *service) checkFactors(ctx context.Context, in substrate.LoginInput) (authMaterial, int64, error) {
	row, rerr := s.identityByName(ctx, s.maint, in.Identity)
	material := authMaterial{passwordHash: dummyPasswordHash}
	key := dummyTOTPKey
	hasFactor := false
	if rerr == nil {
		got, err := s.identityMaterial(row)
		if err == nil {
			material = got
			hasFactor = got.totp.Secret != ""
			if k, err := decodeTOTPSecret(got.totp.Secret); err == nil && len(k) >= totpMinSeedBytes {
				key = k
			}
		} else {
			rerr = err
		}
	}
	passwordOK := verifyPassword(ctx, material.passwordHash, in.Password)
	step, codeOK := totpVerify(key, in.TOTPCode, s.now(), material.totp.Step)
	// The dev escape hatch (WithInsecureDisableTOTP), and a person registered
	// without a second factor: the code is still evaluated above, so the work
	// and the timing are unchanged, and only the VERDICT is dropped. Nothing
	// is spent either — a step spent here would invalidate codes the enrolled
	// authenticator is still showing, which is what would make turning the
	// factor back on a lockout.
	if s.totpDisabled || !hasFactor {
		if rerr != nil || !passwordOK {
			return authMaterial{}, -1, errBadFactors
		}
		return material, -1, nil
	}
	if rerr != nil || !passwordOK {
		return authMaterial{}, -1, errBadFactors
	}
	if normalizeTOTPCode(in.TOTPCode) == "" {
		return authMaterial{}, -1, substrate.ErrTOTPRequired
	}
	if !codeOK {
		return authMaterial{}, -1, errBadFactors
	}
	return material, step, nil
}

// spendCode spends the code checkFactors matched, under the identity row's
// lock, so two requests racing on one code cannot both win. The material
// then carries the step forward, which is what a credential change keeps.
func (s *service) spendCode(ctx context.Context, m *authMaterial, step int64) error {
	won, err := s.spendIdentityCode(ctx, m.identity, m.version, step)
	if err != nil {
		return err
	}
	if !won {
		return errBadFactors
	}
	if step >= 0 {
		m.totp.Step = step
	}
	return nil
}

// verifyFactors checks both factors and spends the code: the credential
// changes' evidence. The verified material is returned so a change writes
// from the SAME read it authenticated against.
func (s *service) verifyFactors(ctx context.Context, in substrate.LoginInput) (authMaterial, error) {
	m, step, err := s.checkFactors(ctx, in)
	if err != nil {
		return authMaterial{}, err
	}
	if err := s.spendCode(ctx, &m, step); err != nil {
		return authMaterial{}, err
	}
	return m, nil
}

// pickRepository is the repository a sign-in opens: the one named, which the
// person must be able to open, else the person's one repository. A person in
// several who names none is told which they may open, and one in none is
// refused; both are told only after the factors were proved.
func (s *service) pickRepository(ctx context.Context, identity, named string) (Repository, error) {
	repos, err := s.identityRepositories(ctx, s.maint, identity)
	if err != nil {
		return Repository{}, err
	}
	if named != "" {
		for _, repo := range repos {
			if repo.ID == named || repo.Authority == named {
				return repo, nil
			}
		}
		return Repository{}, fmt.Errorf("%w: this person may not open repository %s", substrate.ErrForbidden, named)
	}
	switch len(repos) {
	case 0:
		return Repository{}, fmt.Errorf("%w: this person is in no repository", substrate.ErrForbidden)
	case 1:
		return repos[0], nil
	}
	choices := make([]substrate.RepositoryChoice, len(repos))
	for i, repo := range repos {
		choices[i] = substrate.RepositoryChoice{ID: repo.ID, Name: repo.Authority}
	}
	return Repository{}, &substrate.RepositoryRequiredError{Repositories: choices}
}

// Login verifies a person's factors, picks the repository the sign-in opens
// and mints a token record there, naming the person: the session IS that
// record, so a sign-out is a delete and a revoke is the same write from
// somewhere else. The mint runs under the identity's row, held to the
// factors the request proved and to the person still opening the
// repository, so a sign-in proved before a password change and a sign out
// everywhere mints nothing after them.
func (s *service) Login(ctx context.Context, in substrate.LoginInput) (substrate.MintedToken, error) {
	var zero substrate.MintedToken
	m, step, err := s.checkFactors(ctx, in)
	if err != nil {
		if errors.Is(err, substrate.ErrAuth) {
			s.countFailedSignIn(ctx, in.Identity)
		}
		return zero, err
	}
	repo, err := s.pickRepository(ctx, m.identity, in.Repository)
	if err != nil {
		return zero, err
	}
	// Past the factors, and before the code is spent: a repository the start
	// set aside is refused by name.
	if err := s.refuseSetAside(repo.ID, repo.Authority); err != nil {
		return zero, err
	}
	if err := s.spendCode(ctx, &m, step); err != nil {
		return zero, err
	}
	s.afterProof("login")
	ds, err := s.open(ctx, repo)
	if err != nil {
		return zero, err
	}
	hold, err := s.holdFactors(ctx, m.identity, m.version)
	if err != nil {
		return zero, err
	}
	defer func() { _ = hold.Rollback() }()
	if ok, err := mayOpen(ctx, hold, m.identity, repo.ID); err != nil {
		return zero, err
	} else if !ok {
		return zero, fmt.Errorf("%w: this person may not open repository %s", substrate.ErrForbidden, repo.Authority)
	}
	label := in.Label
	if label == "" {
		label = "login"
	}
	info, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: label, Identity: m.identity})
	if err != nil {
		return zero, err
	}
	countAuth(repo.Authority, "login")
	return substrate.MintedToken{Token: info, Secret: secret, Repository: repo.Authority, Identity: m.name}, nil
}

// countFailedSignIn counts a refused sign-in against each repository the
// named identity may open, and only under a name that exists, so a guessed
// one adds no series. The lookup runs whether the name exists or not, so it
// adds the same round trip to both and the timing still tells nothing.
func (s *service) countFailedSignIn(ctx context.Context, name string) {
	rows, err := s.maint.QueryContext(ctx, `
		SELECT p.authority FROM identities i
		JOIN identity_repositories l ON l.identity = i.id
		JOIN repositories p ON p.id = l.repository_id
		WHERE i.name = $1`, name)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var authority string
		if rows.Scan(&authority) == nil {
			countAuth(authority, "login_failed")
		}
	}
	// A count lost to a failed read is a count, not a refusal.
	_ = rows.Err()
}

// ChangePassword rewrites the password hash. It takes the CURRENT password
// and code in its input and nothing else: a bearer token is not evidence
// here, which is what keeps a leaked token's blast radius the data rather
// than the account. It writes the identity's row and nothing else.
func (s *service) ChangePassword(ctx context.Context, in substrate.LoginInput, newPassword string) error {
	if err := validPassword(newPassword); err != nil {
		return err
	}
	m, err := s.verifyFactors(ctx, in)
	if err != nil {
		return err
	}
	s.afterProof("password")
	_, err = s.rewritePassword(ctx, m, newPassword)
	return err
}

// rewritePassword writes a new password hash for factors already verified.
// argon2id runs outside the row's lock, and the factors' `updated_at` rides
// in as the compare-and-swap baseline, so a concurrent change is a conflict,
// not a silent overwrite. The second factor carries over with the step just
// spent, or the code that proved this change could be replayed at the next.
// It answers the factors' new version.
func (s *service) rewritePassword(ctx context.Context, m authMaterial, newPassword string) (time.Time, error) {
	hash, err := hashPassword(ctx, newPassword)
	if err != nil {
		return time.Time{}, err
	}
	return s.rewriteIdentityFactors(ctx, factorWrite{
		identity: m.identity, passwordHash: hash, totp: m.totp, expect: m.version, cas: true,
	})
}

// DeleteRepository verifies a person's factors and erases the repository
// they name whole: its rows, its control-plane row and its directory, blobs
// and spool included. Nothing is kept, so the name is free for the next
// registration. The person's identity goes with its last repository; another
// person who could open it keeps theirs.
//
// It holds the authority's registration lock and its open slot across the
// erase, so no registration of the name and no open of the repository can
// start on rows that are being deleted. Requests already running on the
// dataset fail on its closed pool rather than write into a scope that is
// going away. The erase holds the identity's row first, to the factors this
// request proved, so a delete proved before they changed erases nothing.
func (s *service) DeleteRepository(ctx context.Context, in substrate.LoginInput) error {
	if s.readOnly {
		return ErrDirectoryReadOnly
	}
	if in.Repository == "" {
		return fmt.Errorf("%w: name the repository to delete in `repository`", substrate.ErrValidation)
	}
	m, err := s.verifyFactors(ctx, in)
	if err != nil {
		return err
	}
	s.afterProof("delete")
	repo, err := s.pickRepository(ctx, m.identity, in.Repository)
	if err != nil {
		return err
	}
	// Past the factors, and before anything behind this door writes: a
	// repository the start set aside is refused by name.
	if err := s.refuseSetAside(repo.ID, repo.Authority); err != nil {
		return err
	}
	if err := s.mayDeleteRepository(ctx, repo, m.identity); err != nil {
		return err
	}
	cp, unlock, err := s.lockRegistration(ctx, repo.Authority)
	if err != nil {
		return err
	}
	defer unlock()
	releaseOpen, err := s.claimErase(ctx, repo.ID)
	if err != nil {
		return err
	}
	defer releaseOpen()
	if err := s.eraseRepositoryOn(ctx, cp, repo.ID, eraseFor{person: m.identity, proved: &m.version}); err != nil {
		return err
	}
	s.lease.release(repo.ID)
	s.unitGraphs.forget(repo.ID)
	s.log.Info("substrate: repository deleted by a person who could open it", "repository", repo.ID)
	forgetUsage(repo.Authority)
	return nil
}

// mayDeleteRepository refuses the delete to a person who is not an admin of
// repo: it erases every person's records and sign-in material there. Their
// roles are read as their every request reads them.
func (s *service) mayDeleteRepository(ctx context.Context, repo Repository, identity string) error {
	ds, err := s.open(ctx, repo)
	if err != nil {
		return err
	}
	held, err := ds.PersonRoles(substrate.WithIdentity(ctx, identity))
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(held, func(r substrate.RoleRules) bool { return r.ID == scope.RoleAdmin }) {
		return fmt.Errorf("%w: %w: deleting a repository is an admin's: it erases every person's records", substrate.ErrForbidden, substrate.ErrNotPermitted)
	}
	return nil
}

// BeginTOTPReenrollment verifies the current factors and issues a candidate
// seed, writing nothing: an abandoned re-enrollment cannot lock a person out.
func (s *service) BeginTOTPReenrollment(ctx context.Context, in substrate.LoginInput) (substrate.TOTPEnrollment, error) {
	m, err := s.verifyFactors(ctx, in)
	if err != nil {
		return substrate.TOTPEnrollment{}, err
	}
	return newEnrollment(m.name)
}

// ReenrollTOTP swaps the second factor, or enrolls the first one of a person
// registered without: the current factors AND one code from the candidate
// seed, so the swap cannot land on a seed nobody holds.
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
	step, ok := totpVerify(key, newCode, s.now(), 0)
	if !ok {
		return fmt.Errorf("%w: that code does not match the new enrollment", substrate.ErrAuth)
	}
	m, err := s.verifyFactors(ctx, in)
	if err != nil {
		return err
	}
	s.afterProof("totp")
	// The password is unchanged, so its hash carries over untouched — a
	// re-enrollment must not need the plaintext to keep it. The NEW seed
	// starts at the code just proven, and the verified `updated_at` is the
	// baseline, so a concurrent password change is not silently reverted.
	_, err = s.rewriteIdentityFactors(ctx, factorWrite{
		identity: m.identity, passwordHash: m.passwordHash,
		totp:   totpMaterial{Secret: seed, Step: step},
		expect: m.version, cas: true,
	})
	return err
}

// ErrResetNeedsTheWriter is the reset's refusal beside a running server of a
// repository whose history names no person: giving it one writes a person's
// actor into the repository, which only its writer may.
var ErrResetNeedsTheWriter = fmt.Errorf("%w: the reset writes the repository a person's actor, which needs the server stopped", substrate.ErrConflict)

// ResetUser is the operator's door for a person who lost both factors: a new
// password and a new second factor's enrollment, written to the identity's
// row and nowhere else, so it runs beside a serving server. name is an
// identity's name, or a repository's name or id that no identity may open
// yet (one restored without its people), which gets an identity named
// `createAs`, or its own name when that is empty: the person its first
// person's actor names, under the same id, so the tokens the directory
// carried authenticate again; a repository whose actor names a person this
// server holds has its person, and the reset makes no other (restoredPerson).
// A repository whose history took its person out gets a new person with an
// actor of their own, which only the repository's writer writes: beside a
// running server the reset refuses with ErrResetNeedsTheWriter, and the
// operator's command opens as the writer, which the boot refuses while a
// server runs.
// It is off
// substrate.Service on purpose — `substratectl user reset` on the box is the
// only caller (B8), and nothing reachable from the network resets a sign-in.
func (s *service) ResetUser(ctx context.Context, name, newPassword, createAs string) (substrate.TOTPEnrollment, error) {
	var zero substrate.TOTPEnrollment
	if err := validPassword(newPassword); err != nil {
		return zero, err
	}
	hash, err := hashPassword(ctx, newPassword)
	if err != nil {
		return zero, err
	}
	row, err := s.identityByName(ctx, s.maint, name)
	switch {
	case err == nil:
		enrollment, err := newEnrollment(row.Name)
		if err != nil {
			return zero, err
		}
		// Authoritative and rare: no compare-and-swap, it deliberately
		// overwrites whatever is there for a person who lost both factors.
		_, err = s.rewriteIdentityFactors(ctx, factorWrite{
			identity: row.ID, passwordHash: hash, totp: totpMaterial{Secret: enrollment.Secret},
		})
		return enrollment, err
	case !errors.Is(err, substrate.ErrNotFound):
		return zero, err
	}
	repo, rerr := s.repositoryByKey(ctx, name)
	if rerr != nil {
		return zero, fmt.Errorf("%w: no identity or repository %s", substrate.ErrNotFound, name)
	}
	tx, err := s.maint.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer func() { _ = tx.Rollback() }()
	// One reset of a repository at a time: its row is locked, and who its
	// person is is read under that lock, so two resets naming two people
	// cannot both give it one.
	var locked string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM repositories WHERE id = $1 FOR UPDATE`, repo.ID).Scan(&locked); errors.Is(err, sql.ErrNoRows) {
		return zero, fmt.Errorf("%w: no repository %s", substrate.ErrNotFound, name)
	} else if err != nil {
		return zero, err
	}
	plan, err := s.restoredPerson(ctx, tx, repo, createAs)
	if err != nil {
		return zero, err
	}
	if !plan.missing {
		people, err := repositoryIdentities(ctx, tx, repo.ID)
		if err != nil {
			return zero, err
		}
		if len(people) > 0 {
			return zero, fmt.Errorf("%w: repository %s already has a sign-in: reset it by the name it signs in with", substrate.ErrConflict, repo.label())
		}
	}
	switch {
	case createAs != "":
	case plan.missing && validIdentityName(plan.name) == nil:
		createAs = plan.name
	default:
		createAs = repo.Authority
	}
	if err := validIdentityName(createAs); err != nil {
		return zero, err
	}
	enrollment, err := newEnrollment(createAs)
	if err != nil {
		return zero, err
	}
	if plan.writeActor && s.readOnly {
		return zero, fmt.Errorf("%w: repository %s names no person in its history", ErrResetNeedsTheWriter, repo.label())
	}
	s.afterProof("reset")
	person, err := s.newIdentityRowAs(plan.id, createAs, hash, totpMaterial{Secret: enrollment.Secret})
	if err != nil {
		return zero, err
	}
	if err := insertIdentity(ctx, tx, &person); err != nil {
		return zero, err
	}
	// The person's actor commits before the identity and its index row, so
	// the index is never ahead of it (people.go): the first person's, when it
	// is live and names nobody, else a new one of theirs, never the one the
	// history took out. A reset cut short between the two leaves an actor
	// naming an identity this server does not hold, which admits nobody, and
	// the same reset run again gives the repository that identity under the
	// same id (restoredPerson). The other order would leave the name held by
	// an identity no actor names, which a second run could not take.
	finish := func(err error) error { return err }
	if plan.writeActor {
		key := substrate.PersonActor(person.ID)
		if plan.firstUnnamed {
			key = substrate.ActorPerson
		}
		ds, err := s.open(ctx, repo)
		if err != nil {
			return zero, err
		}
		finish = func(err error) error {
			return fmt.Errorf("%w (the repository's history may now name its new person: run the reset again, and it finishes under the same identity)", err)
		}
		// The repository's person, whoever else it holds: its admin.
		if err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
			if err := t.namePersonActor(key, person.ID, person.Name); err != nil {
				return err
			}
			return t.givePersonRoles(key, scope.RoleAdmin)
		}); err != nil {
			if errors.Is(err, substrate.ErrWriteInDoubt) {
				return zero, finish(err)
			}
			return zero, err
		}
	}
	if err := linkIdentity(ctx, tx, person.ID, repo.ID); err != nil {
		return zero, finish(err)
	}
	if err := tx.Commit(); err != nil {
		return zero, finish(err)
	}
	return enrollment, nil
}

// resetPlan is what a reset of a repository that has no sign-in gives it:
// the identity's id ("" for a new one), and whether the reset writes the
// person's actor, because the repository's history names no person. missing
// says the identity is one a live actor names and this server does not
// hold, which the reset gives back whoever else signs in, under name, the
// name the actor carries.
type resetPlan struct {
	id                       string
	writeActor, firstUnnamed bool
	missing                  bool
	name                     string
}

// restoredPerson reads the actors' word on a repository's person, under the
// reset's lock. A live person actor naming an identity this server does not
// hold is a person a restore did not bring (an export carries only the
// person who took it, and a restore leaves out a person whose name another
// person here holds, or whose sign-in does not open): the reset gives that
// person back under that id, whoever else signs in, so their tokens
// authenticate again. With several such actors, as is the one whose name
// createAs is, and nothing else picks. With none, a live person actor
// naming an identity this server holds is the repository's person, whatever
// the index says: the reset is refused, naming them, and makes no other. A
// repository from before identities whose first person's actor names nobody
// yet gets a new identity, which its first open writes onto that actor. Any
// other repository names no person in its history, and the reset writes the
// person's actor itself.
func (s *service) restoredPerson(ctx context.Context, q dbx, repo Repository, createAs string) (resetPlan, error) {
	actors, err := personActorsOf(ctx, q, repo.ID)
	if err != nil {
		return resetPlan{}, err
	}
	named := make([]string, 0, len(actors))
	for _, a := range actors {
		if a.identity != "" {
			named = append(named, a.identity)
		}
	}
	held, err := heldIdentities(ctx, q, named)
	if err != nil {
		return resetPlan{}, err
	}
	isHeld := make(map[string]bool, len(held))
	for _, id := range held {
		isHeld[id] = true
	}
	var missing []personActor
	for _, a := range actors {
		if a.identity != "" && !isHeld[a.identity] && vocabulary.ValidIdentityID(a.identity) {
			missing = append(missing, a)
		}
	}
	if len(missing) > 0 {
		pick := slices.IndexFunc(missing, func(a personActor) bool { return a.name == createAs })
		if pick < 0 && len(missing) > 1 {
			names := make([]string, len(missing))
			for i, a := range missing {
				names[i] = a.name
			}
			return resetPlan{}, fmt.Errorf("%w: repository %s has %d people nobody here signs in as (%s): name one with --identity", substrate.ErrConflict, repo.label(), len(missing), strings.Join(names, ", "))
		}
		pick = max(pick, 0)
		return resetPlan{id: missing[pick].identity, missing: true, name: missing[pick].name}, nil
	}
	if len(held) > 0 {
		who := held[0]
		if row, err := s.identityByID(ctx, q, held[0]); err == nil {
			who = row.Name
		}
		return resetPlan{}, fmt.Errorf("%w: repository %s has its person, %s: reset them by the name they sign in with", substrate.ErrConflict, repo.label(), who)
	}
	first, err := firstPersonOf(ctx, q, repo.ID)
	if err != nil {
		return resetPlan{}, err
	}
	before, err := fromBeforeIdentities(ctx, q, repo.ID)
	if err != nil {
		return resetPlan{}, err
	}
	if before && (!first.exists || (first.live && first.identity == "")) {
		return resetPlan{}, nil
	}
	id, err := vocabulary.NewIdentityID()
	if err != nil {
		return resetPlan{}, err
	}
	return resetPlan{id: id, writeActor: true, firstUnnamed: first.live && first.identity == ""}, nil
}

// Identity is the person behind an identity id, as their own token reads it.
func (s *service) Identity(ctx context.Context, id string) (substrate.IdentityInfo, error) {
	row, err := s.identityByID(ctx, s.maint, id)
	if err != nil {
		return substrate.IdentityInfo{}, err
	}
	repos, err := s.identityRepositories(ctx, s.maint, id)
	if err != nil {
		return substrate.IdentityInfo{}, err
	}
	out := substrate.IdentityInfo{
		ID: row.ID, Name: row.Name, SecondFactor: len(row.TOTP) > 0,
		Repositories: make([]substrate.RepositoryChoice, len(repos)),
	}
	for i, repo := range repos {
		roles, err := s.rolesHeldIn(ctx, repo, id)
		if err != nil {
			return substrate.IdentityInfo{}, err
		}
		out.Repositories[i] = substrate.RepositoryChoice{ID: repo.ID, Name: repo.Authority, Roles: roles}
	}
	return out, nil
}

// rolesHeldIn are the record ids of the roles the identity's actor holds in
// repo, read as every request reads them. A repository that does not open
// says none, and is logged: the person's read of themselves does not fail
// for one repository set aside. A read of the roles that fails is an error:
// "none" would be an answer it did not read.
func (s *service) rolesHeldIn(ctx context.Context, repo Repository, identity string) ([]string, error) {
	ds, err := s.open(ctx, repo)
	if err != nil {
		s.log.Warn("substrate: the roles a person holds were not read: the repository did not open", "repository", logSafeID(repo.ID), "error", err)
		return nil, nil
	}
	actors, err := personActorsOf(ctx, ds.db, repo.ID)
	if err != nil {
		return nil, fmt.Errorf("read the people of repository %s: %w", repo.ID, err)
	}
	for _, a := range actors {
		if a.identity != identity {
			continue
		}
		roles, err := ds.rolesIn(ctx, ds.db, a.key)
		if err != nil {
			return nil, err
		}
		return roles.IDs(), nil
	}
	return nil, nil
}

// openSealedRef reads and unseals one sealed row by ref, INSIDE the
// transaction and FOR UPDATE, so a compare-and-swap of a sealed value
// (write.go) reads the freshest stored one and no concurrent writer moves it
// between this read and the swap.
func (t *txn) openSealedRef(ref string) ([]byte, error) {
	var payload []byte
	var owner eref
	if err := t.row(`SELECT payload, record_kind, record_id FROM sealed WHERE ref = $1 FOR UPDATE`, ref).
		Scan(&payload, &owner.Kind, &owner.ID); err != nil {
		return nil, err
	}
	return t.ds.openPayload(payload, sealedAAD(ref, owner.Kind, owner.ID))
}

// --- tokens ----------------------------------------------------------------

// tokenPrefix namespaces bearer secrets so leak scanners (gitleaks, GitHub
// secret scanning) match them with no false positives. The secret is
// `substrate_tok_<hex>` and NOTHING else: no repository segment, because a token
// names its repository by being FOUND in it (ruling RB-2's one-repository
// model made the routing hint pointless, and a secret that carries a name
// leaks one).
const tokenPrefix = substrate.TokenSecretPrefix

// MintToken writes one token record and returns its secret, shown exactly
// once. The record holds the SHA-256 and never the secret; the secret is 20
// random bytes, so no KDF is needed to make guessing hopeless.
func (ds *dataset) MintToken(ctx context.Context, in substrate.MintTokenInput) (substrate.TokenInfo, string, error) {
	var info substrate.TokenInfo
	var secret string
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		person, err := t.principalStands()
		if err != nil {
			return err
		}
		// A token minted on another's word acts for that token's person.
		if in.Identity == "" {
			in.Identity = person
		}
		info, secret, err = t.mintToken(in)
		return err
	})
	if err != nil {
		return substrate.TokenInfo{}, "", err
	}
	return info, secret, nil
}

// principalStands refuses a mint whose token no longer authenticates: the
// token that asked for it (`POST /tokens`) or approved it (an app's code
// exchange, which mints standing on the approving token) was revoked, its
// person signed out everywhere, its time ran out, or its person may no longer
// open the repository, after the request was authenticated. It is read
// inside the mint's transaction, which holds the changelog lock a sign-out
// holds, so no token is minted on one the sign-out ended (decision 0272). A
// mint with no principal stands on its factors (a login, held by
// holdFactors) or on nothing that can end (the seed). It answers the
// identity the standing token acts for, which the mint inherits.
func (t *txn) principalStands() (string, error) {
	if t.principal == "" {
		return "", nil
	}
	row, err := t.loadRow(eref{Kind: kindToken, ID: t.principal}, false)
	if err != nil {
		return "", err
	}
	if row == nil || row.DeletedAt != nil {
		return "", fmt.Errorf("%w: the token this request stands on was revoked, or its person signed out everywhere", substrate.ErrAuth)
	}
	// Expired is as gone, by the rule authentication reads it by
	// (resolveToken): a request authenticated just before the token's time
	// ran out does not mint after it.
	if stamp, _ := row.Props["expiresAt"].(string); stamp != "" {
		exp, err := time.Parse(time.RFC3339, stamp)
		if err != nil || nowUTC().After(exp) {
			return "", fmt.Errorf("%w: the token this request stands on expired", substrate.ErrAuth)
		}
	}
	person, _ := row.Props["identity"].(string)
	if person != "" {
		ok, err := mayOpen(t.ctx, t.ds.svc.maint, person, t.ds.info.ID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", errPersonMayNotOpen
		}
	}
	return person, nil
}

// errPersonMayNotOpen refuses a token whose person may no longer open its
// repository: one restored from a server whose people are not this one's, or
// one whose person was taken out of the repository.
var errPersonMayNotOpen = fmt.Errorf("%w: the person this token acts for may not open its repository", substrate.ErrAuth)

// mintToken is the transaction half, so a registration can mint its first
// token in the same commit that creates the repository.
func (t *txn) mintToken(in substrate.MintTokenInput) (substrate.TokenInfo, string, error) {
	var zero substrate.TokenInfo
	if in.Label == "" {
		return zero, "", fmt.Errorf("%w: a token needs a label", substrate.ErrValidation)
	}
	// An empty non-nil list would read back as absent, which is full access:
	// the one spelling of full access is no `scopes` at all.
	if in.Scopes != nil && len(in.Scopes) == 0 {
		return zero, "", fmt.Errorf("%w: a scoped token needs at least one scope", substrate.ErrValidation)
	}
	// A scope over the repository's own kinds is stored under `~`, whichever
	// spelling it came in (decision record 0259).
	if in.Scopes != nil {
		local := make([]string, len(in.Scopes))
		for i, raw := range in.Scopes {
			local[i] = raw
			if s, err := scope.Parse(raw); err == nil {
				local[i] = s.Localize(t.ds.info.Authority).String()
			}
		}
		in.Scopes = local
	}
	if err := scope.CheckStored(in.Scopes); err != nil {
		return zero, "", fmt.Errorf("%w: %w", substrate.ErrValidation, err)
	}
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return zero, "", err
	}
	secret := tokenPrefix + hex.EncodeToString(raw)
	props := map[string]any{"label": in.Label, "hash": hashToken(secret)}
	if in.ExpiresAt != nil {
		props["expiresAt"] = in.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if in.Scopes != nil {
		scopes := make([]any, len(in.Scopes))
		for i, s := range in.Scopes {
			scopes[i] = s
		}
		props["scopes"] = scopes
	}
	if in.ClientID != "" {
		props["clientId"] = in.ClientID
	}
	if in.ClientName != "" {
		props["clientName"] = in.ClientName
	}
	if err := validateAppBuild(in.AppPackages, in.AppSoftwareVersion); err != nil {
		return zero, "", err
	}
	if len(in.AppPackages) > 0 {
		props["appPackages"] = appPackageValues(in.AppPackages)
	}
	if in.AppSoftwareVersion != "" {
		props["appSoftwareVersion"] = in.AppSoftwareVersion
	}
	if in.ClientID != "" {
		props["appCheckedAt"] = nowUTC().Format(time.RFC3339Nano)
	}
	if len(in.AppAddons) > 0 {
		raw, err := json.Marshal(in.AppAddons)
		if err != nil {
			return zero, "", err
		}
		var addons []any
		if err := json.Unmarshal(raw, &addons); err != nil {
			return zero, "", err
		}
		props["appAddons"] = addons
	}
	if in.AppAuthority != "" {
		props["appAuthority"] = in.AppAuthority
	}
	if in.AppAuthorityFromFile {
		props["appAuthorityFromFile"] = true
	}
	if in.AppLocal {
		props["appLocal"] = true
	}
	if in.Identity != "" {
		props["identity"] = in.Identity
	}
	ent, err := t.put(substrate.PutInput{Kind: kindToken, Properties: props})
	if err != nil {
		return zero, "", err
	}
	// An app's approval is when its actor comes to be (decision 0270): the
	// record its writes will link, written beside the token they stand on.
	if in.ClientID != "" {
		if err := t.ensureActors(authorize.AppActorFor(in.ClientID, in.AppAuthority, in.AppLocal)); err != nil {
			return zero, "", err
		}
	}
	return substrate.TokenInfo{
		ID: ent.ID, Label: in.Label, Created: ent.CreatedAt, ExpiresAt: in.ExpiresAt,
		Scopes: in.Scopes, ClientID: in.ClientID, ClientName: in.ClientName,
		AppPackages: in.AppPackages, AppSoftwareVersion: in.AppSoftwareVersion, AppAddons: in.AppAddons, AppCheckedAt: appCheckTime(in.ClientID),
		AppAuthority: in.AppAuthority, AppAuthorityFromFile: in.AppAuthorityFromFile, AppLocal: in.AppLocal,
		Identity: in.Identity,
	}, secret, nil
}

// Tokens lists the repository's live token records, newest first. Metadata
// only: the hash is a secret-typed property and never leaves the store.
func (ds *dataset) Tokens(ctx context.Context) ([]substrate.TokenInfo, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id, props->>'label', props->>'expiresAt', props->'scopes',
		       props->>'clientId', props->>'clientName', created_at,
		       props->>'appAuthority', COALESCE((props->>'appLocal')::boolean, false),
		       COALESCE((props->>'appAuthorityFromFile')::boolean, false), props,
		       COALESCE(props->>'identity', '')
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
		var label, expires, clientID, clientName, appAuthority sql.NullString
		var scopes, build []byte
		if err := rows.Scan(&info.ID, &label, &expires, &scopes, &clientID, &clientName, &info.Created, &appAuthority, &info.AppLocal, &info.AppAuthorityFromFile, &build, &info.Identity); err != nil {
			return nil, err
		}
		info.Label = label.String
		info.ExpiresAt = parseStamp(expires)
		// A damaged grant is refused here as at Authenticate: listing it
		// without scopes would show an app's token as a full-access one. The
		// refusal names the token, which the owner can still revoke by id.
		// A grant a later decision retired is not damage: it lists with the
		// scopes it was approved with, so one such token never fails the
		// owner's list, and Authenticate still refuses it.
		var derr error
		if info.Scopes, derr = decodeScopes(scopes); derr != nil && !errors.Is(derr, scope.ErrRetired) {
			return nil, fmt.Errorf("%w: token %s: %w", substrate.ErrCorrupt, info.ID, derr)
		}
		info.Retired = errors.Is(derr, scope.ErrRetired)
		if err := readAppBuild(build, &info); err != nil {
			return nil, err
		}
		info.ClientID, info.ClientName, info.AppAuthority = clientID.String, clientName.String, appAuthority.String
		out = append(out, info)
	}
	return out, rows.Err()
}

// decodeScopes reads a token record's `scopes` column: nil when the
// property is absent (full access), an error when it is present and not a
// non-empty list of strings, which Authenticate turns into a refusal rather
// than reading a damaged grant as no grant at all. A list that does not pass
// the stored check comes back beside its error, for the listing to show a
// retired grant as it was approved.
func decodeScopes(raw []byte) ([]string, error) {
	// Only an ABSENT property is full access: SQL NULL from `props->'scopes'`
	// when the key is missing. A JSON null is a property that is present and
	// says nothing, and is refused like any other damage.
	if raw == nil {
		return nil, nil
	}
	var list []any
	if err := json.Unmarshal(raw, &list); err != nil || list == nil {
		return nil, errors.New("token scopes are not a list")
	}
	scopes := make([]string, len(list))
	for i, v := range list {
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("token scopes hold an entry that is not a string")
		}
		scopes[i] = s
	}
	if len(scopes) == 0 {
		return nil, errors.New("token scopes are empty")
	}
	if err := scope.CheckStored(scopes); err != nil {
		return scopes, fmt.Errorf("token scopes unreadable: %w", err)
	}
	return scopes, nil
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
	return s.resolveToken(ctx, tokenSecret, false)
}

// RevokeToken never hands a retired grant's dataset back to a caller.
func (s *service) RevokeToken(ctx context.Context, tokenSecret, clientID string) error {
	ds, info, err := s.resolveToken(ctx, tokenSecret, true)
	if errors.Is(err, substrate.ErrAuth) {
		return nil
	}
	if err != nil {
		return err
	}
	if clientID != "" && clientID != info.ClientID {
		return nil
	}
	_, err = ds.Delete(ctx, substrate.ActorPerson, kindToken, info.ID, substrate.DeleteInput{})
	if errors.Is(err, substrate.ErrNotFound) {
		return nil
	}
	return err
}

func (s *service) resolveToken(ctx context.Context, tokenSecret string, revoking bool) (substrate.Dataset, substrate.TokenInfo, error) {
	var zero substrate.TokenInfo
	rest, ok := strings.CutPrefix(tokenSecret, tokenPrefix)
	if !ok || rest == "" {
		return nil, zero, fmt.Errorf("%w: malformed token", substrate.ErrAuth)
	}
	if _, err := hex.DecodeString(rest); err != nil {
		return nil, zero, fmt.Errorf("%w: malformed token", substrate.ErrAuth)
	}
	hash := hashToken(tokenSecret)
	if err := s.ruleFault("token"); err != nil {
		return nil, zero, fmt.Errorf("read the token: %w", err)
	}
	repo, info, err := s.cachedTokenByHash(ctx, hash)
	if err != nil {
		return nil, zero, err
	}
	if info.Retired && !revoking {
		return nil, zero, fmt.Errorf("%w: retired token grant", substrate.ErrAuth)
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		// A well-formed token whose real repository could NOT be opened is a
		// service condition, not a bad credential: surface it so
		// the API maps it to a 5xx instead of a misleading "invalid token".
		return nil, zero, fmt.Errorf("open repository %s: %w", repo.label(), err)
	}
	// A token from before identities names its person at the repository's
	// first open (nameTokensPeople), which may be this one: the request acts
	// for whom the token names now, or an export it asked for would carry
	// nobody and GET /identity would answer nobody.
	if info.Identity == "" {
		if _, info, err = s.cachedTokenByHash(ctx, hash); err != nil {
			return nil, zero, err
		}
		if info.Retired && !revoking {
			return nil, zero, fmt.Errorf("%w: retired token grant", substrate.ErrAuth)
		}
	}
	return ds, info, nil
}

// tokenRepositoryColumns is repositoryColumns qualified for tokenByHash's
// join, derived so the two lists cannot drift apart.
var tokenRepositoryColumns = "p." + strings.ReplaceAll(repositoryColumns, ", ", ", p.")

// tokenByHash is the cross-repository lookup — the fourth and last
// maint-pool path. It matches on the jsonb containment operator so the
// records props index serves it rather than a scan of every repository, and
// joins the repositories row the token lives in, so a request costs one round
// trip on the maintenance pool. A token whose repository row is gone matches
// nothing. A token naming a person authenticates only while that person may
// open its repository; one naming none is from before identities, and the
// open names it after the repository's person (nameTokensPeople).
func (s *service) tokenByHash(ctx context.Context, hash string) (Repository, substrate.TokenInfo, error) {
	var zero substrate.TokenInfo
	var info substrate.TokenInfo
	var label, got, expires, clientID, clientName, appAuthority sql.NullString
	var scopes, build []byte
	row := s.maint.QueryRowContext(ctx, `
		SELECT `+tokenRepositoryColumns+`,
		       r.id, r.props->>'label', r.props->>'hash', r.props->>'expiresAt',
		       r.props->'scopes', r.props->>'clientId', r.props->>'clientName', r.created_at,
		       r.props->>'appAuthority', COALESCE((r.props->>'appLocal')::boolean, false),
		       COALESCE((r.props->>'appAuthorityFromFile')::boolean, false), r.props,
		       COALESCE(r.props->>'identity', ''),
		       COALESCE(r.props->>'identity', '') = '' OR EXISTS (SELECT 1 FROM identity_repositories l
		           WHERE l.identity = r.props->>'identity' AND l.repository_id = r.repository)
		FROM records r JOIN repositories p ON p.id = r.repository
		WHERE r.kind = $1 AND r.deleted_at IS NULL AND r.props @> jsonb_build_object('hash', $2::text)`,
		kindToken, hash)
	var member bool
	repo, err := scanRepositoryRow(func(dest ...any) error {
		return row.Scan(append(dest, &info.ID, &label, &got, &expires, &scopes, &clientID, &clientName, &info.Created, &appAuthority, &info.AppLocal, &info.AppAuthorityFromFile, &build, &info.Identity, &member)...)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, zero, fmt.Errorf("%w: unknown token", substrate.ErrAuth)
	}
	if err != nil {
		return Repository{}, zero, err
	}
	if subtle.ConstantTimeCompare([]byte(got.String), []byte(hash)) != 1 {
		return Repository{}, zero, fmt.Errorf("%w: unknown token", substrate.ErrAuth)
	}
	if !member {
		return Repository{}, zero, errPersonMayNotOpen
	}
	info.Label = label.String
	info.ClientID, info.ClientName, info.AppAuthority = clientID.String, clientName.String, appAuthority.String
	// A grant that cannot be read fails closed: reading it as absent would
	// hand an app's token full access.
	if info.Scopes, err = decodeScopes(scopes); err != nil && !errors.Is(err, scope.ErrRetired) {
		return Repository{}, zero, fmt.Errorf("%w: %w", substrate.ErrAuth, err)
	}
	info.Retired = errors.Is(err, scope.ErrRetired)
	if err := readAppBuild(build, &info); err != nil {
		return Repository{}, zero, err
	}
	// Expiry is SERVER-ENFORCED here: a token past its stamp authenticates as
	// a spent credential, so no dataset is ever handed out for it. An
	// unreadable stamp fails closed for the same reason.
	if expires.Valid && expires.String != "" {
		exp, perr := time.Parse(time.RFC3339, expires.String)
		if perr != nil {
			return Repository{}, zero, fmt.Errorf("%w: token expiry unreadable", substrate.ErrAuth)
		}
		if nowUTC().After(exp) {
			return Repository{}, zero, fmt.Errorf("%w: token expired", substrate.ErrAuth)
		}
		info.ExpiresAt = &exp
	}
	return repo, info, nil
}
