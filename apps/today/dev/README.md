# Trying Substrate Today against a dev server

The app is drawn from records, so trying it needs a repository that holds a
day. This folder makes one on this tree's dev server.

```bash
apps/today/dev/server.sh up        # start the server; on a fresh one, set it up and seed the day
apps/today/dev/seed.py             # wipe the day's kinds and write the day again, dated today
apps/today/dev/seed.py urgent checked held unchecked   # an urgent mark in each of its three states
apps/today/dev/seed.py urgent none
apps/today/dev/server.sh restart   # after a change under kinds/: rebuild, and add the binary's packages again
apps/today/dev/server.sh reset     # drop the database: new repositories and new tokens
```

`server.sh` needs Docker, as `mise run dev:up` does. It runs the server on
port 8098 with a Postgres container of its own, registers two repositories
and adds `routines`, `health`, `tasks` and `today` to each:

| Repository            | Holds                                                         |
| --------------------- | ------------------------------------------------------------- |
| `day.localhost`       | the invented day, and a stand-in calendar (`calendar.yaml`)   |
| `rehearsal.localhost` | nothing: the packages alone, for rehearsing a migration into |

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

A wipe purges every record of the kinds the day uses in that repository, so
`seed.py` refuses a server that is not on this machine.

## The assistant

The dev server holds no model key, so a run of the assistant would only
fail. `seed.py` and `server.sh` turn off the trigger that runs it
(`today-assistant-on-preparation`); the `prepare` function still writes its
`today/preparation` records, which is how to read what it would ask for.
`TODAY_DEV_ASSISTANT=1` and `seed.py --assistant` leave the trigger on, for a
server that has been given a key.

## The calendar

A real calendar comes from a provider, which needs an account. `calendar.yaml`
declares a stand-in in the repository's own namespace: `~/calendar/event`
binds core's `temporal(range)` and `override`, and `~/calendar/series` binds
`temporal(range)` and `recurring`, which is all the app asks of a calendar
kind. Their properties carry the Google provider's names and shapes, so
`attendees` is a list of objects with `value` (the address), `displayName`,
`self` and `responseStatus`, and a contact is matched by its address.
