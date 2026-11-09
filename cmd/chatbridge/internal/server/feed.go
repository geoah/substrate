package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Affected is one record a change moved.
type Affected struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version int64  `json:"version,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
}

// Change is one row of the change feed. It carries no payload: the
// follower reads each record it names.
type Change struct {
	Seq      int64      `json:"seq"`
	Op       string     `json:"op"`
	Kind     string     `json:"kind"`
	RecordID string     `json:"recordId"`
	Actor    string     `json:"actor"`
	Affected []Affected `json:"affected"`
}

// Records are the records a change names: its affected list, or the row's
// own record when the list is empty.
func (c Change) Records() []Affected {
	if len(c.Affected) > 0 {
		return c.Affected
	}
	return []Affected{{Kind: c.Kind, ID: c.RecordID, Deleted: c.Op == "delete"}}
}

type frame struct {
	Change
	Bookmark   *int64    `json:"bookmark"`
	Generation string    `json:"generation"`
	Error      *APIError `json:"error"`
}

// StreamIdle is how long a watch may stay silent before it is taken for
// dead; the server sends a heartbeat every 30 seconds.
var StreamIdle = 90 * time.Second

// Watch tails the change feed over kinds from a bookmark (0 for the head).
// onOpen gets the bookmark and generation the stream opened at, before any
// row; onChange gets each row in order. It returns when ctx ends or the
// stream does: a 410 as an *APIError whose Head and Generation name where to
// resume.
func (c *Client) Watch(ctx context.Context, kinds []string, from int64, generation string,
	onOpen func(bookmark int64, generation string) error, onChange func(Change) error,
) error {
	q := url.Values{"watch": {"1"}, "kinds": {strings.Join(kinds, ",")}}
	if from > 0 {
		q.Set("from", strconv.FormatInt(from, 10))
		q.Set("generation", generation)
	}
	if c.base() == "" {
		return ErrUnauthorized
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/api/v1/changes", q), nil)
	if err != nil {
		return errors.New("building the watch request failed")
	}
	if tok := c.token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	req.Header.Set("Accept", "application/x-ndjson")
	// A stream has no overall deadline; idleness is watched below.
	stream := &http.Client{Transport: c.http.Transport}
	resp, err := stream.Do(req) //nolint:bodyclose // closed by the defer below; the reader goroutine only reads it
	if err != nil {
		return redact(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return decodeError(resp)
	}

	lines := make(chan []byte)
	readErr := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64<<10), 4<<20)
		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)
			select {
			case lines <- line:
			case <-ctx.Done():
				return
			}
		}
		readErr <- sc.Err()
	}()

	idle := time.NewTimer(StreamIdle)
	defer idle.Stop()
	opened := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-idle.C:
			return errors.New("the change feed went silent")
		case err := <-readErr:
			if err == nil {
				return errors.New("the change feed ended")
			}
			return redact(err)
		case line := <-lines:
			idle.Reset(StreamIdle)
			if len(strings.TrimSpace(string(line))) == 0 {
				continue
			}
			var f frame
			if err := json.Unmarshal(line, &f); err != nil {
				return fmt.Errorf("reading the change feed: %w", err)
			}
			switch {
			case f.Error != nil:
				if f.Error.Code == "auth" {
					return ErrUnauthorized
				}
				return f.Error
			case f.Bookmark != nil:
				opened = true
				if err := onOpen(*f.Bookmark, f.Generation); err != nil {
					return err
				}
			case f.Seq > 0:
				if !opened {
					return errors.New("the change feed sent a row before its bookmark")
				}
				if err := onChange(f.Change); err != nil {
					return err
				}
			}
		}
	}
}
