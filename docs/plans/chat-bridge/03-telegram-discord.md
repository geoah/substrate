# Telegram and Discord as the user: connector options

Research for the substrate messenger-provider proposal. Researched 2026-11-09.
Scope: network-specific libraries and behaviour for Telegram (user account over
MTProto) and Discord (user account). The mautrix bridgev2 *framework* is
covered by a sibling report. This report treats mautrix-telegram and
mautrix-discord only as connectors and reference code.

Every star count, release and date below was read from the GitHub API, PyPI,
npm or Codeberg on 2026-11-09 unless a source is linked. Claims marked
**(unverified)** could not be confirmed from a primary source.

---

## 0. Conclusions first

**Telegram: technically clean, contractually tense.**

- Telegram publishes a client API for third-party user clients
  ([core.telegram.org/api](https://core.telegram.org/api)). Full cloud history,
  gap-free catch-up, contacts and every action we need are first-class API
  methods.
- The best fit is a small Go sidecar on **gotd/td** (MIT). It is mature: Beeper's
  own Telegram bridge, mautrix-telegram, was rewritten in Go on a gotd fork in
  April 2026, and that fork is MIT-licensed too.
- Two hard rules shape the architecture:
  1. **One process per logged-in session.** Telegram kills a session that sends
     requests from two main connections at once (`AUTH_KEY_DUPLICATED`). The user
     must then log in again. So every send, react or fetch has to go through the
     process that owns the session. Short-lived functions cannot open their own
     connection with the same key while the sidecar runs.
  2. **Persist before you advance.** gotd's updates manager moves the stored
     `pts` forward even when the handler fails. The sidecar must write each update
     to a durable outbox before its handler returns.
- **The contractual problem is new and real.** Since about February 2026,
  Telegram's API Terms (§1.5) and its Content Licensing terms prohibit using
  Telegram data to "engage in the … deployment of artificial intelligence". The
  only stated exception requires explicit, per-chat consent from *all* relevant
  users. Feeding mirrored Telegram chats into the owner's AI assistant (Moss) is
  plausibly inside that prohibition. **That is a product and legal decision for
  the owner, not a technical one.**
- Who registers `api_id`: the API Terms say "you must obtain your own api_id for
  your application". Self-hosted multi-user software (mautrix-telegram) has the
  **operator** register one app at my.telegram.org and use it for every user.
  Supporting an operator default plus a per-repository override is the
  pragmatic answer.

**Discord: technically well trodden, contractually forbidden.**

- Discord's position is unambiguous: "Automating normal user accounts
  (generally called 'self-bots') outside of the OAuth2/bot API is forbidden, and
  can result in an account termination if found"
  ([support article 115002192352](https://support.discord.com/hc/articles/115002192352)).
- **There is no sanctioned way to read a user's own DMs.**
  - `dm_channels.read` and `rpc` are "only available to approved partners".
  - The Social SDK's DM history is capped at 200 messages and 72 hours, and only
    between two users who both played the game.
  - The GDPR data package contains only the messages *you* sent, and can take up
    to 30 days.
- Every user-account connector — Beeper's included — runs on a user token and
  mimics the web client.
- **If the owner accepts the risk:** build a Go sidecar on **beeper/discordgo**
  (BSD-3). It is the discordgo fork that Beeper's Discord bridge uses. Keep it
  gateway-first and REST-light, and model its behaviour on mautrix-discord's
  bridgev2 rewrite (AGPL; read it, don't copy it).
- **Regardless of that choice:** add a data-package importer as the zero-risk
  history source. Keep Beeper as the fallback path.

---

## 1. What every option was measured against

These are the requirements from the brief, plus two constraints from substrate
itself.

| Requirement             | Detail |
| ----------------------- | ------ |
| History                 | As far back as the network allows |
| Near-realtime + catch-up | No lost messages across downtime |
| Contacts / roster       | Names, handles, phone numbers, avatars |
| Actions as the user     | Send text and media, reply/quote, react, edit, delete/unsend, mark read, typing |
| Runtime shape           | Provider code runs as short-lived sandboxed Python functions (30 s – 2 min, network allowlist). Anything holding a connection or session runs as a long-lived **sidecar** that POSTs event batches to a server webhook. That is the pattern of `cmd/slack-socket`, which acks upstream only after the webhook returns `202`. |
| Licensing               | The repo will be public. See §1.1. |

### 1.1 Licensing, once

| License | Examples | Embed (link) in substrate | Separate process |
| ------- | -------- | ------------------------- | ---------------- |
| MIT, BSD-3-Clause, ISC, Apache-2.0 | gotd, Telethon, mtcute, beeper/discordgo, arikawa, discord.py-self | Fine; keep notices. Apache-2.0 also carries NOTICE and patent terms. | Fine |
| **BSL-1.0 = *Boost* Software License** | TDLib, TDLight | Permissive; fine | Fine |
| LGPL-3.0 | Kurigram, Hydrogram | Importing an unmodified Python lib is generally fine. Changes to the lib stay LGPL. | Fine |
| GPL-3.0 | discord.js-selfbot-v13, Dissent | Linking makes the combined work GPL | Fine |
| AGPL-3.0 | mautrix-telegram, mautrix-discord, MadelineProto, iyear/tdl | Linking, or copying code in, makes substrate AGPL. Network users must then be offered the source. | Generally treated as an aggregate. §13 obliges you to offer source *of that program* (and your modifications) to users who interact with it over a network. |

Two notes on the table:

- **BSL here is the Boost license, not the Business Source License.** None of the
  Telegram or Discord options found uses the Business Source License.
- **mautrix exceptions do not cover us.** Both mautrix repos carry a
  `LICENSE.exceptions` granting extra rights *only to Beeper and Element*
  ([telegram](https://github.com/mautrix/telegram/blob/main/LICENSE.exceptions)).

---

## 2. Telegram

### 2.1 Platform rules that bind any connector

**api_id / api_hash** ([obtaining_api_id](https://core.telegram.org/api/obtaining_api_id))

- **How you get one:** sign up with an official app, log in at my.telegram.org,
  then fill in "API development tools". "For the moment each number can only
  have one api_id connected to it."
- **Who gets notified:** "We will be sending important developer notifications to
  the phone number that you use in this process."
- **Monitoring:** "Due to excessive abuse of the Telegram API, all accounts that
  log in using unofficial Telegram API clients are automatically put under
  observation."
- **Sample ids:** the sample id shipped with Telegram's own open-source code
  "is limited on the server side … using it for anything but testing purposes
  will result in the `API_ID_PUBLISHED_FLOOD` error". gotd's guide adds that an
  app's id and hash "cannot be regenerated and are bound to your Telegram
  account" ([gotd SUPPORT.md](https://github.com/gotd/td/blob/main/.github/SUPPORT.md)).
- **For our public repo:** never commit an api_id or hash.

**API Terms of Service** ([core.telegram.org/api/terms](https://core.telegram.org/api/terms))

- **§1.4** forbids "making actions on behalf of the user without the user's
  knowledge and consent". It also forbids "preventing last seen and online
  statuses from being displayed correctly, tampering with the 'read' statuses of
  messages (e.g. implementing a 'ghost mode'), preventing typing statuses from
  being sent/displayed".
  - *Implication:* assistant-initiated sends need the user's consent.
  - *Implication:* when the human reads a chat in substrate's console, substrate
    should mark it read. A background mirror reading history is not "the user
    viewing", so it should not mark read.
- **§1.5** is new: "you are prohibited from using, accessing or aggregating data
  obtained from the Telegram platform to train, fine-tune or otherwise engage in
  the development, enhancement or deployment of artificial intelligence, machine
  learning models and similar technologies."
- **§2.1** "You must obtain your own api_id for your application."
- **§2.3** The app title must not include "Telegram" unless it is preceded by
  "Unofficial".
- **§3.3** "If your app allows accessing content from Telegram channels, you must
  include support for official sponsored messages." mautrix-telegram calls
  `messages.getSponsoredMessages` / `viewSponsoredMessage` for this. The simplest
  compliance for us is to not mirror broadcast channels, or to mirror them with
  sponsored messages.
- **§4** Breach notices go to the account that owns the api_id. If the app is not
  fixed within 10 days, its API access is discontinued.

**Content Licensing and AI** ([telegram.org/tos/content-licensing](https://telegram.org/tos/content-licensing))

- Access to user content is allowed only for "ordinary, legitimate, and intended
  use". A "legitimate third-party Telegram Client" may access what it needs, on a
  "retractable, limited, non-exclusive … basis solely to the extent strictly
  required to operate the relevant service".
- Under "Large Language Models and AI", Telegram "firmly prohibits the scraping,
  indexing, harvesting, aggregation or use of data obtained from its platform to
  train, fine-tune, validate or otherwise engage in the development,
  enhancement, benchmarking or deployment of artificial intelligence".
- The exception requires that "all relevant users individually provide explicit,
  informed, affirmative and continued consent that is strictly limited to the
  specific content and chat".
- Open Terms Archive dated this change to **2026-02-03**, with "a similar
  modification … in the developer terms"
  ([memo](https://opentermsarchive.org/en/memos/telegram-prohibits-collecting-data-for-ai-use/)).

> **Implication for substrate (an interpretation, not legal advice):** mirroring
> one's own chats into a personal data store is "operating a client". Handing
> them to an LLM assistant is arguably "deployment of AI" with platform data, and
> the consent bar (every participant, per chat) is not something a personal
> assistant can meet for group chats. Options to put to the owner:
> - keep Telegram content out of agent context by default, with a per-chat opt-in;
> - accept the risk knowingly;
> - or restrict the assistant to actions the user explicitly dictates.

**Operator vs. user registering the app**

- **Operator registers one app.** The Terms bind the *application developer*.
  Self-hosted multi-user software takes this route: mautrix-telegram's config
  says `# Get your own API keys at https://my.telegram.org/apps`
  ([example-config](https://github.com/mautrix/telegram/blob/main/pkg/connector/example-config.yaml)).
  - Every user's session then shows the operator's app name in Settings →
    Devices.
  - A breach notice for any user's abuse goes to the operator's phone, and
    revoking that api_id cuts off every user.
- **Each user registers their own.** This is the norm for single-user tools
  (Telethon scripts, the MCP servers in §2.3.7).
  - It isolates risk.
  - It adds friction: a my.telegram.org form, tied to the user's phone, one app
    per number.
- **Recommendation:** an operator-level default plus a per-repository override.
  In a one-person deployment the two coincide.

### 2.2 Protocol facts that shape any Telegram connector

#### Login ([api/auth](https://core.telegram.org/api/auth), [api/qr-login](https://core.telegram.org/api/qr-login))

**Phone flow**

1. `auth.sendCode(phone, api_id, api_hash, codeSettings)` returns
   `auth.sentCode{type, phone_code_hash, next_type, timeout}`, or
   `auth.sentCodeSuccess` when a stored *future auth token* matches.
2. `auth.signIn(phone, phone_code_hash, code)`.
3. If that returns `SESSION_PASSWORD_NEEDED`: `account.getPassword`, then
   `auth.checkPassword(InputCheckPasswordSRP)`. The password is never sent;
   gotd and Telethon implement the SRP math.

**Where the code arrives**

- Third-party apps usually get it as a Telegram service message in the user's
  other logged-in apps (`sentCodeTypeApp`).
- Telegram: "only mobile official apps can make use of Firebase SMS
  authentication … in some conditions, only the official applications can
  receive a login/signup code via SMS". Third parties may ask for SMS via
  sms@telegram.org with `#enableSMS`.
- mautrix's docs put it bluntly: "Telegram will not send the code over SMS. You
  need to have an official Telegram client already logged in"
  ([mautrix docs](https://github.com/mautrix/docs/blob/master/bridges/go/telegram/authentication.md)).
- Other code types: `sentCodeTypeEmailCode`, `sentCodeTypeFragmentSms`, and
  `sentCodeTypeSetUpEmailRequired`. The last one forces an email-verification
  sub-flow; mautrix-telegram refuses it with `ErrLoginEmailRequired`.
- Official apps may also get `auth.sentCodePaymentRequired`, Premium-gated
  logins in expensive SMS markets.

**Other login rules**

- Registration from third-party clients was discontinued on 2023-02-18
  (mautrix docs). The user must already have an account.
- Each phone number gets "a certain number of login attempts per day (e.g. 5)".
- Telegram invalidates a login code if it is forwarded or pasted into any chat.
  The console must never ask the user to send the code through Telegram.

**QR flow**

1. `auth.exportLoginToken(api_id, api_hash, except_ids)` returns
   `auth.loginToken{expires (~30 s), token}`.
2. Render `tg://login?token=<base64url>` as a QR code.
3. The user scans it in an official app (Settings → Devices → Link Desktop
   Device), which calls `auth.acceptLoginToken`.
4. We receive `updateLoginToken` and call `auth.exportLoginToken` again. It
   returns `auth.loginTokenSuccess`, or `auth.loginTokenMigrateTo{dc_id, token}`,
   in which case we call `auth.importLoginToken` on that DC.
5. 2FA still applies (`SESSION_PASSWORD_NEEDED`).
6. QR tokens must be re-exported when they expire.

A passkey login flow also exists (`auth.initPasskeyLogin` /
`auth.finishPasskeyLogin`).

**Recommended console flow:** QR first, phone code as fallback, then a 2FA
password prompt. This is exactly mautrix-telegram's set
(`loginqr.go`, `loginphone.go`).

#### Session state, persistence, revocation

**What to store**

- The authorization is a 2048-bit **auth key per DC** plus the DC id. It is
  bearer-equivalent to full account access, so treat it like a password.
- Optionally store the *future auth tokens* returned on logout or login. They
  let a re-login skip the code.

**How the session appears**

- It shows in Settings → Devices as an `authorization{api_id, app_name,
  app_version, device_model, platform, system_version, ip, country, …}`.
- The device fields come from `initConnection`: gotd's `telegram.DeviceConfig`,
  TDLib's `setTdlibParameters`, Telethon's `device_model=…`.
- mautrix defaults to device model `mautrix-telegram`. **Name the session
  honestly** (e.g. "substrate").

**New sessions are "unconfirmed"**

- Other devices get `updateNewAuthorization{unconfirmed}` and are expected to ask
  "is this you?".
- The user confirms (`account.changeAuthorizationSettings{confirmed}`) or resets
  it (`account.resetAuthorization`). Otherwise it auto-confirms after
  `authorization_autoconfirm_period`.
- Expect the user to see a prompt on their phone at connect time.

**How it ends**

- The user terminates it in Devices, or logs out: `AUTH_KEY_UNREGISTERED` /
  `SESSION_REVOKED`.
- The account's inactive-session TTL (`account.setAuthorizationTTL`,
  `authorization_ttl_days`) ends it. An always-connected sidecar is never
  inactive.
- **The parallel-connection rule.** `AUTH_KEY_DUPLICATED` is "emitted if any of
  the non-media DC detects that an authorized session is sending requests in
  parallel from two separate TCP connections … the user must generate a new auth
  key and login again". Only `tmp_sessions > 1` permits parallel main sessions;
  media-DC transfer connections are exempt
  ([api/errors](https://core.telegram.org/api/errors)).
  **This is the single most important architectural constraint.**

**Frozen accounts** (since about mid-2025)

- Accounts "can be frozen for serious violations". They become read-only, and
  calls fail with `FROZEN_METHOD_INVALID` (420) or `FROZEN_PARTICIPANT_MISSING`.
- `help.getAppConfig` then exposes `freeze_since_date`, `freeze_until_date` and
  `freeze_appeal_url`. Frozen accounts are deleted unless an appeal succeeds
  ([api/auth#frozen-accounts](https://core.telegram.org/api/auth),
  [api/config](https://core.telegram.org/api/config)).
- The sidecar should surface this to the owner.

#### History

**Normal pagination**

- `messages.getHistory(peer, offset_id, offset_date, add_offset, limit,
  max_id, min_id, hash)` pages backwards, ≤100 per call (mautrix uses
  `chunkLimit = 100`).
- Cloud chats keep their full server-side history: private chats, basic groups,
  supergroups and channels.
- **Secret chats are device-local and unreachable from a new session.**
- Messages "deleted for everyone" are gone.
- The updates doc warns that "very old channel/supergroup messages may still be
  inaccessible".

**Throttling**

- No published per-method limits. You get `FLOOD_WAIT_X` when you exceed them.
- mautrix-telegram sleeps `min(requestCount*2, 15)` seconds between history
  batches when not using takeout (`pkg/connector/backfill.go`).

**Takeout (bulk export)** ([api/takeout](https://core.telegram.org/api/takeout))

1. `account.initTakeoutSession{contacts, message_users, message_chats,
   message_megagroups, message_channels, files, file_max_size}` returns
   `account.takeout{id}`.
2. Wrap *every* request in `invokeWithTakeout(id, …)`, uploads and downloads
   included.
3. Paginate dialogs and history per `messages.getSplitRanges` range, wrapped in
   `invokeWithMessagesRange`.
4. Finish with `account.finishTakeoutSession{success}`.

Takeout behaviour to plan for:

- Telethon documents that "some of the calls made through the takeout session
  will have lower flood limits"
  ([Telethon client docs](https://docs.telethon.dev/en/stable/modules/client.html)).
- Init may fail with `TAKEOUT_INIT_DELAY_%d`: "for security reasons, you will be
  able to begin downloading your data in %d seconds. We have notified all your
  devices about the export request." The user can approve it from a phone
  sooner; mautrix waits on a `takeoutAccepted` event and retries.
- A commonly reported ~24 h delay for fresh sessions is **(unverified)**.
- mautrix-telegram makes takeout opt-in (`takeout.dialog_sync`,
  `forward_backfill`, `backward_backfill`). It finishes the takeout after an hour
  idle (`stopTakeout`).

#### Realtime and catch-up ([api/updates](https://core.telegram.org/api/updates))

**How updates arrive**

- "Update events are sent to an authorized user into the last active
  connection". A client must call a method (e.g. `updates.getState`) to start
  receiving.

**What to track** — the sequences:

| Counter | Covers |
| ------- | ------ |
| `seq` | the `Updates` container |
| `pts` (common) | private chats and basic groups |
| `qts` | secret chats, some bot events |
| `pts` per channel/supergroup | that channel or supergroup |

**When to call `updates.getDifference`**

- on startup;
- on a gap not filled within 0.5 s;
- on a "new session created" notice;
- on undecodable or incomplete updates;
- after **15 minutes with no updates**;
- on `updatesTooLong`.

**How to page it**

- `getDifference` returns `differenceSlice` (save the intermediate state and
  repeat) or `differenceTooLong`.
- Channels catch up through `updates.getChannelDifference`. These calls are
  triggered by `updateChannelTooLong` in the common difference, not by polling
  every channel.
- Recommended page sizes: 1000–10000 for the common box, 10–100 per channel.

**How long you can be offline**

- Message boxes have fixed size: "usually … 100000 for channels and 5000000 for
  the common message box".
- Past that, you get `differenceTooLong` / `channelDifferenceTooLong`. Re-fetch
  state and fill the hole with `messages.getMessages` / `channels.getMessages`
  (up to 200 ids per call).
- **For a personal account, days or weeks of downtime are recoverable without
  loss** for the common box. Very busy supergroups can overflow.

**What catch-up recovers**

- New messages, edits (`updateEditMessage`) and deletions
  (`updateDeleteMessages`, `updateDeleteChannelMessages`) are pts-bearing, so
  `getDifference` recovers them.
- Read-state updates also arrive in the difference.
- Reaction changes on others' messages: an **(unverified)** expectation is that
  they are not reliably replayed. Re-fetching recent messages refreshes their
  `reactions` field.

**Channels that go quiet**

- For channels and supergroups, "the API may stop sending updates (or send fewer
  updates)". Clients should short-poll `getChannelDifference` only for channels
  the user is *viewing*, at most 10.
- A mirror should therefore treat big supergroups as best-effort realtime with
  periodic catch-up.

**Message ids**

- Private chats and basic groups share one per-account id sequence.
- Each channel/supergroup has its own sequence.
- Primary key: `(peer, id)`.

#### Contacts / roster

- `contacts.getContacts(hash)` returns `contacts.contacts{contacts:[{user_id,
  mutual}], saved_count, users:[User]}`.
- A `User` carries `id`, `access_hash`, `first_name`, `last_name`, `username` /
  `usernames`, `phone` (only when visible to you: own contacts, or the user
  shares it), `photo` and `status`.
- Avatars download through `upload.getFile` with a photo file location.
- `access_hash` must be stored. It is needed to address any peer
  (`InputPeerUser{user_id, access_hash}`).

#### Actions (raw method names)

| Action | Method |
| ------ | ------ |
| Send text | `messages.sendMessage{peer, message, random_id, reply_to: inputReplyToMessage{reply_to_msg_id, quote_text…}, entities}` |
| Send media | `upload.saveFilePart` / `saveBigFilePart`, then `messages.sendMedia` |
| Edit | `messages.editMessage` |
| Delete | `messages.deleteMessages{id, revoke}` (private, basic groups); `channels.deleteMessages` (supergroups) |
| React | `messages.sendReaction{peer, msg_id, reaction:[reactionEmoji{emoticon}]}` |
| Mark read | `messages.readHistory{peer, max_id}`; `channels.readHistory`; `messages.readMessageContents` for voice/video-note "listened" |
| Typing | `messages.setTyping{peer, action: sendMessageTypingAction / sendMessageCancelAction}` |
| Presence | `account.updateStatus{offline}` |

mautrix-telegram uses exactly these (`handlematrix.go`).

#### Rate limits and errors ([api/errors](https://core.telegram.org/api/errors))

| Error | Meaning |
| ----- | ------- |
| `FLOOD_WAIT_X` | Wait X seconds |
| `FLOOD_PREMIUM_WAIT_X` | Transfer throttling that Premium lifts |
| `SLOWMODE_WAIT_X` | Chat-specific slow mode |
| `PEER_FLOOD` | Account limited from messaging non-contacts; check @SpamBot (Telethon FAQ) |

Telegram publishes no quotas. gotd/contrib's `floodwait` clamps waits to
[1 s, 14 d] "matching TDLib".

#### Ban risk and how to behave like a person

**Sources**

- Telethon FAQ: anti-spam "has gotten more aggressive" since 2023. "Use the
  library only on well-established accounts (and not an account you just
  created)". VoIP numbers and some countries are riskier
  ([FAQ](https://docs.telethon.dev/en/stable/quick-references/faq.html)).
- gotd's guide: use it "passively (i.e. receive more than sending)", "do not use
  VoIP numbers", "implement a rate limiting system". If banned, write to
  recover@telegram.org.
- mautrix: "a brand new account using a 3rd party client is considered
  suspicious. Using a well-established account is perfectly safe."

**Practice for substrate**

- Refuse or strongly warn on fresh accounts.
- Never auto-join chats or message strangers in bulk.
- Pace sends: about 1 msg/s per chat is a reasonable **(unverified)** ceiling.
  Bursts to non-contacts trigger `PEER_FLOOD`.
- Set `account.updateStatus(offline=true)` while no human is active, so the
  mirror does not make the user look online 24/7.
- Send typing only while a human composes.
- Mark read only on user view or explicit command.
- Never resend the login code through Telegram.
- Keep one session, and do not reconnect in tight loops.

### 2.3 Options

#### 2.3.1 gotd/td + gotd/contrib (Go) — *recommended core*

**Project facts**

| Field | Value |
| ----- | ----- |
| URL | [github.com/gotd/td](https://github.com/gotd/td), [github.com/gotd/contrib](https://github.com/gotd/contrib) |
| Language / license | Go / MIT (both) |
| Maintenance | td v0.163.0 (2026-10-22), pushed 2026-11-08, ~2.4k★; contrib v0.25.0 (2026-07-15), pushed 2026-11-07. Pre-1.0 semver, frequent layer bumps. |
| Status | README badge says "stable": "Full MTProto 2.0 implementation … 150kb per idle client … can handle thousands concurrent clients". |
| Form | Library, embedded in a Go sidecar |

**Auth as the user**

- `auth.NewFlow(auth.Constant(phone, password, codeAuthenticator), auth.SendCodeOptions{})`.
- QR: `client.QR().Auth(ctx, qrlogin.OnLoginToken(dispatcher), show)`, then
  `client.Auth().Password(ctx, pwd)` on `SESSION_PASSWORD_NEEDED`.
- Session through `telegram.Options{SessionStorage: …}`, a pluggable
  `session.Storage` with `LoadSession` / `StoreSession`.
- contrib ships stores for bbolt, pebble, redis, s3 and vault, and a
  `telegram/takeout` helper.

**History**

- Raw `MessagesGetHistory`, pagination helpers in `telegram/query`.
- Takeout via `takeout.Run(ctx, client, takeout.Config{…}, fn)`
  ([example](https://github.com/gotd/td/blob/main/examples/takeout/main.go)).

**Realtime and recovery**

- `telegram/updates.Manager` implements the whole pts/qts/seq/channel-pts
  algorithm: getDifference, getChannelDifference, too-long handling.
- Pluggable `StateStorage` (`GetState`, `SetPts`, `SetQts`, `SetSeq`,
  `SetChannelPts`, `ForEachChannels`), `ChannelAccessHasher` and
  `UserAccessHasher`.
- Callbacks: `OnChannelTooLong`, `OnChannelGap` (with the gap range and the
  latest messages to backfill), `OnTooLong`, `OnLoadUserStateFailed`,
  `OnChannelInaccessible`.
- OpenTelemetry metrics for gaps
  ([config.go](https://github.com/gotd/td/blob/main/telegram/updates/config.go),
  [doc.go](https://github.com/gotd/td/blob/main/telegram/updates/doc.go)).

> **Caveat found in source:** `applyPts` dispatches to the handler and then calls
> `storage.SetPts` even if the handler returned an error (the error is only
> logged)
> ([state_apply.go](https://github.com/gotd/td/blob/main/telegram/updates/state_apply.go)).
> At-least-once delivery therefore requires the handler to persist to a local
> durable outbox *before returning*. It must not return an error and expect a
> retry.

**Contacts:** `ContactsGetContacts`, `UsersGetFullUser`, `PhotosGetUserPhotos`,
and the `downloader` package for avatars.

**Actions**

- Everything in §2.2, as generated `tg.Client` methods (`MessagesSendMessage`,
  `MessagesSendReaction`, `MessagesReadHistory`, `MessagesSetTyping` …).
- The `telegram/message` builders (text styling, replies, media) and the
  multi-stream `uploader` / `downloader`.
- `InvokeJSON` takes MTProto-JSON (`{"@type":"messages.sendMessage",…}`). That
  makes a generic "call any method" RPC endpoint for Python functions trivial.

**Interface sketch**

```go
d := tg.NewUpdateDispatcher()
gaps := updates.New(updates.Config{
    Handler: d, Storage: stateStore, AccessHasher: hashStore, // durable, per account
    OnChannelGap: func(g updates.ChannelGap) { backfillQueue.Add(g) },
})
client := telegram.NewClient(apiID, apiHash, telegram.Options{
    SessionStorage: sealedSession,          // auth key: secret material
    UpdateHandler:  gaps,
    Device:         telegram.DeviceConfig{DeviceModel: "substrate", AppVersion: version},
    Middlewares: []telegram.Middleware{
        updhook.UpdateHook(gaps.Handle),
        updhook.AffectedHook(gaps), // keep pts in sync after our own reads/deletes
        floodwait.NewSimpleWaiter().WithMaxRetries(3),
        ratelimit.New(rate.Every(150*time.Millisecond), 3),
    },
})
d.OnNewMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
    return outbox.Append(ctx, "message.new", u, e) // fsync before return; a shipper POSTs batches
})
d.OnEditMessage(...); d.OnDeleteMessages(...); d.OnNewChannelMessage(...); d.OnReadHistoryInbox(...)
client.Run(ctx, func(ctx context.Context) error {
    self, err := client.Self(ctx)
    if err != nil { return err }
    _, _ = client.API().AccountUpdateStatus(ctx, true) // appear offline
    return gaps.Run(ctx, client.API(), self.ID, updates.AuthOptions{})
})
// action endpoint (sidecar RPC), e.g. reply:
client.API().MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
    Peer: peer, Message: text, RandomID: rid,
    ReplyTo: &tg.InputReplyToMessage{ReplyToMsgID: replyID},
})
```

**Risks:** Telegram ToS as in §2.1. gotd is a third-party client, with the same
ban profile as any.

**Recommendation:** the core of a Telegram sidecar. Read mautrix-telegram's
connector as the reference for edge cases (it is AGPL: read, don't copy).

#### 2.3.2 mautrix-telegram (Go, bridgev2) — *best reference implementation*

**Project facts**

| Field | Value |
| ----- | ----- |
| URL | [github.com/mautrix/telegram](https://github.com/mautrix/telegram) |
| Language / license | Go / AGPL-3.0, plus exceptions for Beeper and Element only |
| Maintenance | ~1.76k★. **Rewritten in Go on bridgev2 in v26.04 (2026-04-16)**: "Rewrote bridge in Go using bridgev2 architecture" ([CHANGELOG](https://github.com/mautrix/telegram/blob/main/CHANGELOG.md)). Latest **v0.2610.0 / v26.10 (2026-10-16)**, monthly releases, API layer 228 as of v26.08. The legacy Python bridge is frozen at the `python-final` tag; v0.15.3 (2025-07-16) was its last release. Copyright headers name Sumner Evans. |
| Form | Matrix bridge daemon; Docker images at mau.dev |

**The vendored gotd fork is MIT**

- `pkg/gotd` is a vendored fork of gotd. Its README: "Everything under this
  directory is licensed under MIT like upstream gotd."
- Fixes from production at Beeper scale land there first: DC-transfer
  deadlocks, channel-diff deadlocks, auth-key reconnect.
- **That fork can be used by us without AGPL.**

**Auth as the user**

- Phone, QR or bot login. The device info is configurable. One operator api_id
  for all users.
- Session and update state live in the bridge DB:
  `CustomSessionStorage: &UserLoginMetadata.Session`, and the updates manager
  `Storage` is a scoped store.

**History**

- Forward and backward backfill through `messages.getHistory` in chunks of 100,
  with throttling.
- Optional takeout, which handles `TAKEOUT_INIT_DELAY`.

**Realtime and recovery**

- gotd's updates manager.
- `OnChannelTooLong` queues a chat resync with backfill.
- Ping every 30 s (configurable).

**Contacts:** member-list sync up to 10 000 per chat (server cap). Configurable
`contact_names` and `contact_avatars`.

**Actions:** send/edit/delete/react/read/typing/mentions read, block and unblock,
sponsored messages.

**Roadmap gaps:** presence, pinning, polls from Telegram, secret chats
([ROADMAP.md](https://github.com/mautrix/telegram/blob/main/ROADMAP.md)).

**Interface:** Matrix (appservice) plus bridgev2 provisioning HTTP for login.

**Risks**

- AGPL if linked or copied.
- Running it as a separate process implies running it the bridgev2 way. Whether
  that is viable without Matrix is the sibling report's subject.

**Recommendation**

- Use as the reference for behaviour and edge cases.
- Use its MIT `pkg/gotd` fork if upstream gotd lags on a fix.
- Run it as a separate process only if the framework report concludes bridgev2
  is the integration path.

#### 2.3.3 TDLib (tdjson) and Go bindings

**Project facts**

| Field | Value |
| ----- | ----- |
| URL | [github.com/tdlib/td](https://github.com/tdlib/td) |
| Language / license | C++ / BSL-1.0 (Boost) |
| Maintenance | **1.8.70 (version bump 2026-11-08)**, ~9.3k★, maintained by Telegram. No GitHub releases; versions are commits. |
| Form | Native library with a JSON C API: `td_create_client_id`, `td_send(client_id, json)`, `td_receive(timeout)`, `td_execute(json)` ([td_json_client.h](https://github.com/tdlib/td/blob/master/td/telegram/td_json_client.h)) |

**Go bindings**

- [zelenin/go-tdlib](https://github.com/zelenin/go-tdlib) (MIT, ~540★): pushed
  2026-05-09, pinned to a TDLib commit of 2026-05-08, so it **lags ~6 months**.
  Requires cgo and a self-built TDLib.
- Arman92/go-tdlib (GPL-3.0): stale since 2022.
- c0re100/go-tdlib: archived.

**Auth as the user** — a state machine driven by `updateAuthorizationState`:

1. `authorizationStateWaitTdlibParameters` → `setTdlibParameters{database_directory,
   use_message_database, api_id, api_hash, device_model, …}`
2. `WaitPhoneNumber` → `setAuthenticationPhoneNumber`, *or*
   `requestQrCodeAuthentication` → `WaitOtherDeviceConfirmation{link}`
3. `WaitEmailAddress` / `WaitEmailCode` (when required)
4. `WaitCode` → `checkAuthenticationCode`
5. `WaitPassword` → `checkAuthenticationPassword`
6. `Ready`

There is also `WaitPremiumPurchase` (paid auth). The session lives in TDLib's
SQLite database directory, optionally encrypted.

**History:** `getChatHistory(chat_id, from_message_id, offset, limit,
only_local)`. TDLib may return fewer than `limit`, so repeat.

**Realtime and recovery**

- Automatic. TDLib implements the whole update algorithm and persists it.
- New messages fetched by its getDifference are emitted as `updateNewMessage`.
- **Caveat (unverified):** if the app crashes after TDLib persisted state but
  before the app processed an update, TDLib will not redeliver it. Reconcile
  from the last stored message id with `getChatHistory` at startup.

**Contacts:** `getContacts`, `getUser`, `getUserFullInfo`,
`getUserProfilePhotos`.

**Actions:** `sendMessage{reply_to: inputMessageReplyToMessage{message_id,
quote}}`, `editMessageText`, `deleteMessages{revoke}`, `addMessageReaction` /
`setMessageReactions`, `viewMessages{force_read}` (marks read),
`sendChatAction{chatActionTyping}`, and `setOption("online", false)`.

```json
{"@type":"sendMessage","chat_id":123,"reply_to":{"@type":"inputMessageReplyToMessage","message_id":456},
 "input_message_content":{"@type":"inputMessageText","text":{"@type":"formattedText","text":"hi"}}}
```

**Risks**

- A heavy C++ build: multi-GB RAM for compilation, long build times
  **(unverified figures)**. cgo in the sidecar.
- A large local database that duplicates what substrate stores.

**Recommendation:** the most "official" semantics for free. Choose it only if
we want Telegram's own client logic and accept the build and cgo cost. gotd
gets the same outcome in pure Go.

#### 2.3.4 Telethon (Python)

**Project facts**

| Field | Value |
| ----- | ----- |
| URL | **Moved to [codeberg.org/Lonami/Telethon](https://codeberg.org/Lonami/Telethon)**. GitHub is archived (2026-02-21, "Migrate off GitHub", ~12k★ there). |
| Language / license | Python / MIT |
| Maintenance | PyPI **1.45.0 (2026-09-10)**. Codeberg commits through 2026-11-05 ("Update to layer 233"). Maintainer: Lonami. v1 is the maintained branch; a v2 branch exists. |
| Form | Library |

**Auth as the user**

- `client.start(phone=…, password=…)`, or `send_code_request` / `sign_in`.
- QR via `client.qr_login()`.
- Sessions: `StringSession` (auth key only) or a SQLite session.
- Only the SQLite session also stores update state and the entity cache.
  chigwell/telegram-mcp notes that with StringSession "after a restart, a busy
  account can … receive a large backlog".

**History:** `iter_messages(entity, min_id=…, reverse=True, wait_time=…)`, and
`client.takeout(...)` returning a client "behind a takeout session"; it raises
`TakeoutInitDelayError`.

**Realtime:** event handlers. `catch_up()` uses persisted state. Raw
`functions.updates.GetDifferenceRequest` is available for manual catch-up.

**Contacts:** `functions.contacts.GetContactsRequest(hash=0)`.

**Actions:** `send_message(…, reply_to=)`, `send_file`, `edit_message`,
`delete_messages(revoke=True)`, `SendReactionRequest`,
`send_read_acknowledge(max_id=)`, and `async with client.action(peer,
'typing')`.

```python
client = TelegramClient(StringSession(saved), api_id, api_hash, device_model="substrate")
await client.connect()
diff = await client(functions.updates.GetDifferenceRequest(pts=s.pts, date=s.date, qts=s.qts, pts_total_limit=1000))
await client.send_message(peer, "on my way", reply_to=msg_id)
await client(functions.messages.SendReactionRequest(peer=peer, msg_id=msg_id,
             reaction=[types.ReactionEmoji(emoticon="👍")]))
```

**Recommendation**

- Good inside substrate's **Python functions** for a no-sidecar MVP: a
  scheduled function runs `getDifference` from stored state, plus action
  functions.
- Any two functions for the same account must be **serialized** (one lock per
  account), or they risk `AUTH_KEY_DUPLICATED`.
- Alternatively, give poll and action functions *separate* authorizations: two
  logins, both visible in Devices.
- Latency equals the poll interval. How Telegram treats a session that
  reconnects every few minutes is **(unverified)**.

#### 2.3.5 Pyrogram and forks (Python, LGPL-3.0)

| Project | Status |
| ------- | ------ |
| [pyrogram/pyrogram](https://github.com/pyrogram/pyrogram) | **Archived** (Dec 2024). README: "The project is no longer maintained or supported." Last PyPI 2.0.106 (2023-04). |
| [Kurigram](https://github.com/kurigram-org/kurigram) | Active: **v2.2.27 released 2026-11-09**, ~850★, LGPL-3.0. The most current fork. |
| [Hydrogram](https://github.com/hydrogram/hydrogram) | Commits on `dev` through 2026-10-29, but **no PyPI release since 0.2.0 (2024-06-30)**. |
| [Pyrofork](https://github.com/Mayuri-Chan/pyrofork) | **Archived** (2026-07). Last PyPI 2.3.69 (2025-12). |

- Same capabilities as Telethon: QR and phone login, SQLite or string sessions,
  `get_chat_history`, `read_chat_history`, `send_reaction`, `send_chat_action`,
  `get_contacts`.
- LGPL is fine to import from a separate function or sidecar.
- **Recommendation:** no advantage over Telethon for us. If a Pyrogram-style
  API is wanted, Kurigram is the only well-maintained choice.

#### 2.3.6 JavaScript: GramJS / teleproto, mtcute (MIT)

- [GramJS](https://github.com/gram-js/gramjs) is **archived**: "Development
  continues in [teleproto]". npm `telegram` was last published 2.26.22
  (2025-02-12).
- [teleproto](https://github.com/sanyok12345/teleproto) (MIT, ~300★): npm
  1.233.0 published 2026-11-09.
- [mtcute](https://github.com/mtcute/mtcute) (MIT, ~590★, v0.32.7 2026-10-23):
  modern TypeScript, Node/Bun/Deno/web, with an updates manager and storage
  backends.
- **Recommendation:** only relevant if a sidecar were written in TypeScript.
  Not our case.

#### 2.3.7 Telegram MCP servers (daemons with an API)

**[chigwell/telegram-mcp](https://github.com/chigwell/telegram-mcp)**

| Field | Value |
| ----- | ----- |
| Language / license | Python (Telethon) / Apache-2.0 |
| Maintenance | v3.3.12 (2026-11-08), ~2.0k★ |
| Transports | stdio, **streamable HTTP** (`MCP_TRANSPORT=http`, `/mcp` on 127.0.0.1:8765, Host/Origin checks) and SSE. Docker supported. |
| Tools | 80+: chats, messages (send, schedule, edit, delete, forward, pin, mark read, reply, reactions, polls, inline buttons), `export_chat_history`, `export_unread_messages`, contacts, folders, voice transcription. `TELEGRAM_EXPOSED_TOOLS=read-only[+tool,…]` gating. |
| Auth | API id/hash plus a session string generated by its script (QR or phone). It recommends a persistent SQLite session for long-running servers. |

- **Realtime:** an in-memory, bounded per-message event log
  (`TELEGRAM_EVENT_LOG_SIZE`, default 2000) with `wait_for_events(after_seq,
  boot)` long-poll and `get_messages_after(chat_id, after_id)`. On `reset` or
  `gap` the consumer must "read every chat you follow from your last message
  id".
- It advises one shared HTTP server rather than one per client, because
  "Telegram throttles and may flag accounts that open many parallel sessions".
- **Fit:** fastest path to *assistant actions* via MCP. Weak as a durable mirror,
  because its event log is in-memory. Apache-2.0 is fine as a separate process.

**[chaindead/telegram-mcp](https://github.com/chaindead/telegram-mcp)**

- Go, gotd v0.121, MIT, ~350★, last push 2026-05-28.
- Only 5 tools: me, dialogs, mark read, read dialog, send *draft*.
- Too thin.

**Others found**

- mcp-telegram/mcp-telegram: TS, MIT, claims 182 tools, ~70★.
- fast-mcp-telegram: Python, MIT, ~50★.
- sparfenyuk/mcp-telegram and dryeab/mcp-telegram: no pushes since early or
  mid 2025.

**Recommendation:** use chigwell's as a stopgap "action surface for Moss" if the
§2.1 AI-terms question is resolved. Not as the sync engine.

#### 2.3.8 TDLight Telegram Bot API, "user mode" (C++, BSL-1.0)

- [tdlight-team/tdlight-telegram-bot-api](https://github.com/tdlight-team/tdlight-telegram-bot-api):
  v10.2 (2026-07-14), ~170★, Docker.
- An HTTP daemon with "experimental user support" behind `--allow-users`:
  1. `POST /userlogin {phone_number}` returns a `user_token`;
  2. `/user{token}/authcode`;
  3. `/user{token}/2fapassword`;
  4. then Bot-API-shaped `/user{token}/sendMessage`, `getUpdates` or webhook.
- **No history method** in its Bot-API surface. User mode is "experimental".
- **Recommendation:** not suitable (no backfill). Mentioned because it is the
  only ready-made *HTTP* user-session daemon.

#### 2.3.9 Also seen, not recommended

- **MadelineProto** (PHP, AGPL-3.0, 8.21.1 on 2026-11-08).
- **[iyear/tdl](https://github.com/iyear/tdl)** (Go on gotd, AGPL-3.0, ~8.3k★,
  v0.20.4): a CLI with takeout-based chat export. A useful reference for gotd
  takeout code (AGPL: read only).
- **Telegram Business "connected bots"**: a bot receives a Premium user's 1:1
  chats via business connection. The brief excludes bot and business APIs, and
  it has no history.

### 2.4 Telegram summary

The technical path is clear and well proven (gotd, as used by mautrix-telegram
in production). The open question is §2.1's AI clause, which bears directly on
substrate's assistant.

---

## 3. Discord

### 3.1 Platform rules

**Self-bots**

- Support article: "Automating normal user accounts (generally called
  'self-bots') outside of the OAuth2/bot API is forbidden, and can result in an
  account termination if found"
  ([support.discord.com/hc/articles/115002192352](https://support.discord.com/hc/articles/115002192352)).
- Safety page: "Using a user token in any application (known as a selfbot), or
  any automation of your account, may result in account suspension or
  termination"
  ([discord.com/safety/360044104071](https://discord.com/safety/360044104071-tips-against-spam-and-hacking)).

**Community Guidelines**

- The 2022–2024 versions: "Do not use self-bots or user-bots. Each account must
  be associated with a human, not a bot"
  ([2022 version](https://discord.com/terms/guidelines-march-2022)).
- The current text (dated 2025-09-29) was not readable from here
  **(unverified whether the wording changed)**.

**Developer Policy:** "You may not mine or scrape any data, content, or
information available on or through Discord services."

**Enforcement, 2025–2026**

- No public reports of a selfbot-specific ban wave were found.
- Discord's detection criteria are not public.
- Evidence of risk comes from the projects themselves:
  - mautrix docs: "Discord may ban users who are too suspicious … if you want to
    avoid any risk, use a bot account"
    ([docs](https://github.com/mautrix/docs/blob/master/bridges/go/discord/authentication.md)).
  - Dissent: "Using an unofficial client at all is against Discord's Terms of
    Service and may cause your account to be banned! … tries its best to not use
    the REST API at all unless necessary".
  - DiscordChatExporter: "automating user accounts is against Discord TOS and may
    result in you getting banned".
  - discord.py-self's own README: "a proof of concept and I cannot recommend
    using it".
- Beeper's rewrite tracks "account standing" and blocks DMs to strangers until
  standing is known (§3.6). That suggests unsolicited DMs are the practical ban
  trigger.

**Policy drift**

- Discord rolled out "teen-by-default" globally from March 2026: unverified
  accounts get filtered message requests and age-gated spaces
  ([TechCrunch](https://techcrunch.com/2026/02/09/discord-to-roll-out-age-verification-next-month-for-full-access-to-its-platform/)).
  This affects where DMs from strangers land, not whether a mirror works.
- A July 2026 report of ~8 000 wrongful AI-moderation bans
  ([discords.ai](https://www.discords.ai/blog/discord-ai-moderation-bug-wrongful-bans-july-2026),
  low-authority source) is a reminder that automated enforcement misfires.

### 3.2 Official options for reading a user's own DMs: none usable

**OAuth2 scopes** ([oauth2.mdx](https://github.com/discord/discord-api-docs/blob/main/developers/topics/oauth2.mdx))

| Scope | What it gives |
| ----- | ------------- |
| `dm_channels.read` | "see information about the user's DMs and group DMs — **only available to approved partners**" |
| `messages.read` | "for local rpc server api access" |
| `rpc` | "only available to approved partners" |
| `relationships.read` | Friends list. Part of the Social SDK and under its terms. |

**Social SDK** (games)

- Messaging needs the `sdk.social_layer` scope: "limited access", with rate
  limits until approved.
- Its DM history API (`Client::GetUserMessagesWithLimit`) has hard limits: "Only
  a maximum of 200 messages and up to 72 hours of history can be retrieved" and
  "Both players must have played the game"
  ([sending-direct-messages.mdx](https://github.com/discord/discord-api-docs/blob/main/developers/discord-social-sdk/development-guides/sending-direct-messages.mdx)).
- It also requires "you must only [send] in response to a user action".
- Useless for a personal mirror.

**Bot accounts** cannot see a user's DMs at all.

### 3.3 GDPR data package as a history source

- **Request:** User Settings → Data & Privacy → Request data.
- **Delivery:** emailed as a ZIP. It "can take up to 30 days". The download link
  stays valid 30 days.
- **Content:** the messages folder contains **only messages you sent**. Discord
  staff call this a privacy choice, so DMs come back one-sided. Deleted messages
  are absent ([Discord article 360004957991](https://support.discord.com/hc/articles/360004957991),
  [redact.dev](https://redact.dev/blog/discord-data-package-download)).
- **Format:** JSON files. Per third-party descriptions the layout is
  `messages/index.json` (channel id → name) plus `messages/c<channel_id>/channel.json`
  and `messages.json`. It was CSV in older packages.
  **(Exact filenames unverified: check against a real export.)**
- **Verdict:** a **zero-ToS-risk import** of the user's own side of every
  conversation, with channel ids that join to live data. Worth building
  regardless. Not a substitute for a live connector.

### 3.4 Protocol facts for user-account connectors

Primary reference: the community "userdoccers" docs,
[docs.discord.food](https://docs.discord.food), source at
[github.com/discord-userdoccers/discord-userdoccers](https://github.com/discord-userdoccers/discord-userdoccers),
active Nov 2026.

#### Login

**QR remote auth** ([remote-authentication/desktop](https://github.com/discord-userdoccers/discord-userdoccers/blob/master/pages/remote-authentication/desktop.mdx))

1. Open `wss://remote-auth-gateway.discord.gg/?v=2` with an `Origin:
   https://discord.com` header.
2. Receive `hello`; generate an RSA-OAEP 2048 key pair.
3. Send `init{encoded_public_key}`.
4. Receive `nonce_proof{encrypted_nonce}`; reply `nonce_proof{nonce}`.
5. Receive `pending_remote_init{fingerprint}`; show the QR
   `https://discord.com/ra/<fingerprint>`.
6. The user scans and approves in the mobile app. Receive
   `pending_ticket{encrypted_user_payload}`, then `pending_login{ticket}`.
7. Exchange it: `POST /users/@me/remote-auth/login {ticket}` returns
   `encrypted_token`; decrypt it with the private key.

mautrix docs warn QR "may encounter CAPTCHAs".

**Other login paths**

- **Token paste:** copy the `Authorization` header from a logged-in browser
  session.
- **Browser login:** mautrix-discord's rewrite loads `https://discord.com/login`
  in a webview and extracts `localStorage.token`.
- **Email/phone + password:** with MFA (TOTP, SMS, backup codes) and a
  hCaptcha step. CAPTCHAs can appear on any endpoint:
  ([captcha-handling](https://github.com/discord-userdoccers/discord-userdoccers/blob/master/pages/topics/captcha-handling.mdx)).

#### Token and session lifecycle

- **Store:** the user token is bearer access to the whole account; it is the
  only state. Optionally store the gateway `session_id` and `seq`.
- **Visibility:** each token is an auth session listed in Settings → Devices
  (`GET /auth/sessions`: os, platform, location).
- **Invalidation:**
  - logout (`POST /auth/logout`, or the user removing the device);
  - password reset (an Auth Session Change gateway event);
  - Discord locking "suspicious sessions".
- **Rotation:** READY may carry `auth_token`, "the refreshed auth token … the
  client should discard the current auth token". mautrix has a branch "save the
  refreshed token from READY" (Nov 2026), so **the sidecar must persist token
  rotations**.
- **No time-based expiry** is documented **(unverified)**.
- READY may also include `required_action` (verify email or phone, accept
  agreements …), which blocks use until resolved.

#### Gateway (realtime)

**Identify** ([using-gateway](https://github.com/discord-userdoccers/discord-userdoccers/blob/master/pages/gateway/using-gateway.mdx))

- Op 2 with `token`, `capabilities`, `properties` (os, browser, device,
  `client_build_number`, release channel, user agent …), `presence` and
  `client_state`.

**READY** carries `user`, `guilds`, `private_channels` (DMs, with
`last_message_id`), `relationships`, `read_state`, `sessions`, `session_id` and
`resume_gateway_url`. READY_SUPPLEMENTAL carries presences and lazily sent
private channels.

**Events:** `MESSAGE_CREATE`, `MESSAGE_UPDATE`, `MESSAGE_DELETE`,
`MESSAGE_DELETE_BULK`, `MESSAGE_REACTION_ADD`, `MESSAGE_REACTION_REMOVE` (plus
`_ALL` and `_EMOJI` variants), `TYPING_START`, `MESSAGE_ACK`,
`CHANNEL_CREATE` / `UPDATE` / `DELETE`, `CHANNEL_RECIPIENT_ADD` / `REMOVE`,
`RELATIONSHIP_ADD` / `REMOVE` / `UPDATE`, `USER_UPDATE`.

**Guilds**

- DMs and group DMs are always delivered.
- For guilds the session is not subscribed to, the gateway sends periodic
  `CHANNEL_UNREAD_UPDATE` or Passive Update summaries instead of every message.
  The client subscribes with *Update Guild Subscriptions*.
- The member-size threshold at which a guild becomes passive is
  **(unverified)**.

**Catch-up after downtime**

- Op 6 RESUME replays missed events "starting from the last sequence number",
  but only while the replay buffer holds. Otherwise Op 9 Invalid Session:
  re-identify.
- Closing with 1000/1001 invalidates the session immediately. Other drops keep
  it "a few minutes".
- So for **longer downtime there is no replay**. The client must diff READY's
  `private_channels[].last_message_id` against stored cursors and fetch
  `GET /channels/{id}/messages?after=<last>&limit=100` per changed channel.
- Edits, deletes and reactions that happened while offline are **only
  discoverable by re-fetching windows** of recent messages (`edited_timestamp`,
  `reactions`; deletions show as absence).
- mautrix's rewrite only resyncs the 10 most recently active DMs at startup plus
  existing portals (`client.go`). It has branches for "catchup paging" and
  "limit history fetches", i.e. it deliberately keeps REST volume low.

**Gateway rate limit:** 120 events per connection per 60 s.

#### REST (actions and history)

| Action | Endpoint |
| ------ | -------- |
| History | `GET /channels/{id}/messages?before|after|around=&limit≤100` (full DM history; guild channels need Read Message History) |
| Send / reply | `POST /channels/{id}/messages {content, nonce, message_reference:{message_id, channel_id}, …}`. Attachments via multipart or the cloud-upload flow. |
| Edit | `PATCH /channels/{id}/messages/{mid}` |
| Delete | `DELETE /channels/{id}/messages/{mid}` |
| React | `PUT /channels/{id}/messages/{mid}/reactions/{emoji}/@me` |
| Mark read (user-only) | `POST /channels/{id}/messages/{mid}/ack {token:null, manual?}`. Ack tokens are now ignored by the "new, Rust-based read state service" ([read-state](https://github.com/discord-userdoccers/discord-userdoccers/blob/master/pages/topics/read-state.mdx)). |
| Typing | `POST /channels/{id}/typing` |
| Roster (user-only) | `GET /users/@me/relationships` (friends, with optional nickname), `GET /users/@me/channels`, `GET /users/{id}/profile` |
| Avatars | `cdn.discordapp.com/avatars/{user_id}/{hash}.png` |

**Discord never exposes others' phone numbers or emails.**

**HTTP limits**

- 50 requests/s global per user.
- Per-route buckets; user tokens usually only get `Retry-After`,
  `X-RateLimit-Global` and `X-RateLimit-Scope`.
- **10 000 invalid requests (401/403/429) per 10 min per IP → 24 h Cloudflare
  ban** ([rate-limits](https://github.com/discord-userdoccers/discord-userdoccers/blob/master/pages/topics/rate-limits.mdx)).

#### Detection surface (why "behave like the web client" matters)

**Upstream libraries announce themselves:**

- discordgo sends `User-Agent: DiscordBot (…discordgo…)` and identify browser
  `"DiscordGo v…"` ([discord.go](https://github.com/bwmarrin/discordgo/blob/master/discord.go)).
- arikawa sends `"Arikawa"` as browser and device
  ([gateway/identify.go](https://github.com/diamondburned/arikawa/blob/v3/gateway/identify.go)).

**Beeper's fork mimics the web client.** beeper/discordgo identifies as Chrome
144 on Windows 10, scrapes the live `client_build_number` from discord.com's JS,
and sends a "vanilla" launch signature: "a completely unmodified client, i.e. a
browser environment that does not trip any of the property checks"
([droid.go](https://github.com/beeper/discordgo/blob/beeper/droid.go),
[launchsig.go](https://github.com/beeper/discordgo/blob/beeper/launchsig.go)).

**Behaving like a person** (synthesis of the above):

- gateway-first, REST only for user-visible actions and bounded catch-up;
- no bulk history crawls across many channels at once;
- ack only when the human reads;
- no unsolicited DMs or friend requests;
- solve CAPTCHAs interactively, never automatically;
- one gateway connection per account;
- persist rotated tokens.

### 3.5 Options

#### 3.5.1 mautrix-discord (Go) — *reference implementation, Beeper's production bridge*

**Project facts**

| Field | Value |
| ----- | ----- |
| URL | [github.com/mautrix/discord](https://github.com/mautrix/discord) |
| Language / license | Go / AGPL-3.0 (+ Beeper/Element exceptions) |
| Release state | ~510★. `main` is the **legacy architecture**: v0.7.7 (2026-08-16), maintenance commits to 2026-11-05. |
| Rewrite | The **bridgev2 rewrite is on the `megadiscord` branch**: 379 commits ahead of main, very active (Tulir Asokan, Beeper engineers and an "indent[bot]" committing daily in Nov 2026), **not yet released** (it has a `pkg/connector` layout and a `LICENSE.exceptions`). |
| Form | Matrix bridge daemon (Docker) |

**Auth as the user** — the rewrite offers four flows (`pkg/connector/login*.go`):

1. **Browser**: webview of discord.com/login, then extract `localStorage.token`.
2. **QR**: `pkg/remoteauth`.
3. **Token**.
4. **Email/Phone & Password**: MFA (TOTP, SMS, backup codes) and an hCaptcha step.

The legacy main has QR and token.

**History:** REST backfill with paging. Branches show "page backwards from the
last fetched message" and "limit history fetches".

**Realtime:** gateway through beeper/discordgo. Resyncs on READY. "Keep
retrying the gateway and report drops as transient disconnects".

**Account health:**

- Tracks account "vitals" and the safety hub: `RequiredAction` such as verify
  phone or email, or safety flows. It surfaces them as bad-credentials states.
- "Don't allow stranger DMs before account standing is known".

**Contacts:** relationships, guild member and webhook profiles.

**Actions:** send, edit, delete, react (including super reactions), ack (with a
branch "ack last viewed"), typing, threads, forums, polls (branches).

**Risks:** AGPL; ToS (§3.1).

**Recommendation**

- **Read its rewrite as the behaviour spec**: login flows, catch-up limits,
  account-standing checks, error mapping.
- Run it only as a separate process, and only if the bridgev2 path is chosen.
- Note the rewrite is unreleased today.

#### 3.5.2 beeper/discordgo (Go, BSD-3-Clause) — *recommended core if we build*

**Project facts**

| Field | Value |
| ----- | ----- |
| URL | [github.com/beeper/discordgo](https://github.com/beeper/discordgo), branch `beeper` |
| Language / license | Go / BSD-3-Clause (inherited from discordgo) |
| Maintenance | Commits 2026-11-04 ("retry 429s that come back without a JSON body"), 2026-10-16. 5★: a working fork, not a product. Pinned by mautrix-discord via `replace`. |
| Form | Library |

**What it adds:** user-API endpoints (`UserChannels`, `ChannelMessageAck`,
`RelationshipsGet`, …) and web-client identity mimicry (§3.4).

**Interface sketch**

```go
s, _ := discordgo.New(userToken)                  // no "Bot " prefix
s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) { cursorDiff(r.PrivateChannels) })
s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) { outbox.Append("message.new", m) })
s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageUpdate) { outbox.Append("message.edit", m) })
s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageDelete) { outbox.Append("message.delete", m) })
_ = s.Open()
msgs, _ := s.ChannelMessages(ch, 100, "", afterID, "")                      // catch-up
_, _ = s.ChannelMessageSendComplex(ch, &discordgo.MessageSend{Content: "ok",
        Reference: &discordgo.MessageReference{MessageID: mid, ChannelID: ch}})
_ = s.MessageReactionAdd(ch, mid, "👍")
_, _ = s.ChannelMessageAck(ch, mid, "")                                     // only on user view
_ = s.ChannelTyping(ch)
rels, _ := s.RelationshipsGet()
```

QR login needs a remote-auth client. mautrix's `pkg/remoteauth` is AGPL (no
per-file license header). Reimplementing it from the documented protocol is
roughly 300 lines.

**Risks:**

- A single-consumer fork: we track Beeper's changes.
- ToS (§3.1).

**Recommendation:** the core of a Discord sidecar if the owner opts in.

#### 3.5.3 bwmarrin/discordgo (upstream, Go, BSD-3)

- ~6k★, v0.29.0 (2025-05-24), last push 2026-02-14.
- Bot-oriented. "If the token is for a bot, it must be prefixed with 'Bot '".
  A bare user token works mechanically, but it identifies as "DiscordGo" and
  lacks most user endpoints.
- **Not suitable as-is.** This is why the Beeper fork exists.

#### 3.5.4 arikawa + ningen (Go, ISC)

- [arikawa](https://github.com/diamondburned/arikawa): v3.6.0 (2025-09-04),
  pushed 2026-11-02, ~600★.
- README: "Working user account support, with much of them in ningen. Please do
  not use this for self-botting."
- [ningen](https://github.com/diamondburned/ningen) (ISC, pushed 2026-07) adds
  user state: read states, relationships, guild subscriptions.
- Powers [Dissent](https://github.com/diamondburned/dissent), a GPL-3 GTK
  Discord client (v0.0.41, 2026-10-31) that logs in with a user token.
- Default identity is "Arikawa", so it must be overridden.
- **A credible ISC alternative** to the Beeper fork with a cleaner API.
  - *For:* Dissent's REST-avoidance stance is a useful model.
  - *Against:* fewer production hours than Beeper's fork on exactly our use case.

#### 3.5.5 discord.py-self (Python, MIT)

- [dolfies/discord.py-self](https://github.com/dolfies/discord.py-self): v2.1.0
  (2026-01-18), last commit 2026-08-02, ~1.3k★.
- Claims "Prevents user account automation detection".
- Implements sessions, read states, relationships, protobuf settings and more.
- Full discord.py-style API: `channel.history(limit=None, after=…)`,
  `message.ack()`, `message.add_reaction`, `channel.typing()`, `client.friends`.
- **Fit:** a Python sidecar (a long-lived process). Viable if we preferred
  Python. The Go options keep one toolchain.

#### 3.5.6 DiscordChatExporter (C#, MIT) — *history export tool*

- [Tyrrrz/DiscordChatExporter](https://github.com/Tyrrrz/DiscordChatExporter):
  2.48 (2026-08-27), ~12.4k★.
- CLI, GUI and Docker image. "Authentication via either a user or a bot token".
  JSON, HTML, CSV or TXT exports per channel with date bounds (`--after`,
  `--before`).
- README warns user-token automation "is against Discord TOS".
- README also states that using it "comes with the implicit agreement" to a
  political statement. That is not part of the MIT license, but worth knowing
  before depending on it in a public repo.
- **Fit:** a user-triggered one-off full backfill, both sides of DMs. It is
  bursty REST, so higher detection risk **(unverified)**. Optional, behind a
  warning.

#### 3.5.7 Not recommended

- **discord.js-selfbot-v13** (GPL-3.0): archived 2025-10.
- **Discord-S.C.U.M** (MIT): last commit 2024-03.
- **Upstream libraries with default identities.**
- **Social SDK / OAuth2**: cannot read DMs (§3.2).

### 3.6 How Beeper handles Discord

**What Beeper runs**

- Beeper's Discord network is mautrix-discord, developed by Beeper's own
  engineers (commit authors on `megadiscord`).
- Beeper's help page says Connect "should take you to the Discord web login",
  then 2FA. Users toggle which servers to sync; recent DMs sync automatically.
  The bridge is "not suitable for large servers"
  ([help.beeper.com/discord](https://help.beeper.com/discord)).
- Beeper developer docs label Discord accounts with a cloud network id
  (`discordgo`) rather than a local bridge id. That suggests Discord runs in
  Beeper's cloud **(inference, unverified)**.

**How it stays low-risk**

- identifies as the web client;
- keeps REST low;
- checks account standing before DMing strangers;
- treats CAPTCHAs as user-facing steps.

**Where substrate stands today:** substrate already reads Discord through
Beeper Desktop's local API (the existing Beeper provider). That path inherits
Beeper's ToS exposure on the same account; substrate adds no new exposure by
reading it.

### 3.7 Discord summary

**Everything that acts as the user is against Discord's rules.** The
engineering is solved (beeper/discordgo + mautrix-discord's rewrite as spec).
The decision is whether the owner accepts account-termination risk, which is
lowest when the connector looks and behaves like one browser tab.

---

## 4. Architecture notes for substrate (both networks)

### 4.1 One owner process per account

- **Telegram:** concurrent use of an auth key from two connections kills the
  session (`AUTH_KEY_DUPLICATED`).
- **Discord:** concurrent use is tolerated, but it doubles the fingerprint.
- **Consequence:** functions must not dial the network themselves while a
  sidecar runs. They call the sidecar.

**Sidecar API sketch** (local, authenticated):

```
POST /v1/accounts/{account}/messages      {"chat":"tg:user:123","text":"…","reply_to":"456","media":[…]}
POST /v1/accounts/{account}/reactions     {"chat":"…","message":"456","emoji":"👍"}
POST /v1/accounts/{account}/read          {"chat":"…","up_to":"456"}        // only on user action
GET  /v1/accounts/{account}/history?chat=…&before=…&limit=100
POST /v1/accounts/{account}/login/qr      → {"qr":"tg://login?token=…"} / {"qr":"https://discord.com/ra/…"}
```

For Telegram, gotd's `InvokeJSON` can back a generic `POST
/v1/accounts/{account}/invoke` for long-tail methods.

### 4.2 At-least-once to the server

- Reuse the `slack-socket` contract: batches of
  `{"version":1,"events":[…]}` to a webhook, with *advance only after `202`*.
- **Telegram:** the "ack" is persisting `pts` / `qts` / `seq` / channel `pts`.
  gotd advances them right after the handler returns, so the handler must
  fsync to a local outbox first. A shipper drains the outbox to the webhook.
- **Discord:** the cursor is the last message id per channel. Resume covers
  short drops; READY diff plus `?after=` paging covers long ones.
- Server-side writes should be idempotent on `(network, account, chat,
  message_id, event_kind, edit_version)`.

### 4.3 Session material is secret

- The Telegram auth key and the Discord token each grant full account access.
- Keep them sealed at rest, as substrate does for secret-typed properties.
- Hand them to the sidecar at boot. Persist Discord's READY `auth_token`
  rotations.
- Login flows (QR, code, 2FA password, CAPTCHA) are interactive steps the
  console drives through the sidecar.

### 4.4 Multi-user

- One sidecar process can host many accounts. gotd idles at ~150 KB per client.
- Telegram's api_id is per deployment or per repository (§2.1).

### 4.5 No-sidecar MVP for Telegram (optional)

- A scheduled Python function (Telethon) does `updates.getDifference` from
  stored state every N minutes, and action functions call Telethon directly.
- **Requirements:** a per-account lock serializing all functions (or two
  separate authorizations), and storing `access_hash` for every peer seen.
- **Gives:** catch-up without loss, latency equal to N.
- **Loses:** live typing and read receipts.
- No equivalent for Discord: REST-only polling is the least human-looking
  pattern.

---

## 5. Comparison table

All dates 2026 unless shown. ★ ≈ GitHub stars.

**Telegram**

| Option | Lang | License | Maintenance | Form | Login as user | History | Realtime + catch-up | Actions | Fit |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| **gotd/td + contrib** | Go | MIT | v0.163.0 (10-22), ~2.4k★, active | library | phone/code/2FA, QR | getHistory, takeout helper | full updates manager, durable state hooks | all | **embed in Go sidecar** |
| mautrix-telegram | Go | AGPL-3.0 (its `pkg/gotd` fork: MIT) | v26.10 (10-16), Go since 04-16, ~1.76k★ | Matrix bridge daemon | phone, QR, 2FA, bot | backfill + takeout | gotd manager + resync | all but presence/pins | reference; process only |
| TDLib (+ zelenin/go-tdlib) | C++ (+Go) | BSL-1.0 (Boost) (+MIT) | 1.8.70 (11-08); Go binding lags to 05-08 | native lib, JSON API | state machine incl. QR | getChatHistory | automatic, local DB | all | heavy, cgo |
| Telethon | Python | MIT | 1.45.0 (09-10); Codeberg active | library | phone, QR, 2FA | iter_messages, takeout | events; catch_up with SQLite session | all | functions MVP |
| Kurigram | Python | LGPL-3.0 | v2.2.27 (11-09) | library | phone, QR | yes | yes | all | ≈Telethon |
| Hydrogram / Pyrofork / Pyrogram | Python | LGPL-3.0 | no release since 2024 / archived / archived | — | — | — | — | — | avoid |
| mtcute / teleproto (GramJS archived) | TS | MIT | 0.32.7 (10-23) / 1.233.0 (11-09) | library | yes | yes | yes | all | only for a TS sidecar |
| chigwell/telegram-mcp | Python (Telethon) | Apache-2.0 | v3.3.12 (11-08), ~2.0k★ | MCP daemon (HTTP), Docker | session string (QR/phone) | export_chat_history | in-memory event log (gaps on restart) | 80+ tools | assistant stopgap |
| chaindead/telegram-mcp | Go (gotd) | MIT | 05-28, ~350★ | MCP daemon | env + session | dialog read | none | 5 tools | too thin |
| TDLight Bot API (user mode) | C++ | BSL-1.0 (Boost) | v10.2 (07-14) | HTTP daemon, Docker | /userlogin, code, 2FA | **none** | Bot-API updates | most | not suitable |

**Discord**

| Option | Lang | License | Maintenance | Form | Login as user | History | Realtime + catch-up | Actions | Fit |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| **beeper/discordgo** | Go | BSD-3 | commits 11-04 | library (fork) | token (QR via remote-auth client) | REST paging | gateway + resume; READY diff | all incl. ack, relationships | **embed in Go sidecar (opt-in)** |
| mautrix-discord | Go | AGPL-3.0 | v0.7.7 legacy (08-16); bridgev2 rewrite unreleased, active 11-05 | Matrix bridge daemon | browser, QR, token, password+MFA+captcha | bounded backfill | gateway, standing checks | all | reference; process only |
| arikawa + ningen | Go | ISC | v3.6.0 (2025-09), pushed 11-02 | library | token | REST | gateway | most | alternative core |
| upstream discordgo | Go | BSD-3 | v0.29.0 (2025-05) | library | token (as "DiscordGo") | REST | gateway | partial | not as-is |
| discord.py-self | Python | MIT | v2.1.0 (01-18), commits to 08-02 | library | token | history() | gateway | all incl. ack | Python sidecar alt. |
| DiscordChatExporter | C# | MIT (+ political README clause) | 2.48 (08-27), ~12.4k★ | CLI / Docker | token | full export (JSON) | none | none | one-off backfill |
| Data package | — | — | — | ZIP export | n/a | **own messages only**, ≤30 days | none | none | **zero-risk import** |
| OAuth2 / Social SDK | — | — | — | official | OAuth | 200 msgs / 72 h, game-only | — | in-game only | not usable |
| Beeper Desktop API (today) | — | — | — | local API | via Beeper | what Beeper synced | 15-min poll | send | keep as fallback |

---

## 6. Ranked recommendations

### Telegram

1. **Own Go sidecar on gotd/td + gotd/contrib (MIT).**
   - **Why:**
     - Go matches the server.
     - The license is permissive.
     - gotd's `updates.Manager` already implements Telegram's full gap-recovery
       algorithm with pluggable durable storage.
     - Takeout and QR login are first-class.
     - Beeper's own Telegram bridge has run on (a fork of) it since April 2026.
       That fork is MIT too, so its fixes are reusable.
   - **Build notes:**
     - durable outbox before the handler returns (§4.2);
     - one connection per account;
     - offline presence;
     - read and typing only on human action;
     - takeout for initial backfill, opt-in;
     - operator api_id with a per-repository override.
   - **Precondition:** the owner decides how §2.1's AI clause applies to Moss.
2. **mautrix-telegram as a separate process.** Only if the sibling report picks
   bridgev2 as the integration framework. It is the most complete Telegram
   user-connector in Go, but AGPL and Matrix-shaped.
3. **chigwell/telegram-mcp over streamable HTTP.** A quick action surface for
   the assistant (Apache-2.0, actively maintained). Not a mirror: its event log
   is in-memory, so restarts need per-chat re-reads.
4. **Telethon inside Python functions** (no sidecar). Polling `getDifference`
   with serialized per-account access. Acceptable MVP for history and actions;
   no live typing or read receipts.
5. **TDLib.** Most complete semantics, but a C++ build and cgo cost for little
   gain over gotd.

*Avoid:* Pyrogram, Pyrofork, Hydrogram releases, GramJS (dead or stalled);
TDLight user mode (no history).

### Discord

0. **Regardless of the choice below:** add a **data-package importer**. It is
   zero ToS risk and covers the user's own messages back to account creation.
   Keep **Beeper** as today's live path until a direct connector exists.
1. **Own Go sidecar on beeper/discordgo (BSD-3)**, gated behind an explicit
   owner opt-in that states Discord forbids it.
   - **Behave like one browser tab:**
     - gateway-first;
     - READY-diff catch-up with bounded `?after=` paging;
     - ack only on human read;
     - no unsolicited DMs;
     - interactive CAPTCHA and QR login;
     - persist token rotations.
   - **Why:** it is the exact library Beeper's production Discord bridge uses,
     with the web-client identity work done, under a permissive license.
     Behaviour is specified by mautrix-discord's bridgev2 rewrite (read only:
     AGPL).
2. **mautrix-discord as a separate process.** If bridgev2 is the chosen
   framework. Today its rewrite (`megadiscord`) is unreleased, and the
   released v0.7.7 is the legacy architecture.
3. **arikawa + ningen (ISC).** If the Beeper fork's single-consumer nature
   worries us. Override its "Arikawa" identity; follow Dissent's REST
   minimalism.
4. **discord.py-self.** If a Python sidecar were preferred.
5. **DiscordChatExporter.** Only as a user-triggered one-off backfill of both
   sides of DMs, with a warning.

*Not possible:* OAuth2 / Social SDK DM access. *Not suitable:* upstream
discordgo or arikawa identities; archived selfbot libraries.

---

## 7. Unverified or open items

- **Telegram AI clause:** whether §1.5 / content-licensing covers a personal
  assistant reading the user's own chats. This is interpretation; get the
  owner's (or counsel's) call.
- **`TAKEOUT_INIT_DELAY` length:** the common ~24 h for new sessions is
  community lore, not documented. The server returns the seconds.
- **Polling cadence:** how Telegram treats a session that reconnects every few
  minutes (the §4.5 MVP).
- **Telegram reactions:** whether reaction changes on others' messages are
  replayed in `getDifference`.
- **Telegram send pace:** a safe per-chat or per-account send rate. Not
  published.
- **TDLib redelivery:** whether TDLib redelivers updates after an app crash. The
  docs don't say; reconcile anyway.
- **Discord passive guilds:** the member threshold at which a guild becomes
  "passive" for a user session.
- **Discord token lifetime:** whether user tokens have any time-based expiry.
- **Discord data package:** exact current filenames.
- **Beeper Discord placement:** whether Beeper runs Discord in the cloud or on
  device. Inferred as cloud from its dev docs.
- **Discord guidelines:** the current (2025-09-29) Community Guidelines wording
  on self-bots. The 2022–2024 text is verified.
- **Discord enforcement:** no 2025–2026 enforcement data specific to
  self-bots / bridges was found. Absence of reports is not evidence of safety.

---

## 8. Sources (primary first)

**Telegram**

- API terms: <https://core.telegram.org/api/terms>
- Obtaining api_id: <https://core.telegram.org/api/obtaining_api_id>
- Content licensing: <https://telegram.org/tos/content-licensing>
- Open Terms Archive memo (2026-02-03): <https://opentermsarchive.org/en/memos/telegram-prohibits-collecting-data-for-ai-use/>
- User authorization (incl. future auth tokens, email setup, unconfirmed sessions, frozen accounts): <https://core.telegram.org/api/auth>
- QR login: <https://core.telegram.org/api/qr-login>
- Updates and gap recovery: <https://core.telegram.org/api/updates>
- Takeout: <https://core.telegram.org/api/takeout>, <https://core.telegram.org/method/account.initTakeoutSession>
- Errors (FLOOD, `AUTH_KEY_DUPLICATED`): <https://core.telegram.org/api/errors>
- Client config (freeze fields): <https://core.telegram.org/api/config>
- `messages.getHistory`: <https://core.telegram.org/method/messages.getHistory>
- TDLib getting started: <https://core.telegram.org/tdlib/getting-started>
- TDLib options: <https://core.telegram.org/tdlib/options>
- TDLib API scheme: <https://github.com/tdlib/td/blob/master/td/generate/scheme/td_api.tl>
- gotd: <https://github.com/gotd/td> (README, `.github/SUPPORT.md`, `telegram/updates/*`, `examples/{updates,takeout,qrauth.go,tdesktop-mimic}`)
- gotd/contrib: <https://github.com/gotd/contrib> (`middleware/floodwait`, `middleware/ratelimit`)
- mautrix-telegram: <https://github.com/mautrix/telegram> (`CHANGELOG.md`, `ROADMAP.md`, `LICENSE.exceptions`, `pkg/connector/{login*,client,backfill,handlematrix,example-config.yaml}`, `pkg/gotd/README.md`)
- mautrix docs: <https://github.com/mautrix/docs/blob/master/bridges/go/telegram/authentication.md>
- Telethon (Codeberg): <https://codeberg.org/Lonami/Telethon>
- Telethon FAQ: <https://docs.telethon.dev/en/stable/quick-references/faq.html>
- Telethon client reference (takeout): <https://docs.telethon.dev/en/stable/modules/client.html>
- Kurigram: <https://github.com/kurigram-org/kurigram>
- Hydrogram: <https://github.com/hydrogram/hydrogram>
- Pyrogram (archived): <https://github.com/pyrogram/pyrogram>
- Pyrofork (archived): <https://github.com/Mayuri-Chan/pyrofork>
- GramJS (archived): <https://github.com/gram-js/gramjs>
- teleproto: <https://github.com/sanyok12345/teleproto>
- mtcute: <https://github.com/mtcute/mtcute>
- chigwell/telegram-mcp: <https://github.com/chigwell/telegram-mcp>
- chaindead/telegram-mcp: <https://github.com/chaindead/telegram-mcp>
- TDLight Bot API: <https://github.com/tdlight-team/tdlight-telegram-bot-api>
- zelenin/go-tdlib: <https://github.com/zelenin/go-tdlib>
- iyear/tdl: <https://github.com/iyear/tdl>

**Discord**

- Self-bots support article: <https://support.discord.com/hc/articles/115002192352>
- Safety page: <https://discord.com/safety/360044104071-tips-against-spam-and-hacking>
- Community Guidelines (2022): <https://discord.com/terms/guidelines-march-2022>
- OAuth2 scopes: <https://github.com/discord/discord-api-docs/blob/main/developers/topics/oauth2.mdx>
- Social SDK scopes: <https://github.com/discord/discord-api-docs/blob/main/developers/discord-social-sdk/core-concepts/oauth2-scopes.mdx>
- Social SDK DMs: <https://github.com/discord/discord-api-docs/blob/main/developers/discord-social-sdk/development-guides/sending-direct-messages.mdx>
- Data package: <https://support.discord.com/hc/articles/360004957991>, <https://redact.dev/blog/discord-data-package-download>
- Userdoccers: <https://github.com/discord-userdoccers/discord-userdoccers> (`pages/remote-authentication/*`, `pages/authentication.mdx`, `pages/gateway/*`, `pages/topics/{read-state,rate-limits,captcha-handling}.mdx`, `pages/resources/relationships.mdx`)
- mautrix-discord: <https://github.com/mautrix/discord> (`main`, branch `megadiscord`: `pkg/connector/{login*,client,backfill}.go`, `LICENSE.exceptions`)
- mautrix docs: <https://github.com/mautrix/docs/blob/master/bridges/go/discord/authentication.md>
- beeper/discordgo: <https://github.com/beeper/discordgo/tree/beeper> (`droid.go`, `launchsig.go`, `restapi.go`)
- discordgo: <https://github.com/bwmarrin/discordgo>
- arikawa: <https://github.com/diamondburned/arikawa>
- ningen: <https://github.com/diamondburned/ningen>
- Dissent: <https://github.com/diamondburned/dissent>
- discord.py-self: <https://github.com/dolfies/discord.py-self>
- DiscordChatExporter: <https://github.com/Tyrrrz/DiscordChatExporter>
- discord.js-selfbot-v13 (archived): <https://github.com/aiko-chan-ai/discord.js-selfbot-v13>
- Beeper Discord help: <https://help.beeper.com/discord>
- Teen-by-default (TechCrunch): <https://techcrunch.com/2026/02/09/discord-to-roll-out-age-verification-next-month-for-full-access-to-its-platform/>
- July 2026 wrongful bans (low authority): <https://www.discords.ai/blog/discord-ai-moderation-bug-wrongful-bans-july-2026>

**substrate context:** `cmd/slack-socket/README.md` in this repo (sidecar contract: batches to a webhook, ack upstream only after `202`).
