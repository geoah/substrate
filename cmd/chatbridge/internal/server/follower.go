package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

// Sink takes what the follower reads: the owner's requests and toggles.
type Sink interface {
	// Action is a chataction record as it stands, or its deletion.
	Action(ctx context.Context, rec *Record)
	ActionDeleted(ctx context.Context, id string)
	// Account is an account record as it stands; AccountDeleted names the
	// login a deleted record was, through the bridge's own map.
	Account(ctx context.Context, rec *Record)
	AccountDeleted(ctx context.Context, network, loginID string)
}

// FeedMetrics count what the follower did.
type FeedMetrics struct {
	Rows       atomic.Int64
	Reconnects atomic.Int64
	Relists    atomic.Int64
}

// bookmark is where the follower resumes the feed.
type bookmark struct {
	Seq        int64  `json:"seq"`
	Generation string `json:"generation"`
}

// Follower follows the change feed for the package's kinds, as follow
// changes prescribes: open the watch first and keep its bookmark, then list
// the queued actions and the accounts; on a 410, list again.
type Follower struct {
	Store    *store.Store
	Client   *Client
	Instance string
	Pusher   *Pusher
	Sink     Sink
	Log      *slog.Logger
	Metrics  *FeedMetrics
	Backoff  time.Duration

	mu        sync.Mutex
	connected bool
	lastErr   string
}

// Kinds are the kinds the follower watches.
var Kinds = []string{KindAction, KindAccount, KindInstance}

// FeedStatus is what /healthz shows of the follower.
type FeedStatus struct {
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
}

// Status reads the follower's state.
func (f *Follower) Status() FeedStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return FeedStatus{Connected: f.connected, Error: f.lastErr}
}

// Run follows until ctx ends.
func (f *Follower) Run(ctx context.Context) error {
	if f.Log == nil {
		f.Log = slog.Default()
	}
	if f.Metrics == nil {
		f.Metrics = &FeedMetrics{}
	}
	if f.Backoff <= 0 {
		f.Backoff = 5 * time.Second
	}
	backoff := f.Backoff
	relist := true
	for {
		bm := f.loadBookmark(ctx)
		opened := false
		err := f.Client.Watch(ctx, Kinds, bm.Seq, bm.Generation,
			func(seq int64, gen string) error {
				opened = true
				f.setConnected(true, "")
				if bm.Seq == 0 || bm.Generation != gen {
					if err := f.saveBookmark(ctx, bookmark{Seq: seq, Generation: gen}); err != nil {
						return err
					}
					bm = bookmark{Seq: seq, Generation: gen}
				}
				if relist {
					if err := f.relist(ctx); err != nil {
						return err
					}
					relist = false
				}
				return nil
			},
			func(ch Change) error {
				f.Metrics.Rows.Add(1)
				f.dispatch(ctx, ch)
				bm.Seq = ch.Seq
				return f.saveBookmark(ctx, bm)
			})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		f.setConnected(false, errString(err))
		f.Metrics.Reconnects.Add(1)
		var ae *APIError
		switch {
		case errors.As(err, &ae) && ae.Status == http.StatusGone:
			// The bookmark is under another history or below the horizon:
			// list again and resume where the server says.
			next := bookmark{}
			if ae.Head != nil {
				next = bookmark{Seq: *ae.Head, Generation: ae.Generation}
			}
			if err := f.saveBookmark(ctx, next); err != nil {
				return err
			}
			relist = true
			continue
		case errors.Is(err, ErrUnauthorized):
			f.Log.Warn("the bridge has no token the server takes; run chatbridge authorize")
			backoff = 30 * time.Second
		case opened:
			backoff = f.Backoff
		}
		if err != nil {
			f.Log.Warn("the change feed stopped; reconnecting", "error", err, "retry_in", backoff.String())
		}
		if !sleep(ctx, backoff) {
			return ctx.Err()
		}
		backoff = min(backoff*2, 2*time.Minute)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (f *Follower) setConnected(c bool, errText string) {
	f.mu.Lock()
	f.connected, f.lastErr = c, errText
	f.mu.Unlock()
}

// relist reads the accounts, the queued actions and this instance's row.
func (f *Follower) relist(ctx context.Context) error {
	f.Metrics.Relists.Add(1)
	accounts, err := f.Client.List(ctx, Filter{Kinds: []string{KindAccount}})
	if err != nil {
		return err
	}
	for i := range accounts {
		f.Sink.Account(ctx, &accounts[i])
	}
	actions, err := f.Client.List(ctx, Filter{
		Kinds:      []string{KindAction},
		Properties: map[string]map[string]any{"state": {"eq": "queued"}},
	})
	if err != nil {
		return err
	}
	for i := range actions {
		f.Sink.Action(ctx, &actions[i])
	}
	instances, err := f.Client.List(ctx, Filter{
		Kinds:      []string{KindInstance},
		Properties: map[string]map[string]any{"instanceId": {"eq": f.Instance}},
	})
	if err != nil {
		return err
	}
	for i := range instances {
		f.instance(&instances[i])
	}
	return nil
}

// dispatch reads each record a row names and hands it on.
func (f *Follower) dispatch(ctx context.Context, ch Change) {
	for _, a := range ch.Records() {
		switch a.Kind {
		case KindAction:
			if a.Deleted {
				f.Sink.ActionDeleted(ctx, a.ID)
				continue
			}
			rec, err := f.Client.GetRecord(ctx, a.Kind, a.ID)
			if err != nil {
				f.Log.Warn("reading an action failed", "error", err)
				continue
			}
			if rec == nil || rec.DeletedAt != nil {
				f.Sink.ActionDeleted(ctx, a.ID)
				continue
			}
			f.Sink.Action(ctx, rec)
		case KindAccount:
			if !a.Deleted {
				rec, err := f.Client.GetRecord(ctx, a.Kind, a.ID)
				if err != nil {
					f.Log.Warn("reading an account failed", "error", err)
					continue
				}
				if rec != nil && rec.DeletedAt == nil {
					f.Sink.Account(ctx, rec)
					continue
				}
			}
			ref, err := f.Store.AccountByRecord(ctx, a.ID)
			if err != nil || ref == nil {
				continue
			}
			f.Sink.AccountDeleted(ctx, ref.Network, ref.LoginID)
			_ = f.Store.DeleteAccountRef(ctx, a.ID)
		case KindInstance:
			if a.Deleted {
				continue
			}
			rec, err := f.Client.GetRecord(ctx, a.Kind, a.ID)
			if err != nil || rec == nil {
				continue
			}
			f.instance(rec)
		}
	}
}

// instance keeps this instance's row id, which accounts name, and hands
// its appliedSeq to the pusher.
func (f *Follower) instance(rec *Record) {
	if rec.String("instanceId") != f.Instance {
		return
	}
	if old, _, _ := f.Store.Get(context.Background(), store.KeyInstanceID); old != rec.ID {
		if err := f.Store.Set(context.Background(), store.KeyInstanceID, rec.ID); err != nil {
			f.Log.Warn("recording this instance's row failed", "error", err)
		}
	}
	if n, ok := rec.Int("appliedSeq"); ok && f.Pusher != nil {
		f.Pusher.Applied(n)
	}
}

func (f *Follower) loadBookmark(ctx context.Context) bookmark {
	raw, ok, err := f.Store.Get(ctx, store.KeyFeed)
	if err != nil || !ok || raw == "" {
		return bookmark{}
	}
	var bm bookmark
	_ = json.Unmarshal([]byte(raw), &bm)
	return bm
}

func (f *Follower) saveBookmark(ctx context.Context, bm bookmark) error {
	raw, err := json.Marshal(bm)
	if err != nil {
		return err
	}
	return f.Store.Set(ctx, store.KeyFeed, string(raw))
}
