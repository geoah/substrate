package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

// fakeSlack serves apps.connections.open and a Socket Mode WebSocket. Each
// connection sends hello, then whatever the test pushes on frames, and
// records the acks it reads back.
type fakeSlack struct {
	srv    *httptest.Server
	opens  atomic.Int32
	frames chan string
	mu     sync.Mutex
	acks   []string
}

func newFakeSlack(t *testing.T) *fakeSlack {
	t.Helper()
	f := &fakeSlack{frames: make(chan string, 16)}
	// slack-go sends Origin: https://api.slack.com, which the default
	// same-origin check refuses.
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/apps.connections.open", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer xapp-test" {
			t.Errorf("apps.connections.open got Authorization %q", got)
		}
		f.opens.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":  true,
			"url": "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/ws",
		})
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"hello","num_connections":1,"connection_info":{"app_id":"A0TEST"}}`))
		go func() {
			for {
				var ack struct {
					EnvelopeID string `json:"envelope_id"`
				}
				if err := conn.ReadJSON(&ack); err != nil {
					return
				}
				f.mu.Lock()
				f.acks = append(f.acks, ack.EnvelopeID)
				f.mu.Unlock()
			}
		}()
		for frame := range f.frames {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
				return
			}
			if strings.Contains(frame, `"type":"disconnect"`) {
				return // the next frame goes to the next connection
			}
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(func() {
		close(f.frames)
		f.srv.CloseClientConnections()
		f.srv.Close()
	})
	return f
}

func (f *fakeSlack) acked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.acks...)
}

func TestSocketModeEndToEnd(t *testing.T) {
	slackSrv := newFakeSlack(t)
	wh := newWebhook(t, http.StatusAccepted)
	m := &Metrics{}

	api := slack.New("", slack.OptionAppLevelToken("xapp-test"), slack.OptionAPIURL(slackSrv.srv.URL+"/"))
	smc := socketmode.New(api)
	b := NewBatcher(BatcherOptions{
		Post: NewForwarder(wh.srv.URL, time.Second).Post,
		Ack: func(ctx context.Context, ids []string) error {
			for _, id := range ids {
				if err := smc.AckCtx(ctx, id, nil); err != nil {
					return err
				}
			}
			return nil
		},
		MaxEvents: 200, MaxBytes: 512 << 10, MaxWait: 20 * time.Millisecond,
		Metrics: m,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = smc.RunContext(ctx) }()
	loop := &eventLoop{client: smc, batcher: b, metrics: m, now: time.Now}
	loopDone := make(chan struct{})
	go func() { defer close(loopDone); loop.run(ctx, smc.Events) }()

	// A known event, then one slack-go has no struct for: both must reach
	// the webhook verbatim and be acked.
	reaction := `{"type":"event_callback","event_id":"Ev1","event":{"type":"reaction_added","reaction":"eyes"}}`
	unknown := `{"type":"event_callback","event_id":"Ev2","event":{"type":"a_type_slack_go_lacks"}}`
	slackSrv.frames <- `{"type":"events_api","envelope_id":"env-1","payload":` + reaction + `}`
	slackSrv.frames <- `{"type":"events_api","envelope_id":"env-2","payload":` + unknown + `}`
	waitFor(t, "acks for both events", func() bool { return len(slackSrv.acked()) == 2 })

	var all string
	for _, body := range wh.batches() {
		all += string(body)
	}
	if !strings.Contains(all, reaction) || !strings.Contains(all, unknown) {
		t.Fatalf("webhook bodies %q miss a raw payload", all)
	}
	if !m.Connected.Load() {
		t.Fatal("connected gauge is 0 while connected")
	}

	// disconnect makes slack-go open a new connection; events keep flowing.
	slackSrv.frames <- `{"type":"disconnect","reason":"refresh_requested"}`
	waitFor(t, "a reconnect", func() bool { return m.Reconnects.Load() == 1 && slackSrv.opens.Load() == 2 })
	slackSrv.frames <- `{"type":"events_api","envelope_id":"env-3","payload":` + reaction + `}`
	waitFor(t, "the ack after reconnecting", func() bool { return len(slackSrv.acked()) == 3 })

	cancel()
	<-loopDone
	b.Close()
}

func TestSocketModeNoAckWhenWebhookRefuses(t *testing.T) {
	slackSrv := newFakeSlack(t)
	wh := newWebhook(t, http.StatusServiceUnavailable)
	m := &Metrics{}

	api := slack.New("", slack.OptionAppLevelToken("xapp-test"), slack.OptionAPIURL(slackSrv.srv.URL+"/"))
	smc := socketmode.New(api)
	b := NewBatcher(BatcherOptions{
		Post:      NewForwarder(wh.srv.URL, time.Second).Post,
		Ack:       func(ctx context.Context, ids []string) error { return smc.AckCtx(ctx, ids[0], nil) },
		MaxEvents: 1, MaxBytes: 512 << 10, MaxWait: time.Hour,
		Metrics: m,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = smc.RunContext(ctx) }()
	loop := &eventLoop{client: smc, batcher: b, metrics: m, now: time.Now}
	loopDone := make(chan struct{})
	go func() { defer close(loopDone); loop.run(ctx, smc.Events) }()

	slackSrv.frames <- `{"type":"events_api","envelope_id":"env-1","payload":{"type":"event_callback","event_id":"Ev1","event":{"type":"reaction_added"}}}`
	waitFor(t, "the refused POST", func() bool { return m.BatchesFailed.Load() == 1 })
	time.Sleep(50 * time.Millisecond)
	if got := slackSrv.acked(); len(got) != 0 {
		t.Fatalf("acked %v although the webhook answered 503", got)
	}

	cancel()
	<-loopDone
	b.Close()
}
