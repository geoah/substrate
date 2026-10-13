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

Everything else (History, Search, Settings) serves those four, and there is
no inbox: a change an agent suggests is a card in its thread, and a merge
request is reached from the records it names
([0130](decisions/0130-the-console-serves-four-things-and-its-navigation-follows-them.md)).
[The console's design guide](console/design-guide.md) describes what every
page shares (the two readers, layout, tokens, words and accessibility), and
[the element catalogue](console/elements.md) the components they are built
from.

## Navigation

The sidebar holds, top to bottom: the repository name, which opens the account
menu (**Account and settings**, **Appearance**, the **Technical details**
switch, and **Sign out…**, which asks first), a search button that opens ⌘K,
the five places (**Home**, **All data**, **Agents**, **Tools**,
**Providers**, with the number of providers added beside the last, and
[**Observations**](#observations) after All data where some kind records
measurements),
**Favorites**, the collections, and at the foot **History**, **Settings** and
the **Technical details** switch. Collapsed, it peeks over the page while the
pointer rests at the window's left side or on its toggle.

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
([0133](decisions/0133-a-kind-declares-its-purpose.md)): `primary` is a thing
you browse and open directly, `supporting` is a detail of another kind,
reached from the records it belongs to, and `internal` is machinery. A kind
that declares none reads as `primary`, so a kind you or an agent declares is
listed without anyone classifying it, and every kind under
`substrate.reamde.dev` reads as `internal`. In everyday mode a section lists
its primary collections only. A section heading folds its section away and
remembers it; provider sections start folded, the others open. Resting on a
collection opens its card beside the sidebar: what it holds, where it comes
from ("Made by _agent_" for an agent's app), how many records it has and its
reference. Hovering a collection also shows a star that adds it to
**Favorites**, above the sections, where up and down controls reorder it.

**⌘K** (Ctrl-K elsewhere) jumps to a record, a collection or a page, or hands
what you typed to the [Search page](#search) as a records query. Typing runs
the ranked read (words, five hits) after a short pause and lists the records
it finds first; like the Search page it reaches what you keep and its
details (`filter.purposes` `primary` and `supporting`), and with Technical
details on a switch, **Include the substrate's own records**, widens it to
every purpose and travels to the Search page with the query. Collections and
pages match when every word typed starts a word of their name. It lists
collections the way the sidebar does: primary ones in everyday mode, every
kind with its purpose labelled with Technical details on.

The header above every page is a breadcrumb that reads as where the page sits:
`Your data / Tasks` or `From Google / Contacts` for a collection, then the
record's title; with Technical details on it spells the kind reference segment
by segment and ends in the record id.

### Technical details

The **Technical details** switch, at the foot of the sidebar, in the account
menu and on the Settings page, is one setting for the whole console
([0131](decisions/0131-the-console-writes-for-two-readers-behind-one-switch.md)).
Off, the console speaks in everyday words. On, it adds what a developer or an
agent author needs, without taking anything away:

- every kind, supporting and internal ones included, and the **Substrate**
  section; the sidebar becomes the authority → package → kind tree, each kind
  by its label with its own name in faint mono beside it, and tagged with its
  purpose when that is not `primary`;
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
- on Search, each hit's raw per-arm scores, and on Search and ⌘K the switch
  that includes the substrate's own records;
- on Settings, the **Developer** section.

### Display names

A collection is labelled by the display label its kind declares
([0117](decisions/0117-a-kind-may-declare-its-display-label.md)): Slack's
`conversation` reads **Channels**, Google's `calendarseries` **Repeating
events**. A kind that declares none is named from its name (`person` reads
**People**, `calendareventseries` **Calendar event series**, `apikey` **API
keys**; [how](console/design-guide.md#words)). The registry read remembers the
labels, so a surface that holds only a reference names the kind the same way.
A record with no title reads **Untitled _person_**, never its id.

Display names are labels only. The console never sends one to the API and
never lets one stand where a kind is identified: the identifier is always the
full reference `{authority}/{package}/{name}`
([0101](decisions/0101-a-kind-trait-or-callable-is-named-in-full-on-every-surface.md)),
which is what every address, every copy button and every technical-mode label
carries.

### Layout preferences

The console's preferences follow the person: they live on one record,
`substrate.reamde.dev/core/consolepreference/navigation`, so every browser
signed in to the repository looks the same
([0132](decisions/0132-console-preferences-follow-the-person-and-a-window-fact-stays-in-the-browser.md)).
It carries the sidebar's folded groups and favorites (`collapsed`,
`favorites`), each collection's saved views (`views`) and the layout settings (`recordWidth`, `tableWidth`, `density`,
`technicalDetails`, `theme`), each optional, so an absent one is the console's
own default. Whether the sidebar is open is a fact about one window, so it is
kept in this browser alone and never written to the record, and a
`sidebarOpen` an older console stored there is ignored. A change is a
read-modify-write under `ifVersion`, retried against fresh state after a
conflict, so two sessions changing different settings keep both.

A repository whose stored `consolepreference` kind predates a setting refuses
the undeclared property, so the console writes a setting (or the views) to the
record only when the stored declaration names it; otherwise it stays in this
browser's `localStorage`, and the first save onto a record that declares it
carries it across. A read takes the record first, then `localStorage`,
then the default, and every written setting is mirrored into `localStorage` as
well, so the sign-in page already starts from it.

A few conveniences are per browser by design and live only in
`localStorage`: whether the sidebar is open, a collection's last filters, sort,
nesting and grouping, its columns, and the Search page's ranking choice.

## Home

**Home** (`/`) is what the substrate holds and what just happened: the four
cards (how many collections and providers, the agents by name, how many tools
and how many of them came from your providers), up to nine **Collections**
with their record counts and where each comes from ("Yours", "From Google",
"Made by _agent_" where an agent declared its package; yours first, those
holding something leading, then what providers bring in), and **Recent
changes**, the newest six of [History](#history)'s sentences from one read.
Against a server that cannot count a collection, a card reads at most 100 of
its records once and says **many** past that.

While an [alert](functions.md#driving-triggers) is open, **Needs attention**
sits under the cards: how many alerts are open, and up to five of them, each
with its level and summary, linking to the agent's or tool's page it is about.
That page, and the provider's page for the provider's tools and agents, lists
the open alerts about it with the last error, when each was first and last
seen, its count, and a link to the alert's record, where it can be resolved by
hand. Apart from the open alerts, which link to the pages that explain them,
nothing on Home asks you to act; with no alert open, nothing on Home does
([decision 0148](decisions/0148-an-ongoing-problem-is-one-alert-record-written-under-the-callable-it-is-about.md) amends
[0130](decisions/0130-the-console-serves-four-things-and-its-navigation-follows-them.md)
for this).

## All data

**All data** (`/data`) is every collection, in the sidebar's sections, one
table each: the collection, what it holds and how many records. **Your data**
is yours to change; a **From _Provider_** section holds copies that provider
keeps up to date. In everyday mode a section lists its primary collections and
says how many supporting ones it leaves out and where they are reached from;
with Technical details on it lists every kind with its full reference and its
purpose, the **Substrate** section included. A section holding a package an
agent declared adds a **Made by** column.

The two segments above a kind each have a page too: `/data/{authority}`
tables every kind that authority publishes, package by package, each package
naming the agent that made it where one did, and
`/data/{authority}/{package}` reads one package as the app it is: its
**Collections**, the **Tools** it ships, and, where an agent declared it,
**Made by** with that agent's card and a way to ask it. Who declared a
package is the `package` row's `declaredBy`, stamped by the engine
([0109](decisions/0109-a-package-row-names-the-actor-that-declared-it.md));
a package created before the stamp names no one.

### Add a collection

**Add a collection** offers three ways to start one, and `?add=agent`,
`?add=sample` or `?add=yaml` opens it on that way, so another page can link
straight to it:

- **Ask an agent**: say what you want to keep, and the console opens
  [Agents](#agents) in a new chat with an agent that can set a collection up
  (its write grant covers `substrate.reamde.dev/core/kind` and it holds a
  tool that writes) and sends it as the first message. When no agent can,
  the dialog says so and offers a sample instead.
- **Start from a sample**: the shipped [samples](bundles-catalog.md) that
  declare at least one primary kind (tasks, people, notes and the rest), each
  by the collections it adds and with **Add**. A kind's
  [purpose](decisions/0133-a-kind-declares-its-purpose.md) is read from the
  catalog's closure until the repository holds the kind, so a sample whose
  kinds are all supporting or internal is not offered as a collection. Adding
  one imports it under the repository's own authority
  ([0048](decisions/0048-providers-are-published-samples-are-copied.md)),
  taking first any package it requires that the repository does not hold, and
  says so beforehand. A sample already taken reads **Added**; its package
  page offers the update when the binary ships it at a newer version than the
  copy was taken at, and where the copy was edited since, the confirmation
  says the edits are replaced
  ([0070](decisions/0070-a-copy-is-upgraded-through-its-origin-stamp-and-requires-pins-a-floor.md)).
- **Write it yourself**: a kind is a YAML document naming its properties,
  applied from the command line; the dialog gives the two `substratectl`
  commands, one to print an existing kind to start from and one to apply
  yours. A new kind shows up as soon as it lands.

## A collection

A collection lives at `/data/{authority}/{package}/{kind}`: a data address is
the kind reference, segment for segment. The header carries the kind's glyph,
its display name and its everyday description, and, with Technical details
on, the full reference with a copy button; a provider's collection says its records are read-only copies kept up to date by
that provider, and has no **New** button. The star beside **New** adds the
collection to the sidebar's **Favorites**, or takes it off. With Technical details on,
**Definition** (`?tab=definition`) shows the declaration.

The records are a grid that fills the page, with a pinned header row and a
pinned title column. The columns come from the declaration: the title first,
then the states, the time stamps the kind's temporal trait binds, the
references (each read as the referent's title, fetched beside the page with
`expand`), the enums and the other short values; paragraphs and blobs never
earn a column, and the last change closes the row. A column that holds nothing
on the rows loaded opens hidden, and the **Columns** button says how many
are. So does a property the kind marks `deprecated`, and on a provider's
collection every property a writer role other than the owner keeps (sync
cursors, statuses, tokens). The title is the row's one link to the record; a
parent row's chip reads "1 / 3" beside a ring and says "1 of 3 subtasks
done". **Columns**
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
once (the filter's `search` arm), each typed word also matching as the start
of a longer one, composed with the filters and the sort, so the grid stays a
grid: the rows that match, in the order you chose, paged like
any other list.

**Group** cuts the grid by an enum, a state or a single reference
(`?group=`): the list is ordered by that property first and the view's own
order inside each group, so every page is whole runs of groups, and each run
sits under a head that names the value, counts the whole group (a bounded
count over the view's filter narrowed to that value) and folds it. A page
heads only the groups its own records are in, which the Group menu says, so a
value with no record on the page has no head there. A run that carries on from
or to another page says so; a run that fills a middle page may begin or end on
it, so it says how many of the group's records are on other pages instead.
Nested, a subtree stays in its top-level row's group, and the tree's own
reference is not offered. The groups
come in the server's order: an enum or a state by its stored value, a
reference by the path it stores, the records with none last.

**Saved views** are the tabs above the toolbar: **All** (the collection as it
opens), each view saved for this collection, and **Save view**. A view is a
name for a filter, sort, column set, nesting and grouping, never a search or a
page, and it is kept on the console preference record, so it follows you to
every browser. A tab is lit when the grid shows what it names, the one you
picked where two name the same; the view you picked last stays marked once
you change something, and its menu saves those changes to it or discards
them. Saving what a view already shows is refused, naming that view. Saving,
renaming and deleting ask first.

Opening a collection at its bare address restores the filters, sort, nesting
and grouping you last used there; an address that names them always wins, so a
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

A page is fifty rows read by offset (the wire's `offset=`), so any page is
one request and `?page=` makes one linkable. The footer shows the range and
the total from a bounded count, marked `+` where the count stopped at its
ceiling. Whether there is a next page is not taken from that count: the page
answers with a cursor when more rows follow, and **Next** is offered on that
alone (the cursor itself is never followed), so a collection past the ceiling
still pages to its end. Under a tree the footer counts the collection and its
top level both.

## A record

A record lives at `/data/{authority}/{package}/{kind}/{id}` and reads like a
document, top to bottom on one page. The head is the kind's glyph, the title
(edited in place where the kind titles itself from a property you write), and
one line saying what it is, who added and last changed it, and, where it is
so, that every value is yours; with Technical details on, the line is its full
reference with a copy button. The **⋯** menu holds **Copy link**,
**Duplicate**, **Who holds each value**, **Open in YAML** (with Technical
details on) and **Delete**, which asks first. A provider's copy is read-only
here throughout, and says to change it at the provider.

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

**Who holds each value** is read off the record's
[`propertyMeta`](projection.md#reading-provenance-propertymeta). Where every
value is yours the head says so once; otherwise a chip at the end of a row
marks a value someone else holds (the provider's badge and name, or an
agent), and an amber **_Provider_ differs** marks one a live source offers
differently. **Who holds each value** in the ⋯ menu puts a chip on every row.
Opening one says who holds the value
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
- **Measurement** or **Readings**, on a record of any kind binding the core
  `observed` trait. A single value (the trait's `single` variant) reads its
  value with its unit, the metric, source and thing it is about as record
  mentions, the form, the statistic and period of a vendor summary, when,
  the revision and the record that delivered it. A trace (the `packed`
  variant, told apart by declaring `values`) draws its values over
  `[at, endsAt)` as a line broken wherever two values sit further apart than
  its `hold`, its managed `stats` as labelled values, and every value with
  its time in a table fifty to a page. Both open the timeline on the
  record's stream.
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
  values ("Priority: High → Urgent", "Emails: + grace@example.com"), a
  property rename as one move under the new name ("Display label: renamed
  from Label", its values after it only when they differ), each
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

- **Form** is composed from the declaration. A new record's form is the
  record page's own property sheet over an unsaved draft, with the same
  editors: what the kind requires first, then what a record is commonly given
  (the records it points at, the times it is about), the rest folded, and a
  problem named only once a value has moved or **Create** is asked for. An
  edit's form is one control per declared property, carrying its description
  and a worked example. An enum and a `state` are picked from what the kind
  admits, a `reference` picks a record of the kind it points at, a `secret` is
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
`/merge-requests/{id}`: the matcher's evidence, a property-by-property
comparison of the two records that says what the merge will do to each row
(**Kept** or **Combined**), and **Combine them** or **Keep them apart**, each
behind a confirmation, with an optional note. Combining is an ordinary state
transition, and performing the merge is what that transition does.

A [gated](agents.md#the-policy-door) agent write lands as a
`substrate.reamde.dev/core/recordpatchrequest` instead of applying, and one
opens at `/change-requests/{id}`, read like the chat card that led there: the
change it would make to which record, the rationale, and which agent
suggested it and when; then what applying would do — a patch property by
property, the values now beside the values if applied, a create as the record
it would make, a delete as the record it would remove — and **Apply** or
**Dismiss**. Applying is the state transition that applies the change; a
refused apply comes back on the request rather than as a half-applied change.
The **If applied** values are editable with the property sheet's own
editors, and a value can be left out and put back; nothing is stored until
**Apply**, which then sends the owner's values as the accept's `adjustedDiff`
([0115](decisions/0115-an-owner-adjusts-a-change-request-on-the-accepting-write.md)),
checked against the version of the record the page showed. A decided request
the owner adjusted shows what was suggested beside what was applied, and the
chat card and the thread say it was applied with your edits. With Technical
details on the page adds the ids, the op and versions, the policy that held
the write, the thread it came from, a judge's verdict, the raw diff and the
adjusted one.

Both queues are their collections, under **Substrate** with Technical details
on. A request is also reached from the records it names (their **Connected
to**), and a change request from the agent's thread, as a card.

## Agents

**Agents** (`/agents`) is a chat over your [agents](agents.md): every
conversation on the left, the one being read in the middle, and the agent's
panel on the right.

- **The chats column** has **New chat**, a search over the chats' titles,
  every conversation under Today, Yesterday and Earlier, and the agents
  themselves. It lists the primary agents you talk to; **Show more agents**
  adds the supporting and internal ones, each tagged with its
  [purpose](agents.md), and the ones that only work for other agents. One you
  can talk to starts a chat, and one that only works for other agents says so. **Add agents** lists the shipped samples that bring
  agents, each with **Add**, the same import as
  [Add a collection](#add-a-collection)'s samples.
- **The conversation** is a thread, and a thread is a run. It is rebuilt from
  the `llm/message` records the loop wrote, not from the browser's memory, so a
  reload shows the same conversation; while a run streams, the same turns fill
  in live and are replaced by the stored rows when it settles. Your messages
  are bubbles; the agent's consecutive turns read as one reply, each tool call
  as one line saying what it did and whether it worked. Opening the line shows
  what came back: the records a search found (or that nothing matched), the
  records a write changed, a sub-agent's reply, a function's output, or why the
  call failed. A call with nothing more to say than its check mark does not
  open. With Technical details on, every line opens and adds the function and
  the request and response verbatim. Only what you act on sits under the line
  whether it is open or not: a suggested change and a batch of questions. A
  thread a trigger started opens with the record whose change started it.
- **Suggested changes** are cards inside the thread, not a queue somewhere
  else: each reads the change request's live state, says what it would change
  as before and after, and offers **Apply** (**Add it**, **Delete it**, a
  delete confirming first), **Dismiss** and **Review** (the change request
  page). A suggestion a gate held also offers **Always allow this**, which
  asks first ("_Agent_ will add and change tasks without asking. You can
  take this back in the agent's panel."), then saves an `allow` naming that
  gate in `overrides` for this agent, kind and verb, one per door verb
  ([0108](decisions/0108-an-allow-outranks-a-gate-only-by-naming-it.md)),
  and applies the suggestion. Deciding writes a message into the thread and
  resumes the agent. Questions the agent asks are cards too, answered in
  place.
- **The panel** says what the agent can see, change and ask, what it is
  always allowed to do (each rule with **Revoke**), which tools it uses, its
  model and its provider, and links **Runs and permissions** to the agent's
  page and **Edit agent** to its record; with Technical details on it adds
  the system prompt, each tool's reference, the grants and the budgets.

The address names what is open: `?thread=<id>` opens one conversation,
`?agent=<id>` opens a new chat with that agent, and `?prompt=<text>` fills a
new chat's message box, which is how other pages hand over a question;
`?send=1` beside it sends that question as soon as the agent can take it,
once. A bare `/agents` opens the most recent conversation, or a new chat when
there is none.

**An agent's page** (`/agents/{id}`; a `?thread=` there still opens the
chat) is what the agent is and may do: **Can see** and **Can change**,
edited by collection and written as its `permissions`, an edit that would
leave a tool it holds without its grant held back with the reason; the tools
it uses and the agents it can ask; **Always allowed**, each rule with
**Revoke**; the other rules its changes go through, in words; and its last
fifty **Recent runs**, with how many there were, how many didn't finish, the
tokens and the cost, and per run what started it, how it ended (and why, when
it failed), its tokens and its cost. With Technical details on it adds the
references and the declaration.

The [`llm/provider`](agents.md#providers) rows are not on this page: an agent
names one by id, its panel shows which, and a provider without a key says so
where the conversation would otherwise fail, with **Add your _Provider_ key**,
which writes the row's sealed `apiKey` as the record page would. The rows are the
`substrate.reamde.dev/llm/provider` collection, and
[registering one](agents.md#registering-a-provider) is an ordinary record
write.

## Tools

**Tools** (`/tools`) lists everything that can act on your data besides you,
as cards: **Tools your agents use**, **Syncs** (the functions your providers'
triggers run), **Other tools** that nothing uses yet, and, in everyday mode,
**Built in**, the substrate's own host functions, which technical mode files
with the rest. Each card says what the tool is for and when it runs.
**Add tools** lists the shipped samples that bring functions (Firecrawl,
Notes and the rest), each by the tools it adds and with **Add**, the same
import as [Add a collection](#add-a-collection)'s samples.

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
  is, how many records it holds, when it last synced and which of your own
  kinds its records fill in through a mapping, with the supporting kinds
  counted) and its tools, each with when it runs and how its last run went.
- **Recent activity**: the newest ten changes the provider and its tools
  made, in History's sentences, and **See all in History**.
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

## Observations

**Observations** (`/observations`) lists what your devices, apps and providers
measured, one row per stream: a metric written by one source about one
thing, on one of the source's channels where a source measures a metric
twice (a WHOOP cycle's heart rate beside a workout's). It is a place in the
sidebar and ⌘K only where some kind binds core's
`observed` trait, which the console finds in the registry by the trait and
never by a fixed authority, so an imported `observations` or `health` sample
under your own authority is found the same way. The rows come from one
aggregate latest read: `GET /api/v1/records` with
`filter={"implements":"substrate.reamde.dev/core/observed"}` and
`aggregate={"groupBy":["metric","source","about","channel"],"reducers":["last"],"budget":{"maxGroups":2000}}`,
no bucket and no range. Where the server refuses that read for making more
than 2,000 groups, the page lists the metrics with a point first and reads
their streams a few metrics at a time. Each row shows the metric by its record's name, the
source as a record mention, what it is about (**You** where it names
nothing, which means the owner), its channel, how its newest value reads
(**Reading**: its form in words, **Level**, **Amount**, **Running total** or
**Vendor summary**, or **Category** for a coded stream), that value in its unit to the metric's places (the stored
digits on hover, and as stored with Technical details on; a category is
the one a coded `last` answers in `code`), when, and how many points the
stream holds.

The rows sit in the grid a collection's records do
([A collection](#a-collection)), with the same controls, applied in the page
because discovery loads every stream. **Sort** or a header click picks the
order (`?sort=`, newest first by default). **Filters** offer metric, source,
about, reading and channel, each a set of values to pick, several meaning any
of them (`?filter=`). Chosen metrics narrow discovery on the server to one
latest read per metric, so adding a metric keeps the rows already read;
with no metric chosen, the chosen sources and things narrow the one read
instead. The search box matches the rows' words
(`?search=`). **Columns** shows, hides and reorders the columns, and
**Group** cuts the rows by metric, source, about, reading or channel
(`?group=`). **Saved views** keep a filter, sort, column set and grouping on
the console preference record under the collection key
`observations/streams`; a view is lit while the address matches it and never
holds the search or the selection. A bare address restores the filters, sort
and grouping last used in this browser. A row's metric opens the timeline
narrowed to its stream.

A box on each row selects its stream: shift-click selects the run from the
row clicked before, and the header box selects or clears every row shown.
The selection is in the address (`?select=`, one `metric|source|about|channel`
token per stream, by id where the reference's pin allows), so a link carries
it, and a filter that hides a selected row leaves it selected and drawn.
**Timeline** (`?timeline=true`) opens a panel above the grid that draws the
selected streams in one chart over a range (the last 30 days until you pick
another), with the bucket and what to group the series by: metric, source,
about or category (`?by=`). Each metric is one aggregate read narrowed to
its selected sources, and to its things and channels where every selected
stream names one; each keeps its own reducer and unit (`?reducer=`,
`?unit=`). A stream the read takes that the selection leaves out (WHOOP's
heart rate on no channel, beside its selected workouts) is left out of the
chart where it would draw a line of its own, grouped by source or about, and
named under the chart where it joins a selected line.

**The timeline** (`/observations/timeline`) draws the chosen metrics over one
range on one time axis. Opened with no metric, it draws the newest stream's
metric and writes it into the address (`?metric=`), so a later read never
swaps the chart. They overlay in one chart while they fit two units
and otherwise split into as few charts as hold them two units each: heart
rate, HRV and steps are two charts, heart rate with HRV and then steps. One
row of controls scopes every chart: the metrics, the range (today, the last
7 or 30 days, 6 months, a year, or a custom range of days on the calendar),
the bucket (automatic from the range, 15 minutes, an hour, a day, a week, a
month; calendar buckets in your time zone), what to group by (metric,
source, about or category), where to read from (automatic, raw points,
vendor summaries, or raw where a bucket is covered), and the sources,
things and channels to keep. Under each chart, each metric's numbers as a table that
copies as tab-separated text; with Technical details on, each request itself
as JSON and as a `curl` command.

**One chart, several metrics.** Above the chart, one row per metric holds
its name, which axis it reads on, its reducer (from the ones its form
allows, held to the ones a summary takes where a vendor summary may serve a
bucket) and its output unit (the units of the stored unit's measure the
server converts it to: never another annotation, another assay unit, a
level or a factor of pi), and its
legend. A level is a line with its lowest-to-highest band where its metric
shows one line, an amount or a count is bars, and a category is bars
stacked per code that counts as one unit, its time. The series of one unit
share the left axis and a second unit reads on the right, each axis
captioned with its unit; a third unit is not drawn, and the chart names the
metrics it leaves out and what to deselect to draw them. One crosshair marks
the hovered bucket with a dot on every line, and one tooltip lists every
series there with its value and unit; a chart of one series keeps the full
readout (every reducer, the points, the covered time, the sources, and
where it was read from). Each legend entry hides or
shows its series, and an axis goes with the last series on it. Grouping
applies to the whole chart: heart rate grouped by source is one line per
device, and ungrouped it is one line resolved across them. Click a bucket or
drag across several to zoom in; from the keyboard the arrow keys move
between buckets and Enter zooms.

**The pattern.** The chart follows Grafana's time series panel: the series
of one unit share a y axis, a second unit gets an axis on the right, one
crosshair, an "all series" tooltip, and legend entries that hide a series.
Datadog puts a request on the right axis the same way and splits a graph
into small multiples when one would hold too much, which is what the
timeline does at a third unit rather than adding a third axis. Observable
Plot has no second y axis and recommends charts side by side, and the
fitness apps (Oura on the web, WHOOP's trends, Garmin Connect's reports)
overlay one trend on a second metric with its own scale: two units is the
case they all draw together. Two scales on one plot line up arbitrarily, so
each axis carries its unit, each metric's row names its side, and its
numbers are one click below.

Each metric is one aggregate read naming it, and a chart composes its
metrics' reads on one bucket axis, each read loading, refreshing or failing
on its own row. A metric is planned once its streams are read: a metric
being added waits under the charts already drawn, and choosing a source, a
thing or a channel re-reads the charts, never the streams. While a changed read loads, the chart keeps drawing the
earlier answer and the row says Updating…, where that answer has the same
unit and grouping and carries the new reducer; after a change of unit or
grouping, or to a reducer the earlier answer lacks, the row says Reading…
over an empty chart. A metric that declares no unit is read in the unit of its
newest selected stream, named in the request, so the axis and the answer
agree. Grouped by anything but source, two or more sources are
resolved by `sources.policy: priority` in the order you picked them in the
Source filter, each new pick last (or the newest first), a stream that names
no source counting as one ranked after them, and the chart says which source
wins; grouped by source,
each keeps its own rows, and a source's channels stay apart
(`groupBy: ["source", "channel"]`). Until you pick where to read from, a
metric reads raw points, summaries where every one of its streams keeps
vendor summaries, and `prefer-raw` where some do, since a raw read takes no
summary and WHOOP's summary-only heart rate would draw nothing; with
Technical details on, the chart says so. A metric that answers no values
says why and what helps. A category metric is always grouped by code: a
coded gauge asks for the time in each category (the bare `timeInState`), a
coded amount for its total. A read the console expects to make more than
1,440 buckets, counting every group, is not asked for, and its row says to
pick a larger bucket; a refusal the server makes anyway is shown with its
reason. The read itself is [the aggregate read](api.md#the-aggregate-read).

## History

**History** (`/history`) is [the changelog](changelog.md) as sentences, newest
first, grouped by day, following new changes live. Each sentence is one of the
server's [run summaries](changelog.md#run-summaries) (`runs=1`), so "You added
1,500 organizations" is counted exactly from one read however far the run
reaches; new changes from the live tail continue the newest run. A change to
one record says its values, a run of changes to it their net effect, and a run
across many records the properties they touched, a rename said once by both
names ("Summary renamed to Overview"); the rows behind a sentence are read on
their own, only where it says them. Against a server without `runs=1` the
rows are read and folded in the browser, and the oldest sentence loaded is
said without a count. Four views narrow it by who made the change:
**Everything**, **By you**, **By agents** and **By providers**, each an actor
filter the change feed applies server-side.

In everyday mode, changes to internal kinds (trigger runs, tokens,
preferences) are system changes and are left out server-side
(`excludeKinds`), so a run is whole across them; a line says they are hidden,
and **Show** brings them back (`?system=true`). With Technical details on they
are shown, each sentence carries its sequence numbers and raw actor id after
it, and **Table view** is the full changelog table: filters for authority,
kind, actor, op, a time range and free text, all in the URL, and the same
live tail. Its rows read the date on the first row of each day and the clock
after, name the kind by its display name (the full reference on hover), and
open their detail from anywhere on the row. An actor opens at `/actors/{id}`:
who it is, and History narrowed to it. The old address `/changelog` redirects
here with its filters.

## Search

`/search` is the [ranked read](api.md#search) as a page: a query in the
[search grammar](api.md#the-search-grammar), a collection to narrow to (or
every one), and the hits best first, each with its collection. Each typed
word also matches as the start of a longer one (`ans` finds `Ansel`), and the
server ranks the word itself above its completions. The page searches what
you keep and its details (`filter.purposes` `primary` and `supporting`,
[0136](decisions/0136-search-ranks-by-bm25f-and-a-kinds-purpose.md)); with
Technical details on, **Include the substrate's own records** searches every
purpose (`?system=true`), and picking a collection searches it whatever its
purpose. How to rank is
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
  provider pages), table width (wide, full), table rows (comfortable,
  compact; the height of a collection's rows and All data's), Technical
  details, and appearance (system, light, dark), saved as
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
