package applet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

const (
	bookKind   = "ada.example.com/library/book"
	authorKind = "ada.example.com/library/author"
	searchFn   = "ada.example.com/library/searchbooks"
)

type fakeSchema map[string]KindShape

func (s fakeSchema) Kind(ref string) (KindShape, bool) { k, ok := s[ref]; return k, ok }
func (s fakeSchema) Function(ref string) bool          { return ref == searchFn }

var library = fakeSchema{
	bookKind: {Props: map[string]PropShape{
		"title":      {Type: "string"},
		"author":     {Type: "reference", Target: authorKind},
		"status":     {Type: "enum", Values: []string{"want", "reading", "read", "abandoned"}},
		"rating":     {Type: "int"},
		"isbn":       {Type: "string"},
		"coverUrl":   {Type: "url"},
		"year":       {Type: "int"},
		"finishedAt": {Type: "date"},
		"notes":      {Type: "markdown"},
	}},
	authorKind: {Props: map[string]PropShape{"name": {Type: "string"}}},
}

// fakeData is a repository in memory, as much of one as the runtime reads.
type fakeData struct {
	records map[string]*substrate.Record
	seq     int
	calls   []any
	refuse  error
	fail    error
	keys    []string
}

func newFakeData() *fakeData { return &fakeData{records: map[string]*substrate.Record{}} }

func (f *fakeData) add(kind, id string, props map[string]any) *substrate.Record {
	r := &substrate.Record{ID: id, Kind: kind, Version: 1, Properties: props}
	if t, ok := props["title"].(string); ok {
		r.Title = t
	} else if n, ok := props["name"].(string); ok {
		r.Title = n
	}
	f.records[kind+"/"+id] = r
	return r
}

func (f *fakeData) of(kind string) []*substrate.Record {
	var out []*substrate.Record
	for _, r := range f.records {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b *substrate.Record) int { return strings.Compare(a.Title, b.Title) })
	return out
}

func (f *fakeData) List(_ context.Context, q substrate.Query) (*substrate.Page, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	page := &substrate.Page{Included: map[string]*substrate.Record{}}
	for _, r := range f.of(q.Filter.Kinds[0]) {
		if len(q.Filter.IDs) > 0 && !slices.Contains(q.Filter.IDs, r.ID) {
			continue
		}
		if q.Filter.Search != "" && !strings.Contains(strings.ToLower(r.Title), strings.ToLower(q.Filter.Search)) {
			continue
		}
		if !matches(r, q.Filter.Properties) {
			continue
		}
		page.Records = append(page.Records, r)
		for _, name := range q.Expand {
			if p := refPath(r.Properties[name]); p != "" && f.records[p] != nil {
				page.Included[p] = f.records[p]
			}
		}
		if q.First > 0 && len(page.Records) == q.First {
			break
		}
	}
	return page, nil
}

func matches(r *substrate.Record, conds map[string]substrate.Cond) bool {
	for prop, c := range conds {
		v := r.Properties[prop]
		if c.Eq != nil && v != c.Eq {
			return false
		}
		if c.In != nil && !slices.Contains(c.In, v) {
			return false
		}
	}
	return true
}

func (f *fakeData) Put(ctx context.Context, _ substrate.Actor, in substrate.PutInput) (*substrate.Record, error) {
	f.keys = append(f.keys, substrate.IdempotencyKeyFrom(ctx))
	if f.refuse != nil {
		return nil, f.refuse
	}
	f.seq++
	props := map[string]any{}
	for k, v := range in.Properties {
		if s, ok := v.(string); ok && strings.Count(s, "/") >= 3 {
			v = map[string]any{"ref": s}
		}
		props[k] = v
	}
	return f.add(in.Kind, fmt.Sprintf("r%d", f.seq), props), nil
}

func (f *fakeData) Patch(_ context.Context, _ substrate.Actor, typ, id string, in substrate.PatchInput) (*substrate.Record, error) {
	r := f.records[typ+"/"+id]
	if r == nil {
		return nil, substrate.ErrNotFound
	}
	for k, v := range in.Properties {
		r.Properties[k] = v
	}
	r.Version++
	return r, nil
}

func (f *fakeData) Delete(_ context.Context, _ substrate.Actor, typ, id string, _ substrate.DeleteInput) (*substrate.Record, error) {
	r := f.records[typ+"/"+id]
	if r == nil {
		return nil, substrate.ErrNotFound
	}
	delete(f.records, typ+"/"+id)
	return r, nil
}

// KindByRef answers the library's kinds as the API's kind reads do, the
// status enum with a label on two of its values.
func (f *fakeData) Window(_ context.Context, q substrate.WindowQuery) (*substrate.WindowPage, error) {
	page := &substrate.WindowPage{}
	for _, r := range f.of(q.Filter.Kinds[0]) {
		if _, repeats := r.Properties["recurrence"]; repeats {
			page.Series = append(page.Series, r)
		}
	}
	return page, nil
}

func (f *fakeData) TypesImplementing(context.Context, string) ([]substrate.KindInfo, error) {
	return nil, nil
}

func (f *fakeData) KindByRef(_ context.Context, ref string) (substrate.KindInfo, error) {
	k, ok := library[ref]
	if !ok {
		return substrate.KindInfo{}, substrate.ErrNotFound
	}
	labels := map[string]string{"want": "Want to read", "read": "Read"}
	props := map[string]any{}
	for name, p := range k.Props {
		m := map[string]any{"type": p.Type}
		if len(p.Values) > 0 {
			var values []any
			for _, v := range p.Values {
				values = append(values, map[string]any{"value": v, "label": labels[v]})
			}
			m["values"] = values
		}
		props[name] = m
	}
	return substrate.KindInfo{Identity: ref, Definition: map[string]any{"properties": props}}, nil
}

func (f *fakeData) CallFunction(_ context.Context, _ substrate.Actor, name string, args any) (any, int, error) {
	f.calls = append(f.calls, args)
	if name != searchFn {
		return nil, 0, substrate.ErrNotFound
	}
	return map[string]any{"results": []map[string]any{
		{"title": "The Left Hand of Darkness", "author": "Ursula K. Le Guin", "year": 1969, "isbn": "0441478123", "coverUrl": "https://covers.example.com/1.jpg"},
	}}, 0, nil
}

func booksApplet(t *testing.T) *substrate.Record {
	t.Helper()
	raw, err := os.ReadFile("testdata/books.json")
	if err != nil {
		t.Fatal(err)
	}
	var props map[string]any
	if err := json.Unmarshal(raw, &props); err != nil {
		t.Fatal(err)
	}
	return &substrate.Record{ID: "books", Kind: Kind, Version: 3, Properties: props}
}

func seeded() *fakeData {
	f := newFakeData()
	f.add(authorKind, "leguin", map[string]any{"name": "Ursula K. Le Guin"})
	f.add(authorKind, "herbert", map[string]any{"name": "Frank Herbert"})
	f.add(bookKind, "dune", map[string]any{"title": "Dune", "status": "read", "notes": "Spice.", "author": map[string]any{"ref": authorKind + "/herbert"}})
	f.add(bookKind, "earthsea", map[string]any{"title": "A Wizard of Earthsea", "status": "reading", "author": map[string]any{"ref": authorKind + "/leguin"}})
	return f
}

func runtime(f *fakeData) *Runtime {
	return &Runtime{Data: f, Now: func() time.Time { return time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC) }}
}

func TestTheBooksAppletChecksClean(t *testing.T) {
	rec := booksApplet(t)
	if problems := CheckRecord(rec.ID, rec.Properties, library); len(problems) > 0 {
		t.Fatalf("the books applet is refused:\n%s", strings.Join(problems, "\n"))
	}
}

// Each mutation is a mistake an agent makes, and the problem names where.
func TestCheckNamesEachMistakeByPath(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(p map[string]any)
		want   string
	}{
		{
			"no runtime", func(p map[string]any) { delete(p, "runtime") },
			`props.runtime: is required: what draws the applet's screens, one of a2ui`,
		},
		{
			"a runtime this server does not run", func(p map[string]any) { p["runtime"] = "react" },
			`props.runtime: "react" is not a runtime this server runs: one of a2ui`,
		},
		{
			"a hyphen in an id", func(p map[string]any) { component(p, "library", "count")["id"] = "book-count" },
			`props.screens.library.components[6].id: "book-count" is not an A2UI id`,
		},
		{
			"a component outside the catalog", func(p map[string]any) { component(p, "library", "count")["component"] = "Badge" },
			`"Badge" is not an A2UI v1.0 basic catalog component`,
		},
		{
			"a prop the component lacks", func(p map[string]any) { component(p, "library", "count")["color"] = "red" },
			`Text has no prop "color"`,
		},
		{
			"an enum value outside the set", func(p map[string]any) { component(p, "library", "count")["variant"] = "h2" },
			`components[6].variant: is one of caption, body`,
		},
		{"v0.9's binding spelling", func(p map[string]any) {
			component(p, "book", "bookStatus")["text"] = map[string]any{"path": "/data/book/properties/status"}
		}, `{"path": ...} is v0.9's spelling`},
		{"an input bound outside /ui", func(p map[string]any) {
			component(p, "library", "q")["value"] = map[string]any{"@path": "/data/q"}
		}, `components[2].value: binds a /ui path`},
		{"a button naming no action", func(p map[string]any) {
			component(p, "library", "addButton")["action"] = map[string]any{"event": map[string]any{"name": "create"}}
		}, `action.event.name: names no action: "create"`},
		{"a source the screen does not read", func(p map[string]any) {
			component(p, "library", "count")["text"] = map[string]any{"@path": "/counts/book"}
		}, `reads source "book", which this screen does not list`},
		{"a template over a one-record source", func(p map[string]any) {
			screen(p, "library")["sources"] = []any{"books", "book"}
			component(p, "library", "list")["children"] = map[string]any{"path": "/data/book", "componentId": "row"}
		}, `source "book" reads one record by id, so it is not a list`},
		{"a relative path outside a template", func(p map[string]any) {
			component(p, "book", "bookStatus")["text"] = map[string]any{"@path": "properties/status"}
		}, `"properties/status" is relative`},
		{"a child naming nothing", func(p map[string]any) {
			component(p, "library", "root")["children"] = []any{"toolbar", "filter"}
		}, `children[1]: names no component: "filter"`},
		{
			"no root", func(p map[string]any) { component(p, "book", "root")["id"] = "main" },
			`screens.book.components: no component has id root`,
		},
		{"a property the kind lacks", func(p map[string]any) {
			src(p, "books")["where"] = map[string]any{"state": map[string]any{"in": map[string]any{"@path": "/ui/status"}}}
		}, `sources.books.where.state: ada.example.com/library/book declares no property "state"`},
		{
			"a kind the repository lacks", func(p map[string]any) { src(p, "books")["kind"] = "ada.example.com/library/novel" },
			`sources.books.kind: this repository declares no kind ada.example.com/library/novel`,
		},
		{
			"a bare kind", func(p map[string]any) { src(p, "books")["kind"] = "book" },
			`"book" is not a kind named in full`,
		},
		{
			"expanding a plain property", func(p map[string]any) { src(p, "books")["expand"] = []any{"title"} },
			`expand[0]: "title" is string; expand takes a reference property`,
		},
		{
			"navigating nowhere", func(p map[string]any) { step(p, "add", 0)["screen"] = "new" },
			`actions.add[0].screen: names no screen: "new"`,
		},
		{
			"a result no step named", func(p map[string]any) { delete(step(p, "addResult", 0), "as") },
			`@path "/results/author/ref" names no earlier step`,
		},
		{
			"an op that does not exist", func(p map[string]any) { step(p, "finish", 0)["op"] = "update" },
			`actions.finish[0].op: "update" is not an op`,
		},
		{
			"a function the repository lacks", func(p map[string]any) { step(p, "search", 0)["function"] = "ada.example.com/library/find" },
			`this repository has no function ada.example.com/library/find`,
		},
		{"a source reading the button's context", func(p map[string]any) {
			src(p, "book")["id"] = map[string]any{"@path": "/context/id"}
		}, `@path "/context/id" reads none of /ui, /params here`},
		{
			"a start that is no screen", func(p map[string]any) { p["start"] = "home" },
			`props.start: names no screen: "home"`,
		},
		{"a component that contains itself", func(p map[string]any) {
			component(p, "library", "rowText")["children"] = []any{"rowTitle", "row"}
		}, `screens.library.components: a component contains itself: row → rowLayout → rowText → row`},
		{
			"options that are not a list", func(p map[string]any) { component(p, "library", "filters")["options"] = 42 },
			`components[5].options: is a list of {label, value}`,
		},
		{"a function missing a required argument", func(p map[string]any) {
			component(p, "library", "count")["text"] = map[string]any{"@call": "formatCurrency", "args": map[string]any{"value": 3}}
		}, `components[6].text.args.currency: is required by formatCurrency`},
		{"two questions in one action", func(p map[string]any) {
			step(p, "remove", 1)["confirm"] = "Really?"
		}, `actions.remove[1].confirm: an action asks one question`},
		{"a question reading a step's result", func(p map[string]any) {
			step(p, "addResult", 1)["confirm"] = map[string]any{"@path": "/results/author/title"}
		}, `actions.addResult[1].confirm: @path "/results/author/title" reads none of /ui, /params, /context here`},
		{"an init that writes", func(p map[string]any) {
			screen(p, "book")["init"] = []any{map[string]any{"op": "delete", "kind": bookKind, "id": map[string]any{"@path": "/params/id"}}}
		}, `screens.book.init[0].op: a screen's init reads and sets /ui alone`},
		{"a navigate that leaves out a param the screen reads", func(p map[string]any) {
			delete(step(p, "open", 0), "params")
		}, `actions.open[0].params: screen "book" reads /params/id, so a navigate to it passes id`},
		{
			"a start screen that needs a param", func(p map[string]any) { p["start"] = "book" },
			`props.start: screen "book" reads /params/id, which nothing sets when the applet opens`,
		},
		{"an input over a key the state does not declare", func(p map[string]any) {
			component(p, "library", "q")["value"] = map[string]any{"@path": "/ui/query"}
		}, `/ui/query is under no key the applet's state declares: add "query" to state`},
		{"an init that keeps a result", func(p map[string]any) {
			screen(p, "book")["init"].([]any)[0].(map[string]any)["as"] = "saved"
		}, `screens.book.init[0].as: a screen's init keeps no result`},
		{"a question whose text reads a step's result", func(p map[string]any) {
			step(p, "remove", 0)["confirm"] = map[string]any{"@call": "formatString", "args": map[string]any{"value": "Delete ${/results/x/title}?"}}
		}, `actions.remove[0].confirm.args.value: @path "/results/x/title" reads none of /ui, /params, /context here`},
		{
			"a navigate to a screen that is not a name", func(p map[string]any) { step(p, "add", 0)["screen"] = 123 },
			`actions.add[0].screen: is a string`,
		},
		{
			"a navigate to an empty screen", func(p map[string]any) { step(p, "add", 0)["screen"] = "" },
			`actions.add[0].screen: is required by navigate`,
		},
		{"a placeholder outside formatString", func(p map[string]any) {
			component(p, "library", "count")["text"] = map[string]any{"@call": "pluralize", "args": map[string]any{
				"value": map[string]any{"@path": "/counts/books"}, "one": "1 book", "other": "${/counts/books} books",
			}}
		}, `components[6].text.args.other: ${...} is filled only inside formatString`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := booksApplet(t)
			tc.mutate(rec.Properties)
			problems := CheckRecord(rec.ID, normalize(rec.Properties).(map[string]any), library)
			for _, p := range problems {
				if strings.Contains(p, tc.want) {
					return
				}
			}
			t.Fatalf("want a problem containing %q, got:\n%s", tc.want, strings.Join(problems, "\n"))
		})
	}
}

func screen(p map[string]any, name string) map[string]any {
	return p["screens"].(map[string]any)[name].(map[string]any)
}

func component(p map[string]any, scr, id string) map[string]any {
	for _, c := range screen(p, scr)["components"].([]any) {
		if c.(map[string]any)["id"] == id {
			return c.(map[string]any)
		}
	}
	panic("no component " + id)
}

func src(p map[string]any, name string) map[string]any {
	return p["sources"].(map[string]any)[name].(map[string]any)
}

func step(p map[string]any, action string, i int) map[string]any {
	return p["actions"].(map[string]any)[action].([]any)[i].(map[string]any)
}

func TestRenderStartsOnTheStartScreenWithItsData(t *testing.T) {
	f, rec := seeded(), booksApplet(t)
	frame, err := runtime(f).Render(context.Background(), rec, substrate.AppletRenderInput{})
	if err != nil {
		t.Fatal(err)
	}
	if frame.State.Screen != "library" || frame.Applet.Title != "Library" || frame.Shown.Screen != "library" || frame.Shown.Version != 3 {
		t.Fatalf("frame header: %+v state %+v shown %+v", frame.Applet, frame.State, frame.Shown)
	}
	if len(frame.Messages) != 1 || frame.Messages[0].CreateSurface == nil {
		t.Fatalf("want one createSurface, got %+v", frame.Messages)
	}
	cs := frame.Messages[0].CreateSurface
	if cs.CatalogID != substrate.A2UIBasicCatalog || cs.SurfaceID != SurfaceID {
		t.Fatalf("surface: %+v", cs)
	}
	books := cs.DataModel["data"].(map[string]any)["books"].([]any)
	if len(books) != 2 {
		t.Fatalf("want both books, got %v", books)
	}
	first := books[0].(map[string]any)
	author := first["properties"].(map[string]any)["author"].(map[string]any)
	if first["title"] != "A Wizard of Earthsea" || author["title"] != "Ursula K. Le Guin" || author["properties"].(map[string]any)["name"] != "Ursula K. Le Guin" {
		t.Fatalf("an expanded row: %v", first)
	}
	if cs.DataModel["counts"].(map[string]any)["books"] != float64(2) {
		t.Fatalf("counts: %v", cs.DataModel["counts"])
	}
	if !slices.Equal(frame.Applet.Watch, []string{authorKind, bookKind}) || !slices.Equal(frame.Applet.RefreshOn, []string{"/ui/status", "/ui/q"}) {
		t.Fatalf("watch %v refreshOn %v", frame.Applet.Watch, frame.Applet.RefreshOn)
	}
	if ui := cs.DataModel["ui"].(map[string]any); ui["q"] != "" || ui["form"] == nil {
		t.Fatalf("the initial ui: %v", ui)
	}
}

func TestAFilterBindingThatResolvesToNothingIsDropped(t *testing.T) {
	f, rec := seeded(), booksApplet(t)
	shown := &substrate.AppletShown{Screen: "library", Version: 3}
	frame, err := runtime(f).Render(context.Background(), rec, substrate.AppletRenderInput{
		State: &substrate.AppletState{Screen: "library", UI: map[string]any{
			"status": []any{"read"}, "q": "", "search": map[string]any{}, "form": map[string]any{}, "edit": map[string]any{},
		}},
		Shown: shown,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The same screen of the same version: the data alone, never /ui, so the
	// renderer keeps what the owner is typing.
	var paths []string
	var data map[string]any
	for _, m := range frame.Messages {
		if m.UpdateDataModel == nil {
			t.Fatalf("want data updates alone, got %+v", m)
		}
		paths = append(paths, m.UpdateDataModel.Path)
		if m.UpdateDataModel.Path == "/data" {
			data = m.UpdateDataModel.Value.(map[string]any)
		}
	}
	if slices.Contains(paths, "/ui") {
		t.Fatalf("a plain render sent /ui back: %v", paths)
	}
	books := data["books"].([]any)
	if len(books) != 1 || books[0].(map[string]any)["id"] != "dune" {
		t.Fatalf("status in [read] reads Dune alone, got %v", books)
	}
}

func TestOpenNavigatesToTheRecordsScreen(t *testing.T) {
	f, rec := seeded(), booksApplet(t)
	frame, err := runtime(f).Act(context.Background(), substrate.ActorPerson, rec, substrate.AppletActInput{
		State:  substrate.AppletState{Screen: "library", UI: map[string]any{}},
		Shown:  &substrate.AppletShown{Screen: "library", Version: 3},
		Action: substrate.AppletAction{Name: "open", Context: map[string]any{"id": "dune"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if frame.State.Screen != "book" || frame.State.Params["id"] != "dune" {
		t.Fatalf("state: %+v", frame.State)
	}
	if len(frame.Messages) != 2 || frame.Messages[0].DeleteSurface == nil || frame.Messages[1].CreateSurface == nil {
		t.Fatalf("a new screen replaces the surface: %+v", frame.Messages)
	}
	model := frame.Messages[1].CreateSurface.DataModel
	book := model["data"].(map[string]any)["book"].(map[string]any)
	if book["id"] != "dune" {
		t.Fatalf("the book: %v", book)
	}
	// The read put the book's properties where the notes input binds, so it
	// opens on what the owner saved.
	if notes := model["ui"].(map[string]any)["edit"].(map[string]any)["notes"]; notes != "Spice." {
		t.Fatalf("the edit state: %v", model["ui"])
	}
}

func TestAConfirmedStepRunsNothingUntilConfirmed(t *testing.T) {
	f, rec := seeded(), booksApplet(t)
	in := substrate.AppletActInput{
		State:  substrate.AppletState{Screen: "book", Params: map[string]any{"id": "dune"}, UI: map[string]any{}},
		Action: substrate.AppletAction{Name: "remove", Context: map[string]any{"title": "Dune"}},
	}
	frame, err := runtime(f).Act(context.Background(), substrate.ActorPerson, rec, in)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Confirm != "Delete “Dune”?" || f.records[bookKind+"/dune"] == nil || frame.State.Screen != "book" || len(frame.Messages) != 0 {
		t.Fatalf("an unconfirmed remove ran: confirm %q, state %+v", frame.Confirm, frame.State)
	}
	in.Confirmed = true
	frame, err = runtime(f).Act(context.Background(), substrate.ActorPerson, rec, in)
	if err != nil {
		t.Fatal(err)
	}
	if f.records[bookKind+"/dune"] != nil || frame.State.Screen != "library" || frame.Confirm != "" {
		t.Fatalf("a confirmed remove: confirm %q, state %+v", frame.Confirm, frame.State)
	}
}

func TestAddingASearchResultEnsuresItsAuthorOnce(t *testing.T) {
	f, rec := seeded(), booksApplet(t)
	rt := runtime(f)
	ctx := context.Background()
	search, err := rt.Act(ctx, substrate.ActorPerson, rec, substrate.AppletActInput{
		State:  substrate.AppletState{Screen: "add", UI: map[string]any{"search": map[string]any{"q": "left hand"}}},
		Shown:  &substrate.AppletShown{Screen: "add", Version: 3},
		Action: substrate.AppletAction{Name: "search"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.calls[0].(map[string]any)["query"]; got != "left hand" {
		t.Fatalf("the function's input: %v", f.calls)
	}
	var ui map[string]any
	for _, m := range search.Messages {
		if m.UpdateDataModel != nil && m.UpdateDataModel.Path == "/ui" {
			ui = m.UpdateDataModel.Value.(map[string]any)
		}
	}
	results := ui["search"].(map[string]any)["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("a call's output lands at into and /ui is sent: %v", search.Messages)
	}
	hit := results[0].(map[string]any)
	for range 2 {
		frame, err := rt.Act(ctx, substrate.ActorPerson, rec, substrate.AppletActInput{
			State:  search.State,
			Shown:  &search.Shown,
			Action: substrate.AppletAction{Name: "addResult", Context: hit},
		})
		if err != nil {
			t.Fatal(err)
		}
		if frame.Notice != "Added The Left Hand of Darkness" || frame.State.Screen != "book" {
			t.Fatalf("notice %q state %+v", frame.Notice, frame.State)
		}
	}
	if len(f.of(authorKind)) != 2 {
		t.Fatalf("ensure found Le Guin both times: %d authors", len(f.of(authorKind)))
	}
	var added *substrate.Record
	for _, b := range f.of(bookKind) {
		if b.Title == "The Left Hand of Darkness" {
			added = b
		}
	}
	if added == nil || refPath(added.Properties["author"]) != authorKind+"/leguin" || added.Properties["status"] != "want" {
		t.Fatalf("the added book: %+v", added)
	}
}

func TestARefusedStepNamesItsActionAndStep(t *testing.T) {
	f, rec := seeded(), booksApplet(t)
	f.refuse = &substrate.ValidationError{Problems: []string{"props.title: required"}}
	_, err := runtime(f).Act(context.Background(), substrate.ActorPerson, rec, substrate.AppletActInput{
		State:  substrate.AppletState{Screen: "add", UI: map[string]any{"form": map[string]any{"title": "", "author": "Someone New"}}},
		Action: substrate.AppletAction{Name: "save"},
	})
	var ve *substrate.ValidationError
	if !errors.As(err, &ve) || ve.Problems[0] != "actions.save[0] (ensure): props.title: required" || ve.Reason != "" {
		t.Fatalf("want the step named on the refusal, got %v", err)
	}

	// A stale revision keeps its reason through the naming, so the API
	// answers the action's refusal with it.
	f.refuse = &substrate.ValidationError{
		Problems: []string{"props.revision: 2 is below the stored revision 3; a rewrite never lowers it, so this is a stale delivery"},
		Reason:   substrate.ReasonStaleRevision,
	}
	_, err = runtime(f).Act(context.Background(), substrate.ActorPerson, rec, substrate.AppletActInput{
		State:  substrate.AppletState{Screen: "add", UI: map[string]any{"form": map[string]any{"title": "A Title", "author": "Someone New"}}},
		Action: substrate.AppletAction{Name: "save"},
	})
	if !errors.As(err, &ve) || !strings.HasPrefix(ve.Problems[0], "actions.save[0] (ensure): props.revision:") || ve.Reason != substrate.ReasonStaleRevision {
		t.Fatalf("want the stale refusal named with its reason, got %v (reason %q)", err, ve.Reason)
	}
}

func TestSetAndNowWriteWhatTheyName(t *testing.T) {
	f, rec := seeded(), booksApplet(t)
	frame, err := runtime(f).Act(context.Background(), substrate.ActorPerson, rec, substrate.AppletActInput{
		State:  substrate.AppletState{Screen: "book", Params: map[string]any{"id": "earthsea"}, UI: map[string]any{}},
		Action: substrate.AppletAction{Name: "finish"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := f.records[bookKind+"/earthsea"].Properties
	if got["status"] != "read" || got["finishedAt"] != "2026-11-08" || frame.Notice != "Marked as read" {
		t.Fatalf("finish wrote %v, notice %q", got, frame.Notice)
	}
}

func TestPreviewOutlinesAndFlagsWhatResolvedToNothing(t *testing.T) {
	f, rec := seeded(), booksApplet(t)
	component(rec.Properties, "library", "rowMeta")["text"] = map[string]any{"@path": "properties/author/name"}
	component(rec.Properties, "library", "rowTitle")["text"] = map[string]any{"@call": "formatString", "args": map[string]any{"value": "**${properties/title}** ${properties/author}"}}
	d, problems := Parse(rec)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	frame, err := runtime(f).Render(context.Background(), rec, substrate.AppletRenderInput{})
	if err != nil {
		t.Fatal(err)
	}
	p := Outline(d, frame)
	for _, want := range []string{`(2 items at /data/books)`, `rowTitle Text text="**A Wizard of Earthsea** {`, `addButton Button variant="primary" action=→add`} {
		if !strings.Contains(p.Outline, want) {
			t.Errorf("outline lacks %q:\n%s", want, p.Outline)
		}
	}
	// The cover is a value the seeded books do not have; the author's name is
	// a wrong path, and the line says what the author holds instead.
	if !slices.Equal(p.Unresolved, []string{
		"rowCover.url: <item>/properties/coverUrl resolved to nothing; <item>/properties has author, status, title",
		"rowMeta.text: <item>/properties/author/name resolved to nothing; <item>/properties/author has display, id, kind, properties, ref, title, version",
		"rowTitle.text: <item>/properties/author is a list or an object, drawn as JSON; name one item (<item>/properties/author/0) or one field",
	}) {
		t.Fatalf("unresolved:\n%s", strings.Join(p.Unresolved, "\n"))
	}
}

// A screen opened cold, from a link or a step back through history, reads
// its record into the edit state, so an edit never starts from nothing and
// never from another record's values.
func TestAScreenIsEnteredByItsInitHoweverItIsReached(t *testing.T) {
	f, rec := seeded(), booksApplet(t)
	f.records[bookKind+"/earthsea"].Properties["notes"] = "Ged."
	rt := runtime(f)
	cold, err := rt.Render(context.Background(), rec, substrate.AppletRenderInput{
		State: &substrate.AppletState{Screen: "book", Params: map[string]any{"id": "dune"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := cold.State.UI["edit"].(map[string]any)["notes"]; got != "Spice." {
		t.Fatalf("a cold open left the edit at %v", cold.State.UI["edit"])
	}
	// Back to another book on the same screen: the params differ from what
	// is shown, so the screen is entered again and the edit follows them.
	back, err := rt.Render(context.Background(), rec, substrate.AppletRenderInput{
		State: &substrate.AppletState{Screen: "book", Params: map[string]any{"id": "earthsea"}, UI: cold.State.UI},
		Shown: &cold.Shown,
	})
	if err != nil {
		t.Fatal(err)
	}
	var ui map[string]any
	for _, m := range back.Messages {
		if m.UpdateDataModel != nil && m.UpdateDataModel.Path == "/ui" {
			ui = m.UpdateDataModel.Value.(map[string]any)
		}
	}
	if ui == nil || ui["edit"].(map[string]any)["notes"] != "Ged." {
		t.Fatalf("the edit after a step back: %v", back.Messages)
	}
	// The same screen and params re-rendered keeps what the owner typed.
	typed := cloneMap(back.State.UI)
	typed["edit"].(map[string]any)["notes"] = "Ged, and the shadow."
	again, err := rt.Render(context.Background(), rec, substrate.AppletRenderInput{
		State: &substrate.AppletState{Screen: "book", Params: map[string]any{"id": "earthsea"}, UI: typed},
		Shown: &back.Shown,
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.State.UI["edit"].(map[string]any)["notes"] != "Ged, and the shadow." {
		t.Fatalf("a plain re-render reset the edit: %v", again.State.UI["edit"])
	}
}

// A patch whose binding reads a path the state does not hold writes nothing
// for it: an edit that never loaded is not the owner clearing the notes.
func TestAPatchLeavesOutWhatTheStateDoesNotHold(t *testing.T) {
	f, rec := seeded(), booksApplet(t)
	_, err := runtime(f).Act(context.Background(), substrate.ActorPerson, rec, substrate.AppletActInput{
		State:  substrate.AppletState{Screen: "book", Params: map[string]any{"id": "dune"}, UI: map[string]any{"edit": map[string]any{}}},
		Shown:  &substrate.AppletShown{Screen: "book", Version: 3, Params: map[string]any{"id": "dune"}},
		Action: substrate.AppletAction{Name: "saveNotes"},
	})
	var ve *substrate.ValidationError
	if !errors.As(err, &ve) || !strings.Contains(ve.Problems[0], "every binding read nothing") {
		t.Fatalf("want the empty patch refused, got %v", err)
	}
	if f.records[bookKind+"/dune"].Properties["notes"] != "Spice." {
		t.Fatal("the notes were cleared")
	}
}

// No step of an action carries the request's idempotency key: an action
// is not replayed, and one key on two creates would collide.
func TestNoStepOfAnActionCarriesTheRequestsKey(t *testing.T) {
	f, rec := seeded(), booksApplet(t)
	ctx := substrate.WithIdempotencyKey(context.Background(), "tap-1")
	_, err := runtime(f).Act(ctx, substrate.ActorPerson, rec, substrate.AppletActInput{
		State:  substrate.AppletState{Screen: "add", UI: map[string]any{"form": map[string]any{"title": "Kindred", "author": "Octavia E. Butler"}}},
		Action: substrate.AppletAction{Name: "save"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.keys, []string{"", ""}) {
		t.Fatalf("keys: %v", f.keys)
	}
}

// A literal reference is a value to filter by, not a set of conditions.
func TestAFilterByALiteralReferenceIsAdmitted(t *testing.T) {
	rec := booksApplet(t)
	src(rec.Properties, "books")["where"] = map[string]any{"author": map[string]any{"ref": authorKind + "/leguin"}}
	if problems := CheckRecord(rec.ID, normalize(rec.Properties).(map[string]any), library); len(problems) > 0 {
		t.Fatalf("refused: %v", problems)
	}
}

// A record carries display values beside its properties: an enum's label,
// or its value made readable, and an expanded reference's title.
func TestARecordCarriesDisplayValues(t *testing.T) {
	f, rec := seeded(), booksApplet(t)
	f.records[bookKind+"/dune"].Properties["status"] = "want"
	frame, err := runtime(f).Render(context.Background(), rec, substrate.AppletRenderInput{})
	if err != nil {
		t.Fatal(err)
	}
	books := frame.Messages[0].CreateSurface.DataModel["data"].(map[string]any)["books"].([]any)
	byID := map[string]map[string]any{}
	for _, b := range books {
		byID[b.(map[string]any)["id"].(string)] = b.(map[string]any)["display"].(map[string]any)
	}
	if got := byID["dune"]; got["status"] != "Want to read" || got["author"] != "Frank Herbert" || got["title"] != "Dune" {
		t.Fatalf("dune displays %v", got)
	}
	if got := byID["earthsea"]["status"]; got != "Reading" {
		t.Fatalf("a value with no label reads %v", got)
	}
}

func TestDisplayValuesJoinListsAndSayYesOrNo(t *testing.T) {
	cases := []struct {
		pd   propDisplay
		v    any
		want string
		ok   bool
	}{
		{propDisplay{typ: "string"}, []any{"Ursula K. Le Guin", "", "Kim Stanley Robinson"}, "Ursula K. Le Guin, Kim Stanley Robinson", true},
		{propDisplay{typ: "bool"}, true, "Yes", true},
		{propDisplay{typ: "enum"}, "inProgress", "In progress", true},
		{propDisplay{typ: "reference"}, map[string]any{"ref": "a.example.com/x/y/1"}, "", false},
		{propDisplay{typ: "string"}, []any{}, "", false},
	}
	for _, c := range cases {
		got, ok := displayValue(c.pd, c.v)
		if got != c.want || ok != c.ok {
			t.Errorf("displayValue(%v) = %q %v, want %q %v", c.v, got, ok, c.want, c.ok)
		}
	}
}

// A screen opened cold that cannot load opens the start instead, saying why.
func TestAScreenThatCannotOpenFallsBackToTheStart(t *testing.T) {
	f, rec := seeded(), booksApplet(t)
	frame, err := runtime(f).Render(context.Background(), rec, substrate.AppletRenderInput{
		State: &substrate.AppletState{Screen: "book"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if frame.State.Screen != "library" || !strings.Contains(frame.Notice, "Book opens on a record: pick one here") {
		t.Fatalf("screen %q notice %q", frame.State.Screen, frame.Notice)
	}
}

// A screen whose record is gone, or that the applet does not have, opens
// the start, saying so; a failure that is the server's is answered as one,
// by render and preview alike, so its text never reaches a notice or a
// problem.
func TestOnlyTheAppletsOwnFailuresAreSaidToTheOwner(t *testing.T) {
	f, rec := seeded(), booksApplet(t)
	rt := runtime(f)
	gone := &substrate.AppletState{Screen: "book", Params: map[string]any{"id": "gone"}}
	frame, err := rt.Render(context.Background(), rec, substrate.AppletRenderInput{State: gone})
	if err != nil {
		t.Fatal(err)
	}
	if frame.State.Screen != "library" || frame.Notice != "Book could not open: its record is not there" {
		t.Fatalf("screen %q notice %q", frame.State.Screen, frame.Notice)
	}
	p, err := rt.Preview(context.Background(), rec, substrate.AppletPreviewInput{Screen: "book", Params: map[string]any{"id": "gone"}}, nil, nil)
	if err != nil || len(p.Problems) == 0 || !strings.Contains(p.Problems[len(p.Problems)-1], "not found") {
		t.Fatalf("preview of a gone record: %v %v", p.Problems, err)
	}

	unknown, err := rt.Render(context.Background(), rec, substrate.AppletRenderInput{State: &substrate.AppletState{Screen: "shelf"}})
	if err != nil {
		t.Fatal(err)
	}
	if unknown.State.Screen != "library" || !strings.HasSuffix(unknown.Notice, `has no screen "shelf", so it opened here`) {
		t.Fatalf("screen %q notice %q", unknown.State.Screen, unknown.Notice)
	}

	f.fail = errors.New("pq: the disk is full")
	for name, call := range map[string]func() error{
		"render": func() error {
			_, err := rt.Render(context.Background(), rec, substrate.AppletRenderInput{
				State: &substrate.AppletState{Screen: "book", Params: map[string]any{"id": "dune"}},
			})
			return err
		},
		"preview": func() error {
			_, err := rt.Preview(context.Background(), rec, substrate.AppletPreviewInput{Screen: "book", Params: map[string]any{"id": "dune"}}, nil, nil)
			return err
		},
	} {
		if err := call(); !errors.Is(err, f.fail) || errors.Is(err, substrate.ErrValidation) {
			t.Errorf("%s answered %v, want the server's failure", name, err)
		}
	}
}

// A question about a delete names the record from the button's context, is
// answered "Delete", drawn as a destructive answer, and the header names the
// actions that delete.
func TestADeleteIsAskedAndDrawnAsOne(t *testing.T) {
	f, rec := seeded(), booksApplet(t)
	frame, err := runtime(f).Act(context.Background(), substrate.ActorPerson, rec, substrate.AppletActInput{
		State:  substrate.AppletState{Screen: "book", Params: map[string]any{"id": "dune"}, UI: map[string]any{}},
		Action: substrate.AppletAction{Name: "remove", Context: map[string]any{"title": "Dune"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if frame.Confirm != "Delete “Dune”?" || frame.ConfirmLabel != "Delete" || !frame.ConfirmDanger {
		t.Fatalf("confirm %q %q danger %v", frame.Confirm, frame.ConfirmLabel, frame.ConfirmDanger)
	}
	if !slices.Equal(frame.Applet.Destructive, []string{"remove"}) || frame.Applet.Start != "library" {
		t.Fatalf("header %+v", frame.Applet)
	}
}

// The guide the MCP door serves names every op a step takes and every root
// a screen binds, so an assistant reading it can build what the check admits.
func TestTheGuideNamesEveryOpAndRoot(t *testing.T) {
	for op := range stepKeys {
		if !strings.Contains(Guide, "`"+op+" ") && !strings.Contains(Guide, "`"+op+"`") {
			t.Errorf("the guide never names op %s", op)
		}
	}
	for _, root := range append(slices.Clone(modelRoots), "display/", "init", "refreshOn", "confirm") {
		if !strings.Contains(Guide, root) {
			t.Errorf("the guide never names %s", root)
		}
	}
}
