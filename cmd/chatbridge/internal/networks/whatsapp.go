package networks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	whatsapp "go.mau.fi/mautrix-whatsapp/pkg/connector"
	"go.mau.fi/mautrix-whatsapp/pkg/waid"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/localmatrix"
)

// The WhatsApp connector's own state error codes the contract reads.
const (
	waTemporaryBan   = "wa-temporary-ban"
	waPhoneOffline   = "wa-phone-offline"
	waClientOutdated = "wa-client-outdated"
)

func whatsAppSpec() Spec {
	return Spec{
		ID:          "whatsapp",
		DisplayName: "WhatsApp",
		Flows:       []string{whatsapp.LoginFlowIDQR, whatsapp.LoginFlowIDPhone},
		New:         newWhatsApp,
		TxnID:       WhatsAppTxnID,
	}
}

// WhatsAppTxnID is an action's WhatsApp message id: WhatsApp's message id is
// chosen by the sender, and the connector takes a transaction id as the
// message id only when it has WhatsApp Web's prefix, so the action id is
// hashed into that shape.
func WhatsAppTxnID(action string) string {
	sum := sha256.Sum256([]byte("chatbridge/action/" + action))
	return whatsmeow.WebMessageIDPrefix + strings.ToUpper(hex.EncodeToString(sum[:9]))
}

func newWhatsApp(opts Options) (bridgev2.NetworkConnector, localmatrix.Network, error) {
	wa := &whatsapp.WhatsAppConnector{}
	if err := loadExample(wa); err != nil {
		return nil, nil, err
	}
	cfg := &wa.Config
	// The name the owner sees under Linked devices.
	cfg.OSName = opts.DeviceName
	cfg.BrowserName = "unknown"
	// The person's own name; the owner's address book name travels apart.
	cfg.DisplaynameTemplate = `{{or .BusinessName .PushName .Phone .RedactedPhone ""}}`
	cfg.CallStartNotices = false
	cfg.IdentityChangeNotices = false
	// Presence stays unavailable, so the owner's phone keeps notifying.
	cfg.SendPresenceOnTyping = false
	cfg.EnableStatusBroadcast = false
	cfg.DisableStatusBroadcastSend = true
	cfg.PinnedTag = event.RoomTagFavourite
	cfg.ArchiveTag = event.RoomTagLowPriority
	cfg.URLPreviews = false
	cfg.ForceActiveDeliveryReceipts = false
	// Retry receipts for messages sent before a restart still work.
	cfg.UseWhatsAppRetryStore = true
	// No converter binaries in the image: stickers stay as WhatsApp sent them.
	cfg.AnimatedSticker.Target = "disable"
	cfg.HistorySync.BackwardsOnDemand = true
	switch opts.History {
	case HistoryYear:
		cfg.HistorySync.RequestFullSync = true
	case HistoryAll:
		cfg.HistorySync.RequestFullSync = true
		cfg.HistorySync.FullSyncConfig.DaysLimit = 3650
		cfg.HistorySync.FullSyncConfig.SizeLimit = 10240
		cfg.HistorySync.FullSyncConfig.StorageQuota = 10240
	default:
		cfg.HistorySync.RequestFullSync = false
	}
	if err := cfg.PostProcess(); err != nil {
		return nil, nil, err
	}
	return wa, waHooks{}, nil
}

// waHooks is what only WhatsApp knows.
type waHooks struct{}

var _ localmatrix.Network = waHooks{}

func waClient(login *bridgev2.UserLogin) *whatsapp.WhatsAppClient {
	if login == nil {
		return nil
	}
	c, _ := login.Client.(*whatsapp.WhatsAppClient)
	if c == nil || c.Client == nil || c.Client.Store == nil {
		return nil
	}
	return c
}

// UserExtras reads the owner's address book on the phone, which WhatsApp
// shares with linked devices.
func (waHooks) UserExtras(ctx context.Context, login *bridgev2.UserLogin, user networkid.UserID) localmatrix.Extras {
	c := waClient(login)
	if c == nil {
		return localmatrix.Extras{}
	}
	jid := waid.ParseUserID(user)
	contact, err := c.GetStore().Contacts.GetContact(ctx, jid)
	if err != nil {
		return localmatrix.Extras{}
	}
	if jid.Server == types.HiddenUserServer && contact.FullName == "" && contact.FirstName == "" {
		if alt, err := c.GetStore().GetAltJID(ctx, jid); err == nil && !alt.IsEmpty() {
			if altContact, err := c.GetStore().Contacts.GetContact(ctx, alt); err == nil {
				contact = altContact
			}
		}
	}
	name := contact.FullName
	if name == "" {
		name = contact.FirstName
	}
	return localmatrix.Extras{Known: true, ContactName: name, IsContact: name != ""}
}

// SelfUserID is the owner's LID, else the phone number id.
func (waHooks) SelfUserID(login *bridgev2.UserLogin) networkid.UserID {
	c := waClient(login)
	if c == nil {
		if login != nil {
			return networkid.UserID(login.ID)
		}
		return ""
	}
	if lid := c.GetLID(); !lid.IsEmpty() {
		return waid.MakeUserID(lid)
	}
	return waid.MakeUserID(c.JID)
}

// StateWarnings reads the connector's error codes: a temporary ban stops the
// account, a phone not seen for twelve days is a warning (WhatsApp unlinks
// at fourteen), and a reachout timelock stops messaging until it ends.
func (waHooks) StateWarnings(state *status.BridgeState, st localmatrix.AccountStatus, now time.Time) localmatrix.AccountStatus {
	switch string(state.Error) {
	case waTemporaryBan:
		st.State = contract.StateBanned
		st.Warnings = append(st.Warnings, contract.Warning{Code: contract.WarningTemporaryBan, Message: messageOr(state.Message, "WhatsApp temporarily banned this account")})
	case waPhoneOffline:
		st.State = contract.StateConnected
		st.Message = ""
		st.Warnings = append(st.Warnings, contract.Warning{
			Code:    contract.WarningPhoneOffline,
			Message: "The phone has not been seen in over 12 days; WhatsApp unlinks linked devices after 14. Open WhatsApp on the phone.",
		})
	case waClientOutdated:
		st.Warnings = append(st.Warnings, contract.Warning{
			Code:    contract.WarningClientOutdated,
			Message: "WhatsApp refused this version of the bridge as outdated; update the bridge.",
		})
	}
	if until, ok := state.Info["reachout_timelock_until"].(time.Time); ok && until.After(now) {
		st.Warnings = append(st.Warnings, contract.Warning{
			Code:    contract.WarningReachoutTimelock,
			Message: "WhatsApp limited messaging new contacts from this account", Until: contract.AtPtr(until),
		})
	}
	return st
}

func messageOr(m, fallback string) string {
	if m != "" {
		return m
	}
	return fallback
}

// SentMessageIDs are the ids the connector gives a message sent under a
// transaction id: the chat, the owner's LID or phone number, and the id.
func (waHooks) SentMessageIDs(login *bridgev2.UserLogin, chat, txn string) []networkid.MessageID {
	c := waClient(login)
	chatJID, err := waid.ParsePortalID(networkid.PortalID(chat))
	if c == nil || err != nil {
		return nil
	}
	var ids []networkid.MessageID
	for _, sender := range []types.JID{c.GetLID(), c.JID} {
		if !sender.IsEmpty() {
			ids = append(ids, waid.MakeMessageID(chatJID, sender, txn))
		}
	}
	return ids
}

// ChatCategory reads a WhatsApp channel from its JID's server.
func (waHooks) ChatCategory(portalID string) string {
	if strings.HasSuffix(portalID, "@"+types.NewsletterServer) {
		return contract.CategoryChannel
	}
	return ""
}

// Classify reads whatsmeow's errors: a send that never got its ack may have
// been delivered, a socket that was not open sent nothing.
func (waHooks) Classify(err error) (localmatrix.Outcome, bool) {
	switch {
	case errors.Is(err, whatsmeow.ErrMessageTimedOut):
		return localmatrix.Outcome{Outcome: contract.OutcomeUncertain, ErrorCode: contract.ErrNetwork, ErrorText: err.Error()}, true
	case errors.Is(err, whatsmeow.ErrNotConnected), errors.Is(err, whatsmeow.ErrNotLoggedIn), errors.Is(err, whatsmeow.ErrIQTimedOut):
		return localmatrix.Outcome{Outcome: contract.OutcomeFailed, ErrorCode: contract.ErrNetwork, ErrorText: err.Error()}, true
	case errors.Is(err, whatsmeow.ErrNoPermissionInGroup), errors.Is(err, whatsmeow.ErrNotInGroup):
		return localmatrix.Outcome{Outcome: contract.OutcomeFailed, ErrorCode: contract.ErrRejected, ErrorText: err.Error()}, true
	case errors.Is(err, whatsmeow.ErrBroadcastListUnsupported):
		return localmatrix.Outcome{Outcome: contract.OutcomeFailed, ErrorCode: contract.ErrUnsupported, ErrorText: err.Error()}, true
	}
	return localmatrix.Outcome{}, false
}
