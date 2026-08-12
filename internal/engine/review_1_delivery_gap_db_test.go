package engine_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

// TestZZSK1DeliveryGap models the production shape exactly: a connector sync
// burst writing message rows concurrently with the owner queueing outbound
// messages, watched by the connectors' delivery poller (same filter, same
// cursor algorithm). Any owner "delivery: queued" patch whose seq the poller
// never observes is a message that is never delivered.
func TestZZSK1DeliveryGap(t *testing.T) {
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}
	ctx := context.Background()

	acc := mustPut(t, ds, slack, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "slack-account:T1",
		Properties: map[string]any{"provider": "slack", "label": "Acme"},
	})
	conv := mustPut(t, ds, slack, substrate.PutInput{
		Kind: "conversation", ID: "slack-channel:T1:C1",
		Properties: map[string]any{"kind": "channel", "name": "general"},
		Edges:      []substrate.EdgeInput{{Rel: "account", To: substrate.EdgeRef{Kind: enginetest.AccountType, ID: acc.ID}}},
	})
	author := mustPut(t, ds, slack, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "alex"},
	})

	newMsg := func(actor substrate.Actor, ext, text, delivery string) *substrate.Record {
		return mustPut(t, ds, actor, substrate.PutInput{
			Kind: "conversationmessage", ID: extID("slack.msg", ext),
			Properties: map[string]any{"at": "2026-08-03T10:00:00Z", "text": text, "delivery": delivery},
			Edges: []substrate.EdgeInput{
				{Rel: "conversation", To: substrate.EdgeRef{ID: conv.ID}},
				{Rel: "author", To: substrate.EdgeRef{ID: author.ID}},
			},
		})
	}

	// Owner drafts N outbound messages up front; it will queue them mid-burst.
	const drafts = 40
	var draftIDs []string
	for i := range drafts {
		e := newMsg(owner, fmt.Sprintf("out/%d", i), "outbound", "draft")
		draftIDs = append(draftIDs, e.ID)
	}

	observed := map[int64]bool{}
	var mu sync.Mutex
	var cursor int64
	stop := make(chan struct{})

	// The connectors' delivery poller, verbatim in shape.
	poll := func() {
		chs, err := ds.Changes(ctx, cursor, substrate.ChangeFilter{
			Kinds: []string{"conversationmessage"},
			Ops:   []substrate.Op{substrate.OpPut, substrate.OpPatch},
		}, 200)
		if err != nil {
			t.Errorf("changes: %v", err)
			return
		}
		mx := cursor
		for _, c := range chs {
			mu.Lock()
			observed[c.Seq] = true
			mu.Unlock()
			if c.Seq > mx {
				mx = c.Seq
			}
		}
		cursor = mx
	}
	var pollWG sync.WaitGroup
	pollWG.Add(1)
	go func() {
		defer pollWG.Done()
		for {
			select {
			case <-stop:
				for range 5 {
					poll()
				}
				return
			default:
			}
			poll()
			time.Sleep(2 * time.Millisecond)
		}
	}()

	var wg sync.WaitGroup
	// Connector sync burst.
	for w := range 4 {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range 60 {
				newMsg(slack, fmt.Sprintf("in/%d/%d", w, i), "inbound", "received")
			}
		}(w)
	}
	// Owner queues its drafts concurrently; record the seq each patch earned.
	queuedSeq := map[string]int64{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, id := range draftIDs {
			if _, err := ds.Patch(ctx, owner, "conversationmessage", id, substrate.PatchInput{
				Properties: map[string]any{"delivery": "queued"},
			}); err != nil {
				t.Errorf("patch: %v", err)
				return
			}
			time.Sleep(3 * time.Millisecond)
		}
	}()
	wg.Wait()
	time.Sleep(200 * time.Millisecond)
	close(stop)
	pollWG.Wait()

	// Ground truth: the seq of every owner queued-patch row.
	var last int64
	for {
		chs, err := ds.Changes(ctx, last, substrate.ChangeFilter{
			Kinds:  []string{"conversationmessage"},
			Ops:    []substrate.Op{substrate.OpPatch},
			Actors: []substrate.Actor{owner},
		}, 500)
		if err != nil {
			t.Fatalf("changes: %v", err)
		}
		if len(chs) == 0 {
			break
		}
		for _, c := range chs {
			queuedSeq[c.RecordID] = c.Seq
			last = c.Seq
		}
	}
	var lost []string
	for id, s := range queuedSeq {
		mu.Lock()
		ok := observed[s]
		mu.Unlock()
		if !ok {
			lost = append(lost, fmt.Sprintf("%s@seq%d", id, s))
		}
	}
	t.Logf("queued patches: %d, never observed by the poller: %d %v", len(queuedSeq), len(lost), lost)
	if len(lost) > 0 {
		t.Fatalf("%d queued messages would never be delivered: %v", len(lost), lost)
	}
}
