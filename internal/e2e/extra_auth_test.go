package e2e

// The door, the tokens, the rate window and the isolation between two
// repositories: CASES.md rows AUTH-02, AUTH-04, AUTH-09, AUTH-10, TOK-02,
// TOK-03, TOK-04, RL-01, ISO-01 and ISO-02, orders 100-199.
//
// These run after the stories, over the repository they left. Everything here
// either refuses or reads, with two exceptions that add and leave: the tokens
// TOK-02 and TOK-03 mint (both end revoked or expired), and the second user
// ISO-01 registers. The story graph is never touched.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// xaCoreKindPrefix is what every kind a FRESH repository is seeded with
// starts with: registration installs core and nothing else, which is what
// makes "core only" an assertable statement about a second user's changelog.
const xaCoreKindPrefix = "core.substrate.reamde.dev/"

// xaTokenRecord is the token kind's record route: tokens are records, so the
// ordinary record surface revokes one exactly as `DELETE /tokens/{id}` does.
const xaTokenRecord = "/api/v1/core.substrate.reamde.dev/token"

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
	registerCase(110, "AUTH-04", "Username and password are validated before anything is written",
		"A username outside `[a-z][a-z0-9]{1,29}` and a password under the minimum length are refused "+
			"with a 422 `validation` naming the rule that refused them.",
		xaCaseCredentialValidation)
	registerCase(120, "AUTH-09", "A login failure is one indistinguishable answer",
		"An unknown username and a wrong password answer the same status and the byte-identical body, "+
			"so the door is no oracle for which usernames exist.",
		xaCaseLoginOracle)
	registerCase(130, "AUTH-10", "A refused registration writes nothing",
		"A duplicate registration is refused 422 and the existing user's changelog gains no row: the "+
			"refusal happens before any write, so a taken username cannot be used to touch its repository.",
		xaCaseRefusedRegistrationWrites)
	registerCase(140, "TOK-02", "An expiry is server-enforced",
		"A token minted with `expiresAt` a few seconds out authenticates until that instant and answers "+
			"401 after it, without anything revoking it.",
		xaCaseTokenExpiry)
	registerCase(150, "TOK-03", "Tokens are records, and deleting the record revokes",
		"`DELETE /api/v1/core.substrate.reamde.dev/token/{id}` tombstones the token record and the secret "+
			"stops authenticating, the same revocation `DELETE /tokens/{id}` performs.",
		xaCaseTokenRecordRevoke)
	registerCase(160, "TOK-04", "The bearer and the actor header are both checked",
		"A garbage bearer and a missing Authorization header are each a 401 `auth`; an ordinary actor is "+
			"accepted; the substrate's own writing hands are refused 403 `forbidden` on `X-Substrate-Actor`.",
		xaCaseBearerAndActor)
	registerCase(170, "RL-01", "The rate window is real and it reopens",
		"A second login attempt inside the window is a 429 carrying `Retry-After`; waiting that many "+
			"seconds out, the next attempt is admitted and mints a token.",
		xaCaseRateWindow)
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

// xaSend is the harness's do with the two things it does not carry: extra
// REQUEST headers, and the RESPONSE headers back. TOK-04 has to set
// `X-Substrate-Actor` and RL-01 has to read `Retry-After`.
func xaSend(c *C, token, method, path string, body any, header map[string]string) (int, http.Header, []byte) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		c.requiref(err == nil, "encoding the %s %s body: %v", method, path, err)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.r.base+path, rd)
	c.requiref(err == nil, "building %s %s: %v", method, path, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := c.r.hc.Do(req)
	c.requiref(err == nil, "%s %s: %v", method, path, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	c.requiref(err == nil, "reading %s %s: %v", method, path, err)
	c.stepf("`%s %s` answered %d", method, path, resp.StatusCode)
	return resp.StatusCode, resp.Header, raw
}

// xaChangesForward reads one repository's whole forward feed under an
// EXPLICIT bearer. The harness's reader always carries the run's token, and
// ISO-01 has to read the other user's changelog.
func xaChangesForward(c *C, token string) []changeRow {
	c.t.Helper()
	var rows []changeRow
	from := int64(0)
	for {
		path := fmt.Sprintf("/api/v1/changes?from=%d", from)
		status, raw := c.doAs(token, http.MethodGet, path, nil, nil)
		c.requiref(status == http.StatusOK, "GET %s answered %d: %s", path, status, raw)
		page := 0
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if line == "" {
				continue
			}
			var row changeRow
			c.requiref(json.Unmarshal([]byte(line), &row) == nil, "undecodable ndjson line: %s", line)
			if row.Seq == 0 {
				// A control frame: the bookmark or a heartbeat is fine, the
				// reserved terminal error frame is a failure, never a skip.
				c.requiref(!strings.Contains(line, `"error"`), "the feed ended with an error frame: %s", line)
				continue
			}
			rows = append(rows, row)
			page++
			from = row.Seq
		}
		if page == 0 {
			return rows
		}
	}
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
		map[string]any{"inviteCode": wrongInvite, "username": name}, nil)
	c.requiref(status == http.StatusUnauthorized, "`/register/enroll` with a wrong invite code answered %d, want 401%s", status, redacted(status, raw))
	enrollErr := xaErrorOf(c, raw)
	c.requiref(enrollErr.Error.Code == "auth", "the enroll refusal's code is %q, want `auth`", enrollErr.Error.Code)
	c.requiref(enrollErr.Error.Message == "invalid invite code",
		"the enroll refusal says %q, want `invalid invite code`", enrollErr.Error.Message)

	c.paceAuth()
	status, raw = c.doAs("", http.MethodPost, "/register",
		map[string]any{"inviteCode": wrongInvite, "username": name, "password": c.r.password}, nil)
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

// --- AUTH-04 ---------------------------------------------------------------

// xaCaseCredentialValidation walks the two validation rules registration
// enforces on its own inputs, each with the invite code CORRECT so the 422
// cannot be confused with the 401 AUTH-02 pins.
func xaCaseCredentialValidation(c *C) {
	r := c.r

	// Both spellings are outside `[a-z][a-z0-9]{1,29}`: one leads with a
	// digit, the other carries an uppercase letter.
	for _, name := range []string{"9bad", "Bad"} {
		c.paceAuth()
		status, raw := c.doAs("", http.MethodPost, "/register",
			map[string]any{"inviteCode": r.invite, "username": name, "password": r.password}, nil)
		c.requiref(status == http.StatusUnprocessableEntity,
			"registering the username %q answered %d, want 422%s", name, status, redacted(status, raw))
		e := xaErrorOf(c, raw)
		c.requiref(e.Error.Code == "validation", "the refusal of %q has code %q, want `validation`", name, e.Error.Code)
		c.requiref(strings.Contains(e.Error.Message, `must match [a-z][a-z0-9]{1,29}`),
			"the refusal of %q does not name the grammar: %q", name, e.Error.Message)
		c.requiref(strings.Contains(e.Error.Message, name),
			"the refusal of %q does not name the username it refused: %q", name, e.Error.Message)
	}
	c.stepf("the usernames `9bad` and `Bad` were both refused 422 `validation` naming `[a-z][a-z0-9]{1,29}`")

	// A well-formed username with a password under the floor: the refusal
	// names the length rule and the number.
	name := xaName("xapw")
	c.paceAuth()
	status, raw := c.doAs("", http.MethodPost, "/register",
		map[string]any{"inviteCode": r.invite, "username": name, "password": "short"}, nil)
	c.requiref(status == http.StatusUnprocessableEntity,
		"registering with a five-character password answered %d, want 422: %s", status, raw)
	e := xaErrorOf(c, raw)
	c.requiref(e.Error.Code == "validation", "the short-password refusal has code %q, want `validation`", e.Error.Code)
	c.requiref(strings.Contains(e.Error.Message, "the password must be at least 12 characters"),
		"the short-password refusal does not name the length rule: %q", e.Error.Message)
	c.stepf("a five-character password was refused 422 `validation`: %q", e.Error.Message)
}

// --- AUTH-09 ---------------------------------------------------------------

// xaCaseLoginOracle asserts the two failures are the same ANSWER, not merely
// the same status: a body that differed in a word would still name which
// usernames exist.
func xaCaseLoginOracle(c *C) {
	r := c.r

	ghost := xaName("xaghost")
	c.paceAuth()
	unknownStatus, unknownRaw := c.doAs("", http.MethodPost, "/login",
		map[string]any{"username": ghost, "password": r.password}, nil)
	c.requiref(unknownStatus == http.StatusUnauthorized,
		"logging in as the unregistered `%s` answered %d, want 401%s", ghost, unknownStatus, redacted(unknownStatus, unknownRaw))

	c.paceAuth()
	wrongStatus, wrongRaw := c.doAs("", http.MethodPost, "/login",
		map[string]any{"username": r.username, "password": "definitely-not-the-password"}, nil)
	c.requiref(wrongStatus == http.StatusUnauthorized,
		"a wrong password for `%s` answered %d, want 401%s", r.username, wrongStatus, redacted(wrongStatus, wrongRaw))

	c.requiref(unknownStatus == wrongStatus,
		"an unknown user answered %d and a wrong password answered %d", unknownStatus, wrongStatus)
	c.requiref(string(unknownRaw) == string(wrongRaw),
		"the two failures differ:\nunknown user:   %s\nwrong password: %s", unknownRaw, wrongRaw)
	e := xaErrorOf(c, unknownRaw)
	c.requiref(e.Error.Code == "auth", "the login failure's code is %q, want `auth`", e.Error.Code)
	c.stepf("an unknown username and a wrong password answered the identical 401 body: `%s`, %q",
		e.Error.Code, e.Error.Message)
}

// --- AUTH-10 ---------------------------------------------------------------

// xaCaseRefusedRegistrationWrites proves the refusal of a taken username
// costs the existing repository nothing. The changelog is the truth, so the
// assertion is on the changelog: no row landed, of any kind, by any actor.
func xaCaseRefusedRegistrationWrites(c *C) {
	r := c.r

	before := c.readChangesForward(0)
	c.requiref(len(before) > 0, "the changelog is empty; the stories should have filled it")
	head := before[len(before)-1].Seq

	c.paceAuth()
	status, raw := c.doAs("", http.MethodPost, "/register",
		map[string]any{"inviteCode": r.invite, "username": r.username, "password": "a-completely-different-password"}, nil)
	c.requiref(status == http.StatusUnprocessableEntity,
		"re-registering `%s` answered %d, want 422%s", r.username, status, redacted(status, raw))
	e := xaErrorOf(c, raw)
	c.requiref(e.Error.Code == "validation", "the duplicate refusal's code is %q, want `validation`", e.Error.Code)
	c.requiref(strings.Contains(e.Error.Message, "already exists"),
		"the duplicate refusal does not say the user exists: %q", e.Error.Message)

	after := xaChangesForward(c, r.token)
	c.requiref(len(after) > 0 && after[len(after)-1].Seq >= head, "the changelog shrank: head was %d", head)
	var landed []string
	for _, row := range after {
		if row.Seq > head {
			landed = append(landed, fmt.Sprintf("seq %d %s %s `%s` by %s", row.Seq, row.Op, row.Kind, row.RecordID, row.Actor))
		}
	}
	c.requiref(len(landed) == 0,
		"the refused registration was followed by %d new changelog rows: %s", len(landed), strings.Join(landed, "; "))
	c.stepf("the refused duplicate left the changelog at seq %d: a taken username is refused before any write", head)
}

// --- TOK-02 ----------------------------------------------------------------

// xaCaseTokenExpiry mints a token that dies on its own. The expiry is a few
// seconds out on purpose: the case has to OUTLIVE it, and a longer one would
// only make the suite slower.
func xaCaseTokenExpiry(c *C) {
	// RFC3339 carries no sub-second part, so the instant the server stores is
	// this truncated one; the wait below is measured against the same value.
	// The suite and the dev server share one host, so one clock; the margins
	// below absorb truncation, not skew.
	expiresAt := time.Now().Add(4 * time.Second).UTC().Truncate(time.Second)
	var minted struct {
		Token struct {
			ID        string `json:"id"`
			ExpiresAt string `json:"expiresAt"`
		} `json:"token"`
		Secret string `json:"secret"`
	}
	status, raw := c.do(http.MethodPost, "/tokens",
		map[string]any{"label": "xa-expiring", "expiresAt": expiresAt.Format(time.RFC3339)}, &minted)
	c.requiref(status == http.StatusCreated, "minting an expiring token answered %d, want 201%s", status, redacted(status, raw))
	c.requiref(minted.Token.ExpiresAt != "", "the minted token carries no expiresAt")
	c.requiref(minted.Secret != "", "the mint returned no secret")

	status, raw = c.doAs(minted.Secret, http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusOK, "the expiring token answered %d before its expiry, want 200: %s", status, raw)
	c.stepf("minted token `%s` expiring at %s; it authenticates while it is alive", minted.Token.ID, minted.Token.ExpiresAt)

	// A margin past the stored instant: the server compares against its own
	// clock, so the wait has to clear the expiry rather than merely reach it.
	if wait := time.Until(expiresAt) + 1500*time.Millisecond; wait > 0 {
		c.stepf("waited %s for the expiry to pass", wait.Round(100*time.Millisecond))
		time.Sleep(wait)
	}

	status, raw = c.doAs(minted.Secret, http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusUnauthorized, "the expired token answered %d, want 401: %s", status, raw)
	e := xaErrorOf(c, raw)
	c.requiref(e.Error.Code == "auth", "the expired token's refusal has code %q, want `auth`", e.Error.Code)
	status, _ = c.do(http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusOK, "the run's own token stopped working when an unrelated token expired")
	c.stepf("past %s the same secret is a 401 `auth`; nothing revoked it and the run's token is untouched", minted.Token.ExpiresAt)
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

// --- TOK-04 ----------------------------------------------------------------

// xaCaseBearerAndActor covers the two ways a request arrives with no usable
// credential, and the one thing a request may never say about itself.
func xaCaseBearerAndActor(c *C) {
	status, raw := c.doAs("nonsense", http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusUnauthorized, "a garbage bearer answered %d, want 401: %s", status, raw)
	garbage := xaErrorOf(c, raw)
	c.requiref(garbage.Error.Code == "auth", "the garbage bearer's refusal has code %q, want `auth`", garbage.Error.Code)
	c.requiref(garbage.Error.Message == "invalid token",
		"the garbage bearer's refusal says %q, want `invalid token`", garbage.Error.Message)

	status, raw = c.doAs("", http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusUnauthorized, "a missing Authorization header answered %d, want 401: %s", status, raw)
	missing := xaErrorOf(c, raw)
	c.requiref(missing.Error.Code == "auth", "the missing header's refusal has code %q, want `auth`", missing.Error.Code)
	c.requiref(missing.Error.Message == "missing bearer token",
		"the missing header's refusal says %q, want `missing bearer token`", missing.Error.Message)
	c.stepf("a garbage bearer is 401 `auth` %q and no header at all is 401 `auth` %q", "invalid token", "missing bearer token")

	// An ordinary door name is attribution the caller declares, and it is
	// accepted: the header is not refused wholesale, only the reserved names.
	status, _, raw = xaSend(c, c.r.token, http.MethodGet, "/tokens", nil,
		map[string]string{"X-Substrate-Actor": "console"})
	c.requiref(status == http.StatusOK, "a request claiming the actor `console` answered %d, want 200: %s", status, raw)

	// The substrate's own writing hands: a request that could claim one could
	// forge a credential ref or a shipped declaration.
	for _, actor := range []string{"substrate", "bundle:tasks.substrate.reamde.dev"} {
		status, _, raw = xaSend(c, c.r.token, http.MethodGet, "/tokens", nil,
			map[string]string{"X-Substrate-Actor": actor})
		c.requiref(status == http.StatusForbidden,
			"a valid token claiming the actor %q answered %d, want 403: %s", actor, status, raw)
		e := xaErrorOf(c, raw)
		c.requiref(e.Error.Code == "forbidden", "claiming %q was refused with code %q, want `forbidden`", actor, e.Error.Code)
		c.requiref(strings.Contains(e.Error.Message, "is reserved"),
			"the refusal of %q does not say the actor is reserved: %q", actor, e.Error.Message)
	}
	c.stepf("`X-Substrate-Actor: console` is accepted; `substrate` and `bundle:tasks.substrate.reamde.dev` are 403 `forbidden`, reserved for the substrate's own hands")
}

// --- RL-01 -----------------------------------------------------------------

// xaCaseRateWindow is the one case that deliberately does NOT pace itself: it
// pays the window's price to prove the window exists, then waits out exactly
// what the server asked for and proves it reopened.
func xaCaseRateWindow(c *C) {
	r := c.r
	// The first two attempts carry a wrong password on purpose: neither may
	// succeed, and neither spends a TOTP code on a door that enforces one.
	wrong := map[string]any{"username": r.username, "password": "definitely-not-the-password"}

	c.paceAuth()
	status, _, raw := xaSend(c, "", http.MethodPost, "/login", wrong, nil)
	c.requiref(status == http.StatusUnauthorized,
		"the first attempt answered %d, want the ordinary 401%s", status, redacted(status, raw))

	// No pacing here: this is the case's subject.
	status, header, raw := xaSend(c, "", http.MethodPost, "/login", wrong, nil)
	c.requiref(status == http.StatusTooManyRequests,
		"a second attempt inside the window answered %d, want 429%s", status, redacted(status, raw))
	e := xaErrorOf(c, raw)
	c.requiref(e.Error.Code == "rate_limited", "the 429's code is %q, want `rate_limited`", e.Error.Code)
	retryAfter := header.Get("Retry-After")
	c.requiref(retryAfter != "", "the 429 carries no Retry-After header")
	seconds, err := strconv.Atoi(retryAfter)
	c.requiref(err == nil && seconds >= 1, "Retry-After is %q, want a whole number of seconds", retryAfter)
	c.stepf("a second login inside the window answered 429 `rate_limited` with `Retry-After: %s`", retryAfter)

	// Wait exactly what the server asked, plus a margin for the clock skew
	// between this process and the server's.
	wait := time.Duration(seconds)*time.Second + 500*time.Millisecond
	time.Sleep(wait)
	c.stepf("waited the %s the server asked for", wait.Round(100*time.Millisecond))

	// The window reopened, so the correct credentials get through and mint.
	login := map[string]any{"username": r.username, "password": r.password, "label": "e2e-rate-window"}
	if r.totpSecret != "" {
		login["totpCode"] = r.nextTOTPCode(c)
	}
	var out struct {
		Token struct {
			ID string `json:"id"`
		} `json:"token"`
		Secret string `json:"secret"`
	}
	status, _, raw = xaSend(c, "", http.MethodPost, "/login", login, nil)
	c.requiref(status == http.StatusCreated,
		"the attempt after Retry-After answered %d, want 201%s", status, redacted(status, raw))
	c.requiref(json.Unmarshal(raw, &out) == nil, "the login answer is undecodable")
	c.requiref(out.Secret != "", "the login after the window returned no secret")
	c.stepf("after the wait the door admitted the attempt and minted token `%s`", out.Token.ID)

	// The run's pacing clock has to learn about the attempts made here, or
	// the next case's paceAuth would measure from before them.
	r.lastAuth = time.Now()
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
		map[string]any{"inviteCode": r.invite, "username": second, "password": r.password}, &reg)
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
		tasksBundleID, c.r.username, xaSecond.username)
}
