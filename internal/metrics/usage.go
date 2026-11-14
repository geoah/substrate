package metrics

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/geoah/substrate/internal/vocabulary"
)

// The usage series say what each repository does: the changes it commits,
// the functions and agents it runs, the models it spends on, the doors it
// signs in through and the vocabulary it holds. Their labels name the
// repository, and the kinds, functions and agents it uses, so they are the
// series a deployment may want anonymized (SetUsageLabels). The operational
// series in metrics.go keep the repository's name either way: an operator
// acting on a full spool or a starved pool has to know whose it is.

// usagePolicy is how the usage labels name what a repository owns.
type usagePolicy struct {
	anonymize bool
	key       []byte
}

// usage starts anonymized, as SUBSTRATE_METRICS_ANONYMIZE does: a binary
// that never configures it names nobody.
var usage atomic.Pointer[usagePolicy]

func init() { SetUsageLabels(true, nil) }

// SetUsageLabels sets how the usage series label a repository and the names
// it owns. Anonymized, the repository and every name outside the shipped
// vocabulary become keyed hashes of secret, stable across restarts and
// replicas that share it and unreadable to a dashboard viewer who does not
// hold it; the shipped names stay in clear, since they are the product's
// features and the public tree already spells them. Otherwise every label
// is the name itself.
func SetUsageLabels(anonymize bool, secret []byte) {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("substrate/metrics/usage-labels"))
	usage.Store(&usagePolicy{anonymize: anonymize, key: m.Sum(nil)})
}

// hash is the anonymized form of name: the first 8 bytes of its keyed hash.
func (p *usagePolicy) hash(name string) string {
	m := hmac.New(sha256.New, p.key)
	m.Write([]byte(name))
	return hex.EncodeToString(m.Sum(nil)[:8])
}

// Repository is the repository label for the repository named id.
func Repository(id string) string {
	p := usage.Load()
	if !p.anonymize || id == "" {
		return id
	}
	return "repo:" + p.hash(id)
}

// Name is the label for a kind reference, a package, or a function's or
// agent's identity (`<authority>/<package>[/<name>]`) as repository uses it.
// A name the substrate ships is always in clear. Anonymized, one in the
// repository's own namespace (`~`, or its name) becomes `own:` and the hash
// of what follows the authority, so one sample imported by two repositories reads as one
// name, and any other authority's becomes `ext:` and the hash of the whole.
// Past the repository's name budget every new name is `overflow`.
func Name(repository, name string) string {
	if name == "" {
		return name
	}
	authority, rest, _ := strings.Cut(name, "/")
	if shipped(authority) {
		return name
	}
	if !admitName(repository, name) {
		return overflowLabel
	}
	p := usage.Load()
	switch {
	case !p.anonymize:
		return name
	case authority == "~" || authority == repository:
		return "own:" + p.hash(rest)
	}
	return "ext:" + p.hash(name)
}

// shipped reports whether an authority is the substrate's own publisher or
// one of its subdomains (the providers and the samples).
func shipped(authority string) bool {
	return authority == vocabulary.AuthorityPublisher || strings.HasSuffix(authority, "."+vocabulary.AuthorityPublisher)
}

// shippedProviders are the provider row ids registration seeds; any other
// row is one its owner named.
var shippedProviders = map[string]bool{"openai": true, "anthropic": true, "gemini": true}

// Provider is the label for an llm/provider row id of repository.
func Provider(repository, id string) string {
	if shippedProviders[id] {
		return id
	}
	if !admitName(repository, "provider:"+id) {
		return overflowLabel
	}
	if p := usage.Load(); p.anonymize {
		return "own:" + p.hash(id)
	}
	return id
}

// Model is the label for a model id of repository. A public model, one the
// shipped llm/model records name or an operator entry serves, is in clear;
// any other may carry its owner's organization or project, a fine-tune's id
// does, and is hashed like an owner's name.
func Model(repository, model string, public bool) string {
	if public || model == "" {
		return model
	}
	if !admitName(repository, "model:"+model) {
		return overflowLabel
	}
	if p := usage.Load(); p.anonymize {
		return "own:" + p.hash(model)
	}
	return model
}

// maxNamesPerRepository bounds the distinct names a repository's owner can
// put on the usage series. Every one is a series held for the life of the
// process, and a declaration is cheap, so without a bound one repository
// could grow the process and the Prometheus behind it without end. A real
// repository holds tens; the bound is far above that.
const maxNamesPerRepository = 1000

// overflowLabel stands for every name past a repository's budget.
const overflowLabel = "overflow"

// names is every owner-controlled name each repository has put on a series.
var names = struct {
	sync.Mutex
	seen map[string]map[string]struct{}
}{seen: map[string]map[string]struct{}{}}

// admitName reports whether name may label repository's series: one it has
// used before, or a new one within its budget.
func admitName(repository, name string) bool {
	names.Lock()
	defer names.Unlock()
	set := names.seen[repository]
	if _, ok := set[name]; ok {
		return true
	}
	if len(set) >= maxNamesPerRepository {
		return false
	}
	if set == nil {
		set = map[string]struct{}{}
		names.seen[repository] = set
	}
	set[name] = struct{}{}
	return true
}

// ForgetRepository drops the usage series of the repository named id and
// its name budget: a deleted repository's series would otherwise be held,
// and scraped, for the life of the process. The account events stay, a few
// series at most, so a deletion is still there to count; the duration
// histogram carries no repository and keeps its series.
func ForgetRepository(id string) {
	match := prometheus.Labels{"repository": Repository(id)}
	for _, v := range []interface{ DeletePartialMatch(prometheus.Labels) int }{
		Changes, FunctionInvocations, AgentRuns, LLMTokens, LLMCost, MCPToolCalls,
	} {
		v.DeletePartialMatch(match)
	}
	names.Lock()
	delete(names.seen, id)
	names.Unlock()
}

// ActorClass is the label an actor's changes count under: the type of its
// actor record (decision 0270), so the changes counter keeps one series per
// type rather than one per function, agent or app. The functions and agents
// have counters of their own. A type it does not know is `other`.
func ActorClass(actorType string) string {
	switch actorType {
	case "person", "app", "function", "agent", "package", "system":
		return actorType
	}
	return "other"
}

var (
	// Changes counts committed changelog entries, by the kind of the record
	// each moved, its op and the hand that wrote it. A replay (a rebuild, a
	// boot import) re-derives history and counts nothing.
	Changes = promauto.With(Registry).NewCounterVec(prometheus.CounterOpts{
		Name: "substrate_changes_total",
		Help: "Committed changelog entries by repository, kind, op and actor class.",
	}, []string{"repository", "kind", "op", "actor"})

	// FunctionInvocations counts function bodies run, by the mode that ran
	// them (record, schedule, webhook, manual, call) and how they ended.
	FunctionInvocations = promauto.With(Registry).NewCounterVec(prometheus.CounterOpts{
		Name: "substrate_function_invocations_total",
		Help: "Function body runs by repository, function, mode and outcome.",
	}, []string{"repository", "function", "mode", "outcome"})

	// FunctionDuration is one observation per body run. No repository label:
	// a histogram's buckets multiply every series, and the counter above
	// already says who ran what.
	FunctionDuration = promauto.With(Registry).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "substrate_function_invocation_seconds",
		Help:    "Duration of one function body run, by function.",
		Buckets: []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120, 300},
	}, []string{"function"})

	// AgentRuns counts agent runs by the mode that started them (chat,
	// subagent, call, or a trigger's) and the status they settled with.
	AgentRuns = promauto.With(Registry).NewCounterVec(prometheus.CounterOpts{
		Name: "substrate_agent_runs_total",
		Help: "Agent runs by repository, agent, mode and settled status.",
	}, []string{"repository", "agent", "mode", "status"})

	// LLMTokens counts the tokens each completion reported, by type
	// (prompt, completion, cache_read, cache_write).
	LLMTokens = promauto.With(Registry).NewCounterVec(prometheus.CounterOpts{
		Name: "substrate_llm_tokens_total",
		Help: "LLM tokens by repository, agent, provider row, model and type.",
	}, []string{"repository", "agent", "provider", "model", "type"})

	// LLMCost is the priced spend, by who pays: the operator's entry or the
	// owner's own key.
	LLMCost = promauto.With(Registry).NewCounterVec(prometheus.CounterOpts{
		Name: "substrate_llm_cost_usd_total",
		Help: "Priced LLM spend in USD by repository, agent, provider row, model and payer.",
	}, []string{"repository", "agent", "provider", "model", "payer"})

	// AuthEvents counts the account doors: registered, login, login_failed
	// and repository_deleted. A failed login is counted only for a
	// repository that exists, so a guessed name adds no series.
	AuthEvents = promauto.With(Registry).NewCounterVec(prometheus.CounterOpts{
		Name: "substrate_auth_events_total",
		Help: "Account events by repository and event.",
	}, []string{"repository", "event"})

	// MCPToolCalls counts the tools an assistant called through the MCP door.
	MCPToolCalls = promauto.With(Registry).NewCounterVec(prometheus.CounterOpts{
		Name: "substrate_mcp_tool_calls_total",
		Help: "MCP tool calls by repository, tool and outcome.",
	}, []string{"repository", "tool", "outcome"})
)

// Inventory is one reading of what a repository holds.
type Inventory struct {
	// Records is the live record count per kind.
	Records map[string]int64
	// Packages is every package the repository declares.
	Packages []PackageInventory
	// Since is when the repository's first change was written, zero when
	// the reading has none.
	Since time.Time
}

// PackageInventory is one declared package.
type PackageInventory struct {
	// Package is the package's id, `<authority>/<package>`.
	Package string
	// Origin is where it came from: builtin, published, sample (imported
	// from the shipped package named by From) or own.
	Origin string
	From   string
	// Kinds is the number of kinds it declares.
	Kinds int64
}

// inventoryCollector publishes one repository's Inventory. Its descriptors
// carry the repository as a constant label: the registry tells collectors
// apart by their descriptors, and two repositories' collectors with alike
// descriptors would be one collector to it, the second registration taking
// the first one's place.
type inventoryCollector struct {
	repository        string
	read              func() Inventory
	records, packages *prometheus.Desc
	since             *prometheus.Desc
}

func newInventoryCollector(repository string, read func() Inventory) *inventoryCollector {
	repo := prometheus.Labels{"repository": Repository(repository)}
	return &inventoryCollector{
		repository: repository, read: read,
		records:  prometheus.NewDesc("substrate_repository_records", "Live records by repository and kind.", []string{"kind"}, repo),
		packages: prometheus.NewDesc("substrate_repository_package_kinds", "Kinds each declared package holds, by repository, package and origin.", []string{"package", "origin"}, repo),
		since:    prometheus.NewDesc("substrate_repository_first_change_timestamp_seconds", "When the repository's first change was written.", nil, repo),
	}
}

func (c *inventoryCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.records
	ch <- c.packages
	ch <- c.since
}

// Collect sums what labels alike before it emits: an anonymized label may
// stand for more than one name, and a registry refuses a scrape that carries
// one label set twice.
func (c *inventoryCollector) Collect(ch chan<- prometheus.Metric) {
	inv := c.read()
	records := map[string]int64{}
	for kind, n := range inv.Records {
		records[Name(c.repository, kind)] += n
	}
	for kind, n := range records {
		ch <- prometheus.MustNewConstMetric(c.records, prometheus.GaugeValue, float64(n), kind)
	}
	type pkgKey struct{ pkg, origin string }
	packages := map[pkgKey]int64{}
	for _, p := range inv.Packages {
		name := Name(c.repository, p.Package)
		if authority, _, _ := strings.Cut(p.From, "/"); p.Origin == "sample" && shipped(authority) {
			// The shipped spelling says which sample it is, and it is
			// public, so it reads the same whether or not names are hashed.
			// An origin anyone could claim is not, and stays the package's.
			name = p.From
		}
		packages[pkgKey{name, p.Origin}] += p.Kinds
	}
	for k, n := range packages {
		ch <- prometheus.MustNewConstMetric(c.packages, prometheus.GaugeValue, float64(n), k.pkg, k.origin)
	}
	if !inv.Since.IsZero() {
		ch <- prometheus.MustNewConstMetric(c.since, prometheus.GaugeValue, float64(inv.Since.Unix()))
	}
}

// RegisterInventory publishes read as the substrate_repository_* gauges of
// repository, with RegisterDBStats's take-over rule for a repository
// reopened under the same name, and returns the matching unregister. read
// runs on every scrape, so it answers from what it already holds.
func RegisterInventory(repository string, read func() Inventory) (unregister func()) {
	return register("inventory:"+repository, newInventoryCollector(repository, read))
}
