package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/geoah/substrate/internal/substrate"
)

// streamChanges prints one line per change from an ndjson watch stream.
// Heartbeat lines ("{}") are skipped; the leading bookmark is reported once.
func streamChanges(w io.Writer, r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	header := false
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var probe struct {
			Bookmark *int64 `json:"bookmark"`
			Seq      *int64 `json:"seq"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			continue
		}
		if probe.Bookmark != nil {
			fmt.Fprintf(w, "# watching from seq %d\n", *probe.Bookmark)
			continue
		}
		if probe.Seq == nil {
			continue // heartbeat
		}
		var c substrate.Change
		if err := json.Unmarshal(line, &c); err != nil {
			continue
		}
		if !header {
			fmt.Fprintln(w, changeHeader)
			header = true
		}
		fmt.Fprintln(w, formatChange(c))
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read watch stream: %w", err)
	}
	return nil
}
