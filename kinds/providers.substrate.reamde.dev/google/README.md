# The Google bundle

Package `providers.substrate.reamde.dev/google`: an OAuth provider that
mirrors a Google account's address book, mail, calendars and Drive files into
the repository, in Google's own shape. Four streams (contacts, gmail,
calendar, drive) share one account, each with its own toggle, scope, sync
function and cursor. Read only: nothing is written back to Google.

`bundle.yaml` is the closure (the config and account kinds, the ten mirrors,
the `emailaddress` hub, the `calendarsync` state kind, the four sync functions
and the trusted `oauth2:` metadata) and `triggers.yaml` is the delivery wiring
(three triggers per stream). They are the contract; this file is not.

## Kinds

| Kind | One row per |
| --- | --- |
| `config` | the OAuth client id, its secret and the API base the syncs call |
| `account` | one connected Google account, its toggles and its cursors |
| `emailaddress` | one mailbox address, the hub all four streams converge on |
| `contact` | one People `Person`, from the address book or the directory |
| `contactgroup` | one `ContactGroup` a contact is filed under |
| `gmailthread` | one Gmail thread |
| `gmailmessage` | one Gmail message, its MIME tree included |
| `gmailattachment` | one message part carrying a filename |
| `gmaillabel` | one Gmail label |
| `calendar` | one `calendarList` entry |
| `calendarsync` | one calendar's own drain state |
| `calendarseries` | one recurring master, its rule decoded |
| `calendarevent` | one single event, or one exception of a series |
| `drivefile` | one Drive file, with the export text of a Doc, Sheet or deck |

What it writes, what it deliberately does not, the scopes, `backfillDepth`,
how each stream walks and how to connect it:
[docs/bundles-catalog.md#google](../../../docs/bundles-catalog.md#google).
