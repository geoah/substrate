package engine

// A write is acknowledged only once its changelog lines and sealed files are
// on disk (decision 0062). These are INTERNAL tests: two of the three writers
// they break, updateCredential and consumeTOTPStep, have no public door that
// reaches them without a provider or an authenticator.

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// durabilityPassword is the password openDurabilityService registers with.
const durabilityPassword = "correct-horse-battery-staple"

// openDurabilityService registers a user, so a TOTP row exists to consume,
// and returns the service, its repository, the TOTP seed, the DSN and the
// data root. Registration spends the step BEFORE the current one, so the
// current code is still unspent for a login.
func openDurabilityService(t *testing.T, opts ...Option) (*service, Repository, string, string, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	dsn := testdb.NewSchema(t)
	svcIface, err := Open(ctx, dsn, append([]Option{
		WithDataRoot(root), WithCredentialKey(TestCredentialKey),
		WithKindsDir("../../kinds/substrate.reamde.dev/core"),
	}, opts...)...)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svcIface.Close() })
	s := svcIface.(*service)
	seed, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	code, err := TOTPCode(seed, TOTPStep(nowUTC())-1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Register(ctx, substrate.RegisterInput{
		Username: "geoah", Authority: "geoah.example.com", Password: durabilityPassword,
		TOTPSecret: seed, TOTPCode: code, Label: "cli",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	repo, err := s.repositoryByUsername(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	return s, repo, seed, dsn, root
}

// errDied is what a commit-fault hook returns where the process would die.
var errDied = errors.New("test: the process died here")

// oneShotFault is a WithTestCommitFault hook armed for one stage, once.
type oneShotFault struct {
	mu    sync.Mutex
	stage string
}

func (f *oneShotFault) arm(stage string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stage = stage
}

func (f *oneShotFault) hook(stage string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if stage != f.stage {
		return nil
	}
	f.stage = ""
	return errDied
}

// importCopy copies one repository's directory into a fresh data root and
// boots a service over an empty database, which imports it.
func importCopy(t *testing.T, root, repoID string) (*service, Repository) {
	t.Helper()
	ctx := context.Background()
	src, err := changelogfile.RepoDir(root, repoID)
	if err != nil {
		t.Fatal(err)
	}
	root2 := t.TempDir()
	dst, err := changelogfile.RepoDir(root2, repoID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	svc2, err := Open(ctx, testdb.NewSchema(t), WithDataRoot(root2), WithCredentialKey(TestCredentialKey),
		WithKindsDir("../../kinds/substrate.reamde.dev/core"))
	if err != nil {
		t.Fatalf("import the directory: %v", err)
	}
	t.Cleanup(func() { _ = svc2.Close() })
	s2 := svc2.(*service)
	repo2, err := s2.repositoryByUsername(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	return s2, repo2
}

// noPending fails the test when sealed/ holds a staged file.
func noPending(t *testing.T, dir, when string) {
	t.Helper()
	pending, err := changelogfile.PendingSealed(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("staged sealed files %s: %v", when, pending)
	}
}

// sealedPayloads reads sealed/ as ref to payload.
func sealedPayloads(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files, err := changelogfile.ReadSealed(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string][]byte, len(files))
	for _, f := range files {
		out[f.Ref] = f.Payload
	}
	return out
}

func providerIDs(t *testing.T, ds *dataset) []string {
	t.Helper()
	rows, err := ds.db.Query(`SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL ORDER BY id`, bindingProviderKind)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

// With the changelog writer broken, then with the sealed store broken, a
// write the directory cannot take is refused before its transaction commits:
// a put with a secret, a credential refresh and a TOTP step consume each
// return an error, neither store moves, and a fresh database that imports a
// copy of the directory holds exactly the acknowledged set. A sealed store
// that refuses is retryable, so the refusal is ErrDirectoryWrite, an
// ErrUnavailable; a changelog writer that failed serves nothing until a
// restart, so the first refusal latches ErrChangelogFileBehind and the
// sealed-only writes after it meet the latch.
func TestAWriteIsRefusedWhenTheDirectoryCannotTakeIt(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	for _, tc := range []struct {
		name string
		brk  func(*dataset)
		// want is the refusal every write meets, and retryable says whether it
		// is an ErrUnavailable.
		want      error
		retryable bool
	}{
		{"changelog writer", func(ds *dataset) { BreakChangelogWriter(ds) }, ErrChangelogFileBehind, false},
		{"sealed store", func(ds *dataset) { BreakSealedStore(ds) }, ErrDirectoryWrite, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			s, repo, _, _, root := openDurabilityService(t)
			ds, err := s.open(ctx, repo)
			if err != nil {
				t.Fatal(err)
			}

			// The acknowledged set: a provider with a secret, an OAuth
			// credential on it, one consumed TOTP step.
			account := eref{Kind: bindingProviderKind, ID: "acked"}
			putProviderSecret(t, ds, account.ID, "sk-acked")
			const credRef = "cred-acked"
			if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
				return tx.putCredential(credRef, account, &oauth2.Token{AccessToken: "first", RefreshToken: "r", Expiry: nowUTC().Add(time.Hour)})
			}); err != nil {
				t.Fatalf("put credential: %v", err)
			}
			mat, err := s.authMaterialOf(ctx, repo.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantStep := mat.totp.Step + 5
			if won, err := s.consumeTOTPStep(ctx, repo, mat.totpRef, wantStep); err != nil || !won {
				t.Fatalf("consume a step: won=%v err=%v", won, err)
			}
			const wantToken = "first"
			head := maxSeqOf(t, ds)
			filesBefore := sealedPayloads(t, ds.dir)
			_, _, seen, err := ds.getCredential(ctx, credRef)
			if err != nil {
				t.Fatal(err)
			}

			tc.brk(ds)

			// A changelog line and a sealed write in one transaction.
			_, err = ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
				Kind: bindingProviderKind, ID: "refused",
				Properties: map[string]any{"label": "refused", "wire": "openai", "baseURL": "https://llm.example.com/v1", "apiKey": "sk-refused"},
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("a put the directory cannot take: err = %v, want %v", err, tc.want)
			}
			if errors.Is(err, substrate.ErrUnavailable) != tc.retryable {
				t.Fatalf("a put the directory cannot take: err = %v, retryable = %v", err, tc.retryable)
			}
			// A sealed write and no line, twice.
			swapped, err := ds.updateCredential(ctx, credRef, account, &oauth2.Token{AccessToken: "second", RefreshToken: "r", Expiry: nowUTC().Add(time.Hour)}, seen)
			if !errors.Is(err, tc.want) || swapped {
				t.Fatalf("a credential refresh the directory cannot take: swapped=%v err=%v, want %v", swapped, err, tc.want)
			}
			won, err := s.consumeTOTPStep(ctx, repo, mat.totpRef, wantStep+5)
			if !errors.Is(err, tc.want) || won {
				t.Fatalf("a TOTP consume the directory cannot take: won=%v err=%v, want %v", won, err, tc.want)
			}

			// Neither store took the refused writes.
			if maxSeqOf(t, ds) != head {
				t.Fatal("a refused write reached the table")
			}
			if ids := providerIDs(t, ds); len(ids) != 1 || ids[0] != "acked" {
				t.Fatalf("providers after the refusal: %v", ids)
			}
			ro, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(ds.dir))
			if err != nil {
				t.Fatal(err)
			}
			if ro.Head() != head || ro.TruncatedBytes != 0 {
				t.Fatalf("the file after the refusal: head %d (want %d), %d bytes of incomplete tail", ro.Head(), head, ro.TruncatedBytes)
			}
			noPending(t, ds.dir, "after the refusals")
			filesAfter := sealedPayloads(t, ds.dir)
			if len(filesAfter) != len(filesBefore) {
				t.Fatalf("sealed/ holds %d files after the refusal, %d before", len(filesAfter), len(filesBefore))
			}
			for ref, before := range filesBefore {
				after, ok := filesAfter[ref]
				if !ok {
					t.Fatalf("sealed/%s is gone after the refusal", ref)
				}
				if string(after) != string(before) {
					t.Fatalf("sealed/%s changed under a refused write", ref)
				}
			}
			_ = s.Close()

			// A fresh database importing a copy of the directory holds exactly
			// the acknowledged set.
			s2, repo2 := importCopy(t, root, repo.ID)
			ds2, err := s2.open(ctx, repo2)
			if err != nil {
				t.Fatal(err)
			}
			if ids := providerIDs(t, ds2); len(ids) != 1 || ids[0] != "acked" {
				t.Fatalf("providers after the import: %v", ids)
			}
			tok, _, _, err := ds2.getCredential(ctx, credRef)
			if err != nil {
				t.Fatalf("the credential after the import: %v", err)
			}
			if tok.AccessToken != wantToken {
				t.Fatalf("the imported credential holds %q, want %q", tok.AccessToken, wantToken)
			}
			mat2, err := s2.authMaterialOf(ctx, repo2.ID)
			if err != nil {
				t.Fatal(err)
			}
			if mat2.totp.Step != wantStep {
				t.Fatalf("the imported TOTP step is %d, want %d", mat2.totp.Step, wantStep)
			}
		})
	}
}

// A crash between the sealed stage and the Postgres commit leaves the new
// payload under a pending name and the record's own file as it was. A fresh
// database importing the directory loads the old payload and drops the
// pending file at the first open; the original database's boot drops it too
// and its table stands. Neither store ever holds the write nobody
// acknowledged.
func TestACrashBetweenTheSealedStageAndTheCommitRestoresTheOldPayload(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	fault := &oneShotFault{}
	s, repo, _, dsn, root := openDurabilityService(t, WithTestCommitFault(fault.hook))
	ds, err := s.open(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	account := eref{Kind: bindingProviderKind, ID: "acked"}
	putProviderSecret(t, ds, account.ID, "sk-acked")
	const credRef = "cred-acked"
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		return tx.putCredential(credRef, account, &oauth2.Token{AccessToken: "first", RefreshToken: "r", Expiry: nowUTC().Add(time.Hour)})
	}); err != nil {
		t.Fatalf("put credential: %v", err)
	}
	mat, err := s.authMaterialOf(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	step := mat.totp.Step + 5
	if won, err := s.consumeTOTPStep(ctx, repo, mat.totpRef, step); err != nil || !won {
		t.Fatalf("consume a step: won=%v err=%v", won, err)
	}
	_, _, seen, err := ds.getCredential(ctx, credRef)
	if err != nil {
		t.Fatal(err)
	}
	filesBefore := sealedPayloads(t, ds.dir)

	fault.arm(commitAfterPrepare)
	if _, err := ds.updateCredential(ctx, credRef, account, &oauth2.Token{AccessToken: "second", RefreshToken: "r", Expiry: nowUTC().Add(time.Hour)}, seen); !errors.Is(err, errDied) {
		t.Fatalf("the refresh did not die at the seam: %v", err)
	}
	fault.arm(commitAfterPrepare)
	if _, err := s.consumeTOTPStep(ctx, repo, mat.totpRef, step+5); !errors.Is(err, errDied) {
		t.Fatalf("the consume did not die at the seam: %v", err)
	}
	pending, err := changelogfile.PendingSealed(ds.dir)
	if err != nil || len(pending) != 2 {
		t.Fatalf("two staged files, one per write, expected: %v, %v", pending, err)
	}
	filesAfter := sealedPayloads(t, ds.dir)
	for ref, before := range filesBefore {
		if string(filesAfter[ref]) != string(before) {
			t.Fatalf("sealed/%s changed before its transaction committed", ref)
		}
	}
	_ = s.Close()

	// The fresh database.
	s2, repo2 := importCopy(t, root, repo.ID)
	ds2, err := s2.open(ctx, repo2)
	if err != nil {
		t.Fatal(err)
	}
	tok, _, _, err := ds2.getCredential(ctx, credRef)
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "first" {
		t.Fatalf("the import loaded a payload whose transaction never committed: %q", tok.AccessToken)
	}
	mat2, err := s2.authMaterialOf(ctx, repo2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mat2.totp.Step != step {
		t.Fatalf("the imported TOTP step is %d, want %d", mat2.totp.Step, step)
	}
	noPending(t, ds2.dir, "after the import opened the repository")

	// The original database.
	svc3, err := Open(ctx, dsn, WithDataRoot(root), WithCredentialKey(TestCredentialKey), WithKindsDir("../../kinds/substrate.reamde.dev/core"))
	if err != nil {
		t.Fatalf("reboot: %v", err)
	}
	t.Cleanup(func() { _ = svc3.Close() })
	s3 := svc3.(*service)
	ds3, err := s3.open(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	noPending(t, ds3.dir, "after the reboot")
	if tok, _, _, err := ds3.getCredential(ctx, credRef); err != nil || tok.AccessToken != "first" {
		t.Fatalf("after the reboot the credential is %v, %v", tok, err)
	}
	if mat3, err := s3.authMaterialOf(ctx, repo.ID); err != nil || mat3.totp.Step != step {
		t.Fatalf("after the reboot the TOTP step is %d, %v, want %d", mat3.totp.Step, err, step)
	}
}

// A stage whose rename landed and whose directory fsync failed is refused,
// and the pending file it left is discarded with the refusal: the ref is
// recorded before the stage runs, not after it succeeds.
func TestAStageThatFailsAfterItsRenameLeavesNoPendingFile(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	s, repo, _, _, _ := openDurabilityService(t)
	ds, err := s.open(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	putProviderSecret(t, ds, "acked", "sk-acked")
	head := maxSeqOf(t, ds)
	filesBefore := sealedPayloads(t, ds.dir)

	BreakSealedStoreAfterStage(ds)
	_, err = ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: bindingProviderKind, ID: "refused",
		Properties: map[string]any{"label": "refused", "wire": "openai", "baseURL": "https://llm.example.com/v1", "apiKey": "sk-refused"},
	})
	if !errors.Is(err, ErrDirectoryWrite) {
		t.Fatalf("a put whose stage failed after its rename: err = %v, want ErrDirectoryWrite", err)
	}
	noPending(t, ds.dir, "after the refusal")
	filesAfter := sealedPayloads(t, ds.dir)
	if len(filesAfter) != len(filesBefore) {
		t.Fatalf("sealed/ holds %d files after the refusal, %d before", len(filesAfter), len(filesBefore))
	}
	if maxSeqOf(t, ds) != head {
		t.Fatal("a refused write reached the table")
	}
	ro, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(ds.dir))
	if err != nil {
		t.Fatal(err)
	}
	if ro.Head() != head || ro.TruncatedBytes != 0 {
		t.Fatalf("the file after the refusal: head %d (want %d), %d bytes of incomplete tail", ro.Head(), head, ro.TruncatedBytes)
	}
}

// A read-only process (WithDirectoryReadOnly) is not the directory's writer,
// so it spends no TOTP step: a login through it is refused with
// ErrDirectoryReadOnly before the row commits, the step in the table stands,
// no sealed file moves, and the code the user typed is still good on the
// server that owns the directory.
func TestAReadOnlyServiceRefusesToSpendATOTPStep(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	s, repo, seed, dsn, root := openDurabilityService(t)
	ds, err := s.open(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	dir := ds.dir
	mat, err := s.authMaterialOf(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	step := mat.totp.Step
	filesBefore := sealedPayloads(t, dir)
	_ = s.Close()

	roIface, err := Open(ctx, dsn, WithDataRoot(root), WithCredentialKey(TestCredentialKey),
		WithKindsDir("../../kinds/substrate.reamde.dev/core"), WithDirectoryReadOnly())
	if err != nil {
		t.Fatalf("open read-only: %v", err)
	}
	t.Cleanup(func() { _ = roIface.Close() })
	ro := roIface.(*service)
	code, err := TOTPCode(seed, TOTPStep(nowUTC()))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = ro.Login(ctx, substrate.LoginInput{Username: "geoah", Password: durabilityPassword, TOTPCode: code, Label: "ro"})
	if !errors.Is(err, ErrDirectoryReadOnly) {
		t.Fatalf("a login on a read-only service: err = %v, want ErrDirectoryReadOnly", err)
	}
	after, err := ro.authMaterialOf(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.totp.Step != step {
		t.Fatalf("the read-only service spent the TOTP step: %d, was %d", after.totp.Step, step)
	}
	noPending(t, dir, "after the refused login")
	filesAfter := sealedPayloads(t, dir)
	for ref, before := range filesBefore {
		if string(filesAfter[ref]) != string(before) {
			t.Fatalf("sealed/%s moved under a read-only service", ref)
		}
	}
	if len(filesAfter) != len(filesBefore) {
		t.Fatalf("sealed/ holds %d files, %d before", len(filesAfter), len(filesBefore))
	}
}

// A sealed-only transaction whose commit reports failure after Postgres
// committed has no changelog line whose seq gap would catch it, so the
// refusal latches at once: the caller gets ErrChangelogFileBehind, every
// later write meets the latch, the pending file is discarded and the record's
// file stays the old one, and the next boot rewrites it from the table.
func TestASealedOnlyCommitInDoubtLatchesUntilTheBootRewritesTheFile(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	fault := &oneShotFault{}
	s, repo, _, dsn, root := openDurabilityService(t, WithTestCommitFault(fault.hook))
	ds, err := s.open(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	putProviderSecret(t, ds, "acked", "sk-acked")
	mat, err := s.authMaterialOf(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	step := mat.totp.Step + 5
	filesBefore := sealedPayloads(t, ds.dir)

	fault.arm(commitInDoubt)
	won, err := s.consumeTOTPStep(ctx, repo, mat.totpRef, step)
	if !errors.Is(err, ErrChangelogFileBehind) || won {
		t.Fatalf("a sealed-only commit in doubt: won=%v err=%v, want ErrChangelogFileBehind", won, err)
	}
	if errors.Is(err, substrate.ErrUnavailable) {
		t.Fatalf("a latched refusal must not ask for a retry: %v", err)
	}
	after, err := s.authMaterialOf(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.totp.Step != step {
		t.Fatalf("the table's step is %d, want %d: the seam did not commit", after.totp.Step, step)
	}
	noPending(t, ds.dir, "after the doubted commit")
	if string(sealedPayloads(t, ds.dir)[mat.totpRef]) != string(filesBefore[mat.totpRef]) {
		t.Fatal("the record's file moved under a commit whose answer was lost")
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: bindingProviderKind, ID: "latched",
		Properties: map[string]any{"label": "latched", "wire": "openai", "baseURL": "https://llm.example.com/v1", "apiKey": "sk-latched"},
	}); !errors.Is(err, ErrChangelogFileBehind) {
		t.Fatalf("a write after the latch: err = %v, want ErrChangelogFileBehind", err)
	}
	_ = s.Close()

	svc2, err := Open(ctx, dsn, WithDataRoot(root), WithCredentialKey(TestCredentialKey), WithKindsDir("../../kinds/substrate.reamde.dev/core"))
	if err != nil {
		t.Fatalf("reboot: %v", err)
	}
	t.Cleanup(func() { _ = svc2.Close() })
	s2 := svc2.(*service)
	report, err := s2.VerifyRepository(ctx, "geoah")
	if err != nil || !report.OK {
		t.Fatalf("after the reboot: %+v, %v", report, err)
	}
	ds2, err := s2.open(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if string(sealedPayloads(t, ds2.dir)[mat.totpRef]) == string(filesBefore[mat.totpRef]) {
		t.Fatal("the boot did not rewrite the record's file from the table")
	}
}
