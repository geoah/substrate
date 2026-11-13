# 01: Frameworks that put many messengers behind one interface

Research date: 2026-11-09. Scope: (a) mautrix-go bridgev2, (b) Beeper today, (c) other unified layers (Texts.com SDK, Matterbridge, Unipile and similar, messaging MCP servers), (d) a synthesis of the common connector shape.

**Method.** I read source, not only READMEs. I made shallow clones of `mautrix/go` at `45c2d43` (2026-11-08) and 20 bridge repositories, plus `TextsHQ/platform-sdk`, `beeper/cli` and two third-party bridgev2 harnesses (the Courier `localmatrix` package and Nook `apps/chatd`). I also read the primary docs (`developers.beeper.com/llms-full.txt`, the Desktop API changelog, the Beeper OpenAPI spec and SDK `api.md`, and Unipile's `llms.txt` and docs pages). Repository metadata (stars, push and release dates, licenses) comes from the GitHub API. **Verified** means I read it in source or in primary docs. Everything else is marked **unverified** inline.

---

## 0. Key findings

1. **bridgev2 is the de facto open "many messengers, one Go interface" layer.** It is the only maintained, open framework of that kind. It ships maintained connectors for WhatsApp, Signal, Telegram (now rewritten in Go), Messenger and Instagram, Google Messages, Google Voice, Slack, X, LinkedIn, Bluesky and Discord (the bridgev2 rewrite lives on a branch). Beeper adds LINE (MIT), GroupMe, Teams, Reddit and Tumblr. Beeper says these are "the same bridges we run with On-Device Connections and also on Beeper Cloud" ([developers.beeper.com/open-source](https://developers.beeper.com/open-source)).
2. **bridgev2 runs without a Matrix homeserver.** The whole Matrix side is two interfaces:
   - `MatrixConnector`: 21 methods, plus a required `BotIntent()`.
   - `MatrixAPI`: 22 methods.

   `mautrix/go` ships only the appservice implementation. Two independent projects published in Oct–Nov 2026 run the stock connectors against a local, store-backed implementation:
   - **Courier** by VitruvianSoftware: 11 networks, 965 LOC for the Matrix side.
   - **nook-chatd** by IAMichan/Nook: 8 networks; the whole sidecar, including its HTTP and WebSocket API, is about 2,700 LOC.

   Both are AGPL-3.0 and both use SQLite. Building one for substrate is roughly 1k LOC of glue, plus an API layer of 1–2k LOC.
3. **Licensing dictates the architecture.**
   - mautrix-go is MPL-2.0, so it is fine to embed.
   - Every `mautrix/*` connector is **AGPL-3.0**. `mautrix-signal` also needs Signal's AGPL `libsignal`, a Rust library linked through cgo.
   - `whatsmeow` is MPL-2.0 but depends on **GPL-3.0** `go.mau.fi/libsignal`.

   The practical answer is a **separate AGPL sidecar process** that talks HTTP to the substrate server, which keeps the server's own license free. Both reference projects do exactly this next to an MIT app.
4. **Beeper's local API changed a lot in 2026, but it is still proprietary.** New since January:
   - writes: edit, delete, reactions, mark read/unread, typing, chat PATCH (archive, pin, mute, rename);
   - a contacts list;
   - an **experimental WebSocket** (`/v1/ws`, with `chat.*` and `message.*` upserted/deleted events; its `seq` is per connection, so there is no resume);
   - **bridge and login-session endpoints** that mirror bridgev2's login steps;
   - a proprietary, headless **"Beeper Server"** that the official `beeper` CLI installs on Linux or macOS. The CLI can also forward WebSocket events as HMAC-signed webhooks.

   The limits remain: a Beeper account is required, nothing documents a cloud or server-side REST API, history is limited to what the app has indexed, and sign-ins are not exportable.
5. **The other unified layers do not fit substrate.**
   - **Texts.com SDK** (MIT) is dead: Beeper itself lists the integrations as "no longer maintained". Its `PlatformAPI` and StateSync event model are the visible ancestor of Beeper's API wire shape.
   - **Matterbridge** relays channels between networks, mostly with bot accounts. It does not fit.
   - **Unipile** is a well-designed SaaS for four messaging networks (LinkedIn, WhatsApp, Instagram, Telegram), priced from €49/month for 10 accounts. Data leaves the box.
   - **Messaging MCP servers** are per-network tool wrappers. They only help as a reference for agent tool design.
6. **Recommendation.**
   - Build an AGPL "chat sidecar" that embeds bridgev2 connectors with its own local `MatrixConnector`.
   - Give it a wire contract shaped like the Beeper Desktop API, so one substrate provider can talk to either Beeper or the sidecar.
   - Meanwhile, upgrade the existing Beeper provider to the WebSocket and the new write endpoints. It stays the zero-protocol-code fallback for iMessage and closed networks.

   The ranked list with its reasoning is in §7.

---

## 1. mautrix-go bridgev2

### 1.1 Facts

| | |
|---|---|
| Repo | [github.com/mautrix/go](https://github.com/mautrix/go), directory [`bridgev2/`](https://github.com/mautrix/go/tree/main/bridgev2) |
| Language / license | Go / **MPL-2.0**: file-level copyleft, so embedding in any program is fine and only modified MPL files must stay MPL |
| Maintainer | Tulir Asokan (mau.fi, Beeper). ★669 |
| Releases | v0.32.0 (2026-10-16), v0.31.0 (2026-09-16), v0.30.0 (2026-08-16): monthly, on the 16th. Last commit 2026-11-08. Still **pre-1.0**: bridgev2 interfaces change between minor releases ("expect a small fix with most bumps", [Nook ADR](https://github.com/IAMichan/Nook/blob/main/docs/ADR-CHAT-LINKS.md)) |
| Bridge versioning | `v0.YYMM.patch` (e.g. `v0.2610.0` = Oct 2026), released alongside mautrix-go |
| History | The first bridgev2 releases came in Aug 2024 (Signal v0.7.0, Slack v0.1.0; [mau.fi blog](https://mau.fi/blog/2024-08-mautrix-release/)). Written up in "[How to build a Twilio bridge](https://mau.fi/blog/megabridge-twilio/)" with [mautrix/twilio](https://github.com/mautrix/twilio) (MIT) as the example |

Architecture, from [`bridgev2/unorganized-docs/README.md`](https://github.com/mautrix/go/blob/main/bridgev2/unorganized-docs/README.md): there are three parts, the **network connectors**, the **central bridge module** (portals, ghosts, message DB, backfill, dedupe) and the **Matrix connectors**. The README says: *"Initially there will be two Matrix connectors: one for the standard setup that connects to a Matrix homeserver as an application service, and another for Beeper's local bridge system."* Only the first is public.

### 1.2 `NetworkConnector`

The entry point a bridge implements, from [`networkinterface.go`](https://github.com/mautrix/go/blob/main/bridgev2/networkinterface.go):

```go
type NetworkConnector interface {
    Init(*Bridge)
    Start(context.Context) error
    GetName() BridgeName                    // displayname, network_id, beeper_bridge_type, ...
    GetDBMetaTypes() database.MetaTypes     // connector-specific JSON metadata per table (portal, ghost, message, reaction, user_login)
    GetCapabilities() *NetworkGeneralCapabilities
    GetConfig() (example string, data any, upgrader configupgrade.Upgrader)
    LoadUserLogin(ctx context.Context, login *UserLogin) error   // fill login.Client with a NetworkAPI; no network IO
    GetLoginFlows() []LoginFlow
    CreateLogin(ctx context.Context, user *User, flowID string) (LoginProcess, error)
    GetBridgeInfoVersion() (info, capabilities int)
}
```

The connector may also implement these optional interfaces: `StoppableNetwork` (Stop), `DirectMediableNetwork` (SetUseDirectMedia, Download), `IdentifierValidatingNetwork` (ValidateUserID), `TransactionIDGeneratingNetwork`, `PortalBridgeInfoFillingNetwork`, `ConfigValidatingNetwork`, `MaxFileSizeingNetwork`, `NetworkResettingNetwork` (ResetHTTPTransport, ResetNetworkConnections), `SplitPortalMigratingNetwork` and `PushParsingNetwork` (ParsePushNotification).

### 1.3 `NetworkAPI`: one per logged-in account, plus optional capability interfaces

```go
type NetworkAPI interface {
    Connect(ctx context.Context)              // errors go out via UserLogin.BridgeState, not return values
    Disconnect()
    IsLoggedIn() bool
    LogoutRemote(ctx context.Context)
    IsThisUser(ctx context.Context, userID networkid.UserID) bool
    GetChatInfo(ctx context.Context, portal *Portal) (*ChatInfo, error)
    GetUserInfo(ctx context.Context, ghost *Ghost) (*UserInfo, error)
    GetCapabilities(ctx context.Context, portal *Portal) *event.RoomFeatures
    HandleMatrixMessage(ctx context.Context, msg *MatrixMessage) (*MatrixMessageResponse, error) // the one mandatory "send"
}
```

Every other capability is an **optional interface** that the core type-asserts:

| Capability | Interface | Method(s) |
|---|---|---|
| History | `BackfillingNetworkAPI` (+`…WithLimits`) | `FetchMessages(ctx, FetchMessagesParams) (*FetchMessagesResponse, error)`; `GetBackfillMaxBatchCount(...)` |
| Edit | `EditHandlingNetworkAPI` | `HandleMatrixEdit(ctx, *MatrixEdit) error` |
| React | `ReactionHandlingNetworkAPI` | `PreHandleMatrixReaction`, `HandleMatrixReaction`, `HandleMatrixReactionRemove` |
| Delete / unsend | `RedactionHandlingNetworkAPI` | `HandleMatrixMessageRemove(ctx, *MatrixMessageRemove) error` |
| Mark read | `ReadReceiptHandlingNetworkAPI` | `HandleMatrixReadReceipt(ctx, *MatrixReadReceipt) error` (`ExactMessage` may be nil; `ReadUpTo` timestamp) |
| Typing | `TypingHandlingNetworkAPI` | `HandleMatrixTyping(ctx, *MatrixTyping) error` (`IsTyping`, `Type` text/uploading/recording) |
| Mark unread, mute, tag | `MarkedUnreadHandlingNetworkAPI`, `MuteHandlingNetworkAPI`, `TagHandlingNetworkAPI` | `HandleMarkedUnread`, `HandleMute`, `HandleRoomTag` |
| Polls | `PollHandlingNetworkAPI`, `PollResultsFetchingNetworkAPI` | `HandleMatrixPollStart/Vote`, `FetchPollResults` |
| Chat meta writes | `RoomName/RoomAvatar/RoomTopic HandlingNetworkAPI`, `DisappearTimerChangingNetworkAPI` | `HandleMatrixRoomName/Avatar/Topic`, `HandleMatrixDisappearingTimer` |
| Delete chat, accept request, block | `DeleteChatHandlingNetworkAPI`, `MessageRequestAcceptingNetworkAPI`, `UserBlockingNetworkAPI` | `HandleMatrixDeleteChat`, `HandleMatrixAcceptMessageRequest`, `HandleMatrixBlockUser` |
| Membership, power levels | `MembershipHandlingNetworkAPI`, `PowerLevelHandlingNetworkAPI` | `HandleMatrixMembership`, `HandleMatrixPowerLevels` |
| Start DM / resolve | `IdentifierResolvingNetworkAPI` (+`GhostDMCreatingNetworkAPI`) | `ResolveIdentifier(ctx, identifier string, createChat bool) (*ResolveIdentifierResponse, error)`; `CreateChatWithGhost` |
| Contact roster | `ContactListingNetworkAPI` | `GetContactList(ctx) ([]*ResolveIdentifierResponse, error)` |
| User search | `UserSearchingNetworkAPI` | `SearchUsers(ctx, query string) ([]*ResolveIdentifierResponse, error)` |
| Create group | `GroupCreatingNetworkAPI` | `CreateGroup(ctx, *GroupCreateParams) (*CreateChatResponse, error)` |
| Own profile | `ProfileUpdatingNetworkAPI` | `UpdateProfile(ctx, *ProfileUpdateParams) error` |
| Viewing chat | `ChatViewingNetworkAPI` | `HandleMatrixViewingChat` (clients call it every 5 s) |
| Mobile / background | `BackgroundSyncingNetworkAPI`, `PushableNetworkAPI` | `ConnectBackground(ctx, *ConnectBackgroundParams)` (connect, drain, disconnect); `RegisterPushNotifications`, `GetPushConfigs` |
| Credential export | `CredentialExportingNetworkAPI` | `ExportCredentials(ctx) any` (used by provisioning "session transfers") |
| Media | `MediaResolvingNetworkAPI`, `StickerImportingNetworkAPI`, `ViewLimitedMediaHandlingNetworkAPI` | `ResolveMedia`, `DownloadImagePack/ListImagePacks`, `HandleMatrixViewLimitedMedia` |
| Own user ID | `NetworkAPIWithUserID` | `GetUserID() networkid.UserID` |

`Bridge.RunOnce(ctx, loginID, params)` together with `ConnectBackground` is the hook for "wake on push, sync, disconnect". That is how Beeper's mobile on-device bridges are meant to run. The use is inferred from the interface docs, not verified in Beeper's closed apps.

Identifiers (`bridgev2/networkid`) are typed strings: `PortalKey{ID PortalID; Receiver UserLoginID}` (an empty Receiver means a chat shared by every login, such as a WhatsApp group in split-portal mode), `UserID`, `UserLoginID`, `MessageID`, `PartID` (one network message may become several parts), `EmojiID`, `PaginationCursor`, `TransactionID`.

### 1.4 Inbound events: `RemoteEvent`

A connector holds the network connection, converts each network event into a `RemoteEvent` and calls `Bridge.QueueRemoteEvent(login *UserLogin, evt RemoteEvent) EventHandlingResult`. The core then does portal lookup or creation, dedupe, ordering, DB writes and the Matrix-side sends.

- **Types** (`RemoteEventType`): `Message`, `MessageUpsert`, `Edit`, `Reaction`, `ReactionRemove`, `ReactionSync`, `MessageRemove`, `ReadReceipt`, `DeliveryReceipt`, `MarkUnread`, `Typing`, `ChatInfoChange`, `ChatResync`, `ChatDelete`, `Backfill`.
- **Shape interfaces**:
  - base: `GetType`, `GetPortalKey`, `GetSender() EventSender{IsFromMe, SenderLogin, Sender}`, `AddLogContext`;
  - `RemoteMessage.ConvertMessage(ctx, portal, intent) (*ConvertedMessage, error)`;
  - `RemoteEdit.ConvertEdit(...)`;
  - `RemoteReaction.GetReactionEmoji() (string, EmojiID)`;
  - `RemoteReadReceipt.{GetLastReceiptTarget, GetReceiptTargets, GetReadUpTo}`;
  - `RemoteTyping.GetTimeout()`;
  - `RemoteChatResync{WithInfo, Backfill.CheckNeedsBackfill, BackfillBundle}`;
  - the modifiers `WithTimestamp`, `WithStreamOrder`, `WithTransactionID` (echo dedupe of our own sends), `ThatMayCreatePortal`, `WithUncertainPortalReceiver`.
- **Helpers** in [`bridgev2/simplevent`](https://github.com/mautrix/go/tree/main/bridgev2/simplevent): `Message[T]`, `Reaction`, `ReactionSync`, `MessageRemove`, `Receipt`, `MarkUnread`, `Typing`, `ChatResync`, `ChatInfoChange`, `ChatDelete`, `Backfill`, all on `EventMeta{Type, PortalKey, Sender, CreatePortal, Timestamp, StreamOrder, ...}`.
- **The content is Matrix-shaped.** `ConvertedMessage.Parts[].Content` is an `event.MessageEventContent`:
  - `msgtype` m.text / m.image / m.file / m.audio / m.video / m.location / m.notice;
  - `body` plus HTML `formatted_body`, file info and mentions;
  - `com.beeper.*` extensions such as link previews;
  - reply and thread relations.

  Whatever store you plug in receives Matrix-format content and has to map it to its own records.

### 1.5 History (backfill)

```go
type FetchMessagesParams struct {
    Portal *Portal; ThreadRoot networkid.MessageID
    Forward bool                        // new messages after AnchorMessage, or older before it
    AnchorMessage *database.Message     // oldest (or newest when Forward) bridged message; nil if none
    Cursor networkid.PaginationCursor   // from the previous backward call
    Count int; AllowSlowFetch bool; BundledData any; Task *database.BackfillTask
}
type FetchMessagesResponse struct {
    Messages []*BackfillMessage         // chronological; each carries Sender, ID, TxnID, Timestamp, StreamOrder, Reactions
    Cursor networkid.PaginationCursor; HasMore, Forward, MarkRead bool; ReadUpTo time.Time
    MoreRequiresSlowFetch, Pending, AggressiveDeduplication bool
    ApproxProgress float64; ApproxRemainingCount, ApproxTotalCount int; CompleteCallback func()
}
```

The core runs backfill in three ways ([`portalbackfill.go`](https://github.com/mautrix/go/blob/main/bridgev2/portalbackfill.go), [`backfillqueue.go`](https://github.com/mautrix/go/blob/main/bridgev2/backfillqueue.go), [`bridgeconfig/backfill.go`](https://github.com/mautrix/go/blob/main/bridgev2/bridgeconfig/backfill.go)):

- **Forward backfill** (`doForwardBackfill`):
  - runs when a portal is created, up to `max_initial_messages`;
  - runs again whenever the connector emits a `RemoteChatResync` whose `CheckNeedsBackfill(latestMessage)` says so, up to `max_catchup_messages`;
  - calls `FetchMessages(Forward: true, AnchorMessage: last bridged)`.

  **This is the "catch-up after downtime" path.** Connectors emit resyncs on reconnect for chats with newer activity than the last bridged message. Within the network, catch-up is the protocol library's job: WhatsApp's server queues offline messages for the linked device, Telegram uses `getDifference` via gotd, Signal's server queues messages for the linked device, and Meta uses Lightspeed sync cursors.
- **Backward backfill** runs through a persistent queue (`backfill_task` table: `batch_count`, `cursor`, `oldest_message_id`, `next_dispatch_min_ts`). It uses `queue.batch_size`, `batch_delay`, `max_batches` and per-room-type `max_batches_override`.
  - `MoreRequiresSlowFetch` parks a portal until a user asks.
  - `Pending` suspends the task for 24 h or more, until the connector delivers a `RemoteBackfill` event. WhatsApp uses this for on-demand history requests to the phone.
  - **`RunBackfillQueue` only runs when `Matrix.GetCapabilities().BatchSending` is true.** A standard homeserver cannot insert history before existing events ("Standard Matrix servers can't backwards backfill", says a comment in `networkinterface.go`). Only Beeper's server and a custom `MatrixConnector` can do it.
- **Manual backfill**: `Bridge.WakeupBackfillQueue(&ManualBackfill{Source, Portal, DoneCallback})` covers "load older" on demand. Nook's `POST /v1/chats/{chat}/paginate` uses it.

What each network allows (verified in the connector code and example configs):

| Network | Where history comes from | Depth |
|---|---|---|
| WhatsApp | The phone's history-sync blobs at link time, stored by the bridge; later `ON_DEMAND` history-sync requests to the phone ([`backfill.go`](https://github.com/mautrix/whatsapp/blob/main/pkg/connector/backfill.go)) | About 3 months by default. `request_full_sync` gives about 1 year, and `full_sync_config.days_limit` "seems to be around 3 years". Needs the phone online |
| Signal | `signalmeow`'s **BackupStore**: the message-history transfer offered when linking a device. `FetchMessages` reads `GetBackupChatItems` | Only what the phone transfers at link time (the user must opt in on the phone; the opt-in is general Signal behavior, not verified here). After that, only new messages |
| Telegram, Slack, Discord, LinkedIn, X, Meta | Server-side APIs | Effectively full history, subject to the network's rate limits |
| Google Messages | The phone, via the web-pairing protocol (`libgm`) | Phone-bound |

### 1.6 Contacts, identifiers, chat info, members

- `GetChatInfo` returns `ChatInfo{Name, Topic, Avatar, Members *ChatMemberList, JoinRule, Type, Disappear, ParentID, UserLocal{MutedUntil, Tag}, MessageRequest, UserBlocked, CanBackfill}`. A nil field means "unchanged".
- `ChatMemberList` has `{IsFull, TotalMemberCount, OtherUserID, Members []ChatMember, MemberMap, PowerLevels}`. Each `ChatMember` is `{EventSender, Membership, Nickname, PowerLevel, UserInfo, ...}`.
- `GetUserInfo` returns `UserInfo{Identifiers []string, Name, Avatar, IsBot, ExtraProfile}`. Identifiers are phone numbers, e-mail addresses and network usernames, stored in the `ghost.identifiers` jsonb column. **This is the roster.**
- `ResolveIdentifier(identifier, createChat)` returns `{Ghost, UserID, UserInfo, Chat *CreateChatResponse}`. `GetContactList()` and `SearchUsers(q)` return the same type.
- Capability flags: `ProvisioningCapabilities.ResolveIdentifier{CreateDM, LookupPhone, LookupEmail, LookupUsername, AnyPhone, ContactList, Search}` and `GroupCreation map[type]GroupTypeCapabilities`.

### 1.7 Login flows

```go
type LoginProcess interface { Start(ctx) (*LoginStep, error); Cancel() }
// extension interfaces, type-asserted per step type:
LoginProcessUserInput      { SubmitUserInput(ctx, map[string]string) (*LoginStep, error) }
LoginProcessCookies        { SubmitCookies(ctx, map[string]string) (*LoginStep, error) }
LoginProcessDisplayAndWait { Wait(ctx) (*LoginStep, error) }        // long-poll; may return a refreshed QR
LoginProcessWebAuthn       { SubmitWebAuthnResponse(ctx, json.RawMessage) (*LoginStep, error) }
LoginProcessStepCancel     { CancelStep(ctx) (*LoginStep, error) }
LoginProcessWithParams     { StartWithParams(ctx, LoginStartParams{Override *UserLogin, HTTP, WebSocketHTTP}) }  // re-login over an existing login

type LoginStep struct {
    Type   LoginStepType   // "user_input" | "cookies" | "display_and_wait" | "client_http" | "webauthn" | "complete"
    StepID string          // stable per flow+step, e.g. "fi.mau.telegram.login.code"
    TxnID  string          // random per step instance, for safe retries
    Instructions string
    UserInputParams      *LoginUserInputParams      // Fields[]{Type, ID, Name, Description, DefaultValue, Optional, Pattern, Min/MaxLength, Options}, Attachments, CanCancel
    CookiesParams        *LoginCookiesParams        // URL, UserAgent, InitialCookies, Fields[]{ID, Required, Sources[]{cookie|local_storage|request_header|request_body|special, Name, RequestURLRegex, CookieDomain}, Pattern}, ExtractJS, WaitForURLPattern, Hidden, JSRequired
    DisplayAndWaitParams *LoginDisplayAndWaitParams // Type qr|emoji|code|nothing, Data, ImageURL, CanCancel
    ClientHTTPParams     *LoginClientHTTPParams     // the bridge asks the *client* to make an HTTP request for it (browser fingerprint / anti-bot); has a websocket-tunnel twin
    WebAuthnParams       *LoginWebAuthnParams
    CompleteParams       *LoginCompleteParams       // UserLoginID
}
```

Source: [`login.go`](https://github.com/mautrix/go/blob/main/bridgev2/login.go), with a JSON Schema in [`login-step.schema.json`](https://github.com/mautrix/go/blob/main/bridgev2/unorganized-docs/login-step.schema.json). The input field types are `username`, `password`, `phone_number`, `email`, `2fa_code`, `token`, `url`, `domain`, `select` and `captcha_code`.

**How a client drives a login.** The flow is: `GetLoginFlows()` → `CreateLogin(user, flowID)` → `Start()` → a loop over `SubmitUserInput` / `SubmitCookies` / `Wait` that dispatches on `step.Type` → `complete`, at which point the `UserLogin` already exists and is connected.

The appservice connector exposes this as the **provisioning API**, [`bridgev2/matrix/provisioning.yaml`](https://github.com/mautrix/go/blob/main/bridgev2/matrix/provisioning.yaml):

- `GET /v3/whoami`, `GET /v3/login/flows`, `GET /v3/logins`
- `POST /v3/login/start/{flowID}`, `POST /v3/login/step/{processID}/{stepID}/{user_input|cookies|display_and_wait|webauthn|cancel}`, `POST /v3/login/client_http/...`, `GET /v3/login/client_websocket/...`, `POST /v3/login/cancel/{id}`, `POST /v3/logout/{loginID}`
- `GET /v3/contacts`, `POST /v3/search_users`, `GET /v3/resolve_identifier/{id}`, `POST /v3/create_dm/{id}`, `POST /v3/create_group/{type}`, `POST /v3/profile`, and poll-results, resolve-media and view-limited-media endpoints

Auth is either a shared-secret bearer token with `?user_id=@x:server`, or a Matrix access token or OpenID token ([`provisioning.go`](https://github.com/mautrix/go/blob/main/bridgev2/matrix/provisioning.go)). Note that the `ProvisioningAPI` type lives in the appservice package and is built on its `*Connector`. **A homeserver-less harness must re-implement the driver.** That is small: Nook's `login.go` is 301 LOC and switches on `step.Type` over the three extension interfaces.

Off-the-shelf UIs that drive this protocol:
- [mautrix/manager](https://github.com/mautrix/manager): Electron, AGPL, last push 2026-10-28; extracts cookies through a webview.
- Beeper Desktop/Server's `/v1/bridges/{id}/login-sessions`; see §2.

**Session state** is the connector's job and lives in the bridge DB. One part is `user_login.metadata`, a JSON shape the connector declares through `GetDBMetaTypes`; for example, Meta keeps cookies, push keys and native sessions there, and X keeps cookies, signing keys and sync cursors. The other part is the protocol library's own tables in the **same** database:
- WhatsApp: `whatsmeow_*`, via `sqlstore.NewWithWrappedDB`;
- Signal: the `signalmeow` store;
- Telegram: its store.

On restart, `LoadUserLogin`, then `Connect`, resume every login without user action. The bridge DB therefore holds device identity keys and session cookies. Courier forces its data directory to mode 0700 for this reason.

### 1.8 Capabilities and feature flags

- `NetworkGeneralCapabilities{DisappearingMessages, AggressiveUpdateInfo, ImplicitReadReceipts, OutgoingMessageTimeouts, Provisioning, NetworkIsImmediateParent, TypingRepeatInterval}`.
- Per chat, `NetworkAPI.GetCapabilities(portal)` returns [`event.RoomFeatures`](https://github.com/mautrix/go/blob/main/event/capabilities.go):
  - `Formatting`, `File` (per-msgtype MIME and size limits), `State`, `MemberActions`, `MaxTextLength`, `LocationMessage`;
  - `Poll*`, `Thread`, `Reply`;
  - `Edit` with `EditMaxCount` and `EditMaxAge`; `Delete` with `DeleteForMe`, `DeleteMaxAge` and `DeleteHide`;
  - `DisappearingTimer`; `Reaction` with `ReactionCount`, `AllowedReactions` and `CustomEmojiReactions`;
  - `ReadReceipts`, `TypingNotifications`, `Archive`, `MarkAsUnread`, `DeleteChat` (and `ForEveryone`), `BlockUser`, `ReportSpam`, `MessageRequest`.

  Support levels are `CapLevelRejected` (-2), `Dropped` (-1), `Unsupported` (0), `PartialSupport` (1) and `FullySupported` (2). **This per-chat capability record is exactly what an agent needs before it acts**: whether this chat can be edited, and until when.

### 1.9 Database

The schema is [`database/upgrades/00-latest.sql`](https://github.com/mautrix/go/blob/main/bridgev2/database/upgrades/00-latest.sql), revision v33. It runs on Postgres or SQLite through `go.mau.fi/util/dbutil`, and every table is keyed by `bridge_id`, so several bridges can share one database. The tables:

- `user`, `user_login` (`remote_name`, `remote_profile`, `metadata`)
- `portal` (`id`, `receiver`, `mxid`, `name`, `topic`, avatar, `room_type`, `disappear_*`, `cap_state`, `message_request`, `user_blocked`, `metadata`)
- `ghost` (`name`, avatar, `identifiers`, `is_bot`, `extra_profile`)
- `message` (`id`, `part_id`, `mxid`, `sender_id`, `timestamp`, `edit_count`, `thread_root_id`, `reply_to_id`, `send_txn_id`)
- `reaction`, `user_portal` (`last_read`), `disappearing_message`, `backfill_task`, `kv_store`, `public_media`, `encrypted_direct_media`

**`message.mxid` is `NOT NULL UNIQUE`, and portals and ghosts carry Matrix IDs.** A non-Matrix connector has to mint deterministic synthetic Matrix IDs, and the interface provides `GenerateDeterministicRoomID/EventID/ReactionEventID` for that. The core also runs its own migrations unless `Bridge.ExternallyManagedDB` is set.

### 1.10 Can the connectors run without a homeserver? Yes.

**The interfaces a non-Matrix implementation must provide**, from [`matrixinterface.go`](https://github.com/mautrix/go/blob/main/bridgev2/matrixinterface.go):

```go
type MatrixConnector interface {           // 21 methods
    Init(*Bridge); Start(ctx) error; PreStop(); Stop()
    GetCapabilities() *MatrixCapabilities   // {AutoJoinInvites, BatchSending, ArbitraryMemberChange, ExtraProfileMeta, ReplaceEntireProfile}
    ParseGhostMXID(id.UserID) (networkid.UserID, bool); FormatGhostMXID(networkid.UserID) id.UserID
    GhostIntent(networkid.UserID) MatrixAPI
    NewUserIntent(ctx, id.UserID, accessToken string) (MatrixAPI, string, error)   // "double puppet": the owner themselves
    SendBridgeStatus(ctx, *status.BridgeState) error                          // per-login connection state
    SendMessageStatus(ctx, *MessageStatus, *MessageStatusEventInfo)           // send success/failure, delivery receipts
    GenerateContentURI(ctx, networkid.MediaID) (id.ContentURIString, error); ParseContentURI(...)
    GetPowerLevels(ctx, roomID); GetMembers(ctx, roomID); GetMemberInfo(ctx, roomID, userID)
    BatchSend(ctx, roomID, *mautrix.ReqBeeperBatchSend, []*MatrixSendExtra) (*mautrix.RespBeeperBatchSend, error)
    GenerateDeterministicRoomID(networkid.PortalKey) id.RoomID
    GenerateDeterministicEventID(roomID, portalKey, messageID, partID) id.EventID
    GenerateReactionEventID(roomID, *database.Message, sender, emojiID) id.EventID
    ServerName() string
}
// NewBridge also type-asserts the unexported botIntentGetter: BotIntent() MatrixAPI   (bridge.go)

type MatrixAPI interface {                 // 22 methods; one per ghost / bot / owner
    GetMXID() id.UserID; IsDoublePuppet() bool
    SendMessage(ctx, roomID, event.Type, *event.Content, *MatrixSendExtra) (*mautrix.RespSendEvent, error)
    SendState(ctx, roomID, event.Type, stateKey string, *event.Content, ts time.Time) (*mautrix.RespSendEvent, error)
    MarkRead(ctx, roomID, eventID, ts); MarkUnread(ctx, roomID, bool); MarkTyping(ctx, roomID, TypingType, timeout)
    DownloadMedia(...); DownloadMediaToFile(...); UploadMedia(...); UploadMediaStream(...)
    SetDisplayName(...); SetAvatarURL(...); SetExtraProfileMeta(...); SetProfile(...)
    CreateRoom(ctx, *mautrix.ReqCreateRoom) (id.RoomID, error); DeleteRoom(...); EnsureJoined(...); EnsureInvited(...)
    TagRoom(...); MuteRoom(...); GetEvent(ctx, roomID, eventID) (*event.Event, error)
}
```

Optional extensions, all skippable: `MatrixConnectorWith{EncryptedDirectMedia, ArbitraryRoomState, Server, Provisioning, PublicMedia, NameDisambiguation, BridgeIdentifier, URLPreviews, PostRoomBridgeHandling, Analytics, BeeperStreams, Notifications, HTTPSettings}`, and on the API side `ThreadReadingMatrixAPI`, `StreamOrderReadingMatrixAPI`, `MarkAsDMMatrixAPI`, `MatrixAPIWithArbitraryRoomState` and `MatrixAPIWithRoomListing`. For scale, the official appservice implementation is 806 + 850 LOC (`matrix/connector.go`, `matrix/intent.go`), before crypto and provisioning.

**Outgoing actions** (send, edit, react, delete, read, typing) enter bridgev2 as **Matrix events**. The harness builds a synthetic `event.Event` and calls `Bridge.QueueMatrixEvent(ctx, evt)`, which dispatches to `HandleMatrixMessage/Edit/Reaction/MessageRemove`. Read receipts and typing go through the same queue as ephemeral `m.receipt` and `m.typing` events (`portal.go: handleMatrixReceipts/handleMatrixTyping`). Courier does exactly this. Its `actions` package stores the outgoing row as `pending` and then calls `h.Bridge.QueueMatrixEvent`, and the result comes back through `SendMessageStatus`.

**Who has built a non-Matrix implementation?**

| Project | What | Evidence |
|---|---|---|
| **Beeper's local bridge system** (closed) | On-device bridges inside Beeper Desktop and mobile. The Desktop API's chat IDs look like `!…:ba_<id>.local-whatsapp.localhost`, which suggests a per-account local pseudo-server ([WebSocket docs](https://developers.beeper.com/desktop-api/websocket-experimental)) | bridgev2 README; [Beeper docs](https://developers.beeper.com/bridges). The code is not public, and nothing like "hungryserv" or a standalone harness has been published by Beeper or mau.fi (**verified absence** in `mautrix/go`, which contains only `bridgev2/matrix`) |
| **Courier**, VitruvianSoftware ([`apps/desktop/courier/engine/internal/localmatrix`](https://github.com/VitruvianSoftware/vitruvian-core/tree/main/apps/desktop/courier/engine/internal/localmatrix)) | A macOS Beeper-like client. *"Package localmatrix lets stock mautrix bridges run with courier's local store in place of a Matrix server."* It imports 11 connectors: whatsapp, telegram, gmessages, slack, discord (megadiscord pseudo-version), facebook, instagram, twitter, linkedin, gvoice, bluesky. One `bridgev2.Bridge` and one SQLite file per network | `connector.go` 426 LOC + `intent.go` 539 LOC; files carry an `SPDX: AGPL-3.0-or-later` header and the subtree has an AGPL `LICENSE`. Release `courier-v0.3.13` on 2026-11-07, ★0. Pins mautrix-go v0.31.0 and bridges `v0.2609.x` |
| **nook-chatd**, IAMichan/Nook ([`apps/chatd`](https://github.com/IAMichan/Nook/tree/main/apps/chatd)) | A sidecar for a MIT macOS app: WhatsApp, Telegram, Signal, Messenger, Instagram, X, LinkedIn, Slack. A `LocalMatrix` writes to `chats.db`, and a local HTTP + `/v1/ws` API on 127.0.0.1 with a per-launch token **deliberately copies the Beeper Desktop API subset** (`/v1/bridges`, login sessions, `/v1/chats`, messages, send/edit/delete/react, read/unread, pin/mute/archive, assets, `POST /v1/chats/{chat}/paginate`) | 2,731 LOC total (`api.go` 987, `store.go` 596, `matrix.go` 474, `login.go` 301, …). Separately AGPL-3.0 licensed. Commits run 2026-10-23 → 2026-11-03, ★3. Implements `BatchSend` with `BatchSending: true`, so backward backfill works. Its [ADR](https://github.com/IAMichan/Nook/blob/main/docs/ADR-CHAT-LINKS.md) has a careful licensing and ban-risk analysis |
| nchat ([d99kris/nchat](https://github.com/d99kris/nchat), ★2.0k, MIT app) | A terminal client. It reuses **`signalmeow`** (mautrix-signal's protocol library) directly, **not** bridgev2: [`lib/sgchat/go/gosg.go`](https://github.com/d99kris/nchat/tree/master/lib/sgchat/go) is 3.4k LOC, and it vendors mautrix-signal with its AGPL `LICENSE` and a `THIRD_PARTY_LICENSES` file | Shows that reusing at the protocol-library level means writing your own bridge logic of several thousand lines per network |

**Pitfalls the reference implementations document** (Courier's comments and config):
- Build `bridgeconfig.BridgeConfig` in code. Without `PrivateChatPortalMeta`, DMs never get the contact's name.
- `SendReconnectWait` must be greater than 0, and auto-reconnect has to be configured.
- `Backfill.MaxInitialMessages` and `MaxCatchupMessages` must be greater than 0, or nothing is backfilled.
- Bridge states are sent from a goroutine that outlives `Stop()`, so guard writes after shutdown.
- Delivery receipts arrive as `MessageStatus.DeliveredTo`.
- A chat's login comes from the `m.bridge` state's `fi.mau.receiver`.
- State events (name, topic, avatar, member, `com.beeper.room_features`, space parent and child) arrive through `SendState` and must be mapped.
- Media flows through `UploadMedia` and `DownloadMedia`, so the harness needs a blob store.
- Several connectors build against Beeper forks: mautrix-meta needs a `github.com/imroc/req/v3` replace, mautrix-slack needs `beeper/slackgo` and discord needs `beeper/discordgo`. `replace` directives in a dependency's `go.mod` are **not** inherited, so the host `go.mod` must copy them.
- Signal needs the Rust `libsignal` static library built at the commit the connector pins, with cgo. Nook reports that this build is the hardest step, about 2 minutes the first time.

**Estimate for substrate.** Use the two working implementations as the template:
- `LocalMatrix` plus an intent: 700–1,000 LOC.
- Mapping Matrix-shaped events to substrate wire events, with a durable outbox: 400–800 LOC.
- An action API that injects synthetic events and tracks pending and status: 400–800 LOC.
- A login-session API: about 300 LOC.
- Per-network config and the build (libsignal, replaces): about 150 LOC plus CI.

That totals **2–4k LOC and roughly 2–4 engineer-weeks** to production quality for four to six networks. The ongoing cost is a monthly bump that often touches the harness, because bridgev2 is pre-1.0. The estimate is my own, from the two codebases. Nook's whole sidecar landed in about 11 days of commits.

### 1.11 Connector catalogue

The ✓ marks below mean "the connector implements the bridgev2 optional interface", found by grepping each `pkg/connector` for the method. They do not guarantee that every sub-feature works.

| Connector | Repo · latest release · ★ | In-repo protocol lib | Login flows (IDs) | Hist | Edit | React | Del | Read | Typing | Contacts | Search | Resolve | Group |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| WhatsApp | [mautrix/whatsapp](https://github.com/mautrix/whatsapp) · v0.2610.0 2026-10-16 · ★1.9k | uses `whatsmeow` (external, MPL-2.0) | `qr`, `phone` (pairing code) | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Signal | [mautrix/signal](https://github.com/mautrix/signal) · v0.2610.0 · ★681 | `signalmeow`, `libsignalgo` (cgo → Rust libsignal) | `qr` (linked device) | ✓¹ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | – | ✓ | ✓ |
| Telegram (Go rewrite) | [mautrix/telegram](https://github.com/mautrix/telegram) · v0.2610.0 · ★1.8k | `pkg/gotd` (fork of gotd/td, file license MIT) | `phone` (code, then 2FA password), `qr`, `bot`, `manual`; needs app `api_id`/`api_hash` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Messenger + Instagram | [mautrix/meta](https://github.com/mautrix/meta) · v0.2610.0 · ★469 | `messagix` (web Lightspeed/MQTT), `instameow` (IG app) | FB: `facebook`, `messenger` (cookies), `messenger-lite`, `messenger-lite-android`; IG: `instagram` (cookies), `instagram-password`, `android` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | – | ✓ | ✓ | ✓ |
| Google Messages | [mautrix/gmessages](https://github.com/mautrix/gmessages) · v0.2610.0 · ★199 | `libgm` | `google` (Google-account cookies, then an emoji match on the phone); QR "disabled as Google has discontinued it" | ✓ | – | ✓ | ✓² | ✓ | ✓ | ✓ | – | ✓ | ✓ |
| Slack | [mautrix/slack](https://github.com/mautrix/slack) · v0.2610.0 · ★107 | Beeper fork `slackgo` | `email` (code, captcha, workspace), `token` (browser cookies), `app` (bot tokens) | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | – | ✓ | ✓ | ✓ |
| X (Twitter) | [mautrix/twitter](https://github.com/mautrix/twitter) · v0.2609.0 2026-09-16 · ★127 | `twittermeow`, `juiceboxgo` (XChat E2EE key recovery) | `cookies`, `password` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | – | – | ✓ | – |
| LinkedIn | [mautrix/linkedin](https://github.com/mautrix/linkedin) · v0.2609.0 · ★39 | `linkedingo` | `cookies` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | – | ✓ | ✓ | ✓ |
| Discord | [mautrix/discord@megadiscord](https://github.com/mautrix/discord/tree/megadiscord) (bridgev2, active: last commit 2026-11-05). `main` is still the legacy bridge, v0.7.7 2026-08-16 · ★511 | `meowcord`, Beeper fork `discordgo` | `token` (browser), `qr` (remote auth), `machine` (credentials, captcha, MFA) | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | – |
| Google Voice | [mautrix/gvoice](https://github.com/mautrix/gvoice) · v0.2605.0 2026-05-16 · ★36 | `libgv` | `cookies` | ✓ | – | – | – | ✓ | – | – | – | ✓ | ✓ |
| Bluesky | [mautrix/bluesky](https://github.com/mautrix/bluesky) · v0.2510.0 2025-10-16 · ★45 ("not available in the [Beeper] app") | — | `password` | ✓ | – | – | – | ✓ | – | – | – | – | – |
| LINE | [beeper/line](https://github.com/beeper/line) · 1.2.0 2026-05-17 · ★63 · **MIT** | `line`, `ltsm`, `e2ee` | email, QR | ✓ | – | ✓ | ✓ | ✓ | – | ✓ | ✓ | ✓ | ✓ |
| GroupMe | [beeper/groupme](https://github.com/beeper/groupme) · AGPL · ★23 | `groupmeext` | web | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | – | ✓ | – |
| MS Teams | [beeper/teams](https://github.com/beeper/teams) · AGPL · no releases | — | device code | – | – | ✓ | – | ✓ | ✓ | – | – | – | – |
| Reddit chat | [beeper/reddit](https://github.com/beeper/reddit) · **no license file** | `redditchat` | username + password | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | – | ✓ | ✓ | ✓ |
| Tumblr DMs | [beeper/tumblr-dms](https://github.com/beeper/tumblr-dms) · v0.1.0 · AGPL | `tumblr` | — | ✓ | – | – | – | ✓ | – | – | ✓ | ✓ | – |
| IRC, Zulip, Twilio | [mautrix/irc](https://github.com/mautrix/irc), [mautrix/zulip](https://github.com/mautrix/zulip) (v0.2511.0), [mautrix/twilio](https://github.com/mautrix/twilio) (MIT, example) | — | — | partial | | | | | | | | | |
| *Not bridgev2* | [mautrix/imessage](https://github.com/mautrix/imessage) (legacy, mautrix-go v0.16, last push 2026-05-14); [mautrix/googlechat](https://github.com/mautrix/googlechat) (legacy Python, 2026-04-22); [beeper/imessage](https://github.com/beeper/imessage) (archived 2024). Beeper's macOS iMessage is now [beeper/platform-imessage](https://github.com/beeper/platform-imessage) (Swift, MIT; reads `chat.db` and automates Messages.app through accessibility APIs) | | | | | | | | | | | | |

¹ Signal history comes from the link-time history transfer only (§1.5). ² Google Messages deletion is "own device only".

The bridges that also implement `ConnectBackground`, for mobile on-device use, are whatsapp, signal, meta, telegram, gmessages, gvoice, teams and tumblr-dms.

Not public: the networks Beeper lists in-app beyond these (Viber, WeChat; LINE is public). The Beeper bridge bounty post ([2025-10-28](https://blog.beeper.com/2025/10/28/build-a-beeper-bridge/)) asked for *permissively licensed* bridgev2 connectors for WeChat, Viber, Snapchat, Teams, LINE and dating apps, and now says the bounty "is no longer active as we're building bridges in-house". Whether the Viber and WeChat connectors will be published is **unverified**. GitHub search finds no Beeper repositories for them.

**Can `pkg/connector` be imported as a library? Yes.** These are ordinary public Go packages under module paths such as `go.mau.fi/mautrix-whatsapp/pkg/connector`, and Courier and Nook import them, as `&whatsapp.WhatsAppConnector{}` and `&facebook.MetaConnector{}`. The caveats are the AGPL license, the copied `replace` directives, cgo for Signal, and the fact that each bridge pins a mautrix-go version, so all connectors in one binary must agree on a single mautrix-go version.

### 1.12 Licensing: embedding versus a separate process

| Component | License | Embedding (linking into one binary) | Running as a separate process |
|---|---|---|---|
| mautrix-go | MPL-2.0 | Fine in any program; modified MPL files stay MPL | n/a |
| mautrix-* connectors (whatsapp, signal, meta, telegram, slack, twitter, linkedin, gmessages, gvoice, discord, bluesky) | **AGPL-3.0** | The combined program is AGPL-3.0. §13: if you *modify* it and users interact with it over a network, you must offer the source. That is easy if the code is public anyway | The sidecar is AGPL. The substrate server is unaffected if the two are **separate programs** talking over HTTP/JSON with no shared Go packages (the FSF "aggregate" reading; keep the protocol generic, not intimate internal structures) |
| mautrix-signal [`LICENSE.exceptions`](https://github.com/mautrix/signal/blob/main/LICENSE.exceptions) | exceptions granted to **Beeper** and **Element** only | — | — |
| Signal `libsignal` (Rust, via cgo) | AGPL-3.0 (*"A license exception for libsignal must be acquired separately from Signal"*) | AGPL | AGPL sidecar |
| whatsmeow ([tulir/whatsmeow](https://github.com/tulir/whatsmeow), ★7.8k) | MPL-2.0, **but** it requires [`go.mau.fi/libsignal`](https://github.com/tulir/libsignal-protocol-go) (**GPL-3.0**) | A distributed binary is effectively GPL-3.0 | GPL sidecar |
| gotd fork inside mautrix-telegram | MIT (file license) | — | — |
| beeper/line | MIT | Permissive | — |

The substrate tree has **no LICENSE file yet** (checked in this worktree), so the license is undecided. Unless substrate itself chooses AGPL-3.0, the connectors must live in a separate binary. Following CLAUDE.md's "top level stays empty" rule, a separate Go module with its own `LICENSE` is the clean form, for example under `cmd/` with its own `go.mod`. Nook did the same in-tree as `apps/chatd` with its own AGPL `LICENSE`.

### 1.13 Risks

- **Terms of service and account bans differ per network** (the Nook ADR's assessment matches the networks' public terms):
  - Signal: low. It uses the official linked-device flow.
  - WhatsApp: it is the web multi-device protocol, and WhatsApp's terms discourage unofficial clients. Practical risk is low to medium for normal personal volume.
  - Messenger and Instagram: medium. Meta checkpoints accounts on new IPs and send bursts.
  - X: medium. LinkedIn: medium to high (its user agreement forbids automation).
  - Discord: **high**. User-account automation ("self-bots") is explicitly banned.
  - Telegram: low, with a user API app ID; FloodWait limits apply.
- **Churn.** bridgev2 is pre-1.0 with monthly releases, so expect harness changes on most bumps. Protocol breakages are fixed upstream, quickly, because Beeper runs the same code: mautrix/meta, for example, had 100+ commits in three months (per the Nook ADR).
- **Matrix-shaped internals.** Content is Matrix event content, IDs are synthetic MXIDs, and state arrives as state events. The harness is a translation layer and must track those internals.
- **Build complexity.** cgo plus Rust libsignal; Beeper forks via `replace`.

---

## 2. Beeper today

### 2.1 Beeper Desktop API

| | |
|---|---|
| Docs | [developers.beeper.com/desktop-api](https://developers.beeper.com/desktop-api), [changelog](https://developers.beeper.com/desktop-api/changelog), [API reference](https://developers.beeper.com/desktop-api-reference/), [OpenAPI repo](https://github.com/beeper/desktop-api-openapi) (MIT; info.version 0.2.0, last updated 2026-05-16, behind the SDK) |
| SDKs | Stainless-generated, all **MIT**: [TypeScript](https://github.com/beeper/desktop-api-js), [Go](https://github.com/beeper/desktop-api-go) (`github.com/beeper/desktop-api-go/v6`), [Python](https://github.com/beeper/desktop-api-python), PHP. v6.0.0 2026-09-25; `configured_endpoints: 60` |
| Base / auth | `http://localhost:23373`. Bearer token created in Settings → Integrations → Approved connections, **or OAuth 2.0 + PKCE** (RFC 8414 metadata at `/.well-known/oauth-authorization-server`). `POST /oauth/introspect`. Opt-in **Remote Access** (since 2025-09-26) binds 0.0.0.0 and honors `X-Forwarded-*` headers ([remote access](https://developers.beeper.com/desktop-api/advanced/remote-access)) |
| Status | Marketed as "public beta" and "experimental"; there were breaking changes in May 2026. The docs say: *"We recommend Beeper Desktop API for personal use only. Sending too many messages might result in account suspension by the networks."* *"Message history might be limited… prefer using On-Device Connections instead of Beeper Cloud."* |

**Endpoints** (union of the OpenAPI spec, SDK v6 [`api.md`](https://github.com/beeper/desktop-api-js/blob/main/api.md) and the changelog):

- **Info / app:** `GET /v1/info`, `POST /v1/focus`, `GET /v1/search`, `GET /v1/labels`. The app setup routes are `/v1/app/setup` plus `/email`, `/register`, `/response`, `/start`, and `/verification/recovery-key(/reset(/confirm))`, `/verifications(/{id}/accept|cancel|qr/confirm-scanned|sas/start|sas/confirm)`, `/verifications/qr/scan`. These cover headless sign-in and E2EE device verification.
- **Accounts and contacts:** `GET /v1/accounts`, `GET /v1/accounts/{accountID}`, `GET /v1/accounts/{accountID}/contacts` (search), `GET /v1/accounts/{accountID}/contacts/list` (cursor).
- **Bridges and logins** (SDK v6; not yet in the public changelog, which stops at v4.2.808 2026-05-06, so the shipping product version is **unverified**): `GET /v1/bridges`, `GET /v1/bridges/{bridgeID}`, `GET /v1/bridges/{bridgeID}/capabilities`, `GET /v1/bridges/{bridgeID}/login-flows`, `GET /v1/bridges/{bridgeID}/logins(/{loginID})`, `POST …/logins/{loginID}/remove`, `POST /v1/bridges/{bridgeID}/login-sessions`, `GET|DELETE …/login-sessions/{id}`, `POST …/login-sessions/{id}/steps/{stepID}`. `LoginSession.status` is `waiting_for_input | waiting_for_cookies | waiting_for_display | complete | cancelled | failed`, and `currentStep` is `user_input | cookies | display_and_wait | complete`. **This is bridgev2's login protocol, re-exposed.**
- **Chats:** `GET /v1/chats` (cursor), `GET /v1/chats/search`, `GET /v1/chats/{chatID}`, `POST /v1/chats` (create a group from `type` and `participantIDs`), `POST /v1/chats/start` (DM from `accountID` and `user`, optional `messageText`), `PATCH /v1/chats/{chatID}` (draft, title, description, avatar, archive, pin, mute, low-priority, disappearing timer), `POST …/archive`, `POST …/read`, `POST …/unread`, `POST …/notify-anyway`, `POST|DELETE …/reminders`, and `POST …/typing` with `{state: typing|paused}`. The typing route is used by the official CLI but **missing from the public OpenAPI**.
- **Messages:** `GET /v1/chats/{chatID}/messages` (`cursor` plus `direction=before|after`, returning `{items, hasMore, oldestCursor, newestCursor}`), `POST /v1/chats/{chatID}/messages` (`text`, `replyToMessageID`, `attachment{uploadID,…}`), `GET|PUT|DELETE /v1/chats/{chatID}/messages/{messageID}` (DELETE takes `forEveryone`), `POST /v1/chats/{chatID}/messages/{messageID}/reactions`, `DELETE …/reactions/{reactionKey}`, and `GET /v1/messages/search` (`accountIDs`, `chatIDs`, `chatType`, `dateAfter`, `dateBefore`, `mediaTypes`, `sender`, `query`, …).
- **Assets:** `POST /v1/assets/upload` (multipart), `POST /v1/assets/upload/base64`, `POST /v1/assets/download`, `GET /v1/assets/serve` (range requests; `mxc://`, `localmxc://`, `file://`).

**Realtime.** The **WebSocket at `ws://localhost:23373/v1/ws`** has been experimental since v4.2.557 (2026-02-13) ([docs](https://developers.beeper.com/desktop-api/websocket-experimental)):
- Clients send `{"type":"subscriptions.set","chatIDs":["*"]}`.
- The server answers with control frames `ready`, `subscriptions.updated` and `error`.
- Domain events are `chat.upserted`, `chat.deleted`, `message.upserted` (hydrated with full message `entries`; *"if the message is not yet retrievable, the event is skipped"*) and `message.deleted` (IDs only).
- `seq` is a *"monotonic per-connection sequence number"*, so **there is no resume token**. Catch-up after a disconnect is REST: page `GET /v1/chats` by activity and each chat's messages after `newestCursor`, or use `GET /v1/messages/search?dateAfter=`.
- Beeper documents no webhooks or SSE (MCP's SSE transport was removed in v4.1.294).

**MCP.** Built-in at `http://localhost:23373/v0/mcp` (Streamable HTTP, OAuth or bearer token; stdio through `npx @beeper/mcp-remote`), with a [Claude Desktop extension](https://github.com/beeper/desktop-api-mcp). Beeper publishes no list of tool names. Third-party skill docs list `search_chats`, `get_chat`, `list_messages` and `send_message` (text only) ([example](https://claudepluginhub.com/skills/plurigrid-asi-plugins-asi/beeper)); treat those names as **unverified**. The SDK's own MCP package is a Stainless "code tool" (the agent writes SDK calls) plus docs search.

**What changed in 2025–2026** (Desktop API changelog):
- **2025-07**: Beeper relaunched around **On-Device Connections**, folding in Texts.com ([AlternativeTo coverage](https://alternativeto.net/news/2025/7/beeper-relaunches-with-on-device-model-new-features-and-text-com-merged-services)).
- **2025-08**: first Desktop API and MCP. The changelog's oldest visible entry is v4.1.155 of 2025-08-31; search results cite v4.1.92 of 2025-08-13 (**unverified**).
- **2025-09-26**: Remote Access.
- **2025-10-16**: gRPC-style `/v0` replaced by REST `/v1`; MCP SSE dropped.
- **2026-01-23**: edit, uploads, attachments on send.
- **2026-01-27**: `assets/serve`.
- **2026-02-13**: `/v1/info`, WebSocket, reactions, contacts list, token introspection.
- **2026-05-06**: chat PATCH, read/unread, notify-anyway, `chats/start`, message get and delete, explicit cursors. **Breaking:** the reaction-removal URL changed, and `POST /v1/chats` no longer starts DMs.
- **2026-09-25** (SDK v6): `app.setup`, `bridges.logins`, labels. Standalone MCP over HTTP now requires a token.

### 2.2 Beeper Server and the `beeper` CLI

[beeper/cli](https://github.com/beeper/cli): TypeScript/Bun, v0.6.4 2026-10-31, ★63, **no license file**. It talks to *"Beeper Desktop on this machine, to a Beeper Server you self-host, or to either one running somewhere else."*

- **Beeper Server** is a proprietary headless build. The CLI downloads it from Beeper's update feed (`https://api.beeper.com/desktop/update-feed.json?bundleID=com.automattic.beeper.server&platform=linux|darwin|win32&arch=x64|arm64`; read in `packages/cli/src/lib/installations.ts`). It serves the same API on `127.0.0.1:23373`, signs in with an e-mailed code, and is then verified from another device or with the account's secret storage key (`beeper auth email start|response`, `beeper verify recovery-key`).
  - `beeper accounts add` then links networks, for example a WhatsApp QR code in the terminal.
  - `beeper targets enable server` installs a systemd user unit or a launchd agent.
  - It appears only in the CLI's docs, not on developers.beeper.com, so treat it as early and **partially verified**.
- **`beeper watch`** reads the WebSocket and can forward every event to `--webhook URL`, signed with `X-Beeper-Signature: sha256=<hmac>`. Delivery is *"best-effort, fire-and-forget"*: the queue holds `--webhook-queue` events (default 64) and **drops** the rest (read in `packages/cli/src/commands/watch.ts`). It is close to substrate's Slack sidecar pattern, but lossy, so REST catch-up is still required.

### 2.3 Cloud versus on-device, bbctl, matrix-mirror

- **On-device versus Cloud** ([help: what goes to Beeper servers](https://help.beeper.com/en_US/troubleshooting/beeper-on-device-vs-beeper-cloud-what-goes-to-beeper-servers), [using on-device connections](https://help.beeper.com/using-on-device-chat-network-connections-in-beeper)):
  - On-device bridges run inside the app; messages never touch Beeper servers; several accounts per network are allowed; connections are deleted on logout from that device.
  - Cloud bridges run on Beeper's servers and reach clients over Matrix, one account per network.
  - There is no toggle between the two: an account must be removed and reconnected.
- **[beeper/bridge-manager](https://github.com/beeper/bridge-manager) (`bbctl`)**: Go, Apache-2.0, ★1.4k, v0.15.0 2026-07-16, last commit 2026-11-02. It runs official or third-party bridges on your own machine, connected to **Beeper's homeserver ("hungryserv") only**: *"This tool can not be used with any other Matrix homeserver."* It needs a Beeper account and still runs in the foreground. Configure a bridgev2 bridge with `bbctl config --type bridgev2 sh-<name>` ([self-hosting docs](https://developers.beeper.com/bridges/self-hosting)). Self-hosted accounts are free.
- **[beeper/matrix-mirror](https://github.com/beeper/matrix-mirror)**: Go, Apache-2.0, pushed 2026-11-05. It logs into a Beeper (or any Matrix) account as its own device, verifies with the account's secret storage key, then syncs, decrypts and writes chats and messages to SQLite with full-text search, behind a read-only JSON CLI. *"Chats bridged through Beeper Cloud are Matrix rooms too, so one mirror covers them. Chats connected on-device never reach Beeper's servers, so they aren't mirrored."* It is a working pattern for a **server-side, durable, catch-up-capable** feed of **Cloud** bridges: Matrix `/sync` tokens never lose events.
- **A server-side or cloud API from Beeper:** none is documented for third parties. The only server-side paths are Matrix Client-Server over the user's own Beeper account (Cloud bridges only, with E2EE) and the headless Beeper Server. Android adds [Content Providers and Intents](https://developers.beeper.com/android/content-providers) (local).
- **Sign-ins cannot be exported** from Beeper: *"Beeper keeps them and its API does not hand them out"* (Nook ADR). Moving a network from Beeper to a self-run connector means linking it again.

### 2.4 Assessment of Beeper for substrate

The existing Beeper provider is worth keeping as the zero-protocol-code path. It covers iMessage on macOS through platform-imessage and networks that have no public connector (Viber, WeChat). It should also grow:
- move from polling every 15 minutes to WebSocket-driven sync: a small sidecar holds `/v1/ws` (or runs `beeper watch --webhook`), POSTs batches to a substrate webhook, and REST catch-up fills gaps by cursor;
- add the new writes: edit, delete, react, mark read, typing, chat PATCH.

It cannot be substrate's foundation:
- it is proprietary;
- it needs a Beeper account and a running Desktop (or Server);
- the WebSocket is experimental with no resume;
- history is limited to what Beeper has indexed;
- it carries the same per-network ban risk as any other client.

---

## 3. Other unified layers

### 3.1 Texts.com platform SDK

| | |
|---|---|
| Repo | [TextsHQ/platform-sdk](https://github.com/TextsHQ/platform-sdk): TypeScript, **MIT**, ★29, last push **2024-12-06**. The task's `texts-app/platform-sdk` does not exist |
| Status | Texts.com belongs to Automattic and was merged into Beeper. Beeper's open-source page lists the integrations (platform-whatsapp, telegram, twitter, matrix, discord, slack, linkedin, reddit, snapchat, meta, chatgpt) as **"Deprecated… no longer maintained"**. Several are archived; platform-whatsapp last bumped Baileys on 2025-10-17 |
| Integrations used | WhatsApp → Baileys; Telegram → GramJS (the `telegram` npm package); Meta → Lightspeed MQTT; Slack → `@slack/web-api` + RTM; Discord → raw gateway WebSocket; X, LinkedIn, Snapchat → reverse-engineered HTTP + EventSource |

The `PlatformAPI` interface, from [`src/PlatformAPI.ts`](https://github.com/TextsHQ/platform-sdk/blob/main/src/PlatformAPI.ts):

```ts
init(session, {accountID, dataDirPath, country}, prefs); dispose()
login?(creds: {cookieJarJSON, jsCodeResult, lastURL, code, custom, headers} | {username, password}): LoginResult
    // LoginResult: {type:'code_required', reason, title} | {type:'error'} | {type:'wait'} | {type:'success'}
logout?(); serializeSession?(): any            // the host persists the opaque blob and passes it back to init()
subscribeToEvents(onEvent: (ServerEvent[]) => void); onConnectionStateChange?(cb); getCurrentUser()
getThreads(folder, {cursor, direction:'after'|'before'}) → {items, hasMore, oldestCursor, newestCursor}
getMessages(threadID, pagination) → {items, hasMore}; getThread?, getMessage?, getUser?({userID}|{username}|{phoneNumber}|{email})
getThreadParticipants?; searchUsers?(q); searchThreads?(q); searchMessages?(q, pagination, {threadID, mediaType, sender})
sendMessage?(threadID, {text, filePath|fileBuffer, mimeType, mentionedUserIDs, ...}, {pendingMessageID, quotedMessageID})
editMessage?; deleteMessage?(threadID, messageID, forEveryone); forwardMessage?
addReaction?(threadID, messageID, key); removeReaction?; sendReadReceipt(threadID, messageID?, cursor?)
sendActivityIndicator(type, threadID); markAsUnread?; archiveThread?; pinThread?; notifyAnyway?
createThread?(userIDs, title?, messageText?); updateThread?; deleteThread?; add/removeParticipant?; changeParticipantRole?
getAsset?(opts, ...args) → URL | {url, headers} | Buffer | Readable; registerForPushNotifications?; reconnectRealtime?
```

Events, from [`ServerEvent.ts`](https://github.com/TextsHQ/platform-sdk/blob/main/src/ServerEvent.ts): `STATE_SYNC` with `objectName` (`thread | message | message_reaction | message_seen | participant | custom_emoji`), `mutationType` (`upsert | update | delete | delete-all`) and `objectIDs {threadID, messageID}`; `THREAD_MESSAGES_REFRESH`; `SESSION_UPDATED` (the host calls `serializeSession()`); `REFRESH_ACCOUNT`; `USER_ACTIVITY` (typing); `USER_PRESENCE_UPDATED`; `TOAST`.

Login modes are `'browser-extension' | 'browser' (cookie webview with JS extraction) | 'manual' (form) | 'custom'`, and capabilities are declared as an `Attribute` set (`CAN_MESSAGE_PHONE_NUMBER`, `NO_SUPPORT_TYPING_INDICATOR`, `SHARES_CONTACTS`, …).

**Assessment.** Use it as a design reference only. It is the cleanest small interface of the three, and **Beeper Desktop API's wire shapes descend from it**:
- the pagination objects match (`items`, `hasMore`, `oldestCursor`, `newestCursor`);
- the WebSocket docs say *"both upsert and update sync mutations are delivered as `<resource>.upserted` events"*;
- `Message` has the same `linkedMessageID`, `editedTimestamp`, `isDeleted` and `seen` fields.

Running the integrations themselves would mean Node.js and abandoned code.

### 3.2 Matterbridge

- [42wim/matterbridge](https://github.com/42wim/matterbridge): Go, Apache-2.0, ★7.6k. Last release v1.26.0 on **2023-01-29**; last commit 2024-08. Upstream is dormant.
- Community fork [matterbridge-org/matterbridge](https://github.com/matterbridge-org/matterbridge): its `LICENSE` file is **AGPL-3.0**, ★189, active (2026-10-24). It rewrote history to drop vendoring.
- **Model:** "gateways" that relay messages between *configured channels* across protocols, with spoofed `RemoteNickFormat` names. Most connectors use **bot** credentials (Telegram and Discord bot tokens, a Slack token). WhatsApp uses whatsmeow as a linked device on a "disposable SIM card", as its docs suggest, which is the user acting, but as a relay for listed groups. A REST "API" bridge exists for third-party integration (per its wiki; exact paths **not re-verified**).
- It has no per-account inbox mirroring, no history backfill, no read or typing support and no roster. **Not suitable** for "act as the user across all chats".

### 3.3 Unipile and similar commercial APIs

| | |
|---|---|
| What | A SaaS unified API ("Messaging, Email, Calendar"), French, SOC 2. Docs: [developer.unipile.com](https://developer.unipile.com/llms.txt); vendor summary: [unipile.com/llms.txt](https://www.unipile.com/llms.txt) |
| Messaging networks | **LinkedIn, WhatsApp, Instagram, Telegram** are live. *"Not released: X and Messenger… SMS is also announced and not released."* Messenger is listed as "available in v2" in some docs pages, so the vendor is inconsistent. Telegram runs on official APIs (MTProto client); LinkedIn, WhatsApp and Instagram are *"reverse engineering, always on an account the end user connects"* |
| Pricing | €49/month for up to 10 linked accounts, then €5 per account, falling to €3 at volume; no per-call fees ([pricing](https://www.unipile.com/pricing-api/)) |
| Resource model | `accounts` (list, connect, reconnect, delete, **resync**, solve or resend **checkpoint**, restart, proxy) · `chats` (list, start, get, PATCH action, messages, send, attendees, **"Synchronize a conversation from its beginning"**, delete) · `messages` (get, list all, forward, attachment, **reactions add/remove, edit, delete**) · `chat_attendees` (list, get, picture, chats or messages by attendee) · `users` (profiles, invitations, relations, followers; LinkedIn-heavy) · `webhooks` |
| Realtime | Webhooks per source (`messaging`, `email`, `calendar`, account status), sent as POSTs. Messaging events: `message_received` (also fires for messages the user sent elsewhere), `message_reaction`, `message_read`, `message_edited`, `message_deleted`, `message_delivered`. **5 retries** with growing delays if your endpoint does not return 200 within 30 s. Webhook auth is a custom header you set at creation. *"You will not receive webhooks for old messages upon account connection. However, in the case of reconnection, you will receive messages that arrived during the disconnection period."* For your own downtime beyond the retries, they recommend a cron over `GET /messages` with an overlapping window and dedupe by message ID ([new messages](https://developer.unipile.com/docs/new-messages-webhook.md), [webhooks](https://developer.unipile.com/docs/webhooks-2.md)) |
| Auth flows | A **hosted auth wizard**: a server generates a short-lived link (`POST /api/v1/hosted/accounts/link` with `type: create\|reconnect`, `providers`, `expiresOn`, `notify_url`, `name`) and the user completes QR, credentials or OAuth on Unipile's page ([hosted auth](https://developer.unipile.com/docs/hosted-auth.md)). **Custom auth:** `POST /api/v1/accounts {provider: "WHATSAPP"}` returns a QR string, or a WhatsApp **pairing code** with `pairing_phone_number`; Telegram uses a QR; others use credentials or cookies plus **checkpoints** (2FA, OTP, captcha) solved through the API. Completion is learned by polling account status or from the account-status webhook (`OK`, `CREDENTIALS`, `ERROR/STOPPED`, …) ([WhatsApp](https://developer.unipile.com/docs/whatsapp.md), [Telegram](https://developer.unipile.com/docs/telegram.md), [account status](https://developer.unipile.com/docs/account-lifecycle.md)) |
| Session state | Held by Unipile; you keep an `account_id`. Sessions expire (Instagram and LinkedIn after a year) and are flagged as `CREDENTIALS` |
| Risks | Message data and session secrets sit at a third party, which contradicts a self-hosted personal data server. There is a recurring per-account cost, only four messaging networks, and ban risk on the reverse-engineered networks (they publish [limits](https://developer.unipile.com/docs/provider-limits-and-restrictions.md)) |

**Similar products.** I found no other commercial API that offers *multi-network, personal-account* messaging behind one schema. The "Unipile alternatives" in search results are social-posting APIs (Ayrshare, Zernio and the like) or e-mail APIs (Nylas). Single-network WhatsApp gateways (WAHA, Evolution API, GOWA, Whapi, Green-API) exist but are out of scope for this report; this report did not verify their features.

### 3.4 MCP servers for messaging

These are per-network tool wrappers that each bundle a client. Use them as references for an agent action vocabulary, not as integration layers. Metadata is from the GitHub API on 2026-11-09.

| Server | Network · backend | ★ · last push · license | Tools / notes |
|---|---|---|---|
| [lharries/whatsapp-mcp](https://github.com/lharries/whatsapp-mcp) | WhatsApp · Go `whatsmeow` "bridge" writing SQLite + Python MCP | ★6.8k · **2025-07-13 (stale)** · MIT | `search_contacts`, `list_messages`, `list_chats`, `get_chat`, `get_direct_chat_by_contact`, `get_contact_chats`, `get_last_interaction`, `get_message_context`, `send_message`, `send_file`, `send_audio_message`, `download_media` (verified in the README) |
| [finereli/whatsapp-mcp](https://github.com/finereli/whatsapp-mcp) | WhatsApp · Baileys linked device | ★411 · 2026-11-06 · MIT | **Read-only** by design; history stored encrypted |
| [felipeadeildo/whatsapp-mcp](https://github.com/felipeadeildo/whatsapp-mcp) | WhatsApp | ★99 · 2026-11-07 · GPL-3.0 | read, search, send (README summary) |
| [chigwell/telegram-mcp](https://github.com/chigwell/telegram-mcp) | Telegram · Telethon user session (`TELEGRAM_API_ID/HASH`, session string) | ★2.0k · 2026-11-08 · Apache-2.0 | Broad tool set (chats, messages, contacts, media, folders, admin); explicit FloodWait handling |
| [chaindead/telegram-mcp](https://github.com/chaindead/telegram-mcp) | Telegram · MTProto | ★350 · 2026-05-28 · MIT | `tg_me`, `tg_dialogs`, `tg_read`, … |
| [googlarz/signal-mcp](https://github.com/googlarz/signal-mcp), [rymurr/signal-mcp](https://github.com/rymurr/signal-mcp) | Signal · `signal-cli` linked device | ★10 / ★26 · 2026-10-29 / 2025-04 · MIT / – | send and receive; googlarz claims "80+ tools", a searchable local history and a read-only mode |
| [wyattjoh/imessage-mcp](https://github.com/wyattjoh/imessage-mcp), [anipottsbuilds/imessage-mcp](https://github.com/anipottsbuilds/imessage-mcp) | iMessage · read-only `chat.db` | ★29 / ★31 · 2026 · MIT | read and search only |
| [SaseQ/discord-mcp](https://github.com/SaseQ/discord-mcp) | Discord · **bot token** | ★601 · 2026-10-23 · MIT | Guild and channel management, *not* the user account |
| Beeper Desktop MCP | everything Beeper links · local | proprietary | see §2.1 (tool names unverified) |

**Patterns.** WhatsApp and Signal servers are linked devices (whatsmeow, Baileys, signal-cli). Telegram servers use a user MTProto session. iMessage servers read `chat.db`. Discord servers use bots. None offers catch-up guarantees or a login UI beyond a terminal QR code. Several deliberately default to read-only.

---

## 4. "Real" Matrix as the unified layer (a variant of option A)

Instead of a homeserver-less harness, run the **stock bridge binaries** against a small homeserver and consume them with a Matrix client in the sidecar.

The homeserver options:
- [tuwunel](https://github.com/matrix-construct/tuwunel): Rust, Apache-2.0, ★2.7k, v1.9.4 2026-10-10.
- [continuwuity](https://github.com/continuwuity/continuwuity): Apache-2.0, ★1.2k.
- Synapse: AGPL, heavy.

The Matrix client can be mautrix-go's client, which is MPL. Logins are driven through each bridge's provisioning API.

Pros:
- **Process isolation:** the bridges stay unmodified separate programs, so AGPL is cleanly confined.
- Matrix `/sync` with `since` tokens is a **durable, gap-free catch-up feed**, the same property matrix-mirror relies on.
- Provisioning is already built in.

Cons:
- One more stateful service: homeserver, appservice registrations, ghost users. If you enable bridge E2EE you also carry Olm/Megolm, though you can leave it off on loopback.
- **No backward backfill**, because a standard homeserver cannot batch-send history. You only get the forward window set at portal creation (`max_initial_messages`, which may be large) plus catch-up.
- More resource use, and every bridge runs as its own process.

This is a sound fallback if AGPL code inside the substrate repository is unacceptable even as a separate module.

---

## 5. Synthesis: a common "messenger connector" shape

### 5.1 Concept mapping

| Concept | bridgev2 | Texts PlatformAPI | Beeper Desktop API | Unipile |
|---|---|---|---|---|
| Account | `UserLogin` (ID, RemoteName, RemoteProfile, BridgeState) | `accountID` + `getCurrentUser()` | `Account{accountID, bridge, network?, user}` | `account{id, type, status}` |
| Chat | `Portal` (`PortalKey{ID, Receiver}`, `ChatInfo`, `RoomType` dm/group/space) | `Thread{id, type, title, participants, isUnread, isArchived, isPinned, mutedUntil, lastReadMessageID}` | `Chat{id, accountID, type, title, participants, unreadCount, isArchived, isPinned, isMuted, isLowPriority, draft, reminder, capabilities, …}` | `chat{id, account_id, …}` |
| Message | `ConvertedMessage.Parts[]` (Matrix content) + `database.Message{ID, PartID, SenderID, Timestamp, ThreadRoot, ReplyTo, EditCount}` | `Message{id, timestamp, senderID, text, attachments, reactions, seen, linkedMessageID, editedTimestamp, isDeleted, isSender}` | `Message{id, chatID, senderID, text, type, attachments, reactions, linkedMessageID, editedTimestamp, isDeleted, sendStatus, seen, mentions, links}` | `message{id, chat_id, sender, text, attachments, reactions, …}` |
| Person | `Ghost{ID, Name, Avatar, Identifiers, IsBot}` + `ChatMember{Membership, PowerLevel, Nickname}` | `User/Participant{id, username, phoneNumber, email, fullName, imgURL, isSelf}` | `User{…}` + contacts | `attendee` / `user profile` |
| Live updates | `RemoteEvent` → `QueueRemoteEvent` | `subscribeToEvents(StateSync upsert/update/delete)` | WS `chat.*` / `message.*` `upserted/deleted` | webhooks `message_*`, account status |
| History | `FetchMessages{Forward, AnchorMessage, Cursor, Count}` | `getMessages(threadID, {cursor, direction})` | `GET …/messages?cursor&direction` | `GET chats/{id}/messages` (cursor) + sync-from-beginning |
| Roster | `GetContactList`, `SearchUsers`, `ResolveIdentifier` | `searchUsers`, `getUser` | `contacts/list`, `contacts`, `chats/start` | attendees, profiles, relations |
| Send / reply | `HandleMatrixMessage` (`ReplyTo`, `ThreadRoot`) | `sendMessage(…, {quotedMessageID})` | `POST messages {text, replyToMessageID, attachment}` | `POST chats/{id}/messages` |
| Edit / delete | `HandleMatrixEdit` / `HandleMatrixMessageRemove` | `editMessage` / `deleteMessage(forEveryone)` | `PUT` / `DELETE ?forEveryone` | `PATCH` / `DELETE` message |
| React | `HandleMatrixReaction(+Remove)` | `addReaction` / `removeReaction` | `POST` / `DELETE reactions/{key}` | add / remove reaction |
| Mark read | `HandleMatrixReadReceipt` | `sendReadReceipt` | `POST chats/{id}/read` | `PATCH chats/{id}` "action" (exact action name **unverified**) |
| Typing | `HandleMatrixTyping` | `sendActivityIndicator` | `POST chats/{id}/typing` | not found |
| Login | `LoginProcess` steps | `login(creds) → LoginResult`, `loginMode` | login-sessions + steps | hosted link or `POST accounts` + checkpoints |
| Session | connector-owned DB rows + protocol tables | `serializeSession()` blob, host-stored | Beeper-owned, not exportable | Unipile-owned (`account_id`) |
| Capabilities | `NetworkGeneralCapabilities`, per-chat `RoomFeatures`, `ProvisioningCapabilities` | `PlatformInfo.attributes` | `ChatCapabilities`, bridge capabilities | provider features table |

### 5.2 The minimal shared shape (what substrate should see)

The four systems converge on one shape. Substrate's contract should be the **intersection**, with a capability record so agents know what each chat allows. A sketch of the wire contract a sidecar exposes (JSON over HTTP; the Go types are illustrative):

```go
type Account struct { ID, Network, RemoteID, Name string; Handles []string /* tel:, mailto:, username */
    State string /* connecting|connected|transient_disconnect|bad_credentials|logged_out */; Caps NetworkCaps }
type Chat struct { ID, AccountID, Type /* dm|group|channel */, Title, Topic, AvatarURL string
    OtherUserID string /* DMs */; Members []Member; UnreadCount int; LastReadMessageID string
    Archived, Pinned, LowPriority, MessageRequest bool; MutedUntil *time.Time; DisappearSeconds int
    Caps ChatCaps /* from RoomFeatures: edit window, delete-for-everyone, reactions, max text, file limits, typing, receipts */ }
type Contact struct { ID, AccountID, Name, AvatarURL string; Handles []string; IsBot bool }
type Message struct { ID, ChatID, SenderID string; FromMe bool; At time.Time; SortKey int64 /* stream order */
    Text, HTML string; Attachments []Attachment; ReplyTo, ThreadRoot string; Mentions []string
    Reactions []Reaction /* {SenderID, Key, At} */; EditedAt *time.Time; Deleted bool; Status string /* pending|sent|delivered|read|failed */ }

// events, at-least-once, per-sidecar monotonic Seq, durable until acked
type Event struct { Seq uint64; AccountID, Type string /* account.state|chat.upserted|chat.deleted|message.upserted|message.deleted|reaction.upserted|reaction.deleted|receipt|typing */
    ChatID string; IDs []string; Entries []json.RawMessage; At time.Time }
// actions (idempotent by client txn ID)
POST /v1/chats/{id}/messages {text, html?, attachments?, replyTo?, threadRoot?, txnID}
PUT/DELETE /v1/chats/{id}/messages/{mid}[?forEveryone]  ·  POST/DELETE …/reactions/{key}
POST /v1/chats/{id}/read {upTo?}  ·  POST …/unread  ·  POST …/typing {state}  ·  PATCH /v1/chats/{id} {archived, pinned, mutedUntil, title}
POST /v1/chats/start {accountID, identifier}  ·  POST /v1/chats {accountID, type, participantIDs, title}
GET /v1/chats?cursor  ·  GET /v1/chats/{id}/messages?cursor&direction  ·  POST /v1/chats/{id}/paginate (deep history)
GET /v1/accounts/{id}/contacts?cursor  ·  GET /v1/events?after=<seq> (pull catch-up) + webhook push
```

Choosing **Beeper Desktop API's resource names and shapes** for this contract has three concrete payoffs:
1. Substrate already has a Beeper provider and kinds built on them.
2. A provider written once can target Beeper Desktop, Beeper Server or the substrate sidecar by changing a base URL. Nook proved this: its UI code ran against both.
3. Beeper's shapes are already the Texts-lineage normalization of exactly these concepts.

Add one thing Beeper lacks: a **durable `seq` with `GET /v1/events?after=`**. That turns "near-realtime" into "no lost messages". The sidecar owns its outbox, unlike Slack's 3-second ack window, so substrate can be down for hours and resume from its last acked `seq`.

Catch-up therefore has two layers:
1. **Network → sidecar.** The connector and bridgev2 handle it: the protocol's offline queue, forward catch-up backfill on resync, and backward backfill for depth.
2. **Sidecar → substrate.** Durable outbox, then batched webhook POST, ack by `seq`, and pull-on-resume. Reuse the batching design of substrate's `cmd/slack-socket` (see its README: 200 events / 512 KiB / 1 s), but ack from the outbox rather than from the network.

Idempotency keys come from stable network IDs: `(accountID, chatID, messageID, partID)` for messages, plus `editCount` for edits, which substrate's record writes can use directly.

### 5.3 Login flows: one state machine for every network

Every system reduces to: **list flows → start → (step → submit)* → complete**, with four step kinds:

| Step | Payload the UI renders | User action → submit | Examples |
|---|---|---|---|
| `user_input` | `fields[]{id, type: phone_number\|code(2fa_code)\|password\|email\|username\|token\|url\|domain\|select\|captcha_code, name, description, pattern, options, optional}` | form values `{id: value}` | Telegram phone → code → 2FA password; WhatsApp pairing-code flow (phone number); Bluesky user + app password; Slack e-mail → code → workspace |
| `display_and_wait` | `{type: qr\|code\|emoji\|nothing, data}` | nothing; the client long-polls `wait`, which may return a **refreshed QR** | WhatsApp, Signal, Telegram and Discord QR; WhatsApp pairing code; Google Messages emoji match |
| `cookies` | `{url, userAgent, fields[]{id, required, sources[cookie\|local_storage\|request_header\|request_body\|special]}, extractJS, waitForURLPattern}` | a webview opens `url`; when the fields are present, the client submits the extracted values. Without a webview, the user pastes cookies | Messenger, Instagram, X, LinkedIn, Slack token, Google Messages, Google Voice, Discord token |
| `complete` | `{accountID}` | — | all |

The advanced kinds (`client_http`, `webauthn`, captcha cookies) can be deferred.

How the other systems map onto the same machine:
- **Texts:** `loginMode: browser` is the cookie step; `manual` is the user-input step; `LoginResult.code_required` is the next user-input step; `wait` is display-and-wait.
- **Unipile:** the QR string or pairing code is display-and-wait, checkpoints are user-input steps, and the hosted wizard is the whole machine on the vendor's page.
- **Beeper:** `login-sessions` is the same machine verbatim.

**Substrate implication.** The console and the Apple apps can render the first three step kinds generically. For cookie flows, a native `WKWebView` that runs `extractJS` and watches `waitForURLPattern` gives the best experience; Courier and Nook both do this. In a plain browser console the realistic fallback is "paste the cookie values", which mautrix-manager also supports. QR codes need live refresh, so long-poll the session (bridgev2's `Wait`) or push step updates over the console's existing channel.

**Session persistence.** With bridgev2, the connector persists itself. The harness only provides a durable database: one SQLite file per network (Courier, Nook) or a dedicated Postgres schema (dbutil supports both). It must also back that database up and protect it, since it holds device keys and cookies. Logins resume on boot through `LoadUserLogin`, and re-login uses `LoginProcessWithParams` with an `Override`. If the network kills a session, bridgev2 emits a `BridgeState` of `BAD_CREDENTIALS` or `LOGGED_OUT`; substrate should surface it as the account's state and offer a re-login that runs the same flow. Texts' `serializeSession()` blob and Unipile's server-held sessions are the two alternatives. The first is fine for small JS integrations; the second is out of scope for self-hosting.

---

## 6. Comparison

| Option | Networks as the user | History | Realtime / catch-up | Contacts | Actions | Deployment | License impact on substrate | Maintenance | Main risks | Fit |
|---|---|---|---|---|---|---|---|---|---|---|
| **A. bridgev2 connectors in an own AGPL sidecar with a local MatrixConnector** | WA, Signal, TG, FB, IG, GMessages, GVoice, Slack, X, LinkedIn, Discord¹, Bluesky, LINE, … | Best available: forward + backward queue (with your `BatchSend`) + on-demand | Persistent connections; network catch-up by connector; durable seq to substrate (yours to build) | ✓ (ghost identifiers, contact lists, resolve, search) | Everything in §1.3 | Go sidecar (cgo for Signal), SQLite or Postgres | None if separate process and module; the sidecar is AGPL | Monthly bumps, harness churn; protocols fixed upstream | bridgev2 API churn; Matrix-shaped internals; ToS/ban per network | **High** |
| B. Stock mautrix bridges + tuwunel + Matrix client | Same, as separate binaries | Forward-only window + catch-up (no batch send) | Matrix `/sync` tokens: durable | ✓ via provisioning API | ✓ via Matrix events | Homeserver + N bridge processes + client | None (pure process isolation) | Bridge upgrades = binary swaps | Ops weight; appservice/E2EE config; shallower history | Medium |
| C. Beeper Desktop API / Beeper Server (+ WS) | All Beeper networks incl. iMessage (macOS), WeChat, Viber | What Beeper indexed (on-device better) | Experimental WS, no resume; REST cursors to catch up | ✓ contacts list/search | Send, reply, edit, delete, react, read/unread, typing, chat PATCH | Proprietary app or headless server + Beeper account | None (HTTP); SDKs MIT | Zero protocol work | Vendor lock, beta API, account required, sessions not exportable | **Medium (keep as fallback)** |
| D. Beeper Cloud via Matrix (matrix-mirror pattern) | Beeper **Cloud** bridges only | Whatever Beeper's server holds | Matrix `/sync`: durable | Room members only | Sends via Matrix (bridged) | Matrix client + E2EE device | None (Apache tooling) | Low | Beeper is moving to on-device; one account per network | Low–Medium |
| E. Texts platform-* | WA (Baileys), TG (GramJS), Meta, Slack, Discord, X, LinkedIn, … | Per integration | Push via StateSync | partial | Rich | Node.js host to build | MIT | **Abandoned** | Dead code | Reference only |
| F. Matterbridge | Relay channels; bots + WA linked device | None | Live relay only | – | Send only | Daemon | Apache (orig) / AGPL (fork) | Fork active, upstream dormant | Wrong model | No |
| G. Unipile | LinkedIn, WA, IG, Telegram | Sync-from-beginning endpoint | Webhooks (5 retries) + cron catch-up | ✓ | Send, edit, delete, react, read via chat action | SaaS | None (API) | Vendor's | Data leaves the box; cost; 4 networks | Low (conflicts with self-hosting) |
| H. Messaging MCP servers | One network each | Varies | None / poll | Varies | Send-centric | Per-network processes | Mixed (MIT, GPL) | Hobby-grade, several stale | Inconsistent; no login UX | Reference only |

¹ Discord's bridgev2 is on the `megadiscord` branch, and user-account automation is against Discord's terms. Do not offer it by default.

## 7. Ranked recommendation

1. **Build the substrate chat sidecar on bridgev2 (option A).**
   - **Shape:** a separate Go module and binary under an **AGPL-3.0** license that embeds `mautrix/go` bridgev2 plus the needed `pkg/connector`s (WhatsApp, Signal, Telegram, Messenger, Instagram first; then Slack, LinkedIn, X, Google Messages). It has its own `LocalMatrix` that writes a local store and an outbox.
   - **Toward substrate:** it pushes batched events to a substrate webhook trigger, exactly like `slack-socket`, and serves a **Beeper-Desktop-API-shaped** REST surface (actions, history pagination, contacts, login sessions) that substrate's sandboxed functions call over the egress allowlist, as the Beeper bundle does today.
   - **Why first:**
     - It is the only option that is self-hosted, acts as the user, covers ten or more networks, and delivers history, realtime, contacts and every action.
     - Protocol maintenance stays upstream: the same code runs Beeper's own on-device and cloud bridges.
     - Feasibility is demonstrated twice in the last month (Courier, Nook).
     - Licensing stays contained if the boundary is a process and a generic JSON contract.
   - **Do these first:**
     - `BatchSending: true` with a `BatchSend` that writes history, so deep history works.
     - A durable `seq` outbox, so substrate downtime loses nothing.
     - Per-chat capability records from `RoomFeatures`, so agents can check before they act.
     - Generic login-step rendering in the console and Apple apps (QR, form, cookie webview).
2. **Keep and upgrade the Beeper provider (option C)** as the zero-protocol-code path and for what option A cannot cover: iMessage through Beeper on macOS, WeChat and Viber, and users who already live in Beeper.
   - Replace the 15-minute poll with a WebSocket subscription held by a tiny sidecar, or `beeper watch --webhook` with HMAC verification. Back it with REST cursor catch-up, because the WebSocket has no resume and the CLI forwarder drops events when full.
   - Add functions for edit, delete, react, read/unread and typing.
   - If 1 and 2 share the wire shape, one provider codebase serves both. Make the base URL and token config, and branch only on capability records.
3. **Option B (stock bridges + tuwunel + a Matrix client)** applies only if the project decides no AGPL code may live in the repository even as a separate module, or wants to run unmodified upstream binaries. It costs more ops and gives shallower history (no backward backfill), but its catch-up is the most robust (`/sync`).
4. **Unipile (option G)** is not recommended for a self-hosted personal data server: third-party custody of messages and sessions, a per-account price and only four networks. Copy its webhook-retry, checkpoint and hosted-link ideas.
5. **Texts SDK, Matterbridge and the MCP servers:** references only. Texts for the interface vocabulary and StateSync events; the MCP servers for agent tool naming (`list_chats`, `list_messages`, `get_message_context`, `send_message`, `send_file`, `download_media`) and for read-only defaults.

## 8. Unverified or open

- Beeper Server: licensing terms, Linux support in practice and feature parity with Desktop. It is documented only in the `beeper/cli` README and code.
- Whether `/v1/bridges/*` login sessions ship in Desktop, Server or both, and in which version (SDK v6 has them; the public changelog stops at 2026-05-06).
- Beeper MCP tool names, from third-party docs.
- Viber and WeChat connectors used by Beeper: none is public.
- Unipile v2's Messenger and X status: the vendor's own docs are inconsistent.
- Unipile's exact mark-read call: a "chat action" via PATCH.
- Matterbridge's REST API paths, not re-read.
- Signal history transfer requires the user to opt in on the phone; this is general Signal behavior and was not checked in signalmeow beyond `FetchMessages` reading the `BackupStore`.
- Ban-risk ratings are qualitative: from the Nook ADR and public terms, not measured.
