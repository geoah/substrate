package blobbytes

import (
	"context"
	"errors"
	"fmt"
)

// moveBatch is how many objects one listing page of a move carries. The move
// is per-object, so this bounds memory and nothing else.
const moveBatch = 200

// Move copies every object src holds into dst, then deletes it from src. It
// returns how many objects it moved.
//
// The order per object is copy, verify, delete: an interruption leaves the
// object in both stores, never in neither. It is resumable for the same
// reason — an object dst already holds is not copied again, and one src no
// longer holds was already moved — so a run that stops anywhere continues by
// being run again.
//
// It moves BYTES and nothing else. The blob manifest is a record in Postgres
// and stays the truth whichever store the bytes are in, so no caller of this
// writes to the changelog.
func Move(ctx context.Context, src, dst Store) (int, error) {
	moved := 0
	after := ""
	for {
		objects, err := src.List(ctx, after, moveBatch)
		if err != nil {
			return moved, err
		}
		if len(objects) == 0 {
			return moved, nil
		}
		for _, o := range objects {
			after = o.Digest
			if err := moveObject(ctx, src, dst, o); err != nil {
				return moved, fmt.Errorf("blob %s: %w", o.Digest, err)
			}
			moved++
		}
	}
}

func moveObject(ctx context.Context, src, dst Store, o Object) error {
	held, err := dst.Exists(ctx, o.Digest)
	if err != nil {
		return err
	}
	if !held {
		rc, err := src.Open(ctx, o.Digest)
		if errors.Is(err, ErrNotStored) {
			// Moved by an earlier run, or collected between the listing and
			// here. Either way there is nothing left to move.
			return nil
		}
		if err != nil {
			return err
		}
		err = dst.Put(ctx, o.Digest, o.Size, rc)
		_ = rc.Close()
		if err != nil {
			return err
		}
		// Confirm the target holds the bytes before the source loses them: a
		// put that reported success and stored nothing would otherwise take
		// the last copy with it.
		held, err = dst.Exists(ctx, o.Digest)
		if err != nil {
			return err
		}
		if !held {
			return errors.New("the target store does not hold it after the copy")
		}
	}
	return src.Delete(ctx, o.Digest)
}

// Count reports how many objects a store holds. It is what a dry run reports
// and what an operator checks a finished move against.
func Count(ctx context.Context, s Store) (int, error) {
	n := 0
	after := ""
	for {
		objects, err := s.List(ctx, after, moveBatch)
		if err != nil {
			return n, err
		}
		if len(objects) == 0 {
			return n, nil
		}
		n += len(objects)
		after = objects[len(objects)-1].Digest
	}
}
