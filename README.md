# substrate

A self-hosted store for one person's digital life. Mail, calendar, messages,
people, tasks, notes and media become typed **records** in one
Postgres-backed **repository**, behind one API, on a machine you run. Apps
and assistants read those records and write back through the same API,
instead of every app keeping its own silo.

The source of truth is an append-only **changelog**: the records you read
are computed by replaying it, so what you see cannot drift from what
happened. Tokens, kind declarations, even the functions and agents that
automate your data are records like any other.

It is, roughly, Kubernetes for your own data and the agents acting on it, and
the borrowing is deliberate: the substrate is the API server, a kind is a
declared type it validates every write against, and every app or agent is a
controller that watches the ordered change feed and writes back through the
same API. [docs/introduction.md](docs/introduction.md) draws the comparison
out.

## Features

- **One user, one repository.** Registering creates a user and their one
  repository, and everything they own lives in it. No roles, no scopes, no
  sharing to configure. Isolation between repositories is Postgres row level
  security, not query-layer discipline.
- **Kinds declared in YAML.** A kind names its properties and edges,
  validation runs on every write, and a vocabulary evolves by integer
  versions without breaking the records underneath it.
- **One small write API.** Seven mutations (`put`, `patch`, `delete`,
  `link`, `unlink`, `merge`, `split`), over REST and GraphQL alike, with
  full-text and vector search and a resumable `watch` stream beside them.
- **Bundles.** Vocabulary, functions and agents install and uninstall as one
  unit. The catalog compiled into the binary ships sync for Google, GitHub,
  Linear, Notion, Beeper and Whoop, so a mailbox or a calendar becomes
  records you own rather than an API you rent, plus Firecrawl functions
  agents can call to search and read the web.
- **Functions and triggers.** A function is real code, Python or Go, stored
  in your repository and run by the substrate; a trigger fires it when a
  matching record changes, on a schedule, or from a webhook. Automation
  lives beside the data it manages, installed and removed with its bundle.
- **Agents.** An LLM loop over your records, with threads, tool calls and
  budgets visible in the console. Providers are records in your repository:
  the key is a secret-typed property, redacted on every read, and the server
  process holds no LLM key of its own.
- **One thing to run.** One server process and one Postgres; the web console
  is built into the server image and served at `/`, and `substratectl` is
  the CLI for everything else.

## Quick start

```bash
docker compose up
```

That is the whole thing: Postgres, the API and the console at
<http://localhost:8080>. Every setting has a working default; the first start
mints a credential key and tells you to back it up. Register with the invite
code `let-me-in`.

## Declare your own kinds

From a checkout of this repo, against the substrate above. The CLI is built
from the tree, so this path needs the [mise](https://mise.jdx.dev)
toolchain: `mise install` once.

```bash
mise run build:cli

# Asks for the invite code, a username and a password, then stores the
# minted token as a context in ~/.config/substratectl.
bin/substratectl register --server http://localhost:8080
```

A fresh repository holds the core vocabulary and nothing else. Teach it
yours: kinds are declared in YAML under an authority you name, and a
declaration is itself a record write. One file declares a project and a
task, with typed properties, a state machine, and an edge between them:

```yaml
# chores.yaml
kind: core.substrate.reamde.dev/authority
metadata:
  id: geoah.me
data:
  version: 1
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: geoah.me/project
data:
  authority: geoah.me
  description: A group of tasks with one goal.
  names:
    singular: project
    plural: projects
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      description: what the project is called
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: geoah.me/task
data:
  authority: geoah.me
  description: One thing to do, grouped under a project.
  names:
    singular: task
    plural: tasks
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      description: what to do
    dueAt:
      type: datetime
      description: when it is due
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
  edges:
    project:
      to: project
      description: the project this task groups under
```

Apply it, write records against it, and query them back. Every write is
validated against the declaration, and a state moves only along its
declared transitions:

```bash
bin/substratectl apply -f chores.yaml

cat <<'EOF' | bin/substratectl apply -f -
kind: geoah.me/project
metadata:
  id: home
data:
  properties:
    name: Home
---
kind: geoah.me/task
metadata:
  id: milk
data:
  properties:
    name: Buy milk
    dueAt: 2026-08-30T09:00:00Z
  edges:
    - rel: project
      to:
        kind: geoah.me/project
        id: home
---
kind: geoah.me/task
metadata:
  id: plants
data:
  properties:
    name: Water the plants
    dueAt: 2026-08-29T09:00:00Z
  edges:
    - rel: project
      to:
        kind: geoah.me/project
        id: home
EOF

bin/substratectl get tasks
bin/substratectl get tasks milk -o yaml   # the full envelope, apply-able
bin/substratectl get tasks --filter '{"properties":{"status":{"eq":"open"}}}'

bin/substratectl patch tasks milk --state status=done  # stamps completedAt

# Replay the changelog (--from resumes after a sequence number), then keep
# following it: every write above is in it.
bin/substratectl watch --from 1
```

The same records answer on REST at `/api/v1/geoah.me/task`, on
GraphQL at `/graphql`, and in full-text and semantic search:
[docs/api.md](docs/api.md) and
[docs/graphql-and-search.md](docs/graphql-and-search.md).
[docs/getting-started.md](docs/getting-started.md) is the longer
walkthrough.

## Manage them with a function and an agent

Automation is records too. Append two more documents to `chores.yaml`: a
**function** (real code the substrate runs) and an **agent** (an LLM loop
with tools), each scoped to exactly the kinds it may touch:

```yaml
---
kind: core.substrate.reamde.dev/function
metadata:
  id: geoah.me/triage
data:
  authority: geoah.me
  description: Raise every open task past its due date to urgent.
  runtime: python
  permissions:
    reads:
      kinds:
        - geoah.me/task
    writes:
      - geoah.me/task
  source: |
    from datetime import datetime, timezone

    def main(input, host):
        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        page = host.records.list(["geoah.me/task"],
                                 where={"status": {"eq": "open"}})
        raised = 0
        for task in page.get("records") or []:
            props = task.get("properties") or {}
            due = props.get("dueAt")
            if due and due < now and props.get("priority") != "urgent":
                host.effects.patch("geoah.me/task", task["id"],
                                   properties={"priority": "urgent"})
                raised += 1
        return {"output": {"raised": raised}}
---
kind: core.substrate.reamde.dev/agent
metadata:
  id: geoah.me/assistant
data:
  authority: geoah.me
  description: Reads and updates my projects and tasks on request.
  prompt: |
    You manage the user's projects and tasks. Read them with the query
    tool; create, complete and reprioritize them with mutate. Keep
    answers short.
  provider: anthropic
  model: claude-opus-5
  tools:
    - function: core.substrate.reamde.dev/query
    - function: core.substrate.reamde.dev/mutate
  budgets:
    maxTurns: 8
    maxToolCalls: 16
  permissions:
    reads:
      kinds:
        - geoah.me/task
        - geoah.me/project
    writes:
      - geoah.me/task
      - geoah.me/project
```

Apply the file again (re-applying a closure is how it evolves; the engine
versions the changes itself), then run the function by hand. `plants` is
open and past due, so it comes back urgent:

```bash
bin/substratectl apply -f chores.yaml
bin/substratectl function call geoah.me/triage
bin/substratectl get tasks plants -o yaml
```

What usually runs it is a **trigger**, an ordinary record binding a source
(a record change, a schedule, or a webhook) to a callable:

```bash
cat <<'EOF' | bin/substratectl apply -f -
kind: core.substrate.reamde.dev/trigger
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
    callable: core.substrate.reamde.dev/function/geoah.me/triage
EOF
```

[docs/functions.md](docs/functions.md) is the whole contract: the host SDK,
permissions, triggers and the sandbox.

## Give the agent a model

The agent loop is core: the `agent` kind, its built-in tools and the
console's chat all ship in the engine, which is why `assistant` needed
nothing installed. The one thing a substrate cannot invent is an LLM
provider key. Providers are `llmprovider` records, and the catalog ships a
**bundle** with two keyless rows (`anthropic`, `openai`) and example agents
beside them. A bundle is the install unit: a closure like `chores.yaml`,
installed and removed as one thing, from the console's Registry page or
from the files under [kinds/](kinds):

```bash
bin/substratectl apply \
  -f kinds/llm.examples.substrate.reamde.dev/bundle.yaml \
  -f kinds/llm.examples.substrate.reamde.dev/providers.yaml
```

Then put a key on the row `assistant` names. It is an ordinary record
write, and `apiKey` is secret-typed, so it reads back redacted ever after:

```bash
cat <<'EOF' | bin/substratectl apply -f -
kind: core.substrate.reamde.dev/llmprovider
metadata:
  id: anthropic
data:
  properties:
    apiKey: sk-ant-...
EOF
```

The console's Agents page can then chat with `assistant`, which reads your
tasks and writes them back through the same API as everything else.
[docs/agents.md](docs/agents.md) has the rest: wires, pricing, sub-agents,
budgets. The same catalog carries the provider sync bundles (Google,
GitHub, Linear, Notion, Beeper, Whoop) and vocabularies (tasks, people,
calendar, messaging and more): [docs/bundles.md](docs/bundles.md) is the
model, [docs/bundles-catalog.md](docs/bundles-catalog.md) what ships
today.

## Configuration

Everything is an environment variable, and every one has a working default
under `docker compose up`. Three matter before anyone else can reach your
substrate: `DATABASE_URL` (the one Postgres), `SUBSTRATE_INVITE_CODE` (unset
means registration is closed), and `SUBSTRATE_CREDENTIAL_KEY` (base64 of
exactly 32 bytes; it seals every secret and every repository's changelog
signing seed, and a server without it refuses to boot).
[docs/operations.md](docs/operations.md) has the full table, blob stores and
egress rules included.

## Development

Toolchain is [mise](https://mise.jdx.dev): `mise install` once, then:

```bash
mise run dev            # Postgres in a container + the server on :8080
mise run dev:wipe       # delete the database; the next start is fresh
mise run console:dev    # the console on :5173, proxying /api to :8080
mise run test           # the Go suite (Docker needed for the engine half)
mise run ci             # every CI job, locally
mise tasks              # everything else
```

`docker compose up` builds an image; `mise run dev` runs the binary from the
tree, so a change is a restart rather than a rebuild. Registration is
one-shot per user, so testing it twice means `mise run dev:wipe`.

[AGENTS.md](AGENTS.md) is the working guide for anyone, person or agent,
changing this code: the model in one paragraph, every task, and the house
rules. [docs/testing.md](docs/testing.md) maps the test suites.

## Where to read next

| Where                                                | What                                                              |
| ---------------------------------------------------- | ----------------------------------------------------------------- |
| [docs/README.md](docs/README.md)                     | the documentation index; the pages build one running example      |
| [docs/getting-started.md](docs/getting-started.md)   | register, log in, write a record                                  |
| [docs/introduction.md](docs/introduction.md)         | the design, its terms, and what it borrows from Kubernetes        |
| [docs/data-model.md](docs/data-model.md)             | the repository, its changelog, records, kinds and the envelope    |
| [docs/api.md](docs/api.md)                           | REST, filters, mutations, errors                                  |
| [docs/terms.md](docs/terms.md)                       | one word per thing, and the dead words each replaced              |
| [docs/decisions](docs/decisions/README.md)           | the decision records: one short, dated page per choice            |
| [kinds/](kinds)                                      | the shipped vocabulary, as YAML                                   |
| [AGENTS.md](AGENTS.md)                               | how to work on this code                                          |
| [SECURITY.md](SECURITY.md)                           | how to report a vulnerability                                     |
| [CHANGELOG.md](CHANGELOG.md)                         | released versions                                                 |
| [Issues](https://github.com/geoah/substrate/issues)  | known bugs and planned work                                       |
