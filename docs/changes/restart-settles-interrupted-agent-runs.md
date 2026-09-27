---
type: fix
---

# A restart settles the agent runs it interrupted, and a live run lists as running

An agent run that a server stop cut short used to stay `running` on its
`substrate.reamde.dev/llm/thread` forever, and its trigger delivery stayed
listed under `…/parked` with the error `delivery in flight: an agent run a
restart interrupted stays here, retried by hand`. A run that started after
the restart was listed with the same error.

At its first open of a repository, the server now:

- settles every `running` thread to `status: error` with `reason:
  interrupted: the server stopped during the run`;
- rewrites each interrupted delivery's error to `interrupted: the server
  stopped during this agent run, and nothing reruns it by itself; read its
  thread, then retry this delivery to run the agent again, or forget it`.

Nothing reruns an interrupted delivery by itself, as before (decision record
0064). A delivery the server is running right now lists with `running: true`
and the error `delivery in flight: an agent run is running it now`, and
`GET …/trigger/status` (`substratectl trigger status`) counts it under the new `inFlight` field
instead of `parked`:

```json
{"id": "reflect-daily", "parked": 1, "pending": 0, "inFlight": 1}
```

To finish an interrupted run, read its thread, then retry or forget the
parked row:

```
substratectl trigger parked <trigger>
substratectl trigger retry <trigger> <parked-id>
substratectl trigger forget <trigger> <parked-id>
```
