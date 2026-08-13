package runner

import "regexp"

// PEP 723 inline script metadata (https://peps.python.org/pep-0723/): a
// `# /// script` … `# ///` comment block a Python body may carry to declare
// its own `dependencies` (and `requires-python`). When a function body carries
// one, the runner provisions it with `uv sync --script`, which reads the block
// and builds a cached venv holding those deps, and then runs that venv's
// interpreter. A body with no block runs the system `python3` instead. Either
// way it is one process for that installation alone; the block decides only
// whether uv has to provision an environment first.
//
// The regex is the one PEP 723 specifies for extracting the metadata block.
var pep723Re = regexp.MustCompile(`(?m)^# /// (?P<type>[a-zA-Z0-9-]+)$\s(?P<content>(^#(| .*)$\s)+)^# ///$`)

// pep723Block returns the verbatim `# /// script … # ///` block a Python body
// carries, and whether it has one. The block is embedded verbatim atop the
// generated uv host script so uv resolves exactly what the author declared;
// only a `script`-typed block counts (PEP 723 reserves the type token). The
// first script block wins — PEP 723 forbids two, and the runner needs only to
// know a body opts into dependency provisioning.
func pep723Block(source string) (string, bool) {
	for _, loc := range pep723Re.FindAllStringSubmatchIndex(source, -1) {
		if source[loc[2]:loc[3]] == "script" {
			return source[loc[0]:loc[1]], true
		}
	}
	return "", false
}
