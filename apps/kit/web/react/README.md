# @substrate/react

React for a Substrate web app: the signed-in shell, the record and write
hooks, Settings' sections, and the screen pieces in the Apple apps' grouped
style. Import `@substrate/react/theme.css` after Tailwind 4 and set the
app's accent on `:root` as `--sx-accent` and `--sx-accent-text`; the theme
names the kit's own components as a Tailwind source, so an app's stylesheet
names no path into the kit.

An app imports this package alone: `defineKind`, the property readers
(`stringProp`, `numberProp`, `booleanProp`, `refProp`), `webManifest` and
`packageDetail` are re-exported from `@substrate/client` and
`@substrate/auth`.

## The shell

```tsx
<SubstrateApp manifest={manifest} kinds={[Item]} kept="items" tagline="…" icon={<img … />}>
  <App />
</SubstrateApp>
```

Until a session is kept it shows the connect screen: the repository or
server, checked against the app's requirement ("Server found", or the
server too old for the app with what it cannot do yet, Try Again and Use
Another Server), then the server's own sign-in. Signed in, it checks the
app's standing at once and when the page comes back to the front, ends the
session on a 401 or when the approved time is up (unsent writes stay for
the next sign-in), and retries the revocations a sign-out still owes.

Every tab of an app runs on the one session `localStorage` keeps. A
sign-out in one tab closes the repository in the others and drops their
records; a sign-in in one opens it in the others. A tab that slept through
either (a background tab, a page back from the cache) looks again when it
comes to the front, and until then its requests carry no token that is no
longer the kept one, so the server refuses them and the tab ends.

A sign-out keeps the token to revoke before it clears the session, so a
revocation that meets no server leaves the token held by the queue rather
than by nothing, then forgets what this browser kept for the repository
(the outbox and the saved records) while it revokes. Forgetting that
another tab still holds open is said on the connect screen. Where the
browser will not keep the token, it is revoked before the session is
cleared, and when the server cannot confirm that either, `signOut` rejects
with `NotSignedOut` and the app stays signed in (Settings says so beside
Sign Out). A tab that slept through another tab's sign-in signs out only
its own token: the other session stands, the tab takes it up, and the
repository's data stays where it is that session's too. One sign-out runs
at a time.

`SubstrateProvider` is the same without the screens, for an app with its
own connect screen: its children read `useConnection()` (the `phase`,
`signingIn`, `signedOut`, `opening` or `open`, the `problem`, the
`unrevoked` tokens, `signIn(located)` and the app status) and render the
records once the phase is `open`. Wrap it in a `ToastHost` to hear writes
that were not kept.

## Hooks

| Hook                                 | Answers                                                                                                                                               |
| ------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `useRecords(Item)`                   | every record of a kind, read through its codec: the same array until a record of that kind changes, each value the same until its record does         |
| `useRecord(Item, id)`                | one record: `{ state: "loading" }` until it is known absent, `{ state: "absent" }`, or `{ state: "present", value }`; past a read's limit, read alone |
| `useListed(Item)`                    | whether a kind's records were read whole, or kept from such a read: "none yet" against "none"                                                         |
| `useWriteState(Item, id)`            | `pending` while a record's write waits to be sent, `parked` while one was refused and waits for the owner                                             |
| `useKindWrites(Item)`                | `create(draft)`, `change(id, draft)`, `edit(id, draft, seenVersion)`, `remove(id)`, each draft written through the kind's codec                       |
| `useWrites()`                        | the same writes by kind reference and properties, for a kind the app does not declare                                                                 |
| `useLinkedStatus()`                  | how the reading stands (loading, live, reconnecting and why), and how many records wait or were refused; a new object only when one of these changes  |
| `useOutbox()` / `useParkedWrites()`  | the outbox's summary, and the writes refused                                                                                                          |
| `useStatusSentence()`                | where the app stands, in Settings' one sentence                                                                                                       |
| `useAppStatus()`                     | the last status check, and `refresh`                                                                                                                  |
| `useConnection()` / `useSubstrate()` | the connection, signed in or not; the open repository (manifest, session, client, linked records, outbox, `signInAgain`, `signOut`)                   |

A change draws only the components that read it: a record of one kind
leaves the readers of another alone, and the connection, the app status and
the outbox are apart from the records, so a status check never draws a
list again. A kind's `read` is run once per record object and its value
kept while the object stands, so it must depend on the record alone: never
on the time, the settings or anything else outside it, which a screen
applies to the values it reads instead.

A write resolves once this browser has it: `{ kept: true, id, shown }`, the
record as the owner now sees it, or `{ kept: false, words }`. **Kept is not
landed.** The write waits in the outbox and is sent when the server can be
reached; one the server refuses is parked for the owner (Settings' Refused
by Your Substrate, `RefusedBanner`). A codec that throws, and storage that
would not keep the write, are both not kept, said in a toast, or beside a
form with `{ onNotKept }`:

```tsx
const written = await writes.create({ name }, { onNotKept: setProblem })
if (written.kept) setDraft("") // the outbox has it: the draft may go
```

`shown` is undefined after a delete, for a kind the app does not read, and
for a change to a record not read here; the write is kept all the same. A
create leaves out properties given null, which hold nothing on a record not
made yet.

## Settings

`<SubstrateSettings kindLabels={{ [Item.kind]: "Item" }} />` draws every
section the kit gives an app, in the Swift kit's order and words: Your
Substrate (the status sentence, each package, Send Now, Review Access…,
Renew Access…), Refused by Your Substrate (each write with its kind, record
ID and the server's words, Retry and Discard, or Keep My Edit and Use
Theirs), Repository (server, repository, version, Sign Out), Approved
access (each scope in words and as carried, until when, Change Access…),
Storage and About. Each is also its own component. `<AccessBanner />` puts
the one issue the owner should act on now at the top of a screen, and
`<RefusedBanner onOpenSettings={…} />` the changes refused.

## Pieces

`Section`, `Row`, `NoteRow`, `VerbatimRow`, `ProblemBox`, `ActionRow`,
`SelectRow`, `ToggleRow`, `Field`, `NavBar`, `BarButton`, `LargeTitle`,
`TabBar` (a labelled `nav`), `SidebarItem`, `LinkStatus`, and three modals
on Base UI's dialog: `Sheet`, `Alert` (a title, words, a field, and its
answers) and `Confirm`. A screen has one `h1`: its `LargeTitle`, else the
`NavBar`'s title; a bar beside a large title takes `heading={false}`.

Each modal renders in a portal, locks the page's scroll, hides the rest of
the page from assistive technology and keeps focus inside it, closes on
Escape (only the topmost), and gives focus back to what opened it. Focus
starts on the field marked `autoFocus`, else on an alert's Cancel, else on
the first control. An outside press closes a `Sheet` unless it is
`unsaved`, which an editor sets while it holds input not saved, so a stray
tap never drops an edit; Cancel and Escape still close it. An alert never
closes on one. Motion honours `prefers-reduced-motion`.
