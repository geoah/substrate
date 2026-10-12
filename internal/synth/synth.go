package synth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// File is one generated artifact: a mock recording, a webhook request or a
// YAML document, under a relative path inside the provider's directory.
type File struct {
	Path string
	Body []byte
}

// Output is what one emitter produces: files to write, the requests to POST
// at a webhook door in order (each one also saved as a file), and what the
// repository must hold before the data can land.
type Output struct {
	// Files are the artifacts, relative to the provider's directory.
	Files []File
	// Deliveries are the webhook requests in the order they must be posted,
	// for providers that receive pushes. Each is saved as a file too.
	Deliveries []Delivery
	// Setup is what `load` puts in place first: saved as `setup.json`
	// beside the files, so the loader needs no emitter at load time.
	Setup Setup
}

// Setup is the repository state a provider's data needs: the provider to
// install (a push provider; a mock-driven one is installed rewired by the
// e2e runner), the samples to import after it (the adapter, which requires
// the provider), and the records to write before the first delivery (the
// account the deliveries name). A sample imported after rows landed is
// replayed from the start of the changelog by the loader.
type Setup struct {
	// Provider is the package to install through the catalog, such as
	// providers.substrate.reamde.dev/applehealth; empty for a mock-driven
	// provider, which the e2e runner installs rewired.
	Provider string `json:"provider,omitempty"`
	// Samples are the sample packages to import, in order, such as
	// samples.substrate.reamde.dev/healthapple. The observations and health
	// samples are imported first whatever the list says.
	Samples []string `json:"samples,omitempty"`
	// Records are documents (kind, metadata.id, data.properties) to write
	// before the deliveries post, such as the account record.
	Records []map[string]any `json:"records,omitempty"`
}

// Delivery is one webhook request as the door builds it: the same
// `{"request": {...}}` envelope the provider tests under
// internal/providertest/testdata hold, so one shape serves the tests, the
// files on disk and the loader.
type Delivery struct {
	// Name is the file the request is saved as, in posting order.
	Name string
	// Trigger is the trigger id the request is posted to.
	Trigger string
	// Headers are the request headers, lower-cased names.
	Headers map[string]string
	// Query is the request's query string pairs.
	Query map[string]string
	// Body is the raw body.
	Body []byte
}

// Envelope is the delivery as `{"request": {...}}` JSON.
func (d Delivery) Envelope() ([]byte, error) {
	headers := map[string]any{}
	for k, v := range d.Headers {
		headers[strings.ToLower(k)] = v
	}
	headers["content-length"] = fmt.Sprint(len(d.Body))
	req := map[string]any{
		"method":      http.MethodPost,
		"contentType": d.Headers["content-type"],
		"headers":     headers,
		"query":       orEmpty(d.Query),
		"body":        map[string]any{"text": string(d.Body)},
	}
	return json.MarshalIndent(map[string]any{"trigger": d.Trigger, "request": req}, "", " ")
}

func orEmpty(m map[string]string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Emitter turns the persona into one provider's data.
type Emitter interface {
	// Name is the provider's package name: whoop, garmin, googlefit,
	// applehealth, or manual for the hand-entered records.
	Name() string
	// Emit generates the output.
	Emit(p *Persona) (*Output, error)
}

// registry holds every emitter a provider file registered from its init,
// so adding a provider is one file and never an edit here.
var registry = map[string]Emitter{}

func register(e Emitter) { registry[e.Name()] = e }

func init() { register(Manual{}) }

// loadOrder is the order the demo loads providers: the OAuth ones first so
// their syncs run while the pushes post, the hand-entered records last.
var loadOrder = []string{"whoop", "googlefit", "garmin", "applehealth", "manual"}

// Emitters is every emitter, in load order; one registered under a name
// the order does not list comes after them, by name.
func Emitters() []Emitter {
	var out []Emitter
	seen := map[string]bool{}
	for _, n := range loadOrder {
		if e, ok := registry[n]; ok {
			out = append(out, e)
			seen[n] = true
		}
	}
	var rest []string
	for n := range registry {
		if !seen[n] {
			rest = append(rest, n)
		}
	}
	sort.Strings(rest)
	for _, n := range rest {
		out = append(out, registry[n])
	}
	return out
}

// EmitterByName finds one emitter.
func EmitterByName(name string) (Emitter, bool) {
	for _, e := range Emitters() {
		if e.Name() == name {
			return e, true
		}
	}
	return nil, false
}

// WriteOutput writes the output under dir/<name>/, deliveries as numbered
// envelope files beside the rest, and answers the paths written.
func WriteOutput(dir string, e Emitter, out *Output) ([]string, error) {
	base := filepath.Join(dir, e.Name())
	if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, err
	}
	var written []string
	for _, f := range out.Files {
		p := filepath.Join(base, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, f.Body, 0o644); err != nil { //nolint:gosec // generated data, readable on purpose
			return nil, err
		}
		written = append(written, p)
	}
	for i, d := range out.Deliveries {
		body, err := d.Envelope()
		if err != nil {
			return nil, err
		}
		p := filepath.Join(base, "deliveries", fmt.Sprintf("%04d_%s.json", i+1, d.Name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, body, 0o644); err != nil { //nolint:gosec // generated data, readable on purpose
			return nil, err
		}
		written = append(written, p)
	}
	if out.Setup.Provider != "" || len(out.Setup.Samples) > 0 || len(out.Setup.Records) > 0 {
		p := filepath.Join(base, "setup.json")
		if err := os.WriteFile(p, JSON(out.Setup), 0o644); err != nil { //nolint:gosec // generated data, readable on purpose
			return nil, err
		}
		written = append(written, p)
	}
	sort.Strings(written)
	return written, nil
}

// JSON marshals v the way the recordings are stored: indented, keys in the
// order the map iterates (sorted), no HTML escaping.
func JSON(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(v); err != nil {
		panic(fmt.Sprintf("synth: encoding: %v", err))
	}
	return buf.Bytes()
}
