package api

// A blob carries an optional NAME beside its optional mime type. The door
// takes it two ways — `?name=` and a Content-Disposition filename — and says
// it back on the read, so a browser save keeps the filename it uploaded.

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// echoBlobDS is a blob store that remembers what the door told it and hands
// the same manifest back on the read.
type echoBlobDS struct {
	*fakeDataset
	got substrate.BlobUpload
}

func (b *echoBlobDS) PutBlob(_ context.Context, _ substrate.Actor, up substrate.BlobUpload, data []byte, _ string) (*substrate.BlobInfo, error) {
	b.got = up
	return &substrate.BlobInfo{
		Digest: substrate.BlobDigestPrefix + strings.Repeat("b", 64),
		Size:   int64(len(data)), Name: up.Name, MediaType: up.MediaType,
		Status: substrate.BlobStored,
	}, nil
}

func (b *echoBlobDS) GetBlob(_ context.Context, _ string) (*substrate.BlobInfo, []byte, error) {
	return &substrate.BlobInfo{
		Digest: substrate.BlobDigestPrefix + strings.Repeat("b", 64),
		Size:   7, Name: b.got.Name, MediaType: b.got.MediaType,
		Status: substrate.BlobStored,
	}, []byte("payload"), nil
}

var _ substrate.BlobStore = (*echoBlobDS)(nil)

func putNamedBlob(t *testing.T, ds *echoBlobDS, target string, header map[string]string) *substrate.BlobInfo {
	t.Helper()
	h := &handler{}
	ctx := withRequestAuth(context.Background(), ds, substrate.TokenInfo{}, substrate.ActorAPI)
	req := httptest.NewRequest(http.MethodPut, target, strings.NewReader("payload")).WithContext(ctx)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.putBlob(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT %s = %d, want 201: %s", target, rec.Code, rec.Body)
	}
	var info substrate.BlobInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return &info
}

func TestBlobNameComesFromTheQueryOrTheDisposition(t *testing.T) {
	base := "/api/" + APIVersion + "/blobs"
	for _, tc := range []struct {
		name   string
		target string
		header map[string]string
		want   string
	}{
		{"query", base + "?name=invoice.pdf", map[string]string{"Content-Type": "application/pdf"}, "invoice.pdf"},
		{"disposition", base, map[string]string{
			"Content-Type":        "application/pdf",
			"Content-Disposition": `attachment; filename="invoice.pdf"`,
		}, "invoice.pdf"},
		// Said twice, the query wins: it is the one the caller had to type.
		{"query wins", base + "?name=chosen.pdf", map[string]string{
			"Content-Disposition": `attachment; filename="other.pdf"`,
		}, "chosen.pdf"},
		// A name is OPTIONAL — a plain PUT is still a blob.
		{"absent", base, map[string]string{"Content-Type": "text/plain"}, ""},
		// An unparseable header is not a name; it is not an error either.
		{"junk disposition", base, map[string]string{"Content-Disposition": "!!!"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ds := &echoBlobDS{fakeDataset: newFakeDataset("geoah")}
			info := putNamedBlob(t, ds, tc.target, tc.header)
			if ds.got.Name != tc.want {
				t.Fatalf("store saw name %q, want %q", ds.got.Name, tc.want)
			}
			if info.Name != tc.want {
				t.Fatalf("manifest name %q, want %q", info.Name, tc.want)
			}
		})
	}
}

// The mime type is optional both ways: a PUT with no Content-Type stores
// none, and the read falls back to application/octet-stream rather than
// claiming a type nobody declared.
func TestBlobMediaTypeIsOptional(t *testing.T) {
	ds := &echoBlobDS{fakeDataset: newFakeDataset("geoah")}
	h := &handler{}
	ctx := withRequestAuth(context.Background(), ds, substrate.TokenInfo{}, substrate.ActorAPI)

	req := httptest.NewRequest(http.MethodPut, "/api/"+APIVersion+"/blobs",
		strings.NewReader("payload")).WithContext(ctx)
	req.Header.Del("Content-Type")
	rec := httptest.NewRecorder()
	h.putBlob(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("put = %d, want 201: %s", rec.Code, rec.Body)
	}
	if ds.got.MediaType != "" {
		t.Fatalf("store saw mime %q, want none", ds.got.MediaType)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/"+APIVersion+"/blobs/x", nil).WithContext(ctx)
	rec = httptest.NewRecorder()
	h.getBlob(rec, req)
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != "" {
		t.Fatalf("an unnamed blob sent Content-Disposition %q", got)
	}
}

// A named blob says its name back on the read, escaped by the header's own
// rules rather than by string concatenation.
func TestBlobReadSaysItsName(t *testing.T) {
	ds := &echoBlobDS{fakeDataset: newFakeDataset("geoah")}
	const name = `quarterly report".pdf`
	putNamedBlob(t, ds, "/api/"+APIVersion+"/blobs?name="+url.QueryEscape(name),
		map[string]string{"Content-Type": "application/pdf"})

	h := &handler{}
	ctx := withRequestAuth(context.Background(), ds, substrate.TokenInfo{}, substrate.ActorAPI)
	req := httptest.NewRequest(http.MethodGet, "/api/"+APIVersion+"/blobs/x", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.getBlob(rec, req)

	disp, params, err := mime.ParseMediaType(rec.Header().Get("Content-Disposition"))
	if err != nil {
		t.Fatalf("Content-Disposition %q: %v", rec.Header().Get("Content-Disposition"), err)
	}
	if disp != "inline" {
		t.Fatalf("disposition = %q, want inline", disp)
	}
	if params["filename"] != name {
		t.Fatalf("filename = %q, want the name it was stored under", params["filename"])
	}
}
