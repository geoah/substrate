package e2e

// The door, the credential changes, the tokens and the isolation between two
// repositories: AUTH-02, AUTH-05, AUTH-06, AUTH-07, TOK-03, ISO-01 and
// ISO-02, orders 100-199.
//
// These run after the stories, over the repository they left. Everything here
// either refuses or reads, with four exceptions that add and leave: AUTH-05
// registers a throwaway user whose seed AUTH-07 swaps, AUTH-06 changes the
// run's own password (the report prints the new one), TOK-03 mints a token
// (it ends revoked) and ISO-01 registers a second user. The story graph is
// never touched.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/engine"
)

// xaCoreKindPrefix is what every kind a FRESH repository is seeded with
// starts with: registration installs core and nothing else, which is what
// makes "core only" an assertable statement about a second user's changelog.
const xaCoreKindPrefix = "substrate.reamde.dev/core/"

// xaSeeded reports whether a kind is one registration seeds into every
// repository: core's and the llm package's (decision record 0077). Their
// rows carry the same ids in every repository, so they are not evidence
// that one repository's changelog reached another.
func xaSeeded(kind string) bool {
	return strings.HasPrefix(kind, xaCoreKindPrefix) || strings.HasPrefix(kind, "substrate.reamde.dev/llm/")
}

// xaTokenRecord is the token kind's record route: tokens are records, so the
// ordinary record surface revokes one exactly as `DELETE /tokens/{id}` does.
const xaTokenRecord = "/api/v1/substrate.reamde.dev/core/token"

// xaSecond is the throwaway second user ISO-01 registers and ISO-02 reads
// again. Registration is one-shot per user and rate-limited, so the pair
// shares one, and ISO-02 skips rather than registering its own when ISO-01
// never got there.
var xaSecond struct {
	username string
	token    string
}

// xaTOTPUser is the throwaway user AUTH-05 registers through the enforced
// door and AUTH-07 swaps the seed of, so the run's own seed never moves. The
// last step each seed consumed rides along: the door refuses a step it has
// already seen.
var xaTOTPUser struct {
	repository string
	secret     string
	lastStep   int64
}

func init() {
	registerCase(100, "AUTH-02", "A wrong invite code is refused at the door",
		"Both halves of the registration gesture refuse an invite code that is not the configured one, "+
			"with a 401 `auth` that names the code and nothing about the username.",
		xaCaseInviteCode)
	registerCase(105, "AUTH-05", "Registration proves a live code, and that code is spent",
		"Against the enforced door a registration enrolls a seed and commits with one live code from it: a code "+
			"from another seed is refused 401, the right one registers, the same code cannot also log in, and "+
			"the next step's code does.",
		xaCaseTOTPRegistration)
	registerCase(108, "AUTH-07", "Swapping the second factor retires the old seed",
		"`/totp/enroll` issues a candidate seed only against both current factors and refuses a bearer 403; "+
			"`/totp` refuses a candidate code that does not match, then swaps with a proven one, after which a code "+
			"from the old seed is refused and a code from the new one logs in.",
		xaCaseTOTPSwap)
	registerCase(120, "AUTH-06", "A password change takes both factors and retires the old password",
		"`/password` refuses a bearer token 403, changes the password when the body carries the current "+
			"factors, and afterwards the old password is a 401 while the new one logs in; the run's token "+
			"survives the change.",
		xaCasePasswordChange)
	registerCase(150, "TOK-03", "Tokens are records, and deleting the record revokes",
		"`DELETE /api/v1/substrate.reamde.dev/core/token/{id}` tombstones the token record and the secret "+
			"stops authenticating, the same revocation `DELETE /tokens/{id}` performs.",
		xaCaseTokenRecordRevoke)
	registerCase(180, "ISO-01", "A second user sees none of the first user's repository",
		"A freshly registered second user's token finds the first user's kinds unknown, its record "+
			"404, and its own changelog holding core rows alone, while the first user's token still answers.",
		xaCaseIsolation)
	registerCase(190, "ISO-02", "An install belongs to one repository",
		"The tasks bundle reads installed=true in the first user's catalog and installed=false in the "+
			"second user's: one shipped closure, two independent installs.",
		xaCaseCatalogIsolation)
}

// --- the helpers these cases need beside the harness's ---------------------

// xaError is the wire error envelope, narrowed to what these cases assert on.
// The code is the contract clients switch on, so every refusal here pins the
// code and not only the status.
type xaError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func xaErrorOf(c *C, raw []byte) xaError {
	c.t.Helper()
	var e xaError
	c.requiref(json.Unmarshal(raw, &e) == nil, "undecodable error body: %s", raw)
	c.requiref(e.Error.Code != "", "the refusal carries no error code: %s", raw)
	return e
}

// xaName mints a username inside the [a-z][a-z0-9]{1,29} grammar, distinct
// per call: these cases leave their users behind exactly as the run does, and
// base36 nanoseconds keep two of them apart.
func xaName(prefix string) string {
	return prefix + strconv.FormatInt(time.Now().UnixNano(), 36)
}

// xaChangesForward reads one repository's whole forward feed under an
// EXPLICIT bearer. The harness's reader always carries the run's token, and
// ISO-01 has to read the other user's changelog.
func xaChangesForward(c *C, token string) []changeRow {
	c.t.Helper()
	return c.readChangesForwardAs(token, 0)
}

// xaDoor is what the deployment says about its own door. Every case that
// registers or logs in reads it rather than assuming the dev door.
func xaDoor(c *C) (inviteRequired, totpRequired bool) {
	c.t.Helper()
	var disc struct {
		Registration struct {
			InviteRequired bool `json:"inviteRequired"`
			TOTPRequired   bool `json:"totpRequired"`
		} `json:"registration"`
	}
	status, raw := c.doAs("", http.MethodGet, "/.well-known/substrate/server.json", nil, &disc)
	c.requiref(status == http.StatusOK, "discovery answered %d: %s", status, raw)
	return disc.Registration.InviteRequired, disc.Registration.TOTPRequired
}

func xaTOTPRequired(c *C) bool {
	c.t.Helper()
	_, totp := xaDoor(c)
	return totp
}

// xaBundleInstalled reads one bundle's installed flag from the catalog a
// given token sees. The catalog is per-repository, which is the whole of
// ISO-02.
func xaBundleInstalled(c *C, token, bundleID string) bool {
	c.t.Helper()
	var cat struct {
		Items []struct {
			ID        string `json:"id"`
			Installed bool   `json:"installed"`
		} `json:"items"`
	}
	status, raw := c.doAs(token, http.MethodGet, "/api/v1/catalog", nil, &cat)
	c.requiref(status == http.StatusOK, "the catalog answered %d: %s", status, raw)
	for _, it := range cat.Items {
		if it.ID == bundleID {
			return it.Installed
		}
	}
	c.requiref(false, "the catalog does not list %s", bundleID)
	return false
}

// --- AUTH-02 ---------------------------------------------------------------

// xaCaseInviteCode proves the invite code gates BOTH calls of the
// registration gesture, and that the refusal says only that the code was
// wrong: the username in the body is never confirmed or denied.
func xaCaseInviteCode(c *C) {
	const wrongInvite = "not-the-invite-code"
	name := xaName("xainvite")

	// The gate is a DEPLOYMENT's, not a request's: a server with no
	// SUBSTRATE_INVITE_CODE reads none and would admit the wrong code below
	// as a registration. `mise run test:e2e` starts the dev server with one
	// so this case runs; against a bare `mise run dev` it has nothing to
	// prove.
	if inviteRequired, _ := xaDoor(c); !inviteRequired {
		c.stepf("SKIPPED: discovery reports `registration.inviteRequired: false`, so this server reads no invite code and there is no gate to refuse a wrong one")
		return
	}

	c.paceAuth()
	status, raw := c.doAs("", http.MethodPost, "/register/enroll",
		map[string]any{"inviteCode": wrongInvite, "repository": name}, nil)
	c.requiref(status == http.StatusUnauthorized, "`/register/enroll` with a wrong invite code answered %d, want 401%s", status, redacted(status, raw))
	enrollErr := xaErrorOf(c, raw)
	c.requiref(enrollErr.Error.Code == "auth", "the enroll refusal's code is %q, want `auth`", enrollErr.Error.Code)
	c.requiref(enrollErr.Error.Message == "invalid invite code",
		"the enroll refusal says %q, want `invalid invite code`", enrollErr.Error.Message)

	c.paceAuth()
	status, raw = c.doAs("", http.MethodPost, "/register",
		map[string]any{"inviteCode": wrongInvite, "repository": name, "password": c.r.password}, nil)
	c.requiref(status == http.StatusUnauthorized, "`/register` with a wrong invite code answered %d, want 401%s", status, redacted(status, raw))
	registerErr := xaErrorOf(c, raw)
	c.requiref(registerErr.Error.Code == "auth", "the register refusal's code is %q, want `auth`", registerErr.Error.Code)
	c.requiref(registerErr.Error.Message == "invalid invite code",
		"the register refusal says %q, want `invalid invite code`", registerErr.Error.Message)
	c.stepf("both `/register/enroll` and `/register` refused the code with 401 `auth`, %q, and said nothing about the username `%s`",
		"invalid invite code", name)
}

// --- TOK-03 ----------------------------------------------------------------

// xaCaseTokenRecordRevoke revokes through the RECORD route rather than
// `/tokens/{id}`. Both doors perform the one write, and a client that only
// knows the record surface must be able to end a token with it.
func xaCaseTokenRecordRevoke(c *C) {
	var minted struct {
		Token struct {
			ID string `json:"id"`
		} `json:"token"`
		Secret string `json:"secret"`
	}
	status, raw := c.do(http.MethodPost, "/tokens", map[string]any{"label": "xa-record-revoke"}, &minted)
	c.requiref(status == http.StatusCreated, "minting answered %d, want 201%s", status, redacted(status, raw))
	status, raw = c.doAs(minted.Secret, http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusOK, "the minted token answered %d, want 200: %s", status, raw)

	// The token reads as an ordinary record, secret material redacted.
	var asRecord record
	path := xaTokenRecord + "/" + url.PathEscape(minted.Token.ID)
	status, raw = c.do(http.MethodGet, path, nil, &asRecord)
	c.requiref(status == http.StatusOK, "GET %s answered %d: %s", path, status, raw)
	c.requiref(asRecord.Kind == xaCoreKindPrefix+"token", "the token record's kind is %q", asRecord.Kind)
	c.requiref(asRecord.prop("label") == "xa-record-revoke", "the token record's label is %q", asRecord.prop("label"))

	var tombstone record
	status, raw = c.do(http.MethodDelete, path, nil, &tombstone)
	c.requiref(status == http.StatusOK, "DELETE %s answered %d: %s", path, status, raw)
	c.requiref(tombstone.DeletedAt != "", "the record delete's answer carries no deletedAt: %s", raw)

	status, raw = c.doAs(minted.Secret, http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusUnauthorized, "the revoked token answered %d, want 401: %s", status, raw)
	e := xaErrorOf(c, raw)
	c.requiref(e.Error.Code == "auth", "the revoked token's refusal has code %q, want `auth`", e.Error.Code)
	status, _ = c.do(http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusOK, "the run's own token stopped working after an unrelated revocation")
	c.stepf("deleting the token RECORD `%s` revoked it: the secret is a 401 and the run's token still answers", minted.Token.ID)
}

// --- ISO-01 ----------------------------------------------------------------

// xaCaseIsolation registers a second user and looks for the first one's
// repository through it, in the three places it could leak: a list of its
// kinds, one known record, and the changelog.
func xaCaseIsolation(c *C) {
	r := c.r

	if xaTOTPRequired(c) {
		// A second user would need its own enrolled seed and its own code on
		// every attempt, which is AUTH-05's subject and not this one's.
		c.skipf("this server enforces the second factor, so a second registration needs its own enrolled seed")
		return
	}

	second := xaName("iso")
	c.paceAuth()
	var reg struct {
		Token struct {
			ID string `json:"id"`
		} `json:"token"`
		Secret string `json:"secret"`
	}
	status, raw := c.doAs("", http.MethodPost, "/register",
		map[string]any{"inviteCode": r.invite, "repository": second, "password": r.password}, &reg)
	c.requiref(status == http.StatusCreated, "registering a second user answered %d, want 201%s", status, redacted(status, raw))
	c.requiref(reg.Secret != "", "the second registration returned no token secret")
	xaSecond.username, xaSecond.token = second, reg.Secret
	c.stepf("registered a second user `%s` with its own repository and first token `%s`", second, reg.Token.ID)

	// The first user's vocabulary is not the second user's: the kinds do not
	// exist there at all, so a list narrowed to one is a 404 naming the kind.
	for _, kindPath := range []string{tasksCollection, personCollection} {
		status, raw = c.doAs(xaSecond.token, http.MethodGet, listOf(kindPath), nil, nil)
		c.requiref(status == http.StatusNotFound,
			"the second user's list of %s answered %d, want 404: %s", kindOf(kindPath), status, raw)
		e := xaErrorOf(c, raw)
		c.requiref(e.Error.Code == "not_found" && strings.Contains(e.Error.Message, "unknown kind "+kindOf(kindPath)),
			"the second user's list of %s was refused with %q %q, want `not_found` naming the kind", kindOf(kindPath), e.Error.Code, e.Error.Message)
	}
	status, raw = c.doAs(xaSecond.token, http.MethodGet, personCollection+"/nour", nil, nil)
	c.requiref(status == http.StatusNotFound,
		"the second user's GET of the first user's person `nour` answered %d, want 404: %s", status, raw)
	c.stepf("through the second user's token a list of tasks or people is 404 `unknown kind`, and so is the first user's `nour`")

	// The changelog is the truth, so isolation has to hold there too: the
	// second repository's feed is its own registration and nothing else.
	mine := map[string]bool{}
	for _, row := range c.readChangesForward(0) {
		if !xaSeeded(row.Kind) {
			mine[row.Kind+"/"+row.RecordID] = true
		}
	}
	c.requiref(len(mine) > 0, "the first user's changelog holds no record outside the seeded packages, so this case would prove nothing")
	theirs := xaChangesForward(c, xaSecond.token)
	c.requiref(len(theirs) > 0, "the second user's changelog is empty; registration writes its own rows")
	for _, row := range theirs {
		c.requiref(!mine[row.Kind+"/"+row.RecordID],
			"the second user's changelog carries the first user's %s `%s` at seq %d", row.Kind, row.RecordID, row.Seq)
		c.requiref(row.Kind != taskKind && row.Kind != personKind,
			"the second user's changelog carries a %s row at seq %d; registration seeds core and llm alone", row.Kind, row.Seq)
		c.requiref(xaSeeded(row.Kind),
			"the second user's changelog carries a %s row at seq %d, outside the seed", row.Kind, row.Seq)
	}
	c.stepf("the second user's changelog holds %d rows, all of seeded kinds, none of the first user's %d records outside the seed",
		len(theirs), len(mine))

	// And the first user's repository is exactly where it was.
	status, raw = c.do(http.MethodGet, listOf(tasksCollection), nil, nil)
	c.requiref(status == http.StatusOK, "the first user's task list answered %d, want 200: %s", status, raw)
	nour := c.getRec(personCollection, "nour")
	c.requiref(nour.prop("name") == "Nour Haddad", "the first user's `nour` reads back %q", nour.prop("name"))
	c.stepf("the first user's token still answers: the task list is 200 and `nour` reads back unchanged")
}

// --- ISO-02 ----------------------------------------------------------------

// xaCaseCatalogIsolation reads the same shipped bundle through both tokens.
// The catalog is one server-side list, so its `installed` flag is a statement
// about the READER's repository and nothing else.
func xaCaseCatalogIsolation(c *C) {
	if xaSecond.token == "" {
		c.skipf("ISO-01 registered no second user, so there is no second catalog to read")
		return
	}
	c.requiref(xaBundleInstalled(c, c.r.token, tasksBundleID),
		"the first user's catalog says `%s` is not installed, but REC-01 installed it", tasksBundleID)
	c.requiref(!xaBundleInstalled(c, xaSecond.token, tasksBundleID),
		"the second user's catalog says `%s` is installed; nobody installed it there", tasksBundleID)
	c.stepf("one catalog, two repositories: `%s` is installed=true for `%s` and installed=false for `%s`",
		tasksBundleID, c.r.repository, xaSecond.username)
}

// --- AUTH-05 ---------------------------------------------------------------

// xaEnrollment is what `/register/enroll` and `/totp/enroll` hand back: a
// seed and its otpauth URI, written nowhere until a code from it commits.
type xaEnrollment struct {
	TOTPSecret string `json:"totpSecret"`
	URI        string `json:"otpauthUri"`
}

// xaRequireRefusal holds a credential refusal to its status and code. The
// body is kept in the message only when the answer is not a 2xx, since a 2xx
// from these doors carries a secret.
func xaRequireRefusal(c *C, what string, status int, raw []byte, wantStatus int, wantCode string) xaError {
	c.t.Helper()
	c.requiref(status == wantStatus, "%s answered %d, want %d%s", what, status, wantStatus, redacted(status, raw))
	e := xaErrorOf(c, raw)
	c.requiref(e.Error.Code == wantCode, "%s was refused with code %q, want `%s`: %s", what, e.Error.Code, wantCode, raw)
	return e
}

// xaCaseTOTPRegistration registers a throwaway user through the enforced
// door the way an authenticator app would, and proves the code that
// committed the registration is spent by it.
func xaCaseTOTPRegistration(c *C) {
	r := c.r
	if !xaTOTPRequired(c) {
		c.skipf("discovery reports `registration.totpRequired: false`: this door verifies no code, so there is no live code to prove (the enforced door is `mise run dev:totp`)")
		return
	}
	name := xaName("xatotp")

	var enr xaEnrollment
	c.paceAuth()
	status, raw := c.doAs("", http.MethodPost, "/register/enroll",
		map[string]any{"inviteCode": r.invite, "repository": name}, &enr)
	c.requiref(status == http.StatusOK, "`/register/enroll` answered %d, want 200%s", status, redacted(status, raw))
	c.requiref(enr.TOTPSecret != "" && strings.HasPrefix(enr.URI, "otpauth://totp/"),
		"the enrollment carries no seed or no `otpauth://totp/` URI")
	c.stepf("`/register/enroll` issued a seed and its `otpauth://totp/` URI for `%s`", name)

	// A code from ANOTHER seed: the commit proves THIS enrollment landed in an
	// authenticator, not merely that six digits were sent.
	other, err := engine.NewTOTPSecret()
	c.requiref(err == nil, "minting a second seed: %v", err)
	step := c.totpStepAfter(0)
	reg := map[string]any{
		"inviteCode": r.invite, "repository": name, "password": r.password,
		"totpSecret": enr.TOTPSecret, "totpCode": c.totpCode(other, step),
	}
	c.paceAuth()
	status, raw = c.doAs("", http.MethodPost, "/register", reg, nil)
	xaRequireRefusal(c, "`/register` with a code from another seed", status, raw, http.StatusUnauthorized, "auth")
	c.stepf("`/register` with a live code from a DIFFERENT seed was refused 401 `auth`: the commit proves the enrollment it names")

	reg["totpCode"] = c.totpCode(enr.TOTPSecret, step)
	var out struct {
		Secret     string `json:"secret"`
		Repository string `json:"repository"`
	}
	c.paceAuth()
	status, raw = c.doAs("", http.MethodPost, "/register", reg, &out)
	c.requiref(status == http.StatusCreated, "`/register` with the enrollment's own code answered %d, want 201%s", status, redacted(status, raw))
	c.requiref(strings.HasPrefix(out.Secret, "substrate_tok_") && out.Repository != "", "the registration minted no token or echoed no repository")
	status, _ = c.doAs(out.Secret, http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusOK, "the registration's token answered %d on GET /tokens, want 200", status)
	c.stepf("`/register` with a live code from the enrolled seed created `%s` and its first token authenticates", out.Repository)

	// The step that committed the registration is stored as consumed, so the
	// same code cannot also log in.
	login := map[string]any{"repository": name, "password": r.password, "totpCode": reg["totpCode"], "label": "xa-totp"}
	c.paceAuth()
	status, raw = c.doAs("", http.MethodPost, "/login", login, nil)
	xaRequireRefusal(c, "`/login` with the code that registered", status, raw, http.StatusUnauthorized, "auth")
	c.stepf("`/login` with the very code that registered was refused 401: registration spent that step")

	next := c.totpStepAfter(step)
	login["totpCode"] = c.totpCode(enr.TOTPSecret, next)
	c.paceAuth()
	status, raw = c.doAs("", http.MethodPost, "/login", login, nil)
	c.requiref(status == http.StatusCreated, "`/login` with the next step's code answered %d, want 201%s", status, redacted(status, raw))
	xaTOTPUser.repository, xaTOTPUser.secret, xaTOTPUser.lastStep = name, enr.TOTPSecret, next
	c.stepf("`/login` with the next step's code minted a token: the seed works, only the spent step did not")
}

// --- AUTH-07 ---------------------------------------------------------------

// xaCaseTOTPSwap re-enrolls AUTH-05's user onto a new seed and proves the old
// one is retired by presenting a code the old seed would still have accepted.
func xaCaseTOTPSwap(c *C) {
	r := c.r
	if xaTOTPUser.repository == "" {
		c.skipf("AUTH-05 registered no user through the enforced door, so there is no seed to swap")
		return
	}
	u := &xaTOTPUser

	// The password-factor rule: a bearer token is not evidence for a factor
	// change, whoever's token it is.
	c.paceAuth()
	status, raw := c.doAs(r.token, http.MethodPost, "/totp/enroll", map[string]any{"repository": u.repository}, nil)
	e := xaRequireRefusal(c, "`/totp/enroll` with a bearer and no factors", status, raw, http.StatusForbidden, "forbidden")
	c.requiref(strings.Contains(e.Error.Message, "a bearer token is not accepted"),
		"the 403 does not say a bearer is not accepted: %s", e.Error.Message)
	c.stepf("`/totp/enroll` with a bearer token and no factors was refused 403 `forbidden`: a token never changes a factor")

	enrollStep := c.totpStepAfter(u.lastStep)
	var cand xaEnrollment
	c.paceAuth()
	status, raw = c.doAs("", http.MethodPost, "/totp/enroll", map[string]any{
		"repository": u.repository, "password": r.password, "totpCode": c.totpCode(u.secret, enrollStep),
	}, &cand)
	c.requiref(status == http.StatusOK, "`/totp/enroll` with both factors answered %d, want 200%s", status, redacted(status, raw))
	u.lastStep = enrollStep
	c.requiref(cand.TOTPSecret != "" && cand.TOTPSecret != u.secret, "the candidate seed is empty or the seed already enrolled")
	c.stepf("`/totp/enroll` with both current factors issued a candidate seed; nothing is swapped until a code from it is proven")

	// A candidate "proven" with a code it did not produce is refused before
	// the current factors are spent: the swap cannot land on a seed nobody
	// holds.
	swapStep := c.totpStepAfter(u.lastStep)
	swap := map[string]any{
		"repository": u.repository, "password": r.password,
		"totpCode":      c.totpCode(u.secret, swapStep),
		"newTotpSecret": cand.TOTPSecret,
		"newTotpCode":   c.totpCode(u.secret, c.totpStepAfter(0)),
	}
	c.paceAuth()
	status, raw = c.doAs("", http.MethodPost, "/totp", swap, nil)
	xaRequireRefusal(c, "`/totp` with a candidate code from the old seed", status, raw, http.StatusUnauthorized, "auth")
	c.stepf("`/totp` whose candidate code came from the OLD seed was refused 401: the new seed must be proven")

	proven := c.totpStepAfter(0)
	swap["newTotpCode"] = c.totpCode(cand.TOTPSecret, proven)
	var out struct {
		Repository string `json:"repository"`
	}
	c.paceAuth()
	status, raw = c.doAs("", http.MethodPost, "/totp", swap, &out)
	c.requiref(status == http.StatusOK, "`/totp` with both factors and a proven candidate answered %d, want 200%s", status, redacted(status, raw))
	c.requiref(out.Repository != "", "the swap echoed no repository: %s", raw)
	oldSecret, oldLast := u.secret, swapStep
	u.secret, u.lastStep = cand.TOTPSecret, proven
	c.stepf("`/totp` with both current factors and a live code from the candidate swapped the seed")

	// A code the OLD seed would have accepted (a step after the last one it
	// spent) is refused now, so the refusal is the swap's and not the step's.
	stale := c.totpStepAfter(oldLast)
	login := map[string]any{"repository": u.repository, "password": r.password, "totpCode": c.totpCode(oldSecret, stale), "label": "xa-totp-swap"}
	c.paceAuth()
	status, raw = c.doAs("", http.MethodPost, "/login", login, nil)
	xaRequireRefusal(c, "`/login` with a fresh code from the old seed", status, raw, http.StatusUnauthorized, "auth")
	c.stepf("`/login` with an unspent code from the old seed was refused 401: the old seed is retired")

	fresh := c.totpStepAfter(u.lastStep)
	login["totpCode"] = c.totpCode(u.secret, fresh)
	c.paceAuth()
	status, raw = c.doAs("", http.MethodPost, "/login", login, nil)
	c.requiref(status == http.StatusCreated, "`/login` with a code from the new seed answered %d, want 201%s", status, redacted(status, raw))
	u.lastStep = fresh
	c.stepf("`/login` with a code from the new seed minted a token")
}

// --- AUTH-06 ---------------------------------------------------------------

// xaCasePasswordChange changes the RUN's own password, so the report prints
// the new one and every later login uses it.
func xaCasePasswordChange(c *C) {
	r := c.r
	_, totp := xaDoor(c)
	newPassword := "e2e-changed-" + strconv.FormatInt(time.Now().UnixNano(), 36)

	// The password-factor rule: a bearer with no factors in the body is
	// refused as a whole idea, 403 and not 401.
	c.paceAuth()
	status, raw := c.doAs(r.token, http.MethodPost, "/password",
		map[string]any{"repository": r.repository, "newPassword": newPassword}, nil)
	e := xaRequireRefusal(c, "`/password` with a bearer and no factors", status, raw, http.StatusForbidden, "forbidden")
	c.requiref(strings.Contains(e.Error.Message, "a bearer token is not accepted"),
		"the 403 does not say a bearer is not accepted: %s", e.Error.Message)
	c.stepf("`/password` with the run's bearer token and no factors was refused 403 `forbidden`")

	body := map[string]any{"repository": r.repository, "password": r.password, "newPassword": newPassword}
	if totp {
		body["totpCode"] = r.nextTOTPCode(c)
	}
	var out struct {
		Repository string `json:"repository"`
	}
	c.paceAuth()
	status, raw = c.doAs("", http.MethodPost, "/password", body, &out)
	c.requiref(status == http.StatusOK, "`/password` with the current factors answered %d, want 200%s", status, redacted(status, raw))
	c.requiref(out.Repository == r.authority, "the change echoed repository %q, want %q", out.Repository, r.authority)
	oldPassword := r.password
	r.password, r.rep.Password = newPassword, newPassword
	c.stepf("`/password` with the current factors changed the password of `%s`", r.authority)

	// On the enforced door the old-password attempt carries a live, unspent
	// code, so the password is the only thing wrong with it; a refused login
	// spends nothing, and the same code then carries the new password in.
	login := map[string]any{"repository": r.repository, "password": oldPassword, "label": "xa-password"}
	var step int64
	if totp {
		step = c.totpStepAfter(r.lastStep)
		login["totpCode"] = c.totpCode(r.totpSecret, step)
	}
	c.paceAuth()
	status, raw = c.doAs("", http.MethodPost, "/login", login, nil)
	xaRequireRefusal(c, "`/login` with the old password", status, raw, http.StatusUnauthorized, "auth")
	c.stepf("`/login` with the old password was refused 401")

	login["password"] = newPassword
	var minted struct {
		Secret string `json:"secret"`
	}
	c.paceAuth()
	status, raw = c.doAs("", http.MethodPost, "/login", login, &minted)
	c.requiref(status == http.StatusCreated, "`/login` with the new password answered %d, want 201%s", status, redacted(status, raw))
	if totp {
		r.lastStep = step
	}
	status, _ = c.doAs(minted.Secret, http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusOK, "the new password's token answered %d on GET /tokens, want 200", status)
	status, _ = c.do(http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusOK, "the run's token answered %d after the password change, want 200", status)
	c.stepf("`/login` with the new password minted a token that authenticates, and the run's own token still answers")
}
