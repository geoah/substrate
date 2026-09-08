package engine

// The DEK-only marker (decision 0059) against a database: a fresh repository
// is born marked and refuses the legacy framings, an unmarked one is re-keyed
// and marked by its first open, and a wrong host key is reported by key id.
// INTERNAL tests: they plant bytes in the sealed table and rewind the
// control-plane row the way a pre-DEK release left it.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
)

// plantSealed overwrites one sealed row's payload through the scoped pool.
func plantSealed(t *testing.T, ctx context.Context, ds *dataset, ref string, payload []byte) {
	t.Helper()
	if _, err := ds.db.ExecContext(ctx, `UPDATE sealed SET payload = $1 WHERE ref = $2`, payload, ref); err != nil {
		t.Fatalf("plant payload at %s: %v", ref, err)
	}
}

// hostSealed seals raw under the service's host key, bound to the row, as a
// pre-DEK release sealed it.
func hostSealed(t *testing.T, ds *dataset, raw, aad []byte) []byte {
	t.Helper()
	aead, err := aeadOf(ds.svc.credKey)
	if err != nil || aead == nil {
		t.Fatalf("host key aead: %v", err)
	}
	out, err := sealWith(aead, raw, aad)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// repoKeyRow reads the control-plane row's key columns.
func repoKeyRow(t *testing.T, ctx context.Context, svc *service, repoID string) (hasDEK bool, keyID string, marked bool) {
	t.Helper()
	var id *string
	if err := svc.maint.QueryRowContext(ctx,
		`SELECT dek IS NOT NULL, dek_key_id, sealed_dek_only FROM repositories WHERE id = $1`, repoID).
		Scan(&hasDEK, &id, &marked); err != nil {
		t.Fatalf("read the repository row: %v", err)
	}
	if id != nil {
		keyID = *id
	}
	return hasDEK, keyID, marked
}

func TestMarkedRepositoryRefusesLegacyPayloads(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)
	repoID := ds.scope.Repository

	// Born marked, and the wrap names this host's key.
	hasDEK, keyID, marked := repoKeyRow(t, ctx, ds.svc, repoID)
	if !hasDEK || !marked || keyID != hostKeyID(TestCredentialKeyBytes) || !ds.dekOnly {
		t.Fatalf("a fresh repository is not born DEK-only under the host key: dek=%v keyID=%q marked=%v ds.dekOnly=%v", hasDEK, keyID, marked, ds.dekOnly)
	}
	m, err := changelogfile.ReadManifest(ds.dir)
	if err != nil || m.DEKKeyID != keyID || !m.SealedDEKOnly {
		t.Fatalf("the manifest does not carry the key id and the marker: %+v, %v", m, err)
	}

	ref := putProviderSecret(t, ds, "a", "secret-a")
	aad := sealedAAD(ref, bindingProviderKind, "a")

	// A plain payload is refused on every read path, by framing.
	plantSealed(t, ctx, ds, ref, append([]byte{credPlain}, []byte("secret-a")...))
	if got, err := ds.openSecretValue(ctx, ref); err == nil || !strings.Contains(err.Error(), "'p'") {
		t.Fatalf("the dataset read a plain payload on a marked repository: %q, %v", got, err)
	}
	if got, err := ds.svc.openSealed(ctx, repoID, ref); err == nil || !strings.Contains(err.Error(), "'p'") {
		t.Fatalf("the maintenance read opened a plain payload on a marked repository: %q, %v", got, err)
	}
	// The re-key itself will not launder it: a marked store has no legacy
	// payload to re-key, so one it meets is refused the same way.
	if err := ds.inRawTx(ctx, func(tx *txn) error {
		_, err := tx.rekeySealedStore()
		return err
	}); err == nil || !strings.Contains(err.Error(), "'p'") {
		t.Fatalf("the re-key accepted a plain payload on a marked repository: %v", err)
	}

	// A payload sealed under the host key is refused too, naming the key
	// expected; the fallback is not tried.
	plantSealed(t, ctx, ds, ref, hostSealed(t, ds, []byte("secret-a"), aad))
	if got, err := ds.openSecretValue(ctx, ref); err == nil || !strings.Contains(err.Error(), "repository DEK") || !strings.Contains(err.Error(), "host-key fallback is refused") {
		t.Fatalf("the dataset took the host-key fallback on a marked repository: %q, %v", got, err)
	}
	if got, err := ds.svc.openSealed(ctx, repoID, ref); err == nil || !strings.Contains(err.Error(), "repository DEK") {
		t.Fatalf("the maintenance read took the host-key fallback on a marked repository: %q, %v", got, err)
	}
}

// A repository a pre-DEK release left behind: no DEK, no key id, unmarked, a
// plain payload and a host-key-sealed one in its store. While unmarked the
// fallback still opens them; the first open adopts a DEK, re-keys both under
// it, marks the row, names the key and writes it all into the manifest and
// the sealed files, and from then on the legacy forms are refused.
func TestFirstOpenRekeysAndMarksAnUnmarkedRepository(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)
	repoID := ds.scope.Repository
	svc, dsn, root := ds.svc, ds.svc.dsn, ds.svc.dataRoot

	refA := putProviderSecret(t, ds, "a", "secret-a")
	refB := putProviderSecret(t, ds, "b", "secret-b")
	plantSealed(t, ctx, ds, refA, append([]byte{credPlain}, []byte("secret-a")...))
	plantSealed(t, ctx, ds, refB, hostSealed(t, ds, []byte("secret-b"), sealedAAD(refB, bindingProviderKind, "b")))
	if _, err := svc.maint.ExecContext(ctx,
		`UPDATE repositories SET dek = NULL, dek_key_id = NULL, sealed_dek_only = false WHERE id = $1`, repoID); err != nil {
		t.Fatalf("rewind the row to pre-DEK: %v", err)
	}

	// Unmarked, the maintenance read still opens both legacy forms.
	if got, err := svc.openSealed(ctx, repoID, refA); err != nil || string(got) != "secret-a" {
		t.Fatalf("the unmarked fallback did not open a plain payload: %q, %v", got, err)
	}
	if got, err := svc.openSealed(ctx, repoID, refB); err != nil || string(got) != "secret-b" {
		t.Fatalf("the unmarked fallback did not open a host-key payload: %q, %v", got, err)
	}
	_ = svc.Close()

	reopened, err := Open(ctx, dsn,
		WithDataRoot(root),
		WithKindsDir("../../kinds/substrate.reamde.dev/core"),
		WithCredentialKey(TestCredentialKey))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	d, err := reopened.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open the repository: %v", err)
	}
	ds2 := d.(*dataset)

	hasDEK, keyID, marked := repoKeyRow(t, ctx, ds2.svc, repoID)
	if !hasDEK || !marked || keyID != hostKeyID(TestCredentialKeyBytes) || !ds2.dekOnly {
		t.Fatalf("the first open did not adopt, re-key and mark: dek=%v keyID=%q marked=%v ds.dekOnly=%v", hasDEK, keyID, marked, ds2.dekOnly)
	}
	for ref, payload := range snapshotSealed(t, ctx, ds2) {
		if len(payload) == 0 || payload[0] != credBoundSealed {
			t.Fatalf("row %s is still %q-framed after the first open", ref, payload[0])
		}
	}
	if got, err := ds2.openSecretValue(ctx, refA); err != nil || got != "secret-a" {
		t.Fatalf("row A after the re-key: %q, %v", got, err)
	}
	if got, err := ds2.openSecretValue(ctx, refB); err != nil || got != "secret-b" {
		t.Fatalf("row B after the re-key: %q, %v", got, err)
	}
	m, err := changelogfile.ReadManifest(ds2.dir)
	if err != nil || m.DEKKeyID != keyID || !m.SealedDEKOnly || len(m.DEK) == 0 {
		t.Fatalf("the manifest does not carry the adopted wrap, its key id and the marker: %+v, %v", m, err)
	}
	files, err := changelogfile.ReadSealed(ds2.dir)
	if err != nil || len(files) != 2 {
		t.Fatalf("sealed files after the re-key: %d, %v", len(files), err)
	}
	for _, f := range files {
		if _, err := OpenPayloadWithKey(ds2.dek, f.Payload, sealedAAD(f.Ref, f.RecordKind, f.RecordID)); err != nil {
			t.Fatalf("sealed file %s does not open under the DEK alone: %v", f.Ref, err)
		}
	}
	// Marked now: a planted plain payload is refused on the maintenance path
	// that opened it a moment ago.
	plantSealed(t, ctx, ds2, refA, append([]byte{credPlain}, []byte("secret-a")...))
	if got, err := ds2.svc.openSealed(ctx, repoID, refA); err == nil || !strings.Contains(err.Error(), "'p'") {
		t.Fatalf("the marked repository still opens a plain payload: %q, %v", got, err)
	}
}

// A host booting with the wrong SUBSTRATE_CREDENTIAL_KEY is told which key the
// wraps name and which key it holds, by id, beside the repository.
func TestWrongHostKeyIsNamedById(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)
	repoID := ds.scope.Repository
	dsn, root := ds.svc.dsn, ds.svc.dataRoot
	_ = ds.svc.Close()

	other := make([]byte, 32)
	if _, err := rand.Read(other); err != nil {
		t.Fatal(err)
	}
	_, err := Open(ctx, dsn,
		WithDataRoot(root),
		WithKindsDir("../../kinds/substrate.reamde.dev/core"),
		WithCredentialKey(base64.StdEncoding.EncodeToString(other)))
	if err == nil {
		t.Fatal("a host with the wrong key booted over the database")
	}
	msg := err.Error()
	for _, want := range []string{"SUBSTRATE_CREDENTIAL_KEY", repoID, hostKeyID(TestCredentialKeyBytes), hostKeyID(other)} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the refusal does not name %q: %v", want, err)
		}
	}
}

// A DEK wrap written plain by a keyless release, and one sealed before key
// ids existed, both name no key. A keyed host must not name its key over a
// wrap it does not protect: the first open re-wraps the DEK under the host key
// and only then records the id, and the DEK itself does not change.
func TestFirstOpenRewrapsAWrapThatNamesNoKey(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		wrap func(ds *dataset) []byte
	}{
		{"plain", func(ds *dataset) []byte { return append([]byte{credPlain}, ds.dek...) }},
		{"sealed without an id", func(ds *dataset) []byte {
			wrapped, err := ds.svc.wrapDEK(ds.dek, ds.scope.Repository)
			if err != nil {
				t.Fatal(err)
			}
			return wrapped
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ds := openInternalDataset(t)
			repoID := ds.scope.Repository
			dek := append([]byte(nil), ds.dek...)
			svc, dsn, root := ds.svc, ds.svc.dsn, ds.svc.dataRoot
			if _, err := svc.maint.ExecContext(ctx,
				`UPDATE repositories SET dek = $2, dek_key_id = NULL WHERE id = $1`, repoID, tc.wrap(ds)); err != nil {
				t.Fatalf("rewind the wrap: %v", err)
			}
			_ = svc.Close()

			reopened, err := Open(ctx, dsn,
				WithDataRoot(root),
				WithKindsDir("../../kinds/substrate.reamde.dev/core"),
				WithCredentialKey(TestCredentialKey))
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			t.Cleanup(func() { _ = reopened.Close() })
			d, err := reopened.Dataset(ctx, "geoah")
			if err != nil {
				t.Fatalf("open the repository: %v", err)
			}
			ds2 := d.(*dataset)
			if !bytes.Equal(ds2.dek, dek) {
				t.Fatal("the re-wrap changed the DEK")
			}
			var wrapped []byte
			var keyID *string
			if err := ds2.svc.maint.QueryRowContext(ctx,
				`SELECT dek, dek_key_id FROM repositories WHERE id = $1`, repoID).Scan(&wrapped, &keyID); err != nil {
				t.Fatal(err)
			}
			if keyID == nil || *keyID != hostKeyID(TestCredentialKeyBytes) {
				t.Fatalf("the wrap names %v, want this host's key", keyID)
			}
			if len(wrapped) == 0 || wrapped[0] != credBoundSealed {
				t.Fatalf("the wrap is still %q-framed after a keyed open named it", wrapped[0])
			}
			if got, err := OpenPayloadWithKey(TestCredentialKeyBytes, wrapped, dekAAD(repoID)); err != nil || !bytes.Equal(got, dek) {
				t.Fatalf("the named wrap does not open under the named key: %v", err)
			}
			m, err := changelogfile.ReadManifest(ds2.dir)
			if err != nil || m.DEKKeyID != *keyID || !bytes.Equal(m.DEK, wrapped) {
				t.Fatalf("the manifest does not carry the re-wrap and its id: %+v, %v", m, err)
			}
		})
	}
}

// A wrap that names this host's key and still does not open is damage, and
// the boot must say so rather than send the operator hunting for another key.
func TestBootNamesADamagedWrapUnderTheSameKey(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)
	repoID := ds.scope.Repository
	dsn, root := ds.svc.dsn, ds.svc.dataRoot
	// Flip the wrap's last byte, the GCM tag's, and leave the id it names.
	if _, err := ds.svc.maint.ExecContext(ctx,
		`UPDATE repositories SET dek = set_byte(dek, length(dek) - 1, (get_byte(dek, length(dek) - 1) + 1) % 256) WHERE id = $1`, repoID); err != nil {
		t.Fatalf("damage the wrap: %v", err)
	}
	_ = ds.svc.Close()

	_, err := Open(ctx, dsn,
		WithDataRoot(root),
		WithKindsDir("../../kinds/substrate.reamde.dev/core"),
		WithCredentialKey(TestCredentialKey))
	if err == nil {
		t.Fatal("a damaged wrap booted")
	}
	msg := err.Error()
	for _, want := range []string{repoID, hostKeyID(TestCredentialKeyBytes), "damaged", "keep it"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the refusal does not say %q: %v", want, err)
		}
	}
	if strings.Contains(msg, "written under a different one") {
		t.Fatalf("a damaged wrap under the right key was blamed on the key: %v", err)
	}
}
