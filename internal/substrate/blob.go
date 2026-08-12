package substrate

// BlobDigestPrefix is the fixed prefix of every content-addressed blob's
// digest, which is also the blob record's id: "blob-sha256-" + lowercase hex.
// One hash function, named in the id, so a future algorithm is a new prefix
// and never an ambiguous digest.
const BlobDigestPrefix = "blob-sha256-"

// BlobStatus is a blob record's manifest status: bytes pending upload, stored
// and verified, or a failed store. It reads back as the blob record's `status`
// state and inside a resolved blob-ref.
type BlobStatus string

const (
	BlobPending BlobStatus = "pending"
	BlobStored  BlobStatus = "stored"
	BlobFailed  BlobStatus = "failed"
)

// BlobInfo is a blob's manifest — the metadata every blob record carries and
// the shape a blob-ref property resolves to on reads. The bytes themselves are
// NEVER here: they live in the content-addressed byte store, streamed through
// GET /blobs/{digest}.
type BlobInfo struct {
	// Digest is the content hash and the blob record's id
	// ("blob-sha256-<hex>").
	Digest   string     `json:"digest"`
	Size     int64      `json:"size"`
	MimeType string     `json:"mimeType,omitempty"`
	Status   BlobStatus `json:"status"`
}
