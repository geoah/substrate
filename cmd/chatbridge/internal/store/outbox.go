package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
)

// ErrEventTooLarge refuses an event that could never fit a batch.
var ErrEventTooLarge = errors.New("the event is larger than a batch may be")

// maxEventJSON leaves room in a batch for the envelope around one event.
const maxEventJSON = contract.MaxBatchJSON - 1024

// Append writes ev to the outbox with the next sequence number and returns
// it. media names the media parts the event refers to, which ride along in
// the request that carries it. The sequence advances inside the transaction,
// so it has no gaps: the function refuses a batch past a gap forever.
func (tx *Tx) Append(ctx context.Context, ev contract.Event, media ...string) (int64, error) {
	h := contract.HeadOf(ev)
	var next int64 = 1
	var raw string
	err := tx.tx.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ?`, KeyOutboxNext).Scan(&raw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return 0, err
	default:
		next, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("the outbox sequence is unreadable: %w", err)
		}
	}
	h.Seq = next
	if h.At == "" {
		h.At = contract.At(tx.s.now())
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return 0, err
	}
	if len(body) > maxEventJSON {
		return 0, fmt.Errorf("%w: %s of %d bytes", ErrEventTooLarge, h.Type, len(body))
	}
	if _, err := tx.tx.ExecContext(ctx, `INSERT INTO outbox (seq, type, account, body, media, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		next, h.Type, h.Account, body, strings.Join(media, " "), tx.s.now().UnixMilli()); err != nil {
		return 0, err
	}
	if _, err := tx.tx.ExecContext(ctx, `INSERT INTO kv (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, KeyOutboxNext, strconv.FormatInt(next+1, 10)); err != nil {
		return 0, err
	}
	tx.appended = true
	return next, nil
}

// Row is one outbox event as written.
type Row struct {
	Seq   int64
	Type  string
	Body  json.RawMessage
	Media []string
}

// Limits bound one batch.
type Limits struct {
	Events int
	JSON   int
	// Media is the bytes of files one request may carry beside the JSON.
	Media int64
}

// DefaultLimits are the contract's: 200 events and 1 MiB of JSON, and what
// the door's 32 MiB multipart bound leaves for files after them.
var DefaultLimits = Limits{
	Events: contract.MaxEvents,
	JSON:   contract.MaxBatchJSON,
	Media:  contract.MaxRequest - contract.MaxBatchJSON - 64<<10,
}

// batchOverhead is the JSON around the events: the version, the instance id,
// first and last.
const batchOverhead = 160

// Pending reads the next batch after seq: as many events as fit the limits,
// at least one when any is pending.
func (s *Store) Pending(ctx context.Context, after int64, lim Limits) ([]Row, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT seq, type, body, media FROM outbox WHERE seq > ? ORDER BY seq LIMIT ?`, after, lim.Events)
	if err != nil {
		return nil, err
	}
	var read []Row
	for rows.Next() {
		var r Row
		var media string
		if err := rows.Scan(&r.Seq, &r.Type, &r.Body, &media); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if media != "" {
			r.Media = strings.Fields(media)
		}
		read = append(read, r)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The store has one connection, so media sizes are read after the
	// cursor closes rather than beside it.
	sizes := map[string]int64{}
	for _, r := range read {
		for _, m := range r.Media {
			if _, ok := sizes[m]; ok {
				continue
			}
			info, err := s.Media(ctx, m)
			if err != nil {
				return nil, err
			}
			if info != nil {
				sizes[m] = info.Size
			} else {
				sizes[m] = 0
			}
		}
	}
	var out []Row
	size := batchOverhead
	var mediaSize int64
	seen := map[string]bool{}
	for _, r := range read {
		if len(out) > 0 && out[len(out)-1].Seq+1 != r.Seq {
			// A gap is a bug the function would stall on; stop before it so
			// the batch at least stays contiguous.
			break
		}
		var addedMedia int64
		for _, m := range r.Media {
			if !seen[m] {
				addedMedia += sizes[m]
			}
		}
		if len(out) > 0 && (size+len(r.Body)+1 > lim.JSON || mediaSize+addedMedia > lim.Media) {
			break
		}
		size += len(r.Body) + 1
		mediaSize += addedMedia
		for _, m := range r.Media {
			seen[m] = true
		}
		out = append(out, r)
	}
	return out, nil
}

// Last is the sequence number of the newest event, 0 for none.
func (s *Store) Last(ctx context.Context) (int64, error) {
	next, err := s.GetInt(ctx, KeyOutboxNext)
	if err != nil || next == 0 {
		return 0, err
	}
	return next - 1, nil
}

// Trim deletes the events through seq, which the server has applied.
func (s *Store) Trim(ctx context.Context, through int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM outbox WHERE seq <= ?`, through)
	return err
}

// Depth is how many events wait in the outbox after seq.
func (s *Store) Depth(ctx context.Context, after int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM outbox WHERE seq > ?`, after).Scan(&n)
	return n, err
}
