package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// THE INPUT IS PINNED, AS THE WIRE IS (decision record 0265). A function body
// is written against the function interface: the `input` it is handed and the
// `host` object it calls. A package names the interface version it was written
// against, so the input may change only under a new version, and this golden
// is what notices it moving: every key a body reads, its JSON type, and
// whether it is always there or may be left out (`, optional`). An object in
// an array is recorded under `[]`. A renamed, dropped or retyped key fails
// here; a key added is a diff to review against the interface's beta label
// (docs/functions.md#the-interface). The envelopes are built by the code the
// dispatcher builds them with. The `config` the engine resolves from the
// repository is pinned beside it, in function_config.golden.json, by a test
// that builds it the way an invocation does (TestFunctionConfigMatchesTheGolden).
//
// Regenerate with `go test ./internal/engine/ -run TestFunctionInputMatchesTheGolden -update-function-input`.

var updateFunctionInput = flag.Bool("update-function-input", false,
	"rewrite testdata/function_input.golden.json from the shapes this binary hands a body")

const functionInputGolden = "testdata/function_input.golden.json"

// memorySpool stands in for the blob store a webhook's request is parked
// into and read back from: the parked envelope carries digests, the store the
// bytes.
type memorySpool map[string][]byte

func (m memorySpool) stage(_ context.Context, _ substrate.BlobUpload, data []byte) (string, error) {
	digest := fmt.Sprintf("blob-sha256-%x", sha256.Sum256(data))
	m[digest] = data
	return digest, nil
}

func (m memorySpool) fetch(_ context.Context, digest string) ([]byte, error) {
	data, ok := m[digest]
	if !ok {
		return nil, fmt.Errorf("no blob %s", digest)
	}
	return data, nil
}

func TestFunctionInputMatchesTheGolden(t *testing.T) {
	shapes := map[string]map[string]string{}
	structShape(shapes, "input", reflect.TypeOf(runner.Input{}))

	at := time.Date(2026, 11, 13, 9, 30, 0, 0, time.UTC)
	created := substrate.Change{
		Seq: 7, Op: substrate.OpPut, Kind: "example.com/garden/plant", RecordID: "p1",
		Actor: "app:local", Payload: map[string]any{"created": true, "properties": []any{"name"}},
	}
	deleted := substrate.Change{Seq: 8, Op: substrate.OpDelete, Kind: "example.com/garden/plant", RecordID: "p1", Actor: "app:local"}
	plant := &substrate.Record{ID: "p1", Kind: "example.com/garden/plant", Version: 1, Properties: map[string]any{"name": "fern"}}
	mapShape(shapes, "input.envelope(record)", []map[string]any{
		runner.Envelope(created, plant, "example.com"),
		runner.Envelope(deleted, nil, "example.com"),
	})
	mapShape(shapes, "input.envelope(schedule)", []map[string]any{
		runner.FireEnvelope("f1", at, "example.com"),
	})

	request := substrate.WebhookRequest{
		Method: "POST", ContentType: "application/json",
		Headers: map[string]string{"x-github-event": "push"}, Query: url.Values{"a": {"b"}},
	}
	text, binary, multipart := request, request, request
	text.Body = []byte(`{"ok":true}`)
	binary.Body = []byte{0xff, 0xfe}
	multipart.ContentType = "multipart/form-data"
	multipart.Parts = []substrate.WebhookPart{
		{Name: "note", Value: "hello"},
		{Name: "audio", Filename: "rec.m4a", MediaType: "audio/mp4", Data: []byte("bytes")},
	}
	// A webhook request is delivered the way the door records it: built,
	// parked into the ledger with its bytes in the blob store, and read back
	// from there (admitWebhook, then fireEnvelope). The body reads the
	// envelope as it comes back, so that is the one recorded.
	var ds *dataset // spoolWebhookParts and parkedEnvelope read their blob spool alone
	webhook := func(req substrate.WebhookRequest, verified bool) map[string]any {
		t.Helper()
		ctx := context.Background()
		store := memorySpool{}
		parts, err := ds.spoolWebhookParts(ctx, store, req.Parts)
		if err != nil {
			t.Fatal(err)
		}
		built := webhookFireEnvelope("f1", at, "example.com", req, parts, verified)
		parked, err := ds.parkedEnvelope(ctx, built, map[string]bool{"x-github-event": true}, store)
		if err != nil {
			t.Fatal(err)
		}
		var delivered map[string]any
		if err := json.Unmarshal(parked, &delivered); err != nil {
			t.Fatal(err)
		}
		if err := restoreRequest(ctx, delivered, store.fetch); err != nil {
			t.Fatal(err)
		}
		return delivered
	}
	mapShape(shapes, "input.envelope(webhook)", []map[string]any{
		webhook(text, true),
		webhook(binary, false),
		webhook(multipart, false),
		webhook(request, false),
	})

	got, err := json.MarshalIndent(shapes, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if *updateFunctionInput {
		if err := os.WriteFile(functionInputGolden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(functionInputGolden)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate with -update-function-input)", functionInputGolden, err)
	}
	if string(got) != string(want) {
		t.Fatalf("the input a function body is handed moved, and %s did not.\n"+
			"A body reads these keys, so a change to them is a change to the function interface: an addition stays on interface %d, anything else needs a new version (docs/functions.md#the-interface).\n"+
			"Review the change, then regenerate with -update-function-input.\n\ngot:\n%s", functionInputGolden, vocabulary.FunctionInterface, got)
	}
}

// structShape records one struct's JSON keys with their types, a key Go may
// omit marked optional, and descends into a struct field.
func structShape(shapes map[string]map[string]string, name string, typ reflect.Type) {
	fields := map[string]string{}
	for f := range typ.Fields() {
		tag := f.Tag.Get("json")
		if tag == "-" || !f.IsExported() {
			continue
		}
		parts := strings.Split(tag, ",")
		key := parts[0]
		if key == "" {
			key = f.Name
		}
		fields[key] = jsonKind(f.Type) + optional(!slices.Contains(parts[1:], "omitempty"))
		inner := f.Type
		if inner.Kind() == reflect.Pointer {
			inner = inner.Elem()
		}
		if inner.Kind() == reflect.Struct {
			structShape(shapes, name+"."+key, inner)
		}
	}
	shapes[name] = fields
}

// jsonKind is the JSON type a Go type serializes as.
func jsonKind(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Pointer:
		return jsonKind(t.Elem())
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Map, reflect.Struct:
		return "object"
	case reflect.Slice, reflect.Array:
		return "array"
	default:
		return "any"
	}
}

func optional(always bool) string {
	if always {
		return ""
	}
	return ", optional"
}

// mapShape records the keys of one envelope as a body receives it across
// every variant the engine builds: each key's JSON types, an array's element
// types beside it, and optional where a variant leaves it out. An object
// under a key, or in an array under one, is recorded the same way over the
// variants that carry it. A free-form object (the record's properties, a
// request's headers and query, a config's settings and a view's secret
// errors) carries the writer's keys, not the interface's, so it is recorded
// as `*` and the types of its values. A config's inputs are keyed by the
// package's input names and each value is a record's view, so the view is
// recorded under `*`.
func mapShape(shapes map[string]map[string]string, name string, variants []map[string]any) {
	types := map[string]map[string]bool{}
	present := map[string]int{}
	nested := map[string][]map[string]any{}
	for _, v := range variants {
		for k, val := range v {
			if types[k] == nil {
				types[k] = map[string]bool{}
			}
			types[k][valueKind(val)] = true
			present[k]++
			switch x := val.(type) {
			case map[string]any:
				nested[k] = append(nested[k], x)
			case []any:
				for _, e := range x {
					if m, ok := e.(map[string]any); ok {
						nested[k+"[]"] = append(nested[k+"[]"], m)
					}
				}
			}
		}
	}
	fields := map[string]string{}
	for k, ts := range types {
		fields[k] = strings.Join(slices.Sorted(maps.Keys(ts)), " | ") + optional(present[k] == len(variants))
	}
	shapes[name] = fields
	for k, ms := range nested {
		if k == "inputs" {
			var views []map[string]any
			for _, m := range ms {
				for _, v := range m {
					if view, ok := v.(map[string]any); ok {
						views = append(views, view)
					}
				}
			}
			shapes[name+"."+k] = map[string]string{"*": "object"}
			mapShape(shapes, name+"."+k+".*", views)
			continue
		}
		if k == "properties" && strings.HasPrefix(name, "input.config") {
			// A view's properties are the package's own names, free, but for
			// the one key the engine puts among them: the record's title.
			titled := 0
			for _, m := range ms {
				if _, ok := m["title"].(string); ok {
					titled++
				}
			}
			if titled > 0 {
				shapes[name+"."+k] = map[string]string{"title": "string" + optional(titled == len(ms))}
			}
			continue
		}
		if k == "properties" || k == "headers" || k == "query" || k == "settings" || k == "secretErrors" {
			values := map[string]bool{}
			for _, m := range ms {
				for _, v := range m {
					values[valueKind(v)] = true
				}
			}
			// An object every variant leaves empty (the query, which a
			// parked request drops) has no value type to record.
			if k != "properties" && len(values) > 0 {
				shapes[name+"."+k] = map[string]string{"*": strings.Join(slices.Sorted(maps.Keys(values)), " | ")}
			}
			continue
		}
		mapShape(shapes, name+"."+k, ms)
	}
}

// valueKind is the JSON type of a value as the runner marshals it, an
// array's with the types of its elements.
func valueKind(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return "number"
	case map[string]any:
		return "object"
	case []string:
		return "array of string"
	case []any:
		elems := map[string]bool{}
		for _, e := range x {
			elems[valueKind(e)] = true
		}
		if len(elems) == 0 {
			return "array"
		}
		return "array of " + strings.Join(slices.Sorted(maps.Keys(elems)), " or ")
	default:
		return "any"
	}
}
