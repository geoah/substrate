package engine

import (
	"errors"
	"log/slog"
	"testing"
	"time"
)

// A write's door reads the directory latch without writerMu, which a
// committing write holds across its fsyncs and the Postgres commit (#835),
// and still sees every latch: the commit path's (latchDirectoryErr, called
// with writerMu held) and the background digest's (latchDamaged).
// directoryErr still waits out a commit in flight, which is what a
// background pass's lockSettledRegistry stands on: a commit that publishes a
// registry holds writerMu until it has published or latched.
func TestTheWriteDoorDoesNotWaitForACommit(t *testing.T) {
	t.Parallel()
	read := func(fn func() error) (error, bool) {
		got := make(chan error, 1)
		go func() { got <- fn() }()
		select {
		case err := <-got:
			return err, true
		case <-time.After(2 * time.Second):
			return nil, false
		}
	}
	cause := errors.New("test: the final newline did not land")

	ds := &dataset{svc: &service{log: slog.New(slog.DiscardHandler)}}
	ds.writerMu.Lock()
	if err, ok := read(ds.doorDirectoryErr); !ok || err != nil {
		ds.writerMu.Unlock()
		t.Fatalf("the door on a healthy directory answered %v (answered: %v), want nil without waiting for writerMu", err, ok)
	}
	waited := make(chan error, 1)
	go func() { waited <- ds.directoryErr() }()
	select {
	case err := <-waited:
		ds.writerMu.Unlock()
		t.Fatalf("directoryErr answered %v while a commit held writerMu, want it to wait for the commit's latch", err)
	case <-time.After(200 * time.Millisecond):
	}
	ds.latchDirectoryErr(cause)
	if err, ok := read(ds.doorDirectoryErr); !ok || !errors.Is(err, ErrChangelogFileBehind) || !errors.Is(err, cause) {
		ds.writerMu.Unlock()
		t.Fatalf("the door after a commit's latch answered %v (answered: %v), want ErrChangelogFileBehind", err, ok)
	}
	ds.writerMu.Unlock()
	if err := <-waited; !errors.Is(err, ErrChangelogFileBehind) {
		t.Fatalf("directoryErr after the commit latched answered %v, want ErrChangelogFileBehind", err)
	}

	ds = &dataset{svc: &service{log: slog.New(slog.DiscardHandler)}}
	ds.latchDamaged(cause)
	if err := ds.doorDirectoryErr(); !errors.Is(err, ErrChangelogDamaged) || !errors.Is(err, cause) {
		t.Fatalf("the door after the digest's latch answered %v, want ErrChangelogDamaged", err)
	}
}
