package networks

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	telegram "go.mau.fi/mautrix-telegram/pkg/connector"
	"go.mau.fi/mautrix-telegram/pkg/gotd/tgerr"
	"maunium.net/go/mautrix/bridgev2/status"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/localmatrix"
	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

var testApp = TelegramApp{APIID: 12345, APIHash: "0123456789abcdef0123456789abcdef"}

func TestTelegramIsOfferedOnlyWithAnAppRegistration(t *testing.T) {
	if _, ok := Find("telegram", Options{}); ok {
		t.Fatal("Telegram is offered without an app registration")
	}
	if _, ok := Find("telegram", Options{Telegram: TelegramApp{APIID: 1}}); ok {
		t.Fatal("Telegram is offered with half a registration")
	}
	if _, ok := Find("telegram", Options{Telegram: testApp}); !ok {
		t.Fatal("Telegram is not offered with an app registration")
	}
	if _, _, err := newTelegram(Options{}); err == nil {
		t.Fatal("the connector was built without an app registration")
	}
}

// The connector sends under the transaction id as Telegram's random_id
// only when it reads as a positive integer; Telegram deduplicates a send by
// it.
func TestTelegramTransactionIDsAreRandomIDsTheConnectorTakes(t *testing.T) {
	for i := range 1000 {
		action := fmt.Sprintf("act-%d", i)
		txn := TelegramTxnID(action)
		n, err := strconv.ParseInt(txn, 10, 64)
		if err != nil || n <= 0 {
			t.Fatalf("%s: %q is not a positive int64", action, txn)
		}
		if txn != TelegramTxnID(action) {
			t.Fatal("transaction ids are not stable")
		}
	}
	if TelegramTxnID("a") == TelegramTxnID("b") {
		t.Fatal("transaction ids are not one per action")
	}
}

func TestTelegramConnectorConfiguration(t *testing.T) {
	conn, _, err := newTelegram(Options{DeviceName: "Substrate", History: HistoryYear, Telegram: testApp})
	if err != nil {
		t.Fatal(err)
	}
	c := conn.(*telegram.TelegramConnector).Config
	if c.APIID != testApp.APIID || c.APIHash != testApp.APIHash || c.DeviceInfo.DeviceModel != "Substrate" {
		t.Fatalf("registration or device %+v", c.DeviceInfo)
	}
	if c.Takeout.DialogSync || c.Takeout.ForwardBackfill || c.Takeout.BackwardBackfill || c.ContactNames {
		t.Fatal("a data export or the owner's address book is in use")
	}
	if c.Sync.CreateLimit != 200 || c.Sync.UpdateLimit < 200 {
		t.Fatalf("a year of history creates %d chats of %d", c.Sync.CreateLimit, c.Sync.UpdateLimit)
	}
	conn, _, err = newTelegram(Options{History: HistoryAll, Telegram: testApp})
	if err != nil {
		t.Fatal(err)
	}
	if c := conn.(*telegram.TelegramConnector).Config; c.Sync.CreateLimit != -1 || c.Sync.UpdateLimit != -1 {
		t.Fatalf("all history creates %d chats of %d", c.Sync.CreateLimit, c.Sync.UpdateLimit)
	}
}

func TestTelegramHistoryBoundsTheFirstBackfill(t *testing.T) {
	cases := map[History]int{HistoryRecent: 1000, HistoryYear: 5000, HistoryAll: -1}
	for h, want := range cases {
		cfg := BridgeConfig()
		cfg.UnknownErrorAutoReconnect = time.Minute
		tuneTelegram(Options{History: h}, cfg)
		q := cfg.Backfill.Queue
		got := -1
		if q.MaxBatches >= 0 {
			got = cfg.Backfill.MaxInitialMessages + q.MaxBatches*q.BatchSize
		}
		if got != want {
			t.Fatalf("%s fetches %d messages of a chat, not %d", h, got, want)
		}
		if cfg.UnknownErrorAutoReconnect != 0 {
			t.Fatal("an unknown error reconnects a session Telegram may have closed for being used twice")
		}
	}
	if !HistoryAll.Deeper(HistoryYear) || !HistoryYear.Deeper(HistoryRecent) || HistoryRecent.Deeper(HistoryRecent) {
		t.Fatal("history depths are not ordered")
	}
}

func TestTelegramReadsASessionUsedTwice(t *testing.T) {
	h := tgHooks{}
	for _, state := range []*status.BridgeState{
		{StateEvent: status.StateUnknownError, Message: "rpc error code 406: AUTH_KEY_DUPLICATED"},
		{StateEvent: status.StateUnknownError, Info: map[string]any{"go_error": "auth: AUTH_KEY_DUPLICATED"}},
	} {
		st := h.StateWarnings(state, localmatrix.AccountStatus{State: contract.StateUnknownError}, time.Now())
		if st.State != contract.StateBadCredentials || !strings.Contains(st.Message, "another copy") {
			t.Fatalf("%+v", st)
		}
	}
	plain := h.StateWarnings(&status.BridgeState{StateEvent: status.StateConnected}, localmatrix.AccountStatus{State: contract.StateConnected}, time.Now())
	if plain.State != contract.StateConnected || plain.Message != "" {
		t.Fatalf("a connected state was changed: %+v", plain)
	}
}

func TestTelegramWaitsAndErrors(t *testing.T) {
	h := tgHooks{}
	now := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	w, ok := h.Throttle(fmt.Errorf("send: %w", tgerr.New(420, "FLOOD_WAIT_30")), now)
	if !ok || w.Code != contract.WarningFloodWait || w.Until == nil || *w.Until != contract.At(now.Add(30*time.Second)) {
		t.Fatalf("flood wait %+v %v", w, ok)
	}
	if w, ok := h.Throttle(tgerr.New(400, "PEER_FLOOD"), now); !ok || w.Until == nil || *w.Until != contract.At(now.Add(24*time.Hour)) {
		t.Fatalf("peer flood %+v %v", w, ok)
	}
	if _, ok := h.Throttle(tgerr.New(400, "MESSAGE_ID_INVALID"), now); ok {
		t.Fatal("an invalid id stops sending")
	}
	cases := map[string]string{
		"FLOOD_WAIT_5":         contract.ErrRateLimited,
		"USER_IS_BLOCKED":      contract.ErrBlocked,
		"CHAT_WRITE_FORBIDDEN": contract.ErrRejected,
		"MESSAGE_ID_INVALID":   contract.ErrNotFound,
		"MESSAGE_TOO_LONG":     contract.ErrInvalid,
	}
	for msg, code := range cases {
		out, ok := h.Classify(tgerr.New(400, msg))
		if !ok || out.Outcome != contract.OutcomeFailed || out.ErrorCode != code {
			t.Fatalf("%s: %+v %v", msg, out, ok)
		}
	}
	if _, ok := h.Classify(fmt.Errorf("something else")); ok {
		t.Fatal("an unknown error was classified")
	}
}

// The connector migrates and starts over the store with no login, without
// reaching Telegram.
func TestTelegramStartsWithoutALogin(t *testing.T) {
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	run, err := Open(telegramSpec(), st, nil, Options{DeviceName: "Substrate", Telegram: testApp}, slog.New(slog.DiscardHandler), zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	run.Stop(5 * time.Second)
}
