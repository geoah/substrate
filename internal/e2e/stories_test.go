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

	personKind   = "people.substrate.reamde.dev/person"
	teamKind     = "people.substrate.reamde.dev/team"
	orgKind      = "people.substrate.reamde.dev/organization"
	projectKind  = "tasks.substrate.reamde.dev/project"
	calendarKind = "calendar.substrate.reamde.dev/calendar"
)

// recPath renders a reference value: the canonical `<kind>/<id>` path. A
// reference pinned to one kind also accepts the bare id on write, but it
// reads back canonical, so every assertion here is written against the path.
func recPath(kind, id string) string { return kind + "/" + id }

// linkTo renders a reference whose declaration carries link data: the target
// under the one reserved key, the declared properties beside it.
func linkTo(path string, props map[string]any) map[string]any {
	v := map[string]any{"ref": path}
	for k, p := range props {
		v[k] = p
	}
	return v
}

// putRec writes one record at a chosen id and returns the fold's answer.
// References are ordinary properties, so there is nothing beside props to pass.
func (c *C) putRec(collection, id string, props map[string]any) record {
	c.t.Helper()
	var rec record
	status, raw := c.do(http.MethodPut, collection+"/"+url.PathEscape(id),
		map[string]any{"properties": props}, &rec)
	c.requiref(status == http.StatusCreated || status == http.StatusOK,
		"PUT %s/%s answered %d: %s", collection, id, status, raw)
	return rec
}

// getRec reads one record.
func (c *C) getRec(collection, id string) record {
	c.t.Helper()
	var rec record
	status, raw := c.do(http.MethodGet, collection+"/"+url.PathEscape(id), nil, &rec)
	c.requiref(status == http.StatusOK, "GET %s/%s answered %d: %s", collection, id, status, raw)
	return rec
}

// refPaths flattens one reference property into the canonical paths it names.
// One property covers every arity the declaration can take: a single reference
// is a string, a repeated one an array, and either shape becomes an object
// holding the path under `ref` when the declaration carries link data.
func refPaths(rec record, name string) []string {
	return refPathsOf(rec.Properties[name])
}

func refPathsOf(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case map[string]any:
		if s, ok := t["ref"].(string); ok {
			return []string{s}
		}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			out = append(out, refPathsOf(e)...)
		}
		return out
	}
	return nil
}

// refPath reads a SINGLE-valued reference property as the path it points at.
// A reference is served as the object holding that path under `ref` (decision
// 0044), so `prop`, which reads a string, answers "" for one.
func refPath(v any) string {
	if paths := refPathsOf(v); len(paths) == 1 {
		return paths[0]
	}
	return ""
}

// linkProp reads one link property off a repeated reference, keyed by the
// path it points at.
func linkProp(rec record, name, path, prop string) any {
	list, _ := rec.Properties[name].([]any)
	if list == nil {
		if one, ok := rec.Properties[name].(map[string]any); ok {
			list = []any{one}
		}
	}
	for _, e := range list {
		m, ok := e.(map[string]any)
		if ok && m["ref"] == path {
			return m[prop]
		}
	}
	return nil
}

func sameSet(got []string, want ...string) bool {
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
// every end: forward references, the reverse read, filtered lists, and the
// refusals that keep the pointers well-typed.
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
	// membership. `memberOf` is a repeated reference whose declaration carries
	// link data, so each value is the org path under `ref` with the role beside
	// it.
	c.putRec(orgCollection, "acme", map[string]any{"name": "Acme", "domain": "acme.example"})
	c.putRec(orgCollection, "northwind", map[string]any{"name": "Northwind", "domain": "northwind.example"})
	person := func(id, name, email, pronouns, org, role string) {
		props := map[string]any{
			"name":     name,
			"emails":   []string{email},
			"memberOf": []any{linkTo(recPath(orgKind, org), map[string]any{"role": role})},
		}
		if pronouns != "" {
			props["pronouns"] = pronouns
		}
		c.putRec(personCollection, id, props)
	}
	person("me", "Jo Doe", "jo@acme.example", "they/them", "acme", "founder")
	person("sam", "Sam Rivera", "sam@acme.example", "she/her", "acme", "product lead")
	person("rae", "Rae Kim", "rae@acme.example", "", "acme", "engineering lead")
	person("nour", "Nour Haddad", "nour@acme.example", "they/them", "acme", "engineer")
	person("kai", "Kai Tanaka", "kai@acme.example", "he/him", "acme", "platform engineer")
	person("ada", "Ada Osei", "ada@acme.example", "she/her", "acme", "designer")
	person("priya", "Priya Sharma", "priya@northwind.example", "she/her", "northwind", "client contact")

	// Teams: members and leads (repeated references), and Platform nested
	// under Engineering through the single `parent` reference.
	team := func(id, name string, extra map[string]any, members ...string) {
		refs := make([]any, 0, len(members))
		for _, m := range members {
			refs = append(refs, recPath(personKind, m))
		}
		props := map[string]any{"name": name, "members": refs}
		for k, v := range extra {
			props[k] = v
		}
		c.putRec(teamCollection, id, props)
	}
	team("product", "Product", map[string]any{"leads": []any{recPath(personKind, "sam")}}, "me", "sam", "ada")
	team("engineering", "Engineering", map[string]any{"leads": []any{recPath(personKind, "rae")}}, "rae", "nour", "kai")
	team("platform", "Platform", map[string]any{"parent": recPath(teamKind, "engineering")}, "kai")

	// Projects and their tasks.
	c.putRec(projectCollection, "onboarding-revamp",
		map[string]any{"name": "Onboarding revamp", "summary": "Make the first day feel like the demo."})
	c.putRec(projectCollection, "billing-migration",
		map[string]any{"name": "Billing migration", "summary": "Move billing off the legacy ledger."})
	task := func(id, name, project, assignee string, due time.Duration) {
		props := map[string]any{"name": name, "project": recPath(projectKind, project)}
		if due != 0 {
			props["dueAt"] = time.Now().Add(due).UTC().Format(time.RFC3339)
		}
		if assignee != "" {
			props["assignee"] = recPath(personKind, assignee)
		}
		c.putRec(tasksCollection, id, props)
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
	c.putRec(calendarCollection, "work", map[string]any{"name": "Work", "timezone": "Europe/London"})
	c.putRec(eventCollection, "ev-billing-sync", map[string]any{
		"summary":   "Billing migration sync",
		"at":        r.kickoffAt().Add(30 * time.Minute).Format(time.RFC3339),
		"endsAt":    r.kickoffAt().Add(60 * time.Minute).Format(time.RFC3339),
		"calendar":  recPath(calendarKind, "work"),
		"organizer": recPath(personKind, "rae"),
		"attendees": []any{recPath(personKind, "rae"), recPath(personKind, "kai")},
	})
	c.stepf("seeded the ecosystem: 2 organizations, 7 people, 3 teams, 2 projects, 6 tasks, 1 calendar, 1 event")

	// Forward references read back exactly, and canonical: `project` and
	// `assignee` were authored as full paths, and the pinned `parent` is the
	// path too.
	rec := c.getRec(tasksCollection, "task-usage-export")
	c.requiref(sameSet(refPaths(rec, "assignee"), recPath(personKind, "nour")),
		"task-usage-export assignee: %v", refPaths(rec, "assignee"))
	c.requiref(sameSet(refPaths(rec, "project"), recPath(projectKind, "billing-migration")),
		"task-usage-export project: %v", refPaths(rec, "project"))
	platform := c.getRec(teamCollection, "platform")
	c.requiref(sameSet(refPaths(platform, "parent"), recPath(teamKind, "engineering")),
		"platform parent: %v", refPaths(platform, "parent"))
	c.stepf("forward references read back as canonical paths (task -> assignee/project, team -> parent)")

	// The link data rides with the pointer: Nour's membership carries the role
	// under the same value as the org it points at.
	nour := c.getRec(personCollection, "nour")
	c.requiref(sameSet(refPaths(nour, "memberOf"), recPath(orgKind, "acme")),
		"nour memberOf: %v", refPaths(nour, "memberOf"))
	c.requiref(linkProp(nour, "memberOf", recPath(orgKind, "acme"), "role") == "engineer",
		"nour's membership role: %v", nour.Properties["memberOf"])
	c.stepf("`memberOf` carries its link data: the org under `ref`, the role `engineer` beside it")

	// The reverse view: the references naming Nour name the task assigned to
	// them and the team that holds them, without Nour's record storing either.
	var incoming struct {
		Incoming []struct {
			Property string `json:"property"`
			From     struct {
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
		found[in.Property+" from "+in.From.ID] = true
	}
	c.requiref(found["assignee from task-usage-export"] && found["members from engineering"],
		"the references naming nour miss the task or the team: %v", found)
	c.stepf("incoming on `nour` names both ends: `assignee` from the task, `members` from the team (%d rows)", incoming.Total)

	// References are ordinary properties, so a plain list carries them: there
	// is nothing to opt into, and the retired opt-in is refused as a param.
	var listed struct {
		Records []record `json:"records"`
	}
	status, raw = c.do(http.MethodGet, tasksCollection+"?first=50", nil, &listed)
	c.requiref(status == http.StatusOK, "the task list answered %d: %s", status, raw)
	pointed := 0
	for _, rec := range listed.Records {
		if len(refPaths(rec, "project")) > 0 {
			pointed++
		}
	}
	c.requiref(pointed >= 5, "the list carries a project reference on %d of %d tasks", pointed, len(listed.Records))
	c.stepf("a plain list carries every reference inline: %d of %d tasks name their project", pointed, len(listed.Records))

	// One filtered list: open tasks assigned to Nour, ordered by dueAt. A
	// pinned reference filters by the bare id too, the way a write accepts it.
	filter := url.QueryEscape(`{"properties":{"status":{"eq":"open"},"assignee":{"eq":"nour"}}}`)
	var page struct {
		Records []record `json:"records"`
	}
	status, raw = c.do(http.MethodGet, tasksCollection+"?filter="+filter+"&orderBy=dueAt", nil, &page)
	c.requiref(status == http.StatusOK, "the filtered list answered %d: %s", status, raw)
	c.requiref(len(page.Records) == 1 && page.Records[0].ID == "task-usage-export",
		"open tasks assigned to nour: want exactly task-usage-export, got %d records", len(page.Records))
	c.stepf("the filter grammar answers the one open task assigned to `nour`")

	// The refusals that keep the pointers well-typed: an `assignee` pinned to
	// person cannot name a team, a `mustExist` reference cannot be born
	// dangling, and a link property the declaration does not declare is
	// refused naming it.
	status, raw = c.do(http.MethodPost, tasksCollection, map[string]any{
		"properties": map[string]any{"name": "A task aimed at a team", "assignee": recPath(teamKind, "engineering")},
	}, nil)
	c.requiref(status == http.StatusUnprocessableEntity,
		"an assignee naming a team answered %d, want 422: %s", status, raw)
	status, raw = c.do(http.MethodPost, tasksCollection, map[string]any{
		"properties": map[string]any{"name": "A task aimed at nobody", "assignee": recPath(personKind, "nobody-at-all")},
	}, nil)
	c.requiref(status == http.StatusNotFound,
		"an assignee at an absent person answered %d, want 404: %s", status, raw)
	status, raw = c.do(http.MethodPost, personCollection, map[string]any{
		"properties": map[string]any{
			"name":     "A chatty member",
			"memberOf": []any{linkTo(recPath(orgKind, "acme"), map[string]any{"mood": "hopeful"})},
		},
	}, nil)
	c.requiref(status == http.StatusUnprocessableEntity && strings.Contains(string(raw), "mood"),
		"an undeclared link property answered %d without naming it: %s", status, raw)
	c.stepf("refused: an `assignee` naming a team, an `assignee` at an absent person, and a link property (`mood`) `memberOf` does not declare")

	// The retired shape is refused by name rather than dropped in silence, so
	// a client written against the old model loses nothing quietly.
	status, raw = c.do(http.MethodPost, tasksCollection, map[string]any{
		"properties": map[string]any{"name": "A task with edges"},
		"edges":      map[string]any{"assignee": []any{map[string]any{"id": "sam"}}},
	}, nil)
	c.requiref(status == http.StatusBadRequest, "a body still writing `edges` answered %d, want 400: %s", status, raw)
	c.requiref(strings.Contains(xrRefusal(c, raw).Error.Message, `unknown field "edges"`),
		"the refusal does not name the retired key: %s", raw)
	c.stepf("a body still writing `edges` beside `properties` is a 400 naming the unknown key")
}

// kickoffAt anchors the whole story timeline: one instant, derived once per
// run, so events, transcripts and dueAts agree with each other.
func (r *run) kickoffAt() time.Time {
	return r.rep.Started.UTC().Truncate(time.Minute).Add(-2 * time.Hour)
}
