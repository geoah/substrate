package providertest

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/geoah/substrate/internal/synth"
)

// synthRecording finds the recording the provider e2e runner's mock server
// answers a request with (internal/providere2e/runner/mockserver.py), under
// a directory `synth generate` wrote: for each path in turn (the request's,
// then any its provider's PATH_RELAXATIONS allow), the exact name, then the
// name without the pagination parameters, then the bare path. It answers
// the file's name and bytes and the index of the path that matched.
func synthRecording(dir string, r *http.Request, paths ...string) (string, []byte, int, bool) {
	pairs := synth.Query(r.URL.Query())
	var thin []synth.Pair
	for _, p := range pairs {
		if !synth.PaginationParams[p.Key] {
			thin = append(thin, p)
		}
	}
	for i, p := range append([]string{r.URL.Path}, paths...) {
		for _, name := range []string{
			synth.RecordingName(r.Method, p, pairs...),
			synth.RecordingName(r.Method, p, thin...),
			synth.RecordingName(r.Method, p),
		} {
			raw, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // a name the lookup composed under the test's own directory
			if err == nil {
				return name, raw, i, true
			}
		}
	}
	return "", nil, 0, false
}
