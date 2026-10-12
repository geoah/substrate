// synth generates synthetic provider data for one invented person, in the
// shapes the shipped provider functions read, and loads it into a running
// substrate. It is a development tool: the demo, the screenshots and the
// contract tests in internal/providertest all start from it.
//
//	synth generate [-out DIR] [-providers whoop,garmin,googlefit,applehealth,manual]
//	               [-from 2026-07-14] [-to 2026-10-11] [-seed ada] [-authority ada.example.com]
//	synth load -dir DIR -server URL -authority AUTHORITY [-providers ...] [-key KEY]
//
// `generate` writes one directory per provider under DIR: mock recordings
// named the way internal/providere2e/runner/mockserver.py finds them (WHOOP,
// Google Fit, Garmin's connect step), webhook requests in posting order
// (Garmin pushes, Apple Health deliveries) and the hand-entered records
// (`manual/labs.yaml`). `load` posts the webhook requests to the server's
// door, one at a time, waiting for each run, and applies the records; the
// mock-driven syncs run through the provider e2e runner, and `load` prints
// the exact command. The bearer comes from SUBSTRATE_TOKEN, never argv.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/synth"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "synth:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: synth generate|load [flags]")
	}
	switch args[0] {
	case "generate":
		return generate(args[1:], stdout)
	case "load":
		return load(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown command %q: generate or load", args[0])
	}
}

func emitters(names, authority string) ([]synth.Emitter, error) {
	all := synth.Emitters()
	for i, e := range all {
		if m, ok := e.(synth.Manual); ok {
			m.Authority = authority
			all[i] = m
		}
	}
	if names == "" {
		return all, nil
	}
	var out []synth.Emitter
	for _, n := range strings.Split(names, ",") {
		n = strings.TrimSpace(n)
		found := false
		for _, e := range all {
			if e.Name() == n {
				out = append(out, e)
				found = true
			}
		}
		if !found {
			var known []string
			for _, e := range all {
				known = append(known, e.Name())
			}
			return nil, fmt.Errorf("unknown provider %q: one of %s", n, strings.Join(known, ", "))
		}
	}
	return out, nil
}

func generate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	out := fs.String("out", ".dev/synth", "the directory to write under, one subdirectory per provider")
	providers := fs.String("providers", "", "comma-separated providers; empty is every one")
	from := fs.String("from", "", "the first day, YYYY-MM-DD (default 2026-07-14)")
	to := fs.String("to", "", "the last day, YYYY-MM-DD (default 2026-10-11)")
	seed := fs.String("seed", "", "the seed every value derives from (default ada)")
	zone := fs.String("zone", "", "the home zone (default Europe/Athens)")
	authority := fs.String("authority", "ada.example.com", "the repository the manual records are written to")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts := synth.Options{Seed: *seed, HomeZone: *zone}
	var err error
	if *from != "" {
		if opts.Start, err = synth.ParseDate(*from); err != nil {
			return fmt.Errorf("-from: %w", err)
		}
	}
	if *to != "" {
		if opts.End, err = synth.ParseDate(*to); err != nil {
			return fmt.Errorf("-to: %w", err)
		}
	}
	p, err := synth.NewPersona(opts)
	if err != nil {
		return err
	}
	ems, err := emitters(*providers, *authority)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: %s to %s, seed %q, home %s\n", p.Person.Name, p.Options.Start, p.Options.End, p.Options.Seed, p.Options.HomeZone)
	for _, e := range ems {
		started := time.Now()
		o, err := e.Emit(p)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		paths, err := synth.WriteOutput(*out, e, o)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		var size int64
		for _, pth := range paths {
			if st, err := os.Stat(pth); err == nil {
				size += st.Size()
			}
		}
		fmt.Fprintf(stdout, "  %-12s %4d files, %4d deliveries, %6.1f MB, %s -> %s\n",
			e.Name(), len(o.Files), len(o.Deliveries), float64(size)/1e6, time.Since(started).Round(time.Millisecond), filepath.Join(*out, e.Name()))
	}
	return nil
}

// --- load -------------------------------------------------------------------

type client struct {
	server, token, authority string
	hc                       *http.Client
}

func (c *client) do(ctx context.Context, method, path string, body []byte, headers map[string]string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.server+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil && headers["content-type"] == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, out, err
}

// put writes one record document (kind, metadata.id, data.properties).
func (c *client) put(ctx context.Context, doc map[string]any) error {
	kind, _ := doc["kind"].(string)
	meta, _ := doc["metadata"].(map[string]any)
	id, _ := meta["id"].(string)
	data, _ := doc["data"].(map[string]any)
	if kind == "" || id == "" {
		return fmt.Errorf("document without kind or id: %v", doc)
	}
	body, err := json.Marshal(map[string]any{"properties": data["properties"]})
	if err != nil {
		return err
	}
	st, out, err := c.do(ctx, http.MethodPut, "/api/v1/"+kind+"/"+url.PathEscape(id), body, nil)
	if err != nil {
		return err
	}
	if st >= 300 {
		return fmt.Errorf("PUT %s/%s: %d %s", kind, id, st, truncate(out))
	}
	return nil
}

func truncate(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// trigger reads one trigger record's properties and status.
func (c *client) trigger(ctx context.Context, id string) (map[string]any, error) {
	st, out, err := c.do(ctx, http.MethodGet, "/api/v1/substrate.reamde.dev/core/trigger/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return nil, err
	}
	if st != http.StatusOK {
		return nil, fmt.Errorf("trigger %s: %d %s", id, st, truncate(out))
	}
	var rec map[string]any
	if err := json.Unmarshal(out, &rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// arm enables the trigger with the key, keeping its other properties.
func (c *client) arm(ctx context.Context, id, key string, headers []string) error {
	rec, err := c.trigger(ctx, id)
	if err != nil {
		return err
	}
	props, _ := rec["properties"].(map[string]any)
	source, _ := props["source"].(map[string]any)
	if source == nil {
		source = map[string]any{}
	}
	webhook, _ := source["webhook"].(map[string]any)
	if webhook == nil {
		webhook = map[string]any{}
	}
	webhook["key"] = key
	if len(headers) > 0 {
		webhook["headers"] = headers
	}
	source["webhook"] = webhook
	body, err := json.Marshal(map[string]any{"properties": map[string]any{"enabled": true, "source": source}})
	if err != nil {
		return err
	}
	st, out, err := c.do(ctx, http.MethodPatch, "/api/v1/substrate.reamde.dev/core/trigger/"+url.PathEscape(id), body, nil)
	if err != nil {
		return err
	}
	if st >= 300 {
		return fmt.Errorf("arming %s: %d %s", id, st, truncate(out))
	}
	return nil
}

// settled reports whether the trigger has nothing pending or in flight, and
// how many deliveries it has parked, from the trigger status route.
func (c *client) settled(ctx context.Context, id string) (bool, int64, error) {
	st, out, err := c.do(ctx, http.MethodGet, "/api/v1/"+c.authority+"/trigger/status", nil, nil)
	if err != nil {
		return false, 0, err
	}
	if st != http.StatusOK {
		return false, 0, fmt.Errorf("trigger status: %d %s", st, truncate(out))
	}
	// The route answers substrate.OperationalList: `{"items": [...]}`.
	var list struct {
		Items []struct {
			ID       string `json:"id"`
			Parked   int64  `json:"parked"`
			Pending  int64  `json:"pending"`
			InFlight int64  `json:"inFlight"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return false, 0, fmt.Errorf("trigger status: %w", err)
	}
	for _, s := range list.Items {
		if s.ID == id {
			return s.Pending == 0 && s.InFlight == 0, s.Parked, nil
		}
	}
	return false, 0, fmt.Errorf("trigger %s is not in the status list", id)
}

func (c *client) awaitSettled(ctx context.Context, id string, within time.Duration) (int64, error) {
	deadline := time.Now().Add(within)
	for {
		ok, parked, err := c.settled(ctx, id)
		if err != nil {
			return 0, err
		}
		if ok {
			return parked, nil
		}
		if time.Now().After(deadline) {
			return parked, fmt.Errorf("trigger %s did not settle within %s", id, within)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func load(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("load", flag.ContinueOnError)
	dir := fs.String("dir", ".dev/synth", "the directory `generate` wrote")
	server := fs.String("server", "http://127.0.0.1:8080", "the substrate's base URL")
	authority := fs.String("authority", "", "the repository (its authority)")
	providers := fs.String("providers", "", "comma-separated providers; empty is every one with output in -dir")
	key := fs.String("key", "synth-webhook-key-0123456789abcdef", "the webhook key to arm the push triggers with")
	settle := fs.Duration("settle", 5*time.Minute, "how long to wait for each delivery's run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	token := os.Getenv("SUBSTRATE_TOKEN")
	if token == "" {
		return errors.New("SUBSTRATE_TOKEN is not set")
	}
	if *authority == "" {
		return errors.New("-authority is required")
	}
	c := &client{server: strings.TrimRight(*server, "/"), token: token, authority: *authority, hc: &http.Client{Timeout: 2 * time.Minute}}
	ctx := context.Background()
	ems, err := emitters(*providers, *authority)
	if err != nil {
		return err
	}
	for _, e := range ems {
		base := filepath.Join(*dir, e.Name())
		if _, err := os.Stat(base); err != nil {
			if *providers == "" {
				continue
			}
			return fmt.Errorf("%s: nothing generated under %s", e.Name(), base)
		}
		switch e.Name() {
		case "manual":
			if err := applyYAML(ctx, c, filepath.Join(base, "labs.yaml"), stdout); err != nil {
				return err
			}
		case "whoop", "googlefit":
			fmt.Fprintf(stdout, "%s: a mock-driven sync; run the provider e2e runner over %s:\n", e.Name(), base)
			fmt.Fprintf(stdout, "  SUBSTRATE_TOKEN=… SUBSTRATECTL_CONFIG=… /usr/bin/python3 internal/providere2e/runner/e2e.py %s --server %s --authority %s --ctl bin/substratectl --recordings %s\n",
				e.Name(), c.server, c.authority, filepath.Join(base, "recordings"))
		default:
			if err := postDeliveries(ctx, c, base, *key, *settle, stdout, stderr); err != nil {
				return fmt.Errorf("%s: %w", e.Name(), err)
			}
		}
	}
	return nil
}

func applyYAML(ctx context.Context, c *client, path string, stdout io.Writer) error {
	raw, err := os.ReadFile(path) //nolint:gosec // the path generate wrote
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	n := 0
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("%s: %w", path, err)
		}
		if err := c.put(ctx, normalise(doc)); err != nil {
			return err
		}
		n++
	}
	fmt.Fprintf(stdout, "manual: %d records written from %s\n", n, path)
	return nil
}

// normalise turns yaml's map[string]any nesting into JSON-able maps; yaml.v3
// already decodes mappings as map[string]any, so this only guards integers.
func normalise(v map[string]any) map[string]any { return v }

type envelope struct {
	Request struct {
		Headers map[string]string `json:"headers"`
		Query   map[string]string `json:"query"`
		Body    struct {
			Text string `json:"text"`
		} `json:"body"`
	} `json:"request"`
	Trigger string `json:"trigger"`
}

func postDeliveries(ctx context.Context, c *client, base, key string, settle time.Duration, stdout, stderr io.Writer) error {
	files, err := filepath.Glob(filepath.Join(base, "deliveries", "*.json"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no deliveries under %s", base)
	}
	sort.Strings(files)
	armed := map[string]bool{}
	started := time.Now()
	for i, f := range files {
		raw, err := os.ReadFile(f) //nolint:gosec // the path generate wrote
		if err != nil {
			return err
		}
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		if env.Trigger == "" {
			return fmt.Errorf("%s names no trigger", f)
		}
		if !armed[env.Trigger] {
			var headers []string
			for h := range env.Request.Headers {
				if h == "garmin-client-id" {
					headers = append(headers, h)
				}
			}
			if err := c.arm(ctx, env.Trigger, key, headers); err != nil {
				return err
			}
			armed[env.Trigger] = true
		}
		path := "/webhooks/" + c.authority + "/" + env.Trigger
		headers := map[string]string{}
		for k, v := range env.Request.Headers {
			if k == "content-length" {
				continue
			}
			headers[k] = v
		}
		if env.Request.Headers["authorization"] == "" {
			path += "/" + key
		} else {
			headers["authorization"] = "Bearer " + key
		}
		if len(env.Request.Query) > 0 {
			q := url.Values{}
			for k, v := range env.Request.Query {
				q.Set(k, v)
			}
			path += "?" + q.Encode()
		}
		st, out, err := c.do(ctx, http.MethodPost, path, []byte(env.Request.Body.Text), headers)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		if st >= 300 {
			return fmt.Errorf("%s: %d %s", filepath.Base(f), st, truncate(out))
		}
		parked, err := c.awaitSettled(ctx, env.Trigger, settle)
		if err != nil {
			return err
		}
		if parked > 0 {
			return fmt.Errorf("%s: %d deliveries parked on %s; `substratectl trigger parked %s` names why", filepath.Base(f), parked, env.Trigger, env.Trigger)
		}
		fmt.Fprintf(stdout, "\r%s: %d/%d posted (%s)", filepath.Base(base), i+1, len(files), time.Since(started).Round(time.Second))
	}
	fmt.Fprintln(stdout)
	_ = stderr
	return nil
}
