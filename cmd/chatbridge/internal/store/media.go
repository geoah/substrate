package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MediaInfo is one stored file. Its id is the multipart part name the
// request carries it under, and is derived from its bytes, so a file stored
// twice is one file.
type MediaInfo struct {
	ID       string
	MimeType string
	FileName string
	Size     int64
}

// mediaID names bytes by their digest.
func mediaID(sum []byte) string {
	return "m" + hex.EncodeToString(sum[:16])
}

// PutMedia stores bytes and returns their id.
func (s *Store) PutMedia(ctx context.Context, data []byte, mimeType, fileName string) (MediaInfo, error) {
	sum := sha256.Sum256(data)
	info := MediaInfo{ID: mediaID(sum[:]), MimeType: mimeType, FileName: fileName, Size: int64(len(data))}
	path := s.MediaPath(info.ID)
	if _, err := os.Stat(path); err != nil {
		tmp, err := os.CreateTemp(s.mediaDir, "put-*")
		if err != nil {
			return MediaInfo{}, err
		}
		if _, err := tmp.Write(data); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			return MediaInfo{}, err
		}
		if err := s.finishMediaFile(tmp, path); err != nil {
			return MediaInfo{}, err
		}
	}
	return info, s.recordMedia(ctx, info)
}

// PutMediaFile moves the file at src into the store and returns its id.
func (s *Store) PutMediaFile(ctx context.Context, src, mimeType, fileName string) (MediaInfo, error) {
	f, err := os.Open(src)
	if err != nil {
		return MediaInfo{}, err
	}
	h := sha256.New()
	n, err := io.Copy(h, f)
	_ = f.Close()
	if err != nil {
		return MediaInfo{}, err
	}
	info := MediaInfo{ID: mediaID(h.Sum(nil)), MimeType: mimeType, FileName: fileName, Size: n}
	path := s.MediaPath(info.ID)
	if _, err := os.Stat(path); err == nil {
		_ = os.Remove(src)
	} else if err := os.Rename(src, path); err != nil {
		return MediaInfo{}, fmt.Errorf("moving the media file into the store: %w", err)
	}
	return info, s.recordMedia(ctx, info)
}

// TempMediaFile is a file in the media directory for a stream to write to
// before PutMediaFile moves it into place, so the move never crosses a
// filesystem.
func (s *Store) TempMediaFile() (*os.File, error) {
	return os.CreateTemp(s.mediaDir, "stream-*")
}

func (s *Store) finishMediaFile(tmp *os.File, path string) error {
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

func (s *Store) recordMedia(ctx context.Context, info MediaInfo) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO media (id, mime_type, file_name, size, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET created_at = excluded.created_at`,
		info.ID, info.MimeType, info.FileName, info.Size, s.now().UnixMilli())
	return err
}

// Media reads one file's row, nil when it is not stored.
func (s *Store) Media(ctx context.Context, id string) (*MediaInfo, error) {
	var info MediaInfo
	err := s.db.QueryRowContext(ctx, `SELECT id, mime_type, file_name, size FROM media WHERE id = ?`, id).
		Scan(&info.ID, &info.MimeType, &info.FileName, &info.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(s.MediaPath(id)); err != nil {
		return nil, nil
	}
	return &info, nil
}

// MediaPath is where a file's bytes live.
func (s *Store) MediaPath(id string) string {
	return filepath.Join(s.mediaDir, filepath.Base(id))
}

// The content URIs the local Matrix side hands out name a stored file.
const contentURIPrefix = "mxc://chatbridge.invalid/"

// ContentURI names a stored file the way bridgev2 expects.
func ContentURI(id string) string { return contentURIPrefix + id }

// ParseContentURI returns the media id a content URI names.
func ParseContentURI(uri string) (string, bool) {
	id, ok := strings.CutPrefix(uri, contentURIPrefix)
	return id, ok && id != ""
}

// CollectMedia deletes the files no pending outbox event names and that
// were last stored before cutoff.
func (s *Store) CollectMedia(ctx context.Context, cutoff time.Time) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM media WHERE created_at < ?`, cutoff.UnixMilli())
	if err != nil {
		return 0, err
	}
	var old []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		old = append(old, id)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, id := range old {
		var pending int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM outbox WHERE ' ' || media || ' ' LIKE ?`, "% "+id+" %").Scan(&pending); err != nil {
			return n, err
		}
		if pending > 0 {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM media WHERE id = ?`, id); err != nil {
			return n, err
		}
		if err := os.Remove(s.MediaPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return n, err
		}
		n++
	}
	return n, nil
}

// Media reads one file's row inside a transaction, nil when it is not
// stored.
func (tx *Tx) Media(ctx context.Context, id string) (*MediaInfo, error) {
	var info MediaInfo
	err := tx.tx.QueryRowContext(ctx, `SELECT id, mime_type, file_name, size FROM media WHERE id = ?`, id).
		Scan(&info.ID, &info.MimeType, &info.FileName, &info.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(tx.s.MediaPath(id)); err != nil {
		return nil, nil
	}
	return &info, nil
}
