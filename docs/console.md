# Web console

The console is the substrate in a browser: a single-page app served by the
substrate itself at `/`, talking to the same public surface as every other
client. It reads the whole repository and writes through the same public verbs,
so nothing it does needs an endpoint no other client has.

Signing in is the same exchange [substratectl](substratectl.md) makes: the
repository, the password, and the current 6-digit code where the deployment
asks for one ([users and tokens](auth.md)). The console then holds a token
exactly like a script does: a session is its [token record](auth.md#tokens),
which is why signing out revokes it. A substrate that is open for registration
also serves a registration page at `/register`, with the invite code as its
first field where the substrate reads one.

## What it is for

The console is built around four things, and Home opens on one card for each:

- **Your data**: the collections you keep, and the copies your providers
  bring in. A collection is every record of one kind.
- **Providers**: the services that bring data in and keep it up to date.
- **Agents**: the assistants that work on your data, in a chat.
- **Tools**: everything that can act on your data besides you, each saying
  what it may see and change and when it runs.

Everything else (History, Search, Settings) serves those four.

## Navigation

The sidebar holds, top to bottom: the repository name, a search button that
opens ⌘K, the five places (**Home**, **All data**, **Agents**, **Tools**,
**Providers**, with the number of providers added beside the last),
**Favorites**, the collections, and at the foot **History**, **Settings**, the
**Technical details** switch and the account menu (Account and settings, the
theme, and Sign out).

The collections are sorted by where they come from, and every surface that
lists collections (the sidebar, ⌘K, All data, Home) uses the same sections:

- **Your data**: the repository's own authority first, then every other
  authority that is neither a provider nor the substrate's own. This is what
  you create and change.
- **From _Provider_**: one section per provider (**From Google**, **From
  GitHub**), holding the copies that provider keeps up to date.
- **Substrate**: the kinds under `substrate.reamde.dev`, the substrate's own
  machinery, listed only with Technical details on.

What a section lists is decided by each kind's declared
[`purpose`](vocabulary.md#the-reserved-keys)
([0104](decisions/0104-a-kind-declares-its-purpose.md)): `primary` is a thing
you browse and open directly, `supporting` is a detail of another kind,
reached from the records it belongs to, and `internal` is machinery. A kind
that declares none reads as `primary`, so a kind you or an agent declares is
listed without anyone classifying it, and every kind under
`substrate.reamde.dev` reads as `internal`. In everyday mode a section lists
its primary collections only. A section heading folds its section away and
remembers it; provider sections start folded, the others open. Hovering a
collection shows a star that adds it to **Favorites**, above the sections,
where up and down controls reorder it.

**⌘K** (Ctrl-K elsewhere) jumps to a page or a collection, or hands what you
typed to the [Search page](#search) as a records query. It lists collections
the way the sidebar does: primary ones in everyday mode, every kind with its
purpose labelled with Technical details on.

The header above every page is a breadcrumb that reads as where the page sits:
`Your data / Tasks` or `From Google / Contacts` for a collection, then the
record's title; with Technical details on it spells the kind reference segment
by segment and ends in the record id.

### Technical details

The **Technical details** switch, at the foot of the sidebar and on the
Settings page, is one setting for the whole console. Off, the console speaks
in everyday words. On, it adds what a developer or an agent author needs,
without taking anything away:

- every kind, supporting and internal ones included, and the **Substrate**
  section; the sidebar becomes the authority → package → kind tree, each kind
  by its own name and tagged with its purpose when that is not `primary`;
- full kind references, record ids and actor ids beside the names, with a
  copy button, and the declaration's own description of a kind instead of the
  one-line summary;
- property keys beside their labels, a kind's **Definition**, a record's
  **Record** section and its YAML source;
- History's system changes, sequence numbers and table view;
- the substrate's own host functions filed among the other tools, and each
  tool's runtime, permissions, source and triggers;
- on Providers, every other package the repository holds, bundle ids and
  versions, which record each declared input uses, and each account's
  connection details;
- on Search, each hit's raw per-arm scores;
- on Settings, the **Developer** section.

### Display names

A collection is labelled by a display name the console builds from the kind's
name: the lowercase compound word is split into known words, acronyms and
brands keep their capitals, and the last word takes the plural (`person` reads
**People**, `calendareventseries` **Calendar event series**, `apikey` **API
keys**). A name that does not split cleanly into known words reads as itself,
capitalised. A record with no title reads **Untitled _person_**, never its id.

Display names are labels only. The console never sends one to the API and
never lets one stand where a kind is identified: the identifier is always the
full reference `{authority}/{package}/{name}`
([0101](decisions/0101-a-kind-trait-or-callable-is-named-in-full-on-every-surface.md)),
which is what every address, every copy button and every technical-mode label
carries.

### Layout preferences

The console's preferences live on one record,
`substrate.reamde.dev/core/consolepreference/navigation`, so every browser
signed in to the repository looks the same. It carries the sidebar's state
(`collapsed`, `favorites`, `sidebarOpen`) and the layout settings
(`recordWidth`, `tableWidth`, `density`, `technicalDetails`, `theme`), each
optional, so an absent one is the console's own default. A change is a
read-modify-write under `ifVersion`, retried against fresh state after a
conflict, so two sessions changing different settings keep both.

A repository whose stored `consolepreference` kind predates a setting refuses
the undeclared property, so the console writes a setting to the record only
when the stored declaration names it; otherwise the setting stays in this
browser's `localStorage`. A read takes the record first, then `localStorage`,
then the default, and every written setting is mirrored into `localStorage` as
well, so the sign-in page already starts from it.

A few conveniences are per browser by design and live only in
`localStorage`: a collection's last filters, sort and nesting, its columns,
and the Search page's ranking choice.

## Home

**Home** (`/`) is what the substrate holds and what just happened: the four
cards (how many collections and providers, the agents by name, how many tools
and how many of them came from your providers), up to nine **Collections**
with their record counts (yours first, those holding something leading, then
what providers bring in), and **Recent changes** in [History](#history)'s
sentences. Nothing on Home asks you to act.

## All data

**All data** (`/data`) is every collection, in the sidebar's sections, one
table each: the collection, what it holds and how many records. **Your data**
is yours to change; a **From _Provider_** section holds copies that provider
keeps up to date. In everyday mode a section lists its primary collections and
says how many supporting ones it leaves out and where they are reached from;
with Technical details on it lists every kind with its full reference and its
purpose, the **Substrate** section included.

The two segments above a kind each have a page too: `/data/{authority}`
tables every kind that authority publishes, package by package, and
`/data/{authority}/{package}` tables one package's kinds.

### Add a collection

**Add a collection** offers three ways to start one, and `?add=agent`,
`?add=sample` or `?add=yaml` opens it on that way, so another page can link
straight to it:

- **Ask an agent**: say what you want to keep, and the console opens
  [Agents](#agents) with that as your first message.
- **Start from a sample**: the shipped [samples](bundles-catalog.md) (tasks,
  people, notes and the rest), each with **Add**. Adding one imports it under
  the repository's own authority
  ([0048](decisions/0048-providers-are-published-samples-are-copied.md)),
  taking first any package it requires that the repository does not hold, and
  says so beforehand. A sample already taken reads **Added**, or offers
  **Upgrade** when the binary ships it at a newer version than the copy was
  taken at; where the copy was edited since, the confirmation says the edits
  are replaced
  ([0070](decisions/0070-a-copy-is-upgraded-through-its-origin-stamp-and-requires-pins-a-floor.md)).
- **Write it yourself**: a kind is a YAML document naming its properties,
  applied from the command line; the dialog gives the two `substratectl`
  commands, one to print an existing kind to start from and one to apply
  yours. A new kind shows up as soon as it lands.

## A collection

A collection lives at `/data/{authority}/{package}/{kind}`: a data address is
the kind reference, segment for segment. The header carries the display name,
the full reference with a copy button, and the everyday description; a
provider's collection says its records are read-only copies kept up to date by
that provider, and has no **New** button. With Technical details on,
**Definition** (`?tab=definition`) shows the declaration.

The records are a grid that fills the page, with a pinned header row and a
pinned title column. The columns come from the declaration: the title first,
then the states, the time stamps the kind's temporal trait binds, the
references (each read as the referent's title, fetched beside the page with
`expand`), the enums and the other short values; paragraphs and blobs never
earn a column, and the last change closes the row. A column that holds nothing
on the rows loaded opens hidden, and the footer says how many are. **Columns**
shows, hides and reorders them, per collection, and Reset returns the
collection's own defaults. **Sort** (or a header click) picks the order; the
default is newest change first.

The grid narrows two ways, and both travel in the URL (`?filter=`,
`?search=`) so a view can be shared. **Filters** are one control per
property, offered only for the properties the server will filter. Typing into
a text property (`string`, `text`, `markdown`) is a full-text `match` on that
property's own words, in the [search grammar](api.md#the-search-grammar):
every word must appear, `lay*` is a word prefix, `"a phrase"` keeps words
together, `-word` excludes, `a OR b` takes either, and a leading `=` asks for
the exact value instead. An email, URL or phone takes the exact value, or a
trailing `*` for starts-with; a state or an enum offers its values; a comma
means any of. A reference pinned to a kind (`assignee`, at `person`) offers
that collection to pick from, by title, with a search on top; several picked
records mean any of them, which is the wire's `in`. A reference pinned to no
kind takes the record's whole `<kind>/<id>` path as text. The **search box**
beside the filters is the same grammar against every text the kind indexes at
once (the filter's `search` arm), composed with the filters and the sort, so
the grid stays a grid: the rows that match, in the order you chose, paged like
any other list.

Opening a collection at its bare address restores the filters, sort and
nesting you last used there; an address that names them always wins, so a
shared view stays exact. A search is never restored: it is the question of the
moment, not the shape of the view.

A kind that declares a single-valued reference at itself (a team's `parent`
pinned at `team`, a thread's `parent` thread) opens as a tree, by `parent`
where it declares one and otherwise by its first such reference. The rows are
the records that name no parent, and each opens in place onto the records that
name it, one level per read, in the order the grid sorts by. The nesting
switch in the toolbar is on by default and remembered per collection; off
(`?nest=false`), the same rows are a flat list. While a filter or a search is
set the grid is flat regardless, so a match is shown wherever it sits.

A page is fifty rows, and `?page=` makes one linkable. The footer shows the
range and the total from a bounded count, marked `+` where the count stopped
at its ceiling; **Next** follows the page's own cursor rather than that count,
so a collection past the ceiling still pages to its end. Under a tree the
footer counts the collection and its top level both.

## A record

A record lives at `/data/{authority}/{package}/{kind}/{id}` and reads like a
document, top to bottom on one page. The head is the kind's glyph, the title
(edited in place where the kind titles itself from a property you write), and
one line saying what it is and who added and last changed it; with Technical
details on, the line is its full reference with a copy button. The **⋯** menu
holds **Delete**, which asks first. A provider's copy is read-only here
throughout, and says to change it at the provider.

**The properties** are a sheet: one row per property, its label (and
its key, with Technical details on) on the left and its value on the right,
with empty properties folded into one line that expands. Clicking a value
edits it in place, with the control its datatype earns: a text box, a number,
a date and time, a textarea for prose, a list to pick from for an enum, the
moves a state may make, a record picker for a reference, and, for the shapes
that need room (lists, objects, maps, JSON), the whole control in a panel under
the row. Enter or leaving the box saves, Esc cancels. A save is a `patch`
naming only that property and carrying the version the page read (`ifVersion`),
so an edit against a stale page is refused and says to reload rather than
silently winning; a state move is the same patch along a declared transition.
A row that is not yours to edit says why: the engine stamps it, the host keeps
it (a declared `writer:` other than the owner), the whole record is a
provider's copy, or the kind never declared it. The record's prose, where the
kind has a body property, reads under the sheet and edits in place the same
way.

**Who holds each value** is a chip at the end of its row, read off the
record's [`propertyMeta`](projection.md#reading-provenance-propertymeta):
**You**, or the provider's badge and name, and an amber **_Provider_ differs**
where a live source offers something else. Opening it says who holds the value
at which [tier](terms.md#truth-and-derivation) and what that means for it
(**Yours**, **Synced**, **Set by provider** or **Set by an agent**), the
source record it came from, and every other version a live source offers, with
who offers it and when. **Use _Provider_'s** writes that value, which makes you
its holder at the owner tier; **Stop overriding** patches the property to
null, so projection refills it from the live sources and it follows them
again; **Use my own value** opens the editor. Both writes ask first and name
the consequence.

Under the properties, in order:

- **Sync**, on a record of any kind binding the core `sync` trait (a
  provider's account): the trait rendered whole, with **Sync now** and
  **Pause**, and a link to the account on its provider's page.
- **Connected to**: the records that point here, read through the
  [`referencing`](api.md#who-points-at-a-record-referencing) filter arm and
  grouped by the kind they are and the property they point through ("Tasks
  with this as their Assignee"), a group whose kind has a done-like state
  saying how much of it is done; then what this record points to. The slots a
  provider's copies point through are left to the next section. In everyday
  mode the substrate's machinery is left out except the merge and change
  requests that name the record, which open their review pages.
- **Where it comes from**: the provider copies that fill this record in, read
  from [`linkedFrom`](projection.md#reading-the-links-back-linkedfrom) and
  grouped by the mapping that links them ("Google contact fills in Name,
  Emails"), each copy with when it last filled anything in. A record nothing
  maps onto says only you have added to it.
- **Merged**, where anything was: each record combined into this one, when,
  and whether you confirmed it through a merge request; with Technical details
  on, the `recordmerge`, the request and the former id.
- **History**: this record's own slice of [the changelog](changelog.md) as
  sentences, newest first: who did what, and what each change did to the
  values ("Priority: High → Urgent", "Emails: + grace@example.com"), each
  value shown as the sheet shows it, long text cut to a line with the whole in
  the hover. It reads the feed with
  [`values=1`](changelog.md#values-on-request); against a server that predates
  it, a row names the properties it touched instead. A merged record's history under its former
  ids is stitched in, and where the retained changelog does not reach the
  creation, the last row says so from the record's own `createdAt`.
- **Record**, with Technical details on: the full reference, the kind and the
  version it was written under, the id and any former ids, and who created and
  last changed it, raw actor ids included.

With Technical details on, the **source** button in the head swaps the page
for the record's [envelope](data-model.md#the-envelope) as YAML, every kind
reference and record reference a link, and **Edit YAML** opens the editor.

## The record editor

**New** on a collection opens `/data/{authority}/{package}/{kind}/new`, laid
out like the record it will become: the title as a large input, then the
property rows, the optional ones folded into one line, and the prose under a
divider. **Edit YAML** on a record opens `…/{id}/edit`. Both are two **lenses
over one document**, and the document is the apply-able envelope.

- **Form** is composed from the declaration: one control per declared
  property, carrying its description and a worked example. An enum is a
  dropdown of what the kind admits, a `state` offers its machine's states, a
  `reference` picks a record of the kind it points at, a `secret` is
  write-only (a read serves `<redacted>`, and leaving the field blank keeps the
  sealed value), and a `json` property gets a JSON editor. Host-managed
  properties (a declared `writer:` that is not the owner) are never offered.
- **YAML** is the expert lens: the whole envelope in a code editor that knows
  the kind. **Completion** offers what may be written where the cursor is (the
  envelope's keys, the declared properties with their datatype and one-liner
  and never one already written, an enum's admitted values, a state machine's
  states, the kinds a reference may name), **diagnostics** underline a refused
  value on the line it sits on and mark it in the gutter, and **hovering** a
  property line shows what the kind says about it. **Format** reformats the
  document.

A new record opens on the form, and **Write YAML** (with Technical details on)
switches lens; the edit page opens on YAML, with **Form** beside it. Both
lenses edit the same text, so switching loses nothing and a hand-written
comment survives being edited on the form. Everything is checked against the
declaration **as you type** — the datatypes, required properties, unknown
keys, the shape of a reference value, and the two rules that belong to the
write rather than the value: a `put` may not move a state (that transition is
a `patch`), and the id in the document is not a rename. Problems key to their
line, the gutter marks them, and Save is barred while an error stands. A
create goes out as `POST`, an edit as the ordinary `put`, and a refusal from
the server is shown in place.

## Merge and change requests

A proposed [merge](projection.md#merge-requests) is an ordinary
`substrate.reamde.dev/core/recordmergerequest` record, and one opens at
`/merge-requests/{id}`: the matcher's evidence, a field-by-field comparison of
the two records that says what the merge will do to each row, and accept or
reject, each behind a confirmation, with an optional note. Accepting is an
ordinary state transition, and performing the merge is what that transition
does.

A [gated](agents.md#the-policy-door) agent write lands as a
`substrate.reamde.dev/core/recordpatchrequest` instead of applying, and one
opens at `/change-requests/{id}`: the rationale and who proposed it, then what
accepting would do — a patch field by field against the target's live values,
a create as the record it would make, a delete as the record it would remove —
and accept or reject. Accepting is the state transition that applies the
change; a refused apply comes back on the request rather than as a
half-applied change.

Both queues are their collections, under **Substrate** with Technical details
on. A request is also reached from the records it names (their **Connected
to**), and a change request from the agent's thread, as a card.

## Agents

**Agents** (`/agents`) is a chat over your [agents](agents.md): every
conversation on the left, the one being read in the middle, and the agent's
panel on the right.

- **The chats column** has **New chat**, a search over the chats' titles,
  every conversation under Today, Yesterday and Earlier, and the agents
  themselves: one you can talk to starts a chat, and one that only works for
  other agents says so.
- **The conversation** is a thread, and a thread is a run. It is rebuilt from
  the `llm/message` records the loop wrote, not from the browser's memory, so a
  reload shows the same conversation; while a run streams, the same turns fill
  in live and are replaced by the stored rows when it settles. Your messages
  are bubbles; the agent's consecutive turns read as one reply, each tool call
  as one line saying what it did and whether it worked, opening onto what came
  back (with Technical details on, the function and the request and response
  verbatim). A thread a trigger started opens with the record whose change
  started it.
- **Suggested changes** are cards inside the thread, not a queue somewhere
  else: each reads the change request's live state, says what it would change
  as before and after, and offers **Apply** (**Add it**, **Delete it**),
  **Dismiss**, **Edit first** (the review page) and, on a write the
  [policy door](agents.md#the-policy-door) held, **Always allow this**, which
  writes a narrow `recordpatchpolicy` allowing exactly this agent, this kind
  and this verb before applying. Deciding writes a message into the thread and
  resumes the agent. Questions the agent asks are cards too, answered in
  place.
- **The panel** says what the agent can see, change and ask, which tools it
  uses, its model and its provider, and links **Edit agent** to its record;
  with Technical details on it adds the system prompt, each tool's reference,
  the grants and the budgets.

The address names what is open: `?thread=<id>` opens one conversation,
`?agent=<id>` opens a new chat with that agent, and `?prompt=<text>` fills a
new chat's message box, which is how other pages hand over a question. A bare
`/agents` opens the most recent conversation, or a new chat when there is
none; the old per-agent address `/agents/{id}` redirects to `?agent=`, keeping
a `?thread=` it carried.

The [`llm/provider`](agents.md#providers) rows are not on this page: an agent
names one by id, its panel shows which, and a provider without a key says so
where the conversation would otherwise fail. The rows are the
`substrate.reamde.dev/llm/provider` collection, and
[registering one](agents.md#registering-a-provider) is an ordinary record
write.

## Tools

**Tools** (`/tools`) lists everything that can act on your data besides you,
as cards: **Tools your agents use**, **Syncs** (the functions your providers'
triggers run), **Other tools** that nothing uses yet, and, in everyday mode,
**Built in**, the substrate's own host functions, which technical mode files
with the rest. Each card says what the tool is for and when it runs.

A tool is a [function](functions.md), and its page is at
`/tools/{authority}/{package}/{name}`, the function's reference segment by
segment. It says what the tool is allowed to do (can see, can change, the
internet, can ask), when it runs (its triggers in words: a schedule, a change
to a kind, a webhook), which agents use it, what it takes and gives back, and
its **Recent runs**, each with when, what happened and whether it worked. A
tool a trigger runs has **Pause** and **Resume**, and a sync **Sync now**.
**Try it** is a form built from the declared arguments that runs the tool
once, now, through the call API and shows what it gave back; it is offered
where a direct call can run it, which a sync (started with Sync now) and a
host function that writes (bounded by a calling agent's grant) cannot. With
Technical details on, **Developer** adds the runtime, what the model reads,
the permissions, the source and the triggers.

[Triggers](functions.md#triggers) have no page of their own: they are
ordinary records, so `substrate.reamde.dev/core/trigger` is their collection,
and one trigger's record page is the trigger.

## Providers

**Providers** (`/providers`) is every service that can bring data in, one card
each: the shipped catalog's [providers](bundles-catalog.md) joined with what
this repository holds of them. A card says in one pill where the provider
stands (**On**, **Set up**, **Needs attention**, **Paused**) or offers
**Add**, and one line says what is true right now. With Technical details on,
**Other packages** lists every other bundle the repository holds (imported
samples, the seeded `llm` package, anything applied directly) with its
version, state, update and removal, and states any pending upgrade of the
seeded packages.

A provider's page, `/providers/{authority}/{package}` (a provider is a bundle,
and a bundle's id is its package), is where it is set up and where you come
back to it:

- **Set up** is four numbered steps: add it, give it sign-in details, connect
  your account, choose what to bring in. A done step says what is true; the
  current one is highlighted with its one action. **Sign-in details** asks for
  what to create with the provider (an OAuth client's id and secret, or a
  token provider's key), shows the OAuth callback URL to register as that
  client's redirect URI, and says which secrets are set without ever showing
  one. **Connect your account** opens the account dialog, which asks only what
  the owner decides, read off the account kind's declaration: what to bring
  in, one toggle per stream, and the sync's schedule and depth. On an OAuth
  provider its one button is **Create and connect**: it creates the account
  and opens the provider's [consent](bundles.md#the-oauth-facility) in a new
  tab, and the account reads connected when the approval returns.
- **Accounts**: one row per account, with how its sync is doing in plain
  words, when it last synced and how often it does, and the verbs an account
  takes: **Connect** or **Reconnect**, **Sync now**, **Pause**, change what it
  brings in, and **Disconnect**. `?account=<id>` scrolls to one and highlights
  it.
- **What it adds**: what it brings in (its primary kinds, each with what it
  is, how many records it holds and which of your own kinds its records fill
  in through a mapping, with the supporting kinds counted) and its tools, each
  with when it runs and how its last run went.
- **Settings**: its `setting` and `secret` records as one form
  ([Settings](bundles.md#settings)), a secret write-only, and anything else it
  still needs, in the server's words.
- With Technical details on, **Connection details** for each account (the
  core `sync` trait whole; the record triggers on its kind with cursor, lag,
  last fire, parked and pending, and **Wake** and **Run**; the newest
  `triggerrun` rows; the parked deliveries, each with **Retry**) and **Needs
  these packages**.

**Pause** and **Remove** sit in the header, each confirmed in plain words.
Remove walks the lifecycle the server enforces: pause, delete what it brought
in, then remove its declarations. An upgrade the binary ships is offered as
**Update**, confirmed first where the preview is lossy, and stated instead of
offered where the server would refuse it, with the guard lines that say what
to migrate. The same page serves any other package the repository holds, such
as an imported sample, without the setup steps and accounts.

The old addresses land here: `/registry` and `/connections` open Providers,
`/registry/{bundle id}` that bundle's page (a shipped sample's opens All
data), `/connections/{authority}/{package}/{kind}/{id}` its provider's page at
that account, and `/settings/{bundle id}` that page's Settings.

## History

**History** (`/history`) is [the changelog](changelog.md) as sentences, newest
first, grouped by day, following new changes live. A change to one record
says its values, a run of changes to it their net effect, and a run across
many records the properties they touched. Four views narrow it by
who made the change: **Everything**, **By you**, **By agents** and **By
providers**, each an actor filter the change feed applies server-side.

In everyday mode, changes to internal kinds (trigger runs, tokens,
preferences) are system changes and are hidden; a line says how many, and
**Show** brings them back (`?system=true`). With Technical details on they
are shown, each sentence carries its sequence number and raw actor id, and
**Table view** is the full changelog table: filters for authority, kind,
actor, op, a time range and free text, all in the URL, and the same live
tail. An actor opens at `/actors/{id}`: who it is, and History narrowed to
it. The old address `/changelog` redirects here with its filters.

## Search

`/search` is the [ranked read](api.md#search) as a page: a query in the
[search grammar](api.md#the-search-grammar), a collection to narrow to (or
every one), and the hits best first, each with its collection. How to rank is
the reader's choice and it sticks: **Words** (the default: full-text over
every indexed text, free, and it answers on every repository), **Words +
meaning** (the fused hybrid ranking, which falls back to words alone where no
embeddings provider is configured) or **Meaning** alone (embedding similarity
over the properties that opted in, which needs a provider and spends an
embedding call per search). With Technical details on, each hit adds its full
reference and its raw per-arm score, labelled: `words` is the lexical rank,
`meaning` the embedding similarity. A search the server refuses — a mode with
no provider behind it, a query with no word in it — shows the server's own
problem, verbatim. When the semantic index is still being built, the page says
how many values are pending.

## Settings

**Settings** (`/settings`):

- **Layout**: record page width (narrow, wide, full; it also sizes tool and
  provider pages), table width (wide, full), row height (comfortable,
  compact), Technical details, and appearance (system, light, dark), saved as
  [layout preferences](#layout-preferences).
- **Account**: the repository name you sign in with, and the two credential
  changes: change your password, and replace your authenticator where the
  second factor is on. Both ask for your current password (and code) in the
  form, because
  [the password-factor rule](auth.md#the-credential-and-the-password-factor-rule)
  refuses a bearer token here.
- **Signed in**: this browser first, then every other browser and script
  holding a token, newest first, each one **Sign out…** away. A session is a
  token record, and signing one out deletes it.
- **Your data**: **Download everything**, the recovery export of the
  repository as of now ([backups](operations.md#backups)).
- **Developer**, with Technical details on: the system kinds under
  `substrate.reamde.dev`, **API tokens** (label, created, expiry; mint one for
  a script or a device, each with full access, and revoke), and what this
  server says about itself at `/.well-known/substrate/server.json`: its
  version, its API endpoint and its features.

The old addresses `/account` and `/account/tokens` redirect here.

Next: [running one locally](running-locally.md), the substrate on your own
machine.
