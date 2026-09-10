package api

// THE DOOR, as HTTP. `/register`, `/login` and `/tokens` sit BESIDE the
// versioned API: no repository segment anywhere, because
// registration has no repository yet and everything after it takes one from
// the token.
//
// Registration and the credential changes are the substrate's only
// unauthenticated write paths, so all four share one posture:
//
//   - a per-IP, per-repository and GLOBAL rate limit (h.authRate), so a
//     distributed attempt costs what a single one does;
//   - one answer for every failure — an unknown repository, a wrong password
//     and a wrong code are indistinguishable, which is why the engine does
//     the same argon2id and HMAC work on all three;
//   - the password-factor rule: a credential change carries
//     BOTH current factors in its body. A bearer token alone is refused,
//     so a leaked token's blast radius is the data, never the account.

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// --- wire shapes ---

type registerBeginRequest struct {
	InviteCode string `json:"inviteCode"`
	Repository string `json:"repository"`
}

// loginRequest is the wire struct internal/substrate declares, so a client
// built against the contract sends the keys the door decodes.
type loginRequest = substrate.LoginRequest

type passwordRequest struct {
	Repository  string `json:"repository"`
	Password    string `json:"password"`
	TOTPCode    string `json:"totpCode"`
	NewPassword string `json:"newPassword"`
}

type totpBeginRequest struct {
	Repository string `json:"repository"`
	Password   string `json:"password"`
	TOTPCode   string `json:"totpCode"`
}

type totpRequest struct {
	Repository    string `json:"repository"`
	Password      string `json:"password"`
	TOTPCode      string `json:"totpCode"`
	NewTOTPSecret string `json:"newTotpSecret"`
	NewTOTPCode   string `json:"newTotpCode"`
}

// mintRequest is the mint body. Both fields may be absent; the label defaults
// to "token".
type mintRequest struct {
	Label     string     `json:"label,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// --- the shared gate ---

// repositoryOf resolves the repository name a caller sent into the authority
// it means: the name itself when it carries a dot, else that label under the
// host the request reached (`ada` -> `ada.example.com`). The HTTP layer is the
// one place that knows the host, so every door resolves here and the engine
// sees a concrete authority or refuses. An empty name stays empty, so the
// engine's own refusal is what the caller reads.
func repositoryOf(r *http.Request, name string) string {
	return vocabulary.RepositoryAuthority(strings.TrimSpace(name), r.Host)
}

// authGate rate-limits one unauthenticated auth request. A refusal has already
// written the response. `cost` is what this call spends of the interval's
// allowance: costRequest for a request that IS the attempt, costPaired for one
// of the two calls of the registration gesture.
//
// There is NO failure lockout. One keyed off the caller is a denial-of-service
// lever rather than a defense: the keys include a repository anybody may name,
// so a stranger could lock any account — and while a substrate-wide key sat in
// that set, five unauthenticated failures took login, registration and both
// credential changes offline for EVERY user, doubling to an hour and
// unclearable, because only a request that got past the lock could clear the
// run. Pacing is what remains, and it is per-key: a flood costs the flooder.
//
// The bucket is substrate-wide on purpose: globalKey holds globalBurst
// attempts, so a spray across many repositories is capped without any one
// honest user losing their own allowance.
func (h *handler) authGate(w http.ResponseWriter, r *http.Request, repository string, cost int) bool {
	name := limiterRepository(repository)
	keys := []string{peerIP(r) + "|" + name, "user|" + name, globalKey}
	if ok, wait := h.authRate.allow(cost, keys...); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(wait)))
		writeError(w, http.StatusTooManyRequests, codeRateLimited, "too many requests")
		return false
	}
	return true
}

// inviteOK compares the presented invite code with the configured one in
// constant time. An unconfigured code means registration is OFF, and this is
// the one door that answers `unsupported`: a 501 here is configuration, not a
// capability the build lacks.
func (h *handler) inviteOK(w http.ResponseWriter, presented string) bool {
	if h.inviteCode == "" {
		writeUnsupported(w, "this substrate is not open for registration")
		return false
	}
	if subtle.ConstantTimeCompare([]byte(h.inviteCode), []byte(presented)) != 1 {
		writeError(w, http.StatusUnauthorized, codeAuth, "invalid invite code")
		return false
	}
	return true
}

// writeAuthFailure is the one answer every refused factor gets: no oracle for
// which of the three was wrong, and none for whether the user exists.
func writeAuthFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, substrate.ErrAuth) {
		writeError(w, http.StatusUnauthorized, codeAuth, "invalid repository, password or code")
		return
	}
	writeSubstrateError(w, err)
}

// --- registration ---

// postRegisterBegin issues the TOTP enrollment. It writes nothing: the caller
// holds the seed and hands it back with one code, so an abandoned
// registration leaves no row to expire and nothing to sweep.
func (h *handler) postRegisterBegin(w http.ResponseWriter, r *http.Request) {
	var req registerBeginRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	repository := repositoryOf(r, req.Repository)
	ok := h.authGate(w, r, repository, costPaired)
	if !ok {
		return
	}
	if !h.inviteOK(w, req.InviteCode) {
		return
	}
	enrollment, err := h.svc.BeginRegistration(r.Context(), repository)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	// Enroll proves the shared invite code and NOTHING about the named
	// repository — the engine authenticates nobody here, and it writes
	// nothing.
	writeJSON(w, http.StatusOK, enrollment)
}

// postRegister creates the user and returns the first token, so registration
// ends logged in. Everything durable happens here or not at all.
func (h *handler) postRegister(w http.ResponseWriter, r *http.Request) {
	var req substrate.RegisterRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	repository := repositoryOf(r, req.Repository)
	ok := h.authGate(w, r, repository, costPaired)
	if !ok {
		return
	}
	if !h.inviteOK(w, req.InviteCode) {
		return
	}
	res, err := h.svc.Register(r.Context(), substrate.RegisterInput{
		Repository:        repository,
		Password:          req.Password,
		TOTPSecret:        req.TOTPSecret,
		TOTPCode:          req.TOTPCode,
		Label:             req.Label,
		RecoveryPublicKey: req.RecoveryPublicKey,
	})
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, substrate.Registered{
		MintedToken: substrate.MintedToken{
			Token: res.Token, Secret: res.Secret, Repository: res.Repository,
		},
		RecoveryKey: res.RecoveryKey, RecoveryPublicKey: res.RecoveryPublicKey,
	})
}

// --- login ---

// postLogin mints a token record and hands back its secret once. There is no
// session beside it: the console holds a token like every other client.
func (h *handler) postLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	req.Repository = repositoryOf(r, req.Repository)
	ok := h.authGate(w, r, req.Repository, costRequest)
	if !ok {
		return
	}
	info, secret, err := h.svc.Login(r.Context(), substrate.LoginInput(req))
	if err != nil {
		writeAuthFailure(w, err)
		return
	}
	// The repository is echoed as the door RESOLVED it, so a client that sent
	// a bare label stores the authority rather than the label it typed.
	writeJSON(w, http.StatusCreated, substrate.MintedToken{
		Token: info, Secret: secret, Repository: req.Repository,
	})
}

// --- the credential changes (the password-factor rule) ---

// postPassword changes the password. It takes the CURRENT password and code
// in the body; a bearer token is not evidence here and never will be.
func (h *handler) postPassword(w http.ResponseWriter, r *http.Request) {
	var req passwordRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	repository := repositoryOf(r, req.Repository)
	ok := h.authGate(w, r, repository, costRequest)
	if !ok {
		return
	}
	if !h.factorsPresented(w, req.Password, req.TOTPCode) {
		return
	}
	err := h.svc.ChangePassword(r.Context(), substrate.LoginInput{
		Repository: repository, Password: req.Password, TOTPCode: req.TOTPCode,
	}, req.NewPassword)
	if err != nil {
		writeAuthFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.SessionCredential{Repository: repository})
}

// postTOTPBegin verifies the current factors and issues a candidate seed,
// writing nothing — an abandoned re-enrollment cannot lock anyone out.
func (h *handler) postTOTPBegin(w http.ResponseWriter, r *http.Request) {
	var req totpBeginRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	repository := repositoryOf(r, req.Repository)
	ok := h.authGate(w, r, repository, costRequest)
	if !ok {
		return
	}
	if !h.factorsPresented(w, req.Password, req.TOTPCode) {
		return
	}
	enrollment, err := h.svc.BeginTOTPReenrollment(r.Context(), substrate.LoginInput{
		Repository: repository, Password: req.Password, TOTPCode: req.TOTPCode,
	})
	if err != nil {
		writeAuthFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, enrollment)
}

// postTOTP swaps the second factor, proving BOTH the current factors and one
// code from the new seed.
func (h *handler) postTOTP(w http.ResponseWriter, r *http.Request) {
	var req totpRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	repository := repositoryOf(r, req.Repository)
	ok := h.authGate(w, r, repository, costRequest)
	if !ok {
		return
	}
	if !h.factorsPresented(w, req.Password, req.TOTPCode) {
		return
	}
	err := h.svc.ReenrollTOTP(r.Context(), substrate.LoginInput{
		Repository: repository, Password: req.Password, TOTPCode: req.TOTPCode,
	}, req.NewTOTPSecret, req.NewTOTPCode)
	if err != nil {
		writeAuthFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.SessionCredential{Repository: repository})
}

// factorsPresented enforces the password-factor rule at the door (ruling
// RB-6). A request that brings a bearer token and no password is not an
// authentication failure — it is a REFUSAL of the whole idea: this endpoint
// does not accept tokens, and saying so with 403 is what tells a caller their
// token will never be enough.
//
// The code is not demanded on a deployment whose second factor is disabled
// (SUBSTRATE_INSECURE_DISABLE_TOTP): asking for something nothing verifies
// would refuse honest callers over a formality. The PASSWORD is still
// required — the rule is about what a bearer token buys, and that does not
// change with the factor.
func (h *handler) factorsPresented(w http.ResponseWriter, password, code string) bool {
	if password != "" && (code != "" || h.totpDisabled) {
		return true
	}
	writeError(w, http.StatusForbidden, codeForbidden,
		"changing auth material requires the current password and TOTP code in the request body — a bearer token is not accepted")
	return false
}

// --- tokens ---

// postMintToken mints a script or device token in the caller's own
// repository. It is the authenticated twin of login: the same record, the
// same secret-shown-once, a different door.
func (h *handler) postMintToken(w http.ResponseWriter, r *http.Request) {
	var req mintRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if req.Label == "" {
		req.Label = "token"
	}
	ctx := r.Context()
	info, secret, err := DatasetFrom(ctx).MintToken(ctx, req.Label, req.ExpiresAt)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, substrate.MintedToken{Token: info, Secret: secret})
}

func (h *handler) getTokens(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tokens, err := DatasetFrom(ctx).Tokens(ctx)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.Listed(tokens))
}

// deleteToken revokes: it deletes the token record, which is the same write
// the ordinary record surface performs at
// DELETE /api/v1/substrate.reamde.dev/core/tokens/{id}. No row means no access — there
// is no revocation list and nothing to expire.
func (h *handler) deleteToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ent, err := DatasetFrom(ctx).Delete(ctx, ActorFrom(ctx), tokenType, pathParam(r, "id"), substrate.DeleteInput{})
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ent)
}

// tokenType is the token kind's reference — the one place the HTTP layer
// names it, for the revoke path.
const tokenType = corePackage + "/token"
