# WhatsApp and Signal, acting as the user: libraries, platform behaviour, options

Research for the substrate messenger-provider design. Written 2026-11-09.

**Scope.** WhatsApp, with the person's own account as a linked device of their
phone, and Signal, as a linked device. A sibling report covers mautrix bridgev2
as a framework. Here mautrix-whatsapp and mautrix-signal appear only as
network connectors and reference code.

**Method.** On 2026-11-09 I shallow-cloned the default branch of each main
project (whatsmeow, mautrix-whatsapp, mautrix-signal, signal-cli,
signal-cli-rest-api, presage, Baileys, GOWA, wuzapi, whatsapp-mcp) and read the
source. Stars, releases and licenses come from the GitHub API, and the
platforms' rules come from their own help pages and blog. Two kinds of claim are
marked: **(unverified)** where I found no source or official page for it, and
**(snippet)** where the only source was a search-engine extract of an official
page I could not load (faq.whatsapp.com renders its pages with JavaScript).

---

## 0. Conclusions in brief

- **WhatsApp: whatsmeow is the engine.** mautrix-whatsapp, GOWA, wuzapi,
  Evolution Go, WAHA's GOWS engine and most WhatsApp MCP servers are all built
  on [whatsmeow](https://github.com/tulir/whatsmeow). Embedding it directly in a
  Go sidecar fits our needs best. It covers:
  - pairing by QR code, phone-number code and (since 2026-06) passkey;
  - history sync: about 3 months by default, about 1 year with "full sync", and
    pages of 50 older messages fetched on demand from the phone;
  - app-state sync: the user's address-book names, and archived, pinned and
    muted chats;
  - LID-to-phone-number mapping;
  - realtime messages, with offline catch-up and an ack sent only after our
    handler returns;
  - every action on our list.
- **whatsmeow's licence is MPL-2.0, but it links a GPL-3.0 dependency.** That
  dependency is `go.mau.fi/libsignal`. So run the sidecar as a separate program
  with its own GPL-3.0 licence file, talking HTTP to the server.
- **Signal: two viable paths.**
  - **signalmeow**, the Go library inside mautrix-signal (AGPL-3.0; Go plus
    Rust libsignal through cgo). It is the only maintained library that
    receives Signal's link-time *history transfer*: all chats, plus 45 days of
    media. It acks a message only after the handler succeeds and keeps a buffer
    of decrypted events.
  - **signal-cli** (GPL-3.0, Java) run as a daemon, with JSON-RPC over HTTP
    plus SSE. It is mature, documented and the most used, and has an explicit
    command for every action. However, it has **no history transfer** and does
    not store the messages it receives.
- **Ruled out for Signal.** presage (Rust, AGPL) has no history transfer.
  signald is dead.
- **Decide the Signal library before the user links.** The transfer happens
  only once, at link time, and only to a client that asks for it.
- **Shared limits on both networks:**
  - **Phone activity.** WhatsApp logs out linked devices when the phone is
    unused for more than 14 days. Signal unlinks them if the phone has not come
    online within 30 days.
  - **Linked-device activity.** Signal unlinks a device inactive for 45 days.
    WhatsApp disconnects a companion inactive for about 30 days (snippet).
  - **Queued messages.** WhatsApp keeps undelivered messages for up to 30
    days, Signal for up to 45 days.
  - **Terms of service.** Both forbid unauthorized or automated access.
    WhatsApp states that linking an unofficial app can get an account banned.

---

## 1. Licensing: embedding versus a separate process

| Component | License | What it pulls in |
|---|---|---|
| whatsmeow | MPL-2.0 | imports `go.mau.fi/libsignal` = [tulir/libsignal-protocol-go](https://github.com/tulir/libsignal-protocol-go), **GPL-3.0** (a fork of RadicalApp/libsignal-protocol-go, GPL-3.0) |
| mautrix-whatsapp | AGPL-3.0, plus [exceptions](https://github.com/mautrix/whatsapp/blob/main/LICENSE.exceptions) for Beeper and Element only | whatsmeow |
| Baileys | MIT | npm `libsignal@6.0.0` = [WhiskeySockets/libsignal-node](https://github.com/WhiskeySockets/libsignal-node), **GPL-3.0** |
| whatsapp-web.js | Apache-2.0 | drives WhatsApp's proprietary web app inside Chromium |
| WAHA | Apache-2.0 | engines: whatsapp-web.js, WPPConnect, Baileys-based NOWEB, GOWS (whatsmeow; ships as a compiled binary) |
| Evolution API / Evolution Go | "Apache-2.0 with additional conditions": a closed system must show a notice that it uses Evolution, and must not remove the logo | Baileys or whatsmeow; Evolution Go has a license-activation heartbeat |
| GOWA, wuzapi | MIT | whatsmeow, so the GPL part is in their binaries too |
| whatsapp-rust | MIT, including its own Signal-protocol crate (`wacore-libsignal`, declared MIT; where it came from is not audited) | none GPL as declared |
| signal-cli | GPL-3.0 | patched libsignal-service-java, plus the libsignal-client native library (AGPL-3.0) |
| signal-cli-rest-api | MIT wrapper | its Docker image bundles signal-cli (GPL-3.0) |
| libsignal | AGPL-3.0; "Use outside of Signal is unsupported" | none |
| mautrix-signal (signalmeow, libsignalgo) | AGPL-3.0, plus exceptions for Beeper and Element. "A license exception for libsignal must be acquired separately from Signal." | libsignal (Rust FFI) |
| presage, libsignal-service-rs | AGPL-3.0 | libsignal |
| signald | GPL-3.0 | dead |

What this means for substrate. The repository has no `LICENSE` file yet
(checked 2026-11-09).

- **Linking into one binary** (a Go import, cgo or static linking) makes one
  combined work.
  - A GPL-3.0 component anywhere in it (whatsmeow brings one) requires the
    whole binary to be distributed under GPL-3.0-compatible terms, with source.
  - An AGPL-3.0 component (signalmeow, libsignal) adds the network clause: users
    who interact over a network with a modified version must be offered its
    source.
  - MPL-2.0 is file-level copyleft and compatible with the GPL, so whatsmeow's
    own licence is no obstacle. Its dependency is.
- **A separate process** (daemon, subprocess or container) that talks HTTP,
  JSON-RPC or a websocket is normally a separate program. The FSF calls this
  "mere aggregation"
  ([GPL FAQ](https://www.gnu.org/licenses/gpl-faq.html#MereAggregation)). The
  server's licence is then unaffected. Shipping those binaries in our images
  still means providing their source and licence notices.
- **The pattern to use.** Give each messenger sidecar its own directory or Go
  module with its own `LICENSE`: GPL-3.0-or-later for a whatsmeow sidecar,
  AGPL-3.0 for a signalmeow sidecar. Let it talk to `substrated` only over HTTP,
  exactly as `cmd/slack-socket` does today. The rest of the repository can then
  take any licence.
  - For a self-hosted personal server, publishing the sidecar's source in the
    public repository already satisfies the AGPL's network clause.
- This is not legal advice. Have it checked before the first release.

---

## 2. WhatsApp: platform behaviour (true of every unofficial option)

### 2.1 Linked devices and their limits

- **Device limit.** The phone is the primary device. Up to **4 linked devices**
  may be connected at once
  ([About linked devices](https://faq.whatsapp.com/378279804439436), snippet). A
  second phone can also be linked as a companion
  ([FAQ](https://faq.whatsapp.com/1046791737425017), snippet).
- **14-day phone rule.** "Your companion phones will be logged out if you don't
  use WhatsApp on your primary phone for over 14 days." Other pages repeat it:
  you need to use the primary phone every 14 days to keep linked devices
  connected (snippets of faq.whatsapp.com pages
  [378279804439436](https://faq.whatsapp.com/378279804439436/?cms_platform=android)
  and [1046791737425017](https://faq.whatsapp.com/1046791737425017/?cms_platform=android)).
- **30-day companion rule.** An older FAQ also says linked devices are
  disconnected after **30 days of inactivity**. Which wording is current is
  **(unverified)**, so design for both rules.
- **Logging out the phone.** Linked devices stay connected for 14 days after the
  user logs out of the primary phone
  ([FAQ](https://faq.whatsapp.com/1421067466005147/?cms_platform=android),
  snippet).
- **mautrix-whatsapp builds this in**
  ([phoneping.go](https://github.com/mautrix/whatsapp/blob/main/pkg/connector/phoneping.go)):
  - After 12 days without seeing the phone, it warns the user: "Your phone
    hasn't been seen in over 12 days. The bridge is currently connected, but
    will get disconnected if you don't open the app soon."
  - After 10 days, it sends a "hacky phone ping": an app-state key request sent
    to the user's own devices, so a phone that answers counts as seen.
- **One connection per session.** A second process using the same keys gets
  `events.StreamReplaced`.

### 2.2 Pairing (how the device authenticates as the user)

- **QR code.** In whatsmeow, call `GetQRChannel(ctx)` *before* `Connect()`.
  - It yields one code string after another: the first is valid for 60 s, each
    later one for 20 s. When they run out (about 160 s in total), whatsmeow
    closes the socket
    ([qrchan.go](https://github.com/tulir/whatsmeow/blob/main/qrchan.go),
    [pair-code.go](https://github.com/tulir/whatsmeow/blob/main/pair-code.go)).
  - The user scans it under WhatsApp → Linked devices → Link a device.
- **Phone-number pairing code.**
  `PairPhone(ctx, "+15551234567", showPushNotification, whatsmeow.PairClientChrome, "Chrome (Linux)")`
  returns an 8-character code. The user types it on the phone under "Link with
  phone number instead" ([FAQ](https://faq.whatsapp.com/1324084875126592)).
  - The display name must have the form `Browser (OS)`, using a common browser
    and OS. Otherwise the server returns 400.
  - Call it right after connecting: the same window of about 160 s applies.
- **Passkey (new).** whatsmeow added passkey pairing on 2026-06-30
  ([pair-passkey.go](https://github.com/tulir/whatsmeow/blob/main/pair-passkey.go)).
  The sequence is:
  1. `events.PairPasskeyRequest` arrives.
  2. Answer it with `SendPasskeyResponse(WebAuthnResponse)`.
  3. `events.PairPasskeyConfirmation{Code, SkipHandoffUX}` arrives; unless
     `SkipHandoffUX` is set, the user confirms the code matches their phone.
  4. Finish with `SendPasskeyConfirmation`.

  How often WhatsApp demands this of companions is **(unverified)**. A headless
  sidecar may have to relay a WebAuthn challenge to the user's browser.
- **What pairing leaves behind, and must be kept.** whatsmeow's store holds:
  - the device identity: noise key, identity key, signed prekey, registration
    id, and the ADV signed device identity;
  - Signal sessions, prekeys and sender keys;
  - app-state sync keys, versions and mutation MACs;
  - contacts, chat settings, message secrets, privacy tokens ("tctoken"), the
    LID map and the NCT salt;
  - optionally, plaintext buffers of decrypted and sent messages.
- **How it ends.** The session has no time-based expiry. It ends when:
  - the user unlinks it;
  - the 14-day phone rule or the companion-inactivity rule fires;
  - WhatsApp logs it out. This arrives as `events.LoggedOut`. Reasons include
    401 (logged out), 403 (main device gone; "LOCKED" in WhatsApp's web code)
    and 406 ("BANNED" in that code).

  Losing the store means pairing again as a new device, which also brings a new
  history sync.

### 2.3 History sync: what the phone sends

Types, from `waHistorySync.HistorySync_HistorySyncType` in
[whatsmeow's protos](https://github.com/tulir/whatsmeow/tree/main/proto/waHistorySync):

| Type | Content |
|---|---|
| `INITIAL_BOOTSTRAP` (0) | the first, small payload of recent chats and messages, which may be inline in the notification (`InlineInitialPayloadInE2EeMsg`) |
| `INITIAL_STATUS_V3` (1) | status (stories) messages |
| `FULL` (2) | the full-history chunks, sent when full sync is requested |
| `RECENT` (3) | the recent-history chunks |
| `PUSH_NAME` (4) | the contacts' self-chosen display names |
| `NON_BLOCKING_DATA` (5) | past group participants, call logs and other secondary data |
| `ON_DEMAND` (6) | the reply to a request for older history |

The notification-level enum adds two more values, `NO_HISTORY` (7) and
`MESSAGE_ACCESS_STATUS` (8).

**What a payload contains.** A `HistorySync` holds:

- `Conversations[]`;
- `Pushnames`, `GlobalSettings`, `PastParticipants`, `CallLogRecords`;
- `PhoneNumberToLidMappings`;
- `ChunkOrder` and `Progress`, among others.

Each `Conversation` carries:

- `ID`, `Messages[]` (each a `WebMessageInfo`), `Name`, `DisplayName`;
- `PnJID`, `LidJID`, `Username`;
- `Archived`, `Pinned`, `MuteEndTime`, `UnreadCount`, `EphemeralExpiration`;
- `EndOfHistoryTransferType`. Its values are
  `COMPLETE_BUT_MORE_MESSAGES_REMAIN_ON_PRIMARY`,
  `COMPLETE_AND_NO_MORE_MESSAGE_REMAIN_ON_PRIMARY`, and two on-demand variants.
  This tells you whether the phone still holds older messages.

**How it arrives** ([message.go](https://github.com/tulir/whatsmeow/blob/main/message.go)):

1. A protocol message carries a `HistorySyncNotification`: a direct path and a
   media key, or the inline payload.
2. whatsmeow downloads it, inflates it with zlib and unmarshals it.
3. It dispatches `events.HistorySync`.
4. Then it **deletes the blob from WhatsApp's server**.

So the app must persist the data before its handler returns. The safer setup:

- Set `ManualHistorySyncDownload = true`, optionally with
  `DisableManualHistorySyncReceipt`. The notification then reaches you as an
  `events.Message` whose `ProtocolMessage.HistorySyncNotification` is set, and
  you call `DownloadHistorySync(ctx, notif, true)` yourself.
- If a payload is lost, `SendHistorySyncServerErrorReceipt` asks the phone to
  upload it again.

**How much history.**

- **whatsmeow's defaults**
  ([clientpayload.go](https://github.com/tulir/whatsmeow/blob/main/store/clientpayload.go)):
  `RequireFullSync=false`, `PlatformType=UNKNOWN`, `Os="whatsmeow"`, and
  `HistorySyncConfig{StorageQuotaMb: 10240, InlineInitialPayloadInE2EeMsg: true, ThumbnailSyncDaysLimit: 60, FullSyncDaysLimit: nil, …}`.
- **mautrix-whatsapp's configuration documentation**
  ([example-config.yaml](https://github.com/mautrix/whatsapp/blob/main/pkg/connector/example-config.yaml)):
  - `request_full_sync` "bumps the size of history syncs from 3 months to 1
    year";
  - `full_sync_config.days_limit`: "The limit seems to be around 3 years, but
    using higher values doesn't break". `size_mb_limit` and `storage_quota_mb`
    sit beside it.
  - These go into `DeviceProps` during pairing
    ([connector.go](https://github.com/mautrix/whatsapp/blob/main/pkg/connector/connector.go)).
    **So they must be set before linking.** Changing them later means linking
    again.
- **Platform type also counts.** Baileys documents setting
  `syncFullHistory: true` *and* imitating a desktop browser
  (`Browsers.macOS('Desktop')`) "to receive more message history"
  ([README](https://github.com/WhiskeySockets/Baileys#receive-full-history)).
  mautrix maps its `browser_name` setting to `DeviceProps.PlatformType`.
- **WAHA confirms the same levers.** Its GOWS engine exposes them as
  `WAHA_GOWS_DEVICE_REQUIRE_FULL_SYNC`, `…_FULL_SYNC_DAYS_LIMIT`,
  `…_FULL_SYNC_SIZE_MB_LIMIT` and `…_STORAGE_QUOTA_MB`
  ([docs](https://waha.devlike.pro/docs/engines/gows/)).
- **The ceiling is the phone.** It can send only what it still holds locally
  **(unverified generalization)**.

**Older history on demand.**

- `cli.BuildHistorySyncRequest(&oldestKnownMsgInfo, 50)`, sent with
  `SendPeerMessage` (a peer message to the user's own devices), returns up to
  N messages *before* the anchor message as an `events.HistorySync` of type
  `ON_DEMAND`. whatsmeow's documentation: "The recommended number of messages to
  request at a time is 50."
- The phone must be online to answer.
- mautrix-whatsapp keeps one request in flight per chat, with a 1-hour timeout
  ([backfill.go](https://github.com/mautrix/whatsapp/blob/main/pkg/connector/backfill.go)).
- Baileys' equivalent is `fetchMessageHistory(50, oldestKey, oldestTs)`, at most
  50 per query.

**Messages that failed to decrypt.** `BuildUnavailableMessageRequest` asks the
phone to resend a message the companion could not decrypt (a "placeholder
resend"). It runs automatically when `AutomaticMessageRerequestFromPhone` is
set.

### 2.4 App-state sync: contacts, names, chat flags

**The data is grouped into patches**
([appstate/keys.go](https://github.com/tulir/whatsmeow/blob/main/appstate/keys.go)):

| Patch | Holds |
|---|---|
| `critical_block` | settings such as the user's push name and locale |
| `critical_unblock_low` | the contact list: the names in the user's own address book |
| `regular_low` | pin, archive, and the "unarchive chats" setting |
| `regular_high` | mute and starred messages |
| `regular` | protocol metadata about the patches themselves |

**Events whatsmeow emits:**

- `Contact{FirstName, FullName}`, `PushName`, `BusinessName`;
- `Pin`, `Archive`, `Mute`, `Star`;
- `MarkChatAsRead`, `ClearChat`, `DeleteChat`, `DeleteForMe`;
- `LabelEdit`, `LabelAssociationChat`, `LabelAssociationMessage`;
- `AppStateSyncComplete`.

**Read and write.**

- Read with `FetchAppState(ctx, name, fullSync, onlyIfNotSynced)`.
- Write with `SendAppState(ctx, patch)` and the patch builders: archive, pin,
  mute, mark read, and so on.
- The phone shares the decryption keys after pairing, in an
  `AppStateSyncKeyShare` message.

**When app state breaks.** mautrix-whatsapp ships recovery commands
(`BuildAppStateRecoveryRequest`, `BuildFatalAppStateExceptionNotification`).
An integration on raw whatsmeow needs the same.

### 2.5 Identities: phone-number JIDs versus LIDs (the 2025–2026 migration)

**JID servers.**

| Server | Meaning |
|---|---|
| `s.whatsapp.net` | a phone-number identity |
| `lid` | a hidden-user "LID" |
| `g.us` | a group |
| `broadcast` | status and broadcast lists |
| `newsletter`, `bot`, `msgr`, `hosted`, `hosted.lid` | channels, bots and other special cases |

**Direct messages now go to the LID.** When asked to send to a phone number,
whatsmeow's `SendMessage` **replaces the destination with the recipient's LID**
([send.go](https://github.com/tulir/whatsmeow/blob/main/send.go)):

- It looks the LID up in the LID map. If the map has none, it asks the server
  with `GetUserInfo`.
- If no LID exists at all, it returns `ErrRecipientNotOnWhatsApp`. This
  behaviour dates from 2026-10-26.
- The login payload sets `LidDbMigrated=true`, and the device row stores `lid`
  and `lid_migration_ts`.

**Where the LID-to-phone mappings come from:**

- `PhoneNumberToLidMappings` in history sync;
- `LidMigrationMappingSyncMessage` sent by the phone;
- usync, the user-lookup query: `IsOnWhatsApp` now asks with
  `addressing_mode=lid` and returns both `jid` (the LID) and `pn_jid`;
- each message's `MessageSource.SenderAlt`, `RecipientAlt` and `AddressingMode`
  fields.

**Where they are stored.** In the table `whatsmeow_lid_map(lid PK, pn UNIQUE)`,
read through `Store.LIDs.GetPNForLID`, `GetLIDForPN`, `GetManyLIDsForPNs` and
`Device.GetAltJID`. Since 2026-05, whatsmeow deletes old entries "when LID or
phone number changes", so treat both as attributes that can change.

**Not every LID resolves to a number.**

- Baileys users report historical `@lid` senders whose history payload carried
  no mapping ([Baileys discussion #2551](https://github.com/WhiskeySockets/Baileys/discussions/2551)).
- Evolution API's automations broke on LID-addressed events
  ([issue #1872](https://github.com/evolution-foundation/evolution-api/issues/1872)).

**Usernames.** WhatsApp opened username reservations on 2026-06-29 and is
rolling them out in phases
([BleepingComputer](https://bleepingcomputer.com/news/security/whatsapp-rolls-out-usernames-to-help-users-hide-their-phone-number),
[TechRepublic](https://www.techrepublic.com/article/news-whatsapp-usernames-phone-number-privacy/)).

- Someone who writes to the user for the first time may show only a LID and a
  username, and never a phone number.
- `Conversation` already has a `Username` field, and usync asks for a
  `username` node.

**For substrate:**

- Key a WhatsApp person on the **LID**.
- Store as attributes: the phone number when known, the push name, the
  address-book name (from app state), the verified business name, the username
  and the avatar.
- Whether a LID itself can change is **(unverified)**.

**Names and pictures.**

- **Names have three sources:**
  - the user's own address-book name: app state, `events.Contact`;
  - the contact's self-chosen push name: message metadata and the `PUSH_NAME`
    sync;
  - the verified business name.
- **Avatars.** `GetProfilePictureInfo(ctx, jid, params)` returns a URL and an
  id; the contact's privacy settings apply. Changes arrive as `events.Picture`.
- **Other details.** `GetUserInfo` returns the "about" status and the device
  list.
- **Groups.** Read them with `GetJoinedGroups` and `GetGroupInfo`.

### 2.6 Realtime updates and catch-up after downtime

- **The connection.** A persistent websocket, encrypted with the Noise
  protocol. whatsmeow sends keepalive pings every 20–30 s and reconnects
  automatically.
- **Catch-up after an outage.** While the companion is offline, WhatsApp queues
  messages, receipts and notifications for it. On reconnect whatsmeow emits:
  1. `events.OfflineSyncPreview{Total, Messages, Notifications, Receipts, AppDataChanges}`;
  2. the backlog itself;
  3. `events.OfflineSyncCompleted`.
- **How long the queue lasts.** WhatsApp's privacy policy: an undelivered
  message is kept encrypted for **up to 30 days**, then deleted
  ([privacy policy](https://www.whatsapp.com/legal/privacy-policy)).
- **Settings that prevent losing messages**
  ([client.go](https://github.com/tulir/whatsmeow/blob/main/client.go)):
  - `SynchronousAck`: "acks for messages will only be sent after all event
    handlers return".
  - `AddEventHandlerWithSuccessStatus`: the handler returns a success flag.
  - `EnableDecryptedEventBuffer`: keeps the decrypted plaintext, keyed by a hash
    of the ciphertext. A crash between decryption and handling then loses
    nothing, even though the encryption ratchet has already advanced.
  - `UseRetryMessageStore`: keeps sent messages so that retry receipts still
    work after a restart.
- **Beyond the queue's 30 days,** older messages can be fetched on demand from
  the phone (§2.3), provided the phone is online.

### 2.7 Acting as the user (whatsmeow API names)

- **Text:**
  `SendMessage(ctx, chat, &waE2E.Message{Conversation: proto.String("hi")})`
  returns `SendResponse{ID, Timestamp, ServerID…}`.
- **Replies and quotes:** an `ExtendedTextMessage{Text, ContextInfo{StanzaID, Participant, QuotedMessage}}`.
- **Media:** `Upload(ctx, bytes, whatsmeow.MediaImage)` returns
  `UploadResponse{URL, DirectPath, MediaKey, FileEncSHA256, FileSHA256, FileLength}`,
  which goes into an `ImageMessage`, `DocumentMessage`, `AudioMessage` (set
  `PTT` for a voice note) or `VideoMessage`. For large files, use `UploadReader`
  and `DownloadToFile`.
- **Reactions:** `BuildReaction(chat, sender, msgID, "👍")`; an empty string
  removes the reaction.
- **Edits:** `BuildEdit(chat, msgID, newContent)`. whatsmeow's constant
  `EditWindow = 20*time.Minute`; WhatsApp tells users 15 minutes
  ([TechCrunch](https://techcrunch.com/2023/05/22/whatsapp-now-lets-you-edit-messages-with-a-15-minute-time-limit)).
- **Delete for everyone:** `BuildRevoke(chat, sender, msgID)`.
  - Leave the sender empty for your own message; a group admin may revoke
    others' messages.
  - WhatsApp's time limit is about 2 days **(unverified)**.
- **Delete for me, archive, pin, mute, mark a chat read:** app-state patches,
  sent with `SendAppState`.
- **Read receipts:** `MarkRead(ctx, ids, ts, chat, sender[, types.ReceiptTypePlayed])`.
  If the user turned read receipts off, whatsmeow sends `read-self` instead.
- **Presence:** `SendPresence(ctx, types.PresenceAvailable|PresenceUnavailable)`.
  Being "available" **suppresses notifications on the phone**: Baileys
  recommends `markOnlineOnConnect: false`, and GOWA's default
  `presence-on-connect=unavailable` "preserves phone notifications".
- **Typing:** `SendChatPresence(ctx, jid, types.ChatPresenceComposing|ChatPresencePaused, types.ChatPresenceMediaText|ChatPresenceMediaAudio)`.
  Follow a contact's presence with `SubscribePresence(ctx, jid)`.
- **Not supported** ([README](https://github.com/tulir/whatsmeow#features)):
  sending to broadcast lists, and calls.

### 2.8 Media expiry

- A message carries only a pointer to the media (a direct path and a key) on
  WhatsApp's media servers. Old media there returns **404 or 410**.
- For media from history sync, `SendMediaRetryReceipt(ctx, info, mediaKey)` asks
  the phone to upload the file again. This works only if the phone still has
  the file.
  1. The answer arrives as `events.MediaRetry`.
  2. `DecryptMediaRetryNotification` decrypts it into a new `DirectPath`.
  3. Download from that path
     ([mediaretry.go](https://github.com/tulir/whatsmeow/blob/main/mediaretry.go)).
- mautrix-whatsapp automates this: `media_requests.auto_request_media`, sent
  immediately or at a set local time.
- How long WhatsApp keeps a personal account's media on its servers is **not
  documented (unverified)**. One Business-API vendor documents 30 days for its
  own media ([Bird](https://bird.com/docs/api/errors/E15021)).
- Rule of thumb: **download media when it arrives**, and queue retries for media
  from history.

### 2.9 Ban risk, and how to behave like a person

**What the rules say.**

- **Terms of service** ([ToS](https://www.whatsapp.com/legal/terms-of-service)):
  - acceptable use excludes "bulk messaging, auto-messaging, auto-dialing, and
    the like" and "any non-personal use of our Services unless otherwise
    authorized";
  - "you must not directly or through automated means: (a) reverse engineer…".
- **The Help Center:**
  - Linking an account to an unofficial app or website, *even in the past*, can
    lead to a temporary or permanent ban
    ([About linked devices](https://faq.whatsapp.com/378279804439436/?cms_platform=android),
    snippet).
  - A "Temporarily banned" notice can mean an unofficial app or scraping
    ([FAQ](https://faq.whatsapp.com/1848531392146538),
    [unofficial apps](https://faq.whatsapp.com/1217634902127718)).
- **The libraries say so too.**
  - Baileys: "Do not spam people with this. We discourage any stalkerware, bulk
    or automated messaging usage."
  - whatsapp-web.js: "WhatsApp does not allow bots or unofficial clients on
    their platform."

**What the server enforces.** The server's own reasons for temporary bans,
mirrored as `events.TempBanReason` in whatsmeow
([events.go](https://github.com/tulir/whatsmeow/blob/main/types/events/events.go)):

| Code | Reason |
|---|---|
| 101 | sent too many messages to people who don't have you in their address books |
| 102 | too many people blocked you |
| 103 | created too many groups with people who don't have you |
| 104 | sent the same message to too many people |
| 106 | sent too many messages to a broadcast list |

- **Related connection failures:** 402 (temporarily banned), 403 ("LOCKED") and
  406 ("BANNED").
- **The "reachout timelock".** A softer restriction, often applied before a ban,
  that stops the account from messaging new contacts:
  `events.NotifyAccountReachoutTimelock{EnforcementType, IsActive, TimeEnforcementEnds}`.
  mautrix-whatsapp shows it in its bridge state, and WAHA documents it.
- **During a ban,** WAHA's documentation warns: "Do not re-pair the device
  during a ban — it does not lift the ban … Repeated temporary bans may end in a
  permanent one (406)."

**Rules for staying "human"** (derived from the above):

1. **Messages go to people the user already talks to.**
   - Never message a non-contact or start a new group unless the user confirmed
     that specific message.
   - Never send the same text to several chats.
   - Cap volume per hour and per day.
2. **Pace like a person.** Send a typing indicator (composing) for a time
   proportional to the message's length, add a few seconds of jitter, then
   send.
   - Mark as read only what the user, or the assistant acting for them,
     actually read.
3. **Stay "unavailable" by default,** so the phone keeps notifying the user.
   - Go "available" only around a send the user started. mautrix leaves
     `send_presence_on_typing` off by default.
   - GOWA by default goes "available" for 5 minutes every 24 hours; whether that
     matters is **(unverified)**.
4. **Keep one stable device identity.** Do not pair again and again.
   - Give the device a recognizable name (`DeviceProps.Os`), so the user knows
     what it is in their Linked devices list.
5. **Keep whatsmeow current.** It updated its built-in WhatsApp Web version 7
   times between 2026-09-28 and 2026-10-31. An old build gets
   `ConnectFailureClientOutdated` (405).
   - Calling `GetLatestVersion` and then `SetWAVersion` at startup helps.
6. **Stay connected,** and reconnect with backoff.
   - On `TemporaryBan` or a reachout timelock, stop all sending and tell the
     user.

### 2.10 The official WhatsApp Business Platform (Cloud API), for contrast

- **It serves business numbers, not people.** The Cloud API is for phone
  numbers registered to a Meta WhatsApp Business Account.
  - It never sees a person's personal chats, groups or history.
  - Webhooks deliver only messages sent to the business number.
  - Starting a conversation outside the 24-hour customer-service window needs a
    template Meta approved in advance.
- **"Coexistence" (2025)** lets a number from the **WhatsApp Business app** also
  be connected to the Cloud API
  ([360dialog](https://docs.360dialog.com/partner/waba-management/whatsapp-coexistence),
  [Jelou](https://docs.jelou.ai/en/connect/prerequisites/coexistence.md)).
  - It imports up to 6 months of one-to-one text history once; media goes back
    only about 14 days, and groups are not synced.
  - The Business app must be opened at least every ~13 days.
  - Personal WhatsApp accounts are not eligible. I infer that from the
    eligibility rules **(unverified for personal accounts)**.
- **So it does not fit.** It cannot act as someone's personal account; that
  would mean moving the person to the Business app plus a Meta business setup
  and app review.

---

## 3. WhatsApp options

### 3.1 whatsmeow (recommended engine)

- **Where, and who.**
  [github.com/tulir/whatsmeow](https://github.com/tulir/whatsmeow) ·
  [godoc](https://pkg.go.dev/go.mau.fi/whatsmeow). Written in Go.
  - **License:** MPL-2.0, with a GPL-3.0 dependency (§1).
  - **Maintenance:** about 7.8k stars; latest commit 2026-11-07. It has no
    tagged releases (Go pseudo-versions only). The maintainer is Tulir Asokan
    (mautrix, Beeper), and the built-in WhatsApp Web version is bumped every 1–2
    weeks.
  - **Form:** a library.
- **Authentication:** QR code, pairing code or passkey (§2.2).
- **State:** `sqlstore.New(ctx, dialect, dsn, log)`; per its own comment, "Only
  SQLite and Postgres are currently fully supported".
  - Tables: `whatsmeow_device`, `_identity_keys`, `_pre_keys`, `_sessions`,
    `_sender_keys`, `_app_state_sync_keys`, `_app_state_version`,
    `_app_state_mutation_macs`, `_contacts` (first, full, push and business
    names, redacted phone), `_chat_settings` (muted_until, pinned, archived),
    `_message_secrets`, `_privacy_tokens`, `_nct_salt`, `_lid_map`,
    `_event_buffer` (**plaintext**), `_retry_buffer` (**plaintext**).
  - One container can hold many accounts (`GetAllDevices`).
  - `store.AllStores` defines the interfaces for a custom backend.
- **History, realtime, contacts, actions:** everything in §2.3–2.8.
- **Code sketch:**

```go
// Must be set BEFORE pairing: these props decide how much history the phone sends.
store.DeviceProps.Os = proto.String("substrate")
store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()
store.DeviceProps.RequireFullSync = proto.Bool(true)
store.DeviceProps.HistorySyncConfig.FullSyncDaysLimit = proto.Uint32(3650)

container, _ := sqlstore.New(ctx, "sqlite3", "file:/data/wa.db?_foreign_keys=on", nil) // or "postgres", dsn
dev, _ := container.GetFirstDevice(ctx)
cli := whatsmeow.NewClient(dev, nil)
cli.SynchronousAck = true             // ack to WhatsApp only after our handler returns
cli.EnableDecryptedEventBuffer = true // survive a crash between decrypt and handling
cli.ManualHistorySyncDownload = true  // we download and persist history blobs ourselves
cli.AddEventHandlerWithSuccessStatus(func(evt any) bool {
    switch e := evt.(type) {
    case *events.Message:
        if n := e.Message.GetProtocolMessage().GetHistorySyncNotification(); n != nil {
            hs, err := cli.DownloadHistorySync(ctx, n, true)
            return err == nil && forward(hs) // forward = POST a batch to the substrate webhook
        }
        return forward(e) // e.Info: Chat, Sender, SenderAlt, ID, Timestamp, PushName
    case *events.Receipt, *events.ChatPresence, *events.Contact, *events.PushName,
        *events.Archive, *events.Pin, *events.Mute, *events.Picture, *events.HistorySync:
        return forward(e)
    case *events.LoggedOut, *events.TemporaryBan, *events.NotifyAccountReachoutTimelock:
        alert(e)
    }
    return true
})
if cli.Store.ID == nil { // not yet paired
    qr, _ := cli.GetQRChannel(ctx)
    _ = cli.Connect()
    // either render each qr item with Event=="code", or:
    code, _ := cli.PairPhone(ctx, "+15551234567", true, whatsmeow.PairClientChrome, "Chrome (Linux)")
    _, _ = qr, code
} else {
    _ = cli.Connect()
}

// Actions
chat := types.NewJID("15557654321", types.DefaultUserServer) // rewritten to the LID on send
cli.SendChatPresence(ctx, chat, types.ChatPresenceComposing, types.ChatPresenceMediaText)
resp, _ := cli.SendMessage(ctx, chat, &waE2E.Message{Conversation: proto.String("on my way")})
cli.SendMessage(ctx, chat, cli.BuildReaction(chat, theirJID, theirMsgID, "👍"))
cli.SendMessage(ctx, chat, cli.BuildEdit(chat, resp.ID, &waE2E.Message{Conversation: proto.String("on my way!")}))
cli.SendMessage(ctx, chat, cli.BuildRevoke(chat, types.EmptyJID, resp.ID))
cli.MarkRead(ctx, []types.MessageID{theirMsgID}, time.Now(), chat, theirJID)
// Older history: oldest is the *types.MessageInfo of the oldest message we hold for this chat.
cli.SendPeerMessage(ctx, cli.BuildHistorySyncRequest(oldest, 50))
```

- **Risks:**
  - The WhatsApp terms of service and ban risk (§2.9).
  - The API changes; for example, `ctx` is now the first argument nearly
    everywhere. Pin the version and update weekly.
  - The GPL-3.0 dependency.
  - A raw integration must also cover what mautrix covers: media retry,
    app-state recovery, LID migration and phone-offline warnings.
- **Recommendation: first choice.** Build a small Go sidecar modelled on
  `cmd/slack-socket`:
  - **It holds the session** and forwards batched events (messages, receipts,
    reactions, edits, revokes, history chunks, contacts and app state, LID
    mappings) to a substrate webhook. It acks to WhatsApp only after substrate
    accepted the batch.
  - **It serves a small authenticated HTTP API** for pairing (QR or code) and
    for actions: send, react, edit, revoke, mark read, typing, media upload and
    older-history requests.
  - **The whatsmeow store lives in its own SQLite file,** or its own Postgres
    database, encrypted at rest.

### 3.2 mautrix-whatsapp, as a connector

- **Where, and who.** [github.com/mautrix/whatsapp](https://github.com/mautrix/whatsapp),
  [docs](https://docs.mau.fi/bridges/go/whatsapp/). Written in Go.
  - **License:** AGPL-3.0; the exceptions cover only Beeper and Element.
  - **Maintenance:** about 1.9k stars; monthly releases (v0.2610.0 on
    2026-10-16); latest commit 2026-11-07.
  - **Form:** a bridgev2 Matrix puppeting bridge, which needs a Matrix
    homeserver or Beeper. `pkg/connector` implements the bridgev2 network side.
- **What it adds on top of whatsmeow:**
  - history-sync orchestration: `max_initial_conversations`, `dispatch_wait`, a
    backfill queue, and backward backfill on demand;
  - automatic media retry;
  - phone-offline detection and the phone ping;
  - LID migration of existing chats (`lidmigrate.go`);
  - app-state recovery commands;
  - shows the reachout timelock in its bridge state;
  - a persistent retry store, display-name templates (`{{.PushName}}`,
    `{{.FullName}}`, `{{.BusinessName}}`, `{{.Phone}}`), polls, status
    broadcasts, sticker conversion.
- **Authentication, state, actions:** the same as whatsmeow. Its roadmap ticks
  replies, reactions, redactions, presence, typing and read receipts in both
  directions
  ([ROADMAP](https://github.com/mautrix/whatsapp/blob/main/ROADMAP.md)).
- **Risks:**
  - The AGPL: embedding the connector makes our sidecar AGPL.
  - It is Matrix-shaped: the sibling report covers whether bridgev2 can be
    driven without a homeserver.
- **Recommendation:** read it as the reference for edge cases. Take its code
  only if the framework report picks bridgev2.

### 3.3 Baileys

- **Where, and who.** [github.com/WhiskeySockets/Baileys](https://github.com/WhiskeySockets/Baileys).
  Written in TypeScript for Node 20+.
  - **License:** MIT, but it depends on the GPL-3.0 `libsignal`.
  - **Maintenance:** about 11.6k stars. Both `v7.0.0-rc14` and `v6.7.24` were
    released on 2026-07-29; v7 is still a release candidate after months. Latest
    commit on master 2026-10-26. Maintained by the community.
  - **Form:** a library speaking the websocket protocol; no browser.
- **Authentication:** a QR code, or `sock.requestPairingCode(number)`.
  - The state is an `AuthenticationState` you implement yourself.
  - `useMultiFileAuthState` is a sample. Its README warns that keys change on
    every message and "you must save the updated keys… Not doing so will prevent
    your messages from reaching the recipient".
- **History:**
  - `syncFullHistory: true`, together with imitating a desktop browser;
  - the event `messaging-history.set`;
  - `fetchMessageHistory(50, key, ts)` for older pages.
- **Realtime.** The `sock.ev` events: `messages.upsert`, `messages.update`,
  `message-receipt.update`, `presence.update`, `contacts.upsert`,
  `contacts.update`, `chats.upsert`, `chats.update`, `groups.update`,
  `creds.update`.
  - v7 adds a `LIDMappingStore`, and `remoteJidAlt` / `participantAlt` on
    message keys.
- **Actions.** All through `sendMessage(jid, …)`:
  - text and media: `{text}`, `{image}`;
  - reactions: `{react:{text,key}}`;
  - edits: `{edit:key, text}`;
  - deletes: `{delete:key}`.

  Others: `readMessages([keys])`, `sendPresenceUpdate('composing', jid)`,
  `presenceSubscribe`.
- **Risks:**
  - The same terms of service.
  - A Node runtime in the sidecar.
  - LID breakage for downstream projects during v7.
- **Recommendation:** only if we wanted a Node sidecar. In a Go codebase it has
  no advantage over whatsmeow.

### 3.4 whatsapp-web.js

- **Where, and who.** [github.com/wwebjs/whatsapp-web.js](https://github.com/wwebjs/whatsapp-web.js),
  which moved from `pedroslopez`. Written in JavaScript.
  - **License:** Apache-2.0.
  - **Maintenance:** about 22.9k stars; v1.34.7 (2026-04-24), and a
    `v2.0.0-alpha.0` tag; active.
  - **Form:** a library that runs the real WhatsApp Web in headless Chromium
    through Puppeteer, "to reduce the risk of being blocked".
- **Authentication:** a QR code; the session is the browser profile
  (`LocalAuth`, `RemoteAuth`). Whether it supports pairing codes is
  **(unverified)**.
- **History:** whatever WhatsApp Web itself has loaded. `chat.fetchMessages`
  pages through it; how deep it reaches is **(unverified)**.
- **Downsides:**
  - one Chromium per account;
  - it breaks whenever WhatsApp Web changes its internals;
  - Node.
- **Recommendation:** no.

### 3.5 WAHA (WhatsApp HTTP API)

- **Where, and who.** [github.com/devlikeapro/waha](https://github.com/devlikeapro/waha),
  [docs](https://waha.devlike.pro). Written in TypeScript (NestJS), plus its
  engines.
  - **License:** Apache-2.0.
  - **Maintenance:** about 7.8k stars; release 2026.10.1 on 2026-10-28.
  - **Former Plus features are free.** Since 2026.6.1, everything that needed
    the paid "Plus" tier ships in the free image: Postgres, Mongo and S3
    storage, unlimited sessions, sending media, API keys
    ([blog](https://waha.devlike.pro/blog/waha-2026-6/)).
  - **Form:** a Docker image serving REST, webhooks and websocket events.
- **Engines** ([docs](https://waha.devlike.pro/docs/how-to/engines/)):
  - **WEBJS** (whatsapp-web.js) and **WPP** (WPPConnect) run a browser.
  - **NOWEB** is built on Baileys.
  - **GOWS** is built on whatsmeow (its docs cite whatsmeow's keepalive
    defaults). It ships as a compiled binary: the
    [devlikeapro/gows](https://github.com/devlikeapro/gows) repository is
    archived and held "complied binary and proto files". I found no source for
    it.
- **Authentication:** `POST /api/sessions`, then `POST /api/{session}/auth/qr`
  or `POST /api/{session}/auth/request-code`.
- **History:** `GET /api/{session}/chats/{chatId}/messages`, served from the
  engine's store, plus GOWS's full-sync settings.
- **Realtime:** webhooks for `message`, `message.reaction`, `message.ack`,
  `message.edited`, `message.revoked`, `presence`, `session.status`, and more.
- **Actions:**
  - sending: `sendText`, `sendImage`, `sendFile`, `sendVoice`, `sendVideo`;
  - reacting: `POST /api/reaction`;
  - editing and deleting: `PUT` and `DELETE /api/{session}/chats/{chatId}/messages/{id}`;
  - marking read: `sendSeen`;
  - presence: `startTyping`, `stopTyping`, `presence`;
  - contacts: `GET /api/contacts/all`, `/api/{session}/lids`.
- **Risks:**
  - The response and webhook shapes differ by engine ("API responses and webhook
    payloads may differ significantly").
  - It adds a Node app on top of whatsmeow and still needs a durable bridge into
    substrate.
  - How it retries webhooks is **(unverified)**.
- **Recommendation:** a reasonable ready-made choice if we never want to write
  Go for WhatsApp. For us, it is an extra layer that gives less control over
  acks and history.

### 3.6 Evolution API and Evolution Go

- **Where, and who.**
  [evolution-foundation/evolution-api](https://github.com/evolution-foundation/evolution-api)
  is written in TypeScript and uses Baileys or the official Cloud API.
  [evolution-go](https://github.com/evolution-foundation/evolution-go) is
  written in Go on whatsmeow.
  - **License:** "Apache-2.0 with additional conditions": a closed system must
    show a notice that it uses Evolution, and the frontend logo must stay.
  - **Maintenance:**
    - evolution-api: about 10.1k stars; last stable 2.3.7 (2025-12-05); 2.4.0-rc2
      (2026-05-17).
    - evolution-go: 0.7.2 (2026-07-03).
- **Built for business use.** It integrates with Chatwoot, Typebot, RabbitMQ,
  Kafka and SQS.
- **It phones home.** Evolution Go has "built-in licensing with registration,
  activation, and heartbeat".
- **Recommendation:** no. It is built for CRMs, the licence carries extra
  conditions, and Evolution Go checks in with a license server.

### 3.7 GOWA (go-whatsapp-web-multidevice)

- **Where, and who.**
  [github.com/aldinokemal/go-whatsapp-web-multidevice](https://github.com/aldinokemal/go-whatsapp-web-multidevice).
  Written in Go on whatsmeow.
  - **License:** MIT (the binary still contains the GPL dependency).
  - **Maintenance:** about 5.0k stars; very active, with roughly weekly releases
    (v9.9.0 on 2026-11-06).
- **Form:** a single binary or Docker image, serving:
  - REST, a web UI, and **MCP** at `/mcp`;
  - webhooks signed with HMAC (`X-Hub-Signature-256`), configurable per device
    with event filters;
  - several accounts at once;
  - a chat store in SQLite or Postgres.
- **Choices worth copying:**
  - `presence-on-connect=unavailable` by default (keeps the phone's
    notifications);
  - an optional "daily presence pulse" (24 h interval, 5 min available);
  - a `--os` flag for the device name;
  - `auto-mark-read` exists but is off unless set.
- **How its webhooks fail.** 5 attempts with exponential backoff (1–16 s) and a
  10 s timeout; after that the event is dropped. Recovering it means re-reading
  GOWA's chat store.
- **Recommendation:** the best ready-made whatsmeow daemon, and good reading
  (`history_sync.go`). But it does not guarantee delivery across long outages,
  and its API is its own.

### 3.8 wuzapi

- **Where, and who.** [github.com/asternic/wuzapi](https://github.com/asternic/wuzapi).
  Written in Go on whatsmeow.
  - **License:** MIT.
  - **Maintenance:** about 1.0k stars; v1.1.0 on 2026-10-26.
- **What it offers:**
  - REST with a token per user;
  - HMAC-signed webhooks, retried (`WEBHOOK_RETRY_COUNT=2`, a 30 s delay, then a
    dead-letter queue in RabbitMQ);
  - an optional global RabbitMQ stream;
  - SQLite or Postgres, and S3 for media;
  - `WUZAPI_AUTO_PRESENCE=unavailable` to keep the phone's notifications.
- **Recommendation:** in the same class as GOWA; not needed.

### 3.9 whatsapp-rust (oxidezap)

- **Where, and who.** [github.com/oxidezap/whatsapp-rust](https://github.com/oxidezap/whatsapp-rust).
  Written in Rust.
  - **License:** MIT.
  - **Maintenance:** about 1.0k stars; v0.7.0 (2026-08-07); active.
- "Inspired by whatsmeow and Baileys". It has its own Signal-protocol crate
  (`wacore-libsignal`, declared MIT), so the declared stack is entirely
  permissive (where that crate came from is not audited). It also supports
  calls.
- It is used by `baileyrs` and by felipeadeildo/whatsapp-mcp.
- **Recommendation:** a way out if GPL turns out to be unacceptable. But it is
  younger and smaller, and would mean a Rust sidecar.

### 3.10 WhatsApp MCP servers

- **[lharries/whatsapp-mcp](https://github.com/lharries/whatsapp-mcp)** (MIT,
  about 6.8k stars):
  - **Design:** a Go bridge on whatsmeow, which stores messages in SQLite, plus
    a Python MCP server. Tools: `search_contacts`, `list_messages`,
    `list_chats`, `send_message`, `send_file`, `send_audio_message`,
    `download_media`.
  - **Its README warns:**
    - "After approximately 20 days, you will might need to re-authenticate";
    - the "lethal trifecta" risk of prompt injection leaking private data.
  - **Status: unmaintained.** Latest commit 2025-07-13, 274 open issues and pull
    requests, and whatsmeow pinned at 2025-03-18 (almost certainly broken by the
    LID changes, **unverified**).
- **Maintained alternatives:**
  - [FelixIsaac/whatsapp-mcp-extended](https://github.com/FelixIsaac/whatsapp-mcp-extended)
    (MIT, whatsmeow);
  - [verygoodplugins/whatsapp-mcp](https://github.com/verygoodplugins/whatsapp-mcp)
    (whatsmeow);
  - [finereli/whatsapp-mcp](https://github.com/finereli/whatsapp-mcp)
    (read-only, Baileys, encrypted store; it notes "WhatsApp queues messages for
    a linked device for a limited time");
  - [felipeadeildo/whatsapp-mcp](https://github.com/felipeadeildo/whatsapp-mcp)
    (GPL-3.0, whatsapp-rust on Cloudflare Durable Objects);
  - GOWA and WAHA each expose MCP as well.
- **Relevance:** substrate is already the MCP surface. These confirm the same
  design (a bridge plus a local store), but none is a sync engine to depend on.

---

## 4. Signal: platform behaviour

### 4.1 Linked devices and their limits

- **The official rules** ([Linked Devices](https://support.signal.org/hc/en-us/articles/360007320551),
  updated 2026-09-22):
  - "After linking a device, your phone has to come online at least once every
    30 days. Otherwise, your linked devices will become unlinked."
  - "Linked devices will also become unlinked after 45 days of inactivity."
  - **5 linked devices at most.**
  - Only the registered primary device can link a device again.
  - Desktops, iPads, iPhones and Android devices can all be linked.
- **The mailbox** ([Signal blog, 2025-01-27](https://signal.org/blog/a-synchronized-start-for-linked-devices/)):
  "Signal will hold a message in a device's mailbox for up to 45 days".
  Attachments are deleted from the server 45 days after upload.
- **The client must stay current.** signal-cli's README: "The official Signal
  clients expire after three months and then the Signal-Server can make
  incompatible changes."
  - **A real incident, the SPQR enforcement.** SPQR is Signal's post-quantum
    upgrade to its ratchet.
    - From 6 March 2026, new registrations required clients to support it.
    - From about 31 March 2026, the server unregistered accounts and unlinked
      devices whose client did not.
    - signal-cli has advertised it since v0.14.1
      ([issue #1993](https://github.com/AsamK/signal-cli/issues/1993)).
  - The signal-cli maintainer's rule: "always updating to the latest version is
    recommended."

### 4.2 Linking as a secondary device

**The flow:**

1. The new device opens a provisioning websocket and gets a provisioning
   address.
2. It shows `sgnl://linkdevice?uuid=<address>&pub_key=<base64 Curve25519 key>`
   as a QR code. Newer clients append `&capabilities=…`.
3. The phone scans it: Settings → Linked devices → Link a new device, after a
   biometric or passcode unlock.
4. The phone sends an encrypted `ProvisionMessage`. It carries the ACI/PNI
   identity keys (the account and phone-number identities), the profile key,
   the master key or account entropy, a provisioning code, and optionally the
   *ephemeral backup key* and the media root backup key.
5. The new device registers with its own prekeys, including post-quantum Kyber
   prekeys.

**No captcha or SMS is needed to link.**

**In each client:**

- **signal-cli.**
  - `signal-cli link -n NAME` prints the URI, and since v0.14.0 a QR code in the
    terminal.
  - In the multi-account JSON-RPC daemon: `startLink` returns
    `{deviceLinkUri}`, then `finishLink {deviceLinkUri, deviceName}` returns
    `{number, aci}`.
  - Phone-numberless accounts can be linked since v0.14.9
    ([man page](https://github.com/AsamK/signal-cli/blob/master/man/signal-cli-jsonrpc.5.adoc)).
- **signal-cli-rest-api.** `GET /v1/qrcodelink?device_name=…` returns a PNG, and
  `/v1/qrcodelink/raw` returns the URI.
- **signalmeow.** `PerformProvisioning(ctx, deviceStore, name, allowBackup)`
  returns a channel of `ProvisioningResponse{State, ProvisioningURL, ProvisioningData, Err}`.
  The link URI carries `capabilities=backup5,nopni2` when `allowBackup` is set
  ([provisioning.go](https://github.com/mautrix/signal/blob/main/pkg/signalmeow/provisioning.go)).
- **presage.** `Manager::link_secondary_device(store, SignalServers::Production, name, url_tx)`.

### 4.3 History on a linked device: "link and sync"

**What Signal offers.** Since early 2025 (blog of 2025-01-27, rolled out over
the following weeks), linking offers the choice **Transfer Message History** or
**Don't Transfer**.

- **The transfer** is all chats as one compressed, encrypted archive: text,
  reactions, quotes, receipts, call history, group updates and stickers.
  - The primary uploads it to Signal's file servers as an attachment.
  - It is encrypted with a **one-time 256-bit key carried in the provisioning
    message**.
  - It stays available "only for a limited time".
- **Media** travel only as pointers, so **only media less than 45 days old** can
  be fetched.
- **Choosing "Don't transfer"** means the only way back is to link again
  (support page).

**Which clients can receive it:**

- **signalmeow / mautrix-signal: YES.** Added in v0.8.0 (2025-02-16); the
  "latest backup/transfer protocol" landed in v25.11, and v25.12 fixed the
  option not appearing on Android
  ([CHANGELOG](https://github.com/mautrix/signal/blob/main/CHANGELOG.md)).
  1. It advertises `backup5`.
  2. After linking, `WaitForTransfer` waits up to 1 h and returns
     `TransferArchiveMetadata{cdn, key, error}`, where the error can be
     `RELINK_REQUESTED` or `CONTINUE_WITHOUT_UPLOAD`.
  3. `FetchAndProcessTransfer` downloads the archive, checks its HMAC, decrypts
     it, inflates it and parses the frames into the tables
     `signalmeow_backup_recipient`, `_backup_chat` and `_backup_message`
     ([backup.go](https://github.com/mautrix/signal/blob/main/pkg/signalmeow/backup.go)).
  4. mautrix-signal then creates the chats and backfills them from those tables
     ([chatsync.go](https://github.com/mautrix/signal/blob/main/pkg/connector/chatsync.go)).
- **signal-cli: NO,** as of v0.14.10. It has no transfer-archive code: it keeps
  only the `mediaRootBackupKey` from provisioning, its changelog never mentions
  a transfer, and I found no issue tracking one.
- **presage: NO.** The maintainer wrote on 2026-04-08: "we haven't integrated
  the encrypted backup system (yet) which means we do not support transfering
  message history as of today" ([issue #396](https://github.com/whisperfish/presage/issues/396)).

**A workaround on the signal-cli path.** Link Signal Desktop with the transfer,
export its encrypted SQLCipher database once with a tool such as
[signal-export](https://github.com/carderne/signal-export) (latest release
v3.9.3, 2026-09-02; licence not checked), and import that. Or accept that there
is no history.

### 4.4 Contacts and profiles

**Who a person is.** Several identifiers can name one person, and some may be
missing:

- the ACI (account UUID);
- the PNI (phone-number identity);
- the E.164 phone number, which may be hidden;
- a username (`u:name.01`);
- accounts with no phone number at all: signal-cli 0.14.9 and mautrix-signal
  v26.10, both October 2026, added support for them.

**Where names and details come from:**

- **The Storage Service.** Encrypted records synced across the user's devices:
  `ContactRecord` (the system given and family names, nickname, note, profile
  key; blocked, archived and hidden flags), groups, and account settings.
  - signal-cli has implemented "full remote storage sync" since v0.13.0, and
    syncs nicknames and notes.
  - signalmeow has `StorageSync` and `FetchStorage`; mautrix-signal runs it when
    `sync_contacts_on_startup` is set.
- **The contact-sync message.** A `SyncMessage.Contacts` attachment of
  `ContactDetails`, requested with signal-cli's `sendSyncRequest`, signalmeow's
  `SendContactSyncRequest` or presage's `request_contacts`. Its arrival shows
  up as presage's `Received::Contacts`.
- **Profiles.** Each contact's name, "about" text and avatar are encrypted;
  reading them needs that contact's profile key.
  - signal-cli: `listContacts --detailed`, `getAvatar`.
  - signalmeow: `RetrieveProfileByID`, `DownloadUserAvatar`.
- **Phone-number discovery (CDSI)** is rate-limited.

The presage maintainer adds that contact syncing "has also been moved to this
facility [the backup system] recently". That detail is **(unverified)**.

**For substrate:** key a Signal person on the **ACI**. Store as attributes: the
PNI, phone number, profile name, system name, nickname, username and avatar.

### 4.5 Realtime updates and catch-up

- **The connection.** An authenticated websocket. The server pushes queued
  envelopes, and the client acks each one.
- **When the backlog is drained** a "queue empty" marker follows: signalmeow's
  `events.QueueEmpty`, presage's `Received::QueueEmpty`.
- **Queued messages wait up to 45 days.**
- **The loss window.** Once a message is acked, only the client holds it, so
  each client needs its own guard.
  - **signalmeow.** A buffer of decrypted events (v0.8.3: "prevent losing
    messages if the bridge is stopped in the middle of event handling"), plus
    the handler's success flag.
  - **signal-cli.** It decrypts, acks, then emits. The mode decides what is
    lost:
    - With the daemon's default `--receive-mode on-start`, it receives even when
      no consumer is connected, so a consumer outage drops events.
    - `--receive-mode manual` together with `subscribeReceive` (or holding the
      HTTP `/api/v1/events` stream open) receives only while a consumer is
      subscribed; the server keeps the rest.
    - The HTTP SSE endpoint keeps the **last 1000 events in memory** and replays
      them after a `Last-Event-ID` (v0.14.9). Event ids embed the daemon's start
      time, so all of it is lost when the daemon restarts
      ([SseEventBuffer.java](https://github.com/AsamK/signal-cli/blob/master/src/main/java/org/asamk/signal/http/SseEventBuffer.java)).
  - **signal-cli-rest-api** spells out the trap: in `on-start` mode, "messages
    arriving while no subscriber is attached … are silently dropped"; in
    `manual` mode "messages remain queued server-side"
    ([README](https://github.com/bbernhard/signal-cli-rest-api#advanced-settings)).
- **Keep receiving all the time.** The protocol expects regular receiving for
  prekeys and group updates (signal-cli README).

### 4.6 Acting as the user

- **How messages are identified.** A message is **(author ACI, sent timestamp in
  ms)**. Reactions, quotes, edits, deletes and receipts all name their target
  that way. A group is named by its base64 group id.
- **Reactions:** `DataMessage.Reaction{Emoji, Remove, TargetAuthorAci, TargetSentTimestamp}`.
- **Edits:** `EditMessage{TargetSentTimestamp, DataMessage}`. Allowed within
  **24 h, up to 10 times**
  ([support](https://support.signal.org/hc/articles/6255134251546-Edit-Message)).
- **Delete for everyone:** `DataMessage.Delete{TargetSentTimestamp}`, within
  **24 h**
  ([support](https://support.signal.org/hc/articles/360050426432-Delete-for-everyone)).
- **Receipts:** delivery receipts are automatic; read and viewed receipts are
  sent explicitly.
- **Typing:** STARTED and STOPPED, shown for 15 s.
- **Quotes:** `Quote{Id, AuthorAci, Text, Attachments}`.
- **Attachments:** encrypted with AES-CBC plus an HMAC, and uploaded to Signal's
  file servers.
- **Also available:** stickers, mentions, text styles, link previews, polls,
  pins and stories.
- **What the user's other devices see.** A message sent from a linked device
  reaches the user's other devices as `SyncMessage.Sent`.

### 4.7 Rate limits and challenges

- **How the server pushes back:**
  - 413 and 429, with `Retry-After`;
  - 428 **"proof required"**, carrying a token and the allowed options
    (`recaptcha` or `pushChallenge`).
- **signal-cli:**
  - It reports `token` and `retryAfterSeconds` in `SendMessageResult`, with exit
    code 5 for a rate limit and 6 for a rejected captcha.
  - `submitRateLimitChallenge --challenge TOKEN --captcha signalcaptcha://…`
    lifts the limit; the captcha comes from
    `https://signalcaptchas.org/challenge/generate.html`.
  - `pushChallenge` needs FCM or APNs, which a headless client lacks.
- **signalmeow** treats 413, 428 and 429 as rate limits or retryable errors. I
  found no captcha flow in it (from a grep only).
- **No proof-of-work challenges.** I found no evidence that Signal uses them;
  the searches turned up only unrelated services.
- **What triggers limits:** sending to many new recipients, and phone-number
  lookups. An assistant writing at human volume should rarely meet them.

### 4.8 Terms and posture

- **Signal's terms** ([ToS](https://signal.org/legal/)): "You must not (or
  assist others to) access, use, modify, distribute, transfer, or exploit our
  Services in unauthorized manners… (c) create accounts for our Services through
  unauthorized or automated means." They have no explicit clause against
  third-party clients.
- **libsignal:** "Use outside of Signal is unsupported."
- **In practice,** I found no reports of bans for ordinary use of linked
  third-party clients **(unverified)**. Clients break through protocol changes
  instead, as in the SPQR episode.

---

## 5. Signal options

### 5.1 signal-cli

- **Where, and who.** [github.com/AsamK/signal-cli](https://github.com/AsamK/signal-cli).
  Written in Java (JRE 25 or newer), with a GraalVM native build.
  - **License:** GPL-3.0.
  - **Maintenance:** about 5.0k stars; v0.14.10 (2026-11-07), monthly releases.
    The maintainer is AsamK. It uses a patched libsignal-service-java and the
    native `libsignal-client` 0.106.1.
  - **Form:** a CLI, plus a daemon offering:
    - JSON-RPC over a UNIX socket, TCP or HTTP (`POST /api/v1/rpc`,
      `GET /api/v1/events` for SSE, `GET /api/v1/check`);
    - D-Bus;
    - `jsonRpc` over stdio;
    - several accounts at once.
- **Authentication:** linking (§4.2).
- **State** (`--config`, default `$XDG_DATA_HOME/signal-cli`):
  - `data/accounts.json`;
  - `data/<account>`: JSON holding the keys, profile key, master key and
    password;
  - `data/<account>.d/account.db`: SQLite tables `recipient`, `session`,
    `identity`, `pre_key`, `signed_pre_key`, `kyber_pre_key`, `sender_key`,
    `sender_key_shared`, `group_v2`, `storage_id`, `cdsi`, `message_send_log`,
    `sticker`, `key_value`;
  - `attachments/`, `avatars/`, `stickers/`.

  **It stores no messages.**
- **History:** none; no link and sync.
- **Realtime:** JSON-RPC `receive` notifications, `subscribeReceive` in manual
  mode, or SSE with `Last-Event-ID`.
- **Contacts:**
  - `listContacts` (`--detailed`, `--all-recipients`);
  - `sendSyncRequest`;
  - storage sync;
  - `getAvatar`, `listGroups`.
- **Actions:**
  - **`send`** with: attachments as paths or data URIs; `quote*` for replies;
    mentions; text styles; previews; stickers; view-once; `editTimestamp` for an
    edit; `notifySelf`; `noUrgent`; `timestamp`.
  - `sendReaction` (`emoji`, `targetAuthor`, `targetTimestamp`, `remove`);
  - `sendReceipt` (`read` or `viewed`; takes a list of timestamps);
  - `sendTyping` (`stop`);
  - `remoteDelete`;
  - `sendPollCreate`, `sendPollVote`, `sendPollTerminate`, `sendPinMessage`;
  - `updateContact`, `block`, `unblock`, `updateProfile`;
  - `getAttachment`.
- **Event shape.** An envelope with `sourceUuid`, `sourceNumber`, `sourceName`,
  `sourceDevice`, `timestamp`, and one of:
  - `dataMessage`, which can carry `reaction{emoji, targetAuthorUuid, targetSentTimestamp, isRemove}`,
    `remoteDelete{timestamp}`, `quote` or `attachments[{id, contentType, filename, size}]`;
  - `editMessage{targetSentTimestamp, dataMessage}`;
  - `syncMessage{sentMessage…}`;
  - `receiptMessage{when, isDelivery, isRead, isViewed, timestamps}`;
  - `typingMessage{action, timestamp, groupId}`.

  `./gradlew jsonSchemas` generates JSON Schemas for all of these.
- **Interface sketch:**

```text
signal-cli --config /data daemon --http 127.0.0.1:7583 --receive-mode manual

POST /api/v1/rpc {"jsonrpc":"2.0","id":1,"method":"startLink"}
  -> {"result":{"deviceLinkUri":"sgnl://linkdevice?uuid=…&pub_key=…"}}   # show as QR
POST /api/v1/rpc {"jsonrpc":"2.0","id":2,"method":"finishLink","params":{"deviceLinkUri":"…","deviceName":"substrate"}}
GET  /api/v1/events   (SSE; Last-Event-ID on reconnect; receiving runs only while this is open in manual mode)
POST /api/v1/rpc {"method":"send","params":{"account":"<aci>","recipient":["<aci>"],"message":"hi",
                  "quoteTimestamp":1731150000000,"quoteAuthor":"<aci>"}}
POST /api/v1/rpc {"method":"sendReaction","params":{"account":"…","recipient":["<aci>"],"emoji":"👍",
                  "targetAuthor":"<aci>","targetTimestamp":1731150000000}}
POST /api/v1/rpc {"method":"send","params":{"account":"…","recipient":["<aci>"],"message":"fixed","editTimestamp":1731150000000}}
POST /api/v1/rpc {"method":"remoteDelete","params":{"account":"…","recipient":["<aci>"],"targetTimestamp":1731150000000}}
POST /api/v1/rpc {"method":"sendReceipt","params":{"account":"…","recipient":"<aci>","targetTimestamp":[1731150000000],"type":"read"}}
POST /api/v1/rpc {"method":"sendTyping","params":{"account":"…","recipient":["<aci>"]}}
POST /api/v1/rpc {"method":"listContacts","params":{"account":"…","detailed":true}}
```

- **Risks:**
  - A JVM to run, or the native build.
  - The loss window between ack and consumer (§4.5).
  - It must be updated within about 3 months of Signal changes.
  - Its GPL licence is fine as a separate process.
- **Recommendation:** the solid choice with the lowest integration cost for
  realtime, actions and contacts. Pair it with a small ingestion sidecar that
  holds the SSE stream in manual mode, writes each event to a local outbox at
  once, and forwards to substrate with ack-after-accept.

### 5.2 signal-cli-rest-api

- **Where, and who.** [github.com/bbernhard/signal-cli-rest-api](https://github.com/bbernhard/signal-cli-rest-api),
  [Swagger](https://bbernhard.github.io/signal-cli-rest-api/). A wrapper written
  in Go.
  - **License:** MIT; the image bundles signal-cli (GPL-3.0).
  - **Maintenance:** about 2.9k stars; 0.101 (2026-09-25); latest commit
    2026-10-22.
- **Modes:** `normal`, `native`, `json-rpc` and `json-rpc-native`.
  - In the `json-rpc*` modes, `GET /v1/receive/{number}` is a **websocket**.
  - Set `JSON_RPC_RECEIVE_MODE=manual` for any consumer that restarts.
- **Endpoints:**
  - sending: `POST /v2/send` with `message`, `base64_attachments`, `quote_*`,
    `mentions`, `edit_timestamp`, `text_mode`, `link_preview`, `view_once`,
    `sticker`;
  - reactions: `POST` / `DELETE /v1/reactions/{number}`;
  - receipts: `POST /v1/receipts/{number}`;
  - typing: `PUT` / `DELETE /v1/typing-indicator/{number}`;
  - remote delete: `DELETE /v1/remote-delete/{number}`;
  - contacts: `GET /v1/contacts/{number}[/{uuid}[/avatar]]`,
    `POST /v1/contacts/{number}/sync`;
  - also groups, attachments, `rate-limit-challenge`, devices, polls, profiles
    and identities.
- **Recommendation:** handy for prototypes. In production, signal-cli's own HTTP
  daemon (JSON-RPC plus SSE) is one layer thinner, and this REST layer adds no
  persistence.

### 5.3 mautrix-signal / signalmeow

- **Where, and who.** [github.com/mautrix/signal](https://github.com/mautrix/signal),
  [docs](https://docs.mau.fi/bridges/go/signal/). Written in Go, plus Rust:
  `pkg/libsignalgo` wraps the `libsignal_ffi` library through cgo, built by
  `build-rust.sh`.
  - **License:** AGPL-3.0, with exceptions for Beeper and Element only; an
    exception for libsignal itself must come from Signal.
  - **Maintenance:** about 681 stars; monthly releases (v0.2610.0 on
    2026-10-16); latest commit 2026-11-05; maintained by Tulir and Beeper.
  - **Form:** signalmeow (`pkg/signalmeow`) is a library inside the bridge.
- **State:** stored through dbutil in SQLite or Postgres.
  - Tables: `signalmeow_device`, `_pre_keys`, `_kyber_pre_keys`,
    `_identity_keys`, `_sessions`, `_event_buffer`, `_profile_keys`,
    `_sender_keys`, `_outbound_sender_key_info`, `_groups`, `_recipients`,
    `_unregistered_users`, `_backup_recipient`, `_backup_chat`,
    `_backup_message`.
- **Authentication:** `PerformProvisioning(…, allowBackup=true)`.
- **History:** the transfer archive (§4.3); the only option that has it.
- **Realtime:** `StartReceiveLoops(ctx)` returns a status channel.
  - Events: `ChatEvent`, `DecryptionError`, `Receipt`, `ReadSelf`, `Call`,
    `ContactList`, `ACIFound`, `DeleteForMe`, `MessageRequestResponse`,
    `QueueEmpty`, `LoggedOut`, `IdentityChange`.
  - The handler, `func(events.SignalEvent) bool`, returns a success flag.
- **Contacts:**
  - `StorageSync`, `QueueStorageSync`, `SendContactSyncRequest`;
  - `ContactByACI`, `ContactByE164`;
  - `RetrieveProfileByID`, `DownloadUserAvatar`;
  - `LookupPhone` (CDSI), `ResolveUsername`.
- **Actions:** `SendMessage(ctx, serviceID, *signalpb.Content)` and
  `SendGroupMessage(ctx, gid, content)`, with the helpers:
  - `WrapDataMessage`, `WrapEditMessage`;
  - `TypingMessage(bool)`;
  - `DeliveredReceiptMessageForTimestamps`, `ReadReceptMessageForTimestamps`;
  - `UploadAttachment`.
- **Code sketch:**

```go
ch := signalmeow.PerformProvisioning(ctx, container, "substrate", true) // true: ask for history transfer
for resp := range ch { // URLReceived -> show resp.ProvisioningURL as QR; DataReceived -> linked
    if resp.State == signalmeow.StateProvisioningDataReceived { break }
}
dev, _ := container.DeviceByACI(ctx, aci)
cli := signalmeow.NewClient(dev, log, func(evt events.SignalEvent) bool { return forward(evt) })
if dev.EphemeralBackupKey != nil { // a transfer was requested at link time
    meta, _ := cli.WaitForTransfer(ctx)    // waits up to 1 h for the phone's upload
    _ = cli.FetchAndProcessTransfer(ctx, meta)
    chats, _ := dev.BackupStore.GetBackupChats(ctx) // then read the messages of each chat
    _ = chats
}
status, _ := cli.StartReceiveLoops(ctx)
_ = status
cli.SendMessage(ctx, recipient, signalmeow.WrapDataMessage(&signalpb.DataMessage{
    Timestamp: proto.Uint64(uint64(time.Now().UnixMilli())),
    Reaction: &signalpb.DataMessage_Reaction{Emoji: proto.String("👍"),
        TargetAuthorAci: proto.String(author.String()), TargetSentTimestamp: proto.Uint64(ts)},
}))
```

- **Risks:**
  - The AGPL, once embedded.
  - Building it needs a Rust toolchain and libsignal's FFI, pinned to
    libsignal releases (bumped every month).
  - It promises no stable public API: it is an internal package of a bridge,
    and it breaks along with libsignal upgrades.
  - Its community is smaller than signal-cli's.
- **Recommendation:** the only option that brings Signal history over, and the
  best match in Go for history plus realtime plus actions. Use it if an AGPL
  Signal sidecar is acceptable.

### 5.4 presage (with libsignal-service-rs)

- **Where, and who.** [github.com/whisperfish/presage](https://github.com/whisperfish/presage).
  Written in Rust.
  - **License:** AGPL-3.0.
  - **Maintenance:** about 279 stars. Its last release is 0.7.0 (2025-05-14),
    but commits are still active (2026-11-05). Maintained by the Whisperfish
    developers.
  - **Used by:** gurk-rs (a terminal client, AGPL, about 1.4k stars) and Flare
    (GTK).
- **Store:** `presage-store-sqlite` (sqlx, optional SQLCipher). Contact
  discovery (CDSI) needs a patched `libsqlite3-sys`, because of a BoringSSL
  versus OpenSSL clash.
- **API:**
  - `link_secondary_device`;
  - `receive_messages(ReceivingMode)`, a stream of `Received::{QueueEmpty, Contacts, DecryptionError, Content}`;
  - `send_message`, `send_message_to_group`;
  - `upload_attachments`, `get_attachment`;
  - `request_contacts`, `retrieve_profile`, `discover_contacts_by_phone_number`;
  - `submit_recaptcha_challenge`.
- **History:** none ([issue #396](https://github.com/whisperfish/presage/issues/396)).
- **Recommendation:** no. It would mean a Rust sidecar under the AGPL, with no
  history and infrequent releases.

### 5.5 libsignal

- **Where, and who.** [github.com/signalapp/libsignal](https://github.com/signalapp/libsignal).
  A Rust core with Java, Swift and TypeScript bindings.
  - **License:** AGPL-3.0.
  - **Maintenance:** about 6.1k stars; v0.106.2 (2026-11-05), released roughly
    every week.
- **It is not a client.** It provides the protocol, zkgroup, attestation, the
  network layer and backup parsing.
  - Every option above builds on it: signal-cli through JNI, signalmeow through
    the FFI, presage through libsignal-service-rs.
  - Its README: "Use outside of Signal is unsupported… All APIs and
    implementations are subject to change without notice."
- **Recommendation:** do not build on it directly.

### 5.6 signald

- **Where, and who.** [gitlab.com/signald/signald](https://gitlab.com/signald/signald).
  Written in Java; GPL-3.0.
- **Status: dead.**
  - Its README: "this project is no longer actively maintained. Use signal-cli".
  - Its project description: "Does not work with the current production Signal
    servers."
  - Its last tag is 0.23.2 (2023-01-18), and mautrix-signal dropped it long
    ago.

### 5.7 Signal MCP servers

- [googlarz/signal-mcp](https://github.com/googlarz/signal-mcp) (MIT, active
  2026-10-29): an MCP server and CLI on signal-cli, with a searchable local
  database and a read-only mode.
- Stale ones: [rymurr/signal-mcp](https://github.com/rymurr/signal-mcp)
  (2025-04), stefanstranger/signal-mcp-server (2025-08), and
  alexey-fedorov-com/signal-mcp (which calls itself stale).
- All of them wrap signal-cli and inherit its lack of history.
- **Relevance:** none as a dependency.

---

## 6. Notes on fitting this into substrate

- **Sidecars.** Run one long-lived process per network per account, beside
  `cmd/slack-socket`. Sandboxed functions cannot hold end-to-end encryption
  state.
- **Inbound path, with no lost messages:**
  1. The library emits an event.
  2. The sidecar writes it to a local outbox (or blocks).
  3. It POSTs batches to a substrate webhook trigger.
  4. Only after a 2xx does it ack to the network:
     - whatsmeow: `SynchronousAck`, with the handler returning only after the
       outbox write;
     - signalmeow: the handler returns `true`;
     - signal-cli: persist each SSE event the moment it arrives.
- **Idempotency keys:**
  - WhatsApp: chat JID + message ID (+ sender in groups);
  - Signal: author ACI + sent timestamp.
- **History:**
  - WhatsApp: persist history-sync blobs *before* they are acked. A backfill job
    sends on-demand requests, 50 messages a page per chat, while the phone is
    online: one request in flight per chat, with a 1 h timeout.
  - Signal: process the transfer archive once, right after linking (signalmeow).
- **Outbound actions.** The sidecar serves authenticated HTTP endpoints that
  substrate functions call (the sidecar on the network allowlist).
  - Human pacing lives in the sidecar: typing first, then send.
  - So do rate limits and per-message confirmation before writing to a
    non-contact.
- **Contacts → `core/contact`:**
  - WhatsApp: the LID as the key; the phone number, push name, address-book
    name, business name, username and avatar as attributes.
  - Signal: the ACI as the key; the PNI, phone number, profile name, system
    name, nickname, username and avatar as attributes.
- **Health signals to show the owner:**
  - logged out or unlinked;
  - `TemporaryBan` or a reachout timelock;
  - the phone not seen for 12 or more days (WhatsApp);
  - the risk of unlinking (Signal: the phone within 30 days, the device within
    45);
  - "client outdated", and "time to update the sidecar" (Signal's 3-month
    window; WhatsApp's weekly version bumps).
- **Session state is a secret.** It holds identity keys and plaintext buffers.
  - Keep it in the sidecar's own volume (SQLite) or a separate Postgres
    database, never in substrate's record tables.
  - Encrypt it at rest, and back it up.
  - Losing it means linking again. On WhatsApp that brings a fresh history
    sync; on Signal the history transfer comes only if the new link asks for it.

---

## 7. Comparison table

| Option | Network | Language / form | License, and what linking it implies | Maintenance (2026-11) | History | Realtime and catch-up | Contacts | Actions | Fit for us |
|---|---|---|---|---|---|---|---|---|---|
| **whatsmeow** | WA | Go library | MPL-2.0, plus a GPL-3.0 dependency, so a linked binary is GPL-3.0 | ~7.8k★, commits weekly, no tags | bootstrap, recent (~3 mo), full (~1 yr; up to ~3 yr with the day limit), on demand (50 per page, phone online) | websocket; offline queue (30 d); `SynchronousAck`; decrypted buffer | app state (address book), push and business names, LID map, avatars | all: send, media, reply, react, edit (20 min), revoke, read, presence, typing | ★★★★★ |
| **mautrix-whatsapp** | WA | Go, bridgev2 Matrix bridge | AGPL-3.0 | ~1.9k★, monthly | as whatsmeow, plus a backfill queue and media retry | as whatsmeow | as whatsmeow, plus templates | all | ★★★ (reference, or if bridgev2 is chosen) |
| **Baileys** | WA | TS library (Node) | MIT, plus a GPL-3.0 dependency | ~11.6k★, v7 still RC | `syncFullHistory`, `fetchMessageHistory` (50) | websocket; your own key store | contacts and chat events; LID store (v7) | all | ★★ (wrong runtime) |
| **whatsapp-web.js** | WA | JS + Chromium | Apache-2.0 | ~22.9k★, active | what WA Web has loaded | browser session | yes | most | ★ |
| **WAHA** | WA | Docker REST; four engines | Apache-2.0 (GOWS is a binary) | ~7.8k★, monthly; free since 2026.6 | engine store, plus GOWS full-sync settings | webhooks | contacts, LID endpoints | most, varies by engine | ★★½ |
| **Evolution API / Go** | WA | Docker REST | Apache-2.0 plus conditions; license heartbeat (Go) | ~10.1k★ / ~1k★ | engine store | webhooks, queues | yes | most | ★ |
| **GOWA** | WA | Go binary: REST, MCP, webhooks | MIT, plus the GPL dependency | ~5.0k★, weekly | stored history sync | HMAC webhooks, 5 retries | yes | all common ones | ★★★ (ready-made fallback) |
| **wuzapi** | WA | Go binary: REST, webhooks | MIT, plus the GPL dependency | ~1.0k★, monthly | stored | webhooks, RabbitMQ dead-letter queue | yes | common ones | ★★½ |
| **whatsapp-rust** | WA | Rust library | MIT (declared all the way down) | ~1.0k★, active | yes (port) | yes | yes | all, plus calls | ★★ (escape from GPL) |
| **whatsapp-mcp (lharries)** | WA | Go + Python MCP | MIT | ✖ stale since 2025-07, old whatsmeow | local SQLite | — | basic | send, files | ✖ |
| **signalmeow (mautrix-signal)** | Signal | Go library + Rust FFI | AGPL-3.0 (and libsignal AGPL) | ~0.7k★, monthly | **link and sync: all chats + 45 d of media** | websocket; acks on handler success; event buffer; queue 45 d | storage service, contact sync, profiles, CDSI | all: send, media, quote, react, edit, delete, receipts, typing | ★★★★½ |
| **signal-cli** | Signal | Java CLI / daemon (JSON-RPC, SSE, D-Bus) | GPL-3.0 (separate process: fine) | ~5.0k★, monthly | ✖ none | daemon; manual receive; SSE replay of 1000 in memory | storage sync, `listContacts`, avatars | all, as explicit commands | ★★★★ |
| **signal-cli-rest-api** | Signal | Docker REST around signal-cli | MIT, plus GPL signal-cli | ~2.9k★, active | ✖ | websocket in json-rpc mode | yes | all | ★★★ (prototyping) |
| **presage** | Signal | Rust library | AGPL-3.0 | ~0.3k★; last release 2025-05 | ✖ (#396) | stream; `QueueEmpty` | contact sync, profiles | most | ★½ |
| **libsignal** | Signal | Rust core | AGPL-3.0, "unsupported outside Signal" | weekly | — | — | — | — | ✖ (building block) |
| **signald** | Signal | Java daemon | GPL-3.0 | ✖ dead (2023) | — | — | — | — | ✖ |
| **Cloud API** | WA | official HTTPS | Meta terms | — | ✖ (coexistence: 6 mo, Business app only) | webhooks | ✖ | templates, 24 h window | ✖ (wrong product) |

---

## 8. Ranked recommendations

### WhatsApp

1. **whatsmeow, embedded in our own Go sidecar.** Give the sidecar its own
   GPL-3.0-or-later licence, as a separate program talking HTTP to `substrated`.
   - **Why.** It is the reference implementation the whole ecosystem stands on:
     it gets protocol fixes first (version bumps weekly, LID migration, passkey
     pairing) and is in Go.
   - **Every requirement is met.**
     - History: full sync set before pairing, plus 50-message pages on demand.
     - Realtime catch-up: an offline queue of 30 days, `SynchronousAck` and the
       event buffer.
     - Contacts: app state plus the LID map.
     - Every action: a single method each.
   - **Read mautrix-whatsapp for the edge cases:** media retry, app-state
     recovery, the phone-offline ping and LID migration.
2. **mautrix-whatsapp's connector,** if the framework report chooses bridgev2.
   It solves those edge cases out of the box, at the cost of the AGPL and the
   bridge's shape.
3. **GOWA, or WAHA with GOWS,** as a ready-made daemon, if we decide not to own
   WhatsApp code.
   - Run it as a container and point its webhooks at substrate; drive actions
     through its REST API.
   - Accept that delivery is weaker after outages longer than about 30 s of
     webhook retries (reconcile from its store), and that its API is a third
     party's.
4. **Baileys or whatsapp-rust,** only for a sidecar not written in Go, or (for
   whatsapp-rust, MIT) to escape the GPL entirely.

**Not recommended:**

- whatsapp-web.js: Chromium, and brittle.
- Evolution: licence conditions, a heartbeat, and built for businesses.
- lharries/whatsapp-mcp: unmaintained.
- The Cloud API: a different product, built for business numbers.

### Signal

1. **signalmeow, embedded in a Go sidecar under the AGPL-3.0.**
   - **Why.**
     - It is the only maintained client that receives Signal's link-and-sync
       history (all chats plus 45 days of media).
     - It is in Go.
     - It acks only on handler success and buffers decrypted events.
     - Its storage-service sync covers contacts.
     - Beeper funds its upkeep through mautrix-signal, with releases every
       month.
   - **The costs.**
     - The AGPL.
     - A Rust and libsignal-FFI build.
     - No stable API promise: pin a mautrix-signal release and upgrade every
       month, within Signal's 3-month window.
2. **The signal-cli daemon** (HTTP JSON-RPC plus SSE, `--receive-mode manual`),
   with a thin ingestion sidecar.
   - Choose it if we would rather have a documented, CLI-stable interface and
     can live without history, or with a one-off export from Signal Desktop.
   - Lower integration risk and a bigger community; the cost is a JVM (or the
     native build) and a short window between ack and consumer.
3. **signal-cli-rest-api,** for prototypes only.

**Not recommended:** presage (no history, Rust, AGPL), libsignal directly,
signald (dead), and the MCP servers.

**Decide before the first link.** The Signal history transfer happens once, at
link time, and only to a client that advertised `backup5` and asked for it. A
device linked with signal-cli cannot receive it later. Moving to signalmeow
means linking again and choosing "Transfer Message History".

---

## 9. Unverified or open items

- **Linked-device inactivity on WhatsApp.** Which rule is current: a companion
  disconnected after 30 days of its own inactivity, or only the 14-day phone
  rule (both appear on faq.whatsapp.com)?
- **WhatsApp's remaining limits:** the time limit on revoking (about 2 days?),
  how long personal media stays on its servers, and whether a LID can ever
  change.
- **Passkey pairing:** how often WhatsApp requires it for companions, and how a
  headless sidecar should relay the WebAuthn step.
- **History depth by device type:** how much more history a desktop-type
  companion receives than a web-type one, and the real ceiling of
  `FullSyncDaysLimit` (mautrix says "around 3 years").
- **The engines and libraries I did not read:**
  - WAHA: how GOWS and its webhooks retry or acknowledge;
  - whatsapp-web.js: whether it supports pairing codes, and how deep its history
    goes;
  - whatsapp-rust: where its Signal crate's code came from.
- **Signal:** whether contact sync has moved into the backup system (per the
  presage maintainer); whether signalmeow has any captcha flow for rate-limit
  challenges; and whether Signal bans linked third-party clients in practice
  (none reported).
- **WhatsApp Coexistence:** confirming that personal WhatsApp accounts are
  ineligible.
