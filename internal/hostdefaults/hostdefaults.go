// Package hostdefaults reads the host's defaults file (SUBSTRATE_HOST_DEFAULTS):
// the services a host pays for on behalf of the repositories it serves, and
// the daily budget that bounds them (docs/plans/host-defaults.md, decision
// 0230). A deployment without the file has no host defaults, and nothing in
// this package runs.
//
// The file holds no key material itself. Every credential is a reference, `{file:
// /path}` or `{env: NAME}`, resolved once at Load and held in this process
// only: no record, sealed value, changelog line or export ever carries it.
package hostdefaults

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/embed"
	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/vocabulary"
)

// maxFile bounds the host file. A host listing every shipped provider with a
// dozen models each is a few kilobytes.
const maxFile = 256 << 10

// maxSecret bounds one key file: a vendor key or a client secret is under a
// kilobyte, so a file far past this is a wrong path.
const maxSecret = 16 << 10

// The cache TTL words a host entry may name, as the llm/provider kind spells
// them (an enum value is a lowercase word, so the wire's `1h` cannot be one).
const (
	CacheTTLFiveMinutes = "fiveminutes"
	CacheTTLOneHour     = "onehour"
)

// Config is a host file that passed Load. Its maps are keyed by what each
// entry stands in for: an llm/provider row id, or a published package.
type Config struct {
	// Install is the published packages every repository is given (phase 2;
	// read and validated, acted on by nothing yet).
	Install []string
	// LLM is keyed by the llm/provider row id each entry serves.
	LLM map[string]*LLM
	// OAuthClients is keyed by the published provider package each client
	// signs in for (phase 2; read and validated, acted on by nothing yet).
	OAuthClients map[string]*OAuthClient
	// FunctionSecrets is keyed by the published provider package whose named
	// functions receive the key (phase 3; read and validated, acted on by
	// nothing yet).
	FunctionSecrets map[string]*FunctionSecret
	// Budget is the per-repository daily allowance every host-paid call
	// reserves against.
	Budget Budget
}

// LLM is one host transport: everything a call on an empty, host-listed
// llm/provider row is made with. None of it comes from the row.
type LLM struct {
	// ID is the llm/provider row id this entry serves.
	ID      string
	Wire    llm.Wire
	BaseURL string
	APIKey  Secret
	// Models are the only models the entry serves, each priced. A request
	// naming any other is refused.
	Models []Model
	// MaxOutputTokens is sent where a request names no completion ceiling and
	// caps one that names more; it is also what a reservation prices the
	// completion at.
	MaxOutputTokens int
	// TitleModel titles chat threads; empty titles a thread with the model it
	// ran. It is one of Models.
	TitleModel string
	// Defaults are request params under the agent's own, validated as a
	// provider row's are.
	Defaults map[string]any
	// Cache is the entry's cache block; nil is the wire's own default.
	Cache *Cache
	// Headers ride every request the entry makes.
	Headers []Header
	// Embeddings, on at most one entry of the file, is the embeddings
	// provider of a repository whose rows claim none.
	Embeddings *Embeddings
}

// Model is one model a host entry serves, with its prices in USD per million
// tokens as exact digits. The cache rates are optional, as on a provider row.
type Model struct {
	Model           string
	InputPer1M      string
	OutputPer1M     string
	CacheReadPer1M  string
	CacheWritePer1M string
	ContextWindow   int
}

// ModelIDs lists the models an entry serves, in the file's order.
func (l *LLM) ModelIDs() []string {
	out := make([]string, len(l.Models))
	for i, m := range l.Models {
		out[i] = m.Model
	}
	return out
}

// Model returns the entry's model named id.
func (l *LLM) Model(id string) (Model, bool) {
	for _, m := range l.Models {
		if m.Model == id {
			return m, true
		}
	}
	return Model{}, false
}

// Cache is an entry's cache block, as an llm/provider row's.
type Cache struct {
	// TTL is CacheTTLFiveMinutes, CacheTTLOneHour, or empty for the run's
	// own choice.
	TTL         string
	Key         bool
	Breakpoints bool
}

// Header is one request header an entry sends.
type Header struct {
	Name  string
	Value string
}

// Embeddings is the host's embeddings provider: the model, bought on its
// entry's transport, and its price in USD per million input tokens.
type Embeddings struct {
	Model string
	Per1M string
}

// OAuthClient is a host OAuth client for one published provider. The id and
// the secret are one pair, never mixed with a repository's own.
type OAuthClient struct {
	ClientID     string
	ClientSecret Secret
	// Notice is the host's own words about the client, shown beside it (a
	// client in Google's Testing mode, say); empty for none.
	Notice string
}

// FunctionSecret is a key the host hands to named functions of one published
// provider, priced per call in cents.
type FunctionSecret struct {
	APIKey    Secret
	Consumers []string
	// PricePerCall is cents per call of each consumer, exact digits, one per
	// consumer.
	PricePerCall map[string]string
}

// Budget is the per-repository daily allowance in US cents.
type Budget struct {
	CentsPerDay int64
}

// Secret is key material read from a file or an environment variable. Its
// value never prints: String, GoString and every encoding answer the
// reference, so a Config handed to a log line carries no key.
type Secret struct {
	// File and Env are the reference as the host file wrote it; exactly one
	// is set.
	File  string
	Env   string
	value string
}

// Value is the key material itself, for the one call that pays with it.
func (s Secret) Value() string { return s.value }

// String names where the secret was read from, never its value.
func (s Secret) String() string {
	if s.File != "" {
		return "{file: " + s.File + "}"
	}
	if s.Env != "" {
		return "{env: " + s.Env + "}"
	}
	return "{}"
}

// GoString is String, so %#v does not print the value either.
func (s Secret) GoString() string { return s.String() }

// MarshalText is String, so no encoding carries the value.
func (s Secret) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// NewSecret is a secret holding value, for a test that builds a Config by
// hand. Load is the only path a running server takes.
func NewSecret(value string) Secret { return Secret{value: value} }

// EmbeddingsEntry is the one entry carrying `embeddings`, or nil.
func (c *Config) EmbeddingsEntry() *LLM {
	if c == nil {
		return nil
	}
	for _, id := range slices.Sorted(maps.Keys(c.LLM)) {
		if c.LLM[id].Embeddings != nil {
			return c.LLM[id]
		}
	}
	return nil
}

// Packages is every published package the file names, under install,
// oauthClients and functionSecrets, sorted and without repeats.
func (c *Config) Packages() []string {
	set := map[string]bool{}
	for _, p := range c.Install {
		set[p] = true
	}
	for p := range c.OAuthClients {
		set[p] = true
	}
	for p := range c.FunctionSecrets {
		set[p] = true
	}
	return slices.Sorted(maps.Keys(set))
}

// CheckPackages refuses a file naming a package the binary does not ship as a
// provider. It runs once the catalog has loaded, so it is a second step after
// Load, and the boot refuses on it the same way.
func (c *Config) CheckPackages(isProvider func(id string) bool) error {
	if c == nil {
		return nil
	}
	var bad []string
	for _, p := range c.Packages() {
		if !isProvider(p) {
			bad = append(bad, p)
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("names %s, which this binary does not ship as a provider", strings.Join(bad, ", "))
	}
	return nil
}

// Load reads and validates the host file at path. An empty path is no host
// file: Load returns nil and no error, and the server behaves exactly as it
// does without the feature. Every refusal names the problem and never a key,
// because the caller refuses to boot on it and logs the text.
func Load(path string) (*Config, error) {
	if path == "" {
		return nil, nil
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%q is not an absolute path", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxFile {
		return nil, fmt.Errorf("larger than %d bytes", maxFile)
	}
	return Parse(raw, filepath.Dir(path))
}

// Parse validates a host file's bytes and resolves its secrets. dir is where
// a relative `{file: ...}` would resolve against; a key file must be named by
// an absolute path, so dir is only quoted in a refusal.
func Parse(raw []byte, dir string) (*Config, error) {
	var doc document
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("is empty")
		}
		return nil, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("holds more than one YAML document")
	}
	return doc.validate(dir)
}

// document is the file as YAML spells it. Its key set is closed at every
// level (KnownFields), so a misspelled key is a refusal at boot rather than a
// setting that silently does nothing.
type document struct {
	Install         []string                      `yaml:"install"`
	LLM             map[string]*llmDoc            `yaml:"llm"`
	OAuthClients    map[string]*oauthClientDoc    `yaml:"oauthClients"`
	FunctionSecrets map[string]*functionSecretDoc `yaml:"functionSecrets"`
	Budget          *budgetDoc                    `yaml:"budget"`
}

type llmDoc struct {
	Wire            string         `yaml:"wire"`
	BaseURL         string         `yaml:"baseURL"`
	APIKey          *secretDoc     `yaml:"apiKey"`
	Models          []modelDoc     `yaml:"models"`
	MaxOutputTokens int            `yaml:"maxOutputTokens"`
	TitleModel      string         `yaml:"titleModel"`
	Defaults        map[string]any `yaml:"defaults"`
	Cache           *cacheDoc      `yaml:"cache"`
	Headers         []headerDoc    `yaml:"headers"`
	Embeddings      *embeddingsDoc `yaml:"embeddings"`
}

type modelDoc struct {
	Model           string   `yaml:"model"`
	InputPer1M      *decimal `yaml:"inputPer1M"`
	OutputPer1M     *decimal `yaml:"outputPer1M"`
	CacheReadPer1M  *decimal `yaml:"cacheReadPer1M"`
	CacheWritePer1M *decimal `yaml:"cacheWritePer1M"`
	ContextWindow   int      `yaml:"contextWindow"`
}

type cacheDoc struct {
	TTL         string `yaml:"ttl"`
	Key         bool   `yaml:"key"`
	Breakpoints bool   `yaml:"breakpoints"`
}

type headerDoc struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

type embeddingsDoc struct {
	Model string   `yaml:"model"`
	Per1M *decimal `yaml:"per1M"`
}

type oauthClientDoc struct {
	ClientID     string     `yaml:"clientId"`
	ClientSecret *secretDoc `yaml:"clientSecret"`
	Notice       string     `yaml:"notice"`
}

type functionSecretDoc struct {
	APIKey       *secretDoc          `yaml:"apiKey"`
	Consumers    []string            `yaml:"consumers"`
	PricePerCall map[string]*decimal `yaml:"pricePerCall"`
}

type budgetDoc struct {
	CentsPerDay *int64 `yaml:"centsPerDay"`
}

// secretDoc is a key reference: a mapping with exactly one of `file` and
// `env`. A scalar is refused without quoting it, because a scalar in this
// position is most likely the key itself.
type secretDoc struct {
	File string
	Env  string
}

func (s *secretDoc) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: key material is read from a file or an environment variable: write {file: /path} or {env: NAME}, never the key itself", n.Line)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if v.Kind != yaml.ScalarNode {
			return fmt.Errorf("line %d: %s is a path or a variable name", v.Line, k.Value)
		}
		switch k.Value {
		case "file":
			s.File = v.Value
		case "env":
			s.Env = v.Value
		default:
			return fmt.Errorf("line %d: unknown key %q in a key reference (the keys are file, env)", k.Line, k.Value)
		}
	}
	if (s.File == "") == (s.Env == "") {
		return fmt.Errorf("line %d: a key reference names exactly one of file and env", n.Line)
	}
	return nil
}

// decimal is a non-negative price as exact digits. A quoted string and a bare
// YAML number are both read from the node's own text, so a price never rides
// a float on its way in.
type decimal string

var reDecimal = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

func (d *decimal) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode || !reDecimal.MatchString(n.Value) {
		return fmt.Errorf("line %d: a price is a non-negative decimal such as \"1.25\"", n.Line)
	}
	*d = decimal(n.Value)
	return nil
}

func (d *decimal) String() string {
	if d == nil {
		return ""
	}
	return string(*d)
}

// reHeaderName is RFC 9110's token, the grammar of a field name.
var reHeaderName = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

func (d *document) validate(dir string) (*Config, error) {
	c := &Config{
		LLM:             map[string]*LLM{},
		OAuthClients:    map[string]*OAuthClient{},
		FunctionSecrets: map[string]*FunctionSecret{},
	}
	if d.Budget == nil || d.Budget.CentsPerDay == nil {
		return nil, errors.New("budget.centsPerDay is required: every host-paid call reserves against it")
	}
	if *d.Budget.CentsPerDay < 0 {
		return nil, errors.New("budget.centsPerDay is a whole number of cents at or above 0")
	}
	c.Budget.CentsPerDay = *d.Budget.CentsPerDay

	seen := map[string]bool{}
	for i, p := range d.Install {
		if err := checkPackage(p); err != nil {
			return nil, fmt.Errorf("install[%d]: %w", i, err)
		}
		if seen[p] {
			return nil, fmt.Errorf("install[%d]: %s is listed twice", i, p)
		}
		seen[p] = true
		c.Install = append(c.Install, p)
	}

	var embedders []string
	for _, id := range slices.Sorted(maps.Keys(d.LLM)) {
		entry, err := d.LLM[id].validate(id, dir)
		if err != nil {
			return nil, fmt.Errorf("llm.%s: %w", id, err)
		}
		if entry.Embeddings != nil {
			embedders = append(embedders, id)
		}
		c.LLM[id] = entry
	}
	if len(embedders) > 1 {
		return nil, fmt.Errorf("llm: %s each name embeddings; a host serves embeddings from one entry", strings.Join(embedders, ", "))
	}

	for _, p := range slices.Sorted(maps.Keys(d.OAuthClients)) {
		doc := d.OAuthClients[p]
		if err := checkPackage(p); err != nil {
			return nil, fmt.Errorf("oauthClients.%s: %w", p, err)
		}
		if doc == nil {
			return nil, fmt.Errorf("oauthClients.%s: clientId and clientSecret are required", p)
		}
		if strings.TrimSpace(doc.ClientID) == "" {
			return nil, fmt.Errorf("oauthClients.%s: clientId is required", p)
		}
		secret, err := resolve(doc.ClientSecret, dir)
		if err != nil {
			return nil, fmt.Errorf("oauthClients.%s.clientSecret: %w", p, err)
		}
		c.OAuthClients[p] = &OAuthClient{
			ClientID: strings.TrimSpace(doc.ClientID), ClientSecret: secret,
			Notice: strings.TrimSpace(doc.Notice),
		}
	}

	for _, p := range slices.Sorted(maps.Keys(d.FunctionSecrets)) {
		doc := d.FunctionSecrets[p]
		if err := checkPackage(p); err != nil {
			return nil, fmt.Errorf("functionSecrets.%s: %w", p, err)
		}
		if doc == nil {
			return nil, fmt.Errorf("functionSecrets.%s: apiKey, consumers and pricePerCall are required", p)
		}
		key, err := resolve(doc.APIKey, dir)
		if err != nil {
			return nil, fmt.Errorf("functionSecrets.%s.apiKey: %w", p, err)
		}
		if len(doc.Consumers) == 0 {
			return nil, fmt.Errorf("functionSecrets.%s: consumers names the functions that receive the key, and is required", p)
		}
		fs := &FunctionSecret{APIKey: key, PricePerCall: map[string]string{}}
		for _, name := range doc.Consumers {
			if !vocabulary.ValidName(name) {
				return nil, fmt.Errorf("functionSecrets.%s.consumers: %q is not a function name", p, name)
			}
			if slices.Contains(fs.Consumers, name) {
				return nil, fmt.Errorf("functionSecrets.%s.consumers: %s is listed twice", p, name)
			}
			price := doc.PricePerCall[name]
			if price == nil {
				return nil, fmt.Errorf("functionSecrets.%s.pricePerCall: %s has no price; every consumer is priced per call", p, name)
			}
			fs.Consumers = append(fs.Consumers, name)
			fs.PricePerCall[name] = price.String()
		}
		for name := range doc.PricePerCall {
			if !slices.Contains(fs.Consumers, name) {
				return nil, fmt.Errorf("functionSecrets.%s.pricePerCall: %s is not one of its consumers", p, name)
			}
		}
		c.FunctionSecrets[p] = fs
	}
	return c, nil
}

func (d *llmDoc) validate(id, dir string) (*LLM, error) {
	if !vocabulary.ValidID(id) {
		return nil, errors.New("is not an llm/provider record id")
	}
	if d == nil {
		return nil, errors.New("wire, apiKey, models and maxOutputTokens are required")
	}
	e := &LLM{ID: id, Wire: llm.Wire(d.Wire), TitleModel: d.TitleModel, Defaults: d.Defaults}
	policy, known := e.Wire.Policy()
	if !known {
		return nil, fmt.Errorf("wire %q is not one of %s", d.Wire, llm.WireNames())
	}
	e.BaseURL = strings.TrimSpace(d.BaseURL)
	if e.BaseURL == "" && policy.RequiresBaseURL {
		return nil, fmt.Errorf("baseURL is required: the %s wire has no endpoint of its own", e.Wire)
	}
	if e.BaseURL != "" {
		u, err := url.Parse(e.BaseURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return nil, errors.New("baseURL is an https URL with a host and no credentials in it")
		}
	}
	key, err := resolve(d.APIKey, dir)
	if err != nil {
		return nil, fmt.Errorf("apiKey: %w", err)
	}
	e.APIKey = key
	if len(d.Models) == 0 {
		return nil, errors.New("models is required: the entry serves only the models it lists, each priced")
	}
	for i, m := range d.Models {
		if strings.TrimSpace(m.Model) == "" {
			return nil, fmt.Errorf("models[%d]: model is required", i)
		}
		if _, dup := e.Model(m.Model); dup {
			return nil, fmt.Errorf("models[%d]: %s is listed twice", i, m.Model)
		}
		if m.InputPer1M == nil || m.OutputPer1M == nil {
			return nil, fmt.Errorf("models[%d]: %s has no price; inputPer1M and outputPer1M are required", i, m.Model)
		}
		if m.ContextWindow < 0 {
			return nil, fmt.Errorf("models[%d]: contextWindow is a token count at or above 0", i)
		}
		e.Models = append(e.Models, Model{
			Model: m.Model, InputPer1M: m.InputPer1M.String(), OutputPer1M: m.OutputPer1M.String(),
			CacheReadPer1M: m.CacheReadPer1M.String(), CacheWritePer1M: m.CacheWritePer1M.String(),
			ContextWindow: m.ContextWindow,
		})
	}
	if d.MaxOutputTokens < 1 {
		return nil, errors.New("maxOutputTokens is required: a positive whole number of tokens, the most one host-paid completion may write")
	}
	e.MaxOutputTokens = d.MaxOutputTokens
	if e.TitleModel != "" {
		if _, ok := e.Model(e.TitleModel); !ok {
			return nil, fmt.Errorf("titleModel %s is not one of models", e.TitleModel)
		}
	}
	if len(d.Defaults) > 0 {
		if _, ok := d.Defaults[vocabulary.AgentParamEngineGuidance]; ok {
			return nil, fmt.Errorf("defaults.%s is an agent's own, not a request param", vocabulary.AgentParamEngineGuidance)
		}
		if _, err := vocabulary.ParseAgentParams(d.Defaults); err != nil {
			return nil, fmt.Errorf("defaults.%w", err)
		}
	}
	if d.Cache != nil {
		switch d.Cache.TTL {
		case "", CacheTTLFiveMinutes, CacheTTLOneHour:
		default:
			return nil, fmt.Errorf("cache.ttl %q is one of %s, %s", d.Cache.TTL, CacheTTLFiveMinutes, CacheTTLOneHour)
		}
		e.Cache = &Cache{TTL: d.Cache.TTL, Key: d.Cache.Key, Breakpoints: d.Cache.Breakpoints}
	}
	for i, h := range d.Headers {
		if !reHeaderName.MatchString(h.Name) {
			return nil, fmt.Errorf("headers[%d]: %q is not a header name", i, h.Name)
		}
		if strings.ContainsAny(h.Value, "\r\n\x00") {
			return nil, fmt.Errorf("headers[%d]: the value of %s holds a line break", i, h.Name)
		}
		// Go's transport replays a request carrying either header after a
		// lost response on a reused connection, inside one Do: a second
		// billable attempt the reservation never admitted, and that
		// RetryOnlyUnbilled cannot see.
		switch http.CanonicalHeaderKey(h.Name) {
		case "Idempotency-Key", "X-Idempotency-Key":
			return nil, fmt.Errorf("headers[%d]: %s makes the HTTP transport replay a request it may already have been billed for", i, h.Name)
		}
		for _, prior := range e.Headers {
			if http.CanonicalHeaderKey(prior.Name) == http.CanonicalHeaderKey(h.Name) {
				return nil, fmt.Errorf("headers[%d]: %s is listed twice", i, h.Name)
			}
		}
		e.Headers = append(e.Headers, Header(h))
	}
	if d.Embeddings != nil {
		if !policy.Embeddings {
			return nil, fmt.Errorf("embeddings: the %s wire has no embeddings endpoint; embeddings are bought on the openai wire", e.Wire)
		}
		model := strings.TrimSpace(d.Embeddings.Model)
		if model == "" {
			return nil, errors.New("embeddings.model is required")
		}
		dim, ok := embed.ModelDim(model)
		if !ok {
			return nil, fmt.Errorf("embeddings.model %s: its width is not known here; name one of %s", model, strings.Join(embed.KnownModels(), ", "))
		}
		if dim != embed.Dim {
			return nil, fmt.Errorf("embeddings.model %s is %d wide; stored vectors are %d wide", model, dim, embed.Dim)
		}
		if d.Embeddings.Per1M == nil {
			return nil, fmt.Errorf("embeddings: %s has no price; per1M is required", model)
		}
		e.Embeddings = &Embeddings{Model: model, Per1M: d.Embeddings.Per1M.String()}
	}
	return e, nil
}

// checkPackage refuses a string that is not a package identity,
// `<authority>/<package>`.
func checkPackage(p string) error {
	authority, name := vocabulary.SplitPackageRef(p)
	if authority == "" || !vocabulary.ValidAuthority(authority) || !vocabulary.ValidPackage(name) {
		return fmt.Errorf("%q is not a package identity, <authority>/<package>", p)
	}
	return nil
}

// resolve reads one key reference. A missing file, an unset variable and an
// empty value are each refused: a host that meant to pay for a service and
// boots without its key would hand every repository a service that fails.
func resolve(ref *secretDoc, dir string) (Secret, error) {
	if ref == nil {
		return Secret{}, errors.New("is required: {file: /path} or {env: NAME}")
	}
	s := Secret{File: ref.File, Env: ref.Env}
	if ref.File != "" {
		if !filepath.IsAbs(ref.File) {
			return Secret{}, fmt.Errorf("file %q is not an absolute path (the host file is in %s)", ref.File, dir)
		}
		f, err := os.Open(ref.File)
		if err != nil {
			return Secret{}, fmt.Errorf("file %s: %w", ref.File, errors.Unwrap(err))
		}
		defer func() { _ = f.Close() }()
		raw, err := io.ReadAll(io.LimitReader(f, maxSecret+1))
		if err != nil {
			return Secret{}, fmt.Errorf("file %s: %w", ref.File, err)
		}
		if len(raw) > maxSecret {
			return Secret{}, fmt.Errorf("file %s is larger than %d bytes, which no key is", ref.File, maxSecret)
		}
		s.value = strings.TrimSpace(string(raw))
		if s.value == "" {
			return Secret{}, fmt.Errorf("file %s is empty", ref.File)
		}
		return s, nil
	}
	v, ok := os.LookupEnv(ref.Env)
	if !ok {
		return Secret{}, fmt.Errorf("env %s is not set", ref.Env)
	}
	s.value = strings.TrimSpace(v)
	if s.value == "" {
		return Secret{}, fmt.Errorf("env %s is empty", ref.Env)
	}
	return s, nil
}
