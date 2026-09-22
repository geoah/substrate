# The Linear bundle

Package `providers.substrate.reamde.dev/linear`: a provider that mirrors one
Linear workspace in Linear's own shape, read only. It authenticates with a
personal API key pasted onto the `config` record, not a consent flow. A
mapping onto a person or a task belongs to the package that owns the target,
so this closure declares none.

`bundle.yaml` is the closure (the config and account kinds, the eleven
mirrors and the sync function) and `triggers.yaml` is the delivery wiring: the
on-connect, hourly and sync-now triggers. They are the contract; this file is
not.

The key, the toggles, how a bounded walk resumes across ticks and what version
15 changed:
[docs/bundles-catalog.md#linear](../../../docs/bundles-catalog.md#linear).

## Kinds

| Kind | What it is |
| --- | --- |
| `config` | the pasted Linear API key and the pinned API origin |
| `account` | one connected workspace: the toggles, the viewer, the sync's own state |
| `organization` | the workspace itself, from the `organization` query |
| `team` | one team, with its cycle, estimation and workflow settings |
| `user` | one workspace member, disabled and app members included |
| `workflowstate` | one issue status a team's workflow moves an issue through |
| `issuelabel` | one issue label |
| `projectstatus` | one project status the workspace defines |
| `project` | one project, with its status, lead, teams and members |
| `cycle` | one cycle, a span of planned work |
| `issue` | one issue, its heading at `issueTitle` |
| `comment` | one comment on an issue |
| `reaction` | one emoji reaction on an issue or a comment |
