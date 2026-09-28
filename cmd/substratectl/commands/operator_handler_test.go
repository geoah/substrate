package commands

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
)

// The operator hat's logger drops the engine's info lines (the boot check,
// whose outcome the command's own report covers) and passes warnings and the
// progress lines a long verify, rebuild or snapshot prints, also through a
// logger derived with attributes (issue 745).
func TestOperatorHandlerPassesProgressAndWarningsOnly(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(operatorHandler{slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})})
	log.Debug("substrate: debug")
	log.Info("substrate: boot check started")
	log.Info("substrate: verifying the changelog files", "seq", 5, engine.ProgressKey, true)
	log.With("repository", "ada").Info("substrate: replaying the changelog into the fold", engine.ProgressKey, true)
	log.Warn("substrate: the changelog file was behind the table at open and was caught up")
	out := buf.String()
	for _, absent := range []string{"substrate: debug", "boot check started"} {
		if strings.Contains(out, absent) {
			t.Errorf("stderr carries %q:\n%s", absent, out)
		}
	}
	for _, present := range []string{
		`msg="substrate: verifying the changelog files" seq=5 progress=true`,
		`msg="substrate: replaying the changelog into the fold" repository=ada progress=true`,
		`level=WARN msg="substrate: the changelog file was behind the table at open and was caught up"`,
	} {
		if !strings.Contains(out, present) {
			t.Errorf("stderr lacks %q:\n%s", present, out)
		}
	}
}
