package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/graphql-go/graphql"
	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
)

// openapi.yaml is hand-written, and these five tests are what make it a
// contract rather than a hope: the paths and methods match what chi mounts,
// every component schema's properties, types and required list match the Go
// struct it describes, every shape the console mirrors (wire.golden.json) is
// a component under the console's name (so the three artifacts share one
// name table), every $ref resolves, and the served route answers the same
// document.

// openapiDoc is the parsed document, decoded once per test through yaml.v3
// exactly as the server does.
func openapiDoc(t *testing.T) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal(openapiYAML, &doc); err != nil {
		t.Fatalf("openapi.yaml: %v", err)
	}
	return doc
}

// pathTemplate normalizes a route so chi's parameter names and the document's
// compare: `{a1}` and `{authority}` are both one segment a caller fills.
var pathTemplate = regexp.MustCompile(`\{[^}]*\}`)

func normalizeTemplate(p string) string {
	return pathTemplate.ReplaceAllString(p, "{}")
}

// TestOpenAPIDescribesEveryMountedRoute walks the router and the document
// and refuses a difference in either direction: a route the document does not
// describe is a client generator that cannot reach it, and a documented
// route the router does not mount is a client generator that calls a 404.
func TestOpenAPIDescribesEveryMountedRoute(t *testing.T) {
	h := New(Config{Service: newFakeService(), InviteCode: testInviteCode})
	mounted := map[string]bool{}
	err := chi.Walk(h.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		mounted[method+" "+normalizeTemplate(route)] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	documented := map[string]bool{}
	paths, _ := openapiDoc(t)["paths"].(map[string]any)
	for path, item := range paths {
		ops, _ := item.(map[string]any)
		for key := range ops {
			switch key {
			case "get", "post", "put", "patch", "delete", "head", "options", "trace":
				documented[strings.ToUpper(key)+" "+normalizeTemplate(path)] = true
			}
		}
	}

	for op := range mounted {
		if !documented[op] {
			t.Errorf("mounted but not in openapi.yaml: %s", op)
		}
	}
	for op := range documented {
		if !mounted[op] {
			t.Errorf("in openapi.yaml but not mounted: %s", op)
		}
	}
}

// openapiComponents maps every component schema that mirrors a Go struct to
// an instance of it. The key is the component's name, which for a shape the
// console mirrors is the console's name too (wire_test.go's wireTypes); the
// rest are the request and reply bodies internal to this package and the
// wire structs the console does not read.
var openapiComponents = map[string]any{
	// The shapes wire.golden.json pins, under the console's names.
	"ErrorEnvelope":       substrate.ErrorEnvelope{},
	"ErrorPayload":        substrate.ErrorPayload{},
	"ProblemDetail":       substrate.ProblemDetail{},
	"SubstrateRecord":     substrate.Record{},
	"IncomingReference":   substrate.IncomingReference{},
	"IncomingSource":      substrate.IncomingSource{},
	"IncomingPage":        substrate.IncomingPage{},
	"PropertyMeta":        substrate.PropertyMeta{},
	"PropertyAlternative": substrate.PropertyAlternative{},
	"PutInput":            substrate.PutInput{},
	"RecordPatch":         substrate.PatchInput{},
	"Cond":                substrate.Cond{},
	"RecordFilter":        substrate.Filter{},
	"KindInfo":            substrate.KindInfo{},
	"Change":              substrate.Change{},
	"ChangeTrigger":       substrate.ChangeTrigger{},
	"ChangeRow":           substrate.ChangeRow{},
	"ChangePage":          substrate.ChangePage{},
	"AffectedRecord":      substrate.AffectedRecord{},
	"Page":                substrate.Page{},
	"Occurrence":          substrate.Occurrence{},
	"OccurrenceLog":       substrate.OccurrenceLog{},
	"OccurrenceProblem":   substrate.OccurrenceProblem{},
	"OccurrenceList":      substrate.OccurrenceList{},
	"OperationalList":     substrate.OperationalList[any]{},
	"TokenInfo":           substrate.TokenInfo{},
	"MintedToken":         substrate.MintedToken{},
	"TOTPEnrollment":      substrate.TOTPEnrollment{},
	"RegisterInput":       substrate.RegisterRequest{},
	"RegisterResult":      substrate.Registered{},
	"SessionUser":         substrate.SessionUser{},
	"AgentResult":         substrate.AgentResult{},
	"AgentEvent":          substrate.AgentEvent{},
	"CatalogBundle":       substrate.CatalogBundle{},
	"CatalogItem":         substrate.CatalogItem{},
	"CatalogInput":        substrate.CatalogInput{},
	"BundleClosure":       substrate.CatalogClosure{},
	"ShippedRecord":       substrate.CatalogShippedRecord{},
	"SuggestedMapping":    substrate.SuggestedMapping{},
	"BundleUpgrade":       substrate.BundleUpgrade{},
	"BundleUpgradeChange": substrate.BundleUpgradeChange{},
	"BundleUpgradeRename": substrate.BundleUpgradeRename{},
	"ConversionPlan":      substrate.ConversionPlan{},
	"ConversionStep":      substrate.ConversionStep{},
	"ConversionConfirm":   substrate.ConversionConfirm{},
	"VocabularyPlan":      substrate.VocabularyPlan{},
	"ShippedUpgrade":      substrate.ShippedUpgrade{},
	"BundleStatus":        substrate.BundleStatus{},
	"InputStatus":         substrate.InputStatus{},
	"SetupItem":           substrate.SetupItem{},
	"BundleUninstalled":   substrate.BundleUninstalled{},
	"BundlePurged":        substrate.BundlePurged{},
	"OAuthStarted":        substrate.OAuthStarted{},
	"TriggerReplayed":     substrate.TriggerReplayed{},
	"TriggerRan":          substrate.TriggerRan{},
	"FunctionCalled":      substrate.FunctionCalled{},
	"WebhookAccepted":     substrate.WebhookAccepted{},

	// Wire structs the console does not mirror.
	"BlobInfo":       substrate.BlobInfo{},
	"TriggerStatus":  substrate.TriggerStatus{},
	"TriggerFailure": substrate.TriggerFailure{},
	"ReembedReport":  substrate.ReembedReport{},
	"MergeInput":     substrate.MergeInput{},
	"SplitInput":     substrate.SplitInput{},

	// The operational lists, one instantiation per element type.
	"TokenList":          substrate.OperationalList[substrate.TokenInfo]{},
	"CatalogList":        substrate.OperationalList[substrate.CatalogItem]{},
	"TriggerStatusList":  substrate.OperationalList[substrate.TriggerStatus]{},
	"TriggerFailureList": substrate.OperationalList[substrate.TriggerFailure]{},
	"BundleStatusList":   substrate.OperationalList[substrate.BundleStatus]{},
	"KindInfoList":       substrate.OperationalList[substrate.KindInfo]{},
	"ShippedUpgradeList": substrate.OperationalList[substrate.ShippedUpgrade]{},

	// This package's own request and reply bodies.
	"RegisterBeginRequest":    registerBeginRequest{},
	"LoginRequest":            loginRequest{},
	"PasswordRequest":         passwordRequest{},
	"TOTPBeginRequest":        totpBeginRequest{},
	"TOTPRequest":             totpRequest{},
	"MintRequest":             mintRequest{},
	"RecoveryEnrollRequest":   recoveryEnrollRequest{},
	"RecoveryEnrollResponse":  recoveryEnrollResponse{},
	"Discovery":               discoveryDoc{},
	"APIVersionInfo":          apiVersionInfo{},
	"ServerInfo":              serverInfo{},
	"VocabularyInfo":          vocabularyInfo{},
	"ChangelogInfo":           changelogInfo{},
	"FeatureInfo":             featureInfo{},
	"SurfacesInfo":            surfacesInfo{},
	"SurfaceInfo":             surfaceInfo{},
	"GrammarInfo":             grammarInfo{},
	"EndpointsInfo":           endpointsInfo{},
	"RegistrationInfo":        registrationInfo{},
	"BookmarkFrame":           bookmarkFrame{},
	"ReplayRequest":           replayRequest{},
	"RunRequest":              runRequest{},
	"CallRequest":             callRequest{},
	"ChatRequest":             chatRequest{},
	"OAuthStartRequest":       oauthStartRequest{},
	"BindRequest":             bindRequest{},
	"VocabularyApplyRequest":  vocabularyApplyRequest{},
	"VocabularyApplyResponse": vocabularyApplyResponse{},
	"VocabularyPlanRequest":   vocabularyPlanRequest{},
	"CatalogTakeRequest":      catalogTakeRequest{},
	"ReembedRequest":          reembedRequest{},
	"GraphQLRequest":          graphqlRequest{},
	"GraphQLResponse":         graphql.Result{},
	"BundleTaken":             bundleTaken{},
}

// openapiHandDescribed are the component schemas with no Go struct behind
// them: a value the code builds as a literal or an open map, described by
// hand and listed here with the reason so a new one is a deliberate act.
var openapiHandDescribed = map[string]string{
	"Health":           "the /healthz body is a literal",
	"ReferenceValue":   "a reference value is an open map keyed by vocabulary.ReferenceValueKey",
	"Op":               "a string enum, substrate.Op",
	"ChangeStream":     "an ndjson stream, opaque to the document",
	"AgentEventStream": "an ndjson stream, opaque to the document",
}

// wireFieldsOf is wire_test.go's reflection, repeated here for the same rule:
// the wire name per exported field, and whether the server always writes it.
// omitempty and omitzero make a field optional; an untagged embedded struct is
// flattened as encoding/json promotes it.
func wireFieldsOf(t *testing.T, v any) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	var walk func(rt reflect.Type)
	walk = func(rt reflect.Type) {
		for i := range rt.NumField() {
			f := rt.Field(i)
			if !f.IsExported() {
				continue
			}
			tag, tagged := f.Tag.Lookup("json")
			if f.Anonymous && !tagged {
				if f.Type.Kind() != reflect.Struct {
					t.Fatalf("%s embeds %s, which the golden rule does not allow", rt.Name(), f.Type)
				}
				walk(f.Type)
				continue
			}
			if !tagged {
				t.Fatalf("%s.%s has no json tag", rt.Name(), f.Name)
			}
			name, opts, _ := strings.Cut(tag, ",")
			if name == "-" {
				continue
			}
			optional := false
			for _, o := range strings.Split(opts, ",") {
				if o == "omitempty" || o == "omitzero" {
					optional = true
				}
			}
			out[name] = !optional
		}
	}
	rt := reflect.TypeOf(v)
	if rt.Kind() != reflect.Struct {
		t.Fatalf("%s is not a struct", rt)
	}
	walk(rt)
	return out
}

// wireTypeOf is the JSON Schema `type` (and `format`, for an instant) a Go
// field serializes as. An interface is any JSON value and answers "", which
// skips the check; a []byte is a base64 string; a struct or a map is an
// object, time.Time excepted.
func wireTypeOf(rt reflect.Type) (typ, format string) {
	for rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if rt == reflect.TypeFor[time.Time]() {
		return "string", "date-time"
	}
	switch rt.Kind() {
	case reflect.String:
		return "string", ""
	case reflect.Bool:
		return "boolean", ""
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer", ""
	case reflect.Float32, reflect.Float64:
		return "number", ""
	case reflect.Slice, reflect.Array:
		if rt.Elem().Kind() == reflect.Uint8 {
			return "string", ""
		}
		return "array", ""
	case reflect.Map, reflect.Struct:
		return "object", ""
	default:
		return "", ""
	}
}

// wireFieldTypes maps each wire name to the Go type behind it, with the same
// flattening wireFieldsOf applies.
func wireFieldTypes(v any) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	var walk func(rt reflect.Type)
	walk = func(rt reflect.Type) {
		for i := range rt.NumField() {
			f := rt.Field(i)
			if !f.IsExported() {
				continue
			}
			tag, tagged := f.Tag.Lookup("json")
			if f.Anonymous && !tagged {
				walk(f.Type)
				continue
			}
			name, _, _ := strings.Cut(tag, ",")
			if name == "-" || name == "" {
				continue
			}
			out[name] = f.Type
		}
	}
	walk(reflect.TypeOf(v))
	return out
}

// schemaType reads a property schema's `type` and `format`, following one
// `$ref` to its component. A schema with no type is any JSON value.
func schemaType(schemas map[string]any, s any) (typ, format string) {
	sm, _ := s.(map[string]any)
	if ref, ok := sm["$ref"].(string); ok {
		sm, _ = schemas[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any)
	}
	typ, _ = sm["type"].(string)
	format, _ = sm["format"].(string)
	return typ, format
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// requestComponents names every component schema a request carries: the
// schemas a requestBody or a parameter's `content` references, and every
// schema those reference in turn (a filter's conditions, say). A request
// body is decoded strictly (decodeJSONStrict), so each of these must be
// closed with `additionalProperties: false`, or a generator would build a
// body the server refuses. Their `required` lists are the endpoint's
// validation rules, not the struct's tags, so they are written by hand.
func requestComponents(doc map[string]any) map[string]bool {
	components, _ := doc["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	out := map[string]bool{}
	var collectRefs func(v any, into func(string))
	collectRefs = func(v any, into func(string)) {
		switch v := v.(type) {
		case map[string]any:
			for k, val := range v {
				if k == "$ref" {
					if ref, ok := val.(string); ok && strings.HasPrefix(ref, "#/components/schemas/") {
						into(strings.TrimPrefix(ref, "#/components/schemas/"))
					}
					continue
				}
				collectRefs(val, into)
			}
		case []any:
			for _, val := range v {
				collectRefs(val, into)
			}
		}
	}
	var add func(name string)
	add = func(name string) {
		if out[name] {
			return
		}
		out[name] = true
		collectRefs(schemas[name], add)
	}
	// The roots: every requestBody, inline or shared, and every parameter
	// that carries `content` (the filter document).
	var roots func(v any)
	roots = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, val := range v {
				switch k {
				case "requestBody", "requestBodies":
					collectRefs(val, add)
				case "parameters":
					var params []any
					switch p := val.(type) {
					case []any:
						params = p
					case map[string]any:
						for _, one := range p {
							params = append(params, one)
						}
					}
					for _, p := range params {
						pm, _ := p.(map[string]any)
						collectRefs(pm["content"], add)
					}
				default:
					roots(val)
				}
			}
		case []any:
			for _, val := range v {
				roots(val)
			}
		}
	}
	roots(doc)
	return out
}

// TestOpenAPIComponentsMatchTheWireStructs holds every component schema to
// the struct it describes: the same property names, and for a response
// `required` naming exactly the fields the server always writes. A request
// component is instead held closed (`additionalProperties: false`), because
// the server decodes it strictly, and its `required` list is the endpoint's
// own rule (a TOTP code is required only where discovery says so). A
// component with no struct is hand-described and must say so. The table and
// the document are compared both ways, so a component added to one without
// the other fails.
func TestOpenAPIComponentsMatchTheWireStructs(t *testing.T) {
	doc := openapiDoc(t)
	components, _ := doc["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	requests := requestComponents(doc)
	if len(requests) == 0 {
		t.Fatal("no request components found; the walk is broken")
	}

	for name := range schemas {
		_, described := openapiComponents[name]
		_, hand := openapiHandDescribed[name]
		switch {
		case described && hand:
			t.Errorf("%s is both a Go struct and hand-described; pick one", name)
		case !described && !hand:
			t.Errorf("%s is in openapi.yaml but in neither openapiComponents nor openapiHandDescribed", name)
		}
	}
	for name := range openapiComponents {
		if _, ok := schemas[name]; !ok {
			t.Errorf("%s is in openapiComponents but not in openapi.yaml", name)
		}
	}
	for name := range openapiHandDescribed {
		if _, ok := schemas[name]; !ok {
			t.Errorf("%s is in openapiHandDescribed but not in openapi.yaml", name)
		}
	}

	for _, name := range sortedKeys(openapiComponents) {
		schema, _ := schemas[name].(map[string]any)
		if schema == nil {
			continue
		}
		want := wireFieldsOf(t, openapiComponents[name])
		props, _ := schema["properties"].(map[string]any)
		got := sortedKeys(props)
		if wantKeys := sortedKeys(want); !slices.Equal(got, wantKeys) {
			t.Errorf("%s properties = %v, the struct serializes %v", name, got, wantKeys)
		}
		// The type per field, so a retype fails: a Go string documented as an
		// integer, or an instant without `format: date-time`.
		for field, goType := range wireFieldTypes(openapiComponents[name]) {
			wantType, wantFormat := wireTypeOf(goType)
			if wantType == "" {
				continue
			}
			gotType, gotFormat := schemaType(schemas, props[field])
			if gotType != wantType {
				t.Errorf("%s.%s: type %q, the Go field %s serializes as %q", name, field, gotType, goType, wantType)
			}
			if wantFormat != "" && gotFormat != wantFormat {
				t.Errorf("%s.%s: format %q, the Go field %s wants %q", name, field, gotFormat, goType, wantFormat)
			}
		}
		if requests[name] {
			if closed, ok := schema["additionalProperties"].(bool); !ok || closed {
				t.Errorf("%s is a request body decoded strictly; it needs additionalProperties: false", name)
			}
			continue
		}
		var wantRequired []string
		for field, required := range want {
			if required {
				wantRequired = append(wantRequired, field)
			}
		}
		sort.Strings(wantRequired)
		var gotRequired []string
		if raw, ok := schema["required"].([]any); ok {
			for _, r := range raw {
				gotRequired = append(gotRequired, fmt.Sprint(r))
			}
		}
		sort.Strings(gotRequired)
		if !slices.Equal(gotRequired, wantRequired) {
			t.Errorf("%s required = %v, the struct always writes %v", name, gotRequired, wantRequired)
		}
	}
}

// TestOpenAPICoversTheConsoleGolden holds the document to wire.golden.json:
// every shape the console mirrors is a component under the same name, so a
// field that moves fails the golden, the console and this document together.
// The golden is the one the vitest reads, so the path is the console's.
func TestOpenAPICoversTheConsoleGolden(t *testing.T) {
	raw, err := os.ReadFile("../../web/console/src/lib/api/wire.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden map[string]map[string]bool
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	components, _ := openapiDoc(t)["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	for name, fields := range golden {
		schema, ok := schemas[name].(map[string]any)
		if !ok {
			t.Errorf("wire.golden.json pins %s and openapi.yaml has no such component", name)
			continue
		}
		props, _ := schema["properties"].(map[string]any)
		if got, want := sortedKeys(props), sortedKeys(fields); !slices.Equal(got, want) {
			t.Errorf("%s: openapi.yaml properties %v, golden %v", name, got, want)
		}
	}
}

// TestOpenAPIReferencesResolve walks every `$ref` and requires a component
// behind it: a dangling reference is a document a generator refuses.
func TestOpenAPIReferencesResolve(t *testing.T) {
	doc := openapiDoc(t)
	components, _ := doc["components"].(map[string]any)
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, val := range v {
				if k == "$ref" {
					ref, _ := val.(string)
					section, name, ok := strings.Cut(strings.TrimPrefix(ref, "#/components/"), "/")
					if !ok || !strings.HasPrefix(ref, "#/components/") {
						t.Errorf("%s: $ref %q is not a local component reference", path, ref)
						continue
					}
					group, _ := components[section].(map[string]any)
					if _, found := group[name]; !found {
						t.Errorf("%s: $ref %q does not resolve", path, ref)
					}
					continue
				}
				walk(path+"/"+k, val)
			}
		case []any:
			for i, val := range v {
				walk(fmt.Sprintf("%s[%d]", path, i), val)
			}
		}
	}
	walk("#", doc)
}

// TestOpenAPIIsServedAsJSON reads the document off the route: unauthenticated,
// `application/json`, the same paths the YAML declares.
func TestOpenAPIIsServedAsJSON(t *testing.T) {
	h := New(Config{Service: newFakeService()})
	req := httptest.NewRequest(http.MethodGet, openapiRoute, nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, body %s", openapiRoute, rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var served map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &served); err != nil {
		t.Fatalf("served document is not JSON: %v", err)
	}
	if v := served["openapi"]; v != "3.1.0" {
		t.Fatalf("openapi = %v, want 3.1.0", v)
	}
	// The document's version is the served API version: the one prefix the
	// router mounts is the one the document describes.
	info, _ := served["info"].(map[string]any)
	if v := info["version"]; v != APIVersion {
		t.Fatalf("info.version = %v, want %s", v, APIVersion)
	}
	servedPaths, _ := served["paths"].(map[string]any)
	yamlPaths, _ := openapiDoc(t)["paths"].(map[string]any)
	if got, want := sortedKeys(servedPaths), sortedKeys(yamlPaths); !slices.Equal(got, want) {
		t.Fatalf("served paths %v, yaml paths %v", got, want)
	}
	if _, ok := servedPaths[openapiRoute]; !ok {
		t.Fatalf("the document does not describe its own route %s", openapiRoute)
	}
}
