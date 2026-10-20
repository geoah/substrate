# The Beeper bundle

Package `providers.substrate.reamde.dev/beeper`: a provider that mirrors one
machine's Beeper inbox over the Beeper Client API, which Beeper Desktop serves
locally at `http://localhost:23373`. It authenticates with a token pasted onto
its `config` record, since no OAuth flow runs for this bundle. The sync reads
only and downloads no media. `sendmessage` is the one write: a function a
caller invokes, which sends one message with the same token.

`bundle.yaml` is the closure (the config and account kinds, the six mirrors,
the per-chat state kind, the sync function and `sendmessage`) and `triggers.yaml` is the
delivery wiring: on connect, every 15 minutes, and "sync now". They are the
contract; this file is not.

## Operator setup

Beeper Desktop serves its API on a private address, and the sandbox's connect
gate refuses a function body's connect to a private address unless the server
lists it in `SUBSTRATE_SANDBOX_EGRESS_ALLOW`
([configuration](../../../docs/operations.md#configuration)). Without the
entry every sync fails with
`the sandbox's connect gate refused 127.0.0.1:23373; allow it with SUBSTRATE_SANDBOX_EGRESS_ALLOW=127.0.0.1`.

- Beeper Desktop on the machine the server runs on, the server not in a
  container: set `SUBSTRATE_SANDBOX_EGRESS_ALLOW=127.0.0.1,::1` and leave the
  config's `apiBase` at its default.
- The server in a container, or Beeper Desktop on another machine: set the
  config's `apiBase` to an address the server reaches Desktop at, a LAN or
  tailnet address such as `http://100.101.102.103:23373`, and list that
  address.

The server reads the variable once, so restart it after setting it.
`SUBSTRATE_EGRESS_ALLOW` is the gate for the server's own dials and does not
admit a body's connect.

## When Beeper Desktop is not reachable

When the sync cannot reach Beeper Desktop at all (the computer asleep, the app
closed, no route to its address, or a name that does not resolve), the
account reads `syncState: throttled` with the message "Beeper Desktop is not
reachable (since <time>); syncing resumes when it is back", and no
`syncError`. `unreachableSince` holds the first failed attempt of the stretch.
The 15-minute tick keeps trying, at one connect attempt per tick, and Sync now
works as usual. When a run reaches Beeper again, the walk resumes where it
stopped. A run that ends having reached Beeper deletes `unreachableSince`; a
run that got an answer and then lost the connection starts a new stretch at
its own attempt.

A 401, a 5xx, a body that is not JSON, a read timeout, and the sandbox gate's
refusal still read `erroring`, so a missing `SUBSTRATE_SANDBOX_EGRESS_ALLOW`
entry is not mistaken for a sleeping computer
([decision 0183](../../../docs/decisions/0183-an-unreachable-beeper-desktop-throttles-the-sync-and-throttled-reads-as-waiting.md)).

## Kinds

| Kind | What it is |
| --- | --- |
| `config` | the Beeper Desktop origin and the token this bundle spends |
| `account` | the connection: the owner's toggles and the connector's progress |
| `bridge` | one network this Beeper can run |
| `chataccount` | one login on one bridged network |
| `user` | one person Beeper knows, keyed on a Matrix MXID |
| `label` | one label the owner puts on a chat |
| `chat` | one conversation, whose id is a Matrix room id |
| `message` | one message in one chat, on whatever network sent it |
| `chatsync` | where this connector's walk of one chat got to |

What it writes, where the token comes from, the toggles on the account, how
far the first walk reaches and how a bounded drain resumes:
[docs/bundles-catalog.md#beeper](../../../docs/bundles-catalog.md#beeper).
