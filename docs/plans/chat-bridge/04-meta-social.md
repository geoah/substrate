# Meta (Messenger, Instagram) and the social DMs (X, LinkedIn, Bluesky): libraries, protocols, behaviour

Research date: **2026-11-09**. Scope: the network-specific libraries and how each network behaves for a
**personal** account driven *as the user*. The mautrix **bridgev2 framework** itself is covered by a sibling
report; here the mautrix connectors (mautrix-meta, mautrix-instagram, mautrix-twitter, mautrix-linkedin,
mautrix-bluesky) are assessed as *options* and as the place where the protocol code lives.

Method: source code read on GitHub (`gh api`) at today's `main`, official docs (Meta, X's docs source repo
`xdevplatform/docs` incl. its OpenAPI `v2.174`, the atproto lexicons and specs), release notes, issue trackers,
and news for dated events. **"Verified"** is the default for anything with a code or docs link. Anything from
secondary sources or inference is marked **(unverified)**. X's own domains (`x.com`, `docs.x.com`,
`devcommunity.x.com`) were unreachable from the research machine, so X facts come from X's docs *source* on
GitHub, X's OpenAPI file, and search summaries of the forum.

---

## 0. TL;DR

| Network | Best option | Why / caveat |
|---|---|---|
| **Facebook Messenger** | **mautrix-meta / `messagix`** (Go, AGPL-3.0), run as a separate sidecar process | It's the only maintained, complete implementation. It covers unencrypted chats (Lightspeed over Meta's DGW websocket) and E2EE chats (whatsmeow's Messenger mode), and since **v26.10 (2026-10-16)** it can restore **E2EE history from Messenger secure storage with the user's 6-digit PIN**. Login: web-view cookies (`c_user`, `xs`, `datr`) or username/password through an emulated Messenger iOS/Android login. **Ban/checkpoint risk is real**, and much worse from datacenter IPs. |
| **Instagram** | **mautrix-instagram (`instameow`)** (same repo, AGPL-3.0) as a sidecar; **instagrapi** (Python, MIT) is the fallback | Meta **switched off Instagram E2EE globally on 8 May 2026** and moved the web DMs to a **new protocol**, so mautrix split out a new bridge (v26.07). It has cookie login plus native Android username/password login (v26.10). The official API only serves *professional* accounts as a reply-within-24h inbox. |
| **X / Twitter** | **Official X Chat API (`/2/chat/*`) + Chat XDK** (MIT, Rust core with **Go**, Python and JS bindings) | X replaced DMs with encrypted **XChat** (about Dec 2025). With an OAuth 2.0 user token and the user's XChat **passcode** the XDK loads the user's *existing* keys. It covers history, realtime (Activity API webhooks), send, reply, react, delete, read, typing and media. Pay-per-use: DM event read **$0.010**, DM create **$0.015**, `chat.received` **$0.010**. Fallback: **mautrix-twitter** (AGPL; cookies; supports XChat via Juicebox PIN). **twikit is unmaintained and cannot see XChat.** |
| **LinkedIn** | **mautrix-linkedin (`linkedingo`)** (Go, AGPL-3.0) as a sidecar, opt-in with warnings | No official member messaging API: the official one is partner-only and every send must be a member action. **linkedin-api (Python) is dead** (GitHub repo now 404, last release Nov 2024). **Unipile** is a paid hosted proxy that holds your session. Highest-stakes account, and the ToS explicitly bans bots that "send or redirect messages". |
| **Bluesky** | **Official `chat.bsky.*` API** via **MarshalX `atproto`** (Python, MIT) or **indigo** (Go, MIT/Apache) | No reverse engineering. Auth by app password *with DM access* or OAuth (`transition:chat.bsky` / `include:chat.bsky.authFullChatClient`). Requests go via the PDS with `atproto-proxy: did:web:api.bsky.chat#bsky_chat`. `getLog` with a cursor gives events and catch-up. Groups, replies and reactions are supported; there are no edits, no typing, no media upload, and delete is for-self only. It fits substrate's short-lived Python functions; **no sidecar needed.** |

---

## 1. Licensing: what each license means for substrate

substrate's repo currently has **no LICENSE file** (checked 2026-11-09). The analysis below assumes a
permissive license (MIT/Apache) for substrate. If substrate itself were AGPL-3.0, the mautrix libraries could
simply be embedded.

| License | Projects in this report | Linking into `substrated` (Go) | Running it as a separate process |
|---|---|---|---|
| **AGPL-3.0** | mautrix-meta (incl. `pkg/messagix`, `pkg/instameow`; [LICENSE](https://github.com/mautrix/meta)), mautrix-twitter (`twittermeow`, `juiceboxgo`), mautrix-linkedin (`linkedingo`), mautrix-bluesky, mautrix-manager | The combined binary is AGPL-3.0. All of substrate would have to be offered under AGPL, including source to every network user (§13). **Incompatible with a permissive substrate.** | **Fine.** Separate programs talking over HTTP/IPC are an "aggregate", and substrate keeps its license. The obligations attach to the sidecar: publish its source with our modifications and, because people interact with it over a network, offer that source (§13). mautrix-meta's [`LICENSE.exceptions`](https://github.com/mautrix/meta/blob/main/LICENSE.exceptions) grants extra rights **only to Beeper and Element**. |
| **GPL-3.0** | fbchat-muqit | Copyleft on distribution | Separate process is fine |
| **MPL-2.0** | whatsmeow ([repo](https://github.com/tulir/whatsmeow)) | File-level copyleft: can be linked into permissive code; modified MPL files must be published | n/a |
| **MIT / Apache-2.0 / BSD** | instagrapi (MIT), X `chat-xdk` (MIT), indigo (MIT/Apache dual), MarshalX `atproto` (MIT), twikit (MIT), fbchat (BSD-3), 0xzer/messagix (MIT, stale), Texts `platform-*` (MIT, stale) | Free to embed | n/a |
| **Trap** | **fbchat-v2** is labelled Apache-2.0, but its Go E2EE bridge `require`s `go.mau.fi/mautrix-meta` ([go.mod](https://github.com/m008v/fbchat-v2/blob/main/bridge-e2ee/go.mod)) | The bridge binary is in practice AGPL-encumbered | n/a |

**Practical consequence.** A Meta, X or LinkedIn sidecar built on the mautrix libraries would follow the
pattern of `cmd/slack-socket` (hold the connection, POST event batches to a substrate webhook). But it **must
be its own AGPL-3.0 program** that is never linked into `substrated`. It can live in the public repo in its own
directory with its own LICENSE. That is the standard FSF reading of "aggregate"; have counsel confirm before
shipping.

---

## 2. Facebook Messenger (personal account)

### 2.1 How Messenger works in 2026 (from source)

- **Unencrypted chats: "Lightspeed" (LS).** The server streams batches of stored-procedure calls that mutate a
  client-side database (`LSTable`, e.g. `UpsertMessages`, `LSInsertNewMessageRange`,
  `LSUpdateExistingMessageRange`). Updates are grouped into numbered *sync groups* ("databases": `1` mailbox,
  `2`/`95` contacts and E2EE thread list, `104`, …), each with a `LastAppliedCursor`. The client sends the cursor
  back and receives only the deltas since then
  ([`syncManager.go`](https://github.com/mautrix/meta/blob/main/pkg/messagix/syncManager.go)). This used to run
  as MQTT over WebSocket. Today's `messagix` uses Meta's **DGW** websocket gateway
  (`https://gateway.facebook.com/ws/lightspeed`, package
  [`pkg/messagix/dgw`](https://github.com/mautrix/meta/tree/main/pkg/messagix/dgw); endpoints in
  [`endpoints/facebook.go`](https://github.com/mautrix/meta/blob/main/pkg/messagix/endpoints/facebook.go)).
  Requests are JSON "LS request" payloads with task queues
  ([`socket.go`](https://github.com/mautrix/meta/blob/main/pkg/messagix/socket.go)).
- **E2EE chats** (default for personal 1:1 chats since Dec 2023,
  [Meta engineering](https://engineering.fb.com/2023/12/06/security/building-end-to-end-security-for-messenger/))
  run on **WhatsApp's protocol stack ("Armadillo"; Signal protocol)**:
  - device registration through ICDC (`https://reg-e2ee.facebook.com/v2/fb_register_v2`);
  - a websocket to `wss://web-chat-e2ee.facebook.com/ws/chat`;
  - auth by a **crypto auth token (CAT)** from GraphQL `MAWCatQuery`, refreshed about daily
    ([`e2ee-client.go`](https://github.com/mautrix/meta/blob/main/pkg/messagix/e2ee-client.go),
    [`e2ee-register.go`](https://github.com/mautrix/meta/blob/main/pkg/messagix/e2ee-register.go)).

  `messagix` hands the encrypted side to **whatsmeow** (`whatsmeow.NewClient(device, …)` with
  `MessengerConfig{UserAgent, BaseURL, WebsocketURL}` and `RefreshCAT`). Encrypted Messenger is whatsmeow's
  Messenger mode, which is the "Armadillo implementation in whatsmeow".
- **E2EE history** lives in Messenger's **secure storage** (Meta's *Labyrinth* protocol: ciphertexts on Meta's
  servers, loaded on demand). It is unlocked with a **6-digit PIN**.
  [`pkg/messagix/securehistory`](https://github.com/mautrix/meta/tree/main/pkg/messagix/securehistory)
  implements the recovery: `GetBackup`, then `RecoverPIN`, `GetVirtualDevice`, `DeriveEpochs`, `PrepareDevice`
  and `AddDevice`, with files `opaque/`, `minos*`, `vesta.proto`.

### 2.2 Option A: mautrix-meta and its `messagix` library (recommended)

| | |
|---|---|
| URL | https://github.com/mautrix/meta (binary `mautrix-meta` = Messenger; `mautrix-instagram` = Instagram, §3) |
| Language / license | Go / **AGPL-3.0** (+ exceptions for Beeper and Element only). `messagix` started as [0xzer/messagix](https://github.com/0xzer/messagix) (MIT; last push 2024-03-26, ~19★, predates DGW and E2EE, so treat it as broken). The maintained copy is the AGPL package inside mautrix-meta. |
| Maintenance | ~469★. Release **v26.10 on 2026-10-16** (monthly calendar releases). Commits daily, latest 2026-11-08 to 11-09. Maintainer Tulir Asokan (mautrix), with Beeper contributors. |
| Deployment form | A Matrix appservice bridge (bridgev2): Docker `dock.mau.dev/mautrix/meta` and GitHub binaries. The library `go.mau.fi/mautrix-meta/pkg/messagix` can be imported directly, under AGPL (§1). |

**Authentication** (exact flows, from
[`pkg/connector/login.go`](https://github.com/mautrix/meta/blob/main/pkg/connector/login.go) and the
[auth docs](https://docs.mau.fi/bridges/go/meta/authentication.html)):

1. **`messenger` / `facebook` (cookies).**
   - The login step tells the client to open `https://www.messenger.com/?no_redirect=true` (or
     `https://www.facebook.com/`), using the bridge's user agent (Chrome 141 on Linux,
     [`useragent.go`](https://github.com/mautrix/meta/blob/main/pkg/messagix/useragent/useragent.go)).
   - It waits until the URL matches `^https://www\.messenger\.com/(?:e2ee/)?(?:t/[0-9]+/?)?(?:\?.*)?$`.
   - It then reads cookies **`xs`, `c_user`, `datr`** (the docs also list `sb`). Users can paste a "Copy as cURL"
     of any `graphql` XHR instead.
   - `LoadMessagesPage()` validates the session. Its failures map to `Challenge`, `Checkpoint`, `Consent`,
     `TokenInvalidated` and `RateLimited`.
2. **`messenger-lite` (iOS) / `messenger-lite-android`: username and password, no browser.**
   - The bridge emulates the Messenger app's Bloks-driven native login against `graph.facebook.com` or
     `b-graph.facebook.com`, with `pwd_key_fetch` for password encryption and `auth/create_session_for_app`.
   - 2FA and other steps arrive as Bloks screens. A Bloks `CheckpointError` aborts with "checkpoint".
   - The result is cookies plus a **native session** (access token, app ID, device ID, family device ID).
     Added as experimental in v26.02 and improved through v26.10.
3. **Optional history PIN.** With `e2ee_backfill: true`, after login the bridge asks: "Enter your Messenger
   secure storage PIN to restore encrypted chat history".
   - Six digits. Messenger reports the attempts remaining, and a hard limit ends in `HistoryPINLimit`
     ([`login_history.go`](https://github.com/mautrix/meta/blob/main/pkg/connector/login_history.go)).
4. **mautrix-manager** ([repo](https://github.com/mautrix/manager), Electron, AGPL) automates the cookie step
   with an embedded browser. That is the same pattern our iOS and Android shells could use (§7).

**Session state to persist.**
- Cookies; the jar updates from `Set-Cookie`.
- The optional native session.
- The **whatsmeow device store**: Signal identity, prekeys, sessions and app state.
- The secure-history device and its derived epochs.
- LS sync-group cursors, or a cached "reconnection state" (`cache_connection_state`).
- Per-thread backfill cursors.

**Lifetime.** Meta's cookie policy gives `c_user`/`xs` 365 days and `datr` 400 days
([archived Meta cookie page via search](https://webarchive.library.unt.edu/web/20201218163545/https://www.facebook.com/help/cookies)).
In practice, sessions end on logout, a password change, or a risk-system checkpoint. The bridge:
- force-reconnects every 20 h (`force_refresh_interval_seconds: 72000`), at most once per hour
  (`min_full_reconnect_interval_seconds: 3600`);
- refreshes the CAT when it is within 23 h of expiry
  ([`example-config.yaml`](https://github.com/mautrix/meta/blob/main/pkg/connector/example-config.yaml)).

**History.**
- **Unencrypted:** `socket.FetchMessagesTask{ThreadKey, ReferenceTimestampMs, ReferenceMessageId, SyncGroup:1,
  Cursor}` pages backwards until the server's range says `HasMoreBefore=false`. That reaches the start of the
  thread, as far as Meta holds it
  ([`backfill.go`](https://github.com/mautrix/meta/blob/main/pkg/connector/backfill.go)). The thread list is
  paged with `FetchMoreThreads` (`thread_backfill.batch_count: -1` = all threads, 2 s between pages;
  [`threadbackfill.go`](https://github.com/mautrix/meta/blob/main/pkg/connector/threadbackfill.go)).
- **Encrypted:** new in **v26.10**, from secure storage with the PIN
  ([`backfill_e2ee.go`](https://github.com/mautrix/meta/blob/main/pkg/connector/backfill_e2ee.go) decodes
  `waArmadilloBackupMessage` with edits and reactions folded in).
  - The v26.10 notes say "older unencrypted history will no longer be backfilled" for chats that were cut over
    to E2EE ([CHANGELOG](https://github.com/mautrix/meta/blob/main/CHANGELOG.md)).
  - Current `main` has code to continue into the pre-encryption "open" thread through
    `LSCreateOpenToE2EEThreadLink` ([`history.go`](https://github.com/mautrix/meta/blob/main/pkg/connector/history.go),
    [`history_cutover.go`](https://github.com/mautrix/meta/blob/main/pkg/connector/history_cutover.go)).
    **(Unverified whether this ships in the next release.)**
  - Without the PIN: only E2EE messages that arrive after login.

**Realtime and catch-up.**
- The DGW stream pushes LS deltas. On (re)connect `messagix` re-queries sync groups `1, 2, 95, 104` with their
  last applied cursors (`ensureSyncedSocket`). A thread-list refresh (`FetchThreadsTask`) plus forward backfill
  of each thread's `UpsertMessages` range then fills gaps after the bridge was down.
- E2EE messages arrive over the whatsmeow websocket, which delivers the server's offline queue on reconnect.
  **(How long Meta queues undelivered E2EE messages is unverified.)**
- Connection-state caching speeds restarts.

**Contacts.**
- Thread participants come with names and avatars from LS tables.
- `socket.GetContactsFullTask{ContactID}` does per-user lookup and `SearchUserTask` searches
  ([`socket/contacts.go`](https://github.com/mautrix/meta/blob/main/pkg/messagix/socket/contacts.go)).
- A `GetContactsTask{Limit}` ("search_contacts") exists in the library, but the bridge does not use it to sync a
  friends roster. **(Whether it returns the full friends list is unverified.)**

**Actions** ([ROADMAP](https://github.com/mautrix/meta/blob/main/ROADMAP.md),
[`capabilities.go`](https://github.com/mautrix/meta/blob/main/pkg/connector/capabilities.go)):

| Action | Support |
|---|---|
| Send | Text (≤20,000 chars), images, video, files, voice, GIFs, stickers |
| Replies and mentions | Yes |
| Reactions | Yes, 1 per user |
| Edit | ≤5 edits within 15 min |
| Unsend | For everyone; no delete-for-me |
| Read receipts and typing | Yes |
| Polls | Unencrypted groups only |
| Group management | Add, remove, name, avatar, leave |
| Block / unblock | Yes |
| Disappearing messages | E2EE chats |

Encrypted chats support the same set except polls, locations, and writing to the chat backup.

**Interface sketch** (Go, approximate; names from source):

```go
c := &cookies.Cookies{Platform: types.Messenger}
c.UpdateValues(map[cookies.MetaCookieName]string{"c_user": cUser, "xs": xs, "datr": datr})
cli := messagix.NewClient(c, log, cfg)            // cfg: UA, proxy, retries (the bridge's getMessagixConfig)
user, firstTable, err := cli.LoadMessagesPage(ctx) // validates cookies; checkpoint/consent/invalidated errors
cli.SetEventHandler(handler)                       // receives *table.LSTable (messages, reactions, receipts,
                                                   // typing, thread changes) and *messagix.ConnectedEvent,
                                                   // ReconnectedEvent, TransientDisconnectEvent, PermanentErrorEvent
err = cli.Connect(ctx)
_, err = cli.ExecuteTasks(ctx, &socket.SendMessageTask{ThreadId: tid, Otid: otid, Text: "hi",
    ReplyMetaData: &socket.ReplyMetaData{ReplyMessageId: mid}, SyncGroup: 1 /* + Source, SendType */})
_, err = cli.ExecuteTasks(ctx, &socket.EditMessageTask{MessageID: mid, Text: "fixed"})
_, err = cli.ExecuteTasks(ctx, &socket.ThreadMarkReadTask{ThreadId: tid, LastReadWatermarkTs: ts, SyncGroup: 1})
_, err = cli.ExecuteTasks(ctx, &socket.FetchMessagesTask{ThreadKey: tid, ReferenceTimestampMs: ts,
    ReferenceMessageId: mid, SyncGroup: 1})          // history page; reply arrives as an UpsertMessages table
wa, err := cli.PrepareE2EEClient()                   // *whatsmeow.Client for encrypted chats
hist := cli.GetSecureHistoryClient()                 // PIN-based encrypted history restore
```

**Risks and how to look like a person.**
- **ToS:** Instagram's help center bans "collecting information in an automated way without our express
  permission" ([help.instagram.com](https://help.instagram.com/581066165581870)). Meta has an Automated Data
  Collection Terms page **(exact Meta Terms wording unverified)**.
- **Field evidence:** [mautrix/meta#44](https://github.com/mautrix/meta/issues/44), open since 2024-03, is titled
  "We suspect automated behaviour on your account".
  - Users report captchas, "unusual activity" restrictions on Instagram, weekly disconnects and one claimed ban.
  - Hosting in the **cloud** triggered warnings "within a couple hours", while the same setup at home did not.
  - Users report fewer problems with 2FA on, a stable home or VPN IP, an account that isn't brand new, and
    keeping a normal browser session on the same site the cookies came from.
- **The mautrix docs** warn that Meta "may decide your account has suspicious activity and block you until you…
  complet[e] a captcha, add a phone number or reset your password", and recommend 2FA.
- **Built-in mitigations.**
  - A real Chrome UA, plus a **Chrome TLS fingerprint** via `imroc/req` `ImpersonateChrome()` and uTLS
    ([`httpclient/http.go`](https://github.com/mautrix/meta/blob/main/pkg/messagix/httpclient/http.go)).
  - Retry on 429 with proxy switch.
  - `proxy` / `get_proxy_from` per traffic class (login, E2EE, media).
  - Optional Tor onion endpoint.
  - bridgev2's provisioning API can route the **login HTTP through the user's own device**
    (`LoginStartParams.HTTP FingerprintingRoundTripper`,
    [mautrix-go `bridgev2/login.go`](https://github.com/mautrix/go/blob/main/bridgev2/login.go)). Beeper's
    clients do this.
- **For substrate:**
  - Run the sidecar from the user's **residential IP** (home server), or through a sticky residential proxy in
    their country.
  - Never run two clients on one login.
  - Keep backfill paced.
  - Keep `cache_connection_state` on, so restarts don't look like fresh browser loads.

**Recommendation.** Use it. Run it as an **AGPL sidecar**, either the whole bridge or a thin `messagix` and
whatsmeow wrapper that POSTs events to substrate's webhook. The sibling bridgev2 report should decide which.

### 2.3 Other Messenger clients (assessed, not recommended)

| Option | Facts | Verdict |
|---|---|---|
| **fbchat-v2** ([m008v/fbchat-v2](https://github.com/m008v/fbchat-v2)) | Python async plus a Go JSON-RPC "E2EE bridge" built on mautrix-meta and whatsmeow. 144★, last push 2026-10-26, single maintainer, Vietnamese-first docs. Cookie or password login; MQTT listener; send, edit, react, unsend; receives E2EE "through the Go bridge". | Shows `messagix` can be wrapped as a JSON-RPC sidecar. The Apache-2.0 label is misleading (AGPL dependency). Single-maintainer risk. |
| **fbchat-muqit** ([repo](https://github.com/togashigreat/fbchat-muqit)) | Python, **GPL-3.0**, 82★, last push 2026-09-22. Cookie-only ("login via email and password is no longer supported"). README: **sending to other users isn't supported anymore because of E2EE**, only groups and pages. | Can't act on 1:1 chats. |
| **fbchat** ([fbchat-dev/fbchat](https://github.com/fbchat-dev/fbchat)) | BSD-3, README says **"officially marked as unmaintained"**; last push 2024-02. [fbmessengerexport](https://github.com/karlicoss/fbmessengerexport) depends on it. | Dead. |
| **Texts `platform-meta`** ([TextsHQ](https://github.com/TextsHQ/platform-meta)) | MIT TypeScript, last push 2024-10-20. Texts was merged into Beeper (Automattic). | Stale; predates DGW, the IG split and E2EE history. |

### 2.4 Official Messenger Platform: why it doesn't fit

- The **Messenger Platform** is the *Facebook Page* messaging API. The Send API authenticates with a **Page
  access token**, and recipient IDs are **page-scoped**
  ([Send API reference](https://developers.facebook.com/docs/messenger-platform/reference/send-api)).
- It only sees conversations people start *with the Page*. It is bound by a **24-hour standard messaging
  window**, with the `HUMAN_AGENT` tag allowing 7 days after app approval.
- It cannot read or write a person's own inbox. Personal-inbox API access ended in 2015: the XMPP Chat API shut
  down on **30 April 2015**
  ([reports](https://www.omgubuntu.co.uk/2015/04/facebook-chat-api-empathy-pidgin-stop-working/amp)), and the
  `read_mailbox` permission went in the same Graph API v2.x era **(exact retirement date unverified)**.

### 2.5 How Beeper does Messenger E2EE

- Beeper runs **mautrix-meta** (it holds the license exception). Encrypted Messenger works through whatsmeow's
  Messenger/Armadillo support: register an ICDC device, connect to `web-chat-e2ee.facebook.com`, use the
  Signal protocol.
- Since July 2025 Beeper can run bridges **on the user's device** instead of in its cloud, so end-to-end
  encryption is kept between the device and Meta ([heise, 2025-07-17](https://heise.de/-10491837)).
- Beeper's help pages render only in a browser and couldn't be fetched. Search summaries of them say:
  - only E2EE messages received after connecting are bridged;
  - Messenger history import is "normal chats only, 20 most recent conversations, 100 messages each, E2EE chats
    cannot be imported" ([help.beeper.com](https://help.beeper.com/chat-networks/history-import)).

  **(Unverified and probably older than v26.10's PIN-based encrypted history.)**
- Beeper's clients take part in login through bridgev2. For example, mautrix-twitter shows "The request did not
  complete on this device" when a login request proxied to the Beeper client fails
  ([`login.go`](https://github.com/mautrix/twitter/blob/main/pkg/connector/login.go)). The network sees the
  user's own device and IP during login.

---

## 3. Instagram DMs (personal account)

### 3.1 State of play (2026)

- **E2EE is gone.** Instagram ended end-to-end encrypted DMs globally on **8 May 2026**. It quietly updated its
  terms in March: "End-to-end encrypted messaging on Instagram will no longer be supported after 8 May 2026"
  ([BBC, 2026-05-08](https://www.bbc.com/news/articles/clypzxl3lvqo)). mautrix-meta had already "Removed option
  to use encrypted chats on Instagram" in v26.04.
- **New protocol.** In June 2026 "the current version of Instagram's web client no longer uses the same
  lightspeed API as Facebook Messenger" ([mau.fi June 2026](https://mau.fi/blog/2026-06-mautrix-release/)).
  - **v26.07** shipped a **new Instagram bridge**, `mautrix-instagram`, in the same repo: Docker tags
    `dock.mau.dev/mautrix/meta:ig-*`, `./build-ig.sh` ([mau.fi July 2026](https://mau.fi/blog/2026-07-mautrix-release/)).
  - **v26.08** removed Instagram from `mautrix-meta`.
- **How the new client talks.** The library is
  [`pkg/instameow`](https://github.com/mautrix/meta/tree/main/pkg/instameow). It uses:
  - DGW websockets on `gateway.instagram.com`: `/ws/lightspeed`, `/ws/mqttbypass`, `/ws/streamcontroller`, and
    native variants;
  - GraphQL "Slide" types (mailbox, thread, message);
  - Thrift request streams, flatbuffers, and an `MDCoreSync` protobuf;
  - an emulated **Instagram Android 454** profile for native login (commit
    "use one Instagram Android 454 profile everywhere", 2026-11-08).

### 3.2 Option A: mautrix-instagram (`instameow`) (recommended)

Same repo, license and maintainer as §2.2 (AGPL-3.0, v26.10).

**Authentication** ([`igconnector/login.go`](https://github.com/mautrix/meta/blob/main/pkg/igconnector/login.go),
[`login_native.go`](https://github.com/mautrix/meta/blob/main/pkg/igconnector/login_native.go)):
- **`instagram` (cookies).**
  - Opens `https://www.instagram.com/accounts/login/` and waits for
    `^https://www\.instagram\.com/(?:direct/(?:inbox/|t/[0-9]+/)?|accounts/onetap/)?(?:\?.*)?$`.
  - Required cookies: **`sessionid`, `csrftoken`, `ds_user_id`**. Optional: `mid`, `ig_did`, `rur`, `shbid`,
    `shbts` ([`cookies.go`](https://github.com/mautrix/meta/blob/main/pkg/messagix/cookies/cookies.go)).
- **`android` ("Native Android", v26.10): username and password through the Instagram Android API.**
  - Uses "CAA"/Bloks login screens, a 2FA code, web challenge or checkpoint pages, consent, and a captcha
    solver ([`login_web_captcha.js`](https://github.com/mautrix/meta/tree/main/pkg/instameow)).
  - Persists one Android "installation identity" per user, so the account always sees the same phone
    (`InstagramLoginDevice`, DB migration `13-instagram-login-device.sql`).
- **`instagram-password` ("Native Web").** Web password login. Per the v26.09 notes it may still need a web
  view, so it is offered only through mautrix-manager.

**Session state.** Cookies, the native session (`InstagramNativeSession`), the device identity, and the
Instagram **`seq_id` and its timestamp** (DB table `09-instagram-seq-id-table.sql`). Also cached connection state
(`cache_connection_state: true` by default for IG).

**Realtime and catch-up.**
- One main stream, plus **two extra connections for typing** (config comment: "Bridging typing notifications
  requires 2 extra connections per user").
- On reconnect, "the server replays from the last saved sequence ID"
  ([`instameow/socket.go`](https://github.com/mautrix/meta/blob/main/pkg/instameow/socket.go)). If the saved ID is
  too old, the server demands a **resnapshot**: the client reloads the mailbox (`LoadIndex`/`GetMailbox`), then
  forward-backfills each thread to its last bridged message.
- v26.10 "Fixed backfilling Instagram chats if the bridge is offline for a long time".

**History.**
- Thread messages come from `GetThread` and `PaginateMessages` (pages of 20, `AfterCursor`), paging back until
  `HasNextPage=false` ([`igconnector/backfill.go`](https://github.com/mautrix/meta/blob/main/pkg/igconnector/backfill.go)).
  The thread list comes from `PaginateMailbox`. `thread_backfill.batch_count` defaults to **0** for Instagram,
  so only recent threads.
- **Known bug:** [mautrix/meta#374](https://github.com/mautrix/meta/issues/374), open since 2026-10-01: "Instagram
  chat backfill never gets past the second page; `batch_count: -1` disables it". Deep history is intended but
  currently unreliable.
- `disable_xma_backfill: true` by default (reels and stories in history are not fetched).

**Contacts.** No follower or following roster sync. Participants' profiles come from `GetProfile`,
`GetUserForNewDM` and `SearchUsers`.

**Actions** (`instameow` methods):

| Action | Method / limit |
|---|---|
| Send text | `SendMessage` (≤1,000 chars) |
| Send media | `SendMedia` |
| Edit | `EditMessage` (≤5 edits within 15 min) |
| Unsend | `UnsendMessage` (for everyone) |
| React | `SendReaction` (1 per user) |
| Mark read | `MarkRead` |
| Typing | `SetTyping` |
| Groups | `CreateGroup`, `AddMembers`, `RemoveMember`, `AddAdmins`, `RemoveAdmins`, `EditGroupTitle`, `EditGroupAvatar`, `LeaveGroup` |
| Thread and message state | `PinThread`, `PinMessage`, `MuteThread`, `AcceptMessageRequest`, `DeleteThread` |
| Block / unblock | `SetUserBlocked` |
| Reels | `FetchReel` |

**Risks.** As for Messenger (§2.2), and Instagram is, if anything, more aggressive. #44 includes Instagram
"We've noticed unusual activity from your account and have temporarily restricted certain actions". The same
mitigations apply: residential IP, a stable device identity, 2FA, gentle backfill.

**Recommendation.** Primary option, as an AGPL sidecar. It is young (July 2026), so expect churn.

### 3.3 Option B: instagrapi (Instagram private mobile API)

| | |
|---|---|
| URL | https://github.com/subzeroid/instagrapi |
| Language / license | Python / **MIT** (its realtime MQTT is derived from `instagram_mqtt`, © Nerixyz) |
| Maintenance | ~7.1k★. **Very active**: releases 3.2.4–3.2.8 between 6 and 9 Nov 2026. Maintainer "subzeroid", who also sells the HikerAPI SaaS. |
| Deployment form | A Python library. Usable inside substrate's Python functions, but realtime needs a persistent process. |

- **Auth.** `cl.login(username, password, verification_code=<TOTP>)`, or `login_by_sessionid(...)`.
  - Persist `cl.dump_settings()`: device UUIDs, an Android device profile, cookies, and the
    `Authorization: Bearer IGT:2:…` header. Reload it with `load_settings()`.
  - "Challenge resolver" hooks handle email and SMS codes
    ([docs](https://github.com/subzeroid/instagrapi/tree/master/docs/usage-guide)).
  - Recent issues show frequent login breakage: CAA returns no session
    ([#2881](https://github.com/subzeroid/instagrapi/issues/2881)); `qe/sync` returns 404
    ([#2920](https://github.com/subzeroid/instagrapi/issues/2920)); header fingerprint inconsistencies
    ([#2871](https://github.com/subzeroid/instagrapi/issues/2871),
    [#2868](https://github.com/subzeroid/instagrapi/issues/2868)).
- **History.** `direct_threads(amount)`, `direct_thread(thread_id, amount)` (paginates internally),
  `direct_messages(...)`, `direct_requests()`. Depth goes as far as the private API's cursor goes **(unverified
  for very old threads)**.
- **Realtime (experimental).**
  - `realtime_connect()` opens Instagram's **MQTToT**. `direct_subscribe()` subscribes Iris message-sync with
    `seq_id` and `snapshot_at_ms`. FBNS push uses `mqtt-mini.facebook.com`
    ([realtime docs](https://github.com/subzeroid/instagrapi/blob/master/docs/usage-guide/realtime.md)).
  - Catch-up should come from Iris `seq_id` replay, as in the mobile app **(unverified in instagrapi)**.
  - **Caveat:** mautrix says the *web* client moved to a new protocol in June 2026. instagrapi targets the
    *Android* private API, which still works per its active issue tracker.
- **Contacts.** `user_following`, `user_followers`, `user_info` (heavy, risky calls).
- **Actions.**

  | Action | Method |
  |---|---|
  | Send text | `direct_send(text, thread_ids=…)` |
  | Reply | `direct_send(…, reply_to_message=…)` |
  | Media | `direct_send_photo`, `direct_send_video`, `direct_send_voice` |
  | Reactions | `direct_send_reaction`, `direct_delete_reaction` |
  | Unsend | `direct_message_unsend` |
  | Delete for self | `direct_message_delete` |
  | Mark seen | MQTT `direct_mark_seen` |
  | Typing | MQTT `direct_indicate_activity` |
  | Thread management | mute, hide, `direct_request_approve` |

  **No edit listed in the docs (unverified).**
- **Risks.** Its own [best-practices page](https://github.com/subzeroid/instagrapi/blob/master/docs/usage-guide/best-practices.md)
  advises:
  - one stable proxy per account, matching country, ASN and device;
  - warm accounts up slowly;
  - `delay_range`;
  - treat `ClientThrottledError`/429, `PleaseWaitFewMinutes`, `FeedbackRequired` and `LoginRequired` differently.

  Open issue [#2870](https://github.com/subzeroid/instagrapi/issues/2870) says hard-coded `direct_send` fields make
  cold DMs "look like mass-DM automation". It is a constant arms race.
- **Recommendation.** Fallback only, if AGPL is a blocker. Prefer it read-mostly, from the user's IP.

### 3.4 Instagram and Messenger MCP servers

- [trypeggy/instagram_dm_mcp](https://github.com/trypeggy/instagram_dm_mcp): MIT, 189★, last push **2025-08-13**,
  pins `instagrapi==2.1.5`, username and password in `.env`. Others found are tiny or abandoned.
- No maintained Messenger MCP server turned up.

None is a foundation. They are thin wrappers around the same private APIs.

### 3.5 Official "Instagram API with Instagram Login": why it doesn't fit

- **Professional accounts only.** It works for Business or Creator accounts. The consumer API (Basic Display)
  **shut down on 4 Dec 2024**
  ([Meta, 2024-09-04](https://developers.facebook.com/blog/post/2024/09/04/update-on-instagram-basic-display-api)).
- **A personal workaround exists but is narrow.** A user can switch to a Creator account and register their own
  Meta app; Standard Access is allowed "if your app serves Instagram professional accounts you own or manage".
  Even then, per [Send Messages](https://developers.facebook.com/documentation/instagram-platform/instagram-api-with-instagram-login/messaging-api)
  and [Conversations](https://developers.facebook.com/documentation/instagram-platform/instagram-api-with-instagram-login/conversations-api):
  - **conversations only begin when the other person messages first**;
  - replies are allowed within the **24-hour** window (7 days with the human-agent tag);
  - **group messaging is not supported**;
  - "you can only get details about the **20 most recent messages**";
  - Requests-folder chats idle for over 30 days are not returned;
  - API reads don't mark messages read;
  - shared media comes through as URLs only.

  That is a customer-service inbox, not "act as the user".

---

## 4. X (Twitter) DMs

### 4.1 State of play: XChat

- **What XChat is.** X moved DMs to **XChat**: E2EE with per-user P-256 identity and signing keys. The private
  keys are backed up in **Juicebox** (distributed, PIN-hardened key storage) under a **4-digit passcode**.
- **Criticism.** Cryptographers note there is **no forward secrecy** and that X controls key distribution
  ([Matthew Green, 2025-06-09](https://blog.cryptographyengineering.com/2025/06/09/a-bit-more-on-twitter-xs-new-encrypted-messaging/)).
  mau.fi calls the protocol's quality "extremely dubious" but says supporting it is "necessary to keep receiving
  messages" ([mau.fi March 2026](https://mau.fi/blog/2026-03-mautrix-release/)).
- **Rollout.** Secondary reports say DMs were **replaced by XChat for all users around Dec 2025**
  ([PiunikaWeb](https://piunikaweb.com/2025/12/10/x-formerly-twitter-has-replaced-dms-with-x-chat-for-all-users/))
  **(unverified officially)**.
- **Legacy content.**
  - X's API docs say chat events may be "unencrypted, such as messages sent before the conversation was
    encrypted".
  - Message requests from untrusted senders still flow through the legacy REST inbox, which mautrix-twitter
    polls.

### 4.2 Option A: official X Chat API with the Chat XDK (recommended)

| | |
|---|---|
| Docs | `docs.x.com/xchat/*`. Source: [xdevplatform/docs/xchat](https://github.com/xdevplatform/docs/tree/main/xchat); OpenAPI [`openapi.json`](https://github.com/xdevplatform/docs/blob/main/openapi.json) v2.174 |
| SDK | [xdevplatform/chat-xdk](https://github.com/xdevplatform/chat-xdk): **MIT**, a Rust core with bindings. **Go** `github.com/xdevplatform/chat-xdk/go/chatxdk` uses cgo with prebuilt static libs for macOS arm64/amd64 and Linux amd64 glibc/musl. **Python** `chatxdk` (PyO3), JS/WASM, .NET and JVM also exist. **v0.7.3 on 2026-11-05**; Go releases since 2026-07-20. Pre-1.0: v0.7 had a migration guide. |
| Maintenance | X's developer platform team. Active (repo pushed 2026-11-09). |
| Deployment form | A library plus HTTPS. Realtime by webhook, so **no persistent process is needed** if substrate can expose a public webhook. |

**Auth.**
- A developer account (console.x.com), and an app inside a Project. One forum report describes `/2/chat/*`
  returning a Project-related 403 while the same token worked elsewhere
  ([forum](https://devcommunity.x.com/t/pay-per-use-app-receives-xchat-webhooks-but-2-chat-returns-incorrect-project-403/270490))
  **(resolution unverified)**.
- **OAuth 2.0 Authorization Code with PKCE** and a user-context token with **`dm.read dm.write tweet.read
  users.read`**, plus `media.write` for attachments and **`offline.access`** for refresh tokens. Access tokens
  expire after **2 hours**; only `offline.access` issues a refresh token, so it is required for unattended use
  ([OAuth2 doc](https://github.com/xdevplatform/docs/blob/main/fundamentals/authentication/oauth-2-0/authorization-code.mdx)).

**Keys.** For a user who already uses XChat, **load the existing identity** rather than registering new keys
([Getting started §2](https://github.com/xdevplatform/docs/blob/main/xchat/getting-started.mdx)):
- construct the XDK with the `juicebox_config` from the user's public-key record, then **`unlock(pin)`**; or
- `import_keys` from a blob made with `export_keys`.

X's guidance treats the PIN and keys as **root credentials** that survive OAuth revocation:
- "never put them on your servers for UI apps";
- for "bot/automation on your infrastructure", keep the `export_keys` blob in a secret manager
  ([handling private keys](https://github.com/xdevplatform/docs/blob/main/xchat/handling-private-keys.mdx)).

substrate is the user's *own* server, so the second pattern applies. Seal the blob with substrate's credential key.

**Endpoints** (from the OpenAPI; all user-context):

| Purpose | Method and path |
|---|---|
| Inbox (newest first, ≤1000/page, filters `unread_only`, `type`, `participant_query`, `start_time`…; read state via `chat_conversation.fields=unread,last_read_event_id,newest_event_id`) | `GET /2/chat/conversations` |
| History: messages, key changes, group changes, read receipts (newest first, ≤100/page) | `GET /2/chat/conversations/{id}/events` |
| Conversation details | `GET /2/chat/conversations/{id}` |
| Send a message **or reaction** (body: `message_id`, `encoded_message_create_event`, `encoded_message_event_signature`, `conversation_token`, `xdk_version`) | `POST /2/chat/conversations/{id}/messages` |
| Delete (`delete_for_all` / `delete_for_self`, `sequence_ids`, `action_signatures`) | `POST /2/chat/conversations/{id}/messages/delete` |
| Read / unread (optional signed read receipt) | `POST …/{id}/read`, `POST …/{id}/unread` |
| Typing | `POST …/{id}/typing` |
| Conversation keys (1:1 init, rotation) | `POST …/{id}/keys` |
| Media (encrypted) | `POST /2/chat/media/upload/initialize`, `…/{id}/append`, `…/{id}/finalize`; `GET /2/chat/media/{id}/{media_hash_key}` |
| Groups | `POST /2/chat/conversations/group/initialize`, `…/group`, `…/{id}/members`, `…/members/remove`, `…/admins`, invite links, join requests, `PUT …/{id}` |
| Requests and settings | `GET /2/chat/message_requests`, `POST …/{id}/accept`, mute, archive, disappearing messages (`message_ttl`), `GET/PUT /2/chat/settings` |
| Keys | `POST /2/users/{id}/public_keys`, and a public-key lookup |

**Realtime and catch-up** ([real-time events](https://github.com/xdevplatform/docs/blob/main/xchat/real-time-events.mdx)):
- The **X Activity API** delivers `chat.received`, `chat.sent` and `chat.conversation.join` (the old
  `chat.conversation_join` is a deprecated alias). Subscriptions are created with
  `POST /2/activity/subscriptions {event_type, filter:{user_id}, webhook_id, tag, expires_at}`, using the user's
  token; an optional `direction` filter was added in March 2026.
- Delivery goes either to a registered webhook (`POST /2/webhooks`, CRC challenge, HMAC signature) or to the
  `GET /2/activity/stream` firehose (app bearer token).
- Payloads carry `encoded_event` (ciphertext) and sometimes `conversation_key_change_event`. Deduplicate on
  `event_uuid`, and on the decrypted `message_id`.
- **After downtime:** the stream offers `backfill_minutes` (**max 5**). For longer gaps, list conversations with
  `newest_event_id` / `last_read_event_id` and page `/events` until reaching the last stored event.

**History.** Paginated back from newest. **The retention depth of `/2/chat` events is not documented
(unverified).** The legacy `/2/dm_events` family is documented as "**up to 30 days ago**" only
([lookup intro](https://github.com/xdevplatform/docs/blob/main/x-api/direct-messages/lookup/introduction.mdx)).

**Contacts.**
- `GET /2/users/{id}/following` and `/followers`. These qualify as **"Owned Reads" at $0.001/resource** when the
  authenticated user owns the developer app, which is our case.
- `users.read` lookups for participants: name, username, `profile_image_url`.

**Actions.** Send text, media, replies and reactions are all built by the XDK (`encryptMessage`,
`encryptReply`) and posted to `/messages`. Also: delete for everyone or for self, mark read and unread, typing,
groups, accepting requests, mute, archive, disappearing messages. Edits are supported by the Chat SDK's
"official" XChat adapter ([chat-sdk.dev XChat adapter](https://chat-sdk.dev/adapters/official/xchat): an
`editSafetyDelayMs` option; it also lists emoji reactions, typing and read receipts). There is no separate edit
endpoint in X's OpenAPI, so edits presumably travel as message events built by the XDK **(edit encoding
unverified)**.

**Pricing** ([pricing doc](https://github.com/xdevplatform/docs/blob/main/x-api/getting-started/pricing.mdx); pay-per-use since **6 Feb 2026**):

| Item | Price |
|---|---|
| DM Event Read | **$0.010** per resource |
| DM Interaction Create | **$0.015** per request |
| `chat.received` webhook event | **$0.010** |
| `chat.sent`, `chat.conversation_join`, `dm.read`, `dm.indicate_typing` | not billed |
| Owned Reads (own followers/following) | $0.001 per resource |

- Resources are deduplicated per UTC day.
- That `/2/chat/*` reads bill as "DM Event Read" comes from a forum reply **(unverified)**.
- **Example:** 100 received and 30 sent messages a day is about $1.00 + $0.45 = **~$43/month**. Initial
  history backfill costs $10 per 1,000 events.
- Every self-hoster needs their own developer account and prepaid credits.

**Rate limits.**
- The `/2/chat/*` endpoints are **not in the published rate-limit table**. One developer observed 50/15 min and
  1,000/24 h on conversation details, and 10,000/24 h on history and public keys
  ([forum](https://devcommunity.x.com/t/x-chat-bot-daily-quota-scopes-endpoint-limits-and-streaming-access/278160))
  **(unverified)**.
- Legacy DM limits are documented
  ([rate limits](https://github.com/xdevplatform/docs/blob/main/x-api/fundamentals/rate-limits.mdx)):

  | Legacy endpoint | Limit |
  |---|---|
  | Lookups | 15 per 15 min per user |
  | Create | 15 per 15 min and 1,440 per 24 h per user; 1,440 per 24 h per app |
  | Delete | 300 per 15 min and 1,500 per 24 h per user; 4,000 per 24 h per app |

**Interface sketch** (Python; the same flow exists in Go):

```python
from xdk import Client                       # pip install xdk chatxdk
from chat_xdk import Chat, ImportKeysParams, SetIdentityParams, EncryptMessageParams

api = Client(access_token=user_token)        # dm.read dm.write tweet.read users.read media.write offline.access
chat = Chat()
chat.import_keys(ImportKeysParams(sealed_blob, signing_key_version=v))   # blob from a one-time unlock(pin)+export_keys
chat.set_identity(SetIdentityParams(my_user_id, v)); chat.set_cache_keys(True)
# webhook handler (substrate trigger → function):
ev = chat.decrypt_event(payload["encoded_event"])        # after decrypt_events([key_change]) if present
# send: enc = chat.encrypt_message(EncryptMessageParams(conversation_id, "hi"))
#       POST /2/chat/conversations/{id}/messages {"message_id": ..., "encoded_message_create_event": ...}
```

**Risks.**
- Official, so no anti-bot cat-and-mouse. X's Developer Agreement and automation rules still apply: no
  unsolicited bulk DMs.
- Key custody, as above.
- The XDK is pre-1.0 and its Go binding needs cgo.
- Cost.
- Single-vendor policy risk: pricing changed twice in 2025–26.

**Recommendation.** First choice for X. The webhook model fits substrate without a sidecar.

### 4.3 Option B: mautrix-twitter (`twittermeow` + `juiceboxgo`)

| | |
|---|---|
| URL | https://github.com/mautrix/twitter |
| Language / license | Go / **AGPL-3.0**. Includes `pkg/juiceboxgo`, a Go Juicebox client (OPRF, Noise, Shamir secret sharing). |
| Maintenance | ~127★. **v26.09 on 2026-09-16**, commits daily (latest 2026-11-08). XChat support since **v26.03**. |
| Deployment form | bridgev2 bridge (Docker), or import the libraries under AGPL. |

- **Auth** ([docs](https://docs.mau.fi/bridges/go/twitter/authentication.html),
  [`login.go`](https://github.com/mautrix/twitter/blob/main/pkg/connector/login.go)):
  - **`cookies`:** `auth_token` and `ct0` from x.com.
  - **`password`** (v26.08): X's "Jetfuel" web onboarding flow. It needs a **Castle** anti-bot token generated
    in a *hidden web view* (`castle_token`, from `https://x.com/robots.txt` in a web view), plus 2FA or
    verification steps. Usable only through mautrix-manager or Beeper clients.
  - Then the **4-digit XChat PIN** recovers the user's keys from Juicebox. If the user has no PIN, the bridge
    creates one: "No PIN code is registered yet. Create your X Chat PIN below."
- **Session state.** Cookies, the XChat key material (signing and decryption keys; `keystore.go`), polling
  cursors, XChat sequence cursors (`cache_session: true`).
- **Realtime.**
  - XChat websocket `wss://chat-ws.x.com/ws`.
  - Legacy REST polling of `/i/api/1.1/dm/user_updates.json` every **10 s** with a cursor, "primarily for
    untrusted (message request) conversations which don't receive real-time updates via XChat WebSocket"
    ([`polling.go`](https://github.com/mautrix/twitter/blob/main/pkg/twittermeow/polling.go)).
- **Catch-up.**
  - A checkpointed inbox catch-up by max sequence ID (`GetInitialXChatPageQuery`, `GetInboxPageRequestQuery`;
    [`xchat_reconnect.go`](https://github.com/mautrix/twitter/blob/main/pkg/connector/xchat_reconnect.go)).
  - Per-conversation gap filling in batches of 50, pausing 15 min after a rate limit
    ([`xchat_gap.go`](https://github.com/mautrix/twitter/blob/main/pkg/connector/xchat_gap.go)).
  - v26.06: "forward backfilling to fill gaps", "resuming initial conversation sync after restart".
- **History.** XChat pages (`GetConversationPageQuery`, `GetMessageEventsPageQuery`), then a **fallback to
  legacy REST** (`/i/api/1.1/dm/conversation/{id}.json`) for pre-XChat history
  ([`backfill.go`](https://github.com/mautrix/twitter/blob/main/pkg/connector/backfill.go)).
  `conversation_sync_limit: 20` conversations at startup.
- **Contacts.** Participants' profiles (`GetUsersByIdsForXChat`) and typeahead search. No following roster.
- **Actions** ([`capabilities.go`](https://github.com/mautrix/twitter/blob/main/pkg/connector/capabilities.go)):

  | Action | Support |
  |---|---|
  | Send | Text (≤10,000), images, video, GIFs, voice |
  | Reply | Yes |
  | Edit | ≤10 edits within 15 min, XChat only |
  | Reactions | Yes |
  | Delete for everyone | XChat only |
  | Delete for me | Yes |
  | Read receipts and typing | Yes |
  | Groups | Create, invite, name, avatar |
  | Other | Block, mute, accept requests |

  The ROADMAP.md is stale.
- **Risks.** X's terms limit access to its published interfaces and ban scraping without written consent; there
  is a $15,000-per-million-posts liquidated-damages clause aimed at scraping
  ([summary](https://conductatlas.com/platform/x/x-terms-of-service/anti-scraping-and-acceptable-use-restrictions/))
  **(exact current wording unverified)**. Using cookies means the account itself is the enforcement target.
- **Recommendation.** Fallback when a developer account or its cost is unacceptable, or when the official API
  lacks something.

### 4.4 Other unofficial X clients

- [**twikit**](https://github.com/d60/twikit) (Python, MIT, 4.8k★): last release **2.3.1 on 2025-02-06**, last code
  change about April 2025 (a README edit in March 2026). Its `send_dm`, `get_dm_history` and `delete_dm` hit the
  **legacy** DM endpoints, so it **cannot read or send XChat**. Effectively abandoned for DMs.
- [**twscrape**](https://github.com/vladkens/twscrape) (MIT, active): read-only scraping of posts and users. No DMs.
- [**twitter-api-client**](https://github.com/trevorhobenshield/twitter-api-client) (MIT): last push 2024-05.
  Stale.
- **Texts `platform-twitter`** (MIT): last push 2024-09. Stale.

### 4.5 Legacy official v2 DM endpoints

`GET /2/dm_events`, `/2/dm_conversations/{id}/dm_events`, `POST …/messages`, `DELETE /2/dm_events/{id}`, using
the same OAuth 2.0 scopes. They have **30-day lookback**, cover only the legacy unencrypted DM system, and are
tightly rate limited (15 lookups per 15 min). With XChat they are insufficient on their own. The X Chat API
supersedes them.

---

## 5. LinkedIn messaging

### 5.1 Official APIs: why they don't fit

- The **Messages API is limited to approved partners**. Each send must come from a specific member action
  ("members must choose to send"); no scheduled or automated sending, and no inbox read
  ([Microsoft Learn](https://learn.microsoft.com/en-us/linkedin/shared/integrations/communications/messages)).
- Reading a member's messages exists only in the **Compliance Events API**, for regulated firms in the Compliance
  Partner Program.
- Company Page inbox APIs are partner-only and Page-scoped.

### 5.2 Option A: mautrix-linkedin (`linkedingo`) (recommended, with caveats)

| | |
|---|---|
| URL | https://github.com/mautrix/linkedin. Rewritten in Go in v25.11; previously beeper/linkedin in Python. |
| Language / license | Go / **AGPL-3.0** |
| Maintenance | ~39★. **v26.09 on 2026-09-16**, commits through 2026-11-05. |
| Deployment form | bridgev2 bridge, or the `pkg/linkedingo` library under AGPL |

- **Auth** ([docs](https://docs.mau.fi/bridges/go/linkedin/authentication.html),
  [`login.go`](https://github.com/mautrix/linkedin/blob/main/pkg/connector/login.go)):
  - **Cookies:** paste the **whole Cookie header**, including `li_at` and `JSESSIONID` (the CSRF token is
    `JSESSIONID`'s value), plus the browser's `x-li-track` and `x-li-page-instance` headers and
    `navigator.userAgent`.
  - The docs warn that LinkedIn "doesn't like cookies being used by different user agents", so match the
    bridge's UA (Chrome 141 on Linux). They also recommend an incognito tab, closing the browser before pasting.
  - **Email and password:** `login_password.go` with `login_sdui.go` (server-driven UI) and
    `login_checkpoint.go` (email PIN or challenge).
- **Session state.** The cookie jar (if `li_at` expires or reads "delete me", the session is dead), page
  instance, x-li-track, realtime session ID, and a conversations sync token.
- **Realtime.** **Server-Sent Events** at `https://www.linkedin.com/realtime/connect?rc=1`, with heartbeats and the
  `x-li-recipe-map` / `x-li-query-map` headers, kept as JSON files because they change
  ([`realtime.go`](https://github.com/mautrix/linkedin/blob/main/pkg/linkedingo/realtime.go),
  [README](https://github.com/mautrix/linkedin/blob/main/pkg/linkedingo/README.md)). The stream carries messages,
  typing, seen receipts, reaction summaries, and conversation updates and deletes.
- **Catch-up.** `GetConversationsBySyncToken` (delta sync) and `GetConversationsUpdatedBefore`. v26.04 fixed
  "conversation deletes that happened while the bridge was offline".
- **History.** `GetMessagesBefore(conv, before, count)` and `GetMessagesWithPrevCursor` page backwards with
  `prevCursor` until the start ([`backfill.go`](https://github.com/mautrix/linkedin/blob/main/pkg/connector/backfill.go)).
  **(Whether LinkedIn caps very old history is unverified.)**
- **Contacts.** Participants' profiles and a people search API (updated in v26.09). No connections-list sync.
- **Actions** ([`capabilities.go`](https://github.com/mautrix/linkedin/blob/main/pkg/connector/capabilities.go)):

  | Action | Support |
  |---|---|
  | Send | Text (≤8,000), images, files, video (≤20 MB) |
  | Reply | Yes |
  | Edit | ≤60 min |
  | Delete ("recall") | ≤60 min, for everyone |
  | Reactions | Yes |
  | Read receipts and typing | Yes |
  | Groups | Create, rename, add and remove participants |
  | Block | Yes |

- **Risks.**
  - LinkedIn's User Agreement §8.2 and its "Prohibited software and extensions" page forbid bots or automated
    methods to "access the Services, add or download contacts, **send or redirect messages**"
    ([LinkedIn help](https://www.linkedin.com/help/linkedin/answer/a1341387)).
  - Restrictions land on a *professional identity*.
  - Look like a person: the same UA as the browser the cookies came from, a residential IP, no bulk sends, a
    human-in-the-loop for outbound messages.
- **Recommendation.** Use it, opt-in, defaulting to read-and-reply only.

### 5.3 Dead or unsuitable options

- **linkedin-api** (Tom Quirk, Python, MIT, Voyager API): **the GitHub repo `tomquirk/linkedin-api` now returns
  404** (checked 2026-11-09). Last PyPI release **2.3.1 on 2024-11-07**. Its messaging calls (`get_conversations`,
  `send_message`, `mark_conversation_as_seen`) used legacy Voyager messaging endpoints that LinkedIn has since
  replaced with GraphQL `messengerConversations` (inferred from linkedingo's endpoints). **Dead.**
- **Texts `platform-linkedin`** (MIT): archived.
- **beeper/linkedin-messaging-api** (Python): superseded by `linkedingo`.

### 5.4 Option C: Unipile (commercial, hosted)

- Pricing: **€5/account/month** (11–50 accounts band) with a **€49/month minimum** that includes up to 10
  accounts. No per-request fees ([pricing](https://www.unipile.com/pricing-api/)).
- Channels: LinkedIn, WhatsApp, Instagram, Telegram and email. **X and Messenger are "coming soon".**
- Your session cookies live on Unipile's servers and requests leave from their infrastructure. That contradicts
  self-hosting, and the ToS risk still lands on the user's account.
- Only for users who refuse to run a sidecar.

---

## 6. Bluesky DMs (official; full detail)

### 6.1 Architecture

- DMs launched **22 May 2024**
  ([MarshalX docs](https://github.com/MarshalX/atproto/blob/main/docs/source/guides/direct-messages.md)).
- They are **not in the user's atproto repo** and **not E2EE**: the lexicons carry plaintext `text`.
- They live on a centralized chat service: host `https://api.bsky.chat`, service DID
  **`did:web:api.bsky.chat#bsky_chat`** ([API directory](https://github.com/bluesky-social/bsky-docs/blob/main/docs/advanced-guides/api-directory.mdx)).
- Clients call **their own PDS**, which proxies the call. Every chat request carries the header
  **`atproto-proxy: did:web:api.bsky.chat#bsky_chat`**.

### 6.2 Authentication

1. **App password with DM access.**
   - The user creates an app password at `bsky.app/settings/app-passwords` and ticks **"Allow access to your
     direct messages"**. The grant is baked into that password; without it every chat call fails with "Bad
     token scope".
   - Call `com.atproto.server.createSession {identifier, password}` to get a short-lived `accessJwt` and a
     long-lived `refreshJwt`, then keep the session alive with `com.atproto.server.refreshSession`. **(Exact token
     lifetimes unverified.)**
   - `createSession` is limited to **30 per 5 min and 300 per day per account**
     ([rate limits](https://github.com/bluesky-social/bsky-docs/blob/main/docs/advanced-guides/rate-limits.md)).
     Persist the session; don't log in per run.
2. **OAuth** (atproto OAuth: PAR, PKCE, DPoP-bound tokens).
   - Transitional scopes: `atproto transition:generic transition:chat.bsky`. `transition:chat.bsky` is "equivalent
     to adding the 'DM Access' toggle for App Passwords" and "does not function without `transition:generic`"
     ([OAuth spec](https://github.com/bluesky-social/atproto-website/blob/main/src/app/%5Blocale%5D/specs/oauth/en.mdx)).
   - Granular permissions: the permission set
     **`include:chat.bsky.authFullChatClient?aud=did:web:api.bsky.chat%23bsky_chat`** grants `rpc` on every
     `chat.bsky.*` method, with the audience inherited
     ([lexicon](https://github.com/bluesky-social/atproto/blob/main/packages/api/lexicons/chat/bsky/authFullChatClient.json),
     [permission spec](https://github.com/bluesky-social/atproto-website/blob/main/src/app/%5Blocale%5D/specs/permission/en.mdx)).
   - Individual methods can be granted instead:
     `rpc:chat.bsky.convo.getLog?aud=did:web:api.bsky.chat%23bsky_chat`.
   - OAuth is the better product UX, because substrate never sees a password. It requires substrate to publish a
     client-metadata document.

### 6.3 Operations

From [`lexicons/chat/bsky`](https://github.com/bluesky-social/atproto/tree/main/packages/api/lexicons/chat/bsky).
Recent lexicon updates landed in Jun–Aug 2026; group chats are new.

| Area | Methods |
|---|---|
| Conversations | `convo.listConvos` (limit ≤100, `cursor`, filters `readState`, `status`=request/accepted, `kind`=direct/group, `lockStatus`), `convo.getConvo`, `convo.getConvoForMembers` (get or create a 1:1), `convo.getConvoAvailability`, `convo.getConvoMembers`, `convo.listConvoRequests`, `convo.acceptConvo`, `convo.leaveConvo`, `convo.muteConvo`/`unmuteConvo`, `convo.lockConvo`/`unlockConvo`, `convo.getUnreadCounts` |
| Messages | `convo.getMessages` (limit ≤100, `cursor`), `convo.sendMessage`, `convo.sendMessageBatch`, `convo.deleteMessageForSelf`, `convo.addReaction`/`removeReaction`, `convo.updateRead` (up to a `messageId`), `convo.updateAllRead` |
| Events | **`convo.getLog(cursor)`** returns `{cursor, logs[]}` with `logBeginConvo`, `logAcceptConvo`, `logLeaveConvo`, `logMute/UnmuteConvo`, **`logCreateMessage`**, **`logDeleteMessage`**, **`logAddReaction`/`logRemoveReaction`**, **`logReadConvo`** (`logReadMessage` is deprecated), group events (`logAddMember`, `logRemoveMember`, `logMemberJoin/Leave`, `logEditGroup`, `logLockConvo`…), join-link and join-request events |
| Groups | `group.createGroup {members, name}`, `group.addMembers`, `group.removeMembers`, `group.editGroup`, join links (`createJoinLink`, `enable`/`disable`/`editJoinLink`, `getJoinLinkPreviews`), join requests (`requestJoin`, `approveJoinRequest`, `rejectJoinRequest`, `listJoinRequests`, `withdrawJoinRequest`), `listMutualGroups` |
| Account | `actor.declaration` record (`allowIncoming`: all/none/following; `allowGroupInvites`), `actor.getStatus`, `actor.deleteAccount`, **`actor.exportAccountData`** (JSONL export of the chat account) |

### 6.4 Data shapes

From [`convo/defs.json`](https://github.com/bluesky-social/atproto/blob/main/packages/api/lexicons/chat/bsky/convo/defs.json):

- `messageInput {text (≤1,000 graphemes / 10,000 bytes), facets[] (mentions, links), embed (app.bsky.embed.record | chat.bsky.embed.joinLink), replyTo {messageId}}`
- `messageView {id, rev, text, facets, embed, reactions[{value, sender{did}, createdAt}], replyTo (embedded view), sender{did}, sentAt}`, plus `deletedMessageView` and `systemMessageView`
- `convoView {id, rev, members[profileViewBasic{did, handle, displayName, avatar…}], lastMessage, lastReaction, muted, status, unreadCount, kind (directConvo | groupConvo)}`

### 6.5 Realtime and catch-up

- There is **no push channel**: clients **poll `getLog`** with the last cursor. mautrix-bluesky polls every **5 s**
  and persists the cursor in the login metadata
  ([`client.go`](https://github.com/mautrix/bluesky/blob/main/pkg/connector/client.go)).
- After downtime, resume from the stored cursor. **How long the chat service keeps the log is undocumented
  (unverified).** If a cursor is rejected, reconcile with `listConvos` (compare each convo's `rev`) and
  `getMessages`.

### 6.6 History, contacts and actions

- **History.** `getMessages` pages backwards to the beginning. `exportAccountData` gives a bulk JSONL dump.
  **(Retention unverified.)**
- **Contacts.** Convo members come as `profileViewBasic` (DID, handle, display name, avatar). The social graph
  comes from `app.bsky.graph.getFollows` / `getFollowers` and `app.bsky.actor.getProfiles` on the AppView.
- **Actions.**

  | Supported | Not supported |
  |---|---|
  | Send text with facets | Edit |
  | Quote-embed a post | Typing indicators |
  | Reply (`replyTo`) | Media attachments (images and files are only possible by embedding a post) |
  | React (add/remove) | Delete for everyone (only `deleteMessageForSelf`) |
  | Mark read | |
  | Accept requests, mute, leave | |
  | Full group management | |

### 6.7 Libraries and options

- [**MarshalX `atproto`**](https://github.com/MarshalX/atproto) (Python, MIT, 661★, v0.0.73 on 2026-10-17):
  `dm = client.with_bsky_chat_proxy().chat.bsky.convo`, then `dm.get_log(...)`, `dm.send_message(...)` and so on.
  It fits **substrate's sandboxed Python functions** directly.
- [**indigo**](https://github.com/bluesky-social/indigo) (Go, MIT/Apache-2.0 dual, 1.4k★, pushed 2026-11-07):
  generated `api/chat` package (`chat.ConvoGetLog`, `ConvoSendMessage`, `GroupCreateGroup`…). Embeddable in
  `substrated`.
- **@atproto/api** (TypeScript) is the reference client.
- [**mautrix-bluesky**](https://github.com/mautrix/bluesky) (Go, AGPL, 45★): last release **v25.10 (2025-10-16)**,
  last commit 2026-05-16 (dependency bump). It is in maintenance mode, uses password or app-password login, and
  polls `getLog` every 5 s. **Not needed:** the API is official, and indigo is permissive.

**Sketch** (Python function run on a schedule):

```python
from atproto import Client
c = Client(); c.login(handle, app_password_with_dm_access)        # or restore an exported session string
dm = c.with_bsky_chat_proxy().chat.bsky.convo
res = dm.get_log({"cursor": state.cursor})                         # logs[] + next cursor
for e in res.logs: handle(e)                                       # logCreateMessage, logAddReaction, logReadConvo…
dm.send_message({"convoId": cid, "message": {"text": "hi", "replyTo": {"messageId": mid}}})
dm.update_read({"convoId": cid, "messageId": mid})
```

**Risks.** Official. Respect rate limits (PDS: 3,000 requests per 5 min per IP; the chat service's limits are
undocumented) and Bluesky's developer guidelines. Make polling adaptive: every few seconds while a
conversation is active, minutes when idle.

---

## 7. Logging in from the native app shells (web view) and the IP question

bridgev2's cookie login step already encodes what a web view needs to do: open a URL, optionally force a user
agent, wait for a URL regex, then extract named cookies, headers or JS results. Our iOS and Android shells can
do exactly this.

| Network | Open | Done when the URL matches | Collect | UA |
|---|---|---|---|---|
| Messenger | `https://www.messenger.com/?no_redirect=true` | `^https://www\.messenger\.com/(?:e2ee/)?(?:t/[0-9]+/?)?(?:\?.*)?$` | `c_user`, `xs`, `datr` (+`sb`) | Must match the sidecar's (mautrix: Chrome 141 on Linux) |
| Facebook | `https://www.facebook.com/` | `^https://www\.facebook\.com/(?:messages/(?:e2ee/)?(?:t/[0-9]+/?)?)?(?:\?.*)?$` | same, on facebook.com | same |
| Instagram | `https://www.instagram.com/accounts/login/` | `^https://www\.instagram\.com/(?:direct/(?:inbox/\|t/[0-9]+/)?\|accounts/onetap/)?(?:\?.*)?$` | `sessionid`, `csrftoken`, `ds_user_id` (+`mid`, `ig_did`, `rur`, `shbid`, `shbts`) | not forced |
| X (cookies) | `https://x.com/login` | logged-in home | `auth_token`, `ct0` (password flow: a Castle token from a hidden web view) | Chrome |
| LinkedIn | `https://www.linkedin.com/login` | feed or messaging | the full `Cookie` header (`li_at`, `JSESSIONID`, …), `x-li-track`, `x-li-page-instance`, `navigator.userAgent` | **must match** |
| X (official) / Bluesky | OAuth in the system browser (`ASWebAuthenticationSession` / Custom Tabs) | redirect URI | tokens (+ the XChat PIN entered on device) | n/a |

**Notes.**
- `WKHTTPCookieStore.getAllCookies` (iOS/macOS) and Android `CookieManager` both expose **HttpOnly** cookies.
- Use a non-persistent web-view data store and log the web view out, or discard it, after extraction. That
  avoids a second live session competing with the server, mirroring mautrix's "use a private window and close
  it" advice.
- **IP and fingerprint consistency matters more than anything else for Meta and LinkedIn.**
  - A login from the phone's mobile IP followed by traffic from a VPS IP is the classic "suspicious login".
  - Prefer running the sidecar on the user's home network, or pin a residential proxy in the user's region
    (`proxy`, `get_proxy_from`).
  - Or proxy the login HTTP through the device (bridgev2 `LoginStartParams.HTTP`).

---

## 8. Comparison table

| Option | Network | Lang / license | Maintenance (last release) | Auth as user | History | Realtime / catch-up | Roster | Actions | Ban / ToS risk | Fit for substrate |
|---|---|---|---|---|---|---|---|---|---|---|
| **mautrix-meta (`messagix`)** | Messenger | Go / AGPL-3.0 | Very active (v26.10, 2026-10-16) | Cookies via web view, or user/password (Messenger iOS/Android emulation); PIN for E2EE history | Unencrypted: full paging. E2EE: full from secure storage with the PIN (v26.10) | DGW websocket + LS sync cursors; whatsmeow offline queue | Participants and lookups; no friends sync | Send, media, reply, react, edit, unsend, read, typing, polls, groups, block | **High** (documented checkpoints) | AGPL sidecar |
| **mautrix-instagram (`instameow`)** | Instagram | Go / AGPL-3.0 | Very active (new bridge since v26.07) | Cookies, or user/password (Android API, v26.10) | Cursor paging (20/page); deep backfill bug #374 | DGW + `seq_id` replay; resnapshot | Participants and lookups | Send, media, reply, react, edit, unsend, read, typing, groups | **High** | AGPL sidecar |
| **instagrapi** | Instagram | Python / MIT | Very active (3.2.8, 2026-11-09) | User/password + TOTP; settings blob | Cursor paging | Experimental MQTToT/Iris + FBNS | followers/following (risky) | Send, media, reply, react, unsend, seen, typing | **Very high** | Python functions for reads; sidecar for realtime |
| Instagram API w/ IG Login | Instagram (pro accounts) | Official | n/a | OAuth (pro accounts only) | 20 most recent per conversation | Webhooks | none | Reply within 24h; no groups | none | Not "as the user" |
| **X Chat API + Chat XDK** | X | Official / XDK MIT | Active (XDK 0.7.3, 2026-11-05) | OAuth 2.0 PKCE + XChat PIN (unlock existing keys) | Events API newest-first (depth unverified); legacy 30 days | Activity webhooks; stream backfill ≤5 min; reconcile via events | following/followers ($0.001 owned reads) | Send, media, reply, react, delete, read, typing, groups (edit via XDK, unverified) | ToS-clean; **cost** | Webhook → function (Python XDK) or Go |
| **mautrix-twitter** | X | Go / AGPL-3.0 | Very active (v26.09) | Cookies, or password (Castle token via hidden web view) + PIN | XChat + REST fallback | XChat websocket + 10 s REST polling; checkpointed catch-up | Participants | Send, media, reply, react, edit, delete, read, typing, groups | **Medium–high** | AGPL sidecar |
| twikit | X | Python / MIT | **Unmaintained** (2.3.1, 2025-02) | Cookies, login | Legacy only | none | n/a | Legacy DMs only | High | ✗ |
| **mautrix-linkedin (`linkedingo`)** | LinkedIn | Go / AGPL-3.0 | Active (v26.09) | Cookie header + headers + UA, or email/password | Cursor paging to start | SSE realtime; sync-token catch-up | Participants, search | Send, media, reply, react, edit/delete ≤60 min, read, typing, groups | **High** (ToS §8.2) | AGPL sidecar, opt-in |
| linkedin-api | LinkedIn | Python / MIT | **Dead** (repo 404; 2.3.1, 2024-11) | Credentials/cookies | Legacy | none | Connections | Legacy | High | ✗ |
| Unipile | LinkedIn, IG (+X/FB soon) | SaaS | Commercial | Hosted wizard | Vendor-defined | Webhooks | Vendor | Vendor | High (moved to vendor IPs) | ✗ for self-hosting |
| **Bluesky chat API** | Bluesky | Official; MarshalX MIT / indigo MIT+Apache | Active | App password with DM access, or OAuth (`transition:chat.bsky` / permission set) | `getMessages` to start; JSONL export | Poll `getLog` cursor | Convo members + graph | Send, reply, react, read, groups; **no** edit, typing, media, unsend-for-all | none | **Plain Python functions** |
| mautrix-bluesky | Bluesky | Go / AGPL-3.0 | Maintenance (v25.10) | Password | Paging | `getLog` poll 5 s | Members | Bridge subset | none | Unnecessary |

---

## 9. Ranked recommendations per network

### Facebook Messenger
1. **mautrix-meta / `messagix` as an AGPL sidecar.** It is the only maintained option that handles E2EE, which
   is now the default for 1:1 chats, and the only one that can restore encrypted history (PIN). Run it from the
   user's residential IP or a pinned residential proxy, with 2FA on and paced backfill. Default to cookie login
   from the native web view, with Messenger-Android password login as an alternative.
2. **Keep the Beeper provider** as a zero-risk read path for users who already run Beeper. (Beeper itself runs
   mautrix-meta, so the account risk is the same.)
3. Nothing else: fbchat-v2 wraps the same AGPL code with one maintainer, fbchat-muqit can't message users, and
   the rest are dead.

### Instagram
1. **mautrix-instagram (`instameow`) as an AGPL sidecar**: the protocol that matches today's web client, with
   `seq_id` catch-up and a native Android login. Watch #374 (deep backfill). Expect churn: the bridge is four
   months old and Meta changed protocols twice this year.
2. **instagrapi** (MIT) only if the AGPL sidecar is a non-starter, used read-mostly with a persisted settings
   blob from the user's IP. It is the most ban-prone choice.
3. Official Instagram API only for users who *want* a professional-account customer inbox (24h replies, 20
   recent messages). Not a personal-DM connector.

### X / Twitter
1. **Official X Chat API + Chat XDK.** ToS-clean, end-to-end decryption under the user's own keys (the PIN unlocks
   the existing identity), webhooks map onto substrate's webhook triggers, a Python binding for functions and a
   Go binding for the server. Costs about $0.01 per received or read message and $0.015 per send; each self-hoster
   needs a developer account with credits. Store the exported key blob sealed. Treat the PIN as root.
2. **mautrix-twitter as an AGPL sidecar**, for users without a developer account, or if a gap appears (for
   example history depth, if `/2/chat` turns out shallow). It covers XChat with the PIN.
3. Not twikit or other legacy scrapers: they can't see XChat.

### LinkedIn
1. **mautrix-linkedin (`linkedingo`) as an AGPL sidecar**, behind an explicit risk acknowledgment. Default to
   mirror and reply-with-confirmation, no automated outbound messages. Match the UA and IP to the user's browser.
2. **Unipile** for users who accept a paid third party holding their session (€49/month minimum).
3. Via Beeper (read-only) for users already there. Do **not** build on linkedin-api.

### Bluesky
1. **Official `chat.bsky.*` API from substrate's own Python functions** (MarshalX `atproto`), or from Go
   (indigo). Use OAuth with `include:chat.bsky.authFullChatClient?aud=did:web:api.bsky.chat%23bsky_chat` when
   substrate can host client metadata; otherwise an app password with DM access. Poll `getLog`, persist the
   cursor, and backfill with `getMessages` or `exportAccountData`. No sidecar, no ToS risk.
2. mautrix-bluesky is unnecessary. It is AGPL and in maintenance mode.

---

## 10. Unverified items and open questions

- Meta: how long undelivered E2EE Messenger messages stay queued server-side, which bounds catch-up after a
  long sidecar outage when no PIN or secure storage is enrolled.
- Meta: whether the pre-encryption ("open") thread history backfill returns in the next mautrix-meta release (it
  is in `main`, not in v26.10).
- Meta: whether `GetContactsTask` ("search_contacts") returns the full friends list. The bridges implement no
  roster sync.
- Beeper's current Messenger history-import limits, taken from search snippets; the help center couldn't be
  fetched.
- X: retention depth of `/2/chat/conversations/{id}/events`; the billing category and rate limits of `/2/chat/*`
  (forum reports only); how edits are encoded (no edit endpoint in the OpenAPI); the exact date XChat replaced
  DMs for everyone (secondary sources say about Dec 2025).
- X: whether loading a user's existing XChat keys into a server conflicts with any X developer policy. The docs
  warn against third-party servers, but describe the "bot on your infrastructure" pattern.
- Bluesky: chat-service rate limits, `getLog` cursor retention, token lifetimes.
- LinkedIn: whether old message history is capped server-side; `li_at` lifetime.
- All cookie lifetimes except Meta's documented `c_user`/`xs` (365 days) and `datr` (400 days).
- The licensing conclusions (§1) are the standard FSF reading of "aggregate". Have counsel confirm before
  shipping.
