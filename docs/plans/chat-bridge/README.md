# Research for the chat bridge plan

The evidence behind [the chat bridge plan](../chat-bridge.md): five reports,
written on 2026-11-09 by reading each project's source, documentation and
issue tracker. Each marks what it could not confirm as unverified, ends with a
comparison table and a ranked recommendation, and links its sources inline.
They describe the world on that date; the projects they cover release monthly,
so check a fact against its source before relying on it.

| Report | Covers |
| --- | --- |
| [01-unified-frameworks.md](01-unified-frameworks.md) | mautrix bridgev2 (its interfaces, running it without a Matrix server, the connector catalogue, licensing), Beeper's Desktop API and Beeper Server today, the Texts.com SDK, Matterbridge, Unipile, the messaging MCP servers, and the common shape of a messenger connector and its sign-in flows |
| [02-whatsapp-signal.md](02-whatsapp-signal.md) | WhatsApp as a linked device (whatsmeow and the libraries built on it, history sync, app state, LIDs, ban risk) and Signal as a linked device (signal-cli, signalmeow, presage, the link-time history transfer) |
| [03-telegram-discord.md](03-telegram-discord.md) | Telegram as a user client (gotd, TDLib, Telethon, mautrix-telegram's Go rewrite, the API terms) and Discord as a user account (the terms, the user-token libraries, the data package) |
| [04-meta-social.md](04-meta-social.md) | Messenger and Instagram on personal accounts (mautrix-meta, mautrix-instagram, instagrapi), X's XChat, LinkedIn and Bluesky's chat API, and signing in from the apps' web view |
| [05-device-and-longtail.md](05-device-and-longtail.md) | iMessage and SMS on the Mac, Android SMS and RCS through Google Messages, Viber, LINE, WeChat, KakaoTalk, Google Chat, Teams, Threema, Snapchat, Matrix and the rest |
