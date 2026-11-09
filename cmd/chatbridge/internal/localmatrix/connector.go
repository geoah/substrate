// Package localmatrix is the Matrix side bridgev2 talks to, with no Matrix
// server behind it. Every room, member, profile, message, reaction, edit,
// redaction, read marker and file bridgev2 would send to Matrix lands in the
// bridge store instead, and each change is appended to the outbox as events
// of the contract, inside the call that made it: bridgev2 handles a network
// event synchronously (the portal event buffer is 0), so the event is
// durable before the network is acknowledged.
//
// It is written for this program. Courier's localmatrix and nook-chatd's
// matrix.go (both AGPL-3.0) were read for the pitfalls they document; no code
// is taken from them.
package localmatrix

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

// ServerName is the server every local Matrix id names. It is reserved
// (RFC 2606), so no id the bridge mints names a real host.
const ServerName = "chatbridge.invalid"

// OwnerMXID is the one local user: the owner, whom bridgev2 sees as a double
// puppet, so the owner's own messages from other devices arrive as theirs.
const OwnerMXID = id.UserID("@owner:" + ServerName)

// The owner's media policy for an account (the account's downloadMedia).
const (
	MediaNone  = "none"
	MediaSmall = "small"
	MediaAll   = "all"
)

// Settings reads what the owner set on an account.
type Settings interface {
	// MediaPolicy is the account's downloadMedia, MediaSmall when unset.
	MediaPolicy(network, account string) string
}

// Extras are what only one network knows about a user: whether they are in
// the owner's address book there, and under which name. Known is false when
// the network cannot tell, which leaves both fields as the server has them.
type Extras struct {
	Known     bool
	IsContact bool
	// NameKnown says ContactName is the address book's name, or that there
	// is none when empty; a network that knows who is a contact but not
	// under which name leaves it false.
	NameKnown   bool
	ContactName string
	Handle      string
}

// Network is what the local Matrix side needs of one network beyond
// bridgev2's interfaces.
type Network interface {
	// UserExtras reads the owner's address book entry for a user.
	UserExtras(ctx context.Context, login *bridgev2.UserLogin, user networkid.UserID) Extras
	// SelfUserID is the owner's own user id on the network for a login.
	SelfUserID(login *bridgev2.UserLogin) networkid.UserID
	// StateWarnings reads the network's own error codes in a login's state:
	// a ban, a limit, a phone not seen.
	StateWarnings(state *status.BridgeState, st AccountStatus, now time.Time) AccountStatus
	// ChatCategory names a chat's category where only the network can tell
	// (a channel), else "".
	ChatCategory(portal *bridgev2.Portal) string
	// BeforeRead runs before a markread action reaches the network: what an
	// official client does when the owner opens the chat.
	BeforeRead(ctx context.Context, login *bridgev2.UserLogin, portal *bridgev2.Portal)
	// Throttle reads an action's error as the network telling the account
	// to stop sending for a while, as a warning on the account.
	Throttle(err error, now time.Time) (contract.Warning, bool)
	// SentMessageIDs are the ids a message the owner sent under a
	// transaction id may carry: on a network where the sender chooses the
	// id, the transaction id is in it.
	SentMessageIDs(login *bridgev2.UserLogin, chat, txn string) []networkid.MessageID
	// Classify reads the network's own errors from an action: whether
	// anything left (failed) or the network stopped answering mid-send
	// (uncertain). ok is false for an error it does not know.
	Classify(err error) (out Outcome, ok bool)
}

// StatusListener hears about each account's state and each Matrix event's
// outcome, for the action runner.
type StatusListener interface {
	AccountState(network, account string, state AccountStatus)
	MessageStatus(network string, source id.EventID, st *bridgev2.MessageStatus, info *bridgev2.MessageStatusEventInfo)
}

// Connector implements bridgev2.MatrixConnector over the store, for one
// network.
type Connector struct {
	network  string
	store    *store.Store
	settings Settings
	net      Network
	log      *slog.Logger

	br       *bridgev2.Bridge
	listener atomic.Pointer[StatusListener]

	roomsMu sync.Mutex
	rooms   map[id.RoomID]networkid.PortalKey

	stateMu sync.Mutex
	states  map[networkid.UserLoginID]AccountStatus

	stopped chan struct{}
	stop    sync.Once
}

var _ bridgev2.MatrixConnector = (*Connector)(nil)

// New returns the Matrix side of one network.
func New(network string, st *store.Store, settings Settings, net Network, log *slog.Logger) *Connector {
	if log == nil {
		log = slog.Default()
	}
	return &Connector{
		network:  network,
		store:    st,
		settings: settings,
		net:      net,
		log:      log.With("network", network),
		rooms:    map[id.RoomID]networkid.PortalKey{},
		states:   map[networkid.UserLoginID]AccountStatus{},
		stopped:  make(chan struct{}),
	}
}

// SetListener sets who hears statuses.
func (c *Connector) SetListener(l StatusListener) { c.listener.Store(&l) }

func (c *Connector) statusListener() StatusListener {
	if l := c.listener.Load(); l != nil {
		return *l
	}
	return nil
}

// Bridge is the bridge this side belongs to.
func (c *Connector) Bridge() *bridgev2.Bridge { return c.br }

// Network is the network this side belongs to.
func (c *Connector) Network() string { return c.network }

// Init implements bridgev2.MatrixConnector.
func (c *Connector) Init(br *bridgev2.Bridge) { c.br = br }

// Start implements bridgev2.MatrixConnector.
func (c *Connector) Start(context.Context) error { return nil }

// PreStop implements bridgev2.MatrixConnector.
func (c *Connector) PreStop() {}

// Stop implements bridgev2.MatrixConnector. Bridge states are sent from a
// goroutine that outlives the bridge, so writes after this are dropped.
func (c *Connector) Stop() { c.stop.Do(func() { close(c.stopped) }) }

func (c *Connector) isStopped() bool {
	select {
	case <-c.stopped:
		return true
	default:
		return false
	}
}

// GetCapabilities implements bridgev2.MatrixConnector. Batch sending is what
// lets bridgev2 run its backward history queue.
func (c *Connector) GetCapabilities() *bridgev2.MatrixCapabilities {
	return &bridgev2.MatrixCapabilities{
		AutoJoinInvites:       true,
		BatchSending:          true,
		ArbitraryMemberChange: true,
		ExtraProfileMeta:      true,
	}
}

// ServerName implements bridgev2.MatrixConnector.
func (c *Connector) ServerName() string { return ServerName }

func (c *Connector) ghostPrefix() string { return "@" + c.network + "_" }

// BotMXID is this network's bridge bot.
func (c *Connector) BotMXID() id.UserID { return id.UserID("@" + c.network + "bot:" + ServerName) }

// ParseGhostMXID implements bridgev2.MatrixConnector.
func (c *Connector) ParseGhostMXID(userID id.UserID) (networkid.UserID, bool) {
	local, ok := strings.CutPrefix(string(userID), c.ghostPrefix())
	if !ok {
		return "", false
	}
	local, ok = strings.CutSuffix(local, ":"+ServerName)
	if !ok {
		return "", false
	}
	decoded, err := id.DecodeUserLocalpart(local)
	if err != nil {
		return "", false
	}
	return networkid.UserID(decoded), true
}

// FormatGhostMXID implements bridgev2.MatrixConnector.
func (c *Connector) FormatGhostMXID(userID networkid.UserID) id.UserID {
	return id.UserID(c.ghostPrefix() + id.EncodeUserLocalpart(string(userID)) + ":" + ServerName)
}

// GhostIntent implements bridgev2.MatrixConnector.
func (c *Connector) GhostIntent(userID networkid.UserID) bridgev2.MatrixAPI {
	return &intent{c: c, mxid: c.FormatGhostMXID(userID), ghost: userID}
}

// NewUserIntent implements bridgev2.MatrixConnector: the owner is the one
// user, and needs no token.
func (c *Connector) NewUserIntent(_ context.Context, userID id.UserID, _ string) (bridgev2.MatrixAPI, string, error) {
	if userID != OwnerMXID {
		return nil, "", fmt.Errorf("no local user %s", userID)
	}
	return &intent{c: c, mxid: OwnerMXID, owner: true}, "", nil
}

// BotIntent implements bridgev2.MatrixConnector.
func (c *Connector) BotIntent() bridgev2.MatrixAPI {
	return &intent{c: c, mxid: c.BotMXID(), bot: true}
}

// GenerateContentURI implements bridgev2.MatrixConnector; direct media is
// not used.
func (c *Connector) GenerateContentURI(context.Context, networkid.MediaID) (id.ContentURIString, error) {
	return "", errors.New("direct media is not used")
}

// ParseContentURI implements bridgev2.MatrixConnector; direct media is not
// used.
func (c *Connector) ParseContentURI(context.Context, id.ContentURIString) (networkid.MediaID, error) {
	return nil, errors.New("direct media is not used")
}

func hashID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return base64.RawURLEncoding.EncodeToString(sum[:18])
}

// GenerateDeterministicRoomID implements bridgev2.MatrixConnector, and
// remembers which portal the room is, since CreateRoom names only the room.
func (c *Connector) GenerateDeterministicRoomID(key networkid.PortalKey) id.RoomID {
	roomID := id.RoomID("!" + hashID(c.network, string(key.ID), string(key.Receiver)) + ":" + ServerName)
	c.roomsMu.Lock()
	c.rooms[roomID] = key
	c.roomsMu.Unlock()
	return roomID
}

// GenerateDeterministicEventID implements bridgev2.MatrixConnector.
func (c *Connector) GenerateDeterministicEventID(roomID id.RoomID, _ networkid.PortalKey, messageID networkid.MessageID, partID networkid.PartID) id.EventID {
	return id.EventID("$" + hashID(string(roomID), string(messageID), string(partID)) + ":" + ServerName)
}

// GenerateReactionEventID implements bridgev2.MatrixConnector.
func (c *Connector) GenerateReactionEventID(roomID id.RoomID, target *database.Message, sender networkid.UserID, emojiID networkid.EmojiID) id.EventID {
	var msgID, partID string
	if target != nil {
		msgID, partID = string(target.ID), string(target.PartID)
	}
	return id.EventID("$" + hashID(string(roomID), "reaction", msgID, partID, string(sender), string(emojiID)) + ":" + ServerName)
}

// GetPowerLevels implements bridgev2.MatrixConnector.
func (c *Connector) GetPowerLevels(ctx context.Context, roomID id.RoomID) (*event.PowerLevelsEventContent, error) {
	levels := &event.PowerLevelsEventContent{Users: map[id.UserID]int{c.BotMXID(): 100, OwnerMXID: 100}}
	var members []store.MemberRow
	err := c.store.View(ctx, func(tx *store.Tx) error {
		var err error
		members, err = tx.Members(ctx, string(roomID))
		return err
	})
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		if m.Power != 0 {
			levels.Users[id.UserID(m.MXID)] = m.Power
		}
	}
	return levels, nil
}

// GetMembers implements bridgev2.MatrixConnector.
func (c *Connector) GetMembers(ctx context.Context, roomID id.RoomID) (map[id.UserID]*event.MemberEventContent, error) {
	var members []store.MemberRow
	err := c.store.View(ctx, func(tx *store.Tx) error {
		var err error
		members, err = tx.Members(ctx, string(roomID))
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make(map[id.UserID]*event.MemberEventContent, len(members))
	for _, m := range members {
		out[id.UserID(m.MXID)] = &event.MemberEventContent{Membership: event.Membership(m.Membership)}
	}
	return out, nil
}

// GetMemberInfo implements bridgev2.MatrixConnector.
func (c *Connector) GetMemberInfo(ctx context.Context, roomID id.RoomID, userID id.UserID) (*event.MemberEventContent, error) {
	members, err := c.GetMembers(ctx, roomID)
	if err != nil {
		return nil, err
	}
	if m, ok := members[userID]; ok {
		return m, nil
	}
	return &event.MemberEventContent{Membership: event.MembershipLeave}, nil
}

// SendBridgeStatus implements bridgev2.MatrixConnector: a login's state
// becomes account.upsert and account.state.
func (c *Connector) SendBridgeStatus(ctx context.Context, state *status.BridgeState) error {
	if state == nil || state.RemoteID == "" || c.isStopped() {
		return nil
	}
	return c.accountStatus(ctx, state)
}

// SendMessageStatus implements bridgev2.MatrixConnector: an outcome of a
// Matrix event the action runner queued, or a delivery receipt for one of
// the owner's messages.
func (c *Connector) SendMessageStatus(ctx context.Context, st *bridgev2.MessageStatus, info *bridgev2.MessageStatusEventInfo) {
	if st == nil || info == nil || c.isStopped() {
		return
	}
	if l := c.statusListener(); l != nil {
		l.MessageStatus(c.network, info.SourceEventID, st, info)
	}
	if st.Status == event.MessageStatusSuccess && len(st.DeliveredTo) > 0 {
		c.deliveryReceipt(ctx, info)
	}
}

// BatchSend implements bridgev2.MatrixConnector: a page of history, folded
// into message.upsert events marked as backfill.
func (c *Connector) BatchSend(ctx context.Context, roomID id.RoomID, req *mautrix.ReqBeeperBatchSend, extras []*bridgev2.MatrixSendExtra) (*mautrix.RespBeeperBatchSend, error) {
	resp := &mautrix.RespBeeperBatchSend{EventIDs: make([]id.EventID, len(req.Events))}
	for i, evt := range req.Events {
		resp.EventIDs[i] = evt.ID
	}
	if c.isStopped() {
		return nil, errors.New("the bridge is stopping")
	}
	err := c.batch(ctx, roomID, req, extras)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// portalKey returns the portal a room is, from memory or the store.
func (c *Connector) portalKey(ctx context.Context, tx *store.Tx, roomID id.RoomID) (networkid.PortalKey, bool, error) {
	c.roomsMu.Lock()
	key, ok := c.rooms[roomID]
	c.roomsMu.Unlock()
	if ok {
		return key, true, nil
	}
	r, err := tx.Room(ctx, string(roomID))
	if err != nil || r == nil {
		return networkid.PortalKey{}, false, err
	}
	key = networkid.PortalKey{ID: networkid.PortalID(r.PortalID), Receiver: networkid.UserLoginID(r.Receiver)}
	c.roomsMu.Lock()
	c.rooms[roomID] = key
	c.roomsMu.Unlock()
	return key, true, nil
}

// RoomFor is the room of a portal, for the action runner.
func (c *Connector) RoomFor(chatID, account string) id.RoomID {
	return c.GenerateDeterministicRoomID(networkid.PortalKey{ID: networkid.PortalID(chatID), Receiver: networkid.UserLoginID(account)})
}
