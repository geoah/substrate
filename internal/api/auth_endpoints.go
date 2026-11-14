package api

// THE DOOR, as HTTP. `/register`, `/login` and `/tokens` sit BESIDE the
// versioned API: no repository segment anywhere, because
// registration has no repository yet and everything after it takes one from
// the token.
//
// Registration and the credential changes are the substrate's only
// unauthenticated write paths, so all four share one posture:
//
//   - a per-IP, per-name and GLOBAL rate limit (h.authRate), so a
//     distributed attempt costs what a single one does;
//   - one answer for every failure — an unknown identity, a wrong password
//     and a wrong code are indistinguishable, which is why the engine does
//     the same argon2id and HMAC work on all three. The one other answer,
//     totp_required, comes only after the password verified;
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

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// --- wire shapes ---

type registerBeginRequest struct {
	InviteCode string `json:"inviteCode"`
	Identity   string `json:"identity"`
}

// loginRequest is the wire struct internal/substrate declares, so a client
// built against the contract sends the keys the door decodes.
type loginRequest = substrate.LoginRequest

type passwordRequest struct {
	Identity    string `json:"identity"`
	Password    string `json:"password"`
	TOTPCode    string `json:"totpCode"`
	NewPassword string `json:"newPassword"`
	// SignOutEverywhere ends every token the person holds once the password
	// changed (postSignOutEverywhere), in the same request, because the code
	// it carries is spent once.
	SignOutEverywhere bool `json:"signOutEverywhere,omitempty"`
}

// signOutRequest is the sign out everywhere: the identity and its two
// current factors, nothing else.
type signOutRequest struct {
	Identity string `json:"identity"`
	Password string `json:"password"`
	TOTPCode string `json:"totpCode"`
}

type totpBeginRequest struct {
	Identity string `json:"identity"`
	Password string `json:"password"`
	TOTPCode string `json:"totpCode"`
}

type totpRequest struct {
	Identity      string `json:"identity"`
	Password      string `json:"password"`
	TOTPCode      string `json:"totpCode"`
	NewTOTPSecret string `json:"newTotpSecret"`
	NewTOTPCode   string `json:"newTotpCode"`
}

// mintRequest is the mint body. Every field may be absent; the label
// defaults to "token". Scopes, when present, is `["mcp"]` alone: a token for
// an assistant the owner pastes into it, which works at the MCP door alone
// (decision 0230). Any other scoped token comes from the authorization flow.
type mintRequest struct {
	Label     string     `json:"label,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	Scopes    []string   `json:"scopes,omitempty"`
}

// deleteRequest is the deletion of a repository: the identity, its two
// current factors and the repository to erase.
type deleteRequest struct {
	Identity   string `json:"identity"`
	Repository string `json:"repository"`
	Password   string `json:"password"`
	TOTPCode   string `json:"totpCode"`
}

// --- the shared gate ---

// repositoryOf resolves the name a caller sent for an existing repository
// (vocabulary.NameRule.Lookup): the name itself when it carries a dot, else
// that label under the public URL's host, or the request's where none is
// configured. The HTTP layer is the one place that knows either, so every
// door resolves here and the engine sees a whole name or refuses. An empty
// name stays empty, so the engine's own refusal is what the caller reads.
func (h *handler) repositoryOf(r *http.Request, name string) string {
	return h.names.Lookup(name, r.Host)
}

// newRepositoryName is the name a new repository typed as typed takes
// (vocabulary.NameRule.New), for a registration and a rename's new name, and
// the name a registering person takes by the same rule. A refusal has
// already been written.
func (h *handler) newRepositoryName(w http.ResponseWriter, typed string) (string, bool) {
	name, err := h.names.New(typed)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeValidation, err.Error())
		return "", false
	}
	return name, true
}

// identityOf resolves the sign-in name a caller sent the way repositoryOf
// resolves a repository's: a sign-in name has the same DNS-style form, and a
// short one is completed under the same host (decision record 0288).
func (h *handler) identityOf(r *http.Request, name string) string {
	return h.names.Lookup(name, r.Host)
}

// repositoryKeyOf resolves a repository a caller names by its name or its
// id: an id is taken as it is, a name as repositoryOf takes it.
func (h *handler) repositoryKeyOf(r *http.Request, name string) string {
	if name = strings.TrimSpace(name); vocabulary.ValidRepositoryID(name) {
		return name
	}
	return h.repositoryOf(r, name)
}

// namesAPerson refuses a request that names no identity: a sign-in and every
// credential change name the person (decision 0274), and a request that
// still names only a repository is a client from before it, told what
// replaced the field.
func namesAPerson(w http.ResponseWriter, identity, door string) bool {
	if identity != "" {
		return true
	}
	writeError(w, http.StatusBadRequest, codeBadRequest,
		door+" names a person: send `identity`, the name the person signs in with; a repository is no longer a sign-in name")
	return false
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
// constant time. No configured code means the door reads none: whatever was
// presented, including nothing, admits, and discovery has said so.
func (h *handler) inviteOK(w http.ResponseWriter, presented string) bool {
	if h.inviteCode == "" {
		return true
	}
	if subtle.ConstantTimeCompare([]byte(h.inviteCode), []byte(presented)) != 1 {
		writeError(w, http.StatusUnauthorized, codeAuth, "invalid invite code")
		return false
	}
	return true
}

// writeAuthFailure is the one answer every refused factor gets: no oracle for
// which of the three was wrong, and none for whether the user exists. A
// missing code on an account that has a second factor is the exception, told
// only to a caller who proved the password.
func writeAuthFailure(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, substrate.ErrTOTPRequired) {
		writeError(w, http.StatusUnauthorized, codeTOTPRequired, "a code from your authenticator is required")
		return
	}
	if errors.Is(err, substrate.ErrAuth) {
		writeError(w, http.StatusUnauthorized, codeAuth, "invalid identity, password or code")
		return
	}
	writeSubstrateError(w, r, err)
}

// --- registration ---

// postRegisterBegin issues the TOTP enrollment. It writes nothing: the caller
// holds the seed and hands it back with one code, so an abandoned
// registration leaves no row to expire and nothing to sweep.
func (h *handler) postRegisterBegin(w http.ResponseWriter, r *http.Request) {
	var req registerBeginRequest
	if !readJSON(w, r, &req, jsonBody) {
		return
	}
	// The pace and the invite come first, keyed by the name as a lookup
	// reads it; the name the person takes is completed after them.
	if !namesAPerson(w, h.identityOf(r, req.Identity), "registration") {
		return
	}
	ok := h.authGate(w, r, h.identityOf(r, req.Identity), costPaired)
	if !ok {
		return
	}
	if !h.inviteOK(w, req.InviteCode) {
		return
	}
	identity, named := h.newRepositoryName(w, req.Identity)
	if !named {
		return
	}
	enrollment, err := h.svc.BeginRegistration(r.Context(), identity)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	// Enroll proves the shared invite code and NOTHING about the named
	// identity — the engine authenticates nobody here, and it writes
	// nothing.
	writeJSON(w, http.StatusOK, enrollment)
}

// postRegister creates the user and returns the first token, so registration
// ends logged in. Everything durable happens here or not at all.
func (h *handler) postRegister(w http.ResponseWriter, r *http.Request) {
	var req substrate.RegisterRequest
	if !readJSON(w, r, &req, jsonBody) {
		return
	}
	// The pace and the invite come first, keyed by the name as a lookup
	// reads it; the name the person takes is completed after them.
	if !namesAPerson(w, h.identityOf(r, req.Identity), "registration") {
		return
	}
	ok := h.authGate(w, r, h.identityOf(r, req.Identity), costPaired)
	if !ok {
		return
	}
	if !h.inviteOK(w, req.InviteCode) {
		return
	}
	identity, named := h.newRepositoryName(w, req.Identity)
	if !named {
		return
	}
	repository := identity
	if strings.TrimSpace(req.Repository) != "" {
		if repository, named = h.newRepositoryName(w, req.Repository); !named {
			return
		}
	}
	res, err := h.svc.Register(r.Context(), substrate.RegisterInput{
		Identity:   identity,
		Repository: repository,
		Password:   req.Password,
		TOTPSecret: req.TOTPSecret,
		TOTPCode:   req.TOTPCode,
		SkipTOTP:   req.SkipTOTP,
		Label:      req.Label,
	})
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, substrate.Registered{
		MintedToken: substrate.MintedToken(res),
	})
}

// --- login ---

// postLogin signs a person in: it names the identity, proves the factors,
// picks the repository and mints a token record there, handing back its
// secret once. There is no session beside it: the console holds a token like
// every other client. A person in several repositories who names none is
// answered 409 with the repositories to pick from, and their code is not
// spent, so the same request sent again with `repository` signs in.
func (h *handler) postLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !readJSON(w, r, &req, jsonBody) {
		return
	}
	identity := h.identityOf(r, req.Identity)
	if !namesAPerson(w, identity, "a sign-in") {
		return
	}
	ok := h.authGate(w, r, identity, costRequest)
	if !ok {
		return
	}
	minted, err := h.svc.Login(r.Context(), substrate.LoginInput{
		Identity: identity, Repository: h.repositoryKeyOf(r, req.Repository),
		Password: req.Password, TOTPCode: req.TOTPCode, Label: req.Label,
	})
	if err != nil {
		writeAuthFailure(w, r, err)
		return
	}
	// The names are echoed as the door RESOLVED them, so a client that sent a
	// bare label stores the name rather than the label it typed.
	writeJSON(w, http.StatusCreated, minted)
}

// --- the credential changes (the password-factor rule) ---

// postPassword changes the password. It takes the CURRENT password and code
// in the body; a bearer token is not evidence here and never will be.
func (h *handler) postPassword(w http.ResponseWriter, r *http.Request) {
	var req passwordRequest
	if !readJSON(w, r, &req, jsonBody) {
		return
	}
	identity := h.identityOf(r, req.Identity)
	if !namesAPerson(w, identity, "a password change") {
		return
	}
	ok := h.authGate(w, r, identity, costRequest)
	if !ok {
		return
	}
	if !h.factorsPresented(w, req.Password) {
		return
	}
	in := substrate.LoginInput{Identity: identity, Password: req.Password, TOTPCode: req.TOTPCode}
	if req.SignOutEverywhere {
		if req.NewPassword == "" {
			writeError(w, http.StatusUnprocessableEntity, codeValidation, "newPassword is required")
			return
		}
		ended, err := h.svc.SignOutEverywhere(r.Context(), in, req.NewPassword)
		if errors.Is(err, substrate.ErrSignOutIncomplete) {
			writeSignOutIncomplete(w, r, err)
			return
		}
		if err != nil {
			writeAuthFailure(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, substrate.SessionCredential{Identity: identity, SignedOut: &ended})
		return
	}
	if err := h.svc.ChangePassword(r.Context(), in, req.NewPassword); err != nil {
		writeAuthFailure(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.SessionCredential{Identity: identity})
}

// writeSignOutIncomplete answers a password change whose sign out everywhere
// did not finish with which half landed and the way on, whatever the cause
// (which the log keeps). A write in doubt keeps its status and reason
// (decision 0271), and its message never says the password changed, since it
// may not have.
func writeSignOutIncomplete(w http.ResponseWriter, r *http.Request, err error) {
	status, p := http.StatusInternalServerError, substrate.ErrorPayload{
		Code:    codeInternal,
		Message: "the password changed, and signing out everywhere did not finish: sign out everywhere again with the new password",
	}
	if errors.Is(err, substrate.ErrWriteInDoubt) {
		status, p = inDoubtProblem(err)
		p.Message = "the password may have changed, and signing out everywhere may not have finished: sign out everywhere again, with the new password or, if it is refused, the old one"
	}
	writeProblem(w, r, err, status, p)
}

// postSignOutEverywhere ends every token the person holds: every sign-in,
// every token minted from one, every assistant's and every app's, so a
// leaked token and whatever it minted or approved stop at once, open streams
// included. It takes both current factors in the body, like the credential
// changes: it is how a person gets their account back from a leaked token,
// so a token is no evidence for it.
func (h *handler) postSignOutEverywhere(w http.ResponseWriter, r *http.Request) {
	var req signOutRequest
	if !readJSON(w, r, &req, jsonBody) {
		return
	}
	identity := h.identityOf(r, req.Identity)
	if !namesAPerson(w, identity, "a sign out everywhere") {
		return
	}
	if !h.authGate(w, r, identity, costRequest) {
		return
	}
	if !h.factorsPresented(w, req.Password) {
		return
	}
	ended, err := h.svc.SignOutEverywhere(r.Context(), substrate.LoginInput{
		Identity: identity, Password: req.Password, TOTPCode: req.TOTPCode,
	}, "")
	if err != nil {
		writeAuthFailure(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.SessionCredential{Identity: identity, SignedOut: &ended})
}

// postDeleteRepository erases the repository it names whole and frees its
// name. Like the credential changes it takes the person's two current
// factors in the body, so a leaked bearer token cannot delete the repository
// it reaches.
func (h *handler) postDeleteRepository(w http.ResponseWriter, r *http.Request) {
	var req deleteRequest
	if !readJSON(w, r, &req, jsonBody) {
		return
	}
	identity := h.identityOf(r, req.Identity)
	if !namesAPerson(w, identity, "deleting a repository") {
		return
	}
	ok := h.authGate(w, r, identity, costRequest)
	if !ok {
		return
	}
	if !h.factorsPresented(w, req.Password) {
		return
	}
	err := h.svc.DeleteRepository(r.Context(), substrate.LoginInput{
		Identity: identity, Repository: h.repositoryKeyOf(r, req.Repository), Password: req.Password, TOTPCode: req.TOTPCode,
	})
	if err != nil {
		writeAuthFailure(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// postRenameRepository gives the repository a new name (decision record
// 0287). Like the delete, the person's identity and both current factors
// travel in the body, so a leaked token cannot do it, and only an admin of
// the repository renames it. The answer names the repository's id, its new
// name and every former name it still answers to, until when.
func (h *handler) postRenameRepository(w http.ResponseWriter, r *http.Request) {
	var req substrate.RenameRepositoryRequest
	if !readJSON(w, r, &req, jsonBody) {
		return
	}
	identity := h.identityOf(r, req.Identity)
	if !namesAPerson(w, identity, "a rename") {
		return
	}
	ok := h.authGate(w, r, identity, costRequest)
	if !ok {
		return
	}
	if !h.factorsPresented(w, req.Password) {
		return
	}
	name, named := h.newRepositoryName(w, req.Name)
	if !named {
		return
	}
	names, err := h.svc.RenameRepository(r.Context(), substrate.LoginInput{
		Identity: identity, Repository: h.repositoryKeyOf(r, req.Repository), Password: req.Password, TOTPCode: req.TOTPCode,
	}, name)
	if err != nil {
		writeAuthFailure(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, names)
}

// postTOTPBegin verifies the current factors and issues a candidate seed,
// writing nothing — an abandoned re-enrollment cannot lock anyone out.
func (h *handler) postTOTPBegin(w http.ResponseWriter, r *http.Request) {
	var req totpBeginRequest
	if !readJSON(w, r, &req, jsonBody) {
		return
	}
	identity := h.identityOf(r, req.Identity)
	if !namesAPerson(w, identity, "a second factor's enrollment") {
		return
	}
	ok := h.authGate(w, r, identity, costRequest)
	if !ok {
		return
	}
	if !h.factorsPresented(w, req.Password) {
		return
	}
	enrollment, err := h.svc.BeginTOTPReenrollment(r.Context(), substrate.LoginInput{
		Identity: identity, Password: req.Password, TOTPCode: req.TOTPCode,
	})
	if err != nil {
		writeAuthFailure(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, enrollment)
}

// postTOTP swaps the second factor, proving BOTH the current factors and one
// code from the new seed.
func (h *handler) postTOTP(w http.ResponseWriter, r *http.Request) {
	var req totpRequest
	if !readJSON(w, r, &req, jsonBody) {
		return
	}
	identity := h.identityOf(r, req.Identity)
	if !namesAPerson(w, identity, "a second factor's enrollment") {
		return
	}
	ok := h.authGate(w, r, identity, costRequest)
	if !ok {
		return
	}
	if !h.factorsPresented(w, req.Password) {
		return
	}
	err := h.svc.ReenrollTOTP(r.Context(), substrate.LoginInput{
		Identity: identity, Password: req.Password, TOTPCode: req.TOTPCode,
	}, req.NewTOTPSecret, req.NewTOTPCode)
	if err != nil {
		writeAuthFailure(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.SessionCredential{Identity: identity})
}

// factorsPresented enforces the password-factor rule at the door (ruling
// RB-6). A request that brings a bearer token and no password is not an
// authentication failure — it is a REFUSAL of the whole idea: this endpoint
// does not accept tokens, and saying so with 403 is what tells a caller their
// token will never be enough.
//
// Only the password is demanded here. Whether a code is owed is the
// account's, not the door's: an account registered without a second factor
// owes none, and one that has a factor and sent no code is answered
// totp_required by the engine once the password verifies. The rule is about
// what a bearer token buys, and that does not change with the factor.
func (h *handler) factorsPresented(w http.ResponseWriter, password string) bool {
	if password != "" {
		return true
	}
	writeError(w, http.StatusForbidden, codeForbidden,
		"changing auth material requires the current password, and the TOTP code where the account has one, in the request body — a bearer token is not accepted")
	return false
}

// --- tokens ---

// postMintToken mints a script or device token in the caller's own
// repository. It is the authenticated twin of login: the same record, the
// same secret-shown-once, a different door.
func (h *handler) postMintToken(w http.ResponseWriter, r *http.Request) {
	var req mintRequest
	if !readJSON(w, r, &req, jsonBody) {
		return
	}
	if req.Label == "" {
		req.Label = "token"
	}
	if req.Scopes != nil && !scope.IsMCP(req.Scopes) {
		writeError(w, http.StatusUnprocessableEntity, codeValidation,
			`a minted token is full access, or scopes ["mcp"] for an assistant: an app's scopes come from the authorization flow`)
		return
	}
	ctx := r.Context()
	info, secret, err := DatasetFrom(ctx).MintToken(ctx, substrate.MintTokenInput{Label: req.Label, ExpiresAt: req.ExpiresAt, Scopes: req.Scopes})
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, substrate.MintedToken{Token: info, Secret: secret})
}

// getIdentity answers the person the request's token acts for: their sign-in
// name, whether they have a second factor and the repositories they may
// open. An app's scoped token is refused, since it reaches what its scopes
// name and a person's sign-in is none of them; a token no person stands
// behind finds none.
func (h *handler) getIdentity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, scoped := substrate.ScopesFrom(ctx); scoped {
		writeError(w, http.StatusForbidden, codeForbidden, "an app's token does not read the person's sign-in")
		return
	}
	id := substrate.IdentityFrom(ctx)
	if id == "" {
		writeError(w, http.StatusNotFound, codeNotFound, "this token acts for no person")
		return
	}
	info, err := h.svc.Identity(ctx, id)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (h *handler) getTokens(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tokens, err := DatasetFrom(ctx).Tokens(ctx)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.Listed(tokens))
}

// deleteToken revokes: it deletes the token record, which is the same write
// the ordinary record surface performs at
// DELETE /api/v1/substrate.reamde.dev/core/token/{id}. No row means no access — there
// is no revocation list and nothing to expire.
func (h *handler) deleteToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ent, err := DatasetFrom(ctx).Delete(ctx, ActorFrom(ctx), tokenType, pathParam(r, "id"), substrate.DeleteInput{})
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ent)
}

// tokenType is the token kind's reference — the one place the HTTP layer
// names it, for the revoke path.
const tokenType = corePackage + "/token"
