package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// keyFile writes a P-256 .p8 the test generated: never an Apple key.
func keyFile(t *testing.T) string {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	path := filepath.Join(t.TempDir(), "key.p8")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// baseEnv is a configuration the relay starts with; each test unsets or
// changes what it is about.
func baseEnv(t *testing.T) map[string]string {
	t.Helper()
	k := keyFile(t)
	seal := make([]byte, 32)
	_, _ = rand.Read(seal)
	return map[string]string{
		"SUBSTRATE_PUSH_RELAY_APNS_TEAM_ID":              "TEAM000001",
		"SUBSTRATE_PUSH_RELAY_APNS_PRODUCTION_KEY_FILE":  k,
		"SUBSTRATE_PUSH_RELAY_APNS_PRODUCTION_KEY_ID":    "KEY0000001",
		"SUBSTRATE_PUSH_RELAY_APNS_DEVELOPMENT_KEY_FILE": k,
		"SUBSTRATE_PUSH_RELAY_APNS_DEVELOPMENT_KEY_ID":   "KEY0000001",
		"SUBSTRATE_PUSH_RELAY_APNS_TOPICS":               "dev.example.chat, dev.example.tasks",
		"SUBSTRATE_PUSH_RELAY_SEAL_KEY":                  base64.StdEncoding.EncodeToString(seal),
		"SUBSTRATE_PUSH_RELAY_SEAL_KEY_FILE":             "",
		"SUBSTRATE_PUSH_RELAY_SEAL_KEY_PREVIOUS":         "",
		"SUBSTRATE_PUSH_RELAY_SEAL_KEY_PREVIOUS_FILE":    "",
		"SUBSTRATE_PUSH_RELAY_METRICS_ADDRESS":           "",
		"SUBSTRATE_PUSH_RELAY_LIMIT_INSTALL":             "20",
		"SUBSTRATE_PUSH_RELAY_LIMIT_CALLER":              "600",
		"SUBSTRATE_PUSH_RELAY_LIMIT_REGISTER":            "30",
		"SUBSTRATE_PUSH_RELAY_LIMIT_TOTAL":               "12000",
		"SUBSTRATE_PUSH_RELAY_LIMIT_REGISTER_TOTAL":      "3000",
		"SUBSTRATE_PUSH_RELAY_LIMIT_REFUSED_TOTAL":       "3000",
		"PORT":                               "8080",
		"SUBSTRATE_BIND_ADDRESS":             "127.0.0.1",
		"SUBSTRATE_INSECURE_ALLOW_CLEARTEXT": "false",
		"SUBSTRATE_TRUSTED_PROXIES":          "",
		"LOG_LEVEL":                          "info",
		"LOG_FORMAT":                         "text",
	}
}

func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
}

// sealFile writes a seal key to a file, as a deployment mounts a secret.
func sealFile(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "seal")
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The seal keys may come from files, which a deployment mounts.
func TestTheSealKeysMayBeFiles(t *testing.T) {
	env := baseEnv(t)
	current, previous := make([]byte, 32), make([]byte, 32)
	_, _ = rand.Read(current)
	_, _ = rand.Read(previous)
	env["SUBSTRATE_PUSH_RELAY_SEAL_KEY"] = ""
	env["SUBSTRATE_PUSH_RELAY_SEAL_KEY_FILE"] = sealFile(t, base64.StdEncoding.EncodeToString(current))
	env["SUBSTRATE_PUSH_RELAY_SEAL_KEY_PREVIOUS_FILE"] = sealFile(t, base64.StdEncoding.EncodeToString(previous))
	setEnv(t, env)
	c, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(c.sealKey, current) || !bytes.Equal(c.sealPrevious, previous) {
		t.Fatal("the files' keys were not read")
	}
}

// A relay that listens beyond loopback with no trusted proxy is warned
// about at boot.
func TestABindBeyondLoopbackWithNoProxyIsWarnedAbout(t *testing.T) {
	for _, tc := range []struct {
		bind, proxies string
		warn          bool
	}{
		{"127.0.0.1", "", false},
		{"0.0.0.0", "", true},
		{"0.0.0.0", "172.17.0.1", false},
	} {
		env := baseEnv(t)
		env["SUBSTRATE_BIND_ADDRESS"], env["SUBSTRATE_INSECURE_ALLOW_CLEARTEXT"], env["SUBSTRATE_TRUSTED_PROXIES"] = tc.bind, "true", tc.proxies
		setEnv(t, env)
		c, err := loadConfig()
		if err != nil || c.unproxied() != tc.warn {
			t.Errorf("%s with proxies %q: unproxied = %v, %v", tc.bind, tc.proxies, c.unproxied(), err)
		}
	}
}

func TestItStartsFromTheEnvironment(t *testing.T) {
	setEnv(t, baseEnv(t))
	c, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.keys) != 2 || c.keys[0].environment != "production" || len(c.topics) != 2 || c.listen != "127.0.0.1:8080" {
		t.Fatalf("%+v", c)
	}
	// One environment is enough.
	env := baseEnv(t)
	env["SUBSTRATE_PUSH_RELAY_APNS_PRODUCTION_KEY_FILE"], env["SUBSTRATE_PUSH_RELAY_APNS_PRODUCTION_KEY_ID"] = "", ""
	setEnv(t, env)
	if c, err := loadConfig(); err != nil || len(c.keys) != 1 || c.keys[0].environment != "development" {
		t.Fatalf("development alone: %+v, %v", c, err)
	}
}

func TestItRefusesToStartWithoutWhatItNeeds(t *testing.T) {
	for _, tc := range []struct {
		name, variable, value, says string
	}{
		{"no team", "SUBSTRATE_PUSH_RELAY_APNS_TEAM_ID", "", "TEAM_ID"},
		{"a team id that is not one", "SUBSTRATE_PUSH_RELAY_APNS_TEAM_ID", "TEAM 1", "TEAM_ID"},
		{"a key file without its id", "SUBSTRATE_PUSH_RELAY_APNS_PRODUCTION_KEY_ID", "", "PRODUCTION_KEY_ID"},
		{"a key id without its file", "SUBSTRATE_PUSH_RELAY_APNS_DEVELOPMENT_KEY_FILE", "", "DEVELOPMENT_KEY_FILE"},
		{"a key file that is not there", "SUBSTRATE_PUSH_RELAY_APNS_PRODUCTION_KEY_FILE", "/nonexistent/key.p8", "PRODUCTION_KEY_FILE"},
		{"no topic", "SUBSTRATE_PUSH_RELAY_APNS_TOPICS", " , ", "TOPICS"},
		{"a topic that is no bundle id", "SUBSTRATE_PUSH_RELAY_APNS_TOPICS", "dev.example/chat", "TOPICS"},
		{"no seal key", "SUBSTRATE_PUSH_RELAY_SEAL_KEY", "", "SEAL_KEY"},
		{"a passphrase for a seal key", "SUBSTRATE_PUSH_RELAY_SEAL_KEY", "correct horse battery staple", "SEAL_KEY"},
		{"a seal key of 16 bytes", "SUBSTRATE_PUSH_RELAY_SEAL_KEY", base64.StdEncoding.EncodeToString(make([]byte, 16)), "SEAL_KEY"},
		{"a previous seal key of 31 bytes", "SUBSTRATE_PUSH_RELAY_SEAL_KEY_PREVIOUS", base64.StdEncoding.EncodeToString(make([]byte, 31)), "SEAL_KEY_PREVIOUS"},
		{"a limit of 0", "SUBSTRATE_PUSH_RELAY_LIMIT_TOTAL", "0", "LIMIT_TOTAL"},
		{"a total the replay window cannot hold", "SUBSTRATE_PUSH_RELAY_LIMIT_TOTAL", "18182", "LIMIT_TOTAL"},
		{"a seal key file that is not there", "SUBSTRATE_PUSH_RELAY_SEAL_KEY_FILE", "/nonexistent/seal", "SEAL_KEY_FILE"},
		{"every interface without the cleartext switch", "SUBSTRATE_BIND_ADDRESS", "0.0.0.0", "SUBSTRATE_INSECURE_ALLOW_CLEARTEXT"},
		{"a public address without the cleartext switch", "SUBSTRATE_BIND_ADDRESS", "203.0.113.1", "SUBSTRATE_INSECURE_ALLOW_CLEARTEXT"},
		{"no port", "PORT", "0", "PORT"},
		{"a proxy by name", "SUBSTRATE_TRUSTED_PROXIES", "proxy.internal", "SUBSTRATE_TRUSTED_PROXIES"},
		{"a metrics address with no port", "SUBSTRATE_PUSH_RELAY_METRICS_ADDRESS", "localhost", "METRICS_ADDRESS"},
		{"an unknown log level", "LOG_LEVEL", "loud", "LOG_LEVEL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := baseEnv(t)
			env[tc.variable] = tc.value
			setEnv(t, env)
			if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("loadConfig = %v, want a refusal naming %s", err, tc.says)
			}
		})
	}
	t.Run("no environment at all", func(t *testing.T) {
		env := baseEnv(t)
		for _, k := range []string{"PRODUCTION_KEY_FILE", "PRODUCTION_KEY_ID", "DEVELOPMENT_KEY_FILE", "DEVELOPMENT_KEY_ID"} {
			env["SUBSTRATE_PUSH_RELAY_APNS_"+k] = ""
		}
		setEnv(t, env)
		if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "no Apple key") {
			t.Fatalf("loadConfig = %v", err)
		}
	})
	t.Run("a key that is not a .p8", func(t *testing.T) {
		env := baseEnv(t)
		bad := filepath.Join(t.TempDir(), "bad.p8")
		_ = os.WriteFile(bad, []byte("not a key"), 0o600)
		env["SUBSTRATE_PUSH_RELAY_APNS_PRODUCTION_KEY_FILE"] = bad
		setEnv(t, env)
		if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "PRODUCTION_KEY_FILE") {
			t.Fatalf("loadConfig = %v", err)
		}
	})
	t.Run("a seal key and its file both", func(t *testing.T) {
		env := baseEnv(t)
		env["SUBSTRATE_PUSH_RELAY_SEAL_KEY_FILE"] = sealFile(t, env["SUBSTRATE_PUSH_RELAY_SEAL_KEY"])
		setEnv(t, env)
		if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "both set") {
			t.Fatalf("loadConfig = %v", err)
		}
	})
	t.Run("a previous seal key that is the current one", func(t *testing.T) {
		env := baseEnv(t)
		env["SUBSTRATE_PUSH_RELAY_SEAL_KEY_PREVIOUS"] = env["SUBSTRATE_PUSH_RELAY_SEAL_KEY"]
		setEnv(t, env)
		if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "SEAL_KEY_PREVIOUS is the current seal key") {
			t.Fatalf("loadConfig = %v", err)
		}
	})
	t.Run("every interface with the cleartext switch", func(t *testing.T) {
		env := baseEnv(t)
		env["SUBSTRATE_BIND_ADDRESS"], env["SUBSTRATE_INSECURE_ALLOW_CLEARTEXT"] = "0.0.0.0", "true"
		setEnv(t, env)
		if c, err := loadConfig(); err != nil || c.listen != "0.0.0.0:8080" {
			t.Fatalf("loadConfig = %+v, %v", c, err)
		}
	})
}

// fakeApple answers every send with what the test says, over HTTP/2 with
// TLS, and records the requests.
type fakeApple struct {
	srv    *httptest.Server
	mu     sync.Mutex
	paths  []string
	bodies [][]byte
	status int
	reason string
}

func newFakeApple(t *testing.T, status int, reason string) *fakeApple {
	t.Helper()
	a := &fakeApple{status: status, reason: reason}
	a.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("Apple spoken to over %s", r.Proto)
		}
		body, _ := io.ReadAll(r.Body)
		a.mu.Lock()
		a.paths = append(a.paths, r.URL.Path)
		a.bodies = append(a.bodies, body)
		a.mu.Unlock()
		w.Header().Set("apns-id", "00000000-0000-0000-0000-00000000000c")
		w.WriteHeader(a.status)
		if a.reason != "" {
			_ = json.NewEncoder(w).Encode(map[string]string{"reason": a.reason})
		}
	}))
	a.srv.EnableHTTP2 = true
	a.srv.StartTLS()
	t.Cleanup(a.srv.Close)
	return a
}

// pointAppleAt sends both environments to the fakes for the test.
func pointAppleAt(t *testing.T, prod, dev *fakeApple) {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(prod.srv.Certificate())
	pool.AddCert(dev.srv.Certificate())
	oldHosts, oldTLS := appleHosts, appleTLS
	appleHosts = map[string]string{"production": prod.srv.URL, "development": dev.srv.URL}
	appleTLS = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	t.Cleanup(func() { appleHosts, appleTLS = oldHosts, oldTLS })
}

func runCheck(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(context.Background(), append([]string{"check"}, args...), &out, &errOut)
	return code, out.String() + errOut.String()
}

func TestCheckSendsOneAlertPerEnvironmentToNoInstall(t *testing.T) {
	setEnv(t, baseEnv(t))
	prod := newFakeApple(t, http.StatusBadRequest, "BadDeviceToken")
	dev := newFakeApple(t, http.StatusBadRequest, "BadDeviceToken")
	pointAppleAt(t, prod, dev)
	code, out := runCheck(t)
	if code != 0 || !strings.Contains(out, "production: ok") || !strings.Contains(out, "development: ok") || !strings.Contains(out, "does not prove") {
		t.Fatalf("check = %d\n%s", code, out)
	}
	if len(prod.paths) != 1 || len(dev.paths) != 1 {
		t.Fatalf("Apple got %d and %d", len(prod.paths), len(dev.paths))
	}
	var payload struct {
		APS map[string]any `json:"aps"`
	}
	if err := json.Unmarshal(prod.bodies[0], &payload); err != nil || payload.APS["alert"] == nil {
		t.Fatalf("the alert: %s", prod.bodies[0])
	}
	if alert, _ := payload.APS["alert"].(map[string]any); alert["loc-key"] != "SUBSTRATE_PUSH_REPLY" {
		t.Fatalf("the alert's words: %s", prod.bodies[0])
	}
	if _, ok := payload.APS["badge"]; ok {
		t.Fatalf("check sent a badge, which iOS applies to the test install: %s", prod.bodies[0])
	}
}

func TestCheckFailsOnAKeyAppleRefuses(t *testing.T) {
	setEnv(t, baseEnv(t))
	prod := newFakeApple(t, http.StatusForbidden, "InvalidProviderToken")
	dev := newFakeApple(t, http.StatusBadRequest, "BadDeviceToken")
	pointAppleAt(t, prod, dev)
	code, out := runCheck(t)
	if code != 1 || !strings.Contains(out, "production: FAILED") || !strings.Contains(out, "refuses the key") {
		t.Fatalf("check = %d\n%s", code, out)
	}
}

func TestCheckWithADeviceTokenSendsToThatInstallAlone(t *testing.T) {
	setEnv(t, baseEnv(t))
	prod := newFakeApple(t, http.StatusOK, "")
	dev := newFakeApple(t, http.StatusOK, "")
	pointAppleAt(t, prod, dev)
	token := strings.Repeat("ab", 32)
	code, out := runCheck(t, "--device-token", token, "--environment", "development")
	if code != 0 || !strings.Contains(out, "development: ok") {
		t.Fatalf("check = %d\n%s", code, out)
	}
	if len(prod.paths) != 0 || len(dev.paths) != 1 || dev.paths[0] != "/3/device/"+token {
		t.Fatalf("Apple got %v and %v", prod.paths, dev.paths)
	}
	// The install gone fails it.
	dev.status, dev.reason = http.StatusGone, "Unregistered"
	if code, out := runCheck(t, "--device-token", token, "--environment", "development"); code != 1 || !strings.Contains(out, "410 Unregistered") {
		t.Fatalf("check = %d\n%s", code, out)
	}
	for _, args := range [][]string{
		{"--device-token", token},
		{"--device-token", "xyz", "--environment", "development"},
		{"--environment", "staging"},
		{"--topic", "dev.example.other"},
		{"extra"},
	} {
		if code, out := runCheck(t, args...); code != 2 {
			t.Errorf("check %v = %d\n%s", args, code, out)
		}
	}
}

// When Apple does not answer, check says so without the device token,
// which the request's URL holds, through the real client.
func TestCheckWithNoAnswerPrintsNoDeviceToken(t *testing.T) {
	setEnv(t, baseEnv(t))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	oldHosts := appleHosts
	appleHosts = map[string]string{"production": "https://" + addr, "development": "https://" + addr}
	t.Cleanup(func() { appleHosts = oldHosts })
	token := strings.Repeat("feed", 16)
	code, out := runCheck(t, "--device-token", token, "--environment", "development")
	if code != 1 || strings.Contains(out, token) || strings.Contains(out, "/3/device") || !strings.Contains(out, "no answer from Apple: refused") {
		t.Fatalf("check = %d\n%s", code, out)
	}
}

func TestCheckHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"check", "--help"}, &out, &errOut)
	if code != 0 || !strings.Contains(out.String(), "-device-token") || strings.Count(out.String(), "Flags:") != 1 || errOut.Len() != 0 {
		t.Fatalf("check --help = %d\nstdout:\n%s\nstderr:\n%s", code, out.String(), errOut.String())
	}
	if code, msg := runCheck(t, "--nonsense"); code != 2 || !strings.Contains(msg, "flag provided but not defined") {
		t.Fatalf("check --nonsense = %d\n%s", code, msg)
	}
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

// The relay serves: healthy at once, ready once each environment minted a
// provider token, metrics on their own listener and not on the relay's.
func TestItServes(t *testing.T) {
	env := baseEnv(t)
	port, metricsPort := freePort(t), freePort(t)
	env["PORT"], env["SUBSTRATE_PUSH_RELAY_METRICS_ADDRESS"] = port, "127.0.0.1:"+metricsPort
	setEnv(t, env)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	var logs bytes.Buffer
	go func() { done <- run(ctx, nil, io.Discard, &logs) }()
	base := "http://127.0.0.1:" + port
	get := func(url string) int {
		r, err := http.Get(url) //nolint:noctx // a loopback URL the test built.
		if err != nil {
			return 0
		}
		r.Body.Close()
		return r.StatusCode
	}
	deadline := time.Now().Add(10 * time.Second)
	for get(base+"/readyz") != http.StatusOK {
		if time.Now().After(deadline) {
			t.Fatalf("never ready; logs:\n%s", logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if get(base+"/healthz") != http.StatusOK || get(base+"/metrics") != http.StatusNotFound {
		t.Fatal("healthz or the relay's own port serves metrics")
	}
	r, err := http.Get("http://127.0.0.1:" + metricsPort + "/metrics") //nolint:noctx // a loopback URL the test built.
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if !strings.Contains(string(body), `substrate_push_relay_tokens_minted_total{environment="production"} 1`) {
		t.Fatalf("metrics:\n%s", body)
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("serve exited %d; logs:\n%s", code, logs.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("serve did not stop")
	}
}

// The relay faces the internet and holds the publisher's Apple keys, so it
// links none of the server: of this module, only the packages it is built
// from, and no database driver. (database/sql itself arrives with the
// Prometheus runtime collectors, an interface with no driver behind it.)
func TestTheRelayLinksNothingOfTheServers(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatal(err)
	}
	const module = "github.com/geoah/substrate/"
	allowed := map[string]bool{}
	for _, p := range []string{"cmd/substrate-push-relay", "internal/apns", "internal/build", "internal/peer", "internal/pushrelay", "internal/pushwire", "internal/recordid"} {
		allowed[module+p] = true
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasPrefix(dep, module) && !allowed[dep] {
			t.Errorf("the relay links %s", dep)
		}
		for _, driver := range []string{"github.com/jackc/pgx", "github.com/lib/pq", "modernc.org/sqlite", "github.com/mattn/go-sqlite3"} {
			if strings.HasPrefix(dep, driver) {
				t.Errorf("the relay links the database driver %s", dep)
			}
		}
	}
}
