package blobbytes_test

// A read hashes what it serves. The bytes under a digest are damaged on disk
// here, the way a disk fault or a hand edit damages them, and both readers
// refuse them naming the digest: ReadAll before it returns anything, and the
// streaming reader before it gives out the blob's last byte.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/geoah/substrate/internal/blobbytes"
)

// storedOnDisk puts data into a fresh fs store and returns the store, the
// digest and the object's file, for a test that damages the file.
func storedOnDisk(t *testing.T, data []byte) (blobbytes.Store, string, string) {
	t.Helper()
	root := t.TempDir()
	b, err := blobbytes.NewFS(root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := b.Repository("ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	digest := put(t, s, data)
	return s, digest, filepath.Join(root, "repositories", "ada.example.com", "blobs", digest)
}

// readByteAtATime drains r one byte per Read, so the count says exactly how
// much of the blob a caller held when the error arrived.
func readByteAtATime(r io.Reader) ([]byte, error) {
	return io.ReadAll(iotest.OneByteReader(r))
}

func wantMismatch(t *testing.T, err error, digest, detail string) {
	t.Helper()
	if !errors.Is(err, blobbytes.ErrDigestMismatch) {
		t.Fatalf("got %v, want ErrDigestMismatch", err)
	}
	if !strings.Contains(err.Error(), digest) || !strings.Contains(err.Error(), detail) {
		t.Fatalf("the refusal %q does not name %s and %q", err, digest, detail)
	}
}

func TestReadAllAndOpenVerifiedServeIntactBytes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, data := range [][]byte{
		[]byte("the bytes, whole"),
		{},
		bytes.Repeat([]byte("0123456789abcdef"), 4096), // past one copy buffer
	} {
		s, digest, _ := storedOnDisk(t, data)
		got, err := blobbytes.ReadAll(ctx, s, digest, int64(len(data)))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("ReadAll of %d intact bytes = (%d bytes, %v)", len(data), len(got), err)
		}
		rc, err := blobbytes.OpenVerified(ctx, s, digest, int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		// iotest.TestReader holds the reader to the io.Reader contract with
		// reads of every size, and to returning exactly data.
		if err := iotest.TestReader(rc, data); err != nil {
			t.Fatalf("the verified reader over %d intact bytes: %v", len(data), err)
		}
		if err := rc.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
}

func TestReadAllAndOpenVerifiedRefuseAFlippedByte(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := []byte("one bit of this will flip on disk")
	s, digest, file := storedOnDisk(t, data)
	flipped := bytes.Clone(data)
	flipped[len(flipped)-1] ^= 0x01
	if err := os.WriteFile(file, flipped, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := blobbytes.ReadAll(ctx, s, digest, int64(len(data)))
	wantMismatch(t, err, digest, "hashes to "+digestOf(flipped))
	if got != nil {
		t.Fatalf("ReadAll handed out %q beside the refusal", got)
	}

	// A byte at a time, the reader gives out everything but the last byte and
	// then the refusal: the chunk that completes the blob is held until the
	// hash checks, so a caller never holds the damaged blob whole.
	rc, err := blobbytes.OpenVerified(ctx, s, digest, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	got, err = readByteAtATime(rc)
	wantMismatch(t, err, digest, "hashes to "+digestOf(flipped))
	if len(got) != len(data)-1 || !bytes.Equal(got, flipped[:len(data)-1]) {
		t.Fatalf("the reader gave out %d bytes before the refusal, want the %d before the last", len(got), len(data)-1)
	}
	// The refusal is sticky: a caller that reads again does not get the
	// withheld chunk.
	if n, err := rc.Read(make([]byte, 64)); n != 0 || !errors.Is(err, blobbytes.ErrDigestMismatch) {
		t.Fatalf("a read after the refusal = (%d, %v)", n, err)
	}

	// With a buffer that holds the whole blob, nothing is given out at all.
	rc2, err := blobbytes.OpenVerified(ctx, s, digest, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc2.Close() }()
	if n, err := rc2.Read(make([]byte, 4096)); n != 0 {
		t.Fatalf("one large read handed out %d damaged bytes (%v)", n, err)
	} else {
		wantMismatch(t, err, digest, "hashes to")
	}
}

func TestReadAllAndOpenVerifiedRefuseTheWrongLength(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := []byte("exactly this long")
	for _, tc := range []struct {
		name   string
		onDisk []byte
		detail string
	}{
		{"truncated", data[:len(data)-3], "holds 14 bytes, its manifest declares 17"},
		{"grown", append(bytes.Clone(data), '!'), "holds more than the 17 bytes its manifest declares"},
		{"emptied", []byte{}, "holds 0 bytes, its manifest declares 17"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, digest, file := storedOnDisk(t, data)
			if err := os.WriteFile(file, tc.onDisk, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := blobbytes.ReadAll(ctx, s, digest, int64(len(data)))
			wantMismatch(t, err, digest, tc.detail)
			if got != nil {
				t.Fatalf("ReadAll handed out %q beside the refusal", got)
			}
			rc, err := blobbytes.OpenVerified(ctx, s, digest, int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = rc.Close() }()
			got, err = readByteAtATime(rc)
			wantMismatch(t, err, digest, tc.detail)
			if len(got) >= len(data) {
				t.Fatalf("the reader gave out %d bytes of a %d-byte blob before the refusal", len(got), len(data))
			}
		})
	}
}

// An empty blob is checked too: an object that should hold nothing and holds
// something is refused on the first read.
func TestOpenVerifiedRefusesBytesUnderTheEmptyDigest(t *testing.T) {
	t.Parallel()
	s, digest, file := storedOnDisk(t, []byte{})
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := blobbytes.ReadAll(context.Background(), s, digest, 0)
	wantMismatch(t, err, digest, "holds more than the 0 bytes")
	if got != nil {
		t.Fatalf("ReadAll handed out %q beside the refusal", got)
	}
}

func TestOpenVerifiedRefusesANegativeSize(t *testing.T) {
	t.Parallel()
	s, digest, _ := storedOnDisk(t, []byte("sized"))
	if _, err := blobbytes.OpenVerified(context.Background(), s, digest, -1); err == nil {
		t.Fatal("a verified read opened with no declared size")
	}
}
