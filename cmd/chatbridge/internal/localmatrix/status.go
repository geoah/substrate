package localmatrix

import (
	"context"
	"slices"
	"time"

	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

// AccountStatus is one login's connection as the contract states it.
type AccountStatus struct {
	State    string
	Message  string
	Warnings []contract.Warning
}

// stopCodes are the warnings that stop all sending on an account
// (docs/plans/chat-bridge.md, section 9, rule 6).
var stopCodes = []string{
	contract.WarningTemporaryBan,
	contract.WarningReachoutTimelock,
	contract.WarningFloodWait,
	contract.WarningCheckpoint,
}

// Stopped reports a warning that stops sending, or a ban, still in force
// at now, and the warning's words.
func (s AccountStatus) Stopped(now time.Time) (bool, string) {
	if s.State == contract.StateBanned {
		return true, s.Message
	}
	for _, w := range s.Warnings {
		if !slices.Contains(stopCodes, w.Code) {
			continue
		}
		if w.Until != nil {
			until, err := time.Parse(time.RFC3339Nano, string(*w.Until))
			if err == nil && !until.After(now) {
				continue
			}
		}
		return true, w.Message
	}
	return false, ""
}

// Connected reports an account the network is talking to.
func (s AccountStatus) Connected() bool { return s.State == contract.StateConnected }

// mapState is a bridge state as the contract's state, before the network's
// own reading of its error codes.
func mapState(st status.BridgeStateEvent) string {
	switch st {
	case status.StateStarting, status.StateConnecting:
		return contract.StateConnecting
	case status.StateConnected, status.StateBackfilling:
		return contract.StateConnected
	case status.StateTransientDisconnect:
		return contract.StateTransientDisconnect
	case status.StateBadCredentials:
		return contract.StateBadCredentials
	case status.StateLoggedOut:
		return contract.StateLoggedOut
	default:
		return contract.StateUnknownError
	}
}

// AccountStatusOf reads what the side last heard about a login.
func (c *Connector) AccountStatusOf(account string) (AccountStatus, bool) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	s, ok := c.states[networkid.UserLoginID(account)]
	return s, ok
}

func (c *Connector) accountStatus(ctx context.Context, state *status.BridgeState) error {
	account := string(state.RemoteID)
	st := AccountStatus{State: mapState(state.StateEvent)}
	if st.State != contract.StateConnected {
		st.Message = state.Message
	}
	if c.net != nil {
		st = c.net.StateWarnings(state, st, c.store.Now())
	}
	if st.Warnings == nil {
		st.Warnings = []contract.Warning{}
	}
	// A wait the network imposed on an action outlives a reconnect.
	c.stateMu.Lock()
	if prev, ok := c.states[state.RemoteID]; ok {
		now := c.store.Now()
		for _, w := range prev.Warnings {
			if (w.Code != contract.WarningFloodWait && w.Code != contract.WarningCheckpoint) || w.Until == nil {
				continue
			}
			until, err := time.Parse(time.RFC3339Nano, string(*w.Until))
			if err != nil || !until.After(now) || slices.ContainsFunc(st.Warnings, func(n contract.Warning) bool { return n.Code == w.Code }) {
				continue
			}
			st.Warnings = append(st.Warnings, w)
		}
	}
	c.stateMu.Unlock()

	upsert := &contract.AccountUpsert{Head: c.head(account, contract.TypeAccountUpsert, time.Time{})}
	if state.RemoteName != "" {
		upsert.RemoteName = contract.Ptr(state.RemoteName)
	}
	if state.RemoteProfile.Phone != "" {
		upsert.PhoneNumber = contract.Ptr(state.RemoteProfile.Phone)
	}
	if state.RemoteProfile.Username != "" {
		upsert.Handle = contract.Ptr(state.RemoteProfile.Username)
	}
	var sc *roomScope
	if c.br != nil {
		sc = &roomScope{account: account, login: c.br.GetCachedUserLoginByID(state.RemoteID)}
		if self := c.selfUser(sc); self != "" {
			upsert.Self = contract.Ptr(self)
		}
	}
	stateEv := &contract.AccountState{Head: c.head(account, contract.TypeAccountState, time.Time{}), State: st.State, Warnings: st.Warnings}
	if st.Message != "" {
		stateEv.Message = contract.Ptr(st.Message)
	}

	err := c.store.Do(ctx, func(tx *store.Tx) error {
		upsertKey := "account.upsert." + c.network + "." + account
		if d := digest(upsert); d != tx.GetKV(ctx, upsertKey) {
			if upsert.Self != nil {
				if err := c.ensureUser(ctx, tx, sc, *upsert.Self); err != nil {
					return err
				}
			}
			if _, err := tx.Append(ctx, upsert); err != nil {
				return err
			}
			if err := tx.SetKV(ctx, upsertKey, d); err != nil {
				return err
			}
		}
		stateKey := "account.state." + c.network + "." + account
		if d := digest(stateEv); d != tx.GetKV(ctx, stateKey) {
			if _, err := tx.Append(ctx, stateEv); err != nil {
				return err
			}
			return tx.SetKV(ctx, stateKey, d)
		}
		return nil
	})
	if err != nil {
		return err
	}
	c.stateMu.Lock()
	c.states[state.RemoteID] = st
	c.stateMu.Unlock()
	if l := c.statusListener(); l != nil {
		l.AccountState(c.network, account, st)
	}
	return nil
}

// AccountRemoved appends that the bridge logged an account out and forgot
// it, and forgets what it last sent about the account, so a new link sends
// it whole. forget is for an account whose row the owner deleted, which the
// server removed with its chats and users: those are sent again too.
func (c *Connector) AccountRemoved(ctx context.Context, account string, forget bool) error {
	err := c.store.Do(ctx, func(tx *store.Tx) error {
		if _, err := tx.Append(ctx, &contract.AccountRemoved{Head: c.head(account, contract.TypeAccountRemoved, time.Time{})}); err != nil {
			return err
		}
		if err := tx.SetKV(ctx, "account.upsert."+c.network+"."+account, ""); err != nil {
			return err
		}
		if err := tx.SetKV(ctx, "account.state."+c.network+"."+account, ""); err != nil {
			return err
		}
		if forget {
			return tx.ForgetAccount(ctx, c.network, account)
		}
		return nil
	})
	c.stateMu.Lock()
	delete(c.states, networkid.UserLoginID(account))
	c.stateMu.Unlock()
	return err
}

// Warn adds a warning the network gave outside its state to an account's
// state, replacing one of the same code, and appends the state: a flood
// wait, a rate limit or a captcha stops sending on the account until it
// ends.
func (c *Connector) Warn(ctx context.Context, account string, w contract.Warning) {
	c.stateMu.Lock()
	st, ok := c.states[networkid.UserLoginID(account)]
	if !ok {
		st = AccountStatus{State: contract.StateConnected}
	}
	warnings := make([]contract.Warning, 0, len(st.Warnings)+1)
	for _, old := range st.Warnings {
		if old.Code != w.Code {
			warnings = append(warnings, old)
		}
	}
	st.Warnings = append(warnings, w)
	c.states[networkid.UserLoginID(account)] = st
	c.stateMu.Unlock()
	ev := &contract.AccountState{Head: c.head(account, contract.TypeAccountState, time.Time{}), State: st.State, Warnings: st.Warnings}
	if st.Message != "" {
		ev.Message = contract.Ptr(st.Message)
	}
	err := c.store.Do(ctx, func(tx *store.Tx) error {
		key := "account.state." + c.network + "." + account
		d := digest(ev)
		if d == tx.GetKV(ctx, key) {
			return nil
		}
		if _, err := tx.Append(ctx, ev); err != nil {
			return err
		}
		return tx.SetKV(ctx, key, d)
	})
	if err != nil {
		c.log.Error("recording an account warning failed", "error", err)
	}
	if l := c.statusListener(); l != nil {
		l.AccountState(c.network, account, st)
	}
}
