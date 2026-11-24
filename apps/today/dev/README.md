# Trying Substrate Today against a dev server

The app is drawn from records, so trying it needs a repository that holds a
day. This folder makes one on this tree's dev server.

```bash
apps/today/dev/server.sh up        # start the server; on a fresh one, set it up and seed the day
apps/today/dev/seed.py             # wipe the day's kinds and write the day again, dated today
apps/today/dev/seed.py urgent checked held unchecked   # an urgent mark in each of its three states
apps/today/dev/seed.py urgent none
apps/today/dev/server.sh restart   # after a change under kinds/ or a new tasks version: rebuild, and add the packages again
apps/today/dev/server.sh reset     # drop the database: new repositories and new tokens
```

`server.sh` needs Docker, as `mise run dev:up` does. It runs the server on
port 8098 with a Postgres container of its own, registers two repositories
and adds `routines`, `health`, `tasks` and `today` to each:

| Repository            | Holds                                                         |
| --------------------- | ------------------------------------------------------------- |
| `day.localhost`       | the invented day, and a stand-in calendar (`calendar.yaml`)   |
| `rehearsal.localhost` | nothing: the packages alone, for rehearsing a migration into |

The tasks package comes from a local authority document
(`.dev/authority/authority.json`, built from `apps/tasks/package`), which
lists the version the tree declares after the earlier versions the folder
still holds, so `restart` takes a server from one published version to the
next. A version's bytes never change: a server that added a version while it
was still being edited holds a digest no file matches any more ("the
publisher changed the digest of held version 4"), and only `reset` moves it
on.

`.dev/today-dev.env` (ignored by git) names the server, both repositories and
a token for each, and `.dev/ctl.yaml` and `.dev/ctl-rehearsal.yaml` are
`substratectl` configs for them (`SUBSTRATECTL_CONFIG`). Both passwords are
`devpassword1`.

## The day

`seed.py` writes one day, dated today on this machine's clock, and every
person, project, message and health detail in it is made up: twelve projects
of which five carry a flag, fifteen tasks, thirteen suggested tasks each with
the assistant's advice, a morning and an evening group of medication with
yesterday's logs (one dose skipped, one weekly dose missed), skincare and two
habits, a paused routine, sixteen meetings with four overlaps (two of them
occurrences of a repeating event), a digest of what happened for seven
projects and one for what belongs to none, three dossiers, two rules, and
three changes the assistant suggests as patch requests. News later than now
is left out, so "what is happening" fills as the day goes; `--day` writes a
named day whole.

Each suggested task carries `originAt`, when its words were said, and says
that day as a date in its description: most are a day or two old, three are
a week or more, and one (fifteen days) is past the two weeks after which the
package's daily `fade` abandons it. The trigger runs at 03:10 UTC on a
server left up overnight, and `substratectl function call
substrate.reamde.dev/today/fade` runs it now; `seed.py` puts the suggestion
back. One dismissed suggestion carries a `today/correction`, what the owner
wrote about it.

A wipe purges every record of the kinds the day uses in that repository, so
`seed.py` refuses a server that is not on this machine.

## The preparer

What the package asks a model for (advice on suggested tasks, a dossier for
a meeting, a check of an urgent mark) is run by its own agent, the
`preparer`, on the provider bound to the package's `model` input. The dev
server holds no model key, so a run would only fail. `seed.py` and
`server.sh` turn off the trigger that runs it
(`today-preparer-on-preparation`); the `prepare` function still writes its
`today/preparation` records, which is how to read what it would ask for.
`TODAY_DEV_ASSISTANT=1` and `seed.py --assistant` leave the trigger on, for a
server whose provider has been given a key.

## Asking the assistant

The assistant, Moss, answers on a provider too, so with no model key a question in the app fails at once.
`model.py` is a stand-in for one: it serves the Anthropic Messages API on port 8099 and answers each turn
with how many of the person's messages it was given, which is what shows whether a second question went
into the conversation of the first.

```bash
python3 apps/today/dev/model.py /tmp/today-model.log &
SUBSTRATE_EGRESS_ALLOW=127.0.0.1 apps/today/dev/server.sh up     # the server refuses to call this machine otherwise
. .dev/today-dev.env
curl -sS -X PATCH "$TODAY_DEV_SERVER/api/v1/substrate.reamde.dev/llm/provider/anthropic" \
  -H "Authorization: Bearer $TODAY_DEV_TOKEN" -H "Content-Type: application/json" \
  -d '{"properties":{"baseURL":"http://127.0.0.1:8099/","apiKey":"stand-in"}}'
```

Ask twice about one task in the app: the second answer says "I was given 2 of your messages", and the
repository holds one `llm/thread` for the two. Put the provider back with `"baseURL":
"https://api.anthropic.com/"` and `"apiKey": null`. Start and stop the server through `server.sh`: a bare
`mise run dev:restart` leaves out its port and its database container, and aims at the shared ones.

## The calendar

A real calendar comes from a provider, which needs an account. `calendar.yaml`
declares a stand-in in the repository's own namespace: `~/calendar/event`
binds core's `temporal(range)` and `override`, and `~/calendar/series` binds
`temporal(range)` and `recurring`, which is all the app asks of a calendar
kind. Their properties carry the Google provider's names and shapes, so
`attendees` is a list of objects with `value` (the address), `displayName`,
`self` and `responseStatus`, and a contact is matched by its address.

`~/calendar/calendar` holds what says whose a calendar is, and each event
names the one it is on. The seed writes three: Work (the account's own),
Personal (a second one it owns) and a colleague's, Stefan's, which the
account may only read. Stefan's holds two events today, one across the 14:20
meeting, and the app draws neither. After a change to `calendar.yaml` a
server that is already set up takes it with `server.sh restart`.
