#!/usr/bin/env python3
"""DEPRECATED — both shims are in the platform now; import `tools/mockserver.py`.

  * the search-window relaxation is `mockserver.QUERY_RELAXATIONS`, which
    strips `updated:`/`created:`/`pushed:`/`merged:`/`closed:` terms out of a
    `q` value and tries the relaxed name as its own candidate (logged
    `~relaxed`), so one recording answers every incremental window;
  * the background server is `mockserver.serve_bg(dir, port, provider)`,
    returning `(base_url, httpd, mock)` — and it builds a handler SUBCLASS per
    server, so a scenario's own second-pass mock cannot clobber the runner's.

This file forwards so `providers/github/scenario.py` keeps working; it
carries no logic and should be deleted with the import that needs it.
"""

import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2] / "runner"))
import mockserver  # noqa: E402,F401
from mockserver import serve_bg  # noqa: E402


def serve(directory, port=0, provider="github"):
    return serve_bg(directory, port, provider)


if __name__ == "__main__":
    import threading
    d = sys.argv[1] if len(sys.argv) > 1 else "fixtures/github"
    p = int(sys.argv[2]) if len(sys.argv) > 2 else 8099
    url, srv, _ = serve(d, p)
    sys.stderr.write("serving %s at %s\n" % (d, url))
    threading.Event().wait()
