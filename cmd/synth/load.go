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
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/synth"
)

// --- load -------------------------------------------------------------------

// triggerVerbs is where the trigger verbs live: triggers are records of
// core's trigger kind, so the collection-level status and the per-trigger
// replay sit under core's package, whatever the repository's authority.
const triggerVerbs = "/api/v1/substrate.reamde.dev/core/trigger"

type client struct {
	server, token, authority string
	hc                       *http.Client
}

func (c *client) do(ctx context.Context, method, path string, body []byte, headers map[string]string) (int, []byte, error) {
	st, out, _, err := c.request(ctx, method, path, body, headers)
	return st, out, err
}

// request is do with the response's headers.
func (c *client) request(ctx context.Context, method, path string, body []byte, headers map[string]string) (int, []byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.server+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	// The token reaches the API alone: a webhook post carries the trigger's
	// key and the door never needs the repository's credential.
	if strings.HasPrefix(path, "/api/") {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil && headers["content-type"] == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, out, resp.Header, err
}

// postTries is how many times one delivery is posted before the load stops.
const postTries = 6

// postWebhook posts one delivery to the door, posting it again while the
// door answers 503 (after its Retry-After) or the connection fails, with a
// wait that doubles from a second. A delivery the door took before the
// connection failed is posted twice; the providers key their rows by the
// delivery's own ids, so the second post rewrites them unchanged.
func (c *client) postWebhook(ctx context.Context, path string, body []byte, headers map[string]string) (int, []byte, error) {
	wait := time.Second
	for try := 1; ; try++ {
		st, out, hdr, err := c.request(ctx, http.MethodPost, path, body, headers)
		if err == nil && st != http.StatusServiceUnavailable {
			return st, out, nil
		}
		if try == postTries || ctx.Err() != nil {
			return st, out, err
		}
		next := wait
		if secs, perr := strconv.Atoi(hdr.Get("Retry-After")); err == nil && perr == nil && secs >= 0 {
			next = time.Duration(secs) * time.Second
		}
		if err := sleep(ctx, next); err != nil {
			return st, out, err
		}
		wait = min(2*wait, 30*time.Second)
	}
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

// webhookKeyPattern is the trigger kind's pattern for source.webhook.key.
var webhookKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

// arm enables the trigger to take the loader's posts with the key, keeping
// its other properties, and answers whether it was enabled before. A key
// the trigger already has is the owner's: arm refuses another one unless
// replace says to replace it.
func (c *client) arm(ctx context.Context, id, key string, replace bool, headers []string) (bool, error) {
	rec, err := c.trigger(ctx, id)
	if err != nil {
		return false, err
	}
	props, _ := rec["properties"].(map[string]any)
	enabled, _ := props["enabled"].(bool)
	source, _ := props["source"].(map[string]any)
	if source == nil {
		source = map[string]any{}
	}
	webhook, _ := source["webhook"].(map[string]any)
	if webhook == nil {
		webhook = map[string]any{}
	}
	if had, _ := webhook["key"].(string); had != "" && had != key && !replace {
		return false, fmt.Errorf("%s already has a webhook key: pass that key with -key, or -replace-key to replace it", id)
	}
	webhook["key"] = key
	if len(headers) > 0 {
		webhook["headers"] = headers
	}
	source["webhook"] = webhook
	if err := c.patchTrigger(ctx, id, map[string]any{"enabled": true, "source": source}); err != nil {
		return false, fmt.Errorf("arming %s: %w", id, err)
	}
	return enabled, nil
}

func (c *client) patchTrigger(ctx context.Context, id string, props map[string]any) error {
	body, err := json.Marshal(map[string]any{"properties": props})
	if err != nil {
		return err
	}
	st, out, err := c.do(ctx, http.MethodPatch, "/api/v1/substrate.reamde.dev/core/trigger/"+url.PathEscape(id), body, nil)
	if err != nil {
		return err
	}
	if st >= 300 {
		return fmt.Errorf("%d %s", st, truncate(out))
	}
	return nil
}

// settled reports whether the trigger has nothing pending or in flight, and
// how many deliveries it has parked, from the trigger status route.
func (c *client) settled(ctx context.Context, id string) (bool, int64, error) {
	st, out, err := c.do(ctx, http.MethodGet, triggerVerbs+"/status", nil, nil)
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

// awaitSettled polls the trigger until nothing is pending or in flight and
// answers its parked count. The door records a delivery before it answers,
// so the first read sees it pending; the wait between reads starts at 20 ms
// and doubles to half a second, so a run that takes milliseconds is not
// waited on for longer.
func (c *client) awaitSettled(ctx context.Context, id string, within time.Duration) (int64, error) {
	deadline := time.Now().Add(within)
	wait := 20 * time.Millisecond
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
		if err := sleep(ctx, wait); err != nil {
			return parked, err
		}
		wait = min(2*wait, 500*time.Millisecond)
	}
}

// sleep waits d or until ctx is done.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func load(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("load", flag.ContinueOnError)
	dir := fs.String("dir", ".dev/synth", "the directory `generate` wrote")
	server := fs.String("server", "http://127.0.0.1:8080", "the substrate's base URL")
	authority := fs.String("authority", "", "the repository (its authority)")
	providers := fs.String("providers", "", "comma-separated providers; empty is every one with output in -dir")
	key := fs.String("key", "", "the webhook key to arm the push triggers with (default $SYNTH_WEBHOOK_KEY)")
	replaceKey := fs.Bool("replace-key", false, "replace a webhook key a push trigger already has")
	resume := fs.String("resume", "", "take up an interrupted load at PROVIDER/NNNN, such as garmin/0400: the providers before it and its own setup are skipped")
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
	if *key == "" {
		*key = os.Getenv("SYNTH_WEBHOOK_KEY")
	}
	c := &client{server: strings.TrimRight(*server, "/"), token: token, authority: *authority, hc: &http.Client{Timeout: 2 * time.Minute}}
	// An interrupt cancels the load instead of ending the process, so the
	// runner is stopped and the config holding the token is removed.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ems, err := emitters(*providers, *authority)
	if err != nil {
		return err
	}
	var loading []synth.Emitter
	for _, e := range ems {
		base := filepath.Join(*dir, e.Name())
		if _, err := os.Stat(base); err != nil {
			if *providers == "" {
				continue
			}
			return fmt.Errorf("%s: nothing generated under %s", e.Name(), base)
		}
		loading = append(loading, e)
		if _, posts := statOK(filepath.Join(base, "deliveries")); posts && !webhookKeyPattern.MatchString(*key) {
			return fmt.Errorf("%s posts to a webhook door: name its key with -key or SYNTH_WEBHOOK_KEY, 16 to 128 of A-Z, a-z, 0-9, _ and -", e.Name())
		}
	}
	post := &poster{c: c, key: *key, replaceKey: *replaceKey, settle: *settle, stdout: stdout}
	if *resume != "" {
		provider, n, err := parseResume(*resume)
		if err != nil {
			return err
		}
		for i, e := range loading {
			if e.Name() != provider {
				continue
			}
			if err := post.deliveries(ctx, provider, filepath.Join(*dir, provider), n); err != nil {
				return fmt.Errorf("%s: %w", provider, err)
			}
			loading = loading[i+1:]
			*resume = ""
			break
		}
		if *resume != "" {
			return fmt.Errorf("-resume %s: no %s output under %s", *resume, provider, *dir)
		}
	}
	for _, e := range loading {
		base := filepath.Join(*dir, e.Name())
		setup, err := readSetup(base)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		// A provider with recordings connects and syncs through the e2e
		// runner first (WHOOP, Google Fit, Garmin's connect step), which
		// installs it rewired; a push provider is installed from the
		// catalog. The samples come after the provider they require, and
		// the records (the account) before the first delivery. A sample
		// imported after the runner landed rows is replayed from the start.
		// The documents generate wrote (`labs.yaml`) need the samples too,
		// so they are applied after the import.
		_, hasRecordings := statOK(filepath.Join(base, "recordings"))
		_, hasDeliveries := statOK(filepath.Join(base, "deliveries"))
		if hasRecordings {
			if err := runSync(ctx, c, e.Name(), base, *repo, *ctl, *python, stdout, stderr); err != nil {
				return fmt.Errorf("%s: %w", e.Name(), err)
			}
		} else if setup.Provider != "" {
			if err := c.install(ctx, setup.Provider); err != nil {
				return fmt.Errorf("%s: %w", e.Name(), err)
			}
			fmt.Fprintf(stdout, "%s: installed %s\n", e.Name(), setup.Provider)
		}
		imported, err := c.importSamples(ctx, setup.Samples, stdout)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		for _, doc := range setup.Records {
			if err := c.put(ctx, doc); err != nil {
				return fmt.Errorf("%s: %w", e.Name(), err)
			}
		}
		if hasRecordings && len(imported) > 0 {
			if err := c.replaySamples(ctx, imported, stdout); err != nil {
				return fmt.Errorf("%s: %w", e.Name(), err)
			}
		}
		docs, err := filepath.Glob(filepath.Join(base, "*.yaml"))
		if err != nil {
			return err
		}
		for _, path := range docs {
			if err := applyYAML(ctx, c, path, stdout); err != nil {
				return fmt.Errorf("%s: %w", e.Name(), err)
			}
		}
		if hasDeliveries {
			if err := post.deliveries(ctx, e.Name(), base, 1); err != nil {
				return fmt.Errorf("%s: %w", e.Name(), err)
			}
		}
	}
	return nil
}

func statOK(path string) (os.FileInfo, bool) {
	st, err := os.Stat(path)
	return st, err == nil
}

// readSetup reads the provider's setup.json, empty when generate wrote none.
func readSetup(base string) (synth.Setup, error) {
	var s synth.Setup
	raw, err := os.ReadFile(filepath.Join(base, "setup.json")) //nolint:gosec // the path generate wrote
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, fmt.Errorf("setup.json: %w", err)
	}
	return s, nil
}

// catalogItem reads whether the repository holds a catalog package, and
// whether the server ships a version that would upgrade it.
func (c *client) catalogItem(ctx context.Context, pkg string) (held, upgrade bool, err error) {
	st, out, err := c.do(ctx, http.MethodGet, "/api/v1/catalog/"+url.PathEscape(pkg), nil, nil)
	if err != nil {
		return false, false, err
	}
	if st != http.StatusOK {
		return false, false, fmt.Errorf("reading %s from the catalog: %d %s", pkg, st, truncate(out))
	}
	var item struct {
		Installed bool            `json:"installed"`
		Upgrade   json.RawMessage `json:"upgrade"`
	}
	if err := json.Unmarshal(out, &item); err != nil {
		return false, false, fmt.Errorf("reading %s from the catalog: %w", pkg, err)
	}
	return item.Installed, len(item.Upgrade) > 0 && string(item.Upgrade) != "null", nil
}

// install installs a provider from the catalog, unless the repository holds
// it at the version this server ships.
func (c *client) install(ctx context.Context, pkg string) error {
	held, upgrade, err := c.catalogItem(ctx, pkg)
	if err != nil || (held && !upgrade) {
		return err
	}
	st, out, err := c.do(ctx, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(pkg)+"/install", []byte("{}"), nil)
	if err != nil {
		return err
	}
	if st >= 300 {
		return fmt.Errorf("installing %s: %d %s", pkg, st, truncate(out))
	}
	return nil
}

// importSamples imports the observations and health samples, then the
// listed ones in order. A sample the repository holds at the version this
// server ships is skipped and one it holds at an older version is upgraded;
// a re-import answers 200 like a first one, so the catalog says which is
// which. It answers the samples it imported or upgraded: a new record
// trigger starts at the changelog's head and an upgraded adapter rewrites
// only what it is handed, so those are the ones a replay must reach.
func (c *client) importSamples(ctx context.Context, samples []string, stdout io.Writer) ([]string, error) {
	var imported []string
	seen := map[string]bool{}
	for _, s := range append([]string{"samples.substrate.reamde.dev/observations", "samples.substrate.reamde.dev/health"}, samples...) {
		if seen[s] {
			continue
		}
		seen[s] = true
		held, upgrade, err := c.catalogItem(ctx, s)
		if err != nil {
			return nil, err
		}
		if held && !upgrade {
			continue
		}
		st, out, err := c.do(ctx, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(s)+"/import", []byte("{}"), nil)
		if err != nil {
			return nil, err
		}
		if st >= 300 {
			return nil, fmt.Errorf("importing %s: %d %s", s, st, truncate(out))
		}
		verb := "imported"
		if held {
			verb = "upgraded"
		}
		fmt.Fprintf(stdout, "%s %s\n", verb, s)
		imported = append(imported, s)
	}
	return imported, nil
}

// replaySamples replays the record triggers of the samples just imported
// from the start of the changelog, so rows that landed before the import
// reach the adapters.
func (c *client) replaySamples(ctx context.Context, samples []string, stdout io.Writer) error {
	st, out, err := c.do(ctx, http.MethodGet, triggerVerbs+"/status", nil, nil)
	if err != nil {
		return err
	}
	if st != http.StatusOK {
		return fmt.Errorf("trigger status: %d %s", st, truncate(out))
	}
	var list struct {
		Items []struct {
			ID       string `json:"id"`
			Kind     string `json:"kind"`
			Callable string `json:"callable"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return fmt.Errorf("trigger status: %w", err)
	}
	for _, s := range samples {
		// An imported sample's callables live under the repository's own
		// authority and the sample's package name.
		pkg := s[strings.LastIndex(s, "/")+1:]
		prefix := c.authority + "/" + pkg + "/"
		for _, t := range list.Items {
			if t.Kind != "record" || !strings.HasPrefix(t.Callable, prefix) {
				continue
			}
			st, out, err := c.do(ctx, http.MethodPost, triggerVerbs+"/"+url.PathEscape(t.ID)+"/replay", []byte(`{"from":0}`), nil)
			if err != nil {
				return err
			}
			if st >= 300 {
				return fmt.Errorf("replaying %s: %d %s", t.ID, st, truncate(out))
			}
			fmt.Fprintf(stdout, "replaying %s from the start\n", t.ID)
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

// applyYAML writes every document of one file generate wrote. Each names
// the authority generate was given, in its kind and in its references, so a
// file for another repository is refused before anything is written.
func applyYAML(ctx context.Context, c *client, path string, stdout io.Writer) error {
	raw, err := os.ReadFile(path) //nolint:gosec // the path generate wrote
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	var docs []map[string]any
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("%s: %w", path, err)
		}
		kind, _ := doc["kind"].(string)
		if authority, _, _ := strings.Cut(kind, "/"); authority != c.authority {
			return fmt.Errorf("%s was generated for %s; generate it again with -authority %s", path, authority, c.authority)
		}
		docs = append(docs, doc)
	}
	for _, doc := range docs {
		if err := c.put(ctx, doc); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "%d records written from %s\n", len(docs), path)
	return nil
}

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

// parseResume reads -resume's PROVIDER/NNNN.
func parseResume(v string) (string, int, error) {
	provider, num, ok := strings.Cut(v, "/")
	n, err := strconv.Atoi(num)
	if !ok || provider == "" || err != nil || n < 1 {
		return "", 0, fmt.Errorf("-resume %s: want PROVIDER/NNNN, such as garmin/0400", v)
	}
	return provider, n, nil
}

// poster posts a provider's deliveries to the webhook door.
type poster struct {
	c          *client
	key        string
	replaceKey bool
	settle     time.Duration
	stdout     io.Writer
}

// deliveries posts the deliveries under base in order from the from-th,
// arming each trigger first and waiting for each run. It stops on a trigger
// it cannot arm or read, and on a delivery the door refuses or that parks,
// naming the -resume that takes the load up again. A trigger the loader found disabled is disabled again when it
// returns, so the door it opened with the key closes with it.
func (p *poster) deliveries(ctx context.Context, provider, base string, from int) (err error) {
	c := p.c
	files, err := filepath.Glob(filepath.Join(base, "deliveries", "*.json"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no deliveries under %s", base)
	}
	if from > len(files) {
		return fmt.Errorf("-resume %s/%04d: %s has %d deliveries", provider, from, base, len(files))
	}
	sort.Strings(files)
	armed := map[string]bool{}
	// parked is each trigger's parked count before this load posted to it:
	// a delivery an earlier run parked is not this run's to stop on.
	parked := map[string]int64{}
	defer func() {
		for id, wasEnabled := range armed {
			if wasEnabled {
				continue
			}
			if derr := c.patchTrigger(context.WithoutCancel(ctx), id, map[string]any{"enabled": false}); derr != nil && err == nil {
				err = fmt.Errorf("disabling %s again: %w", id, derr)
			}
		}
	}()
	started := time.Now()
	for i := from - 1; i < len(files); i++ {
		f := files[i]
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
		if _, ok := armed[env.Trigger]; !ok {
			var headers []string
			for h := range env.Request.Headers {
				if h == "garmin-client-id" {
					headers = append(headers, h)
				}
			}
			wasEnabled, err := c.arm(ctx, env.Trigger, p.key, p.replaceKey, headers)
			if err != nil {
				return fmt.Errorf("%w; take the load up with -resume %s/%04d", err, provider, i+1)
			}
			armed[env.Trigger] = wasEnabled
			if _, parked[env.Trigger], err = c.settled(ctx, env.Trigger); err != nil {
				return fmt.Errorf("%w; take the load up with -resume %s/%04d", err, provider, i+1)
			}
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
			path += "/" + p.key
		} else {
			headers["authorization"] = "Bearer " + p.key
		}
		if len(env.Request.Query) > 0 {
			q := url.Values{}
			for k, v := range env.Request.Query {
				q.Set(k, v)
			}
			path += "?" + q.Encode()
		}
		st, out, err := c.postWebhook(ctx, path, []byte(env.Request.Body.Text), headers)
		if err != nil {
			return fmt.Errorf("%s: %w; take the load up with -resume %s/%04d", filepath.Base(f), err, provider, i+1)
		}
		if st >= 300 {
			return fmt.Errorf("%s: %d %s; take the load up with -resume %s/%04d", filepath.Base(f), st, truncate(out), provider, i+1)
		}
		now, err := c.awaitSettled(ctx, env.Trigger, p.settle)
		if err != nil {
			return fmt.Errorf("%w; take the load up with -resume %s/%04d", err, provider, i+2)
		}
		if now > parked[env.Trigger] {
			return fmt.Errorf("%s parked on %s; `substratectl trigger parked %s` names why, and -resume %s/%04d takes the load up after it",
				filepath.Base(f), env.Trigger, env.Trigger, provider, i+2)
		}
		fmt.Fprintf(p.stdout, "\r%s: %d/%d posted (%s)", provider, i+1, len(files), time.Since(started).Round(time.Second))
	}
	fmt.Fprintln(p.stdout)
	return nil
}
