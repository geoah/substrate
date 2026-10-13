"""The google sync bodies' account patches, without a database or uv.

bodyunit_test.go writes the four `sync*` function bodies of
kinds/providers.substrate.reamde.dev/google/bundle.yaml into the directory
named by SUBSTRATE_BODIES_DIR and runs this file under `python3 -I`. Each
body's top level is exec'd into its own namespace, exactly the code the
runner registers, so a case calls the shipped helper and not a copy of it.
The bodies import the standard library alone.
"""

import datetime
import io
import json
import os
import unittest
import urllib.error

STREAMS = ("contacts", "gmail", "calendar", "drive")
NOW = "2026-10-13T10:00:00Z"
REQUESTED = "2026-10-13T09:59:00Z"
EARLIER = "2026-10-13T09:00:00Z"
WALL = "2026-10-13T10:02:00Z"
PAST = "2020-01-01T00:00:00Z"


def _load(stream):
    path = os.path.join(os.environ["SUBSTRATE_BODIES_DIR"], "sync%s.py" % stream)
    with open(path, encoding="utf-8") as f:
        source = f.read()
    namespace = {"__name__": "sync" + stream}
    exec(compile(source, path, "exec"), namespace)
    return namespace


BODIES = {stream: _load(stream) for stream in STREAMS}


def _account(**props):
    base = {"tokenStatus": "connected"}
    for stream in STREAMS:
        base["enabled" + stream.capitalize()] = True
    base.update(props)
    return base


class _Effects:
    def __init__(self):
        self.ops = []

    def put(self, kind, rid, properties=None, **_):
        self.ops.append(("put", kind, rid, properties))

    def patch(self, kind, rid, properties=None, **_):
        self.ops.append(("patch", kind, rid, properties))

    def delete(self, kind, rid, **_):
        self.ops.append(("delete", kind, rid, None))


class _Sink(_Effects):
    """Both doors a `_erroring` copy writes through: contacts and calendar
    patch `host.effects`, gmail and drive a batch."""

    def __init__(self):
        super().__init__()
        self.effects = self


class _Page:
    def __init__(self, resume):
        self._resume = resume

    def resume(self):
        return self._resume

    def more(self, cursor):
        return {"more": cursor}


class _Host:
    def __init__(self, resume=None):
        self.effects = _Effects()
        self.page = _Page(resume)
        self.logs = []

    def log(self, line):
        self.logs.append(line)


class TraitRollupTest(unittest.TestCase):
    """#817: a healthy stream writes another enabled stream's failure."""

    def stamp(self, stream, props, state="ok", started=""):
        # No `started` is a record-sourced delivery, which the dispatcher
        # settles; a scheduled run passes when its walk began.
        return BODIES[stream]["_trait_stamp"](
            props, stream, state, "%s is current" % stream, NOW, completed=True,
            started=started)

    def test_a_healthy_stream_writes_another_streams_failure(self):
        for stream in STREAMS:
            broken = "calendar" if stream == "contacts" else "contacts"
            props = _account(syncStreams={broken: {
                "state": "erroring", "message": "erroring: reconnect needed"}})
            with self.subTest(stream=stream):
                out = self.stamp(stream, props)
                self.assertEqual(out.get("syncState"), "erroring")
                self.assertEqual(out.get("syncMessage"), "erroring: reconnect needed")
                # The settle keeps the pair the failing stream wrote.
                self.assertNotIn("syncError", out)
                self.assertNotIn("syncErrorAt", out)
                self.assertEqual(out["syncStreams"][stream]["state"], "ok")

    def test_a_scheduled_run_leaves_the_stored_state(self):
        # Nothing settles a scheduled run, so the state another stream stored
        # after this invocation read the account is not overwritten.
        for stream in ("gmail", "drive"):
            props = _account(syncStreams={"contacts": {
                "state": "erroring", "message": "erroring: reconnect needed"}})
            with self.subTest(stream=stream):
                out = self.stamp(stream, props, started=EARLIER)
                self.assertNotIn("syncState", out)
                self.assertNotIn("syncMessage", out)
                self.assertEqual(out["syncStreams"][stream]["state"], "ok")

    def test_erroring_outranks_throttled(self):
        for stream in ("contacts", "drive"):
            props = _account(syncStreams={
                "gmail": {"state": "throttled", "message": "throttled: 429"},
                "calendar": {"state": "erroring", "message": "erroring: 403"}})
            with self.subTest(stream=stream):
                out = self.stamp(stream, props)
                self.assertEqual((out.get("syncState"), out.get("syncMessage")),
                                 ("erroring", "erroring: 403"))
        props = _account(syncStreams={"gmail": {"state": "throttled",
                                                "message": "throttled: 429"}})
        out = self.stamp("drive", props)
        self.assertEqual((out.get("syncState"), out.get("syncMessage")),
                         ("throttled", "throttled: 429"))

    def test_a_legacy_sentence_alone_is_a_failure(self):
        props = _account(contactsSyncStatus="erroring: reconnect needed")
        out = self.stamp("gmail", props)
        self.assertEqual((out.get("syncState"), out.get("syncMessage")),
                         ("erroring", "erroring: reconnect needed"))

    def test_a_disabled_streams_failure_is_not_the_accounts(self):
        props = _account(enabledContacts=False, syncStreams={"contacts": {
            "state": "erroring", "message": "erroring: reconnect needed"}})
        out = self.stamp("gmail", props)
        self.assertEqual((out.get("syncState"), out.get("syncMessage")),
                         ("ok", "gmail is current"))

    def test_a_failure_writes_its_own_state_and_error(self):
        props = _account(syncStreams={"contacts": {"state": "throttled",
                                                   "message": "throttled: 429"}})
        out = self.stamp("gmail", props, state="erroring")
        self.assertEqual(out.get("syncState"), "erroring")
        self.assertEqual(out.get("syncError"), "gmail is current")
        self.assertEqual(out.get("syncErrorAt"), NOW)

    def test_each_stream_writes_its_own_entry_alone(self):
        # The engine merges `syncStreams` per stream, so an entry read with the
        # account is never written back over the one its stream wrote since.
        for stream in STREAMS:
            props = _account(syncStreams={s: {"state": "ok", "requestedAck": EARLIER}
                                          for s in STREAMS})
            with self.subTest(stream=stream):
                out = self.stamp(stream, props)
                self.assertEqual(list(out["syncStreams"]), [stream])

    def test_a_rate_limit_does_not_hide_a_broken_stream(self):
        props = _account(syncStreams={"drive": {
            "state": "erroring", "message": "erroring: needs reconsent"}})
        for stream in ("contacts", "gmail", "calendar"):
            with self.subTest(stream=stream):
                out = self.stamp(stream, props, state="throttled")
                self.assertEqual((out.get("syncState"), out.get("syncMessage")),
                                 ("erroring", "erroring: needs reconsent"))
                self.assertEqual(out["syncStreams"][stream]["state"], "throttled")
                # The newest failure is still the pair's.
                self.assertEqual(out.get("syncError"), "%s is current" % stream)
                self.assertEqual(out.get("syncErrorAt"), NOW)
        props = _account(syncStreams={"drive": {
            "state": "throttled", "message": "throttled: 429"}})
        out = self.stamp("gmail", props, state="throttled")
        self.assertEqual((out.get("syncState"), out.get("syncMessage")),
                         ("throttled", "gmail is current"))

    def test_the_four_copies_agree(self):
        variants = {
            "throttled elsewhere": _account(syncRequestedAt=REQUESTED, syncStreams={
                "gmail": {"state": "throttled", "message": "throttled: 429", "pending": 2},
                "calendar": {"state": "ok", "requestedAck": REQUESTED}}),
            "erroring elsewhere": _account(syncErrorAt=EARLIER, syncStreams={
                "gmail": {"state": "throttled", "message": "throttled: 429"},
                "drive": {"state": "erroring", "message": "erroring: 403"}}),
        }
        for name, props in variants.items():
            for stream in STREAMS:
                for state in ("ok", "throttled", "erroring"):
                    for started in ("", EARLIER):
                        outs = [body["_trait_stamp"](props, stream, state, "fine", NOW,
                                                     completed=True, started=started,
                                                     progress={"phase": "p", "done": 1,
                                                               "total": 1, "pending": 0})
                                for body in BODIES.values()]
                        outs += [body["_trait_restate"](props, stream, state, "fine",
                                                        NOW, asked, bool(started))
                                 for body in BODIES.values() for asked in (False, True)]
                        with self.subTest(props=name, stream=stream, state=state,
                                          started=started):
                            stamps, restates = outs[:4], outs[4:]
                            for out in stamps[1:]:
                                self.assertEqual(out, stamps[0])
                            for out in restates[2::2]:
                                self.assertEqual(out, restates[0])
                            for out in restates[3::2]:
                                self.assertEqual(out, restates[1])


class UnstartedAckTest(unittest.TestCase):
    """#818: gmail and drive answer a record-sourced request on the no-budget
    path, through `main`, where the on-request trigger fired again on every
    change to the account."""

    def account(self, stream, **props):
        body = BODIES[stream]
        values = _account(syncRequestedAt=REQUESTED, **props)
        values[stream + "SyncRequestedAck"] = EARLIER
        return {"id": "acct-1", "kind": body["ACCOUNT_TYPE"], "token": "token",
                "properties": values}

    def run_main(self, stream, acct, record=True, resume=None, budget_ms=0, clock=None):
        body = BODIES[stream]
        cfg = {"accounts": [acct]}
        if budget_ms:
            cfg["inputs"] = {"client": {"properties": {"drainBudgetMs": budget_ms}}}
        envelope = {"record": {"kind": acct["kind"], "id": acct["id"]}} if record else {}
        host = _Host(resume)
        real = body["_clock"]
        if clock is not None:
            body["_clock"] = clock
        try:
            out = body["main"]({"config": cfg, "envelope": envelope}, host)
        finally:
            body["_clock"] = real
        return out, host

    def answered(self, stream, host):
        return [props for op, _, rid, props in host.effects.ops
                if op == "patch" and rid == "acct-1"
                and (props or {}).get(stream + "SyncRequestedAck") == REQUESTED]

    def assert_answered(self, stream, host):
        patches = self.answered(stream, host)
        self.assertEqual(len(patches), 1, host.effects.ops)
        stamp = patches[0]
        entry = stamp["syncStreams"][stream]
        self.assertEqual((entry["state"], entry["requestedAck"], entry["pending"]),
                         ("ok", REQUESTED, 1))
        self.assertEqual(stamp["syncProgress"]["pending"], 1)

    def stepping_clock(self):
        # One second per read: a one-millisecond budget is spent at the plan
        # phase's first look, whatever the machine's speed.
        ticks = iter(range(1_000_000, 2_000_000))
        return lambda: float(next(ticks))

    def test_the_plan_phase_answers_a_record_sourced_request(self):
        for stream in ("gmail", "drive"):
            with self.subTest(stream=stream):
                out, host = self.run_main(stream, self.account(stream), budget_ms=1,
                                          clock=self.stepping_clock())
                self.assertEqual(out, {})
                self.assert_answered(stream, host)

    def test_the_plan_phase_leaves_a_scheduled_request_owed(self):
        for stream in ("gmail", "drive"):
            with self.subTest(stream=stream):
                out, host = self.run_main(stream, self.account(stream), record=False,
                                          budget_ms=1, clock=self.stepping_clock())
                self.assertEqual(out, {})
                self.assertEqual(self.answered(stream, host), [])
                self.assertEqual([op for op in host.effects.ops if op[0] == "patch"], [])

    def test_the_backfill_answers_a_record_sourced_request(self):
        for stream in ("gmail", "drive"):
            body = BODIES[stream]
            cur = body["_fresh"](["acct-1"])
            cur.update({"phase": "backfill", "scheduled": False,
                        "acctCalls": body["MAX_CALLS"]})
            with self.subTest(stream=stream):
                out, host = self.run_main(stream, self.account(stream), resume=cur)
                self.assertEqual(out, {})
                self.assertTrue(any("no invocation budget left to start" in line
                                    for line in host.logs), host.logs)
                self.assert_answered(stream, host)

    def test_a_failing_stream_keeps_its_failure(self):
        # Nothing called Google, so the stream's last failure stands, and its
        # error pair keeps the time the failure was written.
        for stream in ("gmail", "drive"):
            acct = self.account(stream, syncStreams={stream: {
                "state": "erroring", "message": "erroring: reconnect needed"}})
            with self.subTest(stream=stream):
                _, host = self.run_main(stream, acct, budget_ms=1,
                                        clock=self.stepping_clock())
                patches = self.answered(stream, host)
                self.assertEqual(len(patches), 1, host.effects.ops)
                stamp = patches[0]
                entry = stamp["syncStreams"][stream]
                self.assertEqual((entry["state"], entry["message"], entry["requestedAck"]),
                                 ("erroring", "erroring: reconnect needed", REQUESTED))
                self.assertEqual(stamp.get("syncState"), "erroring")
                self.assertNotIn("syncError", stamp)
                self.assertNotIn("syncErrorAt", stamp)

    def test_an_answered_request_writes_nothing(self):
        for stream in ("gmail", "drive"):
            acct = self.account(stream)
            acct["properties"][stream + "SyncRequestedAck"] = REQUESTED
            with self.subTest(stream=stream):
                _, host = self.run_main(stream, acct, budget_ms=1,
                                        clock=self.stepping_clock())
                self.assertEqual([op for op in host.effects.ops if op[0] == "patch"], [])


class _Refusing:
    """An opener whose every call Google answers with one HTTP error."""

    def __init__(self, code, body, headers=None):
        self.code, self.body, self.headers, self.calls = code, body, headers, 0

    def open(self, req, timeout=None):
        self.calls += 1
        raise urllib.error.HTTPError(req.full_url, self.code, "refused",
                                     dict(self.headers or {}),
                                     io.BytesIO(json.dumps(self.body).encode()))


class RepeatedFailureTest(unittest.TestCase):
    """#817: a record-sourced delivery that meets the same failure again
    writes its state back over the dispatcher's `running`, whose settle would
    otherwise turn it into `ok` and clear the error pair."""

    UNAVAILABLE = (503, {"error": {"code": 503, "message": "backend unavailable"}})
    NO_SCOPE = (403, {"error": {
        "code": 403, "status": "PERMISSION_DENIED",
        "message": "Request had insufficient authentication scopes.",
        "details": [{"reason": "ACCESS_TOKEN_SCOPE_INSUFFICIENT"}]}})
    RATE_LIMITED = (429, {"error": {"code": 429, "status": "RESOURCE_EXHAUSTED",
                                    "message": "Quota exceeded."}},
                    {"Retry-After": "120"})

    def run_main(self, stream, props, refusal, record=True):
        body = BODIES[stream]
        acct = {"id": "acct-1", "kind": body["ACCOUNT_TYPE"], "token": "token",
                "properties": props}
        envelope = {"record": {"kind": acct["kind"], "id": acct["id"]}} if record else {}
        host = _Host()
        real, opener = body["_OPENER"], _Refusing(*refusal)
        body["_OPENER"] = opener
        try:
            body["main"]({"config": {"accounts": [acct]}, "envelope": envelope}, host)
        finally:
            body["_OPENER"] = real
        # A run that never asked Google repeated no failure.
        self.assertGreater(opener.calls, 0, host.logs)
        return [props for op, _, rid, props in host.effects.ops
                if op == "patch" and rid == "acct-1"]

    def failed_once(self, stream, refusal, state="erroring"):
        """The account as the first failing delivery left it, then as the
        dispatcher's start of the next one leaves it: `running`."""
        props = _account()
        patches = self.run_main(stream, props, refusal)
        self.assertEqual(len(patches), 1, patches)
        first = patches[0]
        self.assertEqual(first.get("syncState"), state)
        self.assertIn("syncErrorAt", first)
        stored = dict(props)
        stored.update({k: v for k, v in first.items() if k != "syncStreams"})
        stored["syncStreams"] = dict(first["syncStreams"])
        stored["syncState"] = "running"
        return stored

    def assert_restated(self, stream, stored, patches, state="erroring"):
        self.assertEqual(len(patches), 1, patches)
        stamp = patches[0]
        self.assertEqual(stamp.get("syncState"), state)
        self.assertEqual(stamp["syncStreams"][stream]["state"], state)
        self.assertEqual(stamp["syncStreams"][stream]["message"],
                         stored[stream + "SyncStatus"])
        # Nothing new failed: the pair keeps the first failure's instant.
        self.assertNotIn("syncError", stamp)
        self.assertNotIn("syncErrorAt", stamp)
        self.assertNotIn(stream + "SyncStatus", stamp)

    def test_a_repeated_initial_failure_stays_erroring(self):
        for stream in STREAMS:
            with self.subTest(stream=stream):
                stored = self.failed_once(stream, self.UNAVAILABLE)
                self.assertNotIn(stream + "LastSyncedAt", stored)
                self.assert_restated(stream, stored,
                                     self.run_main(stream, stored, self.UNAVAILABLE))

    def test_a_repeated_drive_reconsent_stays_erroring(self):
        stored = self.failed_once("drive", self.NO_SCOPE)
        self.assertEqual(stored.get("driveSyncState"), "needsreconsent")
        patches = self.run_main("drive", stored, self.NO_SCOPE)
        self.assert_restated("drive", stored, patches)
        self.assertNotIn("driveSyncState", patches[0])

    def test_a_scheduled_repeat_writes_nothing(self):
        # Nothing settles a scheduled run, so the stored state stands.
        for stream in STREAMS:
            with self.subTest(stream=stream):
                stored = self.failed_once(stream, self.UNAVAILABLE)
                stored["syncState"] = "erroring"
                self.assertEqual(
                    self.run_main(stream, stored, self.UNAVAILABLE, record=False), [])

    def test_a_scheduled_queue_keeps_its_provenance(self):
        # A scheduled queue reaches its second account through a continuation
        # cursor, which must still say scheduled: nothing settles that run.
        for stream in STREAMS:
            body = BODIES[stream]
            stored = self.failed_once(stream, self.UNAVAILABLE)
            stored["syncState"] = "erroring"
            accounts = [{"id": aid, "kind": body["ACCOUNT_TYPE"], "token": "token",
                         "properties": dict(stored)} for aid in ("acct-1", "acct-2")]
            real, opener = body["_OPENER"], _Refusing(*self.UNAVAILABLE)
            body["_OPENER"] = opener
            ops, logs, resume = [], [], None
            try:
                for _ in range(2):
                    host = _Host(resume)
                    out = body["main"]({"config": {"accounts": accounts},
                                        "envelope": {}}, host)
                    ops += host.effects.ops
                    logs += host.logs
                    resume = ((out or {}).get("more") or {}).get("more")
                    if not resume:
                        break
            finally:
                body["_OPENER"] = real
            with self.subTest(stream=stream):
                self.assertTrue(any("acct-2" in line for line in logs), logs)
                self.assertEqual([op for op in ops if op[0] == "patch"], [])

    def test_a_repeated_rate_limit_keeps_the_error_pair(self):
        # The new wall is written, and is no news: the pair keeps its instant.
        for stream in STREAMS:
            with self.subTest(stream=stream):
                stored = self.failed_once(stream, self.RATE_LIMITED, state="throttled")
                patches = self.run_main(stream, stored, self.RATE_LIMITED)
                self.assert_restated(stream, stored, patches, state="throttled")
                self.assertIn("retryNotBefore", patches[0])

    def test_a_scheduled_repeated_rate_limit_writes_only_the_wall(self):
        for stream in STREAMS:
            with self.subTest(stream=stream):
                stored = self.failed_once(stream, self.RATE_LIMITED, state="throttled")
                stored.update({"syncState": "throttled", "retryNotBefore": PAST})
                patches = self.run_main(stream, stored, self.RATE_LIMITED, record=False)
                self.assertEqual(len(patches), 1, patches)
                self.assertEqual(list(patches[0]), ["retryNotBefore"])
                self.assertGreater(patches[0]["retryNotBefore"], stored["syncErrorAt"])

    def test_a_repeat_after_a_reconnect_writes_the_error_pair(self):
        # A reconnect clears the pair and leaves the stream's sentence, so the
        # same failure afterwards is the only failure the account records.
        for stream in STREAMS:
            with self.subTest(stream=stream):
                stored = self.failed_once(stream, self.UNAVAILABLE)
                stored.update({"syncError": None, "syncErrorAt": None})
                patches = self.run_main(stream, stored, self.UNAVAILABLE)
                self.assertEqual(len(patches), 1, patches)
                stamp = patches[0]
                self.assertEqual(stamp.get("syncState"), "erroring")
                self.assertEqual(stamp.get("syncError"), stored[stream + "SyncStatus"])
                self.assertTrue(stamp.get("syncErrorAt"), stamp)

    def test_a_repeat_answering_a_request_moves_the_error_pair(self):
        for stream in STREAMS:
            with self.subTest(stream=stream):
                stored = self.failed_once(stream, self.UNAVAILABLE)
                stored.update({"syncRequestedAt": REQUESTED,
                               stream + "SyncRequestedAck": EARLIER})
                patches = self.run_main(stream, stored, self.UNAVAILABLE)
                self.assertEqual(len(patches), 1, patches)
                stamp = patches[0]
                self.assertEqual(stamp.get(stream + "SyncRequestedAck"), REQUESTED)
                self.assertEqual(stamp.get("syncState"), "erroring")
                self.assertIn("syncErrorAt", stamp)

    def test_a_repeated_rate_limit_does_not_hide_a_broken_stream(self):
        for stream in ("contacts", "gmail", "calendar"):
            with self.subTest(stream=stream):
                stored = self.failed_once(stream, self.RATE_LIMITED, state="throttled")
                stored["syncStreams"]["drive"] = {
                    "state": "erroring", "message": "erroring: needs reconsent"}
                patches = self.run_main(stream, stored, self.RATE_LIMITED)
                self.assertEqual(len(patches), 1, patches)
                stamp = patches[0]
                self.assertEqual((stamp.get("syncState"), stamp.get("syncMessage")),
                                 ("erroring", "erroring: needs reconsent"))
                self.assertEqual(stamp["syncStreams"][stream]["state"], "throttled")
                self.assertNotIn("syncErrorAt", stamp)


class ErroringAgreementTest(unittest.TestCase):
    """The four `_erroring` copies write one account patch for one failure
    their stream already stated, whichever stream they keep."""

    REASON = "google returned HTTP 503"
    CLOCK = datetime.datetime(2026, 10, 13, 10, 0, 0, tzinfo=datetime.timezone.utc)

    # name: (retry_at, scheduled, stored properties, whether the patch
    # carries the error pair or None where nothing is patched).
    SCENARIOS = {
        "repeat": ("", False, {}, False),
        "scheduled repeat": ("", True, {}, None),
        "repeated 429": (WALL, False, {}, False),
        "scheduled repeated 429": (WALL, True, {}, False),
        "new request": ("", False, {"syncRequestedAt": REQUESTED}, True),
        "scheduled new request": ("", True, {"syncRequestedAt": REQUESTED}, True),
        "pair cleared": ("", False, {"syncError": None, "syncErrorAt": None}, True),
    }

    def patch(self, stream, retry_at, scheduled, stored):
        body = BODIES[stream]
        status = "erroring: " + self.REASON
        state = "throttled" if retry_at else "erroring"
        props = _account(syncState="running", syncError=status, syncErrorAt=EARLIER,
                         syncStreams={stream: {"state": state, "message": status}})
        props[stream + "SyncStatus"] = status
        props[stream + "SyncRequestedAck"] = EARLIER
        props.update(stored)
        acct = {"id": "acct-1", "kind": body["ACCOUNT_TYPE"], "properties": props}
        sink, real = _Sink(), body["_now"]
        body["_now"] = lambda: self.CLOCK
        try:
            body["_erroring"](sink, acct, self.REASON, retry_at, scheduled=scheduled)
        finally:
            body["_now"] = real
        self.assertLessEqual(len(sink.ops), 1, sink.ops)
        if not sink.ops:
            return None
        # One spelling for every stream's own keys, so the copies compare.
        out = {}
        for key, value in sink.ops[0][3].items():
            if key == "syncStreams":
                value = {"<stream>": value[stream]}
            elif key.startswith(stream):
                key = "<stream>" + key[len(stream):]
            out[key] = value
        return out

    def test_the_four_copies_agree(self):
        for name, (retry_at, scheduled, stored, pair) in self.SCENARIOS.items():
            outs = [self.patch(stream, retry_at, scheduled, stored)
                    for stream in STREAMS]
            with self.subTest(scenario=name):
                for out in outs[1:]:
                    self.assertEqual(out, outs[0])
                if pair is None:
                    self.assertIsNone(outs[0])
                    continue
                self.assertIsNotNone(outs[0])
                self.assertEqual("syncErrorAt" in outs[0], pair, outs[0])
                self.assertEqual("retryNotBefore" in outs[0], bool(retry_at), outs[0])


if __name__ == "__main__":
    unittest.main(verbosity=2)
