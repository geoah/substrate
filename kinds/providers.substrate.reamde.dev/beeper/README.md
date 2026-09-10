# The Beeper bundle

Package `providers.substrate.reamde.dev/beeper`: a non-OAuth provider that
connects a Beeper (Matrix) homeserver with a pasted access token and mirrors
the bridged rooms and messages (WhatsApp, Telegram, Signal, iMessage and the
rest) into `room` and `message` records. It reads; it never sends.

`bundle.yaml` is the closure (the config and account kinds, the two mirrors and
the sync function) and `triggers.yaml` is the delivery wiring. They are the
contract; this file is not.

What it writes, where the token comes from, the `roomFilter`, and how deep
history arrives:
[docs/bundles-catalog.md#beeper](../../../docs/bundles-catalog.md#beeper).
