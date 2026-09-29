package e2e

// The 800 block: the two host facilities that dial out on a repository's
// behalf, each against a fake the test hosts on loopback. OAU-01 to OAU-03
// connect an account through the OAuth facility and watch the sweep refresh
// it; EMB-01 buys a vector through the embeddings queue. Both need the
// `egress` precondition (SUBSTRATE_EGRESS_ALLOW naming loopback), and the
// OAuth cases need the facility on (SUBSTRATE_OAUTH_CALLBACK_URL).
//
// Everything lives under two authorities of these cases' own. The account
// OAU-01 connects stays connected, and the provider row EMB-01 names stays
// the repository's embeddings provider, pointing at the run's stub.
//
// EMB-01 runs between OAU-02 and OAU-03 on purpose. The server's embed drain
// and its OAuth sweep both tick once a minute from boot, so the drain EMB-01
// waits for comes with the sweep that refreshes OAU-01's grant, and OAU-03
// then finds the refresh already made instead of waiting a second minute.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	xhOAuthAuthority = "oauth.e2e.example"
	xhOAuthPackage   = "mail"
	xhOAuthPkg       = xhOAuthAuthority + "/" + xhOAuthPackage
	xhConfigKind     = xhOAuthPkg + "/mailconfig"
	xhAccountKind    = xhOAuthPkg + "/mailaccount"

	// The fake provider's fixed credentials: what the config record carries
	// and what the token endpoint holds every exchange to.
	xhClientID     = "e2e-client"
	xhClientSecret = "e2e-client-secret"
	xhCode         = "e2e-code"
	xhAccessToken  = "e2e-access-1"
	xhRefreshToken = "e2e-refresh-1"
	xhRenewedToken = "e2e-access-2"
	// xhFirstExpiry is what the code exchange answers: inside the facility's
	// ten-minute refresh window, so the next sweep refreshes it.
	xhFirstExpiry = 300

	xhEmbedAuthority = "embed.e2e.example"
	xhEmbedPackage   = "embed"
	xhEmbedPkg       = xhEmbedAuthority + "/" + xhEmbedPackage
	xhMemoKind       = xhEmbedPkg + "/memo"
	xhMemoText       = "The lighthouse lamp comes on at dusk."
	xhEmbedModel     = "text-embedding-3-small"
)

func init() {
	registerCase(800, "OAU-01", "An account connects through the OAuth facility",
		"`oauth/start` answers a consent URL built from the bundle's declared endpoints and the config "+
			"record's client, with a signed state and a PKCE challenge; the callback with that state exchanges "+
			"the code once, proving the verifier, and the account reads connected with its scopes and no token "+
			"in sight.",
		xhCaseOAuthConnect)
	registerCase(810, "OAU-02", "A tampered or replayed state is refused",
		"A callback whose state has one character changed, or whose state already completed a connect, "+
			"answers the failure page and exchanges nothing; the untampered state of the same flow still "+
			"completes afterwards.",
		xhCaseOAuthBadState)
	registerCase(850, "OAU-03", "The refresh sweep renews an expiring token",
		"A grant the provider said expires inside the facility's refresh window is refreshed by the server's "+
			"own sweep, with the stored refresh token and the client's credentials, and the account stays "+
			"connected.",
		xhCaseOAuthRefresh)
	registerCase(820, "EMB-01", "An embeddable property queues on write and the fake's vector lands",
		"A write of an `embed: true` property queues it: a semantic read answers 503 naming the pending count "+
			"while the provider refuses, and once the provider answers, the drain buys the vector for that "+
			"text and the semantic read ranks the record with the provider's own similarity.",
		xhCaseEmbedding)
}

// --- the fake OAuth provider ----------------------------------------------

// xhProvider is an OAuth provider in a box: `/token` answers code exchanges
// and refreshes and records every form it was sent.
type xhProvider struct {
	srv       *httptest.Server
	mu        sync.Mutex
	exchanges []url.Values
	refreshes []url.Values
}

func newXhProvider() *xhProvider {
	p := &xhProvider{}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		form := r.PostForm
		if form.Get("client_id") != xhClientID || form.Get("client_secret") != xhClientSecret {
			http.Error(w, "bad client", http.StatusUnauthorized)
			return
		}
		out := map[string]any{"token_type": "Bearer"}
		p.mu.Lock()
		defer p.mu.Unlock()
		switch form.Get("grant_type") {
		case "authorization_code":
			p.exchanges = append(p.exchanges, form)
			if form.Get("code") != xhCode {
				http.Error(w, "bad code", http.StatusBadRequest)
				return
			}
			out["access_token"], out["refresh_token"], out["expires_in"] = xhAccessToken, xhRefreshToken, xhFirstExpiry
		case "refresh_token":
			p.refreshes = append(p.refreshes, form)
			out["access_token"], out["expires_in"] = xhRenewedToken, 3600
		default:
			http.Error(w, "bad grant", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/revoke", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	p.srv = httptest.NewServer(mux)
	return p
}

func (p *xhProvider) counts() (exchanges, refreshes int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.exchanges), len(p.refreshes)
}

func (p *xhProvider) exchange(i int) url.Values {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exchanges[i]
}

func (p *xhProvider) refresh(i int) url.Values {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.refreshes[i]
}

// xhOAuth is what OAU-01 connected, for the two cases after it.
var xhOAuth struct {
	provider *xhProvider
	account  string // the account's record path
	state    string // the state OAU-01's callback completed with
}

// xhOAuthDocs is the bundle closure: a client kind binding `oauth2`, an
// account kind binding `accountconfig`, and the trusted provider metadata
// pointing at the fake.
func xhOAuthDocs(base string) []map[string]any {
	return []map[string]any{
		xfDoc("substrate.reamde.dev/core/package", xhOAuthPkg, map[string]any{
			"authority": xhOAuthAuthority, "package": xhOAuthPackage, "version": 1,
		}),
		xfDoc("substrate.reamde.dev/core/actor", "bundle:"+xhOAuthAuthority+":"+xhOAuthPackage, map[string]any{
			"authority": xhOAuthAuthority, "package": xhOAuthPackage,
		}),
		xfDoc("substrate.reamde.dev/core/bundle", xhOAuthPkg, map[string]any{
			"authority":   xhOAuthAuthority,
			"package":     xhOAuthPackage,
			"description": "A mail provider the e2e run hosts itself.",
			"inputs":      map[string]any{"client": map[string]any{"kind": xhConfigKind}},
			"installs":    []string{xhConfigKind, xhAccountKind},
			"oauth2": map[string]any{
				"clientInput":           "client",
				"authorizationEndpoint": base + "/authorize",
				"tokenEndpoint":         base + "/token",
				"revocationEndpoint":    base + "/revoke",
				"featureScopes": map[string]any{
					"enabledMail": map[string]any{"scopes": []string{"mail.read", "mail.send"}},
				},
			},
		}),
		xfDoc("substrate.reamde.dev/core/kind", xhConfigKind, map[string]any{
			"authority": xhOAuthAuthority, "package": xhOAuthPackage,
			"names":  map[string]any{"singular": "mailconfig"},
			"traits": []string{"substrate.reamde.dev/core/oauth2"},
			"properties": map[string]any{
				"clientId":     map[string]any{"type": "string", "description": "the OAuth client id"},
				"clientSecret": map[string]any{"type": "secret", "description": "the OAuth client secret"},
			},
		}),
		xfDoc("substrate.reamde.dev/core/kind", xhAccountKind, map[string]any{
			"authority": xhOAuthAuthority, "package": xhOAuthPackage,
			"names":  map[string]any{"singular": "mailaccount"},
			"traits": []string{"substrate.reamde.dev/core/accountconfig"},
			"properties": map[string]any{
				"tokenRef":      map[string]any{"type": "secret", "writer": "oauth", "description": "the stored grant"},
				"tokenStatus":   map[string]any{"type": "string", "writer": "oauth", "description": "the grant's last reported state"},
				"grantedScopes": map[string]any{"type": "string", "repeated": true, "writer": "oauth", "description": "the scopes the grant covers"},
				"address":       map[string]any{"type": "email", "writer": "owner", "description": "the mailbox"},
				"enabledMail":   map[string]any{"type": "bool", "writer": "owner", "description": "whether mail is synced"},
			},
		}),
	}
}

// xhStart begins a connect flow and returns the consent URL, or "" when the
// facility is off on this server.
func xhStart(c *C) string {
	c.t.Helper()
	var started struct {
		URL string `json:"url"`
	}
	status, raw := c.do(http.MethodPost, "/api/v1/oauth/start", map[string]any{"record": xhOAuth.account}, &started)
	if status == http.StatusUnprocessableEntity && strings.Contains(string(raw), "oauth is not configured") {
		return ""
	}
	c.requiref(status == http.StatusOK && started.URL != "", "oauth/start answered %d: %s", status, raw)
	return started.URL
}

// xhOutcome finds the message the return page posts back to the console:
// `{"source":"substrate-oauth","ok":…}` with the record or the correlation.
var xhOutcome = regexp.MustCompile(`\{[^{}]*"source":"substrate-oauth"[^{}]*\}`)

// xhCallback plays the browser the provider redirects: an unauthenticated GET
// of the callback. The answer is the return page, and the one JSON object it
// posts back is the outcome, which is what this returns. The step leaves the
// query out: a signed state is the callback's only credential until it is
// spent.
func xhCallback(c *C, state, code string) (ok bool, outcome string) {
	c.t.Helper()
	path := "/api/v1/oauth/callback?" + url.Values{"state": {state}, "code": {code}}.Encode()
	status, raw, err := httpJSON(c.r.hc, c.r.base, "", http.MethodGet, path, nil)
	// A transport error quotes the URL, query and all; only its cause is kept.
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	c.requiref(err == nil, "GET /api/v1/oauth/callback: %v", err)
	c.stepf("`GET /api/v1/oauth/callback` (state and code withheld) answered %d", status)
	c.requiref(status == http.StatusOK, "the callback answered %d", status)
	outcome = string(xhOutcome.Find(raw))
	var msg struct {
		OK *bool `json:"ok"`
	}
	c.requiref(outcome != "" && json.Unmarshal([]byte(outcome), &msg) == nil && msg.OK != nil,
		"the callback page carries no outcome message")
	return *msg.OK, outcome
}

// --- OAU-01 --------------------------------------------------------------

func xhCaseOAuthConnect(c *C) {
	p := newXhProvider()
	c.r.t.Cleanup(p.srv.Close)
	status, raw := c.do(http.MethodPost, "/api/v1/vocabulary/apply", map[string]any{"documents": xhOAuthDocs(p.srv.URL)}, nil)
	c.requiref(status == http.StatusOK, "applying the %s bundle answered %d: %s", xhOAuthPkg, status, raw)
	c.putRec("/api/v1/"+xhConfigKind, "default", map[string]any{"clientId": xhClientID, "clientSecret": xhClientSecret})
	c.putRec("/api/v1/"+xhAccountKind, "owner", map[string]any{"address": "owner@example.com", "enabledMail": true})
	xhOAuth.provider, xhOAuth.account = p, xhAccountKind+"/owner"
	c.stepf("installed `%s` with its OAuth endpoints on a fake provider at %s, a client config and one account", xhOAuthPkg, p.srv.URL)

	consent := xhStart(c)
	if consent == "" {
		xhOAuth.account = ""
		c.skipf("oauth/start says the facility is not configured: the server needs SUBSTRATE_OAUTH_CALLBACK_URL")
		return
	}
	// The failure messages below never print the query whole: its state is
	// the callback's credential until the callback spends it.
	u, err := url.Parse(consent)
	c.requiref(err == nil, "the consent URL does not parse")
	c.requiref(u.Scheme+"://"+u.Host+u.Path == p.srv.URL+"/authorize", "the consent URL points at %s, not the bundle's authorization endpoint", u.Scheme+"://"+u.Host+u.Path)
	q := u.Query()
	scopes := strings.Fields(q.Get("scope"))
	c.requiref(q.Get("client_id") == xhClientID && q.Get("response_type") == "code" &&
		slices.Contains(scopes, "mail.read") && slices.Contains(scopes, "mail.send"),
		"the consent carries client_id %q, response_type %q and scopes %v, want the config's client and the enabled feature's scopes",
		q.Get("client_id"), q.Get("response_type"), scopes)
	c.requiref(strings.HasSuffix(q.Get("redirect_uri"), "/api/v1/oauth/callback"), "the consent's redirect_uri %q is not the callback", q.Get("redirect_uri"))
	c.requiref(q.Get("state") != "" && q.Get("code_challenge") != "" && q.Get("code_challenge_method") == "S256",
		"the consent carries a state %t, a challenge %t and challenge method %q, want both and S256",
		q.Get("state") != "", q.Get("code_challenge") != "", q.Get("code_challenge_method"))
	c.stepf("`oauth/start` answered the consent URL at the bundle's `/authorize` with client `%s`, scopes %v, a signed state and an S256 challenge", xhClientID, scopes)

	ok, page := xhCallback(c, q.Get("state"), xhCode)
	c.requiref(ok && strings.Contains(page, xhOAuth.account), "the callback did not report the account connected: %s", page)
	exchanges, _ := p.counts()
	c.requiref(exchanges == 1, "the provider saw %d code exchanges, want 1", exchanges)
	form := p.exchange(0)
	verifier := form.Get("code_verifier")
	sum := sha256.Sum256([]byte(verifier))
	c.requiref(verifier != "" && base64.RawURLEncoding.EncodeToString(sum[:]) == q.Get("code_challenge"),
		"the exchange's code_verifier does not answer the consent's challenge")
	c.requiref(form.Get("code") == xhCode && form.Get("redirect_uri") == q.Get("redirect_uri"),
		"the exchange sent code %q and redirect_uri %q", form.Get("code"), form.Get("redirect_uri"))
	xhOAuth.state = q.Get("state")
	c.stepf("the callback with the state and the code answered the connected page; the server exchanged the code once, with the verifier the challenge was made from")

	status, raw = c.do(http.MethodGet, "/api/v1/"+xhOAuth.account, nil, nil)
	c.requiref(status == http.StatusOK, "reading the account answered %d: %s", status, raw)
	var account record
	c.requiref(json.Unmarshal(raw, &account) == nil, "undecodable account: %s", raw)
	c.requiref(account.prop("tokenStatus") == "connected", "the account's tokenStatus is %q, want connected", account.prop("tokenStatus"))
	granted, _ := account.Properties["grantedScopes"].([]any)
	c.requiref(len(granted) == 2, "the account's grantedScopes are %v, want the two the toggle asks for", account.Properties["grantedScopes"])
	c.requiref(account.Properties["tokenRef"] != nil, "the account carries no tokenRef")
	for _, secret := range []string{xhAccessToken, xhRefreshToken, xhClientSecret} {
		c.requiref(!strings.Contains(string(raw), secret), "the account's read carries a raw credential")
	}
	c.stepf("the account reads `connected` with its two granted scopes and a tokenRef, and its read carries neither token")
}

// --- OAU-02 --------------------------------------------------------------

func xhCaseOAuthBadState(c *C) {
	if xhOAuth.account == "" || xhOAuth.state == "" {
		c.skipf("OAU-01 connected no account, so there is no flow to tamper with")
		return
	}
	p := xhOAuth.provider
	consent := xhStart(c)
	c.requiref(consent != "", "oauth/start says the facility is off, after OAU-01 used it")
	u, err := url.Parse(consent)
	c.requiref(err == nil, "the consent URL does not parse")
	state := u.Query().Get("state")
	before, _ := p.counts()

	// One character changed: the HMAC no longer matches, and the state is
	// refused before any flow row is read or consumed.
	last := state[len(state)-1]
	swap := byte('A')
	if last == 'A' {
		swap = 'B'
	}
	tampered := state[:len(state)-1] + string(swap)
	ok, page := xhCallback(c, tampered, xhCode)
	c.requiref(!ok && strings.Contains(page, "correlation"), "the tampered state was not refused: %s", page)
	c.stepf("the callback with the state's last character changed answered the failure page, naming only a correlation id")

	ok, page = xhCallback(c, xhOAuth.state, xhCode)
	c.requiref(!ok, "the state OAU-01 already completed with connected again: %s", page)
	c.stepf("the callback replaying the state OAU-01 completed with answered the failure page: a state completes once")
	after, _ := p.counts()
	c.requiref(after == before, "the refused callbacks exchanged %d codes, want none", after-before)
	c.stepf("the provider saw no code exchange for either refused callback")

	// The flow the tampered copy imitated is intact: the refusal consumed
	// nothing, so the genuine state completes.
	ok, page = xhCallback(c, state, xhCode)
	c.requiref(ok, "the untampered state of the same flow was refused after the tampered copy: %s", page)
	after, _ = p.counts()
	c.requiref(after == before+1, "the genuine callback exchanged %d codes, want 1", after-before)
	c.stepf("the untampered state of the same flow then completed with one exchange: the refusal was the tamper's, not the flow's")
}

// --- OAU-03 --------------------------------------------------------------

func xhCaseOAuthRefresh(c *C) {
	if xhOAuth.account == "" || xhOAuth.state == "" {
		c.skipf("OAU-01 connected no account, so there is no grant to refresh")
		return
	}
	p := xhOAuth.provider
	// The sweep runs once a minute, and the grant expires in 300 seconds, so
	// the next pass after the exchange refreshes it. Nothing here asks it to.
	deadline := time.Now().Add(c.r.streamDeadline(100 * time.Second))
	for {
		if _, refreshes := p.counts(); refreshes > 0 {
			break
		}
		c.requiref(time.Now().Before(deadline), "no refresh reached the provider within %s of the grant; the sweep runs once a minute", c.r.streamDeadline(100*time.Second))
		time.Sleep(time.Second)
	}
	form := p.refresh(0)
	c.requiref(form.Get("grant_type") == "refresh_token" && form.Get("refresh_token") == xhRefreshToken,
		"the refresh sent grant %q with refresh token %q, want the stored one", form.Get("grant_type"), form.Get("refresh_token"))
	c.stepf("the server's own sweep sent the provider a `refresh_token` grant with the stored refresh token and the client's credentials")

	account := c.getRec("/api/v1/"+xhAccountKind, "owner")
	c.requiref(account.prop("tokenStatus") == "connected", "after the refresh the account reads %q, want connected", account.prop("tokenStatus"))
	c.stepf("the account still reads `connected` after the refresh")
}

// --- EMB-01 --------------------------------------------------------------

func xhCaseEmbedding(c *C) {
	status, raw := c.do(http.MethodPost, "/api/v1/vocabulary/apply", map[string]any{"documents": []map[string]any{
		xfDoc("substrate.reamde.dev/core/package", xhEmbedPkg, map[string]any{
			"authority": xhEmbedAuthority, "package": xhEmbedPackage, "version": 1,
		}),
		xfDoc("substrate.reamde.dev/core/kind", xhMemoKind, map[string]any{
			"authority": xhEmbedAuthority, "package": xhEmbedPackage,
			"names":           map[string]any{"singular": "memo"},
			"description":     "A memo whose text the semantic index holds.",
			"displayTemplate": "{name}",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "what the memo is called"},
				"text": map[string]any{"type": "text", "embed": true, "description": "what the memo says"},
			},
		}),
	}}, nil)
	c.requiref(status == http.StatusOK, "applying the %s package answered %d: %s", xhEmbedPkg, status, raw)
	c.putRec(providerCollection, "e2eembed", map[string]any{
		"label": "the e2e scripted stub, for embeddings", "wire": "openai",
		"baseURL": c.r.stub.url(), "apiKey": "embed-key", "embedModel": xhEmbedModel,
	})
	c.stepf("declared `%s` with an `embed: true` text property, and named the run's stub as the embeddings provider (`%s`)", xhMemoKind, xhEmbedModel)

	c.putRec("/api/v1/"+xhMemoKind, "lighthouse", map[string]any{"name": "Lighthouse", "text": xhMemoText})
	search := "/api/v1/records?" + url.Values{
		"q": {"lighthouse"}, "mode": {"semantic"},
		"filter": {`{"kinds":["` + xhMemoKind + `"]}`},
	}.Encode()

	// The stub's embeddings endpoint is closed, so the queue cannot drain:
	// the pair has no vectors and work is pending, which the semantic read
	// answers as a retryable 503 naming the count.
	status, header, raw := xeRaw(c, c.r.hc, c.r.token, http.MethodGet, search, nil, -1, nil)
	e := xeRequireError(c, "a semantic read with the write still queued", status, raw, http.StatusServiceUnavailable, "unavailable")
	c.requiref(strings.Contains(e.Error.Message, "pending in the embed queue"), "the 503 does not name the pending queue: %s", e.Error.Message)
	xeRequireRetryAfter(c, "the 503", header)
	c.stepf("with the provider refusing, a semantic read answered 503 `unavailable`: %s", e.Error.Message)

	c.r.stub.openEmbeddings()
	deadline := time.Now().Add(c.r.streamDeadline(100 * time.Second))
	var page struct {
		Records []record `json:"records"`
		Scores  map[string]struct {
			Semantic *float64 `json:"semantic"`
		} `json:"scores"`
	}
	for {
		if slices.Contains(c.r.stub.embeddedTexts(), xhMemoText) && c.r.fetch(search, &page) == nil && len(page.Records) > 0 {
			break
		}
		c.requiref(time.Now().Before(deadline), "the memo's vector did not land within %s; the drain runs once a minute", c.r.streamDeadline(100*time.Second))
		time.Sleep(time.Second)
	}
	c.stepf("the drain bought the memo's text from the stub once it answered")

	path := xhMemoKind + "/lighthouse"
	c.requiref(len(page.Records) == 1 && page.Records[0].ID == "lighthouse", "the semantic read ranks %d records, want the memo alone", len(page.Records))
	score := page.Scores[path].Semantic
	c.requiref(score != nil && *score > 0.999, "the memo's semantic score is %v, want the stub vectors' similarity of 1", score)
	c.stepf("the semantic read ranks `%s` with semantic score %.3f: the stored vector is the stub's", path, *score)
}
