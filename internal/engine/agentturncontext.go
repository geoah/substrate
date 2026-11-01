package engine

// The turn context (decision 0196): the block the engine sends after a
// turn's own text, the minute in the owner's zone and the records the turn
// is about. It is rendered ONCE, when the turn opens, and stored on the row
// the turn opens with (`context`, `contextRecords`); replay sends the stored
// bytes, because a cached prompt prefix holds only while every request
// repeats the first one's.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	// timezoneSettingID is the repository's zone: a core `setting` under the
	// llm package's prefix (decision 0076), like spendSettingID. Nothing
	// seeds it, so an absent row or an empty value is UTC.
	timezoneSettingID = "substrate.reamde.dev/llm/timezone"

	// propMsgContext and propMsgContextRecords are where a message row keeps
	// the turn context it was sent with.
	propMsgContext        = "context"
	propMsgContextRecords = "contextRecords"

	// turnContextRecordRunes caps one record's rendering: a record is what a
	// person points at, and a large one is queried, not pasted.
	turnContextRecordRunes = 4000
)

// turnContext is one rendered block and the records it names.
type turnContext struct {
	text    string
	records []any
}

// stamp writes the block onto a message row's properties.
func (tc turnContext) stamp(props map[string]any) {
	if tc.text != "" {
		props[propMsgContext] = tc.text
	}
	if len(tc.records) > 0 {
		props[propMsgContextRecords] = tc.records
	}
}

// loadZone resolves an IANA zone name. `Local` is refused: it names the
// server's zone, which is nobody's.
func loadZone(name string) (*time.Location, error) {
	if name == "" || name == "Local" {
		return nil, fmt.Errorf("%q is not an IANA time zone", name)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("%q is not an IANA time zone this server knows", name)
	}
	return loc, nil
}

// repositoryZone is the repository's own zone: the setting's value, or UTC
// where there is none.
func (ds *dataset) repositoryZone(ctx context.Context) *time.Location {
	return zoneOf(ds.loadRowDB(ctx, eref{Kind: kindSetting, ID: timezoneSettingID}))
}

// zoneOf reads the zone setting's row. A value the guard would refuse now
// (a zone database that lost a name) reads as UTC rather than failing every
// run.
func zoneOf(row *erow, err error) *time.Location {
	if err != nil || row == nil || row.DeletedAt != nil {
		return time.UTC
	}
	value := propString(row, propSettingValue)
	if value == "" {
		return time.UTC
	}
	loc, err := loadZone(value)
	if err != nil {
		return time.UTC
	}
	return loc
}

// guardTimezoneSetting holds the repository's zone to what the engine reads
// it as: an empty value (UTC) or a zone this server loads.
func guardTimezoneSetting(value string) error {
	if value == "" {
		return nil
	}
	if _, err := loadZone(value); err != nil {
		return fmt.Errorf("%w: setting %s: %w", substrate.ErrValidation, timezoneSettingID, err)
	}
	return nil
}

// turnNow renders the clock line's value: the minute, in loc, with its
// weekday and the zone's name.
func turnNow(at time.Time, loc *time.Location) string {
	local := at.In(loc).Truncate(time.Minute)
	return fmt.Sprintf("%s (%s), %s", local.Format("2006-01-02T15:04:05-07:00"), local.Weekday(), loc.String())
}

// renderTurnContext renders the block: the clock, then each record.
func renderTurnContext(at time.Time, loc *time.Location, about []*substrate.Record) turnContext {
	var b strings.Builder
	b.WriteString("<turn_context>\nnow: ")
	b.WriteString(turnNow(at, loc))
	b.WriteString("\n")
	var records []any
	for _, r := range about {
		b.WriteString("about: substrate://")
		b.WriteString(vocabulary.RecordPath(r.Kind, r.ID))
		b.WriteString("\n  ")
		b.WriteString(renderContextRecord(r))
		b.WriteString("\n")
		records = append(records, map[string]any{"kind": r.Kind, "id": r.ID})
	}
	b.WriteString("</turn_context>")
	return turnContext{text: b.String(), records: records}
}

// renderContextRecord is one record as compact JSON (sorted keys, so the
// same record renders the same bytes), cut at turnContextRecordRunes.
func renderContextRecord(r *substrate.Record) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]any{"kind": r.Kind, "title": r.Title, "properties": r.Properties}); err != nil {
		return "{}"
	}
	out := strings.TrimSpace(buf.String())
	if cut := cutRunes(out, turnContextRecordRunes); cut != out {
		return cut + " …(cut: query the record for the rest)"
	}
	return out
}

// chatTurnContext validates a chat turn's zone and records and renders its
// block, before the turn opens: a refusal here is the request's own 4xx.
// Each record is read under the agent's `reads` grant, and one it does not
// reach refuses the turn, so an agent never answers about a record it was
// not shown.
func (ds *dataset) chatTurnContext(ctx context.Context, ag *vocabulary.Agent, in substrate.ChatInput, at time.Time) (turnContext, error) {
	var loc *time.Location
	if in.Timezone != "" {
		l, err := loadZone(in.Timezone)
		if err != nil {
			return turnContext{}, fmt.Errorf("%w: timezone: %w", substrate.ErrValidation, err)
		}
		loc = l
	} else {
		loc = ds.repositoryZone(ctx)
	}
	if len(in.Context) > substrate.MaxChatContextRecords {
		return turnContext{}, fmt.Errorf("%w: context names %d records, and a turn is about at most %d",
			substrate.ErrValidation, len(in.Context), substrate.MaxChatContextRecords)
	}
	var about []*substrate.Record
	seen := map[string]bool{}
	for _, path := range in.Context {
		rec, err := ds.contextRecord(ctx, ag, path)
		if err != nil {
			return turnContext{}, err
		}
		if key := vocabulary.RecordPath(rec.Kind, rec.ID); !seen[key] {
			seen[key] = true
			about = append(about, rec)
		}
	}
	return renderTurnContext(at, loc, about), nil
}

// contextRecord reads one record a chat turn names, under the agent's grant.
// The kind is checked before the read, so a kind outside the grant is
// refused by name whether or not the record exists.
func (ds *dataset) contextRecord(ctx context.Context, ag *vocabulary.Agent, path string) (*substrate.Record, error) {
	kind, id, ok := vocabulary.SplitRecordPath(path)
	if !ok {
		return nil, fmt.Errorf("%w: context %q is not a record path, <authority>/<package>/<kind>/<id>", substrate.ErrValidation, path)
	}
	if ag.Reads == nil {
		return nil, fmt.Errorf("%w: context %s: agent %s declares no reads, so it is shown no records", substrate.ErrValidation, path, ag.Identity())
	}
	ty, err := ds.resolveType(kind)
	if err != nil {
		return nil, fmt.Errorf("%w: context %s: %w", substrate.ErrValidation, path, err)
	}
	scope := queryScope{kinds: ag.Reads.Kinds}
	if !scope.allows(ty.Identity) {
		return nil, fmt.Errorf("%w: context %s: agent %s does not read %s: add it to the agent's reads, or ask another agent",
			substrate.ErrValidation, path, ag.Identity(), ty.Identity)
	}
	rec, err := ds.Get(ctx, ty.Identity, id)
	if err != nil {
		return nil, fmt.Errorf("context %s: %w", path, err)
	}
	return rec, nil
}
