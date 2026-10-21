package engine_test

// The bulk seed (bulkwrite.go): which path a seed takes, a registration's
// whole transaction around it, the runs it must decline, and a fault inside
// its flush and around its commit. Each case ends with the repository's
// segment files held to its table (VerifyRepository and an import of the
// directory into an empty database), every checksum stamped, the seed's
// entries framed as one transaction, and a rebuild equal to the live fold.

import (
	"context"
	"errors"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// seedPaths records which path every seed took (engine.WithTestSeedPath):
// "" for one bulk run, else why it went one put at a time.
type seedPaths struct {
	mu  sync.Mutex
	got []string
}

func (p *seedPaths) hook(_, refusal string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got = append(p.got, refusal)
}

// want fails the test unless the seeds since the last call took exactly the
// paths named, in order.
func (p *seedPaths) want(t *testing.T, when string, paths ...string) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if !slices.Equal(p.got, paths) {
		t.Fatalf("%s: the seeds took %q, want %q (\"\" is the bulk run)", when, p.got, paths)
	}
	p.got = nil
}

// errFault is what a fault hook returns where the write fails or the
// process dies.
var errFault = errors.New("test: the seed failed here")

// oneFault is a fault hook for one stage, once, from when it is armed.
type oneFault struct {
	mu    sync.Mutex
	stage string
	armed bool
	hit   bool
}

func (f *oneFault) arm() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.armed = true
}

func (f *oneFault) hook(stage string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if stage != f.stage || !f.armed || f.hit {
		return nil
	}
	f.hit = true
	return errFault
}

func (f *oneFault) fired(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.hit {
		t.Fatalf("the fault at %q never fired", f.stage)
	}
}

// seedIsWhole fails the test unless the repository holds every shipped
// unit and conversion at the revision a whole seed leaves.
func seedIsWhole(t *testing.T, ds substrate.Dataset, when string) {
	t.Helper()
	units, convs := shippedCounts()
	if tab := unitsOf(t, ds); len(tab.Units) != units || len(tab.Conversions) != convs || len(tab.Unseeded) != 0 {
		t.Fatalf("%s: %d units, %d conversions and %d unseeded, want %d, %d and none",
			when, len(tab.Units), len(tab.Conversions), len(tab.Unseeded), units, convs)
	}
}

// seedEntriesAreOneTransaction fails the test unless the seed's changelog
// entries are contiguous seqs of one transaction, its `txn` the seq of the
// transaction's last entry, and every entry of the repository carries its
// checksum.
func seedEntriesAreOneTransaction(t *testing.T, svc substrate.Service, repo, when string) {
	t.Helper()
	db, err := engine.SharedScopedDB(svc, repo)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	kinds := []string{vocabulary.KindUnit, vocabulary.KindConversion}
	var txns, n, lo, hi, txn int64
	if err := db.QueryRowContext(ctx, `
		SELECT count(DISTINCT txn), count(*), min(seq), max(seq), max(txn)
		FROM changelog WHERE kind = ANY($1::text[])`, kinds).Scan(&txns, &n, &lo, &hi, &txn); err != nil {
		t.Fatal(err)
	}
	units, convs := shippedCounts()
	if txns != 1 || n != int64(units+convs) || hi-lo+1 != n {
		t.Fatalf("%s: the seed's %d entries span seqs %d to %d in %d transactions, want %d contiguous in one", when, n, lo, hi, txns, units+convs)
	}
	var last, unstamped int64
	if err := db.QueryRowContext(ctx, `SELECT max(seq) FROM changelog WHERE txn = $1`, txn).Scan(&last); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE hash IS NULL OR txn IS NULL`).Scan(&unstamped); err != nil {
		t.Fatal(err)
	}
	if last != txn || unstamped != 0 {
		t.Fatalf("%s: the seed's transaction is %d and its last entry %d; %d entries carry no checksum or transaction", when, txn, last, unstamped)
	}
}

// filesAgree fails the test unless the repository's segment files and its
// table agree: the verification walk finds nothing (every line's checksum,
// every stamped checksum against its line, both heads), no unfinished tail
// is left, and an empty database that imports a copy of the directory folds
// it to the tables this one holds.
func filesAgree(t *testing.T, svc substrate.Service, repo, when string) {
	t.Helper()
	report := mustVerify(t, svc, repo)
	if !report.OK || report.FileHead != report.Head || report.TruncatedEntries != 0 {
		t.Fatalf("%s: verify: ok %v, file head %d, table head %d, %d unfinished entries, findings %q",
			when, report.OK, report.FileHead, report.Head, report.TruncatedEntries, report.Findings)
	}
	src, err := changelogfile.RepoDir(engine.DataRootOf(svc), repo)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dst, err := changelogfile.RepoDir(root, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	imported := reopenWith(t, engine.MigratedDSN(t), root, engine.WithUnitSeed(false))
	defer func() { _ = imported.Close() }()
	ds, err := svc.Dataset(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := imported.Dataset(context.Background(), repo)
	if err != nil {
		t.Fatalf("%s: open the imported repository: %v", when, err)
	}
	if got, want := foldOf(t, ids), foldOf(t, ds); string(got) != string(want) {
		t.Fatalf("%s: the imported directory's fold differs from the table's\n%s", when, firstDifference(want, got))
	}
}

// Register is the registration a person makes: the seed, then the
// credential, the recovery key and the token in the same transaction. Its
// seed runs in bulk and writes what the per-record path writes, the
// entries after it land at the same seqs, the whole transaction is one
// frame in the table and in the files, and a rebuild reproduces the fold.
func TestRegisterSeedsInBulkAsThePerRecordPathDoes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := testdb.Repository(t)
	register := func(opts ...engine.Option) (substrate.Service, *seedPaths) {
		paths := &seedPaths{}
		svc, _ := newService(t, append([]engine.Option{engine.WithUnitSeed(true), engine.WithTestSeedPath(paths.hook)}, opts...)...)
		registerUser(t, svc, repo)
		return svc, paths
	}
	bulkSvc, bulkPaths := register()
	bulkPaths.want(t, "the bulk registration", "")
	singleSvc, singlePaths := register(engine.WithTestPerRecordSeed())
	singlePaths.want(t, "the per-record registration", engine.BulkRefusedOff)

	if bulk, single := seedState(t, bulkSvc, repo), seedState(t, singleSvc, repo); bulk != single {
		t.Fatalf("the bulk registration's seed differs from the per-record one\n%s", firstDifference([]byte(single), []byte(bulk)))
	}
	// Every entry of the registration, the auth material's after the seed's,
	// at the same seq with the same op, kind and frame; one transaction.
	frame := func(svc substrate.Service) []string {
		db, err := engine.SharedScopedDB(svc, repo)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := db.QueryContext(ctx, `
			SELECT seq || ' ' || op || ' ' || kind || ' ' || actor || ' ' || (txn = (SELECT max(seq) FROM changelog))
			FROM changelog ORDER BY seq`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			out = append(out, line)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	bulkFrame, singleFrame := frame(bulkSvc), frame(singleSvc)
	if !slices.Equal(bulkFrame, singleFrame) {
		t.Fatalf("the registrations' changelogs differ:\nbulk   %q\nsingle %q", bulkFrame, singleFrame)
	}
	units, convs := shippedCounts()
	if len(bulkFrame) <= units+convs {
		t.Fatalf("the registration wrote %d entries, no more than the seed's %d", len(bulkFrame), units+convs)
	}
	for _, line := range bulkFrame {
		if line[len(line)-4:] != "true" {
			t.Fatalf("entry %q is outside the registration's one transaction", line)
		}
	}
	for _, svc := range []substrate.Service{bulkSvc, singleSvc} {
		ds, err := svc.Dataset(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		seedIsWhole(t, ds, "after the registration")
		seedEntriesAreOneTransaction(t, svc, repo, "after the registration")
		report := mustVerify(t, svc, repo)
		if report.SealedOpened != report.SealedRows || report.SealedRows == 0 {
			t.Fatalf("after the registration: %d sealed rows, %d opened", report.SealedRows, report.SealedOpened)
		}
		filesAgree(t, svc, repo, "after the registration")
		rebuiltEqualsLive(t, svc, ds, "after the registration")
	}
}

// A seed that must write over a record a row already answers, live or
// tombstoned, goes one put at a time, and says why: the planned refresh of
// a shipped unit another writer edited, and the restore of a shipped
// conversion another writer deleted.
func TestASeedOverAHeldRecordGoesOneByOne(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		write func(context.Context, substrate.Dataset) error
		check func(*testing.T, substrate.UnitTable)
	}{
		{
			name: "a planned refresh",
			write: func(ctx context.Context, ds substrate.Dataset) error {
				_, err := ds.Patch(ctx, substrate.ActorSystem, vocabulary.KindUnit, "g", substrate.PatchInput{Properties: map[string]any{"name": "GRAM"}})
				return err
			},
			check: func(t *testing.T, tab substrate.UnitTable) {
				if g, _ := unitByID(tab, "g"); g.Name != "gram" {
					t.Fatalf("g's name is %q, want the shipped gram back", g.Name)
				}
			},
		},
		{
			name: "a tombstone",
			write: func(ctx context.Context, ds substrate.Dataset) error {
				_, err := ds.Delete(ctx, substrate.ActorSystem, vocabulary.KindConversion, "oz-kg", substrate.DeleteInput{})
				return err
			},
			check: func(t *testing.T, tab substrate.UnitTable) {
				if _, ok := conversionByID(tab, "oz-kg"); !ok {
					t.Fatal("the conversion another writer deleted was not restored")
				}
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			paths := &seedPaths{}
			svc, ds, dsn, root := unitSeededRepository(t, true, engine.WithTestSeedPath(paths.hook))
			paths.want(t, "the registration", "")
			if err := c.write(ctx, ds); err != nil {
				t.Fatal(err)
			}
			_ = svc.Close()
			svc = reopenWith(t, dsn, root, engine.WithUnitSeed(true), engine.WithTestSeedPath(paths.hook))
			ds, err := svc.Dataset(ctx, testdb.Repository(t))
			if err != nil {
				t.Fatal(err)
			}
			paths.want(t, "the open", engine.BulkRefusedHeld)
			seedIsWhole(t, ds, "after the open")
			c.check(t, unitsOf(t, ds))
			filesAgree(t, svc, testdb.Repository(t), "after the open")
			rebuiltEqualsLive(t, svc, ds, "after the open")
		})
	}
}

// A fault after any table of a registration's bulk flush fails the
// registration whole: nothing of the repository is left, in the database
// or on disk, and the next registration under the name seeds in bulk.
func TestAFaultInARegistrationsBulkFlushLeavesNothing(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{engine.BulkAfterRecords, engine.BulkAfterRefs, engine.BulkAfterEntries} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			repo := testdb.Repository(t)
			fault := &oneFault{stage: stage, armed: true}
			paths := &seedPaths{}
			svc, _ := newService(t, engine.WithUnitSeed(true), engine.WithTestBulkFault(fault.hook), engine.WithTestSeedPath(paths.hook))
			if _, err := svc.CreateRepository(ctx, repo); !errors.Is(err, errFault) {
				t.Fatalf("the registration did not fail at the fault: %v", err)
			}
			fault.fired(t)
			paths.want(t, "the failed registration")
			if _, err := svc.Dataset(ctx, repo); !errors.Is(err, substrate.ErrNotFound) {
				t.Fatalf("the failed registration's repository opens: %v", err)
			}
			dir, err := changelogfile.RepoDir(engine.DataRootOf(svc), repo)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the failed registration left its directory: %v", err)
			}
			registerUser(t, svc, repo)
			paths.want(t, "the second registration", "")
			ds, err := svc.Dataset(ctx, repo)
			if err != nil {
				t.Fatal(err)
			}
			seedIsWhole(t, ds, "after the second registration")
			seedEntriesAreOneTransaction(t, svc, repo, "after the second registration")
			filesAgree(t, svc, repo, "after the second registration")
			rebuiltEqualsLive(t, svc, ds, "after the second registration")
		})
	}
}

// The open of a repository registered without the units seeds them in one
// bulk run, through the writer, so its lines are prepared before the
// commit and finished after it. A fault inside the flush rolls the seed
// back and leaves the files as they were; the process dying after the
// lines are prepared leaves an unfinished tail the next open cuts, before
// it seeds again; and the process dying after the commit leaves the table
// ahead of the files, which the next open appends to them. Each way the
// next open ends with the whole seed in one transaction, in the table and
// in the files alike.
func TestAFaultAroundTheOpensBulkSeedIsRecovered(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		stage string
		bulk  bool
		// reseeds is whether the next open writes the seed again: true
		// where the faulted one never committed.
		reseeds bool
	}{
		{stage: engine.BulkAfterManagers, bulk: true, reseeds: true},
		{stage: engine.CommitAfterPrepare, reseeds: true},
		{stage: engine.CommitAfterCommit, reseeds: false},
	} {
		t.Run(c.stage, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			repo := testdb.Repository(t)
			svc, ds, dsn, root := unitSeededRepository(t, false)
			if tab := unitsOf(t, ds); len(tab.Units) != 0 {
				t.Fatalf("registered without the seed, the repository holds %d units", len(tab.Units))
			}
			_ = svc.Close()

			// The fault arms when the seed has chosen its path, so no other
			// write of the open meets it.
			fault := &oneFault{stage: c.stage}
			paths := &seedPaths{}
			opts := []engine.Option{engine.WithUnitSeed(true), engine.WithTestSeedPath(func(repo, refusal string) {
				paths.hook(repo, refusal)
				fault.arm()
			})}
			if c.bulk {
				// Only the seed runs in bulk, and it flushes before it
				// reports its path.
				fault.arm()
				opts = append(opts, engine.WithTestBulkFault(fault.hook))
			} else {
				opts = append(opts, engine.WithTestCommitFault(fault.hook))
			}
			svc = reopenWith(t, dsn, root, opts...)
			// The open logs a failed seed and opens on what it holds.
			ds, err := svc.Dataset(ctx, repo)
			if err != nil {
				t.Fatalf("the open: %v", err)
			}
			fault.fired(t)
			if c.bulk {
				paths.want(t, "the faulted open")
			} else {
				paths.want(t, "the faulted open", "")
			}
			switch report := mustVerify(t, svc, repo); c.stage {
			case engine.BulkAfterManagers:
				// A refused write ends nothing: the files stand as the
				// table does, and neither holds a unit.
				filesAgree(t, svc, repo, "after the fault in the flush")
				if tab := unitsOf(t, ds); len(tab.Units) != 0 {
					t.Fatalf("the rolled back seed left %d units", len(tab.Units))
				}
			case engine.CommitAfterPrepare:
				if report.TruncatedEntries == 0 || report.FileHead != report.Head {
					t.Fatalf("after the death past the prepare: %d unfinished entries, file head %d, table head %d; want the seed's lines unfinished and the heads equal",
						report.TruncatedEntries, report.FileHead, report.Head)
				}
			case engine.CommitAfterCommit:
				if report.TruncatedEntries == 0 || report.FileHead >= report.Head {
					t.Fatalf("after the death past the commit: %d unfinished entries, file head %d, table head %d; want the table ahead",
						report.TruncatedEntries, report.FileHead, report.Head)
				}
			}
			_ = svc.Close()

			svc = reopenWith(t, dsn, root, engine.WithUnitSeed(true), engine.WithTestSeedPath(paths.hook))
			ds, err = svc.Dataset(ctx, repo)
			if err != nil {
				t.Fatal(err)
			}
			if c.reseeds {
				paths.want(t, "the next open", "")
			} else {
				paths.want(t, "the next open")
			}
			seedIsWhole(t, ds, "after the next open")
			seedEntriesAreOneTransaction(t, svc, repo, "after the next open")
			filesAgree(t, svc, repo, "after the next open")
			rebuiltEqualsLive(t, svc, ds, "after the next open")
		})
	}
}
