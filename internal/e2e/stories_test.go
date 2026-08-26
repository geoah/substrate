package e2e

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The story cases (STORIES.md) share one fictional ecosystem: acme, its
// people, projects and calendar. Ids are chosen, not server-assigned, so
// every later story (and every scripted agent turn) can name records
// exactly.
const (
	orgCollection     = "/api/v1/people.substrate.reamde.dev/organization"
	personCollection  = "/api/v1/people.substrate.reamde.dev/person"
	teamCollection    = "/api/v1/people.substrate.reamde.dev/team"
	projectCollection = "/api/v1/tasks.substrate.reamde.dev/project"

	calendarCollection   = "/api/v1/calendar.substrate.reamde.dev/calendar"
	eventCollection      = "/api/v1/calendar.substrate.reamde.dev/calendarevent"
	transcriptCollection = "/api/v1/calendar.substrate.reamde.dev/transcript"

	personKind = "people.substrate.reamde.dev/person"
	teamKind   = "people.substrate.reamde.dev/team"
	orgKind    = "people.substrate.reamde.dev/organization"
)

// edge is the write-side edge shape (substrate.EdgeInput).
type edge struct {
	Rel        string         `json:"rel"`
	To         edgeTarget     `json:"to"`
	Properties map[string]any `json:"properties,omitempty"`
}

type edgeTarget struct {
	Kind string `json:"kind,omitempty"`
	ID   string `json:"id"`
}

// putRec writes one record at a chosen id and returns the fold's answer.
func (c *C) putRec(collection, id string, props map[string]any, edges []edge) record {
	c.t.Helper()
	body := map[string]any{"properties": props}
	if len(edges) > 0 {
		body["edges"] = edges
	}
	var rec record
	status, raw := c.do(http.MethodPut, collection+"/"+url.PathEscape(id), body, &rec)
	c.requiref(status == http.StatusCreated || status == http.StatusOK,
		"PUT %s/%s answered %d: %s", collection, id, status, raw)
	return rec
}

// getRec reads one record (single GETs carry edges).
func (c *C) getRec(collection, id string) record {
	c.t.Helper()
	var rec record
	status, raw := c.do(http.MethodGet, collection+"/"+url.PathEscape(id), nil, &rec)
	c.requiref(status == http.StatusOK, "GET %s/%s answered %d: %s", collection, id, status, raw)
	return rec
}

// edgeIDs flattens one rel's targets for exact assertions.
func edgeIDs(rec record, rel string) []string {
	ids := make([]string, 0, len(rec.Edges[rel]))
	for _, to := range rec.Edges[rel] {
		ids = append(ids, to.ID)
	}
	return ids
}

func sameIDs(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]bool{}
	for _, id := range got {
		seen[id] = true
	}
	for _, id := range want {
		if !seen[id] {
			return false
		}
	}
	return true
}

// caseStory01 seeds the ecosystem and proves the graph is navigable from
// every end: forward edges, incoming edges, filtered lists, and the edge
// refusals that keep it well-typed.
func caseStory01(c *C) {
	r := c.r

	// calendar joins the vocabulary REC-01 installed (it requires people and
	// scheduling, both already in).
	var bundle struct {
		Installed bool `json:"installed"`
	}
	status, raw := c.do(http.MethodPost,
		"/api/v1/catalog/"+url.PathEscape("calendar.substrate.reamde.dev/calendar")+"/install", nil, &bundle)
	c.requiref(status == http.StatusOK && bundle.Installed, "installing calendar answered %d: %s", status, raw)

	// The organizations, and the people with their emails, pronouns and org
	// membership (the memberOf edge carries the declared role property).
	c.putRec(orgCollection, "acme", map[string]any{"name": "Acme", "domain": "acme.example"}, nil)
	c.putRec(orgCollection, "northwind", map[string]any{"name": "Northwind", "domain": "northwind.example"}, nil)
	person := func(id, name, email, pronouns, org, role string) {
		props := map[string]any{"name": name, "emails": []string{email}}
		if pronouns != "" {
			props["pronouns"] = pronouns
		}
		c.putRec(personCollection, id, props,
			[]edge{{Rel: "memberOf", To: edgeTarget{ID: org}, Properties: map[string]any{"role": role}}})
	}
	person("me", "Jo Doe", "jo@acme.example", "they/them", "acme", "founder")
	person("sam", "Sam Rivera", "sam@acme.example", "she/her", "acme", "product lead")
	person("rae", "Rae Kim", "rae@acme.example", "", "acme", "engineering lead")
	person("nour", "Nour Haddad", "nour@acme.example", "they/them", "acme", "engineer")
	person("kai", "Kai Tanaka", "kai@acme.example", "he/him", "acme", "platform engineer")
	person("ada", "Ada Osei", "ada@acme.example", "she/her", "acme", "designer")
	person("priya", "Priya Sharma", "priya@northwind.example", "she/her", "northwind", "client contact")

	// Teams: members and leads, and Platform nested under Engineering.
	team := func(id, name string, extra []edge, members ...string) {
		edges := extra
		for _, m := range members {
			edges = append(edges, edge{Rel: "members", To: edgeTarget{ID: m}})
		}
		c.putRec(teamCollection, id, map[string]any{"name": name}, edges)
	}
	team("product", "Product", []edge{{Rel: "leads", To: edgeTarget{ID: "sam"}}}, "me", "sam", "ada")
	team("engineering", "Engineering", []edge{{Rel: "leads", To: edgeTarget{ID: "rae"}}}, "rae", "nour", "kai")
	team("platform", "Platform", []edge{{Rel: "parent", To: edgeTarget{ID: "engineering"}}}, "kai")

	// Projects and their tasks.
	c.putRec(projectCollection, "onboarding-revamp",
		map[string]any{"name": "Onboarding revamp", "summary": "Make the first day feel like the demo."}, nil)
	c.putRec(projectCollection, "billing-migration",
		map[string]any{"name": "Billing migration", "summary": "Move billing off the legacy ledger."}, nil)
	task := func(id, name, project, assignee string, due time.Duration) {
		props := map[string]any{"name": name}
		if due != 0 {
			props["dueAt"] = time.Now().Add(due).UTC().Format(time.RFC3339)
		}
		edges := []edge{{Rel: "project", To: edgeTarget{ID: project}}}
		if assignee != "" {
			edges = append(edges, edge{Rel: "assignee", To: edgeTarget{ID: assignee}})
		}
		c.putRec(tasksCollection, id, props, edges)
	}
	task("task-funnel-metrics", "Define the onboarding funnel metrics", "onboarding-revamp", "ada", 72*time.Hour)
	task("task-invite-flow", "Redesign the invite flow", "onboarding-revamp", "sam", 96*time.Hour)
	task("task-docs-pass", "Read the onboarding docs end to end", "onboarding-revamp", "", 0)
	task("task-billing-cutover", "Write the billing cutover plan", "billing-migration", "rae", 48*time.Hour)
	task("task-billing-dryrun", "Dry-run the billing cutover", "billing-migration", "kai", 0)
	task("task-usage-export", "Export usage snapshots for Northwind", "billing-migration", "nour", 24*time.Hour)

	// The calendar, and the billing sync event, hand-authored with its
	// attendees. (The kickoff event arrives in STORY-02, the way an importer
	// would deliver it.)
	c.putRec(calendarCollection, "work", map[string]any{"name": "Work", "timezone": "Europe/London"}, nil)
	c.putRec(eventCollection, "ev-billing-sync", map[string]any{
		"summary": "Billing migration sync",
		"at":      r.kickoffAt().Add(30 * time.Minute).Format(time.RFC3339),
		"endsAt":  r.kickoffAt().Add(60 * time.Minute).Format(time.RFC3339),
	}, []edge{
		{Rel: "calendar", To: edgeTarget{ID: "work"}},
		{Rel: "organizer", To: edgeTarget{ID: "rae"}},
		{Rel: "attendees", To: edgeTarget{ID: "rae"}},
		{Rel: "attendees", To: edgeTarget{ID: "kai"}},
	})
	c.stepf("seeded the ecosystem: 2 organizations, 7 people, 3 teams, 2 projects, 6 tasks, 1 calendar, 1 event")

	// Forward edges read back exactly.
	rec := c.getRec(tasksCollection, "task-usage-export")
	c.requiref(sameIDs(edgeIDs(rec, "assignee"), "nour"), "task-usage-export assignee edges: %v", edgeIDs(rec, "assignee"))
	c.requiref(sameIDs(edgeIDs(rec, "project"), "billing-migration"), "task-usage-export project edges: %v", edgeIDs(rec, "project"))
	platform := c.getRec(teamCollection, "platform")
	c.requiref(sameIDs(edgeIDs(platform, "parent"), "engineering"), "platform parent edges: %v", edgeIDs(platform, "parent"))
	c.stepf("forward edges read back exactly (task -> assignee/project, team -> parent)")

	// The reverse view: Nour's incoming edges name the task assigned to them
	// and the team that holds them, without Nour's record storing either.
	var incoming struct {
		Incoming []struct {
			Rel  string `json:"rel"`
			From struct {
				ID   string `json:"id"`
				Kind string `json:"kind"`
			} `json:"from"`
		} `json:"incoming"`
		Total int `json:"total"`
	}
	status, raw = c.do(http.MethodGet, personCollection+"/nour/incoming", nil, &incoming)
	c.requiref(status == http.StatusOK, "incoming on nour answered %d: %s", status, raw)
	found := map[string]bool{}
	for _, in := range incoming.Incoming {
		found[in.Rel+" from "+in.From.ID] = true
	}
	c.requiref(found["assignee from task-usage-export"] && found["members from engineering"],
		"nour's incoming edges miss the task or the team: %v", found)
	c.stepf("incoming on `nour` names both ends: `assignee` from the task, `members` from the team (%d rows)", incoming.Total)

	// One filtered list: open tasks assigned to Nour, ordered by dueAt.
	filter := url.QueryEscape(`{"properties":{"status":{"eq":"open"}},"edge":{"rel":"assignee","to":"nour"}}`)
	var page struct {
		Records []record `json:"records"`
	}
	status, raw = c.do(http.MethodGet, tasksCollection+"?filter="+filter+"&orderBy=dueAt", nil, &page)
	c.requiref(status == http.StatusOK, "the filtered list answered %d: %s", status, raw)
	c.requiref(len(page.Records) == 1 && page.Records[0].ID == "task-usage-export",
		"open tasks assigned to nour: want exactly task-usage-export, got %d records", len(page.Records))
	c.stepf("the filter grammar answers the one open task assigned to `nour`")

	// The refusals that keep the graph well-typed: an assignee edge cannot
	// point at a team, and an edge property the rel does not declare is
	// refused naming it.
	status, raw = c.do(http.MethodPost, tasksCollection, map[string]any{
		"properties": map[string]any{"name": "A task aimed at a team"},
		"edges":      []edge{{Rel: "assignee", To: edgeTarget{Kind: teamKind, ID: "engineering"}}},
	}, nil)
	c.requiref(status == http.StatusUnprocessableEntity,
		"an assignee edge aimed at a team answered %d, want 422: %s", status, raw)
	status, raw = c.do(http.MethodPost, tasksCollection, map[string]any{
		"properties": map[string]any{"name": "A task with a chatty edge"},
		"edges": []edge{{Rel: "project", To: edgeTarget{ID: "onboarding-revamp"},
			Properties: map[string]any{"mood": "hopeful"}}},
	}, nil)
	c.requiref(status == http.StatusUnprocessableEntity && strings.Contains(string(raw), "mood"),
		"an undeclared edge property answered %d without naming it: %s", status, raw)
	c.stepf("refused: an `assignee` edge aimed at a team, and an edge property (`mood`) the rel does not declare")
}

// kickoffAt anchors the whole story timeline: one instant, derived once per
// run, so events, transcripts and dueAts agree with each other.
func (r *run) kickoffAt() time.Time {
	return r.rep.Started.UTC().Truncate(time.Minute).Add(-2 * time.Hour)
}

