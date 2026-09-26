---
type: breaking
release: v0.75.0
---

# The `go` function runtime is removed and `runtime: go` is refused

This hits vocabulary authors who wrote a function body in Go. A function
declaration with `runtime: go` is refused on every door:

```
data.runtime: "go" is retired; write the body in python
```

The core `function` kind (version 14) lists `go` under
`retired.values.runtime`, so the value can never be declared again. A bundle
module whose filename ends in `.go` is refused too
(`a module filename ends in .py, the extension the runtime imports`). The
runtime image no longer contains a Go toolchain at `/usr/local/go`. The
runtimes left are `python` and `host`.

## What to do

1. Rewrite each Go body in Python. The entrypoint is `main(input, host)`;
   [functions](../functions.md) documents the `host` object.
2. Set `runtime: python` on the declaration and apply it.
3. Rewrite every `.go` entry under a bundle's `modules` as a `.py` module.
4. Do steps 1 to 3 before the upgrade. If no declaration used `runtime: go`,
   there is nothing to do.
