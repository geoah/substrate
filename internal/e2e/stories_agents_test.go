package e2e

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	kickoffTranscript = `Sam Rivera: Welcome everyone, this is the onboarding revamp kickoff.
Priya Sharma: From our side, the pilot needs to feel effortless on day one.
Nour Haddad: I can draft the new welcome flow. I will have it ready by Friday.
Sam Rivera: Kai should profile the signup path before we redesign anything.
Speaker 3: One note from the hallway: the pilot timeline slides a week.
Sam Rivera: Someone should sync with Northwind about the pilot scope.
Sam Rivera: And given the pilot, the invite flow redesign is now urgent.`

	chitchatTranscript = `Rae Kim: Before we start, did anyone try the new place across the road?
Kai Tanaka: The queue was around the block. I gave up and had a sandwich.
Rae Kim: Fair. Nothing new on billing since Tuesday, so let us keep this short.
Kai Tanaka: Agreed, nothing from me either. See everyone next week.`
)

// caseStory02: attendee emails become people, deterministically. An import
// record lands the way a connector delivers it, a triggered python function
// resolves the emails, mints the stranger, filters the room address, and a
// re-delivery converges instead of duplicating.
func caseStory02(c *C) {
	r := c.r

	// The provider row first (the agents applied next reference it), then
	// the story authority, then the trigger wiring the resolver up.
	c.putRec(providerCollection, "storyllm", map[string]any{
		"label": "the e2e scripted stub", "wire": "openai",
		"baseURL": r.stub.url(), "apiKey": "story-key",
	})
	c.applyStoryVocabulary("storyllm")
	c.putTrigger("story-resolve-attendees", map[string]any{
		"enabled": true,
		"source": map[string]any{"record": map[string]any{
			"kinds": []string{storyPkg + "/eventimport"}, "ops": []string{"create"},
		}},
		"callable": "substrate.reamde.dev/core/function/" + storyPkg + "/resolveattendees",
	})

	before := c.countRecords(personCollection)

	// The kickoff event, as the importer left it: raw emails, a meeting-room
	// address among them, nothing pointing anywhere yet.
	c.putRec(importCollection, "imp-kickoff", map[string]any{
		"summary":        "Onboarding revamp kickoff",
		"at":             r.kickoffAt().Format(time.RFC3339),
		"endsAt":         r.kickoffAt().Add(45 * time.Minute).Format(time.RFC3339),
		"organizerEmail": "sam@acme.example",
		"attendeeEmails": []string{
			"sam@acme.example", "nour@acme.example", "priya@northwind.example",
			"jordan@northwind.example", "c_room7@resource.calendar.example",
		},
	})
	c.wake("story-resolve-attendees")
	c.waitFor("the resolver's event", func() bool {
		rec, err := c.quietGet(eventCollection, "ev-kickoff")
		return err == nil && len(refPaths(rec, "attendees")) > 0
	})

	ev := c.getRec(eventCollection, "ev-kickoff")
	wantAttendees := []string{
		recPath(personKind, "sam"), recPath(personKind, "nour"),
		recPath(personKind, "priya"), recPath(personKind, "person-jordan"),
	}
	c.requiref(sameSet(refPaths(ev, "attendees"), wantAttendees...),
		"ev-kickoff attendees: %v", refPaths(ev, "attendees"))
	c.requiref(sameSet(refPaths(ev, "organizer"), recPath(personKind, "sam")),
		"ev-kickoff organizer: %v", refPaths(ev, "organizer"))
	c.requiref(sameSet(refPaths(ev, "calendar"), recPath(calendarKind, "work")),
		"ev-kickoff calendar: %v", refPaths(ev, "calendar"))
	jordan := c.getRec(personCollection, "person-jordan")
	emails, _ := jordan.Properties["emails"].([]any)
	c.requiref(jordan.prop("name") == "Jordan" && sameSet(refPaths(jordan, "memberOf"), recPath(orgKind, "northwind")) &&
		len(emails) == 1 && emails[0] == "jordan@northwind.example",
		"the minted stranger: name %q, emails %v, memberOf %v", jordan.prop("name"), emails, refPaths(jordan, "memberOf"))
	after := c.countRecords(personCollection)
	c.requiref(after == before+1,
		"the resolver minted %d people, want exactly 1 (the room address mints nothing)", after-before)
	c.stepf("three emails resolved, `person-jordan` was minted under northwind by domain, and the room address `c_room7@resource.calendar.example` minted nothing")

	// Re-delivery converges: same event, same people, no duplicates.
	var reran struct {
		Ran int `json:"ran"`
	}
	status, raw := c.do(http.MethodPost, triggerCollection+"/story-resolve-attendees/run",
		map[string]any{"kind": storyPkg + "/eventimport", "id": "imp-kickoff"}, &reran)
	c.requiref(status == http.StatusOK && reran.Ran == 1, "the synthetic re-delivery answered %d, ran %d: %s", status, reran.Ran, raw)
	ev = c.getRec(eventCollection, "ev-kickoff")
	c.requiref(sameSet(refPaths(ev, "attendees"), wantAttendees...) &&
		c.countRecords(personCollection) == after,
		"re-delivery changed the graph: attendees %v, %d people", refPaths(ev, "attendees"), c.countRecords(personCollection))
	c.stepf("a second delivery of the same import converged: 4 attendees, %d people, nothing duplicated", after)

	// The writes belong to the function, in the changelog's own words.
	c.requireActor("ev-kickoff", "put", actorResolver)
	c.requireActor("person-jordan", "put", actorResolver)
	c.stepf("the changelog attributes the event and the minted person to `%s`", actorResolver)
}

// caseStory03: a transcript finds its meeting, or honestly does not. The
// matcher agent scores candidates through a function tool, attaches the
// winner, and writes its audit either way.
func caseStory03(c *C) {
	r := c.r
	c.putTrigger("story-match-transcript", map[string]any{
		"enabled": true,
		"source": map[string]any{"record": map[string]any{
			"kinds": []string{transcriptKind}, "ops": []string{"create"},
		}},
		"callable": "substrate.reamde.dev/core/agent/" + storyPkg + "/transcriptMatcher",
	})

	// The kickoff's transcript: title copied from the meeting (as recorders
	// do), ten minutes of clock skew, no meeting named yet.
	c.putRec(transcriptCollection, "tr-kickoff", map[string]any{
		"name": "Onboarding revamp kickoff", "source": "example-notetaker",
		"at":     r.kickoffAt().Add(10 * time.Minute).Format(time.RFC3339),
		"endsAt": r.kickoffAt().Add(40 * time.Minute).Format(time.RFC3339),
		"text":   kickoffTranscript,
	})
	c.wake("story-match-transcript")
	c.waitFor("the kickoff transcript's verdict", func() bool {
		_, err := c.quietGet(verdictCollection, "mv-tr-kickoff")
		return err == nil
	})

	tr := c.getRec(transcriptCollection, "tr-kickoff")
	c.requiref(sameSet(refPaths(tr, "meeting"), recPath(eventKind, "ev-kickoff")),
		"tr-kickoff meeting: %v (the billing sync was 30 minutes away and must lose)", refPaths(tr, "meeting"))
	c.requiref(sameSet(refPaths(tr, "speakers"),
		recPath(personKind, "sam"), recPath(personKind, "nour"), recPath(personKind, "priya")),
		"tr-kickoff speakers: %v (jordan attended silently and is named by nothing)", refPaths(tr, "speakers"))
	verdict := c.getRec(verdictCollection, "mv-tr-kickoff")
	score, _ := verdict.Properties["score"].(float64)
	c.requiref(verdict.prop("verdict") == "matched" && score > 0.4 && verdict.prop("reason") != "" &&
		sameSet(refPaths(verdict, "event"), recPath(eventKind, "ev-kickoff")) &&
		sameSet(refPaths(verdict, "transcript"), recPath(transcriptKind, "tr-kickoff")),
		"the kickoff verdict: %v -> event %v, transcript %v", verdict.Properties, refPaths(verdict, "event"), refPaths(verdict, "transcript"))
	c.stepf("the matcher picked `ev-kickoff` over `ev-billing-sync`, named the three people who spoke in `speakers`, and wrote its audit (`mv-tr-kickoff`)")

	// The orphan: nothing within 90 minutes, an alien title. It attaches to
	// nothing, and the audit says so.
	c.putRec(transcriptCollection, "tr-orphan", map[string]any{
		"name": "Quarterly planning notes", "source": "example-notetaker",
		"at":   r.kickoffAt().Add(-6 * time.Hour).Format(time.RFC3339),
		"text": "Speaker 1: A long walk through next quarter, unrelated to any meeting on the calendar.",
	})
	c.wake("story-match-transcript")
	c.waitFor("the orphan transcript's verdict", func() bool {
		_, err := c.quietGet(verdictCollection, "mv-tr-orphan")
		return err == nil
	})
	orphan := c.getRec(transcriptCollection, "tr-orphan")
	c.requiref(len(refPaths(orphan, "meeting")) == 0,
		"the orphan transcript attached to %v; unmatched must attach to nothing", refPaths(orphan, "meeting"))
	verdict = c.getRec(verdictCollection, "mv-tr-orphan")
	c.requiref(verdict.prop("verdict") == "unmatched" && verdict.prop("reason") != "",
		"the orphan verdict: %v", verdict.Properties)
	c.stepf("the orphan transcript attached to nothing, and `mv-tr-orphan` records why: %s", verdict.prop("reason"))

	c.requireActor("mv-tr-kickoff", "put", actorMatcher)
	c.stepf("the changelog attributes the matcher's writes to `%s`", actorMatcher)
}

// caseStory04: reflection turns a matched transcript into proposed work
// through the decision loop, and the arbiter (a different scripted model)
// rejects the one proposal without provenance.
func caseStory04(c *C) {
	c.putTrigger("story-extract-work", map[string]any{
		"enabled": true,
		"source": map[string]any{"record": map[string]any{
			"kinds": []string{transcriptKind}, "ops": []string{"update"},
			"when":     `record != null && "meeting" in record.properties && record.properties.meeting != ""`,
			"coalesce": true,
		}},
		"callable": "substrate.reamde.dev/core/agent/" + storyPkg + "/actionItemExtractor",
	})
	c.putTrigger("story-review-proposals", map[string]any{
		"enabled": true,
		"source": map[string]any{"record": map[string]any{
			"kinds": []string{"substrate.reamde.dev/core/recordpatchrequest"}, "ops": []string{"create"},
		}},
		"callable": "substrate.reamde.dev/core/agent/" + storyPkg + "/changeRequestReviewer",
	})

	// The matcher's `meeting` write predates this trigger, so the cursor
	// rewinds to the beginning; the guard and coalesce pick out exactly the
	// kickoff transcript's attachment. This is the agent-to-agent chain:
	// one agent's write is another trigger's delivery. The wake races the
	// server's own dispatch tick, so the assertions are on the settled
	// state, never on who delivered first.
	status, raw := c.do(http.MethodPost, triggerCollection+"/story-extract-work/replay", map[string]any{"from": 0}, nil)
	c.requiref(status == http.StatusOK, "replaying story-reflect answered %d: %s", status, raw)
	c.wake("story-extract-work")
	c.waitFor("reflection's 5 proposals", func() bool { return c.quietCount(requestCollection) == 5 })

	var requests struct {
		Records []record `json:"records"`
	}
	status, raw = c.do(http.MethodGet, requestCollection, nil, &requests)
	c.requiref(status == http.StatusOK, "listing recordpatchrequests answered %d: %s", status, raw)
	c.requiref(len(requests.Records) == 5, "reflection filed %d proposals, want 5: %s", len(requests.Records), raw)
	c.stepf("reflection proposed exactly 5 changes and wrote NOTHING directly")

	// The arbiter decides each one; the sourceless proposal dies.
	c.wake("story-review-proposals")
	c.waitFor("the arbiter's 5 decisions", func() bool {
		reqs, err := c.quietList(requestCollection)
		if err != nil || len(reqs) != 5 {
			return false
		}
		for _, req := range reqs {
			if req.prop("decidedAt") == "" {
				return false
			}
		}
		return true
	})
	status, raw = c.do(http.MethodGet, requestCollection, nil, &requests)
	c.requiref(status == http.StatusOK, "re-listing recordpatchrequests answered %d: %s", status, raw)
	accepted, rejected := 0, 0
	for _, req := range requests.Records {
		switch req.prop("decision") {
		case "accepted":
			accepted++
		case "rejected":
			rejected++
		}
	}
	c.requiref(accepted == 4 && rejected == 1, "decisions: %d accepted, %d rejected; want 4 and 1", accepted, rejected)
	c.stepf("the arbiter decided all 5: 4 accepted, 1 rejected, every decision stamped")

	// The accepted work landed, with its shape intact.
	welcome := c.getRec(tasksCollection, "task-welcome-flow")
	c.requiref(welcome.prop("status") == "open" && welcome.prop("dueAt") != "" &&
		sameSet(refPaths(welcome, "assignee"), recPath(personKind, "nour")) &&
		sameSet(refPaths(welcome, "project"), recPath(projectKind, "onboarding-revamp")) &&
		sameSet(refPaths(welcome, "source"), recPath(transcriptKind, "tr-kickoff")),
		"task-welcome-flow landed wrong: %v", welcome.Properties)
	profile := c.getRec(tasksCollection, "task-profile-signup")
	c.requiref(profile.prop("status") == "open" &&
		sameSet(refPaths(profile, "assignee"), recPath(personKind, "kai")) &&
		sameSet(refPaths(profile, "project"), recPath(projectKind, "onboarding-revamp")) &&
		sameSet(refPaths(profile, "source"), recPath(transcriptKind, "tr-kickoff")),
		"task-profile-signup landed wrong: %v", profile.Properties)
	pilot := c.getRec(tasksCollection, "task-northwind-pilot")
	c.requiref(pilot.prop("status") == "proposed" && len(refPaths(pilot, "assignee")) == 0,
		"the unnamed action item must wait `proposed` and unassigned: %v %v", pilot.prop("status"), refPaths(pilot, "assignee"))
	invite := c.getRec(tasksCollection, "task-invite-flow")
	c.requiref(invite.prop("priority") == "urgent", "the accepted priority patch did not land: %q", invite.prop("priority"))
	status, _ = c.do(http.MethodGet, tasksCollection+"/task-sourceless", nil, nil)
	c.requiref(status == http.StatusNotFound, "the sourceless task exists (answered %d); the rejection must leave nothing behind", status)
	c.stepf("2 sourced tasks landed assigned, the unnamed one waits `proposed`, the priority patch applied, and the sourceless proposal left no record behind")

	// The references naming the transcript under `source` are exactly the
	// three sourced tasks.
	var incoming struct {
		Incoming []struct {
			Property string `json:"property"`
			From     struct {
				ID string `json:"id"`
			} `json:"from"`
		} `json:"incoming"`
	}
	status, _ = c.do(http.MethodGet, transcriptCollection+"/tr-kickoff/incoming?property=source", nil, &incoming)
	c.requiref(status == http.StatusOK, "incoming on tr-kickoff answered %d", status)
	fromIDs := make([]string, 0, len(incoming.Incoming))
	for _, in := range incoming.Incoming {
		fromIDs = append(fromIDs, in.From.ID)
	}
	c.requiref(sameSet(fromIDs, "task-welcome-flow", "task-profile-signup", "task-northwind-pilot"),
		"the tasks whose `source` names tr-kickoff: %v", fromIDs)

	// Writer and decider are distinct actors on EVERY request: one pass over
	// the feed, checked per request, so a self-graded decision anywhere fails.
	byRecord := map[string]map[string]string{}
	for _, row := range c.readChangesForward(0) {
		if row.Kind != "substrate.reamde.dev/core/recordpatchrequest" {
			continue
		}
		if byRecord[row.RecordID] == nil {
			byRecord[row.RecordID] = map[string]string{}
		}
		byRecord[row.RecordID][row.Op] = row.Actor
	}
	for _, req := range requests.Records {
		ops := byRecord[req.ID]
		c.requiref(ops["put"] == actorReflection && ops["patch"] == actorArbiter,
			"request %s: written by %q, decided by %q; the writer must never grade itself", req.ID, ops["put"], ops["patch"])
	}
	c.stepf("on all 5 requests the changelog shows writer `%s` and decider `%s`: distinct actors, distinct models", actorReflection, actorArbiter)
}

// caseStory05: the quiet window. A transcript with nothing in it flows
// through the whole chain, and the assertion is silence.
func caseStory05(c *C) {
	r := c.r
	requestsBefore := c.countRecords(requestCollection)
	tasksBefore := c.countRecords(tasksCollection)
	turnsBefore := r.stub.count("actionItemExtractor")
	runsBefore := c.quietRuns("story-extract-work")

	c.putRec(transcriptCollection, "tr-chitchat", map[string]any{
		"name": "Billing migration sync", "source": "example-notetaker",
		"at":   r.kickoffAt().Add(35 * time.Minute).Format(time.RFC3339),
		"text": chitchatTranscript,
	})
	c.wake("story-match-transcript")
	c.waitFor("the chitchat transcript's meeting", func() bool {
		rec, err := c.quietGet(transcriptCollection, "tr-chitchat")
		return err == nil && sameSet(refPaths(rec, "meeting"), recPath(eventKind, "ev-billing-sync"))
	})
	c.stepf("the matcher attached the chitchat transcript to `ev-billing-sync`; the chain now hands it to reflection")

	c.wake("story-extract-work")
	c.waitFor("reflection's quiet run to settle", func() bool { return c.quietRuns("story-extract-work") > runsBefore })

	c.requiref(r.stub.count("actionItemExtractor") > turnsBefore,
		"reflection never consulted its model; the silence must be the model's answer, not a skipped run")
	c.requiref(c.countRecords(requestCollection) == requestsBefore,
		"the quiet meeting grew the proposals from %d to %d", requestsBefore, c.countRecords(requestCollection))
	c.requiref(c.countRecords(tasksCollection) == tasksBefore,
		"the quiet meeting grew the tasks from %d to %d", tasksBefore, c.countRecords(tasksCollection))
	c.stepf("reflection ran (its model answered), proposed nothing, and the repository gained no work: silence asserted, not assumed")
}

// caseStory06: the world holds together. Attribution over the whole
// changelog, the signed chain verified, and the fold rebuilt to an
// identical graph.
func caseStory06(c *C) {
	r := c.r

	rows := c.readChangesForward(0)
	storyActors := map[string]int{}
	for _, row := range rows {
		// `substrate` may write only its own core records (registration,
		// tokens, runs, threads): a task or a story record written as the
		// system actor would be laundered attribution.
		ok := row.Actor == "api" || strings.HasPrefix(row.Actor, "bundle:") ||
			(row.Actor == "substrate" && strings.HasPrefix(row.Kind, "substrate.reamde.dev/core/"))
		switch row.Actor {
		case actorResolver, actorMatcher, actorReflection, actorArbiter:
			ok = true
			storyActors[row.Actor]++
		}
		c.requiref(ok, "changelog row %d (%s %s) carries an unaccounted actor %q", row.Seq, row.Op, row.Kind, row.Actor)
	}
	for _, actor := range []string{actorResolver, actorMatcher, actorReflection, actorArbiter} {
		c.requiref(storyActors[actor] > 0, "no changelog row is attributed to %s", actor)
	}
	c.stepf("all %d changelog rows are attributed to the owner, the bundles, or the four story callables (%v)", len(rows), storyActors)

	join := c.graphJoin()
	c.stepf("the GraphQL join over the story graph answers %d bytes", len(join))

	ctl, dsn := ctlEnv()
	if ctl == "" || dsn == "" {
		c.stepf("SKIPPED the verify and rebuild: %s and %s are not both set", envCtl, envDSN)
		return
	}
	out, err := ctlRun(ctl, dsn, "repository", "verify", r.username)
	c.requiref(err == nil, "repository verify: %v: %s", err, out)
	c.requiref(strings.Contains(out, r.rep.SigningPublicKey), "the chain verifies under a different key than /register returned")
	c.stepf("operator verify: %s", verifySummary(out))

	out, err = ctlRun(ctl, dsn, "repository", "rebuild", r.username)
	c.requiref(err == nil, "repository rebuild: %v: %s", err, out)
	rebuilt := c.graphJoin()
	c.requiref(string(join) == string(rebuilt),
		"the rebuilt fold answers a different graph:\nbefore: %s\nafter:  %s", join, rebuilt)
	c.stepf("repository rebuild refolded the changelog and the same GraphQL join answered byte-identically")
}

// graphJoin is the one fixed read STORY-06 compares across a rebuild: the
// whole story graph, every kind the stories touched. References ride in
// `properties` like every other value, so there is no second selection to
// ask for.
func (c *C) graphJoin() []byte {
	c.t.Helper()
	kinds := []string{
		taskKind, projectKind, personKind, teamKind, orgKind,
		eventKind, transcriptKind, storyPkg + "/matchverdict",
		"substrate.reamde.dev/core/recordpatchrequest",
	}
	query := `{ records(filter: {kinds: ["` + strings.Join(kinds, `", "`) + `"]}, orderBy: [{property: "createdAt"}], first: 200) {
		nodes { id kind properties } } }`
	status, raw := c.do(http.MethodPost, "/api/v1/graphql", map[string]any{"query": query}, nil)
	c.requiref(status == http.StatusOK && !strings.Contains(string(raw), `"errors"`),
		"the GraphQL join answered %d: %s", status, raw)
	return raw
}

// countRecords is the size of one collection's live list.
func (c *C) countRecords(collection string) int {
	c.t.Helper()
	var page struct {
		Records []record `json:"records"`
	}
	status, raw := c.do(http.MethodGet, collection+"?first=200", nil, &page)
	c.requiref(status == http.StatusOK, "counting %s answered %d: %s", collection, status, raw)
	return len(page.Records)
}

// rowsFor narrows the feed to one record's history.
func (c *C) rowsFor(recordID string) []changeRow {
	c.t.Helper()
	var out []changeRow
	for _, row := range c.readChangesForward(0) {
		if row.RecordID == recordID {
			out = append(out, row)
		}
	}
	return out
}

// requireActor asserts who the changelog says made a record's op.
func (c *C) requireActor(recordID, op, actor string) {
	c.t.Helper()
	for _, row := range c.rowsFor(recordID) {
		if row.Op == op {
			c.requiref(row.Actor == actor, "the %s of %s is attributed to %q, want %q", op, recordID, row.Actor, actor)
			return
		}
	}
	c.requiref(false, "no %s of %s is in the changelog", op, recordID)
}

func ctlEnv() (ctl, dsn string) {
	return os.Getenv(envCtl), os.Getenv(envDSN)
}

// ctlRun wears the operator hat. The credential key rides along when the
// runner has it (rebuild refuses a repository whose sealed secrets it
// cannot open; verify only warns).
func ctlRun(ctl, dsn string, args ...string) (string, error) {
	full := append([]string{"--dsn", dsn}, args...)
	cmd := exec.Command(ctl, full...)
	if key := os.Getenv("SUBSTRATE_E2E_CREDENTIAL_KEY"); key != "" {
		cmd.Env = append(os.Environ(), "SUBSTRATE_CREDENTIAL_KEY="+key)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// waitFor polls a settled-state condition. The server's own dispatch tick
// races every wake, so the stories await outcomes rather than counting who
// delivered.
func (c *C) waitFor(what string, cond func() bool) {
	c.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			c.stepf("settled: %s", what)
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	c.requiref(false, "timed out waiting for %s", what)
}

// The quiet reads: no step recording, for use inside waitFor conditions.
// quietList follows the cursor to exhaustion, so a count over a grown
// collection is never silently one page of it.
func (c *C) quietList(collection string) ([]record, error) {
	var all []record
	after := ""
	for {
		var page struct {
			Records []record `json:"records"`
			Cursor  string   `json:"cursor"`
		}
		path := collection + "?first=200"
		if after != "" {
			path += "&after=" + url.QueryEscape(after)
		}
		if err := c.r.fetch(path, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Records...)
		if page.Cursor == "" {
			return all, nil
		}
		after = page.Cursor
	}
}

func (c *C) quietCount(collection string) int {
	recs, err := c.quietList(collection)
	if err != nil {
		return -1
	}
	return len(recs)
}

func (c *C) quietGet(collection, id string) (record, error) {
	var rec record
	err := c.r.fetch(collection+"/"+url.PathEscape(id), &rec)
	return rec, err
}

// quietRuns counts one trigger's OK run rows. Parked and skipped runs do
// not count: a delivery that died mid-loop must never satisfy a wait.
func (c *C) quietRuns(trigger string) int {
	recs, err := c.quietList(runCollection)
	if err != nil {
		return -1
	}
	n := 0
	for _, rec := range recs {
		tr := refPath(rec.Properties["trigger"])
		if strings.HasSuffix(tr, "/"+trigger) && rec.prop("status") == "ok" {
			n++
		}
	}
	return n
}

// --- the scripted models -----------------------------------------------

// matcherResponder drives the matcher agent: score, decide, attach, audit.
// There are no link verbs to reach for: attaching the transcript to its
// meeting and its speakers is one patch of the transcript's own reference
// properties, through the same mutate tool the audit goes through.
func matcherResponder(llmReq llmReq) llmTurn {
	rec := llmReq.deliveredRecord()
	tid, _ := rec["id"].(string)
	attach := func(event string, speakers []string) llmCall {
		refs := make([]string, 0, len(speakers))
		for _, s := range speakers {
			refs = append(refs, recPath(personKind, s))
		}
		return llmCall{"mutate", map[string]any{
			"query": fmt.Sprintf(
				"mutation($id: ID!, $in: JSON!) { patch(kind: %q, id: $id, input: $in) { id } }", transcriptKind),
			"variables": map[string]any{
				"id": tid,
				"in": map[string]any{"properties": map[string]any{
					"meeting":  recPath(eventKind, event),
					"speakers": refs,
				}},
			},
		}}
	}
	verdict := func(v string, score float64, reason, event string) llmCall {
		props := map[string]any{
			"verdict": v, "score": score, "reason": reason,
			"transcript": recPath(transcriptKind, tid),
		}
		if event != "" {
			props["event"] = recPath(eventKind, event)
		}
		return llmCall{"mutate", map[string]any{
			"query": "mutation($in: JSON!) { put(input: $in) { id } }",
			"variables": map[string]any{"in": map[string]any{
				"kind": storyPkg + "/matchverdict", "id": "mv-" + tid,
				"properties": props,
			}},
		}}
	}
	// The winner comes out of the TOOL'S answer, never out of this script:
	// a scorer that returns nothing, or the wrong order, changes what the
	// agent links, and the case's assertions catch it.
	winner := func() (string, float64, bool) {
		result := llmReq.lastToolResult("scorecandidates")
		out, _ := result["output"].(map[string]any)
		floor, _ := out["floor"].(float64)
		candidates, _ := out["candidates"].([]any)
		if len(candidates) == 0 {
			return "", 0, false
		}
		best, _ := candidates[0].(map[string]any)
		event, _ := best["event"].(string)
		score, _ := best["score"].(float64)
		return event, score, score >= floor
	}
	// Who spoke is the scripted judgment: the model "reads" the text.
	speakers := map[string][]string{
		"tr-kickoff":  {"sam", "nour", "priya"},
		"tr-chitchat": {"rae", "kai"},
	}
	switch llmReq.assistantTurns() {
	case 0:
		return llmTurn{calls: []llmCall{{"scorecandidates", map[string]any{"transcript": tid}}}}
	case 1:
		event, _, matched := winner()
		if !matched {
			return llmTurn{calls: []llmCall{verdict("unmatched", 0,
				"no event within 90 minutes scored above the 0.4 floor", "")}}
		}
		return llmTurn{calls: []llmCall{attach(event, speakers[tid])}}
	case 2:
		event, score, matched := winner()
		if !matched {
			return llmTurn{content: "Unmatched: attached to nothing, said why."}
		}
		return llmTurn{calls: []llmCall{verdict("matched", score,
			fmt.Sprintf("%s won on time and title against every other candidate", event), event)}}
	default:
		return llmTurn{content: "Settled."}
	}
}

// reflectionResponder proposes the kickoff's work and stays silent on the
// quiet meeting. It writes nothing itself: every change is a propose call.
func reflectionResponder(r *run) func(llmReq) llmTurn {
	return func(req llmReq) llmTurn {
		rec := req.deliveredRecord()
		if tid, _ := rec["id"].(string); tid != "tr-kickoff" {
			return llmTurn{content: "Nothing was decided and nobody committed to anything. Proposing nothing."}
		}
		// Provenance and placement are properties of the proposed task, so a
		// proposal's whole diff is one `properties` map.
		source := recPath(transcriptKind, "tr-kickoff")
		onboarding := recPath(projectKind, "onboarding-revamp")
		proposals := []llmCall{
			{"propose", map[string]any{
				"op": "create", "kind": taskKind, "id": "task-welcome-flow",
				"diff": map[string]any{
					"properties": map[string]any{
						"name":     "Draft the new welcome flow",
						"dueAt":    r.kickoffAt().Add(96 * time.Hour).Format(time.RFC3339),
						"source":   source,
						"project":  onboarding,
						"assignee": recPath(personKind, "nour"),
					},
				},
				"rationale": "Nour committed to it in the kickoff, due Friday.",
			}},
			{"propose", map[string]any{
				"op": "create", "kind": taskKind, "id": "task-profile-signup",
				"diff": map[string]any{
					"properties": map[string]any{
						"name":     "Profile the signup path",
						"source":   source,
						"project":  onboarding,
						"assignee": recPath(personKind, "kai"),
					},
				},
				"rationale": "Sam asked Kai to profile before any redesign.",
			}},
			{"propose", map[string]any{
				"op": "create", "kind": taskKind, "id": "task-northwind-pilot",
				"diff": map[string]any{
					"properties": map[string]any{
						"name":    "Sync with Northwind about the pilot scope",
						"status":  "proposed",
						"source":  source,
						"project": onboarding,
					},
				},
				"rationale": "The meeting named the work but not the person; it waits for the owner.",
			}},
			{"propose", map[string]any{
				"op": "create", "kind": taskKind, "id": "task-sourceless",
				"diff": map[string]any{
					"properties": map[string]any{
						"name":     "A hunch with no evidence behind it",
						"project":  onboarding,
						"assignee": recPath(personKind, "sam"),
					},
				},
				"rationale": "No source cited; the arbiter should refuse this.",
			}},
			{"propose", map[string]any{
				"op": "patch", "kind": taskKind, "target": "task-invite-flow",
				"diff":      map[string]any{"properties": map[string]any{"priority": "urgent"}},
				"rationale": "The kickoff made the invite flow urgent, per the transcript.",
			}},
		}
		if turn := req.assistantTurns(); turn < len(proposals) {
			return llmTurn{calls: []llmCall{proposals[turn]}}
		}
		return llmTurn{content: "Proposed the meeting's work; the owner and the arbiter decide."}
	}
}

// arbiterResponder decides one request per delivery: a create whose diff
// names no `source` is rejected, everything else is accepted.
func arbiterResponder(req llmReq) llmTurn {
	if req.assistantTurns() > 0 {
		return llmTurn{content: "Decided."}
	}
	rec := req.deliveredRecord()
	id, _ := rec["id"].(string)
	props, _ := rec["properties"].(map[string]any)
	decision := "accepted"
	if op, _ := props["op"].(string); op == "create" && !diffCarriesSource(props["diff"]) {
		decision = "rejected"
	}
	return llmTurn{calls: []llmCall{{"mutate", map[string]any{
		"query": `mutation($id: ID!, $in: JSON!) { patch(kind: "substrate.reamde.dev/core/recordpatchrequest", id: $id, input: $in) { id } }`,
		"variables": map[string]any{
			"id": id,
			"in": map[string]any{"properties": map[string]any{"decision": decision}},
		},
	}}}}
}

func diffCarriesSource(diff any) bool {
	m, _ := diff.(map[string]any)
	props, _ := m["properties"].(map[string]any)
	src, _ := props["source"].(string)
	return src != ""
}
