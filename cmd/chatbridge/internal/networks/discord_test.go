package networks

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
	discord "go.mau.fi/mautrix-discord/pkg/connector"
	"go.mau.fi/mautrix-discord/pkg/discordid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/status"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/localmatrix"
)

func TestDiscordIsOfferedBehindItsRule(t *testing.T) {
	spec, ok := Find("discord", Options{})
	if !ok {
		t.Fatal("Discord is not offered")
	}
	if !strings.Contains(spec.Acknowledge, "automating a user account is forbidden and can end in the account's termination") {
		t.Fatalf("the rule shown before linking is %q", spec.Acknowledge)
	}
	if spec.Flows[0] != discord.LoginFlowIDRemoteAuth || spec.Flows[1] != DiscordTokenFlow || len(spec.Flows) != 2 {
		t.Fatalf("flows %v: the QR first, then the token, and no password login", spec.Flows)
	}
	for _, other := range []string{"whatsapp", "telegram"} {
		if s, ok := Find(other, Options{Telegram: testApp}); !ok || s.Acknowledge != "" {
			t.Fatalf("%s asks for an acknowledgment", other)
		}
	}
}

// Discord's client sends a snowflake of the moment as a message's nonce;
// one derived from the action would decode to a time it could not have.
func TestDiscordNoncesAreTheClientsSnowflakes(t *testing.T) {
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	c := &nonceClock{now: func() time.Time { return at }}
	first, second := c.next("act-1"), c.next("act-2")
	n1, err1 := strconv.ParseInt(first, 10, 64)
	n2, err2 := strconv.ParseInt(second, 10, 64)
	if err1 != nil || err2 != nil {
		t.Fatalf("nonces %q %q are not snowflakes", first, second)
	}
	if n1&(1<<22-1) != 0 {
		t.Fatalf("nonce %d carries more than a time, unlike the client's", n1)
	}
	if ts, _ := discordgo.SnowflakeTimestamp(first); !ts.Equal(at) {
		t.Fatalf("nonce %s is from %s, not %s", first, ts, at)
	}
	if n2 <= n1 {
		t.Fatalf("two actions in one millisecond share or reverse a nonce: %d, %d", n1, n2)
	}
	if len(discordid.GenerateNonce()) != len(first) {
		t.Fatalf("nonce %s is not shaped like the connector's %s", first, discordid.GenerateNonce())
	}
}

func TestDiscordConnectorConfiguration(t *testing.T) {
	conn, _, err := newDiscord(Options{})
	if err != nil {
		t.Fatal(err)
	}
	c := conn.(*discordConnector).Config
	if len(c.Guilds.BridgingGuildIDs) != 0 {
		t.Fatal("a server's channels are bridged")
	}
	if c.ForbidDMingStrangersEnabled() || c.CustomEmojiReactionsEnabled() || c.LogWhenDroppingMessages || c.ReportScrubbedAccountStanding {
		t.Fatalf("connector configuration %+v", c)
	}
	cfg := BridgeConfig()
	tuneDiscord(Options{History: HistoryAll}, cfg)
	if cfg.Backfill.MaxInitialMessages != 100 || cfg.Backfill.MaxCatchupMessages != 100 || cfg.Backfill.Queue.MaxBatches != 0 {
		t.Fatalf("backfill %+v pages more than one page of a chat", cfg.Backfill)
	}
}

func TestDiscordReadsTheChecksItAsksTheOwnerFor(t *testing.T) {
	h := dcHooks{}
	now := time.Now()
	check := h.StateWarnings(&status.BridgeState{
		StateEvent: status.StateBadCredentials, UserAction: status.UserActionOpenNative,
		Error: discord.DCRequireVerifiedPhone, Message: "Please use the Discord app to add a verified phone number to your account.",
	}, localmatrix.AccountStatus{State: contract.StateBadCredentials, Message: "x"}, now)
	if check.State != contract.StateConnected || check.Message != "" || len(check.Warnings) != 1 ||
		check.Warnings[0].Code != contract.WarningCheckpoint || !strings.Contains(check.Warnings[0].Message, "phone number") {
		t.Fatalf("a check in Discord's app reads %+v", check)
	}
	if stopped, _ := check.Stopped(now); !stopped {
		t.Fatal("a check in Discord's app does not stop sending")
	}
	relink := h.StateWarnings(&status.BridgeState{
		StateEvent: status.StateBadCredentials, UserAction: status.UserActionRelogin, Error: discord.DCWebsocketDisconnect4004,
	}, localmatrix.AccountStatus{State: contract.StateBadCredentials}, now)
	if relink.State != contract.StateBadCredentials || len(relink.Warnings) != 0 {
		t.Fatalf("a token Discord revoked reads %+v", relink)
	}
}

func TestDiscordWaitsOnRateLimitsAndCaptchas(t *testing.T) {
	now := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		status int
		header http.Header
		body   string
		code   string
		until  time.Duration
	}{
		{"long limit", http.StatusTooManyRequests, nil, `{"message":"You are being rate limited.","retry_after":120.5,"global":false}`, contract.WarningFloodWait, 120500 * time.Millisecond},
		{"short limit", http.StatusTooManyRequests, nil, `{"retry_after":0.35}`, contract.WarningFloodWait, time.Minute},
		{"limit from the edge", http.StatusTooManyRequests, http.Header{"Retry-After": {"300"}}, `<html>error 1015</html>`, contract.WarningFloodWait, 5 * time.Minute},
		{"captcha", http.StatusBadRequest, nil, `{"captcha_key":["captcha-required"],"captcha_sitekey":"x","captcha_service":"hcaptcha"}`, contract.WarningCheckpoint, 24 * time.Hour},
		{"plain refusal", http.StatusBadRequest, nil, `{"code":50035,"message":"Invalid Form Body"}`, "", 0},
		{"success", http.StatusOK, nil, `{}`, "", 0},
	}
	for _, c := range cases {
		w, ok := discordWait(&http.Response{StatusCode: c.status, Header: c.header}, []byte(c.body), now)
		if ok != (c.code != "") || w.Code != c.code {
			t.Fatalf("%s: %+v %v", c.name, w, ok)
		}
		if ok && (w.Until == nil || *w.Until != contract.At(now.Add(c.until))) {
			t.Fatalf("%s: until %v, not %s on", c.name, w.Until, c.until)
		}
	}
}

// The real connector loads a login offline; the session it builds then
// reports Discord's limits and challenges on the account, and the
// connector's own reading of its responses still runs.
func TestDiscordSessionResponsesStopSendingOnTheAccount(t *testing.T) {
	run, err := Open(discordSpec(), tempStore(t), nil, Options{DeviceName: "Substrate"}, slog.New(slog.DiscardHandler), zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	defer run.Bridge.DB.Close()
	conn := run.Bridge.Network.(*discordConnector)
	now := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	conn.now = func() time.Time { return now }
	type warned struct {
		account string
		w       contract.Warning
	}
	var got []warned
	conn.SetWarner(func(_ context.Context, account string, w contract.Warning) { got = append(got, warned{account, w}) })

	login := &bridgev2.UserLogin{
		UserLogin: &database.UserLogin{ID: "80351110224678912", Metadata: &discordid.UserLoginMetadata{Token: "test-token-not-a-real-one"}},
		Bridge:    run.Bridge,
		Log:       zerolog.Nop(),
	}
	if err := conn.LoadUserLogin(context.Background(), login); err != nil {
		t.Fatal(err)
	}
	session := login.Client.(*discord.DiscordClient).Session
	req := httptest.NewRequest(http.MethodPost, "https://discord.com/api/v9/channels/1/messages", nil)
	session.RESTResponseHook(req, &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}, []byte(`{"retry_after":90}`))
	session.RESTResponseHook(req, &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{}}, []byte(`{"captcha_key":["captcha-required"]}`))
	session.RESTResponseHook(req, &http.Response{StatusCode: http.StatusOK, Header: http.Header{}}, []byte(`{}`))
	if len(got) != 2 || got[0].account != "80351110224678912" || got[0].w.Code != contract.WarningFloodWait ||
		*got[0].w.Until != contract.At(now.Add(90*time.Second)) || got[1].w.Code != contract.WarningCheckpoint {
		t.Fatalf("warnings %+v", got)
	}
}

func TestDiscordStartsWithoutALogin(t *testing.T) {
	run, err := Open(discordSpec(), tempStore(t), nil, Options{DeviceName: "Substrate"}, slog.New(slog.DiscardHandler), zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	run.Stop(5 * time.Second)
}

func TestDiscordFriendsAreContacts(t *testing.T) {
	session, err := discordgo.New("")
	if err != nil {
		t.Fatal(err)
	}
	login := &bridgev2.UserLogin{Client: &discord.DiscordClient{Session: session}}
	h := dcHooks{}
	if x := h.UserExtras(context.Background(), login, "2"); x.Known {
		t.Fatalf("before the friends list arrived, a user reads %+v", x)
	}
	session.State.User = &discordgo.User{ID: "1"}
	session.State.Relationships = []*discordgo.Relationship{
		{ID: "2", Type: discordgo.RelationshipFriend, Nickname: "Ada from work"},
		{ID: "3", Type: discordgo.RelationshipFriend},
		{ID: "4", Type: discordgo.RelationshipBlocked, Nickname: "spam"},
		{ID: "5", Type: discordgo.RelationshipIncomingFriendRequest},
	}
	want := map[string]localmatrix.Extras{
		"2": {Known: true, IsContact: true, NameKnown: true, ContactName: "Ada from work"},
		"3": {Known: true, IsContact: true, NameKnown: true},
		"4": {Known: true, NameKnown: true},
		"5": {Known: true, NameKnown: true},
		"6": {Known: true, NameKnown: true},
	}
	for id, w := range want {
		if x := h.UserExtras(context.Background(), login, discordid.MakeUserID(id)); x != w {
			t.Fatalf("user %s reads %+v, not %+v", id, x, w)
		}
	}
	if h.SelfUserID(&bridgev2.UserLogin{UserLogin: &database.UserLogin{ID: "1"}}) != "1" {
		t.Fatal("the owner's own id is not the login's")
	}
}

func TestDiscordErrors(t *testing.T) {
	h := dcHooks{}
	// discordgo returns its REST errors as pointers.
	rest := func(status, code int) error {
		return &discordgo.RESTError{
			Response: &http.Response{StatusCode: status, Status: strconv.Itoa(status)},
			Message:  &discordgo.APIErrorMessage{Code: code},
		}
	}
	cases := []struct {
		err  error
		want string
	}{
		{rest(403, discordgo.ErrCodeCannotSendMessagesToThisUser), contract.ErrBlocked},
		{rest(400, discordNoMutualServer), contract.ErrBlocked},
		{rest(403, discordgo.ErrCodeMissingPermissions), contract.ErrRejected},
		{rest(403, discordgo.ErrCodeActionRequiredVerifiedAccount), contract.ErrRejected},
		{rest(404, discordgo.ErrCodeUnknownMessage), contract.ErrNotFound},
		{rest(400, discordgo.ErrCodeInvalidFormBody), contract.ErrInvalid},
		{rest(400, discordgo.ErrCodeUnknownEmoji), contract.ErrInvalid},
		{rest(429, 0), contract.ErrRateLimited},
		// The connector wraps an error it explains in a message status.
		{bridgev2.WrapErrorInStatus(rest(404, discordgo.ErrCodeUnknownChannel)), contract.ErrNotFound},
	}
	for _, c := range cases {
		if o, ok := h.Classify(c.err); !ok || o.Outcome != contract.OutcomeFailed || o.ErrorCode != c.want {
			t.Fatalf("%v reads %+v %v, not %s", c.err, o, ok, c.want)
		}
	}
	lost := &url.Error{Op: "Post", URL: "https://discord.com/api/v9/channels/1/messages", Err: errors.New("connection reset by peer")}
	if o, ok := h.Classify(lost); !ok || o.Outcome != contract.OutcomeUncertain {
		t.Fatalf("a request lost on the way reads %+v %v", o, ok)
	}
	if _, ok := h.Classify(errors.New("something else")); ok {
		t.Fatal("an error Discord did not give is classified")
	}
}

// Discord runs by default only once a link was tried, which needs its rule
// accepted first.
func TestDiscordIsNotRunUntilLinked(t *testing.T) {
	st := tempStore(t)
	spec := discordSpec()
	if Linked(st, spec) {
		t.Fatal("a fresh store has Discord linked")
	}
	run, err := Open(spec, st, nil, Options{DeviceName: "Substrate"}, slog.New(slog.DiscardHandler), zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	if err := run.StartConnectorsOnly(context.Background()); err != nil {
		t.Fatal(err)
	}
	run.Stop(5 * time.Second)
	if !Linked(st, spec) {
		t.Fatal("a store Discord ran on does not have it linked")
	}
}
