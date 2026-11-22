#!/usr/bin/env python3
"""One invented day, written into a dev substrate as real records.

Substrate Today is drawn from records, so trying it needs a repository that
holds a day: projects with flags, tasks, suggested tasks with the assistant's
advice, medication and routines with some logs, a calendar with overlaps,
what happened overnight, and a change the assistant suggests. This writes
one, dated today on this machine's clock. Every person, project, message and
health detail in it is made up.

    apps/today/dev/server.sh up          # the dev server and its packages
    apps/today/dev/seed.py               # wipe the day's kinds, then write the day
    apps/today/dev/seed.py --keep        # write over what is there, wiping nothing
    apps/today/dev/seed.py urgent checked held unchecked   # put urgent marks on
    apps/today/dev/seed.py urgent none   # take them off again
    apps/today/dev/seed.py wipe          # only wipe

It reads the server and a token from .dev/today-dev.env (server.sh writes
it), or from --server and --token. A wipe purges EVERY record of the kinds
the day uses in that repository, so it refuses a server that is not on this
machine unless --allow-remote is given.

The ids of what it writes are derived from short names (uuid5), in the
shape a minted kind takes, so a second run writes the same records.
"""

from __future__ import annotations

import argparse
import datetime
import json
import pathlib
import sys
import urllib.error
import urllib.parse
import urllib.request
import uuid
from zoneinfo import ZoneInfo

ROOT = pathlib.Path(__file__).resolve().parents[3]
ENV_FILE = ROOT / ".dev" / "today-dev.env"

S = "substrate.reamde.dev"
TASK, LIST = f"{S}/tasks/task", f"{S}/tasks/list"
ROUTINE, LOG = f"{S}/routines/routine", f"{S}/routines/routinelog"
MEDICATION, SCHEDULE = f"{S}/health/medication", f"{S}/health/medicationschedule"
PREFERENCE, PLAN, ADVICE = f"{S}/today/preference", f"{S}/today/plan", f"{S}/today/advice"
DIGEST, REVIEWMARK, DOSSIER = f"{S}/today/digest", f"{S}/today/reviewmark", f"{S}/today/dossier"
RETURNPOINT, RULE, PREPARATION = f"{S}/today/returnpoint", f"{S}/today/rule", f"{S}/today/preparation"
CONTACT, REQUEST, TRIGGER = f"{S}/core/contact", f"{S}/core/recordpatchrequest", f"{S}/core/trigger"
EVENT, SERIES, CALENDAR = "~/calendar/event", "~/calendar/series", "~/calendar/calendar"

# What a wipe purges whole: the kinds the day is written into and the app
# writes into. Contacts and patch requests are purged by the ids below only,
# because a repository holds others (the owner's own contact).
WIPED = [ADVICE, DOSSIER, DIGEST, REVIEWMARK, RETURNPOINT, PLAN, PREFERENCE, PREPARATION, RULE, LOG, TASK, LIST, SCHEDULE, MEDICATION, ROUTINE, EVENT, SERIES, CALENDAR]

NAMESPACE = uuid.UUID("5f6b7a00-0000-4000-8000-746f64617921")


def uid(name: str) -> str:
    """The record id for a short name: a UUID's canonical text, which a
    minted kind takes from any writer."""
    return str(uuid.uuid5(NAMESPACE, name))


# ---------------------------------------------------------------- the day

PEOPLE = {
    "tomas": "Tomas Varga", "viktor": "Viktor Lund", "henrik": "Henrik Aalto", "karim": "Karim Saleh",
    "casey": "Casey Moreau", "callum": "Callum Reyes", "marcus": "Marcus Bell", "grant": "Grant Okafor",
    "patrick": "Patrick Doyle", "stefan": "Stefan Kovac", "kenji": "Kenji Mori", "peter": "Peter Lindqvist",
    "noor": "Noor Haddad", "marco": "Marco Rinaldi", "wei": "Wei Zhao", "leo": "Leo Marsh", "jun": "Jun Park",
    "ned": "Ned Carver", "tariq": "Tariq Nasser", "wes": "Wes Tanner", "milan": "Milan Petrov",
    "rahul": "Rahul Menon", "hao": "Hao Chen", "dana": "Dana Whitlock", "maya": "Maya Keller",
    "ben": "Ben Arnold", "farid": "Farid Aziz", "imran": "Imran Sheikh",
}
OWNER_EMAIL = "you@example.com"

# (key, name, priority, what it is and where it stands)
PROJECTS = [
    ("extapi", "External API reliability", "high", "For 35 minutes overnight about one request in five failed. A release was rolled back and it has been steady since. Your check of the safety vendor endpoints is a day late."),
    ("euai", "EU AI Act", "high", "You called a short call for today at 19:30 with the data platform group. You said you would find the relevant docs first."),
    ("lantern", "Lantern", "high", "Image registration is unblocked for now. Wei posted a backend refactor and a volume API proposal, and is on leave from Monday."),
    ("hiring", "Hiring", "medium", "You interview Grant Okafor at 18:30. Peter is waiting for feedback on two interviews."),
    ("ghorgs", "GitHub orgs", "medium", "You named this as something to look into this week. Nothing has started."),
    ("security", "Security hardening", "none", "Vigil filed over 90 cluster findings overnight and none is triaged in the sources checked."),
    ("northfield", "Northfield bring-up", "none", "Northfield asked for a list of nodes to be drained for a cable swap. Stefan has it."),
    ("bastion", "Bastion", "none", "Henrik posted a proposal for Bastion's future and invited your comments."),
    ("tally", "Tally", "none", "App review flagged the last submission and your fix is in review."),
    ("home", "Home", "none", "A desk lamp and a jacket are on the way. The supplements refill is overdue."),
    ("admin", "Paperwork", "none", "Twelve open and none has a date. The passport renewal is the only one you marked high."),
    ("other", "Everything else", "none", "Leo shared the decisions from the first Product Review. Marco finished the account rename."),
]

# (key, name, project, priority, people, why, extra properties)
TASKS = [
    ("t-vigil", "Triage the Vigil findings and review the four lockdown PRs", "security", "high", ["patrick"], "Over 90 since 02:03 and no owner in the sources checked. Patrick asked for the PR review on Tuesday.", {}),
    ("t-proxy", "Get the proxy update PR approved and deployed", "security", "high", [], "Two published vulnerabilities. A bot approved; it needs a person.", {}),
    ("p-vendor", "Verify the safety vendor endpoint claims", "extapi", "high", [], "You said on Tuesday: “I will go over these tomorrow.”", {"due": -1}),
    ("t-incident", "Read the incident write-up", "extapi", "medium", [], "Due by noon.", {"dueAt": "12:00"}),
    ("t-henrik", "Read Henrik's Bastion proposal", "bastion", "medium", ["henrik"], "You see Henrik at 15:00, and comments were asked for.", {"due": 0}),
    ("t-leo", "Read the Product Review decisions", "other", "none", ["leo"], "Leo shared them at 01:52.", {}),
    ("t-lin", "Enter your score for Lin", "hiring", "medium", [], "Friday's debrief is at 21:00.", {"due": 1}),
    ("p-feedback", "Give Peter feedback on two interviews", "hiring", "none", ["peter"], "Peter is waiting before Friday's debrief.", {"due": 1}),
    ("t-pharmacy", "Order the supplements refill", "home", "low", [], "It is overdue.", {}),
    ("t-followup", "Book the follow-up visit", None, "none", ["dana"], "No date. Worth deciding on the 08:00 call.", {}),
    ("t-passport", "Renew the passport", "admin", "high", [], "The only task you ever marked high.", {"deferrals": 3}),
    ("t-tally", "Fix and resubmit Tally", "tally", "none", [], "App review flagged it.", {}),
    ("t-form", "Fill in the council form", "admin", "none", [], "Came by mail on Tuesday.", {}),
    ("t-contrib", "Reply to a contributor on PR 31", "tally", "none", [], "The contributor waits to know what goes into the next build.", {}),
    ("t-keys", "Buy security keys", "admin", "none", [], "You said it on 24 September.", {"deferrals": 5, "notBefore": 2}),
]

# The thirteen suggested tasks, each with the assistant's answer.
# (key, name, project, priority, people, where it came from, due today?, answer, reason)
SUGGESTED = [
    ("p-kenji", "Sync with Kenji about the stopped VMs", "security", "high", ["kenji"], "You said on Wednesday at 21:37: “I will catch Kenji in the morning, we are in the same office”", True, "task", "You said in the morning."),
    ("p-tomas", "Fix the 1:1 clash with Tomas", "other", "medium", ["tomas"], "You said on Wednesday at 15:06: “Yes. Fixing it.”", False, "task", "You see Tomas at 14:20, and Thursdays at 14:00 are free on your calendar."),
    ("p-casey", "Get back to Casey on renaming the shared account", "other", "medium", ["casey"], "You said on Wednesday at 15:10: “I will find out what the options are for renaming the account, not making a new one, and come back to you”", False, "task", "Marco finished the rename at 00:42. A two line reply is ready."),
    ("p-marcus", "Send Marcus a couple of projects people are working on", "lantern", "high", ["marcus"], "You said on 22 October: “I can send you a couple of projects people are working on now and next”", False, "task", "You see Marcus at 17:40."),
    ("p-dataplatform", "Find the docs for the data platform meeting", "euai", "high", [], "You said on Wednesday at 22:45: “I will try to find the relevant docs”", False, "task", "The call is at 19:30 and the 16:30 gap is the only free time before it."),
    ("p-ticket", "File an IT ticket about the platform group membership", "security", "medium", ["hao"], "You said on Wednesday at 20:54: “Let me file a ticket and talk to IT to see what is going on.”", False, "task", "The permissions sync is at 20:00."),
    ("t-stefan", "Reply to Stefan on the inventory, Halden and Brightwell", "northfield", "high", ["stefan"], "Stefan asked you directly on Tuesday.", False, "task", "Halden and Brightwell is Friday at 20:00."),
    ("t-wei", "Review Wei's refactor, PR 4188", "lantern", "medium", ["wei"], "Wei asked on Slack today at 05:09.", False, "task", "Wei is away from Monday until 22 January."),
    ("p-marco", "A week of time off and a bottle for Marco", "other", "low", ["marco"], "You said today at 00:36: “A week off and a bottle of something good are yours. What do you drink?”", False, "task", "No day was said."),
    ("p-origin", "Ask where the encryption requirement came from", "security", "low", [], "You said on Tuesday at 22:29: “I will ask, though”", False, "task", "No day was said."),
    ("o-farid", "Tell Farid what happened once the announcement is out", "other", "low", ["farid"], "You said on 11 November: “There will be an announcement, and I can give you the short version”", False, "task", "It waits on the announcement."),
    ("o-ben", "Send Ben the retro action items", "other", "low", ["ben"], "You said on 10 November: “I will send some action items over after the weekend.”", False, "task", "No day was said."),
    ("o-maya", "Offer Maya any free slot for the urgent candidate interview", "hiring", "low", ["maya"], "You said on 5 November: “Any time my calendar shows free today or tomorrow will do.”", False, "dismiss", "Two weeks old today, and the two days you offered have passed."),
]

# (name, strength, unit, category)
MEDICATIONS = {
    "vitd": ("Vitamin D", 1000, "iu", "supplement"), "magnesium": ("Magnesium", 200, "mg", "supplement"),
    "omega3": ("Omega-3", None, None, "supplement"), "zinc": ("Zinc", 15, "mg", "supplement"),
    "probiotic": ("Probiotic", None, None, "supplement"), "iron": ("Iron", 14, "mg", "supplement"),
    "k2": ("Vitamin K2", 100, "mcg", "supplement"), "b12": ("Vitamin B12", None, None, "supplement"),
}
# (key, medication, time, window, group, unit)
SCHEDULES = [
    ("am-vitd", "vitd", "07:30", "morning", "Morning medication", "tablet"),
    ("am-magnesium", "magnesium", "07:30", "morning", "Morning medication", "tablet"),
    ("am-omega3", "omega3", "07:30", "morning", "Morning medication", "capsule"),
    ("am-zinc", "zinc", "07:30", "morning", "Morning medication", "tablet"),
    ("am-probiotic", "probiotic", "07:30", "morning", "Morning medication", "capsule"),
    ("pm-magnesium", "magnesium", "19:30", "evening", "Evening medication", "tablet"),
    ("pm-iron", "iron", "19:30", "evening", "Evening medication", "tablet"),
    ("pm-k2", "k2", "19:30", "evening", "Evening medication", "capsule"),
    ("pm-zinc", "zinc", "19:30", "evening", "Evening medication", "tablet"),
]
# (key, name, category, time, window, group, status)
# The steps of one group sit a minute apart, in the order they are done: the
# pane draws a group as one row a day whatever its slots.
ROUTINES = [
    ("am-cleanser", "Cleanser", "hygiene", "07:35", "morning", "Morning skincare", "active"),
    ("am-toner", "Toner", "hygiene", "07:36", "morning", "Morning skincare", "active"),
    ("am-serum", "Vitamin C serum", "hygiene", "07:37", "morning", "Morning skincare", "active"),
    ("am-moisturiser", "Moisturiser", "hygiene", "07:38", "morning", "Morning skincare", "active"),
    ("am-sunscreen", "Sunscreen", "hygiene", "07:39", "morning", "Morning skincare", "active"),
    ("am-meditate", "Meditate", "habit", "07:45", "morning", None, "active"),
    ("pm-cleanser", "Cleanser", "hygiene", "19:35", "evening", "Evening skincare", "active"),
    ("pm-toner", "Toner", "hygiene", "19:36", "evening", "Evening skincare", "active"),
    ("pm-moisturiser", "Moisturiser", "hygiene", "19:37", "evening", "Evening skincare", "active"),
    ("pm-read", "Read 20 pages", "habit", "22:00", "evening", None, "active"),
    ("stretch", "Stretch", "exercise", "13:00", "afternoon", None, "paused"),
]

# The day's meetings: sixteen, with four overlaps.
# (key, start, end, summary, project, people, where, calendar)
MEETINGS = [
    ("physio", "08:00", "08:15", "Physiotherapy call", None, ["dana"], "Telephone", "Personal"),
    ("tomas", "14:20", "14:40", "Tomas / George", "other", ["tomas"], "Google Meet", "Work"),
    ("viktor", "14:40", "15:00", "Viktor / George", "lantern", ["viktor"], "Room 2", "Work"),
    ("henrik", "15:00", "15:20", "Henrik / George", "bastion", ["henrik"], "Google Meet", "Work"),
    ("karim", "15:00", "15:30", "Karim / George", "other", ["karim"], "Room 2", "Work"),
    ("casey", "15:40", "16:00", "Casey / George", "other", ["casey"], "Room 2", "Work"),
    ("debrief", "16:00", "16:30", "Standing hiring debrief", "hiring", ["peter", "noor"], "Google Meet", "Work"),
    ("standup", "17:00", "17:30", "Lantern standup", "lantern", ["callum", "marcus", "wei", "tomas"], "Google Meet", "Work"),
    ("callum", "17:20", "17:40", "Callum / George", "lantern", ["callum"], "Google Meet", "Work"),
    ("weekly", "17:30", "18:15", "Beacon and Lantern weekly sync", "lantern", ["marcus", "wes", "tariq", "viktor"], "Google Meet", "Work"),
    ("marcus", "17:40", "18:00", "Marcus / George", "lantern", ["marcus"], "Room 2", "Work"),
    ("interview", "18:30", "19:30", "Distributed systems design, Grant Okafor", "hiring", ["grant"], "Room 1", "Work"),
    ("euai", "19:30", "20:00", "EU AI Act, Sprout, checkpoint service", "euai", ["jun", "patrick", "imran"], "Google Meet", "Work"),
    ("groups", "20:00", "20:30", "Groups and permissions sync", "security", ["hao", "ned"], "Google Meet", "Work"),
    ("cloud", "21:00", "21:30", "Cloud standup (external)", "other", ["rahul"], "Google Meet", "Work"),
    ("audit", "21:00", "21:30", "Audit tiger team", "security", ["patrick", "ned"], "Google Meet", "Work"),
]
# The calendars the account can see. Work is its own and Personal a second one
# it owns; Stefan's is a colleague's it may read, and nothing on it is drawn.
CALENDARS = {
    "Work": {"summary": "Work", "primary": True, "accessRole": "owner"},
    "Personal": {"summary": "Personal", "accessRole": "owner", "dataOwner": "you@example.com"},
    "Stefan": {"summary": "stefan@example.com", "accessRole": "reader"},
}
# What Stefan's calendar holds today, one of them across the owner's own 14:20.
# (key, start, end, summary, people, where)
NOT_MINE = [
    ("stefan-vendor", "14:20", "15:00", "Stefan's vendor call", ["rahul"], "Google Meet"),
    ("stefan-wes", "16:00", "16:30", "Stefan / Wes", ["wes"], "Room 2"),
]
# The two that repeat: read back as computed occurrences of a series.
REPEATING = {"standup": "FREQ=DAILY", "weekly": "FREQ=WEEKLY"}

# What the assistant prepared for three of them.
DOSSIERS = {
    "tomas": {"purpose": "Weekly 1:1. Tomas has asked twice for this slot to stop clashing with two other meetings.",
              "since": "Tomas unblocked Lantern's image registration with a flag overnight.",
              "raise": ["Tomas sent core PR 4127 with no words. It reads as a review request.", "The vendor tooling thread you meant to pick up with Tomas."],
              "tasks": ["p-tomas"]},
    "henrik": {"purpose": "Weekly 1:1. Henrik will want your view on the proposal.",
               "raise": ["Peter and Henrik both asked about a candidate; you have not replied."], "tasks": ["t-henrik"]},
    "interview": {"purpose": "You are the distributed systems design interviewer. You agreed to this slot yesterday.",
                  "after": "The scorecard is due before Friday's 21:00 debrief."},
}

# What happened, by project: (when, who, text). `when` is a time today, or
# "y HH:MM" for yesterday. Items later than now are left out.
NEWS = {
    "extapi": [("03:12", None, "The error rate rose to about one request in five. The alert fired."),
               ("03:47", None, "The on-call rolled back the 02:50 release. Errors are back to normal."),
               ("04:05", None, "First note from the on-call: the cause is not known yet. Write-up by noon."),
               ("12:02", None, "The write-up for last night's incident is posted."),
               ("20:12", "wes", "Wes posted the cause of both incidents: one config flag.")],
    "euai": [("00:56", "jun", "Jun left suggestions on the custody doc you sent."),
             ("13:05", None, "Two more people accepted your 19:30 call. Eight are coming.")],
    "lantern": [("00:24", "tomas", "Tomas unblocked image registration with a flag."),
                ("05:09", "wei", "Wei posted the backend refactor, PR 4188, with you tagged."),
                ("05:16", "wei", "Wei posted a volume API proposal that folds volume creation into the create call."),
                ("09:10", "callum", "Callum accepted Friday at 16:30. The 17:20 slot is free."),
                ("16:12", "marcus", "Marcus thanked you for the list and asked one follow-up question.")],
    "security": [("y 22:04", "patrick", "Patrick said Imran will manage the Vigil sensor."),
                 ("02:03", None, "Vigil filed over 90 cluster findings in one minute. None is triaged in the sources checked."),
                 ("05:40", "ned", "Ned reported the token probe on six clusters and opened 26 on-call tickets about noisy alerts."),
                 ("09:34", "ned", "Ned closed nine of the 26 noisy alert tickets."),
                 ("11:12", "kenji", "Kenji replied: the VMs can stay stopped until Monday.")],
    "northfield": [("04:17", None, "Northfield asked for a list of nodes to be drained for a cable swap."),
                   ("06:23", "stefan", "Stefan gave the go-ahead on one node.")],
    "hiring": [("01:03", "noor", "Noor questioned one interview role as unapproved."),
               ("10:15", None, "Grant's CV was updated before tonight's interview.")],
    "other": [("00:42", "marco", "Marco finished the account rename."),
              ("01:52", "leo", "Leo shared the decisions from the first Product Review."),
              ("17:05", None, "The Product Review moved to next Thursday.")],
    None: [("06:10", None, "A parcel with a desk lamp is out for delivery.")],
}
NEEDS_OWNER = {"security": "Over 90 new findings, and no owner in the sources checked."}

RULES = [
    ("A page from the on-call tool about a project flagged High is urgent.", "source", "on-call", "urgent"),
    ("Hold everything from the announcements channel until the end of the day.", "channel", "announcements", "hold"),
]

# What the assistant suggests changing: (key, target, priority, why).
SUGGESTED_FLAGS = [
    ("flag-security", ("list", "security"), "high", "Not on your list. Over 90 findings came in overnight with no owner in the sources checked."),
    ("flag-ghorgs", ("list", "ghorgs"), "low", "Nothing new on it this week."),
    ("flag-followup", ("task", "t-followup"), "medium", "Worth deciding on the 08:00 call."),
]


# ---------------------------------------------------------------- the server

class Server:
    def __init__(self, url: str, token: str):
        self.url, self.token = url.rstrip("/"), token

    def call(self, method: str, path: str, body=None, query=None):
        url = self.url + path + ("?" + urllib.parse.urlencode(query) if query else "")
        data = json.dumps(body).encode() if body is not None else None
        request = urllib.request.Request(url, data=data, method=method)
        request.add_header("Authorization", "Bearer " + self.token)
        if data is not None:
            request.add_header("Content-Type", "application/json")
        try:
            with urllib.request.urlopen(request) as response:
                raw = response.read()
                return response.status, json.loads(raw) if raw else None
        except urllib.error.HTTPError as error:
            raw = error.read()
            try:
                return error.code, json.loads(raw)
            except ValueError:
                return error.code, raw.decode("utf-8", "replace")

    def put(self, kind: str, id: str, properties: dict) -> None:
        status, body = self.call("PUT", f"/api/v1/{kind}/{id}", {"properties": properties})
        if status not in (200, 201):
            raise SystemExit(f"seed: PUT {kind}/{id}: {status} {json.dumps(body)[:600]}")

    def patch(self, kind: str, id: str, properties: dict) -> None:
        status, body = self.call("PATCH", f"/api/v1/{kind}/{id}", {"properties": properties})
        if status != 200:
            raise SystemExit(f"seed: PATCH {kind}/{id}: {status} {json.dumps(body)[:600]}")

    def ids(self, kind: str) -> list[str]:
        out, after = [], None
        while True:
            query = {"filter": json.dumps({"kinds": [kind]}), "first": "500"}
            if after:
                query["after"] = after
            status, body = self.call("GET", "/api/v1/records", query=query)
            if status == 404:
                return out  # the repository does not hold this kind
            if status != 200:
                raise SystemExit(f"seed: list {kind}: {status} {json.dumps(body)[:400]}")
            out += [record["id"] for record in body.get("records") or []]
            after = body.get("cursor")
            if not after:
                return out

    def purge(self, kind: str, id: str) -> None:
        status, body = self.call("DELETE", f"/api/v1/{kind}/{id}", query={"purge": "true"})
        if status not in (200, 204, 404):
            raise SystemExit(f"seed: purge {kind}/{id}: {status} {json.dumps(body)[:400]}")


class Day:
    """The day being written: its date and zone, and times of day as instants."""

    def __init__(self, day: datetime.date, zone: ZoneInfo, now: datetime.datetime):
        self.date, self.zone, self.now = day, zone, now

    def at(self, clock: str, days: int = 0) -> datetime.datetime:
        hour, minute = (int(part) for part in clock.split(":"))
        local = datetime.datetime.combine(self.date + datetime.timedelta(days=days), datetime.time(hour, minute), self.zone)
        return local.astimezone(datetime.timezone.utc)

    def instant(self, clock: str, days: int = 0) -> str:
        return stamp(self.at(clock, days))

    def day(self, days: int = 0) -> str:
        return (self.date + datetime.timedelta(days=days)).isoformat()


def stamp(at: datetime.datetime) -> str:
    return at.astimezone(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def slot(at: datetime.datetime) -> str:
    """An occurrence's slot as its id spells it."""
    return at.astimezone(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")


def email(person: str) -> str:
    return person + "@example.com"


def path(kind: str, key: str) -> str:
    return f"{kind}/{uid(key)}"


# ---------------------------------------------------------------- writing

def wipe(server: Server) -> None:
    for kind in WIPED:
        for id in server.ids(kind):
            server.purge(kind, id)
    for person in PEOPLE:
        server.purge(CONTACT, uid("person/" + person))
    for key, *_ in SUGGESTED_FLAGS:
        server.purge(REQUEST, uid("request/" + key))
    print("seed: wiped the day's kinds")


def write_day(server: Server, day: Day, assistant: bool, morning_done: bool) -> None:
    zone = day.zone.key

    for person, name in PEOPLE.items():
        given, _, family = name.partition(" ")
        server.put(CONTACT, uid("person/" + person), {"name": name, "givenName": given, "familyName": family, "emails": [email(person)]})

    for position, (key, name, priority, about) in enumerate(PROJECTS, start=1):
        server.put(LIST, uid("list/" + key), {"name": name, "position": str(position), "description": about, "priority": priority})

    def task(key, name, project, priority, people, why, status):
        properties = {"name": name, "description": why, "priority": priority, "status": status,
                      "people": [path(CONTACT, "person/" + person) for person in people]}
        if project:
            properties["list"] = uid("list/" + project)
        return properties

    for key, name, project, priority, people, why, extra in TASKS:
        properties = task(key, name, project, priority, people, why, "open")
        if priority != "none":
            properties["prioritySetBy"] = "owner"
        if "due" in extra:
            properties["dueDay"] = day.day(extra["due"])
        if "dueAt" in extra:
            properties["dueDay"], properties["dueAt"] = day.day(), day.instant(extra["dueAt"])
        if "deferrals" in extra:
            properties["deferrals"] = extra["deferrals"]
        if "notBefore" in extra:
            properties["notBefore"] = day.instant("09:00", extra["notBefore"])
        server.put(TASK, uid("task/" + key), properties)

    for key, name, project, priority, people, source, due_today, answer, reason in SUGGESTED:
        properties = task(key, name, project, priority, people, source, "proposed")
        properties["prioritySetBy"] = "assistant"
        if due_today:
            properties["dueDay"] = day.day()
        server.put(TASK, uid("task/" + key), properties)
        server.put(ADVICE, uid("task/" + key), {"task": path(TASK, "task/" + key), "answer": answer, "reason": reason})

    for key, (name, strength, unit, category) in MEDICATIONS.items():
        properties = {"name": name, "category": category, "form": "tablet"}
        if strength:
            properties.update({"strengthAmount": strength, "strengthUnit": unit})
        server.put(MEDICATION, uid("medication/" + key), properties)

    series = {}  # key -> (kind, clock)
    for key, medication, clock, window, group, unit in SCHEDULES:
        server.put(SCHEDULE, uid("schedule/" + key), {
            "name": MEDICATIONS[medication][0], "medication": uid("medication/" + medication), "doseAmount": 1, "doseUnit": unit,
            "window": window, "group": group, "recurrence": "FREQ=DAILY", "timezone": zone, "at": day.instant(clock, -30)})
        series[key] = (SCHEDULE, "schedule/" + key, clock)
    # Weekly, on yesterday's weekday: the dose that yesterday left unlogged.
    server.put(SCHEDULE, uid("schedule/b12"), {
        "name": "Vitamin B12", "medication": uid("medication/b12"), "doseAmount": 1, "doseUnit": "tablet", "window": "morning",
        "recurrence": "FREQ=WEEKLY", "timezone": zone, "at": day.instant("07:30", -29)})
    for key, name, category, clock, window, group, status in ROUTINES:
        properties = {"name": name, "category": category, "window": window, "status": status,
                      "recurrence": "FREQ=DAILY", "timezone": zone, "at": day.instant(clock, -30)}
        if group:
            properties["group"] = group
        server.put(ROUTINE, uid("routine/" + key), properties)
        series[key] = (ROUTINE, "routine/" + key, clock)

    def log(key, days, status, minutes_late=3):
        kind, name, clock = series[key]
        due = day.at(clock, days)
        server.put(LOG, f"{uid(name)}_{slot(due)}", {
            "series": path(kind, name), "scheduledAt": stamp(due), "status": status,
            "at": stamp(due + datetime.timedelta(minutes=minutes_late))})

    # Yesterday: the morning done, the evening medication done but one skipped.
    for key in ("am-vitd", "am-magnesium", "am-omega3", "am-zinc", "am-probiotic", "am-cleanser", "am-toner", "am-serum", "am-moisturiser", "am-sunscreen", "am-meditate"):
        log(key, -1, "done")
    for key, status in (("pm-magnesium", "done"), ("pm-iron", "skipped"), ("pm-k2", "done"), ("pm-zinc", "done"), ("pm-cleanser", "done")):
        log(key, -1, status, minutes_late=64)
    if morning_done:
        for key in ("am-vitd", "am-magnesium", "am-omega3", "am-zinc", "am-probiotic"):
            log(key, 0, "done", minutes_late=1)

    def attendees(people):
        me = {"value": OWNER_EMAIL, "displayName": "George", "self": True, "responseStatus": "accepted"}
        return [me] + [{"value": email(person), "displayName": PEOPLE[person], "responseStatus": "accepted"} for person in people]

    for name, properties in CALENDARS.items():
        server.put(CALENDAR, uid("calendar/" + name), properties)
    # On a colleague's calendar `self` marks the colleague, not the owner.
    for key, start, end, summary, people, where in NOT_MINE:
        theirs = [{"value": email("stefan"), "displayName": PEOPLE["stefan"], "self": True, "responseStatus": "accepted"}]
        theirs += [{"value": email(person), "displayName": PEOPLE[person], "responseStatus": "accepted"} for person in people]
        server.put(EVENT, uid("event/" + key), {
            "summary": summary, "location": where, "calendarName": "Stefan", "calendar": path(CALENDAR, "calendar/Stefan"),
            "attendees": theirs, "status": "confirmed", "at": day.instant(start), "endsAt": day.instant(end)})

    occurrence = {}  # key -> the id a window read gives it
    # A calendar event names no project: `project` in the table is only what
    # the day's story files each meeting under.
    for key, start, end, summary, _project, people, where, calendar in MEETINGS:
        properties = {"summary": summary, "location": where, "calendarName": calendar, "calendar": path(CALENDAR, "calendar/" + calendar),
                      "attendees": attendees(people)}
        if where == "Google Meet":
            properties["hangoutLink"] = "https://meet.example.com/" + key
        if key in REPEATING:
            properties.update({"recurrence": REPEATING[key], "timezone": zone, "at": day.instant(start, -28), "endsAt": day.instant(end, -28)})
            server.put(SERIES, uid("series/" + key), properties)
            occurrence[key] = (f"{uid('series/' + key)}_{slot(day.at(start))}", f"{SERIES}/{uid('series/' + key)}")
        else:
            properties.update({"status": "confirmed", "at": day.instant(start), "endsAt": day.instant(end)})
            server.put(EVENT, uid("event/" + key), properties)
            occurrence[key] = (uid("event/" + key), f"{EVENT}/{uid('event/' + key)}")

    for key, dossier in DOSSIERS.items():
        id, event = occurrence[key]
        start = next(m[1] for m in MEETINGS if m[0] == key)
        properties = {"event": event, "slot": day.instant(start), "purpose": dossier["purpose"], "writtenAt": day.instant("06:30")}
        for name in ("since", "raise", "after"):
            if name in dossier:
                properties[name] = dossier[name]
        if "tasks" in dossier:
            properties["tasks"] = [path(TASK, "task/" + t) for t in dossier["tasks"]]
        server.put(DOSSIER, id, properties)

    for project, news in NEWS.items():
        items = []
        for when, who, text in news:
            at = day.at(when[2:], -1) if when.startswith("y ") else day.at(when)
            if at > day.now:
                continue
            item = {"at": stamp(at), "text": text}
            if who:
                item["people"] = [path(CONTACT, "person/" + who)]
            items.append(item)
        if not items:
            continue
        items.sort(key=lambda item: item["at"], reverse=True)
        properties = {"since": day.instant("22:00", -1), "until": items[0]["at"], "line": items[0]["text"], "count": len(items), "items": items}
        if project:
            properties["project"] = path(LIST, "list/" + project)
        if project in NEEDS_OWNER:
            properties.update({"needsOwner": True, "needsOwnerReason": NEEDS_OWNER[project]})
        server.put(DIGEST, uid("list/" + project) if project else "unfiled", properties)

    # Read through last night for two projects, so their overnight news is new
    # and the day before is not.
    for project in ("extapi", "lantern"):
        server.put(REVIEWMARK, uid("list/" + project), {"project": path(LIST, "list/" + project), "through": day.instant("22:00", -1)})

    server.put(PREFERENCE, "default", {"timezone": zone, "prepareAt": "06:30", "lastActiveAt": day.instant("01:57"), "urgentWait": "PT2M"})

    for text, scope, match, effect in RULES:
        server.put(RULE, uid("rule/" + match), {"text": text, "scope": scope, "match": match, "effect": effect})

    for key, (what, target), priority, why in SUGGESTED_FLAGS:
        kind, name = (LIST, "list/" + target) if what == "list" else (TASK, "task/" + target)
        server.put(REQUEST, uid("request/" + key), {"op": "patch", "target": path(kind, name), "diff": {"properties": {"priority": priority}}, "rationale": why})

    # The dev server holds no model key, so the preparer's runs would only
    # fail: its trigger is off unless asked for. An install turns it on
    # again, which is why server.sh turns it off after each one too.
    server.patch(TRIGGER, "today-preparer-on-preparation", {"enabled": assistant})
    print(f"seed: wrote {day.day()} ({zone}): {len(PROJECTS)} projects, {len(TASKS)} tasks, {len(SUGGESTED)} suggested tasks, "
          f"{len(SCHEDULES) + 1} doses, {len(ROUTINES)} routines, {len(MEETINGS)} meetings")


def urgent(server: Server, day: Day, states: list[str]) -> None:
    """Puts an urgent mark on, in each of its three states, or takes all off."""
    now = stamp(day.now)
    off = {"priorityReason": None, "priorityWas": None, "urgentCheckedAt": None, "urgentHeldAt": None, "urgentHeldReason": None, "prioritySetBy": "owner"}
    # Checked: the assistant set it itself. It interrupts.
    incident = uid("task/inc-2")
    if "checked" in states:
        server.put(TASK, incident, {
            "name": "External API is failing again", "list": uid("list/extapi"), "status": "open",
            "description": "About one request in ten fails. The on-call has it, and asks whether to roll back this morning's release.",
            "priority": "urgent", "prioritySetBy": "assistant", "priorityWas": "none",
            "priorityReason": "About one request in ten has failed for the last few minutes, on a project you flagged High."})
    else:
        server.purge(TASK, incident)
    # Held: a rule marked it, and the assistant thinks it can wait.
    wei = uid("task/t-wei")
    if "held" in states:
        server.patch(TASK, wei, {"status": "open"})
        server.patch(TASK, wei, {
            "priority": "urgent", "prioritySetBy": "automation", "priorityWas": "medium",
            "priorityReason": "Wei wrote again about the refactor review, and a rule on the message marked it urgent.",
            "urgentCheckedAt": None, "urgentHeldAt": now,
            "urgentHeldReason": "It is a task for Friday morning, and Wei is here until Monday."})
    else:
        server.patch(TASK, wei, dict(off, priority="medium"))
    # Not checked: the API set it and nobody has looked.
    drain = uid("task/t-drain")
    if "unchecked" in states:
        server.put(TASK, drain, {
            "name": "Confirm the drain list for Northfield", "list": uid("list/northfield"), "status": "open",
            "priority": "urgent", "prioritySetBy": "api", "priorityWas": "none",
            "priorityReason": "Marked by the on-call tool: a drain waits for your confirmation."})
    else:
        server.purge(TASK, drain)
    print("seed: urgent marks on: " + (", ".join(states) if states else "none"))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("command", nargs="?", default="day", choices=["day", "wipe", "urgent"])
    parser.add_argument("states", nargs="*", help="for urgent: any of checked, held, unchecked, or none")
    parser.add_argument("--server")
    parser.add_argument("--token")
    parser.add_argument("--rehearsal", action="store_true", help="the second, empty repository of the env file")
    parser.add_argument("--day", help="the day to write, YYYY-MM-DD; today when absent")
    parser.add_argument("--timezone", help="an IANA zone; this machine's when absent")
    parser.add_argument("--keep", action="store_true", help="write over what is there instead of wiping first")
    parser.add_argument("--morning-done", action="store_true", help="log today's morning medication as taken")
    parser.add_argument("--assistant", action="store_true", help="leave the trigger that runs the preparer on")
    parser.add_argument("--allow-remote", action="store_true", help="run against a server that is not on this machine")
    args = parser.parse_args()

    env = {}
    if ENV_FILE.exists():
        for line in ENV_FILE.read_text().splitlines():
            if "=" in line and not line.startswith("#"):
                name, _, value = line.partition("=")
                env[name.strip()] = value.strip()
    url = args.server or env.get("TODAY_DEV_SERVER")
    token = args.token or env.get("TODAY_REHEARSAL_TOKEN" if args.rehearsal else "TODAY_DEV_TOKEN")
    if not url or not token:
        raise SystemExit("seed: no server or token; run apps/today/dev/server.sh up, or pass --server and --token")
    host = urllib.parse.urlparse(url).hostname or ""
    if host not in ("localhost", "127.0.0.1", "::1") and not args.allow_remote:
        raise SystemExit(f"seed: {url} is not on this machine; this writes an invented day and its wipe purges whole kinds (--allow-remote to insist)")

    zone_name = args.timezone or local_zone()
    zone = ZoneInfo(zone_name)
    now = datetime.datetime.now(datetime.timezone.utc)
    date = datetime.date.fromisoformat(args.day) if args.day else now.astimezone(zone).date()
    if args.day:  # a named day is written whole, as at its last minute
        now = datetime.datetime.combine(date, datetime.time(23, 59), zone).astimezone(datetime.timezone.utc)
    day, server = Day(date, zone, now), Server(url, token)

    if args.command == "urgent":
        states = [state for state in args.states if state != "none"]
        unknown = [state for state in states if state not in ("checked", "held", "unchecked")]
        if unknown or not args.states:
            raise SystemExit("seed: urgent takes any of checked, held, unchecked, or none")
        urgent(server, day, states)
        return 0
    if args.command == "wipe" or not args.keep:
        wipe(server)
    if args.command == "day":
        write_day(server, day, args.assistant, args.morning_done)
    return 0


def local_zone() -> str:
    link = pathlib.Path("/etc/localtime")
    try:
        target = str(link.resolve())
    except OSError:
        return "UTC"
    return target.split("zoneinfo/", 1)[1] if "zoneinfo/" in target else "UTC"


if __name__ == "__main__":
    sys.exit(main())
