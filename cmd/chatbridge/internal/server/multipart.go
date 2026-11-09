package server

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
)

// multipartBody writes a delivery: the batch as a part with no filename and
// a text media type, which the door hands to the function inline, then each
// file with a filename, which the door stores as a blob.
func multipartBody(batch []byte, media []MediaPart) (io.Reader, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": contract.BatchPart}))
	h.Set("Content-Type", contract.BatchContentType)
	pw, err := w.CreatePart(h)
	if err != nil {
		return nil, "", err
	}
	if _, err := pw.Write(batch); err != nil {
		return nil, "", err
	}
	for _, m := range media {
		name := m.FileName
		if name == "" {
			name = m.Name
		}
		mimeType := m.MimeType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": m.Name, "filename": name}))
		h.Set("Content-Type", mimeType)
		pw, err := w.CreatePart(h)
		if err != nil {
			return nil, "", err
		}
		f, err := m.Open()
		if err != nil {
			return nil, "", fmt.Errorf("opening media %s: %w", m.Name, err)
		}
		_, err = io.Copy(pw, f)
		_ = f.Close()
		if err != nil {
			return nil, "", fmt.Errorf("reading media %s: %w", m.Name, err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &buf, w.FormDataContentType(), nil
}
