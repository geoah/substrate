package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
)

const (
	tasksBundleID   = "samples.substrate.reamde.dev/tasks"
	tasksCollection = "/api/v1/samples.substrate.reamde.dev/tasks/task"
)

func TestE2E(t *testing.T) {
	base := os.Getenv(envServer)
	if base == "" {
		t.Skipf("%s is not set; this suite drives a live substrate (mise run test:e2e)", envServer)
	}
	r := newRun(t, base)
	defer func() {
		r.appendix()
		path, err := r.writeReport()
		if err != nil {
			t.Errorf("writing the report: %v", err)
			return
		}
		t.Logf("report: %s", path)
		t.Logf("repository: user %s on %s (left in place; mise run dev:wipe removes it)", r.username, r.base)
	}()

	r.runCase("AUTH-01", "Registration and the token door",
		"The invite code admits a registration; the minted token authenticates; a taken username refuses; "+
			"login mints a second token; a revoked token stops working while the others survive.",
		caseAuth)
	r.runCase("REC-01", "Bundle install and the record lifecycle",
		"The catalog refuses an out-of-order install naming what is missing, then installs the tasks "+
			"vocabulary and its requirements; a record is created, read, merged by put, moved through a state "+
			"transition that stamps completedAt, and tombstoned by delete; an unknown body key is refused.",
		caseRecords)
	r.runCase("LOG-01", "The changelog is the truth",
		"Every write is a hashed changelog row; a live watch delivers a write as it lands; a resume from a "+
			"seq replays exactly the rows after it; the operator's verify walks every hash and signature.",
		caseChangelog)
	r.runCase("STORY-01", "The graph exists",
		"The owner describes their world once (organizations, teams, people, projects, tasks, a calendar) "+
			"and every reference is navigable from both ends; a mistyped target, a dangling one and an "+
			"undeclared link property are refused.",
		caseStory01)

	// The automation stories: the model is a scripted stub the test hosts,
	// the substrate is the real server. The responders are registered up
	// front; each story wires its own triggers.
	r.stub = newLLMStub()
	defer r.stub.close()
	r.stub.respond("transcriptMatcher", matcherResponder)
	r.stub.respond("actionItemExtractor", reflectionResponder(r))
	r.stub.respond("changeRequestReviewer", arbiterResponder)

	r.runCase("STORY-02", "Attendee emails become people, deterministically",
		"An imported event's raw emails resolve through a triggered python function: known people link, "+
			"the stranger is minted under the right organization, the meeting-room address mints nothing, "+
			"and a re-delivery converges instead of duplicating.",
		caseStory02)
	r.runCase("STORY-03", "A transcript finds its meeting, or honestly does not",
		"The matcher agent scores candidates through a function tool, links the winner and the people who "+
			"actually spoke, writes its audit record, and attaches an unmatchable transcript to nothing.",
		caseStory03)
	r.runCase("STORY-04", "Reflection: what was said becomes work, with provenance",
		"A trigger on the matcher's own write chains into the reflection agent, which only proposes; the "+
			"arbiter (a different scripted model) accepts sourced work, leaves the unnamed item waiting, "+
			"applies the priority patch, and rejects the sourceless proposal so it leaves nothing behind.",
		caseStory04)
	r.runCase("STORY-05", "The quiet window",
		"A transcript with nothing in it flows through the whole chain; reflection consults its model and "+
			"proposes nothing, and the repository gains no work. Absence is asserted, not assumed.",
		caseStory05)
	r.runCase("STORY-06", "The world holds together",
		"Every changelog row is attributed to the owner, a bundle, or one of the four story callables; the "+
			"signed chain verifies; a rebuild refolds the changelog into a byte-identical graph.",
		caseStory06)

	// Everything beyond the slice and the stories registers itself into the
	// extra-case table (extra_test.go) and runs here, over the repository the
	// stories left behind.
	r.runExtraCases()
}

// record is the wire read shape, narrowed to what the cases assert on. Every
// pointer this suite follows is a reference property, so Properties is the
// whole of the record's data.
type record struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Version    int64          `json:"version"`
	Properties map[string]any `json:"properties"`
	DeletedAt  string         `json:"deletedAt"`
}

func (r record) prop(name string) string {
	v, _ := r.Properties[name].(string)
	return v
}

// caseAuth walks the whole door: discovery, registration (through TOTP when
// the server enforces it), token use, a refused duplicate, login, and
// revocation.
func caseAuth(c *C) {
	r := c.r

	var health struct {
		Status string `json:"status"`
	}
	status, raw := c.doAs("", http.MethodGet, "/healthz", nil, &health)
	c.requiref(status == http.StatusOK, "healthz answered %d: %s", status, raw)
	c.requiref(health.Status == "ok", "healthz status is %q, want ok", health.Status)

	var disc struct {
		Registration struct {
			Open         bool `json:"open"`
			TOTPRequired bool `json:"totpRequired"`
		} `json:"registration"`
		Changelog *struct {
			Horizon int64 `json:"horizon"`
		} `json:"changelog"`
	}
	status, raw = c.doAs("", http.MethodGet, "/.well-known/substrate/server.json", nil, &disc)
	c.requiref(status == http.StatusOK, "discovery answered %d: %s", status, raw)
	c.requiref(disc.Registration.Open, "registration is closed on this server; the suite needs an invite code")
	c.requiref(disc.Changelog != nil, "discovery publishes no changelog horizon")
	c.stepf("discovery: registration open, totpRequired=%t, changelog horizon %d", disc.Registration.TOTPRequired, disc.Changelog.Horizon)

	// Register. With the factor enforced the suite enrolls a seed and proves
	// it with a live code, exactly as an authenticator would.
	reg := map[string]any{"inviteCode": r.invite, "username": r.username, "password": r.password}
	if disc.Registration.TOTPRequired {
		var enr struct {
			TOTPSecret string `json:"totpSecret"`
		}
		c.paceAuth()
		status, raw = c.doAs("", http.MethodPost, "/register/enroll",
			map[string]string{"inviteCode": r.invite, "username": r.username}, &enr)
		c.requiref(status == http.StatusOK, "register/enroll answered %d: %s", status, raw)
		step := engine.TOTPStep(time.Now())
		code, err := engine.TOTPCode(enr.TOTPSecret, step)
		c.requiref(err == nil, "computing a TOTP code: %v", err)
		r.lastStep = step
		reg["totpSecret"], reg["totpCode"] = enr.TOTPSecret, code
		r.totpSecret = enr.TOTPSecret
		// Without the seed the reviewer cannot sign in to the repository the
		// run leaves behind, which is the whole point of leaving it.
		r.rep.TOTPSecret = enr.TOTPSecret
	}
	var regOut struct {
		Token struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"token"`
		Secret      string `json:"secret"`
		RecoveryKey string `json:"recoveryKey"`
	}
	c.paceAuth()
	// The register, login and mint failure messages carry no body: a 2xx of
	// the wrong shape would otherwise write the secret into the report.
	status, raw = c.doAs("", http.MethodPost, "/register", reg, &regOut)
	c.requiref(status == http.StatusCreated, "register answered %d, want 201%s", status, redacted(status, raw))
	c.requiref(strings.HasPrefix(regOut.Secret, "substrate_tok_"), "token secret has the wrong shape")
	c.requiref(regOut.RecoveryKey != "", "register minted no recovery key (none was supplied)")
	r.token, r.tokenID = regOut.Secret, regOut.Token.ID
	r.rep.Username, r.rep.Password = r.username, r.password
	c.stepf("registered `%s`: repository created, first token `%s` minted, recovery key returned once (not kept)", r.username, regOut.Token.ID)

	// The minted token opens the repository; no token opens nothing.
	var toks struct {
		Items []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"items"`
	}
	status, raw = c.do(http.MethodGet, "/tokens", nil, &toks)
	c.requiref(status == http.StatusOK, "GET /tokens answered %d: %s", status, raw)
	c.requiref(len(toks.Items) == 1 && toks.Items[0].ID == r.tokenID, "token list should hold exactly the registration token: %s", raw)
	status, _ = c.doAs("", http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusUnauthorized, "GET /tokens without a bearer answered %d, want 401", status)

	// A taken username refuses; registration is one-shot per user.
	c.paceAuth()
	status, raw = c.doAs("", http.MethodPost, "/register", reg, nil)
	c.requiref(status == http.StatusUnprocessableEntity && strings.Contains(string(raw), "already exists"),
		"re-registering %q answered %d, want a 422 naming the taken username: %s", r.username, status, raw)
	c.stepf("a second registration of `%s` was refused: 422, %q", r.username, "already exists")

	// Login mints a second token that works.
	login := map[string]any{"username": r.username, "password": r.password, "label": "e2e-login"}
	if disc.Registration.TOTPRequired {
		login["totpCode"] = r.nextTOTPCode(c)
	}
	var loginOut struct {
		Token struct {
			ID string `json:"id"`
		} `json:"token"`
		Secret string `json:"secret"`
	}
	c.paceAuth()
	status, raw = c.doAs("", http.MethodPost, "/login", login, &loginOut)
	c.requiref(status == http.StatusCreated, "login answered %d, want 201%s", status, redacted(status, raw))
	status, _ = c.doAs(loginOut.Secret, http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusOK, "the login token answered %d on GET /tokens, want 200", status)
	c.stepf("login minted token `%s` and it authenticates", loginOut.Token.ID)

	// A wrong password is one indistinguishable 401.
	badLogin := map[string]any{"username": r.username, "password": "not-the-password"}
	c.paceAuth()
	status, _ = c.doAs("", http.MethodPost, "/login", badLogin, nil)
	c.requiref(status == http.StatusUnauthorized, "a wrong password answered %d, want 401", status)

	// Mint, revoke, and prove the revocation touched only its token.
	var minted struct {
		Token struct {
			ID string `json:"id"`
		} `json:"token"`
		Secret string `json:"secret"`
	}
	status, raw = c.do(http.MethodPost, "/tokens", map[string]string{"label": "e2e-revoke-me"}, &minted)
	c.requiref(status == http.StatusCreated, "minting a token answered %d, want 201%s", status, redacted(status, raw))
	status, _ = c.doAs(minted.Secret, http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusOK, "the minted token does not authenticate")
	status, _ = c.do(http.MethodDelete, "/tokens/"+url.PathEscape(minted.Token.ID), nil, nil)
	c.requiref(status == http.StatusOK, "revoking answered %d", status)
	status, _ = c.doAs(minted.Secret, http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusUnauthorized, "the revoked token still answered %d, want 401", status)
	status, _ = c.do(http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusOK, "the registration token stopped working after an unrelated revocation")
	status, _ = c.doAs(loginOut.Secret, http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusOK, "the login token stopped working after an unrelated revocation")
	c.stepf("revocation ended token `%s` alone; the registration and login tokens both survive", minted.Token.ID)
}

// caseRecords installs the tasks vocabulary from the catalog and walks one
// record through its whole life.
func caseRecords(c *C) {
	// Before the install the collection does not exist.
	status, raw := c.do(http.MethodGet, tasksCollection, nil, nil)
	c.requiref(status == http.StatusNotFound, "the task collection answered %d before any install, want 404: %s", status, raw)

	var cat struct {
		Items []struct {
			ID        string `json:"id"`
			Installed bool   `json:"installed"`
		} `json:"items"`
	}
	status, raw = c.do(http.MethodGet, "/api/v1/catalog", nil, &cat)
	c.requiref(status == http.StatusOK, "catalog answered %d: %s", status, raw)
	found := false
	for _, it := range cat.Items {
		if it.ID == tasksBundleID {
			found, c.cr.Steps = true, append(c.cr.Steps, fmt.Sprintf("the catalog lists `%s` (installed=%t)", it.ID, it.Installed))
			c.requiref(!it.Installed, "the tasks bundle is already installed on a fresh repository")
		}
	}
	c.requiref(found, "the catalog does not list %s", tasksBundleID)

	// Dependency order is enforced: tasks requires people and scheduling, and
	// installing it first is refused naming exactly what is missing.
	status, raw = c.do(http.MethodPost, "/api/v1/catalog/"+url.PathEscape(tasksBundleID)+"/install", nil, nil)
	c.requiref(status == http.StatusUnprocessableEntity &&
		strings.Contains(string(raw), "samples.substrate.reamde.dev/people") &&
		strings.Contains(string(raw), "samples.substrate.reamde.dev/scheduling"),
		"an out-of-order install answered %d, want a 422 naming the missing requirements: %s", status, raw)
	c.stepf("installing `%s` before its requirements was refused naming `people` and `scheduling`", tasksBundleID)

	type bundleStatus struct {
		Installed bool `json:"installed"`
		Kinds     int  `json:"kinds"`
	}
	var bundle bundleStatus
	for _, id := range []string{
		"samples.substrate.reamde.dev/people",
		"samples.substrate.reamde.dev/scheduling",
		tasksBundleID,
	} {
		bundle = bundleStatus{}
		status, raw = c.do(http.MethodPost, "/api/v1/catalog/"+url.PathEscape(id)+"/install", nil, &bundle)
		c.requiref(status == http.StatusOK, "installing %s answered %d: %s", id, status, raw)
		c.requiref(bundle.Installed, "the install of %s did not end installed: %s", id, raw)
	}
	c.stepf("installed `people`, `scheduling` and `%s` in requirement order; the last admitted %d kinds", tasksBundleID, bundle.Kinds)

	// Create: 201, version 1, the declared default and the initial state land.
	var created record
	status, raw = c.do(http.MethodPost, tasksCollection,
		map[string]any{"properties": map[string]any{"name": "Review the e2e report", "priority": "high"}}, &created)
	c.requiref(status == http.StatusCreated, "create answered %d, want 201: %s", status, raw)
	c.requiref(created.Version == 1, "a fresh record's version is %d, want 1", created.Version)
	c.requiref(created.prop("status") == "open", "a write naming no state got %q, want the initial `open`", created.prop("status"))
	c.stepf("created task `%s` (version 1, status `open`)", created.ID)

	// Read it back.
	var got record
	status, _ = c.do(http.MethodGet, tasksCollection+"/"+url.PathEscape(created.ID), nil, &got)
	c.requiref(status == http.StatusOK, "reading the record answered %d", status)
	c.requiref(got.prop("name") == "Review the e2e report", "the record read back the wrong name: %q", got.prop("name"))

	// Put merges: the new property lands, the old one survives, version moves.
	var merged record
	status, raw = c.do(http.MethodPut, tasksCollection+"/"+url.PathEscape(created.ID),
		map[string]any{"properties": map[string]any{"description": "Written by the live e2e suite."}}, &merged)
	c.requiref(status == http.StatusOK, "put answered %d, want 200 on an existing record: %s", status, raw)
	c.requiref(merged.Version == 2, "after one put the version is %d, want 2", merged.Version)
	c.requiref(merged.prop("name") == "Review the e2e report", "put pruned the name; apply must merge, never prune")
	c.requiref(merged.prop("description") != "", "put dropped the new property")
	c.stepf("put merged a description in (version 2); `name` survived, because apply never prunes")

	// A state patch is a transition, and this one stamps completedAt.
	var done record
	status, raw = c.do(http.MethodPatch, tasksCollection+"/"+url.PathEscape(created.ID),
		map[string]any{"properties": map[string]any{"status": "done"}}, &done)
	c.requiref(status == http.StatusOK, "the done transition answered %d: %s", status, raw)
	c.requiref(done.prop("status") == "done", "the record's status is %q after the transition", done.prop("status"))
	c.requiref(done.prop("completedAt") != "", "the open->done transition did not stamp completedAt")
	c.stepf("patched status to `done`; the transition stamped completedAt=%s by itself", done.prop("completedAt"))

	// Strict decoding: an unknown body key is refused naming it.
	status, raw = c.do(http.MethodPost, tasksCollection, map[string]any{"bogus": true}, nil)
	c.requiref(status == http.StatusBadRequest, "an unknown body key answered %d, want 400", status)
	c.requiref(strings.Contains(string(raw), "bogus"), "the refusal does not name the bogus key: %s", raw)
	c.stepf("a body with an unknown key `bogus` was refused naming it")

	// Delete is a tombstone: the record leaves the list and its GET is gone.
	var doomed record
	status, _ = c.do(http.MethodPost, tasksCollection,
		map[string]any{"properties": map[string]any{"name": "A task the suite deletes"}}, &doomed)
	c.requiref(status == http.StatusCreated, "creating the doomed record answered %d", status)
	var tombstone record
	status, raw = c.do(http.MethodDelete, tasksCollection+"/"+url.PathEscape(doomed.ID), nil, &tombstone)
	c.requiref(status == http.StatusOK, "delete answered %d: %s", status, raw)
	c.requiref(tombstone.DeletedAt != "", "the delete's answer carries no deletedAt")
	// A tombstone stays addressable: a direct GET answers it, deletedAt and
	// all, while the list below no longer holds it.
	var deleted record
	status, _ = c.do(http.MethodGet, tasksCollection+"/"+url.PathEscape(doomed.ID), nil, &deleted)
	c.requiref(status == http.StatusOK && deleted.DeletedAt != "",
		"a tombstoned record should still GET as a tombstone; answered %d with deletedAt %q", status, deleted.DeletedAt)
	var page struct {
		Records []record `json:"records"`
	}
	status, _ = c.do(http.MethodGet, tasksCollection, nil, &page)
	c.requiref(status == http.StatusOK, "listing tasks answered %d", status)
	c.requiref(len(page.Records) == 1 && page.Records[0].ID == created.ID,
		"the list should hold exactly the surviving task, got %d records", len(page.Records))
	c.stepf("deleted task `%s`: the tombstone carries deletedAt, still GETs by id, and the list holds only the survivor", doomed.ID)
}

// changeRow is one changelog line, narrowed to what the cases assert on.
type changeRow struct {
	Seq      int64  `json:"seq"`
	Op       string `json:"op"`
	Actor    string `json:"actor"`
	Kind     string `json:"kind"`
	RecordID string `json:"recordId"`
	Hash     string `json:"hash"`
}

// caseChangelog reads the feed forward, watches a live write land, resumes
// from a seq, and (when the operator hat is available) verifies the checksums.
func caseChangelog(c *C) {
	r := c.r

	rows := c.readChangesForward(0)
	c.requiref(len(rows) > 0, "the changelog is empty after registration and a bundle install")
	for i, row := range rows {
		// Strictly sequential means CONSECUTIVE from 1, not merely increasing:
		// a gap is a dropped write.
		c.requiref(row.Seq == int64(i)+1, "changelog seqs are not consecutive: seq %d at position %d", row.Seq, i)
		c.requiref(row.Hash != "", "changelog row %d carries no hash", row.Seq)
	}
	taskWrites := 0
	for _, row := range rows {
		if row.Kind == "samples.substrate.reamde.dev/tasks/task" {
			taskWrites++
		}
	}
	c.requiref(taskWrites >= 4, "the feed holds %d task rows; REC-01 wrote at least 4 (two creates, a put, a patch, a delete)", taskWrites)
	head := rows[len(rows)-1].Seq
	c.stepf("forward read: %d rows, consecutive from seq 1, every row hashed, REC-01's %d task writes present; head is seq %d", len(rows), taskWrites, head)

	// A live watch from head delivers the next write as it lands.
	probeID := "e2e-watch-probe"
	row := c.watchForWrite(head, probeID, func() {
		status, raw := c.do(http.MethodPut, tasksCollection+"/"+url.PathEscape(probeID),
			map[string]any{"properties": map[string]any{"name": "The watch probe"}}, nil)
		c.requiref(status == http.StatusCreated, "the probe write answered %d: %s", status, raw)
	})
	c.requiref(row.Op == "put", "the watched row's op is %q, want put", row.Op)
	c.requiref(row.Seq > head, "the watched row's seq %d is not after head %d", row.Seq, head)
	c.stepf("a watch open at seq %d delivered the probe write live: seq %d, op put, hash `%s`", head, row.Seq, short(row.Hash))

	// The probe is the repository's last write, so resuming from the seq
	// before it must replay exactly one row: the probe, nothing else.
	resumed := c.readChangesForward(row.Seq - 1)
	c.requiref(len(resumed) == 1 && resumed[0].Seq == row.Seq && resumed[0].RecordID == probeID,
		"resuming from seq %d replayed %d rows, want exactly the probe row", row.Seq-1, len(resumed))
	c.stepf("a forward read from seq %d replayed exactly the probe row: resume loses nothing and repeats nothing", row.Seq-1)

	// The operator hat walks the changelog: every seq, every checksum.
	ctl, dsn := os.Getenv(envCtl), os.Getenv(envDSN)
	if ctl == "" || dsn == "" {
		c.stepf("SKIPPED the checksum verify: %s and %s are not both set", envCtl, envDSN)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, ctl, "--dsn", dsn, "repository", "verify", r.username).CombinedOutput()
	c.requiref(err == nil, "substratectl repository verify %s: %v: %s", r.username, err, out)
	c.stepf("operator verify (`substratectl repository verify %s`): %s", r.username, verifySummary(string(out)))
}

// readChangesForward reads the forward feed from a seq (exclusive) to its
// end. One forward page holds at most 500 rows, so the read pages until a
// page comes back empty; a story run's changelog outgrows one page.
func (c *C) readChangesForward(from int64) []changeRow {
	c.t.Helper()
	var rows []changeRow
	for {
		path := fmt.Sprintf("/api/v1/changes?from=%d", from)
		status, raw := c.do(http.MethodGet, path, nil, nil)
		c.requiref(status == http.StatusOK, "GET %s answered %d: %s", path, status, raw)
		page := 0
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if line == "" {
				continue
			}
			var row changeRow
			c.requiref(json.Unmarshal([]byte(line), &row) == nil, "undecodable ndjson line: %s", line)
			if row.Seq == 0 {
				// A control frame: the bookmark or a heartbeat is fine, the
				// reserved terminal error frame is a failure, never a skip.
				c.requiref(!strings.Contains(line, `"error"`), "the feed ended with an error frame: %s", line)
				continue
			}
			rows = append(rows, row)
			page++
			from = row.Seq
		}
		if page == 0 {
			return rows
		}
	}
}

// watchForWrite opens a live watch at from, runs the write, and returns the
// row the stream delivers for recordID.
func (c *C) watchForWrite(from int64, recordID string, write func()) changeRow {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	u := fmt.Sprintf("%s/api/v1/changes?watch=1&from=%d", c.r.base, from)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	c.requiref(err == nil, "building the watch request: %v", err)
	req.Header.Set("Authorization", "Bearer "+c.r.token)
	// The stream outlives any sane client timeout, so it gets its own client.
	resp, err := (&http.Client{}).Do(req)
	c.requiref(err == nil, "opening the watch: %v", err)
	defer resp.Body.Close()
	c.stepf("`GET /api/v1/changes?watch=1&from=%d` answered %d and streams", from, resp.StatusCode)
	c.requiref(resp.StatusCode == http.StatusOK, "the watch answered %d, want 200", resp.StatusCode)

	write()

	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		var row changeRow
		c.requiref(json.Unmarshal(sc.Bytes(), &row) == nil, "undecodable watch line: %s", sc.Text())
		if row.Seq == 0 {
			c.requiref(!strings.Contains(sc.Text(), `"error"`), "the watch ended with an error frame: %s", sc.Text())
			continue // the bookmark or a heartbeat
		}
		if row.RecordID == recordID {
			return row
		}
	}
	c.requiref(false, "the watch ended without delivering the write of %s: %v", recordID, sc.Err())
	return changeRow{}
}

// nextTOTPCode computes a code on a step no earlier attempt consumed.
func (r *run) nextTOTPCode(c *C) string {
	step := engine.TOTPStep(time.Now())
	if step <= r.lastStep {
		step = r.lastStep + 1
	}
	code, err := engine.TOTPCode(r.totpSecret, step)
	c.requiref(err == nil, "computing a TOTP code: %v", err)
	r.lastStep = step
	return code
}

// redacted keeps a refusal's body in a failure message and drops a 2xx body,
// which for the credential endpoints would put a secret into the report.
func redacted(status int, raw []byte) string {
	if status >= 300 {
		return ": " + string(raw)
	}
	return " (body withheld: it carries a secret)"
}

// verifySummary keeps the verify verdict lines and drops the preamble.
func verifySummary(out string) string {
	var keep []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "entries:") || strings.HasPrefix(line, "verified in") {
			keep = append(keep, line)
		}
	}
	if len(keep) == 0 {
		return short(strings.TrimSpace(out))
	}
	return strings.Join(keep, "; ")
}

// short keeps hashes and one-line outputs readable in the report.
func short(s string) string {
	return prefix(s, 64)
}

func prefix(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// appendix reads the repository back as it was left and renders it into the
// report: the records a reviewer will find, and the changelog that is their
// truth.
func (r *run) appendix() {
	if r.token == "" {
		return
	}
	var b strings.Builder
	b.WriteString("## The repository, as left\n\n")

	// A failed read is said out loud: an appendix that silently omits a
	// section would read as "there was nothing there".
	var repo struct {
		Records []record `json:"records"`
	}
	if err := r.fetch("/api/v1/substrate.reamde.dev/core/repository", &repo); err != nil {
		fmt.Fprintf(&b, "Reading the repository record failed: %v\n\n", err)
	} else if len(repo.Records) == 1 {
		fmt.Fprintf(&b, "Repository record `%s`.\n\n", repo.Records[0].ID)
	}

	var toks struct {
		Items []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"items"`
	}
	if err := r.fetch("/tokens", &toks); err != nil {
		fmt.Fprintf(&b, "Reading the tokens failed: %v\n\n", err)
	} else {
		b.WriteString("### Tokens\n\n| id | label |\n| --- | --- |\n")
		for _, tk := range toks.Items {
			fmt.Fprintf(&b, "| `%s` | %s |\n", tk.ID, tk.Label)
		}
		b.WriteString("\n")
	}

	var tasks struct {
		Records []record `json:"records"`
	}
	if err := r.fetch(tasksCollection, &tasks); err != nil {
		fmt.Fprintf(&b, "Reading the tasks failed: %v\n\n", err)
	} else {
		b.WriteString("### Tasks (`samples.substrate.reamde.dev/tasks/task`)\n\n")
		b.WriteString("| id | name | status | version |\n| --- | --- | --- | --- |\n")
		for _, rec := range tasks.Records {
			fmt.Fprintf(&b, "| `%s` | %s | %s | %d |\n", rec.ID, rec.prop("name"), rec.prop("status"), rec.Version)
		}
		b.WriteString("\nA tombstoned task is not listed: a tombstone leaves the fold. Its life stays in the changelog below.\n\n")
	}

	b.WriteString("### The changelog\n\n| seq | op | kind | record | actor | hash |\n| --- | --- | --- | --- | --- | --- |\n")
	from := int64(0)
	for {
		status, raw, err := httpJSON(r.hc, r.base, r.token, http.MethodGet,
			fmt.Sprintf("/api/v1/changes?from=%d", from), nil)
		if err != nil || status != http.StatusOK {
			fmt.Fprintf(&b, "\nReading the changelog failed: status %d, %v\n", status, err)
			break
		}
		page := 0
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			var row changeRow
			if json.Unmarshal([]byte(line), &row) != nil || row.Seq == 0 {
				continue
			}
			fmt.Fprintf(&b, "| %d | %s | %s | `%s` | %s | `%s` |\n",
				row.Seq, row.Op, row.Kind, row.RecordID, row.Actor, prefix(row.Hash, 12))
			page++
			from = row.Seq
		}
		if page == 0 {
			break
		}
	}
	b.WriteString("\n")
	r.rep.Appendix = append(r.rep.Appendix, b.String())
}
