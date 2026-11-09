package app

import (
	"net/http"
	"sort"

	"github.com/geoah/substrate/cmd/chatbridge/internal/actions"
	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/health"
	"github.com/geoah/substrate/cmd/chatbridge/internal/server"
)

// accountReport is one login on /healthz.
type accountReport struct {
	Network  string             `json:"network"`
	Account  string             `json:"account"`
	State    string             `json:"state"`
	Message  string             `json:"message,omitempty"`
	Warnings []contract.Warning `json:"warnings,omitempty"`
}

// healthDetail is /healthz's detail.
type healthDetail struct {
	Authorized bool              `json:"authorized"`
	Pusher     server.PushStatus `json:"pusher"`
	Feed       server.FeedStatus `json:"feed"`
	Accounts   []accountReport   `json:"accounts"`
}

func healthMux(a *App, p *server.Pusher, f *server.Follower, r *actions.Runner) http.Handler {
	return health.Mux(func() health.Report {
		ps := p.Status()
		fs := f.Status()
		d := healthDetail{Authorized: a.currentAuth() != nil, Pusher: ps, Feed: fs, Accounts: []accountReport{}}
		connected := 0
		for id, run := range a.Nets {
			for _, login := range run.Bridge.GetAllCachedUserLogins() {
				st, _ := run.Matrix.AccountStatusOf(string(login.ID))
				d.Accounts = append(d.Accounts, accountReport{
					Network: id, Account: string(login.ID), State: st.State,
					Message: st.Message, Warnings: st.Warnings,
				})
				if st.Connected() {
					connected++
				}
			}
		}
		sort.Slice(d.Accounts, func(i, j int) bool {
			return d.Accounts[i].Network+d.Accounts[i].Account < d.Accounts[j].Network+d.Accounts[j].Account
		})
		healthy := d.Authorized && ps.State != server.PushParked && ps.State != server.PushUnauthorized && ps.State != server.PushRefused
		pm, fm, am := p.Metrics, f.Metrics, r.Metrics
		g := func(name, kind, help string, v float64) health.Gauge {
			return health.Gauge{Name: "chatbridge_" + name, Kind: kind, Help: help, Value: v}
		}
		b := func(v bool) float64 {
			if v {
				return 1
			}
			return 0
		}
		metrics := []health.Gauge{
			g("up", "gauge", "1 while the bridge can deliver.", b(healthy)),
			g("outbox_last_seq", "gauge", "The sequence number of the newest outbox event.", float64(ps.Last)),
			g("outbox_applied_seq", "gauge", "The last sequence number the server applied.", float64(ps.Applied)),
			g("accounts_connected", "gauge", "Logins connected to their network.", float64(connected)),
			g("feed_connected", "gauge", "1 while the change feed is open.", b(fs.Connected)),
		}
		if pm != nil {
			metrics = append(metrics,
				g("batches_sent_total", "counter", "Batches the webhook door accepted.", float64(pm.BatchesSent.Load())),
				g("events_sent_total", "counter", "Events in batches the webhook door accepted.", float64(pm.EventsSent.Load())),
				g("batches_failed_total", "counter", "Deliveries the webhook door did not accept.", float64(pm.BatchesFailed.Load())),
				g("batches_parked_total", "counter", "Batches the server parked after its retries.", float64(pm.BatchesParked.Load())),
				g("batches_resent_total", "counter", "Batches sent again after they parked.", float64(pm.Resends.Load())))
		}
		if fm != nil {
			metrics = append(metrics,
				g("feed_rows_total", "counter", "Change feed rows read.", float64(fm.Rows.Load())),
				g("feed_reconnects_total", "counter", "Change feed reconnections.", float64(fm.Reconnects.Load())))
		}
		if am != nil {
			metrics = append(metrics,
				g("actions_claimed_total", "counter", "Actions claimed.", float64(am.Claimed.Load())),
				g("actions_done_total", "counter", "Actions the network carried out.", float64(am.Done.Load())),
				g("actions_failed_total", "counter", "Actions that failed.", float64(am.Failed.Load())),
				g("actions_uncertain_total", "counter", "Actions whose outcome the bridge cannot tell.", float64(am.Uncertain.Load())),
				g("actions_expired_total", "counter", "Actions that expired before they were sent.", float64(am.Expired.Load())),
				g("actions_cancelled_total", "counter", "Actions the owner cancelled before they were sent.", float64(am.Cancelled.Load())))
		}
		return health.Report{Healthy: healthy, Instance: a.Instance, Version: a.Version, Detail: d, Metrics: metrics}
	})
}
