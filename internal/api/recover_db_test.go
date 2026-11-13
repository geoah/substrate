package api

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// A panic after the status line is out cannot change the status, and an
// envelope appended to a stream would end it looking complete, so the
// response is aborted: the client reads an unexpected end of the body.
//
// Serial: it swaps the process's default logger to read the panic line.
func TestAPanicAfterTheStatusAbortsTheResponse(t *testing.T) {
	env := newDBEnv(t, withSamples("people"), withEngine(engine.WithLogger(slog.New(slog.DiscardHandler))))
	env.wrap = func(ds substrate.Dataset) substrate.Dataset { return panicChangesDataset{ds} }
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	logs := captureLogs(t)

	br, stop := startWatch(t, srv, changesPath+"?watch=1", env.token)
	defer stop()
	if bm := readLine(t, br); bm["bookmark"] == nil {
		t.Fatalf("first line = %v, want the bookmark", bm)
	}
	env.put(t, personKind, "c1", map[string]any{"name": "Ada"})

	done := make(chan error, 1)
	var rest []byte
	go func() {
		var err error
		rest, err = io.ReadAll(br)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("read = %v after %q, want the unexpected EOF of an aborted response", err, rest)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stream was still open after its handler panicked")
	}
	if len(logs.at(slog.LevelError)) != 1 {
		t.Fatalf("ERROR lines = %+v, want the one panic line", logs.at(slog.LevelError))
	}
}
