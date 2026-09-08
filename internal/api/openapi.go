package api

import (
	_ "embed"
	"encoding/json"
	"net/http"

	"gopkg.in/yaml.v3"
)

// openapiYAML is the REST contract as an OpenAPI 3.1 document, hand-written
// beside the handlers it describes and held to them by openapi_test.go: every
// mounted route is in it and nothing else is, and every component schema
// names the fields the Go struct serializes with the same required set. It is
// authored as YAML because a reviewer reads the diff; the server serves the
// same document as JSON, which is what a client generator fetches.
//
//go:embed openapi.yaml
var openapiYAML []byte

// openapiRoute is where the document is served: beside discovery, under the
// same well-known prefix, unauthenticated and repository-free, so a generator
// can fetch the contract before it holds a token.
const openapiRoute = "/.well-known/substrate/openapi.json"

// openapiJSON is the document re-encoded as JSON once at init. A document
// that does not parse is a build defect, not a runtime condition, so the
// failure is a panic here and a test failure in openapi_test.go.
var openapiJSON = mustOpenAPIJSON(openapiYAML)

func mustOpenAPIJSON(src []byte) []byte {
	var doc any
	if err := yaml.Unmarshal(src, &doc); err != nil {
		panic("openapi.yaml does not parse: " + err.Error())
	}
	out, err := json.Marshal(doc)
	if err != nil {
		panic("openapi.yaml does not encode as JSON: " + err.Error())
	}
	return out
}

// getOpenAPI serves GET /.well-known/substrate/openapi.json. No auth, no DB.
func (h *handler) getOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openapiJSON)
}
