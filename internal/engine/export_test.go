package engine

import (
	"context"
	"crypto/rand"
	"encoding/base64"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// TestCredentialKey is a conforming credential key: standard-base64 of 32
// random bytes, the shape Open now demands (ADR 0024). It is minted once per
// test binary, so every Open in the suite shares one key and a reopen of the
// same database matches. Generated at run time and never committed, because a
// key checked into the tree is a key everyone has.
var TestCredentialKey = mintTestCredentialKey()

// TestCredentialKeyBytes is the 32-byte AES-256 key TestCredentialKey decodes
// to, for tests that unwrap a DEK the way the engine does.
var TestCredentialKeyBytes = mustDecodeTestCredentialKey(TestCredentialKey)

func mintTestCredentialKey() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func mustDecodeTestCredentialKey(key string) []byte {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		panic(err)
	}
	return raw
}

// DataRootOf is the data root a service was opened with, so a test can find
// a repository's directory (changelogfile.RepoDir) and damage or copy it.
func DataRootOf(svc substrate.Service) string { return svc.(*service).dataRoot }

// WithTestImportFault runs fn at each durable step of a boot import
// (repodir.go importEntries): after every batch of changelog rows commits,
// with ImportAfterBatch, and after the first fold pass commits, with
// ImportAfterFirstFold. An error from fn ends the boot there, which is the
// shape of a process dying at that step. A batch above zero replaces
// rebuildBatch for the import's row batches, so a short history spans
// several.
func WithTestImportFault(batch int, fn func(stage string) error) Option {
	return func(o *options) { o.importFault, o.importBatch = fn, batch }
}

// The import stages WithTestImportFault reports.
const (
	ImportAfterBatch     = importAfterBatch
	ImportAfterFirstFold = importAfterFirstFold
)

// ImportIncomplete reports whether the repository's import-progress marker
// is set, read through the tamperer's seat.
func ImportIncomplete(ctx context.Context, db dbx) (bool, error) {
	_, incomplete, err := importIncomplete(ctx, db)
	return incomplete, err
}

// AdvisoryKeySQL is the engine's advisory-lock key expression (identity.go),
// for a test that takes one of the engine's locks by hand: a barrier test that
// composed the key itself would park on a lock nothing else takes.
const AdvisoryKeySQL = advisoryKeySQL

// BreakChangelogWriter closes a dataset's changelog writer under its mutex, so
// the next commit's append fails the way a full disk would: the tables take
// the write, the directory does not, and the dataset latches
// ErrChangelogFileBehind. The closed writer also releases the directory's
// lock, so a reopened service can take it.
func BreakChangelogWriter(ds substrate.Dataset) {
	d := ds.(*dataset)
	d.writerMu.Lock()
	defer d.writerMu.Unlock()
	_ = d.writer.Close()
}

// SealedAAD builds the additional data a sealed-store row binds to, so a test
// opens a payload the way the engine does (ADR 0023).
func SealedAAD(ref, recordKind, recordID string) []byte { return sealedAAD(ref, recordKind, recordID) }

// DEKAAD builds the additional data the control-plane DEK wrap binds to.
func DEKAAD(repoID string) []byte { return dekAAD(repoID) }

// SealWithKey seals raw under key bound to aad, the way the host credential
// key wraps a DEK, so a test can build a directory another binary wrote.
func SealWithKey(key, raw, aad []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	return sealWith(aead, raw, aad)
}

// refPaths reads a record's reference property as the record paths it names, in
// order. It is the tests' one reader of a stored reference, so a test asserting
// on a pointer does not have to know whether the declaration carries link data:
// both shapes answer here.
func refPaths(e *substrate.Record, name string) []string {
	v := e.Properties[name]
	list, repeated := v.([]any)
	if !repeated {
		list = []any{v}
	}
	var out []string
	for _, item := range list {
		if p := referencePathOf(item); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// refPath is refPaths for a single-valued reference, "" when it names nothing.
func refPath(e *substrate.Record, name string) string {
	if paths := refPaths(e, name); len(paths) > 0 {
		return paths[0]
	}
	return ""
}

// refIDs is refPaths with the kind stripped off each path.
func refIDs(e *substrate.Record, name string) []string {
	paths := refPaths(e, name)
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		_, id, _ := vocabulary.SplitRecordPath(p)
		out = append(out, id)
	}
	return out
}

// refID is refIDs for a single-valued reference, "" when it names nothing.
func refID(e *substrate.Record, name string) string {
	if ids := refIDs(e, name); len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// ReceiveWebhookSync is the public webhook door with the fire run inline
// rather than handed to the background supervisor, so a test asserts on what
// the delivery wrote the moment the call returns.
func ReceiveWebhookSync(ctx context.Context, svc substrate.Service, authority, trigger, key string, req substrate.WebhookRequest) (string, error) {
	return svc.(*service).receiveWebhook(ctx, authority, trigger, key, req, true)
}
