package engine

import (
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// Every place a record holds a name (recordNamePositions) has the publish's
// answer, moves, stops or history, as every dialect key has its class: a
// position added later is a choice its author makes. A MOVES position says
// how a name moves; a STOPS or a HISTORY one says why.
func TestEveryNamePositionHasAPublishAnswer(t *testing.T) {
	for kind, positions := range recordNamePositions {
		answers := publishRecordPositions[kind]
		if _, whole := answers[recordWhole]; whole {
			continue
		}
		for _, p := range positions {
			path := strings.Join(p.path, "/")
			if _, ok := answers[path]; !ok {
				t.Errorf("%s holds a name at %s, which the publish gives no answer: add it to publishRecordPositions", kind, path)
			}
		}
	}
	for kind, answers := range publishRecordPositions {
		for path, p := range answers {
			switch p.answer {
			case vocabulary.PublishMoves:
				if (p.names == nil) == (p.value == nil) {
					t.Errorf("%s %s moves, and names exactly one of names and value: it names %v and %v", kind, path, p.names != nil, p.value != nil)
				}
				if path == recordWhole || path == recordID {
					t.Errorf("%s %s moves: a whole record or an id is answered as history or a stop, never moved in place", kind, path)
				}
			case vocabulary.PublishStops, vocabulary.PublishHistory:
				if p.why == "" {
					t.Errorf("%s %s is answered %s with no reason", kind, path, p.answer)
				}
			default:
				t.Errorf("%s %s has no answer", kind, path)
			}
		}
	}
}
