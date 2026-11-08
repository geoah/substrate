package applet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// Data is what the runtime reaches of a repository. A substrate.Dataset is
// one; the API hands the request's, so an applet's reads and writes are the
// caller's own.
type Data interface {
	List(ctx context.Context, q substrate.Query) (*substrate.Page, error)
	Put(ctx context.Context, actor substrate.Actor, in substrate.PutInput) (*substrate.Record, error)
	Patch(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.PatchInput) (*substrate.Record, error)
	Delete(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.DeleteInput) (*substrate.Record, error)
	CallFunction(ctx context.Context, caller substrate.Actor, name string, args any) (any, int, error)
	// KindByRef answers a kind's declaration, which the runtime reads for
	// the display values it puts beside a record's properties.
	KindByRef(ctx context.Context, ref string) (substrate.KindInfo, error)
}

// Runtime renders and acts. Now is the clock `@now` reads.
type Runtime struct {
	Data Data
	Now  func() time.Time
}

func (rt *Runtime) now() time.Time {
	if rt.Now != nil {
		return rt.Now()
	}
	return time.Now()
}

func recordOf(id string, version int64, props map[string]any) *substrate.Record {
	return &substrate.Record{ID: id, Kind: Kind, Version: version, Properties: props}
}

// load parses an applet record. It does not run Check again: an applet the
// engine admitted stays drawable after a kind it names changes, and a read
// or a write the change broke is refused where it runs.
func load(rec *substrate.Record) (*Def, error) {
	d, problems := Parse(rec)
	if len(problems) > 0 {
		return nil, &substrate.ValidationError{Problems: problems}
	}
	return d, nil
}

// Render answers the frame for the applet's current screen.
func (rt *Runtime) Render(ctx context.Context, rec *substrate.Record, in substrate.AppletRenderInput) (*substrate.AppletFrame, error) {
	d, err := load(rec)
	if err != nil {
		return nil, err
	}
	return rt.render(ctx, d, in, true)
}

// render renders the state's screen. fallback opens the start instead of a
// screen that cannot load, which a person wants and a preview does not.
func (rt *Runtime) render(ctx context.Context, d *Def, in substrate.AppletRenderInput, fallback bool) (*substrate.AppletFrame, error) {
	st, sendUI := d.state(in.State)
	entered, err := rt.enter(ctx, d, &st, in.Shown)
	if err != nil && fallback && st.Screen != d.Start && expected(err) {
		// A screen opened from a link or a step back that cannot load (a
		// detail screen with no record named) opens the applet's start
		// instead, saying why, rather than leaving the owner on an error.
		// Any other failure is the server's and is answered as one.
		failed := d.Screens[st.Screen].Title
		if failed == "" {
			failed = st.Screen
		}
		st = substrate.AppletState{Screen: d.Start, Params: map[string]any{}, UI: st.UI}
		if _, startErr := rt.enter(ctx, d, &st, nil); startErr != nil {
			if !expected(startErr) {
				return nil, startErr
			}
			return nil, err
		}
		f, ferr := rt.frame(ctx, d, st, in.Shown, true)
		if ferr != nil {
			return nil, ferr
		}
		f.Notice = fmt.Sprintf("%s could not open: %s", failed, reason(err))
		if errors.Is(err, errNoRecord) {
			f.Notice = fmt.Sprintf("%s opens on a record: pick one here", failed)
		}
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	f, err := rt.frame(ctx, d, st, in.Shown, sendUI || entered)
	if err != nil {
		return nil, err
	}
	if in.State != nil && in.State.Screen != "" && in.State.Screen != st.Screen {
		// An address naming a screen the applet does not have (an old
		// bookmark, a renamed screen) opens the start, saying so.
		f.Notice = fmt.Sprintf("%s has no screen %q, so it opened here", d.Name, in.State.Screen)
	}
	return f, nil
}

// expected reports whether a screen's failure is the applet's to explain: a
// problem with the applet or its params, or a record that is not there. Any
// other error's text is the server's, which the API's mapping keeps from the
// caller, so it never reaches a notice or a preview.
func expected(err error) bool {
	return errors.Is(err, substrate.ErrValidation) || errors.Is(err, substrate.ErrNotFound)
}

// reason is an expected error as one line an owner reads: a validation
// error's first problem without its path.
func reason(err error) string {
	var ve *substrate.ValidationError
	if errors.As(err, &ve) && len(ve.Problems) > 0 {
		p := ve.Problems[0]
		if i := strings.LastIndex(p, ": "); i >= 0 {
			p = p[i+2:]
		}
		return p
	}
	if errors.Is(err, substrate.ErrNotFound) {
		return "its record is not there"
	}
	return "it failed"
}

// enter runs the screen's init when the renderer is not drawing that screen
// already: a link opened cold, a step back through history, a navigate. So
// an edit always starts from the record the params name, never from what
// another record left in /ui.
func (rt *Runtime) enter(ctx context.Context, d *Def, st *substrate.AppletState, shown *substrate.AppletShown) (bool, error) {
	screen := d.Screens[st.Screen]
	if screen == nil {
		return false, &substrate.ValidationError{Problems: []string{fmt.Sprintf("screen: this applet has no screen %q", st.Screen)}}
	}
	if len(screen.Init) == 0 || (shown != nil && shown.Screen == st.Screen && reflect.DeepEqual(normalize(shown.Params), normalize(st.Params))) {
		return false, nil
	}
	r := &resolver{now: rt.now(), model: map[string]any{"ui": st.UI, "params": st.Params}}
	for i, s := range screen.Init {
		if _, err := rt.step(ctx, "", r, s, st); err != nil {
			return false, stepError("init of "+st.Screen, i, s.Op, err)
		}
	}
	return true, nil
}

// state is the renderer's state checked against the applet: an unknown screen
// is the start, and a UI state the renderer lacks is the applet's initial one,
// whose keys also fill what a newer version added. sendUI says the renderer
// holds no /ui of its own yet.
func (d *Def) state(in *substrate.AppletState) (substrate.AppletState, bool) {
	st := substrate.AppletState{Screen: d.Start}
	sendUI := true
	if in != nil {
		if d.Screens[in.Screen] != nil {
			st.Screen = in.Screen
			st.Params = cloneMap(in.Params)
		}
		if in.UI != nil {
			st.UI = cloneMap(in.UI)
			sendUI = false
		}
	}
	if st.UI == nil {
		st.UI = cloneMap(d.State)
	}
	for k, v := range d.State {
		if _, ok := st.UI[k]; !ok {
			st.UI[k] = clone(v)
			sendUI = true
		}
	}
	if st.Params == nil {
		st.Params = map[string]any{}
	}
	return st, sendUI
}

// frame renders the state's screen: a new surface when the renderer shows
// another screen or another version, else the data alone, so an input keeps
// its focus and its unsent text.
func (rt *Runtime) frame(ctx context.Context, d *Def, st substrate.AppletState, shown *substrate.AppletShown, sendUI bool) (*substrate.AppletFrame, error) {
	screen := d.Screens[st.Screen]
	if screen == nil {
		return nil, &substrate.ValidationError{Problems: []string{fmt.Sprintf("screen: this applet has no screen %q", st.Screen)}}
	}
	title := screen.Title
	if title == "" {
		title = d.Name
	}
	data, counts, watch, err := rt.read(ctx, d, screen, st)
	if err != nil {
		return nil, err
	}
	applet := map[string]any{"id": d.ID, "name": d.Name, "screen": st.Screen, "title": title}
	model := map[string]any{
		"applet": applet,
		"ui":     st.UI,
		"params": st.Params,
		"data":   data,
		"counts": counts,
	}
	model = normalize(model).(map[string]any)
	now := substrate.AppletShown{Screen: st.Screen, Version: d.Version, Params: st.Params}
	var msgs []substrate.A2UIMessage
	if shown == nil || shown.Screen != now.Screen || shown.Version != now.Version {
		if shown != nil {
			msgs = append(msgs, substrate.A2UIMessage{Version: substrate.A2UIVersion, DeleteSurface: &substrate.A2UIDeleteSurface{SurfaceID: SurfaceID}})
		}
		msgs = append(msgs, substrate.A2UIMessage{Version: substrate.A2UIVersion, CreateSurface: &substrate.A2UICreateSurface{
			SurfaceID:  SurfaceID,
			CatalogID:  substrate.A2UIBasicCatalog,
			Components: screen.Components,
			DataModel:  model,
		}})
	} else {
		paths := []string{"applet", "params", "data", "counts"}
		if sendUI {
			paths = append(paths, "ui")
		}
		for _, p := range paths {
			msgs = append(msgs, substrate.A2UIMessage{Version: substrate.A2UIVersion, UpdateDataModel: &substrate.A2UIUpdateDataModel{
				SurfaceID: SurfaceID, Path: "/" + p, Value: model[p],
			}})
		}
	}
	return &substrate.AppletFrame{
		Applet: substrate.AppletHeader{
			ID: d.ID, Name: d.Name, Description: d.Description, Icon: d.Icon, Version: d.Version,
			Title: title, Start: d.Start, Watch: watch, RefreshOn: screen.RefreshOn,
			Destructive: d.destructive(),
		},
		State:    st,
		Shown:    now,
		Messages: msgs,
	}, nil
}

// read runs the screen's sources: what lands at /data and /counts, and the
// kinds whose changes re-render the screen.
func (rt *Runtime) read(ctx context.Context, d *Def, screen *Screen, st substrate.AppletState) (map[string]any, map[string]any, []string, error) {
	data, counts := map[string]any{}, map[string]any{}
	var watch []string
	r := &resolver{model: map[string]any{"ui": st.UI, "params": st.Params}, now: rt.now()}
	shown := &displays{rt: rt, ctx: ctx, kinds: map[string]kindDisplay{}}
	for _, name := range screen.Sources {
		src := d.Sources[name]
		if src == nil {
			continue
		}
		page, single, err := rt.query(ctx, r, src)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("source %s: %w", name, err)
		}
		watch = appendKind(watch, src.Kind)
		var rows []any
		if page != nil {
			for _, inc := range page.Included {
				watch = appendKind(watch, inc.Kind)
			}
			for _, rec := range page.Records {
				row := recordData(rec, page.Included, src.Expand)
				shown.decorate(row)
				rows = append(rows, row)
			}
		}
		if single {
			if len(rows) > 0 {
				data[name] = rows[0]
			} else {
				data[name] = nil
			}
			continue
		}
		if rows == nil {
			rows = []any{}
		}
		data[name] = rows
		counts[name] = len(rows)
	}
	slices.Sort(watch)
	return data, counts, watch, nil
}

func appendKind(kinds []string, k string) []string {
	if k == "" || slices.Contains(kinds, k) {
		return kinds
	}
	return append(kinds, k)
}

// query runs one source. single says it names one record by id; an id that
// resolves to nothing reads nothing.
func (rt *Runtime) query(ctx context.Context, r *resolver, src *Source) (*substrate.Page, bool, error) {
	q := substrate.Query{Filter: substrate.Filter{Kinds: []string{src.Kind}}, OrderBy: src.OrderBy, First: src.First, Expand: src.Expand}
	single := src.ID != nil
	if single {
		v, err := r.resolve(src.ID)
		if err != nil {
			return nil, true, err
		}
		id, _ := v.(string)
		if id == "" {
			return nil, true, nil
		}
		q.Filter.IDs = []string{id}
		q.First = 1
		q.OrderBy = nil
	}
	if src.Search != nil {
		v, err := r.resolve(src.Search)
		if err != nil {
			return nil, single, err
		}
		if s, _ := v.(string); strings.TrimSpace(s) != "" {
			q.Filter.Search = s
		}
	}
	conds, err := conditions(r, src.Where)
	if err != nil {
		return nil, single, err
	}
	q.Filter.Properties = conds
	page, err := rt.Data.List(ctx, q)
	return page, single, err
}

// conditions resolves a where into the filter grammar, dropping each
// condition whose value resolved to nothing.
func conditions(r *resolver, where map[string]any) (map[string]substrate.Cond, error) {
	if len(where) == 0 {
		return nil, nil
	}
	out := map[string]substrate.Cond{}
	for prop, raw := range where {
		v, err := r.resolve(raw)
		if err != nil {
			return nil, err
		}
		ops, isOps := v.(map[string]any)
		if !isOps || isRefValue(ops) {
			if empty(v) {
				continue
			}
			ops = map[string]any{"eq": v}
		}
		kept := map[string]any{}
		for op, operand := range ops {
			if !empty(operand) {
				kept[op] = operand
			}
		}
		if len(kept) == 0 {
			continue
		}
		b, _ := json.Marshal(kept)
		var c substrate.Cond
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			return nil, &substrate.ValidationError{Problems: []string{fmt.Sprintf("where.%s: %v", prop, err)}}
		}
		out[prop] = c
	}
	return out, nil
}

// isRefValue tells a reference value ({ref}) from a set of conditions.
func isRefValue(m map[string]any) bool {
	_, ok := m["ref"]
	return ok && len(m) == 1
}

// recordData is a record as a screen binds it: {id, kind, ref, title,
// version, properties}, each expanded reference holding its record.
func recordData(rec *substrate.Record, included map[string]*substrate.Record, expand []string) map[string]any {
	props := cloneMap(rec.Properties)
	for _, name := range expand {
		props[name] = expandValue(props[name], included)
	}
	title := rec.Title
	if title == "" {
		title, _ = rec.Properties["title"].(string)
	}
	return map[string]any{
		"id":         rec.ID,
		"kind":       rec.Kind,
		"ref":        rec.Kind + "/" + rec.ID,
		"title":      title,
		"version":    rec.Version,
		"properties": props,
	}
}

func expandValue(v any, included map[string]*substrate.Record) any {
	if list, ok := v.([]any); ok {
		out := make([]any, len(list))
		for i, e := range list {
			out[i] = expandValue(e, included)
		}
		return out
	}
	path := refPath(v)
	if path == "" {
		return v
	}
	inc := included[path]
	if inc == nil {
		return v
	}
	return recordData(inc, nil, nil)
}

func refPath(v any) string {
	switch x := v.(type) {
	case string:
		if strings.Count(x, "/") >= 3 {
			return x
		}
	case map[string]any:
		s, _ := x["ref"].(string)
		return s
	}
	return ""
}

// --- actions -------------------------------------------------------------------

// Act runs one of the applet's actions against the renderer's state and answers
// the frame after it. An action with a step that asks first runs nothing
// until in.Confirmed; a step refused stops the action, and what earlier
// steps wrote stays written.
func (rt *Runtime) Act(ctx context.Context, actor substrate.Actor, rec *substrate.Record, in substrate.AppletActInput) (*substrate.AppletFrame, error) {
	d, err := load(rec)
	if err != nil {
		return nil, err
	}
	st, sendUI := d.state(&in.State)
	steps, ok := d.Actions[in.Action.Name]
	if !ok {
		return nil, &substrate.ValidationError{Problems: []string{fmt.Sprintf("action.name: this applet has no action %q; it has %s",
			in.Action.Name, strings.Join(sortedKeys(d.Actions), ", "))}}
	}
	results := map[string]any{}
	r := &resolver{now: rt.now()}
	sync := func() {
		r.model = map[string]any{"ui": st.UI, "params": st.Params, "context": in.Action.Context, "results": results}
	}
	sync()
	if !in.Confirmed {
		for _, s := range steps {
			if s.Confirm == nil {
				continue
			}
			q, err := r.resolve(s.Confirm)
			if err != nil {
				return nil, err
			}
			f, err := rt.frame(ctx, d, st, in.Shown, sendUI)
			if err != nil {
				return nil, err
			}
			// Nothing ran, so the renderer keeps what it draws behind the
			// question rather than redrawing it.
			f.Confirm = text(q)
			f.ConfirmLabel, f.ConfirmDanger = confirmLabel(steps)
			f.Messages = []substrate.A2UIMessage{}
			if in.Shown != nil {
				f.Shown = *in.Shown
			}
			return f, nil
		}
	}
	var notice string
	// An action is not replayed: a retried tap runs its steps again. A step
	// under the request's one key would collide with its siblings, and a
	// patch or a delete consults no key at all, so no step carries one
	// rather than promising a replay only some steps keep (see the plan's
	// "Retries").
	stepCtx := substrate.WithoutIdempotencyKey(ctx)
	before := st.Screen
	for i, s := range steps {
		sync()
		result, err := rt.step(stepCtx, actor, r, s, &st)
		if err == nil && s.Op == "navigate" && d.Screens[st.Screen] == nil {
			err = &substrate.ValidationError{Problems: []string{fmt.Sprintf("screen: this applet has no screen %q", st.Screen)}}
		}
		if err != nil {
			return nil, stepError(in.Action.Name, i, s.Op, err)
		}
		switch s.Op {
		case "set", "call", "read":
			sendUI = sendUI || s.Op == "set" || s.Into != ""
		case "notify":
			notice = text(result)
		}
		if s.As != "" {
			results[s.As] = result
		}
	}
	shown := in.Shown
	if st.Screen != before {
		shown = nil
	}
	entered, err := rt.enter(ctx, d, &st, shown)
	if err != nil {
		return nil, err
	}
	f, err := rt.frame(ctx, d, st, in.Shown, sendUI || entered)
	if err != nil {
		return nil, err
	}
	f.Notice = notice
	return f, nil
}

// stepError names the step a refusal came from, keeping a validation
// error's problems for the renderer to show by path.
func stepError(action string, i int, op string, err error) error {
	var ve *substrate.ValidationError
	if errors.As(err, &ve) {
		problems := make([]string, len(ve.Problems))
		for j, p := range ve.Problems {
			problems[j] = fmt.Sprintf("actions.%s[%d] (%s): %s", action, i, op, p)
		}
		if errors.Is(err, errNoRecord) {
			return noRecord{&substrate.ValidationError{Problems: problems}}
		}
		return &substrate.ValidationError{Problems: problems}
	}
	return fmt.Errorf("actions.%s[%d] (%s): %w", action, i, op, err)
}

func (rt *Runtime) step(ctx context.Context, actor substrate.Actor, r *resolver, s *Step, st *substrate.AppletState) (any, error) {
	switch s.Op {
	case "create":
		props, err := resolveMap(r, s.Properties)
		if err != nil {
			return nil, err
		}
		rec, err := rt.Data.Put(ctx, actor, substrate.PutInput{Kind: s.Kind, Properties: dropEmpty(props)})
		if err != nil {
			return nil, err
		}
		return recordData(rec, nil, nil), nil
	case "patch":
		id, err := resolveID(r, s.ID, "change")
		if err != nil {
			return nil, err
		}
		props, err := resolveMap(r, r.present(s.Properties))
		if err != nil {
			return nil, err
		}
		if len(props) == 0 {
			return nil, &substrate.ValidationError{Problems: []string{"properties: every binding read nothing, so there is nothing to change"}}
		}
		rec, err := rt.Data.Patch(ctx, actor, s.Kind, id, substrate.PatchInput{Properties: props})
		if err != nil {
			return nil, err
		}
		return recordData(rec, nil, nil), nil
	case "delete":
		id, err := resolveID(r, s.ID, "delete")
		if err != nil {
			return nil, err
		}
		if _, err := rt.Data.Delete(ctx, actor, s.Kind, id, substrate.DeleteInput{}); err != nil {
			return nil, err
		}
		return map[string]any{"id": id, "kind": s.Kind, "ref": s.Kind + "/" + id}, nil
	case "ensure":
		conds, err := conditions(r, s.Where)
		if err != nil {
			return nil, err
		}
		if len(conds) == 0 {
			return nil, &substrate.ValidationError{Problems: []string{"where: every condition resolved to nothing, so there is nothing to find"}}
		}
		page, err := rt.Data.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{s.Kind}, Properties: conds}, First: 1})
		if err != nil {
			return nil, err
		}
		if len(page.Records) > 0 {
			return recordData(page.Records[0], nil, nil), nil
		}
		props, err := resolveMap(r, s.Properties)
		if err != nil {
			return nil, err
		}
		rec, err := rt.Data.Put(ctx, actor, substrate.PutInput{Kind: s.Kind, Properties: dropEmpty(props)})
		if err != nil {
			return nil, err
		}
		return recordData(rec, nil, nil), nil
	case "read":
		id, err := resolveID(r, s.ID, "read")
		if err != nil {
			return nil, err
		}
		page, err := rt.Data.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{s.Kind}, IDs: []string{id}}, First: 1})
		if err != nil {
			return nil, err
		}
		if len(page.Records) == 0 {
			return nil, fmt.Errorf("%w: no %s %s", substrate.ErrNotFound, s.Kind, id)
		}
		return rt.into(st, s, recordData(page.Records[0], nil, nil))
	case "call":
		input, err := r.resolve(s.Input)
		if err != nil {
			return nil, err
		}
		if input == nil {
			input = map[string]any{}
		}
		out, _, err := rt.Data.CallFunction(ctx, actor, s.Function, input)
		if err != nil {
			return nil, err
		}
		return rt.into(st, s, out)
	case "navigate":
		params, err := resolveMap(r, s.Params)
		if err != nil {
			return nil, err
		}

		st.Screen = s.Screen
		st.Params = normalize(params).(map[string]any)
		return nil, nil
	case "set":
		for _, p := range sortedKeys(s.Values) {
			v, err := r.resolve(s.Values[p])
			if err != nil {
				return nil, err
			}
			rel, _ := uiPath(p)
			if err := pointerSet(st.UI, rel, normalize(v)); err != nil {
				return nil, err
			}
		}
		return nil, nil
	case "notify":
		return r.resolve(s.Text)
	}
	return nil, fmt.Errorf("unknown op %q", s.Op)
}

// into keeps a read's or a call's result, or the part pick names, at the
// step's /ui path, so an input can start from a value the owner saved.
func (rt *Runtime) into(st *substrate.AppletState, s *Step, out any) (any, error) {
	out = normalize(out)
	if s.Pick != "" {
		out, _ = pointerGet(out, s.Pick)
	}
	if s.Into != "" {
		p, _ := uiPath(s.Into)
		if err := pointerSet(st.UI, p, clone(out)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func resolveMap(r *resolver, m map[string]any) (map[string]any, error) {
	if m == nil {
		return map[string]any{}, nil
	}
	v, err := r.resolve(m)
	if err != nil {
		return nil, err
	}
	return v.(map[string]any), nil
}

// errNoRecord is an id that resolved to nothing: a screen opened without
// the record it is about, or a step whose record went missing.
var errNoRecord = errors.New("no record named")

// noRecord is a validation error that is also errNoRecord, so the problem
// reaches the renderer by path and a screen's entry can tell it apart.
type noRecord struct{ *substrate.ValidationError }

func (e noRecord) Unwrap() []error { return []error{e.ValidationError, errNoRecord} }

func resolveID(r *resolver, v any, verb string) (string, error) {
	got, err := r.resolve(v)
	if err != nil {
		return "", err
	}
	id, _ := got.(string)
	if id == "" {
		return "", noRecord{&substrate.ValidationError{Problems: []string{"id: resolved to nothing, so there is no record to " + verb}}}
	}
	return id, nil
}

// dropEmpty leaves out of a create the properties that resolved to nothing,
// so an untouched optional input writes no empty value.
func dropEmpty(props map[string]any) map[string]any {
	out := make(map[string]any, len(props))
	for k, v := range props {
		if !empty(v) {
			out[k] = v
		}
	}
	return out
}

// destructive names the actions with a delete step.
func (d *Def) destructive() []string {
	var out []string
	for _, name := range sortedKeys(d.Actions) {
		for _, s := range d.Actions[name] {
			if s.Op == "delete" {
				out = append(out, name)
				break
			}
		}
	}
	return out
}

// confirmLabel is the answer that runs an action: the verb of its first
// writing step, so a question about a delete is answered "Delete".
func confirmLabel(steps []*Step) (string, bool) {
	for _, s := range steps {
		if s.Op == "delete" {
			return "Delete", true
		}
	}
	for _, s := range steps {
		switch s.Op {
		case "create", "ensure":
			return "Add", false
		case "patch":
			return "Save", false
		}
	}
	return "Continue", false
}
