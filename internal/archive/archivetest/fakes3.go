// Package archivetest holds what a test of the archive needs and no server
// does: an S3-compatible service in memory.
package archivetest

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// FakeS3 is the part of the S3 API the archive's store speaks, path-style, in
// memory: put, get, delete and a ListObjectsV2 that pages every two keys. It
// checks that every request is signed and nothing about the signature. It is
// how the S3 store is tested with no bucket, in every suite that runs.
type FakeS3 struct {
	bucket string
	mu     sync.Mutex
	// Objects is what the bucket holds, by key; Pages and Unsigned count the
	// listing pages served and the requests that carried no signature. Read
	// them under Lock.
	Objects  map[string][]byte
	Pages    int
	Unsigned int
	// Down, when set, answers every request 503, as a service that cannot be
	// reached would fail it.
	Down bool
	// stall, while open, makes every request wait until its client gives up
	// or the stall ends, as a service that takes the connection and never
	// answers does.
	stall chan struct{}
}

// SetStall makes every request wait until its client gives up, or answer
// again.
func (f *FakeS3) SetStall(stall bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case stall && f.stall == nil:
		f.stall = make(chan struct{})
	case !stall && f.stall != nil:
		close(f.stall)
		f.stall = nil
	}
}

func NewFakeS3(bucket string) *FakeS3 {
	return &FakeS3{bucket: bucket, Objects: map[string][]byte{}}
}

func (f *FakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	if stall := f.stall; stall != nil {
		f.mu.Unlock()
		select {
		case <-r.Context().Done():
		case <-stall:
		}
		s3Error(w, http.StatusServiceUnavailable, "ServiceUnavailable")
		return
	}
	defer f.mu.Unlock()
	if f.Down {
		s3Error(w, http.StatusServiceUnavailable, "ServiceUnavailable")
		return
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
		f.Unsigned++
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, _ := strings.Cut(path, "/")
	if bucket != f.bucket {
		s3Error(w, http.StatusNotFound, "NoSuchBucket")
		return
	}
	switch {
	case r.Method == http.MethodGet && key == "" && r.URL.Query().Get("list-type") == "2":
		f.list(w, r)
	case r.Method == http.MethodPut && key != "":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			s3Error(w, http.StatusBadRequest, "IncompleteBody")
			return
		}
		if n, err := strconv.Atoi(r.Header.Get("Content-Length")); err != nil || n != len(body) {
			s3Error(w, http.StatusBadRequest, "IncompleteBody")
			return
		}
		f.Objects[key] = body
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && key != "":
		body, ok := f.Objects[key]
		if !ok {
			s3Error(w, http.StatusNotFound, "NoSuchKey")
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	case r.Method == http.MethodDelete && key != "":
		delete(f.Objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		s3Error(w, http.StatusNotImplemented, "NotImplemented")
	}
}

func (f *FakeS3) list(w http.ResponseWriter, r *http.Request) {
	f.Pages++
	q := r.URL.Query()
	prefix, after := q.Get("prefix"), q.Get("continuation-token")
	var keys []string
	for k := range f.Objects {
		if strings.HasPrefix(k, prefix) && k > after {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	type content struct {
		Key  string `xml:"Key"`
		Size int64  `xml:"Size"`
	}
	out := struct {
		XMLName               xml.Name  `xml:"ListBucketResult"`
		Name                  string    `xml:"Name"`
		Prefix                string    `xml:"Prefix"`
		KeyCount              int       `xml:"KeyCount"`
		IsTruncated           bool      `xml:"IsTruncated"`
		NextContinuationToken string    `xml:"NextContinuationToken,omitempty"`
		Contents              []content `xml:"Contents"`
	}{Name: f.bucket, Prefix: prefix}
	const page = 2
	if len(keys) > page {
		out.IsTruncated = true
		out.NextContinuationToken = keys[page-1]
		keys = keys[:page]
	}
	for _, k := range keys {
		out.Contents = append(out.Contents, content{Key: k, Size: int64(len(f.Objects[k]))})
	}
	out.KeyCount = len(out.Contents)
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	_ = xml.NewEncoder(&buf).Encode(out)
	w.Header().Set("Content-Type", "application/xml")
	_, _ = w.Write(buf.Bytes())
}

func s3Error(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, xml.Header+"<Error><Code>"+code+"</Code><Message>"+code+"</Message></Error>")
}

// Lock and Unlock hold the bucket still while a test reads it.
func (f *FakeS3) Lock()   { f.mu.Lock() }
func (f *FakeS3) Unlock() { f.mu.Unlock() }

// SetDown makes every request fail, or succeed again.
func (f *FakeS3) SetDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Down = down
}
