// keptdir drives a running substrate through every sort of change the stored
// format holds, then reads back every record it serves. It is the half of
// .mise/keptdir.sh that speaks HTTP: the script builds a released tag, runs
// it, and keeps the repository directory this program made it write beside
// what this program read (docs/stored-format.md, "Kept directories").
//
//	keptdir -server URL -repository AUTHORITY -out DIR
//
// It registers the repository, so the server must have no repository of that
// name, no invite code and TOTP off. It writes DIR/records.json (every live
// and tombstoned record, as one GET of its path answers, in kind and id
// order, beside the list's row where the GET answered another record),
// DIR/triggers.json (each trigger's status and parked failures), and the
// password and the registration's token, DIR/password and DIR/token.
//
// What the history holds, by op and by effect: put, patch, delete, merge,
// split, gc and delivery; record, tombstone, purge, annotation, manager,
// former, resync, cursor, schedule, park, unpark, page, unpage and forget.
// Beside them: a sample imported and a catalogue installed, a kind edited, a
// property renamed and an enum value respelled over live records, a label
// cleared, a tombstone restored, sealed values and a blob.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	core     = "substrate.reamde.dev/core"
	password = "kept-directory-fixture-password"

	// Ids in the minted shape (decision 0228): a new record of a kind the
	// repository declared is refused under any other.
	idRelease      = "00000000000000000000000001"
	idFold         = "00000000000000000000000002"
	idRebuild      = "00000000000000000000000003"
	idCollect      = "00000000000000000000000004"
	idWinner       = "00000000000000000000000005"
	idLoser        = "00000000000000000000000006"
	idSplitA       = "00000000000000000000000007"
	idSplitB       = "00000000000000000000000008"
	idReport       = "00000000000000000000000009"
	idW1           = "00000000000000000000000010"
	idG1           = "00000000000000000000000011"
	idW2           = "00000000000000000000000012"
	idPurgeMe      = "00000000000000000000000013"
	idStaysDeleted = "00000000000000000000000014"
	idClient       = "00000000000000000000000015"

	// The release's GC sweep runs every five minutes, so a purge waits for one.
	sweepDeadline = 7 * time.Minute
	settle        = 90 * time.Second
)

var samples = []string{
	"samples.substrate.reamde.dev/people",
	"samples.substrate.reamde.dev/scheduling",
	"samples.substrate.reamde.dev/tasks",
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "keptdir:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("keptdir", flag.ContinueOnError)
	server := fs.String("server", "", "the substrate to drive, e.g. http://127.0.0.1:8080")
	repository := fs.String("repository", "kept.example.com", "the repository to register")
	out := fs.String("out", "", "the directory records.json, triggers.json, password and token are written to")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *server == "" || *out == "" {
		return errors.New("-server and -out are required")
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	d := &driver{base: strings.TrimRight(*server, "/"), authority: *repository, http: &http.Client{Timeout: 2 * time.Minute}}
	steps := []struct {
		what string
		fn   func() error
	}{
		{"register", d.register},
		{"declare", d.declare},
		{"write records, merges and a split", d.writeRecords},
		{"write sealed values and a blob", d.writeSealedAndBlob},
		{"run triggers to a cursor, a schedule, two parks and a paged drain", d.runTriggers},
		{"retry the parked deliveries", d.retryParked},
		{"rename a property and respell an enum value over live records", d.rename},
		{"purge a tombstone through the GC sweep", d.purge},
		{"leave a tombstone and forget a trigger", d.leaveTombstone},
	}
	for _, s := range steps {
		fmt.Fprintf(os.Stderr, "keptdir: %s\n", s.what)
		if err := s.fn(); err != nil {
			return fmt.Errorf("%s: %w", s.what, err)
		}
	}
	fmt.Fprintln(os.Stderr, "keptdir: read back every record")
	if err := d.capture(*out); err != nil {
		return fmt.Errorf("capture: %w", err)
	}
	if err := os.WriteFile(filepath.Join(*out, "token"), []byte(d.token), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(*out, "password"), []byte(password), 0o600)
}

type driver struct {
	base, authority, token string
	http                   *http.Client
}

func (d *driver) kind(pkgKind string) string { return d.authority + "/" + pkgKind }

// do sends one request and returns the status and body; a JSON body is
// marshaled, a []byte body is sent as it is.
func (d *driver) do(method, path string, body any, headers map[string]string) (int, []byte, error) {
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		r = bytes.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return 0, nil, err
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, d.base+path, r)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if d.token != "" {
		req.Header.Set("Authorization", "Bearer "+d.token)
	}
	for k, v := range headers {
		if v == "" {
			req.Header.Del(k)
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, err
}

// must is do that refuses anything but a 2xx and decodes the answer into out.
func (d *driver) must(method, path string, body, out any) error {
	status, raw, err := d.do(method, path, body, nil)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return fmt.Errorf("%s %s: %d %s", method, path, status, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func recordPath(kind, id string) string { return "/api/v1/" + kind + "/" + url.PathEscape(id) }

func (d *driver) put(kind, id string, body map[string]any) error {
	return d.must(http.MethodPut, recordPath(kind, id), body, nil)
}

func (d *driver) patch(kind, id string, body map[string]any) error {
	return d.must(http.MethodPatch, recordPath(kind, id), body, nil)
}

func (d *driver) del(kind, id string) error {
	return d.must(http.MethodDelete, recordPath(kind, id), nil, nil)
}

func (d *driver) exists(kind, id string) (bool, error) {
	status, raw, err := d.do(http.MethodGet, recordPath(kind, id), nil, nil)
	switch {
	case err != nil:
		return false, err
	case status == http.StatusOK:
		return true, nil
	case status == http.StatusNotFound:
		return false, nil
	}
	return false, fmt.Errorf("GET %s/%s: %d %s", kind, id, status, raw)
}

func (d *driver) apply(doc string) error {
	dec := yaml.NewDecoder(strings.NewReader(authored(doc, d.authority)))
	var docs []map[string]any
	for {
		var m map[string]any
		err := dec.Decode(&m)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if len(m) > 0 {
			docs = append(docs, m)
		}
	}
	return d.must(http.MethodPost, "/api/v1/vocabulary/apply", map[string]any{"documents": docs}, nil)
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(what string, deadline time.Duration, cond func() (bool, error)) error {
	end := time.Now().Add(deadline)
	for {
		ok, err := cond()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(end) {
			return fmt.Errorf("waited %s for %s", deadline, what)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func (d *driver) register() error {
	var out struct {
		Secret     string `json:"secret"`
		Repository string `json:"repository"`
	}
	if err := d.must(http.MethodPost, "/register", map[string]any{
		"repository": d.authority, "password": password, "skipTotp": true, "label": "keptdir",
	}, &out); err != nil {
		return err
	}
	if out.Secret == "" || out.Repository != d.authority {
		return fmt.Errorf("registration answered repository %q and no token", out.Repository)
	}
	d.token = out.Secret
	return nil
}

func (d *driver) declare() error {
	for _, id := range samples {
		if err := d.must(http.MethodPost, "/api/v1/catalog/"+url.PathEscape(id)+"/import", nil, nil); err != nil {
			return err
		}
	}
	if err := d.must(http.MethodPost, "/api/v1/catalog/"+url.PathEscape("substrate.reamde.dev/environment")+"/install", nil, nil); err != nil {
		return err
	}
	for _, doc := range []string{autoVocabulary, attachVocabulary(false), credsVocabulary} {
		if err := d.apply(doc); err != nil {
			return err
		}
	}
	return nil
}

// writeRecords writes the tasks: a reference, labels, an annotation, a
// transition, the last label cleared, a delete and the put that restores it,
// a merge that stands and one that is split.
func (d *driver) writeRecords() error {
	task, project := d.kind("tasks/task"), d.kind("tasks/project")
	due := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second).Format(time.RFC3339)
	steps := []func() error{
		func() error {
			return d.put(project, idRelease, map[string]any{"properties": map[string]any{"name": "The release"}})
		},
		func() error {
			return d.put(task, idFold, map[string]any{
				"properties": map[string]any{
					"name": "Ship the fold", "description": "carry the values, not just the names",
					"dueAt": due, "url": "https://example.com/1", "project": idRelease,
				},
				"labels": map[string]any{"owner/pinned": true},
			})
		},
		func() error {
			return d.put(task, idRebuild, map[string]any{"properties": map[string]any{
				"name": "Rebuild the repository", "description": "replay every entry", "project": idRelease,
			}})
		},
		func() error {
			return d.put(task, idCollect, map[string]any{"properties": map[string]any{"name": "Collect me", "description": "and then go"}})
		},
		func() error {
			return d.patch(task, idFold, map[string]any{
				"properties":  map[string]any{"description": "values, replayable", "url": nil},
				"labels":      map[string]any{"owner/pinned": nil, "owner/urgent": "yes"},
				"annotations": map[string]any{"owner/note": map[string]any{"why": "the payload"}},
			})
		},
		func() error {
			return d.patch(task, idFold, map[string]any{"properties": map[string]any{"status": "done"}})
		},
		func() error {
			return d.patch(task, idFold, map[string]any{"labels": map[string]any{"owner/urgent": nil}})
		},
		func() error { return d.del(task, idCollect) },
		func() error {
			return d.put(task, idCollect, map[string]any{
				"properties": map[string]any{"name": "Collect me", "description": "restored"},
				"labels":     map[string]any{"owner/kept": true},
			})
		},
		func() error {
			return d.put(task, idWinner, map[string]any{"properties": map[string]any{"name": "Winner", "description": "the record that stays"}})
		},
		func() error {
			return d.put(task, idLoser, map[string]any{"properties": map[string]any{"name": "Loser", "description": "the record that folds in"}})
		},
		func() error {
			return d.must(http.MethodPost, "/api/v1/merge", map[string]any{"kind": task, "winner": idWinner, "loser": idLoser}, nil)
		},
		func() error {
			return d.put(task, idSplitA, map[string]any{"properties": map[string]any{"name": "Split winner"}})
		},
		func() error {
			return d.put(task, idSplitB, map[string]any{"properties": map[string]any{"name": "Split loser"}})
		},
		func() error {
			var merge struct {
				ID   string `json:"id"`
				Kind string `json:"kind"`
			}
			if err := d.must(http.MethodPost, "/api/v1/merge", map[string]any{"kind": task, "winner": idSplitA, "loser": idSplitB}, &merge); err != nil {
				return err
			}
			if merge.Kind != core+"/recordmerge" || merge.ID == "" {
				return fmt.Errorf("merge answered %+v, want the recordmerge record", merge)
			}
			return d.must(http.MethodPost, "/api/v1/split", map[string]any{"merge": merge.ID}, nil)
		},
	}
	for _, s := range steps {
		if err := s(); err != nil {
			return err
		}
	}
	return nil
}

// writeSealedAndBlob stores a blob and the record naming it, edits that kind
// so a later record carries a newer kind version, and writes sealed values.
func (d *driver) writeSealedAndBlob() error {
	status, raw, err := d.do(http.MethodPut, "/api/v1/blobs?name=report.txt",
		[]byte("the report the kept directory attaches"), map[string]string{"Content-Type": "text/plain"})
	if err != nil {
		return err
	}
	if status != http.StatusCreated {
		return fmt.Errorf("put blob: %d %s", status, raw)
	}
	var blob struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(raw, &blob); err != nil {
		return fmt.Errorf("blob upload answered %s: %w", raw, err)
	}
	if blob.Digest == "" {
		return fmt.Errorf("blob upload answered %s, with no digest", raw)
	}
	if err := d.apply(attachVocabulary(true)); err != nil {
		return err
	}
	if err := d.put(d.kind("attach/file"), idReport, map[string]any{"properties": map[string]any{
		"name": "report.txt", "data": blob.Digest, "notes": "attached in the kept directory",
	}}); err != nil {
		return err
	}
	return d.put(d.kind("creds/oauthclient"), idClient, map[string]any{"properties": map[string]any{
		"clientId": "kept-client", "clientSecret": "kept-client-secret-value",
		"accessToken": "kept-access-token-value",
		"expiresAt":   time.Now().UTC().Add(time.Hour).Truncate(time.Second).Format(time.RFC3339),
	}})
}

func (d *driver) callable(name string) string {
	return core + "/function/" + d.authority + "/auto/" + name
}

func (d *driver) trigger(id string, source map[string]any, callable string) error {
	return d.put(core+"/trigger", id, map[string]any{"properties": map[string]any{
		"enabled": true, "source": source, "callable": d.callable(callable),
	}})
}

type parkedFailure struct {
	ID        int64  `json:"id"`
	LastError string `json:"lastError"`
}

func (d *driver) parked(trigger string) ([]parkedFailure, error) {
	var out struct {
		Items []parkedFailure `json:"items"`
	}
	err := d.must(http.MethodGet, "/api/v1/"+core+"/trigger/"+url.PathEscape(trigger)+"/parked", nil, &out)
	return out.Items, err
}

// parkedOnce waits for the trigger's one settled park: a failure that is
// still retrying names itself as not settled.
func (d *driver) parkedOnce(trigger string) (parkedFailure, error) {
	var got parkedFailure
	err := waitFor(trigger+" to park", settle, func() (bool, error) {
		p, err := d.parked(trigger)
		if err != nil || len(p) != 1 || strings.Contains(p[0].LastError, "has not settled") {
			return false, err
		}
		got = p[0]
		return true, nil
	})
	return got, err
}

// runTriggers writes a record trigger that delivers (a cursor), a paged
// drain that parks at page two (pages and a park), a webhook trigger that
// parks (an accepted request and a park), and a schedule that fires.
func (d *driver) runTriggers() error {
	task := d.kind("tasks/task")
	record := func(kind string) map[string]any {
		return map[string]any{"record": map[string]any{"kinds": []any{d.kind(kind)}}}
	}
	if err := d.trigger("on-mirror", record("auto/widget"), "mirror"); err != nil {
		return err
	}
	if err := d.trigger("on-page", record("auto/gadget"), "page"); err != nil {
		return err
	}
	if err := d.trigger("on-hook", map[string]any{"webhook": map[string]any{}}, "hook"); err != nil {
		return err
	}
	// One occurrence a minute in the past and the next an hour on, so the
	// schedule fires once while the driver runs.
	startsAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second).Format(time.RFC3339)
	if err := d.trigger("hourly", map[string]any{"schedule": map[string]any{
		"recurrence": "FREQ=HOURLY", "timezone": "UTC", "startsAt": startsAt,
	}}, "tick"); err != nil {
		return err
	}
	if err := d.put(d.kind("auto/widget"), idW1, map[string]any{"properties": map[string]any{"name": "one", "color": "red"}}); err != nil {
		return err
	}
	if err := d.put(d.kind("auto/gadget"), idG1, map[string]any{"properties": map[string]any{"name": "big"}}); err != nil {
		return err
	}
	if err := waitFor("the mirror to deliver", settle, func() (bool, error) { return d.exists(task, "t-"+idW1) }); err != nil {
		return err
	}
	if err := waitFor("pages 0 and 1", settle, func() (bool, error) { return d.exists(task, "p-1") }); err != nil {
		return err
	}
	if _, err := d.parkedOnce("on-page"); err != nil {
		return err
	}
	status, raw, err := d.do(http.MethodPost, "/webhooks/"+d.authority+"/on-hook", []byte(`{"say":"call the dentist"}`),
		map[string]string{"Content-Type": "application/json", "Authorization": ""})
	if err != nil {
		return err
	}
	if status != http.StatusAccepted {
		return fmt.Errorf("webhook: %d %s, want 202", status, raw)
	}
	if _, err := d.parkedOnce("on-hook"); err != nil {
		return err
	}
	return waitFor("the schedule to fire", settle, func() (bool, error) {
		var statuses struct {
			Items []struct {
				ID       string     `json:"id"`
				LastFire *time.Time `json:"lastFire"`
			} `json:"items"`
		}
		if err := d.must(http.MethodGet, "/api/v1/"+core+"/trigger/status", nil, &statuses); err != nil {
			return false, err
		}
		for _, s := range statuses.Items {
			if s.ID == "hourly" && s.LastFire != nil {
				return true, nil
			}
		}
		return false, nil
	})
}

// retryParked opens both gates and retries both parks, so the paged drain
// runs to its end and the webhook delivers.
func (d *driver) retryParked() error {
	for _, gate := range []string{"page-gate", "hook-gate"} {
		if err := d.put(d.kind("auto/flag"), gate, map[string]any{"properties": map[string]any{"name": gate}}); err != nil {
			return err
		}
	}
	for _, trigger := range []string{"on-page", "on-hook"} {
		p, err := d.parked(trigger)
		if err != nil {
			return err
		}
		if len(p) != 1 {
			return fmt.Errorf("%s holds %d parked failures, want 1", trigger, len(p))
		}
		path := fmt.Sprintf("/api/v1/%s/trigger/%s/parked/%d/retry", core, url.PathEscape(trigger), p[0].ID)
		if err := d.must(http.MethodPost, path, nil, nil); err != nil {
			return err
		}
		if err := waitFor(trigger+" to drain its park", settle, func() (bool, error) {
			p, err := d.parked(trigger)
			return len(p) == 0, err
		}); err != nil {
			return err
		}
	}
	if err := waitFor("the last page", settle, func() (bool, error) { return d.exists(d.kind("tasks/task"), "p-3") }); err != nil {
		return err
	}
	return waitFor("the webhook echo", settle, func() (bool, error) { return d.exists(d.kind("auto/echo"), "hook-echo") })
}

// rename disables the mirror, whose body reads `name`, writes a second
// widget, then admits the edit that renames `name` and respells `red`.
func (d *driver) rename() error {
	if err := d.patch(core+"/trigger", "on-mirror", map[string]any{"properties": map[string]any{"enabled": false}}); err != nil {
		return err
	}
	if err := d.put(d.kind("auto/widget"), idW2, map[string]any{"properties": map[string]any{"name": "two", "color": "green"}}); err != nil {
		return err
	}
	return d.apply(renameVocabulary)
}

// purge deletes a task and waits for the release's GC sweep to purge it.
func (d *driver) purge() error {
	task := d.kind("tasks/task")
	if err := d.put(task, idPurgeMe, map[string]any{"properties": map[string]any{"name": "Purge me"}}); err != nil {
		return err
	}
	if err := d.del(task, idPurgeMe); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "keptdir: waiting for the GC sweep, at most", sweepDeadline)
	return waitFor("the GC sweep", sweepDeadline, func() (bool, error) {
		ok, err := d.exists(task, idPurgeMe)
		return !ok, err
	})
}

// leaveTombstone deletes a task after the sweep, so it is a tombstone at the
// stop, and deletes a trigger, whose delivery rows the ledger forgets.
func (d *driver) leaveTombstone() error {
	task := d.kind("tasks/task")
	if err := d.put(task, idStaysDeleted, map[string]any{"properties": map[string]any{
		"name": "Stays deleted", "description": "a tombstone the sweep has not reached",
	}}); err != nil {
		return err
	}
	if err := d.del(task, idStaysDeleted); err != nil {
		return err
	}
	return d.del(core+"/trigger", "hourly")
}

// capture lists every record, live and tombstoned, reads each one, and
// writes them one to a line, sorted by kind and id, with the changelog head
// they were read at; then each trigger's status and parked failures.
func (d *driver) capture(out string) error {
	if err := d.quiet(); err != nil {
		return err
	}
	type listed struct {
		Kind, ID string
		Row      json.RawMessage
	}
	var refs []listed
	var head int64
	for _, deleted := range []bool{false, true} {
		filter := "{}"
		if deleted {
			filter = `{"deleted":true}`
		}
		after := ""
		for {
			q := url.Values{"filter": {filter}, "first": {"500"}, "withAnnotations": {"1"}}
			if after != "" {
				q.Set("after", after)
			}
			var page struct {
				Records []json.RawMessage `json:"records"`
				Cursor  string            `json:"cursor"`
				Head    int64             `json:"head"`
			}
			if err := d.must(http.MethodGet, "/api/v1/records?"+q.Encode(), nil, &page); err != nil {
				return err
			}
			if head == 0 {
				head = page.Head
			} else if page.Head != head {
				return fmt.Errorf("the head moved from %d to %d during the read: something still writes", head, page.Head)
			}
			for _, raw := range page.Records {
				var r struct{ ID, Kind string }
				if err := json.Unmarshal(raw, &r); err != nil {
					return err
				}
				refs = append(refs, listed{r.Kind, r.ID, raw})
			}
			if page.Cursor == "" {
				break
			}
			after = page.Cursor
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Kind != refs[j].Kind {
			return refs[i].Kind < refs[j].Kind
		}
		return refs[i].ID < refs[j].ID
	})
	// One line per record the list named: `read` is what a GET of its path
	// answered, and `listed` the list's row for it, kept only where the GET
	// answered another record (a merge's loser reads as its winner).
	var b bytes.Buffer
	fmt.Fprintf(&b, "{\"head\": %d, \"records\": [\n", head)
	for i, r := range refs {
		status, raw, err := d.do(http.MethodGet, recordPath(r.Kind, r.ID), nil, nil)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("GET %s/%s: %d %s", r.Kind, r.ID, status, raw)
		}
		var read struct{ ID string }
		if err := json.Unmarshal(raw, &read); err != nil {
			return err
		}
		line := map[string]json.RawMessage{"path": jsonString(r.Kind + "/" + r.ID), "read": raw}
		if read.ID != r.ID {
			line["listed"] = r.Row
		}
		enc, err := json.Marshal(line)
		if err != nil {
			return err
		}
		if i > 0 {
			b.WriteString(",\n")
		}
		b.Write(enc)
	}
	b.WriteString("\n]}\n")
	if err := os.WriteFile(filepath.Join(out, "records.json"), b.Bytes(), 0o600); err != nil {
		return err
	}

	var statuses struct {
		Items []map[string]any `json:"items"`
	}
	if err := d.must(http.MethodGet, "/api/v1/"+core+"/trigger/status", nil, &statuses); err != nil {
		return err
	}
	parked := map[string]any{}
	for _, s := range statuses.Items {
		id, _ := s["id"].(string)
		var p struct {
			Items []map[string]any `json:"items"`
		}
		if err := d.must(http.MethodGet, "/api/v1/"+core+"/trigger/"+url.PathEscape(id)+"/parked", nil, &p); err != nil {
			return err
		}
		parked[id] = p.Items
	}
	return writeJSON(filepath.Join(out, "triggers.json"), map[string]any{"statuses": statuses.Items, "parked": parked})
}

// quiet waits until the changelog head holds still for three seconds: a
// delivery settling after the last step would otherwise land mid-read.
func (d *driver) quiet() error {
	var last int64
	stable := time.Now()
	return waitFor("the head to hold still", settle, func() (bool, error) {
		var page struct {
			Head int64 `json:"head"`
		}
		if err := d.must(http.MethodGet, "/api/v1/records?first=1", nil, &page); err != nil {
			return false, err
		}
		if page.Head != last {
			last, stable = page.Head, time.Now()
		}
		return time.Since(stable) >= 3*time.Second, nil
	})
}

func jsonString(s string) json.RawMessage {
	raw, _ := json.Marshal(s)
	return raw
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}
