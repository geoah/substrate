# The Slack live events bundle

Package `providers.substrate.reamde.dev/slacklive`: applies Slack's live
events to the rows the [Slack bundle](../slack/README.md) mirrors. The Slack
bundle polls, so it sees a reaction, an edit or a delete only while the
message is within a day of its conversation's history cursor. With this
bundle and the Socket Mode bridge running, those changes reach the mirror
within seconds.

It is opt-in on two counts. Nothing installs it with the Slack bundle, and
it ships no trigger, so installing it exposes no endpoint. The operator
writes the webhook trigger, key included, in one write (below).

`bundle.yaml` is the closure (the package, the bundle and one function,
`slackevents`) and `settings.yaml` holds the token and the API origin. They
are the contract; this file is not.

## What it needs

- The Slack bundle, at version 22 or later (`requires:` refuses the install
  otherwise), with one account that has `enabledMessages` on and has synced
  once: the events path learns the account's workspace from the row the
  walk's `auth.test` wrote, and does nothing before that.
- The Socket Mode bridge, [`cmd/slack-socket`](../../../cmd/slack-socket/README.md),
  running with an app-level token (`xapp-`, `connections:write`) of a Slack
  app that has Socket Mode on and subscribes to the events below. The bridge
  holds the websocket, acknowledges each `events_api` envelope after the
  trigger accepted it, and POSTs batches to it.
- The user token, pasted into this bundle's `userToken` secret. It is the
  same `xoxp-` token as the Slack bundle's `config`, pasted a second time: an
  injected input must be the bundle's own kind, so the Slack bundle's config
  record never reaches this function. The function calls `reactions.get`,
  `conversations.info` and `files.info` with it.

## The batch

One POST, `Content-Type: application/json`, carries:

```json
{"version": 1, "events": [{"type": "event_callback", "team_id": "T…", "event_id": "Ev…", "event_time": 1760000000, "event": {"type": "message", "…": "…"}}]}
```

Each element of `events` is the `payload` of a Socket Mode `events_api`
envelope, byte for byte. A body that is not version 1 is skipped.

## What it writes

`slackevents` applies the batch to the Slack bundle's `team`, `user`,
`conversation`, `message`, `bot` and `file` rows, under the same ids the walk
writes:

- `message` and its thread replies, `message_changed`, `message_deleted`;
- `reaction_added` and `reaction_removed`, as the message's whole reaction
  set read from `reactions.get` (`full=true`), never as a delta;
- `pin_added`, `pin_removed`;
- `channel_created`, `channel_rename`, `group_rename`, the archive and
  unarchive events, `member_joined_channel`, `member_left_channel`;
- `user_change`, `team_join`;
- `file_shared`, `file_change` (read from `files.info`), `file_deleted`.

It moves no history cursor, writes no `conversationsync` row and no account
property, so the Slack bundle's walk still reads what the socket missed.

Every write survives redelivery and reordering. A batch runs in `event_ts`
order with each `event_id` once. An edit older than the row's edit is not
written over it. A message the batch deletes is not written by the batch's
other events. A callback whose `team_id` is not the account's workspace is
ignored, and so is an event type the body does not know.

One batch makes at most 150 Slack calls in 75 seconds, and one invocation
stops starting calls after 10 seconds so it ends inside the 30-second
function timeout; the rest of the batch runs on the next page. A `429` ends
the batch's Slack calls: the events that need no call are still applied, and
the ones that need one are counted `limited` and left to the walk. A refused
token (`invalid_auth`, `not_authed`, `account_inactive`, `token_revoked`,
`token_expired`) ends the batch's calls the same way: those events are
counted `unauthorized`, the output carries `authError`, and the log says to
paste a current token into `userToken`. Rotating the Slack bundle's token
does not rotate this one. A target Slack refuses (`channel_not_found`,
`message_not_found`) is skipped and the batch goes on.

## Turning it on

1. Install the Slack bundle, connect it and let it sync once.
2. Install this bundle and fill in the `userToken` secret.
3. Generate a key. It must match `^[A-Za-z0-9_-]{16,128}$`;
   `openssl rand -hex 32` prints 64 characters that do. Write the whole
   trigger record, key and `enabled: true` together, in one
   `substratectl apply -f`:

   ```yaml
   kind: substrate.reamde.dev/core/trigger
   metadata:
     id: slack-live-events
   data:
     properties:
       enabled: true
       label: Slack live events
       source:
         webhook:
           key: <the key from openssl>
       callable: substrate.reamde.dev/core/function/providers.substrate.reamde.dev/slacklive/slackevents
   ```

   Never write this record without the key: the webhook door checks a key
   only when the trigger declares one, so a keyless enabled trigger takes a
   POST from anyone who can reach the server, and every POST writes Slack
   rows. For the same reason the bundle ships no trigger at all, not even a
   disabled one: the console's Resume would enable it without a key.
4. Start the bridge with
   `SUBSTRATE_WEBHOOK_URL=https://<server>/webhooks/<authority>/slack-live-events/<key>`.

The trigger is the operator's record, so installing or upgrading this bundle
leaves it and its key alone. Uninstalling the bundle removes it.

## Upgrading from Slack bundle 21

Slack bundle 21 shipped live events inside the Slack bundle, with a
`slack-events` webhook trigger that was enabled and had no key. It was only
ever on `main` and the `latest` image, never in a release. Upgrading to 22
removes the events code but not that trigger record: it stays enabled and
keyless, and a POST to it now starts a `slacksync` walk. A deployment that
ran 21 moves over in this order:

1. Stop the bridge, so nothing posts to `slack-events` meanwhile.
2. `substratectl delete substrate.reamde.dev/core/trigger slack-events`
3. Upgrade the Slack bundle to 22 (`substratectl install
   providers.substrate.reamde.dev/slack`, or the console's Update).
4. Install this bundle, fill in `userToken`, and write the keyed
   `slack-live-events` trigger (above).
5. Point the bridge's `SUBSTRATE_WEBHOOK_URL` at
   `/webhooks/<authority>/slack-live-events/<key>` and start it again.

## Known gap

A `message` event that arrives in a later batch than its own
`message_deleted` writes the message back. Within one batch the delete wins,
but nothing remembers a delete across batches, so a redelivered or late
`message` after the delete was applied mints the row again.
