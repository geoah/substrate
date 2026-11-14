# Your own kinds, a function and an agent

One file of YAML that declares a package of your own, records written against
it, a function that changes them on a schedule, and an agent that reads and
writes them, with the one key it needs. [Getting started](getting-started.md)
is the slower path through registration and a first record.

## Declare your own kinds

From a checkout of this repo, against a substrate on `localhost:8080` (`docker compose up --build`). The CLI is built
from the tree, so this path needs the [mise](https://mise.jdx.dev)
toolchain: `mise install` once.

```bash
mise run build:cli

# Asks for a repository name and a password (and, on a substrate that reads
# them, the invite code and a TOTP code from the enrollment it prints), then
# stores the minted token as a context in ~/.config/substratectl.
bin/substratectl register --server http://localhost:8080
```

A repository declares new packages only in its local namespace, written `~`
([decision 0259](decisions/0259-a-repositorys-own-kinds-live-in-the-local-namespace.md)): `~/chores` is
your `chores` package whatever you named the repository, so the examples
below work as they are. The name you registered is accepted in place of `~`
(`geoah.me/chores/task` for a repository named `geoah.me`), and every answer
says `~`. Quote `~` on a command line, where an unquoted `~/` at the start of
a word is your home directory; inside YAML it needs no quotes, but a bare
`~` alone is YAML's null, so `authority: "~"` is quoted.

A fresh repository holds the core vocabulary, the `llm` package with three
keyless provider rows, the `console` and `observations` packages, the
`assistant` package (Moss) and the shipped units, and none of your own kinds.
Teach it yours: kinds are declared in YAML in a package in your repository's
local namespace,
and a declaration is itself a record write. One file declares a package and,
in it, a project and a task, with typed properties, a state machine, and a
reference between them:

```yaml
# chores.yaml
kind: substrate.reamde.dev/core/package
metadata:
  id: ~/chores
data:
  authority: "~"
  package: chores
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ~/chores/project
data:
  authority: "~"
  package: chores
  description: A group of tasks with one goal.
  names:
    singular: project
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      description: what the project is called
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ~/chores/task
data:
  authority: "~"
  package: chores
  description: One thing to do, grouped under a project.
  names:
    singular: task
  displayTemplate: "{name}"
  # dueAt is a column every record row carries, so it is bound from the
  # temporal trait rather than declared: the point variant, renamed.
  traits:
    - "substrate.reamde.dev/core/temporal(point: dueAt)"
  properties:
    name:
      type: string
      description: what to do
    priority:
      type: enum
      description: how urgent it is
      default: normal
      values:
        - normal
        - urgent
    status:
      type: state
      description: open until done
      states:
        - open
        - done
      initial: open
      transitions:
        - from: open
          to: done
          stamps:
            completedAt: now
        - from: done
          to: open
    completedAt:
      type: datetime
      description: when it was done, stamped by the transition
    project:
      type: reference
      kind: ~/chores/project
      mustExist: true
      description: the project this task groups under
```

Apply it, write records against it, and query them back. Every write is
validated against the declaration, and a state moves only along its
declared transitions. A record you create takes an id in the substrate's
minted shape, a UUIDv7 in 26 Crockford characters: leave `metadata.id` out
and the substrate mints one, or mint your own as below so the next commands
can name the records:

```bash
bin/substratectl apply -f chores.yaml

cat <<'EOF' | bin/substratectl apply -f -
kind: ~/chores/project
metadata:
  id: 01jcd8m0k3p7s2x9v4w6y1z5qa  # home
data:
  properties:
    name: Home
---
kind: ~/chores/task
metadata:
  id: 01jcd8m0k3p7s2x9v4w6y1z5qb  # milk
data:
  properties:
    name: Buy milk
    dueAt: 2026-08-30T09:00:00Z
    project: ~/chores/project/01jcd8m0k3p7s2x9v4w6y1z5qa
---
kind: ~/chores/task
metadata:
  id: 01jcd8m0k3p7s2x9v4w6y1z5qc  # plants
data:
  properties:
    name: Water the plants
    dueAt: 2026-08-29T09:00:00Z
    project: ~/chores/project/01jcd8m0k3p7s2x9v4w6y1z5qa
EOF

bin/substratectl get '~/chores/task'
bin/substratectl get '~/chores/task' 01jcd8m0k3p7s2x9v4w6y1z5qb -o yaml   # the full envelope, apply-able
bin/substratectl get '~/chores/task' --filter '{"properties":{"status":{"eq":"open"}}}'

bin/substratectl patch '~/chores/task' 01jcd8m0k3p7s2x9v4w6y1z5qb --state status=done  # stamps completedAt

# Tail the changelog: from here on every write streams as one line, and the
# opening line prints the sequence and generation a later run resumes from
# (--from N --generation G).
bin/substratectl watch
```

The same records answer on REST at `/api/v1/~/chores/task/{id}`, in a
filtered list at `/api/v1/records`, and in full-text and semantic search on
the same route: [api](api.md). [Getting started](getting-started.md)
is the slower path.

## Manage them with a function and an agent

Automation is records too. Append two more documents to `chores.yaml`: a
**function** (real code the substrate runs) and an **agent** (an LLM loop
with tools), each scoped to exactly the kinds it may touch:

```yaml
---
kind: substrate.reamde.dev/core/function
metadata:
  id: ~/chores/triage
data:
  authority: "~"
  package: chores
  description: Raise every open task past its due date to urgent.
  runtime: python
  permissions:
    reads:
      kinds:
        - ~/chores/task
    writes:
      - ~/chores/task
  source: |
    from datetime import datetime, timezone

    def main(input, host):
        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        page = host.records.list(["~/chores/task"],
                                 where={"status": {"eq": "open"}})
        raised = 0
        for task in page.get("records") or []:
            props = task.get("properties") or {}
            due = props.get("dueAt")
            if due and due < now and props.get("priority") != "urgent":
                host.effects.patch("~/chores/task", task["id"],
                                   properties={"priority": "urgent"})
                raised += 1
        return {"output": {"raised": raised}}
---
kind: substrate.reamde.dev/core/agent
metadata:
  id: ~/chores/assistant
data:
  authority: "~"
  package: chores
  description: Reads and updates my projects and tasks on request.
  prompt: |
    You manage the user's projects and tasks. Read them with the query
    tool; create, complete and reprioritize them with write. Keep
    answers short.
  provider: anthropic
  model: claude-opus-5
  tools:
    - function: substrate.reamde.dev/core/query
    - function: substrate.reamde.dev/core/write
  budgets:
    maxTurns: 8
    maxToolCalls: 16
  permissions:
    reads:
      kinds:
        - ~/chores/task
        - ~/chores/project
    writes:
      - ~/chores/task
      - ~/chores/project
```

Apply the file again (re-applying a closure is how it evolves; the engine
versions the changes itself), then run the function by hand. The plants task is
open and past due, so it comes back urgent:

```bash
bin/substratectl apply -f chores.yaml
bin/substratectl function call '~/chores/triage'
bin/substratectl get '~/chores/task' 01jcd8m0k3p7s2x9v4w6y1z5qc -o yaml
```

What usually runs it is a **trigger**, an ordinary record binding a source
(a record change, a schedule, or a webhook) to a callable:

```bash
cat <<'EOF' | bin/substratectl apply -f -
kind: substrate.reamde.dev/core/trigger
metadata:
  id: chores-triage-daily
data:
  properties:
    enabled: true
    source:
      schedule:
        recurrence: "FREQ=DAILY"
        timezone: Europe/London
        startsAt: "2026-01-01T07:00:00Z"
    callable: substrate.reamde.dev/core/function/~/chores/triage
EOF
```

A `webhook` source instead makes the trigger a public endpoint,
`POST /webhooks/<repository>/<trigger-id>` (the repository's id, as
`trigger status` prints it, or its name), and the callable receives the
request (headers, query, body or parsed multipart parts, file parts stored
as blobs) on its envelope. The endpoint is open unless the trigger sets
`source.webhook.key`; GitHub-style signatures verify inside the function,
which gets the raw body. The shipped `pebble` bundle
([samples/pebble](../samples/pebble)) is a worked example: a voice-recorder ring
POSTs each capture to one URL, and a header decides whether it lands as a note
or is handed to an agent.

[functions](functions.md) is the whole contract: the host SDK,
permissions, triggers, webhooks and the sandbox.

## Give the agent a model

The agent loop is core: the `substrate.reamde.dev/core/agent` kind, its built-in tools and the
console's chat all ship in the engine, which is why `assistant` needed
nothing installed. The one thing a substrate cannot invent is an LLM
provider key. Providers are `substrate.reamde.dev/llm/provider` records, and registration seeded
three keyless rows (`openai`, `anthropic`, `gemini`) beside Moss, the
assistant every repository ships with, so the row `assistant` names already
exists. A package the catalog serves is a **bundle**, the install unit: a
closure like `chores.yaml`, installed and removed as one thing. The console
adds them: a provider from its Providers page, a sample from All data's
**Add a collection**.

Put a key on the row `assistant` names. It is an ordinary record write, and
`apiKey` is secret-typed, so it reads back redacted ever after:

```bash
cat <<'EOF' | bin/substratectl apply -f -
kind: substrate.reamde.dev/llm/provider
metadata:
  id: anthropic
data:
  properties:
    apiKey: sk-ant-...
EOF
```

The console's Agents page can then chat with `assistant`, which reads your
tasks and writes them back through the same API as everything else.
[agents](agents.md) has the rest: wires, pricing, sub-agents,
budgets. The same catalog carries the provider bundles (Google, GitHub,
Linear, Notion, Slack, Beeper, Whoop, Apple Health) and
vocabularies (tasks, people, calendar, messaging and more):
[bundles](bundles.md) is the model,
[the catalog](bundles-catalog.md) what ships
today.

To connect one, your Google mail and calendar say, open it on the console's
**Providers** page. Its page walks you through four steps: add it, give it
sign-in details (an OAuth client you create with the provider; the page shows
the callback URL to give it), connect your account and choose what to bring
in, where *Create and connect* opens the provider for you to approve. The
first sync starts when you come back.
