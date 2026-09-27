# Substrate console redesign — design guide (the "Paper" direction)

Source of truth for every agent working on the console redesign. The owner reviewed and
approved an HTML prototype; its source is the visual/copy reference:

  /private/tmp/claude-501/-Users-geoah-src-github-com-geoah-substrate/ecb153f3-6121-45bd-9577-eebedb8a34f1/scratchpad/proto/console-prototype.html

(open it with a browser or read its CSS/JS; functions `sidebar()`, `home()`, `dataPage()`,
`collection()`/`grid()`, `record()`/`propSheet()`/`ownDetail()`/`linksSection()`/
`sourcesSection()`/`mergedSection()`/`histSection()`/`detailsSection()`, `agentsPage()`,
`toolsList()`/`toolPage()`, `providersPage()`, `historyPage()`, `settingsPage()` are the
page references; CSS classes `.docL`, `.coll`, `table.g`, `.props`, `.pdet`, `.own .ochip`,
`.lk-*`, `.hrow`, `.tcard`, `.caps`, `.io`, `.runrow`, `.set-row`, `.wopt` are the component
references). Port the look and the copy; do NOT port the prototype's code structure.

## What the console is for (owner ruling)
Seeing, navigating and controlling four things: **your data**, **your providers**, **your
agents**, **your (and your agents') tools/functions**. There is NO inbox and no "what needs
you" surface. People build apps (task managers, trip planners, recipe books) through their
agents on top of substrate; what those apps store shows up here as ordinary collections.

## Two readers
- Everyday copy for non-technical people is the default.
- A **Technical details** switch (per user, stored in the console preference record; see
  Preferences) reveals: full kind references on screen, record ids, actor ids, property keys,
  holding tiers (owner/bundle/machine), changelog sequence numbers, mapping ids, permissions,
  the YAML source view, the authority/package tree in the sidebar, supporting/internal kinds.
- Rule: everything technical is still reachable in everyday mode through hover cards
  (a record's hover card always shows its full `authority/package/name/id` reference).

## Vocabulary rules (the repo's contract; `docs/terms.md`)
- A kind is identified by its full reference `authority/package/name`. Never show a short kind
  name as an identifier. Display names (and plurals) are UI-only: fine as labels ("Tasks",
  "People"), never sent to the API, never used as identifiers.
- Dead words that must not appear in UI copy: entity, type (for kind), capability, schema,
  log (use changelog/history), extension, relationship/edge (use reference; note `relationship`
  is also a sample PROPERTY name — that's data, fine), plural (as a vocabulary term), incoming,
  username, tenant, identity, **integration** (use provider), example (for sample).
- Live words: record, kind, package, authority, provider, sample, bundle, agent, function
  (UI label "tool"), trigger, changelog (UI label "History"), reference.

## Kind display names (console only)
`names.singular` is a compound lowercase word (`calendarevent`, `gmailthread`,
`calendareventseries`). Build `displayName(kind)` / `displayPlural(kind)` in one module
(e.g. `lib/kind-names.ts`):
- Split by greedy longest-match against a small word list (calendar, event, series, email,
  message, thread, address, attachment, label, contact, group, drive, file, gmail, conversation,
  task, log, sync, account, config, person, organization, team, project, issue, pull, request,
  review, comment, milestone, repository, page, block, database, source, data, user, bot, chat,
  bridge, cycle, workflow, state, status, reaction, recovery, sleep, workout, note, digest,
  recording, instruction, transcript, scratchpad, web, document, record, merge, patch, policy,
  mapping, split, token, credential, secret, setting, trigger, run, function, agent, kind,
  package, property, type, trait, blob, bundle, actor, authority, repository, key, code, of,
  conduct, license, app, preference, console, interaction, provider, …). Unknown → the raw name.
- Capitalise the first word only ("Calendar event series", "Gmail thread").
- Plural: pluralise the LAST word (person→people, series→series, address→addresses,
  y→ies after a consonant, s/x/ch/sh→es, else +s); "Person" → "People".
- Tests for a table of real names.

## Kind purpose
Kind declarations will carry `purpose: primary | supporting | internal` (absent = primary),
landing in a separate engine branch; read it from `definition.purpose` (kinds are read as
`core/kind` records; see `lib/api/kinds.ts` `kindFromRecord`). One helper `kindPurpose(k)`.
Everyday mode lists only primary kinds in navigation and "All data"; technical mode shows all,
with supporting/internal marked. Until repositories take the upgrade, every kind reads primary
— that is expected. `substrate.reamde.dev/core` kinds are always treated as internal by the
console as well (they are machinery even before the key lands).

## Visual system (Paper)
Font: Geist (already bundled), Geist Mono only for things you would copy (ids, references,
YAML, URLs, code). Never mono for times, verbs, statuses, counts.

Light tokens: background #FFFFFF; sidebar #F8F7F5; panel #FBFAF8; hover rgba(55,53,47,.055);
selection rgba(47,107,219,.09); ink #252420; ink-2 #5D5C56; ink-3 #9A9892; line #ECEBE7;
line-2 #DEDCD6; accent #2F6BDB (text #2358BF, soft #EAF1FD); ok #2F8F5B / soft #E6F4EC;
warn #B7791F / soft #FBF1DE; bad #C94A3D / soft #FBE9E6.
Dark: background #191919; sidebar #202020; panel #1D1D1D; hover rgba(255,255,255,.055);
ink #E8E7E3; ink-2 #A9A8A3; ink-3 #74736E; line #2B2B29; line-2 #3A3A37; accent #5B92F0
(text #8AB2F6, soft rgba(91,146,240,.16)); ok #5BC08A; warn #E0A84A; bad #EA7A6D.
Kind hues (tile bg / glyph ink), light: gray #EFEEEB/#6F6D67, brown #F3EAE3/#8A5A3B,
orange #FCEBDD/#C2621D, yellow #FBF1D2/#A07A12, green #E3F2E8/#2E7D4F, teal #DDF1F0/#1F7A77,
blue #E3EDFB/#2F63C0, purple #EEE8FA/#6B4BB8, pink #FBE7F0/#B03A72, red #FBE6E3/#BE3B2E;
dark variants are in the prototype CSS (`--c-*-b/-f` under the dark media query).
Map these onto the existing shadcn tokens in `web/console/src/index.css` (background,
foreground, card, popover, primary, secondary, muted, muted-foreground, accent, border, input,
ring, destructive, warning, sidebar-*) and add new ones (`--ok`, `--ok-soft`, `--warn-soft`,
`--bad-soft`, `--panel`, `--hover`, `--kind-<hue>-bg/-fg`), both themes.
Spacing: remove the `--spacing: 0.235rem` override (back to Tailwind's 0.25rem).
Type scale: 12 / 12.5 / 13 / 14 (base UI text) / 15 (section h2) / 20 / 26 (page h1) / 32
(record title). Radius 6px base. Row heights: comfortable 38px, compact 30px.

Layout:
- Record-like pages (record, tool, provider detail, settings, home, history) are LEFT-aligned
  documents: padding 36px 48px, max-width from the `recordWidth` preference
  (narrow 720px · wide 960px · full none). Never centred.
- Table pages use the full width by default (`tableWidth`: wide 1200px · full none).

## Shared components (one component per idea; nothing drawn by hand)
- `KindGlyph` — rounded tile with a stable icon + hue per kind. Icon from a keyword map on the
  kind name (task→check-circle, project→folder, person/user/contact→user/at-sign,
  organization→building, team→users, note/document/page/file→file-text, calendar/event→
  calendar, series→repeat, email/mail/gmail→mail, message/conversation/chat→message-square,
  recipe→book-open, trip→globe, label→tag, account/config/sync→settings, default→box);
  hue = stable hash of the full reference into the 10 hues, overridable by the same keyword map.
- `KindRef` — glyph + display plural (label mode) or glyph + full reference with authority and
  package toned down and the name emphasised (reference mode). Technical mode shows the full
  reference beside labels. Hover card: display name, description, record count, purpose, full
  reference in a mono footer.
- `RecordRef` — glyph + title (never a bare id; fallback "Untitled <display name>"), mention
  style (underline on hover). Hover card: title, collection display name (+ "from Google" for
  provider kinds), up to three key property values, full reference in a mono footer.
- `ActorRef` — "You" (person icon on a neutral disc — NEVER a black letter), agents (bot icon,
  purple), provider functions/bundles (the provider's letter badge), engine (shield). Plain
  names ("You", "substrate agent", "Google Contacts sync"); the raw actor id is in the hover
  card and shown inline in technical mode. One hover card design for every actor mark.
- `StateBadge` — dot + plain word (proposed→"Suggested", open, done, abandoned→"Dropped" where
  the kind's states map cleanly; otherwise the state name capitalised); technical mode appends
  the stored value.
- `PropertySheet` — label (icon + label) left, value right, 36px rows; read and edit are the
  same row: click a value to edit in place (single-property PATCH via `patchRecord` with
  `ifVersion`, the pattern in `components/record-config-form.tsx:108-131`). Empty properties
  fold into one "N empty: A, B" line. Descriptions live in the label's hover card.
- `OwnershipChip` — at the right end of a property row, a labelled chip: "You" (person icon) or
  the provider badge + name ("G Google"); when a live source offers a different value, an amber
  "<Provider> differs" pill before it. One hover summary; click toggles a detail panel under the
  row: tier word (Yours / Synced / Set by provider, + tier key in technical mode), who and when,
  the source record (RecordRef), union members with their sources, "Other versions" with
  "Use <Provider>’s" (writes that value), and "Stop overriding · follow <Provider>" (patches the
  property to null) / "Use my own value". Data: `record.propertyMeta`.
- `PageHeader` — breadcrumb (in the top bar), title, one meta line, actions. Same on every page.
- `HoverCard` — one look: top block (title/sub), optional facts grid, mono footer with the full
  reference. Opens after ~500ms, never for whole table rows, closes on scroll.
- `CopyButton`, `IdText` (mono, only in technical mode or where the user must copy it).
- `ProviderBadge` — the provider's letter on a small bordered square in its brand colour.

## Copy
Second person, plain, short. Examples from the prototype: "Works at", "How you know them",
"Kept up to date by Google Contacts", "Linear differs", "Use Linear’s",
"Stop overriding · follow Linear", "Undo merge", "Tasks with this as their Assignee",
"1 of 3 done", "Nothing else points to this yet", "Only you have added to this. No provider
fills it in.", "What it’s allowed to do", "Can see / Can change / Internet / Can ask",
"It hasn’t run yet.", "Sign out…". Every destructive action confirms and names the consequence.
Errors say what went wrong and what to do.

## Earlier owner rulings (2026-08-06) — superseded where noted
- Actors: plain names with a small mark (the owner approved the Paper mocks, which show marks).
- Activity/history: sentence rows (approved in the Paper mocks).
- Data in mono: superseded — mono only for things you copy.
- Filters: full-size `field | value | ×` controls, never chips — KEEP.

## Engineering rules
- Read `AGENTS.md` (CLAUDE.md) house rules. Conventional commit titles (`feat(console): …`).
  No AI attribution lines in commits. Comments carry constraints, not narration.
- `internal/api` contract: the console mirrors the wire by hand; any new exported interface in
  `web/console/src/lib/api/*` must be in `wire.golden.json` or listed in `notOnTheWire`
  (wire.golden.test.ts).
- Keep `mise run ci:console` green (typecheck, lint, fmt:check, test, build). Update tests
  that assert old structure; add tests for new pure helpers.
- Never leak deployment details; `example.com` / localhost only.
