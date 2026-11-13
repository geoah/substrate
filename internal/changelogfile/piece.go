package changelogfile

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
)

// A PIECE is a run of changelog lines the archive holds as one object: whole
// transactions, consecutive seqs, written once and never changed (decision
// 0262). Its name (PieceObjectName) says what it covers and which pass put
// it, and the manifest lists the pieces it selects in order (Manifest.Chain).

// pieceDigits is how many digits each seq of a piece's name has: the same
// fifteen a segment's name has.
const pieceDigits = 15

// Piece is one piece of a chain: the seqs it covers and its key.
type Piece struct {
	First, Last int64
	Key         string
}

// EncodePiece renders lines as one piece: each line and its newline. The
// lines must be consecutive and end a transaction, which is what lets a
// reader hold the piece to its name.
func EncodePiece(lines []Line) ([]byte, error) {
	if len(lines) == 0 {
		return nil, errors.New("changelogfile: a piece holds at least one line")
	}
	var buf bytes.Buffer
	var frame txnFrame
	for i, l := range lines {
		if i > 0 && l.Seq != lines[i-1].Seq+1 {
			return nil, fmt.Errorf("changelogfile: seq %d follows %d in a piece", l.Seq, lines[i-1].Seq)
		}
		if _, err := frame.next(l.Seq, l.Txn); err != nil {
			return nil, err
		}
		buf.Write(l.Bytes)
		buf.WriteByte('\n')
	}
	if frame.open != 0 {
		return nil, ErrTxnIncomplete
	}
	return buf.Bytes(), nil
}

// ErrPieceDamaged is returned by ReadPiece for a piece that is not the run of
// lines its name says: a line that does not verify, a seq out of place, a
// transaction left open, or fewer or more lines than the name covers.
var ErrPieceDamaged = errors.New("changelogfile: the piece is not the lines its name covers")

// ReadPiece reads the piece covering first to last, verifying every line's
// checksum and its place, and hands each entry to fn with its canonical line
// and checksum. A piece is checked whole: fn has seen every line before
// ReadPiece reports that the piece ended short.
func ReadPiece(r io.Reader, first, last int64, fn func(e Entry, line []byte, sum [32]byte) error) error {
	br := bufio.NewReaderSize(r, 1<<20)
	var frame txnFrame
	next := first
	for {
		line, err := readLine(br)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		e, sum, err := Decode(line)
		if err != nil {
			return fmt.Errorf("%w: seq %d: %w", ErrPieceDamaged, next, err)
		}
		if e.Seq != next || e.Seq > last {
			return fmt.Errorf("%w: seq %d where %d belongs", ErrPieceDamaged, e.Seq, next)
		}
		if _, err := frame.next(e.Seq, e.Txn); err != nil {
			return fmt.Errorf("%w: %w", ErrPieceDamaged, err)
		}
		if err := fn(e, line, sum); err != nil {
			return err
		}
		next++
	}
	if next != last+1 || frame.open != 0 {
		return fmt.Errorf("%w: it ends at seq %d, its name says %d", ErrPieceDamaged, next-1, last)
	}
	return nil
}

// readLine reads one newline-terminated line, without its newline, refusing
// one longer than MaxLineBytes and a last line with no newline: a piece is
// written whole, so a line cut short is damage, not a tail to cut.
func readLine(br *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := br.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > MaxLineBytes {
			return nil, fmt.Errorf("%w: a line is longer than %d bytes", ErrPieceDamaged, MaxLineBytes)
		}
		switch {
		case err == nil:
			return line[:len(line)-1], nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(line) == 0:
			return nil, io.EOF
		case errors.Is(err, io.EOF):
			return nil, fmt.Errorf("%w: the last line has no newline", ErrPieceDamaged)
		default:
			return nil, err
		}
	}
}
