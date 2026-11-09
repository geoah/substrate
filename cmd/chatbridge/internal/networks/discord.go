package networks

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	discord "go.mau.fi/mautrix-discord/pkg/connector"
	"go.mau.fi/mautrix-discord/pkg/discordauth"
	"go.mau.fi/mautrix-discord/pkg/discordid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/bridgeconfig"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/localmatrix"
)

// DiscordRule is Discord's rule this bridge breaks, which the owner accepts
// before an account is linked.
const DiscordRule = "Discord's rule: automating a user account is forbidden and can end in the account's termination.\n" +
	"This bridge signs in to Discord as you, mirrors your direct messages and sends what you ask through chat actions,\n" +
	"which is automating your account. Discord can terminate it for that, whatever the bridge does to look like its own app."

// DiscordTokenFlow is the login flow that takes the account's token.
const DiscordTokenFlow = discord.LoginFlowIDToken

// How long sending stops after Discord limits or challenges the account.
const (
	// discordMinWait is the least a rate limit stops sending for, so a
	// limit is never met again at once.
	discordMinWait = time.Minute
	// discordCaptchaWait follows a captcha, which Discord asks for when it
	// suspects automation: a day of the owner's own use, not the bridge's.
	discordCaptchaWait = 24 * time.Hour
	// discordNoMutualServer is the refusal of a DM to someone the owner
	// shares no server with.
	discordNoMutualServer = 50278
)

func discordSpec() Spec {
	return Spec{
		ID:          "discord",
		DisplayName: "Discord",
		Flows:       []string{discord.LoginFlowIDRemoteAuth, discord.LoginFlowIDToken},
		New:         newDiscord,
		TxnID:       discordNonce.next,
		Tune:        tuneDiscord,
		Acknowledge: DiscordRule,
	}
}

// discordNonce hands out the nonce Discord's own client sends with a
// message: a snowflake of the moment, never the same twice in one run. The
// claim stores it, so an action run again after a restart keeps its nonce.
var discordNonce = &nonceClock{now: time.Now}

type nonceClock struct {
	now  func() time.Time
	mu   sync.Mutex
	last int64
}

// next ignores the action: Discord's client puts the time in the nonce, and
// a nonce derived from the id would decode to no time it could have had.
func (c *nonceClock) next(string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	ms := c.now().UnixMilli() - discordid.DiscordEpochMillis
	if ms <= c.last {
		ms = c.last + 1
	}
	c.last = ms
	return strconv.FormatInt(ms<<22, 10)
}

// tuneDiscord keeps the bridge's reads to what the gateway does not bring:
// each chat's latest page when it is created, and one page after the last
// stored message when the account reconnects. Older history is not paged.
func tuneDiscord(_ Options, cfg *bridgeconfig.BridgeConfig) {
	cfg.Backfill.MaxInitialMessages = 100
	cfg.Backfill.MaxCatchupMessages = 100
	cfg.Backfill.Queue.MaxBatches = 0
}

func newDiscord(Options) (bridgev2.NetworkConnector, localmatrix.Network, error) {
	dc := &discord.DiscordConnector{}
	if err := loadExample(dc); err != nil {
		return nil, nil, err
	}
	cfg := &dc.Config
	// Direct messages and group DMs only: no server's channels.
	cfg.Guilds.BridgingGuildIDs = nil
	// allowNewChat decides who may get a first message. The connector's
	// own screen would also refuse a conversation the owner already has
	// with someone who is not a friend.
	cfg.ForbidDMingStrangers = contract.Ptr(false)
	// A custom emoji reaction arrives as its :name:, which the contract
	// carries as text.
	cfg.CustomEmojiReactions = contract.Ptr(false)
	// A message from a server arrives on the gateway and is dropped without
	// a log line each.
	cfg.LogWhenDroppingMessages = false
	cfg.ReportScrubbedAccountStanding = false
	return &discordConnector{DiscordConnector: dc, now: time.Now}, dcHooks{}, nil
}

// discordConnector is the connector with every REST response of a login
// read for the waits and challenges that stop sending on its account.
type discordConnector struct {
	*discord.DiscordConnector
	now  func() time.Time
	warn func(ctx context.Context, account string, w contract.Warning)
}

var _ warnable = (*discordConnector)(nil)

// SetWarner is where a wait or a challenge goes.
func (c *discordConnector) SetWarner(warn func(context.Context, string, contract.Warning)) {
	c.warn = warn
}

// LoadUserLogin wraps the session's response hook, which the connector sets
// for its own use, without taking it away.
func (c *discordConnector) LoadUserLogin(ctx context.Context, login *bridgev2.UserLogin) error {
	if err := c.DiscordConnector.LoadUserLogin(ctx, login); err != nil {
		return err
	}
	client, ok := login.Client.(*discord.DiscordClient)
	if !ok || client.Session == nil {
		return nil
	}
	own := client.Session.RESTResponseHook
	account := string(login.ID)
	client.Session.RESTResponseHook = func(req *http.Request, resp *http.Response, body []byte) {
		if own != nil {
			own(req, resp, body)
		}
		if w, ok := discordWait(resp, body, c.now()); ok && c.warn != nil {
			c.warn(context.Background(), account, w)
		}
	}
	return nil
}

// discordWait reads a REST response for what stops sending on the account:
// a rate limit, for as long as Discord asks and at least a minute, and a
// captcha, for a day.
func discordWait(resp *http.Response, body []byte, now time.Time) (contract.Warning, bool) {
	if resp == nil {
		return contract.Warning{}, false
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		var limit struct {
			RetryAfter float64 `json:"retry_after"`
		}
		wait := time.Duration(0)
		if json.Unmarshal(body, &limit) == nil {
			wait = time.Duration(limit.RetryAfter * float64(time.Second))
		} else if s, err := strconv.ParseFloat(resp.Header.Get("Retry-After"), 64); err == nil {
			wait = time.Duration(s * float64(time.Second))
		}
		return contract.Warning{
			Code:    contract.WarningFloodWait,
			Message: "Discord rate-limited this account; sending waits until the limit ends",
			Until:   contract.AtPtr(now.Add(max(wait, discordMinWait))),
		}, true
	case http.StatusBadRequest:
		if discordauth.CheckCaptcha(context.Background(), resp, body) != nil {
			return contract.Warning{
				Code: contract.WarningCheckpoint,
				Message: "Discord asked this account to solve a captcha, which it does when it suspects automation; " +
					"sending stops for a day. Use Discord's own app meanwhile.",
				Until: contract.AtPtr(now.Add(discordCaptchaWait)),
			}, true
		}
	}
	return contract.Warning{}, false
}

// dcHooks is what only Discord knows.
type dcHooks struct{}

var _ localmatrix.Network = dcHooks{}

func discordClient(login *bridgev2.UserLogin) *discord.DiscordClient {
	if login == nil {
		return nil
	}
	c, _ := login.Client.(*discord.DiscordClient)
	if c == nil || c.Session == nil || c.Session.State == nil {
		return nil
	}
	return c
}

// UserExtras reads the owner's friends list as Discord sent it when the
// account connected: a friend is a contact, and the nickname the owner gave
// a friend is the name they keep for them.
func (dcHooks) UserExtras(_ context.Context, login *bridgev2.UserLogin, user networkid.UserID) localmatrix.Extras {
	c := discordClient(login)
	if c == nil {
		return localmatrix.Extras{}
	}
	state := c.Session.State
	state.RLock()
	defer state.RUnlock()
	if state.User == nil {
		// Not connected yet: the friends list has not arrived.
		return localmatrix.Extras{}
	}
	id := discordid.ParseUserID(user)
	for _, rel := range state.Relationships {
		if rel != nil && rel.ID == id && rel.Type == discordgo.RelationshipFriend {
			return localmatrix.Extras{Known: true, IsContact: true, NameKnown: true, ContactName: rel.Nickname}
		}
	}
	return localmatrix.Extras{Known: true, NameKnown: true}
}

// SelfUserID is the owner's Discord user id, which is the login's.
func (dcHooks) SelfUserID(login *bridgev2.UserLogin) networkid.UserID {
	if login == nil {
		return ""
	}
	return discordid.UserLoginIDToUserID(login.ID)
}

// StateWarnings reads Discord asking the owner to act in its app (verify an
// email or a phone, a safety check, a system message): the session stays
// connected, and nothing is sent until the owner has.
func (dcHooks) StateWarnings(state *status.BridgeState, st localmatrix.AccountStatus, _ time.Time) localmatrix.AccountStatus {
	if state.StateEvent != status.StateBadCredentials || state.UserAction != status.UserActionOpenNative {
		return st
	}
	st.State = contract.StateConnected
	st.Warnings = append(st.Warnings, contract.Warning{
		Code:    contract.WarningCheckpoint,
		Message: messageOr(state.Message, "Discord needs you to act in its app before this account can send"),
	})
	st.Message = ""
	return st
}

// ChatCategory leaves direct and group DMs to their room type.
func (dcHooks) ChatCategory(*bridgev2.Portal) string { return "" }

// BeforeRead does nothing: Discord's client reports reading a chat with the
// acknowledgement the read itself sends.
func (dcHooks) BeforeRead(context.Context, *bridgev2.UserLogin, *bridgev2.Portal) {}

// Throttle is never: the session's responses carry Discord's limits and
// challenges as they happen (discordConnector).
func (dcHooks) Throttle(error, time.Time) (contract.Warning, bool) { return contract.Warning{}, false }

// SentMessageIDs is empty: Discord numbers a message on its server, so a
// sent message cannot be found by its nonce afterwards.
func (dcHooks) SentMessageIDs(*bridgev2.UserLogin, string, string) []networkid.MessageID { return nil }

// Classify reads Discord's REST errors. A request that failed on the way
// may have reached Discord, so it is uncertain.
func (dcHooks) Classify(err error) (localmatrix.Outcome, bool) {
	failed := func(code string) (localmatrix.Outcome, bool) {
		return localmatrix.Outcome{Outcome: contract.OutcomeFailed, ErrorCode: code, ErrorText: err.Error()}, true
	}
	var limited *discordgo.RateLimitError
	if errors.As(err, &limited) {
		return failed(contract.ErrRateLimited)
	}
	var rest *discordgo.RESTError
	if !errors.As(err, &rest) {
		var urlErr *url.Error
		var netErr net.Error
		if errors.As(err, &urlErr) || errors.As(err, &netErr) {
			return localmatrix.Outcome{Outcome: contract.OutcomeUncertain, ErrorCode: contract.ErrNetwork, ErrorText: err.Error()}, true
		}
		return localmatrix.Outcome{}, false
	}
	if rest.Response != nil && rest.Response.StatusCode == http.StatusTooManyRequests {
		return failed(contract.ErrRateLimited)
	}
	if rest.Message == nil {
		return localmatrix.Outcome{}, false
	}
	switch rest.Message.Code {
	case discordgo.ErrCodeCannotSendMessagesToThisUser, discordNoMutualServer, discordgo.ErrCodeReactionBlocked:
		return failed(contract.ErrBlocked)
	case discordgo.ErrCodeMissingAccess, discordgo.ErrCodeMissingPermissions, discordgo.ErrCodeActionRequiredVerifiedAccount,
		discordgo.ErrCodeCannotReplyWithoutPermissionToReadMessageHistory:
		return failed(contract.ErrRejected)
	case discordgo.ErrCodeUnknownMessage, discordgo.ErrCodeUnknownChannel:
		return failed(contract.ErrNotFound)
	case discordgo.ErrCodeInvalidFormBody, discordgo.ErrCodeCannotSendEmptyMessage, discordgo.ErrCodeRequestEntityTooLarge,
		discordgo.ErrCodeUnknownEmoji:
		return failed(contract.ErrInvalid)
	}
	return localmatrix.Outcome{}, false
}
