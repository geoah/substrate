# @substrate/react

React for a Substrate web app: the signed-in shell, the record and write
hooks, Settings' sections, and the screen pieces in the Apple apps' grouped
style. Import `@substrate/react/theme.css` after Tailwind 4 and set the
app's accent on `:root` as `--sx-accent` and `--sx-accent-text`.

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

## Hooks

| Hook                                            | Answers                                                                            |
| ----------------------------------------------- | ---------------------------------------------------------------------------------- |
| `useRecords(Item)` / `useRecord(Item, id)`      | records of a kind read through its codec, current with the feed and the outbox     |
| `useKindWrites(Item)`                           | `create(draft)`, `change(id, props)`, `edit(id, props, seenVersion)`, `remove(id)` |
| `useWrites()`                                   | the untyped `create`, `patch`, `remove`                                            |
| `usePending(Item, id)` / `useRefused(Item, id)` | whether a record's write waits, or was refused                                     |
| `useOutbox()` / `useParkedWrites()`             | the outbox's summary, and the writes refused                                       |
| `useLinkStatus()`                               | where the app stands, in Settings' one sentence                                    |
| `useAppStatus()`                                | the last status check, and `refresh`                                               |
| `useLive()`                                     | the linked records' snapshot                                                       |
| `useSubstrate()`                                | the manifest, session, client, outbox, `signInAgain`, `signOut`                    |

A write resolves once the outbox keeps it, with the record as it now shows;
null (false for a remove) only when the browser could not keep it. Pass
`{ onNotKept }` to say why beside a form instead of in a toast:

```tsx
const id = await writes.create({ name }, { onNotKept: setProblem })
if (id) setDraft("") // kept: the draft may go
```

## Settings

`<SubstrateSettings kindLabels={{ [Item.kind]: "Item" }} />` draws every
section the kit gives an app, in the Swift kit's order and words: Your
Substrate (the status sentence, each package, Send Now, Review Access…,
Renew Access…), Refused by Your Substrate (each write with its kind, record
ID and the server's words, Retry and Discard, or Keep My Edit and Use
Theirs), Repository (server, repository, version, Sign Out), Approved
access (each scope in words and as carried, until when, Change Access…),
Storage and About. Each is also its own component. `<AccessBanner />` puts
the one issue the owner should act on now at the top of a screen.

## Pieces

`Section`, `Row`, `NoteRow`, `VerbatimRow`, `ProblemBox`, `ActionRow`,
`SelectRow`, `ToggleRow`, `Field`, `NavBar`, `BarButton`, `LargeTitle`,
`TabBar`, `SidebarItem`, `LinkStatus`, and two modals: `Sheet` and
`Confirm`, which take focus when they open (Confirm on Cancel), keep Tab
inside, close on Escape (only the topmost), and give focus back. Motion
honours `prefers-reduced-motion`.

## Deprecated

`LiveCollection` with its `apply` and `landed` (write through `useWrites`),
`useSubstrate().collection` (use `linked`), `AccountSection` (use
`SubstrateSettings`, or `RepositorySection` and `ApprovedAccessSection`),
and the `LiveSnapshot` and `LiveStatus` names (`LinkedSnapshot`,
`LinkedStatus` in `@substrate/local`).
