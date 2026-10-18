package commands

import (
	"strings"
	"testing"
)

// reindex names the index it re-derives, and refuses without --observed
// before it reads the DSN or opens anything.
func TestRepositoryReindexNamesTheIndex(t *testing.T) {
	h := newHarness(t)
	t.Setenv("SUBSTRATE_DATA_ROOT", t.TempDir())
	_, _, err := h.run("repository", "reindex", "ada", "--dsn", "postgres://u@127.0.0.1:1/none?sslmode=disable")
	if err == nil || !strings.Contains(err.Error(), "--observed") {
		t.Fatalf("reindex without --observed: err = %v, want a refusal naming --observed", err)
	}
	_, _, err = h.run("repository", "reindex", "ada", "--observed", "--dsn", "postgres://u@127.0.0.1:1/none?sslmode=disable")
	if err == nil || strings.Contains(err.Error(), "--observed") {
		t.Fatalf("reindex --observed against an unreachable database: err = %v, want the connection's failure", err)
	}
}
