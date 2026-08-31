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

## Write your first record

From a checkout of this repo, against the substrate above: register, install
a vocabulary, write a task, complete it, and replay the changelog that
recorded all of it. The CLI is built from the tree, so this path needs the
[mise](https://mise.jdx.dev) toolchain: `mise install` once.

```bash
mise run build:cli

# Asks for the invite code, a username and a password, then stores the
# minted token as a context in ~/.config/substratectl.
bin/substratectl register --server http://localhost:8080

# A fresh repository holds the core vocabulary and nothing else, so install
# the bundle that declares tasks. It requires people (tasks name an
# assignee) and scheduling (a task may repeat), so those two come first.
bin/substratectl apply \
  -f kinds/people.substrate.reamde.dev/bundle.yaml \
  -f kinds/people.substrate.reamde.dev/organization.yaml \
  -f kinds/people.substrate.reamde.dev/person.yaml \
  -f kinds/people.substrate.reamde.dev/team.yaml
bin/substratectl apply \
  -f kinds/scheduling.substrate.reamde.dev/bundle.yaml \
  -f kinds/scheduling.substrate.reamde.dev/recurring.yaml \
  -f kinds/scheduling.substrate.reamde.dev/occurrencelog.yaml
bin/substratectl apply \
  -f kinds/tasks.substrate.reamde.dev/bundle.yaml \
  -f kinds/tasks.substrate.reamde.dev/project.yaml \
  -f kinds/tasks.substrate.reamde.dev/task.yaml \
  -f kinds/tasks.substrate.reamde.dev/tasklog.yaml

cat <<'EOF' | bin/substratectl apply -f -
kind: tasks.substrate.reamde.dev/task
data:
  properties:
    name: Buy milk
    dueAt: 2026-08-13T09:00:00Z
EOF

bin/substratectl get tasks

# <id> is the record id `get tasks` printed. The done transition stamps
# completedAt for you.
bin/substratectl patch tasks <id> --state status=done

# Replay the changelog (--from resumes after a sequence number), then keep
# following it. Your task's create and its done transition land at the end.
bin/substratectl watch --from 1
```

The console's Registry page installs the same bundles from the compiled-in
catalog, through the same validation.
[docs/getting-started.md](docs/getting-started.md) walks this path in full.

## Manage them with a function and an agent

Automation is records too. One file declares a bundle of your own holding a
**function** (real code the substrate runs) and an **agent** (an LLM loop
with tools), both scoped to exactly the kinds they may touch:

```yaml
# chores.yaml
kind: core.substrate.reamde.dev/authority
metadata:
  id: chores.example
data:
  version: 1
---
kind: core.substrate.reamde.dev/bundle
metadata:
  id: chores.example/chores
data:
  authority: chores.example
  description: Task automations of my own.
  requires:
    - tasks.substrate.reamde.dev
  installs:
    - chores.example/triage
    - chores.example/assistant
---
kind: core.substrate.reamde.dev/function
metadata:
  id: chores.example/triage
data:
  authority: chores.example
  description: Raise every open task past its due date to urgent.
  runtime: python
  permissions:
    reads:
      kinds:
        - tasks.substrate.reamde.dev/task
    writes:
      - tasks.substrate.reamde.dev/task
  source: |
    from datetime import datetime, timezone

    def main(input, host):
        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        page = host.records.list(["tasks.substrate.reamde.dev/task"],
                                 where={"status": {"eq": "open"}})
        raised = 0
        for task in page.get("records") or []:
            props = task.get("properties") or {}
            due = props.get("dueAt")
            if due and due < now and props.get("priority") != "urgent":
                host.effects.patch("tasks.substrate.reamde.dev/task",
                                   task["id"],
                                   properties={"priority": "urgent"})
                raised += 1
        return {"output": {"raised": raised}}
---
kind: core.substrate.reamde.dev/agent
metadata:
  id: chores.example/assistant
data:
  authority: chores.example
  description: Reads and updates my tasks on request.
  prompt: |
    You manage the user's tasks. Read them with the query tool; create,
    complete and reprioritize them with mutate. Keep answers short.
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
        - tasks.substrate.reamde.dev/task
    writes:
      - tasks.substrate.reamde.dev/task
```

Apply it, then run the function by hand:

```bash
bin/substratectl apply -f chores.yaml
bin/substratectl function call chores.example/triage
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
    callable: core.substrate.reamde.dev/function/chores.example/triage
EOF
```

[docs/functions.md](docs/functions.md) is the whole contract: the host SDK,
permissions, triggers and the sandbox.

## Give the agent a model

A fresh repository holds no LLM provider, and a substrate cannot invent a
key. The `assistant` above names the provider row `anthropic`; install the
example provider rows from the tree (or from the console's Registry, under
Examples):

```bash
bin/substratectl apply \
  -f kinds/llm.examples.substrate.reamde.dev/bundle.yaml \
  -f kinds/llm.examples.substrate.reamde.dev/providers.yaml
```

Then put a key on the row. It is an ordinary record write, and `apiKey` is
secret-typed, so it reads back redacted ever after:

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
budgets.

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
