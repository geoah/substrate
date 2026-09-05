package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// The record, reference, merge and split cases: CASES.md REC-02 through
// REC-08, REF-01, REF-03, REF-04, MRG-01 and MRG-02. They run after the stories,
// over the repository the stories built, and every record they write or
// delete carries the `x-` id prefix so the acme world stays as it was left.
// The one exception is REC-06, which has to write the reserved words
// themselves; it says so where it does.
func init() {
	registerCase(200, "REC-02", "Optimistic concurrency through ifVersion",
		"A put carrying the stored version wins; a second put carrying the same, now stale, version is a 409 "+
			"`conflict` naming both numbers, and leaves the record exactly as the winner left it.",
		xrCaseIfVersion)
	registerCase(210, "REC-03", "What a PATCH can do to a property",
		"A null deletes the property outright, a state property is a transition that stamps its own time, and a "+
			"transition the kind does not declare is a 403 `guard` naming the pair it refused.",
		xrCasePatch)
	registerCase(220, "REC-04", "An undeclared property is refused naming it",
		"A create and a patch that carry a property the kind never declared both answer 422 `validation` with the "+
			"problem addressed to `props.bogusprop`, and no record is written.",
		xrCaseUndeclaredProperty)
	registerCase(230, "REC-05", "The heading derives from the declared property",
		"A task's title is rendered from its `name` through the `{name|title}` displayTemplate: a `title` written "+
			"beside the name is dropped without a word, and a task written with a title alone reads back with no "+
			"title at all.",
		xrCaseTitleDerivation)
	registerCase(240, "REC-06", "Client-chosen ids, and the two reserved words",
		"A POST carrying an id lands at that id and a PUT lands at the path's id whatever the body says; "+
			"`incoming` is refused as an id at the record path in both directions, and it is the only word that is.",
		xrCaseChosenIDs)
	registerCase(250, "REC-07", "Labels and annotations round-trip",
		"Both maps demand namespaced `<actor>/<name>` keys; a list row carries the labels but never the "+
			"annotations, `withAnnotations=1` adds them, and a single GET carries both.",
		xrCaseLabelsAnnotations)
	registerCase(260, "REC-08", "propertyMeta names who manages each property",
		"Two doors write two properties of one record; the single GET's propertyMeta attributes each property to "+
			"the actor that last wrote it, at the owner tier, and a list row carries no propertyMeta at all.",
		xrCasePropertyMeta)
	registerCase(270, "REF-01", "Writing, keeping and clearing a reference",
		"A put writes `assignee` as an ordinary property and a second put that omits it keeps it (a put merges, "+
			"never prunes), so clearing a pointer is an explicit `null` through patch; the reference reads from "+
			"both ends while it is set, and the retired `…/{id}/edges/{rel}` route is gone.",
		xrCaseReferenceWrites)
	registerCase(280, "REF-03", "A reference outlives the target's tombstone",
		"Deleting a person leaves every reference naming them standing: the holder's GET still spells the target "+
			"path, the tombstone still answers /incoming, and the holder's version never moves (decision 0027).",
		xrCaseReferenceTombstone)
	registerCase(285, "REF-04", "/incoming narrows and pages",
		"`property` narrows to one reference property, `fromKind` to one source kind, `first`+`after` walk the "+
			"fan-in one distinct row at a time, and a parameter the reverse read does not take (`rel`, `orderBy`) "+
			"is a 400 naming it.",
		xrCaseIncoming)
	registerCase(290, "MRG-01", "Merge folds two records into one",
		"`POST /api/v1/merge` writes a `recordmerge` record; the loser's id keeps resolving, now answering the "+
			"canonical record with the loser as a formerId. Nothing repoints: the task's `assignee` still spells "+
			"the loser, and the winner's /incoming finds it through the former id.",
		xrCaseMerge)
	registerCase(295, "MRG-02", "Split reverses the merge",
		"`POST /api/v1/split` gives the loser its record back: no canonicalId, no formerId on the winner, the "+
			"task's unmoved `assignee` naming a live record again, and the merge record itself tombstoned.",
		xrCaseSplit)
}

// The ids this group writes. Everything is `x-` prefixed: the stories own
// every other id in the repository.
const (
	xrCASTask     = "x-rec-cas"
	xrPatchTask   = "x-rec-patch"
	xrPropTask    = "x-rec-prop"
	xrTitleTask   = "x-rec-title"
	xrTitleOnly   = "x-rec-title-only"
	xrChosenPost  = "x-chosen"
	xrChosenPut   = "x-chosen2"
	xrChosenGhost = "x-chosen-elsewhere"
	xrMetaTask    = "x-rec-meta"
	xrActorsTask  = "x-rec-actors"
	xrRefTask     = "x-ref-task"
	xrRefVictim   = "x-ref-victim"
	xrRefHolder   = "x-ref-holder"
	xrDupWinner   = "x-dup-a"
	xrDupLoser    = "x-dup-b"
	xrDupTask     = "x-dup-task"

	xrMergeCollection = "/api/v1/substrate.reamde.dev/core/recordmerge"
	xrMergeKind       = "substrate.reamde.dev/core/recordmerge"
	xrSplitKind       = "substrate.reamde.dev/core/recordsplit"
)

// xrRecord is the read shape these cases assert on: the harness `record`
// narrowed to what the stories needed, widened again by the fields the
// metadata, provenance and merge cases are about.
type xrRecord struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	Version     int64          `json:"version"`
	CanonicalID string         `json:"canonicalId"`
	FormerIDs   []string       `json:"formerIds"`
	Properties  map[string]any `json:"properties"`
	Labels      map[string]any `json:"labels"`
	Annotations map[string]any `json:"annotations"`
	DeletedAt   string         `json:"deletedAt"`

	PropertyMeta map[string]struct {
		Manager      string `json:"manager"`
		Tier         string `json:"tier"`
		Alternatives []struct {
			Actor string `json:"actor"`
			Value any    `json:"value"`
		} `json:"alternatives"`
	} `json:"propertyMeta"`
}

func (r xrRecord) prop(name string) string {
	v, _ := r.Properties[name].(string)
	return v
}

func (r xrRecord) hasProp(name string) bool {
	_, ok := r.Properties[name]
	return ok
}

// xrRefPaths flattens one reference property, the wider shape's twin of
// refPaths.
func xrRefPaths(rec xrRecord, name string) []string { return refPathsOf(rec.Properties[name]) }

// xrProblem is the error envelope every refusal in this file is pinned
// against: the closed `code` set, the message, and the field-addressed
// problems a validation failure carries beside it.
type xrProblem struct {
	Error struct {
		Code           string   `json:"code"`
		Message        string   `json:"message"`
		Problems       []string `json:"problems"`
		ProblemDetails []struct {
			Path    string `json:"path"`
			Message string `json:"message"`
		} `json:"problemDetails"`
	} `json:"error"`
}

func xrRefusal(c *C, raw []byte) xrProblem {
	c.t.Helper()
	var p xrProblem
	c.requiref(json.Unmarshal(raw, &p) == nil, "undecodable error envelope: %s", raw)
	return p
}

// xrTaskPath addresses one task record; the id is escaped because a chosen
// id is the client's to spell.
func xrTaskPath(id string) string { return tasksCollection + "/" + url.PathEscape(id) }

// xrPersonPath addresses one person record.
func xrPersonPath(id string) string { return personCollection + "/" + url.PathEscape(id) }

// xrGet reads one record into the wider shape. Single GETs carry the
// annotations and the propertyMeta a list omits.
func xrGet(c *C, path string) xrRecord {
	c.t.Helper()
	var rec xrRecord
	status, raw := c.do(http.MethodGet, path, nil, &rec)
	c.requiref(status == http.StatusOK, "GET %s answered %d: %s", path, status, raw)
	return rec
}

// xrListFind reads a collection page and returns the row with this id, plus
// whether it was there at all. It exists because the metadata cases assert on
// what a LIST row omits, which a single GET can never show.
func xrListFind(c *C, path, id string) (xrRecord, bool) {
	c.t.Helper()
	var page struct {
		Records []xrRecord `json:"records"`
	}
	status, raw := c.do(http.MethodGet, path, nil, &page)
	c.requiref(status == http.StatusOK, "GET %s answered %d: %s", path, status, raw)
	for _, rec := range page.Records {
		if rec.ID == id {
			return rec, true
		}
	}
	return xrRecord{}, false
}

// xrDoAs sends one exchange under a named door. The harness's own do() names
// no actor, so every write it makes is attributed to `api`; REC-08 needs a
// second door writing the same record, and `X-Substrate-Actor` is the only
// place a request may name one.
func xrDoAs(c *C, actor, method, path string, body, out any) (int, []byte) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		c.requiref(err == nil, "encoding the %s %s body: %v", method, path, err)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.r.base+path, rd)
	c.requiref(err == nil, "building %s %s: %v", method, path, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.r.token)
	req.Header.Set("X-Substrate-Actor", actor)
	resp, err := c.r.hc.Do(req)
	c.requiref(err == nil, "%s %s as %s: %v", method, path, actor, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	c.requiref(err == nil, "reading %s %s as %s: %v", method, path, actor, err)
	c.stepf("`%s %s` as actor `%s` answered %d", method, path, actor, resp.StatusCode)
	if out != nil && resp.StatusCode < 300 {
		c.requiref(json.Unmarshal(raw, out) == nil, "%s %s: undecodable body %s", method, path, raw)
	}
	return resp.StatusCode, raw
}

// xrCaseIfVersion: REC-02. The CAS precondition is `ifVersion` on the put
// input, and the whole point is that the SECOND writer holding a stale
// version loses instead of overwriting.
func xrCaseIfVersion(c *C) {
	var created xrRecord
	status, raw := c.do(http.MethodPut, xrTaskPath(xrCASTask),
		map[string]any{"properties": map[string]any{"name": "The concurrency probe"}}, &created)
	c.requiref(status == http.StatusCreated, "creating %s answered %d: %s", xrCASTask, status, raw)
	c.requiref(created.Version == 1, "a fresh record's version is %d, want 1", created.Version)

	// Read the version back rather than assuming it: a client's precondition
	// comes from what it last read.
	stored := xrGet(c, xrTaskPath(xrCASTask)).Version

	var won xrRecord
	status, raw = c.do(http.MethodPut, xrTaskPath(xrCASTask), map[string]any{
		"ifVersion":  stored,
		"properties": map[string]any{"description": "written by the writer holding version 1"},
	}, &won)
	c.requiref(status == http.StatusOK, "the put at version %d answered %d: %s", stored, status, raw)
	c.requiref(won.Version == stored+1, "the winning put left version %d, want %d", won.Version, stored+1)
	c.stepf("a put carrying `ifVersion: %d` landed and moved the record to version %d", stored, won.Version)

	// The same precondition a second time is the stale writer: it read
	// version 1, someone else has since written version 2.
	status, raw = c.do(http.MethodPut, xrTaskPath(xrCASTask), map[string]any{
		"ifVersion":  stored,
		"properties": map[string]any{"description": "written by the writer that never re-read"},
	}, nil)
	c.requiref(status == http.StatusConflict, "the stale put answered %d, want 409: %s", status, raw)
	p := xrRefusal(c, raw)
	c.requiref(p.Error.Code == "conflict", "the stale put's code is %q, want conflict", p.Error.Code)
	want := fmt.Sprintf("ifVersion %d, stored %d", stored, stored+1)
	c.requiref(strings.Contains(p.Error.Message, want),
		"the conflict message does not name both versions (%q): %q", want, p.Error.Message)
	c.stepf("the same `ifVersion: %d` a second time was refused: 409 `conflict`, %q", stored, p.Error.Message)

	// A refused write writes nothing: the record still reads as the winner
	// left it, version and value both.
	after := xrGet(c, xrTaskPath(xrCASTask))
	c.requiref(after.Version == stored+1 && after.prop("description") == "written by the writer holding version 1",
		"the refused put still changed the record: version %d, description %q", after.Version, after.prop("description"))
	c.stepf("the loser changed nothing: version is still %d and the description is the winner's", after.Version)
}

// xrCasePatch: REC-03. Three different things a property can be in a PATCH
// body, and only two of them are ordinary writes.
func xrCasePatch(c *C) {
	status, raw := c.do(http.MethodPut, xrTaskPath(xrPatchTask), map[string]any{
		"properties": map[string]any{
			"name":        "The patch probe",
			"description": "prose that the next patch deletes",
		},
	}, nil)
	c.requiref(status == http.StatusCreated, "creating %s answered %d: %s", xrPatchTask, status, raw)

	// A null is a DELETE of the property, not a write of an empty value: the
	// key leaves the map entirely.
	var cleared xrRecord
	status, raw = c.do(http.MethodPatch, xrTaskPath(xrPatchTask),
		map[string]any{"properties": map[string]any{"description": nil}}, &cleared)
	c.requiref(status == http.StatusOK, "the null patch answered %d: %s", status, raw)
	c.requiref(!cleared.hasProp("description"), "the null patch left description as %v", cleared.Properties["description"])
	c.requiref(!xrGet(c, xrTaskPath(xrPatchTask)).hasProp("description"),
		"description came back on the next read; a null must delete the property, not blank it")
	c.stepf("`{\"properties\":{\"description\":null}}` deleted the property: the key is gone from the record, not emptied")

	// A state property in a PATCH is a transition, and the declared
	// open -> done transition stamps completedAt without being asked.
	var done xrRecord
	status, raw = c.do(http.MethodPatch, xrTaskPath(xrPatchTask),
		map[string]any{"properties": map[string]any{"status": "done"}}, &done)
	c.requiref(status == http.StatusOK, "the open -> done transition answered %d: %s", status, raw)
	c.requiref(done.prop("status") == "done", "the status is %q after the transition", done.prop("status"))
	c.requiref(done.prop("completedAt") != "", "the open -> done transition did not stamp completedAt")
	c.stepf("patching `status` to `done` ran the declared transition and stamped completedAt=%s", done.prop("completedAt"))

	// The task kind declares four transitions: proposed -> open,
	// proposed -> abandoned, open -> done, done -> open. Nothing else exists,
	// so abandoning a finished task is refused by name.
	status, raw = c.do(http.MethodPatch, xrTaskPath(xrPatchTask),
		map[string]any{"properties": map[string]any{"status": "abandoned"}}, nil)
	c.requiref(status == http.StatusForbidden, "the undeclared transition answered %d, want 403: %s", status, raw)
	p := xrRefusal(c, raw)
	c.requiref(p.Error.Code == "guard", "the refusal's code is %q, want guard", p.Error.Code)
	c.requiref(strings.Contains(p.Error.Message, "no transition") &&
		strings.Contains(p.Error.Message, "done") && strings.Contains(p.Error.Message, "abandoned"),
		"the refusal does not name the transition it refused: %q", p.Error.Message)
	c.stepf("`done` to `abandoned` is not a declared transition and was refused: 403 `guard`, %q", p.Error.Message)
	c.requiref(xrGet(c, xrTaskPath(xrPatchTask)).prop("status") == "done",
		"the refused transition moved the record anyway")
}

// xrCaseUndeclaredProperty: REC-04. The kind's property set is closed, on
// every write door, and the refusal is addressed to the property.
func xrCaseUndeclaredProperty(c *C) {
	before := c.countRecords(tasksCollection)

	status, raw := c.do(http.MethodPost, tasksCollection,
		map[string]any{"properties": map[string]any{"bogusprop": 1}}, nil)
	c.requiref(status == http.StatusUnprocessableEntity, "the undeclared property answered %d, want 422: %s", status, raw)
	p := xrRefusal(c, raw)
	want := "props.bogusprop: not declared on " + taskKind
	c.requiref(p.Error.Code == "validation", "the refusal's code is %q, want validation", p.Error.Code)
	c.requiref(len(p.Error.Problems) == 1 && p.Error.Problems[0] == want,
		"the refusal's problems are %v, want exactly [%q]", p.Error.Problems, want)
	c.requiref(len(p.Error.ProblemDetails) == 1 && p.Error.ProblemDetails[0].Path == "props.bogusprop",
		"the problem is not addressed to props.bogusprop: %v", p.Error.ProblemDetails)
	c.stepf("a create carrying `bogusprop` was refused: 422 `validation`, problem %q at path `props.bogusprop`", want)

	// A refused create writes nothing at all, not even a row holding the
	// declared half of the body.
	c.requiref(c.countRecords(tasksCollection) == before,
		"the refused create still wrote a record: %d tasks before, %d after", before, c.countRecords(tasksCollection))
	c.stepf("nothing was written: the collection still holds the same %d tasks", before)

	// The same refusal on the patch door, so a property cannot slip in on the
	// second write of a record that was created clean.
	status, raw = c.do(http.MethodPut, xrTaskPath(xrPropTask),
		map[string]any{"properties": map[string]any{"name": "The undeclared-property probe"}}, nil)
	c.requiref(status == http.StatusCreated, "creating %s answered %d: %s", xrPropTask, status, raw)
	status, raw = c.do(http.MethodPatch, xrTaskPath(xrPropTask),
		map[string]any{"properties": map[string]any{"bogusprop": 1}}, nil)
	c.requiref(status == http.StatusUnprocessableEntity, "the undeclared property in a patch answered %d, want 422: %s", status, raw)
	c.requiref(xrRefusal(c, raw).Error.Problems[0] == want, "the patch refusal reads differently: %s", raw)
	c.requiref(!xrGet(c, xrTaskPath(xrPropTask)).hasProp("bogusprop"), "the refused patch wrote the property anyway")
	c.stepf("the patch door refuses it identically, so an existing record cannot grow the property either")
}

// xrCaseTitleDerivation: REC-05. A task declares `name` and renders its
// heading with the displayTemplate `{name|title}`, so `name` is where a
// writer puts the heading and the built-in title slot is derived storage
// (decision 0016).
func xrCaseTitleDerivation(c *C) {
	// A `title` beside the name is NOT refused as undeclared: it names the
	// built-in slot, which every kind has. It is dropped instead, silently,
	// because the kind declares a template.
	var created xrRecord
	status, raw := c.do(http.MethodPost, tasksCollection, map[string]any{
		"id": xrTitleTask,
		"properties": map[string]any{
			"name":  "The real heading",
			"title": "A written title",
		},
	}, &created)
	c.requiref(status == http.StatusCreated, "the create carrying a title answered %d, want 201: %s", status, raw)
	c.requiref(created.prop("name") == "The real heading", "the name did not land: %q", created.prop("name"))
	c.requiref(created.prop("title") == "The real heading",
		"the record's title is %q; the displayTemplate `{name|title}` must render the name", created.prop("title"))
	c.stepf("a create carrying both `name` and `title` was admitted (201) and the written title was dropped: the record's title reads `%s`", created.prop("title"))

	// The heading FOLLOWS the property, on every write, because it is
	// rendered and not stored input.
	var renamed xrRecord
	status, raw = c.do(http.MethodPut, xrTaskPath(xrTitleTask),
		map[string]any{"properties": map[string]any{"name": "A renamed heading"}}, &renamed)
	c.requiref(status == http.StatusOK, "the rename answered %d: %s", status, raw)
	c.requiref(renamed.prop("title") == "A renamed heading", "the title did not follow the name: %q", renamed.prop("title"))
	row, ok := xrListFind(c, tasksCollection+"?first=200", xrTitleTask)
	c.requiref(ok && row.prop("title") == "A renamed heading",
		"the list row carries title %q, want the derived heading", row.prop("title"))
	c.stepf("renaming the `name` moved the title with it, on the record and in the list row")

	// The `|title` half of the template is the LEGACY fallback for records
	// written before `name` existed, and a live write can never reach it: the
	// same write path that renders the template also drops the written title,
	// so a task written with a title alone ends up with no title at all.
	var titleOnly xrRecord
	status, raw = c.do(http.MethodPut, xrTaskPath(xrTitleOnly),
		map[string]any{"properties": map[string]any{"title": "Only a written title"}}, &titleOnly)
	c.requiref(status == http.StatusCreated, "the title-only create answered %d: %s", status, raw)
	c.requiref(!titleOnly.hasProp("title"),
		"the title-only record reads back title %v; a written title on a templated kind lands nowhere", titleOnly.Properties["title"])
	c.stepf("a task written with `title` and no `name` reads back with NO title: the writer's title never reaches the `{name|title}` fallback, so the heading belongs to `name`")
}

// xrCaseChosenIDs: REC-06. Who names a record, and the two words no record
// may be named.
func xrCaseChosenIDs(c *C) {
	// POST with an id in the body: a create at the writer's own key.
	var posted xrRecord
	status, raw := c.do(http.MethodPost, tasksCollection, map[string]any{
		"id":         xrChosenPost,
		"properties": map[string]any{"name": "Named by the body"},
	}, &posted)
	c.requiref(status == http.StatusCreated, "the POST carrying an id answered %d, want 201: %s", status, raw)
	c.requiref(posted.ID == xrChosenPost, "the POST landed at %q, want %q", posted.ID, xrChosenPost)

	// PUT at a path, with a DIFFERENT id in the body: the path wins and the
	// body's id is ignored, so nothing lands at the id the body named.
	var put xrRecord
	status, raw = c.do(http.MethodPut, xrTaskPath(xrChosenPut), map[string]any{
		"id":         xrChosenGhost,
		"properties": map[string]any{"name": "Named by the path"},
	}, &put)
	c.requiref(status == http.StatusCreated, "the PUT answered %d, want 201: %s", status, raw)
	c.requiref(put.ID == xrChosenPut, "the PUT landed at %q, want the path's %q", put.ID, xrChosenPut)
	status, _ = c.do(http.MethodGet, xrTaskPath(xrChosenGhost), nil, nil)
	c.requiref(status == http.StatusNotFound, "the body's id answered %d, want 404: a PUT's path is the id", status)
	c.stepf("`POST` with `{\"id\":\"%s\"}` landed at that id; `PUT %s` landed at the path's id and the body's `%s` was ignored",
		xrChosenPost, xrTaskPath(xrChosenPut), xrChosenGhost)

	// `incoming` is the one static sub-resource segment under a record, so no
	// record may take it as an id. The record path refuses both directions,
	// read and write, and writes nothing (decision 0033).
	status, raw = c.do(http.MethodPut, xrTaskPath("incoming"),
		map[string]any{"properties": map[string]any{"name": "A record named after a sub-resource"}}, nil)
	c.requiref(status == http.StatusBadRequest, "PUT at the reserved id answered %d, want 400: %s", status, raw)
	refused := xrRefusal(c, raw)
	c.requiref(refused.Error.Code == "bad_request" && strings.Contains(refused.Error.Message, "is reserved") &&
		strings.Contains(refused.Error.Message, "incoming"),
		"the refusal does not name the reserved id: %q", refused.Error.Message)
	status, raw = c.do(http.MethodDelete, xrTaskPath("incoming"), nil, nil)
	c.requiref(status == http.StatusBadRequest, "DELETE at the reserved id answered %d, want 400: %s", status, raw)
	c.stepf("`PUT` and `DELETE` at the reserved id `incoming` are both 400 `bad_request` naming the id")

	// `edges` is NOT reserved: the word names nothing on the wire any more,
	// so a record may take it as an id like any other string. Asserted here
	// rather than assumed, because a leftover reservation would refuse a
	// perfectly ordinary id forever.
	status, raw = c.do(http.MethodPut, xrTaskPath("edges"),
		map[string]any{"properties": map[string]any{"name": "An ordinary record that happens to be called edges"}}, nil)
	c.requiref(status == http.StatusCreated, "PUT at the id `edges` answered %d, want 201: %s", status, raw)
	ordinary := xrGet(c, xrTaskPath("edges"))
	c.requiref(ordinary.ID == "edges", "the record at the id `edges` reads back as %q", ordinary.ID)
	c.stepf("`edges` is an ordinary record id now: the write is a 201 and the read answers it")

	// The collection door does not pass through that check: the reservation
	// lives on the record path, and a POST names its id in the BODY. What the
	// live server does with it is pinned here either way, because a create
	// that lands at a reserved id is a row no request can read or delete.
	var reserved xrRecord
	status, raw = c.do(http.MethodPost, tasksCollection, map[string]any{
		"id":         "incoming",
		"properties": map[string]any{"name": "A record named after a sub-resource"},
	}, &reserved)
	if status >= http.StatusBadRequest {
		// The closed door must be THE refusal, not any failure: a 500 here
		// would read as the gap fixed when the server merely fell over.
		c.requiref(status == http.StatusBadRequest, "the reserved-id POST answered %d, want the 400 refusal: %s", status, raw)
		p := xrRefusal(c, raw)
		c.requiref(strings.Contains(p.Error.Message, "incoming"),
			"the collection POST refused the reserved id without naming it: %q", p.Error.Message)
		c.stepf("`POST` carrying `{\"id\":\"incoming\"}` was refused too: %d, %q", status, p.Error.Message)
		return
	}
	c.requiref(status == http.StatusCreated, "the collection POST answered %d: %s", status, raw)
	c.requiref(reserved.ID == "incoming", "the collection POST landed at %q, want incoming", reserved.ID)
	status, _ = c.do(http.MethodGet, xrTaskPath("incoming"), nil, nil)
	c.requiref(status == http.StatusMethodNotAllowed, "reading the reserved-id record answered %d, want 405", status)
	status, _ = c.do(http.MethodDelete, xrTaskPath("incoming"), nil, nil)
	c.requiref(status == http.StatusBadRequest, "deleting the reserved-id record answered %d, want 400", status)
	_, listed := xrListFind(c, tasksCollection+"?first=200", "incoming")
	c.requiref(listed, "the reserved-id record is not even in the list; where did it go?")
	c.stepf("GAP: `POST` at the collection carrying `{\"id\":\"incoming\"}` is NOT refused (201). " +
		"The row it creates appears in the list, answers 405 on a GET at its own path and 400 on a DELETE, " +
		"so it can never be read or removed by id. The reservation guards the record path only.")
}

// xrCaseLabelsAnnotations: REC-07. The two side maps, their key rule, and
// which read surface carries which.
func xrCaseLabelsAnnotations(c *C) {
	// A bare key is refused: both maps are namespaced so two writers cannot
	// silently fight over one word.
	status, raw := c.do(http.MethodPut, xrTaskPath(xrMetaTask), map[string]any{
		"properties": map[string]any{"name": "The metadata probe"},
		"labels":     map[string]any{"tier": "gold"},
	}, nil)
	c.requiref(status == http.StatusUnprocessableEntity, "the bare label key answered %d, want 422: %s", status, raw)
	p := xrRefusal(c, raw)
	c.requiref(strings.Contains(p.Error.Message, "namespaced key"),
		"the refusal does not name the key rule: %q", p.Error.Message)
	c.stepf("a label key `tier` was refused: 422 `validation`, %q", p.Error.Message)

	var written xrRecord
	status, raw = c.do(http.MethodPut, xrTaskPath(xrMetaTask), map[string]any{
		"properties":  map[string]any{"name": "The metadata probe"},
		"labels":      map[string]any{"api/tier": "gold"},
		"annotations": map[string]any{"api/note": "hello"},
	}, &written)
	c.requiref(status == http.StatusCreated, "the namespaced write answered %d: %s", status, raw)
	c.requiref(written.Labels["api/tier"] == "gold" && written.Annotations["api/note"] == "hello",
		"the write's own answer lost a key: labels %v, annotations %v", written.Labels, written.Annotations)
	c.stepf("`api/tier` and `api/note` were admitted: the writer's actor is the namespace")

	// A list row is the cheap read: labels ride along, annotations do not.
	row, ok := xrListFind(c, tasksCollection+"?first=200", xrMetaTask)
	c.requiref(ok, "the metadata record is missing from the list")
	c.requiref(row.Labels["api/tier"] == "gold", "the list row lost the label: %v", row.Labels)
	c.requiref(len(row.Annotations) == 0, "the plain list row carries annotations %v; they are the opt-in half", row.Annotations)
	c.stepf("a plain list row carries the labels and omits the annotations entirely")

	row, ok = xrListFind(c, tasksCollection+"?first=200&withAnnotations=1", xrMetaTask)
	c.requiref(ok, "the metadata record is missing from the withAnnotations list")
	c.requiref(row.Annotations["api/note"] == "hello",
		"`withAnnotations=1` did not carry the annotation: %v", row.Annotations)
	c.stepf("`withAnnotations=1` adds them back to the same row")

	got := xrGet(c, xrTaskPath(xrMetaTask))
	c.requiref(got.Labels["api/tier"] == "gold" && got.Annotations["api/note"] == "hello",
		"the single GET lost a key: labels %v, annotations %v", got.Labels, got.Annotations)
	c.stepf("the single GET carries both maps, unasked")
}

// xrCasePropertyMeta: REC-08. Provenance is per PROPERTY, not per record: two
// doors writing two properties of one record leave two managers behind.
func xrCasePropertyMeta(c *C) {
	status, raw := c.do(http.MethodPut, xrTaskPath(xrActorsTask), map[string]any{
		"properties": map[string]any{
			"name":        "The provenance probe",
			"description": "written through the unnamed door",
		},
	}, nil)
	c.requiref(status == http.StatusCreated, "creating %s answered %d: %s", xrActorsTask, status, raw)

	// The run's own writes name no door, so they are attributed to `api`. The
	// console is one of the three doors a request may claim.
	status, raw = xrDoAs(c, "console", http.MethodPut, xrTaskPath(xrActorsTask),
		map[string]any{"properties": map[string]any{"name": "The provenance probe, per the console"}}, nil)
	c.requiref(status == http.StatusOK, "the console's write answered %d: %s", status, raw)

	got := xrGet(c, xrTaskPath(xrActorsTask))
	name, hasName := got.PropertyMeta["name"]
	desc, hasDesc := got.PropertyMeta["description"]
	c.requiref(hasName && hasDesc, "propertyMeta is missing a property: %v", got.PropertyMeta)
	c.requiref(name.Manager == "console" && name.Tier == "owner",
		"`name` is managed by %q at tier %q, want console/owner", name.Manager, name.Tier)
	c.requiref(desc.Manager == "api" && desc.Tier == "owner",
		"`description` is managed by %q at tier %q, want api/owner", desc.Manager, desc.Tier)
	c.stepf("the single GET's propertyMeta splits the record by property: `name` is managed by `console`, `description` by `api`, both at the `owner` tier")

	// No alternative is recorded. An alternative is a live MAPPING offer that
	// disagrees with the stored value (the property_offers rows a sync
	// populates), so a second owner-tier write takes the property over
	// outright instead of queueing beside it.
	c.requiref(len(name.Alternatives) == 0 && len(desc.Alternatives) == 0,
		"the console's write left an alternative behind: %v; an alternative is a mapping offer, not a previous write", name.Alternatives)
	c.stepf("neither property carries an alternative: an owner write TAKES the property, and only a mapping source offers one")

	// The changelog says the same thing about the same two writes.
	rows := c.rowsFor(xrActorsTask)
	c.requiref(len(rows) == 2 && rows[0].Actor == "api" && rows[1].Actor == "console",
		"the changelog rows for %s are %v; want a put by api then a put by console", xrActorsTask, rows)
	c.stepf("the changelog agrees: seq %d put by `api`, seq %d put by `console`", rows[0].Seq, rows[1].Seq)

	// A list row never carries provenance: it is a single-record read.
	row, ok := xrListFind(c, tasksCollection+"?first=200", xrActorsTask)
	c.requiref(ok && len(row.PropertyMeta) == 0,
		"a list row carries propertyMeta %v; provenance is a single-record read", row.PropertyMeta)
	c.stepf("the list row carries no propertyMeta at all")
}

// xrCaseReferenceWrites: REF-01. There are no link verbs: a reference is an
// ordinary property, written by put and patch like every other one. What that
// costs a client is the asymmetry pinned here — a put MERGES, so omitting a
// pointer keeps it, and dropping one is an explicit null.
func xrCaseReferenceWrites(c *C) {
	status, raw := c.do(http.MethodPut, xrTaskPath(xrRefTask),
		map[string]any{"properties": map[string]any{"name": "The reference probe"}}, nil)
	c.requiref(status == http.StatusCreated, "creating %s answered %d: %s", xrRefTask, status, raw)

	// The retired verbs are gone from the router, not merely deprecated.
	status, raw = c.do(http.MethodPost, xrTaskPath(xrRefTask)+"/edges/assignee", map[string]any{"id": "sam"}, nil)
	c.requiref(status == http.StatusNotFound, "the retired link route answered %d, want 404: %s", status, raw)
	c.stepf("`POST …/edges/assignee` is a 404: the link verbs are gone, not deprecated")

	// A reference is never born dangling: `mustExist` refuses the write, at
	// the property's own site.
	status, raw = c.do(http.MethodPut, xrTaskPath(xrRefTask), map[string]any{
		"properties": map[string]any{"assignee": recPath(personKind, "x-nobody-here")},
	}, nil)
	c.requiref(status == http.StatusNotFound, "an assignee at an absent person answered %d, want 404: %s", status, raw)
	c.requiref(strings.Contains(xrRefusal(c, raw).Error.Message, "props.assignee"),
		"the refusal does not address the property: %s", raw)
	c.stepf("a reference at an absent target is a 404 addressed to `props.assignee`")

	var linked xrRecord
	status, raw = c.do(http.MethodPut, xrTaskPath(xrRefTask), map[string]any{
		"properties": map[string]any{"assignee": "sam"},
	}, &linked)
	c.requiref(status == http.StatusOK, "writing the assignee answered %d, want 200: %s", status, raw)
	c.requiref(sameSet(xrRefPaths(linked, "assignee"), recPath(personKind, "sam")),
		"the write's answer does not carry the reference: %v", linked.Properties["assignee"])
	c.requiref(linked.Version > 1, "the write left the version at %d", linked.Version)
	c.stepf("a put of `{\"assignee\": \"sam\"}` stored the canonical `%s` at version %d: a pinned reference completes the bare id",
		recPath(personKind, "sam"), linked.Version)

	// The reference reads from both ends while it is set.
	got := xrGet(c, xrTaskPath(xrRefTask))
	c.requiref(sameSet(xrRefPaths(got, "assignee"), recPath(personKind, "sam")),
		"the source's GET does not carry the reference: %v", got.Properties["assignee"])
	c.requiref(xrIncomingHas(c, xrPersonPath("sam")+"/incoming?property=assignee", xrRefTask),
		"sam's /incoming does not name %s under assignee", xrRefTask)
	c.stepf("the reference reads from both ends: on the task's GET, and on `sam`'s /incoming under `assignee`")

	// A put that says nothing about the pointer keeps it: put merges, never
	// prunes, and that is exactly why clearing needs a word of its own.
	var kept xrRecord
	status, raw = c.do(http.MethodPut, xrTaskPath(xrRefTask), map[string]any{
		"properties": map[string]any{"name": "The reference probe, renamed"},
	}, &kept)
	c.requiref(status == http.StatusOK, "the second put answered %d: %s", status, raw)
	c.requiref(sameSet(xrRefPaths(kept, "assignee"), recPath(personKind, "sam")),
		"a put that omitted `assignee` pruned it: %v", kept.Properties["assignee"])
	c.stepf("a put that omits `assignee` keeps it: a put merges and never prunes")

	// The explicit null is the drop, and it drops it at both ends.
	var cleared xrRecord
	status, raw = c.do(http.MethodPatch, xrTaskPath(xrRefTask),
		map[string]any{"properties": map[string]any{"assignee": nil}}, &cleared)
	c.requiref(status == http.StatusOK, "the clearing patch answered %d, want 200: %s", status, raw)
	c.requiref(len(xrRefPaths(cleared, "assignee")) == 0,
		"the null left the reference standing: %v", cleared.Properties["assignee"])
	c.requiref(!xrIncomingHas(c, xrPersonPath("sam")+"/incoming?property=assignee", xrRefTask),
		"sam's /incoming still names %s after the clearing patch", xrRefTask)
	c.stepf("`PATCH` with `{\"assignee\": null}` dropped the pointer from both ends")
}

// xrCaseReferenceTombstone: REF-03. A soft delete is reversible, so it may
// not cascade: undelete would otherwise return a record every pointer at it
// had been stripped from, with nothing to rebuild them from (decision 0027).
func xrCaseReferenceTombstone(c *C) {
	status, raw := c.do(http.MethodPut, xrPersonPath(xrRefVictim), map[string]any{
		"properties": map[string]any{"name": "Robin Vale", "emails": []string{"robin.vale@acme.example"}},
	}, nil)
	c.requiref(status == http.StatusCreated, "creating %s answered %d: %s", xrRefVictim, status, raw)
	var holder xrRecord
	status, raw = c.do(http.MethodPut, xrTaskPath(xrRefHolder), map[string]any{
		"properties": map[string]any{
			"name":     "A task assigned to someone about to be deleted",
			"assignee": recPath(personKind, xrRefVictim),
		},
	}, &holder)
	c.requiref(status == http.StatusCreated, "creating %s answered %d: %s", xrRefHolder, status, raw)
	held := holder.Version

	var tombstone xrRecord
	status, raw = c.do(http.MethodDelete, xrPersonPath(xrRefVictim), nil, &tombstone)
	c.requiref(status == http.StatusOK, "deleting the target answered %d: %s", status, raw)
	c.requiref(tombstone.DeletedAt != "", "the delete's answer carries no deletedAt")
	_, listed := xrListFind(c, personCollection+"?first=200", xrRefVictim)
	c.requiref(!listed, "the tombstoned person is still in the list; a tombstone leaves the fold")
	c.stepf("deleted `%s`: the tombstone carries deletedAt=%s and leaves the person list", xrRefVictim, tombstone.DeletedAt)

	// The reference is untouched, and the holder's version never moved: the
	// delete wrote the target, not the records naming it.
	after := xrGet(c, xrTaskPath(xrRefHolder))
	c.requiref(sameSet(xrRefPaths(after, "assignee"), recPath(personKind, xrRefVictim)),
		"the holder's reference did not survive the tombstone: %v", after.Properties["assignee"])
	c.requiref(after.Version == held, "the holder moved to version %d from %d; a tombstone must not write the holder", after.Version, held)
	c.stepf("the holder still spells `assignee: %s`, at the same version %d it had before the delete",
		recPath(personKind, xrRefVictim), after.Version)

	// The reverse read answers for a tombstone too, which is what lets an
	// undelete know what pointed at it.
	c.requiref(xrIncomingHas(c, xrPersonPath(xrRefVictim)+"/incoming", xrRefHolder),
		"the tombstone's /incoming lost the holder")
	c.stepf("the tombstone's own /incoming still names `%s`: the reference survives the delete at both ends, and only a purge drops it (decision 0027)", xrRefHolder)
}

// xrIncomingRow is one reverse pointer, narrowed to what these cases assert.
// There is one kind of pointer now, so the row says which PROPERTY names this
// record and where inside it, and nothing about how.
type xrIncomingRow struct {
	Property string `json:"property"`
	Path     string `json:"path"`
	From     struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	} `json:"from"`
}

type xrIncomingPage struct {
	Incoming []xrIncomingRow `json:"incoming"`
	Cursor   string          `json:"cursor"`
	Total    int             `json:"total"`
}

func xrIncomingRead(c *C, path string) xrIncomingPage {
	c.t.Helper()
	var page xrIncomingPage
	status, raw := c.do(http.MethodGet, path, nil, &page)
	c.requiref(status == http.StatusOK, "GET %s answered %d: %s", path, status, raw)
	return page
}

// xrIncomingHas reports whether a reverse read names this source record.
func xrIncomingHas(c *C, path, from string) bool {
	c.t.Helper()
	for _, row := range xrIncomingRead(c, path).Incoming {
		if row.From.ID == from {
			return true
		}
	}
	return false
}

// xrCaseIncoming: REF-04. The reverse read of a record with a wide fan-in is
// only usable if it narrows and pages, so both are pinned here against `sam`,
// who the stories left pointed at by a task, a team, an event and a
// transcript.
func xrCaseIncoming(c *C) {
	all := xrIncomingRead(c, xrPersonPath("sam")+"/incoming")
	c.requiref(all.Total >= 4, "sam has %d incoming rows; the stories left at least four", all.Total)
	props := map[string]bool{}
	for _, row := range all.Incoming {
		props[row.Property] = true
		c.requiref(row.Property != "", "an incoming row does not name the property that points here: %+v", row)
	}
	c.requiref(props["assignee"] && props["members"], "sam's incoming properties are %v; want at least assignee and members", props)
	c.stepf("`sam`'s unnarrowed /incoming answers %d rows across %d reference properties", all.Total, len(props))

	// `property` narrows to one reference property.
	byProp := xrIncomingRead(c, xrPersonPath("sam")+"/incoming?property=assignee")
	c.requiref(len(byProp.Incoming) > 0, "?property=assignee answered nothing")
	c.requiref(byProp.Total <= all.Total, "the narrowed total %d exceeds the whole fan-in %d", byProp.Total, all.Total)
	found := false
	for _, row := range byProp.Incoming {
		c.requiref(row.Property == "assignee", "?property=assignee answered a %q row from %s", row.Property, row.From.ID)
		found = found || row.From.ID == "task-invite-flow"
	}
	c.requiref(found, "?property=assignee lost the task the stories assigned to sam")
	c.stepf("`?property=assignee` narrowed %d rows to %d, all of them assignee rows, `task-invite-flow` among them", all.Total, byProp.Total)

	// `fromKind` narrows to one source kind, by full identity.
	byKind := xrIncomingRead(c, xrPersonPath("sam")+"/incoming?fromKind="+url.QueryEscape(teamKind))
	c.requiref(len(byKind.Incoming) > 0, "?fromKind=%s answered nothing", teamKind)
	fromTeam := false
	for _, row := range byKind.Incoming {
		c.requiref(row.From.Kind == teamKind, "?fromKind answered a row from %s", row.From.Kind)
		fromTeam = fromTeam || row.From.ID == "product"
	}
	c.requiref(fromTeam, "?fromKind lost the team sam leads")
	c.stepf("`?fromKind=%s` answered %d rows, every one of them from a team, `product` among them", teamKind, byKind.Total)

	// The keyset page walks the fan-in one row at a time, and the cursor
	// picks up at the next DISTINCT row rather than repeating the last one.
	first := xrIncomingRead(c, xrPersonPath("sam")+"/incoming?first=1")
	c.requiref(len(first.Incoming) == 1, "?first=1 answered %d rows", len(first.Incoming))
	c.requiref(first.Cursor != "", "?first=1 answered no cursor with more rows to come")
	c.requiref(first.Total == all.Total, "the paged total is %d, want the whole fan-in %d", first.Total, all.Total)
	next := xrIncomingRead(c, xrPersonPath("sam")+"/incoming?first=1&after="+url.QueryEscape(first.Cursor))
	c.requiref(len(next.Incoming) == 1, "the second page answered %d rows", len(next.Incoming))
	c.requiref(next.Incoming[0] != first.Incoming[0], "the second page repeated the first row: %+v", next.Incoming[0])
	c.stepf("`?first=1` answered one row and a cursor; the cursor answered the next distinct row, with the total still %d", next.Total)

	// The list grammar does not apply to a reverse read, and a silently
	// ignored parameter would return the unfiltered fan-in looking filtered.
	status, raw := c.do(http.MethodGet, xrPersonPath("sam")+"/incoming?orderBy=createdAt", nil, nil)
	c.requiref(status == http.StatusBadRequest, "orderBy on /incoming answered %d, want 400: %s", status, raw)
	c.requiref(strings.Contains(xrRefusal(c, raw).Error.Message, "orderBy is not supported on incoming"),
		"the refusal does not name the parameter: %s", raw)

	// The retired spelling of the narrowing parameter is refused too, rather
	// than ignored into an unnarrowed answer that looks narrowed.
	status, raw = c.do(http.MethodGet, xrPersonPath("sam")+"/incoming?rel=assignee", nil, nil)
	c.requiref(status == http.StatusBadRequest, "the retired `rel` parameter answered %d, want 400: %s", status, raw)
	c.requiref(strings.Contains(xrRefusal(c, raw).Error.Message, `rel`),
		"the refusal of `rel` does not name it: %s", raw)
	c.stepf("`?orderBy=createdAt` and the retired `?rel=` are both 400 naming the parameter: the reverse read takes first, after, property and fromKind, and nothing else")
}

// xrSeedDuplicates writes the two people a merge folds and the task pointing
// at the loser. It is called by MRG-01; MRG-02 reads what it left.
func xrSeedDuplicates(c *C) {
	c.t.Helper()
	for id, name := range map[string]string{xrDupWinner: "Alex Ferro", xrDupLoser: "A. Ferro"} {
		status, raw := c.do(http.MethodPut, xrPersonPath(id), map[string]any{
			"properties": map[string]any{"name": name, "emails": []string{"alex.ferro@acme.example"}},
		}, nil)
		c.requiref(status == http.StatusCreated || status == http.StatusOK,
			"seeding %s answered %d: %s", id, status, raw)
	}
	status, raw := c.do(http.MethodPut, xrTaskPath(xrDupTask), map[string]any{
		"properties": map[string]any{
			"name":     "A task assigned to the duplicate",
			"assignee": recPath(personKind, xrDupLoser),
		},
	}, nil)
	c.requiref(status == http.StatusCreated || status == http.StatusOK,
		"seeding %s answered %d: %s", xrDupTask, status, raw)
}

// xrFindMerge finds the live merge record joining the two duplicates. MRG-02
// reads it out of the collection rather than out of MRG-01's memory, so a
// failed MRG-01 fails MRG-02 with a message about what is missing.
func xrFindMerge(c *C) xrRecord {
	c.t.Helper()
	var page struct {
		Records []xrRecord `json:"records"`
	}
	status, raw := c.do(http.MethodGet, xrMergeCollection+"?first=200", nil, &page)
	c.requiref(status == http.StatusOK, "listing the merges answered %d: %s", status, raw)
	for _, rec := range page.Records {
		if sameSet(xrRefPaths(rec, "winner"), recPath(personKind, xrDupWinner)) &&
			sameSet(xrRefPaths(rec, "loser"), recPath(personKind, xrDupLoser)) {
			return rec
		}
	}
	c.requiref(false, "no live merge of %s into %s is in the collection", xrDupLoser, xrDupWinner)
	return xrRecord{}
}

// xrCaseMerge: MRG-01. Identity is the (kind, id) pair, so a merge names the
// kind beside the two ids, and the loser's id keeps resolving afterwards.
func xrCaseMerge(c *C) {
	xrSeedDuplicates(c)

	// The kind is not optional: two bare ids do not address two records.
	status, raw := c.do(http.MethodPost, "/api/v1/merge",
		map[string]any{"winner": xrDupWinner, "loser": xrDupLoser}, nil)
	c.requiref(status == http.StatusUnprocessableEntity, "a merge without a kind answered %d, want 422: %s", status, raw)
	c.requiref(strings.Contains(xrRefusal(c, raw).Error.Message, "kind is required"),
		"the refusal does not name the missing kind: %s", raw)

	var merge xrRecord
	status, raw = c.do(http.MethodPost, "/api/v1/merge", map[string]any{
		"kind": personKind, "winner": xrDupWinner, "loser": xrDupLoser,
	}, &merge)
	c.requiref(status == http.StatusCreated, "the merge answered %d, want 201: %s", status, raw)
	c.requiref(merge.Kind == xrMergeKind, "the merge answered a %s, want a %s", merge.Kind, xrMergeKind)
	c.requiref(sameSet(xrRefPaths(merge, "winner"), recPath(personKind, xrDupWinner)) &&
		sameSet(xrRefPaths(merge, "loser"), recPath(personKind, xrDupLoser)),
		"the merge record's ends are winner %v, loser %v", xrRefPaths(merge, "winner"), xrRefPaths(merge, "loser"))
	c.stepf("`POST /api/v1/merge` wrote merge record `%s`: the command IS a record, naming its two ends in the `winner` and `loser` references", merge.ID)

	// The loser's id keeps resolving, and says what it resolved to.
	loser := xrGet(c, xrPersonPath(xrDupLoser))
	c.requiref(loser.ID == xrDupWinner && loser.CanonicalID == xrDupWinner,
		"reading the loser's id answered record %q with canonicalId %q, want the winner", loser.ID, loser.CanonicalID)
	c.requiref(sameSet(loser.FormerIDs, xrDupLoser), "the answer's formerIds are %v, want [%s]", loser.FormerIDs, xrDupLoser)
	winner := xrGet(c, xrPersonPath(xrDupWinner))
	c.requiref(winner.CanonicalID == "", "the winner answers canonicalId %q; it IS the canonical record", winner.CanonicalID)
	c.requiref(sameSet(winner.FormerIDs, xrDupLoser), "the winner's formerIds are %v, want [%s]", winner.FormerIDs, xrDupLoser)
	c.stepf("`GET` at the loser's id answers the winner, with `canonicalId: %s` and `formerIds: [%s]`; the winner carries the same formerId and no canonicalId",
		xrDupWinner, xrDupLoser)

	// NOTHING REPOINTS. The task's `assignee` is a value in the task's own
	// properties and the merge never touched it, so it still spells the
	// loser — and it resolves to the winner because the reverse read and the
	// record read both follow the former-id trail.
	task := xrGet(c, xrTaskPath(xrDupTask))
	c.requiref(sameSet(xrRefPaths(task, "assignee"), recPath(personKind, xrDupLoser)),
		"the task's assignee is %v; the merge must not rewrite a value in another record", xrRefPaths(task, "assignee"))
	c.requiref(xrIncomingHas(c, xrPersonPath(xrDupWinner)+"/incoming?property=assignee", xrDupTask),
		"the winner's /incoming does not name the task pointing at its former id")
	c.stepf("the task still spells `assignee: %s` (a merge never rewrites another record), and the winner's /incoming finds it through the former id",
		recPath(personKind, xrDupLoser))
}

// xrCaseSplit: MRG-02. The split undoes exactly what the merge moved, which
// is why the merge record keeps the list of moves.
func xrCaseSplit(c *C) {
	merge := xrFindMerge(c)

	var split xrRecord
	status, raw := c.do(http.MethodPost, "/api/v1/split", map[string]any{"merge": merge.ID}, &split)
	c.requiref(status == http.StatusCreated, "the split answered %d, want 201: %s", status, raw)
	c.requiref(split.Kind == xrSplitKind, "the split answered a %s, want a %s", split.Kind, xrSplitKind)
	c.requiref(sameSet(xrRefPaths(split, "merge"), recPath(xrMergeKind, merge.ID)),
		"the split record does not name the merge it reversed: %v", xrRefPaths(split, "merge"))
	c.stepf("`POST /api/v1/split` wrote split record `%s`, pointing at the merge `%s` it reversed", split.ID, merge.ID)

	// The loser is a record again: its own id, its own name, no canonical
	// pointer anywhere.
	loser := xrGet(c, xrPersonPath(xrDupLoser))
	c.requiref(loser.ID == xrDupLoser && loser.CanonicalID == "",
		"the loser answers record %q with canonicalId %q, want itself and none", loser.ID, loser.CanonicalID)
	c.requiref(loser.prop("name") == "A. Ferro", "the restored record's name is %q", loser.prop("name"))
	winner := xrGet(c, xrPersonPath(xrDupWinner))
	c.requiref(len(winner.FormerIDs) == 0, "the winner still carries formerIds %v after the split", winner.FormerIDs)
	c.stepf("both people are their own records again: `%s` resolves to itself and `%s` carries no formerId", xrDupLoser, xrDupWinner)

	// The task's pointer never moved, so the split has nothing to put back:
	// the value it always held names a live record again.
	task := xrGet(c, xrTaskPath(xrDupTask))
	c.requiref(sameSet(xrRefPaths(task, "assignee"), recPath(personKind, xrDupLoser)),
		"the task's assignee is %v after the split, want the loser it always spelled", xrRefPaths(task, "assignee"))
	c.requiref(xrIncomingHas(c, xrPersonPath(xrDupLoser)+"/incoming?property=assignee", xrDupTask),
		"the restored loser's /incoming does not name the task")
	c.stepf("the task's `assignee` never moved and now names a live record again: `%s`", xrDupLoser)

	// The merge record is spent, not erased: it stays addressable as a
	// tombstone, so the trail of what happened survives the reversal.
	spent := xrGet(c, xrMergeCollection+"/"+url.PathEscape(merge.ID))
	c.requiref(spent.DeletedAt != "", "the reversed merge record carries no deletedAt")
	_, listed := xrListFind(c, xrMergeCollection+"?first=200", merge.ID)
	c.requiref(!listed, "the reversed merge is still in the merge list")
	c.stepf("the merge record itself is tombstoned (deletedAt=%s): it leaves the list and stays readable by id", spent.DeletedAt)
}
