"""Call a provider's write function and read what it sent to the mock.

A write function is a callable, not a sync: nothing fires it, so a scenario
calls it through `POST …/core/function/{name}/call` and then asks the mock
what arrived. The mock logs each request's body (`/__mock/requests`), which
is where the scenario checks the form or the JSON the function sent.

    from writecall import Writes
    w = Writes(server, token, mock)
    w.reset()
    status, reply = w.call("providers.substrate.reamde.dev/slack/postmessage",
                           {"channel": "C0…", "text": "hi"})
    sent = w.requests("POST", "/api/chat.postMessage")
"""

from __future__ import annotations

import json
import urllib.parse

from e2e import API

CALL = "/api/v1/substrate.reamde.dev/core/function/%s/call"


class Writes:
    def __init__(self, server: str, token: str, mock: str):
        self.api = API(server, token)
        self.mock = mock.rstrip("/")

    def call(self, function: str, args: dict):
        """One call. Returns (status, reply): the reply is `{output, effects}`
        on a 200 and the error body otherwise."""
        st, body, _ = self.api.call(
            "POST", CALL % urllib.parse.quote(function, safe=""),
            {"input": args})
        return st, body

    def reset(self) -> None:
        self.api.call("DELETE", self.mock + "/__mock/requests")
        self.api.call("DELETE", self.mock + "/__mock/faults")

    def faults(self, rules: list) -> None:
        if rules:
            self.api.call("POST", self.mock + "/__mock/faults", {"rules": rules})
        else:
            self.api.call("DELETE", self.mock + "/__mock/faults")

    def requests(self, method: str = "", path: str = "") -> list[dict]:
        """The logged requests, optionally narrowed to one method and one
        percent-DECODED path, oldest first."""
        st, body, _ = self.api.call("GET", self.mock + "/__mock/requests")
        reqs = (body or {}).get("requests") if isinstance(body, dict) else body
        out = []
        for r in reqs or []:
            if method and r.get("method") != method:
                continue
            if path and urllib.parse.unquote(str(r.get("path") or "")) != path:
                continue
            out.append(r)
        return out


def form(req: dict) -> dict:
    """A logged form body as a flat dict (Slack's Web API)."""
    return dict(urllib.parse.parse_qsl(req.get("body") or "",
                                       keep_blank_values=True))


def body_json(req: dict):
    """A logged JSON body, or None when it is not JSON."""
    try:
        return json.loads(req.get("body") or "")
    except ValueError:
        return None


def error_text(reply) -> str:
    """Everything a refused call said, as one string to search."""
    return json.dumps(reply) if not isinstance(reply, str) else reply
