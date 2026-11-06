package operatordefaults

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// keyFile writes a key into a file under the test's own directory and
// returns its path. The value is a fixture, never a real key.
func keyFile(t *testing.T, value string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(p, []byte(value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// validFile is a defaults file every refusal case edits one line of.
func validFile(t *testing.T) string {
	t.Helper()
	t.Setenv("OPERATORDEFAULTS_TEST_ANTHROPIC", "anthropic-fixture")
	return fmt.Sprintf(`install:
  - providers.substrate.reamde.dev/google
llm:
  openai:
    wire: openai
    baseURL: https://llm.example.com/v1
    apiKey: {file: %s}
    models:
      - {model: gpt-5, inputPer1M: "1.25", outputPer1M: "10", contextWindow: 400000}
      - {model: gpt-5-mini, inputPer1M: 0.25, outputPer1M: 2}
    maxOutputTokens: 16000
    titleModel: gpt-5-mini
    defaults: {reasoningEffort: low}
    cache: {key: true, ttl: onehour}
    headers:
      - {name: X-Title, value: substrate}
    embeddings: {model: text-embedding-3-small, per1M: "0.02"}
  anthropic:
    wire: anthropic
    apiKey: {env: OPERATORDEFAULTS_TEST_ANTHROPIC}
    models:
      - {model: claude-sonnet-5-5, inputPer1M: "3", outputPer1M: "15"}
    maxOutputTokens: 8000
oauthClients:
  providers.substrate.reamde.dev/google:
    clientId: 1234.apps.example.com
    clientSecret: {file: %s}
    notice: Ask us to add your address first.
functionSecrets:
  providers.substrate.reamde.dev/firecrawl:
    apiKey: {env: OPERATORDEFAULTS_TEST_ANTHROPIC}
    consumers: [websearch, webscrape]
    pricePerCall: {websearch: "0.1", webscrape: "0.1"}
budget:
  centsPerDay: 200
`, keyFile(t, "openai-fixture"), keyFile(t, "client-secret-fixture"))
}

func TestParseReadsAWholeFile(t *testing.T) {
	c, err := Parse([]byte(validFile(t)), "/etc")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	oa := c.LLM["openai"]
	if oa == nil || oa.APIKey.Value() != "openai-fixture" || oa.BaseURL != "https://llm.example.com/v1" {
		t.Fatalf("openai entry = %+v", oa)
	}
	if got := strings.Join(oa.ModelIDs(), ","); got != "gpt-5,gpt-5-mini" {
		t.Fatalf("models = %s", got)
	}
	if m, _ := oa.Model("gpt-5-mini"); m.InputPer1M != "0.25" {
		t.Fatalf("a bare YAML number lost its digits: %q", m.InputPer1M)
	}
	if oa.MaxOutputTokens != 16000 || oa.TitleModel != "gpt-5-mini" || oa.Cache == nil || oa.Cache.TTL != CacheTTLOneHour || !oa.Cache.Key {
		t.Fatalf("openai entry = %+v", oa)
	}
	if e := c.EmbeddingsEntry(); e == nil || e.ID != "openai" || e.Embeddings.Model != "text-embedding-3-small" {
		t.Fatalf("embeddings entry = %+v", e)
	}
	if c.LLM["anthropic"].APIKey.Value() != "anthropic-fixture" {
		t.Fatal("the env key did not resolve")
	}
	if c.Budget.CentsPerDay != 200 {
		t.Fatalf("budget = %d", c.Budget.CentsPerDay)
	}
	if got := c.OAuthClients["providers.substrate.reamde.dev/google"]; got == nil || got.ClientSecret.Value() != "client-secret-fixture" || got.Notice == "" {
		t.Fatalf("oauth client = %+v", got)
	}
	fs := c.FunctionSecrets["providers.substrate.reamde.dev/firecrawl"]
	if fs == nil || len(fs.Consumers) != 2 || fs.PricePerCall["websearch"] != "0.1" {
		t.Fatalf("function secret = %+v", fs)
	}
	if got := strings.Join(c.Packages(), ","); got != "providers.substrate.reamde.dev/firecrawl,providers.substrate.reamde.dev/google" {
		t.Fatalf("packages = %s", got)
	}
}

// A Config printed any way a log line might print it carries no key.
func TestASecretNeverPrints(t *testing.T) {
	c, err := Parse([]byte(validFile(t)), "/etc")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, out := range []string{
		fmt.Sprintf("%v", c.LLM["openai"]), fmt.Sprintf("%+v", c.LLM["openai"]), fmt.Sprintf("%#v", c.LLM["openai"]),
		fmt.Sprintf("%v", c.OAuthClients), fmt.Sprintf("%+v", *c.LLM["anthropic"]),
	} {
		for _, key := range []string{"openai-fixture", "anthropic-fixture", "client-secret-fixture"} {
			if strings.Contains(out, key) {
				t.Fatalf("a printed config carries a key: %s", out)
			}
		}
	}
}

func TestParseRefuses(t *testing.T) {
	cases := []struct {
		name, from, to, want string
	}{
		{"a missing key file", "apiKey: {file: ", "apiKey: {file: /nonexistent/key-", "llm.openai: apiKey: file /nonexistent/key-"},
		{"an unset variable", "{env: OPERATORDEFAULTS_TEST_ANTHROPIC}\n    models", "{env: OPERATORDEFAULTS_TEST_UNSET}\n    models", "env OPERATORDEFAULTS_TEST_UNSET is not set"},
		{"an inline key", "apiKey: {env: OPERATORDEFAULTS_TEST_ANTHROPIC}\n    models", "apiKey: sk-inline-fixture\n    models", "never the key itself"},
		{"a key reference naming both", "apiKey: {env: OPERATORDEFAULTS_TEST_ANTHROPIC}\n    models", "apiKey: {env: OPERATORDEFAULTS_TEST_ANTHROPIC, file: /x}\n    models", "exactly one of file and env"},
		{"a plain http base URL", "https://llm.example.com/v1", "http://llm.example.com/v1", "baseURL is an https URL"},
		{"a base URL carrying credentials", "https://llm.example.com/v1", "https://user:pw@llm.example.com/v1", "baseURL is an https URL"},
		{"an openai entry with no base URL", "    baseURL: https://llm.example.com/v1\n", "", "baseURL is required"},
		{"a model without a price", `{model: gpt-5, inputPer1M: "1.25", outputPer1M: "10", contextWindow: 400000}`, `{model: gpt-5, inputPer1M: "1.25"}`, "gpt-5 has no price"},
		{"a negative price", `outputPer1M: "10"`, `outputPer1M: "-10"`, "a price is a non-negative decimal"},
		{"an entry with no models", "    models:\n      - {model: claude-sonnet-5-5, inputPer1M: \"3\", outputPer1M: \"15\"}\n", "", "models is required"},
		{"no maxOutputTokens", "    maxOutputTokens: 8000\n", "", "maxOutputTokens is required"},
		{"a title model off the list", "titleModel: gpt-5-mini", "titleModel: gpt-4", "titleModel gpt-4 is not one of models"},
		{"two embeddings entries", "  anthropic:\n", "  gateway:\n    wire: openai\n    baseURL: https://gateway.example.com/v1\n    apiKey: {env: OPERATORDEFAULTS_TEST_ANTHROPIC}\n    models: [{model: m, inputPer1M: \"1\", outputPer1M: \"1\"}]\n    maxOutputTokens: 10\n    embeddings: {model: text-embedding-3-small, per1M: \"0.02\"}\n  anthropic:\n", "the file serves embeddings from one entry"},
		{"embeddings on a wire without them", "    wire: anthropic\n", "    wire: anthropic\n    embeddings: {model: text-embedding-3-small, per1M: \"0.02\"}\n", "no embeddings endpoint"},
		{"an embeddings model of another width", "model: text-embedding-3-small", "model: text-embedding-3-large", "3072 wide"},
		{"embeddings without a price", `per1M: "0.02"}`, `}`, "per1M is required"},
		{"an unknown top-level key", "budget:\n", "budgett: {}\nbudget:\n", "field budgett not found"},
		{"an unknown key in an entry", "    maxOutputTokens: 16000\n", "    maxOutputTokens: 16000\n    maxInputTokens: 1\n", "field maxInputTokens not found"},
		{"an unknown key in a model", "contextWindow: 400000}", "contextWindow: 400000, vision: true}", "field vision not found"},
		{"an unknown key in a key reference", "{env: OPERATORDEFAULTS_TEST_ANTHROPIC}\n    models", "{env: OPERATORDEFAULTS_TEST_ANTHROPIC, path: x}\n    models", `unknown key "path"`},
		{"an unknown request param", "defaults: {reasoningEffort: low}", "defaults: {topP: 1}", "topP is not a request param"},
		{"an unknown wire", "wire: anthropic", "wire: bedrock", `wire "bedrock" is not one of`},
		{"an unknown cache ttl", "ttl: onehour", "ttl: 1h", `cache.ttl "1h"`},
		{"no budget", "budget:\n  centsPerDay: 200\n", "", "budget.centsPerDay is required"},
		{"a negative budget", "centsPerDay: 200", "centsPerDay: -1", "at or above 0"},
		{"an idempotency header", "name: X-Title, value: substrate", "name: Idempotency-Key, value: k", "replay a request"},
		{"an idempotency header in any case", "name: X-Title, value: substrate", "name: x-idempotency-key, value: k", "replay a request"},
		{"a function consumer without a price", `pricePerCall: {websearch: "0.1", webscrape: "0.1"}`, `pricePerCall: {websearch: "0.1"}`, "webscrape has no price"},
		{"a price for a non-consumer", `webscrape: "0.1"}`, `webscrape: "0.1", crawl: "1"}`, "crawl is not one of its consumers"},
		{"an oauth client without its id", "    clientId: 1234.apps.example.com\n", "", "clientId is required"},
		{"a package that is not an identity", "  - providers.substrate.reamde.dev/google\n", "  - google\n", `"google" is not a package identity`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := validFile(t)
			if !strings.Contains(file, tc.from) {
				t.Fatalf("the fixture does not hold %q", tc.from)
			}
			file = strings.Replace(file, tc.from, tc.to, 1)
			_, err := Parse([]byte(file), "/etc")
			if err == nil {
				t.Fatalf("parsed; want a refusal naming %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal = %v, want it to name %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "sk-inline-fixture") {
				t.Fatalf("the refusal quotes the inline key: %v", err)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	if c, err := Load(""); c != nil || err != nil {
		t.Fatalf("no path = %v, %v; want no defaults file", c, err)
	}
	if _, err := Load("defaults.yaml"); err == nil || !strings.Contains(err.Error(), "not an absolute path") {
		t.Fatalf("a relative path = %v", err)
	}
	p := filepath.Join(t.TempDir(), "defaults.yaml")
	if err := os.WriteFile(p, []byte(validFile(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil || c.LLM["openai"] == nil {
		t.Fatalf("load = %v, %v", c, err)
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("an empty file = %v", err)
	}
}

func TestCheckPackages(t *testing.T) {
	c, err := Parse([]byte(validFile(t)), "/etc")
	if err != nil {
		t.Fatal(err)
	}
	ships := func(id string) bool { return id == "providers.substrate.reamde.dev/google" }
	err = c.CheckPackages(ships)
	if err == nil || !strings.Contains(err.Error(), "providers.substrate.reamde.dev/firecrawl") {
		t.Fatalf("check = %v, want firecrawl refused", err)
	}
	if err := c.CheckPackages(func(string) bool { return true }); err != nil {
		t.Fatalf("check = %v", err)
	}
	var none *Config
	if err := none.CheckPackages(ships); err != nil {
		t.Fatalf("no defaults file = %v", err)
	}
}
