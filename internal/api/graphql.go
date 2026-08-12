package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"

	"github.com/geoah/substrate/internal/substrate"
)

// decodeGraphQLBody decodes the GraphQL request body with the same strict
// exact-key/unknown-field/end-of-stream rules as every other body, but with
// json.UseNumber so numeric VARIABLES arrive as json.Number (codex regress
// #15) — the EXACT decimal text, before any float rounding has happened.
// normalizeVariables then turns each one into the widest Go number that holds
// it losslessly, and the Long scalar rejects what does not fit, instead of
// silently truncating — protecting the CAS seqs (Change.seq, ifVersion, the
// changelog `from`) a truncation would corrupt.
func decodeGraphQLBody(r *http.Request, v *graphqlRequest) error {
	raw, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxRequestBody))
	if err != nil {
		return err
	}
	if err := decodeStrictBytes(raw, v, true); err != nil {
		return err
	}
	v.Variables, _ = normalizeVariables(v.Variables).(map[string]any)
	return nil
}

// normalizeVariables replaces every json.Number in a decoded variables tree
// with a plain Go number, recursing through objects and lists.
//
// It exists because graphql-go coerces variables by type switch and knows
// NOTHING about json.Number: the built-in Int and Float scalars answer nil for
// it, which surfaces as `Variable "$first" got invalid value 5` on a perfectly
// good query. UseNumber is still how the body is decoded — the point is to
// choose the target type from the LITERAL rather than from float64: a whole
// number that fits becomes an int64 (exact, and what Int/Long/Float all coerce
// cleanly), anything else becomes a float64, which the Long scalar refuses
// rather than truncating (regress #15). A number too big even for float64 is
// left as json.Number, where every scalar reads it as invalid — which it is.
func normalizeVariables(v any) any {
	switch v := v.(type) {
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
		if f, err := v.Float64(); err == nil {
			return f
		}
		return v
	case map[string]any:
		for k, val := range v {
			v[k] = normalizeVariables(val)
		}
		return v
	case []any:
		for i, val := range v {
			v[i] = normalizeVariables(val)
		}
		return v
	}
	return v
}

type graphqlRequest struct {
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables"`
	OperationName string         `json:"operationName"`
	// Extensions is the standard GraphQL-over-HTTP escape hatch (persisted
	// queries, tracing). It is an open map so the strict body decoder accepts
	// a spec-compliant client while still rejecting a misspelled `query`.
	Extensions map[string]any `json:"bundles,omitempty"`
}

// cachedSchema is one repository's built schema plus the registry fingerprint it
// was built from.
type cachedSchema struct {
	key    string
	schema graphql.Schema
}

func (h *handler) postGraphQL(w http.ResponseWriter, r *http.Request) {
	var req graphqlRequest
	if err := decodeGraphQLBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "query required")
		return
	}
	ctx := r.Context()
	ds := DatasetFrom(ctx)
	schema, err := h.schemaFor(ctx, ds)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	result := graphql.Do(graphql.Params{
		Schema:         *schema,
		RequestString:  req.Query,
		VariableValues: req.Variables,
		OperationName:  req.OperationName,
		Context:        ctx,
	})
	attachProblemExtensions(result)
	writeJSON(w, http.StatusOK, result)
}

// attachProblemExtensions puts the ONE wire problem object into
// each resolver-originated error's `bundles`, so a GraphQL error carries
// the same {code, message, problems} a REST caller would see. Query
// syntax/validation errors have no underlying resolver error and are left with
// their native GraphQL shape.
func attachProblemExtensions(result *graphql.Result) {
	for i := range result.Errors {
		if result.Errors[i].Extensions != nil {
			continue
		}
		var located *gqlerrors.Error
		if !errors.As(result.Errors[i].OriginalError(), &located) || located.OriginalError == nil {
			continue
		}
		_, p := problemFor(located.OriginalError)
		ext := map[string]any{"code": p.Code, "message": p.Message}
		if len(p.Problems) > 0 {
			ext["problems"] = p.Problems
		}
		result.Errors[i].Extensions = ext
	}
}

// schemaFor returns the repository's schema, rebuilding it when the type
// registry's fingerprint changed (connector installs, schema deploys).
func (h *handler) schemaFor(ctx context.Context, ds substrate.Dataset) (*graphql.Schema, error) {
	types, err := ds.Kinds(ctx)
	if err != nil {
		return nil, err
	}
	key := registryKey(types)
	repository := ds.Repository().Name

	h.schemaMu.Lock()
	defer h.schemaMu.Unlock()
	if c, ok := h.schemaCache[repository]; ok && c.key == key {
		return &c.schema, nil
	}
	schema, err := buildSchema(types)
	if err != nil {
		return nil, err
	}
	c := &cachedSchema{key: key, schema: schema}
	h.schemaCache[repository] = c
	return &c.schema, nil
}

func registryKey(types []substrate.KindInfo) string {
	ids := make([]string, 0, len(types))
	for _, t := range types {
		// The schema builds fields from the DEFINITION, so the key must move
		// with it: schema is records, and a property added through the
		// record path activates on commit — not on the next type add/remove.
		// json.Marshal sorts map keys, so equal definitions hash equal.
		def, _ := json.Marshal(t.Definition)
		ids = append(ids, t.Identity+"@"+t.Version+"@"+t.Plural+"@"+string(def))
	}
	sort.Strings(ids)
	sum := sha256.New()
	for _, id := range ids {
		_, _ = sum.Write([]byte(id))
		_, _ = sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}
