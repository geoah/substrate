package runner

import (
	"context"
	"strings"
	"testing"
)

// A failure is temporary when the body says so, when the network or a
// server's "try later" escapes the body, or when the body runs out of time
// (decision 0266). Any other failure is the body's own, and its text reads
// as it did.
func TestABodySaysWhetherItsFailureIsTemporary(t *testing.T) {
	r := New()
	for i, c := range []struct {
		name, body string
		temporary  bool
		says       string
	}{
		{"host.Temporary", `raise host.Temporary("the provider answered 503")`, true, "runner: the provider answered 503"},
		{"a refused connection", `raise ConnectionRefusedError("connection refused")`, true, "ConnectionRefusedError: connection refused"},
		{"a socket timeout", `raise TimeoutError("timed out")`, true, "TimeoutError: timed out"},
		{"a URL that does not answer", `
    import urllib.error
    raise urllib.error.URLError("no route to host")`, true, "no route to host"},
		{"a server that says try later", `
    import urllib.error
    raise urllib.error.HTTPError("https://api.example.com", 429, "Too Many Requests", {}, None)`, true, "429"},
		{"a library's 503", `
    class Response:
        status_code = 503
    class HTTPError(Exception):
        response = Response()
    raise HTTPError("the service is unavailable")`, true, "the service is unavailable"},
		{"a server that says no", `
    import urllib.error
    raise urllib.error.HTTPError("https://api.example.com", 404, "Not Found", {}, None)`, false, "404"},
		{"the body's own exception", `raise Exception("the body is broken")`, false, "runner: the body is broken"},
		{"a bug", `return {}["missing"]`, false, "KeyError"},
	} {
		t.Run(c.name, func(t *testing.T) {
			spec := Spec{
				Repository: "t1", Function: "temporary.g.test/" + string(rune('a'+i)),
				Runtime: "python",
				Source:  "def main(input, host):\n    " + strings.TrimLeft(c.body, "\n ") + "\n",
			}
			_, err := r.Invoke(context.Background(), spec, testInput(), widgetBackend())
			if err == nil {
				t.Fatal("the body succeeded")
			}
			if Temporary(err) != c.temporary {
				t.Fatalf("Temporary(%q) = %v, want %v", err, !c.temporary, c.temporary)
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Fatalf("the error reads %q, want it to say %q", err, c.says)
			}
		})
	}

	t.Run("a body that runs out of time", func(t *testing.T) {
		spec := Spec{
			Repository: "t1", Function: "temporary.g.test/stall",
			Runtime:   "python",
			Source:    "import time\ndef main(input, host):\n    time.sleep(30)\n",
			TimeoutMs: 1000,
		}
		_, err := r.Invoke(context.Background(), spec, testInput(), widgetBackend())
		if err == nil || !strings.Contains(err.Error(), "invocation exceeded") || !Temporary(err) {
			t.Fatalf("err = %v, want a temporary timeout", err)
		}
	})
}
