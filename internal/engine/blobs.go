package engine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// BlobUploadGrace is the unreferenced-upload grace: a freshly
// stored blob is not GC-collectable until it has had this long to become
// referenced by a follow-up record write. It closes the window between
// PUT /blobs returning and the caller's separate reference write, during which
// the blob is legitimately unreferenced but must not be reaped. Production
// keeps the default; tests set it (0 for immediate collection).
var BlobUploadGrace = time.Minute

// blobLockKey is the per-digest advisory-lock key. One lock is
// taken SHARED by blob-ref validation (an uncommitted reference), and EXCLUSIVE
// by blob upload and blob collection — so GC can never delete a digest a
// still-uncommitted write is validating, and an upload never races a sweep.
func blobLockKey(digest string) string { return "blob|" + digest }

// kindBlob is the shipped blob manifest type: the metadata half of the
// content-addressed store. The bytes live in the `blobs` byte
// table; this record is their manifest, keyed by the same digest.
const kindBlob = "core.substrate.reamde.dev/blob"

// The blob record's manifest property names.
const (
	blobPropDigest    = "digest"
	blobPropSize      = "size"
	blobPropMimeType  = "mimeType"
	blobPropCreatedBy = "createdBy"
	blobStateStatus   = "status"
)

// reBlobDigest matches a blob digest — the fixed prefix plus 64 lowercase hex
// characters (a sha-256). It is both the wire form and the blob record's id.
var reBlobDigest = regexp.MustCompile(`^blob-sha256-[0-9a-f]{64}$`)

// validBlobDigest reports whether s is a well-formed blob digest.
func validBlobDigest(s string) bool { return reBlobDigest.MatchString(s) }

// blobDigest derives the content-addressed digest of data.
func blobDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return substrate.BlobDigestPrefix + hex.EncodeToString(sum[:])
}

// PutBlob stores bytes in the repository's content-addressed byte store, deriving
// (or verifying) their digest, and mints/settles the blob record manifest with
// status=stored. Dedup is by construction: the same bytes always yield the same
// digest, so a re-store is a no-op on the bytes and on the manifest. When
// wantDigest is non-empty, the derived digest must equal it (a client that
// addressed PUT /blobs/{digest}); a mismatch is a validation error.
func (ds *dataset) PutBlob(ctx context.Context, actor substrate.Actor, mimeType string, data []byte, wantDigest string) (*substrate.BlobInfo, error) {
	digest := blobDigest(data)
	if wantDigest != "" && wantDigest != digest {
		return nil, fmt.Errorf("%w: digest mismatch — the bytes hash to %s, not %s", substrate.ErrValidation, digest, wantDigest)
	}
	size := int64(len(data))
	var info *substrate.BlobInfo
	// Bytes AND manifest settle in ONE transaction under the exclusive per-digest
	// lock: a GC sweep can no longer delete the byte row between
	// its insert and the manifest settling, and no reader ever observes a stored
	// manifest whose bytes are missing.
	err := ds.inTx(ctx, actor, true, func(t *txn) error {
		if err := t.lockKey(blobLockKey(digest)); err != nil {
			return err
		}
		// The byte store is dedup-by-digest: first bytes win, a re-store is a
		// no-op. The row and the manifest carry the same digest.
		if _, err := t.exec(`
			INSERT INTO blobs (digest, mime_type, size, bytes)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (repository, digest) DO NOTHING`, digest, mimeType, size, data); err != nil {
			return fmt.Errorf("substrate/engine: store blob bytes: %w", err)
		}
		// The AUTHORITATIVE metadata is the stored row's, not this request's
		//: on a dedup PUT the first writer's mime/size win, so a
		// second PUT of the same bytes as a different mime returns — and settles
		// — the original, never a lie.
		var authMime string
		var authSize int64
		if err := t.row(`SELECT mime_type, size FROM blobs WHERE digest = $1`, digest).Scan(&authMime, &authSize); err != nil {
			return err
		}
		if err := t.settleBlobRecord(actor, digest, authSize, authMime); err != nil {
			return err
		}
		info = &substrate.BlobInfo{Digest: digest, Size: authSize, MimeType: authMime, Status: substrate.BlobStored}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return info, nil
}

// settleBlobRecord mints the blob manifest at status=stored, or transitions an
// existing pending/failed manifest to stored, INSIDE the caller's byte-store
// transaction. A manifest already stored is a no-op (no-op
// suppression writes no changelog). The bytes have already been inserted in this
// same transaction, so guardBlobWrite's "stored ⇒ bytes exist" invariant holds.
func (t *txn) settleBlobRecord(actor substrate.Actor, digest string, size int64, mimeType string) error {
	var status sql.NullString
	err := t.row(
		`SELECT states->>'`+blobStateStatus+`' FROM records WHERE id = $1 AND kind = $2 AND deleted_at IS NULL`,
		digest, kindBlob).Scan(&status)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Mint the manifest fresh, born directly in `stored` (a creating write
		// may name any declared state); a tombstoned manifest resurrects.
		_, err := t.put(substrate.PutInput{
			Kind: kindBlob, ID: digest,
			Properties: map[string]any{
				blobPropDigest: digest, blobPropSize: size,
				blobPropMimeType: mimeType, blobPropCreatedBy: string(actor),
				blobStateStatus: string(substrate.BlobStored),
			},
		})
		return err
	case err != nil:
		return err
	}
	// A pre-existing manifest (a pending ref created ahead of upload, or a
	// prior failed store): transition it to stored, filling the metadata.
	if status.Valid && status.String == string(substrate.BlobStored) {
		return nil
	}
	_, err = t.patch(eref{Kind: kindBlob, ID: digest}, substrate.PatchInput{
		Properties: map[string]any{
			blobPropSize: size, blobPropMimeType: mimeType,
			blobStateStatus: string(substrate.BlobStored),
		},
	})
	return err
}

// guardBlobWrite enforces the blob manifest invariants on every write that
// reaches a `blob` record. The generic record API cannot reach one
// at all (kindBlob is a systemType), so this guards the internal byte-store path
// and any future internal writer: the id IS the digest, and a manifest may only
// be `stored` once its bytes actually exist in the byte store — the store
// inserts bytes then settles in the SAME transaction, so a forged stored
// manifest with no bytes (or false size/mime claimed ahead of upload) is
// refused, forced to stay pending.
func (t *txn) guardBlobWrite(sp *applySpec) error {
	if sp.ty.Identity != kindBlob {
		return nil
	}
	if !validBlobDigest(sp.id) {
		return fmt.Errorf("%w: a blob id must be its content digest (%s<64 hex>)", substrate.ErrValidation, substrate.BlobDigestPrefix)
	}
	if d, ok := sp.props[blobPropDigest].(string); ok && d != sp.id {
		return fmt.Errorf("%w: a blob's digest %q must equal its id %q", substrate.ErrValidation, d, sp.id)
	}
	if sp.states[blobStateStatus] == string(substrate.BlobStored) {
		var one int
		err := t.row(`SELECT 1 FROM blobs WHERE digest = $1`, sp.id).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: a blob is stored only once its bytes exist — upload through the byte store", substrate.ErrForbidden)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// GetBlob streams a blob's bytes by digest. It is repository-scoped by
// construction: the blobs table lives in the caller's repository schema, so a
// digest another repository stored is simply absent here — a cross-repository read is
// a not-found, never a leak. Returns the manifest alongside the bytes.
func (ds *dataset) GetBlob(ctx context.Context, digest string) (*substrate.BlobInfo, []byte, error) {
	if !validBlobDigest(digest) {
		return nil, nil, fmt.Errorf("%w: %q is not a blob digest", substrate.ErrValidation, digest)
	}
	var (
		mimeType string
		size     int64
		data     []byte
	)
	err := ds.db.QueryRowContext(ctx,
		`SELECT mime_type, size, bytes FROM blobs WHERE digest = $1`, digest).Scan(&mimeType, &size, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, fmt.Errorf("%w: blob %s", substrate.ErrNotFound, digest)
	}
	if err != nil {
		return nil, nil, err
	}
	return &substrate.BlobInfo{Digest: digest, Size: size, MimeType: mimeType, Status: substrate.BlobStored}, data, nil
}

// validateBlobRefs checks that every blob-ref value in a coerced property map
// names a blob manifest that exists (in any status). Coercion checked the
// shape; this is the existence gate, taken inside the write transaction. A
// blob-ref points at bytes, so the reference must name a known — or pending —
// blob, never an arbitrary digest.
func (t *txn) validateBlobRefs(ty *vocabulary.Kind, props map[string]any) error {
	for _, name := range sortedKeys(props) {
		p, ok := ty.Prop(name)
		if !ok || p.Datatype != vocabulary.DatatypeBlobRef {
			continue
		}
		v := props[name]
		if v == nil {
			continue
		}
		var digests []string
		if p.Repeated {
			for _, item := range v.([]any) {
				digests = append(digests, fmt.Sprint(item))
			}
		} else {
			digests = append(digests, fmt.Sprint(v))
		}
		for _, d := range digests {
			// Take the digest lock SHARED before the existence check (review
			// #5): while this uncommitted reference holds it, a GC sweep cannot
			// acquire it EXCLUSIVE and delete the blob out from under us, so a
			// validated reference can never commit dangling.
			if err := t.lockKeyShared(blobLockKey(d)); err != nil {
				return err
			}
			ok, err := t.blobExists(d)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("%w: props.%s: blob %s is unknown — store it before referencing it", substrate.ErrValidation, name, d)
			}
		}
	}
	return nil
}

// blobExists reports whether a live blob manifest with this digest exists.
func (t *txn) blobExists(digest string) (bool, error) {
	var one int
	err := t.tx.QueryRowContext(t.ctx,
		`SELECT 1 FROM records WHERE id = $1 AND kind = $2 AND deleted_at IS NULL`, digest, kindBlob).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// resolveBlobRefs rewrites a projected record's blob-ref properties from the
// stored digest string into the blob's manifest ({digest, mimeType, size,
// status}) — the resolved reference reads never carry the bytes inline (ticket
// 004). A digest whose manifest has vanished renders as the bare {digest}.
func (ds *dataset) resolveBlobRefs(ctx context.Context, x dbx, ty *vocabulary.Kind, e *substrate.Record) error {
	if ty == nil {
		return nil
	}
	for _, name := range ty.PropOrder {
		p := ty.Props[name]
		if p.Datatype != vocabulary.DatatypeBlobRef {
			continue
		}
		v, ok := e.Properties[name]
		if !ok || v == nil {
			continue
		}
		if p.Repeated {
			items, ok := v.([]any)
			if !ok {
				continue
			}
			out := make([]any, 0, len(items))
			for _, item := range items {
				m, err := ds.blobManifest(ctx, x, fmt.Sprint(item))
				if err != nil {
					return err
				}
				out = append(out, m)
			}
			e.Properties[name] = out
			continue
		}
		m, err := ds.blobManifest(ctx, x, fmt.Sprint(v))
		if err != nil {
			return err
		}
		e.Properties[name] = m
	}
	return nil
}

// blobGCPass collects blobs no live record references. Victims are the UNION of
// byte rows and live blob manifests: a `stored` blob with a byte
// row AND a `pending` manifest that never received bytes are both collectable
// once their last blob-ref goes. Under the exclusive per-digest lock, and only
// after re-checking live references INSIDE the deletion transaction,
// it hard-deletes the bytes and TOMBSTONES the manifest — ordinary record GC
// hard-deletes the tombstone on a later pass, matching the tombstone contract.
// A freshly uploaded byte row within BlobUploadGrace is spared.
// Bounded per sweep (gcBatch). The manifest's own `digest` property is a plain
// string, not a blob-ref, so a blob never keeps itself alive.
func (ds *dataset) blobGCPass(ctx context.Context) (int, error) {
	referenced, err := ds.referencedDigests(ctx)
	if err != nil {
		return 0, err
	}

	// Candidates: byte rows (with their age, for the grace) UNION live blob
	// manifests (which may have no byte row at all — a pending manifest).
	type cand struct {
		hasBytes bool
		byteAt   time.Time
	}
	cands := map[string]*cand{}
	byteRows, err := ds.db.QueryContext(ctx, `SELECT digest, created_at FROM blobs`)
	if err != nil {
		return 0, err
	}
	for byteRows.Next() {
		var d string
		var at time.Time
		if err := byteRows.Scan(&d, &at); err != nil {
			_ = byteRows.Close()
			return 0, err
		}
		cands[d] = &cand{hasBytes: true, byteAt: at}
	}
	if err := byteRows.Err(); err != nil {
		_ = byteRows.Close()
		return 0, err
	}
	_ = byteRows.Close()

	manRows, err := ds.db.QueryContext(ctx,
		`SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL`, kindBlob)
	if err != nil {
		return 0, err
	}
	for manRows.Next() {
		var id string
		if err := manRows.Scan(&id); err != nil {
			_ = manRows.Close()
			return 0, err
		}
		if cands[id] == nil {
			cands[id] = &cand{}
		}
	}
	if err := manRows.Err(); err != nil {
		_ = manRows.Close()
		return 0, err
	}
	_ = manRows.Close()

	now := nowUTC()
	graceCut := now.Add(-BlobUploadGrace)
	digests := make([]string, 0, len(cands))
	for d := range cands {
		digests = append(digests, d)
	}
	sort.Strings(digests)

	var victims []string
	for _, d := range digests {
		if referenced[d] {
			continue
		}
		// A byte row still inside its unreferenced-upload grace is spared, so an
		// ordinary PUT-then-reference workflow is not raced.
		if c := cands[d]; c.hasBytes && c.byteAt.After(graceCut) {
			continue
		}
		victims = append(victims, d)
		if len(victims) >= gcBatch {
			break
		}
	}

	n := 0
	for _, digest := range victims {
		collected := false
		err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
			// Exclusive per-digest lock: an uncommitted reference holds it SHARED,
			// so this blocks until that writer commits or rolls back.
			if err := t.lockKey(blobLockKey(digest)); err != nil {
				return err
			}
			// Re-check live references INSIDE the deletion transaction: a
			// reference that committed after the snapshot above is now visible,
			// and this blob is no longer a victim.
			ref, err := t.blobReferenced(digest)
			if err != nil {
				return err
			}
			if ref {
				return nil
			}
			if _, err := t.exec(`DELETE FROM blobs WHERE digest = $1`, digest); err != nil {
				return err
			}
			row, err := t.loadRow(eref{Kind: kindBlob, ID: digest}, true)
			if err != nil {
				return err
			}
			if row != nil && row.DeletedAt == nil {
				// Tombstone the manifest (soft-delete); ordinary record GC
				// hard-deletes the tombstone on a later pass.
				if _, err := t.tombstone(eref{Kind: kindBlob, ID: digest}, ""); err != nil {
					return err
				}
				if err := t.appendChange(substrate.ActorSystem, substrate.OpGC, digest, kindBlob,
					map[string]any{"reason": "unreferenced_blob"}); err != nil {
					return err
				}
			}
			collected = true
			return nil
		})
		if err != nil {
			return n, err
		}
		if collected {
			n++
		}
	}
	return n, nil
}

// blobReferenced reports whether any live record references this digest through
// a blob-ref property (scalar or repeated), across every declared type. It is
// the in-transaction re-check GC runs under the per-digest lock.
func (t *txn) blobReferenced(digest string) (bool, error) {
	for _, ty := range t.ds.registry().Kinds() {
		for _, name := range ty.PropOrder {
			p := ty.Props[name]
			if p.Datatype != vocabulary.DatatypeBlobRef {
				continue
			}
			q := `SELECT 1 FROM records
			       WHERE kind = $2 AND deleted_at IS NULL AND props ->> $1 = $3 LIMIT 1`
			if p.Repeated {
				q = `SELECT 1 FROM records
				     WHERE kind = $2 AND deleted_at IS NULL
				       AND jsonb_typeof(props -> $1) = 'array'
				       AND EXISTS (SELECT 1 FROM jsonb_array_elements_text(props -> $1) v WHERE v = $3)
				     LIMIT 1`
			}
			var one int
			err := t.row(q, name, ty.Identity, digest).Scan(&one)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return false, err
			}
			return true, nil
		}
	}
	return false, nil
}

// referencedDigests gathers every blob digest named by a live record through a
// blob-ref property, across every declared type.
func (ds *dataset) referencedDigests(ctx context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	for _, ty := range ds.registry().Kinds() {
		for _, name := range ty.PropOrder {
			p := ty.Props[name]
			if p.Datatype != vocabulary.DatatypeBlobRef {
				continue
			}
			q := `SELECT DISTINCT props ->> $1 FROM records
			       WHERE kind = $2 AND deleted_at IS NULL AND jsonb_typeof(props -> $1) = 'string'`
			if p.Repeated {
				q = `SELECT DISTINCT jsonb_array_elements_text(props -> $1) FROM records
				     WHERE kind = $2 AND deleted_at IS NULL AND jsonb_typeof(props -> $1) = 'array'`
			}
			rows, err := ds.db.QueryContext(ctx, q, name, ty.Identity)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var d sql.NullString
				if err := rows.Scan(&d); err != nil {
					_ = rows.Close()
					return nil, err
				}
				if d.Valid && d.String != "" {
					out[d.String] = true
				}
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return nil, err
			}
			_ = rows.Close()
		}
	}
	return out, nil
}

// blobManifest reads one blob's manifest for the read-time resolution of a
// blob-ref. The digest is the manifest's id; status lives in the manifest's
// state, size and mimeType in its properties.
func (ds *dataset) blobManifest(ctx context.Context, x dbx, digest string) (map[string]any, error) {
	m := map[string]any{blobPropDigest: digest}
	var (
		mimeType sql.NullString
		size     sql.NullInt64
		status   sql.NullString
	)
	err := x.QueryRowContext(ctx, `
		SELECT props->>'`+blobPropMimeType+`', (props->>'`+blobPropSize+`')::bigint, states->>'`+blobStateStatus+`'
		FROM records WHERE id = $1 AND kind = $2 AND deleted_at IS NULL`, digest, kindBlob).
		Scan(&mimeType, &size, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if mimeType.Valid && mimeType.String != "" {
		m[blobPropMimeType] = mimeType.String
	}
	if size.Valid {
		m[blobPropSize] = size.Int64
	}
	if status.Valid {
		m[blobStateStatus] = status.String
	}
	return m, nil
}
