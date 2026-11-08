"""The gmail sync body's address decode, without a database or uv.

bodyunit_test.go writes the `syncgmail` function body of
kinds/providers.substrate.reamde.dev/google/bundle.yaml into the directory
named by SUBSTRATE_BODIES_DIR and runs this file under `python3 -I`. The
body's top level is exec'd into its own namespace, exactly the code the
runner registers, so a case calls the shipped helper and not a copy of it.
"""

import os
import unittest


def _load():
    path = os.path.join(os.environ["SUBSTRATE_BODIES_DIR"], "syncgmail.py")
    with open(path, encoding="utf-8") as f:
        source = f.read()
    namespace = {"__name__": "syncgmail"}
    exec(compile(source, path, "exec"), namespace)
    return namespace


BODY = _load()
TOKEN = "AAEXAMPLETOKEN0000000000000000000000000000000000"


class RelayTest(unittest.TestCase):
    """A per-thread reply address names a thread, never a party."""

    def relay(self, addr):
        normal = BODY["_mailbox"](addr)
        self.assertTrue(normal, "%s does not parse" % addr)
        return BODY["_relay"](normal)

    def test_a_github_reply_address_is_a_relay(self):
        self.assertTrue(self.relay("reply+%s@reply.github.com" % TOKEN))
        # The domain is normalised and the prefix compared ignoring case.
        self.assertTrue(self.relay("Reply+%s@Reply.GitHub.com" % TOKEN))

    def test_an_identity_is_not(self):
        for addr in (
                # GitHub's one sender and its no-reply recipients are shared
                # by every notification: each is one identity.
                "notifications@github.com",
                "noreply@github.com",
                "repo@noreply.github.com",
                # The prefix alone, with no token after it.
                "reply+@reply.github.com",
                # The prefix at another domain, and the domain under another.
                "reply+%s@github.com" % TOKEN,
                "reply+%s@reply.github.com.example.com" % TOKEN,
                "reply+%s@example.com" % TOKEN,
                # Another local part at the relay domain.
                "someone@reply.github.com",
                "ada+reply@example.com"):
            with self.subTest(addr=addr):
                self.assertFalse(self.relay(addr))


class AddressDecodeTest(unittest.TestCase):
    """A relay keeps its occurrence and loses only its hub row."""

    def test_a_relay_entry_names_no_hub_row(self):
        entries, rows = BODY["_addrs"](
            ["owner/repo <reply+%s@reply.github.com>" % TOKEN])
        self.assertEqual(entries, [{"value": "reply+%s@reply.github.com" % TOKEN,
                                    "name": "owner/repo"}])
        self.assertEqual(rows, [])

    def test_the_other_mailboxes_of_the_line_keep_theirs(self):
        entries, rows = BODY["_addrs"]([
            "Ada <ada@Example.com>, reply+%s@reply.github.com" % TOKEN,
            "GitHub <notifications@github.com>"])
        self.assertEqual(entries, [
            {"value": "ada@Example.com", "normal": "ada@example.com", "name": "Ada"},
            {"value": "reply+%s@reply.github.com" % TOKEN},
            {"value": "notifications@github.com",
             "normal": "notifications@github.com", "name": "GitHub"},
        ])
        self.assertEqual(rows, ["ada@example.com", "notifications@github.com"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
