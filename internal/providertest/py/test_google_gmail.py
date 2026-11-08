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
    """A relay address names a thread or a notification reason, never a
    party."""

    def relay(self, addr):
        normal = BODY["_mailbox"](addr)
        self.assertTrue(normal, "%s does not parse" % addr)
        return BODY["_relay"](normal)

    def test_a_github_reply_address_is_a_relay(self):
        self.assertTrue(self.relay("reply+%s@reply.github.com" % TOKEN))
        # The domain is normalised and the prefix compared ignoring case.
        self.assertTrue(self.relay("Reply+%s@Reply.GitHub.com" % TOKEN))

    def test_a_github_notification_reason_is_a_relay(self):
        # Every local part at exactly noreply.github.com, in any case.
        for addr in (
                "repo@noreply.github.com",
                "author@noreply.github.com",
                "mention@noreply.github.com",
                "subscribed@noreply.github.com",
                "comment@noreply.github.com",
                "state_change@noreply.github.com",
                "review_requested@noreply.github.com",
                "team_mention@noreply.github.com",
                "push@noreply.github.com",
                "ci_activity@noreply.github.com",
                "assign@noreply.github.com",
                "manual@noreply.github.com",
                "your_activity@noreply.github.com",
                "Mention@NoReply.GitHub.com"):
            with self.subTest(addr=addr):
                self.assertTrue(self.relay(addr))

    def test_an_identity_is_not(self):
        for addr in (
                # GitHub's sender is one identity, shared by every
                # notification, and so is its no-reply sender.
                "notifications@github.com",
                "noreply@github.com",
                # A person's commit address sits UNDER the reasons' domain.
                "12345+grace@users.noreply.github.com",
                "12345+Grace@Users.NoReply.GitHub.com",
                # The reasons' domain under another, or inside a longer label.
                "repo@noreply.github.com.example.com",
                "repo@xnoreply.github.com",
                # The prefix alone, with no token after it.
                "reply+@reply.github.com",
                # The prefix at another domain, and the domain under another.
                "reply+%s@github.com" % TOKEN,
                "reply+%s@reply.github.com.example.com" % TOKEN,
                "reply+%s@example.com" % TOKEN,
                # Another local part at the reply domain.
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

    def test_a_notification_reason_keeps_its_name_and_no_row(self):
        entries, rows = BODY["_addrs"]([
            "owner/repo <repo@noreply.github.com>, "
            "Mention <Mention@NoReply.GitHub.com>, "
            "Grace <12345+grace@users.noreply.github.com>"])
        self.assertEqual(entries, [
            {"value": "repo@noreply.github.com", "name": "owner/repo"},
            {"value": "Mention@NoReply.GitHub.com", "name": "Mention"},
            {"value": "12345+grace@users.noreply.github.com",
             "normal": "12345+grace@users.noreply.github.com", "name": "Grace"},
        ])
        self.assertEqual(rows, ["12345+grace@users.noreply.github.com"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
