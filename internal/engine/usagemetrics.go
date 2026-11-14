package engine

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/metrics"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// inventoryMaxAge is how stale a repository's inventory may be before a
// scrape asks for a fresh one. The reading counts every live record by kind,
// a scan of the repository's rows, so it runs at most this often and only
// while something scrapes.
const inventoryMaxAge = 5 * time.Minute

// inventoryReadTimeout bounds one reading, so a repository whose scan is slow
// holds a pool connection for no longer than this.
const inventoryReadTimeout = time.Minute

// usageInventory is a repository's last inventory reading and the state of
// the one refresh that may be running.
type usageInventory struct {
	mu         sync.Mutex
	snap       metrics.Inventory
	at         time.Time
	refreshing bool
}

// countChanges adds a committed transaction's entries to the changes counter,
// summed per label set so a bulk write costs one Add per kind.
func (ds *dataset) countChanges(pending []pendingEntry) {
	if len(pending) == 0 {
		return
	}
	type key struct{ kind, op, actor string }
	sums := map[key]float64{}
	for _, e := range pending {
		sums[key{e.Kind, e.Op, e.Actor}]++
	}
	id := ds.info.ID
	repo := metrics.Repository(id)
	for k, n := range sums {
		// The entry links its actor record; this process derived the id, so
		// the type is read without a query.
		key, _ := vocabulary.ActorKeyOf(k.actor)
		metrics.Changes.WithLabelValues(repo, metrics.Name(id, k.kind), k.op, metrics.ActorClass(substrate.ActorTypeOf(key))).Add(n)
	}
}

// countAuth records one account event of repository id. Every series names a
// repository by its id, which a rename does not move.
func countAuth(id, event string) {
	metrics.AuthEvents.WithLabelValues(metrics.Repository(id), event).Inc()
}

// forgetUsage counts a repository's deletion and drops the rest of its usage
// series.
func forgetUsage(id string) {
	countAuth(id, "repository_deleted")
	metrics.ForgetRepository(id)
}

// countInvocation records one function body run under the mode that ran it.
func (ds *dataset) countInvocation(fn *vocabulary.Function, mode string, elapsed time.Duration, err error) {
	id := ds.info.ID
	name := metrics.Name(id, fn.Identity())
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	metrics.FunctionInvocations.WithLabelValues(metrics.Repository(id), name, invocationMode(mode), outcome).Inc()
	metrics.FunctionDuration.WithLabelValues(name).Observe(elapsed.Seconds())
}

// invocationMode is the mode label: one the runner or the agent loop mints,
// or `other`, so the label set stays the set of doors.
func invocationMode(mode string) string {
	switch mode {
	case runner.ModeRecord, runner.ModeSchedule, runner.ModeWebhook, runner.ModeManual, runner.ModeCall,
		agentModeChat, agentModeSubagent:
		return mode
	}
	return "other"
}

// countRun records one agent run with the status it settled with, or
// `error` for a run that returned without one.
func (l *agentLoop) countRun(res *substrate.AgentResult, err error) {
	status := threadError
	if err == nil && res != nil {
		status = res.Status
	}
	id := l.ds.info.ID
	metrics.AgentRuns.WithLabelValues(metrics.Repository(id), metrics.Name(id, l.ag.Identity()), invocationMode(l.in.mode), status).Inc()
}

// countUsage records one completion's tokens and its priced cost.
func (l *agentLoop) countUsage(u *llm.Usage, cost float64) {
	id := l.ds.info.ID
	repo, agent, provider := metrics.Repository(id), metrics.Name(id, l.ag.Identity()), metrics.Provider(id, l.provider.id)
	// An operator entry's models are the operator's choice, and the shipped
	// records' are public: either is in clear.
	model := metrics.Model(id, l.model, l.provider.operator != nil || l.ds.svc.shippedModels[l.model])
	for typ, n := range map[string]int{
		"prompt": u.PromptTokens, "completion": u.CompletionTokens,
		"cache_read": u.CacheReadTokens, "cache_write": u.CacheWriteTokens,
	} {
		if n > 0 {
			metrics.LLMTokens.WithLabelValues(repo, agent, provider, model, typ).Add(float64(n))
		}
	}
	payer := "owner"
	if l.provider.operator != nil {
		payer = "operator"
	}
	if cost > 0 {
		metrics.LLMCost.WithLabelValues(repo, agent, provider, model, payer).Add(cost)
	}
}

// inventory is the scrape's reading: the last one taken, with a fresh one
// started in the background when it is older than inventoryMaxAge. The first
// scrape after an open answers empty and the next one has the counts.
func (ds *dataset) inventory() metrics.Inventory {
	inv := &ds.usage
	inv.mu.Lock()
	defer inv.mu.Unlock()
	if !inv.refreshing && time.Since(inv.at) >= inventoryMaxAge {
		inv.refreshing = ds.svc.spawn("usage inventory", ds.info.ID, func(ctx context.Context) {
			ctx, cancel := context.WithTimeout(ctx, inventoryReadTimeout)
			defer cancel()
			snap, err := ds.readInventory(ctx)
			inv.mu.Lock()
			defer inv.mu.Unlock()
			inv.refreshing = false
			// A failed reading waits out the same interval, so a repository
			// whose scan fails is not scanned again on every scrape.
			inv.at = time.Now()
			if err != nil {
				ds.svc.log.Warn("substrate: reading a repository's usage inventory", "repository", ds.info.ID, "error", err)
				return
			}
			inv.snap = snap
		})
	}
	return inv.snap
}

// readInventory counts the repository's live records by kind, its declared
// packages with the kinds each holds, and when its first change was written.
func (ds *dataset) readInventory(ctx context.Context) (metrics.Inventory, error) {
	out := metrics.Inventory{Records: map[string]int64{}}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT kind, count(*) FROM records WHERE deleted_at IS NULL GROUP BY kind`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var kind string
		var n int64
		if err := rows.Scan(&kind, &n); err != nil {
			_ = rows.Close()
			return out, err
		}
		out.Records[kind] = n
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return out, err
	}
	// A kind record's id is its reference, so the kinds of package p are the
	// ids under `p/`.
	rows, err = ds.db.QueryContext(ctx, `
		SELECT p.id, coalesce(p.props->>'source', ''), coalesce(p.props->>'origin', ''), count(k.id)
		FROM records p
		LEFT JOIN records k ON k.kind = $2 AND k.deleted_at IS NULL AND k.id LIKE p.id || '/%'
		WHERE p.kind = $1 AND p.deleted_at IS NULL
		GROUP BY 1, 2, 3`, vocabulary.PackageCore+"/package", vocabulary.PackageCore+"/kind")
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var p metrics.PackageInventory
		var source string
		if err := rows.Scan(&p.Package, &source, &p.From, &p.Kinds); err != nil {
			_ = rows.Close()
			return out, err
		}
		p.Origin, p.From = packageOrigin(p.Package, source, p.From)
		out.Packages = append(out.Packages, p)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return out, err
	}
	err = ds.db.QueryRowContext(ctx, `SELECT ts FROM changelog ORDER BY seq LIMIT 1`).Scan(&out.Since)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	return out, nil
}

// samplesAuthority publishes the sample packages (samples/samples.go).
const samplesAuthority = "samples." + vocabulary.AuthorityPublisher

// packageOrigin names where a package came from, and the shipped sample it
// was copied from: the seed (builtin), an install of a published package, a
// sample (imported and rehomed, its origin a shipped sample's id, or
// installed under the shipped spelling) or its owner's own. A hand apply may
// claim any origin (vocabularywrite.go claimedOrigin), so one outside the
// samples is the owner's own copy and its origin names nothing public.
func packageOrigin(pkg, source, from string) (origin, shipped string) {
	switch {
	case source == "builtin" || source == "published":
		return source, ""
	case strings.HasPrefix(from, samplesAuthority+"/"):
		return "sample", from
	case strings.HasPrefix(pkg, samplesAuthority+"/"):
		return "sample", pkg
	}
	return "own", ""
}

// shippedModelIDs is the set of model ids the shipped llm/model records name.
func shippedModelIDs(records []vocabulary.ShippedRecord) map[string]bool {
	out := map[string]bool{}
	for _, r := range records {
		if r.Kind != vocabulary.PackageLLM+"/model" {
			continue
		}
		if m, ok := r.Properties["model"].(string); ok && m != "" {
			out[m] = true
		}
	}
	return out
}
