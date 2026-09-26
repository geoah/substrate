---
type: breaking
release: v0.88.0
---

# Six provider bundles are replaced with reshaped versions that rename kinds and functions

Anyone with a shipped provider installed is hit. v0.88.0 replaces beeper 10,
github 12, google 18, linear 14, notion 9 and whoop 9 with versions 13, 21,
30, 15, 11 and 11, and adds slack 5. Clients reading these mirrors break on
the renames and removals. The per-provider "Upgrading from version N"
paragraphs in [the bundles catalog](../bundles-catalog.md) list every change.
The kind and function renames are:

- [Google](../bundles-catalog.md#google): kinds `event` to `calendarevent`,
  `series` to `calendarseries`, `thread` to `gmailthread`, `message` to
  `gmailmessage`; functions `contactssync` to `synccontacts`, `gmailsync` to
  `syncgmail`, `calendarsync` to `synccalendar`.
- [GitHub](../bundles-catalog.md#github): `account.syncCursor` to
  `syncCursors`, `account.login` to `user`, `htmlURL` to `htmlUrl`, `title` to
  `issueTitle` and `pullRequestTitle`; `raw` is gone.
- [Linear](../bundles-catalog.md#linear): function `issuessync` to
  `linearsync`; triggers to `linear-on-connect` and `linear-scheduled`;
  `issue.assignee` is a reference at `linear/user`; `config` holds `apiKey`.
- [WHOOP](../bundles-catalog.md#whoop): `recovery.cycleId` to `recovery.cycle`,
  `workout.sport` to `sportName` and `sportId`.
- [Notion](../bundles-catalog.md#notion): function `workspacesync` to
  `notionsync`; rows move from `database` to `datasource`.
- [Beeper](../bundles-catalog.md#beeper): kind `room` is removed; `chat`
  replaces it.

The `people` sample drops the `linearissueperson` mapping.

## What to do

1. Run `substratectl catalog` to preview each provider's upgrade. An upgrade
   that drops a kind with live rows is refused with
   `kind <ref> has <n> live records: delete or migrate them first`.
2. Delete those rows, or run `substratectl bundle disable <id>` and
   `substratectl bundle purge <id> --yes`, which also deletes the accounts.
   The next sync writes the new kinds.
3. Upgrade with `substratectl install providers.substrate.reamde.dev/<name>`,
   and reconnect any purged account.
4. Import the `people` and `tasks` samples again so their mappings read the
   new kinds.
5. Rewrite clients against the new names.
