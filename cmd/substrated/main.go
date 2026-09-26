// substrate: the Substrate substrate service — one Postgres, ONE schema shared by
// every repository, the versioned REST resource API + the watch
// streams, and the background loops (GC sweep, embed queue drain, triggers,
// oauth), each running over every repository the control-plane table holds.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/geoah/substrate/internal/api"
	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/config"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/kinds"
	"github.com/geoah/substrate/samples"
)

const (
	gcInterval = 5 * time.Minute
	// resumeInterval is the resolution sweep's cadence: the recovery path for
	// resumes a restart or a lost lease dropped, so a slow tick suffices —
	// the common case is delivered by the resolving transaction itself.
	resumeInterval   = 2 * time.Minute
	embedInterval    = time.Minute
	embedBatch       = 64
	triggersInterval = 5 * time.Second
	oauthInterval    = time.Minute
	// triggerDispatchPasses caps the repository passes the dispatcher runs at
	// once. A pass takes a connection per statement or transaction and none
	// while a function body runs, but each pass has a runner process or a
	// transaction in flight most of the time, so a host with hundreds of
	// repositories must not run hundreds of passes side by side. Eight bounds
	// the dispatcher to eight runner processes and eight transactions.
	triggerDispatchPasses = 8
)

func main() {
	// FIRST, before anything reads configuration or opens a connection: every
	// moment between execve and this call is a moment a same-uid process can
	// read this one's environment, and the environment is where the credential
	// key and the database URL are. The window cannot be closed entirely,
	// there is always some: only made as small as a Go program can make it.
	if err := hideProcess(); err != nil {
		slog.Error("fatal", "error", logSafeError(err))
		os.Exit(1)
	}

	if err := run(); err != nil {
		slog.Error("fatal", "error", logSafeError(err))
		os.Exit(1)
	}
}

// logSafeError renders a fatal error into a log line without letting the value
// that caused it forge one. Everything this binary fails on carries something
// somebody typed (a DSN, an invite code, a repository name read back from the
// store), and a control character in any of them would end the line and start a
// second, so they are stripped and the text is capped. It is the engine's
// logSafeID rule (internal/engine/triggers.go) applied to prose: an error is
// not an id, so the text is kept and repaired rather than discarded.
func logSafeError(err error) string {
	if err == nil {
		return ""
	}
	const maxLen = 2000
	var b strings.Builder
	for _, r := range err.Error() {
		if b.Len() >= maxLen {
			break
		}
		if r < 0x20 || r == 0x7f {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	setupLogger(cfg.LogLevel)
	reportSandbox()
	reportTrust()

	// Before anything opens: the credential key unwraps every repository's
	// DEK, so a host without valid key material may not boot.
	if err := cfg.Validate(); err != nil {
		return err
	}

	ctx, cancelCause := context.WithCancelCause(context.Background())
	cancel := func() { cancelCause(nil) }
	defer cancel()

	// SIGNALS ARE HEARD FROM HERE, not from the listener on. engine.Open runs
	// the boot check, and a boot that imports a long repository directory
	// takes minutes; a SIGTERM left to its default action ends the import
	// with nothing in the log, and the next boot's "resuming an interrupted
	// import" has no cause beside it. The first signal during the boot
	// cancels it, logged, so the boot check names the repository it stopped
	// on; a second one takes the default action, for a boot that does not
	// return promptly.
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)
	booted := make(chan struct{})
	go func() {
		select {
		case sig := <-stopCh:
			signal.Stop(stopCh)
			slog.Warn("boot interrupted before the listener was up; the next boot resumes it",
				"signal", sig.String(), "action", "interrupted")
			cancelCause(fmt.Errorf("signal %s", sig))
		case <-booted:
		}
	}()
	go bootHeartbeat(slog.Default(), booted, bootHeartbeatEvery)

	// There is no embedder here, and no LLM gateway either. Both are a
	// REPOSITORY's data: an llm/provider row names the endpoint, the key and
	// (for embeddings) the model, and the engine resolves it per repository
	// per pass. The process holds no key that could reach a
	// repository-chosen endpoint.

	// Where blob bytes live: under the data root, in each repository's own
	// directory, beside everything else that repository needs to come back.
	// There is nothing to configure, because there is one backend.
	blobs, err := blobbytes.NewFS(cfg.Data.Root)
	if err != nil {
		return err
	}

	opts := []engine.Option{
		engine.WithKindsFS(kinds.Seed()),
		engine.WithDataRoot(cfg.Data.Root),
		engine.WithChangelogSegmentBytes(cfg.Data.ChangelogSegmentBytes),
		engine.WithCredentialKey(cfg.CredentialKey),
		engine.WithBlobStore(blobs),
		engine.WithConversionCeiling(cfg.ConversionCeiling),
		engine.WithRepositoryConnections(cfg.RepositoryConnections),
		engine.WithOrphanCollection(cfg.OrphanGrace),
	}
	if cfg.OrphanGrace > 0 {
		// Loud, and at boot: this deployment DELETES records the sweep finds
		// orphaned, and the window is the only thing between a connector
		// outage and the collection.
		slog.Warn("orphan collection is on: the gc sweep tombstones a mapping target whose sources have all been gone for this long",
			"grace", cfg.OrphanGrace)
	}
	if cfg.OAuthCallbackURL != "" {
		stateKey := cfg.OAuthStateKey
		if stateKey == "" {
			// A per-boot key keeps the facility usable in dev; in-flight
			// consents do not survive a restart until the env pins one.
			stateKey = randomStateKey()
			slog.Warn("no SUBSTRATE_OAUTH_STATE_KEY: oauth states will not survive a restart")
		}
		opts = append(opts, engine.WithOAuth(stateKey, cfg.OAuthCallbackURL, nil))
	}
	if cfg.InsecureDisableTOTP {
		// Loud, and at boot: from here on a password is the whole credential.
		slog.Warn("SUBSTRATE_INSECURE_DISABLE_TOTP is set: the second factor is NOT verified — local development only")
		opts = append(opts, engine.WithInsecureDisableTOTP())
	}
	svc, err := engine.Open(ctx, cfg.DatabaseURL, opts...)
	close(booted)
	if err != nil {
		if ctx.Err() != nil {
			// The signal above, already logged: a shutdown, not a fault.
			return nil
		}
		return err
	}
	defer func() { _ = svc.Close() }()

	// The loops share the service, so shutdown has an ORDER: cancel, then wait
	// for every pass in flight to return, and only THEN let the deferred
	// svc.Close() run. Closing under a running pass would pull the pools out
	// from under a transaction — a torn write on the way out, on the one path
	// nobody watches. This defer is the barrier: registered AFTER the Close
	// defer so it runs BEFORE it, and it cancels first itself, because a
	// listener error returns with the loops still live and a Wait on them would
	// hang the exit.
	//
	// This barrier covers the TICKER loops only. The engine's own detached
	// tasks — the judge, a notified thread's resume, the open-time function
	// warm — are counted by the service and drained inside svc.Close(), which
	// is why they are not added here.
	var loops sync.WaitGroup
	defer func() {
		cancel()
		loops.Wait()
	}()
	start := func(name string, every time.Duration, fn func(context.Context)) {
		loops.Add(1)
		go func() {
			defer loops.Done()
			loop(ctx, name, every, fn)
		}()
	}
	start("gc sweep", gcInterval, func(ctx context.Context) { sweepGC(ctx, svc) })
	start("oauth maintenance", oauthInterval, func(ctx context.Context) { maintainOAuth(ctx, svc) })
	start("resolution sweep", resumeInterval, func(ctx context.Context) { sweepResolutions(ctx, svc) })
	// The dispatcher's per-repository passes outlive the tick that started
	// them, so they are counted on the same barrier as the loops.
	start("trigger dispatch", triggersInterval, newTriggerDispatcher(svc, &loops).dispatch)
	// The drain runs unconditionally: whether a repository embeds is its own
	// row's answer, given fresh on every pass, so a provider written after
	// boot starts draining without a restart.
	start("embed queue", embedInterval, func(ctx context.Context) { drainEmbeds(ctx, svc) })

	// BOTH SHIPPED TREES: the provider packages under kinds/ and the sample
	// packages under samples/. A catalog built from one of them serves half
	// the shipped set and 404s every install of the other.
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		return err
	}
	for _, warn := range cat.Warnings() {
		slog.Warn("catalog: dropped a shipped bundle directory", "detail", warn)
	}
	slog.Info("catalog loaded", "bundles", len(cat.Bundles()))

	if cfg.InviteCode == "" {
		// Loud, and at boot: anyone who can reach this port may create a user.
		slog.Warn("no SUBSTRATE_INVITE_CODE: registration asks for no invite code — set one before anyone else can reach this substrate")
	}
	handler := api.New(api.Config{
		Service:      svc,
		WebDir:       cfg.WebDir,
		Catalog:      cat,
		ConsoleURL:   cfg.ConsoleURL,
		InviteCode:   cfg.InviteCode,
		TOTPDisabled: cfg.InsecureDisableTOTP,
		Metrics:      cfg.Metrics,
	})
	if cfg.Metrics {
		// Loud, and at boot, like the other doors: this port answers /metrics
		// with no token, and only the deployment keeps it off the internet.
		slog.Info("SUBSTRATE_METRICS is set: /metrics is served unauthenticated — keep the path off the ingress")
	}
	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "port", cfg.Port)
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case sig := <-stopCh:
		slog.Info("shutting down", "signal", sig.String())
	case <-ctx.Done():
		// A signal that landed as the boot returned.
		slog.Info("shutting down", "cause", context.Cause(ctx).Error())
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// bootHeartbeatEvery is how often a boot that has not reached the listener
// logs that it is still running. The schema migrations and the boot check's
// import log nothing while they work, and a first boot on a new database
// beside a long data root spends minutes there, so the heartbeat is how a
// waiter tells a slow boot from a hung one. .mise/dev.sh's readiness poll
// stops a server whose log has been silent for 30 s: keep this well under it.
const bootHeartbeatEvery = 10 * time.Second

// bootHeartbeat logs, every `every` until done is closed, that the boot is
// still running and for how long.
func bootHeartbeat(log *slog.Logger, done <-chan struct{}, every time.Duration) {
	begun := time.Now()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			log.Info("still booting: the listener opens when the migrations and the boot check finish",
				"elapsed", time.Since(begun).Round(time.Second).String())
		}
	}
}

func loop(ctx context.Context, name string, every time.Duration, fn func(context.Context)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pass(ctx, name, fn)
		}
	}
}

// pass runs one tick of a loop with a recover around it. The recover belongs
// HERE rather than at each callback, so a loop added later cannot be the
// unprotected one: a pass reaches the same agent machinery a request does (the
// resolution sweep continues a thread, the dispatcher delivers a trigger),
// nothing above it recovers, and a panic in one pass would otherwise end the
// process for every request in flight. The cadence survives it: one bad pass is
// a bad pass, not the end of the loop.
func pass(ctx context.Context, name string, fn func(context.Context)) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("background loop panicked and was contained; this pass did no more work",
				"loop", name, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	fn(ctx)
}

// repositoryDatasets opens every repository the control-plane table holds; the
// loops run over all of them. There is no control-plane repository to include
// any more — the ledger is one table, read on the maintenance pool.
//
// THE LOOP DISCIPLINE, which every loop below keeps: enumerate on the
// maintenance pool, then do each repository's WORK on that repository's own
// scoped pool — so a loop is bound by row level security exactly like a request
// is, and the maintenance bypass is spent on the listing alone. One repository
// never ends the pass: an unopenable repository and a failing call both log
// and CONTINUE, or the alphabet would decide whose triggers run.
func repositoryDatasets(ctx context.Context, svc substrate.Service) []substrate.Dataset {
	repos, err := svc.Repositories(ctx)
	if err != nil {
		slog.Error("list repositories", "error", err)
		return nil
	}
	out := make([]substrate.Dataset, 0, len(repos))
	for _, r := range repos {
		ds, err := svc.Dataset(ctx, r.ID)
		if err != nil {
			slog.Error("open repository", "repository", r.ID, "error", err)
			continue
		}
		out = append(out, ds)
	}
	return out
}

func sweepGC(ctx context.Context, svc substrate.Service) {
	for _, ds := range repositoryDatasets(ctx, svc) {
		n, err := ds.RunGC(ctx)
		if err != nil {
			slog.Error("gc sweep", "repository", ds.Repository().ID, "error", err)
			continue
		}
		if n > 0 {
			slog.Info("gc sweep", "repository", ds.Repository().ID, "collected", n)
		}
	}
}

func sweepResolutions(ctx context.Context, svc substrate.Service) {
	for _, ds := range repositoryDatasets(ctx, svc) {
		n, err := ds.SweepResolutions(ctx)
		if err != nil {
			slog.Error("resolution sweep", "repository", ds.Repository().ID, "error", err)
			continue
		}
		if n > 0 {
			slog.Info("resolution sweep", "repository", ds.Repository().ID, "resumed", n)
		}
	}
}

// triggerDispatcher runs each repository's dispatcher pass in a goroutine of
// its own: every enabled trigger drains its changelog backlog (record sources)
// or fires its due occurrence (schedule sources). The pass cadence is the
// schedule ticker: due RRULE occurrences are computed there, missed passes
// coalescing to one fire.
//
// ONE LANE PER REPOSITORY (#639). The control plane lists repositories oldest
// first, and a pass once ran for as long as its slowest trigger's backlog, so
// a serial walk left a new repository's first delivery waiting behind every
// older repository's whole drain. Now each tick queues one pass for every
// repository that has none running or queued, and the queue runs up to
// triggerDispatchPasses passes at once. A pass that ends starts the next
// queued one at once rather than on the next tick, so with passes that
// return quickly every repository still gets a pass per tick, however many
// repositories there are. A repository never has two passes running or
// queued, so its triggers keep their one-at-a-time delivery order and no two
// passes race one repository's cursors. What bounds a slot's hold is the
// engine's per-trigger budget (triggerPassBudget): a pass runs about the sum
// of its triggers' budgets, overrunning by at most one delivery per trigger,
// each bounded by the runner's timeout.
//
// THE LONGEST WAIT GOES FIRST. A queue filled in listing order would put the
// oldest repositories ahead on every tick, and the newest would starve behind
// busy ones exactly as it did under the serial walk. So each tick queues its
// repositories by when their last pass started, one that never ran first, and
// the queue keeps what earlier ticks put in it ahead of them.
type triggerDispatcher struct {
	svc substrate.Service
	// passes counts the goroutines, and is the shutdown barrier's own group:
	// svc.Close must not run under a pass still in flight.
	passes *sync.WaitGroup

	mu sync.Mutex
	// claimed holds every repository with a pass running or queued.
	claimed map[string]bool
	queue   []string
	running int
	// started is the turn each repository's last pass started on; a
	// repository missing from it has never run. turn is a counter rather than
	// a clock so two starts never tie.
	started map[string]uint64
	turn    uint64
}

func newTriggerDispatcher(svc substrate.Service, passes *sync.WaitGroup) *triggerDispatcher {
	return &triggerDispatcher{svc: svc, passes: passes, claimed: map[string]bool{}, started: map[string]uint64{}}
}

// dispatch is one tick: a pass queued for every repository with none running
// or queued, longest waiting first, and the queue started up to the cap.
func (d *triggerDispatcher) dispatch(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	repos, err := d.svc.Repositories(ctx)
	if err != nil {
		// No listing is not an empty listing: the turn records stay.
		slog.Error("list repositories", "error", err)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	listed := make(map[string]bool, len(repos))
	var eligible []string
	for _, r := range repos {
		// Checked against listed too, so a repository the listing names twice
		// is queued once: one pass per repository is this type's rule to
		// hold, not the listing's.
		if !d.claimed[r.ID] && !listed[r.ID] {
			eligible = append(eligible, r.ID)
		}
		listed[r.ID] = true
	}
	// A repository that left the listing takes its turn record with it.
	for id := range d.started {
		if !listed[id] {
			delete(d.started, id)
		}
	}
	// Stable, so repositories that never ran keep the listing's order.
	sort.SliceStable(eligible, func(i, j int) bool { return d.started[eligible[i]] < d.started[eligible[j]] })
	for _, id := range eligible {
		d.claimed[id] = true
		d.queue = append(d.queue, id)
	}
	d.startLocked(ctx)
}

// startLocked starts queued passes until the cap is reached or the queue is
// empty. The caller holds d.mu. A canceled context starts nothing: the
// process is shutting down and the queue is abandoned with it.
func (d *triggerDispatcher) startLocked(ctx context.Context) {
	for d.running < triggerDispatchPasses && len(d.queue) > 0 && ctx.Err() == nil {
		id := d.queue[0]
		d.queue = d.queue[1:]
		d.running++
		d.turn++
		d.started[id] = d.turn
		d.passes.Add(1)
		go d.run(ctx, id)
	}
}

// run is one repository's pass in its slot. Releasing the slot starts the
// next queued pass before the barrier's Done, so the WaitGroup never reaches
// zero with a pass about to start.
func (d *triggerDispatcher) run(ctx context.Context, id string) {
	defer d.passes.Done()
	defer func() {
		d.mu.Lock()
		d.running--
		delete(d.claimed, id)
		d.startLocked(ctx)
		d.mu.Unlock()
	}()
	// The loop's recover does not reach this goroutine, so the pass carries
	// its own.
	pass(ctx, "trigger dispatch", func(ctx context.Context) {
		// Opened in the slot, so a first open's ladder counts against the cap
		// like the pass it precedes.
		ds, err := d.svc.Dataset(ctx, id)
		if err != nil {
			slog.Error("open repository", "repository", id, "error", err)
			return
		}
		dispatchRepository(ctx, ds)
	})
}

// dispatchRepository runs one repository's dispatcher pass and logs it.
func dispatchRepository(ctx context.Context, ds substrate.Dataset) {
	n, err := ds.ProcessTriggers(ctx)
	if err != nil {
		slog.Error("trigger dispatch", "repository", ds.Repository().ID, "error", err)
		return
	}
	if n > 0 {
		slog.Info("trigger dispatch", "repository", ds.Repository().ID, "ran", n)
	}
}

func maintainOAuth(ctx context.Context, svc substrate.Service) {
	for _, ds := range repositoryDatasets(ctx, svc) {
		if n, err := ds.RefreshOAuthTokens(ctx); err != nil {
			slog.Error("oauth refresh", "repository", ds.Repository().ID, "error", err)
		} else if n > 0 {
			slog.Info("oauth refresh", "repository", ds.Repository().ID, "refreshed", n)
		}
		if n, err := ds.ProcessOAuthFinalizers(ctx); err != nil {
			slog.Error("oauth finalizers", "repository", ds.Repository().ID, "error", err)
		} else if n > 0 {
			slog.Info("oauth finalizers", "repository", ds.Repository().ID, "released", n)
		}
	}
}

// randomStateKey mints a per-boot HMAC key for the dev path with no
// configured one.
func randomStateKey() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)
}

func drainEmbeds(ctx context.Context, svc substrate.Service) {
	for _, ds := range repositoryDatasets(ctx, svc) {
		n, err := ds.ProcessEmbedQueue(ctx, embedBatch)
		if err != nil {
			slog.Error("embed queue", "repository", ds.Repository().ID, "error", err)
			continue
		}
		if n > 0 {
			slog.Info("embed queue", "repository", ds.Repository().ID, "embedded", n)
		}
	}
}

func setupLogger(level string) {
	lvl := slog.LevelInfo
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
}
