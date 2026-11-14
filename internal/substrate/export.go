package substrate

import (
	"io"
	"time"
)

// Export is one pinned export: the point it holds and the stream of it. The
// repository's one export slot is held from Export until WriteTo returns,
// however it returns, or Close lets it go, so a second Export in between is
// refused with ErrConflict; a caller that pins and does not stream closes.
type Export interface {
	// Point is the committed point the stream holds, known before the first
	// byte is written, so a server names it in the response headers.
	Point() ExportPoint
	// WriteTo streams the tar to w and returns the bytes written. An error
	// after the first byte leaves w holding an incomplete archive, which the
	// reader tells by the missing manifest, the last entry.
	WriteTo(w io.Writer) (int64, error)
	// Close lets go of what the pin holds for an export that will not be
	// written. WriteTo lets go of it when it returns, so Close after it does
	// nothing.
	Close()
}

// ExportPoint is the committed point an export holds: the repository and the
// head seq and checksum its manifest records, with what the stream carries
// counted.
type ExportPoint struct {
	// ID is the repository's id, the directory the archive lays out under
	// `repositories/`, and Authority its name, which the download is named
	// after.
	ID        string
	Authority string
	// Head is the seq of the last committed entry in the stream, and HeadHash
	// that entry's checksum in hex; both are recorded in the stream's
	// manifest and `repository verify` holds a restore to them.
	Head     int64
	HeadHash string
	TakenAt  time.Time
	// Pieces counts the history's pieces, set once the stream has written
	// them; SealedFiles the sealed values and Blobs the stored blob manifests
	// whose bytes the stream carries.
	Pieces      int
	SealedFiles int
	Blobs       int
}
