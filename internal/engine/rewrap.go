package engine

// The offline restore through the recovery key. A repository's archive copied
// to a host without the SUBSTRATE_CREDENTIAL_KEY it was written under does not
// restore: the manifest's DEK wrap does not open (importHistory). The same
// DEK sits in the changelog, wrapped to the user's age recipient in the
// recoverykey record's sealedKey, so RewrapRepositoryDir unwraps it with the
// identity, proves it opens every sealed file, wraps it under the new host key
// and rewrites the manifest. The boot's restore then does the rest. Nothing here
// touches Postgres, and neither the identity nor the DEK is written anywhere.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"filippo.io/age"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/changelogfile"
)

// RewrapReport is what one rewrap did.
type RewrapReport struct {
	// Repository is the repository's name, its authority, as the manifest
	// names it.
	Repository string `json:"repository"`
	// RecoveryKeySeq is the changelog seq of the recoverykey write whose
	// sealedKey the identity opened.
	RecoveryKeySeq int64 `json:"recoveryKeySeq"`
	// SealedFiles is how many files under sealed/ the recovered DEK opened,
	// every one of them.
	SealedFiles int `json:"sealedFiles"`
}

// ErrNoRecoveryKey is returned for a repository whose changelog holds no
// recoverykey record: it never enrolled one, so no identity can open it.
var ErrNoRecoveryKey = errors.New("substrate/engine: the repository never enrolled a recovery key")

// RewrapRepositoryDir rewrites the manifest of the stopped repository
// directory repoDir so that newKey (a SUBSTRATE_CREDENTIAL_KEY value) opens
// its DEK. The DEK comes from the changelog: the last recoverykey write's
// sealedKey, opened with the user's age identity. Before anything is written
// every file under sealed/ must open under that DEK, so a wrong identity, a
// directory whose sealed store was not re-keyed, or a damaged file refuses the
// whole rewrap. It refuses a directory with no manifest or no recoverykey
// record rather than guess at either. It reads an archive's layout (an
// export extracted) and a 0.x directory's alike.
func RewrapRepositoryDir(repoDir, identity, newKey string) (RewrapReport, error) {
	var report RewrapReport
	hostKey, err := deriveCredentialKey(newKey)
	if err != nil {
		return report, err
	}
	if len(hostKey) == 0 {
		return report, errors.New("substrate/engine: SUBSTRATE_CREDENTIAL_KEY is unset: the rewrap needs the key the repository will boot under (generate one with: openssl rand -base64 32)")
	}
	// age's parse error can echo a prefix of the string it was handed, and a
	// mis-pasted line is still a secret, so the refusal carries no detail.
	id, err := age.ParseX25519Identity(identity)
	if err != nil {
		return report, errors.New("substrate/engine: the recovery key is not an age identity (AGE-SECRET-KEY-1...)")
	}
	// An archive's directory carries its manifests under manifests/; a 0.x
	// directory, its repository.json.
	archived, name := false, ""
	m, err := changelogfile.ReadManifest(repoDir)
	if errors.Is(err, os.ErrNotExist) {
		if m, name, err = changelogfile.ReadArchiveManifestDir(repoDir); err == nil {
			archived = true
		}
	}
	if errors.Is(err, os.ErrNotExist) {
		return report, fmt.Errorf("substrate/engine: %s has no manifest; the rewrap does not synthesize one", repoDir)
	}
	if err != nil {
		return report, err
	}
	report.Repository = m.Authority

	src, err := localHistory(repoDir, m, name)
	if err != nil {
		return report, err
	}
	seq, sealedKey, err := lastRecoveryKey(src)
	if err != nil {
		return report, err
	}
	report.RecoveryKeySeq = seq
	dek, err := unwrapDEKWithIdentity(id, sealedKey)
	if err != nil {
		return report, fmt.Errorf("substrate/engine: the recovery key does not open the recoverykey record of repository %s (seq %d): %w", m.Authority, seq, err)
	}
	n, err := sealedStoreOpens(repoDir, src, dek)
	if err != nil {
		return report, err
	}
	report.SealedFiles = n

	aead, err := newAEAD(hostKey)
	if err != nil {
		return report, err
	}
	if m.DEK, err = sealWith(aead, dek, dekAAD(m.Key())); err != nil {
		return report, err
	}
	// The wrap names the key it is now under; the sealed-store marker is the
	// copy's own and stays as the manifest carried it.
	m.DEKKeyID = hostKeyID(hostKey)
	// Written only now, once every refusal is behind: a refused rewrap leaves
	// the directory exactly as it found it. An archive's manifest is never
	// written over: the rewrapped one is the next epoch's, selecting what the
	// one it read does, and the newest of the newest archive.
	if archived {
		m.Epoch++
		if m.Pass, err = changelogfile.NewPassID(); err != nil {
			return report, err
		}
		if _, err := changelogfile.WriteArchiveManifestDir(repoDir, m); err != nil {
			return report, err
		}
	} else if err := changelogfile.WriteManifest(repoDir, m); err != nil {
		return report, err
	}
	// A pending file is a write the copy's server had staged and not
	// committed: it is nobody's record, and it is sealed under the DEK this
	// copy is leaving behind.
	if _, err := changelogfile.DiscardPendingSealed(repoDir); err != nil {
		return report, err
	}
	return report, nil
}

// lastRecoveryKey walks the history for the last write of the recoverykey
// record and returns its seq and sealedKey. The fold is not available
// offline, so this reads the record delta the way the fold would.
func lastRecoveryKey(src historySource) (int64, string, error) {
	var seq int64
	var sealedKey string
	err := walkHistory(src, func(e changelogfile.Entry) error {
		if e.Kind != kindRecoveryKey || e.RecordID != recoveryKeyID {
			return nil
		}
		var payload map[string]any
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return fmt.Errorf("substrate/engine: seq %d carries an unreadable payload: %w", e.Seq, err)
		}
		forEachRecordDeltaSet(payload, func(kindRef, recordID string, set map[string]any) {
			if kindRef != kindRecoveryKey || recordID != recoveryKeyID {
				return
			}
			if v, ok := set["sealedKey"].(string); ok && v != "" {
				seq, sealedKey = e.Seq, v
			}
		})
		return nil
	})
	if err != nil {
		return 0, "", err
	}
	if sealedKey == "" {
		return 0, "", ErrNoRecoveryKey
	}
	return seq, sealedKey, nil
}

// localHistory is a directory's history whichever layout it is in: an
// archive's, the directory being `repositories/<id>` of an archive (an export
// extracted, decision 0278), whose sealed values the manifest selects, or a
// 0.x directory's segments and sealed files.
func localHistory(repoDir string, m changelogfile.Manifest, name string) (historySource, error) {
	if m.Format != changelogfile.ManifestFormatArchive {
		log, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(repoDir))
		if err != nil {
			return nil, err
		}
		return dirSource{dir: repoDir, log: log, dialect: m.ChangelogDialect}, nil
	}
	store, err := archive.NewDir(filepath.Dir(filepath.Dir(filepath.Clean(repoDir))))
	if err != nil {
		return nil, err
	}
	return openArchiveSource(context.Background(), store, m.ID, m.Archive, name, nil)
}

// walkHistory hands every entry of src to fn, in seq order.
func walkHistory(src historySource, fn func(changelogfile.Entry) error) error {
	cur, err := src.Cursor(context.Background(), 0)
	if err != nil {
		return err
	}
	defer func() { _ = cur.Close() }()
	for {
		entries, err := cur.Next(rebuildBatch)
		if err != nil || len(entries) == 0 {
			return err
		}
		for _, e := range entries {
			if err := fn(e); err != nil {
				return err
			}
		}
	}
}

// unwrapDEKWithIdentity opens a recoverykey record's sealedKey (base64 of an
// age ciphertext over the raw DEK) with the user's identity.
func unwrapDEKWithIdentity(id *age.X25519Identity, sealedKey string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(sealedKey)
	if err != nil {
		return nil, fmt.Errorf("sealedKey is not base64: %w", err)
	}
	r, err := age.Decrypt(bytes.NewReader(raw), id)
	if err != nil {
		return nil, err
	}
	dek, err := io.ReadAll(io.LimitReader(r, 64))
	if err != nil {
		return nil, err
	}
	if len(dek) != 32 {
		return nil, fmt.Errorf("the wrap holds %d bytes, not a 32-byte DEK", len(dek))
	}
	return dek, nil
}

// sealedStoreOpens opens every file under sealed/ with dek, each under its
// own row binding, and returns how many there were. The first file that does
// not open refuses the rewrap by ref: a manifest written over a DEK that does
// not open the store would import a repository no login can open. No file at
// all is refused too: every registered repository seals at least its password
// hash and TOTP seed, so an empty or missing sealed/ is a copy that lost them,
// and a DEK nothing has tested is not one to write a manifest over.
func sealedStoreOpens(repoDir string, src historySource, dek []byte) (int, error) {
	files, err := src.Sealed(context.Background())
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, fmt.Errorf("substrate/engine: %s has no files under sealed/; a registered repository seals at least its login credential, so this copy is incomplete", repoDir)
	}
	if err := sealedFilesOpenUnder(files, dek); err != nil {
		return 0, fmt.Errorf("substrate/engine: the recovered DEK does not open every sealed file: %w", err)
	}
	return len(files), nil
}

// sealedFilesOpenUnder opens every sealed file under dek alone, each under its
// own row binding, and names the first that does not: the check behind the
// offline rewrap and behind the import of a directory marked DEK-only (0059).
func sealedFilesOpenUnder(files []changelogfile.SealedRecord, dek []byte) error {
	for _, f := range files {
		if _, err := OpenPayloadWithKey(dek, f.Payload, sealedAAD(f.Ref, f.RecordKind, f.RecordID)); err != nil {
			return fmt.Errorf("the sealed value %s (%s %s) does not open under the DEK: %w",
				f.Ref, f.RecordKind, f.RecordID, err)
		}
	}
	return nil
}
