package substrate

import (
	"io"
	"time"
)

// Export is one pinned export: the point it holds and the stream of it. A
// caller that pins streams: the repository's one export slot is held from
// Export until WriteTo returns, however it returns, so a second Export in
// between is refused with ErrConflict, and an Export that is never streamed
// keeps the slot for the life of the process.
type Export interface {
	// Point is the committed point the stream holds, known before the first
	// byte is written, so a server names it in the response headers.
	Point() ExportPoint
	// WriteTo streams the tar to w and returns the bytes written. An error
	// after the first byte leaves w holding an incomplete archive, which the
	// reader tells by the missing `snapshot.json`, the last entry.
	WriteTo(w io.Writer) (int64, error)
}

// ExportPoint is the committed point an export holds: the repository and the
// head seq and checksum its snapshot.json records, with what the stream
// carries counted.
type ExportPoint struct {
	// Authority is the repository's id, the directory the archive lays out
	// under `repositories/`.
	Authority string
	// Head is the seq of the last committed entry in the stream, and HeadHash
	// that entry's checksum in hex; both are recorded in the stream's
	// snapshot.json and `repository verify` holds a restore to them.
	Head     int64
	HeadHash string
	TakenAt  time.Time
	// Segments counts the changelog files, SealedFiles the files under
	// sealed/ and Blobs the stored blob manifests whose bytes the stream
	// carries.
	Segments    int
	SealedFiles int
	Blobs       int
}
