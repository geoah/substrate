package engine

// An app's agent (decision 0207): an agent of a package an app owns
// (decision 0205), which the app declared under the approval an `agent`
// authorization detail put on its token. The scoped apply held the agent to
// that approval once; the engine holds it again on every run and before
// every model call, so the agent reaches, spends and outlives no more than
// the owner approved:
//
//   - a live approval: the repository holds a token of the owning app,
//     unexpired and not deleted, that approves this agent, and every such
//     live token's approval covers its reads, writes and daily cap, so the
//     lowest cap and the narrowest reach hold. Revoking the app, letting its
//     access end or approving it narrower stops the agent; signing in again
//     restores it, with nothing to flip back;
//   - a priced model: an unpriced model records no spend, so no cap would
//     hold;
//   - a root run: as another agent's sub-agent it would spend under its
//     caller's cap;
//   - a person's entry: a chat (the owner's, or an app's whose reads cover
//     it), its resume after a question, or the owner's own call. A
//     trigger, a function's call or a patch policy's judge would hand its
//     reply to code that may read far less than it does, which is the
//     sub-agent path again; they are refused until an app's agent needs
//     one (decision 0207).
//
// A package the owner took over is theirs, and none of this applies.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// errAppAgentStopped marks a run an app's agent may not make. It is ErrGuard
// to every caller that knows only the sentinels.
var errAppAgentStopped = fmt.Errorf("%w", substrate.ErrGuard)

// appAgentTokensQuery reads every token that approves agents: the app it was
// minted for, when it ends, what it approves, and whether it was revoked. A
// revoked token covers nothing, but still names the app for the refusal.
const appAgentTokensQuery = `
	SELECT props->>'clientId', props->>'clientName', props->>'expiresAt', props->'agents', deleted_at IS NOT NULL
	FROM records
	WHERE kind = $1 AND props ? 'agents'
	ORDER BY created_at`

// appAgentOwner is the app that owns ag's package, the zero value when no
// app does.
func (ds *dataset) appAgentOwner(ctx context.Context, ag *vocabulary.Agent) (substrate.PackageOwner, error) {
	return ds.packageOwner(ctx, ds.db, ag.Package)
}

// appAgentHold says why a run of ag may not go on, "" when it may. root is
// whether the run is a root run. Only an agent of an app-owned package is
// held; every other answers "".
func (ds *dataset) appAgentHold(ctx context.Context, ag *vocabulary.Agent, root bool) (string, error) {
	owner, err := ds.appAgentOwner(ctx, ag)
	if err != nil || !owner.App() {
		return "", err
	}
	name := agentLabel(ag)
	if !root {
		return fmt.Sprintf("%s is kept by the app %s and runs only on its own, never as another agent's sub-agent, so its own daily cap holds it", name, owner), nil
	}
	app, covered, err := ds.appApprovalCovers(ctx, owner, ag)
	if err != nil {
		return "", err
	}
	if !covered {
		if app == "" {
			app = owner.String()
		}
		return fmt.Sprintf("%s is stopped: %s, the app that keeps it, holds no live approval for it. Sign in to %s again to approve it, or turn %s off on its package's page in the console",
			name, app, app, name), nil
	}
	priced, err := ds.agentModelPriced(ctx, ag)
	if err != nil {
		return "", err
	}
	if !priced {
		if app == "" {
			app = owner.String()
		}
		return fmt.Sprintf("%s is stopped: its model %s has no price on the AI provider %s, so its spend would count as nothing and its daily cap could not hold. Add the model's price back to the provider on the console's Providers page; %s also picks a priced model again the next time it syncs",
			name, ag.Model, ag.Provider, app), nil
	}
	return "", nil
}

// appAgentEntry says why an app's agent may not run on this entry, "" when
// it may: a chat, a resume of its own thread, or the owner's direct call,
// never a trigger, a function's call or a judge.
func appAgentEntry(ag *vocabulary.Agent, in agentInvocation) string {
	switch {
	case in.mode == agentModeChat || in.resume:
		return ""
	case in.mode == "call" && !strings.HasPrefix(string(in.userActor), "function:"):
		return ""
	}
	what := "a trigger"
	switch {
	case in.mode == "call":
		what = "a function's call"
	case in.mode == "judge":
		what = "a policy's judge"
	}
	return fmt.Sprintf("%s is kept by an app and runs only when a person chats with it, never from %s (decision 0207)", agentLabel(ag), what)
}

// refuseAppAgent is appAgentHold as a refusal.
func (ds *dataset) refuseAppAgent(ctx context.Context, ag *vocabulary.Agent, root bool) error {
	text, err := ds.appAgentHold(ctx, ag, root)
	if err != nil {
		return err
	}
	if text != "" {
		return fmt.Errorf("%w: %s", errAppAgentStopped, text)
	}
	return nil
}

// appApprovalCovers reports whether owner's live tokens approve ag as it is
// declared: at least one approves it, and every one that does covers it. It
// also answers the app's own name for the refusal.
func (ds *dataset) appApprovalCovers(ctx context.Context, owner substrate.PackageOwner, ag *vocabulary.Agent) (string, bool, error) {
	rows, err := ds.db.QueryContext(ctx, appAgentTokensQuery, kindToken)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = rows.Close() }()
	var reads []string
	if ag.Reads != nil {
		reads = ag.Reads.Kinds
	}
	ask := false
	for _, t := range ag.Tools {
		ask = ask || t.Builtin == "ask"
	}
	capCents := ag.Budgets.SpendCentsPerDay
	now := nowUTC()
	app := ""
	covered := false
	for rows.Next() {
		var clientID, clientName, expires sql.NullString
		var raw []byte
		var revoked bool
		if err := rows.Scan(&clientID, &clientName, &expires, &raw, &revoked); err != nil {
			return "", false, err
		}
		// The same app the package's ownership names: its client id where
		// the row stamps one (decision 0205).
		u, perr := url.Parse(clientID.String)
		if perr != nil || u.Host == "" {
			continue
		}
		if ok, _ := owner.Admits(substrate.AppActor(u.Host), clientID.String); !ok {
			continue
		}
		if clientName.String != "" {
			app = clientName.String
		}
		if revoked {
			continue
		}
		if exp := parseStamp(expires); expires.Valid && (exp == nil || !now.Before(*exp)) {
			continue
		}
		// A damaged approval covers nothing: it never reads as a wider one.
		approvals, derr := decodeAgents(raw)
		if derr != nil {
			continue
		}
		for _, a := range approvals {
			if a.Agent != ag.Identity() {
				continue
			}
			// Every live approval must cover the agent, not any one: the
			// lowest cap and the narrowest reach hold, so an older, wider
			// token never outlives the owner's newer, narrower approval.
			if scope.AgentReachProblem(a, reads, ag.Emit, ask, capCents) != "" {
				return app, false, rows.Err()
			}
			covered = true
		}
	}
	return app, covered, rows.Err()
}

// agentModelPriced reports whether ag's provider row prices its model, with
// input and output rates above zero.
func (ds *dataset) agentModelPriced(ctx context.Context, ag *vocabulary.Agent) (bool, error) {
	row, err := ds.loadRowDB(ctx, eref{Kind: typeProvider, ID: ag.Provider})
	if err != nil {
		if errors.Is(err, substrate.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	if row == nil || row.DeletedAt != nil {
		return false, nil
	}
	for _, entry := range objectRows(row.Props["pricing"]) {
		if model, _ := entry["model"].(string); model != ag.Model {
			continue
		}
		p := pricingRow(entry)
		return p.inPer1M > 0 && p.outPer1M > 0, nil
	}
	return false, nil
}

// agentLabel names an agent for the owner: its id, which every surface
// shows in full.
func agentLabel(ag *vocabulary.Agent) string { return "agent " + ag.Identity() }

// appAgentTurnHold is the loop's check before a model call after the first,
// for an app's agent: the approval still live, the bundle still enabled,
// the model still priced and the agent's own cap not reached. "" lets the
// call go.
func (l *agentLoop) appAgentTurnHold(ctx context.Context) string {
	if !l.appAgent {
		return ""
	}
	ds, ag := l.ds, l.ag
	if text, err := ds.appAgentHold(ctx, ag, true); err != nil {
		return "the agent's approval could not be read: " + err.Error()
	} else if text != "" {
		return text
	}
	if err := ds.callableGroupBlocked(ctx, ag.Package, ag.Identity()); err != nil {
		return err.Error()
	}
	if c := ag.Budgets.SpendCentsPerDay; c != nil {
		agentUSD, _, err := ds.spendOf(ctx, ag)
		if err != nil {
			return "the agent's spend could not be read: " + err.Error()
		}
		if reachedCap(agentUSD, *c) {
			return fmt.Sprintf("spend cap reached: agent %s spent %d of its %d cents in the last 24 hours (budgets.spendCentsPerDay)",
				ag.Identity(), spentCents(agentUSD), *c)
		}
	}
	return ""
}
