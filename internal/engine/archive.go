package engine

// THE ARCHIVE (decisions 0262 and 0279, docs/operations.md "The archive's
// contract").
//
// Postgres confirms a write. The archive is a copy of each repository taken
// after the fact, by a pass that runs behind the commits, into a store of
// four operations (internal/archive): a directory by default, or a bucket.
// A repository's copy is one ARCHIVE at a time, a life under an id of its
// own (changelogfile/archive.go names its objects), and what the database
// knows of it is the RECORD: archive_records (the archive it writes, the
// manifest it last published, the exact bytes of the one it is about to
// publish) and archive_objects (one row for every object it puts or
// deletes, written before the request is sent). The process's own state
// below is a copy of the record, read again after any pass that fails.
//
// ONE PASS:
//
//  1. lists the repository's manifests and holds them to the record (rule
//     4): it goes on only in the archive this database writes, as it left
//     it. Anything else stops the passes, writes nothing and deletes
//     nothing, and the server goes on serving (ErrArchiveDiverged);
//  2. puts the manifest the record says is about to be published again,
//     the same bytes, and records it published (rule 3);
//  3. reads one snapshot of the repository and puts what the manifest it
//     started from does not select: blobs, a bounded amount (the rest are
//     named pending), the spool, sealed values, the pieces past the mark,
//     each under a name that carries the pass, after its row (rule 1);
//  4. records the manifest's exact bytes as about to be published, puts
//     them, and records the publication with every object it drops in one
//     transaction (rules 2 and 3);
//  5. deletes what the record says it dropped, each after its row says the
//     delete is about to be sent, and the manifests of a lower epoch (rule
//     5).
//
// The pass never stops a write. When the store is unreachable the passes
// fail, the delay grows, and the metrics say by how much
// (substrate_archive_*). How far behind is too far is not decided here
// (issue #1314).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/metrics"
	"github.com/geoah/substrate/internal/substrate"
)

// DefaultArchiveInterval is how often a repository's archive pass runs when
// no option names an interval.
const DefaultArchiveInterval = 10 * time.Second

// DefaultArchivePieceBytes is the size a piece is cut at: the first
// transaction boundary at or past it ends the piece.
const DefaultArchivePieceBytes int64 = 64 << 20

// DefaultArchiveStepBytes bounds the blob bytes one pass copies before it
// publishes; the blobs past it are named pending and the next pass copies
// them. A process stopped in the middle of a long copy loses at most one
// step, because an object a manifest never selected is only ever deleted
// after a restart (rule 1).
const DefaultArchiveStepBytes int64 = 1 << 30

// archiveTailBytes is the size under which the last piece is written again
// with the next pass's lines rather than followed by a new piece, so a
// repository written to a little at a time does not leave a piece per pass.
// Past it a piece is never rewritten.
const archiveTailBytes int64 = 1 << 20

// archivePageRows is how many changelog rows a pass reads at a time. A
// variable only so a test can page a short history.
var archivePageRows = 2000

// ErrArchiveDiverged is the refusal of a pass whose store does not hold the
// archive as this database left it (rule 4): another archive, another
// manifest, two of one epoch, the deletion mark, or the recorded manifest
// gone. Writing would decide between the two, which is the operator's to do
// (`substratectl archive report`, then `archive resolve`), so the passes
// write nothing and delete nothing until then.
var ErrArchiveDiverged = errors.New("substrate/engine: the archive is not as this database left it; resolve it with `substratectl archive report` and `archive resolve`")

// The pass's durable steps, as the test seam names them (testArchiveFault).
const (
	archiveAfterSpool     = "after the spool was copied, before the snapshot"
	archiveAfterListing   = "after the listing was held to the record"
	archiveAfterObjectRow = "after an object's row, before its put"
	archiveAfterObjectPut = "after an object's put, before its row said put"
	archiveAfterBlobs     = "after the blobs"
	archiveAfterSealed    = "after the sealed values"
	archiveAfterPiece     = "after a piece"
	archiveAfterPending   = "after the manifest was recorded as about to be published"
	archiveAfterManifest  = "after the manifest"
	archiveAfterPublished = "after the publication was recorded"
	archiveAfterMark      = "after the mark"
	archiveAfterDeleteRow = "after a delete's row, before the delete"
	archiveAfterDeleteHit = "after a delete, before its row went"
	archiveAfterDelete    = "after a delete and its row"
)

// The states of an object's row in archive_objects.
const (
	objectPutting  = "putting"
	objectPut      = "put"
	objectDropped  = "dropped"
	objectDeleting = "deleting"
)

// archiveRecord is a repository's row of archive_records.
type archiveRecord struct {
	archive        string
	epoch          int64
	published      string
	publishedBytes []byte
	pending        []byte
	stopped        string
	source         string
}

// archiveObject is one row of archive_objects.
type archiveObject struct {
	name, holds, process, state string
	published, pending          bool
}

// archiveState is one repository's archive as this process knows it. mu
// holds one pass at a time.
type archiveState struct {
	mu sync.Mutex
	// known is set once the record has been read this process: rec, start
	// and objects are then the database's.
	known   bool
	rec     archiveRecord
	start   changelogfile.Manifest
	objects map[string]*archiveObject
	// stopped is why rule 4 stopped the passes: they write nothing for the
	// rest of the process. reported is set once the process has said which
	// manifests it found that the record does not hold.
	stopped  string
	reported bool
	// mark and the prints are what the last manifest built from a snapshot
	// was built from, so an idle repository costs one query and a read of a
	// directory.
	mark                                   int64
	dialect                                int
	sealedPrint, spoolPrint, manifestPrint string
	missing, pendingBlobs                  int
	statMu                                 sync.Mutex
	stat                                   metrics.ArchiveStats
	err                                    error
}

func newArchiveState(archiving bool) *archiveState {
	if !archiving {
		return nil
	}
	return &archiveState{}
}

func (a *archiveState) stats() metrics.ArchiveStats {
	a.statMu.Lock()
	defer a.statMu.Unlock()
	return a.stat
}

// ArchiveStatus is a repository's archive as the process that writes it
// reports it.
type ArchiveStatus struct {
	Store    string
	Archive  string
	Mark     int64
	Pending  int64
	Oldest   time.Time
	LastPass time.Time
	Failures int
	// Missing is how many blobs the manifest names as missing, and
	// PendingBlobs how many as not copied yet.
	Missing, PendingBlobs int
	// Stopped is why rule 4 stopped the passes, empty while they go on.
	Stopped string
	Err     error
}

// ArchiveStatus reports where the repository's archive stands, or false when
// this process writes no archive.
func (ds *dataset) ArchiveStatus() (ArchiveStatus, bool) {
	a := ds.archive
	if a == nil {
		return ArchiveStatus{}, false
	}
	a.mu.Lock()
	archive, mark, stopped := a.rec.archive, a.mark, a.stopped
	a.mu.Unlock()
	a.statMu.Lock()
	defer a.statMu.Unlock()
	return ArchiveStatus{
		Store: ds.svc.archive.String(), Archive: archive, Mark: mark, Pending: a.stat.Pending, Oldest: a.stat.Oldest,
		LastPass: a.stat.LastPass, Failures: a.stat.Failures, Missing: a.stat.Missing, PendingBlobs: a.stat.PendingBlobs,
		Stopped: stopped, Err: a.err,
	}, true
}

func (s *service) archiveFault(stage string) error {
	if s.testArchiveFault == nil {
		return nil
	}
	return s.testArchiveFault(stage)
}

// startArchiver runs the repository's passes behind the open, every interval,
// until the dataset closes.
func (ds *dataset) startArchiver() {
	if ds.archive == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	ds.archiveStop = func() {
		cancel()
		<-done
	}
	started := ds.spawn("archive", func(bg context.Context) {
		defer close(done)
		stop := context.AfterFunc(bg, cancel)
		defer stop()
		tick := time.NewTicker(ds.svc.archiveInterval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			ds.runArchivePass(ctx)
		}
	})
	if !started {
		close(done)
	}
}

// stopArchiver ends the repository's passes and runs one more, bounded by
// archiveFlushTimeout, so a server stopped cleanly leaves its archive at the
// head it stopped at: the copy an operator takes before an upgrade, or a
// restore from it, is then the whole history. A pass that does not finish is
// said at the stop, because the next look at the delay may be after that
// copy is taken.
func (ds *dataset) stopArchiver() {
	if ds.archiveStop == nil {
		return
	}
	ds.archiveStop()
	ctx, cancel := context.WithTimeout(context.Background(), archiveFlushTimeout)
	defer cancel()
	err := ds.ArchivePass(ctx)
	if err == nil {
		return
	}
	ds.archive.mu.Lock()
	mark := ds.archive.mark
	ds.archive.mu.Unlock()
	ds.svc.log.Error("substrate: the archive pass at the close did not finish; the archive stays at its mark, behind the head, until a server runs a pass again, and a copy of it taken now is not the whole history",
		"repository", ds.info.ID, "store", ds.svc.archive.String(), "mark", mark, "error", err)
}

// archiveFlushTimeout bounds the pass a closing dataset runs: a store that
// does not answer must not hold a shutdown past its orchestrator's grace.
const archiveFlushTimeout = 30 * time.Second

// runArchivePass runs one pass and records how it went: a failure is logged
// when it starts and when it ends, not at every tick between.
func (ds *dataset) runArchivePass(ctx context.Context) {
	err := ds.ArchivePass(ctx)
	if ctx.Err() != nil {
		return
	}
	a := ds.archive
	a.statMu.Lock()
	failures := a.stat.Failures
	a.statMu.Unlock()
	switch {
	case errors.Is(err, ErrArchiveDiverged):
	case err == nil && failures > 0:
		ds.svc.log.Info("substrate: the archive is written again", "repository", ds.info.ID, "failedPasses", failures)
	case err != nil && failures == 0:
		ds.svc.log.Error("substrate: an archive pass failed; the server goes on serving, and the archive falls behind until a pass finishes",
			"repository", ds.info.ID, "store", ds.svc.archive.String(), "error", err)
	}
	ds.refreshArchiveStats(ctx, err)
}

// refreshArchiveStats sets the delay from the table: how many entries are
// past the mark, and when the oldest of them was written.
func (ds *dataset) refreshArchiveStats(ctx context.Context, passErr error) {
	a := ds.archive
	a.mu.Lock()
	mark, missing, pending, stopped := a.mark, a.missing, a.pendingBlobs, a.stopped != ""
	a.mu.Unlock()
	var head int64
	var oldest sql.NullTime
	err := ds.db.QueryRowContext(ctx, `
		SELECT coalesce(max(seq), 0), (SELECT ts FROM changelog WHERE seq > $1 ORDER BY seq LIMIT 1)
		FROM changelog`, mark).Scan(&head, &oldest)
	a.statMu.Lock()
	defer a.statMu.Unlock()
	a.err = passErr
	a.stat.Stopped = stopped
	if passErr == nil {
		a.stat.Failures = 0
		a.stat.LastPass = nowUTC()
	} else {
		a.stat.Failures++
	}
	if err != nil {
		return
	}
	a.stat.Pending, a.stat.Missing, a.stat.PendingBlobs = max(head-mark, 0), missing, pending
	a.stat.Oldest = time.Time{}
	if a.stat.Pending > 0 && oldest.Valid {
		a.stat.Oldest = oldest.Time.UTC()
	}
}

// ArchivePass runs one pass now (the header). A pass that fails leaves the
// process to read the record again at its next pass.
func (ds *dataset) ArchivePass(ctx context.Context) (err error) {
	a := ds.archive
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	defer func() {
		if err != nil {
			a.known = false
		}
	}()
	if a.stopped != "" {
		return fmt.Errorf("%w: %s", ErrArchiveDiverged, a.stopped)
	}
	// A repository whose deletion began is the deletion's (rule 7).
	if began, err := ds.svc.deletionBegan(ctx, ds.info.ID); err != nil || began {
		return err
	}
	store := ds.svc.archive

	// The spool is read before the snapshot: a request the drain records
	// after the snapshot's head, and then deletes from the spool, is in this
	// copy, and one it deleted before is in the history the snapshot reads.
	// No compaction commits between the two (spoolCopy).
	snapshotTaken, err := ds.spoolCopy.share(ctx)
	if err != nil {
		return err
	}
	defer snapshotTaken()
	spooled, err := spoolFilesForCopy(ds.dir)
	if err != nil {
		return err
	}
	spoolPrint := spoolPrintOf(spooled)
	if err := ds.svc.archiveFault(archiveAfterSpool); err != nil {
		return err
	}
	// Every blob is held from before the snapshot, as an export's are: a
	// collection the snapshot does not see deletes its bytes only after it,
	// and meets the hold. It narrows to the blobs this pass copies.
	ds.holdArchiveBlobs(nil, true)
	defer ds.holdArchiveBlobs(nil, false)

	tx, err := ds.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	head, err := tableChangelogHead(ctx, tx)
	if err != nil {
		return err
	}
	snapshotTaken()
	dialect, err := readChangelogDialect(ctx, tx)
	if err != nil {
		return err
	}
	// A compaction rewrote the lines from rewriteFrom on (compact.go): the
	// pieces that hold them are written again from the snapshot, and the
	// mark is cleared once a publication holds them, unless another
	// compaction came after the snapshot.
	var rewriteFrom sql.NullInt64
	var rewrites int64
	if err := tx.QueryRowContext(ctx, `SELECT rewrite_from, rewrites FROM archive_records`).Scan(&rewriteFrom, &rewrites); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// The sealed table whole, every column of every row, hashed in the
	// database: a change to any of them, committed in any order, is a change
	// here, where a count or the latest updated_at is not.
	var print string
	if err := tx.QueryRowContext(ctx, sealedDigest).Scan(&print); err != nil {
		return err
	}
	repo, err := ds.svc.repositoryByID(ctx, ds.info.ID)
	if err != nil {
		return err
	}
	m, err := ds.svc.manifestOf(ctx, repo, tx)
	if err != nil {
		return err
	}
	// The people are read outside the snapshot: their rows are no part of
	// the history, and a change of them moves no line, so it is the print
	// that says this pass has something to publish.
	if m.Identities, err = ds.svc.manifestIdentities(ctx, ds.info.ID, ""); err != nil {
		return err
	}
	people, err := identitiesPrint(m.Identities)
	if err != nil {
		return err
	}
	manifestPrint := fmt.Sprintf("%s|%s|%s|%x|%d|%d|%s|%s", m.ID, m.Authority, m.DEKKeyID, m.DEK, m.ChangelogDialect, m.VocabularyDialect, m.CreatedAt.Format(time.RFC3339Nano), people)
	if !a.known {
		if err := ds.loadArchiveRecord(ctx); err != nil {
			return err
		}
	}

	// 1. The store against the record (rule 4), at every pass, an idle one
	// included: a manifest that lands late beside the one the record holds
	// stops the passes at the next tick, whether or not anything changed.
	listed, err := listArchives(ctx, store, ds.info.ID)
	if err != nil {
		return err
	}
	landed, err := ds.holdArchiveToRecord(ctx, listed)
	if err != nil {
		return err
	}
	if err := ds.svc.archiveFault(archiveAfterListing); err != nil {
		return err
	}
	ds.reportUnrecordedManifests(listed)
	if a.rec.pending == nil && head == a.mark && dialect == a.dialect && print == a.sealedPrint &&
		spoolPrint == a.spoolPrint && manifestPrint == a.manifestPrint && a.missing == 0 && a.pendingBlobs == 0 && !a.anyDropped() &&
		!rewriteFrom.Valid {
		return nil
	}

	// 2. The publication the record says was about to be made, finished
	// exactly (rule 3).
	if a.rec.pending != nil {
		if err := ds.finishPending(ctx, landed); err != nil {
			return err
		}
	}

	// 3. What the manifest it started from does not select, from one
	// snapshot (rules 1 and 2).
	pass, err := changelogfile.NewPassID()
	if err != nil {
		return err
	}
	b := &archiveBuild{ds: ds, store: store, pass: pass, selected: map[string]bool{}}
	pacer := ds.svc.newArchivePacer(ds.info.ID)
	stored, err := storedBlobs(ctx, tx)
	if err != nil {
		return err
	}
	_, uploading, err := snapshotBlobs(ctx, tx)
	if err != nil {
		return err
	}
	if err := b.blobs(ctx, stored, uploading); err != nil {
		return err
	}
	for _, f := range spooled {
		name := changelogfile.SpoolObjectName(f.name, pass)
		if kept := b.reuse("spool:" + f.name); kept != "" {
			name = kept
		} else if err := b.put(ctx, name, "spool:"+f.name, f.data); err != nil {
			return err
		}
		b.selected[name] = true
		b.spool = append(b.spool, name)
	}
	if err := ds.svc.archiveFault(archiveAfterBlobs); err != nil {
		return err
	}
	sealed, err := readSealedTable(ctx, tx)
	if err != nil {
		return err
	}
	b.sealed = make(map[string]string, len(sealed))
	for _, ref := range sortedKeys(sealed) {
		data, sum, err := changelogfile.EncodeSealedVersion(sealed[ref])
		if err != nil {
			return err
		}
		holds := "sealed:" + ref + "@" + hex.EncodeToString(sum[:])
		name := changelogfile.SealedObjectName(ref, sum, pass)
		if kept := b.reuse(holds); kept != "" {
			name = kept
		} else if err := b.put(ctx, name, holds, data); err != nil {
			return err
		}
		b.selected[name] = true
		b.sealed[ref] = name
	}
	if err := ds.svc.archiveFault(archiveAfterSealed); err != nil {
		return err
	}
	// The pieces from the mark to the head: the ones the manifest it started
	// from selects, but a small last one, which is written again with the
	// new lines, and every one when a conversion moved the dialect.
	var pieces []changelogfile.ArchivePiece
	from := int64(0)
	if a.start.Epoch > 0 && a.start.ChangelogDialect == dialect && a.start.Head <= head {
		pieces, from = slices.Clone(a.start.Pieces), a.start.Head
		if n := len(pieces); n > 0 && pieces[n-1].Size < archiveTailBytes && head > from {
			_, first, _, _, _ := changelogfile.ParsePieceObjectName(pieces[n-1].Name)
			pieces, from = pieces[:n-1], first-1
		}
		// Only the pieces wholly below what a compaction rewrote still hold
		// the lines the table holds.
		for rewriteFrom.Valid && len(pieces) > 0 {
			_, first, last, _, _ := changelogfile.ParsePieceObjectName(pieces[len(pieces)-1].Name)
			if last < rewriteFrom.Int64 {
				break
			}
			pieces, from = pieces[:len(pieces)-1], first-1
		}
	}
	for _, p := range pieces {
		b.selected[p.Name] = true
	}
	headSum := ""
	if head > from {
		written, sum, err := b.writePieces(ctx, tx, dialect, from, head, pacer.pace)
		if err != nil {
			return err
		}
		pieces, headSum = append(pieces, written...), sum
	} else if head > 0 {
		if headSum, err = tableLineSum(ctx, tx, head); err != nil {
			return err
		}
	}

	// 4. The manifest: recorded as about to be published, put, and the
	// publication recorded with what it drops (rules 2 and 3).
	m.Archive, m.Pass, m.Head, m.HeadSum, m.Pieces = a.rec.archive, pass, head, headSum, pieces
	m.Sealed, m.Spool, m.MissingBlobs, m.PendingBlobs = b.sealed, b.spool, b.missing, b.pending
	fingerprint := func() {
		a.mark, a.dialect, a.sealedPrint, a.spoolPrint, a.manifestPrint = head, dialect, print, spoolPrint, manifestPrint
		a.missing, a.pendingBlobs = len(b.missing), len(b.pending)
	}
	if a.start.Epoch > 0 && sameArchiveContent(m, a.start) && !b.wrote {
		// Nothing moved that the published manifest does not already say.
		fingerprint()
		return ds.deleteDropped(ctx)
	}
	m.Epoch = max(a.rec.epoch, listed[a.rec.archive].highest()) + 1
	data, err := changelogfile.EncodeArchiveManifest(m)
	if err != nil {
		return err
	}
	if err := ds.recordPending(ctx, data, m.Epoch, b.selected); err != nil {
		return err
	}
	if err := ds.svc.archiveFault(archiveAfterPending); err != nil {
		return err
	}
	if err := ds.finishPending(ctx, false); err != nil {
		return err
	}
	if rewriteFrom.Valid {
		if _, err := ds.db.ExecContext(ctx, `UPDATE archive_records SET rewrite_from = NULL WHERE rewrites = $1`, rewrites); err != nil {
			return err
		}
	}
	fingerprint()
	if len(b.missing) > 0 {
		ds.svc.log.Warn("substrate: blobs the records hold are not on this server, so the archive does not hold them; it names them in its manifest and every pass tries them again",
			"repository", ds.info.ID, "missing", len(b.missing), "first", b.missing[0])
	}

	// 5. What the publication dropped: objects, and the manifest before.
	return ds.deleteDropped(ctx)
}

// reportUnrecordedManifests says, once a process, which manifests of the
// archive the database writes it has no row for: ones a database restored
// from a backup forgot it put. They are below its epoch, or rule 4 would
// have stopped; nothing reads them, and nothing deletes them (rule 5).
func (ds *dataset) reportUnrecordedManifests(listed archiveListing) {
	a := ds.archive
	if a.reported {
		return
	}
	var unrecorded []string
	for _, names := range listed[a.rec.archive].byEpochOrNil() {
		for _, name := range names {
			if _, ok := a.objects[name]; !ok && name != a.rec.published {
				unrecorded = append(unrecorded, name)
			}
		}
	}
	if len(unrecorded) > 0 {
		sort.Strings(unrecorded)
		ds.svc.log.Warn("substrate: the archive holds manifests of a lower epoch that this database has no record of putting; nothing reads them and the passes leave them, and `substratectl archive report` lists them",
			"repository", ds.info.ID, "archive", a.rec.archive, "manifests", len(unrecorded), "first", unrecorded[0])
	}
	a.reported = true
}

// sameArchiveContent reports whether m selects what start does, in the same
// repository fields: a manifest that would say nothing new.
func sameArchiveContent(m, start changelogfile.Manifest) bool {
	m.Epoch, m.Pass = start.Epoch, start.Pass
	x, err1 := json.Marshal(m)
	y, err2 := json.Marshal(start)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

// archiveBuild is what one pass puts and selects.
type archiveBuild struct {
	ds       *dataset
	store    archive.Store
	pass     string
	selected map[string]bool
	sealed   map[string]string
	spool    []string
	// missing and pending are the blobs the records hold that the manifest
	// does not: bytes this server lost, and bytes past the pass's step.
	missing, pending []string
	// wrote is whether the pass put anything.
	wrote bool
}

// reuse is the object the pass may select for holds without putting it
// (rule 1): one the published manifest selects, or one this process put and
// no manifest selected, never one an earlier process left.
func (b *archiveBuild) reuse(holds string) string {
	a := b.ds.archive
	var mine string
	for _, name := range sortedKeys(a.objects) {
		o := a.objects[name]
		if o.holds != holds || o.state != objectPut {
			continue
		}
		if o.published {
			return name
		}
		if o.process == b.ds.svc.archiveProcess && mine == "" {
			mine = name
		}
	}
	return mine
}

// put puts one object under name, its row written first.
func (b *archiveBuild) put(ctx context.Context, name, holds string, data []byte) error {
	return b.putWith(ctx, name, holds, func(key string) error {
		return b.store.Put(ctx, key, bytes.NewReader(data), int64(len(data)))
	})
}

// putWith runs put for name's key between its row's two states: putting
// before the request is sent, put once it succeeded.
func (b *archiveBuild) putWith(ctx context.Context, name, holds string, put func(key string) error) error {
	ds, a := b.ds, b.ds.archive
	o := &archiveObject{name: name, holds: holds, process: ds.svc.archiveProcess, state: objectPutting}
	if _, err := ds.db.ExecContext(ctx, `INSERT INTO archive_objects (archive, name, holds, process, state) VALUES ($1, $2, $3, $4, $5)`,
		a.rec.archive, name, holds, o.process, objectPutting); err != nil {
		return err
	}
	a.objects[name] = o
	b.wrote = true
	if err := ds.svc.archiveFault(archiveAfterObjectRow); err != nil {
		return err
	}
	if err := put(changelogfile.ArchivePrefix(ds.info.ID, a.rec.archive) + name); err != nil {
		return err
	}
	if err := ds.svc.archiveFault(archiveAfterObjectPut); err != nil {
		return err
	}
	if _, err := ds.db.ExecContext(ctx, `UPDATE archive_objects SET state = $3 WHERE archive = $1 AND name = $2`,
		a.rec.archive, name, objectPut); err != nil {
		return err
	}
	o.state = objectPut
	return nil
}

// blobs selects an object for every blob the snapshot's records hold: one
// the record lets it reuse, or one it puts, a step's bytes at most. A blob
// still uploading keeps the object the published manifest selects, so its
// bytes are not dropped while its record is in flight.
func (b *archiveBuild) blobs(ctx context.Context, stored []storedBlob, uploading map[string]bool) error {
	ds := b.ds
	var local blobbytes.Store
	budget := ds.svc.archiveStepBytes
	copying := map[string]bool{}
	for _, blob := range stored {
		copying[blob.digest] = true
	}
	ds.holdArchiveBlobs(copying, false)
	for digest := range uploading {
		if kept := b.reuse("blob:" + digest); kept != "" {
			b.selected[kept] = true
		}
	}
	for _, blob := range stored {
		holds := "blob:" + blob.digest
		if kept := b.reuse(holds); kept != "" {
			b.selected[kept] = true
			continue
		}
		if budget <= 0 {
			b.pending = append(b.pending, blob.digest)
			continue
		}
		if local == nil {
			var err error
			if local, err = ds.blobBytes(); err != nil {
				return err
			}
		}
		name := changelogfile.BlobObjectName(blob.digest, b.pass)
		var size int64
		err := b.putWith(ctx, name, holds, func(key string) (err error) {
			size, err = putBlob(ctx, b.store, key, local, blob.digest)
			return err
		})
		// Bytes this server no longer has cannot be archived. The hold keeps
		// the collector off them, so they are lost, which verify reports
		// too. Refusing the pass would recover nothing and leave every later
		// write unarchived, so the manifest names them, and every pass tries
		// them again. Nothing was sent, so the row goes.
		if errors.Is(err, blobbytes.ErrNotStored) {
			if _, err := ds.db.ExecContext(ctx, `DELETE FROM archive_objects WHERE archive = $1 AND name = $2`, ds.archive.rec.archive, name); err != nil {
				return err
			}
			delete(ds.archive.objects, name)
			b.missing = append(b.missing, blob.digest)
			continue
		}
		if err != nil {
			return err
		}
		b.selected[name] = true
		budget -= max(size, 1)
	}
	sort.Strings(b.missing)
	sort.Strings(b.pending)
	return nil
}

// writePieces writes the table's lines after `after` up to head as pieces
// in dialect, each cut at the first transaction boundary past the piece
// size, and returns them in order with the checksum of the line at head. A
// row whose stamped checksum is not the checksum of what it holds is
// refused, as the boot check refuses it: it is not history.
func (b *archiveBuild) writePieces(ctx context.Context, q dbx, dialect int, after, head int64, pace func(context.Context, int) error) ([]changelogfile.ArchivePiece, string, error) {
	return writePieces(ctx, q, after, head, b.ds.svc.archivePieceBytes, pace, func(first, last int64, data []byte) (changelogfile.ArchivePiece, error) {
		sum := sha256.Sum256(data)
		name := changelogfile.PieceObjectName(dialect, first, last, b.pass, sum)
		if err := b.put(ctx, name, fmt.Sprintf("piece:%d:%d-%d", dialect, first, last), data); err != nil {
			return changelogfile.ArchivePiece{}, err
		}
		b.selected[name] = true
		if err := b.ds.svc.archiveFault(archiveAfterPiece); err != nil {
			return changelogfile.ArchivePiece{}, err
		}
		return changelogfile.ArchivePiece{Name: name, Sum: changelogfile.SumText(sum), Size: int64(len(data))}, nil
	})
}

// writePieces reads the table's lines after `after` up to head, cuts them
// into pieces at the first transaction boundary past pieceBytes, and hands
// each to put.
func writePieces(ctx context.Context, q dbx, after, head, pieceBytes int64, pace func(context.Context, int) error,
	put func(first, last int64, data []byte) (changelogfile.ArchivePiece, error),
) ([]changelogfile.ArchivePiece, string, error) {
	var (
		lines   []changelogfile.Line
		size    int64
		written []changelogfile.ArchivePiece
		sum     [32]byte
	)
	// A piece covers the seqs from the one after the piece before it, so the
	// pieces cover the history with no hole, a compacted one's gaps
	// included.
	first := after + 1
	flush := func() error {
		data, err := changelogfile.EncodePiece(lines)
		if err != nil {
			return err
		}
		last := lines[len(lines)-1].Seq
		p, err := put(first, last, data)
		if err != nil {
			return err
		}
		written = append(written, p)
		lines, size, first = nil, 0, last+1
		return nil
	}
	for after < head {
		page, err := scanChecksumPage(ctx, q, after, archivePageRows)
		if err != nil {
			return written, "", err
		}
		if len(page) == 0 {
			return written, "", fmt.Errorf("%w: the table ends at seq %d, below its head %d", ErrChangelogDiverged, after, head)
		}
		for _, row := range page {
			if row.entry.Seq > head {
				break
			}
			e := row.entry.fileEntry()
			line, s, err := changelogfile.Encode(e)
			if err != nil {
				return written, "", fmt.Errorf("substrate/engine: seq %d does not encode as a changelog line: %w", e.Seq, err)
			}
			if !bytes.Equal(row.hash, s[:]) {
				return written, "", fmt.Errorf("%w: seq %d: the table's checksum is not the checksum of the row it holds", ErrChangelogDiverged, e.Seq)
			}
			lines = append(lines, changelogfile.Line{Seq: e.Seq, Txn: e.Txn, Bytes: line})
			size += int64(len(line)) + 1
			if pace != nil {
				if err := pace(ctx, len(line)+1); err != nil {
					return written, "", err
				}
			}
			sum, after = s, e.Seq
			if size >= pieceBytes && e.EndsTransaction() {
				if err := flush(); err != nil {
					return written, "", err
				}
			}
		}
	}
	if len(lines) > 0 {
		if err := flush(); err != nil {
			return written, "", err
		}
	}
	return written, changelogfile.SumText(sum), nil
}

// loadArchiveRecord reads the record into the process: the archive it
// writes, creating the record when there is none, and its objects.
func (ds *dataset) loadArchiveRecord(ctx context.Context) error {
	a := ds.archive
	rec, ok, err := readArchiveRecord(ctx, ds.db)
	if err != nil {
		return err
	}
	if !ok {
		// Nothing recorded: a repository created, or one a release before the
		// archive kept. Its archive starts here, under a new id, which rule 4
		// holds to the store before anything is written.
		id, err := changelogfile.NewArchiveID(nowUTC())
		if err != nil {
			return err
		}
		if _, err := ds.db.ExecContext(ctx, `INSERT INTO archive_records (archive) VALUES ($1) ON CONFLICT DO NOTHING`, id); err != nil {
			return err
		}
		if rec, _, err = readArchiveRecord(ctx, ds.db); err != nil {
			return err
		}
	}
	start := changelogfile.Manifest{}
	if rec.publishedBytes != nil {
		if start, err = changelogfile.DecodeArchiveManifest(rec.publishedBytes, ds.info.ID); err != nil {
			return fmt.Errorf("substrate/engine: the manifest the record says was published does not read: %w", err)
		}
	}
	objects, err := readArchiveObjects(ctx, ds.db, rec.archive)
	if err != nil {
		return err
	}
	a.rec, a.start, a.objects, a.known = rec, start, objects, true
	// The mark is the published manifest's. What it was built from is
	// unknown to a new process, so its first pass builds one; one that
	// selects nothing new is not put.
	a.mark, a.dialect, a.manifestPrint = start.Head, start.ChangelogDialect, ""
	a.missing, a.pendingBlobs = len(start.MissingBlobs), len(start.PendingBlobs)
	return nil
}

func readArchiveRecord(ctx context.Context, q dbx) (archiveRecord, bool, error) {
	var rec archiveRecord
	var published, stopped, source sql.NullString
	err := q.QueryRowContext(ctx, `SELECT archive, epoch, published, published_bytes, pending, stopped, source FROM archive_records`).
		Scan(&rec.archive, &rec.epoch, &published, &rec.publishedBytes, &rec.pending, &stopped, &source)
	if errors.Is(err, sql.ErrNoRows) {
		return rec, false, nil
	}
	if err != nil {
		return rec, false, err
	}
	rec.published, rec.stopped, rec.source = published.String, stopped.String, source.String
	return rec, true, nil
}

func readArchiveObjects(ctx context.Context, q dbx, archive string) (map[string]*archiveObject, error) {
	rows, err := q.QueryContext(ctx, `SELECT name, holds, process, state, in_published, in_pending FROM archive_objects WHERE archive = $1`, archive)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]*archiveObject{}
	for rows.Next() {
		o := &archiveObject{}
		if err := rows.Scan(&o.name, &o.holds, &o.process, &o.state, &o.published, &o.pending); err != nil {
			return nil, err
		}
		out[o.name] = o
	}
	return out, rows.Err()
}

// anyDropped reports whether the record holds objects to delete.
func (a *archiveState) anyDropped() bool {
	for _, o := range a.objects {
		if o.state == objectDropped || o.state == objectDeleting {
			return true
		}
	}
	return false
}

// listedArchive is what a listing shows under one id: an archive's
// manifests by epoch, or a deletion mark (rule 7), which takes an id of its
// own past every archive's and covers every archive older than it.
type listedArchive struct {
	deleted bool
	byEpoch map[int64][]string
	top     int64
}

// tops is the names of the manifests of the highest epoch.
func (l *listedArchive) tops() []string {
	if l == nil {
		return nil
	}
	return l.byEpoch[l.top]
}

// highest is the archive's highest epoch listed, 0 when it is not listed.
func (l *listedArchive) highest() int64 {
	if l == nil {
		return 0
	}
	return l.top
}

// byEpochOrNil is the archive's manifests by epoch, nil when it is not
// listed.
func (l *listedArchive) byEpochOrNil() map[int64][]string {
	if l == nil {
		return nil
	}
	return l.byEpoch
}

// archiveListing is every archive a repository's manifests prefix shows.
type archiveListing map[string]*listedArchive

// newest is the id of the newest archive listed with a manifest, empty when
// none is.
func (l archiveListing) newest() string {
	newest := ""
	for id, a := range l {
		if len(a.byEpoch) > 0 && id > newest {
			newest = id
		}
	}
	return newest
}

// newestMark is the id of the newest deletion mark listed, empty when none
// is: every archive whose id sorts below it is covered.
func (l archiveListing) newestMark() string {
	newest := ""
	for id, a := range l {
		if a.deleted && id > newest {
			newest = id
		}
	}
	return newest
}

// ids is every id the listing shows, archives and marks.
func (l archiveListing) ids() []string {
	return sortedKeys(l)
}

// listArchives lists the manifests and the deletion marks of every archive
// of a repository.
func listArchives(ctx context.Context, store archive.Store, repository string) (archiveListing, error) {
	listed, err := store.List(ctx, changelogfile.ManifestsPrefix(repository))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errArchiveUnreadable, err)
	}
	out := archiveListing{}
	for _, o := range listed {
		k, ok := changelogfile.ParseArchiveKey(o.Key)
		if !ok || !k.Manifests || k.Repository != repository {
			continue
		}
		l := out[k.Archive]
		if l == nil {
			l = &listedArchive{byEpoch: map[int64][]string{}}
			out[k.Archive] = l
		}
		if k.Name == changelogfile.DeletedMark {
			l.deleted = true
			continue
		}
		if epoch, _, ok := changelogfile.ParseArchiveManifestName(k.Name); ok {
			l.byEpoch[epoch] = append(l.byEpoch[epoch], k.Name)
			l.top = max(l.top, epoch)
		}
	}
	for _, l := range out {
		for _, names := range l.byEpoch {
			sort.Strings(names)
		}
	}
	return out, nil
}

// holdArchiveToRecord is rule 4: the pass goes on only in the archive the
// record names, as the database left it. It reports whether the manifest the
// record says is about to be published is already the newest. A store
// emptied or replaced starts a new archive, and said loudly; anything else
// stops the passes.
func (ds *dataset) holdArchiveToRecord(ctx context.Context, listed archiveListing) (landed bool, err error) {
	a := ds.archive
	rec := a.rec
	pendingName := ""
	if rec.pending != nil {
		var m changelogfile.Manifest
		if m, err = changelogfile.DecodeArchiveManifest(rec.pending, ds.info.ID); err != nil {
			return false, fmt.Errorf("substrate/engine: the manifest the record says is about to be published does not read: %w", err)
		}
		pendingName = changelogfile.ArchiveManifestName(m.Epoch, rec.pending)
	}
	if mark := listed.newestMark(); mark >= rec.archive {
		return false, ds.stopArchive(ctx, fmt.Sprintf("the repository's deletion mark %s covers archive %s this database writes", mark, rec.archive))
	}
	if newest := listed.newest(); newest > rec.archive {
		return false, ds.stopArchive(ctx, fmt.Sprintf("the store holds archive %s, newer than archive %s this database writes", newest, rec.archive))
	}
	mine := listed[rec.archive]
	tops := mine.tops()
	switch {
	case len(tops) > 1:
		return false, ds.stopArchive(ctx, fmt.Sprintf("archive %s holds %d manifests of epoch %d", rec.archive, len(tops), mine.top))
	case len(tops) == 1 && tops[0] == rec.published:
		return false, nil
	case len(tops) == 1 && tops[0] == pendingName:
		return true, nil
	case len(tops) == 1:
		return false, ds.stopArchive(ctx, fmt.Sprintf("archive %s's newest manifest is %s, which this database did not publish (it published %q)", rec.archive, tops[0], rec.published))
	case rec.published == "":
		// Nothing published here yet, and no manifest of this archive or of
		// a newer one listed: a new archive, whose recorded first manifest,
		// if there is one, is put again, and otherwise a first pass with
		// names of its own.
		return false, nil
	case len(listed) > 0:
		return false, ds.stopArchive(ctx, fmt.Sprintf("the store holds no manifest of archive %s, which this database published (%s)", rec.archive, rec.published))
	}
	// Nothing listed at all: the store was emptied or replaced, if it holds
	// nothing else of the repository either.
	everything, err := ds.svc.archive.List(ctx, changelogfile.RepositoryPrefix(ds.info.ID))
	if err != nil {
		return false, fmt.Errorf("%w: %w", errArchiveUnreadable, err)
	}
	if len(everything) > 0 {
		return false, ds.stopArchive(ctx, fmt.Sprintf("the store holds %d object(s) of the repository and no manifest, where this database published %s", len(everything), rec.published))
	}
	id, err := changelogfile.NewArchiveID(nowUTC(), rec.archive)
	if err != nil {
		return false, err
	}
	ds.svc.log.Error("substrate: the store holds nothing of the repository's archive, which this database published: it was emptied or replaced. A new archive starts now, and the copy the store held is gone",
		"repository", ds.info.ID, "store", ds.svc.archive.String(), "archive", rec.archive, "published", rec.published, "newArchive", id)
	return false, ds.startNewArchive(ctx, id)
}

// startNewArchive points the record at a new archive, forgetting the old
// one's objects: the store holds none of them.
func (ds *dataset) startNewArchive(ctx context.Context, id string) error {
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM archive_objects`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE archive_records SET archive = $1, epoch = 0, published = NULL, published_bytes = NULL, pending = NULL, stopped = NULL, updated_at = now()`, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	a := ds.archive
	a.rec = archiveRecord{archive: id}
	a.start, a.objects = changelogfile.Manifest{}, map[string]*archiveObject{}
	return nil
}

// stopArchive stops the passes for the rest of the process (rule 4) and
// records why, where `inspect` reads it.
func (ds *dataset) stopArchive(ctx context.Context, why string) error {
	a := ds.archive
	a.stopped = why
	if _, err := ds.db.ExecContext(ctx, `UPDATE archive_records SET stopped = $1, updated_at = now()`, why); err != nil {
		return err
	}
	ds.svc.log.Error("substrate: the archive is not as this database left it, so its passes write nothing and delete nothing; the server goes on serving. Run `substratectl archive report` and resolve it",
		"repository", ds.info.ID, "store", ds.svc.archive.String(), "why", why)
	return fmt.Errorf("%w: %s", ErrArchiveDiverged, why)
}

// recordPending records data as the manifest about to be published, with
// the objects it selects (rule 3).
func (ds *dataset) recordPending(ctx context.Context, data []byte, epoch int64, selected map[string]bool) error {
	a := ds.archive
	names := sortedKeys(selected)
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE archive_records SET pending = $1, epoch = $2, updated_at = now()`, data, epoch); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE archive_objects SET in_pending = (name = ANY($2)) WHERE archive = $1 AND state = $3`,
		a.rec.archive, names, objectPut); err != nil {
		return err
	}
	// The manifest's own row, before it is sent, like any object's: the
	// publication after it drops it, and only then is it deleted.
	manifest := &archiveObject{name: changelogfile.ArchiveManifestName(epoch, data), holds: archiveManifestHolds, process: ds.svc.archiveProcess, state: objectPutting, pending: true}
	if _, err := tx.ExecContext(ctx, `INSERT INTO archive_objects (archive, name, holds, process, state, in_pending) VALUES ($1, $2, $3, $4, $5, true)`,
		a.rec.archive, manifest.name, manifest.holds, manifest.process, objectPutting); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	a.rec.pending, a.rec.epoch = data, epoch
	for name, o := range a.objects {
		if o.state == objectPut {
			o.pending = selected[name]
		}
	}
	a.objects[manifest.name] = manifest
	return nil
}

// finishPending puts the manifest the record says is about to be published,
// unless the store already shows it, and records the publication: the
// manifest it now is, and every object it does not select dropped, in one
// transaction (rule 3).
func (ds *dataset) finishPending(ctx context.Context, landed bool) error {
	a := ds.archive
	data := a.rec.pending
	m, err := changelogfile.DecodeArchiveManifest(data, ds.info.ID)
	if err != nil {
		return err
	}
	name := changelogfile.ArchiveManifestName(m.Epoch, data)
	if !landed {
		if err := ds.svc.archive.Put(ctx, changelogfile.ArchiveManifestsPrefix(ds.info.ID, a.rec.archive)+name, bytes.NewReader(data), int64(len(data))); err != nil {
			return err
		}
	}
	if err := ds.svc.archiveFault(archiveAfterManifest); err != nil {
		return err
	}
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE archive_records SET published = $1, published_bytes = $2, pending = NULL, stopped = NULL, updated_at = now()`, name, data); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE archive_objects SET state = $3 WHERE archive = $1 AND name = $2`, a.rec.archive, name, objectPut); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE archive_objects SET in_published = in_pending, in_pending = false WHERE archive = $1 AND state = $2`, a.rec.archive, objectPut); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE archive_objects SET state = $2 WHERE archive = $1 AND state IN ($3, $4) AND NOT in_published`,
		a.rec.archive, objectDropped, objectPutting, objectPut); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	a.rec.published, a.rec.publishedBytes, a.rec.pending, a.start = name, data, nil, m
	if o := a.objects[name]; o != nil {
		o.state = objectPut
	}
	for _, o := range a.objects {
		if o.state == objectPut {
			o.published, o.pending = o.pending, false
		}
		if (o.state == objectPutting || o.state == objectPut) && !o.published {
			o.state = objectDropped
		}
	}
	if err := ds.svc.archiveFault(archiveAfterPublished); err != nil {
		return err
	}
	// The mark where `repository list` reads it.
	if _, err := ds.svc.maint.ExecContext(ctx,
		`UPDATE repositories SET archived_seq = $1, archived_at = $2, archive_store = $3, archive_missing = $4 WHERE id = $5`,
		m.Head, nowUTC(), ds.svc.archive.String(), len(m.MissingBlobs)+len(m.PendingBlobs), ds.info.ID); err != nil {
		return err
	}
	return ds.svc.archiveFault(archiveAfterMark)
}

// deleteDropped deletes every object the record says a publication dropped,
// each after its row says the delete is about to be sent: what the manifest
// before selected and the published one does not, the manifest before
// itself, and what an earlier process put and no manifest selected (rule 5).
// Nothing the record does not hold is deleted.
func (ds *dataset) deleteDropped(ctx context.Context) error {
	a := ds.archive
	for _, name := range sortedKeys(a.objects) {
		o := a.objects[name]
		if o.state != objectDropped && o.state != objectDeleting {
			continue
		}
		if o.state == objectDropped {
			if _, err := ds.db.ExecContext(ctx, `UPDATE archive_objects SET state = $3 WHERE archive = $1 AND name = $2`, a.rec.archive, name, objectDeleting); err != nil {
				return err
			}
			o.state = objectDeleting
		}
		if err := ds.svc.archiveFault(archiveAfterDeleteRow); err != nil {
			return err
		}
		if err := ds.svc.archive.Delete(ctx, archiveObjectKey(ds.info.ID, a.rec.archive, o)); err != nil {
			return err
		}
		if err := ds.svc.archiveFault(archiveAfterDeleteHit); err != nil {
			return err
		}
		if _, err := ds.db.ExecContext(ctx, `DELETE FROM archive_objects WHERE archive = $1 AND name = $2`, a.rec.archive, name); err != nil {
			return err
		}
		delete(a.objects, name)
		if err := ds.svc.archiveFault(archiveAfterDelete); err != nil {
			return err
		}
	}
	return nil
}

// archiveManifestHolds is what a manifest's row says it holds.
const archiveManifestHolds = "manifest"

// archiveObjectKey is the key of an object a row names: a manifest beside
// its archive's manifests, anything else under the archive.
func archiveObjectKey(repository, archiveID string, o *archiveObject) string {
	if o.holds == archiveManifestHolds {
		return changelogfile.ArchiveManifestsPrefix(repository, archiveID) + o.name
	}
	return changelogfile.ArchivePrefix(repository, archiveID) + o.name
}

// sealedDigest fingerprints the repository's sealed table: every column of
// every row, a row's ciphertext after its length, in ref order, hashed where
// it sits.
const sealedDigest = `
	SELECT count(*)::text || ':' || coalesce(encode(sha256(string_agg(
		convert_to(ref || E'\x1f' || record_kind || E'\x1f' || record_id || E'\x1f' ||
			coalesce(expires_at::text, '') || E'\x1f' || updated_at::text || E'\x1f', 'UTF8') ||
		int8send(length(payload)::bigint) || payload, ''::bytea ORDER BY ref)), 'hex'), '')
	FROM sealed`

// deletionBegan reports whether the repository's deletion began (rule 7).
func (s *service) deletionBegan(ctx context.Context, id string) (bool, error) {
	var began sql.NullTime
	if err := s.maint.QueryRowContext(ctx, `SELECT deletion_began_at FROM repositories WHERE id = $1`, id).Scan(&began); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil
		}
		return false, err
	}
	return began.Valid, nil
}

// eraseArchive is a repository's deletion's part in the store (rule 7): a
// deletion mark first, under an id of its own past every archive and mark
// the store lists and the archive the record names, so it covers every
// archive made before it and no boot restores one of them from then on,
// whatever the store applies later; then every other object of the
// repository, of every archive, the record's or not: the one path that
// deletes what the record does not hold. The marks stay: a repository's id is
// never minted again. A server with the archive off still erases the copy
// under the data root's default archive, which a later boot with the archive
// on would restore from; an archive elsewhere that it no longer writes is out
// of its reach.
func (s *service) eraseArchive(ctx context.Context, id string) error {
	store := s.archive
	if store == nil {
		dir := filepath.Join(s.dataRoot, "archive")
		if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		d, err := archive.NewDir(dir)
		if err != nil {
			return err
		}
		store = d
	}
	// Every archive the store shows anything of, with a manifest or not
	// yet, and the one the record names: the mark is past them all.
	everything, err := store.List(ctx, changelogfile.RepositoryPrefix(id))
	if err != nil {
		return err
	}
	var after []string
	for _, o := range everything {
		if k, ok := changelogfile.ParseArchiveKey(o.Key); ok {
			after = append(after, k.Archive)
		}
	}
	var recorded string
	if err := s.maint.QueryRowContext(ctx, `SELECT archive FROM archive_records WHERE repository = $1`, id).Scan(&recorded); err == nil {
		after = append(after, recorded)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	mark, err := changelogfile.NewArchiveID(nowUTC(), after...)
	if err != nil {
		return err
	}
	if err := store.Put(ctx, changelogfile.ArchiveManifestsPrefix(id, mark)+changelogfile.DeletedMark, strings.NewReader(""), 0); err != nil {
		return err
	}
	if err := s.eraseStage(eraseAfterMark); err != nil {
		return err
	}
	for _, o := range everything {
		if k, ok := changelogfile.ParseArchiveKey(o.Key); ok && k.Manifests && k.Name == changelogfile.DeletedMark {
			continue
		}
		if err := store.Delete(ctx, o.Key); err != nil {
			return err
		}
		if err := s.eraseStage(eraseAfterArchiveObject); err != nil {
			return err
		}
	}
	return nil
}

// spoolPrintOf is the spool's fingerprint: what a pass that copied it would
// copy.
func spoolPrintOf(files []spoolCopy) string {
	h := sha256.New()
	for _, f := range files {
		sum := sha256.Sum256(f.data)
		_, _ = io.WriteString(h, f.name)
		_, _ = h.Write(sum[:])
	}
	return string(h.Sum(nil))
}

// tableLineSum is the stamped checksum of the table's line at seq.
func tableLineSum(ctx context.Context, q dbx, seq int64) (string, error) {
	var hash []byte
	if err := q.QueryRowContext(ctx, `SELECT hash FROM changelog WHERE seq = $1`, seq).Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("%w: the table holds no seq %d", ErrChangelogDiverged, seq)
		}
		return "", err
	}
	if len(hash) != sha256.Size {
		return "", fmt.Errorf("%w: seq %d carries no checksum", ErrChangelogDiverged, seq)
	}
	var sum [32]byte
	copy(sum[:], hash)
	return changelogfile.SumText(sum), nil
}

// snapshotBlobs reads the blob manifests the snapshot holds: those whose
// bytes are stored, which the archive must hold, and those still uploading,
// whose bytes it must not drop.
func snapshotBlobs(ctx context.Context, q dbx) (stored, pending map[string]bool, err error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, coalesce(states->>'`+blobStateStatus+`', '') FROM records WHERE kind = $1 AND deleted_at IS NULL ORDER BY id`, kindBlob)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	stored, pending = map[string]bool{}, map[string]bool{}
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			return nil, nil, err
		}
		if status == string(substrate.BlobStored) {
			stored[id] = true
		} else {
			pending[id] = true
		}
	}
	return stored, pending, rows.Err()
}

// putBlob puts one blob's bytes from the server's own store, by a link where
// the archive can take the file and by a copy otherwise, and reports how
// many bytes it put.
func putBlob(ctx context.Context, store archive.Store, key string, local blobbytes.Store, digest string) (int64, error) {
	if l, ok := store.(archive.Linker); ok {
		if p, ok := local.(interface{ Path(string) (string, error) }); ok {
			path, err := p.Path(digest)
			if err != nil {
				return 0, err
			}
			info, serr := os.Stat(path)
			if errors.Is(serr, fs.ErrNotExist) {
				return 0, fmt.Errorf("%w: %s", blobbytes.ErrNotStored, digest)
			}
			if err := l.Link(ctx, key, path); err != nil {
				if _, serr := os.Stat(path); errors.Is(serr, fs.ErrNotExist) {
					return 0, fmt.Errorf("%w: %s", blobbytes.ErrNotStored, digest)
				}
				return 0, err
			}
			if serr != nil {
				return 0, nil
			}
			return info.Size(), nil
		}
	}
	rc, err := local.Open(ctx, digest)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rc.Close() }()
	body, ok := rc.(io.ReadSeeker)
	if !ok {
		data, err := io.ReadAll(rc)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(data)
	}
	size, err := body.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	return size, store.Put(ctx, key, body, size)
}
