package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

// A deleted record's history is the owner's, so unset, the compaction pass
// forgets no deletion (decision 0334); a deployment that wants forgetting
// sets a positive duration, and a negative one is refused, naming the
// variable. Setenv, so not parallel.
func TestDeletionsAreKeptForeverUnlessADurationIsSet(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/substrate")
	t.Setenv("SUBSTRATE_DATA_ROOT", t.TempDir())
	t.Setenv("PORT", "8080")
	t.Setenv("SUBSTRATE_DELETIONS_KEPT", "")
	_ = os.Unsetenv("SUBSTRATE_DELETIONS_KEPT")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Compaction.DeletionsKept != 0 {
		t.Fatalf("unset, SUBSTRATE_DELETIONS_KEPT = %s, want 0: every deletion kept", c.Compaction.DeletionsKept)
	}
	if err := c.Compaction.Validate(); err != nil {
		t.Fatalf("the default was refused: %v", err)
	}

	t.Setenv("SUBSTRATE_DELETIONS_KEPT", "720h")
	if c, err = Load(); err != nil {
		t.Fatal(err)
	}
	if c.Compaction.DeletionsKept != 720*time.Hour {
		t.Fatalf("set to 720h, SUBSTRATE_DELETIONS_KEPT = %s", c.Compaction.DeletionsKept)
	}
	if err := c.Compaction.Validate(); err != nil {
		t.Fatalf("a positive duration was refused: %v", err)
	}

	if err := (Compaction{DeletionsKept: -time.Hour}).Validate(); err == nil || !strings.Contains(err.Error(), "SUBSTRATE_DELETIONS_KEPT") {
		t.Fatalf("a negative duration: err = %v, want a refusal naming SUBSTRATE_DELETIONS_KEPT", err)
	}
}
