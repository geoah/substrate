package commands

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// helpExample is one command line printed by a command's help.
type helpExample struct {
	command string // the command path whose help prints it
	line    string
}

// helpExamples collects every command line the tree's help prints: each line
// of a command's Example, and each indented line of its Long text that opens
// with `substratectl `. A line ending in `\` continues on the next. A line
// that sets the environment first (`DATABASE_URL=… substratectl …`) is an
// operator command, which names a repository and never a kind, so it is not
// collected.
func helpExamples(root *cobra.Command) []helpExample {
	var out []helpExample
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, text := range []string{c.Example, c.Long} {
			var joined []string
			pending := ""
			for _, l := range strings.Split(text, "\n") {
				if pending != "" {
					l = pending + " " + strings.TrimSpace(l)
					pending = ""
				}
				if strings.HasSuffix(l, `\`) {
					pending = strings.TrimSuffix(l, `\`)
					continue
				}
				joined = append(joined, l)
			}
			for _, l := range joined {
				if !strings.HasPrefix(l, "  ") {
					continue
				}
				if line := strings.TrimSpace(l); strings.HasPrefix(line, "substratectl ") {
					out = append(out, helpExample{command: c.CommandPath(), line: line})
				}
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	return out
}

// shellWords splits one example line the way a shell would for these lines:
// whitespace separates words, single quotes are literal, double quotes keep
// spaces and take `\"`. It stops at a redirect or a pipe, which the shell
// would consume rather than pass on.
func shellWords(line string) ([]string, error) {
	var (
		words   []string
		cur     strings.Builder
		inWord  bool
		quote   rune
		escaped bool
	)
	flush := func() {
		if inWord {
			words = append(words, cur.String())
			cur.Reset()
			inWord = false
		}
	}
	for _, r := range line {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			switch r {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			default:
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == '\\':
			escaped, inWord = true, true
		case r == ' ' || r == '\t':
			flush()
		case strings.ContainsRune("<>|;&", r) && !inWord:
			flush()
			return words, nil
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	flush()
	return words, nil
}

// TestHelpExamplesNameKindsInFull runs every command line the help prints
// against the fake and fails on a bare-name refusal. A kind is named in full
// on every surface (decision record 0101), and the help's examples are the
// first thing a reader pastes: `substratectl get task` in the root help and
// `trigger run classify-page task t9` were both refusals. Every other outcome
// passes, because the fake serves one repository and an example may name a
// record it does not hold; what is held is that the kind got past the
// resolver, client-side for `get`, `patch`, `delete` and `search --kinds`,
// and server-side for `trigger run` and `watch --kinds`, where the fake
// refuses a bare kind as the server does and notes the refusal.
func TestHelpExamplesNameKindsInFull(t *testing.T) {
	// Nothing on PATH: a register example that got as far as the recovery
	// key must not reach a signed-in 1Password on the developer's machine.
	t.Setenv("PATH", t.TempDir())

	examples := helpExamples(newApp("test").rootCommand())
	// The walk must find the examples it is here for, or the loop below
	// passes having run nothing.
	from := map[string]int{}
	for _, ex := range examples {
		from[ex.command]++
	}
	for _, cmd := range []string{"substratectl", "substratectl patch", "substratectl trigger run"} {
		if from[cmd] == 0 {
			t.Fatalf("no help example collected from %q; collected %v", cmd, from)
		}
	}

	for _, ex := range examples {
		refusal, err := bareNameRefusal(t, ex.line)
		if err != nil {
			t.Errorf("%s: %q: %v", ex.command, ex.line, err)
			continue
		}
		if refusal != "" {
			t.Errorf("%s help prints %q, which is refused: %s", ex.command, ex.line, refusal)
		}
	}
}

// bareNameRefusal runs one example line against a fresh fake and returns the
// bare-name refusal it met, or "". The CLI refuses a bare kind itself before
// `get`, `patch`, `delete` and `search --kinds` send anything. The server
// refuses the one `trigger run` and `watch --kinds` send, and on the watch
// stream that refusal is a frame after the opening line, so the fake's note
// of it is read as well as the command's error.
func bareNameRefusal(t *testing.T, line string) (string, error) {
	t.Helper()
	words, err := shellWords(line)
	if err != nil {
		return "", err
	}
	h := newHarness(t)
	h.writeConfig()
	args := words[1:]
	// An example's --server names a host nobody runs; the fake stands in.
	for i, arg := range args {
		switch {
		case arg == "--server" && i+1 < len(args):
			args[i+1] = h.server
		case strings.HasPrefix(arg, "--server="):
			args[i] = "--server=" + h.server
		}
	}
	_, stderr, err := h.run(args...)
	if err != nil && strings.Contains(err.Error()+stderr, "is a bare name") {
		return err.Error(), nil
	}
	if refused := h.fake.refusedBareKinds(); len(refused) > 0 {
		return fmt.Sprintf("the server refuses the bare kind %q", refused[0]), nil
	}
	return "", nil
}

// A bare kind is caught wherever it is refused: by the CLI, by the server
// before it answers, and by the server on the watch stream after it has
// answered 200.
func TestBareNameRefusalIsCaughtOnEverySide(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, tc := range []struct {
		line    string
		refused bool
	}{
		{"substratectl get task", true},
		{"substratectl trigger run classify-page task t9", true},
		{"substratectl watch --kinds task", true},
		{"substratectl watch --kinds geoah.example.com/tasks/task", false},
	} {
		refusal, err := bareNameRefusal(t, tc.line)
		if err != nil {
			t.Fatalf("%q: %v", tc.line, err)
		}
		if got := refusal != ""; got != tc.refused {
			t.Errorf("%q: refused = %v (%q), want %v", tc.line, got, refusal, tc.refused)
		}
	}
}

// The splitter is what turns an example into argv, so a quoting it got wrong
// would hand the CLI arguments no shell would.
func TestShellWordsSplitsAnExampleAsAShellWould(t *testing.T) {
	for _, tc := range []struct {
		line string
		want []string
	}{
		{`substratectl get a.example/p/k -l owner/pinned=true`, []string{"substratectl", "get", "a.example/p/k", "-l", "owner/pinned=true"}},
		{`substratectl patch k t9 --prop description="rack layout"`, []string{"substratectl", "patch", "k", "t9", "--prop", "description=rack layout"}},
		{`substratectl patch k t9 -p '{"properties":{"description":null}}'`, []string{"substratectl", "patch", "k", "t9", "-p", `{"properties":{"description":null}}`}},
		{`substratectl login --repository geoah --password-stdin < password`, []string{"substratectl", "login", "--repository", "geoah", "--password-stdin"}},
	} {
		got, err := shellWords(tc.line)
		if err != nil {
			t.Errorf("shellWords(%q): %v", tc.line, err)
			continue
		}
		if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Errorf("shellWords(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}
