package providertest

// The chat bridge contract as files, held without a database: the id vectors
// against the one id algorithm, and the README's JSON Schema of the batch
// against the golden batches and against batches it must refuse. A bridge
// written in another language builds from these two.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/vocabulary"
)

const chatBridgeReadme = providersDir + "/chatbridge/README.md"

// cbVector is one row of testdata/chatbridge/ids.json.
type cbVector struct {
	Row      string            `json:"row"`
	Inputs   map[string]string `json:"inputs"`
	External struct {
		Provider   string `json:"provider"`
		Account    string `json:"account"`
		ExternalID string `json:"externalId"`
	} `json:"external"`
	ID string `json:"id"`
}

func cbVectors(t *testing.T) []cbVector {
	t.Helper()
	raw, err := os.ReadFile(cbFixtures + "ids.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Vectors []cbVector `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Vectors) == 0 {
		t.Fatal("ids.json holds no vector")
	}
	return doc.Vectors
}

// Each vector's three arguments are composed from its event fields by the
// README's table, and give its id through the one algorithm host.py and
// runner.ExternalID share.
func TestChatBridgeIDVectors(t *testing.T) {
	t.Parallel()
	rows := map[string]bool{}
	for _, v := range cbVectors(t) {
		in := v.Inputs
		var want [3]string
		switch v.Row {
		case "instance":
			want = [3]string{"chatbridge", in["instance"], "instance"}
		case "account":
			want = [3]string{in["network"], in["account"], "account"}
		case "user":
			want = [3]string{in["network"], in["account"], "user/" + in["user"]}
		case "chat":
			want = [3]string{in["network"], in["account"], "chat/" + in["chat"]}
		case "message":
			want = [3]string{in["network"], in["account"], "message/" + in["chat"] + "/" + in["message"]}
		case "messageattachment":
			want = [3]string{
				in["network"], in["account"],
				"attachment/" + in["chat"] + "/" + in["message"] + "/" + in["part"],
			}
		default:
			t.Fatalf("a vector for row %q, which the contract does not compose", v.Row)
		}
		rows[v.Row] = true
		got := [3]string{v.External.Provider, v.External.Account, v.External.ExternalID}
		if got != want {
			t.Errorf("%s %v: the arguments are %q, the contract composes %q", v.Row, in, got, want)
		}
		if id := runner.ExternalID(want[0], want[1], want[2]); id != v.ID {
			t.Errorf("%s %v: runner.ExternalID gives %s, the vector says %s", v.Row, in, id, v.ID)
		}
	}
	for _, row := range []string{"instance", "account", "user", "chat", "message", "messageattachment"} {
		if !rows[row] {
			t.Errorf("no vector for %s", row)
		}
	}
}

// cbSchema is the JSON Schema the package README carries, the one code
// block under its "The batch" heading that holds a `$schema`.
func cbSchema(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(chatBridgeReadme)
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```json\n(.*?)\n```").FindAllStringSubmatch(string(raw), -1)
	var found []map[string]any
	for _, b := range blocks {
		if !strings.Contains(b[1], `"$schema"`) {
			continue
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(b[1]), &doc); err != nil {
			t.Fatalf("the README's schema is not JSON: %v", err)
		}
		found = append(found, doc)
	}
	if len(found) != 1 {
		t.Fatalf("the README carries %d JSON Schemas, want the batch's alone", len(found))
	}
	return found[0]
}

// Every golden batch is one the README's schema admits, and each malformed
// batch below is one it refuses, so the schema and the function read one
// shape.
func TestChatBridgeBatchSchema(t *testing.T) {
	t.Parallel()
	schema := cbSchema(t)
	check := func(doc any) []string {
		var problems []string
		jsonSchemaCheck(schema, schema, doc, "batch", &problems)
		return problems
	}
	for _, name := range []string{"batch_1_link.json", "batch_2_live.json", "batch_3_history.json"} {
		var f struct {
			Batch any `json:"batch"`
		}
		raw, err := os.ReadFile(cbFixtures + name)
		if err != nil {
			t.Fatal(err)
		}
		// A decoder keeps the last of two equal keys, so a worked example
		// spelling one twice would read as valid and teach the wrong shape.
		if dup := jsonDuplicateKey(json.NewDecoder(bytes.NewReader(raw)), "$"); dup != "" {
			t.Errorf("%s spells %s twice", name, dup)
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatal(err)
		}
		if problems := check(f.Batch); len(problems) > 0 {
			t.Errorf("%s does not meet the README's schema:\n  %s", name, strings.Join(problems, "\n  "))
		}
	}
	base := func(events ...any) map[string]any {
		return map[string]any{"version": 1.0, "instance": cbUUID, "first": 1.0, "last": 1.0, "events": events}
	}
	ev := func(fields map[string]any) map[string]any {
		e := map[string]any{"seq": 1.0, "at": "2026-11-09T10:00:00.000Z", "network": "whatsapp", "account": "1"}
		for k, v := range fields {
			e[k] = v
		}
		return e
	}
	for name, doc := range map[string]any{
		"a version the package does not read": map[string]any{"version": 2.0, "instance": "x", "first": 1.0, "last": 1.0, "events": []any{}},
		"no instance":                         map[string]any{"version": 1.0, "first": 1.0, "last": 1.0, "events": []any{}},
		"an unknown event type":               base(ev(map[string]any{"type": "typing.start"})),
		"a state outside the enum":            base(ev(map[string]any{"type": "account.state", "state": "asleep"})),
		"a message with no sender": base(ev(map[string]any{
			"type": "message.upsert", "chat": "c", "backfill": false,
			"message": map[string]any{"id": "m", "fromOwner": false, "at": "2026-11-09T10:00:00Z", "content": "text"},
		})),
		"a medium that is no part": base(ev(map[string]any{
			"type": "user.upsert",
			"user": map[string]any{"id": "u", "avatar": "https://example.com/a.jpg"},
		})),
		"an outcome outside the enum": base(map[string]any{
			"seq": 1.0, "at": "2026-11-09T10:00:00.000Z",
			"type": "action.result", "action": "a", "attempt": "b", "outcome": "maybe",
		}),
		"members that are no list": base(ev(map[string]any{
			"type": "chat.upsert",
			"chat": map[string]any{"id": "c", "members": []any{}},
		})),
	} {
		if len(check(doc)) == 0 {
			t.Errorf("the README's schema admits %s", name)
		}
	}
}

// jsonSchemaCheck holds a value to the part of JSON Schema the README's
// batch schema uses: $ref into $defs, type, enum, const, properties,
// required, additionalProperties, items, minimum, minLength, maxItems,
// oneOf, and format date-time. Anything else it meets fails, so the schema
// cannot grow a keyword this check reads past.
func jsonSchemaCheck(root, s map[string]any, v any, at string, problems *[]string) {
	fail := func(format string, args ...any) {
		*problems = append(*problems, at+": "+fmt.Sprintf(format, args...))
	}
	known := []string{
		"$schema", "$id", "$ref", "$defs", "title", "description", "type", "enum", "const",
		"properties", "required", "additionalProperties", "items", "minimum", "minLength", "maxItems",
		"oneOf", "format",
	}
	for k := range s {
		if !slices.Contains(known, k) {
			fail("the schema uses %q, which this check does not read", k)
			return
		}
	}
	if ref, ok := s["$ref"].(string); ok {
		name, found := strings.CutPrefix(ref, "#/$defs/")
		def, _ := root["$defs"].(map[string]any)[name].(map[string]any)
		if !found || def == nil {
			fail("$ref %s names no $defs entry", ref)
			return
		}
		jsonSchemaCheck(root, def, v, at, problems)
		return
	}
	if alts, ok := s["oneOf"].([]any); ok {
		matched := 0
		for _, alt := range alts {
			var p []string
			jsonSchemaCheck(root, alt.(map[string]any), v, at, &p)
			if len(p) == 0 {
				matched++
			}
		}
		if matched != 1 {
			fail("matches %d of oneOf's %d alternatives, want exactly one", matched, len(alts))
		}
		return
	}
	if t, ok := s["type"]; ok && !jsonTypeMatches(t, v) {
		fail("is %T, want %v", v, t)
		return
	}
	if c, ok := s["const"]; ok && fmt.Sprint(c) != fmt.Sprint(v) {
		fail("is %v, want %v", v, c)
	}
	if e, ok := s["enum"].([]any); ok && !slices.ContainsFunc(e, func(x any) bool { return x == v }) {
		fail("%v is not one of %v", v, e)
	}
	if f, ok := s["format"].(string); ok {
		if str, isStr := v.(string); isStr && f == "date-time" {
			if _, err := time.Parse(time.RFC3339Nano, str); err != nil {
				fail("%q is not an RFC 3339 date-time", str)
			}
		}
	}
	if m, ok := s["minimum"].(float64); ok {
		if n, isNum := v.(float64); isNum && n < m {
			fail("%v is below %v", n, m)
		}
	}
	if m, ok := s["minLength"].(float64); ok {
		if str, isStr := v.(string); isStr && float64(len([]rune(str))) < m {
			fail("is shorter than %v", m)
		}
	}
	switch val := v.(type) {
	case map[string]any:
		props, _ := s["properties"].(map[string]any)
		for _, r := range asList(s["required"]) {
			if _, ok := val[r.(string)]; !ok {
				fail("lacks %s", r)
			}
		}
		for k, x := range val {
			if ps, ok := props[k].(map[string]any); ok {
				jsonSchemaCheck(root, ps, x, at+"."+k, problems)
			} else if s["additionalProperties"] == false {
				fail("carries %s, which the schema does not declare", k)
			}
		}
	case []any:
		if m, ok := s["maxItems"].(float64); ok && float64(len(val)) > m {
			fail("holds %d items, more than %v", len(val), m)
		}
		if items, ok := s["items"].(map[string]any); ok {
			for i, x := range val {
				jsonSchemaCheck(root, items, x, fmt.Sprintf("%s[%d]", at, i), problems)
			}
		}
	}
}

func jsonTypeMatches(want, v any) bool {
	types, _ := want.([]any)
	if types == nil {
		types = []any{want}
	}
	for _, t := range types {
		switch t {
		case "object":
			if _, ok := v.(map[string]any); ok {
				return true
			}
		case "array":
			if _, ok := v.([]any); ok {
				return true
			}
		case "string":
			if _, ok := v.(string); ok {
				return true
			}
		case "boolean":
			if _, ok := v.(bool); ok {
				return true
			}
		case "integer":
			if n, ok := v.(float64); ok && n == math.Trunc(n) {
				return true
			}
		case "number":
			if _, ok := v.(float64); ok {
				return true
			}
		case "null":
			if v == nil {
				return true
			}
		}
	}
	return false
}

// jsonDuplicateKey answers the path of the first key an object of the
// decoder's next value spells twice, or "".
func jsonDuplicateKey(dec *json.Decoder, at string) string {
	tok, err := dec.Token()
	if err != nil {
		if err == io.EOF {
			return ""
		}
		return at + ": " + err.Error()
	}
	switch tok {
	case json.Delim('{'):
		seen := map[string]bool{}
		for dec.More() {
			key, _ := dec.Token()
			k, _ := key.(string)
			if seen[k] {
				return at + "." + k
			}
			seen[k] = true
			if dup := jsonDuplicateKey(dec, at+"."+k); dup != "" {
				return dup
			}
		}
		_, _ = dec.Token()
	case json.Delim('['):
		for i := 0; dec.More(); i++ {
			if dup := jsonDuplicateKey(dec, fmt.Sprintf("%s[%d]", at, i)); dup != "" {
				return dup
			}
		}
		_, _ = dec.Token()
	}
	return ""
}

// The authorization request the README spells is one the server takes: its
// client id and its details parse as the authorization endpoint parses them,
// and its scopes cover everything the package's code reads and writes, the
// internal instance kind by its exact scope alone (decision 0215).
func TestChatBridgeAuthorizationRequest(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(chatBridgeReadme)
	if err != nil {
		t.Fatal(err)
	}
	readme := string(raw)
	start := strings.Index(readme, "## Approving a bridge")
	end := strings.Index(readme, "## Delivery")
	if start < 0 || end < start {
		t.Fatal("the README has no Approving a bridge section")
	}
	section := readme[start:end]

	block := regexp.MustCompile("(?s)```json\n(.*?)\n *```").FindStringSubmatch(section)
	if block == nil {
		t.Fatal("the README's request names no authorization_details")
	}
	details, err := authorize.ParseDetails(block[1])
	if err != nil {
		t.Fatalf("the server refuses the README's authorization_details: %v", err)
	}
	var pkg, hook bool
	for _, d := range details {
		pkg = pkg || (d.Type == "package" && d.Package == chatBridgePkg && d.Version == 1)
		hook = hook || (d.Type == "webhook" && d.Trigger == cbTrigger)
	}
	if !pkg || !hook {
		t.Fatalf("the README's details are %+v, want the package at version 1 and its webhook", details)
	}

	client := regexp.MustCompile("`(http://127\\.0\\.0\\.1/[^`]*)`").FindStringSubmatch(section)
	if client == nil {
		t.Fatal("the README's request names no loopback client_id")
	}
	if _, err := authorize.ParseClientID(client[1]); err != nil {
		t.Fatalf("the server refuses the README's client_id %s: %v", client[1], err)
	}

	scopes := regexp.MustCompile("`(write:[^`]+)`").FindAllStringSubmatch(section, -1)
	var asked []string
	for _, m := range scopes {
		asked = append(asked, m[1])
	}
	if _, err := scope.ParseRequest(strings.Join(asked, " ")); err != nil {
		t.Fatalf("the server refuses the README's scope %v: %v", asked, err)
	}
	cat, err := shippedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	b, ok := cat.ByID(chatBridgePkg)
	if !ok {
		t.Fatal("the catalog ships no chat bridge")
	}
	purposes := map[string]string{}
	var internals []string
	for _, k := range b.Kinds() {
		purposes[k.Identity] = k.Purpose
		if k.Purpose == vocabulary.PurposeInternal {
			internals = append(internals, k.Identity)
		}
	}
	uncovered := func(scopes []string) []string {
		approved, err := scope.ApprovalOf(scopes)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range b.Reach() {
			internal := purposes[r.Pattern] == vocabulary.PurposeInternal
			if !approved.Covers(r.Pattern, r.Write, scope.Kind{Internal: internal}, internals...) {
				out = append(out, scope.Covering(r.Pattern, r.Write))
			}
		}
		return out
	}
	if len(b.Reach()) == 0 {
		t.Fatal("the package's code reaches nothing")
	}
	if missing := uncovered(asked); len(missing) > 0 {
		t.Fatalf("the README's scopes leave uncovered: %s", strings.Join(missing, ", "))
	}
	// The exact scope is there because the glob does not reach the
	// internal kind: without it, the instance alone is uncovered.
	glob := slices.DeleteFunc(slices.Clone(asked), func(s string) bool { return s == "write:"+cbInstance })
	if missing := uncovered(glob); len(missing) == 0 || !strings.HasSuffix(missing[0], cbInstance) {
		t.Fatalf("without write:%s the scopes leave uncovered %v", cbInstance, missing)
	}
}
