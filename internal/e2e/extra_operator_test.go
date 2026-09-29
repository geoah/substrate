package e2e

// The 900 block: the operator hat and the server's own restart. They run
// last because DUR-01 and OPR-03 stop the server under test and start it
// again, through the SUBSTRATE_E2E_STOP and SUBSTRATE_E2E_START hooks, and
// OPR-03 re-keys the run's own credential: the report prints the password
// and, on the enforced door, the seed the reset issued.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

func init() {
	registerCase(900, "OPR-02", "`repository list` names the run's repository",
		"The operator hat's `repository list -o json` reads the control-plane table beside the running "+
			"server and names the run's repository by its authority, created while this run was.",
		xoCaseRepositoryList)
	registerCase(910, "DUR-01", "A server restart loses nothing",
		"Stopping the server and starting it again leaves every record byte for byte as it was, the "+
			"history's head and generation unmoved, and the table head and the segment files' head both "+
			"at the same seq and checksum; the next write lands at head+1.",
		xoCaseRestart)
	registerCase(920, "OPR-03", "`user reset` re-keys the credential and leaves the tokens",
		"`user reset` is refused beside the running server, which holds the writer lock; with the server "+
			"stopped it writes a new password and TOTP seed, after which the old password is a 401, the new "+
			"one logs in, and the tokens minted before the reset still authenticate.",
		xoCaseUserReset)
}

// xoVerify is `repository verify -o json`, narrowed to the heads it holds.
type xoVerify struct {
	OK       bool     `json:"ok"`
	Entries  int64    `json:"entries"`
	Head     int64    `json:"head"`
	FileHead int64    `json:"fileHead"`
	HeadHash string   `json:"headHash"`
	Findings []string `json:"findings"`
}

func xoVerifyNow(c *C) xoVerify {
	c.t.Helper()
	ctl, dsn := ctlEnv()
	out, err := ctlRun(ctl, dsn, "repository", "verify", c.r.authority, "-o", "json")
	var v xoVerify
	c.requiref(json.Unmarshal([]byte(out), &v) == nil, "repository verify -o json printed no report (%v): %s", err, tail(out, 20))
	c.requiref(err == nil && v.OK, "repository verify does not pass: %v: findings %v", err, v.Findings)
	return v
}

// xoSnapshot is what DUR-01 compares across the restart: every live record as
// the wire serves it, the history's head and generation, and the operator's
// view of both changelog heads.
type xoSnapshot struct {
	records    map[string]string
	head       int64
	generation string
	verify     xoVerify
}

func xoTakeSnapshot(c *C) xoSnapshot {
	c.t.Helper()
	snap := xoSnapshot{records: map[string]string{}}
	after := ""
	for {
		path := recordsRoute + "?first=200"
		if after != "" {
			path += "&after=" + url.QueryEscape(after)
		}
		var page struct {
			Records []json.RawMessage `json:"records"`
			Cursor  string            `json:"cursor"`
		}
		c.requiref(c.r.fetch(path, &page) == nil, "listing every record failed at cursor %q", after)
		for _, raw := range page.Records {
			var id struct {
				ID   string `json:"id"`
				Kind string `json:"kind"`
			}
			c.requiref(json.Unmarshal(raw, &id) == nil && id.ID != "", "an undecodable record in the list: %s", raw)
			key := id.Kind + "/" + id.ID
			_, dup := snap.records[key]
			c.requiref(!dup, "the list served %s twice", key)
			snap.records[key] = string(raw)
		}
		if page.Cursor == "" {
			break
		}
		after = page.Cursor
	}
	snap.head, snap.generation = c.changelogHead()
	snap.verify = xoVerifyNow(c)
	return snap
}

// xoRedact drops the enrollment lines a reset prints: the seed is a secret
// and must not reach the report or the test log through a failure message.
func xoRedact(out string) string {
	var keep []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "secret:") || strings.Contains(line, "otpauth") {
			keep = append(keep, "  (enrollment line withheld)")
			continue
		}
		keep = append(keep, line)
	}
	return tail(strings.Join(keep, "\n"), 20)
}

// --- OPR-02 --------------------------------------------------------------

func xoCaseRepositoryList(c *C) {
	ctl, dsn := ctlEnv()
	if ctl == "" || dsn == "" {
		c.skipf("%s and %s are not both set, so there is no operator hat to list with", envCtl, envDSN)
		return
	}
	out, err := ctlRun(ctl, dsn, "repository", "list", "-o", "json")
	c.requiref(err == nil, "repository list -o json: %v: %s", err, tail(out, 20))
	var rows []struct {
		Authority string `json:"authority"`
		CreatedAt string `json:"createdAt"`
	}
	c.requiref(json.Unmarshal([]byte(out), &rows) == nil, "repository list -o json printed no list: %s", tail(out, 20))
	i := slices.IndexFunc(rows, func(row struct {
		Authority string `json:"authority"`
		CreatedAt string `json:"createdAt"`
	},
	) bool {
		return row.Authority == c.r.authority
	})
	c.requiref(i >= 0, "repository list names %d repositories and not `%s`", len(rows), c.r.authority)
	created, err := time.Parse(time.RFC3339, rows[i].CreatedAt)
	c.requiref(err == nil, "the row's createdAt %q is not RFC 3339: %v", rows[i].CreatedAt, err)
	// RFC 3339 here carries whole seconds, so the window is widened by one on
	// each side of the run.
	c.requiref(!created.Before(c.r.rep.Started.Add(-time.Second)) && !created.After(time.Now().Add(time.Second)),
		"the row says `%s` was created at %s, outside this run (started %s)", c.r.authority, created, c.r.rep.Started.UTC().Format(time.RFC3339))
	c.stepf("`repository list -o json` lists %d repositories, among them `%s` created at %s, during this run", len(rows), c.r.authority, rows[i].CreatedAt)

	out, err = ctlRun(ctl, dsn, "repository", "list")
	c.requiref(err == nil, "repository list: %v: %s", err, tail(out, 20))
	c.requiref(slices.ContainsFunc(strings.Split(out, "\n"), func(line string) bool {
		fields := strings.Fields(line)
		return len(fields) > 0 && fields[0] == c.r.authority
	}), "the table form of repository list has no row for `%s`", c.r.authority)
	c.stepf("the table form of `repository list` has a row opening with `%s`", c.r.authority)
}

// --- DUR-01 --------------------------------------------------------------

func xoCaseRestart(c *C) {
	if _, _, ok := serverHooks(); !ok {
		c.skipf("%s and %s are not both set, so the suite cannot restart the server it drives", envStop, envStart)
		return
	}
	if ctl, dsn := ctlEnv(); ctl == "" || dsn == "" {
		c.skipf("%s and %s are not both set, so there is no operator hat to read the segment files' head with", envCtl, envDSN)
		return
	}

	before := xoTakeSnapshot(c)
	c.requiref(before.verify.Head == before.head && before.verify.FileHead == before.head,
		"before the restart the heads disagree: the history says %d, the table %d, the files %d", before.head, before.verify.Head, before.verify.FileHead)
	c.stepf("before: %d live records, head %d in generation `%s`, table and files both at seq %d checksum `%s`",
		len(before.records), before.head, before.generation, before.verify.FileHead, short(before.verify.HeadHash))

	defer c.ensureServer()
	c.stopServer()
	c.startServer()

	after := xoTakeSnapshot(c)
	c.requiref(after.head == before.head && after.generation == before.generation,
		"the restart moved the history: head %d generation %q, was %d %q", after.head, after.generation, before.head, before.generation)
	c.requiref(after.verify.Head == before.verify.Head && after.verify.FileHead == before.verify.FileHead &&
		after.verify.Entries == before.verify.Entries && after.verify.HeadHash == before.verify.HeadHash,
		"the restart moved a changelog head: table %d files %d checksum %q entries %d, was %d %d %q %d",
		after.verify.Head, after.verify.FileHead, after.verify.HeadHash, after.verify.Entries,
		before.verify.Head, before.verify.FileHead, before.verify.HeadHash, before.verify.Entries)
	for key, raw := range before.records {
		got, ok := after.records[key]
		c.requiref(ok, "the restart lost %s", key)
		c.requiref(got == raw, "the restart changed %s:\nbefore: %s\nafter:  %s", key, raw, got)
	}
	for key := range after.records {
		_, ok := before.records[key]
		c.requiref(ok, "the restart added %s", key)
	}
	c.stepf("after: the same %d records byte for byte, head %d in the same generation, table and files at the same seq and checksum", len(after.records), after.head)

	// The writer resumed where it stopped: the next write is head+1, and it
	// is the only row after the old head.
	probe := "xo-after-restart"
	c.putRec(tasksCollection, probe, map[string]any{"name": "Written after the restart"})
	rows := c.readChangesForward(before.head)
	c.requiref(len(rows) == 1 && rows[0].Seq == before.head+1 && rows[0].RecordID == probe,
		"after the restart the feed past seq %d holds %d rows, want the probe alone at seq %d", before.head, len(rows), before.head+1)
	c.stepf("the first write after the restart landed at seq %d, the old head plus one", rows[0].Seq)
}

// --- OPR-03 --------------------------------------------------------------

func xoCaseUserReset(c *C) {
	r := c.r
	if _, _, ok := serverHooks(); !ok {
		c.skipf("%s and %s are not both set, and `user reset` needs the server stopped", envStop, envStart)
		return
	}
	ctl, dsn := ctlEnv()
	if ctl == "" || dsn == "" || os.Getenv(envCredKey) == "" {
		c.skipf("%s, %s and %s are not all set, and `user reset` writes sealed material under the credential key", envCtl, envDSN, envCredKey)
		return
	}
	_, totp := xaDoor(c)
	newPassword := "e2e-reset-" + strconv.FormatInt(time.Now().UnixNano(), 36)

	// Beside the running server the reset is refused: it appends to the
	// changelog, and the server holds the repository's writer lock.
	out, err := ctlRunInput(ctl, dsn, newPassword+"\n", "user", "reset", r.authority, "--password-stdin")
	c.requiref(err != nil && strings.Contains(out, "need it stopped first"),
		"user reset beside the running server: err=%v, want the writer refusal: %s", err, xoRedact(out))
	c.stepf("`user reset %s` beside the running server was refused: the server holds the writer lock", r.authority)

	defer c.ensureServer()
	c.stopServer()
	out, err = ctlRunInput(ctl, dsn, newPassword+"\n", "user", "reset", r.authority, "--password-stdin")
	c.requiref(err == nil, "user reset with the server stopped: %v: %s", err, xoRedact(out))
	seed := ""
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "secret:"); ok {
			seed = strings.TrimSpace(v)
		}
	}
	c.requiref(seed != "", "the reset printed no TOTP seed: %s", xoRedact(out))
	c.requiref(strings.Contains(out, "the user's tokens are untouched"), "the reset does not say what it did to the tokens: %s", xoRedact(out))
	// The credentials move the moment the reset lands, so the report prints
	// the ones that work even if the start below fails.
	oldPassword, oldSeed, oldLast := r.password, r.totpSecret, r.lastStep
	r.password, r.rep.Password = newPassword, newPassword
	r.totpSecret, r.lastStep = seed, 0
	if totp {
		r.rep.TOTPSecret = seed
	}
	c.stepf("with the server stopped, `user reset %s --password-stdin` wrote a new password and issued a new TOTP seed", r.authority)
	c.startServer()

	// On the enforced door each refused attempt carries a live code from the
	// seed that is NOT under test, so exactly one factor is wrong at a time.
	login := map[string]any{"repository": r.repository, "password": oldPassword, "label": "xo-reset"}
	var step int64
	if totp {
		step = c.totpStepAfter(r.lastStep)
		login["totpCode"] = c.totpCode(seed, step)
	}
	c.paceAuth()
	status, raw := c.doAs("", http.MethodPost, "/login", login, nil)
	xaRequireRefusal(c, "`/login` with the password from before the reset", status, raw, http.StatusUnauthorized, "auth")
	c.stepf("`/login` with the password from before the reset was refused 401")
	if totp && oldSeed != "" {
		login["password"] = newPassword
		login["totpCode"] = c.totpCode(oldSeed, c.totpStepAfter(oldLast))
		c.paceAuth()
		status, raw = c.doAs("", http.MethodPost, "/login", login, nil)
		xaRequireRefusal(c, "`/login` with a code from the seed before the reset", status, raw, http.StatusUnauthorized, "auth")
		c.stepf("`/login` with a live code from the seed before the reset was refused 401")
		login["totpCode"] = c.totpCode(seed, step)
	}

	login["password"] = newPassword
	var minted struct {
		Secret string `json:"secret"`
	}
	c.paceAuth()
	status, raw = c.doAs("", http.MethodPost, "/login", login, &minted)
	c.requiref(status == http.StatusCreated, "`/login` with the reset password answered %d, want 201%s", status, redacted(status, raw))
	if totp {
		r.lastStep = step
	}
	status, _ = c.doAs(minted.Secret, http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusOK, "the token minted after the reset answered %d, want 200", status)
	c.stepf("`/login` with the reset password minted a token that authenticates")

	status, _ = c.do(http.MethodGet, "/tokens", nil, nil)
	c.requiref(status == http.StatusOK, "the run's token, minted at registration, answered %d after the reset, want 200", status)
	c.stepf("the run's own token, minted before the reset, still authenticates: a reset re-keys the factors and leaves the tokens")
}
