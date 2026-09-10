package e2e

// The door, the tokens and the isolation between two repositories: cases
// AUTH-02, TOK-03, ISO-01 and ISO-02, orders 100-199.
//
// These run after the stories, over the repository they left. Everything here
// either refuses or reads, with two exceptions that add and leave: TOK-03
// mints a token (it ends revoked) and ISO-01 registers a second user. The
// story graph is never touched.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// xaCoreKindPrefix is what every kind a FRESH repository is seeded with
// starts with: registration installs core and nothing else, which is what
// makes "core only" an assertable statement about a second user's changelog.
const xaCoreKindPrefix = "substrate.reamde.dev/core/"

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

func init() {
	registerCase(100, "AUTH-02", "A wrong invite code is refused at the door",
		"Both halves of the registration gesture refuse an invite code that is not the configured one, "+
			"with a 401 `auth` that names the code and nothing about the username.",
		xaCaseInviteCode)
	registerCase(150, "TOK-03", "Tokens are records, and deleting the record revokes",
		"`DELETE /api/v1/substrate.reamde.dev/core/token/{id}` tombstones the token record and the secret "+
			"stops authenticating, the same revocation `DELETE /tokens/{id}` performs.",
		xaCaseTokenRecordRevoke)
	registerCase(180, "ISO-01", "A second user sees none of the first user's repository",
		"A freshly registered second user's token finds the first user's collections absent, its record "+
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

// xaTOTPRequired is what the deployment says about its own door. Every case
// that registers or logs in reads it rather than assuming the dev door.
func xaTOTPRequired(c *C) bool {
	c.t.Helper()
	var disc struct {
		Registration struct {
			Open         bool `json:"open"`
			TOTPRequired bool `json:"totpRequired"`
		} `json:"registration"`
	}
	status, raw := c.doAs("", http.MethodGet, "/.well-known/substrate/server.json", nil, &disc)
	c.requiref(status == http.StatusOK, "discovery answered %d: %s", status, raw)
	return disc.Registration.TOTPRequired
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

	// The other half of the row is a DEPLOYMENT, not a request: `inviteOK`
	// answers 501 `unsupported` when no invite code is configured at all, and
	// this server has one (it admitted the run's own registration). Reaching
	// the 501 takes a second server started without a code.
	c.stepf("SKIPPED the closed-door half: a substrate with no invite code configured answers 501 `unsupported`, and this one is open")
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
// repository through it, in the three places it could leak: the collections,
// one known record, and the changelog.
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

	// The first user's vocabulary is not the second user's: the collections
	// do not exist there at all, which is a 404 naming the collection.
	for _, path := range []string{tasksCollection, personCollection} {
		status, raw = c.doAs(xaSecond.token, http.MethodGet, path, nil, nil)
		c.requiref(status == http.StatusNotFound,
			"the second user's GET %s answered %d, want 404: %s", path, status, raw)
		e := xaErrorOf(c, raw)
		c.requiref(e.Error.Code == "not_found", "the second user's GET %s was refused with code %q, want `not_found`", path, e.Error.Code)
	}
	status, raw = c.doAs(xaSecond.token, http.MethodGet, personCollection+"/nour", nil, nil)
	c.requiref(status == http.StatusNotFound,
		"the second user's GET of the first user's person `nour` answered %d, want 404: %s", status, raw)
	c.stepf("through the second user's token the task and person collections are 404, and so is the first user's `nour`")

	// The changelog is the truth, so isolation has to hold there too: the
	// second repository's feed is its own registration and nothing else.
	mine := map[string]bool{}
	for _, row := range c.readChangesForward(0) {
		if !strings.HasPrefix(row.Kind, xaCoreKindPrefix) {
			mine[row.Kind+"/"+row.RecordID] = true
		}
	}
	c.requiref(len(mine) > 0, "the first user's changelog holds no record outside core, so this case would prove nothing")
	theirs := xaChangesForward(c, xaSecond.token)
	c.requiref(len(theirs) > 0, "the second user's changelog is empty; registration writes its own rows")
	for _, row := range theirs {
		c.requiref(!mine[row.Kind+"/"+row.RecordID],
			"the second user's changelog carries the first user's %s `%s` at seq %d", row.Kind, row.RecordID, row.Seq)
		c.requiref(row.Kind != taskKind && row.Kind != personKind,
			"the second user's changelog carries a %s row at seq %d; registration seeds core alone", row.Kind, row.Seq)
		c.requiref(strings.HasPrefix(row.Kind, xaCoreKindPrefix),
			"the second user's changelog carries a %s row at seq %d, outside the core seed", row.Kind, row.Seq)
	}
	c.stepf("the second user's changelog holds %d rows, all of core kinds, none of the first user's %d non-core records",
		len(theirs), len(mine))

	// And the first user's repository is exactly where it was.
	status, raw = c.do(http.MethodGet, tasksCollection, nil, nil)
	c.requiref(status == http.StatusOK, "the first user's task collection answered %d, want 200: %s", status, raw)
	nour := c.getRec(personCollection, "nour")
	c.requiref(nour.prop("name") == "Nour Haddad", "the first user's `nour` reads back %q", nour.prop("name"))
	c.stepf("the first user's token still answers: the task collection is 200 and `nour` reads back unchanged")
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
