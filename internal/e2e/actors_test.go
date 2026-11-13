package e2e

import (
	"strings"
	"sync"
)

// actorKind is the collection of the actor records a change links (decision
// 0270).
const actorKind = "/api/v1/substrate.reamde.dev/core/actor"

var (
	handsMu sync.Mutex
	hands   = map[string]string{}
)

// hand names the actor record a change, a manager or a stored value links, in
// the words the cases compare: its type, and its link where it has one —
// `person`, `system`, `app local`, `agent <authority>/<package>/<name>`,
// `function <authority>/<package>/<name>`, `package <authority>/<package>`.
// The record is read once over HTTP, as any client reads it.
func (c *C) hand(id string) string {
	c.t.Helper()
	if id == "" {
		return ""
	}
	key := c.r.authority + "\x00" + id
	handsMu.Lock()
	named, ok := hands[key]
	handsMu.Unlock()
	if ok {
		return named
	}
	rec := c.getRec(actorKind, id)
	typ := rec.prop("type")
	named = typ
	switch typ {
	case "app":
		named += " " + rec.prop("publisher")
	case "function", "agent", "package":
		for _, path := range refPaths(rec, typ) {
			named += " " + strings.TrimPrefix(path, "substrate.reamde.dev/core/"+typ+"/")
		}
	}
	handsMu.Lock()
	hands[key] = named
	handsMu.Unlock()
	return named
}
