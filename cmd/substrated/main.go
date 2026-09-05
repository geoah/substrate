// substrate: the Substrate substrate service — one Postgres, ONE schema shared by
// every repository, the versioned REST resource API + GraphQL + the watch
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

	// Before anything opens: the credential key seals every signing seed and
	// unwraps every DEK, so a host without valid key material may not boot.
	if err := cfg.Validate(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// There is no embedder here, and no LLM gateway either. Both are a
	// REPOSITORY's data: an llmprovider row names the endpoint, the key and
	// (for embeddings) the model, and the engine resolves it per repository
	// per pass. The process holds no key that could reach a
	// repository-chosen endpoint.

	// Where blob bytes live. The default is the `blobs` bytea column, so a
	// deployment that sets nothing keeps a database dump as a whole backup.
	blobs, err := cfg.Blobs.Backend()
	if err != nil {
		return err
	}
	if blobs.Name() != blobbytes.BackendPostgres {
		slog.Info("blob bytes are stored outside Postgres: a database dump is no longer a whole backup",
			"backend", blobs.Name())
	}

	opts := []engine.Option{
		engine.WithKindsFS(kinds.Seed()),
		engine.WithCredentialKey(cfg.CredentialKey),
		engine.WithBlobStore(blobs),
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
	if err != nil {
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
	start("trigger dispatch", triggersInterval, func(ctx context.Context) { dispatchTriggers(ctx, svc) })
	// The drain runs unconditionally: whether a repository embeds is its own
	// row's answer, given fresh on every pass, so a provider written after
	// boot starts draining without a restart.
	start("embed queue", embedInterval, func(ctx context.Context) { drainEmbeds(ctx, svc) })

	// BOTH SHIPPED TREES: the provider packages under kinds/ and the sample
	// packages under samples/. A catalog built from one of them serves half
	// the shipped set and 404s every install of the other.
	cat, err := catalog.Load(kinds.Bundles(), samples.Samples())
	if err != nil {
		return err
	}
	for _, warn := range cat.Warnings() {
		slog.Warn("catalog: dropped a shipped bundle directory", "detail", warn)
	}
	slog.Info("catalog loaded", "bundles", len(cat.Bundles()))

	if cfg.InviteCode == "" {
		slog.Info("no SUBSTRATE_INVITE_CODE: registration is closed")
	}
	handler := api.New(api.Config{
		Service:             svc,
		WebDir:              cfg.WebDir,
		Catalog:             cat,
		ConsoleURL:          cfg.ConsoleURL,
		InviteCode:          cfg.InviteCode,
		TOTPDisabled:        cfg.InsecureDisableTOTP,
		MaxDialect:          engine.MaxSchemaDialect(),
		MaxChangelogDialect: engine.MaxChangelogDialect(),
	})
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

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case sig := <-stopCh:
		slog.Info("shutting down", "signal", sig.String())
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
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
// never ends the pass: an unopenable repository, an unimplemented seam and a
// failing call all log and CONTINUE, or the alphabet would decide whose
// triggers run.
func repositoryDatasets(ctx context.Context, svc substrate.Service) []substrate.Dataset {
	repos, err := svc.Repositories(ctx)
	if err != nil {
		slog.Error("list repositories", "error", err)
		return nil
	}
	out := make([]substrate.Dataset, 0, len(repos))
	for _, r := range repos {
		ds, err := svc.Dataset(ctx, r.Name)
		if err != nil {
			slog.Error("open repository", "repository", r.Name, "error", err)
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
			slog.Error("gc sweep", "repository", ds.Repository().Name, "error", err)
			continue
		}
		if n > 0 {
			slog.Info("gc sweep", "repository", ds.Repository().Name, "collected", n)
		}
	}
}

func sweepResolutions(ctx context.Context, svc substrate.Service) {
	for _, ds := range repositoryDatasets(ctx, svc) {
		rs, ok := ds.(substrate.ResolutionSweeper)
		if !ok {
			continue
		}
		n, err := rs.SweepResolutions(ctx)
		if err != nil {
			slog.Error("resolution sweep", "repository", ds.Repository().Name, "error", err)
			continue
		}
		if n > 0 {
			slog.Info("resolution sweep", "repository", ds.Repository().Name, "resumed", n)
		}
	}
}

// dispatchTriggers runs one dispatcher pass per repository: each enabled trigger
// drains its changelog backlog to head (record sources) or fires its due
// occurrence (schedule sources), serially. The pass cadence is the schedule
// ticker: due RRULE occurrences are computed here, missed passes coalescing
// to one fire.
func dispatchTriggers(ctx context.Context, svc substrate.Service) {
	for _, ds := range repositoryDatasets(ctx, svc) {
		fr, ok := ds.(substrate.TriggerDispatcher)
		if !ok {
			continue
		}
		n, err := fr.ProcessTriggers(ctx)
		if err != nil {
			slog.Error("trigger dispatch", "repository", ds.Repository().Name, "error", err)
			continue
		}
		if n > 0 {
			slog.Info("trigger dispatch", "repository", ds.Repository().Name, "ran", n)
		}
	}
}

func maintainOAuth(ctx context.Context, svc substrate.Service) {
	for _, ds := range repositoryDatasets(ctx, svc) {
		om, ok := ds.(substrate.OAuthMaintainer)
		if !ok {
			continue
		}
		if n, err := om.RefreshOAuthTokens(ctx); err != nil {
			slog.Error("oauth refresh", "repository", ds.Repository().Name, "error", err)
		} else if n > 0 {
			slog.Info("oauth refresh", "repository", ds.Repository().Name, "refreshed", n)
		}
		if n, err := om.ProcessOAuthFinalizers(ctx); err != nil {
			slog.Error("oauth finalizers", "repository", ds.Repository().Name, "error", err)
		} else if n > 0 {
			slog.Info("oauth finalizers", "repository", ds.Repository().Name, "released", n)
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
			slog.Error("embed queue", "repository", ds.Repository().Name, "error", err)
			continue
		}
		if n > 0 {
			slog.Info("embed queue", "repository", ds.Repository().Name, "embedded", n)
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
