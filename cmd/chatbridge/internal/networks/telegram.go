package networks

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
	"time"

	telegram "go.mau.fi/mautrix-telegram/pkg/connector"
	"go.mau.fi/mautrix-telegram/pkg/connector/ids"
	"go.mau.fi/mautrix-telegram/pkg/gotd/tgerr"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/bridgeconfig"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/localmatrix"
)

// ErrTelegramUnregistered is a Telegram asked for without the operator's
// app registration.
var ErrTelegramUnregistered = errors.New("the telegram network needs an app registration: " +
	"set CHATBRIDGE_TELEGRAM_API_ID and CHATBRIDGE_TELEGRAM_API_HASH from my.telegram.org")

func telegramSpec() Spec {
	return Spec{
		ID:          "telegram",
		DisplayName: "Telegram",
		Flows:       []string{telegram.LoginFlowIDPhone, telegram.LoginFlowIDQR},
		New:         newTelegram,
		TxnID:       TelegramTxnID,
		Tune:        tuneTelegram,
	}
}

// TelegramTxnID is an action's Telegram random_id, which the connector reads
// from the transaction id as a positive decimal and Telegram deduplicates a
// send by: the action id hashed into a positive 63-bit integer.
func TelegramTxnID(action string) string {
	sum := sha256.Sum256([]byte("chatbridge/action/" + action))
	n := int64(binary.BigEndian.Uint64(sum[:8]) >> 1)
	if n == 0 {
		n = 1
	}
	return strconv.FormatInt(n, 10)
}

// telegramBackfill is how many of each chat's latest messages, and how many
// of the most recent chats, the first backfill fetches for a history depth;
// -1 is everything. Telegram serves everything, so the depth the owner chose
// is what bounds it.
func telegramBackfill(h History) (messages, createChats int) {
	switch h {
	case HistoryAll:
		return -1, -1
	case HistoryYear:
		return 5000, 200
	default:
		return 1000, 50
	}
}

// tuneTelegram bounds the first backfill by the history depth, and leaves
// an unknown error to a restart: bridgev2's automatic reconnect would open a
// session Telegram closed for being used twice (AUTH_KEY_DUPLICATED) again.
func tuneTelegram(opts Options, cfg *bridgeconfig.BridgeConfig) {
	messages, _ := telegramBackfill(opts.History)
	cfg.Backfill.Queue.BatchSize = 100
	cfg.Backfill.Queue.MaxBatches = -1
	if messages >= 0 {
		// A chat's newest messages arrive when it is created; the queue
		// pages back through the rest.
		cfg.Backfill.Queue.MaxBatches = max(0, messages-cfg.Backfill.MaxInitialMessages) / cfg.Backfill.Queue.BatchSize
	}
	cfg.UnknownErrorAutoReconnect = 0
	cfg.UnknownErrorMaxAutoReconnects = 0
}

func newTelegram(opts Options) (bridgev2.NetworkConnector, localmatrix.Network, error) {
	if !opts.Telegram.Set() {
		return nil, nil, ErrTelegramUnregistered
	}
	tc := &telegram.TelegramConnector{}
	if err := loadExample(tc); err != nil {
		return nil, nil, err
	}
	cfg := &tc.Config
	cfg.APIID, cfg.APIHash = opts.Telegram.APIID, opts.Telegram.APIHash
	// The name the owner sees under Settings, Devices.
	cfg.DeviceInfo.DeviceModel = opts.DeviceName
	// No data export: Telegram asks the owner to accept one on another
	// device, and nothing here needs it.
	cfg.Takeout.DialogSync = false
	cfg.Takeout.ForwardBackfill = false
	cfg.Takeout.BackwardBackfill = false
	// A person's name is their own; the owner's address book is not read.
	cfg.ContactNames = false
	_, createChats := telegramBackfill(opts.History)
	cfg.Sync.CreateLimit = createChats
	// The sync looks at no more chats than this, so it is never below the
	// number to create.
	if createChats < 0 || createChats > cfg.Sync.UpdateLimit {
		cfg.Sync.UpdateLimit = createChats
	}
	if err := cfg.PostProcess(); err != nil {
		return nil, nil, err
	}
	return tc, tgHooks{}, nil
}

// tgHooks is what only Telegram knows.
type tgHooks struct{}

var _ localmatrix.Network = tgHooks{}

// UserExtras reads whether a user is in the owner's contacts from the last
// profile the owner's own session saw; Telegram gives the flag, and the
// connector keeps no address book names.
func (tgHooks) UserExtras(ctx context.Context, login *bridgev2.UserLogin, user networkid.UserID) localmatrix.Extras {
	if login == nil {
		return localmatrix.Extras{}
	}
	ghost, err := login.Bridge.DB.Ghost.GetByID(ctx, user)
	if err != nil || ghost == nil {
		return localmatrix.Extras{}
	}
	meta, ok := ghost.Metadata.(*telegram.GhostMetadata)
	self, err := ids.ParseUserLoginID(login.ID)
	if !ok || err != nil || meta.ContactSource != self {
		return localmatrix.Extras{}
	}
	return localmatrix.Extras{Known: true, IsContact: meta.SourceIsContact}
}

// SelfUserID is the owner's Telegram user id, which is the login's.
func (tgHooks) SelfUserID(login *bridgev2.UserLogin) networkid.UserID {
	if login == nil {
		return ""
	}
	return ids.UserLoginIDToUserID(login.ID)
}

// StateWarnings reads a session Telegram closed for being used from two
// connections, which no reconnect brings back.
func (tgHooks) StateWarnings(state *status.BridgeState, st localmatrix.AccountStatus, _ time.Time) localmatrix.AccountStatus {
	goErr, _ := state.Info["go_error"].(string)
	if strings.Contains(state.Message, "AUTH_KEY_DUPLICATED") || strings.Contains(goErr, "AUTH_KEY_DUPLICATED") {
		st.State = contract.StateBadCredentials
		st.Message = "Telegram closed this session because it was used from two connections at once (AUTH_KEY_DUPLICATED): " +
			"another copy of this bridge store is running. Stop the other copy, then link Telegram again."
	}
	return st
}

// ChatCategory tells a broadcast channel from a supergroup, which share a
// peer type.
func (tgHooks) ChatCategory(portal *bridgev2.Portal) string {
	peer, _, topic, err := ids.ParsePortalID(portal.ID)
	if err != nil || peer != ids.PeerTypeChannel || topic != 0 {
		return ""
	}
	if meta, ok := portal.Metadata.(*telegram.PortalMetadata); ok && !meta.IsSuperGroup {
		return contract.CategoryChannel
	}
	return ""
}

// BeforeRead reports the chat viewed, as an official client does when the
// owner opens it: in a broadcast channel the connector then fetches its
// sponsored message, and the read that follows reports it viewed.
func (tgHooks) BeforeRead(ctx context.Context, login *bridgev2.UserLogin, portal *bridgev2.Portal) {
	if login == nil || portal == nil {
		return
	}
	if viewing, ok := login.Client.(bridgev2.ChatViewingNetworkAPI); ok {
		_ = viewing.HandleMatrixViewingChat(ctx, &bridgev2.MatrixViewingChat{Portal: portal})
	}
}

// peerFloodWait is how long a PEER_FLOOD stops sending. Telegram does not
// say when the limit ends (its @SpamBot tells the owner), and a day of
// silence is what keeps a limited account from being banned.
const peerFloodWait = 24 * time.Hour

// Throttle reads a FLOOD_WAIT as a wait on the account and PEER_FLOOD, the
// limit on writing to strangers, as a day's.
func (tgHooks) Throttle(err error, now time.Time) (contract.Warning, bool) {
	if d, ok := tgerr.AsFloodWait(err); ok {
		return contract.Warning{
			Code: contract.WarningFloodWait, Message: "Telegram asked this account to wait before sending again",
			Until: contract.AtPtr(now.Add(d)),
		}, true
	}
	if tgerr.Is(err, "PEER_FLOOD") {
		return contract.Warning{
			Code: contract.WarningFloodWait,
			Message: "Telegram limited this account's messages to people who have not written to it (PEER_FLOOD); " +
				"@SpamBot on Telegram says until when",
			Until: contract.AtPtr(now.Add(peerFloodWait)),
		}, true
	}
	return contract.Warning{}, false
}

// SentMessageIDs is empty: Telegram numbers a message on the server, so a
// sent message cannot be found by its random_id afterwards.
func (tgHooks) SentMessageIDs(*bridgev2.UserLogin, string, string) []networkid.MessageID { return nil }

// Classify reads Telegram's RPC errors.
func (tgHooks) Classify(err error) (localmatrix.Outcome, bool) {
	failed := func(code string) (localmatrix.Outcome, bool) {
		return localmatrix.Outcome{Outcome: contract.OutcomeFailed, ErrorCode: code, ErrorText: err.Error()}, true
	}
	switch {
	case func() bool { _, ok := tgerr.AsFloodWait(err); return ok }(), tgerr.Is(err, "PEER_FLOOD", "SLOWMODE_WAIT"):
		return failed(contract.ErrRateLimited)
	case tgerr.Is(err, "USER_IS_BLOCKED", "YOU_BLOCKED_USER", "USER_PRIVACY_RESTRICTED", "USER_RESTRICTED", "USER_BANNED_IN_CHANNEL"):
		return failed(contract.ErrBlocked)
	case tgerr.Is(err, "CHAT_WRITE_FORBIDDEN", "CHAT_ADMIN_REQUIRED", "CHAT_SEND_PLAIN_FORBIDDEN", "MESSAGE_AUTHOR_REQUIRED",
		"MESSAGE_EDIT_TIME_EXPIRED", "MESSAGE_DELETE_FORBIDDEN"):
		return failed(contract.ErrRejected)
	case tgerr.Is(err, "MESSAGE_ID_INVALID", "PEER_ID_INVALID", "CHANNEL_INVALID", "CHANNEL_PRIVATE"):
		return failed(contract.ErrNotFound)
	case tgerr.Is(err, "MESSAGE_TOO_LONG", "MESSAGE_EMPTY", "REACTION_INVALID", "MESSAGE_NOT_MODIFIED"):
		return failed(contract.ErrInvalid)
	}
	return localmatrix.Outcome{}, false
}
