package e2e

import (
	"net/http"
)

// The story-local package: the import kind an "importer" writes, the match
// audit kind, the deterministic functions, and the three agents. Applied
// through the same vocabulary door a user's own declarations would use;
// none of this ships in kinds/.
const (
	storyAuthority = "stories.e2e.example"
	storyPackage   = "stories"
	storyPkg       = storyAuthority + "/" + storyPackage

	importCollection  = "/api/v1/" + storyPkg + "/eventimport"
	verdictCollection = "/api/v1/" + storyPkg + "/matchverdict"

	triggerCollection  = "/api/v1/substrate.reamde.dev/core/trigger"
	providerCollection = "/api/v1/substrate.reamde.dev/core/llmprovider"
	requestCollection  = "/api/v1/substrate.reamde.dev/core/recordpatchrequest"
	runCollection      = "/api/v1/substrate.reamde.dev/core/run"

	transcriptKind = "samples.substrate.reamde.dev/calendar/transcript"
	eventKind      = "samples.substrate.reamde.dev/calendar/calendarevent"
	taskKind       = "samples.substrate.reamde.dev/tasks/task"

	actorResolver   = "function:" + storyAuthority + ":" + storyPackage + ":resolveattendees"
	actorMatcher    = "agent:" + storyAuthority + ":" + storyPackage + ":transcriptMatcher"
	actorReflection = "agent:" + storyAuthority + ":" + storyPackage + ":actionItemExtractor"
	actorArbiter    = "agent:" + storyAuthority + ":" + storyPackage + ":changeRequestReviewer"
)

// resolveAttendeesSource mirrors what a calendar importer's linker does:
// resolve every attendee email against the people the repository knows, mint
// a person for a stranger (named from the email's local part, org matched by
// domain), and never mint one for an automated room or group address.
const resolveAttendeesSource = `
def automated(email):
    domain = email.split("@", 1)[-1]
    return domain.endswith("resource.calendar.example") or domain.endswith("group.calendar.example")


def main(input, host):
    envelope = input.get("envelope") or {}
    rec = envelope.get("record") or {}
    props = rec.get("properties") or {}
    imp_id = rec.get("id") or ""
    event_id = "ev-" + imp_id.removeprefix("imp-")

    # The two lists together stay under the function's default 500-row read
    # budget; the fixture world is dozens of records, not hundreds.
    people = host.records.list(["samples.substrate.reamde.dev/people/person"], first=300) or {}
    by_email = {}
    for p in people.get("records") or []:
        for e in (p.get("properties") or {}).get("emails") or []:
            by_email[e.lower()] = p["id"]
    orgs = host.records.list(["samples.substrate.reamde.dev/people/organization"], first=100) or {}
    by_domain = {}
    for o in orgs.get("records") or []:
        d = ((o.get("properties") or {}).get("domain") or "").lower()
        if d:
            by_domain[d] = o["id"]

    attendees = []
    for raw in props.get("attendeeEmails") or []:
        email = raw.strip().lower()
        if not email or automated(email):
            continue
        pid = by_email.get(email)
        if not pid:
            # Naive on purpose (the v0 rule): the id derives from the local
            # part alone, so two strangers sharing one local part collide.
            local = email.split("@", 1)[0]
            pid = "person-" + "".join(ch if ch.isalnum() else "-" for ch in local)
            person = {"name": local.replace(".", " ").title(), "emails": [email]}
            org = by_domain.get(email.split("@", 1)[-1])
            if org:
                # memberOf carries link data, so the org sits under "ref".
                person["memberOf"] = [{"ref": "samples.substrate.reamde.dev/people/organization/" + org}]
            host.effects.put(
                "samples.substrate.reamde.dev/people/person", pid,
                properties=person, if_absent=True)
            by_email[email] = pid
        if pid not in attendees:
            attendees.append(pid)

    event = {
        "summary": props.get("summary") or "",
        "at": props.get("at"),
        "endsAt": props.get("endsAt"),
        "calendar": "samples.substrate.reamde.dev/calendar/calendar/work",
        "attendees": ["samples.substrate.reamde.dev/people/person/" + p for p in attendees],
    }
    organizer = (props.get("organizerEmail") or "").strip().lower()
    if organizer and not automated(organizer) and by_email.get(organizer):
        event["organizer"] = "samples.substrate.reamde.dev/people/person/" + by_email[organizer]
    host.effects.put("samples.substrate.reamde.dev/calendar/calendarevent", event_id, properties=event)
    return {"output": {"event": event_id, "attendees": len(attendees)}}
`

// scoreCandidatesSource is the deterministic half of transcript matching:
// every event within 90 minutes, scored on start-time proximity (0.6) and
// title token overlap (0.4). The arithmetic is here; the DECISION stays with
// the agent that calls it.
const scoreCandidatesSource = `
import datetime
import re

STOP = {"the", "a", "an", "of", "for", "and", "to", "in", "on"}


def parse(s):
    if not s:
        return None
    try:
        return datetime.datetime.fromisoformat(str(s).replace("Z", "+00:00"))
    except ValueError:
        return None


def tokens(s):
    return {w for w in re.split(r"[^a-z0-9]+", (s or "").lower()) if w and w not in STOP}


def main(input, host):
    args = input.get("args") or {}
    tid = args["transcript"]
    tr = host.records.get("samples.substrate.reamde.dev/calendar/transcript", tid) or {}
    tp = tr.get("properties") or {}
    t_at = parse(tp.get("at"))
    t_title = tokens(tp.get("name"))

    events = (host.records.list(["samples.substrate.reamde.dev/calendar/calendarevent"], first=200) or {}).get("records") or []
    out = []
    for ev in events:
        ep = ev.get("properties") or {}
        e_at = parse(ep.get("at"))
        if t_at is None or e_at is None:
            continue
        skew = abs((t_at - e_at).total_seconds()) / 60.0
        if skew > 90:
            continue
        time_score = max(0.0, 1.0 - skew / 90.0)
        e_title = tokens(ep.get("summary"))
        union = t_title | e_title
        title_score = (len(t_title & e_title) / len(union)) if union else 0.0
        score = 0.6 * time_score + 0.4 * title_score
        out.append({"event": ev.get("id"), "score": round(score, 4),
                    "signals": {"time": round(time_score, 4), "title": round(title_score, 4)}})
    out.sort(key=lambda c: (-c["score"], c["event"]))
    return {"output": {"candidates": out, "floor": 0.4}}
`

// storyDocuments is the whole story vocabulary, in one admission batch.
func storyDocuments(providerID string) []map[string]any {
	doc := func(kind, id string, data map[string]any) map[string]any {
		return map[string]any{"kind": kind, "metadata": map[string]any{"id": id}, "data": data}
	}
	kindDoc := func(name string, data map[string]any) map[string]any {
		data["authority"] = storyAuthority
		data["package"] = storyPackage
		data["names"] = map[string]any{"singular": name}
		return doc("substrate.reamde.dev/core/kind", storyPkg+"/"+name, data)
	}
	return []map[string]any{
		doc("substrate.reamde.dev/core/package", storyPkg, map[string]any{
			"authority": storyAuthority,
			"package":   storyPackage,
			"version":   1,
		}),
		kindDoc("eventimport", map[string]any{
			"description":     "A calendar event as an importer delivers it: raw emails, nothing resolved yet.",
			"displayTemplate": "{summary}",
			"traits":          []string{"temporal(range)"},
			"properties": map[string]any{
				"summary":        map[string]any{"type": "string", "description": "the event's heading"},
				"organizerEmail": map[string]any{"type": "string", "description": "the organizer's raw email"},
				"attendeeEmails": map[string]any{"type": "string", "repeated": true, "description": "the raw attendee emails, rooms included"},
			},
		}),
		kindDoc("matchverdict", map[string]any{
			"description":     "The matcher's audit: which event a transcript attached to, or why none did.",
			"displayTemplate": "{verdict}",
			"properties": map[string]any{
				"verdict": map[string]any{"type": "enum", "values": []string{"matched", "unmatched"}, "description": "what the matcher decided"},
				"score":   map[string]any{"type": "float", "description": "the winning candidate's score, 0 when unmatched"},
				"reason":  map[string]any{"type": "string", "description": "the decision, in one line"},
				"transcript": map[string]any{
					"type": "reference", "kind": transcriptKind, "mustExist": true,
					"description": "the transcript this verdict is about",
				},
				"event": map[string]any{
					"type": "reference", "kind": eventKind, "mustExist": true,
					"description": "the event it attached to, when matched",
				},
			},
		}),
		doc("substrate.reamde.dev/core/function", storyPkg+"/resolveattendees", map[string]any{
			"authority":   storyAuthority,
			"package":     storyPackage,
			"description": "Resolve an imported event's raw attendee emails into linked people; mint strangers, never rooms.",
			"runtime":     "python",
			"timeout":     "PT10S",
			"permissions": map[string]any{
				"reads": map[string]any{"kinds": []string{"samples.substrate.reamde.dev/people/person", "samples.substrate.reamde.dev/people/organization"}},
				"writes": []string{
					"samples.substrate.reamde.dev/calendar/calendarevent",
					"samples.substrate.reamde.dev/people/person",
				},
			},
			"source": resolveAttendeesSource,
		}),
		doc("substrate.reamde.dev/core/function", storyPkg+"/scorecandidates", map[string]any{
			"authority":   storyAuthority,
			"package":     storyPackage,
			"description": "Score every calendar event within 90 minutes of a transcript on time proximity and title overlap.",
			"runtime":     "python",
			"timeout":     "PT10S",
			"arguments": []map[string]any{
				{"name": "transcript", "type": "string", "required": true, "description": "the transcript record id"},
			},
			"returns": []map[string]any{
				{"name": "candidates", "type": "json", "repeated": true, "required": true, "description": "events scored, best first"},
				{"name": "floor", "type": "float", "description": "the score below which nothing matches"},
			},
			"permissions": map[string]any{
				"reads": map[string]any{"kinds": []string{transcriptKind, eventKind}},
			},
			"source": scoreCandidatesSource,
		}),
		doc("substrate.reamde.dev/core/agent", storyPkg+"/transcriptMatcher", map[string]any{
			"authority":   storyAuthority,
			"package":     storyPackage,
			"description": "Attaches a transcript to the meeting that actually happened, or declines and says why.",
			"prompt": "You attach one transcript per run. Call scorecandidates, pick the clear winner above the " +
				"floor, link meeting and speakers with mutate, and write a matchverdict either way. " +
				"A transcript that matches nothing attaches to nothing.",
			"provider": providerID,
			"model":    "transcriptMatcher",
			"tools": []map[string]any{
				{"function": "substrate.reamde.dev/core/graphql"},
				{"function": "substrate.reamde.dev/core/mutate"},
				{"function": storyPkg + "/scorecandidates"},
			},
			"permissions": map[string]any{
				"writes": []string{transcriptKind, storyPkg + "/matchverdict"},
			},
			"budgets": map[string]any{"maxTurns": 8, "maxToolCalls": 12, "deadlineSeconds": 120},
		}),
		doc("substrate.reamde.dev/core/agent", storyPkg+"/actionItemExtractor", map[string]any{
			"authority":   storyAuthority,
			"package":     storyPackage,
			"description": "Reads a matched transcript and proposes the work it implies; writes nothing directly.",
			"prompt": "You read one matched transcript per run and propose tasks for concrete action items only, " +
				"every one naming the transcript it came from in `source`. Never guess an assignee, never " +
				"invent a person from a " +
				"speaker label, and when the meeting decided a priority, propose that patch. " +
				"When nothing was decided, propose nothing.",
			"provider": providerID,
			"model":    "actionItemExtractor",
			// Decisions must not resume the proposing thread: a resumed turn
			// re-enters the scripted model with a trimmed history, and the
			// stories assert exact proposal counts.
			"resume": "never",
			"tools": []map[string]any{
				{"function": "substrate.reamde.dev/core/graphql"},
				{"function": "substrate.reamde.dev/core/propose"},
			},
			"permissions": map[string]any{
				"writes": []string{"substrate.reamde.dev/core/recordpatchrequest"},
			},
			"budgets": map[string]any{"maxTurns": 10, "maxToolCalls": 12, "deadlineSeconds": 120},
		}),
		doc("substrate.reamde.dev/core/agent", storyPkg+"/changeRequestReviewer", map[string]any{
			"authority":   storyAuthority,
			"package":     storyPackage,
			"description": "Decides one change request per run: work without provenance is rejected.",
			"prompt": "You decide one recordpatchrequest per run, delivered in the envelope. Accept a proposal " +
				"whose diff carries its source, reject one that carries none, with one mutate call patching " +
				"the request's decision.",
			"provider": providerID,
			"model":    "changeRequestReviewer",
			"tools": []map[string]any{
				{"function": "substrate.reamde.dev/core/graphql"},
				{"function": "substrate.reamde.dev/core/mutate"},
			},
			"permissions": map[string]any{
				"writes": []string{"substrate.reamde.dev/core/recordpatchrequest", taskKind},
			},
			"budgets": map[string]any{"maxTurns": 6, "maxToolCalls": 4, "deadlineSeconds": 120},
		}),
	}
}

// applyStoryVocabulary admits the story package in one batch.
func (c *C) applyStoryVocabulary(providerID string) {
	c.t.Helper()
	status, raw := c.do(http.MethodPost, "/api/v1/vocabulary/apply",
		map[string]any{"documents": storyDocuments(providerID)}, nil)
	c.requiref(status == http.StatusOK, "vocabulary/apply of the story package answered %d: %s", status, raw)
	c.stepf("applied the story package `%s`: 2 kinds, 2 python functions, 3 agents", storyPkg)
}

// putTrigger writes one trigger record and requires it to land.
func (c *C) putTrigger(id string, props map[string]any) {
	c.t.Helper()
	status, raw := c.do(http.MethodPut, triggerCollection+"/"+id, map[string]any{"properties": props}, nil)
	c.requiref(status == http.StatusCreated || status == http.StatusOK,
		"PUT trigger %s answered %d: %s", id, status, raw)
}

// wake runs one trigger's scan now and returns how many deliveries settled.
func (c *C) wake(trigger string) int {
	c.t.Helper()
	var out struct {
		Ran int `json:"ran"`
	}
	status, raw := c.do(http.MethodPost, triggerCollection+"/"+trigger+"/wake", nil, &out)
	c.requiref(status == http.StatusOK, "waking %s answered %d: %s", trigger, status, raw)
	return out.Ran
}
