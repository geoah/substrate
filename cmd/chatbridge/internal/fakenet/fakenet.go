// Package fakenet is a bridgev2 network connector for tests: no network,
// every Matrix-side send counted. It takes a transaction id as the message
// id the way the WhatsApp connector does (only with WhatsApp Web's prefix),
// and it never deduplicates, so a test sees every call the bridge makes.
package fakenet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/configupgrade"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/localmatrix"
	"github.com/geoah/substrate/cmd/chatbridge/internal/networks"
	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

// Network is the fake's network id.
const Network = "fakenet"

// The ids the fake uses.
const (
	Login = "447700900123"
	Self  = "lid-100"
	Other = "lid-200"
	Chat  = "200@lid"
)

// Call is one thing the bridge asked the network to do.
type Call struct {
	Kind   string
	Txn    string
	ID     string
	Text   string
	Target string
	Key    string
}

// Connector is the fake network.
type Connector struct {
	Bridge *bridgev2.Bridge

	mu    sync.Mutex
	calls []Call
	// Before runs before every network call and may fail it, delay it or
	// block it; nil lets everything through.
	Before func(ctx context.Context, c Call) error
	// Contacts are the users in the owner's address book.
	Contacts map[networkid.UserID]string
}

var _ bridgev2.NetworkConnector = (*Connector)(nil)

// Calls returns every network call so far.
func (c *Connector) Calls() []Call {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Call(nil), c.calls...)
}

func (c *Connector) record(ctx context.Context, call Call) error {
	if c.Before != nil {
		if err := c.Before(ctx, call); err != nil {
			return err
		}
	}
	c.mu.Lock()
	c.calls = append(c.calls, call)
	c.mu.Unlock()
	return nil
}

func (c *Connector) Init(br *bridgev2.Bridge)            { c.Bridge = br }
func (c *Connector) Start(context.Context) error         { return nil }
func (c *Connector) GetBridgeInfoVersion() (int, int)    { return 1, 1 }
func (c *Connector) GetLoginFlows() []bridgev2.LoginFlow { return nil }
func (c *Connector) GetName() bridgev2.BridgeName {
	return bridgev2.BridgeName{DisplayName: "Fake", NetworkID: Network, BeeperBridgeType: Network}
}

func (c *Connector) GetDBMetaTypes() database.MetaTypes { return database.MetaTypes{} }

func (c *Connector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	return &bridgev2.NetworkGeneralCapabilities{}
}

func (c *Connector) GetConfig() (string, any, configupgrade.Upgrader) { return "", nil, nil }

func (c *Connector) LoadUserLogin(_ context.Context, login *bridgev2.UserLogin) error {
	login.Client = &Client{c: c, login: login}
	return nil
}

func (c *Connector) CreateLogin(context.Context, *bridgev2.User, string) (bridgev2.LoginProcess, error) {
	return nil, errors.New("the fake network has no login flow")
}

// Client is one fake login.
type Client struct {
	c     *Connector
	login *bridgev2.UserLogin
	mu    sync.Mutex
	up    bool
}

var (
	_ bridgev2.NetworkAPI                    = (*Client)(nil)
	_ bridgev2.EditHandlingNetworkAPI        = (*Client)(nil)
	_ bridgev2.ReactionHandlingNetworkAPI    = (*Client)(nil)
	_ bridgev2.RedactionHandlingNetworkAPI   = (*Client)(nil)
	_ bridgev2.ReadReceiptHandlingNetworkAPI = (*Client)(nil)
)

func (cl *Client) Connect(context.Context) {
	cl.mu.Lock()
	cl.up = true
	cl.mu.Unlock()
	cl.login.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
}

func (cl *Client) Disconnect() {
	cl.mu.Lock()
	cl.up = false
	cl.mu.Unlock()
}

func (cl *Client) IsLoggedIn() bool             { return true }
func (cl *Client) LogoutRemote(context.Context) {}

func (cl *Client) IsThisUser(_ context.Context, user networkid.UserID) bool { return user == Self }

func (cl *Client) GetChatInfo(_ context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	return &bridgev2.ChatInfo{
		Name: ptr("Ada"),
		Type: ptr(database.RoomTypeDM),
		Members: &bridgev2.ChatMemberList{
			IsFull:      true,
			OtherUserID: Other,
			MemberMap: bridgev2.ChatMemberMap{
				Self:  {EventSender: bridgev2.EventSender{IsFromMe: true, Sender: Self}, Membership: event.MembershipJoin},
				Other: {EventSender: bridgev2.EventSender{Sender: Other}, Membership: event.MembershipJoin},
			},
		},
	}, nil
}

func (cl *Client) GetUserInfo(_ context.Context, ghost *bridgev2.Ghost) (*bridgev2.UserInfo, error) {
	switch ghost.ID {
	case Other:
		return &bridgev2.UserInfo{Name: ptr("Ada"), Identifiers: []string{"tel:+447700900456"}, IsBot: ptr(false)}, nil
	case Self:
		return &bridgev2.UserInfo{Name: ptr("Owner"), Identifiers: []string{"tel:+" + Login}, IsBot: ptr(false)}, nil
	}
	return nil, nil
}

func (cl *Client) GetCapabilities(context.Context, *bridgev2.Portal) *event.RoomFeatures {
	return &event.RoomFeatures{
		ID:           "fake",
		Edit:         event.CapLevelFullySupported,
		Delete:       event.CapLevelFullySupported,
		Reaction:     event.CapLevelFullySupported,
		Reply:        event.CapLevelFullySupported,
		ReadReceipts: true,
	}
}

// messageID is a sent message's id: like WhatsApp, chat, sender and the
// sender-chosen id.
func messageID(chat networkid.PortalID, id string) networkid.MessageID {
	return networkid.MessageID(fmt.Sprintf("%s:%s:%s", chat, Self, id))
}

func (cl *Client) HandleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, error) {
	id := string(msg.InputTransactionID)
	if !strings.HasPrefix(id, "3EB0") {
		id = "3EB0" + strings.ToUpper(store.RandomID(9))
	}
	if err := cl.c.record(ctx, Call{Kind: "send", Txn: string(msg.InputTransactionID), ID: id, Text: msg.Content.Body}); err != nil {
		return nil, err
	}
	return &bridgev2.MatrixMessageResponse{DB: &database.Message{
		ID: messageID(msg.Portal.ID, id), SenderID: Self, Timestamp: time.Now(),
	}}, nil
}

func (cl *Client) HandleMatrixEdit(ctx context.Context, edit *bridgev2.MatrixEdit) error {
	return cl.c.record(ctx, Call{Kind: "edit", Txn: string(edit.InputTransactionID), Target: string(edit.EditTarget.ID), Text: edit.Content.Body})
}

func (cl *Client) PreHandleMatrixReaction(_ context.Context, msg *bridgev2.MatrixReaction) (bridgev2.MatrixReactionPreResponse, error) {
	return bridgev2.MatrixReactionPreResponse{SenderID: Self, Emoji: msg.Content.RelatesTo.Key, MaxReactions: 1}, nil
}

func (cl *Client) HandleMatrixReaction(ctx context.Context, msg *bridgev2.MatrixReaction) (*database.Reaction, error) {
	return &database.Reaction{}, cl.c.record(ctx, Call{
		Kind: "react", Txn: string(msg.InputTransactionID),
		Target: string(msg.TargetMessage.ID), Key: msg.PreHandleResp.Emoji,
	})
}

func (cl *Client) HandleMatrixReactionRemove(ctx context.Context, msg *bridgev2.MatrixReactionRemove) error {
	return cl.c.record(ctx, Call{Kind: "unreact", Target: string(msg.TargetReaction.MessageID), Key: msg.TargetReaction.Emoji})
}

func (cl *Client) HandleMatrixMessageRemove(ctx context.Context, msg *bridgev2.MatrixMessageRemove) error {
	return cl.c.record(ctx, Call{Kind: "delete", Txn: string(msg.InputTransactionID), Target: string(msg.TargetMessage.ID)})
}

func (cl *Client) HandleMatrixReadReceipt(ctx context.Context, receipt *bridgev2.MatrixReadReceipt) error {
	target := ""
	if receipt.ExactMessage != nil {
		target = string(receipt.ExactMessage.ID)
	}
	return cl.c.record(ctx, Call{Kind: "markread", Target: target})
}

func ptr[T any](v T) *T { return &v }

// Hooks are the fake's localmatrix.Network.
type Hooks struct{ C *Connector }

var _ localmatrix.Network = Hooks{}

func (h Hooks) UserExtras(_ context.Context, _ *bridgev2.UserLogin, user networkid.UserID) localmatrix.Extras {
	name, ok := h.C.Contacts[user]
	return localmatrix.Extras{Known: true, IsContact: ok, NameKnown: true, ContactName: name}
}

func (Hooks) SelfUserID(*bridgev2.UserLogin) networkid.UserID { return Self }

func (Hooks) StateWarnings(_ *status.BridgeState, st localmatrix.AccountStatus, _ time.Time) localmatrix.AccountStatus {
	return st
}

func (Hooks) ChatCategory(*bridgev2.Portal) string { return "" }

func (Hooks) BeforeRead(context.Context, *bridgev2.UserLogin, *bridgev2.Portal) {}

// Throttle reads the fake's own rate limit as a wait on the account.
func (h Hooks) Throttle(err error, now time.Time) (contract.Warning, bool) {
	if errors.Is(err, ErrRateLimited) {
		return contract.Warning{Code: contract.WarningFloodWait, Message: "slow down", Until: contract.AtPtr(now.Add(time.Minute))}, true
	}
	return contract.Warning{}, false
}

// ErrRateLimited is the fake network asking the account to slow down.
var ErrRateLimited = errors.New("fakenet: rate limited")

func (Hooks) SentMessageIDs(_ *bridgev2.UserLogin, chat, txn string) []networkid.MessageID {
	return []networkid.MessageID{messageID(networkid.PortalID(chat), txn)}
}

func (Hooks) Classify(error) (localmatrix.Outcome, bool) { return localmatrix.Outcome{}, false }

// Env is one fake network running over a store, as the bridge runs one.
type Env struct {
	Store  *store.Store
	Conn   *Connector
	Run    *networks.Running
	Login  *bridgev2.UserLogin
	Portal networkid.PortalKey
}

// Spec is the fake network's registry entry; its transaction ids are
// WhatsApp's.
func Spec(conn *Connector) networks.Spec {
	return networks.Spec{
		ID:          Network,
		DisplayName: "Fake",
		New: func(networks.Options) (bridgev2.NetworkConnector, localmatrix.Network, error) {
			return conn, Hooks{C: conn}, nil
		},
		TxnID: networks.WhatsAppTxnID,
	}
}

// Start opens a store in a temporary directory, runs the fake network over
// it with one connected login, and returns it.
func Start(t testing.TB, settings localmatrix.Settings, listener localmatrix.StatusListener) *Env {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return StartOn(t, st, &Connector{Contacts: map[networkid.UserID]string{}}, settings, listener)
}

// StartOn runs the fake network over an existing store.
func StartOn(t testing.TB, st *store.Store, conn *Connector, settings localmatrix.Settings, listener localmatrix.StatusListener) *Env {
	t.Helper()
	ctx := context.Background()
	run, err := networks.Open(Spec(conn), st, settings, networks.Options{}, slog.New(slog.DiscardHandler), zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	if listener != nil {
		run.Matrix.SetListener(listener)
	}
	if err := run.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { run.Stop(5 * time.Second) })
	owner, err := run.Owner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	login := run.Bridge.GetCachedUserLoginByID(Login)
	if login == nil {
		login, err = owner.NewLogin(ctx, &database.UserLogin{
			ID: Login, RemoteName: "+" + Login,
			RemoteProfile: status.RemoteProfile{Phone: "+" + Login},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	login.Client.Connect(ctx)
	env := &Env{Store: st, Conn: conn, Run: run, Login: login, Portal: networkid.PortalKey{ID: Chat, Receiver: Login}}
	env.WaitConnected(t)
	return env
}

// WaitConnected waits for the login's state to reach the local Matrix side.
func (e *Env) WaitConnected(t testing.TB) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if st, ok := e.Run.Matrix.AccountStatusOf(Login); ok && st.State == contract.StateConnected {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the fake login never reported connected")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Receive queues a text message from the network, as the connector would.
func (e *Env) Receive(t testing.TB, id, sender, text string, at time.Time) {
	t.Helper()
	res := e.Run.Bridge.QueueRemoteEvent(e.Login, &simplevent.Message[string]{
		EventMeta: simplevent.EventMeta{
			Type:         bridgev2.RemoteEventMessage,
			PortalKey:    e.Portal,
			Sender:       bridgev2.EventSender{Sender: networkid.UserID(sender), IsFromMe: sender == Self},
			CreatePortal: true,
			Timestamp:    at,
		},
		ID:   networkid.MessageID(id),
		Data: text,
		ConvertMessageFunc: func(_ context.Context, _ *bridgev2.Portal, _ bridgev2.MatrixAPI, data string) (*bridgev2.ConvertedMessage, error) {
			return &bridgev2.ConvertedMessage{Parts: []*bridgev2.ConvertedMessagePart{{
				Type:    event.EventMessage,
				Content: &event.MessageEventContent{MsgType: event.MsgText, Body: data},
			}}}, nil
		},
	})
	if !res.Success {
		t.Fatalf("queueing a message failed: %+v", res)
	}
}

// Events reads every event in the outbox after seq, decoded.
func (e *Env) Events(t testing.TB, after int64) []map[string]any {
	t.Helper()
	return Decode(t, e.Store, after)
}

// Decode reads every outbox event after seq.
func Decode(t testing.TB, st *store.Store, after int64) []map[string]any {
	t.Helper()
	rows, err := st.Pending(context.Background(), after, store.Limits{Events: 100000, JSON: 1 << 30, Media: 1 << 40})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		var m map[string]any
		if err := jsonUnmarshal(r.Body, &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}
