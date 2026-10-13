//go:build unix

package main

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/synth"
)

// An interrupted load lets the runner clean up: it interrupts the runner
// instead of killing it, so the runner quiesces the account, stops its mock
// and removes its workdir before the loader removes the config that holds
// the repository token. The stand-in runner traps the interrupt and takes a
// moment over its cleanup, as the real one does. The signal goes to the
// loader's whole process group, as a terminal's Ctrl-C does, and the runner
// leads a group of its own, so the one interrupt it gets is the loader's.
func TestInterruptLetsTheRunnerCleanUp(t *testing.T) {
	if _, err := exec.LookPath("ps"); err != nil {
		t.Skip("ps is not on PATH")
	}
	tmp := t.TempDir()
	dir := generated(t, testAuthority, synth.Whoop{})
	bin := t.TempDir()
	at := func(name string) string { return filepath.Join(bin, name) }
	python := at("python")
	script := "#!/bin/sh\n" +
		"ps -o pgid= -p $$ > " + at("pgid") + "\n" +
		"ps -o pgid= -p $PPID > " + at("parentpgid") + "\n" +
		"trap 'kill $child 2>/dev/null; sleep 0.3; touch " + at("cleaned") + "; exit 130' INT\n" +
		"sleep 20 &\nchild=$!\n" +
		"touch " + at("started") + "\n" +
		"wait\n"
	if err := os.WriteFile(python, []byte(script), 0o700); err != nil { //nolint:gosec // a script the test runs
		t.Fatal(err)
	}
	ctl := at("substratectl")
	if err := os.WriteFile(ctl, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "load", "-dir", dir, "-server", "http://127.0.0.1:1", "-authority", testAuthority, //nolint:gosec // the test binary itself
		"-providers", "whoop", "-python", python, "-ctl", ctl, "-repo", ".")
	cmd.Env = append(os.Environ(), "SYNTH_TEST_RUN_MAIN=1", "SUBSTRATE_TOKEN="+testToken, "TMPDIR="+tmp)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(at("started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			t.Fatal("the runner never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()

	if _, err := os.Stat(at("cleaned")); err != nil {
		t.Error("the runner was stopped before its cleanup finished")
	}
	pgid, _ := os.ReadFile(at("pgid"))
	parent, _ := os.ReadFile(at("parentpgid"))
	if p := strings.TrimSpace(string(pgid)); p == "" || p == strings.TrimSpace(string(parent)) {
		t.Errorf("the runner is in process group %q, the loader's is %q: a terminal's Ctrl-C reaches it twice", p, strings.TrimSpace(string(parent)))
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "synth-") {
			t.Errorf("the interrupted load left %s, which holds the token", e.Name())
		}
	}
}

// A relative -ctl is resolved against the directory the load runs in: the
// runner starts substratectl from its own working directory, so the path it
// is handed is absolute and names the binary -ctl meant.
func TestRunSyncHandsTheRunnerAnAbsoluteCtl(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := generated(t, testAuthority, synth.Whoop{})
	work := t.TempDir()
	if err := os.MkdirAll(filepath.Join(work, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(work, "bin", "substratectl")
	if err := os.WriteFile(want, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	args := filepath.Join(t.TempDir(), "args")
	python := filepath.Join(t.TempDir(), "python")
	if err := os.WriteFile(python, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+args+"\n"), 0o700); err != nil { //nolint:gosec // a script the test runs
		t.Fatal(err)
	}
	t.Chdir(work)
	c := &client{server: "http://127.0.0.1:1", token: testToken, authority: testAuthority}
	if err := runSync(t.Context(), c, "whoop", filepath.Join(dir, "whoop"), root, "bin/substratectl", python, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(args) //nolint:gosec // the file the stand-in wrote
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got string
	for sc := bufio.NewScanner(f); sc.Scan(); {
		if sc.Text() == "--ctl" && sc.Scan() {
			got = sc.Text()
		}
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("the runner was handed --ctl %q, want an absolute path", got)
	}
	gi, err1 := os.Stat(got)
	wi, err2 := os.Stat(want)
	if err1 != nil || err2 != nil || !os.SameFile(gi, wi) {
		t.Errorf("the runner was handed --ctl %s, want %s", got, want)
	}
}
