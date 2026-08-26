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
	projectKind = "tasks.substrate.reamde.dev/project"

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
	}, nil)
	c.applyStoryVocabulary("storyllm")
	c.putTrigger("story-resolve-attendees", map[string]any{
		"enabled": true,
		"source": map[string]any{"record": map[string]any{
			"kinds": []string{storyAuthority + "/eventimport"}, "ops": []string{"create"},
		}},
		"callable": "core.substrate.reamde.dev/function/" + storyAuthority + "/resolveattendees",
	})

	before := c.countRecords(personCollection)

	// The kickoff event, as the importer left it: raw emails, a meeting-room
	// address among them, no edges anywhere.
	c.putRec(importCollection, "imp-kickoff", map[string]any{
		"summary":        "Onboarding revamp kickoff",
		"at":             r.kickoffAt().Format(time.RFC3339),
		"endsAt":         r.kickoffAt().Add(45 * time.Minute).Format(time.RFC3339),
		"organizerEmail": "sam@acme.example",
		"attendeeEmails": []string{
			"sam@acme.example", "nour@acme.example", "priya@northwind.example",
			"jordan@northwind.example", "c_room7@resource.calendar.example",
		},
	}, nil)
	c.wake("story-resolve-attendees")
	c.waitFor("the resolver's event", func() bool {
		rec, err := c.quietGet(eventCollection, "ev-kickoff")
		return err == nil && len(rec.Edges["attendees"]) > 0
	})

	ev := c.getRec(eventCollection, "ev-kickoff")
	c.requiref(sameIDs(edgeIDs(ev, "attendees"), "sam", "nour", "priya", "person-jordan"),
		"ev-kickoff attendees: %v", edgeIDs(ev, "attendees"))
	c.requiref(sameIDs(edgeIDs(ev, "organizer"), "sam"), "ev-kickoff organizer: %v", edgeIDs(ev, "organizer"))
	c.requiref(sameIDs(edgeIDs(ev, "calendar"), "work"), "ev-kickoff calendar: %v", edgeIDs(ev, "calendar"))
	jordan := c.getRec(personCollection, "person-jordan")
	emails, _ := jordan.Properties["emails"].([]any)
	c.requiref(jordan.prop("name") == "Jordan" && sameIDs(edgeIDs(jordan, "memberOf"), "northwind") &&
		len(emails) == 1 && emails[0] == "jordan@northwind.example",
		"the minted stranger: name %q, emails %v, memberOf %v", jordan.prop("name"), emails, edgeIDs(jordan, "memberOf"))
	after := c.countRecords(personCollection)
	c.requiref(after == before+1,
		"the resolver minted %d people, want exactly 1 (the room address mints nothing)", after-before)
	c.stepf("three emails resolved, `person-jordan` was minted under northwind by domain, and the room address `c_room7@resource.calendar.example` minted nothing")

	// Re-delivery converges: same event, same people, no duplicates.
	var reran struct {
		Ran int `json:"ran"`
	}
	status, raw := c.do(http.MethodPost, triggerCollection+"/story-resolve-attendees/run",
		map[string]any{"kind": storyAuthority + "/eventimport", "id": "imp-kickoff"}, &reran)
	c.requiref(status == http.StatusOK && reran.Ran == 1, "the synthetic re-delivery answered %d, ran %d: %s", status, reran.Ran, raw)
	ev = c.getRec(eventCollection, "ev-kickoff")
	c.requiref(sameIDs(edgeIDs(ev, "attendees"), "sam", "nour", "priya", "person-jordan") &&
		c.countRecords(personCollection) == after,
		"re-delivery changed the graph: attendees %v, %d people", edgeIDs(ev, "attendees"), c.countRecords(personCollection))
	c.stepf("a second delivery of the same import converged: 4 attendee edges, %d people, nothing duplicated", after)

	// The writes belong to the function, in the changelog's own words.
	c.requireActor("ev-kickoff", "put", actorResolver)
	c.requireActor("person-jordan", "put", actorResolver)
	c.stepf("the changelog attributes the event and the minted person to `%s`", actorResolver)
}

// caseStory03: a transcript finds its meeting, or honestly does not. The
// matcher agent scores candidates through a function tool, links the winner,
// and writes its audit either way.
func caseStory03(c *C) {
	r := c.r
	c.putTrigger("story-match-transcript", map[string]any{
		"enabled": true,
		"source": map[string]any{"record": map[string]any{
			"kinds": []string{transcriptKind}, "ops": []string{"create"},
		}},
		"callable": "core.substrate.reamde.dev/agent/" + storyAuthority + "/transcriptMatcher",
	})

	// The kickoff's transcript: title copied from the meeting (as recorders
	// do), ten minutes of clock skew, no meeting edge.
	c.putRec(transcriptCollection, "tr-kickoff", map[string]any{
		"name": "Onboarding revamp kickoff", "source": "example-notetaker",
		"at":     r.kickoffAt().Add(10 * time.Minute).Format(time.RFC3339),
		"endsAt": r.kickoffAt().Add(40 * time.Minute).Format(time.RFC3339),
		"text":   kickoffTranscript,
	}, nil)
	c.wake("story-match-transcript")
	c.waitFor("the kickoff transcript's verdict", func() bool {
		_, err := c.quietGet(verdictCollection, "mv-tr-kickoff")
		return err == nil
	})

	tr := c.getRec(transcriptCollection, "tr-kickoff")
	c.requiref(sameIDs(edgeIDs(tr, "meeting"), "ev-kickoff"),
		"tr-kickoff meeting edges: %v (the billing sync was 30 minutes away and must lose)", edgeIDs(tr, "meeting"))
	c.requiref(sameIDs(edgeIDs(tr, "speakers"), "sam", "nour", "priya"),
		"tr-kickoff speakers: %v (jordan attended silently and gets no edge)", edgeIDs(tr, "speakers"))
	verdict := c.getRec(verdictCollection, "mv-tr-kickoff")
	score, _ := verdict.Properties["score"].(float64)
	c.requiref(verdict.prop("verdict") == "matched" && score > 0.4 && verdict.prop("reason") != "" &&
		sameIDs(edgeIDs(verdict, "event"), "ev-kickoff") && sameIDs(edgeIDs(verdict, "transcript"), "tr-kickoff"),
		"the kickoff verdict: %v -> event %v, transcript %v", verdict.Properties, edgeIDs(verdict, "event"), edgeIDs(verdict, "transcript"))
	c.stepf("the matcher picked `ev-kickoff` over `ev-billing-sync`, linked the three people who spoke, and wrote its audit (`mv-tr-kickoff`)")

	// The orphan: nothing within 90 minutes, an alien title. It attaches to
	// nothing, and the audit says so.
	c.putRec(transcriptCollection, "tr-orphan", map[string]any{
		"name": "Quarterly planning notes", "source": "example-notetaker",
		"at":   r.kickoffAt().Add(-6 * time.Hour).Format(time.RFC3339),
		"text": "Speaker 1: A long walk through next quarter, unrelated to any meeting on the calendar.",
	}, nil)
	c.wake("story-match-transcript")
	c.waitFor("the orphan transcript's verdict", func() bool {
		_, err := c.quietGet(verdictCollection, "mv-tr-orphan")
		return err == nil
	})
	orphan := c.getRec(transcriptCollection, "tr-orphan")
	c.requiref(len(orphan.Edges["meeting"]) == 0,
		"the orphan transcript attached to %v; unmatched must attach to nothing", edgeIDs(orphan, "meeting"))
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
			"when":     `record != null && "meeting" in record.edges && size(record.edges.meeting) > 0`,
			"coalesce": true,
		}},
		"callable": "core.substrate.reamde.dev/agent/" + storyAuthority + "/actionItemExtractor",
	})
	c.putTrigger("story-review-proposals", map[string]any{
		"enabled": true,
		"source": map[string]any{"record": map[string]any{
			"kinds": []string{"core.substrate.reamde.dev/recordpatchrequest"}, "ops": []string{"create"},
		}},
		"callable": "core.substrate.reamde.dev/agent/" + storyAuthority + "/changeRequestReviewer",
	})

	// The matcher's meeting-edge write predates this trigger, so the cursor
	// rewinds to the beginning; the guard and coalesce pick out exactly the
	// kickoff transcript's edge landing. This is the agent-to-agent chain:
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
		sameIDs(edgeIDs(welcome, "assignee"), "nour") &&
		sameIDs(edgeIDs(welcome, "project"), "onboarding-revamp") &&
		sameIDs(edgeIDs(welcome, "source"), "tr-kickoff"),
		"task-welcome-flow landed wrong: %v %v", welcome.Properties, welcome.Edges)
	profile := c.getRec(tasksCollection, "task-profile-signup")
	c.requiref(profile.prop("status") == "open" &&
		sameIDs(edgeIDs(profile, "assignee"), "kai") &&
		sameIDs(edgeIDs(profile, "project"), "onboarding-revamp") &&
		sameIDs(edgeIDs(profile, "source"), "tr-kickoff"),
		"task-profile-signup landed wrong: %v %v", profile.Properties, profile.Edges)
	pilot := c.getRec(tasksCollection, "task-northwind-pilot")
	c.requiref(pilot.prop("status") == "proposed" && len(pilot.Edges["assignee"]) == 0,
		"the unnamed action item must wait `proposed` and unassigned: %v %v", pilot.prop("status"), edgeIDs(pilot, "assignee"))
	invite := c.getRec(tasksCollection, "task-invite-flow")
	c.requiref(invite.prop("priority") == "urgent", "the accepted priority patch did not land: %q", invite.prop("priority"))
	status, _ = c.do(http.MethodGet, tasksCollection+"/task-sourceless", nil, nil)
	c.requiref(status == http.StatusNotFound, "the sourceless task exists (answered %d); the rejection must leave nothing behind", status)
	c.stepf("2 sourced tasks landed assigned, the unnamed one waits `proposed`, the priority patch applied, and the sourceless proposal left no record behind")

	// The transcript's incoming edges name exactly the three sourced tasks.
	var incoming struct {
		Incoming []struct {
			Rel  string `json:"rel"`
			From struct {
				ID string `json:"id"`
			} `json:"from"`
		} `json:"incoming"`
	}
	status, _ = c.do(http.MethodGet, transcriptCollection+"/tr-kickoff/incoming?rel=source", nil, &incoming)
	c.requiref(status == http.StatusOK, "incoming on tr-kickoff answered %d", status)
	fromIDs := make([]string, 0, len(incoming.Incoming))
	for _, in := range incoming.Incoming {
		fromIDs = append(fromIDs, in.From.ID)
	}
	c.requiref(sameIDs(fromIDs, "task-welcome-flow", "task-profile-signup", "task-northwind-pilot"),
		"tr-kickoff's incoming source edges: %v", fromIDs)

	// Writer and decider are distinct actors on EVERY request: one pass over
	// the feed, checked per request, so a self-graded decision anywhere fails.
	byRecord := map[string]map[string]string{}
	for _, row := range c.readChangesForward(0) {
		if row.Kind != "core.substrate.reamde.dev/recordpatchrequest" {
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
	}, nil)
	c.wake("story-match-transcript")
	c.waitFor("the chitchat transcript's meeting edge", func() bool {
		rec, err := c.quietGet(transcriptCollection, "tr-chitchat")
		return err == nil && sameIDs(edgeIDs(rec, "meeting"), "ev-billing-sync")
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
			(row.Actor == "substrate" && strings.HasPrefix(row.Kind, "core.substrate.reamde.dev/"))
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
	c.stepf("the GraphQL join over tasks and their edges answers %d bytes", len(join))

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
// whole story graph, every kind the stories touched, with edges.
func (c *C) graphJoin() []byte {
	c.t.Helper()
	kinds := []string{
		taskKind, projectKind, personKind, teamKind, orgKind,
		eventKind, transcriptKind, storyAuthority + "/matchverdict",
		"core.substrate.reamde.dev/recordpatchrequest",
	}
	query := `{ records(filter: {kinds: ["` + strings.Join(kinds, `", "`) + `"]}, orderBy: [{property: "createdAt"}], first: 200) {
		nodes { id kind properties edges { rel target { id kind } } } } }`
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
func (c *C) quietList(collection string) ([]record, error) {
	var page struct {
		Records []record `json:"records"`
	}
	if err := c.r.fetch(collection+"?first=200", &page); err != nil {
		return nil, err
	}
	return page.Records, nil
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
		tr, _ := rec.Properties["trigger"].(string)
		if strings.HasSuffix(tr, "/"+trigger) && rec.prop("status") == "ok" {
			n++
		}
	}
	return n
}

// --- the scripted models -----------------------------------------------

// matcherResponder drives the matcher agent: score, decide, link, audit.
func matcherResponder(llmReq llmReq) llmTurn {
	rec := llmReq.deliveredRecord()
	tid, _ := rec["id"].(string)
	link := func(alias, rel, dstKind, dst string) string {
		return fmt.Sprintf("%s: link(rel: %q, srcKind: %q, src: %q, dstKind: %q, dst: %q) { ok }",
			alias, rel, transcriptKind, tid, dstKind, dst)
	}
	verdict := func(v string, score float64, reason, event string) llmCall {
		input := map[string]any{
			"kind": storyAuthority + "/matchverdict", "id": "mv-" + tid,
			"properties": map[string]any{"verdict": v, "score": score, "reason": reason},
			"edges":      []map[string]any{{"rel": "transcript", "to": map[string]any{"kind": transcriptKind, "id": tid}}},
		}
		if event != "" {
			input["edges"] = append(input["edges"].([]map[string]any),
				map[string]any{"rel": "event", "to": map[string]any{"kind": eventKind, "id": event}})
		}
		return llmCall{"mutate", map[string]any{
			"query":     "mutation($in: JSON!) { put(input: $in) { id } }",
			"variables": map[string]any{"in": input},
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
		q := "mutation { " + link("m", "meeting", eventKind, event)
		for i, s := range speakers[tid] {
			q += " " + link(fmt.Sprintf("s%d", i+1), "speakers", personKind, s)
		}
		q += " }"
		return llmTurn{calls: []llmCall{{"mutate", map[string]any{"query": q}}}}
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
		source := map[string]any{"rel": "source", "to": map[string]any{"kind": transcriptKind, "id": "tr-kickoff"}}
		onboarding := map[string]any{"rel": "project", "to": map[string]any{"kind": projectKind, "id": "onboarding-revamp"}}
		assignee := func(id string) map[string]any {
			return map[string]any{"rel": "assignee", "to": map[string]any{"kind": personKind, "id": id}}
		}
		proposals := []llmCall{
			{"propose", map[string]any{
				"op": "create", "kind": taskKind, "id": "task-welcome-flow",
				"diff": map[string]any{
					"properties": map[string]any{
						"name":  "Draft the new welcome flow",
						"dueAt": r.kickoffAt().Add(96 * time.Hour).Format(time.RFC3339),
					},
					"edges": []map[string]any{source, onboarding, assignee("nour")},
				},
				"rationale": "Nour committed to it in the kickoff, due Friday.",
			}},
			{"propose", map[string]any{
				"op": "create", "kind": taskKind, "id": "task-profile-signup",
				"diff": map[string]any{
					"properties": map[string]any{"name": "Profile the signup path"},
					"edges":      []map[string]any{source, onboarding, assignee("kai")},
				},
				"rationale": "Sam asked Kai to profile before any redesign.",
			}},
			{"propose", map[string]any{
				"op": "create", "kind": taskKind, "id": "task-northwind-pilot",
				"diff": map[string]any{
					"properties": map[string]any{"name": "Sync with Northwind about the pilot scope", "status": "proposed"},
					"edges":      []map[string]any{source, onboarding},
				},
				"rationale": "The meeting named the work but not the person; it waits for the owner.",
			}},
			{"propose", map[string]any{
				"op": "create", "kind": taskKind, "id": "task-sourceless",
				"diff": map[string]any{
					"properties": map[string]any{"name": "A hunch with no evidence behind it"},
					"edges":      []map[string]any{onboarding, assignee("sam")},
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

// arbiterResponder decides one request per delivery: a create without a
// source edge is rejected, everything else is accepted.
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
		"query": `mutation($id: ID!, $in: JSON!) { patch(kind: "core.substrate.reamde.dev/recordpatchrequest", id: $id, input: $in) { id } }`,
		"variables": map[string]any{
			"id": id,
			"in": map[string]any{"properties": map[string]any{"decision": decision}},
		},
	}}}}
}

func diffCarriesSource(diff any) bool {
	m, _ := diff.(map[string]any)
	edges, _ := m["edges"].([]any)
	for _, e := range edges {
		if em, _ := e.(map[string]any); em != nil && em["rel"] == "source" {
			return true
		}
	}
	return false
}
