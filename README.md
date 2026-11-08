# substrate

A private home for everything one person keeps. The things only you track,
like recipes, plants, the people you know and your notes and sketches, sit
beside the ones that already live elsewhere, like mail, calendar, messages and
health, all linked to each other. An assistant, Moss, knows its way around,
and the apps you use read and write the same data instead of each keeping
its own copy. It runs on a machine you choose.

## For users

- **Keep anything.** Say what you want to keep and what to note about it, and
  the substrate makes room for it: a plant and when it was last watered, a
  recipe and who taught it to you. Start from scratch, or from a ready-made
  collection (tasks, people, calendars, reading, notes) and change it to fit.
- **Everything connects.** A recipe points to the person who taught it, a
  task to its project. Open anything and see what links to it.
- **Moss, your assistant.** Every substrate ships with one. It answers from
  your own records, looks up the world with sources, proposes new collections
  and fills them in, and asks before it removes anything. Standing
  instructions ("keep answers short") shape how it works, and each one is
  yours to edit or switch off.
- **Bring in what you have.** Google mail, calendar, contacts and Drive;
  Slack, Beeper, Notion, GitHub and Linear; sleep, workouts and lab results
  from Apple Health and WHOOP. Each lands as records you own and stays in
  sync.
- **Apps ask first.** An app, or an assistant such as Claude, Codex or Cursor
  over [MCP](docs/mcp.md), gets in only once you approve it, and sees only
  what you allowed. The Substrate apps for iPhone, iPad and Mac (Chat, Tasks,
  Workouts, Sync and the console) are on
  [reamde.dev/substrate](https://reamde.dev/substrate).
- **Yours to keep.** Every change is kept, in order, so nothing is quietly
  lost, and you can download everything, as one file, whenever you like.

To get one, have someone you trust host it for you, or run your own: the
next section is all it takes.

## For developers

```bash
docker compose up --build
```

That is the whole thing: Postgres, the API and the web console at
<http://localhost:8080>. Register with a name and a password; this local
substrate asks for no invite code and no second factor. Put an Anthropic key
on the `anthropic` provider (the console's Providers page) and Moss answers.

Under the console, everything is a typed **record** in one **repository**,
computed from an append-only **changelog**. A **kind**, declared in YAML,
validates every write; kinds, tokens, functions (Python the substrate runs
on a trigger) and agents are records too. One REST API reads and writes all
of it, an app gets a scoped token through an OAuth flow that needs no client
registration, and `substratectl` is the CLI for the rest.

- [Your own kinds, a function and an agent](docs/walkthrough.md): one YAML
  file, from declaration to an agent that works on it.
- [Getting started](docs/getting-started.md), the [API](docs/api.md),
  [apps](docs/apps.md), [MCP](docs/mcp.md) and the
  [documentation index](docs/README.md).
- [Operations](docs/operations.md): before anyone else can reach it, set
  `SUBSTRATE_INVITE_CODE`, unset `SUBSTRATE_INSECURE_DISABLE_TOTP`, back up
  `SUBSTRATE_CREDENTIAL_KEY` and `SUBSTRATE_DATA_ROOT`, and put TLS in front.

### Upgrading

Do not upgrade by hand: have an agent follow the
[upgrade runbook](skills/substrate-runbook-upgrade/SKILL.md). A new binary
migrates the database and every repository it opens, and only a backup taken
beforehand undoes that. The runbook reads every [CHANGELOG.md](CHANGELOG.md)
section between your version and the target, writes a plan for you to
approve, takes the backup, deploys and verifies. In Claude Code:

```
/plugin marketplace add geoah/substrate
/plugin install substrate-runbooks@substrate
```

### Working on this code

```bash
mise install            # once
mise run dev            # Postgres in a container + the server from the tree on :8080
mise run console:dev    # the console on :5173
mise run test           # the Go suite
mise run ci             # every CI job, locally
```

[AGENTS.md](AGENTS.md) is the working guide, for people and agents alike,
[docs/testing.md](docs/testing.md) maps the suites,
[docs/decisions](docs/decisions/README.md) holds one page per design choice,
and [SECURITY.md](SECURITY.md) says how to report a vulnerability.
