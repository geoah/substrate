#!/usr/bin/env python3
"""A substrate that is slow to install, for SetupTimeoutTests.

Forwards every request to the substrate at UPSTREAM, and holds each catalog
install for DELAY seconds before passing it on, as a server installing a
bundle's requirements first does. The install still happens after the client
stopped waiting, which is what the test checks the app copes with.

    python3 apps/apple/scripts/slowserver.py http://localhost:8080 8 8093
"""

import http.server
import re
import sys
import time
import urllib.error
import urllib.request

UPSTREAM, DELAY, PORT = sys.argv[1].rstrip("/"), float(sys.argv[2]), int(sys.argv[3])
HOP = {"host", "connection", "content-length", "transfer-encoding", "keep-alive"}


class Handler(http.server.BaseHTTPRequestHandler):
    def forward(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length) if length else None
        if self.command == "POST" and re.search(
            r"/api/v1/catalog/[^/]+/install$", self.path
        ):
            time.sleep(DELAY)
        request = urllib.request.Request(
            UPSTREAM + self.path, data=body, method=self.command
        )
        for name, value in self.headers.items():
            if name.lower() not in HOP:
                request.add_header(name, value)
        try:
            with urllib.request.urlopen(request, timeout=600) as answer:
                status, headers, data = answer.status, answer.headers, answer.read()
        except urllib.error.HTTPError as refusal:
            status, headers, data = refusal.code, refusal.headers, refusal.read()
        try:
            self.send_response(status)
            for name, value in headers.items():
                if name.lower() not in HOP:
                    self.send_header(name, value)
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        except (BrokenPipeError, ConnectionResetError):
            pass  # the client stopped waiting; the upstream did the work

    do_GET = do_POST = do_PUT = do_PATCH = do_DELETE = forward

    def log_message(self, *args):
        pass


http.server.ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
