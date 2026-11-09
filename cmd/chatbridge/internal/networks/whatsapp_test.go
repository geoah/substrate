package networks

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	whatsapp "go.mau.fi/mautrix-whatsapp/pkg/connector"
	"go.mau.fi/whatsmeow"
	"maunium.net/go/mautrix/bridgev2/status"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/localmatrix"
)

// The connector sends under the transaction id only when it has WhatsApp
// Web's prefix, and the id is a sender-chosen WhatsApp message id: the same
// shape whatsmeow's own client generates.
func TestWhatsAppTransactionIDsAreMessageIDsTheConnectorTakes(t *testing.T) {
	txn := WhatsAppTxnID("act-1")
	if !strings.HasPrefix(txn, whatsmeow.WebMessageIDPrefix) {
		t.Fatalf("%q lacks %q", txn, whatsmeow.WebMessageIDPrefix)
	}
	if len(txn) != len(whatsmeow.WebMessageIDPrefix)+18 || strings.ToUpper(txn) != txn {
		t.Fatalf("%q is not shaped like a WhatsApp Web message id", txn)
	}
	if txn != WhatsAppTxnID("act-1") || txn == WhatsAppTxnID("act-2") {
		t.Fatal("transaction ids are not one per action")
	}
}

func TestWhatsAppStateWarnings(t *testing.T) {
	now := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	h := waHooks{}
	ban := h.StateWarnings(&status.BridgeState{StateEvent: status.StateBadCredentials, Error: waTemporaryBan, Message: "banned"},
		localmatrix.AccountStatus{State: contract.StateBadCredentials}, now)
	if ban.State != contract.StateBanned || len(ban.Warnings) != 1 || ban.Warnings[0].Code != contract.WarningTemporaryBan {
		t.Fatalf("ban %+v", ban)
	}
	if stopped, _ := ban.Stopped(now); !stopped {
		t.Fatal("a ban does not stop sending")
	}
	phone := h.StateWarnings(&status.BridgeState{StateEvent: status.StateTransientDisconnect, Error: waPhoneOffline},
		localmatrix.AccountStatus{State: contract.StateTransientDisconnect}, now)
	if phone.State != contract.StateConnected || phone.Warnings[0].Code != contract.WarningPhoneOffline {
		t.Fatalf("phone %+v", phone)
	}
	if stopped, _ := phone.Stopped(now); stopped {
		t.Fatal("a phone not seen stops sending")
	}
	lock := h.StateWarnings(&status.BridgeState{StateEvent: status.StateConnected, Info: map[string]any{"reachout_timelock_until": now.Add(time.Hour)}},
		localmatrix.AccountStatus{State: contract.StateConnected}, now)
	if len(lock.Warnings) != 1 || lock.Warnings[0].Code != contract.WarningReachoutTimelock || lock.Warnings[0].Until == nil {
		t.Fatalf("timelock %+v", lock)
	}
	if stopped, _ := lock.Stopped(now); !stopped {
		t.Fatal("a timelock does not stop sending")
	}
	if stopped, _ := lock.Stopped(now.Add(2 * time.Hour)); stopped {
		t.Fatal("an ended timelock still stops sending")
	}
}

func TestWhatsAppClassify(t *testing.T) {
	h := waHooks{}
	cases := map[error]string{
		fmt.Errorf("send: %w", whatsmeow.ErrMessageTimedOut): contract.OutcomeUncertain,
		whatsmeow.ErrNotConnected:                            contract.OutcomeFailed,
		whatsmeow.ErrNoPermissionInGroup:                     contract.OutcomeFailed,
	}
	for err, want := range cases {
		out, ok := h.Classify(err)
		if !ok || out.Outcome != want {
			t.Fatalf("%v: %+v %v, want %s", err, out, ok, want)
		}
	}
	if _, ok := h.Classify(errors.New("something else")); ok {
		t.Fatal("an unknown error was classified")
	}
}

func TestWhatsAppConnectorConfiguration(t *testing.T) {
	conn, _, err := newWhatsApp(Options{DeviceName: "Substrate", History: HistoryAll})
	if err != nil {
		t.Fatal(err)
	}
	wa := conn.(*whatsapp.WhatsAppConnector)
	c := wa.Config
	if c.OSName != "Substrate" || c.SendPresenceOnTyping || c.EnableStatusBroadcast || c.CallStartNotices {
		t.Fatalf("config %+v", c)
	}
	if !c.HistorySync.RequestFullSync || c.HistorySync.FullSyncConfig.DaysLimit == 0 {
		t.Fatal("the full history depth asks for no full sync")
	}
}
