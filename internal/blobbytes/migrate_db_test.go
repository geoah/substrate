package blobbytes_test

// Moving the bytes between backends, which is the operator's way across a
// backend switch. It runs against Postgres and a directory, in both
// directions, because those are the two halves of an ordinary upgrade.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/blobbytes"
)

func TestMoveBetweenPostgresAndFS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newPostgresDB(t)
	fs := newFS(t)

	pg, err := blobbytes.NewPostgres().Repository("repomigrate", db)
	if err != nil {
		t.Fatalf("bind postgres: %v", err)
	}
	disk, err := fs.Repository("repomigrate", nil)
	if err != nil {
		t.Fatalf("bind fs: %v", err)
	}

	bodies := []string{"first", "second", "third"}
	stored := map[string]string{}
	for _, body := range bodies {
		stored[put(t, pg, []byte(body))] = body
	}

	moved, err := blobbytes.Move(ctx, pg, disk)
	if err != nil {
		t.Fatalf("move to fs: %v", err)
	}
	if moved != len(bodies) {
		t.Fatalf("moved %d blobs, stored %d", moved, len(bodies))
	}
	// Every byte is on disk, and the source holds nothing: the row is deleted
	// only after the object is durable, and the count is what the operator
	// checks the move against.
	for digest, body := range stored {
		if got := read(t, disk, digest, len(body)); string(got) != body {
			t.Fatalf("fs holds %q for %s, want %q", got, digest, body)
		}
	}
	left, err := blobbytes.Count(ctx, pg)
	if err != nil {
		t.Fatalf("count postgres: %v", err)
	}
	if left != 0 {
		t.Fatalf("%d blobs still hold their bytes in postgres", left)
	}

	// Running it again moves nothing and breaks nothing, which is what makes
	// an interrupted move safe to repeat.
	again, err := blobbytes.Move(ctx, pg, disk)
	if err != nil {
		t.Fatalf("second move: %v", err)
	}
	if again != 0 {
		t.Fatalf("a second move moved %d blobs", again)
	}

	// And back, because a backend switch has to be reversible or it is a trap.
	back, err := blobbytes.Move(ctx, disk, pg)
	if err != nil {
		t.Fatalf("move back to postgres: %v", err)
	}
	if back != len(bodies) {
		t.Fatalf("moved %d blobs back, stored %d", back, len(bodies))
	}
	for digest, body := range stored {
		if got := read(t, pg, digest, len(body)); string(got) != body {
			t.Fatalf("postgres holds %q for %s, want %q", got, digest, body)
		}
	}
	left, err = blobbytes.Count(ctx, disk)
	if err != nil {
		t.Fatalf("count fs: %v", err)
	}
	if left != 0 {
		t.Fatalf("%d objects are still on disk", left)
	}
}

// A move that is interrupted after the copy and before the delete leaves the
// object in both stores. Repeating it must finish the job rather than fail on
// what is already there.
func TestMoveResumesOverACopyThatAlreadyLanded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newPostgresDB(t)
	fs := newFS(t)
	pg, err := blobbytes.NewPostgres().Repository("reporesume", db)
	if err != nil {
		t.Fatalf("bind postgres: %v", err)
	}
	disk, err := fs.Repository("reporesume", nil)
	if err != nil {
		t.Fatalf("bind fs: %v", err)
	}

	data := []byte("copied, not yet deleted")
	digest := put(t, pg, data)
	// The state an interruption leaves: in both stores.
	put(t, disk, data)

	moved, err := blobbytes.Move(ctx, pg, disk)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if moved != 1 {
		t.Fatalf("the resumed move reported %d blobs, want 1", moved)
	}
	if got := read(t, disk, digest, len(data)); string(got) != string(data) {
		t.Fatalf("the target holds %q", got)
	}
	held, err := pg.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("exists in the source: %v", err)
	}
	if held {
		t.Fatal("the source still holds the bytes after the resumed move")
	}
}
