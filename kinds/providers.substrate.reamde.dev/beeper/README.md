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
