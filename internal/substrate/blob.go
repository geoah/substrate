package substrate

import "context"

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
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
	// Name and MediaType are both OPTIONAL and both descriptive: the digest is
	// the identity, so neither takes part in dedup and neither can be trusted
	// as a fact about the bytes. Absent = the uploader said nothing.
	Name      string     `json:"name,omitempty"`
	MediaType string     `json:"mediaType,omitempty"`
	Status    BlobStatus `json:"status"`
}

// BlobUpload is what a caller SAYS about the bytes it is storing, as opposed
// to what the store derives from them (the digest and the size). Every field
// is optional, and a second upload of the same bytes does not overwrite what
// the first one said — dedup is by digest, and the stored manifest wins.
type BlobUpload struct {
	// Name is a display name, typically the filename the bytes arrived as. It
	// must not contain a path separator: it names the blob, it does not
	// address anything.
	Name      string
	MediaType string
}

// BlobStore is the content-addressed byte store, an optional Dataset
// extension (see Dataset): store bytes (deriving the digest, minting the blob
// manifest) and stream them back by digest, both repository-scoped.
type BlobStore interface {
	PutBlob(ctx context.Context, actor Actor, up BlobUpload, data []byte, wantDigest string) (*BlobInfo, error)
	GetBlob(ctx context.Context, digest string) (*BlobInfo, []byte, error)
}
