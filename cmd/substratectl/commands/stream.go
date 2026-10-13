package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/geoah/substrate/internal/substrate"
)

// streamChanges prints one line per change from an ndjson watch stream to
// out. Heartbeat lines ("{}") are skipped; the leading bookmark is reported
// once, with the history generation a resume must hand back beside its seq.
//
// The stream ends one of two ways. The server's terminal error frame is
// returned as an *apiError, so the command prints the problem and exits 1
// rather than reading a failed stream as a finished one. A clean end, which
// every server shutdown makes, prints to errOut the cursor to resume from.
func streamChanges(out, errOut io.Writer, r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	header := false
	// The resume cursor: the bookmark, then each change's seq.
	var (
		opened     bool
		cursor     int64
		generation string
	)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var probe struct {
			Bookmark   *int64                  `json:"bookmark"`
			Generation string                  `json:"generation"`
			Seq        *int64                  `json:"seq"`
			Error      *substrate.ErrorPayload `json:"error"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			continue
		}
		if probe.Error != nil {
			return streamError(*probe.Error, opened, cursor, generation)
		}
		if probe.Bookmark != nil {
			opened, cursor, generation = true, *probe.Bookmark, probe.Generation
			if probe.Generation != "" {
				fmt.Fprintf(out, "# watching from seq %d, generation %s\n", *probe.Bookmark, probe.Generation)
			} else {
				fmt.Fprintf(out, "# watching from seq %d\n", *probe.Bookmark)
			}
			continue
		}
		if probe.Seq == nil {
			continue // heartbeat
		}
		var c substrate.Change
		if err := json.Unmarshal(line, &c); err != nil {
			continue
		}
		cursor = c.Seq
		if !header {
			fmt.Fprintln(out, changeHeader)
			header = true
		}
		fmt.Fprintln(out, formatChange(c))
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read watch stream: %w", err)
	}
	if opened {
		fmt.Fprintf(errOut, "# stream ended after seq %d; resume with %s\n", cursor, resumeFlags(cursor, generation))
	}
	return nil
}

// streamError is the terminal error frame as the error the command fails
// with: the server's problem object, and where to resume once it is fixed.
func streamError(p substrate.ErrorPayload, opened bool, cursor int64, generation string) *apiError {
	ae := &apiError{
		Code:           p.Code,
		Message:        p.Message,
		Problems:       p.Problems,
		ProblemDetails: p.ProblemDetails,
		Head:           p.Head,
		Generation:     p.Generation,
		Headline:       "the server ended the watch stream with an error (" + p.Code + ")",
	}
	if opened {
		ae.Hint = "resume with " + resumeFlags(cursor, generation)
	}
	return ae
}

// resumeFlags is the --from and --generation pair that resumes after cursor.
func resumeFlags(cursor int64, generation string) string {
	if generation == "" {
		return fmt.Sprintf("--from %d", cursor)
	}
	return fmt.Sprintf("--from %d --generation %s", cursor, generation)
}
