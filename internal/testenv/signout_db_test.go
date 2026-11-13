package testenv_test

// A stream ends with its token: when the token is revoked, and when its
// person signs out everywhere, whatever kind of token opened it. A stream on
// a token that survives runs on.

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testenv"
)

const signoutPassword = "correct-horse-battery-staple"

// streamFrames opens one watch under token and reports each line it reads:
// a row's record id, or the code of the terminal error frame.
type streamFrames struct {
	rows  chan string
	ended chan string
}

func openStream(t *testing.T, e *testenv.Env, ctx context.Context, path string) *streamFrames {
	t.Helper()
	resp := e.Request(ctx, http.MethodGet, path, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open %s: %d", path, resp.StatusCode)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	s := &streamFrames{rows: make(chan string, 16), ended: make(chan string, 1)}
	opened := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
		first := true
		for sc.Scan() {
			if first {
				close(opened)
				first = false
			}
			var line struct {
				Seq      int64  `json:"seq"`
				RecordID string `json:"recordId"`
				Error    *struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if json.Unmarshal(sc.Bytes(), &line) != nil {
				continue
			}
			switch {
			case line.Error != nil:
				s.ended <- line.Error.Code
				return
			case line.Seq > 0:
				s.rows <- line.RecordID
			}
		}
		if first {
			close(opened)
		}
		s.ended <- ""
	}()
	select {
	case <-opened:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s sent no bookmark", path)
	}
	return s
}

func (s *streamFrames) wantEnded(t *testing.T, what string) {
	t.Helper()
	select {
	case code := <-s.ended:
		if code != "auth" {
			t.Fatalf("%s ended with %q, want an auth frame", what, code)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not end", what)
	}
}

func (s *streamFrames) wantRow(t *testing.T, id, what string) {
	t.Helper()
	select {
	case got := <-s.rows:
		if id != "" && got != id {
			t.Fatalf("%s delivered %s, want %s", what, got, id)
		}
	case code := <-s.ended:
		t.Fatalf("%s ended with %q, want it running", what, code)
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never delivered %s", what, id)
	}
}

func mintFull(t *testing.T, e *testenv.Env, label string, scopes []string) substrate.MintedToken {
	t.Helper()
	body := map[string]any{"label": label}
	if scopes != nil {
		body["scopes"] = scopes
	}
	var out substrate.MintedToken
	mustDecode(t, e, http.MethodPost, "/tokens", body, &out)
	return out
}

func notesWatch() string {
	filter, _ := json.Marshal(map[string]any{"kinds": []string{detailsRef + "/note"}})
	return "/api/v1/records?watch=1&filter=" + url.QueryEscape(string(filter))
}

func notesFeed() string {
	return "/api/v1/changes?watch=1&kinds=" + url.QueryEscape(detailsRef+"/note")
}

func putNote(t *testing.T, e *testenv.Env, n int) {
	t.Helper()
	if status, body := e.Do(http.MethodPut, detailNotesPath+"/"+noteID(n),
		map[string]any{"properties": map[string]any{"name": noteID(n)}}); status/100 != 2 {
		t.Fatalf("put %d: %d %s", n, status, body)
	}
}

// A revoked full token's streams end, on the records route and on the
// change feed; another token's stream runs on.
func TestAStreamEndsWithItsRevokedToken(t *testing.T) {
	e := testenv.Start(t, testenv.WithRepository(detailsAuthority))
	e.ApplyVocabularyYAML(detailsVocabulary("string"))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	doomed := mintFull(t, e, "doomed", nil)
	kept := mintFull(t, e, "kept", nil)

	onRecords := openStream(t, e.WithToken(doomed.Secret), ctx, notesWatch())
	onFeed := openStream(t, e.WithToken(doomed.Secret), ctx, notesFeed())
	survivor := openStream(t, e.WithToken(kept.Secret), ctx, notesWatch())

	if status, body := e.Do(http.MethodDelete, "/tokens/"+doomed.Token.ID, nil); status != http.StatusOK {
		t.Fatalf("revoke: %d %s", status, body)
	}
	onRecords.wantEnded(t, "the revoked token's records watch")
	onFeed.wantEnded(t, "the revoked token's change feed")

	putNote(t, e, 1)
	survivor.wantRow(t, noteID(1), "another token's watch")
}

// Signing out everywhere ends every token the person holds and every stream
// they opened, a sign-in's, a minted one's and an assistant's token alike;
// another person's stream runs on. A bearer token alone cannot do it.
func TestSigningOutEverywhereEndsEveryToken(t *testing.T) {
	clock := &engine.TestClock{}
	e := testenv.Start(t, testenv.WithRepository(detailsAuthority), testenv.WithClock(clock.Now))
	e.ApplyVocabularyYAML(detailsVocabulary("string"))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	minted := mintFull(t, e, "minted", nil)
	assistant := mintFull(t, e, "assistant", []string{"mcp"})
	signIn := e.Token

	other := e.RegisterUser("someone-else", signoutPassword, "")
	others := openStream(t, other, ctx, "/api/v1/changes?watch=1")

	ownSignIn := openStream(t, e, ctx, notesWatch())
	ownMinted := openStream(t, e.WithToken(minted.Secret), ctx, notesFeed())

	// The factors are the evidence; a token is refused. The clock moves past
	// the door's pacing before each attempt.
	anonymous := map[string]string{"Content-Type": "application/json", "Authorization": ""}
	clock.Advance(engine.TOTPPeriod)
	if status, _, _ := e.DoRaw(http.MethodPost, "/signout/everywhere",
		[]byte(`{"repository":"`+e.Repository+`"}`), map[string]string{"Content-Type": "application/json"}); status != http.StatusForbidden {
		t.Fatalf("a sign out everywhere with a bearer and no password answered %d, want 403", status)
	}
	clock.Advance(engine.TOTPPeriod)
	body, _ := json.Marshal(map[string]string{"repository": e.Repository, "password": signoutPassword, "totpCode": e.TOTPCode()})
	status, raw, _ := e.DoRaw(http.MethodPost, "/signout/everywhere", body, anonymous)
	var answer substrate.SessionCredential
	if err := json.Unmarshal(raw, &answer); status != http.StatusOK || err != nil || answer.SignedOut == nil || *answer.SignedOut < 3 {
		t.Fatalf("sign out everywhere answered %d %s, want 200 with the three tokens and more ended", status, raw)
	}

	ownSignIn.wantEnded(t, "the sign-in's watch")
	ownMinted.wantEnded(t, "the minted token's change feed")
	for what, secret := range map[string]string{"the sign-in": signIn, "the minted token": minted.Secret, "the assistant's token": assistant.Secret} {
		if status, _ := e.WithToken(secret).Do(http.MethodGet, "/api/v1/records?filter=%7B%7D", nil); status != http.StatusUnauthorized {
			t.Fatalf("%s after the sign-out answered %d, want 401", what, status)
		}
	}

	// The other person's own write reaches their stream: a token they mint.
	mintFull(t, other, "after", nil)
	others.wantRow(t, "", "another person's change feed")
}

// A password change that asks to sign out everywhere in the same request,
// and the new password signs in.
func TestAPasswordChangeCanSignOutEverywhere(t *testing.T) {
	clock := &engine.TestClock{}
	e := testenv.Start(t, testenv.WithRepository(detailsAuthority), testenv.WithClock(clock.Now))
	old := e.Token
	clock.Advance(engine.TOTPPeriod)
	body, _ := json.Marshal(map[string]any{
		"repository": e.Repository, "password": signoutPassword, "totpCode": e.TOTPCode(),
		"newPassword": "a-new-and-longer-password", "signOutEverywhere": true,
	})
	status, raw, _ := e.DoRaw(http.MethodPost, "/password", body, map[string]string{"Content-Type": "application/json", "Authorization": ""})
	var answer substrate.SessionCredential
	if err := json.Unmarshal(raw, &answer); status != http.StatusOK || err != nil || answer.SignedOut == nil || *answer.SignedOut < 1 {
		t.Fatalf("the password change answered %d %s, want 200 with the sign-in ended", status, raw)
	}
	if status, _ := e.WithToken(old).Do(http.MethodGet, "/tokens", nil); status != http.StatusUnauthorized {
		t.Fatalf("the old sign-in answered %d, want 401", status)
	}
	clock.Advance(engine.TOTPPeriod)
	if status, raw := e.Login(e.Repository, "a-new-and-longer-password", e.TOTPCode()); status != http.StatusCreated {
		t.Fatalf("the new password does not sign in: %d %s", status, raw)
	}
}

// A password change whose write is in doubt runs no sign-out and says the
// password may have changed, never that it did. Its commit landed, so the
// new password signs in and finishes the sign-out.
func TestAPasswordChangeInDoubtSaysThePasswordMayHaveChanged(t *testing.T) {
	// The request's first commit spends the code; its second is the
	// password write, whose answer is lost.
	var armed atomic.Bool
	var commits atomic.Int32
	lose := func() bool { return armed.Load() && commits.Add(1) == 2 }
	clock := &engine.TestClock{}
	e := testenv.Start(t, testenv.WithRepository(detailsAuthority), testenv.WithClock(clock.Now),
		testenv.WithEngineOptions(engine.WithTestCommitInDoubt(lose)))
	clock.Advance(engine.TOTPPeriod)
	body, _ := json.Marshal(map[string]any{
		"repository": e.Repository, "password": signoutPassword, "totpCode": e.TOTPCode(),
		"newPassword": "a-new-and-longer-password", "signOutEverywhere": true,
	})
	armed.Store(true)
	status, raw, _ := e.DoRaw(http.MethodPost, "/password", body, map[string]string{"Content-Type": "application/json", "Authorization": ""})
	armed.Store(false)
	if commits.Load() != 2 {
		t.Fatalf("the password change made %d commits, want 2: the code's, then the password's", commits.Load())
	}
	var refused substrate.ErrorEnvelope
	if err := json.Unmarshal(raw, &refused); err != nil || status != http.StatusInternalServerError ||
		refused.Error.Code != "internal" || refused.Error.Reason != substrate.ReasonInDoubt ||
		!strings.Contains(refused.Error.Message, "the password may have changed") {
		t.Fatalf("a password change in doubt answered %d %s, want 500 internal in_doubt saying the password may have changed", status, raw)
	}
	if status, _ := e.Do(http.MethodGet, "/tokens", nil); status != http.StatusOK {
		t.Fatalf("the sign-in answered %d after a change in doubt, want 200: no sign-out ran", status)
	}

	clock.Advance(engine.TOTPPeriod)
	signout, _ := json.Marshal(map[string]string{"repository": e.Repository, "password": "a-new-and-longer-password", "totpCode": e.TOTPCode()})
	status, raw, _ = e.DoRaw(http.MethodPost, "/signout/everywhere", signout, map[string]string{"Content-Type": "application/json", "Authorization": ""})
	var answer substrate.SessionCredential
	if err := json.Unmarshal(raw, &answer); status != http.StatusOK || err != nil || answer.SignedOut == nil || *answer.SignedOut < 1 {
		t.Fatalf("signing out with the new password answered %d %s, want 200 with the sign-in ended", status, raw)
	}
	if status, _ := e.Do(http.MethodGet, "/tokens", nil); status != http.StatusUnauthorized {
		t.Fatalf("the sign-in answered %d after the sign-out, want 401", status)
	}
}

// A mint authenticated before a sign out everywhere, whose body arrives
// after it, mints nothing: the mint stands on the token that asked for it.
func TestAMintWhoseBodyArrivesAfterTheSignOutMintsNothing(t *testing.T) {
	clock := &engine.TestClock{}
	e := testenv.Start(t, testenv.WithRepository(detailsAuthority), testenv.WithClock(clock.Now))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// The server asks for the body (100 Continue) at the handler's first
	// read, after authentication, and the client sends none before: the
	// pipe's first write returning is the barrier.
	client := &http.Client{Transport: &http.Transport{ExpectContinueTimeout: time.Minute}}
	defer client.CloseIdleConnections()
	body, finish := io.Pipe()
	type answer struct {
		status int
		raw    []byte
	}
	answered := make(chan answer, 1)
	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.URL+"/tokens", body)
		if err != nil {
			t.Error(err)
			answered <- answer{}
			return
		}
		req.Header.Set("Authorization", "Bearer "+e.Token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Expect", "100-continue")
		resp, err := client.Do(req)
		if err != nil {
			t.Error(err)
			answered <- answer{}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		answered <- answer{status: resp.StatusCode, raw: raw}
	}()
	// The headers are in and authenticated; the body is not.
	if _, err := finish.Write([]byte(`{"label":`)); err != nil {
		t.Fatal(err)
	}

	clock.Advance(engine.TOTPPeriod)
	signout, _ := json.Marshal(map[string]string{"repository": e.Repository, "password": signoutPassword, "totpCode": e.TOTPCode()})
	if status, raw, _ := e.DoRaw(http.MethodPost, "/signout/everywhere", signout,
		map[string]string{"Content-Type": "application/json", "Authorization": ""}); status != http.StatusOK {
		t.Fatalf("sign out everywhere: %d %s", status, raw)
	}
	if _, err := finish.Write([]byte(`"late"}`)); err != nil {
		t.Fatal(err)
	}
	_ = finish.Close()
	got := <-answered
	if got.status != http.StatusUnauthorized {
		t.Fatalf("the late mint answered %d %s, want 401", got.status, got.raw)
	}
}
