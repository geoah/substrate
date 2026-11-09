"""What each Beeper write sends, and where the token may go.

bodyunit_test.go writes the `beeper_api.py` module and the seven write
function bodies of kinds/providers.substrate.reamde.dev/beeper/bundle.yaml
into the directory named by SUBSTRATE_BODIES_DIR, with schemas.json, each
function's compiled arguments and returns, and runs this file with `-I`.
The module is imported from there under its own name, as the runner puts it
on a body's path, and each body is exec'd into its own namespace, exactly the
code the runner registers.

No case opens a socket: the module's opener is replaced by a recorder
wherever a request would be sent.
"""

import importlib.util
import io
import json
import os
import sys
import unittest
import urllib.error
import urllib.request

DIR = os.environ["SUBSTRATE_BODIES_DIR"]
BASE = "http://localhost:23373"
TOKEN = "bdapi-unit-token"
CHAT = "!c1:beeper.local"
ROUTE = BASE + "/v1/chats/%21c1%3Abeeper.local"
THUMBS_UP = "\N{THUMBS UP SIGN}"


def _module():
    spec = importlib.util.spec_from_file_location(
        "beeper_api", os.path.join(DIR, "beeper_api.py"))
    module = importlib.util.module_from_spec(spec)
    sys.modules["beeper_api"] = module
    spec.loader.exec_module(module)
    return module


api = _module()

with open(os.path.join(DIR, "schemas.json"), encoding="utf-8") as f:
    SCHEMAS = json.load(f)


def _body(name):
    path = os.path.join(DIR, name + ".py")
    with open(path, encoding="utf-8") as f:
        source = f.read()
    namespace = {"__name__": name}
    exec(compile(source, path, "exec"), namespace)
    return namespace["main"]


BODIES = {name: _body(name) for name in SCHEMAS}

JSON_TYPES = {"string": (str,), "number": (int, float), "boolean": (bool,)}


def conforms(schema, value):
    """Why value breaks a compiled flat schema, or "" when it holds to it: the
    object is closed, its required names are present, and each value has its
    declared type (a bool is no number, as JSON has it)."""
    props = schema.get("properties") or {}
    extra = sorted(set(value) - set(props))
    if extra:
        return "undeclared %s" % extra
    missing = sorted(set(schema.get("required") or []) - set(value))
    if missing:
        return "missing %s" % missing
    for name, v in value.items():
        want = props[name].get("type")
        if want and (not isinstance(v, JSON_TYPES[want])
                     or (want == "number" and isinstance(v, bool))):
            return "%s is %r, not a %s" % (name, v, want)
    return ""


class _Answer:
    def __init__(self, body):
        self.body = body

    def __enter__(self):
        return self

    def __exit__(self, *_):
        return False

    def read(self):
        return self.body


class _Recorder:
    """Stands in for the opener: records each request, then answers with the
    canned body or raises the canned error."""

    def __init__(self):
        self.requests = []
        self.answer = b""

    def open(self, req, timeout=None):
        self.requests.append(req)
        if isinstance(self.answer, BaseException):
            raise self.answer
        return _Answer(self.answer)


def _refusal(code, body):
    return urllib.error.HTTPError(ROUTE, code, "refused", {}, io.BytesIO(body))


class WriteCase(unittest.TestCase):
    def setUp(self):
        self.real = api._OPENER
        self.recorder = _Recorder()
        api._OPENER = self.recorder

    def tearDown(self):
        api._OPENER = self.real

    def call(self, name, args, answer=b"", **conf):
        """Run one write body as a direct call runs it; its output, checked
        against the function's declared returns."""
        self.assertEqual(conforms(SCHEMAS[name]["arguments"], args), "",
                         "%s: the case's own arguments" % name)
        self.recorder.answer = (json.dumps(answer).encode("utf-8")
                                if isinstance(answer, dict) else answer)
        props = {"accessToken": TOKEN}
        props.update(conf)
        out = BODIES[name]({"mode": "call", "args": args,
                            "config": {"inputs": {"connector": {"properties": props}}}},
                           None)["output"]
        self.assertEqual(conforms(SCHEMAS[name]["returns"], out), "",
                         "%s answered %r" % (name, out))
        return out

    def sent(self):
        """The one request made: its method, URL and JSON body."""
        self.assertEqual(len(self.recorder.requests), 1)
        req = self.recorder.requests[0]
        self.assertEqual(req.get_header("Authorization"), "Bearer " + TOKEN)
        body = json.loads(req.data) if req.data is not None else None
        if body is not None:
            self.assertEqual(req.get_header("Content-type"), "application/json")
        return req.get_method(), req.full_url, body

    def refused(self, name, args, words, error=ValueError, **conf):
        with self.assertRaisesRegex(error, words):
            self.call(name, args, **conf)
        self.assertEqual(self.recorder.requests, [], "%s sent a refused call" % name)


class SendMessageTest(WriteCase):
    def test_a_reply(self):
        out = self.call("sendmessage", {"chat": CHAT, "text": "On my way.", "replyTo": "m1"},
                        {"chatID": CHAT, "pendingMessageID": "p1"})
        self.assertEqual(self.sent(), ("POST", ROUTE + "/messages",
                                       {"text": "On my way.", "replyToMessageID": "m1"}))
        self.assertEqual(out, {"chat": CHAT, "pendingMessageId": "p1"})

    def test_a_merged_chat_answers_the_member_it_routed_to(self):
        out = self.call("sendmessage", {"chat": CHAT, "text": "hi"},
                        {"chatID": "!member:beeper.local", "pendingMessageID": "p1"})
        self.assertEqual(self.sent()[2], {"text": "hi"})
        self.assertEqual(out["chat"], "!member:beeper.local")

    def test_an_answer_with_no_pending_id_fails(self):
        with self.assertRaisesRegex(RuntimeError, "no pendingMessageID"):
            self.call("sendmessage", {"chat": CHAT, "text": "hi"}, {"chatID": CHAT})

    def test_blank_text_sends_nothing(self):
        self.refused("sendmessage", {"chat": CHAT, "text": "  "}, "text is empty")


class EditMessageTest(WriteCase):
    def test_the_answer_is_the_edited_message(self):
        out = self.call("editmessage", {"chat": CHAT, "message": "m1", "text": "Ten minutes."},
                        {"id": "m1", "chatID": CHAT, "text": "Ten minutes.",
                         "messageID": "m1", "success": True})
        self.assertEqual(self.sent(), ("PUT", ROUTE + "/messages/m1", {"text": "Ten minutes."}))
        self.assertEqual(out, {"chat": CHAT, "message": "m1", "text": "Ten minutes."})

    def test_an_older_answer_carries_no_text(self):
        out = self.call("editmessage", {"chat": CHAT, "message": "m1", "text": "x"},
                        {"chatID": CHAT, "messageID": "m1", "success": True})
        self.assertEqual(out, {"chat": CHAT, "message": "m1"})

    def test_blank_text_sends_nothing(self):
        self.refused("editmessage", {"chat": CHAT, "message": "m1", "text": "\n"},
                     "text is empty")


class DeleteMessageTest(WriteCase):
    def test_for_everyone(self):
        out = self.call("deletemessage", {"chat": CHAT, "message": "m1", "forEveryone": True})
        self.assertEqual(self.sent(), ("DELETE", ROUTE + "/messages/m1?forEveryone=true", None))
        self.assertEqual(out, {"chat": CHAT, "message": "m1", "forEveryone": True})

    def test_for_the_owner_alone(self):
        out = self.call("deletemessage", {"chat": CHAT, "message": "m1", "forEveryone": False})
        self.assertEqual(self.sent(), ("DELETE", ROUTE + "/messages/m1?forEveryone=false", None))
        self.assertIs(out["forEveryone"], False)

    def test_a_json_answer_is_ignored(self):
        out = self.call("deletemessage", {"chat": CHAT, "message": "m1", "forEveryone": True},
                        {"success": True})
        self.assertEqual(out, {"chat": CHAT, "message": "m1", "forEveryone": True})

    def test_which_delete_is_never_left_to_beeper(self):
        for value in (None, "true", 1):
            with self.subTest(forEveryone=value):
                with self.assertRaisesRegex(ValueError, "forEveryone is required"):
                    BODIES["deletemessage"](
                        {"args": {"chat": CHAT, "message": "m1", "forEveryone": value},
                         "config": {"inputs": {"connector": {"properties":
                                                             {"accessToken": TOKEN}}}}},
                        None)
                self.assertEqual(self.recorder.requests, [])


class ReactionTest(WriteCase):
    def test_add(self):
        out = self.call("addreaction", {"chat": CHAT, "message": "m1", "reactionKey": THUMBS_UP},
                        {"success": True, "chatID": CHAT, "messageID": "m1",
                         "reactionKey": THUMBS_UP, "transactionID": "t1"})
        self.assertEqual(self.sent(), ("POST", ROUTE + "/messages/m1/reactions",
                                       {"reactionKey": THUMBS_UP}))
        self.assertEqual(out, {"chat": CHAT, "message": "m1", "reactionKey": THUMBS_UP,
                               "transactionId": "t1"})

    def test_add_with_no_transaction_id(self):
        out = self.call("addreaction", {"chat": CHAT, "message": "m1", "reactionKey": "+1"},
                        {"success": True, "chatID": CHAT, "messageID": "m1", "reactionKey": "+1"})
        self.assertNotIn("transactionId", out)

    def test_remove_puts_the_key_in_the_path(self):
        out = self.call("removereaction",
                        {"chat": CHAT, "message": "m1", "reactionKey": THUMBS_UP},
                        {"success": True, "chatID": CHAT, "messageID": "m1",
                         "reactionKey": THUMBS_UP})
        self.assertEqual(self.sent(), ("DELETE", ROUTE + "/messages/m1/reactions/%F0%9F%91%8D",
                                       None))
        self.assertEqual(out, {"chat": CHAT, "message": "m1", "reactionKey": THUMBS_UP})

    def test_a_key_is_one_path_segment(self):
        self.call("removereaction", {"chat": CHAT, "message": "m/1", "reactionKey": "a/b?c"},
                  {"success": True})
        self.assertEqual(self.sent()[1], ROUTE + "/messages/m%2F1/reactions/a%2Fb%3Fc")


class MarkTest(WriteCase):
    def chat(self, **state):
        out = {"id": CHAT, "accountID": "telegram", "title": "Ada", "type": "single",
               "participants": {"items": [], "hasMore": False, "total": 0}}
        out.update(state)
        return out

    def test_read_the_whole_chat(self):
        out = self.call("markread", {"chat": CHAT},
                        self.chat(unreadCount=0, isMarkedUnread=False))
        self.assertEqual(self.sent(), ("POST", ROUTE + "/read", {}))
        self.assertEqual(out, {"chat": CHAT, "unreadCount": 0, "isMarkedUnread": False})

    def test_read_up_to_one_message(self):
        self.call("markread", {"chat": CHAT, "message": "m1"}, self.chat(unreadCount=2))
        self.assertEqual(self.sent(), ("POST", ROUTE + "/read", {"messageID": "m1"}))

    def test_unread(self):
        out = self.call("markunread", {"chat": CHAT},
                        self.chat(unreadCount=0, isMarkedUnread=True))
        self.assertEqual(self.sent(), ("POST", ROUTE + "/unread", {}))
        self.assertEqual(out, {"chat": CHAT, "unreadCount": 0, "isMarkedUnread": True})

    def test_unread_from_one_message(self):
        self.call("markunread", {"chat": CHAT, "message": "m1"}, self.chat())
        self.assertEqual(self.sent()[2], {"messageID": "m1"})

    def test_a_fact_the_answer_lacks_or_mistypes_is_left_out(self):
        out = self.call("markread", {"chat": CHAT},
                        {"id": CHAT, "unreadCount": True, "isMarkedUnread": "no"})
        self.assertEqual(out, {"chat": CHAT})


# One well-formed call per write, for the cases every write must pass.
CALLS = {
    "sendmessage": {"chat": CHAT, "text": "hi"},
    "editmessage": {"chat": CHAT, "message": "m1", "text": "hi"},
    "deletemessage": {"chat": CHAT, "message": "m1", "forEveryone": True},
    "addreaction": {"chat": CHAT, "message": "m1", "reactionKey": THUMBS_UP},
    "removereaction": {"chat": CHAT, "message": "m1", "reactionKey": THUMBS_UP},
    "markread": {"chat": CHAT},
    "markunread": {"chat": CHAT},
}


class EveryWriteTest(WriteCase):
    def test_every_write_is_covered(self):
        self.assertEqual(sorted(CALLS), sorted(BODIES))

    def test_a_blank_id_sends_nothing(self):
        for name, args in CALLS.items():
            for arg in ("chat", "message", "reactionKey"):
                if arg not in args:
                    continue
                with self.subTest(name=name, arg=arg):
                    self.refused(name, dict(args, **{arg: " "}), arg + " is required")

    def test_the_token_goes_nowhere_public(self):
        for name, args in CALLS.items():
            for base in ("https://beeper.example.com", "http://8.8.8.8:23373",
                         "http://100.128.0.1:23373", "http://user:pw@localhost:23373",
                         "ftp://127.0.0.1", "http://localhost.evil.example.com"):
                with self.subTest(name=name, apiBase=base):
                    self.refused(name, args, "not a private origin", RuntimeError,
                                 apiBase=base)

    def test_a_missing_or_sealed_token_sends_nothing(self):
        for name, args in CALLS.items():
            with self.subTest(name=name, token="missing"):
                self.refused(name, args, "no accessToken", RuntimeError, accessToken="")
            with self.subTest(name=name, token="sealed"):
                self.refused(name, args, "arrived sealed", RuntimeError,
                             accessToken="substrate:sealsecret:v1:abc")

    def test_beepers_refusal_reaches_the_caller(self):
        for name, args in CALLS.items():
            with self.subTest(name=name):
                self.recorder.requests = []
                with self.assertRaisesRegex(RuntimeError, "HTTP 404 Chat not found"):
                    self.call(name, args, _refusal(
                        404, b'{"message": "Chat not found", "code": "NOT_FOUND"}'))

    def test_a_private_origin_is_where_the_call_goes(self):
        out = self.call("markread", {"chat": CHAT}, {"id": CHAT},
                        apiBase="http://100.101.102.103:23373/")
        self.assertEqual(self.sent()[1],
                         "http://100.101.102.103:23373/v1/chats/%21c1%3Abeeper.local/read")
        self.assertEqual(out, {"chat": CHAT})


class CallTest(WriteCase):
    def call_api(self, answer):
        self.recorder.answer = answer
        return api.call({"config": {"inputs": {"connector": {"properties":
                                                             {"accessToken": TOKEN}}}}},
                        "POST", "/v1/chats/x/read", "marking x read", {})

    def test_a_refusal_names_the_write_the_status_and_beepers_words(self):
        cases = (
            (_refusal(422, b'{"message": "Messages with attachments cannot be edited"}'),
             "marking x read failed: HTTP 422 Messages with attachments cannot be edited"),
            (_refusal(400, b'{"code": "VALIDATION_ERROR"}'), "HTTP 400 VALIDATION_ERROR"),
            (_refusal(502, b"bad gateway"), "HTTP 502 bad gateway"),
            (_refusal(500, b'["not", "an", "object"]'), "HTTP 500 \\[\"not\""),
            (_refusal(302, b""), "HTTP 302"),
        )
        for error, words in cases:
            with self.subTest(words=words), self.assertRaisesRegex(RuntimeError, words):
                self.call_api(error)

    def test_an_unreachable_desktop_says_so(self):
        with self.assertRaisesRegex(RuntimeError,
                                    "Beeper Desktop is not reachable at http://localhost:23373"):
            self.call_api(urllib.error.URLError(ConnectionRefusedError(61, "refused")))

    def test_an_empty_answer_is_an_empty_object(self):
        self.assertEqual(self.call_api(b""), {})
        self.assertEqual(self.call_api(b"[1]"), {})

    def test_an_answer_that_is_not_json_fails(self):
        with self.assertRaisesRegex(RuntimeError, "not JSON"):
            self.call_api(b"<html>")


class OriginTest(unittest.TestCase):
    def test_private_origins(self):
        for base in ("http://localhost:23373", "http://127.0.0.1:23373", "http://[::1]:23373",
                     "http://10.0.0.5:23373", "http://192.168.1.20:23373",
                     "http://172.16.0.9:23373", "http://100.64.0.1:23373",
                     "https://desk.tail1234.ts.net", "http://beeper.localhost:23373"):
            with self.subTest(base=base):
                self.assertTrue(api.private(base))

    def test_the_default_origin(self):
        token, base = api.connector({"config": {"inputs": {"connector": {"properties":
                                                                         {"accessToken": TOKEN}}}}})
        self.assertEqual((token, base), (TOKEN, BASE))


class OpenerTest(unittest.TestCase):
    def test_every_redirect_is_refused(self):
        handlers = [h for h in api._OPENER.handlers
                    if isinstance(h, urllib.request.HTTPRedirectHandler)]
        self.assertEqual(len(handlers), 1)
        self.assertIsInstance(handlers[0], api._NoRedirect)
        req = urllib.request.Request(ROUTE + "/read", method="POST")
        self.assertIsNone(handlers[0].redirect_request(
            req, None, 302, "Found", {}, "https://evil.example.com/v1/chats"))


if __name__ == "__main__":
    unittest.main(verbosity=2)
