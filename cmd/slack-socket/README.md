# slack-socket

Holds a Slack Socket Mode connection and forwards every Events API callback,
in batches, to the `slack-live-events` webhook trigger of the
[`slacklive`](../../kinds/providers.substrate.reamde.dev/slacklive/README.md)
bundle. Without it, the Slack bundle's poll misses reactions and edits on
messages older than a day; with it, those events reach the substrate within
about a second.

It is configured by environment variables alone: a Slack app-level token and
the webhook URL. Any Slack app with Socket Mode on works.

## What it does

1. Opens a Socket Mode WebSocket with the app-level token (`xapp-`,
   `connections:write`). Socket Mode needs no bot token, so none is configured.
2. For each `events_api` envelope, queues its `payload` (the Events API
   callback JSON) byte for byte. It never decodes the payload into slack-go
   structs, so fields slack-go does not know survive. Event types slack-go
   cannot parse (it reports them as bad messages) are recovered from the raw
   envelope and forwarded the same way.
3. Flushes the queue when it holds 200 events, when the body reaches 512 KiB,
   or 1 s after the first queued event arrived, whichever comes first. A flush
   is one POST of `{"version":1,"events":[<payload>,...]}` with
   `Content-Type: application/json` to `SUBSTRATE_WEBHOOK_URL`, with a 1.5 s
   timeout. Batches go out one at a time, in arrival order.
4. Acks each envelope to Slack only after the POST carrying it returned `202`.
   On any other status (including `503` with `Retry-After`), a timeout or a
   network error, it logs the status and batch size and leaves the envelopes
   unacked; Slack redelivers them. Slack wants an ack within 3 s: 1 s of
   batching plus at most 1.5 s of POST leaves room.
5. Acks `interactive`, `slash_commands` and unknown envelope types without
   doing anything with them. `hello` is logged with `num_connections`.
6. On `disconnect`, slack-go closes the socket and opens a new one (covered by
   `TestSocketModeEndToEnd`); this service logs and counts the reconnect. If
   slack-go gives up (revoked or invalid token), the process exits 1.
7. On SIGTERM it stops reading new events, forwards and acks the pending
   batch, then closes the socket.

Redelivery means the substrate can see an event twice: once from a batch it
accepted whose ack was lost, and again from Slack's retry. `slackevents`
runs each `event_id` of a batch once and writes nothing an older copy of an
event could undo. Slack retries an unacked event a few times and then drops
it, so a substrate outage longer than Slack's retry window loses those
events. The Slack bundle's poll still picks up new messages, but not
reactions or edits on older ones.

The substrate webhook door reads at most 1 MiB of a JSON body, so the 512 KiB
batch cap leaves headroom. A single event larger than 1 MiB is never
accepted.

## Configuration

| Variable                | Default  | Meaning                                         |
| ----------------------- | -------- | ----------------------------------------------- |
| `SLACK_APP_TOKEN`       | required | App-level token, `xapp-` prefix                 |
| `SUBSTRATE_WEBHOOK_URL` | required | Full webhook URL, trigger key included          |
| `LISTEN_ADDR`           | `:8080`  | Address for `/healthz` and `/metrics`           |
| `BATCH_MAX_EVENTS`      | `200`    | Flush at this many events                       |
| `BATCH_MAX_BYTES`       | `524288` | Flush before the body would pass this size      |
| `BATCH_MAX_WAIT`        | `1s`     | Flush this long after the first queued event    |
| `POST_TIMEOUT`          | `1500ms` | Timeout of one webhook POST                     |
| `ACK_TIMEOUT`           | `1s`     | Give up acking a batch after this long          |
| `SHUTDOWN_WAIT`         | `5s`     | Wait for the socket and HTTP server on shutdown |
| `DEBUG`                 | `false`  | Debug logs, including slack-go's own            |

The webhook URL is
`https://<server>/webhooks/<authority>/slack-live-events/<key>`, where the key
is the one set on the trigger record (the slacklive README says how). The URL
holds the key in its path, so the service never logs it: the startup line
shows only the host, and POST errors have the URL stripped. `DEBUG=true`
makes slack-go log every incoming frame, which includes message text.

Run one instance per Slack app. A second socket makes Slack split the events
between the two, and each sees only part of them.

## The Slack app

The app needs Socket Mode on, an app-level token with `connections:write`,
and event subscriptions for what `slackevents` applies: `message.channels`,
`message.groups`, `message.im`, `message.mpim`, `reaction_added`,
`reaction_removed`, `pin_added`, `pin_removed`, `channel_created`,
`channel_rename`, `group_rename`, `channel_archive`, `channel_unarchive`,
`group_archive`, `group_unarchive`, `member_joined_channel`,
`member_left_channel`, `user_change`, `team_join`, `file_shared`,
`file_change` and `file_deleted`.

## Endpoints

- `GET /healthz`: `200 {"connected":true}` while the socket is open, `503
  {"connected":false}` while connecting or backing off.
- `GET /metrics`: Prometheus text format.

| Metric                                      | Type    | Meaning                                             |
| ------------------------------------------- | ------- | --------------------------------------------------- |
| `slack_socket_events_received_total`        | counter | `events_api` envelopes received                     |
| `slack_socket_events_redelivered_total`     | counter | Received envelopes with `retry_attempt` above 0     |
| `slack_socket_events_forwarded_total`       | counter | Events in batches the webhook answered `202`        |
| `slack_socket_events_failed_total`          | counter | Events in batches that failed or got another status |
| `slack_socket_batches_forwarded_total`      | counter | Batches answered `202`                              |
| `slack_socket_batches_failed_total`         | counter | Batches that failed or got another status           |
| `slack_socket_acks_failed_total`            | counter | Accepted batches whose acks could not be sent       |
| `slack_socket_reconnects_total`             | counter | Connections opened after the first                  |
| `slack_socket_connected`                    | gauge   | 1 while the socket is open                          |
| `slack_socket_last_event_timestamp_seconds` | gauge   | Unix time of the last received event                |

## Running it

```sh
SLACK_APP_TOKEN=xapp-... \
SUBSTRATE_WEBHOOK_URL=http://localhost:8080/webhooks/example.com/slack-live-events/<key> \
LISTEN_ADDR=:8090 \
go run ./cmd/slack-socket
```

The image is a stage of the repository's one Dockerfile:

```sh
docker build --target slack-socket -t slack-socket .
```

Every commit on `main` whose `ci` run went green is published as
`ghcr.io/geoah/substrate-slack-socket:main-<sha12>` (the commit's first
twelve hex digits), for `linux/amd64` and `linux/arm64`, and `latest` names
the tip of `main`. The tags follow the server image's rules
([operations](../../docs/operations.md#the-published-image)). Releases do not
publish the bridge yet, so it has no version tags.

It runs as uid 65532 and serves `/healthz` and `/metrics` on port 8080.

## Tests

```sh
go test -race ./cmd/slack-socket/
```

The batcher tests use an `httptest` webhook (flush on count, bytes and time;
payload bytes kept; ack only on `202`; no ack on `200`, `503`, timeout or
network error). `socket_test.go` runs the real slack-go client against a fake
Socket Mode server: two events (one of a type slack-go lacks) are forwarded and
acked, a `disconnect` leads to a reconnect, and a `503` leaves the event
unacked.

slack-go is pinned at v0.29.0, the version this bridge was written and
tested against.
