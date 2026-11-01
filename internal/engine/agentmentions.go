package engine

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// maxReplyMentions bounds what one reply stamps: each mention costs an
// existence read at settle, and a reply that links more than this is a list
// no reader checks one by one.
const maxReplyMentions = 32

// mentionLink is one Markdown record link: `[text](substrate://<path>)`, the
// spelling decision record 0137 chose, or the legacy `ref:<path>`, with an
// optional angle-bracketed target and title. A label holds brackets only
// escaped, so a nested link is not read as one.
var mentionLink = regexp.MustCompile(
	`\[((?:\\.|[^\[\]\\\n])*)\]\(\s*<?(?i:substrate://|ref:)([^\s()<>]+)>?(?:\s+"[^"\n]*")?\s*\)`)

// replyMentions reads the record links of a reply, in order of first
// appearance, one per record. Fenced blocks and code spans are skipped: a
// link there is an example, not a claim. A target that is no record path is
// skipped too.
func replyMentions(reply string) []substrate.AgentMention {
	var out []substrate.AgentMention
	seen := map[string]bool{}
	fence := ""
	for _, line := range strings.Split(reply, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]+" \t\r") == "" {
				fence = ""
			}
			continue
		}
		if f := openingFence(trimmed); f != "" {
			fence = f
			continue
		}
		for _, m := range mentionLink.FindAllStringSubmatch(blankCodeSpans(line), -1) {
			kind, id, ok := vocabulary.SplitRecordPath(m[2])
			if !ok || seen[kind+"/"+id] {
				continue
			}
			seen[kind+"/"+id] = true
			text := strings.TrimSpace(unescapeMarkdown(m[1]))
			if text == "" {
				text = id
			}
			out = append(out, substrate.AgentMention{Kind: kind, ID: id, Text: text})
			if len(out) == maxReplyMentions {
				return out
			}
		}
	}
	return out
}

// openingFence is the fence a line opens (three or more backticks or
// tildes), or "".
func openingFence(trimmed string) string {
	for _, c := range []string{"`", "~"} {
		n := len(trimmed) - len(strings.TrimLeft(trimmed, c))
		if n >= 3 {
			return strings.Repeat(c, n)
		}
	}
	return ""
}

// blankCodeSpans replaces every code span of a line with spaces, keeping the
// offsets: a span opens with a run of backticks and closes at the next run
// of the same length. An unclosed run is literal text.
func blankCodeSpans(line string) string {
	b := []byte(line)
	for i := 0; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		n := 0
		for i+n < len(b) && b[i+n] == '`' {
			n++
		}
		closeAt := -1
		for j := i + n; j < len(b); {
			if b[j] != '`' {
				j++
				continue
			}
			m := 0
			for j+m < len(b) && b[j+m] == '`' {
				m++
			}
			if m == n {
				closeAt = j + m
				break
			}
			j += m
		}
		if closeAt < 0 {
			i += n
			continue
		}
		for k := i; k < closeAt; k++ {
			b[k] = ' '
		}
		i = closeAt
	}
	return string(b)
}

// unescapeMarkdown drops the backslash of a backslash escape.
func unescapeMarkdown(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", s[i+1]) >= 0 {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// stampMentions reads the final reply's record links onto its assistant row
// and returns them for the run's result. A mention is missing when no live
// record has its kind and id (a merged-away id resolves to the record it
// became), and unread when no message the model saw carried it. A failed
// existence read leaves the mention unmarked: the stamp is evidence, and a
// guess is not.
func (l *agentLoop) stampMentions(ctx context.Context, props map[string]any, reply string, seen []llm.Message) []substrate.AgentMention {
	mentions := replyMentions(reply)
	if len(mentions) == 0 {
		return nil
	}
	stored := make([]any, 0, len(mentions))
	for i := range mentions {
		m := &mentions[i]
		if live, err := l.ds.recordLive(ctx, eref{Kind: m.Kind, ID: m.ID}); err == nil {
			m.Missing = !live
		}
		m.Unread = !carried(seen, m.Kind, m.ID)
		row := map[string]any{"kind": m.Kind, "id": m.ID, "text": m.Text}
		if m.Missing {
			row["missing"] = true
		}
		if m.Unread {
			row["unread"] = true
		}
		stored = append(stored, row)
	}
	props["mentions"] = stored
	return mentions
}

// carried reports whether a message other than the model's own carries the
// record: its path verbatim, or (a tool result is JSON) its id as a string
// beside its kind.
func carried(seen []llm.Message, kind, id string) bool {
	path, quoted := kind+"/"+id, `"`+id+`"`
	for _, msg := range seen {
		if msg.Role == llm.RoleAssistant {
			continue
		}
		if strings.Contains(msg.Content, path) ||
			(strings.Contains(msg.Content, quoted) && strings.Contains(msg.Content, kind)) {
			return true
		}
	}
	return false
}

// recordLive reports whether a live record has the kind and id, following a
// merged-away id to the record it became.
func (ds *dataset) recordLive(ctx context.Context, ref eref) (bool, error) {
	canonical, err := ds.canonicalOf(ctx, ds.db, ref)
	if err != nil {
		return false, err
	}
	var live bool
	err = ds.db.QueryRowContext(ctx,
		`SELECT deleted_at IS NULL FROM records WHERE kind = $1 AND id = $2`,
		canonical.Kind, canonical.ID).Scan(&live)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return live, err
}
