package engine

// The offline restore through the recovery key. A repository directory copied
// to a host without the SUBSTRATE_CREDENTIAL_KEY it was written under does not
// import: the manifest's DEK wrap does not open (importRepositoryDir). The same
// DEK sits in the changelog, wrapped to the user's age recipient in the
// recoverykey record's sealedKey, so RewrapRepositoryDir unwraps it with the
// identity, proves it opens every sealed file, wraps it under the new host key
// and rewrites the manifest. The boot import then does the rest. Nothing here
// touches Postgres, and neither the identity nor the DEK is written anywhere.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"filippo.io/age"

	"github.com/geoah/substrate/internal/changelogfile"
)

// RewrapReport is what one rewrap did.
type RewrapReport struct {
	// Repository is the authority, the repository's id; Username is the login
	// label the manifest carries.
	Repository string `json:"repository"`
	Username   string `json:"username"`
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
// record rather than guess at either, and it takes the directory's writer
// lock so a running server is refused (changelogfile.ErrLocked).
func RewrapRepositoryDir(repoDir, identity, newKey string) (RewrapReport, error) {
	var report RewrapReport
	hostKey, err := deriveCredentialKey(newKey)
	if err != nil {
		return report, err
	}
	if len(hostKey) == 0 {
		return report, errors.New("substrate/engine: SUBSTRATE_CREDENTIAL_KEY is unset: the rewrap needs the key the repository will boot under (generate one with: openssl rand -base64 32)")
	}
	id, err := age.ParseX25519Identity(identity)
	if err != nil {
		return report, fmt.Errorf("substrate/engine: the recovery key is not an age identity (AGE-SECRET-KEY-1...): %w", err)
	}
	m, err := changelogfile.ReadManifest(repoDir)
	if errors.Is(err, os.ErrNotExist) {
		return report, fmt.Errorf("substrate/engine: %s has no %s; the rewrap does not synthesize one", repoDir, changelogfile.ManifestName)
	}
	if err != nil {
		return report, err
	}
	report.Repository, report.Username = m.Authority, m.Username

	release, err := changelogfile.LockWriter(changelogfile.ChangelogDir(repoDir))
	if err != nil {
		return report, err
	}
	defer func() { _ = release() }()

	seq, sealedKey, err := lastRecoveryKey(repoDir)
	if err != nil {
		return report, err
	}
	report.RecoveryKeySeq = seq
	dek, err := unwrapDEKWithIdentity(id, sealedKey)
	if err != nil {
		return report, fmt.Errorf("substrate/engine: the recovery key does not open the recoverykey record of repository %s (seq %d): %w", m.Authority, seq, err)
	}
	n, err := sealedStoreOpens(repoDir, dek)
	if err != nil {
		return report, err
	}
	report.SealedFiles = n

	aead, err := newAEAD(hostKey)
	if err != nil {
		return report, err
	}
	if m.DEK, err = sealWith(aead, dek, dekAAD(m.Authority)); err != nil {
		return report, err
	}
	if err := changelogfile.WriteManifest(repoDir, m); err != nil {
		return report, err
	}
	return report, nil
}

// lastRecoveryKey walks the changelog files for the last write of the
// recoverykey record and returns its seq and sealedKey. The fold is not
// available offline, so this reads the record delta the way the fold would.
func lastRecoveryKey(repoDir string) (int64, string, error) {
	log, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(repoDir))
	if err != nil {
		return 0, "", err
	}
	var seq int64
	var sealedKey string
	err = log.Walk(func(e changelogfile.Entry) error {
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
// not open the store would import a repository no login can open.
func sealedStoreOpens(repoDir string, dek []byte) (int, error) {
	files, err := changelogfile.ReadSealed(repoDir)
	if err != nil {
		return 0, err
	}
	for _, f := range files {
		if _, err := OpenPayloadWithKey(dek, f.Payload, sealedAAD(f.Ref, f.RecordKind, f.RecordID)); err != nil {
			return 0, fmt.Errorf("substrate/engine: the recovered DEK does not open sealed/%s (%s %s): %w",
				changelogfile.SealedFileName(f.Ref), f.RecordKind, f.RecordID, err)
		}
	}
	return len(files), nil
}
