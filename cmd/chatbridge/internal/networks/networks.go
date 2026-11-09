// Package networks runs one bridgev2.Bridge per network over its own SQLite
// database in the store directory, with the local Matrix side in place of a
// homeserver. The registry names every network this build carries; Signal
// joins it behind a build tag once its cgo build lands.
package networks

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"gopkg.in/yaml.v3"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/bridgeconfig"
	"maunium.net/go/mautrix/bridgev2/commands"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"github.com/geoah/substrate/cmd/chatbridge/internal/localmatrix"
	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

// History is how much history a new link asks for. WhatsApp fixes it at
// pairing, so it is chosen before the QR is shown.
type History string

// The history depths.
const (
	HistoryRecent History = "recent"
	HistoryYear   History = "year"
	HistoryAll    History = "all"
)

// Deeper reports whether h asks for more history than o.
func (h History) Deeper(o History) bool { return h.rank() > o.rank() }

func (h History) rank() int {
	switch h {
	case HistoryAll:
		return 2
	case HistoryYear:
		return 1
	}
	return 0
}

// ParseHistory reads a depth.
func ParseHistory(s string) (History, error) {
	switch History(s) {
	case HistoryRecent, HistoryYear, HistoryAll:
		return History(s), nil
	}
	return "", fmt.Errorf("history %q is not recent, year or all", s)
}

// Options configure a network's connector.
type Options struct {
	// DeviceName is the name the owner sees in the network's list of linked
	// devices.
	DeviceName string
	// History is what a link made in this run asks for, or the deepest any
	// account of the network asked for, which bounds its backfill.
	History History
	// Telegram is the operator's app registration at my.telegram.org.
	Telegram TelegramApp
}

// TelegramApp is an application registered at my.telegram.org: Telegram's
// API takes a client only under one, and its terms bind whoever holds it.
type TelegramApp struct {
	APIID   int
	APIHash string
}

// Set reports a registration with both halves.
func (a TelegramApp) Set() bool { return a.APIID > 0 && a.APIHash != "" }

// Spec is one network this build carries.
type Spec struct {
	ID          string
	DisplayName string
	// Flows are the login flows a terminal can drive, best first.
	Flows []string
	// New builds the connector and its hooks.
	New func(Options) (bridgev2.NetworkConnector, localmatrix.Network, error)
	// TxnID is an action's transaction id in the network's own format.
	TxnID func(action string) string
	// Tune adjusts the bridge's configuration for the network.
	Tune func(Options, *bridgeconfig.BridgeConfig)
}

// Registry is every network this build carries and this configuration can
// run: Telegram only with an app registration.
func Registry(opts Options) []Spec {
	specs := []Spec{whatsAppSpec()}
	if opts.Telegram.Set() {
		specs = append(specs, telegramSpec())
	}
	return specs
}

// Find returns the network with an id.
func Find(id string, opts Options) (Spec, bool) {
	for _, s := range Registry(opts) {
		if s.ID == id {
			return s, true
		}
	}
	return Spec{}, false
}

// BackfillBatches bounds the backward history queue per chat; -1 is
// everything the network will give.
var BackfillBatches = -1

// BridgeConfig is the bridgev2 configuration every network shares.
func BridgeConfig() *bridgeconfig.BridgeConfig {
	return &bridgeconfig.BridgeConfig{
		// Nothing the bridge queues starts with a NUL, so no action is ever
		// read as a command; commands are refused besides.
		CommandPrefix:         "\x00chatbridge",
		PrivateChatPortalMeta: true,
		// Every chat belongs to exactly one account.
		SplitPortals: true,
		// Zero is synchronous: a network event is in the outbox before the
		// connector returns, which is what the network's ack waits for.
		PortalEventBuffer: 0,
		// The owner's own messages get deterministic ids, so a later edit,
		// delete or receipt of them resolves.
		OutgoingMessageReID: true,
		// A Matrix event bridgev2 already sent is answered from the database
		// rather than sent again.
		DeduplicateMatrixMessages: true,
		SendReconnectWait:         5 * time.Second,
		UnknownErrorAutoReconnect: time.Minute,
		// A logout leaves the chats; the server keeps the account's rows.
		CleanupOnLogout: bridgeconfig.CleanupOnLogouts{Enabled: false},
		Permissions: bridgeconfig.PermissionConfig{
			"*": &bridgeconfig.Permissions{SendEvents: true, Login: true, DoublePuppet: true},
		},
		Backfill: bridgeconfig.BackfillConfig{
			Enabled:              true,
			MaxInitialMessages:   100,
			MaxCatchupMessages:   500,
			UnreadHoursThreshold: 24 * 30,
			Queue: bridgeconfig.BackfillQueueConfig{
				Enabled:    true,
				BatchSize:  50,
				BatchDelay: 20,
				MaxBatches: BackfillBatches,
			},
		},
	}
}

// Running is one network's bridge, started.
type Running struct {
	Spec   Spec
	Bridge *bridgev2.Bridge
	Matrix *localmatrix.Connector
}

// Open builds a network's bridge over its database in the store directory.
// It does not start it.
func Open(spec Spec, st *store.Store, settings localmatrix.Settings, opts Options, log *slog.Logger, zlog zerolog.Logger) (*Running, error) {
	conn, hooks, err := spec.New(opts)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", spec.ID, err)
	}
	db, err := dbutil.NewWithDialect(store.DSN(filepath.Join(st.Dir(), spec.ID+".db")), store.Driver)
	if err != nil {
		return nil, fmt.Errorf("%s: opening the database: %w", spec.ID, err)
	}
	zlog = zlog.With().Str("network", spec.ID).Logger()
	db.Log = dbutil.ZeroLogger(zlog.With().Str("db", spec.ID).Logger())
	matrix := localmatrix.New(spec.ID, st, settings, hooks, log)
	// The WhatsApp connector adds its handlers to bridgev2's own command
	// processor, so the bridge carries one; no event ever reaches it.
	cfg := BridgeConfig()
	if spec.Tune != nil {
		spec.Tune(opts, cfg)
	}
	br := bridgev2.NewBridge(networkid.BridgeID(spec.ID), db, zlog, cfg, matrix, conn,
		func(br *bridgev2.Bridge) bridgev2.CommandProcessor { return commands.NewProcessor(br) })
	return &Running{Spec: spec, Bridge: br, Matrix: matrix}, nil
}

// Start starts the bridge and every login it holds.
func (r *Running) Start(ctx context.Context) error { return r.Bridge.Start(ctx) }

// StartConnectorsOnly starts the bridge without connecting its logins, for a
// sign-in that must not wake the rest.
func (r *Running) StartConnectorsOnly(ctx context.Context) error {
	return r.Bridge.StartConnectors(ctx)
}

// Stop stops the bridge, waiting at most timeout for its logins.
func (r *Running) Stop(timeout time.Duration) { r.Bridge.StopWithTimeout(timeout) }

// Owner is the bridge's one user.
func (r *Running) Owner(ctx context.Context) (*bridgev2.User, error) {
	return r.Bridge.GetUserByMXID(ctx, localmatrix.OwnerMXID)
}

// loadExample fills a connector's configuration with its shipped defaults.
func loadExample(conn bridgev2.NetworkConnector) error {
	example, data, _ := conn.GetConfig()
	if example == "" || data == nil {
		return nil
	}
	return yaml.Unmarshal([]byte(example), data)
}
