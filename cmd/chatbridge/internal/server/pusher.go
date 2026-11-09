package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

// The pusher's states, for health.
const (
	PushIdle         = "idle"
	PushWaiting      = "waiting"
	PushParked       = "parked"
	PushRefused      = "refused"
	PushUnauthorized = "unauthorized"
	PushBehind       = "behind"
)

// inflight is the one batch the pusher has sent and not seen applied. It is
// kept in the store, so a restart waits for it rather than sending past it.
type inflight struct {
	First    int64     `json:"first"`
	Last     int64     `json:"last"`
	Fire     string    `json:"fire"`
	SentAt   time.Time `json:"sentAt"`
	Attempts int       `json:"attempts"`
}

// PushStatus is what /healthz shows of the pusher.
type PushStatus struct {
	State      string    `json:"state"`
	Applied    int64     `json:"appliedSeq"`
	Last       int64     `json:"lastSeq"`
	InFlight   *[2]int64 `json:"inFlight,omitempty"`
	Fire       string    `json:"fire,omitempty"`
	Error      string    `json:"error,omitempty"`
	RetryAfter time.Time `json:"retryAfter,omitzero"`
}

// PushMetrics count what the pusher did.
type PushMetrics struct {
	BatchesSent   atomic.Int64
	EventsSent    atomic.Int64
	BatchesFailed atomic.Int64
	BatchesParked atomic.Int64
	Resends       atomic.Int64
}

// Pusher delivers the outbox to the webhook door one batch at a time: batch
// N+1 leaves only once the instance row reads appliedSeq >= last(N), which
// is what makes the function's applied sequence the order of the stream.
type Pusher struct {
	Store    *store.Store
	Client   *Client
	Instance string
	// WebhookURL is read for every delivery, so a new authorization is used
	// without a restart.
	WebhookURL func() string
	Limits     store.Limits
	Log        *slog.Logger
	Metrics    *PushMetrics

	// Poll is how often a wait reads the instance row and the parked list
	// itself, in case the change feed is down.
	Poll time.Duration
	// Backoff is the first wait after a failed or parked delivery; it
	// doubles up to MaxBackoff.
	Backoff    time.Duration
	MaxBackoff time.Duration
	// Stale is how long a delivery may sit neither applied, filing nor
	// parked before it is sent again; the function skips what it applied.
	Stale time.Duration

	applied chan int64

	mu     sync.Mutex
	status PushStatus
	once   sync.Once
}

func (p *Pusher) init() {
	p.once.Do(func() {
		p.applied = make(chan int64, 16)
		if p.Log == nil {
			p.Log = slog.Default()
		}
		if p.Metrics == nil {
			p.Metrics = &PushMetrics{}
		}
		if p.Limits.Events == 0 {
			p.Limits = store.DefaultLimits
		}
		if p.Poll <= 0 {
			p.Poll = 15 * time.Second
		}
		if p.Backoff <= 0 {
			p.Backoff = 30 * time.Second
		}
		if p.MaxBackoff <= 0 {
			p.MaxBackoff = 30 * time.Minute
		}
		if p.Stale <= 0 {
			p.Stale = 10 * time.Minute
		}
		p.status.State = PushIdle
	})
}

// Applied tells the pusher the instance row's appliedSeq, as the change
// feed read it.
func (p *Pusher) Applied(seq int64) {
	p.init()
	select {
	case p.applied <- seq:
	default:
	}
}

// Status reads the pusher's state.
func (p *Pusher) Status() PushStatus {
	p.init()
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

func (p *Pusher) setStatus(fn func(*PushStatus)) {
	p.mu.Lock()
	fn(&p.status)
	p.mu.Unlock()
}

// Run delivers until ctx ends.
func (p *Pusher) Run(ctx context.Context) error {
	p.init()
	changed := p.Store.Changed()
	applied, err := p.Store.GetInt(ctx, store.KeyApplied)
	if err != nil {
		return err
	}
	fl, err := p.loadInflight(ctx)
	if err != nil {
		return err
	}
	backoff := p.Backoff
	for {
		last, _ := p.Store.Last(ctx)
		p.setStatus(func(s *PushStatus) { s.Applied, s.Last = applied, last })
		if fl == nil {
			rows, err := p.Store.Pending(ctx, applied, p.Limits)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				p.setStatus(func(s *PushStatus) { s.State, s.InFlight, s.Fire, s.Error = PushIdle, nil, "", "" })
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-changed:
				case seq := <-p.applied:
					applied = max(applied, seq)
				}
				continue
			}
			fl = &inflight{First: rows[0].Seq, Last: rows[len(rows)-1].Seq}
			if err := p.send(ctx, fl, rows); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				wait := p.failed(err, backoff)
				fl = nil
				if !sleep(ctx, wait) {
					return ctx.Err()
				}
				backoff = min(backoff*2, p.MaxBackoff)
				continue
			}
			if err := p.saveInflight(ctx, fl); err != nil {
				return err
			}
		}

		// One batch is out: wait for it to apply or park.
		done, parked, err := p.wait(ctx, fl, &applied)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			p.Log.Warn("waiting for a batch to apply failed", "error", err)
		}
		switch {
		case done:
			if err := p.Store.SetInt(ctx, store.KeyApplied, applied); err != nil {
				return err
			}
			if err := p.Store.Trim(ctx, applied); err != nil {
				return err
			}
			if err := p.saveInflight(ctx, nil); err != nil {
				return err
			}
			fl = nil
			backoff = p.Backoff
			p.setStatus(func(s *PushStatus) { s.State, s.InFlight, s.Fire, s.Error = PushIdle, nil, "", "" })
		case parked != nil:
			p.Metrics.BatchesParked.Add(1)
			retry := time.Now().Add(backoff)
			p.setStatus(func(s *PushStatus) {
				s.State, s.Error, s.RetryAfter = PushParked, parked.LastError, retry
			})
			p.Log.Error("a batch parked on the server after its retries; sending it again after a pause",
				"first", fl.First, "last", fl.Last, "fire", fl.Fire, "attempts", parked.Attempts, "retry_in", backoff.String())
			if !sleep(ctx, backoff) {
				return ctx.Err()
			}
			backoff = min(backoff*2, p.MaxBackoff)
			rows, err := p.Store.Pending(ctx, fl.First-1, store.Limits{Events: int(fl.Last - fl.First + 1), JSON: 1 << 30, Media: 1 << 40})
			if err != nil {
				return err
			}
			p.Metrics.Resends.Add(1)
			if err := p.send(ctx, fl, rows); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				wait := p.failed(err, backoff)
				if !sleep(ctx, wait) {
					return ctx.Err()
				}
				continue
			}
			if err := p.saveInflight(ctx, fl); err != nil {
				return err
			}
		}
	}
}

// send posts one batch; on success fl carries the fire id.
func (p *Pusher) send(ctx context.Context, fl *inflight, rows []store.Row) error {
	if len(rows) == 0 || rows[0].Seq != fl.First || rows[len(rows)-1].Seq != fl.Last {
		return errors.New("the outbox no longer holds the batch in flight")
	}
	batch := contract.Batch{Version: contract.Version, Instance: p.Instance, First: fl.First, Last: fl.Last}
	var media []MediaPart
	seen := map[string]bool{}
	for _, r := range rows {
		batch.Events = append(batch.Events, r.Body)
		for _, m := range r.Media {
			if seen[m] {
				continue
			}
			seen[m] = true
			info, err := p.Store.Media(ctx, m)
			if err != nil {
				return err
			}
			if info == nil {
				continue
			}
			path := p.Store.MediaPath(info.ID)
			media = append(media, MediaPart{
				Name: info.ID, FileName: info.FileName, MimeType: info.MimeType,
				Open: func() (io.ReadCloser, error) { return os.Open(path) },
			})
		}
	}
	body, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	webhook := p.WebhookURL()
	if webhook == "" {
		return ErrUnauthorized
	}
	fire, err := p.Client.Deliver(ctx, webhook, body, media)
	if err != nil {
		p.Metrics.BatchesFailed.Add(1)
		return err
	}
	fl.Fire, fl.SentAt = fire, time.Now()
	fl.Attempts++
	p.Metrics.BatchesSent.Add(1)
	p.Metrics.EventsSent.Add(int64(len(rows)))
	inFlight := [2]int64{fl.First, fl.Last}
	p.setStatus(func(s *PushStatus) { s.State, s.InFlight, s.Fire, s.Error = PushWaiting, &inFlight, fire, "" })
	p.Log.Debug("delivered a batch", "first", fl.First, "last", fl.Last, "events", len(rows), "files", len(media), "fire", fire)
	return nil
}

// failed records a delivery that did not reach the door and returns how
// long to wait.
func (p *Pusher) failed(err error, backoff time.Duration) time.Duration {
	state := PushRefused
	var ae *APIError
	switch {
	case errors.Is(err, ErrUnauthorized):
		state = PushUnauthorized
		backoff = min(backoff, 30*time.Second)
	case errors.As(err, &ae) && ae.Status == http.StatusServiceUnavailable:
		backoff = min(backoff, 30*time.Second)
	}
	retry := time.Now().Add(backoff)
	p.setStatus(func(s *PushStatus) { s.State, s.Error, s.RetryAfter = state, err.Error(), retry })
	p.Log.Warn("delivering a batch failed", "error", err, "retry_in", backoff.String())
	return backoff
}

// wait blocks until the batch applied (the instance row's appliedSeq reached
// its last seq) or parked on the server, polling both in case the change
// feed is down.
func (p *Pusher) wait(ctx context.Context, fl *inflight, applied *int64) (bool, *Parked, error) {
	if *applied >= fl.Last {
		return true, nil, nil
	}
	tick := time.NewTicker(p.Poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return false, nil, ctx.Err()
		case seq := <-p.applied:
			*applied = max(*applied, seq)
		case <-tick.C:
			seq, err := p.readApplied(ctx)
			if err != nil {
				return false, nil, err
			}
			*applied = max(*applied, seq)
			if *applied >= fl.Last {
				return true, nil, nil
			}
			parked, pending, err := p.parked(ctx, fl.Fire)
			if err != nil {
				return false, nil, err
			}
			if parked != nil {
				return false, parked, nil
			}
			if !pending && time.Since(fl.SentAt) > p.Stale {
				// Neither applied nor on the trigger's list: the delivery
				// was lost between the door and the fire. Sending it again
				// is safe, since the function skips what it applied.
				return false, &Parked{FireID: fl.Fire, LastError: "the delivery was neither applied nor parked"}, nil
			}
		}
		if *applied >= fl.Last {
			return true, nil, nil
		}
	}
}

// readApplied reads the instance row's appliedSeq.
func (p *Pusher) readApplied(ctx context.Context) (int64, error) {
	recs, err := p.Client.List(ctx, Filter{
		Kinds:      []string{KindInstance},
		Properties: map[string]map[string]any{"instanceId": {"eq": p.Instance}},
	})
	if err != nil {
		return 0, err
	}
	var applied int64
	for i := range recs {
		if n, ok := recs[i].Int("appliedSeq"); ok && n > applied {
			applied = n
		}
	}
	return applied, nil
}

// parked looks for the fire on the trigger's parked list: parked after its
// retries, or still filing.
func (p *Pusher) parked(ctx context.Context, fire string) (*Parked, bool, error) {
	items, err := p.Client.ParkedList(ctx)
	if err != nil {
		return nil, false, err
	}
	for i := range items {
		if items[i].FireID != fire {
			continue
		}
		if items[i].Pending || items[i].Running {
			return nil, true, nil
		}
		return &items[i], false, nil
	}
	return nil, false, nil
}

func (p *Pusher) loadInflight(ctx context.Context) (*inflight, error) {
	raw, ok, err := p.Store.Get(ctx, store.KeyInflight)
	if err != nil || !ok || raw == "" {
		return nil, err
	}
	var fl inflight
	if err := json.Unmarshal([]byte(raw), &fl); err != nil {
		return nil, nil
	}
	inFlight := [2]int64{fl.First, fl.Last}
	p.setStatus(func(s *PushStatus) { s.State, s.InFlight, s.Fire = PushWaiting, &inFlight, fl.Fire })
	return &fl, nil
}

func (p *Pusher) saveInflight(ctx context.Context, fl *inflight) error {
	if fl == nil {
		return p.Store.Delete(ctx, store.KeyInflight)
	}
	raw, err := json.Marshal(fl)
	if err != nil {
		return err
	}
	return p.Store.Set(ctx, store.KeyInflight, string(raw))
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
