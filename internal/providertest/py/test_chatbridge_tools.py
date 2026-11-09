"""The chat bridge's six agent tools, over a host that records what they write.

bodyunit_test.go writes the `chatbridge_actions.py` module and the six tool
bodies of kinds/providers.substrate.reamde.dev/chatbridge/bundle.yaml into the
directory named by SUBSTRATE_BODIES_DIR and runs this file with `-I`. Each
body is exec'd with that directory on its path, exactly the code the runner
registers.

What the database suite cannot see from the call door, which answers a
repeated Idempotency-Key from its store without running the body again: the
body itself names one action per key, writes it create-only, and refuses a
request it can tell is wrong before writing anything.
"""

import hashlib
import os
import sys
import unittest

DIR = os.environ["SUBSTRATE_BODIES_DIR"]
sys.path.insert(0, DIR)

PKG = "providers.substrate.reamde.dev/chatbridge/"
CHAT, MESSAGE, ACTION = PKG + "chat", PKG + "message", PKG + "chataction"


def _load(name):
    path = os.path.join(DIR, name + ".py")
    with open(path, encoding="utf-8") as f:
        source = f.read()
    namespace = {"__name__": name}
    exec(compile(source, path, "exec"), namespace)
    return namespace["main"]


TOOLS = {name: _load(name) for name in ("send", "react", "unreact", "edit", "delete", "markread")}


class _Effects:
    def __init__(self):
        self.puts = []

    def put(self, kind, rid, properties=None, if_absent=False):
        self.puts.append((kind, rid, properties, if_absent))


class _Ids:
    def minted(self, *parts):
        return "m" + hashlib.sha256("\x00".join(parts).encode()).hexdigest()[:25]


class _Records:
    def __init__(self, rows):
        self.rows = rows

    def get(self, kind, rid):
        return self.rows.get((kind, rid))


class _Host:
    def __init__(self, rows):
        self.records = _Records(rows)
        self.effects = _Effects()
        self.ids = _Ids()


def _rows():
    def msg(rid, chat, mine):
        return {"id": rid, "kind": MESSAGE,
                "properties": {"chat": {"ref": CHAT + "/" + chat}, "fromOwner": mine}}
    return {
        (CHAT, "c1"): {"id": "c1", "kind": CHAT, "properties": {}},
        (CHAT, "c2"): {"id": "c2", "kind": CHAT, "properties": {}},
        (MESSAGE, "theirs"): msg("theirs", "c1", False),
        (MESSAGE, "mine"): msg("mine", "c1", True),
        (MESSAGE, "gone"): dict(msg("gone", "c1", True), deletedAt="2026-11-09T12:00:00Z"),
    }


def _call(name, args, key="k1", rows=None):
    host = _Host(rows if rows is not None else _rows())
    out = TOOLS[name]({"args": args, "idempotencyKey": key}, host)
    return out, host.effects.puts


class ToolsTest(unittest.TestCase):
    def test_one_action_per_key_written_create_only(self):
        out, puts = _call("send", {"chat": "c1", "text": "on my way"})
        again, puts_again = _call("send", {"chat": "c1", "text": "on my way"})
        other, _ = _call("send", {"chat": "c1", "text": "on my way"}, key="k2")
        self.assertEqual(out["output"]["action"], again["output"]["action"])
        self.assertNotEqual(out["output"]["action"], other["output"]["action"])
        self.assertEqual(len(puts), 1)
        kind, rid, props, if_absent = puts[0]
        self.assertEqual((kind, rid, if_absent), (ACTION, out["output"]["action"], True))
        self.assertEqual(props, {"action": "send", "chat": {"ref": CHAT + "/c1"}, "text": "on my way"})
        self.assertEqual(puts, puts_again)

    def test_two_tools_on_one_key_name_two_actions(self):
        react, _ = _call("react", {"message": "theirs", "reactionKey": "x"})
        unreact, _ = _call("unreact", {"message": "theirs", "reactionKey": "x"})
        self.assertNotEqual(react["output"]["action"], unreact["output"]["action"])

    def test_an_argument_takes_the_id_or_the_path(self):
        _, by_id = _call("react", {"message": "theirs", "reactionKey": " 👍 "})
        _, by_path = _call("react", {"message": MESSAGE + "/theirs", "reactionKey": "👍"})
        self.assertEqual(by_id, by_path)
        self.assertEqual(by_id[0][2], {"action": "react", "chat": {"ref": CHAT + "/c1"},
                                      "target": {"ref": MESSAGE + "/theirs"}, "reactionKey": "👍"})
        with self.assertRaises(ValueError):
            _call("react", {"message": CHAT + "/c1", "reactionKey": "👍"})

    def test_what_each_tool_writes(self):
        cases = {
            "unreact": ({"message": "theirs", "reactionKey": "👍"},
                        {"action": "unreact", "target": {"ref": MESSAGE + "/theirs"}, "reactionKey": "👍"}),
            "edit": ({"message": "mine", "text": "hey there"},
                     {"action": "edit", "target": {"ref": MESSAGE + "/mine"}, "text": "hey there"}),
            "delete": ({"message": "theirs", "forEveryone": True},
                       {"action": "delete", "target": {"ref": MESSAGE + "/theirs"}, "forEveryone": True}),
            "markread": ({"chat": "c1", "upTo": "theirs"},
                         {"action": "markread", "target": {"ref": MESSAGE + "/theirs"}}),
        }
        for name, (args, want) in cases.items():
            _, puts = _call(name, args)
            want = dict(want, chat={"ref": CHAT + "/c1"})
            self.assertEqual(puts[0][2], want, name)
        _, puts = _call("delete", {"message": "theirs"})
        self.assertIs(puts[0][2]["forEveryone"], False)
        _, puts = _call("markread", {"chat": "c2"})
        self.assertNotIn("target", puts[0][2])

    def test_a_request_it_can_tell_is_wrong_writes_nothing(self):
        for name, args in (
            ("send", {"chat": "c1", "text": "  "}),
            ("send", {"chat": "nowhere", "text": "hi"}),
            ("send", {"chat": "c2", "text": "hi", "replyTo": "theirs"}),
            ("react", {"message": "theirs"}),
            ("react", {"message": "gone", "reactionKey": "👍"}),
            ("edit", {"message": "theirs", "text": "not mine"}),
            ("delete", {"message": "theirs", "forEveryone": "yes"}),
            ("markread", {"chat": "c2", "upTo": "theirs"}),
        ):
            host = _Host(_rows())
            with self.assertRaises(ValueError, msg="%s %r" % (name, args)):
                TOOLS[name]({"args": args, "idempotencyKey": "k"}, host)
            self.assertEqual(host.effects.puts, [], name)

    def test_no_key_no_action(self):
        host = _Host(_rows())
        with self.assertRaises(RuntimeError):
            TOOLS["send"]({"args": {"chat": "c1", "text": "hi"}}, host)
        self.assertEqual(host.effects.puts, [])


if __name__ == "__main__":
    unittest.main()
