package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// A collector parses LOG_FORMAT=json line by line, so every line must be one
// JSON object carrying the fields as keys; text stays key=value.
func TestLogHandlerWritesTheFormatAndLevelAsked(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := slog.New(logHandler(&buf, "warn", "JSON"))
	log.Info("dropped")
	log.Warn("kept", "repository", "alice.example.com")
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("json output %q is not one JSON object: %v", buf.String(), err)
	}
	if line["msg"] != "kept" || line["level"] != "WARN" || line["repository"] != "alice.example.com" {
		t.Errorf("json line = %v", line)
	}

	for _, format := range []string{"", "text", "nonsense"} {
		buf.Reset()
		slog.New(logHandler(&buf, "info", format)).Info("kept", "repository", "alice.example.com")
		if out := buf.String(); !strings.Contains(out, "msg=kept repository=alice.example.com") {
			t.Errorf("format %q wrote %q, want text", format, out)
		}
	}
}
