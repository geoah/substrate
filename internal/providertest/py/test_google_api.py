"""The google tools' shared module: where a token may go, which account a
call acts as, and the mail and calendar shapes it builds.

bodyunit_test.go writes the `google_api.py` module of
kinds/providers.substrate.reamde.dev/google/bundle.yaml into the directory
named by SUBSTRATE_BODIES_DIR and runs this file with `-I`. No case opens a
socket: `request` is replaced by a recorder wherever a call would be made.
TestGoogleToolsCallGoogle in Go runs the bodies through the runner.
"""

import base64
import email
import importlib.util
import os
import unittest

SCOPE = "https://www.googleapis.com/auth/"


def _load():
    path = os.path.join(os.environ["SUBSTRATE_BODIES_DIR"], "google_api.py")
    spec = importlib.util.spec_from_file_location("google_api", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


api = _load()


def acct(email_addr, toggles, scopes=None, **extra):
    props = {"email": email_addr}
    for t in toggles:
        props[t] = True
    props["grantedScopes"] = scopes if scopes is not None else [
        SCOPE + "gmail.modify", SCOPE + "calendar.events", SCOPE + "drive.readonly"]
    out = {"id": "id-" + email_addr, "properties": props, "token": "at-1"}
    out.update(extra)
    return out


def config(*accounts, base=""):
    cfg = {"accounts": list(accounts)}
    if base:
        cfg["inputs"] = {"client": {"properties": {"apiBase": base}}}
    return {"config": cfg}


class PinTest(unittest.TestCase):
    def test_google_origins_on_443_alone(self):
        base = api.GMAIL_API
        self.assertTrue(api._pinned("https://gmail.googleapis.com/gmail/v1/x", base))
        self.assertTrue(api._pinned("https://www.googleapis.com/drive/v3/files", base))
        self.assertTrue(api._pinned("https://www.googleapis.com:443/x", base))
        for url in ("http://gmail.googleapis.com/x", "https://gmail.googleapis.com:8443/x",
                    "https://gmail.googleapis.com.evil.example.com/x",
                    "https://evil.example.com/x"):
            self.assertFalse(api._pinned(url, base), url)

    def test_a_loopback_base_admits_itself(self):
        self.assertTrue(api._pinned("http://127.0.0.1:9/gmail/v1/x", "http://127.0.0.1:9"))
        self.assertFalse(api._pinned("http://127.0.0.2:9/x", "http://127.0.0.1:9"))

    def test_api_base_is_google_or_loopback(self):
        self.assertEqual(api.api_base(config(), api.GOOGLE_API), api.GOOGLE_API)
        self.assertEqual(api.api_base(config(base="http://localhost:8080/"), api.GOOGLE_API),
                         "http://localhost:8080")
        with self.assertRaisesRegex(RuntimeError, "only a loopback host"):
            api.api_base(config(base="https://googleapis.example.com"), api.GOOGLE_API)


class AccountTest(unittest.TestCase):
    def test_the_one_account_that_can(self):
        a = acct("ada@example.com", ["enabledGmailWrite"])
        b = acct("bob@example.com", ["enabledGmail"])
        got, token = api.account(config(a, b), "", api.GMAIL_WRITE)
        self.assertIs(got, a)
        self.assertEqual(token, "at-1")

    def test_named_by_record_id(self):
        a = acct("ada@example.com", ["enabledDrive"])
        b = acct("bob@example.com", ["enabledDrive"])
        got, _ = api.account(config(a, b), "id-bob@example.com", api.DRIVE_READ)
        self.assertIs(got, b)

    def test_none_of_several_can(self):
        a = acct("ada@example.com", ["enabledGmail"])
        b = acct("bob@example.com", ["enabledGmail"])
        with self.assertRaisesRegex(RuntimeError, "no connected account may .*Calendar editing"):
            api.account(config(a, b), "", api.CALENDAR_WRITE)

    def test_a_grant_without_scopes_recorded_is_left_to_google(self):
        a = acct("ada@example.com", ["enabledCalendarWrite"], scopes=[])
        got, _ = api.account(config(a), "", api.CALENDAR_WRITE)
        self.assertIs(got, a)

    def test_the_freebusy_read_needs_the_calendar_stream(self):
        a = acct("ada@example.com", ["enabledCalendarWrite"])
        with self.assertRaisesRegex(RuntimeError, "read calendars"):
            api.account(config(a), "", api.CALENDAR_READ)


class MailTest(unittest.TestCase):
    def reply(self, headers, me="ada@example.com", reply_all=False):
        def fake(input, token, method, origin, path, params=None, body=None, text=False):
            return {"id": "m1", "threadId": "t1",
                    "payload": {"headers": [{"name": k, "value": v} for k, v in headers]}}

        real = api.request
        api.request = fake
        try:
            return api.reply_context({}, "at-1", "m1", me, reply_all)
        finally:
            api.request = real

    def test_reply_to_wins_over_from(self):
        ctx = self.reply([("From", "Sam <sam@example.com>"),
                          ("Reply-To", "list@example.com"), ("Subject", "RE: x")])
        self.assertEqual(ctx["to"], ["list@example.com"])
        self.assertEqual(ctx["subject"], "RE: x")

    def test_a_reply_to_ones_own_message_goes_where_it_went(self):
        ctx = self.reply([("From", "Ada <ada@example.com>"),
                          ("To", "sam@example.com, Bea <bea@example.com>"),
                          ("Message-ID", "<m@x>")])
        self.assertEqual(ctx["to"], ["sam@example.com", "Bea <bea@example.com>"])
        self.assertEqual(ctx["references"], "<m@x>")

    def test_reply_all_leaves_out_the_owner_and_repeats(self):
        ctx = self.reply([("From", "sam@example.com"),
                          ("To", "ADA@example.com, sam@example.com"),
                          ("Cc", "bea@example.com, bea@example.com")], reply_all=True)
        self.assertEqual(ctx["to"], ["sam@example.com"])
        self.assertEqual(ctx["cc"], ["bea@example.com"])

    def test_compose_spells_names_and_html(self):
        raw, thread, summary = api.compose(
            {"to": ["Ada Lovelace <ada@example.com>"], "subject": "Hi",
             "html": "<p>Hello <b>there</b></p>"}, {}, "at-1", "me@example.com")
        msg = email.message_from_bytes(base64.urlsafe_b64decode(raw))
        self.assertEqual(thread, "")
        self.assertEqual(msg["To"], "Ada Lovelace <ada@example.com>")
        self.assertTrue(msg.is_multipart())
        kinds = [p.get_content_type() for p in msg.walk()]
        self.assertIn("text/plain", kinds)
        self.assertIn("text/html", kinds)
        self.assertEqual(summary, {"to": ["Ada Lovelace <ada@example.com>"], "subject": "Hi"})

    def test_two_addresses_in_one_entry_are_refused(self):
        with self.assertRaisesRegex(ValueError, "not one email address"):
            api.address("a@example.com, b@example.com", "to")

    def test_html_text(self):
        self.assertEqual(
            api.html_text("<html><head><title>t</title></head><body>"
                          "<script>x()</script><div>One&nbsp;two</div><ul><li>a</li>"
                          "<li>b</li></ul></body></html>"),
            "One two\n\na\n\nb")

    def test_message_text_prefers_the_plain_part(self):
        def b64(s):
            return base64.urlsafe_b64encode(s.encode()).decode().rstrip("=")
        payload = {"mimeType": "multipart/alternative", "parts": [
            {"mimeType": "text/plain", "body": {"data": b64("plain one")}},
            {"mimeType": "text/html", "body": {"data": b64("<p>rich</p>")}},
        ]}
        self.assertEqual(api.message_text(payload), ("plain one", []))


class TimeTest(unittest.TestCase):
    def test_when(self):
        self.assertEqual(api.when("2026-11-12", "", "start"), {"date": "2026-11-12"})
        self.assertEqual(api.when("2026-11-12T10:00+01:00", "", "start"),
                         {"dateTime": "2026-11-12T10:00:00+01:00"})
        self.assertEqual(api.when("2026-11-12T10:00", "Europe/Athens", "start"),
                         {"dateTime": "2026-11-12T10:00:00", "timeZone": "Europe/Athens"})
        with self.assertRaisesRegex(ValueError, "no offset and no time zone"):
            api.when("2026-11-12T10:00", "", "start")
        with self.assertRaisesRegex(ValueError, "neither a date"):
            api.when("next tuesday", "UTC", "start")

    def test_end_after_keeps_the_offset_and_the_zone(self):
        self.assertEqual(api.end_after({"dateTime": "2026-11-12T23:30:00+01:00"}),
                         {"dateTime": "2026-11-13T00:30:00+01:00"})
        self.assertEqual(api.end_after({"date": "2026-12-31"}), {"date": "2027-01-01"})

    def test_length_of_needs_one_shape(self):
        start = {"dateTime": "2026-11-12T10:00:00Z"}
        self.assertEqual(api.length_of(start, {"dateTime": "2026-11-12T11:30:00+00:00"}).seconds, 5400)
        self.assertIsNone(api.length_of(start, {"date": "2026-11-13"}))
        self.assertIsNone(api.length_of({}, {}))


if __name__ == "__main__":
    unittest.main()
