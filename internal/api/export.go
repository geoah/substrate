package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"time"
)

// exportRoute is the owner's recovery export, a non-record endpoint at the
// version root (decision 0069).
const exportRoute = "/export"

// getExport is GET /api/v1/export: the repository's directory as of one
// committed point, streamed as a tar (`application/x-tar`) laid out as a data
// root, with `snapshot.json` recording the point as its last entry. The
// bearer token is the whole credential: a token already reads every record
// and blob the archive carries, and its sealed files are ciphertext under a
// key the archive does not hold.
//
// The point is pinned before the status is written, so a refusal (a read-only
// process, a directory latched behind the tables) is an ordinary error body.
// Once the point is pinned the 200 is flushed at once, and a failure while
// streaming cannot take it back: the response is aborted instead, which a
// client reads as an unexpected end of the body, and the archive's missing
// `snapshot.json` says the same to whoever extracts what arrived. A client
// that takes longer than streamWriteDeadline over one write, and every export
// still running when the server starts to shut down, ends the same way.
func (h *handler) getExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ds := DatasetFrom(ctx)
	export, err := ds.Export(ctx)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	point := export.Point()
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment",
		map[string]string{"filename": fmt.Sprintf("%s-%d.tar", point.Authority, point.Head)}))
	w.WriteHeader(http.StatusOK)
	// The headers reach the client before the first blob is read, so a
	// client opens its output file against a 200 and a mid-stream failure
	// is a cut body, never a request that got no answer.
	rc := http.NewResponseController(w)
	_ = rc.Flush()
	if _, err := export.WriteTo(&exportWriter{w: w, rc: rc, draining: h.draining}); err != nil {
		// A client that leaves mid-stream fails the next write to it with an
		// error carrying no context.Canceled, so the request's own context is
		// what says the client went away. net/http cancels that context on
		// any failed write, a timed-out one included, so the timeout is told
		// apart first.
		attrs := append(requestLogAttrs(r), "head", point.Head, "error", err)
		switch {
		case errors.Is(err, os.ErrDeadlineExceeded):
			slog.Warn("export aborted mid-stream: the client stopped reading", attrs...)
		case errors.Is(ctx.Err(), context.Canceled):
			slog.Debug("export aborted mid-stream: the client went away", attrs...)
		case h.isDraining():
			slog.Warn("export aborted mid-stream: the server is shutting down", attrs...)
		default:
			slog.Error("export aborted mid-stream", attrs...)
		}
		_ = rc.Flush()
		panic(http.ErrAbortHandler)
	}
}

// errShuttingDown fails an export's next write once the server has started
// to shut down.
var errShuttingDown = errors.New("the server is shutting down")

// exportWriter is the export's view of the response: every write has
// streamWriteDeadline to reach the client, and none starts once the server is
// shutting down, because http.Server.Shutdown waits for the export and an
// archive of many gigabytes outlasts any shutdown budget.
type exportWriter struct {
	w        io.Writer
	rc       *http.ResponseController
	draining <-chan struct{}
}

func (e *exportWriter) Write(p []byte) (int, error) {
	select {
	case <-e.draining:
		return 0, errShuttingDown
	default:
	}
	_ = e.rc.SetWriteDeadline(time.Now().Add(streamWriteDeadline))
	return e.w.Write(p)
}
