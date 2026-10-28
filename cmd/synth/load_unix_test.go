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
	dir := generated(t, synth.Whoop{})
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
	if err := os.WriteFile(python, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	ctl := at("substratectl")
	if err := os.WriteFile(ctl, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f := newFakeSubstrate(t)
	cmd := exec.Command(os.Args[0], "load", "-dir", dir, "-server", f.ts.URL, "-authority", testAuthority,
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
	dir := generated(t, synth.Whoop{})
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
	if err := os.WriteFile(python, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+args+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)
	c := newFakeSubstrate(t).client()
	if err := runSync(t.Context(), c, "whoop", filepath.Join(dir, "whoop"), root, "bin/substratectl", python, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(args)
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

// whoopAccount and whoopConfig are the account and config kinds the
// generated e2e.json names, and synthClient the client id it writes.
const (
	whoopAccount = "providers.substrate.reamde.dev/whoop/account"
	whoopConfig  = "providers.substrate.reamde.dev/whoop/config"
	synthClient  = "e2e-client-id"
)

// standInRunner writes a stand-in for the runner's interpreter that runs
// body as a shell script and touches the answered marker when it starts,
// and a substratectl for -ctl to name. It answers -python, -ctl and the
// marker.
func standInRunner(t *testing.T, body string) (python, ctl, ran string) {
	t.Helper()
	bin := t.TempDir()
	python, ctl, ran = filepath.Join(bin, "python"), filepath.Join(bin, "substratectl"), filepath.Join(bin, "ran")
	if err := os.WriteFile(python, []byte("#!/bin/sh\ntouch "+ran+"\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ctl, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return python, ctl, ran
}

// checkoutRoot is the substrate checkout the tests run in.
func checkoutRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// The runner purges a WHOOP installed from the catalog before it installs
// its mock-wired copy, which tombstones the owner's WHOOP records and
// revokes the account. The load refuses such a repository before the
// runner starts and writes nothing.
func TestLoadRefusesACatalogInstalledProvider(t *testing.T) {
	f := newFakeSubstrate(t)
	f.packages["providers.substrate.reamde.dev/whoop"] = map[string]any{"source": "published", "version": float64(3)}
	f.kinds[whoopAccount] = true
	f.accounts[whoopAccount] = []string{"owner-whoop"}
	python, ctl, ran := standInRunner(t, "exit 0\n")
	dir := generated(t, synth.Whoop{})
	out, err := f.runLoad(t, "-dir", dir, "-providers", "whoop", "-python", python, "-ctl", ctl, "-repo", checkoutRoot(t))
	if err == nil || !strings.Contains(err.Error(), "installed from the catalog") {
		t.Fatalf("a load over a catalog-installed WHOOP = %v, want a refusal\n%s", err, out)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Error("the runner started over a catalog-installed WHOOP")
	}
	for _, r := range f.seen() {
		if !strings.HasPrefix(r, "GET ") {
			t.Errorf("the refused load sent %s", r)
		}
	}
}

// A WHOOP installed otherwise is applied over, so an account synth did not
// connect would dial a mock that stops with the run: the load refuses it,
// and runs where the only account is the one the runner connects.
func TestLoadRefusesAccountsItDidNotConnect(t *testing.T) {
	f := newFakeSubstrate(t)
	f.packages["providers.substrate.reamde.dev/whoop"] = map[string]any{"source": "installed"}
	f.kinds[whoopAccount] = true
	f.accounts[whoopAccount] = []string{"e2e", "owner-whoop"}
	f.configs[whoopConfig] = map[string]string{"default": synthClient}
	python, ctl, ran := standInRunner(t, "exit 0\n")
	dir := generated(t, synth.Whoop{})
	args := []string{"-dir", dir, "-providers", "whoop", "-python", python, "-ctl", ctl, "-repo", checkoutRoot(t)}
	out, err := f.runLoad(t, args...)
	if err == nil || !strings.Contains(err.Error(), "owner-whoop") || strings.Contains(err.Error(), "e2e,") {
		t.Fatalf("a load beside the owner's account = %v, want a refusal naming owner-whoop alone\n%s", err, out)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("the runner started beside an account synth did not connect")
	}
	f.accounts[whoopAccount] = []string{"e2e"}
	if out, err := f.runLoad(t, args...); err != nil {
		t.Fatalf("a load over synth's own account: %v\n%s", err, out)
	}
	if _, err := os.Stat(ran); err != nil {
		t.Error("the runner did not start over synth's own account")
	}
}

// An account named like the runner's is no evidence synth connected it: an
// owner who installed WHOOP by hand and named their account `e2e` has their
// client config overwritten and the account reconnected through the mock.
// The load refuses unless the client config holds the client id the runner
// writes, which no real WHOOP app has.
func TestLoadRefusesARunnerAccountWithoutSynthsClient(t *testing.T) {
	f := newFakeSubstrate(t)
	f.packages["providers.substrate.reamde.dev/whoop"] = map[string]any{"source": "installed"}
	f.kinds[whoopAccount] = true
	f.accounts[whoopAccount] = []string{"e2e"}
	f.configs[whoopConfig] = map[string]string{"default": "the-owners-whoop-app"}
	python, ctl, ran := standInRunner(t, "exit 0\n")
	dir := generated(t, synth.Whoop{})
	args := []string{"-dir", dir, "-providers", "whoop", "-python", python, "-ctl", ctl, "-repo", checkoutRoot(t)}
	refused := func(why string) {
		t.Helper()
		out, err := f.runLoad(t, args...)
		if err == nil || !strings.Contains(err.Error(), whoopConfig) {
			t.Fatalf("a load over %s = %v, want a refusal naming %s\n%s", why, err, whoopConfig, out)
		}
		if _, err := os.Stat(ran); err == nil {
			t.Fatalf("the runner started over %s", why)
		}
		for _, r := range f.seen() {
			if !strings.HasPrefix(r, "GET ") {
				t.Fatalf("the refused load over %s sent %s", why, r)
			}
		}
	}
	refused("the owner's own WHOOP app")
	f.configs[whoopConfig] = map[string]string{}
	refused("an account with no client config")

	// The copy an earlier load left: its client config holds the runner's id.
	f.configs[whoopConfig] = map[string]string{"default": synthClient}
	if out, err := f.runLoad(t, args...); err != nil {
		t.Fatalf("a reload over synth's own install: %v\n%s", err, out)
	}
	if _, err := os.Stat(ran); err != nil {
		t.Error("the runner did not start over synth's own install")
	}
}

// The runner runs in a temporary directory of its own, so a relative -dir
// (the default, .dev/synth) reaches it as an absolute recordings path that
// names the generated recordings.
func TestLoadHandsTheRunnerRecordingsItCanFind(t *testing.T) {
	f := newFakeSubstrate(t)
	python, ctl, _ := standInRunner(t, `while [ $# -gt 0 ]; do
  if [ "$1" = --recordings ]; then
    [ -f "$2/GET_v2_cycle.json" ] || { echo "no recordings at $2" >&2; exit 3; }
    exit 0
  fi
  shift
done
echo "no --recordings" >&2
exit 4
`)
	root := checkoutRoot(t)
	dir := generated(t, synth.Whoop{})
	t.Chdir(filepath.Dir(dir))
	out, err := f.runLoad(t, "-dir", filepath.Base(dir), "-providers", "whoop", "-python", python, "-ctl", ctl, "-repo", root)
	if err != nil {
		t.Fatalf("a load from a relative -dir: %v\n%s", err, out)
	}
}
