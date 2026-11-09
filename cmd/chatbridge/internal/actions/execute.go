package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/localmatrix"
	"github.com/geoah/substrate/cmd/chatbridge/internal/server"
	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

// claim writes the claim and appends action.claimed in one transaction,
// before anything leaves for the network. It reports false when the action
// already has a claim: another worker won the race.
func (r *Runner) claim(ctx context.Context, rec *server.Record, req localmatrix.Request, t target) (*store.Claim, bool, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, false, err
	}
	c := store.Claim{
		Action:    rec.ID,
		Attempt:   store.RandomID(12),
		Version:   rec.Version,
		Digest:    requestDigest(rec),
		Claimed:   true,
		Network:   t.network,
		Account:   t.account,
		Kind:      req.Kind,
		Request:   string(raw),
		Txn:       req.Txn,
		ClaimedAt: r.Now(),
	}
	won := false
	err = r.Store.Do(ctx, func(tx *store.Tx) error {
		var err error
		won, err = tx.Claim(ctx, c)
		if err != nil || !won {
			return err
		}
		_, err = tx.Append(ctx, &contract.ActionClaimed{
			Head:    contract.Head{Type: contract.TypeActionClaimed},
			Action:  c.Action,
			Attempt: c.Attempt,
			Version: c.Version,
		})
		return err
	})
	if err != nil || !won {
		return nil, false, err
	}
	r.Metrics.Claimed.Add(1)
	return &c, true, nil
}

// report records an attempt's outcome and appends what it changed and its
// action.result, in one transaction. A second report of the same attempt
// changes nothing.
func (r *Runner) report(ctx context.Context, c *store.Claim, req localmatrix.Request, out localmatrix.Outcome, exec localmatrix.Request, net *Network) error {
	err := r.Store.Do(ctx, func(tx *store.Tx) error {
		changed, err := tx.SetOutcome(ctx, c.Action, out.Outcome, out.Message, out.ErrorCode, out.ErrorText)
		if err != nil || !changed {
			return err
		}
		if out.Outcome == contract.OutcomeDone && net != nil {
			if err := net.Exec.Echo(ctx, tx, exec, out); err != nil {
				return fmt.Errorf("recording what the action changed: %w", err)
			}
		}
		ev := &contract.ActionResult{
			Head:    contract.Head{Type: contract.TypeActionResult},
			Action:  c.Action,
			Attempt: c.Attempt,
			Outcome: out.Outcome,
		}
		if out.Message != "" {
			ev.Message = contract.Ptr(out.Message)
		}
		if out.ErrorCode != "" {
			ev.Error = &contract.ActionError{Code: out.ErrorCode, Message: out.ErrorText}
		}
		_, err = tx.Append(ctx, ev)
		return err
	})
	if err != nil {
		return err
	}
	switch out.Outcome {
	case contract.OutcomeDone:
		r.Metrics.Done.Add(1)
	case contract.OutcomeFailed:
		r.Metrics.Failed.Add(1)
	case contract.OutcomeUncertain:
		r.Metrics.Uncertain.Add(1)
	}
	r.Log.Info("an action ended", "action", c.Action, "kind", c.Kind, "outcome", out.Outcome, "code", out.ErrorCode)
	return nil
}

// refuse fails an action before a claim: a check refused it, so nothing
// leaves, and the result carries a fresh attempt with no action.claimed
// before it, which the function applies to a row still queued.
func (r *Runner) refuse(ctx context.Context, rec *server.Record, code, text string) error {
	return r.finishUnclaimed(ctx, rec, contract.OutcomeFailed, code, text)
}

// finishUnclaimed ends an action that never reached a claim: refused,
// cancelled or expired while queued. There is no attempt to match, so it
// mints one the result carries.
func (r *Runner) finishUnclaimed(ctx context.Context, rec *server.Record, outcome, code, text string) error {
	c := store.Claim{
		Action:    rec.ID,
		Attempt:   store.RandomID(12),
		Version:   rec.Version,
		Digest:    requestDigest(rec),
		Kind:      rec.String("action"),
		Request:   "{}",
		ClaimedAt: r.Now(),
	}
	err := r.Store.Do(ctx, func(tx *store.Tx) error {
		won, err := tx.Claim(ctx, c)
		if err != nil || !won {
			return err
		}
		if _, err := tx.SetOutcome(ctx, c.Action, outcome, "", code, text); err != nil {
			return err
		}
		_, err = tx.Append(ctx, &contract.ActionResult{
			Head:    contract.Head{Type: contract.TypeActionResult},
			Action:  c.Action,
			Attempt: c.Attempt,
			Outcome: outcome,
			Error:   &contract.ActionError{Code: code, Message: text},
		})
		return err
	})
	if err != nil {
		return err
	}
	switch outcome {
	case contract.OutcomeExpired:
		r.Metrics.Expired.Add(1)
	case contract.OutcomeCancelled:
		r.Metrics.Cancelled.Add(1)
	default:
		r.Metrics.Failed.Add(1)
	}
	r.Log.Info("an action ended before a claim", "action", c.Action, "kind", c.Kind, "outcome", outcome, "code", code)
	return nil
}

// execute claims, marks the claim as leaving, runs the action once and
// reports it.
func (r *Runner) execute(ctx context.Context, rec *server.Record, req localmatrix.Request, t target, net *Network) error {
	c, won, err := r.claim(ctx, rec, req, t)
	if err != nil || !won {
		return err
	}
	return r.dispatch(ctx, c, req, net)
}

// dispatch is the part of an attempt that reaches the network.
func (r *Runner) dispatch(ctx context.Context, c *store.Claim, req localmatrix.Request, net *Network) error {
	if err := r.Store.Dispatched(ctx, c.Action); err != nil {
		return err
	}
	if req.Kind == localmatrix.ActionSend {
		if err := r.Store.RecordSend(ctx, req.Account, req.Chat, textDigest(req.Text), r.Now()); err != nil {
			return err
		}
	}
	out := net.Exec.Execute(ctx, req)
	if ctx.Err() != nil && out.Outcome != contract.OutcomeDone {
		// Stopping mid-call: the restart settles it from the claim.
		return ctx.Err()
	}
	return r.report(ctx, c, req, out, req, net)
}

// Recover settles every claim a previous run left without an outcome. A
// claim that never left for the network runs now, under the same attempt;
// one that left is done when the network shows what it did, and uncertain
// otherwise. Nothing that may have left is sent again.
func (r *Runner) Recover(ctx context.Context) error {
	r.init()
	claims, err := r.Store.OpenClaims(ctx)
	if err != nil {
		return err
	}
	for i := range claims {
		c := &claims[i]
		net := r.Networks[c.Network]
		var req localmatrix.Request
		if err := json.Unmarshal([]byte(c.Request), &req); err != nil || net == nil {
			if err := r.report(ctx, c, req, localmatrix.Outcome{
				Outcome: contract.OutcomeUncertain, ErrorCode: contract.ErrNetwork,
				ErrorText: "the bridge restarted and cannot tell whether the action was carried out",
			}, req, nil); err != nil {
				return err
			}
			continue
		}
		if c.DispatchedAt == nil {
			if err := r.redispatch(ctx, c, req, net); err != nil {
				return err
			}
			continue
		}
		out, found := net.Exec.FindSent(ctx, req)
		if !found {
			out = localmatrix.Outcome{
				Outcome: contract.OutcomeUncertain, ErrorCode: contract.ErrNetwork,
				ErrorText: "the bridge stopped while the action was with the network; it may or may not have been carried out",
			}
		}
		if err := r.report(ctx, c, req, out, req, net); err != nil {
			return err
		}
	}
	return nil
}

// redispatch runs a claim that never left, unless its window closed while
// the bridge was down; the claimed attempt is the one that runs.
func (r *Runner) redispatch(ctx context.Context, c *store.Claim, req localmatrix.Request, net *Network) error {
	rec, err := r.Records.GetRecord(ctx, server.KindAction, c.Action)
	if err != nil {
		return err
	}
	if rec != nil {
		notAfter, ok := rec.Time("notAfter")
		if !ok && (req.Kind == localmatrix.ActionSend || req.Kind == localmatrix.ActionEdit) && !rec.CreatedAt.IsZero() {
			notAfter, ok = rec.CreatedAt.Add(r.TTL), true
		}
		if ok && r.Now().After(notAfter) {
			return r.report(ctx, c, req, localmatrix.Outcome{
				Outcome: contract.OutcomeFailed, ErrorCode: contract.ErrExpired,
				ErrorText: "the bridge stopped before sending, and the action expired meanwhile; nothing was sent",
			}, req, nil)
		}
	}
	status, _ := net.Exec.AccountStatusOf(req.Account)
	if !status.Connected() {
		// The account is not back yet: leave the claim open for the next
		// start, which finds it undispatched still.
		return nil
	}
	return r.dispatch(ctx, c, req, net)
}

// Account implements server.Sink: an account record as it stands, with the
// owner's toggles.
func (r *Runner) Account(ctx context.Context, rec *server.Record) {
	r.init()
	network, login := rec.String("network"), rec.String("loginId")
	if network == "" || login == "" {
		return
	}
	s := settingsOf(rec)
	prev := r.Settings.Get(network, login)
	r.Settings.Put(network, login, s)
	_, instance, _ := rec.Ref("instance")
	if err := r.Store.PutAccountRef(ctx, store.AccountRef{
		RecordID: rec.ID, Network: network, LoginID: login,
		Settings: settingsJSON(s), Version: rec.Version, Instance: instance,
	}); err != nil {
		r.Log.Error("recording an account failed", "error", err)
	}
	net := r.Networks[network]
	if net == nil || (net.Runs != nil && !net.Runs(login)) {
		return
	}
	if s.UnlinkRequestedAt != nil {
		// The row stays, with its chats and messages.
		r.unlink(ctx, net, login, s.UnlinkRequestedAt.UTC().Format(time.RFC3339Nano), false)
		return
	}
	if s.SyncPaused != prev.SyncPaused && net.Pause != nil {
		net.Pause(ctx, login, s.SyncPaused)
	}
	r.AccountChanged(network, login)
}

// AccountDeleted implements server.Sink: the owner deleted the account's
// row, which means unlink; the server removed its chats and users with it.
func (r *Runner) AccountDeleted(ctx context.Context, network, login string) {
	r.init()
	net := r.Networks[network]
	if net == nil || (net.Runs != nil && !net.Runs(login)) {
		return
	}
	r.unlink(ctx, net, login, "deleted", true)
}

// unlink logs a login out once per request.
func (r *Runner) unlink(ctx context.Context, net *Network, login, request string, forget bool) {
	k := "unlinked." + net.ID + "." + login
	if done, _, _ := r.Store.Get(ctx, k); done == request {
		return
	}
	if net.Unlink == nil {
		return
	}
	if err := net.Unlink(ctx, login, forget); err != nil {
		r.Log.Error("logging an account out failed", "network", net.ID, "error", err)
		return
	}
	if err := r.Store.Set(ctx, k, request); err != nil {
		r.Log.Error("recording an unlink failed", "error", err)
	}
	r.Log.Info("logged an account out at the owner's request", "network", net.ID)
}
