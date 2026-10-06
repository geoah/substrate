package engine_test

// A function body's log lines reach the server log as the body writes them.
// They used to ride on the body's response, so a body the runner killed at
// its timeout lost every line it had written: on 2026-10-06 a Gmail sync on an
// arm64 box parked at `runner: invocation exceeded 1m0s` three times with no
// line of its own in the log, and nothing said where its minute went.

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	stallPackage = "stall.test.dev/logs"
	stallFn      = stallPackage + "/stall"
	stallCaller  = stallPackage + "/caller"
)

// stallConnector is a function that logs, then sleeps past its 2 s timeout,
// and one that logs and calls it.
func stallConnector() enginetest.Manifest {
	return enginetest.Manifest{Name: "logs", Authority: stallPackage, Manifests: []map[string]any{
		vocabulary.PackageManifest(stallPackage, 0),
		vocabulary.ActorManifest(stallPackage, vocabulary.PackageActor(stallPackage)),
		vocabulary.FunctionManifest(stallPackage, "stall", map[string]any{
			"description": "logs a line, then sleeps past its timeout",
			"runtime":     vocabulary.RuntimePython,
			"timeout":     "PT2S",
			"source": "import time\n" +
				"def main(input, host):\n" +
				"    host.log(\"stall: before the sleep\")\n" +
				"    print(\"stall: printed before the sleep\")\n" +
				"    time.sleep(30)\n" +
				"    host.log(\"stall: after the sleep\")\n" +
				"    return {}\n",
		}),
		vocabulary.FunctionManifest(stallPackage, "caller", map[string]any{
			"description": "logs a line, then calls the stall",
			"runtime":     vocabulary.RuntimePython,
			"timeout":     "PT10S",
			"permissions": map[string]any{"call": []any{stallFn}},
			"source": "def main(input, host):\n" +
				"    host.log(\"caller: before the call\")\n" +
				"    host.call(\"" + stallFn + "\", {})\n" +
				"    return {}\n",
		}),
	}}
}

// serverLog keeps every record the engine logs, with the instant it was
// written and its attributes as strings.
type serverLog struct {
	mu      sync.Mutex
	records []loggedRecord
}

type loggedRecord struct {
	at    time.Time
	msg   string
	attrs map[string]string
}

func (l *serverLog) Enabled(context.Context, slog.Level) bool { return true }
func (l *serverLog) WithAttrs([]slog.Attr) slog.Handler       { return l }
func (l *serverLog) WithGroup(string) slog.Handler            { return l }

func (l *serverLog) Handle(_ context.Context, r slog.Record) error {
	rec := loggedRecord{at: time.Now(), msg: r.Message, attrs: map[string]string{}}
	r.Attrs(func(a slog.Attr) bool {
		rec.attrs[a.Key] = a.Value.String()
		return true
	})
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, rec)
	return nil
}

// find is the first record with this message whose attributes include every
// pair in want.
func (l *serverLog) find(msg string, want map[string]string) (loggedRecord, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, rec := range l.records {
		if rec.msg != msg {
			continue
		}
		match := true
		for k, v := range want {
			if rec.attrs[k] != v {
				match = false
				break
			}
		}
		if match {
			return rec, true
		}
	}
	return loggedRecord{}, false
}

func TestAFunctionKilledAtItsTimeoutKeepsItsLogLines(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logs := &serverLog{}
	svc, err := engine.OpenForTest(t, ctx, engine.MigratedDSN(t),
		engine.WithDataRoot(t.TempDir()), engine.WithLogger(slog.New(logs)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatal(err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := enginetest.Install(ctx, ds, owner, stallConnector()); err != nil {
		t.Fatalf("install: %v", err)
	}

	// expectKilled calls fn, which must fail at the stall's timeout, and
	// answers the stall's own lines: each in the server log under one
	// invocation id, written while the body still ran, and followed by the
	// failure under the same id.
	expectKilled := func(t *testing.T, fn string) {
		t.Helper()
		_, _, err := ds.CallFunction(ctx, substrate.ActorAPI, fn, map[string]any{})
		if err == nil || !strings.Contains(err.Error(), "invocation exceeded 2s") {
			t.Fatalf("call %s: err = %v, want the stall's timeout", fn, err)
		}
		line, ok := logs.find("substrate: function log", map[string]string{
			"function": stallFn, "line": "stall: before the sleep",
		})
		if !ok {
			t.Fatalf("the stall's line before its sleep is not in the server log")
		}
		if line.attrs["invocation"] == "" {
			t.Fatalf("the stall's line names no invocation: %v", line.attrs)
		}
		if _, ok := logs.find("substrate: function log", map[string]string{
			"function": stallFn, "line": "[stdout] stall: printed before the sleep",
			"invocation": line.attrs["invocation"],
		}); !ok {
			t.Fatalf("the stall's print is not in the server log under its invocation")
		}
		failed, ok := logs.find("substrate: function invocation failed", map[string]string{
			"function": stallFn, "invocation": line.attrs["invocation"],
		})
		if !ok || !strings.Contains(failed.attrs["error"], "invocation exceeded 2s") {
			t.Fatalf("no failure line for invocation %s: %+v", line.attrs["invocation"], failed)
		}
		// Written as the body wrote it, the line is a timeout ahead of the
		// failure; one that waited for the body's end would come with it.
		if ahead := failed.at.Sub(line.at); ahead < time.Second {
			t.Fatalf("the line reached the log %s before the failure, want it written while the body ran", ahead)
		}
		if _, ok := logs.find("substrate: function log", map[string]string{"line": "stall: after the sleep"}); ok {
			t.Fatal("the stall logged past its timeout")
		}
	}

	t.Run("a delivery", func(t *testing.T) {
		expectKilled(t, stallFn)
	})
	logs.mu.Lock()
	logs.records = nil
	logs.mu.Unlock()
	t.Run("a call from another body", func(t *testing.T) {
		expectKilled(t, stallCaller)
		if _, ok := logs.find("substrate: function log", map[string]string{
			"function": stallCaller, "line": "caller: before the call",
		}); !ok {
			t.Fatal("the caller's line is not in the server log")
		}
	})
}
