package engine

// The sealed store's one key (decision 0059) against a database: a repository
// refuses a plain payload and one sealed under the host key, and a wrong or
// damaged DEK wrap is reported by key id. INTERNAL tests: they plant bytes in
// the sealed table and damage the control-plane row.

import (
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
func repoKeyRow(t *testing.T, ctx context.Context, svc *service, repoID string) (hasDEK bool, keyID string) {
	t.Helper()
	var id *string
	if err := svc.maint.QueryRowContext(ctx,
		`SELECT dek IS NOT NULL, dek_key_id FROM repositories WHERE id = $1`, repoID).
		Scan(&hasDEK, &id); err != nil {
		t.Fatalf("read the repository row: %v", err)
	}
	if id != nil {
		keyID = *id
	}
	return hasDEK, keyID
}

func TestARepositoryRefusesAPlainOrHostKeyedPayload(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)
	repoID := ds.scope.Repository

	// Born with a DEK, and the wrap names this host's key.
	hasDEK, keyID := repoKeyRow(t, ctx, ds.svc, repoID)
	if !hasDEK || keyID != hostKeyID(TestCredentialKeyBytes) {
		t.Fatalf("a fresh repository is not born with a DEK under the host key: dek=%v keyID=%q", hasDEK, keyID)
	}
	m, err := changelogfile.ReadManifest(ds.dir)
	if err != nil || m.DEKKeyID != keyID {
		t.Fatalf("the manifest does not carry the key id: %+v, %v", m, err)
	}

	ref := putProviderSecret(t, ds, "a", "secret-a")
	aad := sealedAAD(ref, bindingProviderKind, "a")

	// A plain payload is refused on every read path, by framing.
	plantSealed(t, ctx, ds, ref, append([]byte{credPlain}, []byte("secret-a")...))
	if got, err := ds.openSecretValue(ctx, ref); err == nil || !strings.Contains(err.Error(), "'p'") {
		t.Fatalf("the dataset read a plain payload: %q, %v", got, err)
	}
	if got, err := ds.svc.openSealed(ctx, repoID, ref); err == nil || !strings.Contains(err.Error(), "'p'") {
		t.Fatalf("the maintenance read opened a plain payload: %q, %v", got, err)
	}

	// A payload sealed under the host key is refused too, naming the key
	// expected: the host key is never tried on a repository's payload.
	plantSealed(t, ctx, ds, ref, hostSealed(t, ds, []byte("secret-a"), aad))
	if got, err := ds.openSecretValue(ctx, ref); err == nil || !strings.Contains(err.Error(), "repository DEK") {
		t.Fatalf("the dataset opened a host-key payload: %q, %v", got, err)
	}
	if got, err := ds.svc.openSealed(ctx, repoID, ref); err == nil || !strings.Contains(err.Error(), "repository DEK") {
		t.Fatalf("the maintenance read opened a host-key payload: %q, %v", got, err)
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
	_, err := OpenForTest(t, ctx, dsn,
		WithDataRoot(root),
		WithKindsDir(CoreKindsDir),
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

	_, err := OpenForTest(t, ctx, dsn,
		WithDataRoot(root),
		WithKindsDir(CoreKindsDir),
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
