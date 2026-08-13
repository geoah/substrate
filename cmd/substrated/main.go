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
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/geoah/substrate/internal/api"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/config"
	"github.com/geoah/substrate/internal/embed"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/kinds"
)

const (
	gcInterval       = 5 * time.Minute
	embedInterval    = time.Minute
	embedBatch       = 64
	triggersInterval = 5 * time.Second
	oauthInterval    = time.Minute
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	setupLogger(cfg.LogLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	embedClient, err := embed.New(embed.Config{
		BaseURL: cfg.LLMBaseURL,
		APIKey:  cfg.LLMAPIKey,
		Model:   cfg.LLMEmbedModel,
	})
	if err != nil {
		return err
	}
	// A typed nil pointer must not become a non-nil interface.
	var embedder substrate.Embedder
	if embedClient != nil {
		embedder = embedClient
	} else {
		slog.Warn("SUBSTRATE_LLM_BASE_URL or SUBSTRATE_LLM_API_KEY unset: embed queue will not drain")
	}

	opts := []engine.Option{
		engine.WithKindsFS(kinds.Seed()),
		// The agent loop's gateway fallbacks: an llmprovider row's own
		// baseURL/apiKey win over these.
		engine.WithLLMGateway(cfg.LLMBaseURL, cfg.LLMAPIKey),
		engine.WithCredentialKey(cfg.CredentialKey),
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
	if embedder != nil {
		opts = append(opts, engine.WithEmbedder(embedder))
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
	var loops sync.WaitGroup
	defer func() {
		cancel()
		loops.Wait()
	}()
	start := func(every time.Duration, fn func(context.Context)) {
		loops.Add(1)
		go func() {
			defer loops.Done()
			loop(ctx, every, fn)
		}()
	}
	start(gcInterval, func(ctx context.Context) { sweepGC(ctx, svc) })
	start(oauthInterval, func(ctx context.Context) { maintainOAuth(ctx, svc) })
	start(triggersInterval, func(ctx context.Context) { dispatchTriggers(ctx, svc) })
	if embedder != nil {
		start(embedInterval, func(ctx context.Context) { drainEmbeds(ctx, svc, embedder) })
	}

	cat, err := catalog.Load(kinds.Bundles())
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
		Service:    svc,
		WebDir:     cfg.WebDir,
		Catalog:    cat,
		ConsoleURL: cfg.ConsoleURL,
		InviteCode: cfg.InviteCode,
		MaxDialect: engine.MaxSchemaDialect(),
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

func loop(ctx context.Context, every time.Duration, fn func(context.Context)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn(ctx)
		}
	}
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

// triggerDispatcher is the engine's dispatcher seam, off the frozen Dataset
// interface like ResetUser is off Service.
type triggerDispatcher interface {
	ProcessTriggers(ctx context.Context) (int, error)
}

// dispatchTriggers runs one dispatcher pass per repository: each enabled trigger
// drains its changelog backlog to head (record sources) or fires its due
// occurrence (schedule sources), serially. The pass cadence is the schedule
// ticker: due RRULE occurrences are computed here, missed passes coalescing
// to one fire.
func dispatchTriggers(ctx context.Context, svc substrate.Service) {
	for _, ds := range repositoryDatasets(ctx, svc) {
		fr, ok := ds.(triggerDispatcher)
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

// oauthMaintainer is the engine's OAuth seam, off the frozen Dataset
// interface like the dispatcher is: refresh keeps stored tokens fresh, the
// finalizer pass revokes and releases deleted accounts ahead of GC.
type oauthMaintainer interface {
	RefreshOAuthTokens(ctx context.Context) (int, error)
	ProcessOAuthFinalizers(ctx context.Context) (int, error)
}

func maintainOAuth(ctx context.Context, svc substrate.Service) {
	for _, ds := range repositoryDatasets(ctx, svc) {
		om, ok := ds.(oauthMaintainer)
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

func drainEmbeds(ctx context.Context, svc substrate.Service, e substrate.Embedder) {
	for _, ds := range repositoryDatasets(ctx, svc) {
		n, err := ds.ProcessEmbedQueue(ctx, e, embedBatch)
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
