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

An agent reaches the same function as a tool. `agent_call` installs a
one-tool agent in the repository, points its llm/provider row at a scripted
OpenAI-compatible server in this process, and runs the agent once: the first
turn calls the tool with the arguments given, the second answers in text. The
engine runs the tool through `dispatchFunction`, so the call proves the
connector config (the token, the account, the client input) reaches a
function an agent calls, and not only one the host API calls.

    run = w.agent_call("providers.substrate.reamde.dev/slack/postmessage",
                       {"channel": "C0…", "text": "hi"})
    run["status"], run["result"], run["toolResult"], run["tools"]
"""

from __future__ import annotations

import http.server
import json
import threading
import urllib.parse

from e2e import API

CALL = "/api/v1/substrate.reamde.dev/core/function/%s/call"
AGENT_CALL = "/api/v1/substrate.reamde.dev/core/agent/%s/call"
LLM_PROVIDER = "substrate.reamde.dev/llm/provider"
# The package the scenario's agent lives in: its own, so it never touches the
# provider's closure.
AGENT_PACKAGE = "e2e.test.dev/writer"


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

    def agent_call(self, function: str, args: dict, name: str = "writer") -> dict:
        """Run a one-tool agent whose first turn calls `function` with
        `args`. Returns the call's HTTP `status`, the agent's `result`, the
        `toolResult` the engine handed the model back (parsed JSON), the
        tool names the model was offered (`tools`) and every completion
        request the scripted model saw (`requests`)."""
        tool = function.rsplit("/", 1)[-1]
        llm = ScriptedLLM(tool, args)
        try:
            st, body, _ = self.api.call(
                "PUT", "/api/v1/%s/e2e-%s" % (LLM_PROVIDER, name),
                {"properties": {"wire": "openai", "baseURL": llm.url,
                                "apiKey": "e2e-llm-key"}})
            if st >= 400:
                raise RuntimeError("llm/provider row refused: %s %s" % (st, body))
            authority, package = AGENT_PACKAGE.split("/")
            docs = [
                {"kind": "substrate.reamde.dev/core/package",
                 "metadata": {"id": AGENT_PACKAGE},
                 "data": {"authority": authority, "package": package,
                          "version": 1}},
                {"kind": "substrate.reamde.dev/core/agent",
                 "metadata": {"id": AGENT_PACKAGE + "/" + name},
                 "data": {"authority": authority, "package": package,
                          "description": "calls " + function + " once",
                          "prompt": "Call the tool you are given once.",
                          "provider": "e2e-" + name, "model": "e2e-model",
                          "tools": [{"function": function}]}},
            ]
            st, body, _ = self.api.call("POST", "/api/v1/vocabulary/apply",
                                        {"documents": docs})
            if st >= 400:
                raise RuntimeError("the agent did not apply: %s %s" % (st, body))
            st, result, _ = self.api.call(
                "POST", AGENT_CALL % urllib.parse.quote(AGENT_PACKAGE + "/" + name,
                                                        safe=""),
                {"input": "go"}, timeout=120)
        finally:
            llm.close()
        return {"status": st, "result": result, "toolResult": llm.tool_result(),
                "tools": llm.tools_offered(), "requests": llm.requests}

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


class ScriptedLLM:
    """An OpenAI-compatible `/chat/completions` on loopback with a two-turn
    script: the first request answers one call of `tool` with `args`, every
    later one answers plain text. Streamed and one-shot requests both work."""

    def __init__(self, tool: str, args: dict):
        self.tool, self.args = tool, args
        self.requests: list[dict] = []
        outer = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *a):
                pass

            def do_POST(self):
                n = int(self.headers.get("Content-Length") or 0)
                try:
                    req = json.loads(self.rfile.read(n) or b"{}")
                except ValueError:
                    req = {}
                if not self.path.endswith("/chat/completions"):
                    self.send_response(404)
                    self.end_headers()
                    return
                outer.requests.append(req)
                outer.answer(self, req, len(outer.requests) == 1)

        self.srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.url = "http://127.0.0.1:%d" % self.srv.server_address[1]
        threading.Thread(target=self.srv.serve_forever, daemon=True).start()

    def answer(self, h, req: dict, first: bool) -> None:
        usage = {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
        calls = [{"index": 0, "id": "call_0", "type": "function",
                  "function": {"name": self.tool,
                               "arguments": json.dumps(self.args)}}] if first else []
        content = "" if first else "Done."
        if not req.get("stream"):
            msg = {"role": "assistant", "content": content}
            if calls:
                msg["tool_calls"] = calls
            out = json.dumps({"id": "cmpl", "object": "chat.completion",
                              "choices": [{"index": 0, "message": msg,
                                           "finish_reason": "stop"}],
                              "usage": usage}).encode()
            h.send_response(200)
            h.send_header("Content-Type", "application/json")
            h.send_header("Content-Length", str(len(out)))
            h.end_headers()
            h.wfile.write(out)
            return
        chunks = []
        if content:
            chunks.append({"object": "chat.completion.chunk", "choices": [
                {"index": 0, "delta": {"content": content}}]})
        for c in calls:
            chunks.append({"object": "chat.completion.chunk", "choices": [
                {"index": 0, "delta": {"tool_calls": [c]}}]})
        chunks.append({"object": "chat.completion.chunk", "choices": [],
                       "usage": usage})
        out = "".join("data: %s\n\n" % json.dumps(c) for c in chunks)
        out = (out + "data: [DONE]\n\n").encode()
        h.send_response(200)
        h.send_header("Content-Type", "text/event-stream")
        h.send_header("Content-Length", str(len(out)))
        h.end_headers()
        h.wfile.write(out)

    def tools_offered(self) -> list[str]:
        first = self.requests[0] if self.requests else {}
        return [str((t.get("function") or {}).get("name") or t.get("name"))
                for t in first.get("tools") or [] if isinstance(t, dict)]

    def tool_result(self):
        """The tool message the second request carried, parsed; None when the
        model was never asked again."""
        for req in self.requests[1:]:
            for m in req.get("messages") or []:
                if isinstance(m, dict) and m.get("role") == "tool":
                    try:
                        return json.loads(m.get("content") or "")
                    except ValueError:
                        return m.get("content")
        return None

    def close(self) -> None:
        self.srv.shutdown()
        self.srv.server_close()
