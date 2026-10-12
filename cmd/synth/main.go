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
	"os/exec"
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
	repo := fs.String("repo", ".", "the substrate checkout the e2e runner and the bundles are read from (any directory inside it)")
	ctl := fs.String("ctl", "", "the substratectl binary the runner applies bundles with (default <repo>/bin/substratectl)")
	python := fs.String("python", "/usr/bin/python3", "the interpreter that runs the e2e runner")
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
		if e.Name() == "manual" {
			if err := applyYAML(ctx, c, filepath.Join(base, "labs.yaml"), stdout); err != nil {
				return err
			}
			continue
		}
		// A provider with recordings connects and syncs through the e2e
		// runner first (WHOOP, Google Fit, Garmin's connect step); one with
		// deliveries posts them after (Garmin's pushes, Apple Health).
		if _, err := os.Stat(filepath.Join(base, "recordings")); err == nil {
			if err := runSync(ctx, c, e.Name(), base, *repo, *ctl, *python, stdout, stderr); err != nil {
				return fmt.Errorf("%s: %w", e.Name(), err)
			}
		}
		if _, err := os.Stat(filepath.Join(base, "deliveries")); err == nil {
			if err := postDeliveries(ctx, c, base, *key, *settle, stdout, stderr); err != nil {
				return fmt.Errorf("%s: %w", e.Name(), err)
			}
		}
	}
	return nil
}

// repoRoot walks up from dir to the directory holding go.mod.
func repoRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("not inside the substrate checkout: pass -repo")
		}
		dir = parent
	}
}

// runSync drives the provider's real sync over the generated recordings
// with the provider e2e runner (internal/providere2e/runner/e2e.py): it
// installs the bundle rewired at a mock serving `recordings/`, connects the
// account through the mock's OAuth stub, drains the sync and switches the
// provider's schedule off, since the mock dies with the run. The runner
// reads its suite layout from SUBSTRATE_E2E_ROOT, so a copy is assembled
// with the emitter's `e2e.json` in place of the recorded one and no
// scenario, and a substratectl context for the repository is written for
// it under a temporary config.
func runSync(ctx context.Context, c *client, provider, base, repo, ctl, python string, stdout, stderr io.Writer) error {
	root, err := repoRoot(repo)
	if err != nil {
		return err
	}
	if ctl == "" {
		ctl = filepath.Join(root, "bin", "substratectl")
	}
	if _, err := os.Stat(ctl); err != nil {
		return fmt.Errorf("no substratectl at %s (mise run build:cli, or -ctl)", ctl)
	}
	suite := filepath.Join(root, "internal", "providere2e")
	tmp, err := os.MkdirTemp("", "synth-"+provider+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := copyDir(filepath.Join(suite, "runner"), filepath.Join(tmp, "runner")); err != nil {
		return err
	}
	pdir := filepath.Join(tmp, "providers", provider)
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		return err
	}
	e2e := filepath.Join(base, "e2e.json")
	if _, err := os.Stat(e2e); err != nil {
		e2e = filepath.Join(suite, "providers", provider, "e2e.json")
	}
	if err := copyFile(e2e, filepath.Join(pdir, "e2e.json")); err != nil {
		return err
	}
	cfg := filepath.Join(tmp, "substratectl.yaml")
	cfgBody, err := yaml.Marshal(map[string]any{
		"currentContext": c.authority,
		"contexts": []map[string]any{{
			"name": c.authority, "server": c.server, "repository": c.authority, "token": c.token,
		}},
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(cfg, cfgBody, 0o600); err != nil {
		return err
	}
	args := []string{
		filepath.Join(tmp, "runner", "e2e.py"), provider,
		"--server", c.server, "--authority", c.authority, "--ctl", ctl,
		"--recordings", filepath.Join(base, "recordings"), "--quiesce",
	}
	cmd := exec.CommandContext(ctx, python, args...) //nolint:gosec // every argument is this command's own
	cmd.Dir = tmp
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.Env = append(os.Environ(),
		"SUBSTRATE_TOKEN="+c.token,
		"SUBSTRATECTL_CONFIG="+cfg,
		"SUBSTRATE_E2E_ROOT="+tmp,
		"SUBSTRATE_E2E_REPO="+root,
		"PYTHONUNBUFFERED=1",
	)
	fmt.Fprintf(stdout, "%s: connecting and syncing through the e2e runner over %s\n", provider, filepath.Join(base, "recordings"))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("the e2e runner failed: %w", err)
	}
	return nil
}

func copyFile(src, dst string) error {
	raw, err := os.ReadFile(src) //nolint:gosec // paths this command assembled
	if err != nil {
		return err
	}
	return os.WriteFile(dst, raw, 0o644) //nolint:gosec // a copy of a tree file
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			if d.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		return copyFile(p, filepath.Join(dst, rel))
	})
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
