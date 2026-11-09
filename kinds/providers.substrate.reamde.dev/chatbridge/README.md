# The chat bridge provider

Package `providers.substrate.reamde.dev/chatbridge`: every chat network the
chat bridge program signs in to as you (WhatsApp first), mirrored into one
set of kinds, and what you or an agent ask to be done in a chat, carried out
by the bridge. The bridge is a separate program
([the plan](../../../docs/plans/chat-bridge.md)) that runs beside the
server, at home or on a Mac. It dials out: it POSTs what happened on the
networks to the `chatbridge-events` webhook, one batch at a time, and it
follows the change feed for the `chataction` records it should carry out.
The server never reaches the bridge.

`bundle.yaml` is the closure (seven kinds, the `chatevents` function and six
agent tools), `triggers.yaml` the webhook, `policies.yaml` the gate that
holds agents' requests, and `views.yaml` how a chat and an account read in
the console. They are the contract; this file says how to speak to them, and
`internal/providertest` holds both to it.

## What lands where

| Kind | One record is |
| --- | --- |
| `instance` | one running copy of the bridge program: its name, build, networks, and how far its stream of events has been applied |
| `account` | one sign-in on one network through one instance: its connection, the network's warnings, and your settings for it |
| `user` | someone on a network, as one sign-in sees them: names, handle, numbers, addresses |
| `chat` | one conversation: direct, group, channel or space, with its members, flags, what the network allows in it, and how far you read it |
| `message` | one message in one chat; `at` is when it was sent |
| `messageattachment` | one file sent with a message, and its bytes once fetched |
| `chataction` | one thing you or an agent asked to be done in a chat as you, and what came of it |

The network is a property, never a name prefix: a WhatsApp chat and a Signal
chat are both a `chat` ([0251](../../../docs/decisions/0251-one-provider-package-carries-every-chat-network-and-the-network-is-a-property.md)).
`account` binds core's `sync` trait, which the Providers page reads, but not
`accountconfig`: the engine lets only you create an `accountconfig` record,
and these are created by the deliveries when you sign in on the bridge.

## Approving a bridge

The bridge is an app ([apps](../../../docs/apps.md)) and gets its token
through the authorization flow. Its request:

- `scope`: `write:providers.substrate.reamde.dev/chatbridge/*` and
  `write:providers.substrate.reamde.dev/chatbridge/instance`. A glob never
  reaches an `internal` kind, so the instance is named. `write` includes
  read, and these cover every kind the package's code reads and writes
  ([0215](../../../docs/decisions/0215-code-an-approval-installs-reaches-no-further-than-the-scopes-it-approves.md)).
- `authorization_details`:

  ```json
  [
    {"type": "package", "package": "providers.substrate.reamde.dev/chatbridge", "version": 1},
    {"type": "webhook", "trigger": "chatbridge-events"}
  ]
  ```

- a loopback `client_id`, `http://127.0.0.1/chatbridge`, with PKCE and a
  loopback redirect, so its writes are `app:local`.

Approving installs the package (or upgrades it to version 1), turns the
webhook on and gives it a key, and the token carries
`webhook:chatbridge-events`: the bridge delivers with the token itself as
`Authorization: Bearer`. The trigger ships off and keyless, so nothing
delivers before an approval
([0250](../../../docs/decisions/0250-a-chat-bridge-is-an-app-whose-instances-trust-each-other-and-delivers-one-ordered-stream-per-instance.md)).

Every instance you approve for one repository reaches every account of the
package. An instance id in a batch is the bridge's claim, not something the
server checks: instances are trusted programs of yours, and an instance acts
only on the accounts it runs by convention between them.

## Delivery

One request is `POST /webhooks/<authority>/chatbridge-events`,
`Content-Type: multipart/form-data`:

- a part named `batch`, with no filename and the media type
  `text/plain; charset=utf-8`, holding the batch JSON;
- one part per file the batch names, each with a filename and its media
  type. The door stores each as a blob before the function runs, and a
  `Media` value in the batch, `{"part": "<part name>"}`, names one.

At most 200 events and 1 MiB of JSON per batch, history included; a file at
most 5 MiB and a request at most 32 MiB.

**One batch in flight, applied before the next.** The `instance` row's
`appliedSeq` is the stream's applied watermark. `chatevents` reads it (0
when the row is absent), leaves every event at or below it alone, so a
resent batch writes nothing, and fails the delivery when `first` is past
`appliedSeq + 1`: an earlier batch has not applied, and the retry waits for
it. It applies the rest in `seq` order and moves `appliedSeq` to `last` in
the same write, under the version it read, so two fires of one batch cannot
both apply. The bridge sends batch N + 1 once the instance row reads
`appliedSeq` at or past N's `last`, which the change feed tells it. A fire
that fails three times parks on the trigger's parked list, which the
token's `webhook:` scope reads.

**A bad event does not fail a batch.** An event `chatevents` cannot apply
(a malformed field, a value outside an enum, a file part the request does
not carry, a target the mirror does not hold) is skipped, counted in the
instance's `skippedEvents`, and named in its `lastSkipped`; the batch goes
on. What fails the delivery is the batch itself: a request that is not
multipart, no `batch` part, a part that is not JSON, a `version` other
than 1, no `instance`, `first` or `last` not a sequence number or `first`
past `last`, more than 200 events, an event with no `seq` between `first`
and `last`, or one `seq` twice; and a gap, a read past its budget, or a
write the engine refuses.

**A new store is a new instance.** The instance id is minted with the
bridge's store. A bridge that lost its store starts at `seq` 1 under a new
id, and its `account.upsert` events point the accounts at it. Deleting an
instance row stalls its bridge: its next batch continues a sequence the
server no longer holds.

## The batch

```json
{
  "version": 1,
  "instance": "0b3f6f2e-6d8c-4c47-9a7e-2d8b1f5d9c11",
  "first": 1042,
  "last": 1043,
  "events": [
    {"seq": 1042, "at": "2026-11-09T10:00:01.123Z", "type": "user.upsert",
     "network": "whatsapp", "account": "447700900123",
     "user": {"id": "lid:113920541", "fullName": "Ada", "phoneNumbers": ["+447700900456"]}},
    {"seq": 1043, "at": "2026-11-09T10:00:01.124Z", "type": "message.upsert",
     "network": "whatsapp", "account": "447700900123", "chat": "447700900456@s.whatsapp.net",
     "backfill": false,
     "message": {"id": "3EB0C1F2A9", "sender": "lid:113920541", "fromOwner": false,
                 "at": "2026-11-09T09:59:58.000Z", "content": "text", "text": "see you at 7"}}
  ]
}
```

Every id in an event is the network's or the bridge's own, verbatim
(`account` is the bridge's login id, `chat`, `user`, `message` and `part`
the network's), except `instance` and an action's id, which name the
bridge and a `chataction` record. `Instant` is RFC 3339, in UTC with
milliseconds. An event's one `at` is when the bridge saw it, and for
`chat.read`, `message.delete` and `message.status`, which carry no other,
when it happened on the network. `text` is markdown. An absent field leaves a property as it
is, `null` clears it, and an empty list replaces a list with nothing.
`testdata/chatbridge` under `internal/providertest` holds three worked
batches, and this schema is held to them:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "A chat bridge batch, version 1",
  "type": "object",
  "required": ["version", "instance", "first", "last", "events"],
  "additionalProperties": false,
  "properties": {
    "version": {"const": 1},
    "instance": {"type": "string", "minLength": 1},
    "first": {"type": "integer", "minimum": 1},
    "last": {"type": "integer", "minimum": 1},
    "events": {"type": "array", "maxItems": 200, "items": {"$ref": "#/$defs/event"}}
  },
  "$defs": {
    "event": {"oneOf": [
      {"$ref": "#/$defs/instance.hello"}, {"$ref": "#/$defs/account.upsert"},
      {"$ref": "#/$defs/account.state"}, {"$ref": "#/$defs/account.removed"},
      {"$ref": "#/$defs/user.upsert"}, {"$ref": "#/$defs/chat.upsert"},
      {"$ref": "#/$defs/chat.delete"}, {"$ref": "#/$defs/chat.read"},
      {"$ref": "#/$defs/message.upsert"}, {"$ref": "#/$defs/message.delete"},
      {"$ref": "#/$defs/message.reactions"}, {"$ref": "#/$defs/message.status"},
      {"$ref": "#/$defs/action.claimed"}, {"$ref": "#/$defs/action.result"},
      {"$ref": "#/$defs/backfill.progress"}
    ]},
    "seq": {"type": "integer", "minimum": 1},
    "netId": {"type": "string", "minLength": 1},
    "netIdOrNull": {"type": ["string", "null"], "minLength": 1},
    "instant": {"type": "string", "format": "date-time"},
    "instantOrNull": {"type": ["string", "null"], "format": "date-time"},
    "textOrNull": {"type": ["string", "null"]},
    "flagOrNull": {"type": ["boolean", "null"]},
    "countOrNull": {"type": ["integer", "null"], "minimum": 0},
    "netIdsOrNull": {"type": ["array", "null"], "items": {"$ref": "#/$defs/netId"}},
    "mediaOrNull": {
      "type": ["object", "null"], "required": ["part"], "additionalProperties": false,
      "properties": {"part": {"type": "string", "minLength": 1}}
    },
    "instance.hello": {
      "type": "object", "additionalProperties": false,
      "required": ["seq", "at", "type", "name", "version", "networks"],
      "properties": {
        "seq": {"$ref": "#/$defs/seq"}, "at": {"$ref": "#/$defs/instant"},
        "type": {"const": "instance.hello"},
        "name": {"type": "string", "minLength": 1},
        "version": {"type": "string", "minLength": 1},
        "networks": {"type": "array", "items": {
          "type": "object", "required": ["network"], "additionalProperties": false,
          "properties": {"network": {"$ref": "#/$defs/netId"}, "displayName": {"type": "string"}}
        }}
      }
    },
    "account.upsert": {
      "type": "object", "additionalProperties": false,
      "required": ["seq", "at", "type", "network", "account"],
      "properties": {
        "seq": {"$ref": "#/$defs/seq"}, "at": {"$ref": "#/$defs/instant"},
        "type": {"const": "account.upsert"},
        "network": {"$ref": "#/$defs/netId"}, "account": {"$ref": "#/$defs/netId"},
        "remoteName": {"$ref": "#/$defs/textOrNull"}, "handle": {"$ref": "#/$defs/textOrNull"},
        "phoneNumber": {"$ref": "#/$defs/textOrNull"}, "self": {"$ref": "#/$defs/netIdOrNull"}
      }
    },
    "account.state": {
      "type": "object", "additionalProperties": false,
      "required": ["seq", "at", "type", "network", "account", "state"],
      "properties": {
        "seq": {"$ref": "#/$defs/seq"}, "at": {"$ref": "#/$defs/instant"},
        "type": {"const": "account.state"},
        "network": {"$ref": "#/$defs/netId"}, "account": {"$ref": "#/$defs/netId"},
        "state": {"enum": ["connecting", "connected", "transient_disconnect", "bad_credentials",
                           "logged_out", "banned", "unknown_error"]},
        "message": {"$ref": "#/$defs/textOrNull"},
        "warnings": {"type": ["array", "null"], "items": {
          "type": "object", "required": ["code"], "additionalProperties": false,
          "properties": {
            "code": {"enum": ["phone_offline", "temporary_ban", "reachout_timelock",
                              "client_outdated", "flood_wait", "checkpoint"]},
            "message": {"type": "string"}, "until": {"$ref": "#/$defs/instantOrNull"}
          }
        }}
      }
    },
    "account.removed": {
      "type": "object", "additionalProperties": false,
      "required": ["seq", "at", "type", "network", "account"],
      "properties": {
        "seq": {"$ref": "#/$defs/seq"}, "at": {"$ref": "#/$defs/instant"},
        "type": {"const": "account.removed"},
        "network": {"$ref": "#/$defs/netId"}, "account": {"$ref": "#/$defs/netId"}
      }
    },
    "user.upsert": {
      "type": "object", "additionalProperties": false,
      "required": ["seq", "at", "type", "network", "account", "user"],
      "properties": {
        "seq": {"$ref": "#/$defs/seq"}, "at": {"$ref": "#/$defs/instant"},
        "type": {"const": "user.upsert"},
        "network": {"$ref": "#/$defs/netId"}, "account": {"$ref": "#/$defs/netId"},
        "user": {
          "type": "object", "required": ["id"], "additionalProperties": false,
          "properties": {
            "id": {"$ref": "#/$defs/netId"},
            "fullName": {"$ref": "#/$defs/textOrNull"}, "handle": {"$ref": "#/$defs/textOrNull"},
            "phoneNumbers": {"type": ["array", "null"], "items": {"type": "string"}},
            "emails": {"type": ["array", "null"], "items": {"type": "string"}},
            "isBot": {"$ref": "#/$defs/flagOrNull"}, "isSelf": {"$ref": "#/$defs/flagOrNull"},
            "contactName": {"$ref": "#/$defs/textOrNull"}, "isContact": {"$ref": "#/$defs/flagOrNull"},
            "avatar": {"$ref": "#/$defs/mediaOrNull"}
          }
        }
      }
    },
    "chat.upsert": {
      "type": "object", "additionalProperties": false,
      "required": ["seq", "at", "type", "network", "account", "chat"],
      "properties": {
        "seq": {"$ref": "#/$defs/seq"}, "at": {"$ref": "#/$defs/instant"},
        "type": {"const": "chat.upsert"},
        "network": {"$ref": "#/$defs/netId"}, "account": {"$ref": "#/$defs/netId"},
        "chat": {
          "type": "object", "required": ["id"], "additionalProperties": false,
          "properties": {
            "id": {"$ref": "#/$defs/netId"},
            "category": {"enum": ["direct", "group", "channel", "space", null]},
            "chatName": {"$ref": "#/$defs/textOrNull"}, "topic": {"$ref": "#/$defs/textOrNull"},
            "avatar": {"$ref": "#/$defs/mediaOrNull"}, "otherUser": {"$ref": "#/$defs/netIdOrNull"},
            "members": {
              "type": ["object", "null"], "required": ["complete", "items"], "additionalProperties": false,
              "properties": {
                "complete": {"type": "boolean"},
                "items": {"type": "array", "items": {
                  "type": "object", "required": ["user", "membership"], "additionalProperties": false,
                  "properties": {
                    "user": {"$ref": "#/$defs/netId"},
                    "membership": {"enum": ["join", "invite", "leave", "ban"]},
                    "role": {"enum": ["member", "admin", "owner", null]}
                  }
                }}
              }
            },
            "mutedUntil": {"$ref": "#/$defs/instantOrNull"},
            "archived": {"$ref": "#/$defs/flagOrNull"}, "pinned": {"$ref": "#/$defs/flagOrNull"},
            "markedUnread": {"$ref": "#/$defs/flagOrNull"}, "readOnly": {"$ref": "#/$defs/flagOrNull"},
            "messageRequest": {"$ref": "#/$defs/flagOrNull"},
            "disappearAfterSeconds": {"$ref": "#/$defs/countOrNull"},
            "parent": {"$ref": "#/$defs/netIdOrNull"},
            "features": {
              "type": ["object", "null"], "additionalProperties": false,
              "properties": {
                "edit": {"type": "boolean"}, "editMaxAgeSeconds": {"type": "integer", "minimum": 0},
                "deleteForEveryone": {"type": "boolean"},
                "deleteMaxAgeSeconds": {"type": "integer", "minimum": 0},
                "reactions": {"type": "boolean"},
                "allowedReactions": {"type": "array", "items": {"type": "string"}},
                "replies": {"type": "boolean"}, "threads": {"type": "boolean"},
                "readReceipts": {"type": "boolean"}, "typing": {"type": "boolean"},
                "maxTextLength": {"type": "integer", "minimum": 0}
              }
            },
            "lastActivityAt": {"$ref": "#/$defs/instantOrNull"}
          }
        }
      }
    },
    "chat.delete": {
      "type": "object", "additionalProperties": false,
      "required": ["seq", "at", "type", "network", "account", "chat"],
      "properties": {
        "seq": {"$ref": "#/$defs/seq"}, "at": {"$ref": "#/$defs/instant"},
        "type": {"const": "chat.delete"},
        "network": {"$ref": "#/$defs/netId"}, "account": {"$ref": "#/$defs/netId"},
        "chat": {"$ref": "#/$defs/netId"}
      }
    },
    "chat.read": {
      "type": "object", "additionalProperties": false,
      "required": ["seq", "at", "type", "network", "account", "chat", "upTo"],
      "properties": {
        "seq": {"$ref": "#/$defs/seq"}, "at": {"$ref": "#/$defs/instant"},
        "type": {"const": "chat.read"},
        "network": {"$ref": "#/$defs/netId"}, "account": {"$ref": "#/$defs/netId"},
        "chat": {"$ref": "#/$defs/netId"}, "upTo": {"$ref": "#/$defs/netId"}
      }
    },
    "message.upsert": {
      "type": "object", "additionalProperties": false,
      "required": ["seq", "at", "type", "network", "account", "chat", "backfill", "message"],
      "properties": {
        "seq": {"$ref": "#/$defs/seq"}, "at": {"$ref": "#/$defs/instant"},
        "type": {"const": "message.upsert"},
        "network": {"$ref": "#/$defs/netId"}, "account": {"$ref": "#/$defs/netId"},
        "chat": {"$ref": "#/$defs/netId"}, "backfill": {"type": "boolean"},
        "message": {
          "type": "object", "required": ["id", "sender", "fromOwner", "at", "content"],
          "additionalProperties": false,
          "properties": {
            "id": {"$ref": "#/$defs/netId"}, "sender": {"$ref": "#/$defs/netId"},
            "fromOwner": {"type": "boolean"}, "at": {"$ref": "#/$defs/instant"},
            "content": {"enum": ["text", "notice", "emote", "image", "video", "audio", "voice",
                                 "file", "sticker", "location", "poll", "unsupported"]},
            "text": {"$ref": "#/$defs/textOrNull"},
            "editedAt": {"$ref": "#/$defs/instantOrNull"}, "editCount": {"$ref": "#/$defs/countOrNull"},
            "replyTo": {"$ref": "#/$defs/netIdOrNull"}, "threadRoot": {"$ref": "#/$defs/netIdOrNull"},
            "mentions": {"$ref": "#/$defs/netIdsOrNull"},
            "mentionsEveryone": {"$ref": "#/$defs/flagOrNull"}, "forwarded": {"$ref": "#/$defs/flagOrNull"},
            "expiresAt": {"$ref": "#/$defs/instantOrNull"},
            "attachments": {"type": ["array", "null"], "items": {
              "type": "object", "required": ["part", "content", "mediaState"], "additionalProperties": false,
              "properties": {
                "part": {"$ref": "#/$defs/netId"},
                "content": {"enum": ["image", "video", "audio", "voice", "file", "sticker"]},
                "mimeType": {"$ref": "#/$defs/textOrNull"}, "fileName": {"$ref": "#/$defs/textOrNull"},
                "size": {"$ref": "#/$defs/countOrNull"}, "width": {"$ref": "#/$defs/countOrNull"},
                "height": {"$ref": "#/$defs/countOrNull"}, "durationMs": {"$ref": "#/$defs/countOrNull"},
                "caption": {"$ref": "#/$defs/textOrNull"}, "file": {"$ref": "#/$defs/mediaOrNull"},
                "mediaState": {"enum": ["metadata", "stored", "too_large", "failed", "expired"]}
              }
            }},
            "action": {"$ref": "#/$defs/netIdOrNull"}
          }
        }
      }
    },
    "message.delete": {
      "type": "object", "additionalProperties": false,
      "required": ["seq", "at", "type", "network", "account", "chat", "message", "scope"],
      "properties": {
        "seq": {"$ref": "#/$defs/seq"}, "at": {"$ref": "#/$defs/instant"},
        "type": {"const": "message.delete"},
        "network": {"$ref": "#/$defs/netId"}, "account": {"$ref": "#/$defs/netId"},
        "chat": {"$ref": "#/$defs/netId"}, "message": {"$ref": "#/$defs/netId"},
        "scope": {"enum": ["everyone", "owner"]}, "by": {"$ref": "#/$defs/netIdOrNull"}
      }
    },
    "message.reactions": {
      "type": "object", "additionalProperties": false,
      "required": ["seq", "at", "type", "network", "account", "chat", "message", "reactions"],
      "properties": {
        "seq": {"$ref": "#/$defs/seq"}, "at": {"$ref": "#/$defs/instant"},
        "type": {"const": "message.reactions"},
        "network": {"$ref": "#/$defs/netId"}, "account": {"$ref": "#/$defs/netId"},
        "chat": {"$ref": "#/$defs/netId"}, "message": {"$ref": "#/$defs/netId"},
        "reactions": {"type": "array", "items": {
          "type": "object", "required": ["key", "user"], "additionalProperties": false,
          "properties": {
            "key": {"type": "string", "minLength": 1}, "user": {"$ref": "#/$defs/netId"},
            "at": {"$ref": "#/$defs/instantOrNull"}
          }
        }}
      }
    },
    "message.status": {
      "type": "object", "additionalProperties": false,
      "required": ["seq", "at", "type", "network", "account", "chat", "message", "status"],
      "properties": {
        "seq": {"$ref": "#/$defs/seq"}, "at": {"$ref": "#/$defs/instant"},
        "type": {"const": "message.status"},
        "network": {"$ref": "#/$defs/netId"}, "account": {"$ref": "#/$defs/netId"},
        "chat": {"$ref": "#/$defs/netId"}, "message": {"$ref": "#/$defs/netId"},
        "status": {"enum": ["sent", "delivered", "read", "failed"]}
      }
    },
    "action.claimed": {
      "type": "object", "additionalProperties": false,
      "required": ["seq", "at", "type", "action", "attempt", "version"],
      "properties": {
        "seq": {"$ref": "#/$defs/seq"}, "at": {"$ref": "#/$defs/instant"},
        "type": {"const": "action.claimed"},
        "action": {"$ref": "#/$defs/netId"}, "attempt": {"$ref": "#/$defs/netId"},
        "version": {"type": "integer", "minimum": 1}
      }
    },
    "action.result": {
      "type": "object", "additionalProperties": false,
      "required": ["seq", "at", "type", "action", "attempt", "outcome"],
      "properties": {
        "seq": {"$ref": "#/$defs/seq"}, "at": {"$ref": "#/$defs/instant"},
        "type": {"const": "action.result"},
        "action": {"$ref": "#/$defs/netId"}, "attempt": {"$ref": "#/$defs/netId"},
        "outcome": {"enum": ["done", "failed", "uncertain", "expired", "cancelled"]},
        "message": {"$ref": "#/$defs/netIdOrNull"},
        "error": {
          "type": ["object", "null"], "required": ["code"], "additionalProperties": false,
          "properties": {
            "code": {"enum": ["unsupported", "not_found", "rate_limited", "blocked", "rejected",
                              "network", "expired", "cancelled", "invalid"]},
            "message": {"type": "string"}
          }
        }
      }
    },
    "backfill.progress": {
      "type": "object", "additionalProperties": false,
      "required": ["seq", "at", "type", "network", "account", "complete"],
      "properties": {
        "seq": {"$ref": "#/$defs/seq"}, "at": {"$ref": "#/$defs/instant"},
        "type": {"const": "backfill.progress"},
        "network": {"$ref": "#/$defs/netId"}, "account": {"$ref": "#/$defs/netId"},
        "chat": {"$ref": "#/$defs/netIdOrNull"}, "oldestAt": {"$ref": "#/$defs/instantOrNull"},
        "complete": {"type": "boolean"}, "pendingChats": {"$ref": "#/$defs/countOrNull"}
      }
    }
  }
}
```

## Events

Each event names its account by `network` and `account`, but for
`instance.hello` and the two action reports. An event needs the rows it is
about: only `account.upsert` creates or restores an account, and only
`chat.upsert` a chat, so an event for an account or a chat the mirror does
not hold is skipped and counted. That is what keeps events in flight from
bringing back an account you deleted, or a chat the network deleted.

| Event | What it writes |
| --- | --- |
| `instance.hello` | the instance's `instanceName`, `version` and `networks` |
| `account.upsert` | the account, created or restored, pointed at this instance; `remoteName`, `handle`, `phoneNumber`, and `self` as its `user` |
| `account.state` | the whole connection state: `connectionState`, `connectionMessage` and `warnings`, a message or warnings the event leaves out cleared; and the sync trait (below) |
| `account.removed` | the bridge signed out and forgot the sign-in: `connectionState` reads logged out, `instance` is cleared, and the chats and messages stay |
| `user.upsert` | the person |
| `chat.upsert` | the chat, created or restored; members by the rule below; `lastActivityAt` never moves back |
| `chat.delete` | the chat tombstoned; its people, messages and files go when the engine collects it |
| `chat.read` | the chat's `readUpTo`, your own read marker from any device |
| `message.upsert` | the message, its attachments, and the action it echoes (below) |
| `message.delete` | `deleted`, and `deletedAt` from the event's `at`, by the rule below |
| `message.reactions` | the message's `reactions`, replaced by the whole set the event carries |
| `message.status` | `sendStatus` of one of your messages |
| `action.claimed`, `action.result` | the action's outcome (below) |
| `backfill.progress` | for the account (an event naming a chat changes nothing): `syncStreams.history` and, until it is complete, `syncProgress`; complete answers a pending `syncRequestedAt` |

**The sync trait.** `connected` is `syncState: ok`, `connecting` is
`running`, `transient_disconnect` is `throttled`, and `bad_credentials`,
`logged_out`, `banned` and `unknown_error` are `erroring`, with the
network's message (or a line saying what to do, where it gave none) in
`syncError` and the event's time in `syncErrorAt`. `syncMessage` is the
first warning's message, else the network's. `lastSyncedAt` moves when a
batch applies an event of the account, at most every five minutes, and the
instance's `lastSeenAt` the same.

**Stored spellings.** An enum value is one lowercase word, so the
underscore a wire value carries is dropped where it is stored:
`transient_disconnect` is stored as `transientdisconnect`, `phone_offline`
as `phoneoffline`, `too_large` as `toolarge`, `not_found` as `notfound`.
Each value's label is what the console shows.

## Ids

`chatevents` composes every row's id with
`host.ids.external(provider, account, external_id)`
([functions](../../../docs/functions.md#the-sdk)):

| Row | provider | account | external_id |
| --- | --- | --- | --- |
| `instance` | `chatbridge` | the instance id | `instance` |
| `account` | the network | the login id | `account` |
| `user` | the network | the login id | `user/` + user id |
| `chat` | the network | the login id | `chat/` + chat id |
| `message` | the network | the login id | `message/` + chat id + `/` + message id |
| `messageattachment` | the network | the login id | `attachment/` + chat id + `/` + message id + `/` + part id |

The instance is not part of an account's id, so a sign-in moved to a new
instance keeps every chat and message it had. A reference in an event
(`sender`, `replyTo`, `threadRoot`, `mentions`, `otherUser`, `parent`,
`members[].user`, `upTo`) names the network's id and resolves inside the
same account. `testdata/chatbridge/ids.json` holds a vector per row: the
event's fields, the three arguments, and the id.

WhatsApp has two ids for one person: a LID now, a phone-number id before.
Where the connector moves a person from one to the other, two `user` rows
exist, and the contact mapping (core's `chatbridgeuser`) joins them on the
number.

## Merge rules

- **Members.** `complete: true` replaces the chat's members; `complete:
  false` changes the listed ones only. `leave` and `ban` remove one, so the
  stored list holds who joined or is invited. A long list arrives split
  over several `chat.upsert` events of at most 1,000 members each, in one
  batch or more: the first `complete: true`, the rest `complete: false`.
- **Reactions.** `message.reactions` carries the whole set the bridge holds,
  so a redelivery converges; a duplicate entry is dropped.
- **Edits.** A `message.upsert` whose `editCount` is below the stored one
  (absent counts as 0) is an older copy, from a page of history, and is
  left out, so it cannot undo an edit.
- **Deletes.** A delete marks the message and keeps what it said: `scope:
  everyone` (withdrawn on the network) marks it `deleted: everyone`, `scope:
  owner` (you deleted it on another device) `deleted: owner`, and either sets
  `deletedAt`; its `text`, its attachments' `file` and their `caption` stay.
  The mirror is your archive, so a message withdrawn on the network is still
  here, marked deleted. A message deleted for everyone takes no later
  upsert, and keeps what it said before the delete; one deleted for you
  still takes its edits, and a later delete for everyone marks it so.
- **Chats.** `chat.delete` tombstones the chat. A later `chat.upsert` brings
  it back: with what it held, messages included, while the engine has not
  collected it yet, and fresh once it has.
- **Firsts.** `backfilled` is set when the message is first written, from
  the event's `backfill`. `sourceSeq` on a chat and a message is the `seq`
  of the event that last changed it; a duplicate changes neither.
- **Unknown targets.** An event about a message the mirror does not hold
  (`message.delete`, `message.reactions`, `message.status`) is skipped and
  counted, as is one for an account or chat it does not hold. A `sender`,
  `replyTo` or `readUpTo` naming a row not written yet is written anyway.
- **Disappearing messages.** `expiresAt` is recorded; nothing acts on it.

## Actions

A `chataction` is a request you, the console or an agent write, and the
bridge carries out once
([0252](../../../docs/decisions/0252-what-the-owner-or-an-agent-does-in-a-chat-is-a-chataction-the-bridge-carries-out-behind-a-shipped-gate.md)).

- **Request**: `action` (`send`, `react`, `unreact`, `edit`, `delete`,
  `markread`), `chat`, `target` (the message to react to, edit or delete,
  or to mark read up to; absent on `markread`, the chat's newest), `text`,
  `replyTo`, `reactionKey`, `forEveryone`, `notBefore` (held until then)
  and `notAfter` (expires unsent after it; a `send` or an `edit` with none
  expires an hour after it was written, which the bridge applies).
- **Yours alone** (`writer: owner`, so no app, function or agent writes
  them): `allowNewChat`, which lets a first message reach someone who is not
  a contact on the network, and `cancelRequestedAt`.
- **Outcome** (`writer: connector`, the function's): `attempt`,
  `claimedVersion`, `result`, `errorCode`, `errorMessage`, and `state`.

`state` starts `queued` and moves `queued` to `sending`, `failed`,
`expired` or `cancelled`, and `sending` to `done`, `failed`, `uncertain` or
`expired`; those five are final. It is a state machine, which takes no `writer:`, so
nothing at the write stops you moving it along a transition: the console
never does, every agent write is held by the gate, and `chatevents` moves
it from the bridge's reports alone. Cancel by stamping
`cancelRequestedAt`, never by moving the state.

1. **Claimed.** The bridge reads a queued action at version `v`, checks it,
   writes its claim to its own store before anything leaves, and reports
   `action.claimed` with a fresh `attempt` and `version: v`; the function
   moves the row to `sending` with `attempt` and `claimedVersion`. The
   bridge carries out the version it claimed, and an edit after the claim is
   not carried out. A claim of a row that is not queued is stale.
2. **Refused before a claim.** An action the bridge will not run (not an
   account it runs, a feature the chat lacks, past `notAfter`, cancelled
   first) is reported `action.result` with `failed`, `expired` or
   `cancelled` and an attempt id while the row is still queued and carries
   no attempt; the function moves it from `queued`.
3. **Result.** Otherwise a result applies only when its `attempt` is the
   row's, as `done` (with the network's `message`, which `result` names),
   `failed` (with `error.code` and `error.message`), `uncertain` (the
   bridge cannot tell whether it reached the network, and nothing resends
   it; a retry is a new action), or `expired` (a claim the bridge found had
   never left, after its window closed). A report for another attempt
   changes nothing and is counted.
4. **The echo.** Your message comes back as a `message.upsert` whose
   `message.action` is the action's id; the function points the action's
   `result` at the message row.

A claim and its result in one batch are written as two patches, one
transition each.

## Agent tools and the gate

Six functions are offered to agents (`offer: {agents: true}`), each writing
one `chataction` and answering its id: `chatbridge_send` (`chat`, `text`,
`replyTo`), `chatbridge_react` and `chatbridge_unreact` (`message`,
`reactionKey`), `chatbridge_edit` (`message`, `text`; your own messages
only), `chatbridge_delete` (`message`, `forEveryone`) and
`chatbridge_markread` (`chat`, `upTo`). An argument takes the record's id or
its path. An agent names a new record by a minted id alone
([0228](../../../docs/decisions/0228-a-person-an-app-or-an-agent-creates-a-record-under-a-minted-id.md)),
so the action's id is minted from the call's idempotency key: a retried call
names the action it already wrote. An agent that takes them needs
`providers.substrate.reamde.dev/chatbridge/chataction` in its own writes.

The shipped `chatbridge-agent-actions` policy gates every agent write of a
`chataction`, direct or through a tool, so each lands as a change request
for you to accept. Your own writes, the console's and the function's are not
held. None of the tools declares `confirmation: always`, which would hold
your own calls too and which nothing lifts. To let one agent react unasked,
write an allow that names the gate:

```yaml
kind: substrate.reamde.dev/core/recordpatchpolicy
metadata:
  id: moss-reacts-unasked
data:
  properties:
    selector:
      kinds:
        - providers.substrate.reamde.dev/chatbridge/chataction
      ops: [put]
      agents:
        - substrate.reamde.dev/assistant/moss
      functions:
        - providers.substrate.reamde.dev/chatbridge/react
    action: allow
    overrides: chatbridge-agent-actions
```

Naming the function lifts the gate for writes through that tool, never for
the agent's direct writes
([0108](../../../docs/decisions/0108-an-allow-outranks-a-gate-only-by-naming-it.md)).
The gate may also name a judge agent with criteria, which accepts what fits
its threshold. The policy is yours to edit once installed: an install or an
upgrade writes it only where no other hand wrote or deleted it.

## Accounts

An account appears when you sign in on the bridge's host (`chatbridge login
whatsapp`); you cannot create one, because its `network` and `loginId` are
the bridge's. Your settings on it, which the bridge reads and nothing else
writes:

| Setting | What it does | Absent |
| --- | --- | --- |
| `displayName` | the account's name here | `remoteName`, `handle` or `loginId` |
| `syncPaused` | the bridge mirrors nothing from it and sends nothing through it | not paused |
| `syncRequestedAt` | the bridge catches the account up now; the history it reports complete answers it | |
| `downloadMedia` | which files the bridge fetches: `none`, `small` (images and voice notes up to 5 MiB) or `all` | `small` |
| `sendsPerHour`, `sendsPerDay` | the most messages the bridge sends through it | 30 and 200 |
| `unlinkRequestedAt` | the bridge signs the account out and forgets it | |

**Unlinking keeps, disconnecting deletes.** Stamping `unlinkRequestedAt`
(the account page's Unlink this account) signs the account out of its
network: the bridge reports `account.removed`, and its chats and messages
stay. Deleting the account row (the Providers page's Disconnect) deletes
every person, chat and message it brought in: they cascade from it, and go
when the engine collects the row. The bridge reads a deleted account row as
an unlink too.

## What the tests are checked against

No fixture was captured from a real account. The three batches under
`internal/providertest/testdata/chatbridge` describe an invented owner, Ada,
whose WhatsApp numbers are in Ofcom's drama range, and each says what it
shows in its `_fixture.note`: linking an account (`batch_1_link`), the same
afternoon live (`batch_2_live`), and a page of history arriving after it
(`batch_3_history`). `chatbridge_test.go` applies them through the real
engine, and `chatbridge_contract_test.go` holds the id vectors to the id
algorithm and the schema above to the batches, without a database. They
prove the package agrees with this contract, not that a bridge sends it.
