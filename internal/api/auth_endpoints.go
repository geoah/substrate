package api

// THE DOOR, as HTTP. `/register`, `/login` and `/tokens` sit BESIDE the
// versioned API: no repository segment anywhere, because
// registration has no repository yet and everything after it takes one from
// the token.
//
// Registration and the credential changes are the substrate's only
// unauthenticated write paths, so all four share one posture:
//
//   - a per-IP, per-username and GLOBAL rate limit (h.authRate), so a
//     distributed attempt costs what a single one does;
//   - one answer for every failure — an unknown username, a wrong password
//     and a wrong code are indistinguishable, which is why the engine does
//     the same argon2id and HMAC work on all three;
//   - the password-factor rule: a credential change carries
//     BOTH current factors in its body. A bearer token alone is refused,
//     so a leaked token's blast radius is the data, never the account.

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// --- wire shapes ---

type registerBeginRequest struct {
	InviteCode string `json:"inviteCode"`
	Username   string `json:"username"`
}

type registerRequest struct {
	InviteCode string `json:"inviteCode"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totpSecret"`
	TOTPCode   string `json:"totpCode"`
	Label      string `json:"label,omitempty"`
	// RecoveryPublicKey is the client-generated age recipient; absent asks
	// the server to mint the pair and return the identity once.
	RecoveryPublicKey string `json:"recoveryPublicKey,omitempty"`
}

// registerResponse is the one response that may carry a server-minted
// recovery identity, shown exactly once like the token secret beside it.
type registerResponse struct {
	Token             substrate.TokenInfo `json:"token"`
	Secret            string              `json:"secret"`
	RecoveryKey       string              `json:"recoveryKey,omitempty"`
	RecoveryPublicKey string              `json:"recoveryPublicKey,omitempty"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTPCode string `json:"totpCode"`
	Label    string `json:"label,omitempty"`
}

type passwordRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	TOTPCode    string `json:"totpCode"`
	NewPassword string `json:"newPassword"`
}

type totpBeginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTPCode string `json:"totpCode"`
}

type totpRequest struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	TOTPCode      string `json:"totpCode"`
	NewTOTPSecret string `json:"newTotpSecret"`
	NewTOTPCode   string `json:"newTotpCode"`
}

type mintRequest struct {
	Label     string     `json:"label"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

type tokenResponse struct {
	Token  substrate.TokenInfo `json:"token"`
	Secret string              `json:"secret"`
}

type tokenListResponse struct {
	Tokens []substrate.TokenInfo `json:"tokens"`
}

// --- the shared gate ---

// authGate rate-limits one unauthenticated auth request. A refusal has already
// written the response. `cost` is what this call spends of the interval's
// allowance: costRequest for a request that IS the attempt, costPaired for one
// of the two calls of the registration gesture.
//
// There is NO failure lockout. One keyed off the caller is a denial-of-service
// lever rather than a defense: the keys include a username anybody may name,
// so a stranger could lock any account — and while a substrate-wide key sat in
// that set, five unauthenticated failures took login, registration and both
// credential changes offline for EVERY user, doubling to an hour and
// unclearable, because only a request that got past the lock could clear the
// run. Pacing is what remains, and it is per-key: a flood costs the flooder.
//
// The bucket is substrate-wide on purpose: globalKey holds globalBurst
// attempts, so a spray across many usernames is capped without any one honest
// user losing their own allowance.
func (h *handler) authGate(w http.ResponseWriter, r *http.Request, username string, cost int) bool {
	name := limiterUsername(username)
	keys := []string{peerIP(r) + "|" + name, "user|" + name, globalKey}
	if ok, wait := h.authRate.allow(cost, keys...); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(wait)))
		writeError(w, http.StatusTooManyRequests, codeRateLimited, "too many requests")
		return false
	}
	return true
}

// inviteOK compares the presented invite code with the configured one in
// constant time. An unconfigured code means registration is OFF — the
// endpoint answers `unsupported`, the same way every absent capability does.
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
		writeError(w, http.StatusUnauthorized, codeAuth, "invalid username, password or code")
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
	ok := h.authGate(w, r, req.Username, costPaired)
	if !ok {
		return
	}
	if !h.inviteOK(w, req.InviteCode) {
		return
	}
	enrollment, err := h.svc.BeginRegistration(r.Context(), req.Username)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	// Enroll proves the shared invite code and NOTHING about the named user —
	// the engine authenticates nobody here, and it writes nothing.
	writeJSON(w, http.StatusOK, enrollment)
}

// postRegister creates the user and returns the first token, so registration
// ends logged in. Everything durable happens here or not at all.
func (h *handler) postRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	ok := h.authGate(w, r, req.Username, costPaired)
	if !ok {
		return
	}
	if !h.inviteOK(w, req.InviteCode) {
		return
	}
	res, err := h.svc.Register(r.Context(), substrate.RegisterInput{
		Username:          req.Username,
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
	writeJSON(w, http.StatusCreated, registerResponse{
		Token: res.Token, Secret: res.Secret,
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
	ok := h.authGate(w, r, req.Username, costRequest)
	if !ok {
		return
	}
	info, secret, err := h.svc.Login(r.Context(), substrate.LoginInput{
		Username: req.Username, Password: req.Password,
		TOTPCode: req.TOTPCode, Label: req.Label,
	})
	if err != nil {
		writeAuthFailure(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, tokenResponse{Token: info, Secret: secret})
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
	ok := h.authGate(w, r, req.Username, costRequest)
	if !ok {
		return
	}
	if !h.factorsPresented(w, req.Password, req.TOTPCode) {
		return
	}
	err := h.svc.ChangePassword(r.Context(), substrate.LoginInput{
		Username: req.Username, Password: req.Password, TOTPCode: req.TOTPCode,
	}, req.NewPassword)
	if err != nil {
		writeAuthFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"username": req.Username})
}

// postTOTPBegin verifies the current factors and issues a candidate seed,
// writing nothing — an abandoned re-enrollment cannot lock anyone out.
func (h *handler) postTOTPBegin(w http.ResponseWriter, r *http.Request) {
	var req totpBeginRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	ok := h.authGate(w, r, req.Username, costRequest)
	if !ok {
		return
	}
	if !h.factorsPresented(w, req.Password, req.TOTPCode) {
		return
	}
	enrollment, err := h.svc.BeginTOTPReenrollment(r.Context(), substrate.LoginInput{
		Username: req.Username, Password: req.Password, TOTPCode: req.TOTPCode,
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
	ok := h.authGate(w, r, req.Username, costRequest)
	if !ok {
		return
	}
	if !h.factorsPresented(w, req.Password, req.TOTPCode) {
		return
	}
	err := h.svc.ReenrollTOTP(r.Context(), substrate.LoginInput{
		Username: req.Username, Password: req.Password, TOTPCode: req.TOTPCode,
	}, req.NewTOTPSecret, req.NewTOTPCode)
	if err != nil {
		writeAuthFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"username": req.Username})
}

// factorsPresented enforces the password-factor rule at the door (ruling
// RB-6). A request that brings a bearer token and no password is not an
// authentication failure — it is a REFUSAL of the whole idea: this endpoint
// does not accept tokens, and saying so with 403 is what tells a caller their
// token will never be enough.
func (h *handler) factorsPresented(w http.ResponseWriter, password, code string) bool {
	if password != "" && code != "" {
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
	writeJSON(w, http.StatusCreated, tokenResponse{Token: info, Secret: secret})
}

// recoveryEnroller is the engine seam behind one-time recovery enrollment,
// deliberately off the frozen contract: it exists for repositories that
// predate recovery keys, and registration is the ordinary door.
type recoveryEnroller interface {
	EnrollRecoveryKey(ctx context.Context, in substrate.LoginInput, publicKey string) (identity, recipient string, err error)
}

type recoveryEnrollRequest struct {
	Username          string `json:"username"`
	Password          string `json:"password"`
	TOTPCode          string `json:"totpCode"`
	RecoveryPublicKey string `json:"recoveryPublicKey,omitempty"`
}

type recoveryEnrollResponse struct {
	RecoveryKey       string `json:"recoveryKey,omitempty"`
	RecoveryPublicKey string `json:"recoveryPublicKey"`
}

// postRecoveryEnroll enrolls a recovery key on a repository that predates
// them. Both current factors in the body, a bearer refused as evidence,
// exactly like the credential changes: a stolen token must not be able to
// claim the one recovery slot and walk away with an offline key.
func (h *handler) postRecoveryEnroll(w http.ResponseWriter, r *http.Request) {
	var req recoveryEnrollRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if ok := h.authGate(w, r, req.Username, costRequest); !ok {
		return
	}
	if !h.factorsPresented(w, req.Password, req.TOTPCode) {
		return
	}
	enroller, ok := h.svc.(recoveryEnroller)
	if !ok {
		writeError(w, http.StatusNotImplemented, codeUnsupported, "this build does not support recovery enrollment")
		return
	}
	identity, recipient, err := enroller.EnrollRecoveryKey(r.Context(), substrate.LoginInput{
		Username: req.Username, Password: req.Password, TOTPCode: req.TOTPCode,
	}, req.RecoveryPublicKey)
	if err != nil {
		writeAuthFailure(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, recoveryEnrollResponse{
		RecoveryKey: identity, RecoveryPublicKey: recipient,
	})
}

func (h *handler) getTokens(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tokens, err := DatasetFrom(ctx).Tokens(ctx)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	if tokens == nil {
		tokens = []substrate.TokenInfo{}
	}
	writeJSON(w, http.StatusOK, tokenListResponse{Tokens: tokens})
}

// deleteToken revokes: it deletes the token record, which is the same write
// the ordinary record surface performs at
// DELETE /api/v1/core.substrate.reamde.dev/tokens/{id}. No row means no access — there
// is no revocation list and nothing to expire.
func (h *handler) deleteToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ent, err := DatasetFrom(ctx).Delete(ctx, ActorFrom(ctx), tokenType, pathParam(r, "id"))
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ent)
}

// tokenType is the token kind's reference — the one place the HTTP layer
// names it, for the revoke path.
const tokenType = coreAuthority + "/token"
