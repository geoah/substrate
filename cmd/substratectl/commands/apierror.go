package commands

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// apiError is the rendered form of the substrate error envelope
// {"error":{"code","message","problems"}}.
type apiError struct {
	Status     int
	Code       string
	Message    string
	Problems   []string
	RetryAfter string
	Method     string
	Path       string
	// Hint, when set, replaces the status-derived next action.
	Hint string
}

func (e *apiError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = fmt.Sprintf("request failed with status %d", e.Status)
	}
	if e.Code != "" {
		return fmt.Sprintf("%s: %s", e.Code, msg)
	}
	return msg
}

// headline turns the envelope code into a human sentence.
func (e *apiError) headline() string {
	switch e.Code {
	case "validation":
		return "the substrate rejected this write as invalid"
	case "conflict":
		return "the record changed since it was read"
	case "guard":
		// A guard is a refusal, but WHICH refusal depends on the path. Only an
		// record write runs a state machine; schema/apply and the bundle
		// lifecycle verbs guard on live data and bundle state, so calling those
		// "a transition" describes something that is not there.
		switch {
		case strings.Contains(e.Path, "/vocabulary/apply"):
			return "the schema change was refused: it would break stored data"
		case strings.Contains(e.Path, "/bundle/"):
			return "the bundle's state does not allow this"
		}
		return "that transition is not allowed for this actor"
	case "not_found":
		return "no such resource"
	case "forbidden":
		return "forbidden"
	case "auth":
		return "not authenticated"
	case "rate_limited":
		return "rate limited"
	}
	return fmt.Sprintf("request failed with status %d", e.Status)
}

// hint is the next action a person can take, or "".
func (e *apiError) hint() string {
	if e.Hint != "" {
		return e.Hint
	}
	switch {
	case e.Status == 401 || e.Code == "auth":
		return "run `substratectl login` to mint a new token"
	case e.Status == 409 || e.Code == "conflict":
		return "re-run `substratectl get` for the current version, re-apply, and retry"
	case e.Status == 429 || e.Code == "rate_limited":
		if e.RetryAfter != "" {
			return fmt.Sprintf("wait %ss and try again", e.RetryAfter)
		}
		return "wait a few seconds and try again"
	case e.Code == "guard":
		return guardHint(e.Path)
	case e.Status == 403 || e.Code == "forbidden":
		// A token has FULL access to its repository — there are no scopes and
		// no ACLs — so a forbidden is never about the token's reach. It is the
		// substrate refusing the write itself: a reserved actor, or one of the
		// two auth kinds, which change only through their own endpoints.
		return "this write is refused on principle, not for lack of access: credentials and tokens change only through `substratectl login`, `substratectl user password` and `substratectl token`, and the substrate's own actors cannot be claimed"
	}
	return ""
}

// guardHint branches on what was actually refused. A `guard` on a record
// write is a state machine saying no, and the transition guards are the place
// to look — but schema/apply guards on STORED DATA (a dropped type with live
// records, a narrowing that strands rows, a dropped callable a trigger still
// names) and the bundle verbs guard on the BUNDLE'S STATE, so pointing an
// operator at "the state transition guards" there sends them hunting for
// something that is not in play.
func guardHint(path string) string {
	switch {
	case strings.Contains(path, "/vocabulary/apply"):
		return "the problems above name the rows in the way: migrate or delete them, or keep the declaration wide enough for what is stored"
	case strings.Contains(path, "/bundle/"):
		return "check the bundle's state with `substratectl bundle status <id>` — a disabled bundle freezes its config and accounts"
	}
	return "check the state transition guards for the state this record is in"
}

// renderError writes any error human-first: headline, message, problems as a
// bullet list, then a hint.
func renderError(w io.Writer, err error) {
	var ae *apiError
	if !errors.As(err, &ae) {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	fmt.Fprintf(w, "error: %s\n", ae.headline())
	if msg := strings.TrimSpace(ae.Message); msg != "" && msg != ae.headline() {
		fmt.Fprintf(w, "  %s\n", msg)
	}
	if len(ae.Problems) > 0 {
		fmt.Fprintf(w, "  problems:\n")
		for _, p := range ae.Problems {
			fmt.Fprintf(w, "    - %s\n", p)
		}
	}
	if ae.Path != "" {
		fmt.Fprintf(w, "  request: %s %s (%d)\n", ae.Method, ae.Path, ae.Status)
	}
	if h := ae.hint(); h != "" {
		fmt.Fprintf(w, "  hint: %s\n", h)
	}
}
