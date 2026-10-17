package engine

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// A thread's heading is read in a chat list and a push notification, where
// markdown prints as written, so a subject is the plain text a reader would
// see. The reduction is a single linear pass over a bounded prefix of one
// line: the result is cut to threadSubjectRunes anyway, and a message may be
// as large as a request.

// subjectScanBytes bounds how much of a line the reduction reads.
const subjectScanBytes = 4 << 10

// boundedLine is line cut to subjectScanBytes, never inside a rune.
func boundedLine(line string) string {
	if len(line) <= subjectScanBytes {
		return line
	}
	cut := subjectScanBytes
	for cut > 0 && !utf8.RuneStart(line[cut]) {
		cut--
	}
	return line[:cut]
}

// cleanSubject is one line of a message as a subject: plain text, then
// normalized.
func cleanSubject(line string) string {
	return normalizeSubject(plainText(boundedLine(line)))
}

// normalizeSubject collapses whitespace, strips surrounding quotes, and cuts
// to threadSubjectRunes. It reads no markdown.
func normalizeSubject(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("\"'`“”‘’«»", r)
	})
	return cutRunes(s, threadSubjectRunes)
}

// cutRunes cuts s to at most n runes, never inside one.
func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n]))
}

// cleanTitle is the title model's answer as a subject: its first non-blank
// line, reduced to plain text once, without a "Title:" label or a trailing
// period. Where the reduction leaves nothing, the line's own text stands, so
// cleanup never empties an answer. Empty only when the model said nothing.
func cleanTitle(content string) string {
	finish := func(s string) string {
		s = strings.TrimSpace(s)
		s = strings.TrimSpace(strings.TrimPrefix(s, "Title:"))
		return normalizeSubject(strings.TrimRight(normalizeSubject(s), "."))
	}
	for line := range strings.SplitSeq(content, "\n") {
		raw := strings.TrimSpace(boundedLine(line))
		if raw == "" {
			continue
		}
		if s := finish(plainText(raw)); s != "" {
			return s
		}
		return finish(raw)
	}
	return ""
}

// plainText is one line of markdown as the text a reader sees: no heading,
// quote, list or task marker before it; a link or image as its text; an
// autolink as its address; a code span as its contents, verbatim; and
// emphasis and strikethrough without their delimiters. A delimiter that
// opens or closes nothing (`2 * 3`, `snake_case`, `/tmp/_cache`) is text,
// and so is an unmatched bracket or backtick. A line that is only a
// thematic break reads as blank.
func plainText(line string) string {
	if thematicBreak(line) {
		return ""
	}
	rs := []rune(line[blockPrefix(line):])
	brackets, parens := matchPairs(rs, '[', ']'), matchPairs(rs, '(', ')')
	return renderInline(parseInline(rs, 0, len(rs), brackets, parens, matchTicks(rs)))
}

// thematicBreak reports a line of three or more of one of `-`, `*`, `_`,
// with spaces between allowed and nothing else.
func thematicBreak(line string) bool {
	var mark rune
	n := 0
	for _, r := range line {
		switch {
		case r == ' ' || r == '\t':
		case (r == '-' || r == '*' || r == '_') && (mark == 0 || r == mark):
			mark = r
			n++
		default:
			return false
		}
	}
	return n >= 3
}

// blockPrefix is the byte length of the block markers that open line: any
// run of headings, quotes, list items and task boxes, read once from the
// left (`> - [x] item`).
func blockPrefix(line string) int {
	i := 0
	for {
		j := i
		for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
			j++
		}
		if j >= len(line) {
			return i
		}
		next := -1
		switch c := line[j]; {
		case c == '#':
			k := j
			for k < len(line) && line[k] == '#' {
				k++
			}
			if k-j <= 6 && (k == len(line) || line[k] == ' ' || line[k] == '\t') {
				next = k
			}
		case c == '>':
			next = j + 1
			if next < len(line) && line[next] == ' ' {
				next++
			}
		case c == '-' || c == '*' || c == '+':
			if j+1 < len(line) && line[j+1] == ' ' {
				next = j + 2
				if strings.HasPrefix(line[next:], "[ ] ") || strings.HasPrefix(line[next:], "[x] ") || strings.HasPrefix(line[next:], "[X] ") {
					next += 4
				}
			}
		case c >= '0' && c <= '9':
			k := j
			for k < len(line) && k-j < 9 && line[k] >= '0' && line[k] <= '9' {
				k++
			}
			if k+1 < len(line) && (line[k] == '.' || line[k] == ')') && line[k+1] == ' ' {
				next = k + 2
			}
		}
		if next < 0 {
			return i
		}
		i = next
	}
}

// matchPairs maps each open rune to its balanced close and back, in one pass
// with a stack; -1 where a rune has no partner. An escaped rune pairs with
// nothing.
func matchPairs(rs []rune, open, close rune) []int {
	match := make([]int, len(rs))
	var stack []int
	for i := range rs {
		match[i] = -1
		switch {
		case i > 0 && rs[i-1] == '\\':
		case rs[i] == open:
			stack = append(stack, i)
		case rs[i] == close && len(stack) > 0:
			o := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			match[o], match[i] = i, o
		}
	}
	return match
}

// matchTicks maps the start of each backtick run to the start of the next
// run of the same width, the code span's close; -1 where none follows. One
// pass from the right, so an unclosed run costs nothing to look up.
func matchTicks(rs []rune) []int {
	ticks := make([]int, len(rs))
	nextOf := map[int]int{}
	for i := len(rs) - 1; i >= 0; i-- {
		ticks[i] = -1
		if rs[i] != '`' || (i > 0 && rs[i-1] == '`') {
			continue
		}
		n := i
		for n < len(rs) && rs[n] == '`' {
			n++
		}
		if next, ok := nextOf[n-i]; ok {
			ticks[i] = next
		}
		nextOf[n-i] = i
	}
	return ticks
}

// asciiPunct is what a backslash escapes in markdown.
func asciiPunct(r rune) bool {
	return r < utf8.RuneSelf && (unicode.IsPunct(r) || unicode.IsSymbol(r))
}

// inlineTok is one piece of a line: text, or a run of emphasis delimiters
// that may open or close.
type inlineTok struct {
	text     string
	delim    rune // '*', '_' or '~'; 0 for text
	canOpen  bool
	canClose bool
	removed  bool
}

func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// parseInline reads rs[lo:hi] into tokens and pairs the emphasis delimiters.
// A delimiter run opens when the next rune is text and the previous one is
// not a letter or digit, and closes the other way round, so a delimiter
// inside a word or standing alone between spaces is text. A closer pairs
// with the nearest open run of its own rune; what pairs is dropped.
func parseInline(rs []rune, lo, hi int, brackets, parens, ticks []int) []inlineTok {
	var toks []inlineTok
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			toks = append(toks, inlineTok{text: text.String()})
			text.Reset()
		}
	}
	at := func(i int) rune {
		if i < lo || i >= hi {
			return ' '
		}
		return rs[i]
	}
	for i := lo; i < hi; {
		r := rs[i]
		switch {
		case r == '\\' && i+1 < hi && asciiPunct(rs[i+1]):
			text.WriteRune(rs[i+1])
			i += 2
			continue
		case r == '`':
			n := i
			for n < hi && rs[n] == '`' {
				n++
			}
			width := n - i
			end := ticks[i]
			if end >= hi {
				end = -1
			}
			if end < 0 {
				text.WriteString(string(rs[i:n]))
				i = n
				continue
			}
			text.WriteString(string(rs[n:end]))
			i = end + width
			continue
		case r == '!' && i+1 < hi && rs[i+1] == '[' || r == '[':
			open := i
			if r == '!' {
				open = i + 1
			}
			closeAt := brackets[open]
			if closeAt > open && closeAt < hi && closeAt+1 < hi && rs[closeAt+1] == '(' {
				if end := parens[closeAt+1]; end > closeAt+1 && end < hi {
					flush()
					toks = append(toks, parseInline(rs, open+1, closeAt, brackets, parens, ticks)...)
					i = end + 1
					continue
				}
			}
			text.WriteRune(r)
			i++
			continue
		case r == '<':
			if end, ok := autolinkEnd(rs, i, hi); ok {
				text.WriteString(string(rs[i+1 : end]))
				i = end + 1
				continue
			}
		case r == '*' || r == '_' || r == '~':
			n := i
			for n < hi && rs[n] == r {
				n++
			}
			if r == '~' && n-i != 2 {
				text.WriteString(string(rs[i:n]))
				i = n
				continue
			}
			before, after := at(i-1), at(n)
			flush()
			toks = append(toks, inlineTok{
				text: string(rs[i:n]), delim: r,
				canOpen:  !unicode.IsSpace(after) && !wordRune(before),
				canClose: !unicode.IsSpace(before) && !wordRune(after),
			})
			i = n
			continue
		}
		text.WriteRune(r)
		i++
	}
	flush()
	open := map[rune][]int{}
	for k := range toks {
		t := &toks[k]
		if t.delim == 0 {
			continue
		}
		if t.canClose {
			if stack := open[t.delim]; len(stack) > 0 {
				o := stack[len(stack)-1]
				open[t.delim] = stack[:len(stack)-1]
				toks[o].removed, t.removed = true, true
				continue
			}
		}
		if t.canOpen {
			open[t.delim] = append(open[t.delim], k)
		}
	}
	return toks
}

// autolinkEnd is the index of the `>` that closes an autolink opened at i:
// `<scheme:rest>` or `<user@host>`, with no space or `<` inside.
func autolinkEnd(rs []rune, i, hi int) (int, bool) {
	colon, at := -1, -1
	for k := i + 1; k < hi; k++ {
		switch r := rs[k]; {
		case r == '>':
			body := rs[i+1 : k]
			if colon > 0 && unicode.IsLetter(body[0]) {
				for _, s := range body[:colon] {
					if !wordRune(s) && !strings.ContainsRune("+.-", s) {
						return 0, false
					}
				}
				return k, true
			}
			return k, at > 0 && at < len(body)-1
		case r == '<' || unicode.IsSpace(r):
			return 0, false
		case r == ':' && colon < 0:
			colon = k - i - 1
		case r == '@' && at < 0:
			at = k - i - 1
		}
	}
	return 0, false
}

func renderInline(toks []inlineTok) string {
	var b strings.Builder
	for _, t := range toks {
		if !t.removed {
			b.WriteString(t.text)
		}
	}
	return b.String()
}
