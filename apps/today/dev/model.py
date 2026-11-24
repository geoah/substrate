#!/usr/bin/env python3
"""A stand-in model for the Today dev server, which holds no model key.

    python3 apps/today/dev/model.py [log file]

It serves the Anthropic Messages API on 127.0.0.1:8099 and answers every turn
with how many of the person's messages it was given and how the first one
began, so asking the assistant can be tried with no account: a second
question in one conversation is answered "I was given 2 of your messages".
Each request's shape is appended to the log file as a JSON line.

Point the dev server's provider at it (apps/today/dev/README.md, "Asking the
assistant"). It is for a server on this machine only: it checks no key.
"""
import json, sys, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LOG = sys.argv[1] if len(sys.argv) > 1 else "/dev/null"

def texts(message):
    c = message.get("content")
    if isinstance(c, str): return [c]
    return [b.get("text", "") for b in c or [] if isinstance(b, dict) and b.get("type") == "text"]

class H(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_message(self, *a): pass
    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))) or b"{}")
        users = [m for m in body.get("messages", []) if m.get("role") == "user"]
        said = [" ".join(texts(m)).strip() for m in users]
        said = [s for s in said if s]
        # A message is the person's words, then the turn context the server adds after them.
        reply = "I was given %d of your messages in this conversation. The first began: %s" % (len(said), (said[0][:60] if said else "nothing"))
        with open(LOG, "a") as f:
            f.write(json.dumps({"path": self.path, "stream": bool(body.get("stream")), "model": body.get("model"), "users": len(said), "messages": len(body.get("messages", [])), "first": said[0][:80] if said else None, "last": said[-1][-80:] if said else None}) + "\n")
        if not body.get("stream"):
            out = json.dumps({"id": "msg_fake", "type": "message", "role": "assistant", "model": body.get("model", "fake"), "content": [{"type": "text", "text": "A talk about a task"}], "stop_reason": "end_turn", "stop_sequence": None, "usage": {"input_tokens": 10, "output_tokens": 5}}).encode()
            self.send_response(200); self.send_header("Content-Type", "application/json"); self.send_header("Content-Length", str(len(out))); self.end_headers(); self.wfile.write(out); return
        self.send_response(200); self.send_header("Content-Type", "text/event-stream"); self.send_header("Cache-Control", "no-cache"); self.send_header("Connection", "close"); self.end_headers()
        def ev(name, data):
            self.wfile.write(("event: %s\ndata: %s\n\n" % (name, json.dumps(data))).encode()); self.wfile.flush()
        ev("message_start", {"type": "message_start", "message": {"id": "msg_fake", "type": "message", "role": "assistant", "model": body.get("model", "fake"), "content": [], "stop_reason": None, "stop_sequence": None, "usage": {"input_tokens": 10, "output_tokens": 1}}})
        ev("content_block_start", {"type": "content_block_start", "index": 0, "content_block": {"type": "text", "text": ""}})
        for i in range(0, len(reply), 24):
            ev("content_block_delta", {"type": "content_block_delta", "index": 0, "delta": {"type": "text_delta", "text": reply[i:i+24]}}); time.sleep(0.05)
        ev("content_block_stop", {"type": "content_block_stop", "index": 0})
        ev("message_delta", {"type": "message_delta", "delta": {"stop_reason": "end_turn", "stop_sequence": None}, "usage": {"output_tokens": 20}})
        ev("message_stop", {"type": "message_stop"})
        self.close_connection = True

ThreadingHTTPServer(("127.0.0.1", 8099), H).serve_forever()
