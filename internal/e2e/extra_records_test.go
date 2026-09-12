package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// The record, reference, merge and split cases: CASES.md REC-03, REC-04,
// REC-06, REC-07, REC-08, REF-01, REF-03, REF-04, MRG-01 and MRG-02. They run
// after the stories, over the repository the stories built, and every record
// they write or delete carries the `x-` id prefix so the acme world stays as
// it was left.
// The one exception is REC-06, which has to write the ids that were once
// reserved; it says so where it does.
func init() {
	registerCase(210, "REC-03", "What a PATCH can do to a property",
		"A null deletes the property outright, a state property is a transition that stamps its own time, and a "+
			"transition the kind does not declare is a 403 `guard` naming the pair it refused.",
		xrCasePatch)
	registerCase(220, "REC-04", "An undeclared property is refused naming it",
		"A create and a patch that carry a property the kind never declared both answer 422 `validation` with the "+
			"problem addressed to `props.bogusprop`, and no record is written.",
		xrCaseUndeclaredProperty)
	registerCase(240, "REC-06", "Who names a record: PUT's path does, POST never does",
		"A PUT lands at the path's id whatever the body says, and no id is reserved: the words that once named "+
			"sub-resources are ordinary ids in both directions. `POST /api/v1/records` creates under a server-assigned id "+
			"and refuses a body carrying one, 422 naming the PUT at the record path, writing nothing.",
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
			"path, the tombstone's reverse read (`filter.referencing`) still lists the holder, and the holder's "+
			"version never moves (decision 0027).",
		xrCaseReferenceTombstone)
	registerCase(285, "REF-04", "The reverse read narrows and pages like any list",
		"`filter.referencing` lists the records pointing at one target and `matches` names the property each "+
			"points from; `referencing.property` narrows to one property, `kinds` beside it to one source kind, "+
			"`first`+`after` walk the fan-in one distinct record at a time, `orderBy` is admitted, and a parameter "+
			"the route does not take (`rel`) is a 400 naming it.",
		xrCaseReferencing)
	registerCase(290, "MRG-01", "Merge folds two records into one",
		"`POST /api/v1/merge` writes a `recordmerge` record; the loser's id keeps resolving, now answering the "+
			"canonical record with the loser as a formerId. Nothing repoints: the task's `assignee` still spells "+
			"the loser, and the winner's reverse read finds it through the former id.",
		xrCaseMerge)
	registerCase(295, "MRG-02", "Split reverses the merge",
		"`POST /api/v1/split` gives the loser its record back: no canonicalId, no formerId on the winner, the "+
			"task's unmoved `assignee` naming a live record again, and the merge record itself tombstoned.",
		xrCaseSplit)
}

// The ids this group writes. Everything is `x-` prefixed: the stories own
// every other id in the repository.
const (
	xrPatchTask   = "x-rec-patch"
	xrPropTask    = "x-rec-prop"
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

// xrListFind reads one list page and returns the row with this id, plus
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

	status, raw := c.do(http.MethodPost, recordsRoute, map[string]any{
		"kind":       kindOf(tasksCollection),
		"properties": map[string]any{"bogusprop": 1},
	}, nil)
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
	c.stepf("nothing was written: the task list still holds the same %d records", before)

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

// xrCaseChosenIDs: REC-06. Who names a record. A PUT's path does, a POST's
// body never does, and no word is held back from being an id.
func xrCaseChosenIDs(c *C) {
	// PUT at a path, with a DIFFERENT id in the body: the path wins and the
	// body's id is ignored, so nothing lands at the id the body named.
	var put xrRecord
	status, raw := c.do(http.MethodPut, xrTaskPath(xrChosenPut), map[string]any{
		"id":         xrChosenGhost,
		"properties": map[string]any{"name": "Named by the path"},
	}, &put)
	c.requiref(status == http.StatusCreated, "the PUT answered %d, want 201: %s", status, raw)
	c.requiref(put.ID == xrChosenPut, "the PUT landed at %q, want the path's %q", put.ID, xrChosenPut)
	status, _ = c.do(http.MethodGet, xrTaskPath(xrChosenGhost), nil, nil)
	c.requiref(status == http.StatusNotFound, "the body's id answered %d, want 404: a PUT's path is the id", status)
	c.stepf("`PUT %s` landed at the path's id and the body's `%s` was ignored", xrTaskPath(xrChosenPut), xrChosenGhost)

	// Nothing is reserved. The two words that once named static sub-resources
	// under a record name nothing on the wire now, so each is an ordinary id
	// in both directions, write and read. Asserted rather than assumed, because
	// a leftover reservation would refuse a perfectly ordinary id forever.
	for _, id := range []string{"incoming", "edges"} {
		status, raw = c.do(http.MethodPut, xrTaskPath(id),
			map[string]any{"properties": map[string]any{"name": "An ordinary record that happens to be called " + id}}, nil)
		c.requiref(status == http.StatusCreated, "PUT at the id %q answered %d, want 201: %s", id, status, raw)
		ordinary := xrGet(c, xrTaskPath(id))
		c.requiref(ordinary.ID == id, "the record at the id %q reads back as %q", id, ordinary.ID)
		_, listed := xrListFind(c, listOf(tasksCollection, "first=200"), id)
		c.requiref(listed, "the record at the id %q is missing from the task list", id)
	}
	var gone xrRecord
	status, raw = c.do(http.MethodDelete, xrTaskPath("incoming"), nil, &gone)
	c.requiref(status == http.StatusOK, "DELETE at the id `incoming` answered %d, want 200: %s", status, raw)
	c.requiref(gone.DeletedAt != "", "the delete's answer carries no deletedAt")
	c.stepf("`incoming` and `edges` are ordinary record ids: each write is a 201, each read answers it, and `DELETE` tombstones `incoming` like any record")

	// POST never takes an id: the body-addressed create lands under a
	// server-assigned id, and a body carrying one is refused, pointing at the
	// PUT that writes the record it named. Nothing is written by the refusal.
	before := c.countRecords(tasksCollection)
	status, raw = c.do(http.MethodPost, recordsRoute, map[string]any{
		"kind":       kindOf(tasksCollection),
		"id":         "incoming",
		"properties": map[string]any{"name": "A create that tries to choose its id"},
	}, nil)
	c.requiref(status == http.StatusUnprocessableEntity, "the POST carrying an id answered %d, want 422: %s", status, raw)
	p := xrRefusal(c, raw)
	wantPut := "PUT " + xrTaskPath("incoming")
	c.requiref(p.Error.Code == "validation" && strings.Contains(p.Error.Message, wantPut),
		"the refusal does not name the PUT at the record path %q: %q", wantPut, p.Error.Message)
	c.requiref(c.countRecords(tasksCollection) == before,
		"the refused POST wrote a record: %d tasks before, %d after", before, c.countRecords(tasksCollection))
	c.stepf("`POST %s` carrying `{\"id\":\"incoming\"}` is 422 `validation` naming `%s`, and the task list is unchanged", recordsRoute, wantPut)
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
	row, ok := xrListFind(c, listOf(tasksCollection, "first=200"), xrMetaTask)
	c.requiref(ok, "the metadata record is missing from the list")
	c.requiref(row.Labels["api/tier"] == "gold", "the list row lost the label: %v", row.Labels)
	c.requiref(len(row.Annotations) == 0, "the plain list row carries annotations %v; they are the opt-in half", row.Annotations)
	c.stepf("a plain list row carries the labels and omits the annotations entirely")

	row, ok = xrListFind(c, listOf(tasksCollection, "first=200", "withAnnotations=1"), xrMetaTask)
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
	row, ok := xrListFind(c, listOf(tasksCollection, "first=200"), xrActorsTask)
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
	c.requiref(xrPointsAt(c, personKind, "sam", "assignee", xrRefTask),
		"sam's reverse read does not list %s under assignee", xrRefTask)
	c.stepf("the reference reads from both ends: on the task's GET, and on `sam`'s reverse read (`filter.referencing`) under `assignee`")

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
	c.requiref(!xrPointsAt(c, personKind, "sam", "assignee", xrRefTask),
		"sam's reverse read still lists %s after the clearing patch", xrRefTask)
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
	_, listed := xrListFind(c, listOf(personCollection, "first=200"), xrRefVictim)
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
	c.requiref(xrPointsAt(c, personKind, xrRefVictim, "", xrRefHolder),
		"the tombstone's reverse read lost the holder")
	c.stepf("the tombstone's own reverse read still lists `%s`: the reference survives the delete at both ends, and only a purge drops it (decision 0027)", xrRefHolder)
}

// xrReferencingRead is one page of the reverse read. `matches` rides beside
// the records because one source can point at the target from two sites, and
// a page of distinct records cannot say which properties matched on its own.
func xrReferencingRead(c *C, path string) recordsPage {
	c.t.Helper()
	var page recordsPage
	status, raw := c.do(http.MethodGet, path, nil, &page)
	c.requiref(status == http.StatusOK, "GET %s answered %d: %s", path, status, raw)
	return page
}

// xrPointsAt reports whether the reverse read of (kind, id), narrowed to
// `property` when it is set, lists the record fromID.
func xrPointsAt(c *C, kind, id, property, fromID string) bool {
	c.t.Helper()
	for _, rec := range xrReferencingRead(c, referencingList(kind, id, property, "first=200")).Records {
		if rec.ID == fromID {
			return true
		}
	}
	return false
}

// xrMatchProperties is the set of properties `matches` names for the page's
// records, and the ids the target is pointed at from. Every listed record must
// have at least one match: a record on the reverse read page that `matches`
// cannot explain is a bug in the page, not the test.
func xrMatchProperties(c *C, page recordsPage) (props, from map[string]bool) {
	c.t.Helper()
	props, from = map[string]bool{}, map[string]bool{}
	for _, rec := range page.Records {
		sites := page.Matches[recPath(rec.Kind, rec.ID)]
		c.requiref(len(sites) > 0, "the reverse read lists %s/%s but `matches` has no site for it: %v", rec.Kind, rec.ID, page.Matches)
		from[rec.ID] = true
		for _, site := range sites {
			c.requiref(site.Property != "", "a match site for %s does not name the property that points here: %+v", rec.ID, site)
			props[site.Property] = true
		}
	}
	return props, from
}

// xrCaseReferencing: REF-04. The reverse read of a record with a wide fan-in
// is only usable if it narrows and pages, so both are pinned here against
// `sam`, who the stories left pointed at by a task, a team, an event and a
// transcript. It is an ordinary list with one more filter arm, so everything
// a list takes, it takes.
func xrCaseReferencing(c *C) {
	all := xrReferencingRead(c, referencingList(personKind, "sam", "", "first=200"))
	c.requiref(len(all.Records) >= 4, "sam is pointed at by %d records; the stories left at least four", len(all.Records))
	props, _ := xrMatchProperties(c, all)
	c.requiref(props["assignee"] && props["members"], "sam is pointed at from %v; want at least assignee and members", props)
	c.stepf("`sam`'s unnarrowed reverse read answers %d records, and `matches` names %d reference properties among them", len(all.Records), len(props))

	// `referencing.property` narrows to one reference property, and the
	// narrowing shows in `matches` too: no site from another property rides
	// along on a record that also matched the named one.
	byProp := xrReferencingRead(c, referencingList(personKind, "sam", "assignee", "first=200"))
	c.requiref(len(byProp.Records) > 0, "referencing.property=assignee answered nothing")
	c.requiref(len(byProp.Records) <= len(all.Records), "the narrowed page %d exceeds the whole fan-in %d", len(byProp.Records), len(all.Records))
	narrowed, from := xrMatchProperties(c, byProp)
	c.requiref(len(narrowed) == 1 && narrowed["assignee"], "referencing.property=assignee answered sites from %v", narrowed)
	c.requiref(from["task-invite-flow"], "referencing.property=assignee lost the task the stories assigned to sam")
	c.stepf("`referencing.property=assignee` narrowed %d records to %d, every match an assignee site, `task-invite-flow` among them", len(all.Records), len(byProp.Records))

	// The source kind is narrowed the way any list narrows: `kinds` beside
	// `referencing` in the same filter.
	byKind := xrReferencingRead(c, listWhere(map[string]any{
		"kinds":       []string{teamKind},
		"referencing": map[string]any{"ref": recPath(personKind, "sam")},
	}, "first=200"))
	c.requiref(len(byKind.Records) > 0, "kinds=[%s] beside referencing answered nothing", teamKind)
	fromTeam := false
	for _, rec := range byKind.Records {
		c.requiref(rec.Kind == teamKind, "kinds=[%s] answered a record of kind %s", teamKind, rec.Kind)
		fromTeam = fromTeam || rec.ID == "product"
	}
	c.requiref(fromTeam, "kinds=[%s] beside referencing lost the team sam leads", teamKind)
	c.stepf("`{\"kinds\":[%q],\"referencing\":…}` answered %d records, every one a team, `product` among them", teamKind, len(byKind.Records))

	// The keyset page walks the fan-in one record at a time, and the cursor
	// picks up at the next DISTINCT record rather than repeating the last one.
	first := xrReferencingRead(c, referencingList(personKind, "sam", "", "first=1"))
	c.requiref(len(first.Records) == 1, "first=1 answered %d records", len(first.Records))
	c.requiref(first.Cursor != "", "first=1 answered no cursor with more records to come")
	next := xrReferencingRead(c, referencingList(personKind, "sam", "", "first=1", "after="+url.QueryEscape(first.Cursor)))
	c.requiref(len(next.Records) == 1, "the second page answered %d records", len(next.Records))
	c.requiref(next.Records[0].ID != first.Records[0].ID || next.Records[0].Kind != first.Records[0].Kind,
		"the second page repeated the first record: %s/%s", next.Records[0].Kind, next.Records[0].ID)
	c.stepf("`first=1` answered one record and a cursor; `after` answered the next distinct record")

	// The list grammar applies whole: a reverse read orders like any list.
	ordered := xrReferencingRead(c, referencingList(personKind, "sam", "", "first=200", "orderBy=createdAt"))
	c.requiref(len(ordered.Records) == len(all.Records), "orderBy=createdAt answered %d records, want the same %d", len(ordered.Records), len(all.Records))

	// A parameter the route does not take is refused, never ignored into an
	// unnarrowed answer that looks narrowed.
	status, raw := c.do(http.MethodGet, referencingList(personKind, "sam", "", "rel=assignee"), nil, nil)
	c.requiref(status == http.StatusBadRequest, "the `rel` parameter answered %d, want 400: %s", status, raw)
	refused := xrRefusal(c, raw)
	c.requiref(refused.Error.Code == "bad_request" && strings.Contains(refused.Error.Message, `unknown query parameter "rel"`),
		"the refusal of `rel` does not name it: %s", raw)
	c.stepf("`orderBy=createdAt` is admitted (200, the same %d records); `rel=` is 400 `bad_request`, %q", len(ordered.Records), refused.Error.Message)
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
// reads it out of the merge list rather than out of MRG-01's memory, so a
// failed MRG-01 fails MRG-02 with a message about what is missing.
func xrFindMerge(c *C) xrRecord {
	c.t.Helper()
	var page struct {
		Records []xrRecord `json:"records"`
	}
	status, raw := c.do(http.MethodGet, listOf(xrMergeCollection, "first=200"), nil, &page)
	c.requiref(status == http.StatusOK, "listing the merges answered %d: %s", status, raw)
	for _, rec := range page.Records {
		if sameSet(xrRefPaths(rec, "winner"), recPath(personKind, xrDupWinner)) &&
			sameSet(xrRefPaths(rec, "loser"), recPath(personKind, xrDupLoser)) {
			return rec
		}
	}
	c.requiref(false, "no live merge of %s into %s is in the merge list", xrDupLoser, xrDupWinner)
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
	c.requiref(xrPointsAt(c, kindOf(personCollection), xrDupWinner, "assignee", xrDupTask),
		"the winner's reverse read does not list the task pointing at its former id")
	c.stepf("the task still spells `assignee: %s` (a merge never rewrites another record), and the winner's reverse read finds it through the former id",
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
	c.requiref(xrPointsAt(c, kindOf(personCollection), xrDupLoser, "assignee", xrDupTask),
		"the restored loser's reverse read does not list the task")
	c.stepf("the task's `assignee` never moved and now names a live record again: `%s`", xrDupLoser)

	// The merge record is spent, not erased: it stays addressable as a
	// tombstone, so the trail of what happened survives the reversal.
	spent := xrGet(c, xrMergeCollection+"/"+url.PathEscape(merge.ID))
	c.requiref(spent.DeletedAt != "", "the reversed merge record carries no deletedAt")
	_, listed := xrListFind(c, listOf(xrMergeCollection, "first=200"), merge.ID)
	c.requiref(!listed, "the reversed merge is still in the merge list")
	c.stepf("the merge record itself is tombstoned (deletedAt=%s): it leaves the list and stays readable by id", spent.DeletedAt)
}
