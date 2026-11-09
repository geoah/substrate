package actions

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/geoah/substrate/cmd/chatbridge/internal/localmatrix"
	"github.com/geoah/substrate/cmd/chatbridge/internal/server"
	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

// The send limits when the owner set none (docs/plans/chat-bridge.md,
// section 7).
const (
	DefaultSendsPerHour = 30
	DefaultSendsPerDay  = 200
)

// AccountSettings are the owner's toggles on one account record, the
// `writer: owner` properties no app, agent or function can write.
type AccountSettings struct {
	DownloadMedia     string     `json:"downloadMedia,omitempty"`
	SendsPerHour      int        `json:"sendsPerHour,omitempty"`
	SendsPerDay       int        `json:"sendsPerDay,omitempty"`
	SyncPaused        bool       `json:"syncPaused,omitempty"`
	UnlinkRequestedAt *time.Time `json:"unlinkRequestedAt,omitempty"`
}

func settingsOf(rec *server.Record) AccountSettings {
	s := AccountSettings{DownloadMedia: rec.String("downloadMedia"), SyncPaused: rec.Bool("syncPaused")}
	if n, ok := rec.Int("sendsPerHour"); ok {
		s.SendsPerHour = int(n)
	}
	if n, ok := rec.Int("sendsPerDay"); ok {
		s.SendsPerDay = int(n)
	}
	if t, ok := rec.Time("unlinkRequestedAt"); ok {
		s.UnlinkRequestedAt = &t
	}
	return s
}

func (s AccountSettings) perHour() int {
	if s.SendsPerHour > 0 {
		return s.SendsPerHour
	}
	return DefaultSendsPerHour
}

func (s AccountSettings) perDay() int {
	if s.SendsPerDay > 0 {
		return s.SendsPerDay
	}
	return DefaultSendsPerDay
}

// Settings holds every account's toggles in memory, read from the store at
// start and kept current by the change feed.
type Settings struct {
	mu sync.RWMutex
	m  map[string]AccountSettings
}

// NewSettings loads what the store knows.
func NewSettings() *Settings { return &Settings{m: map[string]AccountSettings{}} }

func key(network, account string) string { return network + "\x00" + account }

// Get reads one account's toggles.
func (s *Settings) Get(network, account string) AccountSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.m[key(network, account)]
}

// Put replaces one account's toggles.
func (s *Settings) Put(network, account string, v AccountSettings) {
	s.mu.Lock()
	s.m[key(network, account)] = v
	s.mu.Unlock()
}

// MediaPolicy implements localmatrix.Settings.
func (s *Settings) MediaPolicy(network, account string) string {
	switch p := s.Get(network, account).DownloadMedia; p {
	case localmatrix.MediaNone, localmatrix.MediaSmall, localmatrix.MediaAll:
		return p
	}
	return localmatrix.MediaSmall
}

// Load fills the settings from the store's account map.
func (s *Settings) Load(ctx context.Context, st *store.Store, refs []store.AccountRef) {
	for _, r := range refs {
		var v AccountSettings
		if json.Unmarshal([]byte(r.Settings), &v) == nil {
			s.Put(r.Network, r.LoginID, v)
		}
	}
}
