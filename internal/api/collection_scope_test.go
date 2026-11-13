package api

import (
	"testing"
)

const tasksPath = "/api/v1/samples.substrate.reamde.dev/tasks/task"

// createPerson returns the id of a person record created through the API.
func createPerson(t *testing.T, env *testEnv, tok string) string {
	t.Helper()
	return createRecord(t, env, tok, personKind, map[string]any{"title": "Ada", "name": "Ada"}).ID
}
