package commands

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// PROMPTS ARE THE DEFAULT AND FLAGS ARE THE SEAM. Every door command asks for
// what it needs — a password never as an argument, where it would land in the
// shell history and the process table — and every prompt has a flag or a
// --*-stdin twin so the same command scripts headlessly (BUILD B8: "the
// operator can run the B3 done-when loop headlessly").
//
// Secrets read from stdin are read ONE LINE AT A TIME through a single reader
// shared by the whole invocation, so a command needing two of them (the
// password change) takes them in order from one heredoc.

// codeDigits is the length of a TOTP code; the substrate accepts nothing else.
const codeDigits = 6

// reader is the invocation's one buffered view of stdin. Sharing it matters:
// a bufio.Reader over stdin reads AHEAD, so a second reader would lose
// whatever the first buffered past its line.
func (a *app) reader() *bufio.Reader {
	if a.stdin == nil {
		a.stdin = bufio.NewReader(a.in)
	}
	return a.stdin
}

// prompt asks on stderr — never stdout, which is the command's output and may
// be piped — and reads one line.
func (a *app) prompt(label string) (string, error) {
	fmt.Fprint(a.errOut, label)
	line, err := a.reader().ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// promptSecret asks for something that must not echo. The QUESTION is always
// asked — a person piping input still deserves to know what was wanted — and
// only the echo suppression depends on there being a terminal to suppress it
// on. Anything else (a pipe, a test, a CI job) reads a line as usual.
func (a *app) promptSecret(label string) (string, error) {
	fmt.Fprint(a.errOut, label)
	f, ok := a.in.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return a.readLine()
	}
	b, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(a.errOut)
	if err != nil {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// readLine takes one line of stdin with no prompt: the --*-stdin path.
func (a *app) readLine() (string, error) {
	line, err := a.reader().ReadString('\n')
	if err != nil && line == "" {
		if errors.Is(err, io.EOF) {
			return "", errors.New("stdin ended before the secret was read")
		}
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// secret resolves one secret: from stdin when the caller asked for that,
// otherwise by prompting. It is the one place the two ways in meet, so no
// command can accidentally support only one.
func (a *app) secret(fromStdin bool, label string) (string, error) {
	if fromStdin {
		return a.readLine()
	}
	return a.promptSecret(label)
}

// newSecret takes a password twice, so a typo cannot become the thing you must
// type tomorrow. The stdin path asks once: a script has no fingers to slip.
func (a *app) newSecret(fromStdin bool, label, confirm string) (string, error) {
	if fromStdin {
		return a.readLine()
	}
	first, err := a.promptSecret(label)
	if err != nil {
		return "", err
	}
	again, err := a.promptSecret(confirm)
	if err != nil {
		return "", err
	}
	if first != again {
		return "", errors.New("the two passwords do not match")
	}
	return first, nil
}

// askCode resolves a TOTP code: the flag, else a prompt. It is validated
// before any request goes out — the substrate rate-limits every auth attempt,
// and a code that cannot be right must not spend one.
func (a *app) askCode(code, label string) (string, error) {
	if code == "" {
		got, err := a.prompt(label)
		if err != nil {
			return "", err
		}
		code = got
	}
	return normalizeCode(code)
}

// normalizeCode strips the spacing authenticators display codes with and
// rejects anything that is not exactly six digits.
func normalizeCode(in string) (string, error) {
	code := strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '\t' {
			return -1
		}
		return r
	}, strings.TrimSpace(in))
	if code == "" {
		return "", fmt.Errorf("a %d-digit code is required: read it from the authenticator holding this account", codeDigits)
	}
	if len(code) != codeDigits || strings.TrimLeft(code, "0123456789") != "" {
		return "", fmt.Errorf("the code must be exactly %d digits, got %q: read the current code from the authenticator holding this account", codeDigits, in)
	}
	return code, nil
}

// askUsername resolves the username: the flag, the context's, else a prompt.
func (a *app) askUsername(username string) (string, error) {
	if username == "" {
		if ctx, err := a.resolveContext(); err == nil {
			username = ctx.Username
		}
	}
	if username != "" {
		return username, nil
	}
	got, err := a.prompt("Username: ")
	if err != nil {
		return "", err
	}
	if got == "" {
		return "", errors.New("a username is required")
	}
	return got, nil
}

// printEnrollment writes the shown-once second factor. The URI is what a
// password manager imports; the bare seed is for the ones that cannot.
func (a *app) printEnrollment(uri, secret string) {
	fmt.Fprintf(a.out, "\n  TOTP enrollment — add it to an authenticator now:\n")
	if uri != "" {
		fmt.Fprintf(a.out, "    otpauth URI: %s\n", uri)
	}
	if secret != "" {
		fmt.Fprintf(a.out, "    secret:      %s\n", secret)
	}
}
