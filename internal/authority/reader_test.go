package authority

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/domains"
)

func TestReaderFreshAndTolerant(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, `{"authority":"example.com","future":true,"name":"Publisher"}`)
	}))
	defer server.Close()
	resolver := domains.Static{"_substrate.example.com.": []string{"v=substrate1 doc=" + server.URL}}
	reader := NewReader(Options{Resolver: resolver, Client: server.Client()})
	for _, fresh := range []bool{false, false, true} {
		if _, err := reader.Read(context.Background(), "example.com", fresh); err != nil {
			t.Fatal(err)
		}
	}
	if hits.Load() != 2 {
		t.Fatal(hits.Load())
	}
	delete(resolver, "_substrate.example.com.")
	if _, err := reader.Read(context.Background(), "example.com", false); err == nil {
		t.Fatal("cached document survived removed DNS")
	}
}

func TestAuthorityPackageValidation(t *testing.T) {
	for _, entry := range []string{
		`{"package":"notes","versions":[{"version":0,"closure":"a.yaml","sha256":"` + strings.Repeat("a", 64) + `"}]}`,
		`{"package":"notes","versions":[{"version":1,"closure":"a.yaml","sha256":"00"}]}`,
		`{"package":"notes","addonFor":"bad"}`,
		`{"package":"notes","versions":"wrong"}`,
		`{"package":"notes","versions":null}`,
		`{"package":"notes","package":"other"}`,
	} {
		if _, err := ParseAuthorityDocument([]byte(`{"authority":"example.com","packages":[`+entry+`]}`), "example.com"); err == nil {
			t.Fatal(entry)
		}
	}
}

func TestDevelopmentClosuresConfinedAndPinned(t *testing.T) {
	dir := t.TempDir()
	body := []byte("closure")
	if err := os.WriteFile(filepath.Join(dir, "v1.yaml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	source := Source{FromFile: true, DocURL: "file:" + filepath.Join(dir, "authority.json")}
	sum := sha256.Sum256(body)
	v := AuthorityPackageVersion{Version: 1, Closure: "v1.yaml", SHA256: hex.EncodeToString(sum[:])}
	r := NewReader(Options{})
	if _, err := r.Closure(context.Background(), source, v); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../escape.yaml", filepath.Join(dir, "v1.yaml")} {
		bad := v
		bad.Closure = name
		if _, err := r.Closure(context.Background(), source, bad); err == nil {
			t.Fatal(name)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.yaml")); err != nil {
		t.Fatal(err)
	}
	v.Closure = "link.yaml"
	if _, err := r.Closure(context.Background(), source, v); err == nil {
		t.Fatal("symlink escaped")
	}
	v.Closure = "v1.yaml"
	v.SHA256 = strings.Repeat("a", 64)
	if _, err := r.Closure(context.Background(), source, v); err == nil {
		t.Fatal("wrong digest accepted")
	}
}

func TestRemoteClosureHasABoundedBody(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", MaxClosure+1)))
	}))
	defer server.Close()
	r := NewReader(Options{Client: server.Client()})
	_, err := r.Closure(context.Background(), Source{}, AuthorityPackageVersion{Version: 1, Closure: server.URL + "/closure.yaml", SHA256: strings.Repeat("0", 64)})
	if err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatal(err)
	}
}
