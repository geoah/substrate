package api

import (
	"fmt"
	"log/slog"
	"mime"
	"net/http"

	"github.com/geoah/substrate/internal/substrate"
)

// exportRoute is the owner's recovery export, a non-record endpoint at the
// version root (decision 0066).
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
// `snapshot.json` says the same to whoever extracts what arrived.
func (h *handler) getExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	exporter, ok := DatasetFrom(ctx).(substrate.Exporter)
	if !ok {
		writeUnsupported(w, "this service does not export a repository")
		return
	}
	export, err := exporter.Export(ctx)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	point := export.Point()
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment",
		map[string]string{"filename": fmt.Sprintf("%s-%d.tar", point.Authority, point.Head)}))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	// The headers reach the client before the first blob is read, so a
	// client opens its output file against a 200 and a mid-stream failure
	// is a cut body, never a request that got no answer.
	_ = http.NewResponseController(w).Flush()
	if _, err := export.WriteTo(w); err != nil {
		slog.Error("export aborted mid-stream", "repository", point.Authority, "head", point.Head, "error", err)
		panic(http.ErrAbortHandler)
	}
}
