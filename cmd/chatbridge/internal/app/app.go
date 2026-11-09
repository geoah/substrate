// Package app wires the bridge together: the store, one bridge per network,
// the pusher, the feed follower, the action runner and the health server.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"github.com/geoah/substrate/cmd/chatbridge/internal/actions"
	"github.com/geoah/substrate/cmd/chatbridge/internal/config"
	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/networks"
	"github.com/geoah/substrate/cmd/chatbridge/internal/server"
	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

// App is one bridge process.
type App struct {
	Cfg      config.Config
	Version  string
	Log      *slog.Logger
	ZLog     zerolog.Logger
	Store    *store.Store
	Instance string
	Settings *actions.Settings
	Nets     map[string]*networks.Running

	unlock func()

	authMu sync.RWMutex
	auth   *server.Auth
}

// ErrBusy is a store another process holds.
var ErrBusy = errors.New("another chatbridge process holds this store; stop it first")

// Open opens the store and takes its lock.
func Open(ctx context.Context, cfg config.Config, version string, log *slog.Logger, zlog zerolog.Logger) (*App, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}
	unlock, err := lock(filepath.Join(cfg.DataDir, "chatbridge.lock"))
	if err != nil {
		return nil, err
	}
	st, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		unlock()
		return nil, err
	}
	instance, err := st.InstanceID(ctx)
	if err != nil {
		_ = st.Close()
		unlock()
		return nil, err
	}
	a := &App{
		Cfg: cfg, Version: version, Log: log, ZLog: zlog, Store: st, Instance: instance,
		Settings: actions.NewSettings(), Nets: map[string]*networks.Running{}, unlock: unlock,
	}
	refs, err := st.AccountRefs(ctx)
	if err != nil {
		a.Close()
		return nil, err
	}
	a.Settings.Load(ctx, st, refs)
	a.loadAuth(ctx)
	return a, nil
}

// Close closes the store and releases the lock.
func (a *App) Close() {
	_ = a.Store.Close()
	if a.unlock != nil {
		a.unlock()
	}
}

// LoadAuth reads the bridge's standing with its server from the store.
func LoadAuth(ctx context.Context, st *store.Store) (*server.Auth, error) {
	raw, ok, err := st.Get(ctx, store.KeyAuth)
	if err != nil || !ok || raw == "" {
		return nil, err
	}
	var auth server.Auth
	if err := json.Unmarshal([]byte(raw), &auth); err != nil {
		return nil, fmt.Errorf("the stored authorization is unreadable: %w", err)
	}
	return &auth, nil
}

// SaveAuth keeps an authorization in the store. The webhook URL is moved
// onto the address the bridge reaches the server at, which may differ from
// the server's public name.
func SaveAuth(ctx context.Context, st *store.Store, auth *server.Auth) error {
	if u, err := url.Parse(auth.WebhookURL); err == nil {
		if base, err := url.Parse(auth.Server); err == nil && base.Host != "" {
			u.Scheme, u.Host = base.Scheme, base.Host
			auth.WebhookURL = u.String()
		}
	}
	raw, err := json.Marshal(auth)
	if err != nil {
		return err
	}
	return st.Set(ctx, store.KeyAuth, string(raw))
}

func (a *App) loadAuth(ctx context.Context) {
	auth, err := LoadAuth(ctx, a.Store)
	if err != nil {
		a.Log.Error("reading the stored authorization failed", "error", err)
		return
	}
	a.authMu.Lock()
	a.auth = auth
	a.authMu.Unlock()
}

func (a *App) currentAuth() *server.Auth {
	a.authMu.RLock()
	defer a.authMu.RUnlock()
	return a.auth
}

func (a *App) token() string {
	if auth := a.currentAuth(); auth != nil {
		return auth.Token
	}
	return ""
}

func (a *App) webhookURL() string {
	if auth := a.currentAuth(); auth != nil {
		return auth.WebhookURL
	}
	return ""
}

// OpenNetworks builds the bridge of every configured network.
func (a *App) OpenNetworks(opts networks.Options, only ...string) error {
	want := a.Cfg.Networks
	if len(only) > 0 {
		want = only
	}
	for _, spec := range networks.Registry() {
		if len(want) > 0 && !slices.Contains(want, spec.ID) {
			continue
		}
		run, err := networks.Open(spec, a.Store, a.Settings, opts, a.Log, a.ZLog)
		if err != nil {
			return err
		}
		a.Nets[spec.ID] = run
	}
	for _, id := range want {
		if _, ok := a.Nets[id]; !ok {
			return fmt.Errorf("this build carries no network %q", id)
		}
	}
	if len(a.Nets) == 0 {
		return errors.New("no network to run")
	}
	return nil
}

// hello appends instance.hello when what it says changed.
func (a *App) hello(ctx context.Context) error {
	ev := &contract.InstanceHello{Head: contract.Head{Type: contract.TypeInstanceHello}, Name: a.Cfg.Name, Version: a.Version}
	ids := make([]string, 0, len(a.Nets))
	for id := range a.Nets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		ev.Networks = append(ev.Networks, contract.NetworkInfo{Network: id, DisplayName: a.Nets[id].Spec.DisplayName})
	}
	raw, _ := json.Marshal([]any{ev.Name, ev.Version, ev.Networks})
	return a.Store.Do(ctx, func(tx *store.Tx) error {
		if tx.GetKV(ctx, "instance.hello") == string(raw) {
			return nil
		}
		if _, err := tx.Append(ctx, ev); err != nil {
			return err
		}
		return tx.SetKV(ctx, "instance.hello", string(raw))
	})
}

// actionNetworks is what the runner needs of each running network.
func (a *App) actionNetworks() map[string]*actions.Network {
	out := map[string]*actions.Network{}
	for id, run := range a.Nets {
		out[id] = &actions.Network{
			ID:    id,
			Exec:  run.Matrix,
			TxnID: run.Spec.TxnID,
			Runs: func(account string) bool {
				return run.Bridge.GetCachedUserLoginByID(networkid.UserLoginID(account)) != nil
			},
			Unlink: func(ctx context.Context, account string, forget bool) error {
				if login := run.Bridge.GetCachedUserLoginByID(networkid.UserLoginID(account)); login != nil {
					login.Logout(ctx)
				}
				return run.Matrix.AccountRemoved(ctx, account, forget)
			},
			Pause: func(ctx context.Context, account string, paused bool) {
				login := run.Bridge.GetCachedUserLoginByID(networkid.UserLoginID(account))
				if login == nil {
					return
				}
				if paused {
					login.Disconnect()
				} else {
					login.Client.Connect(login.Log.WithContext(ctx))
				}
			},
		}
	}
	return out
}

// Run runs the bridge until ctx ends.
func (a *App) Run(ctx context.Context) error {
	if err := a.OpenNetworks(networks.Options{DeviceName: a.Cfg.DeviceName, History: networks.HistoryRecent}); err != nil {
		return err
	}
	if a.currentAuth() == nil {
		a.Log.Warn("the bridge is not authorized yet: run chatbridge authorize; events wait in the outbox until then")
	}
	client := server.NewDynamicClient(func() string {
		if auth := a.currentAuth(); auth != nil {
			return auth.Server
		}
		return ""
	}, a.token, nil)
	runner := &actions.Runner{Store: a.Store, Records: client, Networks: a.actionNetworks(), Settings: a.Settings, Log: a.Log}
	for _, run := range a.Nets {
		run.Matrix.SetListener(runner)
	}
	if err := a.hello(ctx); err != nil {
		return err
	}
	for id, run := range a.Nets {
		if err := run.Start(ctx); err != nil {
			return fmt.Errorf("starting %s: %w", id, err)
		}
	}
	defer func() {
		for _, run := range a.Nets {
			run.Stop(10 * time.Second)
		}
	}()
	if err := runner.Recover(ctx); err != nil {
		return fmt.Errorf("settling the claims of the last run: %w", err)
	}
	pusher := &server.Pusher{Store: a.Store, Client: client, Instance: a.Instance, WebhookURL: a.webhookURL, Log: a.Log}
	follower := &server.Follower{Store: a.Store, Client: client, Instance: a.Instance, Pusher: pusher, Sink: runner, Log: a.Log}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	start := func(name string, fn func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(ctx); err != nil && !errors.Is(err, context.Canceled) {
				errs <- fmt.Errorf("%s: %w", name, err)
			}
		}()
	}
	start("the action runner", runner.Run)
	start("the pusher", pusher.Run)
	start("the feed follower", follower.Run)
	start("the media collector", a.collectMedia)
	start("the authorization reader", a.refreshAuth)

	srv := &http.Server{
		Addr: a.Cfg.ListenAddr, ReadHeaderTimeout: 5 * time.Second,
		Handler: healthMux(a, pusher, follower, runner),
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.Log.Error("the health server failed", "error", err)
		}
	}()
	a.Log.Info("the chat bridge is running", "instance", a.Instance, "version", a.Version, "server", client.Host(),
		"networks", len(a.Nets), "listen", a.Cfg.ListenAddr)

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errs:
	}
	cancel()
	shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	_ = srv.Shutdown(shutdown)
	wg.Wait()
	return runErr
}

// refreshAuth reads the authorization again every half minute, so a
// `chatbridge authorize` while the bridge runs takes effect.
func (a *App) refreshAuth(ctx context.Context) error {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			a.loadAuth(ctx)
		}
	}
}

// collectMedia deletes the files no pending event names, once a day old.
func (a *App) collectMedia(ctx context.Context) error {
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if n, err := a.Store.CollectMedia(ctx, time.Now().Add(-24*time.Hour)); err != nil {
				a.Log.Warn("collecting media failed", "error", err)
			} else if n > 0 {
				a.Log.Debug("collected media", "files", n)
			}
		}
	}
}

// PrintStatus writes what a store holds, for `chatbridge status`.
func PrintStatus(ctx context.Context, st *store.Store, dir string, w io.Writer) error {
	instance, err := st.InstanceID(ctx)
	if err != nil {
		return err
	}
	auth, err := LoadAuth(ctx, st)
	if err != nil {
		return err
	}
	applied, _ := st.GetInt(ctx, store.KeyApplied)
	last, _ := st.Last(ctx)
	depth, _ := st.Depth(ctx, applied)
	fmt.Fprintf(w, "instance:     %s\n", instance)
	fmt.Fprintf(w, "store:        %s\n", dir)
	if auth == nil {
		fmt.Fprintln(w, "server:       not authorized (run chatbridge authorize)")
	} else {
		fmt.Fprintf(w, "server:       %s\n", auth.Server)
		fmt.Fprintf(w, "repository:   %s\n", auth.Repository)
		if auth.ExpiresAt != nil {
			fmt.Fprintf(w, "token ends:   %s\n", auth.ExpiresAt.Format(time.RFC3339))
		}
	}
	fmt.Fprintf(w, "outbox:       last %d, applied %d, waiting %d\n", last, applied, depth)
	refs, err := st.AccountRefs(ctx)
	if err != nil {
		return err
	}
	for _, r := range refs {
		fmt.Fprintf(w, "account:      %s %s\n", r.Network, r.LoginID)
	}
	return nil
}
