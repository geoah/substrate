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

// A fetched closure is kept by its digest: the approval page plans on
// every read, and a published version's bytes never change, so the second
// read of one costs no fetch.
func TestAFetchedClosureIsReadOncePerDigest(t *testing.T) {
	body := []byte("kind: substrate.reamde.dev/core/package\n")
	sum := sha256.Sum256(body)
	fetches := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches++
		_, _ = w.Write(body)
	}))
	defer server.Close()
	r := NewReader(Options{Client: server.Client()})
	v := AuthorityPackageVersion{Version: 1, Closure: server.URL + "/v1.yaml", SHA256: hex.EncodeToString(sum[:])}
	for range 3 {
		got, err := r.Closure(context.Background(), Source{}, v)
		if err != nil || string(got) != string(body) {
			t.Fatalf("closure = %q, %v", got, err)
		}
	}
	if fetches != 1 {
		t.Fatalf("fetched %d times, want once", fetches)
	}
}

// A document may say where its publisher moved (decision record 0293): read
// as another authority, and left out as anything else, the document still
// read.
func TestAnAuthorityDocumentSaysWhereItMoved(t *testing.T) {
	doc, err := ParseAuthorityDocument([]byte(`{"authority":"old.example.com","movedTo":"new.example.com"}`), "old.example.com")
	if err != nil || doc.MovedTo != "new.example.com" {
		t.Fatalf("parse: %+v %v", doc, err)
	}
	for _, moved := range []string{"not a name", "~", "old.example.com"} {
		doc, err := ParseAuthorityDocument([]byte(`{"authority":"old.example.com","movedTo":"`+moved+`"}`), "old.example.com")
		if err != nil || doc.MovedTo != "" {
			t.Errorf("movedTo %q read as %+v (%v), want the document read without it", moved, doc, err)
		}
	}
}
