package api

import (
	"mime"
	"net/http"
	"net/url"
	"testing"
)

// The engine keeps what the door decoded: a blob's name, from `?name=` or a
// Content-Disposition filename, and its mime type come back on the read, the
// name escaped by the header's own rules; a blob stored with neither reads
// back as application/octet-stream with no Content-Disposition.
func TestBlobNameRoundTripsThroughTheStore(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	base := "/api/" + APIVersion + "/blobs"
	read := func(digest string) http.Header {
		t.Helper()
		rec := env.do(t, http.MethodGet, base+"/"+digest, env.token, nil)
		wantStatus(t, rec, http.StatusOK)
		return rec.Header()
	}
	filename := func(header http.Header) string {
		t.Helper()
		disp, params, err := mime.ParseMediaType(header.Get("Content-Disposition"))
		if err != nil {
			t.Fatalf("Content-Disposition %q: %v", header.Get("Content-Disposition"), err)
		}
		if disp != "inline" {
			t.Fatalf("disposition = %q, want inline", disp)
		}
		return params["filename"]
	}

	const name = `quarterly report".pdf`
	queried := blobPut(t, env, base+"?name="+url.QueryEscape(name), "the report", map[string]string{"Content-Type": "application/pdf"})
	if queried.Name != name {
		t.Fatalf("manifest name %q, want %q", queried.Name, name)
	}
	header := read(queried.Digest)
	if got := filename(header); got != name {
		t.Fatalf("filename = %q, want the name it was stored under", got)
	}
	if got := header.Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("Content-Type = %q, want the stored application/pdf", got)
	}

	disposed := blobPut(t, env, base, "the invoice", map[string]string{
		"Content-Type":        "application/pdf",
		"Content-Disposition": `attachment; filename="invoice.pdf"`,
	})
	if disposed.Name != "invoice.pdf" {
		t.Fatalf("manifest name %q, want invoice.pdf", disposed.Name)
	}
	if got := filename(read(disposed.Digest)); got != "invoice.pdf" {
		t.Fatalf("filename = %q, want invoice.pdf", got)
	}

	plain := blobPut(t, env, base, "some bytes", nil)
	if plain.Name != "" || plain.MediaType != "" {
		t.Fatalf("a plain PUT stored name %q and type %q, want neither", plain.Name, plain.MediaType)
	}
	header = read(plain.Digest)
	if got := header.Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", got)
	}
	if got := header.Get("Content-Disposition"); got != "" {
		t.Fatalf("an unnamed blob sent Content-Disposition %q", got)
	}
}
