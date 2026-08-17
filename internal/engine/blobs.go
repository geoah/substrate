package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/geoah/substrate/internal/blobbytes"
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
	blobPropName      = "name"
	blobPropMimeType  = "mimeType"
	blobPropCreatedBy = "createdBy"
	blobStateStatus   = "status"
)

// maxBlobNameLen bounds the display name. A name is metadata on a row that
// also holds the bytes; it is a filename, not a document.
const maxBlobNameLen = 255

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
// addressed PUT /blobs/{digest}); a mismatch is a validation error. What the
// caller SAYS about the bytes — up.Name, up.MimeType — is optional and
// descriptive; the digest is the identity, so neither field takes part in
// dedup and neither displaces what a first upload already said.
func (ds *dataset) PutBlob(ctx context.Context, actor substrate.Actor, up substrate.BlobUpload, data []byte, wantDigest string) (*substrate.BlobInfo, error) {
	digest := blobDigest(data)
	if wantDigest != "" && wantDigest != digest {
		return nil, fmt.Errorf("%w: digest mismatch — the bytes hash to %s, not %s", substrate.ErrValidation, digest, wantDigest)
	}
	name, err := checkBlobName(up.Name)
	if err != nil {
		return nil, err
	}
	store, err := ds.blobBytes()
	if err != nil {
		return nil, err
	}
	if txStore, ok := store.(blobbytes.InTransaction); ok {
		return ds.putBlobOneTx(ctx, actor, txStore, digest, name, up.MimeType, data)
	}
	return ds.putBlobExternal(ctx, actor, store, digest, name, up.MimeType, data)
}

// putBlobOneTx is the postgres path: bytes AND manifest settle in ONE
// transaction under the exclusive per-digest lock, so a GC sweep can never
// delete the byte row between its insert and the manifest settling, and no
// crash can leave either half without the other.
func (ds *dataset) putBlobOneTx(ctx context.Context, actor substrate.Actor, store blobbytes.InTransaction, digest, name, mimeType string, data []byte) (*substrate.BlobInfo, error) {
	size := int64(len(data))
	var info *substrate.BlobInfo
	err := ds.inTx(ctx, actor, true, func(t *txn) error {
		if err := t.lockKey(blobLockKey(digest)); err != nil {
			return err
		}
		// The byte store is dedup-by-digest: first bytes win, a re-store is a
		// no-op. The row and the manifest carry the same digest.
		if err := store.PutTx(t.ctx, t.tx, blobbytes.Blob{
			Digest: digest, Name: name, MimeType: mimeType, Size: size, Bytes: data,
		}); err != nil {
			return err
		}
		// The AUTHORITATIVE metadata is the stored row's, not this request's
		//: on a dedup PUT the first writer's name/mime/size win, so a
		// second PUT of the same bytes as a different name returns — and settles
		// — the original, never a lie.
		var authName, authMime string
		var authSize int64
		if err := t.row(`SELECT name, mime_type, size FROM blobs WHERE digest = $1`, digest).
			Scan(&authName, &authMime, &authSize); err != nil {
			return err
		}
		if err := t.settleBlobRecord(actor, digest, authSize, authName, authMime); err != nil {
			return err
		}
		info = &substrate.BlobInfo{
			Digest: digest, Size: authSize, Name: authName,
			MimeType: authMime, Status: substrate.BlobStored,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return info, nil
}

// putBlobExternal is the fs and s3 path, where the bytes and the manifest
// cannot commit together. It runs in three steps, and the order is the whole
// design:
//
//  1. Under the digest lock, a manifest at `pending` — the intent, recorded in
//     Postgres before a byte is written. It is what makes a crash cheap: the
//     sweep collects an unreferenced pending manifest already, so nothing has
//     to enumerate the store to discover an orphan.
//  2. The bytes, outside any transaction. Idempotent by digest.
//  3. Under the digest lock again, the manifest to `stored`, whose guard
//     probes the store for the bytes.
//
// A crash after 1 leaves a pending manifest and no bytes; a crash after 2
// leaves a pending manifest and an object nothing points at. Both collapse to
// the same collectable state, and in neither does a reader see a `stored`
// manifest whose bytes are missing, because only step 3 writes that word and
// only with the bytes already durable.
func (ds *dataset) putBlobExternal(ctx context.Context, actor substrate.Actor, store blobbytes.Store, digest, name, mimeType string, data []byte) (*substrate.BlobInfo, error) {
	size := int64(len(data))
	// The authoritative metadata is the first writer's, exactly as on the
	// postgres path: a stored manifest is what a second PUT of the same bytes
	// gets back, whatever it called them.
	authName, authMime, authSize := name, mimeType, size
	err := ds.inTx(ctx, actor, true, func(t *txn) error {
		if err := t.lockKey(blobLockKey(digest)); err != nil {
			return err
		}
		m, ok, err := t.blobRecord(digest)
		if err != nil {
			return err
		}
		if ok && m.status == string(substrate.BlobStored) {
			authName, authMime, authSize = m.name, m.mimeType, m.size
			return nil
		}
		if ok {
			// A manifest created ahead of the upload (a reference minted
			// pending): it stays, and step 3 fills it in.
			return nil
		}
		return t.mintPendingBlobRecord(actor, digest, name, mimeType)
	})
	if err != nil {
		return nil, err
	}

	// Re-store nothing: the digest is the content, so bytes already in the
	// store are the bytes this request holds. The probe also heals a manifest
	// whose object went missing, by putting them back.
	held, err := store.Exists(ctx, digest)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: probe blob bytes: %w", err)
	}
	if !held {
		if err := store.Put(ctx, digest, size, bytes.NewReader(data)); err != nil {
			return nil, fmt.Errorf("substrate/engine: store blob bytes: %w", err)
		}
	}

	err = ds.inTx(ctx, actor, true, func(t *txn) error {
		if err := t.lockKey(blobLockKey(digest)); err != nil {
			return err
		}
		return t.settleBlobRecord(actor, digest, authSize, authName, authMime)
	})
	if err != nil {
		return nil, err
	}
	return &substrate.BlobInfo{
		Digest: digest, Size: authSize, Name: authName,
		MimeType: authMime, Status: substrate.BlobStored,
	}, nil
}

// blobBytes binds the configured backend to this repository. The repository is
// half of every key the fs and s3 backends build and the scoped pool is what
// row level security binds for the postgres one, so a store handed to a
// request can only ever reach that request's repository.
func (ds *dataset) blobBytes() (blobbytes.Store, error) {
	return ds.svc.blobs.Repository(ds.scope.Repository, ds.db)
}

// blobRecordMeta is a blob manifest, read as fields rather than as a map.
type blobRecordMeta struct {
	name     string
	mimeType string
	size     int64
	status   string
}

// blobRecord reads one live blob manifest inside the caller's transaction.
func (t *txn) blobRecord(digest string) (blobRecordMeta, bool, error) {
	return scanBlobRecord(t.row(blobRecordQuery, digest, kindBlob))
}

// blobRecordQuery reads the manifest halves a blob read reports: the metadata
// from its properties, the status from its state.
const blobRecordQuery = `
	SELECT props->>'` + blobPropName + `', props->>'` + blobPropMimeType + `',
	       (props->>'` + blobPropSize + `')::bigint, states->>'` + blobStateStatus + `'
	FROM records WHERE id = $1 AND kind = $2 AND deleted_at IS NULL`

func scanBlobRecord(row *sql.Row) (blobRecordMeta, bool, error) {
	var (
		m      blobRecordMeta
		name   sql.NullString
		mime   sql.NullString
		size   sql.NullInt64
		status sql.NullString
	)
	err := row.Scan(&name, &mime, &size, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return m, false, nil
	}
	if err != nil {
		return m, false, err
	}
	m.name, m.mimeType, m.size, m.status = name.String, mime.String, size.Int64, status.String
	return m, true, nil
}

// mintPendingBlobRecord records the INTENT to store bytes: a manifest born
// `pending`, before the bytes are written to a store that cannot join this
// transaction. Only settleBlobRecord may write `stored`, and only with the
// bytes already durable.
func (t *txn) mintPendingBlobRecord(actor substrate.Actor, digest, name, mimeType string) error {
	props := map[string]any{
		blobPropDigest:    digest,
		blobPropCreatedBy: string(actor),
		blobStateStatus:   string(substrate.BlobPending),
	}
	if name != "" {
		props[blobPropName] = name
	}
	if mimeType != "" {
		props[blobPropMimeType] = mimeType
	}
	_, err := t.put(substrate.PutInput{Kind: kindBlob, ID: digest, Properties: props})
	return err
}

// checkBlobName validates and normalizes an uploaded blob's display name. A
// name is metadata, so an absent one is fine — but a name that carries a path
// separator or a control character would read as an address somewhere it is
// rendered, and it is refused rather than quietly rewritten.
func checkBlobName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	if len(name) > maxBlobNameLen {
		return "", fmt.Errorf("%w: a blob name is at most %d bytes", substrate.ErrValidation, maxBlobNameLen)
	}
	if strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("%w: a blob name must not contain a path separator — it names the blob, it does not address one", substrate.ErrValidation)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: a blob name must not contain control characters", substrate.ErrValidation)
		}
	}
	return name, nil
}

// settleBlobRecord mints the blob manifest at status=stored, or transitions an
// existing pending/failed manifest to stored, INSIDE the caller's byte-store
// transaction. A manifest already stored is a no-op (no-op
// suppression writes no changelog). The bytes are already durable when this
// runs — inserted in this same transaction on the postgres backend, written to
// the store before it on fs and s3 — so guardBlobWrite's "stored ⇒ bytes
// exist" invariant holds, and the guard proves it either way.
func (t *txn) settleBlobRecord(actor substrate.Actor, digest string, size int64, name, mimeType string) error {
	var status sql.NullString
	err := t.row(
		`SELECT states->>'`+blobStateStatus+`' FROM records WHERE id = $1 AND kind = $2 AND deleted_at IS NULL`,
		digest, kindBlob).Scan(&status)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Mint the manifest fresh, born directly in `stored` (a creating write
		// may name any declared state); a tombstoned manifest resurrects.
		props := map[string]any{
			blobPropDigest: digest, blobPropSize: size,
			blobPropCreatedBy: string(actor),
			blobStateStatus:   string(substrate.BlobStored),
		}
		// Name and mime type are optional: an empty one is written as nothing
		// at all, so a manifest never claims the uploader said "".
		if name != "" {
			props[blobPropName] = name
		}
		if mimeType != "" {
			props[blobPropMimeType] = mimeType
		}
		_, err := t.put(substrate.PutInput{Kind: kindBlob, ID: digest, Properties: props})
		return err
	case err != nil:
		return err
	}
	// A pre-existing manifest (a pending ref created ahead of upload, or a
	// prior failed store): transition it to stored, filling the metadata.
	if status.Valid && status.String == string(substrate.BlobStored) {
		return nil
	}
	props := map[string]any{
		blobPropSize:    size,
		blobStateStatus: string(substrate.BlobStored),
	}
	if name != "" {
		props[blobPropName] = name
	}
	if mimeType != "" {
		props[blobPropMimeType] = mimeType
	}
	_, err = t.patch(eref{Kind: kindBlob, ID: digest}, substrate.PatchInput{Properties: props})
	return err
}

// guardBlobWrite enforces the blob manifest invariants on every write that
// reaches a `blob` record. The generic record API cannot reach one
// at all (kindBlob is a systemType), so this guards the internal byte-store path
// and any future internal writer: the id IS the digest, and a manifest may only
// be `stored` once its bytes actually exist in the byte store. The probe is a
// query on the postgres backend, taken inside this transaction so it sees the
// insert beside it, and an existence request against fs or s3 otherwise. A
// forged stored manifest with no bytes (or false size/mime claimed ahead of
// upload) is refused, forced to stay pending.
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
		held, err := t.blobBytesExist(sp.id)
		if err != nil {
			return err
		}
		if !held {
			return fmt.Errorf("%w: a blob is stored only once its bytes exist — upload through the byte store", substrate.ErrForbidden)
		}
	}
	return nil
}

// blobBytesExist is the guard's probe. The postgres backend runs it on this
// transaction, so bytes inserted a statement earlier count; an external
// backend answers over its own connection, and only bytes it has already
// acknowledged count.
func (t *txn) blobBytesExist(digest string) (bool, error) {
	store, err := t.ds.blobBytes()
	if err != nil {
		return false, err
	}
	if txStore, ok := store.(blobbytes.InTransaction); ok {
		return txStore.ExistsTx(t.ctx, t.tx, digest)
	}
	return store.Exists(t.ctx, digest)
}

// GetBlob reads a blob's bytes by digest. It resolves through the
// repository-scoped MANIFEST first and reaches the store only for the bytes:
// the manifest read is bound by row level security and the store is bound to
// this repository's key prefix, so a digest another repository stored is
// absent here whichever backend holds the bytes — a cross-repository read is a
// not-found, never a leak. Returns the manifest alongside the bytes.
func (ds *dataset) GetBlob(ctx context.Context, digest string) (*substrate.BlobInfo, []byte, error) {
	if !validBlobDigest(digest) {
		return nil, nil, fmt.Errorf("%w: %q is not a blob digest", substrate.ErrValidation, digest)
	}
	notFound := fmt.Errorf("%w: blob %s", substrate.ErrNotFound, digest)
	m, ok, err := scanBlobRecord(ds.db.QueryRowContext(ctx, blobRecordQuery, digest, kindBlob))
	if err != nil {
		return nil, nil, err
	}
	// A manifest that is not `stored` names bytes nobody has uploaded yet, so
	// the read is a not-found rather than an empty body.
	if !ok || m.status != string(substrate.BlobStored) {
		return nil, nil, notFound
	}
	store, err := ds.blobBytes()
	if err != nil {
		return nil, nil, err
	}
	data, err := blobbytes.ReadAll(ctx, store, digest)
	if errors.Is(err, blobbytes.ErrNotStored) {
		return nil, nil, notFound
	}
	if err != nil {
		return nil, nil, err
	}
	return &substrate.BlobInfo{
		Digest: digest, Size: m.size, Name: m.name,
		MimeType: m.mimeType, Status: substrate.BlobStored,
	}, data, nil
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
// stored digest string into the blob's manifest ({digest, name, mimeType,
// size, status}) — the resolved reference reads never carry the bytes inline (ticket
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

// blobSweepBatch bounds how many objects the orphan sweep enumerates per pass.
// The cursor (ds.blobSweepAfter) carries on where the last pass stopped, so a
// store with more objects than this is walked over several passes rather than
// having its tail never looked at.
const blobSweepBatch = 1000

// blobGCPass collects blobs no live record references. Candidates are the LIVE
// MANIFESTS: a `stored` blob and a `pending` manifest that never received
// bytes are both collectable once their last blob-ref goes, and every blob has
// a manifest whichever store its bytes sit in. Under the exclusive per-digest
// lock, and only after re-checking live references INSIDE the deletion
// transaction, it deletes the bytes and TOMBSTONES the manifest — ordinary
// record GC hard-deletes the tombstone on a later pass, matching the tombstone
// contract. A manifest younger than BlobUploadGrace is spared, so an ordinary
// PUT-then-reference workflow is not raced. Bounded per sweep (gcBatch). The
// manifest's own `digest` property is a plain string, not a blob-ref, so a
// blob never keeps itself alive. The orphan sweep afterwards is the other
// half: bytes no manifest names at all.
func (ds *dataset) blobGCPass(ctx context.Context) (int, error) {
	store, err := ds.blobBytes()
	if err != nil {
		return 0, err
	}
	referenced, err := ds.referencedDigests(ctx)
	if err != nil {
		return 0, err
	}

	live := map[string]time.Time{}
	manRows, err := ds.db.QueryContext(ctx,
		`SELECT id, updated_at FROM records WHERE kind = $1 AND deleted_at IS NULL`, kindBlob)
	if err != nil {
		return 0, err
	}
	for manRows.Next() {
		var id string
		var at time.Time
		if err := manRows.Scan(&id, &at); err != nil {
			_ = manRows.Close()
			return 0, err
		}
		live[id] = at.UTC()
	}
	if err := manRows.Err(); err != nil {
		_ = manRows.Close()
		return 0, err
	}
	_ = manRows.Close()

	graceCut := nowUTC().Add(-BlobUploadGrace)
	digests := make([]string, 0, len(live))
	for d := range live {
		digests = append(digests, d)
	}
	sort.Strings(digests)

	var victims []string
	for _, d := range digests {
		if referenced[d] || live[d].After(graceCut) {
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
			if err := t.deleteBlobBytes(store, digest); err != nil {
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
	if err := ds.blobOrphanSweep(ctx, store); err != nil {
		return n, err
	}
	return n, nil
}

// deleteBlobBytes removes a victim's bytes as part of its collection. Where the
// backend can join this transaction the bytes and the tombstone commit
// together, exactly as they always have. Where it cannot, the tombstone
// commits FIRST and the delete follows it: a failure there leaves an object no
// live manifest names, which the orphan sweep reaps on a later pass. The other
// order would leave a live manifest whose bytes are gone, which is the one
// state a reader must never see.
func (t *txn) deleteBlobBytes(store blobbytes.Store, digest string) error {
	if txStore, ok := store.(blobbytes.InTransaction); ok {
		return txStore.DeleteTx(t.ctx, t.tx, digest)
	}
	ctx, ds := t.ctx, t.ds
	t.afterCommit = append(t.afterCommit, func() {
		if err := store.Delete(ctx, digest); err != nil {
			ds.svc.log.Warn("substrate: a collected blob's bytes could not be deleted; the orphan sweep will retry",
				"digest", digest, "backend", store.Backend(), "error", err)
		}
	})
	return nil
}

// blobOrphanSweep deletes stored bytes no live manifest names. Two things put
// an object in that state: a crash between writing the bytes and settling the
// manifest, and a collection whose post-commit delete did not land. Both are
// re-runnable, because deleting bytes that are already gone succeeds.
//
// The re-check happens under the same exclusive per-digest lock an upload
// takes, so an upload in flight — which has already committed its `pending`
// manifest before writing a byte — is never swept out from under. An object
// younger than BlobUploadGrace is spared as well, which keeps the sweep off
// the write path of the ordinary case entirely.
func (ds *dataset) blobOrphanSweep(ctx context.Context, store blobbytes.Store) error {
	ds.mu.Lock()
	after := ds.blobSweepAfter
	ds.mu.Unlock()

	objects, err := store.List(ctx, after, blobSweepBatch)
	if err != nil {
		return err
	}
	// A short page is the end of the store: the next pass starts over.
	next := ""
	if len(objects) == blobSweepBatch {
		next = objects[len(objects)-1].Digest
	}
	ds.mu.Lock()
	ds.blobSweepAfter = next
	ds.mu.Unlock()

	graceCut := nowUTC().Add(-BlobUploadGrace)
	for _, o := range objects {
		if o.At.After(graceCut) {
			continue
		}
		err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
			if err := t.lockKey(blobLockKey(o.Digest)); err != nil {
				return err
			}
			_, ok, err := t.blobRecord(o.Digest)
			if err != nil || ok {
				return err
			}
			// Nothing to commit but the lock, so the delete runs INSIDE the
			// transaction: an upload that takes the lock next finds the object
			// gone rather than half-deleted.
			if txStore, ok := store.(blobbytes.InTransaction); ok {
				return txStore.DeleteTx(t.ctx, t.tx, o.Digest)
			}
			return store.Delete(t.ctx, o.Digest)
		})
		if err != nil {
			return err
		}
	}
	return nil
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
		name     sql.NullString
		mimeType sql.NullString
		size     sql.NullInt64
		status   sql.NullString
	)
	err := x.QueryRowContext(ctx, `
		SELECT props->>'`+blobPropName+`', props->>'`+blobPropMimeType+`', (props->>'`+blobPropSize+`')::bigint, states->>'`+blobStateStatus+`'
		FROM records WHERE id = $1 AND kind = $2 AND deleted_at IS NULL`, digest, kindBlob).
		Scan(&name, &mimeType, &size, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if name.Valid && name.String != "" {
		m[blobPropName] = name.String
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
