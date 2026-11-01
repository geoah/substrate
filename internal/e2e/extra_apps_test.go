package e2e

// An app's authorization and what its approval sets up (decisions 0194 and
// 0202), through the real door and the real engine: internal/api drives the
// same flow against a hand-written fake, so what only this case shows is
// that the catalog's install door, the create-only account write, the
// trigger's arming, the minted token, the webhook door and the blob store
// agree with each other on a live server.
//
// The app is Substrate Sync's request from a local client id, because the
// server never dials loopback for a web app's document. Nothing listens at
// its redirect: the suite reads the code off the approval page's answer,
// where the browser would follow it.
//
// It runs in the 800 block's upper half, after every case but the 900
// block's: what it installs (apple, health and applehealth, and their
// triggers) shows in every list a later case reads, and the 900 block
// restarts the server over a repository it expects quiet, so this case
// waits for its own delivery to settle before it ends.

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

func init() {
	registerCase(860, "APP-01", "An app's approval sets up what it needs, and its token delivers",
		"Substrate Sync's request parks at /oauth/authorize with its authorization details; the approval "+
			"page plans apple and applehealth installs (health brought first), the account and the webhook; "+
			"approving with the plan's hash sets them up through the engine, and the read that sees it "+
			"approved hands over the code once. The exchanged token carries webhook:applehealth-webhook "+
			"and the URL to deliver to; it delivers there in place of the key (a full-access token does "+
			"not), the delivery files a receipt it reads, it reads that trigger's parked list and no other, "+
			"it stores a blob a share points at, its upload of bytes the owner stored first answers its own "+
			"name and leaves the owner's, and after its revocation the door answers 404.",
		xpCaseAppSetUp)
}

const (
	xpApplePkg       = "providers.substrate.reamde.dev/apple"
	xpAppleHealthPkg = "providers.substrate.reamde.dev/applehealth"
	xpHealthPkg      = "substrate.reamde.dev/health"
	xpAccountKind    = xpAppleHealthPkg + "/account"
	xpExportKind     = xpAppleHealthPkg + "/export"
	xpShareKind      = xpApplePkg + "/share"
	xpTriggerKind    = "substrate.reamde.dev/core/trigger"
	xpBlobKind       = "substrate.reamde.dev/core/blob"
	xpWebhook        = "applehealth-webhook"
	xpAdapter        = "applehealth-to-health"
	xpAccountID      = "substrate-sync-e2e"
	xpAccountName    = "E2E iPhone"
	xpClientID       = "http://127.0.0.1/substrate-sync-e2e"
	xpRedirect       = "http://127.0.0.1:53682/callback"
	xpAppState       = "e2e-app-state"
)

// xpScopes is what Substrate Sync asks for in `scope`.
var xpScopes = []string{"write:" + xpApplePkg + "/*", "read:" + xpAccountKind, "read:" + xpExportKind}

// xpDetails is what Substrate Sync asks to have set up.
var xpDetails = []map[string]any{
	{"type": "package", "package": xpApplePkg, "version": 3},
	{"type": "package", "package": xpAppleHealthPkg, "version": 5},
	{
		"type": "account", "kind": xpAccountKind, "id": xpAccountID,
		"properties": map[string]any{"displayName": xpAccountName, "timezone": "Europe/London"},
	},
	{"type": "webhook", "trigger": xpWebhook},
}

// ---- the wire, narrowed to what the case reads -----------------------------

type xpBlock struct {
	Reason  string `json:"reason"`
	Problem string `json:"problem"`
}

type xpStep struct {
	ID      string   `json:"id"`
	Action  string   `json:"action"`
	Blocked *xpBlock `json:"blocked"`
}

type xpPackage struct {
	ID       string   `json:"id"`
	Required int64    `json:"required"`
	Held     int64    `json:"held"`
	Shipped  int64    `json:"shipped"`
	Requires []xpStep `json:"requires"`
}

type xpRow struct {
	Type     string     `json:"type"`
	Action   string     `json:"action"`
	Blocked  *xpBlock   `json:"blocked"`
	Progress string     `json:"progress"`
	Package  *xpPackage `json:"package"`
	Account  *struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	} `json:"account"`
	Webhook *struct {
		Trigger string `json:"trigger"`
		Package string `json:"package"`
	} `json:"webhook"`
}

type xpRequest struct {
	ID  string `json:"id"`
	App struct {
		ClientID string `json:"clientId"`
		Local    bool   `json:"local"`
		Actor    string `json:"actor"`
	} `json:"app"`
	Repository string `json:"repository"`
	Scopes     []struct {
		Scope   string `json:"scope"`
		Allowed bool   `json:"allowed"`
	} `json:"scopes"`
	Setup    []xpRow         `json:"setup"`
	PlanHash string          `json:"planHash"`
	Previous json.RawMessage `json:"previous"`
	State    string          `json:"state"`
	Failure  *struct {
		Row     *int   `json:"row"`
		Message string `json:"message"`
	} `json:"failure"`
	Next string `json:"next"`
}

type xpAnswer struct {
	Redirect string  `json:"redirect"`
	State    string  `json:"state"`
	Setup    []xpRow `json:"setup"`
}

type xpGranted struct {
	Type       string         `json:"type"`
	Package    string         `json:"package"`
	Version    int64          `json:"version"`
	Kind       string         `json:"kind"`
	ID         string         `json:"id"`
	Properties map[string]any `json:"properties"`
	Trigger    string         `json:"trigger"`
	URL        string         `json:"url"`
}

type xpToken struct {
	AccessToken          string      `json:"access_token"`
	TokenType            string      `json:"token_type"`
	Scope                string      `json:"scope"`
	ExpiresIn            *int64      `json:"expires_in"`
	Repository           string      `json:"repository"`
	AuthorizationDetails []xpGranted `json:"authorization_details"`
}

type xpCheck struct {
	Scopes struct {
		Missing []string `json:"missing"`
	} `json:"scopes"`
	Setup     []xpRow `json:"setup"`
	ExpiresAt string  `json:"expiresAt"`
}

type xpParked struct {
	Items []struct {
		Trigger   string `json:"trigger"`
		FireID    string `json:"fireId"`
		LastError string `json:"lastError"`
		Pending   bool   `json:"pending"`
	} `json:"items"`
}

type xpBundle struct {
	Installed      bool  `json:"installed"`
	ShippedVersion int64 `json:"shippedVersion"`
}

// ---- the case ----------------------------------------------------------------

func xpCaseAppSetUp(c *C) {
	// Installing applehealth admits its python bodies on the server, which
	// the suite runs on this host.
	if err := xvPythonRecentEnough(); err != nil {
		c.skipf("%v: the shipped provider bodies are written for python %d.%d or newer", err, xvMinPython[0], xvMinPython[1])
	}
	r := c.r
	details, err := json.Marshal(xpDetails)
	c.requiref(err == nil, "encoding the details: %v", err)

	var md struct {
		Issuer       string   `json:"issuer"`
		DetailsTypes []string `json:"authorization_details_types_supported"`
	}
	status, raw := c.doAs("", http.MethodGet, "/.well-known/oauth-authorization-server", nil, &md)
	c.requiref(status == http.StatusOK, "the authorization server metadata answered %d: %s", status, raw)
	for _, t := range []string{"package", "account", "webhook"} {
		c.requiref(slices.Contains(md.DetailsTypes, t), "the metadata lists detail types %v, without %s", md.DetailsTypes, t)
	}
	c.stepf("the metadata names issuer `%s` and lists the detail types %v", md.Issuer, md.DetailsTypes)
	head, _ := c.changelogHead()

	// The app's browser: the request parks and the answer is the approval
	// page, never a redirect to the app.
	verifier := xpVerifier(c)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type": {"code"}, "client_id": {xpClientID}, "redirect_uri": {xpRedirect},
		"scope": {strings.Join(xpScopes, " ")}, "state": {xpAppState},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"authorization_details": {string(details)},
	}
	browser := &http.Client{Timeout: r.timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := browser.Get(r.base + "/oauth/authorize?" + q.Encode())
	c.requiref(err == nil, "GET /oauth/authorize: %v", err)
	_ = resp.Body.Close()
	loc := resp.Header.Get("Location")
	c.requiref(resp.StatusCode == http.StatusSeeOther && strings.Contains(loc, "/authorize/"),
		"/oauth/authorize answered %d to %q, want 303 to the approval page", resp.StatusCode, loc)
	id := path.Base(loc)
	c.stepf("`GET /oauth/authorize` with Sync's scopes and details parked request `%s` and sent the browser to the approval page", id)

	// The approval page's read: one row per detail, nothing in place yet.
	var v xpRequest
	status, raw = c.do(http.MethodGet, "/api/v1/authorizations/"+id, nil, &v)
	c.requiref(status == http.StatusOK, "the approval page's read answered %d: %s", status, raw)
	c.requiref(v.State == "pending" && v.App.Local && v.App.Actor == "app:127.0.0.1" && v.Repository == r.authority,
		"the request reads state %q, local %t, actor %q, repository %q", v.State, v.App.Local, v.App.Actor, v.Repository)
	c.requiref(len(v.Previous) == 0 || string(v.Previous) == "null", "a local app's request carries a previous token: %s", v.Previous)
	c.requiref(v.PlanHash != "" && len(v.Setup) == 4, "the request plans %d rows under hash %q: %s", len(v.Setup), v.PlanHash, raw)
	for i, row := range v.Setup {
		c.requiref(row.Blocked == nil, "row %d is blocked on a fresh repository: %+v", i, row.Blocked)
	}
	apple, health := v.Setup[0].Package, v.Setup[1].Package
	c.requiref(v.Setup[0].Action == "install" && apple != nil && apple.ID == xpApplePkg && apple.Shipped >= 3 && len(apple.Requires) == 0,
		"the apple row plans %q: %+v", v.Setup[0].Action, apple)
	c.requiref(v.Setup[1].Action == "install" && health != nil && health.ID == xpAppleHealthPkg && health.Shipped >= 5,
		"the applehealth row plans %q: %+v", v.Setup[1].Action, health)
	c.requiref(len(health.Requires) == 1 && health.Requires[0].ID == xpHealthPkg && health.Requires[0].Action == "install",
		"applehealth's chain is %+v, want health installed first", health.Requires)
	c.requiref(v.Setup[2].Action == "create" && v.Setup[2].Account != nil && v.Setup[2].Account.ID == xpAccountID,
		"the account row plans %q: %+v", v.Setup[2].Action, v.Setup[2].Account)
	c.requiref(v.Setup[3].Action == "enable" && v.Setup[3].Webhook != nil && v.Setup[3].Webhook.Package == xpAppleHealthPkg,
		"the webhook row plans %q: %+v", v.Setup[3].Action, v.Setup[3].Webhook)
	var approved []string
	for _, s := range v.Scopes {
		c.requiref(!s.Allowed, "scope %s reads as already allowed on a first approval", s.Scope)
		approved = append(approved, s.Scope)
	}
	c.requiref(len(approved) == len(xpScopes), "the page lists scopes %v", approved)
	c.stepf("the page plans: install `%s` %d, install `%s` %d with `%s` first, create account `%s`, enable `%s`; no previous token",
		xpApplePkg, apple.Shipped, xpAppleHealthPkg, health.Shipped, xpHealthPkg, xpAccountID, xpWebhook)

	// Approving with the plan's hash runs the set up; the answer carries the
	// rows waiting.
	var ans xpAnswer
	status, raw = c.do(http.MethodPost, "/api/v1/authorizations/"+id,
		map[string]any{"scopes": approved, "lifetimeDays": 30, "planHash": v.PlanHash}, &ans)
	c.requiref(status == http.StatusAccepted && ans.State == "setting-up" && ans.Redirect == "" && len(ans.Setup) == 4,
		"the approval answered %d: %s", status, raw)
	c.requiref(xpProgress(ans.Setup) == "waiting,waiting,waiting,waiting", "the approval's rows start at %s", xpProgress(ans.Setup))
	c.stepf("approving with the plan's hash answered 202 `setting-up`, every row waiting")

	done := c.xpSettle(id)
	c.requiref(xpProgress(done.Setup) == "done,done,done,done", "the approved request's rows read %s", xpProgress(done.Setup))
	next, err := url.Parse(done.Next)
	c.requiref(err == nil && strings.HasPrefix(done.Next, xpRedirect+"?"), "the approved request hands over %q", prefix(done.Next, len(xpRedirect)+8))
	code := next.Query().Get("code")
	c.requiref(code != "" && next.Query().Get("state") == xpAppState && next.Query().Get("iss") == md.Issuer,
		"the redirect carries state %q and iss %q, and a code: %t", next.Query().Get("state"), next.Query().Get("iss"), code != "")
	status, _ = c.do(http.MethodGet, "/api/v1/authorizations/"+id, nil, nil)
	c.requiref(status == http.StatusNotFound, "the request read again after its hand-over answered %d, want 404", status)
	c.stepf("the read that saw it approved handed over the redirect (code, state, iss) once; the next read is 404")

	// The app's exchange.
	status, raw = c.xpForm(http.MethodPost, "/oauth/token", url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "client_id": {xpClientID},
		"redirect_uri": {xpRedirect}, "code_verifier": {verifier},
	})
	c.requiref(status == http.StatusOK, "the code exchange answered %d%s", status, redacted(status, raw))
	tokRaw := raw
	var tok xpToken
	c.requiref(json.Unmarshal(tokRaw, &tok) == nil, "the token response is not JSON")
	webhookURL := md.Issuer + "/webhooks/" + url.PathEscape(r.authority) + "/" + xpWebhook
	wantScopes := append(slices.Clone(approved), "webhook:"+xpWebhook)
	gotScopes := strings.Fields(tok.Scope)
	slices.Sort(wantScopes)
	slices.Sort(gotScopes)
	c.requiref(strings.HasPrefix(tok.AccessToken, "substrate_tok_") && tok.TokenType == "Bearer" && tok.Repository == r.authority &&
		tok.ExpiresIn != nil && *tok.ExpiresIn == 30*24*3600 && slices.Equal(gotScopes, wantScopes),
		"the token is type %q for %q, expires_in %v, scope %q", tok.TokenType, tok.Repository, tok.ExpiresIn, tok.Scope)
	g := tok.AuthorizationDetails
	c.requiref(len(g) == 4 &&
		g[0].Package == xpApplePkg && g[0].Version == apple.Shipped &&
		g[1].Package == xpAppleHealthPkg && g[1].Version == health.Shipped &&
		g[2].Kind == xpAccountKind && g[2].ID == xpAccountID && g[2].Properties == nil &&
		g[3].Trigger == xpWebhook && g[3].URL == webhookURL,
		"the token's authorization_details = %+v, want the versions held and the webhook at %s", g, webhookURL)
	app := tok.AccessToken
	c.stepf("the exchange minted a 30-day token for `%s` scoped `%s`; its details name `%s` at %d, `%s` at %d, the account, and %s",
		tok.Repository, tok.Scope, xpApplePkg, g[0].Version, xpAppleHealthPkg, g[1].Version, webhookURL)

	// What the engine holds now, read with the owner's token.
	for _, want := range []struct {
		id      string
		shipped int64
	}{{xpApplePkg, apple.Shipped}, {xpHealthPkg, 0}, {xpAppleHealthPkg, health.Shipped}} {
		var st xpBundle
		status, raw = c.do(http.MethodGet, xvBundleRecord(want.id)+"/status", nil, &st)
		c.requiref(status == http.StatusOK && st.Installed, "`%s` is not installed: %d %s", want.id, status, raw)
		c.requiref(want.shipped == 0 || st.ShippedVersion == want.shipped,
			"`%s` holds shipped version %d, want %d", want.id, st.ShippedVersion, want.shipped)
	}
	rows := c.readChangesForward(head)
	first := func(kind, id string) int {
		return slices.IndexFunc(rows, func(row changeRow) bool { return row.Kind == kind && row.RecordID == id })
	}
	last := func(kind, id string) int {
		for j := len(rows) - 1; j >= 0; j-- {
			if rows[j].Kind == kind && rows[j].RecordID == id {
				return j
			}
		}
		return -1
	}
	pkgRow := "substrate.reamde.dev/core/package"
	appleAt, healthAt, appleHealthAt := first(pkgRow, xpApplePkg), first(pkgRow, xpHealthPkg), first(pkgRow, xpAppleHealthPkg)
	accountAt, armedAt := first(xpAccountKind, xpAccountID), last(xpTriggerKind, xpWebhook)
	c.requiref(appleAt >= 0 && appleAt < healthAt && healthAt < appleHealthAt && appleHealthAt < accountAt && accountAt < armedAt,
		"the set up's changelog order is apple %d, health %d, applehealth %d, account %d, trigger %d: want packages (health before "+
			"applehealth), then the account, then the webhook", appleAt, healthAt, appleHealthAt, accountAt, armedAt)
	accountRows := 0
	for _, row := range rows {
		if row.Kind == xpAccountKind && row.RecordID == xpAccountID {
			accountRows++
		}
	}
	c.requiref(accountRows == 1 && rows[accountAt].Actor == "api" && rows[armedAt].Actor == "api",
		"the account has %d rows, the first by %q, and the trigger's arming is by %q: want one write each, the owner's door's",
		accountRows, rows[accountAt].Actor, rows[armedAt].Actor)
	c.stepf("the changelog holds the set up in order: `%s` (seq %d), `%s` (seq %d) before `%s` (seq %d), then the account's one write (seq %d) and the trigger's arming (seq %d), both as the owner's `api` door",
		xpApplePkg, rows[appleAt].Seq, xpHealthPkg, rows[healthAt].Seq, xpAppleHealthPkg, rows[appleHealthAt].Seq, rows[accountAt].Seq, rows[armedAt].Seq)
	account := c.getRec("/api/v1/"+xpAccountKind, xpAccountID)
	c.requiref(account.prop("displayName") == xpAccountName && account.prop("timezone") == "Europe/London",
		"the account holds %v", account.Properties)
	trigger := c.getRec("/api/v1/"+xpTriggerKind, xpWebhook)
	source, _ := trigger.Properties["source"].(map[string]any)
	hook, _ := source["webhook"].(map[string]any)
	key, _ := hook["key"].(string)
	c.requiref(trigger.Properties["enabled"] == true && regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`).MatchString(key),
		"the trigger is enabled %v with a key of %d characters", trigger.Properties["enabled"], len(key))
	c.requiref(!strings.Contains(string(tokRaw), key) && !strings.Contains(done.Next, key), "the webhook key reached an answer the app reads")
	c.stepf("account `%s` holds the details' displayName and timezone; `%s` is enabled under a 32-character key no answer to the app carried",
		xpAccountID, xpWebhook)

	// The app's launch check: everything in place, nothing missing.
	var chk xpCheck
	status, raw = c.doAs(app, http.MethodPost, "/api/v1/authorizations/check",
		map[string]any{"scope": strings.Join(xpScopes, " "), "details": json.RawMessage(details)}, &chk)
	c.requiref(status == http.StatusOK, "the app's check answered %d: %s", status, raw)
	c.requiref(len(chk.Scopes.Missing) == 0 && len(chk.Setup) == 4 && chk.ExpiresAt != "",
		"the check answers missing %v, %d rows, expiresAt %q", chk.Scopes.Missing, len(chk.Setup), chk.ExpiresAt)
	for i, row := range chk.Setup {
		c.requiref(row.Action == "none" && row.Blocked == nil, "check row %d reads %q: %s", i, row.Action, raw)
	}
	c.requiref(chk.Setup[3].Webhook != nil && chk.Setup[3].Webhook.Package == xpAppleHealthPkg,
		"the check's webhook row names %+v", chk.Setup[3].Webhook)
	c.stepf("the app's own check answers every row `none`, no scope missing, and the webhook's package `%s`", xpAppleHealthPkg)

	// Delivering with the token, where the key would go.
	hookPath := strings.TrimPrefix(webhookURL, md.Issuer)
	body := []byte(`{"data":{"metrics":[]}}`)
	deliver := func(bearer string) (int, []byte) {
		status, _, raw := c.xpSend(bearer, http.MethodPost, hookPath, "application/json",
			map[string]string{"X-Substrate-Account": xpAccountID}, body)
		return status, raw
	}
	status, raw = deliver(r.token)
	c.requiref(status == http.StatusNotFound, "the owner's full-access token at the door answered %d, want the wrong key's 404: %s", status, raw)
	status, raw = deliver(app)
	c.requiref(status == http.StatusAccepted, "the app's delivery answered %d: %s", status, raw)
	var fire struct {
		Fire string `json:"fire"`
	}
	c.requiref(json.Unmarshal(raw, &fire) == nil && fire.Fire != "", "the delivery's answer names no fire: %s", raw)
	c.stepf("`POST %s` with the app's token in place of the key answered 202, fire `%s`; the owner's full-access token there is the door's 404", hookPath, fire.Fire)
	parkedPath := "/api/v1/" + xpTriggerKind + "/" + xpWebhook + "/parked"
	var parked xpParked
	status, raw = c.doAs(app, http.MethodGet, parkedPath, nil, &parked)
	c.requiref(status == http.StatusOK, "the app's read of its webhook's parked list answered %d: %s", status, raw)
	for _, p := range parked.Items {
		c.requiref(p.Trigger == xpWebhook, "the parked list holds a row of %q", p.Trigger)
	}
	status, raw = c.doAs(app, http.MethodGet, "/api/v1/"+xpTriggerKind+"/"+xpAdapter+"/parked", nil, nil)
	c.requiref(status == http.StatusForbidden, "the app's read of another trigger's parked list answered %d, want 403: %s", status, raw)
	c.stepf("the app reads `%s`'s parked list (%d rows) and is refused `%s`'s with 403", xpWebhook, len(parked.Items), xpAdapter)
	c.xpReceipt(app, fire.Fire, parkedPath)

	// Blobs: the app stores bytes a share points at, and an upload of bytes
	// the owner stored first answers the app's own name.
	ownerBytes := []byte("APP-01: bytes the owner stored first, under a name of their own (" + r.repository + ")\n")
	appBytes := []byte("APP-01: an image the app shares (" + r.repository + ")\n")
	status, _, raw = c.xpSend(r.token, http.MethodPut, "/api/v1/blobs?name=owner-private-scan.png", "image/png", nil, ownerBytes)
	c.requiref(status == http.StatusCreated, "the owner's upload answered %d: %s", status, raw)
	ownerBlob := xqBlobInfo(c, raw)
	status, _, raw = c.xpSend(app, http.MethodPut, "/api/v1/blobs?name=shared-from-sync.png", "image/png", nil, appBytes)
	c.requiref(status == http.StatusCreated, "the app's upload answered %d: %s", status, raw)
	appBlob := xqBlobInfo(c, raw)
	c.requiref(appBlob.Status == "stored" && appBlob.Name == "shared-from-sync.png" && appBlob.Size == int64(len(appBytes)),
		"the app's upload answered %+v", appBlob)
	var share record
	status, raw = c.doAs(app, http.MethodPost, recordsRoute, map[string]any{"kind": xpShareKind, "properties": map[string]any{
		"text": "shared by APP-01", "images": []any{map[string]any{"digest": appBlob.Digest}},
	}}, &share)
	c.requiref(status == http.StatusCreated, "the app's share answered %d: %s", status, raw)
	status, raw = c.doAs(app, http.MethodGet, "/api/v1/"+xpShareKind+"/"+url.PathEscape(share.ID), nil, &share)
	c.requiref(status == http.StatusOK, "the app's read of its share answered %d: %s", status, raw)
	images, _ := share.Properties["images"].([]any)
	img, _ := xpFirst(images).(map[string]any)
	c.requiref(img["digest"] == appBlob.Digest && img["name"] == "shared-from-sync.png",
		"the share's images read %v", share.Properties["images"])
	status, _, _ = c.xpSend(app, http.MethodGet, "/api/v1/blobs/"+appBlob.Digest, "", nil, nil)
	c.requiref(status == http.StatusForbidden, "the app's read of a blob answered %d, want 403", status)
	c.stepf("the app stored `%s` as `shared-from-sync.png` and share `%s` resolves it by digest and name; reading the bytes back is the owner's (403)",
		short(appBlob.Digest), share.ID)
	status, _, raw = c.xpSend(app, http.MethodPut, "/api/v1/blobs?name=from-the-app.png", "image/png", nil, ownerBytes)
	c.requiref(status == http.StatusCreated, "the app's upload of the owner's bytes answered %d: %s", status, raw)
	dup := xqBlobInfo(c, raw)
	c.requiref(dup.Digest == ownerBlob.Digest && dup.Name == "from-the-app.png" && !strings.Contains(string(raw), "owner-private-scan"),
		"the app's upload of bytes the owner stored first answered %s", raw)
	manifest := c.getRec("/api/v1/"+xpBlobKind, ownerBlob.Digest)
	c.requiref(manifest.prop("name") == "owner-private-scan.png", "the manifest's name moved to %q", manifest.prop("name"))
	c.stepf("the app's upload of bytes the owner stored first answered the same digest under the app's own name; the manifest still names the owner's")
	appRows := c.readChangesForward(head)
	for _, want := range []struct{ kind, id string }{{xpShareKind, share.ID}, {xpBlobKind, appBlob.Digest}} {
		i := slices.IndexFunc(appRows, func(row changeRow) bool { return row.Kind == want.kind && row.RecordID == want.id })
		c.requiref(i >= 0 && appRows[i].Actor == v.App.Actor, "the first changelog row of %s/%s is by %q, want %q",
			want.kind, want.id, xpActorAt(appRows, i), v.App.Actor)
	}
	c.stepf("the share and the app's blob manifest are attributed to `%s`", v.App.Actor)

	// Revoking ends the deliveries.
	status, raw = c.xpForm(http.MethodPost, "/oauth/revoke", url.Values{"token": {app}, "client_id": {xpClientID}})
	c.requiref(status == http.StatusOK, "the revocation answered %d: %s", status, raw)
	status, raw = deliver(app)
	c.requiref(status == http.StatusNotFound, "a delivery with the revoked token answered %d, want 404: %s", status, raw)
	status, _ = c.doAs(app, http.MethodGet, parkedPath, nil, nil)
	c.requiref(status == http.StatusUnauthorized, "the revoked token's read answered %d, want 401", status)
	c.stepf("after `POST /oauth/revoke` the door answers the revoked token 404 and the API 401")

	c.xpQuiet()
}

// ---- helpers ----------------------------------------------------------------

// xpVerifier is a fresh PKCE verifier: 64 characters of the unreserved
// alphabet.
func xpVerifier(c *C) string {
	c.t.Helper()
	b := make([]byte, 48)
	_, err := rand.Read(b)
	c.requiref(err == nil, "reading random bytes: %v", err)
	return base64.RawURLEncoding.EncodeToString(b)
}

// xpProgress is rows' progress, in order.
func xpProgress(rows []xpRow) string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row.Progress
	}
	return strings.Join(out, ",")
}

func xpFirst(items []any) any {
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

func xpActorAt(rows []changeRow, i int) string {
	if i < 0 {
		return "(no row)"
	}
	return rows[i].Actor
}

// xpSettle reads the request as the approval page does until its set up
// ended, and holds that it ended approved: the read that sees it so is the
// one that hands the redirect over. The polls are kept out of the report;
// the progress they saw is one step.
func (c *C) xpSettle(id string) xpRequest {
	c.t.Helper()
	deadline := time.Now().Add(c.r.streamDeadline(5 * time.Minute))
	var seen []string
	for {
		status, raw, err := httpJSON(c.r.hc, c.r.base, c.r.token, http.MethodGet, "/api/v1/authorizations/"+id, nil)
		c.requiref(err == nil, "reading the request while it sets up: %v", err)
		c.requiref(status == http.StatusOK, "reading the request while it sets up answered %d: %s", status, raw)
		var v xpRequest
		c.requiref(json.Unmarshal(raw, &v) == nil, "undecodable request: %s", raw)
		if p := xpProgress(v.Setup); len(seen) == 0 || seen[len(seen)-1] != p {
			seen = append(seen, p)
		}
		switch v.State {
		case "approved":
			c.stepf("the page's reads saw the rows go %s, then the request approved", strings.Join(seen, " → "))
			return v
		case "pending":
			c.requiref(false, "the set up stopped: %+v (progress %s)", v.Failure, xpProgress(v.Setup))
		}
		c.requiref(time.Now().Before(deadline), "the set up had not ended by the deadline; progress %s", strings.Join(seen, " → "))
		time.Sleep(250 * time.Millisecond)
	}
}

// xpReceipt waits for the delivery's export receipt, which the app reads
// with its own `read:` scope: the delivery filed through the trigger's
// callable. A delivery that parked instead fails with why.
func (c *C) xpReceipt(app, fire, parkedPath string) {
	c.t.Helper()
	deadline := time.Now().Add(c.r.streamDeadline(2 * time.Minute))
	receipt := "/api/v1/" + xpExportKind + "/" + url.PathEscape(fire)
	for {
		status, raw, err := httpJSON(c.r.hc, c.r.base, app, http.MethodGet, receipt, nil)
		c.requiref(err == nil, "reading the receipt: %v", err)
		if status == http.StatusOK {
			c.stepf("the delivery filed: the app reads its receipt `%s` with its own `read:` scope", receipt)
			return
		}
		c.requiref(status == http.StatusNotFound, "reading the receipt answered %d: %s", status, raw)
		var parked xpParked
		status, raw, err = httpJSON(c.r.hc, c.r.base, app, http.MethodGet, parkedPath, nil)
		c.requiref(err == nil && status == http.StatusOK, "reading the parked list answered %d: %v %s", status, err, raw)
		c.requiref(json.Unmarshal(raw, &parked) == nil, "undecodable parked list: %s", raw)
		for _, p := range parked.Items {
			c.requiref(p.FireID != fire || p.Pending, "the delivery parked: %s", p.LastError)
		}
		c.requiref(time.Now().Before(deadline), "the delivery's receipt did not land by the deadline")
		time.Sleep(500 * time.Millisecond)
	}
}

// xpQuiet waits until applehealth's two triggers have nothing left to
// deliver, so the cases after this one find the repository as it stays.
func (c *C) xpQuiet() {
	c.t.Helper()
	deadline := time.Now().Add(c.r.streamDeadline(2 * time.Minute))
	for {
		var list struct {
			Items []struct {
				ID       string `json:"id"`
				Lag      int64  `json:"lag"`
				Pending  int64  `json:"pending"`
				InFlight int64  `json:"inFlight"`
			} `json:"items"`
		}
		c.requiref(c.r.fetch("/api/v1/"+xpTriggerKind+"/status", &list) == nil, "reading the trigger status failed")
		var busy []string
		for _, it := range list.Items {
			if (it.ID == xpWebhook || it.ID == xpAdapter) && it.Lag+it.Pending+it.InFlight > 0 {
				busy = append(busy, it.ID+" lag "+strconv.FormatInt(it.Lag, 10)+", pending "+
					strconv.FormatInt(it.Pending, 10)+", in flight "+strconv.FormatInt(it.InFlight, 10))
			}
		}
		if len(busy) == 0 {
			c.stepf("`%s` and `%s` have nothing left to deliver", xpWebhook, xpAdapter)
			return
		}
		c.requiref(time.Now().Before(deadline), "the triggers did not settle: %s", strings.Join(busy, "; "))
		time.Sleep(500 * time.Millisecond)
	}
}

// xpSend sends one request with raw bytes and the headers given, under a
// bearer when one is given, and records it as a step.
func (c *C) xpSend(bearer, method, path, contentType string, headers map[string]string, body []byte) (int, http.Header, []byte) {
	c.t.Helper()
	req, err := http.NewRequest(method, c.r.base+path, bytes.NewReader(body))
	c.requiref(err == nil, "building %s %s: %v", method, path, err)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
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

// xpForm sends one form-encoded request, as an app speaks to /oauth/token
// and /oauth/revoke.
func (c *C) xpForm(method, path string, form url.Values) (int, []byte) {
	c.t.Helper()
	status, _, raw := c.xpSend("", method, path, "application/x-www-form-urlencoded", nil, []byte(form.Encode()))
	return status, raw
}
