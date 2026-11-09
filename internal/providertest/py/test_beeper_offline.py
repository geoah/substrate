"""The beeper sync body's pause while Beeper Desktop is not reachable.

bodyunit_test.go writes the `messagessync` function body of
kinds/providers.substrate.reamde.dev/beeper/bundle.yaml into the directory
named by SUBSTRATE_BODIES_DIR and runs this file with `-I`, under a Python the
body's `requires-python` admits, which uv finds. The body's top level is
exec'd into its own namespace, exactly the code the runner registers, so a
case calls the shipped helper and not a copy of it.

The body imports `requests`, which that interpreter need not have, so a stub
with requests' exception tree is installed in `sys.modules` first. The
classifier reads only that tree and the standard library's socket errors.
"""

import errno
import os
import socket
import sys
import types
import unittest

ACCOUNT_TYPE = "providers.substrate.reamde.dev/beeper/account"
EARLIER = "2026-10-20T12:45:10.5+00:00"
MESSAGE_PREFIX = "Beeper Desktop is not reachable (since "


def _stub_requests():
    """requests' exception classes, shaped as requests 2.34 declares them."""
    exceptions = types.ModuleType("requests.exceptions")

    class RequestException(OSError):
        pass

    class ConnectionError(RequestException):  # noqa: A001 (requests' own name)
        pass

    class Timeout(RequestException):
        pass

    class ConnectTimeout(ConnectionError, Timeout):
        pass

    class ReadTimeout(Timeout):
        pass

    class SSLError(ConnectionError):
        pass

    class ProxyError(ConnectionError):
        pass

    for cls in (RequestException, ConnectionError, Timeout, ConnectTimeout,
                ReadTimeout, SSLError, ProxyError):
        setattr(exceptions, cls.__name__, cls)

    module = types.ModuleType("requests")
    module.exceptions = exceptions

    def get(*_a, **_k):
        raise AssertionError("a unit case made a request")

    module.get = get
    sys.modules["requests"] = module
    sys.modules["requests.exceptions"] = exceptions
    return exceptions


EXC = _stub_requests()


def _load():
    path = os.path.join(os.environ["SUBSTRATE_BODIES_DIR"], "messagessync.py")
    with open(path, encoding="utf-8") as f:
        source = f.read()
    namespace = {"__name__": "messagessync"}
    exec(compile(source, path, "exec"), namespace)
    return namespace


BODY = _load()


class MaxRetryError(Exception):
    """urllib3's wrapper: the socket failure rides on `.reason`."""

    def __init__(self, reason):
        super().__init__("Max retries exceeded")
        self.reason = reason


class NewConnectionError(Exception):
    pass


class ConnectTimeoutError(Exception):
    pass


def _wrapped(cause, outer=None):
    """A requests error as requests raises one: MaxRetryError in args[0], the
    urllib3 error on its reason, the socket error that error's cause."""
    inner = NewConnectionError("Failed to establish a new connection")
    inner.__cause__ = cause
    return (outer or EXC.ConnectionError)(MaxRetryError(inner))


class _Effects:
    def __init__(self):
        self.ops = []

    def patch(self, kind, rid, properties=None, **_):
        self.ops.append((kind, rid, properties))


class _Host:
    def __init__(self):
        self.effects = _Effects()
        self.logs = []

    def log(self, line):
        self.logs.append(line)


def _account(**props):
    base = {"enabledMessages": True}
    base.update(props)
    return {"id": "acct", "kind": ACCOUNT_TYPE, "properties": base}


def _cursors(**over):
    out = {"phase": "whole", "cursor": "", "floor": None, "cold": False,
           "filter": "", "wantContacts": False, "failed": [], "retry": [],
           "contacts": [], "queue": ["!c1:beeper.local"], "msgq": [],
           "contact": None, "work": None}
    out.update(over)
    return out


class UnreachableTest(unittest.TestCase):
    def test_a_failure_to_reach_the_desktop_is_unreachable(self):
        cases = {
            "connect timeout": EXC.ConnectTimeout(
                MaxRetryError(ConnectTimeoutError("timed out"))),
            "refused": _wrapped(ConnectionRefusedError(
                errno.ECONNREFUSED, "Connection refused")),
            "no route to host": _wrapped(OSError(errno.EHOSTUNREACH, "No route")),
            "network unreachable": _wrapped(OSError(errno.ENETUNREACH, "unreachable")),
            "name does not resolve": _wrapped(socket.gaierror(-2, "Name or service not known")),
            "socket timeout": _wrapped(socket.timeout("timed out")),
            "urllib3 connect timeout by name": EXC.ConnectionError(
                MaxRetryError(ConnectTimeoutError("timed out"))),
        }
        for name, err in cases.items():
            with self.subTest(name):
                self.assertTrue(BODY["_unreachable"](err))

    def test_anything_else_is_a_failure(self):
        cases = {
            # The sandbox gate refuses a destination outside its allowlist with
            # EACCES: an operator's mistake, not a computer asleep.
            "sandbox gate": _wrapped(PermissionError(errno.EACCES, "Permission denied")),
            "eperm": _wrapped(OSError(errno.EPERM, "Operation not permitted")),
            "tls": _wrapped(ConnectionRefusedError(errno.ECONNREFUSED, "x"),
                            outer=EXC.SSLError),
            "proxy": _wrapped(ConnectionRefusedError(errno.ECONNREFUSED, "x"),
                              outer=EXC.ProxyError),
            "read timeout": EXC.ReadTimeout("read timed out"),
            "an answer": BODY["Beeper"]("/v1/info", "401 invalid token", 401),
            "a server error": BODY["Beeper"]("/v1/chats", "502", 502),
            "bad data": ValueError("not JSON"),
            "an unknown chain": EXC.ConnectionError("connection aborted"),
        }
        for name, err in cases.items():
            with self.subTest(name):
                self.assertFalse(BODY["_unreachable"](err))

    def test_a_cycle_in_the_chain_ends(self):
        a = EXC.ConnectionError("a")
        b = RuntimeError("b")
        a.__cause__, b.__cause__ = b, a
        self.assertFalse(BODY["_unreachable"](a))


class WaitingTest(unittest.TestCase):
    def waiting(self, props, cursors=None):
        host = _Host()
        acct = _account(**props)
        cursors = cursors or _cursors()
        BODY["_waiting"](host, acct, "acct", EXC.ConnectTimeout("timed out"),
                         cursors, run={"calls": 3})
        self.assertEqual(len(host.effects.ops), 1)
        kind, rid, patch = host.effects.ops[0]
        self.assertEqual((kind, rid), (ACCOUNT_TYPE, "acct"))
        self.assertTrue(any("not reachable" in line for line in host.logs), host.logs)
        return patch, cursors

    def test_the_first_unreachable_run_starts_the_stretch(self):
        patch, cursors = self.waiting(
            {"syncRequestedAt": "2026-10-20T12:40:00Z"})
        self.assertEqual(patch["syncState"], "throttled")
        self.assertTrue(patch["syncMessage"].startswith(MESSAGE_PREFIX), patch["syncMessage"])
        self.assertTrue(patch["syncMessage"].endswith(
            "); syncing resumes when it is back"))
        self.assertEqual(patch["syncStatus"], patch["syncMessage"])
        self.assertEqual(patch["unreachableSince"], patch["lastSyncedAt"])
        for key in ("syncError", "syncErrorAt", "retryNotBefore", "syncRequestedAck"):
            self.assertNotIn(key, patch)
        self.assertEqual(patch["streamCursors"], cursors)
        self.assertEqual(patch["streamCursors"]["queue"], ["!c1:beeper.local"])
        for stream in ("chats", "messages"):
            self.assertEqual(patch["syncStreams"][stream]["state"], "throttled")
            self.assertEqual(patch["syncStreams"][stream]["message"], patch["syncMessage"])

    def test_a_run_that_reached_beeper_starts_a_new_stretch(self):
        host = _Host()
        BODY["_waiting"](host, _account(unreachableSince=EARLIER), "acct",
                         EXC.ConnectTimeout("timed out"), _cursors(),
                         run={"calls": 3}, reached=True)
        patch = host.effects.ops[0][2]
        self.assertEqual(patch["unreachableSince"], patch["lastSyncedAt"])
        self.assertNotIn("12:45 UTC", patch["syncMessage"])

    def test_a_later_unreachable_run_keeps_the_first_instant(self):
        patch, _ = self.waiting({"unreachableSince": EARLIER})
        self.assertEqual(patch["unreachableSince"], EARLIER)
        self.assertEqual(
            patch["syncMessage"],
            "Beeper Desktop is not reachable (since 2026-10-20 12:45 UTC); "
            "syncing resumes when it is back")
        self.assertNotEqual(patch["lastSyncedAt"], EARLIER)


class ClearingTest(unittest.TestCase):
    def stamped(self, fn, props):
        host = _Host()
        acct = _account(**props)
        if fn == "_stamp":
            BODY["_stamp"](host, acct, "acct", _cursors(phase="done", queue=[]))
        else:
            BODY["_erroring"](host, acct, "acct", "/v1/info: 401 invalid token",
                              _cursors(), attempted=True, at_phase="info")
        return host.effects.ops[-1][2]

    def test_a_run_that_reaches_beeper_ends_the_stretch(self):
        for fn in ("_stamp", "_erroring"):
            with self.subTest(fn):
                patch = self.stamped(fn, {"unreachableSince": EARLIER})
                self.assertIn("unreachableSince", patch)
                self.assertIsNone(patch["unreachableSince"])

    def test_an_ordinary_run_writes_no_delete(self):
        for fn in ("_stamp", "_erroring"):
            with self.subTest(fn):
                self.assertNotIn("unreachableSince", self.stamped(fn, {}))

    def test_an_answer_still_stamps_erroring(self):
        patch = self.stamped("_erroring", {"unreachableSince": EARLIER})
        self.assertEqual(patch["syncState"], "erroring")
        self.assertIn("syncError", patch)


class SayTest(unittest.TestCase):
    """The account's words for a failure: a sentence with the fix on line one,
    the technical detail on the line after it."""

    LOCAL = "http://localhost:23373"

    def lines(self, err, base=LOCAL):
        said = BODY["_say"](err, base)
        head, _, detail = said.partition("\n")
        return head, detail

    def test_the_sandbox_gate_on_loopback_says_where_desktop_runs(self):
        head, detail = self.lines(_wrapped(PermissionError(errno.EACCES, "Permission denied")))
        self.assertTrue(head.startswith(
            "This server's sandbox blocked the connection to Beeper Desktop at "
            "localhost:23373."), head)
        self.assertIn("SUBSTRATE_SANDBOX_EGRESS_ALLOW", head)
        self.assertIn("API base", head)
        self.assertIn("Max retries exceeded", detail)

    def test_the_sandbox_gate_on_a_private_address_names_it(self):
        head, _ = self.lines(_wrapped(OSError(errno.EPERM, "x")),
                             "http://100.101.102.103:23373")
        self.assertIn("at 100.101.102.103:23373", head)
        self.assertIn("must add 100.101.102.103 to SUBSTRATE_SANDBOX_EGRESS_ALLOW", head)
        head, _ = self.lines(_wrapped(OSError(errno.EACCES, "x")),
                             "http://mac.tail1234.ts.net:23373")
        self.assertIn("the address mac.tail1234.ts.net resolves to", head)

    def test_beepers_own_answers(self):
        cases = {
            401: "Beeper Desktop no longer accepts the token on the Beeper config.",
            403: "Beeper Desktop refused this request with the token",
            404: "Beeper Desktop at localhost:23373 doesn't know this request.",
            409: "Beeper Desktop refused the request (HTTP 409)",
            502: "Beeper Desktop had an internal error (HTTP 502). The next sync tries again.",
        }
        for status, opening in cases.items():
            with self.subTest(status):
                head, detail = self.lines(BODY["Beeper"]("/v1/info", "%d nope" % status, status))
                self.assertTrue(head.startswith(opening), head)
                self.assertEqual(detail, "%d nope" % status)
                self.assertNotIn("/v1/", head)

    def test_a_body_that_is_not_beepers(self):
        head, _ = self.lines(BODY["Beeper"]("/v1/info", "response was not JSON", 200))
        self.assertIn("not like Beeper Desktop's API", head)

    def test_a_read_timeout_and_the_unknown(self):
        head, _ = self.lines(EXC.ReadTimeout("read timed out"))
        self.assertIn("didn't answer within 20 seconds", head)
        head, detail = self.lines(ValueError("boom"))
        self.assertTrue(head.startswith("The Beeper sync stopped on an unexpected error."))
        self.assertEqual(detail, "ValueError: boom")

    def test_only_the_detail_is_cut(self):
        long = "x" * 1000
        said = BODY["_say"](BODY["Beeper"]("/v1/info", long, 401), self.LOCAL)
        head, _, detail = said.partition("\n")
        self.assertEqual(len(detail), BODY["DETAIL_MAX"])
        capped = BODY["_capped"](said)
        self.assertTrue(capped.startswith(head + "\n"))
        self.assertLessEqual(len(capped), BODY["REASON_MAX"])
        self.assertEqual(BODY["_capped"]("y" * 500 + "\nmore"), "y" * 500)

    def test_a_chats_own_failure(self):
        said = BODY["_say_chat"](BODY["Beeper"]("/v1/chats/x", "404 Chat not found", 404))
        self.assertEqual(said, "Beeper Desktop no longer has this chat.\n404 Chat not found")

    def test_the_ok_runs_failure_note_is_words(self):
        note = BODY["_failed_note"]([
            ["messages", "!a", 1, "permanent: 404 Chat not found"],
            ["messages", "!b", 1, "permanent: 404 Chat not found"],
            ["contacts", "c", 2, "502"],
        ])
        self.assertEqual(note, "3 couldn't be read: chat histories (not found) x2, "
                               "address books (HTTP 502) x1")
        self.assertNotIn("pending", note)


if __name__ == "__main__":
    unittest.main(verbosity=2)
