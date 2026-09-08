package substrate

import (
	"context"
	"io"
	"time"
)

// Exporter is the owner's recovery export, an optional Dataset extension (see
// Dataset): the repository's directory as of one committed point, streamed as
// a tar in the layout a data root has and the format an operator's
// `repository snapshot` writes (decision 0066). The bearer token is the whole
// credential, because a token already reads every record and every blob the
// stream carries, and the sealed files in it are ciphertext under a key the
// stream does not hold.
type Exporter interface {
	// Export pins a committed point of the repository and returns the export
	// that streams it. Pinning is short and serializes with the repository's
	// writes; the streaming does not, so writes go on while a client
	// downloads and the stream stays the point it pinned. The context is the
	// stream's too: a caller that goes away ends it.
	Export(ctx context.Context) (Export, error)
}

// Export is one pinned export: the point it holds and the stream of it.
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
