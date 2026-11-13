package api

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// consoleAppSchemeHandler is the Console app's scheme handler, whose
// isSubstratePath decides which requests of the bundled console reach the
// server and which the bundle answers.
const consoleAppSchemeHandler = "../../apps/console/apple/Sources/Shell/ConsoleSchemeHandler.swift"

var consoleAppDoorList = regexp.MustCompile(`let door = \[([^\]]*)\]`)

// The bundled console calls every route of the auth feature at the root,
// beside its own pages, so the Console app's list of door paths
// (isSubstratePath) has to send each one to the server: a route the list
// misses is answered with the console's own HTML, and the call fails in the
// app alone. The app has no test target, so the list is held here, by the
// table the routes are placed in.
func TestTheConsoleAppProxiesEveryAuthRoute(t *testing.T) {
	raw, err := os.ReadFile(consoleAppSchemeHandler)
	if err != nil {
		t.Fatalf("read %s: %v", consoleAppSchemeHandler, err)
	}
	m := consoleAppDoorList.FindSubmatch(raw)
	if m == nil {
		t.Fatalf("%s holds no `let door = [...]` list", consoleAppSchemeHandler)
	}
	var door []string
	for _, item := range strings.Split(string(m[1]), ",") {
		door = append(door, strings.Trim(strings.TrimSpace(item), `"`))
	}
	proxied := func(path string) bool {
		for _, p := range door {
			if path == p || strings.HasPrefix(path, p+"/") {
				return true
			}
		}
		return false
	}
	for _, f := range featureTable {
		if f.name != "auth" {
			continue
		}
		for _, route := range f.routes {
			_, path, _ := strings.Cut(route, " ")
			if !proxied(path) {
				t.Errorf("the Console app does not proxy %s: add it to the door list in %s", route, consoleAppSchemeHandler)
			}
		}
	}
}
